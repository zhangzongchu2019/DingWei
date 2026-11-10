package extshared

import (
	"os"
	"testing"
)

func TestExternalReplyDefaultAndExactConfig(t *testing.T) {
	old, ok := os.LookupEnv(ExternalAutoReplyDisabledEnv)
	os.Unsetenv(ExternalAutoReplyDisabledEnv)
	defer func() {
		if ok {
			os.Setenv(ExternalAutoReplyDisabledEnv, old)
		} else {
			os.Unsetenv(ExternalAutoReplyDisabledEnv)
		}
	}()
	if ExternalAutoReplyAllowed("opus50tc2k") || ExternalAutoReplyAllowed("") || ExternalAutoReplyAllowed("opus50tc2k.") {
		t.Fatal("MCP_EXTERNAL_REPLY_IDENTITY_FAIL_CLOSED")
	}
	if ExternalAutoReplyAllowed("opus50tc2k.zzc") || ExternalAutoReplyAllowed("prodops.zzc") || !ExternalAutoReplyAllowed("worker.zzc") {
		t.Fatal("MCP_EXTERNAL_REPLY_DEFAULT")
	}
	os.Setenv(ExternalAutoReplyDisabledEnv, "worker.zzc,other.zzc")
	if ExternalAutoReplyAllowed("worker.zzc") || !ExternalAutoReplyAllowed("worker2.zzc") || !ExternalAutoReplyAllowed("worker.zzc.attacker") {
		t.Fatal("MCP_EXTERNAL_REPLY_EXACT_CONFIG")
	}
	os.Setenv(ExternalAutoReplyDisabledEnv, "*")
	if ExternalAutoReplyAllowed("any.zzc") {
		t.Fatal("MCP_EXTERNAL_REPLY_CONFIG_FAIL_CLOSED")
	}
}

func TestExternalSenderHeader(t *testing.T) {
	got := ExternalSenderHeader("asist_AG3.zzc", "授权人张", "zzc", "target.zzc")
	want := "【来自外部 Agent asist_AG3.zzc（MCP，授权人 授权人张）→ target.zzc；回复请发给 asist_AG3.zzc】"
	if got != want {
		t.Fatalf("MCP_COPY_HEADER_SHARED ExternalSenderHeader=%q want=%q", got, want)
	}
}
