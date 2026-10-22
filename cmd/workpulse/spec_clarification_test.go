package main

import (
	"github.com/zhangzongchu2019/dingwei/internal/feishu"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

func TestClarificationStartupFlagsIndependent(t *testing.T) {
	for _, outbound := range []string{"", "0", "1"} {
		for _, thread := range []string{"", "0", "1", " true ", " 1 "} {
			for _, reply := range []string{"", "0", "1", "true", " 1 "} {
				t.Setenv("DW_PILOT_QUESTION_FEISHU_ENABLED", outbound)
				t.Setenv("DW_SPEC_CLARIFICATION_ENABLED", thread)
				t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", reply)
				h := &m8.Hub{QuestionFeishuEnabled: outbound == "1"}
				configureSpecClarification(h)
				if h.SpecClarificationEnabled != (thread == "1" || thread == " 1 ") || h.SpecClarificationFeishuReplyEnabled != (reply == "1" || reply == " 1 ") || h.QuestionFeishuEnabled != (outbound == "1") {
					t.Fatal("CLARIFICATION_SWITCH_COUPLED", outbound, thread, reply)
				}
				t.Setenv("DW_SPEC_CLARIFICATION_ENABLED", "opposite")
				t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_REPLY_ENABLED", "opposite")
				if h.SpecClarificationEnabled != (thread == "1" || thread == " 1 ") || h.SpecClarificationFeishuReplyEnabled != (reply == "1" || reply == " 1 ") {
					t.Fatal("CLARIFICATION_SWITCH_NOT_STARTUP_SNAPSHOT")
				}
			}
		}
	}
}

type testClarificationNative struct{ feishu.Gateway }

func (testClarificationNative) ReplyApplication(issuer string) (string, bool) {
	return "app-pinned", issuer == "native"
}
func TestClarificationReplyRequiresPinnedTenantAndNativeCapability(t *testing.T) {
	t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_TENANTS_JSON", `{"native":"tenant-one","unknown":"tenant-two"}`)
	h := &m8.Hub{SpecClarificationFeishuReplyEnabled: true, QuestionFeishuEnabled: false}
	if e := configureClarificationReplyAuthority(h, testClarificationNative{feishu.Stub{}}); e != nil || len(h.ClarificationReplyAuthorities) != 1 || h.ClarificationReplyAuthorities["native"].Tenant != "tenant-one" || h.ClarificationReceiptSender == nil || h.QuestionFeishuEnabled {
		t.Fatal("CLARIFICATION_UNPINNED_AUTHORITY_OR_COUPLED_D3", e, h.ClarificationReplyAuthorities)
	}
	t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_TENANTS_JSON", `{"native":"replacement"}`)
	if h.ClarificationReplyAuthorities["native"].Tenant != "tenant-one" {
		t.Fatal("CLARIFICATION_AUTHORITY_NOT_STARTUP_SNAPSHOT")
	}
	if e := configureClarificationReplyAuthority(h, feishu.Stub{}); e != nil || len(h.ClarificationReplyAuthorities) != 0 || h.ClarificationReceiptSender != nil {
		t.Fatal("CLARIFICATION_UNVERIFIED_GATEWAY_ENABLED", e)
	}
	for _, bad := range []string{`{"native":1}`, `{"native":""}`, `{" native":"tenant"}`, `{"native":"tenant\n"}`, `{"native":`} {
		t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_TENANTS_JSON", bad)
		if e := configureClarificationReplyAuthority(h, feishu.Stub{}); e == nil {
			t.Fatal("CLARIFICATION_BAD_AUTHORITY_CONFIG_ACCEPTED", bad)
		}
	}
}

func TestSingleTenantStartupUsesConfiguredNativeApplicationsWithoutMapping(t *testing.T) {
	for _, mapping := range []string{"", `{"native":"tenant-one"}`} {
		t.Run(mapping, func(t *testing.T) {
			t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_TENANTS_JSON", mapping)
			native, err := feishu.NewLarkGateway("native", "app-one", "SYNTHETIC-ONLY", nil)
			if err != nil {
				t.Fatal(err)
			}
			other, err := feishu.NewLarkGateway("other", "app-two", "SYNTHETIC-ONLY", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, gateway := range []feishu.Gateway{native, feishu.NewMultiGateway(native, other)} {
				h := &m8.Hub{SpecClarificationFeishuReplyEnabled: true}
				if err = configureClarificationReplyAuthority(h, gateway); err != nil {
					t.Fatal(err)
				}
				expected := ""
				if mapping != "" {
					expected = "tenant-one"
				}
				if a, ok := h.ClarificationReplyAuthorities["native"]; !ok || a.AppID != "app-one" || a.Tenant != expected || h.ClarificationReceiptSender == nil {
					t.Fatal("SINGLE_TENANT_NATIVE_STARTUP_UNAVAILABLE", h.ClarificationReplyAuthorities)
				}
				if _, multi := gateway.(*feishu.MultiGateway); multi {
					if a, ok := h.ClarificationReplyAuthorities["other"]; !ok || a.AppID != "app-two" || a.Tenant != "" {
						t.Fatal("SINGLE_TENANT_PER_ISSUER_FALLBACK_LOST")
					}
				}
			}
		})
	}
	t.Setenv("DW_SPEC_CLARIFICATION_FEISHU_TENANTS_JSON", "")
	h := &m8.Hub{SpecClarificationFeishuReplyEnabled: true}
	if err := configureClarificationReplyAuthority(h, feishu.Stub{}); err != nil || len(h.ClarificationReplyAuthorities) != 0 || h.ClarificationReceiptSender != nil {
		t.Fatal("SINGLE_TENANT_UNVERIFIED_FALLBACK_ENABLED")
	}
}
