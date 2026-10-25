// Package ruian answers parcel, address and territory questions from the
// Czech RÚIAN register (ČÚZK) in a shape an LLM agent can use directly:
// Czech names instead of codes, WGS84 centroids instead of S-JTSK polygons,
// and parcel references pulled out of free Czech text.
//
// Data source: ČÚZK ArcGIS REST service RUIAN/MapServer, public and keyless.
// § 62(1) of Act 111/2009 Sb. allows copying the whole register by any means.
// RÚIAN holds no owners; this package never tries to find one.
package ruian

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const DefaultBaseURL = "https://ags.cuzk.cz/arcgis/rest/services/RUIAN/MapServer"

// Layer ids of RUIAN/MapServer (stable since 2023, check with ?f=json).
const (
	LayerParcelPoint = 0
	LayerAddress     = 1
	LayerBuilding    = 3
	LayerParcel      = 5
	LayerKU          = 7
	LayerObec        = 12
	LayerPOU         = 13
	LayerORP         = 14
	LayerOkres       = 15
	LayerKraj        = 17
)

// ErrUpstream wraps failures of the ČÚZK service so callers can answer 503.
var ErrUpstream = errors.New("ČÚZK RÚIAN service unavailable")

type Client struct {
	base string
	ua   string
	hc   *http.Client
	sem  chan struct{}

	mu    sync.Mutex
	cache map[string]cacheEntry
	ttl   time.Duration
	max   int
}

type cacheEntry struct {
	val []byte
	exp time.Time
}

type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.base = strings.TrimRight(u, "/") } }
func WithUserAgent(ua string) Option       { return func(c *Client) { c.ua = ua } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.hc = h } }
func WithConcurrency(n int) Option         { return func(c *Client) { c.sem = make(chan struct{}, max(1, n)) } }

// WithCache sets the response cache TTL and entry cap (0 disables caching).
func WithCache(ttl time.Duration, entries int) Option {
	return func(c *Client) { c.ttl, c.max = ttl, entries }
}

func New(opts ...Option) *Client {
	c := &Client{
		base:  DefaultBaseURL,
		ua:    "ruian-go (+https://github.com/lioilsources/apifysandbox)",
		hc:    &http.Client{Timeout: 20 * time.Second},
		sem:   make(chan struct{}, 4),
		cache: map[string]cacheEntry{},
		ttl:   24 * time.Hour,
		max:   20000,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// esriError is how ArcGIS reports a failed query inside an HTTP 200.
type esriError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	q.Set("f", firstNonEmpty(q.Get("f"), "json"))
	u := c.base + path + "?" + q.Encode()
	if b, ok := c.cached(u); ok {
		return b, nil
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.sem }()

	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(time.Duration(attempt*attempt) * 400 * time.Millisecond):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		b, retry, err := c.do(ctx, u)
		if err == nil {
			c.store(u, b)
			return b, nil
		}
		last = err
		if !retry {
			break
		}
	}
	return nil, last
}

func (c *Client) do(ctx context.Context, u string) (body []byte, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		return nil, true, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, true, fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode >= 500 || resp.StatusCode == 429, fmt.Errorf("%w: HTTP %d", ErrUpstream, resp.StatusCode)
	}
	var e struct {
		Error *esriError `json:"error"`
	}
	if len(b) > 0 && b[0] == '{' && json.Unmarshal(b, &e) == nil && e.Error != nil {
		// ArcGIS answers transient overload with code 400/500 "Failed to execute query".
		return nil, true, fmt.Errorf("%w: %d %s", ErrUpstream, e.Error.Code, e.Error.Message)
	}
	return b, false, nil
}

func (c *Client) cached(k string) ([]byte, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.cache[k]
	if !ok || time.Now().After(e.exp) {
		return nil, false
	}
	return e.val, true
}

func (c *Client) store(k string, v []byte) {
	if c.ttl <= 0 || len(v) > 4<<20 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cache) >= c.max {
		now := time.Now()
		for key, e := range c.cache { // drop expired first, then arbitrary half
			if now.After(e.exp) {
				delete(c.cache, key)
			}
		}
		if len(c.cache) >= c.max {
			n := 0
			for key := range c.cache {
				delete(c.cache, key)
				if n++; n >= c.max/2 {
					break
				}
			}
		}
	}
	c.cache[k] = cacheEntry{val: v, exp: time.Now().Add(c.ttl)}
}

// feature is one ArcGIS query result row.
type feature struct {
	Attributes json.RawMessage `json:"attributes"`
	Geometry   json.RawMessage `json:"geometry"`
}

type queryResult struct {
	Features              []feature `json:"features"`
	ExceededTransferLimit bool      `json:"exceededTransferLimit"`
}

// query runs /<layer>/query; geometry comes back in WGS84.
func (c *Client) query(ctx context.Context, layer int, q url.Values) (*queryResult, error) {
	if q.Get("outSR") == "" {
		q.Set("outSR", "4326")
	}
	b, err := c.get(ctx, fmt.Sprintf("/%d/query", layer), q)
	if err != nil {
		return nil, err
	}
	var r queryResult
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("%w: bad JSON: %v", ErrUpstream, err)
	}
	return &r, nil
}

func firstNonEmpty(s ...string) string {
	for _, v := range s {
		if v != "" {
			return v
		}
	}
	return ""
}

// sqlString quotes a value for an ArcGIS where clause.
func sqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
