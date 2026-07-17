import os
import tempfile
import threading
import time
import unittest
import urllib.error
import urllib.request
import json
from unittest import mock

import view_service as vs
from view_page_store import ViewPageStore


class Clock:
    def __init__(self, value=1_800_000_000.0):
        self.value = value

    def __call__(self):
        return self.value


class ViewPageStoreTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.clock = Clock()
        self.store = ViewPageStore(
            os.path.join(self.tmp.name, "pages.db"), now=self.clock,
            disconnect_grace=300, locked_ttl=1800, grant_ttl=28800,
            replay_ttl=300, audit_ttl=600, max_replay_rows=3,
        )

    def tearDown(self):
        self.store.close()
        self.tmp.cleanup()

    def test_page_token_unlock_cas_and_idempotency(self):
        page = self.store.create_page("alice-developer-0000")
        row = self.store.page(page["page_id"])
        self.assertNotEqual(row["page_token_hash"], page["page_token"])
        command = {
            "command_id": "cmd-1", "request_id": "req-1",
            "session": "alice-developer-0000", "code": page["code"],
            "owner_key": "alice", "sender_open_id": "ou_alice",
            "granted_at": "2027-01-15T08:00:00Z",
        }
        self.assertEqual(self.store.unlock(command), 200)
        self.assertEqual(self.store.unlock(command), 200)
        changed = dict(command, owner_key="mallory")
        self.assertEqual(self.store.unlock(changed), 409)
        row = self.store.page(page["page_id"])
        self.assertEqual(row["state"], "unlocked")
        self.assertEqual(row["grant_owner"], "alice")
        self.assertFalse(self.store.authorize_page(page["page_id"], "wrong", "alice-developer-0000"))
        self.assertTrue(self.store.authorize_page(page["page_id"], page["page_token"], "alice-developer-0000"))
        resumed = self.store.resume_page(page["page_id"], page["page_token"], "alice-developer-0000")
        self.assertEqual(resumed["code"], page["code"])
        self.assertTrue(resumed["can_write"])
        self.assertIsNone(self.store.resume_page(page["page_id"], "wrong", "alice-developer-0000"))

    def test_disconnect_grace_revokes_without_token_bypass(self):
        page = self.store.create_page("alice-developer-0000")
        command = {"command_id": "c", "request_id": "r", "session": "alice-developer-0000",
                   "code": page["code"], "owner_key": "alice"}
        self.assertEqual(self.store.unlock(command), 200)
        self.assertFalse(self.store.mark_disconnected(page["page_id"], "wrong"))
        self.assertTrue(self.store.mark_disconnected(page["page_id"], page["page_token"]))
        self.clock.value += 299
        self.store.sweep()
        self.assertEqual(self.store.page(page["page_id"])["state"], "unlocked")
        self.clock.value += 2
        self.store.sweep()
        self.assertEqual(self.store.page(page["page_id"])["state"], "revoked")

    def test_periodic_sweeper_revokes_within_one_cycle(self):
        page = self.store.create_page("alice-developer-0000")
        command = {"command_id": "sweep-c", "request_id": "sweep-r",
                   "session": "alice-developer-0000", "code": page["code"], "owner_key": "alice"}
        self.assertEqual(self.store.unlock(command), 200)
        self.assertTrue(self.store.mark_disconnected(page["page_id"], page["page_token"]))
        self.clock.value += 301
        stop = threading.Event()
        thread = threading.Thread(target=vs._run_page_sweeper, args=(stop, 0.01, self.store))
        thread.start()
        deadline = time.monotonic() + 0.5
        while time.monotonic() < deadline and self.store.page(page["page_id"])["state"] != "revoked":
            time.sleep(0.005)
        stop.set()
        thread.join(timeout=1)
        self.assertEqual(self.store.page(page["page_id"])["state"], "revoked")

    def test_grant_and_locked_orphan_expiry_boundaries(self):
        unlocked = self.store.create_page("alice-developer-0000")
        command = {"command_id": "expiry-c", "request_id": "expiry-r",
                   "session": "alice-developer-0000", "code": unlocked["code"], "owner_key": "alice"}
        self.assertEqual(self.store.unlock(command), 200)
        locked = self.store.create_page("alice-developer-0000")
        self.clock.value += 1799
        self.store.sweep()
        self.assertEqual(self.store.page(locked["page_id"])["state"], "locked")
        self.clock.value += 2
        self.store.sweep()
        self.assertEqual(self.store.page(locked["page_id"])["state"], "expired")
        self.assertEqual(self.store.page(unlocked["page_id"])["state"], "unlocked")
        self.clock.value += 28800 - 1801 - 1
        self.store.sweep()
        self.assertEqual(self.store.page(unlocked["page_id"])["state"], "unlocked")
        self.clock.value += 1
        self.store.sweep()
        self.assertEqual(self.store.page(unlocked["page_id"])["state"], "expired")

    def test_concurrent_same_command_is_exactly_once_and_idempotent(self):
        page = self.store.create_page("alice-developer-0000")
        command = {"command_id": "race-c", "request_id": "race-r",
                   "session": "alice-developer-0000", "code": page["code"], "owner_key": "alice"}
        barrier = threading.Barrier(3)
        statuses = []

        def unlock():
            barrier.wait()
            statuses.append(self.store.unlock(command))

        threads = [threading.Thread(target=unlock) for _ in range(2)]
        for thread in threads:
            thread.start()
        barrier.wait()
        for thread in threads:
            thread.join(timeout=2)
        self.assertEqual(sorted(statuses), [200, 200])
        self.assertEqual(self.store.page(page["page_id"])["state"], "unlocked")
        commands, _ = self.store.replay_counts()
        self.assertEqual(commands, 1)

    def test_only_successful_token_authenticated_write_renews_grant(self):
        page = self.store.create_page("alice-developer-0000")
        command = {"command_id": "write-c", "request_id": "write-r",
                   "session": "alice-developer-0000", "code": page["code"], "owner_key": "alice"}
        self.assertEqual(self.store.unlock(command), 200)
        original_expiry = self.store.page(page["page_id"])["unlock_expires_at"]
        self.clock.value += 100
        self.assertFalse(self.store.record_successful_write(page["page_id"], "wrong"))
        self.assertEqual(self.store.page(page["page_id"])["unlock_expires_at"], original_expiry)
        self.assertTrue(self.store.record_successful_write(page["page_id"], page["page_token"]))
        renewed = self.store.page(page["page_id"])
        self.assertGreater(renewed["unlock_expires_at"], original_expiry)
        self.assertIsNotNone(renewed["last_write_at"])

    def test_nonce_and_command_tables_are_bounded(self):
        for i in range(5):
            self.assertTrue(self.store.claim_nonce("canary", f"{i:032x}"))
        _, nonces = self.store.replay_counts()
        self.assertLessEqual(nonces, 3)
        self.assertFalse(self.store.claim_nonce("canary", f"{4:032x}"))
        self.clock.value += 301
        self.assertTrue(self.store.claim_nonce("canary", f"{4:032x}"))
        for i in range(5):
            self.assertEqual(self.store.unlock({
                "command_id": f"c-{i}", "request_id": f"r-{i}",
                "session": "alice-developer-0000", "code": f"missing-{i}", "owner_key": "alice",
            }), 404)
        commands, _ = self.store.replay_counts()
        self.assertLessEqual(commands, 3)


class ControlHMACTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.clock = Clock()
        self.store = ViewPageStore(os.path.join(self.tmp.name, "pages.db"), now=self.clock)
        self.secret_path = os.path.join(self.tmp.name, "secret")
        with open(self.secret_path, "wb") as fh:
            fh.write(b"0123456789abcdef0123456789abcdef")

    def tearDown(self):
        self.store.close()
        self.tmp.cleanup()

    def test_valid_signature_then_nonce_replay_rejected(self):
        body = b'{"command_id":"c"}'
        ts = str(int(self.clock.value))
        nonce = "00112233445566778899aabbccddeeff"
        signature = vs._control_signature(
            b"0123456789abcdef0123456789abcdef", "POST", vs.CONTROL_UNLOCK_PATH, ts, nonce, body)
        headers = {"X-DW-VS-Target": "canary", "X-DW-Timestamp": ts,
                   "X-DW-Nonce": nonce, "X-DW-Signature": signature}
        with mock.patch.multiple(
            vs, VIEW_UNLOCK_V2=True, VIEW_TARGET_NAME="canary",
            HUB_TO_VS_SECRET_FILE=self.secret_path,
        ):
            self.assertEqual(vs._verify_control_request(
                headers, "POST", vs.CONTROL_UNLOCK_PATH, body, self.store, now=self.clock.value), (True, 200))
            self.assertEqual(vs._verify_control_request(
                headers, "POST", vs.CONTROL_UNLOCK_PATH, body, self.store, now=self.clock.value), (False, 409))

    def test_wrong_direction_and_stale_timestamp_rejected(self):
        body = b"{}"
        ts = str(int(self.clock.value))
        nonce = "ffeeddccbbaa99887766554433221100"
        secret = b"0123456789abcdef0123456789abcdef"
        with mock.patch.multiple(
            vs, VIEW_UNLOCK_V2=True, VIEW_TARGET_NAME="canary",
            HUB_TO_VS_SECRET_FILE=self.secret_path,
        ):
            reverse = vs._control_signature(
                secret, "POST", vs.CONTROL_UNLOCK_PATH, ts, nonce, body,
                direction="view-service-to-hub/v1")
            headers = {"X-DW-VS-Target": "canary", "X-DW-Timestamp": ts,
                       "X-DW-Nonce": nonce, "X-DW-Signature": reverse}
            self.assertEqual(vs._verify_control_request(
                headers, "POST", vs.CONTROL_UNLOCK_PATH, body, self.store, now=self.clock.value), (False, 401))
            valid = vs._control_signature(secret, "POST", vs.CONTROL_UNLOCK_PATH, ts, nonce, body)
            headers["X-DW-Signature"] = valid
            self.assertEqual(vs._verify_control_request(
                headers, "POST", vs.CONTROL_UNLOCK_PATH, body, self.store, now=self.clock.value + 61), (False, 401))

    def test_http_unlock_is_synchronous_and_replay_safe(self):
        page = self.store.create_page("alice-developer-0000")
        command = {
            "command_id": "msg-9", "request_id": "msg-9", "session": "alice-developer-0000",
            "code": page["code"], "owner_key": "alice", "sender_open_id": "ou_alice",
            "granted_at": "2027-01-15T08:00:00Z",
        }
        body = json.dumps(command, separators=(",", ":")).encode()
        ts = str(int(self.clock.value))
        nonce = "1234567890abcdef1234567890abcdef"
        secret = b"0123456789abcdef0123456789abcdef"
        signature = vs._control_signature(secret, "POST", vs.CONTROL_UNLOCK_PATH, ts, nonce, body)
        server = vs.ThreadingHTTPServer(("127.0.0.1", 0), vs._Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        old_store = vs._page_store
        vs._page_store = self.store
        try:
            with mock.patch.multiple(
                vs, VIEW_UNLOCK_V2=True, VIEW_TARGET_NAME="canary",
                HUB_TO_VS_SECRET_FILE=self.secret_path,
            ), mock.patch("time.time", return_value=self.clock.value):
                req = urllib.request.Request(
                    f"http://127.0.0.1:{server.server_port}{vs.CONTROL_UNLOCK_PATH}", body,
                    {"Content-Type": "application/json", "X-DW-VS-Target": "canary",
                     "X-DW-Timestamp": ts, "X-DW-Nonce": nonce,
                     "X-DW-Request-ID": "msg-9", "X-DW-Signature": signature},
                    method="POST",
                )
                with urllib.request.urlopen(req, timeout=2) as response:
                    self.assertEqual(response.status, 200)
                self.assertEqual(self.store.page(page["page_id"])["state"], "unlocked")
                with self.assertRaises(urllib.error.HTTPError) as replay:
                    urllib.request.urlopen(req, timeout=2)
                self.assertEqual(replay.exception.code, 409)
        finally:
            vs._page_store = old_store
            server.shutdown()
            server.server_close()
            thread.join(timeout=2)

    def test_v2_input_uses_reverse_direction_signature(self):
        captured = {}

        class Response:
            status = 200

            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self):
                return b'{"ok":true}'

        def fake_urlopen(request, timeout):
            captured["request"] = request
            captured["timeout"] = timeout
            return Response()

        with mock.patch.multiple(
            vs, VIEW_TARGET_NAME="canary", VS_TO_HUB_SECRET_FILE=self.secret_path,
            HUB_INTERNAL="http://hub.internal",
        ), mock.patch.object(vs.urllib.request, "urlopen", side_effect=fake_urlopen), \
                mock.patch.object(vs.secrets, "token_hex", return_value="1234567890abcdef1234567890abcdef"), \
                mock.patch.object(vs.time, "time", return_value=self.clock.value):
            status, _ = vs._hub_v2_input("alice-developer-0000", "page-1", "req-1", "a\nb")
        self.assertEqual(status, 200)
        request = captured["request"]
        self.assertEqual(request.full_url, "http://hub.internal/internal/view-v2/alice-developer-0000/input")
        signature = request.headers["X-dw-signature"]
        reverse = vs._control_signature(
            b"0123456789abcdef0123456789abcdef", "POST",
            "/internal/view-v2/alice-developer-0000/input", request.headers["X-dw-timestamp"],
            request.headers["X-dw-nonce"], request.data, direction="view-service-to-hub/v1")
        self.assertEqual(signature, reverse)
        forward = vs._control_signature(
            b"0123456789abcdef0123456789abcdef", "POST",
            "/internal/view-v2/alice-developer-0000/input", request.headers["X-dw-timestamp"],
            request.headers["X-dw-nonce"], request.data)
        self.assertNotEqual(signature, forward)


if __name__ == "__main__":
    unittest.main()
