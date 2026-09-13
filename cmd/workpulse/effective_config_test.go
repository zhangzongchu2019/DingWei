package main

import (
	"strings"
	"testing"

	"github.com/zhangzongchu2019/dingwei/internal/config"
)

func TestWorkpulseEffectiveConfigVersionEnforceSources(t *testing.T) {
	t.Run("default warn is visibly risky", func(t *testing.T) {
		t.Setenv("WP_VERSION_ENFORCE", "")
		line := config.Line(workpulseEffectiveConfig())
		if !strings.Contains(line, "WP_VERSION_ENFORCE=warn(default!)") {
			t.Fatalf("effective config=%q", line)
		}
	})
	t.Run("explicit enforce", func(t *testing.T) {
		t.Setenv("WP_VERSION_ENFORCE", "enforce")
		line := config.Line(workpulseEffectiveConfig())
		if !strings.Contains(line, "WP_VERSION_ENFORCE=enforce(env)") {
			t.Fatalf("effective config=%q", line)
		}
	})
	t.Run("explicit warn differs from default", func(t *testing.T) {
		t.Setenv("WP_VERSION_ENFORCE", "warn")
		line := config.Line(workpulseEffectiveConfig())
		if !strings.Contains(line, "WP_VERSION_ENFORCE=warn(env)") || strings.Contains(line, "WP_VERSION_ENFORCE=warn(default") {
			t.Fatalf("effective config=%q", line)
		}
	})
}
