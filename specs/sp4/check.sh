#!/bin/sh
# SP4 设计覆盖检查：预期集合从组合冻结锚派生；正文缺失/块缺失/预期空/输入漂移均 exit 2。
set -u
BASE_ANCHOR=${BASE_ANCHOR:-6a98629}
SPECDOC_ANCHOR=${SPECDOC_ANCHOR:-4220195}
DOC=${DOC:-docs/dingwei-sp4-design.md}
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
for p in ('docs/dingwei-sp0-design.md','docs/dingwei-sp1-design.md','docs/dingwei-sp2-design.md','docs/dingwei-sp3-design.md'): get(p)

def derive_interfaces(text):
    out=set(); on=False
    for l in text.splitlines():
        if re.match(r'^## 6\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on: continue
        m=re.match(r'^\| `(CAP-(?:CMD|QRY|CAP)-[^`]+)` \| CAP \|',l)
        if m: out.add('IF:'+m.group(1))
    return out

def derive_events(text):
    out=set(); on=False
    for l in text.splitlines():
        if re.match(r'^## 4\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on: continue
        m=re.match(r'^\| `(EV-CAP-[^`]+)` \| CAP \|',l)
        if m: out.add('EVENT:'+m.group(1))
    return out

def derive_src(text,start=r'^## §14\b',stop=r'^## §17\b'):
    # 🔴 单区间取：§14 起 §17 止。SpecDoc 排版异常使 §15.4/§15.5 物理落在 ## §16 之内，
    # 按章分段（§14 段+§15 段+§16 段）拼接会静悄悄丢掉这两节，故此处只允许单区间。
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
    for canary in ('SRC:§15.4','SRC:§15.5'):
        if canary not in src:
            print(f'✗ heading 取法丢节：{canary} 不在派生集合内（是否按章分段取？）'); raise SystemExit(2)
    e=[]; sec=''
    for l in spec.splitlines():
        m=re.match(r'^##\s+(E\.[134])\b',l)
        if m: sec=m.group(1)
        elif re.match(r'^##\s+E\.',l): sec=''
        if sec and l.startswith('|'):
            c=[re.sub(r'[*`]','',x.strip()) for x in l.strip().strip('|').split('|')]
            if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP4' in c[-1].split('·'): e.append(f'E:{sec}#{c[0]}')
    ui=[]; on=False
    for l in arch.splitlines():
        if re.match(r'^## 13\.',l): on=True; continue
        if on and re.match(r'^## ',l): on=False
        if not on or not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)>=2 and 'CAP' in c[1].split('/'):
            for group in re.findall(r'`([^`]+)`',c[0]): ui.extend('UI:#'+x for x in group.split('/'))
    proto_ids=set(re.findall(r'<section\s+class="scr"\s+id="([^"]+)"',proto))
    absent=sorted(x for x in ui if x[4:] not in proto_ids)
    if absent: print('✗ 架构 §13 CAP 入口在原型不存在：'+','.join(absent)); raise SystemExit(2)
    out={'spec':set(src),'E':set(e),'interface':derive_interfaces(arch),'event':derive_events(arch),'prototype':set(ui)}
    if any(not v for v in out.values()): print('✗ 预期集合为空：'+','.join(k for k,v in out.items() if not v)); raise SystemExit(2)
    return out
expected=expected_sets()

def inspect(text,quiet=False):
    m=re.search(r'<!-- COVERAGE-SP4-BEGIN -->(.*?)<!-- COVERAGE-SP4-END -->',text,re.S)
    if not m:
        if not quiet: print('✗ COVERAGE-SP4 结构化块缺失')
        return 2
    actual=set()
    for l in m.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=2: continue
        token=c[0].lstrip('`').split('`',1)[0]
        if re.search(r'\bD-SP4-\d{2}\b',c[1]): actual.add(token)
    bad=False
    for k,want in expected.items():
        got=actual & want; unknown=sorted(want-got); bad|=bool(unknown)
        if not quiet: print(f'[{k}] 预期={len(want)} 实检={len(got)} 未知={len(unknown)} {unknown}')
    ids=re.findall(r'^## (D-SP4-\d+)',text,re.M)
    miss=[f'D-SP4-{i:02d}' for i in range(1,13) if ids.count(f'D-SP4-{i:02d}')!=1]; bad|=bool(miss)
    am=re.search(r'<!-- ACCEPTANCE-SP4-BEGIN -->(.*?)<!-- ACCEPTANCE-SP4-END -->',text,re.S)
    if not am:
        if not quiet: print('✗ ACCEPTANCE-SP4 结构化块缺失')
        return 2
    ac={}
    for l in am.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=4: continue
        aid=c[0].strip('`')
        if re.fullmatch(r'AC-SP4-\d{2}',aid) and all(x and x not in ('—','见全文') for x in c[1:]): ac[aid]=c[1:]
    ac_miss=[f'AC-SP4-{i:02d}' for i in range(1,13) if f'AC-SP4-{i:02d}' not in ac]; bad|=bool(ac_miss)
    if not quiet:
        print(f'[delivery] 预期=12 实检={12-len(miss)} 未知={len(miss)} {miss}')
        print(f'[acceptance] 预期=12 实检={12-len(ac_miss)} 未知={len(ac_miss)} {ac_miss}')
        print('受检范围：冻结 SpecDoc §14–§16 heading、owner_sp 含 SP4 的 E 行、架构 §6 CAP 接口、§4.2 CAP 事件、§13 CAP 原型入口。')
        print('能力边界：只判结构化来源→D-SP4 落点与 ID 形状；判不了判型是否判对、批准是否恰当或实现状态。')
    return 1 if bad else 0

def fixture():
    rows=[f'| `{x}` | `D-SP4-12` |' for g in expected.values() for x in sorted(g)]
    heads='\n'.join(f'## D-SP4-{i:02d} fixture' for i in range(1,13))
    ac='\n'.join(f'| `AC-SP4-{i:02d}` | fixture scope | fixture criterion | fixture boundary |' for i in range(1,13))
    return heads+'\n<!-- COVERAGE-SP4-BEGIN -->\n| 来源 | 设计落点 |\n|---|---|\n'+'\n'.join(rows)+\
      '\n<!-- COVERAGE-SP4-END -->\n<!-- ACCEPTANCE-SP4-BEGIN -->\n| ID | 受检范围 | 判据 | 判不了什么 |\n|---|---|---|---|\n'+ac+'\n<!-- ACCEPTANCE-SP4-END -->\n'
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
    noisy='| `RT-05` | CAP | 非接口 |\n| `EV-WRK-NodeDispatched` | CAP | 非本域 |\n'+arch
    if derive_interfaces(noisy)==expected['interface'] and derive_events(noisy)==expected['event']:
        print('  ✓ §6/§4 外行与他域事件不得污染接口/事件集合')
    else:
        print('  ✗ 跨表行污染 CAP 接口或事件集合'); fail=1
    n+=1
    trap=derive_src(spec,r'^## §15\b',r'^## §16\b')
    if 'SRC:§15.4' not in trap and 'SRC:§15.5' not in trap:
        print('  ✓ 陷阱反测：按章分段取会丢 §15.4/§15.5，故只允许单区间取')
    else:
        print('  ✗ 陷阱反测失效：分段取未复现丢节现象，需重查 SpecDoc 排版'); fail=1
    n+=1
    ac_bad=base.replace('| `AC-SP4-07` | fixture scope | fixture criterion | fixture boundary |','| `AC-SP4-07` | 见全文 | 见全文 | 见全文 |')
    if inspect(ac_bad,True)==1: print('  ✓ AC 四列写「见全文」必须转红')
    else: print('  ✗ AC 占位值未被拦'); fail=1
    test('缺覆盖块判闸坏',base.replace('<!-- COVERAGE-SP4-BEGIN -->',''),2)
    print('--- SP4 预期集合实算 '+' '.join(f'{k}={len(v)}' for k,v in expected.items()))
    print(f'--- SP4 覆盖反测 {n} 项全过' if not fail else '--- SP4 覆盖反测失败')
    raise SystemExit(fail)
if not os.path.isfile(doc): print(f'✗ 目标件缺失：{doc}'); raise SystemExit(2)
raise SystemExit(inspect(open(doc,encoding='utf8').read()))
PY
