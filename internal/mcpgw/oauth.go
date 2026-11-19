package mcpgw

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/feishulogin"
)

const oauthTTL = 5 * time.Minute

type authorization struct {
	ClientID    string
	Redirect    string
	State       string
	Challenge   string
	BrowserHash string
	Expires     time.Time
}
type codeGrant struct {
	Authorization authorization
	Issued        feishulogin.CallbackResult
}

func randomValue() (string, error) {
	var b [32]byte
	if _, e := rand.Read(b[:]); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}
func oauthError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
func (g *Gateway) resourceMetadata(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	writeJSON(w, 200, map[string]any{"resource": g.cfg.Resource, "authorization_servers": []string{g.cfg.PublicOrigin}, "scopes_supported": extshared.MVPScopes, "bearer_methods_supported": []string{"header"}})
}
func (g *Gateway) serverMetadata(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	writeJSON(w, 200, map[string]any{"issuer": g.cfg.PublicOrigin, "authorization_endpoint": g.cfg.PublicOrigin + "/oauth/authorize", "token_endpoint": g.cfg.PublicOrigin + "/oauth/token", "response_types_supported": []string{"code"}, "grant_types_supported": []string{"authorization_code"}, "code_challenge_methods_supported": []string{"S256"}, "token_endpoint_auth_methods_supported": []string{"none"}, "scopes_supported": extshared.MVPScopes, "client_id_metadata_document_supported": true, "authorization_response_iss_parameter_supported": true})
}
func fixedScopes(s string) bool {
	a := strings.Fields(s)
	b := append([]string(nil), extshared.MVPScopes[:]...)
	if len(a) != len(b) {
		return false
	}
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
func unique(values url.Values) bool {
	for _, v := range values {
		if len(v) != 1 {
			return false
		}
	}
	return true
}

var challengeRE = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var verifierRE = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

func cookieName(attempt string) string { return "__Host-dw-oauth-" + hash(attempt)[:20] }
func (g *Gateway) authorize(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "GET") {
		return
	}
	q, e := url.ParseQuery(r.URL.RawQuery)
	if e != nil || !unique(q) {
		oauthError(w, 400, "invalid_request")
		return
	}
	if q.Get("response_type") != "code" || q.Get("resource") != g.cfg.Resource || q.Get("code_challenge_method") != "S256" || !challengeRE.MatchString(q.Get("code_challenge")) || len(q.Get("state")) > 2048 {
		oauthError(w, 400, "invalid_request")
		return
	}
	if !fixedScopes(q.Get("scope")) {
		oauthError(w, 400, "invalid_scope")
		return
	}
	localClient := q.Get("client_id") == g.cfg.PublicOrigin+codexClientPath
	var client Client
	if localClient {
		// The generated, exact same-origin document is trusted server config.
		// No DNS/network request or arbitrary private-address exception is made.
		client = g.codexClient()
	} else if q.Get("client_id") == codexDefaultClientID {
		// Exact pinned ID only: no online fetch, DCR or private-CIMD exception.
		// Any malformed local copy fails closed, without a network fallback.
		client, e = defaultCodexClient()
	} else {
		client, e = g.cfg.Clients.Resolve(r.Context(), q.Get("client_id"))
	}
	redirectAllowed := registered(client, q.Get("redirect_uri"))
	if localClient || q.Get("client_id") == codexDefaultClientID {
		redirectAllowed = registeredCodexRedirect(client, q.Get("redirect_uri"))
	}
	if e != nil || (!localClient && validateClient(client, q.Get("client_id")) != nil) || !redirectAllowed {
		oauthError(w, 400, "invalid_client")
		return
	}
	attempt, e := randomValue()
	if e != nil {
		oauthError(w, 500, "server_error")
		return
	}
	browser, e := randomValue()
	if e != nil {
		oauthError(w, 500, "server_error")
		return
	}
	now := g.cfg.Now()
	a := authorization{ClientID: client.ID, Redirect: q.Get("redirect_uri"), State: q.Get("state"), Challenge: q.Get("code_challenge"), BrowserHash: hash(browser), Expires: now.Add(oauthTTL)}
	raw, _ := json.Marshal(a)
	if e = g.cfg.Store.Put(r.Context(), "attempt", hash(attempt), raw, a.Expires, now); e != nil {
		oauthError(w, 503, "temporarily_unavailable")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName(attempt), Value: browser, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: int(oauthTTL.Seconds())})
	http.Redirect(w, r, "/feishu/connect?attempt_id="+url.QueryEscape(attempt), http.StatusFound)
}
func (g *Gateway) readAttempt(r *http.Request, attempt string, take bool) (authorization, error) {
	var a authorization
	if attempt == "" || len(attempt) > 128 {
		return a, errRecord
	}
	raw, e := g.cfg.Store.Get(r.Context(), "attempt", hash(attempt), g.cfg.Now())
	if e != nil {
		return a, e
	}
	if json.Unmarshal(raw, &a) != nil {
		return a, errRecord
	}
	cookie, e := r.Cookie(cookieName(attempt))
	if e != nil || subtle.ConstantTimeCompare([]byte(hash(cookie.Value)), []byte(a.BrowserHash)) != 1 {
		return authorization{}, errRecord
	}
	if take {
		raw, e = g.cfg.Store.Take(r.Context(), "attempt", hash(attempt), g.cfg.Now())
		if e != nil {
			return authorization{}, e
		}
		if json.Unmarshal(raw, &a) != nil {
			return authorization{}, errRecord
		}
	}
	return a, nil
}
func (g *Gateway) connect(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		oauthError(w, 405, "invalid_request")
		return
	}
	var attempt string
	if r.Method == "GET" {
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || !unique(q) {
			oauthError(w, 400, "invalid_request")
			return
		}
		attempt = q.Get("attempt_id")
	} else {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || !unique(r.PostForm) {
			oauthError(w, 400, "invalid_request")
			return
		}
		attempt = r.PostForm.Get("attempt_id")
	}
	a, attemptErr := g.readAttempt(r, attempt, false)
	if attemptErr != nil {
		failConnectAttempt(w, r)
		return
	}
	// C 的重定向 state 与 B 事务关联，回调先验浏览器和期限再允许 C 签发。
	captured := &loginResponse{header: make(http.Header)}
	g.cfg.Login.ServeConnectPage(captured, r)
	// Browsers check form-action on every hop of the form's redirect chain. When
	// Feishu skips its consent page (already-authorized user) the chain runs
	// connect POST → Feishu → callback → client loopback in one navigation, so
	// the attempt's validated loopback origin must be allowed too (r56.2 CR).
	if csp := captured.header.Get("Content-Security-Policy"); csp != "" {
		captured.header.Set("Content-Security-Policy", withLoopbackFormAction(csp, a.Redirect))
	}
	if captured.status == http.StatusSeeOther {
		location, e := url.Parse(captured.header.Get("Location"))
		if e != nil || location.Query().Get("state") == "" {
			oauthError(w, 500, "server_error")
			return
		}
		now := g.cfg.Now()
		// Claim the enclosing OAuth attempt once, not merely each Feishu state.
		// Multiple tabs/forms may have distinct CSRF tokens for the same attempt.
		// Losing starts remain unbound and cannot reach profile/name generation.
		if e = g.cfg.Store.Put(r.Context(), "login", hash("started:"+attempt), []byte(attempt), a.Expires, now); e != nil {
			oauthError(w, 400, "invalid_request")
			return
		}
		if e = g.cfg.Store.Put(r.Context(), "login", hash("state:"+location.Query().Get("state")), []byte(attempt), a.Expires, now); e != nil {
			oauthError(w, 503, "temporarily_unavailable")
			return
		}
	}
	for k, v := range captured.header {
		w.Header()[k] = v
	}
	status := captured.status
	if status == 0 {
		status = 200
	}
	w.WriteHeader(status)
	_, _ = w.Write(captured.body.Bytes())
}
func (g *Gateway) callback(w http.ResponseWriter, r *http.Request) {
	var req feishulogin.CallbackRequest
	kind, key := "login", ""
	if r.Method == "POST" {
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if r.ParseForm() != nil || !unique(r.PostForm) || r.URL.RawQuery != "" || r.PostForm.Get("selection") == "" || r.PostForm.Get("target_agent") == "" {
			g.failCallback(w, r, http.StatusBadRequest, "invalid_request", feishulogin.FailureOther, "", feishulogin.TargetCategoryUnverified)
			return
		}
		req = feishulogin.CallbackRequest{Selection: r.PostForm.Get("selection"), Target: r.PostForm.Get("target_agent")}
		kind, key = "login", "selection:"+req.Selection
	} else if r.Method == "GET" {
		q, e := url.ParseQuery(r.URL.RawQuery)
		if e != nil || !unique(q) || q.Get("state") == "" || q.Get("code") == "" {
			g.failCallback(w, r, http.StatusBadRequest, "invalid_request", feishulogin.FailureOther, "", feishulogin.TargetCategoryUnverified)
			return
		}
		req = feishulogin.CallbackRequestFromQuery(q)
		key = "state:" + req.State
	} else {
		w.Header().Set("Allow", "GET, POST")
		g.failCallback(w, r, http.StatusMethodNotAllowed, "method_not_allowed", feishulogin.FailureOther, "", feishulogin.TargetCategoryUnverified)
		return
	}
	if len(key) > 128 {
		g.failCallback(w, r, http.StatusBadRequest, "invalid_request", feishulogin.FailureOther, "", feishulogin.TargetCategoryUnverified)
		return
	}
	attemptRaw, e := g.cfg.Store.Get(r.Context(), kind, hash(key), g.cfg.Now())
	if e != nil {
		reason := feishulogin.FailureOther
		if errors.Is(e, errRecord) {
			reason = feishulogin.FailureAuthorizationUsed
		}
		g.failCallback(w, r, http.StatusBadRequest, "invalid_request", reason, "", feishulogin.TargetCategoryUnverified)
		return
	}
	attempt := string(attemptRaw)
	a, e := g.readAttempt(r, attempt, false)
	if e != nil {
		g.failCallback(w, r, http.StatusBadRequest, "invalid_request", feishulogin.FailureAuthorizationUsed, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	if _, e = g.cfg.Store.Take(r.Context(), kind, hash(key), g.cfg.Now()); e != nil {
		reason := feishulogin.FailureOther
		if errors.Is(e, errRecord) {
			reason = feishulogin.FailureAuthorizationUsed
		}
		g.failCallback(w, r, http.StatusBadRequest, "invalid_request", reason, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	// The gateway binds both OAuth and selection callbacks to its encrypted record.
	// No attempt or principal is accepted from callback query/form fields.
	req.AttemptID = attempt
	issued, e := g.cfg.Login.Callback(r.Context(), req)
	if e != nil {
		reason, attemptPrefix, targetCategory, ok := feishulogin.CallbackFailureDetails(e)
		if !ok {
			reason, attemptPrefix, targetCategory = feishulogin.FailureOther, attempt, feishulogin.TargetCategoryUnverified
		}
		g.failCallback(w, r, http.StatusBadRequest, "access_denied", reason, attemptPrefix, targetCategory)
		return
	}
	if issued.Selection != "" {
		g.renderSelection(w, r, issued, attempt, a)
		return
	}

	if issued.AttemptID != attempt {
		g.failCallback(w, r, http.StatusBadRequest, "invalid_request", feishulogin.FailureOther, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	a, e = g.readAttempt(r, attempt, true)
	if e != nil {
		g.failCallback(w, r, http.StatusBadRequest, "invalid_request", feishulogin.FailureAuthorizationUsed, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	if issued.Token == "" || issued.Audience != g.cfg.Resource || !fixedScopes(strings.Join(issued.Scopes, " ")) || !issued.ExpiresAt.After(g.cfg.Now()) {
		g.failCallback(w, r, http.StatusInternalServerError, "server_error", feishulogin.FailureOther, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	code, e := randomValue()
	if e != nil {
		g.failCallback(w, r, http.StatusInternalServerError, "server_error", feishulogin.FailureOther, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	grant := codeGrant{Authorization: a, Issued: issued}
	raw, _ := json.Marshal(grant)
	now := g.cfg.Now()
	expires := now.Add(oauthTTL)
	if a.Expires.Before(expires) {
		expires = a.Expires
	}
	if e = g.cfg.Store.Put(r.Context(), "code", hash(code), raw, expires, now); e != nil {
		g.failCallback(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", feishulogin.FailureOther, attempt, feishulogin.TargetCategoryUnverified)
		return
	}
	redirect, _ := url.Parse(a.Redirect)
	params := redirect.Query()
	params.Set("code", code)
	if a.State != "" {
		params.Set("state", a.State)
	}
	params.Set("iss", g.cfg.PublicOrigin)
	redirect.RawQuery = params.Encode()
	http.SetCookie(w, &http.Cookie{Name: cookieName(issued.AttemptID), Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, redirect.String(), http.StatusFound)
}
func (g *Gateway) token(w http.ResponseWriter, r *http.Request) {
	if !method(w, r, "POST") {
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		oauthError(w, 400, "invalid_request")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if r.ParseForm() != nil || !unique(r.PostForm) || len(r.URL.Query()) != 0 {
		oauthError(w, 400, "invalid_request")
		return
	}
	f := r.PostForm
	if f.Get("grant_type") != "authorization_code" {
		oauthError(w, 400, "unsupported_grant_type")
		return
	}
	if f.Get("resource") != g.cfg.Resource || !verifierRE.MatchString(f.Get("code_verifier")) || f.Get("code") == "" || len(f.Get("code")) > 128 {
		oauthError(w, 400, "invalid_grant")
		return
	}
	raw, e := g.cfg.Store.Take(r.Context(), "code", hash(f.Get("code")), g.cfg.Now())
	if e != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	var grant codeGrant
	if json.Unmarshal(raw, &grant) != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	a := grant.Authorization
	sum := sha256.Sum256([]byte(f.Get("code_verifier")))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if a.ClientID != f.Get("client_id") || a.Redirect != f.Get("redirect_uri") || subtle.ConstantTimeCompare([]byte(a.Challenge), []byte(challenge)) != 1 {
		oauthError(w, 400, "invalid_grant")
		return
	}
	// 回调后到兑换前仍可能吊销/到期；交付前现核 A，不用回调快照授权。
	seat, e := g.cfg.Seats.Authenticate(r.Context(), grant.Issued.Token)
	if e != nil {
		oauthError(w, 400, "invalid_grant")
		return
	}
	expiry := seat.ExpiresAt
	if expiry.IsZero() {
		oauthError(w, 500, "server_error")
		return
	}
	seconds := int64(expiry.Sub(g.cfg.Now()).Seconds())
	if seconds <= 0 {
		oauthError(w, 400, "invalid_grant")
		return
	}
	writeJSON(w, 200, map[string]any{"access_token": grant.Issued.Token, "token_type": "Bearer", "expires_in": seconds, "scope": extshared.ScopeString()})
}

// C 接入页是有限的 HTML/重定向响应；暂存至关联 state 持久化成功才发送。
type loginResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *loginResponse) Header() http.Header { return w.header }
func (w *loginResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *loginResponse) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = 200
	}
	return w.body.Write(b)
}

var loopbackFormSource = regexp.MustCompile(`(?i)^http://(127\.0\.0\.1|\[::1\]|localhost):([0-9]{1,5})$`)

// withLoopbackFormAction appends exactly the validated loopback origin of the
// attempt's redirect_uri to form-action; anything else leaves the policy as is.
func withLoopbackFormAction(csp, redirect string) string {
	u, err := url.Parse(redirect)
	if err != nil || u.User != nil {
		return csp
	}
	source := u.Scheme + "://" + u.Host
	match := loopbackFormSource.FindStringSubmatch(source)
	if match == nil || !strings.Contains(csp, "form-action 'self'") {
		return csp
	}
	port, err := strconv.Atoi(match[2])
	if err != nil || port < 1 || port > 65535 {
		return csp
	}
	host := strings.ToLower(match[1])
	if host == "[::1]" {
		host = "[::1]"
	}
	source = "http://" + host + ":" + strconv.Itoa(port)
	return strings.Replace(csp, "form-action 'self'", "form-action 'self' "+source, 1)
}
