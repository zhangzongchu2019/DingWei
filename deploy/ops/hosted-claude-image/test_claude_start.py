"""Offline launcher fixtures; never invokes Claude, helper, Docker, or real credentials."""
import importlib.util
import json
import os
import hashlib
import io
import tarfile
from pathlib import Path
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('claude_start_fixture', HERE / 'claude-start.py')
start = importlib.util.module_from_spec(spec)
spec.loader.exec_module(start)
verify_spec = importlib.util.spec_from_file_location('claude_verify_fixture', HERE / 'verify_inputs.py')
verify = importlib.util.module_from_spec(verify_spec)
verify_spec.loader.exec_module(verify)
prepare_spec = importlib.util.spec_from_file_location('claude_prepare_fixture', HERE / 'prepare_context.py')
prepare = importlib.util.module_from_spec(prepare_spec)
prepare_spec.loader.exec_module(prepare)


class ClaudeStart(unittest.TestCase):
    def test_definition_keeps_combo_unverified_and_builder_fail_closed(self):
        combo = json.loads((HERE / 'combos.json').read_text())
        self.assertEqual(combo, [{'cli': 'claude', 'llm': 'claude', 'cred_kind': 'token_file', 'verified': False}])
        dockerfile = (HERE / 'Dockerfile').read_text()
        for required in ('USER 10001:10001', 'COPY wheels /wheels',
                         'COPY native/package/ /opt/claude/node_modules/@anthropic-ai/claude-code-linux-x64/',
                         'COPY node/node-v22.23.3-linux-x64.tar.xz /tmp/node.tar.xz',
                         'dingwei.claude.version="2.1.285"'):
            self.assertIn(required, dockerfile)
        out = __import__('subprocess').run(['sh', str(HERE / 'build-image.sh')], text=True, capture_output=True)
        self.assertEqual(out.returncode, 2)
        self.assertNotIn('docker', out.stdout.lower())

    def test_package_and_native_metadata_are_pinned(self):
        lock = json.loads((HERE / 'claude-package-lock.json').read_text())
        self.assertEqual((lock['name'], lock['version']), ('@anthropic-ai/claude-code', '2.1.285'))
        self.assertEqual((lock['native']['name'], lock['native']['version'], lock['native']['platform']),
                         ('@anthropic-ai/claude-code-linux-x64', '2.1.285', 'linux-x64'))
        for part in (lock['dist'], lock['native']):
            self.assertTrue(part['integrity'].startswith('sha512-'))
            self.assertEqual(len(part['shasum']), 40)
        self.assertEqual((lock['runtime']['name'], lock['runtime']['version'], lock['runtime']['file']),
                         ('node', '22.23.3', 'node-v22.23.3-linux-x64.tar.xz'))
        self.assertEqual(len(lock['runtime']['sha256']), 64)

    def test_offline_fixture_hashes_and_missing_node_lock_fail_closed(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            blobs = [b'claude fixture', b'native fixture', b'node fixture']
            files = []
            for i, blob in enumerate(blobs):
                p = root / ('node-v22.23.3-linux-x64.tar.xz' if i == 2 else 'input' + str(i)); p.write_bytes(blob); files.append(p)
            def row(blob):
                import base64, hashlib
                return {'integrity': 'sha512-' + base64.b64encode(hashlib.sha512(blob).digest()).decode(),
                        'shasum': hashlib.sha1(blob).hexdigest()}
            lock = {'name': '@anthropic-ai/claude-code', 'version': '2.1.285', 'dist': row(blobs[0]),
                    'native': dict(name='@anthropic-ai/claude-code-linux-x64', version='2.1.285', **row(blobs[1])),
                    'runtime': dict(name='node', version='22.23.3', platform='linux-x64',
                                    file=files[2].name, sha256=hashlib.sha256(blobs[2]).hexdigest())}
            verify.verify(lock, *files)
            del lock['runtime']['sha256']
            with self.assertRaises(verify.Rejected):
                verify.verify(lock, *files)

    def test_restricted_mcp_and_json_object_credential(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            cred = root / 'account-credential'
            cred.write_text('{"opaque":"synthetic"}')
            cred.chmod(0o600)
            env = {'SH_WS_BASE': 'wss://hub.invalid', 'SH_ENROLL_TOKEN_FILE': str(root/'enroll'),
                   'SH_CRED_FILE': str(cred), 'SH_CRED_KIND': 'token_file', 'SH_CLI': 'claude',
                   'HOME': str(root/'work')}
            with mock.patch.object(start, '_shared_guard'), mock.patch.object(start, 'SEAT_UID', os.getuid()), \
                 mock.patch.object(start, 'IPC_DIR', str(root/'ipc')):
                out = start.prepare(env)
            self.assertEqual((out['SH_IPC_UPLOAD_ONLY'], out['SH_IPC_SEND_BODY_ONLY']), ('0', '1'))
            self.assertIn('--mcp-config', out['SH_CLI_CMD'])
            self.assertTrue(out['SH_ENROLL'])
            self.assertEqual(out['SH_MCP_IPC_SOCK'], out['DINGWEI_MCP_IPC_SOCK'])
            self.assertEqual(json.loads((root/'work'/'.claude'/'.credentials.json').read_text()), {'opaque': 'synthetic'})
            mcp = json.loads((root/'work'/'.claude'/'mcp.json').read_text())['mcpServers']['dingwei-send']
            self.assertEqual(mcp['env']['SH_IPC_SEND_BODY_ONLY'], '1')
            self.assertNotIn('file', json.dumps(mcp))

    def test_rejects_non_object_or_wrong_cli(self):
        for raw, cli in (('[]', 'claude'), ('{}', 'codex')):
            with self.subTest(raw=raw, cli=cli), tempfile.TemporaryDirectory() as tmp:
                root = Path(tmp); cred = root/'credential'; cred.write_text(raw); cred.chmod(0o600)
                env = {'SH_WS_BASE': 'wss://hub.invalid', 'SH_ENROLL_TOKEN_FILE': str(root/'enroll'),
                       'SH_CRED_FILE': str(cred), 'SH_CRED_KIND': 'token_file', 'SH_CLI': cli, 'HOME': str(root/'work')}
                with mock.patch.object(start, '_shared_guard'), mock.patch.object(start, 'SEAT_UID', os.getuid()), self.assertRaises(SystemExit):
                    start.prepare(env)

class ClaudePackager(unittest.TestCase):
    """Real H3 producer, synthetic Git source/deps; no mocked content validators."""
    def setUp(self):
        import base64
        import subprocess
        import zipfile
        self.tmp = tempfile.TemporaryDirectory(prefix='claude-packager-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.repo = self.root/'repo'; self.repo.mkdir()
        self.git('init', '-q')
        def put(name, data):
            path = self.repo/name; path.parent.mkdir(parents=True, exist_ok=True); path.write_bytes(data)
        def tar_blob(data):
            out = io.BytesIO()
            with tarfile.open(fileobj=out, mode='w:gz') as archive:
                info = tarfile.TarInfo('package/bin/tool'); info.mode=0o755; info.size=len(data)
                archive.addfile(info, io.BytesIO(data))
            return out.getvalue()
        def row(data):
            return {'integrity':'sha512-'+base64.b64encode(hashlib.sha512(data).digest()).decode(), 'shasum':hashlib.sha1(data).hexdigest(), 'fileCount':1}
        wheel = io.BytesIO()
        with zipfile.ZipFile(wheel,'w') as archive:
            archive.writestr('websockets-15.0.1.dist-info/METADATA','Name: websockets\nVersion: 15.0.1\n')
        wheel_blob = wheel.getvalue()
        self.runtime_lock = ('websockets==15.0.1 --hash=sha256:'+hashlib.sha256(wheel_blob).hexdigest()+'\n').encode()
        common = HERE.parent/'hosted-image'
        for name in ['Dockerfile','start.py','codex.py','dingwei-file','verify_runtime.py','dockerignore']:
            put('deploy/ops/hosted-image/'+name,(common/name).read_bytes())
        helper = 'tools/sessionhelper/'
        for name, data in {'sessionhelper/__init__.py':b'__version__ = "0.2.3.4"\n', 'sessionhelper/__main__.py':b'# inert factory\n','sessionhelper/mcp_shim.py':b'# inert shim\n','update_launcher/guard.py':b'# inert guard\n','update_launcher/launcher_main.py':b'# inert root\n','update_launcher/mcp_forwarder.py':b'# inert forwarder\n','bootstrap.py':b'# inert bootstrap\n', 'requirements-runtime.lock':self.runtime_lock, 'package_release.py':(HERE.parents[2]/'tools/sessionhelper/package_release.py').read_bytes()}.items():
            put(helper+name,data)
        self.wheels = self.root/'wheels';self.wheels.mkdir();(self.wheels/'websockets-15.0.1-py3-none-any.whl').write_bytes(wheel_blob)
        self.deb_dirs=[]
        for folder in ['runtime-debs','security-debs']:
            directory=self.root/folder;directory.mkdir();data=('synthetic '+folder).encode();(directory/'fixture.deb').write_bytes(data)
            put('deploy/ops/hosted-image/'+folder+'-lock.json', json.dumps([{'file':'fixture.deb','sha256':hashlib.sha256(data).hexdigest()}]).encode());self.deb_dirs.append(directory)
        h3_tar=tar_blob(b'synthetic Codex base -- do not execute');self.h3_tar=self.root/'codex.tgz';self.h3_tar.write_bytes(h3_tar)
        put('deploy/ops/hosted-image/codex-package-lock.json',json.dumps({'dist':row(h3_tar)}).encode())
        self.commit();anchor=self.git('rev-parse','HEAD');tree=self.git('rev-parse','HEAD^{tree}')
        spec=importlib.util.spec_from_file_location('actual_h3_packager_fixture',common/'prepare_context.py');self.h3=importlib.util.module_from_spec(spec);spec.loader.exec_module(self.h3)
        self.base=self.root/'base'
        # Only replace the literal production lock SHA with this synthetic lock's
        # SHA. Git, release-file mapping, wheel/deb/tar and receipt logic are real.
        with mock.patch.object(self.h3,'LOCK_SHA',hashlib.sha256(self.runtime_lock).hexdigest()):
            self.h3.prepare(self.repo,anchor,self.wheels,self.h3_tar,*self.deb_dirs,self.base)
        self.cli=self.root/'claude.tgz';self.native=self.root/'claude-native.tgz';self.node=self.root/'node-v22.23.3-linux-x64.tar.xz'
        self.cli.write_bytes(tar_blob(b'synthetic Claude'));self.native.write_bytes(tar_blob(b'synthetic Claude native'));self.node.write_bytes(b'synthetic Node -- do not execute')
        npm={'name':'@anthropic-ai/claude-code','version':'2.1.285','dist':row(self.cli.read_bytes()),'native':dict(name='@anthropic-ai/claude-code-linux-x64',version='2.1.285',**row(self.native.read_bytes())),'runtime':{'name':'node','version':'22.23.3','platform':'linux-x64','file':self.node.name,'sha256':hashlib.sha256(self.node.read_bytes()).hexdigest()}}
        self.lock=self.root/'claude-lock.json';self.lock.write_text(json.dumps(npm))
        for name in ['Dockerfile','claude-start.py','claude.py','dockerignore']:
            put(prepare.PREFIX+name,(HERE/name).read_bytes())
        put(prepare.PREFIX+'claude-package-lock.json',self.lock.read_bytes())
        mapping=json.loads((HERE/'context-map.json').read_text());mapping['h3']={'commit':anchor,'tree':tree,'lock_sha256':hashlib.sha256(self.runtime_lock).hexdigest()}
        put(prepare.PREFIX+'context-map.json',json.dumps(mapping).encode());self.commit();self.source=self.git('rev-parse','HEAD');self.out=self.root/'output'

    def git(self,*args):
        import subprocess
        return subprocess.check_output(['git','-c','core.hooksPath=/dev/null','-c','user.name=coder56solzzc','-c','user.email=coder56solzzc@dingwei.local','-C',str(self.repo),*args],stderr=subprocess.DEVNULL,text=True).strip()
    def commit(self):
        self.git('add','.');self.git('commit','-qm','synthetic fixture only')
    def run_prepare(self,**kwargs):
        args={'repo':self.repo,'commit':self.source};args.update(kwargs)
        prepare.prepare(self.lock,self.cli,self.native,self.node,self.base,self.out,**args)
    def rehash(self):
        receipt=json.loads((self.base/'BUILD-INPUTS.json').read_text())
        receipt['files']={p.relative_to(self.base).as_posix():hashlib.sha256(p.read_bytes()).hexdigest() for p in self.base.rglob('*') if p.is_file() and p.name!='BUILD-INPUTS.json'}
        (self.base/'BUILD-INPUTS.json').write_text(json.dumps(receipt))
    def rejected(self):
        with self.assertRaises((prepare.Rejected,OSError,ValueError)):
            self.run_prepare()
        self.assertFalse(self.out.exists())

    def test_actual_h3_shape_composes_git_claude_overlay_and_common_guard(self):
        self.run_prepare()
        for dest,src in json.loads((HERE/'context-map.json').read_text())['overlay'].items():
            self.assertEqual((self.out/dest).read_bytes(),(self.repo/src).read_bytes())
        self.assertEqual((self.out/'start.py').read_bytes(),(self.base/'start.py').read_bytes())
        self.assertFalse((self.out/'codex.py').exists());self.assertFalse((self.out/'native/package/bin/codex').exists())
        self.assertEqual((self.out/'native/package/bin/tool').read_bytes(),b'synthetic Claude native')
        self.assertFalse((self.out/'dockerignore').exists());self.assertTrue((self.out/'.dockerignore').is_file())
        receipt=json.loads((self.out/'BUILD-INPUTS.json').read_text())
        actual={p.relative_to(self.out).as_posix():p for p in self.out.rglob('*') if p.is_file() and p.name!='BUILD-INPUTS.json'}
        self.assertEqual(set(actual),set(receipt['files']))
        for name,p in actual.items():
            self.assertEqual(receipt['files'][name],{'sha256':hashlib.sha256(p.read_bytes()).hexdigest(),'mode':p.stat().st_mode&0o777,'type':'regular'})
        self.assertEqual(receipt['source']['commit'],self.source)
        self.assertEqual(receipt['h3']['commit'],json.loads((self.base/'BUILD-INPUTS.json').read_text())['commit'])

    def test_h3_git_or_locked_content_cannot_self_authorize_by_rehash(self):
        for name in ['Dockerfile','start.py','helper/factory/sessionhelper/__init__.py','helper/helper_update_guard.py','helper/requirements-runtime.lock','wheels/websockets-15.0.1-py3-none-any.whl','runtime-debs/fixture.deb','security-debs/fixture.deb']:
            with self.subTest(name=name):
                path=self.base/name;original=path.read_bytes();path.write_bytes(original+b'\nUNAPPROVED fixture\n');self.rehash()
                try:self.rejected()
                finally:
                    path.write_bytes(original);self.rehash()
                    if self.out.exists():__import__('shutil').rmtree(self.out)

    def assert_staged_copy_ignore_contract(self):
        import re
        import shlex
        patterns=(self.out/'.dockerignore').read_text().splitlines()
        def included(path):
            # Dockerignore subset used by this definition: root literals,
            # segment *, recursive **, parent-directory matching and last rule.
            paths=[path.as_posix()]+[x.as_posix() for x in path.parents if x.as_posix()!='.']
            result=True
            for raw in patterns:
                raw=raw.strip()
                if not raw or raw.startswith('#'):continue
                negate=raw.startswith('!');pattern=raw[1:] if negate else raw
                pattern=pattern.strip('/')
                self.assertNotRegex(pattern,r'[?\[\]\\]','unsupported ignore syntax in this definition model')
                regex=re.escape(pattern).replace(r'\*\*','.*').replace(r'\*','[^/]*')
                if any(re.fullmatch(regex,name) for name in paths):result=negate
            return result
        count=0
        for line in (self.out/'Dockerfile').read_text().splitlines():
            if not line.lstrip().startswith('COPY '):continue
            parts=shlex.split(line)
            if any(x.startswith('--from=') for x in parts):continue
            for source in parts[1:-1]:
                self.assertFalse(source.startswith('--'),'unexpected COPY flag in this definition')
                matches=list(self.out.glob(source.rstrip('/')))
                self.assertTrue(matches,'missing staged COPY source '+source)
                for path in matches:
                    entries=[path]+(list(path.rglob('*')) if path.is_dir() else [])
                    for entry in entries:
                        relative=entry.relative_to(self.out)
                        self.assertTrue(included(relative),'staged COPY source excluded: '+relative.as_posix())
                        for parent in relative.parents:
                            if parent.as_posix()!='.':self.assertTrue(included(parent),'COPY ancestor excluded: '+parent.as_posix())
                count+=1
        self.assertGreater(count,8,'COPY parser did not exercise the actual multi-stage definition')

    def test_staged_copy_sources_survive_actual_ignore_rules(self):
        self.run_prepare();self.assert_staged_copy_ignore_contract()
        path=self.out/'.dockerignore';original=path.read_text()
        path.write_text('\n'.join(line for line in original.splitlines() if line!='!start.py')+'\n')
        with self.assertRaisesRegex(AssertionError,'start.py'):self.assert_staged_copy_ignore_contract()
        path.write_text(original);self.assert_staged_copy_ignore_contract()

    def assert_runtime_uid_contract(self):
        import stat
        receipt=json.loads((self.out/'BUILD-INPUTS.json').read_text())
        self.assertEqual(receipt['staging_root'],{'mode':0o700,'copied_into_image':False})
        self.assertEqual(stat.S_IMODE(self.out.stat().st_mode),0o700)
        # Faithful ordinary-COPY model: copied ownership is root:root regardless
        # of the host fixture UID. UID/GID10001 therefore uses OTHER permissions.
        for directory in [p for p in self.out.rglob('*') if p.is_dir()]:
            rel=directory.relative_to(self.out).as_posix();mode=stat.S_IMODE(directory.stat().st_mode)
            self.assertEqual(mode,0o755,'runtime directory mode '+rel)
            self.assertEqual(receipt['directories'][rel],{'type':'directory','mode':mode,'copy_uid':0,'copy_gid':0})
            self.assertTrue(mode & stat.S_IXOTH,'UID10001 cannot traverse root-owned '+rel)
        for root in ['helper','claude/package','native/package']:
            for path in (self.out/root).rglob('*'):
                if path.is_file():self.assertTrue(path.stat().st_mode & stat.S_IROTH,'UID10001 cannot read root-owned '+str(path))
        for name in ['claude/package/bin/tool','native/package/bin/tool']:
            self.assertTrue((self.out/name).stat().st_mode & stat.S_IXOTH,'UID10001 cannot execute native/package fixture')
        dockerfile=(self.out/'Dockerfile').read_text()
        self.assertIn('USER 10001:10001',dockerfile)
        self.assertNotIn('--chown',dockerfile,'root-owned COPY model requires adjustment for new ownership flags')

    def test_runtime_uid10001_traversal_and_directory_receipt_under_private_umask(self):
        previous=os.umask(0o077)
        try:self.run_prepare()
        finally:os.umask(previous)
        self.assert_runtime_uid_contract()
        directory=self.out/'helper/factory/sessionhelper';directory.chmod(0o700)
        with self.assertRaises(AssertionError):self.assert_runtime_uid_contract()
        directory.chmod(0o755);self.assert_runtime_uid_contract()

    def test_archive_modes_are_readable_and_executable_for_runtime_user(self):
        for original,expected in [(0o600,0o644),(0o400,0o644),(0o700,0o755),(0o750,0o755)]:
            with self.subTest(original=oct(original)):
                blob=io.BytesIO()
                with tarfile.open(fileobj=blob,mode='w:gz') as archive:
                    item=tarfile.TarInfo('package/tool');item.mode=original;item.size=1;archive.addfile(item,io.BytesIO(b'x'))
                result=prepare.unpack_package(blob.getvalue(),'native',1)
                self.assertEqual(result['native/package/tool'][1],expected)

    def test_directory_chmod_failure_closes_descriptors_and_removes_new_output(self):
        original=prepare.os.fchmod;before=len(os.listdir('/proc/self/fd'))
        def denied(fd,mode):
            if mode==0o755:raise PermissionError('synthetic directory chmod failure')
            return original(fd,mode)
        with mock.patch.object(prepare.os,'fchmod',side_effect=denied), self.assertRaises(PermissionError):self.run_prepare()
        self.assertFalse(self.out.exists());self.assertEqual(len(os.listdir('/proc/self/fd')),before)

    def test_regular_type_does_not_allow_changed_h3_modes(self):
        for name in ['start.py','helper/factory/sessionhelper/__init__.py','wheels/websockets-15.0.1-py3-none-any.whl','runtime-debs/fixture.deb']:
            with self.subTest(name=name):
                p=self.base/name;p.chmod(0o755)
                try:self.rejected()
                finally:p.chmod(0o644)

    def test_missing_extra_or_receipt_pollution_rejected(self):
        receipt=self.base/'BUILD-INPUTS.json';original=receipt.read_bytes()
        corrupt=json.loads(original);corrupt['files']['start.py']='0'*64;receipt.write_text(json.dumps(corrupt));self.rejected();receipt.write_bytes(original)
        for field in ['commit','tree','lock_sha256']:
            with self.subTest(field=field):
                data=json.loads(original);data[field]='f'*len(data[field]);receipt.write_text(json.dumps(data));self.rejected();receipt.write_bytes(original)
        for name in ['start.py','helper/sessionhelper/mcp_shim.py']:
            p=self.base/name;data=p.read_bytes();p.unlink();self.rehash();self.rejected();p.write_bytes(data);p.chmod(0o644);self.rehash()
        for name in ['helper/unapproved-secret','extra.py']:
            p=self.base/name;p.write_text('synthetic-only');self.rehash();self.rejected();p.unlink();self.rehash()
        (self.base/'helper/empty-extra').mkdir();self.rejected();(self.base/'helper/empty-extra').rmdir()
        data=receipt.read_text();receipt.write_text(data[:-1]+',"commit":"duplicate"}');self.rejected();receipt.write_text(data)

    def test_symlinks_specials_hardlinks_and_redirected_roots_never_read(self):
        import socket
        sentinel=self.root/'synthetic-secret';sentinel.write_bytes(b'synthetic outside data, never consumed')
        p=self.base/'start.py';original=p.read_bytes()
        original_read=prepare.os.read
        def no_external_read(fd,count):
            if os.fstat(fd).st_ino == sentinel.stat().st_ino:
                self.fail('external synthetic secret must not be read')
            return original_read(fd,count)
        for kind in ['symlink','dangling','fifo','socket','hardlink','directory']:
            with self.subTest(kind=kind):
                p.unlink();sock=None
                if kind=='symlink':p.symlink_to(sentinel)
                elif kind=='dangling':p.symlink_to(self.root/'absent')
                elif kind=='fifo':os.mkfifo(p)
                elif kind=='socket':sock=socket.socket(socket.AF_UNIX);sock.bind(str(p))
                elif kind=='hardlink':os.link(sentinel,p)
                else:p.mkdir()
                with mock.patch.object(prepare.os,'read',side_effect=no_external_read):
                    self.rejected()
                if sock:sock.close()
                if kind=='directory':p.rmdir()
                else:p.unlink()
                p.write_bytes(original);p.chmod(0o644)
        helper=self.base/'helper';saved=self.base/'saved-helper';helper.rename(saved);helper.symlink_to(saved,target_is_directory=True);self.rejected();helper.unlink();saved.rename(helper)
        alias=self.root/'base-alias';alias.symlink_to(self.base,target_is_directory=True)
        old=self.base;self.base=alias
        try:self.rejected()
        finally:self.base=old
        ancestor=self.root/'ancestor';ancestor.symlink_to(self.root,target_is_directory=True);self.base=ancestor/'base'
        try:self.rejected()
        finally:self.base=old
        for value in [sentinel, sentinel.parent]:
            link=self.root/'untrusted-link';link.symlink_to(value,target_is_directory=value.is_dir())
            with mock.patch.object(prepare.os,'read',side_effect=no_external_read), self.assertRaises((prepare.Rejected,OSError)):
                if value.is_dir():prepare.open_dir(link)
                else:prepare.regular(link)
            link.unlink()
        self.assertEqual(sentinel.read_bytes(),b'synthetic outside data, never consumed')

    def test_regular_symlink_is_rejected_before_external_read(self):
        target=self.root/'synthetic-secret';target.write_bytes(b'not a real credential')
        link=self.root/'link';link.symlink_to(target)
        original_read=prepare.os.read
        def check(fd,count):
            if os.fstat(fd).st_ino==target.stat().st_ino:
                self.fail('external synthetic secret was read')
            return original_read(fd,count)
        with mock.patch.object(prepare.os,'read',side_effect=check), self.assertRaises((prepare.Rejected,OSError)):
            prepare.regular(link)

    def test_clean_git_source_and_lock_are_required_before_external_inputs(self):
        tracked=self.repo/(prepare.PREFIX+'Dockerfile');old=tracked.read_bytes();tracked.write_bytes(b'changed')
        with mock.patch.object(prepare,'regular',side_effect=AssertionError('input read before clean-source check')):
            self.rejected()
        tracked.write_bytes(old)
        self.lock.write_bytes(self.lock.read_bytes()+b' ');self.rejected()
        self.lock.write_bytes((self.repo/(prepare.PREFIX+'claude-package-lock.json')).read_bytes())
        with self.assertRaises(prepare.Rejected):self.run_prepare(commit='a'*40)
        self.assertFalse(self.out.exists())

    def test_archive_links_traversal_and_duplicates_rejected_even_with_valid_hash(self):
        for kind in ['symlink','hardlink','fifo','duplicate','traversal','alias']:
            with self.subTest(kind=kind):
                buf=io.BytesIO()
                with tarfile.open(fileobj=buf,mode='w:gz') as archive:
                    info=tarfile.TarInfo('../outside' if kind=='traversal' else ('package/./alias' if kind=='alias' else 'package/file'))
                    info.size=1
                    if kind=='symlink':info.type=tarfile.SYMTYPE;info.linkname='/synthetic-secret'
                    elif kind=='hardlink':info.type=tarfile.LNKTYPE;info.linkname='package/other'
                    elif kind=='fifo':info.type=tarfile.FIFOTYPE
                    archive.addfile(info,io.BytesIO(b'x') if info.isfile() else None)
                    if kind=='duplicate':archive.addfile(info,io.BytesIO(b'x'))
                with self.assertRaises(prepare.Rejected):prepare.unpack_package(buf.getvalue(),'claude',2 if kind=='duplicate' else 1)

    def test_pre_docker_script_remains_closed_with_valid_composition(self):
        import subprocess
        # Stubs are traps, not clients; this subprocess never contacts an Engine.
        traps=self.root/'traps';traps.mkdir();marker=self.root/'DOCKER-WAS-CALLED'
        for name in ['docker','podman']:
            p=traps/name;p.write_text('#!/bin/sh\ntouch "'+str(marker)+'"\nexit 91\n');p.chmod(0o755)
        # Test shell control flow using the real packager and a synthetic clean
        # Git source; place only the shell and packager scripts at this layout.
        scripts=self.repo/prepare.PREFIX
        for name in ['build-image.sh','prepare_context.py','verify_inputs.py']:(scripts/name).write_bytes((HERE/name).read_bytes())
        self.commit();self.source=self.git('rev-parse','HEAD')
        env={'PATH':str(traps)+':/usr/bin:/bin','HOME':str(self.root),'PYTHONDONTWRITEBYTECODE':'1','HOSTED_BUILD_ENGINE':'unix:///isolated-fixture.sock','CLAUDE_SOURCE_COMMIT':self.source,'CLAUDE_OFFLINE_LOCK':str(self.lock),'CLAUDE_OFFLINE_TAR':str(self.cli),'CLAUDE_NATIVE_TAR':str(self.native),'CLAUDE_NODE_RUNTIME':str(self.node),'CLAUDE_BASE_CONTEXT':str(self.base),'HOSTED_BUILD_CONTEXT':str(self.out)}
        result=subprocess.run(['sh',str(scripts/'build-image.sh')],env=env,text=True,capture_output=True)
        self.assertEqual(result.returncode,2,result.stderr);self.assertIn('refusing before Docker access',result.stderr)
        self.assertTrue((self.out/'BUILD-INPUTS.json').is_file());self.assertFalse(marker.exists())
        shutil=__import__('shutil');shutil.rmtree(self.out)
        (self.base/'start.py').write_bytes(b'polluted guard');self.rehash()
        result=subprocess.run(['sh',str(scripts/'build-image.sh')],env=env,text=True,capture_output=True)
        self.assertEqual(result.returncode,2,result.stderr);self.assertIn('Claude context rejected',result.stderr);self.assertFalse(self.out.exists());self.assertFalse(marker.exists())

    def test_descriptor_budget_is_constant_and_output_alias_refused(self):
        before=len(os.listdir('/proc/self/fd'))
        for _ in range(100):
            fd=prepare.open_dir(self.base)
            try:prepare.read_at(fd,'BUILD-INPUTS.json',limit=4<<20)
            finally:os.close(fd)
        self.assertEqual(len(os.listdir('/proc/self/fd')),before)
        self.out.mkdir();(self.out/'keep').write_bytes(b'preexisting output')
        with self.assertRaises((prepare.Rejected,OSError)):self.run_prepare()
        self.assertEqual((self.out/'keep').read_bytes(),b'preexisting output');(self.out/'keep').unlink();self.out.rmdir()
        self.out.symlink_to(self.root/'missing-output')
        self.rejected();self.out.unlink()
        self.out=self.base/'nested-output';self.rejected()


if __name__ == '__main__':
    unittest.main()
