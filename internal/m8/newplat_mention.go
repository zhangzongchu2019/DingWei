package m8

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
)

// HandleViewMentionCandidates GET /view/{full_name}/mention-candidates（V11 Hub 侧，组长 9f0cd6ab）。
// 认人同 view 页（RequireConsoleAgent：新平台会话＋归属用户）。候选＝该 Agent 所在全部编组成员的并集，去掉自己，只列 Agent；
// insert 由 identity 定（并集内短名唯一 ⇒ 短名，否则全名）；在线优先，再按短名（不分大小写），再按 full_name。
// cli 取在线会话握手时报的 tool（小写）；离线时为空，页面画灰色问号。
func (h *Hub) HandleViewMentionCandidates(w http.ResponseWriter, r *http.Request) {
	if h.Identity == nil {
		http.NotFound(w, r)
		return
	}
	self, _, err := h.Identity.ResolveFullName(r.Context(), viewRoot(strings.TrimSpace(r.PathValue("sessionName"))))
	if err != nil {
		writeAgentWSError(w, http.StatusNotFound, "not_found")
		return
	}
	cands, err := h.Identity.MentionCandidates(r.Context(), self.ID)
	if err != nil {
		writeAgentWSError(w, http.StatusServiceUnavailable, "candidates_unavailable")
		return
	}
	type candidate struct {
		FullName string `json:"full_name"`
		Name     string `json:"name"`
		CLI      string `json:"cli"`
		Online   bool   `json:"online"`
		Insert   string `json:"insert"`
	}
	out := make([]candidate, 0, len(cands))
	for _, c := range cands {
		v := candidate{FullName: c.FullName, Name: c.Agent.Name, Insert: c.Insert}
		h.mu.Lock()
		if sc := h.sessionClients[AgentKeyID(c.Agent.OwnerLC)][c.Agent.NameLC()]; sc != nil && sc.conn != nil {
			v.Online, v.CLI = true, strings.ToLower(strings.TrimSpace(sc.tool))
		}
		h.mu.Unlock()
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Online != out[j].Online {
			return out[i].Online
		}
		if a, b := strings.ToLower(out[i].Name), strings.ToLower(out[j].Name); a != b {
			return a < b
		}
		return strings.ToLower(out[i].FullName) < strings.ToLower(out[j].FullName)
	})
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"candidates": out})
}
