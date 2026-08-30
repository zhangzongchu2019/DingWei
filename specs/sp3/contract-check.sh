#!/bin/sh
set -eu
BASE_ANCHOR=53e4a2aed1569eef91e4048e7da2a1cd32db3ec2
SPECDOC_ANCHOR=fcfec9a
SPEC=specs/sp3/spec.md

# ============ 常驻反测：改回旧宽写／删能力边界／同义倒签／删行 均须因目标断言转红 ============
if [ "${1:-}" = "--selftest" ]; then
  ROOT=$(pwd); GD=$(git rev-parse --absolute-git-dir)
  T=$(mktemp -d); trap 'rm -rf "$T"' EXIT
  cat > "$T/mut.py" <<'MUT'
import re,sys
P='specs/sp3/spec.md'
def rw(f):
    t=open(P,encoding='utf8').read(); t2=f(t)
    assert t2!=t, '变异未改动 %s（靶失效）' % P
    open(P,'w',encoding='utf8').write(t2)
def row(t):
    m=re.search(r'^\| \*\*SP2（协作输入.*$',t,re.M); assert m,'找不到 SP2 协作输入行'
    return m
def setcell(t, idx, val):
    r=row(t); cells=r.group(0).split('|')
    cells[idx]=val
    return t[:r.start()]+'|'.join(cells)+t[r.end():]
# 四条**实测可绕过旧黑名单**的同义倒签形态（round2 复现所得）：
# 它们都不含 3/3／三票／全票／已…通过／…在案，旧实现全部放行；受控形态下必须全红。
BYPASS={
 'bypass1':'评审结论为 PASS',
 'bypass2':'已获三席认可',
 'bypass3':'评审已完成',
 'bypass4':'经三位评审者认可',
}
m=sys.argv[1]
if m=='oldwide':      # 改回旧宽写（round1 的原字面）
    rw(lambda t: setcell(t,2,' SP2 正文已 3/3 PASS，锚 `53e4a2a`；其 §3.3 是 §11.6 快照的引用侧 '))
elif m=='dropbound':  # 只删能力边界句，其余照旧
    rw(lambda t: t.replace('**其评审效力不由本合同自证**，','',1))
elif m=='synonym':    # 同义倒签：不含 3/3，改说"评审已通过"
    rw(lambda t: setcell(t,2,' 冻结锚 `53e4a2a` 上 SP2 正文评审已通过并在案；其评审效力不由本合同自证，以 SP2 归口登记与有效票为准 '))
elif m in BYPASS:     # 旧黑名单的实测绕过形态，逐条建靶
    rw(lambda t: setcell(t,2,' 冻结锚 `53e4a2a` 上 SP2 正文%s；**其评审效力不由本合同自证**，以 SP2 归口登记与有效票为准；其 §3.3 是 §11.6 快照的引用侧 ' % BYPASS[m]))
elif m=='cell34':     # 阳性对照：只动第三、第四列，判定域正确时必须**仍绿**
    def f(t):
        t=setcell(t,3,' 第三列改写：SP3 以 SpecDoc §11.6 表为字段全集权威源（措辞已变） ')
        return setcell(t,4,' 第四列改写：未裁前用 §11.6 表逐项 fixture（措辞已变） ')
    rw(f)
elif m=='droprow':    # 整行删除
    def f(t):
        r=row(t); return t[:r.start()]+t[r.end()+1:]
    rw(f)
elif m=='duprow':     # 保留受控行，再追加一条同名前缀旧宽写行
    def f(t):
        r=row(t)
        bad='| **SP2（协作输入，非 §0.3 前置）** | SP2 正文已 3/3 PASS 并全票通过在案 | x | y |'
        return t[:r.end()]+"\n"+bad+t[r.end():]
    rw(f)
elif m=='ac08narrow': # AC-SP3-08 去掉 fixture 收准语
    rw(lambda t: t.replace('仅按 fixture 消费合同判定','经该引用装载'))
elif m=='ac08liab':   # AC-SP3-08 去掉挂账登记
    rw(lambda t: t.replace('真实装载列为集成前置挂账','真实装载已完成'))
elif m=='ac08rowdelete':
    rw(lambda t: re.sub(r'^\| `AC-SP3-08` .*\n','',t,count=1,flags=re.M))
elif m=='ac08rowduplicate':
    def f(t):
        m=re.search(r'^\| `AC-SP3-08` .*$',t,re.M); assert m,'找不到 AC-SP3-08 验收行'
        return t[:m.end()]+'\n'+m.group(0)+t[m.end():]
    rw(f)
elif m=='ac08rowforge':
    def f(t):
        m=re.search(r'^\| `AC-SP3-08` .*$',t,re.M); assert m,'找不到 AC-SP3-08 验收行'
        cells=m.group(0).split('|'); cells[3]=' 经该引用已完成真实装载并验证 '
        return t[:m.start()]+'|'.join(cells)+t[m.end():]
    rw(f)
elif m=='reviewrounds':
    rw(lambda t: t.replace('| `max_rounds` | `8` |','| `max_rounds` | `7` |'))
elif m=='reviewprimary':
    rw(lambda t: t.replace('| `primary_per_round` | `1` |','| `primary_per_round` | `2` |'))
elif m=='reviewlegacy':
    rw(lambda t: t.replace('| `legacy_vote_effect` | `archive_only_no_transfer` |','| `legacy_vote_effect` | `transfer` |'))
elif m=='reviewduplicate':
    rw(lambda t: t.replace('<!-- REVIEW-PROTOCOL-SP3-END -->','| `max_rounds` | `8` |\n<!-- REVIEW-PROTOCOL-SP3-END -->'))
elif m=='reviewround9delete':
    rw(lambda t: t.replace('| `round9_effect` | `forbidden` |\n',''))
elif m=='reviewround9allow':
    rw(lambda t: t.replace('| `round9_effect` | `forbidden` |','| `round9_effect` | `authorized` |'))
elif m=='reviewpool':
    rw(lambda t: t.replace('zzc-qwen38maxkhovvob4-2642,zzc-sonnet50tchangshark-2642','zzc-qwen38maxkhovvob4-2642'))
elif m=='reviewroster':
    rw(lambda t: t.replace('zzc-opus50tchangshark-2642,zzc-kimi30kqtuxerh-2642','zzc-outsider1-2642,zzc-outsider2-2642'))
elif m=='reviewmapdelete':
    rw(lambda t: re.sub(r'^\| `AC-SP3-12` \| `[^`]+` \|\n','',t,count=1,flags=re.M))
elif m=='reviewmapduplicate':
    def f(t):
        m=re.search(r'^\| `AC-SP3-08` \| `[^`]+` \|$',t,re.M); assert m,'找不到 AC08 来源映射'
        return t[:m.end()]+'\n'+m.group(0)+t[m.end():]
    rw(f)
else: sys.exit('未知变异模式 '+m)
MUT
  fresh() { rm -rf "$T/w"; mkdir -p "$T/w"; cp -r "$ROOT/docs" "$ROOT/specs" "$T/w/"; }
  n=0; fail=0
  fresh
  if ( cd "$T/w" && GIT_DIR="$GD" sh specs/sp3/contract-check.sh >/dev/null 2>&1 ); then
    echo '  ✓ [基线] 未变异副本为绿（下面每格的前置条件成立）'
  else
    echo '  ✗ [基线] 未变异副本不绿 —— 所有格都测不到目标断言'; exit 1
  fi
  cell() { # $1=格名 $2=变异模式 $3=必须命中的唯一错误标签
    n=$((n+1)); fresh
    if ! ( cd "$T/w" && python3 "$T/mut.py" "$2" ) >"$T/mut.log" 2>&1; then
      echo "  ✗ [$1] 变异命令自身失败（靶失效）"; tail -1 "$T/mut.log"; fail=1; return 0
    fi
    out=$( cd "$T/w" && GIT_DIR="$GD" sh specs/sp3/contract-check.sh 2>&1 ) && rc=0 || rc=$?
    if [ "$rc" -eq 0 ]; then echo "  ✗ [$1] 变异后仍绿（无格）"; fail=1; return 0; fi
    case "$out" in
      *"$3"*) echo "  ✓ [$1] rc=$rc 命中 $3" ;;
      *) echo "  ✗ [$1] rc=$rc 未命中 $3，实得："; printf '%s\n' "$out" | head -3; fail=1 ;;
    esac
    return 0
  }
  cellgreen() { # 阳性对照：变异后**必须仍绿**（证明判定域确实收窄，而不是嘴上说收窄）
    n=$((n+1)); fresh
    if ! ( cd "$T/w" && python3 "$T/mut.py" "$2" ) >"$T/mut.log" 2>&1; then
      echo "  ✗ [$1] 变异命令自身失败（靶失效）"; tail -1 "$T/mut.log"; fail=1; return 0
    fi
    if ( cd "$T/w" && GIT_DIR="$GD" sh specs/sp3/contract-check.sh >/dev/null 2>&1 ); then
      echo "  ✓ [$1] 变异后仍绿（判定域未越界）"
    else
      echo "  ✗ [$1] 变异后转红 —— 判定域越界到了不该判的列"; fail=1
    fi
    return 0
  }
  cell '改回旧宽写 3/3 PASS' oldwide   '[E-SP2-CELL-NONCANON]'
  cell '同义倒签（无票数）'  synonym   '[E-SP2-CELL-NONCANON]'
  cell '删能力边界句'        dropbound '[E-SP2-CELL-NONCANON]'
  # 旧黑名单的四条实测绕过形态：逐条建靶，受控形态下必须全红
  cell '绕过①评审结论为 PASS'  bypass1 '[E-SP2-CELL-NONCANON]'
  cell '绕过②已获三席认可'     bypass2 '[E-SP2-CELL-NONCANON]'
  cell '绕过③评审已完成'       bypass3 '[E-SP2-CELL-NONCANON]'
  cell '绕过④经三位评审者认可' bypass4 '[E-SP2-CELL-NONCANON]'
  # 阳性对照：判定域只在第二单元格 —— 三四列改写不得转红
  cellgreen '阳性对照：只改第三/第四列' cell34
  cell '删整行 SP2 协作输入' droprow   '[E-SP2-ROW-MISSING]'
  cell '重复行不得以存在量词假绿' duprow '[E-SP2-ROW-DUPLICATE]'
  cell 'AC08 去掉 fixture 收准语' ac08narrow '[E-AC08-OVERCLAIM]'
  cell 'AC08 去掉挂账登记'        ac08liab   '[E-AC08-LIABILITY-MISSING]'
  cell 'AC08 验收行删除'          ac08rowdelete '[E-AC08-ROW-MISSING]'
  cell 'AC08 验收行重复'          ac08rowduplicate '[E-AC08-ROW-DUPLICATE]'
  cell 'AC08 验收单元格伪造'      ac08rowforge '[E-AC08-CELL-NONCANON]'
  cell '评审上限漂移必须转红'     reviewrounds '[E-REVIEW-PROTOCOL]'
  cell '单轮双主评审必须转红'     reviewprimary '[E-REVIEW-PROTOCOL]'
  cell '旧票转移效力必须转红'     reviewlegacy '[E-REVIEW-PROTOCOL]'
  cell '协议键重复必须转红'       reviewduplicate '[E-REVIEW-PROTOCOL]'
  cell '第9轮禁止句删除必须转红'   reviewround9delete '[E-REVIEW-PROTOCOL]'
  cell '授权性第9轮必须转红'       reviewround9allow '[E-REVIEW-PROTOCOL]'
  cell '窄票替代池漂移必须转红'     reviewpool '[E-REVIEW-PROTOCOL]'
  cell '主评审名单漂移必须转红'       reviewroster '[E-REVIEW-PROTOCOL]'
  cell '验收面来源映射缺行必须转红'   reviewmapdelete '[E-REVIEW-SURFACE-MAP]'
  cell '验收面来源映射重复必须转红'   reviewmapduplicate '[E-REVIEW-SURFACE-MAP]'
  [ "$fail" -eq 0 ] || { echo "--- SP3 合同反测 $n 项中有失败"; exit 1; }
  echo "--- SP3 合同反测 $n 项全过"; exit 0
fi
got=$(git show "$SPECDOC_ANCHOR:docs/dingwei-platform-specdoc.md" | sha256sum | awk '{print $1}')
[ "$got" = 'bb4cf7becda882e3bafe35b3c293e0577783c4880b756e013ef861b088d8dcaa' ] || { echo '✗ v3.72 SpecDoc 输入漂移'; exit 2; }
for item in \
 'docs/dingwei-platform-architecture.md:0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc' \
 'docs/dingwei-prototype.html:eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8' \
 'docs/dingwei-sp0-design.md:f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec' \
 'docs/dingwei-sp1-design.md:8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf' \
 'docs/dingwei-sp2-design.md:37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86'; do
 f=${item%%:*}; want=${item#*:}; got=$(git show "$BASE_ANCHOR:$f" | sha256sum | awk '{print $1}')
 [ "$got" = "$want" ] || { echo "✗ 冻结输入漂移：$f"; exit 2; }
done
[ -f "$SPEC" ] || { echo "✗ 缺 $SPEC"; exit 2; }
for face in ① ② ③ ④ ⑤ ⑥; do grep -q "^### $face" "$SPEC" || { echo "✗ 缺必备面 $face"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "D-SP3-$n" "$SPEC" || { echo "✗ 缺 D-SP3-$n"; exit 1; }; done
for n in 01 02 03 04 05 06 07 08 09 10 11 12; do grep -q "AC-SP3-$n" "$SPEC" || { echo "✗ 缺 AC-SP3-$n"; exit 1; }; done
for id in IngestItem Search BuildContextSnapshot MarkLifecycle; do grep -q "KNO-.*-$id" "$SPEC" || { echo "✗ 缺 KNO 接口 $id"; exit 1; }; done
grep -q '<!-- EVENT-SET-SP3: NOT_APPLICABLE -->' "$SPEC" || { echo '✗ KNO event 空面未机器声明'; exit 2; }
EVENT_N=$(git show "$BASE_ANCHOR:docs/dingwei-platform-architecture.md" | grep -Ec '^\| `EV-KNO-[^`]+` \| KNO \|' || true)
[ "$EVENT_N" -eq 0 ] || { echo "✗ KNO event 空面与架构实数冲突：$EVENT_N"; exit 2; }
# ============ S3-B1：SP2 协作输入行「冻结事实」单元的受控形态判定 ============
# round2 判 NO 的原因：原实现是**有限形态黑名单**（3/3／三票／全票／已…通过 等 5 条），
# 却在注释里声称「任何同义形态必红」。实测可绕过的同义写法至少 4 条
# （「评审结论为 PASS」「已获三席认可」「评审已完成」「经三位评审者认可」）：
# 它们都不含黑名单里的任何一条，却同样把 SP2 的评审状态倒签成冻结锚可见事实。
# ⇒ 黑名单天然不穷尽，**声称穷尽就是声明大于实现**。
#
# round3 闭法（按 sol 令）：改为**受控形态白名单**，且**判定域只取第二单元格**。
#   · 第二单元格＝该行「冻结事实」列，即本合同对 SP2 唯一的事实陈述面；
#   · 第三、第四列分别是「本件如何消费」与「未裁前如何处置」，**不属事实陈述，不在判定域**
#     （原实现扫整行，等于把这两列也纳进来，既超范围又制造假红面）；
#   · 白名单＝逐字受控形态，唯一可变部分是冻结锚的 sha 字面。
#     任何其他写法——无论是否同义、是否在旧黑名单内——**一律红**。
#     这条声明现在与实现等价：白名单的补集就是「其他一切」，不存在绕过面。
SP2ROW=$(grep '^| \*\*SP2（协作输入' "$SPEC" || true)
[ -n "$SP2ROW" ] || { echo '✗ [E-SP2-ROW-MISSING] 前置处置缺 SP2 协作输入行'; exit 1; }
SP2ROW_N=$(printf '%s\n' "$SP2ROW" | grep -c '^|' || true)
[ "$SP2ROW_N" -eq 1 ] || { echo "✗ [E-SP2-ROW-DUPLICATE] SP2 协作输入行须恰一行，实得 $SP2ROW_N"; exit 1; }
# 判定域收窄：只取第二单元格（awk 以 | 分列，$1 为行首空段，$2 为名称列，$3 即第二单元格）
SP2CELL2=$(printf '%s\n' "$SP2ROW" | awk -F'|' '{print $3}')
[ -n "$SP2CELL2" ] || { echo '✗ [E-SP2-CELL-MISSING] SP2 行取不到第二单元格'; exit 1; }
# 受控形态（白名单）：除冻结锚 sha 外逐字固定
SP2_CANON='^ *冻结锚 `[0-9a-f]{7,40}` 上 SP2 正文候选件字节可取；\*\*其评审效力不由本合同自证\*\*，以 SP2 归口登记与有效票为准；其 §3\.3 是 §11\.6 快照的引用侧 *$'
if ! printf '%s\n' "$SP2CELL2" | grep -Eq "$SP2_CANON"; then
  echo '✗ [E-SP2-CELL-NONCANON] SP2 行第二单元格不合受控形态（只允许受控形态本身；任何改写——含把评审状态倒签成冻结事实的同义写法——均在此转红）'
  echo "  实得：$SP2CELL2"
  exit 1
fi
for dep in SP1 SP7a SP7b SP2; do grep -q "| \*\*$dep" "$SPEC" || { echo "✗ 前置处置缺 $dep"; exit 1; }; done
grep -Eq '§11\.6 .*唯一权威源' "$SPEC" || { echo '✗ snapshot 字段权威源未钉为 §11.6'; exit 1; }
grep -q 'SpecDoc §11.6 与 SP2 §3.3 snapshot 字段不一致' "$SPEC" || { echo '✗ snapshot 字段冲突未登记'; exit 1; }
for token in memory_type deidentification_evidence_ref ExtractionProfile; do grep -q "$token" "$SPEC" || { echo "✗ 正文合同缺 $token"; exit 1; }; done
grep -q '§11.6a 仅 execution_failed/dependency_unavailable 与 gate_fail' "$SPEC" || { echo '✗ v3.72 failure_class 收窄未落合同'; exit 1; }
grep -q 'cancelled/timeout/effect_unknown 按 §11.6b' "$SPEC" || { echo '✗ v3.72 终结/未知分类未落合同'; exit 1; }
# ============ AC-SP3-08：前序证据「仅 fixture 消费 + 真实集成挂账」的受控措辞 ============
# round2 判 NO 的原因：AC-SP3-08 称「经 envelope_ref.prior_state_evidence_ref 装载」，
# 而该字段在冻结 SpecDoc 全文命中 0（未定义），正文 §11.1 映射表登记为「待上游确认、未确认前只用 fixture」。
# ⇒ 合同把「设计阶段的 fixture 消费」写成了「已完成真实装载」。
# 本处只判措辞受控；「SpecDoc 是否已定义 ⇄ 合同口径 ⇄ 正文挂账」三方对撞在 check.sh 的
# prior_state_gate 里做（那里能拿到冻结 SpecDoc 正文，且可被 --selftest 注入变体）。
grep -q 'envelope_ref.prior_state_evidence_ref' "$SPEC" || { echo '✗ [E-AC08-REF-MISSING] §11.6b 前序证据未落装载合同'; exit 1; }
grep -q '仅按 fixture 消费合同判定' "$SPEC" \
  || { echo '✗ [E-AC08-OVERCLAIM] 合同未把 prior_state_evidence_ref 收准为「仅按 fixture 消费合同判定」'; exit 1; }
grep -q '真实装载列为集成前置挂账' "$SPEC" \
  || { echo '✗ [E-AC08-LIABILITY-MISSING] 合同未登记 prior_state_evidence_ref 的真实集成前置挂账'; exit 1; }
grep -q 'N/A:not_failure' "$SPEC" || { echo '✗ §11.6b 非失败槽位判别值未落合同'; exit 1; }
python3 - "$SPEC" <<'PY'
import re,sys
text=open(sys.argv[1],encoding='utf8').read()
# AC-SP3-08 必须是 ACCEPTANCE 块内恰一条 canonical 行；其它章节的同名 token 不得喂绿。
ab=re.findall(r'^<!-- ACCEPTANCE-SP3-BEGIN -->$(.*?)^<!-- ACCEPTANCE-SP3-END -->$',text,re.M|re.S)
if len(ab)!=1:
 print(f'✗ [E-AC08-ROW-MISSING] ACCEPTANCE 块须恰一，实得 {len(ab)}'); raise SystemExit(1)
rows=[line for line in ab[0].splitlines() if re.match(r'^\| `AC-SP3-08` \|',line)]
if not rows:
 print('✗ [E-AC08-ROW-MISSING] AC-SP3-08 验收行缺失'); raise SystemExit(1)
if len(rows)!=1:
 print(f'✗ [E-AC08-ROW-DUPLICATE] AC-SP3-08 验收行须恰一，实得 {len(rows)}'); raise SystemExit(1)
cells=[c.strip() for c in rows[0].strip().strip('|').split('|')]
required=('仅按 fixture 消费合同判定','真实装载列为集成前置挂账','冻结 SpecDoc §11.6 未定义','正文 §11.1 映射表')
forbidden=('已完成真实装载','真实装载已完成','经该引用装载')
if len(cells)!=4 or not all(x in cells[2] for x in required) or any(x in cells[2] for x in forbidden):
 print('✗ [E-AC08-CELL-NONCANON] AC-SP3-08 判据单元格不合受控形态'); raise SystemExit(1)
# 回避按 AC 面执行；来源映射必须覆盖 12 面恰一次，避免文件/函数到验收面边界悬空。
maps=re.findall(r'^<!-- REVIEW-SURFACE-MAP-SP3-BEGIN -->$(.*?)^<!-- REVIEW-SURFACE-MAP-SP3-END -->$',text,re.M|re.S)
if len(maps)!=1:
 print(f'✗ [E-REVIEW-SURFACE-MAP] 映射块须恰一，实得 {len(maps)}'); raise SystemExit(1)
ids=re.findall(r'^\| `(AC-SP3-\d{2})` \| `[^`]+` \|$',maps[0],re.M)
want_ids=[f'AC-SP3-{i:02d}' for i in range(1,13)]
if sorted(ids)!=want_ids or len(ids)!=len(set(ids)):
 print(f'✗ [E-REVIEW-SURFACE-MAP] 映射集合不符：got={ids}'); raise SystemExit(1)
m=list(re.finditer(r'^<!-- REVIEW-PROTOCOL-SP3-BEGIN -->$(.*?)^<!-- REVIEW-PROTOCOL-SP3-END -->$',text,re.M|re.S))
if len(m)!=1:
 print(f'✗ [E-REVIEW-PROTOCOL] 协议块须恰一，实得 {len(m)}'); raise SystemExit(1)
got={}; dup=[]
for line in m[0].group(1).splitlines():
 c=[x.strip().strip('`') for x in line.strip().strip('|').split('|')]
 if len(c)==2 and c[0] not in ('key','---') and not c[0].startswith('---'):
  if c[0] in got: dup.append(c[0])
  got[c[0]]=c[1]
want={
 'owner':'zzc-chatgpt56solzhangzongchu-2642', 'max_rounds':'8',
 'round_ids':'N1,N2,N3,N4,N5,N6,N7,N8',
 'primary_reviewers':'zzc-opus50tchangshark-2642,zzc-kimi30kqtuxerh-2642',
 'primary_per_round':'1', 'rotation':'alternating', 'review_scope':'full_document',
 'recused_narrow_vote_per_surface':'1', 'narrow_reviewer_requirement':'no_involvement',
 'legacy_vote_effect':'archive_only_no_transfer', 'round9_effect':'forbidden',
 'permanent_recusal':'zzc-opus50tchangshark-2642:AC-SP3-08',
 'narrow_replacement_pool':'zzc-qwen38maxkhovvob4-2642,zzc-sonnet50tchangshark-2642',
 'registry_encoding':'authorization_ALL_or_surface_no_ballot_kind'}
if got!=want or dup:
 print(f'✗ [E-REVIEW-PROTOCOL] 协议台账不符：missing={sorted(set(want)-set(got))} unknown={sorted(set(got)-set(want))} dup={dup}'); raise SystemExit(1)
hist=open('specs/sp3/REVIEW-HISTORY.md',encoding='utf8').read()
for needle in ('5aca86595d505c9a07c3c56168eae52d9799e19e','legacy_vote_effect = archive_only_no_transfer','N1–N8'):
 if needle not in hist:
  print(f'✗ [E-REVIEW-PROTOCOL] 评审沿革缺 {needle}'); raise SystemExit(1)
PY
SELFTEST=$(specs/sp3/check.sh --selftest) || { echo '✗ SP3 checker 反测失败'; exit 1; }
N=$(printf '%s\n' "$SELFTEST" | sed -n 's/^--- SP3 覆盖反测 \([0-9][0-9]*\) 项全过$/\1/p')
[ -n "$N" ] || { echo '✗ SP3 checker 未报告实算反测数'; exit 2; }
COUNTS=$(printf '%s\n' "$SELFTEST" | sed -n 's/^--- SP3 预期集合实算 //p')
[ -n "$COUNTS" ] || { echo '✗ SP3 checker 未报告机器派生集合数'; exit 2; }
if DOC=docs/definitely-missing-sp3-design.md specs/sp3/check.sh >/dev/null 2>&1; then
 echo '✗ 目标件缺失被当通过'; exit 1
else rc=$?; [ "$rc" -eq 2 ] || { echo "✗ 缺目标件应 rc=2，实际 $rc"; exit 1; }; fi
REVIEW_SELFTEST=$(specs/sp3/review-aggregate.sh --selftest) || { echo '✗ SP3 review aggregate 反测失败'; exit 1; }
REVIEW_N=$(printf '%s\n' "$REVIEW_SELFTEST" | sed -n 's/^--- SP3 review aggregate selftest \([0-9][0-9]*\) cases PASS$/\1/p')
[ -n "$REVIEW_N" ] || { echo '✗ SP3 review aggregate 未报告实算反测数'; exit 2; }
[ "$REVIEW_N" -eq 25 ] || { echo "✗ SP3 review aggregate 反测数漂移：期望25，实得$REVIEW_N"; exit 1; }
echo "预期集合：$COUNTS；selftest=$N；缺件=rc2"
echo "实检集合：六面6/D12/AC12/KNO接口4/新协议14键/票据聚合反测${REVIEW_N}/前置4"
echo '未知集合：0'
echo '✓ SP3 设计合同与冻结输入通过'
