package ruian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Unit is a territorial unit reduced to what an agent needs.
type Unit struct {
	Code int    `json:"code"`
	Name string `json:"name"`
}

// Territory is the administrative chain of one cadastral area (k.ú.).
// POU is the municipality with an authorised office, ORP the one with
// extended powers; general building offices sit at one of those two.
type Territory struct {
	KU    Unit `json:"ku"`
	Obec  Unit `json:"obec"`
	POU   Unit `json:"pou"`
	ORP   Unit `json:"orp"`
	Okres Unit `json:"okres"`
	Kraj  Unit `json:"kraj"`
}

// KUMatch is a cadastral-area candidate for a name found in text.
type KUMatch struct {
	Territory
	Score float64 `json:"score"` // 1 exact, 0.95 same stems (declension), less per edit, ≤ 0.6 name is a prefix ("Lhota" → "Lhota u Kolína")
}

type kuRec struct {
	code, obec int
	name, fold string
	stems      []string
}

type obecRec struct {
	name               string
	okres, pou, status int
}

// Gazetteer holds all k.ú., municipalities and higher units in memory
// (about 20 000 rows, < 5 MB). Load it once and refresh daily.
type Gazetteer struct {
	ku         []kuRec
	kuByCode   map[int]int
	obec       map[int]obecRec
	obecByName map[string][]int // folded name → codes (names repeat: 15× Lhota)
	pou        map[int]Unit     // + orp code in pouORP
	pouORP     map[int]int
	orp        map[int]Unit
	okres      map[int]Unit
	okresKr    map[int]int
	kraj       map[int]Unit
	Loaded     time.Time
}

func newGazetteer() *Gazetteer {
	return &Gazetteer{kuByCode: map[int]int{}, obec: map[int]obecRec{}, pou: map[int]Unit{}, pouORP: map[int]int{},
		orp: map[int]Unit{}, okres: map[int]Unit{}, okresKr: map[int]int{}, kraj: map[int]Unit{}}
}

func (g *Gazetteer) addKU(code, obec int, name string) {
	g.ku = append(g.ku, kuRec{code: code, obec: obec, name: name, fold: Fold(name), stems: stems(name)})
}

// LoadGazetteer pulls the unit tables from the ArcGIS service (6 requests,
// about 2 s together).
func (c *Client) LoadGazetteer(ctx context.Context) (*Gazetteer, error) {
	g := newGazetteer()
	type job struct {
		layer  int
		fields string
		fn     func(map[string]any)
	}
	var mu sync.Mutex
	jobs := []job{
		{LayerKU, "kod,nazev,obec", func(a map[string]any) {
			g.addKU(num(a["kod"]), num(a["obec"]), str(a["nazev"]))
		}},
		{LayerObec, "kod,nazev,okres,pou,statuskod", func(a map[string]any) {
			g.obec[num(a["kod"])] = obecRec{name: str(a["nazev"]), okres: num(a["okres"]), pou: num(a["pou"]), status: num(a["statuskod"])}
		}},
		{LayerPOU, "kod,nazev,orp", func(a map[string]any) {
			k := num(a["kod"])
			g.pou[k] = Unit{k, str(a["nazev"])}
			g.pouORP[k] = num(a["orp"])
		}},
		{LayerORP, "kod,nazev", func(a map[string]any) { k := num(a["kod"]); g.orp[k] = Unit{k, str(a["nazev"])} }},
		{LayerOkres, "kod,nazev,vusc", func(a map[string]any) {
			k := num(a["kod"])
			g.okres[k] = Unit{k, str(a["nazev"])}
			g.okresKr[k] = num(a["vusc"])
		}},
		{LayerKraj, "kod,nazev", func(a map[string]any) { k := num(a["kod"]); g.kraj[k] = Unit{k, str(a["nazev"])} }},
	}
	errs := make(chan error, len(jobs))
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		go func(j job) {
			defer wg.Done()
			q := url.Values{"where": {"1=1"}, "outFields": {j.fields}, "returnGeometry": {"false"}}
			r, err := c.query(ctx, j.layer, q)
			if err != nil {
				errs <- fmt.Errorf("layer %d: %w", j.layer, err)
				return
			}
			if r.ExceededTransferLimit {
				errs <- fmt.Errorf("layer %d: transfer limit exceeded", j.layer)
				return
			}
			mu.Lock()
			defer mu.Unlock()
			for _, f := range r.Features {
				var a map[string]any
				if json.Unmarshal(f.Attributes, &a) == nil {
					j.fn(a)
				}
			}
		}(j)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return nil, err
	}
	if len(g.ku) < 10000 || len(g.obec) < 6000 {
		return nil, fmt.Errorf("%w: gazetteer incomplete (%d k.ú., %d obcí)", ErrUpstream, len(g.ku), len(g.obec))
	}
	g.index()
	g.Loaded = time.Now()
	return g, nil
}

// index builds the lookup maps after the tables are filled.
func (g *Gazetteer) index() {
	g.obecByName = map[string][]int{}
	for code, o := range g.obec {
		k := strings.Join(words(o.name), " ")
		g.obecByName[k] = append(g.obecByName[k], code)
	}
	sort.Slice(g.ku, func(i, j int) bool { return g.ku[i].code < g.ku[j].code })
	for i, k := range g.ku {
		g.kuByCode[k.code] = i
	}
}

// Size reports row counts (for logs and health checks).
func (g *Gazetteer) Size() map[string]int {
	return map[string]int{"ku": len(g.ku), "obec": len(g.obec), "pou": len(g.pou), "orp": len(g.orp), "okres": len(g.okres), "kraj": len(g.kraj)}
}

// Territory resolves the administrative chain of a k.ú. code.
func (g *Gazetteer) Territory(kuCode int) (Territory, bool) {
	i, ok := g.kuByCode[kuCode]
	if !ok {
		return Territory{}, false
	}
	k := g.ku[i]
	t := Territory{KU: Unit{k.code, k.name}}
	if o, ok := g.obec[k.obec]; ok {
		t.Obec = Unit{k.obec, o.name}
		t.POU = g.pou[o.pou]
		t.ORP = g.orp[g.pouORP[o.pou]]
		t.Okres = g.okres[o.okres]
		t.Kraj = g.kraj[g.okresKr[o.okres]]
	}
	return t, true
}

// FindObec returns municipalities with exactly this name (diacritics and
// case ignored). "Praha 4" and "Brno-střed" fall back to "Praha"/"Brno".
func (g *Gazetteer) FindObec(name string) []Unit {
	ws := words(name)
	for n := len(ws); n >= 1; n-- {
		if codes := g.obecByName[strings.Join(ws[:n], " ")]; len(codes) > 0 {
			out := make([]Unit, 0, len(codes))
			for _, c := range codes {
				out = append(out, Unit{c, g.obec[c].name})
			}
			sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
			return out
		}
	}
	return nil
}

// FindKU matches a k.ú. name as written in Czech text ("Hrutově",
// "k. ú. Staré Město u Uherského Hradiště", "648876"). hint is any other
// place name from the same document (obec, ORP, okres) used to rank
// candidates. k.ú. names are unique nationwide, so an exact match is final.
func (g *Gazetteer) FindKU(name, hint string) []KUMatch {
	name = strings.TrimSpace(name)
	if code, err := strconv.Atoi(name); err == nil {
		if t, ok := g.Territory(code); ok {
			return []KUMatch{{t, 1}}
		}
		return nil
	}
	qf := strings.Join(words(name), " ")
	qs := stems(name)
	if len(qs) == 0 {
		return nil
	}
	hs := stems(hint)
	var out []KUMatch
	for _, k := range g.ku {
		var score float64
		switch {
		case k.fold == qf || strings.Join(words(k.name), " ") == qf:
			score = 1
		case len(k.stems) == len(qs):
			d := totalDist(qs, k.stems)
			if d < 0 {
				continue
			}
			score = 0.95 - 0.05*float64(d)
		case len(k.stems) > len(qs) && isPrepTail(k.stems[len(qs):]):
			d := totalDist(qs, k.stems[:len(qs)])
			if d < 0 {
				continue
			}
			score = 0.6 - 0.05*float64(d)
		default:
			continue
		}
		t, _ := g.Territory(k.code)
		if len(hs) > 0 && score < 1 {
			for _, u := range []Unit{t.Obec, t.POU, t.ORP, t.Okres} {
				if u.Name != "" && containsStems(stems(u.Name), hs) || containsStems(k.stems, hs) {
					score += 0.05
					break
				}
			}
		}
		out = append(out, KUMatch{t, score})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].KU.Name < out[j].KU.Name
	})
	if len(out) > 0 && out[0].Score >= 1 {
		return out[:1]
	}
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

// totalDist sums stemDist over aligned words; -1 if any word differs.
func totalDist(a, b []string) int {
	t := 0
	for i := range a {
		d := stemDist(a[i], b[i])
		if d < 0 {
			return -1
		}
		t += d
	}
	return t
}

// isPrepTail accepts the "u Kolína" / "nad Labem" / "pod Řípem" / "v Čechách"
// distinguishing tail of k.ú. names, so "Lhota" proposes "Lhota u Kolína"
// but "Nová" does not propose "Nová Ves".
func isPrepTail(rest []string) bool {
	if len(rest) == 0 {
		return false
	}
	switch rest[0] {
	case "u", "nad", "pod", "v", "ve", "na", "pri", "za", "mezi", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10":
		return true
	}
	return false
}

// containsStems reports whether every hint stem appears among haystack stems.
func containsStems(haystack, hint []string) bool {
	for _, h := range hint {
		if len(h) <= 3 {
			continue
		}
		found := false
		for _, s := range haystack {
			if stemMatch(h, s) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func num(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
