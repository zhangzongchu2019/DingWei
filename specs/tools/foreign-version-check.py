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
  · 按同行的受控件称谓判「这个号是谁的」，不按主版本号段猜测；架构与原型现均为 v1.x。
  · 含「沿革」「历史时点」或 `style-ok:` 的行**跳过** —— 描述历史的版本号是合法的。
    🔴 **区分「沿革表述」与「现行依据表述」属语义**，本脚本按上述标记切分，
       标记打错就判错 ⇒ 该切分本身归人审。
"""
import hashlib, re, sys

OWN = ('dingwei-platform-specdoc.md', 'dingwei-platform-architecture.md', 'dingwei-prototype.html')
# 指向【某个真实件】的称谓。🔴 只有版本号与这些称谓同行时才判 ——
#   否则会把界面样例数据、文件名里的版本号一并误报。
#   ⚠️ 首版没有这一层，实跑 42 处命中里绝大多数是误报：
#     `dingwei-platform-charter-v1.8.md` 是【文件名】；原型演示表里的 v0.1/v0.4/v3.2 是【样例数据】。
#   🔴 **误伤会逼人绕过闸，而不是满足它** —— 宁可收窄判据，也不要一个没人信的闸。
NAMES = {
    'dingwei-platform-specdoc.md': ('SpecDoc', 'dingwei-platform-specdoc', '规范正文'),
    'dingwei-platform-architecture.md': ('目标架构', '平台架构', 'dingwei-platform-architecture'),
    'dingwei-prototype.html': ('原型', 'dingwei-prototype'),
}
SKIP = ('沿革', '历史时点', '冻结指纹', 'sha256', 'style-ok:', '曾同时写着', '曾写', '原写')

# 🔴 【历史落地表述】不是【现行依据表述】（丙域 kimi R2 轮4 · RV-丙17）：
#   「SpecDoc §25.5 自 v3.0 起已补」说的是**这件事什么时候落的**，
#   不是**他件现在是哪版**。④b 判的是后者，前者不在其作用域内。
#   原实现只按 SKIP 关键词切分，抓不到这一类句式 ⇒ 原型 6 处同形全部误报。
#   ⚠️ 执笔方上一轮把这条误报「修」到了 prose-style-gate 的 PAT_ROLE 上 ——
#     **修到了另一个闸**，④b 一直原样 FAIL。改闸前先确认报错的是哪个闸。
#   ⚠️ 豁免必须按【该处出现】判，不能整行跳过：一行里既有落地句式又有真旧依据时，
#     整行跳过会把真的那处一起放走。本件的反测正是这样抓到第一版实现的。
LANDED_BEFORE = re.compile(r'自\s*$')                      # 「自 v3.0 起」的前半
LANDED_AFTER  = re.compile(r'^\s*(起|扩|补入|落入|新增于)')  # 后半


def _is_landed(line, m):
    return bool(LANDED_BEFORE.search(line[:m.start()][-4:])
                and LANDED_AFTER.match(line[m.end():])) or \
           bool(LANDED_AFTER.match(line[m.end():]) and '（' in line[:m.start()][-2:])


def current_versions(status_path='docs/DELIVERY-STATUS.md'):
    st = open(status_path, encoding='utf-8').read()
    out = {}
    for path in OWN:
        m = re.search(r'\|\s*`' + re.escape(path) + r'`\s*\|\s*\*\*(v[\d.]+)\*\*', st)
        out[path] = m.group(1) if m else None
    return out


def scan(path, cur):
    body = open('docs/' + path, encoding='utf-8').read()
    hits = []
    for ln, line in enumerate(body.split('\n'), 1):
        if any(k in line for k in SKIP):
            continue
        for m in re.finditer(r'\bv(\d+)\.(\d+)\b', line):
            if _is_landed(line, m):
                continue                      # 历史落地表述，按【该处】豁免
            maj, minor = m.group(1), m.group(2)
            owners = [p for p in OWN if p != path and any(nm in line for nm in NAMES[p])
                      and cur.get(p, '').startswith('v' + maj + '.')]
            if len(owners) != 1:
                continue                      # 未具名他件或同句多件，归人审
            owner = owners[0]
            # 文件名里的版本（如 xxx-v1.8.md）不是跨件引用
            if re.search(r'[-_]v' + maj + r'\.' + minor + r'\b', line):
                continue
            want = cur.get(owner)
            got = f'v{maj}.{minor}'
            if want and got != want:
                hits.append((ln, got, owner, want))
    return hits


def scan_arch_input_table(cur):
    """架构 §0 的冻结输入表必须逐项等于当前 SpecDoc/原型件。"""
    arch = open('docs/dingwei-platform-architecture.md', encoding='utf-8').read()
    bad = []
    for label, path in (('SpecDoc', 'dingwei-platform-specdoc.md'),
                        ('原型', 'dingwei-prototype.html')):
        m = re.search(r'^\| ' + re.escape(label) + r' (v[\d.]+) \| sha256 `([0-9a-f]{64})` \|', arch, re.M)
        got_v, got_h = m.groups() if m else (None, None)
        want_v = cur[path]
        want_h = hashlib.sha256(open('docs/' + path, 'rb').read()).hexdigest()
        if (got_v, got_h) != (want_v, want_h):
            bad.append((label, got_v, got_h, want_v, want_h))
    return bad


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
    if '--selftest' not in sys.argv:
        table_bad = scan_arch_input_table(cur)
        if table_bad:
            bad = 1
            for label, got_v, got_h, want_v, want_h in table_bad:
                print(f'  ✗ 架构输入表 {label}: 写 {got_v}/{(got_h or "缺")[:12]}…，当前 {want_v}/{want_h[:12]}…')
        else:
            print('  ✓ 架构输入表：SpecDoc/原型版本与 sha256 逐项等于当前件')
    return bad


if __name__ == '__main__':
    if '--selftest' in sys.argv:
        import tempfile, os
        # 🔴 反测:证明它真的会响,而不是恰好没命中
        t = tempfile.mkdtemp(); os.makedirs(t + '/docs')
        open(t + '/docs/DELIVERY-STATUS.md', 'w', encoding='utf-8').write(
            '| `dingwei-platform-specdoc.md` | **v3.23** |\n'
            '| `dingwei-platform-architecture.md` | **v1.2** |\n'
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
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
            '依据 SpecDoc v3.14 基线\n')
        chk(main() == 1, '拦住：架构正文写他件旧版本 v3.14（现为 v3.23）')
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
            '沿革：曾依据 SpecDoc v3.14\n')
        chk(main() == 0, '不误伤：沿革表述里的旧版本号放行')
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
            'SpecDoc §25.5「Agent 名册」自 v3.0 起已补\n')
        chk(main() == 0, '不误伤：历史落地表述「自 vX 起已补」放行（丙域 RV-丙17）')
        # 🔴 豁免不得把闸豁免掉：同屏若真写「依据 SpecDoc v3.0」仍须被拦
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
            'SpecDoc §25.5 自 v3.0 起已补；本件依据 SpecDoc v3.0 基线\n')
        chk(main() == 1, '豁免不过宽：同行有落地句式也不放过真的旧依据')
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
            '依据 SpecDoc v3.23\n')
        chk(main() == 0, '不误伤：他件【现值】不算旧')
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
            '本件 v0.19 曾如何\n')
        chk(main() == 0, '不误伤：本件自述号段归 ④a，本脚本不判')
        open(t + '/docs/dingwei-platform-architecture.md', 'w', encoding='utf-8').write(
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
