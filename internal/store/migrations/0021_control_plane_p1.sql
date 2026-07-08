-- DingWei platform control-plane P1: L0 task queue + data-driven L1 decision table.

CREATE TABLE IF NOT EXISTS control_task (
  id             TEXT PRIMARY KEY,
  parent_id      TEXT,
  created_at     TEXT NOT NULL,
  updated_at     TEXT NOT NULL,
  source         TEXT NOT NULL,
  source_addr    TEXT NOT NULL,
  owner_key      TEXT NOT NULL,
  bot_channel_id TEXT,
  raw_input      TEXT NOT NULL,
  intent         TEXT,
  layer          TEXT,
  target         TEXT,
  result         TEXT,
  status         TEXT NOT NULL,
  priority       INTEGER NOT NULL DEFAULT 0,
  attempts       INTEGER NOT NULL DEFAULT 0,
  max_attempts   INTEGER NOT NULL DEFAULT 3,
  error          TEXT,
  lease_owner    TEXT,
  lease_until    TEXT,
  expire_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_ct_status_prio ON control_task(status, priority DESC, created_at);
CREATE INDEX IF NOT EXISTS idx_ct_parent ON control_task(parent_id);

CREATE TABLE IF NOT EXISTS l1_decision_rule (
  id          TEXT PRIMARY KEY,
  seq         INTEGER NOT NULL UNIQUE,
  match_type  TEXT NOT NULL,
  pattern     TEXT NOT NULL,
  intent      TEXT NOT NULL,
  action      TEXT NOT NULL,
  exit_queue  INTEGER NOT NULL DEFAULT 0,
  enabled     INTEGER NOT NULL DEFAULT 1,
  description TEXT,
  created_at  TEXT NOT NULL,
  updated_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_l1_decision_rule_enabled_seq ON l1_decision_rule(enabled, seq);

INSERT OR IGNORE INTO l1_decision_rule(id, seq, match_type, pattern, intent, action, exit_queue, enabled, description, created_at, updated_at) VALUES
('l1_command_terminal_input', 1, 'prefix_any', '#unlock |#lock', 'command.unlock', 'grantTerminalInput/revoke', 1, 1, 'Terminal input unlock/lock commands', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_command_roster', 2, 'prefix_any', '#在线|#roster', 'command.roster', 'onlineDirectory', 1, 1, 'Online roster command', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_command_apply_key', 3, 'prefix', '#申请 ', 'command.apply_key', 'applyKeyFlow', 1, 1, 'Session key application command', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_command_mirror', 4, 'prefix_any', '#mirror on|#mirror off|mirror on|mirror off', 'command.mirror', 'setMirror', 1, 1, 'Mirror on/off command', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_route_session', 5, 'regex', '^#[^[:space:]]+[[:space:]]+.+', 'route.session', 'routeToSession', 1, 1, 'Same-owner #session routing', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_route_cross', 6, 'regex', '^@[^[:space:]#]+#[^[:space:]]+[[:space:]]+.+', 'route.cross', 'routeToSession', 1, 1, 'Cross-member @member#session routing', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_route_default_single', 7, 'default', 'personal_single_online_session', 'route.default', 'routeToOnlySession', 1, 1, 'DM without prefix and one online session', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_nl_dispatch', 8, 'default', 'personal_multiple_online_sessions', 'nl_dispatch', 'handoffL2', 0, 1, 'DM without prefix and multiple online sessions', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_decompose', 9, 'keyword_any', '让团队|大家|所有人', 'decompose', 'handoffL2', 0, 1, 'Multi-target team semantics', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now')),
('l1_unknown', 10, 'fallback', '*', 'unknown', 'handoffL2', 0, 1, 'Fallback to L2 clarification', strftime('%Y-%m-%dT%H:%M:%fZ','now'), strftime('%Y-%m-%dT%H:%M:%fZ','now'));
