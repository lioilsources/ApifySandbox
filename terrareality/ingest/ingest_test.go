package ingest

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseLayout(t *testing.T) {
	cases := []struct {
		in      string
		rooms   int
		kitchen string // "", "y", "n"
	}{
		{"2+kk", 2, "n"}, {"3+1", 3, "y"}, {"3 pokoje", 3, ""}, {"2 szoba", 2, ""}, {"garsoniéra", 1, "n"}, {"4", 4, ""},
	}
	for _, c := range cases {
		r, k := ParseLayout(c.in)
		if r == nil || *r != c.rooms {
			t.Errorf("%q: rooms %v", c.in, r)
		}
		got := ""
		if k != nil {
			got = map[bool]string{true: "y", false: "n"}[*k]
		}
		if got != c.kitchen {
			t.Errorf("%q: kitchen %q", c.in, got)
		}
	}
}

func TestParseNumber(t *testing.T) {
	for in, want := range map[string]float64{"18 500 Kč": 18500, "1.250,50": 1250.5, "65 m²": 65, "18.500": 18500, "2 300,5 zł": 2300.5} {
		if got := ParseNumber(in); got == nil || *got != want {
			t.Errorf("%q → %v", in, got)
		}
	}
}

func TestNormalizeDropsPII(t *testing.T) {
	raw := json.RawMessage(`{"portal":"bezrealitky","id":"123","url":"https://www.bezrealitky.cz/nemovitosti-byty-domy/123",
		"city":"Praha","district":"Praha 7","price":"18 500 Kč","layout":"2+kk","area":"54 m²","category":"byt",
		"description":"Krásný byt…","images":["a.jpg"],"ownerName":"Jan Novák","phone":"+420777000000","email":"jan@example.com"}`)
	fx := &Rates{Date: "2026-10-16", PerEU: map[string]float64{"EUR": 1, "CZK": 25}}
	l, err := Normalize(Mappings["pringlesbitch/cz-reality-aggregator"], raw, fx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(l)
	for _, bad := range []string{"Novák", "777000000", "example.com", "a.jpg", "Krásný"} {
		if strings.Contains(string(b), bad) {
			t.Errorf("PII/content leaked: %s", bad)
		}
	}
	if l.Source != "bezrealitky_cz" || l.Country != "CZ" || l.PriceAmount != 1850000 || l.PriceCurrency != "CZK" || l.PriceEUR != 74000 {
		t.Errorf("%+v", l)
	}
	if *l.Rooms != 2 || *l.Kitchen || *l.AreaM2 != 54 || !l.ThirdParty {
		t.Errorf("%+v", l)
	}
	if l.ID != ListingID("bezrealitky_cz", "123") {
		t.Error("id not deterministic")
	}
}

func TestNormalizeSkips(t *testing.T) {
	m := Mappings["bakos_bence/ingatlan-com"]
	_, err := Normalize(m, json.RawMessage(`{"id":"1","url":"https://ingatlan.com/1"}`), nil, time.Now())
	if !errors.Is(err, ErrSkip) {
		t.Fatal("expected skip for missing price", err)
	}
	l, err := Normalize(m, json.RawMessage(`{"id":"1","url":"https://ingatlan.com/1","price":250000,"rooms":"2 szoba"}`), nil, time.Now())
	if err != nil || l.PriceCurrency != "HUF" || *l.Rooms != 2 {
		t.Fatal(l, err)
	}
}

func TestMergeTTLAndHistory(t *testing.T) {
	d := func(n int) time.Time { return time.Date(2026, 10, n, 6, 0, 0, 0, time.UTC) }
	mk := func(id string, price int64, at time.Time) *Listing {
		return &Listing{ID: id, Source: "sreality", PriceAmount: price, Active: true, FetchedAt: at, PriceHistory: []PricePt{{At: at, Amount: price}}}
	}
	state := map[string]*Listing{}
	run := map[string]bool{"sreality": true}
	Merge(state, []*Listing{mk("a", 100, d(1)), mk("b", 200, d(1))}, run, d(1))
	Merge(state, []*Listing{mk("a", 90, d(2))}, run, d(2))
	if len(state["a"].PriceHistory) != 2 || state["a"].FetchedAt != d(1) {
		t.Errorf("history %+v", state["a"])
	}
	if !state["b"].Active {
		t.Error("b deactivated after one miss")
	}
	Merge(state, []*Listing{mk("a", 90, d(3))}, map[string]bool{"otodom": true}, d(3)) // sreality did not run
	if !state["b"].Active {
		t.Error("b deactivated although its source did not run")
	}
	_, _, deact := Merge(state, []*Listing{mk("a", 90, d(4))}, run, d(4))
	if deact != 1 || state["b"].Active || len(state) != 2 {
		t.Errorf("deact=%d b=%+v", deact, state["b"])
	}
}
