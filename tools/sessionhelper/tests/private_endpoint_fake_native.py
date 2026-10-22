"""Real local Unix/WebSocket child for owner process tests. Never imports a CLI."""
import json
import os
from pathlib import Path
import sys
import threading

from websockets.sync.server import unix_serve


def main():
    assert len(sys.argv) == 4 and sys.argv[1:3] == ['app-server', '--listen']
    endpoint = sys.argv[3]
    assert endpoint.startswith('unix://')
    path = endpoint[7:]
    root = Path(path).parent
    trace = root / 'fixture-starts.jsonl'
    with trace.open('a') as stream:
        stream.write(json.dumps({'pid': os.getpid()}) + '\n')
    thread_id = '12345678-1234-1234-1234-123456789abc'
    state = {'busy': False}
    lock = threading.Lock()

    def handler(connection):
        for raw in connection:
            message = json.loads(raw)
            if 'id' not in message:
                continue
            method = message['method']
            with lock:
                result = {}
                if method == 'thread/loaded/list':
                    result = {'data': [thread_id], 'nextCursor': None}
                elif method in ('thread/start', 'thread/read', 'thread/resume'):
                    result = {'thread': {'id': thread_id, 'cwd': str(Path.cwd()),
                                        'status': {'type': 'active' if state['busy'] else 'idle'},
                                        'canAcceptDirectInput': True}}
                elif method == 'fixture/busy':
                    state['busy'] = True
                elif method == 'fixture/idle':
                    state['busy'] = False
            connection.send(json.dumps({'id': message['id'], 'result': result}))

    with unix_serve(handler, path=path, compression=None, open_timeout=2,
                    close_timeout=.1, max_size=65536, logger=__import__('logging').getLogger('fixture')) as server:
        os.chmod(path, 0o600)
        server.serve_forever()


if __name__ == '__main__':
    main()
