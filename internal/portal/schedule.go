// Package portal renders read-only public schedule pages.
package portal

import (
	"context"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type ScheduleServer struct {
	Repo store.Repository
	TTL  time.Duration

	mu    sync.Mutex
	cache map[string]cacheEntry
}

type cacheEntry struct {
	sig       string
	html      string
	expiresAt time.Time
}

func NewScheduleServer(repo store.Repository) *ScheduleServer {
	return &ScheduleServer{Repo: repo, TTL: 10 * time.Minute, cache: map[string]cacheEntry{}}
}

func (s *ScheduleServer) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /schedule/", s.handle)
}

func (s *ScheduleServer) handle(w http.ResponseWriter, r *http.Request) {
	projectID := strings.Trim(strings.TrimPrefix(r.URL.Path, "/schedule/"), "/")
	if projectID == "" {
		projectID = "proj:default"
	}
	page, err := s.Render(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(page))
}

func (s *ScheduleServer) Render(ctx context.Context, projectID string) (string, error) {
	sig, err := s.signature(ctx, projectID)
	if err != nil {
		return "", err
	}
	now := time.Now()
	s.mu.Lock()
	if ent, ok := s.cache[projectID]; ok && ent.sig == sig && now.Before(ent.expiresAt) {
		s.mu.Unlock()
		return ent.html, nil
	}
	s.mu.Unlock()
	page, err := s.renderFresh(ctx, projectID)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	s.cache[projectID] = cacheEntry{sig: sig, html: page, expiresAt: now.Add(s.ttl())}
	s.mu.Unlock()
	return page, nil
}

func (s *ScheduleServer) signature(ctx context.Context, projectID string) (string, error) {
	var parts []string
	team, err := s.Repo.LatestScheduleDoc(ctx, projectID, "team", "")
	if err != nil {
		return "", err
	}
	if team != nil {
		parts = append(parts, "team:", team.ID, ":", itoa(team.Version))
	}
	members, err := s.Repo.ListProjectMembers(ctx, projectID)
	if err != nil {
		return "", err
	}
	for _, m := range members {
		doc, err := s.Repo.LatestScheduleDoc(ctx, projectID, "personal", m.OwnerKey)
		if err != nil {
			return "", err
		}
		if doc != nil {
			parts = append(parts, m.OwnerKey, ":", doc.ID, ":", itoa(doc.Version))
		}
	}
	return strings.Join(parts, "|"), nil
}

func (s *ScheduleServer) renderFresh(ctx context.Context, projectID string) (string, error) {
	project, err := s.Repo.GetProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if project == nil {
		project = &model.Project{ID: projectID, Name: projectID}
	}
	team, err := s.Repo.LatestScheduleDoc(ctx, projectID, "team", "")
	if err != nil {
		return "", err
	}
	members, err := s.Repo.ListProjectMembers(ctx, projectID)
	if err != nil {
		return "", err
	}
	var body strings.Builder
	body.WriteString("<section><h2>团队排期</h2>")
	if team != nil {
		body.WriteString(markdownBlock(team.Content))
	} else {
		body.WriteString("<p>暂无团队排期。</p>")
	}
	body.WriteString("</section><section><h2>个人排期</h2>")
	for _, m := range members {
		doc, err := s.Repo.LatestScheduleDoc(ctx, projectID, "personal", m.OwnerKey)
		if err != nil {
			return "", err
		}
		if doc == nil {
			continue
		}
		body.WriteString("<article><h3>" + html.EscapeString(firstNonEmpty(m.DisplayName, m.OwnerKey)) + "</h3>")
		body.WriteString(markdownBlock(doc.Content))
		body.WriteString("</article>")
	}
	body.WriteString("</section>")
	return pageShell(project.Name, body.String()), nil
}

func (s *ScheduleServer) ttl() time.Duration {
	if s.TTL <= 0 {
		return 10 * time.Minute
	}
	return s.TTL
}

func pageShell(title, body string) string {
	return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>` + html.EscapeString(title) + ` · WorkPulse</title><style>
body{margin:0;background:#f7f8fb;color:#20242c;font:15px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif}
main{max-width:1120px;margin:0 auto;padding:28px 20px 56px}
h1{font-size:28px;margin:0 0 20px}h2{font-size:20px;margin:28px 0 12px}h3{font-size:16px;margin:18px 0 8px}
section,article{background:#fff;border:1px solid #e3e6ee;border-radius:8px;padding:18px;margin:14px 0}
pre{white-space:pre-wrap;word-break:break-word;background:#fbfcff;border:1px solid #e8ebf2;border-radius:6px;padding:14px;overflow:auto}
code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}
</style><script type="module">import mermaid from "https://cdn.jsdelivr.net/npm/mermaid@10/dist/mermaid.esm.min.mjs";mermaid.initialize({startOnLoad:true});</script></head><body><main><h1>` + html.EscapeString(title) + `</h1>` + body + `</main></body></html>`
}

func markdownBlock(md string) string {
	escaped := html.EscapeString(strings.TrimSpace(md))
	escaped = strings.ReplaceAll(escaped, "```mermaid", `</pre><div class="mermaid">`)
	escaped = strings.ReplaceAll(escaped, "```", `</div><pre>`)
	return "<pre>" + escaped + "</pre>"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	n := v
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
