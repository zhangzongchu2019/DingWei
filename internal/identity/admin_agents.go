package identity

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

const AdminAgentPageMax = 200

// AdminAgentRecord is deliberately separate from Agent: connection metadata
// never becomes part of ordinary Agent/group/event serialization.
type AdminAgentRecord struct {
	ID               string
	OwnerID          string
	OwnerShortName   string
	OwnerDisplayName string
	Name             string
	Origin           AgentOrigin
	State            AgentState
	SourceIP         string    `json:"-"`
	SourceAt         time.Time `json:"-"`
}
type AdminAgentPage struct {
	Agents    []AdminAgentRecord
	NextAfter string
}

// AdminAgentStore checks the current role and audits exactly one successful
// bounded read atomically. Audit failure returns no page to the HTTP caller.
type AdminAgentStore interface {
	ListAdminAgents(context.Context, string, string, int, time.Time) (AdminAgentPage, error)
}

func adminAgentLimit(limit int) error {
	if limit < 1 || limit > AdminAgentPageMax {
		return ErrBadRequest
	}
	return nil
}
func adminAgentAudit(actor string, limit, count int, at time.Time) AuditEvent {
	return AuditEvent{Actor: actor, Action: "agent.admin_list", Target: "all-agents", Detail: fmt.Sprintf("limit=%d returned=%d", limit, count), At: at.UTC()}
}
func (m *MemStore) ListAdminAgents(_ context.Context, actor, after string, limit int, at time.Time) (AdminAgentPage, error) {
	if err := adminAgentLimit(limit); err != nil {
		return AdminAgentPage{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	u := m.users[Lower(actor)]
	if u == nil || u.State != UserActive || u.Type != UserPlatformAdmin {
		return AdminAgentPage{}, ErrForbidden
	}
	ids := make([]string, 0, len(m.agents))
	for id := range m.agents {
		if id > after {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	page := AdminAgentPage{Agents: make([]AdminAgentRecord, 0, limit)}
	for _, id := range ids {
		if len(page.Agents) == limit {
			page.NextAfter = page.Agents[len(page.Agents)-1].ID
			break
		}
		a := m.agents[id]
		owner := m.users[a.OwnerLC]
		if owner == nil {
			owner = &User{UserID: a.OwnerLC}
		}
		row := AdminAgentRecord{ID: a.ID, OwnerID: owner.UserID, OwnerShortName: owner.ShortName, OwnerDisplayName: owner.DisplayName, Name: a.Name, Origin: a.Origin, State: a.State}
		if a.Origin == OriginSelfhost {
			row.SourceIP = m.connections[id].IP
			row.SourceAt = m.connections[id].At
		}
		page.Agents = append(page.Agents, row)
	}
	m.audit = append(m.audit, adminAgentAudit(Lower(actor), limit, len(page.Agents), at))
	return page, nil
}
func (s *PGStore) ListAdminAgents(ctx context.Context, actor, after string, limit int, at time.Time) (AdminAgentPage, error) {
	if err := adminAgentLimit(limit); err != nil {
		return AdminAgentPage{}, err
	}
	var page AdminAgentPage
	err := s.inTx(ctx, func(tx repo.Tx) error {
		var state, kind string
		err := tx.QueryRow(ctx, `SELECT state,type FROM np_user WHERE lower(user_id)=$1 FOR SHARE`, Lower(actor)).Scan(&state, &kind)
		if noRows(err) {
			return ErrForbidden
		}
		if err != nil {
			return err
		}
		if state != string(UserActive) || kind != string(UserPlatformAdmin) {
			return ErrForbidden
		}
		rows, err := tx.Query(ctx, `SELECT a.id,COALESCE(u.user_id,a.owner_lc),COALESCE(u.short_name,''),COALESCE(u.display_name,''),a.name,a.origin,a.state,
   CASE WHEN a.origin='selfhost' THEN COALESCE(a.last_source_ip,'') ELSE '' END,
   COALESCE(a.last_source_at,'0001-01-01'::timestamptz)
   FROM np_agent a LEFT JOIN np_user u ON u.user_id_lc=a.owner_lc
   WHERE a.id COLLATE "C">$1 ORDER BY a.id COLLATE "C" LIMIT $2`, after, limit+1)
		if err != nil {
			return err
		}
		defer rows.Close()
		page.Agents = make([]AdminAgentRecord, 0, limit)
		for rows.Next() {
			var row AdminAgentRecord
			if err := rows.Scan(&row.ID, &row.OwnerID, &row.OwnerShortName, &row.OwnerDisplayName, &row.Name, &row.Origin, &row.State, &row.SourceIP, &row.SourceAt); err != nil {
				return err
			}
			if len(page.Agents) == limit {
				page.NextAfter = page.Agents[len(page.Agents)-1].ID
				continue
			}
			row.SourceAt = row.SourceAt.UTC()
			page.Agents = append(page.Agents, row)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		event := adminAgentAudit(Lower(actor), limit, len(page.Agents), at)
		_, err = tx.Exec(ctx, `INSERT INTO audit_log(actor,action,target,after_json,ts) VALUES($1,$2,$3,jsonb_build_object('detail',$4::text),$5)`, event.Actor, event.Action, event.Target, event.Detail, event.At)
		return err
	})
	if err != nil {
		return AdminAgentPage{}, err
	}
	return page, nil
}
