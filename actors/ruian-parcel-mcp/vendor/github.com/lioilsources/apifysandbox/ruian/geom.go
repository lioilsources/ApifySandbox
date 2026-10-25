package ruian

import (
	"encoding/json"
	"math"
)

// LatLon is a WGS84 point rounded to ~1 m.
type LatLon struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

// Polygon is a GeoJSON Polygon geometry (lon, lat order).
type Polygon struct {
	Type        string         `json:"type"`
	Coordinates [][][2]float64 `json:"coordinates"`
}

type esriGeom struct {
	X     *float64       `json:"x"`
	Y     *float64       `json:"y"`
	Rings [][][2]float64 `json:"rings"`
}

func parseGeom(raw json.RawMessage) esriGeom {
	var g esriGeom
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &g)
	}
	return g
}

func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

func (g esriGeom) point() *LatLon {
	if g.X != nil && g.Y != nil {
		return &LatLon{round6(*g.Y), round6(*g.X)}
	}
	if len(g.Rings) > 0 {
		return centroid(g.Rings)
	}
	return nil
}

// centroid is the area-weighted centroid of the outer ring minus holes.
// Parcels are small, so planar math on degrees is accurate to centimetres.
func centroid(rings [][][2]float64) *LatLon {
	var a, cx, cy float64
	for _, r := range rings {
		for i := 0; i+1 < len(r); i++ {
			x0, y0, x1, y1 := r[i][0], r[i][1], r[i+1][0], r[i+1][1]
			cr := x0*y1 - x1*y0
			a += cr
			cx += (x0 + x1) * cr
			cy += (y0 + y1) * cr
		}
	}
	if a == 0 {
		if len(rings) == 0 || len(rings[0]) == 0 {
			return nil
		}
		p := rings[0][0]
		return &LatLon{round6(p[1]), round6(p[0])}
	}
	return &LatLon{round6(cy / (3 * a)), round6(cx / (3 * a))}
}

// polygon converts esri rings to GeoJSON, simplified to tolMeters.
func (g esriGeom) polygon(tolMeters float64) *Polygon {
	if len(g.Rings) == 0 {
		return nil
	}
	p := &Polygon{Type: "Polygon"}
	tol := tolMeters / 111_000 // degrees; good enough at 49–51° N for simplification
	for _, r := range g.Rings {
		s := simplify(r, tol)
		for i := range s {
			s[i] = [2]float64{round6(s[i][0]), round6(s[i][1])}
		}
		p.Coordinates = append(p.Coordinates, s)
	}
	return p
}

// simplify is Douglas–Peucker on a closed ring; keeps at least 4 points.
func simplify(pts [][2]float64, tol float64) [][2]float64 {
	if len(pts) <= 4 || tol <= 0 {
		return append([][2]float64(nil), pts...)
	}
	keep := make([]bool, len(pts))
	keep[0], keep[len(pts)-1] = true, true
	var rec func(i, j int)
	rec = func(i, j int) {
		if j <= i+1 {
			return
		}
		maxD, idx := -1.0, -1
		for k := i + 1; k < j; k++ {
			if d := segDist(pts[k], pts[i], pts[j]); d > maxD {
				maxD, idx = d, k
			}
		}
		if maxD > tol {
			keep[idx] = true
			rec(i, idx)
			rec(idx, j)
		}
	}
	// closed ring: split at the farthest point from the start so both halves work
	far, fd := 0, -1.0
	for k := range pts {
		if d := math.Hypot(pts[k][0]-pts[0][0], pts[k][1]-pts[0][1]); d > fd {
			far, fd = k, d
		}
	}
	keep[far] = true
	rec(0, far)
	rec(far, len(pts)-1)
	out := make([][2]float64, 0, len(pts))
	for k, p := range pts {
		if keep[k] {
			out = append(out, p)
		}
	}
	if len(out) < 4 {
		return append([][2]float64(nil), pts...)
	}
	return out
}

func segDist(p, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	if dx == 0 && dy == 0 {
		return math.Hypot(p[0]-a[0], p[1]-a[1])
	}
	t := ((p[0]-a[0])*dx + (p[1]-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(p[0]-(a[0]+t*dx), p[1]-(a[1]+t*dy))
}

// distanceM is the haversine distance in metres.
func distanceM(a, b LatLon) float64 {
	const r = 6371000
	la1, la2 := a.Lat*math.Pi/180, b.Lat*math.Pi/180
	dla := la2 - la1
	dlo := (b.Lon - a.Lon) * math.Pi / 180
	h := math.Sin(dla/2)*math.Sin(dla/2) + math.Cos(la1)*math.Cos(la2)*math.Sin(dlo/2)*math.Sin(dlo/2)
	return 2 * r * math.Asin(math.Sqrt(h))
}
