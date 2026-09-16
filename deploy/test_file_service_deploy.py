#!/usr/bin/env python3
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEPLOY = ROOT / "deploy"


class FileServiceDeploymentTest(unittest.TestCase):
    def test_service_is_supervised_and_write_scope_is_explicit(self):
        text = (DEPLOY / "file-service.service").read_text()
        self.assertIn("ExecStart={{WORKDIR}}/bin/file-service", text)
        self.assertIn("EnvironmentFile={{WORKDIR}}/.file-service.env", text)
        self.assertIn("Restart=always", text)
        self.assertIn("StartLimitBurst=5", text)
        self.assertIn("ReadWritePaths={{FILE_DATA_DIR}}", text)

    def test_probe_is_continuous_and_targets_only_private_readyz(self):
        unit = (DEPLOY / "file-service-livez.service").read_text()
        timer = (DEPLOY / "file-service-livez.timer").read_text()
        check = (DEPLOY / "file-service-livez-check.sh").read_text()
        self.assertIn("OnUnitActiveSec=10s", timer)
        self.assertIn("file-service-livez-check.sh", unit)
        self.assertIn("http://127.0.0.1:8794/readyz", check)
        self.assertIn("systemctl restart", check)

    def test_nginx_exposes_only_public_file_routes(self):
        text = (DEPLOY / "nginx-dingwei.conf").read_text()
        self.assertIn("location = /files/upload", text)
        self.assertIn("location ^~ /files/ref/", text)
        self.assertEqual(text.count("proxy_pass http://127.0.0.1:8793"), 2)
        self.assertNotIn("proxy_pass http://127.0.0.1:8794", text)
        self.assertNotIn("location /internal/", text)


if __name__ == "__main__":
    unittest.main(verbosity=2)
