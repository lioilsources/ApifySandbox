package market

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Fetcher wraps the public, keyless endpoints of Binance and Kraken.
type Fetcher struct {
	HTTP *http.Client
}

func NewFetcher() *Fetcher {
	return &Fetcher{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (f *Fetcher) getJSON(ctx context.Context, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "olin-tradebot/0.1")
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

// BinanceBackfill pages daily klines from since until the last closed day.
// Weight is 2 per call out of 1200/min, so no throttling is needed here.
func (f *Fetcher) BinanceBackfill(ctx context.Context, pair string, since time.Time) ([]Candle, error) {
	var out []Candle
	start := since.UTC().Truncate(24 * time.Hour).UnixMilli()
	today := time.Now().UTC().Truncate(24 * time.Hour).UnixMilli()
	for start < today {
		q := url.Values{"symbol": {pair}, "interval": {"1d"}, "limit": {"1000"}, "startTime": {strconv.FormatInt(start, 10)}}
		var rows [][]any
		if err := f.getJSON(ctx, "https://api.binance.com/api/v3/klines?"+q.Encode(), &rows); err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			openMs, _ := r[0].(float64)
			if int64(openMs) >= today { // today's candle is still open
				continue
			}
			c := Candle{Date: Day(time.UnixMilli(int64(openMs)))}
			var err error
			for j, p := range []*float64{&c.Open, &c.High, &c.Low, &c.Close, &c.Volume} {
				if *p, err = strconv.ParseFloat(r[j+1].(string), 64); err != nil {
					return nil, err
				}
			}
			out = append(out, c)
		}
		last, _ := rows[len(rows)-1][0].(float64)
		start = int64(last) + 24*60*60*1000
		if len(rows) < 1000 {
			break
		}
	}
	return out, nil
}

// KrakenRecent returns up to 720 closed daily candles for a pair like XBTUSD.
func (f *Fetcher) KrakenRecent(ctx context.Context, pair string) ([]Candle, error) {
	var resp struct {
		Error  []string                   `json:"error"`
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := f.getJSON(ctx, "https://api.kraken.com/0/public/OHLC?pair="+pair+"&interval=1440", &resp); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("kraken %s: %v", pair, resp.Error)
	}
	today := Day(time.Now())
	var out []Candle
	for k, raw := range resp.Result {
		if k == "last" {
			continue
		}
		var rows [][]any // [time, open, high, low, close, vwap, volume, count]
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		for _, r := range rows {
			ts, _ := r[0].(float64)
			c := Candle{Date: Day(time.Unix(int64(ts), 0))}
			if c.Date >= today {
				continue
			}
			var err error
			for j, p := range []*float64{&c.Open, &c.High, &c.Low, &c.Close} {
				if *p, err = strconv.ParseFloat(r[j+1].(string), 64); err != nil {
					return nil, err
				}
			}
			if c.Volume, err = strconv.ParseFloat(r[6].(string), 64); err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

// KrakenTicker returns the last trade price for paper fills, keyed by request pair.
func (f *Fetcher) KrakenTicker(ctx context.Context, pairs []string) (map[string]float64, error) {
	var resp struct {
		Error  []string `json:"error"`
		Result map[string]struct {
			C []string `json:"c"`
		} `json:"result"`
	}
	q := url.Values{"pair": {join(pairs)}}
	if err := f.getJSON(ctx, "https://api.kraken.com/0/public/Ticker?"+q.Encode(), &resp); err != nil {
		return nil, err
	}
	if len(resp.Error) > 0 {
		return nil, fmt.Errorf("kraken ticker: %v", resp.Error)
	}
	// Kraken answers with its internal keys (XXBTZUSD); match by order-insensitive lookup.
	out := map[string]float64{}
	for _, p := range pairs {
		for k, v := range resp.Result {
			if krakenKeyMatches(k, p) && len(v.C) > 0 {
				out[p], _ = strconv.ParseFloat(v.C[0], 64)
			}
		}
	}
	return out, nil
}

func join(s []string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += ","
		}
		out += x
	}
	return out
}

// krakenKeyMatches maps XBTUSD→XXBTZUSD, ETHUSD→XETHZUSD, SOLUSD→SOLUSD.
func krakenKeyMatches(key, pair string) bool {
	if key == pair {
		return true
	}
	base, quote := pair[:len(pair)-3], pair[len(pair)-3:]
	return key == "X"+base+"Z"+quote
}
