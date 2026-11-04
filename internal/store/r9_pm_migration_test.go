package store

import (
	"context"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/clock"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestR9PMMigrationBackfillsExactSchemaAndRefusesPartial(t *testing.T) {
	ctx := context.Background()
	t.Run("complete objects backfill ledger", func(t *testing.T) {
		s := openMigrated(t, filepath.Join(t.TempDir(), "backfill.db"))
		defer s.Close()
		if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='0074_pm_decision_request.sql'`); err != nil {
			t.Fatal(err)
		}
		if err := s.Migrate(ctx); err != nil {
			t.Fatalf("0074 complete-schema ledger backfill: %v", err)
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE version='0074_pm_decision_request.sql'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("backfill rows=%d err=%v", n, err)
		}
	})
	t.Run("partial objects refuse", func(t *testing.T) {
		s := openMigrated(t, filepath.Join(t.TempDir(), "partial.db"))
		defer s.Close()
		if _, err := s.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version='0074_pm_decision_request.sql'; DROP TABLE r9_pm_audit`); err != nil {
			t.Fatal(err)
		}
		err := s.Migrate(ctx)
		if err == nil || !strings.Contains(err.Error(), "0074_pm_decision_request.sql") || !strings.Contains(err.Error(), "partially present") {
			t.Fatalf("partial schema was not rejected at 0074: %v", err)
		}
	})
}

func TestR9PMResponsibilityEmptySetDeleteGenerationAndAudit(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "mapping.db"))
	defer s.Close()
	k := R9PMKey{"github", "github.com", "1234", "openspec/changes/demo", "PM01"}
	if _, err := s.GetR9PMResponsibility(ctx, k); err != ErrR9PMNotFound {
		t.Fatalf("new mapping is not empty: %v", err)
	}
	g, err := s.SetR9PMResponsibility(ctx, k, "pm@example.test", "admin@example.test")
	if err != nil || g != 1 {
		t.Fatalf("first set gen=%d err=%v", g, err)
	}
	now := s.now().UTC().Format("2006-01-02T15:04:05")
	_, err = s.db.ExecContext(ctx, `INSERT INTO r9_pm_request(request_id,provider,instance,repository_id,change_directory,commit_sha,point_id,content_sha256,pm_user_id_lc,mapping_generation,generation,status,created_at_utc,deadline_at_utc,summary_json,created_by_user_id_lc) VALUES('old',?,?,?,?,?,?,?,?,1,1,'open',?,?,?,?)`, k.Provider, k.Instance, k.RepositoryID, k.ChangeDirectory, "abc", k.PointID, "hash", "pm@example.test", now, now, `{"question":"Q","options":["A","B"],"recommendation":"A"}`, "requestor")
	if err != nil {
		t.Fatal(err)
	}
	g, err = s.SetR9PMResponsibilityAndReissue(ctx, k, "pm2@example.test", "admin@example.test", "bot", "ou_pm2")
	if err != nil || g != 2 {
		t.Fatalf("second set gen=%d err=%v", g, err)
	}
	var state string
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM r9_pm_request WHERE request_id='old'`).Scan(&state); err != nil || state != "superseded" {
		t.Fatalf("old request state=%q err=%v", state, err)
	}
	g, err = s.DeleteR9PMResponsibility(ctx, k, "admin@example.test")
	if err != nil || g != 3 {
		t.Fatalf("delete gen=%d err=%v", g, err)
	}
	v, err := s.GetR9PMResponsibility(ctx, k)
	if err != nil || v.PMUserIDLC != "" || v.MappingGeneration != 3 || v.DeletedAtUTC == "" {
		t.Fatalf("delete did not retain empty tombstone: %+v err=%v", v, err)
	}
	var audits int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM r9_pm_audit WHERE actor_user_id_lc='admin@example.test' AND action IN ('set','delete')`).Scan(&audits); err != nil || audits != 3 {
		t.Fatalf("audit rows=%d err=%v", audits, err)
	}
}

func TestR9PMRequestCreatesBothDeliveriesAndCASCloses(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "request.db"))
	defer s.Close()
	k := R9PMKey{"github", "github.com", "9", "openspec/changes/x", "PM01"}
	if _, err := s.SetR9PMResponsibility(ctx, k, "pm", "admin"); err != nil {
		t.Fatal(err)
	}
	in := R9PMCreateInput{RequestID: "req1", R9PMKey: k, CommitSHA: "abcdef", ContentSHA256: "digest", ConsoleRecipient: "pm", FeishuRecipient: "ou_pm", SummaryJSON: `{"question":"Q"}`}
	req, created, err := s.CreateR9PMRequest(ctx, in)
	if err != nil || !created || req.Generation != 1 || req.MappingGeneration != 1 {
		t.Fatalf("create %+v created=%v err=%v", req, created, err)
	}
	if _, created, err = s.CreateR9PMRequest(ctx, in); err != nil || created {
		t.Fatalf("duplicate was not deduped created=%v err=%v", created, err)
	}
	var deliveries int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM r9_pm_delivery WHERE request_id='req1' AND state='pending'`).Scan(&deliveries); err != nil || deliveries != 2 {
		t.Fatalf("pending delivery count=%d err=%v", deliveries, err)
	}
	accepted, err := s.AcceptR9PMResponse(ctx, R9PMResponseInput{ResponseID: "response1", RequestID: "req1", Generation: 1, PrincipalUserIDLC: "pm", Channel: "console", Decision: "agree", OperationID: "op1"})
	if err != nil || !accepted {
		t.Fatalf("first response accepted=%v err=%v", accepted, err)
	}
	accepted, err = s.AcceptR9PMResponse(ctx, R9PMResponseInput{ResponseID: "response2", RequestID: "req1", Generation: 1, PrincipalUserIDLC: "pm", Channel: "feishu", Decision: "disagree", Issuer: "bot", Tenant: "tenant", MessageID: "msg", EventID: "event2", PayloadSHA256: "payload2", OperationID: "op2"})
	if err != nil || accepted {
		t.Fatalf("second response accepted=%v err=%v", accepted, err)
	}
	var status string
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM r9_pm_request WHERE request_id='req1'`).Scan(&status); err != nil || status != "answered" {
		t.Fatalf("request status=%q err=%v", status, err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM r9_pm_delivery WHERE request_id='req1' AND state='pending'`).Scan(&deliveries); err != nil || deliveries != 0 {
		t.Fatalf("pending after answer=%d err=%v", deliveries, err)
	}
}

func TestR9PMSchedulerPersistsOneDuePeriodAndSkipsBacklog(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "cycle.db"))
	defer s.Close()
	k := R9PMKey{"github", "github.com", "x", "change", "PM01"}
	_, _ = s.SetR9PMResponsibility(ctx, k, "pm", "admin")
	now := time.Date(2026, 11, 4, 0, 0, 0, 0, time.UTC)
	s.SetClock(&clock.Fake{T: now})
	req, _, err := s.CreateR9PMRequest(ctx, R9PMCreateInput{RequestID: "cycle", R9PMKey: k, CommitSHA: "c", ContentSHA256: "d", ConsoleRecipient: "pm", FeishuRecipient: "ou_pm"})
	if err != nil {
		t.Fatal(err)
	}
	count, err := s.RunR9PMScheduler(ctx, now.Add(24*time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("first sweep count=%d err=%v", count, err)
	}
	count, err = s.RunR9PMScheduler(ctx, now.Add(24*time.Hour))
	if err != nil || count != 0 {
		t.Fatalf("repeat sweep count=%d err=%v", count, err)
	}
	count, err = s.RunR9PMScheduler(ctx, now.Add(37*time.Hour))
	if err != nil || count != 1 {
		t.Fatalf("restart catchup count=%d err=%v", count, err)
	}
	var reminders, missed int
	if err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM r9_pm_delivery WHERE request_id=? AND kind='reminder'`, req.RequestID).Scan(&reminders); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT missed_periods FROM r9_pm_request WHERE request_id=?`, req.RequestID).Scan(&missed); err != nil {
		t.Fatal(err)
	}
	if reminders != 4 || missed != 1 {
		t.Fatalf("reminders=%d missed=%d", reminders, missed)
	}
}

func TestR9PMConcurrentFirstResponseCAS50Rounds(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "concurrent.db"))
	defer s.Close()
	for i := 0; i < 50; i++ {
		k := R9PMKey{Provider: "github", Instance: "github.com", RepositoryID: "1", ChangeDirectory: fmt.Sprintf("change-%d", i), PointID: "PM01"}
		if _, err := s.SetR9PMResponsibility(ctx, k, "pm", "admin"); err != nil {
			t.Fatal(err)
		}
		id := fmt.Sprintf("req-%d", i)
		if _, _, err := s.CreateR9PMRequest(ctx, R9PMCreateInput{RequestID: id, R9PMKey: k, CommitSHA: "commit", ContentSHA256: "digest", ConsoleRecipient: "pm", FeishuRecipient: "ou_pm", FeishuIssuer: "bot"}); err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		results := make(chan bool, 2)
		errs := make(chan error, 2)
		inputs := []R9PMResponseInput{{ResponseID: fmt.Sprintf("c-%d", i), RequestID: id, Generation: 1, PrincipalUserIDLC: "pm", Channel: "console", Decision: "agree", OperationID: fmt.Sprintf("co-%d", i)}, {ResponseID: fmt.Sprintf("f-%d", i), RequestID: id, Generation: 1, PrincipalUserIDLC: "pm", Channel: "feishu", Decision: "disagree", Issuer: "bot", Tenant: "tenant", MessageID: fmt.Sprintf("msg-%d", i), EventID: fmt.Sprintf("event-%d", i), PayloadSHA256: "payload", OperationID: fmt.Sprintf("fo-%d", i)}}
		for _, in := range inputs {
			go func(v R9PMResponseInput) {
				<-start
				ok, err := s.AcceptR9PMResponse(ctx, v)
				results <- ok
				errs <- err
			}(in)
		}
		close(start)
		wins := 0
		for j := 0; j < 2; j++ {
			if err := <-errs; err != nil {
				t.Fatalf("round %d CAS: %v", i, err)
			}
			if <-results {
				wins++
			}
		}
		var accepted, late int
		if err := s.db.QueryRowContext(ctx, `SELECT SUM(state='accepted'),SUM(state='late') FROM r9_pm_response WHERE request_id=?`, id).Scan(&accepted, &late); err != nil {
			t.Fatal(err)
		}
		if wins != 1 || accepted != 1 || late != 1 {
			t.Fatalf("round %d winner=%d accepted=%d late=%d", i, wins, accepted, late)
		}
	}
}

func TestR9PMNewContentSupersedesOldGenerationAndLeavesLateEvidence(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "supersede.db"))
	defer s.Close()
	k := R9PMKey{"github", "github.com", "1", "change", "PM01"}
	_, _ = s.SetR9PMResponsibility(ctx, k, "pm", "admin")
	base := R9PMCreateInput{R9PMKey: k, CommitSHA: "commit1", ContentSHA256: "sha1", ConsoleRecipient: "pm", FeishuRecipient: "ou_pm", FeishuIssuer: "bot", SummaryJSON: `{"question":"Q","options":["A","B"],"recommendation":"A"}`}
	base.RequestID = "old-generation"
	old, _, err := s.CreateR9PMRequest(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	base.RequestID = "new-generation"
	base.CommitSHA = "commit2"
	base.ContentSHA256 = "sha2"
	newer, created, err := s.CreateR9PMRequest(ctx, base)
	if err != nil || !created || newer.Generation != old.Generation+1 {
		t.Fatalf("new generation=%+v created=%v err=%v", newer, created, err)
	}
	accepted, err := s.AcceptR9PMResponse(ctx, R9PMResponseInput{ResponseID: "late-old", RequestID: old.RequestID, Generation: old.Generation, PrincipalUserIDLC: "pm", Channel: "feishu", Decision: "agree", Issuer: "bot", Tenant: "tenant", MessageID: "old-message", EventID: "old-event", PayloadSHA256: "body"})
	if err != nil || accepted {
		t.Fatalf("old response accepted=%v err=%v", accepted, err)
	}
	var oldStatus, responseState string
	if err = s.db.QueryRowContext(ctx, `SELECT status FROM r9_pm_request WHERE request_id=?`, old.RequestID).Scan(&oldStatus); err != nil {
		t.Fatal(err)
	}
	if err = s.db.QueryRowContext(ctx, `SELECT state FROM r9_pm_response WHERE response_id='late-old'`).Scan(&responseState); err != nil {
		t.Fatal(err)
	}
	if oldStatus != "superseded" || responseState != "late" {
		t.Fatalf("old status=%s late state=%s", oldStatus, responseState)
	}
}

func TestR9PMUnknownReceiptIsNotBlindlyRetriedOrCalledSent(t *testing.T) {
	ctx := context.Background()
	s := openMigrated(t, filepath.Join(t.TempDir(), "unknown.db"))
	defer s.Close()
	k := R9PMKey{"github", "github.com", "1", "change", "PM01"}
	_, _ = s.SetR9PMResponsibility(ctx, k, "pm", "admin")
	_, _, err := s.CreateR9PMRequest(ctx, R9PMCreateInput{RequestID: "unknown", R9PMKey: k, CommitSHA: "c", ContentSHA256: "h", ConsoleRecipient: "pm", FeishuRecipient: "ou_pm", FeishuIssuer: "bot"})
	if err != nil {
		t.Fatal(err)
	}
	ok, err := s.StartR9PMDeliveryAttempt(ctx, "unknown:feishu:initial")
	if err != nil || !ok {
		t.Fatalf("start attempt ok=%v err=%v", ok, err)
	}
	again, err := s.StartR9PMDeliveryAttempt(ctx, "unknown:feishu:initial")
	if err != nil || again {
		t.Fatalf("unknown receipt was retried ok=%v err=%v", again, err)
	}
	var state string
	var message string
	if err = s.db.QueryRowContext(ctx, `SELECT state,provider_message_id FROM r9_pm_delivery WHERE operation_id='unknown:feishu:initial'`).Scan(&state, &message); err != nil {
		t.Fatal(err)
	}
	if state != "unknown" || message != "" {
		t.Fatalf("receipt falsely resolved state=%s message=%s", state, message)
	}
}
