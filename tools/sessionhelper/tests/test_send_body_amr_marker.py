import json
import os
import sys
import unittest
from unittest import mock


sys.path.insert(0, os.path.dirname(os.path.dirname(__file__)))
from sessionhelper import send_dingwei
from sessionhelper.ipc_listener import extract_reply_origin_meta


class FakeSocket:
    def __init__(self, *args, **kwargs):
        self.sent = b""

    def settimeout(self, timeout):
        pass

    def connect(self, path):
        pass

    def sendall(self, data):
        self.sent += data

    def recv(self, size):
        return b'{"ok":true,"outcome":{},"request_id":"r"}\n'

    def close(self):
        pass


class SendBodyAMRMarkerTest(unittest.TestCase):
    def test_send_py_marks_only_its_ipc_request(self):
        sock = FakeSocket()
        with mock.patch.object(send_dingwei.socket, "socket", return_value=sock):
            send_dingwei.ipc_send("target", "@other body")
        payload = json.loads(sock.sent)
        self.assertIs(payload["ipc_body_amr_v1"], True)
        self.assertEqual(
            extract_reply_origin_meta(payload),
            {"ipc_body_amr_v1": True},
        )

    def test_unmarked_mcp_request_keeps_literal_body_semantics(self):
        self.assertEqual(
            extract_reply_origin_meta({"type": "mcp_send", "to": "target", "body": "@other body"}),
            {},
        )


if __name__ == "__main__":
    unittest.main()
