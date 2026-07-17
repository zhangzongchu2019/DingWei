// Package feishu 是飞书网关抽象（接入层）。
//
// 规范 §14.1：Feishu Gateway 接口 → 真实(larksuite/oapi-sdk-go)/Mock 可换。
// 业务模块不依赖飞书 SDK，只依赖此接口（§13.1）。
package feishu

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcore "github.com/larksuite/oapi-sdk-go/v3/core"
	"github.com/larksuite/oapi-sdk-go/v3/event/dispatcher"
	larkcontact "github.com/larksuite/oapi-sdk-go/v3/service/contact/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
	larkws "github.com/larksuite/oapi-sdk-go/v3/ws"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// OutMessage 出站发送请求（目标可按 id 或名称，见规范 §4.5）。
type OutMessage struct {
	BotChannelID string
	ToID         string // chat_id / open_id（与 ToName 二选一）
	ToName       string // 群名 / 姓名（全局唯一可免 type）
	ToType       string // personal|group（按名发送时消歧用）
	Text         string
}

// Gateway 飞书发送与解析抽象。
type Gateway interface {
	// Send 发送消息（按 chat_id / open_id）。
	Send(ctx context.Context, out OutMessage) (msgID string, err error)
	// ResolveChatName 群 chat_id → 群名（§2.5.2 入站名称解析）。
	ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error)
	// ResolveUserName open_id → 姓名。
	ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error)
	// ResolveNameToID 名称 → id（§4.5 按名发送；重名返回错误/候选）。
	ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (id string, err error)
}

// Receiver 是长连接入站抽象。
type Receiver interface {
	// Start 建立长连接并阻塞。SDK 负责自动重连；ctx 取消时由进程退出完成收敛。
	Start(ctx context.Context, onInbound func(ctx context.Context, m model.Message) error) error
}

// SeenPersonCollector 拉取 bot 可见的飞书用户候选。
type SeenPersonCollector interface {
	CollectSeenPersons(ctx context.Context) ([]model.SeenPerson, error)
}

// Stub 是占位实现。真实运行应使用 LarkGateway；测试可使用 Fake。
type Stub struct{}

func (Stub) Send(ctx context.Context, out OutMessage) (string, error) { return "", ErrNotImplemented }
func (Stub) ResolveChatName(ctx context.Context, b, c string) (string, error) {
	return "", ErrNotImplemented
}
func (Stub) ResolveUserName(ctx context.Context, b, o string) (string, error) {
	return "", ErrNotImplemented
}
func (Stub) ResolveNameToID(ctx context.Context, b, n, t string) (string, error) {
	return "", ErrNotImplemented
}
func (Stub) CollectSeenPersons(ctx context.Context) ([]model.SeenPerson, error) {
	return nil, ErrNotImplemented
}

// ErrNotImplemented 占位错误。
var ErrNotImplemented = errString("feishu gateway feature not implemented")

type errString string

func (e errString) Error() string { return string(e) }

// LarkGateway 是飞书开放平台真实实现。一个实例对应一个 bot_channel。
type LarkGateway struct {
	botChannelID string
	appID        string
	appSecret    string
	api          *lark.Client
	logger       *slog.Logger
}

// MultiGateway dispatches calls to per-bot gateways by bot_channel_id.
type MultiGateway struct {
	mu       sync.RWMutex
	gateways map[string]*LarkGateway
}

func NewMultiGateway(gateways ...*LarkGateway) *MultiGateway {
	m := &MultiGateway{gateways: map[string]*LarkGateway{}}
	for _, g := range gateways {
		if g != nil {
			m.gateways[g.botChannelID] = g
		}
	}
	return m
}

func (m *MultiGateway) BotChannelIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, 0, len(m.gateways))
	for id := range m.gateways {
		out = append(out, id)
	}
	return out
}

func (m *MultiGateway) Send(ctx context.Context, out OutMessage) (string, error) {
	g, err := m.gateway(out.BotChannelID)
	if err != nil {
		return "", err
	}
	return g.Send(ctx, out)
}

func (m *MultiGateway) ResolveChatName(ctx context.Context, botChannelID, chatID string) (string, error) {
	g, err := m.gateway(botChannelID)
	if err != nil {
		return "", err
	}
	return g.ResolveChatName(ctx, botChannelID, chatID)
}

func (m *MultiGateway) ResolveUserName(ctx context.Context, botChannelID, openID string) (string, error) {
	g, err := m.gateway(botChannelID)
	if err != nil {
		return "", err
	}
	return g.ResolveUserName(ctx, botChannelID, openID)
}

func (m *MultiGateway) ResolveNameToID(ctx context.Context, botChannelID, name, chatType string) (string, error) {
	g, err := m.gateway(botChannelID)
	if err != nil {
		return "", err
	}
	return g.ResolveNameToID(ctx, botChannelID, name, chatType)
}

func (m *MultiGateway) Start(ctx context.Context, onInbound func(ctx context.Context, m model.Message) error) error {
	m.mu.RLock()
	gateways := make([]*LarkGateway, 0, len(m.gateways))
	for _, g := range m.gateways {
		gateways = append(gateways, g)
	}
	m.mu.RUnlock()
	if len(gateways) == 0 {
		return errors.New("no feishu gateways configured")
	}
	errCh := make(chan error, len(gateways))
	for _, g := range gateways {
		go func(g *LarkGateway) {
			errCh <- g.Start(ctx, onInbound)
		}(g)
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (m *MultiGateway) CollectSeenPersons(ctx context.Context) ([]model.SeenPerson, error) {
	m.mu.RLock()
	gateways := make([]*LarkGateway, 0, len(m.gateways))
	for _, g := range m.gateways {
		gateways = append(gateways, g)
	}
	m.mu.RUnlock()
	var out []model.SeenPerson
	for _, g := range gateways {
		persons, err := g.CollectSeenPersons(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, persons...)
	}
	return out, nil
}

func (m *MultiGateway) gateway(botChannelID string) (*LarkGateway, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if g := m.gateways[botChannelID]; g != nil {
		return g, nil
	}
	if len(m.gateways) == 1 && strings.TrimSpace(botChannelID) == "" {
		for _, g := range m.gateways {
			return g, nil
		}
	}
	return nil, fmt.Errorf("feishu bot channel %s not configured", botChannelID)
}

// NewLarkGateway 构造真实飞书网关。appSecret 只来自 env/secret，不落库、不入日志。
func NewLarkGateway(botChannelID, appID, appSecret string, logger *slog.Logger) (*LarkGateway, error) {
	botChannelID = strings.TrimSpace(botChannelID)
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if botChannelID == "" {
		return nil, errors.New("missing bot channel id")
	}
	if appID == "" || appSecret == "" {
		return nil, errors.New("missing feishu app credentials")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &LarkGateway{
		botChannelID: botChannelID,
		appID:        appID,
		appSecret:    appSecret,
		api:          lark.NewClient(appID, appSecret),
		logger:       logger,
	}, nil
}

// Send 通过 REST im/v1/messages 发送文本消息。
func (g *LarkGateway) Send(ctx context.Context, out OutMessage) (string, error) {
	toID := strings.TrimSpace(out.ToID)
	if toID == "" && strings.TrimSpace(out.ToName) != "" {
		id, err := g.ResolveNameToID(ctx, out.BotChannelID, out.ToName, out.ToType)
		if err != nil {
			return "", err
		}
		toID = id
	}
	if toID == "" {
		return "", errors.New("missing feishu receive id")
	}
	content, err := json.Marshal(map[string]string{"text": out.Text})
	if err != nil {
		return "", err
	}
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(receiveIDType(out.ToType, toID)).
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(toID).
			MsgType("text").
			Content(string(content)).
			Build()).
		Build()
	resp, err := g.api.Im.Message.Create(ctx, req)
	if err != nil {
		return "", err
	}
	if resp == nil || !resp.Success() {
		if resp != nil {
			return "", fmt.Errorf("feishu send failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return "", errors.New("feishu send failed: nil response")
	}
	if resp.Data == nil || resp.Data.MessageId == nil || *resp.Data.MessageId == "" {
		return "", errors.New("feishu send succeeded without message_id")
	}
	return *resp.Data.MessageId, nil
}

// Start 启动飞书长连接入站。
func (g *LarkGateway) Start(ctx context.Context, onInbound func(ctx context.Context, m model.Message) error) error {
	if onInbound == nil {
		return errors.New("missing inbound callback")
	}
	disp := dispatcher.NewEventDispatcher("", "").OnP2MessageReadV1(func(ctx context.Context, ev *larkim.P2MessageReadV1) error {
		return nil // 已读回执无需处理，注册空 handler 避免 SDK 报 "not found handler" 刷错误日志
	}).OnP2MessageReceiveV1(func(ctx context.Context, ev *larkim.P2MessageReceiveV1) error {
		msg, err := g.messageFromEvent(ev)
		if err != nil {
			return err
		}
		return onInbound(ctx, msg)
	})
	wsClient := larkws.NewClient(g.appID, g.appSecret,
		larkws.WithEventHandler(disp),
		larkws.WithLogLevel(larkcore.LogLevelError),
		larkws.WithOnReady(func() {
			g.logger.Info("feishu ws ready", "bot_channel", g.botChannelID, "app_id", g.appID)
		}),
		larkws.WithOnReconnecting(func() {
			g.logger.Warn("feishu ws reconnecting", "bot_channel", g.botChannelID)
		}),
		larkws.WithOnReconnected(func() {
			g.logger.Info("feishu ws reconnected", "bot_channel", g.botChannelID)
		}),
		larkws.WithOnDisconnected(func() {
			g.logger.Warn("feishu ws disconnected", "bot_channel", g.botChannelID)
		}),
		larkws.WithOnError(func(err error) {
			g.logger.Error("feishu ws error", "bot_channel", g.botChannelID, "error", err)
		}),
	)
	return wsClient.Start(ctx)
}

func (g *LarkGateway) messageFromEvent(ev *larkim.P2MessageReceiveV1) (model.Message, error) {
	if ev == nil || ev.Event == nil || ev.Event.Message == nil {
		return model.Message{}, errors.New("empty feishu message event")
	}
	m := ev.Event.Message
	msgID := value(m.MessageId)
	chatType, feishuID, err := normalizeChat(m.ChatType, m.ChatId, ev.Event.Sender)
	if err != nil {
		return model.Message{}, err
	}
	senderOpenID := senderOpenID(ev.Event.Sender)
	content := normalizeContent(value(m.Content))
	return model.Message{
		ID:                msgID,
		ChatEntityID:      entityID(g.botChannelID, chatType, feishuID),
		Direction:         model.DirectionIn,
		BotChannelID:      g.botChannelID,
		FeishuMsgID:       msgID,
		ChatType:          chatType,
		SenderOpenID:      senderOpenID,
		IngressProvenance: model.IngressFeishuWSAuthenticated,
		Content:           content,
		CreatedAt:         parseMillisTime(value(m.CreateTime)),
	}, nil
}

// ResolveChatName 通过 REST 获取群名称。
func (g *LarkGateway) ResolveChatName(ctx context.Context, _, chatID string) (string, error) {
	resp, err := g.api.Im.Chat.Get(ctx, larkim.NewGetChatReqBuilder().ChatId(chatID).UserIdType("open_id").Build())
	if err != nil {
		return "", err
	}
	if resp == nil || !resp.Success() {
		if resp != nil {
			return "", fmt.Errorf("feishu get chat failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return "", errors.New("feishu get chat failed: nil response")
	}
	if resp.Data == nil || resp.Data.Name == nil {
		return "", nil
	}
	return *resp.Data.Name, nil
}

// ResolveUserName 通过 REST 获取用户姓名。
func (g *LarkGateway) ResolveUserName(ctx context.Context, _, openID string) (string, error) {
	resp, err := g.api.Contact.User.Get(ctx, larkcontact.NewGetUserReqBuilder().
		UserId(openID).
		UserIdType("open_id").
		DepartmentIdType("open_department_id").
		Build())
	if err != nil {
		return "", err
	}
	if resp == nil || !resp.Success() {
		if resp != nil {
			return "", fmt.Errorf("feishu get user failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return "", errors.New("feishu get user failed: nil response")
	}
	if resp.Data == nil || resp.Data.User == nil || resp.Data.User.Name == nil {
		return "", nil
	}
	return *resp.Data.User.Name, nil
}

// ResolveNameToID 当前只对群名做精确搜索；个人按姓名反查容易重名，保守拒绝。
func (g *LarkGateway) ResolveNameToID(ctx context.Context, _, name, chatType string) (string, error) {
	if model.ChatType(chatType) != model.ChatGroup {
		return "", errors.New("resolve personal name to open_id is ambiguous")
	}
	resp, err := g.api.Im.Chat.Search(ctx, larkim.NewSearchChatReqBuilder().
		Query(name).
		UserIdType("open_id").
		PageSize(20).
		Build())
	if err != nil {
		return "", err
	}
	if resp == nil || !resp.Success() {
		if resp != nil {
			return "", fmt.Errorf("feishu search chat failed: code=%d msg=%s", resp.Code, resp.Msg)
		}
		return "", errors.New("feishu search chat failed: nil response")
	}
	if resp.Data == nil {
		return "", errors.New("chat name not found")
	}
	var matches []string
	for _, item := range resp.Data.Items {
		if item == nil || item.Name == nil || item.ChatId == nil {
			continue
		}
		if *item.Name == name {
			matches = append(matches, *item.ChatId)
		}
	}
	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", errors.New("chat name not found")
	default:
		return "", errors.New("chat name is ambiguous")
	}
}

func (g *LarkGateway) CollectSeenPersons(ctx context.Context) ([]model.SeenPerson, error) {
	var out []model.SeenPerson
	pageToken := ""
	for {
		builder := larkim.NewListChatReqBuilder().UserIdType("open_id").PageSize(100).Types("group")
		if pageToken != "" {
			builder.PageToken(pageToken)
		}
		resp, err := g.api.Im.Chat.List(ctx, builder.Build())
		if err != nil {
			return nil, err
		}
		if resp == nil || !resp.Success() {
			if resp != nil {
				return nil, fmt.Errorf("feishu list chats failed: code=%d msg=%s", resp.Code, resp.Msg)
			}
			return nil, errors.New("feishu list chats failed: nil response")
		}
		if resp.Data != nil {
			for _, chat := range resp.Data.Items {
				if chat == nil || chat.ChatId == nil || *chat.ChatId == "" {
					continue
				}
				members, err := g.collectChatMembers(ctx, *chat.ChatId)
				if err != nil {
					return nil, err
				}
				out = append(out, members...)
			}
			if resp.Data.HasMore != nil && *resp.Data.HasMore && resp.Data.PageToken != nil {
				pageToken = *resp.Data.PageToken
				continue
			}
		}
		break
	}
	return out, nil
}

func (g *LarkGateway) collectChatMembers(ctx context.Context, chatID string) ([]model.SeenPerson, error) {
	var out []model.SeenPerson
	pageToken := ""
	for {
		builder := larkim.NewGetChatMembersReqBuilder().ChatId(chatID).MemberIdType("open_id").PageSize(100)
		if pageToken != "" {
			builder.PageToken(pageToken)
		}
		resp, err := g.api.Im.ChatMembers.Get(ctx, builder.Build())
		if err != nil {
			return nil, err
		}
		if resp == nil || !resp.Success() {
			if resp != nil {
				return nil, fmt.Errorf("feishu list chat members failed: code=%d msg=%s", resp.Code, resp.Msg)
			}
			return nil, errors.New("feishu list chat members failed: nil response")
		}
		if resp.Data != nil {
			for _, member := range resp.Data.Items {
				if member == nil || member.MemberId == nil || *member.MemberId == "" {
					continue
				}
				out = append(out, model.SeenPerson{
					OpenID:       *member.MemberId,
					BotChannelID: g.botChannelID,
					Name:         value(member.Name),
					Source:       "group",
					LastSeenAt:   time.Now().UTC(),
				})
			}
			if resp.Data.HasMore != nil && *resp.Data.HasMore && resp.Data.PageToken != nil {
				pageToken = *resp.Data.PageToken
				continue
			}
		}
		break
	}
	return out, nil
}

func normalizeChat(chatTypePtr, chatIDPtr *string, sender *larkim.EventSender) (model.ChatType, string, error) {
	switch value(chatTypePtr) {
	case "group", "topic_group":
		chatID := value(chatIDPtr)
		if chatID == "" {
			return "", "", errors.New("missing group chat_id")
		}
		return model.ChatGroup, chatID, nil
	case "p2p", "":
		openID := senderOpenID(sender)
		if openID == "" {
			return "", "", errors.New("missing sender open_id")
		}
		return model.ChatPersonal, openID, nil
	default:
		return "", "", fmt.Errorf("unsupported feishu chat_type %q", value(chatTypePtr))
	}
}

func senderOpenID(sender *larkim.EventSender) string {
	if sender == nil || sender.SenderId == nil || sender.SenderId.OpenId == nil {
		return ""
	}
	return *sender.SenderId.OpenId
}

func normalizeContent(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return `{"text":""}`
	}
	var js map[string]any
	if json.Unmarshal([]byte(raw), &js) == nil {
		return raw
	}
	b, _ := json.Marshal(map[string]string{"text": raw})
	return string(b)
}

func parseMillisTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	ms, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func entityID(botChannelID string, chatType model.ChatType, feishuID string) string {
	return botChannelID + ":" + string(chatType) + ":" + feishuID
}

func receiveIDType(toType, toID string) string {
	if model.ChatType(toType) == model.ChatGroup || strings.HasPrefix(toID, "oc_") {
		return "chat_id"
	}
	return "open_id"
}

func value(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Fake 是本地测试发送器：不访问飞书，只记录发送请求。
type Fake struct {
	mu   sync.Mutex
	Sent []OutMessage
	Seen []model.SeenPerson
}

func (f *Fake) Send(_ context.Context, out OutMessage) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Sent = append(f.Sent, out)
	return fmt.Sprintf("fake_msg_%d", len(f.Sent)), nil
}

func (f *Fake) ResolveChatName(_ context.Context, _, chatID string) (string, error) {
	return chatID, nil
}

func (f *Fake) ResolveUserName(_ context.Context, _, openID string) (string, error) {
	return openID, nil
}

func (f *Fake) ResolveNameToID(_ context.Context, _, name, _ string) (string, error) {
	return name, nil
}

func (f *Fake) CollectSeenPersons(_ context.Context) ([]model.SeenPerson, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]model.SeenPerson, len(f.Seen))
	copy(out, f.Seen)
	return out, nil
}

func (f *Fake) SentMessages() []OutMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]OutMessage, len(f.Sent))
	copy(out, f.Sent)
	return out
}
