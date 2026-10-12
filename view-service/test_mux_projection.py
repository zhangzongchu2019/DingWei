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
