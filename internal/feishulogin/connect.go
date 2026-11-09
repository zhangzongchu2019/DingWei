package feishulogin

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
)

const connectPageCSP = "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

var connectPageTemplate = template.Must(template.New("connect").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>接入外部 Agent</title>
<style>
body{font-family:system-ui,sans-serif;max-width:36rem;margin:3rem auto;padding:0 1rem;color:#17202a}
label{display:block;margin-top:1rem;font-weight:600}input{box-sizing:border-box;width:100%;padding:.65rem;margin-top:.35rem}
button{margin-top:1.5rem;padding:.65rem 1.2rem}.hint{color:#52606d}.error{color:#a40000}
</style>
</head>
<body>
<h1>接入外部 Agent</h1>
<p class="hint">外部 Agent 名将为 短名_AG序号</p>
{{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
<form method="post" action="" autocomplete="off">
<input type="hidden" name="csrf_token" value="{{.CSRFToken}}">
<input type="hidden" name="attempt_id" value="{{.AttemptID}}">
{{if .Automatic}}
<p class="hint">接入名称由飞书身份自动生成；允许查看在线成员、发送消息、接收消息与确认收件。</p>
{{if .TargetAgent}}<p>本次协作目标：{{.TargetAgent}}</p>{{end}}
<button type="submit">同意并进入飞书</button>
{{else}}
<label for="short_name">短名</label>
<input id="short_name" name="short_name" type="text" required minlength="1" maxlength="8" pattern="[a-z0-9]{1,8}" value="{{.ShortName}}" aria-describedby="short_hint">
<small id="short_hint" class="hint">1～8 位小写英文字母或数字</small>
<label for="target_agent">目标 Agent 全名</label>
<input id="target_agent" name="target_agent" type="text" required maxlength="50" placeholder="名称.属主简称" value="{{.TargetAgent}}">
<button type="submit">前往飞书扫码</button>
{{end}}
</form>
</body>
</html>`))

type connectPageData struct {
	CSRFToken   string
	AttemptID   string
	ShortName   string
	TargetAgent string
	Error       string
	Automatic   bool
}

// ServeConnectPage renders the self-contained connection form on GET and
// validates its one-time CSRF token before entering Start on POST. It never
// lists target Agents and never accepts an MCP client redirect_uri.
func (h *Handler) ServeConnectPage(w http.ResponseWriter, r *http.Request) {
	setConnectPageHeaders(w.Header())
	switch r.Method {
	case http.MethodGet:
		h.renderConnectPage(w, r, http.StatusOK, connectPageData{AttemptID: r.URL.Query().Get("attempt_id")})
	case http.MethodPost:
		h.postConnectPage(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func setConnectPageHeaders(header http.Header) {
	header.Set("Content-Security-Policy", connectPageCSP)
	header.Set("X-Frame-Options", "DENY")
	header.Set("Cache-Control", "no-store")
	header.Set("Referrer-Policy", "no-referrer")
	header.Set("X-Content-Type-Options", "nosniff")
}

func (h *Handler) postConnectPage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	token := r.PostForm.Get("csrf_token")
	if token == "" {
		http.Error(w, "CSRF rejected", http.StatusForbidden)
		return
	}
	attempt, err := h.states.Consume(r.Context(), hashState(token), h.now())
	if err != nil || attempt.Purpose != StatePurposeConnectForm ||
		attempt.AppID != h.appID || attempt.RedirectURI != h.redirectURI ||
		attempt.Request.AttemptID != r.PostForm.Get("attempt_id") {
		http.Error(w, "CSRF rejected", http.StatusForbidden)
		return
	}
	data := connectPageData{
		AttemptID: attempt.Request.AttemptID, ShortName: r.PostForm.Get("short_name"),
		TargetAgent: r.PostForm.Get("target_agent"),
	}
	if attempt.Request.Automatic {
		data.Automatic = true
		data.ShortName = ""
		data.TargetAgent = attempt.Request.TargetAgent
		// Browser-supplied defaults are never trusted, including hidden fields.
		if r.PostForm.Get("short_name") != "" || r.PostForm.Get("target_agent") != "" {
			http.Error(w, "接入请求已失效，请重新授权。", http.StatusBadRequest)
			return
		}
	}
	started, err := h.Start(r.Context(), StartRequest{
		ShortName: data.ShortName, TargetAgent: data.TargetAgent, AttemptID: data.AttemptID, Automatic: data.Automatic,
	})
	if err != nil {
		if !errors.Is(err, ErrInvalidStart) {
			http.Error(w, "login unavailable", http.StatusInternalServerError)
			return
		}
		data.Error = "请检查短名和目标 Agent 全名。"
		h.renderConnectPage(w, r, http.StatusBadRequest, data)
		return
	}
	http.Redirect(w, r, started.AuthorizationURL, http.StatusSeeOther)
}

func (h *Handler) renderConnectPage(w http.ResponseWriter, r *http.Request, status int, data connectPageData) {
	if h.defaults != nil && len(h.defaults.Allowed(h.teamTenantKey)) > 0 {
		data.Automatic = true
		targets := h.validTargets(r.Context())
		if len(targets) == 0 {
			http.Error(w, "没有可用协作目标，请联系管理员。", http.StatusForbidden)
			return
		}
		if len(h.defaults.Allowed(h.teamTenantKey)) == 1 {
			data.TargetAgent = targets[0]
		}
	}
	token, err := h.newState()
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	now := h.now()
	attempt := LoginAttempt{
		StateHash: hashState(token), Purpose: StatePurposeConnectForm,
		AppID: h.appID, RedirectURI: h.redirectURI,
		Request: IssueRequest{AttemptID: data.AttemptID, Automatic: data.Automatic, TargetAgent: data.TargetAgent}, CreatedAt: now, ExpiresAt: now.Add(h.stateTTL),
	}
	if err := h.states.Create(r.Context(), attempt); err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	data.CSRFToken = token
	var body bytes.Buffer
	if err := connectPageTemplate.Execute(&body, data); err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}
