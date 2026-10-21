package fcs

import (
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestClarificationFCSUnverifiedServiceCannotAssertReplyTrust(t *testing.T) {
	metadata := &model.FeishuReplyMetadata{Authenticated: true, ParentID: "om_notice", RootID: "om_root", AppID: "app", Tenant: "tenant", EventID: "event"}
	m := envToMessage(InboundEnvelope{BotChannelID: "bot", FeishuReply: metadata, Source: SourceStub{Kind: "feishu", Provenance: string(model.IngressFeishuWSAuthenticated), AttestedBy: "fcs", Attestation: AttestationStub{ServiceID: "self-report", Tenant: "tenant", KeyEpoch: 99, Signature: []byte("not verified")}}})
	if m.FeishuReply == nil || m.FeishuReply.ParentID != "om_notice" || m.FeishuReply.Authenticated {
		t.Fatal("CLARIFICATION_FCS_SELF_REPORTED_TRUST_ACCEPTED", m.FeishuReply)
	}
	if !metadata.Authenticated {
		t.Fatal("CLARIFICATION_FCS_MUTATED_SOURCE_METADATA")
	}
}
