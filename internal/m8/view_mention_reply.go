package m8

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/model"
)

// view 页提及的回复回流（R-V2-0103-MENTION-REPLY-MIRROR；要点板 runbooks/PLAN-R-V2-0103-MENTION-REPLY-MIRROR-opus50tc2k.md，
// 以第五、六节为准）。业主选方案 1「只显示回来」：A 页提及 X 并转发后，X 处理那封派生信封的【那一轮】回复，
// 回合结束时作为一条只读显示事件推进 A 的 view 事件流（publishSessionEvent）——
// 只进 A 页，不进 A 的 helper、不入 session_delivery、不进 AMR，A 的 Agent 看不到、不触发回合。
//
// 识别「那一轮」靠回合事件的 delivery_id ＝ 派生信封的 session_delivery 主键（delivery_id.go 契约）：
// 只有可靠信封且对端报 delivery_ack_v1 才有这个键。不报 ack 的对端在 0.2.1.102 已被拒投
// （view_mention_route.go viewMentionPeerACK，回执 no_ack），收不到派生信封，也就不会触发回流。
// 与 AMR ingest 同口径：delivery_id 或 turn_id 为空、或 data.replay=true 的事件一律不看。

const (
	mentionReplyMaxBytes    = 16 << 10         // 回流正文上限（组长 075655 第 2 条）
	mentionReplyMaxInflight = 256              // 在途累计表上限
	mentionReplyTurnTTL     = 30 * time.Minute // 30 分钟未结束的回合丢弃
	mentionReplyLookupCap   = 1024             // 入站查询结果（是／不是 view_mention）LRU 上限
	mentionReplyDoneCap     = 1024             // 已结束回合集合上限（复核必改：终态后同键事件一律忽略）
)

// mentionReplyLongNote 截断后卡片正文末尾的说明（组长 075655 第 2 条，逐字）。%s＝X 的显示名。
func mentionReplyLongNote(label string) string {
	return "（内容较长，完整回复请到 " + label + " 的页面查看）"
}

type mentionReplySource struct {
	ok            bool   // 入站是 view_mention 派生信封
	mentionedFrom string // A 的 full_name
}

type mentionReplyTurn struct {
	text      string
	observed  int
	truncated bool
	started   time.Time
	source    mentionReplySource
}

type mentionMirror struct {
	hub *Hub
	now func() time.Time

	mu      sync.Mutex
	turns   map[amrTurnKey]*mentionReplyTurn
	lookups map[string]*list.Element // delivery_id → *mentionReplyLookup（LRU）
	order   *list.List
	// done 已发出（或已放弃）的回合：终态后同键的重复终态、迟到文本一律忽略——一轮只发一张，
	// 也不留永不结束的在途轮（同 AMR 的 completed，amr.go F8/T-040；tosonnet55tc2k 复核必改）。
	done      map[amrTurnKey]*list.Element
	doneOrder *list.List
}

type mentionReplyLookup struct {
	deliveryID string
	source     mentionReplySource
}

func newMentionMirror(h *Hub) *mentionMirror {
	return &mentionMirror{hub: h, now: time.Now, turns: map[amrTurnKey]*mentionReplyTurn{}, lookups: map[string]*list.Element{}, order: list.New(),
		done: map[amrTurnKey]*list.Element{}, doneOrder: list.New()}
}

// observe 在 Hub 收到某会话（X）的 session_event 时调用（terminal.go，紧随 AMR ingest）。
func (m *mentionMirror) observe(keyID, sessionName, raw string) {
	if m == nil || m.hub == nil || m.hub.Identity == nil {
		return
	}
	var ev amrSessionEvent
	if json.Unmarshal([]byte(raw), &ev) != nil || ev.DeliveryID == "" || ev.TurnID == "" || amrEventReplay(ev.Data) {
		return
	}
	src, ok := m.lookup(keyID, sessionName, ev.DeliveryID)
	if !ok || !src.ok {
		return
	}
	key := amrTurnKey{KeyID: keyID, Session: sessionName, DeliveryID: ev.DeliveryID, TurnID: ev.TurnID}
	now := m.now()
	m.mu.Lock()
	if _, finished := m.done[key]; finished {
		m.mu.Unlock()
		log.Printf("mention reply decision=ignored reason=completed_turn session=%s delivery_id=%s kind=%s", sessionName, ev.DeliveryID, ev.Kind)
		return
	}
	m.expireLocked(now)
	t := m.turns[key]
	if t == nil {
		if len(m.turns) >= mentionReplyMaxInflight {
			m.evictOldestLocked()
		}
		t = &mentionReplyTurn{started: now, source: src}
		m.turns[key] = t
	}
	if ev.Kind == "assistant_text" && ev.Text != "" {
		// 先把非法 UTF-8 换成替换符：amrKeepHead 遇非法字节会退到 0，整段吞成空卡（复核提醒 2）。
		frag := strings.ToValidUTF8(ev.Text, "\uFFFD")
		t.text, t.observed, t.truncated, _ = accumulateTurnText(t.text, t.observed, frag, mentionReplyMaxBytes, t.truncated)
		if t.observed > mentionReplyMaxBytes {
			t.truncated = true
		}
	}
	if !amrTerminalEvent(ev) {
		m.mu.Unlock()
		return
	}
	delete(m.turns, key)
	m.markDoneLocked(key)
	m.mu.Unlock()
	succeeded, reason := amrTerminalOutcome(ev.Data)
	m.publish(keyID, sessionName, ev.DeliveryID, ev.TurnID, t, succeeded, reason)
}

// deliveryDead 派生信封最终没被处理（failed 不可重试、重试耗尽、过期）：给 A 页一行状态。
func (m *mentionMirror) deliveryDead(d model.SessionDelivery, reason string) {
	if m == nil || m.hub == nil || m.hub.Identity == nil {
		return
	}
	var env model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &env) != nil || metaString(env.Meta, "route_source") != "view_mention" {
		return
	}
	src := mentionReplySource{ok: true, mentionedFrom: metaString(env.Meta, "mentioned_from")}
	t := &mentionReplyTurn{source: src}
	m.publishStatus(d.KeyID, d.SessionName, d.ID, "dead", t, "未能处理这条转发："+firstNonEmpty(strings.TrimSpace(reason), "未知原因"))
}

// lookup 按 delivery_id 查回入站信封判定是否 view_mention 派生信封；结果（含「不是」）进 LRU。
func (m *mentionMirror) lookup(keyID, sessionName, deliveryID string) (mentionReplySource, bool) {
	m.mu.Lock()
	if el, hit := m.lookups[deliveryID]; hit {
		m.order.MoveToFront(el)
		src := el.Value.(*mentionReplyLookup).source
		m.mu.Unlock()
		return src, true
	}
	m.mu.Unlock()
	if m.hub.Repo == nil {
		return mentionReplySource{}, false
	}
	d, err := m.hub.Repo.GetSessionDelivery(context.Background(), deliveryID)
	if err != nil {
		return mentionReplySource{}, false // 查库失败不缓存，下一条事件再试
	}
	src := mentionReplySource{}
	if d != nil && d.KeyID == keyID && d.SessionName == sessionName {
		var env model.Envelope
		if json.Unmarshal([]byte(d.Envelope), &env) == nil && metaString(env.Meta, "route_source") == "view_mention" {
			src = mentionReplySource{ok: true, mentionedFrom: metaString(env.Meta, "mentioned_from")}
		}
	}
	m.remember(deliveryID, src)
	return src, true
}

// remember 把一次查询结果放进 LRU（上限 mentionReplyLookupCap）；淘汰后同一 delivery_id 再来只是多查一次主键。
func (m *mentionMirror) remember(deliveryID string, src mentionReplySource) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, hit := m.lookups[deliveryID]; hit {
		return
	}
	m.lookups[deliveryID] = m.order.PushFront(&mentionReplyLookup{deliveryID: deliveryID, source: src})
	for m.order.Len() > mentionReplyLookupCap {
		oldest := m.order.Back()
		m.order.Remove(oldest)
		delete(m.lookups, oldest.Value.(*mentionReplyLookup).deliveryID)
	}
}

// markDoneLocked 记一轮为已结束（有界，最早的先出；淘汰后同键再来只会在下一个终态多发一次，属可接受的退化）。
func (m *mentionMirror) markDoneLocked(key amrTurnKey) {
	if _, hit := m.done[key]; hit {
		return
	}
	m.done[key] = m.doneOrder.PushFront(key)
	for m.doneOrder.Len() > mentionReplyDoneCap {
		oldest := m.doneOrder.Back()
		m.doneOrder.Remove(oldest)
		delete(m.done, oldest.Value.(amrTurnKey))
	}
}

func (m *mentionMirror) expireLocked(now time.Time) {
	for k, t := range m.turns {
		if now.Sub(t.started) > mentionReplyTurnTTL {
			delete(m.turns, k)
			m.markDoneLocked(k) // 已放弃：迟到的终态不再发半截
			log.Printf("mention reply decision=expired session=%s delivery_id=%s", k.Session, k.DeliveryID)
		}
	}
}

func (m *mentionMirror) evictOldestLocked() {
	var oldestKey amrTurnKey
	var oldest time.Time
	first := true
	for k, t := range m.turns {
		if first || t.started.Before(oldest) {
			oldestKey, oldest, first = k, t.started, false
		}
	}
	if !first {
		delete(m.turns, oldestKey)
		m.markDoneLocked(oldestKey)
		log.Printf("mention reply decision=evicted session=%s delivery_id=%s", oldestKey.Session, oldestKey.DeliveryID)
	}
}

// resolve 复核 A、X 都是未吊销的新平台 Agent 且同 owner（沿用 SCOPE 034953）；返回 A 与 X 的显示名。
func (m *mentionMirror) resolve(xKeyID, xSession string, src mentionReplySource) (a *identity.Agent, xFull, xLabel string, ok bool) {
	ctx := context.Background()
	h := m.hub
	a, _, err := h.Identity.ResolveFullName(ctx, strings.TrimSpace(src.mentionedFrom))
	if err != nil {
		return nil, "", "", false
	}
	x, found := h.newPlatformAgentAt(ctx, xKeyID, xSession)
	if !found || identity.Lower(x.OwnerLC) != identity.Lower(a.OwnerLC) || x.ID == a.ID {
		return nil, "", "", false
	}
	xFull = h.Identity.FullName(ctx, x)
	xLabel = xFull
	if cands, err := h.Identity.MentionCandidates(ctx, a.ID); err == nil {
		for _, c := range cands {
			if c.Agent.ID == x.ID {
				xLabel = firstNonEmpty(c.Insert, xFull)
			}
		}
	}
	return a, xFull, xLabel, true
}

func (m *mentionMirror) publish(xKeyID, xSession, deliveryID, turnID string, t *mentionReplyTurn, succeeded bool, reason string) {
	if !succeeded {
		m.publishStatus(xKeyID, xSession, deliveryID, turnID, t, "这一轮未成功："+firstNonEmpty(strings.TrimSpace(reason), "未知原因"))
		return
	}
	a, xFull, xLabel, ok := m.resolve(xKeyID, xSession, t.source)
	if !ok {
		log.Printf("mention reply decision=skipped reason=identity session=%s delivery_id=%s", xSession, deliveryID)
		return
	}
	text := t.text
	if strings.TrimSpace(text) == "" {
		text = "（这一轮没有文字回复）"
	} else if t.truncated {
		note := "\n\n" + mentionReplyLongNote(xLabel)
		text, _ = amrKeepHead(text, mentionReplyMaxBytes-len(note), true)
		text += note
	}
	m.emit(a, xFull, xLabel, deliveryID, turnID, "ok", text)
}

func (m *mentionMirror) publishStatus(xKeyID, xSession, deliveryID, turnID string, t *mentionReplyTurn, status string) {
	a, xFull, xLabel, ok := m.resolve(xKeyID, xSession, t.source)
	if !ok {
		log.Printf("mention reply decision=skipped reason=identity session=%s delivery_id=%s", xSession, deliveryID)
		return
	}
	m.emit(a, xFull, xLabel, deliveryID, turnID, "failed", xLabel+" "+status)
}

// emit 推一条 kind=mention_reply 到 A 的 view 事件流。
// 🔴 顶层与 data 都【不带】turn_id／delivery_id：渲染器见到任一个就把它并进 A 自己的回合正文
// （与 publishFailureDockEvent 同一条经验）。关联坐标只放 data.source_id。
func (m *mentionMirror) emit(a *identity.Agent, xFull, xLabel, deliveryID, turnID, status, text string) {
	sum := sha256.Sum256([]byte(turnID))
	event := map[string]any{
		"schema_version": "delivery.v2",
		"kind":           "mention_reply",
		"message_id":     "mreply-" + deliveryID + "-" + hex.EncodeToString(sum[:4]),
		"text":           text,
		"data":           map[string]any{"from_full_name": xFull, "from_label": xLabel, "status": status, "source_id": deliveryID},
		"updated_at":     m.now().UTC().Format(time.RFC3339Nano),
	}
	raw, err := json.Marshal(event)
	if err != nil {
		log.Printf("mention reply marshal=failed delivery_id=%s err=%v", deliveryID, err)
		return
	}
	m.hub.publishSessionEvent(AgentKeyID(a.OwnerLC), a.NameLC(), string(raw))
	log.Printf("mention reply decision=published to=%s from=%s status=%s bytes=%d", a.NameLC(), xFull, status, len(text))
}
