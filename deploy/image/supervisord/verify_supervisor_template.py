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

ENV_VALUE = re.compile(r'^([A-Z][A-Z0-9_]*)="%\(ENV_([A-Z][A-Z0-9_]*)\)s"$')


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
        fields, keys = value.get("fields"), value.get("environment_keys")
        if not isinstance(fields, dict) or any(not isinstance(k, str) or not isinstance(v, str) for k, v in fields.items()):
            fail("PROGRAM_FIELDS_INVALID", f"{source}:{name}")
        if not isinstance(keys, list) or any(not isinstance(k, str) for k in keys) or len(keys) != len(set(keys)):
            fail("ENV_KEYS_INVALID", f"{source}:{name}")
        out[name] = {"fields": dict(sorted(fields.items())), "environment_keys": sorted(keys)}
    return dict(sorted(out.items()))


def parse_template(path: Path, schema_programs: dict[str, dict]) -> dict[str, dict]:
    parser = configparser.RawConfigParser(strict=True)
    parser.optionxform = str
    try:
        with path.open() as stream:
            parser.read_file(stream)
    except (OSError, configparser.Error) as exc:
        fail("TEMPLATE_PARSE_ERROR", str(exc))
    out: dict[str, dict] = {}
    for section in parser.sections():
        if not section.startswith("program:"):
            continue
        name = section.removeprefix("program:")
        fields: dict[str, str] = {}
        env_keys: list[str] = []
        for key, value in parser.items(section, raw=True):
            if key != "environment":
                fields[key] = value
                continue
            entries = [entry.strip() for entry in value.split(",") if entry.strip()]
            for entry in entries:
                match = ENV_VALUE.fullmatch(entry)
                if not match or match.group(1) != match.group(2):
                    fail("TEMPLATE_LITERAL_ENV_VALUE", f"{name}:{entry}")
                env_keys.append(match.group(1))
        out[name] = {"fields": dict(sorted(fields.items())), "environment_keys": sorted(env_keys)}
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
        lk, rk = set(left[name]["environment_keys"]), set(right[name]["environment_keys"])
        if lk != rk:
            fail(f"ENV_KEY_MISMATCH_{label}", f"program={name} left_only={sorted(lk-rk)} right_only={sorted(rk-lk)}")


def main() -> None:
    ap = argparse.ArgumentParser()
    ap.add_argument("--evidence", type=Path, required=True)
    ap.add_argument("--schema", type=Path, required=True)
    ap.add_argument("--template", type=Path, required=True)
    args = ap.parse_args()
    evidence = load_json(args.evidence, "EVIDENCE_PARSE_ERROR")
    schema = load_json(args.schema, "SCHEMA_PARSE_ERROR")
    source = schema.get("evidence_source")
    if not isinstance(source, dict):
        fail("EVIDENCE_SOURCE_MISSING", "schema.evidence_source")
    required = {"commit", "ref", "path", "sha256"}
    if set(source) != required:
        fail("EVIDENCE_SOURCE_INVALID", f"keys={sorted(source)}")
    blob = git("cat-file", "blob", f'{source["commit"]}:{source["path"]}')
    digest = hashlib.sha256(blob).hexdigest()
    if digest != source["sha256"] or hashlib.sha256(args.evidence.read_bytes()).hexdigest() != digest:
        fail("EVIDENCE_DIGEST_MISMATCH", f"expected={source['sha256']} git={digest}")
    git("merge-base", "--is-ancestor", source["commit"], source["ref"])
    evidence_programs = normalize_programs(evidence.get("programs"), "evidence")
    schema_programs = normalize_programs(schema.get("programs"), "schema")
    template_programs = parse_template(args.template, schema_programs)
    compare(evidence_programs, schema_programs, "EVIDENCE_SCHEMA")
    compare(schema_programs, template_programs, "SCHEMA_TEMPLATE")
    print(f"PASS programs={len(schema_programs)} evidence_sha256={digest}")


if __name__ == "__main__":
    main()
