// Czech Building Notices — Apify Actor in Go.
//
// Harvests official notice boards (úřední desky, OFN open data listed in
// NKOD), keeps notices about building and land, redacts personal names and
// enriches k.ú./parcel references with RÚIAN (shared ruian package).
//
// Batch mode: one harvest, filtered notices to the dataset. With onlyNew the
// run returns only notices not seen by earlier runs (state in the named
// key-value store "czech-building-notices-state") — meant for a daily schedule.
//
// Standby mode: in-memory index refreshed every HARVEST_INTERVAL (default 3h),
//
//	POST /mcp                        MCP, 6 tools
//	GET  /search?kraj=…&types=zahajeni,rozhodnuti&since=7d&limit=20
//	GET  /notice?id=…
//	GET  /offices?kraj=…
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/czech-building-notices/internal/actor"
)

func userAgent() string {
	ua := "czech-building-notices/" + serverVersion + " (+https://github.com/lioilsources/apifysandbox/tree/master/actors/czech-building-notices"
	if c := os.Getenv("RUIAN_CONTACT"); c != "" { // set in Actor settings, never in code
		ua += "; " + c
	}
	return ua + ")"
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := actor.New()
	ua := userAgent()
	reg := ruian.NewRegistry(ruian.New(ruian.WithUserAgent(ua)))
	s := &service{h: newHarvester(a, reg, ua)}
	if a.Env.Standby {
		every := 3 * time.Hour
		if d, err := time.ParseDuration(os.Getenv("HARVEST_INTERVAL")); err == nil && d >= 15*time.Minute {
			every = d
		}
		go s.keepIndex(ctx, every)
		serve(ctx, a, s)
		return
	}
	if err := reg.Refresh(ctx); err != nil {
		log.Fatalf("gazetteer: %v", err)
	}
	if err := runBatch(ctx, a, s); err != nil {
		log.Fatal(err)
	}
}

type input struct {
	FilterArgs
	OnlyNew          bool   `json:"onlyNew"`
	ResolveParcels   *bool  `json:"resolveParcels"` // default true
	WithContext      bool   `json:"withContext"`
	MaxParcelLookups int    `json:"maxParcelLookups"`
	Limit            int    `json:"limit"`
	MaxBoards        int    `json:"maxBoards"` // testing: harvest only the first N boards
	StateKey         string `json:"stateKey"`  // separate onlyNew state per schedule/filter
}

func runBatch(ctx context.Context, a *actor.Actor, s *service) error {
	var in input
	if err := a.Input(ctx, &in); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	g := s.h.reg.Gazetteer()
	f, err := in.FilterArgs.compile(g, time.Now())
	if err != nil {
		return err
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 500
	}
	budget := in.MaxParcelLookups
	if budget <= 0 {
		budget = 1000
	}
	resolve := in.ResolveParcels == nil || *in.ResolveParcels
	store, err := s.h.loadState(ctx, in.StateKey)
	if err != nil {
		return fmt.Errorf("state store: %w", err)
	}
	runStart := time.Now().UTC()
	ns, st, err := s.h.harvest(ctx, harvestOpts{conditional: in.OnlyNew, maxBoards: in.MaxBoards})
	if err != nil {
		return err
	}
	log.Printf("harvest: %d boards (%d fetched, %d unchanged, %d failed), %d items → %d notices (%d duplicates), %.1f MB in %s",
		st.Boards, st.Fetched, st.Unchanged, st.Failed, st.Items, st.Notices, st.Duplicates, float64(st.Bytes)/1e6, st.Took.Round(time.Second))
	if len(f.describe) > 0 {
		log.Printf("filter: %s", strings.Join(f.describe, ", "))
	}
	pushed, lookups, skippedOld := 0, 0, 0
	for _, n := range ns {
		if in.OnlyNew && n.firstSeen.Before(runStart) {
			skippedOld++
			continue
		}
		if !f.match(n) {
			continue
		}
		if pushed >= limit {
			s.h.unsee(n.ID) // over the limit: the next onlyNew run returns it
			continue
		}
		if (resolve || f.needsParcels()) && lookups < budget {
			pts := 0
			if in.WithContext || f.protected {
				pts = 3
			}
			used, err := s.h.resolve(ctx, n, min(noticeLookupCap, budget-lookups), pts)
			lookups += used
			if err != nil && !errors.Is(err, ruian.ErrUpstream) {
				return err
			}
			if err != nil {
				log.Printf("RÚIAN: %v (notice %s kept unresolved)", err, n.ID)
			}
		}
		if !f.postMatch(n) {
			continue
		}
		if err := a.Push(ctx, n); err != nil {
			return err
		}
		pushed++
	}
	if err := a.Flush(ctx); err != nil {
		return err
	}
	if err := s.h.saveState(ctx, store, in.StateKey); err != nil {
		log.Printf("state not saved: %v", err) // the next onlyNew run will repeat some notices
	}
	log.Printf("done: %d notices pushed, %d RÚIAN lookups, %d already seen", pushed, lookups, skippedOld)
	return nil
}

// ---- standby HTTP

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func httpErr(w http.ResponseWriter, err error) {
	code := 500
	switch {
	case errors.Is(err, errBadInput):
		code = 400
	case errors.Is(err, errWarming), errors.Is(err, ruian.ErrUpstream):
		code = 503
	case errors.Is(err, context.DeadlineExceeded):
		code = 504
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func handle[T any](fn func(*http.Request) (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		defer cancel()
		v, err := fn(r.WithContext(ctx))
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, 200, v)
	}
}

func list(q string) []string {
	if q == "" {
		return nil
	}
	return strings.Split(q, ",")
}

func filterFromQuery(r *http.Request) FilterArgs {
	q := r.URL.Query()
	fa := FilterArgs{Kraj: q.Get("kraj"), ORP: q.Get("orp"), Obec: q.Get("obec"), KU: list(q.Get("ku")), Types: list(q.Get("types")),
		Keywords: list(q.Get("keywords")), Since: q.Get("since"), Until: q.Get("until"),
		IncludeExpired: q.Get("includeExpired") == "true", InProtectedArea: q.Get("inProtectedArea") == "true"}
	if b := list(q.Get("bbox")); len(b) == 4 {
		for _, x := range b {
			v, _ := strconv.ParseFloat(x, 64)
			fa.BBox = append(fa.BBox, v)
		}
	}
	return fa
}

func serve(ctx context.Context, a *actor.Actor, s *service) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		st := map[string]any{"service": "czech-building-notices", "version": serverVersion, "mcp": "/mcp",
			"endpoints": []string{"/search", "/watch", "/notice", "/offices"}}
		s.mu.RLock()
		if s.byID != nil {
			st["index"] = map[string]any{"notices": len(s.index), "boards": s.stats.Boards, "failed": s.stats.Failed, "builtAt": s.builtAt}
		} else {
			st["index"] = "building"
		}
		s.mu.RUnlock()
		writeJSON(w, 200, st)
	})
	mux.HandleFunc("GET /search", handle(func(r *http.Request) (*searchResult, error) {
		l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return s.search(r.Context(), searchArgs{FilterArgs: filterFromQuery(r), Limit: l, WithContext: r.URL.Query().Get("withContext") == "true"})
	}))
	mux.HandleFunc("GET /watch", handle(func(r *http.Request) (*watchResult, error) {
		l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return s.watch(r.Context(), watchArgs{FilterArgs: filterFromQuery(r), Cursor: r.URL.Query().Get("cursor"), Limit: l})
	}))
	mux.HandleFunc("GET /notice", handle(func(r *http.Request) (*Notice, error) {
		return s.notice(r.Context(), noticeArgs{ID: r.URL.Query().Get("id"), WithContext: r.URL.Query().Get("withContext") == "true"})
	}))
	mux.HandleFunc("GET /offices", handle(func(r *http.Request) ([]officeRow, error) {
		return s.offices(r.Context(), officesArgs{Kraj: r.URL.Query().Get("kraj"), ORP: r.URL.Query().Get("orp")})
	}))
	mux.HandleFunc("POST /mcp", s.mcpHandler)
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
