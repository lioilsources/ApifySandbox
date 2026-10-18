package leads

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Web fetches company websites to find the IČO they must publish (§ 435 občanského zákoníku).
type Web struct {
	HTTP *http.Client
}

func NewWeb() *Web {
	return &Web{HTTP: &http.Client{Timeout: 12 * time.Second}}
}

func (w *Web) get(ctx context.Context, u string) string {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; olin-leads/0.1; +https://olin.now)")
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	return string(b)
}

// FindICOs looks at the homepage, then common contact pages, and stops at the first page with an IČO.
func (w *Web) FindICOs(ctx context.Context, site string) []string {
	if site == "" {
		return nil
	}
	if !strings.Contains(site, "://") {
		site = "https://" + site
	}
	u, err := url.Parse(site)
	if err != nil || u.Host == "" || strings.Contains(u.Host, "facebook.") || strings.Contains(u.Host, "instagram.") {
		return nil
	}
	root := u.Scheme + "://" + u.Host
	for _, p := range []string{site, root + "/kontakt", root + "/kontakty", root + "/kontakt/", root + "/o-nas", root + "/obchodni-podminky"} {
		if icos := ICOsInText(w.get(ctx, p)); len(icos) > 0 {
			return icos
		}
	}
	return nil
}

// Match is how an IČO was attached to a lead.
const (
	ICOFromWeb      = "web"            // published on the company's own website, name or seat matches ARES
	ICOFromWebOther = "web_jine_jmeno" // the only IČO on the website, but ARES name differs: operator or web agency, review
	ICOFromARESPSC  = "ares_name_psc"  // ARES name search, unique candidate with the Maps postal code
	ICOFromARESName = "ares_name"      // ARES name search, normalized names equal, same city
)

// ResolveICO tries the website first, then ARES name search filtered by postal code / city.
func ResolveICO(ctx context.Context, a *ARES, w *Web, p Place) (*Subject, string, error) {
	psc := PSC(p.Address)
	icos := w.FindICOs(ctx, p.Website)
	for _, ico := range icos {
		s, err := a.Get(ctx, ico)
		if err != nil {
			return nil, "", err
		}
		if s == nil || s.Dissolved != "" {
			continue
		}
		// A site can list a parent company or a web agency; accept it when name or seat matches,
		// or when it is the only IČO on the page.
		if NameKey(CleanTitle(p.Title)) != "" && (strings.Contains(NameKey(s.Name), NameKey(CleanTitle(p.Title))) ||
			strings.Contains(NameKey(CleanTitle(p.Title)), NameKey(s.Name)) || (psc != 0 && s.Seat.PSC == psc)) {
			return s, ICOFromWeb, nil
		}
		if len(icos) == 1 {
			return s, ICOFromWebOther, nil
		}
	}
	queries := []string{CleanTitle(p.Title)}
	if d := Domain(p.Website); d != "" {
		if stem, _, ok := strings.Cut(d, "."); ok && len(stem) >= 4 {
			queries = append(queries, stem)
		}
	}
	for _, q := range queries {
		cands, err := a.Search(ctx, q, 50)
		if err != nil {
			return nil, "", err
		}
		var byPSC []Subject
		for _, c := range cands {
			if c.Dissolved != "" {
				continue
			}
			if psc != 0 && c.Seat.PSC == psc {
				byPSC = append(byPSC, c)
			}
		}
		if len(byPSC) == 1 {
			return &byPSC[0], ICOFromARESPSC, nil
		}
		for _, c := range cands {
			if c.Dissolved == "" && NameKey(c.Name) == NameKey(q) && sameCity(c.Seat.City, p.City) {
				return &c, ICOFromARESName, nil
			}
		}
	}
	return nil, "", nil
}

// sameCity compares "Praha" (ARES) with "Praha 16" or "Brno-sever" (Maps).
func sameCity(aresCity, mapsCity string) bool {
	a, m := strings.ToLower(aresCity), strings.ToLower(mapsCity)
	return a != "" && (a == m || strings.HasPrefix(m, a+" ") || strings.HasPrefix(m, a+"-"))
}
