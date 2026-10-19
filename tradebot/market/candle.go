// Package market holds daily candles for the tradebot universe: Binance for
// history (2017+), Kraken for the daily update and paper fills. Storage is one
// CSV per symbol, rewritten atomically; a few thousand rows per symbol.
package market

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"
)

// Candle is one UTC day. Volume is in base units.
type Candle struct {
	Date   string // YYYY-MM-DD
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// Instrument maps the internal symbol to venue pairs.
type Instrument struct {
	Symbol  string // BTC
	Binance string // BTCUSDT
	Kraken  string // XBTUSD (request form)
}

// Universe is fixed to coins that already traded in 2020, so the backtest is
// not biased by picking today's winners. Order is irrelevant.
var Universe = []Instrument{
	{"BTC", "BTCUSDT", "XBTUSD"},
	{"ETH", "ETHUSDT", "ETHUSD"},
	{"ADA", "ADAUSDT", "ADAUSD"},
	{"XRP", "XRPUSDT", "XRPUSD"},
	{"LTC", "LTCUSDT", "LTCUSD"},
	{"LINK", "LINKUSDT", "LINKUSD"},
	{"DOGE", "DOGEUSDT", "XDGUSD"},
	{"DOT", "DOTUSDT", "DOTUSD"},
	{"SOL", "SOLUSDT", "SOLUSD"},
	{"ATOM", "ATOMUSDT", "ATOMUSD"},
}

func path(dir, symbol string) string { return filepath.Join(dir, symbol+".csv") }

// Load reads a symbol's candles; a missing file is an empty series.
func Load(dir, symbol string) ([]Candle, error) {
	f, err := os.Open(path(dir, symbol))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		return nil, err
	}
	var out []Candle
	for i, r := range rows {
		if i == 0 || len(r) < 6 {
			continue
		}
		c := Candle{Date: r[0]}
		for j, p := range []*float64{&c.Open, &c.High, &c.Low, &c.Close, &c.Volume} {
			if *p, err = strconv.ParseFloat(r[j+1], 64); err != nil {
				return nil, fmt.Errorf("%s line %d: %w", symbol, i+1, err)
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// Save writes the series sorted by date with duplicates resolved in favour of
// the later entry, through a temp file and rename.
func Save(dir, symbol string, candles []Candle) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	by := map[string]Candle{}
	for _, c := range candles {
		by[c.Date] = c
	}
	dates := make([]string, 0, len(by))
	for d := range by {
		dates = append(dates, d)
	}
	sort.Strings(dates)
	tmp := path(dir, symbol) + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	w.Write([]string{"date", "open", "high", "low", "close", "volume"})
	for _, d := range dates {
		c := by[d]
		w.Write([]string{d, ff(c.Open), ff(c.High), ff(c.Low), ff(c.Close), ff(c.Volume)})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path(dir, symbol))
}

func ff(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

// Merge appends fresh candles to base, later data wins on the same date.
func Merge(base, fresh []Candle) []Candle {
	out := append([]Candle(nil), base...)
	return append(out, fresh...)
}

// LastDate is the newest date in a sorted series, "" when empty.
func LastDate(c []Candle) string {
	if len(c) == 0 {
		return ""
	}
	return c[len(c)-1].Date
}

// Day formats a UTC day the way candles are keyed.
func Day(t time.Time) string { return t.UTC().Format(time.DateOnly) }
