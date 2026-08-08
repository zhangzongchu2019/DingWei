package fanout

// pre-arm 门(设计 v2.3 §A/runbook 阶段1):ws 接入连上飞书【之前】六项全绿,
// 任一红=拒连(代理从第一条事件起就在生产入站路径上,门在连接之前)。
// ①配置校验过(ApplyFileConfig 前置保证)②authoritative sink 连通+token 探测
// ③spool 落盘自检④磁盘水位⑤告警通道已人工实测(配置 attest 旗标)⑥DLQ 空。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/zhangzongchu2019/dingwei/internal/feishu"
)

// PreArm 全量门检。返回 nil=允许连 ws。
func (s *Server) PreArm(ctx context.Context) error {
	if s.spool == nil {
		return fmt.Errorf("pre-arm③: spool 未开(fail-closed)")
	}
	// ② authoritative sink 探测:challenge 探针(url_verification 形+token,
	// Hub 挑战路 200 返回 challenge,不入消息账=无侵入)
	s.mu.RLock()
	sinks := s.dynSinks
	fileCfg := s.fileCfg
	s.mu.RUnlock()
	for _, sk := range sinks {
		if sk.Role != "authoritative" {
			continue
		}
		probe, _ := json.Marshal(map[string]any{"challenge": "t17-prearm", "token": sk.Token})
		url := strings.TrimRight(sk.HubURL, "/") + "/webhook/" + sk.Channel
		pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		req, _ := http.NewRequestWithContext(pctx, "POST", url, bytes.NewReader(probe))
		req.Header.Set("Content-Type", "application/json")
		resp, err := s.client.Do(req)
		cancel()
		if err != nil {
			return fmt.Errorf("pre-arm②: sink %s 不可达: %w", sk.Name, err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			return fmt.Errorf("pre-arm②: sink %s challenge 探测=%d(token/通道配置?)", sk.Name, resp.StatusCode)
		}
	}
	// ③ spool 自检:事务写读删
	if err := s.spool.SelfCheck(ctx); err != nil {
		return fmt.Errorf("pre-arm③: spool 自检失败: %w", err)
	}
	// ④ 磁盘水位(默认>10%可用)
	if s.cfg.SpoolPath != "" {
		var st syscall.Statfs_t
		if err := syscall.Statfs(s.cfg.SpoolPath, &st); err == nil && st.Blocks > 0 {
			freePct := float64(st.Bavail) / float64(st.Blocks) * 100
			min := s.cfg.DiskMinFreePct
			if min <= 0 {
				min = 10
			}
			if freePct < min {
				return fmt.Errorf("pre-arm④: 磁盘余量 %.1f%% < %.0f%%", freePct, min)
			}
		}
	}
	// ⑤ 告警通道 attest(runbook 0.6 人工实测后置 alerts_verified:true)
	if fileCfg != nil && !fileCfg.AlertsVerified {
		return fmt.Errorf("pre-arm⑤: alerts_verified 未置(先按 runbook 0.6 实测告警通道)")
	}
	// ⑥ DLQ 空
	if n, err := s.spool.CountByStatus(ctx, "dlq"); err != nil || n > 0 {
		return fmt.Errorf("pre-arm⑥: 出站 DLQ=%d(err=%v),先清", n, err)
	}
	if n, err := s.spool.CountBlocked(ctx); err != nil || n > 0 {
		return fmt.Errorf("pre-arm⑥: 入站 blocked=%d(err=%v),先 replay/waive", n, err)
	}
	return nil
}

// startAccess pre-arm 全绿才起单个 ws 接入(可 cancel,供热载删除时优雅收)。
func (s *Server) startAccess(ctx context.Context, a AccessConfig) error {
	if !s.cfg.SkipPreArm {
		if err := s.PreArm(ctx); err != nil {
			return err
		}
	}
	factory := s.cfg.ReceiverFactory
	if factory == nil {
		factory = defaultReceiverFactory
	}
	recv, err := factory(a.BotChannel, a.AppID, a.AppSecret)
	if err != nil {
		return fmt.Errorf("access %s: %w", a.BotChannel, err)
	}
	actx, cancel := context.WithCancel(ctx)
	s.accessCancel[a.BotChannel] = cancel
	go func(bot string, r feishu.Receiver) {
		if err := r.Start(actx, s.onWSInbound); err != nil && actx.Err() == nil {
			s.log.Error("ws接入退出(告警)", "bot_channel", bot, "error", err)
		}
	}(a.BotChannel, recv)
	s.log.Info("ws接入已起(pre-arm过)", "bot_channel", a.BotChannel)
	return nil
}

// feishuLarkSender 出站 bot 凭据→真 LarkGateway(Sender 面)。
func feishuLarkSender(botChannel, appID, appSecret string) (Sender, error) {
	return feishu.NewLarkGateway(botChannel, appID, appSecret, nil)
}
