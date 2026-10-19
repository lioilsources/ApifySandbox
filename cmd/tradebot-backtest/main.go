// tradebot-backtest replays strategy variants on the stored candles and prints
// the FINDINGS table. -equity writes the daily curve for the dashboard.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/lioilsources/apifysandbox/tradebot/backtest"
	"github.com/lioilsources/apifysandbox/tradebot/market"
	"github.com/lioilsources/apifysandbox/tradebot/strategy"
)

func main() {
	dir := flag.String("dir", "data/tradebot/prices", "candle directory")
	from := flag.String("from", "", "ignore candles before YYYY-MM-DD")
	bench := flag.String("bench", "BTC", "buy-and-hold benchmark")
	topN := flag.Int("top", strategy.Default.TopN, "positions")
	targetVol := flag.Float64("vol", strategy.Default.TargetVol, "target vol per position")
	band := flag.Float64("band", strategy.Default.RebalanceBand, "rebalance band")
	equityOut := flag.String("equity", "", "write equity curve CSV")
	flag.Parse()

	series := map[string][]market.Candle{}
	for _, ins := range market.Universe {
		cs, err := market.Load(*dir, ins.Symbol)
		if err != nil {
			log.Fatal(err)
		}
		if *from != "" {
			i := 0
			for i < len(cs) && cs[i].Date < *from {
				i++
			}
			cs = cs[i:]
		}
		if len(cs) > 0 {
			series[ins.Symbol] = cs
		}
	}
	p := strategy.Default
	p.TopN, p.TargetVol, p.RebalanceBand = *topN, *targetVol, *band
	r := backtest.Run(p, series, *bench, nil)
	if r.Days == 0 {
		log.Fatal("not enough history")
	}
	fmt.Printf("variant A (momentum, no sentiment) %s → %s, %d days, %d instruments\n", r.Start, r.End, r.Days, len(series))
	fmt.Printf("CAGR %6.1f %%   vol %5.1f %%   Sharpe %4.2f   maxDD %6.1f %%   turnover %.1f×/y   cost drag %.1f %%   trades %d\n",
		100*r.CAGR, 100*r.Vol, r.Sharpe, 100*r.MaxDrawdown, r.TurnoverPerYear, 100*r.Costs, r.Trades)
	fmt.Printf("bench %s hold: CAGR %6.1f %%   vol %5.1f %%   Sharpe %4.2f   maxDD %6.1f %%\n", *bench, 100*r.BenchCAGR, 100*r.BenchVol, r.BenchSharpe, 100*r.BenchMaxDD)
	fmt.Println("year    bot     bench")
	for _, y := range r.Yearly {
		fmt.Printf("%s  %6.1f %%  %6.1f %%\n", y.Year, 100*y.Return, 100*y.BenchReturn)
	}
	if *equityOut != "" {
		f, err := os.Create(*equityOut)
		if err != nil {
			log.Fatal(err)
		}
		w := csv.NewWriter(f)
		w.Write([]string{"date", "equity", "bench", "candidates", "weights"})
		for _, pt := range r.Equity {
			var ws []string
			for k, v := range pt.Weights {
				ws = append(ws, k+"="+strconv.FormatFloat(v, 'f', 3, 64))
			}
			w.Write([]string{pt.Date, strconv.FormatFloat(pt.Equity, 'f', 5, 64), strconv.FormatFloat(pt.Bench, 'f', 5, 64), strconv.Itoa(pt.Candidates), strings.Join(ws, " ")})
		}
		w.Flush()
		f.Close()
	}
}
