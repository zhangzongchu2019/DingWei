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

    def test_minio_is_pinned_private_and_least_privilege(self):
        unit = (DEPLOY / "minio.service").read_text()
        policy = (DEPLOY / "minio-file-service-policy.json").read_text()
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertIn("127.0.0.1:9000", unit)
        self.assertIn("127.0.0.1:9001", unit)
        self.assertIn("/var/lib/dingwei-minio", unit)
        self.assertIn("RELEASE.2025-09-07T16-13-09Z.hotfix.28f01c988", contract)
        self.assertIn("86e8afc8409b7b803ce91ea91f7d3551aff93ea63d8d242e64a317e95d4cd9d5", contract)
        for action in ("s3:PutObject", "s3:GetObject", "s3:DeleteObject"):
            self.assertIn(action, policy)
        for forbidden in ("s3:*", "admin:", "s3:CreateBucket", "s3:PutBucketPolicy"):
            self.assertNotIn(forbidden, policy)

    def test_real_minio_probe_is_not_readyz_only(self):
        probe = (DEPLOY / "file-service-minio-probe.sh").read_text()
        self.assertIn("/internal/files/issue/upload", probe)
        self.assertIn("/files/upload", probe)
        self.assertIn("/internal/files/issue/retrieval", probe)
        self.assertIn("/files/ref/", probe)
        self.assertIn("cmp ", probe)
        self.assertIn("FILE_SERVICE_MINIO_OK", probe)
        self.assertNotIn("/readyz", probe)

    def test_expiry_methods_distinguish_disposable_from_production_mutation(self):
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertIn("isolated short-retention instance", contract)
        self.assertIn("does not modify production metadata", contract)
        self.assertIn("Production-row time travel", contract)
        self.assertIn("direct production database mutation", contract)
        self.assertIn("Quota eviction is not an acceptable substitute", contract)


if __name__ == "__main__":
    unittest.main(verbosity=2)
