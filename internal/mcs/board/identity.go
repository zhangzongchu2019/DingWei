// identity.go —— P3:统一身份 enforce(契约 C4;张宗楚地基要求 D2=a)。
//
// 合法身份=统一命名格式(v1.0):^zzc-[a-z0-9]+-[0-9]+$;此字符串=全体系唯一主键。
// enforce 点:RegisterAgent 与事件 actor 入账(insertEvent)——非法名=写入拒绝+identity_rejected 事件。
// warn/enforce 开关:上线切换判据=收编三步完成(C4 4.3)。**默认 warn**(引擎内部 worker id/既有测试非 zzc- 格式;调度者系统上线=部署侧开 enforce,见 D2=a)。
package board

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// hasControlChar 任何控制字符/空白(含 \n\r\t)=非法(★MC-4 注入面)。
func hasControlChar(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f || r == ' ' }) >= 0
}

// IdentityPattern 合法 agent/actor 身份格式(契约 C4 4.1,改动=改契约须重两签)。
// ★MC-4 fullmatch(\A…\Z 等价:Go 无 ^$ 多行问题,但用 ^…$+显式拒控制字符/空白双保险)。
var IdentityPattern = regexp.MustCompile(`\Azzc-[a-z0-9]+-[0-9]+\z`)

// identityMode: enforce=true(拒收非法名);false=warn(放行)。
// provisional=true(过渡期 hub principal 未就绪):★shence P2——warn 不写权威 identity_warned
// (共享钥可伪造 warn 污染审计/盘点),仅放行;收编盘点(C4.3)源自 hub 只读活跃名单、非逐消息 warn。
// principal 就绪后 SetIdentityProvisional(false),warn 才写权威 identity_warned。
// 生产切换走 SetIdentityEnforce/SetIdentityProvisional(部署配置);默认 warn+provisional。
type identityConfig struct {
	enforce     bool
	provisional bool
	warned      map[string]bool // warn 模式每名恰一次事件(防噪)
}

// SetIdentityEnforce 切换 warn/enforce(C4 4.3:切换判据由部署侧把;Board 只提供开关)。
func (b *Board) SetIdentityEnforce(on bool) {
	b.idmu.Lock()
	defer b.idmu.Unlock()
	b.idcfg.enforce = on
}

// SetIdentityProvisional 切换过渡期(★shence P2:provisional 下 warn 非权威、不写 identity_warned)。
func (b *Board) SetIdentityProvisional(on bool) {
	b.idmu.Lock()
	defer b.idmu.Unlock()
	b.idcfg.provisional = on
}

func (b *Board) identityEnforced() bool {
	b.idmu.Lock()
	defer b.idmu.Unlock()
	return b.idcfg.enforce
}

func (b *Board) identityProvisional() bool {
	b.idmu.Lock()
	defer b.idmu.Unlock()
	return b.idcfg.provisional
}

// ErrIdentityRejected 非法身份被 enforce 拒收。
type ErrIdentityRejected struct{ Name, Source string }

func (e *ErrIdentityRejected) Error() string {
	return fmt.Sprintf("mcs/board: identity %q rejected at %s (C4 格式:zzc-<name>-<tail>)", e.Name, e.Source)
}

// checkIdentity 校验身份。合法→nil。非法:
//   - enforce→落 identity_rejected 事件(独立短事务,拒收也可审计)+错误;
//   - warn+非过渡期→落 identity_warned 事件+放行;
//   - warn+过渡期(provisional,★shence P2)→仅放行、【不写权威 identity_warned】(防伪造 warn 污染审计/盘点)。
// 审计事件 actor 落系统哨兵(违规者本身无合法身份可签,dev56③);data 记裸名与来源。
func (b *Board) checkIdentity(ctx context.Context, name, source string) error {
	if !hasControlChar(name) && IdentityPattern.MatchString(name) {
		return nil
	}
	if !b.identityEnforced() {
		// warn 模式:每名 dedup(防噪)。
		b.idmu.Lock()
		if b.idcfg.warned == nil {
			b.idcfg.warned = map[string]bool{}
		}
		dup := b.idcfg.warned[name]
		b.idcfg.warned[name] = true
		prov := b.idcfg.provisional
		b.idmu.Unlock()
		if dup || prov { // 已警过,或★shence P2 过渡期非权威:放行、不写权威事件
			return nil
		}
		_, _ = b.db.ExecContext(ctx,
			`INSERT INTO board_event(kind,agent_id,ts,data) VALUES('identity_warned',?,?,json_object('name',?,'source',?))`,
			"system:identity-gate", b.now(), name, source)
		return nil
	}
	// enforce:非法=identity_rejected 事件+错误。
	_, _ = b.db.ExecContext(ctx,
		`INSERT INTO board_event(kind,agent_id,ts,data) VALUES('identity_rejected',?,?,json_object('name',?,'source',?))`,
		"system:identity-gate", b.now(), name, source)
	return &ErrIdentityRejected{Name: name, Source: source}
}
