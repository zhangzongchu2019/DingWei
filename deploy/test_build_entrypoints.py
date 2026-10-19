import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class BuildEntrypointContractTest(unittest.TestCase):
    def read(self, name):
        return (ROOT / name).read_text(encoding="utf-8")

    def test_build_sh_owns_gate_flags_and_deploy_provenance(self):
        text = self.read("build.sh")
        self.assertIn("./scripts/run-build-tests.sh", text)
        self.assertIn("-trimpath -buildvcs=false", text)
        self.assertIn("internal/version.Commit=${COMMIT}", text)
        self.assertIn('SOURCE_COMMIT', text)
        self.assertIn('部署制品拒绝脏工作树', text)

    def test_nodeagent_is_a_peer_release_artifact(self):
        text = self.read("build.sh")
        # Keep nodeagent in the same loop as Hub-facing binaries: this gives it
        # the identical GOOS/GOARCH, reproducibility flags and Commit injection.
        self.assertIn("for c in workpulse fcs file-service nodeagent; do", text)
        self.assertIn('sha256sum "$OUT/workpulse" "$OUT/fcs" "$OUT/file-service" "$OUT/nodeagent"', text)
        self.assertEqual(text.count('internal/version.Commit=${COMMIT}'), 1)

    def test_make_build_and_docker_do_not_copy_build_flags(self):
        text = self.read("Makefile")
        build = text.split("build:\n", 1)[1].split("\nrun:", 1)[0]
        docker = text.split("docker:\n", 1)[1].split("\nsessionhelper:", 1)[0]
        self.assertIn("./build.sh ./bin", build)
        self.assertNotIn("go build", build)
        self.assertIn("--build-arg SOURCE_COMMIT", docker)

    def test_docker_builder_runs_gate_and_verifies_embedded_commit(self):
        text = self.read("Dockerfile")
        self.assertIn('RUN test -n "$SOURCE_COMMIT"', text)
        self.assertIn('RUN SOURCE_COMMIT="$SOURCE_COMMIT" ./build.sh /out', text)
        self.assertIn('/out/workpulse version', text)
        self.assertIn('org.opencontainers.image.revision=$SOURCE_COMMIT', text)
        self.assertNotIn('go build -trimpath', text)

    def test_install_never_reuses_or_directly_builds_an_unverified_binary(self):
        text = self.read("deploy/install.sh")
        self.assertIn('SOURCE_COMMIT="$source_commit" ./build.sh', text)
        self.assertIn('"$WORKDIR/bin/workpulse" version', text)
        self.assertNotIn("reuse existing bin/workpulse", text)
        self.assertNotIn("go build -o bin/workpulse", text)

    def test_every_deploy_and_view_service_test_is_wired_once(self):
        runner = self.read("scripts/run-build-tests.sh")
        discovered = set()
        for directory in (ROOT / "deploy", ROOT / "view-service"):
            for pattern in ("test*.py", "test*.sh"):
                discovered.update(path.relative_to(ROOT).as_posix() for path in directory.rglob(pattern))
        self.assertEqual(len(discovered), 25, f"测试文件集合变化，须同步审计清单: {sorted(discovered)}")  # 24→25: hosted Claude offline fixtures are part of canonical
        for test_file in sorted(discovered):
            with self.subTest(test_file=test_file):
                self.assertEqual(runner.count(test_file), 1, f"测试未接线或重复接线: {test_file}")

    def test_ts3_helper_regressions_are_wired_once(self):
        runner = self.read("scripts/run-build-tests.sh")
        for test_file in (
            "tools/sessionhelper/test_supervise_connection_loops.py",
            "tools/sessionhelper/tests/test_connection_receive_quiescence.py",
            "tools/sessionhelper/tests/test_connection_reconnect_backoff.py",
            "tools/sessionhelper/tests/test_connection_claim_cancellation.py",
            "tools/sessionhelper/tests/test_terminal_start_observation.py",
        ):
            with self.subTest(test_file=test_file):
                self.assertEqual(runner.count(test_file), 1)
                self.assertIn('if __name__ == "__main__":', self.read(test_file))
                self.assertIn("unittest.main(", self.read(test_file))

    def test_update_modules_and_canonical_counts_are_wired(self):
        runner = self.read("scripts/run-build-tests.sh")
        for name in ("test_update_release_core.py", "test_update_core_review_regressions.py", "test_self_update_offer.py",
                     "test_sqlite_connection_lifetime.py", "test_mcp_shim_transport_errors.py"):
            self.assertEqual(runner.count("tools/sessionhelper/tests/" + name), 1)
        python_files = runner.split("PYTHON_TESTS='", 1)[1].split("'", 1)[0].split()
        shell_files = runner.split("SHELL_TESTS='", 1)[1].split("'", 1)[0].split()
        self.assertEqual(len(python_files), 50)
        self.assertEqual(len(shell_files), 2)
        self.assertIn("BUILD_TESTS_OK python=50 shell=2", runner)

    def test_build_make_and_ci_share_the_canonical_test_entrypoint(self):
        self.assertIn("./scripts/run-build-tests.sh", self.read("build.sh"))
        makefile = self.read("Makefile")
        self.assertIn("./build.sh --test-only", makefile)
        self.assertNotIn("python3 -m unittest", makefile)
        workflow = self.read(".github/workflows/speclint.yml")
        self.assertIn("run: make test", workflow)

    def test_view_unlock_integration_has_one_configurable_timeout(self):
        text = self.read("view-service/test_view_unlock_v2_integration.py")
        self.assertIn('TEST_TIMEOUT = float(os.environ.get("VS_TEST_TIMEOUT", "2"))', text)
        self.assertEqual(text.count("timeout=TEST_TIMEOUT"), 3)
        self.assertNotIn("timeout=2", text)

    def test_view_page_store_has_one_configurable_timeout(self):
        text = self.read("view-service/test_view_page_store.py")
        self.assertIn('TEST_TIMEOUT = float(os.environ.get("VPS_TEST_TIMEOUT", "2"))', text)
        # 7 → 9：R-V2-0102-VIEW-MENTION-ROUTE（e71d45d9）的 test_mentions_pass_through_and_receipts_return
        # 新增两处（urlopen 与 thread.join），同用可配置超时；漏改此计数致 build.sh 测试门红（tosonnet55tc2k 发现）。
        self.assertEqual(text.count("timeout=TEST_TIMEOUT"), 9)
        self.assertNotIn("timeout=1", text)
        self.assertNotIn("timeout=2", text)
        self.assertIn("time.sleep(0.005)", text)


if __name__ == "__main__":
    unittest.main(verbosity=2)
