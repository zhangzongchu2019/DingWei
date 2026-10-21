#!/usr/bin/env python3
"""Isolate the shared append Feishu role guard, with behavioral marker reds.

The real bound deputy/member ingress tests are independent end-to-end negatives.
They retain redundant production identity/subject checks. This lower-boundary
withdrawal tests only role != pm, without pretending non-PMs can reach that seam
through the actual ingress. No production file is changed in the delivered ref.
"""
import argparse,datetime,json,os,subprocess,tempfile
from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
def main():
 p=argparse.ArgumentParser();p.add_argument('--commit',required=True);p.add_argument('--output',required=True);a=p.parse_args()
 output=Path(a.output).resolve();output.mkdir(parents=True,exist_ok=True)
 source=subprocess.check_output(['git','rev-parse',a.commit+'^{commit}'],cwd=ROOT,text=True).strip()
 wt=Path(tempfile.mkdtemp(prefix='clar-nonpm-withdraw-'))
 subprocess.run(['git','worktree','add','--detach',str(wt),source],cwd=ROOT,check=True,stdout=subprocess.DEVNULL)
 file=wt/'internal/store/spec_clarification.go';original=file.read_text()
 old='if origin.Source != "feishu" || role != "pm" {';new='if origin.Source != "feishu" {'
 assert original.count(old)==1
 report={'source':source,'product_file':'internal/store/spec_clarification.go','old':old,'withdrawn':new,'started_at':datetime.datetime.now(datetime.timezone.utc).isoformat(),'cases':[]}
 env=dict(os.environ,TZ='UTC',GOTOOLCHAIN='local',GOPROXY='off',GOSUMDB='off')
 try:
  for role in ['deputy','technical_reader']:
   row={'role':role,'marker':'CLARIFICATION_FEISHU_NON_PM_ORIGIN_ACCEPTED_'+role,'phases':{}};report['cases'].append(row)
   command=['go','test','./internal/store','-run','^TestClarificationFeishuOriginRequiresPMAtAppendBoundary/'+role+'$','-count=1','-timeout=2m','-v'];row['command']=command
   try:
    for phase in ['before','withdrawn','restored']:
     file.write_text(original.replace(old,new) if phase=='withdrawn' else original)
     result=subprocess.run(command,cwd=wt,env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=150)
     (output/(role+'-'+phase+'.stdout')).write_bytes(result.stdout);(output/(role+'-'+phase+'.stderr')).write_bytes(result.stderr)
     marker=row['marker'].encode() in result.stdout+result.stderr
     row['phases'][phase]={'rc':result.returncode,'marker_present':marker}
     (output/'MUTATIONS.json').write_text(json.dumps(report,indent=2)+'\n')
     assert result.returncode!=0 and marker if phase=='withdrawn' else result.returncode==0,(role,phase,result.stdout,result.stderr)
   finally:file.write_text(original)
   print(role+': green / role-only designated red / restored green',flush=True)
  assert subprocess.check_output(['git','status','--porcelain'],cwd=wt,text=True)==''
  report['finished_at']=datetime.datetime.now(datetime.timezone.utc).isoformat();(output/'MUTATIONS.json').write_text(json.dumps(report,indent=2)+'\n')
 finally:
  file.write_text(original);subprocess.run(['git','worktree','remove',str(wt)],cwd=ROOT,check=True)
if __name__=='__main__':main()
