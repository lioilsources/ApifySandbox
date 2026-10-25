package ruian

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ParcelRef is a parcel mentioned in text, before or after resolution.
type ParcelRef struct {
	Raw             string    `json:"raw"`    // the text the number came from
	Number          string    `json:"number"` // normalised: "st. 56", "123/4"
	Building        *bool     `json:"building"`
	BuildingAssumed bool      `json:"buildingAssumed,omitempty"` // no "st.": cadastral convention says pozemková
	KUText          string    `json:"kuText,omitempty"`          // k.ú. as written
	KU              *Unit     `json:"ku,omitempty"`              // resolved k.ú.
	KUCandidates    []KUMatch `json:"kuCandidates,omitempty"`
	Confidence      float64   `json:"confidence"`
	Offset          int       `json:"offset"` // byte offset in the input
	num             ParcelNumber
}

var (
	// (\b is ASCII-only in Go, so "č." needs an explicit left boundary)
	// "parc. č.", "p. č.", "č. parc.", "pozemek parc. č.", "pozemků č.", "parcela číslo", "st. p. č.", "st. parc. č.", "stavební parcela č."
	triggerRe = regexp.MustCompile(`(?i)(?:\b(?:pozem\p{L}*|parcel\p{L}*)\s+(?:(?:parc\.|p\.)\s*)?(?:č\.|čís(?:l\p{L}*)?\.?)|\bparc\.\s*č(?:\.|ís\p{L}*\.?)|\bp\.\s*č\.|(?:^|[\s(,;])č\.\s*parc\.|\bst\.\s*(?:p\.|parc\.)\s*(?:č\.)?|\bstavební\p{L}*\s+parcel\p{L}*\s+(?:č\.|čís\p{L}*\.?))`)
	// "k. ú.", "k.ú.", "kat. úz.", "katastrální území", "katastrálním území"
	kuRe    = regexp.MustCompile(`(?i)(?:\bk\.\s*ú\.|\bkat\.\s*(?:úz\.|území)|\bkatastrální\p{L}*\s+území\p{L}*)\s*:?\s*`)
	itemRe  = regexp.MustCompile(`^\s*(st\.\s*(?:p\.\s*)?(?:č\.\s*)?)?(\d{1,5})(?:\s*/\s*(\d{1,4}))?`)
	rangeRe = regexp.MustCompile(`^\s*(?:[-–]|až\s)\s*(?:(\d{1,5})\s*/\s*)?(\d{1,5})`)
	sepRe   = regexp.MustCompile(`^\s*(?:,|;|(?:a|i|nebo|a\s+dále)\s)\s*`)
	codeRe  = regexp.MustCompile(`^\s*[(\[]?\s*(?:kód\s*:?\s*)?(\d{6})\s*[)\]]?`)
)

var kuPreps = map[string]bool{"u": true, "nad": true, "pod": true, "v": true, "ve": true, "na": true, "při": true, "za": true, "mezi": true, "-": true}

type kuMention struct {
	start, end int
	text       string
	code       int // from "(648876)"
	ku         *Unit
	cands      []KUMatch
	score      float64
}

// ExtractParcelRefs finds parcel numbers and their k.ú. in Czech text.
// kuHint (name or code) is used for parcels with no k.ú. nearby.
// The gazetteer may be nil; then k.ú. stay unresolved (KUText only).
func ExtractParcelRefs(text string, g *Gazetteer, kuHint string) []ParcelRef {
	text = strings.Map(func(r rune) rune {
		if r == ' ' || r == ' ' || r == ' ' {
			return ' '
		}
		return r
	}, text)
	kus := findKUMentions(text, g)
	var hint *kuMention
	if kuHint != "" && g != nil {
		hint = &kuMention{text: kuHint}
		resolveKU(hint, g)
	}
	trig := triggerRe.FindAllStringIndex(text, -1)
	var out []ParcelRef
	seen := map[string]bool{}
	for ti, t := range trig {
		trigText := strings.ToLower(text[t[0]:t[1]])
		groupBuilding := strings.HasPrefix(trigText, "st.") || strings.HasPrefix(trigText, "stavební")
		limit := len(text)
		if ti+1 < len(trig) {
			limit = trig[ti+1][0]
		}
		items, end := parseItems(text, t[1], limit, groupBuilding)
		if len(items) == 0 {
			continue
		}
		km, conf := pickKU(text, kus, t[0], end, limit, hint)
		for _, it := range items {
			if km != nil {
				it.KUText = km.text
				it.KU = km.ku
				if km.ku == nil {
					it.KUCandidates = km.cands
				}
				it.Confidence = conf * max(km.score, 0.3)
			} else {
				it.Confidence = 0.2
			}
			key := it.Number
			if it.KU != nil {
				key = strconv.Itoa(it.KU.Code) + ":" + key
			} else {
				key = it.KUText + ":" + key
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			it.Confidence = float64(int(it.Confidence*100+0.5)) / 100
			out = append(out, it)
		}
	}
	return out
}

// ExtractKUs returns the distinct cadastral areas named in text
// ("v k. ú. Břevnov", "Katastrální území: Hrabůvka"), resolved only when
// the match is unambiguous.
func ExtractKUs(text string, g *Gazetteer) []Unit {
	if g == nil {
		return nil
	}
	var out []Unit
	seen := map[int]bool{}
	for _, m := range findKUMentions(text, g) {
		if m.ku != nil && !seen[m.ku.Code] {
			seen[m.ku.Code] = true
			out = append(out, *m.ku)
		}
	}
	return out
}

// MatchKUNames finds names of the candidate k.ú. (typically those of the
// office's municipality or ORP) written in text without a "k. ú." marker:
// "Pekařská, Staré Brno", "u Amálky, Jehnice". Single-word names shorter
// than 5 letters and lowercase occurrences are ignored to avoid false hits.
func MatchKUNames(text string, cands []Unit) []Unit {
	type tok struct {
		stem string
		cap  bool
	}
	var toks []tok
	for _, loc := range wordLocRe.FindAllStringIndex(text, -1) {
		w := text[loc[0]:loc[1]]
		r, _ := utf8.DecodeRuneInString(w)
		toks = append(toks, tok{stem(Fold(w)), unicode.IsUpper(r)})
	}
	var out []Unit
	for _, c := range cands {
		cs := stems(c.Name)
		if len(cs) == 0 || (len(cs) == 1 && len([]rune(c.Name)) < 5) {
			continue
		}
		for i := 0; i+len(cs) <= len(toks); i++ {
			if !toks[i].cap {
				continue
			}
			ok := true
			for j, st := range cs {
				if stemDist(toks[i+j].stem, st) < 0 {
					ok = false
					break
				}
			}
			if ok {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

var wordLocRe = regexp.MustCompile(`[\p{L}\d]+`)

// parseItems reads "123/4, 125 a st. 56" or "120–125" from pos, up to limit.
func parseItems(text string, pos, limit int, groupBuilding bool) ([]ParcelRef, int) {
	var out []ParcelRef
	end := pos
	for pos < limit {
		rest := text[pos:limit]
		m := itemRe.FindStringSubmatchIndex(rest)
		if m == nil {
			break
		}
		// reject years, prices and other numbers glued to letters ("123m2", "2024-01")
		after := rest[m[1]:]
		if r, _ := utf8.DecodeRuneInString(after); unicode.IsLetter(r) {
			break
		}
		building := groupBuilding || m[2] >= 0
		kmen, _ := strconv.Atoi(rest[m[4]:m[5]])
		var pod *int
		if m[6] >= 0 {
			n, _ := strconv.Atoi(rest[m[6]:m[7]])
			pod = &n
		}
		raw := strings.TrimSpace(rest[m[0]:m[1]])
		consumed := m[1]
		nums := []ParcelNumber{{Kmen: kmen, Pod: pod}}
		if rm := rangeRe.FindStringSubmatchIndex(rest[consumed:]); rm != nil {
			rk := kmen
			if rm[2] >= 0 {
				rk, _ = strconv.Atoi(rest[consumed+rm[2] : consumed+rm[3]])
			}
			hi, _ := strconv.Atoi(rest[consumed+rm[4] : consumed+rm[5]])
			if exp := expandRange(kmen, pod, rk, hi); exp != nil {
				nums = exp
				raw = strings.TrimSpace(rest[m[0] : consumed+rm[1]])
				consumed += rm[1]
			}
		}
		for _, n := range nums {
			b := building
			n.Building = &b
			out = append(out, ParcelRef{Raw: raw, Number: n.String(), Building: &b, BuildingAssumed: !building, Offset: pos + m[0], num: n})
		}
		pos += consumed
		end = pos
		sm := sepRe.FindStringIndex(text[pos:limit])
		if sm == nil {
			break
		}
		next := text[pos+sm[1] : limit]
		if itemRe.FindStringIndex(next) == nil {
			break
		}
		pos += sm[1]
	}
	return out, end
}

// expandRange turns "123/1–123/3" or "120–125" into numbers (max 50).
func expandRange(kmen int, pod *int, rk, hi int) []ParcelNumber {
	var out []ParcelNumber
	switch {
	case pod != nil && rk == kmen && hi > *pod && hi-*pod <= 50:
		for i := *pod; i <= hi; i++ {
			v := i
			out = append(out, ParcelNumber{Kmen: kmen, Pod: &v})
		}
	case pod == nil && rk == kmen && hi > kmen && hi-kmen <= 50:
		for i := kmen; i <= hi; i++ {
			out = append(out, ParcelNumber{Kmen: i})
		}
	}
	return out
}

// findKUMentions locates "k. ú. Název [(kód)]" and resolves each name.
func findKUMentions(text string, g *Gazetteer) []kuMention {
	var out []kuMention
	for _, m := range kuRe.FindAllStringIndex(text, -1) {
		name, end := readPlaceName(text, m[1])
		if name == "" {
			continue
		}
		km := kuMention{start: m[0], end: end, text: name}
		if cm := codeRe.FindStringSubmatchIndex(text[end:]); cm != nil {
			km.code, _ = strconv.Atoi(text[end+cm[2] : end+cm[3]])
			km.end = end + cm[1]
		}
		if g != nil {
			resolveKU(&km, g)
		}
		out = append(out, km)
	}
	return out
}

// readPlaceName reads capitalised words and the prepositions inside names
// ("Staré Město u Uherského Hradiště", "Brno-město", "Lhota pod Libčany").
func readPlaceName(text string, pos int) (string, int) {
	var words []string
	end := pos
	i := pos
	for len(words) < 8 {
		for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' ||
			(text[i] == '\n' && !strings.HasPrefix(strings.TrimLeft(text[i+1:], " \t\r"), "\n"))) {
			i++ // a single line break inside a name ("Pohořelice nad\nJihlavou") is a space
		}
		j := i
		for j < len(text) {
			r, sz := utf8.DecodeRuneInString(text[j:])
			if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.') {
				break
			}
			j += sz
		}
		if j == i {
			break
		}
		w := strings.TrimRight(text[i:j], ".")
		if w == "" {
			break
		}
		r, _ := utf8.DecodeRuneInString(w)
		switch {
		case unicode.IsUpper(r):
		case kuPreps[w] && len(words) > 0:
		case isDigits(w) && len(words) > 0 && len(w) <= 2: // "Praha 4" style districts
		default:
			goto done
		}
		words = append(words, w)
		i = i + len(w)
		if !kuPreps[w] {
			end = i
		}
	}
done:
	// drop trailing prepositions ("Hrutov u" when the sentence went on)
	for len(words) > 0 && kuPreps[words[len(words)-1]] {
		words = words[:len(words)-1]
	}
	return strings.Join(words, " "), end
}

// resolveKU tries the longest word prefix of the mention first, so
// "Hrutov Obec Hrutov" still resolves to Hrutov.
func resolveKU(km *kuMention, g *Gazetteer) {
	if km.code != 0 {
		if t, ok := g.Territory(km.code); ok {
			km.ku, km.score = &t.KU, 1
			return
		}
	}
	ws := strings.Fields(km.text)
	var best []KUMatch
	for n := len(ws); n >= 1; n-- {
		cands := g.FindKU(strings.Join(ws[:n], " "), "")
		if len(cands) == 0 {
			continue
		}
		if cands[0].Score >= 0.9 && (len(cands) == 1 || cands[1].Score < cands[0].Score) {
			u := cands[0].KU
			km.ku, km.score, km.text = &u, cands[0].Score, strings.Join(ws[:n], " ")
			return
		}
		if best == nil {
			best = cands
		}
	}
	if len(best) > 0 {
		km.cands, km.score = best, best[0].Score*0.7
	}
}

// pickKU chooses the k.ú. for a parcel group: a mention right after the
// numbers ("parc. č. 12 v k. ú. Hrutov") wins, then the nearest one before,
// then the hint.
func pickKU(text string, kus []kuMention, start, end, limit int, hint *kuMention) (*kuMention, float64) {
	between := make([]string, len(kus))
	for i, k := range kus {
		if k.start >= end && k.start <= len(text) {
			between[i] = text[end:k.start]
		}
	}
	for i := range kus {
		k := &kus[i]
		// nearest k.ú. after the numbers, even past another list in the same
		// sentence ("parc. č. 1 a pozemek parc. č. 2 v k. ú. X": both in X)
		if k.start >= end && k.start-end <= 160 && !sentenceBreak(between[i]) {
			return k, 1
		}
	}
	idx := sort.Search(len(kus), func(i int) bool { return kus[i].start >= start })
	if idx > 0 {
		return &kus[idx-1], 0.9
	}
	if hint != nil && (hint.ku != nil || len(hint.cands) > 0) {
		return hint, 0.6
	}
	return nil, 0
}

// sentenceBreak reports a sentence or paragraph end: ". X" or a blank line.
var sentenceRe = regexp.MustCompile(`[.!?]\s+\p{Lu}|\n\s*\n`)

func sentenceBreak(s string) bool { return sentenceRe.MatchString(s) }

// ResolveRefs looks every resolved reference up in RÚIAN. A pozemková
// assumption that finds nothing is retried without the type filter.
func (r *Registry) ResolveRefs(ctx context.Context, refs []ParcelRef, opt ParcelOptions, maxLookups int) ([]ResolvedRef, error) {
	out := make([]ResolvedRef, 0, len(refs))
	n := 0
	for _, ref := range refs {
		rr := ResolvedRef{Ref: ref, Parcels: []Parcel{}}
		if ref.KU == nil || n >= maxLookups {
			out = append(out, rr)
			continue
		}
		n++
		ps, err := r.Parcels(ctx, ref.KU.Code, ref.num, opt)
		if err != nil {
			return out, err
		}
		if len(ps) == 0 && ref.BuildingAssumed {
			loose := ref.num
			loose.Building = nil
			if ps, err = r.Parcels(ctx, ref.KU.Code, loose, opt); err != nil {
				return out, err
			}
			if len(ps) > 0 {
				rr.Note = "Pozemková parcela tohoto čísla v k.ú. neexistuje; vrácena stavební parcela se stejným číslem. Ověřte v textu, zda nejde o překlep nebo chybějící „st.“ či lomené číslo."
			}
		}
		rr.Parcels = ps
		out = append(out, rr)
	}
	return out, nil
}

// ResolvedRef pairs a text reference with the RÚIAN parcels it names.
type ResolvedRef struct {
	Ref     ParcelRef `json:"ref"`
	Parcels []Parcel  `json:"parcels"`
	Note    string    `json:"note,omitempty"`
}
