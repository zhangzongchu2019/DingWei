#!/usr/bin/env python3
"""把 view-service/md-render-core.js 的各段同步进 renderer-canary-sse.html 的生成副本。

为什么需要这个工具(而不是让两个页面各写一份):
  `0.2.1.50` 的交付条件要求 spec 展示面与 timeline **走同一条渲染路径**。两页由不同服务出
  (renderer 走 view-service,spec 页走 Go),没法共用一个 HTTP 静态资产而不新增路由
  (新增静态路由要牵 nginx 前缀改写、哈希 pin 与"非白名单仍 404"的阴格,代价远大于收益)。
  ⇒ 采用【单一事实源 + 生成副本 + 漂移闸】:源是 md-render-core.js,renderer 里那 6 段是副本,
    本工具负责把副本刷成与源**逐字节相同**;test_md_render_core.py 负责在漂移时**变红**。

用法:
    python3 md-render-core-sync.py            # 写入(把 renderer 的 6 段刷成与源一致)
    python3 md-render-core-sync.py --check    # 只检查;有漂移则 rc=1 并打印 diff,不写任何文件
    python3 md-render-core-sync.py --core PATH --renderer PATH   # 路径覆盖(给闸做阳性对照用)

rc:  0 = 一致(或已写入)   1 = 有漂移(--check)   2 = 结构错误(标记缺失/重复/顺序错/空段)
🔴 结构错误一律**硬中止**,不静默跳过任何一段 —— 静默跳过会让"同步成功"变成假绿。
"""
from __future__ import annotations

import argparse
import difflib
import pathlib
import re
import sys

HERE = pathlib.Path(__file__).resolve().parent

# 🔴 段的顺序是**契约**:renderer 里这 6 段的位置顺序必须与此一致(它们是零移动抽出来的,
#    顺序变了说明有人把代码搬了家 ⇒ 那不是同步能做的事,必须硬中止让人来看)。
SECTION_ORDER = ("escape", "own-url", "ext-kind", "ext-of", "never-inline", "render")

MARK_RE = re.compile(r"^\s*//\s*(?:🔴\s*)?DW-MD-CORE:([a-z-]+)\s+(BEGIN|END)\b")


def fail(msg: str) -> "NoReturn":  # noqa: F821
    print(f"🔴 md-render-core-sync: {msg}", file=sys.stderr)
    raise SystemExit(2)


def parse_sections(lines: list[str], host: str) -> dict[str, list[str]]:
    """按标记切段。返回 {段名: 段内行(不含标记行)}。任何结构异常都硬中止。"""
    found: dict[str, list[str]] = {}
    order: list[str] = []
    current: str | None = None
    buf: list[str] = []
    for idx, line in enumerate(lines, start=1):
        m = MARK_RE.match(line)
        if not m:
            if current is not None:
                buf.append(line)
            continue
        name, kind = m.group(1), m.group(2)
        if name not in SECTION_ORDER:
            fail(f"{host}:{idx} 出现未登记的段名 DW-MD-CORE:{name}"
                 f"(已登记:{', '.join(SECTION_ORDER)})—— 新段必须先登记进本工具的 SECTION_ORDER")
        if kind == "BEGIN":
            if current is not None:
                fail(f"{host}:{idx} 段 {current} 还没 END 就又开了 {name}(标记嵌套)")
            if name in found:
                fail(f"{host}:{idx} 段 {name} 的 BEGIN 出现了第二次(应恰好一次)")
            current, buf = name, []
        else:
            if current is None:
                fail(f"{host}:{idx} 出现了孤立的 DW-MD-CORE:{name} END(没有配对的 BEGIN)")
            if current != name:
                fail(f"{host}:{idx} END 的段名 {name} 与当前 BEGIN 的 {current} 不配对")
            found[name] = buf
            order.append(name)
            current = None
    if current is not None:
        fail(f"{host}: 段 {current} 的 BEGIN 没有配对的 END(文件被截断了?)")
    missing = [n for n in SECTION_ORDER if n not in found]
    if missing:
        fail(f"{host}: 缺少段 {', '.join(missing)} —— 标记被删了或文件不是预期的那一份")
    if tuple(order) != SECTION_ORDER:
        fail(f"{host}: 段的顺序是 {', '.join(order)},应为 {', '.join(SECTION_ORDER)}"
             f" —— 顺序变化意味着代码被搬过家,不是同步能做的事")
    for name, body in found.items():
        if not body:
            fail(f"{host}: 段 {name} 是空的(BEGIN 与 END 之间一行都没有)")
    return found


def render_lines(lines: list[str], sections: dict[str, list[str]]) -> list[str]:
    """把 renderer 的每一段替换成源里的段内容,标记行本身原样保留。"""
    out: list[str] = []
    current: str | None = None
    for line in lines:
        m = MARK_RE.match(line)
        if m:
            name, kind = m.group(1), m.group(2)
            if kind == "BEGIN":
                current = name
                out.append(line)
                out.extend(sections[name])
                continue
            out.append(line)          # END
            current = None
            continue
        if current is None:
            out.append(line)
    return out


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--check", action="store_true", help="只检查漂移,不写文件(有漂移 rc=1)")
    ap.add_argument("--core", type=pathlib.Path, default=HERE / "md-render-core.js")
    ap.add_argument("--renderer", type=pathlib.Path, default=HERE / "renderer-canary-sse.html")
    ap.add_argument("--quiet", action="store_true")
    args = ap.parse_args()

    for p, what in ((args.core, "共享件"), (args.renderer, "产品件")):
        if not p.is_file():
            fail(f"{what}不存在:{p}")

    core_lines = args.core.read_text(encoding="utf-8").split("\n")
    rend_lines = args.renderer.read_text(encoding="utf-8").split("\n")
    core_sections = parse_sections(core_lines, str(args.core))
    rend_sections = parse_sections(rend_lines, str(args.renderer))

    drifted = [n for n in SECTION_ORDER if core_sections[n] != rend_sections[n]]
    if not drifted:
        if not args.quiet:
            print(f"✅ md-render-core 同步一致:{len(SECTION_ORDER)} 段逐字节相同"
                  f"({', '.join(SECTION_ORDER)})")
        return 0

    print(f"🔴 md-render-core 漂移:{len(drifted)} 段与共享件不一致 —— {', '.join(drifted)}", file=sys.stderr)
    for name in drifted:
        diff = difflib.unified_diff(
            core_sections[name], rend_sections[name],
            fromfile=f"md-render-core.js[{name}](源)",
            tofile=f"renderer-canary-sse.html[{name}](生成副本)", lineterm="", n=2,
        )
        for line in diff:
            print("   " + line, file=sys.stderr)
    print("\n修法只有一条(🔴 不要直接改 renderer 里那 6 段,它是生成物):", file=sys.stderr)
    print("   1) 改 view-service/md-render-core.js", file=sys.stderr)
    print("   2) python3 view-service/md-render-core-sync.py", file=sys.stderr)
    print("   3) 两份一起提交", file=sys.stderr)
    if args.check:
        return 1
    new_lines = render_lines(rend_lines, core_sections)
    args.renderer.write_text("\n".join(new_lines), encoding="utf-8")
    print(f"✅ 已把 {len(drifted)} 段刷成与共享件一致:{args.renderer}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
