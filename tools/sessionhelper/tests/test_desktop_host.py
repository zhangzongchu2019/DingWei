import json
from pathlib import Path
import plistlib
import tempfile
import unittest
import io
from contextlib import redirect_stdout
from unittest.mock import patch

from sessionhelper.desktop_host import desktop_probe, require_macos_desktop, main


class DesktopHostTest(unittest.TestCase):
    def app(self, root, name="Codex.app"):
        app = Path(root) / name
        (app / "Contents").mkdir(parents=True)
        with (app / "Contents" / "Info.plist").open("wb") as output:
            plistlib.dump({"CFBundleIdentifier": "com.example.fixture", "CFBundleShortVersionString": "26.1012.1", "CFBundleVersion": "fixture123"}, output)
        return app

    def test_metadata_only_not_signer_login_or_auto_notify_proof(self):
        with tempfile.TemporaryDirectory() as root:
            app = self.app(root)
            result = desktop_probe(system="Darwin", roots=[root])
            self.assertEqual(result["status"], "DESKTOP_APP_METADATA_FOUND")
            self.assertEqual(result["applications"][0]["version"], "26.1012.1")
            self.assertEqual(result["account_state"], "NOT_READ")
            self.assertEqual(result["native_auto_notify"], "NOT_VERIFIED")
            self.assertFalse(result["applications"][0]["signature_verified"])
            self.assertEqual(require_macos_desktop(system="Darwin", selected=app)["applications"], result["applications"])

    def test_no_app_and_multiple_apps_do_not_silently_configure(self):
        with tempfile.TemporaryDirectory() as root:
            with patch("sessionhelper.desktop_host.desktop_probe", return_value=desktop_probe(system="Darwin", roots=[root])):
                with self.assertRaises(ValueError): require_macos_desktop()
            selected = self.app(root)
            self.app(root, "ChatGPT.app")
            result = desktop_probe(system="Darwin", roots=[root])
            self.assertEqual(result["status"], "CHOOSE_DESKTOP_APP")
            with patch("sessionhelper.desktop_host.desktop_probe", return_value=result):
                with self.assertRaises(ValueError): require_macos_desktop()
            self.assertEqual(desktop_probe(system="Darwin", selected=selected)["status"], "DESKTOP_APP_METADATA_FOUND")

    def test_malformed_plist_fields_not_output(self):
        with tempfile.TemporaryDirectory() as root:
            app = self.app(root)
            with (app / "Contents" / "Info.plist").open("wb") as output:
                plistlib.dump({"CFBundleShortVersionString": "fixture\nprivate-body", "CFBundleVersion": "1", "CFBundleIdentifier": "fixture"}, output)
            with self.assertRaises(ValueError) as failure:
                desktop_probe(system="Darwin", selected=app)
            self.assertNotIn("private-body", str(failure.exception))

    def test_info_symlink_never_reads_external_file(self):
        with tempfile.TemporaryDirectory() as root:
            app = self.app(root)
            info = app / "Contents" / "Info.plist"
            info.unlink()
            outside = Path(root) / "external-private-file"
            outside.write_text("must-not-read")
            info.symlink_to(outside)
            with patch("sessionhelper.desktop_host.plistlib.load", side_effect=AssertionError("external data must not be read")):
                with self.assertRaises(ValueError):
                    desktop_probe(system="Darwin", selected=app)
            self.assertEqual(outside.read_text(), "must-not-read")

    def test_nonmac_probe_does_not_read_app_or_config(self):
        result = desktop_probe(system="Linux", selected="/never/read/fixture.app")
        self.assertEqual(result["status"], "MACOS_PROBE_NOT_APPLICABLE")
        self.assertEqual(result["applications"], [])

    def test_native_preflight_cli_rejects_nonmac_and_ambiguous_selection(self):
        for result in (desktop_probe(system='Linux'),
                       {'platform':'Darwin','applications':[]},
                       {'platform':'Darwin','applications':[{},{}]}):
            with self.subTest(result=result), patch('sessionhelper.desktop_host.desktop_probe', return_value=result):
                with self.assertRaises(SystemExit): main(['--require-app'])

    def test_native_preflight_reads_only_selected_synthetic_app_metadata(self):
        with tempfile.TemporaryDirectory() as root:
            app = self.app(root)
            with patch('sessionhelper.desktop_host.platform.system', return_value='Darwin'), redirect_stdout(io.StringIO()) as output:
                main(['--require-app','--desktop-app',str(app)])
            result = json.loads(output.getvalue())
            self.assertEqual(result['applications'][0]['build'],'fixture123')
            self.assertFalse(result['native_auto_notify_implemented'])


if __name__ == "__main__":
    unittest.main()
