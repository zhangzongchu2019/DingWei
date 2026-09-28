package admin

// Nova-260921-A(B1-b):**能力表的 fail-closed 极性**。
//
// 🔴 本格是变异 M14 咬出来的缺口:把 adminCapabilityDoors 的兜底
// `return false, false` 翻成 `return true, true` ⇒ 全仓 **rc=0、零转红**(实测)。
// 原因(读码):switch 已覆盖**当前声明的全部**能力常量 ⇒ 兜底那一行今天走不到。
// 🔴 但"今天走不到"不等于"永远走不到":将来有人加一个 `capFoo` 常量却忘了加进 switch,
// 就会落到兜底 —— 而兜底若是 (true,true),那个新能力**两道门都开**,且没有任何闸会红
// (G3a 钉的是路由表,不是能力表;路由表里没有 capFoo 时它根本不参与)。
// ⇒ 把兜底的极性**直接钉住**,不靠"没人会忘"。
//
// 🔴 三腿,每腿都有阳性对照(否则"全 false"与"判据坏了/恒 false"长得一样):
//
//	① 表外能力 ⇒ 对**每一种**主体类别、**每一道**门都必须是 false
//	   阳性对照:同一趟里,已知的 capFiles 对 subjectAdminSession 必须 true
//	② 已声明的每个能力都**不许**落进兜底(即 doors 至少一道门为 true)
//	   ⇒ 忘了加进 switch 的新常量,在这里当场红
//	③ 兜底的返回形状被直接断言(不经过 allowed),防止"用 allowed 间接量、
//	   而 allowed 自己也被改"的那种互相掩护
import "testing"

func TestAdminCapabilityTableFailsClosedForUnknownCapability(t *testing.T) {
	const unknown = adminCapability("__not_in_the_owner_confirmed_table__")

	t.Run("表外能力对任何主体、任何门都不存在(带阳性对照)", func(t *testing.T) {
		// 🔴 阳性对照先行:证明判据本身是活的、且**能**返回 true。
		// 少了这一腿,"表外能力全 false"可能只是 adminCapabilityAllowed 恒 false(判据死了)。
		if !adminCapabilityAllowed(subjectAdminSession, capFiles) {
			t.Fatal("阳性对照失效:已知的 capFiles 对 subjectAdminSession 应为 true ⇒ " +
				"判据本身坏了或恒 false,下面「表外能力被拒」的结论**不采信**")
		}
		if !adminCapabilityAllowed(subjectConsoleManagement, capFiles) {
			t.Fatal("阳性对照失效:已知的 capFiles 对 subjectConsoleManagement 应为 true(双路能力)")
		}
		for _, kind := range []adminSubjectKind{subjectNone, subjectAdminSession, subjectConsoleManagement, subjectConsoleOther} {
			if adminCapabilityAllowed(kind, unknown) {
				t.Fatalf("🔴 fail-closed 被放松:表外能力 %q 对主体 %q 返回 true。\n"+
					"判据:查不到 ≠ 通过 —— 未在业主确认范围内的能力,对任何主体、任何门都必须不存在。\n"+
					"后果:新增能力常量却忘了给它判据时,它会**默认开放**而不是默认拒绝。", unknown, kind)
			}
		}
	})

	t.Run("已声明的能力一个都不许落进兜底", func(t *testing.T) {
		declared := append([]adminCapability{}, adminCapabilitiesInScope()...)
		declared = append(declared, capConsoleIndex)
		for _, capability := range declared {
			doorA, doorB := adminCapabilityDoors(capability)
			if !doorA && !doorB {
				t.Fatalf("🔴 能力 %q 两道门都是 false ⇒ 它落进了 adminCapabilityDoors 的兜底分支。\n"+
					"判据:兜底是给**表外**能力用的 fail-closed;已声明的能力必须在 switch 里显式给判据。\n"+
					"常见成因:新加了能力常量,忘了加进 switch —— 那时它会静默地谁都用不了(或,若兜底被改宽,人人都能用)。",
					capability)
			}
		}
	})

	t.Run("兜底分支的形状被直接钉住", func(t *testing.T) {
		doorA, doorB := adminCapabilityDoors(unknown)
		if doorA || doorB {
			t.Fatalf("🔴 adminCapabilityDoors 的兜底返回 (%t,%t),期望 (false,false)。\n"+
				"判据:这是 fail-closed 的**极性**,不许翻转(M14 实测:翻转后全仓零转红 ⇒ 本格是唯一的闸)。",
				doorA, doorB)
		}
	})
}
