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
	"strings"
	"time"
)

var (
	ErrNotOwner        = errors.New("not_owner")
	ErrConfirmRequired = errors.New("confirm_required")
	ErrNodeOffline     = errors.New("node_offline")
	ErrRolledBack      = errors.New("upgrade_failed_rolled_back")
	ErrStartFailed     = errors.New("start_failed")  // 节点在线、但报告起容器失败（契约第四节「进度/操作」）
	ErrBackupFailed    = errors.New("backup_failed") // 销毁前备份不可用（组长裁定 lead-g4-joint-run-1 第 4 条）
	// 刷新凭据（0.2.1.101）：节点未升级（不认识 refresh_cred_seat）／节点写凭据件失败——两者均未重启，席位回原状态。
	ErrNodeUnsupported  = errors.New("node_unsupported")
	ErrCredInjectFailed = errors.New("credential_inject_failed")
	// invalid_state 复用包级 ErrInvalidState（store.go）。
)

// BackupFailedError 销毁前备份失败：机器码 backup_failed，原因放 Detail（页面不读，供日志与审计）。
type BackupFailedError struct{ Cause error }

func (e *BackupFailedError) Error() string  { return ErrBackupFailed.Error() }
func (e *BackupFailedError) Unwrap() error  { return ErrBackupFailed }
func (e *BackupFailedError) Detail() string { return e.Cause.Error() }

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
	// created_by 为空串（列 NOT NULL 但可为空串）：归属不明一律拒，管理用户也不豁免（复验 REVIEW-G4-T50-REWORK 第四节）。
	if actorLC == "" || seat.CreatedBy == "" || (actorLC != seat.CreatedBy && !isAdmin) {
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
	if err := l.quotaStartGuard(ctx, seat); err != nil {
		return err
	}
	if seat.State != "online" { // 暂停中不可升级（ab3b4c5b：先启用）；busy 同理
		return ErrInvalidState
	}
	// 套餐查不到不能吞（复验第四节）：零资源下发会被节点以参数错拒、再被译成 upgrade_failed_rolled_back，信息误导。
	// 在改任何状态、签任何令牌之前判。
	plan, ok := PlanByID(seat.Plan)
	if !ok {
		return fmt.Errorf("%w: 席位套餐 %q 未登记", ErrParams, seat.Plan)
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
	params, _ := json.Marshal(map[string]any{
		"seat_id":   agentID,
		"image":     imageRef,
		"resources": l.st.resourceParams(ctx, seat.NodeID, plan),
		"token_file": map[string]string{
			"application_id": tok.ApplicationID,
			"poll_token":     tok.PollToken,
			"expires_at":     tok.ExpiresAt.UTC().Format(time.RFC3339),
		},
		"cred_kind": seat.CredKind, // 容器启动契约：升级后的新容器同形态（a26a825f528a 解冻这一行）
		"cli":       seat.CLI,      // 升级后的新容器同一 CLI（组长 R-B12-SEATMODE 第 2 条）
		"model":     seat.Model,    // 包 B2 修订 3：升级用落库记录值（不随节点默认漂移）
	})
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "upgrade_seat", Params: params})
	if err != nil {
		// 节点不通（第 7 条）：席位回 online、旧凭据未动（新令牌未被取走，两小时自然过期），答 node_offline。
		_ = l.st.SetSeatState(ctx, agentID, "online", "", "node_offline", nil)
		return ErrNodeOffline
	}
	if capacityErr := l.st.capacityResult(ctx, seat.NodeID, res); capacityErr != nil {
		_, _ = l.st.TransitionSeatState(ctx, agentID, "busy", seat.State, seat.StateDetail, "capacity_refused", nil)
		return capacityErr
	}
	if l.quotaResult(ctx, seat, res) {
		return ErrSoftQuotaHeld
	}
	if !res.OK {
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
	if err := l.quotaStartGuard(ctx, seat); err != nil {
		return err
	}
	if seat.State != "online" && seat.State != "error" {
		return ErrInvalidState
	}
	// 🔴 复验阻断 3：凭据处于暂停的席位重启时若不先 Resume，席位回 online 而凭据仍 4401，
	//    页面显示运行中却永远连不上。Resume 幂等（未暂停则 no-op），故一律先 Resume 再起。
	alreadyActive, err := l.susp.ResumeHostedAgent(ctx, agentID, actorLC)
	if err != nil {
		return err
	}
	// 🔴 先置 starting（detail=restart）再发节点命令（组长 lead-enable-restart-race ①）：容器里的 Agent 可能在节点回 ok
	//    之前就连入，MarkOnline 只认 starting —— 若此刻席位还不是 starting，这次连入就丢了，5 分钟后被误记 start_timeout。
	//    失败分支一律条件迁移（当前仍是 starting 才改）：命令途中 Agent 已连入置 online 的，不被回写盖掉。
	if err := l.st.SetSeatState(ctx, agentID, "starting", seatStartingRestart, "restart", nil); err != nil {
		// 写库失败：席位没动，刚才的 Resume 也撤回（与 Enable 同处对齐，复验 R-G4-RACE-FOLD 第 2 条）。
		if !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return err
	}
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "restart_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if err != nil {
		// 节点不通（组长裁定 lead-g4-joint-run-1 第 7 条）：席位状态回原样、凭据回原样，答 node_offline。
		moved, _ := l.st.TransitionSeatState(ctx, agentID, "starting", seat.State, seat.StateDetail, "node_offline", nil)
		if moved && !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return ErrNodeOffline
	}
	if capacityErr := l.st.capacityResult(ctx, seat.NodeID, res); capacityErr != nil {
		moved, _ := l.st.TransitionSeatState(ctx, agentID, "starting", seat.State, seat.StateDetail, "capacity_refused", nil)
		if moved && !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return capacityErr
	}
	if l.quotaResult(ctx, seat, res) {
		if !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return ErrSoftQuotaHeld
	}
	if !res.OK {
		_, _ = l.st.TransitionSeatState(ctx, agentID, "starting", "error", "start_failed", "fail", nil)
		return ErrStartFailed
	}
	// 容器重启 ≠ online（组长裁定，板 79498c60，与创建同口径）：已停在 starting，Agent 以专属凭据连入（MarkOnline）才 online；
	// 永不连入 ⇒ 清扫按 5 分钟记 start_timeout（这类既有席位只记错、不吊销，见 SweepTimeouts；之后晚连入仍可置 online）。
	return nil
}

// RefreshCred 刷新凭据（R-V2-ACCOUNT-REFRESH-CRED）：解密本席所引用账号的**池内副本**（属主最近一次登记／换密的那份，
// 不是容器内 Codex 续期后的件）→ 节点原子覆写 /work/account-credential → 重启本席。
// 🔴 对 token_file：即使账号密钥未改，也会以池内副本覆盖本席登录态（容器内续期后的令牌被换回）。
// 只限席位属主本人（isAdmin 不豁免——管理员不能代刷）；席位须 online 或 error；账号须同属主、未停用。
// 节点回报三分：unknown_command_type（旧节点）／credential_write_failed —— 未重启，席位回原状态；
// 其余失败（restart_failed 等）—— 凭据件可能已换、容器状态未知，记 error/start_failed。成功则停 starting 等 MarkOnline。
func (l *Lifecycle) RefreshCred(ctx context.Context, agentID, actorLC string) error {
	seat, err := l.guard(ctx, agentID, actorLC, false)
	if err != nil {
		return err
	}
	if err := l.quotaStartGuard(ctx, seat); err != nil {
		return err
	}
	if seat.State != "online" && seat.State != "error" {
		return ErrInvalidState
	}
	if !seat.AccountRef.Valid {
		return ErrAccountNotAllowed
	}
	acct, err := l.st.GetAccount(ctx, seat.AccountRef.String)
	if err != nil || acct.State == "disabled" || acct.OwnerLC == "" || acct.OwnerLC != seat.CreatedBy {
		return ErrAccountNotAllowed
	}
	// 同 Restart「复验阻断 3」：凭据暂停中的席位（如 Disable 停容器失败落 error、凭据仍暂停）刷新后若不 Resume，
	// 容器重启而平台凭据仍 4401 ⇒ 永远连不上、5 分钟后 start_timeout。Resume 幂等；席位回原状态的分支一律撤回本次 Resume。
	alreadyActive, err := l.susp.ResumeHostedAgent(ctx, agentID, actorLC)
	if err != nil {
		return err
	}
	prevState, prevDetail := seat.State, seat.StateDetail
	back := func(kind string) {
		moved, _ := l.st.TransitionSeatState(ctx, agentID, "starting", prevState, prevDetail, kind, json.RawMessage(`{"op":"refresh_cred"}`))
		if moved && !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
	}
	// 先置 starting（detail=refresh）再发命令（同 Restart：防 MarkOnline 早到丢失，lead-enable-restart-race ①）。
	if err := l.st.SetSeatState(ctx, agentID, "starting", seatStartingRefresh, "refresh_cred", nil); err != nil {
		if !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return err
	}
	cred, err := l.pool.decryptForSeat(ctx, actorLC, seat.AccountRef.String, agentID, "refresh")
	if err != nil {
		back("fail")
		return ErrAccountNotAllowed
	}
	params, _ := json.Marshal(map[string]any{"seat_id": agentID, "credential_file": map[string]string{"content": cred}})
	cred = ""
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "refresh_cred_seat", Params: params})
	if err != nil {
		back("node_offline")
		return ErrNodeOffline
	}
	if capacityErr := l.st.capacityResult(ctx, seat.NodeID, res); capacityErr != nil {
		back("capacity_refused")
		return capacityErr
	}
	if l.quotaResult(ctx, seat, res) {
		return ErrSoftQuotaHeld
	}
	if !res.OK {
		switch {
		case strings.HasPrefix(res.Error, "unknown_command_type"):
			back("fail")
			return ErrNodeUnsupported
		case strings.HasPrefix(res.Error, "credential_write_failed"):
			back("fail")
			return ErrCredInjectFailed
		default:
			_, _ = l.st.TransitionSeatState(ctx, agentID, "starting", "error", "start_failed", "fail", json.RawMessage(`{"op":"refresh_cred"}`))
			return ErrStartFailed
		}
	}
	return nil
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
	alreadySuspended, err := l.susp.SuspendHostedAgent(ctx, agentID, actorLC)
	if err != nil {
		return err
	}
	// ② 再停容器
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "stop_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if err != nil || !res.OK {
		// 🔴 停容器没成（节点不通或节点报失败）⇒ 撤回刚才的暂停、席位状态不动，答 node_offline
		//    （组长裁定 lead-g4-joint-run-1 第 7 条：停用不得先暂停凭据再失败——那会留下「页面说在跑、凭据却拒连」）。
		if !alreadySuspended {
			l.undo(ctx, agentID, actorLC, "resume")
		}
		_ = l.st.AddAudit(ctx, actorLC, "seat.disable_failed", agentID, json.RawMessage(`{"reason":"node_offline"}`))
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
	if err := l.quotaStartGuard(ctx, seat); err != nil {
		return err
	}
	if seat.State != "disabled" {
		return ErrInvalidState
	}
	alreadyActive, err := l.susp.ResumeHostedAgent(ctx, agentID, actorLC)
	if err != nil {
		return err
	}
	// 同重启：先置 starting（detail=enable）再发命令（lead-enable-restart-race ①）。
	if err := l.st.SetSeatState(ctx, agentID, "starting", seatStartingEnable, "enable", json.RawMessage(`{"resumed":true}`)); err != nil {
		if !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return err
	}
	res, err := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "start_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if err == nil {
		if capacityErr := l.st.capacityResult(ctx, seat.NodeID, res); capacityErr != nil {
			moved, _ := l.st.TransitionSeatState(ctx, agentID, "starting", seat.State, seat.StateDetail, "capacity_refused", nil)
			if moved && !alreadyActive {
				l.undo(ctx, agentID, actorLC, "suspend")
			}
			return capacityErr
		}
	}
	if err == nil && l.quotaResult(ctx, seat, res) {
		if !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return ErrSoftQuotaHeld
	}
	if err != nil || !res.OK {
		// 节点不通（第 7 条）或节点报起不来（lead-enable-restart-race ①）：席位回 disabled、撤回恢复。
		// 条件迁移：命令途中 Agent 已连入置 online 的不回写，也不再暂停它的凭据。
		kind, ret := "node_offline", ErrNodeOffline
		if err == nil {
			kind, ret = "fail", ErrStartFailed
		}
		moved, _ := l.st.TransitionSeatState(ctx, agentID, "starting", "disabled", "", kind, json.RawMessage(`{"op":"enable"}`))
		if moved && !alreadyActive {
			l.undo(ctx, agentID, actorLC, "suspend")
		}
		return ret
	}
	// 同重启：容器起来 ≠ online，等 Agent 连入（板 79498c60）。
	return nil
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
	if actorLC == "" || seat.CreatedBy == "" || actorLC != seat.CreatedBy {
		return ErrNotOwner
	}
	if seat.State == "destroyed" {
		return nil // 幂等
	}
	// From the first destructive side effect onward this is server-owned cleanup.
	// A browser closing its lifecycle request must not cancel backup bookkeeping,
	// credential retirement, volume removal, or account release halfway through.
	ctx = context.WithoutCancel(ctx)
	node, nerr := l.st.GetNode(ctx, seat.NodeID)
	if nerr != nil || node.State != "online" {
		return l.destroyOffline(ctx, seat, actorLC)
	}
	// ⓪ 先停容器（组长取甲，2026-10-02）：销毁本来就要停；先停再备份，数据一致，tar 也不会因「打包期间文件有变动」
	//    退出 1（「tar 非零即失败」保持不变）。停不下来 ⇒ node_offline，不吊销、不删、席位状态不变。
	res, serr := l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "stop_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if serr != nil || !res.OK {
		return ErrNodeOffline
	}
	// ① 再备份数据卷（业主 8971bddf：留 14 天到期自动删；备份件须可读且与销毁前一致——D5）
	backupRef, err := l.backups.BackupSeatVolume(ctx, seat.NodeID, agentID)
	if err != nil {
		// 容器已停、备份没成：不吊销、不删数据；席位如实记 error／backup_failed（Agent 确实不在线了），可再销毁或重启。
		_ = l.st.SetSeatState(ctx, agentID, "error", "backup_failed", "fail", json.RawMessage(`{"after":"stopped"}`))
		return &BackupFailedError{Cause: err}
	}
	// ② Retire 吊销（返回前 4401 断开，D5 五秒口径；自备席位不可走此路——G3 侧约束）
	if _, err := l.idp.RetireHostedAgent(ctx, agentID, actorLC); err != nil {
		return err
	}
	// ③ 删容器＋清 XFS 项目配额（节点侧 destroy_seat 内含配额清除）
	res, serr = l.gw.Send(ctx, seat.NodeID, NodeCommand{Type: "destroy_seat", Params: json.RawMessage(`{"seat_id":` + jsonStr(agentID) + `}`)})
	if serr != nil || !res.OK {
		_ = l.st.SetSeatState(ctx, agentID, "error", "node_offline", "fail", json.RawMessage(`{"after":"retired"}`))
		return ErrNodeOffline
	}
	// ④ 释放本席对账号的引用（0.2.1.101：只动本席，账号仍可被本人其他席位共用；审计照记）
	if seat.AccountRef.Valid {
		if err := l.pool.Release(ctx, actorLC, agentID); err != nil {
			return err
		}
	}
	return l.st.SetSeatState(ctx, agentID, "destroyed", "", "destroy",
		json.RawMessage(`{"backup":`+jsonStr(backupRef)+`,"ttl_days":14}`))
}

// destroyOffline 节点离线的销毁（组长裁定 lead-g4-t53-review 第 4 条）。「离线可销毁」与「先备份再删数据」（业主 8971bddf）两条同时成立：
//   - 凭据立即吊销（Retire）、账号回池、席位记 destroyed —— 销毁的首要目的是立刻断掉凭据，在线与否都要做；
//   - 此刻节点不通，备份做不了，数据也删不了 ⇒ 销毁事件带 reconcile_pending＋backup_pending，数据一字不动；
//   - 节点重连时 ReconcileNode 先备份、备份成功才 destroy_seat；备份失败留待下次，不删数据。
//
// 原先离线销毁第一步就是经网关备份 ⇒ 节点不通即 backup_failed，「离线可销毁」实际走不通（复验第四节）。
func (l *Lifecycle) destroyOffline(ctx context.Context, seat Seat, actorLC string) error {
	if _, err := l.idp.RetireHostedAgent(ctx, seat.AgentID, actorLC); err != nil {
		return err
	}
	_ = l.st.AddAudit(ctx, actorLC, "seat.destroy_degraded", seat.AgentID,
		json.RawMessage(`{"node":`+jsonStr(seat.NodeID)+`,"reason":"node_offline","reconcile":"pending","backup":"pending"}`))
	if seat.AccountRef.Valid {
		if err := l.pool.Release(ctx, actorLC, seat.AgentID); err != nil {
			return err
		}
	}
	return l.st.SetSeatState(ctx, seat.AgentID, "destroyed", "", "destroy",
		json.RawMessage(`{"ttl_days":14,"reconcile_pending":true,"backup_pending":true}`))
}

// undo 撤回一次凭据动作（节点不通时把凭据恢复到操作前）。撤回本身失败只记审计——
// 不能让第二个错误盖住真正的原因（node_offline），也不能静默丢掉。
func (l *Lifecycle) undo(ctx context.Context, agentID, actorLC, op string) {
	var err error
	switch op {
	case "resume":
		_, err = l.susp.ResumeHostedAgent(ctx, agentID, actorLC)
	case "suspend":
		_, err = l.susp.SuspendHostedAgent(ctx, agentID, actorLC)
	}
	if err != nil {
		_ = l.st.AddAudit(ctx, actorLC, "seat.credential_undo_failed", agentID,
			json.RawMessage(`{"undo":`+jsonStr(op)+`,"error":`+jsonStr(err.Error())+`}`))
	}
}
