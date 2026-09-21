import pathlib
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]


class BuildEntrypointContractTest(unittest.TestCase):
    def read(self, name):
        return (ROOT / name).read_text(encoding="utf-8")

    def test_build_sh_owns_gate_flags_and_deploy_provenance(self):
        text = self.read("build.sh")
        self.assertIn("deploy.test_generate_console_seed", text)
        self.assertIn("view-service/test_md_render_core.py", text)
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


if __name__ == "__main__":
    unittest.main(verbosity=2)
