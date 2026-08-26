#!/bin/sh
# SP8 设计覆盖检查：预期集合从组合冻结锚派生；正文缺失/块缺失/预期空/输入漂移均 exit 2。
set -u
BASE_ANCHOR=${BASE_ANCHOR:-6a98629}
SPECDOC_ANCHOR=${SPECDOC_ANCHOR:-4220195}
DOC=${DOC:-docs/dingwei-sp8-design.md}
MODE=${1:-check}
python3 - "$BASE_ANCHOR" "$SPECDOC_ANCHOR" "$DOC" "$MODE" <<'PY'
import hashlib,re,subprocess,sys,os
base_anchor,spec_anchor,doc,mode=sys.argv[1:]
PATHS={
 'docs/dingwei-platform-specdoc.md':(spec_anchor,'66df042ec49e6c7345e5c5cdbe1cf4e7f5c3bbdc94a1c671bb35c588532e8e0f'),
 'docs/dingwei-platform-architecture.md':(base_anchor,'0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc'),
 'docs/dingwei-prototype.html':(base_anchor,'eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8'),
 'docs/dingwei-sp0-design.md':(base_anchor,'f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec'),
 'docs/dingwei-sp1-design.md':(base_anchor,'8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf'),
 'docs/dingwei-sp2-design.md':(base_anchor,'37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86'),
 'docs/dingwei-sp3-design.md':(base_anchor,'e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97'),
}
def get(path):
    anchor,want=PATHS[path]
    try: b=subprocess.check_output(['git','show',f'{anchor}:{path}'],stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError: print(f'✗ 冻结输入不可取：{anchor}:{path}'); raise SystemExit(2)
    if hashlib.sha256(b).hexdigest()!=want: print(f'✗ 冻结输入漂移：{anchor}:{path}'); raise SystemExit(2)
    return b.decode()
spec=get('docs/dingwei-platform-specdoc.md'); arch=get('docs/dingwei-platform-architecture.md'); proto=get('docs/dingwei-prototype.html')
sp0=get('docs/dingwei-sp0-design.md')
for p in ('docs/dingwei-sp1-design.md','docs/dingwei-sp2-design.md','docs/dingwei-sp3-design.md'): get(p)
try: contract=open('specs/sp8/spec.md',encoding='utf8').read()
except OSError: print('✗ SP8 合同不可取，无法确认 event 空面声明'); raise SystemExit(2)

def derive_interfaces(text):
    out=set(); on=False
    for l in text.splitlines():
        if re.match(r'^## 6\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on: continue
        m=re.match(r'^\| `(GOV-(?:CMD|QRY|CAP)-[^`]+)` \| GOV \|',l)
        if m: out.add('IF:'+m.group(1))
    return out

def count_gov_events(text):
    n=0; on=False
    for l in text.splitlines():
        if re.match(r'^## 4\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on: continue
        if re.match(r'^\| `EV-GOV-[^`]+` \| GOV \|',l): n+=1
    return n

def derive_src(text,start=r'^## §23\b',stop=r'^## §25\b'):
    # 单区间取：§23 起 §25 止。§23/§24 在冻结件中顺序正常，但仍以单区间取并加 canary，
    # 避免后续有人按章分段拼接（SP4 已在 §15.4/§15.5 上吃过这个亏）。
    out=[]; on=False
    for l in text.splitlines():
        if re.match(start,l): on=True
        if re.match(stop,l): on=False
        if on:
            m=re.match(r'^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b',l)
            if m: out.append('SRC:§'+m.group(1))
    return out

def expected_sets():
    src=derive_src(spec)
    for canary in ('SRC:§23','SRC:§24'):
        if canary not in src:
            print(f'✗ heading 取法丢节：{canary} 不在派生集合内'); raise SystemExit(2)
    e=[]; sec=''
    for l in spec.splitlines():
        m=re.match(r'^##\s+(E\.[134])\b',l)
        if m: sec=m.group(1)
        elif re.match(r'^##\s+E\.',l): sec=''
        if sec and l.startswith('|'):
            c=[re.sub(r'[*`]','',x.strip()) for x in l.strip().strip('|').split('|')]
            if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP8' in c[-1].split('·'): e.append(f'E:{sec}#{c[0]}')
    ui=[]; on=False
    for l in arch.splitlines():
        if re.match(r'^## 13\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on or not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)>=2 and 'GOV' in c[1].split('/'):
            for group in re.findall(r'`([^`]+)`',c[0]): ui.extend('UI:#'+x for x in group.split('/'))
    # 🔴 同域差集：GOV 由 SP0（定义层 §1–§4）与 SP8（治理面 §23–§24）共用，架构域列无法区分。
    # 原型入口 = 架构 §13 GOV 全集 − 已收口 SP0 正文 COVERAGE 已认领的入口。两个输入均带 sha256 校验。
    m0=re.search(r'COVERAGE-SP0-BEGIN -->(.*?)<!-- COVERAGE-SP0-END',sp0,re.S)
    if not m0: print('✗ 差集减数不可取：SP0 正文缺 COVERAGE 块'); raise SystemExit(2)
    claimed=set()
    for l in m0.group(1).splitlines():
        if not l.startswith('|') or 'D-SP0-' not in l: continue
        mm=re.match(r'#([a-z]\w*)',l.split('|')[1].strip().strip('`'))
        if mm: claimed.add('UI:#'+mm.group(1))
    if not claimed: print('✗ 差集减数为空：SP0 未认领任何原型入口，差集规则失去意义'); raise SystemExit(2)
    ui=[x for x in ui if x not in claimed]
    proto_ids=set(re.findall(r'<section\s+class="scr"\s+id="([^"]+)"',proto))
    absent=sorted(x for x in ui if x[4:] not in proto_ids)
    if absent: print('✗ 架构 §13 GOV 入口在原型不存在：'+','.join(absent)); raise SystemExit(2)
    ev_n=count_gov_events(arch)
    declared='<!-- EVENT-SET-SP8: NOT_APPLICABLE -->' in contract
    if ev_n or not declared:
        print(f'✗ GOV event 空面契约不成立：declared={declared} actual={ev_n}'); raise SystemExit(2)
    out={'spec':set(src),'E':set(e),'interface':derive_interfaces(arch),'prototype':set(ui)}
    if any(not v for v in out.values()): print('✗ 预期集合为空：'+','.join(k for k,v in out.items() if not v)); raise SystemExit(2)
    return out
SCOPE_LINE=('受检范围：冻结 SpecDoc §23–§24 heading（单区间取）、owner_sp 含 SP8 的 E 行、'
            '架构 §6 GOV 接口、§13 GOV 入口减 SP0 已认领的差集；GOV 事件面实算为 0，按不适用处理。')
expected=expected_sets()

def recusal_scan(text,label):
    """B1：全量扫描（仅排除 RECUSAL 声明区）。引用与逐条标记必须成对出现。"""
    body=re.sub(r'<!-- RECUSAL-DECL-BEGIN -->.*?<!-- RECUSAL-DECL-END -->','',text,flags=re.S)
    refs=re.findall(r'§11\.6[ab]',body)
    marks=re.findall(r'<!-- RECUSAL-SP8-REF: (AC-SP8-\d{2}) -->',body)
    if refs and not marks: return f'✗ {label}引用 §11.6a/b {len(refs)} 处却无 RECUSAL-SP8-REF 标记（opus50 回避范围不明）'
    if marks and not refs: return f'✗ {label}标了 RECUSAL-SP8-REF 却无实际引用，标记不得空挂'
    return ''

def inspect(text,quiet=False):
    m=re.search(r'<!-- COVERAGE-SP8-BEGIN -->(.*?)<!-- COVERAGE-SP8-END -->',text,re.S)
    if not m:
        if not quiet: print('✗ COVERAGE-SP8 结构化块缺失')
        return 2
    actual=set()
    for l in m.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=2: continue
        token=c[0].lstrip('`').split('`',1)[0]
        if re.search(r'\bD-SP8-\d{2}\b',c[1]): actual.add(token)
    bad=False
    for k,want in expected.items():
        got=actual & want; unknown=sorted(want-got); bad|=bool(unknown)
        if not quiet: print(f'[{k}] 预期={len(want)} 实检={len(got)} 未知={len(unknown)} {unknown}')
    err=recusal_scan(text,'正文')
    if err:
        if not quiet: print(err)
        bad=True
    ids=re.findall(r'^## (D-SP8-\d+)',text,re.M)
    miss=[f'D-SP8-{i:02d}' for i in range(1,13) if ids.count(f'D-SP8-{i:02d}')!=1]; bad|=bool(miss)
    am=re.search(r'<!-- ACCEPTANCE-SP8-BEGIN -->(.*?)<!-- ACCEPTANCE-SP8-END -->',text,re.S)
    if not am:
        if not quiet: print('✗ ACCEPTANCE-SP8 结构化块缺失')
        return 2
    ac={}
    for l in am.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=4: continue
        aid=c[0].strip('`')
        if re.fullmatch(r'AC-SP8-\d{2}',aid) and all(x and x not in ('—','见全文') for x in c[1:]): ac[aid]=c[1:]
    ac_miss=[f'AC-SP8-{i:02d}' for i in range(1,13) if f'AC-SP8-{i:02d}' not in ac]; bad|=bool(ac_miss)
    if not quiet:
        print(f'[delivery] 预期=12 实检={12-len(miss)} 未知={len(miss)} {miss}')
        print(f'[acceptance] 预期=12 实检={12-len(ac_miss)} 未知={len(ac_miss)} {ac_miss}')
        print(SCOPE_LINE)
        print('能力边界：只判结构化来源→D-SP8 落点与 ID 形状；判不了批准是否恰当、危险度评估是否准确或实现状态。')
    return 1 if bad else 0

def fixture():
    rows=[f'| `{x}` | `D-SP8-12` |' for g in expected.values() for x in sorted(g)]
    heads='\n'.join(f'## D-SP8-{i:02d} fixture' for i in range(1,13))
    ac='\n'.join(f'| `AC-SP8-{i:02d}` | fixture scope | fixture criterion | fixture boundary |' for i in range(1,13))
    return heads+'\n<!-- COVERAGE-SP8-BEGIN -->\n| 来源 | 设计落点 |\n|---|---|\n'+'\n'.join(rows)+\
      '\n<!-- COVERAGE-SP8-END -->\n<!-- ACCEPTANCE-SP8-BEGIN -->\n| ID | 受检范围 | 判据 | 判不了什么 |\n|---|---|---|---|\n'+ac+'\n<!-- ACCEPTANCE-SP8-END -->\n'
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
    n+=1
    noisy='| `RT-02` | GOV | 非接口 |\n| `GOV-CMD-Fake` | GOV | §6 外伪造 |\n'+arch
    if derive_interfaces(noisy)==expected['interface'] and count_gov_events(noisy)==0:
        print('  ✓ §6 外 GOV 行不得污染接口集合；GOV 事件面实算恒为 0')
    else:
        print('  ✗ 跨表行污染 GOV 接口集合或事件实算异常'); fail=1
    n+=1
    all_gov=set(); on=False
    for l in arch.splitlines():
        if re.match(r'^## 13\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on or not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)>=2 and 'GOV' in c[1].split('/'):
            for gr in re.findall(r'`([^`]+)`',c[0]): all_gov.update('UI:#'+x for x in gr.split('/'))
    if expected['prototype'] and expected['prototype'] < all_gov:
        print(f'  ✓ 差集反测：GOV 全集 {len(all_gov)} 项，扣 SP0 已认领后本件得 {len(expected["prototype"])} 项，无重复认领')
    else:
        print('  ✗ 差集反测失效：未真正扣除 SP0 已认领入口'); fail=1
    n+=1
    ac_bad=base.replace('| `AC-SP8-07` | fixture scope | fixture criterion | fixture boundary |','| `AC-SP8-07` | 见全文 | 见全文 | 见全文 |')
    if inspect(ac_bad,True)==1: print('  ✓ AC 四列写「见全文」必须转红')
    else: print('  ✗ AC 占位值未被拦'); fail=1
    test('正文普通段引用 §11.6a 而无回避标记必须转红',base+'\n② 运行约束：本节依赖 §11.6a 的证据形状。\n',1)
    test('引用 §11.6b ＋ 逐条回避标记可放行',base+'\n本节依赖 §11.6b。\n<!-- RECUSAL-SP8-REF: AC-SP8-11 -->\n',0)
    test('空挂回避标记而无引用必须转红',base+'\n<!-- RECUSAL-SP8-REF: AC-SP8-11 -->\n',1)
    test('缺覆盖块判闸坏',base.replace('<!-- COVERAGE-SP8-BEGIN -->',''),2)
    print('--- '+SCOPE_LINE)
    print('--- SP8 预期集合实算 '+' '.join(f'{k}={len(v)}' for k,v in expected.items()))
    print(f'--- SP8 覆盖反测 {n} 项全过' if not fail else '--- SP8 覆盖反测失败')
    raise SystemExit(fail)
if not os.path.isfile(doc): print(f'✗ 目标件缺失：{doc}'); raise SystemExit(2)
raise SystemExit(inspect(open(doc,encoding='utf8').read()))
PY
