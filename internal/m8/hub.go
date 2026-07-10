// Package m8 implements the minimal service registry and prefix WS routing.
package m8

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/llm"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/router"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

type MessageQueue interface {
	Enqueue(ctx context.Context, m model.Message) error
}

type SystemHandler interface {
	HandleSystemRequest(ctx context.Context, serviceName, action, body string, source model.Message) (string, error)
}

type aggregateWeeklyReviewHandler interface {
	HandleAggregateWeeklyReviewCommand(ctx context.Context, body string, source model.Message) (string, bool, error)
}

type Hub struct {
	Repo     store.Repository
	Outbound MessageQueue
	System   SystemHandler
	L2       llm.Provider
	L2Config L2Config

	mu               sync.Mutex
	serviceClients   map[string]*client
	sessionClients   map[string]map[string]*sessionClient
	keyAccounts      map[string]map[string]bool
	botChannels      map[string]string
	botNames         map[string]string
	envelopeIDs      map[string]string
	onlineTimers     map[string]*time.Timer
	terminals        map[string]*terminalState
	terminalLastSize map[string][2]int
	syncTargets      map[string]map[string]feishuSyncTarget
	recentTerminal   map[string][]terminalSyncItem
	syncBuffers      map[string]*feishuSyncBuffer
	linkTokens       map[string]ownerLinkToken
	onlineDebounce   time.Duration
}

type L2Config struct {
	Workers             int
	PollInterval        time.Duration
	LeaseDuration       time.Duration
	ProviderTimeout     time.Duration
	ConfidenceThreshold float64
}

type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type sessionClient struct {
	conn           *websocket.Conn
	mu             sync.Mutex
	keyID          string
	sessionName    string
	targetBot      string
	webTerminal    bool
	osName         string
	skillInstalled bool
	skillCancel    context.CancelFunc
}

type feishuSyncTarget struct {
	openID       string
	keyID        string
	botName      string
	botChannelID string
}

type terminalSyncItem struct {
	TS   time.Time
	Text string
}

type feishuSyncBuffer struct {
	keyID       string
	sessionName string
	target      feishuSyncTarget
	items       []terminalSyncItem
	timer       *time.Timer
}

type ownerLinkToken struct {
	ownerKey  string
	expiresAt time.Time
}

type ForwardRequest struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	ChatEntityID string `json:"chat_entity_id"`
	BotChannelID string `json:"bot_channel_id"`
	ChatType     string `json:"chat_type"`
	SenderOpenID string `json:"sender_open_id,omitempty"`
	Text         string `json:"text"`
	RawContent   string `json:"raw_content"`
}

type ForwardResponse struct {
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}

const (
	secOpsOwnerKey             = "systemtaskintl"
	secOpsMemberName           = "SYSTEM-V-TASK-INTERNAL"
	secOpsKeyword              = "#系统安全"
	agentNetworkSkillAck       = "DINGWEI_COMM_SKILL_INSTALLED"
	agentNetworkSkillPushType  = "agent_network_skill"
	agentNetworkSkillAckType   = "agent_network_skill_ack"
	agentNetworkSkillRetryWait = 2 * time.Minute
	ownerLinkTokenTTL          = 8 * time.Hour
)

var (
	secOpsAdminOpenID      = strings.TrimSpace(os.Getenv("WP_SECOPS_ADMIN_OPENID"))
	secOpsAdminOwnerKey    = strings.TrimSpace(os.Getenv("WP_SECOPS_ADMIN_OWNER_KEY"))
	applyKeyApproverID     = strings.TrimSpace(os.Getenv("WP_APPLY_KEY_APPROVER_OPENID"))
	ansiCSIRE              = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSCSTRE            = regexp.MustCompile(`\x1b\][^\x07]*(\x07|\x1b\\)`)
	ansiSimpleRE           = regexp.MustCompile(`\x1b[@-Z\\-_]`)
	controlExceptTextRE    = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	bareSessionNamePattern = regexp.MustCompile(`^[a-z0-9]+$`)
	sessionNamePattern     = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+-[0-9a-f]{4}$`)
)

func New(repo store.Repository) *Hub {
	return &Hub{
		Repo:             repo,
		serviceClients:   map[string]*client{},
		sessionClients:   map[string]map[string]*sessionClient{},
		keyAccounts:      map[string]map[string]bool{},
		botChannels:      map[string]string{},
		botNames:         map[string]string{},
		envelopeIDs:      map[string]string{},
		onlineTimers:     map[string]*time.Timer{},
		terminals:        map[string]*terminalState{},
		terminalLastSize: map[string][2]int{},
		syncTargets:      map[string]map[string]feishuSyncTarget{},
		recentTerminal:   map[string][]terminalSyncItem{},
		syncBuffers:      map[string]*feishuSyncBuffer{},
		linkTokens:       map[string]ownerLinkToken{},
		onlineDebounce:   2500 * time.Millisecond,
		L2Config: L2Config{
			Workers:             4,
			PollInterval:        200 * time.Millisecond,
			LeaseDuration:       5 * time.Minute,
			ProviderTimeout:     60 * time.Second,
			ConfidenceThreshold: 0.60,
		},
	}
}

func (h *Hub) StartL2Workers(ctx context.Context) {
	if h.Repo == nil || h.L2 == nil {
		return
	}
	cfg := h.effectiveL2Config()
	for i := 0; i < cfg.Workers; i++ {
		workerID := fmt.Sprintf("l2-%d-%s", i+1, randomHex(4))
		go h.runL2Worker(ctx, workerID, cfg)
	}
}

func (h *Hub) effectiveL2Config() L2Config {
	cfg := h.L2Config
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 5 * time.Minute
	}
	if cfg.ProviderTimeout <= 0 {
		cfg.ProviderTimeout = 60 * time.Second
	}
	if cfg.ConfidenceThreshold <= 0 {
		cfg.ConfidenceThreshold = 0.60
	}
	return cfg
}

func (h *Hub) runL2Worker(ctx context.Context, workerID string, cfg L2Config) {
	for {
		task, err := h.Repo.ClaimNextL2ControlTask(ctx, workerID, time.Now().UTC().Add(cfg.LeaseDuration), time.Now().UTC())
		if err == nil && task == nil {
			task, err = h.Repo.ClaimNextAggregateControlTask(ctx, workerID, time.Now().UTC().Add(cfg.LeaseDuration), time.Now().UTC())
		}
		if err != nil || task == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(cfg.PollInterval):
				continue
			}
		}
		h.processL2Task(ctx, *task, cfg)
	}
}

func (h *Hub) processL2Task(ctx context.Context, task model.ControlTask, cfg L2Config) {
	start := time.Now()
	var err error
	if task.Status == "aggregating" {
		err = h.aggregateControlParent(ctx, task, start)
	} else {
		var triageCtx model.L2TriageContext
		triageCtx, err = h.buildL2TriageContext(ctx, task)
		if err == nil {
			err = h.dispatchL2Triage(ctx, task, triageCtx, cfg, start)
		}
	}
	duration := time.Since(start)
	if err == nil {
		return
	}
	_ = h.Repo.RecordControlTaskL2Failure(ctx, task.ID, err.Error(), duration)
	var retried *model.ControlTask
	var retryErr error
	if task.Status == "aggregating" {
		retried, retryErr = h.Repo.RetryAggregatingControlTask(ctx, task.ID, err.Error())
	} else {
		retried, retryErr = h.Repo.RetryL2ControlTask(ctx, task.ID, err.Error())
	}
	if retryErr != nil || retried == nil || retried.Status != "failed" {
		return
	}
	_ = h.notifyControlTask(ctx, *retried, controlTaskFailedReply(*retried))
}

func (h *Hub) buildL2TriageContext(ctx context.Context, task model.ControlTask) (model.L2TriageContext, error) {
	sessions, err := h.l2OnlineSessions(ctx, task.OwnerKey)
	if err != nil {
		return model.L2TriageContext{}, err
	}
	return model.L2TriageContext{
		RequestID:      task.ID,
		RawInput:       task.RawInput,
		Source:         task.Source,
		OwnerKey:       task.OwnerKey,
		OnlineSessions: sessions,
	}, nil
}

func (h *Hub) l2OnlineSessions(ctx context.Context, ownerKey string) ([]model.L2OnlineSession, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.L2OnlineSession
	for _, ep := range endpoints {
		if !ep.Active || ep.OwnerKey != ownerKey || ep.NoDirectory || !h.sessionOnline(ep.KeyID, ep.SessionName) {
			continue
		}
		out = append(out, model.L2OnlineSession{
			Session: ep.SessionName,
			Tool:    ep.Tool,
			Model:   ep.Model,
			Role:    ep.TargetGroup,
			Busy:    false,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out, nil
}

func (h *Hub) dispatchL2Triage(ctx context.Context, task model.ControlTask, input model.L2TriageContext, cfg L2Config, startedAt time.Time) error {
	system := strings.Join([]string{
		"你是 DingWei 平台总控 L2 分诊器。只返回 JSON。",
		"schema: {\"intent\":\"dispatch|clarify|reject|decompose|aggregate\",\"reply\":\"string\",\"targets\":[{\"session\":\"string\",\"instruction\":\"string\"}],\"subtasks\":[],\"confidence\":0.0}",
		"支持 dispatch、clarify、decompose；信息不足或低置信度返回 clarify。",
		"dispatch 必须且只能选择一个 online_sessions 中存在的 session；decompose 的 subtasks 必须全部指向 online_sessions。",
	}, "\n")
	userBytes, _ := json.Marshal(input)
	callCtx, cancel := context.WithTimeout(ctx, cfg.ProviderTimeout)
	defer cancel()
	out, err := h.L2.Complete(callCtx, system, string(userBytes))
	if err != nil {
		return err
	}
	result, err := parseL2TriageResult(out)
	if err != nil {
		return err
	}
	if result.Confidence < cfg.ConfidenceThreshold {
		result.Intent = "clarify"
		if strings.TrimSpace(result.Reply) == "" {
			result.Reply = "我还不确定该派给谁，请补充目标会话或更具体的任务背景。"
		}
	}
	switch result.Intent {
	case "dispatch":
		return h.completeL2Dispatch(ctx, task, result, startedAt)
	case "decompose":
		return h.completeL2Decompose(ctx, task, result)
	case "clarify":
		return h.completeL2Clarify(ctx, task, result, startedAt)
	default:
		result.Intent = "clarify"
		result.Reply = "该任务类型暂不支持自动分解，请指定一个明确会话或补充目标。"
		return h.completeL2Clarify(ctx, task, result, startedAt)
	}
}

func (h *Hub) completeL2Decompose(ctx context.Context, task model.ControlTask, result model.L2TriageResult) error {
	if len(result.Subtasks) == 0 {
		result.Intent = "clarify"
		result.Reply = "我无法拆出明确子任务，请补充目标和分工。"
		return h.completeL2Clarify(ctx, task, result, time.Now())
	}
	targetJSON, _ := json.Marshal(result.Subtasks)
	parent := task
	parent.Target = string(targetJSON)
	var children []model.ControlTask
	now := time.Now().UTC()
	for i, sub := range result.Subtasks {
		sub.Session = strings.TrimSpace(sub.Session)
		sub.Instruction = strings.TrimSpace(sub.Instruction)
		if sub.Session == "" || sub.Instruction == "" {
			return fmt.Errorf("invalid subtask %d", i+1)
		}
		childID := fmt.Sprintf("%s-sub-%02d", task.ID, i+1)
		childTarget, _ := json.Marshal([]model.L2Target{sub})
		children = append(children, model.ControlTask{
			ID:           childID,
			ParentID:     task.ID,
			CreatedAt:    now.Add(time.Duration(i) * time.Nanosecond),
			UpdatedAt:    now,
			Source:       "session",
			SourceAddr:   "",
			OwnerKey:     task.OwnerKey,
			BotChannelID: task.BotChannelID,
			RawInput:     sub.Instruction,
			Intent:       "dispatch",
			Layer:        "L2",
			Target:       string(childTarget),
			Status:       "awaiting_result",
			Priority:     task.Priority,
			MaxAttempts:  task.MaxAttempts,
			ExpireAt:     task.ExpireAt,
		})
	}
	if err := h.Repo.CreateControlSubtasks(ctx, parent, children); err != nil {
		return err
	}
	for i, child := range children {
		if err := h.routeL2Target(ctx, child, result.Subtasks[i]); err != nil {
			_, _ = h.Repo.CompleteControlSubtask(ctx, child.ID, "", "failed", err.Error())
		}
	}
	_, err := h.tryAggregateParent(ctx, task.ID)
	return err
}

func (h *Hub) CompleteControlSubtask(ctx context.Context, id, result, status, errText string) error {
	child, err := h.Repo.CompleteControlSubtask(ctx, id, result, status, errText)
	if err != nil || child == nil || child.ParentID == "" {
		return err
	}
	_, err = h.tryAggregateParent(ctx, child.ParentID)
	return err
}

func (h *Hub) tryAggregateParent(ctx context.Context, parentID string) (*model.ControlTask, error) {
	parent, err := h.Repo.TryClaimControlParentForAggregation(ctx, parentID, "aggregate-"+randomHex(4), time.Now().UTC().Add(h.effectiveL2Config().LeaseDuration), time.Now().UTC())
	if err != nil || parent == nil {
		return parent, err
	}
	err = h.aggregateControlParent(ctx, *parent, time.Now())
	if err != nil {
		retried, retryErr := h.Repo.RetryAggregatingControlTask(ctx, parent.ID, err.Error())
		if retryErr != nil {
			return parent, retryErr
		}
		if retried != nil && retried.Status == "failed" {
			_ = h.notifyControlTask(ctx, *retried, controlTaskFailedReply(*retried))
		}
		return parent, err
	}
	return parent, nil
}

func (h *Hub) aggregateControlParent(ctx context.Context, parent model.ControlTask, startedAt time.Time) error {
	children, err := h.Repo.ListControlSubtasks(ctx, parent.ID)
	if err != nil {
		return err
	}
	input := map[string]any{
		"request_id": parent.ID,
		"intent":     "aggregate",
		"raw_input":  parent.RawInput,
		"owner_key":  parent.OwnerKey,
		"children":   children,
	}
	system := "你是 DingWei 平台总控聚合器。只返回 JSON: {\"intent\":\"aggregate\",\"reply\":\"给发起人的综合回复\",\"confidence\":0.0}。即使部分子任务 failed/expired，也要说明完成与失败情况。"
	userBytes, _ := json.Marshal(input)
	callCtx, cancel := context.WithTimeout(ctx, h.effectiveL2Config().ProviderTimeout)
	defer cancel()
	out, err := h.L2.Complete(callCtx, system, string(userBytes))
	if err != nil {
		return err
	}
	result, err := parseL2TriageResult(out)
	if err != nil {
		return err
	}
	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		reply = aggregateFallbackReply(children)
	}
	if err := h.Repo.CompleteControlTaskL2(ctx, parent.ID, "aggregate", parent.Target, reply, time.Since(startedAt)); err != nil {
		return err
	}
	return h.notifyControlTask(ctx, model.ControlTask{ID: parent.ID, Status: "done", SourceAddr: parent.SourceAddr, BotChannelID: parent.BotChannelID}, reply)
}

func aggregateFallbackReply(children []model.ControlTask) string {
	done, failed := 0, 0
	for _, child := range children {
		switch child.Status {
		case "done":
			done++
		case "failed", "expired":
			failed++
		}
	}
	if failed > 0 {
		return fmt.Sprintf("子任务已完成 %d 个，失败/超时 %d 个，请查看详情。", done, failed)
	}
	return fmt.Sprintf("子任务已全部完成，共 %d 个。", done)
}

func parseL2TriageResult(raw string) (model.L2TriageResult, error) {
	var result model.L2TriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return result, err
	}
	result.Intent = strings.TrimSpace(result.Intent)
	for i := range result.Targets {
		result.Targets[i].Session = strings.TrimSpace(result.Targets[i].Session)
		result.Targets[i].Instruction = strings.TrimSpace(result.Targets[i].Instruction)
	}
	return result, nil
}

func (h *Hub) completeL2Dispatch(ctx context.Context, task model.ControlTask, result model.L2TriageResult, completedAt time.Time) error {
	if len(result.Targets) != 1 || result.Targets[0].Session == "" || result.Targets[0].Instruction == "" {
		result.Intent = "clarify"
		result.Reply = "我还不能确定唯一目标，请用 #会话名 内容 指定。"
		return h.completeL2Clarify(ctx, task, result, completedAt)
	}
	target := result.Targets[0]
	if err := h.routeL2Target(ctx, task, target); err != nil {
		return err
	}
	targetJSON, _ := json.Marshal(result.Targets)
	reply := result.Reply
	if strings.TrimSpace(reply) == "" {
		reply = fmt.Sprintf("已分诊给 #%s。", target.Session)
	}
	if err := h.Repo.CompleteControlTaskL2(ctx, task.ID, "dispatch", string(targetJSON), reply, time.Since(completedAt)); err != nil {
		return err
	}
	return h.notifyControlTask(ctx, model.ControlTask{ID: task.ID, Status: "done", SourceAddr: task.SourceAddr, BotChannelID: task.BotChannelID}, reply)
}

func (h *Hub) completeL2Clarify(ctx context.Context, task model.ControlTask, result model.L2TriageResult, completedAt time.Time) error {
	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		reply = "请补充目标会话或更具体的任务背景。"
	}
	if err := h.Repo.CompleteControlTaskL2(ctx, task.ID, "clarify", "", reply, time.Since(completedAt)); err != nil {
		return err
	}
	return h.notifyControlTask(ctx, model.ControlTask{ID: task.ID, Status: "done", SourceAddr: task.SourceAddr, BotChannelID: task.BotChannelID}, reply)
}

func (h *Hub) routeL2Target(ctx context.Context, task model.ControlTask, target model.L2Target) error {
	keyID, found, err := h.l2SessionKey(ctx, task.OwnerKey, target.Session)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("target session %s is not online for owner %s", target.Session, task.OwnerKey)
	}
	env := model.Envelope{
		ID:   task.ID + "-l2-dispatch",
		To:   sessionAddress(target.Session, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: target.Instruction,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"system": true, "control_task_id": task.ID},
	}
	return h.RouteEnvelope(ctx, env)
}

func (h *Hub) l2SessionKey(ctx context.Context, ownerKey, sessionName string) (string, bool, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", false, err
	}
	for _, ep := range endpoints {
		if !ep.Active || ep.OwnerKey != ownerKey || ep.SessionName != sessionName || ep.NoDirectory || !h.sessionOnline(ep.KeyID, ep.SessionName) {
			continue
		}
		return ep.KeyID, true, nil
	}
	return "", false, nil
}

func (h *Hub) RegisterBot(botChannelID, botName string) {
	if botName == "" {
		botName = botChannelID
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.botChannels[botName] = botChannelID
	h.botNames[botChannelID] = botName
}

func (h *Hub) UpsertService(ctx context.Context, svc model.RegisteredService) error {
	if svc.DeliveryType == "" {
		svc.DeliveryType = "ws"
	}
	if svc.ReplyMode == "" {
		svc.ReplyMode = "sync"
	}
	if !svc.Enabled {
		svc.Enabled = true
	}
	return h.Repo.UpsertRegisteredService(ctx, svc)
}

func (h *Hub) IssueAPIKey(ctx context.Context, serviceID, label string) (secret string, meta model.ServiceAPIKey, err error) {
	secret, meta = h.newAPIKey(serviceID, label)
	return secret, meta, h.Repo.InsertServiceAPIKey(ctx, meta)
}

func (h *Hub) newAPIKey(serviceID, label string) (secret string, meta model.ServiceAPIKey) {
	secret = "wp_" + randomHex(32)
	meta = model.ServiceAPIKey{
		ID:        newKeyID(label, serviceID),
		ServiceID: serviceID,
		KeyHash:   HashAPIKey(secret),
		Label:     label,
		Active:    true,
		CreatedAt: time.Now().UTC(),
	}
	return secret, meta
}

func HashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func (h *Hub) BindAccount(ctx context.Context, keyID, chatEntityID string) error {
	return h.Repo.BindAPIKeyAccount(ctx, keyID, chatEntityID)
}

func (h *Hub) AddPrefixRule(ctx context.Context, rule model.RoutingRule) error {
	rule.MatchType = "prefix"
	if rule.Combine == "" {
		rule.Combine = "or"
	}
	if !rule.Enabled {
		rule.Enabled = true
	}
	if err := validatePrefixRule(rule); err != nil {
		return err
	}
	if err := h.ensureNoPrefixOverlap(ctx, rule); err != nil {
		return err
	}
	return h.Repo.InsertRoutingRule(ctx, rule)
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	serviceID := r.PathValue("serviceID")
	key := r.URL.Query().Get("api_key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	meta, err := h.Repo.ResolveServiceAPIKey(r.Context(), HashAPIKey(key))
	if err != nil || meta == nil || meta.ServiceID != serviceID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	c := &client{conn: conn}
	h.mu.Lock()
	if old := h.serviceClients[serviceID]; old != nil {
		_ = old.conn.Close(websocket.StatusPolicyViolation, "replaced")
	}
	h.serviceClients[serviceID] = c
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		if h.serviceClients[serviceID] == c {
			delete(h.serviceClients, serviceID)
		}
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}()
	<-r.Context().Done()
}

func (h *Hub) HandleSessionWS(w http.ResponseWriter, r *http.Request) {
	requestedSessionName := strings.TrimSpace(r.PathValue("sessionName"))
	if requestedSessionName == "" || strings.Contains(requestedSessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	keyID := strings.TrimSpace(r.URL.Query().Get("key_id"))
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if keyID == "" || strings.TrimSpace(secret) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	secretHash := HashAPIKey(secret)
	meta, err := h.Repo.ResolveServiceAPISecret(r.Context(), keyID, secretHash)
	if err != nil || meta == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	accounts, err := h.Repo.ListAPIKeyAccounts(r.Context(), meta.ID)
	if err != nil {
		http.Error(w, "load key accounts failed", http.StatusInternalServerError)
		return
	}
	clientIP := clientIPFromRequest(r)
	tool := strings.TrimSpace(r.URL.Query().Get("tool"))
	modelName := strings.TrimSpace(r.URL.Query().Get("model"))
	fullSessionName := strings.TrimSpace(r.URL.Query().Get("full_session_name"))
	producer := truthyQuery(r.URL.Query().Get("producer"))
	noDirectory := truthyQuery(r.URL.Query().Get("no_directory"))
	targetGroup := strings.TrimSpace(r.URL.Query().Get("target_group"))
	targetBot := strings.TrimSpace(r.URL.Query().Get("target_bot"))
	webTerminal := truthyQuery(r.URL.Query().Get("terminal"))
	osName := strings.TrimSpace(r.URL.Query().Get("os"))
	mirrorTo := sessionMirrorToFromRequest(r)
	ownerKey := h.ownerKeyForKeyWithAccounts(r.Context(), keyID, accounts)
	sessionName := deriveRegisteredSessionName(requestedSessionName, keyID, ownerKey)
	nameWarn := sessionNameRequestWarning(requestedSessionName, keyID, ownerKey)
	switch sessionNameEnforceMode() {
	case "enforce":
		if nameWarn != "" {
			http.Error(w, nameWarn, http.StatusBadRequest)
			return
		}
	case "warn":
		if nameWarn != "" {
			log.Printf("session name warning key_id=%s owner=%s session=%s: %s", keyID, ownerKey, requestedSessionName, nameWarn)
		}
	}
	sessionName, err = h.registerSessionEndpoint(r.Context(), model.SessionEndpoint{
		KeyID:           keyID,
		SessionName:     sessionName,
		FullSessionName: fullSessionName,
		OwnerKey:        ownerKey,
		LastSeenAt:      time.Now().UTC(),
		Active:          true,
		ClientIP:        clientIP,
		Tool:            tool,
		Model:           modelName,
		Producer:        producer,
		TargetGroup:     targetGroup,
		NoDirectory:     noDirectory,
		MirrorEnabled:   mirrorTo != "",
		MirrorTo:        mirrorTo,
	})
	if err != nil {
		http.Error(w, "save session endpoint failed", http.StatusInternalServerError)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(16 << 20) // 终端 PTY 全屏输出可能远超默认 32KB
	c := &sessionClient{conn: conn, keyID: keyID, sessionName: sessionName, targetBot: targetBot, webTerminal: webTerminal, osName: osName}
	h.mu.Lock()
	if h.sessionClients[keyID] == nil {
		h.sessionClients[keyID] = map[string]*sessionClient{}
	}
	if old := h.sessionClients[keyID][sessionName]; old != nil {
		_ = old.conn.Close(websocket.StatusPolicyViolation, "replaced")
	}
	h.sessionClients[keyID][sessionName] = c
	h.keyAccounts[keyID] = stringSet(accounts)
	reSize := h.terminalLastSize[terminalKey(keyID, sessionName)]
	h.mu.Unlock()
	if reSize[0] > 0 && reSize[1] > 0 {
		_ = c.write(r.Context(), model.Envelope{
			ID:   randomHex(16),
			To:   sessionAddress(sessionName, keyID),
			From: sessionAddress("workpulse", keyID),
			TS:   time.Now().Unix(),
			Meta: map[string]any{"type": terminalResizeType, "system": true, "no_mirror": true, "cols": reSize[0], "rows": reSize[1]},
		})
	}
	skillCtx, skillCancel := context.WithCancel(context.Background())
	c.skillCancel = skillCancel
	h.startAgentNetworkSkillPush(skillCtx, c)
	h.scheduleOnlineBroadcastForSession(r.Context(), keyID, sessionName)
	defer func() {
		if c.skillCancel != nil {
			c.skillCancel()
		}
		ownerCtx, ownerCancel := context.WithTimeout(context.Background(), time.Second)
		ownerKey := h.ownerKeyForKey(ownerCtx, keyID)
		ownerCancel()
		h.mu.Lock()
		current := h.sessionClients[keyID][sessionName] == c
		var terminalViewers []*terminalViewer
		if current {
			delete(h.sessionClients[keyID], sessionName)
			if len(h.sessionClients[keyID]) == 0 {
				delete(h.sessionClients, keyID)
				delete(h.keyAccounts, keyID)
			}
			terminalViewers = h.closeTerminalLocked(keyID, sessionName)
			h.closeFeishuSyncLocked(keyID, sessionName)
		}
		h.mu.Unlock()
		for _, viewer := range terminalViewers {
			_ = terminalWrite(context.Background(), viewer, map[string]any{"type": "status", "readonly": true, "message": "会话已离线"})
			_ = viewer.conn.Close(websocket.StatusNormalClosure, "session offline")
		}
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
		if !current {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = h.Repo.UpsertSessionEndpoint(cleanupCtx, model.SessionEndpoint{
			KeyID:       keyID,
			SessionName: sessionName,
			LastSeenAt:  time.Now().UTC(),
			Active:      false,
		})
		if ownerKey != "" {
			h.scheduleOnlineBroadcastForSession(context.Background(), keyID, sessionName)
		}
	}()
	for {
		_, data, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		var env model.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, "", "信封解析失败："+err.Error()))
			continue
		}
		if env.From == "" {
			env.From = sessionAddress(sessionName, keyID)
		}
		if env.ID == "" {
			env.ID = randomHex(16)
		}
		if env.TS == 0 {
			env.TS = time.Now().Unix()
		}
		if c.targetBot != "" {
			if env.Meta == nil {
				env.Meta = map[string]any{}
			}
			if isProducerEnvelope(env) && metaString(env.Meta, "target_bot") == "" {
				env.Meta["target_bot"] = c.targetBot
			}
		}
		if env.From == sessionAddress(requestedSessionName, keyID) && requestedSessionName != sessionName {
			env.From = sessionAddress(sessionName, keyID)
		}
		if env.From != sessionAddress(sessionName, keyID) {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, "from 地址必须等于当前会话地址"))
			continue
		}
		if h.handleAgentNetworkSkillAck(c, env) {
			continue
		}
		if h.handleTerminalOutputEnvelope(c, env) {
			continue
		}
		if h.routeSessionSelectorEnvelope(r.Context(), c, env) {
			continue
		}
		if h.handleControlSubtaskResult(r.Context(), c, env) {
			continue
		}
		if h.handleProvisionAck(r.Context(), c, env) {
			continue
		}
		if err := h.RouteEnvelope(r.Context(), env); err != nil {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, "投递失败："+err.Error()))
		}
	}
}

func (h *Hub) handleProvisionAck(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "provision_ack" {
		return false
	}
	if h.Repo != nil {
		target, _ := json.Marshal(map[string]any{
			"from":         env.From,
			"action":       metaString(env.Meta, "action"),
			"target":       metaString(env.Meta, "target"),
			"version":      metaString(env.Meta, "version"),
			"ok":           metaBool(env.Meta, "ok"),
			"message":      metaString(env.Meta, "message"),
			"from_version": metaString(env.Meta, "from_version"),
			"to_version":   metaString(env.Meta, "to_version"),
		})
		_ = h.Repo.WriteAudit(ctx, env.From, "provision_ack", string(target))
	}
	return true
}

func (h *Hub) handleControlSubtaskResult(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	taskID := metaString(env.Meta, "control_task_id")
	if taskID == "" {
		return false
	}
	task, err := h.Repo.GetControlTask(ctx, taskID)
	if err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "子任务查询失败："+err.Error()))
		return true
	}
	if task == nil || task.ParentID == "" {
		return false
	}
	if err := h.CompleteControlSubtask(ctx, taskID, env.Body, "done", ""); err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "子任务回填失败："+err.Error()))
	}
	return true
}

func sessionMirrorToFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("mirror_to"))
}

func (h *Hub) registerSessionEndpoint(ctx context.Context, ep model.SessionEndpoint) (string, error) {
	requested := ep.SessionName
	if ep.OwnerKey == "" {
		return requested, h.Repo.UpsertSessionEndpoint(ctx, ep)
	}
	for attempt := 0; attempt < 100; attempt++ {
		candidate, err := h.nextSessionNameForOwner(ctx, ep.OwnerKey, ep.KeyID, requested)
		if err != nil {
			return "", err
		}
		if attempt > 0 {
			candidate = suffixedSessionName(requested, attempt+1)
		}
		ep.SessionName = candidate
		if err := h.Repo.UpsertSessionEndpoint(ctx, ep); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				continue
			}
			return "", err
		}
		return candidate, nil
	}
	return "", errors.New("allocate session name failed")
}

func (h *Hub) nextSessionNameForOwner(ctx context.Context, ownerKey, keyID, requested string) (string, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for _, ep := range endpoints {
		if !ep.Active || ep.OwnerKey != ownerKey {
			continue
		}
		if ep.KeyID == keyID && ep.SessionName == requested {
			return requested, nil
		}
		used[ep.SessionName] = true
	}
	if !used[requested] {
		return requested, nil
	}
	for i := 2; ; i++ {
		candidate := suffixedSessionName(requested, i)
		if !used[candidate] {
			return candidate, nil
		}
	}
}

func suffixedSessionName(base string, n int) string {
	return fmt.Sprintf("%s%d", base, n)
}

func deriveRegisteredSessionName(requested, keyID, ownerKey string) string {
	requested = strings.TrimSpace(requested)
	if ownerKey == "" {
		return requested
	}
	short := requested
	if sessionNamePattern.MatchString(requested) {
		short = strings.Split(requested, "-")[1]
	}
	if !bareSessionNamePattern.MatchString(short) {
		return requested
	}
	return fmt.Sprintf("%s-%s-%s", ownerKey, short, keyTail(keyID))
}

// sessionNameRequestWarning 判定客户端上报名本身是否合规:B 方案裸短名合规,
// 完整名须 owner_key 与 key 末4位都与该 key 绑定成员一致。返回非空即触发 warn/enforce。
func sessionNameRequestWarning(requested, keyID, ownerKey string) string {
	requested = strings.TrimSpace(requested)
	ownerKey = strings.TrimSpace(ownerKey)
	if ownerKey == "" {
		return "无法确认该 key 绑定成员 owner_key"
	}
	if bareSessionNamePattern.MatchString(requested) {
		return ""
	}
	return sessionNamePolicyWarning(requested, keyID, ownerKey)
}

func (h *Hub) Dispatch(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, error) {
	if h.Repo == nil {
		return h.dispatchL1Direct(ctx, msg, text)
	}
	for _, expired := range h.reapExpiredControlTasks(ctx) {
		_ = h.notifyControlTask(ctx, expired, controlTaskExpiredReply(expired))
	}
	task, inserted, err := h.Repo.EnqueueControlTask(ctx, h.newControlTask(ctx, msg, text))
	if err != nil {
		return model.PrefixDispatchResult{}, err
	}
	ack := fmt.Sprintf("已受理 #%s", task.ID)
	if !inserted && task.Status != "done" {
		return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
	}
	if !inserted && task.Status == "done" {
		if task.Result == "" {
			return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: task.Result}, nil
	}
	result, intent, target, err := h.dispatchL1(ctx, msg, text)
	if err != nil {
		retried, retryErr := h.Repo.RetryControlTask(ctx, task.ID, err.Error())
		if retryErr != nil {
			return result, retryErr
		}
		if retried != nil && retried.Status == "failed" {
			reply := controlTaskFailedReply(*retried)
			_ = h.notifyControlTask(ctx, *retried, reply)
			return model.PrefixDispatchResult{Matched: true, Reply: reply}, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
	}
	status := "llm_pending"
	layer := "L1"
	resultText := result.Reply
	errText := ""
	if result.Matched {
		status = "done"
	} else if intent == "" {
		intent = "unknown"
	}
	if err := h.Repo.UpdateControlTaskAfterL1(ctx, task.ID, intent, layer, target, resultText, status, errText); err != nil {
		return model.PrefixDispatchResult{}, err
	}
	if result.Matched {
		return result, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
}

func (h *Hub) dispatchL1Direct(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, error) {
	result, _, _, err := h.dispatchL1LegacyFallback(ctx, msg, text)
	return result, err
}

func (h *Hub) dispatchL1(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, string, string, error) {
	if result, handled, err := h.dispatchSecurityOps(ctx, msg, text); handled || err != nil {
		return result, "command.security", "", err
	}
	if result, handled, err := h.dispatchFeishuSyncCommand(ctx, msg, text); handled || err != nil {
		return result, "command.sync", "", err
	}
	if result, handled, err := h.dispatchApplyKeyCommand(ctx, msg, text); handled || err != nil {
		return result, "command.apply_key", "", err
	}
	if result, handled, err := h.dispatchSystem(ctx, msg, text); handled || err != nil {
		return result, "command.system", "", err
	}
	if result, handled, err := h.dispatchAggregateWeeklyReview(ctx, msg, text); handled || err != nil {
		return result, "command.aggregate_weekly_review", "", err
	}
	rules, err := h.Repo.ListL1DecisionRules(ctx)
	if err != nil {
		return model.PrefixDispatchResult{}, "", "", err
	}
	for _, rule := range rules {
		matched, err := h.l1RuleMatches(ctx, rule, msg, text)
		if err != nil {
			return model.PrefixDispatchResult{}, "", "", err
		}
		if !matched {
			continue
		}
		result, target, handled, err := h.executeL1Rule(ctx, rule, msg, text)
		if err != nil {
			return result, rule.Intent, target, err
		}
		if handled {
			return result, rule.Intent, target, nil
		}
		if !rule.ExitQueue {
			return model.PrefixDispatchResult{}, rule.Intent, target, nil
		}
	}
	return model.PrefixDispatchResult{}, "unknown", "", nil
}

func (h *Hub) executeL1Rule(ctx context.Context, rule model.L1DecisionRule, msg model.Message, text string) (model.PrefixDispatchResult, string, bool, error) {
	switch rule.ID {
	case "l1_command_terminal_input":
		result, handled, err := h.dispatchTerminalInputCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_command_roster":
		result, handled, err := h.dispatchOnlineRosterCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_command_apply_key":
		result, handled, err := h.dispatchApplyKeyCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_command_mirror":
		result, handled, err := h.dispatchMirrorCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_route_session":
		result, handled, err := h.dispatchSelector(ctx, msg, text)
		return result, "", handled, err
	case "l1_route_cross":
		result, handled, err := h.dispatchMemberMention(ctx, msg, text)
		return result, "", handled, err
	case "l1_route_default_single":
		return h.dispatchL1LegacyFallback(ctx, msg, text)
	case "l1_nl_dispatch", "l1_unknown":
		return h.dispatchL1LegacyFallback(ctx, msg, text)
	case "l1_decompose":
		return model.PrefixDispatchResult{}, "", false, nil
	default:
		return h.dispatchL1LegacyFallback(ctx, msg, text)
	}
}

func (h *Hub) dispatchL1LegacyFallback(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, string, bool, error) {
	if result, handled, err := h.dispatchSecurityOps(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchFeishuSyncCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchApplyKeyCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchTerminalInputCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchSystem(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchAggregateWeeklyReview(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchMirrorCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchMemberMention(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchSelector(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	routes, err := h.Repo.ListPrefixRoutes(ctx, msg.ChatEntityID)
	if err != nil {
		return model.PrefixDispatchResult{}, "", false, err
	}
	for _, route := range routes {
		if !router.MatchPrefix(route.Rule.MatchExpr, text, route.Rule.CaseSensitive) {
			continue
		}
		forwardText := text
		if route.Rule.StripPrefix {
			forwardText = stripPrefix(route.Rule.MatchExpr, text, route.Rule.CaseSensitive)
		}
		if route.Service.DeliveryType == "session" {
			keyID, sessionName, ok := sessionServiceParts(route.Service.ID)
			if !ok {
				continue
			}
			env := model.Envelope{
				ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
				To:   sessionAddress(sessionName, keyID),
				From: feishuAddress(feishuOpenIDFromAccount(sourceAccount(msg)), keyID, h.botName(msg.BotChannelID)),
				Body: forwardText,
				TS:   time.Now().Unix(),
				Meta: map[string]any{
					"chat_type":      string(msg.ChatType),
					"raw":            msg.Content,
					"bot_channel_id": msg.BotChannelID,
					"route_id":       route.Rule.ID,
				},
			}
			h.annotateSourceContext(env.Meta, msg)
			if msg.ChatType == model.ChatGroup {
				env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
				env.Meta["sender_open_id"] = msg.SenderOpenID
			}
			if err := h.RouteEnvelope(ctx, env); err != nil {
				return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, "", true, nil
			}
			return model.PrefixDispatchResult{Matched: true}, route.Service.ID, true, nil
		}
		if route.Service.DeliveryType != "ws" {
			continue
		}
		reply, err := h.forward(ctx, route.Service.ID, ForwardRequest{
			ID:           msg.ID,
			ServiceID:    route.Service.ID,
			ChatEntityID: msg.ChatEntityID,
			BotChannelID: msg.BotChannelID,
			ChatType:     string(msg.ChatType),
			SenderOpenID: msg.SenderOpenID,
			Text:         forwardText,
			RawContent:   msg.Content,
		}, route.Service.TimeoutMs)
		if err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, route.Service.ID, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: reply}, route.Service.ID, true, nil
	}
	if result, handled, err := h.dispatchDefaultPersonalSession(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	return model.PrefixDispatchResult{}, "", false, nil
}

func (h *Hub) newControlTask(ctx context.Context, msg model.Message, text string) model.ControlTask {
	now := time.Now().UTC()
	sourceAddr := controlSourceAddr(msg, h.botName(msg.BotChannelID))
	ownerKey := h.controlOwnerKey(ctx, msg)
	expireAt := now.Add(5 * time.Minute)
	return model.ControlTask{
		ID:           firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
		CreatedAt:    now,
		UpdatedAt:    now,
		Source:       controlSource(msg),
		SourceAddr:   sourceAddr,
		OwnerKey:     ownerKey,
		BotChannelID: msg.BotChannelID,
		RawInput:     text,
		Status:       "queued",
		Priority:     0,
		MaxAttempts:  3,
		ExpireAt:     &expireAt,
	}
}

func (h *Hub) reapExpiredControlTasks(ctx context.Context) []model.ControlTask {
	if h.Repo == nil {
		return nil
	}
	expired, err := h.Repo.ReapExpiredControlTasks(ctx, time.Now().UTC())
	if err != nil {
		return nil
	}
	return expired
}

func (h *Hub) notifyControlTask(ctx context.Context, task model.ControlTask, reply string) error {
	if strings.TrimSpace(task.SourceAddr) == "" || strings.TrimSpace(reply) == "" {
		return nil
	}
	to, err := parseAddress(task.SourceAddr)
	if err != nil {
		return err
	}
	if to.Kind == addressFeishu {
		if h.Outbound == nil {
			return nil
		}
		chatType := model.ChatPersonal
		entityKind := "personal"
		if strings.HasPrefix(to.OpenID, "oc_") {
			chatType = model.ChatGroup
			entityKind = "group"
		}
		botChannelID := firstNonEmpty(task.BotChannelID, h.botChannel(to.BotName), to.KeyID)
		content, _ := json.Marshal(map[string]string{"text": reply})
		return h.Outbound.Enqueue(ctx, model.Message{
			ID:           "control-" + task.Status + "-" + task.ID,
			ChatEntityID: botChannelID + ":" + entityKind + ":" + to.OpenID,
			BotChannelID: botChannelID,
			FeishuMsgID:  "control-" + task.Status + "-" + task.ID,
			ChatType:     chatType,
			Content:      string(content),
		})
	}
	env := model.Envelope{
		ID:   "control-" + task.Status + "-" + task.ID,
		To:   task.SourceAddr,
		From: sessionAddress("workpulse", to.KeyID),
		Body: reply,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"system": true, "no_mirror": true, "source_bot_channel_id": task.BotChannelID},
	}
	return h.RouteEnvelope(ctx, env)
}

func controlTaskExpiredReply(task model.ControlTask) string {
	return fmt.Sprintf("任务 #%s 已超时，请重新发起或补充更明确的目标。", task.ID)
}

func controlTaskFailedReply(task model.ControlTask) string {
	if strings.TrimSpace(task.Error) == "" {
		return fmt.Sprintf("任务 #%s 处理失败，请稍后重试。", task.ID)
	}
	return fmt.Sprintf("任务 #%s 处理失败：%s", task.ID, task.Error)
}

func (h *Hub) l1RuleMatches(ctx context.Context, rule model.L1DecisionRule, msg model.Message, text string) (bool, error) {
	text = strings.TrimSpace(h.stripLeadingBotMentions(msg.BotChannelID, text))
	switch rule.MatchType {
	case "prefix":
		return strings.HasPrefix(text, rule.Pattern), nil
	case "prefix_any":
		for _, p := range splitRulePattern(rule.Pattern) {
			if strings.HasPrefix(text, p) {
				return true, nil
			}
		}
		return false, nil
	case "keyword_any":
		for _, p := range splitRulePattern(rule.Pattern) {
			if strings.Contains(text, p) {
				return true, nil
			}
		}
		return false, nil
	case "regex":
		return regexp.MatchString(rule.Pattern, text)
	case "default":
		source := sourceAccount(msg)
		_, sessions, err := h.onlineSessionsForAccount(ctx, source)
		if err != nil {
			return false, err
		}
		switch rule.Pattern {
		case "personal_single_online_session":
			return msg.ChatType == model.ChatPersonal && len(sessions) == 1 && !looksPrefixed(text), nil
		case "personal_multiple_online_sessions":
			return msg.ChatType == model.ChatPersonal && len(sessions) > 1 && !looksPrefixed(text), nil
		default:
			return false, nil
		}
	case "fallback":
		return true, nil
	default:
		return false, nil
	}
}

func (h *Hub) dispatchOnlineRosterCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	text = strings.TrimSpace(h.stripLeadingBotMentions(msg.BotChannelID, text))
	if text != "#在线" && text != "#roster" && text != "#清单" {
		return model.PrefixDispatchResult{}, false, nil
	}
	ownerKey := h.controlOwnerKey(ctx, msg)
	items, owner, err := h.onlineDirectory(ctx, ownerKey)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if owner.OwnerKey == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到当前账号的在线清单。"}, true, nil
	}
	if text == "#清单" {
		code := h.issueOwnerLinkCode(owner.OwnerKey)
		return model.PrefixDispatchResult{Matched: true, Reply: renderOwnerLinksText(owner, items, code)}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: renderOnlineDirectory(owner, items)}, true, nil
}

func (h *Hub) controlOwnerKey(ctx context.Context, msg model.Message) string {
	source := sourceAccount(msg)
	if h.Repo == nil {
		return source
	}
	members, err := h.Repo.ListMembers(ctx)
	if err == nil {
		for _, member := range members {
			if member.Active && accountBelongsToMember(source, member) {
				return member.OwnerKey
			}
		}
	}
	if botChannelID, feishuID, ok := accountEntityParts(source); ok {
		if entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID); err == nil && entity != nil && strings.TrimSpace(entity.BoundOwner) != "" {
			return entity.BoundOwner
		}
	}
	return source
}

func controlSource(msg model.Message) string {
	if msg.BotChannelID != "" || msg.ChatEntityID != "" {
		return "feishu"
	}
	return "session"
}

func controlSourceAddr(msg model.Message, botName string) string {
	source := sourceAccount(msg)
	keyID := msg.BotChannelID
	if keyID == "" {
		keyID = "unknown"
	}
	if controlSource(msg) == "feishu" {
		return feishuAddress(feishuOpenIDFromAccount(source), keyID, botName)
	}
	return source
}

func splitRulePattern(pattern string) []string {
	var out []string
	for _, part := range strings.Split(pattern, "|") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func looksPrefixed(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "@")
}

func (h *Hub) dispatchSystem(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	text = h.stripLeadingBotMentions(msg.BotChannelID, text)
	routes, err := h.Repo.ListSystemRoutes(ctx)
	if err != nil {
		return model.PrefixDispatchResult{}, false, err
	}
	for _, route := range routes {
		if !strings.HasPrefix(text, route.Keyword) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(text, route.Keyword))
		if h.System == nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "系统服务未配置：" + route.ServiceName}, true, nil
		}
		reply, err := h.System.HandleSystemRequest(ctx, route.ServiceName, route.Action, body, msg)
		if err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "系统服务执行失败：" + err.Error()}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: reply}, true, nil
	}
	return model.PrefixDispatchResult{}, false, nil
}

func (h *Hub) dispatchAggregateWeeklyReview(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	if h.System == nil {
		return model.PrefixDispatchResult{}, false, nil
	}
	reviewer, ok := h.System.(aggregateWeeklyReviewHandler)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	body := h.stripLeadingBotMentions(msg.BotChannelID, text)
	reply, matched, err := reviewer.HandleAggregateWeeklyReviewCommand(ctx, body, msg)
	if !matched && err == nil {
		return model.PrefixDispatchResult{}, false, nil
	}
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "系统服务执行失败：" + err.Error()}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: reply}, true, nil
}

func (h *Hub) dispatchSecurityOps(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	targetName, _, body, ok := h.parseMemberMention(msg.BotChannelID, text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	target, err := h.resolveMemberByName(ctx, targetName)
	if err != nil || target.OwnerKey != secOpsOwnerKey {
		return model.PrefixDispatchResult{}, false, nil
	}
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, secOpsKeyword) {
		return model.PrefixDispatchResult{}, false, nil
	}
	command := strings.TrimSpace(strings.TrimPrefix(body, secOpsKeyword))
	if command == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "系统安全指令为空"}, true, nil
	}
	allowed, err := h.securityOpsAuthorized(ctx, msg)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if !allowed {
		return model.PrefixDispatchResult{Matched: true, Reply: "无权限"}, true, nil
	}
	targets, err := h.onlineSessionsForOwner(ctx, secOpsOwnerKey)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if len(targets) == 0 {
		return model.PrefixDispatchResult{Matched: true, Reply: "SYSTEM-V-TASK-INTERNAL 当前无在线 sec-ops 会话"}, true, nil
	}
	source := sourceAccount(msg)
	broadcastKey := broadcastDedupKey("sec-ops", firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)), secOpsOwnerKey)
	for i, target := range targets {
		envID := firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16))
		if len(targets) > 1 {
			envID = fmt.Sprintf("%s-%d", envID, i+1)
		}
		env := model.Envelope{
			ID:   envID,
			To:   sessionAddress(target.sessionName, target.keyID),
			From: feishuAddress(feishuOpenIDFromAccount(source), target.keyID, h.botName(msg.BotChannelID)),
			Body: command,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"chat_type":            string(msg.ChatType),
				"raw":                  msg.Content,
				"bot_channel_id":       msg.BotChannelID,
				"system_route":         "sec-ops",
				"system_keyword":       secOpsKeyword,
				"cross_member":         target.ownerKey,
				"cross_member_name":    secOpsMemberName,
				"cross_session_name":   target.sessionName,
				"reply_prefix":         fmt.Sprintf("【%s·%s】", secOpsMemberName, target.sessionName),
				"source_chat_entity":   msg.ChatEntityID,
				"source_sender_openid": msg.SenderOpenID,
			},
		}
		h.annotateSourceContext(env.Meta, msg)
		annotateBroadcastMirror(&env, broadcastKey, i == 0)
		if msg.ChatType == model.ChatGroup {
			env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
			env.Meta["sender_open_id"] = msg.SenderOpenID
		}
		if err := h.RouteEnvelope(ctx, env); err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "sec-ops 投递失败：" + err.Error()}, true, nil
		}
	}
	return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("已投递到%d个sec-ops会话", len(targets))}, true, nil
}

func (h *Hub) securityOpsAuthorized(ctx context.Context, msg model.Message) (bool, error) {
	if secOpsAdminOpenID != "" && msg.SenderOpenID == secOpsAdminOpenID {
		return true, nil
	}
	openID := firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(sourceAccount(msg)))
	if secOpsAdminOpenID != "" && msg.ChatType == model.ChatPersonal && openID == secOpsAdminOpenID {
		return true, nil
	}
	if secOpsAdminOwnerKey != "" {
		members, err := h.Repo.ListMembers(ctx)
		if err != nil {
			return false, err
		}
		for _, member := range members {
			if !member.Active || member.OwnerKey != secOpsAdminOwnerKey {
				continue
			}
			if openID != "" && member.FeishuOpenID == openID {
				return true, nil
			}
		}
		botChannelID, feishuID, ok := accountEntityParts(sourceAccount(msg))
		if !ok {
			return false, nil
		}
		entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID)
		if err != nil {
			return false, err
		}
		return entity != nil && entity.Active && entity.BoundOwner == secOpsAdminOwnerKey, nil
	}
	return false, nil
}

type ownerSessionTarget struct {
	keyID       string
	sessionName string
	ownerKey    string
}

func (h *Hub) onlineSessionsForOwner(ctx context.Context, ownerKey string) ([]ownerSessionTarget, error) {
	member, err := h.Repo.GetMemberByOwnerKey(ctx, ownerKey)
	if err != nil {
		return nil, err
	}
	if member == nil || !member.Active {
		return nil, nil
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	var out []ownerSessionTarget
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		belongs := ep.OwnerKey == ownerKey
		if !belongs {
			accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
			if err != nil {
				return nil, err
			}
			for _, account := range accounts {
				if accountBelongsToMember(account, *member) {
					belongs = true
					break
				}
			}
		}
		if !belongs || !h.sessionOnline(ep.KeyID, ep.SessionName) {
			continue
		}
		out = append(out, ownerSessionTarget{keyID: ep.KeyID, sessionName: ep.SessionName, ownerKey: ownerKey})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].keyID == out[j].keyID {
			return out[i].sessionName < out[j].sessionName
		}
		return out[i].keyID < out[j].keyID
	})
	return out, nil
}

func (h *Hub) sessionOnline(keyID, sessionName string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sessions := h.sessionClients[keyID]; sessions != nil {
		return sessions[sessionName] != nil
	}
	return false
}

func (h *Hub) dispatchMirrorCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	action, sessionName, ok := parseMirrorCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true}, true, nil
	}
	source := sourceAccount(msg)
	keyID, found, err := h.lookupKeyIDForMirror(ctx, source, sessionName)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if !found {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到可控制的会话，请确认会话已注册且当前账号已绑定。"}, true, nil
	}
	enabled := action == "on"
	mirrorTo := ""
	if enabled {
		mirrorTo = feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID))
	}
	if err := h.Repo.SetSessionMirror(ctx, keyID, sessionName, enabled, mirrorTo); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	delivered := true
	if err := h.RouteEnvelope(ctx, model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: "mirror " + action,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":      "mirror_control",
			"enabled":   enabled,
			"mirror_to": mirrorTo,
			"system":    true,
			"no_mirror": true,
		},
	}); err != nil {
		delivered = false
	}
	if enabled {
		if delivered {
			return model.PrefixDispatchResult{Matched: true, Reply: "镜像已开启：" + sessionName}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "镜像已开启并保存；会话当前离线，重连后生效：" + sessionName}, true, nil
	}
	if delivered {
		return model.PrefixDispatchResult{Matched: true, Reply: "镜像已关闭：" + sessionName}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "镜像已关闭并保存；会话当前离线：" + sessionName}, true, nil
}

func (h *Hub) dispatchFeishuSyncCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	action, sessionName, explicitKey, ok := parseSyncCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true}, true, nil
	}
	openID := firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(sourceAccount(msg)))
	if openID == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "无法识别发起人 open_id"}, true, nil
	}
	targetKey, found := "", false
	if explicitKey != "" {
		sourceOwner := h.controlOwnerKey(ctx, msg)
		targetOwner := h.ownerKeyForKey(ctx, explicitKey)
		if sourceOwner == "" || targetOwner == "" || sourceOwner != targetOwner {
			return model.PrefixDispatchResult{Matched: true, Reply: "无权同步他人会话"}, true, nil
		}
		if h.sessionOnline(explicitKey, sessionName) {
			targetKey, found = explicitKey, true
		}
	} else {
		ownerKey := h.controlOwnerKey(ctx, msg)
		targetKey, found = h.resolveOwnerSessionKey(ctx, ownerKey, sessionName, "")
	}
	if !found {
		if explicitKey == "" {
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到你名下在线会话：" + sessionName + "。同步他人会话请显式使用 #sync " + sessionName + "#<key>。"}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到在线会话：" + sessionName + "#" + explicitKey}, true, nil
	}
	target := feishuSyncTarget{
		openID:       openID,
		keyID:        targetKey,
		botName:      h.botName(msg.BotChannelID),
		botChannelID: msg.BotChannelID,
	}
	switch action {
	case "sync":
		for _, item := range h.addFeishuSyncTargetAndSnapshot(targetKey, sessionName, target) {
			if err := h.sendFeishuSyncItem(ctx, targetKey, sessionName, target, item); err != nil {
				return model.PrefixDispatchResult{}, true, err
			}
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "已开启同步 " + sessionName}, true, nil
	case "unsync":
		h.removeFeishuSyncTarget(targetKey, sessionName, target)
		return model.PrefixDispatchResult{Matched: true, Reply: "已停止同步 " + sessionName}, true, nil
	default:
		return model.PrefixDispatchResult{}, false, nil
	}
}

func (h *Hub) dispatchApplyKeyCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	cmd, arg, ok := parseApplyKeyCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if h.Repo == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂不可用：存储未配置"}, true, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true}, true, nil
	}
	switch cmd {
	case "apply":
		return h.createKeyApplication(ctx, msg, arg)
	case "approve":
		return h.approveKeyApplication(ctx, msg, arg)
	case "reject":
		id, reason := splitApplyReviewArg(arg)
		return h.rejectKeyApplication(ctx, msg, id, reason)
	default:
		return model.PrefixDispatchResult{}, false, nil
	}
}

func (h *Hub) createKeyApplication(ctx context.Context, msg model.Message, description string) (model.PrefixDispatchResult, bool, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "请补充申请说明，例如：#申请 接入 zzc-developer"}, true, nil
	}
	openID := applicantOpenID(msg)
	if openID == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "无法识别申请人 open_id"}, true, nil
	}
	approver := h.applyKeyApproverOpenID()
	if approver == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂未配置审批人"}, true, nil
	}
	botName := h.botName(msg.BotChannelID)
	app, err := h.Repo.CreateKeyApplication(ctx, model.KeyApplication{
		ApplicantOpenID:  openID,
		ApplicantAccount: msg.BotChannelID + ":personal:" + openID,
		ApplicantBotID:   msg.BotChannelID,
		ApplicantBotName: botName,
		Description:      description,
		Status:           "pending",
		CreatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	_ = h.Repo.WriteAudit(ctx, app.ApplicantOpenID, "key_application_create", app.ID)
	notice := fmt.Sprintf("收到新的 DingWei key 申请\nID：%s\n申请人：%s\n说明：%s\n\n批准：#批准%s\n拒绝：#拒绝%s 原因", app.ID, app.ApplicantOpenID, app.Description, app.ID, app.ID)
	if err := h.enqueueFeishuText(ctx, msg.BotChannelID, approver, "apply-key-review-"+app.ID, notice); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "已提交 key 申请，申请 ID：" + app.ID + "。审批通过后会私聊发放。"}, true, nil
}

func (h *Hub) approveKeyApplication(ctx context.Context, msg model.Message, id string) (model.PrefixDispatchResult, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "请提供申请 ID，例如：#批准<申请ID>"}, true, nil
	}
	approver := h.applyKeyApproverOpenID()
	if approver == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂未配置审批人"}, true, nil
	}
	if got := applicantOpenID(msg); got != approver {
		return model.PrefixDispatchResult{Matched: true, Reply: "只有指定审批人可以批准 key 申请"}, true, nil
	}
	app, err := h.Repo.GetKeyApplication(ctx, id)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if app == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到 key 申请：" + id}, true, nil
	}
	if app.Status != "pending" {
		return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("申请 %s 当前状态为 %s，不能重复审批", app.ID, app.Status)}, true, nil
	}
	if _, ok := h.Outbound.(sensitiveOutboundRememberer); !ok {
		return model.PrefixDispatchResult{}, true, errors.New("outbound queue does not support one-time secret delivery")
	}
	serviceID := applyKeyServiceID(app.ApplicantOpenID)
	secret, key := h.newAPIKey(serviceID, app.ApplicantOpenID)
	account := firstNonEmpty(app.ApplicantAccount, app.ApplicantBotID+":personal:"+app.ApplicantOpenID)
	grantID := "apply-key-grant-" + app.ID
	grantText := applyKeyGrantText(key.ID, secret)
	storedGrantText := applyKeyGrantStoredText(key.ID)
	grantContent, _ := json.Marshal(map[string]string{"text": storedGrantText})
	now := time.Now().UTC()
	h.rememberSensitiveOutbound(grantID, mustTextContent(grantText))
	if err := h.Repo.ApproveKeyApplicationWithGrant(ctx, app.ID, approver, model.RegisteredService{
		ID:           serviceID,
		Name:         "DingWei key " + app.ApplicantOpenID,
		Description:  "Self-service key application " + app.ID,
		DeliveryType: "ws",
		ReplyMode:    "sync",
		Enabled:      true,
	}, key, account, model.ChatEntity{
		ID:           account,
		BotChannelID: app.ApplicantBotID,
		Type:         model.ChatPersonal,
		FeishuID:     app.ApplicantOpenID,
		DisplayName:  app.ApplicantOpenID,
		Active:       true,
	}, model.Message{
		ID:           grantID,
		ChatEntityID: app.ApplicantBotID + ":personal:" + app.ApplicantOpenID,
		Direction:    model.DirectionOut,
		BotChannelID: app.ApplicantBotID,
		FeishuMsgID:  grantID,
		ChatType:     model.ChatPersonal,
		Content:      string(grantContent),
		Status:       "queued",
	}, now); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	_ = h.Repo.WriteAudit(ctx, approver, "key_application_approve", app.ID+":"+key.ID)
	return model.PrefixDispatchResult{Matched: true, Reply: "已批准申请 " + app.ID + "，key_id：" + key.ID + "。secret 已私聊发放给申请人。"}, true, nil
}

func (h *Hub) rejectKeyApplication(ctx context.Context, msg model.Message, id, reason string) (model.PrefixDispatchResult, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "请提供申请 ID，例如：#拒绝<申请ID> 原因"}, true, nil
	}
	approver := h.applyKeyApproverOpenID()
	if approver == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂未配置审批人"}, true, nil
	}
	if got := applicantOpenID(msg); got != approver {
		return model.PrefixDispatchResult{Matched: true, Reply: "只有指定审批人可以拒绝 key 申请"}, true, nil
	}
	app, err := h.Repo.GetKeyApplication(ctx, id)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if app == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到 key 申请：" + id}, true, nil
	}
	if app.Status != "pending" {
		return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("申请 %s 当前状态为 %s，不能重复审批", app.ID, app.Status)}, true, nil
	}
	now := time.Now().UTC()
	if err := h.Repo.RejectKeyApplication(ctx, app.ID, approver, strings.TrimSpace(reason), now); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	_ = h.Repo.WriteAudit(ctx, approver, "key_application_reject", app.ID)
	reply := "你的 DingWei key 申请已被拒绝。申请 ID：" + app.ID
	if strings.TrimSpace(reason) != "" {
		reply += "\n原因：" + strings.TrimSpace(reason)
	}
	if err := h.enqueueFeishuText(ctx, app.ApplicantBotID, app.ApplicantOpenID, "apply-key-reject-"+app.ID, reply); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "已拒绝申请 " + app.ID}, true, nil
}

func parseApplyKeyCommand(text string) (cmd, arg string, ok bool) {
	text = strings.TrimSpace(stripLeadingMentions(text))
	for _, item := range []struct {
		prefix string
		cmd    string
	}{
		{"#申请", "apply"},
		{"#批准", "approve"},
		{"#拒绝", "reject"},
	} {
		if strings.HasPrefix(text, item.prefix) {
			return item.cmd, strings.TrimSpace(strings.TrimPrefix(text, item.prefix)), true
		}
	}
	return "", "", false
}

func splitApplyReviewArg(arg string) (id, reason string) {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		return "", ""
	}
	id = fields[0]
	if len(fields) > 1 {
		reason = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(arg), id))
	}
	return id, reason
}

func applicantOpenID(msg model.Message) string {
	return firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(sourceAccount(msg)))
}

func (h *Hub) applyKeyApproverOpenID() string {
	return firstNonEmpty(strings.TrimSpace(applyKeyApproverID), strings.TrimSpace(secOpsAdminOpenID))
}

func applyKeyServiceID(openID string) string {
	return "apply:" + sanitizeKeyIDPart(openID)
}

func (h *Hub) enqueueFeishuText(ctx context.Context, botChannelID, openID, id, text string) error {
	return h.enqueueFeishuSensitiveText(ctx, botChannelID, openID, id, text, "")
}

func (h *Hub) enqueueFeishuSensitiveText(ctx context.Context, botChannelID, openID, id, text, storedText string) error {
	if h.Outbound == nil {
		return errors.New("outbound queue not configured")
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	storedContent := string(content)
	sensitiveContent := ""
	if storedText != "" {
		stored, _ := json.Marshal(map[string]string{"text": storedText})
		storedContent = string(stored)
		sensitiveContent = string(content)
	}
	return h.Outbound.Enqueue(ctx, model.Message{
		ID:               id,
		ChatEntityID:     botChannelID + ":personal:" + openID,
		BotChannelID:     botChannelID,
		FeishuMsgID:      id,
		ChatType:         model.ChatPersonal,
		Content:          storedContent,
		SensitiveContent: sensitiveContent,
	})
}

func applyKeyGrantText(keyID, secret string) string {
	return strings.Join([]string{
		"DingWei key 已批准，请立即保存；secret 只显示这一次。",
		"",
		"key_id: " + keyID,
		"secret: " + secret,
		"",
		"Linux / macOS:",
		"1. 确认已安装 python3、tmux 和你的 AI CLI。",
		"2. 拉取或解压 sessionHelper 接入包，进入 tools/sessionhelper。",
		"3. 运行 ./run.sh --reconfigure，按提示填写 SH_WS_BASE、会话名、key_id 和 secret。",
		"4. 后续运行 ./run.sh 启动；Linux 可用 cron/guard，macOS 可用 launchd 或前台 nohup。",
		"",
		"Windows WSL2:",
		"1. 安装 Ubuntu WSL2，并在 WSL 内安装 python3、tmux 和 AI CLI。",
		"2. 在 WSL 内按 Linux 步骤运行 tools/sessionhelper/run.sh --reconfigure。",
		"3. 配置写在 WSL 用户目录，保持 WSL 网络可访问 DingWei Hub。",
	}, "\n")
}

func applyKeyGrantStoredText(keyID string) string {
	return strings.Replace(applyKeyGrantText(keyID, "***"), "secret: ***", "secret 已隐藏", 1)
}

type sensitiveOutboundRememberer interface {
	RememberSensitiveContent(id, content string)
}

func (h *Hub) rememberSensitiveOutbound(id, content string) {
	if q, ok := h.Outbound.(sensitiveOutboundRememberer); ok {
		q.RememberSensitiveContent(id, content)
	}
}

func mustTextContent(text string) string {
	content, _ := json.Marshal(map[string]string{"text": text})
	return string(content)
}

func parseSyncCommand(text string) (action, sessionName, keyID string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(stripLeadingMentions(text)))
	if len(fields) != 2 {
		return "", "", "", false
	}
	switch strings.ToLower(fields[0]) {
	case "#sync":
		action = "sync"
	case "#unsync":
		action = "unsync"
	default:
		return "", "", "", false
	}
	target := strings.TrimSpace(fields[1])
	if target == "" {
		return "", "", "", false
	}
	if strings.Contains(target, "#") {
		parts := strings.SplitN(target, "#", 2)
		sessionName, keyID = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if sessionName == "" || keyID == "" || strings.Contains(keyID, "#") {
			return "", "", "", false
		}
		return action, sessionName, keyID, true
	}
	return action, target, "", true
}

func (h *Hub) addFeishuSyncTargetAndSnapshot(keyID, sessionName string, target feishuSyncTarget) []terminalSyncItem {
	k := terminalKey(keyID, sessionName)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.syncTargets[k] == nil {
		h.syncTargets[k] = map[string]feishuSyncTarget{}
	}
	h.syncTargets[k][target.syncKey()] = target
	items := h.recentTerminal[k]
	out := make([]terminalSyncItem, len(items))
	copy(out, items)
	return out
}

func (h *Hub) removeFeishuSyncTarget(keyID, sessionName string, target feishuSyncTarget) {
	k := terminalKey(keyID, sessionName)
	h.mu.Lock()
	defer h.mu.Unlock()
	if targets := h.syncTargets[k]; targets != nil {
		delete(targets, target.syncKey())
		if len(targets) == 0 {
			delete(h.syncTargets, k)
		}
	}
	for bufferKey, buffer := range h.syncBuffers {
		if buffer.keyID == keyID && buffer.sessionName == sessionName && buffer.target.syncKey() == target.syncKey() {
			if buffer.timer != nil {
				buffer.timer.Stop()
			}
			delete(h.syncBuffers, bufferKey)
		}
	}
}

func (h *Hub) appendTerminalSyncItemLocked(keyID, sessionName, text string, ts time.Time) []feishuSyncTarget {
	k := terminalKey(keyID, sessionName)
	items := append(h.recentTerminal[k], terminalSyncItem{TS: ts.UTC(), Text: text})
	if len(items) > 10 {
		items = items[len(items)-10:]
	}
	h.recentTerminal[k] = items
	targets := make([]feishuSyncTarget, 0, len(h.syncTargets[k]))
	for _, target := range h.syncTargets[k] {
		targets = append(targets, target)
	}
	return targets
}

func (h *Hub) closeFeishuSyncLocked(keyID, sessionName string) {
	k := terminalKey(keyID, sessionName)
	delete(h.recentTerminal, k)
	delete(h.syncTargets, k)
	for bufferKey, buffer := range h.syncBuffers {
		if buffer.keyID == keyID && buffer.sessionName == sessionName {
			if buffer.timer != nil {
				buffer.timer.Stop()
			}
			delete(h.syncBuffers, bufferKey)
		}
	}
}

func (h *Hub) sendFeishuSyncItem(ctx context.Context, keyID, sessionName string, target feishuSyncTarget, item terminalSyncItem) error {
	if strings.TrimSpace(item.Text) == "" {
		return nil
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   feishuAddress(target.openID, keyID, target.botName),
		From: sessionAddress(sessionName, keyID),
		Body: formatTerminalSyncText(item),
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":                  "feishu_sync",
			"source_bot_channel_id": target.botChannelID,
		},
	}
	return h.RouteEnvelope(ctx, env)
}

func (h *Hub) queueFeishuSyncItem(keyID, sessionName string, target feishuSyncTarget, item terminalSyncItem) {
	if strings.TrimSpace(item.Text) == "" {
		return
	}
	bufferKey := terminalKey(keyID, sessionName) + ":" + target.syncKey()
	h.mu.Lock()
	buffer := h.syncBuffers[bufferKey]
	if buffer == nil {
		buffer = &feishuSyncBuffer{keyID: keyID, sessionName: sessionName, target: target}
		h.syncBuffers[bufferKey] = buffer
	}
	buffer.items = append(buffer.items, item)
	if buffer.timer == nil {
		buffer.timer = time.AfterFunc(time.Second, func() {
			h.flushFeishuSyncBuffer(bufferKey)
		})
	}
	h.mu.Unlock()
}

func (h *Hub) flushFeishuSyncBuffer(bufferKey string) {
	h.mu.Lock()
	buffer := h.syncBuffers[bufferKey]
	if buffer == nil {
		h.mu.Unlock()
		return
	}
	delete(h.syncBuffers, bufferKey)
	items := append([]terminalSyncItem(nil), buffer.items...)
	keyID, sessionName, target := buffer.keyID, buffer.sessionName, buffer.target
	active := false
	if targets := h.syncTargets[terminalKey(keyID, sessionName)]; targets != nil {
		_, active = targets[target.syncKey()]
	}
	h.mu.Unlock()
	if !active || len(items) == 0 {
		return
	}
	var b strings.Builder
	for _, item := range items {
		b.WriteString(item.Text)
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return
	}
	_ = h.sendFeishuSyncItem(context.Background(), keyID, sessionName, target, terminalSyncItem{TS: items[0].TS, Text: text})
}

func formatTerminalSyncText(item terminalSyncItem) string {
	return fmt.Sprintf("[%s] %s", item.TS.UTC().Format("2006-01-02 15:04:05 UTC"), item.Text)
}

func sanitizeTerminalSyncText(text string) string {
	text = ansiOSCSTRE.ReplaceAllString(text, "")
	text = ansiCSIRE.ReplaceAllString(text, "")
	text = ansiSimpleRE.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = controlExceptTextRE.ReplaceAllString(text, "")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

func (t feishuSyncTarget) syncKey() string {
	return t.botChannelID + ":" + t.openID
}

func (h *Hub) forward(ctx context.Context, serviceID string, req ForwardRequest, timeoutMs int) (string, error) {
	h.mu.Lock()
	c := h.serviceClients[serviceID]
	h.mu.Unlock()
	if c == nil {
		return "", errors.New("service websocket offline")
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	payload, _ := json.Marshal(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.Write(callCtx, websocket.MessageText, payload); err != nil {
		return "", err
	}
	_, data, err := c.conn.Read(callCtx)
	if err != nil {
		return "", err
	}
	var resp ForwardResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}
	return resp.Reply, nil
}

func (h *Hub) RouteEnvelope(ctx context.Context, env model.Envelope) error {
	to, err := parseAddress(env.To)
	if err != nil {
		return err
	}
	from, err := parseAddress(env.From)
	if err != nil {
		return err
	}
	if isCollectEnvelope(env) {
		if to.KeyID != from.KeyID {
			return errors.New("to/from key_id mismatch")
		}
		return h.storeCollectEnvelope(ctx, from, env)
	}
	switch to.Kind {
	case addressSession:
		// 常规同 key 且在线 → 快路径直投
		if to.KeyID == from.KeyID && h.sessionOnline(to.KeyID, to.SessionName) {
			return h.routeToSession(ctx, to.KeyID, to.SessionName, env, from.Kind == addressFeishu)
		}
		// 否则按发件人账号(owner)解析真实 key：支持同账号跨 key 互通 + 容错发件人补错 key + 强制同 owner。
		// （send.py 会把地址补成发件人自己的 key，所以即便 to.KeyID==from.KeyID，目标也可能在该 owner 的另一个 key 下）
		senderOwner := h.ownerKeyForKey(ctx, from.KeyID)
		if senderOwner == "" {
			return fmt.Errorf("无法解析发件人账号 key_id=%s", from.KeyID)
		}
		k, ok := h.resolveOwnerSessionKey(ctx, senderOwner, to.SessionName, to.KeyID)
		if !ok {
			return errors.New("目标会话不在你的账号下或不在线：" + to.SessionName)
		}
		return h.routeToSession(ctx, k, to.SessionName, env, from.Kind == addressFeishu)
	case addressFeishu:
		if to.KeyID != from.KeyID {
			return errors.New("to/from key_id mismatch")
		}
		return h.routeToFeishu(ctx, to, env)
	default:
		return errors.New("unsupported target address")
	}
}

// resolveOwnerSessionKey 在同一账号(owner)下按会话名找到在线目标会话的真实 key_id。
// 先信地址里给的 hintKey(若在线且同账号),否则在该 owner 的所有在线会话里按名匹配。
func (h *Hub) resolveOwnerSessionKey(ctx context.Context, ownerKey, sessionName, hintKey string) (string, bool) {
	if ownerKey == "" || sessionName == "" {
		return "", false
	}
	if hintKey != "" && h.sessionOnline(hintKey, sessionName) && h.ownerKeyForKey(ctx, hintKey) == ownerKey {
		return hintKey, true
	}
	h.mu.Lock()
	var candidates []string
	for keyID, sessions := range h.sessionClients {
		if sessions[sessionName] != nil {
			candidates = append(candidates, keyID)
		}
	}
	h.mu.Unlock()
	for _, keyID := range candidates {
		if h.ownerKeyForKey(ctx, keyID) == ownerKey {
			return keyID, true
		}
	}
	return "", false
}

func (h *Hub) handleAgentNetworkSkillAck(c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != agentNetworkSkillAckType && strings.TrimSpace(env.Body) != agentNetworkSkillAck {
		return false
	}
	c.mu.Lock()
	c.skillInstalled = true
	cancel := c.skillCancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (h *Hub) startAgentNetworkSkillPush(ctx context.Context, c *sessionClient) {
	go func() {
		h.pushAgentNetworkSkill(ctx, c)
		ticker := time.NewTicker(agentNetworkSkillRetryWait)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if h.agentNetworkSkillInstalled(c) {
					return
				}
				h.pushAgentNetworkSkill(ctx, c)
			}
		}
	}()
}

func (h *Hub) agentNetworkSkillInstalled(c *sessionClient) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.skillInstalled
}

func (h *Hub) pushAgentNetworkSkill(ctx context.Context, c *sessionClient) {
	if h.agentNetworkSkillInstalled(c) {
		return
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(c.sessionName, c.keyID),
		From: sessionAddress("workpulse", c.keyID),
		Body: renderAgentNetworkSkill(c.sessionName, c.keyID),
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":         agentNetworkSkillPushType,
			"system":       true,
			"no_mirror":    true,
			"ack_required": true,
			"ack_token":    agentNetworkSkillAck,
			"retry_after":  int(agentNetworkSkillRetryWait.Seconds()),
		},
	}
	if err := c.write(ctx, env); err != nil {
		return
	}
}

func renderAgentNetworkSkill(sessionName, keyID string) string {
	return strings.Join([]string{
		"DingWei Agent网络协作指南",
		"",
		"你已接入DingWei Agent网络，可与同账号的其它在线AI CLI会话互相发消息、协作。",
		"",
		"【一、先查在线清单】谁在线是会变的，需要时你自己去读这个文件（sessionHelper 每分钟刷新，不会主动推给你）：",
		"  ~/.dingwei/" + sessionName + ".DingWeiOnlineSessions.list",
		"文件里每行是一个在线会话：#会话名 · 工具/模型 · 归属@成员 等。要发现可协作对象、或确认某会话是否在线，读它即可。",
		"重要：文件头有一行“更新于 <时间>”。若该时间距当前已超过 2 分钟，说明本机 sessionHelper 可能异常或已断连，此时清单不可信，不要据此判断在线状态。",
		"",
		"【二、给协作者发消息 / 回复】统一用发信脚本主动发（可靠，不受终端渲染影响，也不会被平台丢弃）：",
		"  python3 ~/.dingwei/send.py <收件会话名> <正文>",
		"收件会话名务必用【在线清单】文件里的精确名字（如 zzc-manager，不要用简称或旧名），脚本会自动补全 #key 地址。例如：",
		"  python3 ~/.dingwei/send.py zzc-manager '【" + sessionName + "→zzc-manager】已收到，链路正常。'",
		"⚠️ 收到别人消息后要回复，也必须用本脚本发回给对方——直接在 CLI 里回答无效（平台只回传飞书来源的消息）。",
		"  回复的收件人 = 对方消息里【X→你】中的那个 X（用精确会话名）。长正文改用 --file <文件路径>。",
		"",
		"AI CLI主动发起的真实机制：当你的assistant输出以 #会话名 正文 或 @成员#会话名 正文 开头时，sessionHelper会把这行输出转成DingWei主动信封并交给Hub路由；对方收到后可以回复。",
		"例如：#developer 请核对X",
		"",
		"协作礼仪：消息要简明，说清诉求与来源，不刷屏。发给队友的正文第一行建议自报身份，例如【" + sessionName + "→manager】。",
		"",
		"收到并理解本指南后，请只回复下面这个固定确认标记，方便sessionHelper回执安装完成：",
		agentNetworkSkillAck,
	}, "\n")
}

func (h *Hub) routeSessionSelectorEnvelope(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	text := strings.TrimSpace(env.To)
	if text == "" || (!strings.HasPrefix(text, "#") && !strings.HasPrefix(text, "@")) {
		return false
	}
	if body := strings.TrimSpace(env.Body); body != "" {
		text += " " + body
	}
	if strings.HasPrefix(text, "#") {
		sessionName, body, ok := parseSelector(text)
		if !ok {
			return false
		}
		env.To = sessionAddress(sessionName, c.keyID)
		env.From = sessionAddress(c.sessionName, c.keyID)
		env.Body = body
		if err := h.RouteEnvelope(ctx, env); err != nil {
			_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "投递失败："+err.Error()))
		}
		return true
	}
	memberName, sessionName, body, ok := parseMemberMention(text)
	if !ok {
		return false
	}
	target, err := h.resolveMemberByName(ctx, memberName)
	if err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "投递失败："+err.Error()))
		return true
	}
	if target.DMOptOut {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "投递失败：该成员未开放会话接入："+memberLabel(target)))
		return true
	}
	keyID, resolvedSession, noDirectory, err := h.resolveMemberSession(ctx, target, sessionName)
	if err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "投递失败："+err.Error()))
		return true
	}
	if noDirectory {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "投递失败：该会话为专职隔离会话不接受任务派发"))
		return true
	}
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["cross_member"] = target.OwnerKey
	env.Meta["cross_member_name"] = memberLabel(target)
	env.Meta["cross_session_name"] = resolvedSession
	env.Meta["source_session_name"] = c.sessionName
	env.Meta["source_key_id"] = c.keyID
	env.Meta["reply_prefix"] = fmt.Sprintf("【%s·%s】", memberLabel(target), resolvedSession)
	env.To = sessionAddress(resolvedSession, keyID)
	env.From = sessionAddress(c.sessionName, keyID)
	env.Body = body
	if err := h.RouteEnvelope(ctx, env); err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "投递失败："+err.Error()))
	}
	return true
}

func isCollectEnvelope(env model.Envelope) bool {
	if env.Meta == nil {
		return false
	}
	if typ, ok := env.Meta["type"].(string); ok && typ == "collect" {
		return true
	}
	if v, ok := env.Meta["collect"].(bool); ok && v {
		return true
	}
	return false
}

func (h *Hub) storeCollectEnvelope(ctx context.Context, from address, env model.Envelope) error {
	if h.Repo == nil {
		return nil
	}
	if optOut, err := h.collectOptOut(ctx, from.KeyID); err != nil {
		return err
	} else if optOut {
		return nil
	}
	collectedAt := time.Now().UTC()
	if env.TS > 0 {
		collectedAt = time.Unix(env.TS, 0).UTC()
	}
	content, _ := json.Marshal(map[string]any{
		"text":         env.Body,
		"role":         firstNonEmpty(metaString(env.Meta, "role"), "unknown"),
		"session":      firstNonEmpty(metaString(env.Meta, "session"), from.SessionName),
		"key_id":       from.KeyID,
		"collect":      true,
		"collected_at": collectedAt.Format(time.RFC3339),
	})
	return h.Repo.EnqueueMessage(ctx, model.Message{
		ID:           env.ID,
		ChatEntityID: sessionAddress(from.SessionName, from.KeyID),
		Direction:    model.DirectionCollect,
		BotChannelID: "sessionhelper",
		FeishuMsgID:  env.ID,
		ChatType:     model.ChatPersonal,
		SenderOpenID: from.SessionName,
		Content:      string(content),
		Status:       "done",
		CreatedAt:    collectedAt,
	})
}

func (h *Hub) collectOptOut(ctx context.Context, keyID string) (bool, error) {
	accounts, err := h.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil {
		return false, err
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if accountBelongsToMember(account, member) {
				return member.EvidenceOptOut, nil
			}
		}
	}
	return false, nil
}

func (h *Hub) dispatchSelector(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	sessionName, body, ok := h.parseSelector(msg.BotChannelID, text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	source := sourceAccount(msg)
	keyID, found := h.lookupKeyIDForSession(source, sessionName)
	if !found {
		return model.PrefixDispatchResult{}, false, nil
	}
	env := model.Envelope{
		ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
		To:   sessionAddress(sessionName, keyID),
		From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"chat_type":      string(msg.ChatType),
			"raw":            msg.Content,
			"bot_channel_id": msg.BotChannelID,
		},
	}
	h.annotateSourceContext(env.Meta, msg)
	if msg.ChatType == model.ChatGroup {
		env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
		env.Meta["sender_open_id"] = msg.SenderOpenID
	}
	if err := h.RouteEnvelope(ctx, env); err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true}, true, nil
}

func (h *Hub) dispatchDefaultPersonalSession(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{}, false, nil
	}
	source := sourceAccount(msg)
	keyID, sessions, err := h.onlineSessionsForAccount(ctx, source)
	if err != nil {
		return model.PrefixDispatchResult{}, false, err
	}
	switch len(sessions) {
	case 0:
		return model.PrefixDispatchResult{}, false, nil
	case 1:
		env := model.Envelope{
			ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
			To:   sessionAddress(sessions[0], keyID),
			From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)),
			Body: text,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"chat_type":      string(msg.ChatType),
				"raw":            msg.Content,
				"bot_channel_id": msg.BotChannelID,
				"reply_prefix":   fmt.Sprintf("【%s】", sessions[0]),
			},
		}
		h.annotateSourceContext(env.Meta, msg)
		if err := h.RouteEnvelope(ctx, env); err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true}, true, nil
	default:
		return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("你有多个在线会话（%s），请用 #会话名 内容 指定", strings.Join(sessions, "、"))}, true, nil
	}
}

func (h *Hub) dispatchMemberMention(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	targetName, sessionName, body, ok := h.parseMemberMention(msg.BotChannelID, text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	target, err := h.resolveMemberByName(ctx, targetName)
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
	}
	if target.DMOptOut {
		return model.PrefixDispatchResult{Matched: true, Reply: "该成员未开放会话接入：" + memberLabel(target)}, true, nil
	}
	keyID, resolvedSession, noDirectory, err := h.resolveMemberSession(ctx, target, sessionName)
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
	}
	if noDirectory {
		return model.PrefixDispatchResult{Matched: true, Reply: "该会话为专职隔离会话不接受任务派发"}, true, nil
	}
	source := sourceAccount(msg)
	env := model.Envelope{
		ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
		To:   sessionAddress(resolvedSession, keyID),
		From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"chat_type":            string(msg.ChatType),
			"raw":                  msg.Content,
			"bot_channel_id":       msg.BotChannelID,
			"cross_member":         target.OwnerKey,
			"cross_member_name":    memberLabel(target),
			"cross_session_name":   resolvedSession,
			"reply_prefix":         fmt.Sprintf("【%s·%s】", memberLabel(target), resolvedSession),
			"source_chat_entity":   msg.ChatEntityID,
			"source_sender_openid": msg.SenderOpenID,
		},
	}
	h.annotateSourceContext(env.Meta, msg)
	if msg.ChatType == model.ChatGroup {
		env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
		env.Meta["sender_open_id"] = msg.SenderOpenID
	}
	if err := h.RouteEnvelope(ctx, env); err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "该成员会话投递失败：" + err.Error()}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true}, true, nil
}

func (h *Hub) routeToSession(ctx context.Context, keyID, sessionName string, env model.Envelope, ackInbound bool) error {
	claimed, sig, err := h.claimEnvelope(env)
	if err != nil || !claimed {
		return err
	}
	h.mu.Lock()
	var c *sessionClient
	if sessions := h.sessionClients[keyID]; sessions != nil {
		c = sessions[sessionName]
	}
	h.mu.Unlock()
	if c == nil {
		h.unclaimEnvelope(env.ID, sig)
		return fmt.Errorf("session %s offline", sessionName)
	}
	if err := c.write(ctx, env); err != nil {
		h.unclaimEnvelope(env.ID, sig)
		return err
	}
	if ackInbound && h.Repo != nil && env.ID != "" {
		_ = h.Repo.AckMessage(ctx, env.ID)
	}
	return nil
}

func (h *Hub) routeToFeishu(ctx context.Context, to address, env model.Envelope) error {
	if shouldSkipMirror(env) {
		return nil
	}
	if h.Outbound == nil {
		return errors.New("outbound queue not configured")
	}
	if replyTo, ok := replySourceAddress(env.Meta); ok {
		to.OpenID = replyTo
	}
	chatType := model.ChatPersonal
	entityKind := "personal"
	if strings.HasPrefix(to.OpenID, "oc_") {
		chatType = model.ChatGroup
		entityKind = "group"
	}
	ensureReplyMention(&env, chatType)
	botChannelID, err := h.resolveFeishuBotChannel(ctx, to, env, chatType)
	if err != nil {
		return err
	}
	content, _ := json.Marshal(map[string]string{"text": renderFeishuText(env, chatType)})
	return h.Outbound.Enqueue(ctx, model.Message{
		ID:           env.ID,
		ChatEntityID: botChannelID + ":" + entityKind + ":" + to.OpenID,
		BotChannelID: botChannelID,
		FeishuMsgID:  env.ID,
		ChatType:     chatType,
		Content:      string(content),
	})
}

func (h *Hub) resolveFeishuBotChannel(ctx context.Context, to address, env model.Envelope, chatType model.ChatType) (string, error) {
	if sourceBot := metaString(env.Meta, "source_bot_channel_id"); sourceBot != "" {
		botChannelID := h.botChannel(sourceBot)
		if botChannelID == "" {
			return "", fmt.Errorf("source bot %s not configured", sourceBot)
		}
		return botChannelID, nil
	}
	if !isProducerEnvelope(env) {
		botChannelID := h.botChannel(to.BotName)
		if botChannelID == "" {
			return "", fmt.Errorf("bot %s not configured", to.BotName)
		}
		return botChannelID, nil
	}
	if targetBot := metaString(env.Meta, "target_bot"); targetBot != "" {
		botChannelID := h.botChannel(targetBot)
		if botChannelID == "" {
			return "", fmt.Errorf("producer target_bot %s not configured", targetBot)
		}
		return botChannelID, nil
	}
	if chatType == model.ChatGroup && h.Repo != nil {
		entities, err := h.Repo.ListChatEntities(ctx, model.ChatGroup)
		if err != nil {
			return "", err
		}
		for _, entity := range entities {
			if entity.Active && entity.FeishuID == to.OpenID && entity.BotChannelID != "" {
				return entity.BotChannelID, nil
			}
		}
	}
	if h.Repo != nil {
		from, err := parseAddress(env.From)
		if err == nil && from.KeyID != "" {
			accounts, err := h.Repo.ListAPIKeyAccounts(ctx, from.KeyID)
			if err != nil {
				return "", err
			}
			for _, account := range accounts {
				botChannelID, _, ok := accountEntityParts(account)
				if ok && botChannelID != "" {
					return botChannelID, nil
				}
			}
		}
	}
	return "", fmt.Errorf("producer target_group %s has no known bot_channel; set SH_TARGET_BOT or seed chat_entity", to.OpenID)
}

func isProducerEnvelope(env model.Envelope) bool {
	if env.Meta == nil {
		return false
	}
	if v, ok := env.Meta["producer"].(bool); ok && v {
		return true
	}
	if v, ok := env.Meta["producer"].(string); ok && truthyQuery(v) {
		return true
	}
	return false
}

func shouldSkipMirror(env model.Envelope) bool {
	if env.Meta == nil {
		return false
	}
	if isProducerEnvelope(env) {
		return false
	}
	if v, ok := env.Meta["no_mirror"].(bool); ok && v {
		return true
	}
	if v, ok := env.Meta["system"].(bool); ok && v {
		return true
	}
	switch metaString(env.Meta, "type") {
	case "system", "online_directory", "mirror_control":
		return true
	default:
		return false
	}
}

func (h *Hub) annotateSourceContext(meta map[string]any, msg model.Message) {
	if meta == nil {
		return
	}
	meta["source_bot_channel_id"] = msg.BotChannelID
	meta["source_chat_type"] = string(msg.ChatType)
	source := sourceAccount(msg)
	if openID := feishuOpenIDFromAccount(source); openID != "" {
		meta["source_open_id"] = openID
	}
	switch msg.ChatType {
	case model.ChatGroup:
		meta["source_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
		if strings.TrimSpace(msg.SenderOpenID) != "" {
			meta["source_sender_openid"] = strings.TrimSpace(msg.SenderOpenID)
		}
	case model.ChatPersonal:
		meta["source_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
	}
}

func replySourceAddress(meta map[string]any) (string, bool) {
	switch model.ChatType(metaString(meta, "source_chat_type")) {
	case model.ChatGroup:
		if chatID := metaString(meta, "source_chat_id"); chatID != "" {
			return chatID, true
		}
	case model.ChatPersonal:
		if openID := firstNonEmpty(metaString(meta, "source_sender_openid"), metaString(meta, "source_open_id"), metaString(meta, "source_chat_id")); openID != "" {
			return openID, true
		}
	}
	return "", false
}

func ensureReplyMention(env *model.Envelope, chatType model.ChatType) {
	if chatType != model.ChatGroup || len(metaStringSlice(env.Meta, "at")) != 0 {
		return
	}
	sender := firstNonEmpty(metaString(env.Meta, "source_sender_openid"), metaString(env.Meta, "sender_open_id"))
	if sender == "" {
		return
	}
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["at"] = []string{sender}
}

type onlineSessionItem struct {
	Endpoint model.SessionEndpoint
	Owner    model.Member
	Client   *sessionClient
}

func (h *Hub) scheduleOnlineBroadcast(ctx context.Context, keyID string) {
	if h.Repo == nil {
		return
	}
	ownerKey := h.ownerKeyForKey(ctx, keyID)
	if ownerKey == "" {
		return
	}
	delay := h.onlineDebounce
	if delay <= 0 {
		delay = 2500 * time.Millisecond
	}
	h.mu.Lock()
	if old := h.onlineTimers[ownerKey]; old != nil {
		old.Stop()
	}
	h.onlineTimers[ownerKey] = time.AfterFunc(delay, func() {
		bctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		h.mu.Lock()
		if h.onlineTimers[ownerKey] != nil {
			delete(h.onlineTimers, ownerKey)
		}
		h.mu.Unlock()
		_ = h.broadcastOnlineDirectory(bctx, ownerKey, true)
	})
	h.mu.Unlock()
}

func (h *Hub) scheduleOnlineBroadcastForSession(ctx context.Context, keyID, sessionName string) {
	if h.sessionEndpointNoDirectory(ctx, keyID, sessionName) {
		return
	}
	h.scheduleOnlineBroadcast(ctx, keyID)
}

func (h *Hub) sessionEndpointNoDirectory(ctx context.Context, keyID, sessionName string) bool {
	if h.Repo == nil {
		return false
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return false
	}
	for _, ep := range endpoints {
		if ep.KeyID == keyID && ep.SessionName == sessionName {
			return ep.NoDirectory
		}
	}
	return false
}

func (h *Hub) BroadcastOnlineDirectories(ctx context.Context, includeFeishu bool) error {
	owners, err := h.onlineOwners(ctx)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if err := h.broadcastOnlineDirectory(ctx, owner, includeFeishu); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) BroadcastOnlineDirectoryForKey(ctx context.Context, keyID string, includeFeishu bool) error {
	ownerKey := h.ownerKeyForKey(ctx, keyID)
	if ownerKey == "" {
		return nil
	}
	return h.broadcastOnlineDirectory(ctx, ownerKey, includeFeishu)
}

func (h *Hub) onlineOwners(ctx context.Context) ([]string, error) {
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	byOwner := map[string]model.Member{}
	for _, member := range members {
		if member.Active {
			byOwner[member.OwnerKey] = member
		}
	}
	owners := map[string]bool{}
	h.mu.Lock()
	keys := make([]string, 0, len(h.sessionClients))
	for keyID, sessions := range h.sessionClients {
		if len(sessions) > 0 {
			keys = append(keys, keyID)
		}
	}
	h.mu.Unlock()
	for _, keyID := range keys {
		if owner := h.ownerKeyForKeyWithMembers(ctx, keyID, byOwner); owner != "" {
			owners[owner] = true
		}
	}
	out := make([]string, 0, len(owners))
	for owner := range owners {
		out = append(out, owner)
	}
	sort.Strings(out)
	return out, nil
}

func (h *Hub) ownerKeyForKey(ctx context.Context, keyID string) string {
	accounts, err := h.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil || len(accounts) == 0 {
		return ""
	}
	return h.ownerKeyForKeyWithAccounts(ctx, keyID, accounts)
}

func (h *Hub) ownerKeyForKeyWithAccounts(ctx context.Context, keyID string, accounts []string) string {
	if owner := h.ownerKeyFromBoundAccounts(ctx, accounts); owner != "" {
		return owner
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return ""
	}
	return h.ownerKeyForKeyWithMembersAndAccounts(accounts, members)
}

func (h *Hub) ownerKeyFromBoundAccounts(ctx context.Context, accounts []string) string {
	owners := map[string]bool{}
	for _, account := range accounts {
		botChannelID, feishuID, ok := accountEntityParts(account)
		if !ok {
			continue
		}
		entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID)
		if err != nil || entity == nil || !entity.Active || entity.BoundOwner == "" {
			continue
		}
		owners[entity.BoundOwner] = true
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) ownerKeyForKeyWithMembersAndAccounts(accounts []string, members []model.Member) string {
	owners := map[string]bool{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if account == member.OwnerKey || accountBelongsToMember(account, member) {
				owners[member.OwnerKey] = true
				break
			}
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) broadcastOnlineDirectory(ctx context.Context, ownerKey string, includeFeishu bool) error {
	items, owner, err := h.onlineDirectory(ctx, ownerKey)
	if err != nil {
		return err
	}
	if owner.OwnerKey == "" {
		return nil
	}
	text := renderOnlineDirectory(owner, items)
	broadcastKey := broadcastDedupKey("online_directory", randomHex(16), owner.OwnerKey)
	for i, item := range items {
		env := model.Envelope{
			ID:   randomHex(16),
			To:   sessionAddress(item.Endpoint.SessionName, item.Endpoint.KeyID),
			From: sessionAddress("workpulse", item.Endpoint.KeyID),
			Body: text,
			TS:   time.Now().Unix(),
			Meta: map[string]any{"type": "online_directory", "owner": owner.OwnerKey, "system": true, "no_mirror": true},
		}
		annotateBroadcastMirror(&env, broadcastKey, i == 0)
		if err := item.Client.write(ctx, env); err != nil {
			continue
		}
	}
	if includeFeishu && h.Outbound != nil && owner.FeishuOpenID != "" {
		botChannelID := h.botChannel("UnifiedRobot")
		content, _ := json.Marshal(map[string]string{"text": text})
		if err := h.Outbound.Enqueue(ctx, model.Message{
			ID:           "online-" + randomHex(16),
			ChatEntityID: botChannelID + ":personal:" + owner.FeishuOpenID,
			BotChannelID: botChannelID,
			ChatType:     model.ChatPersonal,
			Content:      string(content),
		}); err != nil {
			return err
		}
	}
	return nil
}

func annotateBroadcastMirror(env *model.Envelope, dedupKey string, primary bool) {
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["broadcast_dedup_key"] = dedupKey
	env.Meta["mirror_primary"] = primary
}

func broadcastDedupKey(kind, sourceID, ownerKey string) string {
	return strings.Join([]string{
		"broadcast",
		kind,
		firstNonEmpty(ownerKey, "unknown-owner"),
		firstNonEmpty(sourceID, randomHex(16)),
	}, ":")
}

func (h *Hub) onlineDirectory(ctx context.Context, ownerKey string) ([]onlineSessionItem, model.Member, error) {
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return nil, model.Member{}, err
	}
	var owner model.Member
	byOwner := map[string]model.Member{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		byOwner[member.OwnerKey] = member
		if member.OwnerKey == ownerKey {
			owner = member
		}
	}
	if owner.OwnerKey == "" {
		return nil, model.Member{}, nil
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, model.Member{}, err
	}
	endpointByID := map[string]model.SessionEndpoint{}
	for _, ep := range endpoints {
		endpointByID[ep.KeyID+"#"+ep.SessionName] = ep
	}
	h.mu.Lock()
	var clients []onlineSessionItem
	for keyID, sessions := range h.sessionClients {
		if h.ownerKeyForKeyLocked(ctx, keyID, byOwner) != ownerKey {
			continue
		}
		for sessionName, c := range sessions {
			ep := endpointByID[keyID+"#"+sessionName]
			if ep.KeyID == "" {
				ep = model.SessionEndpoint{KeyID: keyID, SessionName: sessionName, Active: true}
			}
			if !ep.Active {
				ep.Active = true
			}
			if ep.NoDirectory {
				continue
			}
			clients = append(clients, onlineSessionItem{Endpoint: ep, Owner: owner, Client: c})
		}
	}
	h.mu.Unlock()
	sort.Slice(clients, func(i, j int) bool {
		a, b := clients[i].Endpoint, clients[j].Endpoint
		if a.SessionName == b.SessionName {
			return a.KeyID < b.KeyID
		}
		return a.SessionName < b.SessionName
	})
	return clients, owner, nil
}

func (h *Hub) ownerKeyForKeyLocked(ctx context.Context, keyID string, members map[string]model.Member) string {
	return h.ownerKeyForKeyWithMembers(ctx, keyID, members)
}

func (h *Hub) ownerKeyForKeyWithMembers(ctx context.Context, keyID string, members map[string]model.Member) string {
	accounts, err := h.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil || len(accounts) == 0 {
		return ""
	}
	if owner := h.ownerKeyFromBoundAccounts(ctx, accounts); owner != "" {
		return owner
	}
	owners := map[string]bool{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if account == member.OwnerKey || accountBelongsToMember(account, member) {
				owners[member.OwnerKey] = true
				break
			}
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) issueOwnerLinkCode(ownerKey string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for code, token := range h.linkTokens {
		if now.After(token.expiresAt) {
			delete(h.linkTokens, code)
		}
	}
	for i := 0; i < 64; i++ {
		code := randomHex(12)
		if _, exists := h.linkTokens[code]; exists {
			continue
		}
		h.linkTokens[code] = ownerLinkToken{ownerKey: ownerKey, expiresAt: now.Add(ownerLinkTokenTTL)}
		return code
	}
	code := randomHex(16)
	h.linkTokens[code] = ownerLinkToken{ownerKey: ownerKey, expiresAt: now.Add(ownerLinkTokenTTL)}
	return code
}

func (h *Hub) ownerLinkCodeValid(ownerKey, code string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	code = strings.TrimSpace(code)
	token, ok := h.linkTokens[code]
	if !ok {
		return false
	}
	if time.Now().After(token.expiresAt) {
		delete(h.linkTokens, code)
		return false
	}
	return token.ownerKey == ownerKey
}

func (h *Hub) terminalPageCodes(keyID, sessionName string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil {
		return nil
	}
	codes := make([]string, 0, len(st.viewers))
	for _, viewer := range st.viewers {
		if viewer.code != "" {
			codes = append(codes, viewer.code)
		}
	}
	sort.Strings(codes)
	return codes
}

func (h *Hub) HandleOwnerLinksPage(w http.ResponseWriter, r *http.Request) {
	ownerKey := strings.TrimSpace(r.PathValue("ownerKey"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if ownerKey == "" || strings.Contains(ownerKey, "/") || code == "" || !h.ownerLinkCodeValid(ownerKey, code) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	items, owner, err := h.onlineDirectory(r.Context(), ownerKey)
	if err != nil {
		http.Error(w, "load links failed", http.StatusInternalServerError)
		return
	}
	if owner.OwnerKey == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderOwnerLinksHTML(owner, items, code, h)))
}

var terminalViewBase = strings.TrimRight(os.Getenv("WP_VIEW_BASE_URL"), "/")

func ownerLinksURL(ownerKey, code string) string {
	path := "/links/" + url.PathEscape(ownerKey) + "?code=" + url.QueryEscape(code)
	if terminalViewBase == "" {
		return path
	}
	return terminalViewBase + path
}

func viewURL(sessionName string) string {
	path := "/view/" + url.PathEscape(sessionName)
	if terminalViewBase == "" {
		return path
	}
	return terminalViewBase + path
}

func renderOwnerLinksText(owner model.Member, items []onlineSessionItem, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【在线客户端访问清单】%s\n", firstNonEmpty(owner.DisplayName, owner.OwnerKey))
	fmt.Fprintf(&b, "清单页面: %s\n", ownerLinksURL(owner.OwnerKey, code))
	if len(items) == 0 {
		b.WriteString("暂无在线会话。")
		return b.String()
	}
	for i, item := range items {
		ep := item.Endpoint
		fmt.Fprintf(&b, "%d. %s · %s/%s · 在线 · view: %s", i+1, ep.SessionName, unknown(ep.Tool), unknown(ep.Model), viewURL(ep.SessionName))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderOwnerLinksHTML(owner model.Member, items []onlineSessionItem, code string, h *Hub) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<meta http-equiv="refresh" content="10">`)
	b.WriteString(`<title>DingWei 在线客户端</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#1f2328;background:#f6f8fa}table{border-collapse:collapse;width:100%;background:#fff}th,td{border:1px solid #d0d7de;padding:8px;text-align:left}th{background:#f0f3f6}a{color:#0969da}.muted{color:#656d76}.code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}</style></head><body>`)
	fmt.Fprintf(&b, `<h1>%s 在线客户端</h1>`, html.EscapeString(firstNonEmpty(owner.DisplayName, owner.OwnerKey)))
	fmt.Fprintf(&b, `<p class="muted">owner: <span class="code">%s</span> · 页面码 <span class="code">%s</span> · 每 10 秒自动刷新</p>`, html.EscapeString(owner.OwnerKey), html.EscapeString(code))
	if len(items) == 0 {
		b.WriteString(`<p>暂无在线会话。</p></body></html>`)
		return b.String()
	}
	b.WriteString(`<table><thead><tr><th>完整名</th><th>工具/模型</th><th>状态</th><th>View</th><th>页面码</th></tr></thead><tbody>`)
	for _, item := range items {
		ep := item.Endpoint
		codes := h.terminalPageCodes(ep.KeyID, ep.SessionName)
		codeText := "未打开"
		if len(codes) > 0 {
			codeText = strings.Join(codes, ", ")
		}
		fmt.Fprintf(&b, `<tr><td class="code">%s</td><td>%s/%s</td><td>在线</td><td><a href="%s" target="_blank" rel="noreferrer">打开</a></td><td class="code">%s</td></tr>`,
			html.EscapeString(ep.SessionName),
			html.EscapeString(unknown(ep.Tool)),
			html.EscapeString(unknown(ep.Model)),
			html.EscapeString(viewURL(ep.SessionName)),
			html.EscapeString(codeText),
		)
	}
	b.WriteString(`</tbody></table></body></html>`)
	return b.String()
}

func renderOnlineDirectory(owner model.Member, items []onlineSessionItem) string {
	var b strings.Builder
	b.WriteString("\n**********\n")
	fmt.Fprintf(&b, "【DingWei在线清单】同账号在线AI会话,供跨会话寻址(上下线): %s\n", firstNonEmpty(owner.DisplayName, owner.OwnerKey))
	if len(items) == 0 {
		b.WriteString("暂无在线会话。")
		b.WriteString("\n**********\n")
		return b.String()
	}
	for i, item := range items {
		ep := item.Endpoint
		short := shortSessionName(ep.SessionName)
		fmt.Fprintf(&b, "%d. #%s · %s/%s · %s(末%s) · @%s#%s", i+1, short, unknown(ep.Tool), unknown(ep.Model), unknown(ep.ClientIP), keyTail(ep.KeyID), owner.OwnerKey, short)
		if item.Client != nil && item.Client.osName != "" {
			fmt.Fprintf(&b, " · %s", item.Client.osName)
		}
		if terminalViewBase != "" && item.Client != nil && item.Client.webTerminal {
			fmt.Fprintf(&b, " · 页面 %s/view/%s", terminalViewBase, url.PathEscape(ep.SessionName))
		}
		if ep.SessionName != short {
			fmt.Fprintf(&b, " · 全名:%s", ep.SessionName)
		}
		if strings.TrimSpace(ep.FullSessionName) != "" && ep.FullSessionName != ep.SessionName {
			fmt.Fprintf(&b, " · 终端:%s", strings.TrimSpace(ep.FullSessionName))
		}
		if sessionNameEnforceMode() == "warn" {
			if nameWarn := sessionNamePolicyWarning(ep.SessionName, ep.KeyID, ep.OwnerKey); nameWarn != "" {
				fmt.Fprintf(&b, " · 命名告警:%s", nameWarn)
			}
		}
		if ep.Producer || strings.TrimSpace(ep.TargetGroup) != "" {
			fmt.Fprintf(&b, " · Producer:%s", yesNo(ep.Producer))
			if strings.TrimSpace(ep.TargetGroup) != "" {
				fmt.Fprintf(&b, "→%s", strings.TrimSpace(ep.TargetGroup))
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString("**********\n")
	return b.String()
}

func shortSessionName(sessionName string) string {
	sessionName = strings.TrimSpace(sessionName)
	parts := strings.Split(sessionName, "-")
	if len(parts) == 3 && sessionNamePattern.MatchString(sessionName) && strings.TrimSpace(parts[1]) != "" {
		return parts[1]
	}
	if len(parts) >= 3 && parts[0] == "sh" && strings.TrimSpace(parts[1]) != "" {
		return parts[1]
	}
	return sessionName
}

func sessionNameEnforceMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WP_NAME_ENFORCE"))) {
	case "off":
		return "off"
	case "enforce":
		return "enforce"
	default:
		return "warn"
	}
}

func sessionNamePolicyWarning(sessionName, keyID, ownerKey string) string {
	sessionName = strings.TrimSpace(sessionName)
	keyID = strings.TrimSpace(keyID)
	ownerKey = strings.TrimSpace(ownerKey)
	if !sessionNamePattern.MatchString(sessionName) {
		return "会话名不合规,须为 <owner_key>-<短名>-<key末4位>,如 fulei-dev1013-3dd6"
	}
	parts := strings.Split(sessionName, "-")
	if len(parts) != 3 {
		return "会话名不合规,须为 <owner_key>-<短名>-<key末4位>,如 fulei-dev1013-3dd6"
	}
	if parts[2] != keyTail(keyID) {
		return "会话名末4位与 SH_KEY_ID 不匹配"
	}
	if ownerKey == "" {
		return "无法确认该 key 绑定成员 owner_key"
	}
	if parts[0] != ownerKey {
		return "会话名 owner_key 与该 key 绑定成员不匹配"
	}
	return ""
}

func keyTail(keyID string) string {
	keyID = strings.ToLower(strings.TrimSpace(keyID))
	if len(keyID) <= 4 {
		return keyID
	}
	return keyID[len(keyID)-4:]
}

func unknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return strings.TrimSpace(value)
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func (c *sessionClient) write(ctx context.Context, env model.Envelope) error {
	payload, _ := json.Marshal(env)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func renderFeishuText(env model.Envelope, chatType model.ChatType) string {
	body := env.Body
	if chatType != model.ChatGroup {
		return body
	}
	at := metaStringSlice(env.Meta, "at")
	if len(at) == 0 {
		return body
	}
	var b strings.Builder
	for _, openID := range at {
		openID = strings.TrimSpace(openID)
		if openID == "" {
			continue
		}
		fmt.Fprintf(&b, `<at user_id="%s"></at>`, openID)
	}
	if b.Len() == 0 {
		return body
	}
	if strings.TrimSpace(body) != "" {
		b.WriteByte(' ')
		b.WriteString(body)
	}
	return b.String()
}

func metaStringSlice(meta map[string]any, key string) []string {
	if meta == nil {
		return nil
	}
	switch v := meta[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func metaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	switch v := meta[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on":
			return true
		}
	}
	return false
}

func (h *Hub) claimEnvelope(env model.Envelope) (bool, string, error) {
	if env.ID == "" {
		return true, "", nil
	}
	sig := envelopeSignature(env)
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.envelopeIDs[env.ID]; ok {
		if old == sig {
			return false, sig, nil
		}
		return false, sig, fmt.Errorf("duplicate envelope id %s with different payload", env.ID)
	}
	h.envelopeIDs[env.ID] = sig
	return true, sig, nil
}

func (h *Hub) unclaimEnvelope(id, sig string) {
	if id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.envelopeIDs[id] == sig {
		delete(h.envelopeIDs, id)
	}
}

func envelopeSignature(env model.Envelope) string {
	sum := sha256.Sum256([]byte(env.To + "\x00" + env.From + "\x00" + env.Body))
	return hex.EncodeToString(sum[:])
}

func stripPrefix(expr, input string, caseSensitive bool) string {
	if ok, n := router.MatchPrefixLen(expr, input, caseSensitive); ok {
		return strings.TrimSpace(input[n:])
	}
	return input
}

func validatePrefixRule(rule model.RoutingRule) error {
	if strings.TrimSpace(rule.MatchExpr) == "" {
		return errors.New("prefix match expr is empty")
	}
	for _, p := range router.SplitPatterns(rule.MatchExpr) {
		if p == "*" {
			return errors.New("high-risk wildcard prefix * requires explicit override")
		}
	}
	return nil
}

func (h *Hub) ensureNoPrefixOverlap(ctx context.Context, rule model.RoutingRule) error {
	newScope, err := h.effectiveAccounts(ctx, rule)
	if err != nil {
		return err
	}
	if len(newScope) == 0 {
		return nil
	}
	routes, err := h.Repo.ListAllPrefixRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.Rule.ID == rule.ID {
			continue
		}
		if !sameScopeEntityType(rule.ScopeEntityType, route.Rule.ScopeEntityType) {
			continue
		}
		oldScope, err := h.effectiveAccounts(ctx, route.Rule)
		if err != nil {
			return err
		}
		if !accountScopesOverlap(newScope, oldScope) {
			continue
		}
		caseSensitive := rule.CaseSensitive && route.Rule.CaseSensitive
		if router.Overlaps(rule.MatchExpr, route.Rule.MatchExpr, caseSensitive) {
			return fmt.Errorf("prefix route overlaps existing rule %s", route.Rule.ID)
		}
	}
	return nil
}

func (h *Hub) effectiveAccounts(ctx context.Context, rule model.RoutingRule) (map[string]bool, error) {
	bound, err := h.Repo.ListServiceBoundAccounts(ctx, rule.ServiceID)
	if err != nil {
		return nil, err
	}
	boundSet := map[string]bool{}
	for _, id := range bound {
		boundSet[id] = true
	}
	if rule.AccountScopeJSON == "" {
		return boundSet, nil
	}
	var scoped []string
	if err := json.Unmarshal([]byte(rule.AccountScopeJSON), &scoped); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, id := range scoped {
		if boundSet[id] {
			out[id] = true
		}
	}
	return out, nil
}

func sameScopeEntityType(a, b string) bool {
	return a == "" || b == "" || a == b
}

func accountScopesOverlap(a, b map[string]bool) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for id := range a {
		if b[id] {
			return true
		}
	}
	return false
}

type addressKind string

const (
	addressSession addressKind = "session"
	addressFeishu  addressKind = "feishu"
)

type address struct {
	Kind        addressKind
	SessionName string
	OpenID      string
	KeyID       string
	BotName     string
}

func parseAddress(raw string) (address, error) {
	parts := strings.Split(strings.TrimSpace(raw), "#")
	switch len(parts) {
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return address{}, errors.New("invalid session address")
		}
		return address{Kind: addressSession, SessionName: parts[0], KeyID: parts[1]}, nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return address{}, errors.New("invalid feishu address")
		}
		return address{Kind: addressFeishu, OpenID: parts[0], KeyID: parts[1], BotName: parts[2]}, nil
	default:
		return address{}, errors.New("invalid address")
	}
}

func sessionAddress(sessionName, keyID string) string {
	return sessionName + "#" + keyID
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

func clientIPFromRequest(r *http.Request) string {
	remote := hostOnly(r.RemoteAddr)
	if isTrustedProxy(remote) {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			return real
		}
	}
	return remote
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

func isTrustedProxy(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func feishuAddress(openID, keyID, botName string) string {
	return openID + "#" + keyID + "#" + botName
}

func parseSelector(text string) (sessionName, body string, ok bool) {
	text = stripLeadingMentions(text)
	if !strings.HasPrefix(text, "#") || len(text) == 1 {
		return "", "", false
	}
	rest := strings.TrimPrefix(text, "#")
	if len(rest) == 0 || strings.TrimLeft(rest, " \t\r\n") != rest {
		return "", "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	sessionName = strings.TrimSpace(fields[0])
	if sessionName == "" || strings.Contains(sessionName, "#") {
		return "", "", false
	}
	body = strings.TrimSpace(strings.TrimPrefix(rest, sessionName))
	return sessionName, body, true
}

func (h *Hub) parseSelector(botChannelID, text string) (sessionName, body string, ok bool) {
	return parseSelector(h.stripLeadingBotMentions(botChannelID, text))
}

func parseMemberMention(text string) (memberName, sessionName, body string, ok bool) {
	text = stripLeadingMentions(text)
	if !strings.HasPrefix(text, "@") || strings.HasPrefix(text, "@_user_") {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(text, "@")
	if rest == "" || strings.TrimLeft(rest, " \t\r\n") != rest {
		return "", "", "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", "", false
	}
	head := strings.TrimSpace(fields[0])
	if head == "" {
		return "", "", "", false
	}
	if strings.Contains(head, "#") {
		parts := strings.SplitN(head, "#", 2)
		memberName = strings.TrimSpace(parts[0])
		sessionName = strings.TrimSpace(parts[1])
	} else {
		memberName = head
	}
	if memberName == "" || strings.Contains(memberName, "#") || strings.Contains(sessionName, "#") {
		return "", "", "", false
	}
	body = strings.TrimSpace(strings.TrimPrefix(rest, head))
	return memberName, sessionName, body, true
}

func (h *Hub) parseMemberMention(botChannelID, text string) (memberName, sessionName, body string, ok bool) {
	return parseMemberMention(h.stripLeadingBotMentions(botChannelID, text))
}

func stripLeadingMentions(text string) string {
	text = strings.TrimSpace(text)
	for strings.HasPrefix(text, "@_user_") {
		i := strings.IndexAny(text, " \t\r\n")
		if i <= 0 {
			break
		}
		text = strings.TrimSpace(text[i:])
	}
	return text
}

func (h *Hub) stripLeadingBotMentions(botChannelID, text string) string {
	text = stripLeadingMentions(text)
	for {
		if !strings.HasPrefix(text, "@") || strings.HasPrefix(text, "@_user_") {
			return text
		}
		raw := strings.TrimPrefix(text, "@")
		if raw == "" {
			return text
		}
		i := strings.IndexAny(raw, " \t\r\n")
		if i <= 0 {
			return text
		}
		name := strings.TrimSpace(raw[:i])
		if !h.isBotMention(botChannelID, name) {
			return text
		}
		text = strings.TrimSpace(raw[i:])
	}
}

func (h *Hub) isBotMention(botChannelID, name string) bool {
	name = strings.TrimSpace(strings.TrimPrefix(name, "@"))
	if name == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if strings.EqualFold(name, botChannelID) {
		return true
	}
	if strings.EqualFold(name, h.botNames[botChannelID]) {
		return true
	}
	for botName, id := range h.botChannels {
		if strings.EqualFold(name, botName) || strings.EqualFold(name, id) {
			return true
		}
	}
	return false
}

func (h *Hub) resolveMemberByName(ctx context.Context, name string) (model.Member, error) {
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return model.Member{}, err
	}
	var matches []model.Member
	for _, member := range members {
		if !member.Active {
			continue
		}
		if member.OwnerKey == name || member.DisplayName == name {
			matches = append(matches, member)
		}
	}
	if len(matches) == 0 {
		return model.Member{}, fmt.Errorf("未找到成员：%s", name)
	}
	if len(matches) > 1 {
		return model.Member{}, fmt.Errorf("成员名不唯一，请使用 owner_key：%s", name)
	}
	return matches[0], nil
}

func (h *Hub) resolveMemberSession(ctx context.Context, member model.Member, requestedSession string) (string, string, bool, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", "", false, err
	}
	type candidate struct {
		keyID       string
		session     string
		noDirectory bool
	}
	var candidates []candidate
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		if requestedSession != "" && ep.SessionName != requestedSession {
			continue
		}
		accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			return "", "", false, err
		}
		for _, account := range accounts {
			if accountBelongsToMember(account, member) {
				candidates = append(candidates, candidate{keyID: ep.KeyID, session: ep.SessionName, noDirectory: ep.NoDirectory})
				break
			}
		}
	}
	if len(candidates) == 0 {
		if requestedSession != "" {
			return "", "", false, fmt.Errorf("成员 %s 的会话 %s 不在线或未绑定", memberLabel(member), requestedSession)
		}
		return "", "", false, fmt.Errorf("成员 %s 暂无在线会话", memberLabel(member))
	}
	if requestedSession != "" {
		return candidates[0].keyID, candidates[0].session, candidates[0].noDirectory, nil
	}
	visible := candidates[:0]
	for _, c := range candidates {
		if !c.noDirectory {
			visible = append(visible, c)
		}
	}
	if len(visible) == 0 {
		return candidates[0].keyID, candidates[0].session, true, nil
	}
	if len(visible) > 1 {
		names := make([]string, 0, len(visible))
		for _, c := range visible {
			names = append(names, c.session)
		}
		return "", "", false, fmt.Errorf("成员 %s 有多个在线会话，请指定：@%s#<会话名>。可选：%s", memberLabel(member), member.OwnerKey, strings.Join(names, "、"))
	}
	return visible[0].keyID, visible[0].session, false, nil
}

func memberLabel(member model.Member) string {
	return firstNonEmpty(member.DisplayName, member.OwnerKey)
}

func parseMirrorCommand(text string) (action, sessionName string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 || strings.ToLower(fields[0]) != "mirror" {
		return "", "", false
	}
	action = strings.ToLower(fields[1])
	if action != "on" && action != "off" {
		return "", "", false
	}
	sessionName = strings.TrimSpace(fields[2])
	if sessionName == "" || strings.Contains(sessionName, "#") {
		return "", "", false
	}
	return action, sessionName, true
}

func (h *Hub) lookupKeyIDForSession(chatEntityID, sessionName string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for keyID, sessions := range h.sessionClients {
		if sessions[sessionName] == nil {
			continue
		}
		if h.keyAccounts[keyID][chatEntityID] {
			return keyID, true
		}
	}
	return "", false
}

func (h *Hub) onlineSessionsForAccount(ctx context.Context, chatEntityID string) (string, []string, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", nil, err
	}
	keySessions := map[string][]string{}
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			return "", nil, err
		}
		for _, account := range accounts {
			if account == chatEntityID {
				keySessions[ep.KeyID] = append(keySessions[ep.KeyID], ep.SessionName)
				break
			}
		}
	}
	if len(keySessions) == 0 {
		return "", nil, nil
	}
	if len(keySessions) == 1 {
		for keyID, sessions := range keySessions {
			sort.Strings(sessions)
			return keyID, sessions, nil
		}
	}
	var all []string
	for _, sessions := range keySessions {
		all = append(all, sessions...)
	}
	sort.Strings(all)
	return "", all, nil
}

func (h *Hub) lookupKeyIDForMirror(ctx context.Context, chatEntityID, sessionName string) (string, bool, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", false, err
	}
	for _, ep := range endpoints {
		if ep.SessionName != sessionName {
			continue
		}
		accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			return "", false, err
		}
		for _, account := range accounts {
			if account == chatEntityID {
				return ep.KeyID, true, nil
			}
		}
	}
	return "", false, nil
}

func (h *Hub) botChannel(botName string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id := h.botChannels[botName]; id != "" {
		return id
	}
	return botName
}

func (h *Hub) botName(botChannelID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name := h.botNames[botChannelID]; name != "" {
		return name
	}
	return botChannelID
}

func sourceAccount(msg model.Message) string {
	if msg.ChatType == model.ChatGroup && msg.SenderOpenID != "" {
		return msg.BotChannelID + ":personal:" + msg.SenderOpenID
	}
	return msg.ChatEntityID
}

func feishuOpenIDFromAccount(chatEntityID string) string {
	parts := strings.Split(chatEntityID, ":")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	return chatEntityID
}

func accountEntityParts(chatEntityID string) (botChannelID, feishuID string, ok bool) {
	parts := strings.Split(chatEntityID, ":")
	if len(parts) < 3 || parts[0] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	return parts[0], parts[len(parts)-1], true
}

func accountBelongsToMember(account string, member model.Member) bool {
	if account == "" {
		return false
	}
	if account == member.OwnerKey {
		return true
	}
	if member.FeishuOpenID != "" && (account == member.FeishuOpenID || strings.HasSuffix(account, ":"+member.FeishuOpenID)) {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func stringSet(items []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		out[item] = true
	}
	return out
}

func errorEnvelope(keyID, sessionName, sourceID, body string) model.Envelope {
	return model.Envelope{
		ID:   firstNonEmpty(sourceID, randomHex(16)),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"error": true, "system": true, "no_mirror": true},
	}
}

func newKeyID(label, fallback string) string {
	base := sanitizeKeyIDPart(firstNonEmpty(label, fallback, "person"))
	return fmt.Sprintf("FB-%s-%s-%s", base, time.Now().UTC().Format("20060102"), randomHex(4))
}

func sanitizeKeyIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteByte('-')
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "person"
	}
	return b.String()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomDigits(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n)
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = '0' + b[i]%10
	}
	return string(out)
}
