package m8

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/zhangzongchu2019/dingwei/internal/model"
	dwversion "github.com/zhangzongchu2019/dingwei/internal/version"
)

const (
	ipcBodyAMRMetaKey = "ipc_body_amr_v1"
	amrRouteBodyMax   = 100_000
)

func (h *Hub) routeIPCSendBodyAMR(ctx context.Context, c *sessionClient, original model.Envelope) {
	if h == nil || c == nil {
		return
	}
	maxTargets := h.ipcBodyAMRMaxTargets()
	candidates, lookedLikeCandidate := parseAMRCandidatesDetailed(original.Body, c.sessionName, maxTargets)
	feedback := amrParseFeedback(original.Body, candidates, lookedLikeCandidate, maxTargets)
	if feedback != "" && h.amr != nil {
		h.amr.notifySourceVisible(amrTurnKey{KeyID: c.keyID, Session: c.sessionName, DeliveryID: original.ID, TurnID: "ipc-send"}, "ipc_syntax_notice", feedback, lookedLikeCandidate, original.Body)
	}
	canonicalOriginalTo := original.To
	if to, err := parseAddress(original.To); err == nil && to.Kind == addressSession {
		probe := model.Envelope{To: "#" + to.SessionName, From: original.From}
		if handled, prepared, err := h.prepareSessionSelector(ctx, c.keyID, c.sessionName, probe); err == nil && handled {
			canonicalOriginalTo = prepared.To
		}
	}
	seen := map[string]bool{canonicalOriginalTo: true}
	for i, candidate := range candidates {
		// S-MENTION-BESTEFFORT ③:投给对应方的正文保持发送方写下的原样(含提及字样与提及前的文字),
		// 不再投 candidate.body(那是剥掉提及、且丢了前文的一段)。
		env := model.Envelope{
			ID:   amrDerivedEnvelopeID(amrTurnKey{KeyID: c.keyID, Session: c.sessionName, DeliveryID: original.ID, TurnID: "ipc-send"}, i, candidate.selector),
			To:   candidate.selector,
			From: original.From,
			Body: original.Body,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"type":               "agent_route",
				"no_mirror":          true,
				"reply_policy":       candidate.replyPolicy,
				"source_delivery_id": original.ID,
				"route_source":       "ipc_send_body_amr",
			},
		}
		env.Meta[metaKeyMentionVerbatim] = true
		handled, prepared, err := h.prepareSessionSelector(ctx, c.keyID, c.sessionName, env)
		if err != nil || !handled {
			h.writeDeliveryFailure(ctx, c, original, candidate.selector, errors.New(firstNonEmpty(errorString(err), "目标无法解析")))
			continue
		}
		if seen[prepared.To] {
			continue
		}
		seen[prepared.To] = true
		if err := h.RouteEnvelope(ctx, prepared); err != nil {
			h.writeDeliveryFailure(ctx, c, original, candidate.selector, err)
		}
	}
}

func (h *Hub) ipcBodyAMRMaxTargets() int {
	if h != nil && h.amr != nil && h.amr.maxTargets > 0 {
		return h.amr.maxTargets
	}
	return 20
}

type amrSessionEvent struct {
	Kind       string         `json:"kind"`
	Text       string         `json:"text"`
	Data       map[string]any `json:"data"`
	Cursor     string         `json:"cursor"`
	DeliveryID string         `json:"delivery_id"`
	TurnID     string         `json:"turn_id"`
}

type amrTurnKey struct {
	KeyID      string
	Session    string
	DeliveryID string
	TurnID     string
}

type amrTurn struct {
	text               string
	updated            time.Time
	started            time.Time
	ttl                time.Duration
	observed           int
	truncated          bool
	systemNotification bool
}

type amrRouteJob struct {
	key                amrTurnKey
	body               string
	systemNotification bool
}

type amrNoticeJob struct {
	key     amrTurnKey
	target  string
	reason  string
	failure bool
}

type amrTargetResult struct {
	selector string
	// resolved 是**实际解析到的目标地址**,与 selector 可能不同。
	// 🔴 T-071:此前通知只回显 selector,于是 `@zzc` 被模糊匹配到 coder 之后,
	// 发信方看到的是「#zzc:成功」—— 投错了人,而两侧都无从察觉。
	// 「系统替你做了选择」必须可见,哪怕那个选择是对的。
	resolved string
	ok       bool
	err      error
}

type amrPreparedTarget struct {
	candidate amrCandidate
	envelope  model.Envelope
}

// amrStats is protected by amrProcessor.mu. Top-level states are mutually
// exclusive: opened = active + queued + closed + incomplete + failed.
// closed is independently partitioned by the four closed* fields.
type amrStats struct {
	eventsObserved          uint64
	opened                  uint64
	queued                  uint64
	closed                  uint64
	incomplete              uint64
	failed                  uint64
	eventsRejected          uint64
	missingIdentityOrReplay uint64 // S-AMR-LOG-HYGIENE:见 ingest 第一道门
	closedNonCandidate      uint64
	closedCandidateRejected uint64
	closedCandidateReady    uint64
	closedDispatched        uint64
	fragmentDuplicate       uint64
	fragmentSnapshot        uint64
	fragmentAppend          uint64
	legacyRouteDropped      uint64
	lateAfterTerminal       uint64
	targetsAttempted        uint64
	targetsDispatched       uint64
	targetsFailed           uint64
	queueUnderflow          uint64
	notificationsSent       uint64
	notificationsSuppressed uint64
	// 回程段 2(reply_to_origin.go)的三个计数,进心跳日志 ——
	// 🔴 组长 C1:「回不去」必须有可见表现。日志是一次性的,计数是可被持续观察的。
	replyOriginRouted      uint64
	replyOriginSkipped     uint64
	replyOriginUnavailable uint64
	terminalUnknown        map[string]uint64
	workerHeartbeat        time.Time
}

type amrProcessor struct {
	// stop 关闭即令 run() 退出（Hub.Close 调用）。此前 run() 无任何退出分支 ⇒ 每建一个 Hub 永久泄漏一个心跳 goroutine，
	// 每分钟往全局 log 打一行 amr heartbeat，随机撞进日志卫生用例的捕获窗口（复验方 qwen38maxkhovvob4 定位到 amr.go:597）。
	stop     chan struct{}
	stopOnce sync.Once
	// done 在 run() 返回时关闭；close() 等它，保证 Close 返回后不会再有一行心跳落进别人的日志窗口。
	done          chan struct{}
	hub           *Hub
	queue         chan amrRouteJob
	noticeQueue   chan amrNoticeJob
	mu            sync.Mutex
	turns         map[amrTurnKey]*amrTurn
	completed     map[amrTurnKey]time.Time
	notices       map[string]time.Time
	stats         amrStats
	maxTurns      int
	maxTurnAge    time.Duration
	maxBytes      int
	routeBodyMax  int
	maxCompleted  int
	routeEnvelope func(context.Context, model.Envelope) error
	turnTTL       time.Duration
	routeTimeout  time.Duration
	maxTargets    int
}

func newAMRProcessor(h *Hub) *amrProcessor {
	p := &amrProcessor{
		hub:          h,
		queue:        make(chan amrRouteJob, envInt("WP_AMR_QUEUE_CAP", 128)),
		noticeQueue:  make(chan amrNoticeJob, envInt("WP_AMR_NOTICE_QUEUE_CAP", 128)),
		turns:        map[amrTurnKey]*amrTurn{},
		completed:    map[amrTurnKey]time.Time{},
		notices:      map[string]time.Time{},
		maxTurns:     envInt("WP_AMR_MAX_TURNS_PER_SESSION", 16),
		maxTurnAge:   envDuration("WP_AMR_MAX_TURN_AGE", 6*time.Hour),
		maxBytes:     envInt("WP_AMR_MAX_TURN_BYTES", 256<<10),
		routeBodyMax: amrRouteBodyMax,
		maxCompleted: envInt("WP_AMR_MAX_COMPLETED_PER_SESSION", 64),
		turnTTL:      envDuration("WP_AMR_TURN_TTL", 5*time.Minute),
		routeTimeout: envDuration("WP_AMR_ROUTE_TIMEOUT", 5*time.Second),
		maxTargets:   envInt("WP_AMR_MAX_TARGETS", 20),
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
	}
	p.stats.terminalUnknown = map[string]uint64{}
	p.routeEnvelope = h.RouteEnvelope
	log.Printf("amr startup mode=route_all structured_failure_min_version=%s", amrStructuredFailureMinVersion())
	go p.run()
	return p
}

func envInt(name string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

func (p *amrProcessor) shouldDropLegacyAgentRoute(keyID, session string, env model.Envelope) bool {
	drop := p != nil && metaString(env.Meta, "type") == "agent_route" && metaBool(env.Meta, "no_mirror")
	if drop {
		p.mu.Lock()
		p.stats.legacyRouteDropped++
		p.mu.Unlock()
	}
	return drop
}

func (p *amrProcessor) ingest(keyID, sessionName, raw string) {
	var ev amrSessionEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		p.transitionFailed("invalid_json")
		return
	}
	p.mu.Lock()
	p.stats.eventsObserved++
	p.mu.Unlock()
	if ev.DeliveryID == "" || ev.TurnID == "" || amrEventReplay(ev.Data) {
		// S-AMR-LOG-HYGIENE:这一支是预期内的常态(多数会话事件本就不带回合标识)。
		// 原先经 transitionFailed 逐条打 `amr decision=failed`,现网 95 分钟 16,724 行、占 99.94%,
		// 把真信号(unsupported_name 当窗只有 2 条)埋了约 8,000 倍。
		// 现改为:与 transitionFailed 同样计入 eventsRejected,另记专用计数,**不逐条打日志**;
		// 量由心跳里的 missing_identity_or_replay= 承载。判定(丢弃该事件)不变。
		// 🔴 计数不能丢:它是这类事件唯一剩下的可见面 —— 把"看不见"换成"数不清"就白做了。
		p.mu.Lock()
		p.stats.eventsRejected++
		p.stats.missingIdentityOrReplay++
		p.mu.Unlock()
		return
	}
	key := amrTurnKey{KeyID: keyID, Session: sessionName, DeliveryID: ev.DeliveryID, TurnID: ev.TurnID}
	now := time.Now().UTC()

	p.mu.Lock()
	if _, done := p.completed[key]; done {
		// F8/T-040:该分支对**所有**落在已完成回合上的事件都打同一行日志,但只有
		// assistant_text 且正文非空的那部分才计入 late_after_terminal。原日志不带任何归因字段,
		// 于是心跳里的 late_after_terminal=3 与日志里的 9 条 ignored 无法对上 ——
		// G2-2 因此长期 NOT_COVERED:不是没数,是数与证据挂不上钩,而拼凑猜测不算验证。
		// counted_late 就是那个判别位;delivery/turn/kind 用于定位到具体回合。
		// 只记 text_len 不记正文:正文可能含凭据/隐私,日志不是它该去的地方。
		late := ev.Kind == "assistant_text" && ev.Text != ""
		if late {
			p.stats.lateAfterTerminal++
		}
		p.mu.Unlock()
		log.Printf("amr decision=ignored reason=completed_turn counted_late=%t session=%s delivery_id=%s turn_id=%s kind=%s text_len=%d",
			late, sessionName, ev.DeliveryID, ev.TurnID, ev.Kind, len(ev.Text))
		return
	}
	t := p.turns[key]
	if t == nil {
		p.evictOldestForSessionLocked(keyID, sessionName, now)
		if p.activeForSessionLocked(keyID, sessionName) >= p.maxTurns {
			p.stats.eventsRejected++
			p.mu.Unlock()
			log.Printf("amr decision=failed reason=turn_capacity")
			return
		}
		t = &amrTurn{updated: now, started: now, ttl: p.turnTTLFor(keyID, sessionName)}
		p.turns[key] = t
		p.stats.opened++
	}
	t.updated = now
	// The helper copies this Hub-authored semantic marker from the inbound
	// envelope into the admitted lifecycle event. It deliberately does not
	// infer notification origin from the synthetic delivery-id prefix.
	if metaBool(ev.Data, "system_notification") {
		t.systemNotification = true
	}
	if ev.Kind == "assistant_text" && ev.Text != "" {
		switch {
		case t.text == "":
			t.observed = len(ev.Text)
			t.text, t.truncated = amrKeepHead(ev.Text, p.routeBodyMax, t.truncated)
			p.stats.fragmentAppend++
		case ev.Text == t.text:
			p.stats.fragmentDuplicate++
		case strings.HasPrefix(ev.Text, t.text):
			if len(ev.Text) > t.observed {
				t.observed = len(ev.Text)
			}
			t.text, t.truncated = amrKeepHead(ev.Text, p.routeBodyMax, t.truncated)
			p.stats.fragmentSnapshot++
		default:
			t.observed += len(ev.Text)
			t.text, t.truncated = amrKeepHead(t.text+ev.Text, p.routeBodyMax, t.truncated)
			p.stats.fragmentAppend++
		}
		if t.observed > p.maxBytes {
			t.truncated = true
		}
	}
	if !amrTerminalEvent(ev) {
		p.mu.Unlock()
		return
	}
	body := t.text
	succeeded, unknownTerminal := amrTerminalOutcome(ev.Data)
	if unknownTerminal != "" {
		p.countUnknownTerminalLocked(unknownTerminal)
	}
	delete(p.turns, key)
	p.completed[key] = now
	p.capCompletedLocked()
	if !succeeded {
		p.stats.failed++
		p.mu.Unlock()
		log.Printf("amr decision=failed reason=terminal_failed")
		p.notifySource(key, "", "终态表示本轮未成功", true)
		return
	}
	if body == "" {
		p.stats.incomplete++
		p.mu.Unlock()
		log.Printf("amr decision=incomplete reason=empty")
		p.notifySource(key, "", "本轮没有可路由的完整文本", true)
		return
	}
	if t.truncated {
		body = amrTruncatedBody(body, p.routeBodyMax)
	}
	select {
	case p.queue <- amrRouteJob{key: key, body: body, systemNotification: t.systemNotification}:
		p.stats.queued++
		p.mu.Unlock()
	default:
		p.stats.failed++
		p.mu.Unlock()
		log.Printf("amr decision=failed reason=queue_full")
		p.notifySource(key, "", "Hub 路由队列已满", true)
	}
}

func (p *amrProcessor) transitionFailed(reason string) {
	p.mu.Lock()
	p.stats.eventsRejected++
	p.mu.Unlock()
	log.Printf("amr decision=failed reason=%s", reason)
}

func amrEventReplay(data map[string]any) bool {
	v, _ := data["replay"].(bool)
	return v
}

func amrTerminalEvent(ev amrSessionEvent) bool {
	return ev.Kind == "state_change" && metaString(ev.Data, "change") == "turn_completed"
}

func amrTerminalSucceeded(data map[string]any) bool {
	succeeded, _ := amrTerminalOutcome(data)
	return succeeded
}

func amrTerminalOutcome(data map[string]any) (bool, string) {
	if failed, _ := data["is_error"].(bool); failed {
		return false, ""
	}
	if subtype := strings.ToLower(strings.TrimSpace(metaString(data, "subtype"))); subtype != "" {
		if subtype == "success" {
			return true, ""
		}
		if subtype == "error_during_execution" {
			return false, ""
		}
		return false, "subtype=" + amrMetricValue(subtype)
	}
	if status := strings.ToLower(strings.TrimSpace(metaString(data, "status"))); status != "" {
		switch status {
		case "completed", "complete", "success", "succeeded", "ok":
			return true, ""
		case "failed", "cancelled", "canceled", "interrupted", "error":
			return false, ""
		default:
			return false, "status=" + amrMetricValue(status)
		}
	}
	if outcome := strings.ToLower(strings.TrimSpace(metaString(data, "outcome"))); outcome != "" {
		switch outcome {
		case "completed", "complete", "success", "succeeded", "ok":
			return true, ""
		case "failed", "cancelled", "canceled", "interrupted", "error":
			return false, ""
		default:
			return false, "outcome=" + amrMetricValue(outcome)
		}
	}
	return false, "missing"
}

func amrMetricValue(value string) string {
	value = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, value)
	if len(value) > 48 {
		value = value[:48]
	}
	return value
}

func (p *amrProcessor) countUnknownTerminalLocked(label string) {
	if p.stats.terminalUnknown == nil {
		p.stats.terminalUnknown = map[string]uint64{}
	}
	if _, exists := p.stats.terminalUnknown[label]; !exists && len(p.stats.terminalUnknown) >= 32 {
		label = "other"
	}
	p.stats.terminalUnknown[label]++
}

func amrKeepHead(text string, limit int, alreadyTruncated bool) (string, bool) {
	if limit <= 0 || len(text) <= limit {
		return text, alreadyTruncated
	}
	end := limit
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end], true
}

func amrTruncatedBody(body string, limit int) string {
	const marker = "\n\n【Hub：原回复超过路由上限；仅转送开头 100KB】"
	body, _ = amrKeepHead(body, limit-len(marker), true)
	return body + marker
}

func (p *amrProcessor) activeForSessionLocked(keyID, session string) int {
	n := 0
	for key := range p.turns {
		if key.KeyID == keyID && key.Session == session {
			n++
		}
	}
	return n
}

func (p *amrProcessor) evictOldestForSessionLocked(keyID, session string, now time.Time) {
	if p.activeForSessionLocked(keyID, session) < p.maxTurns {
		return
	}
	var oldestKey amrTurnKey
	var oldestAt time.Time
	found := false
	for key, turn := range p.turns {
		if key.KeyID != keyID || key.Session != session {
			continue
		}
		if !found || turn.updated.Before(oldestAt) {
			oldestKey, oldestAt, found = key, turn.updated, true
		}
	}
	if found {
		delete(p.turns, oldestKey)
		p.completed[oldestKey] = now
		p.capCompletedLocked()
		p.stats.incomplete++
		log.Printf("amr decision=incomplete reason=session_capacity_eviction")
		p.queueNoticeLocked(amrNoticeJob{key: oldestKey, target: "", reason: "本会话未完成轮次达到配额，最旧轮次已淘汰", failure: true})
	}
}

func (p *amrProcessor) turnTTLFor(keyID, session string) time.Duration {
	if p.hub == nil {
		return p.turnTTL
	}
	p.hub.mu.Lock()
	defer p.hub.mu.Unlock()
	if sessions := p.hub.sessionClients[keyID]; sessions != nil {
		if c := sessions[session]; c != nil && strings.EqualFold(c.tool, "opencode") {
			return time.Minute
		}
	}
	return p.turnTTL
}

// sourceStillConnectedLocked 报告该回合的来源会话此刻是否仍连着 Hub。
// 调用方持 p.mu;此处再取 p.hub.mu —— 与 turnTTLFor 同一顺序(p.mu -> hub.mu),不引入新的加锁次序。
func (p *amrProcessor) sourceStillConnectedLocked(key amrTurnKey) bool {
	if p.hub == nil {
		return false
	}
	p.hub.mu.Lock()
	defer p.hub.mu.Unlock()
	sessions := p.hub.sessionClients[key.KeyID]
	return sessions != nil && sessions[key.Session] != nil
}

func (p *amrProcessor) expireLocked(now time.Time) {
	for key, turn := range p.turns {
		if now.Sub(turn.updated) > turn.ttl {
			// 🔴 T-046(业主 2026-09-10:未解决就不随本版本发布):
			// 原实现只看「安静了多久」。而 turn.updated 每收事件刷新,
			// 一个正在等长命令/等外部响应/等人的回合天然会安静数分钟到数小时 ——
			// 那是 CLI Agent 的常态,不是异常。按安静时长丢弃,等于把常态判成死亡:
			// 该回合里的 @ 提及**永不派送**,发送方只在 TTL 后收到一条失败通知。
			// prodops 实测当日出现 6 次;我们三席都是这种形态。
			//
			// TTL 原本的用途是防止死回合占内存。但内存已由另外两道独立机制兜住:
			// 每会话 maxTurns(默认 16,含 evictOldestForSessionLocked)与每回合 maxBytes(默认 256KiB)。
			// ⇒ 安静本身不再构成丢弃理由。只有两种情况才丢:
			//   ① 来源会话已经断开 —— 该回合确实不会再有终态了
			//   ② 超过绝对年龄上限 maxTurnAge(默认 6h)—— 兜底,防止连接假活导致永不回收
			// 这样既保住了「长时间安静的回合仍能派送」,又没有把内存边界交给运气。
			if p.sourceStillConnectedLocked(key) && now.Sub(turn.started) < p.maxTurnAge {
				continue
			}
			delete(p.turns, key)
			p.completed[key] = now
			p.stats.incomplete++
			log.Printf("amr decision=incomplete reason=turn_ttl")
			p.queueNoticeLocked(amrNoticeJob{key: key, target: "", reason: "本轮未收到可靠终态，已按 TTL 关闭且不会派送", failure: true})
		}
	}
	for key, at := range p.completed {
		if now.Sub(at) > p.turnTTL {
			delete(p.completed, key)
		}
	}
	p.capCompletedLocked()
}

func (p *amrProcessor) capCompletedLocked() {
	for {
		counts := map[string]int{}
		for key := range p.completed {
			counts[key.KeyID+"\x00"+key.Session]++
		}
		var over string
		for session, count := range counts {
			if count > p.maxCompleted {
				over = session
				break
			}
		}
		if over == "" {
			return
		}
		var oldestKey amrTurnKey
		var oldestAt time.Time
		found := false
		for key, at := range p.completed {
			if key.KeyID+"\x00"+key.Session == over && (!found || at.Before(oldestAt)) {
				oldestKey, oldestAt, found = key, at, true
			}
		}
		if found {
			delete(p.completed, oldestKey)
		}
	}
}

func (p *amrProcessor) queueNoticeLocked(notice amrNoticeJob) {
	select {
	case p.noticeQueue <- notice:
	default:
		p.stats.notificationsSuppressed++
		log.Printf("amr notification=suppressed reason=notice_queue_full")
	}
}

func (p *amrProcessor) run() {
	interval := p.turnTTL / 2
	if interval <= 0 || interval > time.Minute {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	if p.done != nil {
		defer close(p.done)
	}
	for {
		select {
		case <-p.stop:
			return
		case job := <-p.queue:
			p.process(job)
		case notice := <-p.noticeQueue:
			p.notifySource(notice.key, notice.target, notice.reason, notice.failure)
		case now := <-ticker.C:
			p.mu.Lock()
			p.stats.workerHeartbeat = now.UTC()
			p.expireLocked(now.UTC())
			p.logStatsLocked()
			p.mu.Unlock()
		}
	}
}

// close 令 run() 退出并等它真正返回；幂等。p 或 stop 为空（测试里直接字面量构造、未经 newAMRProcessor）时什么也不做。
func (p *amrProcessor) close() {
	if p == nil || p.stop == nil {
		return
	}
	p.stopOnce.Do(func() { close(p.stop) })
	if p.done != nil {
		<-p.done
	}
}

func (p *amrProcessor) logStatsLocked() {
	active := uint64(len(p.turns))
	topOK := p.stats.opened == active+p.stats.queued+p.stats.closed+p.stats.incomplete+p.stats.failed
	closedOK := p.stats.closed == p.stats.closedNonCandidate+p.stats.closedCandidateRejected+p.stats.closedCandidateReady+p.stats.closedDispatched
	log.Printf("amr heartbeat events=%d opened=%d active=%d queued=%d closed=%d incomplete=%d failed=%d non_candidate=%d candidate_rejected=%d candidate_ready=%d dispatched=%d fragment_append=%d fragment_snapshot=%d fragment_dup=%d late_after_terminal=%d legacy_dropped=%d targets_attempted=%d targets_dispatched=%d targets_failed=%d queue_underflow=%d notifications_sent=%d notifications_suppressed=%d reply_origin_routed=%d reply_origin_skipped=%d reply_origin_unavailable=%d terminal_unknown=%v structured_failure_min_version=%s rejected_events=%d missing_identity_or_replay=%d top_invariant=%t closed_invariant=%t",
		p.stats.eventsObserved, p.stats.opened, active, p.stats.queued, p.stats.closed, p.stats.incomplete, p.stats.failed,
		p.stats.closedNonCandidate, p.stats.closedCandidateRejected, p.stats.closedCandidateReady, p.stats.closedDispatched,
		p.stats.fragmentAppend, p.stats.fragmentSnapshot, p.stats.fragmentDuplicate, p.stats.lateAfterTerminal, p.stats.legacyRouteDropped,
		p.stats.targetsAttempted, p.stats.targetsDispatched, p.stats.targetsFailed, p.stats.queueUnderflow,
		p.stats.notificationsSent, p.stats.notificationsSuppressed,
		p.stats.replyOriginRouted, p.stats.replyOriginSkipped, p.stats.replyOriginUnavailable,
		p.stats.terminalUnknown, amrStructuredFailureMinVersion(), p.stats.eventsRejected, p.stats.missingIdentityOrReplay,
		topOK, closedOK)
}

func (p *amrProcessor) process(job amrRouteJob) {
	p.mu.Lock()
	if p.stats.queued > 0 {
		p.stats.queued--
	} else {
		p.stats.queueUnderflow++
	}
	p.stats.workerHeartbeat = time.Now().UTC()
	p.mu.Unlock()

	candidates, lookedLikeCandidate := parseAMRCandidatesDetailed(job.body, job.key.Session, p.maxTargets)
	feedback := amrParseFeedback(job.body, candidates, lookedLikeCandidate, p.maxTargets)
	// 🔴 G2-b:渠道候选走渠道投递,不进 Agent 寻址(prepareSessionSelector 解不了它们)。
	// 分流放在这里而不是更下面:下面每一步都假设 selector 是会话选择器。
	if channels := filterChannelCandidates(candidates); len(channels) > 0 {
		for _, candidate := range channels {
			if err := p.deliverChannelCandidate(job, candidate); err != nil {
				log.Printf("[channel] decision=failed session=%s channel=%s err=%v", job.key.Session, candidate.channel, err)
			}
		}
		if feedback != "" {
			p.notifySourceVisible(job.key, "route_notice", feedback, false, job.body)
		}
		p.closeJob("dispatched")
		return
	}
	if len(candidates) == 0 {
		if lookedLikeCandidate {
			p.closeJob("candidate_rejected")
			reason := firstNonEmpty(feedback, "提及格式正确但目标无权限、不可达或超过数量限制")
			p.notifySourceVisible(job.key, amrOffendingToken(job.body), reason, true, job.body)
		} else {
			p.closeJob("non_candidate")
			if feedback != "" {
				p.notifySourceVisible(job.key, "syntax_notice", feedback, false, job.body)
			}
			// 🔴 回程段 2:本轮有输出、但正文里没有任何 @提及目标 ——
			//    业主要的「其内容本身应该回给 A」正是这一支。实现见 reply_to_origin.go。
			p.replyToOrigin(job)
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.routeTimeout)
	defer cancel()
	prepared := make([]amrPreparedTarget, 0, len(candidates))
	results := make([]amrTargetResult, 0, len(candidates))
	// S-MENTION-BESTEFFORT ③ 的连锁处置(原样送达后,收件方引用原文会让提及再被解析):
	//   ① 解析后指向本席自己的目标一律跳过 —— 原文里的短名(如 bee)与本席规范名(owner-bee-xxxx)
	//      在解析层比不上,原先会投给自己;引用一次就分叉成"自己＋对方"两封。
	//   ② 派生信封继承触发本轮那封入站的 hop_count / max_hops(照抄 reply_to_origin.go),
	//      否则每跳从 0 起算,RouteEnvelope 里的 R4 兜底永远够不着。
	//   ③ 一跳封顶(组长 2026-09-29 要求的第二条上限):触发本轮的入站若本身就是"原样送达"派生信封
	//      (meta 带 mention_verbatim)⇒ 本轮不再按提及路由,只走普通回程。标记在 meta 不在正文,
	//      所以不会被原样带进下一跳、也就不会被引用。它比 ② 的 R4(16 跳)紧,两者并存时链停在这一条上。
	//   ②③ 共用同一次入站查询。该查询以"回合事件的 delivery_id == session_delivery 主键"为前提:
	//      Hub 对 reliable 信封且对端自报 delivery_ack_v1 时成立(delivery_id.go 契约;判据见
	//      TestAMRVerbatimTurnDeliveryIDMatchesSessionDelivery)。查不到时打一行可见日志,不再静默。
	self := sessionAddress(job.key.Session, job.key.KeyID)
	inbound, inboundFound := p.inboundOf(ctx, job.key.DeliveryID)
	if inboundFound && metaBool(inbound.Meta, metaKeyMentionVerbatim) {
		log.Printf("amr decision=routing_suppressed reason=verbatim_inbound session=%s delivery_id=%s candidates=%d", job.key.Session, job.key.DeliveryID, len(candidates))
		p.closeJob("non_candidate")
		p.replyToOrigin(job)
		return
	}
	if !inboundFound && job.key.DeliveryID != "" {
		log.Printf("amr decision=inbound_unavailable session=%s delivery_id=%s candidates=%d hop_inherit=0", job.key.Session, job.key.DeliveryID, len(candidates))
	}
	inboundHop, inboundMaxHops := hopCountOf(inbound), metaInt(inbound.Meta, metaKeyMaxHops)
	selfSkipped := 0
	for i, candidate := range candidates {
		// S-MENTION-BESTEFFORT ③:原样送达,理由同 routeIPCSendBodyAMR。
		env := model.Envelope{
			ID:   amrDerivedEnvelopeID(job.key, i, candidate.selector),
			To:   candidate.selector,
			Body: job.body,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"type":               "agent_route",
				"no_mirror":          true,
				"reply_policy":       candidate.replyPolicy,
				"source_delivery_id": job.key.DeliveryID,
				"source_turn_id":     job.key.TurnID,
			},
		}
		env.Meta[metaKeyMentionVerbatim] = true
		handled, resolved, err := p.hub.prepareSessionSelector(ctx, job.key.KeyID, job.key.Session, env)
		if err != nil || !handled {
			log.Printf("amr decision=candidate_rejected handled=%t err=%v", handled, err)
			results = append(results, amrTargetResult{selector: candidate.selector, err: errors.New(firstNonEmpty(errorString(err), "目标无法解析"))})
			continue
		}
		if resolved.To == self {
			selfSkipped++
			log.Printf("amr decision=self_skipped session=%s selector=%s", job.key.Session, candidate.selector)
			continue
		}
		if resolved.Meta == nil {
			resolved.Meta = map[string]any{}
		}
		resolved.Meta[metaKeyHopCount] = inboundHop
		if inboundMaxHops > 0 {
			resolved.Meta[metaKeyMaxHops] = inboundMaxHops
		}
		prepared = append(prepared, amrPreparedTarget{candidate: candidate, envelope: resolved})
	}
	if len(prepared) == 0 && len(results) == 0 && selfSkipped > 0 {
		// 提及全是本席自己(典型:引用了别人发给本席的原文)⇒ 不是投递指令,按普通输出回程,
		// 否则这一轮的回复会被当成"候选全被拒"吞掉。
		p.closeJob("non_candidate")
		p.replyToOrigin(job)
		return
	}
	if len(prepared) == 0 {
		p.mu.Lock()
		p.stats.targetsAttempted += uint64(len(results))
		p.stats.targetsFailed += uint64(len(results))
		p.mu.Unlock()
		p.closeJob("candidate_rejected")
		p.notifyRouteResults(job.key, results)
		return
	}
	routeTargets := make([]string, 0, len(prepared))
	for _, target := range prepared {
		routeTargets = append(routeTargets, target.envelope.To)
	}
	for i := range prepared {
		if prepared[i].envelope.Meta == nil {
			prepared[i].envelope.Meta = map[string]any{}
		}
		prepared[i].envelope.Meta["route_targets"] = routeTargets
	}
	for _, target := range prepared {
		err := p.routeEnvelope(ctx, target.envelope)
		result := amrTargetResult{selector: target.candidate.selector, resolved: target.envelope.To, ok: err == nil, err: err}
		results = append(results, result)
	}
	p.mu.Lock()
	p.stats.targetsAttempted += uint64(len(results))
	for _, result := range results {
		if result.ok {
			p.stats.targetsDispatched++
		} else {
			p.stats.targetsFailed++
		}
	}
	p.mu.Unlock()
	log.Printf("amr decision=dispatched candidates=%d", len(prepared))
	p.closeJob("dispatched")
	p.notifyRouteResults(job.key, results)
}

// metaKeyMentionVerbatim 标记"由提及原样送达产生的派生信封"(S-MENTION-BESTEFFORT ③ 的一跳封顶)。
const metaKeyMentionVerbatim = "mention_verbatim"

// inboundOf 取触发本轮的那封入站信封;取不到返回 false(调用方按无新字段的 legacy 消息处理)。
func (p *amrProcessor) inboundOf(ctx context.Context, deliveryID string) (model.Envelope, bool) {
	if p == nil || p.hub == nil || p.hub.Repo == nil || deliveryID == "" {
		return model.Envelope{}, false
	}
	delivery, err := p.hub.Repo.GetSessionDelivery(ctx, deliveryID)
	if err != nil || delivery == nil {
		return model.Envelope{}, false
	}
	var inbound model.Envelope
	if json.Unmarshal([]byte(delivery.Envelope), &inbound) != nil {
		return model.Envelope{}, false
	}
	return inbound, true
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// amrResolvedShort 从完整地址里取出会话短名,供投递结果回显实际去向。
func amrResolvedShort(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if i := strings.Index(addr, "#"); i >= 0 {
		addr = addr[:i]
	}
	return shortSessionName(addr)
}

func (p *amrProcessor) notifyRouteResults(key amrTurnKey, results []amrTargetResult) {
	succeeded, failed := 0, 0
	var details []string
	for _, result := range results {
		if result.ok {
			succeeded++
			// 解析结果与写法不同时,把实际去向摊开 —— 相同则不啰嗦。
			if to := amrResolvedShort(result.resolved); to != "" && to != strings.TrimPrefix(result.selector, "#") {
				details = append(details, result.selector+" → "+to+"：成功")
			} else {
				details = append(details, result.selector+"：成功")
			}
		} else {
			failed++
			details = append(details, result.selector+"：失败（"+result.err.Error()+"）")
		}
	}
	body := fmt.Sprintf("【投递结果】delivery_id=%s 本轮 @%d：成功 %d，失败 %d", key.DeliveryID, len(results), succeeded, failed)
	if len(details) != 0 {
		body += "\n" + strings.Join(details, "\n")
	}
	// 🔴 这一路**就是业主实际撞到的那个形状**(「本轮 @1：成功 0，失败 1」),而它的信封是
	//    delivery_result、failure=false ⇒ **不经** writeDeliveryFailure。
	//    若不在此覆盖,㈡ 修的就不是业主看见的那一件事(闸会绿,问题还在)。
	// ㈡ 每个失败目标一张卡(与 writeDeliveryFailure 那一路同形),放在通知之前:
	//    看板是 Hub 侧的,即使下面那道版本门把通知关掉,人仍看得到。
	if failed > 0 && p != nil && p.hub != nil {
		now := time.Now().UTC()
		for _, result := range results {
			if result.ok {
				continue
			}
			p.hub.publishFailureDockEvent(key.KeyID, key.Session, "", key.DeliveryID, result.selector, "失败（"+result.err.Error()+"）", now)
		}
	}
	// ㈢ 有目标没投出去 ⇒ 这条【投递结果】就是业主说的「异常信息」⇒ 受版本门约束。
	// 🔵 全成功时 gatedAnomaly=false,行为与今天逐字相同(不在业主这条的范围里,不顺手扩)。
	p.notifySourceNotice(key, "route_result", body, false, "", failed > 0)
}

func (p *amrProcessor) notifySource(key amrTurnKey, target, reason string, failure bool) {
	p.notifySourceVisible(key, target, reason, failure, "")
}

func (p *amrProcessor) notifySourceVisible(key amrTurnKey, target, reason string, failure bool, originalBody string) {
	// 🔵 failure=true 的通知**本身**就是业主 09-29 b 说的「异常信号」⇒ 默认受版本门约束。
	// 保持这个薄包装是为了**不动那 8 个上游调用点**的签名(与 A/C 两件同在 amr.go,少一处合并冲突面)。
	p.notifySourceNotice(key, target, reason, failure, originalBody, failure)
}

// notifySourceNotice 是 amr 侧**回发送方通知的唯一咽喉**。
//
// gatedAnomaly 为真 ⇒ 这条通知属于业主 2026-09-29 逐字 b 所说的「异常信息」:
//
//	「当异常信息送达到 sessionHelper 发送方。**但此信息不用于展示**,仅告诉发送方有异常信息。
//	  **这一点在客户端没有升级支持的情况下可以不发**。」
//
// ⇒ ① 同时送一份进 view 的「错误看板」(Hub 侧,不依赖对端版本);
//
//	② 对端支持时才发,且信封带 display=none;不支持则**不发**并留日志/计数(不静默)。
//
// 🔴 gatedAnomaly 与 failure **不是一回事**:notifyRouteResults 走的是 delivery_result 信封
//
//	(failure=false),但"本轮有目标没投出去"确实是异常 ⇒ 那一路 gatedAnomaly=true 而 failure=false。
func (p *amrProcessor) notifySourceNotice(key amrTurnKey, target, reason string, failure bool, originalBody string, gatedAnomaly bool) {
	if p == nil || p.hub == nil {
		return
	}
	now := time.Now().UTC()
	dedupKey := key.KeyID + "\x00" + key.Session + "\x00" + target + "\x00" + reason
	p.mu.Lock()
	for k, at := range p.notices {
		if now.Sub(at) > time.Minute {
			delete(p.notices, k)
		}
	}
	if at, ok := p.notices[dedupKey]; ok && now.Sub(at) <= time.Minute {
		p.stats.notificationsSuppressed++
		p.mu.Unlock()
		return
	}
	p.notices[dedupKey] = now
	p.mu.Unlock()

	// ㈡ 错误看板。**放在取连接之前**:发送方离线时通知发不出去,但看板这张卡照样要留
	//    (eventRing 会被晚订阅的 View 回放)—— 否则"离线"就等于"这件事没人知道"。
	// 🔵 notifyRouteResults 那一路(failure=false)**不在这里发卡**,它按失败目标逐张发
	//    (一张卡一个目标一个原因,比把整段【投递结果】塞进一张卡可读);
	//    ⇒ 发卡点共**三处**,逐处变异必须每处单独红,见板件 G5。
	if gatedAnomaly && failure {
		p.hub.publishFailureDockEvent(key.KeyID, key.Session, "", key.DeliveryID, target, reason, now)
	}

	p.hub.mu.Lock()
	var c *sessionClient
	if sessions := p.hub.sessionClients[key.KeyID]; sessions != nil {
		c = sessions[key.Session]
	}
	p.hub.mu.Unlock()
	if c == nil {
		log.Printf("amr notification=failed reason=source_offline session=%s", key.Session)
		return
	}
	body := reason
	body += "\n本条为系统通知，不可回程，请勿将回复作为业务答复发送。"
	env := model.Envelope{
		ID:   "amr-notice-" + randomHex(16),
		To:   sessionAddress(key.Session, key.KeyID),
		From: sessionAddress("workpulse", key.KeyID),
		Body: body,
		TS:   now.Unix(),
		Meta: map[string]any{
			"type":      model.MetaTypeDeliveryResult,
			"source_id": key.DeliveryID,
			// 🔴 两支合并:HNI 把字面量换成 model 常量(跨语言单一真源),
			// 回程修复加 suppress_reply_origin —— **两者都要**,不是二选一。
			"suppress_reply_origin": true,
		},
	}
	if failure {
		env = deliveryFailureEnvelopeWithOriginal(c, key.DeliveryID, target, reason, originalBody, now)
		p.hub.annotateRestartWindow(&env, now, "amr_failure")
	}
	if gatedAnomaly {
		// ㈢ 版本门:对端 helper 不支持「送达但不展示」⇒ **不发**(业主:「可以不发」)。
		// 🔴 不静默:留一行日志 ＋ 计一个数。静默跳过就是把 T-071 换个方向重演。
		if amrClientTooOldForNoDisplayNotice(c.version) {
			p.mu.Lock()
			p.stats.notificationsSuppressed++
			p.mu.Unlock()
			log.Printf("amr notification=suppressed reason=client_without_no_display_support session=%s client_version=%q min=%s dock_event=%t",
				key.Session, c.version, amrNoDisplayNoticeMinVersion(), failure)
			return
		}
		if amrSupportsNoDisplayNotice(c.version) {
			if env.Meta == nil {
				env.Meta = map[string]any{}
			}
			// 「不展示」的表达。failure 那一路已由 deliveryFailureEnvelopeWithOriginal 打过,
			// 这里幂等补上 delivery_result 那一路(notifyRouteResults)。
			env.Meta["display"] = "none"
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.routeTimeout)
	defer cancel()
	if err := c.write(ctx, env); err != nil {
		log.Printf("amr notification=failed reason=write err=%v", err)
		return
	}
	p.mu.Lock()
	p.stats.notificationsSent++
	p.mu.Unlock()
}

func amrSupportsStructuredFailure(version string) bool {
	current, ok := parseWireVersion(version)
	if !ok {
		return false
	}
	minimum, ok := parseWireVersion(amrStructuredFailureMinVersion())
	return ok && amrVersionAtLeast(current, minimum)
}

func amrStructuredFailureMinVersion() string {
	return firstNonEmpty(os.Getenv("WP_AMR_STRUCTURED_FAILURE_MIN_VERSION"), "0.1.3.0")
}

// amrSupportsNoDisplayNotice 报告对端 sessionHelper 是否支持「异常信号送达但**不展示**」。
//
// 业主 2026-09-29 逐字:「b、当异常信息送达到 sessionHelper 发送方。**但此信息不用于展示**,
// 仅告诉发送方有异常信息。**这一点在客户端没有升级支持的情况下可以不发**。」
//
// 🔵 机制**照抄** amrSupportsStructuredFailure(同一套"按连接自报版本开能力"),不发明新机制。
// 🔴 为什么用版本门而不是能力位:能力位那条路(deliveryACKV1,hub.go:3686 由消息 meta 上报并持久化)
// 需要**客户端也上报**,而客户端那一半 S-HELPER-NOTICE-NO-INJECT 已闭合、认的是
// Envelope.Meta["type"](真源 internal/model/model.go 的 hub-meta-types 块),没有上报能力位
// ⇒ 要用能力位就得再改一次 helper,而 helper 批已定型。
// 版本门是**三值**的,不是布尔。真值表(两个谓词互斥,未知时两者都 false):
//
//	自报版本 ≥ 门槛   amrSupportsNoDisplayNotice=true   ⇒ 发,且信封带 display=none
//	自报版本 < 门槛   amrClientTooOldForNoDisplayNotice=true ⇒ **不发**(业主:「可以不发」)
//	空 / 非法 / 门槛非法  两者都 false                    ⇒ 发,不打标记(= 与今天逐字相同)
//
// 🔴 第三行是**实测**逼出来的,不是我一开始就想清楚的:第一版把"未知"并进"不支持"(不发),
// 结果一次整包跑打掉 **6 个既有闸**(TestChannelDeliveryLookupFailureIsVisibleAndNeverClaimsNameMissing ·
// TestAgentGroupingReachabilityAndFailureCoordinates · TestShortSessionSelectorRejectsAmbiguousOwnerMatch ·
// TestSessionAddressingRejectsCrossKey · TestReplyToOriginMissingDeliveryNotifiesOnlineSession ·
// TestAMRFailureNotificationCarriesRestartWindowSignal),整包耗时 53.7s→75.7s(它们全在等读超时)。
// ⇒ 那 6 个闸断言的正是「失败必须显式通知发送方」。**我没有去改它们**(既有闸红不自己改回去),
// 而是改了本件的方向:这是一个**减法**能力(少发一条通知),未知情形下应当**保留既有可见性**,
// 不该替对端做决定。
// 🔵 对照:amrSupportsStructuredFailure 是**加法**(多给一个 meta 键),未知 ⇒ 不加 —— 方向相反才对。
func amrSupportsNoDisplayNotice(version string) bool {
	current, ok := parseWireVersion(version)
	if !ok {
		return false
	}
	minimum, ok := parseWireVersion(amrNoDisplayNoticeMinVersion())
	return ok && amrVersionAtLeast(current, minimum)
}

// amrClientTooOldForNoDisplayNotice 报告「我们**确切知道**对端太旧」。未知一律 false(照发)。
func amrClientTooOldForNoDisplayNotice(version string) bool {
	current, ok := parseWireVersion(version)
	if !ok {
		return false
	}
	minimum, ok := parseWireVersion(amrNoDisplayNoticeMinVersion())
	if !ok {
		return false
	}
	return !amrVersionAtLeast(current, minimum)
}

// amrNoDisplayNoticeMinVersion 默认取 **Hub 自己的构建版本**,可用 env 覆盖。
//
// 🔴 理由(不是随手挑的):本仓 Hub 与 sessionHelper **共用同一个 VERSION 文件**
// ⇒ 「带 S-HELPER-NOTICE-NO-INJECT 的 helper」自报的版本**必然 ≥** 与它同批或更晚的 Hub 版本;
// 而升级前在跑的 helper 自报的是旧版本(实测本机 0.2.0.5)⇒ 远低于门槛。
// ⇒ 这个默认值**自动**实现了「Hub 批先上、helper 批后上」期间**不误开**:
// Hub 批上线那一刻门槛就等于它自己的新版本,老 helper 一律不发(符合业主「可以不发」);
// helper 批一上线自动打开,**不需要人去改配置**,也就不会因为忘了改而长期关着(靠记得 = 没有)。
// 🔴 代价(已在板件 DESIGN-S-MENTION-ERRORDOCK-NODISPLAY §六-1 明说,待组长裁):
// 那个窗口期内**发送方 Agent 收不到失败通知**;人仍能在错误看板看到
// (publishFailureDockEvent 是 Hub 侧的,不依赖 helper 版本)。
// 若要把门常开(行为与今天相同),部署时设 WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION=0.0.0.0 即可。
func amrNoDisplayNoticeMinVersion() string {
	return firstNonEmpty(os.Getenv("WP_AMR_NO_DISPLAY_NOTICE_MIN_VERSION"), dwversion.Version)
}

func amrVersionAtLeast(current, minimum [4]int) bool {
	for i := range current {
		if current[i] != minimum[i] {
			return current[i] > minimum[i]
		}
	}
	return true
}

func (p *amrProcessor) closeJob(outcome string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stats.closed++
	switch outcome {
	case "dispatched":
		p.stats.closedDispatched++
	case "candidate_rejected":
		p.stats.closedCandidateRejected++
	case "candidate_ready":
		p.stats.closedCandidateReady++
	case "non_candidate":
		p.stats.closedNonCandidate++
	default:
		panic("unknown AMR closed outcome: " + outcome)
	}
}

func (p *amrProcessor) snapshotStats() (amrStats, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stats, len(p.turns)
}

type amrCandidate struct {
	// channel/channelAddr 非空 = 这是一条【渠道投递】候选,不走 Agent 寻址(G2-a)。
	channel     string
	channelAddr string
	selector    string
	body        string
	replyPolicy string
}

var (
	amrTokenName = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	amrLongName  = regexp.MustCompile(`^([a-z0-9]+)-([a-z0-9]+)-[0-9a-f]{4}$`)
)

func parseAMRCandidates(text, sourceSession string, maxTargets int) []amrCandidate {
	candidates, _ := parseAMRCandidatesDetailed(text, sourceSession, maxTargets)
	return candidates
}

// isMarkdownHeadingPrefix 判断以 `#` 开头的文本是 Markdown 标题而非 legacy 选择器。
// 选择器形如 `#会话名 正文`,`#` 后必须紧跟合法会话名首字符;
// 标题形如 `# 标题` / `## 标题`,`#` 后是空白或又一个 `#`。
func isMarkdownHeadingPrefix(text string) bool {
	rest := strings.TrimPrefix(text, "#")
	if rest == "" {
		return true // 光一个 `#`,不可能是选择器
	}
	if rest[0] == '#' {
		return true // ## / ### / ####
	}
	switch rest[0] {
	case ' ', '\t', '\r', '\n':
		return true
	}
	return false
}

// amrOffendingToken 从解析失败的正文里摘出「看起来像 @/# 目标」的那一段,
// 供失败通知回显。T-060:此处原先硬编码字面量 "candidate",
// 通知读起来像「有个叫 candidate 的会话」,把人引向不存在的东西。
// 只回显首个 token,截断到 64 字节,避免正文泄漏进通知。
func amrOffendingToken(body string) string {
	t := strings.TrimSpace(body)
	if t == "" {
		return "(空)"
	}
	// T-088:同上，按 Unicode 空白切分。此处尤其要紧 —— 这是**用户唯一能看到的线索**，
	// 若按 ASCII 切不开，通知里会回显一整句话而非那个 token，反而更误导。
	if i := strings.IndexFunc(t, unicode.IsSpace); i > 0 {
		t = t[:i]
	}
	if len(t) > 64 {
		t = t[:64] + "…"
	}
	return t
}

func parseAMRCandidatesDetailed(text, sourceSession string, maxTargets int) ([]amrCandidate, bool) {
	if maxTargets <= 0 {
		return nil, false
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	fenced := pairedMarkdownFenceLines(lines)
	lookedLikeCandidate := false
	for i := range lines {
		if fenced[i] {
			continue
		}
		line := lines[i]
		for offset := 0; offset < len(line); {
			rel := strings.IndexAny(line[offset:], "@#")
			if rel < 0 {
				break
			}
			start := offset + rel
			block := line[start:]
			if i+1 < len(lines) {
				block += "\n" + strings.Join(lines[i+1:], "\n")
			}
			candidates, looked := parseAMRCandidateBlock(block, sourceSession, maxTargets)
			lookedLikeCandidate = lookedLikeCandidate || looked
			if len(candidates) > 0 {
				return candidates, true
			}
			if looked {
				// The first syntactically candidate-like block owns the routing
				// decision. Continuing at a later token would let an over-limit or
				// otherwise rejected block evade fail-closed validation.
				return nil, true
			}
			_, size := utf8.DecodeRuneInString(line[start:])
			offset = start + size
		}
	}
	return nil, lookedLikeCandidate
}

func amrHasUnfencedTrigger(text string) bool {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	fenced := pairedMarkdownFenceLines(lines)
	for i, line := range lines {
		if !fenced[i] && strings.ContainsAny(line, "@#") {
			return true
		}
	}
	return false
}

// amrParseFeedback classifies user-actionable syntax failures independently
// from authorization failures. The latter stay fail-closed; unsupported names
// are ordinary text and return to the origin with this explanatory notice.
func amrParseFeedback(text string, candidates []amrCandidate, looked bool, maxTargets int) string {
	if len(candidates) > 0 {
		for _, candidate := range candidates {
			if candidate.channel != "" && amrHasUnfencedTrigger(candidate.body) {
				return "渠道提及只消费第一个目标；正文中的后续提及按普通正文处理，未另行投递"
			}
		}
		return ""
	}
	if !amrHasUnfencedTrigger(text) {
		return ""
	}
	if !looked {
		return "提及写法未识别（名称只支持 ASCII 字母、数字、下划线、点和横杠）；正文已按普通内容回程"
	}
	if strings.HasSuffix(strings.TrimSpace(text), "@") || strings.HasSuffix(strings.TrimSpace(text), "#") ||
		regexp.MustCompile(`[@#][A-Za-z0-9_.-]+!?\s*$`).MatchString(strings.TrimSpace(text)) {
		return "提及后没有可发送的正文，请补充正文"
	}
	return fmt.Sprintf("提及格式正确但目标无权限、不可达或超过数量限制（上限 %d）", maxTargets)
}

// pairedMarkdownFenceLines marks only closed fenced blocks. A fence delimiter
// is a line whose trimmed form starts with at least three backticks or tildes;
// its closer uses the same character at least as many times and has only space
// after it. An unmatched opener marks nothing, so malformed Markdown cannot
// silently swallow every mention through the end of a turn.
func pairedMarkdownFenceLines(lines []string) []bool {
	masked := make([]bool, len(lines))
	for i := 0; i < len(lines); i++ {
		ch, width, ok := markdownFenceOpener(lines[i])
		if !ok {
			continue
		}
		closeAt := -1
		for j := i + 1; j < len(lines); j++ {
			if markdownFenceCloser(lines[j], ch, width) {
				closeAt = j
				break
			}
		}
		if closeAt < 0 {
			continue
		}
		for j := i; j <= closeAt; j++ {
			masked[j] = true
		}
		i = closeAt
	}
	return masked
}

func markdownFenceOpener(line string) (byte, int, bool) {
	trimmed := strings.TrimSpace(line)
	if len(trimmed) < 3 || (trimmed[0] != '`' && trimmed[0] != '~') {
		return 0, 0, false
	}
	width := 0
	for width < len(trimmed) && trimmed[width] == trimmed[0] {
		width++
	}
	return trimmed[0], width, width >= 3
}

func markdownFenceCloser(line string, ch byte, minWidth int) bool {
	trimmed := strings.TrimSpace(line)
	width := 0
	for width < len(trimmed) && trimmed[width] == ch {
		width++
	}
	return width >= minWidth && strings.TrimSpace(trimmed[width:]) == ""
}

func parseAMRCandidateBlock(text, sourceSession string, maxTargets int) ([]amrCandidate, bool) {
	text = strings.TrimSpace(text)
	if text == "" || maxTargets <= 0 {
		return nil, false
	}
	if strings.HasPrefix(text, "#") {
		// T-055: Markdown 标题(`# `、`## `、`#\t`)不是 legacy 选择器。
		// 判别在第二个字符上完成:`#` 后紧跟空白或另一个 `#` ⇒ 标题,直接判非候选,
		// 不置 lookedLikeCandidate —— 否则仍会回一条假的投递失败通知。
		if isMarkdownHeadingPrefix(text) {
			return nil, false
		}
		name, body, ok := parseSelector(text)
		if !ok || name == sourceSession || body == "" || len(body) > amrRouteBodyMax {
			return nil, true
		}
		return []amrCandidate{{selector: "#" + name, body: body, replyPolicy: "no_reply"}}, true
	}
	if !strings.HasPrefix(text, "@") {
		return nil, false
	}
	// 🔴 G2-a:渠道提及必须在这里就与 Agent 寻址分道。
	// 放到后面去判会有两个后果:① 恰好存在同名成员时被静默投给 Agent;
	// ② `@飞书`(无 #)会走下面的多目标 token 分支,被 amrTokenName 判为不合规而**整个跳过**。
	if channel, addr, body, ok := parseChannelMention(text); ok && body != "" && len(body) <= amrRouteBodyMax {
		return []amrCandidate{{channel: channel, channelAddr: addr, body: body, replyPolicy: "no_reply"}}, true
	}
	if owner, session, body, ok := parseMemberMention(text); ok && session != "" && body != "" && len(body) <= amrRouteBodyMax {
		return []amrCandidate{{selector: "@" + owner + "#" + session, body: body, replyPolicy: "no_reply"}}, true
	}

	remaining := text
	tokens := make([]string, 0, maxTargets)
	for strings.HasPrefix(remaining, "@") {
		// 🔴 T-088:此处原为 IndexAny(remaining, " \t\r\n") —— **只认四个 ASCII 空白**。
		// 中文输入法下「@某人　正文」用的是全角空格(U+3000)，切不开 ⇒ 整串被当作一个 token
		// ⇒ amrTokenName 不匹配 ⇒ return nil,true ⇒ **整轮一个目标都不发**，且失败文案
		// 不含任何指向「空格类型」的线索。**单目标同样中招**，不限多目标。
		// 实测受影响:U+00A0 不换行空格 / U+2009 窄空格 / U+3000 全角空格。
		// ⚠️ 与 T-082 是同一文件同类毛病 —— 当时为行首扫描用了 unicode.IsSpace，
		//    却没查 token 切分仍在用 ASCII 四件套。修空白处理须全文搜一遍。
		i := strings.IndexFunc(remaining, unicode.IsSpace)
		if i < 0 {
			token := strings.TrimPrefix(remaining, "@")
			if strings.HasSuffix(token, "!") {
				token = strings.TrimSuffix(token, "!")
			}
			// A valid-looking address without a body is a malformed command and
			// remains fail-closed. Arbitrary prose/URLs after an at sign are not
			// routing commands and must not suppress the whole turn.
			return nil, amrTokenName.MatchString(token)
		}
		if i <= 1 {
			return nil, true
		}
		tokens = append(tokens, remaining[1:i])
		remaining = strings.TrimLeftFunc(remaining[i:], unicode.IsSpace)
		if len(tokens) > maxTargets {
			return nil, true
		}
	}
	body := strings.TrimSpace(remaining)
	if body == "" || len(body) > 100_000 {
		return nil, true
	}
	seen := map[string]bool{}
	skipped := make([]string, 0, 2) // T-091:不合规 token,跳过但须可见
	out := make([]amrCandidate, 0, len(tokens))
	for _, token := range tokens {
		replyPolicy := "no_reply"
		if strings.HasSuffix(token, "!") {
			replyPolicy = "expect_reply"
			token = strings.TrimSuffix(token, "!")
		}
		if !amrTokenName.MatchString(token) {
			// 🔴 T-091(业主 2026-09-12 裁定:「@中文部分直接跳过,暂不支持就好了」):
			// 此处原为 return nil,true —— **一个不合规 token 会让整轮全废**,
			// 连同行的合法英文名一起投不出去(实测 `@opus50ts @张宗楚 hi` → 候选 0)。
			// 改为跳过该 token、继续处理其余,并记入 skipped 供回执回显。
			// ⚠️ 这**放宽了 fail-closed**:此前"有不认识的名字就整轮不发",现在会部分投递。
			// 之所以可接受:①业主明确裁定 ②原行为的代价更大 —— 一个拼错/中文名
			// 就让整轮通信失败,且文案不说明原因。
			//
			// 🔴 **已知缺口(不要照抄旧注释说"跳过会被回显")**:回显只在**全部**token
			// 都被跳过时才有 —— 那时候候选为 0,worker 会走 notifySource(...,
			// "@ 目标无法解析或不符合路由规则", failure=true)。
			// **部分跳过时没有任何回显**:`@opus50ts @张宗楚 hi` 会投给 opus50ts,
			// 而被跳过的 `张宗楚` 只落一条服务端日志,发信方看不到。
			// 唯一的弱信号是投递回执的计数 ——「本轮 @1」与他写的两个名字对不上,
			// 但要靠他自己察觉。**这是"部分成功报成功",与 T-071 同类。**
			// 未补的原因:补回显要改 parseAMRCandidatesDetailed 签名(2 处生产 + 9 处测试),
			// 且业主裁定是「直接跳过,后期再看做集中处理」⇒ 不在本单擅自扩围,另开卡。
			skipped = append(skipped, token)
			continue
		}
		if token == "all" || token == sourceSession || seen[token] {
			continue
		}
		seen[token] = true
		selector := "#" + token
		if parts := amrLongName.FindStringSubmatch(token); len(parts) == 3 {
			if parts[2] == sourceSession {
				continue
			}
			selector = "@" + parts[1] + "#" + parts[2]
		}
		out = append(out, amrCandidate{selector: selector, body: body, replyPolicy: replyPolicy})
	}
	if len(skipped) > 0 {
		// T-091:跳过必须可见。函数签名只返回候选,没有回显通道,
		// 故至少记服务端日志 —— **静默跳过就是把 T-071「误投且报成功」换了个方向重演**。
		// 只记 token 本身(名字),不记正文。
		log.Printf("amr decision=token_skipped reason=unsupported_name tokens=%v source=%s", skipped, sourceSession)
	}
	// When every token is unsupported this was never a valid routing command.
	// Keep the body on the ordinary reply path; the worker emits a syntax notice.
	return out, len(out) > 0
}

func amrDerivedEnvelopeID(key amrTurnKey, index int, selector string) string {
	sum := sha256.Sum256([]byte(key.KeyID + "\x00" + key.Session + "\x00" + key.DeliveryID + "\x00" + key.TurnID + "\x00" + strconv.Itoa(index) + "\x00" + selector))
	return "amr-" + hex.EncodeToString(sum[:16])
}

// filterChannelCandidates 挑出渠道候选(channel 非空)。
func filterChannelCandidates(candidates []amrCandidate) []amrCandidate {
	out := make([]amrCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.channel != "" {
			out = append(out, candidate)
		}
	}
	return out
}
