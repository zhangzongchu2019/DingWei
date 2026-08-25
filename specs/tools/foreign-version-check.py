#!/usr/bin/env python3
"""正文是否出现【他件的旧版本号】。

🔴 立此检查的直接原因（甲域 sol）：
  `analyze.sh` 第 ④ 项原【只读头块一处】（匹配 `**版本**: (v\\d+.\\d+)`），
  正文里的假版本一个都看不见 —— 于是它输出的「三件版本自述与 DELIVERY-STATUS 一致」
  **实际只是「被挑中的一处一致」**。
  同一锚上，原型 S4 写 v3.13、导出函数写 v3.13/v1.13、架构头写 v3.14，而 analyze rc=0。
  ⚠️ 执笔方曾多次以该项通过作为「跨制品版本一致」的证据 ——
  属本仓已归纳的「**断言作用域大于实际覆盖**」，且是**引用自己的验证过程却没读它测什么**。

判据（分两半，各管各的）：
  ④a 头块自述 —— 本件自己负责的版本，仍由 analyze.sh 比对（不在本脚本）
  ④b 本脚本  —— 正文不得出现【他件】的、且与现值不符的版本号

⚠️ 能力边界，写明不假装：
  · 按号段归属（SpecDoc=v3.x · 架构=v0.x · 原型=v1.x）判「这个号是谁的」。
    号段是约定，不是语法；若将来号段跨界，本判据须同步改，否则会漏判。
  · 含「沿革」「历史时点」或 `style-ok:` 的行**跳过** —— 描述历史的版本号是合法的。
    🔴 **区分「沿革表述」与「现行依据表述」属语义**，本脚本按上述标记切分，
       标记打错就判错 ⇒ 该切分本身归人审。
"""
import re, sys

OWN = {  # 件 -> 它自己的主版本号段
    'dingwei-platform-specdoc.md': '3',
    'dingwei-architecture-design.md': '0',
    'dingwei-prototype.html': '1',
}
# 指向【某个真实件】的称谓。🔴 只有版本号与这些称谓同行时才判 ——
#   否则会把界面样例数据、文件名里的版本号一并误报。
#   ⚠️ 首版没有这一层，实跑 42 处命中里绝大多数是误报：
#     `dingwei-platform-charter-v1.8.md` 是【文件名】；原型演示表里的 v0.1/v0.4/v3.2 是【样例数据】。
#   🔴 **误伤会逼人绕过闸，而不是满足它** —— 宁可收窄判据，也不要一个没人信的闸。
NAMES = {
    'dingwei-platform-specdoc.md': ('SpecDoc', 'dingwei-platform-specdoc', '规范正文'),
    'dingwei-architecture-design.md': ('架构设计', 'dingwei-architecture-design', 'A§'),
    'dingwei-prototype.html': ('原型', 'dingwei-prototype'),
}
SKIP = ('沿革', '历史时点', 'style-ok:', '曾同时写着', '曾写', '原写')


def current_versions(status_path='docs/DELIVERY-STATUS.md'):
    st = open(status_path, encoding='utf-8').read()
    out = {}
    for path in OWN:
        m = re.search(r'\|\s*`' + re.escape(path) + r'`\s*\|\s*\*\*(v[\d.]+)\*\*', st)
        out[path] = m.group(1) if m else None
    return out


def scan(path, cur):
    seg = OWN[path]
    body = open('docs/' + path, encoding='utf-8').read()
    hits = []
    for ln, line in enumerate(body.split('\n'), 1):
        if any(k in line for k in SKIP):
            continue
        for maj, minor in re.findall(r'\bv(\d+)\.(\d+)\b', line):
            if maj == seg:
                continue                      # 本件自述号段，归 ④a
            owner = next((p for p, q in OWN.items() if q == maj), None)
            if owner is None:
                continue                      # 不属任何已知件的号段，不判
            # 文件名里的版本（如 xxx-v1.8.md）不是跨件引用
            if re.search(r'[-_]v' + maj + r'\.' + minor + r'\b', line):
                continue
            # 🔴 只有与【该件的称谓】同行时才算跨件引用
            if not any(nm in line for nm in NAMES[owner]):
                continue
            want = cur.get(owner)
            got = f'v{maj}.{minor}'
            if want and got != want:
                hits.append((ln, got, owner, want))
    return hits


def main():
    cur = current_versions()
    bad = 0
    for path in OWN:
        hits = scan(path, cur)
        if hits:
            bad = 1
            print(f'  ✗ {path}: 正文含他件旧版本号 {len(hits)} 处')
            for ln, got, owner, want in hits[:6]:
                print(f'      L{ln} 写 {got}，而 {owner} 现为 {want}')
            if len(hits) > 6:
                print(f'      …（共 {len(hits)} 处）')
        else:
            print(f'  ✓ {path}: 正文无他件旧版本号')
    return bad


if __name__ == '__main__':
    if '--selftest' in sys.argv:
        import tempfile, os
        # 🔴 反测:证明它真的会响,而不是恰好没命中
        t = tempfile.mkdtemp(); os.makedirs(t + '/docs')
        open(t + '/docs/DELIVERY-STATUS.md', 'w', encoding='utf-8').write(
            '| `dingwei-platform-specdoc.md` | **v3.23** |\n'
            '| `dingwei-architecture-design.md` | **v0.22** |\n'
            '| `dingwei-prototype.html` | **v1.20** |\n')
        cwd = os.getcwd(); os.chdir(t)
        n = 0; fail = 0
        def chk(cond, msg):
            global n, fail
            if cond: n += 1; print('  ✓ ' + msg)
            else: fail = 1; print('  ✗ 反测未过：' + msg)
        for p in OWN:
            open(t + '/docs/' + p, 'w', encoding='utf-8').write('干净正文\n')
        chk(main() == 0, '干净正文放行（不误报）')
        open(t + '/docs/dingwei-architecture-design.md', 'w', encoding='utf-8').write(
            '依据 SpecDoc v3.14 基线\n')
        chk(main() == 1, '拦住：架构正文写他件旧版本 v3.14（现为 v3.23）')
        open(t + '/docs/dingwei-architecture-design.md', 'w', encoding='utf-8').write(
            '沿革：曾依据 SpecDoc v3.14\n')
        chk(main() == 0, '不误伤：沿革表述里的旧版本号放行')
        open(t + '/docs/dingwei-architecture-design.md', 'w', encoding='utf-8').write(
            '依据 SpecDoc v3.23\n')
        chk(main() == 0, '不误伤：他件【现值】不算旧')
        open(t + '/docs/dingwei-architecture-design.md', 'w', encoding='utf-8').write(
            '本件 v0.19 曾如何\n')
        chk(main() == 0, '不误伤：本件自述号段归 ④a，本脚本不判')
        open(t + '/docs/dingwei-architecture-design.md', 'w', encoding='utf-8').write(
            '源件 `dingwei-platform-charter-v1.8.md` 已降为沿革依据\n')
        chk(main() == 0, '不误伤：文件名里的版本号不算跨件引用')
        open(t + '/docs/dingwei-prototype.html', 'w', encoding='utf-8').write(
            '<td>示例 spec A</td><td>v3.2</td><td>草案</td>\n')
        chk(main() == 0, '不误伤：界面样例数据里的版本号（不提及真实件名）')
        open(t + '/docs/dingwei-prototype.html', 'w', encoding='utf-8').write(
            '<td>dingwei-platform-specdoc</td><td>v3.13</td>\n')
        chk(main() == 1, '拦住：样例表里【具名真实件】的旧版本（正是 F-3）')
        os.chdir(cwd)
        print(f'--- 反测 {n} 项全过' if not fail else '--- 反测未过')
        sys.exit(fail)
    sys.exit(main())
