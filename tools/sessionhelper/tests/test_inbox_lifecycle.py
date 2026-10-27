"""Recovery evidence is conservative and protocol identity is immutable."""
import json
import sqlite3
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from sessionhelper.inbox import DeliveryInbox


class InboxLifecycleMigrationTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.path = str(Path(self.tmp.name) / 'inbox.db')

    def inbox(self, seat='seat'):
        return DeliveryInbox(self.path, session_name=seat)

    def env(self, protocol=2, generation=1):
        return {'id': 'source', 'body': 'fixture', 'meta': {
            'delivery_ack_protocol': protocol, 'processing_mode': 'driver', 'ack_generation': generation}}

    def row(self, did='a', seat='seat'):
        with sqlite3.connect(self.path) as db:
            db.row_factory = sqlite3.Row
            row = db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?', (seat, did)).fetchone()
            return dict(row) if row else None

    def legacy(self):
        with sqlite3.connect(self.path) as db:
            db.execute("""CREATE TABLE delivery_inbox (
                delivery_id TEXT NOT NULL, session_name TEXT NOT NULL, envelope_json TEXT NOT NULL,
                state TEXT CHECK(state IN ('received','processing','processed','failed','cancelled')),
                attempts INTEGER DEFAULT 0,error TEXT,received_at REAL NOT NULL,updated_at REAL NOT NULL,
                PRIMARY KEY(session_name,delivery_id))""")
            for i, state in enumerate(('received','processing','processed','failed','cancelled')):
                db.execute("INSERT INTO delivery_inbox VALUES(?,?,?,?,?,?,?,?)",
                           (state, 'seat', json.dumps({'body': state}), state, 7, 'old', i+1, i+10))

    def test_old_rows_keep_payload_times_and_unknown_provenance(self):
        self.legacy()
        self.inbox()
        for i, state in enumerate(('received','processing','processed','failed','cancelled')):
            row = self.row(state)
            self.assertEqual(row['state'], 'admission_unknown' if state == 'processing' else state)
            self.assertEqual(json.loads(row['envelope_json']), {'body': state})
            self.assertEqual((row['attempts'], row['received_at'], row['updated_at']), (7, i+1, i+10))
            for key in ('ack_protocol','processing_mode','ack_generation','dispatch_intent_at','turn_id','admitted_at','processed_at','turn_outcome'):
                self.assertIsNone(row[key], key)
        self.inbox()
        self.assertEqual(self.row('processing')['state'], 'admission_unknown')

    def test_migration_rolls_back_whole_table_on_failure(self):
        self.legacy()
        from sessionhelper.inbox_lifecycle import migrate_lifecycle
        def fail(db):
            migrate_lifecycle(db)
            raise RuntimeError('migration interrupted')
        with patch('sessionhelper.inbox.migrate_lifecycle', fail):
            with self.assertRaisesRegex(RuntimeError, 'interrupted'):
                self.inbox()
        with sqlite3.connect(self.path) as db:
            columns = {r[1] for r in db.execute('PRAGMA table_info(delivery_inbox)')}
            self.assertNotIn('ack_protocol', columns)
            self.assertEqual(db.execute('SELECT state FROM delivery_inbox WHERE delivery_id="processing"').fetchone()[0], 'processing')
        self.inbox()
        self.assertEqual(self.row('processing')['state'], 'admission_unknown')

    def test_first_protocol_is_preserved_and_changed_duplicate_rejected(self):
        inbox = self.inbox()
        inbox.accept('a', self.env())
        before = self.row()
        with self.assertRaisesRegex(ValueError, 'provenance changed'):
            inbox.accept('a', self.env(generation=2))
        self.assertEqual(self.row(), before)
        inbox.accept('a', self.env())
        self.assertEqual(self.row(), before)

    def test_dispatch_intent_and_admitted_rows_never_reenter_fifo(self):
        inbox = self.inbox()
        for did in ('a', 'b', 'c'):
            inbox.accept(did, self.env())
        with inbox._connect() as db:
            db.execute("UPDATE delivery_inbox SET state='processing',dispatch_intent_at=100,request_id='a' WHERE delivery_id='a'")
            db.execute("UPDATE delivery_inbox SET state='admitted',dispatch_intent_at=100,turn_id='turn',admitted_at=101 WHERE delivery_id='b'")
            db.execute("UPDATE delivery_inbox SET state='processing' WHERE delivery_id='c'")
        recovered = self.inbox()
        self.assertEqual(self.row('a')['state'], 'admission_unknown')
        self.assertEqual(self.row('b')['state'], 'admitted')
        self.assertEqual(recovered.claim_next()[0], 'c')
        self.assertIsNone(recovered.claim_next())
        recovered.release('a')
        self.assertEqual(self.row('a')['state'], 'admission_unknown')

    def test_session_isolation_and_terminal_history_not_upgraded(self):
        seat = self.inbox()
        other = self.inbox('other')
        seat.accept('a', self.env())
        other.accept('a', self.env(protocol=1))
        seat.claim_next()
        seat.finish('a')
        self.inbox()
        self.assertIsNone(self.row()['processed_at'])
        self.assertEqual(self.row()['state'], 'processed')
        self.assertEqual(other.claim_next()[0], 'a')
        self.assertEqual(self.row(seat='other')['ack_protocol'], 1)

    def test_invalid_v2_metadata_does_not_silently_downgrade(self):
        inbox = self.inbox()
        for update in ({'delivery_ack_protocol': True}, {'processing_mode':'desktop'}, {'ack_generation':0}):
            env = self.env(); env['meta'].update(update)
            with self.assertRaises(ValueError):
                inbox.accept('a', env)
            self.assertIsNone(self.row())


if __name__ == '__main__':
    unittest.main()
