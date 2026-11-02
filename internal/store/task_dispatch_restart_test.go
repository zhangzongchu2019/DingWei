package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
)

func TestR10RestartChildAuthority(t *testing.T) {
	path := os.Getenv("R10_RESTART_DB")
	if path == "" {
		t.Skip("subprocess-only probe, exercised by TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice")
	}
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	var k td.Key
	if err = json.Unmarshal([]byte(os.Getenv("R10_RESTART_KEY")), &k); err != nil {
		t.Fatal(err)
	}
	now, err := time.Parse(time.RFC3339Nano, os.Getenv("R10_RESTART_NOW"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := db.TaskDispatchLock(ctx, k)
	if err != nil || l == nil {
		t.Fatal("R10_PROCESS_RESTART_LOST_AUTHORITY", err)
	}
	hash := sha256.Sum256([]byte(l.LeaseToken))
	if hex.EncodeToString(hash[:]) != os.Getenv("R10_RESTART_TOKEN_HASH") || l.Generation != 1 || l.HolderAgentID != "agent-1" {
		t.Fatal("R10_PROCESS_RESTART_REASSIGNED_AUTHORITY")
	}
	if l.Effective(now) != (os.Getenv("R10_RESTART_EFFECTIVE") == "1") {
		t.Fatal("R10_PROCESS_RESTART_RESET_EXPIRY")
	}
	t.Logf("R10_PROCESS_RESTART generation=%d effective=%t grant_utc=%s expiry_utc=%s token_hash_match=true", l.Generation, l.Effective(now), l.GrantedAt.Format(time.RFC3339), l.ExpiresAt.Format(time.RFC3339))
}
func TestR10SeparateProcessRestartPreservesAuthorityAndUnknownNotice(t *testing.T) {
	db, m, now := r10DB(t)
	ctx := context.Background()
	granted, err := r10Apply(t, db, m, now)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := db.TaskDispatchNotices(ctx, "pending", 100)
	if err != nil || len(pending) != 1 {
		t.Fatal(err)
	}
	if ok, err := db.UpdateTaskDispatchNotice(ctx, pending[0].ID, "pending", "attempting", "", "", now); err != nil || !ok {
		t.Fatal(err)
	}
	var seq int
	var name, path string
	if err = db.db.QueryRow(`PRAGMA database_list`).Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	key, _ := json.Marshal(m.Key)
	tokenHash := sha256.Sum256([]byte(granted.Lock.LeaseToken))
	for _, at := range []time.Time{now.Add(time.Minute), granted.Lock.ExpiresAt} {
		childCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		cmd := exec.CommandContext(childCtx, binary, "-test.run=^TestR10RestartChildAuthority$", "-test.count=1", "-test.v")
		effective := "0"
		if at.Before(granted.Lock.ExpiresAt) {
			effective = "1"
		}
		cmd.Env = append(os.Environ(), "R10_RESTART_DB="+path, "R10_RESTART_KEY="+string(key), "R10_RESTART_NOW="+at.Format(time.RFC3339Nano), "R10_RESTART_EFFECTIVE="+effective, "R10_RESTART_TOKEN_HASH="+hex.EncodeToString(tokenHash[:]))
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("R10_REAL_PROCESS_RESTART_FAILED: %v\n%s", err, output)
		}
		if !strings.Contains(string(output), "token_hash_match=true") {
			t.Fatal("R10_RESTART_CHILD_PROBE_NOT_EXECUTED")
		}
		t.Logf("%s", output)
	}
	reopened, err := OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	t.Setenv("DW_TASK_DISPATCH_LOCK_ENABLED", "1")
	service := td.Service{Repo: reopened, Identity: identity.NewMemStore(), Reader: &specread.Service{}, Now: func() time.Time { return now.Add(time.Minute) }}
	if err = service.RecoverNotices(ctx); err != nil {
		t.Fatal(err)
	}
	notice, err := reopened.TaskDispatchNotice(ctx, pending[0].ID)
	if err != nil || notice.State != "unknown" {
		t.Fatal("R10_RESTART_FALSE_NOTIFICATION_SUCCESS", err)
	}
	result, err := r10Apply(t, reopened, m, now.Add(time.Minute))
	if err != nil || !result.Replay || !result.CurrentLease || result.Lock.LeaseToken != granted.Lock.LeaseToken {
		t.Fatal("R10_RESTART_LOST_OPERATION_IDEMPOTENCE", err)
	}
}
