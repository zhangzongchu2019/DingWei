// Package repo 是受控仓储包(SpecDoc §22.2c / 架构设计 A§12 甲组)。
//
// 🔴 本包存在的唯一理由:让裸库句柄【只在这里】被创建与持有。
//
//	包外任何直接建库都会被 scripts/repo-boundary-gate.sh 拒绝提交(判据一),
//	本包自己也不得把句柄导出去(判据二,允许清单对该条不豁免)。
//
// ⚠️ 能力边界,先写在最前面(§22.2c 开篇同款免责,E.4 #58「断言作用域不得大于实际覆盖」):
//
//	本包拦的是【经由本包的路径】。迁移脚本、报表、手工 psql、测试夹具不在保护内。
//	⇒ 不得表述为「已实现租户隔离」,只能说「正常路径已收敛,异常路径由评审把关」。
package repo

import "errors"

// ErrNoAccess 缺双轴访问上下文。企业或主体任一缺失均默认拒绝。
var ErrNoAccess = errors.New("repo: 缺企业/主体访问上下文,拒绝执行(fail-closed)")

// AccessCtx 双轴访问上下文：Org 决定企业库，Principal 决定 owner/RLS。
//
// 🔴 关于「无可用零值」的准确说法 —— 不夸大:
//
//	Go 里没法让一个 struct 的零值【无法编译】。本类型做到的是两条,分开说:
//	  ① **漏传是编译错误**:本包所有查询入口都以 AccessCtx 为必填形参,
//	     少传一个实参编译就不过 —— 这正是 §22.2c #2 要的「不是运行期错误」。
//	  ② **零值不可用**:零值 ok=false,任何入口拿到它一律返回 ErrNoAccess,
//	     绝不退化成「当作系统调用放行」。
//	⚠️ 把 ② 说成「零值编译错」是不实的,本注释在此钉死,免得后来者据此写出过强的验收断言。
type AccessCtx struct {
	org       string
	principal string
	ok        bool
	// sys 标记显式的系统上下文(建表/迁移/健康检查)。它必须【显式取】,
	// 不能由零值滑进来 —— 否则「缺上下文」和「系统调用」就互相冒充了,
	// 而「安全属性互相冒充」正是本仓已归纳的自造缺陷之一。
	sys bool
	// pending 标记「尚未租户化」的占位调用点,与 sys 严格分开:
	// sys = 本就与租户无关;pending = 本该有租户、但领域 schema 还没到(SP7b)。
	// 两者混为一谈,就等于用「与租户无关」冒充「已做隔离」。
	pending bool
	// domain / ownerSP 是这笔债的【结构化身份】,不是自由文本(乙域 terra SA-12)。
	// 🔴 只有 reason 一行字时,机器无法按领域归并、也判不出「datad 归零了没有」——
	//    于是 SP7b 逐领域收口就没有可复核的判据,只能靠人 grep。
	domain  string // 归属领域,如 "datad" / "fanout";与闸的分组键同源
	ownerSP string // 该债由哪个 SP 还,如 "SP7b"
	reason  string
}

// NewAccessCtx 构造双轴上下文。企业或主体任一为空均拒绝。
func NewAccessCtx(orgID, principalID string) (AccessCtx, error) {
	if orgID == "" || principalID == "" {
		return AccessCtx{}, ErrNoAccess
	}
	return AccessCtx{org: orgID, principal: principalID, ok: true}, nil
}

// SystemCtx 系统上下文:仅用于建表、迁移、健康检查这类【本就无租户语义】的操作。
//
// 🔴 它是一个评审面,不是一个方便入口:每一处 SystemCtx() 调用都应在代码评审里
//
//	被问一句「这个操作真的与租户无关吗」。故意做成必须显式写出来的样子,便于 grep 清点。
func SystemCtx() AccessCtx { return AccessCtx{ok: true, sys: true} }

// Org 返回企业边界；Principal 返回人层 owner/actor。系统上下文均返回空串。
func (t AccessCtx) Org() string       { return t.org }
func (t AccessCtx) Principal() string { return t.principal }

// IsSystem 是否为系统上下文。
func (t AccessCtx) IsSystem() bool { return t.sys }

// check 是所有入口的统一闸口:零值一律拒。
func (t AccessCtx) check() error {
	if !t.ok {
		return ErrNoAccess
	}
	if !t.sys && !t.pending && (t.org == "" || t.principal == "") {
		return ErrNoAccess
	}
	return nil
}

// PendingCtx 是【尚未双轴化】的调用点标记,不是方便入口。
//
// 🔴 为什么要有它 —— 这是本包最要紧的一条设计:
//
//	SP7a 收编时,datad 这类领域包还完全没有租户概念(领域 schema 归 SP7b)。
//	若把这些调用点一律填成 SystemCtx(),判据一确实会达标,
//	但会同时埋下一批「看起来已收编、实则全是系统调用」的调用点 ——
//	那正是「安全属性互相冒充」:用「与租户无关」冒充「已做租户隔离」。
//	⇒ 故另立本构造器:调用照常能跑,但它与 SystemCtx() 在类型内部可区分、可清点、可棘轮,
//	  由闸盯着它随 SP7b 逐个归零。**把债做成可计数的,而不是隐形的。**
//
// domain / ownerSP / reason 三者必填。
//
// 🔴 为什么把 domain 与 ownerSP 从 reason 里拆出来(乙域 terra SA-12):
//
//	原签名只有一个自由文本 reason,于是全仓只能报一个总数(当时 59)。
//	那个数既不是「有几笔债」也不是「有多少处要改」——它是【债源数 + 调用点数】相加,
//	两种单位加在一起。SP7b 按领域收口时,它回答不了「datad 归零了没有」。
//	⇒ 结构化成 (domain, ownerSP),闸即可【按领域各记各的棘轮、各自归零】。
//
// 缺任一项返回【不可用】的零值:于是它在入口处 fail-closed,而不是悄悄放行。
// 这里不返回 error 是为了不让调用点被迫改签名 —— 代价是必须靠零值兜底,故三项都校验。
func PendingCtx(domain, ownerSP, reason string) AccessCtx {
	if domain == "" || ownerSP == "" || reason == "" {
		return AccessCtx{}
	}
	return AccessCtx{ok: true, pending: true, domain: domain, ownerSP: ownerSP, reason: reason}
}

// Domain 该笔债的归属领域(仅 pending 时非空)。
func (t AccessCtx) Domain() string { return t.domain }

// OwnerSP 该笔债由哪个 SP 归还(仅 pending 时非空)。
func (t AccessCtx) OwnerSP() string { return t.ownerSP }

// IsPending 该上下文是否为尚未租户化的占位。
func (t AccessCtx) IsPending() bool { return t.pending }

// Reason 占位理由(仅 pending 时非空)。
func (t AccessCtx) Reason() string { return t.reason }
