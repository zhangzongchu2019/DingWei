import json
import os
import stat
import tempfile
import unittest
from unittest import mock

from sessionhelper import enroll
from sessionhelper.enroll import (
    Credential, CredentialFileError, EnrollError, EnrollRejected, enroll as run_enroll,
)

SECRET = "cred-SECRET-value-0001"


class StubHub:
    """Records every call; replies come from a scripted list of poll statuses."""

    def __init__(self, polls):
        self.polls = list(polls)
        self.applies = []
        self.gets = []

    def post_json(self, path, body):
        self.applies.append((path, body))
        return {"request_id": f"req-{len(self.applies)}"}

    def get_json(self, path, headers):
        self.gets.append((path, headers))
        return self.polls.pop(0)


APPROVED = {"status": "approved", "agent_name": "dev55", "credential": SECRET}
PENDING = {"status": "pending"}


def mode(path):
    return stat.S_IMODE(os.stat(path).st_mode)


class EnrollFlowTest(unittest.TestCase):
    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.dir = os.path.join(self._td.name, "state")
        self.shown = []
        self.addCleanup(self._td.cleanup)

    def run_flow(self, hub, **kw):
        return run_enroll(state_dir=self.dir, owner="zzc", transport=hub,
                          show=self.shown.append, sleep=lambda _s: None, **kw)

    def test_first_boot_shows_code_applies_polls_and_stores_0600(self):
        hub = StubHub([PENDING, PENDING, APPROVED])
        cred = self.run_flow(hub)
        self.assertEqual(cred, Credential("dev55", SECRET))
        self.assertEqual(len(self.shown), 1)
        self.assertRegex(self.shown[0], r"^\d{6}$")
        path = os.path.join(self.dir, enroll.CREDENTIAL_FILE)
        self.assertEqual(mode(path), 0o600)
        self.assertEqual(mode(self.dir), 0o700)
        with open(path) as fh:
            self.assertEqual(json.load(fh)["secret"], SECRET)
        self.assertFalse(os.path.exists(os.path.join(self.dir, enroll.PENDING_FILE)))
        self.assertEqual(len(hub.applies), 1)
        self.assertEqual(hub.gets[0][0], "/api/enroll/req-1")

    def test_hub_never_sees_pairing_code_or_poll_secret(self):
        hub = StubHub([APPROVED])
        self.run_flow(hub)
        _path, body = hub.applies[0]
        wire = json.dumps(body)
        self.assertNotIn(self.shown[0], [str(v) for v in body.values()])
        self.assertEqual(body["pairing_digest"], enroll.pairing_digest(self.shown[0], body["pairing_salt"]))
        poll_secret = hub.gets[0][1]["X-Poll-Secret"]
        self.assertNotIn(poll_secret, wire)
        self.assertEqual(body["poll_digest"], enroll.secret_digest(poll_secret))

    def test_credential_value_never_shown(self):
        self.run_flow(StubHub([APPROVED]))
        self.assertNotIn(SECRET, " ".join(self.shown))
        self.assertNotIn(SECRET, repr(Credential("dev55", SECRET)))

    def test_existing_credential_skips_network(self):
        enroll.save_credential(self.dir, Credential("dev55", SECRET))
        hub = StubHub([])
        self.assertEqual(self.run_flow(hub), Credential("dev55", SECRET))
        self.assertEqual((hub.applies, hub.gets, self.shown), ([], [], []))

    def test_restart_while_waiting_resumes_same_code_and_request(self):
        hub1 = StubHub([PENDING])
        with self.assertRaises(EnrollError):
            self.run_flow(hub1, max_polls=1)
        first_code = self.shown[0]
        hub2 = StubHub([APPROVED])
        self.run_flow(hub2)
        self.assertEqual(self.shown, [first_code, first_code])
        self.assertEqual(hub2.applies, [])  # no second application
        self.assertEqual(hub2.gets[0][0], "/api/enroll/req-1")

    def test_expired_request_is_replaced_with_a_fresh_code(self):
        hub = StubHub([{"status": "expired"}, APPROVED])
        with mock.patch.object(enroll, "generate_pairing_code", side_effect=["111111", "222222"]):
            self.run_flow(hub)
        self.assertEqual(self.shown, ["111111", "222222"])
        self.assertEqual(len(hub.applies), 2)

    def test_rejected_raises_and_leaves_no_credential(self):
        hub = StubHub([{"status": "rejected"}])
        with self.assertRaises(EnrollRejected):
            self.run_flow(hub)
        self.assertFalse(os.path.exists(os.path.join(self.dir, enroll.CREDENTIAL_FILE)))
        self.assertFalse(os.path.exists(os.path.join(self.dir, enroll.PENDING_FILE)))

    def test_approved_without_credential_is_an_error_not_a_blank_file(self):
        with self.assertRaises(EnrollError):
            self.run_flow(StubHub([{"status": "approved", "agent_name": "dev55"}]))
        self.assertFalse(os.path.exists(os.path.join(self.dir, enroll.CREDENTIAL_FILE)))

    def test_unknown_status_is_an_error(self):
        with self.assertRaises(EnrollError):
            self.run_flow(StubHub([{"status": "weird"}]))

    def test_bad_owner_rejected_before_network(self):
        hub = StubHub([])
        with self.assertRaises(EnrollError):
            run_enroll(state_dir=self.dir, owner="Z ZC", transport=hub, show=print)
        self.assertEqual(hub.applies, [])

    def test_group_readable_credential_is_refused(self):
        enroll.save_credential(self.dir, Credential("dev55", SECRET))
        path = os.path.join(self.dir, enroll.CREDENTIAL_FILE)
        os.chmod(path, 0o644)
        with self.assertRaises(CredentialFileError) as ctx:
            enroll.load_credential(self.dir)
        self.assertNotIn(SECRET, str(ctx.exception))

    def test_reverse_control_lax_writer_is_caught_by_the_mode_assertion(self):
        # If write_private_file regressed to a default-umask write, the 0600
        # assertion in the first-boot test must fail; prove the check bites.
        def lax(path, data):
            os.makedirs(os.path.dirname(path), exist_ok=True)
            fd = os.open(path, os.O_WRONLY | os.O_CREAT, 0o644)
            os.fchmod(fd, 0o644)
            with os.fdopen(fd, "w") as fh:
                fh.write(data)

        with mock.patch.object(enroll, "write_private_file", lax):
            self.run_flow(StubHub([APPROVED]))
        path = os.path.join(self.dir, enroll.CREDENTIAL_FILE)
        self.assertNotEqual(mode(path), 0o600)

    def test_device_fingerprint_is_stable_and_not_the_raw_id(self):
        a = enroll.device_fingerprint(self.dir)
        b = enroll.device_fingerprint(self.dir)
        self.assertEqual(a, b)
        with open(os.path.join(self.dir, enroll.DEVICE_FILE)) as fh:
            raw = fh.read().strip()
        self.assertNotEqual(a, raw)
        self.assertEqual(mode(os.path.join(self.dir, enroll.DEVICE_FILE)), 0o600)

    def test_auth_headers_use_the_dedicated_credential(self):
        self.assertEqual(enroll.auth_headers(Credential("a", "s3")), {"Authorization": "Bearer s3"})

    def test_ws_url_never_self_declares_identity(self):
        url = enroll.agent_ws_url("wss://hub.example:55055/", {"key_id": "FB-1", "name": "dev55", "tool": "CLAUDE"})
        self.assertTrue(url.startswith("wss://hub.example:55055/ws/agent?"))
        for bad in ("key_id", "FB-1", "dev55", "name="):
            self.assertNotIn(bad, url)
        self.assertIn("tool=CLAUDE", url)


if __name__ == "__main__":
    unittest.main()
