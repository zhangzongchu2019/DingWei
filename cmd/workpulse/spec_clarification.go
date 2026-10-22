package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

// Startup snapshot. The independent inbound flag does not enable D3 outbound
// and the outbound switch never enables inbound interpretation of a reply.
func configureSpecClarification(h *m8.Hub) {
	h.SpecClarificationEnabled = strings.TrimSpace(os.Getenv("DW_SPEC_CLARIFICATION_ENABLED")) == "1"
	h.SpecClarificationFeishuReplyEnabled = strings.TrimSpace(os.Getenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED")) == "1"
}

func configureClarificationReplyAuthority(h *m8.Hub, gateway feishu.Gateway) error {
	h.ClarificationReplyAuthorities = map[string]m8.ClarificationReplyAuthority{}
	h.ClarificationReceiptSender = nil
	var tenants map[string]string
	raw := strings.TrimSpace(os.Getenv("DW_SPEC_CLARIFICATION_FEISHU_TENANTS_JSON"))
	if raw != "" {
		if len(raw) > 16<<10 || json.Unmarshal([]byte(raw), &tenants) != nil || len(tenants) > 64 {
			return errors.New("invalid clarification tenant configuration")
		}
	}
	capable, ok := gateway.(interface{ ReplyApplication(string) (string, bool) })
	issuers := map[string]bool{}
	for issuer, tenant := range tenants {
		if issuer == "" || len(issuer) > 256 || tenant == "" || len(tenant) > 256 || strings.TrimSpace(issuer) != issuer || strings.TrimSpace(tenant) != tenant || strings.ContainsAny(issuer+tenant, "\x00\r\n") {
			return errors.New("invalid clarification authority identity")
		}
		issuers[issuer] = true
	}
	// Missing mapping is supported only for configured native SDK issuers.
	// FCS/proxy/Fake/Stub cannot manufacture this verified capability.
	if native, listed := gateway.(interface{ BotChannelIDs() []string }); ok && listed {
		for _, issuer := range native.BotChannelIDs() {
			if issuer != "" && len(issuer) <= 256 && strings.TrimSpace(issuer) == issuer && !strings.ContainsAny(issuer, "\x00\r\n") {
				issuers[issuer] = true
			}
		}
	}
	for issuer := range issuers {
		if !ok {
			continue
		}
		if app, ready := capable.ReplyApplication(issuer); ready && app != "" && len(app) <= 256 && strings.TrimSpace(app) == app && !strings.ContainsAny(app, "\x00\r\n") {
			h.ClarificationReplyAuthorities[issuer] = m8.ClarificationReplyAuthority{AppID: app, Tenant: tenants[issuer]}
		}
	}
	if !ok || !h.SpecClarificationFeishuReplyEnabled {
		return nil
	}
	h.ClarificationReceiptSender = func(ctx context.Context, issuer, subject, id, text string) (string, error) {
		return feishu.SendPilotQuestion(ctx, gateway, feishu.OutMessage{BotChannelID: issuer, ToID: subject, ToType: "personal", Text: text, RequestID: feishu.DeriveRequestID("reply", id, "spec-clarification-receipt")})
	}
	return nil
}
