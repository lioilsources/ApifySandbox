// RÚIAN Parcel & Address MCP — Apify Actor in Go.
//
// Standby mode (APIFY_META_ORIGIN=STANDBY): HTTP server with
//
//	POST /mcp                         MCP (Streamable HTTP, JSON responses), 8 tools
//	GET  /parcel?ku=Hrutov&number=st.%201
//	GET  /address?q=Veveří%2095,%20Brno
//	GET  /context?lat=50.03&lon=13.87
//	GET  /nearby?lat=…&lon=…&radiusM=100&what=buildings
//	POST /extract                     body: text/plain Czech text
//
// Batch mode: reads INPUT (parcels, addresses, points, text, exportKu) and
// pushes one dataset item per answer.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/lioilsources/apifysandbox/ruian"
	"github.com/lioilsources/ruian-parcel-mcp/internal/actor"
)

func userAgent() string {
	ua := "ruian-parcel-mcp/" + serverVersion + " (+https://github.com/lioilsources/apifysandbox/tree/master/actors/ruian-parcel-mcp"
	if c := os.Getenv("RUIAN_CONTACT"); c != "" { // set in Actor settings, never in code
		ua += "; " + c
	}
	return ua + ")"
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	a := actor.New()
	s := &service{reg: ruian.NewRegistry(ruian.New(ruian.WithUserAgent(userAgent()))), act: a}
	if a.Env.Standby {
		go s.keepGazetteer(ctx)
		serve(ctx, a, s)
		return
	}
	if err := s.reg.Refresh(ctx); err != nil {
		log.Fatalf("gazetteer: %v", err)
	}
	if err := runBatch(ctx, a, s); err != nil {
		log.Fatal(err)
	}
}

// keepGazetteer loads the gazetteer, retries every minute until it works,
// then refreshes it daily.
func (s *service) keepGazetteer(ctx context.Context) {
	wait := time.Duration(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		t0 := time.Now()
		if err := s.reg.Refresh(ctx); err != nil {
			log.Printf("gazetteer: %v", err)
			wait = time.Minute
			continue
		}
		log.Printf("gazetteer loaded %v in %s", s.reg.Gazetteer().Size(), time.Since(t0).Round(time.Millisecond))
		wait = 24 * time.Hour
	}
}

// ---- batch

type pointInput struct {
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`
}

type input struct {
	Parcels       []parcelArgs `json:"parcels"`
	Text          string       `json:"text"`
	KUHint        string       `json:"kuHint"`
	Addresses     []string     `json:"addresses"`
	Points        []pointInput `json:"points"`
	ExportKU      string       `json:"exportKu"`
	WithGeometry  bool         `json:"withGeometry"`
	WithBuildings *bool        `json:"withBuildings"`
	WithContext   bool         `json:"withContext"`
	Limit         int          `json:"limit"`
}

// item flattens a parcel/address/context answer into one dataset row.
func item(kind, query string, v any) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	m["type"] = kind
	m["query"] = query
	return m
}

func runBatch(ctx context.Context, a *actor.Actor, s *service) error {
	var in input
	if err := a.Input(ctx, &in); err != nil {
		return fmt.Errorf("input: %w", err)
	}
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	t0 := time.Now()
	room := func() bool { return a.Written()+pending(a) < limit }
	push := func(m map[string]any) error {
		if !room() {
			return nil
		}
		return a.Push(ctx, m)
	}
	var problems []string
	note := func(q string, err error) {
		problems = append(problems, fmt.Sprintf("%s: %v", q, err))
		log.Printf("%s: %v", q, err)
	}

	for _, pa := range in.Parcels {
		if !room() {
			break
		}
		pa.WithGeometry, pa.WithContext = pa.WithGeometry || in.WithGeometry, pa.WithContext || in.WithContext
		if pa.WithBuildings == nil {
			pa.WithBuildings = in.WithBuildings
		}
		q := pa.KU + " " + pa.Number
		res, err := s.resolveParcel(ctx, pa)
		if err != nil {
			note(q, err)
			continue
		}
		if len(res.Parcels) == 0 {
			if err := push(item("parcel-not-found", q, res)); err != nil {
				return err
			}
			continue
		}
		for _, p := range res.Parcels {
			if err := push(item("parcel", q, p)); err != nil {
				return err
			}
		}
	}
	if in.Text != "" && room() {
		res, err := s.extract(ctx, extractArgs{Text: in.Text, KUHint: in.KUHint, WithBuildings: boolOr(in.WithBuildings, false), WithContext: in.WithContext}, limit)
		if err != nil {
			note("text", err)
		} else {
			for _, r := range res.References {
				if len(r.Parcels) == 0 {
					if err := push(item("text-reference", r.Ref.Raw, map[string]any{"ref": r.Ref})); err != nil {
						return err
					}
				}
				for _, p := range r.Parcels {
					m := item("text-parcel", r.Ref.Raw, p)
					m["ref"] = r.Ref
					if r.Note != "" {
						m["note"] = r.Note
					}
					if err := push(m); err != nil {
						return err
					}
				}
			}
		}
	}
	for _, q := range in.Addresses {
		if !room() {
			break
		}
		res, err := s.resolveAddress(ctx, addressArgs{Query: q, Limit: 1, WithBuilding: in.WithBuildings})
		if err != nil {
			note(q, err)
			continue
		}
		if len(res.Hits) == 0 {
			if err := push(item("address-not-found", q, res)); err != nil {
				return err
			}
			continue
		}
		m := item("address", q, res.Hits[0])
		m["parsed"] = res.Parsed
		if in.WithContext && res.Hits[0].Point != nil {
			if c, err := s.reg.ContextAt(ctx, *res.Hits[0].Point); err == nil {
				m["context"] = c
			}
		}
		if err := push(m); err != nil {
			return err
		}
	}
	for _, pt := range in.Points {
		if !room() {
			break
		}
		q := strconv.FormatFloat(pt.Lat, 'f', 6, 64) + "," + strconv.FormatFloat(pt.Lon, 'f', 6, 64)
		c, err := s.parcelContext(ctx, contextArgs{Lat: pt.Lat, Lon: pt.Lon})
		if err != nil {
			note(q, err)
			continue
		}
		if err := push(item("context", q, c)); err != nil {
			return err
		}
	}
	if in.ExportKU != "" {
		code, _ := strconv.Atoi(in.ExportKU)
		res, err := s.export(ctx, exportArgs{KU: in.ExportKU, KUCode: code})
		if err != nil {
			note("export "+in.ExportKU, err)
		} else {
			log.Printf("export: %d parcels, %d bytes → %s", res.Parcels, res.Bytes, res.URL)
			if err := push(item("export", in.ExportKU, res)); err != nil {
				return err
			}
		}
	}
	if err := a.Flush(ctx); err != nil {
		return err
	}
	log.Printf("done: %d items in %s, %d problems", a.Written(), time.Since(t0).Round(time.Millisecond), len(problems))
	if a.Written() == 0 && len(problems) > 0 {
		return fmt.Errorf("nothing resolved: %v", problems)
	}
	return nil
}

// pending is the number of buffered, not yet flushed items.
func pending(a *actor.Actor) int { return a.Buffered() }

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
	case errors.Is(err, ruian.ErrUpstream):
		code = 503
	case errors.Is(err, context.DeadlineExceeded):
		code = 504
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func qf(r *http.Request, k string) float64 {
	v, _ := strconv.ParseFloat(r.URL.Query().Get(k), 64)
	return v
}

func qb(r *http.Request, k string) *bool {
	v := r.URL.Query().Get(k)
	if v == "" {
		return nil
	}
	b := v == "1" || v == "true"
	return &b
}

func handle[T any](fn func(*http.Request) (T, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		defer cancel()
		v, err := fn(r.WithContext(ctx))
		if err != nil {
			httpErr(w, err)
			return
		}
		writeJSON(w, 200, v)
	}
}

func serve(ctx context.Context, a *actor.Actor, s *service) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		// Also answers Apify's readiness probe; must not wait for ČÚZK.
		st := map[string]any{"service": "ruian-parcel-mcp", "version": serverVersion, "mcp": "/mcp",
			"endpoints": []string{"/parcel", "/address", "/context", "/nearby", "/extract"}}
		if g := s.reg.Gazetteer(); g != nil {
			st["gazetteer"] = g.Size()
		} else {
			st["gazetteer"] = "loading"
		}
		writeJSON(w, 200, st)
	})
	mux.HandleFunc("GET /parcel", handle(func(r *http.Request) (*parcelResult, error) {
		q := r.URL.Query()
		code, _ := strconv.Atoi(q.Get("kuCode"))
		return s.resolveParcel(r.Context(), parcelArgs{KU: q.Get("ku"), KUCode: code, Number: q.Get("number"),
			WithGeometry: q.Get("withGeometry") == "true", WithBuildings: qb(r, "withBuildings"), WithContext: q.Get("withContext") == "true"})
	}))
	mux.HandleFunc("GET /address", handle(func(r *http.Request) (*addressResult, error) {
		l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return s.resolveAddress(r.Context(), addressArgs{Query: r.URL.Query().Get("q"), Limit: l, WithBuilding: qb(r, "withBuilding")})
	}))
	mux.HandleFunc("GET /context", handle(func(r *http.Request) (*ruian.Context, error) {
		id, _ := strconv.ParseInt(r.URL.Query().Get("parcelId"), 10, 64)
		return s.parcelContext(r.Context(), contextArgs{Lat: qf(r, "lat"), Lon: qf(r, "lon"), ParcelID: id})
	}))
	mux.HandleFunc("GET /nearby", handle(func(r *http.Request) ([]ruian.NearbyItem, error) {
		l, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		return s.nearby(r.Context(), nearbyArgs{Lat: qf(r, "lat"), Lon: qf(r, "lon"), RadiusM: qf(r, "radiusM"), What: r.URL.Query().Get("what"), Limit: l})
	}))
	mux.HandleFunc("POST /extract", handle(func(r *http.Request) (*extractResult, error) {
		b, err := io.ReadAll(io.LimitReader(r.Body, maxTextBytes+1))
		if err != nil {
			return nil, err
		}
		return s.extract(r.Context(), extractArgs{Text: string(b), KUHint: r.URL.Query().Get("kuHint"),
			WithBuildings: r.URL.Query().Get("withBuildings") == "true", WithContext: r.URL.Query().Get("withContext") == "true"}, maxLookups)
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
