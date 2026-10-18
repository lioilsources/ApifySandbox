// Copied from github.com/lioilsources/leadscout internal/rs (LeadsRAG); keep in sync.
// Package rs reads Registr smluv XML dumps (ISRS 1.2) and normalizes records
// into contracts the rest of the pipeline stores.
package rs

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Party is one side of a contract as published.
type Party struct {
	Name      string `xml:"nazev"`
	ICO       string `xml:"ico"`
	Address   string `xml:"adresa"`
	DataBox   string `xml:"datovaSchranka"`
	Recipient string `xml:"prijemce"`     // "1" = receives payment
	Kind      string `xml:"identifikace"` // PO|PFO|FO|OVM|ZPO|ZFO, filled only on newer records
	Unit      string `xml:"utvar"`
	Payer     string `xml:"platce"`
}

// Record is one <zaznam> as it appears in the dump.
type Record struct {
	ContractID string `xml:"identifikator>idSmlouvy"`
	VersionID  string `xml:"identifikator>idVerze"`
	URL        string `xml:"odkaz"`
	Published  string `xml:"casZverejneni"`
	Valid      string `xml:"platnyZaznam"`
	Smlouva    struct {
		Subject      Party   `xml:"subjekt"`
		Parties      []Party `xml:"smluvniStrana"`
		Predmet      string  `xml:"predmet"`
		Signed       string  `xml:"datumUzavreni"`
		Number       string  `xml:"cisloSmlouvy"`
		TenderNumber string  `xml:"evidencniCisloZakazky"`
		Linked       string  `xml:"navazanyZaznam"`
		ValueNoVAT   string  `xml:"hodnotaBezDph"`
		ValueVAT     string  `xml:"hodnotaVcetneDph"`
		NoPrice      string  `xml:"cenaNeuvedena"`
		Foreign      struct {
			Value    string `xml:"hodnota"`
			Currency string `xml:"mena"`
		} `xml:"ciziMena"`
	} `xml:"smlouva"`
	Attachments []struct {
		Name string `xml:"nazevSouboru"`
		URL  string `xml:"odkaz"`
	} `xml:"prilohy>priloha"`
}

// Decoder streams records from a dump without loading it into memory.
type Decoder struct {
	dec *xml.Decoder
}

func NewDecoder(r io.Reader) *Decoder { return &Decoder{dec: xml.NewDecoder(r)} }

// Next returns the next record, or io.EOF when the dump is exhausted.
func (d *Decoder) Next() (*Record, error) {
	for {
		tok, err := d.dec.Token()
		if err != nil {
			return nil, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok || se.Name.Local != "zaznam" {
			continue
		}
		var r Record
		if err := d.dec.DecodeElement(&r, &se); err != nil {
			return nil, fmt.Errorf("decode zaznam: %w", err)
		}
		return &r, nil
	}
}

// Org is an organization reference extracted from a contract.
type Org struct {
	ICO     string
	Name    string
	Address string
	Kind    string
}

// Contract is the normalized form stored in Postgres.
type Contract struct {
	ID          string
	VersionID   int64
	Valid       bool
	Authority   Org
	Supplier    *Org // nil when no counterparty has an IČO
	Reversed    bool // the publishing authority is the one receiving payment
	Parties     []Org
	Subject     string
	ValueCZK    *float64
	Value       *float64
	Currency    string
	SignedAt    *time.Time
	PublishedAt *time.Time
	URL         string
	Attachments []string
}

// Normalize picks the supplier, parses values and dates and drops personal data
// (names of private persons without IČO, the approver's name).
func (r *Record) Normalize() (*Contract, error) {
	ver, err := strconv.ParseInt(strings.TrimSpace(r.VersionID), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("contract %s: bad idVerze %q", r.ContractID, r.VersionID)
	}
	s := &r.Smlouva
	c := &Contract{
		ID:        strings.TrimSpace(r.ContractID),
		VersionID: ver,
		Valid:     strings.TrimSpace(r.Valid) == "1",
		Authority: toOrg(s.Subject),
		Subject:   cleanText(s.Predmet),
		URL:       strings.TrimSpace(r.URL),
	}
	authICO := c.Authority.ICO

	var recipient, firstWithICO *Org
	for _, p := range s.Parties {
		o := toOrg(p)
		if o.ICO == "" && (o.Kind == "FO" || o.Kind == "ZFO" || o.Kind == "") {
			// Private person or unknown without IČO: keep the slot, not the name.
			o.Name = ""
			o.Address = ""
		}
		c.Parties = append(c.Parties, o)
		if o.ICO != "" && o.ICO == authICO {
			if strings.TrimSpace(p.Recipient) == "1" {
				c.Reversed = true
			}
			continue
		}
		oc := o
		if strings.TrimSpace(p.Recipient) == "1" && o.ICO != "" && recipient == nil {
			recipient = &oc
		}
		if o.ICO != "" && firstWithICO == nil {
			firstWithICO = &oc
		}
	}
	if recipient != nil {
		c.Supplier = recipient
	} else {
		c.Supplier = firstWithICO
	}

	switch {
	case s.ValueNoVAT != "":
		c.Value, c.Currency = parseNum(s.ValueNoVAT), "CZK"
	case s.ValueVAT != "":
		c.Value, c.Currency = parseNum(s.ValueVAT), "CZK"
	case s.Foreign.Value != "":
		c.Value, c.Currency = parseNum(s.Foreign.Value), strings.ToUpper(strings.TrimSpace(s.Foreign.Currency))
	}
	if c.Currency == "CZK" {
		c.ValueCZK = c.Value
	}
	if t, err := time.Parse("2006-01-02", strings.TrimSpace(s.Signed)); err == nil {
		c.SignedAt = &t
	}
	if t, err := time.Parse(time.RFC3339, strings.TrimSpace(r.Published)); err == nil {
		c.PublishedAt = &t
	}
	for _, a := range r.Attachments {
		if u := strings.TrimSpace(a.URL); u != "" {
			c.Attachments = append(c.Attachments, u)
		}
	}
	return c, nil
}

func toOrg(p Party) Org {
	return Org{
		ICO:     normICO(p.ICO),
		Name:    cleanText(p.Name),
		Address: cleanText(p.Address),
		Kind:    strings.TrimSpace(p.Kind),
	}
}

// normICO keeps 8-digit Czech IČO (left-padded); anything else is treated as missing.
func normICO(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 8 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return strings.Repeat("0", 8-len(s)) + s
}

func parseNum(s string) *float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	return &f
}

// cleanText collapses whitespace and undoes the double-escaped entities some publishers send (&amp;quot;).
var entityFix = strings.NewReplacer("&quot;", `"`, "&amp;", "&", "&apos;", "'", "&lt;", "<", "&gt;", ">")

func cleanText(s string) string {
	return strings.Join(strings.Fields(entityFix.Replace(s)), " ")
}
