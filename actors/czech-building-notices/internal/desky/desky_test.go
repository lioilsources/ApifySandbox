package desky

import (
	"os"
	"testing"
	"time"
)

func TestParseSample(t *testing.T) {
	b, err := os.ReadFile("../../testdata/board_sample.json")
	if err != nil {
		t.Fatal(err)
	}
	items, meta, err := Parse(b)
	if err != nil {
		t.Fatal(err)
	}
	if meta.OperatorICO != "00283509" || len(items) != 2 {
		t.Fatalf("meta %+v, %d items", meta, len(items))
	}
	a := items[0]
	if a.CaseNumber != "MUPO-149053/2026/SU/TRM" || a.Agenda[0] != "Veřejné vyhlášky" || len(a.Documents) != 1 ||
		!a.Posted.Equal(time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC)) || a.RelevantUntil.IsZero() {
		t.Errorf("first item %+v", a)
	}
	c := items[1]
	if c.Title != `Záměr prodat "část pozemku" p. č. 338/2 v k. ú. Hrutov` || !c.RelevantUntil.IsZero() ||
		c.Agenda[0] != "Majetek" || c.Department != "Odbor majetku" || c.Posted.IsZero() {
		t.Errorf("second item %+v", c)
	}
}

func TestParseTopLevelList(t *testing.T) {
	items, _, err := Parse([]byte(`[{"@context":"x"},{"informace":[{"název":"A"}]}]`))
	if err != nil || len(items) != 1 || items[0].Title != "A" {
		t.Errorf("%v %v", items, err)
	}
	if _, _, err := Parse([]byte(`<html>`)); err == nil {
		t.Error("HTML must fail")
	}
}
