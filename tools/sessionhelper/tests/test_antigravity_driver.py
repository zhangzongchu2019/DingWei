import os
import sys
import tempfile
import threading
import time
import unittest

from sessionhelper.antigravityDriver import AntigravityProtocolDriver


FIXTURE = '''import json,sys
for line in sys.stdin:
    request=json.loads(line); method=request.get("method"); rpc_id=request.get("id")
    if method=="initialize": result={"protocolVersion":1}
    elif method=="session/new": result={"sessionId":"fixture-session"}
    elif method=="session/prompt":
        print(json.dumps({"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"fixture-session","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"HELLO"}}}}),flush=True)
        result={"stopReason":"end_turn"}
    else: continue
    print(json.dumps({"jsonrpc":"2.0","id":rpc_id,"result":result}),flush=True)
'''


class AntigravityDriverTest(unittest.TestCase):
    def test_first_delivery_starts_acp_without_reader_deadlock(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = os.path.join(directory, "acp.py")
            with open(fixture, "w", encoding="utf-8") as handle:
                handle.write(FIXTURE)
            driver = AntigravityProtocolDriver(os.path.join(directory, "ledger.sqlite3"), directory,
                                                [sys.executable, fixture])
            try:
                receipt = driver.deliver("message-1", "test")
                self.assertIn(receipt.state, ("accepted", "done"))
            finally:
                driver.stop()

    def test_turn_events_precede_done_and_duplicate_is_not_submitted(self):
        with tempfile.TemporaryDirectory() as directory:
            fixture = os.path.join(directory, "acp.py")
            with open(fixture, "w", encoding="utf-8") as handle:
                handle.write(FIXTURE)
            driver = AntigravityProtocolDriver(os.path.join(directory, "ledger.sqlite3"), directory,
                                                [sys.executable, fixture])
            driver.start()
            events = []
            reader = threading.Thread(target=lambda: events.extend(driver.events()), daemon=True)
            reader.start()
            try:
                receipt = driver.deliver("message-1", "test")
                deadline = time.monotonic() + 2
                while driver.poll_receipt("message-1").state != "done" and time.monotonic() < deadline:
                    time.sleep(.01)
                self.assertIn(receipt.state, ("accepted", "done"))
                self.assertEqual(driver.poll_receipt("message-1").state, "done")
                self.assertEqual(driver.deliver("message-1", "test").state, "done")
                self.assertEqual(driver.deliver("message-1", "different").state, "failed")
                self.assertFalse(driver.interrupt_delivery("message-1", "stale-turn"))
                deadline = time.monotonic() + 2
                while len(events) < 3 and time.monotonic() < deadline:
                    time.sleep(.01)
                self.assertEqual([e.kind for e in events], ["state_change", "assistant_text", "state_change"])
                self.assertEqual(events[1].text, "HELLO")
                self.assertEqual(events[2].data["status"], "completed")
            finally:
                driver.stop()
                reader.join(2)
            restarted = AntigravityProtocolDriver(os.path.join(directory, "ledger.sqlite3"), directory,
                                                  [sys.executable, fixture])
            self.assertEqual(restarted.deliver("message-1", "test").state, "done")
            self.assertFalse(restarted.health())


if __name__ == "__main__":
    unittest.main()
