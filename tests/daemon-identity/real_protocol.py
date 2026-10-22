"""Measured identity gates on exact official Codex Unix listeners, no model turns.

Binaries must already exist locally and match the prior immutable provenance.
All subprocesses are owned by this probe; no daemon discovery/control occurs.
"""
import argparse
import datetime
import hashlib
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import time
from unittest import mock

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'tools/sessionhelper'))
from sessionhelper.codexDriver import CodexProtocolDriver, UnixWebSocketJsonRpc
from sessionhelper.daemon_identity import DaemonIdentityError, OwnedDaemonIdentity, process_starttime


def main(version, binary, output):
    out = Path(output).resolve(); out.mkdir(mode=0o700, parents=True, exist_ok=False)
    binary = Path(binary).resolve(strict=True)
    provenance_path = ROOT / ('docs/reviews/coder-helper-stale-thread-followup-20261022/raw/' + version + '/PROVENANCE.json')
    provenance = json.loads(provenance_path.read_text())
    raw = binary.read_bytes(); digest = hashlib.sha256(raw).hexdigest()
    assert digest == provenance['binary_sha256'] and len(raw) == provenance['binary_bytes']
    (out / 'PROVENANCE.json').write_text(json.dumps({'version': version, 'binary_sha256': digest,
        'binary_bytes': len(raw), 'prior_provenance': str(provenance_path.relative_to(ROOT)),
        'not_from_PATH': True, 'source': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
        'inputs': {p: hashlib.sha256((ROOT / p).read_bytes()).hexdigest() for p in (
            'tools/sessionhelper/sessionhelper/daemon_identity.py',
            'tools/sessionhelper/sessionhelper/codexDriver.py', 'tests/daemon-identity/real_protocol.py')},
        'start': datetime.datetime.now(datetime.timezone.utc).isoformat()}, indent=2) + '\n')
    rows = []; cleanup = []; processes = []
    with tempfile.TemporaryDirectory(prefix='daemon-p-') as tmp:
        base = Path(tmp); home = base / 'home'; home.mkdir(mode=0o700)
        codex_home = home / '.codex'; codex_home.mkdir(mode=0o700)
        (codex_home / 'config.toml').write_text('model_provider="offline"\n[model_providers.offline]\nname="offline"\nbase_url="http://127.0.0.1:9/v1"\nwire_api="responses"\nrequires_openai_auth=false\n')
        env = {'HOME': str(home), 'CODEX_HOME': str(codex_home), 'PATH': '/usr/bin:/bin',
               'OTEL_SDK_DISABLED': 'true'}
        path = base / 'control.sock'
        def spawn(label):
            stdout = (out / (label + '.stdout')).open('wb')
            stderr = (out / (label + '.stderr')).open('wb')
            proc = subprocess.Popen([str(binary), 'app-server', '--listen', 'unix://' + str(path)],
                cwd=base, env=env, stdin=subprocess.DEVNULL, stdout=stdout, stderr=stderr)
            processes.append((proc, stdout, stderr, label))
            until = time.monotonic() + 12
            while not path.exists():
                assert proc.poll() is None, ('listener exited', version, proc.returncode)
                assert time.monotonic() < until, 'listener did not become ready'
                time.sleep(.02)
            return proc
        def stop(proc, label):
            if proc.poll() is None:
                proc.terminate()
                try: proc.wait(timeout=8)
                except subprocess.TimeoutExpired: proc.kill(); proc.wait(timeout=3)
            cleanup.append({'label': label, 'pid': proc.pid, 'reaped': proc.poll() is not None})
            if path.exists():
                assert stat.S_ISSOCK(path.lstat().st_mode)
                path.unlink()
        def driver(label, owned=None, expected=None):
            d = CodexProtocolDriver(workdir=str(base), manage_daemon=False,
                daemon_identity_check=True, daemon_expected_exe=str(expected or binary),
                owned_daemon_identity=owned, store_path=str(base / (label + '.sqlite3')))
            d._daemon_socket_path = lambda: str(path)
            return d
        def rejected(label, d, reason):
            calls = []; original = UnixWebSocketJsonRpc._send_text
            def record(rpc, text): calls.append(json.loads(text).get('method')); return original(rpc, text)
            try:
                with mock.patch.object(UnixWebSocketJsonRpc, '_send_text', record):
                    receipt = d.deliver('fixture-' + label, 'NOT_SENT_NO_MODEL')
                assert receipt.state == 'failed' and receipt.detail.startswith('session_unavailable:')
                assert 'daemon_identity_' + reason in receipt.detail
                assert not calls and not d.health() and d._thread_id == ''
                rows.append({'case': label, 'result': 'PASS', 'reason': reason,
                             'rpc_methods': calls, 'new_thread': False, 'detail': receipt.detail})
            finally: d.stop()
        try:
            first = spawn('first')
            owned = OwnedDaemonIdentity(first.pid, process_starttime(first.pid))
            d = driver('owned', owned)
            try:
                d.start(); before = d._rpc.peer_identity
                assert before.pid == first.pid and before.uid == os.getuid()
                assert before.starttime == owned.starttime and before.scope == 'owned_pid_starttime'
                d._rpc.stop(); d.start(); after = d._rpc.peer_identity
                assert after == before and d.health() and d._thread_id == ''
                rows.append({'case': 'owned-and-reconnect', 'result': 'PASS', **before.metadata(),
                             'initialized_connections': d._connection_generation, 'new_thread': False})
            finally: d.stop()
            d = driver('uid-exe')
            try:
                d.start(); peer = d._rpc.peer_identity
                assert peer.pid == first.pid and peer.scope == 'uid_exe_only'
                rows.append({'case': 'uid-exe-only', 'result': 'PASS', **peer.metadata(), 'new_thread': False})
            finally: d.stop()
            rejected('wrong-exe', driver('wrong-exe', expected='/bin/true'), 'exe_mismatch')
            rejected('wrong-starttime', driver('wrong-starttime', OwnedDaemonIdentity(first.pid, owned.starttime + 1)), 'owned_starttime_mismatch')
            stop(first, 'first')
            second = spawn('second')
            assert second.pid != first.pid
            rejected('replacement-daemon', driver('replacement', owned), 'owned_pid_mismatch')
            stop(second, 'second')
        finally:
            for proc, stdout, stderr, label in processes:
                if proc.poll() is None: stop(proc, label)
                stdout.close(); stderr.close()
    assert len(rows) == 5 and all(r['result'] == 'PASS' for r in rows)
    (out / 'RESULT.json').write_text(json.dumps({'version': version, 'rows': rows, 'cleanup': cleanup,
        'uid': os.getuid(), 'isolated_HOME_CODEX_HOME': True, 'external_credentials_inherited': False,
        'model_turns': 0, 'listener_launcher': 'owned foreground app-server --listen unix://PATH, not daemon-start ownership proof',
        'end': datetime.datetime.now(datetime.timezone.utc).isoformat()}, indent=2) + '\n')
    print('REAL_DAEMON_IDENTITY_PASS version=' + version + ' cases=5 model_turns=0')


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--version', required=True, choices=['0.161.0', '0.163.0'])
    parser.add_argument('--binary', required=True); parser.add_argument('--output', required=True)
    args = parser.parse_args(); main(args.version, args.binary, args.output)
