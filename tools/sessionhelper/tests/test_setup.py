import json
import os
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from sessionhelper.setup import configure_mcp, profile, write_profile, prepare_cli, start_background, main


class SetupTest(unittest.TestCase):
    def make(self, root, mode="desktop", tool="codex"):
        return profile(mode, tool, "https://dw123.cmjrt.com", "alice", "al", "pmagent", root)

    def test_version_entry_has_no_enrollment_or_environment_read(self):
        from sessionhelper.launch import main as launch
        from io import StringIO
        output = StringIO()
        with patch("sys.argv", ["sessionhelper", "--version"]), patch("sys.stdout", output), patch("sessionhelper.launch.env_get", side_effect=AssertionError("version must not inspect credentials")):
            launch()
        self.assertEqual(output.getvalue(), "sessionHelper 0.2.2.2\n")

    def test_legacy_direct_mcp_script_reports_current_version(self):
        from sessionhelper import mcp_shim
        import subprocess, sys
        request = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "initialize"}) + "\n"
        result = subprocess.run([sys.executable, mcp_shim.__file__], input=request, text=True, capture_output=True, timeout=10, check=True)
        self.assertEqual(json.loads(result.stdout)["result"]["serverInfo"]["version"], "0.2.2.2")

    def test_modes_and_reply_events(self):
        with tempfile.TemporaryDirectory() as root:
            for mode, tool in (("desktop", "codex"), ("driver", "claude"), ("driver", "opencode")):
                values = self.make(root, mode, tool)
                self.assertEqual(values["SH_EMIT_SESSION_EVENT"], "1")
                self.assertEqual(values["SH_ENROLL_METHOD"], "feishu")
                self.assertEqual(values["SH_MODE"], mode)
            with self.assertRaises(ValueError):
                self.make(root, "desktop", "claude")

    def test_origin_and_identifier_validation(self):
        for origin in ("http://dw123.cmjrt.com", "https://user:pass@dw123.cmjrt.com", "https://dw123.cmjrt.com/private", "https://dw123.cmjrt.com?token=x"):
            with self.assertRaises(ValueError):
                profile("desktop", "codex", origin, "alice", "al", "pmagent", "/tmp/fixture")
        with self.assertRaises(ValueError):
            profile("desktop", "codex", "https://dw123.cmjrt.com", "alice.x", "al", "pmagent", "/tmp/fixture")

    def test_private_profile_idempotent_preserves_credentials(self):
        with tempfile.TemporaryDirectory() as root:
            credential = Path(root) / "credential.json"
            credential.write_text("fixture-owned-by-existing-user")
            credential.chmod(0o600)
            values = self.make(root)
            path = write_profile(values)
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(write_profile(values), path)
            self.assertEqual(credential.read_text(), "fixture-owned-by-existing-user")
            changed = dict(values, SH_ENROLL_OWNER="bob")
            with self.assertRaises(ValueError):
                write_profile(changed)
            self.assertEqual(json.loads(path.read_text()), values)

    def test_mcp_exact_append_preserves_other_settings_and_private_backup(self):
        with tempfile.TemporaryDirectory() as root:
            config = Path(root) / "config.toml"
            before = b'model = "fixture-model"\n[mcp_servers.other]\ncommand = "fixture"\n'
            config.write_bytes(before)
            result = configure_mcp(self.make(root), config)
            self.assertEqual(result, "dingwei-pmagent")
            self.assertTrue(config.read_bytes().startswith(before))
            parsed = tomllib.loads(config.read_text())
            self.assertEqual(parsed["mcp_servers"][result]["args"], ["-m", "sessionhelper.mcp_shim"])
            self.assertEqual(parsed["mcp_servers"]["other"], {"command": "fixture"})
            backup = Path(str(config) + ".before-dingwei-pmagent-0222")
            self.assertEqual(backup.read_bytes(), before)
            self.assertEqual(backup.stat().st_mode & 0o777, 0o600)
            after = config.read_bytes()
            self.assertEqual(configure_mcp(self.make(root), config), result)
            self.assertEqual(config.read_bytes(), after)
            config.write_text(config.read_text().replace('command = "' + __import__('sys').executable + '"', 'command = "foreign-runtime"'))
            altered = config.read_bytes()
            with self.assertRaises(ValueError):
                configure_mcp(self.make(root), config)
            self.assertEqual(config.read_bytes(), altered)

    def test_cli_not_marked_ready_without_actual_probe(self):
        with patch("sessionhelper.setup.shutil.which", return_value="/fixture/codex"):
            calls = []
            def runner(args, **kwargs):
                calls.append(args)
                return type("Result", (), {"returncode": 0, "stdout": "not-ready"})()
            prompts = iter(["n", "n", "y"])
            with self.assertRaises(ValueError):
                prepare_cli("codex", lambda _: next(prompts), runner)
            self.assertIn("exec", calls[-1])

    def test_background_launcher_no_credentials_in_argv_and_no_duplicate(self):
        with tempfile.TemporaryDirectory() as root:
            values = self.make(root)
            path = write_profile(values)
            calls = []
            def popen(args, **kwargs):
                calls.append((args, kwargs))
                return type("Process", (), {"pid": os.getpid()})()
            pid = start_background(path, values, popen)
            self.assertEqual(pid, os.getpid())
            self.assertTrue(calls[0][1]["start_new_session"])
            self.assertIn("--run-profile", calls[0][0])
            self.assertNotIn("--secret", calls[0][0])
            with self.assertRaises(ValueError):
                start_background(path, values, popen)
            self.assertEqual(len(calls), 1)

    def test_wizard_desktop_pairs_then_starts_only_after_mcp_setup(self):
        with tempfile.TemporaryDirectory() as root:
            answers = iter(["1", "", "alice", "al", "pmagent", root, root, "feishu"])
            with patch.dict(os.environ, {"CODEX_HOME": str(Path(root) / "codex")}), \
                 patch("builtins.input", side_effect=lambda _: next(answers)), \
                 patch("sessionhelper.enroll.prepare_client_env") as enroll, \
                 patch("sessionhelper.setup.start_background", return_value=1234) as start:
                main([])
            values = enroll.call_args.args[0]
            self.assertEqual(values["SH_MODE"], "desktop")
            self.assertTrue((Path(root) / "codex" / "config.toml").exists())
            self.assertEqual(json.loads((Path(root) / "setup-status.json").read_text())["tool_ready"], "MCP_CONFIGURED")
            start.assert_called_once()

    def test_run_profile_refuses_skipped_model_readiness(self):
        with tempfile.TemporaryDirectory() as root:
            values = self.make(root, "driver", "codex")
            path = write_profile(values)
            with patch("sessionhelper.app.main") as helper:
                with self.assertRaises(ValueError):
                    main(["--run-profile", str(path)])
                helper.assert_not_called()

    def test_packaged_mcp_entry_uses_own_stdio_dispatch(self):
        with tempfile.TemporaryDirectory() as root:
            config = Path(root) / "config.toml"
            with patch("sessionhelper.setup.sys.frozen", True, create=True):
                name = configure_mcp(self.make(root), config)
            server = tomllib.loads(config.read_text())["mcp_servers"][name]
            self.assertEqual(server["args"], ["--mcp"])
            self.assertNotIn("PYTHONPATH", server["env"])

    def test_failed_background_spawn_releases_reservation(self):
        with tempfile.TemporaryDirectory() as root:
            values = self.make(root)
            path = write_profile(values)
            def fail(*args, **kwargs):
                raise OSError("synthetic startup failure")
            with self.assertRaises(OSError):
                start_background(path, values, fail)
            self.assertFalse((Path(root) / "helper-process.json").exists())


if __name__ == "__main__":
    unittest.main()
