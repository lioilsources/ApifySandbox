package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/ruian-parcel-mcp/internal/actor"
)

// Per-request limits for Standby/MCP. Batch runs use Input.Limit instead.
const (
	maxTextBytes     = 50_000
	maxLookups       = 50
	maxAddressResult = 10
)

// errBadInput marks caller mistakes (HTTP 400 / MCP isError).
var errBadInput = errors.New("bad input")

func badInput(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errBadInput, fmt.Sprintf(format, a...))
}

type service struct {
	reg *ruian.Registry
	act *actor.Actor
}

func (s *service) gaz() (*ruian.Gazetteer, error) {
	g := s.reg.Gazetteer()
	if g == nil {
		return nil, fmt.Errorf("%w: gazetteer not loaded yet, retry in a minute", ruian.ErrUpstream)
	}
	return g, nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

// ---- resolve_parcel

type parcelArgs struct {
	KU            string `json:"ku"`     // name as written or 6-digit code
	KUCode        int    `json:"kuCode"` // alternative to ku
	Number        string `json:"number"` // "123/4", "st. 56"
	WithGeometry  bool   `json:"withGeometry"`
	WithBuildings *bool  `json:"withBuildings"` // default true
	WithContext   bool   `json:"withContext"`
}

type parcelResult struct {
	Query        string          `json:"query"`
	KU           *ruian.Unit     `json:"ku,omitempty"`
	KUCandidates []ruian.KUMatch `json:"kuCandidates,omitempty"`
	Parcels      []ruian.Parcel  `json:"parcels"`
	Note         string          `json:"note,omitempty"`
}

func (s *service) resolveParcel(ctx context.Context, a parcelArgs) (*parcelResult, error) {
	g, err := s.gaz()
	if err != nil {
		return nil, err
	}
	res := &parcelResult{Query: strings.TrimSpace(a.KU + " " + a.Number), Parcels: []ruian.Parcel{}}
	n, err := ruian.ParseParcelNumber(a.Number)
	if err != nil {
		return nil, badInput("%v", err)
	}
	code := a.KUCode
	if code == 0 {
		if a.KU == "" {
			return nil, badInput("ku (name or code) or kuCode is required")
		}
		m := g.FindKU(a.KU, "")
		switch {
		case len(m) == 0:
			res.Note = fmt.Sprintf("Katastrální území %q nenalezeno. Zkontrolujte název (k.ú., ne obec) nebo použijte šestimístný kód.", a.KU)
			return res, nil
		case m[0].Score >= 0.9 && (len(m) == 1 || m[1].Score < m[0].Score):
			code = m[0].KU.Code
		default:
			res.KUCandidates = m
			res.Note = "Název k.ú. není jednoznačný. Vyberte kód z kuCandidates a zavolejte znovu s kuCode."
			return res, nil
		}
	}
	t, ok := g.Territory(code)
	if !ok {
		return nil, badInput("unknown k.ú. code %d", code)
	}
	res.KU = &t.KU
	opt := ruian.ParcelOptions{Geometry: a.WithGeometry, Buildings: boolOr(a.WithBuildings, true), Context: a.WithContext}
	ps, err := s.reg.Parcels(ctx, code, n, opt)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 && n.Building != nil && !*n.Building {
		n.Building = nil
		if ps, err = s.reg.Parcels(ctx, code, n, opt); err != nil {
			return nil, err
		}
	}
	res.Parcels = ps
	switch {
	case len(ps) == 0:
		res.Note = fmt.Sprintf("Parcela %s v k.ú. %s v RÚIAN není. Ověřte číslo, případně zda nejde o parcelu zjednodušené evidence (PK/EN), kterou RÚIAN nevede.", n, t.KU.Name)
	case len(ps) > 1:
		res.Note = "Číslo bez „st.“ odpovídá stavební i pozemkové parcele; obě jsou ve výsledku."
	}
	return res, nil
}

// ---- extract_parcels

type extractArgs struct {
	Text          string `json:"text"`
	KUHint        string `json:"kuHint"`
	Resolve       *bool  `json:"resolve"` // default true
	WithBuildings bool   `json:"withBuildings"`
	WithContext   bool   `json:"withContext"`
}

type extractResult struct {
	Found      int                 `json:"found"`
	Resolved   int                 `json:"resolved"`
	References []ruian.ResolvedRef `json:"references"`
	Note       string              `json:"note,omitempty"`
}

func (s *service) extract(ctx context.Context, a extractArgs, lookups int) (*extractResult, error) {
	g, err := s.gaz()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Text) == "" {
		return nil, badInput("text is required")
	}
	if len(a.Text) > maxTextBytes && lookups <= maxLookups {
		return nil, badInput("text is %d bytes; the live endpoint accepts %d (use a batch run for longer documents)", len(a.Text), maxTextBytes)
	}
	refs := ruian.ExtractParcelRefs(a.Text, g, a.KUHint)
	res := &extractResult{Found: len(refs), References: []ruian.ResolvedRef{}}
	if !boolOr(a.Resolve, true) {
		for _, r := range refs {
			res.References = append(res.References, ruian.ResolvedRef{Ref: r, Parcels: []ruian.Parcel{}})
		}
		return res, nil
	}
	rr, err := s.reg.ResolveRefs(ctx, refs, ruian.ParcelOptions{Buildings: a.WithBuildings, Context: a.WithContext}, lookups)
	if err != nil {
		return nil, err
	}
	res.References = rr
	for _, r := range rr {
		if len(r.Parcels) > 0 {
			res.Resolved++
		}
	}
	if len(refs) > lookups {
		res.Note = fmt.Sprintf("Nalezeno %d odkazů, v RÚIAN dohledáno prvních %d.", len(refs), lookups)
	}
	return res, nil
}

// ---- resolve_address

type addressArgs struct {
	Query        string `json:"query"`
	Limit        int    `json:"limit"`
	WithBuilding *bool  `json:"withBuilding"` // default true
}

type addressHit struct {
	ruian.Address
	Building *ruian.Building `json:"building,omitempty"`
}

type addressResult struct {
	Query  string              `json:"query"`
	Parsed ruian.ParsedAddress `json:"parsed"`
	Hits   []addressHit        `json:"addresses"`
	Note   string              `json:"note,omitempty"`
}

func (s *service) resolveAddress(ctx context.Context, a addressArgs) (*addressResult, error) {
	if _, err := s.gaz(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(a.Query) == "" {
		return nil, badInput("query is required")
	}
	limit := a.Limit
	if limit <= 0 || limit > maxAddressResult {
		limit = 5
	}
	as, pa, err := s.reg.SearchAddresses(ctx, a.Query, limit)
	if err != nil {
		if errors.Is(err, ruian.ErrUpstream) {
			return nil, err
		}
		return nil, badInput("%v", err)
	}
	res := &addressResult{Query: a.Query, Parsed: pa, Hits: []addressHit{}}
	for _, ad := range as {
		h := addressHit{Address: ad}
		if boolOr(a.WithBuilding, true) && ad.BuildingKod > 0 && len(as) <= 3 {
			b, err := s.reg.BuildingByKod(ctx, ad.BuildingKod)
			if err != nil {
				return nil, err
			}
			if b != nil {
				b.Addresses = nil
				h.Building = b
			}
		}
		res.Hits = append(res.Hits, h)
	}
	if len(as) == 0 {
		res.Note = "Adresa nenalezena. Zkuste tvar „Ulice číslo, Obec“ s diakritikou, u obcí bez ulic „Obec číslo popisné“."
	}
	return res, nil
}

// ---- building_info

type buildingArgs struct {
	ParcelID    int64 `json:"parcelId"`
	AddressKod  int64 `json:"addressKod"`
	BuildingKod int64 `json:"buildingKod"`
}

func (s *service) buildingInfo(ctx context.Context, a buildingArgs) ([]ruian.Building, error) {
	switch {
	case a.BuildingKod > 0:
		b, err := s.reg.BuildingByKod(ctx, a.BuildingKod)
		if err != nil || b == nil {
			return []ruian.Building{}, err
		}
		return []ruian.Building{*b}, nil
	case a.AddressKod > 0:
		ad, err := s.reg.AddressByKod(ctx, a.AddressKod)
		if err != nil || ad == nil || ad.BuildingKod == 0 {
			return []ruian.Building{}, err
		}
		return s.buildingInfo(ctx, buildingArgs{BuildingKod: ad.BuildingKod})
	case a.ParcelID > 0:
		bs, err := s.reg.BuildingsOnParcel(ctx, a.ParcelID)
		if err != nil {
			return nil, err
		}
		for i := range bs {
			full, err := s.reg.BuildingByKod(ctx, bs[i].Kod)
			if err != nil {
				return nil, err
			}
			if full != nil {
				bs[i].Addresses = full.Addresses
			}
		}
		return bs, nil
	}
	return nil, badInput("one of parcelId, addressKod, buildingKod is required")
}

// ---- parcel_context

type contextArgs struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	ParcelID int64   `json:"parcelId"`
}

func (s *service) parcelContext(ctx context.Context, a contextArgs) (*ruian.Context, error) {
	p := ruian.LatLon{Lat: a.Lat, Lon: a.Lon}
	if a.ParcelID > 0 {
		pc, err := s.reg.ParcelByID(ctx, a.ParcelID, ruian.ParcelOptions{})
		if err != nil {
			return nil, err
		}
		if pc == nil || pc.Centroid == nil {
			return nil, badInput("parcel %d not found", a.ParcelID)
		}
		p = *pc.Centroid
	}
	if err := checkCZ(p); err != nil {
		return nil, err
	}
	return s.reg.ContextAt(ctx, p)
}

func checkCZ(p ruian.LatLon) error {
	if p.Lat < 48.5 || p.Lat > 51.1 || p.Lon < 12.0 || p.Lon > 18.9 {
		return badInput("point %.5f, %.5f is outside the Czech Republic (lat 48.5–51.1, lon 12.0–18.9); RÚIAN covers Czechia only", p.Lat, p.Lon)
	}
	return nil
}

// ---- nearby

type nearbyArgs struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radiusM"`
	What    string  `json:"what"`
	Limit   int     `json:"limit"`
}

func (s *service) nearby(ctx context.Context, a nearbyArgs) ([]ruian.NearbyItem, error) {
	p := ruian.LatLon{Lat: a.Lat, Lon: a.Lon}
	if err := checkCZ(p); err != nil {
		return nil, err
	}
	if a.RadiusM <= 0 {
		a.RadiusM = 100
	}
	if a.Limit <= 0 {
		a.Limit = 20
	}
	items, err := s.reg.Nearby(ctx, p, a.RadiusM, ruian.NearbyKind(a.What), a.Limit)
	if err != nil && !errors.Is(err, ruian.ErrUpstream) {
		return nil, badInput("%v", err)
	}
	return items, err
}

// ---- export_parcels

type exportArgs struct {
	KU     string `json:"ku"`
	KUCode int    `json:"kuCode"`
}

type exportResult struct {
	ruian.ExportStats
	URL  string `json:"url"`
	Note string `json:"note"`
}

func (s *service) kuCode(ku string, code int) (int, error) {
	if code > 0 {
		return code, nil
	}
	g, err := s.gaz()
	if err != nil {
		return 0, err
	}
	m := g.FindKU(ku, "")
	if len(m) == 0 {
		return 0, badInput("k.ú. %q not found", ku)
	}
	if !(m[0].Score >= 0.9 && (len(m) == 1 || m[1].Score < m[0].Score)) {
		var names []string
		for _, c := range m {
			names = append(names, fmt.Sprintf("%s (%d)", c.KU.Name, c.KU.Code))
		}
		return 0, badInput("k.ú. %q is ambiguous: %s", ku, strings.Join(names, ", "))
	}
	return m[0].KU.Code, nil
}

func (s *service) export(ctx context.Context, a exportArgs) (*exportResult, error) {
	code, err := s.kuCode(a.KU, a.KUCode)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	st, err := s.reg.ExportKU(ctx, code, 0.5, &buf)
	if err != nil {
		return nil, err
	}
	key := "parcels-" + strconv.Itoa(code) + ".geojson"
	u, err := s.act.SetRecord(ctx, key, "application/geo+json", buf.Bytes())
	if err != nil {
		return nil, err
	}
	return &exportResult{ExportStats: st, URL: u,
		Note: "GeoJSON (WGS84) všech parcel k.ú. bez vlastníků. Stažení vyžaduje Apify token (Authorization: Bearer). Soubor nevkládejte celý do kontextu modelu."}, nil
}
