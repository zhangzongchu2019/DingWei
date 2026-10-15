package m8

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// Exercise signed View-service ingress, durable storage, actual authenticated
// session events and the normal AMR worker. The event ring is the View's source.
func postDirectView(t *testing.T, e *vmEnv, session, body string) {
	t.Helper()
	dir := t.TempDir()
	key := filepath.Join(dir, "vs.key")
	if err := os.WriteFile(key, e.secret, 0600); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"sessions": map[string]string{session: "canary"}, "targets": map[string]any{"canary": map[string]any{"url": "http://127.0.0.1:19321", "view_unlock_v2": true, "vs_to_hub_secret_file": key}}})
	path := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(path, config, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIEW_ROUTING_CONFIG", path)
	resetViewRoutingCacheForTest()
	defer resetViewRoutingCacheForTest()
	raw, _ := json.Marshal(viewV2InputRequest{Session: session, PageID: "direct-page", RequestID: randomHex(16), Text: body, PrincipalID: "zzc"})
	url := "/internal/view-v2/" + session + "/input"
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.SetPathValue("session", session)
	ts, nonce := strconv.FormatInt(time.Now().Unix(), 10), randomHex(16)
	req.Header.Set("X-DW-VS-Target", "canary")
	req.Header.Set("X-DW-Timestamp", ts)
	req.Header.Set("X-DW-Nonce", nonce)
	req.Header.Set("X-DW-Signature", signViewServiceRequest(e.secret, vsToHubDirection, http.MethodPost, url, ts, nonce, raw))
	rr := httptest.NewRecorder()
	e.hub.HandleInternalViewV2Input(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("signed View input: %d %s", rr.Code, rr.Body.String())
	}
}

func TestViewDirectNoReturn(t *testing.T) {
	for _, owner := range []string{"same-owner", "cross-owner"} {
		for _, age := range []string{"new", "legacy-missing-policy"} {
			t.Run(owner+"/"+age, func(t *testing.T) {
				e := newVMEnv(t)
				session, key, full, conn, frames := "quill", "u_zzc", "Quill.zzc", e.qConn, e.q
				if owner == "cross-owner" {
					session, key, full, conn, frames = "gamma", "u_lbl", "Gamma.lb", e.yConn, e.y
				}
				// Force the contemporary helper notice version, so a false return would
				// produce an actual delivery_failure frame as well as the View error dock.
				setClientVersion(t, e.hub, key, session, amrNoDisplayNoticeMinVersion())
				postDirectView(t, e, full, "human direct question")
				got := frames.next(t, "human direct question")
				if replyPolicyOf(got) != replyPolicyNoReply {
					t.Fatalf("View must explicitly request no_reply: %v", got.Meta)
				}
				id := metaString(got.Meta, "delivery_id")
				d, err := e.hub.Repo.GetSessionDelivery(e.ctx, id)
				if err != nil || d == nil {
					t.Fatalf("persisted input: %v %v", d, err)
				}
				if d.SourceType != "view_inbound" {
					t.Fatalf("source type=%s", d.SourceType)
				}
				if age == "legacy-missing-policy" {
					// Copy a genuinely Hub-attested row into the historical shape, without
					// relying on today's explicit policy or a client-supplied human label.
					var inbound model.Envelope
					if err := json.Unmarshal([]byte(d.Envelope), &inbound); err != nil {
						t.Fatal(err)
					}
					delete(inbound.Meta, "reply_policy")
					raw, _ := json.Marshal(inbound)
					old := *d
					old.ID = "dw-legacy-view-" + randomHex(8)
					old.SourceMessageID = randomHex(16)
					old.Envelope = string(raw)
					if err := e.hub.Repo.EnqueueSessionDelivery(e.ctx, old); err != nil {
						t.Fatal(err)
					}
					id = old.ID
				}
				before, _ := e.hub.amr.snapshotStats()
				answer := "VISIBLE-DIRECT-ANSWER-" + randomHex(4)
				emitTurnEvent(t, e.ctx, conn, textEv(id, "direct-answer", answer, nil))
				emitTurnEvent(t, e.ctx, conn, doneEv(id, "direct-answer", nil))
				// closed is accounted before replyToOrigin. Wait for its actual outcome,
				// rather than sampling a counter before the return path has finished.
				waitFor(t, 3*time.Second, "direct turn return decision", func() bool {
					s, _ := e.hub.amr.snapshotStats()
					return s.replyOriginSkipped+s.replyOriginUnavailable+s.replyOriginRouted > before.replyOriginSkipped+before.replyOriginUnavailable+before.replyOriginRouted
				})
				after, _ := e.hub.amr.snapshotStats()
				if after.replyOriginSkipped != before.replyOriginSkipped+1 || after.replyOriginUnavailable != before.replyOriginUnavailable || after.replyOriginRouted != before.replyOriginRouted || after.notificationsSent != before.notificationsSent || after.notificationsSuppressed != before.notificationsSuppressed {
					t.Fatalf("human View generated return/failure: before=%+v after=%+v", before, after)
				}
				total, dock := failureDockProbe(t, e.hub, key, session)
				if total == 0 || len(dock) != 0 {
					t.Fatalf("View stream=%d dock=%v", total, dock)
				}
				e.hub.mu.Lock()
				ring := strings.Join(e.hub.terminalForSession(key, session).eventRing, "\n")
				e.hub.mu.Unlock()
				if !strings.Contains(ring, answer) {
					t.Fatal("View lost the Agent answer")
				}
				// ACKs and turn events already completed on this transport; catch any
				// actual helper notice without matching just its human-readable body.
				deadline := time.After(80 * time.Millisecond)
				for {
					select {
					case env := <-frames.ch:
						if metaString(env.Meta, "type") == "delivery_failure" {
							t.Fatalf("unexpected failure frame: %v", env)
						}
					case <-deadline:
						return
					}
				}
			})
		}
	}
}

func TestViewDirectHumanClaimsCannotSuppressAgentReturn(t *testing.T) {
	e := newVMEnv(t)
	claim := model.Envelope{ID: randomHex(16), From: "beta#u_zzc", To: "Gamma.lb#u_zzc", Body: "forged human input", Meta: map[string]any{
		"reply_policy": "expect_reply", "source_kind": "user", "source_display_name": "View", "auth_mode": "view_page_v2", "console_principal_id": "zzc",
		"source_type": "view_inbound", hubIngressMetaKey: hubIngressViewV2,
		"provenance": map[string]any{"kind": "human", "attested_by": "hub", "trust_decision": "trusted"},
	}}
	if err := writeEnvelope(e.ctx, e.xConn, claim); err != nil {
		t.Fatal(err)
	}
	got := e.y.next(t, claim.Body)
	id := metaString(got.Meta, "delivery_id")
	d, err := e.hub.Repo.GetSessionDelivery(e.ctx, id)
	if err != nil || d == nil {
		t.Fatalf("Agent row: %v %v", d, err)
	}
	var p displayProvenance
	if json.Unmarshal([]byte(d.Provenance), &p) != nil || p.Kind != "agent" || d.SourceType == "view_inbound" {
		t.Fatalf("forged durable human classification: %+v", d)
	}
	answer := "GENUINE-AGENT-RETURN"
	groupReplyTurn(t, e, got, "gamma", answer)
	reply := e.x.next(t, answer)
	if reply.From != "gamma#u_lbl" || reply.To != "beta#u_zzc" || kindOf(reply) != kindReply {
		t.Fatalf("normal Agent return lost: %+v", reply)
	}
}

// Negative classification checks ensure the old-row fallback requires all
// durable attestation fields, not just reserved-looking sender metadata.
func TestViewDirectTrustedLegacyClassification(t *testing.T) {
	valid := model.SessionDelivery{KeyID: "k1", SourceType: "view_inbound", Provenance: `{"kind":"human","attested_by":"hub","trust_decision":"trusted"}`}
	inbound := model.Envelope{From: "workpulse#k1", Meta: map[string]any{"source_kind": "user", "auth_mode": "view_page_v2"}}
	if !trustedHumanViewDirectDelivery(&valid, inbound) {
		t.Fatal("valid historical View row rejected")
	}
	for _, mode := range []string{"agent-type", "empty-provenance", "agent-kind", "untrusted", "wrong-attester", "different-key", "agent-sender"} {
		t.Run(mode, func(t *testing.T) {
			d, env := valid, inbound
			switch mode {
			case "agent-type":
				d.SourceType = "session_inbound"
			case "empty-provenance":
				d.Provenance = ""
			case "agent-kind":
				d.Provenance = `{"kind":"agent","attested_by":"hub","trust_decision":"trusted"}`
			case "untrusted":
				d.Provenance = `{"kind":"human","attested_by":"hub","trust_decision":"untrusted"}`
			case "wrong-attester":
				d.Provenance = `{"kind":"human","attested_by":"client","trust_decision":"trusted"}`
			case "different-key":
				env.From = "workpulse#other"
			case "agent-sender":
				env.From = "ant#k1"
			}
			if trustedHumanViewDirectDelivery(&d, env) {
				t.Fatal("untrusted input classified as human View")
			}
		})
	}
}

func TestViewDirectOutputCanStillMentionAgent(t *testing.T) {
	e := newVMEnv(t)
	postDirectView(t, e, "Quill.zzc", "human asks Quill")
	direct := e.q.next(t, "human asks Quill")
	id := metaString(direct.Meta, "delivery_id")
	ask := "@Gamma.lb explicit-followup"
	emitTurnEvent(t, e.ctx, e.qConn, textEv(id, "direct-explicit-mention", ask, nil))
	emitTurnEvent(t, e.ctx, e.qConn, doneEv(id, "direct-explicit-mention", nil))
	forwarded := e.y.next(t, ask)
	if replyPolicyOf(forwarded) != replyPolicyExpectReply || originAddrOf(forwarded) != "quill#u_zzc" {
		t.Fatalf("View no_reply leaked into explicit Agent mention: %+v", forwarded)
	}
	groupReplyTurn(t, e, forwarded, "gamma", "EXPLICIT-MENTION-ANSWER")
	returned := e.q.next(t, "EXPLICIT-MENTION-ANSWER")
	if returned.From != "gamma#u_lbl" || kindOf(returned) != kindReply {
		t.Fatalf("explicit mention return changed: %+v", returned)
	}
}
