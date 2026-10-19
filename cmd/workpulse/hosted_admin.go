package main

// hosted-admin：托管面的运维小工具（B12/联调播种用；本期无节点登记与账号登记的管理端点，
// 平面由后续切片补——本件与 bootstrap-admin 同型：连同一 PG，经 hosted 服务层写，不手写 SQL）。
//
// 用法：
//   workpulse hosted-admin register-node --id nd_106 --name 106 [--labels '{"pool":"x"}']
//     [--quota-mode soft --quota-policy-revision <approved-node-policy-sha256>]
//     ⇒ 打印一次性节点凭据（只此一次；落库只存哈希）
//   workpulse hosted-admin assign-owner --account <acct_id> --owner <user_lc>
//     ⇒ 只给无主存量账号指派属主（已有主不可改）
//   workpulse hosted-admin account-disable|account-enable --account <acct_id>
//     ⇒ 管理停用／启用（停用受「无存活引用」守卫）
//
// 0.2.1.101（R-V2-ACCOUNT-SEED-CLI）：seed-account（管理员代人登记）已删除——登记一律由属主本人走
// POST /api/console/llm-accounts；管理员不碰密钥、不能换密。
//
// 环境：DW_NEWPLAT_PG_DSN（同一测试/生产库）；账号子命令另需 DW_G4_MASTER_KEY_FILE（PoolService 装配所需，不解密）。
//
// 注意：本工具每次运行先 openNewPlatformStore（含迁移账本）再 ApplySchema——对生产库同样会执行，
// 与 Hub 启动同行为，运维须知（tosonnet55tc2k 复验观察）。

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

func runHostedAdmin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: hosted-admin register-node|assign-owner|account-disable|account-enable ...")
	}
	ctx := context.Background()
	dsn := os.Getenv("DW_NEWPLAT_PG_DSN")
	_, pool, closeStore, err := openNewPlatformStore(ctx, dsn)
	if err != nil {
		return err
	}
	defer closeStore()
	st := hosted.NewStore(pool)
	if err := st.ApplySchema(ctx); err != nil {
		return err
	}
	switch args[0] {
	case "register-node":
		fs := flag.NewFlagSet("register-node", flag.ContinueOnError)
		id := fs.String("id", "", "节点 ID（nd_ 前缀）")
		name := fs.String("name", "", "节点名")
		labels := fs.String("labels", "{}", "标签 JSON（如 pool）")
		// R-G4-HOSTED-ADMIN-TESTS：默认 29/1 仅联调用；接真节点时按节点实际版本显式传。
		dockerVer := fs.String("docker-version", "29", "节点 Docker 主版本（默认 29 仅联调）")
		protoVer := fs.Int("protocol-version", 1, "节点协议版本（默认 1 仅联调）")
		quotaMode := fs.String("quota-mode", "hard", "磁盘配额模式 hard 或 soft（soft 无硬磁盘配额，须另获批准）")
		quotaRevision := fs.String("quota-policy-revision", "", "hard／soft 均必填：批准的节点策略 SHA256（64 位小写十六进制）")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *name == "" {
			return fmt.Errorf("--id 与 --name 必填")
		}
		secret, err := st.RegisterNode(ctx, hosted.Node{ID: *id, Name: *name, Labels: []byte(*labels),
			DockerVersion: *dockerVer, ProtocolVersion: *protoVer,
			QuotaMode: *quotaMode, QuotaPolicyRevision: *quotaRevision}, "hosted-admin")
		if err != nil {
			return err
		}
		fmt.Printf("节点凭据（只显示这一次，落节点 0600 文件后即弃）: %s\n", secret)
		return nil
	case "assign-owner", "account-disable", "account-enable":
		fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
		acct := fs.String("account", "", "账号 ID（acct_ 前缀）")
		owner := fs.String("owner", "", "属主小写用户 ID（只 assign-owner 用）")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *acct == "" || (args[0] == "assign-owner" && *owner == "") {
			return fmt.Errorf("--account 必填；assign-owner 另需 --owner")
		}
		svc, err := hosted.NewPoolService(st, os.Getenv("DW_G4_MASTER_KEY_FILE"))
		if err != nil {
			return err
		}
		switch args[0] {
		case "assign-owner":
			err = svc.AssignOwner(ctx, "hosted-admin", true, *acct, strings.ToLower(strings.TrimSpace(*owner)))
		case "account-disable":
			err = svc.SetStateAdmin(ctx, "hosted-admin", true, *acct, true)
		default:
			err = svc.SetStateAdmin(ctx, "hosted-admin", true, *acct, false)
		}
		if err != nil {
			return err
		}
		fmt.Printf("%s 完成\n", args[0])
		return nil
	default:
		return fmt.Errorf("未知子命令 %q", args[0])
	}
}
