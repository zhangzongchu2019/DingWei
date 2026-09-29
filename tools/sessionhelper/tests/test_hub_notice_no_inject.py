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

    def test_07_helper_source_carries_no_hardcoded_notice_literals(self):
        """机器钉住「不得另抄字面量」这条,不靠人记得。

        🔴 变异 M4:把 app.py 里引用的常量换回字面量 ⇒ 本格必须红。
        """
        src = (ROOT / "sessionhelper" / "app.py").read_text(encoding="utf-8")
        for lit in ('"delivery_failure"', "'delivery_failure'",
                    '"delivery_result"', "'delivery_result'"):
            self.assertNotIn(lit, src,
                             "判据:helper 侧不得另抄 type 字面量(两份必漂移);"
                             "请引用 sessionhelper/hub_meta.py 里的常量。命中:" + lit)


if __name__ == "__main__":
    unittest.main()
