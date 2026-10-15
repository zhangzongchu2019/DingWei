package m8

// Real local helper -> production DriverAdapter/CodexProtocolDriver -> fake
// external app-server CLI process -> helper session events -> authenticated Hub
// ingress -> live AMR worker. No direct process/ingest/replyToOrigin calls.
import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/zhangzongchu2019/dingwei/internal/bus"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

func TestFailureNoticeDriverTurnEndToEnd(t *testing.T) {
	python := os.Getenv("DW_FAILURE_NOTICE_PYTHON")
	if python == "" {
		t.Skip("set DW_FAILURE_NOTICE_PYTHON to locked helper runtime")
	}
	e := newAgentEnv(t)
	h := e.hub
	ctx := e.ctx
	groups, err := e.svc.Store.ListAgentGroups(ctx, e.agID)
	if err != nil || len(groups) == 0 {
		t.Fatal("target group unavailable")
	}
	sourceCred := e.enrollInGroup("notice-driver-source", "Kimi", groups[0])
	key := AgentKeyID("zzc")
	if err := h.BindAccount(ctx, key, "dev:personal:ou_notice_owner"); err != nil {
		t.Fatal(err)
	}
	if err := h.Repo.UpsertMember(ctx, model.Member{OwnerKey: "zzc", DisplayName: "Owner", FeishuOpenID: "ou_notice_owner", Role: model.RoleMember, Active: true}); err != nil {
		t.Fatal(err)
	}
	h.RegisterBot("notice-test-bot", "notice-test-bot")
	h.Outbound = bus.NewDBQueue(h.Repo, model.DirectionOut)
	target, status, _ := e.dial(e.cred, "")
	if target == nil {
		t.Fatalf("target connect status%d", status)
	}
	defer target.Close(websocket.StatusNormalClosure, "done")
	frames := vmCollect(target)
	waitSessionOnline(t, h, key, "opus50ts")
	cases := []struct{ name, output string }{
		{"full_agent", "@Opus50TS.zzc R3_OUTPUT_full"},
		{"member_session", "@zzc#opus50ts R3_OUTPUT_member"},
		{"short_session", "#opus50ts R3_OUTPUT_session"},
		{"channel", "@飞书#ou_notice_owner R3_OUTPUT_channel"},
		{"plain_return", "R3_OUTPUT_plain"},
	}
	outputs := map[string]string{}
	for _, c := range cases {
		outputs["dw-r3-control-"+c.name] = c.output
		outputs[model.FailureNoticeDeliveryID("r3-failed-"+c.name)] = c.output
	}
	state, err := os.MkdirTemp("", "fn-driver-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(state)
	defer func() {
		if t.Failed() {
			if dir := os.Getenv("DW_FAILURE_NOTICE_LOG_DIR"); dir != "" {
				_ = os.MkdirAll(dir, 0700)
				for _, name := range []string{"helper.log", "injections.jsonl"} {
					if b, err := os.ReadFile(filepath.Join(state, name)); err == nil {
						_ = os.WriteFile(filepath.Join(dir, name), b, 0600)
					}
				}
			}
		}
	}()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	childCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()
	cmd := exec.CommandContext(childCtx, python, "-B", filepath.Join(root, "internal/m8/testdata/failure_notice_driver_client.py"))
	for _, v := range os.Environ() {
		k, _, _ := strings.Cut(v, "=")
		if k == "HOME" || k == "PYTHONPATH" || strings.HasPrefix(k, "SH_") || strings.HasPrefix(k, "DW_") || strings.HasPrefix(k, "DINGWEI_") || strings.HasPrefix(k, "CODEX_") || strings.HasPrefix(k, "CLAUDE_") || strings.HasPrefix(k, "OPENCODE_") {
			continue
		}
		cmd.Env = append(cmd.Env, v)
	}
	cmd.Env = append(cmd.Env, "HOME="+state, "PYTHONPATH="+filepath.Join(root, "tools/sessionhelper"), "PYTHONDONTWRITEBYTECODE=1")
	log, err := os.OpenFile(filepath.Join(state, "helper.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	cmd.Stdout = log
	cmd.Stderr = log
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		input.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case err := <-done:
			if err != nil && !t.Failed() {
				t.Errorf("local helper failed during cleanup: %v", err)
			}
		case <-time.After(5 * time.Second):
			cancel()
			<-done
			if !t.Failed() {
				t.Error("local helper failed to stop within cleanup budget")
			}
		}
	}()
	if err := json.NewEncoder(input).Encode(map[string]any{"state": state, "ws_base": "ws" + strings.TrimPrefix(e.srv.URL, "http"), "secret": sourceCred, "outputs": outputs}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 20*time.Second, "real helper Hub connection", func() bool { _, err := os.Stat(filepath.Join(state, "ready")); return err == nil })
	waitFor(t, 3*time.Second, "persisted source ACK capability", func() bool {
		ack, found, err := h.Repo.SessionEndpointDeliveryACK(ctx, key, "kimi")
		return err == nil && found && ack
	})
	enqueue := func(t *testing.T, id, body, sourceType, provenance string, meta map[string]any, created time.Time) {
		meta["type"] = "delivery"
		meta["delivery_id"] = id
		meta["delivery_version"] = 1
		meta["ack_required"] = true
		raw, _ := json.Marshal(model.Envelope{ID: id, From: sessionAddress("opus50ts", key), To: sessionAddress("kimi", key), Body: body, Meta: meta})
		if err := h.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: id, KeyID: key, SessionName: "kimi", SourceMessageID: id, SourceType: sourceType, Provenance: provenance, Envelope: string(raw), CreatedAt: created}); err != nil {
			t.Fatal(err)
		}
		h.mu.Lock()
		client := h.sessionClients[key]["kimi"]
		h.mu.Unlock()
		h.drainSessionDeliveries(ctx, client)
	}
	done := func(t *testing.T, id, body string) {
		waitFor(t, 25*time.Second, "canonical driver output and terminal event", func() bool {
			h.mu.Lock()
			defer h.mu.Unlock()
			st := h.terminals[terminalKey(key, "kimi")]
			if st == nil {
				return false
			}
			text, completed := false, false
			for _, raw := range st.eventRing {
				var ev amrSessionEvent
				if json.Unmarshal([]byte(raw), &ev) == nil && ev.DeliveryID == id {
					if ev.Kind == "assistant_text" && ev.Text == body {
						text = true
					}
					if amrTerminalEvent(ev) && amrTerminalSucceeded(ev.Data) {
						completed = true
					}
				}
			}
			return text && completed
		})
		waitFor(t, 3*time.Second, "normal AMR worker closed this actual turn", func() bool {
			h.amr.mu.Lock()
			defer h.amr.mu.Unlock()
			_, ok := h.amr.completed[amrTurnKey{KeyID: key, Session: "kimi", DeliveryID: id, TurnID: "turn-" + id}]
			return ok
		})
	}
	// Positive controls prove every configured reference can really forward, and
	// ordinary non-reference output really returns through the same driver path.
	positiveStart, _ := h.amr.snapshotStats()
	for _, c := range cases {
		t.Run("positive_"+c.name, func(t *testing.T) {
			id := "dw-r3-control-" + c.name
			enqueue(t, id, "ordinary local task", "session_inbound", "", map[string]any{"kind": "content", "reply_policy": "expect_reply"}, time.Now())
			done(t, id, c.output)
			if c.name == "channel" {
				waitFor(t, 3*time.Second, "positive channel actually queued", func() bool {
					m, err := h.Repo.ClaimNextMessage(ctx, model.DirectionOut)
					return err == nil && m != nil && strings.Contains(m.Content, "R3_OUTPUT_channel")
				})
			} else {
				frames.next(t, c.output)
			}
		})
	}
	// closed is incremented before replyToOrigin returns. Peer wire receipt also
	// precedes its final counter update, so neither is a completion barrier.
	waitFor(t, 3*time.Second, "positive plain return counter committed before baseline", func() bool {
		s, _ := h.amr.snapshotStats()
		return s.closed >= uint64(len(cases)) && s.replyOriginRouted == positiveStart.replyOriginRouted+1
	})
	baseline, _ := h.amr.snapshotStats()
	for index, c := range cases {
		t.Run("notice_"+c.name, func(t *testing.T) {
			now := time.Now().UTC()
			failed := "r3-failed-" + c.name
			raw, _ := json.Marshal(model.Envelope{ID: failed, From: sessionAddress("kimi", key), To: sessionAddress("never", key), Body: "local expired task"})
			if err := h.Repo.EnqueueSessionDelivery(ctx, model.SessionDelivery{ID: failed, KeyID: key, SessionName: "never", SourceMessageID: failed, SourceType: "session_inbound", Envelope: string(raw), CreatedAt: now.Add(-31 * time.Minute)}); err != nil {
				t.Fatal(err)
			}
			h.expireCapabilityPendingDeliveries(ctx, now)
			id := model.FailureNoticeDeliveryID(failed)
			waitFor(t, 10*time.Second, "actual CLI admission keeps persisted notice ID", func() bool {
				data, err := os.ReadFile(filepath.Join(state, "injections.jsonl"))
				if err != nil || !strings.HasSuffix(string(data), "\n") {
					return false
				}
				lines := strings.Split(strings.TrimSpace(string(data)), "\n")
				found := false
				for _, line := range lines {
					var record struct {
						DeliveryID string `json:"delivery_id"`
					}
					if json.Unmarshal([]byte(line), &record) != nil {
						return false
					}
					if record.DeliveryID == id {
						found = true
					}
				}
				if len(lines) >= len(cases)+index+1 && !found {
					t.Fatal("actual CLI command ID differs from persisted notice ID")
				}
				return found
			})
			done(t, id, c.output)
			waitFor(t, 5*time.Second, "notice processed ACK", func() bool { d, _ := h.Repo.GetSessionDelivery(ctx, id); return d != nil && d.State == "processed" })
		})
	}
	// Wait for all queued jobs to leave process(), not just terminal aggregation.
	waitFor(t, 3*time.Second, "notice jobs closed", func() bool { s, _ := h.amr.snapshotStats(); return s.closed >= baseline.closed+uint64(len(cases)) })
	after, _ := h.amr.snapshotStats()
	if after.targetsAttempted != baseline.targetsAttempted || after.targetsDispatched != baseline.targetsDispatched || after.replyOriginRouted != baseline.replyOriginRouted || after.notificationsSent != baseline.notificationsSent {
		t.Fatalf("notice turn forwarded/returned/notified: attempts%d dispatch%d return%d notice%d", after.targetsAttempted-baseline.targetsAttempted, after.targetsDispatched-baseline.targetsDispatched, after.replyOriginRouted-baseline.replyOriginRouted, after.notificationsSent-baseline.notificationsSent)
	}
	deadline := time.After(150 * time.Millisecond)
observe:
	for {
		select {
		case packet := <-frames.ch:
			if strings.Contains(packet.Body, "R3_OUTPUT_") {
				t.Fatal("notice output forwarded or returned on target wire")
			}
		case <-deadline:
			break observe
		}
	}
	if msg, err := h.Repo.ClaimNextMessage(ctx, model.DirectionOut); err != nil || msg != nil {
		t.Fatal("notice output reached channel queue")
	}
	injections, err := os.ReadFile(filepath.Join(state, "injections.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(string(injections)), "\n") {
		var item struct {
			DeliveryID string `json:"delivery_id"`
			Input      []struct {
				Text string `json:"text"`
			} `json:"input"`
		}
		if json.Unmarshal([]byte(line), &item) != nil {
			t.Fatal("bad private CLI injection record")
		}
		counts[item.DeliveryID]++
		if strings.HasPrefix(item.DeliveryID, "dw-dlv-failure-") {
			business := ""
			if len(item.Input) == 1 {
				business = item.Input[0].Text
				const boundary = "[End DingWei platform context; business request follows.]\n\n"
				if index := strings.Index(business, boundary); index >= 0 {
					business = business[index+len(boundary):]
				}
			}
			if !strings.HasPrefix(business, "【系统投递失败通知】\n") {
				t.Fatal("driver did not receive the actual notice")
			}
		}
	}
	for _, c := range cases {
		if counts[model.FailureNoticeDeliveryID("r3-failed-"+c.name)] != 1 {
			t.Fatal("canonical notice ID rewritten or duplicate CLI injection")
		}
	}
}
