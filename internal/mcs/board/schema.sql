-- mcs Board DDL —— 照抄冻结 spec multi-cli-scheduler-M0-board-draft.md(两签@f41d686 家族)。
-- 表序:orchestrator 先建(task.board_id FK 引用它,★维度13 DDL 顺序)。

CREATE TABLE IF NOT EXISTS orchestrator (
  board_id     TEXT PRIMARY KEY,
  owner        TEXT,
  epoch        INTEGER NOT NULL DEFAULT 0,
  lease_expiry INTEGER
);

CREATE TABLE IF NOT EXISTS task (
  task_id     TEXT PRIMARY KEY,
  board_id    TEXT NOT NULL REFERENCES orchestrator(board_id),
  title       TEXT NOT NULL,
  root_body   TEXT NOT NULL,
  origin_addr TEXT NOT NULL,
  origin_envelope_id TEXT NOT NULL,
  status      TEXT NOT NULL DEFAULT 'planning'
              CHECK(status IN ('planning','running','done','failed','partial')),
  result_version INTEGER NOT NULL DEFAULT 1,
  critical_path_len INTEGER DEFAULT 0,
  created_ts  INTEGER NOT NULL, updated_ts INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS agent (
  agent_id    TEXT PRIMARY KEY,
  driver_type TEXT NOT NULL CHECK(driver_type IN ('claude','codex','opencode')),
  handle      TEXT NOT NULL,
  registered  INTEGER NOT NULL DEFAULT 0,
  state       TEXT NOT NULL DEFAULT 'starting'
              CHECK(state IN ('starting','idle','busy','draining','down')),
  current_node TEXT,
  caps        TEXT NOT NULL DEFAULT '{}',
  lease_ts    INTEGER NOT NULL,
  owner            TEXT,
  lease_expiry     INTEGER,
  lease_generation INTEGER NOT NULL DEFAULT 0,
  created_ts  INTEGER NOT NULL, updated_ts INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_agent_state ON agent(state);

CREATE TABLE IF NOT EXISTS node (
  node_id     TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL REFERENCES task(task_id),
  seq         INTEGER NOT NULL,
  title       TEXT NOT NULL,
  body        TEXT NOT NULL,
  acceptance  TEXT NOT NULL DEFAULT '',
  deps        TEXT NOT NULL DEFAULT '[]',
  status      TEXT NOT NULL DEFAULT 'pending'
              CHECK(status IN ('pending','ready','dispatched','running','done','blocked','failed')),
  generation  INTEGER NOT NULL DEFAULT 1,
  assigned_agent TEXT REFERENCES agent(agent_id),
  delivery_id TEXT,
  attempt     INTEGER NOT NULL DEFAULT 0,
  delivery_attempt INTEGER NOT NULL DEFAULT 0,
  max_attempt INTEGER NOT NULL DEFAULT 3,
  max_delivery INTEGER NOT NULL DEFAULT 3,
  context_budget INTEGER,
  result_ref  TEXT,
  blocker_reason TEXT
              CHECK(blocker_reason IS NULL OR blocker_reason IN
                    ('starved','dep_failed','timeout','retry_exhausted','awaiting_input','delivery_unresolved')),
  sla_deadline INTEGER,
  started_ts  INTEGER, ended_ts INTEGER,
  updated_ts  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_node_task   ON node(task_id, seq);
CREATE INDEX IF NOT EXISTS idx_node_status ON node(status);

CREATE TABLE IF NOT EXISTS board_event (
  cursor    INTEGER PRIMARY KEY AUTOINCREMENT,
  task_id TEXT, node_id TEXT, agent_id TEXT,
  kind      TEXT NOT NULL,
  generation INTEGER,
  ts        INTEGER NOT NULL,
  data      TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS node_request (
  request_id  TEXT PRIMARY KEY,
  node_id     TEXT NOT NULL REFERENCES node(node_id),
  generation  INTEGER NOT NULL,
  op          TEXT NOT NULL,
  expected_state TEXT,
  outcome     TEXT NOT NULL CHECK(outcome IN ('pending','uncertain','applied','cas_miss','rejected')),
  expires_ts  INTEGER,
  applied_ts  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task_request (
  request_id  TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL,
  op          TEXT NOT NULL CHECK(op IN ('create_task','initial_plan','append_revision')),
  body_hash   TEXT NOT NULL,
  outcome     TEXT NOT NULL CHECK(outcome IN ('applied','rejected')),
  applied_ts  INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS result_outbox (
  result_key  TEXT PRIMARY KEY,
  task_id     TEXT NOT NULL REFERENCES task(task_id),
  origin_addr TEXT NOT NULL,
  payload_ref TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','acked')),
  attempts    INTEGER NOT NULL DEFAULT 0,
  next_retry_ts INTEGER,
  created_ts  INTEGER NOT NULL, updated_ts INTEGER NOT NULL
);
