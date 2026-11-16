"""One leased writer for the durable, ordinary-send responsibility journal."""
import asyncio
import fcntl
import json
import os
import sqlite3
import time
import uuid

from . import __version__
from .outbound_queue import OutboundQueue, QueueRejected, QueueCommitUnknown
from .outbound_receipt import OutboundRejected, OutboundReceiptError


class OutboundRecovery:
    def __init__(self, path, broker, source_address):
        self.queue = OutboundQueue(path)
        self.broker, self.source_address = broker, source_address
        self.owner = uuid.uuid4().hex
        self._catalog_generation = None
        self._last_storage_error = 0.0

    def handler(self):
        async def send(to, body, meta=None):
            # An old IPC caller with no durable caller ID cannot receive the
            # stronger guarantee. Never silently give it a new retry identity.
            return {'phase':'rejected','accepted_by':'none','outcome':'definitive_failure',
                    'reason':'request_id_required','retryable':False}
        async def submit(to, body, meta, request_id):
            try:
                return await asyncio.to_thread(self.queue.submit,request_id,to,body,meta)
            except (QueueCommitUnknown,sqlite3.Error,OSError):
                return {'phase':'unknown','accepted_by':'unknown','outcome':'unknown','reason':'local_queue_unavailable','request_id':request_id,'status_ref':request_id,'retryable':False}
            except QueueRejected as exc:
                return {'phase':'rejected','accepted_by':'none','outcome':'definitive_failure',
                        'reason':exc.reason,'retryable':exc.retryable,'request_id':request_id}
        async def status(request_id):
            if not isinstance(request_id,str) or not 0<len(request_id)<=128:
                return {'phase':'rejected','accepted_by':'none','outcome':'definitive_failure','reason':'invalid_request_id','retryable':False}
            return await asyncio.to_thread(self.queue.status,request_id)
        send.submit_request, send.status = submit, status
        return send

    async def run(self):
        # The process lock complements per-row leases: no second helper can
        # operate this outbox concurrently, even for different pending rows.
        fd=os.open(self.queue.path+'.writer',os.O_CREAT|os.O_RDWR|os.O_NOFOLLOW,0o600)
        try:
            try: fcntl.flock(fd,fcntl.LOCK_EX|fcntl.LOCK_NB)
            except BlockingIOError: raise RuntimeError('outbound writer already active') from None
            while True:
                try:
                    await self.step()
                except (sqlite3.Error,OSError):
                    now=time.monotonic()
                    if now-self._last_storage_error>=10:
                        print('[outbound] status=unavailable reason=local_queue_unavailable',flush=True)
                        self._last_storage_error=now
                    await asyncio.sleep(1)
                await asyncio.sleep(0.2)
        finally:
            os.close(fd)

    async def step(self):
        broker=self.broker
        ws,cap=broker.connection,broker.recovery
        if ws is None or not broker.negotiated: return
        if not cap:
            await asyncio.to_thread(self.queue.unsupported_peer)
            return  # Old Hub: no body write and an explicit local result.
        if self._catalog_generation is not cap:
            await asyncio.to_thread(self.queue.update_peer,cap['sender_id'],cap['targets'])
            self._catalog_generation=cap
        row=await asyncio.to_thread(self.queue.claim,self.owner,cap['sender_id'])
        if not row:return
        phase,reason,delivery='unknown','outbound_confirmation_unavailable',''
        try:
            meta=json.loads(row['meta']);meta.setdefault('type','agent_route')
            result=await broker.send(ws,row['target']+'#enrolled',row['body'],self.source_address(),meta,
                                     source_id=row['source_id'],recovery=row,timeout=10)
            phase,reason,delivery='hub_durable_queued','',result['delivery_id']
        except OutboundRejected as exc:
            phase,reason='rejected',getattr(exc,'reason','outbound_rejected')
        except OutboundReceiptError:
            pass # Same source ID is recovered on a negotiated generation only.
        except asyncio.CancelledError:
            # Keep the same durable unknown intent; cancellation is not rejection.
            raise
        finally:
            print('[outbound] '+json.dumps({'source_message_id':row['source_id'],'sender_agent_id':row['sender_id'],'target_agent_id':row['target_id'],'attempt':row['attempts'],'phase':phase,'reason':reason,'delivery_id':delivery,'helper_version':__version__},sort_keys=True),flush=True)
            # Cancellation can race a committed Hub receipt. Unknown is honest;
            # the next writer recovers its existing receipt with the same ID.
            await asyncio.to_thread(self.queue.finish,row['request_id'],self.owner,
                                    phase=phase,reason=reason,delivery_id=delivery)
