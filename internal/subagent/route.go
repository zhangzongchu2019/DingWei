// Package subagent defines route values, not new identity principals.
package subagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"sort"
	"strings"
)

type Error struct{ Code, Detail string }

func (e *Error) Error() string       { return e.Code + ": " + e.Detail }
func Fail(code, detail string) error { return &Error{"sub_agent_" + code, detail} }

type Route struct {
	Root, Owner string
	Path        []string
}

func Parse(name, owner string) (Route, error) {
	parts := strings.Split(name, ".")
	if len(parts) > 5 {
		return Route{}, Fail("depth_exceeded", "最多三级子 Agent")
	}
	if len(parts) == 1 {
		parts = append(parts, owner)
	}
	if len(parts) < 2 || !validOwner(parts[len(parts)-1]) {
		return Route{}, Fail("path_conflict", "用户段非法")
	}
	root := parts[len(parts)-2]
	// Preserve historical external roots only for two-part principal addresses.
	validRoot := validName(root)
	if len(parts) == 2 {
		external := extshared.IsExternalAgentName(root)
		validRoot = validRoot || external
	}
	if !validRoot {
		return Route{}, Fail("path_conflict", "主名非法")
	}
	out := Route{Root: strings.ToLower(root), Owner: strings.ToLower(parts[len(parts)-1])}
	for i := len(parts) - 3; i >= 0; i-- {
		if !validName(parts[i]) {
			return Route{}, Fail("path_conflict", "子段非法")
		}
		out.Path = append(out.Path, strings.ToLower(parts[i]))
	}
	return out, nil
}
func (r Route) FullName() string { return FullName(r.Root+"."+r.Owner, r.Path) }
func FullName(root string, path []string) string {
	parts := make([]string, 0, len(path)+1)
	for i := len(path) - 1; i >= 0; i-- {
		parts = append(parts, path[i])
	}
	return strings.Join(append(parts, root), ".")
}

type Node struct {
	NodeID     string          `json:"node_id"`
	ParentID   string          `json:"parent_node_id"`
	Name       string          `json:"canonical_name"`
	Aliases    []string        `json:"aliases"`
	State      string          `json:"state"`
	Generation int64           `json:"generation"`
	Reason     string          `json:"reason"`
	CreatedAt  string          `json:"created_at"`
	Policy     json.RawMessage `json:"policy,omitempty"`
}
type Snapshot struct {
	ConnSeq      int    `json:"conn_seq"`
	BaseRevision int64  `json:"base_revision"`
	Revision     int64  `json:"subtree_revision"`
	Nodes        []Node `json:"nodes"`
}

// CanonicalDigest is calculated by the Hub, never accepted from the peer.
func (s Snapshot) CanonicalDigest() (string, error) {
	if s.Revision <= 0 || len(s.Nodes) > 1024 {
		return "", Fail("path_conflict", "invalid snapshot bounds")
	}
	nodes := append([]Node(nil), s.Nodes...)
	ids, names := map[string]bool{}, map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		if n.NodeID == "" || len(n.NodeID) > 128 || n.Generation < 1 || n.ParentID != "" || !validName(n.Name) || n.Name != strings.ToLower(n.Name) {
			return "", Fail("path_conflict", "invalid first-level node")
		}
		if ids[n.NodeID] {
			return "", Fail("path_conflict", "duplicate node")
		}
		ids[n.NodeID] = true
		if n.State != "allocating" && n.State != "active" && n.State != "draining" && n.State != "ended" {
			return "", Fail("path_conflict", "invalid state")
		}
		if len(n.Policy) > 0 {
			var policy any
			if err := json.Unmarshal(n.Policy, &policy); err != nil {
				return "", Fail("path_conflict", "invalid policy JSON")
			}
			n.Policy, _ = json.Marshal(policy)
		}
		n.Aliases = append([]string(nil), n.Aliases...)
		sort.Strings(n.Aliases)
		for _, name := range append([]string{n.Name}, n.Aliases...) {
			if !validName(name) || name != strings.ToLower(name) || names[name] {
				return "", Fail("alias_conflict", "duplicate or invalid alias")
			}
			names[name] = true
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].NodeID < nodes[j].NodeID })
	raw, err := json.Marshal(nodes)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

type Scope struct {
	RootID     string   `json:"root_agent_id"`
	FullName   string   `json:"full_name"`
	NodeID     string   `json:"node_id"`
	Generation int64    `json:"generation"`
	Path       []string `json:"sub_path"`
	Revision   int64    `json:"subtree_revision"`
	ConnSeq    int      `json:"conn_seq"`
}
type DeliveryRoute struct {
	From *Scope `json:"from,omitempty"`
	To   *Scope `json:"to,omitempty"`
}

func (s Scope) Identity() string {
	return fmt.Sprintf("%s\x00%s\x00%d", s.RootID, s.NodeID, s.Generation)
}

func alnum(s string, min, max int) bool {
	if len(s) < min || len(s) > max {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}
func validName(s string) bool  { return alnum(s, 2, 32) }
func validOwner(s string) bool { return alnum(s, 2, 16) }

// DeliveryIdentity scopes both ends. It also separates two children originating
// the same native envelope ID toward a root destination.
func (r DeliveryRoute) Identity() string {
	from, to := "", ""
	if r.From != nil {
		from = r.From.Identity()
	}
	if r.To != nil {
		to = r.To.Identity()
	}
	return from + "\x00to\x00" + to
}
func (r DeliveryRoute) SourceType(base string) string {
	sum := sha256.Sum256([]byte(r.Identity()))
	return base + "/subagent/" + hex.EncodeToString(sum[:])
}
