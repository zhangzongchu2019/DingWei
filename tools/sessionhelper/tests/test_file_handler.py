import asyncio
import email.message
import json
import unittest
from unittest import mock

from sessionhelper.app import SessionHelper
from sessionhelper.protocol import AddressBook


FILE_ID = "0123456789abcdef0123456789abcdef"
SHA256 = "a" * 64


class FakeWS:
    def __init__(self, helper, base_url):
        self.helper = helper
        self.base_url = base_url
        self.sent = []

    async def send(self, raw):
        env = json.loads(raw)
        self.sent.append(env)
        request_id = env["meta"]["request_id"]
        self.helper.handle_file_capability_response({"meta": {
            "type": "file_capability_response", "request_id": request_id, "ok": True,
            "token": "secret-ticket", "public_base_url": self.base_url,
        }})


class OneMessageWS:
    def __init__(self, env):
        self.env = env

    def __aiter__(self):
        async def messages():
            yield json.dumps(self.env)
        return messages()


class FakeHTTPResponse:
    def __init__(self, method):
        self.status = 200 if method == "HEAD" else 206
        self.headers = email.message.Message()
        self.headers["X-Content-SHA256"] = SHA256
        self.headers["Accept-Ranges"] = "bytes"
        if method == "HEAD":
            self.headers["Content-Length"] = "4"
        else:
            self.headers["Content-Range"] = "bytes 0-3/4"

    def __enter__(self):
        return self

    def __exit__(self, *_args):
        return False

    def read(self, _limit):
        return b"data"


class FileHandlerBridgeTest(unittest.TestCase):
    def helper(self):
        helper = object.__new__(SessionHelper)
        helper.book = AddressBook("agent", "key", "bot")
        helper.file_ticket_waiters = {}
        return helper

    def test_capability_response_is_consumed_before_cli_path(self):
        async def run():
            helper = self.helper()
            loop = asyncio.get_running_loop()
            waiter = loop.create_future()
            helper.file_ticket_waiters["r1"] = waiter

            async def forbidden(*_args):
                raise AssertionError("file capability reached normal message path")

            async def not_busy(*_args):
                return False

            helper.process_inbound_message = forbidden
            helper.buffer_if_busy = not_busy
            helper.remember_broadcast_mirror_decision = lambda _env: None
            await helper.recv_loop(OneMessageWS({"body": "", "meta": {
                "type": "file_capability_response", "request_id": "r1", "ok": True,
                "token": "must-not-reach-cli", "public_base_url": "https://files.example",
            }}))
            self.assertEqual((await waiter)["token"], "must-not-reach-cli")

        asyncio.run(run())

    def test_head_and_range_use_ticket_only_in_authorization_header(self):
        async def run():
            seen = []
            helper = self.helper()
            ws = FakeWS(helper, "https://files.example")
            handler = helper._make_file_handler(ws)

            def fake_urlopen(request, timeout):
                self.assertEqual(timeout, 15)
                seen.append((request.get_method(), request.get_header("Authorization"),
                             request.get_header("Range"), request.full_url))
                return FakeHTTPResponse(request.get_method())

            with mock.patch("urllib.request.urlopen", side_effect=fake_urlopen):
                head = await handler("dwfile:" + FILE_ID, "HEAD", "")
                get = await handler("dwfile:" + FILE_ID, "GET", "bytes=0-3")
            self.assertEqual(head["size"], 4)
            self.assertEqual(get["content_base64"], "ZGF0YQ==")
            self.assertEqual(seen, [
                ("HEAD", "Bearer secret-ticket", None,
                 "https://files.example/files/ref/" + FILE_ID),
                ("GET", "Bearer secret-ticket", "bytes=0-3",
                 "https://files.example/files/ref/" + FILE_ID),
            ])
            serialized = json.dumps(ws.sent)
            self.assertNotIn("secret-ticket", serialized)
            self.assertEqual(helper.file_ticket_waiters, {})

        asyncio.run(run())


if __name__ == "__main__":
    unittest.main()
