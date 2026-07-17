"""ClaudeProtocolDriver —— claude stream-json 双向管道 driver（P0-C）

契约: contract.py v3。实现要点全部实测:
- 驱动: claude -p --input-format stream-json --output-format stream-json --replay-user-messages
- 送达 ACK: claude 回吐的 user 帧带 isReplay:true
- 完成边界(终结事件): result 帧
- 中断: 往 stdin 写 {"type":"control_request","request":{"subtype":"interrupt"}}
        可观测副作用: result 帧 subtype=error_during_execution(正常完成是 success)
- 幂等: SQLite ledger(抄 codexDriver 的模式),keyed by envelope_id,跨 helper 重启成立
- 事件摄取: start() 起【常驻】后台线程读 stdout 并扇出(多消费者),不依赖 events() 被消费

claude 的结构性短板(如实填,不粉饰):
- 没有 HTTP API ⇒ 管道 1:1 独占 ⇒ human_attach=False、session_survives_helper_restart=False
- 进程死了流就没了 ⇒ replayable_events=False
这三条同源:没有 daemon、只有 1:1 管道。
"""
from __future__ import annotations

import hashlib
import json
import logging
import os
import queue
import shutil
import sqlite3
import subprocess
import threading
import time
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Iterator, Optional

from .contract import (
    Capabilities,
    DeliveryReceipt,
    Driver,
    ReceiptState,
    SessionEvent,
)

log = logging.getLogger("claude-driver")

# claude 二进制:按 SH_CLI_BIN env → PATH 解析,别写死开发机路径(否则每台都挂)
_CLAUDE_BIN = os.environ.get("SH_CLI_BIN") or shutil.which("claude") or "claude"
DEFAULT_CMD = [
    _CLAUDE_BIN,
    "-p",
    "--input-format", "stream-json",
    "--output-format", "stream-json",
    "--replay-user-messages",
    "--dangerously-skip-permissions",
    "--verbose",
]
DEFAULT_STORE = os.environ.get(
    "SH_CLAUDE_DRIVER_STORE",
    str(Path.home() / ".dingwei" / "claude-driver-delivery.sqlite3"),
)
EVENT_DRAIN_TIMEOUT = 5.0        # 协议 done 后等终结事件排空的上限（契约 v3）
TURN_ACCEPT_TIMEOUT = 30.0       # 投递后等 isReplay ACK 的上限
PREV_TURN_WAIT = 180.0           # 投下一条前，等上一个 turn 完成的上限（串行到 turn 边界）
STALL_TIMEOUT = 120.0            # accepted/processing 后迟迟不 done ⇒ [delivery][STALLED]
FANOUT_QUEUE_MAX = 2000


# ---------------------------------------------------------------- 幂等 ledger（抄 codexDriver 的持久化模式）

@dataclass
class _DeliveryRecord:
    envelope_id: str
    turn_id: str
    body_hash: str
    state: str
    detail: str


class _DeliveryStore:
    """持久化幂等账本，keyed by envelope_id。

    claude 管道无服务端去重，也不像 codex 有 clientUserMessageId。
    所以幂等完全靠这个账本：同一 envelope_id 已投过（非 failed）就不再投第二次，
    helper 重启后账本还在，不会重复投递。
    """

    def __init__(self, path: str):
        self._path = path
        self._lock = threading.Lock()
        Path(path).parent.mkdir(parents=True, exist_ok=True)
        with self._connect() as db:
            db.execute(
                """
                CREATE TABLE IF NOT EXISTS deliveries (
                    envelope_id TEXT PRIMARY KEY,
                    turn_id TEXT NOT NULL,
                    body_hash TEXT NOT NULL,
                    state TEXT NOT NULL,
                    detail TEXT NOT NULL DEFAULT '',
                    created_at REAL NOT NULL,
                    updated_at REAL NOT NULL
                )
                """
            )

    def _connect(self) -> sqlite3.Connection:
        return sqlite3.connect(self._path, timeout=10)

    def get(self, envelope_id: str) -> Optional[_DeliveryRecord]:
        with self._lock, self._connect() as db:
            row = db.execute(
                "SELECT envelope_id, turn_id, body_hash, state, detail FROM deliveries WHERE envelope_id=?",
                (envelope_id,),
            ).fetchone()
        if not row:
            return None
        return _DeliveryRecord(*row)

    def upsert(self, envelope_id: str, turn_id: str, body_hash: str, state: str, detail: str = "") -> None:
        now = time.time()
        with self._lock, self._connect() as db:
            db.execute(
                """
                INSERT INTO deliveries (envelope_id, turn_id, body_hash, state, detail, created_at, updated_at)
                VALUES (?,?,?,?,?,?,?)
                ON CONFLICT(envelope_id) DO UPDATE SET
                    turn_id=excluded.turn_id, state=excluded.state,
                    detail=excluded.detail, updated_at=excluded.updated_at
                """,
                (envelope_id, turn_id, body_hash, state, detail, now, now),
            )

    def update_state(self, envelope_id: str, state: str, detail: str = "") -> None:
        with self._lock, self._connect() as db:
            db.execute(
                "UPDATE deliveries SET state=?, detail=?, updated_at=? WHERE envelope_id=?",
                (state, detail, time.time(), envelope_id),
            )


# ---------------------------------------------------------------- 每个 delivery 的运行时状态

@dataclass
class _TurnState:
    envelope_id: str
    turn_id: str
    accepted: threading.Event          # 看到 isReplay:true 的 user 帧
    terminal_seen: threading.Event     # 看到 result 帧（= 终结事件已发出）
    state: ReceiptState = "processing"
    detail: str = ""
    interrupted: bool = False
    submitted_at: float = 0.0
    stalled_logged: bool = False


class ClaudeProtocolDriver(Driver):
    """通过 claude -p stream-json 管道驱动一个 AI 会话。"""

    def __init__(
        self,
        *,
        cmd: list[str] | None = None,
        cwd: str | None = None,
        env: dict[str, str] | None = None,
        store_path: str = DEFAULT_STORE,
    ):
        self._cmd = cmd or DEFAULT_CMD
        self._cwd = cwd or os.getcwd()
        self._env = env
        self._store = _DeliveryStore(store_path)
        self._stall_timeout = STALL_TIMEOUT   # 实例属性，conformance 可设短以验 STALLED 路径

        self._proc: Optional[subprocess.Popen] = None
        self._session_id = ""
        self._stop = threading.Event()
        self._write_lock = threading.Lock()      # 串行化 stdin 写（FIFO/exactly-once）
        self._turn_lock = threading.Lock()        # 一次只有一个 active turn

        # —— 常驻事件摄取 + 多消费者扇出（从一开始就避免单消费者耦合）——
        self._reader_thread: Optional[threading.Thread] = None
        self._consumers: list[queue.Queue] = []
        self._consumers_lock = threading.Lock()
        self._event_seq = 0
        self._history: list[SessionEvent] = []    # 供 events(since=) 重放（同进程内）
        self._history_lock = threading.Lock()

        # —— turn 归属：claude 管道是串行的，任一时刻至多一个 active turn ——
        self._active: Optional[_TurnState] = None
        self._active_lock = threading.Lock()

    # ---------------------------------------------------------------- 能力（如实填）

    caps = Capabilities(
        protocol_ack=True,                      # isReplay ACK + result 帧
        replayable_events=False,                # 进程死流就没了，补不了断线期间的事件
        human_attach=False,                     # 管道 1:1 独占，人挂不上去
        interrupt=True,                         # control_request interrupt，实测有可观测副作用
        resumable=True,                         # 同进程内多轮上下文保持（实测）
        unattended=True,                        # 无需配对/登录
        session_survives_helper_restart=False,  # helper 死会话陪葬
        platforms=frozenset({"linux", "darwin", "windows"}),
    )

    # ---------------------------------------------------------------- 生命周期

    def start(self) -> None:
        if self._proc is not None and self._proc.poll() is None:
            return
        env = dict(os.environ)
        env.setdefault("IS_SANDBOX", "1")  # claude 以 root 跑 --dangerously-skip-permissions 需要,否则拒启动=僵尸
        if self._env:
            env.update(self._env)
        self._proc = subprocess.Popen(
            self._cmd,
            cwd=self._cwd,
            env=env,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            text=True,
            bufsize=1,
        )
        self._stop.clear()
        # 常驻后台读线程：一直读 stdout、更新状态、扇出。不依赖有人消费 events()。
        self._reader_thread = threading.Thread(target=self._read_loop, daemon=True)
        self._reader_thread.start()

    def stop(self) -> None:
        self._stop.set()
        if self._proc is not None:
            try:
                self._proc.stdin.close()
            except Exception:
                pass
            try:
                self._proc.terminate()
                self._proc.wait(timeout=5)
            except Exception:
                try:
                    self._proc.kill()
                except Exception:
                    pass
        # 唤醒所有消费者
        with self._consumers_lock:
            for q in self._consumers:
                try:
                    q.put_nowait(None)
                except Exception:
                    pass

    def health(self) -> bool:
        # 以可观测状态取证：进程活着 + 读线程活着。reader 退出 ⇒ 立即 False（不假健康）。
        proc_ok = self._proc is not None and self._proc.poll() is None
        reader_ok = self._reader_thread is not None and self._reader_thread.is_alive()
        return proc_ok and reader_ok

    # ---------------------------------------------------------------- 投递

    def deliver(self, envelope_id: str, body: str) -> DeliveryReceipt:
        if not self.health():
            self.start()

        body_hash = hashlib.sha256(body.encode("utf-8")).hexdigest()

        # 幂等：已投过且非 failed ⇒ 直接返回已知状态（跨 helper 重启也成立）
        existing = self._store.get(envelope_id)
        if existing is not None and existing.state != "failed":
            return DeliveryReceipt(
                id=envelope_id, state=self._coerce_state(existing.state), source="pipe",
                detail=f"idempotent hit: turn={existing.turn_id} state={existing.state}",
            )

        # 串行化：claude 管道一次只处理一个 turn。**实测**快速连发多帧会串味
        # （AAA/BBB/CCC → BBB 那轮答成 CCC），和 codex turn 合并同类。
        # 所以必须【串行到 turn 完成】：投下一条前，等上一个 turn 的 result 帧。
        with self._turn_lock:
            with self._active_lock:
                prev = self._active
            if prev is not None and prev.envelope_id != envelope_id and not prev.terminal_seen.is_set():
                if not prev.terminal_seen.wait(PREV_TURN_WAIT):
                    log.warning("[delivery][SERIAL_WAIT_TIMEOUT] prev turn %s not terminal after %.0fs; "
                                "proceeding may risk merge", prev.envelope_id, PREV_TURN_WAIT)

            turn_id = uuid.uuid4().hex
            ts = _TurnState(
                envelope_id=envelope_id, turn_id=turn_id,
                accepted=threading.Event(), terminal_seen=threading.Event(),
            )
            ts.submitted_at = time.monotonic()
            with self._active_lock:
                self._active = ts
            self._store.upsert(envelope_id, turn_id, body_hash, "processing", "sent")

            frame = {
                "type": "user",
                "message": {"role": "user", "content": [{"type": "text", "text": body}]},
            }
            try:
                with self._write_lock:
                    assert self._proc is not None and self._proc.stdin is not None
                    self._proc.stdin.write(json.dumps(frame, ensure_ascii=False) + "\n")
                    self._proc.stdin.flush()
            except Exception as exc:
                self._store.update_state(envelope_id, "failed", f"stdin write failed: {exc}")
                return DeliveryReceipt(id=envelope_id, state="failed", source="pipe", detail=str(exc))

            # 等 isReplay ACK（协议级送达确认）
            if ts.accepted.wait(TURN_ACCEPT_TIMEOUT):
                self._store.update_state(envelope_id, "accepted", "isReplay ack")
                return DeliveryReceipt(
                    id=envelope_id, state="accepted", source="pipe",
                    detail=f"isReplay ack, turn={turn_id}",
                )
            # 没等到 ACK ⇒ unknown（不谎报成功；L3 会走超时逻辑）
            self._store.update_state(envelope_id, "processing", "no isReplay ack yet")
            return DeliveryReceipt(
                id=envelope_id, state="processing", source="pipe",
                detail=f"submitted, awaiting ack, turn={turn_id}",
            )

    def poll_receipt(self, envelope_id: str) -> DeliveryReceipt:
        rec = self._store.get(envelope_id)
        if rec is None:
            return DeliveryReceipt(id=envelope_id, state="unknown", source="pipe", detail="no record")
        if rec.state == "failed":
            return DeliveryReceipt(id=envelope_id, state="failed", source="pipe", detail=rec.detail)

        # 找 active turn（可能已切走）
        with self._active_lock:
            active = self._active
        is_active = active is not None and active.envelope_id == envelope_id

        if is_active and active is not None:
            if active.interrupted:
                self._store.update_state(envelope_id, "failed", "interrupted")
                return DeliveryReceipt(id=envelope_id, state="failed", source="pipe", detail="turn interrupted")
            # 契约 v3 的 done 语义：协议 done + 终结事件(result 帧)已发出，才算 done
            if active.terminal_seen.is_set():
                self._store.update_state(envelope_id, "done", "result frame drained")
                return DeliveryReceipt(id=envelope_id, state="done", source="pipe", detail="result drained")
            # 停滞不静默：accepted/processing 后迟迟不 done ⇒ [delivery][STALLED] + failed
            elapsed = time.monotonic() - (active.submitted_at or time.monotonic())
            if elapsed > self._stall_timeout:
                if not active.stalled_logged:
                    active.stalled_logged = True
                    log.warning("[delivery][STALLED] id=%s elapsed=%.0fs no terminal event; "
                                "turn appears hung", envelope_id, elapsed)
                self._store.update_state(envelope_id, "failed", f"stalled after {elapsed:.0f}s")
                return DeliveryReceipt(id=envelope_id, state="failed", source="pipe",
                                       detail=f"[STALLED] no terminal after {elapsed:.0f}s")
            if active.accepted.is_set():
                return DeliveryReceipt(id=envelope_id, state="processing", source="pipe",
                                       detail="accepted, draining events")
            return DeliveryReceipt(id=envelope_id, state="processing", source="pipe", detail="submitted")

        # 不是 active（turn 已结束并切走）⇒ ledger 里的终态就是结论
        return DeliveryReceipt(id=envelope_id, state=self._coerce_state(rec.state), source="pipe", detail=rec.detail)

    # ---------------------------------------------------------------- 事件流（多消费者扇出）

    def events(self, since: str | None = None) -> Iterator[SessionEvent]:
        q: queue.Queue = queue.Queue(maxsize=FANOUT_QUEUE_MAX)
        # 先补历史（同进程内的 replay；replayable_events=False，跨重启补不了，如实）
        if since is not None:
            try:
                start_seq = int(since)
            except ValueError:
                start_seq = 0
            with self._history_lock:
                backlog = [e for e in self._history if int(e.cursor or "0") > start_seq]
            for e in backlog:
                yield e
        with self._consumers_lock:
            self._consumers.append(q)
        try:
            while not self._stop.is_set():
                try:
                    item = q.get(timeout=0.5)
                except queue.Empty:
                    continue
                if item is None:
                    break
                yield item
        finally:
            with self._consumers_lock:
                if q in self._consumers:
                    self._consumers.remove(q)

    # ---------------------------------------------------------------- 人机通道

    def interrupt(self) -> bool:
        """control_request interrupt。可观测副作用: result 帧 subtype=error_during_execution。"""
        with self._active_lock:
            active = self._active
        if active is None:
            return False
        return self.interrupt_delivery(active.envelope_id, active.turn_id)

    def interrupt_delivery(self, envelope_id: str, turn_id: str) -> bool:
        """Fail closed unless the serial pipe is executing this exact delivery/turn."""
        with self._active_lock:
            active = self._active
            if active is None or active.envelope_id != envelope_id or active.turn_id != turn_id:
                return False
        frame = {"type": "control_request", "request": {"subtype": "interrupt"}}
        try:
            with self._write_lock:
                assert self._proc is not None and self._proc.stdin is not None
                self._proc.stdin.write(json.dumps(frame) + "\n")
                self._proc.stdin.flush()
        except Exception as exc:
            log.warning("interrupt write failed: %s", exc)
            return False
        # 取证：等 result 帧 subtype=error_during_execution（被打断），不以"没报错"取证
        deadline = time.monotonic() + 10.0
        while time.monotonic() < deadline:
            if active.interrupted:
                return True
            if active.terminal_seen.is_set() and not active.interrupted:
                # turn 正常结束了（打断太晚）⇒ 保守返回 False，不谎报
                return False
            time.sleep(0.2)
        return False

    def human_input(self, data: bytes) -> None:
        # 人的输入走这里；外部消息走 deliver()，不碰这条路（P2 的解法）。
        if not data:
            return
        try:
            with self._write_lock:
                assert self._proc is not None and self._proc.stdin is not None
                self._proc.stdin.write(data.decode("utf-8", errors="replace"))
                self._proc.stdin.flush()
        except Exception as exc:
            log.warning("human_input write failed: %s", exc)

    def resize(self, rows: int, cols: int) -> None:
        # 管道无终端尺寸概念，no-op。
        return

    # ---------------------------------------------------------------- 内部：常驻读线程

    def _read_loop(self) -> None:
        assert self._proc is not None and self._proc.stdout is not None
        for line in self._proc.stdout:
            if self._stop.is_set():
                break
            line = line.strip()
            if not line:
                continue
            try:
                d = json.loads(line)
            except Exception:
                continue
            self._handle_frame(d)
        # stdout 结束 ⇒ 进程退出。唤醒消费者。
        with self._consumers_lock:
            for q in self._consumers:
                try:
                    q.put_nowait(None)
                except Exception:
                    pass

    def _handle_frame(self, d: dict) -> None:
        t = d.get("type")
        with self._active_lock:
            active = self._active
        turn_id = active.turn_id if active else ""
        delivery_id = active.envelope_id if active else ""

        if t == "system" and d.get("subtype") == "init":
            self._session_id = d.get("session_id", self._session_id)
            return

        if t == "user" and d.get("isReplay"):
            # 送达 ACK：claude 回吐了我们的 user 帧
            if active is not None and not active.accepted.is_set():
                active.accepted.set()
            return

        if t == "assistant":
            for c in d.get("message", {}).get("content", []):
                if c.get("type") == "text" and c.get("text", "").strip():
                    self._emit(SessionEvent(
                        kind="assistant_text", text=c["text"], data={},
                        session_id=self._session_id, delivery_id=delivery_id, turn_id=turn_id,
                    ))
                elif c.get("type") == "tool_use":
                    self._emit(SessionEvent(
                        kind="tool_call", text="", data={"name": c.get("name"), "input": c.get("input")},
                        session_id=self._session_id, delivery_id=delivery_id, turn_id=turn_id,
                    ))
            return

        if t == "result":
            # 终结事件：这一轮结束。subtype=error_during_execution ⇒ 被打断。
            interrupted = d.get("subtype") == "error_during_execution" or (
                d.get("is_error") and "interrupt" in json.dumps(d).lower()
            )
            self._emit(SessionEvent(
                kind="state_change", text="", data={"change": "turn_completed", "subtype": d.get("subtype"),
                                                     "is_error": d.get("is_error"), "num_turns": d.get("num_turns"),
                                                     "cost_usd": d.get("total_cost_usd")},
                session_id=self._session_id, delivery_id=delivery_id, turn_id=turn_id,
            ))
            if active is not None:
                if interrupted:
                    active.interrupted = True
                    active.state = "failed"
                    self._store.update_state(active.envelope_id, "failed", "interrupted (error_during_execution)")
                else:
                    active.state = "done"
                    self._store.update_state(active.envelope_id, "done", "result frame")
                active.terminal_seen.set()   # 终结事件已发出 ⇒ done 可以返回了（契约 v3）
            return

        if t == "control_response":
            return

    def _emit(self, ev: SessionEvent) -> None:
        self._event_seq += 1
        ev.cursor = str(self._event_seq)
        with self._history_lock:
            self._history.append(ev)
            if len(self._history) > 5000:
                self._history = self._history[-4000:]
        with self._consumers_lock:
            dead = []
            for q in self._consumers:
                try:
                    q.put_nowait(ev)
                except queue.Full:
                    dead.append(q)   # 慢消费者：不阻塞摄取（诚实的背压，宁丢慢消费者不卡全局）
            # 慢消费者留给它自己超时；这里不移除，避免误杀
            _ = dead

    # ---------------------------------------------------------------- helpers

    @staticmethod
    def _coerce_state(s: str) -> ReceiptState:
        if s in ("accepted", "queued", "processing", "done", "failed", "unknown"):
            return s  # type: ignore[return-value]
        return "unknown"
