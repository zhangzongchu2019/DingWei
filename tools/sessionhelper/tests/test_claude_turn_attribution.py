"""Claude terminal frames fence delivery attribution for later assistant text."""

import json
import os
from pathlib import Path
import queue
import sys
import tempfile
import threading
import unittest
from unittest import mock

from sessionhelper.claudeDriver import ClaudeProtocolDriver


FAKE_CLAUDE = r'''import json, os, sys
leaks = sorted(k for k in os.environ if (k.startswith("SH_") or k.startswith("DINGWEI_")) and k != "SH_MCP_IPC_SOCK")
if leaks:
    raise SystemExit("unexpected inherited helper environment names: " + ",".join(leaks))
def emit(frame):
    sys.stdout.write(json.dumps(frame) + "\n")
    sys.stdout.flush()
emit({"type":"system","subtype":"init","session_id":"fake-claude-session"})
turn = 0
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("type") != "user":
        continue
    turn += 1
    emit({"type":"user","isReplay":True,"message":{"role":"user","content":[]}})
    emit({"type":"assistant","message":{"content":[{"type":"text","text":"reply-"+str(turn)}]}})
    emit({"type":"result","subtype":"success","is_error":False,"num_turns":1})
    if turn == 1:
        # Known defect: text after the terminal frame must not inherit turn 1.
        emit({"type":"assistant","message":{"content":[{"type":"text","text":"late-after-result"}]}})
'''


class ClaudeTurnAttributionTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix="claude-turn-fence-")
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.fake_cli = root / "fake_claude.py"
        self.fake_cli.write_text(FAKE_CLAUDE, encoding="utf-8")

        # Keep the caller's ordinary runtime variables, but remove inherited
        # helper/seat identity and credentials. The sole SH_* value is a fresh
        # socket path in this test's private temporary directory.
        clean_env = {
            key: value for key, value in os.environ.items()
            if not key.startswith(("SH_", "DINGWEI_"))
        }
        clean_env["SH_MCP_IPC_SOCK"] = str(root / "sessionhelper.sock")
        self.env_patch = mock.patch.dict(os.environ, clean_env, clear=True)
        self.env_patch.start()
        self.addCleanup(self.env_patch.stop)

        self.driver = ClaudeProtocolDriver(
            cmd=[sys.executable, str(self.fake_cli)],
            cwd=str(root),
            store_path=str(root / "delivery.sqlite3"),
            env={"SH_MCP_IPC_SOCK": clean_env["SH_MCP_IPC_SOCK"]},
        )
        self.driver.start()
        self.events = queue.Queue()

        def collect():
            for event in self.driver.events():
                self.events.put(event)

        self.collector = threading.Thread(target=collect, daemon=True)
        self.collector.start()

        def stop_driver():
            self.driver.stop()
            self.collector.join(timeout=2)

        self.addCleanup(stop_driver)

    def _next_events(self, count):
        return [self.events.get(timeout=5) for _ in range(count)]

    def _complete_first_turn(self):
        receipt = self.driver.deliver("delivery-1", "first prompt")
        self.assertEqual(receipt.state, "accepted")
        observed = self._next_events(3)
        self.assertEqual(
            [event.kind for event in observed],
            ["assistant_text", "state_change", "assistant_text"],
        )
        self.assertEqual(observed[1].data.get("change"), "turn_completed")
        self.assertEqual(observed[2].text, "late-after-result")
        return observed

    def test_text_after_terminal_has_no_completed_delivery_attribution(self):
        observed = self._complete_first_turn()
        late = observed[2]
        self.assertEqual(
            (late.delivery_id, late.turn_id),
            ("", ""),
            "post-turn assistant text must not retain the completed delivery's attribution",
        )
        with self.driver._active_lock:
            self.assertIsNone(self.driver._active)

    def test_next_delivery_text_uses_new_delivery_and_turn_ids(self):
        first = self._complete_first_turn()
        first_turn_id = first[0].turn_id
        # deliver() waits for the terminal fence and then installs the next
        # state. A late old frame after that installation remains outside fix A.
        receipt = self.driver.deliver("delivery-2", "second prompt")
        self.assertEqual(receipt.state, "accepted")
        observed = self._next_events(2)
        self.assertEqual([event.kind for event in observed], ["assistant_text", "state_change"])
        self.assertEqual(observed[1].data.get("change"), "turn_completed")
        second = observed[0]
        second_turn_id = self.driver._store.get("delivery-2").turn_id
        self.assertEqual(second.delivery_id, "delivery-2")
        self.assertEqual(second.turn_id, second_turn_id)
        self.assertNotEqual(second.turn_id, first_turn_id)


if __name__ == "__main__":
    unittest.main(verbosity=2)
