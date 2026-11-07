-- D 路：SEQUENCE 不因事务回滚退号；镜像由同一创建/解散事务维护。
CREATE SEQUENCE np_temp_group_sequence AS bigint MINVALUE 1 MAXVALUE 999999999999999999 NO CYCLE;
CREATE TABLE np_temp_group (
 code text PRIMARY KEY, seq bigint NOT NULL UNIQUE CHECK(seq>0),
 generation bigint NOT NULL DEFAULT 1 CHECK(generation=1),
 created_by text NOT NULL, created_by_union text NOT NULL DEFAULT '', created_by_display text NOT NULL,
 owner_lc text NOT NULL, external_agent_id text NOT NULL UNIQUE REFERENCES np_agent(id),
 target_agent_id text NOT NULL REFERENCES np_agent(id),
 created_at timestamptz NOT NULL, last_used_at timestamptz,
 expires_at timestamptz NOT NULL, dissolved_at timestamptz,
 dissolve_reason text NOT NULL DEFAULT '', notified_at timestamptz,
 CHECK(external_agent_id<>target_agent_id),
 CHECK(code='AG'||seq::text), CHECK(dissolved_at IS NULL OR dissolve_reason<>'')
);
CREATE INDEX np_temp_group_expiry ON np_temp_group(expires_at) WHERE dissolved_at IS NULL;
CREATE INDEX np_temp_group_target ON np_temp_group(target_agent_id);
CREATE TABLE np_temp_group_member (
 code text NOT NULL REFERENCES np_temp_group(code), agent_id text NOT NULL REFERENCES np_agent(id),
 role text NOT NULL CHECK(role IN ('external','internal')), created_at timestamptz NOT NULL,
 PRIMARY KEY(code,agent_id), UNIQUE(code,role)
);
CREATE UNIQUE INDEX np_temp_group_member_external ON np_temp_group_member(agent_id) WHERE role='external';
CREATE INDEX np_temp_group_member_agent ON np_temp_group_member(agent_id);
CREATE TABLE np_temp_group_use (
 code text NOT NULL REFERENCES np_temp_group(code), event_key text NOT NULL, kind text NOT NULL
 CHECK(kind IN ('accepted_send','first_received_ack')), at timestamptz NOT NULL,
 PRIMARY KEY(code,event_key)
);
