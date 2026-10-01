"""回滚状态机：rolled_back 是终态，不得在下一次启动时再次回滚并重启（重启循环）。

线上形状：升级失败 → 回滚 + request_restart(os._exit) → 状态文件留在 rolled_back（非 confirmed）
→ 重启后 SessionHelper() 构造再调 rollback_stale_update_if_needed，ts 已陈旧 → 再回滚、再重启……
"""
import json
import shutil
import tempfile
import time
import unittest
from pathlib import Path
from unittest import mock

from sessionhelper.config import load_config
from sessionhelper.provision import Provisioner

try:
    from _home_isolation import HomeIsolation
except ImportError:
    from tests._home_isolation import HomeIsolation

_HOME = HomeIsolation()


def setUpModule():
    _HOME.start()


def tearDownModule():
    _HOME.finish()


class RolledBackIsTerminalTest(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp(prefix="rollback-loop-"))
        self.addCleanup(shutil.rmtree, self.tmp, True)
        self.root = self.tmp / "sessionhelper"
        self.backup = self.tmp / "backup"
        for d in (self.root, self.backup):
            d.mkdir()
        (self.root / "app.py").write_text("new", encoding="utf-8")
        (self.backup / "app.py").write_text("old", encoding="utf-8")
        cfg = load_config({
            "SH_SESSION_NAME": "home", "SH_OWNER": "zzc", "SH_KEY_ID": "FB-2642", "SH_SECRET": "secret",
            "SH_PROVISION_STATE_FILE": str(self.tmp / "state.json"),
            "SH_PROVISION_VERSIONS_FILE": str(self.tmp / "versions.json"),
            "SH_PROVISION_AUDIT_DB": str(self.tmp / "audit.db"),
        })
        self.prov = Provisioner(cfg, package_root=str(self.root))
        self.restarts = mock.patch.object(Provisioner, "request_restart", autospec=True)
        self.restart_mock = self.restarts.start()
        self.addCleanup(self.restarts.stop)

    def write_state(self, status, age=1000):
        self.prov.update_state_file.write_text(json.dumps(
            {"version": "9.9", "backup": str(self.backup), "status": status, "ts": int(time.time()) - age}),
            encoding="utf-8")

    def state(self):
        return json.loads(self.prov.update_state_file.read_text(encoding="utf-8"))

    def test_stale_pending_rolls_back_exactly_once_across_restarts(self):
        # 整条循环：第一次启动回滚+请求重启；之后的每次"重启"（再次构造/调用）都不得再回滚。
        self.write_state("pending")
        self.assertTrue(self.prov.rollback_stale_update_if_needed(300))
        self.assertEqual(self.state()["status"], "rolled_back")
        self.assertEqual(self.restart_mock.call_count, 1)
        (self.root / "app.py").write_text("old-then-edited", encoding="utf-8")
        for _ in range(3):
            self.assertFalse(self.prov.rollback_stale_update_if_needed(300))
        self.assertEqual(self.restart_mock.call_count, 1, "rolled_back 之后不得再请求重启（重启循环）")
        self.assertEqual((self.root / "app.py").read_text(encoding="utf-8"), "old-then-edited",
                         "rolled_back 之后不得再用备份覆盖当前包")

    def test_rolled_back_with_old_ts_is_not_rolled_back_again(self):
        self.write_state("rolled_back", age=5425)
        self.assertFalse(self.prov.rollback_stale_update_if_needed(300))
        self.assertEqual(self.restart_mock.call_count, 0)

    def test_confirmed_stays_terminal(self):
        self.write_state("confirmed")
        self.assertFalse(self.prov.rollback_stale_update_if_needed(300))
        self.assertEqual(self.restart_mock.call_count, 0)

    def test_fresh_pending_within_timeout_is_not_rolled_back(self):
        # 原行为不变：新升级在超时窗口内，不回滚。
        self.write_state("pending", age=10)
        self.assertFalse(self.prov.rollback_stale_update_if_needed(300))

    def test_old_version_connecting_after_rollback_does_not_claim_confirmed(self):
        self.write_state("rolled_back")
        self.prov.confirm_update_connected()
        self.assertEqual(self.state()["status"], "rolled_back")

    def test_pending_is_still_confirmed_on_connect(self):
        # 原行为不变：新版本连上 Hub 后 pending → confirmed。
        self.write_state("pending", age=5)
        self.prov.confirm_update_connected()
        self.assertEqual(self.state()["status"], "confirmed")


if __name__ == "__main__":
    unittest.main()
