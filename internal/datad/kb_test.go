package datad

// D4a-2 用例:kb(§6 fsgrep)+P8 token 轮换。

import (
	"encoding/json"
	"strings"
	"testing"
)

func testServerKB(t *testing.T) *Server {
	t.Helper()
	ms := NewMemStore()
	return New(Config{Store: ms, KB: &FSGrepProvider{Dir: t.TempDir()},
		Tokens: map[string]Identity{
			"tok-coord": {Session: "zzc-coordinator-2642", Role: "coordinator"},
			"tok-devo":  {Session: "zzc-devo48-2642", Role: "member"},
		}})
}

func TestKBIngestQueryRBAC(t *testing.T) {
	s := testServerKB(t)
	doc := `{"source":"rules-22.md","content":"规约22:派工归口coordinator,发版归口终校。整版评审三轮终止。"}`
	// member ingest=403(策展=管理域)
	if w := call(t, s, "POST", "/v1/kb/ingest", "tok-devo", doc, nil); w.Code != 403 {
		t.Fatalf("member ingest want 403 got %d", w.Code)
	}
	if w := call(t, s, "POST", "/v1/kb/ingest", "tok-coord", doc, nil); w.Code != 201 {
		t.Fatalf("coord ingest got %d: %s", w.Code, w.Body.String())
	}
	// 路径穿越拒
	if w := call(t, s, "POST", "/v1/kb/ingest", "tok-coord",
		`{"source":"../etc/passwd","content":"x"}`, nil); w.Code != 422 {
		t.Fatalf("穿越 source want 422 got %d", w.Code)
	}
	// query=全员
	w := call(t, s, "POST", "/v1/kb/query", "tok-devo", `{"q":"派工归口"}`, nil)
	if w.Code != 200 {
		t.Fatalf("query got %d", w.Code)
	}
	var resp struct{ Hits []KBHit }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Hits) != 1 || resp.Hits[0].Source != "rules-22.md" ||
		!strings.Contains(resp.Hits[0].Chunk, "派工归口") {
		t.Fatalf("hits=%+v", resp.Hits)
	}
	// 无命中=空
	w = call(t, s, "POST", "/v1/kb/query", "tok-devo", `{"q":"不存在词xyz"}`, nil)
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Hits) != 0 {
		t.Fatalf("空查询 hits=%+v", resp.Hits)
	}
}

func TestKBTopKOrdering(t *testing.T) {
	s := testServerKB(t)
	call(t, s, "POST", "/v1/kb/ingest", "tok-coord",
		`{"source":"a.md","content":"锚 锚 锚 锚"}`, nil) // 高频短文=高分
	call(t, s, "POST", "/v1/kb/ingest", "tok-coord",
		`{"source":"b.md","content":"锚"}`, nil)
	w := call(t, s, "POST", "/v1/kb/query", "tok-devo", `{"q":"锚","top_k":1}`, nil)
	var resp struct{ Hits []KBHit }
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Hits) != 1 || resp.Hits[0].Source != "a.md" {
		t.Fatalf("topK 排序:%+v", resp.Hits)
	}
}

func TestP8TokenRotation(t *testing.T) {
	s, _ := testServer()
	// 双活窗:新旧同表→旧新都通
	s.ReloadTokens(map[string]Identity{
		"tok-devo":     {Session: "zzc-devo48-2642", Role: "member"},
		"tok-devo-new": {Session: "zzc-devo48-2642", Role: "member"},
	})
	if w := call(t, s, "GET", "/v1/tasks", "tok-devo", "", nil); w.Code != 200 {
		t.Fatalf("双活窗旧token got %d", w.Code)
	}
	if w := call(t, s, "GET", "/v1/tasks", "tok-devo-new", "", nil); w.Code != 200 {
		t.Fatalf("双活窗新token got %d", w.Code)
	}
	// 窗末:仅新
	s.ReloadTokens(map[string]Identity{
		"tok-devo-new": {Session: "zzc-devo48-2642", Role: "member"},
	})
	if w := call(t, s, "GET", "/v1/tasks", "tok-devo", "", nil); w.Code != 401 {
		t.Fatalf("轮换后旧token want 401 got %d", w.Code)
	}
	if w := call(t, s, "GET", "/v1/tasks", "tok-devo-new", "", nil); w.Code != 200 {
		t.Fatalf("轮换后新token got %d", w.Code)
	}
}
