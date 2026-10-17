"""Controlled awaited-completion delay, confined to a test wrapper; product unchanged."""
import asyncio
import json
import sys
import unittest
from unittest import mock
import test_sessionhelper as tests

original = tests.SessionHelper.process_inbound_message
observations = []


async def delayed_completion(helper, ws, env, *args, **kwargs):
    result = await original(helper, ws, env, *args, **kwargs)
    if env['body'] == 'two':
        release = asyncio.Event()
        # A permitted scheduler delay after handle, before drain can clear busy ack.
        asyncio.get_running_loop().call_later(0.2, release.set)
        try:
            await release.wait()
            observations.append('last-completion-released')
        except asyncio.CancelledError:
            observations.append('cancelled-before-last-completion')
            raise
    return result


if __name__ == '__main__':
    with mock.patch.object(tests.SessionHelper, 'process_inbound_message', delayed_completion):
        result = unittest.TextTestRunner(verbosity=2).run(unittest.TestSuite([
            tests.SessionHelperTest('test_busy_buffer_queues_busy_messages_and_drains_fifo')]))
    print('COMPLETION_PROBE=' + json.dumps(observations))
    sys.exit(not result.wasSuccessful())
