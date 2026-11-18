from pathlib import Path
import json,re,hashlib
A=Path(__file__).parent;allow={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent','TestSessionNameReconnectKeepsNameAndReleaseReuses','TestViewMentionRoute/a_peer_without_delivery_ack_v1_is_not_sent_to','TestViewMentionRoute'};rows=[]
gates=json.loads((A/'gates.json').read_text())+json.loads((A/'z3-followup-gates.json').read_text())
for r in gates:
 if r['name']!='m8-store-full3' and not r['name'].startswith('z3-'):continue
 p=A/(r['name']+'.log');lines=p.read_text().splitlines();failure=[]
 for i,line in enumerate(lines):
  m=re.match(r'^\s*--- FAIL: ([^ ]+)',line)
  if not m:continue
  name=m.group(1);assert name in allow,(r['name'],name);root=name.split('/')[0];starts=[j for j in range(i+1)if re.match(r'^=== RUN\s+'+re.escape(root)+r'$',lines[j])];assert starts,(name,'no RUN scope');start=starts[-1];errs=[{'line':j+1,'text':lines[j]}for j in range(start,i+1)if 'interrupted (9)'in lines[j]];assert errs,(r['name'],name,'failure lacks interrupted in its own test scope');failure.append({'name':name,'fail_line':i+1,'scope_start_line':start+1,'interrupted_in_scope':errs})
 assert bool(failure)==bool(r['rc']),(r['name'],r['rc'],failure)
 rows.append({'gate':r['name'],'source':r['source'],'rc':r['rc'],'raw_sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'failures':failure,'interrupted_total_lines':sum('interrupted (9)'in l for l in lines)})
d={'passed':True,'rule':'known3 only; each actual FAIL must contain interrupted9 in own top-test RUN scope; rc0 diagnostics are counted separately, never erased','rows':rows};(A/'z3-admissibility-audit.json').write_text(json.dumps(d,ensure_ascii=False,indent=2)+'\n');print('Z3_AUDIT_OK logs',len(rows),'actual_failures',sum(len(r['failures'])for r in rows))
