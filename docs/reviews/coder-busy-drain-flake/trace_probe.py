"""Observe unchanged test/product boundaries; no delays, clock patch or network."""
import asyncio
import json
import time
import unittest
from unittest import mock
import test_sessionhelper as tests

observations = []
original_loop = tests.SessionHelper.pending_drain_loop
original_process = tests.SessionHelper.process_inbound_message


def snapshot(helper, event):
    observations.append(dict(event=event, at=time.monotonic(),
                             pending=len(helper.pending_inbound),
                             handled=list(helper.adapter.handled),
                             busy_acked=sorted(helper.busy_acked_from)))


async def observed_loop(helper, ws):
    snapshot(helper, 'drain-start')
    try:
        return await original_loop(helper, ws)
    finally:
        snapshot(helper, 'drain-cancelled-or-finished')


async def observed_process(helper, ws, env, *args, **kwargs):
    snapshot(helper, 'process-start-' + env['body'])
    completed = False
    try:
        value = await original_process(helper, ws, env, *args, **kwargs)
        completed = True
        return value
    finally:
        snapshot(helper, ('process-complete-' if completed else 'process-interrupted-') + env['body'])


if __name__ == '__main__':
    with mock.patch.object(tests.SessionHelper, 'pending_drain_loop', observed_loop), mock.patch.object(tests.SessionHelper, 'process_inbound_message', observed_process):
        suite = unittest.TestSuite(tests.SessionHelperTest('test_busy_buffer_queues_busy_messages_and_drains_fifo') for _ in range(12))
        result = unittest.TextTestRunner(verbosity=1).run(suite)
    print('TRACE_JSON=' + json.dumps(observations))
    print('TRACE_RESULT=' + json.dumps(dict(run=result.testsRun, failures=len(result.failures), errors=len(result.errors))))
