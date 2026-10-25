//go:build online

// Live checks against the ČÚZK service: go test -tags online ./...
// Values verified by hand on 2026-10-25; RÚIAN can change, so a failure
// here means "look", not necessarily "bug".
package ruian

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

func liveRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry(New(WithUserAgent("ruian-go-test (+https://github.com/lioilsources/apifysandbox)")))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := r.Refresh(ctx); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestLiveParcel(t *testing.T) {
	r := liveRegistry(t)
	n, _ := ParseParcelNumber("st. 1")
	ps, err := r.Parcels(context.Background(), 648876, n, ParcelOptions{Buildings: true})
	if err != nil || len(ps) != 1 {
		t.Fatalf("parcels = %v, %v", ps, err)
	}
	p := ps[0]
	if p.AreaM2 != 318 || p.LandTypeCode != 13 || p.Territory.ORP.Name != "Jihlava" {
		t.Errorf("unexpected parcel %+v", p)
	}
	if len(p.Buildings) != 1 || p.Buildings[0].Number != "č.p. 12" {
		t.Errorf("buildings %+v", p.Buildings)
	}
}

func TestLiveContext(t *testing.T) {
	r := liveRegistry(t)
	c, err := r.ContextAt(context.Background(), LatLon{Lat: 50.03, Lon: 13.87})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range c.ProtectedAreas {
		if a.Kind == "chráněná krajinná oblast" && a.Name == "Křivoklátsko" {
			found = true
		}
	}
	if !found || c.Territory == nil || c.Territory.ORP.Name != "Rakovník" {
		t.Errorf("context %+v", c)
	}
	c2, err := r.ContextAt(context.Background(), LatLon{Lat: 49.42, Lon: 15.75})
	if err != nil || c2.Territory == nil || c2.Territory.ORP.Name != "Jihlava" {
		t.Errorf("ORP Jihlava expected: %+v %v", c2, err)
	}
}

func TestLiveAddress(t *testing.T) {
	r := liveRegistry(t)
	as, _, err := r.SearchAddresses(context.Background(), "Veveří 953/73, Brno", 3)
	if err != nil || len(as) == 0 || as[0].Kod != 19176651 {
		t.Errorf("address = %+v, %v", as, err)
	}
}

func TestLiveExport(t *testing.T) {
	r := liveRegistry(t)
	var buf bytes.Buffer
	st, err := r.ExportKU(context.Background(), 648876, 0.5, &buf)
	if err != nil || st.Parcels < 1000 {
		t.Fatalf("export %+v %v", st, err)
	}
	var fc struct {
		Features []json.RawMessage `json:"features"`
	}
	if err := json.Unmarshal(buf.Bytes(), &fc); err != nil || len(fc.Features) != st.Parcels {
		t.Errorf("invalid GeoJSON: %v (%d features)", err, len(fc.Features))
	}
	t.Logf("Hrutov export: %+v", st)
}
