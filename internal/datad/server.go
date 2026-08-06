package datad

// dingwei-datad HTTP 服务(设计冻结版 §3/§4):唯一 PG 入口;
// 两不变量在此执法——①管理域写=role:coordinator 唯一(task 状态唯一例外=owner
// 经 /pull 白名单流转 dispatched→in_progress/in_progress→review,zzc 批 B 案)
// ②自报域=行级(token.session==目标行;task progress/pull=owner 本人)。
// 每写一条 audit(含未遂 403)。
// me 别名:路径/查询中的 "me" 按 token 解析(§7b:shim 不感知身份)。

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Identity token 注册表条目(datad 配置文件,PG 不存明文 token)。
type Identity struct {
	Session string `json:"session" yaml:"session"` // zzc-xxx-2642 全名
	Role    string `json:"role" yaml:"role"`       // coordinator|member
}

type Config struct {
	Store  Store
	Tokens map[string]Identity // token→身份;空=全 401
	Logger *slog.Logger
	KB     KBProvider // nil=kb 端点 501
}

type Server struct {
	st     Store
	tokMu  sync.RWMutex
	tokens map[string]Identity // P8:轮换=ReloadTokens 原子换表(双活窗=新旧同在文件)
	kb     KBProvider
	log    *slog.Logger
	mux    *http.ServeMux
}

// ReloadTokens P8 token 轮换:原子换表;双活窗语义=轮换期文件同含新旧 token。
func (s *Server) ReloadTokens(tokens map[string]Identity) {
	s.tokMu.Lock()
	s.tokens = tokens
	s.tokMu.Unlock()
	s.log.Info("token 注册表已热载", "count", len(tokens))
}

func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Server{st: cfg.Store, tokens: cfg.Tokens, kb: cfg.KB, log: cfg.Logger, mux: http.NewServeMux()}
	m := s.mux
	m.HandleFunc("GET /v1/tasks", s.withAuth(s.handleListTasks))
	m.HandleFunc("GET /v1/tasks/{id}", s.withAuth(s.handleGetTask))
	m.HandleFunc("PUT /v1/tasks/{id}", s.withAuth(s.handlePutTask))
	m.HandleFunc("POST /v1/tasks/{id}/progress", s.withAuth(s.handleProgress))
	m.HandleFunc("POST /v1/tasks/{id}/pull", s.withAuth(s.handlePullTask))
	m.HandleFunc("GET /v1/tasks/pull-lag", s.withAuth(s.handlePullLag))
	m.HandleFunc("POST /v1/dispatch", s.withAuth(s.handleDispatch))
	m.HandleFunc("GET /v1/dispatch", s.withAuth(s.handleListDispatch))
	m.HandleFunc("GET /v1/heartbeats/latest", s.withAuth(s.handleLatestHeartbeats))
	m.HandleFunc("GET /v1/config", s.withAuth(s.handleListConfig))
	m.HandleFunc("GET /v1/config/{key}", s.withAuth(s.handleGetConfig))
	m.HandleFunc("PUT /v1/config/{key}", s.withAuth(s.handlePutConfig))
	m.HandleFunc("GET /v1/sessions", s.withAuth(s.handleListSessions))
	m.HandleFunc("PUT /v1/sessions/{name}/state", s.withAuth(s.handlePutState))
	m.HandleFunc("POST /v1/sessions/{name}/heartbeat", s.withAuth(s.handleHeartbeat))
	m.HandleFunc("GET /v1/skills", s.withAuth(s.handleListSkills))
	m.HandleFunc("POST /v1/skills", s.withAuth(s.handleProposeSkill))
	m.HandleFunc("POST /v1/skills/{name}/{version}/approve", s.withAuth(s.handleApproveSkill))
	m.HandleFunc("GET /v1/audit", s.withAuth(s.handleListAudit))
	m.HandleFunc("POST /v1/kb/query", s.withAuth(s.handleKBQuery))
	m.HandleFunc("POST /v1/kb/ingest", s.withAuth(s.handleKBIngest))
	m.HandleFunc("GET /healthz", s.handleHealthz)
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// ---- 鉴权/审计中间件 ----

type authedHandler func(w http.ResponseWriter, r *http.Request, id Identity)

func (s *Server) withAuth(h authedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authz := r.Header.Get("Authorization")
		if !strings.HasPrefix(authz, "Bearer ") { // 协议严格(claudek3 nit):必带 Bearer 前缀,裸 token 拒
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		tok := strings.TrimPrefix(authz, "Bearer ")
		s.tokMu.RLock()
		tokens := s.tokens
		s.tokMu.RUnlock()
		for t, id := range tokens {
			if subtle.ConstantTimeCompare([]byte(tok), []byte(t)) == 1 {
				h(w, r, id)
				return
			}
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}
}

// deny 403+审计未遂(§4:未遂也记账)。
func (s *Server) deny(w http.ResponseWriter, r *http.Request, id Identity, op string) {
	s.audit(r.Context(), id, op+".denied", r.URL.Path, nil)
	http.Error(w, "forbidden", http.StatusForbidden)
}

func (s *Server) audit(ctx context.Context, id Identity, op, target string, detail any) {
	var raw json.RawMessage
	if detail != nil {
		raw, _ = json.Marshal(detail)
	}
	if err := s.st.AppendAudit(ctx, AuditEntry{
		Actor: id.Session, Op: op, Target: target, Detail: raw, At: time.Now().UTC(),
	}); err != nil {
		s.log.Error("audit写入失败(告警)", "op", op, "error", err)
	}
}

// resolveMe me 别名(§7b):路径段/查询值 "me"→token 会话全名。
func resolveMe(v string, id Identity) string {
	if v == "me" {
		return id.Session
	}
	return v
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	case errors.Is(err, ErrConflict):
		http.Error(w, "optimistic lock conflict", http.StatusConflict)
	case errors.Is(err, ErrCASFailed):
		http.Error(w, "generation mismatch", http.StatusPreconditionFailed)
	default: // PG 不可达等=503 fail-closed(P9),不静默降级
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
	}
}

func decodeBody(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// ---- tasks ----

func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request, id Identity) {
	tasks, err := s.st.ListTasks(r.Context(), r.URL.Query().Get("status"),
		resolveMe(r.URL.Query().Get("owner"), id))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, tasks)
}

func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request, id Identity) {
	t, err := s.st.GetTask(r.Context(), r.PathValue("id"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, t)
}

func (s *Server) handlePutTask(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" { // 单写不变量
		s.deny(w, r, id, "task.put")
		return
	}
	var t Task
	if err := decodeBody(r, &t); err != nil {
		http.Error(w, "bad schema: "+err.Error(), http.StatusBadRequest)
		return
	}
	t.ID = r.PathValue("id")
	if t.Title == "" || t.OwnerSession == "" || !taskStatuses[t.Status] {
		http.Error(w, "title/owner_session/status 必填且合法", http.StatusUnprocessableEntity)
		return
	}
	var ifMatch time.Time
	force := false
	if im := r.Header.Get("If-Match"); im != "" {
		ts, err := time.Parse(time.RFC3339Nano, im)
		if err != nil {
			http.Error(w, "If-Match 须 RFC3339Nano(取上次 updated_at)", http.StatusBadRequest)
			return
		}
		ifMatch = ts
	} else if _, err := s.st.GetTask(r.Context(), t.ID); errors.Is(err, ErrNotFound) {
		force = true // 首建免锁
	} else {
		http.Error(w, "已存在件须带 If-Match", http.StatusConflict)
		return
	}
	out, err := s.st.PutTask(r.Context(), t, ifMatch, force)
	if err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "task.put", t.ID, map[string]string{"status": t.Status})
	writeJSON(w, 200, out)
}

func (s *Server) handleProgress(w http.ResponseWriter, r *http.Request, id Identity) {
	taskID := r.PathValue("id")
	t, err := s.st.GetTask(r.Context(), taskID)
	if err != nil {
		storeErr(w, err)
		return
	}
	if id.Role != "coordinator" && t.OwnerSession != id.Session { // 自报域:owner 本人
		s.deny(w, r, id, "task.progress")
		return
	}
	var p TaskProgress
	if err := decodeBody(r, &p); err != nil {
		http.Error(w, "bad schema: "+err.Error(), http.StatusBadRequest)
		return
	}
	out, err := s.st.UpdateProgress(r.Context(), taskID, p)
	if err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "task.progress", taskID, p)
	writeJSON(w, 200, out)
}

func (s *Server) handleDispatch(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" {
		s.deny(w, r, id, "dispatch.append")
		return
	}
	var d Dispatch
	if err := decodeBody(r, &d); err != nil || d.TaskID == "" || d.Assignee == "" {
		http.Error(w, "bad dispatch", http.StatusBadRequest)
		return
	}
	if d.Role == "" {
		d.Role = "executor"
	}
	if err := s.st.AppendDispatch(r.Context(), d); err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "dispatch.append", d.TaskID, d)
	writeJSON(w, 201, d)
}

// handlePullTask POST /v1/tasks/{id}/pull:自报域执行态流转(zzc 批 B 案自取)。
// owner 本人,白名单仅两条:to=in_progress(拉取,每次一项闸)/to=review(交审)。
// 派发/终态/越闸插队权仍 coordinator=PUT 单写路径,此端点不给 coordinator 特权。
func (s *Server) handlePullTask(w http.ResponseWriter, r *http.Request, id Identity) {
	taskID := r.PathValue("id")
	var req struct {
		To string `json:"to"`
	}
	if err := decodeBody(r, &req); err != nil || (req.To != "in_progress" && req.To != "review") {
		http.Error(w, "to 须 in_progress|review", http.StatusUnprocessableEntity)
		return
	}
	t, err := s.st.OwnerTransition(r.Context(), taskID, id.Session, req.To)
	switch {
	case errors.Is(err, ErrNotOwner):
		s.deny(w, r, id, "task.pull")
		return
	case errors.Is(err, ErrPullGate):
		http.Error(w, "pull_gate: 名下已有 in_progress,先交审再拉", http.StatusConflict)
		return
	case errors.Is(err, ErrWrongState):
		http.Error(w, "wrong_state: 仅 dispatched→in_progress / in_progress→review", http.StatusConflict)
		return
	case err != nil:
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "task.pull."+req.To, taskID, map[string]string{"to": req.To})
	writeJSON(w, 200, t)
}

// handlePullLag GET /v1/tasks/pull-lag:coordinator 监控面(zzc 令)——谁名下有
// dispatched 停留未拉,按最久在前;age 由服务端计算。
// 已知边界(CR n3/n4 记账):task id 恰为"pull-lag"会被本路由遮蔽取不到单件;
// 查询无 LIMIT,dispatched 常态量级小,量涨加分页=backlog。
func (s *Server) handlePullLag(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" {
		s.deny(w, r, id, "task.pull_lag.read")
		return
	}
	out, err := s.st.PullLag(r.Context())
	if err != nil {
		storeErr(w, err)
		return
	}
	now := time.Now().UTC()
	for i := range out {
		age := int64(now.Sub(out[i].Since).Seconds())
		if age < 0 { // 时钟回拨钳 0(CR n6),不输出负龄误导消费方
			age = 0
		}
		out[i].AgeSeconds = age
	}
	writeJSON(w, 200, out)
}

// handleListDispatch GET /v1/dispatch:dispatch 权威只读侧(audit 重建不权威——
// audit 写失败仅告警不回滚,可缺行)。coordinator=全量;member=自报域行级
// (zzc 批 Q1:须 assignee=me/本人全名,否则 403——同 sessions 行级规)。
// 参数:task_id/assignee(过滤)/since_id(增量下界,不含)/limit(默认与上限 1000);id 升序。
func (s *Server) handleListDispatch(w http.ResponseWriter, r *http.Request, id Identity) {
	q := r.URL.Query()
	assignee := resolveMe(q.Get("assignee"), id)
	if id.Role != "coordinator" && assignee != id.Session {
		s.deny(w, r, id, "dispatch.read")
		return
	}
	var sinceID int64
	if v := q.Get("since_id"); v != "" { // 解析错=400 不静默当 0(n2)
		var err error
		if sinceID, err = strconv.ParseInt(v, 10, 64); err != nil || sinceID < 0 {
			http.Error(w, "since_id 须非负整数", http.StatusBadRequest)
			return
		}
	}
	limit := 1000
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 {
			http.Error(w, "limit 须正整数", http.StatusBadRequest)
			return
		}
		if n < limit {
			limit = n
		}
	}
	out, err := s.st.ListDispatches(r.Context(), q.Get("task_id"), assignee, sinceID, limit)
	if err != nil {
		storeErr(w, err)
		return
	}
	if len(out) == limit { // 截断信号(n3):体保持纯数组,游标走响应头
		w.Header().Set("X-Has-More", "true")
		w.Header().Set("X-Next-Since-Id", strconv.FormatInt(out[len(out)-1].ID, 10))
	}
	writeJSON(w, 200, out)
}

// handleLatestHeartbeats GET /v1/heartbeats/latest:每会话最新心跳(活性核/快照)。
// coordinator 读(planning 面;明细行不开列表,高频表全量读无消费方)。
func (s *Server) handleLatestHeartbeats(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" {
		s.deny(w, r, id, "heartbeat.read")
		return
	}
	out, err := s.st.LatestHeartbeats(r.Context())
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

// ---- config ----

// handleListConfig GET /v1/config:每 key 最新代次(project_context 配置上下文用)。
func (s *Server) handleListConfig(w http.ResponseWriter, r *http.Request, id Identity) {
	entries, err := s.st.ListConfig(r.Context())
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, entries)
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request, id Identity) {
	gen := int64(0)
	if g := r.URL.Query().Get("generation"); g != "" {
		gen, _ = strconv.ParseInt(g, 10, 64)
	}
	e, err := s.st.GetConfig(r.Context(), r.PathValue("key"), gen)
	if err != nil {
		storeErr(w, err)
		return
	}
	w.Header().Set("ETag", strconv.FormatInt(e.Generation, 10))
	writeJSON(w, 200, e)
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" {
		s.deny(w, r, id, "config.put")
		return
	}
	expected, err := strconv.ParseInt(r.Header.Get("If-Match"), 10, 64)
	if r.Header.Get("If-Match") == "" {
		expected, err = 0, nil // 新 key 首写
	}
	if err != nil {
		http.Error(w, "If-Match 须整数代次", http.StatusBadRequest)
		return
	}
	value, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if rerr != nil || !json.Valid(value) {
		http.Error(w, "value 须合法 JSON", http.StatusBadRequest)
		return
	}
	e, err := s.st.PutConfig(r.Context(), r.PathValue("key"), value, expected, id.Session)
	if err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "config.put", e.Key, map[string]int64{"generation": e.Generation})
	w.Header().Set("ETag", strconv.FormatInt(e.Generation, 10))
	writeJSON(w, 200, e)
}

// ---- sessions(行级自报域) ----

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request, id Identity) {
	out, err := s.st.ListSessions(r.Context())
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handlePutState(w http.ResponseWriter, r *http.Request, id Identity) {
	name := resolveMe(r.PathValue("name"), id)
	if name != id.Session { // 行级不变量:coordinator 也不代写他人状态(状态=自报语义)
		s.deny(w, r, id, "session.state")
		return
	}
	var st SessionState
	if err := decodeBody(r, &st); err != nil || !sessionStates[st.State] {
		http.Error(w, "bad state", http.StatusUnprocessableEntity)
		return
	}
	st.Session = name
	if err := s.st.PutSessionState(r.Context(), st); err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "session.state", name, map[string]string{"state": st.State})
	writeJSON(w, 200, st)
}

func (s *Server) handleHeartbeat(w http.ResponseWriter, r *http.Request, id Identity) {
	name := resolveMe(r.PathValue("name"), id)
	if name != id.Session {
		s.deny(w, r, id, "session.heartbeat")
		return
	}
	payload, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if len(payload) > 0 && !json.Valid(payload) {
		http.Error(w, "payload 须 JSON", http.StatusBadRequest)
		return
	}
	if err := s.st.AppendHeartbeat(r.Context(), name, payload, time.Now().UTC()); err != nil {
		storeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted) // 心跳高频:不记 audit(设计 §4 审计=写操作;心跳=打点面,PG 表自身即账)
}

// ---- skills ----

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request, id Identity) {
	out, err := s.st.ListSkills(r.Context())
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleProposeSkill(w http.ResponseWriter, r *http.Request, id Identity) {
	var sk Skill
	if err := decodeBody(r, &sk); err != nil || sk.Name == "" || sk.Version == "" ||
		sk.Content == "" || sk.SHA256 == "" {
		http.Error(w, "name/version/content/sha256 必填", http.StatusUnprocessableEntity)
		return
	}
	sk.ProposedBy = id.Session
	sk.Status = "proposed"
	if err := s.st.ProposeSkill(r.Context(), sk); err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "skill.propose", sk.Name+"@"+sk.Version, nil)
	writeJSON(w, 201, sk)
}

func (s *Server) handleApproveSkill(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" {
		s.deny(w, r, id, "skill.approve")
		return
	}
	status := "approved"
	if r.URL.Query().Get("deprecate") == "1" {
		status = "deprecated"
	}
	name, ver := r.PathValue("name"), r.PathValue("version")
	if err := s.st.SetSkillStatus(r.Context(), name, ver, status, id.Session); err != nil {
		storeErr(w, err)
		return
	}
	s.audit(r.Context(), id, "skill."+status, name+"@"+ver, nil)
	w.WriteHeader(http.StatusNoContent)
}

// ---- audit/health ----

func (s *Server) handleListAudit(w http.ResponseWriter, r *http.Request, id Identity) {
	if id.Role != "coordinator" {
		s.deny(w, r, id, "audit.read")
		return
	}
	since := time.Time{}
	if v := r.URL.Query().Get("since"); v != "" {
		since, _ = time.Parse(time.RFC3339, v)
	}
	out, err := s.st.ListAudit(r.Context(), since, 1000)
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.st.Ping(r.Context()); err != nil {
		http.Error(w, "store down", http.StatusServiceUnavailable) // P9 fail-closed
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
