// Package nodeagent 节点代理骨架（T-G4-10；节点代理设计件）。
//
// 职责边界：只做「按 Hub 指令起停容器、上报容量」，不做业务逻辑。
// 部署边界（隔离四条）：受 systemd 管理的宿主进程；payload 容器永不可见 Docker socket。
//
// 骨架四件：
//
//	① WSS 长连：节点验 Hub（TLS＋钉住的服务证书 SHA-256），Hub 验节点（node_id＋凭据，
//	  由 Hub 侧 AuthenticateNode 核——本包只管把凭据放对地方：Authorization 头，不进 URL）。
//	② 指令接收/结果回报框架：Command{command_id,type,params} → Handler → Result 回送。
//	③ command_id 幂等台账：已执行的指令重发 ⇒ 直接重报首次结果、不重复执行；
//	  台账落盘（0600）⇒ 进程重启、断线重连后重放旧指令同样不重复执行（重放用例的锚）。
//	④ Handler 注册表：T-G4-12 的 Docker 指令集按类型注册进来；骨架自带 report_capacity 桩。
package nodeagent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Config 节点代理配置（设计件第七节：仅三项＋钉证）。
type Config struct {
	HubURL        string // wss://hub/api/node/connect
	NodeID        string // nd_<hex16>
	SecretPath    string // 节点凭据文件（600；内容即凭据本身，单行）
	LedgerPath    string // 幂等台账文件（600）
	HubCertSHA256 string // 钉住的 Hub 服务证书 SHA-256（hex，冒号分隔与否均可）
	Labels        map[string]string
}

func (c Config) validate() error {
	if c.HubURL == "" || c.NodeID == "" || c.SecretPath == "" || c.LedgerPath == "" {
		return fmt.Errorf("nodeagent: HubURL/NodeID/SecretPath/LedgerPath 均必填")
	}
	if !strings.HasPrefix(c.HubURL, "wss://") {
		return fmt.Errorf("nodeagent: HubURL 必须 wss://（TLS 双向认证的一层）")
	}
	return nil
}

// Secret 节点凭据（导出给 export_seat 这类需要直连 Hub 的指令用；读取纪律同 secret()）。
func (c Config) Secret() (string, error) { return c.secret() }

func (c Config) secret() (string, error) {
	raw, err := os.ReadFile(c.SecretPath)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(c.SecretPath)
	if err != nil {
		return "", err
	}
	if fi.Mode().Perm() > 0o600 {
		return "", fmt.Errorf("nodeagent: 凭据文件权限 %o 宽于 600", fi.Mode().Perm())
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return "", fmt.Errorf("nodeagent: 凭据文件为空")
	}
	return s, nil
}

// Command Hub 下发的一条指令。
type Command struct {
	CommandID string          `json:"command_id"`
	Type      string          `json:"type"`
	Params    json.RawMessage `json:"params,omitempty"`
}

// Result 节点代理回送的执行结果（幂等台账里存的就是它）。
type Result struct {
	CommandID string          `json:"command_id"`
	OK        bool            `json:"ok"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Error     string          `json:"error,omitempty"`
}

// Handler 一类指令的执行器。T-G4-12 的 Docker 指令集按此注册。
type Handler func(ctx context.Context, params json.RawMessage) (json.RawMessage, error)

// 指令类型（白名单；不在表内的一律拒——设计件第三节）。
const (
	CmdPullImage      = "pull_image"
	CmdCreateSeat     = "create_seat"
	CmdStopSeat       = "stop_seat"
	CmdStartSeat      = "start_seat"
	CmdRestartSeat    = "restart_seat"
	CmdUpgradeSeat    = "upgrade_seat"
	CmdDestroySeat    = "destroy_seat"
	CmdReportCapacity = "report_capacity"
	CmdExportSeat     = "export_seat" // T-G4-53：席位卷备份回传
	// CmdRefreshCredSeat 刷新凭据（0.2.1.101，R-V2-ACCOUNT-REFRESH-CRED）：原子覆写 account-credential 后重启席位。
	CmdRefreshCredSeat = "refresh_cred_seat"
)

var commandWhitelist = map[string]bool{
	CmdPullImage: true, CmdCreateSeat: true, CmdStopSeat: true, CmdStartSeat: true,
	CmdRestartSeat: true, CmdUpgradeSeat: true, CmdDestroySeat: true, CmdReportCapacity: true,
	CmdExportSeat: true, CmdRefreshCredSeat: true,
}

const reconnectBase = 2 * time.Second
const reconnectMax = 30 * time.Second
