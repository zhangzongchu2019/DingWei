package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

type ClaimBot struct {
	Name  string `json:"name"`
	AppID string `json:"app_id"`
	URL   string `json:"url"`
}

func (a *API) claimApply(w http.ResponseWriter, r *http.Request) {
	if a.Claims == nil {
		writeErr(w, 503, "self_enroll_unavailable", nil)
		return
	}
	var bots []ClaimBot
	if a.ClaimBots != nil {
		var err error
		bots, err = a.ClaimBots(r.Context())
		if err != nil || len(bots) == 0 {
			writeErr(w, 503, "feishu_pairing_unavailable", nil)
			return
		}
	}
	var req identity.ClaimRequest
	if !decodeClaim(w, r, &req) {
		return
	}
	result, err := a.Claims.ApplyDeviceClaim(r.Context(), req, a.sourceIP(r))
	if err != nil {
		mapErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"claim_id": result.ID, "poll_token": result.PollToken, "expires_at": result.ExpiresAt.UTC().Format(time.RFC3339), "command": "#pair", "state": "awaiting_feishu", "feishu_bots": bots})
}
func (a *API) claimPoll(w http.ResponseWriter, r *http.Request) {
	if a.Claims == nil {
		writeErr(w, 503, "self_enroll_unavailable", nil)
		return
	}
	token := bearer(r)
	if token == "" {
		writeErr(w, 404, "not_found", nil)
		return
	}
	result, err := a.Claims.PollDeviceClaim(r.Context(), r.PathValue("id"), token)
	if err != nil {
		mapErr(w, err)
		return
	}
	body := map[string]any{"state": result.State}
	if result.LastError != "" {
		body["last_error"] = result.LastError
	}
	if result.UserID != "" {
		body["owner_reused"] = result.OwnerReused
		body["user_id"] = result.UserID
		body["agent_name"] = result.AgentName
		body["agent_full_name"] = result.FullName
	}
	if result.Credential != "" {
		body["credential"] = result.Credential
	}
	writeJSON(w, 200, body)
}
func (a *API) claimAck(w http.ResponseWriter, r *http.Request) {
	if a.Claims == nil {
		writeErr(w, 503, "self_enroll_unavailable", nil)
		return
	}
	token := bearer(r)
	if token == "" {
		writeErr(w, 404, "not_found", nil)
		return
	}
	if err := a.Claims.AckDeviceClaim(r.Context(), r.PathValue("id"), token); err != nil {
		mapErr(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(204)
}

// Claim fields are flat strings. Reject duplicate keys and trailing JSON rather
// than allowing proxies/UI and the Hub to disagree about the requested owner.
func decodeClaim(w http.ResponseWriter, r *http.Request, out *identity.ClaimRequest) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	fail := func() bool { writeErr(w, 400, "bad_request", nil); return false }
	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return fail()
	}
	fields := map[string]string{}
	allowed := map[string]bool{"requested_user_id": true, "requested_short_name": true, "agent_name": true, "code_digest": true, "device_fingerprint": true, "client_version": true, "mode": true}
	for dec.More() {
		key, e := dec.Token()
		if e != nil {
			return fail()
		}
		name, ok := key.(string)
		if !ok || !allowed[name] {
			return fail()
		}
		if _, seen := fields[name]; seen {
			return fail()
		}
		var value string
		if e = dec.Decode(&value); e != nil {
			return fail()
		}
		fields[name] = value
	}
	if token, err = dec.Token(); err != nil || token != json.Delim('}') {
		return fail()
	}
	if _, err = dec.Token(); err != io.EOF {
		return fail()
	}
	data, err := json.Marshal(fields)
	if err != nil {
		return fail()
	}
	if err = json.Unmarshal(data, out); err != nil {
		return fail()
	}
	return true
}
