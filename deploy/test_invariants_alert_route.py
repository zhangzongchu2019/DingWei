#!/usr/bin/env python3
"""Shape guard for the runtime alert-route invariant.

The runtime invariant deliberately reads the workpulse process environment and
prints only counts/booleans.  This test prevents deleting it or weakening it
into checking a stale configuration file.
"""

import json
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
CONFIG = ROOT / "deploy" / "invariants-dingwei-gray.json"
GUARD_ID = "alert-route-stays-wired"
REQUIRED_KEYS = {
    "WP_NOTIFY_SOCKET",
    "WP_ALERT_CHAT_ENTITY",
    "WP_ALERT_BOT_CHANNEL",
    "WP_ALERT_CHAT_TYPE",
}


class AlertRouteInvariantTest(unittest.TestCase):
    def test_runtime_guard_covers_all_keys_and_unix_socket_without_values(self):
        config = json.loads(CONFIG.read_text(encoding="utf-8"))
        matches = [item for item in config["assertions"] if item.get("id") == GUARD_ID]
        self.assertEqual(len(matches), 1, f"missing or duplicate {GUARD_ID}")
        command = matches[0]["cmd"]

        self.assertIn("/proc/", command, "must inspect the running workpulse process")
        self.assertIn("supervisorctl pid workpulse", command)
        self.assertIn("stat.S_ISSOCK", command)
        for key in REQUIRED_KEYS:
            self.assertIn(key, command)

        # The command may report shape, never the environment map or socket path.
        self.assertNotIn("print(env", command)
        self.assertNotIn("print(sock", command)
        self.assertIn("configured=%d/4 socket_unix=%s", command)


if __name__ == "__main__":
    unittest.main()
