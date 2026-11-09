"""Run criterion-specific mutations on private copied sources; leave the writer tree intact."""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[2]
PREFIX = 'tools.sessionhelper.tests.test_sub_agent_helper.'

# (criterion, name, file, exact guard, replacement, test)
CASES = [
('AA007', 'turn_is_not_task_completion', 'sub_agent_runtime.py', 'self.store.touch(facade.identity.lane_id)', "self.store.touch(facade.identity.lane_id)\n            if message.get('method') == 'turn/completed': self.close(facade.identity.lane_id, 'completed')", 'RuntimeTests.test_aa007_idle_progress_and_pending_work_are_distinct'),
('AA009', 'confirmation_scope', 'sub_agent_store.py', "return True, dict(rows[0])", "return False, None", 'RuntimeTests.test_m1_confirmation_without_lane_echo_uses_immutable_scope'),
('AA004', 'wire_destination', 'sub_agent_connection.py', "target = args['to'] if '#' in args['to'] else session_addr(args['to'], self.book.key_id)", "target = args['to']", 'ConnectionTests.test_native_send_full_route_transport_and_durable_receipt'),
('AA007', 'creation_lock', 'sub_agent_runtime.py', "result = broker.request(None, 'thread/start', params)", "with self._lock:\n                result = broker.request(None, 'thread/start', params)", 'RuntimeTests.test_slow_creation_does_not_block_parent_lifecycle_seal'),
('AA004', 'send_journal', 'sub_agent_store.py', 'if prior is not None:\n                if prior[0]: return json.loads(prior[0])', 'if prior is not None:\n                return None\n                if prior[0]: return json.loads(prior[0])', 'RuntimeTests.test_native_send_journal_survives_restart'),
('AA004', 'outer_source', 'sub_agent_runtime.py', "or params.get('threadId') != facade.thread_id", '', 'RuntimeTests.test_aa004_trusted_native_context_source_and_forgery'),
('AA004', 'stale_turn', 'sub_agent_runtime.py', "adapter.driver._active_turn_id != params.get('turnId')", 'False', 'RuntimeTests.test_aa004_trusted_native_context_source_and_forgery'),
('AA004', 'argument_source', 'sub_agent_runtime.py', 'or set(args) - allowed', '', 'RuntimeTests.test_aa004_trusted_native_context_source_and_forgery'),
('AA006', 'timestamp_prefix', 'sub_agent_store.py', 'candidate = tail[-width:]', 'candidate = tail[:width]', 'StoreTests.test_aa006_random_tail_collision_alias_and_tombstone'),
('AA006', 'tail_collision', 'sub_agent_store.py', 'if candidate not in occupied:', 'if True:', 'StoreTests.test_aa006_random_tail_collision_alias_and_tombstone'),
('AA006', 'self_context', 'sub_agent_runtime.py', "parent=self.root_full_name if node_id else ''", "parent=''", 'RuntimeTests.test_aa004_trusted_native_context_source_and_forgery'),
('AA007', 'default_completion', 'sub_agent_store.py', 'dict(exit_on_task_complete=True, idle_timeout=3600', 'dict(exit_on_task_complete=False, idle_timeout=3600', 'StoreTests.test_aa007_policy_defaults_and_negative_bounds'),
('AA007', 'unexecuted_filter', 'sub_agent_store.py', 'AND native_dispatch_at IS NULL AND admitted_at IS NULL', 'AND admitted_at IS NULL', 'StoreTests.test_aa007_atomic_seal_returns_only_definitely_unexecuted'),
('AA007', 'platform_cap', 'sub_agent_runtime.py', "if now - node['created_at'] >= policy['platform_lifetime_cap']:", 'if False:', 'RuntimeTests.test_aa007_completion_idle_cap_restart_and_parent_cascade'),
('AA007', 'parent_cascade', 'sub_agent_runtime.py', "for node in self.store.nodes(): self.close(node['node_id'], 'parent_closed')", 'for node in []: pass', 'RuntimeTests.test_aa007_completion_idle_cap_restart_and_parent_cascade'),
('AA007', 'completion_override', 'sub_agent_runtime.py', "if operation == 'task_completed' and not json.loads(node['policy_json'])['exit_on_task_complete']:", 'if False:', 'RuntimeTests.test_aa007_completion_idle_cap_restart_and_parent_cascade'),
('AA007', 'persisted_idle', 'sub_agent_runtime.py', 'busy |= bool(db.execute', 'busy |= False and bool(db.execute', 'RuntimeTests.test_aa007_restart_unknown_work_does_not_become_idle'),
('AA008', 'event_owner', 'app_server_broker.py', 'facade = self._threads.get(thread_id)', "facade = self._threads.get('thread-a')", 'BrokerTests.test_aa008_outer_thread_only_and_bounded_quarantine'),
('AA008', 'request_shape', 'codexDriver.py', 'pending = self._pending.get(request_id) if "method" not in msg else None', 'pending = self._pending.get(request_id)', 'BrokerTests.test_native_request_id_collision_with_client_pending'),
('AA008', 'child_stops_process', 'app_server_broker.py', 'self.broker.detach(self)', 'self.broker.transport.stop()\n        self.broker.detach(self)', 'BrokerTests.test_aa008_parallel_response_wait_and_single_physical_writer'),
('AA008', 'ack_gate', 'sub_agent_store.py', "if require_ack and row['changed_revision'] > ack:", 'if False:', 'StoreTests.test_tree_ack_current_connection_required_and_ended_not_revived'),
('AA009', 'envelope_unscoped', 'codexDriver.py', 'FROM deliveries WHERE envelope_id = ? AND lane_id=? AND lane_generation=? ORDER BY updated_at DESC LIMIT 1', 'FROM deliveries WHERE envelope_id = ? AND (? IS NOT NULL) AND (? IS NOT NULL) ORDER BY updated_at DESC LIMIT 1', 'StoreTests.test_aa009_inbox_ledger_query_callback_generation_scope'),
('AA009', 'shared_inbox', 'sub_agent_store.py', "return root + '\\x1flane:' + hashlib.sha256(lane_id.encode()).hexdigest() + ':' + str(generation)", 'return root', 'StoreTests.test_aa009_inbox_ledger_query_callback_generation_scope'),
('AA009', 'callback_lane', 'inbox_lifecycle.py', "if expected_context is not None and 'lane_id' in expected_context and (row['lane_id'],row['lane_generation'])!=(expected_context['lane_id'],expected_context.get('lane_generation')):", 'if False:', 'StoreTests.test_aa009_inbox_ledger_query_callback_generation_scope'),
]


def main():
    parser = argparse.ArgumentParser(description=__doc__); parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args(); args.output.mkdir(parents=True, exist_ok=False)
    records = []
    baseline = subprocess.run([sys.executable, '-m', 'unittest', '-q', PREFIX.rstrip('.')], cwd=ROOT,
        env={**os.environ, 'PYTHONPATH': str(ROOT/'tools/sessionhelper')}, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=45)
    (args.output/'baseline.stdout').write_text(baseline.stdout)
    assert baseline.returncode == 0, 'positive criterion tests failed before mutation'
    for criterion, name, filename, needle, replacement, test in CASES:
        with tempfile.TemporaryDirectory(prefix='m2-mut-', dir='/tmp') as tmp:
            source = Path(tmp)/'sessionhelper'; shutil.copytree(ROOT/'tools/sessionhelper/sessionhelper', source, ignore=shutil.ignore_patterns('__pycache__'))
            path = source/filename; original = path.read_text()
            assert needle in original, (name, 'mutation needle missing')
            path.write_text(original.replace(needle, replacement))
            environment = {**os.environ, 'PYTHONPATH': tmp}
            run = subprocess.run([sys.executable, '-B', '-m', 'unittest', '-v', PREFIX+test], cwd=ROOT, env=environment,
                text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=45)
            (args.output/(name+'.stdout')).write_text(run.stdout)
            target_red = run.returncode != 0 and ('FAIL: '+test.split('.')[-1]) in run.stdout and 'AssertionError' in run.stdout and not any(error in run.stdout for error in ('ImportError', 'ModuleNotFoundError', 'SyntaxError'))
            records.append(dict(criterion=criterion, name=name, target_test=test, rc=run.returncode, target_red=target_red))
            (args.output/'RESULT.json').write_text(json.dumps(records, indent=2)+'\n')
            print(json.dumps(records[-1]), flush=True)
            assert target_red, (name, 'did not fail at criterion assertion')
    print('M2_MUTATIONS_TARGET_RED='+str(len(records)))


if __name__ == '__main__': main()
