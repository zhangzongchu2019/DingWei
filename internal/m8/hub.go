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
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

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

	mu             sync.Mutex
	serviceClients map[string]*client
	sessionClients map[string]map[string]*sessionClient
	keyAccounts    map[string]map[string]bool
	botChannels    map[string]string
	botNames       map[string]string
	envelopeIDs    map[string]string
	onlineTimers   map[string]*time.Timer
	onlineDebounce time.Duration
}

type client struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

type sessionClient struct {
	conn        *websocket.Conn
	mu          sync.Mutex
	keyID       string
	sessionName string
	targetBot   string
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
	secOpsOwnerKey   = "system-v-task-internal"
	secOpsMemberName = "SYSTEM-V-TASK-INTERNAL"
	secOpsKeyword    = "#系统安全"
)

var (
	secOpsAdminOpenID   = strings.TrimSpace(os.Getenv("WP_SECOPS_ADMIN_OPENID"))
	secOpsAdminOwnerKey = strings.TrimSpace(os.Getenv("WP_SECOPS_ADMIN_OWNER_KEY"))
)

func New(repo store.Repository) *Hub {
	return &Hub{
		Repo:           repo,
		serviceClients: map[string]*client{},
		sessionClients: map[string]map[string]*sessionClient{},
		keyAccounts:    map[string]map[string]bool{},
		botChannels:    map[string]string{},
		botNames:       map[string]string{},
		envelopeIDs:    map[string]string{},
		onlineTimers:   map[string]*time.Timer{},
		onlineDebounce: 2500 * time.Millisecond,
	}
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
	secret = "wp_" + randomHex(32)
	meta = model.ServiceAPIKey{
		ID:        newKeyID(label, serviceID),
		ServiceID: serviceID,
		KeyHash:   HashAPIKey(secret),
		Label:     label,
		Active:    true,
		CreatedAt: time.Now().UTC(),
	}
	return secret, meta, h.Repo.InsertServiceAPIKey(ctx, meta)
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
	mirrorTo := sessionMirrorToFromRequest(r)
	ownerKey := h.ownerKeyForKeyWithAccounts(r.Context(), keyID, accounts)
	sessionName, err := h.registerSessionEndpoint(r.Context(), model.SessionEndpoint{
		KeyID:           keyID,
		SessionName:     requestedSessionName,
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
	c := &sessionClient{conn: conn, keyID: keyID, sessionName: sessionName, targetBot: targetBot}
	h.mu.Lock()
	if h.sessionClients[keyID] == nil {
		h.sessionClients[keyID] = map[string]*sessionClient{}
	}
	if old := h.sessionClients[keyID][sessionName]; old != nil {
		_ = old.conn.Close(websocket.StatusPolicyViolation, "replaced")
	}
	h.sessionClients[keyID][sessionName] = c
	h.keyAccounts[keyID] = stringSet(accounts)
	h.mu.Unlock()
	h.scheduleOnlineBroadcastForSession(r.Context(), keyID, sessionName)
	defer func() {
		ownerCtx, ownerCancel := context.WithTimeout(context.Background(), time.Second)
		ownerKey := h.ownerKeyForKey(ownerCtx, keyID)
		ownerCancel()
		h.mu.Lock()
		if h.sessionClients[keyID][sessionName] == c {
			delete(h.sessionClients[keyID], sessionName)
			if len(h.sessionClients[keyID]) == 0 {
				delete(h.sessionClients, keyID)
				delete(h.keyAccounts, keyID)
			}
		}
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
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
		if err := h.RouteEnvelope(r.Context(), env); err != nil {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, "投递失败："+err.Error()))
		}
	}
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

func (h *Hub) Dispatch(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, error) {
	if result, handled, err := h.dispatchSecurityOps(ctx, msg, text); handled || err != nil {
		return result, err
	}
	if result, handled, err := h.dispatchSystem(ctx, msg, text); handled || err != nil {
		return result, err
	}
	if result, handled, err := h.dispatchAggregateWeeklyReview(ctx, msg, text); handled || err != nil {
		return result, err
	}
	if result, handled, err := h.dispatchMirrorCommand(ctx, msg, text); handled || err != nil {
		return result, err
	}
	if result, handled, err := h.dispatchMemberMention(ctx, msg, text); handled || err != nil {
		return result, err
	}
	if result, handled, err := h.dispatchSelector(ctx, msg, text); handled || err != nil {
		return result, err
	}
	routes, err := h.Repo.ListPrefixRoutes(ctx, msg.ChatEntityID)
	if err != nil {
		return model.PrefixDispatchResult{}, err
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
				return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, nil
			}
			return model.PrefixDispatchResult{Matched: true}, nil
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
			return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: reply}, nil
	}
	if result, handled, err := h.dispatchDefaultPersonalSession(ctx, msg, text); handled || err != nil {
		return result, err
	}
	return model.PrefixDispatchResult{}, nil
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
	if to.KeyID != from.KeyID {
		return errors.New("to/from key_id mismatch")
	}
	if isCollectEnvelope(env) {
		return h.storeCollectEnvelope(ctx, from, env)
	}
	switch to.Kind {
	case addressSession:
		return h.routeToSession(ctx, to.KeyID, to.SessionName, env, from.Kind == addressFeishu)
	case addressFeishu:
		return h.routeToFeishu(ctx, to, env)
	default:
		return errors.New("unsupported target address")
	}
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
		displayName := firstNonEmpty(ep.FullSessionName, ep.SessionName)
		short := shortSessionName(displayName)
		fmt.Fprintf(&b, "%d. #%s · %s/%s · %s(末%s) · @%s#%s", i+1, short, unknown(ep.Tool), unknown(ep.Model), unknown(ep.ClientIP), keyTail(ep.KeyID), owner.OwnerKey, short)
		if displayName != short {
			fmt.Fprintf(&b, " · 全名:%s", displayName)
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
	parts := strings.Split(sessionName, "-")
	if len(parts) >= 3 && parts[0] == "sh" && strings.TrimSpace(parts[1]) != "" {
		return parts[1]
	}
	return sessionName
}

func keyTail(keyID string) string {
	keyID = strings.TrimSpace(keyID)
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
