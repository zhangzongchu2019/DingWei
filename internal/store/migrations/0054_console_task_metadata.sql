ALTER TABLE console_spec_task ADD COLUMN started_at TEXT NULL;
ALTER TABLE console_spec_task ADD COLUMN planned_finish_at TEXT NULL;
ALTER TABLE console_spec_task ADD COLUMN blocker TEXT NULL;
ALTER TABLE console_spec_task ADD COLUMN resource_coordination TEXT NULL;
