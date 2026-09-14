#!/usr/bin/env python3
from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent
VERIFY = HERE / "verify_supervisor_template.py"


class ThreeWayGateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        subprocess.run(["git", "init", "-q"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.name", "test"], cwd=self.root, check=True)
        subprocess.run(["git", "config", "user.email", "test@example.invalid"], cwd=self.root, check=True)
        self.programs = {
            "fcs": {"fields": {"command": "/app/bin/fcs", "autorestart": "true"}, "environment_keys": ["WP_SECRET_KEY"]},
            "viewservice": {"fields": {"command": "python3 /app/vs/view_service.py", "autorestart": "true"}, "environment_keys": ["FEISHU_APP_SECRET"]},
        }
        self.evidence = self.root / "evidence.json"
        self.evidence.write_text(json.dumps({"programs": self.programs}, sort_keys=True) + "\n")
        subprocess.run(["git", "add", "evidence.json"], cwd=self.root, check=True)
        subprocess.run(["git", "commit", "-qm", "evidence"], cwd=self.root, check=True)
        self.commit = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=self.root, text=True).strip()
        digest = subprocess.check_output(["sha256sum", "evidence.json"], cwd=self.root, text=True).split()[0]
        self.schema = self.root / "schema.json"
        self.schema.write_text(json.dumps({
            "evidence_source": {"commit": self.commit, "ref": "HEAD", "path": "evidence.json", "sha256": digest},
            "programs": self.programs,
        }, sort_keys=True) + "\n")
        self.template = self.root / "dingwei.conf.template"
        self.template.write_text(
            '[program:fcs]\ncommand=/app/bin/fcs\nautorestart=true\n'
            'environment=WP_SECRET_KEY="%(ENV_WP_SECRET_KEY)s"\n\n'
            '[program:viewservice]\ncommand=python3 /app/vs/view_service.py\nautorestart=true\n'
            'environment=FEISHU_APP_SECRET="%(ENV_FEISHU_APP_SECRET)s"\n'
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
        self.template.write_text(self.template.read_text().replace('environment=WP_SECRET_KEY="%(ENV_WP_SECRET_KEY)s"\n', ''))
        self.assert_failure("ENV_KEY_MISMATCH_SCHEMA_TEMPLATE")

    def test_evidence_schema_extra_env_key(self):
        value = json.loads(self.schema.read_text())
        value["programs"]["fcs"]["environment_keys"].append("UNEXPECTED_KEY")
        self.schema.write_text(json.dumps(value))
        self.assert_failure("ENV_KEY_MISMATCH_EVIDENCE_SCHEMA")

    def test_template_extra_program(self):
        with self.template.open("a") as stream:
            stream.write("\n[program:unexpected]\ncommand=/bin/false\n")
        self.assert_failure("PROGRAM_SET_MISMATCH_SCHEMA_TEMPLATE")

    def test_literal_value(self):
        self.template.write_text(self.template.read_text().replace('%(ENV_WP_SECRET_KEY)s', 'literal'))
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


if __name__ == "__main__": unittest.main()
