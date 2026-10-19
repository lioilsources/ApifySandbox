// Package strategy is the pure decision function of the tradebot: features in,
// target weights out. No I/O, no clock, so the backtest and the live runner
// share it byte for byte.
package strategy

import (
	"math"
	"sort"
)

// Params are fixed for v0; they are not fitted to the data.
type Params struct {
	TopN          int     // positions held
	TargetVol     float64 // annualized, per position before capping
	MaxWeight     float64 // cap per instrument
	RebalanceBand float64 // skip trades smaller than this weight change
	MomLookback   int     // days, 252
	MomSkip       int     // days skipped at the end, 21 (12-1 momentum)
	VolWindow     int     // days, 60
	TrendWindow   int     // SMA days, 200
	SentWeight    float64 // 0 = variant A, 0.25 = variant B overlay
	FeeRate       float64 // per side, Kraken taker 0.0026
	Slippage      float64 // per side, assumed 0.001
}

// Default is variant A (baseline, no sentiment).
var Default = Params{TopN: 3, TargetVol: 0.10, MaxWeight: 0.40, RebalanceBand: 0.05,
	MomLookback: 252, MomSkip: 21, VolWindow: 60, TrendWindow: 200, FeeRate: 0.0026, Slippage: 0.001}

// Feature is one instrument on one day.
type Feature struct {
	Symbol  string
	Mom     float64 // 12-1 momentum
	Vol     float64 // annualized
	Trend   bool    // close > SMA
	Carry   float64 // commodities only, 0 for crypto
	Sent    float64 // z-score, used only when HasSent
	HasSent bool
}

// Features computes today's feature from a close series ending at today. ok is
// false while the history is shorter than the longest window.
func Features(p Params, symbol string, closes []float64) (f Feature, ok bool) {
	n := len(closes)
	need := p.MomLookback + 1
	if p.TrendWindow+1 > need {
		need = p.TrendWindow + 1
	}
	if n < need {
		return Feature{}, false
	}
	last := closes[n-1]
	f.Symbol = symbol
	f.Mom = closes[n-1-p.MomSkip]/closes[n-1-p.MomLookback] - 1
	var sum float64
	for _, c := range closes[n-p.TrendWindow:] {
		sum += c
	}
	f.Trend = last > sum/float64(p.TrendWindow)
	rets := make([]float64, 0, p.VolWindow)
	for i := n - p.VolWindow; i < n; i++ {
		rets = append(rets, math.Log(closes[i]/closes[i-1]))
	}
	f.Vol = std(rets) * math.Sqrt(365) // crypto trades every day
	return f, f.Vol > 0
}

func std(x []float64) float64 {
	if len(x) < 2 {
		return 0
	}
	var m float64
	for _, v := range x {
		m += v
	}
	m /= float64(len(x))
	var s float64
	for _, v := range x {
		s += (v - m) * (v - m)
	}
	return math.Sqrt(s / float64(len(x)-1))
}

// Target ranks candidates in an uptrend with positive momentum, takes TopN,
// sizes each to TargetVol and caps. Sentiment only scales a weight, never
// flips the direction. Remaining weight is cash.
func Target(p Params, feats []Feature) map[string]float64 {
	var cands []Feature
	for _, f := range feats {
		if f.Trend && f.Mom > 0 && f.Vol > 0 {
			cands = append(cands, f)
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		return cands[i].Mom+0.5*cands[i].Carry > cands[j].Mom+0.5*cands[j].Carry
	})
	if len(cands) > p.TopN {
		cands = cands[:p.TopN]
	}
	w := map[string]float64{}
	var total float64
	for _, f := range cands {
		x := (p.TargetVol / f.Vol) / float64(p.TopN)
		if f.HasSent && p.SentWeight > 0 {
			x *= clamp(1+p.SentWeight*f.Sent, 0.5, 1.5)
		}
		x = math.Min(x, p.MaxWeight)
		w[f.Symbol] = x
		total += x
	}
	if total > 1 {
		for k := range w {
			w[k] /= total
		}
	}
	return w
}

// Rebalance returns the weight changes worth trading given the band.
func Rebalance(p Params, current, target map[string]float64) map[string]float64 {
	d := map[string]float64{}
	for k, t := range target {
		if math.Abs(t-current[k]) > p.RebalanceBand {
			d[k] = t - current[k]
		}
	}
	for k, c := range current {
		if _, ok := target[k]; !ok && c > p.RebalanceBand {
			d[k] = -c
		}
	}
	return d
}

func clamp(v, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, v)) }
