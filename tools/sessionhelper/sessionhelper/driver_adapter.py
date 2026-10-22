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
import weakref

from .contract import SessionEvent
from .unified_queue import EnqueueResult, QueuedCommand, UnifiedCommandQueue
from .config import env_get

# R-V2-0105 件二：驱动重连退避（秒）与事件桥空转退避上限。
DRIVER_RECONNECT_BACKOFF = (1.0, 2.0, 5.0, 10.0, 30.0)
DRIVER_FAILURE_LOG_INTERVAL = 600.0  # 持续重连失败期间至多每 10 分钟一行
DRIVER_HEALTHY_RESET = 60.0          # 持续健康 60 s 复位退避


class DriverAdapter:
    def __init__(self, cfg):
        self.cfg = cfg
        which = (env_get("SH_DRIVER") or "codex").strip().lower()
        self._which = which
        self.driver = self._build_driver(which)
        self._term_q: queue.Queue[str] = queue.Queue(maxsize=4000)
        self._event_q: queue.Queue[str] = queue.Queue(maxsize=4000)  # C5:结构化 session_event(JSON)
        self._started = False
        self._start_lock = threading.Lock()
        self._commands = UnifiedCommandQueue(cfg.busy_buffer_max)
        self._blocker_store=None
        self.blocker_prepare=None
        self._work_available = threading.Event()
        self._turn_progress: dict[tuple[str, str], float] = {}
        self._admission_callbacks: dict[str, tuple[object | None, object | None]] = {}
        self._timed_out_turns: set[tuple[str, str]] = set()
        # R-V2-0105 件二：事件桥已处理（已入 _event_q，或有意不发）的事件对象，按 id 记、值为对象本身。
        # 驱动无游标重订阅时会把缓冲里的【同一批对象】整份重放；见过的直接跳过，不再发给 Hub。
        # SessionEvent 是可比较的非冻结 dataclass、不可哈希，故不用 WeakSet；判定须 get(id(ev)) is ev 防 id 复用。
        # 对象被驱动缓冲截掉并回收后条目自动消失，不膨胀。队列满被丢的事件不登记，重放时可补发。
        self._bridged: "weakref.WeakValueDictionary[int, SessionEvent]" = weakref.WeakValueDictionary()
        # R-V2-0105 件二（组长 BRIDGE-RECONNECT）：桥的睡眠可替换（用例用快睡眠）；驱动不健康时按退避重连。
        # 可中断睡眠：驱动重连成功／发出新事件时经 on_activity 唤醒（RECONNECT-MUSTFIX 必须 2）；用例可替换。
        self._bridge_wake = threading.Event()
        self._bridge_sleep = self._bridge_wait
        self._bridge_clock = time.monotonic
        try:
            self.driver.on_activity = self._bridge_wake.set
        except Exception:
            pass
        self._interrupt_confirmed: set[tuple[str, str]] = set()
        self._terminal_timeout = max(0.1, float(cfg.turn_terminal_timeout_seconds))
        self._input_buf = ""          # view 输入的行缓冲
        self._cost_total = 0.0        # 会话累计成本(状态栏用)
        self._cost_last_turn = 0.0    # 上一轮本次成本(=累计差)
        self._turns = 0

    def _build_driver(self, which: str):
        # 按机器取路径,别写死 pilot 开发机路径(否则每台都挂):
        cwd = env_get("SH_CLI_CWD") or os.path.expanduser("~")
        store = (os.path.join(self.cfg.state_dir, f"{which}-ledger.sqlite3") if getattr(self.cfg, "state_dir", "")
                 else os.path.join(os.path.expanduser("~"), f".dw-{which}-ledger.sqlite3"))
        model = getattr(self.cfg, "model", "")
        if which == "claude":
            from .claudeDriver import ClaudeProtocolDriver, DEFAULT_CMD
            cmd = [*DEFAULT_CMD, "--model", model] if model else None
            return ClaudeProtocolDriver(store_path=store, cwd=cwd, cmd=cmd)
        if which == "opencode":
            from .openCodeDriver import OpenCodeProtocolDriver
            pw = env_get("OPENCODE_SERVER_PASSWORD", "pilotpw")
            port = int(env_get("SH_OPENCODE_PORT", "19300"))
            return OpenCodeProtocolDriver(password=pw, port=port, workdir=cwd,
                                          model=model or env_get("SH_OPENCODE_MODEL", "deepseek-anthropic/deepseek-v4-pro"),
                                          store_path=store)
        # 默认 codex —— sandbox/approval 给足权限(对齐 claude 的 --dangerously-skip-permissions 意图),可用 env 收紧
        from .codexDriver import CodexProtocolDriver
        return CodexProtocolDriver(store_path=store, workdir=cwd, model=model or None,
                                   sandbox=env_get("SH_CODEX_SANDBOX", "danger-full-access"),
                                   approval_policy=env_get("SH_CODEX_APPROVAL", "never"),
                                   view_item_backfill=(env_get("SH_VIEW_ITEM_BACKFILL", "") == "item-backfill/v1"))

    def agent_metrics_workdir(self):
        from .agent_disk_metrics import process_workdir
        if not self._started:
            return None
        if self._which == "codex":
            if getattr(self.driver, "_transport_mode", None) == "daemon_ws":
                # Managed local daemon threads are started with this cwd. The
                # sampler validates the directory with O_NOFOLLOW/fstat before
                # publishing; never substitute the helper's own cwd.
                if getattr(self.driver, "_manage_daemon", False) is not True:
                    return None
                workdir = getattr(self.driver, "_workdir", None)
                return workdir if isinstance(workdir, str) and os.path.isabs(workdir) else None
            rpc = getattr(self.driver, "_rpc", None)
            proc = getattr(rpc, "_proc", None)
        elif self._which in ("claude", "opencode"):
            proc = getattr(self.driver, "_proc", None)
        else:
            return None
        if proc is None:
            return None
        return process_workdir(proc.pid, lambda: proc.poll() is None)

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
        port = env_get("SH_SIDECAR_PORT")
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
        return self.handle_delivery(env, None)

    def handle_delivery(self, env: dict, on_injected=None, on_admitted=None, on_admission_failed=None) -> str:
        body = str(env.get("body") or "")
        if not body:
            return ""
        if not self._started:
            self.start()
        if (env.get("meta") or {}).get("kind")=="task_blocker_ready":
            staged=self._blocker_journal().stage(env)
            env=json.loads(staged['envelope_json'])
            if staged['state'] in ('done','failed','cancelled','unknown'):
                if on_injected is not None:on_injected()
                return ""
        # Register admission callbacks before waking the worker.  Calling the
        # public enqueue_envelope here would set _work_available first, allowing
        # a fast fake/local driver to admit before the callback is installed.
        result = self._commands.enqueue(env)
        self._publish_queue_event(result.event)
        if result.state == "rejected":
            if on_admission_failed is not None:
                on_admission_failed()
            raise OverflowError(result.reason or "queue_full")
        if result.state == "duplicate":
            # Preserve main's ACK semantics: a Hub redelivery may mean the
            # previous injected ACK was lost, so acknowledge the duplicate
            # again even though it creates no new model admission.
            if on_injected is not None:
                on_injected()
            if on_admission_failed is not None:
                on_admission_failed()
            return ""
        if result.state == "queued" and (on_admitted is not None or on_admission_failed is not None):
            self._admission_callbacks[result.command.delivery_id] = (on_admitted, on_admission_failed)
        if result.state == "queued":
            self._work_available.set()
            if on_injected is not None:
                on_injected()
        return ""

    def enqueue_envelope(self, env: dict) -> EnqueueResult:
        if not self._started:
            try:
                self.start()
            except Exception as exc:
                # R-V2-HOSTED-DRIVER-IMPL-AMEND 异议 2：driver 起不来（如 codex app-server 启动失败）
                # 时投递必须以带原因的 failed 结束——不静默挂起、也不把异常甩给调用方。
                print(f"[driver-adapter][FAILED] driver start: {exc}", flush=True)
                result = self._commands.enqueue(env)
                self._publish_queue_event(result.event)
                cmd = self._commands.start_next()
                if cmd is not None:
                    self._publish_terminal_failure(cmd, "driver_start_failed", str(exc))
                    self._fail_admission(cmd.delivery_id)
                    self._commands.finish(cmd.delivery_id, "failed")
                return result
        result = self._commands.enqueue(env)
        self._publish_queue_event(result.event)
        if result.state == "queued":
            self._work_available.set()
        return result

    def enqueue_human(self, text: str) -> EnqueueResult:
        env = {"body": text, "meta": {"source_kind": "user"}}
        if not self._started:
            try:
                self.start()
            except Exception as exc:
                # 同 enqueue_envelope 的失败面（异议 2）：带原因 failed 结束，不甩异常
                print(f"[driver-adapter][FAILED] driver start: {exc}", flush=True)
                result = self._commands.enqueue(env, source="user")
                self._publish_queue_event(result.event)
                cmd = self._commands.start_next()
                if cmd is not None:
                    self._publish_terminal_failure(cmd, "driver_start_failed", str(exc))
                    self._fail_admission(cmd.delivery_id)
                    self._commands.finish(cmd.delivery_id, "failed")
                return result
        result = self._commands.enqueue(env, source="user")
        self._publish_queue_event(result.event)
        if result.state == "queued":
            self._work_available.set()
        return result

    def _command_worker(self) -> None:
        while True:
            try:self._restore_blocker_commands()
            except Exception:
                if not getattr(self,'_blocker_restore_warned',False):
                    print('[blocker-wake] recovery unavailable; no new wake RPC authorized',flush=True)
                    self._blocker_restore_warned=True
            command = self._commands.start_next()
            if command is None:
                self._work_available.clear()
                if self._commands.pending:
                    self._work_available.set()
                    continue
                self._work_available.wait(0.5)
                continue
            self._deliver_command(command)

    def _blocker_path(self):
        return getattr(self.cfg,'inbox_db','') or os.path.join(getattr(self.cfg,'state_dir','') or os.path.expanduser('~'),'.blocker-wake.sqlite3')

    def _blocker_journal(self):
        if getattr(self,'_blocker_store',None) is None:
            from .blocker_journal import BlockerJournal
            self._blocker_store=BlockerJournal(self._blocker_path(),getattr(self.cfg,'session_name','default'))
        return self._blocker_store

    def _restore_blocker_commands(self):
        from .blocker_journal import BlockerJournal
        if not hasattr(self,'cfg'):return
        if getattr(self,'_blocker_store',None) is None and not BlockerJournal.exists(self._blocker_path()):return
        for env in self._blocker_journal().recover():
            result=self._commands.enqueue(env)
            if result.state=='queued':self._publish_queue_event(result.event)

    def _deliver_command(self, command: QueuedCommand) -> None:
        if (command.envelope.get('meta') or {}).get('kind')!='task_blocker_ready':
            return self._deliver_regular_command(command)
        journal=None;signal=(command.envelope.get('meta') or {}).get('signal_id')
        try:
            journal=self._blocker_journal()
            # Only an unattempted command may make a delivery RPC. Recovered
            # dispatching/admitted rows query durable protocol receipts instead.
            current=journal.stage(command.envelope)['state']
            if current=='pending':
                if self.blocker_prepare is None:
                    journal.finish(signal,'unknown','preflight_unavailable');self._commands.finish(command.delivery_id,'failed');return
                safe,reason=self.blocker_prepare(command.envelope)
                if safe is None:
                    if reason=='current_authorization_or_read_failed':
                        self._commands.defer_current(command.delivery_id);self._bridge_sleep(0.5);return
                    journal.finish(signal,'cancelled',reason);self._commands.finish(command.delivery_id,'interrupted');return
                command.envelope=safe;command.text=safe['body']
            previous=journal.start(signal)
            if previous=='pending':
                receipt=self.driver.deliver(command.delivery_id,command.text)
            elif previous in ('dispatching','admitted'):
                receipt=self.driver.poll_receipt(command.delivery_id)
            else:
                self._commands.finish(command.delivery_id,'completed');return
            if receipt.state not in ('accepted','processing','done','failed'):
                journal.finish(signal,'unknown','protocol_receipt_unknown_no_replay');self._commands.finish(command.delivery_id,'failed');self._publish_terminal_failure(command,'blocker_wake_outcome_unknown','Inspect receipt; do not retry the execution');return
            journal.finish(signal,'admitted','protocol_receipt_observed')
            self._settle_command(command,receipt)
            journal.finish(signal,'done' if command.state=='completed' else 'failed',command.state)
        except Exception:
            # A request may already have crossed the protocol boundary. Never
            # turn an exception or a process restart into a second delivery RPC.
            if journal is not None:
                try:journal.finish(signal,'unknown','dispatch_exception_no_replay')
                except Exception:pass
            self._commands.finish(command.delivery_id,'failed')
            self._publish_terminal_failure(command,'blocker_wake_outcome_unknown','Inspect receipt; do not retry the execution')

    def _deliver_regular_command(self, command: QueuedCommand) -> None:
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
            self._fail_admission(command.delivery_id)
            self._commands.finish(command.delivery_id, "failed")

    def _settle_command(self, command: QueuedCommand, receipt) -> None:
        admitted = False
        while True:
            if not admitted:
                admitted = self._admit_from_receipt(command, receipt)
            if receipt.state in ("done", "failed"):
                if not admitted:
                    self._fail_admission(command.delivery_id)
                if receipt.state == "failed" and not command.turn_id:
                    # turn 未建立的同步失败（turn/start 鉴权错等）：driver 不会发 turn/completed
                    # 通知 ⇒ 事件面补发终态（带原因），否则业主在 view 看不到（复验确认的缺口）。
                    # turn 已建立的失败由 driver 通知驱动事件面，不重复发。
                    reason = "turn_failed"
                    for admission_reason in ("session_unavailable", "model_admission_unknown"):
                        if str(receipt.detail).startswith(admission_reason + ":"):
                            reason = admission_reason
                            break
                    self._publish_terminal_failure(command, reason, str(receipt.detail))
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
        callback, _ = self._admission_callbacks.pop(command.delivery_id, (None, None))
        if callable(callback):
            callback()
        return True

    def _fail_admission(self, delivery_id: str) -> None:
        _, callback = self._admission_callbacks.pop(delivery_id, (None, None))
        if callable(callback):
            callback()

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
            **({"origin_delivery_id": command.origin_delivery_id} if command.origin_delivery_id else {}),
        })

    # ------------------------------------------------------------ 事件→文本桥(喂老 view 页)

    def _bridge_wait(self, seconds: float) -> None:
        self._bridge_wake.wait(seconds)
        self._bridge_wake.clear()

    def _event_bridge(self) -> None:
        # 必须能重订阅:events() 在 driver 还没建 session 时会立刻返回空迭代器
        # (opencode 是懒建 session —— 第一条消息到了才建)。事件桥是 start() 后马上起的,
        # 只订阅一次的话 for 循环当场跑完、线程静静退出,此后整个 helper 生命周期内一个事件都不发,
        # view 页永远空白且无任何报错。见 2026-07-14 reviewer view 事件流为空。
        #
        # R-V2-0105 件二（组长 RECONNECT-MUSTFIX／P2-RECONNECT-GAP／REVIEW-ADD）：
        # - 驱动提供 healthy() 且报不健康 ⇒ 按 1、2、5、10、30 s 退避调 driver.start() 重连。
        #   start() 在本线程同步执行（可能起 daemon，耗时不定），期间桥不消费，驱动缓冲累积（10000 封顶）不丢；
        #   start() 抛异常不退出本线程。重连只重建 rpc，不 resume Codex 线程——线程到下一次 deliver 才恢复，
        #   故重连成功到下一条投递之间没有新事件属预期（不是「重连即恢复事件流」）。
        # - 退避只作用于上述不健康路径；健康或无探测的驱动（claude／opencode）重订阅间隔保持 1 s。
        # - 睡眠可被唤醒（重连成功、deliver 内被动重连、新事件发出）。
        # - 复位：重连成功、转发了新事件、或持续健康 60 s；复位后下一次故障从 1 s 起并重新打 WARN。
        # - 日志只在状态变化时打（转不可用一行、恢复一行），持续失败期间至多每 10 分钟一行带累计次数。
        subscriptions = 0
        reconnect_attempt = 0
        reconnect_failures = 0
        last_failure_log = 0.0
        healthy_since = None
        while True:
            subscriptions += 1
            skipped = 0
            forwarded = 0
            try:
                for ev in self.driver.events():
                    if ev is None:
                        continue
                    if self._bridged.get(id(ev)) is ev:
                        skipped += 1  # 重订阅重放的已处理对象：不再发（R-V2-0105 件二）
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
                        self._bridged[id(ev)] = ev  # 有意不发，也算已处理（重放时不再重复记日志）
                        continue
                    is_backfill = ev.data.get("backfill") is True
                    if ev.delivery_id and ev.turn_id and not is_backfill:
                        self._turn_progress[event_key] = time.monotonic()
                    # C5:同一条事件既喂老 view(文本)也喂新 View Service(结构化 session_event)
                    try:
                        self._event_q.put_nowait(self._event_json(ev))
                    except queue.Full:
                        pass  # 未入队 ⇒ 不登记，重订阅重放时可补发
                    else:
                        self._bridged[id(ev)] = ev
                        self._track_cost(ev)  # 入队成功后才记成本：被丢后重放补发的事件不重复记（复核 P23-REVIEW-FIX 二 3）
                        forwarded += 1
                    text = self._format_event(ev)
                    if text:
                        try:
                            self._term_q.put_nowait(text)
                        except queue.Full:
                            pass
            except Exception as exc:
                print(f"[driver-adapter] event bridge error: {exc}", flush=True)
            if skipped:
                print(f"[driver-adapter] bridge resubscribe skipped={skipped} subscription={subscriptions}", flush=True)
            if forwarded:
                reconnect_attempt = 0
            probe = getattr(self.driver, "healthy", None)
            if callable(probe) and not probe():
                healthy_since = None
                if reconnect_attempt == 0:
                    print("[driver-adapter][WARN] driver unavailable (app-server connection lost); "
                          "deliveries fail until reconnect", flush=True)
                    reconnect_failures = 0
                    last_failure_log = self._bridge_clock()
                delay = DRIVER_RECONNECT_BACKOFF[min(reconnect_attempt, len(DRIVER_RECONNECT_BACKOFF) - 1)]
                self._bridge_sleep(delay)
                reconnect_attempt += 1
                error = ""
                try:
                    if not probe():  # 睡眠期间可能已被 deliver 被动重连
                        self.driver.start()
                    ok = probe()
                except Exception as exc:
                    ok, error = False, type(exc).__name__
                if ok:
                    print(f"[driver-adapter] driver reconnected attempt={reconnect_attempt}", flush=True)
                    reconnect_attempt = 0
                    continue
                reconnect_failures += 1
                now = self._bridge_clock()
                if now - last_failure_log >= DRIVER_FAILURE_LOG_INTERVAL:
                    print(f"[driver-adapter] driver still unavailable failures={reconnect_failures}"
                          + (f" last_error={error}" if error else ""), flush=True)
                    last_failure_log = now
                continue
            if callable(probe):
                now = self._bridge_clock()
                if healthy_since is None:
                    healthy_since = now
                elif now - healthy_since >= DRIVER_HEALTHY_RESET:
                    reconnect_attempt = 0
            self._bridge_sleep(1.0)  # 迭代器耗尽(通常=还没建 session)→ 稍后重订阅

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
            # A completed item replacement is for the structured View only;
            # appending it to the legacy terminal stream would duplicate text.
            if ev.data.get("backfill") is True:
                return ""
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
        # ts=UTC epoch 毫秒,渲染端按查看者本地时区显示(mcs-demo 同款先例);
        # 打在事件体内(而非仅信封 TS),eventRing 回放才不丢。
        event = {
            "kind": ev.kind, "text": ev.text, "data": ev.data,
            "session_id": ev.session_id, "cursor": ev.cursor,
            "delivery_id": ev.delivery_id, "turn_id": ev.turn_id,
            "ts": int(time.time() * 1000),
        }
        origin = self._commands.origin_delivery_id(ev.delivery_id)
        if origin:
            event["origin_delivery_id"] = origin
        return json.dumps(event, ensure_ascii=False)

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
