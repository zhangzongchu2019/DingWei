"""Authenticated resource binding and actual renderer state machine."""
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch
import view_service as vs
import test_view_refresh as fixtures

class InboxLagResource(fixtures.ViewRefreshRecorderTest):
    def test_lag_resource_subject_binding_and_existing_view_acl(self):
        lag={'agent_id':'stable','status':'ok','disk_not_admitted_count':1}
        payload={'agent_id':'stable','full_name':'agent.owner','agent_disk':{'agent_id':'stable','source':'helper_self_report','status':'unavailable'},'inbox_lag':lag}
        with patch.object(vs,'_bounded_agent_disk_fetch',return_value=payload):
            self.assertEqual(vs._agent_disk_from_hub('agent.owner',{'agent_id':'stable'})['inbox_lag'],lag)
            self.assertNotIn('inbox_lag',vs._agent_disk_from_hub('other.owner',{'agent_id':'other'}))
        with patch.object(vs,'_agent_disk_from_hub',return_value={'inbox_lag':lag}) as fetch:
            h=fixtures.Recorder('/status?resources_only=1',None);h._serve_status();self.assertEqual(h.code,401);fetch.assert_not_called()
            h=fixtures.Recorder('/status?resources_only=1',self.signed());h._serve_status();self.assertEqual(h.result()['agent_disk']['inbox_lag'],lag)

    def test_lag_only_poll_is_subject_bound_and_does_not_sample_resources_or_cost(self):
        lag={'agent_id':'stable','status':'ok','disk_not_admitted_count':1}
        with patch.object(vs,'_agent_disk_from_hub',return_value={'inbox_lag':lag}) as fetch, patch.object(vs,'_read_mem',side_effect=AssertionError('unneeded resource sampling')), patch.object(vs,'_read_disk',side_effect=AssertionError('unneeded resource sampling')), patch.object(vs,'_cost_projection',side_effect=AssertionError('cost touched')):
            for headers in (None,self.signed(True)):
                h=fixtures.Recorder('/status?inbox_lag_only=1',headers);h._serve_status();self.assertEqual(h.code,401)
            fetch.assert_not_called()
            h=fixtures.Recorder('/status?inbox_lag_only=1',self.signed());h._serve_status()
            self.assertEqual(h.result(),{'agent_id':'stable','inbox_lag':lag})
            fetch.assert_called_once_with('agent.owner',{'agent_id':'stable','full_name':'agent.owner'})
        self.assertEqual(fixtures.snapshot(self.cost),self.before)

class InboxLagRenderer(unittest.TestCase):
    def test_actual_state_40minute_queue_then_stale_unknown_and_epoch(self):
        html=Path(__file__).with_name('renderer-canary-sse.html').read_text()
        source=html.split('// INBOX_LAG_STATE_BEGIN',1)[1].split('\n',1)[1].split('// INBOX_LAG_STATE_END',1)[0]
        script=source+'''
const assert=require('assert');
const s={status:'ok',agent_id:'seat',source:'helper_self_report',disk_not_admitted_count:2,dispatch_unknown_count:1,connection_seq:73,connection_generation:'c'.repeat(32),sample_instance:'a'.repeat(32),sample_seq:1,remaining_fresh_ms:10000,oldest_wait_ms:2400000};
assert(inboxLagState.apply(s,'seat',0,100));
assert.equal(inboxLagState.display(100),'已到盘未注入 2 条，最老等待 40 分钟；另有 1 条注入状态未知');
assert(!inboxLagState.apply(s,'seat',9000,9000));
assert.equal(inboxLagState.display(10000),'排队采样已过期');
assert(!inboxLagState.apply({...s,connection_seq:72,sample_seq:99},'seat',10001,10001));
assert(!inboxLagState.display(10001).includes('0 条'));
assert(inboxLagState.apply({...s,sample_seq:2,disk_not_admitted_count:0,oldest_wait_ms:null,dispatch_unknown_count:0},'seat',11000,11000));
assert.equal(inboxLagState.display(11000),'已到盘未注入 0 条');
assert(!inboxLagState.apply({...s,sample_seq:3},'other',11001,11001));
assert.equal(inboxLagState.display(11001),'注入状态未知');
inboxLagState.apply({status:'unknown'},'seat',11002,11002);
assert.equal(inboxLagState.display(11002),'注入状态未知');
assert(!inboxLagState.apply({...s,connection_generation:'d'.repeat(32),sample_seq:99},'seat',11003,11003));
inboxLagState.clear();assert(inboxLagState.apply({...s,connection_seq:74},'seat',11004,11004));
assert.equal(inboxLagState.display(21004),'排队采样已过期');
'''
        result=subprocess.run(['node','-e',script],capture_output=True,text=True,timeout=10)
        self.assertEqual(result.returncode,0,result.stderr)
        self.assertIn("el.textContent=inboxLagState.display(performance.now())",html)

if __name__=='__main__':unittest.main()
