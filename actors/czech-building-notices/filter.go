package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/czech-building-notices/internal/classify"
)

// FilterArgs is shared by batch input, HTTP and MCP.
type FilterArgs struct {
	Kraj            string    `json:"kraj"`
	ORP             string    `json:"orp"`
	Obec            string    `json:"obec"`
	KU              []string  `json:"ku"`   // names or codes
	BBox            []float64 `json:"bbox"` // [minLon, minLat, maxLon, maxLat]
	Types           []string  `json:"types"`
	Keywords        []string  `json:"keywords"`
	Since           string    `json:"since"` // YYYY-MM-DD or "7d"
	Until           string    `json:"until"`
	IncludeExpired  bool      `json:"includeExpired"`
	InProtectedArea bool      `json:"inProtectedArea"`
}

type filter struct {
	kraj, orp, obec int
	kus             map[int]bool
	bbox            []float64
	types           map[classify.Type]bool
	keywords        []string
	since, until    time.Time
	includeExpired  bool
	protected       bool
	describe        []string
}

// compile resolves names to codes once; unknown names are an input error.
func (a FilterArgs) compile(g *ruian.Gazetteer, now time.Time) (*filter, error) {
	f := &filter{includeExpired: a.IncludeExpired, protected: a.InProtectedArea}
	unit := func(level, name string) (int, error) {
		if name == "" {
			return 0, nil
		}
		u, ok := g.FindUnit(level, name)
		if !ok {
			return 0, badInput("unknown %s %q", level, name)
		}
		f.describe = append(f.describe, level+"="+u.Name)
		return u.Code, nil
	}
	var err error
	if f.kraj, err = unit("kraj", a.Kraj); err != nil {
		return nil, err
	}
	if f.orp, err = unit("orp", a.ORP); err != nil {
		return nil, err
	}
	if a.Obec != "" {
		if code, err := strconv.Atoi(strings.TrimSpace(a.Obec)); err == nil {
			t, ok := g.ObecTerritory(code)
			if !ok {
				return nil, badInput("unknown obec code %d", code)
			}
			f.obec = code
			f.describe = append(f.describe, "obec="+t.Obec.Name)
		} else {
			os := g.FindObec(a.Obec)
			switch {
			case len(os) == 0:
				return nil, badInput("unknown obec %q", a.Obec)
			case len(os) > 1:
				var c []string
				for _, o := range os {
					t, _ := g.ObecTerritory(o.Code)
					c = append(c, fmt.Sprintf("%d (okres %s)", o.Code, t.Okres.Name))
				}
				return nil, badInput("obec %q is ambiguous, pass its code: %s", a.Obec, strings.Join(c, "; "))
			}
			f.obec = os[0].Code
			f.describe = append(f.describe, "obec="+os[0].Name)
		}
	}
	for _, k := range a.KU {
		m := g.FindKU(k, "")
		if len(m) == 0 || !(m[0].Score >= 0.9 && (len(m) == 1 || m[1].Score < m[0].Score)) {
			return nil, badInput("k.ú. %q not found or ambiguous", k)
		}
		if f.kus == nil {
			f.kus = map[int]bool{}
		}
		f.kus[m[0].KU.Code] = true
		f.describe = append(f.describe, "ku="+m[0].KU.Name)
	}
	if len(a.BBox) > 0 {
		if len(a.BBox) != 4 || a.BBox[0] >= a.BBox[2] || a.BBox[1] >= a.BBox[3] {
			return nil, badInput("bbox must be [minLon, minLat, maxLon, maxLat]")
		}
		f.bbox = a.BBox
	}
	for _, t := range a.Types {
		tt := classify.Type(strings.TrimSpace(t))
		if _, ok := classify.Labels[tt]; !ok {
			return nil, badInput("unknown type %q (one of %v)", t, classify.All)
		}
		if f.types == nil {
			f.types = map[classify.Type]bool{}
		}
		f.types[tt] = true
	}
	for _, k := range a.Keywords {
		if k = strings.TrimSpace(ruian.Fold(k)); k != "" {
			f.keywords = append(f.keywords, k)
		}
	}
	if f.since, err = parseWhen(a.Since, now); err != nil {
		return nil, err
	}
	if f.until, err = parseWhen(a.Until, now); err != nil {
		return nil, err
	}
	if !f.until.IsZero() {
		f.until = f.until.Add(24*time.Hour - time.Nanosecond)
	}
	return f, nil
}

// parseWhen accepts "2026-10-01" or a relative "7d"/"2w".
func parseWhen(s string, now time.Time) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	if n, err := strconv.Atoi(strings.TrimRight(s, "dw")); err == nil && (strings.HasSuffix(s, "d") || strings.HasSuffix(s, "w")) {
		if strings.HasSuffix(s, "w") {
			n *= 7
		}
		y, m, d := now.AddDate(0, 0, -n).Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()), nil
	}
	t, err := time.ParseInLocation(time.DateOnly, s, now.Location())
	if err != nil {
		return time.Time{}, badInput("date %q: use YYYY-MM-DD or e.g. 7d", s)
	}
	return t, nil
}

// match applies the cheap filters; bbox and inProtectedArea need RÚIAN
// lookups and are checked by postMatch.
func (f *filter) match(n *Notice) bool {
	if n.Expired && !f.includeExpired {
		return false
	}
	if f.types != nil && !f.types[n.Type] {
		return false
	}
	if !f.since.IsZero() && n.posted.Before(f.since) {
		return false
	}
	if !f.until.IsZero() && n.posted.After(f.until) {
		return false
	}
	if len(f.keywords) > 0 {
		t := ruian.Fold(n.Title + " " + n.Description)
		ok := false
		for _, k := range f.keywords {
			if strings.Contains(t, k) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if f.kraj != 0 || f.orp != 0 || f.obec != 0 || f.kus != nil {
		ok := false
		for _, t := range n.territories {
			if (f.kraj == 0 || t.Kraj.Code == f.kraj) && (f.orp == 0 || t.ORP.Code == f.orp) &&
				(f.obec == 0 || t.Obec.Code == f.obec) && (f.kus == nil || f.kus[t.KU.Code]) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// postMatch runs after parcels were resolved (and context fetched).
func (f *filter) postMatch(n *Notice) bool {
	if f.bbox != nil && !f.inBBox(n) {
		return false
	}
	if f.protected && len(n.ProtectedAreas) == 0 {
		return false
	}
	return true
}

// inBBox needs resolved parcels; notices without coordinates never match.
func (f *filter) inBBox(n *Notice) bool {
	for _, p := range n.Parcels {
		if c := p.Centroid; c != nil && c.Lon >= f.bbox[0] && c.Lat >= f.bbox[1] && c.Lon <= f.bbox[2] && c.Lat <= f.bbox[3] {
			return true
		}
	}
	return false
}

func (f *filter) needsParcels() bool { return f.bbox != nil || f.protected }
