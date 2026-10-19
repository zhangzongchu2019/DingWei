package identitytest

import (
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"strings"
	"testing"
	"time"
)

// RunAdminAgents applies the same private-read contract to memory and PG.
func RunAdminAgents(t *testing.T, f Factory) {
	seed := func(t *testing.T) (identity.Store, identity.AdminAgentStore) {
		s := f(t)
		for _, u := range []identity.User{{UserID: "admin", ShortName: "ad", Type: identity.UserPlatformAdmin, State: identity.UserActive, CreatedAt: t0}, {UserID: "alice", ShortName: "al", Type: identity.UserMember, State: identity.UserActive, CreatedAt: t0}, {UserID: "manager", ShortName: "mg", Type: identity.UserManager, State: identity.UserActive, CreatedAt: t0}, {UserID: "disabled", ShortName: "ds", Type: identity.UserPlatformAdmin, State: identity.UserDisabled, CreatedAt: t0}} {
			if err := s.CreateUser(ctx, u); err != nil {
				t.Fatal(err)
			}
		}
		mustApp(t, s, "app-a", "alice", "device-a", t0)
		mustApprove(t, s, "app-a", "alice", "ag-a", "Self", t0)
		mustHosted(t, s, "ag-b", "alice", "Hosted", "app-b", t0)
		mustApp(t, s, "app-c", "manager", "device-c", t0)
		mustApprove(t, s, "app-c", "manager", "ag-c", "Other", t0)
		if err := s.(identity.ConnectionStore).RecordAgentConnection(ctx, "ag-a", "203.0.113.44", t0); err != nil {
			t.Fatal(err)
		}
		return s, s.(identity.AdminAgentStore)
	}
	t.Run("all_owners_private_source_and_stable_pages", func(t *testing.T) {
		s, st := seed(t)
		page, err := st.ListAdminAgents(ctx, "ADMIN", "", 2, t0)
		if err != nil || len(page.Agents) != 2 || page.NextAfter != "ag-b" {
			t.Fatalf("ADMIN_PAGE_MISSING: %+v %v", page, err)
		}
		if page.Agents[0].ID != "ag-a" || page.Agents[0].OwnerID != "alice" || page.Agents[0].OwnerShortName != "al" || page.Agents[0].SourceIP != "203.0.113.44" || !page.Agents[0].SourceAt.Equal(t0) {
			t.Fatalf("ADMIN_SOURCE_MISSING: %+v", page.Agents)
		}
		if page.Agents[1].Origin != identity.OriginHosted || page.Agents[1].SourceIP != "" {
			t.Fatal("hosted source projection")
		}
		raw, _ := json.Marshal(page.Agents[0])
		if strings.Contains(string(raw), "203.0.113.44") {
			t.Fatal("private source became generic serialization")
		}
		next, err := st.ListAdminAgents(ctx, "admin", page.NextAfter, 2, t0)
		if err != nil || len(next.Agents) != 1 || next.Agents[0].ID != "ag-c" || next.NextAfter != "" {
			t.Fatalf("ADMIN_PAGINATION_BROKEN: %+v %v", next, err)
		}
		own, err := s.ListAgents(ctx, "admin")
		if err != nil || len(own) != 0 {
			t.Fatal("ordinary list widened for admin")
		}
		for _, limit := range []int{0, -1, identity.AdminAgentPageMax + 1} {
			if _, err := st.ListAdminAgents(ctx, "admin", "", limit, t0); !errors.Is(err, identity.ErrBadRequest) {
				t.Fatalf("ADMIN_BOUND_MISSING: %d %v", limit, err)
			}
		}
	})
	t.Run("nonadmin_and_inactive_rejected_on_every_read", func(t *testing.T) {
		s, st := seed(t)
		for _, actor := range []string{"alice", "manager", "disabled", "missing"} {
			if _, err := st.ListAdminAgents(ctx, actor, "", 100, t0); !errors.Is(err, identity.ErrForbidden) {
				t.Fatalf("ADMIN_ROLE_GATE_MISSING: %s %v", actor, err)
			}
		}
		if err := s.SetUserState(ctx, "admin", identity.UserDisabled, t0.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		if _, err := st.ListAdminAgents(ctx, "admin", "", 100, t0); !errors.Is(err, identity.ErrForbidden) {
			t.Fatalf("ADMIN_ACTIVE_GATE_MISSING: %v", err)
		}
	})
	t.Run("revoked_records_visible_for_readonly_inventory", func(t *testing.T) {
		s, st := seed(t)
		if _, err := s.RetireAgent(ctx, "ag-a", "admin", t0.Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		page, err := st.ListAdminAgents(ctx, "admin", "", 100, t0)
		if err != nil || len(page.Agents) != 3 || page.Agents[0].State != identity.AgentRevoked {
			t.Fatalf("revoked inventory omitted: %+v %v", page, err)
		}
	})
}
