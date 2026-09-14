#!/usr/bin/env python3
"""T-112 three-way gate: redacted evidence <-> schema <-> template."""
from __future__ import annotations

import argparse
import configparser
import hashlib
import json
import re
import subprocess
import sys
from pathlib import Path

ENV_PLACEHOLDER = re.compile(r'^%\(ENVIRONMENT_([A-Z][A-Z0-9_]*)\)s$')
ENV_CONTRACT = re.compile(r'^; t112-environment key_count=(\d+) keys_sorted_sha256=([0-9a-f]{64})$')
SHA256 = re.compile(r'^[0-9a-f]{64}$')


def fail(kind: str, detail: str) -> None:
    print(f"FAIL failure_class={kind} detail={detail}")
    raise SystemExit(1)


def git(*args: str, stdin: bytes | None = None) -> bytes:
    try:
        return subprocess.run(
            ["git", *args], input=stdin, check=True, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        ).stdout
    except subprocess.CalledProcessError as exc:
        fail("EVIDENCE_GIT_ANCHOR_INVALID", exc.stderr.decode(errors="replace").strip())


def resolve_ref(ref: str) -> str:
    candidates = [ref]
    if ref.startswith("refs/heads/"):
        candidates.append("refs/remotes/origin/" + ref.removeprefix("refs/heads/"))
    for candidate in candidates:
        result = subprocess.run(["git", "rev-parse", "--verify", candidate], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if result.returncode == 0:
            return candidate
    fail("EVIDENCE_GIT_ANCHOR_INVALID", f"ref not fetched: {ref}")


def load_json(path: Path, kind: str) -> dict:
    try:
        value = json.loads(path.read_text())
    except (OSError, json.JSONDecodeError) as exc:
        fail(kind, str(exc))
    if not isinstance(value, dict):
        fail(kind, "root must be an object")
    return value


def normalize_programs(raw: object, source: str) -> dict[str, dict]:
    if not isinstance(raw, dict) or not raw:
        fail("PROGRAM_SET_INVALID", f"{source}: programs must be a non-empty object")
    out: dict[str, dict] = {}
    for name, value in raw.items():
        if not isinstance(name, str) or not isinstance(value, dict):
            fail("PROGRAM_SET_INVALID", f"{source}: invalid program entry")
        fields, environment = value.get("fields"), value.get("environment")
        if not isinstance(fields, dict) or any(not isinstance(k, str) or not isinstance(v, str) for k, v in fields.items()):
            fail("PROGRAM_FIELDS_INVALID", f"{source}:{name}")
        if not isinstance(environment, dict) or set(environment) != {"key_count", "keys_sorted_sha256"}:
            fail("ENV_DIGEST_INVALID", f"{source}:{name}")
        count, digest = environment.get("key_count"), environment.get("keys_sorted_sha256")
        if not isinstance(count, int) or count < 0 or not isinstance(digest, str) or not SHA256.fullmatch(digest):
            fail("ENV_DIGEST_INVALID", f"{source}:{name}")
        out[name] = {"fields": dict(sorted(fields.items())), "environment": {"key_count": count, "keys_sorted_sha256": digest}}
    return dict(sorted(out.items()))


def parse_evidence(content: str) -> dict[str, dict]:
    parser = configparser.RawConfigParser(strict=True)
    parser.optionxform = str
    try:
        parser.read_string(content)
    except configparser.Error as exc:
        fail("EVIDENCE_PARSE_ERROR", str(exc))
    raw: dict[str, dict] = {}
    for section in parser.sections():
        if not section.startswith("program:"):
            fail("EVIDENCE_SECTION_INVALID", section)
        name = section.removeprefix("program:")
        fields: dict[str, str] = {}
        environment: dict[str, object] = {}
        for key, value in parser.items(section, raw=True):
            if key == "environment.key_count":
                try:
                    environment["key_count"] = int(value)
                except ValueError:
                    fail("ENV_DIGEST_INVALID", f"evidence:{name}:key_count")
            elif key == "environment.keys_sorted_sha256":
                environment["keys_sorted_sha256"] = value
            else:
                fields[key] = value
        raw[name] = {"fields": fields, "environment": environment}
    return normalize_programs(raw, "evidence")


def parse_template(path: Path, schema_programs: dict[str, dict]) -> dict[str, dict]:
    parser = configparser.RawConfigParser(strict=True)
    parser.optionxform = str
    try:
        content = path.read_text()
        parser.read_string(content)
    except (OSError, configparser.Error) as exc:
        fail("TEMPLATE_PARSE_ERROR", str(exc))
    contracts: dict[str, dict] = {}
    current = None
    for line in content.splitlines():
        if line.startswith("[program:") and line.endswith("]"):
            current = line[len("[program:"):-1]
        elif current is not None and (match := ENV_CONTRACT.fullmatch(line)):
            contracts[current] = {"key_count": int(match.group(1)), "keys_sorted_sha256": match.group(2)}
    out: dict[str, dict] = {}
    for section in parser.sections():
        if not section.startswith("program:"):
            continue
        name = section.removeprefix("program:")
        fields: dict[str, str] = {}
        environment = None
        for key, value in parser.items(section, raw=True):
            if key != "environment":
                fields[key] = value
                continue
            match = ENV_PLACEHOLDER.fullmatch(value)
            expected = f"ENVIRONMENT_{name.upper().replace('-', '_')}"
            if not match or match.group(1) != expected.removeprefix("ENVIRONMENT_"):
                fail("TEMPLATE_LITERAL_ENV_VALUE", f"{name}:{value}")
            environment = contracts.get(name)
        if environment is None:
            fail("TEMPLATE_ENV_PLACEHOLDER_MISSING", name)
        out[name] = {"fields": dict(sorted(fields.items())), "environment": environment}
    return normalize_programs(out, "template")


def compare(left: dict[str, dict], right: dict[str, dict], label: str) -> None:
    left_names, right_names = set(left), set(right)
    if left_names != right_names:
        fail(f"PROGRAM_SET_MISMATCH_{label}", f"left_only={sorted(left_names-right_names)} right_only={sorted(right_names-left_names)}")
    for name in sorted(left):
        lf, rf = left[name]["fields"], right[name]["fields"]
        if lf != rf:
            keys = sorted(set(lf) | set(rf))
            differences = [key for key in keys if lf.get(key) != rf.get(key)]
            fail(f"FIELD_MISMATCH_{label}", f"program={name} fields={differences}")
        if left[name]["environment"] != right[name]["environment"]:
            fail(f"ENV_DIGEST_MISMATCH_{label}", f"program={name}")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--evidence", type=Path, help="optional local copy; must byte-match the anchored Git blob")
    ap.add_argument("--schema", type=Path, required=True)
    ap.add_argument("--template", type=Path, required=True)
    args = ap.parse_args()
    schema = load_json(args.schema, "SCHEMA_PARSE_ERROR")
    source = schema.get("evidence_source")
    if not isinstance(source, dict):
        fail("EVIDENCE_SOURCE_MISSING", "schema.evidence_source")
    required = {"commit", "ref", "path", "sha256"}
    if set(source) != required:
        fail("EVIDENCE_SOURCE_INVALID", f"keys={sorted(source)}")
    blob = git("cat-file", "blob", f'{source["commit"]}:{source["path"]}')
    digest = hashlib.sha256(blob).hexdigest()
    if digest != source["sha256"]:
        fail("EVIDENCE_DIGEST_MISMATCH", f"expected={source['sha256']} git={digest}")
    if args.evidence is not None:
        try:
            local_digest = hashlib.sha256(args.evidence.read_bytes()).hexdigest()
        except OSError as exc:
            fail("EVIDENCE_PARSE_ERROR", str(exc))
        if local_digest != digest:
            fail("EVIDENCE_DIGEST_MISMATCH", f"expected={digest} local={local_digest}")
    git("merge-base", "--is-ancestor", source["commit"], resolve_ref(source["ref"]))
    evidence_programs = parse_evidence(blob.decode())
    schema_programs = normalize_programs(schema.get("programs"), "schema")
    compare(evidence_programs, schema_programs, "EVIDENCE_SCHEMA")
    template_programs = parse_template(args.template, schema_programs)
    compare(schema_programs, template_programs, "SCHEMA_TEMPLATE")
    print(f"PASS programs={len(schema_programs)} evidence_sha256={digest}")


if __name__ == "__main__":
    main()
