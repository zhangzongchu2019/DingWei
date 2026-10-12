import copy, os, sqlite3, tempfile, unittest
from unittest.mock import patch
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

    def test_idempotent_repeat_can_validate_advanced_cut_without_overwrite(self):
        self.p.seed(self.rows(),'approved')
        self.p.apply('stable-id','new.owner','deadbeef-9007199254740994',dict(kind='state_change',data=dict(change='turn_completed',cost_usd=15)))
        fresh=copy.deepcopy(self.authority);fresh['agents'][0]['H']='9007199254740994'
        rows=validate_seed(self.legacy,self.mapping,fresh,'t',already_seeded=self.p.has_seed('approved'))
        self.assertFalse(self.p.seed(rows,'approved'))
        self.assertEqual(self.p.status('stable-id')['turns'],8)

    def test_unreadable_legacy_cannot_be_treated_as_empty_history(self):
        path=os.path.join(self.root.name,'legacy.json')
        with open(path,'w') as f:f.write('{broken')
        with patch.object(vs,'STATE_FILE',path),patch.object(vs,'_legacy_state_unreadable',False):
            vs._load_state()
            self.assertTrue(vs._legacy_state_unreadable)
            with self.assertRaises(sqlite3.Error):self.p.require_legacy_seed({},vs._legacy_state_unreadable)
            self.p.seed(self.rows(),'approved')
            self.p.require_legacy_seed({},vs._legacy_state_unreadable)

    def restart_proof(self):
        return dict(version='cost-restart-v1',target='t',approval_ref='synthetic-approved',
            publishers_frozen=True,consumers_drained_stopped=True,old_hub_stopped=True,
            no_completed_republication=True,old_cuts=[self.record.copy()],
            **{k:'a'*64 for k in ('cut_capture_sha256','drain_reconciliation_sha256',
                                  'publication_fence_sha256','old_hub_stop_sha256')})

    def test_restart_preserves_old_floor_not_new_epoch(self):
        fresh=copy.deepcopy(self.authority);fresh['agents'][0].update(epoch='cafebabe',H='0')
        proof=self.restart_proof()
        rows=validate_seed(self.legacy,self.mapping,fresh,'t',restart_proof=proof)
        self.p.seed(rows,'restart-approved')
        with sqlite3.connect(self.p.path) as db:
            self.assertEqual(db.execute('SELECT epoch,H FROM mux_seed_floor').fetchall(),
                             [('deadbeef','9007199254740993')])
        event=dict(kind='state_change',data=dict(change='turn_completed',cost_usd=15))
        self.assertFalse(self.p.apply('stable-id','new.owner','deadbeef-9007199254740993',event))
        self.assertTrue(self.p.apply('stable-id','new.owner','cafebabe-1',event))
        self.assertFalse(self.p.apply('stable-id','new.owner','cafebabe-1',event))
        self.assertEqual(self.p.status('stable-id'),dict(cost_total=15,cost_last=2.5,turns=8))
        fresh['agents'][0]['H']='4'
        rows=validate_seed(self.legacy,self.mapping,fresh,'t',True,restart_proof=proof)
        self.assertFalse(self.p.seed(rows,'restart-approved'))

    def test_restart_missing_drain_relabel_and_warm_start_refuse(self):
        proof=self.restart_proof();fresh=copy.deepcopy(self.authority)
        fresh['agents'][0].update(epoch='cafebabe',H='0')
        for key in ('drain_reconciliation_sha256','publication_fence_sha256','old_hub_stop_sha256',
                    'approval_ref','no_completed_republication'):
            bad=copy.deepcopy(proof);bad.pop(key)
            with self.assertRaises(ValueError):validate_seed(self.legacy,self.mapping,fresh,'t',restart_proof=bad)
        for change in ({'H':'1'},{'epoch':'deadbeef'}):
            bad=copy.deepcopy(fresh);bad['agents'][0].update(change)
            with self.assertRaises(ValueError):validate_seed(self.legacy,self.mapping,bad,'t',restart_proof=proof)
        bad=copy.deepcopy(self.mapping);bad['bindings'][0]['epoch']='cafebabe'
        with self.assertRaises(ValueError):validate_seed(self.legacy,bad,fresh,'t',restart_proof=proof)

    def test_old_sse_capture_is_tail_only_and_refuses_gaps(self):
        import io
        from capture_old_cost_cut import read_cut
        cut=read_cut(io.BytesIO(b'id: deadbeef-7\ndata: {"kind":"test"}\n\nid: deadbeef-8\ndata: {}\n\n: ping\n\n'),'new.owner')
        self.assertEqual((cut['epoch'],cut['H'],cut['floor']),('deadbeef','8','7'))
        self.assertEqual(cut['coverage'],'TAIL_ONLY_NOT_COST_COVERAGE')
        for wire in (b': ping\n',b'data: {}\n',b'id: deadbeef-1\ndata: {}\n\nid: deadbeef-3\ndata: {}\n\n: ping\n',b'event: replay_gap\n',b'id: deadbeef-1\ndata: {}\n'):
            with self.assertRaises(ValueError):read_cut(io.BytesIO(wire),'new.owner')
