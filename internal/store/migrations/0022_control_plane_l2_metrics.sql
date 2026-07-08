-- DingWei platform control-plane P2: L2 processing observability.

CREATE TABLE IF NOT EXISTS control_task_l2_metric (
  task_id     TEXT PRIMARY KEY,
  duration_ms INTEGER NOT NULL,
  success     INTEGER NOT NULL,
  error       TEXT,
  created_at  TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_ctl2_metric_created ON control_task_l2_metric(created_at);
