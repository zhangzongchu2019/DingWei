"""transport-v2 Driver 契约（manager 定稿 2026-07-13）

三个 P0 driver 必须实现这份接口。**不要改这个文件**——有异议先找 manager，
改契约要所有人一起改，各自改自己的会写出三套不兼容的东西。

核心原则：**送达确认由协议给出，不靠猜屏幕。** 不模拟人。
"""
from __future__ import annotations

from dataclasses import dataclass, field
from typing import Iterator, Literal, Protocol

# ---------------------------------------------------------------- 回执

ReceiptState = Literal[
    "accepted",    # 会话确认收到（协议回执）—— 有这个就绝不重投
    "queued",      # 已录入，尚未开始处理（例：CLI 正忙）
    "processing",  # 正在处理
    "done",        # 处理完成（有完成边界，如 claude 的 result 帧）
    "failed",      # 明确失败 —— 可按 envelope id 幂等重投
    "unknown",     # 后端无法判断 —— 只有 PtyDriver 会返回；L3 才启用超时兜底
]


@dataclass
class DeliveryReceipt:
    id: str                     # envelope id = 幂等键
    state: ReceiptState
    source: str                 # "protocol" | "pipe" | "embed" | "pty" —— 回执可信度来源，写日志用
    detail: str = ""


# ---------------------------------------------------------------- 事件

EventKind = Literal[
    "assistant_text",   # 模型输出的文本
    "tool_call",        # 工具调用（含结果）
    "state_change",     # 忙/闲、轮次开始结束
    "raw_terminal",     # 仅 PtyDriver：原始终端字节（view 转发用）
    "error",
]


@dataclass
class SessionEvent:
    kind: EventKind
    text: str = ""
    data: dict = field(default_factory=dict)
    session_id: str = ""
    cursor: str = ""            # 单调游标。消费方持久化它；helper 重启后用它续读（见 events(since=...)）
                                # replayable_events=False 的后端可留空，但 L3 会因此启用 poll 兜底
    delivery_id: str = ""       # 该事件属于哪条 delivery（= envelope_id）。拿不到就留空。
                                # 没有它，消费方无法知道"这条 delivery 的事件收完了没有"
    turn_id: str = ""           # 底层协议的 turn / message id（诊断用；也是 driver 内部做归属的依据）


# ---------------------------------------------------------------- 能力

@dataclass(frozen=True)
class Capabilities:
    protocol_ack: bool          # 能给可信送达回执？False ⇒ L3 必须启用超时+幂等重投兜底
    replayable_events: bool     # events(since=cursor) 能补齐断线期间的事件？
                                # False ⇒ helper 重启会永久漏事件 ⇒ 已投递的消息可能卡在 processing 永不销账
                                # ⇒ L3 必须靠 poll_receipt() 主动补状态，且启动时打降级告警
                                # （claude 管道 1:1 独占，进程死了流就没了 ⇒ 必然 False）
    human_attach: bool          # 人能挂上来看/交互？（管道型 1:1 独占 ⇒ False）
    interrupt: bool             # 能打断正在跑的长任务？（示例用户硬要求；False 者不得用于人看的会话）
    resumable: bool             # 断连/重启后上下文还在？
    unattended: bool            # 无人值守可自动化？（需人工扫码/配对 ⇒ False）
    session_survives_helper_restart: bool   # helper 重启后会话还活着？（daemon=True，管道=False）
    platforms: frozenset[str]   # {"linux","darwin","windows"}


# ---------------------------------------------------------------- 契约

class Driver(Protocol):
    """一个 driver = 一种"怎么和这个 CLI 打交道"。

    L3（DeliveryManager：inbox 幂等 / FIFO / 重试上限 / FAILED 告警）只认这份契约，
    **它不再自己判断送达**，只消费 DeliveryReceipt。
    """

    caps: Capabilities

    # —— 生命周期 ——
    def start(self) -> None: ...
    def stop(self) -> None: ...
    def health(self) -> bool: ...

    # —— 投递（L3 唯一入口）——
    def deliver(self, envelope_id: str, body: str) -> DeliveryReceipt:
        """投递一条消息。**必须幂等**：同一个 envelope_id 重复调用不得产生第二条消息。"""
        ...

    def poll_receipt(self, envelope_id: str) -> DeliveryReceipt:
        """异步销账：L3 轮询直到 done/failed。unknown 才走超时重试。

        🔴 `done` 的语义（2026-07-13 实测后定死，方案 A）：
        **done = 协议完成了，【而且这条 delivery 的所有事件都已经从 events() 发出去了】。**

        为什么必须这样定（tester 实测数据）：
            poll_receipt() done : t+2.043s
            text.ended  (文本)  : t+2.311s   ← done 之后 0.27s
            step.ended  (终结)  : t+2.494s   ← done 之后 0.45s
        ⇒ 协议说"完成了"的时候，事件流里还有半秒的内容没发出来。
        ⇒ 消费方若"看到 done 就停止收集"，**view 上模型回答的最后一段会永远丢失**——
           而系统显示"一切正常"。**这是最恶劣的一种静默：答案少了一截，没有任何人知道。**

        实现要求（driver 内部定序，别把这个问题甩给消费方）：
        1. 查协议状态 == done 之后，**再检查自己的事件流是否已发出该 delivery 的终结事件**
           （靠 SessionEvent.delivery_id / turn_id 归属；终结事件：opencode=step.ended，codex=turn/completed）
        2. 事件还没排空 ⇒ **返回 processing**（detail="protocol done, draining events"），不要报 done
        3. **有界等待**：超过 EVENT_DRAIN_TIMEOUT 仍未排空 ⇒ 报 done，但
           **必须打 `[delivery][EVENTS_INCOMPLETE]` 告警 + 在 detail 里写明** —— 绝不假装事件完整

        为什么由 driver 做而不是消费方做：
        **driver 是唯一同时握着"状态通道"和"事件通道"的人。**
        服务端不保证两条通道的定序（opencode 是两条独立 HTTP 连接），
        但 adapter 的职责恰恰是**把不一致的底层，收敛成一致的契约**——
        复杂度付一次（在 driver 里），而不是让 N 个消费方（L3 / view / 镜像 / 归档）各自去踩。
        """
        ...

    # —— 事件流（喂 hub / view / 镜像）——
    def events(self, since: str | None = None) -> Iterator[SessionEvent]:
        """从 since 游标之后【重放 + 继续实时流】。since=None ⇒ 从当前位置开始。

        ⚠️ 这是被 conformance 逼出来的契约修正（2026-07-13）：
        events() 若只有"从现在开始的实时流"，则【谁晚订阅谁就永远错过】。
        生产后果：helper 重启 → 流断开 → 中间的事件永远拿不回来
                → 已投递的消息卡在 processing 永不销账 → 【消息静默卡住】
                → 这正是 P1 那个病的新版本，而我们的立身之本是"可以失败，不能静默"。

        实现要求：
        - 每个 SessionEvent 带单调 cursor，消费方持久化最后一个 cursor
        - helper 重启后用它续读，不丢事件
        - 做得到 ⇒ caps.replayable_events=True
        - 做不到（如 claude 管道，进程死了流就没了）⇒ 如实填 False，别粉饰
        各家能力：opencode 有历史 API 可补齐；codex 有 thread/read；claude 做不到。
        """
        ...

    # —— 人机通道 ——
    def interrupt(self) -> bool:
        """打断正在跑的长任务。协议型用 API（codex Turn/interrupt、opencode /api/session/{id}/interrupt），
        **不要发 Ctrl-C**。返回是否真的中断了（要以可观测的副作用取证，不以"没报错"取证）。"""
        ...

    def interrupt_delivery(self, envelope_id: str, turn_id: str) -> bool:
        """Interrupt exactly this delivery/turn; stale or unverifiable targets must fail closed."""
        ...

    def human_input(self, data: bytes) -> None:
        """view 页面的人类输入。注意：**外部消息不走这里**（走 deliver），
        这样就不会把人正在打的字挤走（P2 自然消失）。"""
        ...

    def resize(self, rows: int, cols: int) -> None: ...


# ---------------------------------------------------------------- 各家实现要点（实测得来，不照做必栽）

CODEX_NOTES = """
codex（P0-A，developer）—— app-server，JSON-RPC over stdio
- 协议定义自己导出，别猜：codex app-server generate-json-schema --out <DIR>（87 个方法）
- 关键方法：thread/start、thread/resume、turn/start（投递）、turn/interrupt（打断）、
  turn/steer（轮进行中"转向"插话）、thread/inject_items、thread/compact/start
- ⚠️ app-server 要求 stdin **保持打开**。喂完即关的管道会让它直接退出、一字节不回。
  必须长驻进程 + 持续写 stdin。
- daemon + proxy 可驱动【已在跑的】会话 ⇒ session_survives_helper_restart=True，human_attach=True
"""

OPENCODE_NOTES = """
opencode（P0-B，tester）—— serve，headless HTTP + SSE
- ⚠️ OpenAPI(GET /doc) 里 **两代 API 并存**（162 个端点）。**用 /api/* 那一代**：
    POST /api/session/{id}/prompt      投递
    POST /api/session/{id}/interrupt   打断
    POST /api/session/{id}/compact     压缩
    GET  /api/session/{id}/event       按会话 SSE（比全局 /event 干净）
  旧的 /session/{id}/{message,abort,summarize} 不要用。
- ⚠️ serve **默认无鉴权**（自曝 OPENCODE_SERVER_PASSWORD is not set; server is unsecured）
  ⇒ **只绑 127.0.0.1 + 必设 OPENCODE_SERVER_PASSWORD**。
- 人机同看：opencode attach <url>
- 顺带：/tui/append-prompt 与 /tui/submit-prompt 是**分开的原语**（放进输入框 vs 提交），
  但外部消息**根本不该走输入框**，走 /api/session/{id}/prompt。
"""

CLAUDE_NOTES = """
claude（P0-C，reviewer）—— stream-json 双向管道
- claude -p --input-format stream-json --output-format stream-json --replay-user-messages
- **送达 ACK = 回吐的 user 帧带 isReplay:true**（manager 实测）；**完成边界 = result 帧**
- 单进程可喂多轮、上下文保持（实测：第二轮准确答出第一轮让它记的词）
- 协议自报 capabilities: ["interrupt_receipt_v1","msg_lifecycle_v1"] —— 中断回执待验
- ⚠️ **没有 HTTP API** ⇒ 管道 **1:1 独占**：
    human_attach=False、session_survives_helper_restart=False
  这是 claude 的结构性短板，如实填 Capabilities，别粉饰。
"""
