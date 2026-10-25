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
// same shape as actors/czech-public-contracts/mcp.go but with a tool table.

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

const noOwners = " RÚIAN (Czech register of territorial identification, ČÚZK) has no owners; the result links to the manual cadastre lookup instead."

var tools = []tool{
	{map[string]any{
		"name": "resolve_parcel",
		"description": "Look up a Czech cadastral parcel by cadastral area (katastrální území, k.ú.) and parcel number. " +
			"Accepts the k.ú. as written in Czech documents, declined forms included (\"Hrutově\", \"Horních Počernicích\") or as a 6-digit code. " +
			"Number forms: \"123/4\" (pozemková), \"st. 56\" (stavební). Returns area in m², land type in Czech, WGS84 centroid, municipality/ORP/district/region, " +
			"the building on a stavební parcel (č.p., use, floors, flats), and optionally protected areas and a simplified polygon." + noOwners,
		"inputSchema": obj(map[string]any{
			"ku":            p("string", "cadastral area name or 6-digit code, e.g. \"Hrutov\", \"k. ú. Vinohrady\", \"648876\""),
			"kuCode":        p("integer", "6-digit k.ú. code, alternative to ku (use it after an ambiguous answer)"),
			"number":        p("string", "parcel number, e.g. \"123/4\" or \"st. 56\""),
			"withGeometry":  p("boolean", "include a simplified GeoJSON polygon (default false; adds 1–20 kB)"),
			"withBuildings": p("boolean", "include the building on a stavební parcel (default true)"),
			"withContext":   p("boolean", "include protected areas (CHKO, national park, Natura 2000), mining areas and soil unit (default false)"),
		}, "number"),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[parcelArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.resolveParcel(ctx, a)
	}},
	{map[string]any{
		"name": "extract_parcels",
		"description": "Find every parcel reference in Czech free text (public notice, contract, decision) and look each one up in RÚIAN. " +
			"Understands \"parc. č.\", \"p. č.\", \"st. p. č.\", lists (\"123/4, 123/5 a st. 56\"), ranges (\"120–125\") and the k.ú. before or after the numbers. " +
			"Each reference carries the raw text, the resolved k.ú., a confidence and the matching parcels. Max 50 kB text, 50 lookups." + noOwners,
		"inputSchema": obj(map[string]any{
			"text":          p("string", "Czech text, up to 50 000 bytes"),
			"kuHint":        p("string", "k.ú. to use for numbers with no k.ú. nearby (e.g. from the document header)"),
			"resolve":       p("boolean", "look the parcels up in RÚIAN (default true); false = parse only"),
			"withBuildings": p("boolean", "include buildings on stavební parcels (default false)"),
			"withContext":   p("boolean", "include protected areas per parcel (default false, slower)"),
		}, "text"),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[extractArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.extract(ctx, a, maxLookups)
	}},
	{map[string]any{
		"name": "resolve_address",
		"description": "Find a Czech address point (adresní místo) and its building. Forms: \"Veveří 95, Brno\", \"Veveří 331/95, 602 00 Brno\", " +
			"\"Hrutov 12\" (village without streets), \"Vinohradská 12, Praha 2\". Use Czech diacritics. Returns the official address text, PSČ, WGS84 point " +
			"and, for up to 3 hits, the building's technical data (use, floors, flats, built-up area, heating, connections, elevator).",
		"inputSchema": obj(map[string]any{
			"query":        p("string", "address in Czech"),
			"limit":        p("integer", "max hits, 1–10 (default 5)"),
			"withBuilding": p("boolean", "include building data (default true)"),
		}, "query"),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[addressArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.resolveAddress(ctx, a)
	}},
	{map[string]any{
		"name": "building_info",
		"description": "Technical data of a building (stavební objekt) from RÚIAN, codes translated to Czech: use (rodinný dům, bytový dům…), č.p./č.ev., floors, flats, " +
			"built-up and floor area, volume, completion date, construction, heating, water/sewer/gas, elevator, plus its addresses. " +
			"Give one of parcelId (from resolve_parcel), addressKod or buildingKod (from resolve_address). Many attributes are empty for older buildings.",
		"inputSchema": obj(map[string]any{
			"parcelId":    p("integer", "RÚIAN parcel id"),
			"addressKod":  p("integer", "RÚIAN address point code"),
			"buildingKod": p("integer", "RÚIAN building code"),
		}),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[buildingArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.buildingInfo(ctx, a)
	}},
	{map[string]any{
		"name": "parcel_context",
		"description": "What applies at a place in Czechia: k.ú., municipality, POÚ, ORP, district, region, protected areas (national park and zones, CHKO and zone I–IV, " +
			"nature reserves and monuments, Natura 2000 sites and bird areas, protective zones), mining areas with mineral, and the soil quality unit (BPEJ). " +
			"Give lat/lon (WGS84) or a parcelId. Useful to tell whether a building plan needs a nature-protection opinion.",
		"inputSchema": obj(map[string]any{
			"lat":      p("number", "WGS84 latitude"),
			"lon":      p("number", "WGS84 longitude"),
			"parcelId": p("integer", "RÚIAN parcel id (uses its centroid)"),
		}),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[contextArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.parcelContext(ctx, a)
	}},
	{map[string]any{
		"name":        "nearby",
		"description": "Addresses, buildings or parcels within a radius (max 2 km) of a WGS84 point, nearest first, with distance in metres.",
		"inputSchema": obj(map[string]any{
			"lat":     p("number", "WGS84 latitude"),
			"lon":     p("number", "WGS84 longitude"),
			"radiusM": p("number", "radius in metres, 1–2000 (default 100)"),
			"what":    map[string]any{"type": "string", "enum": []string{"addresses", "buildings", "parcels"}, "description": "default addresses"},
			"limit":   p("integer", "max hits, 1–100 (default 20)"),
		}, "lat", "lon"),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[nearbyArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.nearby(ctx, a)
	}},
	{map[string]any{
		"name": "export_parcels",
		"description": "Export all parcels of one k.ú. as a GeoJSON file (WGS84 polygons, number, area, land type, use) into the Actor's key-value store " +
			"and return its URL and counts, not the data itself. For GIS work, EUDR plot geolocation or bulk filtering (e.g. arable land over 5 000 m²).",
		"inputSchema": obj(map[string]any{
			"ku":     p("string", "k.ú. name or code"),
			"kuCode": p("integer", "6-digit k.ú. code"),
		}),
		"annotations": map[string]any{"readOnlyHint": false, "openWorldHint": true, "idempotentHint": true},
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		a, err := decode[exportArgs](raw)
		if err != nil {
			return nil, err
		}
		return s.export(ctx, a)
	}},
	{map[string]any{
		"name":        "list_codebooks",
		"description": "Official ČÚZK codebooks used in the answers (land type, land use, building use, construction, heating, connections, special-purpose areas), code → Czech name.",
		"inputSchema": obj(map[string]any{}),
		"annotations": readOnly,
	}, func(ctx context.Context, s *service, raw json.RawMessage) (any, error) {
		return map[string]any{"fetchedAt": ruian.CodebooksDate, "codebooks": ruian.ListCodebooks()}, nil
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
			"serverInfo":      map[string]any{"name": "ruian-parcel-mcp", "version": serverVersion},
			"instructions": "Czech parcels, addresses, buildings and protected areas from RÚIAN (ČÚZK). Answers are in Czech terms. " +
				"No owner data: for owners, point the user to Nahlížení do katastru nemovitostí (manual lookup).",
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
			if errors.Is(err, ruian.ErrUpstream) {
				msg = "Služba ČÚZK RÚIAN je dočasně nedostupná, zkuste to za chvíli. (" + msg + ")"
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
