#!/bin/sh
# SP4 跨件引用门：凡合同文本称「N 项 / N 个接口 / N 个事件」，一律按台账所记目标锚的精确节界机器派生实数。
# 目标件不可取、节界取空、声称数≠派生数、节界声明与派生器不符、正文数词与派生数不符、台账缺失/孤儿 —— 一律 exit 2。
set -u
SPEC=${SPEC:-specs/sp4/spec.md}
MODE=${1:-check}
python3 - "$SPEC" "$MODE" <<'PY'
import hashlib,re,subprocess,sys

spec_path,mode=sys.argv[1:]

# ── 派生器：节界写死在此，并以 label 与台账「节界」列逐字对撞（两边任一改动而另一边不改即红）
def _slice(text,start,stop):
    out=[];on=False
    for l in text.splitlines():
        if re.match(start,l): on=True; continue
        if on and re.match(stop,l): break
        if on: out.append(l)
    return out

def d_sp8_items(text):
    return len([l for l in _slice(text,r'^### 11\.5a',r'^### 11\.6') if re.match(r'^[0-9]+\. ',l)])

def d_arch_interfaces(text):
    return len({m.group(1) for l in _slice(text,r'^## 6\.',r'^## ')
                for m in [re.match(r'^\| `(CAP-(?:CMD|QRY|CAP)-[^`]+)` \| CAP \|',l)] if m})

def d_arch_events(text):
    return len({m.group(1) for l in _slice(text,r'^## 4\.',r'^## ')
                for m in [re.match(r'^\| `(EV-CAP-[^`]+)` \| CAP \|',l)] if m})

DERIVERS={
 'sp8-11.5a-items':      (d_sp8_items,      '^### 11.5a … ^### 11.6'),
 'arch-cap-interfaces':  (d_arch_interfaces,'^## 6. … 下一个 ^##'),
 'arch-cap-events':      (d_arch_events,    '^## 4. … 下一个 ^##'),
}

CN={1:('一',),2:('两','二'),3:('三',),4:('四',),5:('五',),6:('六',),
    7:('七',),8:('八',),9:('九',),10:('十',)}
ALL_CN=[c for v in CN.values() for c in v]

def parse_ledger(spec):
    m=re.search(r'<!-- XREF-SP4-BEGIN -->(.*?)<!-- XREF-SP4-END -->',spec,re.S)
    if not m: return None,'✗ XREF-SP4 跨件引用台账缺失'
    rows=[]
    for l in m.group(1).splitlines():
        if not l.startswith('|'): continue
        c=[x.strip().strip('`') for x in l.strip().strip('|').split('|')]
        if len(c)!=8 or not re.fullmatch(r'\d+',c[0]): continue
        rows.append({'n':c[0],'deriver':c[1],'file':c[2],'anchor':c[3],
                     'bounds':c[4],'what':c[5],'claim':c[6],'phrase':c[7]})
    if not rows: return None,'✗ XREF-SP4 台账为空（0 行可解析）'
    return rows,''

def run(spec):
    rows,err=parse_ledger(spec)
    if err: return [err]
    errs=[];seen=set()
    for r in rows:
        tag=f"[{r['n']} {r['deriver']}]"
        fn=DERIVERS.get(r['deriver'])
        if not fn: errs.append(f"{tag} ✗ 台账所记派生器在 checker 中不存在"); continue
        fn,label=fn; seen.add(r['deriver'])
        if r['bounds']!=label:
            errs.append(f"{tag} ✗ 节界声明与派生器不符：台账「{r['bounds']}」≠ checker「{label}」"); continue
        try: b=subprocess.check_output(['git','show',f"{r['anchor']}:{r['file']}"],stderr=subprocess.DEVNULL)
        except subprocess.CalledProcessError:
            errs.append(f"{tag} ✗ 目标件不可取：{r['anchor']}:{r['file']}"); continue
        got=fn(b.decode('utf8','replace'))
        if got==0: errs.append(f"{tag} ✗ 节界取空：{r['anchor']}:{r['file']} 派生实数=0（目标节不存在或取法失效）"); continue
        if not re.fullmatch(r'\d+',r['claim']) or int(r['claim'])!=got:
            errs.append(f"{tag} ✗ 声称实数={r['claim']} ≠ 目标锚派生实数={got}"); continue
        phrase=r['phrase']
        cn,rest=phrase[0],phrase[1:]
        if cn not in CN.get(got,()):
            errs.append(f"{tag} ✗ 正文短语「{phrase}」的数词与派生实数 {got} 不符"); continue
        if phrase not in spec:
            errs.append(f"{tag} ✗ 正文短语「{phrase}」在合同中 0 命中，台账与正文脱节"); continue
        bad=[c+rest for c in ALL_CN if c not in CN[got] and (c+rest) in spec]
        if bad: errs.append(f"{tag} ✗ 合同中仍有旧数词残留：{'、'.join(bad)}")
    orphan=sorted(set(DERIVERS)-seen)
    if orphan: errs.append('✗ checker 有派生器未被台账登记（分母可绕过）：'+'、'.join(orphan))
    return errs

spec=open(spec_path,encoding='utf8').read()

if mode!='--selftest':
    errs=run(spec)
    for e in errs: print(e)
    if errs: raise SystemExit(2)
    rows,_=parse_ledger(spec)
    print('跨件引用台账实算：'+' · '.join(f"{r['deriver']}@{r['anchor']}={r['claim']}（{r['what']}／{r['phrase']}）" for r in rows))
    print(f'--- SP4 跨件引用门 {len(rows)} 条全部按目标锚机器派生一致')
    raise SystemExit(0)

# ── 反测：钉死「分母写错要红」
n=0;fail=0
def t(name,text,want_red):
    global n,fail;n+=1
    red=bool(run(text))
    if red==want_red: print('  ✓ '+name)
    else: print(f'  ✗ {name}: want_red={want_red} got_red={red}');fail=1

t('原样台账放行',spec,False)
t('声称数改错（6→5）必红',spec.replace('| `sp8-11.5a-items` | `docs/dingwei-sp8-design.md` | `970effe` | `^### 11.5a … ^### 11.6` | 行首编号条目 | 6 |',
                                        '| `sp8-11.5a-items` | `docs/dingwei-sp8-design.md` | `970effe` | `^### 11.5a … ^### 11.6` | 行首编号条目 | 5 |'),True)
t('换锚到 1f703b8（该节实为 5 项）而声称仍 6 必红',spec.replace('`970effe` | `^### 11.5a','`1f703b8` | `^### 11.5a'),True)
t('目标件不可取必红',spec.replace('`970effe` | `^### 11.5a','`deadbee` | `^### 11.5a'),True)
t('节界声明与派生器不符必红',spec.replace('^### 11.5a … ^### 11.6','^### 11.5 … ^### 11.6'),True)
t('派生器写错（台账登记不存在的派生器）必红',spec.replace('`sp8-11.5a-items`','`sp8-11.5a-lines`'),True)
t('正文残留旧数词「五项独立复算」必红',spec+'\n附注：仍按五项独立复算执行。\n',True)
t('正文短语与台账脱节（正文改成七项）必红',spec.replace('六项独立复算','七项独立复算'),True)
t('删掉一行台账留下孤儿派生器必红',
  re.sub(r'\n\| 2 \| `arch-cap-interfaces`[^\n]*','',spec),True)
t('台账整块缺失必红',spec.replace('<!-- XREF-SP4-BEGIN -->',''),True)
print(f'--- SP4 跨件引用反测 {n} 项全过' if not fail else '--- SP4 跨件引用反测失败')
raise SystemExit(fail)
PY
