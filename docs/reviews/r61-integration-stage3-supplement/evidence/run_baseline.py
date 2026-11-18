from pathlib import Path
import json,subprocess,datetime,hashlib,re
A=Path(__file__).parent;B=Path('/data/ai-dev-data/r61-stage3-base0ab');e=json.loads((A/'env.json').read_text());s=subprocess.check_output(['git','rev-parse','HEAD'],cwd=B,text=True).strip();assert s=='0ab43c630c37fdf01ce68d5c66c0db0141a682af';e['SOURCE_COMMIT']=s;cmd=['go','test','./internal/m8','-run','^TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent$','-count=20','-p','1','-timeout','30m','-v'];r={'source':s,'command':cmd,'started_utc':datetime.datetime.now(datetime.timezone.utc).isoformat()}
p=A/'baseline0ab-newsession20.log'
with p.open('wb')as f:r['rc']=subprocess.run(cmd,cwd=B,env=e,stdout=f,stderr=subprocess.STDOUT).returncode
r['ended_utc']=datetime.datetime.now(datetime.timezone.utc).isoformat();text=p.read_text();r.update(sha256=hashlib.sha256(p.read_bytes()).hexdigest(),top_pass=len(re.findall(r'^--- PASS:',text,re.M)),failures=re.findall(r'^--- FAIL: ([^ ]+)',text,re.M),interrupted_lines=sum('interrupted (9)'in l for l in text.splitlines()))
if r['rc']:assert set(r['failures'])=={'TestNewSessionNameNotifiesFeishuOnceAndReconnectFlappingStaySilent'} and r['interrupted_lines']>0 and not any(t in text for t in ['panic:','DATA RACE','test timed out'])
(A/'baseline0ab-newsession20.json').write_text(json.dumps(r,indent=2)+'\n');print(json.dumps(r,indent=2))
