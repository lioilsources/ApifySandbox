package ruian

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
)

//go:embed codebook/codebooks.json
var codebooksJSON []byte

// CodeEntry is one row of an official ČÚZK codebook.
type CodeEntry struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	ValidTo string `json:"validTo,omitempty"`
}

// Codebook names, as used by Lookup and ListCodebooks.
const (
	CBDruhPozemku          = "druhPozemku"
	CBZpusobVyuzitiPozemku = "zpusobVyuzitiPozemku"
	CBDruhCislovani        = "druhCislovani"
	CBTypStavebnihoObjektu = "typStavebnihoObjektu"
	CBZpusobVyuzitiObjektu = "zpusobVyuzitiObjektu"
	CBDruhKonstrukce       = "druhKonstrukce"
	CBZpusobVytapeni       = "zpusobVytapeni"
	CBPripojeniKanalizace  = "pripojeniKanalizace"
	CBPripojeniPlynu       = "pripojeniPlynu"
	CBPripojeniVody        = "pripojeniVody"
	CBVybaveniVytahem      = "vybaveniVytahem"
	CBTypUUP               = "typUUP"
	CBStatusObce           = "statusObce"
)

type codebookFile struct {
	FetchedAt string `json:"fetchedAt"`
	Books     map[string]struct {
		Source  string      `json:"source"`
		Entries []CodeEntry `json:"entries"`
	} `json:"books"`
}

var (
	codebooks     = map[string]map[int]CodeEntry{}
	codebookList  = map[string][]CodeEntry{}
	codebookSrc   = map[string]string{}
	CodebooksDate string // when ruian-codebooks fetched them
)

func init() {
	var f codebookFile
	if err := json.Unmarshal(codebooksJSON, &f); err != nil {
		panic("ruian: bad embedded codebooks: " + err.Error())
	}
	CodebooksDate = f.FetchedAt
	for name, b := range f.Books {
		m := make(map[int]CodeEntry, len(b.Entries))
		for _, e := range b.Entries {
			m[e.Code] = e
		}
		codebooks[name] = m
		codebookList[name] = b.Entries
		codebookSrc[name] = b.Source
	}
}

// Lookup translates a code to its Czech name. Unknown codes come back as
// "neznámý (N)" so callers never print a bare number as if it were a value.
func Lookup(book string, code int) string {
	if e, ok := codebooks[book][code]; ok {
		return e.Name
	}
	return fmt.Sprintf("neznámý (%d)", code)
}

// lookupPtr is Lookup for optional codes; nil and the ČÚZK placeholders
// "Nedefinováno"/"Nezjištěno" mean "no data" and return nil.
func lookupPtr(book string, code *int) *string {
	if code == nil {
		return nil
	}
	e, ok := codebooks[book][*code]
	if ok && (e.Name == "Nedefinováno" || e.Name == "Nezjištěno") {
		return nil
	}
	s := Lookup(book, *code)
	return &s
}

// Codebook is one codebook for list_codebooks.
type Codebook struct {
	Name    string      `json:"name"`
	Source  string      `json:"source"`
	Entries []CodeEntry `json:"entries"`
}

// ListCodebooks returns all embedded codebooks sorted by name.
func ListCodebooks() []Codebook {
	out := make([]Codebook, 0, len(codebookList))
	for n, es := range codebookList {
		out = append(out, Codebook{Name: n, Source: codebookSrc[n], Entries: es})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
