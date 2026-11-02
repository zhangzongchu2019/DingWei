package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	td "github.com/zhangzongchu2019/dingwei/internal/taskdispatch"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Readiness helper reused from frozen R14 cf6134a6, only renamed for R10.
func r10CompatFreeAddress(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// Uses an independently compiled exact-r48 binary, never the new Go test binary.
// The final R14 evidence runner supplies this fixture, with source/tree/SHA256.
func r10CompatRunOldHub(t *testing.T, binary, dbpath, stage string) {
	t.Helper()
	root := t.TempDir()
	readyRoot, err := os.MkdirTemp("/tmp", "r10-ready-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(readyRoot)
	readyPath := filepath.Join(readyRoot, "notify")
	ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: readyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer ready.Close()
	addr := r10CompatFreeAddress(t)
	internal := r10CompatFreeAddress(t)
	var out bytes.Buffer
	cmd := exec.Command(binary)
	cmd.Env = []string{"NOTIFY_SOCKET=" + readyPath, "PATH=/usr/bin:/bin", "HOME=" + root, "TMPDIR=" + root, "TZ=UTC", "WP_DATA_DIR=" + root, "WP_DB_PATH=" + dbpath, "WP_ADDR=" + addr, "WP_INTERNAL_ADDR=" + internal, "FEISHU_TRANSPORT=fake", "WP_CONSOLE_AUTH_ENABLED=false", "WP_SCHEDULE_TEAM_FILE=" + filepath.Join(root, "none"), "WP_SCHEDULE_PERSONAL_DIR=" + root, "WP_ADMIN_INIT_PASSWORD=isolated-fixture-only"}
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	stopped := false
	defer func() {
		if !stopped {
			cmd.Process.Kill()
			<-done
		}
	}()
	// HTTP health can precede signal.Notify in r48. Wait for its real READY
	// notification before testing graceful shutdown; never race startup with SIGTERM.
	ready.SetReadDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 4096)
	n, _, err := ready.ReadFromUnix(buf)
	if err != nil || !strings.Contains(string(buf[:n]), "READY=1") {
		t.Fatal("OLD_HUB_READY_TIMEOUT", stage, err)
	}
	client := &http.Client{Timeout: 200 * time.Millisecond}
	healthy := false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case err := <-done:
			stopped = true
			t.Fatalf("OLD_HUB_START_FAILED stage=%s err=%v log=%s", stage, err, out.String())
		default:
		}
		r, err := client.Get("http://" + addr + "/healthz")
		if err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				healthy = true
				break
			}
		}
		time.Sleep(25 * time.Millisecond)
	}
	if !healthy {
		t.Fatal("OLD_HUB_HEALTH_TIMEOUT", stage)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		stopped = true
		if err != nil {
			t.Fatal("OLD_HUB_EXIT_FAILED", stage, err, out.String())
		}
	case <-time.After(20 * time.Second):
		t.Fatal("OLD_HUB_EXIT_TIMEOUT", stage)
	}
	t.Logf("OLD_HUB stage=%s healthz=200 exit=0 fake_feishu=true", stage)
	// Keep the complete owned-child startup/shutdown output in verbose evidence.
	// The explicit fixture password above prevents bootstrap printing a new secret.
	t.Logf("OLD_HUB_LOG stage=%s\n%s", stage, out.String())
}
func TestR10OldR48HubUpgradeAndRollbackOnIsolatedCopy(t *testing.T) {
	binary := os.Getenv("R10_OLD_HUB_BINARY")
	if binary == "" {
		binary = os.Getenv("R14_OLD_HUB_BINARY")
	}
	if binary == "" {
		t.Skip("exact-r48 binary not provided; supplied by the isolated R10 compatibility evidence gate")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil || !strings.Contains(string(version), "1e17255f28ced042784337903914ca8c126ef4cf") {
		t.Fatal("R10_OLD_BINARY_SOURCE_PIN", err)
	}
	root := t.TempDir()
	original := filepath.Join(root, "r48.db")
	r10CompatRunOldHub(t, binary, original, "r48-original")
	data, err := os.ReadFile(original)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(root, "r48-copy.db")
	if err = os.WriteFile(copyPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	db, err := OpenSQLite(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	ledger := func() string {
		rows, err := db.db.Query(`SELECT version,digest,applied_at FROM schema_migrations WHERE version<>'0073_task_dispatch_lock.sql' ORDER BY version`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var b strings.Builder
		n := 0
		for rows.Next() {
			var v, d, a string
			if err = rows.Scan(&v, &d, &a); err != nil {
				t.Fatal(err)
			}
			fmt.Fprintf(&b, "%s|%s|%s\n", v, d, a)
			n++
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if n != 70 {
			t.Fatal("R10_OLD_LEDGER_NOT_70", n)
		}
		return fmt.Sprintf("%x", sha256.Sum256([]byte(b.String())))
	}
	before := ledger()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := ledger(); after != before {
		t.Fatal("R10_UPGRADE_OLD_LEDGER_CHANGED", before, after)
	}
	var count int
	if err = db.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != 71 {
		t.Fatal("R10_UPGRADE_NOT_70_TO_71", count, err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal("R10_UPGRADE_RESTART", err)
	}
	for _, id := range []string{"dispatcher", "holder"} {
		if err = db.UpsertConsolePrincipal(ctx, model.ConsolePrincipal{ID: id, Provider: "feishu", ProviderIssuer: "test", ProviderSubject: id, DisplayName: id, Active: true}); err != nil {
			t.Fatal(err)
		}
		if err = db.SetSpecReadBinding(ctx, "admin", specread.Binding{Principal: id, Provider: "github", Instance: "github.com", AccountID: "123", Login: id}); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC()
	m := td.Mutation{OperationID: "migration_isolation_grant_01", Kind: "grant", Key: td.Key{Provider: "github", Instance: "github.com", Repository: "1", ChangeDir: "openspec/changes/example", TaskID: "T-01"}, ActorKind: "user", ActorUserLC: "dispatcher", HolderAgentID: "agent-1", HolderOwnerLC: "holder", AssignedByUserLC: "dispatcher", AuthorizationGroup: "group", CommitSHA: strings.Repeat("a", 40), TasksBlobSHA: strings.Repeat("b", 40), TaskLine: 1, MonitorRef: "main", PRDimension: "pr_not_opened", ComparisonState: "clean", Bindings: []td.BindingFact{{Principal: "dispatcher", Provider: "github", Instance: "github.com", AccountID: "123", Login: "dispatcher"}, {Principal: "holder", Provider: "github", Instance: "github.com", AccountID: "123", Login: "holder"}}}
	granted, err := db.ApplyTaskDispatch(ctx, m, func() time.Time { return now }, func() error { return nil })
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
	db.Close()
	r10CompatRunOldHub(t, binary, copyPath, "r48-on-0073")
	db, err = OpenSQLite(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sql, err := os.ReadFile("../../openspec/changes/r10-task-dispatch-lock/rollback.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.db.Exec(string(sql)); err != nil {
		t.Fatal("R10_ROLLBACK_SQL_FAILED", err)
	}
	lock, err := db.TaskDispatchLock(ctx, m.Key)
	if err != nil || lock.State != "expired" || lock.Generation <= granted.Lock.Generation || lock.LeaseToken == granted.Lock.LeaseToken {
		t.Fatal("R10_ROLLBACK_LEFT_VALID_AUTHORITY", err)
	}
	notices, err := db.TaskDispatchNotices(ctx, "unknown", 100)
	if err != nil || len(notices) != 1 {
		t.Fatal("R10_ROLLBACK_FALSE_NOTIFY_SUCCESS", len(notices), err)
	}
	events, err := db.TaskDispatchEvents(ctx, m.Key, 0)
	if err != nil || len(events) != 2 || events[0].Type != "grant" || events[1].Type != "rollback" {
		t.Fatal("R10_ROLLBACK_HISTORY_LOST", len(events), err)
	}
	if after := ledger(); after != before {
		t.Fatal("R10_ROLLBACK_OLD_LEDGER_CHANGED")
	}
	if _, err = db.db.Exec(string(sql)); err != nil {
		t.Fatal("R10_ROLLBACK_NOT_REPEATABLE", err)
	}
	events, _ = db.TaskDispatchEvents(ctx, m.Key, 0)
	if len(events) != 2 {
		t.Fatal("R10_ROLLBACK_DUPLICATE_INVALIDATION_EVENT", len(events))
	}
	t.Logf("R10_R48_UPGRADE old_rows=70 new_rows=71 added=0073 old_ledger_sha256=%s unchanged=true; rollback state=expired generation=%d unknown_notices=1 events=2 tables_retained=true", before, lock.Generation)
	db.Close()
	r10CompatRunOldHub(t, binary, copyPath, "r48-after-r10-rollback")
	badPath := filepath.Join(root, "failed-0073.db")
	if err = os.WriteFile(badPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	bad, err := OpenSQLite(badPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.db.Exec(`CREATE TABLE r10_task_lock_operation(conflicting_shape TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err = bad.Migrate(ctx); err == nil {
		t.Fatal("R10_MIGRATION_FAILURE_NOT_PROPAGATED")
	}
	for _, query := range []string{`SELECT count(*) FROM sqlite_master WHERE name='r10_task_lock'`, `SELECT count(*) FROM schema_migrations WHERE version='0073_task_dispatch_lock.sql'`} {
		if err = bad.db.QueryRow(query).Scan(&count); err != nil || count != 0 {
			t.Fatal("R10_MIGRATION_PARTIAL_COMMIT", count, err)
		}
	}
	bad.Close()
	r10CompatRunOldHub(t, binary, badPath, "r48-after-failed-0073")
}
