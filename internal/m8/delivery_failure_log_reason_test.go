package m8

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type publicationLogCapture struct {
	sync.Mutex
	bytes.Buffer
}

func (b *publicationLogCapture) Write(p []byte) (int, error) {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.Write(p)
}
func (b *publicationLogCapture) snapshot() string {
	b.Lock()
	defer b.Unlock()
	return b.Buffer.String()
}

func TestPublicationDiagnosticPreservesPublicBytes(t *testing.T) {
	h, _, _ := newTestHub(t)
	key, name := "u_synthetic", "pending"
	h.beginCapabilityPendingAdmission(key, name)
	target := sessionAddress(name, key)
	if got := h.deliveryFailureLogReason(errGroupAgentNotFound, target); got != "target_not_published" {
		t.Fatalf("unpublished target diagnostic=%s", got)
	}
	cross := newGroupingRejection("cross_group_denied")
	if got := h.deliveryFailureLogReason(cross, target); got != "cross_group_denied" {
		t.Fatal(got)
	}
	// Only the random envelope ID is normalized; all public body, metadata,
	// address, source ID and timestamp bytes must match the legacy error.
	now := time.Unix(1793120000, 0).UTC()
	for _, version := range []string{"", "0.1.2.0", "0.2.3.14"} {
		c := &sessionClient{keyID: key, sessionName: "source", version: version}
		encode := func(err error) []byte {
			env := deliveryFailureEnvelopeWithOriginal(c, "original-id", target, err.Error(), "unchanged draft", now)
			env.ID = "normalized-random-envelope-id"
			if errors.Is(err, errGroupAgentNotFound) {
				env.Body = strings.Replace(env.Body, err.Error(), deliveryFailureNoticeReason(err), 1)
			}
			raw, err := json.Marshal(env)
			if err != nil {
				t.Fatal(err)
			}
			return raw
		}
		baseline := encode(errGroupAgentNotFound)
		if got := encode(cross); !bytes.Equal(got, baseline) {
			t.Fatalf("public bytes changed for version=%s:\n%s\n%s", version, got, baseline)
		}
		if strings.Contains(string(baseline), "target_not_published") {
			t.Fatal("internal reason leaked")
		}
	}
	// No current admission marker => no assertion that a publication race occurred.
	if got := h.deliveryFailureLogReason(errGroupAgentNotFound, "missing#u_synthetic"); got != "target_offline_or_unresolved" {
		t.Fatal(got)
	}
	h.sessionClients[key] = map[string]*sessionClient{name: {}}
	if got := h.deliveryFailureLogReason(errGroupAgentNotFound, target); got != "target_offline_or_unresolved" {
		t.Fatalf("stale marker misreported a published target: %s", got)
	}
	if got := h.deliveryFailureLogReason(newGroupingRejection("sender_not_active_member"), target); got != "sender_not_active_member" {
		t.Fatal(got)
	}
}

func TestPublicationDiagnosticAuthenticatedRoutingAndWireNotice(t *testing.T) {
	e := newAgentEnv(t)
	ctx, cancel := context.WithTimeout(e.ctx, 10*time.Second)
	defer cancel()
	targetCred, targetID := e.enroll("publication-target", "PublicationTarget")
	_, outsiderID := e.enroll("publication-outsider", "PublicationOutsider")
	group, err := e.svc.CreateGroup(ctx, "zzc", e.agID, "publication")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.svc.JoinGroup(ctx, "zzc", group.Code, targetID); err != nil {
		t.Fatal(err)
	}
	held, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	e.hub.sessionBeforePublishHook = func(key, name string) {
		if name == "publicationtarget" {
			close(held)
			<-release
		}
	}
	source, _, _ := e.dial(e.cred, "version=0.2.3.14")
	defer source.CloseNow()
	waitFor(t, 10*time.Second, "source published", func() bool { return e.hub.sessionOnline(AgentKeyID("zzc"), "opus50ts") })
	target, _, _ := e.dial(targetCred, "")
	defer target.CloseNow()
	select {
	case <-held:
	case <-ctx.Done():
		t.Fatal("target gate not reached")
	}
	key := AgentKeyID("zzc")
	e.hub.mu.Lock()
	sourceClient := e.hub.sessionClients[key]["opus50ts"]
	// A real active identity in its own separate group. Publication lets the
	// actual ACL branch decide, independent of endpoint-resolution failure.
	e.hub.sessionClients[key]["publicationoutsider"] = &sessionClient{keyID: key, sessionName: "publicationoutsider", metricsAgentID: outsiderID}
	e.hub.mu.Unlock()
	var captured publicationLogCapture
	previous := log.Writer()
	log.SetOutput(&captured)
	defer log.SetOutput(previous)
	for _, tc := range []struct{ name, reason string }{
		{"publicationtarget", "target_not_published"},
		{"publicationoutsider", "cross_group_denied"},
	} {
		env := model.Envelope{ID: "publication-" + tc.name, From: sessionAddress("opus50ts", key), To: sessionAddress(tc.name, key), Body: "must remain rejected"}
		routeErr := e.hub.RouteEnvelope(ctx, env)
		if !errors.Is(routeErr, errGroupAgentNotFound) {
			t.Fatalf("route accepted or changed public error: %v", routeErr)
		}
		if got := e.hub.deliveryFailureLogReason(routeErr, env.To); got != tc.reason {
			t.Fatalf("internal reason=%s want=%s", got, tc.reason)
		}
		if got := deliveryFailureNoticeReason(routeErr); got != groupAgentNotFoundText+"（reason=cross_group_denied）" {
			t.Fatalf("public reason changed: %s", got)
		}
		e.hub.writeDeliveryFailure(ctx, sourceClient, env, env.To, routeErr)
		for {
			notice := readEnvelope(t, ctx, source)
			if metaString(notice.Meta, "type") != model.MetaTypeDeliveryFailure {
				continue
			}
			if metaString(notice.Meta, "source_id") != env.ID || !strings.Contains(notice.Body, "reason=cross_group_denied") || strings.Contains(notice.Body, "target_not_published") || notice.Meta["reason"] != nil {
				t.Fatalf("wire response disclosed internal state: %+v", notice)
			}
			// Compare the actual wire result with the baseline public construction.
			// Only generated ID and unrelated startup-window annotations vary.
			expected := deliveryFailureEnvelopeWithOriginal(sourceClient, env.ID, env.To, errGroupAgentNotFound.Error(), env.Body, time.Unix(notice.TS, 0))
			expected.ID = notice.ID
			expected.Body = strings.Replace(expected.Body, errGroupAgentNotFound.Error(), groupAgentNotFoundText+"（reason=cross_group_denied）", 1)
			for _, field := range []string{"within_restart_window", "hub_uptime_seconds", "restart_window_seconds"} {
				delete(notice.Meta, field)
			}
			actualBytes, marshalErr := json.Marshal(notice)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			expectedBytes, marshalErr := json.Marshal(expected)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			if !bytes.Equal(actualBytes, expectedBytes) {
				t.Fatalf("wire bytes changed: %s != %s", actualBytes, expectedBytes)
			}
			break
		}
		if !strings.Contains(captured.snapshot(), "reason="+tc.reason+" ") {
			t.Fatalf("actual reject logger missed %s: %s", tc.reason, captured.snapshot())
		}
	}
	once.Do(func() { close(release) })
	waitFor(t, 10*time.Second, "target published", func() bool { return e.hub.sessionOnline(key, "publicationtarget") })
	if got := e.hub.deliveryFailureLogReason(errGroupAgentNotFound, sessionAddress("publicationtarget", key)); got != "target_offline_or_unresolved" {
		t.Fatal("published connection mislabeled", got)
	}
}
