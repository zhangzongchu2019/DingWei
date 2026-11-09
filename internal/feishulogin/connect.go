package feishulogin

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"regexp"
)

const connectPageCSP = "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// connectPageCSPFor allows the form to follow its 303 to the Feishu authorize
// origin. Browsers enforce form-action across redirects, so 'self' alone blocks
// the hop to Feishu (r56.1 现网实测). Only one exact https origin is added.
func connectPageCSPFor(authorizeOrigin string) string {
	if authorizeOrigin == "" {
		return connectPageCSP
	}
	return "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; form-action 'self' " + authorizeOrigin + "; frame-ancestors 'none'"
}

var cspOriginHost = regexp.MustCompile(`^[A-Za-z0-9.-]+(:[0-9]{1,5})?$`)

// authorizeOriginOf derives the exact origin of the configured Feishu authorize
// endpoint without network access; anything unusual keeps form-action 'self'.
func authorizeOriginOf(client OAuthClient, redirectURI string) string {
	raw, err := client.AuthorizationURL("csp-origin-probe", redirectURI)
	if err != nil {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || !cspOriginHost.MatchString(u.Host) {
		return ""
	}
	return "https://" + u.Host
}

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
<label for="short_name">短名</label>
<input id="short_name" name="short_name" type="text" required minlength="1" maxlength="8" pattern="[a-z0-9]{1,8}" value="{{.ShortName}}" aria-describedby="short_hint">
<small id="short_hint" class="hint">1～8 位小写英文字母或数字</small>
<label for="target_agent">目标 Agent 全名</label>
<input id="target_agent" name="target_agent" type="text" required maxlength="50" placeholder="名称.属主简称" value="{{.TargetAgent}}">
<button type="submit">前往飞书扫码</button>
</form>
</body>
</html>`))

type connectPageData struct {
	CSRFToken   string
	AttemptID   string
	ShortName   string
	TargetAgent string
	Error       string
}

// ServeConnectPage renders the self-contained connection form on GET and
// validates its one-time CSRF token before entering Start on POST. It never
// lists target Agents and never accepts an MCP client redirect_uri.
func (h *Handler) ServeConnectPage(w http.ResponseWriter, r *http.Request) {
	setConnectPageHeaders(w.Header(), h.connectCSP)
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

func setConnectPageHeaders(header http.Header, csp string) {
	if csp == "" {
		csp = connectPageCSP
	}
	header.Set("Content-Security-Policy", csp)
	header.Set("X-Frame-Options", "DENY")
	header.Set("Cache-Control", "no-store")
	// same-origin, not no-referrer: under no-referrer browsers serialize the
	// form POST Origin as "null", which the gateway origin guard rejects.
	header.Set("Referrer-Policy", "same-origin")
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
	started, err := h.Start(r.Context(), StartRequest{
		ShortName: data.ShortName, TargetAgent: data.TargetAgent, AttemptID: data.AttemptID,
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
	token, err := h.newState()
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	now := h.now()
	attempt := LoginAttempt{
		StateHash: hashState(token), Purpose: StatePurposeConnectForm,
		AppID: h.appID, RedirectURI: h.redirectURI,
		Request: IssueRequest{AttemptID: data.AttemptID}, CreatedAt: now, ExpiresAt: now.Add(h.stateTTL),
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
