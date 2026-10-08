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

    def test_same_item_delta_after_resubscribe_is_delivered(self):
        # r5 真退出格 REAL-EXIT-BLOCKER：Codex 同一 item 的各片共用 cursor；重连重订阅后同 cursor 的新片
        # 不得被当成已重放过的丢掉（旧实现按 cursor 去重即丢）。必须恰好到一次。
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

    def test_replayed_object_also_in_live_queue_is_yielded_once(self):
        # 重放与实时重叠（缓冲里的同一对象又进了订阅队列）只出一次；同 cursor 的不同对象照出。
        h = BridgeHarness(self)
        drv = h.driver
        drv._rpc = HealthyRpc()
        old = SessionEvent(kind="assistant_text", text="old", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
        drv._emit_event(old)
        gen = drv.events()
        self.assertIs(self._next(gen), old)                # 重放（订阅队列此刻尚未登记）
        fresh = SessionEvent(kind="assistant_text", text="fresh", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
        marker = SessionEvent(kind="assistant_text", text="marker", cursor="turn:t1:item:0002", delivery_id="d1", turn_id="t1")

        def feed():
            deadline = time.time() + 5
            while time.time() < deadline:
                with drv._consumers_lock:
                    if drv._consumers:
                        for q in drv._consumers:
                            q.put_nowait(old)              # 同一对象重叠：须跳过
                            q.put_nowait(fresh)            # 同 cursor 的新对象：须出
                            q.put_nowait(marker)           # 不同 cursor 的哨兵：fresh 若被误丢，下一条就是它（判红不挂死）
                        return
                time.sleep(0.01)

        threading.Thread(target=feed, daemon=True).start()
        got = self._next(gen)
        self.assertIs(got, fresh, f"expected the same-cursor new object, got {getattr(got, 'text', None)!r}")
        self.assertIs(self._next(gen), marker)

    @staticmethod
    def _next(gen, timeout=5.0):
        # 限时取下一条：被测实现若丢事件，生成器会在健康驱动上一直等——这里超时即返回哨兵，判红不挂死
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

    def _driver_with_buffer(self, texts, cursor="turn:t1:item:0001"):
        h = BridgeHarness(self)
        drv = h.driver
        drv._rpc = HealthyRpc()
        evs = [SessionEvent(kind="assistant_text", text=t, cursor=cursor, delivery_id="d1", turn_id="t1") for t in texts]
        for ev in evs:
            drv._emit_event(ev)
        return drv, evs

    def _feed_after_registration(self, drv, events):
        # 订阅队列登记后才经 _emit_event 发出（真实时路径）；登记前不发，避免与追平路径混淆
        def run():
            deadline = time.time() + 5
            while time.time() < deadline:
                with drv._consumers_lock:
                    ready = bool(drv._consumers)
                if ready:
                    for ev in events:
                        drv._emit_event(ev)
                    return
                time.sleep(0.01)
        threading.Thread(target=run, daemon=True).start()

    def test_same_cursor_same_text_new_object_is_kept(self):
        # 同 cursor 且同文本的不同对象是新片，追平路径与实时路径都须保留
        drv, (old,) = self._driver_with_buffer(["same"])
        gen = drv.events()
        self.assertIs(self._next(gen), old)
        twin_catchup = SessionEvent(kind="assistant_text", text="same", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
        drv._emit_event(twin_catchup)                      # 重放进行中发出 ⇒ 走追平
        got = self._next(gen)
        self.assertIs(got, twin_catchup, f"same cursor+text new object dropped on catch-up path, got {getattr(got, 'text', None)!r}")
        twin_live = SessionEvent(kind="assistant_text", text="same", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
        marker = SessionEvent(kind="assistant_text", text="marker", cursor="turn:t1:item:0002", delivery_id="d1", turn_id="t1")
        self._feed_after_registration(drv, [twin_live, marker])
        got = self._next(gen)
        self.assertIs(got, twin_live, f"same cursor+text new object dropped on live path, got {getattr(got, 'text', None)!r}")
        self.assertIs(self._next(gen), marker)

    def test_event_emitted_during_replay_is_delivered_exactly_once(self):
        # 衔接窗口：重放到一半时发出的事件既不能丢，也不能因随后进队列而出两次
        drv, (e0, e1) = self._driver_with_buffer(["e0", "e1"])
        gen = drv.events()
        self.assertIs(self._next(gen), e0)                 # 停在重放中途，订阅队列尚未登记
        mid = SessionEvent(kind="assistant_text", text="mid", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
        drv._emit_event(mid)
        marker = SessionEvent(kind="assistant_text", text="marker", cursor="turn:t1:item:0002", delivery_id="d1", turn_id="t1")
        self._feed_after_registration(drv, [marker])
        seen = [getattr(self._next(gen), "text", "<none>") for _ in range(3)]
        self.assertEqual(seen, ["e1", "mid", "marker"], f"window loss or duplicate: {seen}")

    def test_flood_during_replay_beyond_queue_cap_is_not_lost(self):
        # 重放期间灌入超过订阅队列上限（1000）的事件：追平路径接住，一条不丢、顺序不乱
        from sessionhelper.codexDriver import FANOUT_QUEUE_MAX
        drv, buf = self._driver_with_buffer([f"b{i}" for i in range(10)])
        gen = drv.events()
        self.assertIs(self._next(gen), buf[0])
        flood = [SessionEvent(kind="assistant_text", text=f"f{i}", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
                 for i in range(2 * FANOUT_QUEUE_MAX)]
        for ev in flood:
            drv._emit_event(ev)
        marker = SessionEvent(kind="assistant_text", text="marker", cursor="turn:t1:item:0002", delivery_id="d1", turn_id="t1")
        self._feed_after_registration(drv, [marker])
        want = [e.text for e in buf[1:]] + [e.text for e in flood] + ["marker"]
        got = []
        for _ in range(len(want)):
            ev = self._next(gen)
            if ev is None:
                break
            got.append(ev.text)
        self.assertEqual(len(got), len(want), f"lost {len(want) - len(got)} of {len(want)} (last got {got[-1:] })")
        self.assertEqual(got, want)

    def test_trimmed_replayed_objects_do_not_shadow_new_ones_by_id(self):
        # 重放过的对象从缓冲裁掉并释放后，新对象可能拿到同一个 id()；去重若只存 id 不持引用，会把这条新事件
        # 误当成已重放丢掉。实现持引用，故这些 id 不会被复用；新对象全部须到。
        drv, buf = self._driver_with_buffer([f"old{i}" for i in range(200)])
        gen = drv.events()
        for want in buf:
            self.assertIs(self._next(gen), want)
        with drv._event_log_lock:
            drv._event_log = []                            # 模拟裁剪：缓冲不再持有这些对象
        old_ids = {id(e) for e in buf}
        del buf
        news = []
        def feed():
            deadline = time.time() + 5
            while time.time() < deadline:
                with drv._consumers_lock:
                    ready = bool(drv._consumers)
                if ready:
                    for i in range(400):
                        ev = SessionEvent(kind="assistant_text", text=f"new{i}", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
                        news.append(ev)
                        drv._emit_event(ev)
                    return
                time.sleep(0.01)
        feeder = threading.Thread(target=feed, daemon=True)
        feeder.start()
        first = self._next(gen)                            # 追平为空 ⇒ 登记 ⇒ 等实时
        feeder.join(5)
        got = [first.text] if first is not None else []
        while len(got) < 400:
            ev = self._next(gen, timeout=2.0)
            if ev is None:
                break
            got.append(ev.text)
        reused = sum(1 for e in news if id(e) in old_ids)
        self.assertEqual(got, [f"new{i}" for i in range(400)], f"lost new events (ids reused from replayed objects: {reused})")

    def test_replay_dedup_table_is_bounded_and_live_events_do_not_accumulate(self):
        import sessionhelper.codexDriver as cd
        with mock.patch.object(cd, "REPLAY_DEDUP_MAX", 50):
            drv, buf = self._driver_with_buffer([f"b{i}" for i in range(120)])
            gen = drv.events()
            for want in buf:
                self.assertIs(self._next(gen), want)
            live = [SessionEvent(kind="assistant_text", text=f"l{i}", cursor="turn:t1:item:0001", delivery_id="d1", turn_id="t1")
                    for i in range(200)]
            self._feed_after_registration(drv, live)
            for want in live:
                self.assertIs(self._next(gen), want)
            table = gen.gi_frame.f_locals["replayed"]
            self.assertLessEqual(len(table), 50, f"replay dedup table grew to {len(table)}")
            self.assertTrue(all(e not in table.values() for e in live), "live events must not enter the replay table")
            gen.close()

    def test_close_or_error_at_any_point_leaves_no_subscription(self):
        # 重放中途 close、登记后 close、登记后抛异常：finally 都须把订阅队列清掉
        drv, buf = self._driver_with_buffer(["a", "b"])
        gen = drv.events()
        self.assertIs(self._next(gen), buf[0])
        gen.close()                                        # 重放中途关闭：从未登记
        self.assertEqual(drv._consumers, [])
        gen = drv.events()
        self.assertIs(self._next(gen), buf[0])
        self.assertIs(self._next(gen), buf[1])
        marker = SessionEvent(kind="assistant_text", text="m", cursor="turn:t1:item:0002", delivery_id="d1", turn_id="t1")
        self._feed_after_registration(drv, [marker])
        self.assertIs(self._next(gen), marker)             # 已登记、停在实时 yield
        self.assertEqual(len(drv._consumers), 1)
        gen.close()
        self.assertEqual(drv._consumers, [])
        gen = drv.events()
        for want in buf:
            self.assertIs(self._next(gen), want)
        self._feed_after_registration(drv, [marker])
        self.assertIs(self._next(gen), marker)
        with self.assertRaises(RuntimeError):
            gen.throw(RuntimeError("consumer blew up"))
        self.assertEqual(drv._consumers, [])

    def _reset_driver(self, drv):
        with drv._event_log_lock:
            drv._event_log = []
            drv._event_log_base = 0
        with drv._consumers_lock:
            drv._consumers.clear()

    def test_boundary_and_overlap_cells_hold_for_50_rounds(self):
        # 关键并发格各连跑 50 轮：屏障＝暂停生成器（重放中途）＋等登记后再发（实时），不靠 sleep 凑碰撞
        h = BridgeHarness(self)
        drv = h.driver
        drv._rpc = HealthyRpc()
        mk = lambda t, c="turn:t1:item:0001": SessionEvent(kind="assistant_text", text=t, cursor=c, delivery_id="d1", turn_id="t1")
        for r in range(50):
            with self.subTest(round=r, cell="boundary"):
                self._reset_driver(drv)
                e0, e1 = mk("e0"), mk("e1")
                drv._emit_event(e0); drv._emit_event(e1)
                gen = drv.events()
                self.assertIs(self._next(gen), e0)
                mid = mk("mid")
                drv._emit_event(mid)                       # 登记前、快照后注入
                marker = mk("marker", "turn:t1:item:0002")
                self._feed_after_registration(drv, [marker])
                self.assertEqual([getattr(self._next(gen), "text", "<none>") for _ in range(3)], ["e1", "mid", "marker"])
                gen.close()
            with self.subTest(round=r, cell="overlap+same-text"):
                self._reset_driver(drv)
                old = mk("same")
                drv._emit_event(old)
                gen = drv.events()
                self.assertIs(self._next(gen), old)
                twin = mk("same")
                marker = mk("marker", "turn:t1:item:0002")
                def feed(old=old, twin=twin, marker=marker):
                    deadline = time.time() + 5
                    while time.time() < deadline:
                        with drv._consumers_lock:
                            if drv._consumers:
                                for q in drv._consumers:
                                    q.put_nowait(old); q.put_nowait(twin); q.put_nowait(marker)
                                return
                        time.sleep(0.005)
                threading.Thread(target=feed, daemon=True).start()
                self.assertIs(self._next(gen), twin)
                self.assertIs(self._next(gen), marker)
                gen.close()
        self.assertEqual(drv._consumers, [])

    def test_real_session_event_is_unhashable(self):
        # 守卫若用 WeakSet／set 会在真对象上直接 TypeError；用例用真 SessionEvent 而非替身类。
        with self.assertRaises(TypeError):
            hash(SessionEvent(kind="assistant_text"))


if __name__ == "__main__":
    unittest.main()
