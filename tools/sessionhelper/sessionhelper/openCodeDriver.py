"""OpenCodeProtocolDriver —— opencode serve REST + SSE 驱动

P0-B, tester 实现。契约见 contract.py，设计依据 DESIGN.md 第十一/十四节。

架构：
  opencode serve（独立 daemon，HTTP basic auth）← 本 driver（REST client + SSE consumer）
  人通过 opencode attach <url> 挂载，与程序共享同一 session。

API 代际：用 /api/* 那一代（DESIGN.md 第十四节）：
  投递   POST /api/session/{id}/prompt       → SessionInputAdmitted
  打断   POST /api/session/{id}/interrupt    → 204 No Content
  压缩   POST /api/session/{id}/compact      → 204 No Content
  事件   GET  /api/session/{id}/event        → SSE (text/event-stream)
  销账   GET  /api/session/{id}/message      → 按 message id 确认是否处理完成
"""
from __future__ import annotations

import json
import logging
import os
import queue
import shlex
import shutil
import sqlite3
import subprocess
import threading
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator, Optional
from urllib.request import Request, urlopen
from urllib.error import HTTPError, URLError

from .contract import (
    Capabilities,
    DeliveryReceipt,
    Driver,
    EventKind,
    ReceiptState,
    SessionEvent,
)

log = logging.getLogger(__name__)

# ---------------------------------------------------------------- 常量

# opencode 二进制:SH_OPENCODE_BIN env → PATH 解析,别写死开发机路径(否则每台都挂)
_OPENCODE_BIN = os.environ.get("SH_OPENCODE_BIN") or shutil.which("opencode") or "opencode"
DEFAULT_USERNAME = "opencode"
DEFAULT_HOST = "127.0.0.1"
DEFAULT_PORT = 0  # random
# 默认模型。可通过 constructor 的 model= 参数覆盖。
# 生产部署时应根据 API key 配置选择合适的模型。
# 注: opencode serve 对部分 provider 的处理存在兼容差异(opencode run 可用但 serve 不处理),
# 部署前务必验证所选模型在 serve 模式下的可用性。
DEFAULT_MODEL = "deepseek/deepseek-chat"

# 5 s 内没有 SSE 事件就算流断了，触发重连
SSE_READ_TIMEOUT = 30.0  # 长超时——频繁重连可能丢事件，宁可等久一点
# 重连退避
SSE_RECONNECT_BASE = 0.5
SSE_RECONNECT_MAX = 15.0
# accepted → done 超时（秒），超时打 [delivery][STALLED] 告警
# 协议回执只保证 prompt 被受理，不保证会被处理（如模型未配好）
ACCEPTED_STALL_TIMEOUT = 120.0
EVENT_DRAIN_TIMEOUT = 5.0     # poll_receipt done 后等事件排空的超时（实测差值 0.45s ×10）
DEFAULT_STORE = os.path.join(os.path.expanduser("~"), ".opencode-driver-delivery.sqlite3")


# ---------------------------------------------------------------- 工具

def _basic_auth_header(username: str, password: str) -> str:
    import base64
    raw = f"{username}:{password}"
    return "Basic " + base64.b64encode(raw.encode()).decode()


class _DeliveryStore:
    """Durable no-redelivery ledger for helper restarts."""

    def __init__(self, path: str):
        self._path = path
        self._lock = threading.Lock()
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        with self._connect() as db:
            db.execute("""CREATE TABLE IF NOT EXISTS deliveries (
                envelope_id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
                message_id TEXT NOT NULL, state TEXT NOT NULL, detail TEXT NOT NULL,
                updated_at REAL NOT NULL)""")

    def _connect(self):
        return sqlite3.connect(self._path, timeout=10)

    def get(self, envelope_id: str):
        with self._lock, self._connect() as db:
            return db.execute(
                "SELECT session_id,message_id,state,detail FROM deliveries WHERE envelope_id=?",
                (envelope_id,),
            ).fetchone()

    def upsert(self, envelope_id: str, session_id: str, message_id: str, state: str, detail: str):
        with self._lock, self._connect() as db:
            db.execute("""INSERT INTO deliveries VALUES (?,?,?,?,?,?)
                ON CONFLICT(envelope_id) DO UPDATE SET session_id=excluded.session_id,
                message_id=excluded.message_id,state=excluded.state,
                detail=excluded.detail,updated_at=excluded.updated_at""",
                (envelope_id, session_id, message_id, state, detail, time.time()))


# ---------------------------------------------------------------- 驱动

class OpenCodeProtocolDriver(Driver):
    """通过 opencode serve 的 /api/* REST + SSE 驱动一个 AI 会话。

    用法：
        driver = OpenCodeProtocolDriver(password="...", port=19877)
        driver.start()
        receipt = driver.deliver("msg-001", "帮我做...")
        for ev in driver.events():
            print(ev)
        driver.stop()
    """

    # —— 能力声明（契约 v2，如实填）——
    caps = Capabilities(
        protocol_ack=True,
        replayable_events=True,             # GET /api/session/{id}/event?after=<seq> SSE 续读 + /message 历史补齐
        human_attach=True,                  # opencode attach <url>
        interrupt=True,                     # POST /interrupt
        resumable=True,                     # SQLite 持久化
        unattended=True,                    # 无需配对/扫码/浏览器登录
        session_survives_helper_restart=True,   # daemon 独立于 helper 进程
        platforms=frozenset({"linux", "darwin", "windows"}),
    )

    # -------------------------------------------------------- 构造

    def __init__(
        self,
        password: str,
        *,
        username: str = DEFAULT_USERNAME,
        port: int = DEFAULT_PORT,
        host: str = DEFAULT_HOST,
        model: str = DEFAULT_MODEL,
        workdir: str = "",
        manage_server: bool = True,
        stall_timeout: float = ACCEPTED_STALL_TIMEOUT,
        store_path: str = DEFAULT_STORE,
    ):
        """
        password: OPENCODE_SERVER_PASSWORD（必设，serve 默认无鉴权）
        manage_server: True=start()/stop() 管理 serve 子进程；False=只连已有 server
        stall_timeout: accepted→done 超时秒数（测试时可设小值以加速 STALLED 验证）
        """
        self._host = host
        self._port = port
        self._username = username
        self._password = password
        self._model = model
        self._workdir = workdir
        self._manage_server = manage_server
        self._stall_timeout = stall_timeout
        self._store = _DeliveryStore(store_path)

        self._base_url = f"http://{host}:{port}"
        self._auth_header = _basic_auth_header(username, password)

        self._proc: Optional[subprocess.Popen] = None
        self._session_id: str = ""
        self._envelope_states: dict[str, ReceiptState] = {}  # envelope_id → last known state
        self._envelope_to_msg: dict[str, str] = {}          # envelope_id → message_id (msg_*)
        self._msg_to_envelope: dict[str, str] = {}          # message_id → envelope_id（反向映射）
        self._delivery_timestamps: dict[str, float] = {}    # envelope_id → time of deliver()
        self._delivery_terminal: set[str] = set()           # envelope_ids whose terminal event has fired
        self._delivery_drain_deadline: dict[str, float] = {}  # envelope_id → drain deadline
        self._asst_msg_to_envelope: dict[str, str] = {}      # assistant messageID → envelope_id
        self._current_delivery: str = ""                      # envelope_id of most recently admitted prompt
        self._event_seq: int = 0
        self._stop_event = threading.Event()
        self._sse_event_queues: list[queue.Queue[SessionEvent]] = []  # fan-out subscribers
        self._sse_thread: threading.Thread | None = None
        self._sse_lock = threading.Lock()

    # -------------------------------------------------------- 生命周期

    def start(self) -> None:
        """健康检查（或启动 serve 子进程）+ 启动后台 SSE 摄取线程。"""
        if self._manage_server:
            self._start_server()
        # 等 serve 就绪
        deadline = time.monotonic() + 10.0
        while time.monotonic() < deadline:
            # 进程已死 → 立刻失败,不要干等 10 秒
            if self._proc is not None and self._proc.poll() is not None:
                stderr_tail = "\n".join(getattr(self, "_stderr_tail", []) or [])
                raise RuntimeError(
                    f"opencode serve exited early (rc={self._proc.returncode}): {stderr_tail[:500]}"
                )
            if self.health():
                log.info("opencode serve healthy at %s", self._base_url)
                break
            time.sleep(0.3)
        else:
            raise RuntimeError(f"opencode serve not reachable at {self._base_url}")

        # 启动后台 SSE 摄取线程（常驻，不依赖 events() 被消费）
        self._stop_event.clear()
        self._sse_thread = threading.Thread(target=self._sse_background_reader, daemon=True)
        self._sse_thread.start()

    def stop(self) -> None:
        """停止事件流，若管理了子进程则终止。"""
        self._stop_event.set()
        if self._sse_thread is not None:
            self._sse_thread.join(timeout=3)
            self._sse_thread = None
        if self._proc is not None:
            self._proc.terminate()
            try:
                self._proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                self._proc.kill()
            self._proc = None

    def health(self) -> bool:
        """GET /api/health → {"healthy":true}

        使用短超时（3s）—— serve 刚启动时端口可能已监听但 HTTP 尚未就绪，
        此时 connect 成功但 read 阻塞；30s 默认超时会撑爆 start() 的 10s 等待窗口。
        """
        try:
            code, body = self._request("GET", "/api/health", timeout=3)
            if code == 200:
                data = json.loads(body)
                return data.get("healthy", False)
        except Exception:
            pass
        return False

    # -------------------------------------------------------- 投递

    def deliver(self, envelope_id: str, body: str) -> DeliveryReceipt:
        """投递一条消息。幂等：同一 envelope_id 重复调用不产生第二条消息。

        首次投递自动创建 session（若尚未建）。
        返回 DeliveryReceipt，state 由 prompt 响应中的 delivery 字段推导：
          "steer" → accepted（直送对话）
          "queue" → queued（排队等待）
        """
        # Durable query-first recovery: never blindly POST an admitting delivery.
        durable = self._store.get(envelope_id)
        if durable is not None and durable[2] != "failed":
            session_id, msg_id, state, detail = durable
            self._session_id = session_id
            self._envelope_to_msg[envelope_id] = msg_id
            self._msg_to_envelope[msg_id] = envelope_id
            self._envelope_states[envelope_id] = state
            self._current_delivery = envelope_id if state in ("accepted", "processing", "queued") else ""
            return DeliveryReceipt(id=envelope_id, state=state, source="protocol",
                                   detail=f"persistent idempotency hit: {detail}")

        # 幂等：已投过则返回已知状态
        prev = self._envelope_states.get(envelope_id)
        if prev is not None and prev != "unknown":
            return DeliveryReceipt(
                id=envelope_id, state=prev, source="protocol",
                detail=f"cached state for {envelope_id}"
            )

        # 确保有 session
        if not self._session_id:
            self._ensure_session()

        # 幂等 message ID（API 要求 id 以 "msg_" 开头）
        msg_id = self._make_msg_id(envelope_id)
        self._envelope_to_msg[envelope_id] = msg_id
        self._msg_to_envelope[msg_id] = envelope_id

        # POST /api/session/{id}/prompt
        payload = {
            "prompt": {"text": body},
            "id": msg_id,
        }
        code, resp_body = self._request(
            "POST", f"/api/session/{self._session_id}/prompt", payload
        )

        if code == 200:
            data = json.loads(resp_body).get("data", {})
            delivery = data.get("delivery", "steer")
            state: ReceiptState = "accepted" if delivery == "steer" else "queued"
            self._envelope_states[envelope_id] = state
            self._current_delivery = envelope_id
            detail = f"delivery={delivery} admittedSeq={data.get('admittedSeq')}"
            self._store.upsert(envelope_id, self._session_id, msg_id, state, detail)
            if state == "accepted":
                self._delivery_timestamps[envelope_id] = time.monotonic()
            return DeliveryReceipt(
                id=envelope_id, state=state, source="protocol",
                detail=detail
            )
        elif code == 409:
            # opencode 服务端按 message id 去重，重复投递返回 200（不是 409）。
            # 此分支保留以防未来版本行为变化。
            self._envelope_states[envelope_id] = "accepted"
            self._current_delivery = envelope_id
            self._store.upsert(envelope_id, self._session_id, msg_id, "accepted", "server conflict")
            return DeliveryReceipt(
                id=envelope_id, state="accepted", source="protocol",
                detail="conflict - already delivered (server returned 409)"
            )
        else:
            self._envelope_states[envelope_id] = "failed"
            return DeliveryReceipt(
                id=envelope_id, state="failed", source="protocol",
                detail=f"HTTP {code}: {resp_body[:200]}"
            )

    def poll_receipt(self, envelope_id: str) -> DeliveryReceipt:
        """异步销账：查 /api/session/{id}/message 确认消息是否处理完成。

        判据：
        - 看到 assistant 消息 → done
        - session tokens 增长 → processing
        - 未找到 → 保持已知状态
        - HTTP error → failed
        """
        if not self._session_id:
            return DeliveryReceipt(
                id=envelope_id, state="unknown", source="protocol",
                detail="no session"
            )

        # STALLED 检查：accepted 超时未 done → 协议回执只保证受理，不保证处理
        current_state = self._envelope_states.get(envelope_id)
        delivered_at = self._delivery_timestamps.get(envelope_id)
        if current_state == "accepted" and delivered_at is not None:
            elapsed = time.monotonic() - delivered_at
            if elapsed > self._stall_timeout:
                log.warning(
                    "[delivery][STALLED] envelope=%s accepted but not done after %.0fs — "
                    "model may be misconfigured or session stalled. "
                    "Protocol receipt guarantees admission, not processing.",
                    envelope_id, elapsed,
                )
                self._envelope_states[envelope_id] = "failed"
                return DeliveryReceipt(
                    id=envelope_id, state="failed", source="protocol",
                    detail=f"stalled: accepted but no progress after {elapsed:.0f}s"
                )

        msg_id = self._envelope_to_msg.get(envelope_id, envelope_id)

        code, body = self._request(
            "GET", f"/api/session/{self._session_id}/message?order=asc&limit=50"
        )
        if code != 200:
            return DeliveryReceipt(
                id=envelope_id, state="unknown", source="protocol",
                detail=f"HTTP {code}"
            )

        messages = json.loads(body).get("data", [])
        found_user = False
        found_assistant_after = False
        for msg in messages:
            mid = msg.get("id", "")
            role = self._infer_role(msg)
            if mid == msg_id or mid == envelope_id:
                found_user = True
                continue
            if found_user and role == "assistant":
                found_assistant_after = True
                break
            if found_user and role == "tool":
                # 工具调用中 → processing
                continue

        if not found_user:
            # 消息可能还在队列里
            prev = self._envelope_states.get(envelope_id)
            return DeliveryReceipt(
                id=envelope_id, state=prev or "unknown", source="protocol",
                detail="message not yet visible in history"
            )
        if found_assistant_after:
            return self._check_drain_then_done(envelope_id, "assistant response found")

        # 用户在但还没回复 → processing 或保持
        # 也查 tokens
        code2, body2 = self._request(
            "GET", f"/api/session/{self._session_id}"
        )
        if code2 == 200:
            tokens = json.loads(body2).get("data", {}).get("tokens", {})
            if tokens.get("output", 0) > 0:
                return self._check_drain_then_done(envelope_id, f"tokens output={tokens['output']}")
            if tokens.get("input", 0) > 0:
                self._envelope_states[envelope_id] = "processing"
                return DeliveryReceipt(
                    id=envelope_id, state="processing", source="protocol",
                    detail=f"tokens input={tokens['input']}"
                )

        prev = self._envelope_states.get(envelope_id, "unknown")
        return DeliveryReceipt(
            id=envelope_id, state=prev, source="protocol",
            detail="no state change detected"
        )

    # -------------------------------------------------------- 事件流

    def events(self, since: str | None = None) -> Iterator[SessionEvent]:
        """事件流。since 游标 ⇒ 先补齐历史，再订阅后台 SSE 实时流。

        后台 SSE 摄取线程在 start() 时启动，常驻运行，不依赖 events() 被消费。
        支持多消费者（fan-out）：每个 events() 调用创建独立的订阅队列。
        """
        if not self._session_id:
            return

        # 先补齐历史
        if since is not None:
            yield from self._replay_history(since)
            # since 模式只补齐历史，不订阅实时流（消费方通过轮询或新 events() 调用获取后续事件）
            return

        # 订阅后台 SSE 实时流
        sub_q: queue.Queue[SessionEvent] = queue.Queue()
        with self._sse_lock:
            self._sse_event_queues.append(sub_q)

        try:
            while not self._stop_event.is_set():
                try:
                    ev = sub_q.get(timeout=1.0)
                    yield ev
                except queue.Empty:
                    continue
        finally:
            with self._sse_lock:
                if sub_q in self._sse_event_queues:
                    self._sse_event_queues.remove(sub_q)

    # -------------------------------------------------------- 后台 SSE 摄取

    def _sse_background_reader(self) -> None:
        """后台线程：持续读 SSE 流，解析事件 → 更新 drain 标记 + fan-out 到所有订阅者。

        独立于 events() 消费方：即使没人订阅，drain 标记也会正确更新。
        SSE 断开时自动重连 + 历史补齐。
        """
        backoff = SSE_RECONNECT_BASE
        while not self._stop_event.is_set():
            if not self._session_id:
                time.sleep(0.5)
                continue
            try:
                self._read_sse_stream_into_queues(backoff)
                backoff = SSE_RECONNECT_BASE
                # 正常 EOF 也补齐间隙
                if self._event_seq > 0:
                    try:
                        for ev in self._replay_history(str(self._event_seq)):
                            self._fanout_event(ev)
                    except Exception:
                        pass
            except Exception:
                if self._event_seq > 0:
                    try:
                        for ev in self._replay_history(str(self._event_seq)):
                            self._fanout_event(ev)
                    except Exception:
                        pass
                time.sleep(backoff)
                backoff = min(backoff * 2, SSE_RECONNECT_MAX)

    def _read_sse_stream_into_queues(self, backoff: float) -> None:
        """读 SSE 流，解析事件 → _map_sse_event → 更新 drain 标记 + fan-out。"""
        url = f"{self._base_url}/api/session/{self._session_id}/event"
        if self._event_seq > 0:
            url += f"?after={self._event_seq}"

        req = Request(url, headers={"Authorization": self._auth_header})
        resp = urlopen(req, timeout=SSE_READ_TIMEOUT)

        ct = resp.headers.get("Content-Type", "")
        if "text/event-stream" not in ct:
            body = resp.read().decode("utf-8", errors="replace")
            log.warning("SSE: unexpected content-type=%s body=%s", ct, body[:500])
            return

        lines_buf: list[str] = []
        while not self._stop_event.is_set():
            line = resp.readline()
            if not line:
                return
            text = line.decode("utf-8", errors="replace").rstrip("\r\n")
            if text == "":
                for buf_line in lines_buf:
                    if buf_line.startswith("data:"):
                        data_str = buf_line[5:].strip()
                        if data_str:
                            ev = self._map_sse_event("", data_str, "")
                            if ev is not None:
                                self._fanout_event(ev)
                lines_buf.clear()
                continue
            lines_buf.append(text)

    def _fanout_event(self, ev: SessionEvent) -> None:
        """把事件推送到所有订阅者队列（非阻塞）。"""
        with self._sse_lock:
            for q in self._sse_event_queues:
                try:
                    q.put_nowait(ev)
                except queue.Full:
                    pass

    def _replay_history(self, since: str) -> Iterator[SessionEvent]:
        """从 since 游标之后补齐历史事件。"""
        # 尝试解析 since 为 event seq 数字
        try:
            after_seq = int(since)
        except (ValueError, TypeError):
            after_seq = 0

        if after_seq == 0:
            # 无游标 ⇒ 从 message API 拉全部已完成的消息
            try:
                code, body = self._request(
                    "GET", f"/api/session/{self._session_id}/message?order=asc&limit=100")
                if code == 200:
                    messages = json.loads(body).get("data", [])
                    for i, msg in enumerate(messages):
                        mtype = msg.get("type", "")
                        cursor = f"msg-{i}"
                        if mtype == "assistant":
                            for c in msg.get("content", []):
                                if c.get("type") == "text" and c.get("text"):
                                    yield SessionEvent(
                                        kind="assistant_text", text=c["text"],
                                        data={"role": "assistant", "replay": True},
                                        session_id=self._session_id, cursor=cursor)
                            for c in msg.get("content", []):
                                if c.get("type") in ("tool_use", "tool_result"):
                                    yield SessionEvent(
                                        kind="tool_call", text=c.get("name", ""),
                                        data={"replay": True, "content": c},
                                        session_id=self._session_id, cursor=cursor)
                        elif mtype == "user":
                            yield SessionEvent(
                                kind="state_change", text=msg.get("text", ""),
                                data={"change": "prompted", "replay": True},
                                session_id=self._session_id, cursor=cursor)
            except Exception:
                pass  # 历史补齐失败不影响实时流
        else:
            # 有 seq 游标 ⇒ 用 history API 拉增量事件
            try:
                code, body = self._request(
                    "GET", f"/api/session/{self._session_id}/history?after={after_seq}&limit=50")
                if code == 200:
                    events = json.loads(body).get("data", [])
                    for ev in events:
                        ev_type = ev.get("type", "")
                        ev_id = ev.get("id", "")
                        ev_data = ev.get("data", {})
                        durable = ev.get("durable", {})
                        seq = durable.get("seq", 0)
                        cursor = str(seq) if seq else ev_id
                        if ev_type.endswith(".text.ended"):
                            yield SessionEvent(
                                kind="assistant_text", text=ev_data.get("text", ""),
                                data={"replay": True},
                                session_id=self._session_id, cursor=cursor)
                        elif ev_type.endswith((".step.ended", ".step.failed", ".step.started", ".prompted", ".prompt.admitted")):
                            yield SessionEvent(
                                kind="state_change", text="",
                                data={"change": ev_type, "replay": True},
                                session_id=self._session_id, cursor=cursor)
            except Exception:
                pass

    # -------------------------------------------------------- 人机通道

    def interrupt(self) -> bool:
        """POST /api/session/{id}/interrupt → 打断正在跑的任务。

        以可观测的副作用取证（对照实验设计）：
        1. POST /interrupt（返回 204 表示指令已受理）
        2. 轮询 history，查最后一个 step 事件：
           - step.failed + error.message="Provider turn interrupted" → 真被打断了 ✅
           - step.ended + finish="stop" → 模型在 interrupt 生效前已自然结束（打断太晚）
           - 超时仍无 step 事件 → 模型可能卡死（不算打断成功）
        3. 对照：同 prompt 不打断 → 必然 step.ended + finish=stop + 完整输出
        """
        envelope_id = self._current_delivery
        turn_id = self._envelope_to_msg.get(envelope_id, "")
        if not envelope_id or not turn_id:
            return False
        return self.interrupt_delivery(envelope_id, turn_id)

    def interrupt_delivery(self, envelope_id: str, turn_id: str) -> bool:
        """OpenCode interrupts the session current turn; guard it with exact local ownership."""
        if (
            not self._session_id
            or self._current_delivery != envelope_id
            or self._envelope_to_msg.get(envelope_id) != turn_id
        ):
            return False

        code, _ = self._request(
            "POST", f"/api/session/{self._session_id}/interrupt"
        )
        if code not in (204, 200):
            log.warning("interrupt returned HTTP %d", code)
            return False

        # 取证：轮询 history 查找显式中断标记
        deadline = time.monotonic() + 5.0
        while time.monotonic() < deadline:
            time.sleep(0.5)
            code, body = self._request(
                "GET", f"/api/session/{self._session_id}/history?limit=10"
            )
            if code != 200:
                continue
            events = json.loads(body).get("data", [])
            # 从后往前找最后一个 step 事件
            for ev in reversed(events):
                ev_type = ev.get("type", "")
                if not ev_type.endswith((".step.ended", ".step.failed")):
                    continue
                ev_data = ev.get("data", {})
                if ev_type.endswith(".step.failed"):
                    error = ev_data.get("error", {})
                    if isinstance(error, dict) and "interrupt" in error.get("message", "").lower():
                        log.info("interrupt: confirmed — step.failed with Provider turn interrupted")
                        return True
                    log.info("interrupt: step.failed but not interrupt-related: %s", error)
                    return False
                elif ev_type.endswith(".step.ended"):
                    finish = ev_data.get("finish", "")
                    log.info("interrupt: model finished naturally (finish=%s) before interrupt took effect", finish)
                    return False
                break  # 只看最后一个 step 事件

        log.warning("interrupt: timed out waiting for step resolution — may have worked, but unconfirmed")
        return False  # 超时未确认 = 不算成功（不许静默乐观）

    def human_input(self, data: bytes) -> None:
        """不适用——人通过 opencode attach 直接输入。"""
        pass

    def resize(self, rows: int, cols: int) -> None:
        """不适用——没有 PTY。"""
        pass

    # -------------------------------------------------------- 内部

    def _start_server(self) -> None:
        """启动 opencode serve 子进程。"""
        if self._port == 0:
            import socket
            with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
                s.bind((self._host, 0))
                self._port = s.getsockname()[1]
            self._base_url = f"http://{self._host}:{self._port}"

        env = os.environ.copy()
        env["OPENCODE_SERVER_PASSWORD"] = self._password

        workdir = self._workdir or os.getcwd()
        cmd = [
            _OPENCODE_BIN, "serve",
            "--port", str(self._port),
            "--hostname", self._host,
            "--log-level", "WARN",
            "--print-logs",           # 把日志打到 stderr,不设的话崩溃诊断信息不可见
        ]
        log.info("spawning: %s (cwd=%s)", shlex.join(cmd), workdir)
        self._proc = subprocess.Popen(
            cmd,
            cwd=workdir,
            env=env,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
        )
        self._stderr_tail: list[str] = []  # 最近 20 行 stderr,供错误上报用
        # 必须消费 stderr,否则 pipe 缓冲区满后 serve 进程会阻塞写
        threading.Thread(target=self._drain_stderr, args=(self._proc,), daemon=True).start()

    def _drain_stderr(self, proc: subprocess.Popen) -> None:
        """消费 serve 进程的 stderr，防止 pipe 缓冲区满导致进程阻塞。

        同时保留最近 20 行到 self._stderr_tail，供 start() 报错时诊断。
        """
        try:
            for line in proc.stderr:
                text = line.decode("utf-8", errors="replace").rstrip("\n")
                if text:
                    log.debug("opencode serve stderr: %s", text)
                    tail = getattr(self, "_stderr_tail", None)
                    if tail is not None:
                        tail.append(text)
                        if len(tail) > 20:
                            tail.pop(0)
        except Exception:
            pass

    def _ensure_session(self) -> None:
        """POST /api/session 创建新 session，然后钉模型。"""
        payload: dict = {}
        if self._workdir:
            payload["directory"] = self._workdir
        code, body = self._request("POST", "/api/session", payload)
        if code == 200:
            data = json.loads(body).get("data", {})
            self._session_id = data["id"]
            log.info("session created: %s", self._session_id)
        else:
            raise RuntimeError(f"Failed to create session: HTTP {code} {body[:200]}")

        # 钉模型：opencode serve 的 config 顶层 model 不被可靠采纳，
        # 会随机回落到其它模型。必须每个会话显式指定。
        if self._model:
            self._pin_model()

    def _pin_model(self) -> None:
        """POST /api/session/{id}/model — 显式钉模型。

        Payload 形状（实测）：{"model":{"providerID":"...","id":"..."}}
        注意是 "id" 不是 "modelID"。
        """
        provider_id, model_id = self._model.split("/", 1)
        payload = {"model": {"providerID": provider_id, "id": model_id}}
        code, body = self._request("POST", f"/api/session/{self._session_id}/model", payload)
        if code in (200, 204):
            log.info("model pinned: %s → HTTP %d", self._model, code)
        else:
            log.warning("failed to pin model %s: HTTP %d %s", self._model, code, body[:200])

    def _request(self, method: str, path: str, payload: dict | None = None, *, timeout: float = 30.0) -> tuple[int, str]:
        """发 HTTP 请求，返回 (status_code, body_str)。"""
        url = self._base_url + path
        data_bytes = None
        headers = {
            "Authorization": self._auth_header,
        }
        if payload is not None:
            data_bytes = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"

        req = Request(url, data=data_bytes, headers=headers, method=method)
        try:
            resp = urlopen(req, timeout=timeout)
            return resp.status, resp.read().decode("utf-8")
        except HTTPError as e:
            return e.code, e.read().decode("utf-8", errors="replace")
        except URLError as e:
            return 0, str(e)

    def _check_drain_then_done(self, envelope_id: str, protocol_detail: str) -> DeliveryReceipt:
        """协议状态已是 done → 检查事件流是否已排空该 delivery 的终结事件。

        契约 v3（方案 A）：done = 协议完成 + 事件已全部发出。
        有界等待 EVENT_DRAIN_TIMEOUT；超时打 EVENTS_INCOMPLETE 告警，绝不假装完整。
        """
        if envelope_id in self._delivery_terminal:
            # 事件已排空 → 真 done
            self._envelope_states[envelope_id] = "done"
            return DeliveryReceipt(
                id=envelope_id, state="done", source="protocol",
                detail=f"{protocol_detail}; events drained"
            )

        # 事件还没排空 → 有界等待
        now = time.monotonic()
        deadline = self._delivery_drain_deadline.get(envelope_id)
        if deadline is None:
            deadline = now + EVENT_DRAIN_TIMEOUT
            self._delivery_drain_deadline[envelope_id] = deadline

        if now < deadline:
            # 还在等 → 报 processing（不是 done）
            self._envelope_states[envelope_id] = "processing"
            return DeliveryReceipt(
                id=envelope_id, state="processing", source="protocol",
                detail=f"protocol done, draining events ({deadline - now:.1f}s remaining)"
            )

        # 超时 → 降级报 done，但打告警
        log.warning(
            "[delivery][EVENTS_INCOMPLETE] envelope=%s protocol done but terminal event "
            "not received after %.1fs — events may be incomplete",
            envelope_id, EVENT_DRAIN_TIMEOUT
        )
        self._envelope_states[envelope_id] = "done"
        return DeliveryReceipt(
            id=envelope_id, state="done", source="protocol",
            detail=f"protocol done ({protocol_detail}); WARNING: events may be incomplete (drain timeout)"
        )

    def _get_token_output(self) -> int:
        """查 session 的 output token 数。"""
        if not self._session_id:
            return 0
        code, body = self._request("GET", f"/api/session/{self._session_id}")
        if code == 200:
            return json.loads(body).get("data", {}).get("tokens", {}).get("output", 0)
        return 0

    @staticmethod
    def _make_msg_id(envelope_id: str) -> str:
        """从 envelope_id 生成合法的 opencode message ID（必须以 msg_ 开头）。"""
        import hashlib
        if envelope_id.startswith("msg_"):
            return envelope_id
        h = hashlib.sha256(envelope_id.encode()).hexdigest()[:24]
        return f"msg_{h}"

    @staticmethod
    def _infer_role(msg: dict) -> str:
        """从消息结构推断角色：user / assistant / tool。"""
        # opencode API: top-level "type" field = "user" | "assistant"
        mtype = msg.get("type", "")
        if mtype == "user":
            return "user"
        if mtype == "assistant":
            return "assistant"
        # also check content for tool calls
        content = msg.get("content", [])
        if isinstance(content, list):
            for c in content:
                ctype = c.get("type", "")
                if ctype in ("tool_use", "tool_result"):
                    return "tool"
        # legacy: check "role" field
        role = msg.get("role", "")
        if role:
            return role
        return "?"

    def _map_sse_event(self, event_type: str, data_str: str, event_id: str) -> Optional[SessionEvent]:
        """把 opencode SSE 事件映射为契约 SessionEvent。

        opencode 事件格式（实测）：
          {"id":"evt_...","type":"session.next.text.ended",
           "durable":{"aggregateID":"ses_...","seq":N,"version":M},
           "data":{...具体数据...}}
        """
        try:
            payload = json.loads(data_str)
        except json.JSONDecodeError:
            return None

        # 实际事件类型在 JSON 的 type 字段
        ev_type = payload.get("type", event_type)
        ev_id = payload.get("id", event_id)
        ev_data = payload.get("data", {})

        # 更新序列号（用于 after 重连）
        durable = payload.get("durable", {})
        seq = durable.get("seq", 0)
        if seq:
            self._event_seq = max(self._event_seq, seq)

        # 提取 delivery_id 和 turn_id（契约 v3）
        # prompt.admitted: messageID = user msg ID → 直接映射到 envelope_id
        # step.started: assistantMessageID = asst msg ID → 需要关联到当前 delivery
        # step.ended/failed: assistantMessageID → 查 _asst_msg_to_envelope
        user_msg_id = ev_data.get("messageID", "")
        asst_msg_id = ev_data.get("assistantMessageID", "")
        msg_id = user_msg_id or asst_msg_id
        delivery_id = ""
        turn_id = msg_id

        # prompt.admitted → 记录当前 delivery 的 user messageID
        if user_msg_id:
            delivery_id = self._msg_to_envelope.get(user_msg_id, "")
            if delivery_id:
                self._current_delivery = delivery_id
        # step.started → 把 assistantMessageID 关联到当前 delivery
        if asst_msg_id and self._current_delivery:
            self._asst_msg_to_envelope[asst_msg_id] = self._current_delivery
        # 用 assistantMessageID 反查 delivery_id（step.ended/failed 等后续事件用）
        if asst_msg_id and not delivery_id:
            delivery_id = self._asst_msg_to_envelope.get(asst_msg_id, "")

        # 追踪终结事件：step.ended / step.failed → 标记 delivery 事件已排空
        if delivery_id and (ev_type.endswith(".step.ended") or ev_type.endswith(".step.failed")):
            self._delivery_terminal.add(delivery_id)

        kind: EventKind
        text = ""
        extra: dict = {"raw_type": ev_type, "id": ev_id, "seq": seq}

        # 按 opencode 事件类型路由
        if ev_type.endswith(".step.started"):
            kind = "state_change"
            extra["change"] = "step_started"
        elif ev_type.endswith(".step.ended"):
            kind = "state_change"
            extra["change"] = "step_ended"
            finish = ev_data.get("finish", "")
            if finish:
                extra["finish"] = finish
            tokens = ev_data.get("tokens", {})
            if tokens:
                extra["tokens"] = tokens
        elif ev_type.endswith(".step.failed"):
            kind = "error"
            extra["error"] = ev_data.get("error", str(ev_data))
        elif ev_type.endswith(".text.started"):
            kind = "state_change"
            extra["change"] = "text_started"
        elif ev_type.endswith(".text.ended"):
            kind = "assistant_text"
            text = ev_data.get("text", "")
        elif ".tool." in ev_type:
            kind = "tool_call"
            tool_name = ev_data.get("name", ev_data.get("tool", ""))
            text = tool_name
            extra["tool"] = ev_data
        elif ev_type.endswith(".prompt.admitted") or ev_type.endswith(".prompted"):
            kind = "state_change"
            extra["change"] = "prompted"
            extra["messageID"] = ev_data.get("messageID", "")
        elif "compaction" in ev_type.lower():
            kind = "state_change"
            extra["change"] = "compaction"
        elif "error" in ev_type.lower() or "fail" in ev_type.lower():
            kind = "error"
            extra["error"] = data_str
        else:
            kind = "state_change"
            extra["change"] = ev_type

        return SessionEvent(
            kind=kind,
            text=text,
            data=extra,
            session_id=self._session_id,
            cursor=str(self._event_seq) if self._event_seq else ev_id,
            delivery_id=delivery_id,
            turn_id=turn_id,
        )


# ---------------------------------------------------------------- 自测

if __name__ == "__main__":
    import sys
    logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

    password = sys.argv[1] if len(sys.argv) > 1 else "test-pwd"
    port = int(sys.argv[2]) if len(sys.argv) > 2 else 19877

    driver = OpenCodeProtocolDriver(
        password=password,
        port=port,
        workdir=os.getcwd(),
        manage_server=False,  # 连已有 server
    )

    print("=== health ===")
    print(driver.health())

    print("\n=== deliver ===")
    receipt = driver.deliver("test-env-001", "reply with exactly the word: ACK")
    print(receipt)

    print("\n=== poll_receipt ===")
    for _ in range(6):
        time.sleep(2)
        r = driver.poll_receipt("test-env-001")
        print(r)
        if r.state in ("done", "failed"):
            break

    print("\n=== events (5s) ===")
    driver._stop_event.clear()
    start = time.monotonic()
    for ev in driver.events():
        print(f"  {ev.kind}: {ev.text[:80]} | {ev.data.get('raw_type','')}")
        if time.monotonic() - start > 5:
            break
    driver._stop_event.set()
