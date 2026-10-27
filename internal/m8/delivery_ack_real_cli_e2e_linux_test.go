package m8

// Actual local Hub + SessionHelper + production DriverAdapter/protocol driver +
// installed native CLI. Only the loopback model endpoint and transport losses
// are controlled. Never substitute a synthetic app-server/stream-json process.
import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/model"
)

type realACKObservation struct {
	Requests map[string][]struct {
		At float64 `json:"at"`
	} `json:"requests"`
	Admissions map[string]struct {
		At     float64 `json:"at"`
		TurnID string  `json:"turn_id"`
	} `json:"admissions"`
	Terminals map[string]struct {
		At     float64 `json:"at"`
		TurnID string  `json:"turn_id"`
	} `json:"terminals"`
}
type realACKLocal struct {
	State     string `json:"state"`
	TurnID    string `json:"turn_id"`
	Injected  string `json:"injected_stamp"`
	Processed string `json:"processed_stamp"`
	Outcome   string `json:"turn_outcome"`
	Protocol  int    `json:"ack_protocol"`
}
type realACKProbe struct {
	*agentEnv
	kind, root string
	cmd        *exec.Cmd
	input      io.WriteCloser
	done       chan struct{}
	controlID  int
	inputMu    sync.Mutex
}

func startRealACKProbe(t *testing.T, e *agentEnv, kind, root string, options ...map[string]any) *realACKProbe {
	t.Helper()
	python := os.Getenv("DW_ACK_V2_E2E_PYTHON")
	if python == "" {
		t.Skip("INCONCLUSIVE: set DW_ACK_V2_E2E_PYTHON for actual CLI/Hub acceptance")
	}
	if _, err := exec.LookPath(kind); err != nil {
		t.Fatalf("INCONCLUSIVE: actual %s CLI missing: %v", kind, err)
	}
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	if root == "" {
		root = t.TempDir()
	}
	cmd := exec.Command(python, "-B", filepath.Join(source, "internal/m8/testdata/ack_v2_real_cli_client.py"))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PATH" || key == "HOME" || key == "LANG" || key == "USER" || key == "LOGNAME" || key == "TMPDIR" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	packageRoot := filepath.Join(source, "tools/sessionhelper")
	legacy := false
	for _, option := range options {
		if path, ok := option["package_root"].(string); ok {
			packageRoot = path
			legacy = true
		}
	}
	cmd.Env = append(cmd.Env, "PYTHONPATH="+packageRoot, "PYTHONDONTWRITEBYTECODE=1")
	log, err := os.OpenFile(filepath.Join(root, "helper.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &realACKProbe{agentEnv: e, kind: kind, root: root, cmd: cmd, input: input, done: make(chan struct{})}
	go func() { _ = cmd.Wait(); log.Close(); close(p.done) }()
	t.Cleanup(func() {
		input.Close()
		select {
		case <-p.done:
		case <-time.After(7 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-p.done
		}
		if t.Failed() {
			if dir := os.Getenv("DW_ACK_V2_E2E_LOG_DIR"); dir != "" {
				_ = os.MkdirAll(dir, 0700)
				for _, name := range []string{"helper.log", "model-observations.json", "local-snapshot.json", "debug-state.json", "native-acks.json", "native-confirms.json", "view.log"} {
					if body, err := os.ReadFile(filepath.Join(root, name)); err == nil {
						_ = os.WriteFile(filepath.Join(dir, kind+"-"+filepath.Base(root)+"-"+name), body, 0600)
					}
				}
			}
		}
	})
	boot := map[string]any{"driver": kind, "state": root, "ws_base": "ws" + strings.TrimPrefix(e.srv.URL, "http"), "secret": e.cred}
	for _, option := range options {
		for key, value := range option {
			boot[key] = value
		}
	}
	if err := json.NewEncoder(input).Encode(boot); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 25*time.Second, "真实 helper 已连 Hub 并协商 ACK v2", func() bool {
		e.hub.mu.Lock()
		c := e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
		e.hub.mu.Unlock()
		if c == nil {
			return false
		}
		c.stateMu.Lock()
		defer c.stateMu.Unlock()
		return c.helperReady && c.deliveryACKV1 && c.deliveryACKV2 != legacy
	})
	t.Cleanup(func() {
		if t.Failed() {
			select {
			case <-p.done:
				return
			default:
			}
			p.control("debug", nil)
		}
	})
	return p
}
func (p *realACKProbe) control(op string, fields map[string]any) {
	p.t.Helper()
	p.inputMu.Lock()
	defer p.inputMu.Unlock()
	if p.controlID == 0 {
		p.controlID = int(time.Now().UnixNano())
	}
	p.controlID++
	if fields == nil {
		fields = map[string]any{}
	}
	fields["operation"] = op
	fields["id"] = p.controlID
	if err := json.NewEncoder(p.input).Encode(fields); err != nil {
		p.t.Fatal(err)
	}
	waitFor(p.t, 10*time.Second, "isolated CLI control completed: "+op, func() bool {
		_, err := os.Stat(filepath.Join(p.root, fmt.Sprintf("control-%d.json", p.controlID)))
		return err == nil
	})
}
func (p *realACKProbe) observations() realACKObservation {
	p.t.Helper()
	var out realACKObservation
	body, err := os.ReadFile(filepath.Join(p.root, "model-observations.json"))
	if err == nil {
		_ = json.Unmarshal(body, &out)
	}
	return out
}
func (p *realACKProbe) record(id string) *model.SessionDelivery {
	p.t.Helper()
	d, err := p.hub.Repo.GetSessionDelivery(p.ctx, id)
	if err != nil {
		p.t.Fatal(err)
	}
	return d
}
func (p *realACKProbe) enqueue(id, label string) {
	p.t.Helper()
	env := model.Envelope{ID: "source-" + id, From: "origin#" + AgentKeyID("zzc"), To: "opus50ts#" + AgentKeyID("zzc"), Body: label + " Return a short controlled answer.", Meta: map[string]any{"type": "delivery", "delivery_id": id, "delivery_version": 1, "ack_required": true, "reply_policy": "no_reply", "conversation_terminal": true, "suppress_reply_origin": true}}
	body, err := json.Marshal(env)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := p.hub.Repo.EnqueueSessionDelivery(p.ctx, model.SessionDelivery{ID: id, KeyID: AgentKeyID("zzc"), SessionName: "opus50ts", SourceMessageID: env.ID, SourceType: "session_inbound", Envelope: string(body), CreatedAt: time.Now().UTC()}); err != nil {
		p.t.Fatal(err)
	}
	p.hub.mu.Lock()
	c := p.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
	p.hub.mu.Unlock()
	if c == nil {
		p.t.Fatal("isolated helper disconnected before enqueue")
	}
	p.hub.publishInputObservation(AgentKeyID("zzc"), "opus50ts", env, id, "hub_received", "primary", true)
	p.hub.drainSessionDeliveries(p.ctx, c)
}
func (p *realACKProbe) duplicate(id string) {
	p.t.Helper()
	d := p.record(id)
	var env model.Envelope
	if err := json.Unmarshal([]byte(d.Envelope), &env); err != nil {
		p.t.Fatal(err)
	}
	env.Meta["delivery_ack_protocol"] = d.AckProtocol
	env.Meta["processing_mode"] = d.ProcessingMode
	env.Meta["ack_generation"] = d.AckGeneration
	p.hub.mu.Lock()
	c := p.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
	p.hub.mu.Unlock()
	if err := c.write(p.ctx, env); err != nil {
		p.t.Fatal(err)
	}
}
func (p *realACKProbe) waitState(id, state, criterion string) *model.SessionDelivery {
	p.t.Helper()
	var out *model.SessionDelivery
	waitFor(p.t, 25*time.Second, criterion, func() bool { out = p.record(id); return out != nil && out.State == state })
	return out
}
func (p *realACKProbe) snapshot(ids ...string) map[string]realACKLocal {
	p.t.Helper()
	p.control("snapshot", map[string]any{"identifiers": ids})
	var out map[string]realACKLocal
	body, err := os.ReadFile(filepath.Join(p.root, "local-snapshot.json"))
	if err != nil {
		p.t.Fatal(err)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		p.t.Fatal(err)
	}
	return out
}

type realACKView struct {
	t     *testing.T
	root  string
	input io.WriteCloser
	seq   int
}
type realACKViewSnapshot struct {
	Waiting     bool
	WaitingText string
	Stage       string
	CardKey     string
	Timeline    string
	Errors      []string
}

func startRealACKView(t *testing.T, p *realACKProbe) *realACKView {
	t.Helper()
	html, err := os.ReadFile("../../view-service/renderer-canary-sse.html")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/events"):
			replay, ch, unsub := p.hub.subscribeSessionEvents(AgentKeyID("zzc"), "opus50ts", "real-view")
			p.hub.mu.Lock()
			originalTerminal := p.hub.terminals[terminalKey(AgentKeyID("zzc"), "opus50ts")]
			p.hub.mu.Unlock()
			epochPoll := time.NewTicker(100 * time.Millisecond)
			defer epochPoll.Stop()
			defer unsub()
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(200)
			flush := w.(http.Flusher)
			for _, event := range replay {
				fmt.Fprintf(w, "data: %s\n\n", event)
			}
			flush.Flush()
			for {
				select {
				case <-r.Context().Done():
					return
				case <-epochPoll.C:
					p.hub.mu.Lock()
					current := p.hub.terminals[terminalKey(AgentKeyID("zzc"), "opus50ts")]
					p.hub.mu.Unlock()
					if current != originalTerminal {
						return
					} // reconnect to the new actual Hub epoch
				case event := <-ch:
					fmt.Fprintf(w, "data: %s\n\n", event)
					flush.Flush()
				}
			}
		case strings.HasSuffix(r.URL.Path, "/status"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"session":"ack-fixture","state":"idle","readonly":true,"can_write":false,"auth_mode":"legacy","owner":"fixture"}`)
		case r.URL.Path == "/view/ack-fixture":
			w.Header().Set("Content-Type", "text/html")
			w.Write(html)
		default:
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `[]`)
		}
	}))
	t.Cleanup(server.Close)
	cmd := exec.Command("node", "testdata/ack_v2_real_view.cjs")
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		if key == "PATH" || key == "HOME" || key == "NODE_PATH" || key == "LANG" {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	log, err := os.Create(filepath.Join(p.root, "view.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout = log
	cmd.Stderr = log
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatalf("INCONCLUSIVE: actual browser missing: %v", err)
	}
	done := make(chan struct{})
	go func() { cmd.Wait(); log.Close(); close(done) }()
	t.Cleanup(func() {
		input.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	})
	json.NewEncoder(input).Encode(map[string]any{"url": server.URL + "/view/ack-fixture", "root": p.root})
	waitFor(t, 15*time.Second, "INCONCLUSIVE: actual View browser starts", func() bool { _, err := os.Stat(filepath.Join(p.root, "view-ready.json")); return err == nil })
	return &realACKView{t: t, root: p.root, input: input}
}
func (v *realACKView) snapshot(id string) realACKViewSnapshot {
	v.t.Helper()
	v.seq++
	if err := json.NewEncoder(v.input).Encode(map[string]any{"id": v.seq, "delivery_id": id}); err != nil {
		v.t.Fatal(err)
	}
	file := filepath.Join(v.root, fmt.Sprintf("view-%d.json", v.seq))
	waitFor(v.t, 5*time.Second, "actual View snapshot", func() bool { _, err := os.Stat(file); return err == nil })
	body, _ := os.ReadFile(file)
	var out realACKViewSnapshot
	if err := json.Unmarshal(body, &out); err != nil {
		v.t.Fatal(err)
	}
	if len(out.Errors) > 0 {
		v.t.Fatalf("actual View script errors: %v", out.Errors)
	}
	return out
}

func TestDeliveryACKActualCLIQueuedAdmissionAndTerminal(t *testing.T) {
	for _, variant := range []string{"claude", "codex", "claude_explicit"} {
		kind := strings.TrimSuffix(variant, "_explicit")
		t.Run(variant, func(t *testing.T) {
			e := newAgentEnv(t)
			p := startRealACKProbe(t, e, kind, "", map[string]any{"explicit_claude": variant == "claude_explicit"})
			view := startRealACKView(t, p)
			a, b := "dw-dlv-r40-A", "dw-dlv-r40-B"
			p.enqueue(a, "R40_ACK_A")
			waitFor(t, 25*time.Second, "判据1：A 已由实际CLI接纳且模型保持运行", func() bool {
				d := p.record(a)
				return len(p.observations().Requests["R40_ACK_A"]) == 1 && d != nil && (d.State == "injected" || d.State == "processed")
			})
			originalA := p.record(a)
			p.enqueue(b, "R40_ACK_B")
			waitFor(t, 10*time.Second, "判据2：Hub可见B本地排队", func() bool { d := p.record(b); return d != nil && d.DeliveredAt != nil })
			p.duplicate(b)
			until := time.Now().Add(31 * time.Second)
			for time.Now().Before(until) {
				d := p.record(b)
				if d.State != "delivered" || d.InjectedAt != nil || d.ProcessedAt != nil || len(p.observations().Requests["R40_ACK_B"]) != 0 {
					t.Fatalf("判据2：A长回合期间B排队不得提前 injected/processed: %+v", d)
				}
				time.Sleep(50 * time.Millisecond)
			}
			waitFor(t, 5*time.Second, "判据2：实际View超过30秒仍显示helper已排队、未注入及下一步", func() bool {
				snap := view.snapshot(b)
				return snap.Waiting && strings.Contains(snap.WaitingText, "尚未注入会话") && strings.Contains(snap.WaitingText, "不要按已执行处理")
			})
			queued := p.record(b)
			p.control("release", map[string]any{"label": "R40_ACK_A"})
			waitFor(t, 60*time.Second, "判据3：实际CLI接纳B后Hub才可记录注入", func() bool {
				d := p.record(b)
				return len(p.observations().Requests["R40_ACK_B"]) == 1 && d != nil && (d.State == "injected" || d.State == "processed")
			})
			activeB := p.record(b)
			admission := p.observations().Admissions[b]
			if activeB.TurnID == "" || admission.TurnID != activeB.TurnID || activeB.InjectedAt == nil || float64(activeB.InjectedAt.UnixNano())/1e9+0.000001 < admission.At {
				t.Fatalf("判据3：B注入须关联真实admission回执及其后时刻: db=%+v receipt=%+v", activeB, admission)
			}
			time.Sleep(200 * time.Millisecond)
			if d := p.record(b); d.State != "injected" || d.ProcessedAt != nil || len(p.observations().Terminals) != 1 {
				t.Fatalf("判据4：B真实终态前不得因函数返回标 processed: %+v", d)
			}
			waitFor(t, 5*time.Second, "判据3：实际View只在原生admission之后显示已注入执行", func() bool {
				snap := view.snapshot(b)
				return !snap.Waiting && strings.Contains(snap.Stage, "已注入执行")
			})
			injectedCard := view.snapshot(b).CardKey
			if originalA.State != "injected" || originalA.InjectedAt == nil || originalA.ProcessedAt != nil {
				t.Fatalf("判据1：保持A运行时须仅有真实 injected: %+v", originalA)
			}
			if queued.DeliveredAt == nil || !queued.DeliveredAt.Equal(*p.record(b).DeliveredAt) {
				t.Fatal("判据5：排队首个Hub时刻不得重置")
			}
			p.control("release", map[string]any{"label": "R40_ACK_B"})
			done := p.waitState(b, "processed", "判据4：真实terminal后才允许processed")
			waitFor(t, 5*time.Second, "判据4：实际View真实terminal后显示回合已结束", func() bool { return strings.Contains(view.snapshot(b).Stage, "回合已结束（完成）") })
			if injectedCard == "" || view.snapshot(b).CardKey != injectedCard {
				t.Fatal("判据4：终态必须原位更新同一张实际View卡")
			}
			terminal := p.observations().Terminals[b]
			if done.TurnOutcome != "completed" || done.ProcessedAt == nil || terminal.TurnID != done.TurnID || float64(done.ProcessedAt.UnixNano())/1e9+0.000001 < terminal.At {
				t.Fatalf("判据4：processed必须关联真实terminal和原turn: db=%+v event=%+v", done, terminal)
			}
			p.duplicate(b)
			confirms := func() int {
				body, _ := os.ReadFile(filepath.Join(p.root, "native-confirms.json"))
				var rows []map[string]any
				_ = json.Unmarshal(body, &rows)
				count := 0
				for _, row := range rows {
					if row["delivery_id"] == b {
						count++
					}
				}
				return count
			}
			priorConfirm := confirms()
			p.control("replay_queued", map[string]any{"delivery_id": b})
			waitFor(t, 5*time.Second, "判据5：迟到原queued ACK由Hub真实确认", func() bool { return confirms() > priorConfirm })
			time.Sleep(200 * time.Millisecond)
			replayed := p.record(b)
			if replayed.State != "processed" || replayed.TurnID != done.TurnID || !replayed.DeliveredAt.Equal(*queued.DeliveredAt) || !replayed.ProcessedAt.Equal(*done.ProcessedAt) {
				t.Fatalf("判据5：真实重复投递不得倒退终态或改写原时刻: %+v", replayed)
			}
			local := p.snapshot(b)[b]
			if local.Injected != done.InjectedAt.Format(time.RFC3339Nano) || local.Processed != done.ProcessedAt.Format(time.RFC3339Nano) { // RFC3339Nano trims trailing fractional zeros.
				x, _ := time.Parse(time.RFC3339Nano, local.Injected)
				y, _ := time.Parse(time.RFC3339Nano, local.Processed)
				if !x.Equal(*done.InjectedAt) || !y.Equal(*done.ProcessedAt) {
					t.Fatalf("判据5：Hub必须保留helper原时刻: %+v %+v", local, done)
				}
			}
			if len(p.observations().Requests["R40_ACK_A"]) != 1 || len(p.observations().Requests["R40_ACK_B"]) != 1 {
				t.Fatal("判据5：不得重复模型请求")
			}
			t.Log("判据1–5真实Hub/helper/CLI通过；判据6由独立变异运行核验；实际View三阶段及30秒提示同链路通过")
		})
	}
}

func TestDeliveryACKActualCLIReplaysPredecessorAcrossDisconnect(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		for _, fault := range []string{"drop_injected", "drop_injected_confirm"} {
			t.Run(kind+"/"+fault, func(t *testing.T) {
				e := newAgentEnv(t)
				p := startRealACKProbe(t, e, kind, "")
				p.control("fault", map[string]any{"name": fault, "value": true})
				id := "dw-dlv-r40-R1"
				p.enqueue(id, "R40_ACK_R1")
				waitFor(t, 25*time.Second, "判据7：实际CLI已经接纳R1", func() bool { return p.observations().Admissions[id].TurnID != "" })
				expected := "delivered"
				if fault == "drop_injected_confirm" {
					expected = "injected"
				}
				before := p.waitState(id, expected, "判据7：注入回执或阶段确认丢失窗口")
				p.control("disconnect", nil)
				p.control("release", map[string]any{"label": "R40_ACK_R1"})
				waitFor(t, 25*time.Second, "判据7：断线期间真实回合终态落本地", func() bool {
					return p.observations().Terminals[id].TurnID != "" && p.snapshot(id)[id].State == "processed"
				})
				local := p.snapshot(id)[id]
				p.control("fault", map[string]any{"name": fault, "value": false})
				p.control("reconnect", nil)
				after := p.waitState(id, "processed", "判据7：重连主动补放injected前驱再processed")
				injected, err1 := time.Parse(time.RFC3339Nano, local.Injected)
				processed, err2 := time.Parse(time.RFC3339Nano, local.Processed)
				if err1 != nil || err2 != nil || after.InjectedAt == nil || after.ProcessedAt == nil || !after.InjectedAt.Equal(injected) || !after.ProcessedAt.Equal(processed) || after.TurnID != local.TurnID || after.TurnOutcome != "completed" || !after.DeliveredAt.Equal(*before.DeliveredAt) || len(p.observations().Requests["R40_ACK_R1"]) != 1 {
					t.Fatalf("判据7：原时刻、原turn、有序前驱及只执行一次: local=%+v hub=%+v", local, after)
				}
				t.Log("判据7真实CLI断连确认边界通过", kind, fault)
			})
		}
	}
}

func (p *realACKProbe) crash() {
	p.t.Helper()
	if err := syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL); err != nil {
		p.t.Fatal(err)
	}
	select {
	case <-p.done:
	case <-time.After(5 * time.Second):
		p.t.Fatal("isolated CLI crash group did not terminate")
	}
	waitFor(p.t, 5*time.Second, "isolated helper disconnected after crash", func() bool {
		p.hub.mu.Lock()
		defer p.hub.mu.Unlock()
		return p.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"] == nil
	})
}

func TestDeliveryACKActualCLIAdmissionCrashWindows(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		for _, window := range []string{"dispatch_sent_receipt_unread", "receipt_before_admitted_persist"} {
			t.Run(kind+"/"+window, func(t *testing.T) {
				e := newAgentEnv(t)
				p := startRealACKProbe(t, e, kind, "", map[string]any{"fault_window": window, "persistent_thread": true})
				id := "dw-dlv-r40-CRASH"
				p.enqueue(id, "R40_ACK_CRASH")
				waitFor(t, 25*time.Second, "判据8：实际CLI已接纳且helper停在指定持久化窗口", func() bool {
					_, err := os.Stat(filepath.Join(p.root, "crash-window.json"))
					return err == nil && len(p.observations().Requests["R40_ACK_CRASH"]) == 1
				})
				before := p.waitState(id, "delivered", "判据8：持久化前没有假的injected")
				originalAdmission := p.observations().Admissions[id]
				p.crash()
				restored := startRealACKProbe(t, e, kind, p.root, map[string]any{"persistent_thread": true})
				waitFor(t, 40*time.Second, "判据8：等待原请求恢复对账实际完成", func() bool {
					restored.control("recovery_status", nil)
					var status struct {
						Started  bool
						Restored bool
						Current  string
					}
					body, _ := os.ReadFile(filepath.Join(restored.root, "recovery-status.json"))
					_ = json.Unmarshal(body, &status)
					queryBody, _ := os.ReadFile(filepath.Join(restored.root, "recovery-queries.json"))
					var queries []string
					_ = json.Unmarshal(queryBody, &queries)
					queried := false
					for _, query := range queries {
						if query == id {
							queried = true
						}
					}
					return queried && status.Started && status.Restored && (status.Current == "" || restored.snapshot(id)[id].State == "admitted")
				})
				var after *model.SessionDelivery
				waitFor(t, 30*time.Second, "判据8：恢复只对账原请求；无法证实必须admission_unknown隔离", func() bool {
					after = restored.record(id)
					return after != nil && (after.State == "admission_unknown" || after.State == "injected")
				})
				if after.State == "injected" && (originalAdmission.TurnID == "" || after.TurnID != originalAdmission.TurnID) {
					t.Fatalf("判据8：不能把无证据或新turn当作原admission: before=%+v after=%+v", originalAdmission, after)
				}
				if after.ProcessedAt != nil || !after.DeliveredAt.Equal(*before.DeliveredAt) || len(restored.observations().Requests["R40_ACK_CRASH"]) != 1 {
					t.Fatalf("判据8：不确定窗恢复不得二次模型请求、伪造终态或重置原排队时刻: %+v", after)
				}
				time.Sleep(2 * time.Second)
				if len(restored.observations().Requests["R40_ACK_CRASH"]) != 1 {
					t.Fatal("判据8：恢复后发生延迟重复注入")
				}
				t.Log("判据8真实CLI崩溃窗口通过", kind, window, after.State)
			})
		}
	}
}

func (p *realACKProbe) nativeACKs() []model.Envelope {
	p.t.Helper()
	var out []model.Envelope
	body, err := os.ReadFile(filepath.Join(p.root, "native-acks.json"))
	if err == nil {
		_ = json.Unmarshal(body, &out)
	}
	return out
}
func TestDeliveryACKActualCLICancelAtomicBoundary(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) {
			e := newAgentEnv(t)
			p := startRealACKProbe(t, e, kind, "")
			p.control("fault", map[string]any{"name": "pause_claim", "value": true})
			cancelled := "dw-dlv-r40-CANCEL"
			p.enqueue(cancelled, "R40_ACK_CANCEL")
			p.waitState(cancelled, "delivered", "判据10：取消先行窗口已本地落库但未claim")
			result := p.hub.cancelSessionDeliveryCore(p.ctx, AgentKeyID("zzc"), "opus50ts", "source-"+cancelled, cancelled)
			if result.State != "forwarded" {
				t.Fatalf("判据10：取消须经实际helper CAS确认: %+v", result)
			}
			p.waitState(cancelled, "cancelled", "判据10：received取消CAS获胜后Hub才可显示已取消")
			p.control("fault", map[string]any{"name": "pause_claim", "value": false})
			if p.snapshot(cancelled)[cancelled].State != "cancelled" || len(p.observations().Requests["R40_ACK_CANCEL"]) != 0 {
				t.Fatal("判据10：取消获胜后不得进入实际模型")
			}
			p.control("fault", map[string]any{"name": "pause_dispatch", "value": true})
			dispatched := "dw-dlv-r40-DISPATCH"
			p.enqueue(dispatched, "R40_ACK_DISPATCH")
			waitFor(t, 15*time.Second, "判据10：dispatch intent CAS先获胜", func() bool { _, err := os.Stat(filepath.Join(p.root, "dispatch-window.json")); return err == nil })
			p.waitState(dispatched, "delivered", "判据10：派发后尚未有模型回执")
			result = p.hub.cancelSessionDeliveryCore(p.ctx, AgentKeyID("zzc"), "opus50ts", "source-"+dispatched, dispatched)
			if result.State != "forwarded" {
				t.Fatalf("判据10：排队可取消位仅表示可发请求: %+v", result)
			}
			waitFor(t, 10*time.Second, "判据10：真实helper拒绝dispatch后的取消", func() bool {
				for _, ack := range p.nativeACKs() {
					if metaString(ack.Meta, "type") == "delivery_cancel_ack" && metaString(ack.Meta, "delivery_id") == dispatched && metaString(ack.Meta, "result") == "not_cancelable" {
						return true
					}
				}
				return false
			})
			if d := p.record(dispatched); d.State == "cancelled" {
				t.Fatalf("判据10：dispatch获胜不得假取消: %+v", d)
			}
			p.control("fault", map[string]any{"name": "pause_dispatch", "value": false})
			p.waitState(dispatched, "injected", "判据10：dispatch获胜后实际CLI正常接纳")
			p.control("release", map[string]any{"label": "R40_ACK_DISPATCH"})
			p.waitState(dispatched, "processed", "判据10：原回合完成且未回退queued/cancelled")
			if len(p.observations().Requests["R40_ACK_DISPATCH"]) != 1 {
				t.Fatal("判据10：取消竞态不得产生重复模型请求")
			}
			t.Log("判据10两向原子边界真实Hub/helper/CLI通过", kind)
		})
	}
}

func extractOldACKHelper(t *testing.T) string {
	t.Helper()
	const pin = "ec40752d6f059aa46b998b2a58965ad4bfc0607c"
	root := t.TempDir()
	files, err := exec.Command("git", "-C", "../..", "ls-tree", "-r", "--name-only", pin, "--", "tools/sessionhelper/sessionhelper").Output()
	if err != nil {
		t.Fatalf("INCONCLUSIVE: pinned old helper Git source unavailable: %v", err)
	}
	if len(strings.Fields(string(files))) == 0 {
		t.Fatal("INCONCLUSIVE: pinned helper source path empty")
	}
	for _, file := range strings.Fields(string(files)) {
		if !strings.HasSuffix(file, ".py") {
			continue
		}
		relative := strings.TrimPrefix(file, "tools/sessionhelper/")
		if relative == file || strings.Contains(relative, "..") {
			t.Fatal("invalid pinned helper path")
		}
		body, err := exec.Command("git", "-C", "../..", "show", pin+":"+file).Output()
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
func TestDeliveryACKActualCLILegacyUpgradeKeepsPerDeliveryProvenance(t *testing.T) {
	for _, kind := range []string{"claude", "codex"} {
		t.Run(kind, func(t *testing.T) {
			e := newAgentEnv(t)
			old := startRealACKProbe(t, e, kind, "", map[string]any{"package_root": extractOldACKHelper(t), "drop_legacy_acks": true})
			view := startRealACKView(t, old)
			id := "dw-dlv-r40-LEGACY"
			old.enqueue(id, "R40_ACK_LEGACY")
			old.waitState(id, "delivered", "判据9：旧helper收件，模型ACK受控丢失")
			waitFor(t, 25*time.Second, "判据9：旧helper的实际CLI进入模型", func() bool { return len(old.observations().Requests["R40_ACK_LEGACY"]) == 1 })
			old.control("release", map[string]any{"label": "R40_ACK_LEGACY"})
			waitFor(t, 25*time.Second, "判据9：旧helper真实回合结束且本地processed", func() bool {
				return old.observations().Terminals[id].TurnID != "" && old.snapshot(id)[id].State == "processed"
			})
			var late model.Envelope
			for _, ack := range old.nativeACKs() {
				if metaString(ack.Meta, "delivery_id") == id && metaString(ack.Meta, "state") == "processed" {
					late = ack
				}
			}
			if late.Meta == nil {
				t.Fatal("判据9：必须使用真实旧helper生成的processed ACK")
			}
			e.hub.mu.Lock()
			oldClient := e.hub.sessionClients[AgentKeyID("zzc")]["opus50ts"]
			e.hub.mu.Unlock()
			old.crash()
			upgraded := startRealACKProbe(t, e, kind, old.root)
			record := upgraded.waitState(id, "legacy_received", "判据9：升级后只重放legacy收件，不重新执行")
			// Replay the exact captured native frame as a pending old-connection read.
			// This is not a manufactured model ACK or a replacement for the real CLI.
			e.hub.handleSessionDeliveryAck(e.ctx, oldClient, late)
			record = upgraded.record(id)
			if record.AckProtocol != 1 || record.ProcessingMode != "legacy" || record.InjectedAt != nil || record.ProcessedAt != nil || record.TurnID != "" || len(upgraded.observations().Requests["R40_ACK_LEGACY"]) != 1 {
				t.Fatalf("判据9：旧件不得按新连接能力升级为v2执行/终态: %+v", record)
			}
			t.Cleanup(func() {
				if t.Failed() {
					t.Logf("legacy actual View: %+v", view.snapshot(id))
				}
			})
			waitFor(t, 5*time.Second, "判据9：实际View保持旧版helper已收、状态无法确认", func() bool {
				stage := view.snapshot(id).Stage
				return strings.Contains(stage, "旧版 helper 已收／状态无法确认") && !strings.Contains(stage, "回合已结束") && !strings.Contains(stage, "已注入执行")
			})
			fresh := "dw-dlv-r40-FRESH"
			upgraded.enqueue(fresh, "R40_ACK_FRESH")
			upgraded.waitState(fresh, "injected", "判据9：同一升级连接新件仍用v2真实admission")
			upgraded.control("release", map[string]any{"label": "R40_ACK_FRESH"})
			done := upgraded.waitState(fresh, "processed", "判据9：新v2件正常完成三阶段")
			if done.AckProtocol != 2 || done.TurnID == "" || done.TurnOutcome != "completed" {
				t.Fatalf("判据9：新件v2合同回归: %+v", done)
			}
			t.Log("判据9真实旧helper ec40752d→新helper升级/迟到原ACK/新件v2通过", kind)
		})
	}
}
