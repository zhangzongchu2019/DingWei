// Package fanout 实现 T17 飞书消息 fan-out 代理(设计 docs/t17-feishu-fanout-proxy-design-v1.md,冻结 1b67eec)。
//
// 入站:POST /webhook/{channel} 收飞书原始事件——生产路原样字节同步转发(生产 Hub 响应
// 即对飞书的响应=权威);测试路仅白名单命中,异步 best-effort(有界队列,满丢最旧)。
// 解密仅用于读 sender open_id 判白名单,转发不改写字节。
// 出站:POST /outbound 收测试 Hub 的 OutMessage,追加[测试环境]后缀(幂等)后经注入的
// Sender(生产=LarkGateway)发飞书;bearer 鉴权+审计日志(prodops 注①)。
// 降级铁律:生产投递永不被测试侧阻塞;白名单/通道配置坏=fail-closed 只走生产。
package fanout

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

const TestEnvSuffix = "[测试环境]"

// ChannelConfig 每个 bot 通道的验签/解密配置(与 Hub 侧 bot_channel 同源同值)。
type ChannelConfig struct {
	EncryptKey        string `json:"encrypt_key"`
	VerificationToken string `json:"verification_token"`
}

// Whitelist 白名单(需求③):open_id 或 chat_id 命中即进测试路。
type Whitelist struct {
	OpenIDs []string `json:"open_ids"`
	ChatIDs []string `json:"chat_ids"`
}

// Sender 出站发送抽象(生产=feishu.LarkGateway;测试注入 recorder)。
type Sender interface {
	Send(ctx context.Context, out feishu.OutMessage) (string, error)
}

// Config 代理配置(全 env/文件,无库,见设计 §5)。
type Config struct {
	ProdURL       string                   // 生产 Hub base URL(权威路)
	TestURL       string                   // 测试 Hub base URL(best-effort 路)
	Channels      map[string]ChannelConfig // channel → 验签/解密(encrypt_key 旋转=热重载本表,shence 注)
	WhitelistFile string
	OutboundToken string // /outbound bearer(legacy 无 v 路;v:1 托管路用 Callers)
	QueueSize     int    // 测试路队列,默认 1024
	ForwardTO     time.Duration
	TestTO        time.Duration
	Logger        *slog.Logger
	Sender        Sender // nil=出站禁用(healthz 报 degraded);Bots 空时的退化单发送器

	// v2.3 托管出站(§C):caller token→身份/后缀策略;bot_channel→发送器;spool 路径。
	Callers     map[string]CallerConfig // token→caller;空=托管路禁用(503)
	Bots        map[string]Sender       // bot_channel→Sender;空=退化用 Sender
	SpoolPath   string                  // 空=不开 spool(v:1 请求得 503)
	MaxAttempts int                     // 终败入 DLQ 阈值,默认 8

	// v2.3 入站(§A/§B):接入实例+N 路 sink;ReceiverFactory 测试可注入。
	Accesses              []AccessConfig
	Sinks                 []SinkConfig
	ReceiverFactory       ReceiverFactory
	BesteffortMaxAttempts int // besteffort 有界重试,默认 5,到界=dropped

	// v2.3 配置热载+管理面(§E/§F)。
	ConfigPath       string                   // fanout.yaml;空=纯 Config 注入(测试)
	AdminTokens      map[string]AdminIdentity // 空=admin 端点全 401
	AdminAllowRemote bool                     // false=仅 loopback(§F 默认)
	SkipPreArm       bool                     // 测试用;生产恒 false
	DiskMinFreePct   float64                  // pre-arm④,默认 10
	ReplayWatermark  int                      // DLQ replay 水位门,默认 1000
	SenderFactory    SenderFactory            // outbound_bots→Sender;默认真 LarkGateway
	TestInject       bool                     // 仅测试(G9 e2e):/_test/inject 驱动 ws 管道;生产恒 false
}

// Metrics 计数器(/metrics 输出,设计 §3 可观测)。
type Metrics struct {
	ProdForwarded, ProdFailed          atomic.Int64
	TestEnqueued, TestSent, TestFailed atomic.Int64
	TestDroppedFull, WhitelistMiss     atomic.Int64
	DecryptFailed, OutboundSent        atomic.Int64
	OutboundAuthFailed                 atomic.Int64
	OutboundQueued, OutboundDLQ        atomic.Int64 // v2.3 托管路(§C)
	InboundAccepted, SinkDelivered     atomic.Int64 // v2.3 入站(§A/§B)
	SinkDLQ, BesteffortDropped         atomic.Int64
}

type queuedEvent struct {
	path        string
	contentType string
	body        []byte
}

// Server fan-out 代理主体。用 New 构造,ServeHTTP 即 http.Handler。
type Server struct {
	cfg     Config
	log     *slog.Logger
	metrics Metrics
	client  *http.Client

	mu        sync.RWMutex
	whitelist Whitelist
	wlOK      bool // fail-closed 旗标:白名单不可用=测试路全停
	channels  map[string]ChannelConfig

	queue  chan queuedEvent
	seenMu sync.Mutex
	seen   map[string]struct{} // 测试路 event_id 去重(LRU 粗化:满即清)
	prodOK atomic.Bool         // startup/周期探测生产 Hub 可达(prodops 注②)
	testOK atomic.Bool
	mux    *http.ServeMux
	spool  *Spool // v2.3 托管 spool(§D);nil=托管路 503

	// v2.3 热载动态表(§E:s.mu 保护,原子表交换):
	dynSinks   []SinkConfig
	dynCallers map[string]CallerConfig
	dynBots    map[string]Sender
	fileCfg    *FileConfig

	accessMu     sync.Mutex
	accessCancel map[string]context.CancelFunc // bot_channel→ws 接入停止
}

func New(cfg Config) *Server {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 1024
	}
	if cfg.ForwardTO <= 0 {
		cfg.ForwardTO = 10 * time.Second
	}
	if cfg.TestTO <= 0 {
		cfg.TestTO = 3 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	s := &Server{
		cfg:          cfg,
		log:          cfg.Logger,
		client:       &http.Client{},
		channels:     cfg.Channels,
		queue:        make(chan queuedEvent, cfg.QueueSize),
		seen:         make(map[string]struct{}),
		dynSinks:     cfg.Sinks,
		dynCallers:   cfg.Callers,
		dynBots:      cfg.Bots,
		accessCancel: map[string]context.CancelFunc{},
	}
	s.reloadWhitelist()
	if cfg.SpoolPath != "" {
		sp, err := OpenSpool(cfg.SpoolPath)
		if err != nil {
			// fail-closed(§B):spool 起不来=托管路 503,不静默降级为立发
			cfg.Logger.Error("spool打开失败,托管路禁用", "path", cfg.SpoolPath, "error", err)
		} else if err := sp.initInbound(); err != nil {
			cfg.Logger.Error("入站表初始化失败,spool禁用", "error", err)
			_ = sp.Close()
		} else {
			s.spool = sp
		}
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /webhook/{channel}", s.handleInbound)
	s.mux.HandleFunc("POST /outbound", s.handleOutbound)
	s.mux.HandleFunc("GET /outbound/{id}", s.handleOutboundStatus)
	s.mux.HandleFunc("POST /resolve", s.handleResolve)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /admin/config", s.handleAdminConfigGet)
	s.mux.HandleFunc("PUT /admin/config", s.handleAdminConfigPut)
	s.mux.HandleFunc("POST /admin/reload", s.handleAdminReload)
	s.mux.HandleFunc("POST /admin/dlq/replay", s.handleAdminDLQReplay)
	s.mux.HandleFunc("POST /admin/dlq/waive", s.handleAdminDLQWaive)
	if cfg.TestInject { // G9 e2e 专用面(默认关);生产配置校验不会开它
		cfg.Logger.Warn("TEST-ONLY inject endpoint enabled (/_test/inject) — 勿在生产开启")
		s.mux.HandleFunc("POST /_test/inject", s.handleTestInject)
	}
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

// Start 启动后台件:startup 依赖检查(prodops 注②:先验生产 Hub 可达)、测试路 worker、
// 白名单/通道周期重载(shence 注:encrypt_key 旋转走同一重载)。
func (s *Server) Start(ctx context.Context) {
	s.probe() // 同步先探一轮,healthz 起点即真值
	go s.testWorker(ctx)
	go s.periodic(ctx)
	go s.custodyWorker(ctx) // v2.3 托管发送循环(spool nil 时自退出)
	go s.sinkWorker(ctx)    // v2.3 入站 N 路投递循环
	if err := s.StartAccesses(ctx); err != nil {
		s.log.Error("接入启动失败(告警)", "error", err)
	}
}

// ---- 入站(需求①③) ----

func (s *Server) handleInbound(w http.ResponseWriter, r *http.Request) {
	channel := r.PathValue("channel")
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	contentType := r.Header.Get("Content-Type")

	// 测试路判定先做(纯内存,不阻塞),投递在生产路成功后
	testCopy := s.shouldCopyToTest(channel, raw)

	// 生产路:原样字节同步转发,响应原样中继=权威(设计 §2)
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.ForwardTO)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(s.cfg.ProdURL, "/")+"/webhook/"+channel, bytes.NewReader(raw))
	if err != nil {
		http.Error(w, "bad prod url", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := s.client.Do(req)
	if err != nil {
		s.metrics.ProdFailed.Add(1)
		s.prodOK.Store(false)
		// 生产不可达:对飞书 5xx=飞书原生重推;测试路本次不投(设计 §3)
		http.Error(w, "prod hub unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	s.metrics.ProdForwarded.Add(1)
	s.prodOK.Store(true)
	for _, h := range []string{"Content-Type"} {
		if v := resp.Header.Get(h); v != "" {
			w.Header().Set(h, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)

	// 测试路:异步入队(生产 2xx 才投,避免测试先于生产看到;challenge 已被 shouldCopy 排除)
	if testCopy && resp.StatusCode < 300 {
		select {
		case s.queue <- queuedEvent{path: "/webhook/" + channel, contentType: contentType, body: raw}:
			s.metrics.TestEnqueued.Add(1)
		default: // 满丢最旧(设计 §3)
			select {
			case <-s.queue:
				s.metrics.TestDroppedFull.Add(1)
			default:
			}
			select {
			case s.queue <- queuedEvent{path: "/webhook/" + channel, contentType: contentType, body: raw}:
				s.metrics.TestEnqueued.Add(1)
			default:
				s.metrics.TestDroppedFull.Add(1)
			}
		}
	}
}

// shouldCopyToTest 白名单判定:解密仅为读 sender;任何一步失败=fail-closed 不进测试路。
func (s *Server) shouldCopyToTest(channel string, raw []byte) bool {
	s.mu.RLock()
	wlOK, wl := s.wlOK, s.whitelist
	ch, chOK := s.channels[channel]
	s.mu.RUnlock()
	if !wlOK || !chOK || s.cfg.TestURL == "" {
		return false // fail-closed:名单/通道配置不可用=只走生产(设计 §3/§5)
	}
	data, err := decodeWebhookPayload(raw, ch.EncryptKey)
	if err != nil {
		s.metrics.DecryptFailed.Add(1) // 旋转窗口内解密失败按 fail-closed(shence 注)
		return false
	}
	if err := verifyWebhookToken(data, ch.VerificationToken); err != nil {
		return false
	}
	if webhookChallenge(data) != "" {
		return false // challenge 由生产路权威应答,测试路不需要(设计 §2)
	}
	openID, chatID, eventID := senderFields(data)
	hit := contains(wl.OpenIDs, openID) || contains(wl.ChatIDs, chatID)
	if !hit {
		s.metrics.WhitelistMiss.Add(1)
		return false
	}
	if eventID != "" && s.seenBefore(eventID) {
		return false // 测试路 event_id 去重(防代理自身重试重复注入)
	}
	return true
}

func (s *Server) seenBefore(eventID string) bool {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if _, ok := s.seen[eventID]; ok {
		return true
	}
	if len(s.seen) >= 4096 {
		s.seen = make(map[string]struct{})
	}
	s.seen[eventID] = struct{}{}
	return false
}

func (s *Server) testWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case ev := <-s.queue:
			tctx, cancel := context.WithTimeout(ctx, s.cfg.TestTO)
			req, err := http.NewRequestWithContext(tctx, http.MethodPost,
				strings.TrimRight(s.cfg.TestURL, "/")+ev.path, bytes.NewReader(ev.body))
			if err == nil {
				req.Header.Set("Content-Type", ev.contentType)
				resp, derr := s.client.Do(req)
				if derr == nil {
					_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
					resp.Body.Close()
					err = derr
					if resp.StatusCode >= 300 {
						err = fmt.Errorf("test hub status %d", resp.StatusCode)
					}
				} else {
					err = derr
				}
			}
			cancel()
			if err != nil {
				s.metrics.TestFailed.Add(1)
				s.testOK.Store(false)
				s.log.Warn("test路投递失败(best-effort,生产不受影响)", "error", err)
			} else {
				s.metrics.TestSent.Add(1)
				s.testOK.Store(true)
			}
		}
	}
}

// ---- 出站(需求②,prodops 注①审计) ----

func (s *Server) handleOutbound(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		http.Error(w, "read", http.StatusBadRequest)
		return
	}
	// v 判别(与 K1 条件严格同哲学):含 "v" 字段=v2.3 托管契约;无=legacy 立发路。
	var probe struct {
		V *int `json:"v"`
	}
	if json.Unmarshal(body, &probe) == nil && probe.V != nil {
		s.handleOutboundCustody(w, r, body)
		return
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))
	if tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "); s.cfg.OutboundToken == "" ||
		subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.OutboundToken)) != 1 {
		s.metrics.OutboundAuthFailed.Add(1)
		s.log.Warn("outbound鉴权失败(审计)", "remote", r.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if s.cfg.Sender == nil {
		http.Error(w, "outbound disabled", http.StatusServiceUnavailable)
		return
	}
	var out feishu.OutMessage
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&out); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	out.Text = AppendTestSuffix(out.Text)
	msgID, err := s.cfg.Sender.Send(r.Context(), out)
	// 审计日志:凭据每次使用都留痕(prodops 注①:remote/bot/to/长度/结果)
	s.log.Info("outbound审计", "remote", r.RemoteAddr, "bot_channel", out.BotChannelID,
		"to_id", out.ToID, "to_name", out.ToName, "text_len", len(out.Text),
		"msg_id", msgID, "error", errStr(err))
	if err != nil {
		http.Error(w, "send failed", http.StatusBadGateway) // 测试 Hub 按其出站队列重试(设计 §3)
		return
	}
	s.metrics.OutboundSent.Add(1)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"msg_id": msgID})
}

// AppendTestSuffix 文本尾部恰一个[测试环境](幂等,F3)。
func AppendTestSuffix(text string) string {
	if strings.HasSuffix(strings.TrimRight(text, " \n"), TestEnvSuffix) {
		return text
	}
	return text + TestEnvSuffix
}

// ---- healthz / metrics / 重载 ----

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	wlOK := s.wlOK
	s.mu.RUnlock()
	st := map[string]any{
		"prod_reachable": s.prodOK.Load(),
		"test_reachable": s.testOK.Load(),
		"whitelist_ok":   wlOK,
		"outbound":       s.cfg.Sender != nil,
	}
	// §B/T2:blocked_dlq>0=degraded(waive/replay 后恢复 ok)
	if s.spool != nil {
		if blocked, err := s.spool.CountBlocked(r.Context()); err == nil {
			st["blocked_dlq"] = blocked
			st["degraded"] = blocked > 0
		}
	}
	w.Header().Set("Content-Type", "application/json")
	// startup 依赖检查(prodops 注②):生产 Hub 不可达=503,让编排/探针挡住上线
	if !s.prodOK.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	_ = json.NewEncoder(w).Encode(st)
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	m := &s.metrics
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int64{
		"prod_forwarded": m.ProdForwarded.Load(), "prod_failed": m.ProdFailed.Load(),
		"test_enqueued": m.TestEnqueued.Load(), "test_sent": m.TestSent.Load(),
		"test_failed": m.TestFailed.Load(), "test_dropped_queue_full": m.TestDroppedFull.Load(),
		"whitelist_miss": m.WhitelistMiss.Load(), "decrypt_failed": m.DecryptFailed.Load(),
		"outbound_sent": m.OutboundSent.Load(), "outbound_auth_failed": m.OutboundAuthFailed.Load(),
		"outbound_queued": m.OutboundQueued.Load(), "outbound_dlq": m.OutboundDLQ.Load(),
		"inbound_accepted": m.InboundAccepted.Load(), "sink_delivered": m.SinkDelivered.Load(),
		"sink_dlq": m.SinkDLQ.Load(), "besteffort_dropped": m.BesteffortDropped.Load(),
	})
}

func (s *Server) probe() {
	for url, flag := range map[string]*atomic.Bool{s.cfg.ProdURL: &s.prodOK, s.cfg.TestURL: &s.testOK} {
		if url == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(url, "/")+"/readyz", nil)
		resp, err := s.client.Do(req)
		ok := false
		if err == nil {
			ok = resp.StatusCode < 500
			resp.Body.Close()
		}
		flag.Store(ok)
		cancel()
	}
}

func (s *Server) periodic(ctx context.Context) {
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.probe()
			s.reloadWhitelist()
		}
	}
}

// Reload 热重载白名单(SIGHUP 或周期;encrypt_key 旋转经 SetChannels 或进程重启,见设计 §5)。
func (s *Server) Reload() { s.reloadWhitelist() }

// SetChannels 热更新通道验签/解密配置(encrypt_key 旋转,shence 注)。
func (s *Server) SetChannels(ch map[string]ChannelConfig) {
	s.mu.Lock()
	s.channels = ch
	s.mu.Unlock()
}

func (s *Server) reloadWhitelist() {
	if s.cfg.WhitelistFile == "" {
		s.mu.Lock()
		s.wlOK, s.whitelist = false, Whitelist{}
		s.mu.Unlock()
		return
	}
	data, err := os.ReadFile(s.cfg.WhitelistFile)
	var wl Whitelist
	if err == nil {
		err = json.Unmarshal(data, &wl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		if s.wlOK {
			s.log.Warn("白名单不可用=fail-closed测试路全停(设计§3)", "error", err)
		}
		s.wlOK, s.whitelist = false, Whitelist{}
		return
	}
	s.wlOK, s.whitelist = true, wl
}

// ---- 飞书 payload 工具(与 Hub cmd/workpulse 同语义;解密仅用于白名单判定) ----

func decodeWebhookPayload(raw []byte, encryptKey string) ([]byte, error) {
	if strings.TrimSpace(encryptKey) == "" {
		return raw, nil
	}
	var envelope struct {
		Encrypt string `json:"encrypt"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Encrypt == "" {
		return nil, errors.New("encrypted webhook missing encrypt field")
	}
	ciphertext, err := base64.StdEncoding.DecodeString(envelope.Encrypt)
	if err != nil {
		return nil, err
	}
	if len(ciphertext) < aes.BlockSize*2 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("invalid encrypted webhook length")
	}
	sum := sha256.Sum256([]byte(encryptKey))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	plain := make([]byte, len(ciphertext)-aes.BlockSize)
	cipher.NewCBCDecrypter(block, ciphertext[:aes.BlockSize]).CryptBlocks(plain, ciphertext[aes.BlockSize:])
	if pad := int(plain[len(plain)-1]); pad > 0 && pad <= aes.BlockSize && pad <= len(plain) {
		plain = plain[:len(plain)-pad]
	}
	return plain, nil
}

func verifyWebhookToken(data []byte, want string) error {
	want = strings.TrimSpace(want)
	if want == "" {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return err
	}
	got := stringField(root, "token")
	if got == "" {
		if header, ok := root["header"].(map[string]any); ok {
			got = stringField(header, "token")
		}
	}
	if got != want {
		return errors.New("verification token mismatch")
	}
	return nil
}

func webhookChallenge(data []byte) string {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return ""
	}
	return stringField(root, "challenge")
}

func senderFields(data []byte) (openID, chatID, eventID string) {
	var root struct {
		Header struct {
			EventID string `json:"event_id"`
		} `json:"header"`
		Event struct {
			Sender struct {
				SenderID struct {
					OpenID string `json:"open_id"`
				} `json:"sender_id"`
			} `json:"sender"`
			Message struct {
				ChatID string `json:"chat_id"`
			} `json:"message"`
		} `json:"event"`
	}
	if err := json.Unmarshal(data, &root); err != nil {
		return "", "", ""
	}
	return root.Event.Sender.SenderID.OpenID, root.Event.Message.ChatID, root.Header.EventID
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func contains(list []string, v string) bool {
	if v == "" {
		return false
	}
	for _, item := range list {
		if item == v {
			return true
		}
	}
	return false
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
