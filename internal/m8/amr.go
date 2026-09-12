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
)

type amrMode string

const (
	amrOff          amrMode = "off"
	amrObserve      amrMode = "observe"
	amrRoute        amrMode = "route"
	amrRouteBodyMax         = 100_000
)

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
	text      string
	updated   time.Time
	started   time.Time
	ttl       time.Duration
	observed  int
	truncated bool
}

type amrRouteJob struct {
	key  amrTurnKey
	body string
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
	terminalUnknown         map[string]uint64
	workerHeartbeat         time.Time
}

type amrProcessor struct {
	hub           *Hub
	queue         chan amrRouteJob
	noticeQueue   chan amrNoticeJob
	mu            sync.Mutex
	turns         map[amrTurnKey]*amrTurn
	completed     map[amrTurnKey]time.Time
	notices       map[string]time.Time
	modes         map[string]amrMode
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
		modes:        parseAMRSessionModes(os.Getenv("WP_AMR_OBSERVE_SESSIONS"), os.Getenv("WP_AMR_ROUTE_SESSIONS")),
		maxTurns:     envInt("WP_AMR_MAX_TURNS_PER_SESSION", 16),
		maxTurnAge:   envDuration("WP_AMR_MAX_TURN_AGE", 6*time.Hour),
		maxBytes:     envInt("WP_AMR_MAX_TURN_BYTES", 256<<10),
		routeBodyMax: amrRouteBodyMax,
		maxCompleted: envInt("WP_AMR_MAX_COMPLETED_PER_SESSION", 64),
		turnTTL:      envDuration("WP_AMR_TURN_TTL", 5*time.Minute),
		routeTimeout: envDuration("WP_AMR_ROUTE_TIMEOUT", 5*time.Second),
		maxTargets:   envInt("WP_AMR_MAX_TARGETS", 20),
	}
	p.stats.terminalUnknown = map[string]uint64{}
	p.routeEnvelope = h.RouteEnvelope
	if len(p.modes) != 0 {
		log.Printf("amr startup structured_failure_min_version=%s", amrStructuredFailureMinVersion())
		go p.run()
	}
	return p
}

func envInt(name string, fallback int) int {
	v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(name)))
	if err != nil || v <= 0 {
		return fallback
	}
	return v
}

// Session selectors are either an exact "keyID/session" or a session name.
// Exact selectors win. A route entry overrides an observe entry.
func parseAMRSessionModes(observe, route string) map[string]amrMode {
	out := map[string]amrMode{}
	for _, item := range strings.Split(observe, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out[item] = amrObserve
		}
	}
	for _, item := range strings.Split(route, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out[item] = amrRoute
		}
	}
	return out
}

func (p *amrProcessor) modeFor(keyID, session string) amrMode {
	if p == nil {
		return amrOff
	}
	if mode := p.modes[keyID+"/"+session]; mode != "" {
		return mode
	}
	if mode := p.modes[session]; mode != "" {
		return mode
	}
	return amrOff
}

func (p *amrProcessor) shouldDropLegacyAgentRoute(keyID, session string, env model.Envelope) bool {
	drop := p != nil && p.modeFor(keyID, session) == amrRoute &&
		metaString(env.Meta, "type") == "agent_route" && metaBool(env.Meta, "no_mirror")
	if drop {
		p.mu.Lock()
		p.stats.legacyRouteDropped++
		p.mu.Unlock()
	}
	return drop
}

func (p *amrProcessor) ingest(keyID, sessionName, raw string) {
	mode := p.modeFor(keyID, sessionName)
	if mode == amrOff {
		return
	}
	var ev amrSessionEvent
	if err := json.Unmarshal([]byte(raw), &ev); err != nil {
		p.transitionFailed("invalid_json")
		return
	}
	p.mu.Lock()
	p.stats.eventsObserved++
	p.mu.Unlock()
	if ev.DeliveryID == "" || ev.TurnID == "" || amrEventReplay(ev.Data) {
		p.transitionFailed("missing_identity_or_replay")
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
		p.notifySource(key, "terminal", "终态表示本轮未成功", true)
		return
	}
	if body == "" {
		p.stats.incomplete++
		p.mu.Unlock()
		log.Printf("amr decision=incomplete reason=empty")
		p.notifySource(key, "empty", "本轮没有可路由的完整文本", true)
		return
	}
	if t.truncated {
		body = amrTruncatedBody(body, p.routeBodyMax)
	}
	select {
	case p.queue <- amrRouteJob{key: key, body: body}:
		p.stats.queued++
		p.mu.Unlock()
	default:
		p.stats.failed++
		p.mu.Unlock()
		log.Printf("amr decision=failed reason=queue_full")
		p.notifySource(key, "queue", "Hub 路由队列已满", true)
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
	for {
		select {
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

func (p *amrProcessor) logStatsLocked() {
	active := uint64(len(p.turns))
	topOK := p.stats.opened == active+p.stats.queued+p.stats.closed+p.stats.incomplete+p.stats.failed
	closedOK := p.stats.closed == p.stats.closedNonCandidate+p.stats.closedCandidateRejected+p.stats.closedCandidateReady+p.stats.closedDispatched
	log.Printf("amr heartbeat events=%d opened=%d active=%d queued=%d closed=%d incomplete=%d failed=%d non_candidate=%d candidate_rejected=%d candidate_ready=%d dispatched=%d fragment_append=%d fragment_snapshot=%d fragment_dup=%d late_after_terminal=%d legacy_dropped=%d targets_attempted=%d targets_dispatched=%d targets_failed=%d queue_underflow=%d notifications_sent=%d notifications_suppressed=%d terminal_unknown=%v structured_failure_min_version=%s rejected_events=%d top_invariant=%t closed_invariant=%t",
		p.stats.eventsObserved, p.stats.opened, active, p.stats.queued, p.stats.closed, p.stats.incomplete, p.stats.failed,
		p.stats.closedNonCandidate, p.stats.closedCandidateRejected, p.stats.closedCandidateReady, p.stats.closedDispatched,
		p.stats.fragmentAppend, p.stats.fragmentSnapshot, p.stats.fragmentDuplicate, p.stats.lateAfterTerminal, p.stats.legacyRouteDropped,
		p.stats.targetsAttempted, p.stats.targetsDispatched, p.stats.targetsFailed, p.stats.queueUnderflow,
		p.stats.notificationsSent, p.stats.notificationsSuppressed, p.stats.terminalUnknown, amrStructuredFailureMinVersion(), p.stats.eventsRejected,
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
	if len(candidates) == 0 {
		if lookedLikeCandidate {
			p.closeJob("candidate_rejected")
			if p.modeFor(job.key.KeyID, job.key.Session) == amrRoute {
				p.notifySource(job.key, amrOffendingToken(job.body), "@ 目标无法解析或不符合路由规则", true)
			}
		} else {
			p.closeJob("non_candidate")
		}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), p.routeTimeout)
	defer cancel()
	prepared := make([]amrPreparedTarget, 0, len(candidates))
	results := make([]amrTargetResult, 0, len(candidates))
	for i, candidate := range candidates {
		env := model.Envelope{
			ID:   amrDerivedEnvelopeID(job.key, i, candidate.selector),
			To:   candidate.selector,
			Body: candidate.body,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"type":               "agent_route",
				"no_mirror":          true,
				"reply_policy":       candidate.replyPolicy,
				"source_delivery_id": job.key.DeliveryID,
				"source_turn_id":     job.key.TurnID,
			},
		}
		handled, resolved, err := p.hub.prepareSessionSelector(ctx, job.key.KeyID, job.key.Session, env)
		if err != nil || !handled {
			log.Printf("amr decision=candidate_rejected handled=%t err=%v", handled, err)
			results = append(results, amrTargetResult{selector: candidate.selector, err: errors.New(firstNonEmpty(errorString(err), "目标无法解析"))})
			continue
		}
		prepared = append(prepared, amrPreparedTarget{candidate: candidate, envelope: resolved})
	}
	if p.modeFor(job.key.KeyID, job.key.Session) != amrRoute {
		if len(prepared) == 0 {
			p.closeJob("candidate_rejected")
			return
		}
		log.Printf("amr decision=candidate_ready candidates=%d", len(prepared))
		p.closeJob("candidate_ready")
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
	body := fmt.Sprintf("【投递结果】本轮 @%d：成功 %d，失败 %d", len(results), succeeded, failed)
	if len(details) != 0 {
		body += "\n" + strings.Join(details, "\n")
	}
	p.notifySource(key, "route_result", body, false)
}

func (p *amrProcessor) notifySource(key amrTurnKey, target, reason string, failure bool) {
	if p == nil || p.hub == nil || p.modeFor(key.KeyID, key.Session) != amrRoute {
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
	env := model.Envelope{
		ID:   "amr-notice-" + randomHex(16),
		To:   sessionAddress(key.Session, key.KeyID),
		From: sessionAddress("workpulse", key.KeyID),
		Body: body,
		TS:   now.Unix(),
		Meta: map[string]any{"type": "delivery_result", "source_id": key.DeliveryID},
	}
	if failure {
		env = deliveryFailureEnvelope(c, key.DeliveryID, target, reason, now)
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
	lookedLikeCandidate := false
	for i := range lines {
		line := lines[i]
		if i == 0 {
			// T-082: retain the historical first-line whitespace allowance.
			line = strings.TrimLeftFunc(line, unicode.IsSpace)
		} else if first, _ := utf8.DecodeRuneInString(line); unicode.IsSpace(first) {
			// Later lines are intentionally column-zero only. Indentation is the
			// documented escape hatch for examples and pasted logs.
			continue
		}
		if !strings.HasPrefix(line, "@") && !strings.HasPrefix(line, "#") {
			continue
		}
		block := line
		if i+1 < len(lines) {
			block += "\n" + strings.Join(lines[i+1:], "\n")
		}
		candidates, looked := parseAMRCandidateBlock(block, sourceSession, maxTargets)
		lookedLikeCandidate = lookedLikeCandidate || looked
		if len(candidates) > 0 {
			return candidates, true
		}
		// A Markdown heading is not a candidate, but it also must not stop the
		// scan: a later column-zero route directive remains eligible.
	}
	return nil, lookedLikeCandidate
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
	out := make([]amrCandidate, 0, len(tokens))
	for _, token := range tokens {
		replyPolicy := "no_reply"
		if strings.HasSuffix(token, "!") {
			replyPolicy = "expect_reply"
			token = strings.TrimSuffix(token, "!")
		}
		if !amrTokenName.MatchString(token) {
			return nil, true
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
	return out, true
}

func amrDerivedEnvelopeID(key amrTurnKey, index int, selector string) string {
	sum := sha256.Sum256([]byte(key.KeyID + "\x00" + key.Session + "\x00" + key.DeliveryID + "\x00" + key.TurnID + "\x00" + strconv.Itoa(index) + "\x00" + selector))
	return "amr-" + hex.EncodeToString(sum[:16])
}
