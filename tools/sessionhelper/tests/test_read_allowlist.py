import tempfile
import unittest
from pathlib import Path

from sessionhelper.read_allowlist import authorized_envelope, execute, load, matches


class ReadAllowlistTest(unittest.TestCase):
    def test_only_authenticated_owner_view_delivery_can_manage(self):
        envelope = {"from": "workpulse#u_zhangzongchu", "meta": {
            "source_kind": "user", "auth_mode": "view_page_v2", "console_principal_id": "zhangzongchu"}}
        self.assertTrue(authorized_envelope(envelope, "u_zhangzongchu", "driver"))
        self.assertFalse(authorized_envelope({**envelope, "from": "other#u_zhangzongchu"}, "u_zhangzongchu", "driver"))
        self.assertFalse(authorized_envelope({**envelope, "meta": {**envelope["meta"], "console_principal_id": "other"}}, "u_zhangzongchu", "driver"))
        self.assertFalse(authorized_envelope(envelope, "u_zhangzongchu", "cli"))

    def test_defaults_and_exact_domain_boundary(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "allowlist.json"
            self.assertTrue(matches("dw123.cmjrt.com", path))
            self.assertTrue(matches("bucket.myqcloud.com", path))
            self.assertTrue(matches("d.szuma.net", path))
            self.assertFalse(matches("evilcmjrt.com", path))
            self.assertFalse(matches("cmjrt.com.evil.example", path))

    def test_management_add_remove_persists(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "allowlist.json"
            execute("/read-allowlist add example.org", path)
            self.assertTrue(matches("files.example.org", path))
            self.assertIn("example.org", load(path))
            execute("/read-allowlist remove example.org", path)
            self.assertFalse(matches("files.example.org", path))
            with self.assertRaises(ValueError):
                execute("/read-allowlist add example.org;id", path)


if __name__ == "__main__":
    unittest.main()
