package feishu

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// WebhookMessageFromPayload normalizes a Feishu webhook event payload into the
// same model.Message shape produced by the long-connection receiver.
func WebhookMessageFromPayload(botChannelID string, data []byte) (model.Message, error) {
	botChannelID = strings.TrimSpace(botChannelID)
	if botChannelID == "" {
		botChannelID = "default"
	}
	if msg, ok, err := fakeWebhookMessage(botChannelID, data); ok || err != nil {
		return msg, err
	}
	if msg, ok, err := v2WebhookMessage(botChannelID, data); ok || err != nil {
		return msg, err
	}
	if msg, ok, err := legacyWebhookMessage(botChannelID, data); ok || err != nil {
		return msg, err
	}
	return model.Message{}, errors.New("unsupported feishu webhook event")
}

type fakeWebhookPayload struct {
	MsgID        string          `json:"msg_id"`
	ChatType     string          `json:"chat_type"`
	ChatID       string          `json:"chat_id"`
	OpenID       string          `json:"open_id"`
	SenderOpenID string          `json:"sender_open_id"`
	Text         string          `json:"text"`
	Raw          json.RawMessage `json:"raw"`
}

func fakeWebhookMessage(botChannelID string, data []byte) (model.Message, bool, error) {
	var p fakeWebhookPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return model.Message{}, false, err
	}
	if p.MsgID == "" && p.ChatType == "" && p.ChatID == "" && p.OpenID == "" && p.SenderOpenID == "" && p.Text == "" && len(p.Raw) == 0 {
		return model.Message{}, false, nil
	}
	chatType := model.ChatType(p.ChatType)
	if chatType == "" {
		chatType = model.ChatPersonal
	}
	feishuID := p.ChatID
	if chatType == model.ChatPersonal {
		feishuID = firstNonEmpty(p.OpenID, feishuID, p.SenderOpenID)
	}
	if feishuID == "" {
		return model.Message{}, true, errors.New("missing chat_id/open_id")
	}
	content := string(p.Raw)
	if len(p.Raw) == 0 {
		b, _ := json.Marshal(map[string]string{"text": p.Text})
		content = string(b)
	}
	msgID := firstNonEmpty(p.MsgID, "webhook-"+time.Now().UTC().Format("20060102150405.000000000"))
	return model.Message{
		ID:           msgID,
		ChatEntityID: entityID(botChannelID, chatType, feishuID),
		Direction:    model.DirectionIn,
		BotChannelID: botChannelID,
		FeishuMsgID:  msgID,
		ChatType:     chatType,
		SenderOpenID: firstNonEmpty(p.SenderOpenID, p.OpenID),
		Content:      content,
		CreatedAt:    time.Now().UTC(),
	}, true, nil
}

type webhookV2Payload struct {
	Schema string `json:"schema"`
	Header struct {
		EventID    string `json:"event_id"`
		EventType  string `json:"event_type"`
		CreateTime string `json:"create_time"`
		Token      string `json:"token"`
	} `json:"header"`
	Event struct {
		Sender  webhookSender  `json:"sender"`
		Message webhookMessage `json:"message"`
	} `json:"event"`
}

type webhookSender struct {
	SenderID struct {
		OpenID  string `json:"open_id"`
		UserID  string `json:"user_id"`
		UnionID string `json:"union_id"`
	} `json:"sender_id"`
}

type webhookMessage struct {
	MessageID  string `json:"message_id"`
	ChatID     string `json:"chat_id"`
	ChatType   string `json:"chat_type"`
	Content    string `json:"content"`
	CreateTime string `json:"create_time"`
}

func v2WebhookMessage(botChannelID string, data []byte) (model.Message, bool, error) {
	var p webhookV2Payload
	if err := json.Unmarshal(data, &p); err != nil {
		return model.Message{}, false, err
	}
	if p.Event.Message.MessageID == "" && p.Header.EventType == "" {
		return model.Message{}, false, nil
	}
	if p.Header.EventType != "" && p.Header.EventType != "im.message.receive_v1" {
		return model.Message{}, true, fmt.Errorf("unsupported feishu event_type %q", p.Header.EventType)
	}
	sender := p.Event.Sender.SenderID.OpenID
	chatType, feishuID, err := normalizeWebhookChat(p.Event.Message.ChatType, p.Event.Message.ChatID, sender)
	if err != nil {
		return model.Message{}, true, err
	}
	msgID := firstNonEmpty(p.Event.Message.MessageID, p.Header.EventID)
	created := parseMillisTime(firstNonEmpty(p.Event.Message.CreateTime, p.Header.CreateTime))
	return model.Message{
		ID:           msgID,
		ChatEntityID: entityID(botChannelID, chatType, feishuID),
		Direction:    model.DirectionIn,
		BotChannelID: botChannelID,
		FeishuMsgID:  msgID,
		ChatType:     chatType,
		SenderOpenID: sender,
		Content:      normalizeContent(p.Event.Message.Content),
		CreatedAt:    created,
	}, true, nil
}

type legacyWebhookPayload struct {
	Type  string `json:"type"`
	Token string `json:"token"`
	Event struct {
		Type          string `json:"type"`
		OpenMessageID string `json:"open_message_id"`
		MessageID     string `json:"message_id"`
		OpenChatID    string `json:"open_chat_id"`
		ChatID        string `json:"chat_id"`
		ChatType      string `json:"chat_type"`
		OpenID        string `json:"open_id"`
		SenderOpenID  string `json:"sender_open_id"`
		Text          string `json:"text"`
		Content       string `json:"content"`
		CreateTime    string `json:"create_time"`
	} `json:"event"`
}

func legacyWebhookMessage(botChannelID string, data []byte) (model.Message, bool, error) {
	var p legacyWebhookPayload
	if err := json.Unmarshal(data, &p); err != nil {
		return model.Message{}, false, err
	}
	if p.Type != "event_callback" && p.Event.Type == "" {
		return model.Message{}, false, nil
	}
	if p.Event.Type != "" && p.Event.Type != "message" {
		return model.Message{}, true, fmt.Errorf("unsupported legacy feishu event type %q", p.Event.Type)
	}
	sender := firstNonEmpty(p.Event.SenderOpenID, p.Event.OpenID)
	chatType, feishuID, err := normalizeWebhookChat(p.Event.ChatType, firstNonEmpty(p.Event.ChatID, p.Event.OpenChatID), sender)
	if err != nil {
		return model.Message{}, true, err
	}
	content := firstNonEmpty(p.Event.Content, p.Event.Text)
	msgID := firstNonEmpty(p.Event.MessageID, p.Event.OpenMessageID)
	return model.Message{
		ID:           msgID,
		ChatEntityID: entityID(botChannelID, chatType, feishuID),
		Direction:    model.DirectionIn,
		BotChannelID: botChannelID,
		FeishuMsgID:  msgID,
		ChatType:     chatType,
		SenderOpenID: sender,
		Content:      normalizeContent(content),
		CreatedAt:    parseMillisTime(p.Event.CreateTime),
	}, true, nil
}

func normalizeWebhookChat(chatType, chatID, senderOpenID string) (model.ChatType, string, error) {
	switch strings.TrimSpace(chatType) {
	case "group", "topic_group":
		if strings.TrimSpace(chatID) == "" {
			return "", "", errors.New("missing group chat_id")
		}
		return model.ChatGroup, strings.TrimSpace(chatID), nil
	case "p2p", "personal", "":
		if strings.TrimSpace(senderOpenID) == "" {
			return "", "", errors.New("missing sender open_id")
		}
		return model.ChatPersonal, strings.TrimSpace(senderOpenID), nil
	default:
		return "", "", fmt.Errorf("unsupported feishu chat_type %q", chatType)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
