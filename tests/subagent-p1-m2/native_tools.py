"""Pinned native dynamicTools source probe against synthetic loopback Responses SSE."""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
from pathlib import Path
import sys
import tempfile
import threading
import time
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT/'tools/sessionhelper'))
from sessionhelper.app_server_broker import AppServerBroker
from sessionhelper.codexDriver import StdioJsonRpc, CodexProtocolDriver
from sessionhelper.config import Config
from sessionhelper.driver_adapter import DriverAdapter
from sessionhelper.sub_agent_runtime import SubAgentRuntime, dynamic_tools

SHA = '1748767b230ebfc3d4ab7e4e254920d0c0ad9691fd8c11f190e7d44511a4a92e'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', required=True); parser.add_argument('--output', required=True)
    args = parser.parse_args()
    binary = str(Path(args.binary).resolve())
    assert hashlib.sha256(Path(binary).read_bytes()).hexdigest() == SHA
    out = Path(args.output).resolve(); out.mkdir(parents=True, exist_ok=False)
    done = threading.Event(); lock = threading.Lock(); requests = []; bound_calls = []; results = []
    runtime = None; broker = None; result = {'real_inference': False, 'binary_sha256': SHA}
    class Handler(BaseHTTPRequestHandler):
        protocol_version = 'HTTP/1.1'
        def log_message(self, *args): pass
        def do_POST(self):
            size = int(self.headers.get('Content-Length', '0'))
            if size > 1024*1024: self.send_error(413); return
            request = json.loads(self.rfile.read(size))
            with lock:
                number = len(requests) + 1; requests.append({'number': number, 'input': request.get('input')})
            self.send_response(200); self.send_header('Content-Type', 'text/event-stream'); self.send_header('Connection', 'close'); self.end_headers()
            def event(kind, **fields):
                self.wfile.write(('event: '+kind+'\ndata: '+json.dumps({'type': kind, **fields})+'\n\n').encode()); self.wfile.flush()
            try:
                event('response.created', response={'id': 'resp_'+str(number), 'object': 'response', 'created_at': int(time.time()), 'status': 'in_progress', 'model': 'synthetic', 'output': []})
                # Follow-up after the tool reply is intentionally held: it
                # proves no finish signal/processed ACK was fabricated.
                has_result = any(i.get('type') == 'function_call_output' for i in request.get('input', []) if isinstance(i, dict))
                if has_result:
                    done.wait(15); return
                payload = json.dumps(request.get('input', []))
                tool_name = 'dingwei_send_message'
                arguments = {'to': 'target.owner', 'body': 'LOCAL_SYNTHETIC_NO_REAL_INFERENCE'}
                if 'M2_NATIVE_FORGED' in payload: arguments['from_node_id'] = 'forged-sibling'
                if 'M2_NATIVE_CREATE' in payload:
                    tool_name = 'sub_agent_control'
                    arguments = {'operation': 'create', 'request_id': 'native-control-create'}
                time.sleep(.1)
                item = {'id': 'fc_'+str(number), 'type': 'function_call', 'call_id': 'call_'+str(number), 'name': tool_name, 'arguments': json.dumps(arguments)}
                event('response.output_item.added', output_index=0, item=dict(item, arguments=''))
                event('response.function_call_arguments.done', item_id=item['id'], output_index=0, arguments=item['arguments'])
                event('response.output_item.done', output_index=0, item=item)
                event('response.completed', response={'id': 'resp_'+str(number), 'object': 'response', 'status': 'completed', 'model': 'synthetic', 'output': [item], 'usage': {'input_tokens': 1, 'output_tokens': 1, 'total_tokens': 2}})
            except (BrokenPipeError, ConnectionResetError): pass
    server = ThreadingHTTPServer(('127.0.0.1', 0), Handler); server.daemon_threads = True
    serving = threading.Thread(target=server.serve_forever, daemon=True); serving.start()
    try:
        with tempfile.TemporaryDirectory(prefix='m2-native-', dir='/tmp') as tmp:
            ch = Path(tmp)/'codex'; ch.mkdir()
            (ch/'config.toml').write_text(f'''model_provider="local_probe"
model="synthetic"
[model_providers.local_probe]
name="local synthetic provider"
base_url="http://127.0.0.1:{server.server_port}/v1"
wire_api="responses"
requires_openai_auth=false
supports_websockets=false
request_max_retries=0
stream_max_retries=0
''')
            clean = dict(HOME=tmp, CODEX_HOME=str(ch), PATH='/usr/bin:/bin', RUST_LOG='error')
            with patch.dict(os.environ, clean, clear=True):
                raw = StdioJsonRpc([binary, 'app-server', '--stdio'], cwd=tmp, env=clean); raw.start()
                broker = AppServerBroker(raw); broker.initialize({'clientInfo': {'name': 'm2-source-probe', 'version': '1'}, 'capabilities': {'experimentalApi': True}})
                root = broker.facade('', 1, root=True)
                thread_id = root.request('thread/start', {'cwd': tmp, 'ephemeral': False, 'sandbox': 'read-only', 'approvalPolicy': 'never', 'dynamicTools': dynamic_tools()})['thread']['id']
                driver = CodexProtocolDriver(workdir=tmp, store_path=str(Path(tmp)/'driver.db'), shared_facade=root, bound_thread_id=thread_id)
                driver._rpc = root; driver._broker = broker; driver._initialized = True; driver.native_tools_bound = True
                cfg = Config(session_name='root', key_id='synthetic', secret='synthetic-only', session_full_name='root.owner', inbox_db=str(Path(tmp)/'inbox.db'))
                adapter = DriverAdapter(cfg, driver=driver); adapter._started = True
                runtime = SubAgentRuntime(cfg, adapter, 'root.owner', watchdog=False)
                runtime.on_capability(dict(sub_agents_v1=True, max_supported_depth=1, conn_seq=1, subtree_revision=0))
                def ack():
                    report = runtime.report(); assert report is not None
                    assert runtime.on_ack(dict(ok=True, conn_seq=1, subtree_revision=report['subtree_revision']))
                ack()
                runtime._ensure_broker()
                runtime.send_from_native = lambda source, args: results.append({'source': source, 'args': args}) or {'ok': True, 'stage': 'hub_durable_queued'}
                original_handler = broker.tool_handler
                def handle(facade, params):
                    bound_calls.append({'lane_id': facade.identity.lane_id, 'outer_threadId': params.get('threadId'), 'outer_turnId': params.get('turnId'), 'callId': params.get('callId'), 'tool': params.get('tool'), 'arguments': params.get('arguments')})
                    return original_handler(facade, params)
                broker.tool_handler = handle
                driver.deliver('dw-root-management', 'M2_NATIVE_CREATE')
                deadline = time.monotonic()+10
                while time.monotonic() < deadline and not runtime.store.nodes(): time.sleep(.02)
                assert len(runtime.store.nodes()) == 1, 'NATIVE_CONTROL_CREATE_NOT_BOUND'
                deadline = time.monotonic()+5
                while time.monotonic() < deadline and not runtime.store.nodes()[0]['native_thread_id']: time.sleep(.02)
                first = runtime.store.nodes()[0]
                assert first['native_thread_id'], 'NATIVE_CONTROL_CREATE_NOT_FINISHED'
                children = [runtime.public_node(first), runtime.create('probe-b')]; ack()
                for index, child in enumerate(children):
                    node = runtime.store.node(child['node_id']); did = 'dw-probe-'+str(index)
                    runtime.accept({'id': did, 'from': 'source.owner#synthetic', 'body': 'M2_NATIVE_FORGED' if index else 'M2_NATIVE_VALID', 'meta': {'delivery_id': did, 'delivery_ack_protocol': 2, 'processing_mode': 'driver', 'ack_generation': 1, 'target_node_id': node['node_id'], 'lane_generation': node['generation']}})
                    runtime.worker_step()
                end = time.monotonic()+12
                while time.monotonic() < end and len(bound_calls) < 3: time.sleep(.02)
                assert len(bound_calls) == 3, 'NATIVE_DYNAMIC_TOOLS_NOT_DISPATCHED'
                assert len(results) == 1, 'FORGED_ARGUMENT_SOURCE_ACCEPTED_OR_VALID_SOURCE_REJECTED'
                valid = next(c for c in bound_calls if c['tool'] == 'dingwei_send_message' and 'from_node_id' not in c['arguments'])
                assert results[0]['source']['node_id'] == valid['lane_id'], 'OUTER_THREAD_SOURCE_NOT_BOUND'
                assert len({c['outer_threadId'] for c in bound_calls}) == 3, 'NATIVE_LANES_MERGED'
                # Real active-turn binding rejects delayed old calls.
                facade = runtime.adapters[valid['lane_id']].driver._rpc
                try:
                    runtime.native_tool(facade, {'threadId': valid['outer_threadId'], 'turnId': 'stale', 'tool': 'dingwei_send_message', 'arguments': {'to': 'target.owner', 'body': 'late'}})
                except PermissionError: pass
                else: raise AssertionError('STALE_NATIVE_TURN_ACCEPTED')
                result.update(native_control_create=True, native_dynamic_tools=True, outer_thread_binding=True, forged_arguments_rejected=True, stale_turn_rejected=True, distinct_native_threads=2, outbound_calls=len(results), local_provider_requests=len(requests))
                print(json.dumps(result, indent=2))
                done.set(); runtime.shutdown(); runtime = None; broker = None
    finally:
        done.set()
        if runtime is not None: runtime.shutdown()
        elif broker is not None: broker.shutdown()
        server.shutdown(); server.server_close(); serving.join(2)
        (out/'RESULT.json').write_text(json.dumps(result, indent=2)+'\n')
        (out/'native-tools.json').write_text(json.dumps({'bound_calls': bound_calls, 'outbound': results, 'provider_requests': requests}, indent=2)+'\n')


if __name__ == '__main__': main()
