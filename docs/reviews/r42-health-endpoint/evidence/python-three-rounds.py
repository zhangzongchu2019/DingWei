import subprocess,sys
for round_number in range(1,4):
 print('PYTHON_FULL_ROUND',round_number,flush=True)
 rc=subprocess.run([sys.executable,'-B','-m','unittest','discover','-s','view-service','-p','test_*.py','-v']).returncode
 if rc:sys.exit(rc)
