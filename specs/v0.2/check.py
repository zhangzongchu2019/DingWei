#!/usr/bin/env python3
import csv
import hashlib
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
ARCH = ROOT / "docs/dingwei-platform-architecture.md"
SP4_SPEC = ROOT / "specs/sp4/spec.md"


def fail(message):
    print(f"✗ {message}")
    raise SystemExit(2)


def rows(path, width):
    out = []
    with path.open(encoding="utf-8") as fh:
        for row in csv.reader((line for line in fh if line.strip() and not line.startswith("#")), delimiter="\t"):
            if len(row) != width:
                fail(f"{path.relative_to(ROOT)} 列数错误：期望 {width}，实得 {len(row)}")
            out.append(row)
    return out


def architecture_rows():
    text = ARCH.read_text(encoding="utf-8")
    match = re.search(r"^## 17\..*?\n(.*?)(?=^## 18\.)", text, re.S | re.M)
    if not match:
        fail("架构 §17 起界/止界不可取")
    result = []
    for line in match.group(1).splitlines():
        if not line.startswith("|"):
            continue
        cells = [cell.strip().strip("`") for cell in line.strip().strip("|").split("|")]
        if len(cells) == 4 and cells[0] not in ("SpecDoc", "---"):
            result.append((cells[0], cells[1]))
    if not result:
        fail("架构 §17 责任表为空")
    return result


def git(*args):
    return subprocess.check_output(["git", *args], cwd=ROOT, stderr=subprocess.DEVNULL)


def check_ancestry():
    anchors = (
        "4220195b311ca9b9b734706ce80d8b3fcc1abfae",
        "6a98629fefd91e69b146516291491cbc6d1136da",
        "c7ac0c3a191167ab7652fbddb712578dbe579a12",
        "16028ac41ad7aab2779bba0c6395dd37494cac96",
        "4006cbeeda3edd986d6a9da53268e68aa01c9c9e",
        "970effe3fdcc52b43e455597dd877a39341f9cba",
    )
    head = git("rev-parse", "HEAD").decode().strip()
    for anchor in anchors:
        try:
            git("cat-file", "-e", f"{anchor}^{{commit}}")
            subprocess.check_call(["git", "merge-base", "--is-ancestor", anchor, head], cwd=ROOT,
                                  stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        except (subprocess.CalledProcessError, FileNotFoundError):
            fail(f"历史锚不可达或不是 v0.2 ancestor：{anchor}")
    print(f"可达性：{len(anchors)}/{len(anchors)} 历史锚均为 HEAD ancestor")


def check_manifest():
    manifest = rows(ROOT / "specs/v0.2/baseline-manifest.tsv", 3)
    paths = [row[0] for row in manifest]
    if len(paths) != len(set(paths)) or not manifest:
        fail("v0.2 baseline manifest 为空或路径重复")
    bad = []
    for path, want, _source in manifest:
        target = ROOT / path
        if not target.is_file():
            bad.append((path, "missing", want))
            continue
        got = hashlib.sha256(target.read_bytes()).hexdigest()
        if got != want:
            bad.append((path, got, want))
    if bad:
        fail(f"v0.2 当前树权威件漂移：{bad}")
    print(f"v0.2 manifest：{len(manifest)}/{len(manifest)} 路径指纹一致")


def check_domain_equivalence():
    arch_rows = architecture_rows()
    baseline = rows(ROOT / "specs/v0.2/domain-baseline.tsv", 3)
    registry = rows(ROOT / "specs/v0.2/domain-mismatches.tsv", 7)
    ids = [row[0] for row in registry]
    if len(ids) != len(set(ids)):
        fail("域错配登记 ID 重复")
    registered = {(row[1], row[2], row[3], row[4]) for row in registry if row[6] == "registered"}
    actual = set()
    summary = []
    for sp, domain, pattern in baseline:
        selected = [(scope, owner) for scope, owner in arch_rows if re.search(pattern, scope)]
        if not selected:
            fail(f"{sp} 旧范围在架构 §17 派生为空")
        mismatches = [(scope, owner) for scope, owner in selected if owner.split(".")[0] != domain]
        actual.update((sp, scope, domain, owner) for scope, owner in mismatches)
        summary.append((sp, domain, len(selected), len(mismatches)))
    missing = sorted(actual - registered)
    stale = sorted(registered - actual)
    if missing or stale:
        fail(f"域错配登记不精确：未登记={missing} 过期登记={stale}")
    print("域等价性：" + " · ".join(
        f"{sp}/{domain} rows={count} mismatch={bad}" for sp, domain, count, bad in summary
    ))
    print("未登记错配：0")


def check_future_packages():
    arch_domains = {owner.split(".")[0] for _, owner in architecture_rows()}
    packages = rows(ROOT / "specs/v0.2/future-domain-packages.tsv", 4)
    names = [row[0] for row in packages]
    if names != ["SP5-COL", "SP6-AGT", "SP6-MSG", "SP7-PLT"]:
        fail(f"未起件分包集合不符裁定：{names}")
    for _, domain, _, status in packages:
        if domain not in arch_domains or status != "planned":
            fail(f"未起件域映射非法：{domain}/{status}")
    print("未起件域包：SP5-COL · SP6-AGT · SP6-MSG · SP7-PLT")


def function_calls(source, call_name):
    found = set()
    matches = list(re.finditer(r"^def\s+(\w+)\s*\(", source, re.M))
    for index, match in enumerate(matches):
        end = matches[index + 1].start() if index + 1 < len(matches) else len(source)
        if re.search(rf"\b{re.escape(call_name)}\s*\(", source[match.end():end]):
            found.add(match.group(1))
    return found


def deriver_ledger():
    text = SP4_SPEC.read_text(encoding="utf-8")
    match = re.search(r"<!-- DERIVERS-SP4-BEGIN -->(.*?)<!-- DERIVERS-SP4-END -->", text, re.S)
    if not match:
        fail("SP4 区间派生器台账缺失")
    result = set()
    for line in match.group(1).splitlines():
        cells = [cell.strip().strip("`") for cell in line.strip().strip("|").split("|")]
        if len(cells) == 5 and re.fullmatch(r"\d+", cells[0]):
            result.add(cells[1])
    return result


def check_interval_callsites():
    check_src = (ROOT / "specs/sp4/check.sh").read_text(encoding="utf-8")
    xref_src = (ROOT / "specs/sp4/xref-check.sh").read_text(encoding="utf-8")
    mapped = {
        "check:specdoc-14-17": "derive_src",
        "check:specdoc-E1-E5": "derive_E",
        "check:arch-6": "derive_interfaces",
        "check:arch-4.2": "derive_events",
        "check:arch-13-14": "derive_prototype",
        "xref:sp8-11.5a-items": "d_sp8_items",
        "xref:arch-cap-interfaces": "d_arch_interfaces",
        "xref:arch-cap-events": "d_arch_events",
    }
    calls = function_calls(check_src, "slice_lines") | function_calls(xref_src, "_slice")
    ledger = deriver_ledger()
    if ledger != set(mapped):
        fail(f"派生器台账与 v0.2 映射不等：{sorted(ledger ^ set(mapped))}")
    expected_calls = set(mapped.values())
    if calls != expected_calls:
        fail(f"区间 helper 调用点未全登记：未登记={sorted(calls-expected_calls)} 空登记={sorted(expected_calls-calls)}")
    print(f"区间 helper 调用点：{len(calls)}/{len(calls)} 已登记")


def check_sp4_inputs_and_exemptions():
    text = SP4_SPEC.read_text(encoding="utf-8")
    source = re.search(r"### ⑥ 输入来源(.*?)(?=\n## )", text, re.S)
    if not source:
        fail("SP4 ⑥ 输入来源节缺失")
    docs_rows = [line for line in source.group(1).splitlines() if re.match(r"^\| `docs/", line)]
    missing = [line for line in docs_rows if not re.match(r"^\| `docs/[^`]+` @ `[0-9a-f]{7,40}` \|", line)]
    if not docs_rows or missing:
        fail(f"SP4 ⑥ docs 行未全部显式带 @锚：{missing}")
    exempt = re.search(r"<!-- XREF-EXEMPT-BEGIN -->(.*?)<!-- XREF-EXEMPT-END -->", text, re.S)
    if not exempt:
        fail("SP4 数词形态豁免台账缺失")
    forbidden = []
    for line in exempt.group(1).splitlines():
        cells = [cell.strip().strip("`") for cell in line.strip().strip("|").split("|")]
        if len(cells) == 3 and re.fullmatch(r"\d+", cells[0]) and any(unit in cells[1] for unit in ("heading", "接口", "事件", "项")):
            forbidden.append(cells[1])
    if forbidden:
        fail(f"豁免台账承载派生器计数单位：{forbidden}")
    print(f"SP4 ⑥ 显式锚：{len(docs_rows)}/{len(docs_rows)}；错分母豁免：0")


def check_obligations():
    obligations = rows(ROOT / "specs/v0.2/obligations.tsv", 6)
    ids = [row[0] for row in obligations]
    expected = [f"V02-OBL-{i:02d}" for i in range(1, 8)]
    if ids != expected or len(ids) != len(set(ids)):
        fail(f"七条义务 ID 不精确：{ids}")
    status = {row[0]: row[5] for row in obligations}
    if status["V02-OBL-03"] != "registered" or any(status[key] != "closed-by-v0.2" for key in expected if key != "V02-OBL-03"):
        fail(f"七条义务状态不符裁定：{status}")
    arch = dict(architecture_rows())
    if arch.get("F.15 skill/MCP 归属鉴权") != "CAP":
        fail("F.15 整行未归 CAP/SP4")
    baseline_spec = (ROOT / "specs/v0.2/spec.md").read_text(encoding="utf-8")
    for needle in ("F.15.5 #4", "GOV/SP8 §24", "fail closed"):
        if needle not in baseline_spec:
            fail(f"F.15 跨件义务缺语义：{needle}")
    registration = ROOT / "specs/sp4/REVIEW-REGISTRATION-v1.8.md"
    if hashlib.sha256(registration.read_bytes()).hexdigest() != "1167be6eadd79cef0bb1f9d010b691d8c9e2c632bd0c1f8023f5412899a3a0b1":
        fail("SP4 v1.8 评审登记件漂移")
    print("七条义务：6 closed-by-v0.2 · 1 registered(P6) · 缺失=0")


def check_archive_and_ledger():
    if (ROOT / "CODEOWNERS").exists():
        fail("无效 CODEOWNERS 仍存在")
    if (ROOT / "docs/dingwei-architecture-design.md").exists():
        fail("旧架构仍占用现行路径")
    if not (ROOT / "docs/v0.1/dingwei-architecture-design-v0.34.md").is_file():
        fail("旧架构归档件缺失")
    ledger = rows(ROOT / "specs/gates/rv-ledger.txt", 7)
    ids = [row[0] for row in ledger]
    if len(ids) != 84 or len(set(ids)) != 84:
        fail(f"RV ledger 行/ID 不是 84/84：{len(ids)}/{len(set(ids))}")
    open_ids = {row[0] for row in ledger if row[4] in ("挂账", "未决", "未处置")}
    expected_open = {"RV-执笔01", "RV-丙01", "RV-甲08"}
    if open_ids != expected_open:
        fail(f"RV 未闭集合漂移（SP1 三条已由 v1.2 收口）：{sorted(open_ids)}")
    print("RV ledger：84/84 unique · SP1 三条已闭 · 其余未闭 3/3 原样保留")


def main():
    check_ancestry()
    check_manifest()
    check_domain_equivalence()
    check_future_packages()
    check_interval_callsites()
    check_sp4_inputs_and_exemptions()
    check_obligations()
    check_archive_and_ledger()
    print("预期集合：历史锚6 + 存量SP5 + 未起域包4 + 义务7 + RV84")
    print("实检集合：6/6 + 5/5 + 4/4 + 7/7 + 84/84")
    print("未知集合：0")
    print("✓ design/v0.2 域主轴统一基准通过")


if __name__ == "__main__":
    main()
