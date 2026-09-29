#!/usr/bin/env python3
"""从 Go 侧单一真源生成 sessionhelper/hub_meta.py。

真源:internal/model/model.go 的 `hub-meta-types:begin … :end` 块。

用法:
    python3 tools/sessionhelper/gen_hub_meta.py            # 重新生成(写盘)
    python3 tools/sessionhelper/gen_hub_meta.py --check    # 只比对;漂移则 rc=1(供 build.sh / CI)

🔴 生成件 tools/sessionhelper/sessionhelper/hub_meta.py 不许手改。
🔴 本文件不含任何 meta.type 字面量 —— 它只解析 Go 源。字面量出现在这里就等于又抄了一份。
漂移闸:tools/sessionhelper/tests/test_hub_notice_no_inject.py(HubMetaSameSourceTest)。
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

BEGIN_MARKER = "// hub-meta-types:begin"
END_MARKER = "// hub-meta-types:end"
CONST_RE = re.compile(r'^\s*(?P<name>[A-Za-z_][A-Za-z0-9_]*)\s*=\s*"(?P<value>[^"]*)"\s*$')
GO_PREFIX = "MetaType"
PY_PREFIX = "HUB_META_TYPE_"

HEADER = '''"""GENERATED FILE — DO NOT EDIT.

真源:internal/model/model.go 的 hub-meta-types 块(Envelope.Meta["type"] 的跨语言单一真源)。
重生成:python3 tools/sessionhelper/gen_hub_meta.py
漂移闸:tools/sessionhelper/tests/test_hub_notice_no_inject.py(HubMetaSameSourceTest)

🔴 helper 侧只从这里取值,不写字面量。两侧漂移不会编译错,只会让
   「Hub 自发通知不注入 CLI」那条判据静默失效 ⇒ 配额环复活。
"""

'''


def go_model_path(repo_root: Path) -> Path:
    return Path(repo_root) / "internal" / "model" / "model.go"


def output_path(helper_root: Path) -> Path:
    return Path(helper_root) / "sessionhelper" / "hub_meta.py"


def parse_go_block(repo_root: Path) -> dict[str, str]:
    """解析 Go 侧的 hub-meta-types 块,返回 {GoConstName: value}(保持声明顺序)。"""
    path = go_model_path(Path(repo_root))
    if not path.exists():
        raise SystemExit(f"找不到真源 {path};请在仓内运行本生成器")
    text = path.read_text(encoding="utf-8")
    if BEGIN_MARKER not in text or END_MARKER not in text:
        raise SystemExit(
            f"{path} 里找不到 {BEGIN_MARKER} / {END_MARKER} 标记 ⇒ 真源形状变了。\n"
            "请更新生成器去匹配新形状,🔴 不要退化成在本文件里写字面量。"
        )
    block = text.split(BEGIN_MARKER, 1)[1].split(END_MARKER, 1)[0]
    found: dict[str, str] = {}
    for line in block.splitlines():
        match = CONST_RE.match(line)
        if match:
            found[match.group("name")] = match.group("value")
    if not found:
        raise SystemExit(
            f"{path} 的 hub-meta-types 块里一个常量都没解析到 ⇒ 形状变了。\n"
            "请更新生成器,🔴 不要放宽判据、也不要在 helper 侧另抄字面量。"
        )
    return found


def py_const_name(go_name: str) -> str:
    """MetaTypeDeliveryFailure -> HUB_META_TYPE_DELIVERY_FAILURE"""
    if not go_name.startswith(GO_PREFIX):
        raise SystemExit(
            f"真源里的常量 {go_name} 不以 {GO_PREFIX} 开头 ⇒ 命名约定变了。\n"
            "请更新 py_const_name,不要静默生成一个没人认得的名字。"
        )
    tail = go_name[len(GO_PREFIX):]
    snake = re.sub(r"(?<!^)(?=[A-Z])", "_", tail).upper()
    return PY_PREFIX + snake


def render(repo_root: Path) -> str:
    parts = [HEADER]
    for go_name, value in parse_go_block(repo_root).items():
        parts.append(f'{py_const_name(go_name)} = "{value}"\n')
    return "".join(parts)


def main(argv: list[str]) -> int:
    here = Path(__file__).resolve().parent          # tools/sessionhelper
    repo_root = here.parents[1]
    check = "--check" in argv[1:]
    rendered = render(repo_root)
    target = output_path(here)
    current = target.read_text(encoding="utf-8") if target.exists() else None
    if current == rendered:
        print(f"[gen_hub_meta] up to date: {target.relative_to(repo_root)}")
        return 0
    if check:
        print(
            f"[gen_hub_meta] DRIFT: {target.relative_to(repo_root)} 与真源 "
            f"{go_model_path(repo_root).relative_to(repo_root)} 的 hub-meta-types 块不一致。\n"
            f"  修法:python3 tools/sessionhelper/gen_hub_meta.py\n"
            f"  🔴 不要手改生成件,也不要在 helper 侧另抄字面量。",
            file=sys.stderr,
        )
        return 1
    target.write_text(rendered, encoding="utf-8")
    print(f"[gen_hub_meta] wrote {target.relative_to(repo_root)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv))
