package fanout

// 声明式配置+原子热载(设计 v2.3 §E):磁盘文件=唯一真源;PUT/SIGHUP 同一校验管道;
// 烂配置不接管(校验失败=保旧跑);authoritative 删除/降级需 force;
// sink 地址安全规则=远端必须 https,http 仅 loopback;持久化=temp+fsync+rename+dir fsync。

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type BotCred struct {
	AppID     string `yaml:"app_id" json:"app_id"`
	AppSecret string `yaml:"app_secret" json:"app_secret"`
}

// FileConfig fanout.yaml 全量模型。Generation 由服务端单调递增(CAS 见 admin PUT)。
type FileConfig struct {
	Generation     int64                   `yaml:"generation" json:"generation"`
	Force          bool                    `yaml:"force" json:"force"` // authoritative 变更确认旗标
	AlertsVerified bool                    `yaml:"alerts_verified" json:"alerts_verified"`
	Accesses       []AccessConfig          `yaml:"accesses" json:"accesses"`
	Sinks          []SinkConfig            `yaml:"sinks" json:"sinks"`
	Callers        map[string]CallerConfig `yaml:"callers" json:"callers"`
	OutboundBots   map[string]BotCred      `yaml:"outbound_bots" json:"outbound_bots"`
}

func isLoopbackURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

// Validate §E 防呆:烂配置在此拦截(调用方保旧配置继续跑)。
// prev!=nil 时执行 force 规则:删除/降级 authoritative sink 须 Force=true。
func (fc *FileConfig) Validate(prev *FileConfig) error {
	for _, sk := range fc.Sinks {
		if sk.Name == "" || sk.HubURL == "" || sk.Channel == "" {
			return fmt.Errorf("sink %q: name/hub_url/channel 必填", sk.Name)
		}
		if sk.Role != "authoritative" && sk.Role != "besteffort" {
			return fmt.Errorf("sink %q: role 须 authoritative|besteffort", sk.Name)
		}
		if sk.Filter != "all" && sk.Filter != "whitelist" {
			return fmt.Errorf("sink %q: filter 须 all|whitelist", sk.Name)
		}
		u, err := url.Parse(sk.HubURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return fmt.Errorf("sink %q: hub_url 非法", sk.Name)
		}
		if u.Scheme == "http" && !isLoopbackURL(sk.HubURL) {
			return fmt.Errorf("sink %q: 远端必须 https(http 仅 loopback)", sk.Name) // §E/C3
		}
	}
	for _, a := range fc.Accesses {
		if a.Mode != "ws" && a.Mode != "https" {
			return fmt.Errorf("access %q: mode 须 ws|https", a.BotChannel)
		}
		if a.Mode == "ws" && (a.AppID == "" || a.AppSecret == "") {
			return fmt.Errorf("access %q: ws 须 app_id/app_secret", a.BotChannel)
		}
	}
	if prev != nil && !fc.Force {
		for _, old := range prev.Sinks {
			if old.Role != "authoritative" {
				continue
			}
			still := false
			for _, now := range fc.Sinks {
				if now.Name == old.Name && now.Role == "authoritative" {
					still = true
					break
				}
			}
			if !still {
				return fmt.Errorf("authoritative sink %q 删除/降级须 force:true(§E 防呆)", old.Name)
			}
		}
	}
	return nil
}

func LoadFileConfig(path string) (*FileConfig, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fc := &FileConfig{}
	if err := yaml.Unmarshal(raw, fc); err != nil {
		return nil, fmt.Errorf("配置解析失败: %w", err)
	}
	return fc, nil
}

// SaveFileConfigAtomic §E:temp+fsync+rename+dir fsync;失败=旧文件原样(真源不腐)。
func SaveFileConfigAtomic(path string, fc *FileConfig) error {
	raw, err := yaml.Marshal(fc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".fanout-*.yaml")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// SenderFactory 出站 bot 凭据→Sender(默认=真 LarkGateway;测试注入)。
type SenderFactory func(botChannel, appID, appSecret string) (Sender, error)

func defaultSenderFactory(botChannel, appID, appSecret string) (Sender, error) {
	return feishuLarkSender(botChannel, appID, appSecret)
}

// ApplyFileConfig 原子热载:校验已在调用方做过;交换 dyn 表+接入生命周期+代次推进。
// 任一步失败=不换内存态(§E:热载最坏结果是"没变")。
func (s *Server) ApplyFileConfig(ctx context.Context, fc *FileConfig) error {
	// 先构建(可失败部分全在交换前):出站 bots
	bots := map[string]Sender{}
	factory := s.cfg.SenderFactory
	if factory == nil {
		factory = defaultSenderFactory
	}
	for bot, cred := range fc.OutboundBots {
		sd, err := factory(bot, cred.AppID, cred.AppSecret)
		if err != nil {
			return fmt.Errorf("outbound bot %s: %w", bot, err)
		}
		bots[bot] = sd
	}
	s.mu.Lock()
	s.dynSinks = fc.Sinks
	s.dynCallers = fc.Callers
	if len(bots) > 0 {
		s.dynBots = bots
	}
	s.fileCfg = fc
	s.mu.Unlock()
	// 接入生命周期:停删除的,起新增的(pre-arm 门在 startAccess 内)
	return s.reconcileAccesses(ctx, fc.Accesses)
}

// reconcileAccesses 增删 ws 接入实例(删=cancel 优雅收;增=pre-arm 后起)。
func (s *Server) reconcileAccesses(ctx context.Context, want []AccessConfig) error {
	s.accessMu.Lock()
	defer s.accessMu.Unlock()
	wanted := map[string]AccessConfig{}
	for _, a := range want {
		if a.Mode == "ws" {
			wanted[a.BotChannel] = a
		}
	}
	for bot, cancel := range s.accessCancel {
		if _, ok := wanted[bot]; !ok {
			cancel()
			delete(s.accessCancel, bot)
			s.log.Info("ws接入已停(配置删除)", "bot_channel", bot)
		}
	}
	var firstErr error
	for bot, a := range wanted {
		if _, running := s.accessCancel[bot]; running {
			continue
		}
		if err := s.startAccess(ctx, a); err != nil {
			s.log.Error("ws接入启动拒绝", "bot_channel", bot, "error", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}
