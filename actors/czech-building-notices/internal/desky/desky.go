// Package desky lists Czech official notice boards (úřední desky) published
// as open data under the OFN "Úřední desky" standard and parses them.
//
// The list comes from the national open-data catalogue (NKOD) SPARQL
// endpoint. Each board is one JSON-LD file; the parser is lenient because
// publishers differ in shapes (string vs {"cs": …}, list vs object).
package desky

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

const NKODSparql = "https://data.gov.cz/sparql"

// Board is one notice-board dataset from NKOD.
type Board struct {
	Dataset      string `json:"dataset"`
	Title        string `json:"title"`
	PublisherICO string `json:"publisherIco"`
	URL          string `json:"url"`
}

const boardsQuery = `PREFIX dcat: <http://www.w3.org/ns/dcat#>
PREFIX dcterms: <http://purl.org/dc/terms/>
SELECT ?ds ?title ?publisher ?dl WHERE {
  ?ds a dcat:Dataset ; dcterms:title ?title ; dcterms:publisher ?publisher ; dcat:distribution ?d .
  ?d dcterms:conformsTo ?c ; dcat:downloadURL ?dl .
  FILTER(CONTAINS(STR(?c), "ofn.gov.cz/úřední-desky") || CONTAINS(STR(?c), "ofn.gov.cz/%C3%BA%C5%99edn%C3%AD-desky"))
  FILTER(LANG(?title) = "cs")
}`

// ListBoards queries NKOD. Courts and prosecutors (kiosek.justice.cz, about
// 200 boards) never publish building notices and are skipped unless asked.
func ListBoards(ctx context.Context, hc *http.Client, ua string, includeCourts bool) ([]Board, error) {
	form := url.Values{"query": {boardsQuery}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, NKODSparql, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", ua)
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("NKOD: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("NKOD: HTTP %d", resp.StatusCode)
	}
	var r struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("NKOD: %w", err)
	}
	seen := map[string]bool{}
	var out []Board
	for _, b := range r.Results.Bindings {
		u := b["dl"].Value
		if u == "" || seen[u] {
			continue
		}
		if !includeCourts && strings.Contains(u, "kiosek.justice.cz") {
			continue
		}
		seen[u] = true
		pub := b["publisher"].Value
		out = append(out, Board{Dataset: b["ds"].Value, Title: strings.TrimSpace(b["title"].Value),
			PublisherICO: pub[strings.LastIndex(pub, "/")+1:], URL: u})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].URL < out[j].URL })
	return out, nil
}

// Fetch downloads a board. With etag/lastMod from the previous run it may
// return notModified=true and no body.
func Fetch(ctx context.Context, hc *http.Client, ua, u, etag, lastMod string) (body []byte, newETag, newLastMod string, notModified bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, "", "", false, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/ld+json, application/json;q=0.9, */*;q=0.1")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastMod != "" {
		req.Header.Set("If-Modified-Since", lastMod)
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, "", "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		return nil, etag, lastMod, true, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", false, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 80<<20))
	return b, resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"), false, err
}

// Doc is an attached document (usually a PDF of the notice).
type Doc struct {
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

// Info is one item on a board ("informace na úřední desce").
type Info struct {
	IRI           string    `json:"iri,omitempty"`
	URL           string    `json:"url,omitempty"`
	Title         string    `json:"title"`
	Description   string    `json:"description,omitempty"`
	Posted        time.Time `json:"posted"`
	RelevantUntil time.Time `json:"relevantUntil"` // zero = not specified
	CaseNumber    string    `json:"caseNumber,omitempty"`
	FileMark      string    `json:"fileMark,omitempty"`
	Agenda        []string  `json:"agenda,omitempty"`
	Department    string    `json:"department,omitempty"`
	Documents     []Doc     `json:"documents,omitempty"`
}

// Meta is the board header.
type Meta struct {
	OperatorICO string
	Page        string
}

// Parse reads an OFN notice board, tolerating the variants seen in the wild.
func Parse(body []byte) ([]Info, Meta, error) {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	var top any
	err := json.Unmarshal(body, &top)
	if err != nil {
		// Some boards put raw control characters (\r, \v) inside strings,
		// which JSON forbids; outside strings they are whitespace anyway.
		clean := bytes.Map(func(r rune) rune {
			if r < 0x20 {
				return ' '
			}
			return r
		}, body)
		err = json.Unmarshal(clean, &top)
	}
	if err != nil {
		return nil, Meta{}, fmt.Errorf("not JSON: %w", err)
	}
	var board map[string]any
	switch t := top.(type) {
	case map[string]any:
		board = t
	case []any:
		for _, x := range t {
			if m, ok := x.(map[string]any); ok && m["informace"] != nil {
				board = m
				break
			}
		}
	}
	if board == nil {
		return nil, Meta{}, fmt.Errorf("no board object")
	}
	var meta Meta
	if p, ok := board["provozovatel"].(map[string]any); ok {
		meta.OperatorICO = firstNonEmpty(str(p["ičo"]), str(p["identifikátor_ovm"]))
	}
	meta.Page = str(board["stránka"])
	raw, _ := board["informace"].([]any)
	out := make([]Info, 0, len(raw))
	for _, x := range raw {
		it, ok := x.(map[string]any)
		if !ok {
			continue
		}
		in := Info{
			IRI: str(it["iri"]), URL: str(it["url"]),
			Title:       clean(text(it["název"])),
			Description: clean(text(it["popis"])),
			Posted:      when(it["vyvěšení"]), RelevantUntil: when(it["relevantní_do"]),
			CaseNumber: strings.TrimSpace(str(it["číslo_jednací"])), FileMark: strings.TrimSpace(str(it["spisová_značka"])),
			Department: strings.TrimSpace(text(it["odbor"])),
		}
		for _, a := range list(it["agenda"]) {
			if s := strings.TrimSpace(text(a)); s != "" {
				in.Agenda = append(in.Agenda, s)
			}
		}
		for _, d := range list(it["dokument"]) {
			dm, ok := d.(map[string]any)
			if !ok {
				continue
			}
			if u := str(dm["url"]); u != "" {
				in.Documents = append(in.Documents, Doc{Name: strings.TrimSpace(text(dm["název"])), URL: u})
			}
		}
		if in.Title == "" && in.Description == "" {
			continue
		}
		out = append(out, in)
	}
	return out, meta, nil
}

// clean unescapes HTML entities some boards leave in titles (&quot;, &#8211;).
func clean(s string) string { return strings.TrimSpace(html.UnescapeString(s)) }

func str(v any) string {
	s, _ := v.(string)
	return s
}

// text reads "x", {"cs": "x"} or {"název": {"cs": "x"}} (agenda items).
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case map[string]any:
		if s, ok := t["cs"].(string); ok {
			return s
		}
		if n, ok := t["název"]; ok {
			return text(n)
		}
	}
	return ""
}

func list(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case nil:
		return nil
	default:
		return []any{t}
	}
}

// when reads {"datum": "2026-10-23"} or {"datum_a_čas": "…"}; anything else is zero.
func when(v any) time.Time {
	m, ok := v.(map[string]any)
	if !ok {
		return time.Time{}
	}
	if s := str(m["datum_a_čas"]); s != "" {
		for _, l := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if t, err := time.Parse(l, s); err == nil {
				return t
			}
		}
	}
	if s := str(m["datum"]); s != "" {
		if t, err := time.Parse(time.DateOnly, s[:min(10, len(s))]); err == nil {
			return t
		}
	}
	return time.Time{}
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}
