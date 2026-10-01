"""Hosted-seat enrollment: the Hub issues a one-time token; the container polls it directly."""
import calendar
import json
import os
import shutil
import stat
import tempfile
import unittest

from sessionhelper import enroll
from sessionhelper.enroll import (
    Credential, CredentialAlreadyIssued, EnrollError, EnrollStopped, HubHTTPError, enroll_with_token,
)

SECRET = "dwa_" + "ef" * 32
POLL = "ab" * 32
APP = "cd" * 16
APPROVED = {"state": "approved", "agent_name": "Codex1", "agent_full_name": "Codex1.zzc", "credential": SECRET}


FIXED_NOW = calendar.timegm((2026, 10, 1, 7, 0, 0))   # 固定时钟:令牌过期判断不得依赖真实时间

class Hub:
    """Scripted Hub: poll replies are dicts or exceptions; no apply endpoint exists on this route."""

    def __init__(self, polls):
        self.polls, self.gets, self.posts = list(polls), [], []

    def post_json(self, path, body):
        self.posts.append(path)
        raise AssertionError("the hosted route must never apply or post anything")

    def get_json(self, path, headers):
        self.gets.append((path, headers))
        item = self.polls.pop(0)
        if isinstance(item, Exception):
            raise item
        return item


def mode(path):
    return stat.S_IMODE(os.stat(path).st_mode)


class HostedEnrollTest(unittest.TestCase):
    def setUp(self):
        self._td = tempfile.TemporaryDirectory()
        self.addCleanup(self._td.cleanup)
        self.dir = os.path.join(self._td.name, "state")
        self.token_file = os.path.join(self._td.name, "token.json")
        self.notes = []

    def put_token(self, **over):
        # Far-future default so tests that use the REAL clock (prepare_client_env has no clock parameter) never go stale;
        # tests that care about expiry pass their own expires_at together with an injected clock.
        body = {"application_id": APP, "poll_token": POLL, "expires_at": "2099-01-01T00:00:00Z"}
        body.update(over)
        with open(self.token_file, "w") as fh:
            json.dump(body, fh)

    def run_it(self, hub, **kw):
        kw.setdefault("now", lambda: 1_790_000_000.0)  # 2026-09-21: well before the default expiry
        return enroll_with_token(state_dir=self.dir, token_file=self.token_file, transport=hub,
                                 notify=self.notes.append, sleep=lambda _s: None, **kw)

    def path(self, name):
        return os.path.join(self.dir, name)

    def test_polls_the_given_token_directly_and_stores_the_credential_0600(self):
        self.put_token()
        hub = Hub([{"state": "pending"}, APPROVED])
        cred = self.run_it(hub)
        self.assertEqual(cred, Credential("Codex1", SECRET))
        self.assertEqual(hub.posts, [], "no application is created: the Hub already issued the token")
        self.assertEqual([g[0] for g in hub.gets], [f"/api/enroll/applications/{APP}"] * 2)
        self.assertEqual(hub.gets[0][1], {"Authorization": f"Bearer {POLL}"})
        self.assertEqual(mode(self.path(enroll.CREDENTIAL_FILE)), 0o600)
        with open(self.path(enroll.CREDENTIAL_FILE)) as fh:
            self.assertEqual(json.load(fh), {"agent_name": "Codex1", "secret": SECRET})
        self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)), "poll token dropped with the pending state")

    def test_token_file_is_deleted_after_being_read(self):
        self.put_token()
        self.run_it(Hub([APPROVED]))
        self.assertFalse(os.path.exists(self.token_file))

    def test_token_file_is_gone_even_when_the_poll_then_fails(self):
        self.put_token()
        with self.assertRaises(EnrollError):
            self.run_it(Hub([{"state": "pending"}]), max_polls=1)
        self.assertFalse(os.path.exists(self.token_file), "deleted as soon as it was safely held")
        self.assertEqual(mode(self.path(enroll.PENDING_FILE)), 0o600)  # the state lives on in a private file

    def test_restart_after_the_file_was_consumed_resumes_from_the_saved_state(self):
        self.put_token()
        with self.assertRaises(EnrollError):
            self.run_it(Hub([{"state": "pending"}]), max_polls=1)
        hub = Hub([APPROVED])
        self.assertEqual(self.run_it(hub).secret, SECRET)
        self.assertEqual(hub.gets[0][1], {"Authorization": f"Bearer {POLL}"})

    def test_hub_says_expired_means_clear_failure_and_nothing_half_done(self):
        self.put_token()
        with self.assertRaises(EnrollStopped):
            self.run_it(Hub([{"state": "expired"}]))
        self.assertFalse(os.path.exists(self.path(enroll.CREDENTIAL_FILE)))
        self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)))
        self.assertFalse(os.path.exists(self.token_file))
        self.assertIn("expired", self.notes[0])

    def test_clearly_expired_token_is_refused_locally_without_calling_the_hub(self):
        self.put_token(expires_at="2026-10-01T08:00:00Z")
        hub = Hub([])
        with self.assertRaises(EnrollStopped):
            self.run_it(hub, now=lambda: 1_791_000_000.0)  # long after expiry
        self.assertEqual(hub.gets, [])
        self.assertFalse(os.path.exists(self.token_file))
        self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)))
        self.assertFalse(os.path.exists(self.path(enroll.CREDENTIAL_FILE)))

    def test_small_clock_skew_does_not_refuse_a_token_the_hub_still_honours(self):
        self.put_token(expires_at="2026-10-01T08:00:00Z")
        from datetime import datetime, timezone
        exp = datetime(2026, 10, 1, 8, 0, tzinfo=timezone.utc).timestamp()
        cred = self.run_it(Hub([APPROVED]), now=lambda: exp + 60)  # 1 minute past: within the grace
        self.assertEqual(cred.secret, SECRET)

    def test_unknown_token_404_stops_clearly(self):
        self.put_token()
        with self.assertRaises(EnrollStopped):
            self.run_it(Hub([HubHTTPError(404, "")]))
        self.assertFalse(os.path.exists(self.path(enroll.PENDING_FILE)))

    def test_credential_already_taken_is_an_explicit_error_not_an_empty_file(self):
        self.put_token()
        with self.assertRaises(CredentialAlreadyIssued):
            self.run_it(Hub([{"state": "approved", "agent_name": "Codex1", "credential": ""}]))
        self.assertFalse(os.path.exists(self.path(enroll.CREDENTIAL_FILE)))

    def test_429_waits_as_told(self):
        self.put_token()
        sleeps = []
        enroll_with_token(state_dir=self.dir, token_file=self.token_file, transport=Hub([HubHTTPError(429, "rate_limited", 6), APPROVED]),
                          sleep=sleeps.append, now=lambda: 1_790_000_000.0)
        self.assertEqual(sleeps, [6])

    def test_malformed_token_file_fails_and_is_not_left_behind(self):
        with open(self.token_file, "w") as fh:
            fh.write('{"application_id": "x"')  # truncated
        with self.assertRaises(EnrollError):
            self.run_it(Hub([]))
        self.assertFalse(os.path.exists(self.token_file))
        self.put_token(poll_token="")
        with self.assertRaises(EnrollError):
            self.run_it(Hub([]))
        self.assertFalse(os.path.exists(self.token_file))

    def test_if_the_state_cannot_be_saved_the_token_file_is_kept_for_another_try(self):
        self.put_token()
        os.makedirs(self.dir, exist_ok=True)
        os.makedirs(self.path(enroll.PENDING_FILE))  # a directory where the state file should go
        with self.assertRaises(OSError):
            self.run_it(Hub([APPROVED]))
        self.assertTrue(os.path.exists(self.token_file))

    def test_no_token_and_no_credential_is_an_explicit_error(self):
        with self.assertRaises(EnrollError):
            self.run_it(Hub([]))

    def test_no_token_but_a_stored_credential_is_just_used(self):
        enroll.save_credential(self.dir, Credential("Codex1", SECRET))
        hub = Hub([])
        self.assertEqual(self.run_it(hub).secret, SECRET)
        self.assertEqual(hub.gets, [])

    def test_a_new_token_rotates_the_credential_only_once_it_is_really_taken(self):
        old = Credential("Codex1", "dwa_" + "11" * 32)
        enroll.save_credential(self.dir, old)
        self.put_token()
        with self.assertRaises(EnrollStopped):
            self.run_it(Hub([{"state": "expired"}]))
        self.assertEqual(enroll.load_credential(self.dir), old, "a failed rotation leaves the old credential untouched")
        self.put_token()
        self.assertEqual(self.run_it(Hub([APPROVED])).secret, SECRET)
        self.assertEqual(enroll.load_credential(self.dir).secret, SECRET)

    def test_secrets_are_never_in_the_notifications(self):
        self.put_token()
        with self.assertRaises(EnrollStopped):
            self.run_it(Hub([{"state": "expired"}]))
        self.assertNotIn(POLL, " ".join(self.notes))

    def test_prepare_client_env_picks_the_hosted_route_without_an_owner(self):
        self.put_token(expires_at="2026-10-01T08:00:00Z")      # 按真实时钟已过期、按注入的 07:00Z 仍有效:只有用了注入时钟才过
        env, cred, state_dir = enroll.prepare_client_env(
            {"SH_WS_BASE": "wss://h", "SH_STATE_DIR": self.dir, "SH_ENROLL": "1", "SH_ENROLL_TOKEN_FILE": self.token_file},
            show=lambda _c: self.fail("a hosted seat has no pairing code to show"), notify=self.notes.append,
            transport=Hub([APPROVED]), now=lambda: FIXED_NOW)
        self.assertEqual((cred.secret, env["SH_SESSION_NAME"]), (SECRET, "codex1"))
        self.assertNotIn(POLL, " ".join(env.values()))
        self.assertFalse(os.path.exists(self.token_file))

    def test_prepare_client_env_uses_the_injected_clock_for_token_expiry(self):
        self.put_token()                                        # 远期令牌:按真实时钟有效、按注入的 2100 年已过期
        env = {"SH_WS_BASE": "wss://h", "SH_STATE_DIR": self.dir, "SH_ENROLL": "1", "SH_ENROLL_TOKEN_FILE": self.token_file}
        past_grace = calendar.timegm((2100, 1, 1, 0, 0, 0))
        with self.assertRaises(EnrollStopped):
            enroll.prepare_client_env(env, show=lambda _c: None, notify=self.notes.append,
                                      transport=Hub([APPROVED]), now=lambda: past_grace)


if __name__ == "__main__":
    unittest.main()
