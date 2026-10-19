package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
	"github.com/zhangzongchu2019/dingwei/internal/store/migrate"
	"github.com/zhangzongchu2019/dingwei/internal/store/pgtest"
)

// The real production registrar, real PG identity and N3 cookies; no Hosted
// callback or read-only callback is injected by this test.
func TestR31AdminAgentNodeWithoutMasterKeyProductionWiring(t *testing.T) {
	for _, mode := range []string{"existing", "no-hosted-tables", "no-seat-table", "no-node-table", "unrelated-query-error"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			t.Setenv("DW_G4_MASTER_KEY_FILE", "")
			t.Setenv("DW_G4_ACCESS_MODE", "allowlist")
			t.Setenv("DW_G4_ACCESS_ALLOW_USER_IDS", "")
			pool := pgtest.Pool(t)
			if _, err := migrate.Migrate(ctx, pool, migrate.HubDomains()); err != nil {
				t.Fatal(err)
			}
			tc, err := repo.NewAccessCtx("test", "r31-admin-node-read")
			if err != nil {
				t.Fatal(err)
			}
			exec := func(q string) {
				t.Helper()
				if _, err := pool.Exec(ctx, tc, q); err != nil {
					t.Fatal(err)
				}
			}
			exec(`INSERT INTO g4_node(id,name) VALUES('nd-inventory','inventory-node')`)
			exec(`INSERT INTO g4_hosted_seat(agent_id,node_id,cli,llm,cred_kind,plan,created_by) VALUES('ag-inventory','nd-inventory','codex','gpt','token_file','2c4g','alice')`)
			st := identity.NewPGStore(pool)
			now := time.Now().UTC()
			for _, u := range []identity.User{{UserID: "admin", ShortName: "ad", Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: now}, {UserID: "alice", ShortName: "al", Type: identity.UserMember, State: identity.UserActive, CreatedAt: now}} {
				if err := st.CreateUser(ctx, u); err != nil {
					t.Fatal(err)
				}
			}
			if err := st.CreateHostedAgent(ctx, identity.Agent{ID: "ag-inventory", OwnerLC: "alice", Name: "Inventory", CreatedAt: now}, identity.Application{ID: "app-inventory", OwnerLC: "alice", PairingSalt: "salt", PairingDigest: "digest", PollTokenHash: "poll", DeviceFingerprint: "fixture", RequestedAt: now, ExpiresAt: now.Add(time.Hour)}, nil); err != nil {
				t.Fatal(err)
			}
			if err := st.CreateSession(ctx, identity.Session{UserIDLC: "admin", TokenHash: identity.HashSecret("inventory-human"), ExpiresAt: now.Add(time.Hour)}); err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "no-hosted-tables":
				exec(`DROP TABLE g4_hosted_seat,g4_node CASCADE`)
			case "no-seat-table":
				exec(`DROP TABLE g4_hosted_seat CASCADE`)
			case "no-node-table":
				exec(`DROP TABLE g4_node CASCADE`)
			case "unrelated-query-error":
				exec(`ALTER TABLE g4_node RENAME COLUMN name TO unavailable_name`)
			}
			mux := http.NewServeMux()
			hub := newplatHub(t)
			if err := wireNewPlatform(ctx, mux, hub, newPlatformConfig{publicOrigin: "https://x"}, st, pool); err != nil {
				t.Fatal(err)
			}
			assertHostedRouteState(t, mux, false)
			req := httptest.NewRequest("GET", "/api/console/platform/agents", nil)
			req.AddCookie(&http.Cookie{Name: httpapi.SessionCookie, Value: "inventory-human"})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if mode == "unrelated-query-error" {
				if w.Code != 500 || strings.Contains(w.Body.String(), "Inventory.al") {
					t.Fatalf("ADMIN_NODE_UNRELATED_ERROR_HIDDEN: %d %s", w.Code, w.Body.String())
				}
				return
			}
			var page struct {
				Agents []struct {
					ID   string `json:"agent_id"`
					Name string `json:"full_name"`
					Host string `json:"host"`
				} `json:"agents"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &page); err != nil {
				t.Fatal(err)
			}
			if w.Code != 200 || len(page.Agents) != 1 || page.Agents[0].ID != "ag-inventory" {
				t.Fatalf("ADMIN_NODE_TABLE_ABSENCE_BROKE_LIST: %d %s", w.Code, w.Body.String())
			}
			want := ""
			if mode == "existing" {
				want = "inventory-node"
			}
			if page.Agents[0].Host != want || page.Agents[0].Name != "Inventory.al" {
				t.Fatalf("ADMIN_NODE_KEYLESS_WIRING_MISSING: %s %s", mode, w.Body.String())
			}
			row, err := pool.QueryRow(ctx, tc, `SELECT count(*) FROM audit_log WHERE action='agent.admin_list'`)
			if err != nil {
				t.Fatal(err)
			}
			var audits int
			if err := row.Scan(&audits); err != nil {
				t.Fatal(err)
			}
			if audits != 1 {
				t.Fatalf("admin read audit=%d", audits)
			}
			if strings.Contains(w.Body.String(), "account_ref") || strings.Contains(w.Body.String(), "cred_kind") {
				t.Fatal("hosted private columns leaked")
			}
		})
	}
}
