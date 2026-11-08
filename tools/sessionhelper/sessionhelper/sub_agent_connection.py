"""Authenticated-connection glue for opt-in helper lanes."""
import asyncio
import json
import time

from .protocol import envelope, session_addr


class SubAgentConnection:
    def sub_agent_capabilities(self):
        runtime = getattr(self, 'sub_agents', None)
        if runtime is None or not getattr(self, 'enroll_credential', None): return {}
        return dict(sub_agents_v1=True, max_supported_depth=1,
                    subtree_revision=runtime.store.snapshot(0, 0)['subtree_revision'],
                    sub_agent_control_transport='native_dynamicTools',
                    sub_agent_root_tools=runtime.root_tools_available)

    def sub_agent_control_frame(self, ws, env):
        meta = env.get('meta') or {}
        kind = meta.get('type')
        if kind not in ('sub_agent_capabilities', 'sub_agent_tree_ack'): return False
        runtime = getattr(self, 'sub_agents', None)
        # Capability and ACK are control frames on this authenticated connection.
        # Consume even invalid/stale controls; never pass them to any model.
        if (runtime is not None and getattr(self, 'enroll_credential', None) and
                self.outbound_receipts.connection is ws and
                env.get('from') == getattr(self, '_connection_delivery_ack_source', '') and
                self.outbound_receipts.valid_hub_address(env)):
            if kind == 'sub_agent_capabilities': runtime.on_capability(meta)
            else: runtime.on_ack(meta)
        return True

    def sub_agent_ack_inbox(self, meta):
        node_id = meta.get('target_node_id')
        if not node_id: return self.delivery_inbox
        runtime = getattr(self, 'sub_agents', None)
        if runtime is None: return None
        inbox = runtime.inboxes.get(node_id)
        if inbox is None or type(meta.get('lane_generation')) is not int or meta['lane_generation'] != inbox.lane_generation: return None
        return inbox

    async def accept_sub_agent_delivery(self, ws, env):
        meta = env.get('meta') or {}
        if not any(key in meta for key in ('target_node_id', 'sub_path', 'target_full_name', 'lane_generation')): return False
        identity = {k: meta[k] for k in ('target_node_id', 'lane_generation', 'ack_generation', 'processing_mode') if k in meta}
        if 'delivery_ack_protocol' in meta: identity['ack_protocol'] = meta['delivery_ack_protocol']
        try:
            runtime = getattr(self, 'sub_agents', None)
            if runtime is None or not getattr(self, 'enroll_credential', None): raise ValueError('sub_agent_unsupported')
            # First-phase lanes use the evidence-backed driver ACK protocol.
            if meta.get('delivery_ack_protocol') != 2: raise ValueError('sub_agent_unsupported')
            await asyncio.to_thread(runtime.accept, env)
            return True  # durable lane outbox owns queued/injected/terminal ACKs
        except OverflowError:
            reason, state, retryable = 'queue_full', 'busy', True
        except Exception as exc:
            allowed = {'sub_agent_not_found', 'sub_agent_ended', 'sub_agent_generation_mismatch',
                       'sub_agent_registry_unavailable', 'sub_agent_unsupported'}
            reason = str(exc) if str(exc) in allowed else 'sub_agent_registry_unavailable'
            state, retryable = 'failed', reason == 'sub_agent_registry_unavailable'
        await ws.send(json.dumps(self.delivery_ack(str(meta.get('delivery_id') or ''), state,
            reason=reason, retryable=retryable, **identity), ensure_ascii=False))
        return True

    async def sub_agent_loop(self, ws):
        runtime = getattr(self, 'sub_agents', None)
        if runtime is None or not getattr(self, 'enroll_credential', None):
            await asyncio.Event().wait()
        loop = asyncio.get_running_loop()

        async def send_native(source, args):
            meta = {'reply_policy': args.get('reply_policy', 'no_reply')}
            if source['node_id']:
                meta.update(from_full_name=source['full_name'], from_node_id=source['node_id'], from_generation=source['generation'])
            # Transport From remains the authenticated root. Sub-source metadata
            # comes exclusively from native thread binding and is checked by Hub.
            return await self.outbound_receipts.send(ws, args['to'], args['body'], self.book.self_addr, meta)

        async def send_return(item):
            source = json.loads(item['source_json'])
            target = source.get('from_full_name') or source['address']
            result = await self.outbound_receipts.send(ws, target,
                '子会话已结束；此信确认未执行。', self.book.self_addr,
                {'type': 'sub_agent_delivery_return', 'original_delivery_id': item['delivery_id'],
                 'ended_node_id': item['node_id'], 'ended_generation': item['generation'], 'reason': item['reason']})
            return result

        def submit(coroutine):
            return asyncio.run_coroutine_threadsafe(coroutine, loop).result(timeout=15)
        runtime.send_from_native = lambda source, args: submit(send_native(source, args))
        runtime.return_to_source = lambda item: submit(send_return(item))
        last_sent = {}
        try:
            while True:
                report = await asyncio.to_thread(runtime.report)
                if report is not None:
                    await ws.send(json.dumps(envelope(session_addr('workpulse', self.book.key_id), '', self.book.self_addr, report), ensure_ascii=False))
                if runtime.ready:
                    await asyncio.to_thread(runtime._ensure_broker)
                    await asyncio.to_thread(runtime.worker_step)
                    for inbox in list(runtime.inboxes.values()):
                        for item in await asyncio.to_thread(inbox.pending_delivery_acks):
                            token = (inbox.lane_id, inbox.lane_generation, item['delivery_id'], item['sequence'])
                            if time.monotonic() - last_sent.get(token, -10) < 1: continue
                            await ws.send(json.dumps(envelope(session_addr('workpulse', self.book.key_id), '', self.book.self_addr, item['payload']), ensure_ascii=False))
                            last_sent[token] = time.monotonic()
                    await asyncio.to_thread(runtime.flush_returns)
                await asyncio.sleep(.05)
        finally:
            runtime.disconnect()
