package main

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/lioilsources/czech-public-contracts/internal/search"
)

// Minimal MCP server over Streamable HTTP: stateless, JSON responses only,
// one tool. Enough for Claude Code / Director to call it as a remote MCP.

const mcpProtocol = "2025-06-18"

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

var searchTool = map[string]any{
	"name":        "search_contracts",
	"description": "Search the Czech contract registry (Registr smluv) by IČO, party name, subject keywords and publication date (max 31 days, max 50 results). Returns contract facts with links to smlouvy.gov.cz.",
	"inputSchema": map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ico":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "IČO of the authority or a counterparty"},
			"name":        map[string]any{"type": "string", "description": "substring of a party name"},
			"keywords":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "any of these in the subject (case-insensitive)"},
			"dateFrom":    map[string]any{"type": "string", "format": "date"},
			"dateTo":      map[string]any{"type": "string", "format": "date"},
			"minValueCzk": map[string]any{"type": "number"},
			"limit":       map[string]any{"type": "integer", "maximum": 50},
		},
	},
	"annotations": map[string]any{"readOnlyHint": true, "openWorldHint": true},
}

func mcpHandler(w http.ResponseWriter, r *http.Request) {
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
			"serverInfo":      map[string]any{"name": "czech-public-contracts", "version": "0.1.0"},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": []any{searchTool}}
	case "tools/call":
		var p struct {
			Name      string       `json:"name"`
			Arguments search.Input `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name != "search_contracts" {
			resp.Error = &rpcErr{-32602, "unknown tool or bad arguments"}
			break
		}
		items, err := standbySearch(r.Context(), p.Arguments)
		if err != nil {
			resp.Result = map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": err.Error()}}}
			break
		}
		text, _ := json.Marshal(items)
		resp.Result = map[string]any{
			"content":           []any{map[string]any{"type": "text", "text": string(text)}},
			"structuredContent": map[string]any{"contracts": items},
		}
	default:
		resp.Error = &rpcErr{-32601, "method not found"}
	}
	writeJSON(w, 200, resp)
}
