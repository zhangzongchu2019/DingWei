ALTER TABLE np_agent ADD COLUMN last_source_ip text;
ALTER TABLE np_agent ADD COLUMN last_source_at timestamptz;
