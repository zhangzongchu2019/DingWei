from pathlib import Path
import os,time,json,datetime,subprocess
A=Path(__file__).parent;root=json.loads((A/'shipping-go-runner.json').read_text())['pid']
for _ in range(1800):
 if not Path('/proc',str(root)).exists():break
 parents={}
 for p in Path('/proc').iterdir():
  if p.name.isdigit():
   try:
    st=(p/'stat').read_text();parents[int(p.name)]=int(st[st.rfind(')')+2:].split()[1])
   except(OSError,ValueError,IndexError):pass
 for pid in parents:
  x=pid
  for depth in range(30):
   if x==root:break
   x=parents.get(x,0)
   if x==0:break
  if x!=root:continue
  try:exe=os.readlink('/proc/'+str(pid)+'/exe')
  except OSError:continue
  if exe.endswith('/bin/postgres'):
   result={'gates_completed_at_capture':1 if (A/'go-count3-shipping.result.json').exists() else 0,'utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'runner':root,'pid':pid,'exe':exe,'version':subprocess.check_output([exe,'--version'],text=True).strip(),'scope':'owned temporary test PG descendant only'}
   (A/'shipping-pg-runtime.json').write_text(json.dumps(result,indent=2)+'\n');print(json.dumps(result),flush=True);raise SystemExit
 time.sleep(2)
print('owned test PG not observed before runner completion',flush=True)
