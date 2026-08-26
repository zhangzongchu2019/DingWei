#!/bin/sh
set -eu
BASE_ANCHOR=6a98629
SPECDOC_ANCHOR=4220195
SPEC=specs/sp4/spec.md
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
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "D-SP4-$n" "$SPEC" || { echo "✗ 缺 D-SP4-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "AC-SP4-$n" "$SPEC" || { echo "✗ 缺 AC-SP4-$n"; exit 1; }; done
for id in RegisterAsset RequestPromotion Distribute AcknowledgeInstall Uninstall; do
 grep -q "CAP-CMD-$id" "$SPEC" || { echo "✗ 缺 CAP 接口 $id"; exit 1; }; done
for ev in EV-CAP-DistributionRequested EV-CAP-InstallAcknowledged; do
 grep -q "$ev" "$SPEC" || { echo "✗ 缺 CAP 事件 $ev"; exit 1; }; done
# 事件面：本域有事件 ⇒ 必须声明 DERIVED，且与架构实数一致；不得误抄 SP3 的 NOT_APPLICABLE
grep -q '<!-- EVENT-SET-SP4: DERIVED -->' "$SPEC" || { echo '✗ CAP event 面未机器声明为 DERIVED'; exit 2; }
! grep -q 'EVENT-SET-SP4: NOT_APPLICABLE' "$SPEC" || { echo '✗ 本域有事件却声明为不适用'; exit 2; }
EVENT_N=$(git show "$BASE_ANCHOR:docs/dingwei-platform-architecture.md" | grep -Ec '^\| `EV-CAP-[^`]+` \| CAP \|' || true)
[ "$EVENT_N" -eq 2 ] || { echo "✗ CAP 事件实数与合同不符：$EVENT_N"; exit 2; }
for dep in SP1 SP3 SP8 SP7a SP2 SP7b; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置/协作处置缺 $dep"; exit 1; }; done
grep -q '不自造批准权' "$SPEC" || { echo '✗ 批准权处置未落合同'; exit 1; }
grep -q 'SP8 §11.5a 消费侧合同接线' "$SPEC" || { echo '✗ SP8 已收口后未按其消费侧合同接线'; exit 1; }
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
SELFTEST_OUT=$(specs/sp4/check.sh --selftest) || { echo '✗ SP4 coverage checker 反测未过'; exit 1; }
SELFTEST_N=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP4 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$SELFTEST_N" ] || { echo '✗ coverage checker 未报告实算反测数'; exit 2; }
SETS=$(printf '%s\n' "$SELFTEST_OUT" | sed -n 's/^--- SP4 预期集合实算 //p')
[ -n "$SETS" ] || { echo '✗ coverage checker 未报告预期集合实算值'; exit 2; }
if DOC=docs/definitely-missing-sp4-design.md specs/sp4/check.sh >/dev/null 2>&1; then
 echo '✗ 目标件缺失被 coverage checker 当通过'; exit 1
else
 rc=$?; [ "$rc" -eq 2 ] || { echo "✗ 目标件缺失应 rc=2，实际 $rc"; exit 1; }
fi
echo "预期集合：六面 + D/AC各12 + CAP接口5 + CAP事件2 + 四轮 + 三票 + 前置6 + 机器派生($SETS)"
echo "实检集合：6/6 + 12/12 + 12/12 + 5/5 + 2/2 + 1/1 + 3/3 + 6/6 + selftest=$SELFTEST_N + 缺件rc2"
echo '未知集合：0'
echo '✓ SP4 设计合同与组合冻结输入通过'
