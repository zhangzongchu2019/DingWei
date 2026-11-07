package m8

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"

	"github.com/zhangzongchu2019/dingwei/internal/changeflow"
	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

//go:embed console_change_evidence.html
var changeEvidencePage string

func changeError(w http.ResponseWriter, e error) {
	code, message := http.StatusServiceUnavailable, "提供方或证据依赖未就绪，不能验证当前权限与事实"
	var blocked *changeflow.Blocked
	switch {
	case errors.Is(e, changeflow.ErrForbidden):
		code, message = 403, "当前主体没有仓库操作权限"
	case errors.Is(e, changeflow.ErrInvalid):
		code, message = 400, "坐标、操作或证据无效"
	case errors.Is(e, changeflow.ErrConflict):
		code, message = 409, "版本、修订或操作已变化，请重新读取"
	case errors.As(e, &blocked):
		code, message = 409, blocked.Reason
	}
	http.Error(w, message, code)
}
func changeCoordinate(q url.Values) (changeflow.Coordinate, error) {
	for k, v := range q {
		if len(v) != 1 {
			return changeflow.Coordinate{}, changeflow.ErrInvalid
		}
		switch k {
		case "provider", "git_instance", "repository", "change_dir", "commit_sha", "change_id":
		default:
			return changeflow.Coordinate{}, changeflow.ErrInvalid
		}
	}
	c := changeflow.Coordinate{Provider: q.Get("provider"), ChangeID: q.Get("change_id"), Coordinate: specconfirm.Coordinate{Instance: q.Get("git_instance"), Repository: q.Get("repository"), ChangeDir: q.Get("change_dir"), Commit: q.Get("commit_sha")}}
	return c, c.Validate()
}
func (h *Hub) HandleChangeEvidencePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if platformPrincipal(r) == "" {
		changeError(w, changeflow.ErrForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, changeEvidencePage)
}
func (h *Hub) HandleChangeEvidence(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	actor := platformPrincipal(r)
	if actor == "" {
		changeError(w, changeflow.ErrForbidden)
		return
	}
	if h.ChangeEvidence == nil {
		changeError(w, changeflow.ErrUnavailable)
		return
	}
	var c changeflow.Coordinate
	var e error
	if r.Method == http.MethodGet {
		var q url.Values
		q, e = url.ParseQuery(r.URL.RawQuery)
		if e == nil {
			c, e = changeCoordinate(q)
		}
	} else if r.Method == http.MethodPost {
		media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || media != "application/json" || r.URL.RawQuery != "" || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			changeError(w, changeflow.ErrInvalid)
			return
		}
		if raw := r.Header.Get("Origin"); raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || (u.Scheme != "http" && u.Scheme != "https") || !trustedorigin.AuthorityMatches(u.Host, r.Host, len(h.TrustedOrigins) > 0) || !h.originAllowed(raw, h.PublicOrigin == "" || raw == h.PublicOrigin, len(r.Header.Values("Origin"))) {
				changeError(w, changeflow.ErrForbidden)
				return
			}
		} else if len(h.TrustedOrigins) > 0 {
			changeError(w, changeflow.ErrForbidden)
			return
		}
		var q struct {
			Action       string                `json:"action"`
			Coordinate   changeflow.Coordinate `json:"coordinate"`
			OperationID  string                `json:"operation_id"`
			Revision     string                `json:"revision"`
			Expected     changeflow.Phase      `json:"expected"`
			To           changeflow.Phase      `json:"to"`
			Environment  string                `json:"environment"`
			Scenario     string                `json:"scenario"`
			Status       string                `json:"status"`
			TestedCommit string                `json:"tested_commit"`
			Evidence     string                `json:"evidence"`
			Reason       string                `json:"reason"`
			Supersedes   string                `json:"supersedes"`
			Name         string                `json:"name"`
			Success      bool                  `json:"success"`
		}
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
		d.DisallowUnknownFields()
		if d.Decode(&q) != nil || d.Decode(&struct{}{}) != io.EOF {
			changeError(w, changeflow.ErrInvalid)
			return
		}
		c = q.Coordinate
		switch q.Action {
		case "detect":
			e = h.ChangeEvidence.Detect(r.Context(), actor, c)
		case "ack":
			e = h.ChangeEvidence.Acknowledge(r.Context(), actor, c, q.Revision)
		case "deliver":
			e = h.ChangeEvidence.Deliver(r.Context(), actor, c)
		case "advance":
			e = h.ChangeEvidence.Advance(r.Context(), actor, changeflow.AdvanceRequest{Coordinate: c, OperationID: q.OperationID, Expected: q.Expected, To: q.To, Environment: q.Environment})
		case "result":
			e = h.ChangeEvidence.RecordResult(r.Context(), actor, changeflow.ResultRequest{Coordinate: c, OperationID: q.OperationID, Scenario: q.Scenario, Status: q.Status, TestedCommit: q.TestedCommit, Evidence: q.Evidence, Reason: q.Reason, Supersedes: q.Supersedes})
		case "tool":
			e = h.ChangeEvidence.RecordTool(r.Context(), actor, changeflow.ToolRequest{Coordinate: c, OperationID: q.OperationID, Name: q.Name, Evidence: q.Evidence, Success: q.Success})
		default:
			e = changeflow.ErrInvalid
		}
	} else {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if e != nil {
		changeError(w, e)
		return
	}
	v, e := h.ChangeEvidence.View(r.Context(), actor, c)
	if e != nil {
		changeError(w, e)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}
