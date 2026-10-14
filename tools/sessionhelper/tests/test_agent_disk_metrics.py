import json,stat,types,unittest
from unittest.mock import Mock
from sessionhelper.agent_disk_metrics import DiskSampler
class Collector(unittest.TestCase):
 def fixture(self):
  directory=types.SimpleNamespace(st_dev=42,st_ino=7,st_mode=stat.S_IFDIR|0o700)
  fs=types.SimpleNamespace(f_blocks=100,f_bfree=40,f_bavail=30,f_frsize=4096)
  calls=types.SimpleNamespace(open=Mock(return_value=9),fstat=Mock(return_value=directory),fstatvfs=Mock(return_value=fs),stat=Mock(return_value=directory),close=Mock())
  clock=[10.0];sampler=DiskSampler(instance='a'*32,utc=lambda:1000,monotonic=lambda:clock[0],syscalls=calls)
  return sampler,calls,clock
 def test_correct_bytes_reserved_separate(self):
  s,o,c=self.fixture();d=json.loads(s.wire(s.collect('/verified/work')));self.assertEqual(d['used_bytes'],60*4096);self.assertEqual(d['available_bytes'],30*4096);self.assertEqual(d['total_bytes'],100*4096);o.fstatvfs.assert_called_once_with(9);o.close.assert_called_once_with(9)
 def test_valid_actual_zero(self):
  s,o,c=self.fixture();o.fstatvfs.return_value.f_bfree=100;d=json.loads(s.wire(s.collect('/work')));self.assertEqual(d['status'],'ok');self.assertEqual(d['used_bytes'],0)
 def test_no_workdir_no_syscalls(self):
  for path in (None,'','relative'):
   s,o,c=self.fixture();d=json.loads(s.wire(s.collect(path)));self.assertEqual(d['reason'],'workdir_unbound');o.open.assert_not_called()
 def test_paths_not_exported(self):
  s,o,c=self.fixture();wire=s.wire(s.collect('/private/agent/workdir'));self.assertNotIn('/private',wire);self.assertNotIn('agent_id',wire);self.assertLess(len(wire.encode()),2048)
 def test_rename_invalidates_and_closes(self):
  s,o,c=self.fixture();o.stat.return_value=types.SimpleNamespace(st_dev=42,st_ino=8,st_mode=stat.S_IFDIR|0o700);d=json.loads(s.wire(s.collect('/work')));self.assertEqual(d['status'],'unavailable');self.assertNotIn('used_bytes',d);o.close.assert_called_once()
 def test_symlink_named_path_invalidates(self):
  s,o,c=self.fixture();o.stat.return_value=types.SimpleNamespace(st_dev=42,st_ino=7,st_mode=stat.S_IFLNK|0o777);d=json.loads(s.wire(s.collect('/work')));self.assertEqual(d['status'],'unavailable')
 def test_error_no_raw_exception(self):
  s,o,c=self.fixture();o.fstatvfs.side_effect=OSError('private-path-secret');wire=s.wire(s.collect('/work'));self.assertNotIn('private-path-secret',wire);self.assertEqual(json.loads(wire)['status'],'unavailable');o.close.assert_called_once()
 def test_unsafe_or_bool_values_rejected(self):
  for value in [True,-1,2**53]:
   s,o,c=self.fixture();o.fstatvfs.return_value.f_blocks=value;self.assertEqual(json.loads(s.wire(s.collect('/work')))['status'],'unavailable')
 def test_delayed_send_does_not_refresh(self):
  s,o,c=self.fixture();v=s.collect('/work');c[0]=16
  with self.assertRaisesRegex(ValueError,'sample_too_old'):s.wire(v)
 def test_backwards_clock_rejected(self):
  s,o,c=self.fixture();v=s.collect('/work');c[0]=9
  with self.assertRaisesRegex(ValueError,'sample_too_old'):s.wire(v)
 def test_sequence_and_original_sample_time(self):
  s,o,c=self.fixture();first=json.loads(s.wire(s.collect('/work')));second=json.loads(s.wire(s.collect('/work')));self.assertEqual((first['sample_seq'],second['sample_seq']),('1','2'));self.assertEqual(first['sampled_at_ms'],1000000)
 def test_close_failure_unavailable(self):
  s,o,c=self.fixture();o.close.side_effect=OSError();self.assertEqual(json.loads(s.wire(s.collect('/work')))['status'],'unavailable')

class ProcessBinding(unittest.TestCase):
 def test_owned_process_namespace_and_cwd(self):
  from sessionhelper.agent_disk_metrics import process_workdir
  calls=types.SimpleNamespace(readlink=Mock(side_effect=['mnt:[1]','mnt:[1]','/agent/work','mnt:[1]']))
  self.assertEqual(process_workdir(123,lambda:True,syscalls=calls),'/agent/work')
  self.assertEqual([x.args[0] for x in calls.readlink.call_args_list],['/proc/self/ns/mnt','/proc/123/ns/mnt','/proc/123/cwd','/proc/123/ns/mnt'])
 def test_unbound_or_changed_process_is_unknown(self):
  from sessionhelper.agent_disk_metrics import process_workdir
  for links in [['a','b'],['a','a','/work (deleted)'],['a','a','relative'],['a','a','/work','b']]:
   calls=types.SimpleNamespace(readlink=Mock(side_effect=links));self.assertIsNone(process_workdir(123,lambda:True,syscalls=calls))
  calls=types.SimpleNamespace(readlink=Mock());self.assertIsNone(process_workdir(123,lambda:False,syscalls=calls));calls.readlink.assert_not_called()
 def test_adapter_uses_owned_pid_not_configured_path(self):
  from sessionhelper.driver_adapter import DriverAdapter
  from sessionhelper.cli import CLIAdapter
  from unittest.mock import patch
  proc=types.SimpleNamespace(pid=123,poll=lambda:None)
  with patch('sessionhelper.agent_disk_metrics.process_workdir',return_value='/actual') as get:
   for driver in ['codex','claude','opencode']:
    obj=object.__new__(DriverAdapter);obj._started=True;obj._which=driver;obj.driver=types.SimpleNamespace(_rpc=types.SimpleNamespace(_proc=proc),_proc=proc)
    self.assertEqual(obj.agent_metrics_workdir(),'/actual');self.assertEqual(get.call_args.args[0],123)
   obj._started=False;self.assertIsNone(obj.agent_metrics_workdir())
   cli=object.__new__(CLIAdapter);cli.launch_mode='tmux';cli.child=types.SimpleNamespace(pid=123,isalive=lambda:True)
   self.assertIsNone(cli.agent_metrics_workdir());cli.launch_mode='pty';self.assertEqual(cli.agent_metrics_workdir(),'/actual')

class Publisher(unittest.IsolatedAsyncioTestCase):
 async def test_singleflight_timeout_and_late_discard(self):
  import asyncio,threading
  from sessionhelper.agent_disk_metrics import BoundedDiskPublisher
  entered=threading.Event();release=threading.Event();calls=[]
  class Sampler:
   def collect(self,path):calls.append(path);entered.set();release.wait(1);return 'sample'
   def wire(self,sample):return 'fresh'
  publisher=BoundedDiskPublisher(Sampler());adapter=object()
  try:
   self.assertIsNone(await publisher.sample(adapter,timeout=.01));self.assertTrue(entered.is_set())
   self.assertIsNone(await publisher.sample(adapter,timeout=.01));self.assertEqual(len(calls),1)
  finally:release.set()
  await asyncio.to_thread(publisher.pending.result,1)
  self.assertEqual(len(calls),1) # late result is never sent/re-aged by the expired awaiter
 async def test_workdir_change_strips_numbers(self):
  from sessionhelper.agent_disk_metrics import BoundedDiskPublisher
  sampler,syscalls,clock=Collector().fixture()
  adapter=types.SimpleNamespace(agent_metrics_workdir=Mock(side_effect=['/first','/second']))
  raw=await BoundedDiskPublisher(sampler).sample(adapter)
  data=json.loads(raw);self.assertEqual(data['status'],'unavailable');self.assertEqual(data['reason'],'namespace_unbound');self.assertNotIn('used_bytes',data)
 async def test_actual_loop_uses_same_ws_control_and_300_seconds(self):
  import asyncio
  from unittest.mock import AsyncMock,patch
  from sessionhelper.app import SessionHelper
  obj=object.__new__(SessionHelper);obj.adapter=object();obj.agent_disk_publisher=types.SimpleNamespace(sample=AsyncMock(return_value='metric'))
  obj.cfg=types.SimpleNamespace(key_id='synthetic');obj.book=types.SimpleNamespace(self_addr='agent#synthetic')
  ws=types.SimpleNamespace(send=AsyncMock(),response=types.SimpleNamespace(headers={"X-DW-Agent-Metrics":"agent-disk/v1"}))
  with patch('sessionhelper.app.asyncio.sleep',new=AsyncMock(side_effect=asyncio.CancelledError)) as sleep:
   with self.assertRaises(asyncio.CancelledError):await obj.agent_disk_metrics_loop(ws)
  sleep.assert_awaited_once_with(300);ws.send.assert_awaited_once()
  sent=json.loads(ws.send.call_args.args[0]);self.assertEqual(sent['body'],'metric');self.assertEqual(sent['meta']['type'],'agent_environment_metrics');self.assertTrue(sent['meta']['no_mirror']);self.assertEqual(sent['from'],'agent#synthetic')

class Compatibility(unittest.IsolatedAsyncioTestCase):
 async def test_old_hub_never_samples_or_routes_metadata(self):
  import asyncio
  from unittest.mock import AsyncMock
  from sessionhelper.app import SessionHelper
  obj=object.__new__(SessionHelper);obj.agent_disk_publisher=types.SimpleNamespace(sample=AsyncMock());ws=types.SimpleNamespace(send=AsyncMock())
  task=asyncio.create_task(obj.agent_disk_metrics_loop(ws));await asyncio.sleep(0);task.cancel()
  with self.assertRaises(asyncio.CancelledError):await task
  obj.agent_disk_publisher.sample.assert_not_called();ws.send.assert_not_called()

if __name__=='__main__':unittest.main(verbosity=2)
