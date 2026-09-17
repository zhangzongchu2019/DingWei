#!/usr/bin/env python3
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEPLOY = ROOT / "deploy"


class FileServiceDeploymentTest(unittest.TestCase):
    def test_service_is_supervised_and_write_scope_is_explicit(self):
        text = (DEPLOY / "file-service.service").read_text()
        self.assertIn("ExecStart=/opt/dingwei/file-service/bin/file-service", text)
        self.assertIn("EnvironmentFile={{FILE_SERVICE_ENV_FILE}}", text)
        self.assertIn("Restart=always", text)
        self.assertIn("StartLimitBurst=5", text)
        self.assertIn("ReadWritePaths=/var/lib/dingwei-file-service", text)

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

    def test_local_storage_replaces_minio_and_is_non_executable(self):
        unit = (DEPLOY / "file-service.service").read_text()
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertIn("UMask=0077", unit)
        self.assertIn("NoExecPaths=/var/lib/dingwei-file-service/objects", unit)
        self.assertIn("FS_DATA_ROOT=/var/lib/dingwei-file-service/objects", contract)
        self.assertNotIn("FS_MINIO_", contract)
        self.assertFalse((DEPLOY / "minio.service").exists())
        self.assertFalse((DEPLOY / "minio-file-service-policy.json").exists())

    def test_real_storage_probe_is_not_readyz_only(self):
        probe = (DEPLOY / "file-service-storage-probe.sh").read_text()
        self.assertIn("/internal/files/issue/upload", probe)
        self.assertIn("/files/upload", probe)
        self.assertIn("/internal/files/issue/retrieval", probe)
        self.assertIn("/files/ref/", probe)
        self.assertIn("cmp ", probe)
        self.assertIn("FILE_SERVICE_STORAGE_OK", probe)
        self.assertNotIn("/readyz", probe)

    def test_expiry_methods_distinguish_disposable_from_production_mutation(self):
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertIn("repository integration test against a real temporary", contract)
        self.assertIn("changes no production metadata or clock", contract)
        self.assertIn("Disposable isolated-instance time travel", contract)
        self.assertIn("time-travel one production row", contract)
        self.assertIn("60 to 61", contract)
        self.assertIn("Quota eviction is not an acceptable substitute", contract)


if __name__ == "__main__":
    unittest.main(verbosity=2)
