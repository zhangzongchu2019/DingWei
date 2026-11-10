import io
import os
import sqlite3
import tempfile
import threading
import unittest
from view_service import CostProjection
import view_service as vs

class ProjectionTest(unittest.TestCase):
    def test_duplicate_restart_and_concurrent_legacy_aggregate(self):
        with tempfile.TemporaryDirectory() as root:
            path=os.path.join(root,'cost.db')
            projection=CostProjection(path)
            event={'kind':'state_change','data':{'change':'turn_completed','cost_usd':1.25}}
            workers=[threading.Thread(target=projection.apply,args=('id','agent.owner','deadbeef-9007199254740993',event)) for _ in range(12)]
            for w in workers:w.start()
            for w in workers:w.join()
            self.assertEqual(projection.status('id'),{'cost_total':1.25,'cost_last':1.25,'turns':1})
            reopened=CostProjection(path)
            self.assertFalse(reopened.apply('id','agent.owner','deadbeef-9007199254740993',event))
            self.assertEqual(reopened.status('id')['cost_last'],1.25)
            reopened.apply('id','agent.owner','deadbeef-9007199254740994',{'kind':'state_change','data':{'change':'turn_completed','cost_usd':2,'num_turns':2}})
            self.assertEqual(reopened.status('id'),{'cost_total':2.,'cost_last':.75,'turns':2})
            # A new stable identity with the same display name cannot inherit previous costs.
            self.assertEqual(reopened.status('new-id')['turns'],0)
    def test_rollback_cannot_mark_failed_projection_seen(self):
        with tempfile.TemporaryDirectory() as root:
            p=CostProjection(os.path.join(root,'cost.db'))
            event={'kind':'state_change','data':{'change':'turn_completed','cost_usd':1}}
            with sqlite3.connect(p.path) as db:
                db.execute("CREATE TRIGGER fail BEFORE INSERT ON mux_cost BEGIN SELECT RAISE(ABORT,'disk-side failure'); END")
            with self.assertRaises(sqlite3.IntegrityError):p.apply('id','a.b','deadbeef-1',event)
            with sqlite3.connect(p.path) as db:
                self.assertEqual(db.execute('SELECT count(*) FROM mux_completed').fetchone()[0],0)
                db.execute('DROP TRIGGER fail')
            self.assertTrue(p.apply('id','a.b','deadbeef-1',event))
    def test_canonical_uint64_and_frame_caps(self):
        with tempfile.TemporaryDirectory() as root:
            p=CostProjection(os.path.join(root,'cost.db'))
            for cursor in ('deadbeef-01','deadbeef-18446744073709551616','xyz-2','deadbeef-1\n'):
                with self.assertRaises(ValueError):p.apply('id','a.b',cursor,{'kind':'test'})
        frame=b'data: '+b'x'*(vs.MUX_FRAME_MAX-8)+b'\n\n'
        self.assertEqual(len(next(vs._bounded_sse_frames(io.BytesIO(frame)))),vs.MUX_FRAME_MAX)
        with self.assertRaises(ValueError):list(vs._bounded_sse_frames(io.BytesIO(frame[:-2]+b'x\n\n')))
        with self.assertRaises(ValueError):list(vs._bounded_sse_frames(io.BytesIO(b'data: partial')))


class SubAgentScopeProjectionTest(unittest.TestCase):
    def subject(self, node='a', generation=1):
        import hashlib
        identity='sub-v1-'+hashlib.sha256(('root\x00'+node+'\x00'+str(generation)).encode()).hexdigest()
        return dict(agent_id=identity,full_name=node+'.agent.owner',root_agent_id='root',node_id=node,lane_generation=generation,viewer_session_hash='synthetic-viewer')

    def test_aa010_sibling_cost_cursor_and_legacy_survive_restart(self):
        with tempfile.TemporaryDirectory() as root:
            p=CostProjection(os.path.join(root,'projection.db'))
            event=lambda cost:dict(kind='state_change',data=dict(change='turn_completed',cost_usd=cost))
            p.apply('root','agent.owner','a-1',event(7))
            a,b,a2=self.subject(),self.subject('b'),self.subject(generation=2)
            for sub,cost in ((a,1),(b,3),(a2,5)):
                self.assertTrue(p.apply(sub['agent_id'],sub['full_name'],'a-1',event(cost),subject=sub),'AA010_SIBLING_COST')
            p=CostProjection(p.path)
            self.assertEqual([p.status(x['agent_id'])['cost_total'] for x in (a,b,a2)],[1,3,5],'AA010_SIBLING_COST')
            self.assertEqual(p.status('root')['cost_total'],7,'AA010_LEGACY_COST_PRESERVED')
            self.assertFalse(p.apply(a['agent_id'],a['full_name'],'a-1',event(1),subject=a))
            p.apply(a['agent_id'],a['full_name'],'a-9',dict(kind='assistant_text'),subject=a)
            with sqlite3.connect(p.path) as db:
                rows=db.execute('SELECT node_id,generation,H FROM sub_agent_cursor ORDER BY node_id,generation').fetchall()
                self.assertEqual(rows,[('a',1,'9'),('a',2,'1'),('b',1,'1')],'AA010_SIBLING_CURSOR')
                self.assertEqual(db.execute('SELECT count(*) FROM sub_agent_cost').fetchone()[0],3,'AA010_SCOPE_COST_ROWS')

    def test_aa010_scope_ticket_required_and_bound(self):
        with tempfile.TemporaryDirectory() as root:
            p=CostProjection(os.path.join(root,'projection.db'));sub=self.subject()
            with self.assertRaisesRegex(ValueError,'attested',msg='AA010_HEADER_ONLY_REJECT'):
                p.apply('root',sub['full_name'],'a-1',dict(kind='test'))
            forged={**sub,'lane_generation':2}
            with self.assertRaisesRegex(ValueError,'mismatch',msg='AA010_TICKET_GENERATION_BINDING'):
                vs._projection_scope(forged)
            with self.assertRaises(ValueError,msg='AA010_SCOPE_PAYLOAD_BINDING'):
                p.apply('other',sub['full_name'],'a-1',dict(kind='test'),subject=sub)
            handler=object.__new__(vs._Handler);handler.headers={'X-DW-Session':sub['full_name']}
            with self.assertRaisesRegex(ValueError,'ticket',msg='AA010_UNSIGNED_CHILD_REJECT'):
                handler._mux_subject()

    def test_aa010_real_signature_tamper_and_page_cache_generation(self):
        import base64,json,time
        from unittest.mock import patch
        sub=self.subject()
        handler=object.__new__(vs._Handler);handler.path='/status'
        handler.headers={'X-DW-Session':sub['full_name'],'X-DW-Mux-Subject':base64.urlsafe_b64encode(json.dumps(sub).encode()).decode()}
        with patch.object(vs,'_get_page_store',return_value=None),patch.object(vs,'_verify_control_request',return_value=(False,401)):
            with self.assertRaises(ValueError,msg='AA010_INVALID_SIGNATURE_REJECT'):handler._mux_subject()
        with open(os.path.join(os.path.dirname(__file__),'renderer-canary-sse.html')) as fh: renderer=fh.read()
        import re,subprocess,json
        function=re.search(r'function pageStorageKey\(\) \{(.*?)\n    \}',renderer,re.S).group(0)
        js="const stats={sessionName:'same.agent.owner'};let VS_SCOPE_KEY='scope-a';"+function+";const a=pageStorageKey();VS_SCOPE_KEY='scope-b';const b=pageStorageKey();VS_SCOPE_KEY='';console.log(JSON.stringify([a,b,pageStorageKey()]));"
        result=subprocess.run(['node','-e',js],capture_output=True,text=True,check=True)
        self.assertEqual(json.loads(result.stdout),['dw-view-page:scope-a','dw-view-page:scope-b','dw-view-page:same.agent.owner'],'AA010_PAGE_CACHE_SCOPE')
