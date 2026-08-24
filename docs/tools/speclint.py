#!/usr/bin/env python3
"""SpecDoc 声明式校验器 —— 一个 lint，靠声明扩展，不逐残留造脚本。
用法: speclint.py <file.md>
"""
import re,sys,json

# ============ 声明区：新增规则只改这里 ============
CONTROLLED_SYMBOLS = {
    # 符号族 -> (正则, 允许出现的语境判据, 说明)
    'risk':   (r'\bL[123]\b',  ['§5.', '§23.5', '级别', '风险', 'node 的门 PASS', 'L1 试点', 'L3（'],
               'L1–L3 只表风险级别'),
    'domain': (r'\bD[0-3]\b',  None, 'D0–D3 只表归属域'),
    'gate':   (r'\bG[123]\b',  ['§19', '出站闸', 'agent↔agent', '转述', '收件面', '试点计量'],
               'G1–G3 只表出站闸层级'),
}
# 同义/冲突字段：同一概念的【顶层字段声明】只允许一种写法
# 只匹配字段声明位置（表格首列的 `field`），不匹配正文中的自由提及与子字段
FIELD_DECLS = [
    # (正则捕获字段名, 该概念的规范写法, 说明)
    # anchor_type 是 anchors[] 条目内的合法子字段（{anchor_type, value}），不是别名，不纳入本组
    (r'^\|\s*[🔴⚠️]*\s*`(anchors?\[\]|anchor)`\s*\|', 'anchors[]', '锚字段（顶层声明）'),
    (r'^\|\s*[🔴⚠️]*\s*`(domain_level|domain)`\s*\|', 'domain_level', '归属域字段（顶层声明）'),
]
# 交叉引用的类型化符号：§N 后面跟的符号必须属于该节的符号族
TYPED_REFS = [
    (r'§19(?:\.\d+)?\s*(L[123])', 'gate', '§19 应用 G 系符号'),
    (r'§11(?:\.\d+)?\s*(L[0-3])', 'domain', '§11 应用 D 系符号'),
]
# 「不写死条数」节的远端引用：凡引用这些节的行，不得携带条数/序数（E.3 #36a）
# 声明哪些措辞算「该节已声明不写死条数」——命中即把该节纳入受保护集，无需逐节登记
NO_HARDCODED_COUNT_DECLS = ['不写死条数', '不写死数量', '增删以本表为准']
# 条数/序数 token（引用侧一旦携带即失效）
COUNT_TOKENS = r'第\s*[一二三四五六七八九十百0-9]+\s*[–\-]?\s*[0-9]*\s*[条项类种]|[一二三四五六七八九十百]\s*[条项类种]|(?<![.\d])[0-9]+\s*[条项类种]'
# 本规则自身的文档行豁免（含规则名即视为在讲这条规则）
COUNT_REF_EXEMPT = 'NO_HARDCODED_COUNT_REFS'
# 历史引文:仅识别【声明式引文形态】,扫描前剥离。
# ⚠️ 边界:自由行文里的历史陈述机器分不出(kimi ③),那仍归 E.4 人审 —— 本表不宣称能覆盖。
HISTORICAL_QUOTES = [r'原写「[^」]*」', r'原文写「[^」]*」', r'曾写「[^」]*」', r'原作「[^」]*」']
# ================================================

def load(p): return open(p,encoding='utf-8').read()

def check_typed_refs(s):
    out=[]
    for pat,fam,msg in TYPED_REFS:
        for m in re.finditer(pat,s):
            ln=s[:m.start()].count('\n')+1
            out.append((ln,'typed-symbol',msg+f'（现为 {m.group(1)}）',s.split('\n')[ln-1].strip()[:70]))
    return out

def check_symbol_context(s):
    out=[]; lines=s.split('\n')
    for fam,(pat,ctx,msg) in CONTROLLED_SYMBOLS.items():
        if ctx is None: continue
        for i,ln in enumerate(lines,1):
            if re.search(pat,ln) and not any(c in ln for c in ctx):
                # 向上找 3 行取语境
                near='\n'.join(lines[max(0,i-4):i])
                if not any(c in near for c in ctx):
                    out.append((i,'symbol-context',msg,ln.strip()[:70]))
    return out

def check_field_aliases(s):
    """只看【字段声明位置】(表格首列的 `field`),不看正文自由提及与子字段。"""
    out=[]; lines=s.split('\n')
    for pat,canon,label in FIELD_DECLS:
        seen={}
        for i,ln in enumerate(lines,1):
            m=re.match(pat,ln)
            if m: seen.setdefault(m.group(1),[]).append(i)
        bad={k:v for k,v in seen.items() if k!=canon}
        if bad and canon in seen:
            out.append((min(sum(bad.values(),[])),'field-alias',
                        f'{label}声明处出现 {canon} 与 {list(bad)}，须统一',
                        f'规范写法在 L{seen[canon]}，冲突在 L{sum(bad.values(),[])}'))
    return out

def check_tables(s):
    out=[]; lines=s.split('\n'); i=0
    while i<len(lines):
        if lines[i].startswith('|'):
            st=i; blk=[]
            while i<len(lines) and lines[i].startswith('|'): blk.append(lines[i]); i+=1
            if len(blk)==1: out.append((st+1,'table','孤立表行',blk[0][:70]))
            elif not re.match(r'^\|[\s:*-]+\|',blk[1]): out.append((st+1,'table','缺分隔行',blk[0][:70]))
        else: i+=1
    return out

def check_refs(s):
    secs=set()
    for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?(?:-\d+)?)?)\s',s,re.M):
        secs.add('§'+m.group(1))
    return [(0,'ref','悬空引用: '+r,'') for r in sorted(set(re.findall(r'§\d+(?:\.\d+[a-z]?(?:-\d+)?)?',s))-secs)]

def check_E(s):
    def seg(a,b): return s[s.find(a):s.find(b)]
    def rows(t): return re.findall(r'^\| \*{0,2}(\d+[a-z]?)\*{0,2} \|',t,re.M)
    E=[len(rows(seg(a,b))) for a,b in [('## E.1 第 I 类','## E.2'),('## E.2 第 II 类','## E.3'),
                                        ('## E.3 第 III 类','## E.4'),('## E.4 第 IV 类','## E.5')]]
    out=[]
    # E.5 四行 + 复合数全覆盖:原只查 E.1/E.3/合计,E.2 与 E.4 两行漏查
    # (本席自己的重算脚本也只补这三行,于是 E.4 行长期无人校 —— 与 E.3 #36a 同一类:作用域小于断言)
    comp=len(re.findall(r'复合 ← E\.1',seg('## E.4 第 IV 类','## E.5')))
    for lbl,pat,val in [('E.1',r'\| 第 I 类 恒零计数器 \| \*{0,2}(\d+)\*{0,2}',E[0]),
                        ('E.2',r'\| 第 II 类 趋势指标 \| \*{0,2}(\d+)\*{0,2}',E[1]),
                        ('E.3',r'\| 第 III 类 建成核验项 \| \*{0,2}(\d+)\*{0,2}',E[2]),
                        ('E.4',r'\| 第 IV 类 评审检查项 \| \*{0,2}(\d+)\*{0,2}',E[3]),
                        ('E.4 复合',r'第 IV 类 评审检查项 \|[^|\n]*?(\d+) 项为复合项',comp),
                        ('合计',r'\| \*\*登记条目合计\*\* \| \*\*(\d+)\*\*',sum(E))]:
        m=re.search(pat,s)
        if m and int(m.group(1))!=val: out.append((0,'count',f'{lbl} 表载 {m.group(1)} 实算 {val}',''))
    # 支持多重引用: 「复合 ← E.1 #117 · #127」
    b=set()
    for m in re.finditer(r'复合 ← E\.1 ((?:#\d+(?:\s*·\s*)?)+)',s):
        b.update(re.findall(r'#(\d+)',m.group(1)))
    e=set(re.findall(r'\| \*{0,2}(\d+[a-z]?)\*{0,2} \|[^\n]*复合 → E\.4',s))
    for x in sorted(set(e)-b): out.append((0,'link',f'E.1 #{x} 标复合但无 E.4 指向',''))
    for x in sorted(b-set(e)): out.append((0,'link',f'E.4 指向 E.1 #{x} 但该行未标复合',''))
    return out

def check_count_refs(s):
    lines=s.split('\n')
    # 1) 找出【已声明不写死条数】的节:从 heading 到下一个同/更高级 heading
    # 多级 heading:4.6.5 须整段捕获(原 (?:\.\d+)? 只吃一级,把 4.6.5 截成 4.6 —— 规则因此静默不报)
    heads=[(m.start(),m.group(1))
           for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?)*)\s',s,re.M)]
    protected=set()
    for i,(pos,sec) in enumerate(heads):
        end=heads[i+1][0] if i+1<len(heads) else len(s)
        if any(d in s[pos:end] for d in NO_HARDCODED_COUNT_DECLS): protected.add(sec)
    # 2) 引用侧同行携带条数/序数即报
    out=[]
    for i,ln in enumerate(lines,1):
        if COUNT_REF_EXEMPT in ln: continue
        for sec in protected:
            if not re.search(r'§\s*'+re.escape(sec)+r'(?![.\d])',ln): continue
            # 该节自身正文内的就地枚举不算远端引用
            scan=ln
            for q in HISTORICAL_QUOTES: scan=re.sub(q,'',scan)
            hit=re.search(COUNT_TOKENS,scan)
            if hit: out.append((i,'count-ref',
                f'引用 §{sec}（已声明不写死条数）却带计数「{hit.group(0).strip()}」',ln.strip()[:70]))
    return out

def selftest(cpath):
    """反例须被拦且拦在预期规则上;正例须全过。规则改动后先跑这个(E.3 #47a)。"""
    txt=load(cpath); fx=txt[txt.find('FIXTURE\n')+8:txt.find('\nEND')]
    bad=0; total=0
    for ln in txt.split('\n'):
        ln=ln.strip()
        if not (ln.startswith('POS ') or ln.startswith('NEG ')): continue
        total+=1
        kind,_,rest=ln.partition(' ')
        rule,_,body=(rest.partition('|') if kind=='POS' else rest.partition('|'))
        rule=rule.strip(); body=body.strip()
        out=check_count_refs(fx+'\n'+body)
        got={o[1] for o in out}
        if kind=='NEG':
            if rule not in got: print(f'  ❌ 反例未拦在 {rule}: {body}  (实得 {got or "无"})'); bad+=1
        else:
            if got: print(f'  ❌ 正例误拦 {got}: {body}'); bad+=1
    print(f'--- 语料 {total} 条,{"全部通过" if not bad else str(bad)+" 条不符"}')
    return bad

if len(sys.argv)>2 and sys.argv[1]=='--selftest':
    sys.exit(1 if selftest(sys.argv[2]) else 0)

s=load(sys.argv[1])
all_out=(check_typed_refs(s)+check_field_aliases(s)+check_tables(s)+check_refs(s)+check_E(s)+check_count_refs(s))
for ln,kind,msg,ctx in sorted(all_out,key=lambda x:(x[1],x[0])):
    print(f"[{kind:14s}] L{ln:<5d} {msg}  {ctx}")
print("---", f"{len(all_out)} 处" if all_out else "全部通过")
sys.exit(1 if all_out else 0)
