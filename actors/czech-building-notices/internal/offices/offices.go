// Package offices resolves the authority behind a notice board (by IČO) to
// its seat municipality using ARES, the public business register API.
package offices

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

const aresURL = "https://ares.gov.cz/ekonomicke-subjekty-v-be/rest/ekonomicke-subjekty/"

// Office is the publisher of a board.
type Office struct {
	ICO      string    `json:"ico"`
	Name     string    `json:"name"`
	ObecCode int       `json:"obecCode"`
	ObecName string    `json:"obecName"`
	Checked  time.Time `json:"checked"`
	Err      string    `json:"err,omitempty"`
}

// Resolver caches ARES answers; Snapshot/Restore persist them between runs.
type Resolver struct {
	hc *http.Client
	ua string

	mu    sync.Mutex
	cache map[string]Office
	sem   chan struct{}
}

func New(hc *http.Client, ua string) *Resolver {
	return &Resolver{hc: hc, ua: ua, cache: map[string]Office{}, sem: make(chan struct{}, 2)}
}

// Restore loads a previous Snapshot.
func (r *Resolver) Restore(b []byte) {
	var m map[string]Office
	if json.Unmarshal(b, &m) == nil {
		r.mu.Lock()
		r.cache = m
		r.mu.Unlock()
	}
}

func (r *Resolver) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, _ := json.Marshal(r.cache)
	return b
}

// Lookup returns the office for an IČO; answers are reused for 30 days,
// failures for one day.
func (r *Resolver) Lookup(ctx context.Context, ico string) Office {
	r.mu.Lock()
	o, ok := r.cache[ico]
	r.mu.Unlock()
	ttl := 30 * 24 * time.Hour
	if o.Err != "" {
		ttl = 24 * time.Hour
	}
	if ok && time.Since(o.Checked) < ttl {
		return o
	}
	o = r.fetch(ctx, ico)
	r.mu.Lock()
	r.cache[ico] = o
	r.mu.Unlock()
	return o
}

func (r *Resolver) fetch(ctx context.Context, ico string) Office {
	o := Office{ICO: ico, Checked: time.Now().UTC()}
	if len(ico) != 8 {
		o.Err = "not an IČO"
		return o
	}
	select {
	case r.sem <- struct{}{}:
	case <-ctx.Done():
		o.Err = ctx.Err().Error()
		return o
	}
	defer func() { <-r.sem }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, aresURL+ico, nil)
	if err != nil {
		o.Err = err.Error()
		return o
	}
	req.Header.Set("User-Agent", r.ua)
	req.Header.Set("Accept", "application/json")
	resp, err := r.hc.Do(req)
	if err != nil {
		o.Err = err.Error()
		return o
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		o.Err = fmt.Sprintf("ARES HTTP %d", resp.StatusCode)
		return o
	}
	var a struct {
		ObchodniJmeno string `json:"obchodniJmeno"`
		Sidlo         struct {
			KodObce   int    `json:"kodObce"`
			NazevObce string `json:"nazevObce"`
		} `json:"sidlo"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&a); err != nil {
		o.Err = err.Error()
		return o
	}
	o.Name, o.ObecCode, o.ObecName = a.ObchodniJmeno, a.Sidlo.KodObce, a.Sidlo.NazevObce
	return o
}
