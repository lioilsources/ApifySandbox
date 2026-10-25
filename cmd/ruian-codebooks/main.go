// ruian-codebooks downloads the official ČÚZK codebooks (CSV in Windows-1250
// inside zip files) and writes ruian/codebook/codebooks.json, which the ruian
// package embeds. Run it when ČÚZK announces a codebook change:
//
//	go run ./cmd/ruian-codebooks
package main

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

const base = "https://services.cuzk.cz/sestavy/cis/"

// name in codebooks.json → ČÚZK file
var sources = map[string]string{
	"druhPozemku":          "SC_D_POZEMKU",
	"zpusobVyuzitiPozemku": "SC_ZP_VYUZITI_POZ",
	"druhCislovani":        "CS_DRUH_CISLOVANI_PARCEL",
	"typStavebnihoObjektu": "CS_TYP_STAVEBNIHO_OBJEKTU",
	"zpusobVyuzitiObjektu": "CE_ZPUSOB_VYUZITI_OBJEKTU",
	"druhKonstrukce":       "CE_DRUH_KONSTRUKCE",
	"zpusobVytapeni":       "CE_ZPUSOB_VYTAPENI",
	"pripojeniKanalizace":  "CE_PRIPOJENI_KANAL",
	"pripojeniPlynu":       "CE_PRIPOJENI_PLYNU",
	"pripojeniVody":        "CE_PRIPOJENI_VODY",
	"vybaveniVytahem":      "CE_VYBAVENI_VYTAHEM",
	"typUUP":               "CE_TYP_UUP",
	"subtypUUP":            "CE_SUBTYP_UUP",
	"statusObce":           "CS_STATUS_OBCE",
}

type entry struct {
	Code    int    `json:"code"`
	Name    string `json:"name"`
	ValidTo string `json:"validTo,omitempty"` // empty = still valid
}

type book struct {
	Source  string  `json:"source"`
	Entries []entry `json:"entries"`
}

func main() {
	out := map[string]any{}
	names := make([]string, 0, len(sources))
	for n := range sources {
		names = append(names, n)
	}
	sort.Strings(names)
	hc := &http.Client{Timeout: time.Minute}
	books := map[string]book{}
	for _, n := range names {
		b, err := fetch(hc, sources[n])
		if err != nil {
			log.Fatalf("%s: %v", n, err)
		}
		books[n] = b
		fmt.Printf("%-22s %3d entries\n", n, len(b.Entries))
	}
	out["fetchedAt"] = time.Now().UTC().Format(time.DateOnly)
	out["books"] = books
	j, _ := json.MarshalIndent(out, "", " ")
	if err := os.WriteFile("ruian/codebook/codebooks.json", append(j, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

func fetch(hc *http.Client, file string) (book, error) {
	resp, err := hc.Get(base + file + ".zip")
	if err != nil {
		return book{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return book{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return book{}, err
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return book{}, err
	}
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), ".csv") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return book{}, err
		}
		r := csv.NewReader(charmap.Windows1250.NewDecoder().Reader(rc))
		r.Comma = ';'
		r.FieldsPerRecord = -1
		r.LazyQuotes = true
		rows, err := r.ReadAll()
		rc.Close()
		if err != nil {
			return book{}, err
		}
		return parse(file, rows)
	}
	return book{}, fmt.Errorf("no CSV in zip")
}

func parse(file string, rows [][]string) (book, error) {
	if len(rows) < 2 {
		return book{}, fmt.Errorf("empty")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	ki, ok1 := col["KOD"]
	ni, ok2 := col["NAZEV"]
	if !ok1 || !ok2 {
		return book{}, fmt.Errorf("header %v", rows[0])
	}
	end := -1
	for _, h := range []string{"KONEC_PLATNOSTI", "PLATNOST_DO"} {
		if i, ok := col[h]; ok {
			end = i
		}
	}
	b := book{Source: base + file + ".zip"}
	for _, r := range rows[1:] {
		if len(r) <= ni || len(r) <= ki {
			continue
		}
		code, err := strconv.Atoi(strings.TrimSpace(r[ki]))
		if err != nil {
			continue
		}
		e := entry{Code: code, Name: strings.TrimSpace(r[ni])}
		if end >= 0 && end < len(r) {
			if v := strings.TrimSpace(r[end]); v != "" {
				e.ValidTo = strings.Fields(v)[0]
			}
		}
		b.Entries = append(b.Entries, e)
	}
	return b, nil
}
