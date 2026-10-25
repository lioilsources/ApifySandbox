// smoke exercises the live ČÚZK service; not part of the library.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
)

func show(label string, v any, err error) {
	b, _ := json.MarshalIndent(v, "", " ")
	if len(b) > 2500 {
		b = append(b[:2500], []byte("…")...)
	}
	fmt.Printf("== %s err=%v\n%s\n", label, err, b)
}

func main() {
	ctx := context.Background()
	r := ruian.NewRegistry(ruian.New())
	t0 := time.Now()
	if err := r.Refresh(ctx); err != nil {
		fmt.Println("refresh:", err)
		os.Exit(1)
	}
	fmt.Println("gazetteer", r.Gazetteer().Size(), time.Since(t0))
	for _, n := range []string{"Hrutově", "Horních Počernicích", "Starém Městě", "Lhota", "Veveří", "Libni", "Mníšku u Liberce", "Brno-město"} {
		m := r.Gazetteer().FindKU(n, "")
		var s []string
		for _, x := range m {
			s = append(s, fmt.Sprintf("%s(%d %.2f)", x.KU.Name, x.KU.Code, x.Score))
		}
		fmt.Println("FindKU", n, "→", s)
	}
	pn, _ := ruian.ParseParcelNumber("st. 1")
	ps, err := r.Parcels(ctx, 648876, pn, ruian.ParcelOptions{Buildings: true, Context: true})
	show("parcel Hrutov st. 1", ps, err)
	c, err := r.ContextAt(ctx, ruian.LatLon{Lat: 50.03, Lon: 13.87})
	show("context Křivoklátsko", c, err)
	for _, q := range []string{"Veveří 95, Brno", "Veveří 331/95, 602 00 Brno", "Hrutov 12", "č.p. 12, Hrutov", "Vinohradská 12, Praha 2", "Karlovo náměstí 13 Praha"} {
		t1 := time.Now()
		as, pa, err := r.SearchAddresses(ctx, q, 3)
		var txt []string
		for _, a := range as {
			txt = append(txt, a.Text)
		}
		fmt.Printf("addr %-30q %+v → %v err=%v (%s)\n", q, pa, txt, err, time.Since(t1).Round(time.Millisecond))
	}
	text := "Stavební úřad oznamuje zahájení řízení o povolení záměru „Rodinný dům“ na pozemcích parc. č. 123/4, 123/5 a st. 56 v katastrálním území Hrutov. Dále se záměr dotkne pozemku p. č. 1 v k. ú. Hrutov."
	refs := ruian.ExtractParcelRefs(text, r.Gazetteer(), "")
	show("extract", refs, nil)
	rr, err := r.ResolveRefs(ctx, refs, ruian.ParcelOptions{}, 10)
	for _, x := range rr {
		fmt.Println(" resolved", x.Ref.Number, x.Ref.KU, len(x.Parcels))
	}
	near, err := r.Nearby(ctx, ruian.LatLon{Lat: 49.205215, Lon: 16.594049}, 80, ruian.NearBuildings, 3)
	show("nearby buildings", near, err)
}
