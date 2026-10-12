import json
import os
import tempfile
import tomllib
import unittest
from pathlib import Path
from unittest.mock import patch

from sessionhelper.setup import configure_mcp, profile, write_profile, prepare_cli, start_background, profile_command, main


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

    def test_profile_rejects_oversized_unix_socket_path(self):
        with self.assertRaises(ValueError):
            self.make("/tmp/" + "fixture" * 30)

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
                 patch("sessionhelper.desktop_host.require_macos_desktop", return_value={"platform": "Darwin", "applications": [{"name": "Codex.app", "version": "fixture1", "build": "1"}]}) as host, \
                 patch("sessionhelper.setup.start_background", return_value=1234) as start:
                main([])
            values = enroll.call_args.args[0]
            self.assertEqual(values["SH_MODE"], "desktop")
            self.assertTrue((Path(root) / "codex" / "config.toml").exists())
            self.assertEqual(json.loads((Path(root) / "setup-status.json").read_text())["tool_ready"], "MCP_CONFIGURED")
            start.assert_called_once()
            host.assert_called_once_with(selected=None)

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

    def test_packaged_wizard_prints_usable_restart_with_spaces_and_web_hint(self):
        import shlex
        from io import StringIO
        with tempfile.TemporaryDirectory(prefix="dw setup ") as root:
            answers = iter(["1", "", "alice", "al", "pmagent", root, root, "Web"])
            output = StringIO()
            binary = "/fixture/Applications/Ding Wei/sessionhelper"
            with patch.dict(os.environ, {"CODEX_HOME": str(Path(root) / "codex")}), \
                 patch("builtins.input", side_effect=lambda _: next(answers)), \
                 patch("sys.stdout", output), \
                 patch("sessionhelper.setup.sys.frozen", True, create=True), \
                 patch("sessionhelper.setup.sys.executable", binary), \
                 patch("sessionhelper.desktop_host.require_macos_desktop", return_value={"platform": "Darwin", "applications": []}), \
                 patch("sessionhelper.enroll.prepare_client_env") as enroll, \
                 patch("sessionhelper.setup.start_background") as start:
                main(["--configure-only"])
            line = next(line for line in output.getvalue().splitlines() if line.startswith("启动： "))
            self.assertEqual(shlex.split(line[len("启动： "):]),
                             [binary, "--run-profile", str(Path(root) / "setup-profile.json")])
            self.assertIn("需要已创建的用户账号", output.getvalue())
            self.assertIn("6位配对码", output.getvalue())
            self.assertNotIn("#pair", output.getvalue())
            enroll.assert_not_called()
            start.assert_not_called()

    def test_source_profile_command_uses_shared_launch_entry(self):
        import sys
        path = Path("/fixture/with spaces/setup-profile.json")
        self.assertEqual(profile_command(path), [sys.executable, "-m", "sessionhelper", "--run-profile", str(path)])
        from sessionhelper.launch import main as launch
        with patch("sys.argv", ["sessionhelper", *profile_command(path)[3:]]), patch("sessionhelper.setup.main") as setup, \
             patch("sessionhelper.launch.env_get", side_effect=AssertionError("profile must be handled before legacy environment")):
            launch()
        setup.assert_called_once_with(["--run-profile", str(path)])

    def test_background_profile_drops_other_device_env_preserves_native_auth(self):
        with tempfile.TemporaryDirectory() as root:
            values = self.make(root)
            path = write_profile(values)
            calls = []
            def popen(args, **kwargs):
                calls.append((args, kwargs))
                return type("Process", (), {"pid": os.getpid()})()
            inherited = {"SH_SECRET": "synthetic-other-secret", "SH_KEY_ID": "other-device",
                         "SH_ENROLL_TOKEN_FILE": "/fixture/other/hosted-token", "SH_MCP_IPC_SOCK": "/fixture/other.sock",
                         "DINGWEI_REPLY_CONTEXT": "synthetic-other-context", "CODEX_HOME": "/fixture/codex",
                         "OPENAI_API_KEY": "synthetic-provider-key"}
            with patch.dict(os.environ, inherited):
                before = dict(os.environ)
                start_background(path, values, popen)
                self.assertEqual(dict(os.environ), before)
            child_env = calls[0][1]["env"]
            for key in ("SH_SECRET", "SH_KEY_ID", "SH_ENROLL_TOKEN_FILE", "SH_MCP_IPC_SOCK", "DINGWEI_REPLY_CONTEXT"):
                self.assertNotIn(key, child_env)
            self.assertEqual(child_env["SH_WS_BASE"], values["SH_WS_BASE"])
            self.assertEqual(child_env["DINGWEI_MCP_IPC_SOCK"], values["DINGWEI_MCP_IPC_SOCK"])
            self.assertEqual(child_env["CODEX_HOME"], inherited["CODEX_HOME"])
            self.assertEqual(child_env["OPENAI_API_KEY"], inherited["OPENAI_API_KEY"])

    def test_run_profile_uses_own_credential_not_inherited_hosted_token(self):
        from sessionhelper.enroll import prepare_client_env, write_private_file
        with tempfile.TemporaryDirectory() as root:
            state = Path(root) / "own"
            values = self.make(str(state))
            path = write_profile(values)
            write_private_file(str(state / "setup-status.json"), json.dumps({"mode": "desktop", "tool_ready": "MCP_CONFIGURED"}))
            write_private_file(str(state / "credential.json"), json.dumps({"agent_name": "ownagent", "secret": "dwa_" + "a" * 64}))
            foreign = Path(root) / "foreign-token"
            write_private_file(str(foreign), "synthetic-token-from-other-device")
            native = {"SH_ENROLL_TOKEN_FILE": str(foreign), "SH_SECRET": "synthetic-other-secret",
                      "SH_KEY_ID": "other-device", "SH_OWNER": "otherowner", "SH_MCP_IPC_SOCK": "/fixture/other.sock",
                      "DINGWEI_REPLY_CONTEXT": "synthetic-other-context", "CODEX_HOME": "/fixture/codex"}
            def run():
                def no_output(_):
                    raise AssertionError("existing own credential must not request or print a code")
                env, credential, actual_state = prepare_client_env(os.environ, show=no_output, notify=no_output, transport=object())
                self.assertEqual(credential.agent_name, "ownagent")
                self.assertEqual(actual_state, str(state))
                self.assertEqual(env["SH_KEY_ID"], "enrolled")
                self.assertNotIn("SH_OWNER", env)
                self.assertNotIn("SH_ENROLL_TOKEN_FILE", env)
                self.assertNotIn("DINGWEI_REPLY_CONTEXT", env)
                self.assertEqual(os.environ["CODEX_HOME"], native["CODEX_HOME"])
                raise RuntimeError("synthetic helper stopped")
            with patch.dict(os.environ, native), patch("sessionhelper.app.main", side_effect=run), \
                 patch("sessionhelper.enroll.enroll_with_token", side_effect=AssertionError("must not claim other device's token")):
                before = dict(os.environ)
                with self.assertRaisesRegex(RuntimeError, "synthetic helper stopped"):
                    main(["--run-profile", str(path)])
                self.assertEqual(dict(os.environ), before)
            self.assertEqual(foreign.read_text(), "synthetic-token-from-other-device")

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
