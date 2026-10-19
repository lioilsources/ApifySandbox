// tradebot-data maintains the daily candle CSVs: -backfill pulls the full
// history from Binance (keyless, 2017+), -update appends the last closed days
// from Kraken. Both are idempotent and free.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/lioilsources/apifysandbox/tradebot/market"
)

func main() {
	dir := flag.String("dir", "data/tradebot/prices", "candle directory")
	backfill := flag.Bool("backfill", false, "Binance history from 2017-08-17")
	update := flag.Bool("update", false, "Kraken last 720 days")
	flag.Parse()
	ctx := context.Background()
	f := market.NewFetcher()
	for _, ins := range market.Universe {
		have, err := market.Load(*dir, ins.Symbol)
		if err != nil {
			log.Fatal(err)
		}
		var fresh []market.Candle
		if *backfill {
			since := time.Date(2017, 8, 17, 0, 0, 0, 0, time.UTC)
			if last := market.LastDate(have); last != "" {
				since, _ = time.Parse(time.DateOnly, last)
			}
			if fresh, err = f.BinanceBackfill(ctx, ins.Binance, since); err != nil {
				log.Fatalf("%s: %v", ins.Symbol, err)
			}
		}
		if *update {
			k, err := f.KrakenRecent(ctx, ins.Kraken)
			if err != nil {
				log.Fatalf("%s: %v", ins.Symbol, err)
			}
			// Kraken only fills days Binance did not: venues differ by a few bps.
			seen := map[string]bool{}
			for _, c := range have {
				seen[c.Date] = true
			}
			for _, c := range fresh {
				seen[c.Date] = true
			}
			for _, c := range k {
				if !seen[c.Date] {
					fresh = append(fresh, c)
				}
			}
		}
		all := market.Merge(have, fresh)
		if err := market.Save(*dir, ins.Symbol, all); err != nil {
			log.Fatal(err)
		}
		saved, _ := market.Load(*dir, ins.Symbol)
		first := ""
		if len(saved) > 0 {
			first = saved[0].Date
		}
		fmt.Printf("%-5s +%-4d total %-5d %s … %s\n", ins.Symbol, len(fresh), len(saved), first, market.LastDate(saved))
	}
}
