import hashlib
import json
import os
import stat
import tempfile
import unittest
from unittest import mock

from sessionhelper import enroll
from sessionhelper.enroll import (
    Credential, CredentialAlreadyIssued, CredentialFileError, EnrollError, EnrollRejected,
    EnrollStopped, enroll as run_enroll,
)

SECRET = "cred-SECRET-value-0001"


class StubHub:
    """Records every call; replies come from a scripted list of poll states."""

    def __init__(self, polls):
        self.polls = list(polls)
        self.applies = []
        self.gets = []

    def post_json(self, path, body):
        self.applies.append((path, body))
        n = len(self.applies)
        return {"application_id": f"app-{n}", "poll_token": f"POLLTOKEN-{n}",
                "expires_at": "2026-10-02T00:00:00Z"}

    def get_json(self, path, headers):
        self.gets.append((path, headers))
        return self.polls.pop(0)


APPROVED = {"state": "approved", "agent_name": "Dev55", "agent_full_name": "Dev55.zzc", "credential": SECRET}
PENDING = {"state": "pending"}


def mode(path):
    return stat.S_IMODE(os.stat(path).st_mode)


class EnrollFlowTest(unittest.TestCase):
    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.dir = os.path.join(self._td.name, "state")
        self.shown, self.notes = [], []
        self.addCleanup(self._td.cleanup)

    def run_flow(self, hub, **kw):
        return run_enroll(state_dir=self.dir, owner="zzc", transport=hub, show=self.shown.append,
                          notify=self.notes.append, sleep=lambda _s: None, **kw)

    def path(self, name):
        return os.path.join(self.dir, name)

    def test_first_boot_shows_code_applies_polls_and_stores_0600(self):
        hub = StubHub([PENDING, PENDING, APPROVED])
        cred = self.run_flow(hub)
        self.assertEqual(cred, Credential("Dev55", SECRET))  # name case preserved
        self.assertEqual(len(self.shown), 1)
        self.assertRegex(self.shown[0], r"^\d{6}$")
        self.assertEqual(mode(self.path(enroll.CREDENTIAL_FILE)), 0o600)
        self.assertEqual(mode(self.dir), 0o700)
        with open(self.path(enroll.CREDENTIAL_FILE)) as fh:
            self.assertEqual(json.load(fh)["secret"], SECRET)
        self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)))  # poll_token dropped
        self.assertEqual(len(hub.applies), 1)
        self.assertEqual(hub.applies[0][0], "/api/enroll/applications")
        self.assertEqual(hub.gets[0][0], "/api/enroll/applications/app-1")

    def test_apply_body_matches_contract_and_hides_the_code(self):
        hub = StubHub([APPROVED])
        self.run_flow(hub)
        _p, body = hub.applies[0]
        self.assertEqual(set(body), {"owner_user_id", "pairing_salt", "pairing_digest",
                                     "device_fingerprint", "client_version"})
        self.assertEqual(body["owner_user_id"], "zzc")
        self.assertEqual(len(bytes.fromhex(body["pairing_salt"])), 16)
        want = hashlib.sha256(bytes.fromhex(body["pairing_salt"]) + self.shown[0].encode("ascii")).hexdigest()
        self.assertEqual(body["pairing_digest"], want)
        self.assertNotIn(self.shown[0], [str(v) for v in body.values()])

    def test_poll_uses_bearer_poll_token(self):
        hub = StubHub([APPROVED])
        self.run_flow(hub)
        self.assertEqual(hub.gets[0][1], {"Authorization": "Bearer POLLTOKEN-1"})

    def test_secrets_never_shown_or_in_repr(self):
        self.run_flow(StubHub([APPROVED]))
        text = " ".join(self.shown + self.notes)
        self.assertNotIn(SECRET, text)
        self.assertNotIn("POLLTOKEN", text)
        self.assertNotIn(SECRET, repr(Credential("Dev55", SECRET)))

    def test_existing_credential_skips_network(self):
        enroll.save_credential(self.dir, Credential("Dev55", SECRET))
        hub = StubHub([])
        self.assertEqual(self.run_flow(hub), Credential("Dev55", SECRET))
        self.assertEqual((hub.applies, hub.gets, self.shown), ([], [], []))

    def test_restart_while_waiting_resumes_same_code_and_application(self):
        hub1 = StubHub([PENDING])
        with self.assertRaises(EnrollError):
            self.run_flow(hub1, max_polls=1)
        first_code = self.shown[0]
        self.assertEqual(mode(self.path(enroll.PENDING_FILE)), 0o600)  # holds poll_token
        hub2 = StubHub([APPROVED])
        self.run_flow(hub2)
        self.assertEqual(self.shown, [first_code, first_code])
        self.assertEqual(hub2.applies, [])
        self.assertEqual(hub2.gets[0][0], "/api/enroll/applications/app-1")
        self.assertEqual(hub2.gets[0][1], {"Authorization": "Bearer POLLTOKEN-1"})

    def test_terminal_states_stop_explain_and_leave_nothing(self):
        for state, exc in (("rejected", EnrollRejected), ("expired", EnrollStopped), ("superseded", EnrollStopped)):
            with self.subTest(state=state):
                self.notes.clear()
                with self.assertRaises(exc):
                    self.run_flow(StubHub([{"state": state}]))
                self.assertEqual(len(self.notes), 1)
                self.assertFalse(os.path.exists(self.path(enroll.CREDENTIAL_FILE)))
                self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)))

    def test_expired_does_not_silently_reapply(self):
        hub = StubHub([{"state": "expired"}])
        with self.assertRaises(EnrollStopped):
            self.run_flow(hub)
        self.assertEqual(len(hub.applies), 1)

    def test_credential_given_only_once_second_look_is_explicit_error(self):
        hub = StubHub([{"state": "approved", "agent_name": "Dev55", "credential": ""}])
        with self.assertRaises(CredentialAlreadyIssued):
            self.run_flow(hub)
        self.assertFalse(os.path.exists(self.path(enroll.CREDENTIAL_FILE)))
        self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)))

    def test_unknown_state_is_an_error(self):
        with self.assertRaises(EnrollError):
            self.run_flow(StubHub([{"state": "weird"}]))

    def test_bad_owner_rejected_before_network(self):
        hub = StubHub([])
        for bad in ("", "Z ZC", "a/b"):
            with self.assertRaises(EnrollError):
                run_enroll(state_dir=self.dir, owner=bad, transport=hub, show=print)
        self.assertEqual(hub.applies, [])

    def test_group_readable_credential_is_refused(self):
        enroll.save_credential(self.dir, Credential("Dev55", SECRET))
        os.chmod(self.path(enroll.CREDENTIAL_FILE), 0o644)
        with self.assertRaises(CredentialFileError) as ctx:
            enroll.load_credential(self.dir)
        self.assertNotIn(SECRET, str(ctx.exception))

    def test_reverse_control_lax_writer_is_caught_by_the_mode_assertion(self):
        def lax(path, data):
            os.makedirs(os.path.dirname(path), exist_ok=True)
            fd = os.open(path, os.O_WRONLY | os.O_CREAT, 0o644)
            os.fchmod(fd, 0o644)
            with os.fdopen(fd, "w") as fh:
                fh.write(data)

        with mock.patch.object(enroll, "write_private_file", lax):
            self.run_flow(StubHub([APPROVED]))
        self.assertNotEqual(mode(self.path(enroll.CREDENTIAL_FILE)), 0o600)

    def test_device_fingerprint_is_stable_and_not_the_raw_id(self):
        a = enroll.device_fingerprint(self.dir)
        self.assertEqual(a, enroll.device_fingerprint(self.dir))
        with open(self.path(enroll.DEVICE_FILE)) as fh:
            raw = fh.read().strip()
        self.assertNotEqual(a, raw)
        self.assertEqual(mode(self.path(enroll.DEVICE_FILE)), 0o600)

    def test_revoked_removes_credential_notifies_and_does_not_reapply(self):
        enroll.save_credential(self.dir, Credential("Dev55", SECRET))
        enroll.revoke_local(self.dir, self.notes.append)
        self.assertFalse(os.path.exists(self.path(enroll.CREDENTIAL_FILE)))
        self.assertEqual(len(self.notes), 1)
        self.assertIn("revoked", self.notes[0])
        self.assertIsNone(enroll.load_credential(self.dir))  # nothing auto-created

    def test_run_enrolled_hands_credential_to_connect_stub(self):
        got = []
        enroll.run_enrolled(connect=got.append, state_dir=self.dir, owner="zzc", transport=StubHub([APPROVED]),
                            show=self.shown.append, sleep=lambda _s: None)
        self.assertEqual(got, [Credential("Dev55", SECRET)])

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
