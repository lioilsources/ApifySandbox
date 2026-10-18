// Package ingest normalizes third-party rental listings from Apify Store Actors
// into the TerraReality listing model (APIFY_PLAN Fáze 1b).
//
// Legal minimum is enforced here, not in the UI: the model has no field for
// photos, descriptions, advertiser names or phones, so whatever an Actor
// returns beyond the whitelisted facts is dropped on decode.
package ingest

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Listing struct {
	ID                     string     `json:"id"` // stable UUIDv5-like hash of (source, source_id)
	Source                 string     `json:"source"`
	SourceID               string     `json:"source_id"`
	SourceURL              string     `json:"source_url"`
	ThirdParty             bool       `json:"third_party"`
	Country                string     `json:"country"`
	City                   string     `json:"city,omitempty"`
	District               string     `json:"district,omitempty"`
	Lat                    *float64   `json:"lat,omitempty"`
	Lng                    *float64   `json:"lng,omitempty"`
	Transaction            string     `json:"transaction"`
	Category               string     `json:"category"`
	Layout                 string     `json:"layout,omitempty"`
	Rooms                  *int       `json:"rooms,omitempty"`
	Kitchen                *bool      `json:"kitchen,omitempty"` // true = separate kitchen ("+1"), false = kitchenette ("+kk")
	Floor                  *int       `json:"floor,omitempty"`
	AreaM2                 *float64   `json:"area_m2,omitempty"`
	PriceAmount            int64      `json:"price_amount"` // minor units, like TerraReality money values
	PriceCurrency          string     `json:"price_currency"`
	PriceEUR               int64      `json:"price_eur"` // minor units, converted at FXDate
	FXRate                 float64    `json:"fx_rate"`   // units of PriceCurrency per 1 EUR
	FXDate                 string     `json:"fx_date"`
	PriceIncludesUtilities *bool      `json:"price_includes_utilities"`
	AvailableFrom          *string    `json:"available_from"`
	FetchedAt              time.Time  `json:"fetched_at"`
	LastSeenAt             time.Time  `json:"last_seen_at"`
	MissedRuns             int        `json:"missed_runs"`
	Active                 bool       `json:"active"`
	PriceHistory           []PricePt  `json:"price_history,omitempty"`
	InactiveSince          *time.Time `json:"inactive_since,omitempty"`
}

type PricePt struct {
	At     time.Time `json:"at"`
	Amount int64     `json:"amount"`
}

var Sources = map[string]string{
	"sreality": "CZ", "bezrealitky_cz": "CZ", "idnes": "CZ", "realitymix": "CZ", "reality_cz": "CZ",
	"nehnutelnosti_sk": "SK", "topreality_sk": "SK", "reality_sk": "SK",
	"otodom": "PL", "ingatlan": "HU",
}

var currencyByCountry = map[string]string{"CZ": "CZK", "SK": "EUR", "PL": "PLN", "HU": "HUF"}

// ListingID derives a deterministic ID so reruns upsert instead of duplicating.
func ListingID(source, sourceID string) string {
	h := sha1.Sum([]byte(source + "\x00" + sourceID))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}

var (
	reCZLayout = regexp.MustCompile(`(?i)(\d+)\s*\+\s*(kk|1)`)
	reRooms    = regexp.MustCompile(`(?i)(\d+)\s*(pokoj|pokoje|pokojů|izb|izby|izbový|room|rooms|szoba|szobás)`)
	reNum      = regexp.MustCompile(`-?\d+(?:[.,]\d+)?`)
)

// ParseLayout maps "2+kk", "3+1", "3 pokoje", "2 szoba", "garsoniéra" to rooms + kitchen.
func ParseLayout(s string) (rooms *int, kitchen *bool) {
	s = strings.TrimSpace(s)
	if m := reCZLayout.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		k := strings.EqualFold(m[2], "1")
		return &n, &k
	}
	if m := reRooms.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		return &n, nil
	}
	low := strings.ToLower(s)
	if strings.Contains(low, "garson") || strings.Contains(low, "kawalerka") || strings.Contains(low, "studio") {
		n, k := 1, false
		return &n, &k
	}
	if n, err := strconv.Atoi(low); err == nil && n > 0 && n < 20 {
		return &n, nil
	}
	return nil, nil
}

// ParseNumber reads "18 500 Kč", "1.250,50", "65 m²" → float.
func ParseNumber(v any) *float64 {
	switch x := v.(type) {
	case float64:
		return &x
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return nil
		}
		return &f
	case string:
		s := strings.NewReplacer(" ", "", " ", "", " ", "").Replace(x)
		// "1.250,50" → "1250.50"; "1,250.50" → "1250.50"; "18.500" (thousands) → "18500"
		if strings.Contains(s, ",") && strings.Contains(s, ".") {
			if strings.LastIndex(s, ",") > strings.LastIndex(s, ".") {
				s = strings.ReplaceAll(strings.ReplaceAll(s, ".", ""), ",", ".")
			} else {
				s = strings.ReplaceAll(s, ",", "")
			}
		} else if i := strings.LastIndex(s, "."); i >= 0 && len(s)-i-1 == 3 && strings.Count(s, ".") >= 1 {
			s = strings.ReplaceAll(s, ".", "")
		} else {
			s = strings.ReplaceAll(s, ",", ".")
		}
		m := reNum.FindString(s)
		if m == "" {
			return nil
		}
		f, err := strconv.ParseFloat(m, 64)
		if err != nil {
			return nil
		}
		return &f
	}
	return nil
}

func toMinor(f float64) int64 { return int64(math.Round(f * 100)) }
