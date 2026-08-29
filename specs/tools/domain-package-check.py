#!/usr/bin/env python3
"""SP6/SP7 contract checker. rc: 0 PASS, 1 contract mismatch, 2 UNKNOWN/broken gate."""
from __future__ import annotations

import hashlib
from pathlib import Path
import re
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
CFG = {
    "sp6": {
        "owners": {"AGT", "MSG"}, "interfaces": {"AGT", "MSG"},
        "eowners": {"SP6"}, "body": "docs/dingwei-sp6-design.md", "count": 12,
    },
    "sp7": {
        "owners": {"PLT", "PLT.Security"}, "interfaces": {"PLT"},
        "eowners": {"SP7a", "SP7b"}, "body": "docs/dingwei-sp7-design.md", "count": 12,
    },
}

class Unknown(RuntimeError): pass

def section(text: str, start: str, end: str) -> str:
    m = re.search(start + r"(?P<body>.*?)" + end, text, re.S | re.M)
    if not m: raise Unknown(f"missing structured section: {start}")
    return m.group("body")

def cells(line: str) -> list[str]:
    return [x.strip() for x in line.strip().strip("|").split("|")]

def frozen_inputs(contract: str) -> tuple[str, dict[str, str]]:
    m = re.search(r"Frozen baseline: `design/v0\.2` at `([0-9a-f]{40})`", contract)
    if not m: raise Unknown("missing 40-hex frozen baseline")
    anchor = m.group(1); out = {}
    for line in contract.splitlines():
        c = cells(line) if line.startswith("|") else []
        if len(c) == 3 and re.fullmatch(r"`[0-9a-f]{64}`", c[1]):
            path = c[0].strip("`"); out[path] = c[1].strip("`")
    if not out: raise Unknown("frozen input ledger is empty")
    return anchor, out

def git_bytes(anchor: str, path: str) -> bytes:
    try: return subprocess.check_output(["git", "show", f"{anchor}:{path}"], cwd=ROOT, stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError as e: raise Unknown(f"frozen input unavailable: {anchor}:{path}") from e

def verify_frozen(contract: str) -> dict[str, str]:
    anchor, ledger = frozen_inputs(contract); texts = {}
    for path, want in ledger.items():
        raw = git_bytes(anchor, path); got = hashlib.sha256(raw).hexdigest()
        if got != want: raise Unknown(f"frozen digest mismatch: {path} want={want} got={got}")
        texts[path] = raw.decode()
    for required in ("docs/dingwei-platform-specdoc.md", "docs/dingwei-platform-architecture.md"):
        if required not in texts: raise Unknown(f"required frozen input absent from ledger: {required}")
    return texts

def derive_arch17(arch: str, owners: set[str]) -> set[str]:
    block = section(arch, r"^## 17\. SpecDoc 责任追踪$", r"^<!-- COVERAGE-SPECDOC-BEGIN -->$")
    out = set()
    for line in block.splitlines():
        if not line.startswith("|"): continue
        c = cells(line)
        if len(c) >= 4 and c[1] in owners and not c[0].startswith("---"):
            out.add("ARCH17:" + re.sub(r"[*`]", "", c[0]))
    if not out: raise Unknown("architecture §17 owner-column expected set is empty")
    return out

def derive_interfaces(arch: str, prefixes: set[str]) -> set[str]:
    out = set()
    for line in arch.splitlines():
        if not line.startswith("|"): continue
        c = cells(line)
        if len(c) < 2: continue
        m = re.fullmatch(r"`([A-Z]+-(?:CMD|QRY|CAP)-[^`]+)`", c[0])
        if m and m.group(1).split("-", 1)[0] in prefixes and c[1] in prefixes:
            out.add("IF:" + m.group(1))
    if not out: raise Unknown("stable-interface expected set is empty")
    return out

def derive_appendix(spec: str, owners: set[str]) -> set[str]:
    sec = ""; out = set()
    for line in spec.splitlines():
        m = re.match(r"^## (E\.[1234])\b", line)
        if m: sec = m.group(1); continue
        if re.match(r"^## E\.", line): sec = ""; continue
        if not sec or not line.startswith("|"): continue
        c = [re.sub(r"[*`]", "", x) for x in cells(line)]
        if c and re.fullmatch(r"\d+[a-z]?", c[0]) and owners & set(c[-1].split("·")):
            out.add(f"E:{sec}#{c[0]}")
    if not out: raise Unknown("Appendix E owner_sp expected set is empty")
    return out

def contract_ids(contract: str, pkg: str, count: int) -> set[str]:
    block = section(contract, r"^## 2\. Deliverables$", r"^## 3\.")
    ids = set()
    for line in block.splitlines():
        c = cells(line) if line.startswith("|") else []
        if len(c) == 3 and re.fullmatch(fr"`D-{pkg.upper()}-\d{{2}}`", c[0]): ids.add(c[0].strip("`"))
    canonical = {f"D-{pkg.upper()}-{i:02d}" for i in range(1, count + 1)}
    if ids != canonical: raise Unknown(f"deliverable ledger malformed: missing={sorted(canonical-ids)} unknown={sorted(ids-canonical)}")
    return ids

def coverage_rows(body: str, pkg: str) -> dict[str, set[str]]:
    block = section(body, fr"^<!-- COVERAGE-{pkg.upper()}-BEGIN -->$", fr"^<!-- COVERAGE-{pkg.upper()}-END -->$")
    rows = {}; data = 0
    for line in block.splitlines():
        if not line.startswith("|"): continue
        c = cells(line)
        if len(c) != 2: continue
        token = c[0].strip("`")
        if token in {"Source obligation", "---"} or token.startswith("---"): continue
        ids = set(re.findall(fr"D-{pkg.upper()}-\d{{2}}", c[1]))
        if not token or not ids: raise Unknown(f"coverage row lacks token or real deliverable mapping: {line}")
        if token in rows: raise Unknown(f"duplicate coverage token: {token}")
        rows[token] = ids; data += 1
    if not data: raise Unknown("coverage mapping table has no data rows")
    return rows

def acceptance(body_contract: str, pkg: str) -> None:
    block = section(body_contract, fr"^<!-- ACCEPTANCE-{pkg.upper()}-BEGIN -->$", fr"^<!-- ACCEPTANCE-{pkg.upper()}-END -->$")
    ids = []
    for line in block.splitlines():
        c = cells(line) if line.startswith("|") else []
        if len(c) == 4 and re.fullmatch(fr"`AC-{pkg.upper()}-\d{{2}}`", c[0]):
            if any(not x or x == "—" or x == "见全文" for x in c[1:]): raise Unknown(f"empty acceptance cell: {c[0]}")
            ids.append(c[0].strip("`"))
    canonical = {f"AC-{pkg.upper()}-{i:02d}" for i in range(1, 9)}
    actual = set(ids)
    if actual != canonical or len(ids) != len(actual):
        raise Unknown(f"acceptance ledger malformed: missing={sorted(canonical-actual)} unknown={sorted(actual-canonical)} duplicates={len(ids)-len(actual)}")

def review_protocol(contract: str) -> None:
    block = section(contract, r"^<!-- REVIEW-PROTOCOL-BEGIN -->$", r"^<!-- REVIEW-PROTOCOL-END -->$")
    got = {}
    for line in block.splitlines():
        c = cells(line) if line.startswith("|") else []
        if len(c) == 2 and c[0].startswith("`") and c[1].startswith("`"): got[c[0].strip("`")] = c[1].strip("`")
    want = {"max_rounds":"6", "reviewers_per_round":"1", "rotation":"alternating", "scope":"full_document", "exchange":"whole_round", "decision_ref":"DECISION-20260829T070251Z"}
    if got != want: raise Unknown(f"review protocol ledger mismatch: want={want} got={got}")

def inspect(pkg: str, contract: str, body: str, frozen: dict[str, str], quiet=False) -> int:
    cfg = CFG[pkg]; arch = frozen["docs/dingwei-platform-architecture.md"]; spec = frozen["docs/dingwei-platform-specdoc.md"]
    expected = {
        "arch17": derive_arch17(arch, cfg["owners"]),
        "interfaces": derive_interfaces(arch, cfg["interfaces"]),
        "appendix_e": derive_appendix(spec, cfg["eowners"]),
    }
    ids = contract_ids(contract, pkg, cfg["count"]); rows = coverage_rows(body, pkg); actual = set(rows)
    missing = sorted(set().union(*expected.values()) - actual)
    unknown = sorted(actual - set().union(*expected.values()) - {f"BOUNDARY:{pkg.upper()}"})
    mapped = set().union(*rows.values()) if rows else set()
    unmapped_ids = sorted(ids - mapped); bad_ids = sorted(mapped - ids)
    acceptance(contract, pkg); review_protocol(contract)
    if pkg == "sp6":
        sp8 = frozen.get("docs/dingwei-sp8-design.md", "")
        if "§24.3a" not in sp8 or "AGT-CAP-EmergencyAction" not in body or "fail-closed before effect" not in body:
            raise Unknown("SP8 emergency authority to SP6 enforcement seam is incomplete")
    if pkg == "sp7" and not all(x in body for x in ("IDN/SP1", "WRK/SP2", "KNO/SP3", "CAP/SP4", "COL/SP5", "MSG/SP6", "GOV/SP8")):
        raise Unknown("SP7b domain-owner handoff ledger is incomplete")
    if not quiet:
        for k, v in expected.items(): print(f"[{k}] expected={len(v)} mapped={len(v & actual)} missing={sorted(v-actual)}")
        print(f"[coverage] unknown={unknown} deliverables_unmapped={unmapped_ids} bad_ids={bad_ids}")
        print("scope: frozen input bytes; exact architecture owner column; stable interfaces; Appendix E owner_sp rows; structured two-column mappings")
        print("boundary: proves mapping shape and frozen provenance, not semantic correctness or implementation state")
    if missing or unknown or unmapped_ids or bad_ids: return 1
    return 0

def selftest(pkg: str, contract: str, body: str, frozen: dict[str, str]) -> int:
    n = 0; failed = False
    def test(name, c, b, want):
        nonlocal n, failed; n += 1
        try: got = inspect(pkg, c, b, frozen, True)
        except Unknown: got = 2
        if got == want: print("  ✓ " + name)
        else: print(f"  ✗ {name}: want={want} got={got}"); failed = True
    test("baseline fixture passes", contract, body, 0)
    cfg = CFG[pkg]
    expected = derive_arch17(frozen["docs/dingwei-platform-architecture.md"], cfg["owners"]) | derive_interfaces(frozen["docs/dingwei-platform-architecture.md"], cfg["interfaces"]) | derive_appendix(frozen["docs/dingwei-platform-specdoc.md"], cfg["eowners"])
    for token in sorted(expected):
        mutated = "\n".join(line for line in body.splitlines() if not line.startswith(f"| `{token}` |")) + "\n"
        test(f"drop mapping {token}", contract, mutated, 1)
    naked = re.sub(r"<!-- COVERAGE-.*?-BEGIN -->.*?<!-- COVERAGE-.*?-END -->", "<!-- COVERAGE-X-BEGIN -->\n" + " ".join(sorted(contract_ids(contract,pkg,cfg['count']))) + "\n<!-- COVERAGE-X-END -->", body, flags=re.S)
    test("naked ID list is not a mapping", contract, naked, 2)
    first = f"D-{pkg.upper()}-01"
    test("contract deliverable deletion breaks ledger", contract.replace(next(x for x in contract.splitlines() if x.startswith(f"| `{first}`")), ""), body, 2)
    ac8 = next(x for x in contract.splitlines() if x.startswith(f"| `AC-{pkg.upper()}-08`"))
    test("acceptance deletion breaks canonical ledger", contract.replace(ac8, ""), body, 2)
    test("acceptance unknown ID breaks canonical ledger", contract.replace(ac8, ac8.replace("-08`", "-99`")), body, 2)
    test("acceptance duplicate ID breaks canonical ledger", contract.replace(ac8, ac8 + "\n" + ac8), body, 2)
    bad_arch = re.sub(rf"`({'|'.join(cfg['interfaces'])})-(CMD|QRY|CAP)-", r"`ZZZ-\2-", frozen["docs/dingwei-platform-architecture.md"])
    bad_frozen = dict(frozen); bad_frozen["docs/dingwei-platform-architecture.md"] = bad_arch
    try: derive_interfaces(bad_arch, cfg["interfaces"]); got = 0
    except Unknown: got = 2
    n += 1
    if got == 2: print("  ✓ empty interface derivation is UNKNOWN")
    else: print("  ✗ empty interface derivation passed"); failed = True
    n += 1
    try:
        anchor, ledger = frozen_inputs(contract); path, want = next(iter(ledger.items()))
        got = hashlib.sha256(git_bytes(anchor, path) + b"tamper").hexdigest()
        assert got != want
        print("  ✓ frozen digest tamper is detectable")
    except Exception:
        print("  ✗ frozen digest tamper test failed"); failed = True
    print(f"--- {pkg.upper()} mutation selftests={n} result={'FAIL' if failed else 'PASS'}")
    return 1 if failed else 0

def main() -> int:
    if len(sys.argv) not in {2, 3} or sys.argv[1] not in CFG or (len(sys.argv) == 3 and sys.argv[2] != "--selftest"):
        print("usage: domain-package-check.py <sp6|sp7> [--selftest]", file=sys.stderr); return 2
    pkg = sys.argv[1]; contract = (ROOT / f"specs/{pkg}/spec.md").read_text(); body_path = ROOT / CFG[pkg]["body"]
    if not body_path.exists(): raise Unknown(f"target body missing: {body_path}")
    frozen = verify_frozen(contract); body = body_path.read_text()
    return selftest(pkg, contract, body, frozen) if len(sys.argv) == 3 else inspect(pkg, contract, body, frozen)

if __name__ == "__main__":
    try: raise SystemExit(main())
    except Unknown as e: print("UNKNOWN: " + str(e)); raise SystemExit(2)
