// Package search streams Registr smluv open-data dumps and yields contracts
// matching the Actor input. Nothing is stored on disk: dumps are parsed while
// downloading, so the Actor fits in 512 MB.
package search

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
	_ "time/tzdata" // alpine image ships without zoneinfo

	"github.com/lioilsources/czech-public-contracts/internal/rs"
)

// prague is the registry's time zone; publication dates are compared as local days.
var prague, _ = time.LoadLocation("Europe/Prague")

type Input struct {
	ICO       []string `json:"ico"`      // authority or counterparty IČO
	Name      string   `json:"name"`     // substring of any party name
	Keywords  []string `json:"keywords"` // any of them in the subject
	DateFrom  string   `json:"dateFrom"` // YYYY-MM-DD, publication date
	DateTo    string   `json:"dateTo"`
	MinValue  float64  `json:"minValueCzk"`
	Limit     int      `json:"limit"`
	OnlyValid *bool    `json:"onlyValid"`
}

type Org struct {
	ICO  string `json:"ico"`
	Name string `json:"name"`
}

// Item is one dataset row. Private persons appear only as an empty slot (no name, no address).
type Item struct {
	Source      string   `json:"source"`
	ContractID  string   `json:"contractId"`
	VersionID   int64    `json:"versionId"`
	URL         string   `json:"url"`
	Valid       bool     `json:"valid"`
	PublishedAt string   `json:"publishedAt,omitempty"`
	SignedAt    string   `json:"signedAt,omitempty"`
	Subject     string   `json:"subject"`
	Value       *float64 `json:"value"`
	Currency    string   `json:"currency,omitempty"`
	ValueCZK    *float64 `json:"valueCzk"`
	Authority   Org      `json:"authority"`
	Supplier    *Org     `json:"supplier"`
	Reversed    bool     `json:"supplierRoleReversed"`
	Parties     []Org    `json:"parties"`
	Attachments []string `json:"attachments,omitempty"`
}

const dateLayout = "2006-01-02"

// Normalize validates the input and fills defaults (last 30 days, limit 100).
func (in *Input) Normalize(now time.Time) (from, to time.Time, err error) {
	to = now
	if in.DateTo != "" {
		if to, err = time.Parse(dateLayout, in.DateTo); err != nil {
			return from, to, fmt.Errorf("dateTo: %w", err)
		}
	}
	from = to.AddDate(0, 0, -30)
	if in.DateFrom != "" {
		if from, err = time.Parse(dateLayout, in.DateFrom); err != nil {
			return from, to, fmt.Errorf("dateFrom: %w", err)
		}
	}
	if from.After(to) {
		return from, to, errors.New("dateFrom is after dateTo")
	}
	if from.Before(time.Date(2016, 7, 1, 0, 0, 0, 0, time.UTC)) {
		from = time.Date(2016, 7, 1, 0, 0, 0, 0, time.UTC) // registry starts 1 July 2016
	}
	if in.Limit <= 0 {
		in.Limit = 100
	}
	for i, ico := range in.ICO {
		in.ICO[i] = strings.TrimLeft(strings.TrimSpace(ico), "0")
	}
	for i, k := range in.Keywords {
		in.Keywords[i] = strings.ToLower(strings.TrimSpace(k))
	}
	in.Name = strings.ToLower(strings.TrimSpace(in.Name))
	return from, to, nil
}

// SelectDumps picks monthly dumps for finished months and daily dumps otherwise,
// newest first so a limited run returns the most recent contracts.
func SelectDumps(all []rs.Dump, from, to time.Time) []rs.Dump {
	monthKey := func(d rs.Dump) int { return d.Year*100 + d.Month }
	complete := map[int]bool{}
	for _, d := range all {
		if d.Monthly() && d.Complete == 1 {
			complete[monthKey(d)] = true
		}
	}
	fromM := from.Year()*100 + int(from.Month())
	toM := to.Year()*100 + int(to.Month())
	var out []rs.Dump
	for _, d := range all {
		mk := monthKey(d)
		if mk < fromM || mk > toM {
			continue
		}
		if d.Monthly() {
			if d.Complete == 1 {
				out = append(out, d)
			}
			continue
		}
		day := d.Date()
		if !complete[mk] && !day.Before(truncDay(from)) && !day.After(truncDay(to)) {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date().After(out[j].Date()) })
	return out
}

func truncDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func normICO(s string) string { return strings.TrimLeft(s, "0") }

// Match reports whether a normalized contract satisfies the filters.
func (in *Input) Match(c *rs.Contract, from, to time.Time) bool {
	if in.OnlyValid == nil || *in.OnlyValid {
		if !c.Valid {
			return false
		}
	}
	if c.PublishedAt != nil {
		p := truncDay(c.PublishedAt.In(prague))
		if p.Before(truncDay(from)) || p.After(truncDay(to)) {
			return false
		}
	}
	if in.MinValue > 0 && (c.ValueCZK == nil || *c.ValueCZK < in.MinValue) {
		return false
	}
	if len(in.ICO) > 0 {
		ok := false
		for _, want := range in.ICO {
			if normICO(c.Authority.ICO) == want {
				ok = true
			}
			for _, p := range c.Parties {
				if p.ICO != "" && normICO(p.ICO) == want {
					ok = true
				}
			}
		}
		if !ok {
			return false
		}
	}
	if in.Name != "" {
		ok := strings.Contains(strings.ToLower(c.Authority.Name), in.Name)
		for _, p := range c.Parties {
			ok = ok || (p.Name != "" && strings.Contains(strings.ToLower(p.Name), in.Name))
		}
		if !ok {
			return false
		}
	}
	if len(in.Keywords) > 0 {
		subj := strings.ToLower(c.Subject)
		ok := false
		for _, k := range in.Keywords {
			ok = ok || (k != "" && strings.Contains(subj, k))
		}
		if !ok {
			return false
		}
	}
	return true
}

func ToItem(c *rs.Contract) Item {
	it := Item{Source: "registr_smluv", ContractID: c.ID, VersionID: c.VersionID, URL: c.URL, Valid: c.Valid,
		Subject: c.Subject, Value: c.Value, Currency: c.Currency, ValueCZK: c.ValueCZK,
		Authority: Org{ICO: c.Authority.ICO, Name: c.Authority.Name}, Reversed: c.Reversed, Attachments: c.Attachments}
	if c.PublishedAt != nil {
		it.PublishedAt = c.PublishedAt.Format(time.RFC3339)
	}
	if c.SignedAt != nil {
		it.SignedAt = c.SignedAt.Format(dateLayout)
	}
	if c.Supplier != nil {
		it.Supplier = &Org{ICO: c.Supplier.ICO, Name: c.Supplier.Name}
	}
	for _, p := range c.Parties {
		if p.ICO != "" { // parties without IČO are private persons or unknown: never exported
			it.Parties = append(it.Parties, Org{ICO: p.ICO, Name: p.Name})
		}
	}
	return it
}

// Scan streams the selected dumps and calls emit for each match until emit
// returns false, the limit is reached or ctx ends. It returns the dumps read.
func Scan(ctx context.Context, hc *http.Client, in Input, now time.Time, progress func(string), emit func(Item) (bool, error)) (int, error) {
	from, to, err := in.Normalize(now)
	if err != nil {
		return 0, err
	}
	idx, err := rs.FetchIndex(ctx, hc)
	if err != nil {
		return 0, err
	}
	dumps := SelectDumps(idx, from, to)
	seen := map[string]bool{}
	n := 0
	for i, d := range dumps {
		if progress != nil {
			progress(fmt.Sprintf("dump %d/%d %s (%d MB)", i+1, len(dumps), d.Name(), d.Size>>20))
		}
		more, err := scanDump(ctx, hc, d, func(c *rs.Contract) (bool, error) {
			if seen[c.ID] || !in.Match(c, from, to) {
				return true, nil
			}
			seen[c.ID] = true
			ok, err := emit(ToItem(c))
			if err != nil || !ok {
				return false, err
			}
			n++
			return n < in.Limit, nil
		})
		if err != nil {
			return i + 1, err
		}
		if !more {
			return i + 1, nil
		}
	}
	return len(dumps), nil
}

func scanDump(ctx context.Context, hc *http.Client, d rs.Dump, fn func(*rs.Contract) (bool, error)) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.URL, nil)
	if err != nil {
		return false, err
	}
	resp, err := hc.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("%s: HTTP %d", d.Name(), resp.StatusCode)
	}
	return ScanReader(resp.Body, fn)
}

// ScanReader parses one dump stream; exported for tests on fixtures.
func ScanReader(r io.Reader, fn func(*rs.Contract) (bool, error)) (bool, error) {
	dec := rs.NewDecoder(r)
	for {
		rec, err := dec.Next()
		if err == io.EOF {
			return true, nil
		}
		if err != nil {
			return false, err
		}
		c, err := rec.Normalize()
		if err != nil {
			continue // one malformed record must not kill a monthly dump
		}
		more, err := fn(c)
		if err != nil || !more {
			return false, err
		}
	}
}
