package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/extseat"
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
)

type r53FeishuCopyTestGateway struct {
	feishu.Stub
	sent []feishu.OutMessage
}

func (g *r53FeishuCopyTestGateway) Send(_ context.Context, message feishu.OutMessage) (string, error) {
	g.sent = append(g.sent, message)
	return "copy-message", nil
}

func TestR53FeishuCopySenderIdempotencyRecipient(t *testing.T) {
	event := extseat.FeishuCopyEvent{
		ID: "copy-event-one", Issuer: "issuer-app-42", Subject: "ou_subject_42",
		ExternalName: "external-seat.zzc", TargetName: "ou_target_42", Body: "copy body",
	}
	channels := []model.BotChannel{
		{ID: "wrong-app-channel", AppID: "different-issuer", Active: true, CanSend: true},
		{ID: "inactive-channel", AppID: event.Issuer, Active: false, CanSend: true},
		{ID: "cannot-send-channel", AppID: event.Issuer, Active: true, CanSend: false},
		{ID: "selected-bot-channel", AppID: event.Issuer, Active: true, CanSend: true},
	}
	gateway := &r53FeishuCopyTestGateway{}
	sender := newR53FeishuCopySender(func(context.Context) ([]model.BotChannel, error) {
		return channels, nil
	}, gateway)
	wantRequestID := feishu.DeriveRequestID("reply", event.ID, "mcp-feishu-copy")
	for attempt := 0; attempt < 2; attempt++ {
		receipt, err := sender(context.Background(), event)
		if err != nil {
			t.Fatalf("MCP_COPY_REQUEST_ID attempt=%d sender rejected stable request ID: %v", attempt, err)
		}
		if receipt.BotChannelID != "selected-bot-channel" || receipt.MessageID != "copy-message" {
			t.Fatalf("MCP_COPY_SENDER_SUCCESS attempt=%d receipt=%+v", attempt, receipt)
		}
		message := gateway.sent[len(gateway.sent)-1]
		if message.RequestID != wantRequestID {
			t.Fatalf("MCP_COPY_REQUEST_ID attempt=%d got=%q want=%q", attempt, message.RequestID, wantRequestID)
		}
		if message.BotChannelID != "selected-bot-channel" {
			t.Fatalf("MCP_COPY_CHANNEL_MATCH attempt=%d got=%q", attempt, message.BotChannelID)
		}
		if message.ToID != event.Subject || message.ToType != "personal" {
			t.Fatalf("MCP_COPY_RECIPIENT attempt=%d to_id=%q to_type=%q", attempt, message.ToID, message.ToType)
		}
	}
	if gateway.sent[0].RequestID != gateway.sent[1].RequestID {
		t.Fatalf("MCP_COPY_RETRY_ID_CHANGED first=%q second=%q", gateway.sent[0].RequestID, gateway.sent[1].RequestID)
	}

	other := event
	other.ID = "copy-event-two"
	if _, err := sender(context.Background(), other); err != nil {
		t.Fatalf("MCP_COPY_DIFFERENT_EVENT_SEND err=%v", err)
	}
	third := gateway.sent[2]
	if third.RequestID != feishu.DeriveRequestID("reply", other.ID, "mcp-feishu-copy") || third.RequestID == gateway.sent[0].RequestID {
		t.Fatalf("MCP_COPY_DIFFERENT_EVENT_ID got=%q first=%q", third.RequestID, gateway.sent[0].RequestID)
	}
}

func TestR53FeishuCopySenderRejectsUnusableChannels(t *testing.T) {
	event := extseat.FeishuCopyEvent{ID: "copy-negative", Issuer: "issuer-app-42", Subject: "ou_subject_42", ExternalName: "external-seat.zzc", TargetName: "ou_target_42", Body: "copy body"}
	tests := []struct {
		name     string
		marker   string
		channels []model.BotChannel
	}{
		{name: "no_match", marker: "MCP_COPY_CHANNEL_NO_MATCH", channels: []model.BotChannel{
			{ID: "wrong-app", AppID: "other-issuer", Active: true, CanSend: true},
			{ID: "inactive", AppID: event.Issuer, Active: false, CanSend: true},
		}},
		{name: "ambiguous", marker: "MCP_COPY_CHANNEL_AMBIGUOUS", channels: []model.BotChannel{
			{ID: "bot-one", AppID: event.Issuer, Active: true, CanSend: true},
			{ID: "bot-two", AppID: event.Issuer, Active: true, CanSend: true},
		}},
		{name: "cannot_send", marker: "MCP_COPY_CHANNEL_CAN_SEND", channels: []model.BotChannel{
			{ID: "disabled-send", AppID: event.Issuer, Active: true, CanSend: false},
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &r53FeishuCopyTestGateway{}
			sender := newR53FeishuCopySender(func(context.Context) ([]model.BotChannel, error) {
				return test.channels, nil
			}, gateway)
			_, err := sender(context.Background(), event)
			if err == nil || len(gateway.sent) != 0 {
				t.Fatalf("%s err=%v gateway_calls=%d", test.marker, err, len(gateway.sent))
			}
		})
	}
}

func TestR53ConfigFromEnv(t *testing.T) {
	const res = "https://dw123.cmjrt.com/mcp"
	cases := []struct {
		name     string
		on       string
		tenant   string
		resource string
		newplat  bool
		wantOn   bool
		copyOff  string
		wantErr  error
	}{
		{name: "off_by_default", on: "", tenant: "tk", resource: res, newplat: true},
		{name: "off_when_not_1", on: "true", tenant: "tk", resource: res, newplat: true},
		{name: "needs_newplat", on: "1", tenant: "tk", resource: res, newplat: false, wantOn: true, wantErr: errR53NeedsNewplat},
		{name: "needs_tenant", on: "1", tenant: " ", resource: res, newplat: true, wantOn: true, wantErr: errR53TenantKey},
		{name: "needs_resource", on: "1", tenant: "tk", resource: "", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_http_rejected", on: "1", tenant: "tk", resource: "http://dw123.cmjrt.com/mcp", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_relative_rejected", on: "1", tenant: "tk", resource: "/mcp", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_query_rejected", on: "1", tenant: "tk", resource: res + "?a=1", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "resource_fragment_rejected", on: "1", tenant: "tk", resource: res + "\x23x", newplat: true, wantOn: true, wantErr: errR53Resource},
		{name: "ok", on: "1", tenant: "tk-2d31", resource: res, newplat: true, wantOn: true},
		{name: "copy_disabled", on: "1", tenant: "tk-2d31", resource: res, newplat: true, wantOn: true, copyOff: "0"},
		{name: "copy_switch_invalid", on: "1", tenant: "tk", resource: res, newplat: true, wantOn: true, copyOff: "false", wantErr: errR53CopySwitch},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("DW_R53_EXTSEAT", c.on)
			t.Setenv("DW_FEISHU_TEAM_TENANT_KEY", c.tenant)
			t.Setenv("DW_MCP_RESOURCE", c.resource)
			t.Setenv("DW_MCP_FEISHU_COPY_ENABLED", c.copyOff)
			cfg, on, err := r53ConfigFromEnv(c.newplat)
			if on != c.wantOn || !errors.Is(err, c.wantErr) {
				t.Fatalf("on=%v err=%v want on=%v err=%v", on, err, c.wantOn, c.wantErr)
			}
			if c.name == "ok" && (cfg.tenantKey != "tk-2d31" || cfg.resource != res || cfg.copyDisabled) {
				t.Fatalf("cfg=%+v", cfg)
			}
			if c.name == "copy_disabled" && !cfg.copyDisabled {
				t.Fatalf("copy switch not disabled: %+v", cfg)
			}
		})
	}
}

func TestR53FeishuCopyEnabledByDefault(t *testing.T) {
	t.Setenv("DW_R53_EXTSEAT", "1")
	t.Setenv("DW_FEISHU_TEAM_TENANT_KEY", "team")
	t.Setenv("DW_MCP_RESOURCE", "https://dw123.cmjrt.com/mcp")
	t.Setenv("DW_MCP_FEISHU_COPY_ENABLED", "")
	cfg, _, err := r53ConfigFromEnv(true)
	if err != nil || cfg.copyDisabled {
		t.Fatalf("MCP_COPY_DEFAULT_ENABLED cfg=%+v err=%v", cfg, err)
	}
}

// 开启时：0105 表齐 ⇒ 过表核对、停在「D 路未并入」；删一张表 ⇒ 报出缺的那张表名。
func TestWireR53SchemaCheckPG(t *testing.T) {
	t.Setenv("DW_R53_EXTSEAT", "1")
	dsn := pgSchemaDSN(t, false)
	ctx := context.Background()
	_, pool, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer closeStore()
	cfg := r53Config{publicOrigin: "https://dw123.cmjrt.com", tenantKey: "tk", resource: "https://dw123.cmjrt.com/mcp"}
	if err := wireR53(ctx, cfg, pgTableChecker{pool: pool}, nil, r53Dependencies{}); !errors.Is(err, errR53GroupsMissing) {
		t.Fatalf("all tables present: err=%v want errR53GroupsMissing", err)
	}
	if _, err := pool.Exec(ctx, repo.SystemCtx(), `DROP TABLE np_extseat_outbox`); err != nil {
		t.Fatal(err)
	}
	err = wireR53(ctx, cfg, pgTableChecker{pool: pool}, nil, r53Dependencies{})
	if err == nil || errors.Is(err, errR53GroupsMissing) || !strings.Contains(err.Error(), "np_extseat_outbox") {
		t.Fatalf("dropped table: err=%v want missing np_extseat_outbox", err)
	}
}

// r52 受信来源接到 r53 网关：名单空 ⇒ 只认 PublicOrigin；名单含 :50443 ⇒ 精确认它，错端口、http、多余路径都拒。
func TestR53TrustedOriginsAdapter(t *testing.T) {
	const pub = "https://dw123.cmjrt.com"
	empty := r53TrustedOrigins{publicOrigin: pub}
	if !empty.TrustedOrigin(context.Background(), pub) || empty.TrustedOrigin(context.Background(), pub+":50443") {
		t.Fatal("empty list must accept only PublicOrigin")
	}
	list, err := trustedorigin.ParseList(pub + ":50443")
	if err != nil {
		t.Fatal(err)
	}
	with := r53TrustedOrigins{publicOrigin: pub, trusted: list}
	for origin, want := range map[string]bool{
		pub: true, pub + ":50443": true, "HTTPS://DW123.CMJRT.COM:50443": true,
		pub + ":50444": false, "http://dw123.cmjrt.com:50443": false, "https://evil.example": false,
	} {
		if got := with.TrustedOrigin(context.Background(), origin); got != want {
			t.Fatalf("origin %q: got %v want %v", origin, got, want)
		}
	}
}
