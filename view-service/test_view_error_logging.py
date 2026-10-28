import contextlib
import http.client
import io
import json
import os
import tempfile
import threading
import unittest
from http.server import ThreadingHTTPServer
from unittest import mock

import view_service as vs


class ViewErrorLoggingTest(unittest.TestCase):
    def request(self, server, path):
        conn = http.client.HTTPConnection(*server.server_address, timeout=3)
        conn.request("GET", path, headers={"X-DW-Session": "test"})
        response = conn.getresponse()
        result = response.status, response.read()
        conn.close()
        return result

    def test_permission_000_returns_500_and_logs_type_and_paths(self):
        self.assertNotEqual(os.geteuid(), 0, "run as non-root: permission 000 must really deny open")
        with tempfile.TemporaryDirectory() as tmp:
            renderer = os.path.join(tmp, "renderer.html")
            with open(renderer, "w") as fh:
                fh.write("renderer")
            os.chmod(renderer, 0)
            output = io.StringIO()
            server = ThreadingHTTPServer(("127.0.0.1", 0), vs._Handler)
            thread = threading.Thread(target=server.serve_forever)
            thread.start()
            try:
                with mock.patch.object(vs, "RENDERER_PATH", renderer), contextlib.redirect_stderr(output):
                    status, body = self.request(server, "/?token=do-not-log-this")
                self.assertEqual(status, 500)
                records = [json.loads(line) for line in output.getvalue().splitlines()]
                self.assertTrue(any(r["exception"] == "PermissionError" and r["target_path"] == renderer and r["request_path"] == "/" for r in records), records)
                self.assertTrue(any(r.get("status") == 500 for r in records), records)
                self.assertNotIn("do-not-log-this", output.getvalue())
                self.assertNotIn(renderer.encode(), body)
            finally:
                server.shutdown()
                thread.join()
                server.server_close()
                os.chmod(renderer, 0o600)

    def test_json_5xx_logs_without_sensitive_response_body(self):
        handler = object.__new__(vs._Handler)
        handler.path = "/status?secret=hidden"
        handler.request_version = "HTTP/1.1"
        handler.wfile = io.BytesIO()
        handler.requestline = "GET /status HTTP/1.1"
        output = io.StringIO()
        with contextlib.redirect_stderr(output):
            handler._json_response(502, {"private": "never-log-value"})
        record = json.loads(output.getvalue())
        self.assertEqual(record["status"], 502)
        self.assertNotIn("never-log-value", output.getvalue())


if __name__ == "__main__":
    unittest.main()
