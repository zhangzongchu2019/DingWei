from pathlib import Path
import subprocess,json,datetime,hashlib
r=Path(__file__).resolve().parent
w=Path('/data/ai-dev-data/wt-r62-groups-baseline206')
src=Path('/data/ai-dev-data/mcp-auth-first/internal/mcpgw/auth_first_test.go')
target=w/'internal/mcpgw/auth_first_test.go'
assert subprocess.check_output(['git','status','--porcelain'],cwd=w)==b''
assert subprocess.check_output(['git','rev-parse','HEAD'],cwd=w).decode().strip()=='206d41b26dab646c0e50a111eed5a45ccf7c72f6'
e=json.loads((r/'env-anchor-canonical.json').read_text());e['SOURCE_COMMIT']='206d41b26dab646c0e50a111eed5a45ccf7c72f6';e['PYTHONPATH']=str(w/'tools/sessionhelper')
cmd=['go','test','./internal/mcpgw','-run','^(TestMCPAuthFirstThreeVersionsAndMissingHeaders|TestMCPInitializeNegotiatesSupportedVersionAndUsesItForTools)$','-count=1','-v']
target.write_bytes(src.read_bytes());start=datetime.datetime.now(datetime.timezone.utc).isoformat()
try:
 with (r/'old-product-overlay-anchor.log').open('w') as f: p=subprocess.run(cmd,cwd=w,env=e,stdout=f,stderr=subprocess.STDOUT)
finally: target.unlink()
log=(r/'old-product-overlay-anchor.log').read_bytes()
x={'base':'206d41b26dab646c0e50a111eed5a45ccf7c72f6','test_source':'ce9b7a959e828ad717b8708651c9094a421272ec','test_sha256':hashlib.sha256(src.read_bytes()).hexdigest(),'command':cmd,'start_utc':start,'end_utc':datetime.datetime.now(datetime.timezone.utc).isoformat(),'rc':p.returncode,'log_sha256':hashlib.sha256(log).hexdigest(),'compile_ok':b'[build failed]' not in log,'business_red':b'AUTH_BEFORE_PROTOCOL' in log and b'VERSION_NEGOTIATION' in log,'porcelain':subprocess.check_output(['git','status','--porcelain'],cwd=w).decode()}
(r/'old-product-overlay-anchor.json').write_text(json.dumps(x,indent=2)+'\n');print(json.dumps(x))
