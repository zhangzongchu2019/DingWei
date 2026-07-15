import json
import os
import tempfile
import unittest
from unittest import mock

import view_service as vs


class ViewStatePersistenceTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.old_file = vs.STATE_FILE
        vs.STATE_FILE = os.path.join(self.tmp.name, "state.json")
        with vs._state_lock:
            vs._sessions.clear()

    def tearDown(self):
        vs.STATE_FILE = self.old_file
        self.tmp.cleanup()

    def test_restart_reuses_registered_unlocked_viewer(self):
        with vs._state_lock:
            vs._sessions["zzc-dev55-2642"] = {
                "viewer_id": "viewer-stable",
                "token": "unlock-token",
                "code": "8231",
                "can_write": True,
                "cost_total": 1.25,
                "cost_last": 0.1,
                "turns": 4,
            }
            vs._save_state_locked()
            vs._sessions.clear()
        vs._load_state()

        with mock.patch.object(vs, "_hub_post", return_value=(200, {
            "viewer_id": "viewer-stable",
            "can_write": True,
            "writer_token": "unlock-token",
        })) as post:
            state = vs._ensure_ready("zzc-dev55-2642")

        self.assertEqual(state["viewer_id"], "viewer-stable")
        self.assertEqual(state["code"], "8231")
        self.assertEqual(state["token"], "unlock-token")
        self.assertTrue(state["can_write"])
        post.assert_called_once_with(vs.HUB_INTERNAL, "/internal/view/authorize", {
            "session": "zzc-dev55-2642", "viewer_id": "viewer-stable",
        })
        self.assertEqual(os.stat(vs.STATE_FILE).st_mode & 0o777, 0o600)


class RendererCanaryTest(unittest.TestCase):
    def test_canary_session_uses_canary_renderer_and_other_uses_default(self):
        with tempfile.TemporaryDirectory() as tmp:
            default_renderer = os.path.join(tmp, "default.html")
            canary_renderer = os.path.join(tmp, "canary.html")
            with open(default_renderer, "wb") as fh:
                fh.write(b"default renderer")
            with open(canary_renderer, "wb") as fh:
                fh.write(b"canary renderer")

            with mock.patch.multiple(
                vs,
                RENDERER=default_renderer,
                CANARY_RENDERER=canary_renderer,
                CANARY_SESSIONS=frozenset({"zzc-reviewer-2642"}),
            ):
                self.assertEqual(vs._read_renderer("zzc-reviewer-2642"), b"canary renderer")
                self.assertEqual(vs._read_renderer("zzc-dev55-2642"), b"default renderer")

    def test_empty_canary_config_keeps_default_renderer(self):
        with mock.patch.multiple(vs, CANARY_RENDERER="", CANARY_SESSIONS=frozenset()):
            self.assertEqual(vs._renderer_path_for_session("any-session"), vs.RENDERER)


if __name__ == "__main__":
    unittest.main()
