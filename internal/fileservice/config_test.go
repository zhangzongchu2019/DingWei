package fileservice

import (
	"context"
	"strings"
	"testing"
	"time"
)

func setRequiredConfigEnv(t *testing.T) {
	t.Helper()
	t.Setenv("FS_DATA_ROOT", t.TempDir())
	t.Setenv("FS_INTERNAL_ISSUER_TOKEN", "issuer")
	t.Setenv("FS_INTERNAL_AUDIT_TOKEN", "audit")
}

func TestConfigRequiresLocalRootAndKeepsCapacityConfigurable(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("FS_TOTAL_QUOTA_BYTES", "12345")
	t.Setenv("FS_RETENTION", "48h")
	t.Setenv("FS_TOMBSTONE_RETENTION", "72h")
	c, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.QuotaBytes != 12345 || c.Retention.Hours() != 48 || c.TombstoneRetention.Hours() != 72 {
		t.Fatalf("config=%+v", c)
	}
}

func TestConfigRejectsNonPositiveNumericAndDurationFields(t *testing.T) {
	fields := []string{
		"FS_MAX_FILE_BYTES",
		"FS_TOTAL_QUOTA_BYTES",
		"FS_RETENTION",
		"FS_TOMBSTONE_RETENTION",
		"FS_SWEEP_INTERVAL",
		"FS_CAPABILITY_MAX_AGE",
	}
	for _, field := range fields {
		for _, value := range []string{"0", "-1"} {
			t.Run(field+"="+value, func(t *testing.T) {
				setRequiredConfigEnv(t)
				t.Setenv(field, value)
				if _, err := ConfigFromEnv(); err == nil {
					t.Fatalf("non-positive config accepted: %s=%s", field, value)
				}
			})
		}
	}
}

func TestValidRetentionConfigActuallyDeletesExpiredObject(t *testing.T) {
	setRequiredConfigEnv(t)
	t.Setenv("FS_RETENTION", "1h")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	m, clock, objects := testManager(t, cfg.QuotaBytes)
	m.Retention = cfg.Retention
	f, err := m.Put(context.Background(), Upload{OwnerKey: "owner", SessionName: "agent", OriginalName: "expires", ContentLength: 4, Body: strings.NewReader("data")})
	if err != nil {
		t.Fatal(err)
	}
	clock.Time = f.ExpiresAt.Add(time.Second)
	if err := m.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := m.Repo.Get(context.Background(), f.ID)
	if err != nil || got.State != StateExpired || got.RemovedAt == nil {
		t.Fatalf("expired metadata=%+v err=%v", got, err)
	}
	objects.mu.Lock()
	_, remains := objects.data[f.ObjectKey]
	objects.mu.Unlock()
	if remains {
		t.Fatal("expired object bytes were not deleted")
	}
}
