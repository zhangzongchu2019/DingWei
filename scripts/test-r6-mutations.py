#!/usr/bin/env python3
"""R6 target mutations via Go overlays; never edits the worktree or deployments."""
import argparse,json,os,pathlib,subprocess,tempfile,sys
ROOT=pathlib.Path(__file__).resolve().parents[1]
CORE='internal/coordinatereview/service.go'
CASES=[
 ('R6-001-sha','internal/coordinatereview/model.go','c.Coordinate.Validate() != nil ||','false ||','./internal/coordinatereview','TestR6CoordinateValidationAndIndependentReads','R6_001_INVALID_ACCEPTED'),
 ('R6-001-envelope','internal/m8/coordinate_review.go','json.Marshal(current)','json.Marshal(map[string]any{"provider":current.Provider,"git_instance":current.Instance,"repository":current.Repository,"change_dir":current.ChangeDir,"commit_sha":current.Commit,"source":"injected source"})','./internal/m8','TestR6ActualReliableEnvelopeCoordinateOnlyAndNoACKCompletion','R6_001_SOURCE_IN_ACTUAL_ENVELOPE'),
 ('R6-002-agent','internal/m8/coordinate_review_access.go','a.reader.AuthorizeRead(ctx, cr.AgentActor(agent.ID), c)','a.reader.AuthorizeRead(ctx, r.AssignedBy, c)','./internal/m8','TestR6R5IndependentACLRevocationAndForeignCommits','R6_002_REVOKED_READ'),
 ('R6-003-version',CORE,'out, x = s.Reader.Read(ctx, actor, r.Coordinate, path)','r.Coordinate.Commit=strings.Repeat("b",40);out, x = s.Reader.Read(ctx, actor, r.Coordinate, path)','./internal/coordinatereview','TestR6PinnedReadResultsIdempotencyAndIndependentPeople','R6_003_HEAD_FALLBACK'),
 ('R6-003-read-proof',CORE,'q.Verdict != "UNABLE" && r.ReadStage < 4','false','./internal/coordinatereview','TestR6PinnedReadResultsIdempotencyAndIndependentPeople','R6_003_UNREAD_PASS'),
 ('R6-004-idempotent',CORE,'old.Operation == q.Operation','false','./internal/coordinatereview','TestR6PinnedReadResultsIdempotencyAndIndependentPeople','R6_004_RETRY_OVERWRITE'),
 ('R6-005-open-stop',CORE,'!q.Unread() || len(q.Notices) >= 3','false || len(q.Notices) >= 3','./internal/coordinatereview','TestR6OpenStopsOnlyOwnUnreadAndEndedStates','R6_005_ENDED_STILL_SENT'),
 ('R6-005-database-fence',CORE,'s.Store.BeginCoordinateReview(ctx)','func(context.Context)(func(),error){return func(){},nil}(ctx)','./internal/coordinatereview','TestR6AcrossServicesOpenAndSendUseSameDatabaseFence','R6_005_CROSS_SERVICE_OPEN_PASSED_SEND'),
 ('R6-005-spacing',CORE,'now.Before(q.Notices[len(q.Notices)-1].At.Add(time.Hour))','false','./internal/coordinatereview','TestR6NoticeBudgetsPersistenceSpacingAndFailures','R6_005_RECOVERY_BURST'),
 ('R6-005-event-owner','internal/store/coordinate_review.go','owner != "r6" || assignment != r.ID','false','./internal/coordinatereview','TestR6DurableEventOwnershipAndIndependentR9','R6_005_DOUBLE_OWNER'),
 ('R6-006-material',CORE,'if a.Status == specconfirm.Stale {','if false {','./internal/coordinatereview','TestR6ComparisonPolicyMaterialExemptionsForcePushAndUnknown','R6_006_NO_REVIEW_PROMPT'),
 ('R6-007-unknown','internal/specconfirm/confirmation.go','a.Reason = "待核对：原版本无法读取"','a.Status=Unchanged;a.Reason = "待核对：原版本无法读取"','./internal/coordinatereview','TestR6ComparisonPolicyMaterialExemptionsForcePushAndUnknown','R6_007_UNREADABLE_GREEN'),
 ('R6-007-recheck',CORE,'s.now().Sub(r.CheckedAt) < time.Minute','true','./internal/coordinatereview','TestR6TrackRechecksHistoryWithoutUnboundedDuplicateFacts','R6_007_HISTORY_CACHE_GREEN'),
 ('R6-008-visible',CORE,'r, e := s.Get(ctx, actor, id)','r, e := s.Store.CoordinateReview(ctx, id)','./internal/coordinatereview','TestR6PinnedReadResultsIdempotencyAndIndependentPeople','R6_008_REVOKED_VISIBLE'),
]
def main():
 p=argparse.ArgumentParser();p.add_argument('--output',type=pathlib.Path,required=True);args=p.parse_args();args.output.mkdir(parents=True,exist_ok=True)
 results=[]
 with tempfile.TemporaryDirectory(prefix='r6-mutations-') as td:
  td=pathlib.Path(td)
  env={k:v for k,v in os.environ.items() if k in ('PATH','LANG','TZ','GOCACHE','GOMODCACHE','GOPATH','NODE_PATH','PLAYWRIGHT_BROWSERS_PATH')};env['HOME']=str(td/'home');env['HUB_TEST_PG_STRICT']='1'
  for name,file,old,new,pkg,test,marker in CASES:
   text=(ROOT/file).read_text();assert text.count(old)==1,(name,'mutation anchor count',text.count(old))
   mutant=td/(name+'.go');mutant.write_text(text.replace(old,new));overlay=td/(name+'.json');overlay.write_text(json.dumps({'Replace':{str(ROOT/file):str(mutant)}}))
   cmd=['go','test','-overlay='+str(overlay),pkg,'-count=1','-run','^'+test+'$','-timeout=90s'];r=subprocess.run(cmd,cwd=ROOT,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (args.output/(name+'.log')).write_text(r.stdout);ok=r.returncode!=0 and marker in r.stdout and '[build failed]' not in r.stdout
   results.append({'mutation':name,'rc':r.returncode,'expected_assertion':marker,'matched':ok});print(json.dumps(results[-1]),flush=True)
   if not ok:break
  for mode,marker in [('html','R6_008 untrusted evidence text'),('state','R6_007 unknown not green'),('sha','R6_001 exact dispatch SHA'),('freshness','R6_008 late authorized response cannot restore revoked history')]:
   cmd=['node','view-service/coordinate-review-browser-regression.js','--mutation='+mode];r=subprocess.run(cmd,cwd=ROOT,env=env,text=True,stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
   (args.output/('browser-'+mode+'.log')).write_text(r.stdout);ok=r.returncode!=0 and marker in r.stdout
   results.append({'mutation':'browser-'+mode,'rc':r.returncode,'expected_assertion':marker,'matched':ok});print(json.dumps(results[-1]),flush=True)
 (args.output/'results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2)+'\n')
 return 0 if len(results)==len(CASES)+4 and all(r['matched'] for r in results) else 1
if __name__=='__main__':sys.exit(main())
