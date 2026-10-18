package leads

import "strings"

// Google Maps categories (Czech UI) accepted per segment. A place is kept when
// any of its categories is allowed for its segment and none is blocked.
// Built from the first real run (FINDINGS 2026-10-17): Maps categories are
// weak for game studios ("Ústředí společnosti"), so hry also accepts a title hint.
var allowedCategories = map[string][]string{
	"agentury": {"marketingová agentura", "reklamní agentura", "internetový marketing", "marketingové poradenství",
		"videoprodukce", "pr agentura", "grafický designér", "webdesignér", "mediální agentura", "reklamní fotograf"},
	"teren": {"stavební společnost", "stavební firma", "úklidové služby", "topenář", "dodavatel klimatizace",
		"klempířské a pokrývačské práce", "zemní a výkopové práce", "elektroinstalační služby", "rekonstrukce budov",
		"instalatér", "dodavatel vytápění a vzduchotechniky", "zedník", "malíř", "čištění oken", "stavební dodavatel"},
	"hry":    {"vývoj softwaru", "softwarová společnost", "vydavatel videoher", "vývojář videoher", "ústředí společnosti"},
	"eshopy": {"internetový obchod"},
}

var blockedCategories = []string{"muzeum", "sex shop", "suvenýry", "sportovní hala", "tabák", "prodejna elektronických cigaret",
	"sdružení nebo organizace", "hobbymarket", "obchod pro kutily"}

var gameHints = []string{"game", "games", "gaming", "hry", "herní"}

// Filter reports whether a place belongs to its segment and, if not, why.
func Filter(p Place, segment string) (bool, string) {
	cats := p.Categories
	if len(cats) == 0 && p.CategoryName != "" {
		cats = []string{p.CategoryName}
	}
	low := make([]string, len(cats))
	for i, c := range cats {
		low[i] = strings.ToLower(c)
	}
	for _, c := range low {
		for _, b := range blockedCategories {
			if strings.Contains(c, b) {
				return false, "blocked category: " + c
			}
		}
	}
	if p.Closed {
		return false, "permanently closed"
	}
	allowed := allowedCategories[segment]
	if allowed == nil {
		return false, "no segment"
	}
	ok, generic := false, true
	for _, c := range low {
		for _, a := range allowed {
			if c == a {
				ok = true
				generic = generic && c == "ústředí společnosti"
			}
		}
	}
	if segment == "eshopy" && !ok {
		// Maps lists shops by what they sell; an online shop says so in its name.
		t := strings.ToLower(p.Title)
		ok = strings.Contains(t, "e-shop") || strings.Contains(t, "eshop")
	}
	if segment == "hry" && ok && generic {
		// "Ústředí společnosti" says nothing about games; require a hint in the name or web.
		t := strings.ToLower(p.Title + " " + p.Website)
		hint := false
		for _, h := range gameHints {
			hint = hint || strings.Contains(t, h)
		}
		if !hint {
			return false, "generic category, no game hint in name/web"
		}
	}
	if !ok {
		if len(low) == 0 {
			return false, "no category"
		}
		return false, "category not in segment: " + strings.Join(low, ", ")
	}
	return true, ""
}
