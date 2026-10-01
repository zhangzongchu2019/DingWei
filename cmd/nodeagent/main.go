// nodeagent 节点代理（T-G4-10；部署边界：受 systemd 管理的宿主进程，见设计件第七节）。
// 配置仅五项（环境变量注入，机密只给路径不给值）：
//
//	NODEAGENT_HUB_URL          wss://hub/api/node/connect（必须 wss）
//	NODEAGENT_NODE_ID          nd_<hex16>
//	NODEAGENT_SECRET_FILE      节点凭据文件（600，单行）
//	NODEAGENT_LEDGER_FILE      幂等台账文件（600）
//	NODEAGENT_HUB_CERT_SHA256  钉住的 Hub 服务证书 SHA-256（自签阶段必填；正式证书后可空）
//	NODEAGENT_LABELS           可选，形如 site=tc,spec=8c16g,pool=p1
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/zhangzongchu2019/dingwei/internal/nodeagent"
)

func main() {
	cfg := nodeagent.Config{
		HubURL:        os.Getenv("NODEAGENT_HUB_URL"),
		NodeID:        os.Getenv("NODEAGENT_NODE_ID"),
		SecretPath:    os.Getenv("NODEAGENT_SECRET_FILE"),
		LedgerPath:    os.Getenv("NODEAGENT_LEDGER_FILE"),
		HubCertSHA256: os.Getenv("NODEAGENT_HUB_CERT_SHA256"),
		Labels:        map[string]string{},
	}
	for _, kv := range strings.Split(os.Getenv("NODEAGENT_LABELS"), ",") {
		if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
			cfg.Labels[k] = v
		}
	}
	ag, err := nodeagent.New(cfg)
	if err != nil {
		log.Fatalf("nodeagent: %v", err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	// T-G4-12 的 Docker 指令集在此注册（骨架自带 report_capacity）。
	if err := ag.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("nodeagent: %v", err)
	}
}
