#!/bin/sh
# SP4 设计覆盖检查：预期集合从组合冻结锚派生；正文缺失/块缺失/预期空/输入漂移均 exit 2。
set -u
BASE_ANCHOR=${BASE_ANCHOR:-6a98629}
SPECDOC_ANCHOR=${SPECDOC_ANCHOR:-4220195}
SP8_ANCHOR=${SP8_ANCHOR:-970effe}
DOC=${DOC:-docs/dingwei-sp4-design.md}
MODE=${1:-check}
python3 - "$BASE_ANCHOR" "$SPECDOC_ANCHOR" "$SP8_ANCHOR" "$DOC" "$MODE" <<'PY'
import hashlib,re,subprocess,sys,os
base_anchor,spec_anchor,sp8_anchor,doc,mode=sys.argv[1:]
PATHS={
 'docs/dingwei-platform-specdoc.md':(spec_anchor,'66df042ec49e6c7345e5c5cdbe1cf4e7f5c3bbdc94a1c671bb35c588532e8e0f'),
 'docs/dingwei-platform-architecture.md':(base_anchor,'0a2288c7b9e245eded06944cd063a6d0d458c6b1f45a3a9534d29817b6c58fdc'),
 'docs/dingwei-prototype.html':(base_anchor,'eeb71a5a77a53990569e08f9745165903bcf9e34cf6ce0a5047824167e1df4b8'),
 'docs/dingwei-sp0-design.md':(base_anchor,'f635b96e6c3574319d2e8df7e6a1c41e9693a66d7581504df60a6801531b8bec'),
 'docs/dingwei-sp1-design.md':(base_anchor,'8fafe6c5bac5595eaa771543dfc4199ba3670081786f1769f39833dc616e27bf'),
 'docs/dingwei-sp2-design.md':(base_anchor,'37d2c17cc0761914aae91abd20273746231d894c50163cb89b8123cc03d76a86'),
 'docs/dingwei-sp3-design.md':(base_anchor,'e6317b5c7257eb86b405d355b3bbcdb72751eb8c677fa2f388891c1d307b1e97'),
 'docs/dingwei-sp8-design.md':(sp8_anchor,'4f5e4e06d29d4a9fffcdb46f413fbc37bcfd615e9af9b04215e0a6c20205a98a'),
}
def get(path):
    anchor,want=PATHS[path]
    try: b=subprocess.check_output(['git','show',f'{anchor}:{path}'],stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError: print(f'✗ 冻结输入不可取：{anchor}:{path}'); raise SystemExit(2)
    if hashlib.sha256(b).hexdigest()!=want: print(f'✗ 冻结输入漂移：{anchor}:{path}'); raise SystemExit(2)
    return b.decode()
spec=get('docs/dingwei-platform-specdoc.md'); arch=get('docs/dingwei-platform-architecture.md'); proto=get('docs/dingwei-prototype.html')
for p in ('docs/dingwei-sp0-design.md','docs/dingwei-sp1-design.md','docs/dingwei-sp2-design.md','docs/dingwei-sp3-design.md','docs/dingwei-sp8-design.md'): get(p)

class Boundary(Exception): pass

def slice_lines(text,start,stop,label,keep_start=False):
    # 🔴 通用不变量①：起界与止界都必须实证命中，任一未命中即 Boundary。
    # 不得靠"结果非 0"兜底——止界不命中时切片会一路取到 EOF，分母仍非 0 却已不可信。
    out=[]; on=False; start_seen=False; stop_seen=False
    for l in text.splitlines():
        if not on and re.match(start,l):
            on=True; start_seen=True
            if keep_start: out.append(l)
            continue
        if on and re.match(stop,l): stop_seen=True; break
        if on: out.append(l)
    if not start_seen: raise Boundary(f'起界未命中：{label}')
    if not stop_seen:  raise Boundary(f'止界未命中：{label}（切片取到 EOF，分母不可信）')
    return out

def derive_interfaces(text):
    return {'IF:'+m.group(1) for l in slice_lines(text,r'^## 6\.(?![0-9A-Za-z])',r'^## ','架构 ^## 6. … 下一个 ^##')
            for m in [re.match(r'^\| `(CAP-(?:CMD|QRY|CAP)-[^`]+)` \| CAP \|',l)] if m}

def derive_events(text):
    # N4 同族：收窄到 §4.2（§0.2 与 AC-SP4-04 都称 §4.2），与 xref-check 的派生器同口径
    return {'EVENT:'+m.group(1) for l in slice_lines(text,r'^### 4\.2(?![0-9A-Za-z])',r'^## ','架构 ^### 4.2 … 下一个 ^##')
            for m in [re.match(r'^\| `(EV-CAP-[^`]+)` \| CAP \|',l)] if m}

def derive_E(text):
    # BX：E 行扫描原为裸循环（^## E.[134] 起、下一个 ^## E. 复位），无两端强校 ——
    # 把 ## E.5 降级成 ### E.5 再于文末追加一行 SP4 表行，集合会由 45 涨到 46 而不报错。
    out=[]; sec=''
    for l in slice_lines(text,r'^## E\.1(?![0-9])',r'^## E\.5(?![0-9])','SpecDoc ^## E.1 … ^## E.5',keep_start=True):
        m=re.match(r'^##\s+(E\.[134])\b',l)
        if m: sec=m.group(1); continue
        if re.match(r'^##\s+E\.',l): sec=''; continue
        if sec and l.startswith('|'):
            c=[re.sub(r'[*`]','',x.strip()) for x in l.strip().strip('|').split('|')]
            if c and re.fullmatch(r'\d+[a-z]?',c[0]) and 'SP4' in c[-1].split('·'): out.append(f'E:{sec}#{c[0]}')
    return out

def derive_prototype(text):
    # BX：原型入口扫描原为裸循环（^## 13. 起、泛化 ^## 复位）——把 §13 设为末节并追加伪行，集合 1→2 不报错。
    out=[]
    for l in slice_lines(text,r'^## 13\.(?![0-9])',r'^## 14\.(?![0-9])','架构 ^## 13. … ^## 14.'):
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)>=2 and 'CAP' in c[1].split('/'):
            for g in re.findall(r'`([^`]+)`',c[0]): out.extend('UI:#'+x for x in g.split('/'))
    return out

def derive_src(text,start=r'^## §14\b',stop=r'^## §17\b',label='SpecDoc ^## §14 … ^## §17'):
    # 🔴 单区间取：§14 起 §17 止。SpecDoc 排版异常使 §15.4/§15.5 物理落在 ## §16 之内，
    # 按章分段（§14 段+§15 段+§16 段）拼接会静悄悄丢掉这两节，故此处只允许单区间。
    out=[]
    for l in slice_lines(text,start,stop,label,keep_start=True):
        m=re.match(r'^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b',l)
        if m: out.append('SRC:§'+m.group(1))
    return out

def recusal_scan(text,label):
    """B1：全量扫描（仅排除 RECUSAL 声明区）。引用与逐条标记必须成对出现。"""
    body=re.sub(r'<!-- RECUSAL-DECL-BEGIN -->.*?<!-- RECUSAL-DECL-END -->','',text,flags=re.S)
    refs=re.findall(r'§11\.6[ab]',body)
    marks=re.findall(r'<!-- RECUSAL-SP4-REF: (AC-SP4-\d{2}) -->',body)
    if refs and not marks: return f'✗ {label}引用 §11.6a/b {len(refs)} 处却无 RECUSAL-SP4-REF 标记（opus50 回避范围不明）'
    if marks and not refs: return f'✗ {label}标了 RECUSAL-SP4-REF 却无实际引用，标记不得空挂'
    return ''

def interval_derivers():
    # 🔴 BX：「派生器个数」不再手写。本清单即分母来源，selftest 逐个跑两端反测并断言覆盖齐全；
    # 新增派生器若不登记进来，两端覆盖断言与 §0.2b 台账对撞都会立刻转红。
    return [
     ('check:specdoc-14-17', lambda t: derive_src(t), lambda: spec,
      lambda t: t.replace('## §14','## §14X'),
      lambda t: '\n'.join(l for l in t.splitlines() if not re.match(r'^## §17\b',l))),
     ('check:specdoc-E1-E5', derive_E, lambda: spec,
      lambda t: t.replace('## E.1','## E1.'),
      lambda t: t.replace('## E.5','### E.5')),
     ('check:arch-6', derive_interfaces, lambda: arch,
      lambda t: t.replace('## 6.','## 6X.'),
      lambda t: t.split('\n## 7.')[0]),
     ('check:arch-4.2', derive_events, lambda: arch,
      lambda t: t.replace('### 4.2','### 4.2X'),
      lambda t: t.split('\n## 5.')[0]),
     ('check:arch-13-14', derive_prototype, lambda: arch,
      lambda t: t.replace('## 13.','## 13X.'),
      lambda t: t.replace('## 14.','### 14.')),
    ]

def expected_sets():
    try: src=derive_src(spec)
    except Boundary as e: print(f'✗ 冻结输入节界不可信：{e}'); raise SystemExit(2)
    for canary in ('SRC:§15.4','SRC:§15.5'):
        if canary not in src:
            print(f'✗ heading 取法丢节：{canary} 不在派生集合内（是否按章分段取？）'); raise SystemExit(2)
    try: e=derive_E(spec); ui=derive_prototype(arch)
    except Boundary as ex: print(f'✗ 冻结输入节界不可信：{ex}'); raise SystemExit(2)
    # P5：区间派生结果的列表长度必须等于集合大小，重复项不得静默折叠
    for name,lst in (('spec',src),('E',e),('prototype',ui)):
        if len(lst)!=len(set(lst)):
            from collections import Counter
            d=sorted(k for k,v in Counter(lst).items() if v>1)
            print(f'✗ {name} 派生结果含重复项（会静默折叠进集合）：{d}'); raise SystemExit(2)
    proto_ids=set(re.findall(r'<section\s+class="scr[^"]*"\s+id="([^"]+)"',proto))
    absent=sorted(x for x in ui if x[4:] not in proto_ids)
    if absent: print('✗ 架构 §13 CAP 入口在原型不存在：'+','.join(absent)); raise SystemExit(2)
    try: out={'spec':set(src),'E':set(e),'interface':derive_interfaces(arch),'event':derive_events(arch),'prototype':set(ui)}
    except Boundary as ex: print(f'✗ 冻结输入节界不可信：{ex}'); raise SystemExit(2)
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
    # 🔴 通用不变量②：集合精确相等——只判 want-got 会放过 actual 里多出的伪落点
    extra=sorted(actual-set().union(*expected.values()))
    if extra:
        bad=True
        if not quiet: print(f'[extra] COVERAGE 块含 {len(extra)} 个不属于任一派生集合的来源 token：{extra}')
    err=recusal_scan(text,'正文')
    if err:
        if not quiet: print(err)
        bad=True
    ids=re.findall(r'^## (D-SP4-\d+)',text,re.M)
    miss=[f'D-SP4-{i:02d}' for i in range(1,13) if ids.count(f'D-SP4-{i:02d}')!=1]; bad|=bool(miss)
    am=re.search(r'<!-- ACCEPTANCE-SP4-BEGIN -->(.*?)<!-- ACCEPTANCE-SP4-END -->',text,re.S)
    if not am:
        if not quiet: print('✗ ACCEPTANCE-SP4 结构化块缺失')
        return 2
    ac={}; ac_ids=[]
    for l in am.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip() for x in l.strip().strip('|').split('|')]
        if len(c)!=4: continue
        aid=c[0].strip('`')
        if not re.fullmatch(r'AC-SP4-\d{2}',aid): continue
        # 🔴 BZ：唯一性只数 ID、不看内容（照 D 行 ids.count(...)!=1 的做法）。
        # 统计若写在值过滤之后，重复行只要有一行是占位就既不入表也不计重复，
        # 「见全文」占位禁令即可靠再加一条重复行解除。
        ac_ids.append(aid)
        if all(x and x not in ('—','见全文') for x in c[1:]): ac[aid]=c[1:]
    ac_miss=[f'AC-SP4-{i:02d}' for i in range(1,13) if f'AC-SP4-{i:02d}' not in ac]; bad|=bool(ac_miss)
    ac_dup=sorted({a for a in ac_ids if ac_ids.count(a)>1}); bad|=bool(ac_dup)
    if ac_dup and not quiet: print(f'✗ ACCEPTANCE 块内 AC-ID 重复出现（不看先后、不看哪条更好）：{ac_dup}')
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
    trap=derive_src(spec,r'^## §15\b',r'^## §16\b','SpecDoc ^## §15 … ^## §16')
    if 'SRC:§15.4' not in trap and 'SRC:§15.5' not in trap:
        print('  ✓ 陷阱反测：按章分段取会丢 §15.4/§15.5，故只允许单区间取')
    else:
        print('  ✗ 陷阱反测失效：分段取未复现丢节现象，需重查 SpecDoc 排版'); fail=1
    n+=1
    ac_bad=base.replace('| `AC-SP4-07` | fixture scope | fixture criterion | fixture boundary |','| `AC-SP4-07` | 见全文 | 见全文 | 见全文 |')
    if inspect(ac_bad,True)==1: print('  ✓ AC 四列写「见全文」必须转红')
    else: print('  ✗ AC 占位值未被拦'); fail=1
    test('正文普通段引用 §11.6a 而无回避标记必须转红',base+'\n② 运行约束：本节依赖 §11.6a 的证据形状。\n',1)
    test('引用 §11.6b ＋ 逐条回避标记可放行',base+'\n本节依赖 §11.6b。\n<!-- RECUSAL-SP4-REF: AC-SP4-11 -->\n',0)
    test('空挂回避标记而无引用必须转红',base+'\n<!-- RECUSAL-SP4-REF: AC-SP4-11 -->\n',1)
    test('缺覆盖块判闸坏',base.replace('<!-- COVERAGE-SP4-BEGIN -->',''),2)
    # 🔴 三条通用不变量各自的常驻反测：不加固就必须能证明它会红
    tok=sorted(expected['spec'])[0]
    test('COVERAGE 含不属于任一派生集合的伪落点必须转红',
         base.replace('<!-- COVERAGE-SP4-END -->','| `SRC:§99.9` | `D-SP4-12` |\n<!-- COVERAGE-SP4-END -->'),1)
    for tag,dup in (('第二行占位「见全文」','| `AC-SP4-07` | 见全文 | 见全文 | 见全文 |'),
                    ('第二行四列全空','| `AC-SP4-07` |  |  |  |'),
                    ('占位行在前、完整行在后','| `AC-SP4-07` | 见全文 | 见全文 | 见全文 |')):
        one='| `AC-SP4-07` | fixture scope | fixture criterion | fixture boundary |'
        repl = (dup+'\n'+one) if tag.startswith('占位行在前') else (one+'\n'+dup)
        test(f'AC 重复行 · {tag} 必须转红',base.replace(one,repl),1)
    test('同一 AC 写两行（后者会静默覆盖前者）必须转红',
         base.replace('| `AC-SP4-07` | fixture scope | fixture criterion | fixture boundary |',
                      '| `AC-SP4-07` | fixture scope | fixture criterion | fixture boundary |\n'
                      '| `AC-SP4-07` | 另一套范围 | 另一套判据 | 另一套边界 |'),1)
    n+=1
    try:
        derive_src('\n'.join(l for l in spec.splitlines() if not re.match(r'^## §17\b',l)))
        print('  ✗ 止界不命中未抛错（切片取到 EOF，派生集合会由 28 撑到 114 而不报错）'); fail=1
    except Boundary: print('  ✓ 止界不命中必抛错（原缺陷：删 SpecDoc ## §17 后集合 28→114 静默通过）')
    n+=1
    try:
        derive_src(spec.replace('## §14','## §14X'))
        print('  ✗ 起界不命中未抛错'); fail=1
    except Boundary: print('  ✓ 起界不命中必抛错')
    # 🔴 通用判据：每个区间派生器起界／止界各一条反测，只报一端不算证明有牙
    covered=set()
    for name,fn,src_fn,mut_start,mut_stop in interval_derivers():
        base_text=src_fn()
        for end,mut in (('起界',mut_start),('止界',mut_stop)):
            n+=1
            try:
                fn(mut(base_text)); print(f'  ✗ {name} {end}不命中未抛错'); fail=1
            except Boundary: print(f'  ✓ {name} {end}不命中必抛错'); covered.add((name,end))
    want_cov={(nm,e) for nm,*_ in interval_derivers() for e in ('起界','止界')}
    n+=1
    if covered==want_cov: print(f'  ✓ 两端反测覆盖齐全：{len(interval_derivers())} 个区间派生器 × 2 端')
    else: print(f'  ✗ 两端反测覆盖不全，缺：{sorted(want_cov-covered)}'); fail=1
    # 🔴 直接断言集合数：证明未加固取法在止界失守时把 28 撑成 113 且 canary 仍在
    n+=1
    def unhardened_src(text,start=r'^## §14\b',stop=r'^## §17\b'):
        out=[];on=False
        for l in text.splitlines():
            if re.match(start,l): on=True
            if re.match(stop,l): on=False
            if on:
                m=re.match(r'^#{2,4}\s+(?:§)?(\d+(?:\.\d+[a-z]?)*)\b',l)
                if m: out.append('SRC:§'+m.group(1))
        return out
    no17='\n'.join(l for l in spec.splitlines() if not re.match(r'^## §17\b',l))
    raw=unhardened_src(no17)
    ok = len(set(unhardened_src(spec)))==28 and len(set(raw))==113 and len(raw)==114 \
         and 'SRC:§15.4' in raw and 'SRC:§15.5' in raw
    if ok: print('  ✓ 断言集合数：未加固取法删 ## §17 后 集合=113（列表=114，重复 SRC:§21.4）且 canary 仍在 ⇒ 原判据不报错')
    else:
        print(f'  ✗ 集合数断言不符：原样集合={len(set(unhardened_src(spec)))} 删止界后 集合={len(set(raw))} 列表={len(raw)}'); fail=1
    print('--- SP4 预期集合实算 '+' '.join(f'{k}={len(v)}' for k,v in expected.items()))
    from collections import Counter
    ec=Counter(x.split('#')[0][2:] for x in expected['E'])
    print('--- SP4 E 分项实算 '+' '.join(f'{k}={ec[k]}' for k in ('E.1','E.3','E.4')))
    print('--- SP4 区间派生器 '+str(len(interval_derivers()))+' 个：'+' '.join(nm for nm,*_ in interval_derivers()))
    print(f'--- SP4 覆盖反测 {n} 项全过' if not fail else '--- SP4 覆盖反测失败')
    raise SystemExit(fail)
if not os.path.isfile(doc): print(f'✗ 目标件缺失：{doc}'); raise SystemExit(2)
raise SystemExit(inspect(open(doc,encoding='utf8').read()))
PY
