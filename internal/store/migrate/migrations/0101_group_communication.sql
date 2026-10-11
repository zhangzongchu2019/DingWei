ALTER TABLE np_group ADD COLUMN IF NOT EXISTS leader_only boolean NOT NULL DEFAULT false;
