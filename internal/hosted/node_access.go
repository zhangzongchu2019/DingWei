package hosted

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// NodeAuthorizer queries identity in the same PG transaction that locks the
// node. Member deletion, user disabling and node scope edits serialize here.
type NodeAuthorizer func(context.Context, repo.Tx, string, string, []string, []string) (bool, error)

func (s *Store) SetNodeAuthorizer(f NodeAuthorizer) { s.nodeAuthorizer = f }

type NodeAccess struct {
	Scope  string   `json:"scope"`
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}
type NodeRegistration struct {
	labels              json.RawMessage
	dockerVersion       string
	protocolVersion     int
	ID                  string     `json:"id"`
	Name                string     `json:"name"`
	Pool                string     `json:"pool"`
	QuotaMode           string     `json:"quota_mode"`
	QuotaPolicyRevision string     `json:"quota_policy_revision"`
	Address             string     `json:"address"`
	Access              NodeAccess `json:"access"`
	SchedulingState     string     `json:"scheduling_state"`
}

var ErrNodeExists = errors.New("node_exists")

func validateNodeAccess(a *NodeAccess) error {
	if a.Scope == "" {
		a.Scope = "admin_only"
	}
	if a.Scope != "admin_only" && a.Scope != "public" && a.Scope != "restricted" {
		return ErrParams
	}
	if len(a.Users)+len(a.Groups) > 256 {
		return ErrParams
	}
	if a.Scope != "restricted" && len(a.Users)+len(a.Groups) > 0 {
		return ErrParams
	}
	for _, list := range [][]string{a.Users, a.Groups} {
		for _, id := range list {
			if id == "" || len(id) > 128 || strings.TrimSpace(id) != id || strings.ContainsAny(id, "\x00\r\n\t") {
				return ErrParams
			}
		}
	}
	for i, id := range a.Users {
		if !identity.ValidUserID(id) {
			return ErrParams
		}
		a.Users[i] = identity.Lower(id)
	}
	for _, code := range a.Groups {
		if len(code) != identity.GroupCodeLen || strings.ContainsFunc(code, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_')
		}) {
			return ErrParams
		}
	}
	return nil
}
func validateNodeText(s string, limit int, empty bool) bool {
	if (!empty && strings.TrimSpace(s) == "") || utf8.RuneCountInString(s) > limit {
		return false
	}
	return !strings.ContainsFunc(s, func(r rune) bool { return unicode.IsControl(r) })
}
func nodeScope(n Node) string {
	if n.AccessScope == "" {
		return "admin_only"
	}
	return n.AccessScope
}
func nodeScheduling(n Node) string {
	if n.SchedulingState == "" {
		return "enabled"
	}
	return n.SchedulingState
}
func (s *Store) accessTx(ctx context.Context, tx repo.Tx, user string, n Node) (bool, error) {
	rows, err := tx.Query(ctx, `SELECT subject_kind,subject_id FROM g4_node_access_grant WHERE node_id=$1 ORDER BY subject_kind,subject_id`, n.ID)
	if err != nil {
		return false, err
	}
	var users, groups []string
	for rows.Next() {
		var kind, id string
		if err = rows.Scan(&kind, &id); err != nil {
			rows.Close()
			return false, err
		}
		if kind == "user" {
			users = append(users, id)
		} else {
			groups = append(groups, id)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return false, err
	}
	if s.nodeAuthorizer == nil {
		return nodeScope(n) == "public", nil
	}
	return s.nodeAuthorizer(ctx, tx, user, nodeScope(n), users, groups)
}
func (s *Store) CanAccessNode(ctx context.Context, user string, n Node) (bool, error) {
	if user == "" {
		return false, ErrForbiddenRole
	}
	ac, err := repo.NewAccessCtx("dingwei-platform", user)
	if err != nil {
		return false, err
	}
	var ok bool
	err = s.pool.InTx(ctx, ac, nil, func(tx repo.Tx) error {
		var err error
		if err = tx.QueryRow(ctx, `SELECT access_scope,scheduling_state FROM g4_node WHERE id=$1 FOR SHARE`, n.ID).Scan(&n.AccessScope, &n.SchedulingState); err != nil {
			return err
		}
		ok, err = s.accessTx(ctx, tx, user, n)
		return err
	})
	return ok, err
}
func saveAccessTx(ctx context.Context, tx repo.Tx, node string, a NodeAccess) error {
	if _, err := tx.Exec(ctx, `DELETE FROM g4_node_access_grant WHERE node_id=$1`, node); err != nil {
		return err
	}
	for kind, list := range map[string][]string{"user": a.Users, "group": a.Groups} {
		for _, id := range list {
			if _, err := tx.Exec(ctx, `INSERT INTO g4_node_access_grant(node_id,subject_kind,subject_id) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, node, kind, id); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Store) RegisterManagedNode(ctx context.Context, r NodeRegistration, by string) (string, error) {
	if !nodeSeatID.MatchString(r.ID) || !policyRevisionPattern.MatchString(r.QuotaPolicyRevision) || !validateNodeText(r.ID, 64, false) || !validateNodeText(r.Name, 80, false) || !validateNodeText(r.Pool, 64, true) || !validateNodeText(r.Address, 255, true) {
		return "", ErrParams
	}
	if err := validateNodeAccess(&r.Access); err != nil {
		return "", err
	}
	if r.SchedulingState == "" {
		r.SchedulingState = "enabled"
	}
	if r.SchedulingState != "enabled" && r.SchedulingState != "disabled" && r.SchedulingState != "draining" {
		return "", ErrParams
	}
	labelMap := map[string]string{}
	if len(r.labels) > 0 {
		if err := json.Unmarshal(r.labels, &labelMap); err != nil || labelMap == nil {
			return "", ErrParams
		}
	}
	labelMap["pool"] = r.Pool
	if r.QuotaMode == "soft" && labelMap["allow_soft"] == "" {
		labelMap["allow_soft"] = "true"
	}
	labels, _ := json.Marshal(labelMap)
	n := Node{ID: r.ID, Name: r.Name, Labels: labels, QuotaMode: r.QuotaMode, QuotaPolicyRevision: r.QuotaPolicyRevision}
	if err := normalizeQuotaRegistration(&n); err != nil {
		return "", err
	}
	secret, _, err := issueNodeSecret()
	if err != nil {
		return "", err
	}
	ac, err := repo.NewAccessCtx("dingwei-platform", by)
	if err != nil {
		return "", err
	}
	err = s.pool.InTx(ctx, ac, nil, func(tx repo.Tx) error {
		result, err := tx.Exec(ctx, `INSERT INTO g4_node(id,name,labels,address,address_source,scheduling_state,access_scope,docker_version,node_protocol_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT DO NOTHING`, r.ID, r.Name, n.Labels, r.Address, addressSource(r.Address), r.SchedulingState, r.Access.Scope, r.dockerVersion, max(1, r.protocolVersion))
		if err != nil {
			return err
		}
		count, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrNodeExists
		}
		if _, err = tx.Exec(ctx, `INSERT INTO g4_node_credential(node_id,secret_hash,issued_by)VALUES($1,$2,$3)`, r.ID, hashNodeSecret(secret), by); err != nil {
			return err
		}
		if err = saveAccessTx(ctx, tx, r.ID, r.Access); err != nil {
			return err
		}
		details := map[string]any{}
		_ = json.Unmarshal(quotaRegistrationDetail(n), &details)
		details["access"], details["address"], details["scheduling_state"], details["name"], details["pool"] = r.Access, r.Address, r.SchedulingState, r.Name, r.Pool
		detail, _ := json.Marshal(details)
		_, err = tx.Exec(ctx, `INSERT INTO g4_audit(actor,action,target,detail)VALUES($1,'node.register',$2,$3)`, by, r.ID, detail)
		return err
	})
	if err != nil {
		return "", err
	}
	return secret, nil
}
func addressSource(address string) string {
	if address != "" {
		return "admin"
	}
	return ""
}

// NodePatch carries only administrator-editable fields. Omitted fields are
// loaded under the same node lock as writes, so a state-only request cannot
// restore access from an earlier read or promote a node-reported address.
type NodePatch struct {
	Address         *string     `json:"address,omitempty"`
	Access          *NodeAccess `json:"access,omitempty"`
	SchedulingState *string     `json:"scheduling_state,omitempty"`
}

func (s *Store) UpdateManagedNode(ctx context.Context, id, by string, r NodeRegistration) error {
	return s.PatchManagedNode(ctx, id, by, NodePatch{Address: &r.Address, Access: &r.Access, SchedulingState: &r.SchedulingState})
}
func (s *Store) PatchManagedNode(ctx context.Context, id, by string, patch NodePatch) error {
	if patch.Access != nil {
		if err := validateNodeAccess(patch.Access); err != nil {
			return err
		}
	}
	if patch.SchedulingState != nil && *patch.SchedulingState != "enabled" && *patch.SchedulingState != "disabled" && *patch.SchedulingState != "draining" {
		return ErrParams
	}
	if patch.Address != nil && !validateNodeText(*patch.Address, 255, true) {
		return ErrParams
	}
	ac, err := repo.NewAccessCtx("dingwei-platform", by)
	if err != nil {
		return err
	}
	return s.pool.InTx(ctx, ac, nil, func(tx repo.Tx) error {
		var address, source, state, scope string
		if err := tx.QueryRow(ctx, `SELECT address,address_source,scheduling_state,access_scope FROM g4_node WHERE id=$1 FOR UPDATE`, id).Scan(&address, &source, &state, &scope); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			return err
		}
		if patch.Address != nil {
			address = *patch.Address
			source = addressSource(address)
		}
		if patch.SchedulingState != nil {
			state = *patch.SchedulingState
		}
		if patch.Access != nil {
			scope = patch.Access.Scope
		}
		if _, err := tx.Exec(ctx, `UPDATE g4_node SET address=$2,address_source=$3,scheduling_state=$4,access_scope=$5,updated_at=now() WHERE id=$1`, id, address, source, state, scope); err != nil {
			return err
		}
		if patch.Access != nil {
			if err := saveAccessTx(ctx, tx, id, *patch.Access); err != nil {
				return err
			}
		}
		detail, _ := json.Marshal(patch)
		_, err := tx.Exec(ctx, `INSERT INTO g4_audit(actor,action,target,detail)VALUES($1,'node.update',$2,$3)`, by, id, detail)
		return err
	})
}

// Node configuration is platform metadata read only after administrative
// authorization; actor remains explicit in the repository access context.
func (s *Store) GetNodeRegistration(ctx context.Context, id, actor string) (NodeRegistration, error) {
	ac, err := repo.NewAccessCtx("dingwei-platform", actor)
	if err != nil {
		return NodeRegistration{}, err
	}
	var n Node
	row, err := s.pool.QueryRow(ctx, ac, `SELECT id,name,labels,address,access_scope,scheduling_state FROM g4_node WHERE id=$1`, id)
	if err != nil {
		return NodeRegistration{}, err
	}
	if err = row.Scan(&n.ID, &n.Name, &n.Labels, &n.Address, &n.AccessScope, &n.SchedulingState); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return NodeRegistration{}, ErrNotFound
		}
		return NodeRegistration{}, err
	}
	var labels map[string]string
	_ = json.Unmarshal(n.Labels, &labels)
	mode, rev := registeredQuota(n)
	r := NodeRegistration{ID: n.ID, Name: n.Name, Pool: labels["pool"], QuotaMode: mode, QuotaPolicyRevision: rev, Address: n.Address, SchedulingState: nodeScheduling(n), Access: NodeAccess{Scope: nodeScope(n), Users: []string{}, Groups: []string{}}}
	rows, err := s.pool.Query(ctx, ac, `SELECT subject_kind,subject_id FROM g4_node_access_grant WHERE node_id=$1 ORDER BY subject_kind,subject_id`, id)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, id string
		if err = rows.Scan(&kind, &id); err != nil {
			return r, err
		}
		if kind == "user" {
			r.Access.Users = append(r.Access.Users, id)
		} else {
			r.Access.Groups = append(r.Access.Groups, id)
		}
	}
	return r, rows.Err()
}
