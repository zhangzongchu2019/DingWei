package nodeagent

// T-G4-12 · Docker 指令集：pull/create/stop/start/restart/upgrade/destroy。
//
// 白名单纪律（设计件第三节，prodops 复验硬要求）：
//  · 参数严格解码（DisallowUnknownFields）——env/mount/network/capability 等任意字段一律拒；
//  · 镜像只接固定仓库＋sha256 digest；
//  · 命令绑定 seat：容器名/网络名/配额项目名一律由 seat_id 派生，参数不得指定；
//  · 凭据永不经 env——令牌以文件（0600）写入席位目录（B7）；
//  · 安全参数清单固定：非 root UID、no-new-privileges、cap drop ALL、只读 rootfs＋带限 tmpfs、
//    禁 host PID/IPC/network（默认私有）、每席独立网络与 XFS 项目配额 bind 目录；
//  · create 失败即清理不留半成品（V6-B3）；升级先起新容器健康后再删旧（V8-D2）。

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"
)

var (
	ErrImageRef     = errors.New("image ref 必须是固定仓库＋sha256 digest")
	ErrParams       = errors.New("参数不合白名单")
	ErrSeatNotFound = errors.New("席位容器不存在")
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// seatCLIs 席位可选的 CLI——与 sessionHelper 的 CLI 档（tools/sessionhelper/sessionhelper/cli.py CLI_PROFILES）同名；
// 其它值 sessionHelper 会静默回落到 claude 档，故在节点代理处即拒。
var seatCLIs = map[string]bool{"claude": true, "codex": true, "aider": true, "opencode": true, "cline": true, "gemini": true}

func validateImageRef(ref, registry string) error {
	// 形态二（组长 a26a825f528a）：local@sha256:<64 位 image ID>——本机构建本机用，免 pull；
	// 钉扎从「固定仓库」换成「固定 ID 白名单」：节点只接受配置里登记过的 ID
	// （NODEAGENT_LOCAL_IMAGES，与板上 HOSTED-IMAGE-VERSIONS 同源），且 create 前 inspect
	// 读出的 ID 必须与 ref 逐字相等。
	if repo, digest, ok := strings.Cut(ref, "@"); ok && repo == "local" {
		if !digestPattern.MatchString(digest) {
			return fmt.Errorf("%w: local 形态的 ID 须 64 位 hex: %q", ErrImageRef, ref)
		}
		return nil
	}
	repo, digest, ok := strings.Cut(ref, "@")
	if !ok || !strings.HasPrefix(repo, registry+"/") || !digestPattern.MatchString(digest) {
		return fmt.Errorf("%w: %q", ErrImageRef, ref)
	}
	return nil
}

// checkLocalImage local 形态闸（三条指令共用）：白名单＋inspect 读出 ID 逐字相等。
// 复验关注点：任何一条路径都不得让未登记的本机镜像被用上。
func (d *dockerOps) checkLocalImage(ctx context.Context, ref string) (bool, error) {
	id := localImageID(ref)
	if id == "" {
		return false, nil // 非 local 形态，不管
	}
	if !d.localImages[id] {
		return false, fmt.Errorf("%w: 未登记的本机 image ID", ErrImageRef)
	}
	got, err := d.eng.imageInspectID(ctx, id)
	if err != nil {
		return false, fmt.Errorf("本机镜像不存在: %v", err)
	}
	if got != id {
		return false, fmt.Errorf("%w: inspect 读出 %q 与 ref 逐字不等", ErrImageRef, got)
	}
	return true, nil
}

// localImageID 取出 local 形态的 image ID（64 位裸 hex；不是 local 形态 ⇒ ""）。
// 白名单与 inspect 比对都用裸 hex（引擎的 Id 常带 sha256: 前缀，docker.go 剥）。
func localImageID(ref string) string {
	repo, digest, ok := strings.Cut(ref, "@")
	if !ok || repo != "local" {
		return ""
	}
	return strings.TrimPrefix(digest, "sha256:")
}

// strictDecode 白名单解码：任何未知字段（env/mount/network/capability/…）一律拒。
func strictDecode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrParams, err)
	}
	return nil
}

type dockerOps struct {
	eng          *engine
	policy       NodePolicy
	soft         *softQuota
	quota        quotaOps
	seatRoot     string          // /data/dingwei-newplat/agents 或 /data/dingwei-node
	registry     string          // 固定镜像仓库（如 hub.local/dingwei）
	cgroupParent string          // 🔴 必填：cgroup 父切片（如 dingwei-hosted.slice——组长 eccc54bc）
	seatProxy    string          // 🔴 必填：席位代理（第 12 条②：注入容器 env，如 http://172.30.68.1:11080）
	hubWSBase    string          // 🔴 必填（容器启动契约 R-G1-HOSTED-IMAGE-CONTRACT）：SH_WS_BASE＝代理配置 HubURL 的 ws 源
	localImages  map[string]bool // local 形态白名单（NODEAGENT_LOCAL_IMAGES；与板上登记同源）
	// seatHubViaProxy 席位到 Hub 全走 relay（R-SEAT-HUB-VIA-PROXY-IMPL-20261002232257 取案②）：
	// 开 ⇒ NO_PROXY 不含 Hub 主机（入网 HTTP 走 relay）＋注 SH_PROXY=seatProxy（WS 走 relay CONNECT）；
	// 关 ⇒ 现行为逐字不变（B12／本机 Hub 场景：Hub 主机追加进 NO_PROXY、WS 直连）。
	seatHubViaProxy bool
	// seatCodexModel 席位 Codex 模型指定（R-SEAT-MODEL-NODEAGENT-20261003014344，业主方案 1）：
	// 非空 ⇒ 注入 CODEX_MODEL=<值>；空 ⇒ 不注入（与 597b4d93 逐字相同）。
	// 校验（字符集 [A-Za-z0-9._-]、≤64）在 RegisterDockerHandlers，非法即拒启。
	seatCodexModel string
	// seatModels 本节点可选模型清单（R-V2-SEAT-ITEMS 包 B2；NODEAGENT_SEAT_CODEX_MODELS，
	// 运维配置——prodops 按业主要求设）。随容量帧上报；空＝不支持按席自选。
	seatModels []string
	// seatMode 席位 helper 模式（R-V2-HOSTED-DRIVER-IMPL-20261004143130，版本 2 指标 2）：
	// "cli"｜"" ⇒ 现行为逐字不变（SH_MODE=cli）；driver ⇒ SH_MODE=driver＋SH_EMIT_SESSION_EVENT=1
	//（cli 模式不产结构事件、view 看不到 Agent 回复——方案件 9b9bb1d9 取甲）。
	// 非法值在 RegisterDockerHandlers 拒启报键名。
	seatMode string
	waitMax  time.Duration
}

func containerName(seatID string) string { return "seat-" + seatID }
func networkName(seatID string) string   { return "seatnet-" + seatID }

func (d *dockerOps) pull(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		Image string `json:"image"`
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if err := validateImageRef(p.Image, d.registry); err != nil {
		return nil, err
	}
	if ok, err := d.checkLocalImage(ctx, p.Image); localImageID(p.Image) != "" {
		if err != nil {
			return nil, err
		}
		_ = ok
		return json.Marshal(map[string]string{"image": p.Image, "state": "present"})
	}
	if err := d.eng.imagePull(ctx, p.Image); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"image": p.Image, "state": "pulled"})
}

func (d *dockerOps) seatContainerID(ctx context.Context, seatID string) (string, error) {
	info, err := d.eng.containerInspect(ctx, containerName(seatID))
	if err != nil {
		if ee, ok := err.(*engineError); ok && ee.Status == 404 {
			return "", ErrSeatNotFound
		}
		return "", err
	}
	if d.soft != nil && !d.soft.recorded(seatID, info.ID) {
		return "", fmt.Errorf("%w: unregistered_container", ErrSoftQuota)
	}
	return info.ID, nil
}

type seatResources struct {
	MemoryMB           int64 `json:"memory_mb"`
	CPUShares          int   `json:"cpu_shares"`
	DiskMB             int64 `json:"disk_mb"`
	CPULimitMillicores int64 `json:"cpu_limit_millicores,omitempty"`
}

func (r seatResources) validate() error {
	switch {
	case r.MemoryMB < 4 || r.MemoryMB > 1<<20:
		return fmt.Errorf("%w: memory_mb 越界 %d", ErrParams, r.MemoryMB)
	case r.CPUShares < 2 || r.CPUShares > 262144:
		return fmt.Errorf("%w: cpu_shares 越界 %d", ErrParams, r.CPUShares)
	case r.CPULimitMillicores < 0 || r.CPULimitMillicores > 256000 || (r.CPULimitMillicores > 0 && r.CPULimitMillicores < 10):
		return fmt.Errorf("%w: cpu_limit_millicores out of range", ErrParams)
	case r.DiskMB < 0:
		return fmt.Errorf("%w: disk_mb 越界 %d", ErrParams, r.DiskMB)
	}
	return nil
}

type tokenFile struct {
	ApplicationID string `json:"application_id"`
	PollToken     string `json:"poll_token"`
	ExpiresAt     string `json:"expires_at"`
}

// credentialFile 团队账号凭据件（T-G4-32）：内容经白名单指令一次性下发，写席位目录固定名
// account-credential（0600、属主 10001）——🔴 凭据不经 env/命令行/镜像层（B7）；
// CLI 各自的落点（auth.json、settings.json…）由容器内启动脚本从该文件扇出，指令不带路径。
type credentialFile struct {
	Content string `json:"content"`
}

// engineImage 给引擎的镜像引用：local 形态翻成裸 sha256 ID（引擎把 local@… 当仓库摘要会 404——
// 复验 G3）；其余形态原样。
func engineImage(ref string) string {
	if id := localImageID(ref); id != "" {
		return "sha256:" + id
	}
	return ref
}

// buildSpec 安全参数清单（白名单固定，不可被指令关改）。
// 🔴 CgroupParent 必带（组长 eccc54bc：托管席位统一挂在可配置父切片下做资源切片；
// 漏设＝席位逃逸出平台切片，故 create 在空值时直接拒）。
func (d *dockerOps) buildSpec(image, seatID, netName string, res seatResources, credKind, cli, model string) map[string]any {
	memBytes := res.MemoryMB * 1024 * 1024
	policy := d.policyValue()
	cpuQuota := int64(res.CPUShares) * 100000 / 1024
	if res.CPULimitMillicores > 0 {
		cpuQuota = res.CPULimitMillicores * 100
	}
	labels := map[string]string{}
	if d.soft != nil {
		labels[managedSeatLabel] = d.soft.nodeID
		labels[managedSeatIDLabel] = seatID
	}
	return map[string]any{
		"Image":  engineImage(image),
		"Labels": labels,
		"User":   "10001:10001",                   // 非 root（隔离底线；与 seatRunUID 一致）
		"Env":    d.seatEnv(credKind, cli, model), // 🔴 凭据永不经 env（B7）；令牌走文件。代理非凭据（eccc54bc②）
		"HostConfig": map[string]any{
			"ReadonlyRootfs": true,
			"Tmpfs":          map[string]string{"/tmp": "size=8m"},
			"Memory":         memBytes,
			"MemorySwap":     memBytes, // memory＝memory-swap（禁 swap 绕过）
			"CpuShares":      res.CPUShares,
			"CpuPeriod":      int64(100000),
			"CpuQuota":       max(int64(1000), cpuQuota),
			"PidsLimit":      policy.PidsLimit,
			"ShmSize":        policy.ShmBytes,
			"LogConfig":      policy.logConfig(),
			"CgroupParent":   d.cgroupParent, // --cgroup-parent＝可配置父切片（eccc54bc）
			"CapDrop":        []string{"ALL"},
			"SecurityOpt":    []string{"no-new-privileges"},
			"Privileged":     false,
			"NetworkMode":    netName, // 每席独占网络
			"Binds":          []string{seatDir(d.seatRoot, seatID) + ":/work"},
			// B1（R-V2-SEAT-ITEMS-DISPATCH 包 B1）：tini 式 init 收孤儿＋转 SIGTERM——
			// 席位主进程非 PID 1 时信号不到子进程，docker stop 白等 10s 再 SIGKILL（退出码 137）。
			// 🔴 前置闸（复验预审）：Init 依赖 dockerd 的 docker-init 二进制——部署前必须在节点
			// 私有 dockerd 上核其在位（docker info），缺则停报（否则建席全挂、引擎报错）。
			"Init": true,
		},
	}
}

// 席位容器内运行用户（安全参数清单：非 root UID；令牌文件与席位目录属主亦为它——
// G3 确认件：0600 且属主＝容器内运行客户端的用户，否则客户端读不到）。
const seatRunUID = 10001
const seatRunGID = 10001

// chownForSeatFn 把路径属主改为席位运行用户（可注入——M1 返修阻断 2 的正反断言用）。
// 生产代理以 root（systemd）运行必执行；非 root 环境（单测）跳过——chown 本身就需要特权。
var chownForSeatFn = func(path string) error {
	if os.Geteuid() != 0 {
		return nil
	}
	return os.Chown(path, seatRunUID, seatRunGID)
}

func chownForSeat(path string) error { return chownForSeatFn(path) }

// seatEnv 席位容器环境变量。容器启动契约（R-G1-HOSTED-IMAGE-CONTRACT 一）：五个非凭据键
// 由节点代理注入——SH_WS_BASE（Hub 的 ws 源）／SH_ENROLL_TOKEN_FILE／SH_ENROLL=1／SH_CRED_FILE（均为路径）／
// SH_CRED_KIND（api_key|token_file，create 参数 cred_kind 带入）。SH_ENROLL=1 使无交互托管启动只走令牌入组。
// 🔴 凭据内容永不进 env/日志。
func (d *dockerOps) seatEnv(credKind, cli, model string) []string {
	env := []string{
		"SH_WS_BASE=" + d.hubWSBase,
		"SH_ENROLL_TOKEN_FILE=/work/enroll-token.json",
		"SH_ENROLL=1",
		"SH_CRED_FILE=/work/account-credential",
		"SH_CRED_KIND=" + credKind,
		// 席位的 CLI 由 create／upgrade 参数 cli 定（组长 R-B12-SEATMODE 第 2 条取乙：基础镜像含多家 CLI，模式按席位定、不写死进镜像）。
		// sessionHelper 未设 SH_MODE 时默认 echo（不起任何 CLI）；SH_CLI 选 CLI 档。
		// SH_MODE：节点级开关 seatMode（R-V2-HOSTED-DRIVER-IMPL）——driver 且本席 cli=codex
		// ⇒ driver（其余 CLI 与现行逐字相同，组长采纳 opus50tc2k 异议 1）；否则 cli。
		"SH_MODE=" + map[bool]string{true: "driver", false: "cli"}[d.seatMode == "driver" && cli == "codex"],
		"SH_CLI=" + cli,
	}
	if d.seatMode == "driver" && cli == "codex" {
		// driver 两件套：SH_DRIVER 显式选 codex driver（driver_adapter 默认也是 codex，写明防漂移）、
		// 事件键让 helper 产结构事件（view 才看得到 Agent 回复）。异议 1：仅 codex 席位。
		env = append(env, "SH_DRIVER=codex", "SH_EMIT_SESSION_EVENT=1")
	}
	if d.seatProxy == "" {
		return env
	}
	noProxy := "localhost,127.0.0.1" // 本机回环不经代理
	if !d.seatHubViaProxy {
		// 关（现行为逐字不变）：Hub 主机也不经代理（组长 R-B12-NOPROXY／R-B12-NET）——席位里
		// sessionHelper 的入网 HTTP（urllib）认 HTTP(S)_PROXY 与 NO_PROXY，不排除 Hub 就会把
		// 申请／轮询送进 relay；WS 本已直连。Python 的 NO_PROXY 只认主机名后缀与精确 IP、
		// 不认网段 ⇒ 追加 HubURL 的精确 host（不含端口）。B12／本机 Hub 场景适用。
		if host := hubHost(d.hubWSBase); host != "" {
			noProxy += "," + host
		}
	}
	env = append(env,
		"HTTP_PROXY="+d.seatProxy,
		"HTTPS_PROXY="+d.seatProxy,
		"NO_PROXY="+noProxy,
	)
	if d.seatHubViaProxy {
		// 开（R-SEAT-HUB-VIA-PROXY 案②，生产默认）：Hub 主机不入 NO_PROXY（入网 HTTP 走 relay），
		// 且必须另注 SH_PROXY——helper WS 只认它（app.py:487-497：空＝显式 proxy=None 不读 HTTPS_PROXY）。
		// 两个动作缺一不可：a 管 HTTP、b 管 WS。
		env = append(env, "SH_PROXY="+d.seatProxy)
	}
	// 模型生效值（包 B2）：按席 model（create/upgrade 参数）＞节点默认 NODEAGENT_SEAT_CODEX_MODEL；
	// 两皆空 ⇒ 不注入（与 597b4d93 逐字相同）。镜像侧 start.py 校验后写 config.toml 的 model。
	effective := model
	if effective == "" {
		effective = d.seatCodexModel
	}
	if effective != "" {
		env = append(env, "CODEX_MODEL="+effective)
	}
	return env
}

// hubHost 取 Hub ws 源的主机部分（去端口；IPv6 去方括号）；解析不出 ⇒ ""。
func hubHost(wsBase string) string {
	u, err := url.Parse(wsBase)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (d *dockerOps) writeTokenFile(seatID string, tf *tokenFile) error {
	if tf == nil {
		return nil
	}
	raw, err := json.Marshal(tf) // 字段名由 struct tag 钉为小写下划线（G3 确认件第 1 条）
	if err != nil {
		return err
	}
	dir := seatDir(d.seatRoot, seatID)
	if err := os.MkdirAll(dir, 0o700); err != nil { // 防御：配额实现未建目录时也能落文件
		return err
	}
	if err := chownForSeat(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "enroll-token.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return chownForSeat(path)
}

// writeCredentialFile 账号凭据件（T-G4-32）：固定名 account-credential、0600、属主 10001。
func (d *dockerOps) writeCredentialFile(seatID string, cf *credentialFile) error {
	if cf == nil {
		return nil
	}
	dir := seatDir(d.seatRoot, seatID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := chownForSeat(dir); err != nil {
		return err
	}
	path := filepath.Join(dir, "account-credential")
	if err := os.WriteFile(path, []byte(cf.Content), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return err
	}
	return chownForSeat(path)
}

func (d *dockerOps) waitRunning(ctx context.Context, id string) error {
	deadline := time.Now().Add(d.waitMax)
	for {
		info, err := d.eng.containerInspect(ctx, id)
		if err != nil {
			return err
		}
		if info.State.Running && (info.State.Health == nil || info.State.Health.Status == "healthy") {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("容器未在 %v 内健康上线", d.waitMax)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func (d *dockerOps) create(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SeatID    string          `json:"seat_id"`
		Image     string          `json:"image"`
		Resources seatResources   `json:"resources"`
		Token     *tokenFile      `json:"token_file"`
		Cred      *credentialFile `json:"credential_file"`
		CredKind  string          `json:"cred_kind"` // api_key|token_file（容器启动契约：账号形态）
		CLI       string          `json:"cli"`       // 席位 CLI（seatCLIs 白名单；漏设即拒）
		Model     string          `json:"model"`     // 包 B2：按席模型（可空＝节点默认）
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !validSeatID(p.SeatID) {
		return nil, fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
	}
	// 包 B2：按席模型校验——格式同 validModelName；本节点有清单时必须在清单内（Hub 已按清单选点，这里纵深）。
	if p.Model != "" {
		if !validModelName(p.Model) {
			return nil, fmt.Errorf("%w: model %q 格式不对", ErrParams, p.Model)
		}
		if len(d.seatModels) == 0 {
			// 口径 3：清单空＝本节点不支持选型；带 model 一律拒（fail-closed，Hub 歪斜时兜住）
			return nil, fmt.Errorf("%w: model 选型不受支持（本节点无清单）", ErrParams)
		}
		if !slices.Contains(d.seatModels, p.Model) {
			return nil, fmt.Errorf("%w: model %q 不在本节点可选清单", ErrParams, p.Model)
		}
	}
	if err := validateImageRef(p.Image, d.registry); err != nil {
		return nil, err
	}
	// 🔴 create 也过 local 闸（复验关注面：未经 pull 直达 create 时，未登记本机镜像不得起容器）
	if _, err := d.checkLocalImage(ctx, p.Image); err != nil {
		return nil, err
	}
	if err := p.Resources.validate(); err != nil {
		return nil, err
	}
	if d.cgroupParent == "" {
		return nil, fmt.Errorf("%w: cgroup 父切片未配置（eccc54bc：漏设即拒）", ErrParams)
	}
	if d.seatProxy == "" {
		return nil, fmt.Errorf("%w: 席位代理未配置（第 12 条②：漏设即拒）", ErrParams)
	}
	// 容器启动契约：带凭据件必须带形态（缺 ⇒ 启动脚本无法扇出）；SH_WS_BASE 漏设即拒
	if d.hubWSBase == "" {
		return nil, fmt.Errorf("%w: SH_WS_BASE 未配置（容器启动契约：漏设即拒）", ErrParams)
	}
	if p.CredKind != "api_key" && p.CredKind != "token_file" {
		return nil, fmt.Errorf("%w: cred_kind 须 api_key|token_file（容器启动契约：缺任一键即拒）", ErrParams)
	}
	if !seatCLIs[p.CLI] {
		return nil, fmt.Errorf("%w: cli %q 不在白名单（漏设即拒）", ErrParams, p.CLI)
	}

	if d.soft != nil {
		if err := d.soft.reserve(p.SeatID, p.Resources); err != nil {
			return nil, err
		}
	}
	name := containerName(p.SeatID)
	netName := networkName(p.SeatID)
	// 失败清理（V6-B3：不留半成品）——逆序：容器→网络→配额与目录。
	// 🔴 M1 复验阻断 5：清理自身失败必须上抛（清理都失败而报"已清理"是比残留更糟的假象）。
	cleanup := func(containerID string) error {
		if d.soft != nil {
			return nil
		} // soft failures retain data; monitor/owner reconcile, never Clear
		var errs []string
		if containerID != "" {
			if err := d.eng.containerRemove(context.Background(), containerID); err != nil {
				errs = append(errs, "container_remove: "+err.Error())
			}
		}
		if err := d.eng.networkRemove(context.Background(), netName); err != nil {
			errs = append(errs, "network_remove: "+err.Error())
		}
		if d.soft == nil {
			if err := d.quota.Clear(d.seatRoot, p.SeatID); err != nil {
				errs = append(errs, "quota_clear: "+err.Error())
			}
		}
		if len(errs) > 0 {
			return fmt.Errorf("清理失败（半成品可能残留）: %s", strings.Join(errs, "; "))
		}
		return nil
	}
	fail := func(stageErr error, containerID string) (json.RawMessage, error) {
		if cerr := cleanup(containerID); cerr != nil {
			return nil, fmt.Errorf("create 失败(%v)，且%v", stageErr, cerr)
		}
		return nil, stageErr
	}

	// ① 磁盘三件套之席位目录＋XFS 项目配额
	// 🔴 复验 r2 阻断①：Setup 失败也要进清理（Setup 虽自拆目录，但网络/容器侧若已动过
	//    不能假设没有半成品——一律走同一 fail 路径）。
	if err := d.quota.Setup(d.seatRoot, p.SeatID, p.Resources.DiskMB); err != nil {
		return fail(err, "")
	}
	// ② 令牌文件（0600，不经 env）
	if err := d.writeTokenFile(p.SeatID, p.Token); err != nil {
		return fail(err, "")
	}
	// ②b 账号凭据件（固定名，0600，不经 env）
	if err := d.writeCredentialFile(p.SeatID, p.Cred); err != nil {
		return fail(err, "")
	}
	// ③ 每席独占网络
	if err := d.eng.networkCreate(ctx, netName); err != nil {
		return fail(err, "")
	}
	// ④ 安全参数清单起容器
	id, err := d.eng.containerCreate(ctx, name, d.buildSpec(p.Image, p.SeatID, netName, p.Resources, p.CredKind, p.CLI, p.Model))
	if err != nil {
		return fail(err, "")
	}
	if d.soft != nil {
		if err := d.soft.track(p.SeatID, id); err != nil {
			return fail(err, id)
		}
	}
	if err := d.startWriter(ctx, p.SeatID, id, false); err != nil {
		return fail(err, id)
	}
	if err := d.waitRunning(ctx, id); err != nil {
		return fail(err, id)
	}
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "container_id": id, "state": "running"})
}

func (d *dockerOps) stop(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	id, err := d.seatIDParam(params)
	if err != nil {
		return nil, err
	}
	cid, err := d.seatContainerID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := d.eng.containerStop(ctx, cid); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": id, "state": "stopped"})
}

func (d *dockerOps) start(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	id, err := d.seatIDParam(params)
	if err != nil {
		return nil, err
	}
	if err := d.preflightSeat(id); err != nil {
		return nil, err
	}
	cid, err := d.seatContainerID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := d.startWriter(ctx, id, cid, false); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": id, "state": "running"})
}

func (d *dockerOps) restart(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	id, err := d.seatIDParam(params)
	if err != nil {
		return nil, err
	}
	if err := d.preflightSeat(id); err != nil {
		return nil, err
	}
	cid, err := d.seatContainerID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := d.startWriter(ctx, id, cid, true); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": id, "state": "running"})
}

// refreshCred 刷新凭据（0.2.1.101）：参数严格解码 {seat_id, credential_file{content}}；
// ① 席位容器须存在；② 原子覆写 account-credential（同目录临时文件→fsync→rename，0600、属主 10001）；
// ③ containerRestart（同 restart）。失败前缀供 Hub 分类：credential_write_failed（未重启、旧件不变）／
// restart_failed（件已换、重启失败）。🔴 错误文本与日志一律不含凭据内容。
func (d *dockerOps) refreshCred(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SeatID string          `json:"seat_id"`
		Cred   *credentialFile `json:"credential_file"`
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !validSeatID(p.SeatID) {
		return nil, fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
	}
	if err := d.preflightSeat(p.SeatID); err != nil {
		return nil, err
	}
	if p.Cred == nil || p.Cred.Content == "" {
		return nil, fmt.Errorf("%w: credential_file 必填", ErrParams)
	}
	cid, err := d.seatContainerID(ctx, p.SeatID)
	if err != nil {
		return nil, err
	}
	// 先撤摘要标记再写凭据件（镜像 r4 判据，R4-ADDENDUM 复核必改 1 取 A）：下次启动无标记且凭据件较新 ⇒ 必重写 auth.json，
	// 「刷新＝以池内副本覆盖登录态」不因内容未变而落空。先撤后写：中途失败只会落到「无标记、凭据件较旧」⇒ 不重写（同 r3→r4 首升）。
	if err := clearAppliedMarker(seatDir(d.seatRoot, p.SeatID)); err != nil {
		return nil, fmt.Errorf("credential_write_failed: marker: %v", err)
	}
	if err := d.writeCredentialFileAtomic(p.SeatID, p.Cred); err != nil {
		return nil, fmt.Errorf("credential_write_failed: %v", err)
	}
	if err := d.startWriter(ctx, p.SeatID, cid, true); err != nil {
		return nil, fmt.Errorf("restart_failed: %v", err)
	}
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "state": "running"})
}

// appliedMarkerName 镜像 r4 start.py 记录「上次写 auth.json 所用凭据摘要」的文件（在 CODEX_HOME＝/work/.codex 下）。
const appliedMarkerName = ".dw-applied-credential.sha256"

// clearAppliedMarker 删除席位 .codex 下的摘要标记。.codex 由容器内进程可写 ⇒ 不跟随符号链接：
// 以 O_NOFOLLOW|O_DIRECTORY 打开 .codex，再相对该句柄 unlinkat（标记本身是链接也只删链接）。
// .codex 或标记不存在 ⇒ 无事可做；.codex 是链接或非目录 ⇒ 报错（不碰宿主别处文件）。
func clearAppliedMarker(seatDirPath string) error {
	fd, err := syscall.Open(filepath.Join(seatDirPath, ".codex"), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err == syscall.ENOENT {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open .codex: %w", err)
	}
	defer syscall.Close(fd)
	if err := syscall.Unlinkat(fd, appliedMarkerName); err != nil && err != syscall.ENOENT {
		return fmt.Errorf("unlink marker: %w", err)
	}
	return nil
}

// writeCredentialFileAtomic 刷新路径的原子写：写到一半失败不留残缺件（旧件保持原样）。错误不含内容。
func (d *dockerOps) writeCredentialFileAtomic(seatID string, cf *credentialFile) error {
	dir := seatDir(d.seatRoot, seatID)
	tmp, err := os.CreateTemp(dir, ".account-credential.")
	if err != nil {
		return fmt.Errorf("create temp: %w", errNoContent(err))
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if writeCredHook != nil {
		if err := writeCredHook(); err != nil {
			_ = tmp.Close()
			return err
		}
	}
	if _, err := tmp.WriteString(cf.Content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write: %w", errNoContent(err))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := chownForSeat(name); err != nil {
		return err
	}
	if err := os.Rename(name, filepath.Join(dir, "account-credential")); err != nil {
		return err
	}
	ok = true
	return nil
}

// writeCredHook 只供测试：在写入内容前注入失败（验「写失败不留残件、不重启」）。
var writeCredHook func() error

// errNoContent 保证错误文本不夹带写入内容（os 错误本就只含路径与原因；此处显式收口）。
func errNoContent(err error) error { return err }

func (d *dockerOps) seatIDParam(params json.RawMessage) (string, error) {
	var p struct {
		SeatID string `json:"seat_id"`
	}
	if err := strictDecode(params, &p); err != nil {
		return "", err
	}
	if !validSeatID(p.SeatID) {
		return "", fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
	}
	return p.SeatID, nil
}

// upgrade 升级：拉新镜像 → 同卷同资源起新容器 → 新容器健康上线后再删旧（V8-D2）→ 改名归位。
// 资源由 Hub 按 profile 计算后随指令显式下发（plan→resources 的换算在 Hub 侧，代理不读 plan）。
func (d *dockerOps) upgrade(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	var p struct {
		SeatID    string        `json:"seat_id"`
		Image     string        `json:"image"`
		Resources seatResources `json:"resources"`
		Token     *tokenFile    `json:"token_file"` // 轮换的新令牌（Hub Reissue 先行；写新令牌文件再起新容器）
		CredKind  string        `json:"cred_kind"`
		CLI       string        `json:"cli"`   // 与 create 同口径：升级后的新容器同一 CLI
		Model     string        `json:"model"` // 包 B2：Hub 按席位记录值下发（修订 3：不得因节点默认变化悄悄换模型）
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !validSeatID(p.SeatID) {
		return nil, fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
	}
	if err := d.preflightSeat(p.SeatID); err != nil {
		return nil, err
	}
	// 复验缺口（tosonnet55tc2k）：upgrade 的 model 至少过格式校验（成员关系不查——
	// 记录值可能早于清单变更；Hub 下发是记录值，格式纵深仍在）。
	if p.Model != "" && !validModelName(p.Model) {
		return nil, fmt.Errorf("%w: model %q 格式不对", ErrParams, p.Model)
	}
	if err := validateImageRef(p.Image, d.registry); err != nil {
		return nil, err
	}
	if err := p.Resources.validate(); err != nil {
		return nil, err
	}
	// 与 create 同一闸（复验同型加固）：父切片与代理漏设即拒
	if d.cgroupParent == "" {
		return nil, fmt.Errorf("%w: cgroup 父切片未配置（eccc54bc：漏设即拒）", ErrParams)
	}
	if d.seatProxy == "" {
		return nil, fmt.Errorf("%w: 席位代理未配置（第 12 条②：漏设即拒）", ErrParams)
	}
	// 容器启动契约：带凭据件必须带形态（缺 ⇒ 启动脚本无法扇出）；SH_WS_BASE 漏设即拒
	if d.hubWSBase == "" {
		return nil, fmt.Errorf("%w: SH_WS_BASE 未配置（容器启动契约：漏设即拒）", ErrParams)
	}
	// G5：与 create 同口径——cred_kind 缺即拒（否则新容器带空 SH_CRED_KIND 起）
	if p.CredKind != "api_key" && p.CredKind != "token_file" {
		return nil, fmt.Errorf("%w: cred_kind 须 api_key|token_file（与 create 同口径，G5）", ErrParams)
	}
	if !seatCLIs[p.CLI] {
		return nil, fmt.Errorf("%w: cli %q 不在白名单（与 create 同口径）", ErrParams, p.CLI)
	}
	if d.soft != nil {
		if err := d.soft.checkResources(p.SeatID, p.Resources); err != nil {
			return nil, err
		}
	}
	oldID, err := d.seatContainerID(ctx, p.SeatID)
	if err != nil {
		return nil, err
	}
	if localImageID(p.Image) != "" {
		if _, err := d.checkLocalImage(ctx, p.Image); err != nil {
			return nil, err
		}
	} else if err := d.eng.imagePull(ctx, p.Image); err != nil {
		return nil, err
	}
	// 新令牌文件先于新容器写入（轮换：新容器凭它取新凭据，取走旧才作废）
	if err := d.writeTokenFile(p.SeatID, p.Token); err != nil {
		return nil, err
	}
	newName := containerName(p.SeatID) + "-new"
	newID, err := d.eng.containerCreate(ctx, newName, d.buildSpec(p.Image, p.SeatID, networkName(p.SeatID), p.Resources, p.CredKind, p.CLI, p.Model))
	if err != nil {
		return nil, err
	}
	if d.soft != nil {
		if err := d.soft.track(p.SeatID, newID); err != nil {
			return nil, err
		}
	}
	if err := d.startWriter(ctx, p.SeatID, newID, false); err != nil {
		_ = d.eng.containerRemove(context.Background(), newID)
		return nil, err
	}
	if err := d.waitRunning(ctx, newID); err != nil { // 新容器不健康 ⇒ 删新留旧（回滚，V8 自动回滚口径）
		_ = d.eng.containerRemove(context.Background(), newID)
		return nil, fmt.Errorf("upgrade_failed_rolled_back: %v", err)
	}
	// 新容器健康上线后才删旧
	if err := d.eng.containerStop(ctx, oldID); err != nil {
		return nil, err
	}
	if err := d.eng.containerRemove(ctx, oldID); err != nil {
		return nil, err
	}
	if d.soft != nil {
		if err := d.soft.untrack(p.SeatID, oldID); err != nil {
			return nil, err
		}
	}
	if err := d.eng.call(ctx, "POST", "/containers/"+newID+"/rename?name="+containerName(p.SeatID), nil, nil); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": p.SeatID, "container_id": newID, "state": "running", "image": p.Image})
}

// destroy 销毁：停删容器 → 删网络 → 清项目配额并删席位目录（迁移与销毁须验证配额项目清除——V8-D5）。
func (d *dockerOps) destroy(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	id, err := d.seatIDParam(params)
	if err != nil {
		return nil, err
	}
	cid, err := d.seatContainerID(ctx, id)
	if err != nil && !errors.Is(err, ErrSeatNotFound) {
		return nil, err
	}
	if cid != "" {
		if err := d.eng.containerStop(ctx, cid); err != nil {
			return nil, err
		}
		if err := d.eng.containerRemove(ctx, cid); err != nil {
			return nil, err
		}
	}
	if err := d.eng.networkRemove(ctx, networkName(id)); err != nil {
		return nil, err
	}
	if err := d.quota.Clear(d.seatRoot, id); err != nil {
		return nil, err
	}
	if d.soft != nil {
		if err := d.soft.forget(id); err != nil {
			return nil, err
		}
	}
	return json.Marshal(map[string]string{"seat_id": id, "state": "destroyed", "quota": "cleared"})
}

// ParseLocalImages 解析 NODEAGENT_LOCAL_IMAGES（CSV，每项一个本机镜像 ID）：
// 归一 sha256: 前缀（剥掉）；剥后每项须为 64 位小写裸 hex，否则报错并点名键名——
// 启动期拒启用（R-B12-WHITELIST-FIX 附派；前缀形态曾是 106 的 image_pull_failed 根因）。
func ParseLocalImages(csv string) (map[string]bool, error) {
	allow := map[string]bool{}
	for _, item := range strings.Split(csv, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		item = strings.TrimPrefix(item, "sha256:")
		if !isBareImageID(item) {
			return nil, fmt.Errorf("NODEAGENT_LOCAL_IMAGES 项 %q 格式不对（须 64 位小写裸 hex，可带 sha256: 前缀）", item)
		}
		allow[item] = true
	}
	return allow, nil
}

// validModelName 模型名合法形：1–64 位、[A-Za-z0-9._-]（R-SEAT-MODEL-NODEAGENT；非法即启动失败，防注入串进席位 env）。
func validModelName(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// isBareImageID：64 位小写裸 hex（Docker 镜像 ID 形态）。
func isBareImageID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// RegisterDockerHandlers 把 Docker 指令集挂进代理骨架（cmd/nodeagent 在真机上调用）。
// 同时接上 T-G4-13 容量上报（真采集替换骨架桩，并注册心跳）。
// cgroupParent 必填（eccc54bc）——create 的 --cgroup-parent 指向可配置父切片。
// localImagesCSV 格式不对即报错（报键名），调用方拒启。
func (a *Agent) RegisterDockerHandlers(eng *engine, quota quotaOps, seatRoot, registry, cgroupParent, seatProxy, hubWSS, localImagesCSV string, seatHubViaProxy bool, seatCodexModel, seatMode string, seatModels []string, policies ...NodePolicy) error {
	if seatCodexModel != "" && !validModelName(seatCodexModel) {
		return fmt.Errorf("NODEAGENT_SEAT_CODEX_MODEL %q 格式不对（须 1–64 位 [A-Za-z0-9._-]）", seatCodexModel)
	}
	for _, m := range seatModels { // 清单逐项同校验（运维配置，坏值拒启报键名）
		if !validModelName(m) {
			return fmt.Errorf("NODEAGENT_SEAT_CODEX_MODELS 项 %q 格式不对（须 1–64 位 [A-Za-z0-9._-]）", m)
		}
	}
	// 口径（复验预审 3）：清单非空时，节点默认 NODEAGENT_SEAT_CODEX_MODEL 必须是清单成员
	//（否则默认注出的值节点自己都不认账）⇒ 拒启报键名。
	if len(seatModels) > 0 && seatCodexModel != "" && !slices.Contains(seatModels, seatCodexModel) {
		return fmt.Errorf("NODEAGENT_SEAT_CODEX_MODEL %q 不在 NODEAGENT_SEAT_CODEX_MODELS 清单内", seatCodexModel)
	}
	// R-V2-HOSTED-DRIVER-IMPL：cli｜driver｜空（＝cli）；其它即拒启报键名（与 CODEX_MODEL 同款）。
	if seatMode != "" && seatMode != "cli" && seatMode != "driver" {
		return fmt.Errorf("NODEAGENT_SEAT_MODE %q 取值不对（只认 cli｜driver）", seatMode)
	}
	// SH_WS_BASE＝HubURL 的 ws 源（wss://host[:port]；路径剥掉）
	base := hubWSS
	if i := strings.Index(base, "://"); i >= 0 {
		rest := base[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			rest = rest[:j]
		}
		base = base[:i+3] + rest
	}
	allow, err := ParseLocalImages(localImagesCSV)
	if err != nil {
		return err
	}
	policy := DefaultNodePolicy()
	if len(policies) > 0 {
		policy = policies[0]
	}
	if err := policy.Validate(); err != nil {
		return err
	}
	d := &dockerOps{eng: eng, policy: policy, quota: quota, seatRoot: seatRoot, registry: registry, cgroupParent: cgroupParent, seatProxy: seatProxy, hubWSBase: base, localImages: allow, waitMax: 60 * time.Second, seatHubViaProxy: seatHubViaProxy, seatCodexModel: seatCodexModel, seatMode: seatMode, seatModels: seatModels}
	if policy.Mode == "soft" {
		q, err := newSoftQuota(policy, seatRoot, a.cfg.LedgerPath+".soft-quota", a.cfg.NodeID)
		if err != nil {
			return err
		}
		d.soft = q
		q.capacity = &seatCapacityState{}
		d.quota = softDirectory{}
		a.commandGate = q.commandGate
		a.heartbeatInterval = 15 * time.Second
		a.background = d.runSoft
		a.policyClose = q.close
	}
	a.docker = d
	a.Register(CmdPullImage, d.pull)
	a.Register(CmdCreateSeat, d.create)
	a.Register(CmdStopSeat, d.stop)
	a.Register(CmdReleaseHold, d.releaseHold)
	a.Register(CmdStartSeat, d.start)
	a.Register(CmdRestartSeat, d.restart)
	a.Register(CmdUpgradeSeat, d.upgrade)
	a.Register(CmdDestroySeat, d.destroy)
	a.Register(CmdRefreshCredSeat, d.refreshCred)
	a.Register(CmdReportCapacity, d.reportCapacity)
	a.capacity = d.collectCapacity
	return nil
}

// SetHeartbeatInterval 测试用（心跳周期，默认 60s）。
func (a *Agent) SetHeartbeatInterval(d time.Duration) { a.heartbeatInterval = d }
