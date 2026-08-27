#!/usr/bin/env python3
"""处置兑现 triage —— 「标了采纳」与「正文真改了」之间的对账。

🔴 立此件的直接原因（R2 轮3–轮4，三域独立各抓一次同形）：
  执笔方在 RV 标注上写【采纳】而正文一字未动，或改在了别处。实例：
    · 乙域 RV-乙01「头块仍载评审过程」标采纳，§0 头块一直没改；
    · 丙域 RV-丙13「已按该清单逐段改完」标采纳，原型两处文本未动；
    · 丙域 RV-丙17 点的是 analyze.sh ④b，执笔方改的是 prose-style-gate；
    · 丙域 RV-丙14「原文恢复」——只做了预防（⑯），恢复一直没发生。
  ⚠️ 四次**没有一次**是执笔方自查出来的，全部由门方**实取正文**发现。
  ⇒ 本件把那个核对方式机器化一部分：标注里被「」引起来的原句，
    如果处置声称「已删/已改写/已移」，那句话就不该还在正文里。

🔴 **本件不判门，只做 triage** —— 理由写明，不假装：
  它按【引号 + 动词】判，而两者都可能出现在不表达该含义的语境里。
  首次实跑 4 个命中里 **3 个是误报**：
    · 「一律不入正文」是体例规则本身，标注只是引用它；
    · 「移出表格」被 `移出` 模式当成了「移入沿革」（同一个字，两回事）。
  ⇒ 命中须逐条人判。把它接成硬闸只会逼人绕过它（见 foreign-version-check 头注同一条教训）。
  ⚠️ 它也**判不了没被引号引起来的诉求** —— 门方实取正文仍不可替代。
"""
import re, sys

RV_SEG = re.compile(r'【RV[^】]*】[^\n]*')
FILES = ['docs/dingwei-platform-specdoc.md',
         'docs/dingwei-platform-architecture.md',
         'docs/dingwei-prototype.html']
HIST = 'docs/dingwei-revision-history.md'
# ⚠️ `移出` 不在表内：它同样匹配「移出表格」，是首次实跑两个误报的来源。
CLAIM = re.compile(r'已删|已改写|已清零|整段删除|已按[^，。]{0,12}改完|移入沿革|移沿革')


def audit():
    bodies = {f: RV_SEG.sub('', open(f, encoding='utf-8').read()) for f in FILES}
    hits = []
    for f in FILES:
        s = open(f, encoding='utf-8').read()
        for m in RV_SEG.finditer(s):
            seg = m.group(0)
            if '|采纳】' not in seg:
                continue
            d = seg.find('⟹')
            if d < 0:
                continue
            claim = CLAIM.search(seg[d:])
            if not claim:
                continue
            rid = re.match(r'【(RV-[^|]+)\|', seg)
            rid = rid.group(1) if rid else '?'
            for q in re.findall(r'「([^」]{6,40})」', seg[:d]):
                for g in FILES:
                    if q in bodies[g]:
                        hits.append((rid, claim.group(0), q, g.split('/')[-1]))
    return hits


def main():
    hits = audit()
    for rid, verb, q, where in hits:
        print(f'  ? {rid} 称「{verb}」，而正文仍有「{q}」 → {where}')
    print(f'  ⓘ 处置兑现 triage：{len(hits)} 处待人判 —— '
          f'**本项不判门**，误报率高（首跑 4 命中 3 误报），命中须逐条读原文。')
    print('  ⚠️ 它判不了没被「」引起来的诉求 —— 门方实取正文不可替代。')
    return 0


if __name__ == '__main__':
    if '--selftest' in sys.argv:
        # 反测只证明「它真会响」：造一条标采纳、称已删、而正文仍在的标注
        import tempfile, os
        t = tempfile.mkdtemp(); os.makedirs(t + '/docs')
        cwd = os.getcwd(); os.chdir(t)
        for f in FILES:
            open(f, 'w', encoding='utf-8').write('无关正文\n')
        open(HIST, 'w', encoding='utf-8').write('沿革\n')
        open(FILES[0], 'w', encoding='utf-8').write(
            '这里写着本轮已定的口径。\n'
            '【RV-甲01|甲|阻断|采纳】本段「本轮已定的口径」是过程叙述　　⟹ **【采纳】** 已删。\n')  # rv-fixture
        n = len(audit())
        os.chdir(cwd)
        print('  ✓ 反测：称已删而正文仍在 → 命中' if n == 1 else f'  ✗ 反测未过：应命中 1 处，实得 {n}')
        sys.exit(0 if n == 1 else 1)
    sys.exit(main())
