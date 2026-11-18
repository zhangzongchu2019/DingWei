from pathlib import Path
import subprocess,json,re,hashlib,gzip,shutil
A=Path(__file__).parent;T=Path('/data/ai-dev-data/r61-integration');D=T/'docs/reviews/r61-integration-stage2-n3';E=D/'evidence';S='4581b34ac1e4b9973d12c559cf6ac7772107bebf';B='c430c3c65f0f8d9b669c8bef24f4194871f11283'
def git(*a):return subprocess.check_output(['git',*a],cwd=T).decode().strip()
def sha(b):return hashlib.sha256(b).hexdigest()
assert not git('status','--porcelain');acc=json.loads((A/'gate-acceptance.json').read_text());assert acc['accepted'] and acc['matching_baselines_complete'];assert json.loads((A/'z3-admissibility-audit.json').read_text())['passed'];cr=json.loads((A/'compat-results.json').read_text());assert len(cr)==3 and all(r['rc']==0 for r in cr)
E.mkdir(parents=True);(E/'raw').mkdir()
for n in ['premerge.json','merge.json','precommit-audit.json','initial-go-assessment.json','postgres-startup-path.json','history-specific-verdicts.json','invariants.json','environment.json','env.json','gates.json','z3-followup-gates.json','z3-qualification.json','gate-acceptance.json','matching-baselines.json','z3-admissibility-audit.json','audit_z3.py','run_matching_baselines.py','compat-results.json','compat-runs.json','binary-shas.json','integrated-hub-go-version-m.txt','browser-receipt.txt','run_gates.py','run_compat_after_z3.py','helper_process.py','compat_real.py']:
 shutil.copy2(A/n,E/n)
rows=json.loads((A/'gates.json').read_text())+json.loads((A/'z3-followup-gates.json').read_text());parsed=[];skips=[];pg=[]
for r in rows:
 lines=(A/(r['name']+'.log')).read_text().splitlines();s='\n'.join(lines);x=dict(r);x.update(top_pass=len(re.findall(r'^--- PASS:',s,re.M)),all_pass=len(re.findall(r'^\s*--- PASS:',s,re.M)),top_skip=len(re.findall(r'^--- SKIP:',s,re.M)),all_skip=len(re.findall(r'^\s*--- SKIP:',s,re.M)),failures=re.findall(r'^\s*--- FAIL: (.*)$',s,re.M),interrupted=[{'line':i+1,'text':l}for i,l in enumerate(lines)if 'interrupted (9)'in l],data_race_lines=[l for l in lines if 'DATA RACE'in l],packages=[l for l in lines if re.match(r'^(ok|FAIL)\s',l)]);parsed.append(x)
 for i,l in enumerate(lines):
  m=re.match(r'^\s*--- SKIP: ([^ ]+)',l)
  if m:skips.append({'gate':r['name'],'name':m.group(1),'line':i+1,'condition_context':lines[max(0,i-4):i]})
  m=re.match(r'^(test_.*) \.\.\. skipped (.*)$',l)
  if m:skips.append({'gate':r['name'],'name':m.group(1),'condition':m.group(2),'line':i+1})
  if 'server_version_num='in l:pg.append({'gate':r['name'],'line':i+1,'text':l})
(E/'parsed-gates.json').write_text(json.dumps(parsed,ensure_ascii=False,indent=2)+'\n');(E/'SKIPS.json').write_text(json.dumps(skips,ensure_ascii=False,indent=2)+'\n');pge={'versions':sorted(set(re.findall(r'server_version_num=(\d+)', '\n'.join(r['text']for r in pg)))),'full3_version_lines':sum(r['gate']=='m8-store-full3'for r in pg),'entries':pg,'mode':'PGstrict real pgtest temporary PostgreSQL; fresh HOME /data/ai-dev-data/r61i23h, cached18.3archive only; no externalDSN'};(E/'postgres-evidence.json').write_text(json.dumps(pge,indent=2)+'\n')
redact=re.compile(r'((?:secret|password|cookie|token)["\s]*[:=]["\s]*)[A-Za-z0-9_+/-]{16,}',re.I);raw=[];logs=list(A.glob('*.log'))+list((A/'compat').glob('*/*.log'))+list((A/'compat').glob('*/acks.jsonl'))+list((A/'compat').glob('*/accepted.json'))
for p in sorted(logs):
 b=p.read_bytes();pub,n=redact.subn(r'\1[REDACTED]',b.decode());pub,n2=re.subn(r'(?i)(Bearer\s+)[A-Za-z0-9._~+/-]{8,}',r'\1[REDACTED]',pub);n+=n2;pub=pub.encode();dest=E/'raw'/(str(p.relative_to(A)).replace('/','__')+'.gz');dest.write_bytes(gzip.compress(pub,mtime=0));raw.append({'name':str(p.relative_to(A)),'raw_sha256':sha(b),'published_sha256':sha(pub),'redactions':n,'archive_path':str(dest.relative_to(T)),'archive_sha256':sha(dest.read_bytes())})
(E/'raw-logs.json').write_text(json.dumps(raw,indent=2)+'\n')
can=(A/'canonical.log').read_text();profiles=[json.loads(l.split('HELPER_GATE_RESULT ',1)[1])for l in can.splitlines()if l.startswith('HELPER_GATE_RESULT ')];assert len(profiles)==2 and all(r['rc']==0 and r['actual']==r['expected']and r['files']==97 for r in profiles);(E/'helper-profile-results.json').write_text(json.dumps(profiles,indent=2)+'\n')
codex=can.split('HELPER_GATE_PROFILE selected=codex actual_SH_DRIVER=codex',1)[1];target='test_claude_stream_json_user_frame_boundary_counts_zero_one_two';block=codex[codex.index(target):];block=re.split(r'\n(?=test_\w+ \()',block,maxsplit=1)[0];assert re.search(r'^ok\s*$',block,re.M);(E/'codex-guide-verdict.txt').write_text('source='+S+'\nprofile=codex\n'+block+'\n')
receipt=(A/'browser-receipt.txt').read_text();suites=dict(re.findall(r'^suite\.([^.]+)\.rc=(\d+)$',receipt,re.M));assert len(suites)==48 and all(v=='0'for v in suites.values())and suites['history_provenance']=='0'
(E/'known-browser-boundaries.json').write_text(json.dumps([{'line':i+1,'text':l}for i,l in enumerate((A/'browser-full.log').read_text().splitlines())if 'KNOWN-DEFECT'in l],ensure_ascii=False,indent=2)+'\n');q=json.loads((A/'z3-qualification.json').read_text())
report=f'''# R61 第二段 N-1b＋N-3 合后交件

依据组长2026-11-18T12:57:47Z补充令。固定合后产品/测试源 `{S}`，tree `{git('rev-parse',S+'^{tree}')}`；parents `{B}`、`1a2a4160208c839c97e1b35039c7eafc6e7216e1`。N1b冻结产品ed7259fa/作者b81737d0已先按12:24令合入cd01d67a，并在c430c3c6完成仅N1b阶段交件。补充令于14:29Z注入，故保留其历史，在c430上追加N3 no-ff merge；不reset、不重写、不重复N1b。VERSION仍0.2.3.40，N1c未并入，不推main、不出发布件、不部署。

## 合并、来源与边界

合前ls-remote核N1b=b81737d0、N3=1a2a4160、集成=c430c3c6。N3冻结源9313197f8ac3e2a585cd399935d5b313084b3b1e到tip全部docs/openspec。第一父链自main206d41b2恰9个merge，先七项、N1b、N3；原文/UTC见premerge.json、merge.json、invariants.json。首次no-commit merge未附身份被Git拒绝、分支未变；随后每命令用本席身份重试成功。ort自动合并hub.go与runner，无文本冲突、无手工产品解冲突。

N1b R1/R2文件与c430逐字相同，N3八个关键文件与931逐字相同；共享hub保留queue_full协商与legacy重锚显式nil目标。独立history DB新增0078，应用库migration0077及既有迁移均不改。helper全子树、renderer-canary-sse.html（retainCanonicalTrustedProvenance）、cmd、deps、scripts/deploy、VERSION相对c430都不变。N1c25af非祖先。导入原日志有尾空白，保留已冻结原字节，产品/openspec范围check为0，见precommit-audit.json；不虚称全路径whitespace零问题。

## 新合后源完整门

所有门均固定本合并锚重新执行，不复用先前N1b-only或N3单源绿色。普通用户uid1001，Go1.22.2、锁定Python3.12.3、Node22.23.1/OpenSpec1.16.0，env白名单，fresh HOME /data/ai-dev-data/r61i23h与真实数据盘TMPDIR /data/ai-dev-data/r61i23t。PG strict＋DW_DESKTOP_INTEGRATION_PYTHON＋DW_FAILURE_NOTICE_PYTHON显式设置。真实临时PG版本{','.join(pge['versions'])}，三遍日志{pge['full3_version_lines']}行版本证据，原位置见postgres-evidence.json。

canonical BUILD_TESTS_OK helper_profiles=2 python=27 shell=2，inventory97，两个profile数量/分组与rc见helper-profile-results；codex guide指定格实际ok（独立摘录）。PASS/SKIP是日志行数（含count重复与子项），非不同场景数。跳过逐名和原条件见SKIPS.json。

| 门 | rc | 顶层PASS行 | 含子项PASS行 | SKIP行 | interrupted行 |
|---|---:|---:|---:|---:|---:|
'''
for r in parsed:report+=f"| {r['name']} | {r['rc']} | {r['top_pass']} | {r['all_pass']} | {r['all_skip']} | {len(r['interrupted'])} |\n"
if q['required']:
 report+=f'''\n初始m8/store三遍命令非绿，实际FAIL和包结果见parsed-gates/raw原日志；只因既定三格interrupted(9)适用Z-3，首轮rc1不抹除。按7e622c94d104固定五轮，{q['green_rounds']}/5全包rc0，其余仅允许名单内同机制中断，合格；独立main206目标count20对照分列。所有轮次与诊断行保留，不用单跑替代、不择优、不扩名单、不改store。\n'''
else:report+='\n初始m8/store三遍整体rc0，故Z-3资格轮次不需要、没有另外起五轮或基线对照；z3-qualification明确required=false/rounds=[]，不冒称跑过。\n'
report+='''
focused race同时覆盖ACK/queue_full/failure_notice与HistoryProvenance/SessionHistory，包rc/计数/SKIP/竞态按原日志；未跑整包race，不作扩大。浏览器48/48，新增history_provenance与原47套均实际rc0，r15三格及N2回归保留；既有KNOWN-DEFECT观察单列，不宣称修复。来源strict与最终文档strict分开保留。

## 三向真实进程兼容

三个组合都在合后源重新跑：旧Hub501e5b8a＋合后helper，新合后Hub＋真实0.2.3.39 helper(2e1e4c9d)，另有新新正例。真实shipping Hub独立进程＋Python helper独立进程，真实WS握手/recv/accept/ACK；source也走真实WS，Hub DB状态与通知行数独立查询。

| 组合 | 真实ACK | Hub状态/通知数 |
|---|---|---|
| 旧Hub501＋合后helper | busy/queue_full/retryable=true | queued/helper_busy，通知0 |
| 合后Hub＋合后helper | failed/queue_full/retryable=false | dead/queue_full，通知1 |
| 合后Hub＋真实39helper | busy/queue_full/retryable=true | queued/helper_busy，通知0 |

边界：容量1预填队列、fixture暂停模型消费，adapter为stub且暂停无关后台任务；无真实CLI/模型回合声明。真实的是运输进程、实际协商/accept/ACK代码与DB落库。每格本机loopback、独立HOME/IPC/私有DB、一次性fixture身份，无线上/预发连接、无同名在线席位，无真实凭据。私有状态/DB/凭据及测试exe不进Git，exe非发布件、未上传。命令/UTC/各source/实际ACK/DB字段见compat-runs/results；binary-shas及go version -m只作本机联测来源证明。

## 证据与交接

所有原日志私有保留；Git raw/*.gz为发布副本，测试生成secret/token样式字段脱敏，原SHA/发布SHA/gzipSHA与次数分列raw-logs.json，不把脱敏件称逐字原文。N1b导入旧清单两条obsolete路径按前件已披露，不在本段新清单混计，也不改作者冻结证据。N3既有独立PASS按组长指定621df13e保留，不代替本段作者门，也不冒称本席新做单项独立复核。

完成：依序N1b＋N3合并和合后门、真实501/39协议兼容。下一步：组长/独立复核，N1c待修订冻结后另令并入；VERSION最后一段再定。不推main、不部署。本件不是r61整体验收或发布GO。
'''
(D/'REPORT.md').write_text(report);(D/'VERIFY.py').write_bytes((A/'VERIFY.py.template').read_bytes());print('ASSEMBLED raw',len(raw),'gates',len(parsed),'SKIPrecords',len(skips),'Z3required',q['required'])
