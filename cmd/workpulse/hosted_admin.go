package main

// hosted-admin：托管面的运维小工具（B12/联调播种用；本期无节点登记与账号登记的管理端点，
// 平面由后续切片补——本件与 bootstrap-admin 同型：连同一 PG，经 hosted 服务层写，不手写 SQL）。
//
// 用法：
//   workpulse hosted-admin register-node --id nd_106 --name 106 [--labels '{"pool":"x"}']
//     ⇒ 打印一次性节点凭据（只此一次；落库只存哈希）
//   workpulse hosted-admin seed-account --name team-codex --kind token_file --cli codex --llm gpt \
//       --grant-to <user_lc> --secret-stdin  < 凭据件
//     ⇒ 凭据从标准输入读（不进命令行、不进 shell 历史）；按用户授权（grant-to 可省）
//
// 环境：DW_NEWPLAT_PG_DSN（同一测试/生产库）；seed-account 另需 DW_G4_MASTER_KEY_FILE。

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/hosted"
)

func runHostedAdmin(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("用法: hosted-admin register-node|seed-account ...")
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
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *name == "" {
			return fmt.Errorf("--id 与 --name 必填")
		}
		secret, err := st.RegisterNode(ctx, hosted.Node{ID: *id, Name: *name, Labels: []byte(*labels),
			DockerVersion: "29", ProtocolVersion: 1}, "hosted-admin")
		if err != nil {
			return err
		}
		fmt.Printf("节点凭据（只显示这一次，落节点 0600 文件后即弃）: %s\n", secret)
		return nil
	case "seed-account":
		fs := flag.NewFlagSet("seed-account", flag.ContinueOnError)
		name := fs.String("name", "", "账号名")
		kind := fs.String("kind", "", "api_key|token_file")
		cli := fs.String("cli", "", "如 codex")
		llm := fs.String("llm", "", "如 gpt")
		grantTo := fs.String("grant-to", "", "授权给该用户（可省）")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *name == "" || *cli == "" || *llm == "" || (*kind != "api_key" && *kind != "token_file") {
			return fmt.Errorf("--name/--cli/--llm 必填，--kind 须 api_key|token_file")
		}
		svc, err := hosted.NewPoolService(st, os.Getenv("DW_G4_MASTER_KEY_FILE"))
		if err != nil {
			return err
		}
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return err
		}
		a, err := svc.Register(ctx, "hosted-admin", true, *name, *kind, *cli, *llm, strings.TrimSpace(string(raw)))
		if err != nil {
			return err
		}
		if *grantTo != "" {
			if err := svc.Grant(ctx, "hosted-admin", true, a.ID, *grantTo); err != nil {
				return err
			}
		}
		fmt.Printf("账号已入池：%s（%s/%s/%s）\n", a.Name, a.Kind, a.CLI, a.LLM)
		return nil
	default:
		return fmt.Errorf("未知子命令 %q", args[0])
	}
}
