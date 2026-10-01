"""S-HELPER-NOTICE-NO-INJECT 的判据(派工单第四节四格 + 同源漂移闸)。

派工:runbooks/DISPATCH-20260929-S-HELPER-NOTICE-NO-INJECT.md
背景:runbooks/FINDING-amr-hash-branch-and-alignment-20260929-qwen38maxkhovvob4.md §十六/§十七

要治的环:Hub 自发的系统通知(meta.type 为 delivery_failure / delivery_result)
    被 helper 注入 CLI ⇒ 产生新一轮 ⇒ 新通知。席位侧无法退出(有正文会通知、
    没正文也通知),限速器 dedupKey 不含 DeliveryID/TurnID、窗口 1 分钟
    ⇒ 约 60 轮/小时、零业务产出。

修法:在 recv_loop 的 dispatch 链上为这两类 type 加一个分支 —— 留痕但不注入。

🔴 运行本套件的注意事项(存量地雷,非本件引入,已另行上报):
    `python3 -m unittest discover -s tools/sessionhelper/tests` 若不带
    SH_PROVISION_NO_EXIT=1,provision.request_restart 的 os._exit(0) 会在
    rc=0、无汇总行的情况下把 runner 静默带走(实测 182 格只跑 58 格)。
    本文件把 HOME 与全部 provision/inbox 路径指到临时目录,因此既不产生那份
    会引爆下一次运行的残留,也不依赖那个环境变量(仍附带设上,双保险)。
"""

import asyncio
import ast
import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path
from unittest import mock

from sessionhelper import app
from sessionhelper.app import SessionHelper
from sessionhelper.config import load_config

ROOT = Path(__file__).resolve().parents[1]   # tools/sessionhelper
REPO_ROOT = ROOT.parents[1]                  # 仓根
if str(ROOT) not in sys.path:
    sys.path.insert(0, str(ROOT))

BASE_ENV = {
    "SH_SESSION_NAME": "hubnoticetest",
    "SH_OWNER": "zzc",
    "SH_KEY_ID": "FB-2642",
    "SH_SECRET": "secret",
    "SH_MODE": "cli",
}

SELF_ADDR = "hubnoticetest#FB-2642"
HUB_ADDR = "workpulse#FB-2642"
PEER_ADDR = "zzc-coder56solzzc-2642"
NOTICE_REASON = "本轮的来源记录已不可用,内容未自动回程"


class FakeWS:
    """按顺序吐出给定信封,并收集 helper 的出站。"""

    def __init__(self, envs):
        self.raw = [json.dumps(e, ensure_ascii=False) for e in envs]
        self.sent = []

    def __aiter__(self):
        return self

    async def __anext__(self):
        if not self.raw:
            raise StopAsyncIteration
        return self.raw.pop(0)

    async def send(self, payload):
        self.sent.append(json.loads(payload))


class FakeAdapter:
    def __init__(self):
        self.handled = []

    def is_idle(self):
        return True

    def handle(self, env):
        self.handled.append(env)
        return ""


def build_helper(tmp):
    """在临时 HOME 下构造 SessionHelper。

    🔴 load_config 必须在 patch HOME 的作用域【内】调用 —— 它当场展开 `~`。
       反例:test_sessionhelper.py:1538 先 load_config 再 patch HOME,
       于是 provision 的 update_state 残留写进了真 HOME,300s 后下一次
       discover 会被 os._exit(0) 静默带走(rc=0、无汇总行)。
    """
    env = dict(
        BASE_ENV,
        SH_INBOX_DB=str(Path(tmp) / "inbox.db"),
        SH_PROVISION_AUDIT_DB=str(Path(tmp) / "audit.db"),
        SH_PROVISION_VERSIONS_FILE=str(Path(tmp) / "versions.json"),
        SH_PROVISION_STATE_FILE=str(Path(tmp) / "state.json"),
    )
    with mock.patch.dict("os.environ", {"HOME": str(tmp), "SH_PROVISION_NO_EXIT": "1"}):
        helper = SessionHelper(load_config(env))
    helper.adapter = FakeAdapter()
    return helper


def run_recv(helper, envs):
    ws = FakeWS(envs)
    out = io.StringIO()
    with redirect_stdout(out):
        asyncio.run(helper.recv_loop(ws))
    return ws, out.getvalue()


def injection_evidence(helper):
    """三个可能把信封变成「新一轮」的出口,逐个量。

    只量 adapter.handle 是不够的:CLI 模式下 buffer_if_busy 会把信封塞进
    pending_inbound 由 pending_drain_loop 稍后注入,durable 路径则入 inbox
    由 delivery_worker_loop 注入。「没有新增轮次」必须三个出口都为空。
    """
    return {
        "adapter_handled": len(helper.adapter.handled),
        "pending_inbound": len(helper.pending_inbound),
        "inbox_claimable": helper.delivery_inbox.claim_next() is not None,
    }


NO_INJECTION = {"adapter_handled": 0, "pending_inbound": 0, "inbox_claimable": False}


def spec_hiddenimports(spec_path: Path) -> list[str]:
    """用 AST 从 sessionhelper.spec 里取出 hiddenimports 那张表(不用 grep)。

    🔴 为什么用 AST 而不是 grep/子串断言 —— 两刀都实测过,结论要写准:
       M7  只删列表项(spec 现有注释写的是 hub_meta.py,**不含**带点号的全串)
           ⇒ 子串断言**也会红**(实测 grep -c 该全串 = 0)⇒ 🔴 这一刀**证不出**取法的必要。
       M7b 删列表项,同时留一句含完整点号串的散文注释(现实里很容易出现,例如
           "本表已登记 sessionhelper.hub_meta")⇒ 子串断言**绿着放过**(实测命中 1),本格红。
       ⇒ 所以理由不是"grep 一定被骗",而是:**断言的真值必须只取决于那张表,
          不取决于文件里写了什么散文。** AST 是结构性的,子串靠运气。
       (与复验方证「生成器里没有 type 字面量」时用 AST 扫常量、而非 grep,是同一个道理。)
    🔴 取不到那张表时**抛错**,不返回空表:形状变了属于「前提不成立」,
       不能让它长得像「判据通过」。
    """
    tree = ast.parse(spec_path.read_text(encoding="utf-8"))
    for node in ast.walk(tree):
        if not isinstance(node, ast.Call):
            continue
        if getattr(node.func, "id", None) != "Analysis":
            continue
        for kw in node.keywords:
            if kw.arg != "hiddenimports" or not isinstance(kw.value, (ast.List, ast.Tuple)):
                continue
            return [e.value for e in kw.value.elts
                    if isinstance(e, ast.Constant) and isinstance(e.value, str)]
    raise AssertionError(
        "sessionhelper.spec 里找不到 Analysis(hiddenimports=[...]) ⇒ 打包件形状变了。"
        "这是【前提不成立】,不是判据通过:请更新本格的取法,🔴 不要放宽成 grep 子串断言。"
    )


def business_env(delivery_id="dlv_biz_1", body="请复核这一格"):
    return {
        "id": "msg-biz-1",
        "from": PEER_ADDR,
        "to": SELF_ADDR,
        "body": body,
        "meta": {"type": "delivery", "ack_required": True, "delivery_id": delivery_id},
    }


def notice_env(meta_type, env_id=None, source_id="dlv_src_1"):
    """与 Hub 侧真实信封同形(hub.go 的 deliveryFailureEnvelopeWithOriginal /
    amr.go 的 notify):From 是 workpulse,Meta 只有 type 与 source_id,
    且【不带】system=true —— 这正是 is_no_mirror_envelope 判否、落到兜底注入的原因。
    """
    if env_id is None:
        env_id = "delivery-failure-abc123" if meta_type.endswith("failure") else "amr-notice-abc123"
    return {
        "id": env_id,
        "from": HUB_ADDR,
        "to": SELF_ADDR,
        "body": "【投递失败】" + env_id + "；原因：" + NOTICE_REASON,
        "meta": {"type": meta_type, "source_id": source_id},
    }


class HubNoticeNoInjectTest(unittest.TestCase):
    def test_01_business_delivery_still_enters_injection_path(self):
        """判据①(阳格):type=delivery + ack_required 的普通业务投递仍必须被注入。

        🔴 变异 M1:把 is_hub_notice_envelope 的判据掏空成恒真 ⇒ 本格必须红。
           这就是 T-045 那次的形状(把该投的也丢了,计数还说成功)。
        """
        with tempfile.TemporaryDirectory() as tmp:
            helper = build_helper(tmp)
            ws, _ = run_recv(helper, [business_env()])
            self.assertEqual(len(ws.sent), 1, "判据①:业务投递须回且仅回一条 ACK")
            self.assertEqual(ws.sent[0]["meta"]["state"], "delivered",
                             "判据①:ACK 状态须为 delivered,否则 Hub 会当失败重投")
            self.assertTrue(injection_evidence(helper)["inbox_claimable"],
                            "判据①:业务投递必须进入注入路径(inbox 可 claim)。"
                            "本格红 ⇒ 新分支把该投的也吞了(T-045 的形状),不是"
                            "「对照没有牙」。")

    def test_01b_plain_business_message_still_enters_injection_path(self):
        """判据①(阳格·真正对变异敏感的那一格):不带 delivery 元数据的业务消息仍须被注入。

        🔴 为什么必须有本格(实测,不是推理):变异 M1(把 is_hub_notice_envelope 掏空成
           恒真)打不红 test_01 —— 因为 type=delivery + ack_required 的信封在 dispatch 链上
           被更前面的 is_delivery 先接走了,根本走不到新分支。
           ⇒ 只留 test_01 就是「断在一个看不见变异的量上」(本板已立的纪律)。
           本格走的是兜底路径,M1 之下必须红。
        """
        with tempfile.TemporaryDirectory() as tmp:
            helper = build_helper(tmp)
            env = {"id": "msg-plain-1", "from": PEER_ADDR, "to": SELF_ADDR,
                   "body": "请复核这一格", "meta": {}}
            run_recv(helper, [env])
            evidence = injection_evidence(helper)
            self.assertEqual(evidence["pending_inbound"], 1,
                             "判据①b:普通业务消息必须进入注入路径(CLI 模式下经 "
                             "buffer_if_busy 入 pending_inbound,由 pending_drain_loop 注入)。"
                             "本格红 ⇒ 新分支把该投的也吞了 —— 这正是 T-045 的形状。")
            self.assertEqual(evidence["adapter_handled"], 0,
                             "判据①b:recv_loop 不得直接调 adapter(CLI 模式一律先入队)")
            self.assertEqual(evidence["inbox_claimable"], False,
                             "判据①b:无 delivery_id 的消息不该出现在 inbox 里")

    def test_01c_no_mirror_business_message_still_enters_injection_path(self):
        """判据①(阳格·T-045 的原始受害形状):no_mirror=true 的业务信封仍须被注入。

        T-045:把 no_mirror 当丢弃理由 ⇒ AMR 提及被静默丢弃、计数还说成功。
        本单改的正是它隔壁 ⇒ 这一格钉住「no_mirror 单独出现不构成丢弃理由」。
        """
        with tempfile.TemporaryDirectory() as tmp:
            helper = build_helper(tmp)
            env = {"id": "msg-nm-1", "from": PEER_ADDR, "to": SELF_ADDR,
                   "body": "请复核这一格", "meta": {"no_mirror": True}}
            run_recv(helper, [env])
            self.assertEqual(injection_evidence(helper)["pending_inbound"], 1,
                             "判据①c:no_mirror=true 的业务信封必须仍被注入"
                             "(no_mirror 只管要不要镜像到外部面,不是「不要投递」)")

    def test_02_notice_leaves_trace_with_coordinates_but_no_body(self):
        """判据②(阳格):通知必须留痕,且留痕要能定位;正文不得进日志。

        🔴 变异 M2:删掉留痕那一行(只留不注入)⇒ 本格必须红。
           只「不注入」不「留痕」不算过 —— 那是把可见故障换成不可见故障。
        """
        with tempfile.TemporaryDirectory() as tmp:
            helper = build_helper(tmp)
            _, log = run_recv(helper, [notice_env("delivery_failure")])
            self.assertIn("[hub-notice]", log,
                          "判据②:通知必须留痕(可 grep 的一行),否则故障从可见变不可见")
            for needle in ("type=delivery_failure", "source_id=dlv_src_1",
                           "id=delivery-failure-abc123", "body_len="):
                self.assertIn(needle, log,
                              "判据②:留痕必须带定位坐标,缺 " + needle +
                              "(本次事故的直接代价就是通知里没有 delivery_id/目标)")
            self.assertNotIn(NOTICE_REASON, log,
                             "判据②:留痕不得写正文 —— 与 recv_loop 既有口径一致"
                             "(正文可能含凭据,日志不是它该去的地方)")

    def test_03_notice_is_not_injected_on_any_path(self):
        """判据③(阴格):两类通知都不得注入 CLI ⇒ 断言「没有新增轮次」。

        🔴 变异 M3(= 派工单判据④):把 recv_loop 里那个分支整段删掉
           ⇒ 本格必须显式断言红(超时/panic/编译红不算)。
        """
        for meta_type in ("delivery_failure", "delivery_result"):
            with self.subTest(meta_type=meta_type), tempfile.TemporaryDirectory() as tmp:
                helper = build_helper(tmp)
                ws, _ = run_recv(helper, [notice_env(meta_type)])
                self.assertEqual(injection_evidence(helper), dict(NO_INJECTION),
                                 "判据③:" + meta_type + " 被注入 CLI 了(环没断)。"
                                 "三个出口(adapter.handle / pending_inbound / inbox)"
                                 "必须全空。本格红 ⇒ 先确认 recv_loop 里 hub-notice"
                                 "那个分支还在、判据没被放宽。")
                self.assertEqual(ws.sent, [],
                                 "判据③:通知不得触发任何出站(不回 ACK、不回 reply)"
                                 "—— 出站本身又会成为新一轮的素材")

    def test_03b_notice_does_not_swallow_the_next_business_message(self):
        """判据③的邻格:同一条连接上「通知 + 业务」先后到达,业务仍须被注入。

        防的是「分支写成了吞掉其后所有帧」这一类过度屏蔽。
        """
        with tempfile.TemporaryDirectory() as tmp:
            helper = build_helper(tmp)
            ws, _ = run_recv(helper, [notice_env("delivery_result"), business_env()])
            self.assertEqual(injection_evidence(helper), {"adapter_handled": 0,
                                                          "pending_inbound": 0,
                                                          "inbox_claimable": True},
                             "邻格:通知之后的那条业务投递必须照常进入注入路径")
            self.assertEqual(len(ws.sent), 1, "邻格:只对业务投递回一条 ACK")

    def test_03c_helper_does_not_write_into_the_real_home(self):
        """本文件自己的卫生格:跑完不得在真 HOME 留下 provision 残留。

        留下残留 = 给下一次 `unittest discover` 埋一颗 rc=0 的静默炸弹(见文件头)。
        """
        real_home = Path.home()
        before = set(p.name for p in (real_home / ".dingwei").glob("sessionhelper_update_state-*")) \
            if (real_home / ".dingwei").exists() else set()
        with tempfile.TemporaryDirectory() as tmp:
            helper = build_helper(tmp)
            run_recv(helper, [notice_env("delivery_failure"), business_env()])
            self.assertEqual(injection_evidence(helper)["inbox_claimable"], True)
        after = set(p.name for p in (real_home / ".dingwei").glob("sessionhelper_update_state-*")) \
            if (real_home / ".dingwei").exists() else set()
        self.assertEqual(after - before, set(),
                         "本测试把 provision 状态写进了真 HOME(会话名 "
                         + str(sorted(after - before)) + ");load_config 必须在 patch "
                         "HOME 的作用域内调用")


class HubMetaSameSourceTest(unittest.TestCase):
    """判据(同源):helper 侧的两个 type 串必须由 Hub 侧单一真源生成。

    派工单第三节:「不得在 helper 里另抄一份字面量 —— 两份必漂移」。
    """

    def test_04_generated_module_matches_go_const_block(self):
        import gen_hub_meta
        committed = (ROOT / "sessionhelper" / "hub_meta.py").read_text(encoding="utf-8")
        rendered = gen_hub_meta.render(REPO_ROOT)
        self.assertEqual(committed, rendered,
                         "判据④(同源):tools/sessionhelper/sessionhelper/hub_meta.py 与 "
                         "internal/model/model.go 的 hub-meta-types 块不一致。"
                         "改了 Go 侧 ⇒ 重跑 python3 tools/sessionhelper/gen_hub_meta.py;"
                         "手改了生成件 ⇒ 改回去(生成件不许手改)。")

    def test_05_go_block_pins_the_two_notice_types(self):
        import gen_hub_meta
        values = gen_hub_meta.parse_go_block(REPO_ROOT)
        for name, want in (("MetaTypeDeliveryFailure", "delivery_failure"),
                           ("MetaTypeDeliveryResult", "delivery_result")):
            self.assertEqual(values.get(name), want,
                             "判据④:Go 侧单一真源里 " + name + " 不是 " + want +
                             "。若这是有意的改动,须同时重跑生成器并更新本格。")

    def test_06_helper_branch_keys_off_generated_constants(self):
        from sessionhelper import hub_meta
        for name in ("HUB_META_TYPE_DELIVERY_FAILURE", "HUB_META_TYPE_DELIVERY_RESULT"):
            value = getattr(hub_meta, name)
            self.assertTrue(app.is_hub_notice_envelope({"meta": {"type": value}}),
                            "判据④:helper 的分支认不出由 Go 侧生成的 " + name +
                            " ⇒ 两侧已经分家(通知会重新被注入)")
        self.assertFalse(app.is_hub_notice_envelope({"meta": {"type": "delivery"}}),
                         "判据④:业务投递 type=delivery 不得被认成通知")
        self.assertFalse(app.is_hub_notice_envelope({"meta": {}}),
                         "判据④:无 meta.type 的信封不得被认成通知")
        self.assertFalse(app.is_hub_notice_envelope({}),
                         "判据④:无 meta 的信封不得被认成通知")

    NOTICE_LITERALS = ('"delivery_failure"', "'delivery_failure'", '"delivery_result"', "'delivery_result'")

    @staticmethod
    def _scanned_sources():
        """扫描范围：sessionhelper/*.py，除 hub_meta.py（单一真源的生成件，字面量本就在那儿）。

        R3-B 起判定（含 is_hub_notice_envelope）搬到了 routing.py；原先只扫 app.py，搬走后会悄悄失效，
        所以范围扩到整个包——只扩范围、不放宽断言。
        """
        pkg = ROOT / "sessionhelper"
        return {p.name: p.read_text(encoding="utf-8") for p in sorted(pkg.glob("*.py")) if p.name != "hub_meta.py"}

    def test_07_helper_source_carries_no_hardcoded_notice_literals(self):
        """机器钉住「不得另抄字面量」这条,不靠人记得。

        🔴 变异 M4:把 helper 源码里引用的常量换回字面量 ⇒ 本格必须红。
        """
        sources = self._scanned_sources()
        self.assertIn("app.py", sources)
        self.assertIn("routing.py", sources, "扫描范围必须含 routing.py（is_hub_notice_envelope 的新家）")
        for name, src in sources.items():
            for lit in self.NOTICE_LITERALS:
                self.assertNotIn(lit, src,
                                 "判据:helper 侧不得另抄 type 字面量(两份必漂移);"
                                 "请引用 sessionhelper/hub_meta.py 里的常量。命中:" + lit + " 于 " + name)

    def test_07b_m4_goes_red_when_a_literal_lands_in_routing(self):
        """M4 的反向对照（R3-B）：往 routing.py 塞一个字面量，M4 的判据必须命中。

        直接用 test_07 同一组字面量与同一扫描范围，对"routing.py 源码＋注入的字面量"判定；
        同时验证真实的 routing.py 通过常量引用 hub_meta、自身不含字面量（阳性对照，防检查空转）。
        """
        routing_src = self._scanned_sources()["routing.py"]
        self.assertIn("HUB_META_TYPE_DELIVERY_FAILURE", routing_src, "阳性对照:routing.py 应经常量引用 hub_meta")
        self.assertFalse(any(lit in routing_src for lit in self.NOTICE_LITERALS))
        for injected in ('x = "delivery_failure"\n', "x = 'delivery_result'\n"):
            self.assertTrue(any(lit in routing_src + "\n" + injected for lit in self.NOTICE_LITERALS),
                            "M4 对注入 routing.py 的字面量没有反应 ⇒ 检查失效")

    def test_08_packaging_contract_registers_hub_meta(self):
        """判据(打包合同):sessionhelper.spec 的 hiddenimports 必须显式登记 hub_meta。

        由来:组长裁定「现在就显式登记」+ 复验方要求「补这一行**及对应合同断言**」。
        🔴 变异(每刀单独红、还原后 git diff 为空):
           M7  只删列表项(注释留下)        ⇒ 本格红;🔵 但子串断言在这一刀下**也会红**,
               所以 M7 只证明本格有牙,**不**证明"必须用 AST"。
           M7b 删列表项 + 留一句含全串的散文 ⇒ 本格红,而**子串断言绿着放过**(实测)
               ⇒ 这一刀才是用 AST 的理由,见 spec_hiddenimports 的头注。
           M8  把 hiddenimports 关键字改名   ⇒ 本格红,且报的是【前提不成立】而不是判据不通过。
           M9  打掉阳性对照(取法取不到 app)⇒ 本格红,并报"取法失效,下面那条断言不算数"。
        """
        spec = ROOT / "sessionhelper.spec"
        self.assertTrue(spec.exists(), "前提不成立:找不到 " + str(spec))
        names = spec_hiddenimports(spec)
        self.assertIn("sessionhelper.app", names,
                      "阳性对照:AST 连既有的 sessionhelper.app 都取不到 ⇒ 取法失效了,"
                      "本格下面那条断言不算数(先修取法,不要读成通过)")
        self.assertIn("sessionhelper.hub_meta", names,
                      "判据(打包合同):hiddenimports 必须显式登记 sessionhelper.hub_meta。"
                      "漏收的症状是**运行时** ModuleNotFoundError —— 发现成本远高于这一行。"
                      "🔴 修法是在 Analysis(hiddenimports=[...]) 里加那一项,"
                      "不是改本格断言;也不要顺手补 inbox/provision/reply_origin"
                      "(那是另一件事,没量过就不动 —— 组长明令)。")


if __name__ == "__main__":
    unittest.main()
