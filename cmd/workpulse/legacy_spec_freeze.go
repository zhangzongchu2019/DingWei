package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func configureLegacySpecFreeze(ctx context.Context, db *store.SQLite, raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return fmt.Errorf("WP_LEGACY_SPEC_FROZEN must be true/false or 1/0")
	}
	if enabled {
		return db.EnableLegacySpecFreeze(ctx)
	}
	return nil
}
