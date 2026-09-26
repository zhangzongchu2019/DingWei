ALTER TABLE console_spec_task
  ADD COLUMN developer_agent_id TEXT NULL REFERENCES console_agent(id);

ALTER TABLE console_spec_task
  ADD COLUMN reviewer_agent_id TEXT NULL REFERENCES console_agent(id);
