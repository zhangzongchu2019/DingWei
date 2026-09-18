// Package fileservice implements the iteration-3 file data plane. It owns
// metadata and policy; object bytes remain behind the ObjectStore interface.
package fileservice

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	KiB = int64(1 << 10)
	MiB = int64(1 << 20)
	GiB = int64(1 << 30)

	LongTextThreshold   = 4 * KiB
	LongTextMaxBytes    = 8 * MiB
	defaultMaxFileBytes = 50 * MiB
	defaultQuotaBytes   = 500 * GiB
)

type Config struct {
	Addr               string
	InternalAddr       string
	DatabasePath       string
	DataRoot           string
	ObjectPrefix       string
	PublicBaseURL      string
	MaxFileBytes       int64
	QuotaBytes         int64
	Retention          time.Duration
	TombstoneRetention time.Duration
	SweepInterval      time.Duration
	CapabilityMaxAge   time.Duration
	IssuerToken        string
	AuditToken         string
}

func ConfigFromEnv() (Config, error) {
	c := Config{
		Addr:               env("FS_ADDR", "127.0.0.1:8793"),
		InternalAddr:       env("FS_INTERNAL_ADDR", "127.0.0.1:8794"),
		DatabasePath:       env("FS_DATABASE_PATH", "./data/file-service.db"),
		DataRoot:           env("FS_DATA_ROOT", "/var/lib/dingwei-file-service/objects"),
		ObjectPrefix:       env("FS_OBJECT_PREFIX", "files"),
		PublicBaseURL:      strings.TrimRight(os.Getenv("FS_PUBLIC_BASE_URL"), "/"),
		MaxFileBytes:       defaultMaxFileBytes,
		QuotaBytes:         defaultQuotaBytes,
		Retention:          7 * 24 * time.Hour,
		TombstoneRetention: 90 * 24 * time.Hour,
		SweepInterval:      time.Minute,
		CapabilityMaxAge:   2 * time.Minute,
		IssuerToken:        os.Getenv("FS_INTERNAL_ISSUER_TOKEN"),
		AuditToken:         os.Getenv("FS_INTERNAL_AUDIT_TOKEN"),
	}
	var err error
	if c.MaxFileBytes, err = envInt64("FS_MAX_FILE_BYTES", c.MaxFileBytes); err != nil {
		return Config{}, err
	}
	if c.QuotaBytes, err = envInt64("FS_TOTAL_QUOTA_BYTES", c.QuotaBytes); err != nil {
		return Config{}, err
	}
	if c.Retention, err = envDuration("FS_RETENTION", c.Retention); err != nil {
		return Config{}, err
	}
	if c.TombstoneRetention, err = envDuration("FS_TOMBSTONE_RETENTION", c.TombstoneRetention); err != nil {
		return Config{}, err
	}
	if c.SweepInterval, err = envDuration("FS_SWEEP_INTERVAL", c.SweepInterval); err != nil {
		return Config{}, err
	}
	if c.CapabilityMaxAge, err = envDuration("FS_CAPABILITY_MAX_AGE", c.CapabilityMaxAge); err != nil {
		return Config{}, err
	}
	// Every numeric/duration limit in Config has the same fail-closed zero
	// semantics. In particular, a non-positive SweepInterval must not reach
	// RunSweeper, where it would otherwise silently disable retention.
	if c.MaxFileBytes <= 0 || c.QuotaBytes <= 0 || c.Retention <= 0 || c.TombstoneRetention <= 0 || c.SweepInterval <= 0 || c.CapabilityMaxAge <= 0 {
		return Config{}, fmt.Errorf("file-service limits must be positive")
	}
	if !filepath.IsAbs(c.DataRoot) || c.ObjectPrefix == "" || strings.ContainsAny(c.ObjectPrefix, `/\\`) || c.ObjectPrefix == "." || c.ObjectPrefix == ".." {
		return Config{}, fmt.Errorf("FS_DATA_ROOT must be absolute and FS_OBJECT_PREFIX must be one path segment")
	}
	if c.IssuerToken == "" || c.AuditToken == "" {
		return Config{}, fmt.Errorf("FS_INTERNAL_ISSUER_TOKEN/audit token are required")
	}
	u, parseErr := url.Parse(c.PublicBaseURL)
	if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return Config{}, fmt.Errorf("FS_PUBLIC_BASE_URL must be an absolute https origin")
	}
	return c, nil
}

func env(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
func envInt64(key string, fallback int64) (int64, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}
func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return d, nil
}
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
