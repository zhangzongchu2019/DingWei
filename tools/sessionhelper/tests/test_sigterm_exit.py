"""B1③（R-V2-SEAT-ITEMS-ADDENDUM-20261005000704）：helper 在 SIGTERM 下正常退出。

背景：B1 给席位容器加了 Init:true（tini 式 init 收孤儿＋转 SIGTERM）——helper 变成
init 的子进程，SIGTERM 会被转发到它。本格钉：helper 收到 SIGTERM 须迅速退出
（不卡到 docker stop 的 10s 强杀窗、不被 SIGKILL）。
真 helper 子进程（echo 模式、死 Hub＝重连退避中）。"""
import os
import signal
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path


class SigtermExitTest(unittest.TestCase):
    def test_sigterm_exits_promptly(self):
        try:
            import websockets  # noqa: F401——缺则 helper 起不来；缺依赖时 skip 不误红
        except ImportError:
            raise unittest.SkipTest("websockets 未安装（helper 依赖）")
        with tempfile.TemporaryDirectory() as home:
            env = {k: v for k, v in os.environ.items() if not k.startswith("SH_")}  # 剔除本席的 SH_*（复验隔离提示）
            env.update({
                "SH_SESSION_NAME": "sigprobe",
                "SH_OWNER": "zzc",
                "SH_KEY_ID": "FB-1234",
                "SH_SECRET": "x",
                "SH_HUB_URL": "ws://127.0.0.1:1/ws",  # 死 Hub ⇒ 重连退避循环（最常见的在跑态）
                "SH_MODE": "echo",
                "HOME": home,
            })
            root = Path(__file__).resolve().parent.parent
            p = subprocess.Popen([sys.executable, "-m", "sessionhelper"], cwd=root, env=env,
                                 stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            time.sleep(3)  # 起来并进重连循环
            if p.poll() is not None:
                self.fail(f"helper 没起来: rc={p.returncode}")
            t0 = time.monotonic()
            p.send_signal(signal.SIGTERM)
            try:
                rc = p.wait(timeout=5)
            except subprocess.TimeoutExpired:
                p.kill()
                self.fail("SIGTERM 后 5s 未退出（会被 SIGKILL——Init 件失效）")
            self.assertLess(time.monotonic() - t0, 5, "须快速退出")
            self.assertNotEqual(rc, -signal.SIGKILL, "不得是被 SIGKILL 杀的")
            # rc=-15（被 SIGTERM 终止）或 0（优雅处理）都算正常退出


if __name__ == "__main__":
    unittest.main()
