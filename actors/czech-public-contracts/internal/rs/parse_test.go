package rs

import (
	"io"
	"os"
	"testing"
)

func load(t *testing.T) map[string][]*Contract {
	t.Helper()
	f, err := os.Open("testdata/fragment.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]*Contract{}
	d := NewDecoder(f)
	for {
		r, err := d.Next()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		c, err := r.Normalize()
		if err != nil {
			t.Fatal(err)
		}
		out[c.ID] = append(out[c.ID], c)
	}
}

func TestSimple(t *testing.T) {
	c := load(t)["36628570"][0]
	if c.Authority.ICO != "27661989" || c.Authority.Name != "Krajská nemocnice T. Bati, a. s." {
		t.Errorf("authority = %+v", c.Authority)
	}
	if c.Supplier == nil || c.Supplier.ICO != "45359326" {
		t.Fatalf("supplier = %+v", c.Supplier)
	}
	if c.ValueCZK == nil || *c.ValueCZK != 3097.13 || c.Currency != "CZK" {
		t.Errorf("value = %v %s", c.ValueCZK, c.Currency)
	}
	if c.SignedAt == nil || c.SignedAt.Format("2006-01-02") != "2026-07-31" {
		t.Errorf("signed = %v", c.SignedAt)
	}
	if !c.Valid || c.VersionID != 38971462 || c.URL != "https://smlouvy.gov.cz/smlouva/38971462" {
		t.Errorf("meta = %v %d %s", c.Valid, c.VersionID, c.URL)
	}
}

// The publisher is listed among the parties too; the supplier is the recipient, not the publisher.
func TestMultiPartySkipsPublisher(t *testing.T) {
	c := load(t)["36630218"][0]
	if c.Supplier == nil || c.Supplier.ICO != "25753487" {
		t.Fatalf("supplier = %+v", c.Supplier)
	}
	if c.Reversed {
		t.Error("not reversed: the publisher is not the recipient")
	}
	if len(c.Parties) != 3 || c.Value != nil {
		t.Errorf("parties=%d value=%v", len(c.Parties), c.Value)
	}
}

func TestForeignCurrencyNoICO(t *testing.T) {
	c := load(t)["36632310"][0]
	if c.Supplier != nil {
		t.Errorf("foreign party without IČO must not be a supplier: %+v", c.Supplier)
	}
	if c.Currency == "CZK" || c.Value == nil || c.ValueCZK != nil {
		t.Errorf("value = %v %s czk=%v", c.Value, c.Currency, c.ValueCZK)
	}
}

func TestVersionsAndValidity(t *testing.T) {
	vs := load(t)["36630234"]
	if len(vs) != 3 {
		t.Fatalf("versions = %d", len(vs))
	}
	valid := 0
	for _, c := range vs {
		if c.Valid {
			valid++
		}
	}
	if valid != 1 {
		t.Errorf("valid versions = %d, want 1", valid)
	}
}

// Kreativní Praha publishes and receives money; the private person has no IČO and no stored name.
func TestReversedAndPersonalData(t *testing.T) {
	c := load(t)["36630994"][0]
	if !c.Reversed {
		t.Error("want reversed")
	}
	if c.Supplier != nil {
		t.Errorf("supplier = %+v", c.Supplier)
	}
	for _, p := range c.Parties {
		if p.ICO == "" && p.Name != "" {
			t.Errorf("personal name stored: %q", p.Name)
		}
	}
}

func TestNormICO(t *testing.T) {
	for in, want := range map[string]string{"07835256            ": "07835256", "216275": "00216275", "CZ123": "", "123456789": ""} {
		if got := normICO(in); got != want {
			t.Errorf("normICO(%q) = %q, want %q", in, got, want)
		}
	}
}
