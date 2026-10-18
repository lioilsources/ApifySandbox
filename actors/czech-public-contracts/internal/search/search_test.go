package search

import (
	"os"
	"testing"
	"time"

	"github.com/lioilsources/czech-public-contracts/internal/rs"
)

func TestSelectDumps(t *testing.T) {
	all := []rs.Dump{
		{Year: 2026, Month: 8, Complete: 1, URL: "https://x/dump_2026_08.xml"},
		{Year: 2026, Month: 9, Complete: 1, URL: "https://x/dump_2026_09.xml"},
		{Year: 2026, Month: 10, Day: 1, URL: "https://x/dump_2026_10_01.xml"},
		{Year: 2026, Month: 10, Day: 2, URL: "https://x/dump_2026_10_02.xml"},
		{Year: 2026, Month: 9, Day: 30, URL: "https://x/dump_2026_09_30.xml"}, // month already complete → skip
	}
	from := time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := SelectDumps(all, from, to)
	if len(got) != 2 || got[0].Name() != "dump_2026_10_01.xml" || got[1].Name() != "dump_2026_09.xml" {
		t.Fatalf("%v", got)
	}
}

func TestScanFixture(t *testing.T) {
	f, err := os.Open("../rs/testdata/fragment.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	in := Input{ICO: []string{"27661989"}, DateFrom: "2026-08-01", DateTo: "2026-08-31"}
	from, to, err := in.Normalize(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var items []Item
	if _, err := ScanReader(f, func(c *rs.Contract) (bool, error) {
		if in.Match(c, from, to) {
			items = append(items, ToItem(c))
		}
		return true, nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("no match for the hospital IČO in the fixture")
	}
	for _, it := range items {
		if it.Authority.ICO != "27661989" && it.Supplier == nil {
			t.Errorf("unexpected %+v", it)
		}
		for _, p := range it.Parties {
			if p.ICO == "" {
				t.Error("party without IČO exported")
			}
		}
	}
}
