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
	eng      *engine
	quota    quotaOps
	seatRoot string // /data/dingwei-newplat/agents 或 /data/dingwei-node
	registry string // 固定镜像仓库（如 hub.local/dingwei）
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
	CPUShares int    `json:"cpu_shares"`
	DiskMB    int64  `json:"disk_mb"`
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

// buildSpec 安全参数清单（白名单固定，不可被指令关改）。
func (d *dockerOps) buildSpec(image, seatID, netName string, res seatResources) map[string]any {
	memBytes := res.MemoryMB * 1024 * 1024
	return map[string]any{
		"Image": image,
		"User":  "10001:10001", // 非 root（隔离底线）
		"Env":   []string{},    // 🔴 凭据永不经 env（B7）；令牌走文件
		"HostConfig": map[string]any{
			"ReadonlyRootfs": true,
			"Tmpfs":          map[string]string{"/tmp": "size=8m"},
			"Memory":         memBytes,
			"MemorySwap":     memBytes, // memory＝memory-swap（禁 swap 绕过）
			"CpuShares":      res.CPUShares,
			"CapDrop":        []string{"ALL"},
			"SecurityOpt":    []string{"no-new-privileges"},
			"Privileged":     false,
			"NetworkMode":    netName, // 每席独占网络
			"Binds":          []string{seatDir(d.seatRoot, seatID) + ":/work"},
		},
	}
}

func (d *dockerOps) writeTokenFile(seatID string, tf *tokenFile) error {
	if tf == nil {
		return nil
	}
	raw, err := json.Marshal(tf)
	if err != nil {
		return err
	}
	dir := seatDir(d.seatRoot, seatID)
	if err := os.MkdirAll(dir, 0o700); err != nil { // 防御：配额实现未建目录时也能落文件
		return err
	}
	path := filepath.Join(dir, "enroll-token.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
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
		SeatID    string        `json:"seat_id"`
		Image     string        `json:"image"`
		Resources seatResources `json:"resources"`
		Token     *tokenFile    `json:"token_file"`
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

	name := containerName(p.SeatID)
	netName := networkName(p.SeatID)
	// 失败清理（V6-B3：不留半成品）——逆序：容器→网络→配额与目录
	cleanup := func(containerID string) {
		if containerID != "" {
			_ = d.eng.containerRemove(context.Background(), containerID)
		}
		_ = d.eng.networkRemove(context.Background(), netName)
		_ = d.quota.Clear(d.seatRoot, p.SeatID)
	}

	// ① 磁盘三件套之席位目录＋XFS 项目配额
	if err := d.quota.Setup(d.seatRoot, p.SeatID, p.Resources.DiskMB); err != nil {
		return nil, err
	}
	// ② 令牌文件（0600，不经 env）
	if err := d.writeTokenFile(p.SeatID, p.Token); err != nil {
		cleanup("")
		return nil, err
	}
	// ③ 每席独占网络
	if err := d.eng.networkCreate(ctx, netName); err != nil {
		cleanup("")
		return nil, err
	}
	// ④ 安全参数清单起容器
	id, err := d.eng.containerCreate(ctx, name, d.buildSpec(p.Image, p.SeatID, netName, p.Resources))
	if err != nil {
		cleanup("")
		return nil, err
	}
	if err := d.eng.containerStart(ctx, id); err != nil {
		cleanup(id)
		return nil, err
	}
	if err := d.waitRunning(ctx, id); err != nil {
		cleanup(id)
		return nil, err
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
	oldID, err := d.seatContainerID(ctx, p.SeatID)
	if err != nil {
		return nil, err
	}
	if err := d.eng.imagePull(ctx, p.Image); err != nil {
		return nil, err
	}
	newName := containerName(p.SeatID) + "-new"
	newID, err := d.eng.containerCreate(ctx, newName, d.buildSpec(p.Image, p.SeatID, networkName(p.SeatID), p.Resources))
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
func (a *Agent) RegisterDockerHandlers(eng *engine, quota quotaOps, seatRoot, registry string) {
	d := &dockerOps{eng: eng, quota: quota, seatRoot: seatRoot, registry: registry, waitMax: 60 * time.Second}
	a.Register(CmdPullImage, d.pull)
	a.Register(CmdCreateSeat, d.create)
	a.Register(CmdStopSeat, d.stop)
	a.Register(CmdStartSeat, d.start)
	a.Register(CmdRestartSeat, d.restart)
	a.Register(CmdUpgradeSeat, d.upgrade)
	a.Register(CmdDestroySeat, d.destroy)
}
