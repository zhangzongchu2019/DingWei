"""app.py wiring for SH_ENROLL: handshake failures and close codes, and the legacy path left alone.

Needs the `websockets` package (present in the helper venv); skipped when it is missing.
"""
import asyncio
import os
import tempfile
import unittest
from unittest import mock
from urllib.parse import parse_qs, urlparse

try:
    import websockets
    from websockets.datastructures import Headers
    from websockets.exceptions import ConnectionClosedError, InvalidStatus
    from websockets.frames import Close
    from websockets.http11 import Response
except ImportError:  # pragma: no cover
    websockets = None

from sessionhelper import enroll
from sessionhelper.app import SessionHelper
from sessionhelper.config import load_config

SECRET = "dwa_" + "cd" * 32
BASE = {"SH_SESSION_NAME": "dev55", "SH_KEY_ID": "FB-2642", "SH_SECRET": "legacy-secret",
        "SH_WS_BASE": "wss://hub.example:55055", "SH_TOOL": "claude"}


class Stop(Exception):
    pass


def refused(status, body):
    return InvalidStatus(Response(status, "x", Headers(), body))


@unittest.skipIf(websockets is None, "websockets not installed")
class EnrollWiringTest(unittest.TestCase):
    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.dir = self._td.name
        self.cred_path = os.path.join(self.dir, enroll.CREDENTIAL_FILE)
        enroll.save_credential(self.dir, enroll.Credential("Dev55", SECRET))
        self.says = []

    def helper(self, enrolled=True):
        h = SessionHelper(load_config(BASE))
        h._enroll_say = self.says.append
        if enrolled:
            h.enroll_credential, h.enroll_state_dir = enroll.Credential("Dev55", SECRET), self.dir
        return h

    def run_loop(self, helper, exc):
        async def go():
            with mock.patch.object(helper, "_run_once", side_effect=exc), \
                 mock.patch("asyncio.sleep", side_effect=Stop):
                await helper.run_forever()
        return asyncio.run(go())

    # -- connection shape
    def test_legacy_connection_is_unchanged(self):
        h = self.helper(enrolled=False)
        url, headers = h._connection()
        self.assertEqual(url, h.cfg.ws_url)
        self.assertIn("key_id=FB-2642", url)
        self.assertEqual(headers, {"Authorization": "Bearer legacy-secret"})

    def test_enrolled_connection_carries_only_the_credential(self):
        url, headers = self.helper()._connection()
        parts = urlparse(url)
        self.assertEqual((parts.scheme, parts.netloc, parts.path), ("wss", "hub.example:55055", "/ws/agent"))
        self.assertEqual(set(parse_qs(parts.query)), {"version", "tool", "os"})
        for leak in ("FB-2642", "dev55", "Dev55", "legacy-secret", "key_id"):
            self.assertNotIn(leak, url)
        self.assertEqual(headers, {"Authorization": f"Bearer {SECRET}"})

    # -- handshake refused
    def test_revoked_at_handshake_is_fatal_and_removes_the_credential(self):
        with self.assertRaises(enroll.CredentialRevoked):
            self.run_loop(self.helper(), refused(401, b'{"error":"credential_revoked"}'))
        self.assertFalse(os.path.exists(self.cred_path))

    def test_invalid_and_already_connected_are_fatal_and_keep_the_credential(self):
        for status, body, exc in ((401, b'{"error":"credential_invalid"}', enroll.CredentialInvalid),
                                  (409, b'{"error":"already_connected"}', enroll.AlreadyConnected)):
            with self.subTest(body=body):
                with self.assertRaises(exc):
                    self.run_loop(self.helper(), refused(status, body))
                self.assertTrue(os.path.exists(self.cred_path))

    def test_other_refusals_keep_reconnecting(self):
        with self.assertRaises(Stop):  # reached asyncio.sleep, i.e. the normal retry path
            self.run_loop(self.helper(), refused(502, b"<html>"))
        self.assertTrue(os.path.exists(self.cred_path))

    def test_legacy_mode_ignores_the_new_error_codes(self):
        with self.assertRaises(Stop):
            self.run_loop(self.helper(enrolled=False), refused(401, b'{"error":"credential_revoked"}'))
        self.assertTrue(os.path.exists(self.cred_path), "the legacy path must never delete a credential file")

    # -- closed while online
    def test_close_4401_credential_revoked_is_fatal_and_removes_the_credential(self):
        with self.assertRaises(enroll.CredentialRevoked):
            self.run_loop(self.helper(), ConnectionClosedError(Close(4401, "credential_revoked"), None))
        self.assertFalse(os.path.exists(self.cred_path))

    def test_other_closes_keep_reconnecting(self):
        for code, reason in ((1006, ""), (4401, "something_else"), (4400, "credential_revoked")):
            with self.subTest(code=code):
                with self.assertRaises(Stop):
                    self.run_loop(self.helper(), ConnectionClosedError(Close(code, reason), None))
                self.assertTrue(os.path.exists(self.cred_path))

    def test_legacy_mode_ignores_close_4401(self):
        with self.assertRaises(Stop):
            self.run_loop(self.helper(enrolled=False), ConnectionClosedError(Close(4401, "credential_revoked"), None))
        self.assertTrue(os.path.exists(self.cred_path))


if __name__ == "__main__":
    unittest.main()
