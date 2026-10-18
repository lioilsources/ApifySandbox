// leads-normalize joins Google Maps places and website contacts (Apify JSONL
// from apify-run) with LeadsRAG exports, drops places outside their segment,
// resolves IČO (company website, then ARES) and writes leads/apify/leads.{csv,json}
// plus dropped.csv for review.
//
//	leads-normalize -places raw/maps.jsonl -contacts raw/contacts.jsonl \
//	    -registry ../LeadsRAG/export/leads_agentury.csv -registry ../LeadsRAG/export/leads_teren.csv
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/lioilsources/apifysandbox/leads"
)

type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(s string) error { *m = append(*m, s); return nil }

func readLines(path string) [][]byte {
	if path == "" {
		return nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	return bytes.Split(b, []byte("\n"))
}

// readRegistry indexes LeadsRAG export rows by NameKey and by IČO.
func readRegistry(paths []string) (byName, byICO map[string]leads.Registry) {
	byName, byICO = map[string]leads.Registry{}, map[string]leads.Registry{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			log.Fatal(err)
		}
		r := csv.NewReader(bufio.NewReader(f))
		r.FieldsPerRecord = -1
		head, err := r.Read()
		if err != nil {
			log.Fatal(err)
		}
		col := map[string]int{}
		for i, h := range head {
			col[strings.TrimPrefix(h, "\ufeff")] = i
		}
		for {
			rec, err := r.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				log.Fatal(err)
			}
			get := func(k string) string {
				if i, ok := col[k]; ok && i < len(rec) {
					return rec[i]
				}
				return ""
			}
			score, _ := strconv.ParseFloat(get("score"), 64)
			row := leads.Registry{ICO: get("ico"), Name: get("nazev"), Segment: get("segment"), City: get("mesto"), Size: get("velikost"), Score: score}
			if k := leads.NameKey(row.Name); k != "" {
				if prev, ok := byName[k]; !ok || prev.Score < row.Score {
					byName[k] = row
				}
			}
			if k := strings.TrimLeft(row.ICO, "0"); k != "" {
				if prev, ok := byICO[k]; !ok || prev.Score < row.Score {
					byICO[k] = row
				}
			}
		}
		f.Close()
	}
	return byName, byICO
}

func writeCSV(path string, head []string, rows [][]string) {
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	f.WriteString("\ufeff") // Excel opens UTF-8 with BOM correctly, like LeadsRAG exports
	w := csv.NewWriter(f)
	w.Write(head)
	w.WriteAll(rows)
	if err := w.Error(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	placesPath := flag.String("places", "", "Google Maps scraper JSONL")
	contactsPath := flag.String("contacts", "", "website contacts JSONL (optional)")
	outDir := flag.String("out", "leads/apify", "output directory")
	enrich := flag.Bool("enrich", true, "resolve IČO from company websites and ARES (free, network)")
	var registry multi
	flag.Var(&registry, "registry", "LeadsRAG export CSV (repeatable)")
	flag.Parse()
	if *placesPath == "" {
		log.Fatal("-places is required")
	}

	places, err := leads.DecodeJSONL[leads.Place](readLines(*placesPath))
	if err != nil {
		log.Fatal(err)
	}
	contacts, err := leads.DecodeJSONL[leads.Contacts](readLines(*contactsPath))
	if err != nil {
		log.Fatal(err)
	}
	byName, byICO := readRegistry(registry)
	out, kept, dropped, st := leads.Merge(places, contacts, byName)

	icoBy := map[string]int{}
	if *enrich {
		ctx := context.Background()
		ares, web := leads.NewARES(), leads.NewWeb()
		for i := range out {
			l := &out[i]
			if l.ICO == "" {
				s, how, err := leads.ResolveICO(ctx, ares, web, kept[i])
				if err != nil {
					log.Printf("%s: %v", l.Name, err)
				}
				if s != nil {
					l.ICO, l.ICOSource, l.ARESName, l.LegalForm, l.OSVC = s.ICO, how, s.Name, s.LegalForm, s.OSVC()
				}
			} else {
				l.ICOSource = "registr_smluv"
			}
			if l.ICO == "" {
				continue
			}
			icoBy[l.ICOSource]++
			if l.Employees == "" {
				if e, err := ares.EmployeeCategory(ctx, l.ICO); err == nil {
					l.Employees = e
				}
			}
		}
		st.JoinedRegistry += leads.JoinRegistryByICO(out, byICO)
	}

	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}
	jb, _ := json.MarshalIndent(out, "", " ")
	if err := os.WriteFile(filepath.Join(*outDir, "leads.json"), jb, 0o644); err != nil {
		log.Fatal(err)
	}
	var rows [][]string
	withICO, osvc := 0, 0
	for _, l := range out {
		if l.ICO != "" {
			withICO++
		}
		if l.OSVC {
			osvc++
		}
		rows = append(rows, []string{l.ICO, l.ICOSource, l.Name, l.ARESName, l.Segment, l.City, l.Address, l.Web,
			strings.Join(l.Emails, ";"), strings.Join(l.Phones, ";"), l.Category, strconv.FormatFloat(l.Rating, 'f', 1, 64),
			strconv.Itoa(l.Reviews), l.LegalForm, strconv.FormatBool(l.OSVC), l.Employees, l.LastActive,
			strconv.FormatFloat(l.LeadsScore, 'f', 2, 64), strings.Join(l.Sources, "+"), l.MapsURL})
	}
	writeCSV(filepath.Join(*outDir, "leads.csv"), []string{"ico", "ico_zdroj", "nazev", "nazev_ares", "segment", "mesto", "adresa", "web",
		"emaily", "telefony", "kategorie", "hodnoceni", "pocet_recenzi", "pravni_forma", "osvc", "zamestnanci_odhad",
		"posledni_aktivita", "leads_score", "zdroje", "maps_url"}, rows)
	var drows [][]string
	for _, d := range dropped {
		drows = append(drows, []string{d.Title, d.Segment, d.Category, d.Reason, d.MapsURL})
	}
	writeCSV(filepath.Join(*outDir, "dropped.csv"), []string{"nazev", "segment", "kategorie", "duvod", "maps_url"}, drows)

	pct := func(n int) float64 {
		if st.Leads == 0 {
			return 0
		}
		return 100 * float64(n) / float64(st.Leads)
	}
	fmt.Printf("places %d, duplicates %d, filtered %d, leads %d\n", st.Places, st.Duplicates, st.Filtered, st.Leads)
	fmt.Printf("contact: email %d (%.0f %%), phone %d (%.0f %%), any %d (%.0f %%)\n",
		st.WithEmail, pct(st.WithEmail), st.WithPhone, pct(st.WithPhone), st.WithAnyContact, pct(st.WithAnyContact))
	fmt.Printf("IČO %d (%.0f %%) by %v, OSVČ %d, joined Registr smluv %d\n", withICO, pct(withICO), icoBy, osvc, st.JoinedRegistry)
}
