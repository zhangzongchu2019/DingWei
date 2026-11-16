"""Same-connection correlated Hub durable queue receipts (explicit opt-in).

No auto-resend. A nonce remains sender/Hub private and is removed before the
recipient queue. Queue acceptance is not recipient read or model execution.
"""
import asyncio
import re
import json
import secrets
from .protocol import envelope


class OutboundReceiptError(Exception):
    def __init__(self, message, *, source_message_id=""):
        super().__init__(message)
        self.source_message_id = source_message_id


class OutboundRejected(OutboundReceiptError):
    pass


class OutboundReceiptBroker:
    def __init__(self, session_names):
        self.session_names = frozenset(name.casefold() for name in session_names if name)
        self.source_address = next((name for name in session_names if name), "") + "#enrolled"
        self.pending = {}
        self.status_pending = {}
        self.connection = None
        self.cap_nonce = ""
        self.negotiated = False
        self.recovery = None

    def prepare(self, ws):
        if self.connection is not None: self.abort(self.connection)
        self.connection = ws
        self.cap_nonce = secrets.token_hex(16)
        self.negotiated = False
        self.recovery = None
        return {'outbound_receipt_v1': True, 'outbound_receipt_cap_nonce': self.cap_nonce}

    def valid_hub_address(self, env):
        source = str(env.get('from') or '').split('#')
        target = str(env.get('to') or '').split('#')
        return (len(source)==2 and len(target)==2 and source[0]=='workpulse' and
                target[0].casefold() in self.session_names and bool(source[1]) and source[1]==target[1])

    async def send(self, ws, to, body, from_addr, meta=None, *, timeout=10, observational=False, source_id=None, recovery=None):
        if self.connection is not ws or not self.negotiated:
            raise OutboundReceiptError("Hub durable receipt capability not negotiated")
        if not isinstance(body, str) or len(body.encode('utf-8')) > ((1 << 20) if recovery is not None else 65536):
            raise ValueError('reply too large or invalid')
        if not isinstance(to, str) or not to or len(to)>256 or any(c.isspace() for c in to):
            raise ValueError('invalid explicit target')
        if not 0 < timeout <= 30 or len(self.pending) >= 32:
            raise ValueError('receipt wait limit')
        nonce = secrets.token_hex(16)
        metadata = dict(meta or {})
        metadata['inbox_receipt_v1' if observational else 'outbound_receipt_v1'] = True
        metadata['outbound_receipt_nonce'] = nonce
        env = envelope(to, body, from_addr, metadata)
        if recovery is not None:
            if not self.recovery or not isinstance(source_id,str) or not re.fullmatch(r'[0-9a-f]{32}',source_id):
                raise OutboundReceiptError('durable recovery not negotiated')
            env['id']=source_id
            metadata.update({'outbound_recovery_v1':True,'outbound_sender_id':recovery['sender_id'],'outbound_target_id':recovery['target_id']})
        future = asyncio.get_running_loop().create_future()
        pending=(ws,nonce,future)
        self.pending[env['id']] = pending
        try:
            async with asyncio.timeout(timeout):
                await ws.send(json.dumps(env, ensure_ascii=False))
                return await future
        except OutboundReceiptError as exc:
            exc.source_message_id = env['id']
            raise
        except Exception:
            raise OutboundReceiptError('outbound outcome uncertain', source_message_id=env['id']) from None
        finally:
            if self.pending.get(env['id']) is pending:
                self.pending.pop(env['id'], None)
            if not future.done():
                future.cancel()
            elif not future.cancelled():
                future.exception()  # consume an abort error if cancellation won during send

    def receive(self, ws, env):
        meta = env.get('meta') or {}
        if not isinstance(meta, dict): return False
        if meta.get('type') == 'outbound_capabilities':
            if (self.connection is ws and self.valid_hub_address(env) and
                    meta.get('nonce') == self.cap_nonce and type(meta.get('version')) is int and
                    meta['version']==1 and meta.get('durable_agent_queue') is True):
                self.negotiated = True
                if (meta.get('outbound_recovery_v1') is True and isinstance(meta.get('sender_agent_id'),str) and 0<len(meta['sender_agent_id'])<=256
                        and isinstance(meta.get('targets'),list) and len(meta['targets'])<=1024):
                    self.recovery = {'sender_id':meta['sender_agent_id'], 'targets':meta['targets'], 'targets_available':meta.get('targets_available',True) is True}
            return True
        if meta.get('type') == 'delivery_status_result_v1':
            pending = self.status_pending.get(meta.get('nonce')) if isinstance(meta.get('nonce'), str) else None
            if pending is not None:
                connection, delivery_id, future = pending
                result = meta.get('result')
                if (connection is ws and self.valid_hub_address(env) and not future.done()
                        and meta.get('delivery_id') == delivery_id and isinstance(result, dict)):
                    if result.get('ok') is False or result.get('delivery_id') == delivery_id:
                        future.set_result(result)
            return True
        if meta.get('type') != 'outbound_receipt': return False
        # Control receipts never become inbox/AI input, even when malformed.
        pending = self.pending.get(meta.get('source_message_id')) if isinstance(meta.get('source_message_id'), str) else None
        if pending is None: return True
        connection, nonce, future = pending
        source = str(env.get('from') or '').split('#')
        target = str(env.get('to') or '').split('#')
        if (connection is not ws or future.done() or meta.get('nonce') != nonce or
                len(source)!=2 or len(target)!=2 or source[0]!='workpulse' or
                target[0].casefold() not in self.session_names or not source[1] or source[1]!=target[1] or
                type(meta.get('version')) is not int or meta['version']!=1):
            return True
        if (meta.get('state')=='queued' and meta.get('durable') is True and
                isinstance(meta.get('delivery_id'), str) and re.fullmatch(r'dw-dlv_[0-9a-f]{32}',meta['delivery_id'])):
            future.set_result({'ok': True, 'stage': 'hub_durable_queued',
                               'source_message_id':meta['source_message_id'], 'delivery_id':meta['delivery_id']})
        elif meta.get('state')=='unknown' and meta.get('durable') is False:
            error=OutboundReceiptError('Hub confirmation unavailable')
            reason=meta.get('reason')
            error.reason=reason if isinstance(reason,str) and re.fullmatch(r'[a-z_]{1,64}',reason) else 'outbound_confirmation_unavailable'
            future.set_exception(error)
        elif meta.get('state')=='rejected' and meta.get('durable') is False:
            reason=meta.get('reason')
            error=OutboundRejected('Hub rejected outbound request')
            error.reason=reason if isinstance(reason,str) and re.fullmatch(r'[a-z_]{1,64}',reason) else 'outbound_rejected'
            future.set_exception(error)
        return True

    async def query_status(self, ws, delivery_id, *, timeout=2):
        if (self.connection is not ws or not self.negotiated
                or not isinstance(delivery_id, str) or not re.fullmatch(r'dw-dlv_[0-9a-f]{32}', delivery_id)
                or len(self.status_pending) >= 32):
            return {'ok': False, 'status': 'unknown'}
        nonce = secrets.token_hex(16)
        future = asyncio.get_running_loop().create_future()
        self.status_pending[nonce] = (ws, delivery_id, future)
        try:
            # Actual sender identity is rewritten/bound by Hub credential admission.
            control = envelope('workpulse#enrolled', '', self.source_address,
                               {'type': 'delivery_status_query_v1', 'delivery_id': delivery_id,
                                'nonce': nonce, 'no_mirror': True})
            async with asyncio.timeout(timeout):
                await ws.send(json.dumps(control))
                return await future
        except Exception:
            # A status transport failure cannot undo a sent body or cause replay.
            # CancelledError remains cancellation (BaseException), with cleanup below.
            return {'ok': False, 'status': 'unknown'}
        finally:
            self.status_pending.pop(nonce, None)
            if not future.done(): future.cancel()
            elif not future.cancelled(): future.exception()

    def abort(self, ws):
        for connection, _, future in list(self.status_pending.values()):
            if connection is ws and not future.done():
                future.set_exception(OutboundReceiptError("connection closed"))
        if self.connection is ws:
            self.connection = None
            self.recovery = None
            self.cap_nonce = ""
            self.negotiated = False
        for connection, _, future in list(self.pending.values()):
            if connection is ws and not future.done():
                future.set_exception(OutboundReceiptError('connection closed; outcome uncertain'))
