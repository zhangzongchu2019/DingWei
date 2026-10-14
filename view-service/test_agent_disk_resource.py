"""Only recorders and mocked transports/OS; no HTTP listener or real sampling."""
import json
import threading
import time
import unittest
from unittest.mock import patch
import view_service as vs
import test_view_refresh as fixtures
Recorder, snapshot = fixtures.Recorder, fixtures.snapshot

class ResourceStatus(fixtures.ViewRefreshRecorderTest):
 def test_verified_subject_precedes_resource_fetch(self):
  disk={'agent_id':'stable','source':'helper_self_report','status':'unavailable'}
  with patch.object(vs,'_agent_disk_from_hub',return_value=disk) as fetch:
   for headers in [None,self.signed(True)]:
    h=Recorder('/status?resources_only=1',headers);h._serve_status();self.assertEqual(h.code,401)
   fetch.assert_not_called()
   h=Recorder('/status?resources_only=1',self.signed());h._serve_status()
   self.assertEqual(h.code,200);self.assertEqual(h.result()['agent_disk'],disk)
   fetch.assert_called_once_with('agent.owner',{'agent_id':'stable','full_name':'agent.owner'})
  self.assertEqual(snapshot(self.cost),self.before)
 def test_internal_request_signed_and_bound(self):
  def fake(make):
   req=make();self.assertEqual(req.method,'POST');self.assertTrue(req.full_url.endswith('/internal/view-v2/agent.owner/resources'))
   data=json.loads(req.data);self.assertEqual(data,{'agent_id':'stable','full_name':'agent.owner'})
   headers={k.lower():v for k,v in req.header_items()}
   self.assertEqual(headers['x-dw-vs-target'],'fixture')
   expected=vs._control_signature(self.key,'POST','/internal/view-v2/agent.owner/resources',headers['x-dw-timestamp'],headers['x-dw-nonce'],req.data,direction='view-service-to-hub/v1')
   self.assertEqual(headers['x-dw-signature'],expected)
   return {'agent_id':'stable','full_name':'agent.owner','agent_disk':{'agent_id':'stable','source':'helper_self_report','status':'ok'}}
  with patch.object(vs,'_bounded_agent_disk_fetch',side_effect=fake):
   self.assertEqual(vs._agent_disk_from_hub('agent.owner',{'agent_id':'stable'})['status'],'ok')
  for data in [None,{}, {'agent_id':'other','full_name':'agent.owner'}, {'agent_id':'stable','full_name':'agent.owner','agent_disk':{'agent_id':'other','source':'helper_self_report'}}]:
   with patch.object(vs,'_bounded_agent_disk_fetch',return_value=data):
    got=vs._agent_disk_from_hub('agent.owner',{'agent_id':'stable'});self.assertEqual(got['status'],'unavailable');self.assertNotIn('used_bytes',got)
  with patch.object(vs,'_bounded_agent_disk_fetch',side_effect=AssertionError('no verified subject')):
   self.assertEqual(vs._agent_disk_from_hub('agent.owner',None)['status'],'unavailable')

class BoundedTransport(unittest.TestCase):
 def test_oversize_and_parse_failure_unknown(self):
  class Response:
   status=200
   def __init__(self,body):self.body=body
   def __enter__(self):return self
   def __exit__(self,*args):pass
   def read(self,n):assert n==4097;return self.body
  for body in [b'x'*4097,b'{invalid',b'{"ok":true}']:
   class Opener:
    def open(self,*args,**kwargs):return Response(body)
   with patch.object(vs.urllib.request,'build_opener',return_value=Opener()):
    result=vs._bounded_agent_disk_fetch(lambda:object())
    self.assertEqual(result,{'ok':True} if body==b'{"ok":true}' else None)
 def test_entire_read_is_bounded_and_stuck_workers_not_multiplied(self):
  release=threading.Event();slots=threading.BoundedSemaphore(2);calls=[]
  def blocked():calls.append(1);release.wait(1);return None
  with patch.object(vs,'_AGENT_DISK_FETCH_SLOTS',slots):
   try:
    for _ in range(2):
     before=time.monotonic();self.assertIsNone(vs._bounded_agent_disk_fetch(blocked,timeout=.01));self.assertLess(time.monotonic()-before,.5)
    self.assertIsNone(vs._bounded_agent_disk_fetch(blocked,timeout=.01));self.assertEqual(len(calls),2)
   finally:release.set()
  # Await semaphore releases before restoring patched global, so all worker
  # ownership is accounted for; no kernel cancellation is asserted.
   for _ in range(2):self.assertTrue(slots.acquire(timeout=1))

if __name__=='__main__':unittest.main(verbosity=2)
