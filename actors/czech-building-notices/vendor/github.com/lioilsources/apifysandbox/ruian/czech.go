package ruian

import (
	"strings"
	"unicode"
)

var fold = strings.NewReplacer(
	"á", "a", "č", "c", "ď", "d", "é", "e", "ě", "e", "í", "i", "ň", "n", "ó", "o",
	"ř", "r", "š", "s", "ť", "t", "ú", "u", "ů", "u", "ý", "y", "ž", "z",
	"ä", "a", "ö", "o", "ü", "u", "ĺ", "l", "ľ", "l", "ŕ", "r", "ô", "o",
)

// Fold lowercases s and strips Czech (and Slovak/German) diacritics.
func Fold(s string) string {
	return fold.Replace(strings.ToLower(s))
}

// words splits folded text into letter/digit runs.
func words(s string) []string {
	return strings.FieldsFunc(Fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Case endings of Czech place names, longest first. Stripping one of them maps
// "Hrutově", "Horních Počernicích", "Starém Městě" to the same stem as the
// nominative. Vowel alternations (Libeň → Libni) are left to the edit distance.
var endings = []string{
	"ich", "ech", "ach", "ami", "emi", "ymi", "imi", "eho", "emu", "ych",
	"ym", "im", "em", "om", "ou", "ce", "ci", "ec", "ek", "ku",
	"e", "i", "y", "a", "o", "u",
}

// stem reduces a folded word to a declension-insensitive stem. Short words
// (prepositions "u", "nad", numbers) are kept as they are.
func stem(w string) string {
	if len(w) <= 3 || isDigits(w) {
		return w
	}
	for _, e := range endings {
		if strings.HasSuffix(w, e) && len(w)-len(e) >= 3 {
			return w[:len(w)-len(e)]
		}
	}
	return w
}

func stems(s string) []string {
	ws := words(s)
	out := ws[:0]
	for _, w := range ws {
		out = append(out, stem(w))
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// levenshtein on bytes of folded ASCII-ish strings; bounded by max for speed.
func levenshtein(a, b string, max int) int {
	if d := len(a) - len(b); d > max || -d > max {
		return max + 1
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		rowMin := cur[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
			rowMin = min(rowMin, cur[j])
		}
		if rowMin > max {
			return max + 1
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// stemDist is the declension-tolerant distance of two stems, or -1 when
// they are different words. 0 means the same stem.
func stemDist(a, b string) int {
	if a == b {
		return 0
	}
	if isDigits(a) || isDigits(b) || len(a) <= 3 || len(b) <= 3 {
		return -1
	}
	if d := len(a) - len(b); (d == 1 || d == 2 || d == -1 || d == -2) && min(len(a), len(b)) >= 4 &&
		(strings.HasPrefix(a, b) || strings.HasPrefix(b, a)) {
		return max(d, -d)
	}
	allowed := 1
	if len(a) >= 8 {
		allowed = 2
	}
	if d := levenshtein(a, b, allowed); d <= allowed {
		return d
	}
	return -1
}

// stemMatch tells whether two stems are the same word up to declension noise.
func stemMatch(a, b string) bool { return stemDist(a, b) >= 0 }
