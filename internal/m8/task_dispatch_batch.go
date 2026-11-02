package m8

import (
	"encoding/json"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
	"io"
	"net/http"
	"net/url"
)

func (h *Hub) HandleTaskDispatchBatch(w http.ResponseWriter, r *http.Request) {
	h.handleTaskDispatchBatch(w, r, false)
}
func (h *Hub) HandleAgentTaskDispatchBatch(w http.ResponseWriter, r *http.Request) {
	h.handleTaskDispatchBatch(w, r, true)
}
func (h *Hub) handleTaskDispatchBatch(w http.ResponseWriter, r *http.Request, agent bool) {
	if !td.Enabled() {
		http.NotFound(w, r)
		return
	}
	svc := h.taskDispatchService()
	if svc == nil {
		taskDispatchError(w, td.ErrUnavailable)
		return
	}
	if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" || r.URL.RawQuery != "" {
		taskDispatchError(w, td.ErrInvalid)
		return
	}
	if !agent {
		origin, e := url.Parse(r.Header.Get("Origin"))
		if e != nil || origin.Host != r.Host || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" || (origin.Scheme != "https" && origin.Scheme != "http") || (h.PublicOrigin != "" && r.Header.Get("Origin") != h.PublicOrigin) || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			taskDispatchError(w, td.ErrForbidden)
			return
		}
	}
	var request struct {
		Operations []td.Request `json:"operations"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF {
		taskDispatchError(w, td.ErrInvalid)
		return
	}
	actor := td.Actor{UserLC: platformPrincipal(r)}
	if agent {
		actor = td.Actor{}
		actor.AgentID, _ = r.Context().Value(taskAgentContextKey{}).(string)
	}
	out, e := svc.ExecuteBatch(r.Context(), actor, request.Operations)
	if e != nil {
		taskDispatchError(w, e)
		return
	}
	status := http.StatusOK
	if out.Failed > 0 {
		status = http.StatusMultiStatus
	}
	taskJSON(w, status, out)
}
