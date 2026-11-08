"""Helper-owned first-level Codex lanes. No registration or RPC source comes from model claims."""
from __future__ import annotations

import copy
import json
import threading
import time

from .app_server_broker import LocalDispatchRejected
from .codexDriver import CodexProtocolDriver
from .driver_adapter import DriverAdapter
from .outbound_receipt import OutboundRejected
from .sub_agent_store import SubAgentStore, lane_session


def dynamic_tools():
    def tool(name, description, properties, required=()):
        return dict(type='function', name=name, description=description,
                    inputSchema=dict(type='object', properties=properties, required=list(required), additionalProperties=False))
    return [tool('dingwei_self_context', '查询当前分道全名、父、直接孩子和生命周期策略', {}),
            tool('sub_agent_control', '主分道创建或关闭子分道；子分道只能明确完成或关闭自己',
                 dict(operation=dict(type='string', enum=['create', 'close', 'task_completed']), request_id=dict(type='string'),
                      target_node_id=dict(type='string'), aliases=dict(type='array', items=dict(type='string')),
                      policy=dict(type='object')), ['operation']),
            tool('dingwei_send_message', '以当前真实原生thread绑定的分道发送消息；不得提供来源字段',
                 dict(to=dict(type='string'), body=dict(type='string'), reply_policy=dict(type='string', enum=['expect_reply', 'no_reply', 'ack_only'])), ['to', 'body'])]


class SubAgentRuntime:
    def __init__(self, cfg, root_adapter, root_full_name, *, clock=time.time, watchdog=True):
        if not root_full_name or root_full_name.count('.') != 1: raise ValueError('sub-agent root full name required')
        if not isinstance(getattr(root_adapter, 'driver', None), CodexProtocolDriver): raise ValueError('sub-agents require Codex driver')
        self.cfg, self.root_adapter, self.root_full_name = cfg, root_adapter, root_full_name
        self.clock = clock
        self.store = SubAgentStore(cfg.inbox_db, cfg.session_name, clock=clock)
        self.adapters, self.inboxes = {}, {}
        self._broker = None
        self._endpoint = None
        self._lock = threading.RLock()
        self._stop = threading.Event()
        self._closing_parent = False
        self._pending_tools = {}
        self.ready = False
        self.conn_seq = None
        self.hub_revision = 0
        self._last_report = None
        self._last_report_at = 0
        self._context_seen = {}
        self.error = ''
        self.root_tools_available = None
        self.send_from_native = None  # bound by authenticated SessionHelper WS only
        self.return_to_source = None
        root_adapter.driver.dynamic_tools = dynamic_tools()
        root_adapter.driver.context_provider = lambda instructions=False: self.context_input('', instructions)
        if watchdog:
            self._watchdog = threading.Thread(target=self._watch, daemon=True, name='sub-agent-lifecycle')
            self._watchdog.start()

    def _ensure_broker(self):
        with self._lock:
            if self._broker is None:
                self.root_adapter.start()
                driver = self.root_adapter.driver
                if driver._broker is None: raise RuntimeError('sub-agent broker was not enabled')
                self._broker = driver._broker
                self._endpoint = driver._private_endpoint
                self._broker.tool_handler = self.native_tool
                self._broker.progress_handler = self.native_progress
                # Root binding remains the only private_endpoint saved_thread.
                if not driver._thread_id:
                    with driver._start_lock:
                        if not driver._thread_id: driver._ensure_thread()
                if driver.native_tools_bound: self.store.record_native_tools(driver._thread_id)
                self.root_tools_available = self.store.has_native_tools(driver._thread_id)
                if not self.root_tools_available:
                    self.error = 'sub_agent_existing_root_tools_unavailable'
                    print('[sub-agent] existing root thread has no installed dynamicTools; root control tools unavailable', flush=True)
                self.root_adapter._lane_driver_factory = lambda: self._driver_for('', 1, driver._thread_id)
            return self._broker

    def _driver_for(self, node_id, generation, thread_id):
        root = self.root_adapter.driver
        facade = self._broker.facade(node_id, generation, thread_id, root=not node_id)
        if node_id:
            node = self.store.node(node_id)
            facade.write_guard = lambda params: self.store.mark_native_write(node, params['clientUserMessageId'])
        driver = CodexProtocolDriver(workdir=root._workdir, transport_mode=root._transport_mode,
            manage_daemon=False, daemon_verified=root._daemon_verified, replay_verified=root._replay_verified,
            store_path=root._store._path, model=root._model, model_provider=root._model_provider,
            sandbox=root._sandbox, approval_policy=root._approval_policy, view_item_backfill=root._view_item_backfill,
            shared_facade=facade, bound_thread_id=thread_id, lane_id=node_id, lane_generation=generation)
        driver.context_provider = lambda instructions=False: self.context_input(node_id, instructions)
        driver.dynamic_tools = dynamic_tools()
        return driver

    def _restore(self, node):
        if not node['native_thread_id']: return
        if node['node_id'] not in self.inboxes:
            self.inboxes[node['node_id']] = self.store.inbox(node, ready=lambda: self.ready, capacity=self.cfg.busy_buffer_max)
        if node['node_id'] in self.adapters or node['state'] == 'ended': return
        self._ensure_broker()
        node_id, generation, thread_id = node['node_id'], node['generation'], node['native_thread_id']
        cfg = copy.copy(self.cfg)
        cfg.sub_agents_v1 = False
        factory = lambda: self._driver_for(node_id, generation, thread_id)
        adapter = DriverAdapter(cfg, driver=factory(), driver_factory=factory)
        inbox = self.inboxes[node_id]
        adapter.delivery_lifecycle = inbox
        adapter.delivery_progress = inbox.progress
        self.adapters[node_id], self.inboxes[node_id] = adapter, inbox
        if node['state'] == 'draining': adapter.seal_dispatch()
        adapter.start()

    def create(self, request_id, policy=None, aliases=()):
        with self._lock:
            if self._closing_parent: raise ValueError('sub_agent_ended')
            if not self.ready: raise ValueError('sub_agent_registry_unavailable')
            node, fresh = self.store.allocate(request_id, policy, aliases)
            if not fresh and node['creation_status'] != 'intent':
                return self.public_node(node)
            broker = self._ensure_broker()
            if not self.store.mark_creation_submitted(node['node_id']): return self.public_node(self.store.node(node['node_id']))
            root = self.root_adapter.driver
            params = dict(cwd=root._workdir, ephemeral=False, dynamicTools=dynamic_tools(),
                developerInstructions='你是 DingWei 受控一级子分道；只使用本线程 dynamicTools。用 dingwei_self_context 查询自己的完整名、父、孩子及策略；不得从环境变量、IPC或参数声明来源。')
            for key, value in [('model', root._model), ('modelProvider', root._model_provider), ('sandbox', root._sandbox), ('approvalPolicy', root._approval_policy)]:
                if value is not None: params[key] = value
            # Submitted is committed before thread/start. A lost response or
            # crash leaves an uncertain creation; repeating request_id never
            # issues a second thread/start or adopts arbitrary thread/list data.
            try:
                result = broker.request(None, 'thread/start', params)
                self.store.bind(node['node_id'], result['thread']['id'])
            except Exception:
                self.error = 'sub_agent_creation_unknown'
                raise RuntimeError('sub_agent_creation_unknown: native result uncertain; no retry') from None
            node = self.store.node(node['node_id'])
            self._restore(node)
            return self.public_node(node)

    def public_node(self, node):
        return dict(node_id=node['node_id'], generation=node['generation'], state=node['state'],
            full_name=(node['canonical_name'] + '.' + self.root_full_name) if node['canonical_name'] else '',
            registered=self.ready and node['state'] == 'active' and node['changed_revision'] <= self.hub_revision and bool(node['canonical_name']),
            creation_status=node['creation_status'], reason=node['reason'], policy=json.loads(node['policy_json']))

    def control(self, actor, operation, *, request_id=None, target_node_id=None, policy=None, aliases=()):
        if operation == 'create':
            if actor: raise PermissionError('children cannot create descendants')
            return self.create(request_id, policy, aliases)
        if operation not in ('close', 'task_completed'): raise ValueError('unsupported sub-agent control')
        target = target_node_id or actor
        if not target:
            if operation != 'close': raise ValueError('root completion is not child completion')
            self.close_parent(); return {'state': 'draining'}
        if actor and actor != target: raise PermissionError('child cannot close sibling')
        node = self.store.node(target)
        if node is None: raise ValueError('sub_agent_not_found')
        if operation == 'task_completed' and not json.loads(node['policy_json'])['exit_on_task_complete']:
            return self.public_node(node)
        self.close(target, 'completed' if operation == 'task_completed' else 'operator_closed')
        return self.public_node(self.store.node(target))

    def close(self, node_id, reason):
        with self._lock:
            self.store.seal(node_id, reason)
            adapter = self.adapters.get(node_id)
            if adapter is not None: adapter.seal_dispatch()

    def close_parent(self):
        with self._lock:
            self._closing_parent = True
            for node in self.store.nodes(): self.close(node['node_id'], 'parent_closed')

    def context(self, node_id):
        node = self.store.node(node_id) if node_id else None
        children = [self.public_node(n) for n in self.store.nodes()] if not node_id else []
        return dict(full_name=(node['canonical_name'] + '.' + self.root_full_name) if node else self.root_full_name,
            parent=self.root_full_name if node_id else '', node_id=node_id, generation=node['generation'] if node else 1,
            direct_children=children, policy=json.loads(node['policy_json']) if node else None,
            subtree_revision=self.store.snapshot(self.conn_seq or 0, self.hub_revision)['subtree_revision'],
            source_tools='native_thread_bound_dynamicTools', max_supported_depth=1)

    def context_input(self, node_id, instructions=False):
        context = self.context(node_id)
        revision = context['subtree_revision']
        if not instructions and self._context_seen.get(node_id) == revision: return ''
        self._context_seen[node_id] = revision
        return '【平台分道上下文；独立于业务正文】' + json.dumps(context, ensure_ascii=False, sort_keys=True)

    def native_tool(self, facade, params):
        node_id, generation = facade.identity.lane_id, facade.identity.generation
        with self._lock:
            if facade.closed or params.get('namespace') is not None or params.get('threadId') != facade.thread_id: raise PermissionError('unbound native source')
            adapter = self.adapters.get(node_id) if node_id else self.root_adapter
            if adapter is None or adapter.driver._active_turn_id != params.get('turnId') or not params.get('turnId'): raise PermissionError('stale native turn')
            if node_id:
                node = self.store.node(node_id)
                if node is None or node['generation'] != generation or node['state'] != 'active': raise PermissionError('closed native source')
            if not isinstance(params.get('callId'), str) or not 1 <= len(params['callId']) <= 256: raise PermissionError('missing native call binding')
            args = params.get('arguments')
            if not isinstance(args, dict): raise ValueError('invalid native tool arguments')
            tool = params.get('tool')
            allowed = {'dingwei_self_context': set(), 'sub_agent_control': {'operation', 'request_id', 'target_node_id', 'policy', 'aliases'},
                       'dingwei_send_message': {'to', 'body', 'reply_policy'}}.get(tool)
            if allowed is None or set(args) - allowed: raise PermissionError('untrusted native source fields')
            self._pending_tools[node_id] = self._pending_tools.get(node_id, 0) + 1
        try:
            if tool == 'dingwei_self_context': return self.context(node_id)
            if tool == 'sub_agent_control': return self.control(node_id, **args)
            if self.send_from_native is None or not self.ready: raise RuntimeError('sub_agent_send_unavailable')
            # Source is derived only here, from outer native thread binding.
            source = self.context(node_id)
            binding = (node_id, generation, facade.thread_id, params['turnId'], params['callId'])
            prior = self.store.begin_native_send(*binding)
            if prior is not None: return prior
            result = self.send_from_native(source, args)
            self.store.finish_native_send(*binding, result)
            return result
        finally:
            with self._lock: self._pending_tools[node_id] -= 1

    def native_progress(self, facade, message):
        if facade.identity.lane_id and message.get('method') in ('turn/started', 'turn/completed', 'item/started', 'item/completed', 'item/agentMessage/delta', 'item/commandExecution/outputDelta'):
            self.store.touch(facade.identity.lane_id)

    def on_capability(self, meta):
        with self._lock:
            self.ready = False; self._last_report = None
            if meta.get('sub_agents_v1') is not True or type(meta.get('conn_seq')) is not int or meta['conn_seq'] < 1 or meta.get('max_supported_depth') != 1:
                self.conn_seq = None; self.error = 'sub_agent_unsupported'; return
            self.conn_seq, self.hub_revision = meta['conn_seq'], meta.get('subtree_revision', 0)
            if type(self.hub_revision) is not int or self.hub_revision < 0: self.conn_seq = None

    def disconnect(self):
        with self._lock:
            self.ready = False; self.conn_seq = None; self._last_report = None
            self.send_from_native = None; self.return_to_source = None

    def report(self):
        with self._lock:
            if self.conn_seq is None: return None
            if self._last_report is not None:
                if time.monotonic() - self._last_report_at < 5: return None
                self._last_report_at = time.monotonic(); return self._last_report
            try: snapshot = self.store.snapshot(self.conn_seq, self.hub_revision)
            except RuntimeError: self.error = 'sub_agent_registry_unavailable'; return None
            if self.ready and snapshot['subtree_revision'] == self.hub_revision: return None
            self._last_report = snapshot; self._last_report_at = time.monotonic()
            return snapshot

    def on_ack(self, meta):
        with self._lock:
            if self._last_report is None or meta.get('conn_seq') != self.conn_seq: return False
            if meta.get('ok') is not True:
                self.ready = False; self.error = 'sub_agent_tree_resync_required'; return False
            if meta.get('subtree_revision') != self._last_report['subtree_revision']: return False
            self.hub_revision = meta['subtree_revision']; self.store.acknowledge(self.hub_revision)
            self._last_report = None; self.ready = True; self.error = ''
            return True

    def accept(self, env):
        meta = env.get('meta') or {}; node_id = meta.get('target_node_id')
        with self._lock:
            node = self.store.node(node_id)
            if node is None: raise ValueError('sub_agent_not_found')
            if not self.ready: raise ValueError('sub_agent_registry_unavailable')
            self._restore(node)
            inbox = self.inboxes.get(node_id)
            if inbox is None: raise ValueError('sub_agent_ended')
            return inbox.accept(str(meta['delivery_id']), env)

    def worker_step(self):
        if not self.ready: return
        with self._lock:
            for node in self.store.nodes():
                if node['native_thread_id']: self._restore(node)
            lanes = list(self.inboxes.items())
        for node_id, inbox in lanes:
            node = self.store.node(node_id)
            if node['state'] != 'active': continue
            item = inbox.claim_next()
            if item is None: continue
            delivery_id, env = item
            try: self.adapters[node_id].handle_delivery(env)
            except Exception: inbox.record_admission_unknown(delivery_id)

    def tick(self):
        now = self.clock()
        for node in self.store.nodes():
            policy = json.loads(node['policy_json']); node_id = node['node_id']
            if node['state'] in ('active', 'allocating'):
                reason = ''
                if now - node['created_at'] >= policy['platform_lifetime_cap']: reason = 'platform_cap'
                elif policy['max_lifetime'] is not None and now - node['created_at'] >= policy['max_lifetime']: reason = 'max_lifetime'
                elif policy['idle_timeout'] is not None and now - node['last_activity'] >= policy['idle_timeout']:
                    busy = bool(self._pending_tools.get(node_id)) or node['creation_status'] == 'submitted'
                    # Read persisted work even before adapters are restored.
                    # Restart/offline never turns ambiguous work into idle.
                    with self.store.transaction() as db:
                        busy |= bool(db.execute("SELECT 1 FROM delivery_inbox WHERE session_name=? AND state IN ('received','processing','admitted','admission_unknown') LIMIT 1", (lane_session(self.store.root, node_id, node['generation']),)).fetchone())
                    adapter = self.adapters.get(node_id)
                    if adapter is not None: busy |= not adapter.is_idle()
                    if not busy: reason = 'idle_timeout'
                if reason: self.close(node_id, reason)
            elif node['state'] == 'draining' and now - node['draining_at'] >= policy['exit_grace']:
                adapter = self.adapters.get(node_id)
                if adapter is not None:
                    driver = adapter.driver
                    if driver._active_turn_id:
                        try: driver.interrupt()
                        except Exception: pass
                    try: driver._rpc.request('thread/unsubscribe', {'threadId': node['native_thread_id']}, timeout=2)
                    except Exception: pass
                    adapter.close_lane()
                self.store.end(node_id)

    def flush_returns(self):
        if not self.ready or self.return_to_source is None: return
        for item in self.store.returns():
            self.store.return_result(item['delivery_id'], {'state': 'sending'}, 'uncertain', node_id=item['node_id'], generation=item['generation'])
            try:
                result = self.return_to_source(item)
                self.store.return_result(item['delivery_id'], result, 'returned' if (result.get('state') == 'queued' or (result.get('ok') is True and result.get('stage') == 'hub_durable_queued')) else 'undeliverable', node_id=item['node_id'], generation=item['generation'])
            except OutboundRejected:
                self.store.return_result(item['delivery_id'], {'reason': 'hub_rejected'}, 'undeliverable', node_id=item['node_id'], generation=item['generation'])
            except Exception:
                # Uncertain send is retained and never retried automatically.
                pass

    def _watch(self):
        while not self._stop.wait(1):
            try: self.tick()
            except Exception: self.error = 'sub_agent_lifecycle_unavailable'

    def shutdown(self):
        self.close_parent()
        self._stop.set()
        for adapter in list(self.adapters.values()): adapter.close_lane()
        self.root_adapter.close_lane()
        if self._broker is not None: self._broker.shutdown()
        if self._endpoint is not None: self._endpoint.release_writer()
