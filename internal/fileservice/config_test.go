package fileservice

import "testing"

func TestConfigRequiresMinIOAndKeepsCapacityConfigurable(t *testing.T) {
	t.Setenv("FS_MINIO_ENDPOINT", "minio:9000")
	t.Setenv("FS_MINIO_ACCESS_KEY", "access")
	t.Setenv("FS_MINIO_SECRET_KEY", "secret")
	t.Setenv("FS_INTERNAL_ISSUER_TOKEN", "issuer")
	t.Setenv("FS_INTERNAL_AUDIT_TOKEN", "audit")
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

func TestConfigRejectsZeroTombstoneRetention(t *testing.T) {
	t.Setenv("FS_MINIO_ENDPOINT", "minio:9000")
	t.Setenv("FS_MINIO_ACCESS_KEY", "access")
	t.Setenv("FS_MINIO_SECRET_KEY", "secret")
	t.Setenv("FS_INTERNAL_ISSUER_TOKEN", "issuer")
	t.Setenv("FS_INTERNAL_AUDIT_TOKEN", "audit")
	t.Setenv("FS_TOMBSTONE_RETENTION", "0")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("zero tombstone retention was accepted")
	}
}
