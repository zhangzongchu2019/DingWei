package datad

// dingwei-datad 存储抽象(T18 设计冻结版 §2/§3):Store 接口=端点需求面;
// PG 实现=pgstore.go(生产),内存实现=memstore.go(单测/联调 dev 模式)。
// 铁律:datad=唯一 PG 入口;audit 只增不改(接口上无 Update/Delete)。

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("optimistic lock conflict") // 409
	ErrCASFailed = errors.New("generation mismatch")      // 412
)

type Task struct {
	ID           string          `json:"id"`
	Title        string          `json:"title"`
	OwnerSession string          `json:"owner_session"`
	Status       string          `json:"status"`
	Anchor       string          `json:"anchor,omitempty"`
	Gate         string          `json:"gate,omitempty"`
	ETA          string          `json:"eta,omitempty"`
	Detail       json.RawMessage `json:"detail,omitempty"`
	CreatedAt    time.Time       `json:"created_at"`
	UpdatedAt    time.Time       `json:"updated_at"`
}

var taskStatuses = map[string]bool{"proposed": true, "dispatched": true, "in_progress": true,
	"review": true, "blocked": true, "waiting_gate": true, "done": true, "archived": true}

type Dispatch struct {
	TaskID   string `json:"task_id"`
	Assignee string `json:"assignee"`
	Role     string `json:"role"`
	Round    int    `json:"round"`
	Note     string `json:"note,omitempty"`
}

// DispatchRow dispatch 读侧行:含 DB 生成列 id/created_at(快照锚 max_id/增量分页用)。
// 读侧与写侧(Dispatch)分型:写侧不接受调用方指定 id。
type DispatchRow struct {
	ID        int64     `json:"id"`
	TaskID    string    `json:"task_id"`
	Assignee  string    `json:"assignee"`
	Role      string    `json:"role"`
	Round     int       `json:"round"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// HeartbeatLatest 每会话最新心跳(活性核/快照用;不含 payload——payload 上限 64KB,
// 聚合读只需时间戳)。
type HeartbeatLatest struct {
	Session string    `json:"session"`
	At      time.Time `json:"at"`
}

type ConfigEntry struct {
	Key        string          `json:"key"`
	Generation int64           `json:"generation"`
	Value      json.RawMessage `json:"value"`
	UpdatedBy  string          `json:"updated_by"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

type SessionState struct {
	Session      string    `json:"session"`
	State        string    `json:"state"`
	WatermarkPct float64   `json:"watermark_pct,omitempty"`
	CurrentTask  string    `json:"current_task,omitempty"`
	Note         string    `json:"note,omitempty"`
	UpdatedAt    time.Time `json:"updated_at"`
}

var sessionStates = map[string]bool{"active": true, "idle": true, "silent_notice": true,
	"restarting": true, "dead": true}

type Skill struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Status     string `json:"status"`
	Content    string `json:"content,omitempty"`
	SHA256     string `json:"sha256"`
	ProposedBy string `json:"proposed_by"`
	ApprovedBy string `json:"approved_by,omitempty"`
}

type AuditEntry struct {
	Actor  string          `json:"actor"`
	Op     string          `json:"op"`
	Target string          `json:"target"`
	Detail json.RawMessage `json:"detail,omitempty"`
	At     time.Time       `json:"at"`
}

// TaskProgress 自报域可改字段白名单(§3:不可改 status/owner=防越权)。
type TaskProgress struct {
	Anchor   string          `json:"anchor,omitempty"`
	ETA      string          `json:"eta,omitempty"`
	Progress json.RawMessage `json:"progress,omitempty"` // 并入 detail.progress
}

type Store interface {
	// tasks(管理域写=coordinator;progress=owner 自报)
	ListTasks(ctx context.Context, status, owner string) ([]Task, error)
	GetTask(ctx context.Context, id string) (*Task, error)
	PutTask(ctx context.Context, t Task, ifMatch time.Time, force bool) (*Task, error) // 乐观锁:ifMatch=updated_at;force=首建或跳锁(仅内部导入)
	UpdateProgress(ctx context.Context, id string, p TaskProgress) (*Task, error)
	// dispatch(append-only;读=权威只读侧,audit 重建不权威——audit 写失败仅告警不回滚)
	AppendDispatch(ctx context.Context, d Dispatch) error
	ListDispatches(ctx context.Context, taskID, assignee string, sinceID int64, limit int) ([]DispatchRow, error) // id 升序;taskID/assignee 空=不过滤;sinceID=增量下界(不含)
	// config(版本化 CAS)
	GetConfig(ctx context.Context, key string, generation int64) (*ConfigEntry, error) // generation<=0=最新
	ListConfig(ctx context.Context) ([]ConfigEntry, error)                             // 每 key 最新代次(project_context 用)
	PutConfig(ctx context.Context, key string, value json.RawMessage, expectedGen int64, by string) (*ConfigEntry, error)
	// sessions(行级自报)
	PutSessionState(ctx context.Context, s SessionState) error
	ListSessions(ctx context.Context) ([]SessionState, error)
	AppendHeartbeat(ctx context.Context, session string, payload json.RawMessage, at time.Time) error
	LatestHeartbeats(ctx context.Context) ([]HeartbeatLatest, error) // 每 session 最新一条,session 升序
	// skills
	ListSkills(ctx context.Context) ([]Skill, error)
	ProposeSkill(ctx context.Context, s Skill) error
	SetSkillStatus(ctx context.Context, name, version, status, by string) error
	// audit(只增;查询=coordinator)
	AppendAudit(ctx context.Context, e AuditEntry) error
	ListAudit(ctx context.Context, since time.Time, limit int) ([]AuditEntry, error)
	// 健康
	Ping(ctx context.Context) error
}
