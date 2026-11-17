package main

import (
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"os"
)

func configureOutboundIntentRetentionForRun(db *store.SQLite) error {
	return db.ConfigureOutboundIntentRetention(os.Getenv("WP_OUTBOUND_INTENT_RETENTION"))
}
