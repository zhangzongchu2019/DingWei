import importlib.util
import pathlib
import os
import shutil
import subprocess
import tempfile
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

    def run_build_with_go_version(self, version):
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            shutil.copy2(ROOT / "build.sh", root / "build.sh")
            (root / "scripts").mkdir()
            (root / "fake-bin").mkdir()
            scripts = {
                "scripts/run-build-tests.sh": "#!/bin/sh\nset -eu\ntouch gate-ran\n",
                "fake-bin/git": "#!/bin/sh\nif [ \"$1\" = rev-parse ]; then printf '%s\\n' 0123456789012345678901234567890123456789; fi\n",
                "fake-bin/go": """#!/bin/sh
set -eu
if [ "$1" = version ]; then printf '%s\\n' "$TEST_GO_VERSION"; exit 0; fi
if [ "$1" = build ]; then
  touch build-ran
  while [ "$#" -gt 0 ]; do
    if [ "$1" = -o ]; then printf artifact > "$2"; exit 0; fi
    shift
  done
fi
exit 1
""",
            }
            for name, text in scripts.items():
                script = root / name
                script.write_text(text)
                script.chmod(0o755)
            result = subprocess.run(
                ["sh", str(root / "build.sh"), str(root / "out")],
                cwd=root, capture_output=True, text=True,
                env={"PATH": str(root / "fake-bin") + os.pathsep + os.defpath,
                     "TEST_GO_VERSION": version, "LC_ALL": "C.UTF-8"},
            )
            return result, (root / "gate-ran").exists(), (root / "build-ran").exists()

    def test_build_rejects_go_126_before_tests_or_artifacts(self):
        result, gate, built = self.run_build_with_go_version("go version go1.26.0 linux/amd64")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("构建拒绝", result.stderr)
        self.assertIn("go1.22.2", result.stderr)
        self.assertIn("go1.26.0", result.stderr)
        self.assertFalse(gate)
        self.assertFalse(built)

    def test_build_accepts_exact_go_1222_and_constructs_artifacts(self):
        result, gate, built = self.run_build_with_go_version("go version go1.22.2 linux/amd64")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertTrue(gate)
        self.assertTrue(built)
        self.assertIn("commit=0123456789012345678901234567890123456789", result.stdout)
        self.assertIn("go version go1.22.2 linux/amd64", result.stdout)

    def test_build_checks_entire_go_version_output(self):
        for version in ("go version go1.22.20 linux/amd64", "go version go1.22.2rc1 linux/amd64",
                        "go version go1.22.2 linux/amd64 extra", "go version go1.22.2 linux/amd64\nextra"):
            with self.subTest(version=version):
                result, gate, built = self.run_build_with_go_version(version)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(gate)
                self.assertFalse(built)

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
        self.assertEqual(len(discovered), 28, f"测试文件集合变化，须同步审计清单: {sorted(discovered)}")  # R42 integrates both View test files into the d635aeed inventory
        for test_file in sorted(discovered):
            with self.subTest(test_file=test_file):
                self.assertEqual(runner.count(test_file), 1, f"测试未接线或重复接线: {test_file}")

    def test_ts3_helper_regressions_are_wired_once(self):
        runner = self.read("scripts/helper-test-files.txt")
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
        helper_inventory = self.read("scripts/helper-test-files.txt")
        for name in ("test_update_release_core.py", "test_cwd_import.py", "test_boot_origin.py", "test_update_core_review_regressions.py", "test_self_update_offer.py",
                     "test_sqlite_connection_lifetime.py", "test_private_endpoint.py", "test_pce_retention.py", "test_codex_stale_thread.py", "test_mcp_shim_transport_errors.py", "test_spec_review_r32.py", "test_project_context.py", "test_task_blocker.py", "test_pilot_question.py", "test_pilot_template.py", "test_spec_clarification.py", "test_claude_launch_contract.py", "test_unified_queue.py", "test_dispatch_lifecycle.py"):
            self.assertEqual(helper_inventory.splitlines().count("tools/sessionhelper/tests/" + name), 1)
        python_files = runner.split("PYTHON_TESTS='", 1)[1].split("'", 1)[0].split()
        shell_files = runner.split("SHELL_TESTS='", 1)[1].split("'", 1)[0].split()
        self.assertEqual(len(python_files), 27)
        self.assertEqual(len(shell_files), 2)
        self.assertIn("BUILD_TESTS_OK helper_profiles=2 python=27 shell=2", runner)
        self.assertEqual(runner.count('python3 scripts/run-helper-build-tests.py'), 1)
        actual = {p.relative_to(ROOT).as_posix() for p in (ROOT/'tools/sessionhelper').rglob('test*.py')}
        self.assertEqual(set(helper_inventory.splitlines()), actual)
        self.assertEqual(len(helper_inventory.splitlines()), len(actual))

    def test_quality_gates_fail_before_helper_and_preserve_status(self):
        gates = ('repo-boundary-gate', 'dependency-ratchet', 'tenant-isolation-gate')
        # Use the real entrypoint with controlled child commands: verify failure
        # propagation and that expensive helper tests cannot run ahead of gates.
        for failed in (*gates, None):
            with self.subTest(failed=failed), tempfile.TemporaryDirectory() as directory:
                root = pathlib.Path(directory)
                scripts = root / 'scripts'
                scripts.mkdir()
                shutil.copy2(ROOT / 'scripts/run-build-tests.sh', scripts)
                for gate in gates:
                    rc = 37 if gate == failed else 0
                    (scripts / (gate + '.sh')).write_text(
                        'echo ' + gate + ' >> gate-order\nexit ' + str(rc) + '\n')
                (scripts / 'run-helper-build-tests.py').write_text(
                    'from pathlib import Path\nPath("helper-started").touch()\nraise SystemExit(77)\n')
                result = subprocess.run(['sh', str(scripts / 'run-build-tests.sh')],
                                        cwd=root, capture_output=True, text=True)
                expected = list(gates) if failed is None else list(gates[:gates.index(failed) + 1])
                self.assertEqual((root / 'gate-order').read_text().splitlines(), expected)
                self.assertEqual(result.returncode, 77 if failed is None else 37)
                self.assertEqual((root / 'helper-started').exists(), failed is None)

    def test_missing_helper_file_rejected_before_import_or_execution(self):
        import runpy
        inventory = runpy.run_path(str(ROOT/'scripts/run-helper-build-tests.py'))['inventory']
        files = ['tools/sessionhelper/tests/test_ack_outbox.py',
                 'tools/sessionhelper/tests/test_sessionhelper.py',
                 'tools/sessionhelper/tests/test_update_release_core.py']
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root/'scripts').mkdir()
            (root/'scripts/helper-test-files.txt').write_text('\n'.join(files)+'\n')
            for name in files:
                path=root/name;path.parent.mkdir(parents=True,exist_ok=True)
                path.write_text('raise AssertionError("must not import during inventory check")')
            self.assertEqual(inventory(root), files)
            for name in files:
                path=root/name;content=path.read_text();path.unlink()
                with self.subTest(file=name), self.assertRaisesRegex(ValueError, 'helper inventory mismatch: missing='):
                    inventory(root)
                path.write_text(content)

    def test_helper_inventory_rejects_each_missing_file_and_manifest_entry(self):
        spec = importlib.util.spec_from_file_location(
            "helper_build_gate", ROOT / "scripts/run-helper-build-tests.py")
        gate = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(gate)
        files = gate.inventory(ROOT)
        self.assertEqual(len(files), 97)  # base94 + R60-III two tool suites + R60-I outbound recovery; retain all deletion checks.
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            (root / "scripts").mkdir()
            manifest = root / "scripts/helper-test-files.txt"
            manifest.write_text("\n".join(files) + "\n")
            for name in files:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.touch()
            self.assertEqual(gate.inventory(root), files)
            for name in files:
                with self.subTest(missing_file=name):
                    path = root / name
                    path.unlink()
                    with self.assertRaisesRegex(ValueError, "inventory mismatch"):
                        gate.inventory(root)
                    path.touch()
                with self.subTest(missing_entry=name):
                    manifest.write_text("\n".join(f for f in files if f != name) + "\n")
                    with self.assertRaisesRegex(ValueError, "inventory mismatch"):
                        gate.inventory(root)
                    manifest.write_text("\n".join(files) + "\n")
            manifest.write_text("\n".join(files + files[:1]) + "\n")
            with self.assertRaisesRegex(ValueError, "inventory mismatch"):
                gate.inventory(root)

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
