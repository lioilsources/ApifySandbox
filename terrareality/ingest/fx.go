package ingest

import (
	"context"
	"encoding/xml"
	"fmt"
	"math"
	"net/http"
	"strconv"
)

// ECBDailyURL lists CZK, PLN and HUF against EUR; one fetch per day is enough.
const ECBDailyURL = "https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml"

// Rates are units of currency per 1 EUR on Date.
type Rates struct {
	Date  string             `json:"date"`
	PerEU map[string]float64 `json:"per_eur"`
}

func FetchECB(ctx context.Context, c *http.Client) (*Rates, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ECBDailyURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("ecb: HTTP %d", resp.StatusCode)
	}
	var doc struct {
		Cube struct {
			Cube struct {
				Time  string `xml:"time,attr"`
				Rates []struct {
					Currency string `xml:"currency,attr"`
					Rate     string `xml:"rate,attr"`
				} `xml:"Cube"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, err
	}
	r := &Rates{Date: doc.Cube.Cube.Time, PerEU: map[string]float64{"EUR": 1}}
	for _, x := range doc.Cube.Cube.Rates {
		f, err := strconv.ParseFloat(x.Rate, 64)
		if err == nil {
			r.PerEU[x.Currency] = f
		}
	}
	return r, nil
}

func (r *Rates) ToEUR(minor int64, cur string) (eurMinor int64, rate float64, err error) {
	rate, ok := r.PerEU[cur]
	if !ok || rate == 0 {
		return 0, 0, fmt.Errorf("fx: no rate for %s on %s", cur, r.Date)
	}
	return int64(math.Round(float64(minor) / rate)), rate, nil
}
