#!/bin/sh
set -eu
BASE_ANCHOR=6a98629
SPECDOC_ANCHOR=4220195
SP8_ANCHOR=970effe
SPEC=specs/sp4/spec.md
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
# 冻结输入的唯一常量源在下面这段 python 里；它同时做三件事：
#   ① 在各自锚上实取并校验 sha256；② 与合同 ⑥ 表逐项对撞；③ 与 check.sh 的 PATHS 逐项对撞。
# 三方任一处写错（含 ⑥ 表把 sha 写成 64 个 0）即 exit 2，杜绝"表里写错机器不响"。
python3 - "$SPEC" "$BASE_ANCHOR" "$SPECDOC_ANCHOR" "$SP8_ANCHOR" <<'PYIN'
import hashlib,re,subprocess,sys
spec_path,base,specdoc,sp8=sys.argv[1:]
CONST={
 'docs/dingwei-platform-specdoc.md':(specdoc,'66df042ec49e6c7345e5c5cdbe1cf4e7f5c3bbdc94a1c671bb35c588532e8e0f'),
 'docs/dingwei-platform-architecture.md':(base,'0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc'),
 'docs/dingwei-prototype.html':(base,'eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8'),
 'docs/dingwei-sp0-design.md':(base,'f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec'),
 'docs/dingwei-sp1-design.md':(base,'8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf'),
 'docs/dingwei-sp2-design.md':(base,'37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86'),
 'docs/dingwei-sp3-design.md':(base,'e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97'),
 'docs/dingwei-sp8-design.md':(sp8,'4f5e4e06d29d4a9fffcdb46f413fbc37bcfd615e9af9b04215e0a6c20205a98a'),
}
# ① 实取校验
for path,(anchor,want) in sorted(CONST.items()):
    try: b=subprocess.check_output(['git','show',f'{anchor}:{path}'],stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError: print(f'✗ 冻结输入不可取：{anchor}:{path}'); raise SystemExit(2)
    if hashlib.sha256(b).hexdigest()!=want: print(f'✗ 冻结输入漂移：{anchor}:{path}'); raise SystemExit(2)
# ② ⑥ 表对撞
spec=open(spec_path,encoding='utf8').read()
m=re.search(r'### ⑥ 输入来源(.*?)(?=\n## )',spec,re.S)
if not m: print('✗ 找不到 ⑥ 输入来源节，无法与常量对撞'); raise SystemExit(2)
table={}
for l in m.group(1).splitlines():
    r=re.match(r'^\| `(docs/[^`]+)`(?: @ `([0-9a-f]{7,40})`)? \| `([0-9a-f]{64})` \|',l)
    if r: table[r.group(1)]=(r.group(2) or base, r.group(3))
if table!=CONST:
    for k in sorted(set(table)|set(CONST)):
        if table.get(k)!=CONST.get(k): print(f'✗ ⑥ 表与 checker 常量不一致：{k} 表={table.get(k)} 常量={CONST.get(k)}')
    raise SystemExit(2)
# ③ check.sh PATHS 对撞
src=open('specs/sp4/check.sh',encoding='utf8').read()
VAR={'base_anchor':base,'spec_anchor':specdoc,'sp8_anchor':sp8}
paths={}
for r in re.finditer(r"'(docs/[^']+)':\((\w+),'([0-9a-f]{64})'\)",src):
    if r.group(2) not in VAR: print(f'✗ check.sh 用了未知锚变量 {r.group(2)}'); raise SystemExit(2)
    paths[r.group(1)]=(VAR[r.group(2)],r.group(3))
if paths!=CONST:
    for k in sorted(set(paths)|set(CONST)):
        if paths.get(k)!=CONST.get(k): print(f'✗ check.sh PATHS 与 contract 常量不一致：{k} check={paths.get(k)} 常量={CONST.get(k)}')
    raise SystemExit(2)
print(f'冻结输入三方对撞：{len(CONST)} 件 × (实取 sha256 · ⑥ 表 · check.sh PATHS) 全一致')
PYIN
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for face in ① ② ③ ④ ⑤ ⑥; do grep -q "^### $face" "$SPEC" || { echo "✗ 缺必备面 $face"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "D-SP4-$n" "$SPEC" || { echo "✗ 缺 D-SP4-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "AC-SP4-$n" "$SPEC" || { echo "✗ 缺 AC-SP4-$n"; exit 1; }; done
for id in RegisterAsset RequestPromotion Distribute AcknowledgeInstall Uninstall; do
 grep -q "CAP-CMD-$id" "$SPEC" || { echo "✗ 缺 CAP 接口 $id"; exit 1; }; done
for ev in EV-CAP-DistributionRequested EV-CAP-InstallAcknowledged; do
 grep -q "$ev" "$SPEC" || { echo "✗ 缺 CAP 事件 $ev"; exit 1; }; done
# 事件面：本域有事件 ⇒ 必须声明 DERIVED，且与架构实数一致；不得误抄 SP3 的 NOT_APPLICABLE
grep -q '<!-- EVENT-SET-SP4: DERIVED -->' "$SPEC" || { echo '✗ CAP event 面未机器声明为 DERIVED'; exit 2; }
! grep -q 'EVENT-SET-SP4: NOT_APPLICABLE' "$SPEC" || { echo '✗ 本域有事件却声明为不适用'; exit 2; }
# N5：事件实数不再手写，取自跨件引用台账的机器派生值（见下方 XREF_OUT）
for dep in SP1 SP3 SP8 SP7a SP2 SP7b; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置/协作处置缺 $dep"; exit 1; }; done
grep -q '不自造批准权' "$SPEC" || { echo '✗ 批准权处置未落合同'; exit 1; }
grep -q 'SP8 §11.5a 消费侧合同接线' "$SPEC" || { echo '✗ SP8 已收口后未按其消费侧合同接线'; exit 1; }
grep -q 'docs/dingwei-sp8-design.md` @ `970effe`' "$SPEC" || { echo '✗ SP8 件未列为冻结输入'; exit 1; }
# 依赖点必须同步：D 表与 AC 都要落 permit 语义，不得只写旧 approval_ref 形状
grep -qE '^\| `D-SP4-08` \|.*execute permit' "$SPEC" || { echo '✗ D-SP4-08 未落 §11.5a 六项与 execute permit'; exit 1; }
grep -qE '^\| `AC-SP4-07` \|.*execute permit' "$SPEC" || { echo '✗ AC-SP4-07 仍是旧弱语义（缺 permit 判据）'; exit 1; }
# §11.5a 第 ⑥ 项（接管须持新 fence_token 且先对账）必须有落点，不得只写前五条
grep -qE '^\| `D-SP4-08` \|.*retry_after' "$SPEC" || { echo '✗ D-SP4-08 未落重放 IN_PROGRESS 的等待/重查语义'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*新 `fence_token`' "$SPEC" || { echo '✗ D-SP4-11 未落 lease 过期接管须持新 fence_token'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*先对账' "$SPEC" || { echo '✗ D-SP4-11 未落接管前先对账再决定是否执行'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*CAP-CMD-RequestPromotion' "$SPEC" || { echo '✗ D-SP4-11 外部 effect 接口举例缺 RequestPromotion（§0.3 明列的晋升路径）'; exit 1; }
grep -qE '^\| `AC-SP4-07` \|.*先对账' "$SPEC" || { echo '✗ AC-SP4-07 未把接管对账写成判据'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*execution_owner' "$SPEC" || { echo '✗ D-SP4-11 未落 CAS 校验 execution_owner'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*同代' "$SPEC" || { echo '✗ D-SP4-11 未落 grant 仍有效且同代'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*unknown.*reconcile' "$SPEC" || { echo '✗ D-SP4-11 未落对账不可判置 unknown 转 reconcile'; exit 1; }
grep -qE '^\| `D-SP4-11` \|.*不得盲重执行' "$SPEC" || { echo '✗ D-SP4-11 未落不得盲重执行'; exit 1; }
grep -qE '^\| `AC-SP4-07` \|.*unknown.*reconcile' "$SPEC" || { echo '✗ AC-SP4-07 未把 unknown/reconcile 写成判据'; exit 1; }
grep -qE '^\| `AC-SP4-07` \|.*不得盲重执行' "$SPEC" || { echo '✗ AC-SP4-07 未把「不得盲重执行」写成判据'; exit 1; }
# 旧数词残留（含本文件自身）由 specs/sp4/xref-check.sh 的全域穷举扫描统一判，见下方跨件引用门。
! grep -q '缺 SP8 定义时 fail-closed' "$SPEC" || { echo '✗ 验收仍允许"SP8 未定义"的旧口径'; exit 1; }
! grep -q '前置 SP8 未设计' "$SPEC" || { echo '✗ 冲突表仍写 SP8 未设计'; exit 1; }
! grep -q '回执 §0.4 三项待裁' "$SPEC" || { echo '✗ 开工门仍挂旧待裁回执'; exit 1; }
grep -q 'AGT-CAP-EmergencyAction' "$SPEC" || { echo '✗ 紧急执行入口的跨件登记缺失'; exit 1; }
grep -q '紧急路径一律 fail-closed' "$SPEC" || { echo '✗ SP6 未定义前的紧急路径处置未写死'; exit 1; }
grep -q 'SpecDoc §15.4 / §15.5 排版位置落在 §16 之后' "$SPEC" || { echo '✗ SpecDoc 排版异常未登记'; exit 1; }
grep -q '按 capability effect 判' "$SPEC" || { echo '✗ 紧急路径判据未按 capability effect 落合同'; exit 1; }
grep -q 'installation_generation' "$SPEC" || { echo '✗ 装卸 fencing 代际口径未落合同'; exit 1; }
# heading 单区间取法必须写成判据；并复算「按章分段取会丢节」以证明该判据非空谈
grep -q '禁止按章分段' "$SPEC" || { echo '✗ heading 单区间取法未落为判据'; exit 1; }
LOST=$(git show "$SPECDOC_ANCHOR:docs/dingwei-platform-specdoc.md" | python3 -c '
import re,sys
t=sys.stdin.read(); out=[]; on=False
for l in t.splitlines():
 if re.match(r"^## §15\b",l): on=True
 if re.match(r"^## §16\b",l): on=False
 if on:
  m=re.match(r"^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b",l)
  if m: out.append("§"+m.group(1))
print(len([x for x in ("§15.4","§15.5") if x not in out]))
')
[ "$LOST" -eq 2 ] || { echo "✗ 排版异常前提已变（分段取丢节数=$LOST），单区间判据须重核"; exit 2; }
# 回避边界（B1）：剥离 RECUSAL 声明区后，合同全部生效内容不得出现 §11.6a/§11.6b
grep -q '<!-- RECUSAL-SP4: NO-REF-11.6AB -->' "$SPEC" || { echo '✗ 回避边界未在合同以机器标记点明'; exit 1; }
grep -q '<!-- RECUSAL-DECL-BEGIN -->' "$SPEC" && grep -q '<!-- RECUSAL-DECL-END -->' "$SPEC" || { echo '✗ 回避声明区围栏缺失，无法界定唯一豁免区'; exit 1; }
REF=$(python3 - "$SPEC" <<'PY2'
import re,sys
t=open(sys.argv[1],encoding='utf8').read()
body=re.sub(r'<!-- RECUSAL-DECL-BEGIN -->.*?<!-- RECUSAL-DECL-END -->','',t,flags=re.S)
print(len(re.findall(r'§11\.6[ab]',body)))
PY2
)
[ "$REF" -eq 0 ] || { echo "✗ 合同生效内容中有 $REF 处引用 §11.6a/§11.6b（声明区已排除），须 bump 并逐条标注 opus50 回避"; exit 1; }
for reviewer in zzc-chatgpt56soltchangshark-2642 zzc-opus50tchangshark-2642 zzc-deepseekv4pro08130ca16c13-2642; do
 grep -q "$reviewer" "$SPEC" || { echo "✗ 缺评审票：$reviewer"; exit 1; }; done
grep -q '3/3' "$SPEC" && grep -q 'AC-E05' "$SPEC" || { echo '✗ 三票门未落 AC-E05'; exit 1; }
ACTIVE=$(sed -n '/^### 4\.1 评审协议/,/^## 5\./p' "$SPEC")
printf '%s\n' "$ACTIVE" | grep -q '最多 4 轮' || { echo '✗ 生效协议缺 4 轮上限'; exit 1; }
! printf '%s\n' "$ACTIVE" | grep -q '6 轮' || { echo '✗ 生效协议残留 6 轮'; exit 1; }
# 机器派生分母：只要求非空并打印实算值，不与手写常数比对
XREF_OUT=$(sh specs/sp4/xref-check.sh 2>&1) || { printf '%s\n' "$XREF_OUT"; echo '✗ 跨件引用门未过（合同所称项数与目标锚现算不符，或台账/正文/锚不自洽）'; exit 2; }
XREF_N=$(printf '%s\n' "$XREF_OUT" | sed -n 's/^--- SP4 跨件引用门 \([0-9][0-9]*\) 条.*$/\1/p')
[ -n "$XREF_N" ] && [ "$XREF_N" -ge 3 ] || { echo '✗ 跨件引用台账条目不足（至少须覆盖 §11.5a 项数、CAP 接口数、CAP 事件数）'; exit 2; }
EVENT_N=$(printf '%s\n' "$XREF_OUT" | sed -n 's/^XREF-VALUE arch-cap-events=\([0-9][0-9]*\)$/\1/p')
IFACE_N=$(printf '%s\n' "$XREF_OUT" | sed -n 's/^XREF-VALUE arch-cap-interfaces=\([0-9][0-9]*\)$/\1/p')
ITEM_N=$(printf '%s\n' "$XREF_OUT" | sed -n 's/^XREF-VALUE sp8-11.5a-items=\([0-9][0-9]*\)$/\1/p')
[ -n "$EVENT_N" ] && [ -n "$IFACE_N" ] && [ -n "$ITEM_N" ] || { echo '✗ 跨件引用门未报告机器派生值'; exit 2; }
sh specs/sp4/xref-check.sh --selftest >/dev/null 2>&1 || { echo '✗ 跨件引用门反测未过（加固处不加固也不转红）'; exit 2; }
SELFTEST_OUT=$(specs/sp4/check.sh --selftest) || { echo '✗ SP4 coverage checker 反测未过'; exit 1; }
SELFTEST_N=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP4 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$SELFTEST_N" ] || { echo '✗ coverage checker 未报告实算反测数'; exit 2; }
SETS=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP4 预期集合实算 //p')
[ -n "$SETS" ] || { echo '✗ coverage checker 未报告预期集合实算值'; exit 2; }
# §0.2 的机器派生快照不得只是文字：与 check.sh 实算、与跨件引用门派生值逐项对撞
SPEC="$SPEC" SETS="$SETS" IFACE_N="$IFACE_N" EVENT_N="$EVENT_N" python3 - <<'PYSNAP' || exit 2
import os,re,sys
spec=open(os.environ['SPEC'],encoding='utf8').read()
sets=dict(kv.split('=') for kv in os.environ['SETS'].split())
m=re.search(r'### 0\.2 冻结锚上的机器派生实算值(.*?)(?=\n### 0\.2a)',spec,re.S)
if not m: print('✗ 找不到 §0.2 机器派生快照节'); sys.exit(2)
snap=m.group(1)
def g(pat,label):
    r=re.search(pat,snap)
    if not r: print(f'✗ §0.2 快照缺 {label}'); sys.exit(2)
    return r.group(1)
pairs=[(g(r'§14–§16 heading = (\d+)','heading'),sets['spec'],'§14–§16 heading'),
       (g(r'owner_sp 含 SP4 的 E 行 = (\d+)','E 行'),sets['E'],'E 行'),
       (g(r'架构 §6 CAP 接口 = (\d+)','CAP 接口'),os.environ['IFACE_N'],'CAP 接口'),
       (g(r'架构 §4\.2 CAP 事件 = (\d+)','CAP 事件'),os.environ['EVENT_N'],'CAP 事件'),
       (g(r'架构 §13 主责域含 CAP 的原型入口 = (\d+)','原型入口'),sets['prototype'],'原型入口')]
bad=[(l,a,b) for a,b,l in pairs if a!=b]
for l,a,b in bad: print(f'✗ §0.2 快照与实算不符：{l} 快照={a} 实算={b}')
sys.exit(2 if bad else 0)
PYSNAP
if DOC=docs/definitely-missing-sp4-design.md specs/sp4/check.sh >/dev/null 2>&1; then
 echo '✗ 目标件缺失被 coverage checker 当通过'; exit 1
else
 rc=$?; [ "$rc" -eq 2 ] || { echo "✗ 目标件缺失应 rc=2，实际 $rc"; exit 1; }
fi
echo "预期集合：六面 + D/AC各12 + CAP接口${IFACE_N} + CAP事件${EVENT_N} + §11.5a ${ITEM_N}项 + 四轮 + 三票 + 前置6 + 机器派生($SETS) + 跨件引用台账${XREF_N}条"
echo "实检集合：6/6 + 12/12 + 12/12 + ${IFACE_N}/${IFACE_N} + ${EVENT_N}/${EVENT_N} + 1/1 + 3/3 + 6/6 + selftest=$SELFTEST_N + xref=${XREF_N}/${XREF_N} + §0.2快照对撞 + 缺件rc2"
echo '未知集合：0'
echo '✓ SP4 设计合同与组合冻结输入通过'
