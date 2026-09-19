#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from deploy.image.supervisord import verify_supervisor_template as verifier

HERE = Path(__file__).resolve().parent
VERIFY = HERE / "verify_supervisor_template.py"
EMPTY = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
ONE = "559aead08264d5795d3909718cdd05abd49572e84fe55590eef31a88a08fdffd"


class ThreeWayGateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.name", "test"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=self.root, check=True)
        self.programs = {
            "fcs": {"fields": {"command": "/app/bin/fcs", "autorestart": "true"}, "environment": {"key_count": 1, "keys_sorted_sha256": ONE}},
            "viewservice": {"fields": {"command": "python3 /app/vs/view_service.py", "autorestart": "true"}, "environment": {"key_count": 0, "keys_sorted_sha256": EMPTY}},
        }
        self.evidence = self.root / "evidence.txt"
        self.evidence.write_text(
            "[program:fcs]\ncommand=/app/bin/fcs\nautorestart=true\n"
            f"environment.key_count=1\nenvironment.keys_sorted_sha256={ONE}\n\n"
            "[program:viewservice]\ncommand=python3 /app/vs/view_service.py\nautorestart=true\n"
            f"environment.key_count=0\nenvironment.keys_sorted_sha256={EMPTY}\n"
        )
        subprocess.run(["git", "add", "evidence.txt"], cwd=self.root, check=True)
        subprocess.run(["git", "commit", "-qm", "evidence"], cwd=self.root, check=True)
        self.commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=self.root, text=True).strip()
        digest = subprocess.check_output(["sha256sum", "evidence.txt"], cwd=self.root, text=True).split()[0]
        self.schema = self.root / "schema.json"
        self.schema.write_text(json.dumps({
            "evidence_source": {"commit": self.commit, "ref": "HEAD", "path": "evidence.txt", "sha256": digest},
            "programs": self.programs,
        }, sort_keys=True) + "\n")
        self.template = self.root / "dingwei.conf.template"
        self.template.write_text(
            '[program:fcs]\ncommand=/app/bin/fcs\nautorestart=true\n'
            f'; t112-environment key_count=1 keys_sorted_sha256={ONE}\n'
            'environment=%(ENVIRONMENT_FCS)s\n\n'
            '[program:viewservice]\ncommand=python3 /app/vs/view_service.py\nautorestart=true\n'
            f'; t112-environment key_count=0 keys_sorted_sha256={EMPTY}\n'
            'environment=%(ENVIRONMENT_VIEWSERVICE)s\n'
        )

    def tearDown(self): self.tmp.cleanup()

    def run_gate(self):
        return subprocess.run([
            "python3", str(VERIFY), "--evidence", str(self.evidence),
            "--schema", str(self.schema), "--template", str(self.template),
        ], cwd=self.root, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)

    def assert_failure(self, failure_class):
        result = self.run_gate(); self.assertNotEqual(result.returncode, 0, result.stdout)
        self.assertIn(f"failure_class={failure_class}", result.stdout)

    def test_positive(self):
        result = self.run_gate(); self.assertEqual(result.returncode, 0, result.stdout); self.assertIn("PASS programs=2", result.stdout)

    def test_evidence_schema_program_gap(self):
        value = json.loads(self.schema.read_text()); del value["programs"]["fcs"]
        self.schema.write_text(json.dumps(value)); self.assert_failure("PROGRAM_SET_MISMATCH_EVIDENCE_SCHEMA")

    def test_schema_template_env_gap(self):
        self.template.write_text(self.template.read_text().replace('environment=%(ENVIRONMENT_FCS)s\n', ''))
        self.assert_failure("TEMPLATE_ENV_PLACEHOLDER_MISSING")

    def test_evidence_schema_extra_env_key(self):
        value = json.loads(self.schema.read_text())
        value["programs"]["fcs"]["environment"] = {"key_count": 2, "keys_sorted_sha256": ONE}
        self.schema.write_text(json.dumps(value)); self.assert_failure("ENV_DIGEST_MISMATCH_EVIDENCE_SCHEMA")

    def test_template_extra_program(self):
        with self.template.open("a") as stream:
            stream.write(f"\n[program:unexpected]\ncommand=/bin/false\n; t112-environment key_count=0 keys_sorted_sha256={EMPTY}\nenvironment=%(ENVIRONMENT_UNEXPECTED)s\n")
        self.assert_failure("PROGRAM_SET_MISMATCH_SCHEMA_TEMPLATE")

    def test_literal_value(self):
        self.template.write_text(self.template.read_text().replace('%(ENVIRONMENT_FCS)s', 'A=literal'))
        self.assert_failure("TEMPLATE_LITERAL_ENV_VALUE")

    def test_swapped_labels(self):
        text = self.template.read_text().replace('/app/bin/fcs', 'TOKEN').replace('python3 /app/vs/view_service.py', '/app/bin/fcs').replace('TOKEN', 'python3 /app/vs/view_service.py')
        self.template.write_text(text); self.assert_failure("FIELD_MISMATCH_SCHEMA_TEMPLATE")

    def test_command_path_drift(self):
        self.template.write_text(self.template.read_text().replace('/app/bin/fcs', '/app/fcs'))
        self.assert_failure("FIELD_MISMATCH_SCHEMA_TEMPLATE")

    def test_evidence_digest_anchor(self):
        self.evidence.write_text(self.evidence.read_text() + " ")
        self.assert_failure("EVIDENCE_DIGEST_MISMATCH")

    def test_placeholder_label_drift(self):
        self.template.write_text(self.template.read_text().replace('ENVIRONMENT_FCS', 'ENVIRONMENT_VIEWSERVICE'))
        self.assert_failure("TEMPLATE_LITERAL_ENV_VALUE")

    def test_template_env_digest_drift(self):
        self.template.write_text(self.template.read_text().replace(ONE, EMPTY))
        self.assert_failure("ENV_DIGEST_MISMATCH_SCHEMA_TEMPLATE")

    def test_declared_environment_transition_is_exact(self):
        value = json.loads(self.schema.read_text())
        value["programs"]["fcs"]["environment"] = {
            "key_count": 2,
            "keys_sorted_sha256": EMPTY,
        }
        value["environment_transitions"] = {
            "fcs": {
                "from": {"key_count": 1, "keys_sorted_sha256": ONE},
                "to": {"key_count": 2, "keys_sorted_sha256": EMPTY},
            }
        }
        self.schema.write_text(json.dumps(value))
        self.template.write_text(
            self.template.read_text().replace(
                f"key_count=1 keys_sorted_sha256={ONE}",
                f"key_count=2 keys_sorted_sha256={EMPTY}",
            )
        )
        result = self.run_gate()
        self.assertEqual(result.returncode, 0, result.stdout)
        self.assertIn("environment_transitions=1", result.stdout)

    def test_environment_transition_does_not_cover_other_drift(self):
        value = json.loads(self.schema.read_text())
        value["programs"]["fcs"]["environment"] = {
            "key_count": 2,
            "keys_sorted_sha256": EMPTY,
        }
        value["environment_transitions"] = {
            "fcs": {
                "from": {"key_count": 1, "keys_sorted_sha256": ONE},
                "to": {"key_count": 3, "keys_sorted_sha256": EMPTY},
            }
        }
        self.schema.write_text(json.dumps(value))
        self.assert_failure("ENV_DIGEST_MISMATCH_EVIDENCE_SCHEMA")

    def test_environment_transition_must_be_removed_after_evidence_catches_up(self):
        value = json.loads(self.schema.read_text())
        value["environment_transitions"] = {
            "fcs": {
                "from": {"key_count": 1, "keys_sorted_sha256": ONE},
                "to": {"key_count": 2, "keys_sorted_sha256": EMPTY},
            }
        }
        self.schema.write_text(json.dumps(value))
        self.assert_failure("ENV_TRANSITION_STALE")


class RepositoryTemplateContractTest(unittest.TestCase):
    def test_workpulse_template_comment_matches_target_schema(self):
        schema_path = HERE / "supervisor-structure.schema.json"
        template_path = HERE / "dingwei.conf.template"
        schema = json.loads(schema_path.read_text())
        schema_programs = verifier.normalize_programs(schema["programs"], "schema")
        template_programs = verifier.parse_template(template_path, schema_programs)

        verifier.compare(schema_programs, template_programs, "SCHEMA_TEMPLATE")
        self.assertEqual(schema_programs["workpulse"]["environment"]["key_count"], 16)
        self.assertEqual(len(verifier.normalize_environment_transitions(schema["environment_transitions"])), 1)


if __name__ == "__main__": unittest.main()
