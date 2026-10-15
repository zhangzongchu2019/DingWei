import unittest
from unittest.mock import patch

from sessionhelper import mcp_shim


class SpecMCPToolTest(unittest.TestCase):
    def body(self):
        return {
            "title": "draft",
            "content_markdown": "body",
            "acceptance": [{"id": "a", "text": "done", "owner_ref": "owner"}],
            "input_anchors": [{"kind": "message", "ref": "m-1"}],
        }

    def test_create_direct_write_without_grant_or_capability_argument(self):
        with patch.object(mcp_shim, "_ipc_request", return_value={"status":"ok","spec_id":"spec-1"}) as ipc:
            self.assertEqual(mcp_shim._dw_spec_create(self.body())["status"], "ok")
        payload = ipc.call_args.args[0]
        self.assertEqual(payload["type"], "spec_write_v1")
        self.assertEqual(payload["action"], "create")
        self.assertNotIn("capability", payload)
        self.assertNotIn("grant_request_id",payload)
        self.assertEqual(payload["protocol"],"direct-v1")

    def test_update_needs_its_own_spec_and_revision(self):
        body = self.body() | {"spec_id": "spec-1", "expected_revision": 2}
        with patch.object(mcp_shim, "_ipc_request", return_value={"status": "ok", "spec_id": "spec-1", "revision": 3}):
            self.assertEqual(mcp_shim._dw_spec_update(body)["revision"], 3)
        with self.assertRaises(ValueError):
            mcp_shim._dw_spec_update(self.body() | {"spec_id": "spec-1"})

    def test_unknown_or_capability_argument_is_rejected(self):
        with self.assertRaises(ValueError):
            mcp_shim._dw_spec_create(self.body() | {"capability": "forbidden"})
