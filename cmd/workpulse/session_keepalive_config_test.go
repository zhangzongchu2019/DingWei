package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/m8"
)

func TestSessionKeepaliveStartupReadsEnvironmentAndRejectsInvalid(t *testing.T) {
	h := &m8.Hub{}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	t.Setenv("WP_SESSION_KEEPALIVE_INTERVAL", "12s")
	t.Setenv("WP_SESSION_KEEPALIVE_TIMEOUT", "3s")
	if err := configureSessionKeepaliveForRun(h, logger); err != nil {
		t.Fatal(err)
	}
	if h.SessionKeepaliveInterval() != 12*time.Second || h.SessionKeepaliveTimeout() != 3*time.Second || !strings.Contains(logs.String(), "12s") {
		t.Fatal("startup settings not applied/logged", logs.String())
	}
	logs.Reset()
	t.Setenv("WP_SESSION_KEEPALIVE_TIMEOUT", "private-invalid-duration")
	if configureSessionKeepaliveForRun(h, logger) == nil {
		t.Fatal("invalid value did not stop startup")
	}
	if logs.Len() != 0 {
		t.Fatal("invalid/raw config leaked to startup log")
	}
}
