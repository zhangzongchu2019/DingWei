"""Actual frozen old claimer reads the new rollback-prepared isolated database."""
import argparse,datetime,json,os,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
p=argparse.ArgumentParser();p.add_argument('--old-tree',required=True);p.add_argument('--output',required=True);a=p.parse_args();old=Path(a.old_tree).resolve();out=Path(a.output).resolve();out.mkdir(parents=True,exist_ok=True)
with tempfile.TemporaryDirectory(prefix='m3rb-') as td:
 td=Path(td);env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','DW_','DATAD_')) and k not in ('CODEX_HOME','CLAUDE_CONFIG_DIR')};env.update(HOME=str(td/'home'),TMPDIR=str(td),TZ='UTC',M3_ROLLBACK_DB=str(td/'prepared.db'));(td/'home').mkdir();env.setdefault('GOCACHE',str(Path.home()/'.cache/go-build'));env.setdefault('GOMODCACHE',str(Path.home()/'go/pkg/mod'))
 receipts=[]
 def run(label,cmd,cwd):
  start=datetime.datetime.now(datetime.timezone.utc).isoformat();r=subprocess.run(cmd,cwd=cwd,env=env,capture_output=True,text=True);(out/(label+'.stdout')).write_text(r.stdout+r.stderr);receipts.append(dict(label=label,command=[x if not x.startswith('-overlay=') else '-overlay=<isolated>' for x in cmd],source=subprocess.check_output(['git','rev-parse','HEAD'],cwd=cwd,text=True).strip(),start=start,end=datetime.datetime.now(datetime.timezone.utc).isoformat(),rc=r.returncode));print(label,r.returncode,flush=True)
  if r.returncode:raise RuntimeError(label+' failed; see raw log')
 run('prepare',['go','test','./internal/store','-run','^TestSubAgentOfflineRollbackLegacyClaimer$','-count=1','-v'],ROOT)
 candidate=td/'old_test.go';candidate.write_text((ROOT/'tests/subagent-p1-m3/old_claimer_test.go.txt').read_text());overlay=td/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(old/'internal/store/failure_notice_test.go'):str(candidate)}}))
 run('actual-old-claimer',['go','test','-overlay='+str(overlay),'./internal/store','-run','^TestM3ActualOldClaimer$','-count=1','-v'],old)
 (out/'RESULT.json').write_text(json.dumps(receipts,ensure_ascii=False,indent=2)+'\n')
