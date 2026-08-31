#!/usr/bin/env python3
from html.parser import HTMLParser
from pathlib import Path
from collections import Counter
import hashlib,re,subprocess,sys
ROOT=Path(__file__).resolve().parents[2]; PROTO=ROOT/'docs/dingwei-prototype.html'; ARCH=ROOT/'docs/dingwei-platform-architecture.md'; MANIFEST=ROOT/'specs/v0.2/baseline-manifest.tsv'; DECISIONS=ROOT/'specs/prototype/DECISIONS.md'
class Scan(HTMLParser):
 def __init__(self): super().__init__(); self.ids=[]; self.nav=[]; self.sections=[]; self.innav=False
 def handle_starttag(self,t,a):
  d=dict(a); self.innav=self.innav or (t=='nav' and d.get('id')=='nav')
  if d.get('id'): self.ids.append(d['id'])
  if t=='section' and d.get('id'): self.sections.append(d['id'])
  if self.innav and t=='a' and d.get('href','').startswith('#'): self.nav.append(d['href'][1:])
 def handle_endtag(self,t):
  if t=='nav': self.innav=False
def sections(text):
 return {m.group(1):re.sub(r'<[^>]+>',' ',m.group(2)) for m in re.finditer(r'<section class="scr(?: on)?" id="([^"]+)">(.*?)(?=<section class="scr(?: on)?" id=|\Z)',text,re.S)}
# 每一项都是（架构 §13 状态子项，目标屏，原型实现词）。
# 架构新增状态时，必须新增一条含非空实现词的绑定，否则集合对撞立即转红。
STATE_BINDINGS={
's1':(('无批准权','s1',('无批准权',)),('过期','s1',('过期',)),('代际冲突','s1',('代际冲突',)),('空队列','s1',('空队列',))),
's2':(('starved','s2',('starved',)),('无人愿接','s2',('无人愿接',)),('gate pending','s2',('待',)),('gate fail','s2',('FAIL',))),
's8/s8r/s8h':(('`s8`:缺第二签','s8',('两签',)),('`s8`:闸债务摘要/链接','s8',('REVIEW-STATE.md',)),('`s8h`:运行态依赖不可用','s8h',('依赖不可用',)),('`s8h`:计数器未埋','s8h',('计数器未埋',))),
's4/a5/a6':(('缺面','s4+a5+a6',('缺「预计数据」',)),('锚悬空','s4+a5+a6',('无锚',)),('门未过','s4+a5+a6',('硬门',))),
's3':(('无可见结果（不显示被过滤对象/数量）','s3',('无可见结果',)),('suspect','s3',('suspect',))),
'b1':(('规则版本缺失','b1',('规则版本缺失',)),('规则版本冲突','b1',('规则冲突',))),
'b2':(('数据截止时刻','b2',('数据截止',)),('部分项目缺数','b2',('部分项目缺数',))),
'b3':(('未达标','b3',('未达标',)),('负测缺失','b3',('负测缺失',)),('旁路命中','b3',('旁路',))),
's5':(('无可见性','s5',('无可见性',)),('待批','s5',('待批',)),('迟到 ACK','s5',('迟到',))),
's6/a1/a4':(('无 principal 绑定','s6+a1+a4',('无 principal 绑定',)),('资源不足','s6+a1+a4',('资源不足',)),('环境不可信','s6+a1+a4',('环境不可信',))),
'a2':(('信封缺字段','a2',('缺任一字段',)),('已有 in_progress','a2',('in_progress',))),
's7':(('gated','s7',('触门',)),('quota exhausted','s7',('阈值',)),('digest pending','s7',('digest',))),
'a8/a3':(('session fenced','a8+a3',('session fenced',)),('内容拒收','a8+a3',('内容拒收',))),
'a9':(('本地认证失败','a9',('本地认证失败',)),('effect 不在 allowlist','a9',('effect 不在 allowlist',))),
's9/a7':(('弱渠道','s9+a7',('弱渠道',)),('challenge 过期','s9+a7',('challenge 过期',)),('rebind 冲突','s9+a7',('rebind 冲突',))),
's0':(('状态源不可达','s0',('状态源不可达',)),('状态未同步','s0',('状态未同步',)),('未过门时显示阻断','s0',('未过门时显示阻断',))),
}
FORBIDDEN=('生命周期仍为「草案」','两项生效前置未闭','草案 · 未评审','本原型属 S6','不砍 view 页','请业主一句话定','待裁：开放性讨论')
def arch_states(text):
 part=re.search(r'^## 13\..*?(?=^## |\Z)',text,re.M|re.S); out=[]
 if not part: return out
 for line in part.group(0).splitlines():
  if not line.startswith('|'): continue
  c=[x.strip() for x in line.strip().strip('|').split('|')]
  ids=re.findall(r'`([^`]+)`',c[0]) if len(c)==4 else []
  if ids: out.append((ids[0],c[3]))
 return out
def state_clauses(group,raw):
 if group=='s8/s8r/s8h':
  out=[]
  for segment in raw.split('；'):
   prefix,items=segment.split('：',1)
   out.extend(prefix+':'+x for x in items.split('、'))
  return out
 out=[]
 for item in re.split(r'[、；]',raw):
  if item=='gate pending/fail': out.extend(('gate pending','gate fail'))
  elif item=='规则版本缺失/冲突': out.extend(('规则版本缺失','规则版本冲突'))
  elif item=='状态源不可达/状态未同步': out.extend(('状态源不可达','状态未同步'))
  else: out.append(item)
 return out
def inspect(text,digest=True,arch_text=None,manifest_text=None,decision_text=None):
 err=[]; arch_text=ARCH.read_text() if arch_text is None else arch_text; manifest_text=MANIFEST.read_text() if manifest_text is None else manifest_text; decision_text=DECISIONS.read_text() if decision_text is None else decision_text; scan=Scan(); scan.feed(text); ids=Counter(scan.ids); blocks=re.findall(r'<!-- COVERAGE-PROTOTYPE-BEGIN -->(.*?)<!-- COVERAGE-PROTOTYPE-END -->',arch_text,re.S); arch=set(blocks[0].split()) if len(blocks)==1 else set()
 if not scan.nav or set(scan.nav)!=set(scan.sections) or set(scan.nav)!=arch: err.append('nav/section/architecture sets differ or empty')
 if len(scan.nav)!=len(set(scan.nav)) or len(scan.sections)!=len(set(scan.sections)) or any(v>1 for v in ids.values()): err.append('duplicate id')
 versions=(
  '<title>DingWei 平台原型 v1.50' in text,
  re.search(r'<h1>DingWei 平台原型 .*?v1\.50',text) is not None,
  re.search(r'class="anchor"[^>]*>原型 <b>v1\.50</b>',text) is not None,
 )
 if not all(versions): err.append('version mismatch')
 state_rows=arch_states(arch_text); row_counts=Counter(k for k,_ in state_rows)
 expected_groups=set(STATE_BINDINGS); actual_groups=set(row_counts)
 if actual_groups!=expected_groups or any(n!=1 for n in row_counts.values()):
  err.append('architecture state rows mismatch expected='+str(sorted(expected_groups))+' actual='+str(sorted(row_counts.items())))
 if '@media (max-width:720px)' not in text: err.append('narrow-screen contract missing')
 if not all(x in text for x in ('aria-label="操作者视角"','aria-label="项目筛选"')): err.append('accessible names missing')
 for x in FORBIDDEN:
  if x in text: err.append('stale:'+x)
 if {f'PROTO-DEC-0{i}' for i in range(1,5)} != set(re.findall(r'^\| (PROTO-DEC-\d{2}) \|',decision_text,re.M)): err.append('decision ledger mismatch')
 sec=sections(text)
 for group,raw in state_rows:
  actual_clauses=Counter(state_clauses(group,raw)); expected_clauses=Counter(x[0] for x in STATE_BINDINGS.get(group,()))
  if actual_clauses!=expected_clauses:
   err.append(f'architecture state clauses mismatch {group} expected={sorted(expected_clauses.elements())} actual={sorted(actual_clauses.elements())}')
 for group,bindings in STATE_BINDINGS.items():
  for clause,target,words in bindings:
   if not words: err.append(f'empty implementation mapping {group}:{clause}'); continue
   body=' '.join(sec.get(x,'') for x in target.split('+'))
   for w in words:
    if w not in body: err.append(f'missing state {group}:{clause}->{target}:{w}')
 if not (text.count('见对应合同')==2 and text.count('见评审登记')==2 and text.count('状态不在原型内复制')==2): err.append('spec status pointer policy mismatch')
 c=re.search(r'路径 <code>specs/sp4/spec\.md</code> · commit <code>([0-9a-f]{40})</code> · sha256 <code>([0-9a-f]{64})</code> · (\d+) 行 / (\d+) 字节',text)
 if not c: err.append('A5 coordinate malformed')
 else:
  try: data=subprocess.check_output(['git','show',c.group(1)+':specs/sp4/spec.md'],cwd=ROOT,stderr=subprocess.DEVNULL)
  except subprocess.CalledProcessError: data=b''; err.append('A5 commit/path unresolved')
  if hashlib.sha256(data).hexdigest()!=c.group(2) or len(data.decode().splitlines())!=int(c.group(3)) or len(data)!=int(c.group(4)): err.append('A5 coordinate mismatch')
 if digest:
  rows=[x.split('\t') for x in manifest_text.splitlines() if x.startswith('docs/dingwei-prototype.html\t')]
  if len(rows)!=1 or hashlib.sha256(text.encode()).hexdigest()!=rows[0][1]: err.append('manifest digest mismatch')
  arch_input=re.findall(r'^\| 原型 v1\.50 \| sha256 `([0-9a-f]{64})` \|',arch_text,re.M)
  if len(arch_input)!=1 or hashlib.sha256(text.encode()).hexdigest()!=arch_input[0]: err.append('architecture prototype digest mismatch')
 return err
def selftest():
 base=PROTO.read_text(); arch=ARCH.read_text(); manifest=MANIFEST.read_text(); decisions=DECISIONS.read_text(); cases=[('nav','<a href="#s1"','<a href="#missing"'),('id','id="s2"','id="s1"'),('state','空队列','队列正常'),('stale','状态不在原型内复制','生命周期仍为「草案」'),('pointer','见评审登记','已收口'),('anchor','ccccd4f540d26313eda3098d115d74de746b1a89','4a91c02f00000000000000000000000000000000'),('coordinate','e24c0e93f6c8eacc6a40f1ac4496a105e6b6f96a7787449635b954f42f744b3d','0'*64),('version','<b>v1.50</b>','<b>v1.49</b>'),('responsive','@media (max-width:720px)','@media (max-width:719px)'),('accessible','aria-label="项目筛选"','title="项目筛选"')]; bad=0
 for name,a,b in cases:
  assert a in base; t=base.replace(a,b,1); ok=bool(inspect(t,False,arch,manifest,decisions)); print(('✓' if ok else '✗')+' '+name); bad|=not ok
 arch_cases=(('s3-policy','无可见结果（不显示被过滤对象/数量）、suspect','过滤拒绝、suspect'),('s3-added-state','、suspect','、suspect、检索超时'),('s8-policy','闸债务摘要/链接','闸债务'),('s0-policy','状态源不可达/状态未同步','状态正常'))
 for name,a,b in arch_cases:
  assert a in arch; t=arch.replace(a,b,1); ok=bool(inspect(base,False,t,manifest,decisions)); print(('✓' if ok else '✗')+' '+name); bad|=not ok
 bad_manifest=manifest.replace(re.search(r'^docs/dingwei-prototype\.html\t([0-9a-f]{64})',manifest,re.M).group(1),'0'*64,1)
 ok='manifest digest mismatch' in inspect(base,True,arch,bad_manifest,decisions); print(('✓' if ok else '✗')+' manifest-production-path'); bad|=not ok
 row='| 知识库 `s3` | KNO | search/ingest/lifecycle | 无可见结果（不显示被过滤对象/数量）、suspect |'
 assert arch.count(row)==1; duplicate_arch=arch.replace(row,row+'\n'+row,1); ok=any('architecture state rows mismatch' in x for x in inspect(base,False,duplicate_arch,manifest,decisions)); print(('✓' if ok else '✗')+' architecture-duplicate-row'); bad|=not ok
 bad_arch_digest=arch.replace(re.search(r'^\| 原型 v1\.50 \| sha256 `([0-9a-f]{64})`',arch,re.M).group(1),'0'*64,1); ok='architecture prototype digest mismatch' in inspect(base,True,bad_arch_digest,manifest,decisions); print(('✓' if ok else '✗')+' architecture-digest'); bad|=not ok
 bad_decisions=decisions.replace('PROTO-DEC-04','PROTO-DEC-99',1); ok='decision ledger mismatch' in inspect(base,False,arch,manifest,bad_decisions); print(('✓' if ok else '✗')+' decision-ledger'); bad|=not ok
 print(f'--- prototype selftest {len(cases)+len(arch_cases)+4} cases '+('PASS' if not bad else 'FAIL')); return bad
if __name__=='__main__':
 if len(sys.argv)==2 and sys.argv[1]=='--selftest': raise SystemExit(selftest())
 if len(sys.argv)!=1: raise SystemExit(2)
 e=inspect(PROTO.read_text()); [print('✗ '+x) for x in e]
 if not e:
  s=Scan(); s.feed(PROTO.read_text()); state_n=sum(len(x) for x in STATE_BINDINGS.values()); print(f'PASS: prototype nav={len(s.nav)} sections={len(s.sections)} ids={len(set(s.ids))} state_bindings={state_n} unknown=0')
  print('受检范围：现行原型字节、架构 §13 入口/失败态、A5 取件坐标、v0.2 manifest。')
  print('能力边界：不证明后端实现、权限执行、生产可用性或可访问性标准认证。')
 raise SystemExit(1 if e else 0)
