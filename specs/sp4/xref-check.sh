#!/bin/sh
# SP4 跨件引用门 v2：凡合同称「N 项 / N 个接口 / N 个事件」，一律按台账所记目标锚的精确节界机器派生。
# 本版按 v1.4 三票的 B1–B5 收窄：
#   B1 判「正文短语命中」前先剥离台账块与说明区，台账自身不算命中；
#   B2 全域穷举扫描：数词形态每一处命中都须归属某台账行，未归属即红（声明=实现）；
#   B3 台账「锚」列须与 ⑥ 输入表一致，不得手写任意 commit；
#   B4 切片起界与止界都须实证命中，任一未命中即红；
#   B5 台账行号与派生器各自唯一，且与 checker 派生器集合精确相等。
set -u
SPEC=${SPEC:-specs/sp4/spec.md}
MODE=${1:-check}
python3 - "$SPEC" "$MODE" <<'PY'
import re,subprocess,sys

spec_path,mode=sys.argv[1:]
CHECKER_FILES=('specs/sp4/xref-check.sh','specs/sp4/contract-check.sh','specs/sp4/check.sh')

class Boundary(Exception): pass

def _slice(text,start,stop,label):
    """B4：起界与止界都必须实证命中，任一未命中即抛错，不靠『结果非 0』兜底。"""
    out=[];on=False;start_seen=False;stop_seen=False
    for l in text.splitlines():
        if not on and re.match(start,l): on=True; start_seen=True; continue
        if on and re.match(stop,l): stop_seen=True; break
        if on: out.append(l)
    if not start_seen: raise Boundary(f'起界未命中：{label} 的起界在目标件中 0 命中')
    if not stop_seen:  raise Boundary(f'止界未命中：{label} 的止界在目标件中 0 命中（切片一路取到 EOF，分母不可信）')
    return out

def d_sp8_items(text):
    return len([l for l in _slice(text,r'^### 11\.5a(?![0-9A-Za-z])',r'^### 11\.6(?![0-9])','^### 11.5a … ^### 11.6')
                if re.match(r'^[0-9]+\. ',l)])

def d_arch_interfaces(text):
    return len({m.group(1) for l in _slice(text,r'^## 6\.(?![0-9])',r'^## ','^## 6. … 下一个 ^##')
                for m in [re.match(r'^\| `(CAP-(?:CMD|QRY|CAP)-[^`]+)` \| CAP \|',l)] if m})

def d_arch_events(text):
    return len({m.group(1) for l in _slice(text,r'^### 4\.2(?![0-9])',r'^## ','^### 4.2 … 下一个 ^##')
                for m in [re.match(r'^\| `(EV-CAP-[^`]+)` \| CAP \|',l)] if m})

DERIVERS={
 'sp8-11.5a-items':     (d_sp8_items,      '^### 11.5a … ^### 11.6'),
 'arch-cap-interfaces': (d_arch_interfaces,'^## 6. … 下一个 ^##'),
 'arch-cap-events':     (d_arch_events,    '^### 4.2 … 下一个 ^##'),
}

CN={1:('一',),2:('两','二'),3:('三',),4:('四',),5:('五',),6:('六',),
    7:('七',),8:('八',),9:('九',),10:('十',)}
ALL_CN=[c for v in CN.values() for c in v]
UNIT=re.compile(r'[0-9]+|['+''.join(ALL_CN)+r']+')
# B2：全域穷举的数词形态族（与门槛给的字面一致）
FORM=re.compile(r'(?:[0-9]+|['+''.join(ALL_CN)+r']+)\s?(?:项|个?(?:既有)?接口|个?(?:既有)?(?:域)?事件)')

LEDGER=re.compile(r'<!-- XREF-SP4-BEGIN -->(.*?)<!-- XREF-SP4-END -->',re.S)
NOTE=re.compile(r'<!-- XREF-NOTE-BEGIN -->.*?<!-- XREF-NOTE-END -->',re.S)

def parse_ledger(spec):
    m=LEDGER.search(spec)
    if not m: return None,['✗ XREF-SP4 跨件引用台账缺失']
    rows=[];errs=[]
    for l in m.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip().strip('`') for x in l.strip().strip('|').split('|')]
        if not c or not re.fullmatch(r'\d+',c[0]): continue
        if len(c)!=8:   # N2：块内数字开头的行解析不出 8 列，必须报错，不得静默跳过
            errs.append(f'✗ 台账有行未被解析（应为 8 列，实为 {len(c)} 列）：{l.strip()[:80]}'); continue
        rows.append({'n':c[0],'deriver':c[1],'file':c[2],'anchor':c[3],'bounds':c[4],
                     'what':c[5],'claim':c[6],'forms':[x.strip() for x in c[7].split('；') if x.strip()]})
    if not rows and not errs: errs.append('✗ XREF-SP4 台账为空（0 行可解析）')
    return rows,errs

def parse_input_table(spec):
    """B3：⑥ 输入来源表 → {path: anchor}，台账锚只能取自这里。"""
    m=re.search(r'### ⑥ 输入来源(.*?)(?=\n## )',spec,re.S)
    if not m: return None
    t={}
    for l in m.group(1).splitlines():
        r=re.match(r'^\| `(docs/[^`]+)`(?: @ `([0-9a-f]{7,40})`)? \| `([0-9a-f]{64})` \|',l)
        if r: t[r.group(1)]=r.group(2)
    return t

def scan_body(spec):
    """B1：剥离台账块与说明区后的『生效正文』，判命中只认这里。"""
    return NOTE.sub('',LEDGER.sub('',spec))

def run(spec,checkers=None):
    errs=[]
    rows,perr=parse_ledger(spec); errs+=perr
    if rows is None: return errs
    body=scan_body(spec)

    # 说明区不得吞掉判据行（防止把 D/AC 行划进豁免区绕过扫描）
    for blk in NOTE.findall(spec):
        bad=[l for l in blk.splitlines() if re.match(r'^\| `(?:D|AC)-SP4-\d{2}`',l)]
        if bad: errs.append(f'✗ 说明区内含 {len(bad)} 条判据行，判据行不得进豁免区')
    if spec.count('<!-- XREF-NOTE-BEGIN -->')!=spec.count('<!-- XREF-NOTE-END -->'):
        errs.append('✗ 说明区围栏不配对')

    itab=parse_input_table(spec)
    if itab is None: errs.append('✗ 找不到 ⑥ 输入来源表，台账锚无从校验')

    # B5：行号与派生器各自唯一
    for key,label in (('n','行号'),('deriver','派生器')):
        vals=[r[key] for r in rows]
        dup=sorted({v for v in vals if vals.count(v)>1})
        if dup: errs.append(f'✗ 台账{label}重复：{"、".join(dup)}（重复行会折叠进集合却抬高条目数）')

    registered=[]   # (form, derived_n)
    for r in rows:
        tag=f"[{r['n']} {r['deriver']}]"
        d=DERIVERS.get(r['deriver'])
        if not d: errs.append(f'{tag} ✗ 台账所记派生器在 checker 中不存在'); continue
        fn,label=d
        if r['bounds']!=label:
            errs.append(f"{tag} ✗ 节界声明与派生器不符：台账「{r['bounds']}」≠ checker「{label}」"); continue
        if itab is not None:                                   # B3
            if r['file'] not in itab:
                errs.append(f"{tag} ✗ 目标件不在 ⑥ 输入表：{r['file']}"); continue
            want=itab[r['file']]
            if want is not None and r['anchor']!=want:
                errs.append(f"{tag} ✗ 台账锚 `{r['anchor']}` 与 ⑥ 表所冻结的 `{want}` 不一致（锚不得手写）"); continue
        try: b=subprocess.check_output(['git','show',f"{r['anchor']}:{r['file']}"],stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError:
            errs.append(f"{tag} ✗ 目标件不可取：{r['anchor']}:{r['file']}"); continue
        try: got=fn(b.decode('utf8','replace'))
        except Boundary as e:
            errs.append(f'{tag} ✗ {e}'); continue
        if got==0: errs.append(f'{tag} ✗ 派生实数=0（节界内无计数对象）'); continue
        if not re.fullmatch(r'\d+',r['claim']) or int(r['claim'])!=got:
            errs.append(f"{tag} ✗ 声称实数={r['claim']} ≠ 目标锚派生实数={got}"); continue
        if not r['forms']: errs.append(f'{tag} ✗ 未登记任何正文形态'); continue
        for ph in r['forms']:
            m=UNIT.match(ph)
            if not m: errs.append(f'{tag} ✗ 正文形态「{ph}」不以数词开头'); continue
            head,rest=m.group(0),ph[m.end():]
            ok = head==str(got) or head in CN.get(got,())
            if not ok: errs.append(f'{tag} ✗ 正文形态「{ph}」的数词与派生实数 {got} 不符'); continue
            if ph not in body:                                  # B1：剥离台账/说明区后仍须命中
                errs.append(f'{tag} ✗ 正文形态「{ph}」在生效正文中 0 命中（台账与正文脱节；台账自身不算命中）'); continue
            registered.append((ph,got))
            bad=[c+rest for c in ALL_CN+[str(i) for i in range(1,11)]
                 if c!=head and c not in CN.get(got,()) and (c+rest) in body]
            if bad: errs.append(f'{tag} ✗ 生效正文残留旧数词：{"、".join(sorted(set(bad)))}')

    orphan=sorted(set(DERIVERS)-{r['deriver'] for r in rows})
    if orphan: errs.append('✗ checker 有派生器未被台账登记（分母可绕过）：'+'、'.join(orphan))

    # B2：全域穷举——生效正文里每一处数词形态都须归属某已登记形态
    forms=[f for f,_ in registered]
    if forms:
        unattr=[]
        for m in FORM.finditer(body):
            if not any(body.startswith(f,m.start()) for f in forms):
                ctx=body[max(0,m.start()-28):m.end()+18].replace('\n',' ')
                unattr.append(f'「{m.group(0)}」… {ctx.strip()}')
        if unattr:
            errs.append(f'✗ 生效正文有 {len(unattr)} 处数词形态未归属任何台账行（声明称「一律」则实现须全覆盖）：')
            errs+= ['    · '+u for u in unattr]

    # N1：旧数词残留扫描纳入 checker 自身
    if checkers:
        for path,txt in checkers.items():
            for ph,got in registered:
                m=UNIT.match(ph); rest=ph[m.end():]
                bad=[c+rest for c in ALL_CN+[str(i) for i in range(1,11)]
                     if c not in CN.get(got,()) and c!=str(got) and (c+rest) in txt]
                if bad: errs.append(f'✗ {path} 残留旧数词：{"、".join(sorted(set(bad)))}')
    return errs

spec=open(spec_path,encoding='utf8').read()

def read_checkers():
    out={}
    for f in CHECKER_FILES:
        try: out[f]=open(f,encoding='utf8').read()
        except OSError: pass
    return out

if mode!='--selftest':
    errs=run(spec,read_checkers())
    for e in errs: print(e)
    if errs: raise SystemExit(2)
    rows,_=parse_ledger(spec)
    for r in rows: print(f"XREF-VALUE {r['deriver']}={r['claim']}")
    print('跨件引用台账实算：'+' · '.join(
        f"{r['deriver']}@{r['anchor']}={r['claim']}（{r['what']}／{'；'.join(r['forms'])}）" for r in rows))
    print(f'--- SP4 跨件引用门 {len(rows)} 条全部按目标锚机器派生一致')
    raise SystemExit(0)

# ────────── 反测：每条加固都必须能证明「不加固就会红」 ──────────
n=0;fail=0
def t(name,text,want_red,checkers=None):
    global n,fail;n+=1
    red=bool(run(text,checkers))
    if red==want_red: print('  ✓ '+name)
    else: print(f'  ✗ {name}: want_red={want_red} got_red={red}');fail=1

ROW1=[l for l in spec.splitlines() if l.startswith('| 1 | `sp8-11.5a-items`')][0]
ROW2=[l for l in spec.splitlines() if l.startswith('| 2 | `arch-cap-interfaces`')][0]
FORM1=ROW1.strip().strip('|').split('|')[7].strip()          # 六项独立复算
WRONG=CN[5][0]+FORM1[UNIT.match(FORM1).end():]               # 旧数词形态，避免在本文件写死字面量

t('原样台账放行',spec,False)
t('B5 台账重复整行必红',spec.replace(ROW1,ROW1+'\n'+ROW1.replace('| 1 |','| 4 |',1)),True)
t('B5 台账重复行号必红',spec.replace('| 2 | `arch-cap-interfaces`','| 1 | `arch-cap-interfaces`'),True)
t('B5 孤儿派生器（删台账行）必红',re.sub(r'\n\| 2 \| `arch-cap-interfaces`[^\n]*','',spec),True)
t('B1 真脱节：只改正文、不改台账 必红',
  scan_body(spec).replace(FORM1,'逐项复算共 6 条')+LEDGER.search(spec).group(0),True)
t('B2 正文出现未登记数词形态必红',spec.replace('## 3. 明确不交付','本域另有 9 接口待议。\n\n## 3. 明确不交付'),True)
t('B3 台账锚与 ⑥ 表不一致必红',spec.replace('`970effe` | `^### 11.5a','`c7ac0c3` | `^### 11.5a'),True)
t('B3 目标件不在 ⑥ 表必红',spec.replace('`docs/dingwei-sp8-design.md` | `970effe` | `^### 11.5a',
                                        '`docs/not-in-table.md` | `970effe` | `^### 11.5a'),True)
t('声称数改错必红',spec.replace(ROW1,ROW1.replace('| 6 |','| 5 |')),True)
t('目标件不可取必红',spec.replace('`970effe` | `^### 11.5a','`deadbee` | `^### 11.5a'),True)
t('节界声明与派生器不符必红',spec.replace('^### 11.5a … ^### 11.6','^### 11.5 … ^### 11.6'),True)
t('派生器不存在必红',spec.replace('`sp8-11.5a-items`','`sp8-11.5a-lines`'),True)
t('N2 台账行列数写坏（7 列）必红',spec.replace(ROW2,'| 4 | `arch-cap-events` | x | y | z | w | 2 |'),True)
t('说明区吞掉判据行必红',
  spec.replace('<!-- ACCEPTANCE-SP4-BEGIN -->','<!-- ACCEPTANCE-SP4-BEGIN -->\n<!-- XREF-NOTE-BEGIN -->')
      .replace('<!-- ACCEPTANCE-SP4-END -->','<!-- XREF-NOTE-END -->\n<!-- ACCEPTANCE-SP4-END -->'),True)
t('说明区围栏不配对必红',spec.replace('<!-- XREF-NOTE-END -->','',1),True)
t('台账整块缺失必红',spec.replace('<!-- XREF-SP4-BEGIN -->',''),True)
t('N1 checker 自身残留旧数词必红',spec,True,{'specs/sp4/fake.sh':f'echo 仍按{WRONG}执行'})
t('N1 checker 无残留则不红',spec,False,{'specs/sp4/fake.sh':'echo ok'})

# B4：止界不命中必须抛错（目标件在 git 里不可变，故直接对派生器喂截断文本）
n+=1
raw=subprocess.check_output(['git','show','970effe:docs/dingwei-sp8-design.md']).decode()
trunc=raw.split('### 11.6')[0]
try:
    d_sp8_items(trunc); print('  ✗ B4 止界不命中未抛错（切片取到 EOF 仍判过）'); fail=1
except Boundary: print('  ✓ B4 止界不命中必抛错')
n+=1
try:
    d_sp8_items(raw.replace('### 11.5a ','### 11.5X ')); print('  ✗ B4 起界不命中未抛错'); fail=1
except Boundary: print('  ✓ B4 起界不命中必抛错')

n+=1
try:
    d_sp8_items(raw.replace('### 11.5a ','### 11.5abc ')); print('  ✗ 起界正则误命中同前缀更长标题（11.5abc 被当成 11.5a）'); fail=1
except Boundary: print('  ✓ 起界正则不误命中同前缀更长标题')
print(f'--- SP4 跨件引用反测 {n} 项全过' if not fail else '--- SP4 跨件引用反测失败')
raise SystemExit(fail)
PY
