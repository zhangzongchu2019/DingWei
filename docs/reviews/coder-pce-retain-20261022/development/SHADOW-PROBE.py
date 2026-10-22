"""Local admission probe: project package must not execute as pinned owner."""
import json,os,subprocess,sys,tempfile
from pathlib import Path
ROOT=Path.cwd();sys.path.insert(0,str(ROOT/'tools/sessionhelper'))
from sessionhelper import private_endpoint as ep
with tempfile.TemporaryDirectory(prefix='pcer-shadow-') as t:
 base=Path(t);base.chmod(0o700);home=base/'ch';home.mkdir(mode=0o700)
 package=base/'sessionhelper';package.mkdir(mode=0o700);marker=base/'shadow-marker'
 (package/'__init__.py').write_text('from pathlib import Path\nPath('+repr(str(marker))+').write_text("local fixture only")\n')
 os.environ['SH_UPDATE_LAUNCHER']=str(ROOT/'tools/sessionhelper/update_launcher/guard.py')
 spec=ep.Spec(str(base/'s'),'shadow-fixture',str(home),str(base),sys.executable,sys.executable)
 client=ep.Client(spec);reason=None
 try:client.ensure()
 except ep.DaemonIdentityError as error:reason=error.reason
 finally:
  for child in client._launchers:
   child.wait(timeout=3)
 print(json.dumps({'source':subprocess.check_output(['git','rev-parse','HEAD'],text=True).strip(),
                   'shadow_package_executed':marker.exists(),'admission_error':reason,
                   'scope':'isolated fixture, no native process or credentials'}))
