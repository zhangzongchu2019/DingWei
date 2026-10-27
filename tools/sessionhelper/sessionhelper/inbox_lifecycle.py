"""Per-delivery protocol provenance and durable driver lifecycle storage.

No driver request is made here. Historical rows retain unknown provenance and
never acquire model admission/terminal evidence during migration.
"""
from __future__ import annotations

import sqlite3


LIFECYCLE_COLUMNS = """
    ack_protocol INTEGER CHECK(ack_protocol IN (1,2)),
    processing_mode TEXT,
    ack_generation INTEGER CHECK(ack_generation > 0),
    dispatch_intent_at REAL,
    request_id TEXT,
    turn_id TEXT,
    admitted_at REAL,
    processed_at REAL,
    turn_outcome TEXT CHECK(turn_outcome IN ('completed','interrupted','failed'))
"""


def migrate_lifecycle(db: sqlite3.Connection) -> None:
    columns = {row['name'] for row in db.execute('PRAGMA table_info(delivery_inbox)')}
    if 'ack_protocol' not in columns:
        db.execute('ALTER TABLE delivery_inbox RENAME TO delivery_inbox_pre_lifecycle')
        db.execute(f"""CREATE TABLE delivery_inbox (
            delivery_id TEXT NOT NULL, session_name TEXT NOT NULL,
            envelope_json TEXT NOT NULL,
            state TEXT NOT NULL CHECK(state IN
                ('received','processing','admitted','admission_unknown','processed','failed','cancelled')),
            attempts INTEGER NOT NULL DEFAULT 0, error TEXT,
            received_at REAL NOT NULL, updated_at REAL NOT NULL,
            {LIFECYCLE_COLUMNS},
            PRIMARY KEY(session_name,delivery_id))""")
        db.execute("""INSERT INTO delivery_inbox
            (delivery_id,session_name,envelope_json,state,attempts,error,received_at,updated_at)
            SELECT delivery_id,session_name,envelope_json,
                CASE WHEN state='processing' THEN 'admission_unknown' ELSE state END,
                attempts,error,received_at,updated_at FROM delivery_inbox_pre_lifecycle""")
        db.execute('DROP TABLE delivery_inbox_pre_lifecycle')
    db.execute("""CREATE TABLE IF NOT EXISTS delivery_ack_outbox (
        session_name TEXT NOT NULL, delivery_id TEXT NOT NULL,
        phase TEXT NOT NULL CHECK(phase IN ('queued','injected','processed','admission_unknown','failed')),
        sequence INTEGER NOT NULL,
        payload_json TEXT NOT NULL,
        created_at REAL NOT NULL,
        PRIMARY KEY(session_name,delivery_id,phase),
        UNIQUE(session_name,delivery_id,sequence))""")


def delivery_protocol(env: dict) -> tuple[int, str, int]:
    meta = env.get('meta') or {}
    protocol = meta.get('delivery_ack_protocol', 1)
    mode = meta.get('processing_mode') or 'legacy'
    generation = meta.get('ack_generation', 1)
    if type(protocol) is not int or protocol not in (1, 2):
        raise ValueError('invalid delivery ACK protocol')
    if type(generation) is not int or generation < 1:
        raise ValueError('invalid delivery ACK generation')
    if mode not in ('driver', 'desktop', 'legacy') or (protocol == 2 and mode != 'driver'):
        raise ValueError('invalid delivery ACK processing mode')
    return protocol, mode, generation

