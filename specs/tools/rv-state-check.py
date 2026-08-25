#!/usr/bin/env python3
"""RV 状态/时点一致性单次解析 —— 替代 prose-style-gate 里的逐行 shell 循环。

🔴 立此件的直接原因（甲域 sol，R2 轮5 · RV-甲46）：
  原实现是 `while read _line` 逐行循环，每行再起 printf|grep|sed 四个进程。
  三件正文约七千行 ⇒ **两万余次 fork**，真实规模上 `--rv` 跑 47–60 秒以上，
  在门方那里直接 `timeout 60` 未完成。
  ⚠️ **selftest 的夹具只有几行，38/38 全过覆盖不到真实规模** ——
    这是「反测有盲区」的一个新形态：盲的不是逻辑分支，是**输入规模**。
  ⇒ 判据没变，实现改为单进程一次解析；并加时限反测（合成大文件）。

判据（三条，与原 shell 实现逐条等价）：
  1. 处置文本里出现的状态记号必须与第 4 列一致 —— 状态只由第 4 列表达；
  2. 同一条 RV 不得出现多个时点；
  3. 时点不得是相对表述（会到期），须具体日期。

⚠️ 判前先剥引文（「」『』“”）：评审方**引述**缺陷文本是常态，
  不剥的话闸会把「指出问题的那句话」当成问题本身（甲域 RV-甲42 实例）。
"""
import re, sys

RV = re.compile(r'【(RV-[^|]+)\|(甲|乙|丙|执笔)\|(阻断|非阻断)\|(未决|采纳|驳回|挂账)】')
QUOTE = re.compile(r'「[^」]*」|“[^”]*”|『[^』]*』')
STATE = re.compile(r'【(未决|采纳|驳回|挂账)】')
TP = re.compile(r'时点[=＝][^（，。；*]{0,20}')
RELATIVE = re.compile(r'轮[0-9一二三四五六七八九]+ ?前|本轮|下轮|收官前')


def check_file(path):
    """返回错误行列表；空列表＝通过。"""
    errs = []
    with open(path, encoding='utf-8', errors='replace') as fh:
        for line in fh:
            if 'rv-fixture' in line:
                continue
            if '【RV-' not in line:
                continue                      # 绝大多数行在此一次性跳过
            l = QUOTE.sub('', line)
            m = RV.search(l)
            if not m:
                continue
            rid, st = m.group(1), m.group(4)
            tail = l.split('⟹', 1)[1] if '⟹' in l else ''
            for emb in {x for x in STATE.findall(tail)}:
                if emb != st:
                    errs.append(f'✗ {path}：{rid} 第 4 列为【{st}】，处置文本里却又写着【{emb}】'
                                f' —— 两处互相矛盾，读的人不知信哪个\n'
                                f'  🔴 状态只能由第 4 列表达；处置列只写【做了什么】。')
            tps = sorted(set(TP.findall(l)))
            if len(tps) > 1:
                errs.append(f'✗ {path}：{rid} 同一条 RV 出现多个时点 —— 无法判定以哪个为准\n'
                            + '\n'.join('    ' + t for t in tps))
            for t in tps:
                if RELATIVE.search(t):
                    errs.append(f'✗ {path}：{rid} 用了相对时点（{t}）'
                                f' —— 相对时点会到期，须写具体日期 YYYY-MM-DD')
    return errs


def main(paths):
    bad = 0
    for p in paths:
        for e in check_file(p):
            print(e); bad = 1
    return bad


def selftest():
    import tempfile, os, time
    ok = fail = 0
    def chk(c, m):
        nonlocal ok, fail
        if c: ok += 1; print(f'  ✓ {m}')
        else: fail = 1; print(f'  ✗ 反测未过：{m}')
    t = tempfile.mkdtemp()
    def w(s):
        p = os.path.join(t, 'a.md')
        open(p, 'w', encoding='utf-8').write(s)
        return p

    chk(check_file(w('【RV-甲01|甲|阻断|挂账】某条　　⟹ **【挂账】** 责任人=X；时点＝2026-09-01\n')) == [],  # rv-fixture
        '不误伤：处置内记号与第 4 列一致时放行')
    chk(len(check_file(w('【RV-甲02|甲|阻断|采纳】某条　　⟹ **【挂账】** 责任人=X\n'))) == 1,  # rv-fixture
        '拦住：处置文本冒充状态列（甲域 RV-甲42）')
    chk(len(check_file(w('【RV-甲03|甲|阻断|挂账】某条　　⟹ **【挂账】** 时点＝2026-09-01；时点=R2 轮3 前\n'))) >= 1,  # rv-fixture
        '拦住：同一 RV 多个时点')
    chk(len(check_file(w('【RV-甲04|甲|阻断|挂账】某条　　⟹ **【挂账】** 时点=R2 轮3 前\n'))) == 1,  # rv-fixture
        '拦住：相对时点（会到期）')
    chk(check_file(w('【RV-甲05|甲|阻断|采纳】闸只查绝对日期，故「时点=R2 轮3 前」仍全绿　　⟹ **【采纳】** 已修\n')) == [],  # rv-fixture
        '不误伤：标注中【引述】的相对时点不算真时点（先剥引文）')
    chk(check_file(w('【RV-甲06|甲|阻断|采纳】某条　　⟹ **【采纳】** 已修   # rv-fixture\n')) == [],
        '不误伤：rv-fixture 标记行跳过')

    # 🔴 时限反测（甲域 RV-甲46 明确要求）：夹具只有几行，覆盖不到真实规模。
    #   合成一个比三件正文更大的输入，断言单次解析在秒级完成。
    big = os.path.join(t, 'big.md')
    with open(big, 'w', encoding='utf-8') as fh:
        for i in range(20000):
            fh.write(f'第 {i} 行普通正文，不含标注。\n')
            if i % 200 == 0:
                fh.write(f'【RV-甲{i}|甲|阻断|采纳】某条　　⟹ **【采纳】** 已修\n')  # rv-fixture
    t0 = time.time(); check_file(big); dt = time.time() - t0
    chk(dt < 5, f'时限：2 万行合成件单次解析 {dt:.2f}s < 5s（原逐行 shell 实现在真实规模上 >60s）')

    print(f'--- 反测 {ok} 项{"全过（件有牙）" if not fail else "未过"}')
    return 1 if fail else 0


if __name__ == '__main__':
    if '--selftest' in sys.argv:
        sys.exit(selftest())
    sys.exit(main([a for a in sys.argv[1:] if not a.startswith('--')]))
