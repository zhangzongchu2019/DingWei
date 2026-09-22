ALTER TABLE console_task_proposal ADD COLUMN task_content TEXT NOT NULL DEFAULT '';
ALTER TABLE console_task_proposal ADD COLUMN status TEXT NOT NULL DEFAULT 'proposed';
ALTER TABLE console_task_proposal ADD COLUMN estimated_hours REAL NOT NULL DEFAULT 0 CHECK(estimated_hours >= 0);
ALTER TABLE console_task_proposal ADD COLUMN depends_on_json TEXT NOT NULL DEFAULT '[]';
