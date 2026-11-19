package mcpgw

import (
	"bytes"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"io"
	"net/http"
	"strings"
)

// CompatProtocolVersion is the fully supported Streamable HTTP dialect.
// Other initialize proposals negotiate this version; they are not advertised
// as fully supported codecs (notably 03-26 batch and 11-25 tasks).
const CompatProtocolVersion = "2025-06-18"

// route2025 selects a codec only after mcp has authenticated this HTTP request.
// Selection never grants capabilities or caches authentication across requests.
func (g *Gateway) route2025(w http.ResponseWriter, r *http.Request, seat extseat.Seat) bool {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, extshared.MessageJSONMaxBytes))
	if err != nil {
		rpcError(w, 400, nil, wireError{Code: -32700, Message: "invalid JSON request"})
		return true
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	var hint struct {
		Method string `json:"method"`
		Params struct {
			Version string                     `json:"protocolVersion"`
			Meta    map[string]json.RawMessage `json:"_meta"`
		} `json:"params"`
	}
	_ = json.Unmarshal(body, &hint)
	header := singleHeader(r, "MCP-Protocol-Version")
	// Preserve both established June selection forms. Otherwise explicit
	// private hints must retain the private envelope's exact guards, even when
	// a standard initialize proposal appears in the same request.
	compatSelected := header == CompatProtocolVersion || (len(r.Header.Values("MCP-Protocol-Version")) == 0 && hint.Method == "initialize" && hint.Params.Version == CompatProtocolVersion)
	privateMeta := false
	for key := range hint.Params.Meta {
		if strings.HasPrefix(key, metaPrefix) {
			privateMeta = true
			break
		}
	}
	privateHint := header == ProtocolVersion || len(r.Header.Values("Mcp-Method")) != 0 || len(r.Header.Values("Mcp-Name")) != 0 || privateMeta
	if !compatSelected && privateHint {
		return false
	}
	g.mcp2025(w, r, seat)
	return true
}

type request2025 struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type params2025 struct {
	Meta map[string]json.RawMessage `json:"_meta,omitempty"`
}
type initialize2025 struct {
	Meta         map[string]json.RawMessage `json:"_meta,omitempty"`
	Version      string                     `json:"protocolVersion"`
	Capabilities map[string]json.RawMessage `json:"capabilities"`
	ClientInfo   struct {
		Name        string            `json:"name"`
		Title       string            `json:"title,omitempty"`
		Version     string            `json:"version"`
		Description string            `json:"description,omitempty"`
		WebsiteURL  string            `json:"websiteUrl,omitempty"`
		Icons       []json.RawMessage `json:"icons,omitempty"`
	} `json:"clientInfo"`
}
type list2025 struct {
	Meta   map[string]json.RawMessage `json:"_meta,omitempty"`
	Cursor *string                    `json:"cursor,omitempty"`
}
type call2025 struct {
	Meta      map[string]json.RawMessage `json:"_meta,omitempty"`
	Name      string                     `json:"name"`
	Arguments json.RawMessage            `json:"arguments,omitempty"`
}

func decodeParams2025(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return errParams
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return e
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return errParams
	}
	return nil
}
func (g *Gateway) mcp2025(w http.ResponseWriter, r *http.Request, seat extseat.Seat) {
	var q request2025
	if decode(w, r, &q) != nil {
		rpcError(w, 400, nil, wireError{Code: -32700, Message: "invalid JSON request"})
		return
	}
	notification := q.Method == "notifications/initialized"
	if q.JSONRPC != "2.0" || q.Method == "" || (notification && len(q.ID) != 0) || (!notification && !validID(q.ID)) {
		rpcError(w, 400, q.ID, wireError{Code: -32600, Message: "invalid request"})
		return
	}
	version := "2025-03-26" // Required stateless HTTP fallback when no header exists.
	if len(r.Header.Values("MCP-Protocol-Version")) != 0 {
		version = singleHeader(r, "MCP-Protocol-Version")
	}
	if len(r.Header.Values("MCP-Protocol-Version")) > 1 ||
		(q.Method != "initialize" && version != CompatProtocolVersion) ||
		(len(r.Header.Values("MCP-Protocol-Version")) != 0 && version == "") {
		rpcError(w, 400, q.ID, wireError{Code: -32022, Message: "Unsupported protocol version"})
		return
	}
	// The caller authenticated once for this request; never across requests.
	var e error
	var result any
	switch q.Method {
	case "initialize":
		var p initialize2025
		if decodeParams2025(q.Params, &p) != nil || p.Version == "" || p.Capabilities == nil || p.ClientInfo.Name == "" || p.ClientInfo.Version == "" {
			e = errParams
			break
		}
		for _, v := range p.Capabilities {
			var obj map[string]json.RawMessage
			if json.Unmarshal(v, &obj) != nil || obj == nil {
				e = errParams
				break
			}
		}
		if e == nil {
			result = map[string]any{"protocolVersion": CompatProtocolVersion, "capabilities": map[string]any{"tools": map[string]bool{"listChanged": false}}, "serverInfo": map[string]string{"name": serverInfoName, "version": serverInfoVersion}}
		}
	case "notifications/initialized":
		var p params2025
		if decodeParams2025(q.Params, &p) != nil {
			e = errParams
			break
		}
		w.WriteHeader(http.StatusAccepted)
		return
	case "ping":
		var p params2025
		if decodeParams2025(q.Params, &p) != nil {
			e = errParams
			break
		}
		result = map[string]any{}
	case "tools/list":
		var p list2025
		if decodeParams2025(q.Params, &p) != nil || (p.Cursor != nil && *p.Cursor != "") {
			e = errParams
			break
		}
		result = map[string]any{"tools": toolDefinitions(g.cfg.SendReplyMax)}
	case "tools/call":
		var p call2025
		if decodeParams2025(q.Params, &p) != nil || p.Name == "" {
			e = errParams
			break
		}
		var data any
		data, e = g.call(r.Context(), seat, p.Name, p.Arguments)
		if e == nil {
			b, _ := json.Marshal(data)
			result = map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}, "structuredContent": data, "isError": false}
		}
	default:
		e = errMethod
	}
	if e != nil {
		st, we := mapped(e)
		g.challenge(w, st)
		rpcError(w, st, q.ID, we)
		return
	}
	writeJSON(w, 200, map[string]any{"jsonrpc": "2.0", "id": q.ID, "result": result})
}
