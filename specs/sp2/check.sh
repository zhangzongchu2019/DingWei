#!/bin/sh
# SP2 设计覆盖检查：只认 COVERAGE-SP2 块内两列表格；正文缺失/块缺失/预期空均 exit 2。
set -u
ANCHOR=${ANCHOR:-53d91aedd158c0a8618f0196f013cfee5cfd44aa}
DOC=${DOC:-docs/dingwei-sp2-design.md}
MODE=${1:-check}
python3 - "$ANCHOR" "$DOC" "$MODE" <<'PY'
import hashlib,re,subprocess,sys,os
anchor,doc,mode=sys.argv[1:]
PATHS={
 'docs/dingwei-platform-specdoc.md':'8b0b20b97af85bf1c42c1b8dd5a7c94a0e73d1de3100666c5acb2370ae56c61e',
 'docs/dingwei-platform-architecture.md':'0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc',
 'docs/dingwei-prototype.html':'eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8',
 'docs/dingwei-sp0-design.md':'f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec',
 'docs/dingwei-sp1-design.md':'8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf',
}
def get(path):
    try: b=subprocess.check_output(['git','show',f'{anchor}:{path}'],stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError: print(f'✗ 冻结输入不可取：{anchor}:{path}'); raise SystemExit(2)
    got=hashlib.sha256(b).hexdigest()
    if got!=PATHS[path]: print(f'✗ 冻结输入漂移：{path}'); raise SystemExit(2)
    return b.decode()
spec=get('docs/dingwei-platform-specdoc.md'); arch=get('docs/dingwei-platform-architecture.md'); proto=get('docs/dingwei-prototype.html')
get('docs/dingwei-sp0-design.md'); get('docs/dingwei-sp1-design.md')

def derive_interfaces(text):
    out=set(); in_s6=False
    for l in text.splitlines():
        if re.match(r'^## 6\.',l): in_s6=True; continue
        if in_s6 and re.match(r'^## ',l): in_s6=False
        if not in_s6: continue
        m=re.match(r'^\| `(WRK-(?:CMD|QRY|CAP)-[^`]+)` \| WRK \|',l)
        if m: out.add(m.group(1))
    return out

def expected_sets():
    src=[]; on=False
    for l in spec.splitlines():
        if re.match(r'^## §5\b',l): on=True
        if re.match(r'^## §10\b',l): on=False
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
            if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP2' in c[-1].split('·'): e.append(f'E:{sec}#{c[0]}')
    iface=['IF:'+x for x in derive_interfaces(arch)]
    ui=[]; in_s13=False
    for l in arch.splitlines():
        if re.match(r'^## 13\.',l): in_s13=True; continue
        if in_s13 and re.match(r'^## ',l): in_s13=False
        if not in_s13 or not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)>=2 and 'WRK' in c[1].split('/'):
            for group in re.findall(r'`([^`]+)`',c[0]):
                ui.extend('UI:#'+x for x in group.split('/'))
    proto_ids=set(re.findall(r'<section\s+class="scr"\s+id="([^"]+)"',proto))
    absent=sorted(x for x in ui if x[4:] not in proto_ids)
    if absent: print('✗ 架构 §13 WRK 入口在原型不存在：'+','.join(absent)); raise SystemExit(2)
    f=[]; f12=re.search(r'^### F\.1\.2\b',spec,re.M); f13=re.search(r'^### F\.1\.3\b',spec,re.M)
    if f12: f.append('F:§F.1.2')
    if f13: f.append('F:§F.1.3')
    in_f2=False
    for l in spec.splitlines():
        if re.match(r'^## F\.2\b',l): in_f2=True; continue
        if in_f2 and re.match(r'^## ',l): in_f2=False
        if in_f2 and l.startswith('|'):
            c=[x.strip() for x in l.strip().strip('|').split('|')]
            if c and c[0]=='4': f.append('F:F.2#4')
    out={'spec':set(src),'E':set(e),'interface':set(iface),'prototype':set(ui),'F':set(f)}
    if any(not v for v in out.values()): print('✗ 预期集合为空：'+','.join(k for k,v in out.items() if not v)); raise SystemExit(2)
    return out
expected=expected_sets()

def inspect(text,quiet=False):
    m=re.search(r'<!-- COVERAGE-SP2-BEGIN -->(.*?)<!-- COVERAGE-SP2-END -->',text,re.S)
    if not m:
        if not quiet: print('✗ COVERAGE-SP2 结构化块缺失')
        return 2
    actual=set()
    for l in m.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=2: continue
        token=c[0].lstrip('`').split('`',1)[0]
        if re.search(r'\bD-SP2-\d{2}\b',c[1]): actual.add(token)
    bad=False
    for k,want in expected.items():
        got=actual & want; unknown=sorted(want-got); bad|=bool(unknown)
        if not quiet: print(f'[{k}] 预期={len(want)} 实检={len(got)} 未知={len(unknown)} {unknown}')
    ids=re.findall(r'^## (D-SP2-\d+)',text,re.M); miss=[f'D-SP2-{i:02d}' for i in range(1,13) if ids.count(f'D-SP2-{i:02d}')!=1]
    bad|=bool(miss)
    am=re.search(r'<!-- ACCEPTANCE-SP2-BEGIN -->(.*?)<!-- ACCEPTANCE-SP2-END -->',text,re.S)
    if not am:
        if not quiet: print('✗ ACCEPTANCE-SP2 结构化块缺失')
        return 2
    ac={}
    for l in am.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=4: continue
        aid=c[0].strip('`')
        if re.fullmatch(r'AC-SP2-\d{2}',aid) and all(x and x not in ('—','见全文') for x in c[1:]): ac[aid]=c[1:]
    ac_miss=[f'AC-SP2-{i:02d}' for i in range(1,13) if f'AC-SP2-{i:02d}' not in ac]
    bad|=bool(ac_miss)
    if not quiet:
        print(f'[delivery] 预期=12 实检={12-len(miss)} 未知={len(miss)} {miss}')
        print(f'[acceptance] 预期=12 实检={12-len(ac_miss)} 未知={len(ac_miss)} {ac_miss}')
        print('受检范围：冻结 SpecDoc §5–§9 heading/owner_sp=SP2 E 行/F.1.2/F.1.3/F.2#4、目标架构 WRK 接口、架构 §13 WRK 原型入口。')
        print('能力边界：只判结构化来源→D-SP2 落点与 ID 形状；判不了映射语义、接口充分性或实现状态。')
    return 1 if bad else 0

def fixture():
    rows=[f'| `{x}` | `D-SP2-12` |' for g in expected.values() for x in sorted(g)]
    heads='\n'.join(f'## D-SP2-{i:02d} fixture' for i in range(1,13))
    ac='\n'.join(f'| `AC-SP2-{i:02d}` | fixture scope | fixture criterion | fixture boundary |' for i in range(1,13))
    return heads+'\n<!-- COVERAGE-SP2-BEGIN -->\n| 来源 | 设计落点 |\n|---|---|\n'+'\n'.join(rows)+\
      '\n<!-- COVERAGE-SP2-END -->\n<!-- ACCEPTANCE-SP2-BEGIN -->\n| ID | 受检范围 | 判据 | 判不了什么 |\n|---|---|---|---|\n'+ac+'\n<!-- ACCEPTANCE-SP2-END -->\n'
if mode=='--selftest':
    base=fixture(); first=sorted(expected['spec'])[0]; n=0; fail=0
    def test(name,text,want):
        global n,fail; n+=1; got=inspect(text,True)
        if got==want: print('  ✓ '+name)
        else: print(f'  ✗ {name}: want={want} got={got}'); fail=1
    test('正常结构化覆盖放行',base,0)
    missing='\n'.join(l for l in base.splitlines() if not l.startswith(f'| `{first}` |'))+'\n'
    test('删除真落点必须转红',missing,1)
    test('块外散文追加同 token 仍红',missing+f'正文提及 {first} 已考虑。\n',1)
    for kind in ('prototype','F'):
        token=sorted(expected[kind])[0]
        changed='\n'.join(l for l in base.splitlines() if not l.startswith(f'| `{token}` |'))+'\n'
        test(f'删除 {kind} 真落点必须转红',changed,1)
        test(f'{kind} 块外散文提及不得转绿',changed+f'正文提及 {token} 已考虑。\n',1)
    n+=1
    noisy='| `RT-03` | WRK | 非接口 |\n| `EV-WRK-NodeDispatched` | WRK | 非接口 |\n'+arch
    if derive_interfaces(noisy)=={x[3:] for x in expected['interface']}:
        print('  ✓ §6 外 WRK 责任/事件行不得混入接口集合')
    else:
        print('  ✗ §6 外 WRK 责任/事件行污染接口集合'); fail=1
    test('缺结构化块判闸坏',base.replace('<!-- COVERAGE-SP2-BEGIN -->',''),2)
    print(f'--- SP2 覆盖反测 {n} 项全过' if not fail else '--- SP2 覆盖反测失败')
    raise SystemExit(fail)
if not os.path.isfile(doc): print(f'✗ 目标件缺失：{doc}'); raise SystemExit(2)
raise SystemExit(inspect(open(doc,encoding='utf8').read()))
PY
