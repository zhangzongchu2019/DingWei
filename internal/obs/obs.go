// Package obs 是 Hub 进程内的有界观测登记处（DW123-PER-RELEASE-OBSERVABILITY 第一步）。
//
// 只收低基数聚合：route 模板／方法／状态类、WSS 生命周期、流的数量与时长、运行身份。
// 不收消息正文、prompt、cookie、token、DSN、签名、IP、原始路径或 query；标签值全部来自
// 有限集合（路由模板来自 mux 注册表，不来自请求）。
//
// 口径：
//   - 没有数据的项给 null 并写明原因，不造 0；计数里的 0 只表示「本进程启动以来真实观测到 0 次」。
//   - 序列有上限（MaxRouteSeries 含溢出桶），超出的样本进溢出桶并计 dropped，不影响业务请求。
//   - 快照时刻（snapshot_at）与进程起点、流持续时长分开给。
package obs

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Schema = "dw-obs/v1"

	// MaxRouteSeries 是短请求直方图的序列上限（含溢出桶）；spec first_phase_budget_ruling.maximum_route_metric_series。
	MaxRouteSeries = 64
	// MaxStreamSeries 是长连接（SSE／升级）按路由聚合的序列上限（含溢出桶）。
	MaxStreamSeries = 32

	overflowRoute = "_overflow"
	unmatched     = "_unmatched"
)

// 短请求延迟桶（毫秒，上界含）；最后一格是 +Inf。
var latencyBucketsMS = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000, 10000}

// 长连接持续时长桶（秒）。
var durationBucketsS = []float64{1, 10, 60, 300, 1800, 3600, 21600}

// keepalive 往返桶（毫秒）。
var rttBucketsMS = []float64{5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

type histogram struct {
	bounds []float64
	counts []uint64 // len(bounds)+1，最后一格为 +Inf
	count  uint64
	sum    float64
}

func newHistogram(bounds []float64) *histogram {
	return &histogram{bounds: bounds, counts: make([]uint64, len(bounds)+1)}
}

func (h *histogram) observe(v float64) {
	i := sort.SearchFloat64s(h.bounds, v)
	h.counts[i]++
	h.count++
	h.sum += v
}

// HistogramView 是直方图的 JSON 形状；le 与 counts 一一对应，最后一格 le 为 null（+Inf）。
type HistogramView struct {
	LE     []*float64 `json:"le"`
	Counts []uint64   `json:"counts"`
	Count  uint64     `json:"count"`
	Sum    float64    `json:"sum"`
}

func (h *histogram) view() HistogramView {
	le := make([]*float64, len(h.bounds)+1)
	for i := range h.bounds {
		b := h.bounds[i]
		le[i] = &b
	}
	counts := append([]uint64(nil), h.counts...)
	return HistogramView{LE: le, Counts: counts, Count: h.count, Sum: h.sum}
}

type routeKey struct {
	Server, Route, Method, Class string
}

type streamKey struct {
	Server, Route, Kind string
}

type streamStat struct {
	opened, closed uint64
	active         int64
	bytes          uint64
	duration       *histogram
}

type wsStat struct {
	opened  uint64
	active  int64
	closed  map[string]uint64
	pingOK  uint64
	pingErr uint64
	rtt     *histogram
}

// Identity 是运行身份。字符串为空表示 UNKNOWN（快照里给 null 加原因）。
type Identity struct {
	Service      string
	Version      string
	Commit       string
	ConfigSHA256 string
	ConfigScope  string // ConfigSHA256 覆盖的范围（写明不是全量配置）
	DeployID     string
}

// Registry 是一个进程内登记处。零值不可用，用 New。
type Registry struct {
	mu sync.Mutex

	now         func() time.Time
	started     time.Time // 墙钟 UTC，只用于展示
	startedMono time.Time // 带单调读数的原值，只用于算 uptime（墙钟跳变不影响）

	routes         map[routeKey]*histogram
	routeOverflow  map[string]*histogram // 按 server 分的溢出桶
	routeDropped   uint64                // 进入溢出桶的样本数
	streams        map[streamKey]*streamStat
	streamOverflow *streamStat
	streamDropped  uint64
	ws             map[string]*wsStat

	identity Identity

	artifactOnce sync.Once
	artifactSHA  string // 由后台协程写入，读写都在 mu 下
	artifactErr  string
	artifactPath func() (string, error)
}

// New 建一个登记处；now 为 nil 时用 time.Now。
func New(now func() time.Time) *Registry {
	if now == nil {
		now = time.Now
	}
	start := now()
	return &Registry{
		now:           now,
		started:       start.UTC(),
		startedMono:   start,
		routes:        map[routeKey]*histogram{},
		routeOverflow: map[string]*histogram{},
		streams:       map[streamKey]*streamStat{},
		ws:            map[string]*wsStat{},
		artifactPath:  os.Executable,
	}
}

// Default 是 Hub 进程用的登记处；hook 走包级函数，测试用 New 自建。
var Default = New(nil)

// SetIdentity 在启动时设一次运行身份。
// 同时在后台起一次制品 sha 计算（读可执行文件），不放在任何请求路径或锁内。
func (r *Registry) SetIdentity(id Identity) {
	r.mu.Lock()
	r.identity = id
	r.mu.Unlock()
	r.startArtifactHash()
}

// SanitizeDeployID 只接受 [A-Za-z0-9._:-]，长度 1–128；其他一律当 UNKNOWN（返回空串），防止环境变量把任意内容带进快照。
func SanitizeDeployID(v string) string {
	if len(v) == 0 || len(v) > 128 {
		return ""
	}
	for _, c := range v {
		ok := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '_' || c == ':' || c == '-'
		if !ok {
			return ""
		}
	}
	return v
}

// ConfigSHA256 对确定性的有效配置行求 sha256（不含 secret 的那一行由调用方保证）。
func ConfigSHA256(line string) string {
	sum := sha256.Sum256([]byte(line))
	return hex.EncodeToString(sum[:])
}

func (r *Registry) observeRoute(server, route, method, class string, ms float64) {
	k := routeKey{Server: server, Route: route, Method: method, Class: class}
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.routes[k]
	if h == nil {
		// 溢出桶本身占用序列名额：普通序列 + 已建溢出桶 < 上限 才允许新建。
		if len(r.routes)+len(r.routeOverflow)+1 > MaxRouteSeries-1 {
			o := r.routeOverflow[server]
			if o == nil {
				if len(r.routes)+len(r.routeOverflow) >= MaxRouteSeries {
					// 极端情况：名额已被其他 server 的溢出桶占满 ⇒ 记到任一已有溢出桶。
					for _, any := range r.routeOverflow {
						o = any
						break
					}
				}
				if o == nil {
					o = newHistogram(latencyBucketsMS)
					r.routeOverflow[server] = o
				}
			}
			o.observe(ms)
			r.routeDropped++
			return
		}
		h = newHistogram(latencyBucketsMS)
		r.routes[k] = h
	}
	h.observe(ms)
}

func (r *Registry) streamFor(server, route, kind string) *streamStat {
	k := streamKey{Server: server, Route: route, Kind: kind}
	s := r.streams[k]
	if s != nil {
		return s
	}
	if len(r.streams) >= MaxStreamSeries-1 {
		if r.streamOverflow == nil {
			r.streamOverflow = &streamStat{duration: newHistogram(durationBucketsS)}
		}
		r.streamDropped++
		return r.streamOverflow
	}
	s = &streamStat{duration: newHistogram(durationBucketsS)}
	r.streams[k] = s
	return s
}

func (r *Registry) streamOpened(server, route, kind string) *streamStat {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.streamFor(server, route, kind)
	s.opened++
	s.active++
	return s
}

func (r *Registry) streamClosed(s *streamStat, d time.Duration, bytes uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s.closed++
	s.active--
	s.bytes += bytes
	s.duration.observe(d.Seconds())
}

func (r *Registry) wsFor(kind string) *wsStat {
	s := r.ws[kind]
	if s == nil {
		s = &wsStat{closed: map[string]uint64{}, rtt: newHistogram(rttBucketsMS)}
		r.ws[kind] = s
	}
	return s
}

// WSConn 跟踪一条 WebSocket 的生命周期。nil 安全：未接线时所有方法无副作用。
type WSConn struct {
	r      *Registry
	kind   string
	opened time.Time
	reason atomic.Pointer[string]
	done   atomic.Bool
}

// OpenWS 在 Accept 成功后调用一次；kind 取有限集合（session、service、view、node）。
func (r *Registry) OpenWS(kind string) *WSConn {
	if !knownWSKind(kind) {
		kind = "other"
	}
	r.mu.Lock()
	s := r.wsFor(kind)
	s.opened++
	s.active++
	r.mu.Unlock()
	return &WSConn{r: r, kind: kind, opened: r.now()}
}

// SetLocalReason 记下 Hub 主动关闭的原因（取有限集合）；先到先得，之后的不覆盖。
func (c *WSConn) SetLocalReason(reason string) {
	if c == nil {
		return
	}
	if !knownLocalReason(reason) {
		reason = "local_other"
	}
	c.reason.CompareAndSwap(nil, &reason)
}

// Closed 在连接结束时调用一次（重复调用只记第一次）。readErr 是读循环的终止错误，可以为 nil。
func (c *WSConn) Closed(readErr error) {
	if c == nil || !c.done.CompareAndSwap(false, true) {
		return
	}
	reason := ""
	if p := c.reason.Load(); p != nil {
		reason = *p
	} else {
		reason = classifyReadErr(readErr)
	}
	c.r.mu.Lock()
	s := c.r.wsFor(c.kind)
	s.active--
	s.closed[reason]++
	c.r.mu.Unlock()
}

// Ping 记一次 keepalive 结果；err 非 nil 记为失败（RTT 不入直方图）。
func (c *WSConn) Ping(rtt time.Duration, err error) {
	if c == nil {
		return
	}
	c.r.mu.Lock()
	defer c.r.mu.Unlock()
	s := c.r.wsFor(c.kind)
	if err != nil {
		s.pingErr++
		return
	}
	s.pingOK++
	s.rtt.observe(float64(rtt) / float64(time.Millisecond))
}

func knownWSKind(k string) bool {
	switch k {
	case "session", "service", "view", "node":
		return true
	}
	return false
}

func knownLocalReason(r string) bool {
	switch r {
	case "keepalive_failed", "idle_timeout", "replaced", "revoked", "rejected", "server_shutdown":
		return true
	}
	return false
}

// closeStatuser 让本包不依赖具体 WebSocket 库：coder/websocket 的 CloseError 有 Code 字段，
// 调用方用 WSCloseCode 把它转成整数后传 PeerClose 错误。
type peerCloseError struct{ code int }

func (e peerCloseError) Error() string { return "peer close" }

// PeerClose 包装对端关闭码，供 Closed 分类（调用方从库错误里取码，取不到就传原错误）。
func PeerClose(code int) error { return peerCloseError{code: code} }

func classifyReadErr(err error) string {
	if err == nil {
		return "ended"
	}
	var pc peerCloseError
	if errors.As(err, &pc) {
		switch {
		case pc.code == 1000:
			return "peer_normal"
		case pc.code == 1001:
			return "peer_going_away"
		case pc.code >= 4000 && pc.code < 5000:
			return "peer_app_code"
		default:
			return "peer_other_code"
		}
	}
	if errors.Is(err, io.EOF) {
		return "network_eof"
	}
	if errors.Is(err, errContextDone) {
		return "request_ctx_done"
	}
	return "network_error"
}

var errContextDone = errors.New("obs: request context done")

// ContextDone 是调用方在「请求上下文结束」而非读错误时传给 Closed 的哨兵。
func ContextDone() error { return errContextDone }

// ---------------------------------------------------------------- 快照

type Snapshot struct {
	Schema           string    `json:"schema"`
	SnapshotAt       time.Time `json:"snapshot_at"`
	ProcessStartedAt time.Time `json:"process_started_at"`
	UptimeSeconds    float64   `json:"uptime_seconds"`

	Identity IdentityView `json:"identity"`

	HTTP    HTTPView    `json:"http"`
	Streams StreamsView `json:"streams"`
	WS      []WSView    `json:"ws"`

	// NotInstrumented 列出本版明确没有埋点的面；不是 0，也不是 PASS。
	NotInstrumented []NotInstrumented `json:"not_instrumented"`
}

type IdentityView struct {
	Service        string            `json:"service"`
	Version        *string           `json:"version"`
	Commit         *string           `json:"commit"`
	ArtifactSHA256 *string           `json:"artifact_sha256"`
	ConfigSHA256   *string           `json:"config_sha256"`
	ConfigScope    string            `json:"config_scope"`
	DeployID       *string           `json:"deploy_id"`
	Unknown        map[string]string `json:"unknown,omitempty"` // 字段名 → UNKNOWN 的原因
}

type HTTPView struct {
	MaxSeries      int              `json:"max_series"`
	Series         []RouteSeries    `json:"series"`
	Overflow       []OverflowSeries `json:"overflow"`
	DroppedSamples uint64           `json:"dropped_samples"`
	Note           string           `json:"note"`
}

type RouteSeries struct {
	Server    string        `json:"server"`
	Route     string        `json:"route"`
	Method    string        `json:"method"`
	Class     string        `json:"class"`
	LatencyMS HistogramView `json:"latency_ms"`
}

type OverflowSeries struct {
	Server    string        `json:"server"`
	LatencyMS HistogramView `json:"latency_ms"`
}

type StreamsView struct {
	MaxSeries      int            `json:"max_series"`
	Series         []StreamSeries `json:"series"`
	Overflow       *StreamSeries  `json:"overflow"`
	DroppedSamples uint64         `json:"dropped_samples"`
	Note           string         `json:"note"`
}

type StreamSeries struct {
	Server    string        `json:"server"`
	Route     string        `json:"route"`
	Kind      string        `json:"kind"`
	Opened    uint64        `json:"opened"`
	Closed    uint64        `json:"closed"`
	Active    int64         `json:"active"`
	Bytes     uint64        `json:"bytes_written_closed"`
	DurationS HistogramView `json:"duration_s"`
}

type WSView struct {
	Kind     string            `json:"kind"`
	Opened   uint64            `json:"opened"`
	Active   int64             `json:"active"`
	Closed   map[string]uint64 `json:"closed_by_reason"`
	PingOK   uint64            `json:"ping_ok"`
	PingFail uint64            `json:"ping_fail"`
	PingRTT  HistogramView     `json:"ping_rtt_ms"`
}

type NotInstrumented struct {
	Area   string `json:"area"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// startArtifactHash 只启动一次后台计算；结果在 mu 下发布。读文件失败只记 UNKNOWN 原因，不影响任何请求。
func (r *Registry) startArtifactHash() {
	r.artifactOnce.Do(func() {
		r.mu.Lock()
		r.artifactErr = "UNKNOWN: artifact hash pending"
		r.mu.Unlock()
		go func() {
			sum, reason := hashExecutable(r.artifactPath)
			r.mu.Lock()
			r.artifactSHA, r.artifactErr = sum, reason
			r.mu.Unlock()
		}()
	})
}

func hashExecutable(path func() (string, error)) (string, string) {
	p, err := path()
	if err != nil {
		return "", "UNKNOWN: executable path unavailable"
	}
	f, err := os.Open(p)
	if err != nil {
		return "", "UNKNOWN: executable not readable"
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", "UNKNOWN: executable read failed"
	}
	return hex.EncodeToString(h.Sum(nil)), ""
}

// Snapshot 返回当前快照（深拷贝，可直接序列化）。
func (r *Registry) Snapshot() Snapshot {
	r.startArtifactHash() // 没调 SetIdentity 的登记处也会起后台计算；本次快照不等它
	raw := r.now()        // 带单调读数：uptime 用它与 startedMono 相减
	now := raw.UTC()      // .UTC() 会去掉单调读数，只作展示用的墙钟

	// 锁内只复制内存里的计数，不做 I/O；编码在锁外（SnapshotHandler）。
	r.mu.Lock()
	defer r.mu.Unlock()
	artSHA, artErr := r.artifactSHA, r.artifactErr

	id := r.identity
	unknown := map[string]string{}
	iv := IdentityView{
		Service:        id.Service,
		Version:        strPtr(id.Version),
		Commit:         strPtr(id.Commit),
		ArtifactSHA256: strPtr(artSHA),
		ConfigSHA256:   strPtr(id.ConfigSHA256),
		DeployID:       strPtr(id.DeployID),
		ConfigScope:    id.ConfigScope,
	}
	if iv.Version == nil {
		unknown["version"] = "UNKNOWN: not set at startup"
	}
	if iv.Commit == nil || id.Commit == "unknown" {
		iv.Commit = nil
		unknown["commit"] = "UNKNOWN: build did not inject commit"
	}
	if iv.ArtifactSHA256 == nil {
		unknown["artifact_sha256"] = artErr
	}
	if iv.ConfigSHA256 == nil {
		unknown["config_sha256"] = "UNKNOWN: effective config line not registered"
	}
	if iv.DeployID == nil {
		unknown["deploy_id"] = "UNKNOWN: WP_DEPLOY_ID not set or not [A-Za-z0-9._:-]{1,128}"
	}
	if len(unknown) > 0 {
		iv.Unknown = unknown
	}

	hv := HTTPView{
		MaxSeries:      MaxRouteSeries,
		Series:         []RouteSeries{},
		Overflow:       []OverflowSeries{},
		DroppedSamples: r.routeDropped,
		Note:           "short requests only; upgraded (101) and text/event-stream responses are counted under streams",
	}
	for k, h := range r.routes {
		hv.Series = append(hv.Series, RouteSeries{Server: k.Server, Route: k.Route, Method: k.Method, Class: k.Class, LatencyMS: h.view()})
	}
	sort.Slice(hv.Series, func(i, j int) bool {
		a, b := hv.Series[i], hv.Series[j]
		if a.Server != b.Server {
			return a.Server < b.Server
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		if a.Method != b.Method {
			return a.Method < b.Method
		}
		return a.Class < b.Class
	})
	for server, h := range r.routeOverflow {
		hv.Overflow = append(hv.Overflow, OverflowSeries{Server: server, LatencyMS: h.view()})
	}
	sort.Slice(hv.Overflow, func(i, j int) bool { return hv.Overflow[i].Server < hv.Overflow[j].Server })

	sv := StreamsView{MaxSeries: MaxStreamSeries, Series: []StreamSeries{}, DroppedSamples: r.streamDropped,
		Note: "duration is observed when a stream ends; active streams contribute only to active"}
	for k, s := range r.streams {
		sv.Series = append(sv.Series, StreamSeries{Server: k.Server, Route: k.Route, Kind: k.Kind,
			Opened: s.opened, Closed: s.closed, Active: s.active, Bytes: s.bytes, DurationS: s.duration.view()})
	}
	sort.Slice(sv.Series, func(i, j int) bool {
		a, b := sv.Series[i], sv.Series[j]
		if a.Server != b.Server {
			return a.Server < b.Server
		}
		if a.Route != b.Route {
			return a.Route < b.Route
		}
		return a.Kind < b.Kind
	})
	if o := r.streamOverflow; o != nil {
		sv.Overflow = &StreamSeries{Server: "*", Route: overflowRoute, Kind: "*", Opened: o.opened, Closed: o.closed,
			Active: o.active, Bytes: o.bytes, DurationS: o.duration.view()}
	}

	wv := []WSView{}
	for kind, s := range r.ws {
		closed := map[string]uint64{}
		for k, v := range s.closed {
			closed[k] = v
		}
		wv = append(wv, WSView{Kind: kind, Opened: s.opened, Active: s.active, Closed: closed,
			PingOK: s.pingOK, PingFail: s.pingErr, PingRTT: s.rtt.view()})
	}
	sort.Slice(wv, func(i, j int) bool { return wv[i].Kind < wv[j].Kind })

	return Snapshot{
		Schema:           Schema,
		SnapshotAt:       now,
		ProcessStartedAt: r.started,
		UptimeSeconds:    raw.Sub(r.startedMono).Seconds(),
		Identity:         iv,
		HTTP:             hv,
		Streams:          sv,
		WS:               wv,
		NotInstrumented:  notInstrumented(),
	}
}

func notInstrumented() []NotInstrumented {
	return []NotInstrumented{
		{Area: "feishu", Status: "NOT_INSTRUMENTED", Reason: "outbound attempt stages and ingress counters not wired in this candidate"},
		{Area: "view_service", Status: "NOT_INSTRUMENTED", Reason: "python view-service has no metrics; SSE relay timing not wired"},
		{Area: "browser_report", Status: "NOT_INSTRUMENTED", Reason: "no client report endpoint in this candidate"},
		{Area: "nginx", Status: "NOT_INSTRUMENTED", Reason: "owned by ops; not part of Hub process"},
		{Area: "collector", Status: "NOT_INSTRUMENTED", Reason: "owned by ops; this snapshot is in-memory since process start, not persisted"},
	}
}
