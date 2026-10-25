package ruian

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Special-purpose layers (účelové územní prvky) checked by ContextAt.
var areaLayers = map[int]string{
	22: "dobývací prostor",
	24: "chráněné ložiskové území",
	30: "národní park",
	32: "ochranné pásmo národního parku",
	34: "zóna národního parku",
	38: "klidové území národního parku",
	40: "chráněná krajinná oblast",
	42: "zóna CHKO",
	44: "národní přírodní rezervace",
	46: "ochranné pásmo NPR",
	48: "národní přírodní památka",
	50: "ochranné pásmo NPP",
	52: "přírodní rezervace",
	54: "ochranné pásmo PR",
	56: "přírodní památka",
	58: "ochranné pásmo PP",
	60: "evropsky významná lokalita (Natura 2000)",
	62: "ptačí oblast (Natura 2000)",
	65: "ochranné pásmo památného stromu",
	67: "smluvně chráněné území",
}

const layerBPEJ = 28

// Area is a protected or mining area that covers the point.
type Area struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Kod    int64  `json:"kod"`
	Zone   string `json:"zone,omitempty"`   // CHKO/NP zone
	Detail string `json:"detail,omitempty"` // mineral for mining areas, declaring act otherwise
	Link   string `json:"link,omitempty"`   // AOPK / source agenda
}

// Context is everything RÚIAN knows about the place around a point.
type Context struct {
	Point          LatLon     `json:"point"`
	Territory      *Territory `json:"territory,omitempty"`
	ProtectedAreas []Area     `json:"protectedAreas"`
	MiningAreas    []Area     `json:"miningAreas"`
	BPEJ           string     `json:"bpej,omitempty"` // soil quality unit code (5 digits)
	BuildingOffice string     `json:"buildingOffice,omitempty"`
}

// ContextAt runs one identify request over k.ú. and all special-purpose
// layers. Building offices are not in RÚIAN; the hint names the POÚ/ORP
// where a general building office usually sits.
func (r *Registry) ContextAt(ctx context.Context, p LatLon) (*Context, error) {
	ids := []string{strconv.Itoa(LayerKU), strconv.Itoa(layerBPEJ)}
	for id := range areaLayers {
		ids = append(ids, strconv.Itoa(id))
	}
	sort.Strings(ids)
	d := 0.001
	q := url.Values{
		"geometry":       {fmt.Sprintf("%.6f,%.6f", p.Lon, p.Lat)},
		"geometryType":   {"esriGeometryPoint"},
		"sr":             {"4326"},
		"layers":         {"all:" + strings.Join(ids, ",")},
		"tolerance":      {"0"},
		"mapExtent":      {fmt.Sprintf("%.6f,%.6f,%.6f,%.6f", p.Lon-d, p.Lat-d, p.Lon+d, p.Lat+d)},
		"imageDisplay":   {"400,400,96"},
		"returnGeometry": {"false"},
	}
	b, err := r.C.get(ctx, "/identify", q)
	if err != nil {
		return nil, err
	}
	var res struct {
		Results []struct {
			LayerID    int               `json:"layerId"`
			Attributes map[string]string `json:"attributes"`
		} `json:"results"`
	}
	if err := json.Unmarshal(b, &res); err != nil {
		return nil, fmt.Errorf("%w: bad identify JSON: %v", ErrUpstream, err)
	}
	c := &Context{Point: p, ProtectedAreas: []Area{}, MiningAreas: []Area{}}
	for _, it := range res.Results {
		a := it.Attributes
		switch {
		case it.LayerID == LayerKU:
			if code, err := strconv.Atoi(a["Kód katastrálního území"]); err == nil {
				t := r.territory(code)
				c.Territory = &t
			}
		case it.LayerID == layerBPEJ:
			c.BPEJ = firstNonEmpty(clean(a["Název účelového prvku"]), clean(a["Číslo účelového prvku"]))
		default:
			kind, ok := areaLayers[it.LayerID]
			if !ok {
				continue
			}
			ar := Area{Kind: kind, Name: clean(a["Název účelového prvku"]), Link: clean(a["Odkaz do agendového systému zdroje dat"])}
			ar.Kod, _ = strconv.ParseInt(a["Kód účelového prvku"], 10, 64)
			if st, err := strconv.Atoi(a["Subtyp účelového prvku"]); err == nil {
				ar.Zone = Lookup("subtypUUP", st)
			}
			if it.LayerID == 22 || it.LayerID == 24 {
				ar.Detail = clean(a["Nerost"])
				c.MiningAreas = append(c.MiningAreas, ar)
			} else {
				c.ProtectedAreas = append(c.ProtectedAreas, ar)
			}
		}
	}
	if c.Territory != nil && c.Territory.POU.Name != "" {
		c.BuildingOffice = fmt.Sprintf("Obecný stavební úřad pro obec %s bývá na úřadě obce %s nebo ORP %s; RÚIAN příslušnost neeviduje, ověřte na webu obce.",
			c.Territory.Obec.Name, c.Territory.POU.Name, c.Territory.ORP.Name)
	}
	return c, nil
}

func clean(s string) string {
	if s == "Null" {
		return ""
	}
	return strings.TrimSpace(s)
}

// NearbyKind selects what Nearby looks for.
type NearbyKind string

const (
	NearAddresses NearbyKind = "addresses"
	NearBuildings NearbyKind = "buildings"
	NearParcels   NearbyKind = "parcels"
)

// NearbyItem is one hit of Nearby, sorted by distance.
type NearbyItem struct {
	DistanceM int       `json:"distanceM"`
	Address   *Address  `json:"address,omitempty"`
	Building  *Building `json:"building,omitempty"`
	Parcel    *Parcel   `json:"parcel,omitempty"`
}

// Nearby returns objects within radiusM (≤ 2000) of p, nearest first.
func (r *Registry) Nearby(ctx context.Context, p LatLon, radiusM float64, kind NearbyKind, limit int) ([]NearbyItem, error) {
	radiusM = min(max(radiusM, 1), 2000)
	limit = min(max(limit, 1), 100)
	layer, fields := LayerAddress, "kod,adresa,psc,stavebniobjekt"
	switch kind {
	case NearBuildings:
		layer, fields = LayerBuilding, buildingFields
	case NearParcels:
		layer, fields = LayerParcel, parcelFields
	case NearAddresses, "":
	default:
		return nil, fmt.Errorf("unknown kind %q (addresses, buildings, parcels)", kind)
	}
	q := url.Values{
		"geometry": {fmt.Sprintf("%.6f,%.6f", p.Lon, p.Lat)}, "geometryType": {"esriGeometryPoint"}, "inSR": {"4326"},
		"distance": {strconv.Itoa(int(radiusM))}, "units": {"esriSRUnit_Meter"}, "spatialRel": {"esriSpatialRelIntersects"},
		"outFields": {fields}, "returnGeometry": {"true"}, "geometryPrecision": {"7"}, "resultRecordCount": {"1000"},
	}
	res, err := r.C.query(ctx, layer, q)
	if err != nil {
		return nil, err
	}
	out := make([]NearbyItem, 0, len(res.Features))
	for _, f := range res.Features {
		var it NearbyItem
		var at *LatLon
		switch layer {
		case LayerAddress:
			a := addressFrom(f)
			it.Address, at = &a, a.Point
		case LayerBuilding:
			b := buildingFrom(f)
			it.Building, at = &b, b.Centroid
		case LayerParcel:
			pp := r.parcelFrom(f, ParcelOptions{})
			it.Parcel, at = &pp, pp.Centroid
		}
		if at != nil {
			it.DistanceM = int(distanceM(p, *at) + 0.5)
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DistanceM < out[j].DistanceM })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}
