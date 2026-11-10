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
