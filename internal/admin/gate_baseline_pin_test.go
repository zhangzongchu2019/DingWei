package admin

// 🔴 门 A 的**基线字节 pin** —— 把「旧 admin 原有行为不因本次搬迁而改变」
// 从**人工 diff** 变成**常规跑里就会自动红**的闸。
//
// 为什么必须有这一格(Nova-260921-A / B1-b):
//
//	派工第五节阴格逐字:「旧 admin 原有行为**不因本次搬迁而改变**(并存,不是迁移)」。
//	而原先承载这条的只有一个**跳过态**取数装置(TestGateDumpDoorABaseline:不设环境变量就 t.Skip)
//	⇒ 🔴 常规 `go test ./...` 里它根本不跑,谁改了门 A 的字节都不会红。
//	"靠人记得去两棵树上各跑一次再 diff" = 靠记得 = 没有。
//
// 期望值的来源(**独立**,不是从被测代码算出来的 ⇒ 不是盲量):
//
//	在**基线树 `bcd5f337`(= 0.2.1.50 preprod tip)上实跑取数装置**得到的 sha256。
//	🔵 实测(不是推论):本席 2026-09-21 在基线 worktree 与本分支各跑一次,两份输出 `diff -q` 全等;
//	   本树上**连跑 6 次哈希全同** ⇒ 装置确定性已实测。
//
// 🔴 红了怎么处置(判据写在它生效的地方,不写在文档里让人记得):
//
//	① 先看是不是装置在抖:本格自带"连跑 3 次必须逐字全同"的自证,抖了会**明说是装置**。
//	② 再定位:DW_ADMIN_BASELINE_OUT=/tmp/doorA-now.txt go test ./internal/admin/ \
//	     -run TestGateDumpDoorABaseline -count=1
//	   然后在**基线树**上同样跑一次,`diff` 两份 ⇒ 差异逐行可读(状态码/头/正文)。
//	③ 🔴 **不许就地重算哈希了事**:并存期门 A 是业主的「回退的地方」,本次搬迁无权改它的输出。
//	   确需改 ⇒ 先报组长。
//	④ 覆盖面也不许悄悄缩:记录条数被单独钉住(少跑一条 = 少一条证据,不能含糊过去)。
import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// doorABaselineSHA256 初始取自基线树 bcd5f337 (0.2.1.50)，旧值为
// ea1766cb001d2cc60f0235d3b3b4725f4f18776847b1102af88651785f4356cd。
// 2026-10-15 leader「允许窄幅重定基线」裁定（先报记录 13fb2de768c8）：
// 已授权退役公开上传/在线打包，故仅更新 /admin/provision 对应的页面字节。
// 父 r12 与候选 16f86731 的同一夹具保留全部 20 条记录；19 条逐字节一致，
// 唯一差异是删除上传/打包控件、multipart 属性与直接相关的退役/已有 URL 文案。
// 完整父/候选输出和逐行 diff：docs/reviews/coder-helper-self-update-console/retirement/。
// 文件头③「确需改先报组长」原样保留；本次是已报告且获准的例外，不授权未来改 pin。
// 🔴 改动它等于声称"门 A 的输出**应该**变了" —— 那不是本格的权限,见文件头 ③。
const doorABaselineSHA256 = "4c7b45af8b8b0f4f950a1eae23ddda12175a5ae4daf0d67110910e87a456f859"

// doorABaselineRecords = 取数装置必须覆盖的门 A 记录条数(8 GET 有正文 + 1 凭证页只记码
// + 6 ANON GET + 5 POST = 20)。🔴 与 G3b 的 `probed < 20`、G7 的 `paired < 8` 同一族:
// 覆盖面缩水必须自己报警,不能靠人发现"咦怎么变快了"。
const doorABaselineRecords = 20

func TestGateDoorABytesMatchThePinnedBaseline(t *testing.T) {
	first := dumpDoorABaseline(t)

	// ── 装置自证:取数必须**确定** ─────────────────────────────────────────────
	// 🔴 少了这一步,"哈希不同"就有两种成因(产品变了 / 装置在抖),而它们要求相反的处置:
	// 前者要回退产品改动,后者要修装置。混在一起就会去改 pin —— 那是把信号抹掉。
	for pass := 2; pass <= 3; pass++ {
		if again := dumpDoorABaseline(t); again != first {
			t.Fatalf("🔴 取数装置本身不确定(第 %d 次输出与第 1 次不同)⇒ 本格的哈希结论**不采信**。\n"+
				"处置:先修装置(固定顺序／去时间戳／去映射迭代序),**不要**改 pin。", pass)
		}
	}

	// ── 覆盖面自证:记录条数 ───────────────────────────────────────────────────
	// 🔴 按**行前缀**数,不用 strings.Count(first, "\n=== "):首条记录在偏移 0,
	// 前面没有换行 ⇒ 那种写法恒少算一条(本闸第一跑就是这么红的:报 19,实为 20)。
	records := 0
	for _, line := range strings.Split(first, "\n") {
		if strings.HasPrefix(line, "=== ") {
			records++
		}
	}
	if records != doorABaselineRecords {
		t.Fatalf("🔴 取数装置只覆盖了 %d 条门 A 记录,期望 %d 条。\n"+
			"判据:「门 A 字节不变」这句话的效力只覆盖被取到的那些记录 —— 少一条就是少一条证据。\n"+
			"处置:补回被删掉的那条 dump(...) 调用;若是**有意**调整覆盖面,先报组长。",
			records, doorABaselineRecords)
	}

	// ── 主判据:与基线树 pin 住的哈希逐字节对齐 ────────────────────────────────
	sum := sha256.Sum256([]byte(first))
	got := hex.EncodeToString(sum[:])
	if got != doorABaselineSHA256 {
		t.Fatalf("🔴 门 A 的输出字节与已批准基线**不同**。\n"+
			"  本树 sha256 = %s\n"+
			"  基线 sha256 = %s(2026-10-15 授权退役后的基线,见常量注释)\n"+
			"判据:并存期旧 admin(/admin/*,wp_admin)是业主的「回退的地方」,\n"+
			"      本次搬迁**不得**改它的任何输出(派工第五节阴格:并存,不是迁移)。\n"+
			"定位:DW_ADMIN_BASELINE_OUT=/tmp/doorA-now.txt go test ./internal/admin/ -run TestGateDumpDoorABaseline -count=1\n"+
			"      然后在基线树上同样跑一次,diff 两份输出(差异是逐行可读的状态码/头/正文)。\n"+
			"🔴 不许就地重算哈希:确需改门 A 的输出 ⇒ 先报组长。", got, doorABaselineSHA256)
	}
}
