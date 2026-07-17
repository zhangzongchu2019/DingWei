import tempfile
import unittest
from urllib.parse import parse_qs, urlparse

from sessionhelper.config import load_config


BASE = {
    "SH_SESSION_NAME": "developer",
    "SH_KEY_ID": "FB-0000",
    "SH_SECRET": "secret",
}


class DeviceIDTest(unittest.TestCase):
    def test_gate_off_preserves_legacy_url(self):
        cfg = load_config(BASE)
        self.assertFalse(cfg.device_id_v1)
        self.assertNotIn("device_id", parse_qs(urlparse(cfg.ws_url).query))

    def test_gate_on_persists_same_device_across_restart(self):
        with tempfile.TemporaryDirectory() as td:
            env = dict(BASE, SH_DEVICE_ID_V1="1", SH_STATE_DIR=td)
            first = load_config(env)
            restarted = load_config(env)
            self.assertRegex(first.device_id, r"^[0-9a-f]{16}$")
            self.assertEqual(first.device_id, restarted.device_id)
            query = parse_qs(urlparse(first.ws_url).query)
            self.assertEqual(query["device_id_v1"], ["1"])
            self.assertEqual(query["device_id"], [first.device_id])


if __name__ == "__main__":
    unittest.main()
