package leads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// ARES (ares.gov.cz) is the free public business register API; no key needed.
const aresBase = "https://ares.gov.cz/ekonomicke-subjekty-v-be/rest"

type ARES struct {
	HTTP *http.Client
	Base string
	last time.Time
}

func NewARES() *ARES {
	return &ARES{HTTP: &http.Client{Timeout: 20 * time.Second}, Base: aresBase}
}

type Subject struct {
	ICO       string `json:"ico"`
	Name      string `json:"obchodniJmeno"`
	LegalForm string `json:"pravniForma"`
	Founded   string `json:"datumVzniku"`
	Dissolved string `json:"datumZaniku"`
	Seat      struct {
		PSC     int    `json:"psc"`
		City    string `json:"nazevObce"`
		Address string `json:"textovaAdresa"`
	} `json:"sidlo"`
}

// OSVC: legal forms 10x are natural persons doing business.
func (s Subject) OSVC() bool { return strings.HasPrefix(s.LegalForm, "10") }

func (a *ARES) do(ctx context.Context, method, path string, body any, out any) (int, error) {
	// ARES allows ~500 requests/min; stay well below.
	if d := time.Until(a.last.Add(150 * time.Millisecond)); d > 0 {
		time.Sleep(d)
	}
	a.last = time.Now()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, a.Base+path, rd)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return 404, nil
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("ares %s: HTTP %d: %s", path, resp.StatusCode, b)
	}
	return 200, json.NewDecoder(resp.Body).Decode(out)
}

// Get returns the subject or nil when the IČO does not exist.
func (a *ARES) Get(ctx context.Context, ico string) (*Subject, error) {
	var s Subject
	code, err := a.do(ctx, http.MethodGet, "/ekonomicke-subjekty/"+ico, nil, &s)
	if err != nil || code == 404 {
		return nil, err
	}
	return &s, nil
}

// Search finds subjects by business name (ARES matches words, not substrings).
func (a *ARES) Search(ctx context.Context, name string, limit int) ([]Subject, error) {
	var out struct {
		Subjects []Subject `json:"ekonomickeSubjekty"`
	}
	code, err := a.do(ctx, http.MethodPost, "/ekonomicke-subjekty/vyhledat", map[string]any{"obchodniJmeno": name, "pocet": limit}, &out)
	if err != nil {
		if code == 400 { // too generic or invalid name: no match, not a failure
			return nil, nil
		}
		return nil, err
	}
	return out.Subjects, nil
}

// EmployeeCategory reads the RES employee-count category (ČSÚ code list) as a label.
func (a *ARES) EmployeeCategory(ctx context.Context, ico string) (string, error) {
	var out struct {
		Records []struct {
			Stats struct {
				Employees string `json:"kategoriePoctuPracovniku"`
			} `json:"statistickeUdaje"`
		} `json:"zaznamy"`
	}
	code, err := a.do(ctx, http.MethodGet, "/ekonomicke-subjekty-res/"+ico, nil, &out)
	if err != nil || code == 404 || len(out.Records) == 0 {
		return "", err
	}
	c := out.Records[0].Stats.Employees
	if l, ok := employeeLabels[c]; ok {
		return l, nil
	}
	return c, nil
}

// employeeLabels is ČSÚ code list 579 (KATPOECD), copied from LeadsRAG internal/res/codebooks/cis_579.csv.
var employeeLabels = map[string]string{
	"000": "Neuvedeno", "110": "Bez zaměstnanců", "120": "1 - 5 zaměstnanců", "130": "6 - 9 zaměstnanců",
	"210": "10 - 19 zaměstnanců", "220": "20 - 24 zaměstnanci", "230": "25 - 49 zaměstnanců", "240": "50 - 99 zaměstnanců",
	"310": "100 - 199 zaměstnanců", "320": "200 - 249 zaměstnanců", "330": "250 - 499 zaměstnanců", "340": "500 - 999 zaměstnanců",
	"410": "1000 - 1499 zaměstnanců", "420": "1500 - 1999 zaměstnanců", "430": "2000 - 2499 zaměstnanců",
	"440": "2500 - 2999 zaměstnanců", "450": "3000 - 3999 zaměstnanců", "460": "4000 - 4999 zaměstnanců",
	"470": "5000 - 9999 zaměstnanců", "510": "10 000 a více zaměstnanců",
}

// ValidICO checks the mod-11 control digit.
func ValidICO(s string) bool {
	if len(s) != 8 {
		return false
	}
	sum := 0
	for i := 0; i < 7; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
		sum += int(s[i]-'0') * (8 - i)
	}
	var c int
	switch r := sum % 11; r {
	case 0:
		c = 1
	case 1:
		c = 0
	default:
		c = 11 - r
	}
	return int(s[7]-'0') == c
}

var (
	reICO = regexp.MustCompile(`(?i)I[ČC]\s*O?\s*[:.]?\s*(?:CZ)?\s*(\d{2}\s?\d{3}\s?\d{3}|\d{8})`)
	rePSC = regexp.MustCompile(`\b(\d{3})\s?(\d{2})\b`)
)

// ICOsInText returns valid IČOs mentioned as "IČ"/"IČO" in the text, in order.
func ICOsInText(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range reICO.FindAllStringSubmatch(s, -1) {
		ico := strings.ReplaceAll(m[1], " ", "")
		if ValidICO(ico) && !seen[ico] {
			seen[ico] = true
			out = append(out, ico)
		}
	}
	return out
}

// PSC extracts the postal code from a Google address ("…, 153 00 Praha 16, Česko").
func PSC(addr string) int {
	m := rePSC.FindAllStringSubmatch(addr, -1)
	if len(m) == 0 {
		return 0
	}
	last := m[len(m)-1]
	n := 0
	fmt.Sscanf(last[1]+last[2], "%d", &n)
	return n
}

var titleCut = regexp.MustCompile(`\s+[-|–—]\s+|\s*\|\s*`)

// CleanTitle drops marketing suffixes Maps titles carry ("Fanl - marketingová agentura").
func CleanTitle(t string) string {
	parts := titleCut.Split(t, -1)
	return strings.TrimSpace(parts[0])
}
