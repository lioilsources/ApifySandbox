package ruian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// Registry is the high-level API: a Client plus the in-memory gazetteer.
type Registry struct {
	C *Client
	g atomic.Pointer[Gazetteer]
}

func NewRegistry(c *Client) *Registry { return &Registry{C: c} }

// Refresh (re)loads the gazetteer; call at start and then daily.
func (r *Registry) Refresh(ctx context.Context) error {
	g, err := r.C.LoadGazetteer(ctx)
	if err != nil {
		return err
	}
	r.g.Store(g)
	return nil
}

// Gazetteer returns the loaded gazetteer or nil before the first Refresh.
func (r *Registry) Gazetteer() *Gazetteer { return r.g.Load() }

func (r *Registry) territory(kuCode int) Territory {
	if g := r.g.Load(); g != nil {
		if t, ok := g.Territory(kuCode); ok {
			return t
		}
	}
	return Territory{KU: Unit{Code: kuCode}}
}

// ParcelNumber is a parsed parcel number: "st. 56", "123/4", "1234".
type ParcelNumber struct {
	Kmen     int   // main number
	Pod      *int  // subdivision after "/", nil = none
	Building *bool // true = "st." (stavební parcela), false = pozemková, nil = not stated
}

var parcelNumRe = regexp.MustCompile(`(?i)^\s*(?:(?:parc(?:ela)?\.?\s*|p\.\s*)?(?:č\.|čís(?:lo)?\.?)\s*)?(st\.?\s*(?:p\.?\s*)?(?:č\.\s*)?|poz\.?\s*)?(\d{1,5})(?:\s*/\s*(\d{1,4}))?\s*$`)

// ParseParcelNumber accepts the usual written forms of a parcel number.
func ParseParcelNumber(s string) (ParcelNumber, error) {
	m := parcelNumRe.FindStringSubmatch(s)
	if m == nil {
		return ParcelNumber{}, fmt.Errorf("unrecognised parcel number %q (expected e.g. \"123/4\" or \"st. 56\")", s)
	}
	var p ParcelNumber
	p.Kmen, _ = strconv.Atoi(m[2])
	if m[3] != "" {
		n, _ := strconv.Atoi(m[3])
		p.Pod = &n
	}
	if pre := strings.ToLower(strings.TrimSpace(m[1])); pre != "" {
		b := strings.HasPrefix(pre, "st")
		p.Building = &b
	}
	return p, nil
}

func (p ParcelNumber) String() string {
	s := strconv.Itoa(p.Kmen)
	if p.Pod != nil {
		s += "/" + strconv.Itoa(*p.Pod)
	}
	if p.Building != nil && *p.Building {
		s = "st. " + s
	}
	return s
}

// Parcel is one RÚIAN parcel. RÚIAN holds no owners.
type Parcel struct {
	ID           int64       `json:"id"`
	Number       string      `json:"number"`   // "st. 1" or "123/4"
	Building     bool        `json:"building"` // stavební parcela
	Territory    Territory   `json:"territory"`
	AreaM2       float64     `json:"areaM2"`
	LandType     string      `json:"landType"`
	LandTypeCode int         `json:"landTypeCode"`
	Use          *string     `json:"use"`
	UseCode      *int        `json:"useCode,omitempty"`
	Centroid     *LatLon     `json:"centroid"`
	Geometry     *Polygon    `json:"geometry,omitempty"`
	ValidFrom    string      `json:"validFrom,omitempty"`
	Buildings    []Building  `json:"buildings,omitempty"`
	Context      *Context    `json:"context,omitempty"`
	Links        ParcelLinks `json:"links"`
}

type ParcelLinks struct {
	VDP             string `json:"vdp"`
	Nahlizeni       string `json:"nahlizeni"`
	OwnerLookupHint string `json:"ownerLookupHint"`
}

type parcelAttrs struct {
	ID      float64  `json:"id"`
	Kmen    float64  `json:"kmenovecislo"`
	Pod     *float64 `json:"poddelenicisla"`
	Cislo   string   `json:"cisloparcely"`
	Vymera  float64  `json:"vymeraparcely"`
	Druh    *float64 `json:"druhpozemkukod"`
	Vyuziti *float64 `json:"zpusobyvyuzitipozemku"`
	Cislov  *float64 `json:"druhcislovanikod"`
	KU      float64  `json:"katastralniuzemi"`
	PlatiOd *float64 `json:"platiod"`
}

const parcelFields = "id,kmenovecislo,poddelenicisla,cisloparcely,vymeraparcely,druhpozemkukod,zpusobyvyuzitipozemku,druhcislovanikod,katastralniuzemi,platiod"

// ParcelOptions controls how much is fetched per parcel.
type ParcelOptions struct {
	Geometry  bool // simplified GeoJSON polygon (adds 1–20 kB per parcel)
	Buildings bool // building(s) standing on a stavební parcela
	Context   bool // protected areas etc. at the centroid
}

func (r *Registry) parcelFrom(f feature, opt ParcelOptions) Parcel {
	var a parcelAttrs
	_ = json.Unmarshal(f.Attributes, &a)
	g := parseGeom(f.Geometry)
	p := Parcel{
		ID:        int64(a.ID),
		Number:    a.Cislo,
		Building:  a.Cislov != nil && int(*a.Cislov) == 1,
		Territory: r.territory(int(a.KU)),
		AreaM2:    a.Vymera,
		Centroid:  g.point(),
	}
	if p.Building {
		p.Number = "st. " + p.Number
	}
	if a.Druh != nil {
		p.LandTypeCode = int(*a.Druh)
		p.LandType = Lookup(CBDruhPozemku, p.LandTypeCode)
	}
	if a.Vyuziti != nil {
		c := int(*a.Vyuziti)
		p.UseCode = &c
		p.Use = lookupPtr(CBZpusobVyuzitiPozemku, &c)
	}
	if a.PlatiOd != nil {
		p.ValidFrom = time.UnixMilli(int64(*a.PlatiOd)).UTC().Format(time.DateOnly)
	}
	if opt.Geometry {
		p.Geometry = g.polygon(0.5)
	}
	p.Links = ParcelLinks{
		VDP:       fmt.Sprintf("https://vdp.cuzk.gov.cz/vdp/ruian/parcely/%d", p.ID),
		Nahlizeni: "https://nahlizenidokn.cuzk.gov.cz/",
		OwnerLookupHint: fmt.Sprintf("Vlastníka RÚIAN neobsahuje. Ručně: Nahlížení do KN → Vyhledat parcelu → k.ú. %s (%d), %s parcela %s. Údaje o vlastníkovi lze použít jen v souladu s § 53 katastrálního zákona.",
			p.Territory.KU.Name, p.Territory.KU.Code, map[bool]string{true: "stavební", false: "pozemková"}[p.Building], strings.TrimPrefix(p.Number, "st. ")),
	}
	return p
}

// Parcels finds parcels by k.ú. code and number. Without "st." both the
// stavební and the pozemková parcel with that number may come back.
func (r *Registry) Parcels(ctx context.Context, kuCode int, n ParcelNumber, opt ParcelOptions) ([]Parcel, error) {
	where := fmt.Sprintf("katastralniuzemi=%d AND kmenovecislo=%d", kuCode, n.Kmen)
	if n.Pod != nil {
		where += fmt.Sprintf(" AND poddelenicisla=%d", *n.Pod)
	} else {
		where += " AND poddelenicisla IS NULL"
	}
	if n.Building != nil {
		k := 2
		if *n.Building {
			k = 1
		}
		where += fmt.Sprintf(" AND druhcislovanikod=%d", k)
	}
	return r.parcelQuery(ctx, where, 20, opt)
}

// ParcelByID fetches one parcel by its RÚIAN id.
func (r *Registry) ParcelByID(ctx context.Context, id int64, opt ParcelOptions) (*Parcel, error) {
	ps, err := r.parcelQuery(ctx, fmt.Sprintf("id=%d", id), 1, opt)
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	return &ps[0], nil
}

func (r *Registry) parcelQuery(ctx context.Context, where string, limit int, opt ParcelOptions) ([]Parcel, error) {
	q := url.Values{"where": {where}, "outFields": {parcelFields}, "returnGeometry": {"true"},
		"geometryPrecision": {"7"}, "resultRecordCount": {strconv.Itoa(limit)}}
	res, err := r.C.query(ctx, LayerParcel, q)
	if err != nil {
		return nil, err
	}
	out := make([]Parcel, 0, len(res.Features))
	for _, f := range res.Features {
		out = append(out, r.parcelFrom(f, opt))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Building && !out[j].Building })
	if err := r.enrich(ctx, out, opt); err != nil {
		return out, err
	}
	return out, nil
}

func (r *Registry) enrich(ctx context.Context, ps []Parcel, opt ParcelOptions) error {
	for i := range ps {
		if opt.Buildings {
			bs, err := r.BuildingsOnParcel(ctx, ps[i].ID)
			if err != nil {
				return err
			}
			ps[i].Buildings = bs
		}
		if opt.Context && ps[i].Centroid != nil {
			c, err := r.ContextAt(ctx, *ps[i].Centroid)
			if err != nil {
				return err
			}
			c.Territory = nil // already on the parcel
			ps[i].Context = c
		}
	}
	return nil
}
