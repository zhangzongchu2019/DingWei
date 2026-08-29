#!/usr/bin/env python3
from pathlib import Path
import re, sys

ROOT = Path(__file__).resolve().parents[2]
ARCH = (ROOT / "docs/dingwei-platform-architecture.md").read_text()
SPEC = (ROOT / "docs/dingwei-platform-specdoc.md").read_text()
CONTRACT = (ROOT / "specs/sp6/spec.md").read_text()
BODY_PATH = ROOT / "docs/dingwei-sp6-design.md"

def die(msg, rc=2):
    print(f"UNKNOWN: {msg}")
    raise SystemExit(rc)

def section(text, start, end):
    m = re.search(start + r"(?P<body>.*?)" + end, text, re.S | re.M)
    if not m:
        die(f"missing section {start}")
    return m.group("body")

if not BODY_PATH.exists():
    die("target body missing")
BODY = BODY_PATH.read_text()
cov = section(BODY, r"^<!-- COVERAGE-SP6-BEGIN -->$", r"^<!-- COVERAGE-SP6-END -->$")
expected_ids = {f"D-SP6-{i:02d}" for i in range(1, 13)}
actual_ids = set(re.findall(r"D-SP6-\d{2}", cov))
missing = sorted(expected_ids - actual_ids)
unknown = sorted(actual_ids - expected_ids)

interfaces = set(re.findall(r"`((?:AGT|MSG)-(?:CMD|QRY|CAP)-[^`]+)`", ARCH))
body_interfaces = set(re.findall(r"`((?:AGT|MSG)-(?:CMD|QRY|CAP)-[^`]+)`", BODY))
missing_if = sorted(interfaces - body_interfaces)

arch17 = section(ARCH, r"^## 17\. SpecDoc 责任追踪$", r"^<!-- COVERAGE-SPECDOC-BEGIN -->$")
rows = [line for line in arch17.splitlines() if re.search(r"\| (?:AGT|MSG) \|", line)]
if not rows:
    die("architecture §17 AGT/MSG expected set is empty")
for token in ("§25", "§26", "§27"):
    if token not in SPEC:
        die(f"SpecDoc source {token} missing")

print(f"expected_deliverables={len(expected_ids)} actual={len(actual_ids)} missing={missing} unknown={unknown}")
print(f"expected_interfaces={len(interfaces)} actual={len(body_interfaces & interfaces)} missing={missing_if}")
print(f"architecture_rows={len(rows)}")
if missing or unknown or missing_if:
    raise SystemExit(1)
if "At most six rounds" not in CONTRACT or "No single-detail ping-pong" not in CONTRACT:
    raise SystemExit("review protocol missing")
print("PASS: SP6 shape and coverage")
