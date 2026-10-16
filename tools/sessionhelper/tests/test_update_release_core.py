"""Real Git/export/archive/process tests; no Docker/production acceptance implied."""
import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT))
from update_launcher.guard import State, UpdateError, run
import package_release as package
import bootstrap


class GuardTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.factory = self.root / 'factory'
        (self.factory / 'sessionhelper').mkdir(parents=True)
        (self.factory / 'sessionhelper' / '__init__.py').write_text('')
        self.factory_marker = self.root / 'factory-executed'
        (self.factory / 'sessionhelper' / '__main__.py').write_text(
            'from pathlib import Path;Path(' + repr(str(self.factory_marker)) + ').write_text("SYNTHETIC_FACTORY");print("SYNTHETIC_FACTORY")\n')
        self.state = State(self.root / 'state', self.factory)

    def item(self, letter):
        sha = (hex(ord(letter))[2:] * 32)[:64]
        item = dict(job_id='job-' + letter, release_id='release-' + letter,
                    agent_id='agent', owner_id='owner', version='0.2.3.' + str(ord(letter)), sha256=sha)
        slot = self.state.root / 'slots' / sha
        (slot / 'sessionhelper').mkdir(parents=True)
        (slot / 'sessionhelper' / '__init__.py').write_text('')
        (slot / 'sessionhelper' / '__main__.py').write_text('print("' + letter + '")\n')
        return item

    def run_captured(self, *args):
        output = self.root / 'child-output.log'
        real_popen = subprocess.Popen
        def captured(*pargs, **kwargs):
            return real_popen(*pargs, **dict(kwargs, stdout=log, stderr=subprocess.STDOUT))
        with output.open('w') as log, patch('update_launcher.guard.subprocess.Popen', side_effect=captured):
            result = run(*args)
        text = output.read_text()
        self.assertNotIn('首次运行请在交互终端执行', text)
        return result, text

    def test_manual_factory_has_no_job_or_deadline(self):
        selected, pending = self.state.select()
        self.assertEqual(selected, self.factory)
        self.assertIsNone(pending)
        self.assertIsNone(self.state.read()['pending'])
        rc, output = self.run_captured(self.factory, self.state.root, ['--version'])
        self.assertEqual(rc, 0, output)
        self.assertEqual(self.factory_marker.read_text(), 'SYNTHETIC_FACTORY')
        self.assertIn('SYNTHETIC_FACTORY', output)

    def test_consecutive_factory_a_b_c_and_restart_each_trial(self):
        previous = None
        for letter in 'abc':
            item = self.item(letter)
            self.assertEqual(self.state.arm(item), 'pending')
            self.assertEqual(self.state.select()[1]['job_id'], item['job_id'])
            # A new State instance models independently restarted guard and durable budget.
            fresh = State(self.state.root, self.factory)
            self.assertEqual(fresh.select()[1]['starts'], 2)
            fresh.confirm(item)
            data = fresh.read()
            self.assertEqual(data['current'], item)
            self.assertEqual(data['previous'], previous)
            self.assertEqual(fresh.arm(item), 'confirmed')
            previous = item

    def test_failed_b_retains_a_new_authorized_c_succeeds(self):
        a, b, c = [self.item(x) for x in 'abc']
        self.state.arm(a); self.state.select(); self.state.confirm(a)
        self.state.arm(b)
        self.assertEqual(self.state.select()[1]['job_id'], b['job_id'])
        self.state.fail(b)
        self.assertEqual(self.state.select()[0], self.state.slot(a))
        self.assertEqual(self.state.arm(b), 'rolled_back')
        self.state.arm(c); self.state.select(); self.state.confirm(c)
        self.assertEqual(self.state.read()['current'], c)
        self.assertEqual(self.state.read()['previous'], a)

    def test_failed_c_rolls_back_b_and_cannot_rearm(self):
        a, b, c = [self.item(x) for x in 'abc']
        for item in (a, b):
            self.state.arm(item); self.state.select(); self.state.confirm(item)
        self.state.arm(c); self.state.select(); self.state.fail(c)
        self.assertEqual(self.state.select()[0], self.state.slot(b))
        self.assertEqual(self.state.read()['previous'], a)
        self.assertEqual(self.state.arm(c), 'rolled_back')

    def test_budget_and_deadline_persist(self):
        a = self.item('a')
        now = time.time()
        self.state.arm(a, now=now)
        self.state.select(now=now)
        self.state.select(now=now)
        path, pending = State(self.state.root, self.factory).select(now=now)
        self.assertEqual(path, self.factory)
        self.assertIsNone(pending)
        self.assertEqual(self.state.read()['terminal'][a['job_id']]['stage'], 'rolled_back')
        b = self.item('b')
        self.state.arm(b, now=now)
        self.assertEqual(self.state.select(now=now + 121), (self.factory, None))

    def test_wrong_tuple_never_confirms(self):
        a = self.item('a'); self.state.arm(a); self.state.select()
        for field in ('job_id', 'release_id', 'agent_id', 'owner_id', 'version', 'sha256'):
            bad = dict(a); bad[field] = 'wrong'
            with self.assertRaisesRegex(UpdateError, 'confirmation_mismatch'):
                self.state.confirm(bad)
        self.assertIsNotNone(self.state.read()['pending'])

    def test_real_unstartable_child_rolls_back_without_candidate_import(self):
        a = self.item('a')
        (self.state.slot(a) / 'sessionhelper' / '__main__.py').write_text('raise RuntimeError("synthetic import failure")\n')
        self.state.arm(a)
        # A real Python child fails; guard survives, records rollback and exits for Docker.
        self.assertEqual(run(self.factory, self.state.root), 75)
        self.assertEqual(self.state.read()['terminal'][a['job_id']]['stage'], 'rolled_back')
        self.assertEqual(self.state.select(), (self.factory, None))

    def test_guard_preserves_original_business_workdir(self):
        observer = self.root / 'observed-cwd'
        (self.factory / 'sessionhelper/__main__.py').write_text(
            'import os;from pathlib import Path;Path(' + repr(str(observer)) + ').write_text(os.getcwd())\n')
        rc, output = self.run_captured(self.factory, self.state.root)
        self.assertEqual(rc, 0, output)
        self.assertEqual(observer.read_text(), os.getcwd())

    def test_auxiliary_uses_running_trial_without_consuming_budget(self):
        a = self.item('a'); self.state.arm(a)
        self.assertEqual(self.state.select(trial=False), (self.factory, None))
        self.state.select()
        self.assertEqual(self.state.select(trial=False), (self.state.slot(a), None))
        self.assertEqual(self.state.read()['pending']['starts'], 1)

    def test_parent_home_alias_resolves_to_same_private_state(self):
        real = self.root / 'real-home'; real.mkdir(mode=0o700)
        alias = self.root / 'home-alias'; alias.symlink_to(real, target_is_directory=True)
        state = State(alias / 'update', self.factory)
        self.assertEqual(state.root, real / 'update')
        self.assertEqual(state.select(), (self.factory, None))
        # A link at the update-root itself is still rejected.
        linked = self.root / 'linked-update'; linked.symlink_to(real / 'update', target_is_directory=True)
        with self.assertRaisesRegex(UpdateError, 'state_invalid'):
            State(linked, self.factory)

    def test_terminal_job_id_cannot_be_reused_for_different_tuple(self):
        a = self.item('a'); self.state.arm(a); self.state.select(); self.state.confirm(a)
        b = self.item('b'); b['job_id'] = a['job_id']
        with self.assertRaisesRegex(UpdateError, 'offer_mismatch'):
            self.state.arm(b)
        self.assertEqual(self.state.read()['current'], a)

    def test_actual_hung_candidate_deadline_is_independent(self):
        a = self.item('a')
        marker = self.root / 'hung-candidate-executed'
        (self.state.slot(a) / 'sessionhelper/__main__.py').write_text(
            'import time;from pathlib import Path;Path(' + repr(str(marker)) + ').write_text("SYNTHETIC_HUNG_CANDIDATE");time.sleep(60)\n')
        self.state.arm(a, now=time.time() - 119.8)
        started = time.monotonic()
        rc, output = self.run_captured(self.factory, self.state.root)
        self.assertEqual(rc, 75, output)
        self.assertEqual(marker.read_text(), 'SYNTHETIC_HUNG_CANDIDATE')
        self.assertLess(time.monotonic() - started, 3)
        self.assertEqual(self.state.read()['last_result']['reason'], 'health_timeout')

    def test_symlink_slot_rejected(self):
        a = self.item('a'); path = self.state.slot(a)
        shutil.rmtree(path); path.symlink_to(self.factory, target_is_directory=True)
        with self.assertRaisesRegex(UpdateError, 'slot_invalid'):
            self.state.arm(a)


class PackageTests(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.repo = Path(self.tmp.name) / 'repo'; self.repo.mkdir()
        self.base = self.repo / 'tools/sessionhelper'
        (self.base / 'sessionhelper').mkdir(parents=True)
        (self.base / 'update_launcher').mkdir()
        (self.base / 'sessionhelper/__init__.py').write_text('__version__ = "0.2.3.1"\n')
        (self.base / 'sessionhelper/__main__.py').write_text('print("synthetic factory")\n')
        for name in ('bootstrap.py', 'package_release.py', 'requirements-runtime.lock', 'update_launcher/guard.py', 'update_launcher/launcher_main.py', 'update_launcher/mcp_forwarder.py'):
            shutil.copyfile(ROOT / name, self.base / name)
        self.git('init', '-q')
        self.commit()

    def git(self, *args):
        return subprocess.check_output(['git', '-C', str(self.repo), '-c', 'user.name=coder56solzzc',
                                        '-c', 'user.email=coder56solzzc@dingwei.local', *args], stderr=subprocess.DEVNULL).decode().strip()

    def commit(self):
        self.git('add', '.')
        self.git('commit', '-qm', 'synthetic release')
        return self.git('rev-parse', 'HEAD')

    def test_exact_git_source_determinism_manifest_and_bootstrap_restore(self):
        sha = self.git('rev-parse', 'HEAD')
        # Even untracked input makes the source non-clean, never silently excludes a private file.
        (self.base / '.env').write_text('SYNTHETIC_PRIVATE_VALUE')
        with self.assertRaisesRegex(package.PackageError, 'source_not_clean'):
            package.build(self.repo, sha, Path(self.tmp.name) / 'dirty.tgz')
        (self.base / '.env').unlink()
        first, second = Path(self.tmp.name) / 'a.tgz', Path(self.tmp.name) / 'b.tgz'
        pin = package.build(self.repo, sha, first)
        package.build(self.repo, sha, second)
        self.assertEqual(first.read_bytes(), second.read_bytes())
        manifest, files = package.verify(first, pin['sha256'])
        self.assertEqual(manifest['commit'], sha)
        self.assertNotIn('.env', files)
        stage = Path(self.tmp.name) / 'stage'; stage.mkdir()
        for name, content in files.items():
            p = stage / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(content)
        (stage / 'MANIFEST.json').write_text(json.dumps(manifest))
        mount = Path(self.tmp.name) / 'mount'; (mount / 'sessionhelper').mkdir(parents=True)
        (mount / 'sessionhelper/__init__.py').write_text('old source')
        backup = Path(self.tmp.name) / 'backup'
        receipt = bootstrap.install(stage, mount, backup)
        self.assertTrue((mount / 'factory/sessionhelper/__main__.py').is_file())
        self.assertIn('helper_update_guard', (mount / 'sessionhelper/__main__.py').read_text())
        bootstrap.restore(receipt)
        self.assertEqual((mount / 'sessionhelper/__init__.py').read_text(), 'old source')
        self.assertFalse((mount / 'factory').exists())

    def test_installed_readonly_launcher_runs_factory_with_writable_state(self):
        artifact = Path(self.tmp.name) / 'release.tgz'
        package.build(self.repo, self.git('rev-parse', 'HEAD'), artifact)
        manifest, files = package.verify(artifact)
        stage = Path(self.tmp.name) / 'stage'; stage.mkdir()
        for name, content in files.items():
            p = stage / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(content)
        (stage / 'MANIFEST.json').write_text(json.dumps(manifest))
        mount = Path(self.tmp.name) / 'mount'; mount.mkdir()
        bootstrap.install(stage, mount, Path(self.tmp.name) / 'backup')
        for p in mount.rglob('*'):
            p.chmod(0o555 if p.is_dir() else 0o444)
        state = Path(self.tmp.name) / 'private-state'
        env = {'PATH': os.environ.get('PATH', ''), 'PYTHONPATH': str(mount),
               'PYTHONDONTWRITEBYTECODE': '1', 'SH_STATE_DIR': str(state)}
        proc = subprocess.run([sys.executable, '-B', '-m', 'sessionhelper'], cwd=mount,
                              env=env, text=True, capture_output=True, timeout=5)
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertEqual(proc.stdout.strip(), 'synthetic factory')
        self.assertTrue((state / 'update/state.lock').exists())
        self.assertFalse((mount / 'update').exists())
        # Restore local test permissions solely to let TemporaryDirectory clean itself.
        for p in mount.rglob('*'):
            p.chmod(0o700 if p.is_dir() else 0o600)

    def test_preflight_executes_real_state_probe_but_docker_is_controlled(self):
        from unittest.mock import patch
        artifact = Path(self.tmp.name) / 'release.tgz'
        package.build(self.repo, self.git('rev-parse', 'HEAD'), artifact)
        manifest, files = package.verify(artifact)
        stage = Path(self.tmp.name) / 'stage'; stage.mkdir()
        for name, content in files.items():
            p = stage / name; p.parent.mkdir(parents=True, exist_ok=True); p.write_bytes(content)
        (stage / 'MANIFEST.json').write_text(json.dumps(manifest))
        mount = Path(self.tmp.name) / 'mount'; mount.mkdir()
        state_parent = Path(self.tmp.name) / 'private'
        calls = []
        def docker(*args):
            calls.append(args)
            if args[0] == 'ps':
                return b'synthetic-container-id'
            if args[0] == 'inspect':
                return json.dumps([{'Id':'synthetic-container-id','Mounts':[{'Destination':'/opt/dingwei/sessionhelper-src',
                    'Source':str(mount),'RW':False}], 'HostConfig':{'RestartPolicy':{'Name':'unless-stopped'}},
                    'Config':{'Entrypoint':['/usr/bin/tini','--'], 'Cmd':['bash','-c',
                    'source /run/secrets/sessionhelper_env; exec /opt/dingwei/sessionhelper-venv/bin/python -m sessionhelper'],
                    'Env':['PYTHONPATH=/opt/dingwei/sessionhelper-src']}}]).encode()
            self.assertEqual(args, ('restart', 'synthetic'))
            return b'synthetic'
        def local_probe(container, script, *args):
            # Actual state ownership/mode/marker/proof/dependency Python, no Docker claim.
            env = dict(os.environ, SH_STATE_DIR=str(state_parent), PYTHONPATH='/opt/dingwei/sessionhelper-src')
            return subprocess.check_output([sys.executable, '-c', script, *args], env=env)
        with patch.object(bootstrap, 'docker', side_effect=docker), patch.object(bootstrap, 'in_container', side_effect=local_probe):
            self.assertFalse(bootstrap.preflight(stage, mount, 'synthetic')['restart_verified'])
            self.assertFalse(state_parent.exists(), 'plain inspection must not initialize state')
            verified = bootstrap.preflight(stage, mount, 'synthetic', restart_check=True)
            self.assertTrue(verified['restart_verified'])
            self.assertEqual(verified['state'], str(state_parent / 'update'))
            self.assertTrue(bootstrap.preflight(stage, mount, 'synthetic')['restart_verified'])
            self.assertEqual(sum(c[0]=='restart' for c in calls),1)
        self.assertFalse(list((state_parent/'update').glob('.bootstrap-*')))

    def test_each_forbidden_tracked_file_fails_without_echoing_values(self):
        for number, name in enumerate(('.env', 'credentials.json', 'seat.key', 'inbox.db', 'ledger.json')):
            with self.subTest(name=name):
                path = self.base / name; path.write_text('SYNTHETIC_DO_NOT_PRINT')
                sha = self.commit()
                with self.assertRaisesRegex(package.PackageError, '^forbidden_private_state$'):
                    package.build(self.repo, sha, Path(self.tmp.name) / ('bad' + str(number)))
                path.unlink(); self.commit()

    def test_literal_secret_content_rejected(self):
        (self.base / 'sessionhelper/extra.py').write_text('SH_SECRET = "SYNTHETIC_DO_NOT_PRINT"\n')
        sha = self.commit()
        with self.assertRaisesRegex(package.PackageError, '^forbidden_private_state$'):
            package.build(self.repo, sha, Path(self.tmp.name) / 'bad')

    def test_hash_mismatch_and_archive_duplicate_rejected(self):
        artifact = Path(self.tmp.name) / 'release.tgz'
        package.build(self.repo, self.git('rev-parse', 'HEAD'), artifact)
        with self.assertRaisesRegex(package.PackageError, 'sha256_mismatch'):
            package.verify(artifact, '0' * 64)
        import tarfile, io
        bad = Path(self.tmp.name) / 'duplicate.tgz'
        with tarfile.open(bad, 'w:gz') as out:
            for _ in range(2):
                info = tarfile.TarInfo('helper-release/bootstrap.py'); info.size = 1
                out.addfile(info, io.BytesIO(b'x'))
        with self.assertRaises(package.PackageError):
            package.verify(bad)


if __name__ == '__main__':
    unittest.main()
