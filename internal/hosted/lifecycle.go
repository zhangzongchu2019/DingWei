package hosted

// T-G4-50 · 生命周期四操作编排（M5 主线；组长 09:48:02Z／台账 4ecd2246＋业主 8971bddf）。
//
// 调用顺序铁律（G3 三问③＋停语义裁定）：
//  · 升级：先 ReissueHostedToken（轮换：新凭据被取走旧才作废）→ 节点起新容器 →
//    新容器健康上线后再删旧（D2）；失败自动回滚——旧容器与旧凭据全程不受影响；
//    暂停中的席位不可升级（先启用）。
//  · 停用：先 SuspendHostedAgent（返回前 4401 断开）→ 再停容器；
//    启用：先 ResumeHostedAgent（不重签）→ 再起容器。
//  · 销毁：先备份数据卷（留 14 天到期自动删，业主 8971bddf；恢复入口本期不做）→
//    RetireHostedAgent（4401＋吊销）→ 停删容器 → 清 XFS 项目配额 → 账号回池 → 配额释放。
//  · 轮换（Reissue）不算新建、不占新配额（G3 重审⑥）——本编排不触发配额校验。
//
// 归属与角色：操作者必须是该席所属业主或管理用户（归属由 identity 侧取——本层以入参 seatOwner 判，
// 接线时经 httpapi.N3.Actor 与 identity 的 Agent 记录给）。停用/销毁必须 confirm=true（D8，裸调必拒）。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
)

// BackupFailedError 备份不可用（契约：error=backup_failed＋detail 字段；detail 只供日志与审计，页面不读）。
type BackupFailedError struct{ Detail string }

func (e *BackupFailedError) Error() string { return ErrBackupFailed.Error() }
func (e *BackupFailedError) Unwrap() error { return ErrBackupFailed }

var (
	ErrNotOwner        = errors.New("not_owner")
	ErrConfirmRequired = errors.New("confirm_required")
	ErrNodeOffline     = errors.New("node_offline")
	ErrRolledBack      = errors.New("upgrade_failed_rolled_back")
	// invalid_state 复用包级 ErrInvalidState（store.go）。
)

// IdentitySuspender 停语义两接口（G3 ab3b4c5b 签名）。
type IdentitySuspender interface {
	SuspendHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	ResumeHostedAgent(ctx context.Context, agentID, by string) (bool, error)
	ReissueHostedToken(ctx context.Context, agentID, by string) (EnrollToken, error)
}

// VolumeBackuper 数据卷备份（业主 8971bddf：销毁先备份、留 14 天到期删除）。
// 🔴 目的地口径（组长 2026-10-01T11:23:25Z，台账 eccc54bc）：备份**统一送 dw.cmjrt.com 的 /data，
// 不落节点**——实现按此选址（节点侧只出卷内容，不落盘在节点本地）。
// 备份件须可读、内容与销毁前一致（D5 判据）。
type VolumeBackuper interface {
	BackupSeatVolume(ctx context.Context, nodeID, seatID string) (backupRef string, err error)
}

type Lifecycle struct {
	st      *Store
	pool    *PoolService
	idp     IdentityIssuer
	susp    IdentitySuspender
	gw      NodeGateway
	images  ImageCatalog
	backups VolumeBackuper
}

func NewLifecycle(st *Store, pool *PoolService, idp IdentityIssuer, susp IdentitySuspender, gw NodeGateway, images ImageCatalog, backups VolumeBackuper) *Lifecycle {
	return &Lifecycle{st: st, pool: pool, idp: idp, susp: susp, gw: gw, images: images, backups: backups}
}

// guard 归属与节点健康预检。🔴 归属本层强制（复验阻断 4：任何非空操作者都能动任意席位
// 不可接受）：操作者必须是建席者（＝归属业主，hosted_seat.created_by）；管理用户可在
// 升级/重启/停用/启用上代操作；销毁不走 guard——Destroy 里更严（业主 8971bddf：
// 管理者不能吊销别人的 Agent，isAdmin 不豁免）。
func (l *Lifecycle) guard(ctx context.Context, agentID, actorLC string, isAdmin bool) (Seat, error) {
	seat, err := l.st.GetSeat(ctx, agentID)
	if err != nil {
		return seat, err
	}
	if actorLC == "" || (seat.CreatedBy != "" && actorLC != seat.CreatedBy && !isAdmin) {
		return seat, ErrNotOwner
	}
	node, err := l.st.GetNode(ctx, seat.NodeID)
	if err != nil {
		return seat, err
	}
	if node.State != "online" {
		return seat, ErrNodeOffline
	}
	return seat, nil
}

// Upgrade 升级：默认最新（version=""）或指定版本 ref；先 Reissue 再起新，健康后删旧，失败自动回滚。
func (l *Lifecycle) Upgrade(ctx context.Context, agentID, actorLC, version string, isAdmin bool) error {
	seat, err := l.guard(ctx, agentID, actorLC, isAdmin)
	if err != nil {
		return err
	}
	if seat.State != "online" { // 暂停中不可升级（ab3b4c5b：先启用）；busy 同理
		return ErrInvalidState
	}
	imageRef := version
	if imageRef == "" {
		var ok bool
		imageRef, ok = l.images.Latest(seat.CLI)
		if !ok {
			return fmt.Errorf("%w: 无 %s 的登记镜像", ErrParams, seat.CLI)
		}
	}
	if imageRef == seat.ImageRef {
		return nil // 已是最新：幂等
	}
	if err := l.st.SetSeatState(ctx, agentID, "busy", "upgrade", "upgrade", json.RawMessage(`{"to":`+jsonStr(imageRef)+`}`)); err != nil {
		return err
	}
	// ① 先 Reissue（轮换：新凭据被取走旧才作废——旧容器不被提前踢下线）。
	// 🔴 复验阻断 2：新令牌必须随 upgrade_seat 下发（节点起新容器前写入令牌文件）——
	//    丢弃返回值＝轮换永不发生＋身份层多一枚悬空令牌。节点侧失败回滚时该令牌未取走，
	//    两小时自然过期（旧凭据全程有效）。
	tok, err := l.susp.ReissueHostedToken(ctx, agentID, actorLC)
	if err != nil {
		_ = l.st.SetSeatState(ctx, agentID, "online", "", "online", nil)
		return err
	}
	// ② 节点升级（先起新容器、健康后再删旧）。参数满足节点契约：image＋resources＋token_file
	//    一个都不能少（复验阻断 1——节点 validate 缺 resources 一律 ErrParams）。
	plan, _ := PlanByID(seat.Plan)
	params, _ := json.Marshal(map[string]any{
		"seat_id": agentID,
		"image":   imageRef,
		"resources": map[string]any{
			"memory_mb":  plan.MemMB,
			"cpu_shares": cpuShares(plan.VCPU),
			"disk_mb":    plan.DiskMB,
		},
		"token_file": map[string]string{
			"application_id": tok.ApplicationID,
			"poll_token":     tok.PollToken,
			"expires_at":     tok.ExpiresAt.UTC().Format(time.RFC3339),
		},
	})
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "upgrade_seat", Params: params})
	if err != nil || !res.OK {
		// 自动回滚：席位回到 online（旧容器与旧凭据未受影响——回滚本身即"旧态仍在"）
		_ = l.st.SetSeatState(ctx, agentID, "online", "", "online", json.RawMessage(`{"rolled_back":true}`))
		return ErrRolledBack
	}
	return l.st.setSeatImage(ctx, agentID, imageRef)
}

// setSeatImage 升级成功后更新镜像位并回到 online。
func (s *Store) setSeatImage(ctx context.Context, agentID, imageRef string) error {
	if _, err := s.pool.Exec(ctx, htx(), `
UPDATE g4_hosted_seat SET image_ref=$2, updated_at=now() WHERE agent_id=$1`, agentID, imageRef); err != nil {
		return err
	}
	return s.SetSeatState(ctx, agentID, "online", "", "online", json.RawMessage(`{"upgraded_to":`+jsonStr(imageRef)+`}`))
}

// Restart 重启：数据卷与工作目录保留（节点侧同卷重启）。
func (l *Lifecycle) Restart(ctx context.Context, agentID, actorLC string, isAdmin bool) error {
	seat, err := l.guard(ctx, agentID, actorLC, isAdmin)
	if err != nil {
		return err
	}
	if seat.State != "online" && seat.State != "error" {
		return ErrInvalidState
	}
	// 🔴 复验阻断 3：停用失败留下的席位是「容器活着但凭据已暂停」——重启若不先 Resume，
	//    席位回 online 而凭据仍 4401，页面显示运行中却永远连不上。Resume 幂等（未暂停则
	//    no-op），故一律先 Resume 再起。
	if _, err := l.susp.ResumeHostedAgent(ctx, agentID, actorLC); err != nil {
		return err
	}
	if err := l.st.SetSeatState(ctx, agentID, "busy", "restart", "restart", nil); err != nil {
		return err
	}
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "restart_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if err != nil || !res.OK {
		_ = l.st.SetSeatState(ctx, agentID, "error", "start_failed", "fail", nil)
		return ErrStartFailed
	}
	return l.st.SetSeatState(ctx, agentID, "online", "", "online", nil)
}

// Disable 停用：confirm 必带（D8）；先 Suspend 凭据（4401 断开）再停容器。
func (l *Lifecycle) Disable(ctx context.Context, agentID, actorLC string, isAdmin, confirm bool) error {
	if !confirm {
		return ErrConfirmRequired
	}
	seat, err := l.guard(ctx, agentID, actorLC, isAdmin)
	if err != nil {
		return err
	}
	if seat.State != "online" {
		return ErrInvalidState
	}
	// ① 先暂停凭据（返回前 4401/credential_suspended 断开；客户端不删凭据）
	if _, err := l.susp.SuspendHostedAgent(ctx, agentID, actorLC); err != nil {
		return err
	}
	// ② 再停容器
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "stop_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if err != nil || !res.OK {
		// 停容器失败 ⇒ 凭据已暂停是安全态（连接已不可能），如实上报，不回滚暂停
		_ = l.st.SetSeatState(ctx, agentID, "error", "node_offline", "fail", json.RawMessage(`{"after":"suspended"}`))
		return ErrNodeOffline
	}
	return l.st.SetSeatState(ctx, agentID, "disabled", "", "disable", nil)
}

// Enable 启用：先 Resume 凭据（不重签）再起容器。
func (l *Lifecycle) Enable(ctx context.Context, agentID, actorLC string, isAdmin bool) error {
	seat, err := l.guard(ctx, agentID, actorLC, isAdmin)
	if err != nil {
		return err
	}
	// disabled 或 error（停用失败留下的"凭据已暂停、容器状态未知"——联调第 7 条：
	// 否则停用失败后启用也回不去，席位永久卡死）都可启用；启用＝Resume＋起容器，正是恢复路径
	if seat.State != "disabled" && seat.State != "error" {
		return ErrInvalidState
	}
	if _, err := l.susp.ResumeHostedAgent(ctx, agentID, actorLC); err != nil {
		return err
	}
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "start_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if err != nil || !res.OK {
		_ = l.st.SetSeatState(ctx, agentID, "error", "start_failed", "fail", nil)
		return ErrStartFailed
	}
	return l.st.SetSeatState(ctx, agentID, "online", "", "online", json.RawMessage(`{"resumed":true}`))
}

// Destroy 销毁：confirm 必带（D8）；先备份卷（留 14 天）→ Retire 吊销 → 停删容器 → 清配额 → 账号回池。
// 暂停中可直接销毁（ab3b4c5b 规则）。
func (l *Lifecycle) Destroy(ctx context.Context, agentID, actorLC string, isAdmin, confirm bool) error {
	if !confirm {
		return ErrConfirmRequired
	}
	seat, err := l.st.GetSeat(ctx, agentID)
	if err != nil {
		return err
	}
	// 🔴 归属严格（复验阻断 4＋业主 8971bddf：管理者不能吊销别人的 Agent——isAdmin 不豁免）：
	//    只有建席者（＝归属业主）本人能销毁。
	if actorLC == "" || (seat.CreatedBy != "" && actorLC != seat.CreatedBy) {
		return ErrNotOwner
	}
	if seat.State == "destroyed" {
		return nil // 幂等
	}
	// ① 先备份数据卷（业主 8971bddf：留 14 天到期自动删；备份件须可读且与销毁前一致——D5）
	backupRef, err := l.backups.BackupSeatVolume(ctx, seat.NodeID, agentID)
	if err != nil {
		// 🔴 契约机器码不拼原因（联调第 4 条）：细节进日志/审计，错误只回 backup_failed
		log.Printf("hosted destroy backup failed agent=%s: %v", agentID, err)
		return &BackupFailedError{Detail: err.Error()}
	}
	// ② Retire 吊销（返回前 4401 断开，D5 五秒口径；自备席位不可走此路——G3 侧约束）
	if _, err := l.idp.RetireHostedAgent(ctx, agentID, actorLC); err != nil {
		return err
	}
	// ③ 停删容器＋清 XFS 项目配额（节点侧 destroy_seat 内含配额清除；离线节点也可销毁——
	//    节点离线时凭据已吊销是安全态，容器清理由节点重连后对账，登记 degraded 而不阻塞）
	degraded := false
	node, nerr := l.st.GetNode(ctx, seat.NodeID)
	if nerr == nil && node.State == "online" {
		res, serr := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "destroy_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
		if serr != nil || !res.OK {
			_ = l.st.SetSeatState(ctx, agentID, "error", "node_offline", "fail", json.RawMessage(`{"after":"retired"}`))
			return ErrNodeOffline
		}
	} else {
		// 🔴 复验阻断 5：节点离线销毁必须留对账记录——凭据已吊销是安全态，容器与配额
		//    待节点重连后对账清理；审计一条＋销毁事件带 reconcile_pending 标记。
		degraded = true
		_ = l.st.AddAudit(ctx, actorLC, "seat.destroy_degraded", agentID,
			json.RawMessage(`{"node":`+jsonStr(seat.NodeID)+`,"reason":"node_offline","reconcile":"pending"}`))
	}
	// ④ 账号回池（组长定：自动回池＋审计）
	if seat.AccountRef.Valid {
		if err := l.pool.Unbind(ctx, actorLC, true, seat.AccountRef.String); err != nil {
			return err
		}
	}
	detail := `{"backup":` + jsonStr(backupRef) + `,"ttl_days":14`
	if degraded {
		detail += `,"reconcile_pending":true`
	}
	return l.st.SetSeatState(ctx, agentID, "destroyed", "", "destroy", json.RawMessage(detail+`}`))
}
