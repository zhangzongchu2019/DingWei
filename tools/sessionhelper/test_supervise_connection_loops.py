"""T-078 回归:WS 断开后,绑在这条连接上的循环必须**全部**被收掉。

背景(实测,非推断):
  · Hub 侧 8 个失败时刻逐条查过 —— 连接健康、conn_seq 不变、client_present=true
    照常入队,**没有任何关闭/错误码事件**。
  · helper 收件库同 8 条记为 failed,error="no close frame received or sent",
    且是在收到后 0.05~0.2 秒内盖上的、attempts=1。
  · 投递号与 Hub 日志逐个对得上。
=> 断的不是 Hub 看到的那条连接,而是本进程里一条早该死掉的旧循环。
"""
import asyncio
import sys
import types
import unittest

# 只取被测函数,不导入整包(整包会拉起驱动/网络依赖)
import importlib.util
import os

_spec_path = os.path.join(os.path.dirname(os.path.abspath(__file__)), "sessionhelper", "app.py")


def _load_supervise():
    """从 app.py 源码里单独取出被测函数,避免 import 整个模块的副作用。"""
    src = open(_spec_path, encoding="utf-8").read()
    start = src.index("async def supervise_connection_loops")
    end = src.index("\nclass ", start)
    ns = {"asyncio": asyncio}
    exec(compile(src[start:end], _spec_path, "exec"), ns)
    return ns["supervise_connection_loops"]


supervise = _load_supervise()


class SuperviseConnectionLoops(unittest.IsolatedAsyncioTestCase):

    async def test_clean_receive_eof_cancels_siblings(self):
        """A normal WebSocket iterator EOF must close this connection generation."""
        started, stopped = asyncio.Event(), asyncio.Event()

        async def recv():
            await started.wait()

        async def sender():
            started.set()
            try:
                await asyncio.Future()
            finally:
                stopped.set()

        task = asyncio.create_task(supervise([recv(), sender()]))
        try:
            done, _ = await asyncio.wait([task], timeout=0.2)
            self.assertIn(task, done, "clean EOF left the connection supervisor waiting forever")
            await task
            self.assertTrue(stopped.is_set())
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_normal_sender_exit_also_closes_generation(self):
        started, stopped = asyncio.Event(), asyncio.Event()

        async def receiver():
            started.set()
            try:
                await asyncio.Future()
            finally:
                stopped.set()

        async def sender():
            await started.wait()

        task = asyncio.create_task(supervise([receiver(), sender()]))
        try:
            done, _ = await asyncio.wait([task], timeout=0.2)
            self.assertIn(task, done)
            await task
            self.assertTrue(stopped.is_set())
        finally:
            task.cancel()
            await asyncio.gather(task, return_exceptions=True)

    async def test_twenty_clean_disconnects_leave_no_old_workers(self):
        active, peak = 0, 0
        for _ in range(20):
            started = asyncio.Event()

            async def recv():
                await started.wait()

            async def worker():
                nonlocal active, peak
                active += 1
                peak = max(peak, active)
                started.set()
                try:
                    await asyncio.Future()
                finally:
                    active -= 1

            task = asyncio.create_task(supervise([recv(), worker()]))
            try:
                done, _ = await asyncio.wait([task], timeout=0.2)
                self.assertIn(task, done)
                await task
                self.assertEqual(active, 0, "a disconnected worker survived into the next generation")
            finally:
                task.cancel()
                await asyncio.gather(task, return_exceptions=True)
        self.assertEqual(peak, 1)

    async def test_empty_worker_list_returns(self):
        await supervise([])

    async def test_siblings_are_cancelled_when_one_raises(self):
        """① 核心:一个循环抛异常后,兄弟循环必须停,不得带着死 ws 继续跑。"""
        ticks = {"n": 0}

        async def recv_loop():
            await asyncio.sleep(0.02)
            raise RuntimeError("no close frame received or sent")

        async def worker():
            while True:
                ticks["n"] += 1
                await asyncio.sleep(0.005)

        with self.assertRaises(RuntimeError):
            await supervise([recv_loop(), worker()])
        before = ticks["n"]
        await asyncio.sleep(0.1)          # 模拟外层重连那段时间
        self.assertEqual(ticks["n"], before,
                         f"旧 worker 在连接已断后仍在跑({before}->{ticks['n']}) —— T-078 根因复现")

    async def test_original_exception_still_propagates(self):
        """② 语义不得变:第一个异常照样向外抛,外层重连逻辑依赖它。"""
        async def boom():
            raise ValueError("原样抛出")

        async def idle():
            await asyncio.sleep(10)

        with self.assertRaises(ValueError):
            await supervise([boom(), idle()])

    async def test_cancellation_propagates_to_all(self):
        """③ 外部取消 supervise 本身时,子循环同样要收掉(重启/退出路径)。"""
        started = {"a": False, "b": False}
        done = {"a": False, "b": False}

        async def loop(name):
            started[name] = True
            try:
                await asyncio.sleep(10)
            except asyncio.CancelledError:
                done[name] = True
                raise

        task = asyncio.ensure_future(supervise([loop("a"), loop("b")]))
        await asyncio.sleep(0.02)
        self.assertTrue(all(started.values()), "前置:两个循环都应已启动")
        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task
        self.assertTrue(all(done.values()), f"未全部收到取消: {done}")

    async def test_positive_control_bare_gather_still_leaks(self):
        """④ 阳性对照:同样场景下,**裸 gather** 必须仍然泄漏。

        否则说明本测试的场景根本触发不了该缺陷,①的通过就毫无意义
        ——「不可能失败的检查等于没检查」。
        """
        ticks = {"n": 0}

        async def recv_loop():
            await asyncio.sleep(0.02)
            raise RuntimeError("no close frame received or sent")

        async def worker():
            while True:
                ticks["n"] += 1
                await asyncio.sleep(0.005)

        with self.assertRaises(RuntimeError):
            await asyncio.gather(recv_loop(), worker())
        before = ticks["n"]
        await asyncio.sleep(0.1)
        self.assertGreater(ticks["n"], before,
                           "裸 gather 竟未泄漏 —— 本测试场景无法证伪,①不可信")


if __name__ == "__main__":
    unittest.main(verbosity=2)
