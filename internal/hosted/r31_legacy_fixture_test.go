package hosted

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
	"strings"
	"time"
)

// Existing pre-R31 feature fixtures explicitly grant their test user access and
// supply the hard node's new measured-frame capability. R31 permission, unknown
// measurement and legacy-node negatives live in r31_hosted_test.go instead.
func r31LegacyNode(n Node) Node {
	if n.AccessScope == "" {
		n.AccessScope = "public"
	}
	if n.QuotaMode != "soft" && n.QuotaPolicyRevision == "" {
		n.QuotaPolicyRevision = strings.Repeat("a", 64)
	}
	return n
}
func (s *Store) upsertR31LegacyNode(ctx context.Context, n Node) error {
	return s.UpsertNode(ctx, r31LegacyNode(n))
}
func (s *Store) registerR31LegacyNode(ctx context.Context, n Node, by string) (string, error) {
	return s.RegisterNode(ctx, r31LegacyNode(n), by)
}
func (s *Store) setR31LegacyOnline(ctx context.Context, id string, raw json.RawMessage) error {
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil && m["quota_mode"] == nil && m["seat_capacity"] == nil {
		n, err := s.GetNode(ctx, id)
		if err != nil {
			return err
		}
		mode, rev := registeredQuota(n)
		if mode == "hard" {
			cpu := int64(64000)
			if value, ok := m["cpu_total"].(float64); ok {
				cpu = int64(value) * 1000
			}
			memory := int64(0)
			if value, ok := m["mem_free_mb"].(float64); ok {
				memory = int64(value) << 20
			}
			disk := int64(1 << 40)
			if value, ok := m["disk_free_mb"].(float64); ok {
				disk = int64(value) << 20
			}
			m["quota_mode"], m["quota_policy_revision"] = mode, rev
			m["seat_capacity"] = seatcapacity.Evaluate(seatcapacity.DefaultPolicy(), seatcapacity.Snapshot{Known: true, At: time.Now(), Total: seatcapacity.Resources{CPU: cpu, Memory: memory, Disk: disk}}, nil, time.Now(), time.Minute)
			m["soft_seats"] = map[string]any{}
			raw, err = json.Marshal(m)
			if err != nil {
				return err
			}
		}
	}
	return s.SetNodeOnline(ctx, id, raw)
}
