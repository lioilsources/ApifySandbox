// Package backtest replays the strategy on daily candles. Signal uses close[t],
// the trade fills at close[t+1] with fee and slippage, so there is no lookahead.
package backtest

import (
	"math"
	"sort"

	"github.com/lioilsources/apifysandbox/tradebot/market"
	"github.com/lioilsources/apifysandbox/tradebot/strategy"
)

// Sentiment is an optional daily z-score per symbol per date (variant B).
type Sentiment map[string]map[string]float64 // date → symbol → z

// Result is what FINDINGS asks for.
type Result struct {
	Start, End      string
	Days            int
	CAGR            float64
	Vol             float64
	Sharpe          float64
	MaxDrawdown     float64
	TurnoverPerYear float64 // sum |Δw| per year
	Costs           float64 // total cost drag as fraction of start capital
	Trades          int
	BenchCAGR       float64 // buy-and-hold of Bench symbol
	BenchVol        float64
	BenchSharpe     float64
	BenchMaxDD      float64
	Equity          []Point
	Yearly          []Year
}

type Point struct {
	Date       string
	Equity     float64
	Bench      float64
	Weights    map[string]float64
	Candidates int
}

type Year struct {
	Year        string
	Return      float64
	BenchReturn float64
}

// Run aligns the series on the benchmark's calendar; a symbol without a close
// on a day is simply absent from that day's features.
func Run(p strategy.Params, series map[string][]market.Candle, bench string, sent Sentiment) Result {
	cal := series[bench]
	if len(cal) == 0 {
		return Result{}
	}
	closeAt := map[string]map[string]float64{} // symbol → date → close
	for sym, cs := range series {
		m := make(map[string]float64, len(cs))
		for _, c := range cs {
			m[c.Date] = c.Close
		}
		closeAt[sym] = m
	}
	syms := make([]string, 0, len(series))
	for s := range series {
		syms = append(syms, s)
	}
	sort.Strings(syms)

	hist := map[string][]float64{} // closes seen so far, contiguous per symbol
	equity, benchEq := 1.0, 1.0
	weights := map[string]float64{} // weights held now (as fraction of equity, drift ignored day to day for simplicity: rebalanced nominal)
	var pending map[string]float64  // trades decided yesterday, filled today
	var r Result
	peak, benchPeak := 1.0, 1.0
	var sumR, sumR2, sumB, sumB2 float64
	var n int
	yearStart := map[string][2]float64{}
	cost := p.FeeRate + p.Slippage

	for i := 1; i < len(cal); i++ {
		date, prev := cal[i].Date, cal[i-1].Date
		// portfolio return from prev to date on yesterday's weights
		var ret float64
		for s, w := range weights {
			c0, ok0 := closeAt[s][prev]
			c1, ok1 := closeAt[s][date]
			if ok0 && ok1 {
				ret += w * (c1/c0 - 1)
			}
		}
		equity *= 1 + ret
		benchEq *= closeAt[bench][date] / closeAt[bench][prev]
		// fill yesterday's decision at today's close
		if pending != nil {
			var traded float64
			for s, d := range pending {
				weights[s] += d
				if weights[s] < 1e-9 {
					delete(weights, s)
				}
				traded += math.Abs(d)
				r.Trades++
			}
			equity *= 1 - traded*cost
			r.Costs += traded * cost
			r.TurnoverPerYear += traded
			pending = nil
		}
		// features on today's close, decision for tomorrow
		var feats []strategy.Feature
		for _, s := range syms {
			c, ok := closeAt[s][date]
			if !ok {
				continue
			}
			hist[s] = append(hist[s], c)
			if f, ok := strategy.Features(p, s, hist[s]); ok {
				if z, ok := sent[date][s]; ok && p.SentWeight > 0 {
					f.Sent, f.HasSent = z, true
				}
				feats = append(feats, f)
			}
		}
		warm := len(hist[bench]) > p.MomLookback+1
		if warm {
			target := strategy.Target(p, feats)
			if d := strategy.Rebalance(p, weights, target); len(d) > 0 {
				pending = d
			}
		}
		if !warm {
			// reset so performance stats start when trading can
			equity, benchEq, peak, benchPeak = 1, 1, 1, 1
			continue
		}
		if r.Start == "" {
			r.Start = date
		}
		peak = math.Max(peak, equity)
		benchPeak = math.Max(benchPeak, benchEq)
		r.MaxDrawdown = math.Min(r.MaxDrawdown, equity/peak-1)
		r.BenchMaxDD = math.Min(r.BenchMaxDD, benchEq/benchPeak-1)
		sumR += ret
		sumR2 += ret * ret
		bret := closeAt[bench][date]/closeAt[bench][prev] - 1
		sumB += bret
		sumB2 += bret * bret
		n++
		y := date[:4]
		if _, ok := yearStart[y]; !ok {
			yearStart[y] = [2]float64{equity / (1 + ret), benchEq / (closeAt[bench][date] / closeAt[bench][prev])}
		}
		r.End = date
		wcopy := make(map[string]float64, len(weights))
		for k, v := range weights {
			wcopy[k] = math.Round(v*1000) / 1000
		}
		r.Equity = append(r.Equity, Point{Date: date, Equity: equity, Bench: benchEq, Weights: wcopy, Candidates: countCands(feats)})
	}
	if n == 0 {
		return r
	}
	r.Days = n
	years := float64(n) / 365
	r.CAGR = math.Pow(equity, 1/years) - 1
	r.BenchCAGR = math.Pow(benchEq, 1/years) - 1
	mean := sumR / float64(n)
	r.Vol = math.Sqrt(sumR2/float64(n)-mean*mean) * math.Sqrt(365)
	if r.Vol > 0 {
		r.Sharpe = mean * 365 / r.Vol
	}
	bmean := sumB / float64(n)
	r.BenchVol = math.Sqrt(sumB2/float64(n)-bmean*bmean) * math.Sqrt(365)
	if r.BenchVol > 0 {
		r.BenchSharpe = bmean * 365 / r.BenchVol
	}
	r.TurnoverPerYear /= years
	ys := make([]string, 0, len(yearStart))
	for y := range yearStart {
		ys = append(ys, y)
	}
	sort.Strings(ys)
	for i, y := range ys {
		endE, endB := equity, benchEq
		if i+1 < len(ys) {
			next := yearStart[ys[i+1]]
			endE, endB = next[0], next[1]
		}
		s := yearStart[y]
		r.Yearly = append(r.Yearly, Year{Year: y, Return: endE/s[0] - 1, BenchReturn: endB/s[1] - 1})
	}
	return r
}

func countCands(f []strategy.Feature) int {
	n := 0
	for _, x := range f {
		if x.Trend && x.Mom > 0 {
			n++
		}
	}
	return n
}
