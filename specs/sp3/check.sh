#!/bin/sh
# SP3 设计覆盖检查：预期集合从冻结锚派生；正文缺失/块缺失/预期空/输入漂移均 exit 2。
set -u
BASE_ANCHOR=${BASE_ANCHOR:-53e4a2aed1569eef91e4048e7da2a1cd32db3ec2}
SPECDOC_ANCHOR=${SPECDOC_ANCHOR:-fcfec9a}
DOC=${DOC:-docs/dingwei-sp3-design.md}
MODE=${1:-check}
python3 - "$BASE_ANCHOR" "$SPECDOC_ANCHOR" "$DOC" "$MODE" <<'PY'
import hashlib,re,subprocess,sys,os
base_anchor,spec_anchor,doc,mode=sys.argv[1:]
PATHS={
 'docs/dingwei-platform-specdoc.md':(spec_anchor,'bb4cf7becda882e3bafe35b3c293e0577783c4880b756e013ef861b088d8dcaa'),
 'docs/dingwei-platform-architecture.md':(base_anchor,'0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc'),
 'docs/dingwei-prototype.html':(base_anchor,'eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8'),
 'docs/dingwei-sp0-design.md':(base_anchor,'f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec'),
 'docs/dingwei-sp1-design.md':(base_anchor,'8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf'),
 'docs/dingwei-sp2-design.md':(base_anchor,'37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86'),
}
def get(path):
    anchor,want=PATHS[path]
    try: b=subprocess.check_output(['git','show',f'{anchor}:{path}'],stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError: print(f'✗ 冻结输入不可取：{anchor}:{path}'); raise SystemExit(2)
    if hashlib.sha256(b).hexdigest()!=want: print(f'✗ 冻结输入漂移：{anchor}:{path}'); raise SystemExit(2)
    return b.decode()
spec=get('docs/dingwei-platform-specdoc.md'); arch=get('docs/dingwei-platform-architecture.md'); proto=get('docs/dingwei-prototype.html')
get('docs/dingwei-sp0-design.md'); get('docs/dingwei-sp1-design.md'); get('docs/dingwei-sp2-design.md')
try: contract=open('specs/sp3/spec.md',encoding='utf8').read()
except OSError: print('✗ SP3 合同不可取，无法确认 event 空面声明'); raise SystemExit(2)

def derive_interfaces(text):
    out=set(); on=False
    for l in text.splitlines():
        if re.match(r'^## 6\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on: continue
        m=re.match(r'^\| `(KNO-(?:CMD|QRY|CAP)-[^`]+)` \| KNO \|',l)
        if m: out.add('IF:'+m.group(1))
    return out

def expected_sets():
    src=[]; on=False
    for l in spec.splitlines():
        if re.match(r'^## §10\b',l): on=True
        if re.match(r'^## §14\b',l): on=False
        if on:
            m=re.match(r'^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b',l)
            if m: src.append('SRC:§'+m.group(1))
    e=[]; sec=''
    for l in spec.splitlines():
        m=re.match(r'^##\s+(E\.[134])\b',l)
        if m: sec=m.group(1)
        elif re.match(r'^##\s+E\.',l): sec=''
        if sec and l.startswith('|'):
            c=[re.sub(r'[*`]','',x.strip()) for x in l.strip().strip('|').split('|')]
            if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP3' in c[-1].split('·'): e.append(f'E:{sec}#{c[0]}')
    ui=[]; on=False
    for l in arch.splitlines():
        if re.match(r'^## 13\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on or not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)>=2 and 'KNO' in c[1].split('/'):
            for group in re.findall(r'`([^`]+)`',c[0]): ui.extend('UI:#'+x for x in group.split('/'))
    proto_ids=set(re.findall(r'<section\s+class="scr"\s+id="([^"]+)"',proto))
    absent=sorted(x for x in ui if x[4:] not in proto_ids)
    if absent: print('✗ 架构 §13 KNO 入口在原型不存在：'+','.join(absent)); raise SystemExit(2)
    kno_events=re.findall(r'^\| `(EV-KNO-[^`]+)` \| KNO \|',arch,re.M)
    declared='<!-- EVENT-SET-SP3: NOT_APPLICABLE -->' in contract
    if kno_events or not declared:
        print(f'✗ KNO event 空面契约不成立：declared={declared} actual={len(kno_events)}'); raise SystemExit(2)
    out={'spec':set(src),'E':set(e),'interface':derive_interfaces(arch),'prototype':set(ui)}
    if any(not v for v in out.values()): print('✗ 预期集合为空：'+','.join(k for k,v in out.items() if not v)); raise SystemExit(2)
    return out
expected=expected_sets()

# ============ AC-SP3-08：前序证据口径的三方对撞（round3 闭合）============
# round2 判 NO：合同称「经 envelope_ref.prior_state_evidence_ref 装载」，
# 而该字段在冻结 SpecDoc 全文命中 0（未定义），正文 §11.1 映射表登记为「待上游确认、未确认前只用 fixture」。
# ⇒ 把「设计阶段的 fixture 消费」写成了「已完成真实装载」。
#
# 这里**不做关键字 grep**：三个来源彼此独立——
#   ① 冻结 SpecDoc（上游是否已定义该字段，来自锚，本件改不动）
#   ② 本合同 specs/sp3/spec.md（口径是否已收准为 fixture 消费 + 挂账）
#   ③ 正文 docs/dingwei-sp3-design.md（是否仍登记「未定义、待上游确认」）
# 任一与另两者不一致即红，且**四种不一致各有可分辨标签**。
# 关键在于它是**双向**的：上游哪天真定义了该字段，②③ 还写着挂账 ⇒ 同样红，
# 逼合同回来复核，而不是让一句陈旧的「挂账」永远绿着。
def prior_state_gate(spec_text, contract_text, doc_text, quiet=False):
    field='prior_state_evidence_ref'
    defined   = field in spec_text                      # ① 上游已定义？
    narrowed  = '仅按 fixture 消费合同判定' in contract_text   # ② 合同已收准？
    liability = '真实装载列为集成前置挂账' in contract_text
    sec11_1 = re.search(r'^### 11\.1\b(.*?)(?=^### |^## |\Z)', doc_text, re.M|re.S)
    mapping = sec11_1.group(1) if sec11_1 else ''
    doc_pending = ('SP2 与 SpecDoc §11.6 均未定义' in mapping
                   and '未确认前只用 fixture' in mapping)       # ③ §11.1 映射表仍挂账？
    bad=[]
    if defined:
        # 上游已定义：合同/正文不得再以「未定义、挂账」为由停在 fixture 口径
        if narrowed or liability or doc_pending:
            bad.append('[E-AC08-STALE-LIABILITY] 冻结 SpecDoc 已定义 %s，'
                       '但合同/正文仍停在「未定义·挂账·仅 fixture」口径，须复核后收口' % field)
    else:
        if not narrowed:
            bad.append('[E-AC08-OVERCLAIM] %s 在冻结 SpecDoc 未定义，'
                       '合同却未把它收准为「仅按 fixture 消费合同判定」' % field)
        if not liability:
            bad.append('[E-AC08-LIABILITY-MISSING] 合同未登记 %s 的「真实装载列为集成前置挂账」' % field)
        if not doc_pending:
            bad.append('[E-AC08-DOC-LIABILITY-GONE] 正文未再登记 %s「SpecDoc 未定义·未确认前只用 fixture」，'
                       '与合同口径脱节' % field)
    if not quiet:
        print('[prior-state] upstream-defined=%s contract-narrowed=%s contract-liability=%s doc-pending=%s'
              % (defined, narrowed, liability, doc_pending))
        for m in bad: print('✗ '+m)
    return 1 if bad else 0

def inspect(text,quiet=False):
    m=re.search(r'<!-- COVERAGE-SP3-BEGIN -->(.*?)<!-- COVERAGE-SP3-END -->',text,re.S)
    if not m:
        if not quiet: print('✗ COVERAGE-SP3 结构化块缺失')
        return 2
    actual=set()
    for l in m.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=2: continue
        token=c[0].lstrip('`').split('`',1)[0]
        if re.search(r'\bD-SP3-\d{2}\b',c[1]): actual.add(token)
    bad=False
    for k,want in expected.items():
        got=actual & want; unknown=sorted(want-got); bad|=bool(unknown)
        if not quiet: print(f'[{k}] 预期={len(want)} 实检={len(got)} 未知={len(unknown)} {unknown}')
    ids=re.findall(r'^## (D-SP3-\d+)',text,re.M)
    miss=[f'D-SP3-{i:02d}' for i in range(1,13) if ids.count(f'D-SP3-{i:02d}')!=1]; bad|=bool(miss)
    am=re.search(r'<!-- ACCEPTANCE-SP3-BEGIN -->(.*?)<!-- ACCEPTANCE-SP3-END -->',text,re.S)
    if not am:
        if not quiet: print('✗ ACCEPTANCE-SP3 结构化块缺失')
        return 2
    ac={}; ac_counts={}
    for l in am.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=4: continue
        aid=c[0].strip('`')
        if re.fullmatch(r'AC-SP3-\d{2}',aid) and all(x and x not in ('—','见全文') for x in c[1:]):
            ac_counts[aid]=ac_counts.get(aid,0)+1; ac[aid]=c[1:]
    ac_miss=[f'AC-SP3-{i:02d}' for i in range(1,13) if f'AC-SP3-{i:02d}' not in ac]; bad|=bool(ac_miss)
    ac08=ac.get('AC-SP3-08',[])
    required=('仅按 fixture 消费合同判定','真实装载列为集成前置挂账','冻结 SpecDoc §11.6 未定义','§11.1 映射表')
    forbidden=('已完成真实装载','真实装载已完成','经该引用装载')
    ac08_bad=(ac_counts.get('AC-SP3-08',0)!=1 or len(ac08)!=3
              or not all(x in ac08[1] for x in required) or any(x in ac08[1] for x in forbidden))
    bad|=ac08_bad
    if not quiet:
        print(f'[delivery] 预期=12 实检={12-len(miss)} 未知={len(miss)} {miss}')
        print(f'[acceptance] 预期=12 实检={12-len(ac_miss)} 未知={len(ac_miss)} {ac_miss}')
        if ac08_bad: print('✗ [E-AC08-DOC-ROW-NONCANON] AC-SP3-08 正文验收行须恰一且判据单元格合受控形态')
        print('受检范围：冻结 SpecDoc §10–§13 heading、owner_sp=SP3 E 行、架构 §6 KNO 接口、§13 KNO 原型入口；event 面显式不适用且实算为 0。')
        print('能力边界：判结构化来源→D-SP3 落点与 ID 形状；判不了语义正确性、授权充分性或实现状态。')
    return 1 if bad else 0

def fixture():
    rows=[f'| `{x}` | `D-SP3-12` |' for g in expected.values() for x in sorted(g)]
    heads='\n'.join(f'## D-SP3-{i:02d} fixture' for i in range(1,13))
    ac='\n'.join((f'| `AC-SP3-{i:02d}` | fixture scope | '
                  + ('execution fixture；仅按 fixture 消费合同判定；冻结 SpecDoc §11.6 未定义；真实装载列为集成前置挂账（见 §11.1 映射表）' if i==8 else 'fixture criterion')
                  + ' | fixture boundary |') for i in range(1,13))
    return heads+'\n<!-- COVERAGE-SP3-BEGIN -->\n| 来源 | 设计落点 |\n|---|---|\n'+'\n'.join(rows)+\
      '\n<!-- COVERAGE-SP3-END -->\n<!-- ACCEPTANCE-SP3-BEGIN -->\n| ID | 受检范围 | 判据 | 判不了什么 |\n|---|---|---|---|\n'+ac+'\n<!-- ACCEPTANCE-SP3-END -->\n'
if mode=='--selftest':
    base=fixture(); n=0; fail=0
    def test(name,text,want):
        global n,fail; n+=1; got=inspect(text,True)
        if got==want: print('  ✓ '+name)
        else: print(f'  ✗ {name}: want={want} got={got}'); fail=1
    test('正常结构化覆盖放行',base,0)
    for kind in expected:
        token=sorted(expected[kind])[0]
        changed='\n'.join(l for l in base.splitlines() if not l.startswith(f'| `{token}` |'))+'\n'
        test(f'删除 {kind} 真落点必须转红',changed,1)
        test(f'{kind} 块外散文提及不得转绿',changed+f'正文提及 {token} 已考虑。\n',1)
    noisy='| `RT-04` | KNO | 非接口 |\n| `EV-WRK-NodeDispatched` | KNO | 非接口 |\n'+arch
    n+=1
    if derive_interfaces(noisy)==expected['interface']: print('  ✓ §6 外 KNO/事件行不得污染接口集合')
    else: print('  ✗ 跨表行污染 KNO 接口集合'); fail=1
    test('缺覆盖块判闸坏',base.replace('<!-- COVERAGE-SP3-BEGIN -->',''),2)
    ac08line=next(l for l in base.splitlines() if l.startswith('| `AC-SP3-08` |'))
    test('AC08 正文验收行删除必须转红',base.replace(ac08line+'\n',''),1)
    test('AC08 正文验收行重复必须转红',base.replace(ac08line,ac08line+'\n'+ac08line),1)
    test('AC08 正文验收单元格伪造必须转红',base.replace('仅按 fixture 消费合同判定','已完成真实装载',1),1)
    # ---- AC-SP3-08 三方对撞的分辨力反测（结构反测，非关键字 grep）----
    real_doc=open(doc,encoding='utf8').read() if os.path.isfile(doc) else ''
    def pcell(name,sp,ct,dc,want_label):
        global n,fail; n+=1
        import io as _io, contextlib as _cl
        buf=_io.StringIO()
        with _cl.redirect_stdout(buf): rc=prior_state_gate(sp,ct,dc)
        out=buf.getvalue()
        if want_label is None:
            if rc==0: print('  ✓ '+name)
            else: print(f'  ✗ {name}: 期望绿，实得 rc={rc}\n{out}'); fail=1
        elif rc!=0 and want_label in out: print(f'  ✓ {name} 命中 {want_label}')
        else: print(f'  ✗ {name}: rc={rc} 未命中 {want_label}，实得：\n{out}'); fail=1
    pcell('[前置] 真实三方一致必须绿', spec, contract, real_doc, None)
    pcell('合同去掉 fixture 收准语', spec, contract.replace('仅按 fixture 消费合同判定','经该引用装载'), real_doc, '[E-AC08-OVERCLAIM]')
    pcell('合同去掉挂账登记',       spec, contract.replace('真实装载列为集成前置挂账','真实装载已完成'), real_doc, '[E-AC08-LIABILITY-MISSING]')
    pcell('正文撤掉待确认登记',     spec, contract, real_doc.replace('SP2 与 SpecDoc §11.6 均未定义','SP2 与 SpecDoc §11.6 已定义'), '[E-AC08-DOC-LIABILITY-GONE]')
    pcell('上游改为已定义（挂账变陈）', spec+'\nprior_state_evidence_ref 字段定义\n', contract, real_doc, '[E-AC08-STALE-LIABILITY]')
    print('--- SP3 预期集合实算 '+ ' '.join(f'{k}={len(v)}' for k,v in expected.items()))
    print(f'--- SP3 覆盖反测 {n} 项全过' if not fail else '--- SP3 覆盖反测失败')
    raise SystemExit(fail)
if not os.path.isfile(doc): print(f'✗ 目标件缺失：{doc}'); raise SystemExit(2)
doc_text=open(doc,encoding='utf8').read()
rc_inspect=inspect(doc_text)
rc_prior=prior_state_gate(spec,contract,doc_text)
raise SystemExit(2 if 2 in (rc_inspect,) else max(rc_inspect,rc_prior))
PY
