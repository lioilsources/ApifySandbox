package ruian

import (
	"bufio"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var (
	testGazOnce sync.Once
	testGaz     *Gazetteer
)

// testGazetteer loads the k.ú. and obec snapshot from testdata (names and
// codes only; higher units are not needed for text matching).
func testGazetteer(t *testing.T) *Gazetteer {
	t.Helper()
	testGazOnce.Do(func() {
		g := newGazetteer()
		readTSV(t, "testdata/ku.tsv", func(f []string) {
			code, _ := strconv.Atoi(f[0])
			obec, _ := strconv.Atoi(f[2])
			g.addKU(code, obec, f[1])
		})
		readTSV(t, "testdata/obce.tsv", func(f []string) {
			code, _ := strconv.Atoi(f[0])
			g.obec[code] = obecRec{name: f[1]}
		})
		g.index()
		testGaz = g
	})
	return testGaz
}

func readTSV(t *testing.T, path string, fn func([]string)) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		fn(strings.Split(s.Text(), "\t"))
	}
}

func TestStemDeclension(t *testing.T) {
	pairs := [][2]string{
		{"Hrutov", "Hrutově"}, {"Horní Počernice", "Horních Počernicích"}, {"Staré Město", "Starém Městě"},
		{"Žižkov", "Žižkově"}, {"Vinohrady", "Vinohradech"}, {"Mníšek u Liberce", "Mníšku u Liberce"},
		{"Lhota", "Lhotě"}, {"Veveří", "Veveří"}, {"Bubeneč", "Bubenči"}, {"Libeň", "Libni"},
	}
	for _, p := range pairs {
		a, b := stems(p[0]), stems(p[1])
		if len(a) != len(b) || totalDist(a, b) < 0 {
			t.Errorf("%q vs %q: stems %v %v do not match", p[0], p[1], a, b)
		}
	}
}

func TestFindKU(t *testing.T) {
	g := testGazetteer(t)
	cases := []struct {
		in   string
		want int // expected top code, 0 = no confident match expected
	}{
		{"Hrutov", 648876},
		{"Hrutově", 648876},
		{"Horních Počernicích", 643777},
		{"Starém Městě", 727024},
		{"Staré Město u Uherského Hradiště", 754617},
		{"648876", 648876},
		{"Vinohradech", 727164},
		{"Nové Město", 727181},
		{"Lhota", 744409},
	}
	for _, c := range cases {
		m := g.FindKU(c.in, "")
		if c.want == 0 {
			continue
		}
		if len(m) == 0 || m[0].KU.Code != c.want {
			t.Errorf("FindKU(%q) = %v, want %d", c.in, m, c.want)
		}
	}
	// "Lhota" with a tail: the bare name exists, so it wins outright.
	if m := g.FindKU("Lhota", ""); len(m) != 1 {
		t.Errorf("exact Lhota should be final, got %d candidates", len(m))
	}
}

func TestParseParcelNumber(t *testing.T) {
	cases := map[string]string{
		"123/4": "123/4", "st. 56": "st. 56", "st.56": "st. 56", "parc. č. 12": "12",
		"p. č. st. 7/2": "st. 7/2", "1234": "1234", "st. p. č. 9": "st. 9",
	}
	for in, want := range cases {
		n, err := ParseParcelNumber(in)
		if err != nil || n.String() != want {
			t.Errorf("ParseParcelNumber(%q) = %q, %v; want %q", in, n.String(), err, want)
		}
	}
	if _, err := ParseParcelNumber("abc"); err == nil {
		t.Error("expected error for abc")
	}
}

type wantRef struct {
	number string
	ku     int // 0 = unresolved
}

func TestExtractGolden(t *testing.T) {
	g := testGazetteer(t)
	cases := []struct {
		name, text, hint string
		want             []wantRef
	}{
		{"list then k.ú. after", "na pozemcích parc. č. 123/4, 123/5 a st. 56 v katastrálním území Hrutov", "",
			[]wantRef{{"123/4", 648876}, {"123/5", 648876}, {"st. 56", 648876}}},
		{"p. č.", "Záměr se dotkne pozemku p. č. 1 v k. ú. Hrutov.", "", []wantRef{{"1", 648876}}},
		{"k.ú. before", "v k.ú. Horní Počernice na pozemcích parc. č. 1234/1, 1234/2", "",
			[]wantRef{{"1234/1", 643777}, {"1234/2", 643777}}},
		{"declined k.ú.", "stavba na pozemku parc. č. st. 56 v k.ú. Hrutově", "", []wantRef{{"st. 56", 648876}}},
		{"range", "pozemky parc. č. 120–123 v k. ú. Veveří", "",
			[]wantRef{{"120", 610372}, {"121", 610372}, {"122", 610372}, {"123", 610372}}},
		{"sub range", "parc. č. 12/1–12/3, k. ú. Staré Město u Uherského Hradiště", "",
			[]wantRef{{"12/1", 754617}, {"12/2", 754617}, {"12/3", 754617}}},
		{"st. p. č. group", "st. p. č. 45 a 46 v katastrálním území Libeň", "",
			[]wantRef{{"st. 45", 730891}, {"st. 46", 730891}}},
		{"code in parentheses", "k. ú. Hrutov (648876), parc. č. 7", "", []wantRef{{"7", 648876}}},
		{"two k.ú.", "pozemek parc. č. 10 v k.ú. Hrutov a pozemek parc. č. 20 v k.ú. Veveří", "",
			[]wantRef{{"10", 648876}, {"20", 610372}}},
		{"area not a parcel", "pozemek parc. č. 55 o výměře 1200 m2 v k. ú. Hrutov", "", []wantRef{{"55", 648876}}},
		{"date not a parcel", "Dne 12. 3. 2025 bylo zahájeno řízení o pozemku parc. č. 15 v k. ú. Hrutov.", "",
			[]wantRef{{"15", 648876}}},
		{"parc. čísla", "pozemků parc. čísla 300/1 a 300/2 v k.ú. Hrutov", "",
			[]wantRef{{"300/1", 648876}, {"300/2", 648876}}},
		{"stavební parcela č.", "stavební parcela č. 89 v k.ú. Hrutov", "", []wantRef{{"st. 89", 648876}}},
		{"hint only", "na pozemcích parc. č. 5 a 6", "Hrutov", []wantRef{{"5", 648876}, {"6", 648876}}},
		{"no k.ú.", "na pozemku parc. č. 5", "", []wantRef{{"5", 0}}},
		{"Prague district after name", "k. ú. Vinohrady, Praha 2, parc. č. 4140", "", []wantRef{{"4140", 727164}}},
		{"sentence break", "Záměr leží v k.ú. Hrutov. Dále pozemek parc. č. 3 v k.ú. Veveří.", "", []wantRef{{"3", 610372}}},
		{"no spaces", "parc.č.1015/3 v k.ú. Hrutov", "", []wantRef{{"1015/3", 648876}}},
		{"kat. území", "pozemek p.č. 2269/14 v kat. území Hrutov", "", []wantRef{{"2269/14", 648876}}},
		{"katastrálního území", "pozemku parc. č. 8 katastrálního území Vinohrady", "", []wantRef{{"8", 727164}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			refs := ExtractParcelRefs(c.text, g, c.hint)
			var got []wantRef
			for _, r := range refs {
				w := wantRef{number: r.Number}
				if r.KU != nil {
					w.ku = r.KU.Code
				}
				got = append(got, w)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("got %v\nwant %v", got, c.want)
			}
		})
	}
}

func TestParseAddress(t *testing.T) {
	g := testGazetteer(t)
	cases := map[string]ParsedAddress{
		"Veveří 95, Brno":             {Street: "Veveří", HouseNo: 95, City: "Brno"},
		"Veveří 331/95, 602 00 Brno":  {Street: "Veveří", HouseNo: 331, OrientNo: 95, City: "Brno", PSC: "60200"},
		"Hrutov 12":                   {HouseNo: 12, City: "Hrutov"},
		"č.p. 12, Hrutov":             {HouseNo: 12, City: "Hrutov"},
		"Karlovo náměstí 13 Praha":    {Street: "Karlovo náměstí", HouseNo: 13, City: "Praha"},
		"Na Příkopě 12a, Praha 1":     {Street: "Na Příkopě", HouseNo: 12, Letter: "a", City: "Praha 1"},
		"ul. Masarykova 5/12b, Opava": {Street: "Masarykova", HouseNo: 5, OrientNo: 12, Letter: "b", City: "Opava"},
	}
	for in, want := range cases {
		if got := g.ParseAddress(in); got != want {
			t.Errorf("ParseAddress(%q) = %+v, want %+v", in, got, want)
		}
	}
}

func TestCodebooks(t *testing.T) {
	if got := Lookup(CBDruhPozemku, 13); got != "zastavěná plocha a nádvoří" {
		t.Errorf("druh pozemku 13 = %q", got)
	}
	if got := Lookup(CBZpusobVyuzitiObjektu, 7); got != "rodinný dům" {
		t.Errorf("využití objektu 7 = %q", got)
	}
	if got := Lookup(CBDruhPozemku, 999); got != "neznámý (999)" {
		t.Errorf("unknown = %q", got)
	}
	nine := 9
	if lookupPtr(CBZpusobVytapeni, &nine) != nil {
		t.Error("Nezjištěno must read as no data")
	}
	if len(ListCodebooks()) < 14 {
		t.Errorf("codebooks: %d", len(ListCodebooks()))
	}
}

func TestGeometry(t *testing.T) {
	sq := [][][2]float64{{{0, 0}, {2, 0}, {2, 2}, {0, 2}, {0, 0}}}
	c := centroid(sq)
	if c == nil || c.Lat != 1 || c.Lon != 1 {
		t.Errorf("centroid = %+v", c)
	}
	// a nearly straight edge collapses under simplification
	ring := [][2]float64{{0, 0}, {1, 0.000001}, {2, 0}, {2, 2}, {0, 2}, {0, 0}}
	if s := simplify(ring, 0.001); len(s) != 5 {
		t.Errorf("simplify kept %d points: %v", len(s), s)
	}
	d := distanceM(LatLon{50, 14}, LatLon{50.001, 14})
	if d < 110 || d > 112 {
		t.Errorf("distance = %f", d)
	}
}
