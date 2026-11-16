package main

import (
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"path/filepath"
	"testing"
	"time"
)

func TestOutboundIntentRetentionStartupUsesEnvironment(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		valid bool
		want  time.Duration
	}{{"", true, 7 * 24 * time.Hour}, {"168h", true, 168 * time.Hour}, {"48h", true, 48 * time.Hour}, {"26h1ns", true, 26*time.Hour + time.Nanosecond}, {"24h", false, 0}, {"26h", false, 0}, {"-1h", false, 0}, {"invalid", false, 0}} {
		t.Run(tc.raw, func(t *testing.T) {
			t.Setenv("WP_OUTBOUND_INTENT_RETENTION", tc.raw)
			db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			err = configureOutboundIntentRetentionForRun(db)
			if (err == nil) != tc.valid {
				t.Fatal("startup retention validation", err)
			}
			if tc.valid && db.OutboundIntentRetention() != tc.want {
				t.Fatal("startup ignored environment value")
			}
		})
	}
}
