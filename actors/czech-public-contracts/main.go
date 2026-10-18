// Czech Public Contracts Scraper — Apify Actor in Go.
//
// Batch mode: reads INPUT, streams Registr smluv dumps, pushes matching
// contracts to the default dataset (pay-per-result counts dataset items).
//
// Standby mode (APIFY_META_ORIGIN=STANDBY): HTTP server with
//
//	GET  /search?ico=…&keywords=a,b&dateFrom=…&dateTo=…&limit=…   JSON array
//	POST /mcp                                                     MCP (Streamable HTTP, JSON responses)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lioilsources/czech-public-contracts/internal/actor"
	"github.com/lioilsources/czech-public-contracts/internal/search"
)

const (
	standbyMaxDays  = 31
	standbyMaxLimit = 50
)

var hc = &http.Client{Timeout: 30 * time.Minute} // monthly dumps take minutes to stream

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := actor.New()
	if a.Env.Standby {
		serve(ctx, a)
		return
	}
	if err := runBatch(ctx, a); err != nil {
		log.Fatal(err)
	}
}

func runBatch(ctx context.Context, a *actor.Actor) error {
	var in search.Input
	if err := a.Input(ctx, &in); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	t0 := time.Now()
	dumps, err := search.Scan(ctx, hc, in, time.Now().UTC(), func(s string) { log.Print(s) },
		func(it search.Item) (bool, error) { return true, a.Push(ctx, it) })
	if ferr := a.Flush(ctx); err == nil {
		err = ferr
	}
	log.Printf("done: %d contracts from %d dumps in %s", a.Written(), dumps, time.Since(t0).Round(time.Second))
	return err
}

// standbySearch runs a bounded scan for one HTTP or MCP request.
func standbySearch(ctx context.Context, in search.Input) ([]search.Item, error) {
	now := time.Now().UTC()
	from, to, err := in.Normalize(now)
	if err != nil {
		return nil, err
	}
	if to.Sub(from) > standbyMaxDays*24*time.Hour {
		return nil, fmt.Errorf("standby queries cover at most %d days; use a batch run for longer ranges", standbyMaxDays)
	}
	if in.Limit > standbyMaxLimit {
		in.Limit = standbyMaxLimit
	}
	items := []search.Item{}
	_, err = search.Scan(ctx, hc, in, now, nil, func(it search.Item) (bool, error) {
		items = append(items, it)
		return true, nil
	})
	return items, err
}

func inputFromQuery(q map[string][]string) search.Input {
	get := func(k string) string {
		if v := q[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	split := func(s string) []string {
		if s == "" {
			return nil
		}
		return strings.Split(s, ",")
	}
	in := search.Input{ICO: split(get("ico")), Name: get("name"), Keywords: split(get("keywords")), DateFrom: get("dateFrom"), DateTo: get("dateTo")}
	in.Limit, _ = strconv.Atoi(get("limit"))
	in.MinValue, _ = strconv.ParseFloat(get("minValueCzk"), 64)
	return in
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func serve(ctx context.Context, a *actor.Actor) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// Also answers Apify's readiness probe (x-apify-container-server-readiness-probe).
		writeJSON(w, 200, map[string]string{"service": "czech-public-contracts", "search": "/search", "mcp": "/mcp"})
	})
	mux.HandleFunc("GET /search", func(w http.ResponseWriter, r *http.Request) {
		items, err := standbySearch(r.Context(), inputFromQuery(r.URL.Query()))
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("POST /mcp", mcpHandler)
	mux.HandleFunc("GET /mcp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "SSE stream not supported; POST JSON-RPC", http.StatusMethodNotAllowed)
	})
	srv := &http.Server{Addr: fmt.Sprintf(":%d", a.Env.Port), Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(sctx)
	}()
	log.Printf("standby on :%d", a.Env.Port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
