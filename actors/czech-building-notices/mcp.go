package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/lioilsources/apifysandbox/ruian"
)

// Minimal stateless MCP server over Streamable HTTP with JSON responses,
// same shape as actors/ruian-parcel-mcp/mcp.go.

const (
	mcpProtocol   = "2025-06-18"
	serverVersion = "0.1.0"
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type tool struct {
	def  map[string]any
	call func(ctx context.Context, s *service, args json.RawMessage) (any, error)
}

func obj(props map[string]any, required ...string) map[string]any {
	m := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		m["required"] = required
	}
	return m
}

func p(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

var readOnly = map[string]any{"readOnlyHint": true, "openWorldHint": true}

// decode is a helper for tool handlers.
func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 || string(raw) == "null" {
		return v, nil
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return v, badInput("arguments: %v", err)
	}
	return v, nil
}

func filterProps(extra map[string]any) map[string]any {
	m := map[string]any{
		"kraj":            p("string", "region, e.g. \"Jihomoravský\", \"Vysočina\", \"Praha\""),
		"orp":             p("string", "municipality with extended powers (ORP), e.g. \"Jihlava\""),
		"obec":            p("string", "municipality name or code"),
		"ku":              map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "cadastral areas (k.ú.) by name or 6-digit code"},
		"bbox":            map[string]any{"type": "array", "items": map[string]any{"type": "number"}, "minItems": 4, "maxItems": 4, "description": "[minLon, minLat, maxLon, maxLat] WGS84; only notices with resolvable parcels match"},
		"types":           map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": typeNames()}, "description": "notice types (default all)"},
		"keywords":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "any of these in title/description, diacritics ignored (\"fotovoltaick\", \"bytový dům\")"},
		"since":           p("string", "posted on/after: YYYY-MM-DD or relative \"7d\", \"2w\""),
		"until":           p("string", "posted on/before: YYYY-MM-DD"),
		"includeExpired":  p("boolean", "include notices past their 'relevant until' date (metadata only)"),
		"inProtectedArea": p("boolean", "only notices with a parcel in a protected area (CHKO, national park, Natura 2000…); slower"),
	}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func typeNames() []string {
	var out []string
	for _, t := range typesList() {
		out = append(out, t["type"])
	}
	return out
}

const scope = " Source: official notice boards (úřední desky) of Czech municipalities, regions and state bodies published as open data (OFN) in the national catalogue; about two thirds of ORP offices publish one. Natural persons' names are redacted."

var tools = []tool{
	{map[string]any{
		"name": "search_building_notices",
		"description": "Search Czech public notices about building and land: start of permit proceedings, building permits and decisions, occupancy approvals, demolitions, binding opinions, zoning plans, EIA, and municipal land sales/leases. " +
			"Filter by region (kraj/ORP/obec/k.ú./bbox), type, keywords and date. Each notice has type, redacted title, case number, office, territory, k.ú. and parcel numbers found in the text, documents (PDF links) and the source URL." + scope,
		"inputSchema": obj(filterProps(map[string]any{
			"limit":       p("integer", "max notices, 1–50 (default 20)"),
			"withContext": p("boolean", "resolve parcels in RÚIAN and add protected/mining areas (slower)"),
		})),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[searchArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.search(ctx, a)
	}},
	{map[string]any{
		"name": "watch_area",
		"description": "New building/land notices for an area since the last call. First call (no cursor) returns the last 7 days and a cursor; pass that cursor next time to get only notices first seen after it. " +
			"Same filters as search_building_notices. The index refreshes every few hours." + scope,
		"inputSchema": obj(filterProps(map[string]any{
			"cursor": p("string", "cursor from the previous call"),
			"limit":  p("integer", "max notices, 1–50 (default 50)"),
		})),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[watchArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.watch(ctx, a)
	}},
	{map[string]any{
		"name":        "get_notice",
		"description": "One notice by id with its parcels resolved in RÚIAN (area, land type, WGS84 centroid, territory) and optionally the protected and mining areas they lie in.",
		"inputSchema": obj(map[string]any{
			"id":          p("string", "notice id from search_building_notices or watch_area"),
			"withContext": p("boolean", "add protected and mining areas (default false)"),
		}, "id"),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[noticeArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.notice(ctx, a)
	}},
	{map[string]any{
		"name": "notice_parcels",
		"description": "Full RÚIAN detail of every parcel named in a notice: area, land type, buildings standing on it (use, floors, flats), territory and optionally protected areas. " +
			"Owners are not available (RÚIAN has none); each parcel carries a hint for a manual cadastre lookup.",
		"inputSchema": obj(map[string]any{
			"id":          p("string", "notice id"),
			"withContext": p("boolean", "add protected and mining areas (default false)"),
		}, "id"),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[noticeArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.noticeParcels(ctx, a)
	}},
	{map[string]any{
		"name":        "list_offices",
		"description": "Notice boards covered, with the publishing office, its municipality/ORP/region, item counts and last fetch status. Use it to tell whether an area is covered at all.",
		"inputSchema": obj(map[string]any{
			"kraj": p("string", "region filter"),
			"orp":  p("string", "ORP filter"),
		}),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[officesArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.offices(ctx, a)
	}},
	{map[string]any{
		"name":        "list_notice_types",
		"description": "The notice types used by the classifier, with Czech labels.",
		"inputSchema": obj(map[string]any{}),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		return typesList(), nil
	}},
}

func toolDefs() []any {
	out := make([]any, len(tools))
	for i, t := range tools {
		out[i] = t.def
	}
	return out
}

func findTool(name string) *tool {
	for i := range tools {
		if tools[i].def["name"] == name {
			return &tools[i]
		}
	}
	return nil
}

func (s *service) mcpHandler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	var req rpcReq
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, 200, rpcResp{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcErr{-32700, "parse error"}})
		return
	}
	if len(req.ID) == 0 { // notification (e.g. notifications/initialized)
		w.WriteHeader(http.StatusAccepted)
		return
	}
	resp := rpcResp{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": mcpProtocol,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "czech-building-notices", "version": serverVersion},
			"instructions": "Czech building and land notices from official notice boards, enriched with RÚIAN parcels. Coverage is partial (boards published as open data); " +
				"say so when an area returns nothing and check list_offices. Personal names are redacted; never try to identify private persons or owners.",
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolDefs()}
	case "tools/call":
		var pr struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &pr); err != nil {
			resp.Error = &rpcErr{-32602, "bad params"}
			break
		}
		t := findTool(pr.Name)
		if t == nil {
			resp.Error = &rpcErr{-32602, "unknown tool " + pr.Name}
			break
		}
		out, err := t.call(r.Context(), s, pr.Arguments)
		if err != nil {
			msg := err.Error()
			switch {
			case errors.Is(err, ruian.ErrUpstream):
				msg = "Služba ČÚZK RÚIAN je dočasně nedostupná, zkuste to za chvíli. (" + msg + ")"
			case errors.Is(err, errWarming):
				msg = "Index úředních desek se právě sestavuje (první sklizeň trvá asi minutu). Zkuste to znovu."
			}
			resp.Result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": msg}}}
			break
		}
		text, _ := json.Marshal(out)
		resp.Result = map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": string(text)}},
			"structuredContent": map[string]any{"result": out},
		}
	default:
		resp.Error = &rpcErr{-32601, "method not found"}
	}
	writeJSON(w, 200, resp)
}
