CREATE TABLE IF NOT EXISTS console_spec_task_acceptance (
  task_id       TEXT NOT NULL REFERENCES console_spec_task(id) ON DELETE CASCADE,
  acceptance_id TEXT NOT NULL,
  PRIMARY KEY (task_id, acceptance_id)
);

ALTER TABLE console_spec_task ADD COLUMN definition_of_done TEXT NOT NULL DEFAULT '';
