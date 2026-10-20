#!/usr/bin/env python3
"""Run old r32.1 store startup/read against a new-template disposable database.

This proves data preservation and old reads only. Old writes are not safe after
opting specs into templates; operational rollback must freeze those writes.
"""
import argparse,io,json,subprocess,tarfile,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
OLD='36afa6215f00bc39a992441ab0ff8fe81442aa3e'
NEW_PROBE=r'''package store
import("context";"os";"testing")
func TestPilotTemplateCompatibilityExport(t *testing.T){
 db,now,d,snap:=templateFixture(t)
 v:=templateSave(t,db,now,d,snap,"compat")
 v=templateConfirm(t,db,now,templateAcceptAll(t,db,now,v,snap),snap)
 templateReviewFinal(t,db,now,v)
 if _,err:=db.db.ExecContext(context.Background(),`VACUUM INTO ?`,os.Getenv("PILOT_COMPAT_DB"));err!=nil{t.Fatal(err)}
}
'''
OLD_PROBE=r'''package store
import("context";"os";"testing")
func TestPilotTemplateCompatibilityRead(t *testing.T){
 db,err:=OpenSQLite(os.Getenv("PILOT_COMPAT_DB"));if err!=nil{t.Fatal(err)};defer db.Close()
 if err=db.Migrate(context.Background());err!=nil{t.Fatal("TEMPLATE_OLD_STARTUP_REJECTED_NEW_LEDGER",err)}
 var id string;if err=db.db.QueryRow(`SELECT confirmed_spec_id FROM pilot_template_draft WHERE state='confirmed'`).Scan(&id);err!=nil{t.Fatal(err)}
 spec,err:=db.ConsoleSpec(context.Background(),"owner-a",id,false);if err!=nil||spec.State!="final"{t.Fatal("TEMPLATE_OLD_SPEC_READ_FAILED",err)}
 for table,want:=range map[string]int{"pilot_spec_source":1,"pilot_template_draft":1,"pilot_template_revision":1,"pilot_template_receipt":3,"pilot_template_enrollment":1,"pilot_template_binding":1}{var count int;if err=db.db.QueryRow(`SELECT count(*) FROM `+table).Scan(&count);err!=nil||count!=want{t.Fatal("TEMPLATE_OLD_STARTUP_DAMAGED_DATA",table,count,err)}}
 var n int;db.db.QueryRow(`SELECT count(*) FROM schema_migrations WHERE version='0060_pilot_spec_template.sql'`).Scan(&n);if n!=1{t.Fatal("TEMPLATE_OLD_STARTUP_REMOVED_LEDGER")}
 t.Log("OLD_R321_STARTUP_READ_PRESERVES_TEMPLATE_DATA_NO_WRITE_AUTHORIZATION")
}
'''

def run(args):
 out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True);results=[]
 args.commit=subprocess.check_output(['git','-C',str(ROOT),'rev-parse',args.commit+'^{tree}'],text=True).strip()
 with tempfile.TemporaryDirectory(prefix='blocker-sqlite-compat-') as temp:
  home=Path(temp)/'home';home.mkdir(mode=0o700)
  env={'HOME':str(home),'PATH':'/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOPROXY':'off','GOSUMDB':'off','PILOT_COMPAT_DB':str(Path(temp)/'compat.sqlite')}
  for label,sha,probe,test in [('new',args.commit,NEW_PROBE,'TestPilotTemplateCompatibilityExport'),('old',OLD,OLD_PROBE,'TestPilotTemplateCompatibilityRead')]:
   root=Path(temp)/label;root.mkdir()
   archive=subprocess.check_output(['git','-C',str(ROOT),'archive',sha,'internal','tools/pgtest','go.mod','go.sum'])
   with tarfile.open(fileobj=io.BytesIO(archive)) as tar:tar.extractall(root,filter='data')
   (root/'internal/store/pilot_blocker_compat_probe_test.go').write_text(probe)
   cmd=['go','test','./internal/store','-count=1','-run','^'+test+'$','-v']
   p=subprocess.run(cmd,cwd=root,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=180)
   (out/(label+'.log')).write_text(p.stdout);results.append(dict(label=label,commit=sha,argv=cmd,rc=p.returncode))
   (out/'RESULTS.json').write_text(json.dumps(results,indent=2)+'\n');print(label,p.returncode,flush=True)
   if p.returncode:raise AssertionError(label+' compatibility failed')
if __name__=='__main__':
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);run(p.parse_args())
