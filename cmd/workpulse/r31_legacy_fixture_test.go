package main

import (
	"context"
	"encoding/json"
	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/seatcapacity"
	"strings"
	"time"
)

// Old route-feature fixtures explicitly opt into public access and the new hard
// capacity extension; new R31 negative tests use the production methods directly.
func r31LegacyNode(n hosted.Node) hosted.Node {
	if n.AccessScope == "" {
		n.AccessScope = "public"
	}
	if n.QuotaMode != "soft" && n.QuotaPolicyRevision == "" {
		n.QuotaPolicyRevision = strings.Repeat("a", 64)
	}
	return n
}
func r31LegacyUpsertNode(s *hosted.Store, ctx context.Context, n hosted.Node) error {
	return s.UpsertNode(ctx, r31LegacyNode(n))
}
func r31LegacyRegisterNode(s *hosted.Store, ctx context.Context, n hosted.Node, by string) (string, error) {
	return s.RegisterNode(ctx, r31LegacyNode(n), by)
}
func r31LegacySetOnline(s *hosted.Store, ctx context.Context, id string, raw json.RawMessage) error {
	var m map[string]any
	if json.Unmarshal(raw, &m) == nil && m["quota_mode"] == nil && m["seat_capacity"] == nil {
		n, err := s.GetNode(ctx, id)
		if err != nil {
			return err
		}
		var labels map[string]string
		_ = json.Unmarshal(n.Labels, &labels)
		if labels["quota_mode"] == "hard" {
			cpu := int64(64000)
			if v, ok := m["cpu_total"].(float64); ok {
				cpu = int64(v) * 1000
			}
			memory := int64(0)
			if v, ok := m["mem_free_mb"].(float64); ok {
				memory = int64(v) << 20
			}
			disk := int64(1 << 40)
			if v, ok := m["disk_free_mb"].(float64); ok {
				disk = int64(v) << 20
			}
			m["quota_mode"], m["quota_policy_revision"] = "hard", labels["quota_policy_revision"]
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
