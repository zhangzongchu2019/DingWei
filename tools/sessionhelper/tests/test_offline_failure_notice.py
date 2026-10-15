"""Ordinary reliable failure notices: real local helper, persistent inbox, no production."""
import asyncio
import importlib.util
from pathlib import Path
import json
import os
import sqlite3
import subprocess
import time
import unittest
from unittest import mock

# Load the existing local fixture by exact path, independent of test invocation cwd.
_fixture_spec = importlib.util.spec_from_file_location(
    "offline_notice_reliable_fixture", Path(__file__).with_name("test_reliable_delivery_reply.py"))
reliable = importlib.util.module_from_spec(_fixture_spec)
_fixture_spec.loader.exec_module(reliable)
from sessionhelper.routing import classify_reply_decision, is_no_mirror_envelope


@unittest.skipUnless(reliable._WS_OK, "needs websockets>=15")
class OfflineFailureNoticeCompatibility(unittest.TestCase):
    tearDown = reliable.DurableWorkerRepliesToReliableDelivery.tearDown
    _wait = reliable.DurableWorkerRepliesToReliableDelivery._wait
    _acks = reliable.DurableWorkerRepliesToReliableDelivery._acks
    _replies = reliable.DurableWorkerRepliesToReliableDelivery._replies

    def setUp(self):
        real_popen = subprocess.Popen
        # Instrument the real adapter in the child, not the inbox/delivery paths.
        # Only a fixed effect marker is saved; no body or credentials are logged.
        script = """
import os,runpy
from sessionhelper.app import EchoAdapter
original=EchoAdapter.handle
def counted(self,env):
    with open(os.environ['TEST_EFFECT_PATH'],'a') as f: f.write('effect\\n')
    return original(self,env)
EchoAdapter.handle=counted
runpy.run_module('sessionhelper',run_name='__main__')
"""
        def launch(args, **kwargs):
            if args[1:] == ["-m", "sessionhelper"]:
                args = [args[0], "-c", script]
                kwargs["env"] = dict(kwargs["env"], TEST_EFFECT_PATH=os.path.join(self.tmp, "effects"))
                self.restart_args, self.restart_kwargs = args, kwargs
            return real_popen(args, **kwargs)
        with mock.patch.object(subprocess, "Popen", side_effect=launch):
            reliable.DurableWorkerRepliesToReliableDelivery.setUp(self)

    def notice(self):
        return {"id": "dw-dlv-failure-fixture", "from": "workpulse#u_owner", "to": "seat#u_owner",
                "body": "【系统投递失败通知】\n原消息未能完成投递；原因码：delivery_expired。本通知不要求回复。",
                "ts": int(time.time()), "meta": {"type": "delivery", "delivery_version": 1,
                "delivery_id": "dw-dlv-failure-fixture", "ack_required": True,
                "source_message_id": "failed-delivery", "failed_delivery_id": "failed-delivery",
                "kind": "reply", "reply_policy": "no_reply", "conversation_terminal": True,
                "suppress_reply_origin": True, "no_mirror": True}}

    def send_notice(self, env):
        asyncio.run_coroutine_threadsafe(self.ws.send(json.dumps(env)), self.loop).result(5)

    def test_old_protocol_injects_once_after_lost_ack_and_persistent_restart(self):
        self.assertTrue(self.connected.wait(20))
        self._wait(lambda: any(f.get("meta", {}).get("type") == "session_capabilities" for f in self.frames), 20, "capabilities")
        env = self.notice()
        self.assertFalse(is_no_mirror_envelope(env), "ordinary delivery must not be discarded as system/control")
        self.assertFalse(classify_reply_decision(env).allow, "rollback-era reply semantics must suppress return")
        self.assertNotIn("@", env["body"])
        self.assertNotIn("#", env["body"])
        self.send_notice(env)
        self._wait(lambda: self._acks(env["id"]).count("processed") >= 1, 20, "first processed ACK")
        # Treat the first ACK as lost at the Hub: retry the exact persisted ID.
        self.send_notice(env)
        self._wait(lambda: self._acks(env["id"]).count("processed") >= 2, 20, "duplicate processed ACK")
        with open(os.path.join(self.tmp, "effects")) as f:
            self.assertEqual(f.read().splitlines(), ["effect"])
        self.helper.kill()
        self.helper.wait(timeout=10)
        before = sum(f.get("meta", {}).get("type") == "session_capabilities" for f in self.frames)
        self.helper = subprocess.Popen(self.restart_args, **self.restart_kwargs)
        self._wait(lambda: sum(f.get("meta", {}).get("type") == "session_capabilities" for f in self.frames) > before,
                   20, "restarted helper capabilities")
        self.send_notice(env)
        self._wait(lambda: self._acks(env["id"]).count("processed") >= 3, 20, "persistent duplicate processed ACK")
        with open(os.path.join(self.tmp, "effects")) as f:
            self.assertEqual(f.read().splitlines(), ["effect"])
        with sqlite3.connect(os.path.join(self.tmp, "inbox.db")) as db:
            self.assertEqual(db.execute("SELECT attempts,state FROM delivery_inbox WHERE delivery_id=?", (env["id"],)).fetchall(),
                             [(1, "processed")])
        self.assertFalse(any(f.get("meta", {}).get("kind") == "reply" for f in self.frames), "notice generated a business return")
        self.assertFalse(any(env["body"] in str(f.get("body") or "") for f in self.frames), "notice echoed back")


if __name__ == "__main__":
    unittest.main()
