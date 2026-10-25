package ruian

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strconv"
)

// ExportStats summarises an ExportKU run.
type ExportStats struct {
	KU       Unit `json:"ku"`
	Parcels  int  `json:"parcels"`
	Bytes    int  `json:"bytes"`
	Requests int  `json:"requests"`
}

// ExportKU writes every parcel of a k.ú. as a GeoJSON FeatureCollection
// (WGS84, polygons simplified to tolMeters, codes translated). A typical
// rural k.ú. has 1–3 thousand parcels and gives 1–5 MB.
func (r *Registry) ExportKU(ctx context.Context, kuCode int, tolMeters float64, w io.Writer) (ExportStats, error) {
	st := ExportStats{KU: r.territory(kuCode).KU}
	cw := &countWriter{w: w}
	fmt.Fprintf(cw, `{"type":"FeatureCollection","name":%q,"source":"ČÚZK RÚIAN, § 62 z. 111/2009 Sb.","features":[`, st.KU.Name)
	const page = 2000
	for offset := 0; ; offset += page {
		q := url.Values{"where": {fmt.Sprintf("katastralniuzemi=%d", kuCode)}, "outFields": {parcelFields},
			"returnGeometry": {"true"}, "geometryPrecision": {"7"}, "orderByFields": {"id"},
			"resultOffset": {strconv.Itoa(offset)}, "resultRecordCount": {strconv.Itoa(page)}}
		res, err := r.C.query(ctx, LayerParcel, q)
		st.Requests++
		if err != nil {
			return st, err
		}
		for _, f := range res.Features {
			p := r.parcelFrom(f, ParcelOptions{Geometry: true})
			if p.Geometry == nil {
				continue
			}
			p.Geometry = parseGeom(f.Geometry).polygon(tolMeters)
			props := map[string]any{"id": p.ID, "number": p.Number, "building": p.Building, "areaM2": p.AreaM2,
				"landType": p.LandType, "landTypeCode": p.LandTypeCode, "use": p.Use, "kuCode": kuCode}
			b, _ := json.Marshal(map[string]any{"type": "Feature", "geometry": p.Geometry, "properties": props})
			if st.Parcels > 0 {
				cw.Write([]byte{','})
			}
			cw.Write(b)
			st.Parcels++
		}
		if len(res.Features) < page {
			break
		}
		if offset > 200000 {
			return st, fmt.Errorf("k.ú. %d: more than 200 000 parcels, refusing", kuCode)
		}
	}
	fmt.Fprint(cw, "]}")
	st.Bytes = cw.n
	return st, cw.err
}

type countWriter struct {
	w   io.Writer
	n   int
	err error
}

func (c *countWriter) Write(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	n, err := c.w.Write(p)
	c.n += n
	c.err = err
	return n, err
}
