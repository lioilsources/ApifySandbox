package apifyrest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestActorPath(t *testing.T) {
	if got := ActorPath("compass/crawler-google-places"); got != "compass~crawler-google-places" {
		t.Fatal(got)
	}
	if got := StandbyURL("pringlesbitch/cz-reality-aggregator"); got != "https://pringlesbitch--cz-reality-aggregator.apify.actor" {
		t.Fatal(got)
	}
}

func TestDatasetItemsPaginates(t *testing.T) {
	const total = 25
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("missing auth header")
		}
		if r.URL.Query().Has("token") {
			t.Errorf("token must not be in the query")
		}
		off, _ := strconv.Atoi(r.URL.Query().Get("offset"))
		lim, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		var page []map[string]int
		for i := off; i < total && i < off+lim; i++ {
			page = append(page, map[string]int{"i": i})
		}
		json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()

	c := New("tok")
	c.BaseURL = srv.URL
	pages := 0
	n, err := c.DatasetItems(context.Background(), "ds", 10, func(p []json.RawMessage) error { pages++; return nil })
	if err != nil || n != total || pages != 3 {
		t.Fatalf("n=%d pages=%d err=%v", n, pages, err)
	}
}

func TestGetRecordNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"type":"record-not-found"}}`)
	}))
	defer srv.Close()
	c := New("")
	c.BaseURL = srv.URL
	var v any
	found, err := c.GetRecord(context.Background(), "kv", "seen", &v)
	if found || err != nil {
		t.Fatalf("found=%v err=%v", found, err)
	}
}
