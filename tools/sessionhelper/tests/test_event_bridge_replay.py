"""R-V2-0105 件二：事件桥重订阅不得把驱动缓冲里已发过的事件再发一遍（要点第七、九至十一、十四节）。

现场机理：codexDriver.events() 无游标时先把 _event_log 整份 yield，rpc 不健康即返回；
事件桥 1 秒后重订阅 ⇒ 整份再发，Hub 当新事件（dw2 洪流）。这里用真 CodexProtocolDriver 的缓冲与
events()，只把 rpc 换成「不健康」的桩，让桥真实地反复重订阅；另起线程持续取 _event_q 计数。
"""
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

    def test_real_session_event_is_unhashable(self):
        # 守卫若用 WeakSet／set 会在真对象上直接 TypeError；用例用真 SessionEvent 而非替身类。
        with self.assertRaises(TypeError):
            hash(SessionEvent(kind="assistant_text"))


if __name__ == "__main__":
    unittest.main()
