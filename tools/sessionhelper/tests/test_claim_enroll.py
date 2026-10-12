import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path

from sessionhelper.claim_enroll import claim_enroll, cancel_pending_claim, claim_operation, PENDING
from sessionhelper.enroll import load_credential, EnrollError, EnrollStopped, HubHTTPError


class FixtureTransport:
    def __init__(self):
        self.applies = 0
        self.polls = 0
        self.acks = 0
        self.lose_ack = False
        self.owner = "alice"

    def post_json(self, path, body):
        self.applies += 1
        self.body = body
        assert path == "/api/enroll/claims"
        return {"claim_id": "11111111111111111111111111111111", "poll_token": "2222222222222222222222222222222222222222222222222222222222222222", "expires_at": "2026-10-12T03:00:00Z"}

    def get_json(self, path, headers):
        self.polls += 1
        return {"state": "credential_ready", "credential": "dwa_" + "c" * 64, "agent_name": "pmagent",
                "agent_full_name": "pmagent.al", "user_id": self.owner}

    def post_authenticated(self, path, body, headers):
        self.acks += 1
        if self.lose_ack:
            self.lose_ack = False
            raise OSError("fixture lost response")
        return {}


class ClaimEnrollTest(unittest.TestCase):
    def start(self, root, transport, show=None):
        return claim_enroll(state_dir=root, owner="alice", short_name="al", agent_name="pmagent", mode="desktop",
                            transport=transport, show=show or (lambda _: None), notify=lambda _: None,
                            sleep=lambda _: None, max_polls=2)

    def test_code_not_sent_and_credential_private_before_ack(self):
        with tempfile.TemporaryDirectory() as root:
            transport = FixtureTransport()
            codes = []
            credential = self.start(root, transport, codes.append)
            self.assertTrue(codes[0].startswith("#pair "))
            code = codes[0].split()[1]
            self.assertEqual(len(code), 12)
            self.assertEqual(transport.body["code_digest"], hashlib.sha256(code.encode()).hexdigest())
            self.assertNotIn(code, json.dumps(transport.body))
            self.assertEqual(credential, load_credential(root))
            self.assertEqual((Path(root) / "credential.json").stat().st_mode & 0o777, 0o600)
            self.assertFalse((Path(root) / PENDING).exists())
            self.assertEqual(transport.acks, 1)

    def test_ack_loss_resumes_without_new_claim_or_credential(self):
        with tempfile.TemporaryDirectory() as root:
            transport = FixtureTransport()
            transport.lose_ack = True
            with self.assertRaises(OSError):
                self.start(root, transport)
            saved = (Path(root) / "credential.json").read_bytes()
            self.assertTrue((Path(root) / PENDING).exists())
            self.start(root, transport)
            self.assertEqual(transport.applies, 1)
            self.assertEqual(transport.polls, 1)
            self.assertEqual((Path(root) / "credential.json").read_bytes(), saved)

    def test_different_owner_never_saves(self):
        with tempfile.TemporaryDirectory() as root:
            transport = FixtureTransport()
            transport.owner = "other"
            with self.assertRaises(Exception):
                self.start(root, transport)
            self.assertFalse((Path(root) / "credential.json").exists())
            self.assertEqual(transport.acks, 0)

    def pending(self, root):
        value = {"claim_id": "11111111111111111111111111111111", "poll_token": "2222222222222222222222222222222222222222222222222222222222222222", "code": "ABCDEFGHJKMN", "owner": "alice"}
        path = Path(root)/PENDING
        path.write_text(json.dumps(value));path.chmod(0o600)
        return path

    def test_cancel_archives_only_after_matching_server_proof_and_next_apply(self):
        with tempfile.TemporaryDirectory() as root:
            path = self.pending(root)
            class CancelTransport(FixtureTransport):
                def post_authenticated(self, route, body, headers):
                    if route.endswith('/cancel'):
                        self.cancelled = True
                        return {"claim_id": "11111111111111111111111111111111", "state": "cancelled"}
                    return super().post_authenticated(route, body, headers)
            transport = CancelTransport()
            archive = Path(cancel_pending_claim(state_dir=root, owner='alice', transport=transport))
            self.assertEqual(json.loads(archive.read_text())['claim_id'], '11111111111111111111111111111111')
            self.assertEqual(archive.stat().st_mode & 0o777, 0o600)
            self.assertFalse(path.exists())
            self.assertEqual(transport.applies, 0)
            self.start(root, transport)
            self.assertEqual(transport.applies, 1)

    def test_cancel_unknown_or_issued_keeps_pending_no_automatic_new_claim(self):
        for outcome in ({}, {"claim_id": "wrong", "state": "cancelled"}, HubHTTPError(403, 'not_allowed'), OSError('synthetic lost cancel response')):
            with self.subTest(outcome=type(outcome).__name__), tempfile.TemporaryDirectory() as root:
                path = self.pending(root);before = path.read_bytes()
                class CancelTransport(FixtureTransport):
                    def post_authenticated(self, *args):
                        if isinstance(outcome, BaseException): raise outcome
                        return outcome
                transport = CancelTransport()
                with self.assertRaises((EnrollError, OSError)):
                    cancel_pending_claim(state_dir=root, owner='alice', transport=transport)
                self.assertEqual(path.read_bytes(), before)
                self.assertEqual(list(Path(root).glob('claim-cancelled-*')), [])
                self.assertEqual(transport.applies, 0)

    def test_cancel_refuses_existing_credential_and_symlink(self):
        with tempfile.TemporaryDirectory() as root:
            path = self.pending(root)
            credential = Path(root)/'credential.json';credential.write_text('synthetic retained data');credential.chmod(0o600)
            with self.assertRaises(EnrollStopped):
                cancel_pending_claim(state_dir=root, owner='alice', transport=object())
            self.assertTrue(path.exists())
            credential.unlink()
            target = Path(root)/'target.json';path.rename(target);path.symlink_to(target)
            with self.assertRaises(EnrollError):
                cancel_pending_claim(state_dir=root, owner='alice', transport=object())
            self.assertTrue(target.exists())

    def test_active_claim_wait_refuses_parallel_cancel_and_releases_on_interrupt(self):
        with tempfile.TemporaryDirectory() as root:
            path = self.pending(root)
            with claim_operation(root):
                with self.assertRaises(EnrollStopped):
                    cancel_pending_claim(state_dir=root, owner='alice', transport=object())
            transport = FixtureTransport()
            with self.assertRaises(KeyboardInterrupt):
                self.start(root, transport, lambda _: (_ for _ in ()).throw(KeyboardInterrupt()))
            self.assertTrue(path.exists())
            self.start(root, transport)
            self.assertEqual(transport.applies, 0)
            self.assertEqual(transport.polls, 1)

    def test_corrupt_private_auth_fields_stop_before_header_or_url_use(self):
        for field, bad in (("poll_token", "synthetic-private-value\nInjected: value"), ("claim_id", "../different-claim")):
            with self.subTest(field=field), tempfile.TemporaryDirectory() as root:
                path = self.pending(root)
                value = json.loads(path.read_text());value[field] = bad
                path.write_text(json.dumps(value));before = path.read_bytes()
                with self.assertRaises(EnrollError) as stopped:
                    cancel_pending_claim(state_dir=root, owner='alice', transport=object())
                self.assertNotIn(bad, str(stopped.exception))
                self.assertEqual(path.read_bytes(), before)

    def test_nonregular_or_oversized_pending_refused_without_blocking(self):
        import subprocess, sys
        with tempfile.TemporaryDirectory() as root:
            path = Path(root)/PENDING
            os.mkfifo(path, 0o600)
            script = "from sessionhelper.claim_enroll import cancel_pending_claim; from sessionhelper.enroll import EnrollError; import sys\ntry: cancel_pending_claim(state_dir=sys.argv[1],owner='alice',transport=object())\nexcept EnrollError: print('REFUSED')\n"
            env = dict(os.environ, PYTHONPATH=str(Path(__file__).resolve().parents[1]))
            result = subprocess.run([sys.executable, '-c', script, root], env=env, timeout=3,
                                    text=True, capture_output=True, check=True)
            self.assertEqual(result.stdout.strip(), 'REFUSED')
            self.assertTrue(path.exists())
            path.unlink();path.write_text('x' * 4097);path.chmod(0o600)
            with self.assertRaises(EnrollError):
                cancel_pending_claim(state_dir=root, owner='alice', transport=object())
            self.assertEqual(path.stat().st_size, 4097)


if __name__ == "__main__":
    unittest.main()
