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
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	ErrImageRef     = errors.New("image ref 必须是固定仓库＋sha256 digest")
	ErrParams       = errors.New("参数不合白名单")
	ErrSeatNotFound = errors.New("席位容器不存在")
)

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func validateImageRef(ref, registry string) error {
	repo, digest, ok := strings.Cut(ref, "@")
	if !ok || !strings.HasPrefix(repo, registry+"/") || !digestPattern.MatchString(digest) {
		return fmt.Errorf("%w: %q", ErrImageRef, ref)
	}
	return nil
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
	quota        quotaOps
	seatRoot     string // /data/dingwei-newplat/agents 或 /data/dingwei-node
	registry     string // 固定镜像仓库（如 hub.local/dingwei）
	cgroupParent string // 🔴 必填：cgroup 父切片（如 dingwei-hosted.slice——组长 eccc54bc）
	seatProxy    string // 🔴 必填：席位代理（第 12 条②：注入容器 env，如 http://172.30.68.1:11080）
	hubWSBase    string // 🔴 必填（容器启动契约 R-G1-HOSTED-IMAGE-CONTRACT）：SH_WS_BASE＝代理配置 HubURL 的 ws 源
	waitMax      time.Duration
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
	return info.ID, nil
}

type seatResources struct {
	MemoryMB  int64 `json:"memory_mb"`
	CPUShares int   `json:"cpu_shares"`
	DiskMB    int64 `json:"disk_mb"`
}

func (r seatResources) validate() error {
	switch {
	case r.MemoryMB < 4 || r.MemoryMB > 1<<20:
		return fmt.Errorf("%w: memory_mb 越界 %d", ErrParams, r.MemoryMB)
	case r.CPUShares < 2 || r.CPUShares > 262144:
		return fmt.Errorf("%w: cpu_shares 越界 %d", ErrParams, r.CPUShares)
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

// buildSpec 安全参数清单（白名单固定，不可被指令关改）。
// 🔴 CgroupParent 必带（组长 eccc54bc：托管席位统一挂在可配置父切片下做资源切片；
// 漏设＝席位逃逸出平台切片，故 create 在空值时直接拒）。
func (d *dockerOps) buildSpec(image, seatID, netName string, res seatResources, credKind string) map[string]any {
	memBytes := res.MemoryMB * 1024 * 1024
	return map[string]any{
		"Image": image,
		"User":  "10001:10001", // 非 root（隔离底线；与 seatRunUID 一致）
		"Env":   d.seatEnv(credKind),   // 🔴 凭据永不经 env（B7）；令牌走文件。代理非凭据（eccc54bc②）
		"HostConfig": map[string]any{
			"ReadonlyRootfs": true,
			"Tmpfs":          map[string]string{"/tmp": "size=8m"},
			"Memory":         memBytes,
			"MemorySwap":     memBytes, // memory＝memory-swap（禁 swap 绕过）
			"CpuShares":      res.CPUShares,
			"CgroupParent":   d.cgroupParent, // --cgroup-parent＝可配置父切片（eccc54bc）
			"CapDrop":        []string{"ALL"},
			"SecurityOpt":    []string{"no-new-privileges"},
			"Privileged":     false,
			"NetworkMode":    netName, // 每席独占网络
			"Binds":          []string{seatDir(d.seatRoot, seatID) + ":/work"},
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

// seatEnv 席位容器环境变量。容器启动契约（R-G1-HOSTED-IMAGE-CONTRACT 一）：四个非凭据键
// 由节点代理注入——SH_WS_BASE（Hub 的 ws 源）／SH_ENROLL_TOKEN_FILE／SH_CRED_FILE（均为路径）／
// SH_CRED_KIND（api_key|token_file，create 参数 cred_kind 带入）。🔴 凭据内容永不进 env/日志。
func (d *dockerOps) seatEnv(credKind string) []string {
	env := []string{
		"SH_WS_BASE=" + d.hubWSBase,
		"SH_ENROLL_TOKEN_FILE=/work/enroll-token.json",
		"SH_CRED_FILE=/work/account-credential",
		"SH_CRED_KIND=" + credKind,
	}
	if d.seatProxy == "" {
		return env
	}
	return append(env,
		"HTTP_PROXY="+d.seatProxy,
		"HTTPS_PROXY="+d.seatProxy,
		"NO_PROXY=localhost,127.0.0.1", // 本机回环不经代理
	)
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
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !validSeatID(p.SeatID) {
		return nil, fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
	}
	if err := validateImageRef(p.Image, d.registry); err != nil {
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

	name := containerName(p.SeatID)
	netName := networkName(p.SeatID)
	// 失败清理（V6-B3：不留半成品）——逆序：容器→网络→配额与目录。
	// 🔴 M1 复验阻断 5：清理自身失败必须上抛（清理都失败而报"已清理"是比残留更糟的假象）。
	cleanup := func(containerID string) error {
		var errs []string
		if containerID != "" {
			if err := d.eng.containerRemove(context.Background(), containerID); err != nil {
				errs = append(errs, "container_remove: "+err.Error())
			}
		}
		if err := d.eng.networkRemove(context.Background(), netName); err != nil {
			errs = append(errs, "network_remove: "+err.Error())
		}
		if err := d.quota.Clear(d.seatRoot, p.SeatID); err != nil {
			errs = append(errs, "quota_clear: "+err.Error())
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
	id, err := d.eng.containerCreate(ctx, name, d.buildSpec(p.Image, p.SeatID, netName, p.Resources, p.CredKind))
	if err != nil {
		return fail(err, "")
	}
	if err := d.eng.containerStart(ctx, id); err != nil {
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
	cid, err := d.seatContainerID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := d.eng.containerStart(ctx, cid); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": id, "state": "running"})
}

func (d *dockerOps) restart(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	id, err := d.seatIDParam(params)
	if err != nil {
		return nil, err
	}
	cid, err := d.seatContainerID(ctx, id)
	if err != nil {
		return nil, err
	}
	if err := d.eng.containerRestart(ctx, cid); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"seat_id": id, "state": "running"})
}

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
	}
	if err := strictDecode(params, &p); err != nil {
		return nil, err
	}
	if !validSeatID(p.SeatID) {
		return nil, fmt.Errorf("%w: seat_id %q", ErrParams, p.SeatID)
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
	oldID, err := d.seatContainerID(ctx, p.SeatID)
	if err != nil {
		return nil, err
	}
	if err := d.eng.imagePull(ctx, p.Image); err != nil {
		return nil, err
	}
	// 新令牌文件先于新容器写入（轮换：新容器凭它取新凭据，取走旧才作废）
	if err := d.writeTokenFile(p.SeatID, p.Token); err != nil {
		return nil, err
	}
	newName := containerName(p.SeatID) + "-new"
	newID, err := d.eng.containerCreate(ctx, newName, d.buildSpec(p.Image, p.SeatID, networkName(p.SeatID), p.Resources, p.CredKind))
	if err != nil {
		return nil, err
	}
	if err := d.eng.containerStart(ctx, newID); err != nil {
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
	return json.Marshal(map[string]string{"seat_id": id, "state": "destroyed", "quota": "cleared"})
}

// RegisterDockerHandlers 把 Docker 指令集挂进代理骨架（cmd/nodeagent 在真机上调用）。
// 同时接上 T-G4-13 容量上报（真采集替换骨架桩，并注册心跳）。
// cgroupParent 必填（eccc54bc）——create 的 --cgroup-parent 指向可配置父切片。
func (a *Agent) RegisterDockerHandlers(eng *engine, quota quotaOps, seatRoot, registry, cgroupParent, seatProxy, hubWSS string) {
	// SH_WS_BASE＝HubURL 的 ws 源（wss://host[:port]；路径剥掉）
	base := hubWSS
	if i := strings.Index(base, "://"); i >= 0 {
		rest := base[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			rest = rest[:j]
		}
		base = base[:i+3] + rest
	}
	d := &dockerOps{eng: eng, quota: quota, seatRoot: seatRoot, registry: registry, cgroupParent: cgroupParent, seatProxy: seatProxy, hubWSBase: base, waitMax: 60 * time.Second}
	a.Register(CmdPullImage, d.pull)
	a.Register(CmdCreateSeat, d.create)
	a.Register(CmdStopSeat, d.stop)
	a.Register(CmdStartSeat, d.start)
	a.Register(CmdRestartSeat, d.restart)
	a.Register(CmdUpgradeSeat, d.upgrade)
	a.Register(CmdDestroySeat, d.destroy)
	a.Register(CmdReportCapacity, d.reportCapacity)
	a.capacity = d.collectCapacity
}

// SetHeartbeatInterval 测试用（心跳周期，默认 60s）。
func (a *Agent) SetHeartbeatInterval(d time.Duration) { a.heartbeatInterval = d }
