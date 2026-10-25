package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/czech-building-notices/internal/actor"
	"github.com/lioilsources/czech-building-notices/internal/aia"
	"github.com/lioilsources/czech-building-notices/internal/desky"
	"github.com/lioilsources/czech-building-notices/internal/offices"
)

const stateStoreName = "czech-building-notices-state"

// boardState is what we remember about a board between harvests.
type boardState struct {
	Title     string    `json:"title"`
	ICO       string    `json:"ico"`
	ETag      string    `json:"etag,omitempty"`
	LastMod   string    `json:"lastMod,omitempty"`
	Fetched   time.Time `json:"fetched"`
	Bytes     int       `json:"bytes"`
	Items     int       `json:"items"`
	Notices   int       `json:"notices"`
	Err       string    `json:"err,omitempty"`
	Unchanged bool      `json:"unchanged,omitempty"`
}

type state struct {
	Boards map[string]*boardState `json:"boards"`
	Seen   map[string]time.Time   `json:"seen"` // notice id → first seen
}

type harvester struct {
	hc  *http.Client
	ua  string
	reg *ruian.Registry
	off *offices.Resolver
	act *actor.Actor

	mu      sync.Mutex
	st      state
	byBoard map[string][]*Notice // last parsed notices per board (Standby keeps them for 304s)
}

func newHarvester(a *actor.Actor, reg *ruian.Registry, ua string) *harvester {
	hc := aia.Client(60 * time.Second)
	return &harvester{hc: hc, ua: ua, reg: reg, off: offices.New(hc, ua), act: a,
		st: state{Boards: map[string]*boardState{}, Seen: map[string]time.Time{}}, byBoard: map[string][]*Notice{}}
}

// loadState restores board ETags, seen ids and the ARES cache from the
// named key-value store (batch runs on a schedule).
func stateFile(key string) string {
	key = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '-'
	}, key)
	if key == "" {
		return "state.json"
	}
	return "state-" + key + ".json"
}

// unsee forgets a notice so the next onlyNew run reports it again.
func (h *harvester) unsee(id string) {
	h.mu.Lock()
	delete(h.st.Seen, id)
	h.mu.Unlock()
}

func (h *harvester) loadState(ctx context.Context, key string) (string, error) {
	store, err := h.act.OpenStore(ctx, stateStoreName)
	if err != nil {
		return "", err
	}
	if b, ok, err := h.act.GetRecordFrom(ctx, store, stateFile(key)); err != nil {
		return store, err
	} else if ok {
		var st state
		if json.Unmarshal(b, &st) == nil && st.Boards != nil {
			if st.Seen == nil {
				st.Seen = map[string]time.Time{}
			}
			h.st = st
		}
	}
	if b, ok, _ := h.act.GetRecordFrom(ctx, store, "offices.json"); ok {
		h.off.Restore(b)
	}
	return store, nil
}

func (h *harvester) saveState(ctx context.Context, store, key string) error {
	h.mu.Lock()
	cut := time.Now().Add(-180 * 24 * time.Hour)
	for id, t := range h.st.Seen {
		if t.Before(cut) {
			delete(h.st.Seen, id)
		}
	}
	b, err := json.Marshal(h.st)
	h.mu.Unlock()
	if err != nil {
		return err
	}
	if err := h.act.SetRecordIn(ctx, store, stateFile(key), "application/json", b); err != nil {
		return err
	}
	return h.act.SetRecordIn(ctx, store, "offices.json", "application/json", h.off.Snapshot())
}

type harvestOpts struct {
	conditional bool // send If-None-Match; unchanged boards reuse byBoard (Standby) or yield nothing (onlyNew)
	maxBoards   int
}

type harvestStats struct {
	Boards, Fetched, Unchanged, Failed, Items, Notices, Duplicates int
	Bytes                                                          int64
	Took                                                           time.Duration
}

// harvest downloads every board and returns building/land notices, newest first.
func (h *harvester) harvest(ctx context.Context, o harvestOpts) ([]*Notice, harvestStats, error) {
	t0 := time.Now()
	var st harvestStats
	boards, err := desky.ListBoards(ctx, h.hc, h.ua, false)
	if err != nil {
		return nil, st, err
	}
	if o.maxBoards > 0 && len(boards) > o.maxBoards {
		boards = boards[:o.maxBoards]
	}
	st.Boards = len(boards)
	g := h.reg.Gazetteer()
	now := time.Now()

	hostSem := map[string]chan struct{}{}
	for _, b := range boards {
		if u, err := url.Parse(b.URL); err == nil {
			if _, ok := hostSem[u.Host]; !ok {
				hostSem[u.Host] = make(chan struct{}, 2) // ≤ 2 parallel requests per server
			}
		}
	}
	jobs := make(chan desky.Board)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var all []*Notice
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for b := range jobs {
				ns, bs := h.harvestBoard(ctx, b, hostSem, o, g, now)
				mu.Lock()
				all = append(all, ns...)
				switch {
				case bs.Err != "":
					st.Failed++
				case bs.Unchanged:
					st.Unchanged++
				default:
					st.Fetched++
				}
				st.Items += bs.Items
				st.Bytes += int64(bs.Bytes)
				mu.Unlock()
			}
		}()
	}
	for _, b := range boards {
		select {
		case jobs <- b:
		case <-ctx.Done():
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return nil, st, ctx.Err()
	}

	// cross-board duplicates: keep the earliest-posted copy
	sort.SliceStable(all, func(i, j int) bool { return all[i].posted.Before(all[j].posted) })
	seenKey := map[string]bool{}
	out := all[:0]
	h.mu.Lock()
	for _, n := range all {
		k := dedupeKey(n)
		if seenKey[k] {
			st.Duplicates++
			continue
		}
		seenKey[k] = true
		if fs, ok := h.st.Seen[n.ID]; ok {
			n.firstSeen = fs
		} else {
			n.firstSeen = now.UTC()
			h.st.Seen[n.ID] = n.firstSeen
		}
		n.FirstSeenAt = n.firstSeen.Format(time.RFC3339)
		out = append(out, n)
	}
	h.mu.Unlock()
	sort.SliceStable(out, func(i, j int) bool { return out[i].posted.After(out[j].posted) })
	st.Notices = len(out)
	st.Took = time.Since(t0)
	return out, st, nil
}

func (h *harvester) harvestBoard(ctx context.Context, b desky.Board, hostSem map[string]chan struct{}, o harvestOpts, g *ruian.Gazetteer, now time.Time) ([]*Notice, boardState) {
	h.mu.Lock()
	prev := h.st.Boards[b.URL]
	h.mu.Unlock()
	bs := boardState{Title: b.Title, ICO: b.PublisherICO, Fetched: now.UTC()}
	etag, lastMod := "", ""
	if o.conditional && prev != nil && prev.Err == "" {
		etag, lastMod = prev.ETag, prev.LastMod
	}
	if u, err := url.Parse(b.URL); err == nil {
		sem := hostSem[u.Host]
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
		case <-ctx.Done():
			bs.Err = ctx.Err().Error()
			return nil, bs
		}
	}
	body, newETag, newLM, notMod, err := desky.Fetch(ctx, h.hc, h.ua, b.URL, etag, lastMod)
	var ns []*Notice
	switch {
	case err != nil:
		bs.Err = err.Error()
	case notMod:
		bs = *prev
		bs.Fetched, bs.Unchanged = now.UTC(), true
		h.mu.Lock()
		ns = h.byBoard[b.URL] // Standby: keep serving; batch onlyNew: they are not new anyway
		h.mu.Unlock()
		if !o.conditional {
			ns = nil
		}
	default:
		bs.ETag, bs.LastMod, bs.Bytes = newETag, newLM, len(body)
		items, meta, perr := desky.Parse(body)
		if perr != nil {
			bs.Err = perr.Error()
			break
		}
		bs.Items = len(items)
		ico := b.PublisherICO
		if ico == "" {
			ico = meta.OperatorICO
		}
		bc := newBoardCtx(g, h.off.Lookup(ctx, ico))
		for _, in := range items {
			if n := buildNotice(b, in, bc, g, now); n != nil {
				ns = append(ns, n)
			}
		}
		bs.Notices = len(ns)
		h.mu.Lock()
		h.byBoard[b.URL] = ns
		h.mu.Unlock()
	}
	if bs.Err != "" {
		log.Printf("board %s: %s", b.URL, bs.Err)
	}
	h.mu.Lock()
	h.st.Boards[b.URL] = &bs
	h.mu.Unlock()
	return ns, bs
}

// resolve looks the notice's parcel references up in RÚIAN (budget = max
// lookups) and, with points > 0, collects protected and mining areas at up
// to that many parcel centroids. Returns the lookups used.
func (h *harvester) resolve(ctx context.Context, n *Notice, budget int, points int) (int, error) {
	if budget <= 0 || len(n.refs) == 0 {
		return 0, nil
	}
	rr, err := h.reg.ResolveRefs(ctx, n.refs, ruian.ParcelOptions{}, budget)
	used := 0
	for i, r := range rr {
		if r.Ref.KU != nil {
			used++
		}
		if len(r.Parcels) == 0 || i >= len(n.Parcels) {
			continue
		}
		p := r.Parcels[0]
		po := &n.Parcels[i]
		po.Resolved, po.ID, po.AreaM2, po.LandType, po.Centroid, po.VDP, po.Number = true, p.ID, p.AreaM2, p.LandType, p.Centroid, p.Links.VDP, p.Number
		po.Note = r.Note
		if len(r.Parcels) > 1 {
			po.Note = firstNonEmpty(po.Note, "Číslo odpovídá stavební i pozemkové parcele; uvedena stavební.")
		}
		if n.TerritorySource != "parcel" {
			n.Territory, n.TerritorySource = &p.Territory, "parcel"
		}
	}
	if err != nil {
		return used, err
	}
	if points > 0 {
		seenArea := map[int64]bool{}
		checked := 0
		for _, p := range n.Parcels {
			if p.Centroid == nil || checked >= points {
				continue
			}
			checked++
			c, err := h.reg.ContextAt(ctx, *p.Centroid)
			if err != nil {
				return used, err
			}
			for _, a := range c.ProtectedAreas {
				if !seenArea[a.Kod] {
					seenArea[a.Kod] = true
					n.ProtectedAreas = append(n.ProtectedAreas, a)
				}
			}
			for _, a := range c.MiningAreas {
				if !seenArea[a.Kod] {
					seenArea[a.Kod] = true
					n.MiningAreas = append(n.MiningAreas, a)
				}
			}
		}
		if n.ProtectedAreas == nil {
			n.ProtectedAreas = []ruian.Area{}
		}
	}
	return used, nil
}
