import importlib.util,os,pathlib,time,unittest
p=pathlib.Path(os.environ['DID_TEST_COPY'])
spec=importlib.util.spec_from_file_location('delayed_identity_fixture',p)
m=importlib.util.module_from_spec(spec);spec.loader.exec_module(m)
original=m.DaemonWiring.setUp
class Delayed(list):
 def append(self,value):
  if value=='initialized':
   if os.environ['DID_DROP']=='1':return
   time.sleep(.2)
  super().append(value)
def setup(self):
 original(self);self.peer.methods=Delayed()
m.DaemonWiring.setUp=setup
suite=unittest.TestSuite([m.DaemonWiring('test_default_observe_wrong_exe_warns_and_connects')])
result=unittest.TextTestRunner(verbosity=2).run(suite)
raise SystemExit(0 if result.wasSuccessful() else 1)
