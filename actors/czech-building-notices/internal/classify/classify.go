// Package classify decides whether a notice-board item concerns building or
// land and of which kind, using rules over the folded title, description and
// agenda. No LLM: rules are cheap, explainable and testable.
package classify

import (
	"regexp"

	"github.com/lioilsources/apifysandbox/ruian"
)

// Type of a notice. Empty means "not about building or land".
type Type string

const (
	EIA         Type = "eia"
	UzemniPlan  Type = "uzemni_plan"
	Kolaudace   Type = "kolaudace"
	Odstraneni  Type = "odstraneni"
	Majetek     Type = "majetek"
	Stanovisko  Type = "stanovisko"
	Zahajeni    Type = "zahajeni"
	Rozhodnuti  Type = "rozhodnuti"
	OstatniStav Type = "ostatni_stavebni"
)

// Labels are Czech names for output.
var Labels = map[Type]string{
	EIA:         "posuzování vlivů na životní prostředí (EIA)",
	UzemniPlan:  "územní plánování",
	Kolaudace:   "kolaudace a užívání stavby",
	Odstraneni:  "odstranění stavby",
	Majetek:     "nakládání s majetkem obce (prodej, pronájem, pacht)",
	Stanovisko:  "závazné stanovisko k záměru",
	Zahajeni:    "zahájení řízení o záměru",
	Rozhodnuti:  "rozhodnutí o záměru (povolení stavby)",
	OstatniStav: "jiná informace o stavbě",
}

// All lists the types in output order.
var All = []Type{Zahajeni, Rozhodnuti, Kolaudace, Odstraneni, Stanovisko, UzemniPlan, EIA, Majetek, OstatniStav}

func re(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// All patterns run on ruian.Fold(text): lowercase ASCII without diacritics,
// so \b works.
var (
	exclude       = re(`vozidl|oklesten|\bnalez|toulav|\bvolb|rozpoct|uzavirk|zvlastni uzivani|kaceni|\bpsa\b|\bpes\b|ztrat|pisemnost|exekuc|insolven|\blekar|zdravotni sluz|myslivost|stav nedostatku vody|\bdan[ie]? |poplat|kontrolni|stiznost|106/1999|\bdz\b|dopravni znac|o dorucovani|metodik|poskytnuti informace|poskytnuti (kopie|vydan)|odpoved na zadost|\bvr\b|referent|pracovni pozic|pracovni misto|verejnopravni smlouv|\bvps\b|smlouv[ay] o poskytnuti|uprav[ay] provozu|\bpdz\b|dopravniho znaceni|zabor|\bpup\b|\bdio\b|kanalizacniho radu`)
	reEIA         = re(`zjistovaci rizeni|100/2001|posuzovani vlivu|\beia\b|vlivu na zivotni prostredi`)
	rePlan        = re(`uzemni(ho|m)? plan|zasad(y|ach)? uzemniho rozvoje|\bzur\b|uzemni studi|regulacni plan|uzemni opatreni|stavebni uzaver|politik[ay] uzemniho rozvoje|uplatnovani|zprav\w* .{0,30}\bup\b|zmen\w* (c\. ?\d+ )?up\b`)
	reKol         = re(`kolaud|povoleni k uzivani|uzivani stavby|predcasne uzivani|zkusebni provoz`)
	reOdstr       = re(`odstraneni (stavby|objektu|staveb)|odstranit stavbu|demolic|bourani`)
	reMaj         = re(`zamer[ue]?\b.{0,60}(prodat|prodej[ei]?\b|pronaj|pronajm|propacht|pacht|smen|darov|vypujc|bezuplatn|prevod|zatiz|vecne bremeno|najem)|zverejneni zameru|zamer (obce|mesta)\b|aukcni vyhlask|vyberove rizeni na prodej|prodej (pozemk|nemovit|casti pozemk)|nabidka (pozemk|nemovit)|oznameni o zameru (pronajmu|prodeje)`)
	reStan        = re(`zavazne stanovisko|stanovisk\w* .{0,30}\bke? (stavebnim )?zamer|jednotne environmentalni stanovisko|\bjes\b`)
	reZah         = re(`(oznameni|vyrozumeni|uvedomeni|oznamuje).{0,60}zahajeni|zahajeni (spolecneho |stavebniho |uzemniho )?rizeni|verejne ustni jednani|ohledani na miste|seznamit se s podklady|vyzva k vyjadreni|oznameni o podani zadosti|namitk|zadost o (povoleni|vydani)`)
	reRozh        = re(`rozhodnuti|povoleni (stavby|zameru|zmeny)|stavebni povoleni|spolecne povoleni|uzemni souhlas|ramcove povoleni|dodatecne povoleni|prodlouzeni platnosti|zmena stavby pred dokoncenim|usneseni o (zastaveni|preruseni)|o umisteni stavby|\bur\b o umisteni`)
	reBuild       = re(`stavb|staveb|stavebn|zamer|vystavb|novostavb|pristavb|nastavb|rekonstrukc|rodinn\w* d[ou]m|bytov\w* d[ou]m|\brd\b|kanalizac|vodovod|pripoj|fotovolt|\bfve\b|\bbess\b|trafostanic|\bdts\b|\bts\b|\bknn\b|\bkvn\b|\bvn\b|\bnn\b|parc\. ?c|p\. ?c\.|k\. ?u\.|katastraln|chodnik|parkovist|\bmost|silnic|garaz|\bhal[ay]\b|sklad|areal|teplovod|plynovod|nabijec|vystavby|uzemni|zastavb|inzenyrsk|siti\b|prelozk|cyklostezk|okruzni krizovatk|spolecne(ho)? rizeni|uzemni(ho)? rizeni|stavebni(ho)? rizeni|\(u\+s\)|verejneho osvetleni|fttx|optick|\bcov\b|zamer: z/\d{4}/|\bcetin\b|deleni (nebo sceleni )?pozemk|sceleni pozemk`)
	reStrongBuild = re(`\bstavb[ay]\b|stavebni\w* zamer|zamer: z/\d{4}/\d+|rizeni: r/\d{4}/\d+|vystavb|novostavb|stavebni upravy|pristavb|nastavb`)
	reBuildAgenda = re(`stavebn|uzemni|vystavb|rozvoj uzemi|uzemni planovani`)
)

// Classify returns the notice type, or "" if the item is not about building
// or land. agenda is the item's agenda/department text.
func Classify(title, description, agenda string) Type {
	t := ruian.Fold(title + " \n " + description)
	ag := ruian.Fold(agenda)
	if exclude.MatchString(ruian.Fold(title)) {
		return ""
	}
	building := reBuild.MatchString(t) || reBuildAgenda.MatchString(ag)
	switch {
	case reEIA.MatchString(t):
		return EIA
	case rePlan.MatchString(t):
		return UzemniPlan
	case reMaj.MatchString(t):
		return Majetek
	case !building:
		return ""
	case reKol.MatchString(t):
		return Kolaudace
	case reOdstr.MatchString(t):
		return Odstraneni
	case reStan.MatchString(t):
		return Stanovisko
	case reZah.MatchString(t):
		return Zahajeni
	case reRozh.MatchString(t):
		return Rozhodnuti
	case reBuildAgenda.MatchString(ag) || reStrongBuild.MatchString(t):
		return OstatniStav
	}
	return ""
}
