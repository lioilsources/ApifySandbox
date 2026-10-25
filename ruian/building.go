package ruian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Building is a RÚIAN stavební objekt with its technical-economic
// attributes (TEA) translated to Czech. nil means ČÚZK has no value.
type Building struct {
	Kod          int64     `json:"kod"`
	Number       string    `json:"number,omitempty"` // "č.p. 12", "č.ev. 5"
	Type         string    `json:"type"`
	Use          *string   `json:"use"`
	Floors       *int      `json:"floors"`
	Flats        *int      `json:"flats"`
	BuiltAreaM2  *float64  `json:"builtAreaM2"`
	FloorAreaM2  *float64  `json:"floorAreaM2"`
	VolumeM3     *float64  `json:"volumeM3"`
	Completed    *string   `json:"completed"`
	Construction *string   `json:"construction"`
	Heating      *string   `json:"heating"`
	Water        *string   `json:"water"`
	Sewer        *string   `json:"sewer"`
	Gas          *string   `json:"gas"`
	Elevator     *string   `json:"elevator"`
	ParcelID     int64     `json:"parcelId"`
	Centroid     *LatLon   `json:"centroid,omitempty"`
	Addresses    []Address `json:"addresses,omitempty"`
	VDP          string    `json:"vdp"`
}

type buildingAttrs struct {
	Kod        float64  `json:"kod"`
	Cisla      *string  `json:"cisladomovni"`
	Parcela    *float64 `json:"identifikacniparcela"`
	Typ        *float64 `json:"typstavebnihoobjektukod"`
	Vyuziti    *float64 `json:"zpusobvyuzitikod"`
	Dokonceni  *float64 `json:"dokonceni"`
	Konstrukce *float64 `json:"druhkonstrukcekod"`
	Obestaveny *float64 `json:"obestavenyprostor"`
	Byty       *float64 `json:"pocetbytu"`
	Podlazi    *float64 `json:"pocetpodlazi"`
	Podlahova  *float64 `json:"podlahovaplocha"`
	Kanal      *float64 `json:"pripojenikanalizacekod"`
	Plyn       *float64 `json:"pripojeniplynkod"`
	Voda       *float64 `json:"pripojenivodovodkod"`
	Vytah      *float64 `json:"vybavenivytahemkod"`
	Zastavena  *float64 `json:"zastavenaplocha"`
	Vytapeni   *float64 `json:"zpusobvytapenikod"`
}

const buildingFields = "kod,cisladomovni,identifikacniparcela,typstavebnihoobjektukod,zpusobvyuzitikod,dokonceni,druhkonstrukcekod,obestavenyprostor,pocetbytu,pocetpodlazi,podlahovaplocha,pripojenikanalizacekod,pripojeniplynkod,pripojenivodovodkod,vybavenivytahemkod,zastavenaplocha,zpusobvytapenikod"

func intp(f *float64) *int {
	if f == nil {
		return nil
	}
	v := int(*f)
	return &v
}

func posp(f *float64) *float64 {
	if f == nil || *f <= 0 {
		return nil
	}
	return f
}

func buildingFrom(f feature) Building {
	var a buildingAttrs
	_ = json.Unmarshal(f.Attributes, &a)
	b := Building{Kod: int64(a.Kod), Use: lookupPtr(CBZpusobVyuzitiObjektu, intp(a.Vyuziti)),
		Floors: intp(a.Podlazi), Flats: intp(a.Byty), BuiltAreaM2: posp(a.Zastavena), FloorAreaM2: posp(a.Podlahova),
		VolumeM3: posp(a.Obestaveny), Construction: lookupPtr(CBDruhKonstrukce, intp(a.Konstrukce)),
		Heating: lookupPtr(CBZpusobVytapeni, intp(a.Vytapeni)), Water: lookupPtr(CBPripojeniVody, intp(a.Voda)),
		Sewer: lookupPtr(CBPripojeniKanalizace, intp(a.Kanal)), Gas: lookupPtr(CBPripojeniPlynu, intp(a.Plyn)),
		Elevator: lookupPtr(CBVybaveniVytahem, intp(a.Vytah)), Centroid: parseGeom(f.Geometry).point()}
	if a.Parcela != nil {
		b.ParcelID = int64(*a.Parcela)
	}
	typ := 0
	if a.Typ != nil {
		typ = int(*a.Typ)
		b.Type = Lookup(CBTypStavebnihoObjektu, typ)
	}
	if a.Cisla != nil && *a.Cisla != "" {
		prefix := map[int]string{1: "č.p. ", 2: "č.ev. "}[typ]
		b.Number = prefix + *a.Cisla
	}
	if a.Dokonceni != nil {
		s := time.UnixMilli(int64(*a.Dokonceni)).UTC().Format(time.DateOnly)
		b.Completed = &s
	}
	b.VDP = fmt.Sprintf("https://vdp.cuzk.gov.cz/vdp/ruian/stavebniobjekty/%d", b.Kod)
	return b
}

func (r *Registry) buildingQuery(ctx context.Context, where string, limit int) ([]Building, error) {
	q := url.Values{"where": {where}, "outFields": {buildingFields}, "returnGeometry": {"true"},
		"geometryPrecision": {"7"}, "resultRecordCount": {strconv.Itoa(limit)}}
	res, err := r.C.query(ctx, LayerBuilding, q)
	if err != nil {
		return nil, err
	}
	out := make([]Building, 0, len(res.Features))
	for _, f := range res.Features {
		out = append(out, buildingFrom(f))
	}
	return out, nil
}

// BuildingsOnParcel returns the building(s) whose identifying parcel is id.
func (r *Registry) BuildingsOnParcel(ctx context.Context, parcelID int64) ([]Building, error) {
	return r.buildingQuery(ctx, fmt.Sprintf("identifikacniparcela=%d", parcelID), 10)
}

// BuildingByKod fetches a building with its addresses.
func (r *Registry) BuildingByKod(ctx context.Context, kod int64) (*Building, error) {
	bs, err := r.buildingQuery(ctx, fmt.Sprintf("kod=%d", kod), 1)
	if err != nil || len(bs) == 0 {
		return nil, err
	}
	b := bs[0]
	as, err := r.addressQuery(ctx, fmt.Sprintf("stavebniobjekt=%d", kod), 50)
	if err != nil {
		return nil, err
	}
	b.Addresses = as
	return &b, nil
}

// Address is a RÚIAN adresní místo.
type Address struct {
	Kod         int64   `json:"kod"`
	Text        string  `json:"text"`
	PSC         string  `json:"psc,omitempty"`
	Point       *LatLon `json:"point"`
	BuildingKod int64   `json:"buildingKod,omitempty"`
	VDP         string  `json:"vdp"`
	DistanceM   *int    `json:"distanceM,omitempty"`
}

type addressAttrs struct {
	Kod      float64  `json:"kod"`
	Adresa   string   `json:"adresa"`
	PSC      *float64 `json:"psc"`
	Stavebni *float64 `json:"stavebniobjekt"`
}

func addressFrom(f feature) Address {
	var a addressAttrs
	_ = json.Unmarshal(f.Attributes, &a)
	ad := Address{Kod: int64(a.Kod), Text: a.Adresa, Point: parseGeom(f.Geometry).point()}
	if a.PSC != nil {
		ad.PSC = strconv.Itoa(int(*a.PSC))
	}
	if a.Stavebni != nil {
		ad.BuildingKod = int64(*a.Stavebni)
	}
	ad.VDP = fmt.Sprintf("https://vdp.cuzk.gov.cz/vdp/ruian/adresnimista/%d", ad.Kod)
	return ad
}

func (r *Registry) addressQuery(ctx context.Context, where string, limit int) ([]Address, error) {
	q := url.Values{"where": {where}, "outFields": {"kod,adresa,psc,stavebniobjekt"}, "returnGeometry": {"true"},
		"resultRecordCount": {strconv.Itoa(limit)}}
	res, err := r.C.query(ctx, LayerAddress, q)
	if err != nil {
		return nil, err
	}
	out := make([]Address, 0, len(res.Features))
	for _, f := range res.Features {
		out = append(out, addressFrom(f))
	}
	return out, nil
}

// AddressByKod fetches one address point by its RÚIAN code.
func (r *Registry) AddressByKod(ctx context.Context, kod int64) (*Address, error) {
	as, err := r.addressQuery(ctx, fmt.Sprintf("kod=%d", kod), 1)
	if err != nil || len(as) == 0 {
		return nil, err
	}
	return &as[0], nil
}

var (
	houseNoRe = regexp.MustCompile(`(?i)(?:č\.\s*(?:p|ev)\.\s*)?\b(\d{1,5})\s*([a-z])?(?:\s*/\s*(\d{1,4})\s*([a-z])?)?\b`)
	pscRe     = regexp.MustCompile(`\b(\d{3})\s?(\d{2})\b`)
)

// ParsedAddress is a free-text address split into its parts.
type ParsedAddress struct {
	Street   string `json:"street,omitempty"`
	HouseNo  int    `json:"houseNo,omitempty"` // číslo popisné/evidenční, or the only number given
	OrientNo int    `json:"orientNo,omitempty"`
	Letter   string `json:"letter,omitempty"`
	City     string `json:"city,omitempty"`
	PSC      string `json:"psc,omitempty"`
}

// ParseAddress splits "Veveří 331/95, 602 00 Brno", "Hrutov 12" or
// "č.p. 12, Hrutov". The city is the last comma part, or the trailing
// words that name a municipality.
func (g *Gazetteer) ParseAddress(s string) ParsedAddress {
	var a ParsedAddress
	if m := pscRe.FindStringSubmatch(s); m != nil {
		a.PSC = m[1] + m[2]
		s = strings.Replace(s, m[0], " ", 1)
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	var streetPart string
	var cityParts []string
	for _, p := range parts {
		if p == "" {
			continue
		}
		if streetPart == "" && houseNoRe.MatchString(p) {
			streetPart = p
			continue
		}
		cityParts = append(cityParts, p)
	}
	if streetPart == "" && len(cityParts) > 0 {
		streetPart, cityParts = cityParts[0], cityParts[1:]
	}
	if m := houseNoRe.FindStringSubmatchIndex(streetPart); m != nil {
		n1, _ := strconv.Atoi(streetPart[m[2]:m[3]])
		if m[6] >= 0 {
			a.HouseNo = n1
			a.OrientNo, _ = strconv.Atoi(streetPart[m[6]:m[7]])
			if m[8] >= 0 {
				a.Letter = strings.ToLower(streetPart[m[8]:m[9]])
			}
		} else {
			a.HouseNo = n1
			if m[4] >= 0 {
				a.Letter = strings.ToLower(streetPart[m[4]:m[5]])
			}
		}
		streetPart = strings.TrimSpace(streetPart[:m[0]] + " " + streetPart[m[1]:])
	}
	streetPart = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(streetPart, "ul."), "ulice"))
	if len(cityParts) > 0 {
		// "Veveří, Brno": the last part is the city; a middle part is usually část obce
		a.City = cityParts[len(cityParts)-1]
	} else if g != nil {
		// "Veveří 95 Brno" / "Hrutov 12": find trailing words that name a municipality
		ws := strings.Fields(streetPart)
		for k := 0; k < len(ws); k++ {
			if len(g.FindObec(strings.Join(ws[k:], " "))) > 0 {
				a.City = strings.Join(ws[k:], " ")
				streetPart = strings.Join(ws[:k], " ")
				break
			}
		}
	}
	a.Street = strings.TrimSpace(streetPart)
	return a
}

// SearchAddresses resolves a free-text address with indexed lookups
// (street → street code → address points; or municipality → building
// number for villages without streets). Only if that finds nothing it
// falls back to a slow (~2 s) substring search on the full address text.
func (r *Registry) SearchAddresses(ctx context.Context, query string, limit int) ([]Address, ParsedAddress, error) {
	limit = min(max(limit, 1), 50)
	g := r.Gazetteer()
	pa := g.ParseAddress(query)
	var obce []Unit
	if g != nil && pa.City != "" {
		obce = g.FindObec(pa.City)
	}
	numCond := ""
	switch {
	case pa.OrientNo > 0:
		numCond = fmt.Sprintf(" AND cislodomovni=%d AND cisloorientacni=%d", pa.HouseNo, pa.OrientNo)
	case pa.HouseNo > 0:
		numCond = fmt.Sprintf(" AND (cislodomovni=%d OR cisloorientacni=%d)", pa.HouseNo, pa.HouseNo)
	}
	if pa.Letter != "" {
		numCond += " AND cisloorientacnipismeno=" + sqlString(pa.Letter)
	}
	if pa.PSC != "" {
		numCond += " AND psc=" + pa.PSC
	}
	if pa.Street != "" {
		where := "UPPER(nazev)=UPPER(" + sqlString(pa.Street) + ")"
		if len(obce) > 0 {
			where += " AND obec IN (" + joinCodes(obce) + ")"
		}
		res, err := r.C.query(ctx, layerStreet, url.Values{"where": {where}, "outFields": {"kod"}, "returnGeometry": {"false"}, "resultRecordCount": {"50"}})
		if err != nil {
			return nil, pa, err
		}
		if codes := codesOf(res); codes != "" {
			as, err := r.addressQuery(ctx, "ulice IN ("+codes+")"+numCond, limit)
			if err != nil || len(as) > 0 {
				return as, pa, err
			}
		}
	}
	if len(obce) > 0 && pa.HouseNo > 0 && pa.OrientNo == 0 {
		// village or address without street: obec → části obce → building č.p./č.ev. → address points
		res, err := r.C.query(ctx, layerCastObce, url.Values{"where": {"obec IN (" + joinCodes(obce) + ")"}, "outFields": {"kod"}, "returnGeometry": {"false"}})
		if err != nil {
			return nil, pa, err
		}
		if parts := codesOf(res); parts != "" {
			bres, err := r.C.query(ctx, LayerBuilding, url.Values{"where": {fmt.Sprintf("castobce IN (%s) AND cisladomovni=%s", parts, sqlString(strconv.Itoa(pa.HouseNo)))},
				"outFields": {"kod"}, "returnGeometry": {"false"}, "resultRecordCount": {"20"}})
			if err != nil {
				return nil, pa, err
			}
			if bs := codesOf(bres); bs != "" {
				as, err := r.addressQuery(ctx, "stavebniobjekt IN ("+bs+")", limit)
				if err != nil || len(as) > 0 {
					return as, pa, err
				}
			}
		}
	}
	// fallback: substring match on the composed address text (case-sensitive)
	var conds []string
	for _, t := range []string{pa.Street, pa.City} {
		if t != "" {
			conds = append(conds, "adresa LIKE "+sqlString("%"+t+"%"))
		}
	}
	if len(conds) == 0 {
		return nil, pa, fmt.Errorf("could not parse address %q", query)
	}
	if pa.HouseNo > 0 {
		conds = append(conds, fmt.Sprintf("(cislodomovni=%d OR cisloorientacni=%d)", pa.HouseNo, max(pa.OrientNo, pa.HouseNo)))
	}
	as, err := r.addressQuery(ctx, strings.Join(conds, " AND "), limit)
	return as, pa, err
}

const (
	layerCastObce = 11
	layerStreet   = 4
)

func joinCodes(us []Unit) string {
	s := make([]string, len(us))
	for i, u := range us {
		s[i] = strconv.Itoa(u.Code)
	}
	return strings.Join(s, ",")
}

func codesOf(res *queryResult) string {
	var s []string
	for _, f := range res.Features {
		var a struct {
			Kod float64 `json:"kod"`
		}
		if json.Unmarshal(f.Attributes, &a) == nil && a.Kod > 0 {
			s = append(s, strconv.FormatInt(int64(a.Kod), 10))
		}
	}
	return strings.Join(s, ",")
}
