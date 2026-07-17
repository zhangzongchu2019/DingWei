import io
import json
import os
import tempfile
import threading
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
            vs._sessions["owner1-developer-0000"] = {
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
            state = vs._ensure_ready("owner1-developer-0000")

        self.assertEqual(state["viewer_id"], "viewer-stable")
        self.assertEqual(state["code"], "8231")
        self.assertEqual(state["token"], "unlock-token")
        self.assertTrue(state["can_write"])
        post.assert_called_once_with(vs.HUB_INTERNAL, "/internal/view/authorize", {
            "session": "owner1-developer-0000", "viewer_id": "viewer-stable",
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
                CANARY_SESSIONS=frozenset({"owner1-reviewer-0000"}),
            ):
                self.assertEqual(vs._read_renderer("owner1-reviewer-0000"), b"canary renderer")
                self.assertEqual(vs._read_renderer("owner1-developer-0000"), b"default renderer")

    def test_empty_canary_config_keeps_default_renderer(self):
        with mock.patch.multiple(vs, CANARY_RENDERER="", CANARY_SESSIONS=frozenset()):
            self.assertEqual(vs._renderer_path_for_session("any-session"), vs.RENDERER)

    def _render_for_public_base(self, public_base):
        handler = object.__new__(vs._Handler)
        handler.headers = {"X-DW-Session": "owner1-tester-0000"}
        handler.wfile = io.BytesIO()
        handler.send_response = mock.Mock()
        handler.send_header = mock.Mock()
        handler.end_headers = mock.Mock()
        renderer = b"const API={events:'/events',input:'/input',interrupt:'/interrupt',status:'/status',page:'/page'}"
        with mock.patch.object(vs, "PUBLIC_VIEW_BASE", public_base), \
                mock.patch.object(vs, "_read_renderer", return_value=renderer):
            handler._serve_file()
        return handler.wfile.getvalue().decode()

    def test_public_view_base_rewrites_canary_api_paths_once(self):
        rendered = self._render_for_public_base("/v2c/view")
        for endpoint in ("events", "input", "interrupt", "status", "page"):
            self.assertIn(f"'/v2c/view/owner1-tester-0000/{endpoint}'", rendered)
        self.assertNotIn("'/view/owner1-tester-0000/", rendered)

    def test_default_public_view_base_preserves_main_view_paths(self):
        rendered = self._render_for_public_base("/view")
        for endpoint in ("events", "input", "interrupt", "status", "page"):
            self.assertIn(f"'/view/owner1-tester-0000/{endpoint}'", rendered)

    def test_public_view_base_validation_fails_closed(self):
        self.assertEqual(vs._validate_public_view_base(" /v2c/view "), "/v2c/view")
        for value in ("", "/", "v2c/view", "/v2c/view/", "/v2c//view", "/v2c/../view",
                      "/v2c/./view", "/v2c/view?session=x", "/v2c/view#fragment", "https://host/v2c/view"):
            with self.subTest(value=value):
                with self.assertRaises(ValueError):
                    vs._validate_public_view_base(value)


class ViewerStabilityTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.old_file = vs.STATE_FILE
        vs.STATE_FILE = os.path.join(self.tmp.name, "state.json")
        with vs._state_lock:
            vs._sessions.clear()
        with vs._ready_locks_lock:
            vs._ready_locks.clear()

    def tearDown(self):
        vs.STATE_FILE = self.old_file
        self.tmp.cleanup()

    def test_parallel_first_requests_register_only_one_viewer(self):
        calls = []

        def fake_post(_base, path, obj):
            calls.append((path, dict(obj)))
            if not obj.get("viewer_id"):
                return 200, {"viewer_id": "viewer-stable", "code": "6613"}
            return 200, {"viewer_id": "viewer-stable", "can_write": False}

        results = []
        with mock.patch.object(vs, "_hub_post", side_effect=fake_post), \
                mock.patch.object(vs, "AUTO_UNLOCK", False):
            threads = [threading.Thread(target=lambda: results.append(vs._ensure_ready("reviewer"))) for _ in range(2)]
            for thread in threads:
                thread.start()
            for thread in threads:
                thread.join()

        registrations = [obj for path, obj in calls if path == "/internal/view/authorize" and not obj.get("viewer_id")]
        self.assertEqual(len(registrations), 1)
        self.assertEqual([result["viewer_id"] for result in results], ["viewer-stable", "viewer-stable"])
        self.assertEqual([result["code"] for result in results], ["6613", "6613"])

    def test_transient_authorize_failure_keeps_cached_viewer_and_unlock(self):
        state = vs._sess_state("reviewer")
        state.update({"viewer_id": "viewer-stable", "code": "6613", "token": "token", "can_write": True})

        with mock.patch.object(vs, "_hub_post", return_value=(0, {"error": "timeout"})):
            result = vs._ensure_ready("reviewer")

        self.assertEqual(result["viewer_id"], "viewer-stable")
        self.assertEqual(result["code"], "6613")
        self.assertEqual(result["token"], "token")
        self.assertTrue(result["can_write"])


if __name__ == "__main__":
    unittest.main()
