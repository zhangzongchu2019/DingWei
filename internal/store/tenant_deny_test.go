package store

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Wrap the real isolated SQLite connection: count every non-audit SQL request,
// including Prepare fallback. Audit writes are allowed independently of reads.
type denySQLSpy struct {
	resources, audits int
	fail              bool
}
type denyConnector struct {
	drv driver.Driver
	dsn string
	spy *denySQLSpy
}

func (c *denyConnector) Driver() driver.Driver { return c.drv }
func (c *denyConnector) Connect(context.Context) (driver.Conn, error) {
	conn, e := c.drv.Open(c.dsn)
	if e != nil {
		return nil, e
	}
	return &denyConn{Conn: conn, spy: c.spy}, nil
}

type denyConn struct {
	driver.Conn
	spy *denySQLSpy
}

func (c *denyConn) count(q string) error {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(q)), "insert into audit_log") {
		c.spy.audits++
		if c.spy.fail {
			return errors.New("synthetic audit failure")
		}
	} else {
		c.spy.resources++
	}
	return nil
}
func (c *denyConn) Prepare(q string) (driver.Stmt, error) {
	if e := c.count(q); e != nil {
		return nil, e
	}
	return c.Conn.Prepare(q)
}
func (c *denyConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	x, ok := c.Conn.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if e := c.count(q); e != nil {
		return nil, e
	}
	return x.ExecContext(ctx, q, args)
}
func (c *denyConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	x, ok := c.Conn.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	if e := c.count(q); e != nil {
		return nil, e
	}
	return x.QueryContext(ctx, q, args)
}
func newDenyStore(t *testing.T) (*SQLite, *denySQLSpy, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tenant-deny.db")
	s, e := OpenSQLite(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	raw := s.db
	spy := &denySQLSpy{}
	counted := sql.OpenDB(&denyConnector{drv: raw.Driver(), dsn: path, spy: spy})
	counted.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	s.db = counted
	return s, spy, raw
}

var denyNow = time.Date(2026, 11, 8, 6, 0, 0, 0, time.UTC)

func denyMessage(parent string) model.Message {
	m := clarificationGuardMessage(parent)
	m.FeishuReply.Authenticated = false
	m.FeishuReply.Tenant = "foreign-tenant-secret"
	m.FeishuReply.ContentSHA = strings.Repeat("b", 64)
	return m
}
func assertStageDeny(t *testing.T, s *SQLite, parent string) {
	t.Helper()
	result, e := s.StageClarificationInbound(context.Background(), denyMessage(parent), "body-secret", denyNow)
	if e != ErrTaskFlowPermission || !reflect.DeepEqual(result, ClarificationInbound{}) {
		t.Fatalf("original deny changed: %#v %v", result, e)
	}
}

// YIZU_PEP_BEFORE_RESOLVE(store)
func TestStageDenyBeforeResourceSQL(t *testing.T) {
	s, spy, _ := newDenyStore(t)
	assertStageDeny(t, s, "resource-secret")
	if spy.resources != 0 || spy.audits != 1 {
		t.Fatalf("resource spy=%d audit spy=%d", spy.resources, spy.audits)
	}
}

// YIZU_DENY_NOT_EMPTY_SET(store)
func TestStageDenyNotSuccessfulEmptyResult(t *testing.T) {
	s, _, _ := newDenyStore(t)
	assertStageDeny(t, s, "resource-secret")
}

// YIZU_EXISTENCE_INDISTINGUISHABLE(store)
func TestStageDenyExistingMissingSameResponseAndAudit(t *testing.T) {
	s, now, _, _, n, _ := clarificationNoticeFixture(t)
	if e := s.FinishQuestionNotice(context.Background(), n.ID, "om_parent", true, now); e != nil {
		t.Fatal(e)
	}
	// Verify actual stored parent existence independently before the denied calls.
	var count int
	if e := s.db.QueryRow(`SELECT count(*) FROM spec_clarification_notice WHERE provider_message_id='om_parent'`).Scan(&count); e != nil || count != 1 {
		t.Fatalf("existing parent fixture count=%d err=%v", count, e)
	}
	if e := s.db.QueryRow(`SELECT count(*) FROM spec_clarification_notice WHERE provider_message_id='missing-parent'`).Scan(&count); e != nil || count != 0 {
		t.Fatal(count, e)
	}
	for _, p := range []string{"om_parent", "missing-parent"} {
		assertStageDeny(t, s, p)
	}
	rows, e := s.db.Query(`SELECT actor,action,target,after_json,ts FROM audit_log WHERE action='clarification.stage_denied' ORDER BY rowid`)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	var got [][5]string
	for rows.Next() {
		var a [5]string
		if e = rows.Scan(&a[0], &a[1], &a[2], &a[3], &a[4]); e != nil {
			t.Fatal(e)
		}
		got = append(got, a)
	}
	if e = rows.Err(); e != nil {
		t.Fatal(e)
	}
	if len(got) != 2 || got[0] != got[1] {
		t.Fatalf("existence influenced minimal audit: %#v", got)
	}
}

// YIZU_MINIMAL_DENY_AUDIT(store)
func TestStageDenyMinimalAudit(t *testing.T) {
	s, spy, raw := newDenyStore(t)
	assertStageDeny(t, s, "resource-secret")
	var actor, action, target, details, at string
	if e := raw.QueryRow(`SELECT actor,action,target,after_json,ts FROM audit_log WHERE action='clarification.stage_denied'`).Scan(&actor, &action, &target, &details, &at); e != nil {
		t.Fatalf("deny audit missing: %v", e)
	}
	var d map[string]string
	if e := json.Unmarshal([]byte(details), &d); e != nil {
		t.Fatal(e)
	}
	if actor != blockerHash([]string{"bot", "ou_pm"}) || action != "clarification.stage_denied" || target != "" || at != denyNow.Format(time.RFC3339Nano) || !reflect.DeepEqual(d, map[string]string{"tenant": "unresolved", "op": "clarification.stage", "decision": "deny"}) {
		t.Fatalf("minimal audit shape: %q %q %q %q %q", actor, action, target, details, at)
	}
	all := actor + action + target + details + at
	for _, s := range []string{"resource-secret", "foreign-tenant-secret", "body-secret", "ou_pm"} {
		if strings.Contains(all, s) {
			t.Fatal("audit contains input")
		}
	}
	if spy.resources != 0 || spy.audits != 1 {
		t.Fatal("minimal audit unexpectedly resolved resources")
	}
}
func TestStageDenyAuditFailureStillRejects(t *testing.T) {
	s, spy, _ := newDenyStore(t)
	spy.fail = true
	assertStageDeny(t, s, "resource-secret")
	if spy.resources != 0 || spy.audits != 1 {
		t.Fatal("audit failure changed guard order")
	}
}
func TestStageDenyMetadataInvalidPreservesError(t *testing.T) {
	s, spy, _ := newDenyStore(t)
	m := clarificationGuardMessage("bad\nparent")
	out, e := s.StageClarificationInbound(context.Background(), m, "body", denyNow)
	if e != ErrTaskFlowInvalid || !reflect.DeepEqual(out, ClarificationInbound{}) || spy.resources != 0 || spy.audits != 1 {
		t.Fatalf("metadata invalid result=%#v err=%v spy=%#v", out, e, spy)
	}
}
func TestStageDenyUnavailableAuditStillRejects(t *testing.T) {
	s := &SQLite{}
	assertStageDeny(t, s, "resource-secret")
}
