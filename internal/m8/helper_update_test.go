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
	catalog := HelperUpdateCatalog{SHA256: valid, LockSHA256: valid, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
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
	catalog.URL = "https://files.example/helper.tgz"
	catalog.FileServiceOrigin = "https://other.example"
	if err := validateHelperUpdateCatalog(catalog); err == nil {
		t.Fatal("catalog URL from unregistered file-service origin accepted")
	}
}

func TestHelperUpdateCatalogAndJobAreDurableAndSingleUse(t *testing.T) {
	h, _, ctx := newTestHub(t)
	digest := strings.Repeat("c", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	if err := h.RegisterHelperUpdateCatalog(ctx, "ops", catalog); err == nil {
		t.Fatal("catalog registration replay accepted")
	}
	got, err := h.HelperUpdateCatalog(ctx, digest)
	if err != nil || got == nil || got.Version != catalog.Version || got.FileServiceOrigin != catalog.FileServiceOrigin {
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

func helperUpdateHealthMeta(job *helperUpdateJob) map[string]any {
	return map[string]any{
		"type": "helper_update_health", "job_id": job.JobID, "release_id": job.ReleaseID,
		"agent_id": job.AgentID, "owner_id": job.OwnerID, "version": job.Version,
		"sha256": job.SHA256, "lock_sha256": job.LockSHA256, "health_nonce": randomHex(16),
	}
}

func TestIssueHelperUpdateSendsTrustedTargetBoundOffer(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("d", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, body := e.dial(e.cred, "version=0.2.3.0")
	if conn == nil {
		t.Fatalf("dial: %s", body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-update", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
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
	if got, want := metaString(offer.Meta, "file_service_origin"), "https://files.example"; got != want {
		t.Fatalf("offer origin=%q want %q", got, want)
	}
}

func TestIssueHelperUpdateDefersUntilHelperUpdateCapability(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("6", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.0")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	if _, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest); err == nil || !strings.Contains(err.Error(), "manual_required") {
		t.Fatalf("legacy helper issue err=%v, want manual_required", err)
	}
	if e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]) {
		t.Fatal("legacy helper unexpectedly has update capability")
	}
	stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored != nil {
		t.Fatalf("legacy helper created durable job: %+v err=%v", stored, err)
	}
	e.sendEnv(conn, model.Envelope{ID: "caps-upgrade", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability after bootstrap", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if got := e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }); got == nil {
		t.Fatal("capable helper did not receive deferred update offer")
	}
}

func TestOfflineHelperUpdateReplayWaitsForAuthenticatedCapability(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("8", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	// The Agent is offline at issuance, so the job is durable but unarmed on
	// any particular connection.
	job, err := e.hub.IssueHelperUpdate(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if job.OfferConnSeq != 0 {
		t.Fatalf("offline issue recorded offer connection: %+v", job)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.2.1")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	// sendHelperUpdateOffer records OfferConnSeq before writing. Its remaining
	// zero therefore proves that legacy registration did not replay an offer,
	// without cancelling a WebSocket read just to assert silence.
	stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.OfferConnSeq != 0 {
		t.Fatalf("legacy reconnect replayed update offer: %+v err=%v", stored, err)
	}
	e.sendEnv(conn, model.Envelope{ID: "caps-offline-replay", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability after offline replay", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	if got := e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool {
		return metaString(env.Meta, "job_id") == job.JobID && metaString(env.Meta, "action") == "update_self"
	}); got == nil {
		t.Fatal("capable reconnect did not receive deferred update offer")
	}
	// An offline-issued offer can arrive on this first capable connection, but
	// it is still only offered: health on it must not stand in for installation
	// and a restarted endpoint.
	e.sendEnv(conn, model.Envelope{ID: "offline-offer-health-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: helperUpdateHealthMeta(job)})
	if got := e.readEnvUntil(conn, 100*time.Millisecond, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" }); got != nil {
		t.Fatalf("offline first offer connection confirmed: %+v", got)
	}
	stored, _, err = e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.OfferConnSeq <= 0 || stored.Stage != "offered" {
		t.Fatalf("capable replay changed offline job: %+v err=%v", stored, err)
	}
}

func TestHelperUpdateHealthRequiresFreshAuthenticatedConnection(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("e", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, body := e.dial(e.cred, "version=0.2.3.1")
	if conn == nil {
		t.Fatalf("dial: %s", body)
	}
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-health", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
		t.Fatal("missing update offer")
	}
	// An offer alone cannot confirm itself, including when it was delivered on
	// the Agent's first connection after an offline issue.  Installation is the
	// durable boundary, not offer delivery.
	e.sendEnv(conn, model.Envelope{ID: "health-same-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: helperUpdateHealthMeta(job)})
	if got := e.readEnvUntil(conn, 100*time.Millisecond, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" }); got != nil {
		t.Fatalf("same offer connection confirmed: %+v", got)
	}
	stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.Stage != "offered" {
		t.Fatalf("same connection changed job: %+v err=%v", stored, err)
	}
	// readEnvUntil closes its socket on its deliberate timeout above. The helper
	// reconnects before reporting installation, but this remains an
	// installation-boundary connection rather than a post-install connection.
	conn.CloseNow()
	conn, _, body = e.dial(e.cred, "version=0.2.3.1")
	if conn == nil {
		t.Fatalf("installation redial: %s", body)
	}
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-health-installed", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	// Record the installed boundary on this connection. It too cannot confirm:
	// a later authenticated connection is required to prove restart.
	e.sendEnv(conn, model.Envelope{ID: "installed-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{
		"type": "provision_ack", "action": "update_self", "stage": "installed", "job_id": job.JobID,
		"release_id": job.ReleaseID, "sha256": job.SHA256, "lock_sha256": job.LockSHA256,
	}})
	waitFor(t, time.Second, "durable installed receipt", func() bool {
		stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
		return err == nil && stored != nil && stored.Stage == "installed" && stored.ActivationConnSeq > 0
	})
	e.sendEnv(conn, model.Envelope{ID: "health-installed-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: helperUpdateHealthMeta(job)})
	if got := e.readEnvUntil(conn, 100*time.Millisecond, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" }); got != nil {
		t.Fatalf("installation connection confirmed itself: %+v", got)
	}
	conn.CloseNow()

	conn, _, body = e.dial(e.cred, "version=0.2.3.1")
	if conn == nil {
		t.Fatalf("redial: %s", body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-health-reconnected", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability after reconnect", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	freshHealth := helperUpdateHealthMeta(job)
	e.sendEnv(conn, model.Envelope{ID: "health-fresh-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: freshHealth})
	confirm := e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" })
	if confirm == nil || metaString(confirm.Meta, "health_nonce") != metaString(freshHealth, "health_nonce") {
		t.Fatalf("missing exact confirmation: %+v", confirm)
	}
	stored, _, err = e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.Stage != "confirmed" || stored.Revision != 4 {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
	// Confirmation delivery may have been lost. A new health nonce after a
	// later reconnect re-echoes confirmation but must not mutate the job.
	conn.CloseNow()
	conn, _, body = e.dial(e.cred, "version=0.2.3.1")
	if conn == nil {
		t.Fatalf("idempotent redial: %s", body)
	}
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-health-idempotent", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	freshRetry := helperUpdateHealthMeta(job)
	e.sendEnv(conn, model.Envelope{ID: "health-retry-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: freshRetry})
	confirm = e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" })
	if confirm == nil || metaString(confirm.Meta, "health_nonce") != metaString(freshRetry, "health_nonce") {
		t.Fatalf("missing idempotent confirmation: %+v", confirm)
	}
	stored, _, err = e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil || stored.Stage != "confirmed" || stored.Revision != 4 {
		t.Fatalf("idempotent confirmation mutated job: %+v err=%v", stored, err)
	}
}

func TestHelperUpdateHealthRejectsMissingFreshNonce(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("f", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.1")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-nonce", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
		t.Fatal("missing update offer")
	}
	stored, raw, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored == nil {
		t.Fatalf("job=%+v err=%v", stored, err)
	}
	stored.Stage, stored.ActivationConnSeq = "installed", job.OfferConnSeq
	if ok, err := e.hub.updateHelperUpdateJob(e.ctx, raw, *stored); err != nil || !ok {
		t.Fatalf("installed setup ok=%v err=%v", ok, err)
	}
	job, _, err = e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || job == nil {
		t.Fatalf("stored installed job=%+v err=%v", job, err)
	}
	// The direct call isolates nonce validation from the separately covered
	// real-WebSocket reconnect boundary. It deliberately does not read the
	// live session map while the WebSocket server may be tearing it down.
	c := &sessionClient{metricsAgentID: e.agID, keyID: AgentKeyID("zzc"), sessionName: "opus50ts", version: job.Version, connSeq: job.ActivationConnSeq + 1, helperUpdateV1: true}
	for name, mutate := range map[string]func(map[string]any){
		"missing nonce": func(m map[string]any) { m["health_nonce"] = "" },
		"wrong lock":    func(m map[string]any) { m["lock_sha256"] = strings.Repeat("0", 64) },
		"wrong tuple":   func(m map[string]any) { m["sha256"] = strings.Repeat("1", 64) },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := helperUpdateHealthMeta(job)
			mutate(candidate)
			e.hub.confirmHelperUpdateHealth(e.ctx, c, model.Envelope{Meta: candidate})
			got, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
			if err != nil || got == nil || got.Stage != "installed" || got.Revision != job.Revision {
				t.Fatalf("invalid health changed job: %+v err=%v", got, err)
			}
		})
	}
}

func TestHelperUpdateReceiptUsesAuthenticatedJobTuple(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("2", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.0")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-receipt", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
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
		return err == nil && stored != nil && stored.Stage == "downloaded" && stored.Revision == 3
	})
}

func TestHelperUpdateReceiptNeverRegressesStage(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("4", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.0")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-stage", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	job, err := e.hub.IssueHelperUpdateForActor(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
		t.Fatal("missing update offer")
	}
	for _, stage := range []string{"restarted", "downloaded"} {
		e.sendEnv(conn, model.Envelope{ID: "receipt-" + stage, From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{
			"type": "provision_ack", "action": "update_self", "stage": stage, "job_id": job.JobID,
			"release_id": job.ReleaseID, "sha256": job.SHA256, "lock_sha256": job.LockSHA256,
		}})
	}
	waitFor(t, time.Second, "restart receipt", func() bool {
		stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
		return err == nil && stored != nil && stored.Stage == "restarted"
	})
	stored, _, err := e.hub.helperUpdateJob(e.ctx, e.agID)
	if err != nil || stored.Revision != 3 {
		t.Fatalf("stale receipt changed revision: %+v err=%v", stored, err)
	}
}

func TestHelperUpdateHealthNeverResurrectsTerminalJob(t *testing.T) {
	for _, terminal := range []string{"failed", "rolled_back"} {
		t.Run(terminal, func(t *testing.T) {
			e := newAgentEnv(t)
			digest := strings.Repeat("7", 64)
			catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
			if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", catalog); err != nil {
				t.Fatal(err)
			}
			conn, _, _ := e.dial(e.cred, "version=0.2.3.1")
			waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
			e.sendEnv(conn, model.Envelope{ID: "caps-terminal", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
			waitFor(t, time.Second, "helper update capability", func() bool {
				return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
			})
			job, err := e.hub.IssueHelperUpdate(e.ctx, "zzc", e.agID, digest)
			if err != nil {
				t.Fatal(err)
			}
			if e.readEnvUntil(conn, time.Second, func(env model.Envelope) bool { return metaString(env.Meta, "job_id") == job.JobID }) == nil {
				t.Fatal("missing update offer")
			}
			stored, raw, err := e.hub.helperUpdateJob(e.ctx, e.agID)
			if err != nil || stored == nil {
				t.Fatalf("job=%+v err=%v", stored, err)
			}
			stored.Stage, stored.Reason = terminal, "sha_mismatch"
			if ok, err := e.hub.updateHelperUpdateJob(e.ctx, raw, *stored); err != nil || !ok {
				t.Fatalf("terminal setup ok=%v err=%v", ok, err)
			}
			conn.CloseNow()
			conn, _, _ = e.dial(e.cred, "version=0.2.3.1")
			defer conn.CloseNow()
			waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
			e.sendEnv(conn, model.Envelope{ID: "caps-terminal-reconnect", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
			waitFor(t, time.Second, "helper update capability after reconnect", func() bool {
				return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
			})
			e.sendEnv(conn, model.Envelope{ID: "health-terminal-" + randomHex(4), From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: helperUpdateHealthMeta(job)})
			if got := e.readEnvUntil(conn, 100*time.Millisecond, func(env model.Envelope) bool { return metaString(env.Meta, "type") == "helper_update_confirm" }); got != nil {
				t.Fatalf("terminal job resurrected: %+v", got)
			}
			stored, _, err = e.hub.helperUpdateJob(e.ctx, e.agID)
			if err != nil || stored == nil || stored.Stage != terminal {
				t.Fatalf("terminal job changed: %+v err=%v", stored, err)
			}
		})
	}
}

func TestHelperUpdateTerminalStagesIncludeRollback(t *testing.T) {
	for _, stage := range []string{"confirmed", "failed", "rolled_back"} {
		if !helperUpdateTerminal(stage) {
			t.Fatalf("terminal stage %q was accepted for resurrection", stage)
		}
	}
	if helperUpdateTerminal("restarted") {
		t.Fatal("nonterminal restarted stage treated as terminal")
	}
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
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.hub.IssueHelperUpdate(e.ctx, "zzc", agents[0].ID, digest); err == nil {
		t.Fatal("cross-owner helper update accepted")
	}
}

func TestGetHelperUpdateJobRechecksPrincipalOwnership(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("f", 64)
	if err := e.hub.RegisterHelperUpdateCatalog(e.ctx, "ops", HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}); err != nil {
		t.Fatal(err)
	}
	conn, _, _ := e.dial(e.cred, "version=0.2.3.1")
	defer conn.CloseNow()
	waitSessionOnline(t, e.hub, AgentKeyID("zzc"), "opus50ts")
	e.sendEnv(conn, model.Envelope{ID: "caps-get-job", From: "opus50ts#enrolled", To: "workpulse#enrolled", Meta: map[string]any{"type": "session_capabilities", "helper_update_v1": true}})
	waitFor(t, time.Second, "helper update capability", func() bool {
		return e.hub.sessionSupportsHelperUpdate(e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"])
	})
	job, err := e.hub.IssueHelperUpdate(e.ctx, "zzc", e.agID, digest)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := e.hub.GetHelperUpdateJob(e.ctx, "zzc", e.agID); err != nil || got == nil || got.JobID != job.JobID {
		t.Fatalf("owned job=%+v err=%v", got, err)
	}
	if err := e.svc.Store.CreateUser(e.ctx, identity.User{UserID: "other", ShortName: "other", State: identity.UserActive}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.hub.GetHelperUpdateJob(e.ctx, "other", e.agID); err == nil || got != nil {
		t.Fatalf("cross-owner job read got=%+v err=%v", got, err)
	}
}

func TestRecoverHelperUpdatesTerminalizesExpiredJob(t *testing.T) {
	e := newAgentEnv(t)
	digest := strings.Repeat("1", 64)
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
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
	catalog := HelperUpdateCatalog{SHA256: digest, LockSHA256: digest, Version: "0.2.3.1", URL: "https://files.example/helper.tgz", FileServiceOrigin: "https://files.example", Platform: "linux"}
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
