#!/bin/sh
set -eu
BASE_ANCHOR=6a98629
SPECDOC_ANCHOR=4220195
SPEC=specs/sp8/spec.md
got=$(git show "$SPECDOC_ANCHOR:docs/dingwei-platform-specdoc.md" | sha256sum | awk '{print $1}')
[ "$got" = '66df042ec49e6c7345e5c5cdbe1cf4e7f5c3bbdc94a1c671bb35c588532e8e0f' ] || { echo '✗ v3.73 SpecDoc 输入漂移'; exit 2; }
for item in \
 'docs/dingwei-platform-architecture.md:0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc' \
 'docs/dingwei-prototype.html:eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8' \
 'docs/dingwei-sp0-design.md:f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec' \
 'docs/dingwei-sp1-design.md:8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf' \
 'docs/dingwei-sp2-design.md:37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86' \
 'docs/dingwei-sp3-design.md:e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97'; do
 f=${item%%:*}; want=${item#*:}; got=$(git show "$BASE_ANCHOR:$f" | sha256sum | awk '{print $1}')
 [ "$got" = "$want" ] || { echo "✗ 冻结输入漂移：$f"; exit 2; }
done
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for face in ① ② ③ ④ ⑤ ⑥; do grep -q "^### $face" "$SPEC" || { echo "✗ 缺必备面 $face"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "D-SP8-$n" "$SPEC" || { echo "✗ 缺 D-SP8-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "AC-SP8-$n" "$SPEC" || { echo "✗ 缺 AC-SP8-$n"; exit 1; }; done
for id in SubmitSpec DecidePending ResolveGateSet IssueEffectGrant; do
 grep -q "GOV-\(CMD\|QRY\)-$id" "$SPEC" || { echo "✗ 缺 GOV 接口 $id"; exit 1; }; done
# 事件面：本域实算为 0 ⇒ 必须显式声明不适用，且不得误抄 SP4 的 DERIVED
grep -q '<!-- EVENT-SET-SP8: NOT_APPLICABLE -->' "$SPEC" || { echo '✗ GOV event 空面未机器声明'; exit 2; }
! grep -q 'EVENT-SET-SP8: DERIVED' "$SPEC" || { echo '✗ 本域无事件却声明为机器派生'; exit 2; }
EVENT_N=$(git show "$BASE_ANCHOR:docs/dingwei-platform-architecture.md" | grep -Ec '^\| `EV-GOV-[^`]+` \| GOV \|' || true)
[ "$EVENT_N" -eq 0 ] || { echo "✗ GOV event 空面与架构实数冲突：$EVENT_N"; exit 2; }
for dep in SP1 SP0 SP2 SP3 SP4 SP7a SP7b; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置/协作处置缺 $dep"; exit 1; }; done
# 同域差集规则必须写成判据，并复算「SP0 已认领入口非空、且差集真扣掉了东西」
grep -q '差集派生' "$SPEC" || { echo '✗ GOV 同域差集派生规则未落为判据'; exit 1; }
DIFF=$(git show "$BASE_ANCHOR:docs/dingwei-sp0-design.md" | python3 -c '
import re,sys
t=sys.stdin.read(); m=re.search(r"COVERAGE-SP0-BEGIN -->(.*?)<!-- COVERAGE-SP0-END",t,re.S)
c=set()
for l in (m.group(1).splitlines() if m else []):
 if not l.startswith("|") or "D-SP0-" not in l: continue
 mm=re.match(r"#([a-z]\w*)",l.split("|")[1].strip().strip("`"))
 if mm: c.add(mm.group(1))
print(len(c))
')
[ "$DIFF" -gt 0 ] || { echo '✗ 差集减数为空：SP0 未认领任何入口，差集规则失效'; exit 2; }
grep -q 'capability effect' "$SPEC" || { echo '✗ 紧急通道判据未按 capability effect 落合同'; exit 1; }
grep -q '签数由批内最高危项决定' "$SPEC" || { echo '✗ 批次签数判据未落合同'; exit 1; }
grep -q '不把批准语义留白' "$SPEC" || { echo '✗ 对下游 SP4 的交付义务未落合同'; exit 1; }
# heading 单区间取法必须写成判据
grep -q '单区间' "$SPEC" || { echo '✗ heading 单区间取法未落为判据'; exit 1; }
# 回避边界：判据行（D 表与 ACCEPTANCE 块）不得引用 §11.6a/§11.6b；声明段可以提它
grep -q '<!-- RECUSAL-SP8: NO-REF-11.6AB -->' "$SPEC" || { echo '✗ 回避边界未在合同以机器标记点明'; exit 1; }
REF=$(python3 - "$SPEC" <<'PY2'
import re,sys
t=open(sys.argv[1],encoding='utf8').read()
rows=[l for l in t.splitlines() if re.match(r'^\| `(D|AC)-SP8-\d{2}`',l)]
blk=re.search(r'ACCEPTANCE-SP8-BEGIN -->(.*?)<!-- ACCEPTANCE-SP8-END',t,re.S)
rows+= (blk.group(1).splitlines() if blk else [])
print(sum(1 for l in rows if re.search(r'§11\.6[ab]',l)))
PY2
)
[ "$REF" -eq 0 ] || { echo "✗ 有 $REF 条 D/AC 判据引用 §11.6a/§11.6b，须显式标注 opus50 对该条回避"; exit 1; }
for reviewer in zzc-chatgpt56soltchangshark-2642 zzc-opus50tchangshark-2642 zzc-deepseekv4pro08130ca16c13-2642; do
 grep -q "$reviewer" "$SPEC" || { echo "✗ 缺评审票：$reviewer"; exit 1; }; done
grep -q '3/3' "$SPEC" && grep -q 'AC-E05' "$SPEC" || { echo '✗ 三票门未落 AC-E05'; exit 1; }
ACTIVE=$(sed -n '/^### 4\.1 评审协议/,/^## 5\./p' "$SPEC")
printf '%s\n' "$ACTIVE" | grep -q '最多 4 轮' || { echo '✗ 生效协议缺 4 轮上限'; exit 1; }
! printf '%s\n' "$ACTIVE" | grep -q '6 轮' || { echo '✗ 生效协议残留 6 轮'; exit 1; }
# 机器派生分母：只要求非空并打印实算值，不与手写常数比对
SELFTEST_OUT=$(specs/sp8/check.sh --selftest) || { echo '✗ SP8 coverage checker 反测未过'; exit 1; }
SELFTEST_N=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP8 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$SELFTEST_N" ] || { echo '✗ coverage checker 未报告实算反测数'; exit 2; }
SETS=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP8 预期集合实算 //p')
[ -n "$SETS" ] || { echo '✗ coverage checker 未报告预期集合实算值'; exit 2; }
if DOC=docs/definitely-missing-sp8-design.md specs/sp8/check.sh >/dev/null 2>&1; then
 echo '✗ 目标件缺失被 coverage checker 当通过'; exit 1
else
 rc=$?; [ "$rc" -eq 2 ] || { echo "✗ 目标件缺失应 rc=2，实际 $rc"; exit 1; }
fi
echo "预期集合：六面 + D/AC各12 + GOV接口4 + 事件面NOT_APPLICABLE(实算0) + 四轮 + 三票 + 前置7 + 机器派生($SETS)"
echo "实检集合：6/6 + 12/12 + 12/12 + 4/4 + 0/0 + 1/1 + 3/3 + 7/7 + selftest=$SELFTEST_N + 缺件rc2"
echo '未知集合：0'
echo '✓ SP8 设计合同与组合冻结输入通过'
