package classify

import (
	"regexp"
	"strings"
	"unicode"

	"github.com/lioilsources/apifysandbox/ruian"
)

// Redact replaces names of natural persons with "[osoba]". It is a
// heuristic (GDPR data minimisation, not anonymisation): birth dates,
// academic titles, "žadatel/stavebník/pro …" and common Czech first names
// followed by a surname. Company and authority names are kept.
func Redact(s string) string {
	s = birthRe.ReplaceAllString(s, "[osoba]")
	s = titleRe.ReplaceAllString(s, "[osoba]")
	s = familyRe.ReplaceAllString(s, "${1}[osoba]")
	s = roleRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := roleRe.FindStringSubmatch(m)
		if isOrg(sub[2]) {
			return m
		}
		return sub[1] + "[osoba]"
	})
	return redactFirstNames(s)
}

const nameWord = `\p{Lu}[\p{Ll}\p{Lu}'-]+`

var (
	// "Jan Novák, nar. 1. 2. 1980" / "PETR KOVÁŘ, nar. 01.02.1990"
	birthRe = regexp.MustCompile(nameWord + `(?:\s+` + nameWord + `){1,3},?\s+nar\.\s*\d{1,2}\.\s*\d{1,2}\.\s*\d{4}`)
	// "Ing. Jan Novák", "MUDr. Šárka Nováková Dvořáková"
	titleRe = regexp.MustCompile(`(?:\b(?:Ing|Mgr|MUDr|JUDr|Bc|PhDr|RNDr|MVDr|PaedDr|doc|prof|Ing\. arch)\.\s*)+` + nameWord + `(?:\s+` + nameWord + `){0,2}(?:,?\s*(?:Ph\.D\.|CSc\.|DiS\.|MBA))?`)
	// "žadatel: Jan Novák", "pro manžele Novákovy", "stavebník Petr Svoboda"
	roleRe = regexp.MustCompile(`((?i:žadatel[ée]?|žadatelka|povinn[ýáí]|stavebník[ci]?|stavebnice|investor|vlastník|vlastnice|manžel[ée]?|pro\s+(?:pana|paní|manžele|p\.))\s*:?\s+)(` + nameWord + `(?:\s+` + nameWord + `){0,2})`)
)

// "Novostavba RD Kopeckovi", "rodinný dům pro Novákovy": a family named in
// the dative/plural after RD or "pro".
var familyRe = regexp.MustCompile(`((?:\bRD|rodinn\p{L}* dom\p{L}*|\bpro)\s+)\p{Lu}\p{Ll}+(?:ovi|ovým|ových|ovou|ovy)\b`)

var orgWords = []string{"s.r.o", "a.s", "spol", "z.s", "o.p.s", "s.p", "v.o.s", "k.s", "obec", "město", "mesto", "kraj", "městys",
	"státní", "statni", "ředitelství", "reditelstvi", "správa", "sprava", "čez", "cez", "egd", "pre ", "innogy", "gasnet", "družstvo", "svj",
	"společenství", "fakultní", "univerzit", "česká republika", "ceska republika", "povodí", "lesy", "úřad", "urad", "nemocnice", "škola", "skola"}

func isOrg(s string) bool {
	f := strings.ToLower(s)
	for _, w := range orgWords {
		if strings.Contains(f, w) {
			return true
		}
	}
	return false
}

// Common Czech first names (nominative), folded. Enough to catch
// "Miroslav Dvořák" or "MATĚJ NOVÝ"; streets in genitive ("Jana Palacha")
// are protected by the street-prefix check.
var firstNames = map[string]bool{}

func init() {
	for _, n := range strings.Fields(`jan jiri petr josef pavel martin tomas jaroslav miroslav zdenek vaclav michal frantisek
	jakub milan karel lukas david ondrej vladimir ladislav roman marek radek stanislav daniel antonin vojtech filip adam matej
	ales dominik patrik libor jaromir vlastimil jindrich zbynek rostislav dusan oldrich bohumil vit robert ivan igor viktor
	lubos lubomir kamil richard rudolf radim bohuslav otakar emil eduard leos stepan sebastian krystof simon matyas jonas
	marie jana eva hana anna lenka katerina vera lucie alena petra jaroslava ludmila helena veronika jitka zdenka martina
	michaela ivana monika tereza zuzana jarmila marcela barbora dagmar kristyna milada vlasta irena jirina marketa pavla
	miroslava olga andrea klara nikola denisa simona radka romana blanka iveta renata sarka dana eliska adela natalie
	karolina vendula magdalena bozena libuse marta stanislava drahomira kamila sona silvie`) {
		firstNames[n] = true
	}
}

var wordRe = regexp.MustCompile(`\p{L}[\p{L}'-]*`)

var streetPrefix = []string{"ul.", "ulice", "ulici", "nám.", "náměstí", "nábř.", "nábřeží", "třída", "tř.", "sídliště", "sídl.", "most", "park", "sady", "škola", "ZŠ", "MŠ", "gymnázium"}

func redactFirstNames(s string) string {
	locs := wordRe.FindAllStringIndex(s, -1)
	var b strings.Builder
	last := 0
	for i := 0; i+1 < len(locs); i++ {
		w1 := s[locs[i][0]:locs[i][1]]
		w2 := s[locs[i+1][0]:locs[i+1][1]]
		if !firstNames[ruian.Fold(w1)] || !isCap(w1) || !isCap(w2) || len([]rune(w2)) < 3 {
			continue
		}
		// only whitespace between the two words
		if strings.TrimSpace(s[locs[i][1]:locs[i+1][0]]) != "" {
			continue
		}
		before := strings.ToLower(strings.TrimSpace(s[max(0, locs[i][0]-12):locs[i][0]]))
		skip := false
		for _, p := range streetPrefix {
			if strings.HasSuffix(before, strings.ToLower(p)) {
				skip = true
				break
			}
		}
		if skip || isOrg(w2) {
			continue
		}
		b.WriteString(s[last:locs[i][0]])
		b.WriteString("[osoba]")
		last = locs[i+1][1]
		i++
	}
	b.WriteString(s[last:])
	return b.String()
}

func isCap(w string) bool {
	for _, r := range w {
		return unicode.IsUpper(r)
	}
	return false
}
