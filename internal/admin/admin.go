// Package admin 是 M9 后台管理 Web（必须密码登录，规范 §4.6 / §M9）。
//
// 脚手架版：实现 健康检查 + 登录(密码哈希校验) + 简单会话 + 状态页骨架。
// 路由字头 CRUD/冲突可视化、API key 管理、最近100条、数据清理 等在此基础上补。
package admin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/redact"
	"github.com/zhangzongchu2019/dingwei/internal/schedule"
	"github.com/zhangzongchu2019/dingwei/internal/scheduler"
	"github.com/zhangzongchu2019/dingwei/internal/secretbox"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type SeenPersonCollector interface {
	CollectSeenPersons(ctx context.Context) ([]model.SeenPerson, error)
}

// Server M9 后台。
type Server struct {
	Repo      store.Repository
	Outbound  bus.Queue
	Prefix    *m8.Hub
	Collector SeenPersonCollector
	Scheduler *scheduler.Service
	SecretKey string
	mu        sync.Mutex
	sessions  map[string]string // token -> username
}

func New(repo store.Repository) *Server {
	return &Server{Repo: repo, Prefix: m8.New(repo), sessions: map[string]string{}}
}

// Mount 注册路由到 mux。
func (s *Server) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.health)
	mux.HandleFunc("GET /portal", s.requireAuth(s.portal))
	mux.HandleFunc("GET /admin/login", s.loginForm)
	mux.HandleFunc("POST /admin/login", s.loginSubmit)
	mux.HandleFunc("GET /admin", s.requireAuth(s.status))
	mux.HandleFunc("GET /admin/messages", s.requireAuth(s.messages))
	mux.HandleFunc("GET /admin/members", s.requireAuth(s.members))
	mux.HandleFunc("POST /admin/members", s.requireAuth(s.upsertMember))
	mux.HandleFunc("GET /admin/projects", s.requireAuth(s.projects))
	mux.HandleFunc("POST /admin/projects", s.requireAuth(s.upsertProject))
	mux.HandleFunc("GET /admin/projects/{id}/members", s.requireAuth(s.projectMembers))
	mux.HandleFunc("POST /admin/projects/{id}/members", s.requireAuth(s.manageProjectMembers))
	mux.HandleFunc("GET /admin/projects/{id}/aggregate-sources", s.requireAuth(s.aggregateSources))
	mux.HandleFunc("POST /admin/projects/{id}/aggregate-sources", s.requireAuth(s.manageAggregateSources))
	mux.HandleFunc("GET /admin/projects/{id}/team-schedule", s.requireAuth(s.teamSchedule))
	mux.HandleFunc("POST /admin/projects/{id}/team-schedule", s.requireAuth(s.postTeamSchedule))
	mux.HandleFunc("GET /admin/projects/{id}/history", s.requireAuth(s.teamScheduleHistory))
	mux.HandleFunc("POST /admin/projects/{id}/history", s.requireAuth(s.rollbackTeamSchedule))
	mux.HandleFunc("GET /admin/projects/{id}/members/{owner}/schedule", s.requireAuth(s.personalSchedule))
	mux.HandleFunc("POST /admin/projects/{id}/members/{owner}/schedule", s.requireAuth(s.postPersonalSchedule))
	mux.HandleFunc("GET /admin/projects/{id}/members/{owner}/schedule/history", s.requireAuth(s.personalScheduleHistory))
	mux.HandleFunc("POST /admin/projects/{id}/members/{owner}/schedule/history", s.requireAuth(s.rollbackPersonalSchedule))
	mux.HandleFunc("GET /admin/bot-channels", s.requireAuth(s.botChannels))
	mux.HandleFunc("POST /admin/bot-channels", s.requireAuth(s.upsertBotChannel))
	mux.HandleFunc("GET /admin/services", s.requireAuth(s.services))
	mux.HandleFunc("POST /admin/services", s.requireAuth(s.upsertService))
	mux.HandleFunc("GET /admin/api-keys", s.requireAuth(s.apiKeys))
	mux.HandleFunc("POST /admin/api-keys", s.requireAuth(s.manageAPIKey))
	mux.HandleFunc("GET /admin/routes", s.requireAuth(s.routes))
	mux.HandleFunc("POST /admin/routes", s.requireAuth(s.addRoute))
	mux.HandleFunc("GET /admin/sessions", s.requireAuth(s.sessionsPage))
	mux.HandleFunc("POST /admin/sessions", s.requireAuth(s.manageSession))
	mux.HandleFunc("GET /admin/config", s.requireAuth(s.configs))
	mux.HandleFunc("POST /admin/config", s.requireAuth(s.upsertConfig))
	mux.HandleFunc("POST /admin/delegate/schedule", s.requireAuth(s.delegateSchedule))
	mux.HandleFunc("POST /admin/aggregate/draft-now", s.requireAuth(s.aggregateDraftNow))
	mux.HandleFunc("POST /admin/aggregate/publish-now", s.requireAuth(s.aggregatePublishNow))
	mux.HandleFunc("POST /admin/cleanup", s.requireAuth(s.cleanup))
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) loginForm(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset=utf-8><title>WorkPulse 后台登录</title>
<form method=post action=/admin/login>
<h3>WorkPulse 后台登录</h3>
<p>用户名 <input name=username></p>
<p>密码 <input name=password type=password></p>
<button>登录</button></form>`))
}

func (s *Server) loginSubmit(w http.ResponseWriter, r *http.Request) {
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	u, err := s.Repo.GetAdmin(r.Context(), username)
	if err != nil || u == nil || !u.Active || !VerifyPassword(u.PasswordHash, password) {
		// TODO §M9：登录失败限速防暴破（admin_login_attempt）
		http.Error(w, "用户名或密码错误", http.StatusUnauthorized)
		return
	}
	tok := token()
	s.mu.Lock()
	s.sessions[tok] = username
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: "wp_admin", Value: tok, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	_ = s.Repo.WriteAudit(r.Context(), username, "admin_login", "")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	stats, err := s.Repo.MessageStats(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>WorkPulse</title>
<h3>WorkPulse 后台</h3><p>服务运行中。</p>
<p>消息总数: %d | queued: %d | processing: %d | done: %d | dead: %d</p>
<ul>
<li><a href="/portal">团队门户</a></li><li><a href="/admin/messages">最近 100 条消息</a></li>
<li><a href="/admin/members">成员 / 租户候选</a></li><li><a href="/admin/bot-channels">机器人管道</a></li>
<li><a href="/admin/projects">项目组 / 排期文档</a></li>
<li><a href="/admin/services">租户 / 成员空间</a></li><li><a href="/admin/api-keys">凭证 API key</a></li>
<li><a href="/admin/sessions">会话端点 / 通配 / 镜像</a></li>
<li><a href="/admin/config">运行配置</a></li>
</ul>`, stats.Total, stats.Queued, stats.Processing, stats.Done, stats.Dead)
}

func (s *Server) portal(w http.ResponseWriter, r *http.Request) {
	members, err := s.Repo.ListMembers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	risks, err := s.Repo.ListOpenRisks(r.Context(), "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset=utf-8><title>WorkPulse 门户</title><h3>WorkPulse 团队门户</h3>`))
	_, _ = w.Write([]byte(`<h4>团队排期</h4><table border=1 cellpadding=4><tr><th>成员</th><th>日期</th><th>任务</th><th>状态</th></tr>`))
	for _, m := range members {
		schedules, err := s.Repo.ListSchedules(r.Context(), m.OwnerKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, sc := range schedules {
			_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s~%s</td><td>%s</td><td>%s</td></tr>`,
				esc(displayName(m)), esc(sc.StartDate), esc(sc.EndDate), esc(sc.Task), esc(sc.Status))
		}
	}
	_, _ = w.Write([]byte(`</table><h4>进度看板</h4><table border=1 cellpadding=4><tr><th>成员</th><th>任务</th><th>最新进度</th><th>百分比</th></tr>`))
	for _, m := range members {
		progress, err := s.Repo.LatestProgress(r.Context(), m.OwnerKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for _, p := range progress {
			_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%d%%</td></tr>`,
				esc(displayName(m)), esc(p.TaskKey), esc(p.Note), p.Percent)
		}
	}
	_, _ = w.Write([]byte(`</table><h4>风险列表</h4><table border=1 cellpadding=4><tr><th>负责人</th><th>风险</th><th>状态</th></tr>`))
	for _, risk := range risks {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td></tr>`, esc(risk.OwnerKey), esc(risk.Content), esc(risk.Status))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	filter := model.MessageFilter{
		BotChannelID: strings.TrimSpace(r.URL.Query().Get("channel")),
		ChatEntityID: strings.TrimSpace(r.URL.Query().Get("entity")),
		Limit:        100,
	}
	items, err := s.Repo.RecentMessages(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(`<!doctype html><meta charset=utf-8><title>最近消息</title><h3>最近 100 条消息</h3>
<form method=get><p>管道 <input name=channel> 会话 <input name=entity> <button>筛选</button></p></form>
<table border=1 cellpadding=4><tr><th>时间</th><th>方向</th><th>管道</th><th>会话</th><th>类型</th><th>状态</th><th>尝试</th><th>内容</th></tr>`))
	for _, m := range items {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td><pre>%s</pre></td></tr>`,
			esc(m.CreatedAt.Format(time.RFC3339)), esc(string(m.Direction)), esc(m.BotChannelID), esc(m.ChatEntityID), esc(string(m.ChatType)), esc(m.Status), m.Attempts, esc(redact.Content(m.Content)))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) members(w http.ResponseWriter, r *http.Request) {
	items, err := s.Repo.ListMembers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	seen, err := s.Repo.ListSeenPersons(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeHTMLHeader(w, "成员配置")
	_, _ = w.Write([]byte(`<h3>成员配置</h3>
<p>数据来源：候选池来自飞书入站发送者和“刷新采集”拉取的机器人所在群成员；录为成员后成为排期、权限、佐证对账的 owner。</p>
<form method=post action=/admin/members><input type=hidden name=action value=refresh_seen><button>刷新采集</button></form>
<h4>候选池</h4>
<table border=1 cellpadding=4><tr><th>open_id</th><th>名称</th><th>管道</th><th>来源</th><th>最后看到</th><th>状态/操作</th></tr>`))
	if len(seen) == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=6>暂无候选人。先让用户给机器人发消息，或点击“刷新采集”从机器人所在群拉取成员。</td></tr>`))
	}
	for _, p := range seen {
		name := firstNonEmpty(p.Name, p.OpenID)
		if p.IsMember {
			_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>已是成员</td></tr>`,
				esc(p.OpenID), esc(name), esc(p.BotChannelID), esc(p.Source), esc(p.LastSeenAt.Format(time.RFC3339)))
			continue
		}
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td><form method=post action=/admin/members><input type=hidden name=action value=promote_seen><input type=hidden name=feishu_open_id value="%s"><input type=hidden name=display_name value="%s">owner_key <input name=owner_key value="%s"> 角色 <select name=role><option>member</option><option>collaborator</option><option>manager</option><option>system</option></select><button>录为成员</button></form></td></tr>`,
			esc(p.OpenID), esc(name), esc(p.BotChannelID), esc(p.Source), esc(p.LastSeenAt.Format(time.RFC3339)), esc(p.OpenID), esc(name), esc(defaultOwnerKey(name, p.OpenID)))
	}
	_, _ = w.Write([]byte(`</table>
<details><summary>手动添加兜底</summary>
<form method=post action=/admin/members>
<p>owner_key <input name=owner_key> 名称 <input name=display_name> 飞书open_id <input name=feishu_open_id>
角色 <select name=role><option>member</option><option>collaborator</option><option>manager</option><option>system</option></select>
<label><input type=checkbox name=evidence_optout>关闭佐证</label>
<label><input type=checkbox name=disabled>禁用</label> <button>保存成员</button></p>
</form>
</details>
<h4>已录入成员</h4>
<table border=1 cellpadding=4><tr><th>owner_key</th><th>名称</th><th>open_id</th><th>角色</th><th>佐证关闭</th><th>启用</th><th>操作</th></tr>`))
	for _, m := range items {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%t</td><td>%t</td><td><form method=post action=/admin/members><input type=hidden name=action value=delete><input type=hidden name=owner_key value="%s"><button>删除/禁用</button></form></td></tr>`,
			esc(m.OwnerKey), esc(m.DisplayName), esc(m.FeishuOpenID), esc(string(m.Role)), m.EvidenceOptOut, m.Active, esc(m.OwnerKey))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) upsertMember(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("action") == "refresh_seen" {
		if s.Collector == nil {
			http.Error(w, "collector not configured", http.StatusNotImplemented)
			return
		}
		persons, err := s.Collector.CollectSeenPersons(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		for _, p := range persons {
			if err := s.Repo.UpsertSeenPerson(r.Context(), p); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_seen_person_refresh", fmt.Sprintf("%d", len(persons)))
		writeOK(w, fmt.Sprintf("seen persons refreshed: %d", len(persons)))
		return
	}
	if r.FormValue("action") == "delete" {
		owner := strings.TrimSpace(r.FormValue("owner_key"))
		m, err := s.Repo.GetMemberByOwnerKey(r.Context(), owner)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if m == nil {
			m = &model.Member{OwnerKey: owner, Role: model.RoleMember}
		}
		m.Active = false
		if err := s.Repo.UpsertMember(r.Context(), *m); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_member_delete", owner)
		writeOK(w, "member disabled")
		return
	}
	m := model.Member{
		OwnerKey:       strings.TrimSpace(r.FormValue("owner_key")),
		DisplayName:    strings.TrimSpace(r.FormValue("display_name")),
		FeishuOpenID:   strings.TrimSpace(r.FormValue("feishu_open_id")),
		Role:           model.Role(firstNonEmpty(r.FormValue("role"), string(model.RoleMember))),
		EvidenceOptOut: truthy(r.FormValue("evidence_optout")),
		Active:         !truthy(r.FormValue("disabled")),
	}
	if m.OwnerKey == "" {
		http.Error(w, "owner_key required", http.StatusBadRequest)
		return
	}
	if err := s.Repo.UpsertMember(r.Context(), m); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_member_upsert", m.OwnerKey)
	writeOK(w, "member saved")
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.Repo.ListProjects(r.Context(), false)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	groups, err := s.Repo.ListChatEntities(r.Context(), model.ChatGroup)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	members, err := s.Repo.ListMembers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeHTMLHeader(w, "项目组管理")
	_, _ = w.Write([]byte(`<h3>项目组管理</h3>
<p>项目组定义通知群、佐证源和排期文档归属。通知群可从已知群会话下拉选择，也可手工填写 chat_id。</p>
<form method=post action=/admin/projects>
<p>ID <input name=id placeholder="proj:team-a"> 名称 <input name=name>
parent <select name=parent_id><option value="proj:default">Default Project (proj:default)</option>`))
	_, _ = w.Write([]byte(projectOptions(projects, "proj:default", "")))
	_, _ = w.Write([]byte(`</select>
负责人 <select name=owner_key><option value="">必选</option>`))
	_, _ = w.Write([]byte(memberOptions(members, "")))
	_, _ = w.Write([]byte(`</select>
产品经理 <select name=product_manager_key><option value="">不设置</option>`))
	_, _ = w.Write([]byte(memberOptions(members, "")))
	_, _ = w.Write([]byte(`</select>
通知群 <select name=notify_chat_id><option value="">不设置</option>`))
	_, _ = w.Write([]byte(groupOptions(groups, "")))
	_, _ = w.Write([]byte(`</select> 或 <input name=notify_chat_id_manual placeholder="oc_xxx">
notify_bot <input name=notify_bot_id value=unifiedrobot>
transcript_dirs <input name=transcript_dirs>
evidence_cron <input name=evidence_cron size=10> tz <input name=evidence_tz size=10>
<label><input type=checkbox name=disabled>禁用</label> <button>保存项目</button></p>
</form>
<table border=1 cellpadding=4><tr><th>ID</th><th>名称</th><th>parent</th><th>负责人</th><th>产品经理</th><th>通知群</th><th>生效通知群</th><th>通知bot</th><th>成员数</th><th>active</th><th>编辑/操作</th></tr>`))
	counts := s.projectMemberCounts(r.Context(), projects)
	resolver := scheduler.New(scheduler.Config{}, nil, nil, nil)
	resolver.Repo = s.Repo
	for _, p := range projects {
		target, err := resolver.ResolveNotifyTarget(r.Context(), p.ID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		effective := target.ChatID
		if target.Source != "" {
			effective += " ← " + target.Source
		}
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%t</td><td>
<form method=post action=/admin/projects>
<input type=hidden name=id value="%s"> 名称 <input name=name value="%s">
parent <select name=parent_id><option value="">无</option>%s</select>
负责人 <select name=owner_key><option value="">必选</option>%s</select>
产品经理 <select name=product_manager_key><option value="">不设置</option>%s</select>
通知群 <select name=notify_chat_id><option value="">不设置</option>%s</select> 或 <input name=notify_chat_id_manual value="">
notify_bot <input name=notify_bot_id value="%s">
transcript_dirs <input name=transcript_dirs value="%s">
evidence_cron <input name=evidence_cron value="%s" size=10> tz <input name=evidence_tz value="%s" size=10>
<label><input type=checkbox name=disabled %s>禁用</label> <button>保存</button>
<button name=action value=disable>删除/禁用</button>
</form>
<a href="/admin/projects/%s/members">成员</a> |
<a href="/admin/projects/%s/aggregate-sources">聚合来源</a> |
<a href="/admin/projects/%s/team-schedule">团队排期</a> |
<a href="/admin/projects/%s/history">历史</a>
</td></tr>`,
			esc(p.ID), esc(p.Name), esc(p.ParentID), esc(projectMemberLabel(members, p.OwnerKey)), esc(projectMemberLabel(members, p.ProductManagerKey)), esc(p.NotifyChatID), esc(effective), esc(p.NotifyBotID), counts[p.ID], p.Active,
			esc(p.ID), esc(p.Name), projectOptions(projects, p.ParentID, p.ID), memberOptions(members, p.OwnerKey), memberOptions(members, p.ProductManagerKey), groupOptions(groups, p.NotifyChatID), esc(p.NotifyBotID), esc(p.TranscriptDirs), esc(p.EvidenceCron), esc(p.EvidenceTZ), checked(!p.Active),
			esc(p.ID), esc(p.ID), esc(p.ID), esc(p.ID))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) upsertProject(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		http.Error(w, "project id required", http.StatusBadRequest)
		return
	}
	if r.FormValue("action") == "disable" {
		p, err := s.Repo.GetProject(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if p == nil {
			p = &model.Project{ID: id, Name: id}
		}
		p.Active = false
		if err := s.Repo.UpsertProject(r.Context(), *p); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_project_disable", id)
		writeOK(w, "project disabled")
		return
	}
	notifyChat := strings.TrimSpace(r.FormValue("notify_chat_id_manual"))
	if notifyChat == "" {
		notifyChat = strings.TrimSpace(r.FormValue("notify_chat_id"))
	}
	p := model.Project{
		ID:                id,
		Name:              strings.TrimSpace(r.FormValue("name")),
		ParentID:          strings.TrimSpace(r.FormValue("parent_id")),
		OwnerKey:          strings.TrimSpace(r.FormValue("owner_key")),
		ProductManagerKey: strings.TrimSpace(r.FormValue("product_manager_key")),
		NotifyChatID:      notifyChat,
		NotifyBotID:       firstNonEmpty(strings.TrimSpace(r.FormValue("notify_bot_id")), "unifiedrobot"),
		TranscriptDirs:    strings.TrimSpace(r.FormValue("transcript_dirs")),
		EvidenceCron:      strings.TrimSpace(r.FormValue("evidence_cron")),
		EvidenceTZ:        strings.TrimSpace(r.FormValue("evidence_tz")),
		Active:            !truthy(r.FormValue("disabled")),
	}
	if p.Name == "" {
		http.Error(w, "project name required", http.StatusBadRequest)
		return
	}
	if p.OwnerKey == "" {
		http.Error(w, "project owner_key required", http.StatusBadRequest)
		return
	}
	if p.ParentID == "" && p.ID != "proj:default" {
		p.ParentID = "proj:default"
	}
	if p.ParentID == p.ID {
		http.Error(w, "project parent cannot be self", http.StatusBadRequest)
		return
	}
	if err := s.Repo.UpsertProject(r.Context(), p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_project_upsert", p.ID)
	writeOK(w, "project saved")
}

func (s *Server) projectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("id"))
	project, err := s.Repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	members, err := s.Repo.ListProjectMembers(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	allMembers, err := s.Repo.ListMembers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	seen, err := s.Repo.ListSeenPersonsByGroup(r.Context(), project.NotifyChatID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	assigned := map[string]bool{}
	for _, m := range members {
		assigned[m.OwnerKey] = true
	}
	writeHTMLHeader(w, "项目成员分配")
	_, _ = fmt.Fprintf(w, `<h3>项目成员分配：%s</h3><p><a href="/admin/projects">返回项目</a></p>
<h4>已加入</h4><table border=1 cellpadding=4><tr><th>owner_key</th><th>名称</th><th>open_id</th><th>操作</th></tr>`, esc(project.Name))
	for _, m := range members {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td><form method=post action=/admin/projects/%s/members><input type=hidden name=action value=remove><input type=hidden name=owner_key value="%s"><button>移除</button></form> <a href="/admin/projects/%s/members/%s/schedule">个人日程</a></td></tr>`,
			esc(m.OwnerKey), esc(m.DisplayName), esc(m.FeishuOpenID), esc(projectID), esc(m.OwnerKey), esc(projectID), esc(m.OwnerKey))
	}
	_, _ = w.Write([]byte(`</table><h4>候选池：本项目通知群里见过的人</h4><table border=1 cellpadding=4><tr><th>open_id</th><th>群</th><th>最后看到</th><th>加入</th></tr>`))
	for _, p := range seen {
		owner := ownerForOpenID(allMembers, p.OpenID)
		if owner == "" {
			_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>尚未录为成员</td></tr>`, esc(p.OpenID), esc(p.GroupName), esc(p.LastSeenAt.Format(time.RFC3339)))
			continue
		}
		if assigned[owner] {
			continue
		}
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td><form method=post action=/admin/projects/%s/members><input type=hidden name=action value=add><input type=hidden name=owner_key value="%s"><button>加入</button></form></td></tr>`,
			esc(p.OpenID), esc(p.GroupName), esc(p.LastSeenAt.Format(time.RFC3339)), esc(projectID), esc(owner))
	}
	_, _ = w.Write([]byte(`</table><h4>所有成员</h4><form method=post><input type=hidden name=action value=add>owner_key <select name=owner_key>`))
	for _, m := range allMembers {
		if m.Active && !assigned[m.OwnerKey] {
			_, _ = fmt.Fprintf(w, `<option value="%s">%s</option>`, esc(m.OwnerKey), esc(firstNonEmpty(m.DisplayName, m.OwnerKey)))
		}
	}
	_, _ = w.Write([]byte(`</select><button>加入</button></form>`))
}

func (s *Server) manageProjectMembers(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("id"))
	owner := strings.TrimSpace(r.FormValue("owner_key"))
	if projectID == "" || owner == "" {
		http.Error(w, "project/owner required", http.StatusBadRequest)
		return
	}
	if r.FormValue("action") == "remove" {
		if err := s.Repo.UnassignProjectMember(r.Context(), projectID, owner); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_project_member_remove", projectID+":"+owner)
		writeOK(w, "project member removed")
		return
	}
	if err := s.Repo.AssignProjectMember(r.Context(), projectID, owner); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_project_member_add", projectID+":"+owner)
	writeOK(w, "project member added")
}

func (s *Server) aggregateSources(w http.ResponseWriter, r *http.Request) {
	projectID := strings.TrimSpace(r.PathValue("id"))
	project, err := s.Repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	projects, err := s.Repo.ListProjects(r.Context(), true)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	sources, err := s.Repo.ListProjectAggregateSources(r.Context(), projectID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	selected := map[string]bool{}
	for _, source := range sources {
		selected[source.ID] = true
	}
	writeHTMLHeader(w, "聚合来源")
	_, _ = fmt.Fprintf(w, `<h3>聚合来源：%s</h3><p><a href="/admin/projects">返回项目</a></p>
<p>聚合通知会按本项目的 <code>evidence_cron</code> 自动发送；来源项目可跨部门勾选，也可一键选择同 parent 下子项目。</p>
<form method=post action=/admin/projects/%s/aggregate-sources>
<table border=1 cellpadding=4><tr><th>选择</th><th>ID</th><th>名称</th><th>parent</th><th>通知群</th></tr>`, esc(project.Name), esc(projectID))
	for _, p := range projects {
		if p.ID == projectID {
			continue
		}
		_, _ = fmt.Fprintf(w, `<tr><td><input type=checkbox name=source_project_id value="%s" %s></td><td>%s</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			esc(p.ID), checked(selected[p.ID]), esc(p.ID), esc(p.Name), esc(p.ParentID), esc(p.NotifyChatID))
	}
	_, _ = w.Write([]byte(`</table><button name=action value=save>保存来源</button> <button name=action value=select_children>选择同parent子项目</button></form>`))
}

func (s *Server) manageAggregateSources(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	projectID := strings.TrimSpace(r.PathValue("id"))
	project, err := s.Repo.GetProject(r.Context(), projectID)
	if err != nil || project == nil {
		http.Error(w, "project not found", http.StatusNotFound)
		return
	}
	sourceIDs := r.Form["source_project_id"]
	if r.FormValue("action") == "select_children" {
		projects, err := s.Repo.ListProjects(r.Context(), true)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		sourceIDs = sourceIDs[:0]
		for _, p := range projects {
			if p.ID != projectID && p.ParentID == project.ParentID {
				sourceIDs = append(sourceIDs, p.ID)
			}
		}
	}
	if err := s.Repo.SetProjectAggregateSources(r.Context(), projectID, sourceIDs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_project_aggregate_sources", fmt.Sprintf("%s:%d", projectID, len(sourceIDs)))
	writeOK(w, "aggregate sources saved")
}

func (s *Server) teamSchedule(w http.ResponseWriter, r *http.Request) {
	s.renderScheduleEditor(w, r, r.PathValue("id"), "team", "", "团队排期")
}

func (s *Server) personalSchedule(w http.ResponseWriter, r *http.Request) {
	s.renderScheduleEditor(w, r, r.PathValue("id"), "personal", r.PathValue("owner"), "个人日程")
}

func (s *Server) renderScheduleEditor(w http.ResponseWriter, r *http.Request, projectID, kind, owner, title string) {
	doc, err := s.Repo.LatestScheduleDoc(r.Context(), projectID, kind, owner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	content := ""
	version := 0
	if doc != nil {
		content = doc.Content
		version = doc.Version
	}
	action := fmt.Sprintf("/admin/projects/%s/team-schedule", projectID)
	history := fmt.Sprintf("/admin/projects/%s/history", projectID)
	if kind == "personal" {
		action = fmt.Sprintf("/admin/projects/%s/members/%s/schedule", projectID, owner)
		history = fmt.Sprintf("/admin/projects/%s/members/%s/schedule/history", projectID, owner)
	}
	writeHTMLHeader(w, title)
	_, _ = fmt.Fprintf(w, `<h3>%s：%s %s v%d</h3><p><a href="/admin/projects">项目列表</a> | <a href="%s">版本历史</a></p>
<form method=post action="%s">
<textarea name=content rows=24 cols=110>%s</textarea>
<p><label><input type=checkbox name=confirm value=1>确认写入新版本</label> <button>预览/提交</button></p>
</form>`, esc(title), esc(projectID), esc(owner), version, esc(history), esc(action), esc(content))
}

func (s *Server) postTeamSchedule(w http.ResponseWriter, r *http.Request) {
	s.postScheduleDoc(w, r, r.PathValue("id"), "team", "")
}

func (s *Server) postPersonalSchedule(w http.ResponseWriter, r *http.Request) {
	s.postScheduleDoc(w, r, r.PathValue("id"), "personal", r.PathValue("owner"))
}

func (s *Server) postScheduleDoc(w http.ResponseWriter, r *http.Request, projectID, kind, owner string) {
	content := strings.TrimSpace(r.FormValue("content"))
	prev := ""
	if doc, err := s.Repo.LatestScheduleDoc(r.Context(), projectID, kind, owner); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	} else if doc != nil {
		prev = doc.Content
	}
	if err := scheduler.ValidateTeamDoc(content, prev); err != nil {
		http.Error(w, "排期文档校验失败："+err.Error(), http.StatusBadRequest)
		return
	}
	if !truthy(r.FormValue("confirm")) {
		writeHTMLHeader(w, "排期预览")
		_, _ = fmt.Fprintf(w, `<h3>预览 diff：%s %s %s</h3><form method=post><input type=hidden name=confirm value=1><textarea name=content rows=24 cols=110>%s</textarea><pre>%s</pre><button>确认写入新版本</button></form>`,
			esc(projectID), esc(kind), esc(owner), esc(content), esc(simpleDiff(prev, content)))
		return
	}
	doc, err := s.Repo.AppendScheduleDoc(r.Context(), model.ScheduleDoc{
		ProjectID: projectID,
		Kind:      kind,
		OwnerKey:  owner,
		Content:   content,
		Source:    "paste",
		CreatedBy: adminUser(r),
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_schedule_doc_append", fmt.Sprintf("%s:%s:%s:v%d", projectID, kind, owner, doc.Version))
	writeOK(w, fmt.Sprintf("schedule doc appended v%d", doc.Version))
}

func (s *Server) teamScheduleHistory(w http.ResponseWriter, r *http.Request) {
	s.renderScheduleHistory(w, r, r.PathValue("id"), "team", "")
}

func (s *Server) personalScheduleHistory(w http.ResponseWriter, r *http.Request) {
	s.renderScheduleHistory(w, r, r.PathValue("id"), "personal", r.PathValue("owner"))
}

func (s *Server) renderScheduleHistory(w http.ResponseWriter, r *http.Request, projectID, kind, owner string) {
	versions, err := s.Repo.ListScheduleDocVersions(r.Context(), projectID, kind, owner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	action := fmt.Sprintf("/admin/projects/%s/history", projectID)
	if kind == "personal" {
		action = fmt.Sprintf("/admin/projects/%s/members/%s/schedule/history", projectID, owner)
	}
	writeHTMLHeader(w, "排期版本历史")
	_, _ = fmt.Fprintf(w, `<h3>排期版本历史：%s %s %s</h3><table border=1 cellpadding=4><tr><th>版本</th><th>来源</th><th>创建人</th><th>时间</th><th>内容</th><th>操作</th></tr>`, esc(projectID), esc(kind), esc(owner))
	for _, doc := range versions {
		_, _ = fmt.Fprintf(w, `<tr><td>v%d</td><td>%s</td><td>%s</td><td>%s</td><td><pre>%s</pre></td><td><form method=post action="%s"><input type=hidden name=version value="%d"><button>回滚为新版</button></form></td></tr>`,
			doc.Version, esc(doc.Source), esc(doc.CreatedBy), esc(doc.CreatedAt.Format(time.RFC3339)), esc(doc.Content), esc(action), doc.Version)
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) rollbackTeamSchedule(w http.ResponseWriter, r *http.Request) {
	s.rollbackScheduleDoc(w, r, r.PathValue("id"), "team", "")
}

func (s *Server) rollbackPersonalSchedule(w http.ResponseWriter, r *http.Request) {
	s.rollbackScheduleDoc(w, r, r.PathValue("id"), "personal", r.PathValue("owner"))
}

func (s *Server) rollbackScheduleDoc(w http.ResponseWriter, r *http.Request, projectID, kind, owner string) {
	version, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("version")))
	if version <= 0 {
		http.Error(w, "version required", http.StatusBadRequest)
		return
	}
	versions, err := s.Repo.ListScheduleDocVersions(r.Context(), projectID, kind, owner)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, old := range versions {
		if old.Version != version {
			continue
		}
		doc, err := s.Repo.AppendScheduleDoc(r.Context(), model.ScheduleDoc{
			ProjectID: projectID,
			Kind:      kind,
			OwnerKey:  owner,
			Content:   old.Content,
			Source:    "rollback",
			CreatedBy: adminUser(r),
		})
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_schedule_doc_rollback", fmt.Sprintf("%s:%s:%s:v%d->v%d", projectID, kind, owner, old.Version, doc.Version))
		writeOK(w, fmt.Sprintf("rolled back as v%d", doc.Version))
		return
	}
	http.Error(w, "version not found", http.StatusNotFound)
}

func (s *Server) botChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.Repo.ListBotChannels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeHTMLHeader(w, "机器人管道")
	_, _ = w.Write([]byte(`<h3>机器人管道</h3>
<p>数据来源：后台录入的飞书自建应用。bot_name 就是寻址地址第三段，例如 <code>ou_xxx#key_id#UnifiedRobot</code> 会选择 UnifiedRobot 管道发送；接收由各自长连接或 webhook 完成。</p>
<form method=post action=/admin/bot-channels>
<p>ID <input name=id> 名称 <input name=name> app_id <input name=app_id> app_secret <input name=app_secret type=password placeholder="留空不改"> 用途 <input name=purpose value=general>
verification_token <input name=verification_token type=password placeholder="可选，留空不改"> encrypt_key <input name=encrypt_key type=password placeholder="可选，留空不改">
<label><input type=checkbox name=cannot_send>禁发</label><label><input type=checkbox name=cannot_receive>禁收</label>
<label><input type=checkbox name=disabled>禁用</label> <button>保存管道</button></p>
</form>
<table border=1 cellpadding=4><tr><th>ID</th><th>bot_name</th><th>app_id</th><th>secret</th><th>webhook token</th><th>encrypt_key</th><th>用途</th><th>收/发</th><th>启用</th><th>WS状态</th><th>操作</th></tr>`))
	if len(items) == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=11>暂无机器人管道。请在此录入 app_id/app_secret；secret/token/encrypt_key 只写入不回显。</td></tr>`))
	}
	for _, c := range items {
		secretStatus := "未设置"
		if c.AppSecretSet {
			secretStatus = "已设置"
		}
		tokenStatus := "未设置"
		if c.VerificationTokenSet {
			tokenStatus = "已设置"
		}
		encryptStatus := "未设置"
		if c.EncryptKeySet {
			encryptStatus = "已设置"
		}
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%t/%t</td><td>%t</td><td>%s</td><td>
<form method=post action=/admin/bot-channels>
<input type=hidden name=id value="%s"> 名称 <input name=name value="%s"> app_id <input name=app_id value="%s">
app_secret <input name=app_secret type=password placeholder="留空不改"> 用途 <input name=purpose value="%s">
verification_token <input name=verification_token type=password placeholder="留空不改"> encrypt_key <input name=encrypt_key type=password placeholder="留空不改">
<label><input type=checkbox name=cannot_send %s>禁发</label><label><input type=checkbox name=cannot_receive %s>禁收</label>
<label><input type=checkbox name=disabled %s>禁用</label> <button>保存</button>
<button name=action value=delete>删除</button>
</form></td></tr>`,
			esc(c.ID), esc(c.Name), esc(c.AppID), secretStatus, tokenStatus, encryptStatus, esc(c.Purpose), c.CanReceive, c.CanSend, c.Active, esc(botChannelWSStatus(c)),
			esc(c.ID), esc(c.Name), esc(c.AppID), esc(c.Purpose), checked(!c.CanSend), checked(!c.CanReceive), checked(!c.Active))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) upsertBotChannel(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("action") == "delete" {
		id := strings.TrimSpace(r.FormValue("id"))
		if err := s.Repo.DeleteBotChannel(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_bot_channel_delete", id)
		writeOK(w, "bot channel deleted")
		return
	}
	c := model.BotChannel{
		ID:         strings.TrimSpace(r.FormValue("id")),
		Name:       strings.TrimSpace(r.FormValue("name")),
		AppID:      strings.TrimSpace(r.FormValue("app_id")),
		Purpose:    firstNonEmpty(r.FormValue("purpose"), "general"),
		CanSend:    !truthy(r.FormValue("cannot_send")),
		CanReceive: !truthy(r.FormValue("cannot_receive")),
		Active:     !truthy(r.FormValue("disabled")),
	}
	if secret := strings.TrimSpace(r.FormValue("app_secret")); secret != "" {
		enc, err := secretbox.Encrypt(s.SecretKey, secret)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		c.AppSecretEnc = enc
		c.AppSecretSet = true
	}
	if token := strings.TrimSpace(r.FormValue("verification_token")); token != "" {
		c.VerificationToken = token
		c.VerificationTokenSet = true
	}
	if encryptKey := strings.TrimSpace(r.FormValue("encrypt_key")); encryptKey != "" {
		c.EncryptKey = encryptKey
		c.EncryptKeySet = true
	}
	if c.ID == "" || c.Name == "" || c.AppID == "" {
		http.Error(w, "id/name/app_id required", http.StatusBadRequest)
		return
	}
	if err := s.Repo.UpsertBotChannel(r.Context(), c); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_bot_channel_upsert", c.ID)
	writeOK(w, "bot channel saved")
}

func (s *Server) services(w http.ResponseWriter, r *http.Request) {
	items, err := s.Repo.ListRegisteredServices(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeHTMLHeader(w, "租户 / 成员空间")
	_, _ = w.Write([]byte(`<h3>租户 / 成员空间</h3>
<p>概念：租户是一个成员或业务空间；API key 归属于租户。旧“注册服务”在后台统一改称租户/成员空间，内部仍复用 registered_service 表。</p>
<form method=post action=/admin/services>
<p>ID <input name=id> 名称 <input name=name> 描述 <input name=description>
投递 <input name=delivery_type value=ws> 回复 <input name=reply_mode value=sync>
<label><input type=checkbox name=disabled>禁用</label> <button>保存服务</button></p>
</form>
<table border=1 cellpadding=4><tr><th>租户ID</th><th>名称</th><th>投递</th><th>状态</th><th>操作</th></tr>`))
	if len(items) == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=5>暂无租户。先创建租户，再签发 API key 并绑定飞书账号。</td></tr>`))
	}
	for _, svc := range items {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s/%s</td><td>%t</td><td><form method=post action=/admin/services><input type=hidden name=action value=delete><input type=hidden name=id value="%s"><button>删除</button></form></td></tr>`,
			esc(svc.ID), esc(svc.Name), esc(svc.DeliveryType), esc(svc.ReplyMode), svc.Enabled, esc(svc.ID))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) upsertService(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("action") == "delete" {
		id := strings.TrimSpace(r.FormValue("id"))
		if err := s.Repo.DeleteRegisteredService(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_service_delete", id)
		writeOK(w, "service deleted")
		return
	}
	svc := model.RegisteredService{
		ID:           strings.TrimSpace(r.FormValue("id")),
		Name:         strings.TrimSpace(r.FormValue("name")),
		Description:  strings.TrimSpace(r.FormValue("description")),
		DeliveryType: firstNonEmpty(r.FormValue("delivery_type"), "ws"),
		ReplyMode:    firstNonEmpty(r.FormValue("reply_mode"), "sync"),
		Enabled:      !truthy(r.FormValue("disabled")),
	}
	if svc.ID == "" || svc.Name == "" {
		http.Error(w, "id/name required", http.StatusBadRequest)
		return
	}
	if err := s.Repo.UpsertRegisteredService(r.Context(), svc); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_service_upsert", svc.ID)
	writeOK(w, "service saved")
}

func (s *Server) apiKeys(w http.ResponseWriter, r *http.Request) {
	items, err := s.Repo.ListServiceAPIKeys(r.Context(), strings.TrimSpace(r.URL.Query().Get("service_id")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeHTMLHeader(w, "凭证 API key")
	_, _ = w.Write([]byte(`<h3>凭证 API key</h3>
<p>概念：<b>key_id</b> 是公开地址标识，会出现在 <code>会话名#key_id</code>；<b>secret</b> 是私密连接密钥，只在签发时返回一次，库内只存 hash。</p>
<form method=post action=/admin/api-keys>
<input type=hidden name=action value=issue>
<p>租户ID <input name=service_id> 标签 <input name=label> <button>签发 key（secret 只显示一次）</button></p>
</form>
<form method=post action=/admin/api-keys>
<input type=hidden name=action value=bind>
<p>key_id <input name=key_id> 绑定账号 <input name=chat_entity_id placeholder="dev:personal:ou_xxx"> <button>绑定账号</button></p>
</form>
<table border=1 cellpadding=4><tr><th>key_id</th><th>租户</th><th>标签</th><th>启用</th><th>操作</th></tr>`))
	if len(items) == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=5>暂无凭证。签发后请立即保存返回的 secret；后台不会再次显示。</td></tr>`))
	}
	for _, key := range items {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%t</td><td><form method=post action=/admin/api-keys><input type=hidden name=action value=revoke><input type=hidden name=key_id value="%s"><button>吊销</button></form></td></tr>`,
			esc(key.ID), esc(key.ServiceID), esc(key.Label), key.Active, esc(key.ID))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) manageAPIKey(w http.ResponseWriter, r *http.Request) {
	switch r.FormValue("action") {
	case "issue":
		secret, key, err := s.Prefix.IssueAPIKey(r.Context(), strings.TrimSpace(r.FormValue("service_id")), strings.TrimSpace(r.FormValue("label")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_api_key_issue", key.ID)
		writeJSON(w, map[string]string{"id": key.ID, "key_id": key.ID, "secret": secret, "api_key": secret})
	case "revoke":
		id := strings.TrimSpace(r.FormValue("key_id"))
		if err := s.Repo.RevokeServiceAPIKey(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_api_key_revoke", id)
		writeOK(w, "api key revoked")
	case "bind":
		id := strings.TrimSpace(r.FormValue("key_id"))
		account := strings.TrimSpace(r.FormValue("chat_entity_id"))
		if err := s.Repo.BindAPIKeyAccount(r.Context(), id, account); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_api_key_bind", id+":"+account)
		writeOK(w, "api key bound")
	default:
		http.Error(w, "action required", http.StatusBadRequest)
	}
}

func (s *Server) routes(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/sessions", http.StatusSeeOther)
}

func (s *Server) addRoute(w http.ResponseWriter, r *http.Request) {
	if r.FormValue("action") == "delete" {
		id := strings.TrimSpace(r.FormValue("id"))
		if err := s.Repo.DeleteRoutingRule(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_route_delete", id)
		writeOK(w, "route deleted")
		return
	}
	rule := model.RoutingRule{
		ID:               strings.TrimSpace(r.FormValue("id")),
		ServiceID:        strings.TrimSpace(r.FormValue("service_id")),
		MatchExpr:        strings.TrimSpace(r.FormValue("match_expr")),
		AccountScopeJSON: strings.TrimSpace(r.FormValue("account_scope_json")),
		CaseSensitive:    truthy(r.FormValue("case_sensitive")),
		StripPrefix:      truthy(r.FormValue("strip_prefix")),
		Enabled:          !truthy(r.FormValue("disabled")),
	}
	if err := s.Prefix.AddPrefixRule(r.Context(), rule); err != nil {
		http.Error(w, "路由冲突/非法："+err.Error(), http.StatusConflict)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_route_add", rule.ID+":"+rule.MatchExpr)
	writeOK(w, "route saved")
}

func (s *Server) configs(w http.ResponseWriter, r *http.Request) {
	items, err := s.Repo.ListAppConfig(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	systemRoutes, err := s.Repo.ListSystemRoutes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	current := map[string]model.AppConfig{}
	for _, c := range items {
		current[c.Key] = c
	}
	writeHTMLHeader(w, "运行配置")
	_, _ = w.Write([]byte(`<h3>运行配置</h3>
<p>数据来源：预置项写入 app_config 表。这里不配置监听端口、DB 路径、WP_SECRET_KEY 等 bootstrap 项；这些仍来自 .env / Secret。</p>
<table border=1 cellpadding=4><tr><th>配置项</th><th>说明</th><th>默认值</th><th>当前值</th><th>设置</th></tr>`))
	for _, preset := range configPresets() {
		cur := ""
		if c, ok := current[preset.Key]; ok {
			cur = c.ValueJSON
		}
		_, _ = fmt.Fprintf(w, `<tr><td>%s<br><code>%s</code></td><td>%s</td><td><code>%s</code></td><td><pre>%s</pre></td><td><form method=post action=/admin/config><input type=hidden name=key value="%s"><input name=value_json value="%s" size=38><button>保存</button></form></td></tr>`,
			esc(preset.Name), esc(preset.Key), esc(preset.Description), esc(preset.DefaultJSON), esc(firstNonEmpty(cur, preset.DefaultJSON)), esc(preset.Key), esc(firstNonEmpty(cur, preset.DefaultJSON)))
	}
	_, _ = w.Write([]byte(`</table>
<h4>系统关键词</h4>
<p>系统关键词必须在消息头部出现；群聊可在前面 @ 机器人。自然语言关键词走固定路径：record=进度上报/汇报，只记录不唤醒 deepseek；coordinate=排期修改/调整，唤醒 deepseek。<code>sys:调度</code> 是隐藏兼容别名。</p>
<form method=post action=/admin/config>
<input type=hidden name=action value=upsert_system_route>
<p>关键词 <input name=keyword placeholder="#进度汇报"> 路径 <select name=route_action><option value=record>record</option><option value=coordinate>coordinate</option><option value=auto>auto</option></select> 服务 <input name=service_name value=scheduler> 优先级 <input name=priority value=10 size=4> <button>保存关键词</button></p>
</form>
<table border=1 cellpadding=4><tr><th>关键词</th><th>服务</th><th>路径</th><th>优先级</th><th>操作</th></tr>`))
	for _, route := range systemRoutes {
		_, _ = fmt.Fprintf(w, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td><td>%d</td><td><form method=post action=/admin/config><input type=hidden name=action value=delete_system_route><input type=hidden name=keyword value="%s"><button>删除</button></form></td></tr>`,
			esc(route.Keyword), esc(route.ServiceName), esc(route.Action), route.Priority, esc(route.Keyword))
	}
	_, _ = w.Write([]byte(`</table>
<details><summary>高级：原始 key-value</summary>
<form method=post action=/admin/config>
<p>key <input name=key> value_json <textarea name=value_json rows=3 cols=60>{}</textarea> <button>保存配置</button></p>
</form>
<table border=1 cellpadding=4><tr><th>key</th><th>value_json</th><th>updated</th><th>操作</th></tr>`))
	if len(items) == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=4>暂无自定义配置。常用项请直接使用上面的预置项。</td></tr>`))
	}
	for _, c := range items {
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td><pre>%s</pre></td><td>%s</td><td><form method=post action=/admin/config><input type=hidden name=action value=delete><input type=hidden name=key value="%s"><button>删除</button></form></td></tr>`,
			esc(c.Key), esc(redact.Content(c.ValueJSON)), esc(c.UpdatedAt.Format(time.RFC3339)), esc(c.Key))
	}
	_, _ = w.Write([]byte(`</table></details>`))
}

func (s *Server) sessionsPage(w http.ResponseWriter, r *http.Request) {
	items, err := s.Repo.ListSessionEndpoints(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	routes, err := s.Repo.ListAllPrefixRoutes(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bots, err := s.Repo.ListBotChannels(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bySession := sessionRoutes(routes)
	writeHTMLHeader(w, "会话端点 / 通配 / 镜像")
	_, _ = w.Write([]byte(`<h3>会话端点 / 通配 / 镜像</h3>
<p>数据来源：sessionHelper 连接 <code>/ws/session/{会话名}?key_id=</code> 后自动写入。断开时会置为离线。路由分三层：①系统默认 M1-M7 指令优先且只读；②会话名精确选择器 <code>#会话名</code> 自动路由且只读；③用户通配在本页按会话行配置。</p>
<p>客户端 IP：仅当连接来自本机可信反代时信任 <code>X-Forwarded-For</code>/<code>X-Real-IP</code>，否则使用 RemoteAddr，防止伪造。</p>
<p>镜像默认回本会话 key 绑定的飞书账号，只需选择机器人账号；open_id 与 key_id 由后台自动推断。需要镜像到其他人或群时，可展开高级填写完整地址。</p>
<h4>系统默认</h4><p>排期、进度、风险、查询、mirror on/off 等 M1-M7/M10 内置指令优先级最高，不在此处修改。</p>
<table border=1 cellpadding=4><tr><th>key_id</th><th>会话名</th><th>工具</th><th>大模型</th><th>完整会话名</th><th>producer</th><th>target_group</th><th>last_seen</th><th>在线</th><th>client_ip</th><th>清单/派发隔离</th><th>会话自动</th><th>用户通配</th><th>镜像</th><th>镜像目标/操作</th></tr>`))
	visible := 0
	for _, ep := range items {
		if !ep.Active {
			continue
		}
		visible++
		key := ep.KeyID + "#" + ep.SessionName
		isolationAction := "no_directory_on"
		isolationButton := "开启"
		if ep.NoDirectoryAdmin {
			isolationAction = "no_directory_off"
			isolationButton = "关闭"
		}
		_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%s</td><td>%t</td><td>%s</td><td>%s</td><td>%t</td><td>%s</td><td>生效:%t 后台:%t<form method=post action=/admin/sessions><input type=hidden name=action value="%s"><input type=hidden name=key_id value="%s"><input type=hidden name=session_name value="%s"><button>%s不进清单不可派发</button></form></td><td><code>#%s</code> 精确匹配</td><td>%s
<form method=post action=/admin/sessions><input type=hidden name=action value=add_wildcard><input type=hidden name=key_id value="%s"><input type=hidden name=session_name value="%s">通配 <input name=match_expr placeholder="dev* 或 foo;bar"><label><input type=checkbox name=strip_prefix>剥离通配</label><button>添加</button></form></td><td>%t</td><td>
<form method=post action=/admin/sessions>
<input type=hidden name=key_id value="%s"><input type=hidden name=session_name value="%s">
目标 %s<br>
机器人账号 <select name=bot_name>%s</select>
<details><summary>高级：镜像到其他人或群</summary>
完整地址 <input name=mirror_to value="" placeholder="ou_xxx#key#UnifiedRobot 或 oc_xxx#key#UnifiedRobot">
</details>
<button name=action value=mirror_on>开启镜像</button><button name=action value=mirror_off>关闭镜像</button>
</form></td></tr>`,
			esc(ep.KeyID), esc(ep.SessionName), esc(ep.Tool), esc(ep.Model), esc(ep.FullSessionName), ep.Producer, esc(ep.TargetGroup), esc(ep.LastSeenAt.Format(time.RFC3339)), ep.Active, esc(ep.ClientIP), ep.NoDirectory, ep.NoDirectoryAdmin, isolationAction, esc(ep.KeyID), esc(ep.SessionName), isolationButton, esc(ep.SessionName), renderSessionRoutes(bySession[key]),
			esc(ep.KeyID), esc(ep.SessionName), ep.MirrorEnabled, esc(ep.KeyID), esc(ep.SessionName), esc(s.mirrorTargetDisplay(r.Context(), ep.MirrorTo, bots)), botOptions(bots, mirrorBotName(ep.MirrorTo)))
	}
	if visible == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=15>暂无在线会话。启动 sessionHelper 后会自动出现；离线会话默认隐藏，避免测试残留干扰。</td></tr>`))
	}
	_, _ = w.Write([]byte(`</table>`))
	s.renderMemberSessionDirectory(w, r.Context(), items)
}

func (s *Server) renderMemberSessionDirectory(w http.ResponseWriter, ctx context.Context, endpoints []model.SessionEndpoint) {
	members, err := s.Repo.ListMembers(ctx)
	if err != nil {
		_, _ = fmt.Fprintf(w, `<p>成员会话目录加载失败：%s</p>`, esc(err.Error()))
		return
	}
	_, _ = w.Write([]byte(`<h4>成员会话目录</h4>
<p>数据来源：成员 open_id、API key 绑定账号和在线 sessionHelper。跨成员寻址格式：<code>@成员#会话名 内容</code>；若成员只有一个在线会话，也可用 <code>@成员 内容</code>。</p>
<table border=1 cellpadding=4><tr><th>成员</th><th>owner_key</th><th>在线会话</th><th>可用地址</th><th>状态</th></tr>`))
	count := 0
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, ep := range endpoints {
			if !ep.Active || !s.endpointBelongsToMember(ctx, ep, member) {
				continue
			}
			count++
			status := "可达"
			if member.DMOptOut {
				status = "未开放会话接入"
			}
			addr := fmt.Sprintf("@%s#%s", memberLabelForAdmin(member), ep.SessionName)
			_, _ = fmt.Fprintf(w, `<tr><td>%s</td><td>%s</td><td>%s</td><td><code>%s</code></td><td>%s</td></tr>`,
				esc(memberLabelForAdmin(member)), esc(member.OwnerKey), esc(ep.SessionName), esc(addr), esc(status))
		}
	}
	if count == 0 {
		_, _ = w.Write([]byte(`<tr><td colspan=5>暂无成员在线会话。成员启动 sessionHelper 后会出现在这里。</td></tr>`))
	}
	_, _ = w.Write([]byte(`</table>`))
}

func (s *Server) endpointBelongsToMember(ctx context.Context, ep model.SessionEndpoint, member model.Member) bool {
	accounts, err := s.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
	if err != nil {
		return false
	}
	for _, account := range accounts {
		if account == member.OwnerKey || (member.FeishuOpenID != "" && (account == member.FeishuOpenID || strings.HasSuffix(account, ":"+member.FeishuOpenID))) {
			return true
		}
	}
	return false
}

func memberLabelForAdmin(member model.Member) string {
	return firstNonEmpty(member.DisplayName, member.OwnerKey)
}

func (s *Server) manageSession(w http.ResponseWriter, r *http.Request) {
	keyID := strings.TrimSpace(r.FormValue("key_id"))
	sessionName := strings.TrimSpace(r.FormValue("session_name"))
	if keyID == "" || sessionName == "" {
		http.Error(w, "key_id/session_name required", http.StatusBadRequest)
		return
	}
	switch r.FormValue("action") {
	case "no_directory_on", "no_directory_off":
		enabled := r.FormValue("action") == "no_directory_on"
		if err := s.Repo.SetSessionNoDirectory(r.Context(), keyID, sessionName, enabled); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if s.Prefix != nil {
			_ = s.Prefix.BroadcastOnlineDirectoryForKey(r.Context(), keyID, true)
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_session_no_directory", fmt.Sprintf("%s#%s:%t", sessionName, keyID, enabled))
		writeOK(w, "session no_directory updated")
		return
	case "add_wildcard":
		expr := strings.TrimSpace(r.FormValue("match_expr"))
		if expr == "" {
			http.Error(w, "match_expr required", http.StatusBadRequest)
			return
		}
		serviceID := sessionServiceID(keyID, sessionName)
		if err := s.Repo.UpsertRegisteredService(r.Context(), model.RegisteredService{
			ID:           serviceID,
			Name:         "会话 " + sessionName,
			Description:  "会话通配路由，由 M9 会话端点页自动维护",
			DeliveryType: "session",
			ReplyMode:    "none",
			Enabled:      true,
		}); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		rule := model.RoutingRule{
			ID:              sessionRouteID(keyID, sessionName, expr),
			ServiceID:       serviceID,
			MatchExpr:       expr,
			StripPrefix:     truthy(r.FormValue("strip_prefix")),
			Enabled:         true,
			ScopeEntityType: "",
		}
		if err := s.Prefix.AddPrefixRule(r.Context(), rule); err != nil {
			http.Error(w, "通配冲突/非法："+err.Error(), http.StatusConflict)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_session_wildcard_add", rule.ID+":"+expr)
		writeOK(w, "session wildcard saved")
		return
	case "delete_wildcard":
		id := strings.TrimSpace(r.FormValue("route_id"))
		if id == "" {
			http.Error(w, "route_id required", http.StatusBadRequest)
			return
		}
		if err := s.Repo.DeleteRoutingRule(r.Context(), id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_session_wildcard_delete", id)
		writeOK(w, "session wildcard deleted")
		return
	}
	enabled := r.FormValue("action") == "mirror_on"
	mirrorTo := ""
	if enabled {
		var err error
		mirrorTo, err = s.resolveMirrorTarget(r.Context(), keyID, strings.TrimSpace(r.FormValue("bot_name")), strings.TrimSpace(r.FormValue("mirror_to")))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	if err := s.Repo.SetSessionMirror(r.Context(), keyID, sessionName, enabled, mirrorTo); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.Prefix != nil {
		_ = s.Prefix.RouteEnvelope(r.Context(), mirrorControlEnvelope(keyID, sessionName, enabled, mirrorTo))
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_session_mirror", fmt.Sprintf("%s#%s:%t", sessionName, keyID, enabled))
	writeOK(w, "session mirror updated")
}

func (s *Server) upsertConfig(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.FormValue("key"))
	switch r.FormValue("action") {
	case "upsert_system_route":
		priority, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("priority")))
		route := model.SystemRoute{
			Keyword:     strings.TrimSpace(r.FormValue("keyword")),
			ServiceName: firstNonEmpty(r.FormValue("service_name"), "scheduler"),
			Action:      firstNonEmpty(r.FormValue("route_action"), "auto"),
			Priority:    priority,
			Active:      true,
		}
		if route.Keyword == "" {
			http.Error(w, "keyword required", http.StatusBadRequest)
			return
		}
		if err := s.Repo.UpsertSystemRoute(r.Context(), route); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_system_route_upsert", route.Keyword+":"+route.Action)
		writeOK(w, "system route saved")
		return
	case "delete_system_route":
		keyword := strings.TrimSpace(r.FormValue("keyword"))
		if err := s.Repo.DeleteSystemRoute(r.Context(), keyword); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_system_route_delete", keyword)
		writeOK(w, "system route deleted")
		return
	}
	if r.FormValue("action") == "delete" {
		if err := s.Repo.DeleteAppConfig(r.Context(), key); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_config_delete", key)
		writeOK(w, "config deleted")
		return
	}
	value := strings.TrimSpace(r.FormValue("value_json"))
	if key == "" || !json.Valid([]byte(value)) {
		http.Error(w, "key and valid value_json required", http.StatusBadRequest)
		return
	}
	if err := s.Repo.UpsertAppConfig(r.Context(), key, value); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_config_upsert", key)
	writeOK(w, "config saved")
}

func (s *Server) delegateSchedule(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.FormValue("owner_key"))
	task := strings.TrimSpace(r.FormValue("task"))
	start := strings.TrimSpace(r.FormValue("start_date"))
	end := firstNonEmpty(r.FormValue("end_date"), start)
	if owner == "" || task == "" || start == "" {
		http.Error(w, "owner_key/start_date/task required", http.StatusBadRequest)
		return
	}
	change := []schedule.Change{{Label: "🆕", Action: "insert", New: model.Schedule{OwnerKey: owner, StartDate: start, EndDate: end, Task: task, Status: "planned", Priority: 100}}}
	payload, _ := json.Marshal(change)
	if _, err := s.Repo.PutPending(r.Context(), owner, string(payload), time.Now().Add(24*time.Hour)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	notice := fmt.Sprintf("管理者提交了代改排期：%s %s~%s。请在机器人对话回复『确认』生效，或回复『取消』/申诉。", task, start, end)
	_ = s.enqueueNotice(r.Context(), owner, notice)
	_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_delegate_schedule", owner+":"+task)
	writeOK(w, "delegate pending created")
}

func (s *Server) cleanup(w http.ResponseWriter, r *http.Request) {
	cutoff, err := time.Parse("2006-01-02", strings.TrimSpace(r.FormValue("cutoff")))
	if err != nil {
		http.Error(w, "cutoff must be YYYY-MM-DD", http.StatusBadRequest)
		return
	}
	if cutoff.After(time.Now().AddDate(0, -1, 0)) {
		http.Error(w, "cutoff must be at least 1 month ago", http.StatusBadRequest)
		return
	}
	confirm := truthy(r.FormValue("confirm"))
	result, err := s.Repo.CleanupBefore(r.Context(), cutoff, confirm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if confirm {
		_ = s.Repo.WriteAudit(r.Context(), adminUser(r), "admin_cleanup_confirm", cutoff.Format("2006-01-02"))
	}
	writeJSON(w, result)
}

func (s *Server) aggregateDraftNow(w http.ResponseWriter, r *http.Request) {
	if s.Scheduler == nil {
		writeAggregateManualResult(w, nil, errors.New("scheduler service is not configured"), http.StatusServiceUnavailable)
		return
	}
	reports, err := s.Scheduler.RunAggregateWeeklyDrafts(r.Context(), "admin手动聚合周报草稿")
	writeAggregateManualResult(w, reports, err, http.StatusInternalServerError)
}

func (s *Server) aggregatePublishNow(w http.ResponseWriter, r *http.Request) {
	if s.Scheduler == nil {
		writeAggregateManualResult(w, nil, errors.New("scheduler service is not configured"), http.StatusServiceUnavailable)
		return
	}
	reports, err := s.Scheduler.PublishDueAggregateWeeklyReports(r.Context(), "admin手动聚合周报发布")
	writeAggregateManualResult(w, reports, err, http.StatusInternalServerError)
}

func writeAggregateManualResult(w http.ResponseWriter, reports []model.ProjectWeeklyReport, err error, errorStatus int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	projectIDs := make([]string, 0, len(reports))
	for _, report := range reports {
		projectIDs = append(projectIDs, report.ProjectID)
	}
	status := http.StatusOK
	resp := map[string]any{
		"processed":   len(reports),
		"project_ids": projectIDs,
	}
	if err != nil {
		status = errorStatus
		resp["error"] = err.Error()
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("wp_admin")
		if err != nil {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		s.mu.Lock()
		_, ok := s.sessions[c.Value]
		s.mu.Unlock()
		if !ok {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (s *Server) enqueueNotice(ctx context.Context, ownerKey, text string) error {
	if s.Outbound == nil {
		return nil
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	return s.Outbound.Enqueue(ctx, model.Message{
		ChatEntityID: ownerKey,
		BotChannelID: botChannel(ownerKey),
		ChatType:     model.ChatPersonal,
		Content:      string(content),
	})
}

func botChannel(ownerKey string) string {
	if i := strings.Index(ownerKey, ":"); i > 0 {
		return ownerKey[:i]
	}
	return "default"
}

func mirrorControlEnvelope(keyID, sessionName string, enabled bool, mirrorTo string) model.Envelope {
	action := "off"
	if enabled {
		action = "on"
	}
	return model.Envelope{
		ID:   token(),
		To:   sessionName + "#" + keyID,
		From: "workpulse#" + keyID,
		Body: "mirror " + action,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":      "mirror_control",
			"enabled":   enabled,
			"mirror_to": mirrorTo,
			"system":    true,
			"no_mirror": true,
		},
	}
}

func (s *Server) resolveMirrorTarget(ctx context.Context, keyID, botName, advanced string) (string, error) {
	if advanced != "" {
		if _, _, _, ok := parseMirrorAddress(advanced); !ok {
			return "", fmt.Errorf("完整镜像地址格式应为 ou_xxx#key_id#bot_name 或 oc_xxx#key_id#bot_name")
		}
		return advanced, nil
	}
	if botName == "" {
		bots, err := s.Repo.ListBotChannels(ctx)
		if err != nil {
			return "", err
		}
		for _, bot := range bots {
			if bot.Active && bot.CanSend {
				botName = firstNonEmpty(bot.Name, bot.ID)
				break
			}
		}
	}
	if botName == "" {
		return "", fmt.Errorf("请选择机器人账号")
	}
	openID, err := s.defaultMirrorOpenID(ctx, keyID)
	if err != nil {
		return "", err
	}
	return openID + "#" + keyID + "#" + botName, nil
}

func (s *Server) defaultMirrorOpenID(ctx context.Context, keyID string) (string, error) {
	accounts, err := s.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil {
		return "", err
	}
	for _, account := range accounts {
		if openID, ok := personalOpenIDFromAccount(account); ok {
			return openID, nil
		}
	}
	return "", fmt.Errorf("该 key_id 未绑定个人飞书账号，无法自动推断镜像目标")
}

func (s *Server) mirrorTargetDisplay(ctx context.Context, mirrorTo string, bots []model.BotChannel) string {
	target, _, botName, ok := parseMirrorAddress(mirrorTo)
	if !ok {
		return "未设置"
	}
	label := "未知账号"
	if strings.HasPrefix(target, "oc_") {
		label = "群聊"
	} else if name := s.displayNameForOpenID(ctx, target, botChannelIDForName(bots, botName)); name != "" {
		label = name
	}
	return label + " via " + botName
}

func (s *Server) displayNameForOpenID(ctx context.Context, openID, botChannelID string) string {
	members, err := s.Repo.ListMembers(ctx)
	if err == nil {
		for _, m := range members {
			if m.FeishuOpenID == openID && m.DisplayName != "" {
				return m.DisplayName
			}
		}
	}
	if botChannelID != "" {
		if e, err := s.Repo.GetChatEntity(ctx, botChannelID, openID); err == nil && e != nil && e.DisplayName != "" {
			return e.DisplayName
		}
	}
	persons, err := s.Repo.ListSeenPersons(ctx)
	if err == nil {
		for _, p := range persons {
			if p.OpenID == openID && p.Name != "" {
				return p.Name
			}
		}
	}
	return ""
}

func parseMirrorAddress(addr string) (target, keyID, botName string, ok bool) {
	parts := strings.Split(addr, "#")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[2], true
}

func mirrorBotName(addr string) string {
	_, _, botName, ok := parseMirrorAddress(addr)
	if !ok {
		return ""
	}
	return botName
}

func personalOpenIDFromAccount(account string) (string, bool) {
	parts := strings.Split(account, ":")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] == string(model.ChatPersonal) && parts[i+1] != "" {
			return parts[i+1], true
		}
	}
	return "", false
}

func botChannelIDForName(bots []model.BotChannel, botName string) string {
	for _, bot := range bots {
		if bot.Name == botName || bot.ID == botName {
			return bot.ID
		}
	}
	return botName
}

func botOptions(bots []model.BotChannel, selected string) string {
	var b strings.Builder
	for _, bot := range bots {
		if !bot.Active || !bot.CanSend {
			continue
		}
		name := firstNonEmpty(bot.Name, bot.ID)
		sel := ""
		if name == selected || (selected == "" && b.Len() == 0) {
			sel = " selected"
		}
		fmt.Fprintf(&b, `<option value="%s"%s>%s</option>`, esc(name), sel, esc(name))
	}
	if b.Len() == 0 {
		b.WriteString(`<option value="">未配置可发送机器人</option>`)
	}
	return b.String()
}

func writeHTMLHeader(w http.ResponseWriter, title string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!doctype html><meta charset=utf-8><title>%s</title>`, esc(title))
}

func writeOK(w http.ResponseWriter, msg string) {
	writeJSON(w, map[string]any{"ok": true, "message": msg})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

type configPreset struct {
	Key         string
	Name        string
	Description string
	DefaultJSON string
}

func configPresets() []configPreset {
	return []configPreset{
		{Key: "triggers.schedule", Name: "排期触发词（废弃）", Description: "历史配置项。当前排期入口只由系统关键词（#四字甲乙类 / sys:调度）触发，未命中系统关键词的消息静默。", DefaultJSON: `["+","-","改","顺延","全量","确认","取消"]`},
		{Key: "evidence.default_opt_out", Name: "佐证默认关闭", Description: "新成员是否默认关闭 AI 佐证抽取。", DefaultJSON: `false`},
		{Key: "global_schedule.path", Name: "全局排期文件", Description: "§M3 联动导出的全局日程 JSON 路径；为空则使用启动环境。", DefaultJSON: `""`},
		{Key: "notify.group_cron", Name: "群通知时间", Description: "R16 群通知 cron，默认 UTC 0 点；可填 0 0 或完整五段 cron。", DefaultJSON: `"0 0"`},
		{Key: "notify.personal_cron", Name: "个人提醒时间", Description: "R16 个人 DM 提醒 cron，默认 UTC 0 点和 6 点；可填 0 0,6 或完整五段 cron。", DefaultJSON: `"0 0,6"`},
		{Key: "schedule.evidence_cron", Name: "项目佐证覆盖时间", Description: "项目级 evidence_cron 覆盖使用的兼容配置；R16 群/个人通知使用 notify.*_cron。", DefaultJSON: `"0 0,6 * * *"`},
		{Key: "schedule.evidence_tz", Name: "佐证通报时区", Description: "R8 定时佐证 cron 使用的 IANA 时区；默认 UTC，除非运行配置显式指定其它时区。", DefaultJSON: `"UTC"`},
		{Key: "schedule.notify_chat", Name: "排期通知群", Description: "R8 定时佐证完成后发送摘要的飞书群 chat_id；为空则使用 WP_SCHEDULE_NOTIFY_CHAT。", DefaultJSON: `""`},
		{Key: "schedule.notify_bot", Name: "排期通知机器人", Description: "发送排期协调/佐证通知的 bot_channel_id；为空则使用 WP_SCHEDULE_NOTIFY_BOT。", DefaultJSON: `"default"`},
		{Key: "schedule.transcript_dirs", Name: "Transcript 目录", Description: "逗号分隔的额外 transcript 数据源；成员 sessionHelper 接入后优先用 WorkPulse 已收集消息。", DefaultJSON: `""`},
		{Key: "collect.retain_days", Name: "会话采集保留天数", Description: "sessionHelper 静默采集消息的保留天数；佐证前会清理超过该天数的 collect 消息。", DefaultJSON: `31`},
		{Key: "cleanup.retention_days", Name: "数据保留下限", Description: "清理保护下限，默认至少保留 31 天。", DefaultJSON: `31`},
		{Key: "m8.route.default_timeout_ms", Name: "M8 投递超时", Description: "外部 WS 服务同步转发默认超时。", DefaultJSON: `5000`},
	}
}

func sessionRoutes(routes []model.PrefixRoute) map[string][]model.PrefixRoute {
	out := map[string][]model.PrefixRoute{}
	for _, route := range routes {
		keyID, sessionName, ok := sessionServiceParts(route.Service.ID)
		if !ok {
			continue
		}
		out[keyID+"#"+sessionName] = append(out[keyID+"#"+sessionName], route)
	}
	return out
}

func renderSessionRoutes(routes []model.PrefixRoute) string {
	if len(routes) == 0 {
		return `<span>未配置</span>`
	}
	var b strings.Builder
	for _, route := range routes {
		_, _ = fmt.Fprintf(&b, `<div><code>%s</code> <form method=post action=/admin/sessions style="display:inline"><input type=hidden name=action value=delete_wildcard><input type=hidden name=key_id value="%s"><input type=hidden name=session_name value="%s"><input type=hidden name=route_id value="%s"><button>删除</button></form></div>`,
			esc(route.Rule.MatchExpr), esc(routeKeyID(route.Service.ID)), esc(routeSessionName(route.Service.ID)), esc(route.Rule.ID))
	}
	return b.String()
}

func sessionRouteID(keyID, sessionName, expr string) string {
	sum := sha256.Sum256([]byte(keyID + "\x00" + sessionName + "\x00" + expr))
	return "sr-" + hex.EncodeToString(sum[:])[:24]
}

func sessionServiceID(keyID, sessionName string) string {
	return "session:" + keyID + ":" + sessionName
}

func sessionServiceParts(serviceID string) (keyID string, sessionName string, ok bool) {
	if !strings.HasPrefix(serviceID, "session:") {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(serviceID, "session:"), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func routeKeyID(serviceID string) string {
	keyID, _, _ := sessionServiceParts(serviceID)
	return keyID
}

func routeSessionName(serviceID string) string {
	_, sessionName, _ := sessionServiceParts(serviceID)
	return sessionName
}

func defaultOwnerKey(name, openID string) string {
	base := strings.TrimSpace(name)
	if base == "" {
		base = strings.TrimSpace(openID)
	}
	base = strings.ToLower(base)
	base = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		default:
			return '-'
		}
	}, base)
	base = strings.Trim(base, "-_")
	if base == "" && len(openID) > 8 {
		base = openID[len(openID)-8:]
	}
	if len(base) > 32 {
		base = base[:32]
	}
	return base
}

func (s *Server) projectMemberCounts(ctx context.Context, projects []model.Project) map[string]int {
	out := map[string]int{}
	for _, p := range projects {
		members, err := s.Repo.ListProjectMembers(ctx, p.ID)
		if err == nil {
			out[p.ID] = len(members)
		}
	}
	return out
}

func groupOptions(groups []model.ChatEntity, selected string) string {
	var b strings.Builder
	for _, g := range groups {
		if !g.Active {
			continue
		}
		label := firstNonEmpty(g.DisplayName, g.FeishuID)
		sel := ""
		if g.FeishuID == selected {
			sel = " selected"
		}
		_, _ = fmt.Fprintf(&b, `<option value="%s"%s>%s (%s)</option>`, esc(g.FeishuID), sel, esc(label), esc(g.FeishuID))
	}
	return b.String()
}

func projectOptions(projects []model.Project, selected, exclude string) string {
	var b strings.Builder
	for _, p := range projects {
		if !p.Active || p.ID == exclude {
			continue
		}
		sel := ""
		if p.ID == selected {
			sel = " selected"
		}
		label := firstNonEmpty(p.Name, p.ID)
		_, _ = fmt.Fprintf(&b, `<option value="%s"%s>%s (%s)</option>`, esc(p.ID), sel, esc(label), esc(p.ID))
	}
	return b.String()
}

func memberOptions(members []model.Member, selected string) string {
	var b strings.Builder
	for _, m := range members {
		if !m.Active {
			continue
		}
		sel := ""
		if m.OwnerKey == selected {
			sel = " selected"
		}
		label := firstNonEmpty(m.DisplayName, m.OwnerKey)
		_, _ = fmt.Fprintf(&b, `<option value="%s"%s>%s (%s)</option>`, esc(m.OwnerKey), sel, esc(label), esc(m.OwnerKey))
	}
	return b.String()
}

func projectMemberLabel(members []model.Member, ownerKey string) string {
	if ownerKey == "" {
		return ""
	}
	for _, m := range members {
		if m.OwnerKey == ownerKey {
			return fmt.Sprintf("%s (%s)", firstNonEmpty(m.DisplayName, m.OwnerKey), m.OwnerKey)
		}
	}
	return ownerKey
}

func botChannelWSStatus(c model.BotChannel) string {
	switch {
	case !c.Active:
		return "未启动：已禁用"
	case !c.CanReceive:
		return "未启动：禁收"
	case !c.AppSecretSet:
		return "未启动：缺 app_secret"
	default:
		return "启动时建立WS"
	}
}

func checked(v bool) string {
	if v {
		return "checked"
	}
	return ""
}

func ownerForOpenID(members []model.Member, openID string) string {
	for _, m := range members {
		if m.Active && m.FeishuOpenID == openID {
			return m.OwnerKey
		}
	}
	return ""
}

func simpleDiff(prev, next string) string {
	prevLines := strings.Split(prev, "\n")
	nextLines := strings.Split(next, "\n")
	limit := max(len(prevLines), len(nextLines))
	var b strings.Builder
	for i := 0; i < limit; i++ {
		old, neu := "", ""
		if i < len(prevLines) {
			old = prevLines[i]
		}
		if i < len(nextLines) {
			neu = nextLines[i]
		}
		if old == neu {
			_, _ = fmt.Fprintf(&b, "  %s\n", old)
			continue
		}
		if old != "" {
			_, _ = fmt.Fprintf(&b, "- %s\n", old)
		}
		if neu != "" {
			_, _ = fmt.Fprintf(&b, "+ %s\n", neu)
		}
	}
	return b.String()
}

func adminUser(r *http.Request) string {
	if c, err := r.Cookie("wp_admin"); err == nil {
		return "admin_session:" + c.Value[:min(len(c.Value), 8)]
	}
	return "admin"
}

// HashPassword 生成密码哈希。
// 脚手架用 sha256+salt；TODO §M9：生产改 bcrypt / argon2id。
func HashPassword(plain string) string {
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	sum := sha256.Sum256(append(salt, []byte(plain)...))
	return "sha256$" + hex.EncodeToString(salt) + "$" + hex.EncodeToString(sum[:])
}

// VerifyPassword 校验密码（常量时间比较）。
func VerifyPassword(stored, plain string) bool {
	parts := strings.Split(stored, "$")
	if len(parts) != 3 || parts[0] != "sha256" {
		return false
	}
	salt, err := hex.DecodeString(parts[1])
	if err != nil {
		return false
	}
	sum := sha256.Sum256(append(salt, []byte(plain)...))
	want, err := hex.DecodeString(parts[2])
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(sum[:], want) == 1
}

func token() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func displayName(m model.Member) string {
	if m.DisplayName != "" {
		return m.DisplayName
	}
	return m.OwnerKey
}

func esc(s string) string {
	return html.EscapeString(s)
}

// SeedAdmin 若无管理员则用初始密码创建（首次 bootstrap，§9.1）。
func SeedAdmin(ctx context.Context, repo store.Repository, username, initPass string) (bool, error) {
	n, err := repo.AdminCount(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 || username == "" || initPass == "" {
		return false, nil
	}
	a := model.AdminUser{
		ID:           token(),
		Username:     username,
		PasswordHash: HashPassword(initPass),
		Active:       true,
		CreatedAt:    time.Now().UTC(),
	}
	return true, repo.CreateAdmin(ctx, a)
}
