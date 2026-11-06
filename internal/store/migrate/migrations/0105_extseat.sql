-- R53 A 路：外部身份/令牌/收件权威均在 PG。0106 的 AG 由 D 路维护。
ALTER TABLE np_agent DROP CONSTRAINT np_agent_origin_check;
ALTER TABLE np_agent ADD CONSTRAINT np_agent_origin_check CHECK (origin IN ('selfhost','hosted','external'));
CREATE TABLE np_external_principal (
 id text PRIMARY KEY,
 provider text NOT NULL CHECK(provider='feishu'), tenant_key text NOT NULL,
 issuer text NOT NULL, subject text NOT NULL, union_id text NOT NULL DEFAULT '',
 display_name text NOT NULL, owner_lc text NOT NULL REFERENCES np_user(user_id_lc),
 created_at timestamptz NOT NULL,
 UNIQUE(provider,tenant_key,issuer,subject)
);
CREATE TABLE np_extseat (
 agent_id text PRIMARY KEY REFERENCES np_agent(id),
 principal_id text NOT NULL REFERENCES np_external_principal(id),
 short_name text NOT NULL CHECK(short_name ~ '^[a-z0-9]{1,8}$'),
 ag_code text NOT NULL, ag_generation bigint NOT NULL CHECK(ag_generation>0),
 auth_generation bigint NOT NULL DEFAULT 1 CHECK(auth_generation>0),
 created_at timestamptz NOT NULL, last_seen_at timestamptz,
 revoked_at timestamptz
);
CREATE TABLE np_extseat_token (
 id text PRIMARY KEY, token_hash text NOT NULL UNIQUE CHECK(token_hash ~ '^[a-f0-9]{64}$'),
 seat_id text NOT NULL REFERENCES np_extseat(agent_id),
 audience text NOT NULL, scopes text NOT NULL CHECK(scopes='list_online send_message wait_for_messages ack'),
 auth_generation bigint NOT NULL CHECK(auth_generation>0),
 created_at timestamptz NOT NULL, last_auth_at timestamptz NOT NULL,
 expires_at timestamptz NOT NULL, revoked_at timestamptz
);
CREATE INDEX np_extseat_token_seat ON np_extseat_token(seat_id);
CREATE TABLE np_extseat_inbox (
 id text PRIMARY KEY, seat_id text NOT NULL REFERENCES np_extseat(agent_id),
 generation bigint NOT NULL DEFAULT 1 CHECK(generation>0),
 ag_code text NOT NULL, ag_generation bigint NOT NULL,
 source_agent_id text NOT NULL REFERENCES np_agent(id), source_message_id text NOT NULL,
 source_name text NOT NULL, target_name text NOT NULL, body text NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','leased','received','dead')),
 created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 lease_until timestamptz, received_at timestamptz, dead_reason text NOT NULL DEFAULT '',
 UNIQUE(source_agent_id,seat_id,ag_code,ag_generation,source_message_id)
);
CREATE INDEX np_extseat_inbox_claim ON np_extseat_inbox(seat_id,state,lease_until,created_at,id);
CREATE TABLE np_extseat_send_key (
 seat_id text NOT NULL REFERENCES np_extseat(agent_id), client_message_id text NOT NULL,
 request_hash text NOT NULL, delivery_id text NOT NULL UNIQUE,
 ag_code text NOT NULL, ag_generation bigint NOT NULL,
 created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 PRIMARY KEY(seat_id,client_message_id)
);
CREATE TABLE np_extseat_outbox (
 delivery_id text PRIMARY KEY REFERENCES np_extseat_send_key(delivery_id),
 seat_id text NOT NULL REFERENCES np_extseat(agent_id), target_agent_id text NOT NULL REFERENCES np_agent(id),
 source_name text NOT NULL, target_name text NOT NULL, body text NOT NULL,
 ag_code text NOT NULL, ag_generation bigint NOT NULL,
 state text NOT NULL DEFAULT 'queued' CHECK(state IN ('queued','projected','dead')),
 created_at timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 projected_at timestamptz, dead_reason text NOT NULL DEFAULT ''
);
CREATE INDEX np_extseat_outbox_pending ON np_extseat_outbox(state,created_at,delivery_id);
