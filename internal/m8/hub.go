// Package m8 implements the minimal service registry and prefix WS routing.
package m8

import (
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zhangzongchu2019/dingwei/internal/extshared"
	"github.com/zhangzongchu2019/dingwei/internal/tempgroup"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/coder/websocket"

	"github.com/zhangzongchu2019/dingwei/internal/agentmetrics"
	"github.com/zhangzongchu2019/dingwei/internal/changeflow"
	"github.com/zhangzongchu2019/dingwei/internal/coordinatereview"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/llm"
	"github.com/zhangzongchu2019/dingwei/internal/model"
	"github.com/zhangzongchu2019/dingwei/internal/obs"
	"github.com/zhangzongchu2019/dingwei/internal/router"
	"github.com/zhangzongchu2019/dingwei/internal/specconfirm"
	"github.com/zhangzongchu2019/dingwei/internal/specread"
	"github.com/zhangzongchu2019/dingwei/internal/store"
	"github.com/zhangzongchu2019/dingwei/internal/subagent"
	"github.com/zhangzongchu2019/dingwei/internal/trustedorigin"
	dwversion "github.com/zhangzongchu2019/dingwei/internal/version"
)

type MessageQueue interface {
	Enqueue(ctx context.Context, m model.Message) error
}

type SystemHandler interface {
	HandleSystemRequest(ctx context.Context, serviceName, action, body string, source model.Message) (string, error)
}

type aggregateWeeklyReviewHandler interface {
	HandleAggregateWeeklyReviewCommand(ctx context.Context, body string, source model.Message) (string, bool, error)
}

// ViewPageUnlocker is the capability boundary for page-scoped view unlock v2.
// A nil implementation keeps the capability disabled.
type ViewPageUnlocker interface {
	UnlockViewPage(ctx context.Context, request ViewPageUnlockRequest) error
}

type ViewPageUnlockRequest struct {
	Code            string
	FullSessionName string
	OwnerKey        string
	SenderOpenID    string
	MessageID       string
}

type Hub struct {
	ExternalSeats       ExternalSeats
	ExternalCopyReplies ExternalCopyReplyLookup
	TempGroups          *tempgroup.Service

	stickyLanes         stickyLanes
	stickyCleanupMu     sync.Mutex
	stickyCleanupCursor store.FeishuStickyKey

	lifecycle hubLifecycle
	// Startup-only R7 service. Nil keeps Git/committee operations fail-closed until trusted adapters exist.
	SpecReader               *specread.Service
	CoordinateReviews        *coordinatereview.Service
	CoordinateReviewEnabled  bool
	CoordinateReviewSender   QuestionSender
	coordinateReviewAuthMu   sync.RWMutex
	coordinateReviewWorkerMu sync.Mutex
	coordinateReviewCursor   string
	SpecConfirmations        *specconfirm.Service
	// Startup-only provider-neutral R1/R8 core; nil remains unavailable.
	ChangeEvidence *changeflow.Service

	projectContextCursorOnce sync.Once
	projectContextCursorKey  [32]byte
	projectContextCursorErr  error
	TrustProxy               bool // R31: only configured loopback nginx can supply X-Real-IP.
	consoleMux               *consoleMuxRegistry
	Repo                     store.Repository
	SessionHistory           *store.SessionHistory
	SessionHistoryEnabled    bool
	Outbound                 MessageQueue
	System                   SystemHandler
	L2                       llm.Provider
	L2Config                 L2Config
	ViewPageUnlocker         ViewPageUnlocker
	consoleAuthEnabled       bool
	pmSpecFlowEnabled        bool
	// consoleHostedEnabled 只在服务端实际挂载托管路由后置 true；页面据此决定是否渲染托管区。
	// 未挂路由时保持 false，避免回滚/降级部署向用户暴露一个必然 404 的坏入口。
	consoleHostedEnabled bool
	consoleHostedAccess  func(*http.Request) bool
	// 统一登录的一次性登录票据(T-113)。仅内存、短 TTL、与页面写锁 code 池分开。
	consoleLoginStore *consoleLoginStore
	consoleLoginOnce  sync.Once

	// Identity 新平台 G3：/ws/agent 按专属凭据认人（为空＝不启用该入口）。
	Identity *identity.Service
	// OnHostedAgentOnline 托管 Agent 以专属凭据连入、会话登记成功后异步回调（G4 接 orch.MarkOnline：starting → online，板 ac33b1cb）。
	// 自备 Agent 不回调。为空＝不回调。
	OnHostedAgentOnline func(ctx context.Context, agentID string)
	// PublicOrigin 新平台对外源（例如 https://dw.cmjrt.com:55055）：view 写操作与 WebSocket 升级校验 Origin 用。
	PublicOrigin   string
	TrustedOrigins []trustedorigin.Origin
	// Onboarding 新平台 N3：飞书里的登录与注册指令改由它处理（为空＝沿用旧登录）。
	Onboarding *identity.Onboarding
	// newPlatformShimRepo 测试缝：非空时垫片用它代替 h.Repo（注入写库失败，复验方 H15）。
	newPlatformShimRepo newPlatformShimRepository
	// AgentCredentialOnly 新平台模式：旧入口 /ws/session 一律回 410（只认凭据，设计件 2.3）；
	// /ws/service 只收本机与内网来源（台账 c796e65e）。
	AgentCredentialOnly bool

	// NowFunc 只为测试注入时钟。D1 的滑动续期要验「跨过 12 小时」,
	// 真等 12 小时不是验收,是浪费;而只看代码里有 UPDATE 又证明不了行为。
	NowFunc func() time.Time
	// IdleTickerFunc 只为测试注入会话空闲巡检的 tick（组长 R-M8-TESTSIDE 取甲：挂在实例上，不用包级变量）；
	// 返回 tick 通道与停止函数。nil ⇒ time.NewTicker（生产行为不变）。会话活跃时刻同用 h.now()（NowFunc）。
	IdleTickerFunc func(d time.Duration) (<-chan time.Time, func())
	// deliveryFailedBackoffBase／Cap／Max 可重试 failed 的退避与上限；0 ⇒ 默认 2s／300s／30（只为测试注入）。
	deliveryFailedBackoffBase time.Duration
	deliveryFailedBackoffCap  time.Duration
	deliveryFailedMax         int
	// keepaliveInterval／keepaliveTimeout 会话保活 ping 的间隔与等 pong 上限（组长 R-M8-KEEPALIVE-RACE：挂实例，
	// 原为包级变量，测试改写时与上一个会话仍在跑的保活协程竞争）。0 ⇒ 默认 30s／5s（生产行为不变）；间隔为负 ⇒ 不保活。
	keepaliveInterval time.Duration
	keepaliveTimeout  time.Duration

	// D1 滑动续期的写放大闸:同一会话在 consoleSessionTouchMin 内只写一次库。
	consoleTouchMu sync.Mutex
	consoleTouched map[string]time.Time

	// sessionWG 正在服务中的会话（serveSession）个数；Close 等它们收尾。
	// httptest.Server.Close 不等已升级为 WebSocket 的连接 ⇒ 不等的话，会话收尾（释放投递租约、写库、打日志）
	// 会拖到下一条用例里，撞进别人的日志捕获窗口（复验方 qwen38maxkhovvob4 的日志卫生用例即被打红过）。
	sessionWG             sync.WaitGroup
	taskFlowDispatchMu    sync.Mutex
	blockerDispatchMu     sync.Mutex
	questionDispatchMu    sync.Mutex
	QuestionSender        QuestionSender
	R9PMQuestionSender    QuestionSender
	questionReminderAfter string
	questionOutboxAfter   string

	// Startup-only D3 outbound opt-in; unrelated to spec clarification inbound.
	QuestionFeishuEnabled bool
	// Startup-only clarification flags. Reply ingestion never reads the D3
	// outbound opt-in; disabling either clarification flag does not undo policy.
	SpecClarificationEnabled            bool
	SpecClarificationFeishuReplyEnabled bool
	ClarificationReplyAuthorities       map[string]ClarificationReplyAuthority
	ClarificationReceiptSender          QuestionSender
	clarificationDispatchMu             sync.Mutex
	clarificationInboundAfter           int64

	mu                hubMutex
	serviceClients    map[string]*client
	sessionClients    map[string]map[string]*sessionClient
	keyAccounts       map[string]map[string]bool
	botChannels       map[string]string
	botNames          map[string]string
	envelopeIDs       map[string]string
	replyDedup        *replyDedup
	onlineTimers      map[string]*time.Timer
	seenSessions      map[string]bool
	seenSessionsOnce  sync.Once
	seenSessionsOK    bool
	retainedRingBytes int
	retainedRingFIFO  list.List
	terminals         map[string]*terminalState
	terminalLastSize  map[string][2]int
	syncTargets       map[string]map[string]feishuSyncTarget
	recentTerminal    map[string][]terminalSyncItem
	syncBuffers       map[string]*feishuSyncBuffer
	syncToolSummaries map[string]*terminalToolSummary
	linkTokens        map[string]ownerLinkToken
	onboardPushed     map[string]time.Time
	onboardContexts   map[string]onboardContext
	onlineDebounce    time.Duration
	onboardInterval   time.Duration
	newSessionNotice  bool
	viewV2Mu          sync.Mutex
	viewRetryMu       sync.Mutex
	unknownDeliveryMu sync.Mutex
	unknownDeliveries map[string]map[string]time.Time
	viewV2Nonces      map[string]time.Time
	viewV2Requests    map[string]viewV2RequestResult
	deliveryNotices   map[string]time.Time
	// capabilityPendingAdmissions covers only the small, live handshake window
	// after an endpoint has been recorded but before its WebSocket client is
	// published. It is deliberately in-memory, so an offline endpoint cannot
	// claim this admission window.
	capabilityPendingAdmissions  map[string]*capabilityPendingAdmission
	capabilityPendingCleanupStop chan struct{}
	capabilityPendingCleanupOnce sync.Once
	capabilityPendingCleanupWG   sync.WaitGroup
	// sessionBeforePublishHook is a test seam for the registered-but-unpublished
	// window. Production leaves it nil.
	sessionBeforePublishHook func(keyID, sessionName string)
	// freshTargetBeforePendingInsertHook pauses only a test immediately after a
	// route has observed the fresh admission, before it serializes its INSERT.
	// It makes the late-insert capability race reproducible without sleeps.
	freshTargetBeforePendingInsertHook func(keyID, sessionName string)
	// capabilityPendingCleanupBeforeExit is test-only: it lets Close prove it
	// waits for the pending-delivery cleanup goroutine to exit.
	capabilityPendingCleanupBeforeExit func()
	amr                                *amrProcessor
	mentionMirror                      *mentionMirror // R-V2-0103：view 提及的回复回流（只显示）
	startedAt                          time.Time
	restartWindow                      time.Duration
	fileIssuer                         FileTicketIssuer
	fileArchive                        FileArchiveClient
	filePermanent                      FilePermanentClient
	specPresplitDispatch               func(context.Context, model.ConsoleAgent, model.Envelope) error
	specInferenceDispatch              func(context.Context, model.ConsoleAgent, model.Envelope) error
	SpecPresplitTimeout                time.Duration

	// sessionTouchMinInterval（R-V2-0105 件三）：同一会话连接 last_seen 落库的最小间隔；0 = 不节流。
	sessionTouchMinInterval time.Duration
}

type L2Config struct {
	Workers             int
	PollInterval        time.Duration
	LeaseDuration       time.Duration
	ProviderTimeout     time.Duration
	ConfidenceThreshold float64
}

type client struct {
	conn *websocket.Conn
	mu   sync.Mutex // 串行化 forward 的「写请求→等回复」
	// HandleWS 的读协程是这条连接唯一的读者（组长 lead-m8-flake-findings ③）：回复经 resp 交给 forward；
	// readDone 在连接结束时关闭。
	resp     chan []byte
	readDone chan struct{}
}

type sessionClient struct {
	subAgentMu       sync.Mutex // serializes scoped writes with reports/closure
	subAgentsV1      bool       // stateMu; only current credential connection can negotiate
	subTreeReady     bool
	subTreeRevision  int64
	inboxLag         *inboxLagCache // Observational, bound to this authenticated connection.
	lastInboxAttempt time.Time
	lastInboxQuery   time.Time

	heartbeat            *sessionHeartbeatCache // Observational self-report, bound to this authenticated connection.
	lastHeartbeatAttempt time.Time              // Read/written under h.mu; rejected attempts do not renew the cache.

	metricsAgentID     string // Set only by dedicated credential admission, never helper JSON.
	metricsGeneration  string
	diskMetrics        *agentmetrics.Cache // Access to the pointer is under h.mu.
	lastMetricsAttempt time.Time           // Read/written under h.mu; rejected attempts do not renew the cache.

	conn               *websocket.Conn
	mu                 sync.Mutex
	stateMu            sync.Mutex
	keyID              string
	ownerKey           string
	sessionName        string
	localName          string // 客户端握手上报的原始名；sessionHelper 用它命名本机在线清单文件
	connSeq            int
	lastSeenAt         time.Time
	targetBot          string
	webTerminal        bool
	osName             string
	tool               string
	version            string
	skillInstalled     bool
	skillCancel        context.CancelFunc
	deliveryACKV1      bool
	deliveryACKV2      bool
	taskFlowV1         bool
	blockerNoticeV1    bool
	blockerWakeV1      bool
	specReviewV1       bool
	coordinateReviewV1 bool
	pmSpecFlowV1       bool
	helperUpdateV1     bool // authenticated helper explicitly supports update_self v1
	desktopInboxV1     bool
	helperReady        bool
	deviceID           string
	deviceIDV1         bool

	lastTouchWritten time.Time // R-V2-0105 件三：上次把 last_seen 落库的时刻（touchSessionEndpoint 节流）

	obsConn *obs.WSConn // DW123-PER-RELEASE-OBSERVABILITY：生命周期计数；nil 安全
}

type feishuSyncTarget struct {
	openID       string
	keyID        string
	botName      string
	botChannelID string
}

type terminalSyncItem struct {
	TS   time.Time
	Text string
}

type terminalToolSummary struct {
	total  int
	byName map[string]int
}

type feishuSyncBuffer struct {
	keyID       string
	sessionName string
	target      feishuSyncTarget
	items       []terminalSyncItem
	timer       *time.Timer
}

type ownerLinkToken struct {
	ownerKey  string
	expiresAt time.Time
}

type ForwardRequest struct {
	ID           string `json:"id"`
	ServiceID    string `json:"service_id"`
	ChatEntityID string `json:"chat_entity_id"`
	BotChannelID string `json:"bot_channel_id"`
	ChatType     string `json:"chat_type"`
	SenderOpenID string `json:"sender_open_id,omitempty"`
	Text         string `json:"text"`
	RawContent   string `json:"raw_content"`
}

type ForwardResponse struct {
	Reply string `json:"reply"`
	Error string `json:"error,omitempty"`
}

const (
	// systemAddressName 是保留的系统地址名,用于 helper<->Hub 的控制面信封(T-044)。
	// 它**不是会话名**:任何以它为目标的信封都终止于 Hub,绝不进入会话解析。
	systemAddressName = "workpulse"

	secOpsOwnerKey             = "systemtaskintl"
	secOpsMemberName           = "SYSTEM-V-TASK-INTERNAL"
	secOpsKeyword              = "#系统安全"
	agentNetworkSkillAck       = "DINGWEI_COMM_SKILL_INSTALLED"
	agentNetworkSkillPushType  = "agent_network_skill"
	agentNetworkSkillAckType   = "agent_network_skill_ack"
	agentNetworkSkillRetryWait = 2 * time.Minute
	ownerLinkTokenTTL          = 8 * time.Hour
)

var (
	// sessionReadIdleTimeout is the maximum interval between application data or
	// WebSocket ping/pong activity. It must not be used as a Read deadline:
	// coder/websocket closes the whole connection when a Read context expires.
	sessionReadIdleTimeout = 75 * time.Second
	// A 15 second cadence bounds an idle disconnect to 90 seconds while avoiding
	// a per-connection timer for every activity update.
	sessionIdleWatchdogInterval = 15 * time.Second
	// keepalivePingHook 仅供用例观察保活 Ping 的起止（done=false 发出前，true 返回后）；生产为 nil。
	keepalivePingHook atomic.Pointer[func(done bool)]
	// agentDisconnectWait DisconnectAgent 等关闭握手的上限：关闭帧发出即生效（之后该连接一切写都失败），
	// 对端不读时库会等满 5 秒握手 —— 不让吊销／暂停请求陪着等（组长 lead-m8-flake-findings ②）。
	agentDisconnectWait = 500 * time.Millisecond
)

var (
	secOpsAdminOpenID      = strings.TrimSpace(os.Getenv("WP_SECOPS_ADMIN_OPENID"))
	secOpsAdminOwnerKey    = strings.TrimSpace(os.Getenv("WP_SECOPS_ADMIN_OWNER_KEY"))
	applyKeyApproverID     = strings.TrimSpace(os.Getenv("WP_APPLY_KEY_APPROVER_OPENID"))
	ansiCSIRE              = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)
	ansiOSCSTRE            = regexp.MustCompile(`\x1b\][^\x07]*(\x07|\x1b\\)`)
	ansiSimpleRE           = regexp.MustCompile(`\x1b[@-Z\\-_]`)
	controlExceptTextRE    = regexp.MustCompile(`[\x00-\x08\x0b\x0c\x0e-\x1f\x7f]`)
	bareSessionNamePattern = regexp.MustCompile(`^[a-z0-9]+$`)
	sessionNamePattern     = regexp.MustCompile(`^[a-z0-9]+-[a-z0-9]+-[0-9a-f]{4}$`)
	deviceIDPattern        = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

func New(repo store.Repository, consoleAuthMode ConsoleAuthMode) *Hub {
	h := &Hub{
		Repo:                          repo,
		ClarificationReplyAuthorities: map[string]ClarificationReplyAuthority{},
		serviceClients:                map[string]*client{},
		sessionClients:                map[string]map[string]*sessionClient{},
		keyAccounts:                   map[string]map[string]bool{},
		botChannels:                   map[string]string{},
		botNames:                      map[string]string{},
		envelopeIDs:                   map[string]string{},
		replyDedup:                    newReplyDedup(envDuration("REPLY_ORIGIN_DEDUP_TTL", replyDedupDefaultTTL)),
		onlineTimers:                  map[string]*time.Timer{},
		seenSessions:                  map[string]bool{},
		terminals:                     map[string]*terminalState{},
		terminalLastSize:              map[string][2]int{},
		syncTargets:                   map[string]map[string]feishuSyncTarget{},
		recentTerminal:                map[string][]terminalSyncItem{},
		syncBuffers:                   map[string]*feishuSyncBuffer{},
		syncToolSummaries:             map[string]*terminalToolSummary{},
		linkTokens:                    map[string]ownerLinkToken{},
		onboardPushed:                 map[string]time.Time{},
		consoleTouched:                map[string]time.Time{},
		onboardContexts:               map[string]onboardContext{},
		viewV2Nonces:                  map[string]time.Time{},
		viewV2Requests:                map[string]viewV2RequestResult{},
		deliveryNotices:               map[string]time.Time{},
		unknownDeliveries:             map[string]map[string]time.Time{},
		capabilityPendingAdmissions:   map[string]*capabilityPendingAdmission{},
		startedAt:                     time.Now().UTC(),
		restartWindow:                 envDuration("WP_RESTART_STABILITY_WINDOW", time.Minute),
		onlineDebounce:                2500 * time.Millisecond,
		onboardInterval:               envDuration("WP_ONBOARD_PUSH_INTERVAL", time.Hour),
		newSessionNotice:              true,
		consoleAuthEnabled:            consoleAuthMode.Enabled(),
		pmSpecFlowEnabled:             strings.TrimSpace(os.Getenv("DW_PM_SPEC_FLOW_ENABLED")) == "1",
		L2Config: L2Config{
			Workers:             4,
			PollInterval:        200 * time.Millisecond,
			LeaseDuration:       5 * time.Minute,
			ProviderTimeout:     60 * time.Second,
			ConfidenceThreshold: 0.60,
		},
	}
	h.amr = newAMRProcessor(h)
	h.mentionMirror = newMentionMirror(h)
	h.sessionTouchMinInterval = envDuration("WP_SESSION_TOUCH_MIN_INTERVAL", 5*time.Second)
	h.applyReplayGuardSettings()
	h.startCapabilityPendingCleanup()
	h.configureSpecRead()
	h.startFeishuStickyCleanup()
	h.startR9PMScheduler()
	return h
}

// applyReplayGuardSettings（R-V2-0105 件三）：R5 去重总量上限取 env；AMR 已完成记忆须不短于 R5 去重窗
// （否则过期的旧回合会在去重窗内被重开、重路由——dw2 720 s 节律的成因），不满足即抬到去重窗并留痕；
// 启动打一行实际取值供运维核对。
func (h *Hub) applyReplayGuardSettings() {
	if h.replyDedup != nil {
		h.replyDedup.max = envInt("REPLY_ORIGIN_DEDUP_MAX", replyDedupDefaultMax)
	}
	if h.amr != nil && h.replyDedup != nil && h.amr.completedTTL < h.replyDedup.ttl {
		log.Printf("replay guard: WP_AMR_COMPLETED_TTL=%s shorter than REPLY_ORIGIN_DEDUP_TTL=%s; raised to %s",
			h.amr.completedTTL, h.replyDedup.ttl, h.replyDedup.ttl)
		h.amr.completedTTL = h.replyDedup.ttl
	}
	if h.amr != nil && h.replyDedup != nil {
		log.Printf("replay guard: amr_completed_ttl=%s amr_completed_per_session=%d amr_completed_total=%d reply_dedup_ttl=%s reply_dedup_max=%d session_touch_min_interval=%s",
			h.amr.completedTTL, h.amr.maxCompleted, h.amr.maxCompletedTotal, h.replyDedup.ttl, h.replyDedup.max, h.sessionTouchMinInterval)
	}
}

func (h *Hub) StartL2Workers(ctx context.Context) {
	if h.Repo == nil || h.L2 == nil {
		return
	}
	cfg := h.effectiveL2Config()
	for i := 0; i < cfg.Workers; i++ {
		workerID := fmt.Sprintf("l2-%d-%s", i+1, randomHex(4))
		h.backgroundWork("owned", func() { h.runL2Worker(ctx, workerID, cfg) })
	}
}

func (h *Hub) effectiveL2Config() L2Config {
	cfg := h.L2Config
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 200 * time.Millisecond
	}
	if cfg.LeaseDuration <= 0 {
		cfg.LeaseDuration = 5 * time.Minute
	}
	if cfg.ProviderTimeout <= 0 {
		cfg.ProviderTimeout = 60 * time.Second
	}
	if cfg.ConfidenceThreshold <= 0 {
		cfg.ConfidenceThreshold = 0.60
	}
	return cfg
}

func (h *Hub) runL2Worker(ctx context.Context, workerID string, cfg L2Config) {
	for {
		task, err := h.Repo.ClaimNextL2ControlTask(ctx, workerID, time.Now().UTC().Add(cfg.LeaseDuration), time.Now().UTC())
		if err == nil && task == nil {
			task, err = h.Repo.ClaimNextAggregateControlTask(ctx, workerID, time.Now().UTC().Add(cfg.LeaseDuration), time.Now().UTC())
		}
		if err != nil || task == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(cfg.PollInterval):
				continue
			}
		}
		h.processL2Task(ctx, *task, cfg)
	}
}

func (h *Hub) processL2Task(ctx context.Context, task model.ControlTask, cfg L2Config) {
	start := time.Now()
	var err error
	if task.Status == "aggregating" {
		err = h.aggregateControlParent(ctx, task, start)
	} else {
		var triageCtx model.L2TriageContext
		triageCtx, err = h.buildL2TriageContext(ctx, task)
		if err == nil {
			err = h.dispatchL2Triage(ctx, task, triageCtx, cfg, start)
		}
	}
	duration := time.Since(start)
	if err == nil {
		return
	}
	_ = h.Repo.RecordControlTaskL2Failure(ctx, task.ID, err.Error(), duration)
	var retried *model.ControlTask
	var retryErr error
	if task.Status == "aggregating" {
		retried, retryErr = h.Repo.RetryAggregatingControlTask(ctx, task.ID, err.Error())
	} else {
		retried, retryErr = h.Repo.RetryL2ControlTask(ctx, task.ID, err.Error())
	}
	if retryErr != nil || retried == nil || retried.Status != "failed" {
		return
	}
	_ = h.notifyControlTask(ctx, *retried, controlTaskFailedReply(*retried))
}

func (h *Hub) buildL2TriageContext(ctx context.Context, task model.ControlTask) (model.L2TriageContext, error) {
	sessions, err := h.l2OnlineSessions(ctx, task.OwnerKey)
	if err != nil {
		return model.L2TriageContext{}, err
	}
	return model.L2TriageContext{
		RequestID:      task.ID,
		RawInput:       task.RawInput,
		Source:         task.Source,
		OwnerKey:       task.OwnerKey,
		OnlineSessions: sessions,
	}, nil
}

func (h *Hub) l2OnlineSessions(ctx context.Context, ownerKey string) ([]model.L2OnlineSession, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	var out []model.L2OnlineSession
	for _, ep := range endpoints {
		if !ep.Active || ep.OwnerKey != ownerKey || ep.NoDirectory || !h.sessionOnline(ep.KeyID, ep.SessionName) {
			continue
		}
		out = append(out, model.L2OnlineSession{
			Session: ep.SessionName,
			Tool:    ep.Tool,
			Model:   ep.Model,
			Role:    ep.TargetGroup,
			Busy:    false,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out, nil
}

func (h *Hub) dispatchL2Triage(ctx context.Context, task model.ControlTask, input model.L2TriageContext, cfg L2Config, startedAt time.Time) error {
	system := strings.Join([]string{
		"你是 DingWei 平台总控 L2 分诊器。只返回 JSON。",
		"schema: {\"intent\":\"dispatch|clarify|reject|decompose|aggregate\",\"reply\":\"string\",\"targets\":[{\"session\":\"string\",\"instruction\":\"string\"}],\"subtasks\":[],\"confidence\":0.0}",
		"支持 dispatch、clarify、decompose；信息不足或低置信度返回 clarify。",
		"dispatch 必须且只能选择一个 online_sessions 中存在的 session；decompose 的 subtasks 必须全部指向 online_sessions。",
	}, "\n")
	userBytes, _ := json.Marshal(input)
	callCtx, cancel := context.WithTimeout(ctx, cfg.ProviderTimeout)
	defer cancel()
	out, err := h.L2.Complete(callCtx, system, string(userBytes))
	if err != nil {
		return err
	}
	result, err := parseL2TriageResult(out)
	if err != nil {
		return err
	}
	if result.Confidence < cfg.ConfidenceThreshold {
		result.Intent = "clarify"
		if strings.TrimSpace(result.Reply) == "" {
			result.Reply = "我还不确定该派给谁，请补充目标会话或更具体的任务背景。"
		}
	}
	switch result.Intent {
	case "dispatch":
		return h.completeL2Dispatch(ctx, task, result, startedAt)
	case "decompose":
		return h.completeL2Decompose(ctx, task, result)
	case "clarify":
		return h.completeL2Clarify(ctx, task, result, startedAt)
	default:
		result.Intent = "clarify"
		result.Reply = "该任务类型暂不支持自动分解，请指定一个明确会话或补充目标。"
		return h.completeL2Clarify(ctx, task, result, startedAt)
	}
}

func (h *Hub) completeL2Decompose(ctx context.Context, task model.ControlTask, result model.L2TriageResult) error {
	if len(result.Subtasks) == 0 {
		result.Intent = "clarify"
		result.Reply = "我无法拆出明确子任务，请补充目标和分工。"
		return h.completeL2Clarify(ctx, task, result, time.Now())
	}
	targetJSON, _ := json.Marshal(result.Subtasks)
	parent := task
	parent.Target = string(targetJSON)
	var children []model.ControlTask
	now := time.Now().UTC()
	for i, sub := range result.Subtasks {
		sub.Session = strings.TrimSpace(sub.Session)
		sub.Instruction = strings.TrimSpace(sub.Instruction)
		if sub.Session == "" || sub.Instruction == "" {
			return fmt.Errorf("invalid subtask %d", i+1)
		}
		childID := fmt.Sprintf("%s-sub-%02d", task.ID, i+1)
		childTarget, _ := json.Marshal([]model.L2Target{sub})
		children = append(children, model.ControlTask{
			ID:           childID,
			ParentID:     task.ID,
			CreatedAt:    now.Add(time.Duration(i) * time.Nanosecond),
			UpdatedAt:    now,
			Source:       "session",
			SourceAddr:   "",
			OwnerKey:     task.OwnerKey,
			BotChannelID: task.BotChannelID,
			RawInput:     sub.Instruction,
			Intent:       "dispatch",
			Layer:        "L2",
			Target:       string(childTarget),
			Status:       "awaiting_result",
			Priority:     task.Priority,
			MaxAttempts:  task.MaxAttempts,
			ExpireAt:     task.ExpireAt,
		})
	}
	if err := h.Repo.CreateControlSubtasks(ctx, parent, children); err != nil {
		return err
	}
	for i, child := range children {
		if err := h.routeL2Target(ctx, child, result.Subtasks[i]); err != nil {
			_, _ = h.Repo.CompleteControlSubtask(ctx, child.ID, "", "failed", err.Error())
		}
	}
	_, err := h.tryAggregateParent(ctx, task.ID)
	return err
}

func (h *Hub) CompleteControlSubtask(ctx context.Context, id, result, status, errText string) error {
	child, err := h.Repo.CompleteControlSubtask(ctx, id, result, status, errText)
	if err != nil || child == nil || child.ParentID == "" {
		return err
	}
	_, err = h.tryAggregateParent(ctx, child.ParentID)
	return err
}

func (h *Hub) tryAggregateParent(ctx context.Context, parentID string) (*model.ControlTask, error) {
	parent, err := h.Repo.TryClaimControlParentForAggregation(ctx, parentID, "aggregate-"+randomHex(4), time.Now().UTC().Add(h.effectiveL2Config().LeaseDuration), time.Now().UTC())
	if err != nil || parent == nil {
		return parent, err
	}
	err = h.aggregateControlParent(ctx, *parent, time.Now())
	if err != nil {
		retried, retryErr := h.Repo.RetryAggregatingControlTask(ctx, parent.ID, err.Error())
		if retryErr != nil {
			return parent, retryErr
		}
		if retried != nil && retried.Status == "failed" {
			_ = h.notifyControlTask(ctx, *retried, controlTaskFailedReply(*retried))
		}
		return parent, err
	}
	return parent, nil
}

func (h *Hub) aggregateControlParent(ctx context.Context, parent model.ControlTask, startedAt time.Time) error {
	children, err := h.Repo.ListControlSubtasks(ctx, parent.ID)
	if err != nil {
		return err
	}
	input := map[string]any{
		"request_id": parent.ID,
		"intent":     "aggregate",
		"raw_input":  parent.RawInput,
		"owner_key":  parent.OwnerKey,
		"children":   children,
	}
	system := "你是 DingWei 平台总控聚合器。只返回 JSON: {\"intent\":\"aggregate\",\"reply\":\"给发起人的综合回复\",\"confidence\":0.0}。即使部分子任务 failed/expired，也要说明完成与失败情况。"
	userBytes, _ := json.Marshal(input)
	callCtx, cancel := context.WithTimeout(ctx, h.effectiveL2Config().ProviderTimeout)
	defer cancel()
	out, err := h.L2.Complete(callCtx, system, string(userBytes))
	if err != nil {
		return err
	}
	result, err := parseL2TriageResult(out)
	if err != nil {
		return err
	}
	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		reply = aggregateFallbackReply(children)
	}
	if err := h.Repo.CompleteControlTaskL2(ctx, parent.ID, "aggregate", parent.Target, reply, time.Since(startedAt)); err != nil {
		return err
	}
	return h.notifyControlTask(ctx, model.ControlTask{ID: parent.ID, Status: "done", SourceAddr: parent.SourceAddr, BotChannelID: parent.BotChannelID}, reply)
}

func aggregateFallbackReply(children []model.ControlTask) string {
	done, failed := 0, 0
	for _, child := range children {
		switch child.Status {
		case "done":
			done++
		case "failed", "expired":
			failed++
		}
	}
	if failed > 0 {
		return fmt.Sprintf("子任务已完成 %d 个，失败/超时 %d 个，请查看详情。", done, failed)
	}
	return fmt.Sprintf("子任务已全部完成，共 %d 个。", done)
}

func parseL2TriageResult(raw string) (model.L2TriageResult, error) {
	var result model.L2TriageResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &result); err != nil {
		return result, err
	}
	result.Intent = strings.TrimSpace(result.Intent)
	for i := range result.Targets {
		result.Targets[i].Session = strings.TrimSpace(result.Targets[i].Session)
		result.Targets[i].Instruction = strings.TrimSpace(result.Targets[i].Instruction)
	}
	return result, nil
}

func (h *Hub) completeL2Dispatch(ctx context.Context, task model.ControlTask, result model.L2TriageResult, completedAt time.Time) error {
	if len(result.Targets) != 1 || result.Targets[0].Session == "" || result.Targets[0].Instruction == "" {
		result.Intent = "clarify"
		result.Reply = "我还不能确定唯一目标，请用 #会话名 内容 指定。"
		return h.completeL2Clarify(ctx, task, result, completedAt)
	}
	target := result.Targets[0]
	if err := h.routeL2Target(ctx, task, target); err != nil {
		return err
	}
	targetJSON, _ := json.Marshal(result.Targets)
	reply := result.Reply
	if strings.TrimSpace(reply) == "" {
		reply = fmt.Sprintf("已分诊给 #%s。", target.Session)
	}
	if err := h.Repo.CompleteControlTaskL2(ctx, task.ID, "dispatch", string(targetJSON), reply, time.Since(completedAt)); err != nil {
		return err
	}
	return h.notifyControlTask(ctx, model.ControlTask{ID: task.ID, Status: "done", SourceAddr: task.SourceAddr, BotChannelID: task.BotChannelID}, reply)
}

func (h *Hub) completeL2Clarify(ctx context.Context, task model.ControlTask, result model.L2TriageResult, completedAt time.Time) error {
	reply := strings.TrimSpace(result.Reply)
	if reply == "" {
		reply = "请补充目标会话或更具体的任务背景。"
	}
	if err := h.Repo.CompleteControlTaskL2(ctx, task.ID, "clarify", "", reply, time.Since(completedAt)); err != nil {
		return err
	}
	return h.notifyControlTask(ctx, model.ControlTask{ID: task.ID, Status: "done", SourceAddr: task.SourceAddr, BotChannelID: task.BotChannelID}, reply)
}

func (h *Hub) routeL2Target(ctx context.Context, task model.ControlTask, target model.L2Target) error {
	keyID, found, err := h.l2SessionKey(ctx, task.OwnerKey, target.Session)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("target session %s is not online for owner %s", target.Session, task.OwnerKey)
	}
	env := model.Envelope{
		ID:   task.ID + "-l2-dispatch",
		To:   sessionAddress(target.Session, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: target.Instruction,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"system": true, "control_task_id": task.ID},
	}
	return h.RouteEnvelope(ctx, env)
}

func (h *Hub) l2SessionKey(ctx context.Context, ownerKey, sessionName string) (string, bool, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", false, err
	}
	for _, ep := range endpoints {
		if !ep.Active || ep.OwnerKey != ownerKey || ep.SessionName != sessionName || ep.NoDirectory || !h.sessionOnline(ep.KeyID, ep.SessionName) {
			continue
		}
		return ep.KeyID, true, nil
	}
	return "", false, nil
}

func (h *Hub) RegisterBot(botChannelID, botName string) {
	if botName == "" {
		botName = botChannelID
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.botChannels[botName] = botChannelID
	// Historical envelopes persist the old display name in their three-part
	// Feishu address. Keep that name as a read alias while the channel's
	// canonical display name and reverse mapping remain dingwei.
	if botName == "dingwei" {
		h.botChannels["GreyTestRobot"] = botChannelID
	}
	h.botNames[botChannelID] = botName
}

func (h *Hub) UpsertService(ctx context.Context, svc model.RegisteredService) error {
	if svc.DeliveryType == "" {
		svc.DeliveryType = "ws"
	}
	if svc.ReplyMode == "" {
		svc.ReplyMode = "sync"
	}
	if !svc.Enabled {
		svc.Enabled = true
	}
	return h.Repo.UpsertRegisteredService(ctx, svc)
}

func (h *Hub) IssueAPIKey(ctx context.Context, serviceID, label string) (secret string, meta model.ServiceAPIKey, err error) {
	secret, meta = h.newAPIKey(serviceID, label)
	return secret, meta, h.Repo.InsertServiceAPIKey(ctx, meta)
}

func (h *Hub) newAPIKey(serviceID, label string) (secret string, meta model.ServiceAPIKey) {
	secret = "wp_" + randomHex(32)
	meta = model.ServiceAPIKey{
		ID:        newKeyID(label, serviceID),
		ServiceID: serviceID,
		KeyHash:   HashAPIKey(secret),
		Label:     label,
		Active:    true,
		CreatedAt: time.Now().UTC(),
	}
	return secret, meta
}

func HashAPIKey(plain string) string {
	sum := sha256.Sum256([]byte(plain))
	return hex.EncodeToString(sum[:])
}

func (h *Hub) BindAccount(ctx context.Context, keyID, chatEntityID string) error {
	if err := h.Repo.BindAPIKeyAccount(ctx, keyID, chatEntityID); err != nil {
		return err
	}
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.keyAccounts, keyID)
	}()
	return nil
}

func (h *Hub) AddPrefixRule(ctx context.Context, rule model.RoutingRule) error {
	rule.MatchType = "prefix"
	if rule.Combine == "" {
		rule.Combine = "or"
	}
	if !rule.Enabled {
		rule.Enabled = true
	}
	if err := validatePrefixRule(rule); err != nil {
		return err
	}
	if err := h.ensureNoPrefixOverlap(ctx, rule); err != nil {
		return err
	}
	return h.Repo.InsertRoutingRule(ctx, rule)
}

func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	registrationDone, handlerDone, ok := h.beginRegistration(w, "service_ws")
	if !ok {
		return
	}
	defer registrationDone()
	defer handlerDone()
	if h.AgentCredentialOnly && !isInternalSource(r) {
		writeAgentWSError(w, http.StatusForbidden, "internal_only")
		return
	}
	serviceID := r.PathValue("serviceID")
	key := r.URL.Query().Get("api_key")
	if key == "" {
		key = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	}
	meta, err := h.Repo.ResolveServiceAPIKey(r.Context(), HashAPIKey(key))
	if err != nil || meta == nil || meta.ServiceID != serviceID {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer h.trackSocket(conn, "service_ws")()
	obsConn := obs.Default.OpenWS("service")
	var obsReadErr, svcReadErr error
	defer func() { obsConn.Closed(obsReadErr) }()
	c := &client{conn: conn, resp: make(chan []byte, 1), readDone: make(chan struct{})}
	var oldConn *websocket.Conn
	h.mu.Lock()
	if old := h.serviceClients[serviceID]; old != nil {
		oldConn = old.conn
	}
	h.serviceClients[serviceID] = c
	h.mu.Unlock()
	// 关闭旧连接必须在锁外:Close 会写关闭帧,对端半死时可阻塞数秒,
	// 持 h.mu 期间阻塞会让整个 hub 停摆(与 365b3a2 修的 HandleSessionWS 同型)。
	if oldConn != nil {
		_ = oldConn.Close(websocket.StatusPolicyViolation, "replaced")
	}
	defer func() {
		h.mu.Lock()
		if h.serviceClients[serviceID] == c {
			delete(h.serviceClients, serviceID)
		}
		h.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "bye")
	}()
	// 🔴 建连后必须一直有人读（组长 lead-m8-flake-findings ③）：原先只有 forward 在途时才 Read，
	//    空闲时对端发来的关闭帧／ping 无人处理 —— 对端优雅关闭必等满 5 秒，Hub 也察觉不到对端已走。
	h.backgroundWork("owned", func() {
		func() {
			defer close(c.readDone)
			for {
				_, data, err := conn.Read(context.Background())
				if err != nil {
					svcReadErr = obsWSReadErr(context.Background(), err) // 只在 readDone 关闭后由主协程读取
					return
				}
				// 🔴 签注（组长 R-HUB-WSB-COMMENT）：服务只许应答，不得主动发帧。c.resp 容量 1：无请求在途时的主动帧会进缓冲，
				//    被下一次调用当成答案（之后整体错位）。缓冲 1 是为了接住 forward 写完请求、尚未进入接收时就先到的快回复；
				//    缓冲已满（不合协议的多余帧）才丢弃。超时的调用已关掉连接，迟到回复不会留到下一次调用。
				select {
				case c.resp <- data:
				default:
				}
			}
		}()
	})
	registrationDone()
	select {
	case <-r.Context().Done():
		obsReadErr = obs.ContextDone()
	case <-c.readDone:
		obsReadErr = svcReadErr
	}
}

func (h *Hub) HandleSessionWS(w http.ResponseWriter, r *http.Request) {
	if h.AgentCredentialOnly {
		writeAgentWSError(w, http.StatusGone, "use_ws_agent")
		return
	}
	timingStart := time.Now()
	timingLast := timingStart
	var timingParts []string
	markTiming := func(name string) {
		now := time.Now()
		timingParts = append(timingParts, fmt.Sprintf("%s=%s", name, now.Sub(timingLast).Round(time.Millisecond)))
		timingLast = now
	}
	requestedSessionName := strings.TrimSpace(r.PathValue("sessionName"))
	if requestedSessionName == "" || strings.Contains(requestedSessionName, "#") {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	keyID := strings.TrimSpace(r.URL.Query().Get("key_id"))
	secret := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if keyID == "" || strings.TrimSpace(secret) == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	secretHash := HashAPIKey(secret)
	meta, err := h.Repo.ResolveServiceAPISecret(r.Context(), keyID, secretHash)
	if err != nil || meta == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	markTiming("auth")
	accounts, err := h.Repo.ListAPIKeyAccounts(r.Context(), meta.ID)
	if err != nil {
		http.Error(w, "load key accounts failed", http.StatusInternalServerError)
		return
	}
	markTiming("accounts")
	clientIP := clientIPFromRequest(r)
	tool := strings.TrimSpace(r.URL.Query().Get("tool"))
	modelName := strings.TrimSpace(r.URL.Query().Get("model"))
	clientVersion := strings.TrimSpace(r.URL.Query().Get("version"))
	deviceID := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("device_id")))
	deviceIDV1 := truthyQuery(r.URL.Query().Get("device_id_v1")) && deviceIDPattern.MatchString(deviceID)
	versionWarn := versionCompatibilityWarning(clientVersion)
	fullSessionName := strings.TrimSpace(r.URL.Query().Get("full_session_name"))
	producer := truthyQuery(r.URL.Query().Get("producer"))
	noDirectory := truthyQuery(r.URL.Query().Get("no_directory"))
	targetGroup := strings.TrimSpace(r.URL.Query().Get("target_group"))
	targetBot := strings.TrimSpace(r.URL.Query().Get("target_bot"))
	webTerminal := truthyQuery(r.URL.Query().Get("terminal"))
	osName := strings.TrimSpace(r.URL.Query().Get("os"))
	mirrorTo := sessionMirrorToFromRequest(r)
	ownerKey := h.ownerKeyForKeyWithAccounts(r.Context(), keyID, accounts)
	markTiming("owner")
	sessionName := deriveRegisteredSessionName(requestedSessionName, keyID, ownerKey)
	if deviceIDV1 {
		h.mu.Lock()
		old := h.sessionClients[keyID][sessionName]
		occupied := old != nil && old.deviceIDV1 && old.deviceID != deviceID
		h.mu.Unlock()
		if occupied {
			http.Error(w, "session held by another device", http.StatusConflict)
			return
		}
	}
	nameWarn := sessionNameRequestWarning(requestedSessionName, keyID, ownerKey)
	if agentHandshakeEnforced() && nameWarn != "" {
		http.Error(w, nameWarn, http.StatusBadRequest)
		return
	}
	switch sessionNameEnforceMode() {
	case "enforce":
		if nameWarn != "" {
			http.Error(w, nameWarn, http.StatusBadRequest)
			return
		}
	case "warn":
		if nameWarn != "" {
			log.Printf("session name warning key_id=%s owner=%s session=%s: %s", keyID, ownerKey, requestedSessionName, nameWarn)
		}
	}
	switch versionEnforceMode() {
	case "enforce":
		if versionWarn != "" {
			http.Error(w, versionRejectionMessage(clientVersion, versionWarn), http.StatusBadRequest)
			return
		}
	case "warn":
		if versionWarn != "" {
			log.Printf("session version warning key_id=%s owner=%s session=%s version=%q required=%s: %s", keyID, ownerKey, requestedSessionName, clientVersion, dwversion.Version, versionWarn)
		}
	}
	h.serveSession(w, r, sessionAdmission{
		keyID: keyID, ownerKey: ownerKey, requestedSessionName: requestedSessionName, sessionName: sessionName,
		fullSessionName: fullSessionName, accounts: accounts, clientIP: clientIP, tool: tool, modelName: modelName,
		clientVersion: clientVersion, producer: producer, targetGroup: targetGroup, noDirectory: noDirectory,
		mirrorTo: mirrorTo, targetBot: targetBot, webTerminal: webTerminal, osName: osName,
		deviceID: deviceID, deviceIDV1: deviceIDV1,
		timingStart: timingStart, timingLast: timingLast, timingParts: timingParts,
	})
}

// sessionAdmission 是「已认出是谁」之后进入会话所需的全部输入。
// HandleSessionWS（账号 key＋自报名字，旧平台）与 HandleAgentWS（专属凭据，新平台 G3）各自认人，然后交给 serveSession。
// 桥接裁定见台账 ruling-2026-10-01T090343Z-lead-ws-agent-bridge。
type sessionAdmission struct {
	keyID, ownerKey, requestedSessionName, sessionName, fullSessionName string
	accounts                                                            []string
	clientIP, tool, modelName, clientVersion, targetGroup, mirrorTo     string
	targetBot, osName, deviceID                                         string
	producer, noDirectory, webTerminal, deviceIDV1                      bool
	timingStart, timingLast                                             time.Time
	timingParts                                                         []string
	// onRegistered 会话登记成功（未被设备冲突拒绝、旧连接已顶掉）后异步调用一次；为空不调。
	onRegistered func()

	// credentialIdentity 为真＝身份由专属凭据认定（HandleAgentWS）：信封里自报的 from 不是身份的来源，见 serveSession 读循环里的改写。
	credentialIdentity bool
	metricsAgentID     string
}

// serveSession 的函数体是从 HandleSessionWS 原样搬来的（逐字未改），开头只做同名解包。
func (h *Hub) serveSession(w http.ResponseWriter, r *http.Request, a sessionAdmission) {
	registrationDone, handlerDone, ok := h.beginRegistration(w, "session_ws")
	if !ok {
		return
	}
	defer registrationDone()
	defer handlerDone()
	h.sessionWG.Add(1)
	defer h.sessionWG.Done()
	keyID, ownerKey, requestedSessionName, sessionName, fullSessionName := a.keyID, a.ownerKey, a.requestedSessionName, a.sessionName, a.fullSessionName
	accounts, clientIP, tool, modelName, clientVersion := a.accounts, a.clientIP, a.tool, a.modelName, a.clientVersion
	producer, targetGroup, noDirectory, mirrorTo := a.producer, a.targetGroup, a.noDirectory, a.mirrorTo
	credentialIdentity := a.credentialIdentity
	targetBot, webTerminal, osName, deviceID, deviceIDV1 := a.targetBot, a.webTerminal, a.osName, a.deviceID, a.deviceIDV1
	timingStart, timingLast, timingParts := a.timingStart, a.timingLast, a.timingParts
	markTiming := func(name string) {
		now := time.Now()
		timingParts = append(timingParts, fmt.Sprintf("%s=%s", name, now.Sub(timingLast).Round(time.Millisecond)))
		timingLast = now
	}
	var err error
	h.loadSeenSessions(r.Context())
	// Only a brand-new endpoint has an unknown delivery_ack_v1 capability. A
	// reconnect of a previously recorded no-ACK endpoint must keep the existing
	// visible failure behavior rather than entering the fresh-target queue.
	endpointPreviouslyKnown := false
	if h.Repo != nil {
		_, endpointPreviouslyKnown, err = h.Repo.SessionEndpointDeliveryACK(r.Context(), keyID, sessionName)
		if err != nil {
			http.Error(w, "load session endpoint failed", http.StatusInternalServerError)
			return
		}
	}
	sessionName, err = h.registerSessionEndpoint(r.Context(), model.SessionEndpoint{
		KeyID:           keyID,
		SessionName:     sessionName,
		FullSessionName: fullSessionName,
		OwnerKey:        ownerKey,
		LastSeenAt:      time.Now().UTC(),
		Active:          true,
		ClientIP:        clientIP,
		PrivateClientIP: a.credentialIdentity,
		Tool:            tool,
		Model:           modelName,
		Version:         clientVersion,
		Producer:        producer,
		TargetGroup:     targetGroup,
		NoDirectory:     noDirectory,
		MirrorEnabled:   mirrorTo != "",
		MirrorTo:        mirrorTo,
	})
	if err != nil {
		http.Error(w, "save session endpoint failed", http.StatusInternalServerError)
		return
	}
	markTiming("register")
	var c *sessionClient
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true,
		OnPingReceived: func(ctx context.Context, payload []byte) bool {
			if c != nil {
				c.recordActivity(h.now().UTC())
				h.backgroundWork("owned", func() { h.touchSessionEndpoint(context.Background(), c) })
			}
			return true
		},
		OnPongReceived: func(ctx context.Context, payload []byte) {
			if c != nil {
				c.recordActivity(h.now().UTC())
				h.backgroundWork("owned", func() { h.touchSessionEndpoint(context.Background(), c) })
			}
		},
	})
	if err != nil {
		return
	}
	markTiming("accept")
	defer h.trackSocket(conn, "session_ws")()
	obsConn := obs.Default.OpenWS("session")
	var obsReadErr error
	defer func() { obsConn.Closed(obsReadErr) }()
	if !endpointPreviouslyKnown {
		admission := h.beginCapabilityPendingAdmission(keyID, sessionName)
		defer h.endCapabilityPendingAdmission(keyID, sessionName, admission)
	}
	conn.SetReadLimit(16 << 20) // 终端 PTY 全屏输出可能远超默认 32KB
	connSeq, err := h.Repo.IncrementSessionEndpointConnSeq(r.Context(), keyID, sessionName)
	if err != nil {
		obsConn.SetLocalReason("rejected")
		if conn != nil {
			_ = conn.Close(websocket.StatusInternalError, "save session endpoint failed")
		}
		return
	}
	markTiming("conn_seq")
	c = &sessionClient{conn: conn, keyID: keyID, ownerKey: ownerKey, sessionName: sessionName, localName: requestedSessionName, connSeq: connSeq, lastSeenAt: h.now().UTC(), targetBot: targetBot, webTerminal: webTerminal, osName: osName, tool: tool, version: clientVersion, deviceID: deviceID, deviceIDV1: deviceIDV1}
	c.metricsAgentID = a.metricsAgentID
	c.metricsGeneration = randomHex(16)
	c.obsConn = obsConn
	idleWatchdogCtx, stopIdleWatchdog := context.WithCancel(context.Background())
	h.backgroundWork("owned", func() { h.watchSessionIdle(idleWatchdogCtx, c) })
	h.backgroundWork("owned", func() { h.watchSessionKeepalive(idleWatchdogCtx, c) })
	skillCtx, skillCancel := context.WithCancel(context.Background())
	c.skillCancel = skillCancel
	// Send onboarding before publishing the client to directory broadcasters, so
	// no concurrent online-directory frame can overtake the initial guide.
	onboardStart := time.Now()
	h.startAgentNetworkSkillPush(skillCtx, c)
	timingParts = append(timingParts, fmt.Sprintf("onboard=%s", time.Since(onboardStart).Round(time.Millisecond)))
	if h.sessionBeforePublishHook != nil {
		h.sessionBeforePublishHook(keyID, sessionName)
	}
	lockStart := time.Now()
	var oldConn *websocket.Conn
	var oldObs *obs.WSConn
	var rejectDevice bool
	var reSize [2]int
	h.mu.Lock()
	func() {
		defer h.mu.Unlock()
		if h.sessionClients[keyID] == nil {
			h.sessionClients[keyID] = map[string]*sessionClient{}
		}
		if old := h.sessionClients[keyID][sessionName]; old != nil {
			if c.deviceIDV1 && old.deviceIDV1 && old.deviceID != c.deviceID {
				rejectDevice = true
			} else {
				oldConn = old.conn
				oldObs = old.obsConn
			}
		}
		if !rejectDevice && c != nil && c.conn != nil {
			h.sessionClients[keyID][sessionName] = c
		}
		h.keyAccounts[keyID] = stringSet(accounts)
		reSize = h.terminalLastSize[terminalKey(keyID, sessionName)]
	}()
	if rejectDevice {
		stopIdleWatchdog()
		skillCancel()
		obsConn.SetLocalReason("rejected")
		_ = conn.Close(websocket.StatusPolicyViolation, "session held by another device")
		return
	}
	if oldConn != nil {
		oldObs.SetLocalReason("replaced")
		_ = oldConn.Close(websocket.StatusPolicyViolation, "replaced")
	}
	if a.onRegistered != nil {
		h.backgroundWork("owned", func() { a.onRegistered() })
	}
	if c.metricsAgentID != "" {
		h.backgroundWork("owned", func() { h.resumeHelperUpdateOffer(context.Background(), c) })
	}
	if h.markSessionSeen(ownerKey, sessionName) {
		h.notifyNewSession(r.Context(), ownerKey, sessionName)
	}
	timingParts = append(timingParts, fmt.Sprintf("register_lock=%s", time.Since(lockStart).Round(time.Millisecond)))
	log.Printf("session handshake timing key_id=%s session=%s total=%s %s", keyID, sessionName, time.Since(timingStart).Round(time.Millisecond), strings.Join(timingParts, " "))
	if reSize[0] > 0 && reSize[1] > 0 {
		_ = c.write(r.Context(), model.Envelope{
			ID:   randomHex(16),
			To:   sessionAddress(sessionName, keyID),
			From: sessionAddress("workpulse", keyID),
			TS:   time.Now().Unix(),
			Meta: map[string]any{"type": terminalResizeType, "system": true, "no_mirror": true, "cols": reSize[0], "rows": reSize[1]},
		})
	}
	h.scheduleOnlineBroadcastForSession(r.Context(), keyID, sessionName)
	registrationDone()
	defer func() {
		stopIdleWatchdog()
		if c.skillCancel != nil {
			c.skillCancel()
		}
		ownerCtx, ownerCancel := context.WithTimeout(context.Background(), time.Second)
		ownerKey := h.ownerKeyForKey(ownerCtx, keyID)
		ownerCancel()
		h.mu.Lock()
		current := h.sessionClients[keyID][sessionName] == c
		var terminalViewers []*terminalViewer
		if current {
			delete(h.sessionClients[keyID], sessionName)
			if len(h.sessionClients[keyID]) == 0 {
				delete(h.sessionClients, keyID)
				delete(h.keyAccounts, keyID)
			}
			terminalViewers = h.closeTerminalLocked(keyID, sessionName)
			h.closeFeishuSyncLocked(keyID, sessionName)
		}
		h.mu.Unlock()
		if current && h.amr != nil {
			h.amr.sessionClosed(keyID, sessionName) // R-V2-0105 件三：断开时补出聚合日志计数并清表项
		}
		if current && h.sessionSupportsDeliveryACK(c) {
			h.releaseSessionDeliveryLeases(c, "session_disconnected")
		}
		for _, viewer := range terminalViewers {
			_ = terminalWrite(context.Background(), viewer, map[string]any{"type": "status", "readonly": true, "message": "会话已离线"})
			if viewer != nil && viewer.conn != nil {
				_ = viewer.conn.Close(websocket.StatusNormalClosure, "session offline")
			}
		}
		if conn != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "bye")
		}
		if !current {
			return
		}
		cleanupCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = h.Repo.UpsertSessionEndpoint(cleanupCtx, model.SessionEndpoint{
			KeyID:       keyID,
			SessionName: sessionName,
			LastSeenAt:  c.lastSeen(),
			Active:      false,
		})
		if ownerKey != "" {
			h.scheduleOnlineBroadcastForSession(context.Background(), keyID, sessionName)
		}
	}()
	r = r.WithContext(context.WithValue(r.Context(), subAgentClientKey{}, c))
	for {
		_, data, err := h.readSessionMessage(r.Context(), c)
		if err != nil {
			obsReadErr = obsWSReadErr(r.Context(), err)
			return
		}
		h.touchSessionEndpoint(r.Context(), c)
		var env model.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, "", "信封解析失败："+err.Error()))
			continue
		}
		if h.handleSessionCapability(c, env) || h.handleSubAgentControl(r.Context(), c, env) {
			continue
		}
		if err := h.rejectSubAgentIngress(c, env); err != nil {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, err.Error()))
			continue
		}
		if coordinateReviewEnvelope(env) || specReviewType(specReviewEnvelopeType(env)) || metaString(env.Meta, "kind") == "task_assigned" || metaString(env.Meta, "kind") == "task_blocker_ready" || metaBool(env.Meta, "task_flow_internal") {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, "task notification is Hub-only"))
			continue
		}
		if h.handleSessionCapability(c, env) || h.handleSessionDeliveryAck(r.Context(), c, env) {
			continue
		}
		if env.From == "" {
			env.From = sessionAddress(sessionName, keyID)
		}
		if env.ID == "" {
			env.ID = randomHex(16)
		}
		if env.TS == 0 {
			env.TS = time.Now().Unix()
		}
		if c.targetBot != "" {
			if env.Meta == nil {
				env.Meta = map[string]any{}
			}
			if h.handleFileCapabilityRequest(r.Context(), c, env) {
				continue
			}
			if isProducerEnvelope(env) && metaString(env.Meta, "target_bot") == "" {
				env.Meta["target_bot"] = c.targetBot
			}
		}
		if env.From == sessionAddress(requestedSessionName, keyID) && requestedSessionName != sessionName {
			env.From = sessionAddress(sessionName, keyID)
		}
		// 🔴 凭据连接（/ws/agent）由凭据定身份，不由信封自报定（组长裁定·甲，R-HUB-FROM-REWRITE）：
		//    入网 helper 的 from 是「会话名#key」，而它的 key 只是占位（SH_KEY_ID 缺省 "enrolled"，真身份是 Hub 按凭据查出的 u_属主），
		//    原先这里一律判 from≠会话地址而丢弃——terminal_output、回飞书、Agent 间发信、session_event 全被拒。
		//    现在：from 的【会话名部分】等于本连接的会话名 ⇒ 改写成真地址 sessionAddress(会话名, 本连接 keyID) 再往下走；
		//    会话名不同 ⇒ 仍落到下面的拒绝（不得冒用同属主下另一会话）。key 部分不看：它是占位，真值恒取本连接的 keyID。
		if credentialIdentity && env.From != sessionAddress(sessionName, keyID) {
			if i := strings.LastIndex(env.From, "#"); i > 0 && strings.EqualFold(env.From[:i], sessionName) {
				env.From = sessionAddress(sessionName, keyID)
			}
		}
		if env.From != sessionAddress(sessionName, keyID) {
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, "from 地址必须等于当前会话地址"))
			continue
		}
		if env.Meta == nil {
			env.Meta = map[string]any{}
		}
		if agentHandshakeEnforced() && sessionEnvelopeRequiresHelperReady(env) && !sessionHelperReady(c) {
			// M2 is a protocol admission hurdle, not trusted identity. A client
			// holding the shared API secret can imitate session_capabilities.
			// Strong helper identity requires a separately provisioned device key.
			log.Printf("session content rejected reason=helper_capabilities_required key_id=%s session=%s conn_seq=%d", c.keyID, c.sessionName, c.connSeq)
			_ = c.write(r.Context(), errorEnvelope(keyID, sessionName, env.ID, "当前连接尚未完成 sessionHelper capabilities 握手"))
			continue
		}
		// R-V2-AGENT-FILE-UPLOAD：取文件票原先只在带 target_bot 的连接上分派（见上）；托管席与多数自备 helper 不带 target_bot，
		// 取票信封会落进普通路由。这里对其余已认证会话同样就地应答（票绑定本连接的 owner／会话，信封自报的身份一律不看）。
		if h.handleInboxLagControl(r.Context(), c, env) {
			continue
		}
		if h.handleAgentDiskMetrics(r.Context(), c, env) {
			continue
		}
		if h.handleSessionHeartbeat(r.Context(), c, env) {
			continue
		}
		if h.handleFileCapabilityRequest(r.Context(), c, env) {
			continue
		}
		if h.handleSpecGrantRequest(r.Context(), c, env) {
			continue
		}
		// Reserved attestation fields are never accepted from a session. The
		// authenticated WS admission below derives the canonical agent channel.
		if _, claimed := env.Meta["provenance"]; claimed {
			log.Printf("provenance claim dropped ingress=agent_session session=%s", sessionName)
		}
		delete(env.Meta, hubFailureNoticeMetaKey)
		delete(env.Meta, "provenance")
		delete(env.Meta, metaKeyViewMention)
		delete(env.Meta, metaKeyReplyOriginReturn)
		delete(env.Meta, "source_key_id")
		delete(env.Meta, "source_session_name")
		delete(env.Meta, hubIngressMetaKey)
		env.Meta[hubIngressMetaKey] = hubIngressAgent
		if metaString(env.Meta, "type") == "console_spec_pm_assist_result" {
			h.handlePMSpecAssistResult(r.Context(), metaString(env.Meta, "request_id"), c, env.Body)
			continue
		}
		if h.handleSpecAgentReviewResult(r.Context(), c, env) {
			continue
		}
		if h.handleConsoleSpecPresplitResult(r.Context(), c, env) {
			continue
		}
		if h.handleAgentNetworkSkillAck(c, env) {
			continue
		}
		if h.handleTerminalOutputEnvelope(c, env) {
			continue
		}
		if h.handleSessionEventEnvelopeContext(r.Context(), c, env) {
			continue
		}
		if h.amr != nil && h.amr.shouldDropLegacyAgentRoute(c.keyID, c.sessionName, env) {
			log.Printf("amr decision=legacy_route_dropped session=%s", c.sessionName)
			continue
		}
		if h.handleConfirmedOutbound(r.Context(), c, env) {
			continue
		}
		if metaBool(env.Meta, ipcBodyAMRMetaKey) {
			// T-089: send.py opts in explicitly. Parse only this authenticated
			// helper ingress, using the exact same Go AMR parser as turn output.
			delete(env.Meta, ipcBodyAMRMetaKey)
			h.routeIPCSendBodyAMR(r.Context(), c, env)
		}
		if h.routeSessionSelectorEnvelope(r.Context(), c, env) {
			continue
		}
		if h.handleControlSubtaskResult(r.Context(), c, env) {
			continue
		}
		if h.confirmHelperUpdateHealth(r.Context(), c, env) {
			continue
		}
		if h.recordHelperUpdateReceipt(r.Context(), c, env) || h.handleProvisionAck(r.Context(), c, env) {
			continue
		}
		if err := h.RouteEnvelope(r.Context(), env); err != nil {
			h.writeDeliveryFailure(r.Context(), c, env, env.To, err)
		}
	}
}

func (h *Hub) handleProvisionAck(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "provision_ack" {
		return false
	}
	if h.Repo != nil {
		target, _ := json.Marshal(map[string]any{
			"from":         env.From,
			"action":       metaString(env.Meta, "action"),
			"target":       metaString(env.Meta, "target"),
			"version":      metaString(env.Meta, "version"),
			"ok":           metaBool(env.Meta, "ok"),
			"message":      metaString(env.Meta, "message"),
			"from_version": metaString(env.Meta, "from_version"),
			"to_version":   metaString(env.Meta, "to_version"),
		})
		_ = h.Repo.WriteAudit(ctx, env.From, "provision_ack", string(target))
		// F2: 只有 helper 确认 apply 成功(ok=true)才推进「最后已确认版本」，供下发前单调闸判断，
		// 避免「下发成功但 helper 拒绝(如 sha256 失配)」烧掉版本号。
		// update_self 不在此记账——它成功时会重启且不回 ack，改以 session_endpoint.version 为准
		// （见 admin.ensureProvisionVersionMonotonic）。
		if metaBool(env.Meta, "ok") {
			action := strings.TrimSpace(metaString(env.Meta, "action"))
			version := strings.TrimSpace(metaString(env.Meta, "version"))
			if action != "" && action != "update_self" && version != "" {
				if addr, err := parseAddress(env.From); err == nil && addr.Kind == addressSession {
					payload, _ := json.Marshal(map[string]string{"version": version})
					_ = h.Repo.UpsertAppConfig(ctx, store.ProvisionVersionKey(action, addr.KeyID, addr.SessionName, strings.TrimSpace(metaString(env.Meta, "target"))), string(payload))
				}
			}
		}
	}
	return true
}

func (h *Hub) handleControlSubtaskResult(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	taskID := metaString(env.Meta, "control_task_id")
	if taskID == "" {
		return false
	}
	task, err := h.Repo.GetControlTask(ctx, taskID)
	if err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "子任务查询失败："+err.Error()))
		return true
	}
	if task == nil || task.ParentID == "" {
		return false
	}
	if err := h.CompleteControlSubtask(ctx, taskID, env.Body, "done", ""); err != nil {
		_ = c.write(ctx, errorEnvelope(c.keyID, c.sessionName, env.ID, "子任务回填失败："+err.Error()))
	}
	return true
}

func sessionMirrorToFromRequest(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("mirror_to"))
}

func (h *Hub) registerSessionEndpoint(ctx context.Context, ep model.SessionEndpoint) (string, error) {
	requested := ep.SessionName
	// 🔴 业主定的第三条:保留字不能用于 Agent 短名。必须在**写入口**拦 ——
	// 只在解析期拦的话,名字照样落库,歧义从解析期搬进数据里,那时再修要动存量。
	if isReservedForNaming(requested) {
		return "", fmt.Errorf("会话短名不得使用渠道保留字：%s", requested)
	}
	if ep.OwnerKey == "" {
		return requested, h.Repo.UpsertSessionEndpoint(ctx, ep)
	}
	for attempt := 0; attempt < 100; attempt++ {
		candidate, err := h.nextSessionNameForOwner(ctx, ep.OwnerKey, ep.KeyID, requested)
		if err != nil {
			return "", err
		}
		if attempt > 0 {
			candidate = suffixedSessionName(requested, attempt+1)
		}
		ep.SessionName = candidate
		if err := h.Repo.UpsertSessionEndpoint(ctx, ep); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				continue
			}
			return "", err
		}
		return candidate, nil
	}
	return "", errors.New("allocate session name failed")
}

func (h *Hub) nextSessionNameForOwner(ctx context.Context, ownerKey, keyID, requested string) (string, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for _, ep := range endpoints {
		if !ep.Active || ep.OwnerKey != ownerKey {
			continue
		}
		if ep.KeyID == keyID && ep.SessionName == requested {
			return requested, nil
		}
		used[ep.SessionName] = true
	}
	if !used[requested] {
		return requested, nil
	}
	for i := 2; ; i++ {
		candidate := suffixedSessionName(requested, i)
		if !used[candidate] {
			return candidate, nil
		}
	}
}

func suffixedSessionName(base string, n int) string {
	return fmt.Sprintf("%s%d", base, n)
}

func deriveRegisteredSessionName(requested, keyID, ownerKey string) string {
	requested = strings.TrimSpace(requested)
	if ownerKey == "" {
		return requested
	}
	short := requested
	if sessionNamePattern.MatchString(requested) {
		short = strings.Split(requested, "-")[1]
	}
	if !bareSessionNamePattern.MatchString(short) {
		return requested
	}
	return fmt.Sprintf("%s-%s-%s", ownerKey, short, keyTail(keyID))
}

// sessionNameRequestWarning 判定客户端上报名本身是否合规:B 方案裸短名合规,
// 完整名须 owner_key 与 key 末4位都与该 key 绑定成员一致。返回非空即触发 warn/enforce。
func sessionNameRequestWarning(requested, keyID, ownerKey string) string {
	requested = strings.TrimSpace(requested)
	ownerKey = strings.TrimSpace(ownerKey)
	if ownerKey == "" {
		return "无法确认该 key 绑定成员 owner_key"
	}
	if bareSessionNamePattern.MatchString(requested) {
		return ""
	}
	return sessionNamePolicyWarning(requested, keyID, ownerKey)
}

func (h *Hub) Dispatch(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, error) {
	if h.ExternalCopyReplies != nil && msg.FeishuReply != nil && msg.FeishuReply.Authenticated && msg.FeishuReply.ParentID != "" {
		isCopy, err := h.ExternalCopyReplies.IsCopyReply(ctx, msg.BotChannelID, msg.FeishuReply.ParentID)
		if err != nil {
			return model.PrefixDispatchResult{}, err
		}
		if isCopy {
			return model.PrefixDispatchResult{Matched: true}, nil
		}
	}
	if result, handled, err := h.dispatchR9PMReply(ctx, msg, text); handled {
		return result, err
	}
	if result, handled, err := h.dispatchClarificationReply(ctx, msg, text); handled {
		return result, err
	}
	if h.Repo == nil {
		return h.dispatchL1Direct(ctx, msg, text)
	}
	for _, expired := range h.reapExpiredControlTasks(ctx) {
		_ = h.notifyControlTask(ctx, expired, controlTaskExpiredReply(expired))
	}
	task, inserted, err := h.Repo.EnqueueControlTask(ctx, h.newControlTask(ctx, msg, text))
	if err != nil {
		return model.PrefixDispatchResult{}, err
	}
	ack := fmt.Sprintf("已受理 #%s", task.ID)
	if !inserted && task.Status == "ignored" {
		// 已判定为群聊非命令消息:重复投递也保持静默(与首次一致)
		return model.PrefixDispatchResult{Matched: false}, nil
	}
	if !inserted && task.Status != "done" {
		return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
	}
	if !inserted && task.Status == "done" {
		if task.Result == "" {
			return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: task.Result}, nil
	}
	if msg.ChatType == model.ChatGroup {
		explicit, gateErr := h.groupHasExplicitIntent(ctx, msg, text)
		if gateErr != nil {
			return model.PrefixDispatchResult{}, gateErr
		}
		if !explicit {
			if err := h.Repo.UpdateControlTaskAfterL1(ctx, task.ID, "ignored.group_natural_language", "L1", "", "", "ignored", ""); err != nil {
				return model.PrefixDispatchResult{}, err
			}
			return model.PrefixDispatchResult{Matched: false}, nil
		}
	}
	result, intent, target, err := h.dispatchL1(ctx, msg, text)
	if err != nil {
		retried, retryErr := h.Repo.RetryControlTask(ctx, task.ID, err.Error())
		if retryErr != nil {
			return result, retryErr
		}
		if retried != nil && retried.Status == "failed" {
			reply := controlTaskFailedReply(*retried)
			_ = h.notifyControlTask(ctx, *retried, reply)
			return model.PrefixDispatchResult{Matched: true, Reply: reply}, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
	}
	status := "llm_pending"
	layer := "L1"
	resultText := result.Reply
	errText := ""
	if result.Matched {
		status = "done"
	} else if intent == "" {
		intent = "unknown"
	}
	// 止血(fix/control-plane-intent-gate):群聊里 L1 未命中任何命令的消息
	// (闲聊/"好的"/@别人)——不进 L2、不回执,保持静默。
	// 群聊只应答明确命令;自然语言路由仅限个人会话直聊机器人,
	// 避免机器人截胡人对人的消息、或对不懂的话幻觉执行(如"正在为您安排会议")。
	gateSilent := !result.Matched && msg.ChatType == model.ChatGroup
	if gateSilent {
		status = "ignored"
	}
	if err := h.Repo.UpdateControlTaskAfterL1(ctx, task.ID, intent, layer, target, resultText, status, errText); err != nil {
		return model.PrefixDispatchResult{}, err
	}
	if result.Matched {
		return result, nil
	}
	if gateSilent {
		return model.PrefixDispatchResult{Matched: false}, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: ack}, nil
}

func (h *Hub) groupHasExplicitIntent(ctx context.Context, msg model.Message, text string) (bool, error) {
	if msg.ChatType != model.ChatGroup {
		return true, nil
	}
	trimmed := strings.TrimSpace(text)
	if withoutBot := strings.TrimSpace(h.stripLeadingBotMentions(msg.BotChannelID, trimmed)); withoutBot != trimmed {
		return true, nil
	}
	if _, _, ok := parseSelector(trimmed); ok {
		return true, nil
	}
	if _, _, _, ok := h.parseMemberMention(msg.BotChannelID, trimmed); ok {
		return true, nil
	}
	routes, err := h.Repo.ListPrefixRoutes(ctx, msg.ChatEntityID)
	if err != nil {
		return false, err
	}
	for _, route := range routes {
		if router.MatchPrefix(route.Rule.MatchExpr, trimmed, route.Rule.CaseSensitive) {
			return true, nil
		}
	}
	// System/security/apply/sync/roster commands are explicit hash commands even
	// when their backing route is temporarily unavailable.
	return strings.HasPrefix(trimmed, "#"), nil
}

func (h *Hub) dispatchL1Direct(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, error) {
	result, _, _, err := h.dispatchL1LegacyFallback(ctx, msg, text)
	return result, err
}

func (h *Hub) dispatchL1(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, string, string, error) {
	if result, handled, err := h.dispatchSecurityOps(ctx, msg, text); handled || err != nil {
		return result, "command.security", "", err
	}
	if result, handled, err := h.dispatchConsoleLogin(ctx, msg, text); handled || err != nil {
		return result, "command.console_login", "", err
	}
	if result, handled, err := h.dispatchFeishuSyncCommand(ctx, msg, text); handled || err != nil {
		return result, "command.sync", "", err
	}
	if result, handled, err := h.dispatchApplyKeyCommand(ctx, msg, text); handled || err != nil {
		return result, "command.apply_key", "", err
	}
	if result, handled, err := h.dispatchSystem(ctx, msg, text); handled || err != nil {
		return result, "command.system", "", err
	}
	if result, handled, err := h.dispatchAggregateWeeklyReview(ctx, msg, text); handled || err != nil {
		return result, "command.aggregate_weekly_review", "", err
	}
	rules, err := h.Repo.ListL1DecisionRules(ctx)
	if err != nil {
		return model.PrefixDispatchResult{}, "", "", err
	}
	for _, rule := range rules {
		matched, err := h.l1RuleMatches(ctx, rule, msg, text)
		if err != nil {
			return model.PrefixDispatchResult{}, "", "", err
		}
		if !matched {
			continue
		}
		result, target, handled, err := h.executeL1Rule(ctx, rule, msg, text)
		if err != nil {
			return result, rule.Intent, target, err
		}
		if handled {
			return result, rule.Intent, target, nil
		}
		if !rule.ExitQueue {
			return model.PrefixDispatchResult{}, rule.Intent, target, nil
		}
	}
	return model.PrefixDispatchResult{}, "unknown", "", nil
}

func (h *Hub) executeL1Rule(ctx context.Context, rule model.L1DecisionRule, msg model.Message, text string) (model.PrefixDispatchResult, string, bool, error) {
	switch rule.ID {
	case "l1_command_terminal_input":
		result, handled, err := h.dispatchTerminalInputCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_command_roster":
		result, handled, err := h.dispatchOnlineRosterCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_command_apply_key":
		result, handled, err := h.dispatchApplyKeyCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_command_mirror":
		result, handled, err := h.dispatchMirrorCommand(ctx, msg, text)
		return result, "", handled, err
	case "l1_route_session":
		result, handled, err := h.dispatchSelector(ctx, msg, text)
		return result, "", handled, err
	case "l1_route_cross":
		result, handled, err := h.dispatchMemberMention(ctx, msg, text)
		return result, "", handled, err
	case "l1_route_default_single":
		return h.dispatchL1LegacyFallback(ctx, msg, text)
	case "l1_nl_dispatch", "l1_unknown":
		return h.dispatchL1LegacyFallback(ctx, msg, text)
	case "l1_decompose":
		return model.PrefixDispatchResult{}, "", false, nil
	default:
		return h.dispatchL1LegacyFallback(ctx, msg, text)
	}
}

func (h *Hub) dispatchL1LegacyFallback(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, string, bool, error) {
	if result, handled, err := h.dispatchSecurityOps(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchConsoleLogin(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchFeishuSyncCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchApplyKeyCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchTerminalInputCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchSystem(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchAggregateWeeklyReview(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchMirrorCommand(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchMemberMention(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	if result, handled, err := h.dispatchSelector(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	routes, err := h.Repo.ListPrefixRoutes(ctx, msg.ChatEntityID)
	if err != nil {
		return model.PrefixDispatchResult{}, "", false, err
	}
	for _, route := range routes {
		if !router.MatchPrefix(route.Rule.MatchExpr, text, route.Rule.CaseSensitive) {
			continue
		}
		forwardText := text
		if route.Rule.StripPrefix {
			forwardText = stripPrefix(route.Rule.MatchExpr, text, route.Rule.CaseSensitive)
		}
		if route.Service.DeliveryType == "session" {
			keyID, sessionName, ok := sessionServiceParts(route.Service.ID)
			if !ok {
				continue
			}
			env := model.Envelope{
				ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
				To:   sessionAddress(sessionName, keyID),
				From: feishuAddress(feishuOpenIDFromAccount(sourceAccount(msg)), keyID, h.botName(msg.BotChannelID)),
				Body: forwardText,
				TS:   time.Now().Unix(),
				Meta: map[string]any{
					"chat_type":      string(msg.ChatType),
					"raw":            msg.Content,
					"bot_channel_id": msg.BotChannelID,
					"route_id":       route.Rule.ID,
				},
			}
			h.annotateSourceContext(ctx, env.Meta, msg)
			if msg.ChatType == model.ChatGroup {
				env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
				env.Meta["sender_open_id"] = msg.SenderOpenID
			}
			if err := h.RouteEnvelope(ctx, env); err != nil {
				logFeishuDispatchRejected("route_error", feishuOpenIDFromAccount(sourceAccount(msg)), route.Service.ID, msg.BotChannelID)
				return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, "", true, nil
			}
			return model.PrefixDispatchResult{Matched: true}, route.Service.ID, true, nil
		}
		if route.Service.DeliveryType != "ws" {
			continue
		}
		reply, err := h.forward(ctx, route.Service.ID, ForwardRequest{
			ID:           msg.ID,
			ServiceID:    route.Service.ID,
			ChatEntityID: msg.ChatEntityID,
			BotChannelID: msg.BotChannelID,
			ChatType:     string(msg.ChatType),
			SenderOpenID: msg.SenderOpenID,
			Text:         forwardText,
			RawContent:   msg.Content,
		}, route.Service.TimeoutMs)
		if err != nil {
			logFeishuDispatchRejected("route_error", feishuOpenIDFromAccount(sourceAccount(msg)), route.Service.ID, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, route.Service.ID, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: reply}, route.Service.ID, true, nil
	}
	if result, handled, err := h.dispatchDefaultPersonalSession(ctx, msg, text); handled || err != nil {
		return result, "", handled, err
	}
	return model.PrefixDispatchResult{}, "", false, nil
}

func (h *Hub) newControlTask(ctx context.Context, msg model.Message, text string) model.ControlTask {
	now := time.Now().UTC()
	sourceAddr := controlSourceAddr(msg, h.botName(msg.BotChannelID))
	ownerKey := h.controlOwnerKey(ctx, msg)
	expireAt := now.Add(5 * time.Minute)
	return model.ControlTask{
		ID:           firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
		CreatedAt:    now,
		UpdatedAt:    now,
		Source:       controlSource(msg),
		SourceAddr:   sourceAddr,
		OwnerKey:     ownerKey,
		BotChannelID: msg.BotChannelID,
		RawInput:     text,
		Status:       "queued",
		Priority:     0,
		MaxAttempts:  3,
		ExpireAt:     &expireAt,
	}
}

func (h *Hub) reapExpiredControlTasks(ctx context.Context) []model.ControlTask {
	if h.Repo == nil {
		return nil
	}
	expired, err := h.Repo.ReapExpiredControlTasks(ctx, time.Now().UTC())
	if err != nil {
		return nil
	}
	return expired
}

func (h *Hub) notifyControlTask(ctx context.Context, task model.ControlTask, reply string) error {
	if strings.TrimSpace(task.SourceAddr) == "" || strings.TrimSpace(reply) == "" {
		return nil
	}
	to, err := parseAddress(task.SourceAddr)
	if err != nil {
		return err
	}
	if to.Kind == addressFeishu {
		if h.Outbound == nil {
			return nil
		}
		chatType := model.ChatPersonal
		entityKind := "personal"
		if strings.HasPrefix(to.OpenID, "oc_") {
			chatType = model.ChatGroup
			entityKind = "group"
		}
		botChannelID := firstNonEmpty(task.BotChannelID, h.botChannel(to.BotName), to.KeyID)
		content, _ := json.Marshal(map[string]string{"text": reply})
		return h.Outbound.Enqueue(ctx, model.Message{
			ID:           "control-" + task.Status + "-" + task.ID,
			ChatEntityID: botChannelID + ":" + entityKind + ":" + to.OpenID,
			BotChannelID: botChannelID,
			FeishuMsgID:  "control-" + task.Status + "-" + task.ID,
			ChatType:     chatType,
			Content:      string(content),
		})
	}
	env := model.Envelope{
		ID:   "control-" + task.Status + "-" + task.ID,
		To:   task.SourceAddr,
		From: sessionAddress("workpulse", to.KeyID),
		Body: reply,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"system": true, "no_mirror": true, "source_bot_channel_id": task.BotChannelID},
	}
	return h.RouteEnvelope(ctx, env)
}

func controlTaskExpiredReply(task model.ControlTask) string {
	return fmt.Sprintf("任务 #%s 已超时，请重新发起或补充更明确的目标。", task.ID)
}

func controlTaskFailedReply(task model.ControlTask) string {
	if strings.TrimSpace(task.Error) == "" {
		return fmt.Sprintf("任务 #%s 处理失败，请稍后重试。", task.ID)
	}
	return fmt.Sprintf("任务 #%s 处理失败：%s", task.ID, task.Error)
}

func (h *Hub) l1RuleMatches(ctx context.Context, rule model.L1DecisionRule, msg model.Message, text string) (bool, error) {
	text = strings.TrimSpace(h.stripLeadingBotMentions(msg.BotChannelID, text))
	switch rule.MatchType {
	case "prefix":
		return strings.HasPrefix(text, rule.Pattern), nil
	case "prefix_any":
		for _, p := range splitRulePattern(rule.Pattern) {
			if strings.HasPrefix(text, p) {
				return true, nil
			}
		}
		return false, nil
	case "keyword_any":
		for _, p := range splitRulePattern(rule.Pattern) {
			if strings.Contains(text, p) {
				return true, nil
			}
		}
		return false, nil
	case "regex":
		return regexp.MatchString(rule.Pattern, text)
	case "default":
		source := sourceAccount(msg)
		_, sessions, err := h.onlineSessionsForAccount(ctx, source)
		if err != nil {
			return false, err
		}
		switch rule.Pattern {
		case "personal_single_online_session":
			return msg.ChatType == model.ChatPersonal && len(sessions) == 1 && !looksPrefixed(text), nil
		case "personal_multiple_online_sessions":
			return msg.ChatType == model.ChatPersonal && len(sessions) > 1 && !looksPrefixed(text), nil
		default:
			return false, nil
		}
	case "fallback":
		return true, nil
	default:
		return false, nil
	}
}

func (h *Hub) dispatchOnlineRosterCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	text = strings.TrimSpace(h.stripLeadingBotMentions(msg.BotChannelID, text))
	if text != "#在线" && text != "#roster" && text != "#清单" {
		return model.PrefixDispatchResult{}, false, nil
	}
	ownerKey := h.controlOwnerKey(ctx, msg)
	items, owner, err := h.onlineDirectory(ctx, ownerKey)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if owner.OwnerKey == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到当前账号的在线清单。"}, true, nil
	}
	if text == "#清单" {
		code := h.issueOwnerLinkCode(owner.OwnerKey)
		return model.PrefixDispatchResult{Matched: true, Reply: renderOwnerLinksText(owner, items, code)}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: renderOnlineDirectory(owner, items)}, true, nil
}

func (h *Hub) controlOwnerKey(ctx context.Context, msg model.Message) string {
	if user, ok := h.newPlatformFeishuUser(ctx, msg); ok && user.State == identity.UserActive {
		return user.UserIDLC()
	}
	source := sourceAccount(msg)
	if h.Repo == nil {
		return source
	}
	members, err := h.Repo.ListMembers(ctx)
	if err == nil {
		for _, member := range members {
			if member.Active && accountBelongsToMember(source, member) {
				return member.OwnerKey
			}
		}
	}
	if botChannelID, feishuID, ok := accountEntityParts(source); ok {
		if entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID); err == nil && entity != nil && strings.TrimSpace(entity.BoundOwner) != "" {
			return entity.BoundOwner
		}
	}
	return source
}

func controlSource(msg model.Message) string {
	if msg.BotChannelID != "" || msg.ChatEntityID != "" {
		return "feishu"
	}
	return "session"
}

func controlSourceAddr(msg model.Message, botName string) string {
	source := sourceAccount(msg)
	keyID := msg.BotChannelID
	if keyID == "" {
		keyID = "unknown"
	}
	if controlSource(msg) == "feishu" {
		return feishuAddress(feishuOpenIDFromAccount(source), keyID, botName)
	}
	return source
}

func splitRulePattern(pattern string) []string {
	var out []string
	for _, part := range strings.Split(pattern, "|") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func looksPrefixed(text string) bool {
	return strings.HasPrefix(text, "#") || strings.HasPrefix(text, "@")
}

func (h *Hub) dispatchSystem(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	text = h.stripLeadingBotMentions(msg.BotChannelID, text)
	routes, err := h.Repo.ListSystemRoutes(ctx)
	if err != nil {
		return model.PrefixDispatchResult{}, false, err
	}
	for _, route := range routes {
		if !strings.HasPrefix(text, route.Keyword) {
			continue
		}
		body := strings.TrimSpace(strings.TrimPrefix(text, route.Keyword))
		if h.System == nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "系统服务未配置：" + route.ServiceName}, true, nil
		}
		reply, err := h.System.HandleSystemRequest(ctx, route.ServiceName, route.Action, body, msg)
		if err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "系统服务执行失败：" + err.Error()}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: reply}, true, nil
	}
	return model.PrefixDispatchResult{}, false, nil
}

func (h *Hub) dispatchAggregateWeeklyReview(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	if h.System == nil {
		return model.PrefixDispatchResult{}, false, nil
	}
	reviewer, ok := h.System.(aggregateWeeklyReviewHandler)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	body := h.stripLeadingBotMentions(msg.BotChannelID, text)
	reply, matched, err := reviewer.HandleAggregateWeeklyReviewCommand(ctx, body, msg)
	if !matched && err == nil {
		return model.PrefixDispatchResult{}, false, nil
	}
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "系统服务执行失败：" + err.Error()}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: reply}, true, nil
}

func (h *Hub) dispatchSecurityOps(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	targetName, _, body, ok := h.parseMemberMention(msg.BotChannelID, text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	target, err := h.resolveMemberByName(ctx, targetName)
	if err != nil || target.OwnerKey != secOpsOwnerKey {
		return model.PrefixDispatchResult{}, false, nil
	}
	body = strings.TrimSpace(body)
	if !strings.HasPrefix(body, secOpsKeyword) {
		return model.PrefixDispatchResult{}, false, nil
	}
	command := strings.TrimSpace(strings.TrimPrefix(body, secOpsKeyword))
	if command == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "系统安全指令为空"}, true, nil
	}
	allowed, err := h.securityOpsAuthorized(ctx, msg)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if !allowed {
		return model.PrefixDispatchResult{Matched: true, Reply: "无权限"}, true, nil
	}
	targets, err := h.onlineSessionsForOwner(ctx, secOpsOwnerKey)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if len(targets) == 0 {
		return model.PrefixDispatchResult{Matched: true, Reply: "SYSTEM-V-TASK-INTERNAL 当前无在线 sec-ops 会话"}, true, nil
	}
	source := sourceAccount(msg)
	broadcastKey := broadcastDedupKey("sec-ops", firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)), secOpsOwnerKey)
	for i, target := range targets {
		envID := firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16))
		if len(targets) > 1 {
			envID = fmt.Sprintf("%s-%d", envID, i+1)
		}
		env := model.Envelope{
			ID:   envID,
			To:   sessionAddress(target.sessionName, target.keyID),
			From: feishuAddress(feishuOpenIDFromAccount(source), target.keyID, h.botName(msg.BotChannelID)),
			Body: command,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"chat_type":            string(msg.ChatType),
				"raw":                  msg.Content,
				"bot_channel_id":       msg.BotChannelID,
				"system_route":         "sec-ops",
				"system_keyword":       secOpsKeyword,
				"cross_member":         target.ownerKey,
				"cross_member_name":    secOpsMemberName,
				"cross_session_name":   target.sessionName,
				"reply_prefix":         fmt.Sprintf("【%s·%s】", secOpsMemberName, target.sessionName),
				"source_chat_entity":   msg.ChatEntityID,
				"source_sender_openid": msg.SenderOpenID,
			},
		}
		h.annotateSourceContext(ctx, env.Meta, msg)
		annotateBroadcastMirror(&env, broadcastKey, i == 0)
		if msg.ChatType == model.ChatGroup {
			env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
			env.Meta["sender_open_id"] = msg.SenderOpenID
		}
		if err := h.RouteEnvelope(ctx, env); err != nil {
			return model.PrefixDispatchResult{Matched: true, Reply: "sec-ops 投递失败：" + err.Error()}, true, nil
		}
	}
	return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("已投递到%d个sec-ops会话", len(targets))}, true, nil
}

func (h *Hub) securityOpsAuthorized(ctx context.Context, msg model.Message) (bool, error) {
	if secOpsAdminOpenID != "" && msg.SenderOpenID == secOpsAdminOpenID {
		return true, nil
	}
	openID := firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(sourceAccount(msg)))
	if secOpsAdminOpenID != "" && msg.ChatType == model.ChatPersonal && openID == secOpsAdminOpenID {
		return true, nil
	}
	if secOpsAdminOwnerKey != "" {
		members, err := h.Repo.ListMembers(ctx)
		if err != nil {
			return false, err
		}
		for _, member := range members {
			if !member.Active || member.OwnerKey != secOpsAdminOwnerKey {
				continue
			}
			if openID != "" && member.FeishuOpenID == openID {
				return true, nil
			}
		}
		botChannelID, feishuID, ok := accountEntityParts(sourceAccount(msg))
		if !ok {
			return false, nil
		}
		entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID)
		if err != nil {
			return false, err
		}
		return entity != nil && entity.Active && entity.BoundOwner == secOpsAdminOwnerKey, nil
	}
	return false, nil
}

type ownerSessionTarget struct {
	keyID       string
	sessionName string
	ownerKey    string
}

type ownerSessionResolveStatus string

const (
	ownerSessionUniqueHit ownerSessionResolveStatus = "unique-hit"
	ownerSessionNoMatch   ownerSessionResolveStatus = "no-match"
	ownerSessionAmbiguous ownerSessionResolveStatus = "ambiguous"
)

type ownerSessionResolveResult struct {
	keyID       string
	sessionName string
	status      ownerSessionResolveStatus
	candidates  []string
}

func (h *Hub) onlineSessionsForOwner(ctx context.Context, ownerKey string) ([]ownerSessionTarget, error) {
	member, err := h.Repo.GetMemberByOwnerKey(ctx, ownerKey)
	if err != nil {
		return nil, err
	}
	if member == nil || !member.Active {
		return nil, nil
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	var out []ownerSessionTarget
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		belongs := ep.OwnerKey == ownerKey
		if !belongs {
			accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
			if err != nil {
				return nil, err
			}
			for _, account := range accounts {
				if accountBelongsToMember(account, *member) {
					belongs = true
					break
				}
			}
		}
		if !belongs || !h.sessionOnline(ep.KeyID, ep.SessionName) {
			continue
		}
		out = append(out, ownerSessionTarget{keyID: ep.KeyID, sessionName: ep.SessionName, ownerKey: ownerKey})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].keyID == out[j].keyID {
			return out[i].sessionName < out[j].sessionName
		}
		return out[i].keyID < out[j].keyID
	})
	return out, nil
}

func (h *Hub) sessionOnline(keyID, sessionName string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if sessions := h.sessionClients[keyID]; sessions != nil {
		return sessions[sessionName] != nil
	}
	return false
}

func (h *Hub) resolveOwnerSessionByShort(ctx context.Context, ownerKey, requested, hintKey string) ownerSessionResolveResult {
	ownerKey = strings.TrimSpace(ownerKey)
	requested = strings.TrimSpace(requested)
	if ownerKey == "" || requested == "" {
		return ownerSessionResolveResult{status: ownerSessionNoMatch}
	}
	if full := h.resolveNewPlatformVisibleSession(ctx, requested); full.fullName {
		if !full.found || full.ownerLC != identity.Lower(ownerKey) || !h.sessionOnline(full.keyID, full.sessionName) {
			return ownerSessionResolveResult{status: ownerSessionNoMatch}
		}
		return ownerSessionResolveResult{keyID: full.keyID, sessionName: full.sessionName, status: ownerSessionUniqueHit}
	}
	h.mu.Lock()
	type candidate struct {
		keyID       string
		sessionName string
	}
	var online []candidate
	for keyID, sessions := range h.sessionClients {
		for sessionName, c := range sessions {
			if c != nil {
				online = append(online, candidate{keyID: keyID, sessionName: sessionName})
			}
		}
	}
	h.mu.Unlock()
	sort.Slice(online, func(i, j int) bool {
		if online[i].sessionName == online[j].sessionName {
			return online[i].keyID < online[j].keyID
		}
		return online[i].sessionName < online[j].sessionName
	})
	var exactMatches []candidate
	var fuzzyMatches []candidate
	for _, item := range online {
		if h.ownerKeyForKey(ctx, item.keyID) != ownerKey {
			continue
		}
		if hintKey != "" && item.keyID == hintKey && ownerSessionExactMatch(ownerKey, item.sessionName, requested) {
			return ownerSessionResolveResult{keyID: item.keyID, sessionName: item.sessionName, status: ownerSessionUniqueHit}
		}
		if ownerSessionExactMatch(ownerKey, item.sessionName, requested) {
			exactMatches = append(exactMatches, item)
			continue
		}
		if ownerSessionFuzzyMatch(item.sessionName, requested) {
			fuzzyMatches = append(fuzzyMatches, item)
		}
	}
	matches := exactMatches
	if len(matches) == 0 {
		matches = fuzzyMatches
	}
	if len(matches) == 0 {
		return ownerSessionResolveResult{status: ownerSessionNoMatch}
	}
	if len(matches) == 1 {
		return ownerSessionResolveResult{keyID: matches[0].keyID, sessionName: matches[0].sessionName, status: ownerSessionUniqueHit}
	}
	names := make([]string, 0, len(matches))
	for _, item := range matches {
		names = append(names, item.sessionName)
	}
	return ownerSessionResolveResult{status: ownerSessionAmbiguous, candidates: names}
}

func ownerSessionFuzzyMatch(sessionName, requested string) bool {
	requested = strings.TrimSpace(requested)
	if utf8.RuneCountInString(requested) < 3 {
		return false
	}
	short := shortSessionName(sessionName)
	// 🔴 T-071(2026-09-11 实测):此处原为 strings.Contains —— **任意位置的子串**都算命中。
	// 后果:`@zzc`(业主 key 前缀)命中 `coder56sol**zzc**`,把发给业主的整段话
	// 静默投给了 coder,而发信方收到的是「成功」。两侧都不会察觉。
	// 改为**前缀**匹配:既保留 `opus`→`opus50ts`、`prod`→`prodops` 这类有用的简写,
	// 又让「名字出现在别人短名中间」不再构成命中。
	// ⚠️ 前缀仍是一种猜测,故必须与「投递结果回显实际去向」(见 amrTargetResult.resolved)
	// 配套使用 —— 缩小猜错的面,同时让猜的结果可见。
	return short != "" && strings.HasPrefix(strings.ToLower(short), strings.ToLower(requested))
}

// ownerSessionExactMatch keeps exact matches ahead of fuzzy prefix matches, but
// accepts case differences across the canonical name, short name, and
// owner-short alias. Session names are ASCII by policy, so EqualFold gives a
// deterministic widening without introducing locale-dependent guesses.
func ownerSessionExactMatch(ownerKey, sessionName, requested string) bool {
	requested = strings.TrimSpace(requested)
	if strings.EqualFold(sessionName, requested) {
		return true
	}
	return ownerSessionAliasMatch(ownerKey, sessionName, requested)
}

func ownerSessionAliasMatch(ownerKey, sessionName, requested string) bool {
	short := shortSessionName(sessionName)
	if short == "" {
		return false
	}
	if strings.EqualFold(requested, short) {
		return true
	}
	parts := strings.Split(sessionName, "-")
	if len(parts) == 3 && sessionNamePattern.MatchString(sessionName) && parts[0] == ownerKey {
		return strings.EqualFold(requested, parts[0]+"-"+parts[1])
	}
	return false
}

func ambiguousSessionError(requested string, candidates []string) error {
	if len(candidates) == 0 {
		return fmt.Errorf("存在多个 %s 会话，请用全名指定", requested)
	}
	return fmt.Errorf("存在多个 %s 会话：%s，请用全名指定", requested, strings.Join(candidates, "、"))
}

func (h *Hub) dispatchMirrorCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	action, sessionName, ok := parseMirrorCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true}, true, nil
	}
	source := sourceAccount(msg)
	keyID, found, err := h.lookupKeyIDForMirror(ctx, source, sessionName)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if user, ok := h.newPlatformFeishuUser(ctx, msg); ok {
		if user.State != identity.UserActive {
			logFeishuDispatchRejected("sender_inactive", msg.SenderOpenID, sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "你的账号已停用，无法投递消息"}, true, nil
		}
		resolved := h.resolveNewPlatformFeishuSession(ctx, user.UserIDLC(), sessionName)
		keyID, found = resolved.keyID, resolved.status == ownerSessionUniqueHit
		if found {
			sessionName = resolved.sessionName
		}
	} else if h.Identity != nil && msg.SenderOpenID != "" {
		logFeishuDispatchRejected("sender_not_active_member", msg.SenderOpenID, sessionName, msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "你的账号未绑定或已停用，无法控制会话"}, true, nil
	}
	if !found {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到可控制的会话，请确认会话已注册且当前账号已绑定。"}, true, nil
	}
	enabled := action == "on"
	mirrorTo := ""
	if enabled {
		mirrorTo = feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID))
	}
	if err := h.Repo.SetSessionMirror(ctx, keyID, sessionName, enabled, mirrorTo); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	delivered := true
	if err := h.RouteEnvelope(ctx, model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: "mirror " + action,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":      "mirror_control",
			"enabled":   enabled,
			"mirror_to": mirrorTo,
			"system":    true,
			"no_mirror": true,
		},
	}); err != nil {
		delivered = false
	}
	if enabled {
		if delivered {
			return model.PrefixDispatchResult{Matched: true, Reply: "镜像已开启：" + sessionName}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "镜像已开启并保存；会话当前离线，重连后生效：" + sessionName}, true, nil
	}
	if delivered {
		return model.PrefixDispatchResult{Matched: true, Reply: "镜像已关闭：" + sessionName}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "镜像已关闭并保存；会话当前离线：" + sessionName}, true, nil
}

func (h *Hub) dispatchFeishuSyncCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	action, sessionName, explicitKey, ok := parseSyncCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true, Reply: "该指令仅支持与机器人的单聊"}, true, nil
	}
	openID := firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(sourceAccount(msg)))
	if openID == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "无法识别发起人 open_id"}, true, nil
	}
	target := feishuSyncTarget{
		openID:       openID,
		botName:      h.botName(msg.BotChannelID),
		botChannelID: msg.BotChannelID,
	}
	newUser, hasNewUser := h.newPlatformFeishuUser(ctx, msg)
	if !hasNewUser && h.Identity != nil && msg.SenderOpenID != "" {
		logFeishuDispatchRejected("sender_not_active_member", msg.SenderOpenID, sessionName, msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "你的账号未绑定或已停用，无法控制会话"}, true, nil
	}
	if hasNewUser && newUser.State != identity.UserActive {
		logFeishuDispatchRejected("sender_inactive", msg.SenderOpenID, sessionName, msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "你的账号已停用，无法控制会话"}, true, nil
	}
	if action == "unsync" {
		ownerKey := h.controlOwnerKey(ctx, msg)
		if hasNewUser {
			ownerKey = newUser.UserIDLC()
			if full := h.resolveNewPlatformVisibleSession(ctx, sessionName); full.fullName {
				if !full.found || full.ownerLC != newUser.UserIDLC() {
					return model.PrefixDispatchResult{Matched: true, Reply: "未找到该会话的同步登记（可能是从别的飞书会话开启的）"}, true, nil
				}
				sessionName = full.sessionName
			}
		}
		var candidateKeys []string
		if explicitKey != "" {
			if hasNewUser {
				if explicitKey != AgentKeyID(newUser.UserIDLC()) {
					return model.PrefixDispatchResult{Matched: true, Reply: "无权同步他人会话"}, true, nil
				}
				candidateKeys = []string{explicitKey}
			} else {
				targetOwner := h.ownerKeyForKey(ctx, explicitKey)
				if ownerKey == "" || targetOwner == "" || ownerKey != targetOwner {
					return model.PrefixDispatchResult{Matched: true, Reply: "无权同步他人会话"}, true, nil
				}
				candidateKeys = []string{explicitKey}
			}
		} else if hasNewUser {
			candidateKeys = []string{AgentKeyID(newUser.UserIDLC())}
		} else {
			var err error
			candidateKeys, err = h.registeredOwnerSessionKeys(ctx, ownerKey, sessionName)
			if err != nil {
				return model.PrefixDispatchResult{}, true, err
			}
		}
		for _, keyID := range candidateKeys {
			target.keyID = keyID
			if h.removeFeishuSyncTarget(keyID, sessionName, target) {
				return model.PrefixDispatchResult{Matched: true, Reply: "已停止同步 " + sessionName}, true, nil
			}
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到该会话的同步登记（可能是从别的飞书会话开启的）"}, true, nil
	}
	targetKey, found := "", false
	if explicitKey != "" {
		if hasNewUser {
			if explicitKey != AgentKeyID(newUser.UserIDLC()) {
				return model.PrefixDispatchResult{Matched: true, Reply: "无权同步他人会话"}, true, nil
			}
			resolved := h.resolveNewPlatformFeishuSession(ctx, newUser.UserIDLC(), sessionName)
			targetKey, found = resolved.keyID, resolved.status == ownerSessionUniqueHit
			if found {
				sessionName = resolved.sessionName
			}
		} else {
			sourceOwner := h.controlOwnerKey(ctx, msg)
			targetOwner := h.ownerKeyForKey(ctx, explicitKey)
			if sourceOwner == "" || targetOwner == "" || sourceOwner != targetOwner {
				return model.PrefixDispatchResult{Matched: true, Reply: "无权同步他人会话"}, true, nil
			}
			if h.sessionOnline(explicitKey, sessionName) {
				targetKey, found = explicitKey, true
			}
		}
	} else if hasNewUser {
		resolved := h.resolveNewPlatformFeishuSession(ctx, newUser.UserIDLC(), sessionName)
		targetKey, found = resolved.keyID, resolved.status == ownerSessionUniqueHit
		if found {
			sessionName = resolved.sessionName
		}
	} else {
		ownerKey := h.controlOwnerKey(ctx, msg)
		targetKey, found = h.resolveOwnerSessionKey(ctx, ownerKey, sessionName, "")
	}
	if !found {
		if explicitKey == "" {
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到你名下在线会话：" + sessionName + "。同步他人会话请显式使用 #sync " + sessionName + "#<key>。"}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到在线会话：" + sessionName + "#" + explicitKey}, true, nil
	}
	target.keyID = targetKey
	switch action {
	case "sync":
		for _, item := range h.addFeishuSyncTargetAndSnapshot(targetKey, sessionName, target) {
			if err := h.sendFeishuSyncItem(ctx, targetKey, sessionName, target, item); err != nil {
				return model.PrefixDispatchResult{}, true, err
			}
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "已开启同步 " + sessionName}, true, nil
	default:
		return model.PrefixDispatchResult{}, false, nil
	}
}

// registeredOwnerSessionKeys resolves persisted endpoints without requiring an
// online websocket. It is intentionally used only for local #unsync cleanup;
// enabling sync still requires a live session.
func (h *Hub) registeredOwnerSessionKeys(ctx context.Context, ownerKey, sessionName string) ([]string, error) {
	if h.Repo == nil || ownerKey == "" || sessionName == "" {
		return nil, nil
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var keys []string
	for _, ep := range endpoints {
		if ep.OwnerKey != ownerKey || (ep.SessionName != sessionName && ep.FullSessionName != sessionName) || seen[ep.KeyID] {
			continue
		}
		seen[ep.KeyID] = true
		keys = append(keys, ep.KeyID)
	}
	sort.Strings(keys)
	return keys, nil
}

func (h *Hub) dispatchApplyKeyCommand(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	cmd, arg, ok := parseApplyKeyCommand(text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if h.Repo == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂不可用：存储未配置"}, true, nil
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{Matched: true}, true, nil
	}
	switch cmd {
	case "apply":
		return h.createKeyApplication(ctx, msg, arg)
	case "approve":
		return h.approveKeyApplication(ctx, msg, arg)
	case "reject":
		id, reason := splitApplyReviewArg(arg)
		return h.rejectKeyApplication(ctx, msg, id, reason)
	default:
		return model.PrefixDispatchResult{}, false, nil
	}
}

func (h *Hub) createKeyApplication(ctx context.Context, msg model.Message, description string) (model.PrefixDispatchResult, bool, error) {
	description = strings.TrimSpace(description)
	if description == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "请补充申请说明，例如：#申请 接入 zzc-developer"}, true, nil
	}
	openID := applicantOpenID(msg)
	if openID == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "无法识别申请人 open_id"}, true, nil
	}
	approver := h.applyKeyApproverOpenID()
	if approver == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂未配置审批人"}, true, nil
	}
	botName := h.botName(msg.BotChannelID)
	app, err := h.Repo.CreateKeyApplication(ctx, model.KeyApplication{
		ApplicantOpenID:  openID,
		ApplicantAccount: msg.BotChannelID + ":personal:" + openID,
		ApplicantBotID:   msg.BotChannelID,
		ApplicantBotName: botName,
		Description:      description,
		Status:           "pending",
		CreatedAt:        time.Now().UTC(),
	})
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	_ = h.Repo.WriteAudit(ctx, app.ApplicantOpenID, "key_application_create", app.ID)
	notice := fmt.Sprintf("收到新的 DingWei key 申请\nID：%s\n申请人：%s\n说明：%s\n\n批准：#批准%s\n拒绝：#拒绝%s 原因", app.ID, app.ApplicantOpenID, app.Description, app.ID, app.ID)
	if err := h.enqueueFeishuText(ctx, msg.BotChannelID, approver, "apply-key-review-"+app.ID, notice); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "已提交 key 申请，申请 ID：" + app.ID + "。审批通过后会私聊发放。"}, true, nil
}

func (h *Hub) approveKeyApplication(ctx context.Context, msg model.Message, id string) (model.PrefixDispatchResult, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "请提供申请 ID，例如：#批准<申请ID>"}, true, nil
	}
	approver := h.applyKeyApproverOpenID()
	if approver == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂未配置审批人"}, true, nil
	}
	if got := applicantOpenID(msg); got != approver {
		return model.PrefixDispatchResult{Matched: true, Reply: "只有指定审批人可以批准 key 申请"}, true, nil
	}
	app, err := h.Repo.GetKeyApplication(ctx, id)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if app == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到 key 申请：" + id}, true, nil
	}
	if app.Status != "pending" {
		return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("申请 %s 当前状态为 %s，不能重复审批", app.ID, app.Status)}, true, nil
	}
	if _, ok := h.Outbound.(sensitiveOutboundRememberer); !ok {
		return model.PrefixDispatchResult{}, true, errors.New("outbound queue does not support one-time secret delivery")
	}
	serviceID := applyKeyServiceID(app.ApplicantOpenID)
	secret, key := h.newAPIKey(serviceID, app.ApplicantOpenID)
	account := firstNonEmpty(app.ApplicantAccount, app.ApplicantBotID+":personal:"+app.ApplicantOpenID)
	grantID := "apply-key-grant-" + app.ID
	grantText := applyKeyGrantText(key.ID, secret)
	storedGrantText := applyKeyGrantStoredText(key.ID)
	grantContent, _ := json.Marshal(map[string]string{"text": storedGrantText})
	now := time.Now().UTC()
	h.rememberSensitiveOutbound(grantID, mustTextContent(grantText))
	if err := h.Repo.ApproveKeyApplicationWithGrant(ctx, app.ID, approver, model.RegisteredService{
		ID:           serviceID,
		Name:         "DingWei key " + app.ApplicantOpenID,
		Description:  "Self-service key application " + app.ID,
		DeliveryType: "ws",
		ReplyMode:    "sync",
		Enabled:      true,
	}, key, account, model.ChatEntity{
		ID:           account,
		BotChannelID: app.ApplicantBotID,
		Type:         model.ChatPersonal,
		FeishuID:     app.ApplicantOpenID,
		DisplayName:  app.ApplicantOpenID,
		Active:       true,
	}, model.Message{
		ID:           grantID,
		ChatEntityID: app.ApplicantBotID + ":personal:" + app.ApplicantOpenID,
		Direction:    model.DirectionOut,
		BotChannelID: app.ApplicantBotID,
		FeishuMsgID:  grantID,
		ChatType:     model.ChatPersonal,
		Content:      string(grantContent),
		Status:       "queued",
	}, now); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	_ = h.Repo.WriteAudit(ctx, approver, "key_application_approve", app.ID+":"+key.ID)
	return model.PrefixDispatchResult{Matched: true, Reply: "已批准申请 " + app.ID + "，key_id：" + key.ID + "。secret 已私聊发放给申请人。"}, true, nil
}

func (h *Hub) rejectKeyApplication(ctx context.Context, msg model.Message, id, reason string) (model.PrefixDispatchResult, bool, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "请提供申请 ID，例如：#拒绝<申请ID> 原因"}, true, nil
	}
	approver := h.applyKeyApproverOpenID()
	if approver == "" {
		return model.PrefixDispatchResult{Matched: true, Reply: "自助申请 key 暂未配置审批人"}, true, nil
	}
	if got := applicantOpenID(msg); got != approver {
		return model.PrefixDispatchResult{Matched: true, Reply: "只有指定审批人可以拒绝 key 申请"}, true, nil
	}
	app, err := h.Repo.GetKeyApplication(ctx, id)
	if err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	if app == nil {
		return model.PrefixDispatchResult{Matched: true, Reply: "未找到 key 申请：" + id}, true, nil
	}
	if app.Status != "pending" {
		return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("申请 %s 当前状态为 %s，不能重复审批", app.ID, app.Status)}, true, nil
	}
	now := time.Now().UTC()
	if err := h.Repo.RejectKeyApplication(ctx, app.ID, approver, strings.TrimSpace(reason), now); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	_ = h.Repo.WriteAudit(ctx, approver, "key_application_reject", app.ID)
	reply := "你的 DingWei key 申请已被拒绝。申请 ID：" + app.ID
	if strings.TrimSpace(reason) != "" {
		reply += "\n原因：" + strings.TrimSpace(reason)
	}
	if err := h.enqueueFeishuText(ctx, app.ApplicantBotID, app.ApplicantOpenID, "apply-key-reject-"+app.ID, reply); err != nil {
		return model.PrefixDispatchResult{}, true, err
	}
	return model.PrefixDispatchResult{Matched: true, Reply: "已拒绝申请 " + app.ID}, true, nil
}

func parseApplyKeyCommand(text string) (cmd, arg string, ok bool) {
	text = strings.TrimSpace(stripLeadingMentions(text))
	for _, item := range []struct {
		prefix string
		cmd    string
	}{
		{"#申请", "apply"},
		{"#批准", "approve"},
		{"#拒绝", "reject"},
	} {
		if strings.HasPrefix(text, item.prefix) {
			return item.cmd, strings.TrimSpace(strings.TrimPrefix(text, item.prefix)), true
		}
	}
	return "", "", false
}

func splitApplyReviewArg(arg string) (id, reason string) {
	fields := strings.Fields(strings.TrimSpace(arg))
	if len(fields) == 0 {
		return "", ""
	}
	id = fields[0]
	if len(fields) > 1 {
		reason = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(arg), id))
	}
	return id, reason
}

func applicantOpenID(msg model.Message) string {
	return firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(sourceAccount(msg)))
}

func (h *Hub) applyKeyApproverOpenID() string {
	return firstNonEmpty(strings.TrimSpace(applyKeyApproverID), strings.TrimSpace(secOpsAdminOpenID))
}

func applyKeyServiceID(openID string) string {
	return "apply:" + sanitizeKeyIDPart(openID)
}

func (h *Hub) enqueueFeishuText(ctx context.Context, botChannelID, openID, id, text string) error {
	return h.enqueueFeishuSensitiveText(ctx, botChannelID, openID, id, text, "")
}

func (h *Hub) enqueueFeishuSensitiveText(ctx context.Context, botChannelID, openID, id, text, storedText string) error {
	if h.Outbound == nil {
		return errors.New("outbound queue not configured")
	}
	content, _ := json.Marshal(map[string]string{"text": text})
	storedContent := string(content)
	sensitiveContent := ""
	if storedText != "" {
		stored, _ := json.Marshal(map[string]string{"text": storedText})
		storedContent = string(stored)
		sensitiveContent = string(content)
	}
	return h.Outbound.Enqueue(ctx, model.Message{
		ID:               id,
		ChatEntityID:     botChannelID + ":personal:" + openID,
		BotChannelID:     botChannelID,
		FeishuMsgID:      id,
		ChatType:         model.ChatPersonal,
		Content:          storedContent,
		SensitiveContent: sensitiveContent,
	})
}

func applyKeyGrantText(keyID, secret string) string {
	return strings.Join([]string{
		"DingWei key 已批准，请立即保存；secret 只显示这一次。",
		"",
		"key_id: " + keyID,
		"secret: " + secret,
		"",
		"Linux / macOS:",
		"1. 确认已安装 python3、tmux 和你的 AI CLI。",
		"2. 拉取或解压 sessionHelper 接入包，进入 tools/sessionhelper。",
		"3. 运行 ./run.sh --reconfigure，按提示填写 SH_WS_BASE、会话名、key_id 和 secret。",
		"4. 后续运行 ./run.sh 启动；Linux 可用 cron/guard，macOS 可用 launchd 或前台 nohup。",
		"",
		"Windows WSL2:",
		"1. 安装 Ubuntu WSL2，并在 WSL 内安装 python3、tmux 和 AI CLI。",
		"2. 在 WSL 内按 Linux 步骤运行 tools/sessionhelper/run.sh --reconfigure。",
		"3. 配置写在 WSL 用户目录，保持 WSL 网络可访问 DingWei Hub。",
	}, "\n")
}

func applyKeyGrantStoredText(keyID string) string {
	return strings.Replace(applyKeyGrantText(keyID, "***"), "secret: ***", "secret 已隐藏", 1)
}

type sensitiveOutboundRememberer interface {
	RememberSensitiveContent(id, content string)
}

func (h *Hub) rememberSensitiveOutbound(id, content string) {
	if q, ok := h.Outbound.(sensitiveOutboundRememberer); ok {
		q.RememberSensitiveContent(id, content)
	}
}

func mustTextContent(text string) string {
	content, _ := json.Marshal(map[string]string{"text": text})
	return string(content)
}

func parseSyncCommand(text string) (action, sessionName, keyID string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(stripLeadingMentions(text)))
	if len(fields) != 2 {
		return "", "", "", false
	}
	switch strings.ToLower(fields[0]) {
	case "#sync":
		action = "sync"
	case "#unsync":
		action = "unsync"
	default:
		return "", "", "", false
	}
	target := strings.TrimSpace(fields[1])
	if target == "" {
		return "", "", "", false
	}
	if strings.Contains(target, "#") {
		parts := strings.SplitN(target, "#", 2)
		sessionName, keyID = strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
		if sessionName == "" || keyID == "" || strings.Contains(keyID, "#") {
			return "", "", "", false
		}
		return action, sessionName, keyID, true
	}
	return action, target, "", true
}

func (h *Hub) addFeishuSyncTargetAndSnapshot(keyID, sessionName string, target feishuSyncTarget) []terminalSyncItem {
	k := terminalKey(keyID, sessionName)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.syncTargets[k] == nil {
		h.syncTargets[k] = map[string]feishuSyncTarget{}
	}
	h.syncTargets[k][target.syncKey()] = target
	items := h.recentTerminal[k]
	out := make([]terminalSyncItem, len(items))
	copy(out, items)
	return out
}

func (h *Hub) removeFeishuSyncTarget(keyID, sessionName string, target feishuSyncTarget) bool {
	k := terminalKey(keyID, sessionName)
	h.mu.Lock()
	defer h.mu.Unlock()
	removed := false
	if targets := h.syncTargets[k]; targets != nil {
		if _, ok := targets[target.syncKey()]; ok {
			delete(targets, target.syncKey())
			removed = true
		}
		if len(targets) == 0 {
			delete(h.syncTargets, k)
		}
	}
	for bufferKey, buffer := range h.syncBuffers {
		if buffer.keyID == keyID && buffer.sessionName == sessionName && buffer.target.syncKey() == target.syncKey() {
			if buffer.timer != nil {
				buffer.timer.Stop()
			}
			delete(h.syncBuffers, bufferKey)
		}
	}
	return removed
}

func (h *Hub) appendTerminalSyncItemLocked(keyID, sessionName, text string, ts time.Time) (terminalSyncItem, []feishuSyncTarget, bool) {
	k := terminalKey(keyID, sessionName)
	kind, toolName := classifyTerminalSyncText(text)
	if kind == terminalSyncTool {
		summary := h.syncToolSummaries[k]
		if summary == nil {
			summary = &terminalToolSummary{byName: map[string]int{}}
			h.syncToolSummaries[k] = summary
		}
		summary.total++
		summary.byName[toolName]++
		return terminalSyncItem{}, nil, false
	}
	if kind == terminalSyncTurnEnd {
		summary := h.syncToolSummaries[k]
		delete(h.syncToolSummaries, k)
		if summary == nil || summary.total == 0 {
			return terminalSyncItem{}, nil, false
		}
		text = formatTerminalToolSummary(*summary)
	}
	item := terminalSyncItem{TS: ts.UTC(), Text: text}
	items := append(h.recentTerminal[k], item)
	if len(items) > 10 {
		items = items[len(items)-10:]
	}
	h.recentTerminal[k] = items
	targets := make([]feishuSyncTarget, 0, len(h.syncTargets[k]))
	for _, target := range h.syncTargets[k] {
		targets = append(targets, target)
	}
	return item, targets, true
}

func (h *Hub) closeFeishuSyncLocked(keyID, sessionName string) {
	k := terminalKey(keyID, sessionName)
	delete(h.recentTerminal, k)
	delete(h.syncTargets, k)
	delete(h.syncToolSummaries, k)
	for bufferKey, buffer := range h.syncBuffers {
		if buffer.keyID == keyID && buffer.sessionName == sessionName {
			if buffer.timer != nil {
				buffer.timer.Stop()
			}
			delete(h.syncBuffers, bufferKey)
		}
	}
}

type terminalSyncKind uint8

const (
	terminalSyncContent terminalSyncKind = iota
	terminalSyncTool
	terminalSyncTurnEnd
)

// classifyTerminalSyncText recognizes the stable textual protocol emitted by
// sessionhelper.driver_adapter._format_event. Only a chunk whose trimmed text
// begins with "[tool] <name>" is a tool operation. Embedded mentions such as
// assistant prose containing "[tool]" and error/session lines remain content.
func classifyTerminalSyncText(text string) (terminalSyncKind, string) {
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "[tool] ") {
		fields := strings.Fields(strings.TrimSpace(strings.TrimPrefix(trimmed, "[tool] ")))
		if len(fields) > 0 {
			return terminalSyncTool, fields[0]
		}
	}
	if strings.HasPrefix(trimmed, "--- turn done (") && strings.HasSuffix(trimmed, "---") {
		return terminalSyncTurnEnd, ""
	}
	return terminalSyncContent, ""
}

func formatTerminalToolSummary(summary terminalToolSummary) string {
	names := make([]string, 0, len(summary.byName))
	for name := range summary.byName {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s×%d", name, summary.byName[name]))
	}
	return fmt.Sprintf("本回合发生工具操作 %d 次（%s）", summary.total, strings.Join(parts, "、"))
}

func (h *Hub) sendFeishuSyncItem(ctx context.Context, keyID, sessionName string, target feishuSyncTarget, item terminalSyncItem) error {
	if strings.TrimSpace(item.Text) == "" {
		return nil
	}
	env := model.Envelope{
		ID:   randomHex(16),
		To:   feishuAddress(target.openID, keyID, target.botName),
		From: sessionAddress(sessionName, keyID),
		Body: formatTerminalSyncText(item),
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":                  "feishu_sync",
			"source_bot_channel_id": target.botChannelID,
		},
	}
	return h.RouteEnvelope(ctx, env)
}

func (h *Hub) queueFeishuSyncItem(keyID, sessionName string, target feishuSyncTarget, item terminalSyncItem) {
	if strings.TrimSpace(item.Text) == "" {
		return
	}
	bufferKey := terminalKey(keyID, sessionName) + ":" + target.syncKey()
	h.mu.Lock()
	buffer := h.syncBuffers[bufferKey]
	if buffer == nil {
		buffer = &feishuSyncBuffer{keyID: keyID, sessionName: sessionName, target: target}
		h.syncBuffers[bufferKey] = buffer
	}
	buffer.items = append(buffer.items, item)
	if buffer.timer == nil {
		buffer.timer = time.AfterFunc(time.Second, func() {
			h.backgroundWork("sync_timer", func() { h.flushFeishuSyncBuffer(bufferKey) })
		})
	}
	h.mu.Unlock()
}

// hasFeishuSyncOrMirror reports whether this session already has an effective
// sync/mirror path to the exact Feishu origin. reply-to-origin must defer to
// that path before sending; otherwise a later terminal flush duplicates the
// same assistant answer regardless of how long after turn completion it lands.
func (h *Hub) hasFeishuSyncOrMirror(ctx context.Context, keyID, sessionName, origin string) bool {
	to, err := parseAddress(origin)
	if err != nil || to.Kind != addressFeishu || to.OpenID == "" {
		return false
	}
	h.mu.Lock()
	for _, target := range h.syncTargets[terminalKey(keyID, sessionName)] {
		if target.openID != to.OpenID {
			continue
		}
		if to.BotName != "" && target.botName != "" && !strings.EqualFold(to.BotName, target.botName) {
			continue
		}
		h.mu.Unlock()
		return true
	}
	h.mu.Unlock()
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return false
	}
	for _, ep := range endpoints {
		if ep.KeyID != keyID || ep.SessionName != sessionName || !ep.Active || !ep.MirrorEnabled {
			continue
		}
		mirror, err := parseAddress(ep.MirrorTo)
		if err == nil && mirror.Kind == addressFeishu && mirror.OpenID == to.OpenID &&
			(to.BotName == "" || mirror.BotName == "" || strings.EqualFold(to.BotName, mirror.BotName)) {
			return true
		}
	}
	return false
}

func (h *Hub) flushFeishuSyncBuffer(bufferKey string) {
	h.mu.Lock()
	buffer := h.syncBuffers[bufferKey]
	if buffer == nil {
		h.mu.Unlock()
		return
	}
	delete(h.syncBuffers, bufferKey)
	items := append([]terminalSyncItem(nil), buffer.items...)
	keyID, sessionName, target := buffer.keyID, buffer.sessionName, buffer.target
	active := false
	if targets := h.syncTargets[terminalKey(keyID, sessionName)]; targets != nil {
		_, active = targets[target.syncKey()]
	}
	h.mu.Unlock()
	if !active || len(items) == 0 {
		return
	}
	var b strings.Builder
	for _, item := range items {
		b.WriteString(item.Text)
	}
	text := strings.TrimSpace(b.String())
	if text == "" {
		return
	}
	_ = h.sendFeishuSyncItem(context.Background(), keyID, sessionName, target, terminalSyncItem{TS: items[0].TS, Text: text})
}

func formatTerminalSyncText(item terminalSyncItem) string {
	return fmt.Sprintf("[%s] %s", item.TS.UTC().Format("2006-01-02 15:04:05 UTC"), item.Text)
}

func sanitizeTerminalSyncText(text string) string {
	text = ansiOSCSTRE.ReplaceAllString(text, "")
	text = ansiCSIRE.ReplaceAllString(text, "")
	text = ansiSimpleRE.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = controlExceptTextRE.ReplaceAllString(text, "")
	if strings.TrimSpace(text) == "" {
		return ""
	}
	return text
}

func (t feishuSyncTarget) syncKey() string {
	return t.botChannelID + ":" + t.openID
}

func (h *Hub) forward(ctx context.Context, serviceID string, req ForwardRequest, timeoutMs int) (string, error) {
	var c *client
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		c = h.serviceClients[serviceID]
	}()
	if c == nil {
		return "", errors.New("service websocket offline")
	}
	if timeoutMs <= 0 {
		timeoutMs = 5000
	}
	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	payload, _ := json.Marshal(req)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.conn.Write(callCtx, websocket.MessageText, payload); err != nil {
		return "", err
	}
	var data []byte
	select {
	case data = <-c.resp:
	case <-c.readDone:
		return "", errors.New("service websocket closed")
	case <-callCtx.Done():
		// 🔴 超时即关掉这条服务连接（复验 tosonnet55tc2k，hub-ws-blocking ③）：ForwardResponse 不带请求 ID，
		//    回复只能按先后对应 —— 连接留着，这次调用的迟到回复会被下一次调用当成自己的答案（且之后整体错位）。
		//    关掉后迟到回复无处可去，下一次调用写不出去、报错；服务重连后从干净的连接重新开始。
		_ = c.conn.CloseNow()
		return "", callCtx.Err()
	}
	var resp ForwardResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", err
	}
	if resp.Error != "" {
		return "", errors.New(resp.Error)
	}
	return resp.Reply, nil
}

func logFeishuDispatchRejected(reason, openID, target, botChannel string) {
	log.Printf("feishu dispatch rejected reason=%s open_id=%q target=%q bot_channel=%q", reason, openID, target, botChannel)
}

func feishuDispatchLogSource(addr address) (openID, botChannel string) {
	if addr.Kind == addressFeishu {
		return addr.OpenID, addr.BotName
	}
	return "", ""
}

// rejectUnnegotiatedSubAgentEnvelope fails closed while this Hub has no
// sub_agents_v1 negotiation. These fields are Hub-owned routing attestations;
// a sender cannot create trust by supplying them in envelope metadata.
func rejectUnnegotiatedSubAgentEnvelope(env model.Envelope) error {
	for _, key := range []string{
		"sub_path", "target_full_name", "target_node_id", "lane_generation", "subtree_revision",
		"from_full_name", "from_node_id", "from_generation",
	} {
		if _, present := env.Meta[key]; present {
			return fmt.Errorf("sub_agent_unsupported: reserved route field %q requires negotiated sub-agent capability", key)
		}
	}
	return nil
}

func (h *Hub) RouteEnvelope(ctx context.Context, env model.Envelope) error {
	client, _ := ctx.Value(subAgentClientKey{}).(*sessionClient)
	if err := h.rejectSubAgentIngress(client, env); err != nil {
		return err
	}
	if err := h.taskDispatchEnvelopeAllowed(ctx, env); err != nil {
		return err
	}
	if legacySpecGenerationEnvelope(env) {
		guarded, release, err := h.beginLegacySpecWrite(ctx)
		if err != nil {
			return err
		}
		defer release()
		ctx = guarded
	}

	if metaString(env.Meta, "kind") == "task_blocker_ready" && ctx.Value(blockerDispatchKey{}) != true {
		return errors.New("blocker wake is Hub-only")
	}
	if coordinateReviewEnvelope(env) && ctx.Value(coordinateReviewDispatchKey{}) != true {
		return errors.New("coordinate review is Hub-only")
	}
	if specReviewType(specReviewEnvelopeType(env)) && ctx.Value(specAgentDispatchContextKey{}) != true {
		return errors.New("spec review request is Hub-only")
	}
	if metaString(env.Meta, "kind") == "task_assigned" && ctx.Value(taskNotificationContextKey{}) != true {
		return errors.New("task notification is Hub-only")
	}
	stripForgedExtseatAttribution(&env)

	if subAgentRequested(env) {
		return h.routeSubAgentEnvelope(ctx, env)
	}
	// Root address plus any child hint can never imply a child target.
	if err := rejectUnnegotiatedSubAgentEnvelope(env); err != nil {
		if capable, _ := h.subAgentCapable(client); capable {
			return subagent.Fail("path_conflict", "explicit child target required")
		}
		return err
	}
	// reply-origin / loop-termination:conversation 类消息先过 R4 hop / R5 dedup 兜底 + origin 捕获。
	// control 类与无新字段的 legacy 消息透传不变(向后兼容)。drop=静默丢弃,不回错给发方。
	env, drop := h.applyReplyOriginTermination(env)
	if drop {
		return nil
	}
	to, err := parseAddress(env.To)
	if err != nil {
		return err
	}
	from, err := parseAddress(env.From)
	if err != nil {
		return err
	}
	if isCollectEnvelope(env) {
		if to.KeyID != from.KeyID {
			return errors.New("to/from key_id mismatch")
		}
		return h.storeCollectEnvelope(ctx, from, env)
	}
	switch to.Kind {
	case addressSession:
		// Only a Hub-generated automatic reply uses the durable exact source
		// address. It still passes the current group policy before any delivery.
		if metaBool(env.Meta, metaKeyReplyOriginReturn) && kindOf(env) == kindReply &&
			metaString(env.Meta, "route_source") == replyToOriginRouteSource && from.Kind == addressSession && !isAddressableFeishuID(to.SessionName) {
			if h.Identity != nil {
				if _, ok := h.newPlatformAgentAt(ctx, from.KeyID, from.SessionName); !ok {
					return errGroupAgentNotFound
				}
				if _, ok := h.newPlatformAgentAt(ctx, to.KeyID, to.SessionName); !ok {
					return errGroupAgentNotFound
				}
			}
			if err := h.enforceAgentGrouping(ctx, from, env, to.KeyID, to.SessionName); err != nil {
				return err
			}
			return h.routeToSession(ctx, to.KeyID, to.SessionName, env, false)
		}
		// send.py addresses a Feishu account/group as <ou_/oc_/on_ id>#<key>.
		// Recognize that shape before session/group resolution: a channel target is
		// not an Agent endpoint and must never be rejected by the Agent grouping gate.
		if isAddressableFeishuID(to.SessionName) {
			if to.KeyID != from.KeyID {
				return errors.New("to/from key_id mismatch")
			}
			return h.routeToFeishu(ctx, address{Kind: addressFeishu, OpenID: to.SessionName, KeyID: to.KeyID, BotName: h.defaultBotName()}, env)
		}
		// 🔴 T-044:workpulse 是**保留的系统地址**,不是会话名。
		// helper 会向 workpulse#<key> 发至少 9 类信封(session_capabilities / provision_ack /
		// delivery_ack / delivery_cancel_ack / comm_skill_ack / terminal_interrupt_ack /
		// terminal_output 等),Hub 按 meta.type 在会话路由**之前**拦截消费其中一部分。
		// 凡类型不在拦截名单内的,此前会掉进下面的会话解析 —— 而 workpulse 从来不是注册会话,
		// 于是报「目标会话不在你的账号下或不在线：workpulse」并**回一条投递失败通知**。
		// 实测当日复现两次,且失败形态是「再生成一条通知」,有构成通知风暴的风险。
		//
		// 修法:保留地址一律终止于 Hub,绝不落到会话解析。
		// 未被消费即属**本端程序问题**(helper 发了 Hub 不认的类型),不是发信方能处置的事,
		// 故记服务端日志而不回投递失败 —— 回给 Agent 它也做不了什么,只会刷屏。
		// 日志带 type/from,正是此前查不出"是哪一类漏出去"所缺的那条(helper 侧不记出站)。
		if to.SessionName == systemAddressName {
			log.Printf("session route decision=system_address_unconsumed target=%s type=%q from=%s env_id=%s",
				systemAddressName, metaString(env.Meta, "type"), env.From, env.ID)
			return nil
		}
		// 常规同 key 且在线 → 快路径直投
		if to.KeyID == from.KeyID && h.sessionOnline(to.KeyID, to.SessionName) {
			if err := h.enforceAgentGrouping(ctx, from, env, to.KeyID, to.SessionName); err != nil {
				return err
			}
			return h.routeToSession(ctx, to.KeyID, to.SessionName, env, from.Kind == addressFeishu)
		}
		// 可靠 helper 的能力已经由 Hub 在在线握手时坐实并持久化。连接瞬断时，
		// 同 key 的精确地址仍可安全入 durable queue；不把这一规则扩展到别名
		// 或跨 key，避免离线状态下猜测 owner 路由。
		if to.KeyID == from.KeyID && reliablePrimaryEnvelope(env) && h.Repo != nil {
			if deliveryACK, found, lookupErr := h.Repo.SessionEndpointDeliveryACK(ctx, to.KeyID, to.SessionName); lookupErr != nil {
				return lookupErr
			} else if found && deliveryACK {
				if err := h.enforceAgentGrouping(ctx, from, env, to.KeyID, to.SessionName); err != nil {
					return err
				}
				return h.routeToSession(ctx, to.KeyID, to.SessionName, env, from.Kind == addressFeishu)
			}
		}
		// 否则按发件人账号(owner)解析真实 key：支持同账号跨 key 互通 + 容错发件人补错 key + 强制同 owner。
		// （send.py 会把地址补成发件人自己的 key，所以即便 to.KeyID==from.KeyID，目标也可能在该 owner 的另一个 key 下）
		senderOwner := h.ownerKeyForKey(ctx, from.KeyID)
		if senderOwner == "" {
			return fmt.Errorf("无法解析发件人账号 key_id=%s", from.KeyID)
		}
		resolved := h.resolveOwnerSessionByShort(ctx, senderOwner, to.SessionName, to.KeyID)
		if resolved.status == ownerSessionAmbiguous {
			return ambiguousSessionError(to.SessionName, resolved.candidates)
		}
		if resolved.status != ownerSessionUniqueHit {
			// Cross-owner addressing is deliberately exact-only: the sender must
			// use the persisted FullSessionName, never a short/global fuzzy name.
			if full := h.resolveNewPlatformVisibleSession(ctx, to.SessionName); full.fullName && full.found {
				// External-seat replies are authorized by the external-seat route
				// itself. Handle the exact full name before the ordinary shared-group
				// gate, which intentionally does not grant every group member access
				// to an external endpoint.
				if handled, routeErr := h.routeExternalSeat(ctx, full.keyID, full.sessionName, env); handled {
					return routeErr
				}
				if err := h.enforceAgentGrouping(ctx, from, env, full.keyID, full.sessionName); err != nil {
					return err
				}
				env.To = sessionAddress(full.sessionName, full.keyID)
				return h.routeToSession(ctx, full.keyID, full.sessionName, env, from.Kind == addressFeishu)
			}
			if h.Identity == nil {
				full, found, lookupErr := h.resolveSessionByFullName(ctx, to.SessionName)
				if lookupErr != nil {
					return lookupErr
				}
				if found {
					if err := h.enforceAgentGrouping(ctx, from, env, full.KeyID, full.SessionName); err != nil {
						return err
					}
					env.To = sessionAddress(full.SessionName, full.KeyID)
					return h.routeToSession(ctx, full.KeyID, full.SessionName, env, from.Kind == addressFeishu)
				}
			}
			if _, isAgent, identityErr := h.sourceAgentIdentity(ctx, from, env); identityErr != nil {
				return identityErr
			} else if isAgent {
				return errGroupAgentNotFound
			}
			openID, botChannel := feishuDispatchLogSource(from)
			logFeishuDispatchRejected("target_not_owned_or_offline", openID, to.SessionName, botChannel)
			return errors.New("目标会话不在你的账号下或不在线：" + to.SessionName)
		}
		if err := h.enforceAgentGrouping(ctx, from, env, resolved.keyID, resolved.sessionName); err != nil {
			return err
		}
		env.To = sessionAddress(resolved.sessionName, resolved.keyID)
		return h.routeToSession(ctx, resolved.keyID, resolved.sessionName, env, from.Kind == addressFeishu)
	case addressFeishu:
		if to.KeyID != from.KeyID {
			return errors.New("to/from key_id mismatch")
		}
		return h.routeToFeishu(ctx, to, env)
	default:
		return errors.New("unsupported target address")
	}
}

// resolveOwnerSessionKey 在同一账号(owner)下按会话名找到在线目标会话的真实 key_id。
// 先信地址里给的 hintKey(若在线且同账号),否则在该 owner 的所有在线会话里按名匹配。
func (h *Hub) resolveOwnerSessionKey(ctx context.Context, ownerKey, sessionName, hintKey string) (string, bool) {
	if ownerKey == "" || sessionName == "" {
		return "", false
	}
	if hintKey != "" && h.sessionOnline(hintKey, sessionName) && h.ownerKeyForKey(ctx, hintKey) == ownerKey {
		return hintKey, true
	}
	h.mu.Lock()
	var candidates []string
	for keyID, sessions := range h.sessionClients {
		if sessions[sessionName] != nil {
			candidates = append(candidates, keyID)
		}
	}
	h.mu.Unlock()
	for _, keyID := range candidates {
		if h.ownerKeyForKey(ctx, keyID) == ownerKey {
			return keyID, true
		}
	}
	return "", false
}

func (h *Hub) handleAgentNetworkSkillAck(c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != agentNetworkSkillAckType && strings.TrimSpace(env.Body) != agentNetworkSkillAck {
		return false
	}
	c.mu.Lock()
	c.skillInstalled = true
	cancel := c.skillCancel
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return true
}

func (h *Hub) startAgentNetworkSkillPush(ctx context.Context, c *sessionClient) {
	if !h.reserveOnboardPush(c, time.Now().UTC()) {
		h.pushAgentNetworkSkillResume(ctx, c)
		return
	}
	h.pushAgentNetworkSkill(ctx, c)
	h.backgroundWork("owned", func() {
		func() {
			ticker := time.NewTicker(agentNetworkSkillRetryWait)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if h.agentNetworkSkillInstalled(c) {
						return
					}
					h.pushAgentNetworkSkill(ctx, c)
				}
			}
		}()
	})
}

func (h *Hub) pushAgentNetworkSkillResume(ctx context.Context, c *sessionClient) {
	_ = c.write(ctx, model.Envelope{
		ID: randomHex(16), To: sessionAddress(c.sessionName, c.keyID), From: sessionAddress("workpulse", c.keyID),
		Body: "近期已推送完整上线指南；本次重连沿用，不重复注入。", TS: time.Now().Unix(),
		Meta: map[string]any{"type": agentNetworkSkillPushType, "system": true, "no_mirror": true, "debounced": true},
	})
}

func (h *Hub) agentNetworkSkillInstalled(c *sessionClient) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.skillInstalled
}

func (h *Hub) pushAgentNetworkSkill(ctx context.Context, c *sessionClient) {
	if h.agentNetworkSkillInstalled(c) {
		return
	}
	body := h.renderAgentNetworkOnboard(ctx, c.sessionName, c.localName, c.keyID)
	env := model.Envelope{
		ID:   randomHex(16),
		To:   sessionAddress(c.sessionName, c.keyID),
		From: sessionAddress("workpulse", c.keyID),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"type":         agentNetworkSkillPushType,
			"system":       true,
			"no_mirror":    true,
			"ack_required": true,
			"ack_token":    agentNetworkSkillAck,
			"retry_after":  int(agentNetworkSkillRetryWait.Seconds()),
		},
	}
	if err := c.write(ctx, env); err != nil {
		return
	}
}

func (h *Hub) reserveOnboardPush(c *sessionClient, now time.Time) bool {
	key := terminalKey(c.keyID, c.sessionName)
	h.mu.Lock()
	defer h.mu.Unlock()
	if last := h.onboardPushed[key]; !last.IsZero() && now.Sub(last) < h.onboardInterval {
		return false
	}
	h.onboardPushed[key] = now
	return true
}

type onboardGitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type onboardOwnerIdentity struct {
	Public  onboardGitIdentity `json:"public"`
	Private onboardGitIdentity `json:"private"`
}

type onboardContext struct {
	OwnerKey  string
	OwnerName string
	Identity  onboardOwnerIdentity
	Brief     string
	Generated time.Time
}

func (h *Hub) renderAgentNetworkOnboard(ctx context.Context, sessionName, localName, keyID string) string {
	h.mu.Lock()
	info, cached := h.onboardContexts[keyID]
	h.mu.Unlock()
	if cached && time.Since(info.Generated) < h.onboardInterval {
		return renderAgentNetworkSkillWithContext(sessionName, localName, keyID, info)
	}
	info = onboardContext{Generated: time.Now().UTC()}
	if h.Repo != nil {
		info.OwnerKey = h.ownerKeyForKey(ctx, keyID)
	}
	info.OwnerName = info.OwnerKey
	if h.Repo != nil && info.OwnerKey != "" {
		if member, err := h.Repo.GetMemberByOwnerKey(ctx, info.OwnerKey); err == nil && member != nil && strings.TrimSpace(member.DisplayName) != "" {
			info.OwnerName = strings.TrimSpace(member.DisplayName)
		}
		if configs, err := h.Repo.ListAppConfig(ctx); err == nil {
			for _, cfg := range configs {
				switch cfg.Key {
				case "onboard.git_identities":
					var identities map[string]onboardOwnerIdentity
					if json.Unmarshal([]byte(cfg.ValueJSON), &identities) == nil {
						info.Identity = identities[info.OwnerKey]
					}
				case "onboard.project_briefs":
					var briefs map[string]string
					if json.Unmarshal([]byte(cfg.ValueJSON), &briefs) == nil {
						info.Brief = strings.TrimSpace(briefs[info.OwnerKey])
					}
				}
			}
		}
	}
	h.mu.Lock()
	h.onboardContexts[keyID] = info
	h.mu.Unlock()
	return renderAgentNetworkSkillWithContext(sessionName, localName, keyID, info)
}

func renderAgentNetworkSkill(sessionName, localName, keyID string) string {
	return renderAgentNetworkSkillWithContext(sessionName, localName, keyID, onboardContext{Generated: time.Now().UTC()})
}

func renderAgentNetworkSkillWithContext(sessionName, localName, keyID string, info onboardContext) string {
	// 在线清单文件由 sessionHelper 按【客户端自己配置的会话名】(SH_SESSION_NAME)命名，
	// 不是 Hub 派生的完整名；指南里的路径必须回指前者，否则会话读到的是不存在的文件。
	if strings.TrimSpace(localName) == "" {
		localName = sessionName
	}
	owner := info.OwnerKey
	if owner == "" {
		owner = "未识别"
	}
	ownerName := info.OwnerName
	if ownerName == "" {
		ownerName = owner
	}
	identityLine := func(label string, identity onboardGitIdentity) string {
		if identity.Name == "" || identity.Email == "" {
			return label + "：未配置；提交前从仓库/负责人确认，禁止猜测或沿用他人身份。"
		}
		return fmt.Sprintf("%s：git config user.name %q；git config user.email %q", label, identity.Name, identity.Email)
	}
	brief := info.Brief
	if brief == "" {
		brief = "当前 owner 暂无已配置项目简报；开始工作前读取仓库文档、任务书和最新交接。"
	}
	return strings.Join([]string{
		"DingWei 客户端上线指南",
		"生成时间（UTC）：" + info.Generated.UTC().Format(time.RFC3339),
		"",
		"你已接入DingWei Agent网络，可与同账号的其它在线AI CLI会话互相发消息、协作。",
		"",
		"【一、先查在线清单】谁在线是会变的，需要时你自己去读这个文件（sessionHelper 每分钟刷新，不会主动推给你）：",
		"  ~/.dingwei/" + localName + ".DingWeiOnlineSessions.list",
		"文件里每行是一个在线会话：#会话名 · 工具/模型 · 归属@成员 等。要发现可协作对象、或确认某会话是否在线，读它即可。",
		"重要：文件头有一行“更新于 <时间>”。若该时间距当前已超过 2 分钟，说明本机 sessionHelper 可能异常或已断连，此时清单不可信，不要据此判断在线状态。",
		"",
		"【二、给协作者发消息 / 回复】首选：在你输出的**第一行行首**写 @提及。",
		"  正例：@prodops 请复核这份 diff，重点看加锁次序。",
		"  正例（多目标）：@prodops @coder56solzzc 收到请各回一句。",
		"规则三条，缺一条就不会投递：",
		"  1) @ 必须在**整轮输出的行首**。写在文中、文末的 @ 不作为投递依据。",
		"  2) 名字可以只写短名，会自动补全到你 owner 名下的会话；但**子串至少 3 个字符**。",
		"     同一 owner 下若有多个会话都能匹配上，会回一条「无法分辨目标对象」而不是乱投。",
		"     ⚠️ **当前仅支持英文字母、数字及 _ . - 组成的短名，中文短名暂不支持**：",
		"     含中文的 @ 目标不会产生候选，也不会回任何提示，属静默无事发生。需要中文显示名时请另行提出。",
		"  3) ⚠️ **投出去的是整轮输出的全文**，不是那一行。@ 之前请先想清楚这一轮要不要整段给对方看。",
		"回复别人的消息，用同样的方式：在行首 @ 对方的会话名即可，**不需要额外工具**。",
		"  收件人 = 对方消息里【X→你】中的那个 X。",
		"",
		"兜底通道（首选不通时用，或需要发超长正文时用）：",
		"  python3 ~/.dingwei/send.py <收件会话名> <正文>",
		"  python3 ~/.dingwei/send.py <收件会话名> --file <文件路径>",
		"收件会话名务必用【在线清单】文件里的精确名字（如 zzc-manager，不要用简称或旧名），脚本会自动补全 #key 地址。",
		"",
		"⚠️ 下面两种写法在当前部署形态（driver 模式）下**不生效，且不会报错**，请不要使用：",
		"  · [[DW_ROUTE_V1 … ]] 控制块",
		"  · 「#会话名 正文」「@成员#会话名 正文」这类 legacy 前缀",
		"它们是早期形态的遗留，Hub 侧对应代码在 driver 模式下不可达。用了只会静默无事发生。",
		"",
		"不论走哪条路，Hub 收到的 from 永远由 helper 填写，不能伪造。",
		"",
		"【三、回复时间戳】每次状态回复、交接和结论都带明确时间戳（推荐 RFC3339，注明时区），不要只写“刚才/稍后”。",
		"",
		"【四、代码、测试与文档合规】当前身份：" + ownerName + "（owner=" + owner + "）。提交前按仓库公开性选用下列 DB 配置身份：",
		identityLine("公开仓身份", info.Identity.Public),
		identityLine("私有仓身份", info.Identity.Private),
		"公开仓与私有仓身份可能不同；不得硬编码其他 owner 的姓名/邮箱，不得添加未经要求的协作者尾注。",
		"代码改动须有相称测试；文档与行为同步；提交前检查 diff、敏感信息和作者信息。",
		"",
		"【五、项目知识简报（仅当前 owner）】",
		brief,
		"",
		"协作礼仪：消息要简明，说清诉求与来源，不刷屏。发给队友的正文第一行建议自报身份，例如【" + sessionName + "→manager】。",
		"",
		"收到并理解本指南后，请只回复下面这个固定确认标记，方便sessionHelper回执安装完成：",
		agentNetworkSkillAck,
	}, "\n")
}

func (h *Hub) routeSessionSelectorEnvelope(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	handled, err := h.routeSessionSelector(ctx, c.keyID, c.sessionName, env)
	if err != nil {
		h.writeDeliveryFailure(ctx, c, env, env.To, err)
	}
	return handled
}

func (h *Hub) writeDeliveryFailure(ctx context.Context, c *sessionClient, original model.Envelope, target string, routeErr error) {
	if c == nil || routeErr == nil || metaString(original.Meta, "type") == model.MetaTypeDeliveryFailure {
		log.Printf("delivery failure notification=suppressed reason=recursive_or_missing")
		return
	}
	routeErr = h.explainRevokedTarget(ctx, target, routeErr)
	now := time.Now().UTC()
	dedupKey := c.keyID + "\x00" + c.sessionName + "\x00" + target + "\x00" + routeErr.Error()
	h.mu.Lock()
	for key, at := range h.deliveryNotices {
		if now.Sub(at) > time.Minute {
			delete(h.deliveryNotices, key)
		}
	}
	if at, ok := h.deliveryNotices[dedupKey]; ok && now.Sub(at) <= time.Minute {
		h.mu.Unlock()
		log.Printf("delivery failure notification=suppressed reason=deduplicated session=%s", c.sessionName)
		return
	}
	h.deliveryNotices[dedupKey] = now
	h.mu.Unlock()
	env := deliveryFailureEnvelopeWithOriginal(c, original.ID, target, routeErr.Error(), original.Body, now)
	var large *extshared.BodyTooLargeError
	if errors.As(routeErr, &large) {
		env.Meta["error_code"] = "body_too_large"
		env.Meta["actual_bytes"] = large.ActualBytes
		env.Meta["limit_bytes"] = large.LimitBytes
	}
	if errors.Is(routeErr, errGroupAgentNotFound) {
		logFeishuDispatchRejected(h.deliveryFailureLogReason(routeErr, target), "", target, "")
		env.Body = strings.Replace(env.Body, routeErr.Error(), deliveryFailureNoticeReason(routeErr), 1)
	}
	// ㈡ 错误看板(业主 09-28「这类消息可以发到错误消息栏」/ 09-29「传到 view 页错误提示区」)。
	// 🔴 放在版本门**之前**:这一份是 Hub 自发的,不依赖对端 helper 版本 ⇒
	//    即使下面那道门把回发送方的通知关掉,**人**在看板上仍然看得到。
	//    少了这一步,"对旧客户端不发"就会把可见故障换成不可见故障。
	h.publishFailureDockEvent(c.keyID, c.sessionName, env.ID, original.ID, target, deliveryFailureNoticeReason(routeErr), now)
	h.annotateRestartWindow(&env, now, "delivery_failure")
	// ㈢ 版本门(业主 09-29 逐字「这一点在客户端没有升级支持的情况下可以不发」)。
	if amrClientTooOldForNoDisplayNotice(c.version) {
		log.Printf("delivery failure notification=suppressed reason=client_without_no_display_support session=%s client_version=%q min=%s dock_event=published",
			c.sessionName, c.version, amrNoDisplayNoticeMinVersion())
		return
	}
	if err := c.write(ctx, env); err != nil {
		log.Printf("delivery failure notification=failed session=%s err=%v", c.sessionName, err)
	}
}

// deliveryFailureNoticeReason 把 errGroupAgentNotFound 这一类**内部错误**换成给人看的那句话。
//
// 🔵 抽出来是因为它现在有**两个**消费方(通知正文的 Replace 与错误看板那张卡)。
// 两处各写一遍必然漂移 —— 本仓已在"两份清单各说各话"上吃过亏。
func deliveryFailureNoticeReason(routeErr error) string {
	if errors.Is(routeErr, errGroupAgentNotFound) {
		return groupAgentNotFoundText + "（reason=" + groupingRejectionReason(routeErr) + "）"
	}
	return routeErr.Error()
}

func (h *Hub) annotateRestartWindow(env *model.Envelope, now time.Time, event string) bool {
	if h == nil || env == nil || h.startedAt.IsZero() || h.restartWindow <= 0 {
		return false
	}
	age := now.Sub(h.startedAt)
	if age < 0 || age >= h.restartWindow {
		return false
	}
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["within_restart_window"] = true
	env.Meta["hub_uptime_seconds"] = int64(age / time.Second)
	env.Meta["restart_window_seconds"] = int64(h.restartWindow / time.Second)
	log.Printf("restart window signal event=%s within_restart_window=true hub_uptime_seconds=%d restart_window_seconds=%d",
		event, int64(age/time.Second), int64(h.restartWindow/time.Second))
	return true
}

func deliveryFailureEnvelope(c *sessionClient, sourceID, target, reason string, now time.Time) model.Envelope {
	return deliveryFailureEnvelopeWithOriginal(c, sourceID, target, reason, "", now)
}

func deliveryFailureEnvelopeWithOriginal(c *sessionClient, sourceID, target, reason, original string, now time.Time) model.Envelope {
	// T-060：`→ %s` 槽位的语义是**投递目标（会话名）**。回合级失败（TTL 关闭、配额淘汰）
	// 发生在目标解析**之前**，根本没有目标可填 —— 此前两处调用点往这里塞内部原因码
	// （"ttl"、"capacity"），渲染出「投递失败 … → ttl」，读起来像投给了一个叫 ttl 的会话。
	// 修法不是编一个目标，而是**没有目标时不出现那个箭头**。
	//
	// 2026-09-12 补完：首轮只清了上述两处，**遗漏了 amr.go 的另外三处**
	// （"terminal"、"empty"、"queue"，均经 notifySource 的 failure=true 分支进到这里）。
	// 现已全部改为空目标。清点方法记在这里，免得下次再漏：
	//   ① 直接调用 deliveryFailureEnvelope 的点
	//   ② **经 notifySource(..., failure=true) 间接到达的点** —— 首轮漏的就是这一类
	// 合法传值只有一种：真实会话名/写法（如 amrOffendingToken 给出的那个 @token）。
	// 凡是内部状态码、原因码、阶段名，一律传 ""。
	body := fmt.Sprintf("【投递失败】%s → %s；原因：%s", sourceID, target, reason)
	if target == "" {
		body = fmt.Sprintf("【投递失败】%s；原因：%s", sourceID, reason)
	}
	if original != "" {
		head, truncated := original, false
		if len([]rune(head)) > 80 {
			head = string([]rune(head)[:80])
			truncated = true
		}
		body += "；原文头部：" + head
		if truncated {
			body += "……（已截断，仅保留前80字）"
		} else {
			body += "（未截断）"
		}
	}
	body += "\n本条为系统通知，不可回程，请勿将回复作为业务答复发送。"
	env := model.Envelope{
		ID:   "delivery-failure-" + randomHex(16),
		To:   sessionAddress(c.sessionName, c.keyID),
		From: sessionAddress("workpulse", c.keyID),
		Body: body,
		TS:   now.Unix(),
		Meta: map[string]any{
			"type":      model.MetaTypeDeliveryFailure,
			"source_id": sourceID,
			// Failure notices remain visible to the CLI, but a turn caused by
			// one is a system-notification turn: it must not reply or generate
			// another failure notice. The helper propagates this explicit
			// semantic marker instead of inferring from the envelope ID.
			// 🔴 两支合并:类型值用 HNI 的 model 常量,语义标记保留回程修复那一支的。
			"suppress_reply_origin": true,
		},
	}
	if amrSupportsStructuredFailure(c.version) {
		env.Meta["presentation"] = "structured"
	}
	if amrSupportsNoDisplayNotice(c.version) {
		// 「不展示」的表达(业主 09-29 b「此信息不用于展示」)。
		// 🔴 **不覆盖** presentation="structured" —— 那是另一个语义(结构化失败),两键并存。
		// 🔴 已知:今天**没有消费方读这个键**(helper 那半 S-HELPER-NOTICE-NO-INJECT 认的是
		//    Meta["type"],见 internal/model/model.go 的 hub-meta-types 块)。加它的理由是
		//    协议上得有这个表达(否则"不展示"只是某个客户端的实现巧合),并给下一个消费方一个契约。
		//    它有自己的闸(键存在 ⇔ 版本支持),不是装饰 —— 见板件 §四 与 G3/G4。
		env.Meta["display"] = "none"
	}
	return env
}

// routeSessionSelector resolves and routes a selector emitted by an authenticated
// session. It deliberately has no websocket dependency so background consumers
// (notably completed-turn AMR) do not write errors to a stale source connection.
func (h *Hub) routeSessionSelector(ctx context.Context, sourceKeyID, sourceSession string, env model.Envelope) (bool, error) {
	handled, prepared, err := h.prepareSessionSelector(ctx, sourceKeyID, sourceSession, env)
	if err != nil || !handled {
		return handled, err
	}
	return true, h.RouteEnvelope(ctx, prepared)
}

// prepareSessionSelector performs authoritative owner/session resolution without
// sending. Observation-mode AMR uses it to distinguish a valid candidate from
// one that Hub policy would reject.
func (h *Hub) prepareSessionSelector(ctx context.Context, sourceKeyID, sourceSession string, env model.Envelope) (bool, model.Envelope, error) {
	text := strings.TrimSpace(env.To)
	if text == "" || (!strings.HasPrefix(text, "#") && !strings.HasPrefix(text, "@")) {
		return false, env, nil
	}
	if body := strings.TrimSpace(env.Body); body != "" {
		text += " " + body
	}
	if strings.HasPrefix(text, "#") {
		sessionName, body, ok := parseSelector(text)
		if !ok {
			return false, env, nil
		}
		// A selector emitted by an authenticated Agent may name a Feishu ID
		// directly. Canonicalize it here, before short-session resolution claims
		// the envelope; RouteEnvelope remains the single place that enforces the
		// to/from key match and performs Feishu delivery.
		if isAddressableFeishuID(sessionName) {
			env.To = feishuAddress(sessionName, sourceKeyID, h.defaultBotName())
			env.From = sessionAddress(sourceSession, sourceKeyID)
			env.Body = body
			return true, env, nil
		}
		// Exact public full_names may refer to another owner's group member.
		// Keep legacy short/fuzzy owner resolution unchanged; RouteEnvelope
		// remains responsible for the current group authorization check.
		if full := h.resolveNewPlatformVisibleSession(ctx, sessionName); full.fullName && full.found &&
			full.ownerLC != identity.Lower(h.ownerKeyForKey(ctx, sourceKeyID)) {
			// Retain the exact public name until RouteEnvelope resolves and
			// authorizes it; an internal short name would be owner-scoped again.
			env.To = sessionAddress(sessionName, sourceKeyID)
			env.From = sessionAddress(sourceSession, sourceKeyID)
			env.Body = body
			return true, env, nil
		}
		resolved := h.resolveOwnerSessionByShort(ctx, h.ownerKeyForKey(ctx, sourceKeyID), sessionName, "")
		if resolved.status == ownerSessionAmbiguous {
			return true, env, ambiguousSessionError(sessionName, resolved.candidates)
		}
		if resolved.status != ownerSessionUniqueHit {
			logFeishuDispatchRejected("short_name_not_resolvable", "", sessionName, "")
			return true, env, errors.New("目标会话无法补全为在线 canonical 地址：" + sessionName)
		}
		env.To = sessionAddress(resolved.sessionName, resolved.keyID)
		env.From = sessionAddress(sourceSession, sourceKeyID)
		env.Body = body
		return true, env, nil
	}
	memberName, sessionName, body, ok := parseMemberMention(text)
	if !ok {
		return false, env, nil
	}
	if h.Identity != nil {
		// New-platform member selectors use the same user-short-name directory as
		// public FullName resolution, never the retired legacy member table.
		user, err := h.Identity.Store.GetUserByShortName(ctx, identity.Lower(memberName))
		if err != nil || user.State != identity.UserActive {
			return true, env, errGroupAgentNotFound
		}
		publicName := sessionName + "." + user.ShortName
		target := h.resolveNewPlatformVisibleSession(ctx, publicName)
		if !target.found {
			return true, env, errGroupAgentNotFound
		}
		if env.Meta == nil {
			env.Meta = map[string]any{}
		}
		env.Meta["source_key_id"], env.Meta["source_session_name"] = sourceKeyID, sourceSession
		env.To, env.From, env.Body = sessionAddress(publicName, sourceKeyID), sessionAddress(sourceSession, sourceKeyID), body
		return true, env, nil
	}
	target, err := h.resolveMemberByName(ctx, memberName)
	if err != nil {
		return true, env, err
	}
	if target.DMOptOut {
		return true, env, errors.New("该成员未开放会话接入：" + memberLabel(target))
	}
	keyID, resolvedSession, noDirectory, err := h.resolveMemberSession(ctx, target, sessionName)
	if err != nil {
		return true, env, err
	}
	if noDirectory {
		return true, env, errors.New("该会话为专职隔离会话不接受任务派发")
	}
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["cross_member"] = target.OwnerKey
	env.Meta["cross_member_name"] = memberLabel(target)
	env.Meta["cross_session_name"] = resolvedSession
	env.Meta["source_session_name"] = sourceSession
	env.Meta["source_key_id"] = sourceKeyID
	env.Meta["reply_prefix"] = fmt.Sprintf("【%s·%s】", memberLabel(target), resolvedSession)
	env.To = sessionAddress(resolvedSession, keyID)
	env.From = sessionAddress(sourceSession, keyID)
	env.Body = body
	return true, env, nil
}

func isCollectEnvelope(env model.Envelope) bool {
	if env.Meta == nil {
		return false
	}
	if typ, ok := env.Meta["type"].(string); ok && typ == "collect" {
		return true
	}
	if v, ok := env.Meta["collect"].(bool); ok && v {
		return true
	}
	return false
}

func (h *Hub) storeCollectEnvelope(ctx context.Context, from address, env model.Envelope) error {
	if h.Repo == nil {
		return nil
	}
	if optOut, err := h.collectOptOut(ctx, from.KeyID); err != nil {
		return err
	} else if optOut {
		return nil
	}
	collectedAt := time.Now().UTC()
	if env.TS > 0 {
		collectedAt = time.Unix(env.TS, 0).UTC()
	}
	content, _ := json.Marshal(map[string]any{
		"text":         env.Body,
		"role":         firstNonEmpty(metaString(env.Meta, "role"), "unknown"),
		"session":      firstNonEmpty(metaString(env.Meta, "session"), from.SessionName),
		"key_id":       from.KeyID,
		"collect":      true,
		"collected_at": collectedAt.Format(time.RFC3339),
	})
	return h.Repo.EnqueueMessage(ctx, model.Message{
		ID:           env.ID,
		ChatEntityID: sessionAddress(from.SessionName, from.KeyID),
		Direction:    model.DirectionCollect,
		BotChannelID: "sessionhelper",
		FeishuMsgID:  env.ID,
		ChatType:     model.ChatPersonal,
		SenderOpenID: from.SessionName,
		Content:      string(content),
		Status:       "done",
		CreatedAt:    collectedAt,
	})
}

func (h *Hub) collectOptOut(ctx context.Context, keyID string) (bool, error) {
	accounts, err := h.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil {
		return false, err
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return false, err
	}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if accountBelongsToMember(account, member) {
				return member.EvidenceOptOut, nil
			}
		}
	}
	return false, nil
}

func (h *Hub) dispatchSelector(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	sessionName, body, ok := h.parseSelector(msg.BotChannelID, text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if result, handled, err := h.dispatchFeishuSticky(ctx, msg, text, true); handled || err != nil {
		return result, handled, err
	}
	source := sourceAccount(msg)
	keyID, found := h.lookupKeyIDForSession(source, sessionName)
	if user, ok := h.newPlatformFeishuUser(ctx, msg); ok {
		if user.State != identity.UserActive {
			logFeishuDispatchRejected("sender_inactive", msg.SenderOpenID, sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "你的账号已停用，无法投递消息"}, true, nil
		}
		resolved := h.resolveNewPlatformFeishuSession(ctx, user.UserIDLC(), sessionName)
		if resolved.status == ownerSessionAmbiguous {
			return model.PrefixDispatchResult{Matched: true, Reply: ambiguousSessionError(sessionName, resolved.candidates).Error()}, true, nil
		}
		if resolved.status != ownerSessionUniqueHit {
			logFeishuDispatchRejected("target_agent_unavailable", msg.SenderOpenID, sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到你名下在线且可用的 Agent：" + sessionName}, true, nil
		}
		keyID, sessionName, found = resolved.keyID, resolved.sessionName, true
	} else if h.Identity != nil && msg.SenderOpenID != "" {
		logFeishuDispatchRejected("sender_not_active_member", msg.SenderOpenID, sessionName, msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "你的账号未绑定或已停用，无法投递消息"}, true, nil
	}
	if !found {
		resolved := h.resolveOwnerSessionByShort(ctx, h.controlOwnerKey(ctx, msg), sessionName, "")
		if resolved.status == ownerSessionAmbiguous {
			return model.PrefixDispatchResult{Matched: true, Reply: ambiguousSessionError(sessionName, resolved.candidates).Error()}, true, nil
		}
		if resolved.status != ownerSessionUniqueHit {
			logFeishuDispatchRejected("short_name_not_resolvable", feishuOpenIDFromAccount(source), sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "目标会话无法补全为在线 canonical 地址：" + sessionName}, true, nil
		}
		keyID, sessionName, found = resolved.keyID, resolved.sessionName, true
	}
	env := model.Envelope{
		ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
		To:   sessionAddress(sessionName, keyID),
		From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"chat_type":      string(msg.ChatType),
			"raw":            msg.Content,
			"bot_channel_id": msg.BotChannelID,
		},
	}
	h.annotateSourceContext(ctx, env.Meta, msg)
	if msg.ChatType == model.ChatGroup {
		env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
		env.Meta["sender_open_id"] = msg.SenderOpenID
	}
	if err := h.RouteEnvelope(ctx, env); err != nil {
		if errors.Is(err, errGroupAgentNotFound) {
			logFeishuDispatchRejected(groupingRejectionReason(err), feishuOpenIDFromAccount(source), sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: groupAgentNotFoundText}, true, nil
		}
		logFeishuDispatchRejected("route_error", feishuOpenIDFromAccount(source), sessionName, msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true}, true, nil
}

func (h *Hub) dispatchDefaultPersonalSession(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	if result, handled, err := h.dispatchFeishuSticky(ctx, msg, text, false); handled || err != nil {
		return result, handled, err
	}
	if msg.ChatType != model.ChatPersonal {
		return model.PrefixDispatchResult{}, false, nil
	}
	source := sourceAccount(msg)
	if user, ok := h.newPlatformFeishuUser(ctx, msg); ok {
		if user.State != identity.UserActive {
			logFeishuDispatchRejected("sender_inactive", msg.SenderOpenID, "", msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "你的账号已停用，无法投递消息"}, true, nil
		}
		keyID := AgentKeyID(user.UserIDLC())
		sessions := h.newPlatformOnlineSessions(ctx, user.UserIDLC())
		switch len(sessions) {
		case 0:
			return model.PrefixDispatchResult{}, false, nil
		case 1:
			env := model.Envelope{ID: firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)), To: sessionAddress(sessions[0], keyID), From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)), Body: text, TS: time.Now().Unix(), Meta: map[string]any{"chat_type": string(msg.ChatType), "raw": msg.Content, "bot_channel_id": msg.BotChannelID, "reply_prefix": fmt.Sprintf("【%s】", sessions[0])}}
			h.annotateSourceContext(ctx, env.Meta, msg)
			if err := h.RouteEnvelope(ctx, env); err != nil {
				logFeishuDispatchRejected("route_error", msg.SenderOpenID, sessions[0], msg.BotChannelID)
				return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, true, nil
			}
			return model.PrefixDispatchResult{Matched: true}, true, nil
		default:
			return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("你有多个在线会话（%s），请用 #会话名 内容 指定", strings.Join(sessions, "、"))}, true, nil
		}
	} else if h.Identity != nil && msg.SenderOpenID != "" {
		logFeishuDispatchRejected("sender_not_active_member", msg.SenderOpenID, "", msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "你的账号未绑定或已停用，无法投递消息"}, true, nil
	}
	keyID, sessions, err := h.onlineSessionsForAccount(ctx, source)
	if err != nil {
		return model.PrefixDispatchResult{}, false, err
	}
	switch len(sessions) {
	case 0:
		return model.PrefixDispatchResult{}, false, nil
	case 1:
		env := model.Envelope{
			ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
			To:   sessionAddress(sessions[0], keyID),
			From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)),
			Body: text,
			TS:   time.Now().Unix(),
			Meta: map[string]any{
				"chat_type":      string(msg.ChatType),
				"raw":            msg.Content,
				"bot_channel_id": msg.BotChannelID,
				"reply_prefix":   fmt.Sprintf("【%s】", sessions[0]),
			},
		}
		h.annotateSourceContext(ctx, env.Meta, msg)
		if err := h.RouteEnvelope(ctx, env); err != nil {
			logFeishuDispatchRejected("route_error", feishuOpenIDFromAccount(source), sessions[0], msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "该消息投递失败：" + text}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true}, true, nil
	default:
		return model.PrefixDispatchResult{Matched: true, Reply: fmt.Sprintf("你有多个在线会话（%s），请用 #会话名 内容 指定", strings.Join(sessions, "、"))}, true, nil
	}
}

func (h *Hub) dispatchMemberMention(ctx context.Context, msg model.Message, text string) (model.PrefixDispatchResult, bool, error) {
	// 🔴 G2-a:渠道名不是成员名。不拦的话,`@飞书#x` 会走 resolveMemberByName,
	// 而它匹配 OwnerKey 或 DisplayName —— 一旦有人叫「飞书」,渠道语法就被吃掉且无错可看。
	if _, _, _, isChannel := parseChannelMention(h.stripLeadingBotMentions(msg.BotChannelID, text)); isChannel {
		return model.PrefixDispatchResult{}, false, nil
	}
	targetName, sessionName, body, ok := h.parseMemberMention(msg.BotChannelID, text)
	if !ok {
		return model.PrefixDispatchResult{}, false, nil
	}
	if sender, senderOK := h.newPlatformFeishuUser(ctx, msg); senderOK {
		if sender.State != identity.UserActive {
			logFeishuDispatchRejected("sender_inactive", msg.SenderOpenID, sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "你的账号已停用，无法投递消息"}, true, nil
		}
		targetUser, err := h.Identity.Store.GetUserByShortName(ctx, identity.Lower(targetName))
		if err != nil || targetUser.State != identity.UserActive {
			logFeishuDispatchRejected("target_member_unavailable", msg.SenderOpenID, targetName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到可用成员：" + targetName}, true, nil
		}
		resolved := h.resolveNewPlatformFeishuSession(ctx, targetUser.UserIDLC(), sessionName)
		if resolved.status != ownerSessionUniqueHit {
			logFeishuDispatchRejected("target_agent_unavailable", msg.SenderOpenID, sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "未找到该成员的在线 Agent：" + sessionName}, true, nil
		}
		targetAgent, active := h.newPlatformAgentAt(ctx, resolved.keyID, resolved.sessionName)
		if !active || targetAgent.State != identity.AgentActive || !h.newPlatformUsersShareAgentGroup(ctx, sender.UserIDLC(), targetAgent) {
			err := newGroupingRejection("cross_group_denied")
			logFeishuDispatchRejected(groupingRejectionReason(err), msg.SenderOpenID, resolved.sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: groupAgentNotFoundText}, true, nil
		}
		source := sourceAccount(msg)
		env := model.Envelope{ID: firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)), To: sessionAddress(resolved.sessionName, resolved.keyID), From: feishuAddress(feishuOpenIDFromAccount(source), resolved.keyID, h.botName(msg.BotChannelID)), Body: body, TS: time.Now().Unix(), Meta: map[string]any{"chat_type": string(msg.ChatType), "raw": msg.Content, "bot_channel_id": msg.BotChannelID, "cross_member": targetUser.UserIDLC(), "cross_member_name": targetUser.ShortName, "cross_session_name": resolved.sessionName, "reply_prefix": fmt.Sprintf("【%s·%s】", targetUser.ShortName, resolved.sessionName), "source_chat_entity": msg.ChatEntityID, "source_sender_openid": msg.SenderOpenID}}
		h.annotateSourceContext(ctx, env.Meta, msg)
		if err := h.RouteEnvelope(ctx, env); err != nil {
			logFeishuDispatchRejected("route_error", msg.SenderOpenID, resolved.sessionName, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: "该成员会话投递失败：" + err.Error()}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true}, true, nil
	} else if h.Identity != nil && msg.SenderOpenID != "" {
		logFeishuDispatchRejected("sender_not_active_member", msg.SenderOpenID, sessionName, msg.BotChannelID)
		return model.PrefixDispatchResult{Matched: true, Reply: "你的账号未绑定或已停用，无法投递消息"}, true, nil
	}
	target, err := h.resolveMemberByName(ctx, targetName)
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
	}
	if target.DMOptOut {
		return model.PrefixDispatchResult{Matched: true, Reply: "该成员未开放会话接入：" + memberLabel(target)}, true, nil
	}
	keyID, resolvedSession, noDirectory, err := h.resolveMemberSession(ctx, target, sessionName)
	if err != nil {
		return model.PrefixDispatchResult{Matched: true, Reply: err.Error()}, true, nil
	}
	if noDirectory {
		return model.PrefixDispatchResult{Matched: true, Reply: "该会话为专职隔离会话不接受任务派发"}, true, nil
	}
	source := sourceAccount(msg)
	env := model.Envelope{
		ID:   firstNonEmpty(msg.ID, msg.FeishuMsgID, randomHex(16)),
		To:   sessionAddress(resolvedSession, keyID),
		From: feishuAddress(feishuOpenIDFromAccount(source), keyID, h.botName(msg.BotChannelID)),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{
			"chat_type":            string(msg.ChatType),
			"raw":                  msg.Content,
			"bot_channel_id":       msg.BotChannelID,
			"cross_member":         target.OwnerKey,
			"cross_member_name":    memberLabel(target),
			"cross_session_name":   resolvedSession,
			"reply_prefix":         fmt.Sprintf("【%s·%s】", memberLabel(target), resolvedSession),
			"source_chat_entity":   msg.ChatEntityID,
			"source_sender_openid": msg.SenderOpenID,
		},
	}
	h.annotateSourceContext(ctx, env.Meta, msg)
	if msg.ChatType == model.ChatGroup {
		env.Meta["group_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
		env.Meta["sender_open_id"] = msg.SenderOpenID
	}
	if err := h.RouteEnvelope(ctx, env); err != nil {
		if errors.Is(err, errGroupAgentNotFound) {
			logFeishuDispatchRejected(groupingRejectionReason(err), feishuOpenIDFromAccount(source), resolvedSession, msg.BotChannelID)
			return model.PrefixDispatchResult{Matched: true, Reply: groupAgentNotFoundText}, true, nil
		}
		return model.PrefixDispatchResult{Matched: true, Reply: "该成员会话投递失败：" + err.Error()}, true, nil
	}
	return model.PrefixDispatchResult{Matched: true}, true, nil
}

func (h *Hub) routeToSession(ctx context.Context, keyID, sessionName string, env model.Envelope, ackInbound bool) error {
	if handled, err := h.routeExternalSeat(ctx, keyID, sessionName, env); handled {
		return err
	}
	claimed, sig, err := h.claimEnvelope(env)
	if err != nil || !claimed {
		return err
	}
	var c *sessionClient
	var capabilityAdmission *capabilityPendingAdmission
	func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if sessions := h.sessionClients[keyID]; sessions != nil {
			c = sessions[sessionName]
		}
		capabilityAdmission = h.capabilityPendingAdmissions[terminalKey(keyID, sessionName)]
	}()
	if !h.stickyDeliveryAllowed(ctx, c, env) {
		h.unclaimEnvelope(subAgentClaimKey(env), sig)
		return errors.New("R14 target identity unavailable")
	}
	wantReceipt := outboundReceiptRequested(env) || inboxReceiptRequested(env)
	receiptNonce := metaString(env.Meta, outboundReceiptNonceKey)
	reliable := reliablePrimaryEnvelope(env)
	deliveryACK := c != nil && h.sessionSupportsDeliveryACK(c)
	endpointKnown := c != nil
	if reliable && !deliveryACK && h.Repo != nil {
		var err error
		deliveryACK, endpointKnown, err = h.Repo.SessionEndpointDeliveryACK(ctx, keyID, sessionName)
		if err != nil {
			h.unclaimEnvelope(subAgentClaimKey(env), sig)
			return err
		}
	}
	capabilityPending := c == nil && reliable && !deliveryACK && endpointKnown && capabilityAdmission != nil && h.freshTargetDeliveryRepo() != nil
	if capabilityPending {
		if h.freshTargetBeforePendingInsertHook != nil {
			h.freshTargetBeforePendingInsertHook(keyID, sessionName)
		}
		// Serialize the delayed INSERT with the first capability resolution. If
		// resolution won first, use its settled capability instead of creating a
		// stranded capability_pending row after it has drained.
		capabilityAdmission.mu.Lock()
		if capabilityAdmission.resolved {
			deliveryACK = capabilityAdmission.deliveryACK
			capabilityPending = false
			capabilityAdmission.mu.Unlock()
			if deliveryACK {
				h.mu.Lock()
				if sessions := h.sessionClients[keyID]; sessions != nil {
					c = sessions[sessionName]
				}
				h.mu.Unlock()
			}
		} else {
			defer capabilityAdmission.mu.Unlock()
		}
	}
	if outboundReceiptRequested(env) && !(reliable && (deliveryACK || capabilityPending) && h.Repo != nil) {
		h.unclaimEnvelope(subAgentClaimKey(env), sig)
		return errors.New("confirmed outbound requires a durable target")
	}
	if c == nil && !(reliable && (deliveryACK || capabilityPending) && endpointKnown && h.Repo != nil) {
		log.Printf("session route decision=offline_rejected key_id=%s session=%s client_present=false reliable=%t endpoint_known=%t delivery_ack_v1=%t reason=session_offline",
			keyID, sessionName, reliable, endpointKnown, deliveryACK)
		if source, sourceErr := parseAddress(env.From); sourceErr == nil {
			openID, botChannel := feishuDispatchLogSource(source)
			logFeishuDispatchRejected("session_offline", openID, sessionName, botChannel)
		}
		h.unclaimEnvelope(subAgentClaimKey(env), sig)
		return fmt.Errorf("session %s offline", sessionName)
	}
	if reliable && (deliveryACK || capabilityPending) && h.Repo != nil {
		if env.Meta == nil {
			env.Meta = map[string]any{}
		}
		// 🔴 T-074 根因修复:投递号必须**一次生成、全链同形**。
		// 此前 Hub 发 `dlv_X`,helper 的 unified_queue._delivery_id 收到后统一加前缀成
		// `dw-dlv_X` 作内部队列键,于是渲染器同时见到两种写法 ⇒ 同一条输入画两张卡。
		// helper 的加前缀是**幂等**的(`raw if raw.startswith("dw-") else "dw-"+raw`),
		// ⇒ Hub 直接生成已带前缀的形式,helper 原样透传,两侧字符串逐字相同,helper 无需改动。
		// ack 链不受影响:helper 回 ack 时用的是**从本信封 meta 原样取出**的 delivery_id
		// (sessionhelper/app.py:532),Hub 侧 GetSessionDelivery 为精确匹配、全树不剥前缀。
		// 存量行仍为 `dlv_` 开头,精确匹配照常命中,不需要迁移。
		deliveryID := deliveryIDPrefix + randomHex(16)
		// A crash after enqueue but before marking the blocker outbox must reuse
		// the durable ID, even after the in-memory envelope claim map is lost.
		if coordinateReviewEnvelope(env) {
			sum := sha256.Sum256([]byte("coordinate-review-v1\x00" + keyID + "\x00" + sessionName + "\x00" + env.ID))
			deliveryID = deliveryIDPrefix + hex.EncodeToString(sum[:])
		}
		if metaString(env.Meta, "kind") == "task_blocker_ready" {
			sum := sha256.Sum256([]byte("blocker-wake-v1\x00" + keyID + "\x00" + sessionName + "\x00" + env.ID))
			deliveryID = deliveryIDPrefix + hex.EncodeToString(sum[:])
		}
		if retryOf := metaString(env.Meta, "r52_retry_of"); retryOf != "" {
			deliveryID = r52RetryDeliveryID(retryOf)
		}
		route, _ := ctx.Value(subAgentDeliveryKey{}).(*subagent.DeliveryRoute)
		if route != nil {
			deliveryID = subAgentRouteDeliveryID(env.ID+"\x00"+keyID+"\x00"+sessionName, *route)
		}
		env.Meta["type"] = "delivery"
		env.Meta["delivery_id"] = deliveryID
		env.Meta["delivery_version"] = 1
		env.Meta["ack_required"] = true
		env.Meta["source_message_id"] = env.ID
		if wantReceipt {
			// Receipt control flag belongs to the sender/Hub only.
			cleanMeta := make(map[string]any, len(env.Meta))
			for k, v := range env.Meta {
				if k != outboundReceiptRequestKey && k != outboundReceiptNonceKey && k != inboxReceiptRequestKey {
					cleanMeta[k] = v
				}
			}
			env.Meta = cleanMeta
		}
		payload, err := json.Marshal(sessionClientEnvelope(env))
		if err != nil {
			h.unclaimEnvelope(subAgentClaimKey(env), sig)
			return err
		}
		delivery := model.SessionDelivery{SubRoute: route, ID: deliveryID, KeyID: keyID, SessionName: sessionName, Envelope: string(payload), SourceMessageID: env.ID, SourceType: deliverySourceType(env), Provenance: provenanceJSON(env)}
		if capabilityPending {
			delivery.State = capabilityPendingState
			delivery.Provenance = capabilityPendingProvenance
		}
		if err := h.Repo.EnqueueSessionDelivery(ctx, delivery); err != nil {
			h.unclaimEnvelope(subAgentClaimKey(env), sig)
			if route != nil {
				return subAgentStorageError(err)
			}
			return err
		}
		if wantReceipt {
			h.writeQueuedReceiptToSource(ctx, env, receiptNonce, deliveryID)
		}
		h.publishInputObservation(keyID, sessionName, env, deliveryID, "hub_received", "primary", true)
		h.publishSourceCopyObservation(env, deliveryID, "hub_received")
		connSeq := 0
		if c != nil {
			connSeq = c.connSeq
		}
		decision := "durable_queued"
		if capabilityPending {
			decision = "capability_pending_queued"
		}
		log.Printf("session route decision=%s key_id=%s session=%s client_present=%t conn_seq=%d delivery_id=%s source_message_id=%s",
			decision, keyID, sessionName, c != nil, connSeq, deliveryID, env.ID)
		if c != nil {
			// PM assist dispatch is invoked inside withSpecAgent's current-owner
			// authorization transaction. Revalidation in the drain must not be
			// re-entered synchronously from that callback (the in-memory test store
			// serializes the authorization lock); the durable queue is safe to wake
			// just after the dispatch transaction returns.
			if specReviewEnvelopeType(env) == "console_spec_pm_assist_request" || coordinateReviewEnvelope(env) {
				h.scheduleSessionDeliveryDrain(c, 10*time.Millisecond)
			} else {
				h.drainSessionDeliveries(ctx, c)
			}
		}
		return nil
	}
	if ctx.Value(subAgentDeliveryKey{}) != nil {
		h.unclaimEnvelope(subAgentClaimKey(env), sig)
		return subagent.Fail("unsupported", "durable helper required")
	}
	if inboxReceiptRequested(env) {
		cleanMeta := make(map[string]any, len(env.Meta))
		for k, v := range env.Meta {
			if k != inboxReceiptRequestKey && k != outboundReceiptNonceKey {
				cleanMeta[k] = v
			}
		}
		env.Meta = cleanMeta
	}
	if err := c.write(ctx, sessionClientEnvelope(env)); err != nil {
		log.Printf("session route decision=direct_write_failed key_id=%s session=%s client_present=true conn_seq=%d source_message_id=%s reason=%q",
			keyID, sessionName, c.connSeq, env.ID, err.Error())
		h.unclaimEnvelope(subAgentClaimKey(env), sig)
		return err
	}
	if reliablePrimaryEnvelope(env) {
		directID := "direct_" + env.ID
		// Legacy helpers provide no injection ACK. A successful WebSocket write only
		// proves handoff, so use the weaker pty_write/injection-unconfirmed stage.
		h.publishInputObservation(keyID, sessionName, env, directID, "pty_write", "primary", false)
		h.publishSourceCopyObservation(env, directID, "pty_write")
	}
	if ackInbound && h.Repo != nil && env.ID != "" {
		_ = h.Repo.AckMessage(ctx, env.ID)
	}
	return nil
}

// reliablePrimaryEnvelope 判断信封是否走可靠投递(持久队列 + ACK + 离线补投 + 留日志)。
//
// 🔴 业主 2026-09-10 裁定:@ 提及要「尽可能可靠投递,只要客户端 inbox 收到就是送达」。
// 原实现把 no_mirror 也当作不可靠的理由,而 no_mirror 的语义只是「不要镜像到外部面」
// (本文件 shouldSkipMirror 是它唯一的消费者),与「要不要可靠送达」无关。
// 这个耦合的后果:AMR 的 @ 提及既不进持久队列、也不要求 ACK、直写成功还完全不打日志,
// 对方离线就无痕丢弃,而 Hub 仍把它计入 targets_dispatched —— 全链路查无可查(T-045/T-044)。
//
// 故解耦:不镜像 ≠ 不可靠。只有 system(控制面)才排除在可靠投递之外。
// 同时把 agent_route 纳入可靠类型 —— 它承载 Agent 间协作指令,与人发的消息同级。
// 注:走可靠路径后 Hub 会把 Meta["type"] 覆写为 "delivery" 并补 delivery_id/ack_required,
// 即 @ 提及在收件侧与普通消息完全同形;no_mirror 仍保留在 Meta 里,
// shouldSkipMirror 照旧生效,**不会**因此被镜像到飞书。
func reliablePrimaryEnvelope(env model.Envelope) bool {
	if env.ID != "" && (specReviewType(specReviewEnvelopeType(env)) || coordinateReviewEnvelope(env)) && metaBool(env.Meta, "system") {
		return true
	}
	if env.ID == "" || metaBool(env.Meta, "system") {
		return false
	}
	switch metaString(env.Meta, "type") {
	case "", "agent_route":
		return true
	}
	return false
}

func deliverySourceType(env model.Envelope) string {
	from, err := parseAddress(env.From)
	if err == nil {
		if from.Kind == addressFeishu {
			return "feishu_inbound"
		}
		if from.Kind == addressSession && from.SessionName == "workpulse" {
			return "view_inbound"
		}
	}
	return "session_inbound"
}

func (h *Hub) publishSourceCopyObservation(env model.Envelope, deliveryID, stage string) {
	from, err := parseAddress(env.From)
	if err != nil || from.Kind != addressSession || from.SessionName == "" || from.SessionName == "workpulse" {
		return
	}
	h.publishInputObservation(from.KeyID, from.SessionName, env, deliveryID, stage, "copy", false)
}

func (h *Hub) sessionSupportsDeliveryACK(c *sessionClient) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.deliveryACKV1
}

func (h *Hub) handleSessionCapability(c *sessionClient, env model.Envelope) bool {
	if metaString(env.Meta, "type") != "session_capabilities" {
		return false
	}
	c.stateMu.Lock()
	wasDeliveryACKV1 := c.deliveryACKV1
	c.helperReady = true
	c.deliveryACKV1 = metaBool(env.Meta, "delivery_ack_v1")
	c.taskFlowV1 = metaBool(env.Meta, "task_flow_v1")
	c.blockerNoticeV1 = metaBool(env.Meta, "blocker_notice_v1")
	c.blockerWakeV1 = metaBool(env.Meta, "blocker_wake_v1")
	c.specReviewV1 = metaBool(env.Meta, "spec_review_v1")
	c.coordinateReviewV1 = metaBool(env.Meta, "coordinate_review_v1")
	c.pmSpecFlowV1 = metaBool(env.Meta, "pm_spec_flow_v1")
	c.helperUpdateV1 = metaBool(env.Meta, "helper_update_v1")
	c.desktopInboxV1 = metaBool(env.Meta, "desktop_inbox_v1") && metaString(env.Meta, "processed_semantics") == "saved_to_desktop_inbox"
	c.deliveryACKV2 = metaBool(env.Meta, "delivery_ack_v2") && metaString(env.Meta, "processing_mode") == "driver" && !c.desktopInboxV1
	c.deliveryACKV1 = c.deliveryACKV1 || c.deliveryACKV2
	c.stateMu.Unlock()
	h.writeSubAgentCapability(c, env)
	h.writeOutboundCapabilities(c, env)
	if metaBool(env.Meta, "file_upload_v1") {
		// 只作统计（组长裁定 C）：上传说明由 helper 自行附加，Hub 不据此下发任何内容。
		log.Printf("session capability file_upload_v1=true key_id=%s session=%s conn_seq=%d", c.keyID, c.sessionName, c.connSeq)
	}
	if h.Repo != nil {
		if err := h.Repo.SetSessionEndpointDeliveryACK(context.Background(), c.keyID, c.sessionName, c.deliveryACKV1); err != nil {
			log.Printf("session capability persist failed key_id=%s session=%s conn_seq=%d delivery_ack_v1=%t err=%v",
				c.keyID, c.sessionName, c.connSeq, c.deliveryACKV1, err)
		}
	}
	if h.freshTargetDeliveryRepo() != nil {
		dead, err := h.resolveCapabilityPendingAdmission(context.Background(), c.keyID, c.sessionName, c.deliveryACKV1, h.now().UTC())
		if err != nil {
			log.Printf("capability pending resolution failed key_id=%s session=%s conn_seq=%d delivery_ack_v1=%t err=%v", c.keyID, c.sessionName, c.connSeq, c.deliveryACKV1, err)
		} else {
			for _, d := range dead {
				h.finishCapabilityPendingDelivery(context.Background(), d, pendingCapabilityFailureReason(d))
			}
		}
	}
	if h.sessionSupportsDeliveryACK(c) {
		if !wasDeliveryACKV1 {
			h.releaseSessionDeliveryLeases(c, "session_reconnected")
		}
		h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
	}
	if h.sessionSupportsHelperUpdate(c) {
		h.backgroundWork("owned", func() { h.resumeHelperUpdateOffer(context.Background(), c) })
	}
	h.backgroundWork("owned", func() { h.dispatchTaskNotifications(context.Background()) })
	return true
}

func (h *Hub) sessionSupportsHelperUpdate(c *sessionClient) bool {
	if c == nil {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.helperUpdateV1
}

func sessionHelperReady(c *sessionClient) bool {
	if c == nil {
		return false
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	return c.helperReady
}

func sessionEnvelopeRequiresHelperReady(env model.Envelope) bool {
	typeName := strings.ToLower(strings.TrimSpace(metaString(env.Meta, "type")))
	return typeName == "" || typeName == "content" || typeName == "agent_route"
}

func agentHandshakeEnforced() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WP_AGENT_HANDSHAKE_ENFORCE"))) {
	case "1", "true", "yes", "on", "enforce":
		return true
	default:
		return false
	}
}

func (h *Hub) releaseSessionDeliveryLeases(c *sessionClient, reason string) {
	if h.Repo == nil || c == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	released, err := h.Repo.ReleaseSessionDeliveryLeases(
		ctx, c.keyID, c.sessionName, reason, time.Now().UTC(),
	)
	if err != nil {
		log.Printf("session delivery lease release failed key_id=%s session=%s conn_seq=%d reason=%s error=%q",
			c.keyID, c.sessionName, c.connSeq, reason, err.Error())
		return
	}
	log.Printf("session delivery lease release decision=requeue key_id=%s session=%s conn_seq=%d reason=%s released=%d",
		c.keyID, c.sessionName, c.connSeq, reason, released)
}

func (h *Hub) handleSessionDeliveryAck(ctx context.Context, c *sessionClient, env model.Envelope) bool {
	ackType := metaString(env.Meta, "type")
	if (ackType != "delivery_ack" && ackType != "delivery_cancel_ack") || h.Repo == nil {
		return false
	}
	id := metaString(env.Meta, "delivery_id")
	if !h.subAgentACKAllowed(ctx, c, env, id) {
		return true
	}
	if ackType == "delivery_cancel_ack" {
		if id == "" {
			return true
		}
		d, err := h.Repo.GetSessionDelivery(ctx, id)
		if err != nil || d == nil || d.KeyID != c.keyID || d.SessionName != c.sessionName || d.State == "dead" || d.State == "cancelled" {
			return true
		}
		if d.AckProtocol == 2 {
			protocol, mode := deliveryACKConnectionProtocol(c)
			h.mu.Lock()
			current := h.sessionClients[c.keyID][c.sessionName]
			h.mu.Unlock()
			if protocol != 2 || mode != d.ProcessingMode || (c.conn != nil && current != c) || metaInt(env.Meta, "ack_protocol") != 2 || metaString(env.Meta, "processing_mode") != d.ProcessingMode || metaInt(env.Meta, "ack_generation") != d.AckGeneration {
				return true
			}
		}
		messageID := metaString(env.Meta, "message_id")
		if metaString(env.Meta, "result") == "cancelled" && messageID != "" && messageID == d.SourceMessageID {
			if cancelled, err := h.Repo.ConfirmSessionDeliveryCancelled(ctx, messageID, id, c.keyID, c.sessionName); err == nil && cancelled {
				h.publishDeliveryCancelled(c.keyID, c.sessionName, messageID, id, "helper_received")
				h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
			}
		} else if metaString(env.Meta, "result") == "not_cancelable" && messageID == d.SourceMessageID {
			// The helper has already taken/started this input; keep the delivery and
			// tell View why cancellation did not remove it.
			h.publishDeliveryUpdateForDelivery(*d, d.State, "helper_received", "", "cancel_not_cancelable")
		}
		return true
	}
	state := metaString(env.Meta, "state")
	if id == "" || (state != "delivered" && state != "queued" && state != "admission_unknown" && state != "injected" && state != "processed" && state != "failed" && state != "busy") {
		return true
	}
	d, err := h.Repo.GetSessionDelivery(ctx, id)
	if err != nil || d == nil {
		return true
	}
	if d.KeyID != c.keyID || d.SessionName != c.sessionName {
		return true
	}
	if d.AckProtocol == 2 {
		return h.handleDeliveryACKV2(ctx, c, env, d)
	}
	if metaInt(env.Meta, "ack_protocol") == 2 || state == "queued" || state == "admission_unknown" || d.State == "dead" || d.State == "cancelled" {
		return true
	}
	// Historical terminal receipts are transport-only and cannot be regressed by
	// a delayed delivered/busy/failed frame from the same legacy helper.
	if d.State == "legacy_received" || d.State == "processed" {
		return true
	}
	c.stateMu.Lock()
	desktopInbox := c.desktopInboxV1
	c.stateMu.Unlock()
	// Durable desktop receipt completes transport only, not model admission.
	if state == "injected" && desktopInbox {
		return true
	}
	if !desktopInbox && (state == "injected" || state == "processed") {
		if err := h.Repo.UpdateSessionDelivery(ctx, id, "legacy_received", "", h.now().UTC()); err != nil {
			return true
		}
		h.publishDeliveryUpdateForDelivery(*d, "legacy_received", "helper_received", "", "legacy_injection_unknown")
		if d.SourceMessageID != "" {
			_ = h.Repo.AckMessage(ctx, d.SourceMessageID)
		}
		h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
		return true
	}
	if state == "processed" {
		_ = h.Repo.UpdateSessionDelivery(ctx, id, "processed", "", time.Now().UTC())
		if d.State != "injected" {
			if desktopInbox {
				h.publishDeliveryUpdateForDelivery(*d, "processed", "helper_received", "", "desktop_inbox_saved")
			} else {
				h.publishDeliveryUpdateForDelivery(*d, "processed", "cli_injected", "", "")
			}
		}
		if d.SourceMessageID != "" {
			_ = h.Repo.AckMessage(ctx, d.SourceMessageID)
		}
		h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
	} else if state == "busy" {
		_ = h.Repo.UpdateSessionDeliveryBusy(ctx, id, metaString(env.Meta, "reason"), time.Now().UTC().Add(15*time.Second))
		h.publishDeliveryUpdateForDelivery(*d, "queued", "helper_received", "", metaString(env.Meta, "reason"))
		h.scheduleSessionDeliveryDrain(c, 15*time.Second)
	} else if state == "failed" {
		retryable := metaBool(env.Meta, "retryable")
		if retryable {
			// 可重试的失败：指数退避（2 秒起翻倍、封顶 300 秒），连续 30 次转 dead 并通知发送方（组长取甲；原为恒 2 秒、无上限）。
			reason := metaString(env.Meta, "reason")
			now := time.Now().UTC()
			base, maxDelay, maxFailed := h.deliveryFailedRetryParams()
			dead, failed, next, err := h.Repo.FailSessionDeliveryRetryable(ctx, id, reason, now, base, maxDelay, maxFailed)
			if err != nil {
				log.Printf("session delivery failed-retry record error delivery_id=%s err=%v", id, err)
				return true
			}
			if dead {
				log.Printf("session delivery dead reason=retry_exhausted delivery_id=%s failed=%d last_reason=%s", id, failed, reason)
				h.publishDeliveryUpdateForDelivery(*d, "dead", "helper_received", "failed", reason)
				h.notifyDeliveryRetryExhausted(ctx, *d, failed, reason)
				h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
			} else {
				h.publishDeliveryUpdateForDelivery(*d, "queued", "helper_received", "", reason)
				h.scheduleSessionDeliveryDrain(c, next.Sub(now))
			}
		} else {
			_ = h.Repo.UpdateSessionDelivery(ctx, id, "dead", metaString(env.Meta, "reason"), time.Now().UTC())
			h.publishDeliveryUpdateForDelivery(*d, "dead", "helper_received", "failed", metaString(env.Meta, "reason"))
			h.backgroundWork("owned", func() { h.drainSessionDeliveries(context.Background(), c) })
		}
	} else if d.State != "processed" {
		_ = h.Repo.UpdateSessionDelivery(ctx, id, "delivered", "", time.Now().UTC().Add(2*time.Minute))
		h.publishDeliveryUpdateForDelivery(*d, "delivered", "helper_received", "", "")
		h.scheduleSessionDeliveryDrain(c, 2*time.Minute)
		if position := metaInt(env.Meta, "queue_position"); position > 0 {
			h.notifyDeliveryQueued(ctx, *d, position)
		}
	}
	return true
}

func (h *Hub) publishDeliveryUpdateForDelivery(d model.SessionDelivery, deliveryState, stage, status, reason string) {
	if deliveryState == "legacy_received" {
		// An old native CLI can reset its View stream after the original input
		// observation. Re-anchor this receipt to its persisted original input;
		// no model admission/terminal is inferred or sent to the driver.
		var original model.Envelope
		if json.Unmarshal([]byte(d.Envelope), &original) == nil {
			h.publishInputObservation(d.KeyID, d.SessionName, original, d.ID, "helper_received", "primary", false)
		}
	}
	metadata := map[string]any{"ack_protocol": d.AckProtocol, "processing_mode": d.ProcessingMode, "ack_generation": d.AckGeneration}
	if d.DeliveredAt != nil {
		metadata["queued_at"] = d.DeliveredAt.UTC().Format(time.RFC3339Nano)
	}
	if d.InjectedAt != nil {
		metadata["occurred_at"] = d.InjectedAt.UTC().Format(time.RFC3339Nano)
		metadata["turn_id"] = d.TurnID
	}
	if d.AckProtocol == 2 && deliveryState == "processed" && d.ProcessedAt != nil {
		metadata["occurred_at"] = d.ProcessedAt.UTC().Format(time.RFC3339Nano)
		metadata["turn_outcome"] = d.TurnOutcome
	}
	targetKey, targetSession := h.subViewObservation(d.KeyID, d.SessionName, d.ID, false)
	h.publishDeliveryUpdateWithMetadata(targetKey, targetSession, d.SourceMessageID, d.ID, deliveryState, stage, status, reason, metadata)
	if deliveryState == "dead" {
		h.mentionMirror.deliveryDead(d, reason) // R-V2-0103：view 提及转来的那封终未处理 ⇒ 发起页一行状态
	}
	var original model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &original) != nil {
		return
	}
	from, err := parseAddress(original.From)
	if err == nil && from.Kind == addressSession && from.SessionName != "" && from.SessionName != "workpulse" {
		sourceKey, sourceSession := h.subViewObservation(from.KeyID, from.SessionName, d.ID, true)
		h.publishDeliveryUpdateWithMetadata(sourceKey, sourceSession, d.SourceMessageID, d.ID, deliveryState, stage, status, reason, metadata)
	}
}

func (h *Hub) drainSessionDeliveries(ctx context.Context, c *sessionClient) {
	if h.Repo == nil || !h.sessionSupportsDeliveryACK(c) {
		return
	}
	h.mu.Lock()
	var replacement *sessionClient
	if sessions := h.sessionClients[c.keyID]; sessions != nil {
		replacement = sessions[c.sessionName]
	}
	h.mu.Unlock()
	// A real WebSocket client must still be the published client. Tests may use
	// an in-memory client without a connection to exercise the drain directly.
	if replacement != c && c.conn != nil {
		log.Printf("session delivery drain skipped decision=stale_or_offline_client key_id=%s session=%s conn_seq=%d client_present=%t",
			c.keyID, c.sessionName, c.connSeq, replacement != nil)
		return
	}
	for {
		expired, err := h.Repo.ExpireSessionDeliveries(ctx, c.keyID, c.sessionName, time.Now().UTC())
		if err != nil {
			return
		}
		for _, d := range expired {
			h.publishDeliveryUpdateForDelivery(d, "dead", "helper_received", "failed", "delivery_expired")
			h.drainDurableFailureNotice(ctx, d)
		}
		d, err := h.Repo.ClaimSessionDelivery(ctx, c.keyID, c.sessionName, time.Now().UTC())
		if err != nil || d == nil {
			return
		}
		var env model.Envelope
		if err := json.Unmarshal([]byte(d.Envelope), &env); err != nil {
			if updateErr := h.Repo.UpdateSessionDelivery(ctx, d.ID, "dead", "malformed_envelope", time.Now().UTC()); updateErr != nil {
				return
			}
			h.publishDeliveryUpdateForDelivery(*d, "dead", "helper_received", "failed", "malformed_envelope")
			continue
		}
		if scopeErr := h.subAgentDrainCheck(ctx, c, d); scopeErr != nil {
			state := "queued"
			if subAgentPermanent(scopeErr) {
				state = "dead"
			}
			_ = h.Repo.UpdateSessionDelivery(ctx, d.ID, state, subAgentErrorCode(scopeErr), time.Now().UTC().Add(5*time.Second))
			if state == "dead" {
				h.drainDurableFailureNotice(ctx, *d)
				continue
			}
			return
		}
		if !h.stickyDeliveryAllowed(ctx, c, env) {
			if h.Repo.UpdateSessionDelivery(ctx, d.ID, "dead", "r14_target_unavailable", time.Now().UTC()) != nil {
				return
			}
			h.publishDeliveryUpdateForDelivery(*d, "dead", "helper_received", "failed", "r14_target_unavailable")
			h.drainDurableFailureNotice(ctx, *d)
			continue
		}
		if legacySpecGenerationEnvelope(env) {
			guarded, release, freezeErr := h.beginLegacySpecWrite(ctx)
			if freezeErr != nil {
				if errors.Is(freezeErr, store.ErrLegacySpecFrozen) {
					if h.Repo.UpdateSessionDelivery(ctx, d.ID, "dead", "legacy_spec_frozen", time.Now().UTC()) != nil {
						return
					}
					continue
				}
				_ = h.Repo.UpdateSessionDelivery(ctx, d.ID, "queued", "legacy_spec_freeze_unavailable", time.Now().UTC().Add(5*time.Second))
				h.scheduleSessionDeliveryDrain(c, 5*time.Second)
				return
			}
			// A drain returns after its first successful send. Keep the lease through
			// the actual socket write; invalid queued generations may be skipped first.
			defer release()
			ctx = guarded
		}

		if coordinateReviewEnvelope(env) && !h.coordinateReviewDeliveryAllowed(ctx, c, env) {
			if h.Repo.UpdateSessionDelivery(ctx, d.ID, "dead", "coordinate_review_unavailable_or_revoked", time.Now().UTC()) != nil {
				return
			}
			continue
		}
		if specReviewType(specReviewEnvelopeType(env)) {
			reviewType := specReviewEnvelopeType(env)
			c.stateMu.Lock()
			capable := c.specReviewV1 && c.deliveryACKV1
			if reviewType == "console_spec_pm_assist_request" {
				capable = capable && c.pmSpecFlowV1
			}
			c.stateMu.Unlock()
			valid := false
			if capable {
				if reviewType == "console_spec_pm_assist_request" {
					if h.pmSpecFlowEnabled {
						if pm, ok := h.pmSpecFlowStore(); ok {
							q, e := pm.PMSpecAssistRequest(ctx, metaString(env.Meta, "request_id"))
							if e == nil && q != nil && q.Status == "pending" && q.Deadline > h.now().Unix() && q.HubBoot == h.specAgentBoot() {
								valid = h.pmSpecAssistDeliveryAuthorized(ctx, q, c)
							}
						}
					}
				} else if sr, ok := h.Repo.(specAgentRepository); ok {
					q, e := sr.SpecAgentRequest(ctx, metaString(env.Meta, "request_id"))
					valid = e == nil && q.Status == "pending" && q.Deadline > h.now().Unix() && q.Boot == h.specAgentBoot()
				}
			}
			if !valid {
				if h.Repo.UpdateSessionDelivery(ctx, d.ID, "dead", "spec_review_expired_or_unsupported", time.Now().UTC()) != nil {
					return
				}
				continue
			}
		}
		protocol, mode := deliveryACKConnectionProtocol(c)
		// Receipt-only task notices have no driver turn. Keep their transport
		// semantics separate even on a driver connection capable of ACK v2.
		if protocol == 2 && (metaString(env.Meta, "kind") == "task_assigned" || (metaString(env.Meta, "kind") == "task_blocker_ready" && !metaBool(env.Meta, "automatic_recheck"))) {
			protocol, mode = 1, "legacy"
		}
		d, err = h.Repo.BindSessionDeliveryACK(ctx, d.ID, c.keyID, c.sessionName, protocol, mode)
		if err != nil || d == nil || (d.AckProtocol == 2 && protocol != 2) {
			// Never downgrade an already-dispatched v2 item to an older helper.
			return
		}
		if env.Meta == nil {
			env.Meta = make(map[string]any)
		}
		env.Meta["delivery_ack_protocol"] = d.AckProtocol
		env.Meta["processing_mode"] = d.ProcessingMode
		env.Meta["ack_generation"] = d.AckGeneration
		if err := h.writeSubAgentDelivery(ctx, c, d, env); err != nil {
			if subAgentPermanent(err) {
				if h.Repo.UpdateSessionDelivery(ctx, d.ID, "dead", subAgentErrorCode(err), time.Now().UTC()) != nil {
					return
				}
				h.drainDurableFailureNotice(ctx, *d)
				continue
			}
			_ = h.Repo.UpdateSessionDelivery(ctx, d.ID, "queued", err.Error(), time.Now().UTC().Add(5*time.Second))
			log.Printf("session delivery write failed decision=requeue key_id=%s session=%s conn_seq=%d delivery_id=%s reason=%q retry_after=5s",
				c.keyID, c.sessionName, c.connSeq, d.ID, err.Error())
			return
		}
		// Once the socket write succeeds, the helper may have taken this delivery;
		// tell View to remove the cancel affordance before processing later ACKs.
		h.publishDeliveryUpdateForDelivery(*d, d.State, "hub_received", "", "")
		delay := time.Until(d.NextAttemptAt)
		if delay <= 0 {
			delay = time.Second
		}
		h.scheduleSessionDeliveryDrain(c, delay)
		return
	}
}

// Revalidate the persisted Agent owner and active state at actual delivery time.
// A queued PM envelope contains spec content, so authorization at enqueue time
// is insufficient if the Agent is suspended or reassigned while offline.
func (h *Hub) pmSpecAssistDeliveryAuthorized(ctx context.Context, q *store.PMSpecAssistRequest, c *sessionClient) bool {
	if q == nil || c == nil || !h.pmSpecFlowEnabled {
		return false
	}
	specReq := store.SpecAgentRequest{Actor: q.ActorID, AgentID: q.AgentID, KeyID: c.keyID, SessionName: c.sessionName}
	err := h.withSpecAgent(ctx, &specReq, func(a model.ConsoleAgent, check func() error) error {
		if a.ID != q.AgentID || a.KeyID != c.keyID || a.SessionName != c.sessionName {
			return identity.ErrForbidden
		}
		return check()
	})
	return err == nil
}

func (h *Hub) scheduleSessionDeliveryDrain(c *sessionClient, delay time.Duration) {
	if delay < 10*time.Millisecond {
		delay = 10 * time.Millisecond
	}
	time.AfterFunc(delay, func() {
		h.backgroundWork("delivery_timer", func() { h.drainSessionDeliveries(context.Background(), c) })
	})
}

// 可重试 failed 的退避与上限（组长取甲）。实例字段为 0 ⇒ 默认：2 秒起翻倍、封顶 300 秒、连续 30 次转 dead。
const (
	defaultDeliveryFailedBackoffBase = 2 * time.Second
	defaultDeliveryFailedBackoffCap  = 300 * time.Second
	defaultDeliveryFailedMax         = 30
	deliveryRetryExhaustedCode       = "retry_exhausted"
)

func (h *Hub) deliveryFailedRetryParams() (time.Duration, time.Duration, int) {
	base, maxDelay, maxFailed := h.deliveryFailedBackoffBase, h.deliveryFailedBackoffCap, h.deliveryFailedMax
	if base <= 0 {
		base = defaultDeliveryFailedBackoffBase
	}
	if maxDelay <= 0 {
		maxDelay = defaultDeliveryFailedBackoffCap
	}
	if maxFailed <= 0 {
		maxFailed = defaultDeliveryFailedMax
	}
	return base, maxDelay, maxFailed
}

// notifyDeliveryRetryExhausted 连续失败到限转 dead 时给发送方一条可见的投递失败通知：原因码＋最后一次失败原因。
func (h *Hub) notifyDeliveryRetryExhausted(ctx context.Context, d model.SessionDelivery, failed int, lastReason string) {
	if d.SourceType == "failure_notice" || d.Provenance == model.FailureNoticeProvenance {
		return
	}
	if h.drainDurableFailureNotice(ctx, d) {
		return
	}
	var original model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &original) != nil || original.From == "" {
		return
	}
	notice := model.Envelope{ID: "delivery_failed_" + randomHex(8), From: sessionAddress(d.SessionName, d.KeyID), To: original.From,
		Body: fmt.Sprintf("消息投递失败，已连续重试 %d 次仍未成功，不再重投（原因码 %s；最后一次失败原因：%s）", failed, deliveryRetryExhaustedCode, lastReason),
		Meta: map[string]any{"type": "delivery_status", "no_mirror": true, "system": true,
			"delivery_id": d.ID, "reason_code": deliveryRetryExhaustedCode, "last_error": lastReason}}
	if err := h.RouteEnvelope(ctx, notice); err != nil {
		log.Printf("session delivery dead notice route failed delivery_id=%s err=%v", d.ID, err)
	}
}

func (h *Hub) notifyDeliveryQueued(ctx context.Context, d model.SessionDelivery, position int) {
	var original model.Envelope
	if json.Unmarshal([]byte(d.Envelope), &original) != nil || original.From == "" {
		return
	}
	status := model.Envelope{ID: "delivery_status_" + randomHex(8), From: sessionAddress(d.SessionName, d.KeyID), To: original.From,
		Body: fmt.Sprintf("消息已接收，前面还有 %d 条待处理", position), Meta: map[string]any{"type": "delivery_status", "no_mirror": true, "system": true}}
	_ = h.RouteEnvelope(ctx, status)
}

func (h *Hub) routeToFeishu(ctx context.Context, to address, env model.Envelope) error {
	if shouldSkipMirror(env) {
		log.Printf("[feishu] skip_mirror env_id=%s to=%s type=%q", env.ID, env.To, metaString(env.Meta, "type"))
		// An explicit reply-to-origin must never be reported as routed when the
		// Feishu exit silently suppresses it. Preserve silent no_mirror semantics
		// for unrelated mirror/system traffic, but surface this decision through
		// replyToOrigin's visible unavailable path.
		if metaString(env.Meta, "route_source") == replyToOriginRouteSource {
			return errors.New("reply_origin_feishu_skipped")
		}
		return nil
	}
	if h.Outbound == nil {
		return errors.New("outbound queue not configured")
	}
	if replyTo, ok := replySourceAddress(env.Meta); ok {
		to.OpenID = replyTo
	}
	chatType := model.ChatPersonal
	entityKind := "personal"
	if strings.HasPrefix(to.OpenID, "oc_") {
		chatType = model.ChatGroup
		entityKind = "group"
	}
	ensureReplyMention(&env, chatType)
	botChannelID, err := h.resolveFeishuBotChannel(ctx, to, env, chatType)
	if err != nil {
		return err
	}
	rendered := env
	rendered.Body = h.renderAgentFeishuBody(ctx, env)
	content, _ := json.Marshal(map[string]string{"text": renderFeishuText(rendered, chatType)})
	err = h.Outbound.Enqueue(ctx, model.Message{
		ID:           env.ID,
		ChatEntityID: botChannelID + ":" + entityKind + ":" + to.OpenID,
		BotChannelID: botChannelID,
		FeishuMsgID:  env.ID,
		ChatType:     chatType,
		Content:      string(content),
	})
	if err != nil {
		return err
	}
	from, parseErr := parseAddress(env.From)
	if parseErr == nil && from.Kind == addressSession && from.SessionName != "" && from.SessionName != "workpulse" {
		h.publishInputObservation(from.KeyID, from.SessionName, env, "out_"+env.ID, "cli_injected", "copy", false)
	}
	return nil
}

func (h *Hub) resolveFeishuBotChannel(ctx context.Context, to address, env model.Envelope, chatType model.ChatType) (string, error) {
	if sourceBot := metaString(env.Meta, "source_bot_channel_id"); sourceBot != "" {
		botChannelID := h.botChannel(sourceBot)
		if botChannelID == "" {
			return "", fmt.Errorf("source bot %s not configured", sourceBot)
		}
		return botChannelID, nil
	}
	if !isProducerEnvelope(env) {
		botChannelID := h.botChannel(to.BotName)
		if botChannelID == "" {
			return "", fmt.Errorf("bot %s not configured", to.BotName)
		}
		return botChannelID, nil
	}
	if targetBot := metaString(env.Meta, "target_bot"); targetBot != "" {
		botChannelID := h.botChannel(targetBot)
		if botChannelID == "" {
			return "", fmt.Errorf("producer target_bot %s not configured", targetBot)
		}
		return botChannelID, nil
	}
	if chatType == model.ChatGroup && h.Repo != nil {
		entities, err := h.Repo.ListChatEntities(ctx, model.ChatGroup)
		if err != nil {
			return "", err
		}
		for _, entity := range entities {
			if entity.Active && entity.FeishuID == to.OpenID && entity.BotChannelID != "" {
				return entity.BotChannelID, nil
			}
		}
	}
	if h.Repo != nil {
		from, err := parseAddress(env.From)
		if err == nil && from.KeyID != "" {
			accounts, err := h.Repo.ListAPIKeyAccounts(ctx, from.KeyID)
			if err != nil {
				return "", err
			}
			for _, account := range accounts {
				botChannelID, _, ok := accountEntityParts(account)
				if ok && botChannelID != "" {
					return botChannelID, nil
				}
			}
		}
	}
	return "", fmt.Errorf("producer target_group %s has no known bot_channel; set SH_TARGET_BOT or seed chat_entity", to.OpenID)
}

func isProducerEnvelope(env model.Envelope) bool {
	if env.Meta == nil {
		return false
	}
	if v, ok := env.Meta["producer"].(bool); ok && v {
		return true
	}
	if v, ok := env.Meta["producer"].(string); ok && truthyQuery(v) {
		return true
	}
	return false
}

func shouldSkipMirror(env model.Envelope) bool {
	if env.Meta == nil {
		return false
	}
	if isProducerEnvelope(env) {
		return false
	}
	// reply-to-origin is an explicit Feishu destination, not an incidental
	// terminal mirror. Keep no_mirror on the envelope so every other reuse still
	// suppresses mirroring, but do not let it cancel the requested return itself.
	if metaString(env.Meta, "type") == "agent_route" &&
		metaString(env.Meta, "route_source") == replyToOriginRouteSource &&
		kindOf(env) == kindReply {
		return false
	}
	// A channel route is already an explicit Feishu delivery, not an incidental
	// mirror. Keep no_mirror on the envelope for every other reuse of this
	// metadata, but do not let it cancel the requested channel delivery itself.
	channel := metaString(env.Meta, "channel")
	if metaString(env.Meta, "type") == "agent_route" && channel != "" && metaString(env.Meta, "route_source") == "channel_"+channel {
		return false
	}
	if v, ok := env.Meta["no_mirror"].(bool); ok && v {
		return true
	}
	if v, ok := env.Meta["system"].(bool); ok && v {
		return true
	}
	switch metaString(env.Meta, "type") {
	case "system", "online_directory", "mirror_control":
		return true
	default:
		return false
	}
}

func (h *Hub) annotateSourceContext(ctx context.Context, meta map[string]any, msg model.Message) {
	if meta == nil {
		return
	}
	meta["source_bot_channel_id"] = msg.BotChannelID
	meta["source_chat_type"] = string(msg.ChatType)
	meta["source_kind"] = "feishu"
	if model.NormalizeIngressProvenance(msg.IngressProvenance) == model.IngressUntrusted {
		meta[hubIngressMetaKey] = hubIngressFeishuUnknown
	} else {
		meta[hubIngressMetaKey] = hubIngressFeishu
	}
	source := sourceAccount(msg)
	if openID := feishuOpenIDFromAccount(source); openID != "" {
		meta["source_open_id"] = openID
	}
	sender := firstNonEmpty(msg.SenderOpenID, feishuOpenIDFromAccount(source))
	if sender != "" {
		meta["source_display_name"] = firstNonEmpty(h.displayNameForOpenID(ctx, sender, msg.BotChannelID), sender)
	}
	switch msg.ChatType {
	case model.ChatGroup:
		meta["source_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
		if strings.TrimSpace(msg.SenderOpenID) != "" {
			meta["source_sender_openid"] = strings.TrimSpace(msg.SenderOpenID)
		}
	case model.ChatPersonal:
		meta["source_chat_id"] = feishuOpenIDFromAccount(msg.ChatEntityID)
	}
}

func (h *Hub) displayNameForOpenID(ctx context.Context, openID, botChannelID string) string {
	// Every repository lookup stays outside h.mu; callers enrich before routing.
	if h.Repo == nil {
		return ""
	}
	if members, err := h.Repo.ListMembers(ctx); err == nil {
		for _, member := range members {
			if member.FeishuOpenID == openID && member.DisplayName != "" {
				return member.DisplayName
			}
		}
	}
	if botChannelID != "" {
		if entity, err := h.Repo.GetChatEntity(ctx, botChannelID, openID); err == nil && entity != nil && entity.DisplayName != "" {
			return entity.DisplayName
		}
	}
	if persons, err := h.Repo.ListSeenPersons(ctx); err == nil {
		for _, person := range persons {
			if person.OpenID == openID && person.Name != "" {
				return person.Name
			}
		}
	}
	return ""
}

func replySourceAddress(meta map[string]any) (string, bool) {
	switch model.ChatType(metaString(meta, "source_chat_type")) {
	case model.ChatGroup:
		if chatID := metaString(meta, "source_chat_id"); chatID != "" {
			return chatID, true
		}
	case model.ChatPersonal:
		if openID := firstNonEmpty(metaString(meta, "source_sender_openid"), metaString(meta, "source_open_id"), metaString(meta, "source_chat_id")); openID != "" {
			return openID, true
		}
	}
	return "", false
}

func ensureReplyMention(env *model.Envelope, chatType model.ChatType) {
	if chatType != model.ChatGroup || len(metaStringSlice(env.Meta, "at")) != 0 {
		return
	}
	sender := firstNonEmpty(metaString(env.Meta, "source_sender_openid"), metaString(env.Meta, "sender_open_id"))
	if sender == "" {
		return
	}
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["at"] = []string{sender}
}

type onlineSessionItem struct {
	Endpoint model.SessionEndpoint
	Owner    model.Member
	Client   *sessionClient
}

type onlineSessionSnapshot struct {
	KeyID    string
	Session  string
	Client   *sessionClient
	Accounts []string
	ConnSeq  int
}

func (h *Hub) loadSeenSessions(ctx context.Context) {
	h.seenSessionsOnce.Do(func() {
		if h.Repo == nil {
			return
		}
		endpoints, err := h.Repo.ListSessionEndpoints(ctx)
		if err != nil {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		for _, ep := range endpoints {
			if ep.OwnerKey != "" && ep.SessionName != "" {
				h.seenSessions[terminalKey(ep.OwnerKey, ep.SessionName)] = true
			}
		}
		h.seenSessionsOK = true
	})
}

func (h *Hub) markSessionSeen(ownerKey, sessionName string) bool {
	key := terminalKey(ownerKey, sessionName)
	h.mu.Lock()
	defer h.mu.Unlock()
	// Failure to load history is fail-closed: never turn a transient repository
	// error or Hub restart into a burst of "new session" notifications.
	if !h.seenSessionsOK || h.seenSessions[key] {
		return false
	}
	h.seenSessions[key] = true
	return true
}

func (h *Hub) notifyNewSession(ctx context.Context, ownerKey, sessionName string) {
	if !h.newSessionNotice || h.Outbound == nil || h.Repo == nil || ownerKey == "" || sessionName == "" {
		return
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return
	}
	var openID string
	for _, member := range members {
		if member.Active && member.OwnerKey == ownerKey {
			openID = member.FeishuOpenID
			break
		}
	}
	if openID == "" {
		return
	}
	botChannelID := h.botChannel("UnifiedRobot")
	content, _ := json.Marshal(map[string]string{"text": "会话 #" + sessionName + " 已上线"})
	_ = h.Outbound.Enqueue(ctx, model.Message{
		ID:           "session-online-" + randomHex(16),
		ChatEntityID: botChannelID + ":personal:" + openID,
		BotChannelID: botChannelID,
		ChatType:     model.ChatPersonal,
		Content:      string(content),
	})
}

func (h *Hub) scheduleOnlineBroadcast(ctx context.Context, keyID string) {
	if h.Repo == nil {
		return
	}
	ownerKey := h.ownerKeyForKey(ctx, keyID)
	if ownerKey == "" {
		return
	}
	delay := h.onlineDebounce
	if delay <= 0 {
		delay = 2500 * time.Millisecond
	}
	h.mu.Lock()
	if old := h.onlineTimers[ownerKey]; old != nil {
		old.Stop()
	}
	h.onlineTimers[ownerKey] = time.AfterFunc(delay, func() {
		h.backgroundWork("directory_timer", func() {
			bctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			h.mu.Lock()
			if h.onlineTimers[ownerKey] != nil {
				delete(h.onlineTimers, ownerKey)
			}
			h.mu.Unlock()
			_ = h.broadcastOnlineDirectory(bctx, ownerKey)
		})
	})
	h.mu.Unlock()
}

func (h *Hub) scheduleOnlineBroadcastForSession(ctx context.Context, keyID, sessionName string) {
	if h.sessionEndpointNoDirectory(ctx, keyID, sessionName) {
		return
	}
	h.scheduleOnlineBroadcast(ctx, keyID)
}

func (h *Hub) sessionEndpointNoDirectory(ctx context.Context, keyID, sessionName string) bool {
	if h.Repo == nil {
		return false
	}
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return false
	}
	for _, ep := range endpoints {
		if ep.KeyID == keyID && ep.SessionName == sessionName {
			return ep.NoDirectory
		}
	}
	return false
}

func (h *Hub) BroadcastOnlineDirectories(ctx context.Context) error {
	if h.Identity != nil {
		return h.broadcastNewPlatformDirectories(ctx)
	}
	owners, err := h.onlineOwners(ctx)
	if err != nil {
		return err
	}
	for _, owner := range owners {
		if err := h.broadcastOnlineDirectory(ctx, owner); err != nil {
			return err
		}
	}
	return nil
}

func (h *Hub) BroadcastOnlineDirectoryForKey(ctx context.Context, keyID string) error {
	ownerKey := h.ownerKeyForKey(ctx, keyID)
	if ownerKey == "" {
		return nil
	}
	return h.broadcastOnlineDirectory(ctx, ownerKey)
}

func (h *Hub) onlineOwners(ctx context.Context) ([]string, error) {
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return nil, err
	}
	byOwner := map[string]model.Member{}
	for _, member := range members {
		if member.Active {
			byOwner[member.OwnerKey] = member
		}
	}
	owners := map[string]bool{}
	h.mu.Lock()
	keys := make([]string, 0, len(h.sessionClients))
	for keyID, sessions := range h.sessionClients {
		if len(sessions) > 0 {
			keys = append(keys, keyID)
		}
	}
	h.mu.Unlock()
	for _, keyID := range keys {
		if owner := h.ownerKeyForKeyWithMembers(ctx, keyID, byOwner); owner != "" {
			owners[owner] = true
		}
	}
	out := make([]string, 0, len(owners))
	for owner := range owners {
		out = append(out, owner)
	}
	sort.Strings(out)
	return out, nil
}

func (h *Hub) ownerKeyForKey(ctx context.Context, keyID string) string {
	if h.Identity != nil {
		if owner, ok := strings.CutPrefix(keyID, "u_"); ok && owner != "" {
			u, err := h.Identity.Store.GetUser(ctx, owner)
			if err == nil && u.State == identity.UserActive {
				return u.UserIDLC()
			}
			return ""
		}
	}
	accounts, err := h.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil || len(accounts) == 0 {
		return ""
	}
	return h.ownerKeyForKeyWithAccounts(ctx, keyID, accounts)
}

func (h *Hub) ownerKeyForKeyWithAccounts(ctx context.Context, keyID string, accounts []string) string {
	if owner := h.ownerKeyFromBoundAccounts(ctx, accounts); owner != "" {
		return owner
	}
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return ""
	}
	return h.ownerKeyForKeyWithMembersAndAccounts(accounts, members)
}

func (h *Hub) ownerKeyFromBoundAccounts(ctx context.Context, accounts []string) string {
	owners := map[string]bool{}
	for _, account := range accounts {
		botChannelID, feishuID, ok := accountEntityParts(account)
		if !ok {
			continue
		}
		entity, err := h.Repo.GetChatEntity(ctx, botChannelID, feishuID)
		if err != nil || entity == nil || !entity.Active || entity.BoundOwner == "" {
			continue
		}
		owners[entity.BoundOwner] = true
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) ownerKeyForKeyWithMembersAndAccounts(accounts []string, members []model.Member) string {
	owners := map[string]bool{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if account == member.OwnerKey || accountBelongsToMember(account, member) {
				owners[member.OwnerKey] = true
				break
			}
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) broadcastOnlineDirectory(ctx context.Context, ownerKey string) error {
	if h.Identity != nil {
		return h.broadcastNewPlatformDirectories(ctx)
	}
	start := time.Now()
	items, owner, err := h.onlineDirectory(ctx, ownerKey)
	if err != nil {
		return err
	}
	snapshotDur := time.Since(start)
	if owner.OwnerKey == "" {
		return nil
	}
	text := renderOnlineDirectory(owner, items)
	broadcastKey := broadcastDedupKey("online_directory", randomHex(16), owner.OwnerKey)
	fanoutStart := time.Now()
	var wg sync.WaitGroup
	for i, item := range items {
		env := model.Envelope{
			ID:   randomHex(16),
			To:   sessionAddress(item.Endpoint.SessionName, item.Endpoint.KeyID),
			From: sessionAddress("workpulse", item.Endpoint.KeyID),
			Body: text,
			TS:   time.Now().Unix(),
			Meta: map[string]any{"type": "online_directory", "owner": owner.OwnerKey, "system": true, "no_mirror": true},
		}
		annotateBroadcastMirror(&env, broadcastKey, i == 0)
		// 异步扇出:每个会话独立 goroutine + 5s 超时。一个慢/半死的客户端只卡自己的
		// goroutine、超时即放弃,不再阻塞后续所有会话的广播——修"全体同时收不到在线清单"根因。
		wg.Add(1)
		go func(c *sessionClient, e model.Envelope) {
			defer wg.Done()
			wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			_ = c.write(wctx, e)
		}(item.Client, env)
	}
	wg.Wait()
	fanoutDur := time.Since(fanoutStart)
	log.Printf("online directory broadcast timing owner=%s total=%s snapshot=%s fanout=%s clients=%d", owner.OwnerKey, time.Since(start).Round(time.Millisecond), snapshotDur.Round(time.Millisecond), fanoutDur.Round(time.Millisecond), len(items))
	return nil
}

func annotateBroadcastMirror(env *model.Envelope, dedupKey string, primary bool) {
	if env.Meta == nil {
		env.Meta = map[string]any{}
	}
	env.Meta["broadcast_dedup_key"] = dedupKey
	env.Meta["mirror_primary"] = primary
}

func broadcastDedupKey(kind, sourceID, ownerKey string) string {
	return strings.Join([]string{
		"broadcast",
		kind,
		firstNonEmpty(ownerKey, "unknown-owner"),
		firstNonEmpty(sourceID, randomHex(16)),
	}, ":")
}

func (h *Hub) onlineDirectory(ctx context.Context, ownerKey string) ([]onlineSessionItem, model.Member, error) {
	start := time.Now()
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return nil, model.Member{}, err
	}
	membersDur := time.Since(start)
	var owner model.Member
	byOwner := map[string]model.Member{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		byOwner[member.OwnerKey] = member
		if member.OwnerKey == ownerKey {
			owner = member
		}
	}
	if owner.OwnerKey == "" && h.Identity != nil {
		if u, err := h.Identity.Store.GetUser(ctx, ownerKey); err == nil && u.State == identity.UserActive {
			owner = model.Member{OwnerKey: u.UserIDLC(), DisplayName: firstNonEmpty(u.DisplayName, u.ShortName, u.UserID), Active: true}
		}
	}
	if owner.OwnerKey == "" {
		return nil, model.Member{}, nil
	}
	endpointsStart := time.Now()
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return nil, model.Member{}, err
	}
	endpointsDur := time.Since(endpointsStart)
	endpointByID := map[string]model.SessionEndpoint{}
	for _, ep := range endpoints {
		endpointByID[ep.KeyID+"#"+ep.SessionName] = ep
	}
	snapshotStart := time.Now()
	h.mu.Lock()
	var snapshots []onlineSessionSnapshot
	var missingAccountKeys []string
	missingAccountSeen := map[string]bool{}
	for keyID, sessions := range h.sessionClients {
		accounts := stringSetKeys(h.keyAccounts[keyID])
		if len(accounts) == 0 && !missingAccountSeen[keyID] {
			missingAccountSeen[keyID] = true
			missingAccountKeys = append(missingAccountKeys, keyID)
		}
		for sessionName, c := range sessions {
			snapshots = append(snapshots, onlineSessionSnapshot{
				KeyID: keyID, Session: sessionName, Client: c, Accounts: append([]string(nil), accounts...),
				ConnSeq: c.connSeq,
			})
		}
	}
	h.mu.Unlock()
	snapshotDur := time.Since(snapshotStart)
	accountsStart := time.Now()
	accountByKey := map[string][]string{}
	if len(missingAccountKeys) > 0 {
		accountByKey, err = h.Repo.ListAPIKeyAccountsForKeys(ctx, missingAccountKeys)
		if err != nil {
			return nil, model.Member{}, err
		}
	}
	accountsDur := time.Since(accountsStart)
	boundStart := time.Now()
	boundOwners, err := h.boundOwnersByAccount(ctx)
	if err != nil {
		return nil, model.Member{}, err
	}
	boundDur := time.Since(boundStart)
	var clients []onlineSessionItem
	for _, snap := range snapshots {
		accounts := snap.Accounts
		if len(accounts) == 0 {
			accounts = accountByKey[snap.KeyID]
		}
		resolvedOwner := ownerKeyForAccounts(accounts, byOwner, boundOwners)
		if resolvedOwner == "" {
			resolvedOwner = h.ownerKeyForKey(ctx, snap.KeyID)
		}
		if resolvedOwner != ownerKey {
			continue
		}
		ep := endpointByID[snap.KeyID+"#"+snap.Session]
		if ep.KeyID == "" {
			ep = model.SessionEndpoint{KeyID: snap.KeyID, SessionName: snap.Session, Active: true}
		}
		if !ep.Active {
			ep.Active = true
		}
		if ep.ConnSeq == 0 && snap.ConnSeq > 0 {
			ep.ConnSeq = snap.ConnSeq
		}
		if ep.NoDirectory {
			continue
		}
		clients = append(clients, onlineSessionItem{Endpoint: ep, Owner: owner, Client: snap.Client})
	}
	sort.Slice(clients, func(i, j int) bool {
		a, b := clients[i].Endpoint, clients[j].Endpoint
		if a.SessionName == b.SessionName {
			return a.KeyID < b.KeyID
		}
		return a.SessionName < b.SessionName
	})
	log.Printf("online directory snapshot timing owner=%s total=%s members=%s endpoints=%s snapshot=%s account_fill=%s bound=%s sessions=%d clients=%d", ownerKey, time.Since(start).Round(time.Millisecond), membersDur.Round(time.Millisecond), endpointsDur.Round(time.Millisecond), snapshotDur.Round(time.Millisecond), accountsDur.Round(time.Millisecond), boundDur.Round(time.Millisecond), len(snapshots), len(clients))
	return clients, owner, nil
}

func (h *Hub) boundOwnersByAccount(ctx context.Context) (map[string]string, error) {
	out := map[string]string{}
	for _, typ := range []model.ChatType{model.ChatPersonal, model.ChatGroup} {
		entities, err := h.Repo.ListChatEntities(ctx, typ)
		if err != nil {
			return nil, err
		}
		for _, entity := range entities {
			if !entity.Active || entity.BoundOwner == "" {
				continue
			}
			out[entity.ID] = entity.BoundOwner
			if entity.BotChannelID != "" && entity.FeishuID != "" {
				out[entity.BotChannelID+":"+entity.FeishuID] = entity.BoundOwner
			}
		}
	}
	return out, nil
}

func ownerKeyForAccounts(accounts []string, members map[string]model.Member, boundOwners map[string]string) string {
	if owner := ownerKeyFromBoundAccountsCached(accounts, boundOwners); owner != "" {
		return owner
	}
	owners := map[string]bool{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if account == member.OwnerKey || accountBelongsToMember(account, member) {
				owners[member.OwnerKey] = true
				break
			}
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func ownerKeyFromBoundAccountsCached(accounts []string, boundOwners map[string]string) string {
	owners := map[string]bool{}
	for _, account := range accounts {
		if owner := boundOwners[account]; owner != "" {
			owners[owner] = true
			continue
		}
		botChannelID, feishuID, ok := accountEntityParts(account)
		if !ok {
			continue
		}
		if owner := boundOwners[botChannelID+":"+feishuID]; owner != "" {
			owners[owner] = true
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) ownerKeyForKeyWithMembers(ctx context.Context, keyID string, members map[string]model.Member) string {
	accounts, err := h.Repo.ListAPIKeyAccounts(ctx, keyID)
	if err != nil || len(accounts) == 0 {
		return ""
	}
	if owner := h.ownerKeyFromBoundAccounts(ctx, accounts); owner != "" {
		return owner
	}
	owners := map[string]bool{}
	for _, member := range members {
		if !member.Active {
			continue
		}
		for _, account := range accounts {
			if account == member.OwnerKey || accountBelongsToMember(account, member) {
				owners[member.OwnerKey] = true
				break
			}
		}
	}
	if len(owners) != 1 {
		return ""
	}
	for owner := range owners {
		return owner
	}
	return ""
}

func (h *Hub) issueOwnerLinkCode(ownerKey string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := time.Now()
	for code, token := range h.linkTokens {
		if now.After(token.expiresAt) {
			delete(h.linkTokens, code)
		}
	}
	for i := 0; i < 64; i++ {
		code := randomHex(12)
		if _, exists := h.linkTokens[code]; exists {
			continue
		}
		h.linkTokens[code] = ownerLinkToken{ownerKey: ownerKey, expiresAt: now.Add(ownerLinkTokenTTL)}
		return code
	}
	code := randomHex(16)
	h.linkTokens[code] = ownerLinkToken{ownerKey: ownerKey, expiresAt: now.Add(ownerLinkTokenTTL)}
	return code
}

func (h *Hub) ownerLinkCodeValid(ownerKey, code string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	code = strings.TrimSpace(code)
	token, ok := h.linkTokens[code]
	if !ok {
		return false
	}
	if time.Now().After(token.expiresAt) {
		delete(h.linkTokens, code)
		return false
	}
	return token.ownerKey == ownerKey
}

func (h *Hub) terminalPageCodes(keyID, sessionName string) []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.terminals[terminalKey(keyID, sessionName)]
	if st == nil {
		return nil
	}
	codes := make([]string, 0, len(st.viewers))
	for _, viewer := range st.viewers {
		if viewer.code != "" {
			codes = append(codes, viewer.code)
		}
	}
	sort.Strings(codes)
	return codes
}

func (h *Hub) HandleOwnerLinksPage(w http.ResponseWriter, r *http.Request) {
	ownerKey := strings.TrimSpace(r.PathValue("ownerKey"))
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if ownerKey == "" || strings.Contains(ownerKey, "/") || code == "" || !h.ownerLinkCodeValid(ownerKey, code) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	items, owner, err := h.onlineDirectory(r.Context(), ownerKey)
	if err != nil {
		http.Error(w, "load links failed", http.StatusInternalServerError)
		return
	}
	if owner.OwnerKey == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(renderOwnerLinksHTML(owner, items, code, h)))
}

var terminalViewBase = strings.TrimRight(os.Getenv("WP_VIEW_BASE_URL"), "/")

func ownerLinksURL(ownerKey, code string) string {
	path := "/links/" + url.PathEscape(ownerKey) + "?code=" + url.QueryEscape(code)
	if terminalViewBase == "" {
		return path
	}
	return terminalViewBase + path
}

func viewURL(sessionName string) string {
	path := "/view/" + url.PathEscape(sessionName)
	if terminalViewBase == "" {
		return path
	}
	return terminalViewBase + path
}

func renderOwnerLinksText(owner model.Member, items []onlineSessionItem, code string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "【在线客户端访问清单】%s\n", firstNonEmpty(owner.DisplayName, owner.OwnerKey))
	fmt.Fprintf(&b, "清单页面: %s\n", ownerLinksURL(owner.OwnerKey, code))
	if len(items) == 0 {
		b.WriteString("暂无在线会话。")
		return b.String()
	}
	for i, item := range items {
		ep := item.Endpoint
		fmt.Fprintf(&b, "%d. %s · %s/%s · 在线 · view: %s", i+1, ep.SessionName, unknown(ep.Tool), unknown(ep.Model), viewURL(ep.SessionName))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderOwnerLinksHTML(owner model.Member, items []onlineSessionItem, code string, h *Hub) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">`)
	b.WriteString(`<meta http-equiv="refresh" content="10">`)
	b.WriteString(`<title>DingWei 在线客户端</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;margin:24px;color:#1f2328;background:#f6f8fa}table{border-collapse:collapse;width:100%;background:#fff}th,td{border:1px solid #d0d7de;padding:8px;text-align:left}th{background:#f0f3f6}a{color:#0969da}.muted{color:#656d76}.code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace}</style></head><body>`)
	fmt.Fprintf(&b, `<h1>%s 在线客户端</h1>`, html.EscapeString(firstNonEmpty(owner.DisplayName, owner.OwnerKey)))
	fmt.Fprintf(&b, `<p class="muted">owner: <span class="code">%s</span> · 页面码 <span class="code">%s</span> · 每 10 秒自动刷新</p>`, html.EscapeString(owner.OwnerKey), html.EscapeString(code))
	if len(items) == 0 {
		b.WriteString(`<p>暂无在线会话。</p></body></html>`)
		return b.String()
	}
	b.WriteString(`<table><thead><tr><th>完整名</th><th>工具/模型</th><th>状态</th><th>View</th><th>页面码</th></tr></thead><tbody>`)
	for _, item := range items {
		ep := item.Endpoint
		codes := h.terminalPageCodes(ep.KeyID, ep.SessionName)
		codeText := "未打开"
		if len(codes) > 0 {
			codeText = strings.Join(codes, ", ")
		}
		fmt.Fprintf(&b, `<tr><td class="code">%s</td><td>%s/%s</td><td>在线</td><td><a href="%s" target="_blank" rel="noreferrer">打开</a></td><td class="code">%s</td></tr>`,
			html.EscapeString(ep.SessionName),
			html.EscapeString(unknown(ep.Tool)),
			html.EscapeString(unknown(ep.Model)),
			html.EscapeString(viewURL(ep.SessionName)),
			html.EscapeString(codeText),
		)
	}
	b.WriteString(`</tbody></table></body></html>`)
	return b.String()
}

func renderOnlineDirectory(owner model.Member, items []onlineSessionItem) string {
	var b strings.Builder
	b.WriteString("\n**********\n")
	fmt.Fprintf(&b, "【DingWei在线清单】同账号在线AI会话,供跨会话寻址(上下线): %s\n", firstNonEmpty(owner.DisplayName, owner.OwnerKey))
	if len(items) == 0 {
		b.WriteString("暂无在线会话。")
		b.WriteString("\n**********\n")
		return b.String()
	}
	for i, item := range items {
		ep := item.Endpoint
		short := shortSessionName(ep.SessionName)
		fmt.Fprintf(&b, "%d. #%s · %s/%s · %s · %s(末%s) · @%s#%s", i+1, short, unknown(ep.Tool), unknown(ep.Model), versionLabel(ep.Version), unknown(ep.ClientIP), keyTail(ep.KeyID), owner.OwnerKey, short)
		if item.Client != nil && item.Client.osName != "" {
			fmt.Fprintf(&b, " · %s", item.Client.osName)
		}
		if ep.ConnSeq > 0 {
			fmt.Fprintf(&b, " · 连接:%s", sessionConnSeqName(ep.SessionName, ep.ConnSeq))
		}
		if terminalViewBase != "" && item.Client != nil && item.Client.webTerminal {
			fmt.Fprintf(&b, " · 页面 %s/view/%s", terminalViewBase, url.PathEscape(ep.SessionName))
		}
		if ep.SessionName != short {
			fmt.Fprintf(&b, " · 全名:%s", ep.SessionName)
		}
		if strings.TrimSpace(ep.FullSessionName) != "" && ep.FullSessionName != ep.SessionName {
			fmt.Fprintf(&b, " · 终端:%s", strings.TrimSpace(ep.FullSessionName))
		}
		if sessionNameEnforceMode() == "warn" {
			if nameWarn := sessionNamePolicyWarning(ep.SessionName, ep.KeyID, ep.OwnerKey); nameWarn != "" {
				fmt.Fprintf(&b, " · 命名告警:%s", nameWarn)
			}
		}
		if versionEnforceMode() == "warn" {
			if versionWarn := versionCompatibilityWarning(ep.Version); versionWarn != "" {
				fmt.Fprintf(&b, " · 版本告警:%s", versionWarn)
			}
		}
		if ep.Producer || strings.TrimSpace(ep.TargetGroup) != "" {
			fmt.Fprintf(&b, " · Producer:%s", yesNo(ep.Producer))
			if strings.TrimSpace(ep.TargetGroup) != "" {
				fmt.Fprintf(&b, "→%s", strings.TrimSpace(ep.TargetGroup))
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString("**********\n")
	return b.String()
}

func versionLabel(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "v?"
	}
	if strings.HasPrefix(strings.ToLower(version), "v") {
		return version
	}
	return "v" + version
}

func versionEnforceMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WP_VERSION_ENFORCE"))) {
	case "enforce":
		return "enforce"
	default:
		return "warn"
	}
}

func versionCompatibilityWarning(clientVersion string) string {
	clientVersion = strings.TrimSpace(clientVersion)
	if clientVersion == "" {
		return fmt.Sprintf("未上报版本,要求 %s", dwversion.Version)
	}
	client, ok := parseWireVersion(clientVersion)
	if !ok {
		return fmt.Sprintf("版本格式非法 %q,要求 %s", clientVersion, dwversion.Version)
	}
	required, ok := parseWireVersion(dwversion.Version)
	if !ok {
		return fmt.Sprintf("hub 版本格式非法 %q", dwversion.Version)
	}
	if client[0] != required[0] || client[1] != required[1] {
		return fmt.Sprintf("版本不兼容,当前 %s,要求 %s", clientVersion, dwversion.Version)
	}
	return ""
}

func parseWireVersion(v string) ([4]int, bool) {
	var out [4]int
	parts := strings.Split(strings.TrimSpace(v), ".")
	if len(parts) != 4 {
		return out, false
	}
	for i, part := range parts {
		if part == "" {
			return out, false
		}
		n := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				return out, false
			}
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out, true
}

func versionRejectionMessage(clientVersion, reason string) string {
	current := strings.TrimSpace(clientVersion)
	if current == "" {
		current = "未上报"
	}
	return fmt.Sprintf("sessionHelper 版本不兼容: 当前版本=%s; 要求版本=%s; 原因=%s; 下载地址=%s", current, dwversion.Version, reason, sessionHelperDownloadURL())
}

func sessionHelperDownloadURL() string {
	return "https://dingwei.wegoab.com/downloads/sessionHelper-" + dwversion.Version + "/"
}

func shortSessionName(sessionName string) string {
	sessionName = strings.TrimSpace(sessionName)
	parts := strings.Split(sessionName, "-")
	if len(parts) == 3 && sessionNamePattern.MatchString(sessionName) && strings.TrimSpace(parts[1]) != "" {
		return parts[1]
	}
	if len(parts) >= 3 && parts[0] == "sh" && strings.TrimSpace(parts[1]) != "" {
		return parts[1]
	}
	return sessionName
}

func sessionNameEnforceMode() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("WP_NAME_ENFORCE"))) {
	case "off":
		return "off"
	case "enforce":
		return "enforce"
	default:
		return "warn"
	}
}

func sessionNamePolicyWarning(sessionName, keyID, ownerKey string) string {
	sessionName = strings.TrimSpace(sessionName)
	keyID = strings.TrimSpace(keyID)
	ownerKey = strings.TrimSpace(ownerKey)
	if !sessionNamePattern.MatchString(sessionName) {
		return "会话名不合规,须为 <owner_key>-<短名>-<key末4位>,如 fulei-dev1013-3dd6"
	}
	parts := strings.Split(sessionName, "-")
	if len(parts) != 3 {
		return "会话名不合规,须为 <owner_key>-<短名>-<key末4位>,如 fulei-dev1013-3dd6"
	}
	if parts[2] != keyTail(keyID) {
		return "会话名末4位与 SH_KEY_ID 不匹配"
	}
	if ownerKey == "" {
		return "无法确认该 key 绑定成员 owner_key"
	}
	if parts[0] != ownerKey {
		return "会话名 owner_key 与该 key 绑定成员不匹配"
	}
	return ""
}

func keyTail(keyID string) string {
	keyID = strings.ToLower(strings.TrimSpace(keyID))
	if len(keyID) <= 4 {
		return keyID
	}
	return keyID[len(keyID)-4:]
}

func unknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "未知"
	}
	return strings.TrimSpace(value)
}

func yesNo(v bool) string {
	if v {
		return "是"
	}
	return "否"
}

func (h *Hub) readSessionMessage(ctx context.Context, c *sessionClient) (websocket.MessageType, []byte, error) {
	typ, data, err := c.conn.Read(ctx)
	if err == nil {
		c.recordActivity(h.now().UTC())
	}
	return typ, data, err
}

// watchSessionIdle closes only genuinely inactive sessions. Ping and pong
// handlers update lastSeenAt independently of the Hub write lock, so a healthy
// idle client is not disconnected merely because it sends no data frames.
func (h *Hub) watchSessionIdle(ctx context.Context, c *sessionClient) {
	idleTimeout := sessionReadIdleTimeout
	if idleTimeout <= 0 {
		idleTimeout = 75 * time.Second
	}
	interval := sessionIdleWatchdogInterval
	if interval <= 0 {
		interval = 15 * time.Second
	}
	tick, stop := h.idleTicker(interval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick:
			if now.Sub(c.lastSeen()) > idleTimeout {
				c.obsConn.SetLocalReason("idle_timeout")
				_ = c.conn.Close(websocket.StatusGoingAway, "idle timeout")
				return
			}
		}
	}
}

func (h *Hub) idleTicker(d time.Duration) (<-chan time.Time, func()) {
	if h.IdleTickerFunc != nil {
		return h.IdleTickerFunc(d)
	}
	t := time.NewTicker(d)
	return t.C, t.Stop
}

// Server-initiated WebSocket control pings keep otherwise idle helpers alive
// through proxies and detect half-open connections before a replacement
// reconnect starts a 1008 churn loop.
const (
	defaultSessionKeepaliveInterval = 30 * time.Second
	defaultSessionKeepaliveTimeout  = 5 * time.Second
)

// keepaliveParams 由实例字段取保活参数（组长 R-M8-KEEPALIVE-DEFAULT：抽成纯函数，好让「零值取默认、负数关闭」有格钉住）。
// 间隔 0 ⇒ 默认 30s；间隔为负 ⇒ 不保活（enabled=false）；等 pong 上限 <=0 ⇒ 默认 5s。
func keepaliveParams(interval, timeout time.Duration) (time.Duration, time.Duration, bool) {
	if interval == 0 {
		interval = defaultSessionKeepaliveInterval
	}
	if interval < 0 {
		return 0, 0, false
	}
	if timeout <= 0 {
		timeout = defaultSessionKeepaliveTimeout
	}
	return interval, timeout, true
}

func (h *Hub) watchSessionKeepalive(ctx context.Context, c *sessionClient) {
	interval, timeout, enabled := keepaliveParams(h.keepaliveInterval, h.keepaliveTimeout)
	if !enabled || c == nil || c.conn == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// 🔴 Ping 不持 c.mu（组长 lead-m8-flake-findings ①）：Ping 要等对端回 pong，最长 timeout；
			//    持写锁期间该会话的所有出站写都被挡住。库本身对控制帧与数据帧的写已有帧级互斥，无需再加锁。
			pingCtx, cancel := context.WithTimeout(ctx, timeout)
			if hook := keepalivePingHook.Load(); hook != nil {
				(*hook)(false)
			}
			pingStart := time.Now()
			err := c.conn.Ping(pingCtx)
			c.obsConn.Ping(time.Since(pingStart), err)
			if hook := keepalivePingHook.Load(); hook != nil {
				(*hook)(true)
			}
			cancel()
			if err != nil {
				c.obsConn.SetLocalReason("keepalive_failed")
				_ = c.conn.Close(websocket.StatusGoingAway, "keepalive failed")
				return
			}
		}
	}
}

func (c *sessionClient) recordActivity(ts time.Time) {
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	c.stateMu.Lock()
	c.lastSeenAt = ts
	c.stateMu.Unlock()
}

func (c *sessionClient) lastSeen() time.Time {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if c.lastSeenAt.IsZero() {
		return time.Now().UTC()
	}
	return c.lastSeenAt
}

func (h *Hub) touchSessionEndpoint(ctx context.Context, c *sessionClient) {
	if h.Repo == nil || c == nil {
		return
	}
	// R-V2-0105 件三：每条入站消息都会走到这里；Hub 的 SQLite 只有一条连接（SetMaxOpenConns(1)），
	// 洪流时每秒数千次 UPDATE 与控制台 HTTP 同队排。同一连接 sessionTouchMinInterval 内只落库一次；
	// 内存 lastSeen 照常更新（在线判定读内存），断开收尾的 UpsertSessionEndpoint 不经此处、照写。
	// session_endpoint.last_seen_at 只供管理页展示，无按新鲜度判定的读者（核于 767e073a）。
	now := h.now()
	c.stateMu.Lock()
	skip := h.sessionTouchMinInterval > 0 && !c.lastTouchWritten.IsZero() && now.Sub(c.lastTouchWritten) < h.sessionTouchMinInterval
	if !skip {
		c.lastTouchWritten = now
	}
	c.stateMu.Unlock()
	if skip {
		return
	}
	_ = h.Repo.TouchSessionEndpoint(ctx, c.keyID, c.sessionName, c.lastSeen())
}

func sessionConnSeqName(sessionName string, seq int) string {
	if seq <= 0 {
		return ""
	}
	return fmt.Sprintf("%s%06d", sessionName, seq)
}

func (c *sessionClient) write(ctx context.Context, env model.Envelope) error {
	payload, _ := json.Marshal(env)
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Write(ctx, websocket.MessageText, payload)
}

func renderFeishuText(env model.Envelope, chatType model.ChatType) string {
	body := env.Body
	if chatType != model.ChatGroup {
		return body
	}
	at := metaStringSlice(env.Meta, "at")
	if len(at) == 0 {
		return body
	}
	var b strings.Builder
	for _, openID := range at {
		openID = strings.TrimSpace(openID)
		if openID == "" {
			continue
		}
		fmt.Fprintf(&b, `<at user_id="%s"></at>`, openID)
	}
	if b.Len() == 0 {
		return body
	}
	if strings.TrimSpace(body) != "" {
		b.WriteByte(' ')
		b.WriteString(body)
	}
	return b.String()
}

func metaStringSlice(meta map[string]any, key string) []string {
	if meta == nil {
		return nil
	}
	switch v := meta[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func metaString(meta map[string]any, key string) string {
	if meta == nil {
		return ""
	}
	if v, ok := meta[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func metaInt(meta map[string]any, key string) int {
	if meta == nil {
		return 0
	}
	switch value := meta[key].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func metaBool(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	switch v := meta[key].(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "1", "true", "yes", "y", "on":
			return true
		}
	}
	return false
}

func (h *Hub) claimEnvelope(env model.Envelope) (bool, string, error) {
	if env.ID == "" {
		return true, "", nil
	}
	sig := envelopeSignature(env)
	h.mu.Lock()
	defer h.mu.Unlock()
	claimKey := subAgentClaimKey(env)
	if old, ok := h.envelopeIDs[claimKey]; ok {
		if old == sig {
			return false, sig, nil
		}
		return false, sig, fmt.Errorf("duplicate envelope id %s with different payload", env.ID)
	}
	h.envelopeIDs[claimKey] = sig
	return true, sig, nil
}

func (h *Hub) unclaimEnvelope(id, sig string) {
	if id == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.envelopeIDs[id] == sig {
		delete(h.envelopeIDs, id)
	}
}

func envelopeSignature(env model.Envelope) string {
	sum := sha256.Sum256([]byte(env.To + "\x00" + env.From + "\x00" + env.Body))
	return hex.EncodeToString(sum[:])
}

func stripPrefix(expr, input string, caseSensitive bool) string {
	if ok, n := router.MatchPrefixLen(expr, input, caseSensitive); ok {
		return strings.TrimSpace(input[n:])
	}
	return input
}

func validatePrefixRule(rule model.RoutingRule) error {
	if strings.TrimSpace(rule.MatchExpr) == "" {
		return errors.New("prefix match expr is empty")
	}
	for _, p := range router.SplitPatterns(rule.MatchExpr) {
		if p == "*" {
			return errors.New("high-risk wildcard prefix * requires explicit override")
		}
	}
	return nil
}

func (h *Hub) ensureNoPrefixOverlap(ctx context.Context, rule model.RoutingRule) error {
	newScope, err := h.effectiveAccounts(ctx, rule)
	if err != nil {
		return err
	}
	if len(newScope) == 0 {
		return nil
	}
	routes, err := h.Repo.ListAllPrefixRoutes(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		if route.Rule.ID == rule.ID {
			continue
		}
		if !sameScopeEntityType(rule.ScopeEntityType, route.Rule.ScopeEntityType) {
			continue
		}
		oldScope, err := h.effectiveAccounts(ctx, route.Rule)
		if err != nil {
			return err
		}
		if !accountScopesOverlap(newScope, oldScope) {
			continue
		}
		caseSensitive := rule.CaseSensitive && route.Rule.CaseSensitive
		if router.Overlaps(rule.MatchExpr, route.Rule.MatchExpr, caseSensitive) {
			return fmt.Errorf("prefix route overlaps existing rule %s", route.Rule.ID)
		}
	}
	return nil
}

func (h *Hub) effectiveAccounts(ctx context.Context, rule model.RoutingRule) (map[string]bool, error) {
	bound, err := h.Repo.ListServiceBoundAccounts(ctx, rule.ServiceID)
	if err != nil {
		return nil, err
	}
	boundSet := map[string]bool{}
	for _, id := range bound {
		boundSet[id] = true
	}
	if rule.AccountScopeJSON == "" {
		return boundSet, nil
	}
	var scoped []string
	if err := json.Unmarshal([]byte(rule.AccountScopeJSON), &scoped); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, id := range scoped {
		if boundSet[id] {
			out[id] = true
		}
	}
	return out, nil
}

func sameScopeEntityType(a, b string) bool {
	return a == "" || b == "" || a == b
}

func accountScopesOverlap(a, b map[string]bool) bool {
	if len(a) > len(b) {
		a, b = b, a
	}
	for id := range a {
		if b[id] {
			return true
		}
	}
	return false
}

type addressKind string

const (
	addressSession addressKind = "session"
	addressFeishu  addressKind = "feishu"
)

type address struct {
	Kind        addressKind
	SessionName string
	OpenID      string
	KeyID       string
	BotName     string
}

func parseAddress(raw string) (address, error) {
	parts := strings.Split(strings.TrimSpace(raw), "#")
	switch len(parts) {
	case 2:
		if parts[0] == "" || parts[1] == "" {
			return address{}, errors.New("invalid session address")
		}
		return address{Kind: addressSession, SessionName: parts[0], KeyID: parts[1]}, nil
	case 3:
		if parts[0] == "" || parts[1] == "" || parts[2] == "" {
			return address{}, errors.New("invalid feishu address")
		}
		return address{Kind: addressFeishu, OpenID: parts[0], KeyID: parts[1], BotName: parts[2]}, nil
	default:
		return address{}, errors.New("invalid address")
	}
}

func sessionAddress(sessionName, keyID string) string {
	return sessionName + "#" + keyID
}

func sessionServiceID(keyID, sessionName string) string {
	return "session:" + keyID + ":" + sessionName
}

func sessionServiceParts(serviceID string) (keyID string, sessionName string, ok bool) {
	if !strings.HasPrefix(serviceID, "session:") {
		return "", "", false
	}
	parts := strings.SplitN(strings.TrimPrefix(serviceID, "session:"), ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

func clientIPFromRequest(r *http.Request) string {
	remote := hostOnly(r.RemoteAddr)
	if isTrustedProxy(remote) {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
				return first
			}
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			return real
		}
	}
	return remote
}

func truthyQuery(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func envDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return fallback
	}
	return duration
}

func hostOnly(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(addr)
}

func isTrustedProxy(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func feishuAddress(openID, keyID, botName string) string {
	return openID + "#" + keyID + "#" + botName
}

func parseSelector(text string) (sessionName, body string, ok bool) {
	text = stripLeadingMentions(text)
	if !strings.HasPrefix(text, "#") || len(text) == 1 {
		return "", "", false
	}
	rest := strings.TrimPrefix(text, "#")
	if len(rest) == 0 || strings.TrimLeft(rest, " \t\r\n") != rest {
		return "", "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", false
	}
	sessionName = strings.TrimSpace(fields[0])
	if sessionName == "" || strings.Contains(sessionName, "#") {
		return "", "", false
	}
	body = strings.TrimSpace(strings.TrimPrefix(rest, sessionName))
	return sessionName, body, true
}

func (h *Hub) parseSelector(botChannelID, text string) (sessionName, body string, ok bool) {
	return parseSelector(h.stripLeadingBotMentions(botChannelID, text))
}

func parseMemberMention(text string) (memberName, sessionName, body string, ok bool) {
	text = stripLeadingMentions(text)
	if !strings.HasPrefix(text, "@") || strings.HasPrefix(text, "@_user_") {
		return "", "", "", false
	}
	rest := strings.TrimPrefix(text, "@")
	if rest == "" || strings.TrimLeft(rest, " \t\r\n") != rest {
		return "", "", "", false
	}
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return "", "", "", false
	}
	head := strings.TrimSpace(fields[0])
	if head == "" {
		return "", "", "", false
	}
	if strings.Contains(head, "#") {
		parts := strings.SplitN(head, "#", 2)
		memberName = strings.TrimSpace(parts[0])
		sessionName = strings.TrimSpace(parts[1])
	} else {
		memberName = head
	}
	if memberName == "" || strings.Contains(memberName, "#") || strings.Contains(sessionName, "#") {
		return "", "", "", false
	}
	body = strings.TrimSpace(strings.TrimPrefix(rest, head))
	return memberName, sessionName, body, true
}

func (h *Hub) parseMemberMention(botChannelID, text string) (memberName, sessionName, body string, ok bool) {
	return parseMemberMention(h.stripLeadingBotMentions(botChannelID, text))
}

func stripLeadingMentions(text string) string {
	text = strings.TrimSpace(text)
	for strings.HasPrefix(text, "@_user_") {
		i := strings.IndexAny(text, " \t\r\n")
		if i <= 0 {
			break
		}
		text = strings.TrimSpace(text[i:])
	}
	return text
}

func (h *Hub) stripLeadingBotMentions(botChannelID, text string) string {
	text = stripLeadingMentions(text)
	for {
		if !strings.HasPrefix(text, "@") || strings.HasPrefix(text, "@_user_") {
			return text
		}
		raw := strings.TrimPrefix(text, "@")
		if raw == "" {
			return text
		}
		i := strings.IndexAny(raw, " \t\r\n")
		if i <= 0 {
			return text
		}
		name := strings.TrimSpace(raw[:i])
		if !h.isBotMention(botChannelID, name) {
			return text
		}
		text = strings.TrimSpace(raw[i:])
	}
}

func (h *Hub) isBotMention(botChannelID, name string) bool {
	name = strings.TrimSpace(strings.TrimPrefix(name, "@"))
	if name == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if strings.EqualFold(name, botChannelID) {
		return true
	}
	if strings.EqualFold(name, h.botNames[botChannelID]) {
		return true
	}
	for botName, id := range h.botChannels {
		if strings.EqualFold(name, botName) || strings.EqualFold(name, id) {
			return true
		}
	}
	return false
}

func (h *Hub) resolveMemberByName(ctx context.Context, name string) (model.Member, error) {
	members, err := h.Repo.ListMembers(ctx)
	if err != nil {
		return model.Member{}, err
	}
	var matches []model.Member
	for _, member := range members {
		if !member.Active {
			continue
		}
		if member.OwnerKey == name || member.DisplayName == name {
			matches = append(matches, member)
		}
	}
	if len(matches) == 0 {
		return model.Member{}, fmt.Errorf("未找到成员：%s", name)
	}
	if len(matches) > 1 {
		return model.Member{}, fmt.Errorf("成员名不唯一，请使用 owner_key：%s", name)
	}
	return matches[0], nil
}

func (h *Hub) resolveMemberSession(ctx context.Context, member model.Member, requestedSession string) (string, string, bool, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", "", false, err
	}
	type candidate struct {
		keyID       string
		session     string
		noDirectory bool
	}
	var candidates []candidate
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		if requestedSession != "" && ep.SessionName != requestedSession {
			continue
		}
		accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			return "", "", false, err
		}
		for _, account := range accounts {
			if accountBelongsToMember(account, member) {
				candidates = append(candidates, candidate{keyID: ep.KeyID, session: ep.SessionName, noDirectory: ep.NoDirectory})
				break
			}
		}
	}
	if len(candidates) == 0 {
		if requestedSession != "" {
			return "", "", false, fmt.Errorf("成员 %s 的会话 %s 不在线或未绑定", memberLabel(member), requestedSession)
		}
		return "", "", false, fmt.Errorf("成员 %s 暂无在线会话", memberLabel(member))
	}
	if requestedSession != "" {
		return candidates[0].keyID, candidates[0].session, candidates[0].noDirectory, nil
	}
	visible := candidates[:0]
	for _, c := range candidates {
		if !c.noDirectory {
			visible = append(visible, c)
		}
	}
	if len(visible) == 0 {
		return candidates[0].keyID, candidates[0].session, true, nil
	}
	if len(visible) > 1 {
		names := make([]string, 0, len(visible))
		for _, c := range visible {
			names = append(names, c.session)
		}
		return "", "", false, fmt.Errorf("成员 %s 有多个在线会话，请指定：@%s#<会话名>。可选：%s", memberLabel(member), member.OwnerKey, strings.Join(names, "、"))
	}
	return visible[0].keyID, visible[0].session, false, nil
}

func memberLabel(member model.Member) string {
	return firstNonEmpty(member.DisplayName, member.OwnerKey)
}

func parseMirrorCommand(text string) (action, sessionName string, ok bool) {
	fields := strings.Fields(strings.TrimSpace(text))
	if len(fields) != 3 || strings.ToLower(fields[0]) != "mirror" {
		return "", "", false
	}
	action = strings.ToLower(fields[1])
	if action != "on" && action != "off" {
		return "", "", false
	}
	sessionName = strings.TrimSpace(fields[2])
	if sessionName == "" || strings.Contains(sessionName, "#") {
		return "", "", false
	}
	return action, sessionName, true
}

func (h *Hub) lookupKeyIDForSession(chatEntityID, sessionName string) (string, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for keyID, sessions := range h.sessionClients {
		if sessions[sessionName] == nil {
			continue
		}
		if h.keyAccounts[keyID][chatEntityID] {
			return keyID, true
		}
	}
	return "", false
}

func (h *Hub) onlineSessionsForAccount(ctx context.Context, chatEntityID string) (string, []string, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", nil, err
	}
	keySessions := map[string][]string{}
	for _, ep := range endpoints {
		if !ep.Active {
			continue
		}
		accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			return "", nil, err
		}
		for _, account := range accounts {
			if account == chatEntityID {
				keySessions[ep.KeyID] = append(keySessions[ep.KeyID], ep.SessionName)
				break
			}
		}
	}
	if len(keySessions) == 0 {
		return "", nil, nil
	}
	if len(keySessions) == 1 {
		for keyID, sessions := range keySessions {
			sort.Strings(sessions)
			return keyID, sessions, nil
		}
	}
	var all []string
	for _, sessions := range keySessions {
		all = append(all, sessions...)
	}
	sort.Strings(all)
	return "", all, nil
}

func (h *Hub) lookupKeyIDForMirror(ctx context.Context, chatEntityID, sessionName string) (string, bool, error) {
	endpoints, err := h.Repo.ListSessionEndpoints(ctx)
	if err != nil {
		return "", false, err
	}
	for _, ep := range endpoints {
		if ep.SessionName != sessionName {
			continue
		}
		accounts, err := h.Repo.ListAPIKeyAccounts(ctx, ep.KeyID)
		if err != nil {
			return "", false, err
		}
		for _, account := range accounts {
			if account == chatEntityID {
				return ep.KeyID, true, nil
			}
		}
	}
	return "", false, nil
}

func (h *Hub) botChannel(botName string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if id := h.botChannels[botName]; id != "" {
		return id
	}
	return botName
}

func (h *Hub) botName(botChannelID string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if name := h.botNames[botChannelID]; name != "" {
		return name
	}
	return botChannelID
}

func sourceAccount(msg model.Message) string {
	if msg.ChatType == model.ChatGroup && msg.SenderOpenID != "" {
		return msg.BotChannelID + ":personal:" + msg.SenderOpenID
	}
	return msg.ChatEntityID
}

func feishuOpenIDFromAccount(chatEntityID string) string {
	parts := strings.Split(chatEntityID, ":")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	return chatEntityID
}

func accountEntityParts(chatEntityID string) (botChannelID, feishuID string, ok bool) {
	parts := strings.Split(chatEntityID, ":")
	if len(parts) < 3 || parts[0] == "" || parts[len(parts)-1] == "" {
		return "", "", false
	}
	return parts[0], parts[len(parts)-1], true
}

func accountBelongsToMember(account string, member model.Member) bool {
	if account == "" {
		return false
	}
	if account == member.OwnerKey {
		return true
	}
	if member.FeishuOpenID != "" && (account == member.FeishuOpenID || strings.HasSuffix(account, ":"+member.FeishuOpenID)) {
		return true
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func stringSet(items []string) map[string]bool {
	out := map[string]bool{}
	for _, item := range items {
		if item != "" {
			out[item] = true
		}
	}
	return out
}

func stringSetKeys(items map[string]bool) []string {
	if len(items) == 0 {
		return nil
	}
	out := make([]string, 0, len(items))
	for item := range items {
		if item != "" {
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func errorEnvelope(keyID, sessionName, sourceID, body string) model.Envelope {
	return model.Envelope{
		ID:   firstNonEmpty(sourceID, randomHex(16)),
		To:   sessionAddress(sessionName, keyID),
		From: sessionAddress("workpulse", keyID),
		Body: body,
		TS:   time.Now().Unix(),
		Meta: map[string]any{"error": true, "system": true, "no_mirror": true},
	}
}

func newKeyID(label, fallback string) string {
	base := sanitizeKeyIDPart(firstNonEmpty(label, fallback, "person"))
	return fmt.Sprintf("FB-%s-%s-%s", base, time.Now().UTC().Format("20060102"), randomHex(4))
}

func sanitizeKeyIDPart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteByte('-')
		}
		if b.Len() >= 24 {
			break
		}
	}
	if b.Len() == 0 {
		return "person"
	}
	return b.String()
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func randomDigits(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", n)
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = '0' + b[i]%10
	}
	return string(out)
}
