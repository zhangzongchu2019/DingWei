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


class ProductionRendererTest(unittest.TestCase):
    def test_all_sessions_use_single_production_renderer(self):
        with tempfile.TemporaryDirectory() as tmp:
            renderer = os.path.join(tmp, "production-renderer.html")
            with open(renderer, "wb") as fh:
                fh.write(b"production renderer")
            with mock.patch.object(vs, "RENDERER_PATH", renderer):
                self.assertEqual(vs._read_renderer(), b"production renderer")

    def test_all_html_artifacts_are_free_of_production_backdoors(self):
        root = os.path.dirname(vs.__file__)
        forbidden = ("useMockApi", "?mock=1", "params.get('unlocked')", "mockEvents",
                     "mock-model", "demo-1a2b", "unlockCode: '1460'")
        html_files = [name for name in os.listdir(root) if name.endswith(".html")]
        self.assertEqual(html_files, ["renderer-canary-sse.html"])
        for name in html_files:
            with open(os.path.join(root, name), encoding="utf-8") as fh:
                text = fh.read()
            for token in forbidden:
                self.assertNotIn(token, text, f"{token!r} remains in {name}")

    def test_session_query_cannot_select_target_and_missing_session_fails_closed(self):
        handler = object.__new__(vs._Handler)
        handler.path = "/status?session=attacker-manager-0000"
        handler.headers = {}
        with mock.patch.object(vs, "DEFAULT_SESSION", ""):
            self.assertEqual(handler._session(), "")
        with mock.patch.object(vs, "DEFAULT_SESSION", "fixed-manager-2642"):
            self.assertEqual(handler._session(), "fixed-manager-2642")
        handler.headers = {"X-DW-Session": "trusted-manager-2642"}
        self.assertEqual(handler._session(), "trusted-manager-2642")

    def test_status_rejects_request_without_trusted_or_fixed_session(self):
        handler = object.__new__(vs._Handler)
        handler.path = "/status?session=attacker-manager-0000"
        handler.headers = {}
        handler.send_error = mock.Mock()
        with mock.patch.object(vs, "DEFAULT_SESSION", ""):
            handler._serve_status()
        handler.send_error.assert_called_once_with(400, "session required")

    def _render_for_public_base(self, public_base):
        handler = object.__new__(vs._Handler)
        handler.headers = {"X-DW-Session": "zzc-tester-2642"}
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
            self.assertIn(f"'/v2c/view/zzc-tester-2642/{endpoint}'", rendered)
        self.assertNotIn("'/view/zzc-tester-2642/", rendered)

    def test_default_public_view_base_preserves_main_view_paths(self):
        rendered = self._render_for_public_base("/view")
        for endpoint in ("events", "input", "interrupt", "status", "page"):
            self.assertIn(f"'/view/zzc-tester-2642/{endpoint}'", rendered)

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
        with mock.patch.object(vs, "_hub_post", side_effect=fake_post):
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


class ControlForwardTest(unittest.TestCase):
    """走甲最小 control(pre-injection cancel):过 page 写权 → 转发 hub、G-ADDR 线格式。"""

    def setUp(self):
        with vs._state_lock:
            vs._sessions.clear()

    def test_cancel_with_write_permission_forwards_g_addr(self):
        sess = "zzc-dev55-2642"
        with mock.patch.object(vs, "_ensure_ready", return_value={"viewer_id": "v1", "token": "wtok"}), \
             mock.patch.object(vs, "_hub_post", return_value=(200, {"ok": True})) as post:
            ok, code = vs._forward_control(sess, "cancel", "msg_abc", "d-001")
        self.assertTrue(ok)
        self.assertEqual(code, 200)
        # 锁定线格式(VS→hub, dev55 2026-07-21):POST /internal/view/{session}/cancel,
        # 写权(viewer_id+token) + G-ADDR(message_id+delivery_id,注入前无 turn_id);
        # path 即表 cancel,不带 kind。
        post.assert_called_once_with(vs.HUB_INTERNAL, f"/internal/view/{sess}/cancel", {
            "viewer_id": "v1", "token": "wtok",
            "message_id": "msg_abc", "delivery_id": "d-001",
        })

    def test_no_write_permission_does_not_forward(self):
        # 无 writer token = 未解锁/无写权 → 不转发(别裸),403。
        with mock.patch.object(vs, "_ensure_ready", return_value={"viewer_id": "v1", "token": ""}), \
             mock.patch.object(vs, "_hub_post") as post:
            ok, code = vs._forward_control("s", "cancel", "msg_abc", "d-001")
        self.assertFalse(ok)
        self.assertEqual(code, 403)
        post.assert_not_called()

    def test_unknown_kind_rejected(self):
        # 走甲最小 scope:仅 cancel;其余 control kind 拒(400),不碰 hub。
        with mock.patch.object(vs, "_hub_post") as post:
            ok, code = vs._forward_control("s", "resume", "msg_abc", "d-001")
        self.assertFalse(ok)
        self.assertEqual(code, 400)
        post.assert_not_called()

    def test_missing_g_addr_rejected(self):
        with mock.patch.object(vs, "_hub_post") as post:
            ok, code = vs._forward_control("s", "cancel", "", "d-001")  # 缺 message_id
        self.assertFalse(ok)
        self.assertEqual(code, 400)
        post.assert_not_called()


if __name__ == "__main__":
    unittest.main()
