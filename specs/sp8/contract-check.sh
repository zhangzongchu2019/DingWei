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
# ③ 协作方是通配：必须有通则行且明说本表非穷举，否则枚举必然漏域
grep -q '本表不是穷举' "$SPEC" || { echo '✗ 前置表未声明非穷举（架构 §17 协作方是通配）'; exit 1; }
grep -q '各 effect owner（架构 §17 的通配协作方 · 通则）' "$SPEC" || { echo '✗ 缺 effect owner 通则行'; exit 1; }
grep -q '新增域自动适用本行，不需 bump 本合同' "$SPEC" || { echo '✗ 通则未覆盖将来新增域'; exit 1; }
# ⑤ SP6 侧跨件登记不得丢
grep -q 'AGT-CAP-EmergencyAction' "$SPEC" || { echo '✗ §24.3a 执行入口的跨件登记缺失'; exit 1; }
grep -q 'SP4 阻断①的 SP6 侧不因 SP8 收口而闭' "$SPEC" || { echo '✗ 未登记「SP8 收口不闭 SP6 侧」'; exit 1; }
# 同域差集规则必须写成判据，并复算「SP0 已认领入口非空、且差集真扣掉了东西」
grep -q '差集派生' "$SPEC" || { echo '✗ GOV 同域差集派生规则未落为判据'; exit 1; }
# 首轮阻断①：部分认领的入口不得整块扣除，必须落成通则与判据
grep -q '部分认领的入口不得整块扣除' "$SPEC" || { echo '✗ 「部分认领不得整块扣除」未落为判据'; exit 1; }
grep -q '`s1`' "$SPEC" || { echo '✗ 合同未点名 s1 的归属处置'; exit 1; }
grep -q '只读消费、不改其语义' "$SPEC" || { echo '✗ 未写明对 SP0 已认领切片的只读口径'; exit 1; }
# opus50 复核要求①：s1 的批准权那一片必须有实体落点（不能只进集合）
grep -qE '^\| `D-SP8-08` \|.*`s1`' "$SPEC" || { echo '✗ `s1` 批准决策屏在 D 表无实体落点'; exit 1; }
grep -q '只把 `s1` 放进集合不算认领' "$SPEC" || { echo '✗ 未写明「进集合≠有落点」的落点约束'; exit 1; }
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
for tok in '待批事项摘要' '单次使用' '门 PASS 的授权层级' 'L3 门'; do
 grep -q "$tok" "$SPEC" || { echo "✗ §23.5 硬约束未进 D/AC：缺「$tok」"; exit 1; }; done
grep -q '不把批准语义留白' "$SPEC" || { echo '✗ 对下游 SP4 的交付义务未落合同'; exit 1; }
# heading 单区间取法必须写成判据
grep -q '单区间' "$SPEC" || { echo '✗ heading 单区间取法未落为判据'; exit 1; }
# 回避边界（B1）：剥离 RECUSAL 声明区后，合同全部生效内容不得出现 §11.6a/§11.6b
grep -q '<!-- RECUSAL-SP8: NO-REF-11.6AB -->' "$SPEC" || { echo '✗ 回避边界未在合同以机器标记点明'; exit 1; }
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
SELFTEST_OUT=$(specs/sp8/check.sh --selftest) || { echo '✗ SP8 coverage checker 反测未过'; exit 1; }
SELFTEST_N=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP8 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$SELFTEST_N" ] || { echo '✗ coverage checker 未报告实算反测数'; exit 2; }
SETS=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP8 预期集合实算 //p')
SCOPE=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- 受检范围：//p')
[ -n "$SCOPE" ] || { echo '✗ coverage checker 未报告受检范围'; exit 2; }
for tok in '§23–§24' 'GOV 接口' '差集' '完整认领'; do
 printf '%s\n' "$SCOPE" | grep -q "$tok" || { echo "✗ 受检范围文案缺 $tok，与实际派生不符"; exit 1; }; done
for bad in '§14–§16' 'CAP'; do
 ! printf '%s\n' "$SCOPE" | grep -q "$bad" || { echo "✗ 受检范围文案残留 SP4 口径：$bad"; exit 1; }; done
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
