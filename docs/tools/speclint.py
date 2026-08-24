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
# 受控 ID 表:表名 -> (起始锚, 结束锚)。ID 须唯一;基数须连续(允许 12a/12b 形态的插入项)
ID_TABLES = [('E.1','## E.1 第 I 类','## E.2'), ('E.2','## E.2 第 II 类','## E.3'),
             ('E.3','## E.3 第 III 类','## E.4'), ('E.4','## E.4 第 IV 类','## E.5')]
# 跨表 typed 外键:引用写法 -> 目标表。任一引用须解析到该表实际存在的 ID
TYPED_FKEYS = [(r'复合 ← E\.1 ((?:#\d+(?:\s*·\s*)?)+)', 'E.1'),
               (r'复合 → E\.4 ((?:#\d+(?:\s*·\s*)?)+)', 'E.4'),
               (r'(?<![.\w])E\.4 (#\d+)', 'E.4'), (r'(?<![.\w])E\.1 (#\d+)', 'E.1')]
# 权威定义唯一:概念 -> 须恰好出现一次的【标记行】。标记为 〔权威定义〕,写在该概念的唯一定义处
AUTHORITATIVE_MARK = '〔权威定义〕'
AUTHORITATIVE_DEFS = ['domain_level', 'visibility', '生命周期', '记忆类型', '凭据分级', 'task']
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
                # 语境按【所属节】判,不按前 3 行 —— 前 3 行窗口太窄,把合法用法误报成漂移
                near=_section_of(s,i)
                if not any(c in near for c in ctx):
                    out.append((i,'symbol-context',msg,ln.strip()[:70]))
    return out

def _section_of(s,lineno):
    """取该行所属 heading 的整节正文(用于语境判定)"""
    hs=_headings(s); lines=s.split('\n')
    start=0; end=len(lines)
    for _,ln,_ in hs:
        if ln<=lineno: start=ln-1
        elif ln>lineno: end=ln-1; break
    return '\n'.join(lines[start:end])

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

# 🔴 跨文档引用带来源类型时，闸也得认识那个类型（结构域重走实测）：
#    正文写 `A§0.0`（架构设计的节号），闸剥掉 A 后当成本件的 §0.0 → 误报悬空。
#    ⇒ 带来源前缀者不纳入本件判定。这不是放行，是【判定范围】：
#      本件的闸只管本件的编号空间；A§ 由 scripts/arch-refcheck.sh 管，两边不重叠也不留空。
# ⚠️ 抽成共用函数，是因为扫 §N 的地方有两处（check_refs / check_link_resolve）——
#    只改一处就是「改一处漏一处」，本轮已犯四次。
FOREIGN_PREFIXES = [r'A§(?:\d+(?:\.\d+[a-z]?)*)']
def _drop_foreign(s):
    for p in FOREIGN_PREFIXES: s = re.sub(p, '', s)
    return s

def check_refs(s):
    s = _drop_foreign(s)
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

def _tbl_ids(s, a, b):
    seg=s[s.find(a):s.find(b)]
    return re.findall(r'^\| \*{0,2}(\d+[a-z]?)\*{0,2} \|', seg, re.M)

def check_ids(s):
    """#47b-1 受控 ID 唯一且连续(基数连续;字母后缀视为插入项,须挂在已存在的基数上)"""
    out=[]
    for name,a,b in ID_TABLES:
        ids=_tbl_ids(s,a,b)
        seen=set()
        for x in ids:
            if x in seen: out.append((0,'id-dup',f'{name} ID #{x} 重复',''))
            seen.add(x)
        # 基数集只取【纯数字 ID】:若把 7a 也算进基数,「7a 的基数 7 不存在」就永远判不出来
        base=sorted({int(x) for x in ids if x.isdigit()})
        for x in ids:
            m=re.fullmatch(r'(\d+)([a-z])',x)
            if m and int(m.group(1)) not in base:
                out.append((0,'id-orphan',f'{name} #{x} 是插入项但基数 #{m.group(1)} 不存在',''))
        if base:
            miss=[n for n in range(base[0],base[-1]+1) if n not in base]
            # 允许显式作废(行内写「已删除」「已并入」)的空号
            for n in miss:
                if not re.search(r'#%d\D{0,20}(已删除|已并入|已撤销)'%n, s):
                    out.append((0,'id-gap',f'{name} 基数 #{n} 缺号且未标注作废',''))
    return out

def check_fkeys(s):
    """#47b-2 跨表 typed 外键完整:每个引用须解析到目标表实际存在的 ID"""
    exist={name:set(_tbl_ids(s,a,b)) for name,a,b in ID_TABLES}
    out=[]
    for pat,tgt in TYPED_FKEYS:
        for m in re.finditer(pat,s):
            for num in re.findall(r'#(\d+[a-z]?)',m.group(1)):
                if num not in exist.get(tgt,set()):
                    ln=s[:m.start()].count('\n')+1
                    out.append((ln,'fkey',f'引用 {tgt} #{num},但该表无此 ID',m.group(0)[:50]))
    return out

def check_authoritative(s):
    """#47b-3 权威定义唯一:每个受控概念的 〔权威定义〕 标记须恰好一次"""
    out=[]
    for c in AUTHORITATIVE_DEFS:
        hits=[i for i,ln in enumerate(s.split('\n'),1)
              if AUTHORITATIVE_MARK in ln and c in ln]
        if len(hits)==0: out.append((0,'authdef',f'概念「{c}」无 {AUTHORITATIVE_MARK} 标记',''))
        elif len(hits)>1: out.append((hits[1],'authdef',f'概念「{c}」有 {len(hits)} 处权威定义标记',f'L{hits}'))
    return out

def _headings(s):
    return [(m.group(1), s[:m.start()].count('\n')+1, m.group(0).strip())
            for m in re.finditer(r'^#{2,4}\s+(?:[🔴⚠️✅⛔]\s*)*(?:§\s*)?(\d+(?:\.\d+[a-z]?(?:-\d+)?)*)\s',s,re.M)]

def check_heading_ids(s):
    """#47b-4 heading 编号唯一"""
    out=[]; seen={}
    for num,ln,txt in _headings(s): seen.setdefault(num,[]).append((ln,txt))
    for num,v in seen.items():
        if len(v)>1: out.append((v[1][0],'head-dup',f'章节号 §{num} 出现 {len(v)} 次',
                                 ' / '.join(t[:28] for _,t in v)))
    return out

def check_fences(s):
    """#47b-5 代码围栏闭合"""
    n=len(re.findall(r'^```',s,re.M))
    return [] if n%2==0 else [(0,'fence',f'代码围栏 ``` 共 {n} 处,未闭合','')]

def check_link_resolve(s):
    """#47b-6 内部链接唯一解析:每个 §N 须解析到【恰好一个】heading"""
    idx={}
    for num,ln,_ in _headings(s): idx.setdefault(num,[]).append(ln)
    out=[]
    for r in sorted(set(re.findall(r'§(\d+(?:\.\d+[a-z]?(?:-\d+)?)*)', _drop_foreign(s)))):
        t=idx.get(r,[])
        if len(t)==0: out.append((0,'link-dangling',f'§{r} 无对应章节',''))
        elif len(t)>1: out.append((t[1],'link-ambig',f'§{r} 解析到 {len(t)} 个章节',f'L{t}'))
    return out

def check_s_family(s):
    """S 系收口后的不变量:裸 S0-S2 只表凭据分级;裸 S3-S8 恒为 0(凭据分级只定义三级);
    子 spec 一律写 SP0-SP8。写登记不写判据等于没登记 —— 故落成规则,不落成备忘。"""
    out=[]
    # 区间写法(S0-S8 / `S3`-`S8`)指的是【符号族】,不是某个具体编号 —— 扫描前剥离。
    # 不剥离的话,本规则会把「解释这条规则的文字」自己判成违例。
    scan=re.sub(r'S\d[`\s]*[–—-][`\s]*S\d', '', s)
    for m in re.finditer(r'(?<!P)\bS[3-8][ab]?\b', scan):
        ln=scan[:m.start()].count('\n')+1
        out.append((ln,'s-family',
                    '裸 %s:凭据分级只定义 S0-S2,子 spec 须写 SP 系' % m.group(0),
                    scan.split('\n')[ln-1].strip()[:70]))
    return out

ALL_CHECKS = [check_typed_refs, check_symbol_context, check_field_aliases, check_tables,
              check_refs, check_E, check_count_refs, check_ids, check_fkeys,
              check_authoritative, check_heading_ids, check_fences, check_link_resolve, check_s_family]

def run_all(doc):
    out=[]
    for f in ALL_CHECKS:
        try: out+=f(doc)
        except Exception as e: out.append((0,'crash',f'{f.__name__} 抛异常: {e}',''))
    return out

def parse_corpus(txt):
    """块式语料:CASE <POS|NEG> <rule-kind> [USEFIXTURE] / <doc 多行> / ENDCASE"""
    fx=txt[txt.find('FIXTURE\n')+8:txt.find('\nENDFIXTURE')] if 'FIXTURE\n' in txt else ''
    cases=[]
    for m in re.finditer(r'^CASE (POS|NEG) (\S+)( USEFIXTURE)?\n(.*?)^ENDCASE\s*$', txt, re.M|re.S):
        kind,rule,usefx,body=m.group(1),m.group(2),bool(m.group(3)),m.group(4)
        cases.append((kind,rule,(fx+'\n'+body) if usefx else body))
    return cases

def selftest(cpath):
    """NEG 须命中【预期的那一条规则】;POS 须该规则不命中。规则改动前置门(E.3 #47a)。"""
    cases=parse_corpus(load(cpath)); bad=0; cov={}
    for kind,rule,doc in cases:
        got={o[1] for o in run_all(doc)}
        cov.setdefault(rule,[0,0]); cov[rule][0 if kind=='NEG' else 1]+=1
        if kind=='NEG' and rule not in got:
            print(f'  ❌ NEG 未命中 {rule}(实得 {sorted(got) or "无"}): {doc.strip().splitlines()[-1][:60]}'); bad+=1
        if kind=='POS' and rule in got:
            print(f'  ❌ POS 被 {rule} 误拦: {doc.strip().splitlines()[-1][:60]}'); bad+=1
    kinds={k for _,k,_ in cases}
    print(f'--- 语料 {len(cases)} 例 / 覆盖规则 {len(kinds)} 条,'
          + ('全部通过' if not bad else f'{bad} 例不符'))
    for r in sorted(cov): 
        n,p_=cov[r]
        if not n or not p_: print(f'  ⚠️ 规则 {r} 正反例不全(NEG {n} / POS {p_})')
    return bad

# 变异体:每条规则配一处「故意改坏」,其 NEG 例必须随之失守。
# 不失守 = 该 NEG 没在测这条规则,语料是空的(E.3 #47a「语料须有牙」)。
MUTATIONS = [
    ('count-ref',     "(?:\\.\\d+[a-z]?)*)\\s'", "(?:\\.\\d+[a-z]?)?)\\s'"),
    ('id-gap',        "if not re.search(r'#%d", "if False and not re.search(r'#%d"),
    ('id-dup',        "if x in seen: out.append", "if False: out.append"),
    ('id-orphan',     "if m and int(m.group(1)) not in base:", "if False:"),
    ('fkey',          "if num not in exist.get(tgt,set()):", "if False:"),
    ('authdef',       "if len(hits)==0:", "if False:"),
    ('head-dup',      "if len(v)>1: out.append", "if False: out.append"),
    ('fence',         "return [] if n%2==0 else", "return [] if True else"),
    ('link-ambig',    "elif len(t)>1: out.append", "elif False: out.append"),
    ('link-dangling', "if len(t)==0: out.append", "if False: out.append"),
    ('table',         "if len(blk)==1: out.append", "if False: out.append"),
    ('symbol-context',"if not any(c in near for c in ctx):", "if False:"),
    ('s-family',      "for m in re.finditer(r'(?<!P)\\bS[3-8]", "for m in re.finditer(r'(?<!P)\\bZZ[3-8]"),
    ('typed-symbol',  "for m in re.finditer(pat,s):\n            ln=s[:m.start()]",
                      "for m in []:\n            ln=s[:m.start()]"),
]

def mutate(cpath, selfpath):
    """对每条规则施加一处变异,验证其 NEG 例随之失守。"""
    import subprocess, tempfile, os
    src=load(selfpath); cases=parse_corpus(load(cpath)); bad=0
    for rule,pat,repl in MUTATIONS:
        neg=[c for c in cases if c[0]=='NEG' and c[1]==rule]
        if not neg: print(f'  ⚠️ 规则 {rule} 无 NEG 例,变异无从验证'); bad+=1; continue
        n=src.count(pat); mut=src.replace(pat,repl)
        if n==0: print(f'  ❌ 变异未命中源码:{rule}({pat[:40]})'); bad+=1; continue
        with tempfile.NamedTemporaryFile('w',suffix='.py',delete=False,encoding='utf-8') as f:
            f.write(mut); mp=f.name
        r=subprocess.run([sys.executable,mp,'--selftest',cpath],capture_output=True,text=True)
        os.unlink(mp)
        if r.returncode==0:
            print(f'  ❌ 变异体仍通过:{rule} 的 NEG 例没在测这条规则'); bad+=1
        elif f'NEG 未命中 {rule}' not in r.stdout:
            print(f'  ❌ 变异体失守在别处,不是 {rule}:\n{r.stdout.strip()[:200]}'); bad+=1
    print(f'--- 变异体 {len(MUTATIONS)} 个,'+('全部如期失守(语料有牙)' if not bad else f'{bad} 个不符'))
    return bad

if len(sys.argv)>2 and sys.argv[1]=='--mutate':
    sys.exit(1 if mutate(sys.argv[2], __file__) else 0)

if len(sys.argv)>2 and sys.argv[1]=='--selftest':
    sys.exit(1 if selftest(sys.argv[2]) else 0)

s=load(sys.argv[1])
all_out=(check_typed_refs(s)+check_symbol_context(s)+check_field_aliases(s)+check_tables(s)
         +check_refs(s)+check_E(s)+check_count_refs(s)
         +check_ids(s)+check_fkeys(s)+check_authoritative(s)+check_heading_ids(s)
         +check_fences(s)+check_link_resolve(s)+check_s_family(s))
for ln,kind,msg,ctx in sorted(all_out,key=lambda x:(x[1],x[0])):
    print(f"[{kind:14s}] L{ln:<5d} {msg}  {ctx}")
print("---", f"{len(all_out)} 处" if all_out else "全部通过")
sys.exit(1 if all_out else 0)
