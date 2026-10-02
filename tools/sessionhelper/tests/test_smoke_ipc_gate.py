"""smoke 起真 helper 前的 IPC 路径机械闸（组长 R-SMOKE-IPC-HIJACK-20261002154248）。

闸在 tests/_smoke_isolation.py：IPC 路径（含 DINGWEI_ 回落键）与 HOME 不在本次临时目录内 ⇒ SystemExit、不启动 helper。
最后一格是真进程：IPC 路径指向临时目录外的一个哨兵文件（代替在线 helper 的真路径；它在另一个测试临时目录里，
不碰 /run/sessionhelper）。有闸 ⇒ 不启动、哨兵原样；去掉闸 ⇒ helper 启动即先删后绑，哨兵被删，本格红。
"""
import os
import subprocess
import sys
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

try:
    from _smoke_isolation import check_helper_env_isolated, isolated_helper_env, spawn_helper
except ImportError:  # pragma: no cover
    from tests._smoke_isolation import check_helper_env_isolated, isolated_helper_env, spawn_helper

_PKG_DIR = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


class SmokeIpcGate(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="sh-smoke-gate-"))
        self.outside = Path(tempfile.mkdtemp(prefix="sh-smoke-gate-outside-"))

    def _env(self, **overrides):
        live = {"SH_MCP_IPC_SOCK": "/run/sessionhelper/dingwei-mcp.sock",
                "DINGWEI_MCP_IPC_SOCK": "/run/sessionhelper/dingwei-mcp.sock",
                "SH_SYSTEM_PROMPT": "live seat prompt", "PATH": os.environ.get("PATH", "/usr/bin:/bin")}
        with mock.patch.dict(os.environ, live, clear=True):
            return isolated_helper_env(self.tmp, "x", overrides)

    def test_isolated_env_strips_inherited_seat_variables(self):
        env = self._env(SH_MODE="echo")
        self.assertNotIn("DINGWEI_MCP_IPC_SOCK", env)
        self.assertNotIn("SH_SYSTEM_PROMPT", env)
        self.assertEqual(env["SH_MCP_IPC_SOCK"], str(self.tmp / "x-mcp.sock"))
        self.assertEqual(env["HOME"], str(self.tmp / "home-x"))
        self.assertEqual(env["SH_MODE"], "echo")
        check_helper_env_isolated(env, self.tmp)  # 不退出

    def test_gate_rejects_ipc_outside_tmp(self):
        for bad in ("/run/sessionhelper/dingwei-mcp.sock", str(self.outside / "s.sock"), str(self.tmp),
                    str(self.tmp / ".." / self.outside.name / "s.sock")):
            env = self._env(SH_MCP_IPC_SOCK=bad)
            with self.assertRaises(SystemExit, msg=bad):
                check_helper_env_isolated(env, self.tmp)

    def test_gate_rejects_missing_ipc_and_outside_fallback_key(self):
        env = self._env()
        env.pop("SH_MCP_IPC_SOCK")
        with self.assertRaises(SystemExit):
            check_helper_env_isolated(env, self.tmp)
        env = self._env(DINGWEI_MCP_IPC_SOCK="/run/sessionhelper/dingwei-mcp.sock")
        with self.assertRaises(SystemExit):
            check_helper_env_isolated(env, self.tmp)

    def test_gate_rejects_symlink_escape(self):
        link = self.tmp / "link"
        link.symlink_to(self.outside)
        env = self._env(SH_MCP_IPC_SOCK=str(link / "s.sock"))
        with self.assertRaises(SystemExit):
            check_helper_env_isolated(env, self.tmp)

    def test_gate_rejects_home_outside_tmp(self):
        env = self._env(HOME=str(Path.home()))
        with self.assertRaises(SystemExit):
            check_helper_env_isolated(env, self.tmp)

    def test_gate_stops_real_helper_from_hijacking_a_path_outside_tmp(self):
        sentinel = self.outside / "live-ipc.sock"
        sentinel.write_text("live helper socket stand-in\n")
        env = self._env(SH_MCP_IPC_SOCK=str(sentinel), PYTHONPATH=_PKG_DIR, SH_SESSION_NAME="gateprobe",
                        SH_KEY_ID="k", SH_SECRET="s", SH_WS_BASE="ws://127.0.0.1:9", SH_MODE="echo",
                        SH_MIRROR_TO="", SH_COLLECT="0", SH_NO_DIRECTORY="1", SH_PROXY="off",
                        SH_INBOX_DB=str(self.tmp / "inbox.db"))
        proc = None
        log = open(self.tmp / "helper.log", "w")
        try:
            try:
                proc = spawn_helper([sys.executable, "-m", "sessionhelper"], self.tmp, env, cwd=_PKG_DIR,
                                    stdout=log, stderr=subprocess.STDOUT)
            except SystemExit:
                proc = None
            if proc is not None:  # 闸失效：给 helper 时间走到先删后绑，好让读数说清后果
                deadline = time.time() + 10
                while time.time() < deadline and sentinel.exists() and not sentinel.is_socket():
                    time.sleep(0.1)
        finally:
            if proc is not None:
                proc.kill()
                proc.wait(timeout=10)
            log.close()
        hijacked = (not sentinel.exists()) or sentinel.is_socket()
        self.assertIsNone(proc, f"gate let a helper start with IPC outside tmp; sentinel hijacked={hijacked}")
        self.assertEqual(sentinel.read_text(), "live helper socket stand-in\n")


if __name__ == "__main__":
    unittest.main()
