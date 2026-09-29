package admin

// Nova-260921-A(B1-b):**并存期两道门共用一份能力判据。**
//
// 业主逐字(板件 runbooks/SPEC-OWNER-TAB-ADMIN-20260920.md §六②):
//
//	「先并存一段时间,**有一个回退的地方**」
//
// ⇒ 旧 `/admin/*`(wp_admin cookie)与 console 内新门(`/console/admin/*`,console 角色)
// **同时活着**,旧后台并存至 `0.2.2.0`。
//
// 组长派工第四节的交付条件:同一能力**两条路的授权判据必须等价**,且**闸要咬住**
// (并存期内任何一侧改了权限判据,闸必须变红)。
// ⇒ 判据只写在这里一份,**两道门都调它**;"两路判据分叉"在结构上不可能发生。
//
// 🔴 代价与对策(如实写明,不藏):门 A 也依赖这张表 ⇒ 改表会同时影响**回退口**。
// 因此这张表被两面钉住:
//
//	G1 授权矩阵的期望值**在测试里硬写**(取自业主确认范围,**不调用本文件**)
//	   ⇒ 改表 ⇒ G1 红(否则期望值跟着变,那是**盲量**)
//	G4 门 A 基线字节对照 ⇒ 门 A 的输出与状态码**逐字不变**
//
// 🔴 能力表 = 业主确认的搬迁范围(板件 runbooks/BATCH-B1-CONTENT-20260921.md §六-2 逐字):
//
//	文件对象浏览 · Provision 下发 · 凭证 API key · 机器人通道
//
// ＋ `Nova-260921-A` 新增的「增删管理者」(板件 REQUIREMENT-REGISTRY-20260921.md:42
// 逐字「增删管理者(🔴 剩最后一个不能删)」)。
//
// 🔴 `/admin/agent-groups` **不在表内** —— 它被 B1-a 的编组 tab **替代**,不是搬迁(派工逐字)。
// 🔴 `config`／`routes`／`sessions`／`messages`／`projects`／`services`／`control-plane`／
// `datad`／`cleanup`／`portal` **一律不在表内**(业主:「其余暂不搬」)。
type adminCapability string

const (
	capFiles         adminCapability = "files"          // 文件对象浏览
	capFileContent   adminCapability = "files.content"  // 文件对象:预览/下载
	capProvision     adminCapability = "provision"      // Provision 下发
	capAPIKeys       adminCapability = "api-keys"       // 凭证 API key(🔴 明文,业主知情后决定)
	capBotChannels   adminCapability = "bot-channels"   // 机器人通道
	capPlatformRoles adminCapability = "platform-roles" // 增删管理者(Nova-260921-A 新增)

	// capConsoleIndex **不是"能力"**,是门 B 的导航页。
	// 🔴 它不出现在 adminCapabilitiesInScope() 里 —— 那张表是**业主确认的搬迁范围**,
	// 把导航页混进去会让"范围"这个词失去意义(G3 要拿它与真实 mux 对账)。
	// 它的判据只有一条:必须是 management。
	capConsoleIndex adminCapability = "console-index"
)

// adminSubjectKind 描述"拿什么凭据站在这道门前"。
//
// 🔴 分类的依据是**凭据种类**,不是"人":两套凭据互不相干
// (wp_admin 是 admin 侧内存会话;console 是 dw_console_session ＋ console_platform_role)。
type adminSubjectKind string

const (
	subjectNone              adminSubjectKind = "none"               // 无任何凭据
	subjectAdminSession      adminSubjectKind = "admin-session"      // 门 A:wp_admin cookie 命中内存会话
	subjectConsoleManagement adminSubjectKind = "console-management" // 门 B:console_platform_role == management
	// subjectConsoleOther 是**本格最该被钉住的那一类**:登录了 console 但不是 management。
	// 🔴 它不是罕见形状 —— store/console_auth.go:222 的 ResolveConsolePlatformRole 用
	// COALESCE(…,'owner'),**每个 active principal 默认就是 owner**。
	subjectConsoleOther adminSubjectKind = "console-other"
)

// adminCapabilityDoors 说明一个能力**挂在哪几道门上**。
//
// 🔴 `capPlatformRoles` **只有门 B**:它是 Nova-260921-A 新增的能力,
// 旧 admin 从来没有运行时增删管理者的入口 —— 实测:`UpsertConsolePlatformRole`
// 全仓唯一的非测试调用点是 `cmd/workpulse/console_seed.go:81`(seed)。
// ⇒ **这一格没有双路对照面**,一致性闸(G2)覆盖不到它;它由 G5(最后一个不能删 ＋ fail-closed)
// 与 G6(审计归属)单独守。**如实标注,不假装它也在等价性里。**
//
// 🔴 `capConsoleIndex` 同理只有门 B(门 A 的导航页是 `/admin` 状态页,不在搬迁范围)。
func adminCapabilityDoors(capability adminCapability) (doorA bool, doorB bool) {
	switch capability {
	case capFiles, capFileContent, capProvision, capAPIKeys, capBotChannels:
		return true, true
	case capPlatformRoles, capConsoleIndex:
		return false, true
	}
	// fail-closed:表外能力对**任何**主体、**任何**门都不存在。查不到 ≠ 通过。
	return false, false
}

// adminCapabilitiesInScope 是**业主确认的搬迁范围**的机器可读形式。
// 闸 G3 拿它与**真实 mux** 对账:门 B 多挂一项或少挂一项都红。
// 🔴 顺序固定 —— 它同时是名册页/索引页的呈现顺序,别让 map 迭代顺序决定界面。
func adminCapabilitiesInScope() []adminCapability {
	return []adminCapability{capFiles, capFileContent, capProvision, capAPIKeys, capBotChannels, capPlatformRoles}
}

// adminCapabilityAllowed 是**唯一**的授权判据,两道门都调它。
//
// 🔴 fail-closed:未列出的主体类别(含 subjectNone 与 subjectConsoleOther)一律 false。
// 新增一种主体类别时**必须显式在这里给它判据**,否则默认拒 —— 这是刻意的。
func adminCapabilityAllowed(kind adminSubjectKind, capability adminCapability) bool {
	doorA, doorB := adminCapabilityDoors(capability)
	switch kind {
	case subjectAdminSession:
		return doorA
	case subjectConsoleManagement:
		return doorB
	default:
		return false
	}
}
