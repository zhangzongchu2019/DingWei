#!/usr/bin/env python3
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEPLOY = ROOT / "deploy"


class FileServiceDeploymentTest(unittest.TestCase):
    def test_owner_selected_limits_are_pinned_in_deployment_contract(self):
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertEqual(contract.count("FS_RETENTION=168h"), 1)
        self.assertEqual(contract.count("FS_MAX_FILE_BYTES=52428800"), 1)

    def test_service_is_supervised_and_write_scope_is_explicit(self):
        text = (DEPLOY / "file-service.service").read_text()
        self.assertIn("ExecStart=/opt/dingwei/file-service/bin/file-service", text)
        self.assertIn("EnvironmentFile={{FILE_SERVICE_ENV_FILE}}", text)
        self.assertIn("Restart=always", text)
        self.assertIn("StartLimitBurst=5", text)
        self.assertIn("ReadWritePaths=/var/lib/dingwei-file-service", text)
        self.assertIn("User=dingwei-file", text)
        self.assertIn("Group=dingwei-file", text)
        self.assertIn("UMask=0077", text)

    def test_nginx_uses_only_the_dedicated_object_read_group(self):
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertIn("www-data", contract)
        self.assertIn("dingwei-file-read", contract)
        self.assertIn("file-service user `dingwei-file` and nginx user `www-data`", contract)
        self.assertIn("Membership\n  of `dingwei-file` is required", contract)
        self.assertIn("Do not add nginx to the `dingwei-file` primary group", contract)
        for mode in ("`0710`", "`2750`", "`0640`", "`0600`"):
            self.assertIn(mode, contract)
        self.assertIn("must never belong to\n  `dingwei-file-read`", contract)

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
        self.assertNotIn("location ^~ /files/ref/", text)
        self.assertEqual(text.count("proxy_pass http://127.0.0.1:8793"), 1)
        self.assertNotIn("proxy_pass http://127.0.0.1:8794", text)
        self.assertNotIn("location /internal/", text)

    def test_direct_links_inline_only_whitelisted_extensions(self):
        text = (DEPLOY / "nginx-dingwei.conf").read_text()
        self.assertIn("location ~ ^/f/", text)
        self.assertIn("jpg|png|gif|webp|avif|heic|bmp|tiff|mp4|webm|ogv|mov|mkv|oga|wav|pdf|txt|md|csv|json|log|py|js|go|java|yaml|yml|toml|ini|sh|sql", text)
        self.assertIn("video/quicktime mov", text)
        self.assertIn("audio/ogg oga", text)
        self.assertIn("audio/wav wav", text)
        self.assertIn("(?<file_ext>bin|docx|xlsx|pptx|doc|xls|ppt|zip)", text)
        self.assertIn("objects/$file_rel.$file_ext", text)
        self.assertIn("text/plain txt md csv json log py js go java yaml yml toml ini sh sql", text)
        self.assertIn("charset utf-8", text)
        self.assertIn("charset_types text/plain", text)
        self.assertIn("default_type application/octet-stream", text)
        self.assertIn("add_header Content-Disposition attachment always", text)
        self.assertEqual(text.count("add_header X-Content-Type-Options nosniff always"), 2)
        self.assertEqual(text.count("add_header Referrer-Policy no-referrer always"), 2)
        inline = text.split("location ~ ^/f/", 1)[1].split("location ~ ^/f/", 1)[0]
        self.assertNotIn("Content-Disposition", inline)
        self.assertIn("location /f/", text)
        self.assertIn("return 404", text)

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
        self.assertIn('["url"]', probe)
        self.assertIn('curl --fail --silent --show-error "$file_url"', probe)
        self.assertIn("cmp ", probe)
        self.assertIn("FILE_SERVICE_STORAGE_OK", probe)
        self.assertNotIn("/readyz", probe)

    def test_expiry_methods_distinguish_disposable_from_production_mutation(self):
        contract = (DEPLOY / "H-FILE-SERVICE-DEPLOYMENT.md").read_text()
        self.assertIn("candidate-binary isolated verification", contract)
        self.assertIn("verify-retention --work-dir", contract)
        self.assertIn("RETENTION_DIRECTORY_OK", contract)
        self.assertIn("production metadata, data root or host clock", contract)
        self.assertIn("Disposable isolated-instance time travel", contract)
        self.assertIn("time-travel one production row", contract)
        self.assertIn("exactly 7 days", contract)
        self.assertIn("Quota eviction is not an acceptable substitute", contract)


if __name__ == "__main__":
    unittest.main(verbosity=2)
