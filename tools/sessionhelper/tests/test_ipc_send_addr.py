"""Legacy helper address completion and R60 directory-bound caller addresses."""
import unittest
import os
import importlib.util
from pathlib import Path
from unittest import mock
import sys

SEND_SCRIPT = Path(__import__("os").environ.get(
    "SEND_DINGWEI_TEST_MODULE",
    Path(__file__).parents[1] / "sessionhelper" / "send_dingwei.py",
))
_spec = importlib.util.spec_from_file_location("send_dingwei_under_test", SEND_SCRIPT)
send_dingwei = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(send_dingwei)


def complete(to: str, key_id: str) -> str:
    """与 app.py::_make_send_handler 中的补全规则一致(行为契约)。"""
    return to if "#" in to else f"{to}#{key_id}"


class TestIPCSendAddressCompletion(unittest.TestCase):
    KEY = "FB-zzc-devteam-e0d12642"

    def test_bare_session_name_is_completed(self):
        # 🔴 回归点:defb424f 曾删掉补全,导致裸名进信封 → Hub invalid address
        self.assertEqual(complete("zzc-prodops-2642", self.KEY),
                         "zzc-prodops-2642#" + self.KEY)

    def test_full_address_is_left_alone(self):
        # 防修过头:已带 # 的完整地址不得被二次拼接
        full = "zzc-prodops-2642#" + self.KEY
        self.assertEqual(complete(full, self.KEY), full)

    def test_cross_key_address_preserved(self):
        # 指向别的 key 的地址必须原样保留,不得被本机 key 覆盖
        other = "zzc-someone-1234#FB-other-key"
        self.assertEqual(complete(other, self.KEY), other)

    def test_three_segment_target_rejected_before_ipc(self):
        with mock.patch.object(sys, "argv", [str(SEND_SCRIPT), "r2.agent.user#key", "hello"]), \
             mock.patch.object(send_dingwei, "ipc_send", side_effect=AssertionError("IPC must not run")):
            with self.assertRaisesRegex(SystemExit, "三段及以上子 Agent 目标当前不受支持"):
                send_dingwei.main()

    def test_root_two_segment_target_stays_supported(self):
        with mock.patch.object(sys, "argv", [str(SEND_SCRIPT), "agent.user#key", "hello"]), \
             mock.patch.object(send_dingwei, "ipc_send", return_value={"ok": True, "request_id":"fixture", "outcome": {"to": "agent.user#key", "outcome":"accepted", "phase":"local_queued", "accepted_by":"local_helper","request_id":"fixture","source_message_id":"a"*32}}) as ipc:
            send_dingwei.main()
        self.assertEqual(ipc.call_count,1)
        self.assertEqual(ipc.call_args.args[:2],("agent.user#key","hello"))
        self.assertRegex(ipc.call_args.args[2],r"^[0-9a-f]{32}$")


class DestinationIdentityTest(unittest.TestCase):
    def test_inherited_sender_key_never_rewrites_recipient(self):
        with mock.patch.dict(os.environ,{'SH_KEY_ID':'sender-owner-key'}):
            self.assertEqual(send_dingwei.complete_addr('peer.other-owner'),'peer.other-owner')
            self.assertEqual(send_dingwei.complete_addr('peer#verified-key'),'peer#verified-key')


if __name__ == "__main__":
    unittest.main()
