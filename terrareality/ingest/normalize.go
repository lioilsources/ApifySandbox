package ingest

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Mapping tells the normalizer where each fact lives in one Actor's output.
// Each field lists candidate keys (dot paths), first non-empty wins. Field
// names of the Store Actors are confirmed with fetch-actor-details and a smoke
// run; until then the candidates below are a best guess (see FINDINGS).
type Mapping struct {
	Source      []string // per-item portal key, e.g. "portal"; empty → FixedSource
	FixedSource string
	SourceMap   map[string]string // Actor's portal names → our source enum
	SourceID    []string
	URL         []string
	City        []string
	District    []string
	Lat, Lng    []string
	Category    []string
	Layout      []string
	Floor       []string
	Area        []string
	Price       []string
	Currency    []string
	Utilities   []string
	Available   []string
}

var common = Mapping{
	SourceID:  []string{"id", "listingId", "sourceId", "hash_id", "externalId"},
	URL:       []string{"url", "propertyUrl", "detailUrl", "link", "sourceUrl"},
	City:      []string{"city", "location.city", "address.city", "locality"},
	District:  []string{"district", "location.district", "address.district", "cityPart"},
	Lat:       []string{"lat", "latitude", "gps.lat", "location.lat", "coordinates.lat"},
	Lng:       []string{"lng", "lon", "longitude", "gps.lon", "location.lng", "coordinates.lng"},
	Category:  []string{"category", "propertyType", "type", "estateType"},
	Layout:    []string{"layout", "disposition", "rooms", "roomsCount", "numberOfRooms"},
	Floor:     []string{"floor", "floorNumber"},
	Area:      []string{"area", "surface", "areaM2", "usableArea", "size", "floorArea"},
	Price:     []string{"price", "priceAmount", "price.value", "rent", "priceTotal"},
	Currency:  []string{"currency", "priceCurrency", "price.currency"},
	Utilities: []string{"priceIncludesUtilities", "utilitiesIncluded"},
	Available: []string{"availableFrom", "available_from", "moveInDate"},
}

func with(m Mapping, f func(*Mapping)) Mapping { f(&m); return m }

// Mappings per Actor ID from the plan's table.
var Mappings = map[string]Mapping{
	"pringlesbitch/cz-reality-aggregator": with(common, func(m *Mapping) {
		m.Source = []string{"portal", "source"}
		m.SourceMap = map[string]string{
			"sreality": "sreality", "bezrealitky": "bezrealitky_cz", "bezrealitky_cz": "bezrealitky_cz",
			"idnes": "idnes", "realitymix": "realitymix", "reality.cz": "reality_cz", "reality_cz": "reality_cz",
			"nehnutelnosti": "nehnutelnosti_sk", "nehnutelnosti.sk": "nehnutelnosti_sk",
			"topreality": "topreality_sk", "topreality.sk": "topreality_sk", "reality.sk": "reality_sk", "reality_sk": "reality_sk",
		}
	}),
	// Plan IDs conversational_kermis/realtysync-aggregator, automation-lab/otodom-property-scraper and
	// unfenced-group/ingatlan-scraper do not exist in the Store (404, 2026-10-17); replaced by search-actors picks.
	"memo23/sreality-scraper":            with(common, func(m *Mapping) { m.FixedSource = "sreality" }),
	"nice_dev/sreality-listings-scraper": with(common, func(m *Mapping) { m.FixedSource = "sreality" }),
	"trev0n/otodom-scraper":              with(common, func(m *Mapping) { m.FixedSource = "otodom" }),
	"dadhalfdev/otodom-scraper":          with(common, func(m *Mapping) { m.FixedSource = "otodom" }),
	"bakos_bence/ingatlan-com":           with(common, func(m *Mapping) { m.FixedSource = "ingatlan" }),
	"studio-amba/ingatlan-scraper":       with(common, func(m *Mapping) { m.FixedSource = "ingatlan" }),
}

var ErrSkip = errors.New("skip")

// Normalize maps one raw Actor item. It never copies free text beyond the
// whitelisted facts, so photos, descriptions and contacts cannot leak.
func Normalize(m Mapping, raw json.RawMessage, fx *Rates, now time.Time) (*Listing, error) {
	var item map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&item); err != nil {
		return nil, err
	}
	src := m.FixedSource
	if src == "" {
		p := strings.ToLower(str(item, m.Source))
		src = m.SourceMap[p]
		if src == "" {
			return nil, fmt.Errorf("%w: unknown portal %q", ErrSkip, p)
		}
	}
	country := Sources[src]
	l := &Listing{
		Source: src, SourceID: str(item, m.SourceID), SourceURL: str(item, m.URL),
		ThirdParty: true, Country: country, Transaction: "rent",
		City: str(item, m.City), District: str(item, m.District),
		Lat: num(item, m.Lat), Lng: num(item, m.Lng),
		Category: category(str(item, m.Category)), Layout: str(item, m.Layout),
		AreaM2: num(item, m.Area), FetchedAt: now, LastSeenAt: now, Active: true,
	}
	if l.SourceID == "" || l.SourceURL == "" {
		return nil, fmt.Errorf("%w: %s item without id or url", ErrSkip, src)
	}
	l.ID = ListingID(src, l.SourceID)
	l.Rooms, l.Kitchen = ParseLayout(l.Layout)
	if l.Category == "flat" && l.Rooms == nil {
		if r := num(item, m.Layout); r != nil {
			n := int(*r)
			l.Rooms = &n
		}
	}
	if f := num(item, m.Floor); f != nil {
		n := int(*f)
		l.Floor = &n
	}
	p := num(item, m.Price)
	if p == nil || *p <= 0 {
		return nil, fmt.Errorf("%w: %s/%s without price", ErrSkip, src, l.SourceID)
	}
	l.PriceAmount = toMinor(*p)
	l.PriceCurrency = strings.ToUpper(str(item, m.Currency))
	switch l.PriceCurrency {
	case "KČ", "KC":
		l.PriceCurrency = "CZK"
	case "€":
		l.PriceCurrency = "EUR"
	case "ZŁ", "ZL":
		l.PriceCurrency = "PLN"
	case "FT":
		l.PriceCurrency = "HUF"
	case "":
		l.PriceCurrency = currencyByCountry[country]
	}
	if fx != nil {
		eur, rate, err := fx.ToEUR(l.PriceAmount, l.PriceCurrency)
		if err != nil {
			return nil, err
		}
		l.PriceEUR, l.FXRate, l.FXDate = eur, rate, fx.Date
	}
	if b, ok := lookup(item, m.Utilities).(bool); ok {
		l.PriceIncludesUtilities = &b
	}
	if s := str(item, m.Available); s != "" {
		l.AvailableFrom = &s
	}
	l.PriceHistory = []PricePt{{At: now, Amount: l.PriceAmount}}
	return l, nil
}

func category(s string) string {
	s = strings.ToLower(s)
	switch {
	case s == "", strings.Contains(s, "flat"), strings.Contains(s, "byt"), strings.Contains(s, "apartment"),
		strings.Contains(s, "mieszkan"), strings.Contains(s, "lakás"), strings.Contains(s, "lakas"):
		return "flat"
	case strings.Contains(s, "house"), strings.Contains(s, "dům"), strings.Contains(s, "dom"), strings.Contains(s, "ház"):
		return "house"
	case strings.Contains(s, "room"), strings.Contains(s, "pokój"), strings.Contains(s, "pokoj"), strings.Contains(s, "szoba"):
		return "room"
	default:
		return "commercial"
	}
}

func lookup(item map[string]any, keys []string) any {
	for _, k := range keys {
		var cur any = item
		for _, part := range strings.Split(k, ".") {
			mm, ok := cur.(map[string]any)
			if !ok {
				cur = nil
				break
			}
			cur = mm[part]
		}
		if cur != nil && cur != "" {
			return cur
		}
	}
	return nil
}

func str(item map[string]any, keys []string) string {
	switch v := lookup(item, keys).(type) {
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case nil:
		return ""
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func num(item map[string]any, keys []string) *float64 { return ParseNumber(lookup(item, keys)) }
