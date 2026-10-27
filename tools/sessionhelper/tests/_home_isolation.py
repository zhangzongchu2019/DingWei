"""测试隔离真实 ~/.dingwei：模块级 setUpModule 把 HOME 指向临时目录，tearDownModule 断言真实目录没动。

为什么需要：SessionHelper() 构造会往 ~/.dingwei 装 send.py（覆盖真席位的发信脚本）、
写 inbox/audit 库和在线清单；升级状态文件留在真实 HOME 里还会让下一轮测试进程 os._exit(0)
无声退出（rc=0、日志为空）。

用法（三种跑法都能导入：pytest、-m unittest tests.x、直接运行）：
    try:
        from _home_isolation import HomeIsolation
    except ImportError:
        from tests._home_isolation import HomeIsolation
    _HOME = HomeIsolation()
    def setUpModule(): _HOME.start()
    def tearDownModule(): _HOME.finish()
"""
import hashlib
import os
import shutil
import tempfile
import unittest
from pathlib import Path
from unittest import mock
try:
    from _suite_isolation import isolated_environment
except ImportError:
    from tests._suite_isolation import isolated_environment

# 在 import 时（HOME 还没被改）记下真实 ~/.dingwei。
REAL_DINGWEI = Path.home() / ".dingwei"
# 这些由真席位的在线 helper 自己持续改写，不属于测试泄漏，不纳入比对。
LIVE_NOISE_SUFFIXES = ("-wal", "-shm", ".log", ".DingWeiOnlineSessions.list")


def snapshot_real_dingwei() -> dict:
    """文件名 → (内容哈希, mtime_ns)。mtime 必须比：旧行为是用同内容覆盖 send.py，只比哈希抓不到。"""
    snap = {}
    if REAL_DINGWEI.is_dir():
        for p in REAL_DINGWEI.iterdir():
            if p.is_file() and not p.name.endswith(LIVE_NOISE_SUFFIXES):
                snap[p.name] = (hashlib.sha256(p.read_bytes()).hexdigest(), p.stat().st_mtime_ns)
    return snap


class HomeIsolation:
    def __init__(self):
        self._patcher = None
        self._tmp = None
        self._before = None

    def start(self):
        self._before = snapshot_real_dingwei()
        # SH_PROVISION_NO_EXIT 兜底：任何路径走到 request_restart 都不许真的 os._exit 掉测试进程。
        self._patcher = isolated_environment(os.environ.get('SH_DRIVER', 'unset'))
        self._patcher.__enter__()

    def finish(self):
        self._patcher.__exit__(None, None, None)
        after = snapshot_real_dingwei()
        changed = sorted(k for k in set(after) | set(self._before) if after.get(k) != self._before.get(k))
        assert not changed, f"测试泄漏到真实 ~/.dingwei: {changed}"
