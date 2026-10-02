package m8

import (
	"testing"
	"time"
)

// 组长 R-M8-KEEPALIVE-DEFAULT（复验 tosonnet50ts 对 f11b5b70 的建议）：零值 Hub 的保活参数＝生产默认 30s／5s，
// 原先无格钉住（其余用例都显式设值）——默认常量改成 0 或负数、或把「0 取默认」的映射改坏，生产保活会悄悄关掉而用例全绿。
// 期望值写成字面量（不引用 default 常量），改常量即红。
func TestKeepaliveParamsDefaultsAndMapping(t *testing.T) {
	for _, c := range []struct {
		name                      string
		interval, timeout         time.Duration
		wantInterval, wantTimeout time.Duration
		wantEnabled               bool
	}{
		{"zero value is the production default", 0, 0, 30 * time.Second, 5 * time.Second, true},
		{"negative interval disables keepalive", -1, 0, 0, 0, false},
		{"negative interval disables even with a timeout", -time.Second, 2 * time.Second, 0, 0, false},
		{"explicit values are kept", 20 * time.Millisecond, 3 * time.Second, 20 * time.Millisecond, 3 * time.Second, true},
		{"zero timeout takes the default", 25 * time.Millisecond, 0, 25 * time.Millisecond, 5 * time.Second, true},
		{"negative timeout takes the default", 25 * time.Millisecond, -time.Second, 25 * time.Millisecond, 5 * time.Second, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			gotInterval, gotTimeout, gotEnabled := keepaliveParams(c.interval, c.timeout)
			if gotInterval != c.wantInterval || gotTimeout != c.wantTimeout || gotEnabled != c.wantEnabled {
				t.Fatalf("keepaliveParams(%s, %s) = (%s, %s, %v), want (%s, %s, %v)",
					c.interval, c.timeout, gotInterval, gotTimeout, gotEnabled, c.wantInterval, c.wantTimeout, c.wantEnabled)
			}
		})
	}
	// 零值 Hub 走的正是这条路：实例字段未设即 0。
	var h Hub
	if i, to, on := keepaliveParams(h.keepaliveInterval, h.keepaliveTimeout); i != 30*time.Second || to != 5*time.Second || !on {
		t.Fatalf("zero-value Hub keepalive = (%s, %s, %v), want (30s, 5s, true)", i, to, on)
	}
}
