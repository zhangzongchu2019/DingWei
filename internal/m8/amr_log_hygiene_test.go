package m8

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"testing"
)

// S-AMR-LOG-HYGIENE 判据。派工单:runbooks/DISPATCH-20260929-S-AMR-LOG-HYGIENE.md(板 tasks/v0.1-B)。
//
// 两条方向相反的判据必须同时成立,缺一条这批就是白做:
//   ① 正侧:unsupported_name 这类真信号改后仍须【逐条】可检出 —— 对照素材是现网两条原文;
//   ② 反侧:missing_identity_or_replay 不再逐条打,但【计数不能丢】,心跳里的值须与事件数对上。
// 噪音量取现网实测窗口(95 分钟)的 16,724 条,让"万级噪音"在测试里真实出现,而不是用 3 条代表。

const amrHygieneNoise = 16_724

// 现网日志原文(FINDING-my-own-amr-gate-gave-false-safe-verdicts-20260929 §一,prodops 只读实取)。
// body 是能让产品解析出该 token 的最小回合正文;want 是现网那一行 decision 的逐字内容。
var amrHygieneProdSamples = []struct {
	name string
	body string
	want string
}{
	{
		name: "06:59:05 git SSH URL",
		body: "git clone git@git.code.tencent.com:wego-ai/dingwei.git /tmp/dingwei",
		want: "amr decision=token_skipped reason=unsupported_name tokens=[git.code.tencent.com:wego-ai/dingwei.git] source=zzc-opus50ts-2642",
	},
	{
		name: "07:09:16 回合正文",
		body: "「@提及信息发送不成功的，没有转到错误消息里吗？」——答：没有，…**\n后续正文",
		want: "amr decision=token_skipped reason=unsupported_name tokens=[提及信息发送不成功的，没有转到错误消息里吗？」——答：没有，…**] source=zzc-opus50ts-2642",
	},
}

func amrHygieneCaptureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev, flags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prev); log.SetFlags(flags) })
	return &buf
}

// 三种形状轮流:缺 delivery_id / 缺 turn_id / 重放 —— 第一道门的三个入口都要覆盖到。
func amrHygieneIngestNoise(t *testing.T, p *amrProcessor, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		ev := amrSessionEvent{Kind: "assistant_text", Text: "noise"}
		switch i % 3 {
		case 0:
			ev.TurnID = "t"
		case 1:
			ev.DeliveryID = "d"
		default:
			ev.DeliveryID, ev.TurnID, ev.Data = "d", "t", map[string]any{"replay": true}
		}
		p.ingest("key", "zzc-opus50ts-2642", amrEventJSON(t, ev))
	}
}

// 走真实链路 ingest → 终态 → 出队 → process,不直接调解析函数。
func amrHygieneRunTurn(t *testing.T, p *amrProcessor, id, body string) {
	t.Helper()
	p.ingest("key", "zzc-opus50ts-2642", amrEventJSON(t, amrSessionEvent{Kind: "assistant_text", Text: body, DeliveryID: id, TurnID: "t"}))
	p.ingest("key", "zzc-opus50ts-2642", amrEventJSON(t, amrSessionEvent{Kind: "state_change", Data: map[string]any{"change": "turn_completed", "status": "completed"}, DeliveryID: id, TurnID: "t"}))
	select {
	case job := <-p.queue:
		p.process(job)
	default:
		t.Fatalf("回合 %s 没有进入路由队列", id)
	}
}

func amrHygieneDecisionLines(buf *bytes.Buffer) []string {
	var out []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "amr decision=") {
			out = append(out, line)
		}
	}
	return out
}

// 正侧与信噪比共用的现场:万级噪音夹在两条现网原文回合之间,走真实链路。
func amrHygieneRunProdWindow(t *testing.T) []string {
	t.Helper()
	buf := amrHygieneCaptureLog(t)
	p := testAMRProcessor()
	amrHygieneIngestNoise(t, p, amrHygieneNoise/2)
	amrHygieneRunTurn(t, p, "d-sample-0", amrHygieneProdSamples[0].body)
	amrHygieneIngestNoise(t, p, amrHygieneNoise-amrHygieneNoise/2)
	amrHygieneRunTurn(t, p, "d-sample-1", amrHygieneProdSamples[1].body)
	return amrHygieneDecisionLines(buf)
}

// ① 正侧:两条现网原文仍须各自逐条、逐字可检出。
// 🔵 本格与改法无关 —— 反向变异(改回逐条打)下它应【仍绿】,证明改动没有碰到真信号那一支。
func TestAMRLogHygieneUnsupportedNameStillLoggedPerLine(t *testing.T) {
	decisions := amrHygieneRunProdWindow(t)
	for _, s := range amrHygieneProdSamples {
		hits := 0
		for _, line := range decisions {
			if line == s.want {
				hits++
			}
		}
		if hits != 1 {
			t.Errorf("① 正侧 %s:现网原文那一行须逐字出现恰好 1 次,实得 %d 次;want=%q", s.name, hits, s.want)
		}
	}
}

// ①b 信噪比:同一窗口里 amr decision= 只剩真信号。
// 🔴 反向变异下本格应红,且打出的实得行数回到万级 —— 证明这一批确实改变了量,而不只是改了措辞。
func TestAMRLogHygieneDecisionPrefixOnlyRealSignal(t *testing.T) {
	decisions := amrHygieneRunProdWindow(t)
	if len(decisions) != len(amrHygieneProdSamples) {
		t.Errorf("①b 信噪比:%d 条噪音事件之后,amr decision= 行应只剩 %d 条真信号,实得 %d 条(信噪比 %d:%d)",
			amrHygieneNoise, len(amrHygieneProdSamples), len(decisions), len(amrHygieneProdSamples), len(decisions)-len(amrHygieneProdSamples))
	}
}

// ② 反侧:噪音不再逐条打,但计数一条不丢,且心跳里的数与事件数逐项对上。
func TestAMRLogHygieneMissingIdentityCountedNotLogged(t *testing.T) {
	buf := amrHygieneCaptureLog(t)
	p := testAMRProcessor()

	amrHygieneIngestNoise(t, p, amrHygieneNoise)

	if lines := amrHygieneDecisionLines(buf); len(lines) != 0 {
		t.Errorf("② 反侧:missing_identity_or_replay 仍在逐条打 amr decision=,实得 %d 行(want 0);首行=%q", len(lines), lines[0])
	}
	stats, _ := p.snapshotStats()
	if stats.missingIdentityOrReplay != amrHygieneNoise {
		t.Errorf("② 反侧:计数丢了 —— missingIdentityOrReplay=%d,want %d(把'看不见'换成了'数不清')", stats.missingIdentityOrReplay, amrHygieneNoise)
	}
	if stats.eventsRejected != amrHygieneNoise || stats.eventsObserved != amrHygieneNoise {
		t.Errorf("② 反侧:既有计数被改变 —— eventsRejected=%d eventsObserved=%d,want 各 %d", stats.eventsRejected, stats.eventsObserved, amrHygieneNoise)
	}

	buf.Reset()
	p.mu.Lock()
	p.logStatsLocked()
	p.mu.Unlock()
	hb := buf.String()
	for _, field := range []string{
		fmt.Sprintf(" events=%d ", amrHygieneNoise),
		fmt.Sprintf(" rejected_events=%d ", amrHygieneNoise),
		fmt.Sprintf(" missing_identity_or_replay=%d ", amrHygieneNoise),
	} {
		if !strings.Contains(hb, field) {
			t.Errorf("② 反侧:心跳里对不上 %q;心跳=%q", strings.TrimSpace(field), strings.TrimSpace(hb))
		}
	}
}

// 边界:只降这一支。invalid_json 是罕见的真异常,仍须逐条打(防止"顺手把别的也关了")。
func TestAMRLogHygieneInvalidJSONStillLogged(t *testing.T) {
	buf := amrHygieneCaptureLog(t)
	p := testAMRProcessor()
	p.ingest("key", "zzc-opus50ts-2642", "{not json")
	if !strings.Contains(buf.String(), "amr decision=failed reason=invalid_json") {
		t.Errorf("边界:invalid_json 不在本单范围内,仍须逐条打;日志=%q", buf.String())
	}
	stats, _ := p.snapshotStats()
	if stats.eventsRejected != 1 || stats.missingIdentityOrReplay != 0 {
		t.Errorf("边界:invalid_json 只应计入 eventsRejected;eventsRejected=%d missingIdentityOrReplay=%d", stats.eventsRejected, stats.missingIdentityOrReplay)
	}
}
