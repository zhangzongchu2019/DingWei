package datad

// 知识库可插拔面(设计冻结版 §6):KBProvider 接口冻结;首 provider=fsgrep
// (文件目录+全文子串检索,零新依赖);向量库 provider 接口不变后批插入。
// 写权:ingest=coordinator(知识策展归管理域,合 §4 单写默认);query=全员。

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type KBHit struct {
	Source string  `json:"source"`
	Chunk  string  `json:"chunk"`
	Score  float64 `json:"score"`
}

type KBDoc struct {
	Source  string          `json:"source"`
	Content string          `json:"content"`
	Meta    json.RawMessage `json:"meta,omitempty"`
}

type KBProvider interface {
	Query(ctx context.Context, q string, topK int) ([]KBHit, error)
	Ingest(ctx context.Context, doc KBDoc) error
}

// ---- fsgrep provider ----

type FSGrepProvider struct{ Dir string }

var kbSourceRe = regexp.MustCompile(`^[a-zA-Z0-9._\-]{1,128}$`) // 防路径穿越

func (f *FSGrepProvider) Ingest(_ context.Context, doc KBDoc) error {
	if !kbSourceRe.MatchString(doc.Source) {
		return fmt.Errorf("source 名非法(仅[a-zA-Z0-9._-],≤128)")
	}
	if err := os.MkdirAll(f.Dir, 0o700); err != nil {
		return err
	}
	// 原子写(temp+rename,同 §E 持久纪律)
	tmp, err := os.CreateTemp(f.Dir, ".kb-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(doc.Content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(f.Dir, doc.Source))
}

// Query 子串命中计分:命中次数/文档长度归一;chunk=首命中前后 200 字节窗。
func (f *FSGrepProvider) Query(_ context.Context, q string, topK int) ([]KBHit, error) {
	if topK <= 0 {
		topK = 5
	}
	entries, err := os.ReadDir(f.Dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ql := strings.ToLower(q)
	var hits []KBHit
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(f.Dir, e.Name()))
		if err != nil {
			continue
		}
		text := string(raw)
		lower := strings.ToLower(text)
		n := strings.Count(lower, ql)
		if n == 0 {
			continue
		}
		idx := strings.Index(lower, ql)
		start := idx - 200
		if start < 0 {
			start = 0
		}
		end := idx + len(q) + 200
		if end > len(text) {
			end = len(text)
		}
		hits = append(hits, KBHit{
			Source: e.Name(), Chunk: text[start:end],
			Score: float64(n) / float64(1+len(text)/1000),
		})
	}
	sort.Slice(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > topK {
		hits = hits[:topK]
	}
	return hits, nil
}

// ---- 端点 ----

func (s *Server) handleKBQuery(w http.ResponseWriter, r *http.Request, id Identity) {
	if s.kb == nil {
		http.Error(w, "kb provider not configured", http.StatusNotImplemented)
		return
	}
	var req struct {
		Q    string `json:"q"`
		TopK int    `json:"top_k"`
	}
	if err := decodeBody(r, &req); err != nil || req.Q == "" {
		http.Error(w, "q 必填", http.StatusBadRequest)
		return
	}
	hits, err := s.kb.Query(r.Context(), req.Q, req.TopK)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, 200, map[string]any{"hits": hits})
}

func (s *Server) handleKBIngest(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" { // 知识策展=管理域单写
		s.deny(w, r, id, "kb.ingest")
		return
	}
	if s.kb == nil {
		http.Error(w, "kb provider not configured", http.StatusNotImplemented)
		return
	}
	var doc KBDoc
	if err := decodeBody(r, &doc); err != nil || doc.Source == "" || doc.Content == "" {
		http.Error(w, "source/content 必填", http.StatusUnprocessableEntity)
		return
	}
	if err := s.kb.Ingest(r.Context(), doc); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	s.audit(r.Context(), id, "kb.ingest", doc.Source, nil)
	w.WriteHeader(http.StatusCreated)
}
