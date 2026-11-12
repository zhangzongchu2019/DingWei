"""One seat owns one native reader/writer; lane facades never consume each other's events."""
from __future__ import annotations

from collections import deque
from concurrent.futures import Future
from dataclasses import dataclass
import queue
import threading
import time


@dataclass(frozen=True)
class LaneIdentity:
    lane_id: str = ''
    generation: int = 1


class NativeToolUnavailable(RuntimeError):
    """A helper-owned, safe-to-display control error."""


class LocalDispatchRejected(PermissionError):
    not_written = True


class SeatReadBudget:
    """Seat-wide bounded read round, shared across adapters, never per-lane 200."""
    def __init__(self, clock=time.monotonic):
        self.clock = clock
        self._lock = threading.Lock()
        self._start = float('-inf')
        self._remaining = 200

    def available(self):
        with self._lock:
            now = self.clock()
            if now - self._start >= 15:
                self._start, self._remaining = now, 200
            return self._remaining

    def consume(self, count):
        with self._lock:
            self._remaining = max(0, self._remaining - count)


class LaneRpcFacade:
    def __init__(self, broker, identity, thread_id='', *, root=False):
        self.broker, self.identity = broker, identity
        self.thread_id, self.root = thread_id, root
        self.notifications = queue.Queue(maxsize=1000)
        self.closed = False
        self.on_turn_start = None
        self.write_guard = None
        self.read_budget = broker.read_budget
        self.readonly_threads = set()

    @property
    def generation(self):
        return self.broker.generation

    @property
    def peer_identity(self):
        return getattr(self.broker.transport, 'peer_identity', None)

    @property
    def identity_warning(self):
        return getattr(self.broker.transport, 'identity_warning', None)

    @property
    def _write_started(self):
        return getattr(self.broker.transport, '_write_started', None)

    def healthy(self):
        return not self.closed and self.broker.healthy()

    def start(self):
        self.broker.ensure_started()

    def stop(self):
        self.broker.detach(self)

    def abort_transport(self):
        self.broker.recover()

    def bind_thread(self, thread_id):
        self.broker.bind(self, thread_id)

    def request(self, method, params=None, *, timeout=30):
        params = dict(params or {})
        if self.closed:
            raise ConnectionError('lane facade closed')
        if method == 'initialize':
            return self.broker.initialize(params)
        if method == 'thread/start':
            if not self.root or self.thread_id:
                raise PermissionError('native thread creation is manager-only')
        elif 'threadId' in params:
            allowed = {self.thread_id}
            if method in ('thread/read', 'thread/turns/list'):
                allowed |= self.readonly_threads
            if not params['threadId'] or params['threadId'] not in allowed:
                raise PermissionError('native request belongs to another lane')
        else:
            raise PermissionError('unscoped lane native request')
        result = self.broker.request(self, method, params, timeout=timeout)
        if method == 'thread/start':
            self.bind_thread(result['thread']['id'])
        return result

    def replace_root_thread(self, previous, params):
        return self.broker.replace_root_thread(self, previous, params)

    def notify(self, method, params=None):
        if method != 'initialized':
            raise PermissionError('unscoped native notification')
        # initialize/initialized are owned once by the seat broker.


class AppServerBroker:
    def __init__(self, transport, *, transport_factory=None, admission=None, queue_capacity=64):
        self.transport = transport
        self.transport.notifications.maxsize=1000
        self.progress_handler=None
        self.transport_factory = transport_factory
        self.admission = admission
        self.capacity = queue_capacity
        self._condition = threading.Condition(threading.RLock())
        self._queues = {}
        self._round = deque()
        self._control = deque()
        self._facades = {}
        self._threads = {}
        self._stop = threading.Event()
        self._init_lock = threading.Lock()
        self._recovery_lock = threading.Lock()
        self._root_rotation_lock = threading.Lock()
        self._initialized = False
        self._ready = threading.Event()
        self._initialize_params = None
        self.generation = 1
        self.read_budget = SeatReadBudget()
        self.quarantine = deque(maxlen=128)
        self.tool_handler = None
        self._tool_queue = queue.Queue(maxsize=32)
        self._calls = set()
        self._call_order = deque(maxlen=1024)
        self._writer = threading.Thread(target=self._write_loop, daemon=True, name='sub-agent-native-writer')
        self._reader = threading.Thread(target=self._read_loop, daemon=True, name='sub-agent-native-reader')
        self._tools = threading.Thread(target=self._tool_loop, daemon=True, name='sub-agent-native-tools')
        self._writer.start(); self._reader.start(); self._tools.start()
        self._watchdog = threading.Thread(target=self._watch_transport, daemon=True, name='sub-agent-transport-watchdog')
        self._watchdog.start()

    def facade(self, lane_id='', generation=1, thread_id='', *, root=False):
        identity = LaneIdentity(lane_id, generation)
        with self._condition:
            old = self._facades.get(identity)
            if old is not None and not old.closed:
                raise ValueError('one active facade per lane generation')
            facade = LaneRpcFacade(self, identity, root=root)
            self._facades[identity] = facade
            if thread_id:
                self.bind(facade, thread_id)
            return facade

    def bind(self, facade, thread_id):
        if not isinstance(thread_id, str) or not thread_id:
            raise ValueError('invalid native thread binding')
        with self._condition:
            owner = self._threads.get(thread_id)
            if owner is not None and owner is not facade:
                raise ValueError('native thread already belongs to another lane')
            if facade.thread_id and facade.thread_id != thread_id:
                raise ValueError('lane thread binding is immutable')
            facade.thread_id = thread_id
            self._threads[thread_id] = facade

    def replace_root_thread(self, facade, previous, params):
        # Only the helper's inherited root may rotate. Ordinary bind remains
        # immutable and child facades never receive manager creation privilege.
        with self._root_rotation_lock:
            with self._condition:
                if (not facade.root or facade.identity.lane_id or facade.closed
                        or facade.thread_id != previous
                        or self._threads.get(previous) is not facade):
                    raise PermissionError('root replacement binding mismatch')
            result = self.request(facade, 'thread/start', params)
            thread_id = result.get('thread', {}).get('id')
            with self._condition:
                if (not isinstance(thread_id, str) or not thread_id or thread_id == previous
                        or thread_id in self._threads or facade.closed
                        or self._threads.get(previous) is not facade):
                    raise ValueError('root replacement result binding mismatch')
                self._threads.pop(previous)
                facade.thread_id = thread_id
                self._threads[thread_id] = facade
            return result

    def detach(self, facade):
        with self._condition:
            facade.closed = True
            if self._threads.get(facade.thread_id) is facade:
                self._threads.pop(facade.thread_id)
            for job in self._queues.pop(facade.identity, ()):
                job[3].set_exception(ConnectionError('lane closed before native write'))
            self._round = deque(key for key in self._round if key != facade.identity)

    def healthy(self):
        return not self._stop.is_set() and self.transport.healthy()

    def ensure_started(self):
        if not self.healthy():
            self.recover()

    def initialize(self, params):
        with self._init_lock:
            if not self._initialized:
                self._initialize_params = params
                result = self.transport.request('initialize', params, timeout=15)
                self.transport.notify('initialized')
                self._initialize_result = result
                self._initialized = True
                self._ready.set()
            return self._initialize_result

    def request(self, facade, method, params, *, timeout=30):
        future = Future()
        # Queue wait is local. RPC/admission clocks begin in submit_request,
        # inside the sole physical writer after this job wins fair scheduling.
        with self._condition:
            if self._stop.is_set() or (facade is not None and facade.closed):
                raise ConnectionError('native broker closed')
            key = facade.identity if facade else None
            pending = self._control if facade is None else self._queues.setdefault(key, deque())
            if len(pending) >= self.capacity:
                raise OverflowError('native lane writer queue full')
            pending.append((facade, method, params, future))
            if facade is not None and key not in self._round:
                self._round.append(key)
            self._condition.notify()
        # No deadline while waiting locally. Physical write has its own recovery
        # watchdog; once submitted, the bounded native response wait starts.
        transport, token = future.result()
        return transport.wait_request(token, method, timeout=timeout)

    def _write_loop(self):
        controls = 0
        while not self._stop.is_set():
            with self._condition:
                self._condition.wait_for(lambda: self._stop.is_set() or self._control or self._round, timeout=.5)
                if self._stop.is_set(): return
                if self._control and (controls < 4 or not self._round):
                    job = self._control.popleft(); controls += 1
                elif self._round:
                    key = self._round.popleft(); pending = self._queues[key]
                    job = pending.popleft(); controls = 0
                    if pending: self._round.append(key)
                else: continue
            facade, method, params, future = job
            while not self._stop.is_set() and not self._ready.wait(.1):pass
            if self._stop.is_set():future.set_exception(ConnectionError('broker stopped'));return
            try:
                if facade is not None and facade.closed:
                    raise ConnectionError('lane closed before native write')
                transport = self.transport
                if method=='@response':
                    if params.pop('epoch')==self.generation:transport.respond(**params)
                    future.set_result(None)
                    continue
                if facade is not None and method=='turn/start' and facade.write_guard is not None:
                    try:facade.write_guard(params)
                    except Exception as exc:raise LocalDispatchRejected('lane closed before native write') from exc
                callback = facade.on_turn_start if facade is not None else None
                if self.admission is not None and method in ('thread/start', 'turn/start'):
                    with self.admission(transport):
                        token = transport.submit_request(method, params, on_write=callback)
                else:
                    token = transport.submit_request(method, params, on_write=callback)
                future.set_result((transport, token))
            except Exception as exc:
                future.set_exception(exc)

    def _read_loop(self):
        while not self._stop.is_set():
            transport, epoch = self.transport, self.generation
            try: message = transport.notifications.get(timeout=.2)
            except queue.Empty: continue
            if transport is not self.transport or epoch != self.generation:
                continue
            params = message.get('params') or {}
            thread_id = params.get('threadId')
            with self._condition:
                facade = self._threads.get(thread_id)
            if facade is None or facade.closed:
                self.quarantine.append(('unknown_thread', message.get('method')))
                if 'id' in message and 'method' in message:
                    self._reject_request(message, 'unknown native thread')
                continue
            if 'method' in message and 'id' in message:
                if message['method'] != 'item/tool/call':
                    self._reject_request(message, 'unsupported native request'); continue
                token = (facade.identity, thread_id, params.get('turnId'), params.get('callId'))
                if not params.get('callId') or token in self._calls:
                    self._reject_request(message, 'stale native call'); continue
                if len(self._call_order) == self._call_order.maxlen:
                    self._calls.discard(self._call_order[0])
                self._call_order.append(token); self._calls.add(token)
                try: self._tool_queue.put_nowait((epoch, facade, message))
                except queue.Full: self._reject_request(message, 'native tools busy')
            else:
                if self.progress_handler is not None:
                    try:self.progress_handler(facade,message)
                    except Exception:pass
                try: facade.notifications.put_nowait(message)
                except queue.Full:
                    self.quarantine.append(('lane_event_overflow', facade.identity.lane_id))
                    # Never silently mark an execution complete when its event
                    # stream overflowed. The driver will retain unknown evidence.

    def _enqueue_response(self,request_id,*,result=None,error=None,epoch=None):
        with self._condition:
            if len(self._control)>=self.capacity:
                self.quarantine.append(('response_overflow','native'));return
            self._control.append((None,'@response',{'request_id':request_id,'result':result,'error':error,
                'epoch':self.generation if epoch is None else epoch},Future()))
            self._condition.notify()

    def _reject_request(self,message,reason,epoch=None):
        self._enqueue_response(message['id'],error={'code':-32602,'message':reason},epoch=epoch)

    def _tool_loop(self):
        while not self._stop.is_set():
            try: epoch, facade, message = self._tool_queue.get(timeout=.2)
            except queue.Empty: continue
            try:
                if epoch != self.generation or facade.closed or self.tool_handler is None:
                    raise PermissionError('native tool source unavailable')
                result = self.tool_handler(facade, message['params'])
                self._enqueue_response(message['id'], epoch=epoch, result={'contentItems': [{'type': 'inputText', 'text': __import__('json').dumps(result, ensure_ascii=False)}], 'success': True})
            except NativeToolUnavailable as exc:
                self._reject_request(message, str(exc), epoch=epoch)
            except Exception:
                self._reject_request(message, 'native tool rejected',epoch=epoch)

    def _watch_transport(self):
        while not self._stop.wait(.5):
            if self.transport_factory is None or not self._initialized: continue
            writing = getattr(self.transport, '_write_started', None)
            if self.healthy() and (writing is None or time.monotonic() - writing < 60): continue
            try: self.recover()
            except Exception: self.quarantine.append(('transport_recovery_failed', 'native'))

    def _close_transport(self):
        # A close frame can block behind a stuck socket send. Shutdown the local
        # connection first; never signal a shared daemon process.
        sock = getattr(self.transport, '_sock', None)
        if sock is not None:
            try: sock.shutdown(__import__('socket').SHUT_RDWR)
            except OSError: pass
        self.transport.stop()

    def recover(self,*,stuck_after=60):
        with self._recovery_lock:
            if self._stop.is_set(): raise ConnectionError('broker stopped')
            writing = getattr(self.transport, '_write_started', None)
            if self.healthy() and (writing is None or time.monotonic() - writing < stuck_after):
                return False
            if self.transport_factory is None:
                raise ConnectionError('broker transport recovery unavailable')
            self._ready.clear()
            self._close_transport()
            transport = self.transport_factory()
            transport.notifications.maxsize=1000
            transport.start()
            self.transport = transport
            self.generation += 1
            self._initialized = False
            if self._initialize_params is not None:
                self.initialize(self._initialize_params)
            return True

    def shutdown(self):
        self._stop.set()
        self._ready.set()
        with self._condition:
            for facade in list(self._facades.values()): self.detach(facade)
            for job in self._control: job[3].set_exception(ConnectionError('broker stopped'))
            self._control.clear(); self._condition.notify_all()
        self._close_transport()
