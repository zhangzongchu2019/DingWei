import asyncio
import json
import unittest

from sessionhelper.ipc_listener import _handle_connection


class CapturePipe:
    def __init__(self, request):
        self.line = (json.dumps(request) + "\n").encode()
        self.written = b""

    async def readline(self):
        line, self.line = self.line, b""
        return line

    def write(self, data):
        self.written += data

    async def drain(self):
        pass


async def unused_send(_to, _body, _meta):
    raise AssertionError("send handler called by file request")


class FileIPCContractTest(unittest.TestCase):
    def run_request(self, request, handler=None):
        pipe = CapturePipe(request)
        asyncio.run(_handle_connection(pipe, pipe, unused_send, file_handler=handler))
        return json.loads(pipe.written)

    def test_head_returns_metadata_without_capability(self):
        async def handler(file_ref, method, byte_range):
            self.assertEqual((file_ref, method, byte_range), ("dwfile:f1", "HEAD", ""))
            return {"file_ref": file_ref, "size": 12, "sha256": "abc", "range": True}

        response = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "HEAD"}, handler)
        self.assertTrue(response["ok"])
        self.assertNotIn("token", json.dumps(response).lower())
        self.assertNotIn("capability", json.dumps(response).lower())

    def test_get_requires_one_explicit_range(self):
        async def handler(file_ref, method, byte_range):
            return {"file_ref": file_ref, "content_base64": "YQ==", "content_range": byte_range, "sha256": "abc", "total_size": 10}

        valid = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "GET", "range": "bytes=0-0"}, handler)
        self.assertTrue(valid["ok"])
        for value in ("", "bytes=0-1,3-4", "items=0-1", "bytes=x-1"):
            rejected = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "GET", "range": value}, handler)
            self.assertFalse(rejected["ok"])

    def test_unsafe_handler_result_is_rejected_without_echo(self):
        async def handler(_file_ref, _method, _byte_range):
            return {"file_ref": "dwfile:f1", "token": "raw-sensitive-value"}

        response = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "HEAD"}, handler)
        self.assertEqual(response, {"ok": False, "error": "unsafe file response"})
        self.assertNotIn("raw-sensitive-value", json.dumps(response))

    def test_incomplete_or_wrong_file_result_is_rejected(self):
        for result in ({}, {"file_ref": "dwfile:other", "size": 1, "sha256": "abc"}):
            async def handler(_file_ref, _method, _byte_range, value=result):
                return value

            response = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "HEAD"}, handler)
            self.assertEqual(response, {"ok": False, "error": "unsafe file response"})

    def test_handler_exception_is_not_reflected(self):
        async def handler(_file_ref, _method, _byte_range):
            raise RuntimeError("Authorization: Bearer raw-sensitive-value")

        response = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "HEAD"}, handler)
        self.assertEqual(response, {"ok": False, "error": "file access failed"})
        self.assertNotIn("raw-sensitive-value", json.dumps(response))

    def test_file_handler_absent_fails_closed(self):
        response = self.run_request({"type": "file_access_v1", "file_ref": "dwfile:f1", "method": "HEAD"})
        self.assertEqual(response, {"ok": False, "error": "file access unavailable"})


if __name__ == "__main__":
    unittest.main()
