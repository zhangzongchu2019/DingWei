"""Real isolated venv starts; no live helper, Hub, production venv or secrets."""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
import venv

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from update_launcher.cwd_import import render_hook
from update_launcher import guard


class CwdImportTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='dw-cwd-')
        self.addCleanup(self.tmp.cleanup)
        self.base = Path(self.tmp.name); self.base.chmod(0o700)
        self.venv = self.base/'venv'
        venv.EnvBuilder(with_pip=False).create(self.venv)
        self.python = str(self.venv/'bin/python')
        self.site = next((self.venv/'lib').glob('python*/site-packages'))
        self.stable = self.base/'stable'; self.stable.mkdir(mode=0o755)
        self.business = self.base/'business'; self.business.mkdir(mode=0o700)
        self.make_package(self.stable)
        self.hook = self.site/'00-dingwei-helper-cwd-import.pth'
        self.hook.write_bytes(render_hook(str(self.stable), self.python))
        self.hook.chmod(0o644)
        self.env = {'HOME': str(self.base), 'PATH': '/usr/bin:/bin', 'LANG': 'C.UTF-8',
                    'PYTHONPATH': str(self.stable), 'PYTHONDONTWRITEBYTECODE': '1',
                    'SH_CLI_CWD': str(self.business), 'CODEX_HOME': str(self.base/'codex')}

    def make_package(self, root):
        package=root/'sessionhelper';package.mkdir(mode=0o755)
        (package/'__init__.py').write_text('')
        code = ('import json,os,sys\n'
                'print(json.dumps(dict(isolated=sys.flags.isolated,cwd=os.getcwd(),'
                'prefix=sys.prefix,unbuffered=sys.stdout.write_through,argv=sys.argv[1:],codex=os.environ.get("CODEX_HOME"),'
                'workdir=os.environ.get("SH_CLI_CWD"),file=__file__)))\n')
        (package/'__main__.py').write_text(code)
        (package/'mcp_shim.py').write_text(code)

    def run_python(self, *args, env=None):
        return subprocess.run([self.python, *args], cwd=self.business,
                              env=self.env if env is None else env,
                              capture_output=True, text=True, timeout=10)

    def shadow(self, name):
        marker=self.base/(name.replace('/','-')+'.executed')
        destination=self.business/name
        destination.parent.mkdir(parents=True, exist_ok=True)
        destination.write_text('open('+repr(str(marker))+',"w").write("EXECUTED")\n')
        return marker

    def helper_output(self, *args):
        p=self.run_python('-m', 'sessionhelper', *args)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertRegex(p.stderr,r'HELPER_CWD_IMPORT_READY isolated=1 pid=[0-9]+','HOOK_READY_NOT_VISIBLE')
        return json.loads(p.stdout)

    def test_cwd_fake_package_cannot_execute(self):
        marker=self.shadow('sessionhelper/__init__.py')
        (self.business/'sessionhelper/__main__.py').write_text('')
        p=self.run_python('-m','sessionhelper')
        self.assertFalse(marker.exists(), 'CWD_PACKAGE_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(p.stdout)['isolated'],1,'CWD_ISOLATION_MISSING')

    def test_cwd_fake_json_cannot_execute(self):
        marker=self.shadow('json.py')
        p=self.run_python('-m','sessionhelper')
        self.assertFalse(marker.exists(),'CWD_JSON_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)

    def test_cwd_fake_sitecustomize_cannot_execute(self):
        marker=self.shadow('sitecustomize.py')
        p=self.run_python('-m','sessionhelper',
                          env={**self.env,'PYTHONPATH':str(self.stable)+os.pathsep+str(self.business)})
        self.assertFalse(marker.exists(),'CWD_SITECUSTOMIZE_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)

    def test_cwd_fake_runpy_cannot_execute(self):
        marker=self.shadow('runpy.py')
        p=self.run_python('-m','sessionhelper')
        self.assertFalse(marker.exists(),'CWD_RUNPY_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)

    def test_preserves_venv_arguments_workdir_and_nonpython_environment(self):
        data=self.helper_output('argument with spaces','--name','unchanged')
        self.assertEqual(data['argv'],['argument with spaces','--name','unchanged'])
        self.assertEqual(data['cwd'],str(self.business))
        self.assertEqual(data['workdir'],str(self.business))
        self.assertEqual(data['prefix'],str(self.venv))
        self.assertEqual(data['codex'],self.env['CODEX_HOME'])
        self.assertTrue(data['file'].startswith(str(self.stable)))

    def test_same_venv_nonhelper_script_c_and_module_unaffected(self):
        code='import sys;print(sys.flags.isolated)'
        script=self.business/'unrelated.py';script.write_text(code)
        (self.business/'unrelated_module.py').write_text(code)
        for args in [('-c',code),(str(script),),('-m','unrelated_module')]:
            with self.subTest(args=args):
                p=self.run_python(*args);self.assertEqual((p.returncode,p.stdout.strip()),(0,'0'),'NONHELPER_BEHAVIOR_CHANGED')
        # A helper-looking string inside -c is data, not a module launch.
        p=self.run_python('-c','import sys;print("-m sessionhelper",sys.flags.isolated)')
        self.assertEqual(p.stdout.strip(),'-m sessionhelper 0')

    def test_already_isolated_and_internal_marker_are_idempotent(self):
        isolated='import sys;sys.orig_argv=[sys.executable,"-m","sessionhelper"];'+self.hook.read_text()+'print("ISOLATED_NOOP")'
        p=self.run_python('-I','-c',isolated)
        self.assertEqual((p.returncode,p.stdout.strip()),(0,'ISOLATED_NOOP'),'ISOLATED_HOOK_REEXECUTED')
        code='import sys;sys.orig_argv=[sys.executable,"-m","sessionhelper"];sys._dw_cwd_import_reexecuted=True;'+self.hook.read_text()+'print("MARKER_NOOP")'
        p=self.run_python('-c',code)
        self.assertEqual(p.returncode,0,p.stderr);self.assertIn('MARKER_NOOP',p.stdout)

    def test_environment_marker_cannot_bypass_isolation(self):
        data=self.run_python('-m','sessionhelper',env={**self.env,'SH_CWD_IMPORT_REEXECUTED':'1'})
        self.assertEqual(data.returncode,0,data.stderr)
        self.assertEqual(json.loads(data.stdout)['isolated'],1,'FORGED_MARKER_BYPASSED_ISOLATION')

    def test_mcp_module_and_supported_python_switches_isolated(self):
        for args in [('-m','sessionhelper.mcp_shim'),('-B','-u','-m','sessionhelper')]:
            with self.subTest(args=args):
                p=self.run_python(*args);self.assertEqual(p.returncode,0,p.stderr)
                self.assertEqual(json.loads(p.stdout)['isolated'],1,'HELPER_MODULE_NOT_ISOLATED')
                if '-u' in args:self.assertTrue(json.loads(p.stdout)['unbuffered'])

    def test_bad_root_missing_package_symlink_and_mode_fail_closed_visible(self):
        marker=self.shadow('sessionhelper/__init__.py')
        for fault in ('missing','symlink','mode','interpreter'):
            with self.subTest(fault=fault):
                root=self.stable;python=self.python
                if fault=='missing':root=self.base/'absent'
                if fault=='symlink':
                    root=self.base/'alias';root.symlink_to(self.stable,target_is_directory=True)
                if fault=='mode':self.stable.chmod(0o775)
                if fault=='interpreter':python='/wrong/venv/bin/python'
                self.hook.write_bytes(render_hook(str(root),python))
                try:
                    p=self.run_python('-m','sessionhelper')
                    self.assertEqual(p.returncode,78,'BAD_BOOTSTRAP_FELL_THROUGH_'+fault)
                    self.assertIn('HELPER_CWD_IMPORT_ERROR startup_refused',p.stderr,'BOOTSTRAP_FAILURE_NOT_VISIBLE')
                    self.assertFalse(marker.exists(),'BAD_BOOTSTRAP_FELL_THROUGH_'+fault)
                finally:self.stable.chmod(0o755)

    def test_exec_error_exits_instead_of_site_swallowing_exception(self):
        code='import sys,os;sys.orig_argv=[sys.executable,"-m","sessionhelper"];os.execve=lambda *a: (_ for _ in ()).throw(OSError("PRIVATE_SENTINEL"));'+self.hook.read_text()
        p=self.run_python('-c',code)
        self.assertEqual(p.returncode,78,'EXEC_FAILURE_FELL_THROUGH')
        self.assertIn('HELPER_CWD_IMPORT_ERROR',p.stderr)
        self.assertNotIn('PRIVATE_SENTINEL',p.stderr)

    def test_hook_is_one_deterministic_file_and_rejects_relative_paths(self):
        raw=render_hook(str(self.stable),self.python)
        self.assertEqual(raw.count(b'\n'),1)
        self.assertTrue(raw.startswith(b'import '))
        self.assertEqual(raw,self.hook.read_bytes())
        for root,python in [('relative',self.python),(str(self.stable),'relative')]:
            with self.assertRaises(ValueError):render_hook(root,python)

    def test_guard_child_excludes_cwd_standard_library_shadow(self):
        marker=self.shadow('json.py')
        # Import the guard in an isolated parent, then use real guard.run/Popen.
        code=('import sys;sys.path.insert(0,sys.argv.pop(1));from update_launcher.guard import run;'
              'raise SystemExit(run(sys.argv[1],sys.argv[2],["--version"]))')
        p=self.run_python('-I','-c',code,str(ROOT),str(self.stable),str(self.base/'update'))
        self.assertFalse(marker.exists(),'GUARD_CHILD_CWD_JSON_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(p.stdout)['isolated'],1,'GUARD_CHILD_NOT_ISOLATED')

    def test_mcp_forwarder_child_excludes_cwd_standard_library_shadow(self):
        marker=self.shadow('json.py')
        mount=self.base/'mount';(mount/'sessionhelper').mkdir(parents=True)
        shutil.copyfile(ROOT/'update_launcher/mcp_forwarder.py',mount/'sessionhelper/mcp_shim.py')
        (mount/'factory').mkdir();self.make_package(mount/'factory')
        p=self.run_python(str(mount/'sessionhelper/mcp_shim.py'))
        self.assertFalse(marker.exists(),'MCP_CHILD_CWD_JSON_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(p.stdout)['isolated'],1,'MCP_CHILD_NOT_ISOLATED')

    def test_legacy_mcp_entry_child_excludes_cwd_standard_library_shadow(self):
        marker=self.shadow('json.py')
        p=self.run_python(str(ROOT/'legacy_mcp_entry.py'), '--state-dir',str(self.base/'state'),
                          '--factory-root',str(self.stable))
        self.assertFalse(marker.exists(),'LEGACY_MCP_CHILD_CWD_JSON_EXECUTED')
        self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(p.stdout)['isolated'],1,'LEGACY_MCP_CHILD_NOT_ISOLATED')

    def test_hook_is_not_a_runtime_lock_or_distribution_change(self):
        (self.stable/'requirements-runtime.lock').write_bytes((ROOT/'requirements-runtime.lock').read_bytes())
        # Read the already locked test venv's packages through a private .pth;
        # never install anything or write into the real test/runtime venv.
        dependency_site=Path(sys.prefix)/'lib'/('python'+str(sys.version_info.major)+'.'+str(sys.version_info.minor))/'site-packages'
        (self.site/'locked-test-dependencies.pth').write_text(str(dependency_site)+'\n')
        code="""import sys;sys.path.insert(0,sys.argv[1])
import json,hashlib,importlib.metadata
from pathlib import Path
from types import SimpleNamespace
from datetime import datetime,timedelta,timezone
from sessionhelper.self_update import SelfUpdater,UpdateRejected
obj=object.__new__(SelfUpdater);obj.mount=Path(sys.argv[2]);obj.agent_name='fixture';obj.allow_loopback=True
obj.cfg=SimpleNamespace(ws_base='ws://127.0.0.1:1234');obj.validate_source=lambda packet: None
meta=dict.fromkeys(('job_id','release_id','agent_id','owner_id','version'),'fixture')
meta.update(action='update_self',agent_name='fixture',sha256='a'*64,lock_sha256=sys.argv[3],
 url='http://127.0.0.1:1234/package',expires_at=(datetime.now(timezone.utc)+timedelta(seconds=60)).isoformat(),platform='linux')
try:
 obj.validate_offer({'meta':meta}); print('LOCK_ACCEPTED',obj.runtime_lock_sha256())
except UpdateRejected as e:
 print('LOCK_REJECTED',str(e))
"""
        lock=self.stable/'requirements-runtime.lock';sha=hashlib.sha256(lock.read_bytes()).hexdigest()
        payload=self.hook.read_bytes();self.hook.unlink()
        before=self.run_python('-c',code,str(ROOT),str(self.stable),sha)
        self.hook.write_bytes(payload)
        after=self.run_python('-c',code,str(ROOT),str(self.stable),sha)
        self.assertEqual(before.returncode,0,before.stderr)
        self.assertEqual(after.returncode,0,after.stderr)
        self.assertEqual(after.stdout,before.stdout,'HOOK_CHANGED_RUNTIME_LOCK')
        self.assertEqual(after.stdout.strip(),'LOCK_ACCEPTED '+sha,'HOOK_CHANGED_RUNTIME_LOCK')
        lock.write_bytes(lock.read_bytes()+b'\n')
        changed=self.run_python('-c',code,str(ROOT),str(self.stable),sha)
        self.assertEqual(changed.stdout.strip(),'LOCK_REJECTED lock_changed','LOCK_CHANGE_WAS_IGNORED')

    def test_install_remove_changes_only_the_hook_file(self):
        def others():
            return {str(p.relative_to(self.site)):(p.stat().st_mode,p.stat().st_uid,p.stat().st_gid,p.read_bytes())
                    for p in self.site.rglob('*') if p.is_file() and p!=self.hook}
        before=others();payload=self.hook.read_bytes();self.hook.unlink()
        self.assertEqual(others(),before,'HOOK_REMOVAL_TOUCHED_OTHER_FILES')
        temporary=self.hook.with_suffix('.new');temporary.write_bytes(payload);temporary.chmod(0o644)
        os.replace(temporary,self.hook)
        self.assertEqual(others(),before,'HOOK_INSTALL_TOUCHED_OTHER_FILES')
        self.assertEqual(self.hook.stat().st_mode&0o777,0o644)
        self.assertEqual(self.hook.read_bytes(),payload)
        self.helper_output()

    def test_rollback_delete_restores_original_entry_behavior(self):
        self.hook.unlink()
        p=self.run_python('-m','sessionhelper');self.assertEqual(p.returncode,0,p.stderr)
        self.assertEqual(json.loads(p.stdout)['isolated'],0)


if __name__=='__main__':unittest.main()
