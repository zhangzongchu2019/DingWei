from pathlib import Path
import subprocess,sys
lab=Path(__file__).parent
wt=Path('/data/ai-dev-data/hold-release-r24')
steps=[
 ('final-extra-probes',lab/'mutant',['go','test','-p','1','-race','-count=1','-v','-run','^TestQQQuota','./internal/nodeagent','./internal/m8','./cmd/workpulse']),
 ('canonical-final',wt,['./build.sh','--test-only']),
 ('packages-final',wt,['go','test','-p','1','-race','-json','-count=1','-timeout=45m','./internal/nodeagent','./internal/hosted','./cmd/workpulse','./internal/nodegate','./internal/seatcapacity']),
 ('m8-final',wt,['go','test','-race','-count=1','-v','-run','^TestHostedQuotaNotice','./internal/m8']),
]
for label,cwd,args in steps:
 print('START',label,flush=True)
 rc=subprocess.run([sys.executable,str(lab/'run_gate.py'),label,*args],cwd=cwd).returncode
 if rc:sys.exit(rc)
print('FINAL_LOCAL_GATES_OK',flush=True)
