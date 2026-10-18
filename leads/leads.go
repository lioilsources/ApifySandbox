// Package leads merges Apify output (Google Maps places + website contacts)
// with LeadsRAG leads (Registr smluv) into one lead model (APIFY_PLAN Fáze 1).
package leads

import (
	"encoding/json"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Lead extends the LeadsRAG export columns (ico, nazev, segment, mesto) with contacts.
// Only company-level contacts are kept; reviewer names, owner names and photos are dropped.
type Lead struct {
	ICO        string   `json:"ico,omitempty"`
	ICOSource  string   `json:"ico_zdroj,omitempty"` // web | ares_name_psc | ares_name
	ARESName   string   `json:"nazev_ares,omitempty"`
	LegalForm  string   `json:"pravni_forma,omitempty"` // ARES code, 112 = s.r.o., 121 = a.s.
	OSVC       bool     `json:"osvc,omitempty"`
	Name       string   `json:"nazev"`
	Segment    string   `json:"segment"`
	City       string   `json:"mesto"`
	Address    string   `json:"adresa,omitempty"`
	Web        string   `json:"web,omitempty"`
	Emails     []string `json:"emaily,omitempty"`
	Phones     []string `json:"telefony,omitempty"`
	Category   string   `json:"kategorie,omitempty"`
	Rating     float64  `json:"hodnoceni,omitempty"`
	Reviews    int      `json:"pocet_recenzi,omitempty"`
	Employees  string   `json:"zamestnanci_odhad,omitempty"` // RES velikost from LeadsRAG when joined, else empty
	LastActive string   `json:"posledni_aktivita,omitempty"` // newest date found on the website
	MapsURL    string   `json:"maps_url,omitempty"`
	Sources    []string `json:"zdroje"` // google_maps, website, registr_smluv
	LeadsScore float64  `json:"leads_score,omitempty"`
}

// Place is the subset of compass/crawler-google-places output used.
type Place struct {
	Title        string   `json:"title"`
	CategoryName string   `json:"categoryName"`
	Address      string   `json:"address"`
	City         string   `json:"city"`
	Website      string   `json:"website"`
	Categories   []string `json:"categories"`
	Closed       bool     `json:"permanentlyClosed"`
	Phone        string   `json:"phone"`
	TotalScore   float64  `json:"totalScore"`
	ReviewsCount int      `json:"reviewsCount"`
	URL          string   `json:"url"`
	PlaceID      string   `json:"placeId"`
	Emails       []string `json:"emails"` // filled when the Actor's contact enrichment is on
	SearchString string   `json:"searchString"`
}

// Contacts is the subset of a contact-details / website crawler item.
type Contacts struct {
	Domain     string   `json:"domain"`
	URL        string   `json:"url"`
	Emails     []string `json:"emails"`
	Phones     []string `json:"phones"`
	LastActive string   `json:"lastActive"`
}

var segmentByQuery = []struct{ kw, seg string }{
	{"agentur", "agentury"}, {"marketing", "agentury"}, {"reklam", "agentury"}, {"pr ", "agentury"},
	{"game", "hry"}, {"herní", "hry"}, {"studio", "hry"},
	{"e-shop", "eshopy"}, {"eshop", "eshopy"}, {"internetový obchod", "eshopy"},
	{"stavebn", "teren"}, {"montáž", "teren"}, {"úklid", "teren"}, {"servis", "teren"}, {"instalat", "teren"},
}

// Segment guesses the olin.now/business segment from the search query and category.
func Segment(p Place) string {
	s := strings.ToLower(p.SearchString + " " + p.CategoryName)
	for _, x := range segmentByQuery {
		if strings.Contains(s, x.kw) {
			return x.seg
		}
	}
	return "none"
}

// Domain returns the bare host of a website ("www.example.cz/x" → "example.cz").
func Domain(web string) string {
	if web == "" {
		return ""
	}
	if !strings.Contains(web, "://") {
		web = "https://" + web
	}
	u, err := url.Parse(web)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

var legalSuffix = regexp.MustCompile(`(?i)[,\s]+(s\.?\s?r\.?\s?o\.?|spol\.?\s?s\s?r\.?\s?o\.?|a\.?\s?s\.?|v\.?\s?o\.?\s?s\.?|k\.?\s?s\.?|z\.?\s?s\.?|o\.?\s?p\.?\s?s\.?)\.?\s*$`)

// NameKey normalizes a company name for joining Maps titles with registry names.
func NameKey(s string) string {
	s = legalSuffix.ReplaceAllString(strings.TrimSpace(s), "")
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var reEmail = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[a-z]{2,}$`)

// personalEmail flags addresses that look like a person's private mailbox; we keep only company ones.
func personalEmail(e string) bool {
	for _, d := range []string{"@gmail.", "@seznam.cz", "@email.cz", "@centrum.cz", "@post.cz", "@yahoo.", "@outlook.", "@hotmail.", "@icloud."} {
		if strings.Contains(e, d) {
			return true
		}
	}
	return false
}

func cleanEmails(in []string, domain string) []string {
	set := map[string]bool{}
	for _, e := range in {
		e = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(e, "mailto:")))
		if !reEmail.MatchString(e) || personalEmail(e) || strings.HasSuffix(e, ".png") || strings.HasSuffix(e, ".jpg") {
			continue
		}
		set[e] = true
	}
	return sortedKeys(set)
}

func cleanPhones(in []string) []string {
	set := map[string]bool{}
	for _, p := range in {
		var b strings.Builder
		for _, r := range p {
			if unicode.IsDigit(r) || (r == '+' && b.Len() == 0) {
				b.WriteRune(r)
			}
		}
		s := b.String()
		if len(s) == 9 {
			s = "+420" + s
		}
		if len(s) >= 12 {
			set[s] = true
		}
	}
	return sortedKeys(set)
}

func sortedKeys(m map[string]bool) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Registry is one row of a LeadsRAG export CSV, indexed by NameKey.
type Registry struct {
	ICO, Name, Segment, City, Size string
	Score                          float64
}

// Stats are the numbers FINDINGS asks for.
type Stats struct {
	Places, Duplicates, Filtered, Leads, WithEmail, WithPhone, WithAnyContact, JoinedRegistry int
}

// Dropped is a place the category filter rejected, kept for manual review.
type Dropped struct {
	Title, Segment, Category, Reason, MapsURL string
}

// Merge builds leads from places (deduped by placeId, then domain, then name+city),
// drops places outside their segment, attaches website contacts by domain and
// registry rows by name. The kept places are returned index-aligned with the leads.
func Merge(places []Place, contacts []Contacts, registry map[string]Registry) ([]Lead, []Place, []Dropped, Stats) {
	st := Stats{Places: len(places)}
	byDomain := map[string]Contacts{}
	for _, c := range contacts {
		d := Domain(c.Domain)
		if d == "" {
			d = Domain(c.URL)
		}
		if d == "" {
			continue
		}
		prev := byDomain[d]
		prev.Emails = append(prev.Emails, c.Emails...)
		prev.Phones = append(prev.Phones, c.Phones...)
		if c.LastActive > prev.LastActive {
			prev.LastActive = c.LastActive
		}
		byDomain[d] = prev
	}
	seen := map[string]bool{}
	var out []Lead
	var kept []Place
	var dropped []Dropped
	for _, p := range places {
		d := Domain(p.Website)
		keys := []string{"pid:" + p.PlaceID, "dom:" + d, "nc:" + NameKey(p.Title) + "|" + strings.ToLower(p.City)}
		dup := false
		for i, k := range keys {
			if (i == 0 && p.PlaceID == "") || (i == 1 && d == "") {
				continue
			}
			if seen[k] {
				dup = true
			}
		}
		if dup {
			st.Duplicates++
			continue
		}
		for _, k := range keys {
			seen[k] = true
		}
		seg := Segment(p)
		if ok, why := Filter(p, seg); !ok {
			dropped = append(dropped, Dropped{Title: p.Title, Segment: seg, Category: p.CategoryName, Reason: why, MapsURL: p.URL})
			st.Filtered++
			continue
		}
		l := Lead{Name: p.Title, Segment: seg, City: p.City, Address: p.Address, Web: p.Website,
			Category: p.CategoryName, Rating: p.TotalScore, Reviews: p.ReviewsCount, MapsURL: p.URL, Sources: []string{"google_maps"}}
		emails, phones := p.Emails, []string{p.Phone}
		if c, ok := byDomain[d]; ok {
			emails = append(emails, c.Emails...)
			phones = append(phones, c.Phones...)
			l.LastActive = c.LastActive
			l.Sources = append(l.Sources, "website")
		}
		l.Emails, l.Phones = cleanEmails(emails, d), cleanPhones(phones)
		if r, ok := registry[NameKey(p.Title)]; ok {
			l.ICO, l.Employees, l.LeadsScore = r.ICO, r.Size, r.Score
			if l.Segment == "none" {
				l.Segment = r.Segment
			}
			l.Sources = append(l.Sources, "registr_smluv")
			st.JoinedRegistry++
		}
		if len(l.Emails) > 0 {
			st.WithEmail++
		}
		if len(l.Phones) > 0 {
			st.WithPhone++
		}
		if len(l.Emails)+len(l.Phones) > 0 {
			st.WithAnyContact++
		}
		out = append(out, l)
		kept = append(kept, p)
	}
	st.Leads = len(out)
	return out, kept, dropped, st
}

// JoinRegistryByICO attaches LeadsRAG rows once IČOs are known (Maps titles rarely match registry names).
func JoinRegistryByICO(leads []Lead, byICO map[string]Registry) int {
	n := 0
	for i := range leads {
		l := &leads[i]
		if l.ICO == "" {
			continue
		}
		r, ok := byICO[strings.TrimLeft(l.ICO, "0")]
		if !ok {
			continue
		}
		if l.LeadsScore == 0 {
			n++
			l.Sources = append(l.Sources, "registr_smluv")
		}
		l.LeadsScore = r.Score
		if l.Employees == "" {
			l.Employees = r.Size
		}
	}
	return n
}

// DecodeJSONL decodes one JSON value per non-empty line.
func DecodeJSONL[T any](lines [][]byte) ([]T, error) {
	var out []T
	for _, ln := range lines {
		if len(strings.TrimSpace(string(ln))) == 0 {
			continue
		}
		var v T
		if err := json.Unmarshal(ln, &v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}
