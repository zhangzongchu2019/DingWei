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
        self.assertEqual(len(discovered), 16, f"测试文件集合变化，须同步审计清单: {sorted(discovered)}")
        for test_file in sorted(discovered):
            with self.subTest(test_file=test_file):
                self.assertEqual(runner.count(test_file), 1, f"测试未接线或重复接线: {test_file}")

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


if __name__ == "__main__":
    unittest.main(verbosity=2)
