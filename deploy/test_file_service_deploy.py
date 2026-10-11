#!/usr/bin/env python3
import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
DEPLOY = ROOT / "deploy"


class FileServiceDeploymentTest(unittest.TestCase):
    def test_direct_link_regex_accepts_canonical_sessions_and_rejects_unsafe_paths(self):
        import re

        text = (DEPLOY / "nginx-dingwei.conf").read_text()
        patterns = re.findall(r'location ~ "([^"\n]+)"', text)
        self.assertEqual(len(patterns), 2)
        patterns = [re.compile(re.sub(r'\(\?<([a-z_]+)>', r'(?P<\1>', p)) for p in patterns]
        for extension, pattern in zip(("yml", "zip"), patterns):
            suffix = "/26/1011/0123456789abcdef0123456789abcdef." + extension
            for session in ("timer", "timer.zzc", "timer.z9"):
                self.assertIsNotNone(pattern.fullmatch("/f/u_zhangzongchu/" + session + suffix))
            for session in ("..", "timer.z", "timer..zzc", "timer.zzc.extra", "timer.", "timer.zz-c", "timer/zzc"):
                self.assertIsNone(pattern.fullmatch("/f/u_zhangzongchu/" + session + suffix))

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
        # 🔴 回流合并(2026-09-19)逐块取舍,理由记在这里:
        #   · 引号断言取 **preprod 那条严格的**(必须带引号),不取锚那条 assertRegex ——
        #     锚那条写的是「带不带引号都接受」,而 `0.2.0.6 v2` 正是**不带引号被整批回滚**的:
        #     nginx 会把 `{1,64}` 的 `{` 当块起始 ⇒ pcre2_compile() failed。
        #     严格那条**蕴含**宽松那条,保留宽松的反而会让后来人以为不带引号也行。
        #   · 扩展名串取 **锚** 的新全集:preprod 那串(…avif|mp4…)在新 conf 里**已不再连续出现**
        #     (avif 之后插了 heic),保留它会红在一个已经被取代的旧事实上。
        self.assertIn('location ~ "^/f/', text)
        self.assertIn("jpg|png|gif|webp|avif|heic|bmp|tiff|mp4|webm|ogv|mov|mkv|oga|wav|mp3|m4a|flac|aac|opus|pdf|txt|md|csv|json|log|py|js|go|java|yaml|yml|toml|ini|sh|sql", text)
        self.assertIn("video/quicktime mov", text)
        self.assertIn("audio/ogg oga", text)
        self.assertIn("audio/wav wav", text)
        self.assertIn("audio/mpeg mp3", text)
        self.assertIn("audio/mp4 m4a", text)
        self.assertIn("audio/flac flac", text)
        self.assertIn("audio/aac aac", text)
        self.assertIn("audio/opus opus", text)
        self.assertIn("(?<file_ext>bin|docx|xlsx|pptx|doc|xls|ppt|zip)", text)
        self.assertIn("objects/$file_rel.$file_ext", text)
        self.assertIn("text/plain txt md csv json log py js go java yaml yml toml ini sh sql", text)
        self.assertIn("charset utf-8", text)
        self.assertIn("charset_types text/plain", text)
        self.assertIn("default_type application/octet-stream", text)
        self.assertIn("add_header Content-Disposition attachment always", text)
        self.assertEqual(text.count("add_header X-Content-Type-Options nosniff always"), 2)
        self.assertEqual(text.count("add_header Referrer-Policy no-referrer always"), 2)
        # 两侧写法等价(都取两个 /f/ location 之间那一段);取锚方的切法,
        # 它对引号不敏感 —— 而「必须带引号」已由上面那条严格断言单独钉住,不靠这里。
        import re as _re
        _parts = _re.split(r'location ~ "?\^/f/', text)
        inline = _parts[1]
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
