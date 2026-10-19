package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// Actual CLI -> RegisterNode -> normalizeQuotaRegistration -> local disposable PG.
// No server, external PG, real node, master key file or host operation is used.
func TestHostedAdminQuotaRegistrationCLIToNormalize(t *testing.T) {
	dsn := pgtest.DSN(t)
	revision := strings.Repeat("a", 64)
	out, err := hostedAdminRun(t, dsn, "", "register-node", "--id", "nd_soft_cli", "--name", "soft-cli", "--quota-mode", "soft", "--quota-policy-revision", revision, "--labels", `{"pool":"fixture","allow_soft":"true","quota_mode":"hard","quota_policy_revision":"old"}`)
	if err != nil || strings.Count(out, "节点凭据") != 1 {
		t.Fatalf("soft CLI registration failed or credential output count invalid: %v", err)
	}
	labels := map[string]string{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(hostedAdminDump(t, dsn, `SELECT labels::text FROM g4_node WHERE id='nd_soft_cli'`))), &labels); err != nil {
		t.Fatal(err)
	}
	if labels["quota_mode"] != "soft" || labels["quota_policy_revision"] != revision || labels["pool"] != "fixture" || labels["allow_soft"] != "true" {
		t.Fatal("CLI mode/revision did not pass normalization and persistence", labels)
	}
	audit := hostedAdminDump(t, dsn, `SELECT detail::text FROM g4_audit WHERE action='node.register' AND target='nd_soft_cli'`)
	var detail struct {
		Mode     string `json:"quota_mode"`
		Revision string `json:"quota_policy_revision"`
		NoHard   bool   `json:"no_hard_disk_quota"`
	}
	if json.Unmarshal([]byte(strings.TrimSpace(audit)), &detail) != nil || detail.Mode != "soft" || detail.Revision != revision || !detail.NoHard {
		t.Fatal("soft node audit does not expose normalized policy")
	}

	t.Run("default-hard", func(t *testing.T) {
		if _, err := hostedAdminRun(t, dsn, "", "register-node", "--id", "nd_hard_cli", "--name", "hard-cli"); err != nil {
			t.Fatal(err)
		}
		var labels map[string]string
		if err := json.Unmarshal([]byte(strings.TrimSpace(hostedAdminDump(t, dsn, `SELECT labels::text FROM g4_node WHERE id='nd_hard_cli'`))), &labels); err != nil {
			t.Fatal(err)
		}
		if labels["quota_mode"] != "hard" || labels["quota_policy_revision"] != revision {
			t.Fatal("legacy hard default changed", labels)
		}
	})
	for _, c := range []struct {
		name string
		args []string
	}{
		{"unknown-mode", []string{"--quota-mode", "auto", "--quota-policy-revision", revision}},
		{"missing-revision", []string{"--quota-mode", "soft"}},
		{"invalid-revision", []string{"--quota-mode", "soft", "--quota-policy-revision", "bad"}},
		{"uppercase-revision", []string{"--quota-mode", "soft", "--quota-policy-revision", strings.Repeat("A", 64)}},
		{"labels-cannot-enable-soft", []string{"--labels", `{"quota_mode":"soft","quota_policy_revision":"` + revision + `"}`}},
	} {
		t.Run(c.name, func(t *testing.T) {
			args := append([]string{"register-node", "--id", "nd_invalid_cli", "--name", "invalid-cli"}, c.args...)
			out, err := hostedAdminRun(t, dsn, "", args...)
			if !errors.Is(err, hosted.ErrParams) || strings.Contains(out, "节点凭据") {
				t.Fatalf("invalid CLI did not reach normalizer or issued credential: %v", err)
			}
			if rows := hostedAdminDump(t, dsn, `SELECT id FROM g4_node WHERE id='nd_invalid_cli'`); rows != "" {
				t.Fatal("invalid registration wrote node")
			}
			if rows := hostedAdminDump(t, dsn, `SELECT node_id FROM g4_node_credential WHERE node_id='nd_invalid_cli'`); rows != "" {
				t.Fatal("invalid registration issued credential")
			}
		})
	}
}
