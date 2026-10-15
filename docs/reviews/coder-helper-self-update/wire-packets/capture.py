"""Regenerate local helper packet examples; never connects to a real Hub.

Run from repository root with an isolated HOME, locked helper Python and
PYTHONPATH=. ; argv[1] is the output JSON path. The tests create loopback HTTP
and private temporary state only. Hub confirmation is a synthetic input.
"""
import asyncio,json,sys
from pathlib import Path
from tools.sessionhelper.tests import test_self_update_offer as tests
tests.setUpModule()
try:packets=asyncio.run(tests.capture_wire_packet_fixtures())
finally:tests.tearDownModule()
Path(sys.argv[1]).write_text(json.dumps(packets,indent=2,ensure_ascii=False)+'\n')
print('actual helper packets captured; synthetic Hub echo only')
