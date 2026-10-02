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

// maxSecretStdinBytes 凭据 stdin 读入上限（1MiB；凭据件 KB 级，超限即拒）。
const maxSecretStdinBytes = 1 << 20

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
		// R-G4-HOSTED-ADMIN-TESTS：默认 29/1 仅联调用；接真节点时按节点实际版本显式传。
		dockerVer := fs.String("docker-version", "29", "节点 Docker 主版本（默认 29 仅联调）")
		protoVer := fs.Int("protocol-version", 1, "节点协议版本（默认 1 仅联调）")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if *id == "" || *name == "" {
			return fmt.Errorf("--id 与 --name 必填")
		}
		secret, err := st.RegisterNode(ctx, hosted.Node{ID: *id, Name: *name, Labels: []byte(*labels),
			DockerVersion: *dockerVer, ProtocolVersion: *protoVer}, "hosted-admin")
		if err != nil {
			return err
		}
		fmt.Printf("节点凭据（只显示这一次，落节点 0600 文件后即弃）: %s\n", secret)
		return nil
	case "seed-account":
		fs := flag.NewFlagSet("seed-account", flag.ContinueOnError)
		fs.Bool("secret-stdin", true, "凭据从标准输入读（唯一通道；该开关只是显式标记）")
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
		// R-G4-HOSTED-ADMIN-TESTS：stdin 读入限 1MiB（凭据件 KB 级，1MiB 已极宽；
		// 超限即拒——LimitReader 多读一字节探溢出，不静默截断）。
		raw, err := io.ReadAll(io.LimitReader(os.Stdin, maxSecretStdinBytes+1))
		if err != nil {
			return err
		}
		if int64(len(raw)) > maxSecretStdinBytes {
			return fmt.Errorf("凭据超长（上限 %d 字节）", maxSecretStdinBytes)
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
