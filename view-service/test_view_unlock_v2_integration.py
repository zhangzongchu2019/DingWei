import hmac
import json
import os
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest import mock

import view_service as vs
from view_page_store import ViewPageStore


class _FakeHubHandler(BaseHTTPRequestHandler):
    secret = b""
    received = []

    def log_message(self, *_args):
        pass

    def do_POST(self):
        raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
        timestamp, nonce = self.headers["X-DW-Timestamp"], self.headers["X-DW-Nonce"]
        expected = vs._control_signature(
            self.secret, "POST", self.path, timestamp, nonce, raw,
            direction="view-service-to-hub/v1")
        if self.headers.get("X-DW-VS-Target") != "canary" or not hmac.compare_digest(
                self.headers.get("X-DW-Signature", ""), expected):
            self.send_error(401)
            return
        self.received.append(json.loads(raw))
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(b'{"ok":true}')


class ViewUnlockV2IntegrationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.forward = b"forward-0123456789abcdef0123456789"
        self.reverse = b"reverse-0123456789abcdef0123456789"
        self.forward_file = os.path.join(self.tmp.name, "forward.key")
        self.reverse_file = os.path.join(self.tmp.name, "reverse.key")
        for path, secret in ((self.forward_file, self.forward), (self.reverse_file, self.reverse)):
            with open(path, "wb") as fh:
                fh.write(secret)
        self.store = ViewPageStore(os.path.join(self.tmp.name, "pages.db"), now=time.time)
        self.old_store = vs._page_store
        vs._page_store = self.store
        _FakeHubHandler.secret = self.reverse
        _FakeHubHandler.received = []
        self.hub = ThreadingHTTPServer(("127.0.0.1", 0), _FakeHubHandler)
        self.hub_thread = threading.Thread(target=self.hub.serve_forever, daemon=True)
        self.hub_thread.start()
        self.view = vs.ThreadingHTTPServer(("127.0.0.1", 0), vs._Handler)
        self.view_thread = threading.Thread(target=self.view.serve_forever, daemon=True)
        self.patches = mock.patch.multiple(
            vs, VIEW_UNLOCK_V2=True, VIEW_TARGET_NAME="canary",
            HUB_TO_VS_SECRET_FILE=self.forward_file, VS_TO_HUB_SECRET_FILE=self.reverse_file,
            HUB_INTERNAL=f"http://127.0.0.1:{self.hub.server_port}", DEFAULT_SESSION="alice-developer-0000",
        )
        self.patches.start()
        self.view_thread.start()

    def tearDown(self):
        self.view.shutdown()
        self.view.server_close()
        self.hub.shutdown()
        self.hub.server_close()
        self.view_thread.join(timeout=2)
        self.hub_thread.join(timeout=2)
        self.patches.stop()
        vs._page_store = self.old_store
        self.store.close()
        self.tmp.cleanup()

    def _post(self, path, payload, headers=None):
        raw = json.dumps(payload, separators=(",", ":")).encode()
        request = urllib.request.Request(
            f"http://127.0.0.1:{self.view.server_port}{path}", raw,
            {"Content-Type": "application/json", **(headers or {})}, method="POST")
        try:
            with urllib.request.urlopen(request, timeout=2) as response:
                return response.status, json.loads(response.read() or b"{}")
        except urllib.error.HTTPError as error:
            return error.code, json.loads(error.read() or b"{}")

    def _new_page(self):
        status, page = self._post("/page", {})
        self.assertEqual(status, 201)
        return page

    def _unlock(self, page, command_id, nonce):
        payload = {"command_id": command_id, "request_id": command_id,
                   "session": "alice-developer-0000", "code": page["code"],
                   "owner_key": "alice", "sender_open_id": "ou_alice",
                   "granted_at": "2027-01-16T00:00:00Z"}
        raw = json.dumps(payload, separators=(",", ":")).encode()
        timestamp = str(int(time.time()))
        signature = vs._control_signature(
            self.forward, "POST", vs.CONTROL_UNLOCK_PATH, timestamp, nonce, raw)
        return self._post(vs.CONTROL_UNLOCK_PATH, payload, {
            "X-DW-VS-Target": "canary", "X-DW-Timestamp": timestamp,
            "X-DW-Nonce": nonce, "X-DW-Request-ID": command_id,
            "X-DW-Signature": signature,
        })

    def test_multi_page_unlock_input_replay_and_restart(self):
        page_a, page_b, page_c = self._new_page(), self._new_page(), self._new_page()
        self.assertEqual(len({page_a["page_id"], page_b["page_id"], page_c["page_id"]}), 3)
        self.assertEqual(self._unlock(page_a, "unlock-a", "0" * 32)[0], 200)
        self.assertEqual(self._unlock(page_b, "unlock-b", "1" * 32)[0], 200)
        self.assertEqual(self._unlock(page_a, "unlock-a", "0" * 32)[0], 409)

        input_a = "a" * 32
        input_b = "b" * 32
        for page, request_id in ((page_a, input_a), (page_b, input_b)):
            status, result = self._post("/input", {
                "text": f"hello {request_id}", "page_id": page["page_id"],
                "page_token": page["page_token"], "request_id": request_id,
            })
            self.assertEqual((status, result["ok"]), (200, True))
        status, result = self._post("/input", {
            "text": "must reject", "page_id": page_c["page_id"],
            "page_token": page_c["page_token"], "request_id": "c" * 32,
        })
        # /input 失败返 403(与 /page/disconnect、/page/resume 同家法);此前返 200
        # 导致前端只查 res.ok 时静默假成功。
        self.assertEqual((status, result["ok"]), (403, False))
        self.assertEqual([item["request_id"] for item in _FakeHubHandler.received], [input_a, input_b])

        # VS restart: SQLite restores both independent grants and page codes.
        self.store.close()
        self.store = ViewPageStore(os.path.join(self.tmp.name, "pages.db"), now=time.time)
        vs._page_store = self.store
        for page in (page_a, page_b):
            status, resumed = self._post("/page/resume", {
                "page_id": page["page_id"], "page_token": page["page_token"],
            })
            self.assertEqual(status, 200)
            self.assertEqual(resumed["code"], page["code"])
            self.assertTrue(resumed["can_write"])
        status, resumed_c = self._post("/page/resume", {
            "page_id": page_c["page_id"], "page_token": page_c["page_token"],
        })
        self.assertEqual(status, 200)
        self.assertFalse(resumed_c["can_write"])


if __name__ == "__main__":
    unittest.main()
