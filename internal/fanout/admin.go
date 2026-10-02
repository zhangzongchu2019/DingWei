package fanout

// 管理 API(设计 v2.3 §F/§K5):默认仅 loopback;bearer 管理身份=token_id 入审计;
// GET 掩码;PUT=If-Match CAS(412)+校验+原子持久+热载(同一管道);
// /admin/dlq/replay(水位门)、/admin/dlq/waive(admin 角色,T2)。

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

// AdminIdentity 管理身份(§F):Role∈{admin,operator};authoritative/force/secret/waive=admin。
type AdminIdentity struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

const masked = "****"

func (s *Server) adminIdentity(r *http.Request) (AdminIdentity, bool) {
	if !s.cfg.AdminAllowRemote {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil || (host != "127.0.0.1" && host != "::1") {
			return AdminIdentity{}, false // 监听面:默认仅 loopback(§F)
		}
	}
	tok := r.Header.Get("Authorization")
	if len(tok) > 7 && tok[:7] == "Bearer " {
		tok = tok[7:]
	}
	for t, id := range s.cfg.AdminTokens {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(t)) == 1 {
			return id, true
		}
	}
	return AdminIdentity{}, false
}

// maskedConfig GET 视图:secret/token 恒掩码,不回显(§F)。
func maskedConfig(fc *FileConfig) *FileConfig {
	if fc == nil {
		return &FileConfig{}
	}
	out := *fc
	out.Sinks = append([]SinkConfig(nil), fc.Sinks...)
	for i := range out.Sinks {
		if out.Sinks[i].Token != "" {
			out.Sinks[i].Token = masked
		}
	}
	out.Callers = map[string]CallerConfig{}
	for range fc.Callers { // caller token 本身是键=秘密,整表只报个数形状
		out.Callers[masked+strconv.Itoa(len(out.Callers))] = CallerConfig{Name: masked}
	}
	out.Accesses = append([]AccessConfig(nil), fc.Accesses...)
	for i := range out.Accesses {
		if out.Accesses[i].AppSecret != "" {
			out.Accesses[i].AppSecret = masked
		}
	}
	out.OutboundBots = map[string]BotCred{}
	for bot, c := range fc.OutboundBots {
		out.OutboundBots[bot] = BotCred{AppID: c.AppID, AppSecret: masked}
	}
	return &out
}

func (s *Server) handleAdminConfigGet(w http.ResponseWriter, r *http.Request) {
	id, ok := s.adminIdentity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	s.mu.RLock()
	fc := s.fileCfg
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if fc != nil {
		w.Header().Set("ETag", strconv.FormatInt(fc.Generation, 10))
	}
	_ = json.NewEncoder(w).Encode(maskedConfig(fc))
	s.log.Info("admin审计", "op", "config.get", "actor", id.ID)
}

func (s *Server) handleAdminConfigPut(w http.ResponseWriter, r *http.Request) {
	id, ok := s.adminIdentity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // §F size 1MB
	if err != nil || len(body) == 1<<20 {
		http.Error(w, "body too large", http.StatusRequestEntityTooLarge)
		return
	}
	fc := &FileConfig{}
	if err := json.Unmarshal(body, fc); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.mu.RLock()
	prev := s.fileCfg
	s.mu.RUnlock()
	// CAS:If-Match 必带且=当前代次(§E 412 杜绝 lost update)
	cur := int64(0)
	if prev != nil {
		cur = prev.Generation
	}
	ifMatch := r.Header.Get("If-Match")
	if g, err := strconv.ParseInt(ifMatch, 10, 64); err != nil || g != cur {
		http.Error(w, fmt.Sprintf("generation mismatch (current=%d)", cur), http.StatusPreconditionFailed)
		return
	}
	// force/authoritative 变更/secret 旋转=admin 角色(§F RBAC)
	if id.Role != "admin" {
		if fc.Force {
			http.Error(w, "force requires admin role", http.StatusForbidden)
			return
		}
		if prev != nil && fc.Validate(prev) != nil {
			// operator 触发了需 force 的变更(authoritative 删/降)=403 而非 400
			http.Error(w, "authoritative change requires admin", http.StatusForbidden)
			return
		}
	}
	if err := fc.Validate(prev); err != nil {
		http.Error(w, "invalid config: "+err.Error(), http.StatusBadRequest) // 烂配置不接管
		return
	}
	fc.Generation = cur + 1
	if s.cfg.ConfigPath != "" {
		if err := SaveFileConfigAtomic(s.cfg.ConfigPath, fc); err != nil {
			// 持久失败=不换内存态(§E)
			http.Error(w, "persist failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if err := s.ApplyFileConfig(r.Context(), fc); err != nil {
		// 应用部分失败(如接入 pre-arm 拒)不回滚已持久文件:表已换/接入拒=显式报错让操作者处置
		s.log.Error("admin审计", "op", "config.put", "actor", id.ID, "gen", fc.Generation, "apply_error", err)
		w.WriteHeader(http.StatusMultiStatus)
		_ = json.NewEncoder(w).Encode(map[string]any{"generation": fc.Generation, "apply_warning": err.Error()})
		return
	}
	s.log.Info("admin审计", "op", "config.put", "actor", id.ID, "gen", fc.Generation)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"generation": fc.Generation})
}

func (s *Server) handleAdminReload(w http.ResponseWriter, r *http.Request) {
	id, ok := s.adminIdentity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := s.ReloadFromFile(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.log.Info("admin审计", "op", "reload", "actor", id.ID)
	w.WriteHeader(http.StatusNoContent)
}

// ReloadFromFile SIGHUP/admin 同管道:load→validate→apply;失败=保旧(§E)。
func (s *Server) ReloadFromFile(ctx context.Context) error {
	if s.cfg.ConfigPath == "" {
		s.Reload() // v1 行为:仅白名单
		return nil
	}
	fc, err := LoadFileConfig(s.cfg.ConfigPath)
	if err != nil {
		s.log.Error("配置重载失败,保旧配置跑(告警)", "error", err)
		return err
	}
	s.mu.RLock()
	prev := s.fileCfg
	s.mu.RUnlock()
	if err := fc.Validate(prev); err != nil {
		s.log.Error("配置校验失败,保旧配置跑(告警)", "error", err)
		return err
	}
	s.Reload() // 白名单同步重载
	return s.ApplyFileConfig(ctx, fc)
}

func (s *Server) handleAdminDLQReplay(w http.ResponseWriter, r *http.Request) {
	id, ok := s.adminIdentity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.spool == nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	// 水位门(§B):spool 在途高于阈值=拒 replay
	queued, _ := s.spool.CountByStatus(r.Context(), "queued")
	limit := int64(s.cfg.ReplayWatermark)
	if limit <= 0 {
		limit = 1000
	}
	if queued > limit {
		http.Error(w, fmt.Sprintf("spool queued=%d over watermark %d", queued, limit), http.StatusConflict)
		return
	}
	ob, ib, err := s.spool.ReplayDLQ(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.log.Info("admin审计", "op", "dlq.replay", "actor", id.ID, "outbound", ob, "inbound", ib)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{"outbound_requeued": ob, "inbound_requeued": ib})
}

func (s *Server) handleAdminDLQWaive(w http.ResponseWriter, r *http.Request) {
	id, ok := s.adminIdentity(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if id.Role != "admin" { // T2:waive=admin 角色,403 负例
		http.Error(w, "waive requires admin role", http.StatusForbidden)
		return
	}
	if s.spool == nil {
		http.Error(w, "spool unavailable", http.StatusServiceUnavailable)
		return
	}
	var req struct {
		MsgKey string `json:"msg_key"`
		Sink   string `json:"sink"`
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil ||
		req.MsgKey == "" || req.Sink == "" || req.Reason == "" {
		http.Error(w, "msg_key/sink/reason 必填", http.StatusBadRequest)
		return
	}
	if err := s.spool.WaiveSink(r.Context(), req.MsgKey, req.Sink, id.ID, req.Reason); err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	s.log.Info("admin审计", "op", "dlq.waive", "actor", id.ID,
		"msg_key", req.MsgKey, "sink", req.Sink, "reason", req.Reason)
	w.WriteHeader(http.StatusNoContent)
}

// handleResolve §K4:名称解析透传(caller bearer;代理凭据调 Lark 通讯录)。
func (s *Server) handleResolve(w http.ResponseWriter, r *http.Request) {
	tok := r.Header.Get("Authorization")
	if len(tok) > 7 {
		tok = tok[7:]
	}
	if _, _, ok := s.callerByToken(tok); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req struct {
		V          int    `json:"v"`
		BotChannel string `json:"bot_channel"`
		Name       string `json:"name"`
		ChatType   string `json:"chat_type"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	sender, ok := s.senderFor(req.BotChannel)
	if !ok {
		http.Error(w, "unknown bot_channel", http.StatusNotFound)
		return
	}
	gw, ok := sender.(feishu.Gateway)
	if !ok {
		http.Error(w, "resolve unsupported by sender", http.StatusNotImplemented)
		return
	}
	id, err := gw.ResolveNameToID(r.Context(), req.BotChannel, req.Name, req.ChatType)
	if err != nil || id == "" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
}

// ReplayDLQ spool 侧:出站 dlq 归队+入站 sink dlq 归队(全局态重算)。
func (sp *Spool) ReplayDLQ(ctx context.Context) (outbound, inbound int64, err error) {
	res, err := sp.db.Exec(ctx, ftx(),
		`UPDATE outbound SET status='queued', attempts=0, next_retry=0 WHERE status='dlq'`)
	if err != nil {
		return 0, 0, err
	}
	outbound, _ = res.RowsAffected()
	rows, err := sp.db.Query(ctx, ftx(),
		`SELECT DISTINCT msg_key FROM inbound_sink WHERE status='dlq'`)
	if err != nil {
		return outbound, 0, err
	}
	var keys []string
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			keys = append(keys, k)
		}
	}
	rows.Close()
	res, err = sp.db.Exec(ctx, ftx(),
		`UPDATE inbound_sink SET status='queued', attempts=0, next_retry=0 WHERE status='dlq'`)
	if err != nil {
		return outbound, 0, err
	}
	inbound, _ = res.RowsAffected()
	for _, k := range keys {
		if err := sp.recomputeGlobal(ctx, k); err != nil {
			return outbound, inbound, err
		}
	}
	return outbound, inbound, nil
}

// SelfCheck pre-arm③:真写读删一轮(证 fsync 路活)。
// 🔴 本方法【不建表】:selfcheck 由 pgschema.sql 建(OpenSpool 一次建齐五表),
// 故这里只做写读删。原先在此懒建有两处坏处:① 建表 DDL 走在 ftx() 下,把「无租户语义的 DDL」
// 记成租户债;② 五表分散三处建,账本登记哪一张由代码顺序决定,漏一张没有判据能发现。
func (sp *Spool) SelfCheck(ctx context.Context) error {
	if _, err := sp.db.Exec(ctx, ftx(),
		`INSERT INTO selfcheck(k,v) VALUES('probe','ok')
ON CONFLICT (k) DO UPDATE SET v=EXCLUDED.v`); err != nil {
		return err
	}
	var v string
	row, qErr := sp.db.QueryRow(ctx, ftx(), `SELECT v FROM selfcheck WHERE k='probe'`)
	if qErr != nil {
		return fmt.Errorf("selfcheck read: %v", qErr)
	}
	if err := row.Scan(&v); err != nil || v != "ok" {
		return fmt.Errorf("selfcheck read: %v v=%q", err, v)
	}
	_, err := sp.db.Exec(ctx, ftx(), `DELETE FROM selfcheck WHERE k='probe'`)
	return err
}
