package main

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"os"
	"testing"
)

func TestVerifyRetentionUsesProductionPathWithoutStartingServer(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("FS_DATA_ROOT", "relative-production-path-must-not-be-read")
	t.Setenv("FS_INTERNAL_ISSUER_TOKEN", "")
	called := false
	var out bytes.Buffer
	err := execute([]string{"file-service", "verify-retention", "--work-dir", workDir}, &out, slog.New(slog.NewTextHandler(io.Discard, nil)), func(*slog.Logger) error {
		called = true
		return errors.New("server sentinel called")
	})
	if err != nil || called || out.String() != "RETENTION_DIRECTORY_OK\n" {
		t.Fatalf("err=%v server_called=%t out=%q", err, called, out.String())
	}
	entries, err := os.ReadDir(workDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("isolated work directory not cleaned: entries=%v err=%v", entries, err)
	}
}

func TestVerifyRetentionRejectsInvalidInvocationBeforeServer(t *testing.T) {
	called := false
	err := execute([]string{"file-service", "verify-retention"}, io.Discard, slog.Default(), func(*slog.Logger) error { called = true; return nil })
	if err == nil || called {
		t.Fatalf("invalid invocation err=%v server_called=%t", err, called)
	}
}
