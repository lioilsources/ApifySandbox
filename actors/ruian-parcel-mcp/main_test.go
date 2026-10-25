package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lioilsources/apifysandbox/ruian"
)

func TestToolsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, tl := range tools {
		name, _ := tl.def["name"].(string)
		if name == "" || seen[name] {
			t.Fatalf("bad or duplicate tool name %q", name)
		}
		seen[name] = true
		desc, _ := tl.def["description"].(string)
		if len(desc) < 40 {
			t.Errorf("%s: description too short", name)
		}
		if _, err := json.Marshal(tl.def["inputSchema"]); err != nil {
			t.Errorf("%s: schema: %v", name, err)
		}
	}
	if len(seen) != 8 {
		t.Errorf("want 8 tools, got %d", len(seen))
	}
}

func mcpCall(t *testing.T, s *service, body string) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	s.mcpHandler(rec, httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body)))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestMCPWithoutGazetteer(t *testing.T) {
	s := &service{reg: ruian.NewRegistry(ruian.New())}
	if r := mcpCall(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`); r["result"] == nil {
		t.Fatalf("tools/list: %v", r)
	}
	// Before the gazetteer loads, tools answer isError with a retry hint instead of hanging.
	r := mcpCall(t, s, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"resolve_parcel","arguments":{"ku":"Hrutov","number":"1"}}}`)
	res, _ := r["result"].(map[string]any)
	if res["isError"] != true {
		t.Errorf("expected isError, got %v", r)
	}
	// list_codebooks needs no network.
	r = mcpCall(t, s, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_codebooks","arguments":{}}}`)
	res, _ = r["result"].(map[string]any)
	if res["isError"] == true || !bytes.Contains([]byte(r["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)), []byte("zastavěná plocha")) {
		t.Errorf("list_codebooks: %v", r)
	}
	r = mcpCall(t, s, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"nope"}}`)
	if r["error"] == nil {
		t.Errorf("unknown tool should be a JSON-RPC error: %v", r)
	}
}

func TestHTTPErrorCodes(t *testing.T) {
	for err, want := range map[error]int{
		badInput("x"):                         400,
		ruian.ErrUpstream:                     503,
		errors.New("something else entirely"): 500,
	} {
		rec := httptest.NewRecorder()
		httpErr(rec, err)
		if rec.Code != want {
			t.Errorf("%v → %d, want %d", err, rec.Code, want)
		}
	}
}

func TestCheckCZ(t *testing.T) {
	if checkCZ(ruian.LatLon{Lat: 50.08, Lon: 14.42}) != nil {
		t.Error("Prague rejected")
	}
	if checkCZ(ruian.LatLon{Lat: 48.2, Lon: 16.37}) == nil {
		t.Error("Vienna accepted")
	}
}
