package strategy

import (
	"math"
	"testing"
)

func series(n int, step float64) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = 100 + step*float64(i) + math.Sin(float64(i))
	}
	return out
}

func TestFeaturesNeedHistory(t *testing.T) {
	if _, ok := Features(Default, "X", series(200, 0.1)); ok {
		t.Fatal("200 days must not be enough for 252-day momentum")
	}
	f, ok := Features(Default, "X", series(300, 0.1))
	if !ok || f.Mom <= 0 || !f.Trend || f.Vol <= 0 {
		t.Fatalf("%+v %v", f, ok)
	}
	f, ok = Features(Default, "Y", series(300, -0.1))
	if !ok || f.Mom >= 0 || f.Trend {
		t.Fatalf("downtrend: %+v", f)
	}
}

func TestTargetSizesAndCaps(t *testing.T) {
	feats := []Feature{
		{Symbol: "A", Mom: 0.5, Vol: 0.20, Trend: true},  // 0.10/0.20/3 = 0.167
		{Symbol: "B", Mom: 0.4, Vol: 0.05, Trend: true},  // 0.667 → capped 0.40
		{Symbol: "C", Mom: 0.3, Vol: 0.80, Trend: true},  // 0.042
		{Symbol: "D", Mom: 0.9, Vol: 0.50, Trend: false}, // no trend → out
		{Symbol: "E", Mom: -0.1, Vol: 0.50, Trend: true}, // negative mom → out
	}
	w := Target(Default, feats)
	if len(w) != 3 || w["D"] != 0 || w["E"] != 0 {
		t.Fatalf("%v", w)
	}
	if math.Abs(w["B"]-0.40) > 1e-9 || math.Abs(w["A"]-0.1/0.2/3) > 1e-9 {
		t.Fatalf("%v", w)
	}
	var sum float64
	for _, x := range w {
		sum += x
	}
	if sum > 1 {
		t.Fatal("gross exposure over 100 %")
	}
}

func TestSentimentOnlyScales(t *testing.T) {
	p := Default
	p.SentWeight = 0.25
	hype := Target(p, []Feature{{Symbol: "A", Mom: 0.5, Vol: 0.5, Trend: true, Sent: 4, HasSent: true}})
	fear := Target(p, []Feature{{Symbol: "A", Mom: 0.5, Vol: 0.5, Trend: true, Sent: -4, HasSent: true}})
	base := Target(Default, []Feature{{Symbol: "A", Mom: 0.5, Vol: 0.5, Trend: true}})
	if math.Abs(hype["A"]-1.5*base["A"]) > 1e-9 || math.Abs(fear["A"]-0.5*base["A"]) > 1e-9 {
		t.Fatalf("base %v hype %v fear %v", base, hype, fear)
	}
}

func TestRebalanceBand(t *testing.T) {
	d := Rebalance(Default, map[string]float64{"A": 0.30, "B": 0.20, "C": 0.03}, map[string]float64{"A": 0.33, "B": 0.00})
	if _, ok := d["A"]; ok {
		t.Fatal("3 pb change is inside the band")
	}
	if d["B"] != -0.20 || len(d) != 1 {
		t.Fatalf("%v", d)
	}
}
