-- 切片三；0112 由显示名切片占用。原 AG 地址、期限与成员不转换。
ALTER TABLE np_extseat ALTER COLUMN ag_code DROP NOT NULL;
ALTER TABLE np_extseat ALTER COLUMN ag_generation DROP NOT NULL;
ALTER TABLE np_extseat ADD CONSTRAINT np_extseat_optional_pair CHECK
 ((ag_code IS NULL AND ag_generation IS NULL) OR (ag_code IS NOT NULL AND ag_generation IS NOT NULL AND ag_generation > 0));
CREATE UNIQUE INDEX np_extseat_unpaired_principal_short ON np_extseat(principal_id,short_name)
 WHERE ag_code IS NULL AND revoked_at IS NULL;

-- NULL 仅表示没有 AG，不以空串或伪 AG 表达普通组。
ALTER TABLE np_extseat_inbox ALTER COLUMN ag_code DROP NOT NULL;
ALTER TABLE np_extseat_inbox ALTER COLUMN ag_generation DROP NOT NULL;
ALTER TABLE np_extseat_send_key ALTER COLUMN ag_code DROP NOT NULL;
ALTER TABLE np_extseat_send_key ALTER COLUMN ag_generation DROP NOT NULL;
ALTER TABLE np_extseat_outbox ALTER COLUMN ag_code DROP NOT NULL;
ALTER TABLE np_extseat_outbox ALTER COLUMN ag_generation DROP NOT NULL;

ALTER TABLE np_extseat_inbox ADD COLUMN route_kind text NOT NULL DEFAULT 'temporary' CHECK(route_kind IN ('ordinary','temporary'));
ALTER TABLE np_extseat_inbox ADD COLUMN route_ref text;
ALTER TABLE np_extseat_inbox ADD COLUMN route_generation bigint NOT NULL DEFAULT 1 CHECK(route_generation>0);
UPDATE np_extseat_inbox SET route_ref=ag_code,route_generation=ag_generation;
-- 旧件继续由旧路径插入；普通件必须显式给非空真实绑定。
ALTER TABLE np_extseat_inbox ADD CONSTRAINT np_extseat_inbox_ordinary_binding CHECK(route_kind<>'ordinary' OR (route_ref IS NOT NULL AND route_ref<>'' AND ag_code IS NULL AND ag_generation IS NULL));
ALTER TABLE np_extseat_outbox ADD COLUMN route_kind text NOT NULL DEFAULT 'temporary' CHECK(route_kind IN ('ordinary','temporary'));
ALTER TABLE np_extseat_outbox ADD COLUMN route_ref text;
ALTER TABLE np_extseat_outbox ADD COLUMN route_generation bigint NOT NULL DEFAULT 1 CHECK(route_generation>0);
UPDATE np_extseat_outbox SET route_ref=ag_code,route_generation=ag_generation;
ALTER TABLE np_extseat_outbox ADD CONSTRAINT np_extseat_outbox_ordinary_binding CHECK(route_kind<>'ordinary' OR (route_ref IS NOT NULL AND route_ref<>'' AND ag_code IS NULL AND ag_generation IS NULL));
ALTER TABLE np_extseat_send_key ADD COLUMN route_kind text NOT NULL DEFAULT 'temporary' CHECK(route_kind IN ('ordinary','temporary'));
ALTER TABLE np_extseat_send_key ADD COLUMN route_ref text;
ALTER TABLE np_extseat_send_key ADD COLUMN route_generation bigint NOT NULL DEFAULT 1 CHECK(route_generation>0);
ALTER TABLE np_extseat_send_key ADD COLUMN target_agent_id text REFERENCES np_agent(id);
UPDATE np_extseat_send_key k SET route_ref=k.ag_code,route_generation=k.ag_generation,target_agent_id=o.target_agent_id FROM np_extseat_outbox o WHERE o.delivery_id=k.delivery_id;
CREATE INDEX np_extseat_inbox_route ON np_extseat_inbox(seat_id,route_kind,route_ref,state,created_at,id);

ALTER TABLE np_extseat_send_key ADD CONSTRAINT np_extseat_send_key_ordinary_binding CHECK(route_kind<>'ordinary' OR (route_ref IS NOT NULL AND route_ref<>'' AND target_agent_id IS NOT NULL AND ag_code IS NULL AND ag_generation IS NULL));
