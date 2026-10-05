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
	// T-G4-12 · Docker 指令集（真机 Engine＋XFS 项目配额）。
	// 🔴 NODEAGENT_DOCKER_SOCK【必填、无默认值】（组长硬要求，台账 01e52e7d）：
	//    节点上托管席位用【独立的 dockerd 实例】（独立 socket/data-root/网段），
	//    代理只能连该实例、绝不能碰节点现有 Docker——默认值写死成 /var/run/docker.sock
	//    就是把"误连业务 Docker"做成默认行为，故不设默认、缺省即拒起。
	// NODEAGENT_IMAGE_REGISTRY 固定镜像仓库（白名单）；NODEAGENT_SEAT_ROOT 席位目录根
	// （生产 /data/dingwei-newplat/agents；实验节点 /data/dingwei-node）。
	sockPath := os.Getenv("NODEAGENT_DOCKER_SOCK")
	if sockPath == "" {
		log.Fatal("nodeagent: NODEAGENT_DOCKER_SOCK 必填（独立 dockerd 实例的 socket；不得使用默认 /var/run/docker.sock）")
	}
	// 🔴 NODEAGENT_CGROUP_PARENT 必填（组长 eccc54bc）：托管席位统一挂可配置父切片
	//    （如 dingwei-hosted.slice）做资源切片；漏设＝席位逃逸出平台切片。
	cgroupParent := os.Getenv("NODEAGENT_CGROUP_PARENT")
	if cgroupParent == "" {
		log.Fatal("nodeagent: NODEAGENT_CGROUP_PARENT 必填（cgroup 父切片，如 dingwei-hosted.slice）")
	}
	// 🔴 NODEAGENT_SEAT_PROXY 必填（第 12 条②）：席位出网经 relay（如 http://172.30.68.1:11080），
	//    注入容器 env；漏设＝席位可能直连外网绕过 relay 链。
	seatProxy := os.Getenv("NODEAGENT_SEAT_PROXY")
	if seatProxy == "" {
		log.Fatal("nodeagent: NODEAGENT_SEAT_PROXY 必填（席位代理，如 http://172.30.68.1:11080）")
	}
	seatRoot := envOr("NODEAGENT_SEAT_ROOT", "/data/dingwei-newplat/agents")
	if err := ag.RegisterDockerHandlers(
		nodeagent.NewEngine(sockPath),
		nodeagent.XFSQuota(),
		seatRoot,
		envOr("NODEAGENT_IMAGE_REGISTRY", "hub.local/dingwei"),
		cgroupParent,
		seatProxy,
		cfg.HubURL,
		os.Getenv("NODEAGENT_LOCAL_IMAGES"), // local 形态白名单（与板上 HOSTED-IMAGE-VERSIONS 同源；格式不对即拒启报键名）
		// R-SEAT-HUB-VIA-PROXY 案②（生产默认开）：席位到 Hub 全走 relay
		//（NO_PROXY 不含 Hub＋注 SH_PROXY）；不设/非真值＝关（B12／本机 Hub 旧行为逐字不变）。
		envTruthy("NODEAGENT_SEAT_HUB_VIA_PROXY"),
		// R-SEAT-MODEL-NODEAGENT（业主方案 1）：席位 Codex 模型指定；可空——空则不注入
		//（与 597b4d93 逐字相同）；非空非法格式即拒启（RegisterDockerHandlers 内校验并报键名）。
		os.Getenv("NODEAGENT_SEAT_CODEX_MODEL"),
		// R-V2-HOSTED-DRIVER-IMPL（版本 2 指标 2）：cli｜driver；未设＝cli（逐字不变）；
		// 非法值拒启报键名（RegisterDockerHandlers 内校验）。
		os.Getenv("NODEAGENT_SEAT_MODE"),
		// 包 B2：本节点可选模型清单（运维配置；逐项格式校验在 RegisterDockerHandlers）。
		csvList("NODEAGENT_SEAT_CODEX_MODELS"),
	); err != nil {
		log.Fatalf("nodeagent: %v", err)
	}
	// T-G4-53：席位卷备份回传（销毁先备份；不落节点盘）
	exportH, err := nodeagent.NewExportSeatHandler(seatRoot, cfg.HubURL, cfg.Secret, cfg.HubCertSHA256)
	if err != nil {
		log.Fatalf("nodeagent: export_seat: %v", err)
	}
	ag.Register(nodeagent.CmdExportSeat, exportH)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := ag.Run(ctx); err != nil && ctx.Err() == nil {
		log.Fatalf("nodeagent: %v", err)
	}
}

// csvList 读 CSV 列表 env（逐项 TrimSpace、去空项；空＝nil）。
func csvList(key string) []string {
	var out []string
	for _, it := range strings.Split(os.Getenv(key), ",") {
		if it = strings.TrimSpace(it); it != "" {
			out = append(out, it)
		}
	}
	return out
}

// envTruthy 开关解析：1/true/yes/on（大小写不敏感）为真，其余（含未设）为假——缺省即关， fail-closed 到旧行为。
func envTruthy(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
