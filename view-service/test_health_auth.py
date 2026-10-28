import concurrent.futures
import hashlib
import hmac
import http.client
import json
import os
import tempfile
import threading
import time
import unittest
from http.server import ThreadingHTTPServer
from unittest import mock

import view_service as vs


class HealthAuthTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.key = b"test-monitor-only-key-at-least-32-bytes"
        self.keyfile = os.path.join(self.tmp.name, "monitor.key")
        with open(self.keyfile, "wb") as fh:
            fh.write(self.key)
        os.chmod(self.keyfile, 0o600)
        self.patches = [mock.patch.object(vs, "HEALTH_SECRET_FILE", self.keyfile),
                        mock.patch.object(vs, "HEALTH_TARGET", "staging-view")]
        for patch in self.patches:
            patch.start()
        with vs._health_nonce_lock:
            vs._health_nonces.clear()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), vs._Handler)
        self.thread = threading.Thread(target=self.server.serve_forever)
        self.thread.start()

    def tearDown(self):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()
        for patch in self.patches:
            patch.stop()
        self.tmp.cleanup()

    def headers(self, nonce="a" * 32, timestamp=None, key=None, target="staging-view"):
        timestamp = str(int(time.time()) if timestamp is None else timestamp)
        message = "\n".join(("dingwei-health/v1", "GET", vs.HEALTH_PATH, target, timestamp, nonce))
        signature = hmac.new(key or self.key, message.encode(), hashlib.sha256).hexdigest()
        return {"X-DW-Health-Timestamp": timestamp, "X-DW-Health-Nonce": nonce,
                "X-DW-Health-Signature": signature}

    def request(self, headers, path=None):
        conn = http.client.HTTPConnection(*self.server.server_address, timeout=3)
        conn.request("GET", path or vs.HEALTH_PATH, headers=headers)
        response = conn.getresponse()
        status, body = response.status, response.read()
        conn.close()
        return status, json.loads(body)

    def test_valid_response_allowlist(self):
        status, payload = self.request(self.headers())
        self.assertEqual(status, 200)
        self.assertEqual(payload, {"health": "ok", "version": "0.2.3.15", "target": "staging-view"})

    def test_wrong_signature(self):
        self.assertEqual(self.request(self.headers(key=b"wrong-key"))[0], 401)
        self.assertEqual(self.request(self.headers())[0], 200, "bad signatures cannot reserve nonces")

    def test_expired(self):
        self.assertEqual(self.request(self.headers(timestamp=int(time.time()) - 61))[0], 401)

    def test_future(self):
        self.assertEqual(self.request(self.headers(timestamp=int(time.time()) + 61))[0], 401)

    def test_replay(self):
        headers = self.headers()
        self.assertEqual(self.request(headers)[0], 200)
        self.assertEqual(self.request(headers)[0], 401)

    def test_missing_key(self):
        os.unlink(self.keyfile)
        self.assertEqual(self.request(self.headers())[0], 401)

    def test_wrong_target(self):
        self.assertEqual(self.request(self.headers(target="prod-view"))[0], 401)

    def test_query_not_accepted(self):
        self.assertEqual(self.request(self.headers(), vs.HEALTH_PATH + "?x=y")[0], 401)

    def test_malformed_or_missing_headers(self):
        self.assertEqual(self.request({})[0], 401)
        self.assertEqual(self.request(self.headers(nonce="bad"))[0], 401)

    def test_concurrent_replay(self):
        headers = self.headers()
        with concurrent.futures.ThreadPoolExecutor(max_workers=8) as pool:
            statuses = list(pool.map(lambda _: self.request(headers)[0], range(16)))
        self.assertEqual(statuses.count(200), 1)
        self.assertEqual(statuses.count(401), 15)

    def test_missing_target(self):
        with mock.patch.object(vs, "HEALTH_TARGET", ""):
            self.assertEqual(self.request(self.headers())[0], 401)

    def test_version_matches_release_file(self):
        with open(os.path.join(vs.BASE_DIR, "..", "VERSION")) as fh:
            self.assertEqual(vs.HEALTH_VERSION, fh.read().strip())

    def test_key_size(self):
        for size in (4, 4097):
            with open(self.keyfile, "wb") as fh:
                fh.write(b"k" * size)
            self.assertEqual(self.request(self.headers())[0], 401)


if __name__ == "__main__":
    unittest.main()
