"""R-V2-0105 件二：事件桥重订阅不得把驱动缓冲里已发过的事件再发一遍（要点第七、九至十一、十四节）。

现场机理：codexDriver.events() 无游标时先把 _event_log 整份 yield，rpc 不健康即返回；
事件桥 1 秒后重订阅 ⇒ 整份再发，Hub 当新事件（dw2 洪流）。这里用真 CodexProtocolDriver 的缓冲与
events()，只把 rpc 换成「不健康」的桩，让桥真实地反复重订阅；另起线程持续取 _event_q 计数。
"""
import contextlib
import io
import json
import os
import queue
import threading
import time
import unittest
from collections import Counter
from unittest import mock

from sessionhelper.config import load_config
from sessionhelper.contract import SessionEvent
from sessionhelper.driver_adapter import DriverAdapter

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation

_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


BASE_ENV = {"SH_SESSION_NAME": "bridge", "SH_OWNER": "zzc", "SH_KEY_ID": "FB-2642", "SH_SECRET": "secret",
            "SH_PROVISION_NO_EXIT": "1", "SH_MODE": "driver"}


class UnhealthyRpc:
    def healthy(self):
        return False


class HealthyRpc:
    def __init__(self):
        self.alive = True

    def healthy(self):
        return self.alive

    def stop(self):
        self.alive = False


class _Parked:
    """测试结束后换上它：桥线程停在 events() 里，不再碰本测试的驱动。"""

    def events(self, since=None):
        time.sleep(3600)
        return iter(())


class BridgeHarness:
    def __init__(self, testcase, drain=True):
        patcher = mock.patch.dict(os.environ, {"SH_DRIVER": "codex"})
        patcher.start()
        testcase.addCleanup(patcher.stop)
        self.adapter = DriverAdapter(load_config(dict(BASE_ENV)))
        self.driver = self.adapter.driver
        self.driver._rpc = UnhealthyRpc()
        # 不起真 Codex：start() 换成记录器（默认重连失败＝仍不健康）；桥的睡眠换成快睡眠并记录请求时长。
        self.starts = 0
        self.reconnect_to = None
        def fake_start():
            self.starts += 1
            if self.reconnect_to is not None:
                self.driver._rpc = self.reconnect_to
        self.driver.start = fake_start
        self.sleeps = []
        self.adapter._bridge_sleep = lambda d: (self.sleeps.append(d), time.sleep(min(d, 0.02)))
        self.received = []
        self._drain = threading.Event()
        if drain:
            self._drain.set()
        self._stop = threading.Event()
        threading.Thread(target=self._drainer, daemon=True).start()
        self._bridge = None
        testcase.addCleanup(self.close)

    def _drainer(self):
        while not self._stop.is_set():
            if not self._drain.is_set():
                time.sleep(0.02)
                continue
            try:
                self.received.append(json.loads(self.adapter._event_q.get(timeout=0.05)))
            except queue.Empty:
                pass

    def start_draining(self):
        self._drain.set()

    def emit(self, n, prefix, cursor=None, text=None):
        for i in range(n):
            self.driver._emit_event(SessionEvent(kind="assistant_text", text=text if text is not None else f"{prefix}-{i}",
                                                 cursor=cursor or f"{prefix}:{i:06d}", delivery_id="d1", turn_id="t1"))

    def start_bridge(self):
        self._bridge = threading.Thread(target=self.adapter._event_bridge, daemon=True)
        self._bridge.start()

    def wait_for(self, predicate, timeout=20.0):
        deadline = time.time() + timeout
        while time.time() < deadline:
            if predicate():
                return True
            time.sleep(0.05)
        return predicate()

    def settle(self, resubscriptions=2):
        # 每轮：重放 → 0.5 s 取不到实时事件且 rpc 不健康即返回 → sleep 1 s → 重订阅。等足够多轮。
        time.sleep(1.6 * resubscriptions + 0.6)

    def close(self):
        self._stop.set()
        self.adapter.driver = _Parked()


class EventBridgeReplayTest(unittest.TestCase):
    def texts(self, h):
        return [ev["text"] for ev in h.received]

    def test_resubscribe_does_not_resend_buffered_events(self):
        h = BridgeHarness(self)
        h.emit(5, "first")  # 首次订阅前就在缓冲里（Bug1：晚订阅者不丢）⇒ 照常发出
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 5))
        h.settle(3)
        self.assertEqual(Counter(self.texts(h)), Counter(f"first-{i}" for i in range(5)),
                         "每条恰一次：首次照发、重订阅不重发")

    def test_new_events_between_resubscriptions_arrive_exactly_once(self):
        h = BridgeHarness(self)
        h.start_bridge()
        for round_ in range(3):
            h.emit(4, f"r{round_}")
            time.sleep(0.9)
        h.settle(2)
        expected = Counter(f"r{r}-{i}" for r in range(3) for i in range(4))
        self.assertEqual(Counter(self.texts(h)), expected)

    def test_equal_deltas_with_shared_cursor_are_not_swallowed(self):
        # Codex 同一 item 的多片 delta 共用 cursor，文本也可能相同：按内容或 cursor 判已见会吞正文。
        h = BridgeHarness(self)
        h.emit(3, "same", cursor="turn:t1:item:0003", text="的")
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 3))
        h.settle(2)
        self.assertEqual(self.texts(h), ["的", "的", "的"])

    def test_buffer_truncation_keeps_identity_and_never_resends(self):
        h = BridgeHarness(self)
        h.start_bridge()
        h.emit(6000, "a")
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 6000, timeout=60))
        h.emit(4001, "b")  # 第 10001 条触发截断：_event_log 只留后 5000（切片保留原对象）
        self.assertEqual(len(h.driver._event_log), 5000)
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 10001, timeout=60))
        h.settle(2)
        counts = Counter(self.texts(h))
        self.assertEqual(len(counts), 10001)
        self.assertEqual(set(counts.values()), {1}, "截断后重放仍跳过已发对象")

    def test_events_dropped_on_full_queue_are_sent_on_replay(self):
        h = BridgeHarness(self, drain=False)  # 先不取：_event_q（上限 4000）会满，多出的被桥丢弃
        h.emit(4005, "q")
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: h.adapter._event_q.full()))
        time.sleep(0.5)
        h.start_draining()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 4005, timeout=60),
                        f"队列满被丢的事件须在重订阅重放时补发，实收 {len(h.received)}")
        h.settle(2)
        counts = Counter(self.texts(h))
        self.assertEqual(len(counts), 4005)
        self.assertEqual(set(counts.values()), {1})

    def test_dead_driver_reconnects_with_backoff_and_never_resends(self):
        h = BridgeHarness(self)
        h.emit(3, "pre")
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: h.starts >= 6, timeout=30))
        backoff = [d for d in h.sleeps[:6]]
        self.assertEqual(backoff, [1.0, 2.0, 5.0, 10.0, 30.0, 30.0], "不健康期间按 1、2、5、10、30 s 退避重连")
        self.assertEqual(Counter(self.texts(h)), Counter(f"pre-{i}" for i in range(3)), "退避空转中不重发")

    def test_unavailable_warning_printed_once_per_outage(self):
        h = BridgeHarness(self)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            h.start_bridge()
            self.assertTrue(h.wait_for(lambda: h.starts >= 4, timeout=30))
            h.reconnect_to = HealthyRpc()
            self.assertTrue(h.wait_for(lambda: "driver reconnected" in out.getvalue(), timeout=30))
        text = out.getvalue()
        self.assertEqual(text.count("driver unavailable"), 1, text)
        self.assertEqual(text.count("driver reconnected"), 1, text)

    def test_events_resume_in_order_after_reconnect(self):
        h = BridgeHarness(self)
        h.emit(2, "old")
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 2 and h.starts >= 1))
        h.reconnect_to = HealthyRpc()  # 下一次重连成功
        self.assertTrue(h.wait_for(lambda: h.driver.healthy()))
        time.sleep(0.3)  # 桥进入新一轮订阅（健康时阻塞在实时队列上）
        h.emit(5, "new")
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 7, timeout=20), f"重连后新事件未续流: {self.texts(h)}")
        time.sleep(0.5)
        self.assertEqual(self.texts(h), ["old-0", "old-1"] + [f"new-{i}" for i in range(5)])

    def test_driver_without_health_probe_is_not_restarted(self):
        h = BridgeHarness(self)

        class NoProbeDriver:  # 形同 opencode 未建 session：events() 立即返回空，且不提供 healthy()
            def __init__(self):
                self.starts = 0

            def events(self, since=None):
                return iter(())

            def start(self):
                self.starts += 1

        fake = NoProbeDriver()
        h.adapter.driver = fake
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.sleeps) >= 8))
        self.assertEqual(fake.starts, 0)
        self.assertEqual(set(h.sleeps[:8]), {1.0}, "无探测驱动保持 1 s，不退避到 30 s")

    def test_healthy_driver_quiet_for_minutes_still_picks_up_new_events_within_1s(self):
        # (a) 健康却无事件即返回（如 claude 之外的探测驱动空闲）：不退避，重订阅间隔恒 1 s。
        h = BridgeHarness(self)
        pending = []

        class HealthyQuietDriver:
            def healthy(self):
                return True

            def events(self, since=None):
                while pending:
                    yield pending.pop(0)

            def start(self):
                raise AssertionError("healthy driver must not be restarted")

        h.adapter.driver = HealthyQuietDriver()
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.sleeps) >= 180, timeout=30))  # 约 3 分钟的空闲轮次
        pending.append(SessionEvent(kind="assistant_text", text="late", cursor="x", delivery_id="d", turn_id="t"))
        self.assertTrue(h.wait_for(lambda: any(e["text"] == "late" for e in h.received)))
        self.assertEqual(set(h.sleeps), {1.0}, f"healthy idle path backed off: {sorted(set(h.sleeps))}")

    def test_codex_start_stops_old_unhealthy_rpc_before_reconnecting(self):
        from sessionhelper.codexDriver import CodexProtocolDriver

        class DeadRpc:
            stops = 0

            def healthy(self):
                return False

            def stop(self):
                DeadRpc.stops += 1

        drv = CodexProtocolDriver.__new__(CodexProtocolDriver)
        drv._rpc = DeadRpc()
        drv._endpoint_mode = 'legacy'  # This fixture bypasses constructor defaults.
        drv._transport_mode = "__test_abort__"  # 停旧 rpc 之后立即因未知传输方式中止，不起真进程
        with self.assertRaises(ValueError):
            CodexProtocolDriver.start(drv)
        self.assertEqual(DeadRpc.stops, 1, "重连前须先停旧的不健康 rpc（防孤儿 app-server）")
        self.assertFalse(CodexProtocolDriver.healthy(drv))

    def test_reconnect_lock_builds_one_rpc_under_concurrent_start(self):
        from sessionhelper.codexDriver import CodexProtocolDriver

        class DeadRpc:
            stops = 0

            def healthy(self):
                return False

            def stop(self):
                DeadRpc.stops += 1

        drv = CodexProtocolDriver.__new__(CodexProtocolDriver)
        drv._rpc = DeadRpc()
        builds = []

        def fake_start_locked():
            time.sleep(0.2)  # 撑开竞态窗口
            builds.append(1)
            drv._rpc = HealthyRpc()

        drv._start_locked = fake_start_locked
        threads = [threading.Thread(target=drv.start) for _ in range(2)]
        for t in threads:
            t.start()
        for t in threads:
            t.join(5)
        self.assertEqual((len(builds), DeadRpc.stops), (1, 1), "两线程同时 start ⇒ 只建一个 rpc、旧 rpc 停一次")

    def test_sleep_is_woken_by_deliver_reconnect_and_resumes_within_1s(self):
        # (c) 桥在 30 s 重连退避中睡眠；deliver 内被动重连成功 ⇒ 唤醒，1 s 内续流。
        import sessionhelper.driver_adapter as da
        h = BridgeHarness(self)
        h.adapter._bridge_sleep = h.adapter._bridge_wait  # 用真可中断睡眠
        h.driver.start = type(h.driver).start.__get__(h.driver)  # 真 start（锁＋唤醒）
        drv = h.driver

        def fake_start_locked():
            drv._rpc = HealthyRpc()
            drv._stop_event.clear()

        drv._start_locked = fake_start_locked
        with mock.patch.object(da, "DRIVER_RECONNECT_BACKOFF", (30.0,)):
            h.start_bridge()
            time.sleep(1.5)  # 桥已进入 30 s 退避睡眠
            t0 = time.time()
            drv.start()      # 相当于 deliver() 发现不健康后的被动重连
            time.sleep(0.2)
            h.emit(2, "after")
            self.assertTrue(h.wait_for(lambda: len(h.received) >= 2, timeout=5))
            self.assertLess(time.time() - t0, 1.5, "deliver 重连后须 1 s 内续流（睡眠被唤醒）")

    def test_reset_after_reconnect_restarts_backoff_and_warns_again(self):
        # (d) 重连成功后复位：下一次故障从 1 s 起退避（第一个大于 1 s 的退避是 2 s），且重新打一行「转不可用」。
        h = BridgeHarness(self)
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            h.start_bridge()
            self.assertTrue(h.wait_for(lambda: h.starts >= 4))   # 已退避到 10 s 档
            h.reconnect_to = HealthyRpc()
            self.assertTrue(h.wait_for(lambda: "driver reconnected" in out.getvalue()))
            mark = len(h.sleeps)
            h.reconnect_to = None
            h.driver._rpc.alive = False  # 第二次故障
            self.assertTrue(h.wait_for(lambda: any(d > 1.0 for d in h.sleeps[mark:])))
            text = out.getvalue()
        first_backoff = next(d for d in h.sleeps[mark:] if d > 1.0)
        self.assertEqual(first_backoff, 2.0, f"backoff not reset after reconnect: {h.sleeps[mark:mark + 6]}")
        self.assertEqual(text.count("driver unavailable"), 2, "第二次故障须重新打一行 WARN")

    def test_persistent_failure_logs_at_most_every_10_minutes(self):
        h = BridgeHarness(self)
        clock = [0.0]
        h.adapter._bridge_clock = lambda: clock[0]

        def failing_start():
            h.starts += 1
            raise RuntimeError("app-server down")
        h.driver.start = failing_start
        out = io.StringIO()
        with contextlib.redirect_stdout(out):
            h.start_bridge()
            self.assertTrue(h.wait_for(lambda: h.starts >= 8))
            quiet = out.getvalue()
            clock[0] = 601.0
            n = h.starts
            self.assertTrue(h.wait_for(lambda: h.starts >= n + 2))
        self.assertEqual(quiet.count("driver unavailable"), 1)
        self.assertNotIn("still unavailable", quiet, "持续失败期间每次尝试都打日志")
        self.assertEqual(out.getvalue().count("still unavailable"), 1)
        self.assertIn("last_error=RuntimeError", out.getvalue())

    def test_stop_then_resubscribe_resends_nothing(self):
        h = BridgeHarness(self)
        drv = h.driver
        drv._rpc = HealthyRpc()

        def fake_start_locked():
            drv._rpc = HealthyRpc()
            drv._stop_event.clear()

        drv._start_locked = fake_start_locked
        h.driver.start = type(drv).start.__get__(drv)
        h.emit(3, "s")
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 3))
        drv.stop()   # 所有订阅结束一次、rpc 不健康
        drv.start()  # 重连（锁内重建）
        time.sleep(1.5)
        h.emit(2, "t")
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 5))
        time.sleep(0.5)
        self.assertEqual(Counter(self.texts(h)), Counter(["s-0", "s-1", "s-2", "t-0", "t-1"]))

    def test_cost_tracked_once_for_events_resent_after_queue_full(self):
        h = BridgeHarness(self, drain=False)
        h.emit(4000, "fill")
        for i in range(3):
            h.driver._emit_event(SessionEvent(kind="state_change", data={"change": "turn_completed", "cost_usd": 0.1 * (i + 1)},
                                              cursor=f"z{i}", delivery_id="d", turn_id=f"tc{i}"))
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: h.adapter._event_q.full()))
        time.sleep(0.5)
        h.start_draining()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 4003, timeout=60))
        h.settle(2)
        self.assertEqual(h.adapter._turns, 3, "被丢后重放补发的终态不得重复记成本／回合数")

    # ── R-V2-0105 REAL-EXIT-SAMECURSOR（组长 B）：先登记后读缓冲、按对象同一性去重 ────────────────
    def test_same_item_delta_after_resubscribe_is_delivered(self):
        # r5 真退出格：Codex 同一 item 的各片共用 cursor；重连重订阅后同 cursor 的新片必须恰到一次
        h = BridgeHarness(self)
        h.emit(3, "pre", cursor="turn:t1:item:0001")
        h.reconnect_to = HealthyRpc()
        h.start_bridge()
        self.assertTrue(h.wait_for(lambda: len(h.received) >= 3))
        self.assertTrue(h.wait_for(lambda: h.starts >= 1))
        h.driver._emit_event(SessionEvent(kind="assistant_text", text="after-reconnect", cursor="turn:t1:item:0001",
                                          delivery_id="d1", turn_id="t1"))
        self.assertTrue(h.wait_for(lambda: any(e["text"] == "after-reconnect" for e in h.received), timeout=5),
                        f"same-item delta after resubscribe was dropped: {[e['text'] for e in h.received]}")
        time.sleep(1.5)
        self.assertEqual([e["text"] for e in h.received].count("after-reconnect"), 1)

    @staticmethod
    def _next(gen, timeout=5.0):
        # 限时取下一条：被测实现若丢事件，生成器会在健康驱动上一直等——超时返回 None，判红不挂死
        box = {}
        def run():
            try:
                box["v"] = next(gen)
            except BaseException as exc:  # noqa: BLE001
                box["e"] = exc
        t = threading.Thread(target=run, daemon=True)
        t.start()
        t.join(timeout)
        if t.is_alive():
            return None
        if "e" in box:
            raise box["e"]
        return box["v"]

    @staticmethod
    def _ev(text, cursor="turn:t1:item:0001"):
        return SessionEvent(kind="assistant_text", text=text, cursor=cursor, delivery_id="d1", turn_id="t1")

    def _fresh_driver(self):
        h = BridgeHarness(self)
        drv = h.driver
        drv._rpc = HealthyRpc()
        return drv

    def _reset(self, drv):
        with drv._event_log_lock:
            drv._event_log = []
        with drv._consumers_lock:
            drv._consumers.clear()

    def _emit_from_thread(self, drv, events):
        # 另起线程经 _emit_event 发出：重放期间生成器持缓冲锁，发事件者在锁上等，放锁后才入缓冲与队列
        t = threading.Thread(target=lambda: [drv._emit_event(e) for e in events], daemon=True)
        t.start()
        return t

    class _HookLock:
        # 可控屏障：被测代码下一次 with 此锁时，先跑一次回调（只触发一次），再真正取锁
        def __init__(self, real):
            self._real, self._hook = real, None
        def arm(self, fn):
            self._hook = fn
        def __enter__(self):
            fn, self._hook = self._hook, None
            if fn:
                fn()
            return self._real.__enter__()
        def __exit__(self, *a):
            return self._real.__exit__(*a)

    def test_replay_order_then_live_in_order(self):
        drv = self._fresh_driver()
        buf = [self._ev(f"b{i}") for i in range(5)]
        for e in buf:
            drv._emit_event(e)
        gen = drv.events()
        first = self._next(gen)
        live = [self._ev(f"l{i}") for i in range(5)]
        self._emit_from_thread(drv, live)
        got = [getattr(first, "text", "<none>")] + [getattr(self._next(gen), "text", "<none>") for _ in range(9)]
        self.assertEqual(got, [e.text for e in buf] + [e.text for e in live])
        gen.close()

    def test_same_cursor_new_objects_new_text_and_same_text_each_once(self):
        drv = self._fresh_driver()
        old = self._ev("same")
        drv._emit_event(old)
        gen = drv.events()
        first = self._next(gen)                            # 已登记、正在重放：下面的走实时路径
        twin = self._ev("same")                            # 同 cursor 同文本的新对象
        newer = self._ev("newer")                          # 同 cursor 新文本
        marker = self._ev("marker", "turn:t1:item:0002")
        self._emit_from_thread(drv, [twin, newer, marker])
        got = [first] + [self._next(gen) for _ in range(3)]
        self.assertEqual([getattr(e, "text", "<none>") for e in got], ["same", "same", "newer", "marker"])
        self.assertIs(got[0], old)
        self.assertIs(got[1], twin)
        gen.close()

    def _boundary_cells(self, drv):
        # 屏障 a：生成器取消费者锁登记的那一刻注入（旧顺序下这正是快照之后、登记之前的空窗）
        self._reset(drv)
        b0 = self._ev("b0")
        drv._emit_event(b0)
        x = self._ev("x-at-register")
        hook = self._HookLock(drv._consumers_lock)
        drv._consumers_lock = hook
        try:
            hook.arm(lambda: drv._emit_event(x))
            gen = drv.events()
            marker = self._ev("marker-a", "turn:t1:item:0002")
            first = self._next(gen)
            self._emit_from_thread(drv, [marker])
            rest = [self._next(gen), self._next(gen)]
            got = [getattr(e, "text", "<none>") for e in [first] + rest]
            self.assertEqual(got, ["b0", "x-at-register", "marker-a"], f"barrier a (register): {got}")
            gen.close()
        finally:
            drv._consumers_lock = hook._real
        # 屏障 b：生成器取缓冲锁读快照的那一刻注入（登记之后、快照之前：既入缓冲又入队列，须只出一次）
        self._reset(drv)
        b0 = self._ev("b0")
        drv._emit_event(b0)
        y = self._ev("y-at-snapshot")
        hook = self._HookLock(drv._event_log_lock)
        drv._event_log_lock = hook
        try:
            gen = drv.events()
            hook.arm(lambda: (setattr(drv, "_event_log_lock", hook._real), drv._emit_event(y), setattr(drv, "_event_log_lock", hook)))
            marker = self._ev("marker-b", "turn:t1:item:0002")
            got_objs = [self._next(gen), self._next(gen)]
            self._emit_from_thread(drv, [marker])
            got_objs.append(self._next(gen))
            got = [getattr(e, "text", "<none>") for e in got_objs]
            self.assertEqual(got, ["b0", "y-at-snapshot", "marker-b"], f"barrier b (snapshot): {got}")
            gen.close()
        finally:
            drv._event_log_lock = hook._real

    def test_register_and_snapshot_boundaries_lose_nothing_and_duplicate_nothing(self):
        self._boundary_cells(self._fresh_driver())

    def test_boundary_cells_hold_for_50_rounds(self):
        drv = self._fresh_driver()
        for r in range(50):
            with self.subTest(round=r):
                self._boundary_cells(drv)
        self.assertEqual(drv._consumers, [])

    def test_replayed_object_also_in_live_queue_is_yielded_once(self):
        # 同一对象既在缓冲又被放进订阅队列：只出一次；同 cursor 的新对象照出（哨兵防挂死）
        drv = self._fresh_driver()
        old = self._ev("old")
        drv._emit_event(old)
        gen = drv.events()                                 # 尚未执行：首个 next 才登记
        fresh = self._ev("fresh")
        marker = self._ev("marker", "turn:t1:item:0002")
        def feed():
            deadline = time.time() + 5
            while time.time() < deadline:
                with drv._consumers_lock:
                    if drv._consumers:
                        for q in drv._consumers:
                            q.put_nowait(old); q.put_nowait(fresh); q.put_nowait(marker)
                        return
                time.sleep(0.01)
        threading.Thread(target=feed, daemon=True).start()
        self.assertIs(self._next(gen), old)
        got = self._next(gen)
        self.assertIs(got, fresh, f"expected the same-cursor new object, got {getattr(got, 'text', None)!r}")
        self.assertIs(self._next(gen), marker)
        gen.close()

    def test_forced_id_reuse_after_trim_does_not_drop_new_event(self):
        # 缓冲截断释放旧对象后，强制造一个与旧对象 id 相同的新对象再发：只存 id 不持引用的实现会误丢它。
        # 持引用的实现里旧对象仍活着，id 不可能复用——循环找不到同 id，发出最后一个新对象，同样须到。
        drv = self._fresh_driver()
        victim = self._ev("victim")
        filler = self._ev("filler")                        # 重放的最后一个：生成器的循环变量停在它身上，victim 只剩去重表可能持有
        drv._emit_event(victim)
        drv._emit_event(filler)
        gen = drv.events()
        self.assertIs(self._next(gen), victim)
        self.assertIs(self._next(gen), filler)
        box = {}
        waiter = threading.Thread(target=lambda: box.setdefault("v", next(gen)), daemon=True)
        waiter.start()                                     # 生成器走完重放、放下缓冲锁、进入实时等待
        target = id(victim)
        with drv._event_log_lock:                          # 重放结束后才拿得到
            drv._event_log = []                            # 模拟截断：缓冲不再持有
        del victim
        reused, keep = None, []
        for i in range(20000):
            cand = self._ev(f"cand{i}")
            if id(cand) == target:
                reused = cand
                break
            keep.append(cand)
        new = reused if reused is not None else keep[-1]
        drv._emit_event(new)
        waiter.join(5)
        self.assertIs(box.get("v"), new, f"new event dropped (forced id reuse happened: {reused is not None})")
        gen.close()

    def test_replay_memory_bounded_by_snapshot_and_live_not_accumulated(self):
        drv = self._fresh_driver()
        buf = [self._ev(f"b{i}") for i in range(30)]
        for e in buf:
            drv._emit_event(e)
        gen = drv.events()
        self.assertIs(self._next(gen), buf[0])             # 已登记、正持缓冲锁重放：此后的才是实时事件
        live = [self._ev(f"l{i}") for i in range(300)]
        self._emit_from_thread(drv, live)
        for want in buf[1:] + live:
            self.assertIs(self._next(gen), want)
        table = gen.gi_frame.f_locals["replayed"]
        self.assertEqual(len(table), len(buf), f"replay table holds {len(table)}, snapshot was {len(buf)}")
        self.assertTrue(all(table.get(id(e)) is not e for e in live), "live events must not enter the replay table")
        gen.close()

    def test_close_or_error_at_any_point_leaves_no_subscription(self):
        drv = self._fresh_driver()
        a, b = self._ev("a"), self._ev("b")
        drv._emit_event(a); drv._emit_event(b)
        gen = drv.events()
        self.assertIs(self._next(gen), a)                  # 停在缓冲 yield（已登记、持缓冲锁）
        self.assertEqual(len(drv._consumers), 1)
        gen.close()
        self.assertEqual(drv._consumers, [])
        self.assertTrue(drv._event_log_lock.acquire(timeout=1)); drv._event_log_lock.release()   # 缓冲锁已放
        gen = drv.events()
        self.assertIs(self._next(gen), a)
        with self.assertRaises(RuntimeError):
            gen.throw(RuntimeError("consumer blew up during replay"))
        self.assertEqual(drv._consumers, [])
        gen = drv.events()
        self.assertIs(self._next(gen), a); self.assertIs(self._next(gen), b)
        m = self._ev("m", "turn:t1:item:0002")
        self._emit_from_thread(drv, [m])
        self.assertIs(self._next(gen), m)                  # 停在实时 yield
        gen.close()
        self.assertEqual(drv._consumers, [])

    class _AttemptLock:
        # 包装 log 锁：标名线程一旦尝试取锁即置 attempted（取锁前置位），用来确认 producer 真的在等这把锁
        def __init__(self, real, watch_name):
            self._real, self._watch = real, watch_name
            self.attempted = threading.Event()
        def __enter__(self):
            if threading.current_thread().name == self._watch:
                self.attempted.set()
            return self._real.__enter__()
        def __exit__(self, *a):
            return self._real.__exit__(*a)

    def _f8_cell(self, drv):
        # F8：重放在 log 锁内 yield——生成器停在重放中途时，独立 producer 的 _emit_event 必须被挡在 log 锁上、
        # 不能完成；放行后才完成，且该事件恰到一次。锁外复制快照再 yield 的实现里 producer 会立刻完成 ⇒ 红在
        # 「重放中 producer 不能完成」这条断言（有限等待，不靠挂起判红）。
        self._reset(drv)
        a, b = self._ev("a"), self._ev("b")
        drv._emit_event(a); drv._emit_event(b)
        lock = self._AttemptLock(drv._event_log_lock, "f8-producer")
        drv._event_log_lock = lock
        x = self._ev("x-from-producer")
        done = threading.Event()
        producer = threading.Thread(target=lambda: (drv._emit_event(x), done.set()), name="f8-producer", daemon=True)
        gen = drv.events()
        try:
            self.assertIs(self._next(gen), a)              # 停在重放中途
            producer.start()
            self.assertTrue(lock.attempted.wait(2.0), "producer never tried to take the log lock")
            self.assertFalse(done.wait(0.3), "producer's _emit_event completed while replay was paused (replay not holding the log lock)")
            self.assertIs(self._next(gen), b)              # 继续重放；走完缓冲才放锁
            got = self._next(gen)                          # 放锁后 producer 完成，经订阅队列到达
            self.assertIs(got, x, f"producer event not delivered after replay, got {getattr(got, 'text', None)!r}")
            self.assertTrue(done.wait(2.0), "producer did not finish after replay released the log lock")
            marker = self._ev("marker", "turn:t1:item:0002")
            self._emit_from_thread(drv, [marker])
            self.assertIs(self._next(gen), marker, "producer event delivered more than once or out of order")
        finally:
            gen.close()
            producer.join(2.0)
            drv._event_log_lock = lock._real
        self.assertFalse(producer.is_alive(), "producer thread left running")
        self.assertEqual(drv._consumers, [])

    def test_replay_holds_log_lock_so_producer_waits_and_event_arrives_once(self):
        self._f8_cell(self._fresh_driver())

    def test_replay_holds_log_lock_cell_holds_for_50_rounds(self):
        drv = self._fresh_driver()
        for r in range(50):
            with self.subTest(round=r):
                self._f8_cell(drv)

    def test_real_session_event_is_unhashable(self):
        # 守卫若用 WeakSet／set 会在真对象上直接 TypeError；用例用真 SessionEvent 而非替身类。
        with self.assertRaises(TypeError):
            hash(SessionEvent(kind="assistant_text"))


if __name__ == "__main__":
    unittest.main()
