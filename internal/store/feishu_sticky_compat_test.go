package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
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

func stickyFreeAddress(t *testing.T) string {
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
func stickyRunOldHub(t *testing.T, binary, dbpath, stage string) {
	t.Helper()
	root := t.TempDir()
	readyRoot, err := os.MkdirTemp("/tmp", "r14-ready-")
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
	addr := stickyFreeAddress(t)
	internal := stickyFreeAddress(t)
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
func TestFeishuStickyOldHubMigrationRollbackCompatibility(t *testing.T) {
	binary := os.Getenv("R14_OLD_HUB_BINARY")
	if binary == "" {
		t.Skip("R14_OLD_HUB_BINARY not supplied; exact-r48 compatibility fixture is run by the R14 evidence gate")
	}
	ver, err := exec.Command(binary, "version").Output()
	if err != nil || !strings.Contains(string(ver), "1e17255f28ced042784337903914ca8c126ef4cf") {
		t.Fatal("OLD_HUB_SOURCE_PIN", string(ver), err)
	}
	root := t.TempDir()
	source := filepath.Join(root, "r48.db")
	stickyRunOldHub(t, binary, source, "r48-original")
	data, err := os.ReadFile(source)
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
	var count int
	if err = db.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version='0072_feishu_agent_sticky.sql'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("NOT_R48_DB_COPY", count, err)
	}
	// 精确旧 Hub 创建的现网同构库副本：保留旧账逐行值，不能只验新表。
	ledger := func() string {
		rows, e := db.db.Query(`SELECT version,digest,applied_at FROM schema_migrations WHERE version<>'0072_feishu_agent_sticky.sql' ORDER BY version`)
		if e != nil {
			t.Fatal(e)
		}
		defer rows.Close()
		var snapshot strings.Builder
		n := 0
		for rows.Next() {
			var v, d, a string
			if e = rows.Scan(&v, &d, &a); e != nil {
				t.Fatal(e)
			}
			fmt.Fprintf(&snapshot, "%s|%s|%s\n", v, d, a)
			n++
		}
		if e = rows.Err(); e != nil {
			t.Fatal(e)
		}
		if n != 70 {
			t.Fatalf("R48_LEDGER_ROWS got=%d want=70", n)
		}
		return fmt.Sprintf("%x", sha256.Sum256([]byte(snapshot.String())))
	}
	before := ledger()
	if err = db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if after := ledger(); after != before {
		t.Fatal("R48_LEDGER_REWRITTEN", before, after)
	}
	if err = db.db.QueryRow(`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil || count != 71 {
		t.Fatal("UPGRADE_LEDGER_COUNT", count, err)
	}
	if err = db.Migrate(ctx); err != nil {
		t.Fatal("UPGRADE_RESTART", err)
	}
	t.Logf("R48_UPGRADE old_rows=70 new_rows=71 added=0072 old_ledger_sha256=%s unchanged=true restart=ok", before)
	now := time.Now().UTC()
	for i := 0; i < 2; i++ {
		k := FeishuStickyKey{"owner", fmt.Sprint(i)}
		o, _, e := db.BeginFeishuSticky(ctx, k, "selected", "select-only", now)
		if e != nil {
			t.Fatal(e)
		}
		if e = db.FinishFeishuSticky(ctx, o, "accepted", "stable-agent", "select", now); e != nil {
			t.Fatal(e)
		}
		if _, _, e = db.BeginFeishuSticky(ctx, k, "pending", "explicit", now); e != nil {
			t.Fatal(e)
		}
	}
	db.Close()
	stickyRunOldHub(t, binary, copyPath, "r48-on-0072")
	db, err = OpenSQLite(copyPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.db.Exec(`BEGIN IMMEDIATE; UPDATE r14_feishu_sticky_state SET expires_at_utc=updated_at_utc; UPDATE r14_feishu_sticky_operation SET status='unknown' WHERE status='pending'; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	var invalid, unknown, states, ops int
	db.db.QueryRow(`SELECT count(*) FROM r14_feishu_sticky_state WHERE expires_at_utc<=updated_at_utc`).Scan(&invalid)
	db.db.QueryRow(`SELECT count(*) FROM r14_feishu_sticky_operation WHERE status='unknown'`).Scan(&unknown)
	db.db.QueryRow(`SELECT count(*) FROM r14_feishu_sticky_state`).Scan(&states)
	db.db.QueryRow(`SELECT count(*) FROM r14_feishu_sticky_operation`).Scan(&ops)
	if invalid != 2 || unknown != 2 || states != 2 || ops != 4 {
		t.Fatal("ROLLBACK_NOT_ALL_INVALIDATED_OR_ROWS_LOST", invalid, unknown, states, ops)
	}
	t.Logf("ROLLBACK_SQL states=%d invalidated=%d operations=%d pending_to_unknown=%d tables_retained=true", states, invalid, ops, unknown)
	// A conflicting 0072 object must roll back its whole migration transaction.
	badPath := filepath.Join(root, "failed-migration.db")
	if err = os.WriteFile(badPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	bad, err := OpenSQLite(badPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = bad.db.Exec(`CREATE TABLE r14_feishu_sticky_operation(conflicting_shape TEXT)`); err != nil {
		t.Fatal(err)
	}
	if err = bad.Migrate(ctx); err == nil {
		t.Fatal("MIGRATION_FAILURE_NOT_PROPAGATED")
	}
	for _, q := range []string{`SELECT count(*) FROM sqlite_master WHERE name='r14_feishu_sticky_state'`, `SELECT count(*) FROM schema_migrations WHERE version='0072_feishu_agent_sticky.sql'`} {
		if e := bad.db.QueryRow(q).Scan(&count); e != nil || count != 0 {
			t.Fatal("MIGRATION_PARTIAL_COMMIT", count, e)
		}
	}
	bad.Close()
	stickyRunOldHub(t, binary, badPath, "r48-after-failed-0072")
}
