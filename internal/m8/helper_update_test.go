package m8

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestHelperUpdateCatalogValidation(t *testing.T) {
	valid := strings.Repeat("a", 64)
	catalog := HelperUpdateCatalog{SHA256: valid, LockSHA256: valid, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := validateHelperUpdateCatalog(catalog); err != nil {
		t.Fatalf("valid catalog: %v", err)
	}
	catalog.SHA256 = "not-a-digest"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("invalid artifact digest accepted")
	}
	catalog.SHA256 = valid
	catalog.Platform = "darwin"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("non-linux catalog accepted")
	}
	catalog.Platform = "linux"
	catalog.URL = "https://files.example/helper.tgz?credential=bad"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("catalog URL query accepted")
	}
	catalog.URL = "http://files.example/helper.tgz"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("non-TLS catalog URL accepted")
	}
}

func TestHelperUpdateCatalogAndJobAreDurableAndSingleUse(t *testing.T) {
	h, _, ctx := newTestHub(t)
	digest := strings.Repeat("c", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err == nil {
		t.Fatal("catalog registration replay accepted")
	}
	got, err := h.HelperUpdateCatalog(ctx, digest)
	if err != nil || got == nil || got.Version != catalog.Version {
		t.Fatalf("catalog=%+v err=%v", got, err)
	}
	job, err := h.createHelperUpdateJob(context.Background(), *got, "agent-1", "owner", "agent", "owner")
	if err != nil {
		t.Fatal(err)
	}
	if job.Stage != "offered" || job.Revision != 1 || job.ExpiresAt.IsZero() {
		t.Fatalf("job=%+v", job)
	}
	if _, err := h.createHelperUpdateJob(ctx, *got, "agent-1", "owner", "agent", "owner"); err == nil {
		t.Fatal("second active job accepted")
	}
	stored, _, err := h.helperUpdateJob(ctx, "agent-1")
	if err != nil || stored == nil || stored.JobID != job.JobID {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	stored.Stage = "failed"
	if ok, err := h.updateHelperUpdateJob(ctx, rawForJob(t, h, ctx, "agent-1"), *stored); err != nil || !ok {
		t.Fatalf("terminal setup ok=%v err=%v", ok, err)
	}
	if next, err := h.createHelperUpdateJob(ctx, *got, "agent-1", "owner", "agent", "owner"); err != nil || next.JobID == job.JobID {
		t.Fatalf("authorized retry=%+v err=%v", next, err)
	}
}

func rawForJob(t *testing.T, h *Hub, ctx context.Context, agentID string) string {
	t.Helper()
	_, raw, err := h.helperUpdateJob(ctx, agentID)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestIssueHelperUpdateSendsTrustedTargetBoundOffer(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("d", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, body := e.dial(e.cred, "version=0.2.3.0")
	if conn == nil {
		t.Fatalf("dial: %s", body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	offer := e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool {
		return metaString(env.Meta, "action") == "update_self"
	})
	if offer == nil {
		t.Fatal("missing update offer")
	}
	if got, want := metaString(offer.Meta, "job_id"), job.JobID; got != want {
		t.Fatalf("job id=%q want %q", got, want)
	}
	if got, want := metaString(offer.Meta, "owner_id"), "zzc"; got != want {
		t.Fatalf("owner id=%q want %q", got, want)
	}
	if got, want := offer.From, "workpulse#u_zzc"; got != want {
		t.Fatalf("offer source=%q want %q", got, want)
	}
}

func TestHelperUpdateHealthRequiresFreshAuthenticatedTuple(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("e", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, body := e.dial(e.cred, "version=0.2.3.1")
	if conn == nil {
		t.Fatalf("dial: %s", body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
		t.Fatal("missing update offer")
	}
	e.sendEnv(conn, model.Envelope{ID: "health-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{
		"type": "helper_update_health", "job_id": job.JobID, "release_id": job.ReleaseID, "sha256": job.SHA256,
		"lock_sha256": job.LockSHA256, "health_nonce": job.HealthNonce,
	}})
	confirm := e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" })
	if confirm == nil || metaString(confirm.Meta, "health_nonce") != job.HealthNonce {
		t.Fatalf("missing exact confirmation: %+v", confirm)
	}
	stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.Stage != "confirmed" || stored.Revision != 2 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func TestHelperUpdateHealthRejectsWrongNonce(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("f", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.1")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
		t.Fatal("missing update offer")
	}
	e.sendEnv(conn, model.Envelope{ID: "health-bad-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{
		"type": "helper_update_health", "job_id": job.JobID, "release_id": job.ReleaseID, "sha256": job.SHA256,
		"lock_sha256": job.LockSHA256, "health_nonce": "wrong",
	}})
	stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.Stage != "offered" || stored.Revision != 1 {
		t.Fatalf("wrong nonce changed job: %+v err=%v", stored, err)
	}
}

func TestHelperUpdateReceiptUsesAuthenticatedJobTuple(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("2", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.0")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
		t.Fatal("missing update offer")
	}
	e.sendEnv(conn, model.Envelope{ID: "receipt-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{
		"type": "provision_ack", "action": "update_self", "stage": "downloaded", "job_id": job.JobID,
		"release_id": job.ReleaseID, "sha256": job.SHA256, "lock_sha256": job.LockSHA256,
	}})
	waitFor(t, time.Second, "durable download receipt", func() bool {
		stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
		return err == nil && stored != nil && stored.Stage == "downloaded" && stored.Revision == 2
	})
}

func TestIssueHelperUpdateForActorRejectsCrossOwner(t *testing.T) {
	e := newAgentEnv(t)
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "other", ShortName: "other", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	e.enrollForOwner("other", "other-device", "OtherAgent")
	agents, err := e.svc.Store.ListAgents(e.ctx, "other")
	if err != nil || len(agents) != 1 {
		t.Fatalf("agents=%+v err=%v", agents, err)
	}
	digest := strings.Repeat("3", 64)
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", agents[0].ID, digest); err == nil {
		t.Fatal("cross-owner helper update accepted")
	}
}

func TestRecoverHelperUpdatesTerminalizesExpiredJob(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("1", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	job, err := e.hub.createHelperUpdateJob(e.ctx, catalog, e.agID, "zzc", "opus50ts", "zzc")
	if err != nil {
		t.Fatal(err)
	}
	stored, raw, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil {
		t.Fatalf("job=%+v err=%v", stored, err)
	}
	stored.ExpiresAt = time.Now().UTC().Add(-time.Minute)
	if ok, err := e.hub.updateHelperUpdateJob(e.ctx, raw, *stored); err != nil || !ok {
		t.Fatalf("expire setup ok=%v err=%v", ok, err)
	}
	if err := e.hub.RecoverHelperUpdates(e.ctx); err != nil {
		t.Fatal(err)
	}
	got, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || got == nil || got.Stage != "failed" || got.Reason != "offer_expired" || got.JobID != job.JobID {
		t.Fatalf("recovered=%+v err=%v", got, err)
	}
}

func TestHelperUpdateKeysAreNamespaced(t *testing.T) {
	sha := strings.Repeat("b", 64)
	if got, want := HelperUpdateCatalogKey(strings.ToUpper(sha)), HelperUpdateCatalogPrefix+sha; got != want {
		t.Fatalf("catalog key=%q want %q", got, want)
	}
	if got, want := helperUpdateJobKey("agent-1"), "helper_update.job.agent-1"; got != want {
		t.Fatalf("job key=%q want %q", got, want)
	}
}

func TestListHelperUpdateCatalogsAndOwnerJobsFilter(t *testing.T) {
	h, _, ctx := newTestHub(t)
	digest := strings.Repeat("9", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", Platform: "linux"}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	items, err := h.ListHelperUpdateCatalogs(ctx)
	if err != nil || len(items) != 1 || items[0].SHA256 != digest {
		t.Fatalf("catalogs=%+v err=%v", items, err)
	}
	if _, err := h.createHelperUpdateJob(ctx, catalog, "agent-1", "owner-a", "a", "owner-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.createHelperUpdateJob(ctx, catalog, "agent-2", "owner-b", "b", "owner-b"); err != nil {
		t.Fatal(err)
	}
	jobs, err := h.ListHelperUpdateJobsForOwner(ctx, "owner-a")
	if err != nil || len(jobs) != 1 || jobs[0].AgentID != "agent-1" {
		t.Fatalf("jobs=%+v err=%v", jobs, err)
	}
}
