package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
)

// BeginSubAgentConnection binds recovery to an authenticated connection. The Hub
// additionally keeps readiness in memory; persisted conn_seq alone grants nothing.
func (s *SQLite) BeginSubAgentConnection(ctx context.Context, root string, seq int) error {
	var barred int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM sub_agent_rollback WHERE root_agent_id=?`, root).Scan(&barred); err != nil {
		return err
	}
	if barred != 0 {
		return subagent.Fail("unsupported", "rollback barrier retained; remain feature-off")
	}
	result, err := s.db.ExecContext(ctx, `INSERT INTO sub_agent_registry(root_agent_id,node_id,generation,state,conn_seq) VALUES(?,'',1,'active',?) ON CONFLICT(root_agent_id,node_id) DO UPDATE SET conn_seq=excluded.conn_seq WHERE excluded.conn_seq>=sub_agent_registry.conn_seq`, root, seq)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return subagent.Fail("generation_mismatch", "connection epoch rollback")
	}
	return nil
}
func (s *SQLite) ReportSubAgentTree(ctx context.Context, root string, snap subagent.Snapshot) (int64, error) {
	digest, err := snap.CanonicalDigest()
	if err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var revision int64
	var priorDigest string
	var seq int
	if err = tx.QueryRowContext(ctx, `SELECT revision,digest,conn_seq FROM sub_agent_registry WHERE root_agent_id=? AND node_id=''`, root).Scan(&revision, &priorDigest, &seq); err != nil {
		return 0, err
	}
	if seq != snap.ConnSeq {
		return revision, subagent.Fail("generation_mismatch", "stale connection")
	}
	if snap.Revision == revision && digest == priorDigest {
		return revision, tx.Commit()
	}
	if snap.Revision <= revision || snap.BaseRevision != revision {
		return revision, subagent.Fail("path_conflict", "revision conflict; resync required")
	}
	// Full snapshots must include every durable node and retained alias. Omission
	// cannot erase a tombstone or free its name for a different native thread.
	rows, err := tx.QueryContext(ctx, `SELECT node_id,generation,canonical_name,state,created_at,policy_json FROM sub_agent_registry WHERE root_agent_id=? AND node_id<>''`, root)
	if err != nil {
		return revision, err
	}
	type prior struct {
		gen                          int64
		name, state, created, policy string
	}
	old := map[string]prior{}
	for rows.Next() {
		var id string
		var p prior
		if err = rows.Scan(&id, &p.gen, &p.name, &p.state, &p.created, &p.policy); err != nil {
			rows.Close()
			return revision, err
		}
		old[id] = p
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return revision, err
	}
	included := map[string]bool{}
	for _, n := range snap.Nodes {
		included[n.NodeID] = true
		if p, ok := old[n.NodeID]; ok {
			if p.gen != n.Generation {
				return revision, subagent.Fail("generation_mismatch", "node generation is immutable")
			}
			policy := string(n.Policy)
			if policy == "" {
				policy = "{}"
			}
			var oldPolicy, newPolicy any
			_ = json.Unmarshal([]byte(p.policy), &oldPolicy)
			_ = json.Unmarshal([]byte(policy), &newPolicy)
			oldJSON, _ := json.Marshal(oldPolicy)
			newJSON, _ := json.Marshal(newPolicy)
			if p.name != n.Name || p.created != n.CreatedAt || string(oldJSON) != string(newJSON) {
				return revision, subagent.Fail("path_conflict", "node binding is immutable")
			}
			if (p.state == "ended" && n.State != "ended") || (p.state == "draining" && n.State != "draining" && n.State != "ended") || (p.state == "active" && n.State == "allocating") {
				return revision, subagent.Fail("ended", "closed node cannot revive")
			}
		}
	}
	for id := range old {
		if !included[id] {
			return revision, subagent.Fail("path_conflict", "snapshot omitted durable node")
		}
	}
	for _, n := range snap.Nodes {
		policy := string(n.Policy)
		if policy == "" {
			policy = "{}"
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO sub_agent_registry(root_agent_id,node_id,parent_node_id,generation,canonical_name,state,reason,revision,conn_seq,created_at,policy_json) VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(root_agent_id,node_id) DO UPDATE SET state=excluded.state,reason=excluded.reason,revision=excluded.revision,conn_seq=excluded.conn_seq`, root, n.NodeID, n.ParentID, n.Generation, n.Name, n.State, n.Reason, snap.Revision, seq, n.CreatedAt, policy)
		if err != nil {
			return revision, err
		}
		for _, alias := range append([]string{n.Name}, n.Aliases...) {
			var owner string
			err = tx.QueryRowContext(ctx, `SELECT node_id FROM sub_agent_alias WHERE root_agent_id=? AND parent_node_id=? AND alias=?`, root, n.ParentID, alias).Scan(&owner)
			if err == nil && owner != n.NodeID {
				return revision, subagent.Fail("alias_conflict", "alias is permanently reserved")
			}
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return revision, err
			}
			if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO sub_agent_alias(root_agent_id,parent_node_id,alias,node_id) VALUES(?,?,?,?)`, root, n.ParentID, alias, n.NodeID); err != nil {
				return revision, err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE sub_agent_registry SET revision=?,digest=? WHERE root_agent_id=? AND node_id='' AND conn_seq=?`, snap.Revision, digest, root, seq)
	if err != nil {
		return revision, err
	}
	return snap.Revision, tx.Commit()
}

type subAgentQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func resolveSubAgent(ctx context.Context, q subAgentQuery, root string, seq int, path []string) (*subagent.Scope, error) {
	if len(path) != 1 {
		return nil, subagent.Fail("unsupported", "phase_not_enabled")
	}
	var revision int64
	var currentSeq int
	if err := q.QueryRowContext(ctx, `SELECT revision,conn_seq FROM sub_agent_registry WHERE root_agent_id=? AND node_id=''`, root).Scan(&revision, &currentSeq); err != nil {
		return nil, subagent.Fail("registry_unavailable", "tree not recovered")
	}
	if currentSeq != seq || revision == 0 {
		return nil, subagent.Fail("registry_unavailable", "connection tree not confirmed")
	}
	var id, state, reason, name string
	var gen int64
	err := q.QueryRowContext(ctx, `SELECT r.node_id,r.generation,r.state,r.reason,r.canonical_name FROM sub_agent_alias a JOIN sub_agent_registry r ON r.root_agent_id=a.root_agent_id AND r.node_id=a.node_id WHERE a.root_agent_id=? AND a.parent_node_id='' AND a.alias=?`, root, path[0]).Scan(&id, &gen, &state, &reason, &name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, subagent.Fail("not_found", "子 Agent 不存在")
	}
	if err != nil {
		return nil, subagent.Fail("registry_unavailable", "registry read failed")
	}
	if state == "ended" || state == "draining" {
		return nil, subagent.Fail("ended", reason)
	}
	if state != "active" {
		return nil, subagent.Fail("registry_unavailable", "node not active")
	}
	return &subagent.Scope{RootID: root, NodeID: id, Generation: gen, Path: []string{name}, Revision: revision, ConnSeq: seq}, nil
}
func (s *SQLite) ResolveSubAgent(ctx context.Context, root string, seq int, path []string) (*subagent.Scope, error) {
	return resolveSubAgent(ctx, s.db, root, seq, path)
}
func checkSubAgentScope(ctx context.Context, q subAgentQuery, scope *subagent.Scope) error {
	if scope == nil {
		return nil
	}
	got, err := resolveSubAgent(ctx, q, scope.RootID, scope.ConnSeq, scope.Path)
	if err != nil {
		return err
	}
	if got.NodeID != scope.NodeID || got.Generation != scope.Generation {
		return subagent.Fail("generation_mismatch", "target binding changed")
	}
	return nil
}
func (s *SQLite) CheckSubAgentRoute(ctx context.Context, r subagent.DeliveryRoute) error {
	if r.From != nil {
		// A queued source identity survives transport reconnect. Admission still
		// requires the original live conn_seq in EnqueueSessionDelivery.
		scope := *r.From
		if err := s.db.QueryRowContext(ctx, `SELECT conn_seq FROM sub_agent_registry WHERE root_agent_id=? AND node_id=''`, scope.RootID).Scan(&scope.ConnSeq); err != nil {
			return subagent.Fail("registry_unavailable", "source registry unavailable")
		}
		if err := checkSubAgentScope(ctx, s.db, &scope); err != nil {
			return err
		}
	}
	return checkSubAgentScope(ctx, s.db, r.To)
}
func (s *SQLite) SessionDeliverySubRoute(ctx context.Context, id string) (*subagent.DeliveryRoute, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT route_json FROM session_delivery_sub_route WHERE delivery_id=?`, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var r subagent.DeliveryRoute
	err = json.Unmarshal([]byte(raw), &r)
	return &r, err
}

func (s *SQLite) SubAgentRevision(ctx context.Context, root string) (int64, error) {
	var revision int64
	err := s.db.QueryRowContext(ctx, `SELECT revision FROM sub_agent_registry WHERE root_agent_id=? AND node_id=''`, root).Scan(&revision)
	return revision, err
}

// ListSubAgentNodes includes retained tombstones; it never makes offline nodes active.
func (s *SQLite) ListSubAgentNodes(ctx context.Context, root string) ([]subagent.Node, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id,parent_node_id,canonical_name,generation,state,reason FROM sub_agent_registry WHERE root_agent_id=? AND node_id<>'' ORDER BY canonical_name`, root)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []subagent.Node{}
	for rows.Next() {
		var n subagent.Node
		if err := rows.Scan(&n.NodeID, &n.ParentID, &n.Name, &n.Generation, &n.State, &n.Reason); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}
