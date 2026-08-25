#!/usr/bin/env python3
"""RV 段免疫的批量替换 —— 执笔方做全文清理时必须走本件，不得直接 sed/replace。

立此件的直接原因（丙域 kimi，R2 轮3 第 ③ 条）：
  执笔方清理「三级映射」旧模型名时用了全文替换，把丙域标注的**正文**一并改了，
  标注语因此语义不通。丙域的要求是两条，不是一条：
    ① 标注移沿革前**原文不可改**；
    ② **工具作用域须排除 RV 段**，且要**防复发**。
  闸 ⑯ 只实现了 ①（事后检测摘要前缀还在不在）。
  🔴 **检测不是预防** —— 事后能发现，不等于当时不会发生。本件补的是 ②。

作用域规则：
  · 【RV…】起、至该行行尾或下一个【为止，视为 RV 段，段内一律不替换；
  · 同理遮蔽紧随其后的「⟹ 处置行」——那是执笔方自己写的，但它复述评审方原话；
  · 若替换模式**只**命中 RV 段（即遮蔽后零命中），判为误操作并拒绝，
    而不是静默地什么都不做 —— 静默零命中会被读成「已清理干净」。
"""
import re, sys, argparse

# ⚠️ 段止于【行尾】而非下一个【：处置行里正常含有 **【采纳】** 这样的方括号，
#   若以「下一个【」收尾，遮蔽会在处置行中途断开，复述文字仍会被替换 —— 已实测踩到。
#   代价是同行内标注之后的正文也被遮蔽；宁可漏改，不可改坏评审方原文。
RV_SEG = re.compile(r'【RV[^】]*】[^\n]*')

def mask(text):
    segs = []
    def _m(m):
        segs.append(m.group(0))
        return f'\x00RV{len(segs)-1}\x00'
    return RV_SEG.sub(_m, text), segs

def unmask(text, segs):
    for i, s in enumerate(segs):
        text = text.replace(f'\x00RV{i}\x00', s)
    return text

def replace(text, old, new):
    masked, segs = mask(text)
    n_all = text.count(old)
    n_body = masked.count(old)
    if n_all and not n_body:
        raise SystemExit(
            f'✗ 拒绝：模式 {old!r} 在正文中零命中，{n_all} 次命中全部落在 RV 标注段内。\n'
            '  🔴 这正是「工具改写评审方原文」的形态。如确要改标注，须从评审方分支取回原文。')
    return unmask(masked.replace(old, new), segs), n_body, n_all - n_body

def selftest():
    ok = fail = 0
    def chk(cond, msg):
        nonlocal ok, fail
        if cond: ok += 1; print(f'  ✓ {msg}')
        else:    fail += 1; print(f'  ✗ 反测未过：{msg}')

    t = '正文写三级映射。\n【RV-丙05|丙|非阻断|采纳】本行「三级映射」沿用旧模型名　　⟹ **【采纳】** 已改三级映射\n尾部三级映射。'  # rv-fixture
    out, nb, ns = replace(t, '三级映射', '§21.1 双层结构')
    chk('【RV-丙05|丙|非阻断|采纳】本行「三级映射」沿用旧模型名' in out, '标注正文原样保留（丙域抓到的那一类）')  # rv-fixture
    chk('⟹ **【采纳】** 已改三级映射' in out, '处置行内的复述也不被改写')
    chk(out.startswith('正文写§21.1 双层结构。'), '正文照常替换')
    chk(out.endswith('尾部§21.1 双层结构。'), '标注之后的正文照常替换')
    chk((nb, ns) == (2, 2), f'命中计数分正文/标注两侧（实得 {nb}/{ns}）')

    # 🔴 只命中标注段时必须拒绝，不能静默零替换 —— 静默会被读成「已清理干净」
    try:
        replace('【RV-甲01|甲|阻断|采纳】只有标注里有旧词三级映射\n', '三级映射', 'X')  # rv-fixture
        chk(False, '只命中标注段时应拒绝')
    except SystemExit:
        chk(True, '拦住：模式只命中标注段（判误操作，非静默零替换）')

    # 多行文档里标注不吃掉后续行
    t2 = '【RV-甲02|甲|阻断|未决】旧词\n下一行旧词\n'  # rv-fixture
    out2, nb2, _ = replace(t2, '旧词', '新词')
    chk(out2 == '【RV-甲02|甲|阻断|未决】旧词\n下一行新词\n', 'RV 段止于行尾，不吃掉后续行')  # rv-fixture

    print(f'--- 反测 {ok} 项{"全过（件有牙）" if not fail else "未过"}')
    return 1 if fail else 0

if __name__ == '__main__':
    ap = argparse.ArgumentParser()
    ap.add_argument('--selftest', action='store_true')
    ap.add_argument('files', nargs='*')
    ap.add_argument('--old'); ap.add_argument('--new')
    a = ap.parse_args()
    if a.selftest: sys.exit(selftest())
    if not (a.files and a.old is not None and a.new is not None):
        sys.exit('用法：safe-replace.py --old 旧 --new 新 <文件…>  |  --selftest')
    for f in a.files:
        s = open(f, encoding='utf-8').read()
        out, nb, ns = replace(s, a.old, a.new)
        if out != s: open(f, 'w', encoding='utf-8').write(out)
        print(f'  {f}：正文替换 {nb} 处 · RV 段内保留 {ns} 处')
