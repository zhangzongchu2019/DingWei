CREATE TABLE IF NOT EXISTS console_spec_task_case_result (
  task_id       TEXT NOT NULL REFERENCES console_spec_task(id) ON DELETE CASCADE,
  acceptance_id TEXT NOT NULL,
  case_name     TEXT NOT NULL,
  marked_by     TEXT NOT NULL REFERENCES console_agent(id),
  marked_at     TEXT NOT NULL,
  confirmed_by  TEXT NULL REFERENCES console_agent(id),
  confirmed_at  TEXT NULL,
  PRIMARY KEY (task_id, acceptance_id, case_name)
);
