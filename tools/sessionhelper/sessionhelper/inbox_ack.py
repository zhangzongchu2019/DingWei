"""Durable ordered ACK transport. Sending never changes execution state."""
from __future__ import annotations

from datetime import datetime, timezone
import json
import time


def utc_stamp(value):
    return datetime.fromtimestamp(value, timezone.utc).isoformat(timespec='microseconds').replace('+00:00', 'Z')


def initialize_ack_cursor(db):
    db.execute("""CREATE TABLE IF NOT EXISTS delivery_ack_cursor (
        session_name TEXT NOT NULL, delivery_id TEXT NOT NULL,
        confirmed_sequence INTEGER NOT NULL DEFAULT 0,
        confirmed_phases TEXT NOT NULL DEFAULT '[]',
        PRIMARY KEY(session_name,delivery_id))""")


class DeliveryACKOutbox:
    def _lifecycle_ack(self, db, delivery_id, phase):
        row = db.execute('SELECT * FROM delivery_inbox WHERE session_name=? AND delivery_id=?',
                         (self.session_name, delivery_id)).fetchone()
        if row is None or row['ack_protocol'] is None or row['state'] == 'cancelled':
            return  # Historical unknown provenance never gains v2 evidence.
        if db.execute('SELECT 1 FROM delivery_ack_outbox WHERE session_name=? AND delivery_id=? AND phase=?',
                      (self.session_name, delivery_id, phase)).fetchone():
            return
        cursor = db.execute('SELECT confirmed_sequence,confirmed_phases FROM delivery_ack_cursor WHERE session_name=? AND delivery_id=?',
                            (self.session_name, delivery_id)).fetchone()
        confirmed = cursor[0] if cursor else 0
        if cursor and phase in json.loads(cursor[1]):
            return
        if phase == 'queued' and confirmed:
            return
        pending = db.execute('SELECT COALESCE(MAX(sequence),0) FROM delivery_ack_outbox WHERE session_name=? AND delivery_id=?',
                             (self.session_name, delivery_id)).fetchone()[0]
        sequence = max(confirmed, pending) + 1
        stamp = row['received_at'] if phase == 'queued' else row['admitted_at'] if phase == 'injected' else row['processed_at'] if phase == 'processed' else row['updated_at']
        payload = {'type':'delivery_ack','delivery_id':delivery_id,'ack_protocol':row['ack_protocol'],
                   'processing_mode':row['processing_mode'],'ack_generation':row['ack_generation'],
                   'state':'delivered' if phase == 'queued' and row['ack_protocol'] == 1 else phase,
                   'occurred_at':utc_stamp(stamp),'no_mirror':True}
        if phase in ('injected','processed'):
            payload['turn_id']=row['turn_id']
        if phase == 'processed':
            payload['turn_outcome']=row['turn_outcome']
            had_unknown = bool(cursor and 'admission_unknown' in json.loads(cursor[1])) or bool(db.execute(
                "SELECT 1 FROM delivery_ack_outbox WHERE session_name=? AND delivery_id=? AND phase='admission_unknown'",
                (self.session_name,delivery_id)).fetchone())
            if had_unknown:payload['late_confirmation']=True
        if phase == 'failed':payload.update(reason='session_unavailable',retryable=False)
        if phase == 'admission_unknown':payload['reason']='turn_terminal_unknown' if row['admitted_at'] is not None else 'model_admission_unknown'
        # v1 has no truthful wire state for unknown; receipt stays delivered.
        if phase == 'admission_unknown' and row['ack_protocol'] == 1:payload['state']='delivered'
        db.execute('INSERT INTO delivery_ack_outbox VALUES(?,?,?,?,?,?)',
                   (self.session_name,delivery_id,phase,sequence,json.dumps(payload,separators=(',',':')),time.time()))

    def pending_delivery_acks(self):
        with self._lock, self._connect() as db:
            rows = db.execute("""SELECT a.* FROM delivery_ack_outbox a
                WHERE a.session_name=? AND NOT EXISTS (
                    SELECT 1 FROM delivery_ack_outbox prior WHERE prior.session_name=a.session_name
                    AND prior.delivery_id=a.delivery_id AND prior.sequence<a.sequence)
                ORDER BY a.created_at,a.delivery_id""",(self.session_name,)).fetchall()
            return [dict(row, payload=json.loads(row['payload_json'])) for row in rows]

    def delivery_ack_chain(self):
        """v1 has no stage confirmations, so replay its whole retained chain."""
        with self._lock, self._connect() as db:
            rows=db.execute('SELECT * FROM delivery_ack_outbox WHERE session_name=? ORDER BY delivery_id,sequence',
                            (self.session_name,)).fetchall()
            return [dict(row,payload=json.loads(row['payload_json'])) for row in rows]

    def confirm_delivery_ack(self, meta):
        if meta.get('type') != 'delivery_ack_confirm' or type(meta.get('ack_protocol')) is not int or meta['ack_protocol'] != 2:
            return False
        with self._lock, self._connect() as db:
            db.execute('BEGIN IMMEDIATE')
            row=db.execute("""SELECT * FROM delivery_ack_outbox WHERE session_name=? AND delivery_id=?
                ORDER BY sequence LIMIT 1""",(self.session_name,meta.get('delivery_id'))).fetchone()
            if row is None:return False
            payload=json.loads(row['payload_json'])
            for key in ('delivery_id','ack_protocol','processing_mode','ack_generation','state'):
                if type(meta.get(key)) is not type(payload.get(key)) or meta.get(key) != payload.get(key):return False
            cursor = db.execute('SELECT confirmed_phases FROM delivery_ack_cursor WHERE session_name=? AND delivery_id=?',
                                (self.session_name,row['delivery_id'])).fetchone()
            phases = json.loads(cursor[0]) if cursor else []
            phases.append(row['phase'])
            db.execute('''INSERT INTO delivery_ack_cursor VALUES(?,?,?,?)
                ON CONFLICT(session_name,delivery_id) DO UPDATE SET confirmed_sequence=excluded.confirmed_sequence,confirmed_phases=excluded.confirmed_phases''',
                (self.session_name,row['delivery_id'],row['sequence'],json.dumps(phases)))
            db.execute('DELETE FROM delivery_ack_outbox WHERE session_name=? AND delivery_id=? AND phase=?',
                       (self.session_name,row['delivery_id'],row['phase']))
            return True
