import hashlib
import importlib.util
import json
import os
import platform
import sys
import subprocess
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch


ROOT = Path(__file__).resolve().parents[3]
ACTUAL_SYSTEM = platform.system()
spec = importlib.util.spec_from_file_location('mac_build_receipt', ROOT / 'scripts/record-sessionhelper-macos-build.py')
recorder = importlib.util.module_from_spec(spec)
spec.loader.exec_module(recorder)
spec = importlib.util.spec_from_file_location('source_verify_for_fixture', ROOT / 'scripts/verify-sessionhelper-source.py')
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class MacBuildTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.source = Path(self.tmp.name) / 'source'; self.source.mkdir()
        self.output = Path(self.tmp.name) / 'output'; self.output.mkdir(mode=0o700)
        self.commit = 'a' * 40
        for name in verifier.REQUIRED:
            p = self.source / name; p.parent.mkdir(parents=True,exist_ok=True)
            p.write_text('0.2.2.2\n' if name=='VERSION' else 'synthetic pinned member\n')
        (self.source/'scripts/verify-sessionhelper-source.py').write_bytes((ROOT/'scripts/verify-sessionhelper-source.py').read_bytes())
        (self.source/'START-HERE.txt').write_text('fixture only\n')
        files=[]
        for name in sorted(verifier.REQUIRED):
            b=(self.source/name).read_bytes();files.append({'path':name,'bytes':len(b),'sha256':hashlib.sha256(b).hexdigest()})
        (self.source/'SOURCE-MEMBER-PINS.json').write_text(json.dumps({'source_commit':self.commit,'version':'0.2.2.2','files':files}))
        self.metadata={'platform':'Darwin','architecture':'arm64','native_auto_notify_implemented':False,
                       'applications':[{'name':'Codex.app','version':'fixture1','build':'fixture2','signature_verified':False}]}
        for name in ('desktop-metadata-before-build.json','desktop-metadata.json'):
            (self.output/name).write_text(json.dumps(self.metadata))
        (self.output/'bin').mkdir();(self.output/'bin/sessionhelper').write_bytes(b'not an executable: synthetic test bytes')
        b=(self.output/'bin/sessionhelper').read_bytes()
        self.smoke={'status':'FROZEN_LOCAL_SMOKE_PASS','version':'0.2.2.2','bytes':len(b),'sha256':hashlib.sha256(b).hexdigest(),
                    'real_hub_or_ai_calls':0,'native_desktop_runtime':'NOT_VERIFIED',
                    'frozen_stop_hook':'LOCAL_REQUEST_AND_DEDUP_PASS'}
        self.write_smoke()
        self.patches=[patch.object(recorder.platform,'system',return_value='Darwin'),
                      patch.object(recorder.platform,'machine',return_value='arm64'),
                      patch.object(recorder.importlib.metadata,'version',return_value='fixture1')]
        for p in self.patches:p.start()

    def tearDown(self):
        for p in reversed(self.patches):p.stop()
        self.tmp.cleanup()

    def write_smoke(self):
        (self.output/'local-smoke.jsonl').write_text('synthetic non-JSON log\n'+json.dumps(self.smoke)+'\n')

    def receipt(self):
        # Native shell entry runs recorder with -B; preserve the same no-source-
        # cache contract when unit-testing its callable in the canonical runner.
        with patch.object(sys, 'dont_write_bytecode', True):
            return recorder.create_receipt(self.source,self.commit,self.output)

    def test_fixture_build_manifest_does_not_claim_native_runtime_or_signature(self):
        result=self.receipt()
        self.assertEqual(result['binary']['sha256'],self.smoke['sha256'])
        self.assertEqual(result['source_kind'],'fixed_source_archive')
        self.assertEqual(result['dependencies'],{'pyinstaller':'fixture1','websockets':'fixture1','pexpect':'fixture1'})
        self.assertFalse(result['signature_verified']);self.assertFalse(result['release_ready'])
        self.assertEqual(result['native_desktop_model_runtime'],'NOT_VERIFIED')
        self.assertFalse((self.output/'MAC-BUILD-MANIFEST.json').exists())

    def test_missing_or_wrong_packaged_stop_coverage_refuses_build_receipt(self):
        for value in (None, 'NOT_RUN', 'MODEL_EXECUTED'):
            if value is None:
                self.smoke.pop('frozen_stop_hook',None)
            else:
                self.smoke['frozen_stop_hook']=value
            self.write_smoke()
            with self.assertRaises(ValueError):self.receipt()

    def test_changed_app_or_architecture_refused(self):
        for field in ('build','architecture'):
            changed=json.loads(json.dumps(self.metadata))
            if field=='build':changed['applications'][0]['build']='updated'
            else:changed['architecture']='x86_64'
            (self.output/'desktop-metadata.json').write_text(json.dumps(changed))
            with self.assertRaises(ValueError):self.receipt()

    def test_failed_missing_or_duplicate_smoke_never_recorded_as_pass(self):
        p=self.output/'local-smoke.jsonl'
        for text in ('uncompleted\n',json.dumps(self.smoke)+'\n'+json.dumps(self.smoke)+'\n'):
            p.write_text(text)
            with self.assertRaises(ValueError):self.receipt()

    def test_wrong_binary_pin_or_side_effect_qualification_refused(self):
        self.smoke['sha256']='0'*64;self.write_smoke()
        with self.assertRaises(ValueError):self.receipt()
        self.smoke['sha256']=hashlib.sha256((self.output/'bin/sessionhelper').read_bytes()).hexdigest()
        self.smoke['real_hub_or_ai_calls']=1;self.write_smoke()
        with self.assertRaises(ValueError):self.receipt()

    def test_source_mutation_during_build_refused(self):
        (self.source/'tools/sessionhelper/sessionhelper/setup.py').write_text('changed after build\n')
        with self.assertRaises(ValueError):self.receipt()

    def test_nonmac_or_nonprivate_output_refused(self):
        with patch.object(recorder.platform,'system',return_value='Linux'):
            with self.assertRaises(ValueError):self.receipt()
        self.output.chmod(0o755)
        with self.assertRaises(ValueError):self.receipt()

    @unittest.skipUnless(ACTUAL_SYSTEM == 'Linux', 'Linux refusal harness only; no native Mac build here')
    def test_real_shell_preflight_stops_before_output_or_dependencies(self):
        # Enter the outer shell's native branch, then require its actual Python
        # app preflight to reject Linux before any build/dependency actions.
        for p in (ROOT/'tools/sessionhelper/sessionhelper').glob('*.py'):
            (self.source/'tools/sessionhelper/sessionhelper'/p.name).write_bytes(p.read_bytes())
        build='tools/sessionhelper/deploy/macos/build-desktop-candidate.sh'
        (self.source/build).write_bytes((ROOT/build).read_bytes())
        (self.source/'tools/sessionhelper/requirements.txt').write_text('')
        files=[]
        for p in sorted(self.source.rglob('*')):
            if p.is_file() and p.name not in ('SOURCE-MEMBER-PINS.json','START-HERE.txt'):
                b=p.read_bytes();files.append({'path':p.relative_to(self.source).as_posix(),'bytes':len(b),'sha256':hashlib.sha256(b).hexdigest()})
        (self.source/'SOURCE-MEMBER-PINS.json').write_text(json.dumps({'source_commit':self.commit,'version':'0.2.2.2','files':files}))
        shim=Path(self.tmp.name)/'commands';shim.mkdir()
        (shim/'uname').write_text('#!/bin/sh\nprintf "Darwin\\n"\n');(shim/'uname').chmod(0o700)
        target=Path(self.tmp.name)/'never-created'
        env={k:v for k,v in os.environ.items() if not k.startswith(('SH_','DINGWEI_','CODEX_','CLAUDE_','OPENCODE_','PYTHON'))}
        env['PATH']=str(shim)+os.pathsep+os.environ.get('PATH','')
        result=subprocess.run(['sh',str(self.source/build),str(target)],env=env,text=True,capture_output=True,timeout=10)
        self.assertNotEqual(result.returncode,0)
        self.assertIn('桌面应用预检未通过',result.stderr)
        self.assertFalse(target.exists())


if __name__=='__main__':unittest.main()
