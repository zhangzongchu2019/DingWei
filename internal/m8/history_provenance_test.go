package m8

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/store"
)

func TestHistoryProvenanceRealHubAgentTimerReplay(t *testing.T) {
	fixtures := map[string]any{}
	for _, name := range []string{"agent", "timer"} {
		t.Run(name, func(t *testing.T) {
			v := newViewEnv(t)
			history := openEnabledNewPlatformHistory(t, v)
			cred := v.cred
			sourceID := v.agID
			sourceName := "Opus50TS"
			if name == "timer" {
				cred, sourceID = v.enroll("timer-device", "timer")
				sourceName = "timer"
			}
			group, e := v.svc.CreateGroup(v.ctx, "zzc", sourceID, tg())
			if e != nil {
				t.Fatal(e)
			}
			targetAgent, e := v.svc.Store.FindAgentByName(v.ctx, "zzc", "kimi")
			if e != nil {
				t.Fatal(e)
			}
			if e = v.svc.JoinGroup(v.ctx, "zzc", group.Code, targetAgent.ID); e != nil {
				t.Fatal(e)
			}
			source, status, body := v.dial(cred, "")
			if source == nil {
				t.Fatalf("source %d %s", status, body)
			}
			defer source.CloseNow()
			target, status, body := v.dial(v.zKimiCred, "delivery_ack_v1=1")
			if target == nil {
				t.Fatalf("target %d %s", status, body)
			}
			defer target.CloseNow()
			waitSessionOnline(t, v.hub, AgentKeyID("zzc"), "kimi")
			if e := writeEnvelope(v.ctx, target, model.Envelope{ID: "cap", Meta: map[string]any{"type": "session_capabilities", "delivery_ack_v1": true}}); e != nil {
				t.Fatal(e)
			}
			waitFor(t, 3*time.Second, "reliable helper capability", func() bool {
				ok, _, e := v.hub.Repo.SessionEndpointDeliveryACK(v.ctx, AgentKeyID("zzc"), "kimi")
				return e == nil && ok
			})

			message := "hp-message-" + name
			if e := writeEnvelope(v.ctx, source, model.Envelope{ID: message, To: "kimi#" + AgentKeyID("zzc"), From: sourceName + "#enrolled", Body: "synthetic history " + name}); e != nil {
				t.Fatal(e)
			}
			var delivered model.Envelope
			for {
				delivered = readEnvelope(t, v.ctx, target)
				if delivered.ID == message {
					break
				}
			}
			delivery := metaString(delivered.Meta, "delivery_id")
			if delivery == "" {
				t.Fatal("missing canonical delivery")
			}
			// The real helper's admitted event contract: correlation IDs are top-level.
			event := map[string]any{"kind": "user_input", "text": "synthetic history " + name, "delivery_id": delivery, "origin_delivery_id": delivery, "turn_id": "turn-" + name, "data": map[string]any{"lifecycle": "admitted", "source": "agent"}, "provenance": map[string]string{"kind": "human", "attested_by": "client"}}
			raw, _ := json.Marshal(event)
			if e := writeEnvelope(v.ctx, target, model.Envelope{ID: "echo-" + name, Body: string(raw), Meta: map[string]any{"type": terminalSessionEventType}}); e != nil {
				t.Fatal(e)
			}
			waitFor(t, 3*time.Second, "admitted history", func() bool { n, _, e := history.HistoryStats(v.ctx); return e == nil && n == 1 })
			status, response := v.get(http.MethodGet, "/view/Kimi.zzc/history", v.tokZZC, "")
			if status != 200 {
				t.Fatalf("history HTTP %d %s", status, response)
			}
			var page store.SessionHistoryPage
			if e := json.Unmarshal([]byte(response), &page); e != nil || len(page.Events) != 1 {
				t.Fatalf("history page %v %s", e, response)
			}
			var stored map[string]any
			if e := json.Unmarshal(page.Events[0].Event, &stored); e != nil {
				t.Fatal(e)
			}
			p, ok := stored["provenance"].(map[string]any)
			// In export mode the browser owns the provenance assertion, so deletion
			// mutations fail on DOM icon_count rather than aborting fixture export.
			if os.Getenv("N3_HISTORY_FIXTURE_OUTPUT") == "" && (!ok || p["kind"] != "agent" || p["attested_by"] != "hub" || p["trust_decision"] != "trusted" || !strings.EqualFold(fmt.Sprint(p["display_name"]), sourceName)) {
				t.Fatalf("canonical history provenance missing or wrong: %s", page.Events[0].Event)
			}
			// Exercise the actual SSE handler independently of browser memory.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest("GET", "/internal/view/kimi/events", nil).WithContext(ctx)
			req.SetPathValue("session", "Kimi.zzc")
			w := httptest.NewRecorder()
			v.hub.HandleInternalViewEvents(w, req)
			sse := w.Body.String()
			if !strings.Contains(sse, "id: ") || !strings.Contains(sse, message) {
				t.Fatalf("canonical SSE missing id/message: %s", sse)
			}
			fixtures[name] = map[string]any{"history": page, "sse": sse, "message_id": message, "delivery_id": delivery, "origin_delivery_id": delivery, "turn_id": "turn-" + name, "db_event": stored}
		})
	}
	if output := os.Getenv("N3_HISTORY_FIXTURE_OUTPUT"); output != "" {
		raw, e := json.MarshalIndent(fixtures, "", "  ")
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(output, raw, 0600); e != nil {
			t.Fatal(e)
		}
	}
}

func TestHistoryProvenanceObservationRejectsCopyAndUntrusted(t *testing.T) {
	for _, which := range []string{"copy", "control", "untrusted", "primary"} {
		t.Run(which, func(t *testing.T) {
			h, _, ctx := newTestHub(t)
			history, e := store.OpenSessionHistory(filepath.Join(t.TempDir(), "history.db"))
			if e != nil {
				t.Fatal(e)
			}
			defer history.Close()
			if e = history.SetEnabled(ctx, true, time.Now()); e != nil {
				t.Fatal(e)
			}
			h.SessionHistory, h.SessionHistoryEnabled = history, true
			c := &sessionClient{keyID: "key", ownerKey: "owner", metricsAgentID: "agent", sessionName: "work"}
			h.sessionClients = map[string]map[string]*sessionClient{"key": {"work": c}}
			role := which
			if role == "untrusted" {
				role = "primary"
			}
			env := model.Envelope{ID: "source", From: "timer#key", Body: "synthetic", Meta: map[string]any{hubIngressMetaKey: hubIngressAgent}}
			if which == "untrusted" {
				delete(env.Meta, hubIngressMetaKey)
			}
			h.publishInputObservation("key", "work", env, "d", "hub_received", role, true)
			event := `{"kind":"user_input","delivery_id":"d","origin_delivery_id":"d","turn_id":"t","data":{"lifecycle":"admitted"}}`
			h.handleSessionEventEnvelope(c, model.Envelope{ID: "echo", Body: event, Meta: map[string]any{"type": terminalSessionEventType}})
			page, e := history.Page(ctx, "owner", "agent", "work", "", nil)
			if e != nil || len(page.Events) != 1 {
				t.Fatalf("page %+v err=%v", page, e)
			}
			var stored map[string]any
			_ = json.Unmarshal(page.Events[0].Event, &stored)
			_, found := stored["provenance"]
			if found != (which == "primary") {
				t.Fatalf("%s proof=%v", which, found)
			}
		})
	}
}
