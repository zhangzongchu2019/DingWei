import json
import unittest
import urllib.error
from unittest.mock import patch
from sessionhelper import pilot_template as api,mcp_shim

def receipt():return dict(action='responsibility',draft_id='draft',request_id='stable',expected_version=1,plan_sha256='a'*64,entry_ids=['R1'],role='executor',decision='accepted')
class PilotTemplateTest(unittest.TestCase):
 def test_roles_and_human_boundary(self):
  for action in ({'action':'list'},{'action':'get','draft_id':'draft'},receipt()):
   with patch.object(mcp_shim,'_ipc_request',return_value={}) as call:mcp_shim._dw_spec_template(action)
   self.assertEqual(call.call_args.args[0]['type'],'pilot_template_v1')
  for key,value in [('role','signoff'),('role','full_review'),('action','confirm'),('action','save'),('confirm_human',True),('owner','other'),('credential','x'),('url','https://outside.invalid'),('after','leak')]:
   with patch.object(mcp_shim,'_ipc_request'),self.assertRaises(ValueError,msg='TEMPLATE_MCP_HUMAN_BOUNDARY'):mcp_shim._dw_spec_template(dict(receipt(),**{key:value}))
 def test_strict_version_and_hash(self):
  for k,v in [('expected_version',True),('expected_version',0),('expected_version','1'),('plan_sha256','A'*64),('entry_ids',['R1','R1']),('entry_ids',[]),('entry_ids',['R1\nforge'])]:
   with self.assertRaises(ValueError,msg='TEMPLATE_MCP_PLAN_CAS_WEAK'):mcp_shim._dw_spec_template(dict(receipt(),**{k:v}))
 def test_list_get_disjoint(self):
  for args in ({'action':'list','draft_id':'x'},{'action':'get','draft_id':'x','after':'y'},{'action':'list','request_id':'x'},{'action':'get'}):
   with self.assertRaises(ValueError,msg='TEMPLATE_MCP_READ_SCOPE_WEAK'):mcp_shim._dw_spec_template(args)
 def test_old_helper_fails_closed(self):
  with patch.object(mcp_shim,'_ipc_request',side_effect=mcp_shim._IPCFailure('helper_rejected')) as call:
   with self.assertRaisesRegex(mcp_shim._IPCFailure,'template_unsupported_upgrade_helper'):mcp_shim._dw_spec_template(receipt())
  self.assertEqual(call.call_count,1)
 def test_unknown_never_retried_or_secret_returned(self):
  req={'action':'responsibility','draft_id':'draft','body':{k:v for k,v in receipt().items() if k not in ('action','draft_id')}}
  for e in (OSError('PRIVATE_TOKEN'),urllib.error.HTTPError('https://hub.invalid',503,'PRIVATE_TOKEN',{},None)):
   with patch('urllib.request.OpenerDirector.open',side_effect=e) as call:r=api.template_request('wss://hub.invalid/ws',{},req)
   self.assertEqual(r['outcome'],'unknown','TEMPLATE_UNKNOWN_AUTO_RETRY');self.assertFalse(r['retryable']);self.assertEqual(call.call_count,1);self.assertNotIn('PRIVATE_TOKEN',json.dumps(r))
 def test_one_new_visible_tool(self):
  r=mcp_shim._dispatch(mcp_shim.Request(id=1,jsonrpc='2.0',method='tools/list',params={}))
  names={x['name'] for x in r.result['tools']};self.assertEqual(len(names),25);self.assertIn('dw_spec_template',names,'TEMPLATE_MCP_UNREGISTERED')
if __name__=='__main__':unittest.main()
