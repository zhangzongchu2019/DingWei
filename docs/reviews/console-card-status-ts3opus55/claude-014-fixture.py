"""Map a synthetic frame through frozen 0.2.3.14 code; never start a CLI."""
import dataclasses
import json
import pathlib
import subprocess
import sys
import threading
import types

anchor = 'edaf40e60ac522decedefee56affe861d0545aaf'
source = subprocess.check_output(['git', 'show', anchor + ':tools/sessionhelper/sessionhelper/claudeDriver.py'], text=True)
module = types.ModuleType('sessionhelper._claude014_field_fixture')
module.__file__ = '<frozen-git-claude014-fixture>'
sys.modules[module.__name__] = module
exec(compile(source, module.__file__, 'exec'), module.__dict__)
driver = object.__new__(module.ClaudeProtocolDriver)
driver._active_lock = threading.Lock()
driver._session_id = 'synthetic-session'
driver._active = types.SimpleNamespace(envelope_id='dw-dlv_a073022f53991cacfa695447539a0d75', turn_id='synthetic-turn', last_event_at=0, stalled_logged=False)
events = []
driver._emit = events.append
driver._handle_frame({'type':'assistant','message':{'content':[{'type':'text','text':'synthetic answer'}]}})
assert len(events) == 1
payload = dataclasses.asdict(events[0])
assert payload['delivery_id'] == driver._active.envelope_id
assert payload['kind'] == 'assistant_text'
print(json.dumps({'source_anchor':anchor,'version_reference':'0.2.3.14','binding':'historical source reference only; deployed binary/source hash unknown','event':payload}))
