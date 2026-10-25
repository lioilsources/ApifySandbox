package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/czech-building-notices/internal/classify"
)

var errBadInput = errors.New("bad input")

func badInput(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errBadInput, fmt.Sprintf(format, a...))
}

// errWarming means the Standby index is not built yet.
var errWarming = errors.New("index is being built (first harvest of ~250 notice boards takes about a minute); retry shortly")

const (
	maxSearchLimit   = 50
	searchLookupCap  = 300 // RÚIAN lookups per search when bbox/inProtectedArea needs parcels
	noticeLookupCap  = 40
	defaultWatchDays = 7
)

type service struct {
	h *harvester

	mu      sync.RWMutex
	index   []*Notice
	byID    map[string]*Notice
	stats   harvestStats
	builtAt time.Time
}

func (s *service) setIndex(ns []*Notice, st harvestStats) {
	m := make(map[string]*Notice, len(ns))
	for _, n := range ns {
		m[n.ID] = n
	}
	s.mu.Lock()
	s.index, s.byID, s.stats, s.builtAt = ns, m, st, time.Now().UTC()
	s.mu.Unlock()
}

func (s *service) snapshot() ([]*Notice, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.byID == nil {
		return nil, errWarming
	}
	return s.index, nil
}

// keepIndex (Standby) loads the gazetteer, harvests, then re-harvests every
// interval with conditional requests.
func (s *service) keepIndex(ctx context.Context, every time.Duration) {
	for s.h.reg.Gazetteer() == nil {
		if err := s.h.reg.Refresh(ctx); err != nil {
			log.Printf("gazetteer: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Minute):
			}
		}
	}
	gazDay := time.Now().YearDay()
	for {
		ns, st, err := s.h.harvest(ctx, harvestOpts{conditional: true})
		if err != nil {
			log.Printf("harvest: %v", err)
		} else {
			s.setIndex(ns, st)
			log.Printf("harvest: %d boards (%d fetched, %d unchanged, %d failed), %d items → %d notices (%d duplicates), %.1f MB in %s",
				st.Boards, st.Fetched, st.Unchanged, st.Failed, st.Items, st.Notices, st.Duplicates, float64(st.Bytes)/1e6, st.Took.Round(time.Second))
		}
		wait := every
		if err != nil {
			wait = 5 * time.Minute
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if d := time.Now().YearDay(); d != gazDay {
			if s.h.reg.Refresh(ctx) == nil {
				gazDay = d
			}
		}
	}
}

// copyNotice gives a request its own Parcels slice to resolve into.
func copyNotice(n *Notice) *Notice {
	c := *n
	c.Parcels = append([]ParcelOut(nil), n.Parcels...)
	c.ProtectedAreas, c.MiningAreas = nil, nil
	return &c
}

// ---- search_building_notices

type searchArgs struct {
	FilterArgs
	Limit       int  `json:"limit"`
	WithContext bool `json:"withContext"`
}

type searchResult struct {
	Count     int       `json:"count"`
	Matched   int       `json:"matched"` // before limit
	Notices   []*Notice `json:"notices"`
	IndexedAt string    `json:"indexedAt"`
	Boards    int       `json:"boards"`
	Note      string    `json:"note,omitempty"`
}

func (s *service) search(ctx context.Context, a searchArgs) (*searchResult, error) {
	idx, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	g := s.h.reg.Gazetteer()
	f, err := a.FilterArgs.compile(g, time.Now())
	if err != nil {
		return nil, err
	}
	limit := a.Limit
	if limit <= 0 || limit > maxSearchLimit {
		limit = 20
	}
	res := &searchResult{Notices: []*Notice{}}
	s.mu.RLock()
	res.IndexedAt, res.Boards = s.builtAt.Format(time.RFC3339), s.stats.Boards
	s.mu.RUnlock()
	var slow []*Notice
	for _, n := range idx {
		if !f.match(n) {
			continue
		}
		if !f.needsParcels() && !a.WithContext {
			res.Matched++
			if len(res.Notices) < limit {
				res.Notices = append(res.Notices, n)
			}
			continue
		}
		slow = append(slow, n)
	}
	if len(slow) > 0 {
		if err := s.resolveBatch(ctx, slow, f, limit, a.WithContext || f.protected, res); err != nil {
			return nil, err
		}
	}
	res.Count = len(res.Notices)
	if f.needsParcels() {
		res.Note = firstNonEmpty(res.Note, "bbox a inProtectedArea fungují jen u vyhlášek, z nichž šlo vyčíst k.ú. a čísla parcel.")
	}
	return res, nil
}

// resolveBatch resolves candidates newest first, 8 at a time, until limit
// matches, the lookup budget or ~25 s are used up.
func (s *service) resolveBatch(ctx context.Context, cands []*Notice, f *filter, limit int, withContext bool, res *searchResult) error {
	deadline := time.Now().Add(25 * time.Second)
	budget := searchLookupCap
	pts := 0
	if withContext {
		pts = 1
	}
	for i := 0; i < len(cands) && len(res.Notices) < limit; i += 8 {
		if budget <= 0 || time.Now().After(deadline) {
			res.Note = fmt.Sprintf("Prohledáno %d z %d kandidátů (limit času nebo dohledání v RÚIAN); zužte region, typ nebo období.", i, len(cands))
			return nil
		}
		chunk := cands[i:min(i+8, len(cands))]
		out := make([]*Notice, len(chunk))
		used := make([]int, len(chunk))
		errs := make([]error, len(chunk))
		var wg sync.WaitGroup
		for j, n := range chunk {
			wg.Add(1)
			go func(j int, n *Notice) {
				defer wg.Done()
				c := copyNotice(n)
				used[j], errs[j] = s.h.resolve(ctx, c, min(noticeLookupCap, max(budget/len(chunk), 1)), pts)
				out[j] = c
			}(j, n)
		}
		wg.Wait()
		for j, c := range out {
			budget -= used[j]
			if errs[j] != nil {
				return errs[j]
			}
			if f.postMatch(c) && len(res.Notices) < limit {
				res.Matched++
				res.Notices = append(res.Notices, c)
			}
		}
	}
	return nil
}

// ---- watch_area

type watchArgs struct {
	FilterArgs
	Cursor string `json:"cursor"` // RFC3339 from the previous call
	Limit  int    `json:"limit"`
}

type watchResult struct {
	Cursor  string    `json:"cursor"`
	New     int       `json:"new"`
	Notices []*Notice `json:"notices"`
	Note    string    `json:"note,omitempty"`
}

func (s *service) watch(ctx context.Context, a watchArgs) (*watchResult, error) {
	idx, err := s.snapshot()
	if err != nil {
		return nil, err
	}
	f, err := a.FilterArgs.compile(s.h.reg.Gazetteer(), time.Now())
	if err != nil {
		return nil, err
	}
	limit := a.Limit
	if limit <= 0 || limit > maxSearchLimit {
		limit = 50
	}
	var since time.Time
	res := &watchResult{Notices: []*Notice{}}
	if a.Cursor != "" {
		if since, err = time.Parse(time.RFC3339, a.Cursor); err != nil {
			return nil, badInput("cursor must be the RFC3339 value returned by the previous call")
		}
	} else {
		res.Note = fmt.Sprintf("První volání: vráceny vyhlášky vyvěšené za posledních %d dní. Příště pošlete cursor.", defaultWatchDays)
		f.since = time.Now().AddDate(0, 0, -defaultWatchDays)
	}
	var hits []*Notice
	for _, n := range idx {
		if !n.firstSeen.After(since) || !f.match(n) {
			continue
		}
		hits = append(hits, n)
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].firstSeen.Before(hits[j].firstSeen) })
	cursor := since
	for _, n := range hits {
		if len(res.Notices) >= limit {
			res.Note = "Více nových vyhlášek než limit; zavolejte znovu s vráceným cursorem."
			break
		}
		res.Notices = append(res.Notices, n)
		cursor = n.firstSeen
	}
	if len(res.Notices) < limit {
		s.mu.RLock()
		cursor = s.builtAt
		s.mu.RUnlock()
	}
	res.New, res.Cursor = len(res.Notices), cursor.UTC().Format(time.RFC3339Nano)
	return res, nil
}

// ---- get_notice / notice_parcels

type noticeArgs struct {
	ID          string `json:"id"`
	WithContext bool   `json:"withContext"`
}

func (s *service) notice(ctx context.Context, a noticeArgs) (*Notice, error) {
	if _, err := s.snapshot(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	n := s.byID[a.ID]
	s.mu.RUnlock()
	if n == nil {
		return nil, badInput("notice %q not in the current index (notices drop out when the board removes them)", a.ID)
	}
	c := copyNotice(n)
	pts := 0
	if a.WithContext {
		pts = 5
	}
	if _, err := s.h.resolve(ctx, c, noticeLookupCap, pts); err != nil {
		return nil, err
	}
	return c, nil
}

type parcelsResult struct {
	NoticeID string         `json:"noticeId"`
	Parcels  []ruian.Parcel `json:"parcels"`
	Missing  []ParcelOut    `json:"unresolved"`
}

func (s *service) noticeParcels(ctx context.Context, a noticeArgs) (*parcelsResult, error) {
	n, err := s.notice(ctx, noticeArgs{ID: a.ID})
	if err != nil {
		return nil, err
	}
	res := &parcelsResult{NoticeID: n.ID, Parcels: []ruian.Parcel{}, Missing: []ParcelOut{}}
	for _, p := range n.Parcels {
		if !p.Resolved {
			res.Missing = append(res.Missing, p)
			continue
		}
		full, err := s.h.reg.ParcelByID(ctx, p.ID, ruian.ParcelOptions{Buildings: true, Context: a.WithContext})
		if err != nil {
			return nil, err
		}
		if full != nil {
			res.Parcels = append(res.Parcels, *full)
		}
	}
	return res, nil
}

// ---- list_offices

type officesArgs struct {
	Kraj string `json:"kraj"`
	ORP  string `json:"orp"`
}

type officeRow struct {
	Board   string    `json:"board"`
	URL     string    `json:"url"`
	Office  OfficeOut `json:"office"`
	Items   int       `json:"items"`
	Notices int       `json:"buildingNotices"`
	Fetched string    `json:"fetched"`
	Error   string    `json:"error,omitempty"`
}

func (s *service) offices(ctx context.Context, a officesArgs) ([]officeRow, error) {
	if _, err := s.snapshot(); err != nil {
		return nil, err
	}
	g := s.h.reg.Gazetteer()
	f, err := FilterArgs{Kraj: a.Kraj, ORP: a.ORP}.compile(g, time.Now())
	if err != nil {
		return nil, err
	}
	s.h.mu.Lock()
	defer s.h.mu.Unlock()
	rows := []officeRow{}
	for u, bs := range s.h.st.Boards {
		off := s.h.off.Lookup(ctx, bs.ICO)
		o := OfficeOut{Name: firstNonEmpty(off.Name, bs.Title), ICO: bs.ICO}
		var t ruian.Territory
		if off.ObecCode > 0 {
			t, _ = g.ObecTerritory(off.ObecCode)
			o.Obec, o.ORP, o.Okres, o.Kraj = t.Obec.Name, t.ORP.Name, t.Okres.Name, t.Kraj.Name
		}
		if (f.kraj != 0 && t.Kraj.Code != f.kraj) || (f.orp != 0 && t.ORP.Code != f.orp) {
			continue
		}
		rows = append(rows, officeRow{Board: bs.Title, URL: u, Office: o, Items: bs.Items, Notices: bs.Notices,
			Fetched: bs.Fetched.Format(time.RFC3339), Error: bs.Err})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Notices > rows[j].Notices })
	return rows, nil
}

// typesList documents the classification for agents.
func typesList() []map[string]string {
	out := []map[string]string{}
	for _, t := range classify.All {
		out = append(out, map[string]string{"type": string(t), "label": classify.Labels[t]})
	}
	return out
}
