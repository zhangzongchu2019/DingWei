import copy, os, sqlite3, tempfile, unittest
import view_service as vs
from migrate_mux_cost import validate_seed

class CostSeedTest(unittest.TestCase):
    def setUp(self):
        self.root=tempfile.TemporaryDirectory();self.addCleanup(self.root.cleanup)
        self.p=vs.CostProjection(os.path.join(self.root.name,'cost.db'))
        self.legacy={'old-name':dict(cost_total=12.5,cost_last=2.5,turns=7,token='synthetic-do-not-export')}
        self.record=dict(agent_id='stable-id',owner_lc='owner',full_name='new.owner',epoch='deadbeef',H='9007199254740993')
        self.mapping=dict(version='cost-seed-v1',target='t',bindings=[dict(self.record,legacy_name='old-name',coverage='all_completed_through_H')])
        self.authority=dict(target='t',agents=[self.record])
    def rows(self):return validate_seed(self.legacy,self.mapping,self.authority,'t')
    def test_seed_replay_restart_growth_and_seen_retention(self):
        p=self.p
        with sqlite3.connect(p.path) as db:db.execute("INSERT INTO mux_completed VALUES('unrelated','aaa','1')")
        self.assertTrue(p.seed(self.rows(),'approved'))
        self.assertEqual(p.status('stable-id'),dict(cost_total=12.5,cost_last=2.5,turns=7))
        for n in (None,7):
            data=dict(change='turn_completed',cost_usd=12.5)
            if n is not None:data['num_turns']=n
            self.assertFalse(p.apply('stable-id','new.owner','deadbeef-9007199254740993',dict(kind='state_change',data=data)))
        event=dict(kind='state_change',data=dict(change='turn_completed',cost_usd=15))
        self.assertTrue(p.apply('stable-id','new.owner','deadbeef-9007199254740994',event))
        p=vs.CostProjection(p.path);self.assertFalse(p.seed(self.rows(),'approved'))
        self.assertFalse(p.apply('stable-id','new.owner','deadbeef-9007199254740994',event))
        self.assertEqual(p.status('stable-id'),dict(cost_total=15,cost_last=2.5,turns=8))
        with sqlite3.connect(p.path) as db:self.assertEqual(db.execute("SELECT count(*) FROM mux_completed WHERE agent_id='unrelated'").fetchone()[0],1)
    def test_trusted_identity_alias_cut_and_coverage_failures(self):
        for kind in ('owner_lc','agent_id','H','merge','missing','coverage'):
            m=copy.deepcopy(self.mapping)
            if kind in ('owner_lc','agent_id','H'):m['bindings'][0][kind]='other'
            if kind=='merge':m['bindings']*=2
            if kind=='missing':m['bindings']=[]
            if kind=='coverage':m['bindings'][0]['coverage']='unknown'
            with self.assertRaises(ValueError):validate_seed(self.legacy,m,self.authority,'t')
        self.assertNotIn('token',self.rows()[0])
    def test_atomic_failure_and_existing_cost_conflict(self):
        p=self.p
        with sqlite3.connect(p.path) as db:db.execute("CREATE TRIGGER fault BEFORE INSERT ON mux_migration BEGIN SELECT RAISE(ABORT,'controlled fault'); END")
        with self.assertRaises(sqlite3.Error):p.seed(self.rows(),'approved')
        with sqlite3.connect(p.path) as db:
            for table in ('mux_cost','mux_seed_floor','mux_migration'):self.assertEqual(db.execute('SELECT count(*) FROM '+table).fetchone()[0],0)
            db.execute('DROP TRIGGER fault');db.execute("INSERT INTO mux_cost VALUES('stable-id','new.owner',20,1,10)")
        with self.assertRaises(ValueError):p.seed(self.rows(),'approved')
        self.assertEqual(p.status('stable-id')['cost_total'],20)
    def test_historical_nonzero_requires_migration(self):
        with self.assertRaises(sqlite3.Error):self.p.require_legacy_seed(self.legacy)
        self.p.seed(self.rows(),'approved');self.p.require_legacy_seed(self.legacy)
        with self.assertRaises(ValueError):self.p.seed(self.rows(),'different')
