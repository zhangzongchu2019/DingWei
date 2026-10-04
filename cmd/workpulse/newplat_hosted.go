package main

// G4 托管装配（组长裁定 88928305：归 kimi）：在 wireNewPlatform 里建 hosted.Store／PoolService／
// nodegate.Gate／静态登记表／Orchestrator／Lifecycle，挂 console hosted 路由与节点接入点，
// 并把列表注入那一行接上（cfg.hosted = orch.HostedItem——与 API.Hosted 逐字同签名，
// opus50tc2k/g3-hosted-hook 9fc73077 起不需要适配层）。
//
// 开关：DW_G4_MASTER_KEY_FILE 存在才装（团队主密钥文件，≤600；缺席＝托管面不注册、记日志——
// 比带着解不了密的账号池跑半挂安全）。登记表 env：DW_G4_COMBOS_FILE／DW_G4_IMAGES_FILE
// （缺省＝空表：一切组合不兼容、无登记镜像——B12 验通回板前的诚实态）。

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
	"github.com/zhangzongchu2019/dingwei/internal/identity"
	"github.com/zhangzongchu2019/dingwei/internal/identity/httpapi"
	"github.com/zhangzongchu2019/dingwei/internal/m8"
	"github.com/zhangzongchu2019/dingwei/internal/nodegate"
	"github.com/zhangzongchu2019/dingwei/internal/repo"
)

// wireHosted G4 装配。返回 false＝未装（缺主密钥配置）；错误＝装配失败（启动即拒）。
// 须在 httpapi.API 构造之前调用（cfg.hosted 注入点在其上）。
func wireHosted(ctx context.Context, mux httpapi.Mux, hub *m8.Hub, cfg *newPlatformConfig, pool *repo.Pool,
	idStore identity.Store, svc *identity.Service, actor func(r *http.Request) (string, error)) (bool, error) {
	mkPath := os.Getenv("DW_G4_MASTER_KEY_FILE")
	if mkPath == "" {
		log.Printf("newplat hosted: DW_G4_MASTER_KEY_FILE 未设置——托管面不注册（账号池无法解密，半挂比缺席更糟）")
		return false, nil
	}
	hst := hosted.NewStore(pool)
	if err := hst.ApplySchema(ctx); err != nil { // 幂等 SchemaDDL（迁移账本：一次性变更无 ALTER）
		return false, err
	}
	poolSvc, err := hosted.NewPoolService(hst, mkPath)
	if err != nil {
		return false, err
	}
	combos, err := hosted.LoadCombos(os.Getenv("DW_G4_COMBOS_FILE"))
	if err != nil {
		return false, err
	}
	images, err := hosted.LoadImages(os.Getenv("DW_G4_IMAGES_FILE"))
	if err != nil {
		return false, err
	}
	gate := nodegate.New(hst)
	idp := newHostedIdentity[hosted.EnrollToken, hosted.IssuedAgent](svc, hosted.ErrGroupNotFound, hosted.ErrGroupClosed)
	orch := hosted.NewOrchestrator(hst, poolSvc, idp, gate, combos, images)
	// T-G4-53 备份管线（真实现）：销毁先备份——节点出卷流式回传，落 dw 的 /data，14 天到期删
	backupDir := os.Getenv("DW_G4_BACKUP_DIR")
	if backupDir == "" {
		backupDir = "/data/dingwei-newplat/backups/hosted"
	}
	backups := hosted.NewHubBackuper(hst, gate, cfg.publicOrigin, backupDir)
	orch.SetBackuper(backups) // 离线销毁的席位在重连对账时先备份、成功才删（lead-g4-t53-review 第 4 条）
	mux.HandleFunc("POST /api/node/backup/{id}", backups.BackupHandler().ServeHTTP)
	go func() { // 到期清扫（每小时一轮；失败记日志不炸进程）
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for range t.C {
			if err := backups.SweepExpiredBackups(context.Background(), time.Now()); err != nil {
				log.Printf("hosted backup sweep: %v", err)
			}
		}
	}()
	life := hosted.NewLifecycle(hst, poolSvc, idp, idp, gate, images, backups)
	gate.OnConnect = orch.ReconcileNode // 节点重连对账（离线销毁补清——组长 7fabddb0 ⑤）

	mux.HandleFunc("GET /api/node/connect", gate.Handler().ServeHTTP) // 节点代理拨入（Bearer 节点凭据）
	registerHostedRoutes(mux, hostedDeps{
		orch: orch, life: life, qs: hosted.NewQuotaService(hst, idp),
		actor: actor,
		ownsAgent: func(r *http.Request, userLC, agentID string) bool {
			ags, err := svc.ListAgents(r.Context(), userLC)
			if err != nil {
				return false
			}
			for _, a := range ags {
				if a.ID == agentID {
					return true
				}
			}
			return false
		},
		isManager: func(r *http.Request, userLC string) bool {
			u, err := idStore.GetUser(r.Context(), userLC)
			return err == nil && (u.Type == identity.UserPlatformAdmin || u.Type == identity.UserManager)
		},
		listUsers: func(r *http.Request) ([]hostedUser, error) {
			us, err := idStore.ListUsers(r.Context())
			if err != nil {
				return nil, err
			}
			out := make([]hostedUser, 0, len(us))
			for _, u := range us {
				// 配额行按小写 ID 存取（UserID 是展示用的原样大小写，拿它查配额会查不到）。
				out = append(out, hostedUser{ID: u.UserID, IDLC: u.UserIDLC(), ShortName: u.ShortName})
			}
			return out, nil
		},
	})
	cfg.hosted = orch.HostedItem // 那一行：G3 列表注入点接上
	// online 语义（板 ac33b1cb）：托管 Agent 以专属凭据真正连入时才 starting → online；
	// 永不连入的由周期清扫按 starting 5 分钟清成 start_timeout。原先两者都没接：容器一起就 online，清扫也没人调。
	hub.OnHostedAgentOnline = func(ctx context.Context, agentID string) {
		if err := orch.MarkOnline(ctx, agentID); err != nil {
			log.Printf("newplat hosted: MarkOnline agent=%s: %v", agentID, err)
		}
	}
	go runHostedSweeper(ctx, orch, hostedSweepEvery)
	return true, nil
}

// hostedSweepEvery 超时清扫周期：远小于最短的阶段时限（starting 5 分钟），超时后至多晚这么久被清。
var hostedSweepEvery = 30 * time.Second // 变量：接线用例里缩短以实测清扫真在跑

// runHostedSweeper 周期调用 SweepTimeouts（排队 10m／拉镜像 30m／启动 5m），ctx 结束即停。
func runHostedSweeper(ctx context.Context, orch *hosted.Orchestrator, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			orch.SweepTimeouts(ctx, now)
		}
	}
}

// notReadyBackups 备份管线未就位期的占位（T-G4-53 真实现前）：销毁一律 backup_failed——
// fail-closed，宁不可用不跳过备份（业主 8971bddf：销毁先备份留 14 天）。
type notReadyBackups struct{}

func (notReadyBackups) BackupSeatVolume(ctx context.Context, nodeID, seatID string) (string, error) {
	return "", errors.New("backup_pipeline_not_ready")
}
