package leads

import "testing"

func TestNameKey(t *testing.T) {
	if NameKey("REMMARK, a.s.") != NameKey("Remmark") || NameKey("Studio Šťastný s.r.o.") != "studiostastny" {
		t.Fatal(NameKey("REMMARK, a.s."), NameKey("Studio Šťastný s.r.o."))
	}
}

func TestMerge(t *testing.T) {
	places := []Place{
		{Title: "Remmark a.s.", City: "Praha", Website: "https://www.remmark.cz/", Phone: "777 123 456", PlaceID: "p1", SearchString: "reklamní agentura Praha", Categories: []string{"Reklamní agentura"}},
		{Title: "Remmark", City: "Praha", Website: "remmark.cz", PlaceID: "p2", Categories: []string{"Reklamní agentura"}}, // same domain → dup
		{Title: "Pixel Forge Games", City: "Brno", PlaceID: "p3", SearchString: "game studio Brno", Categories: []string{"Vývoj softwaru"}},
		{Title: "Cibien's Corner Arcade & Museum", City: "Praha", PlaceID: "p4", SearchString: "herní studio", Categories: []string{"Muzeum"}},
	}
	contacts := []Contacts{{Domain: "remmark.cz", Emails: []string{"info@remmark.cz", "jan.novak@gmail.com", "logo@2x.png"}, LastActive: "2026-09-30"}}
	reg := map[string]Registry{NameKey("REMMARK, a.s."): {ICO: "25652869", Segment: "agentury", Size: "10 - 19 zaměstnanců", Score: 132.3}}
	out, kept, dropped, st := Merge(places, contacts, reg)
	if st.Duplicates != 1 || st.Filtered != 1 || st.Leads != 2 || st.WithEmail != 1 || st.WithAnyContact != 1 || st.JoinedRegistry != 1 {
		t.Fatalf("%+v", st)
	}
	if len(kept) != len(out) || len(dropped) != 1 || dropped[0].Title != "Cibien's Corner Arcade & Museum" {
		t.Fatalf("kept=%d dropped=%+v", len(kept), dropped)
	}
	l := out[0]
	if l.ICO != "25652869" || l.Segment != "agentury" || len(l.Emails) != 1 || l.Emails[0] != "info@remmark.cz" || l.Phones[0] != "+420777123456" || l.LastActive != "2026-09-30" {
		t.Fatalf("%+v", l)
	}
	if out[1].Segment != "hry" {
		t.Fatal(out[1].Segment)
	}
}

func TestFilter(t *testing.T) {
	cases := []struct {
		p    Place
		seg  string
		keep bool
	}{
		{Place{Title: "Warhorse Studios", Categories: []string{"Vývoj softwaru"}, Website: "warhorsestudios.cz"}, "hry", true},
		{Place{Title: "Gametoria S.r.o.", Categories: []string{"Ústředí společnosti"}}, "hry", true},
		{Place{Title: "Holding XY", Categories: []string{"Ústředí společnosti"}}, "hry", false},
		{Place{Title: "Icarus Games S.r.o.", Categories: []string{"Ústředí společnosti"}}, "hry", true},
		{Place{Title: "JaK studio", Categories: []string{"Sportovní hala / tělocvična"}}, "hry", false},
		{Place{Title: "Czech Game Developers Association", Categories: []string{"Sdružení nebo organizace"}}, "hry", false},
		{Place{Title: "E-shop iSETOS", Categories: []string{"Prodej výpočetní techniky"}}, "eshopy", true},
		{Place{Title: "UNI HOBBY Market Brno", Categories: []string{"Prodejna hobby potřeb", "Hobbymarket"}}, "eshopy", false},
		{Place{Title: "SEXSHOP.cz", Categories: []string{"Sex shop"}}, "eshopy", false},
		{Place{Title: "MIVELA - úklidové služby", Categories: []string{"Úklidové služby"}}, "teren", true},
		{Place{Title: "Content Company", Categories: []string{"Videoprodukce", "Internetový marketing"}}, "agentury", true},
		{Place{Title: "Firma", Categories: []string{"Úklidové služby"}, Closed: true}, "teren", false},
	}
	for _, c := range cases {
		if ok, why := Filter(c.p, c.seg); ok != c.keep {
			t.Errorf("%s: keep=%v (%s)", c.p.Title, ok, why)
		}
	}
}

func TestICOHelpers(t *testing.T) {
	if !ValidICO("24155489") || !ValidICO("25652869") || ValidICO("24155488") || ValidICO("1234567") {
		t.Fatal("checksum")
	}
	got := ICOsInText(`<footer>Warhorse Studios s.r.o., IČO: 24155489, DIČ: CZ24155489; IČ 241 55 488</footer>`)
	if len(got) != 1 || got[0] != "24155489" {
		t.Fatal(got)
	}
	if PSC("Karlická 1724/41, 153 00 Praha 16, Česko") != 15300 || PSC("Praha") != 0 {
		t.Fatal("psc")
	}
	if CleanTitle("Fanl - marketingová agentura") != "Fanl" || CleanTitle("UMAN | Úklidová firma Brno") != "UMAN" || CleanTitle("CTRL M – Marketingová agentura Brno") != "CTRL M" {
		t.Fatal(CleanTitle("UMAN | Úklidová firma Brno"))
	}
	if !sameCity("Praha", "Praha 16") || !sameCity("Brno", "Brno-sever") || sameCity("Brno", "Brnov") {
		t.Fatal("sameCity")
	}
}
