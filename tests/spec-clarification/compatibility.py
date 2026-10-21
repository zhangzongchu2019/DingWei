#!/usr/bin/env python3
"""Exact r33 libraries and Hub read a disposable new-thread database.

All source inputs come from local Git objects; all runtime artifacts stay local.
The old Hub is loopback/fake in an isolated environment. Its copy is disposable:
this is read compatibility evidence, never permission for old production writes.
"""
import argparse,datetime,hashlib,io,json,os,shutil,socket,sqlite3,subprocess,tarfile,tempfile,time,urllib.request
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
OLD='ab0f214496e6a5df1c71826cc978f40528923b8f'
NEW_PROBE=r'''package store
import("context";"os";"testing")
func TestClarificationCompatibilityExport(t *testing.T){
 db,now,q:=clarificationFixture(t)
 if _,e:=clarificationApply(t,db,now,BlockerActor{UserID:"pm"},q,clarificationUpdate("compatibility"));e!=nil{t.Fatal(e)}
 if _,e:=db.db.ExecContext(context.Background(),`VACUUM INTO ?`,os.Getenv("CLARIFICATION_COMPAT_DB"));e!=nil{t.Fatal(e)}
}
func TestClarificationCompatibilityMigrate(t *testing.T){
 db,e:=OpenSQLite(os.Getenv("CLARIFICATION_COMPAT_DB"));if e!=nil{t.Fatal(e)};defer db.Close()
 if e=db.Migrate(context.Background());e!=nil{t.Fatal(e)}
}
'''
OLD_PROBE=r'''package store
import("context";"os";"strings";"testing")
func TestClarificationCompatibilityOldSeed(t *testing.T){
 db,_,_:=questionFixture(t);questionCreate(t,db,timeForCompat(),"requirement_unclear")
 if _,e:=db.db.ExecContext(context.Background(),`VACUUM INTO ?`,os.Getenv("CLARIFICATION_COMPAT_DB"));e!=nil{t.Fatal(e)}
}
func TestClarificationCompatibilityOldRead(t *testing.T){
 ctx:=context.Background();db,e:=OpenSQLite(os.Getenv("CLARIFICATION_COMPAT_DB"));if e!=nil{t.Fatal(e)};defer db.Close()
 if e=db.Migrate(ctx);e!=nil{t.Fatal("CLARIFICATION_OLD_STARTUP_REJECTED_LEDGER",e)}
 spec,e:=db.ConsoleSpec(ctx,"owner-a","r32",false);if e!=nil||spec==nil{t.Fatal(e)}
 tasks,e:=db.ListConsoleTaskProposals(ctx,"r32");if e!=nil||len(tasks)!=1{t.Fatal(e)}
 var qid string;if e=db.db.QueryRow(`SELECT id FROM pilot_question ORDER BY id LIMIT 1`).Scan(&qid);e!=nil{t.Fatal(e)}
 c,e:=db.QuestionAuthorizationContext(ctx,"","",qid);if e!=nil{t.Fatal(e)}
 q,answers,_,e:=db.ReadQuestion(ctx,c,BlockerActor{UserID:"owner-a"},qid,0,func()error{return nil});if e!=nil||q.ID!=qid||len(answers)!=1||!strings.Contains(answers[0].Body,"维持原验收范围"){t.Fatal("CLARIFICATION_OLD_BODY_READ_FAILED",q.ID,len(answers),e)}
 for table,want:=range map[string]int{"spec_clarification_post":1,"spec_clarification_policy_history":1,"pilot_question_answer":1}{var n int;if e=db.db.QueryRow(`SELECT count(*) FROM `+table).Scan(&n);e!=nil||n!=want{t.Fatal("CLARIFICATION_OLD_DATA_NOT_PRESERVED",table,n,e)}}
}
'''
# Seed time matches the historical test fixture and is a test-only helper.
OLD_PROBE=OLD_PROBE.replace('"testing")','"testing";"time")').replace('func TestClarificationCompatibilityOldSeed','func timeForCompat()time.Time{return time.Date(2026,9,26,7,0,0,0,time.UTC)}\nfunc TestClarificationCompatibilityOldSeed')

def main():
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);args=p.parse_args()
 out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
 commit=subprocess.check_output(['git','-C',str(ROOT),'rev-parse',args.commit+'^{commit}'],text=True).strip()
 readings={'old_commit':OLD,'new_commit':commit,'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'scope':'local exact old/new libraries; actual old Hub loopback fake; no deployment or old production writes','commands':[]}
 def save(): (out/'COMPATIBILITY.json').write_text(json.dumps(readings,ensure_ascii=False,indent=2)+'\n')
 with tempfile.TemporaryDirectory(prefix='clar-compat-') as tmp:
  tmp=Path(tmp);private=tmp/'home';private.mkdir(mode=0o700)
  env={'PATH':'/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin','HOME':str(private),'LANG':'C.UTF-8','TZ':'UTC','GOCACHE':'/home/dev/.cache/go-build','GOMODCACHE':'/home/dev/go/pkg/mod','GOPATH':'/home/dev/go','GOTOOLCHAIN':'local','GOPROXY':'off','GOSUMDB':'off','CLARIFICATION_COMPAT_DB':str(tmp/'new.sqlite')}
  roots={}
  for label,sha,probe in [('old',OLD,OLD_PROBE),('new',commit,NEW_PROBE)]:
   root=tmp/label;root.mkdir();roots[label]=root
   raw=subprocess.check_output(['git','-C',str(ROOT),'archive',sha,'internal','cmd','view-service','tools/pgtest','go.mod','go.sum','VERSION'])
   with tarfile.open(fileobj=io.BytesIO(raw)) as tar:tar.extractall(root,filter='data')
   (root/'internal/store/clarification_compat_probe_test.go').write_text(probe)
  def run(label,test,db,allow_fail=False):
   cmd=['go','test','./internal/store','-run','^'+test+'$','-count=1','-v','-timeout=3m']
   e=dict(env,CLARIFICATION_COMPAT_DB=str(db));r=subprocess.run(cmd,cwd=roots[label],env=e,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=210)
   name=str(len(readings['commands']))+'-'+label+'-'+test
   (out/(name+'.log')).write_text(r.stdout);readings['commands'].append({'source':label,'test':test,'rc':r.returncode,'log':name+'.log'});save()
   if r.returncode and not allow_fail:raise AssertionError(name+' failed')
   return r
  db=tmp/'new.sqlite';run('new','TestClarificationCompatibilityExport',db)
  run('old','TestClarificationCompatibilityOldRead',db)
  # Inspect immutable discussion data before/after old Hub startup on a COPY.
  def discussion_state(file):
   with sqlite3.connect(file) as c:
    return {table:c.execute('SELECT * FROM '+table+' ORDER BY 1,2').fetchall() for table in ('console_spec_revision','spec_clarification_policy','spec_clarification_policy_history','spec_clarification_post','pilot_question_answer','pilot_question_change')}
  original=discussion_state(db);copy=tmp/'hub.sqlite';shutil.copy2(db,copy)
  hub=tmp/'old-hub';r=subprocess.run(['go','build','-o',str(hub),'./cmd/workpulse'],cwd=roots['old'],env=env,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,text=True,timeout=180);(out/'old-build.log').write_text(r.stdout);assert r.returncode==0,'old Hub build'
  readings['old_hub_sha256']=hashlib.sha256(hub.read_bytes()).hexdigest()
  with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
  hubenv={'PATH':env['PATH'],'HOME':str(private),'WP_DB_PATH':str(copy),'WP_DATA_DIR':str(private),'WP_ADDR':'127.0.0.1:'+str(port),'WP_INTERNAL_ADDR':'127.0.0.1:0','WP_CONSOLE_AUTH_ENABLED':'false','WP_HUB_ID':'clarification-compat-local','FEISHU_TRANSPORT':'fake','WP_SECRET_KEY':'synthetic-local-only','WP_ADMIN_INIT_PASSWORD':'synthetic-local-only','TZ':'UTC'}
  with (out/'old-hub.log').open('w') as log:
   child=subprocess.Popen([str(hub)],cwd=private,env=hubenv,stdout=log,stderr=log)
   try:
    deadline=time.monotonic()+20
    while time.monotonic()<deadline:
     if child.poll() is not None:raise AssertionError('old Hub exited')
     try:
      with urllib.request.urlopen('http://127.0.0.1:'+str(port)+'/api/specs/shared/r32',timeout=1) as response:body=json.load(response)
      if body.get('spec',{}).get('id')=='r32':break
     except OSError:pass
     time.sleep(.05)
    else:raise AssertionError('old Hub HTTP read unavailable')
    (out/'old-hub-read.json').write_text(json.dumps(body,ensure_ascii=False,indent=2)+'\n')
    readings['actual_old_hub_http_read']=True
   finally:
    child.terminate()
    try:child.wait(timeout=10)
    except subprocess.TimeoutExpired:child.kill();child.wait()
  assert discussion_state(copy)==original,'old Hub damaged immutable discussion data'
  readings['old_hub_discussion_data_preserved']=True
  # Restore the new snapshot, then prove new startup still reads it.
  restored=tmp/'restored.sqlite';shutil.copy2(db,restored);run('new','TestClarificationCompatibilityMigrate',restored)
  assert discussion_state(restored)==original,'snapshot restore changed discussion data'
  readings['snapshot_restore_preserves_threads']=True
  # A migration-ledger failure rolls its own DDL back, retaining earlier committed migrations.
  rollback=tmp/'rollback.sqlite';run('old','TestClarificationCompatibilityOldSeed',rollback)
  with sqlite3.connect(rollback) as c:c.execute("CREATE TRIGGER reject_0062 BEFORE INSERT ON schema_migrations WHEN NEW.version='0062_spec_clarification_transport.sql' BEGIN SELECT RAISE(ABORT,'injected ledger failure'); END")
  failed=run('new','TestClarificationCompatibilityMigrate',rollback,True);assert failed.returncode!=0,'migration failure accepted'
  with sqlite3.connect(rollback) as c:
   assert not c.execute("SELECT 1 FROM sqlite_master WHERE name='spec_clarification_notice'").fetchone(),'partial 0062 table'
   assert 'feishu_reply_json' not in {r[1] for r in c.execute('PRAGMA table_info(message)')},'partial 0062 column'
   assert c.execute('SELECT count(*) FROM pilot_question').fetchone()[0]==1
   c.execute('DROP TRIGGER reject_0062')
  with sqlite3.connect(rollback) as c:assert c.execute('SELECT count(*) FROM schema_migrations').fetchone()[0]==60
  run('new','TestClarificationCompatibilityMigrate',rollback)
  with sqlite3.connect(rollback) as c:
   expected=c.execute('SELECT spec_id,pm_user,updated_by,updated_at FROM pilot_question_policy ORDER BY spec_id').fetchall()
   got=c.execute('SELECT spec_id,pm_user,updated_by,updated_at FROM spec_clarification_policy ORDER BY spec_id').fetchall()
   assert got==expected and len(got)>0,'legacy explicit PM backfill differs'
   assert c.execute('SELECT count(*) FROM schema_migrations').fetchone()[0]==62
   assert c.execute("SELECT count(*) FROM spec_clarification_policy WHERE deputy_user<>''").fetchone()[0]==0
   policies=c.execute('SELECT * FROM spec_clarification_policy').fetchall()
   history=c.execute('SELECT * FROM spec_clarification_policy_history').fetchall()
  run('new','TestClarificationCompatibilityMigrate',rollback)
  with sqlite3.connect(rollback) as c:
   assert c.execute('SELECT * FROM spec_clarification_policy').fetchall()==policies
   assert c.execute('SELECT * FROM spec_clarification_policy_history').fetchall()==history
   assert c.execute('SELECT count(*) FROM schema_migrations').fetchone()[0]==62
  readings['explicit_pm_backfill_idempotent']=True
  readings['ledger_counts']={'r33':59,'after_committed_0061_and_failed_0062':60,'after_0061_0062_0063':62,'after_repeated_migrate':62}
  readings['failed_0062_ddl_rolled_back']=True
  readings['old_writes_authorized']=False
 readings['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat();save();print('CLARIFICATION_COMPATIBILITY_PASS')
if __name__=='__main__':main()
