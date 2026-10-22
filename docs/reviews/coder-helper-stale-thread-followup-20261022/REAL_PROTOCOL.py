"""Pinned official Codex 0.161.0 / 0.163.0 follow-up protocol probe. No credentials or model traffic.

Usage: python3 REAL_PROTOCOL.py /private/codex /private/new-output-directory VERSION FROZEN_SOURCE_WORKTREE
Binary hashes are pinned below from independently downloaded official npm packages.
Only private test app-server children are started; no managed daemon commands.
"""
import hashlib
import json
import os
from pathlib import Path
import queue
import subprocess
import sys
import threading
import time


def main(binary, output, pinned_version, source):
    binary = str(Path(binary).resolve())
    binary_sha = hashlib.sha256(Path(binary).read_bytes()).hexdigest()
    hashes = {'0.161.0': '9a820c17865fa825d04db416818679a9d63bd72e50835c396f496e5684626c9c', '0.163.0': '399ff69c431c066e40de318fba45c2b07f45a76b19939d247ee4828fc70c4215'}
    assert binary_sha == hashes[pinned_version], 'wrong pinned binary'
    assert subprocess.check_output(['git', '-C', source, 'rev-parse', 'HEAD'], text=True).strip() == '70358a01e2002ff05e001ce5ff702c238c80f984'
    assert not subprocess.check_output(['git', '-C', source, 'status', '--porcelain'])
    sys.path.insert(0, str(Path(source).resolve() / 'tools/sessionhelper'))
    from sessionhelper.codexDriver import JsonRpcError
    classifications = []
    root = Path(output).resolve()
    root.mkdir(mode=0o700, parents=True, exist_ok=False)
    home = root / 'home'
    home.mkdir(mode=0o700)
    ch = home / 'codex'
    ch.mkdir(mode=0o700)
    # A deliberately unreachable loopback inference endpoint. A local test turn
    # materializes a rollout; no authentication file or real model is required.
    (ch / 'config.toml').write_text('''model_provider="local_probe"
model="probe-no-inference"
[model_providers.local_probe]
name="local probe"
base_url="http://127.0.0.1:9/v1"
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
''')
    env = {'HOME': str(home), 'CODEX_HOME': str(ch), 'PATH': '/usr/bin:/bin', 'RUST_LOG': 'error'}
    version = subprocess.check_output([binary, '--version'], env=env, text=True).strip()
    assert version == 'codex-cli ' + pinned_version, version
    transcript = []

    class RPC:
        def __init__(self, phase):
            self.phase, self.seq, self.q = phase, 0, queue.Queue()
            self.err = (root / (phase + '.stderr')).open('w')
            self.proc = subprocess.Popen([binary, 'app-server', '--stdio'], env=env, cwd=home,
                                         text=True, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=self.err)
            def reader():
                for line in self.proc.stdout:
                    self.q.put(json.loads(line))
            self.reader = threading.Thread(target=reader, daemon=True)
            self.reader.start()
        def call(self, method, params):
            self.seq += 1
            msg = {'id': self.seq, 'method': method, 'params': params}
            self.proc.stdin.write(json.dumps(msg) + '\n')
            self.proc.stdin.flush()
            while True:
                result = self.q.get(timeout=30)
                transcript.append({'phase': self.phase, 'request': msg if result.get('id') == self.seq else None, 'response': result})
                if result.get('id') == self.seq:
                    return result
        def init(self):
            assert 'result' in self.call('initialize', {'clientInfo': {'name': 'stale-thread-local-probe', 'version': '0'}, 'capabilities': {'experimentalApi': True}})
            self.proc.stdin.write('{"method":"initialized"}\n')
            self.proc.stdin.flush()
        def stop(self):
            self.proc.terminate()
            try:
                self.proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(timeout=10)
            self.reader.join(timeout=2)
            self.proc.stdin.close()
            self.proc.stdout.close()
            self.err.close()

    r = RPC('first')
    try:
        r.init()
        created = r.call('thread/start', {'cwd': str(home), 'ephemeral': False, 'sandbox': 'read-only', 'approvalPolicy': 'never'})
        tid = created['result']['thread']['id']
        assert r.call('thread/read', {'threadId': tid, 'includeTurns': False})['result']['thread']['id'] == tid
        accepted = r.call('turn/start', {'threadId': tid, 'input': [{'type': 'text', 'text': 'LOCAL_PROTOCOL_TEST_ONLY'}]})
        assert accepted['result']['turn']['id']
        # Persistence is the property under test, not model completion. Stop our
        # private server only after its generated rollout exists on disk.
        rollout = Path(created['result']['thread']['path'])
        deadline = time.monotonic() + 10
        while not (rollout.exists() and rollout.stat().st_size > 0):
            assert time.monotonic() < deadline, 'local rollout not materialized'
            time.sleep(0.02)
        # Empty ephemeral thread: read can see it while live, but after restart
        # it must not be silently replaced with a fresh conversation.
        ephemeral = r.call('thread/start', {'cwd': str(home), 'ephemeral': True, 'sandbox': 'read-only', 'approvalPolicy': 'never'})['result']['thread']['id']
    finally:
        r.stop()
        (root / 'transcript-first.json').write_text(json.dumps(transcript, indent=2) + '\n')
    r = RPC('restarted')
    try:
        r.init()
        before = r.call('thread/read', {'threadId': tid, 'includeTurns': False})
        # A persisted thread may be readable as notLoaded; read alone is not admission.
        assert 'error' in before or before['result']['thread']['id'] == tid
        resumed = r.call('thread/resume', {'threadId': tid, 'excludeTurns': True})
        assert resumed['result']['thread']['id'] == tid, resumed
        loaded = r.call('thread/read', {'threadId': tid, 'includeTurns': False})['result']['thread']
        assert loaded['id'] == tid and loaded['status']['type'] == 'idle', loaded
        for missing in [ephemeral, '00000000-0000-4000-8000-000000000000']:
            for method, expected in [('thread/read', 'thread not loaded: '), ('thread/resume', 'no rollout found for thread id '), ('turn/start', 'thread not found: ')]:
                params = {'threadId': missing}
                if method == 'turn/start':
                    params['input'] = [{'type': 'text', 'text': 'MUST_NOT_BE_ADMITTED'}]
                response = r.call(method, params)
                assert response['error'] == {'code': -32600, 'message': expected + missing}, response
                actual = JsonRpcError(method, response['error']).classify(missing)
                assert actual == ('thread_not_loaded' if method == 'thread/read' else 'missing_thread'), (method, actual)
                classifications.append({'method': method, 'thread_id': missing, 'error': response['error'], 'classify': actual})
    finally:
        r.stop()
        (root / 'transcript.json').write_text(json.dumps(transcript, indent=2) + '\n')
    # Exercise the real helper implementation against the same pinned server.
    from sessionhelper.codexDriver import CodexProtocolDriver, StdioJsonRpc
    driver_results = []
    for label, bound, expected in [('persisted', tid, 'processing'), ('missing', ephemeral, 'failed')]:
        driver = CodexProtocolDriver(workdir=str(home), manage_daemon=False, store_path=str(root / ('ledger-' + label + '.db')))
        transport = StdioJsonRpc([binary, 'app-server', '--stdio'], cwd=str(home), env=env)
        calls = []
        try:
            transport.start()
            driver._rpc = transport
            driver._initialize()
            driver._thread_id = driver._session_id = bound
            original = driver._request
            def tracked(method, params=None, **kw):
                calls.append(method)
                return original(method, params, **kw)
            driver._request = tracked
            receipt = driver.deliver('local-' + label, 'LOCAL_PROTOCOL_TEST_ONLY')
            assert receipt.state == expected, (label, receipt)
            assert 'thread/resume' in calls, calls
            assert 'thread/start' not in calls, calls
            if label == 'missing':
                assert 'turn/start' not in calls, calls
                assert 'error_class=missing_thread' in receipt.detail, receipt
                assert receipt.detail.startswith('session_unavailable:')
            else:
                assert calls.index('thread/resume') < calls.index('turn/start')
            driver_results.append({'case': label, 'state': receipt.state, 'detail': receipt.detail, 'methods': calls})
        finally:
            driver.stop()
    (root / 'classifications.json').write_text(json.dumps(classifications, indent=2) + '\n')
    (root / 'driver-results.json').write_text(json.dumps(driver_results, indent=2) + '\n')
    result = {'version': version, 'binary_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
              'result': 'LOCAL_REAL_PROTOCOL_PASS', 'credential_env': False, 'managed_daemon_commands': False,
              'real_inference_endpoint': False, 'four_private_processes': True, 'real_helper_admission_cases': 2,
              'persisted_resume_same_id': True, 'missing_error_code': -32600, 'classifier_cases': len(classifications), 'source': '70358a01e2002ff05e001ce5ff702c238c80f984'}
    (root / 'RESULT.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(result))


if __name__ == '__main__':
    main(*sys.argv[1:])
