"""DriverAdapter —— 把 transport-v2 的协议 Driver 桥接成 helper 的 Adapter 接口。

helper 本来就有 Adapter 抽象(handle/is_idle/start/next_terminal_chunk…),
CLIAdapter 用 PTY 注入实现它。本适配器用协议 Driver 实现同一接口:
- handle(env)  → driver.deliver()（幂等,不再重打3次）
- 事件→文本桥 → driver.events() 渲染成文本喂 next_terminal_chunk(老 view 页照看)

选哪个 driver:环境变量 SH_DRIVER = codex | claude | opencode(默认 codex)。
本目录是协议 Driver 与 SessionHelper 集成的单一版本化真源。
"""
from __future__ import annotations

import json
import os
import queue
import threading
import time

from .contract import SessionEvent
from .unified_queue import EnqueueResult, QueuedCommand, UnifiedCommandQueue


class DriverAdapter:
    def __init__(self, cfg):
        self.cfg = cfg
        which = (os.environ.get("SH_DRIVER") or "codex").strip().lower()
        self._which = which
        self.driver = self._build_driver(which)
        self._term_q: queue.Queue[str] = queue.Queue(maxsize=4000)
        self._event_q: queue.Queue[str] = queue.Queue(maxsize=4000)  # C5:结构化 session_event(JSON)
        self._started = False
        self._start_lock = threading.Lock()
        self._commands = UnifiedCommandQueue(cfg.busy_buffer_max)
        self._work_available = threading.Event()
        self._turn_progress: dict[tuple[str, str], float] = {}
        self._timed_out_turns: set[tuple[str, str]] = set()
        self._interrupt_confirmed: set[tuple[str, str]] = set()
        self._terminal_timeout = max(0.1, float(cfg.turn_terminal_timeout_seconds))
        self._input_buf = ""          # view 输入的行缓冲
        self._cost_total = 0.0        # 会话累计成本(状态栏用)
        self._cost_last_turn = 0.0    # 上一轮本次成本(=累计差)
        self._turns = 0

    def _build_driver(self, which: str):
        # 按机器取路径,别写死 pilot 开发机路径(否则每台都挂):
        cwd = os.environ.get("SH_CLI_CWD") or os.path.expanduser("~")
        store = os.path.join(os.path.expanduser("~"), f".dw-{which}-ledger.sqlite3")
        if which == "claude":
            from .claudeDriver import ClaudeProtocolDriver
            return ClaudeProtocolDriver(store_path=store, cwd=cwd)
        if which == "opencode":
            from .openCodeDriver import OpenCodeProtocolDriver
            pw = os.environ.get("OPENCODE_SERVER_PASSWORD", "pilotpw")
            port = int(os.environ.get("SH_OPENCODE_PORT", "19300"))
            return OpenCodeProtocolDriver(password=pw, port=port, workdir=cwd,
                                          model=os.environ.get("SH_OPENCODE_MODEL", "deepseek-anthropic/deepseek-v4-pro"),
                                          store_path=store)
        # 默认 codex —— sandbox/approval 给足权限(对齐 claude 的 --dangerously-skip-permissions 意图),可用 env 收紧
        from .codexDriver import CodexProtocolDriver
        return CodexProtocolDriver(store_path=store, workdir=cwd,
                                   sandbox=os.environ.get("SH_CODEX_SANDBOX", "danger-full-access"),
                                   approval_policy=os.environ.get("SH_CODEX_APPROVAL", "never"))

    # ------------------------------------------------------------ 生命周期

    def start(self) -> None:
        with self._start_lock:
            if self._started:
                return
            self.driver.start()
            threading.Thread(target=self._event_bridge, daemon=True).start()
            threading.Thread(target=self._command_worker, daemon=True).start()
            self._maybe_start_sidecar()
            self._started = True
            print(f"[driver-adapter] started, driver={self._which}", flush=True)

    def _maybe_start_sidecar(self) -> None:
        port = os.environ.get("SH_SIDECAR_PORT")
        if not port:
            return
        try:
            from .sidecar import start_sidecar
            start_sidecar(self, int(port))
        except Exception as exc:
            print(f"[driver-adapter] sidecar not started: {exc}", flush=True)

    # 状态栏用:会话标签 + 人类输入投递入口(sidecar 调)
    def session_label(self) -> str:
        try:
            return self.cfg.registered_session_name()
        except Exception:
            return getattr(self.cfg, "session_name", "session")

    def deliver_human_text(self, text: str) -> None:
        if not self._started:
            self.start()
        self.enqueue_human(text)

    # ------------------------------------------------------------ 投递(handle)

    def handle(self, env: dict) -> str:
        body = str(env.get("body") or "")
        if not body:
            return ""
        if not self._started:
            self.start()
        result = self.enqueue_envelope(env)
        if result.state == "rejected":
            raise OverflowError(result.reason or "queue_full")
        return ""

    def enqueue_envelope(self, env: dict) -> EnqueueResult:
        if not self._started:
            self.start()
        result = self._commands.enqueue(env)
        self._publish_queue_event(result.event)
        if result.state == "queued":
            self._work_available.set()
        return result

    def enqueue_human(self, text: str) -> EnqueueResult:
        env = {"body": text, "meta": {"source_kind": "user"}}
        result = self._commands.enqueue(env, source="user")
        self._publish_queue_event(result.event)
        if result.state == "queued":
            self._work_available.set()
        return result

    def _command_worker(self) -> None:
        while True:
            command = self._commands.start_next()
            if command is None:
                self._work_available.clear()
                if self._commands.pending:
                    self._work_available.set()
                    continue
                self._work_available.wait(0.5)
                continue
            self._deliver_command(command)

    def _deliver_command(self, command: QueuedCommand) -> None:
        try:
            receipt = self.driver.deliver(command.delivery_id, command.text)
            print(
                f"[driver-adapter] deliver id={command.delivery_id} -> "
                f"{receipt.state} ({str(receipt.detail)[:60]})",
                flush=True,
            )
            self._settle_command(command, receipt)
        except Exception as exc:
            print(f"[driver-adapter][FAILED] id={command.delivery_id} deliver raised: {exc}", flush=True)
            self._publish_terminal_failure(command, "deliver_failed", str(exc))
            self._commands.finish(command.delivery_id, "failed")

    def _settle_command(self, command: QueuedCommand, receipt) -> None:
        admitted = False
        while True:
            if not admitted:
                admitted = self._admit_from_receipt(command, receipt)
            if receipt.state in ("done", "failed"):
                key = (command.delivery_id, command.turn_id)
                outcome = (
                    "completed" if receipt.state == "done"
                    else "interrupted" if key in self._interrupt_confirmed
                    else "failed"
                )
                self._commands.finish(command.delivery_id, outcome)
                self._interrupt_confirmed.discard(key)
                self._turn_progress.pop((command.delivery_id, command.turn_id), None)
                print(
                    f"[delivery] {receipt.state} id={command.delivery_id} "
                    f"source={receipt.source} ({str(receipt.detail)[:60]})",
                    flush=True,
                )
                return
            if admitted:
                key = (command.delivery_id, command.turn_id)
                last_progress = self._turn_progress.get(key, time.monotonic())
                if time.monotonic() - last_progress >= self._terminal_timeout:
                    self._best_effort_timeout_interrupt(command)
                    self._publish_terminal_failure(command, "terminal_timeout", "driver emitted no terminal event")
                    self._timed_out_turns.add(key)
                    self._commands.finish(command.delivery_id, "failed")
                    self._turn_progress.pop(key, None)
                    print(f"[delivery][TIMEOUT] id={command.delivery_id} turn={command.turn_id}", flush=True)
                    return
            time.sleep(0.1)
            receipt = self.driver.poll_receipt(command.delivery_id)

    def _best_effort_timeout_interrupt(self, command: QueuedCommand) -> None:
        """Bounded cleanup attempt; watchdog progress never depends on driver cooperation."""
        fn = getattr(self.driver, "interrupt_delivery", None)
        if not callable(fn) or not command.turn_id:
            return
        done = threading.Event()

        def run() -> None:
            try:
                fn(command.delivery_id, command.turn_id)
            except Exception:
                pass
            finally:
                done.set()

        threading.Thread(target=run, daemon=True, name="driver-timeout-interrupt").start()
        done.wait(min(2.0, self._terminal_timeout))

    @staticmethod
    def _receipt_turn_id(receipt) -> str:
        for field in str(receipt.detail or "").split():
            if field.startswith("turn="):
                return field.removeprefix("turn=").rstrip(",")
        return ""

    def _admit_from_receipt(self, command: QueuedCommand, receipt) -> bool:
        if receipt.state not in ("accepted", "processing", "done"):
            return False
        turn_id = self._receipt_turn_id(receipt)
        if not turn_id:
            return False
        event = self._commands.admit(command.delivery_id, turn_id)
        if event is None:
            return command.turn_id == turn_id and command.state == "admitted"
        self._turn_progress[(command.delivery_id, turn_id)] = time.monotonic()
        self._publish_queue_event(event)
        return True

    def _publish_queue_event(self, event: dict | None) -> None:
        if event is None:
            return
        try:
            self._event_q.put_nowait(json.dumps(event, ensure_ascii=False))
        except queue.Full:
            print("[driver-adapter][WARN] session event queue full", flush=True)

    def _publish_terminal_failure(self, command: QueuedCommand, reason: str, detail: str) -> None:
        self._publish_queue_event({
            "kind": "state_change",
            "text": "",
            "data": {"change": "turn_completed", "outcome": "failed", "reason": reason, "detail": detail},
            "session_id": "",
            "cursor": f"queue:{command.queue_seq:020d}:terminal",
            "delivery_id": command.delivery_id,
            "turn_id": command.turn_id,
        })

    # ------------------------------------------------------------ 事件→文本桥(喂老 view 页)

    def _event_bridge(self) -> None:
        # 必须能重订阅:events() 在 driver 还没建 session 时会立刻返回空迭代器
        # (opencode 是懒建 session —— 第一条消息到了才建)。事件桥是 start() 后马上起的,
        # 只订阅一次的话 for 循环当场跑完、线程静静退出,此后整个 helper 生命周期内一个事件都不发,
        # view 页永远空白且无任何报错。见 2026-07-14 reviewer view 事件流为空。
        while True:
            try:
                for ev in self.driver.events():
                    if ev is None:
                        continue
                    event_key = (ev.delivery_id, ev.turn_id)
                    if (
                        event_key in self._timed_out_turns
                        and ev.kind == "state_change"
                        and ev.data.get("change") == "turn_completed"
                    ):
                        print(
                            f"[delivery][LATE_TERMINAL] id={ev.delivery_id} turn={ev.turn_id}",
                            flush=True,
                        )
                        continue
                    if ev.delivery_id and ev.turn_id:
                        self._turn_progress[event_key] = time.monotonic()
                    self._track_cost(ev)
                    # C5:同一条事件既喂老 view(文本)也喂新 View Service(结构化 session_event)
                    try:
                        self._event_q.put_nowait(self._event_json(ev))
                    except queue.Full:
                        pass
                    text = self._format_event(ev)
                    if text:
                        try:
                            self._term_q.put_nowait(text)
                        except queue.Full:
                            pass
            except Exception as exc:
                print(f"[driver-adapter] event bridge error: {exc}", flush=True)
            time.sleep(1.0)  # 迭代器耗尽(通常=还没建 session)→ 稍后重订阅

    def _track_cost(self, ev: SessionEvent) -> None:
        # cost_usd = 会话累计(claude 用 total_cost_usd)。本次 = 累计差。
        if ev.kind != "state_change" or ev.data.get("change") != "turn_completed":
            return
        cost = ev.data.get("cost_usd")
        if isinstance(cost, (int, float)):
            self._cost_last_turn = max(0.0, cost - self._cost_total)
            self._cost_total = cost
        n = ev.data.get("num_turns")
        if isinstance(n, int):
            self._turns = n
        else:
            self._turns += 1

    def _format_event(self, ev: SessionEvent) -> str:
        if ev.kind == "assistant_text":
            # 终端要 \r\n（只 \n 会逐行右移成阶梯）
            return ev.text.replace("\r\n", "\n").replace("\n", "\r\n")
        if ev.kind == "tool_call":
            name = ev.data.get("name", "tool")
            inp = json.dumps(ev.data.get("input", {}), ensure_ascii=False)
            return f"\r\n\x1b[36m[tool] {name}\x1b[0m {inp[:100]}\r\n"
        if ev.kind == "state_change":
            ch = ev.data.get("change")
            if ch == "turn_completed":
                n = ev.data.get("num_turns", "?")
                cost = ev.data.get("cost_usd")
                tail = f" ${cost:.4f}" if isinstance(cost, (int, float)) else ""
                return f"\r\n\x1b[90m--- turn done ({n} turns{tail}) ---\x1b[0m\r\n"
            if ch == "session_init":
                return f"\x1b[90m[session {ev.data.get('session_id','')[:8]} · {ev.data.get('model','')}]\x1b[0m\r\n"
            return ""
        if ev.kind == "error":
            return f"\r\n\x1b[31m[error] {ev.text}\x1b[0m\r\n"
        return ""

    def next_terminal_chunk(self, timeout: float = 0.5):
        try:
            return self._term_q.get(timeout=timeout)
        except queue.Empty:
            return None

    def _event_json(self, ev: SessionEvent) -> str:
        return json.dumps({
            "kind": ev.kind, "text": ev.text, "data": ev.data,
            "session_id": ev.session_id, "cursor": ev.cursor,
            "delivery_id": ev.delivery_id, "turn_id": ev.turn_id,
        }, ensure_ascii=False)

    def next_session_event(self, timeout: float = 0.5):
        # C5:helper 用它拉结构化事件,发 session_event 信封给 hub(新 View Service 用)
        try:
            return self._event_q.get(timeout=timeout)
        except queue.Empty:
            return None

    def request_terminal_refresh(self) -> str:
        return ""      # 协议型无"重绘",返回空即可

    # ------------------------------------------------------------ 人机 & 忙闲

    def is_idle(self) -> bool:
        return self._commands.depth == 0

    def interrupt_current(self, delivery_id: str, turn_id: str) -> dict:
        """High-priority control path; queue owns the stale-turn CAS."""
        target = self._commands.interrupt_target(delivery_id, turn_id)
        if target is None:
            return {"ok": False, "state": "stale", "delivery_id": delivery_id, "turn_id": turn_id}
        fn = getattr(self.driver, "interrupt_delivery", None)
        if not callable(fn):
            return {"ok": False, "state": "unsupported", "delivery_id": delivery_id, "turn_id": turn_id}
        ok = bool(fn(delivery_id, turn_id))
        if ok:
            self._interrupt_confirmed.add((delivery_id, turn_id))
        return {
            "ok": ok,
            "state": "interrupt_requested" if ok else "unconfirmed",
            "delivery_id": delivery_id,
            "turn_id": turn_id,
        }

    def write_terminal_input(self, data: str) -> None:
        # 人在 view 里敲的键。协议 driver 不吃裸键(claude stdin 要 JSONL),
        # 所以行缓冲 + 回显,回车时把整行当一条新消息 deliver。
        if isinstance(data, bytes):
            data = data.decode("utf-8", errors="replace")
        for ch in data:
            if ch == "\r":
                line = self._input_buf.rstrip()
                self._input_buf = ""
                try:
                    self._term_q.put_nowait("\r\n")   # 回显换行
                except queue.Full:
                    pass
                if line:
                    self.enqueue_human(line)
            elif ch == "\n":                         # 多行输入:保留换行,最终 CR 才提交
                self._input_buf += ch
                try:
                    self._term_q.put_nowait("\r\n")
                except queue.Full:
                    pass
            elif ch in ("\x7f", "\b"):                 # 退格
                if self._input_buf:
                    self._input_buf = self._input_buf[:-1]
                    try:
                        self._term_q.put_nowait("\b \b")
                    except queue.Full:
                        pass
            elif ch == "\x03":                         # Ctrl-C → 中断当前轮
                fn = getattr(self.driver, "interrupt", None)
                if callable(fn):
                    threading.Thread(target=fn, daemon=True).start()
                try:
                    self._term_q.put_nowait("\r\n^C\r\n")
                except queue.Full:
                    pass
            elif ch >= " ":                            # 可见字符 → 回显
                self._input_buf += ch
                try:
                    self._term_q.put_nowait(ch)
                except queue.Full:
                    pass

    def _deliver_human(self, line: str) -> None:
        # 兼容旧调用点；不再创建自有 deliver 线程，统一进入同一个 FIFO。
        self.enqueue_human(line)

    def inject_text(self, text: str) -> None:
        # 兼容旧接口:等价于投一条(无 envelope 场景,少用)
        if not self._started:
            self.start()
        self.enqueue_envelope({"body": text, "from": "inject", "meta": {"source_kind": "agent"}})

    def set_winsize(self, rows: int, cols: int) -> None:
        fn = getattr(self.driver, "resize", None)
        if callable(fn):
            try:
                fn(rows, cols)
            except Exception:
                pass
