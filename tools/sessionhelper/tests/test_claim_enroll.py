import hashlib
import json
import os
import tempfile
import unittest
from pathlib import Path

from sessionhelper.claim_enroll import claim_enroll, PENDING
from sessionhelper.enroll import load_credential


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
        return {"claim_id": "fixture-claim", "poll_token": "fixture-private-token", "expires_at": "2026-10-12T03:00:00Z"}

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


if __name__ == "__main__":
    unittest.main()
