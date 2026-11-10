package migrate

// 账本的【按域施加】：号段 → 域，装配点只施加自己那几个域（口径页 §五第二版）。
//
// 为什么要有这一层（组长裁定 R-G2-LEDGER-DOMAIN，RULING-REGISTER 2026-10-02T11:06:25Z）：
// 账本原本只有一个「全部条目」的口径，而 fanout 自 R-G2-FANOUT-HARDEN 起也有了自己的
// 冻结条目 0400。fanout 是【独立服务、独立库】，于是「把整个账本施加到手上这个库」
// 从「省事」变成了「事故」——Hub 部署会把 fanout 的五表建进生产 Hub 库。
// 本文件把「这个库属于哪几个域」变成装配点必须显式回答的问题：
//
//	· 没有「全账本」的生产入口（Migrate 少了域集合参数就编译不过）；
//	· 号段判不出域 ⇒ 拒（不猜、不默认归 core）；
//	· 库里已记着【别的域】的账 ⇒ 拒（DSN 指错库时它在第一条 DDL 之前就出声）。

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Domain 是账本条目所属的域。域名本身只用于报错与判集合，不进库（库里记的是条目名）。
type Domain string

const (
	DomainCore     Domain = "core"     // 0001-0099 G2 core（含 0001 基线快照）
	DomainIdentity Domain = "identity" // 0100-0199 G3 identity
	DomainHosted   Domain = "hosted"   // 0200-0299 G4 hosted
	DomainView     Domain = "view"     // 0300-0399 G5 view
	DomainFanout   Domain = "fanout"   // 0400-0499 G2 fanout（独立库）
	DomainCross    Domain = "cross"    // 0900-9999 跨域／收尾
)

// domainRange 是一段号段（含两端）与它所属的域。
type domainRange struct {
	lo, hi int
	domain Domain
}

// domainRanges 号段 → 域，逐条对应口径页 §五第二版。
//
// 🔴 未列出的号段（现在是 0500-0899）一律 fail-closed：新域要先在这里登记，
// 而不是让条目悄悄落进某个既有域。「悄悄落进 core」的形状会让一个新服务的表
// 被 Hub 施加——正是本文件要消掉的那件事。
// 🔴 改这张表＝改各装配点会施加什么，属评审面：先在口径页 §五 立号段，再改这里。
var domainRanges = []domainRange{
	{lo: 1, hi: 99, domain: DomainCore},
	{lo: 100, hi: 199, domain: DomainIdentity},
	{lo: 200, hi: 299, domain: DomainHosted},
	{lo: 300, hi: 399, domain: DomainView},
	{lo: 400, hi: 499, domain: DomainFanout},
	{lo: 900, hi: 9999, domain: DomainCross},
}

// knownDomains 已知域全集（校验请求集合用；顺序只为报错稳定）。
var knownDomains = []Domain{DomainCore, DomainIdentity, DomainHosted, DomainView, DomainFanout, DomainCross}

// HubDomains Hub 侧装配点（cmd/workpulse 的 openNewPlatformStore：Hub 本体与 bootstrap-admin 共用）
// 施加的域集合。🔴 不含 fanout —— fanout 是独立服务、独立库（R-G2-LEDGER-DOMAIN）。
//
// 🔵 cross（0900+）算在 Hub 侧：跨域／收尾条目只可能落在「一个库里住着几个域」的那一侧，
// 而按裁定那一侧只有 Hub；fanout 库恒只装 fanout 表。若将来出现一条 cross 条目是给 fanout 库的，
// 🔴 不要把它塞进 FanoutDomains()，而是给它一个自己的号段（口径页 §五）——那是评审面的事。
//
// 🔵 hosted 已有 0200_hosted_r30_baseline.sql、0201_hosted_node_access_r31.sql；
// view 已有 0300_console_transport_slot.sql。三条均由 Hub 侧施加，fanout 库不施加。
func HubDomains() []Domain {
	return []Domain{DomainCore, DomainIdentity, DomainHosted, DomainView, DomainCross}
}

// FanoutDomains fanout 服务（cmd/feishu-fanout 的 openSpoolPool）对【自己那个库】施加的域集合。
func FanoutDomains() []Domain {
	return []Domain{DomainFanout}
}

// DomainOf 从条目名判域：名字前四位是号段（账本主键＝文件名，口径页 §五）。
//
// 🔴 判不出就报错，不给默认值：默认值会让「写错号」与「新号段没登记」两种事故静默通过。
func DomainOf(name string) (Domain, error) {
	if len(name) < 5 || name[4] != '_' {
		return "", fmt.Errorf("migrate: 条目名 %q 不是「四位号段_描述.sql」的形状 —— 号段即域（口径页 §五），"+
			"判不出号段就判不出该施加到哪个库", name)
	}
	for _, c := range name[:4] {
		if c < '0' || c > '9' {
			return "", fmt.Errorf("migrate: 条目名 %q 的前四位不是数字 —— 号段即域（口径页 §五）", name)
		}
	}
	num, err := strconv.Atoi(name[:4])
	if err != nil {
		return "", fmt.Errorf("migrate: 条目 %s 的号段读不成数: %w", name, err)
	}
	for _, r := range domainRanges {
		if num >= r.lo && num <= r.hi {
			return r.domain, nil
		}
	}
	return "", fmt.Errorf("migrate: 条目 %s 的号段 %04d 未分配域（已登记：%s）—— "+
		"新域先在口径页 §五 立号段、再登记进 internal/store/migrate/domains.go，"+
		"不要让它悄悄落进别的域（那会让它的表被别的库施加）", name, num, rangesString())
}

// LedgerInDomains 返回属于给定域集合的账本条目（Ledger() 的子集，按 Name 字典序）。
//
// 🔴 域集合为空即报错：空集合意味着「一条都不施加」，那在装配点上永远是写错了，
// 不是「这个库不需要表」。
func LedgerInDomains(domains []Domain) ([]Entry, error) {
	set, err := domainSet(domains)
	if err != nil {
		return nil, err
	}
	all, err := Ledger()
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(all))
	for _, e := range all {
		d, err := DomainOf(e.Name)
		if err != nil {
			return nil, err
		}
		if set[d] {
			out = append(out, e)
		}
	}
	sortEntries(out)
	return out, nil
}

// foreignEntries 账上【已记】但不属于给定域集合的条目名，按字典序。
//
// 非空即说明「这个库不是本装配点的库」（或本装配点的域集合漏了域）：
// 两种都必须在动库之前出声，而不是建完表再让人从库里反推。
func foreignEntries(applied map[string]string, domains []Domain) ([]string, error) {
	set, err := domainSet(domains)
	if err != nil {
		return nil, err
	}
	var out []string
	for name := range applied {
		d, err := DomainOf(name)
		if err != nil {
			// 账上有判不出域的名字（手工插的、或旧形状遗留）：与「号段未登记」同族，一律拒。
			return nil, err
		}
		if !set[d] {
			out = append(out, fmt.Sprintf("%s（%s 域）", name, d))
		}
	}
	sort.Strings(out)
	return out, nil
}

// isolationErr 是「库里有别的域的账」的拒绝错误。
func isolationErr(foreign []string, domains []Domain) error {
	return fmt.Errorf("migrate: 这个库里记着不属于本次域集合 [%s] 的账：%s —— "+
		"要么 DSN 指错了库（Hub 与 fanout 各用一个独立库，裁定 R-G2-LEDGER-DOMAIN 2026-10-02T11:06:25Z），"+
		"要么本装配点的域集合漏了那个域。拒绝启动，且不施加任何条目",
		domainsString(domains), strings.Join(foreign, ", "))
}

// domainSet 校验请求的域集合并转成集合：不得为空、不得有未知域、不得重复。
func domainSet(domains []Domain) (map[Domain]bool, error) {
	if len(domains) == 0 {
		return nil, fmt.Errorf("migrate: 域集合为空 —— 那等于「一条都不施加」，装配点必须点名自己的域" +
			"（Hub 用 HubDomains()、fanout 用 FanoutDomains()，口径页 §五）")
	}
	set := make(map[Domain]bool, len(domains))
	for _, d := range domains {
		if set[d] {
			return nil, fmt.Errorf("migrate: 域 %q 在请求集合里出现两次", d)
		}
		if !knownDomain(d) {
			return nil, fmt.Errorf("migrate: 未知域 %q（已知：%s）—— 域由号段决定，不接受自造域名",
				d, domainsString(knownDomains))
		}
		set[d] = true
	}
	return set, nil
}

func knownDomain(d Domain) bool {
	for _, k := range knownDomains {
		if k == d {
			return true
		}
	}
	return false
}

func domainsString(domains []Domain) string {
	parts := make([]string, 0, len(domains))
	for _, d := range domains {
		parts = append(parts, string(d))
	}
	return strings.Join(parts, " ")
}

func rangesString() string {
	parts := make([]string, 0, len(domainRanges))
	for _, r := range domainRanges {
		parts = append(parts, fmt.Sprintf("%04d-%04d %s", r.lo, r.hi, r.domain))
	}
	return strings.Join(parts, "、")
}
