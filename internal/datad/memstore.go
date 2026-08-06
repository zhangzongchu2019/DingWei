package datad

// MemStore 内存实现:单测/联调 dev 模式(-store=mem)。语义与 PG 实现一致
// (乐观锁/CAS/append-only),供 P 组用例在无 PG 环境跑核心逻辑。

import (
	"context"
	"encoding/json"
	"sort"
	"sync"
	"time"
)

type MemStore struct {
	mu        sync.RWMutex
	tasks     map[string]Task
	dispatch  []DispatchRow            // 自增 id=len+1,append-only(语义同 PG BIGSERIAL)
	config    map[string][]ConfigEntry // key→按 generation 升序
	sessions  map[string]SessionState
	heartbeat []struct {
		Session string
		At      time.Time
		Payload json.RawMessage
	}
	skills []Skill
	audit  []AuditEntry
}

func NewMemStore() *MemStore {
	return &MemStore{
		tasks: map[string]Task{}, config: map[string][]ConfigEntry{},
		sessions: map[string]SessionState{},
	}
}

func (m *MemStore) Ping(context.Context) error { return nil }

func (m *MemStore) ListTasks(_ context.Context, status, owner string) ([]Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []Task
	for _, t := range m.tasks {
		if (status == "" || t.Status == status) && (owner == "" || t.OwnerSession == owner) {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *MemStore) GetTask(_ context.Context, id string) (*Task, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	return &t, nil
}

func (m *MemStore) PutTask(_ context.Context, t Task, ifMatch time.Time, force bool) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	old, exists := m.tasks[t.ID]
	if exists && !force && !old.UpdatedAt.Equal(ifMatch) {
		return nil, ErrConflict
	}
	if !exists {
		t.CreatedAt = now
	} else {
		t.CreatedAt = old.CreatedAt
	}
	t.UpdatedAt = now
	m.tasks[t.ID] = t
	return &t, nil
}

func (m *MemStore) UpdateProgress(_ context.Context, id string, p TaskProgress) (*Task, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.tasks[id]
	if !ok {
		return nil, ErrNotFound
	}
	if p.Anchor != "" {
		t.Anchor = p.Anchor
	}
	if p.ETA != "" {
		t.ETA = p.ETA
	}
	if len(p.Progress) > 0 {
		var d map[string]json.RawMessage
		if len(t.Detail) > 0 {
			_ = json.Unmarshal(t.Detail, &d)
		}
		if d == nil {
			d = map[string]json.RawMessage{}
		}
		d["progress"] = p.Progress
		t.Detail, _ = json.Marshal(d)
	}
	t.UpdatedAt = time.Now().UTC()
	m.tasks[id] = t
	return &t, nil
}

func (m *MemStore) AppendDispatch(_ context.Context, d Dispatch) error {
	// id 序=提交序不变量:mutex 串行天然成立(PG 侧靠 advisory xact 锁,见 pgstore)。
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatch = append(m.dispatch, DispatchRow{
		ID: int64(len(m.dispatch) + 1), TaskID: d.TaskID, Assignee: d.Assignee,
		Role: d.Role, Round: d.Round, Note: d.Note, CreatedAt: time.Now().UTC(),
	})
	return nil
}

func (m *MemStore) ListDispatches(_ context.Context, taskID string, sinceID int64, limit int) ([]DispatchRow, error) {
	if limit <= 0 {
		limit = 1000 // 语义与 PG 归一(N3)
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []DispatchRow{}
	for _, r := range m.dispatch {
		if r.ID <= sinceID || (taskID != "" && r.TaskID != taskID) {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *MemStore) GetConfig(_ context.Context, key string, generation int64) (*ConfigEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entries := m.config[key]
	if len(entries) == 0 {
		return nil, ErrNotFound
	}
	if generation <= 0 {
		e := entries[len(entries)-1]
		return &e, nil
	}
	for _, e := range entries {
		if e.Generation == generation {
			return &e, nil
		}
	}
	return nil, ErrNotFound
}

func (m *MemStore) ListConfig(_ context.Context) ([]ConfigEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []ConfigEntry
	for _, entries := range m.config {
		if len(entries) > 0 {
			out = append(out, entries[len(entries)-1]) // 每 key 最新代次
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

func (m *MemStore) PutConfig(_ context.Context, key string, value json.RawMessage, expectedGen int64, by string) (*ConfigEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.config[key]
	cur := int64(0)
	if len(entries) > 0 {
		cur = entries[len(entries)-1].Generation
	}
	if expectedGen != cur {
		return nil, ErrCASFailed
	}
	e := ConfigEntry{Key: key, Generation: cur + 1, Value: value, UpdatedBy: by, UpdatedAt: time.Now().UTC()}
	m.config[key] = append(entries, e)
	return &e, nil
}

func (m *MemStore) PutSessionState(_ context.Context, s SessionState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.UpdatedAt = time.Now().UTC()
	m.sessions[s.Session] = s
	return nil
}

func (m *MemStore) ListSessions(_ context.Context) ([]SessionState, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []SessionState
	for _, s := range m.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out, nil
}

func (m *MemStore) AppendHeartbeat(_ context.Context, session string, payload json.RawMessage, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.heartbeat = append(m.heartbeat, struct {
		Session string
		At      time.Time
		Payload json.RawMessage
	}{session, at, payload})
	return nil
}

func (m *MemStore) LatestHeartbeats(_ context.Context) ([]HeartbeatLatest, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	latest := map[string]time.Time{}
	for _, h := range m.heartbeat {
		if h.At.After(latest[h.Session]) {
			latest[h.Session] = h.At
		}
	}
	out := make([]HeartbeatLatest, 0, len(latest))
	for s, at := range latest {
		out = append(out, HeartbeatLatest{Session: s, At: at})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out, nil
}

func (m *MemStore) ListSkills(_ context.Context) ([]Skill, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append([]Skill(nil), m.skills...), nil
}

func (m *MemStore) ProposeSkill(_ context.Context, s Skill) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s.Status = "proposed" // 提案态强制(批准=另端点 coordinator)
	m.skills = append(m.skills, s)
	return nil
}

func (m *MemStore) SetSkillStatus(_ context.Context, name, version, status, by string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.skills {
		if m.skills[i].Name == name && m.skills[i].Version == version {
			m.skills[i].Status = status
			m.skills[i].ApprovedBy = by
			return nil
		}
	}
	return ErrNotFound
}

func (m *MemStore) AppendAudit(_ context.Context, e AuditEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.audit = append(m.audit, e)
	return nil
}

func (m *MemStore) ListAudit(_ context.Context, since time.Time, limit int) ([]AuditEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []AuditEntry
	for _, e := range m.audit {
		if e.At.After(since) {
			out = append(out, e)
		}
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}
