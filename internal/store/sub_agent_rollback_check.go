package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

type SubAgentRollbackPendingRoot struct {
	RootAgentID string   `json:"root_agent_id"`
	Count       int      `json:"count"`
	DeliveryIDs []string `json:"delivery_ids"`
}

type SubAgentRollbackCheck struct {
	Pending int                           `json:"pending"`
	Roots   []SubAgentRollbackPendingRoot `json:"roots"`
}

// CheckSubAgentRollback inventories every retained sidecar lacking a per-mail
// rollback audit, including terminal rows and roots absent from the registry.
// A source-only sidecar is still pending; it must never disappear from the gate.
// This method only queries; callers of the offline command open mode=ro.
func (s *SQLite) CheckSubAgentRollback(ctx context.Context) (SubAgentRollbackCheck, error) {
	out := SubAgentRollbackCheck{Roots: []SubAgentRollbackPendingRoot{}}
	rows, err := s.db.QueryContext(ctx, `SELECT r.delivery_id,r.route_json
 FROM session_delivery_sub_route r
 WHERE NOT EXISTS (SELECT 1 FROM sub_agent_rollback_delivery a WHERE a.delivery_id=r.delivery_id)
 ORDER BY r.delivery_id`)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	groups := map[string]*SubAgentRollbackPendingRoot{}
	for rows.Next() {
		var id, raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return out, err
		}
		var route subagent.DeliveryRoute
		if json.Unmarshal([]byte(raw), &route) != nil {
			return out, fmt.Errorf("invalid retained route for delivery %s", id)
		}
		root := ""
		if route.To != nil {
			root = route.To.RootID
		} else if route.From != nil {
			root = route.From.RootID
		}
		if root == "" {
			return out, fmt.Errorf("missing retained root for delivery %s", id)
		}
		if groups[root] == nil {
			groups[root] = &SubAgentRollbackPendingRoot{RootAgentID: root, DeliveryIDs: []string{}}
		}
		groups[root].Count++
		groups[root].DeliveryIDs = append(groups[root].DeliveryIDs, id)
		out.Pending++
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	for _, group := range groups {
		out.Roots = append(out.Roots, *group)
	}
	sort.Slice(out.Roots, func(i, j int) bool { return out.Roots[i].RootAgentID < out.Roots[j].RootAgentID })
	return out, nil
}
