-- First-time R30 adoption is fail-closed. Recognize the complete frozen
-- catalog before idempotent baseline DDL; do not repair partial/foreign shapes.
DO $adopt$
DECLARE actual jsonb; expected jsonb := $r31catalog${"columns":[["g4_account_grant","account_id","text",true,null],["g4_account_grant","user_lc","text",true,null],["g4_account_grant","granted_by","text",true,null],["g4_account_grant","granted_at","timestamp with time zone",true,"now()"],["g4_account_login","id","text",true,null],["g4_account_login","owner_lc","text",true,null],["g4_account_login","cli","text",true,null],["g4_account_login","llm","text",true,null],["g4_account_login","email","text",true,null],["g4_account_login","account_id","text",false,null],["g4_account_login","node_id","text",true,null],["g4_account_login","state","text",true,null],["g4_account_login","url","text",true,"''::text"],["g4_account_login","user_code","text",true,"''::text"],["g4_account_login","reason","text",true,"''::text"],["g4_account_login","result_account_id","text",false,null],["g4_account_login","created_at","timestamp with time zone",true,"now()"],["g4_account_login","expires_at","timestamp with time zone",true,null],["g4_account_login","finished_at","timestamp with time zone",false,null],["g4_account_login","claimed_at","timestamp with time zone",false,null],["g4_account_pool","id","text",true,null],["g4_account_pool","name","text",true,null],["g4_account_pool","kind","text",true,null],["g4_account_pool","cli","text",true,null],["g4_account_pool","llm","text",true,null],["g4_account_pool","secret_enc","bytea",true,null],["g4_account_pool","key_version","integer",true,null],["g4_account_pool","last4","text",true,"''::text"],["g4_account_pool","state","text",true,"'available'::text"],["g4_account_pool","bound_agent_id","text",false,null],["g4_account_pool","created_by","text",true,null],["g4_account_pool","created_at","timestamp with time zone",true,"now()"],["g4_account_pool","updated_at","timestamp with time zone",true,"now()"],["g4_account_pool","owner_lc","text",true,"''::text"],["g4_account_pool","email","text",true,"''::text"],["g4_audit","id","bigint",true,"nextval('g4_audit_id_seq'::regclass)"],["g4_audit","actor","text",true,null],["g4_audit","action","text",true,null],["g4_audit","target","text",true,"''::text"],["g4_audit","detail","jsonb",false,null],["g4_audit","at","timestamp with time zone",true,"now()"],["g4_backup","id","text",true,null],["g4_backup","node_id","text",true,null],["g4_backup","seat_id","text",true,null],["g4_backup","state","text",true,"'pending'::text"],["g4_backup","path","text",true,"''::text"],["g4_backup","size_bytes","bigint",true,"0"],["g4_backup","expires_at","timestamp with time zone",true,null],["g4_backup","created_at","timestamp with time zone",true,"now()"],["g4_hosted_seat","agent_id","text",true,null],["g4_hosted_seat","node_id","text",true,null],["g4_hosted_seat","cli","text",true,null],["g4_hosted_seat","llm","text",true,null],["g4_hosted_seat","cred_kind","text",true,null],["g4_hosted_seat","account_ref","text",false,null],["g4_hosted_seat","plan","text",true,null],["g4_hosted_seat","model","text",true,"''::text"],["g4_hosted_seat","state","text",true,"'queued'::text"],["g4_hosted_seat","state_detail","text",true,"''::text"],["g4_hosted_seat","image_ref","text",true,"''::text"],["g4_hosted_seat","created_by","text",true,null],["g4_hosted_seat","created_at","timestamp with time zone",true,"now()"],["g4_hosted_seat","updated_at","timestamp with time zone",true,"now()"],["g4_lifecycle_job","id","text",true,null],["g4_lifecycle_job","agent_id","text",true,null],["g4_lifecycle_job","created_by","text",true,null],["g4_lifecycle_job","op","text",true,null],["g4_lifecycle_job","args","jsonb",true,"'{}'::jsonb"],["g4_lifecycle_job","stage","text",true,"'queued'::text"],["g4_lifecycle_job","reason","text",true,"''::text"],["g4_lifecycle_job","created_at","timestamp with time zone",true,"now()"],["g4_lifecycle_job","updated_at","timestamp with time zone",true,"now()"],["g4_node","id","text",true,null],["g4_node","name","text",true,null],["g4_node","labels","jsonb",true,"'{}'::jsonb"],["g4_node","state","text",true,"'offline'::text"],["g4_node","capacity","jsonb",true,"'{}'::jsonb"],["g4_node","docker_version","text",true,"''::text"],["g4_node","node_protocol_version","integer",true,"1"],["g4_node","last_seen_at","timestamp with time zone",false,null],["g4_node","created_at","timestamp with time zone",true,"now()"],["g4_node","updated_at","timestamp with time zone",true,"now()"],["g4_node_credential","node_id","text",true,null],["g4_node_credential","secret_hash","text",true,null],["g4_node_credential","issued_by","text",true,null],["g4_node_credential","issued_at","timestamp with time zone",true,"now()"],["g4_node_credential","rotated_at","timestamp with time zone",false,null],["g4_node_credential","revoked_at","timestamp with time zone",false,null],["g4_quota","user_lc","text",true,null],["g4_quota","agents_limit","integer",true,"4"],["g4_quota","mem_mb_limit","bigint",true,"16384"],["g4_quota","disk_mb_limit","bigint",true,"65536"],["g4_quota","vcpu_limit","integer",true,"4"],["g4_quota","updated_by","text",true,null],["g4_quota","updated_at","timestamp with time zone",true,"now()"],["g4_quota_notice","node_id","text",true,null],["g4_quota_notice","event_id","text",true,null],["g4_quota_notice","recipient","text",true,null],["g4_quota_notice","seat_id","text",true,null],["g4_quota_notice","reason","text",true,null],["g4_quota_notice","writer_kind","text",true,"''::text"],["g4_quota_notice","measured_at","timestamp with time zone",true,null],["g4_quota_notice","delivered_at","timestamp with time zone",false,null],["g4_quota_notice","next_attempt_at","timestamp with time zone",true,"now()"],["g4_seat_event","id","bigint",true,"nextval('g4_seat_event_id_seq'::regclass)"],["g4_seat_event","agent_id","text",true,null],["g4_seat_event","kind","text",true,null],["g4_seat_event","detail","jsonb",false,null],["g4_seat_event","at","timestamp with time zone",true,"now()"],["g4_team_key","version","integer",true,null],["g4_team_key","wrapped","bytea",true,null],["g4_team_key","state","text",true,"'active'::text"],["g4_team_key","created_by","text",true,null],["g4_team_key","created_at","timestamp with time zone",true,"now()"]],"indexes":[["g4_account_grant","g4_account_grant_pkey","CREATE UNIQUE INDEX g4_account_grant_pkey ON g4_account_grant USING btree (account_id, user_lc)"],["g4_account_login","g4_account_login_active","CREATE INDEX g4_account_login_active ON g4_account_login USING btree (state) WHERE (state = ANY (ARRAY['starting'::text, 'waiting_user'::text, 'completing'::text]))"],["g4_account_login","g4_account_login_one_active","CREATE UNIQUE INDEX g4_account_login_one_active ON g4_account_login USING btree (owner_lc) WHERE (state = ANY (ARRAY['starting'::text, 'waiting_user'::text, 'completing'::text]))"],["g4_account_login","g4_account_login_owner","CREATE INDEX g4_account_login_owner ON g4_account_login USING btree (owner_lc, created_at)"],["g4_account_login","g4_account_login_pkey","CREATE UNIQUE INDEX g4_account_login_pkey ON g4_account_login USING btree (id)"],["g4_account_pool","g4_account_pool_owner","CREATE INDEX g4_account_pool_owner ON g4_account_pool USING btree (owner_lc)"],["g4_account_pool","g4_account_pool_owner_name","CREATE UNIQUE INDEX g4_account_pool_owner_name ON g4_account_pool USING btree (owner_lc, name)"],["g4_account_pool","g4_account_pool_pkey","CREATE UNIQUE INDEX g4_account_pool_pkey ON g4_account_pool USING btree (id)"],["g4_audit","g4_audit_action_idx","CREATE INDEX g4_audit_action_idx ON g4_audit USING btree (action, id)"],["g4_audit","g4_audit_pkey","CREATE UNIQUE INDEX g4_audit_pkey ON g4_audit USING btree (id)"],["g4_backup","g4_backup_pkey","CREATE UNIQUE INDEX g4_backup_pkey ON g4_backup USING btree (id)"],["g4_backup","g4_backup_state_idx","CREATE INDEX g4_backup_state_idx ON g4_backup USING btree (state, expires_at)"],["g4_hosted_seat","g4_hosted_seat_pkey","CREATE UNIQUE INDEX g4_hosted_seat_pkey ON g4_hosted_seat USING btree (agent_id)"],["g4_lifecycle_job","g4_lifecycle_job_agent_idx","CREATE INDEX g4_lifecycle_job_agent_idx ON g4_lifecycle_job USING btree (agent_id, created_at DESC)"],["g4_lifecycle_job","g4_lifecycle_job_one_active_idx","CREATE UNIQUE INDEX g4_lifecycle_job_one_active_idx ON g4_lifecycle_job USING btree (agent_id) WHERE (stage = ANY (ARRAY['queued'::text, 'running'::text]))"],["g4_lifecycle_job","g4_lifecycle_job_pkey","CREATE UNIQUE INDEX g4_lifecycle_job_pkey ON g4_lifecycle_job USING btree (id)"],["g4_node","g4_node_name_key","CREATE UNIQUE INDEX g4_node_name_key ON g4_node USING btree (name)"],["g4_node","g4_node_pkey","CREATE UNIQUE INDEX g4_node_pkey ON g4_node USING btree (id)"],["g4_node_credential","g4_node_credential_pkey","CREATE UNIQUE INDEX g4_node_credential_pkey ON g4_node_credential USING btree (node_id)"],["g4_quota","g4_quota_pkey","CREATE UNIQUE INDEX g4_quota_pkey ON g4_quota USING btree (user_lc)"],["g4_quota_notice","g4_quota_notice_node_idx","CREATE INDEX g4_quota_notice_node_idx ON g4_quota_notice USING btree (node_id, delivered_at)"],["g4_quota_notice","g4_quota_notice_pending_idx","CREATE INDEX g4_quota_notice_pending_idx ON g4_quota_notice USING btree (next_attempt_at, measured_at) WHERE (delivered_at IS NULL)"],["g4_quota_notice","g4_quota_notice_pkey","CREATE UNIQUE INDEX g4_quota_notice_pkey ON g4_quota_notice USING btree (node_id, event_id, recipient)"],["g4_seat_event","g4_seat_event_agent_idx","CREATE INDEX g4_seat_event_agent_idx ON g4_seat_event USING btree (agent_id, id)"],["g4_seat_event","g4_seat_event_pkey","CREATE UNIQUE INDEX g4_seat_event_pkey ON g4_seat_event USING btree (id)"],["g4_team_key","g4_team_key_pkey","CREATE UNIQUE INDEX g4_team_key_pkey ON g4_team_key USING btree (version)"]],"constraints":[["g4_account_grant","g4_account_grant_account_id_fkey","FOREIGN KEY (account_id) REFERENCES g4_account_pool(id)"],["g4_account_grant","g4_account_grant_account_id_not_null","NOT NULL account_id"],["g4_account_grant","g4_account_grant_granted_at_not_null","NOT NULL granted_at"],["g4_account_grant","g4_account_grant_granted_by_not_null","NOT NULL granted_by"],["g4_account_grant","g4_account_grant_pkey","PRIMARY KEY (account_id, user_lc)"],["g4_account_grant","g4_account_grant_user_lc_not_null","NOT NULL user_lc"],["g4_account_login","g4_account_login_cli_not_null","NOT NULL cli"],["g4_account_login","g4_account_login_created_at_not_null","NOT NULL created_at"],["g4_account_login","g4_account_login_email_not_null","NOT NULL email"],["g4_account_login","g4_account_login_expires_at_not_null","NOT NULL expires_at"],["g4_account_login","g4_account_login_id_not_null","NOT NULL id"],["g4_account_login","g4_account_login_llm_not_null","NOT NULL llm"],["g4_account_login","g4_account_login_node_id_not_null","NOT NULL node_id"],["g4_account_login","g4_account_login_owner_lc_not_null","NOT NULL owner_lc"],["g4_account_login","g4_account_login_pkey","PRIMARY KEY (id)"],["g4_account_login","g4_account_login_reason_not_null","NOT NULL reason"],["g4_account_login","g4_account_login_state_check","CHECK ((state = ANY (ARRAY['starting'::text, 'waiting_user'::text, 'completing'::text, 'done'::text, 'failed'::text, 'expired'::text, 'cancelled'::text])))"],["g4_account_login","g4_account_login_state_not_null","NOT NULL state"],["g4_account_login","g4_account_login_url_not_null","NOT NULL url"],["g4_account_login","g4_account_login_user_code_not_null","NOT NULL user_code"],["g4_account_pool","g4_account_pool_cli_not_null","NOT NULL cli"],["g4_account_pool","g4_account_pool_created_at_not_null","NOT NULL created_at"],["g4_account_pool","g4_account_pool_created_by_not_null","NOT NULL created_by"],["g4_account_pool","g4_account_pool_email_not_null","NOT NULL email"],["g4_account_pool","g4_account_pool_id_not_null","NOT NULL id"],["g4_account_pool","g4_account_pool_key_version_not_null","NOT NULL key_version"],["g4_account_pool","g4_account_pool_kind_check","CHECK ((kind = ANY (ARRAY['api_key'::text, 'token_file'::text])))"],["g4_account_pool","g4_account_pool_kind_not_null","NOT NULL kind"],["g4_account_pool","g4_account_pool_last4_not_null","NOT NULL last4"],["g4_account_pool","g4_account_pool_llm_not_null","NOT NULL llm"],["g4_account_pool","g4_account_pool_name_not_null","NOT NULL name"],["g4_account_pool","g4_account_pool_owner_lc_not_null","NOT NULL owner_lc"],["g4_account_pool","g4_account_pool_pkey","PRIMARY KEY (id)"],["g4_account_pool","g4_account_pool_secret_enc_not_null","NOT NULL secret_enc"],["g4_account_pool","g4_account_pool_state_check","CHECK ((state = ANY (ARRAY['available'::text, 'bound'::text, 'disabled'::text])))"],["g4_account_pool","g4_account_pool_state_not_null","NOT NULL state"],["g4_account_pool","g4_account_pool_updated_at_not_null","NOT NULL updated_at"],["g4_audit","g4_audit_action_not_null","NOT NULL action"],["g4_audit","g4_audit_actor_not_null","NOT NULL actor"],["g4_audit","g4_audit_at_not_null","NOT NULL at"],["g4_audit","g4_audit_id_not_null","NOT NULL id"],["g4_audit","g4_audit_pkey","PRIMARY KEY (id)"],["g4_audit","g4_audit_target_not_null","NOT NULL target"],["g4_backup","g4_backup_created_at_not_null","NOT NULL created_at"],["g4_backup","g4_backup_expires_at_not_null","NOT NULL expires_at"],["g4_backup","g4_backup_id_not_null","NOT NULL id"],["g4_backup","g4_backup_node_id_not_null","NOT NULL node_id"],["g4_backup","g4_backup_path_not_null","NOT NULL path"],["g4_backup","g4_backup_pkey","PRIMARY KEY (id)"],["g4_backup","g4_backup_seat_id_not_null","NOT NULL seat_id"],["g4_backup","g4_backup_size_bytes_not_null","NOT NULL size_bytes"],["g4_backup","g4_backup_state_check","CHECK ((state = ANY (ARRAY['pending'::text, 'complete'::text, 'expired'::text])))"],["g4_backup","g4_backup_state_not_null","NOT NULL state"],["g4_hosted_seat","g4_hosted_seat_account_ref_fkey","FOREIGN KEY (account_ref) REFERENCES g4_account_pool(id)"],["g4_hosted_seat","g4_hosted_seat_agent_id_not_null","NOT NULL agent_id"],["g4_hosted_seat","g4_hosted_seat_cli_not_null","NOT NULL cli"],["g4_hosted_seat","g4_hosted_seat_created_at_not_null","NOT NULL created_at"],["g4_hosted_seat","g4_hosted_seat_created_by_not_null","NOT NULL created_by"],["g4_hosted_seat","g4_hosted_seat_cred_kind_check","CHECK ((cred_kind = ANY (ARRAY['api_key'::text, 'token_file'::text])))"],["g4_hosted_seat","g4_hosted_seat_cred_kind_not_null","NOT NULL cred_kind"],["g4_hosted_seat","g4_hosted_seat_image_ref_not_null","NOT NULL image_ref"],["g4_hosted_seat","g4_hosted_seat_llm_not_null","NOT NULL llm"],["g4_hosted_seat","g4_hosted_seat_model_not_null","NOT NULL model"],["g4_hosted_seat","g4_hosted_seat_node_id_fkey","FOREIGN KEY (node_id) REFERENCES g4_node(id)"],["g4_hosted_seat","g4_hosted_seat_node_id_not_null","NOT NULL node_id"],["g4_hosted_seat","g4_hosted_seat_pkey","PRIMARY KEY (agent_id)"],["g4_hosted_seat","g4_hosted_seat_plan_check","CHECK ((plan = ANY (ARRAY['1c1.5g40'::text, '2c4g'::text, '4c8g'::text])))"],["g4_hosted_seat","g4_hosted_seat_plan_not_null","NOT NULL plan"],["g4_hosted_seat","g4_hosted_seat_state_check","CHECK ((state = ANY (ARRAY['queued'::text, 'pulling'::text, 'starting'::text, 'online'::text, 'busy'::text, 'disabled'::text, 'stopped'::text, 'error'::text, 'destroyed'::text])))"],["g4_hosted_seat","g4_hosted_seat_state_detail_not_null","NOT NULL state_detail"],["g4_hosted_seat","g4_hosted_seat_state_not_null","NOT NULL state"],["g4_hosted_seat","g4_hosted_seat_updated_at_not_null","NOT NULL updated_at"],["g4_lifecycle_job","g4_lifecycle_job_agent_id_fkey","FOREIGN KEY (agent_id) REFERENCES g4_hosted_seat(agent_id)"],["g4_lifecycle_job","g4_lifecycle_job_agent_id_not_null","NOT NULL agent_id"],["g4_lifecycle_job","g4_lifecycle_job_args_not_null","NOT NULL args"],["g4_lifecycle_job","g4_lifecycle_job_created_at_not_null","NOT NULL created_at"],["g4_lifecycle_job","g4_lifecycle_job_created_by_not_null","NOT NULL created_by"],["g4_lifecycle_job","g4_lifecycle_job_id_not_null","NOT NULL id"],["g4_lifecycle_job","g4_lifecycle_job_op_check","CHECK ((op = ANY (ARRAY['upgrade'::text, 'restart'::text, 'disable'::text, 'enable'::text, 'destroy'::text, 'refresh_cred'::text])))"],["g4_lifecycle_job","g4_lifecycle_job_op_not_null","NOT NULL op"],["g4_lifecycle_job","g4_lifecycle_job_pkey","PRIMARY KEY (id)"],["g4_lifecycle_job","g4_lifecycle_job_reason_not_null","NOT NULL reason"],["g4_lifecycle_job","g4_lifecycle_job_stage_check","CHECK ((stage = ANY (ARRAY['queued'::text, 'running'::text, 'completed'::text, 'failed'::text])))"],["g4_lifecycle_job","g4_lifecycle_job_stage_not_null","NOT NULL stage"],["g4_lifecycle_job","g4_lifecycle_job_updated_at_not_null","NOT NULL updated_at"],["g4_node","g4_node_capacity_not_null","NOT NULL capacity"],["g4_node","g4_node_created_at_not_null","NOT NULL created_at"],["g4_node","g4_node_docker_version_not_null","NOT NULL docker_version"],["g4_node","g4_node_id_not_null","NOT NULL id"],["g4_node","g4_node_labels_not_null","NOT NULL labels"],["g4_node","g4_node_name_key","UNIQUE (name)"],["g4_node","g4_node_name_not_null","NOT NULL name"],["g4_node","g4_node_node_protocol_version_not_null","NOT NULL node_protocol_version"],["g4_node","g4_node_pkey","PRIMARY KEY (id)"],["g4_node","g4_node_state_check","CHECK ((state = ANY (ARRAY['online'::text, 'offline'::text])))"],["g4_node","g4_node_state_not_null","NOT NULL state"],["g4_node","g4_node_updated_at_not_null","NOT NULL updated_at"],["g4_node_credential","g4_node_credential_issued_at_not_null","NOT NULL issued_at"],["g4_node_credential","g4_node_credential_issued_by_not_null","NOT NULL issued_by"],["g4_node_credential","g4_node_credential_node_id_fkey","FOREIGN KEY (node_id) REFERENCES g4_node(id)"],["g4_node_credential","g4_node_credential_node_id_not_null","NOT NULL node_id"],["g4_node_credential","g4_node_credential_pkey","PRIMARY KEY (node_id)"],["g4_node_credential","g4_node_credential_secret_hash_not_null","NOT NULL secret_hash"],["g4_quota","g4_quota_agents_limit_not_null","NOT NULL agents_limit"],["g4_quota","g4_quota_disk_mb_limit_not_null","NOT NULL disk_mb_limit"],["g4_quota","g4_quota_mem_mb_limit_not_null","NOT NULL mem_mb_limit"],["g4_quota","g4_quota_pkey","PRIMARY KEY (user_lc)"],["g4_quota","g4_quota_updated_at_not_null","NOT NULL updated_at"],["g4_quota","g4_quota_updated_by_not_null","NOT NULL updated_by"],["g4_quota","g4_quota_user_lc_not_null","NOT NULL user_lc"],["g4_quota","g4_quota_vcpu_limit_not_null","NOT NULL vcpu_limit"],["g4_quota_notice","g4_quota_notice_event_id_not_null","NOT NULL event_id"],["g4_quota_notice","g4_quota_notice_measured_at_not_null","NOT NULL measured_at"],["g4_quota_notice","g4_quota_notice_next_attempt_at_not_null","NOT NULL next_attempt_at"],["g4_quota_notice","g4_quota_notice_node_id_not_null","NOT NULL node_id"],["g4_quota_notice","g4_quota_notice_pkey","PRIMARY KEY (node_id, event_id, recipient)"],["g4_quota_notice","g4_quota_notice_reason_not_null","NOT NULL reason"],["g4_quota_notice","g4_quota_notice_recipient_check","CHECK ((recipient = ANY (ARRAY['owner'::text, 'ops'::text])))"],["g4_quota_notice","g4_quota_notice_recipient_not_null","NOT NULL recipient"],["g4_quota_notice","g4_quota_notice_seat_id_not_null","NOT NULL seat_id"],["g4_quota_notice","g4_quota_notice_writer_kind_not_null","NOT NULL writer_kind"],["g4_seat_event","g4_seat_event_agent_id_fkey","FOREIGN KEY (agent_id) REFERENCES g4_hosted_seat(agent_id)"],["g4_seat_event","g4_seat_event_agent_id_not_null","NOT NULL agent_id"],["g4_seat_event","g4_seat_event_at_not_null","NOT NULL at"],["g4_seat_event","g4_seat_event_id_not_null","NOT NULL id"],["g4_seat_event","g4_seat_event_kind_not_null","NOT NULL kind"],["g4_seat_event","g4_seat_event_pkey","PRIMARY KEY (id)"],["g4_team_key","g4_team_key_created_at_not_null","NOT NULL created_at"],["g4_team_key","g4_team_key_created_by_not_null","NOT NULL created_by"],["g4_team_key","g4_team_key_pkey","PRIMARY KEY (version)"],["g4_team_key","g4_team_key_state_check","CHECK ((state = ANY (ARRAY['active'::text, 'retired'::text])))"],["g4_team_key","g4_team_key_state_not_null","NOT NULL state"],["g4_team_key","g4_team_key_version_not_null","NOT NULL version"],["g4_team_key","g4_team_key_wrapped_not_null","NOT NULL wrapped"]]}$r31catalog$::jsonb;
BEGIN
 IF EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE 'g4\_%' AND c.relkind='r') THEN
 SELECT jsonb_build_object(
 'columns',(SELECT jsonb_agg(jsonb_build_array(c.relname,a.attname,format_type(a.atttypid,a.atttypmod),a.attnotnull,pg_get_expr(d.adbin,d.adrelid)) ORDER BY c.relname,a.attnum) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace JOIN pg_attribute a ON a.attrelid=c.oid LEFT JOIN pg_attrdef d ON d.adrelid=c.oid AND d.adnum=a.attnum WHERE n.nspname=current_schema() AND c.relname LIKE 'g4\_%' AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped),
 'constraints',(SELECT jsonb_agg(jsonb_build_array(c.relname,k.conname,pg_get_constraintdef(k.oid)) ORDER BY c.relname,k.conname) FROM pg_constraint k JOIN pg_class c ON c.oid=k.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE 'g4\_%'),
 'indexes',(SELECT jsonb_agg(jsonb_build_array(tablename,indexname,replace(indexdef,current_schema()||'.','')) ORDER BY tablename,indexname) FROM pg_indexes WHERE schemaname=current_schema() AND tablename LIKE 'g4\_%')) INTO actual;
 IF actual IS DISTINCT FROM expected THEN RAISE EXCEPTION 'r31 hosted baseline shape mismatch; refusing adoption'; END IF;
 END IF;
END $adopt$;
-- Existing complete R30 shapes are adopted without replaying data backfills
-- or rebuilding indexes/constraints. Only an empty hosted domain runs the DDL.
DO $create_baseline$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relname LIKE 'g4\_%' AND c.relkind='r') THEN
-- R30_FROZEN_DDL_START
-- Frozen hosted SchemaDDL from integration/spec5ff-r30 3d99db2b. Never edit after application.

-- Durable two-recipient notification outbox. Node event receipt and notices are
-- committed in one transaction before the node is allowed to discard its event.
CREATE TABLE IF NOT EXISTS g4_quota_notice (
  node_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  recipient TEXT NOT NULL CHECK (recipient IN ('owner','ops')),
  seat_id TEXT NOT NULL,
  reason TEXT NOT NULL,
  writer_kind TEXT NOT NULL DEFAULT '',
  measured_at timestamptz NOT NULL,
  delivered_at timestamptz,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (node_id,event_id,recipient));
CREATE INDEX IF NOT EXISTS g4_quota_notice_pending_idx ON g4_quota_notice(next_attempt_at,measured_at) WHERE delivered_at IS NULL;
CREATE INDEX IF NOT EXISTS g4_quota_notice_node_idx ON g4_quota_notice(node_id,delivered_at);

-- 托管节点（G1 准备的宿主；节点代理登记）
CREATE TABLE IF NOT EXISTS g4_node (
  id            TEXT PRIMARY KEY,             -- nd_<hex16>
  name          TEXT NOT NULL UNIQUE,
  labels        JSONB NOT NULL DEFAULT '{}',  -- {site, spec, pool}
  state         TEXT NOT NULL DEFAULT 'offline'
                CHECK (state IN ('online','offline')),
  capacity      JSONB NOT NULL DEFAULT '{}',  -- 最近一次上报：cpu/mem/disk 总量与余量
  docker_version TEXT NOT NULL DEFAULT '',
  node_protocol_version INT NOT NULL DEFAULT 1,
  last_seen_at  timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now());

-- 节点凭据：只存哈希（轮换/撤销经 rotated_at/revoked_at；发行即旧替新）
CREATE TABLE IF NOT EXISTS g4_node_credential (
  node_id     TEXT PRIMARY KEY REFERENCES g4_node(id),
  secret_hash TEXT NOT NULL,
  issued_by   TEXT NOT NULL,
  issued_at   timestamptz NOT NULL DEFAULT now(),
  rotated_at  timestamptz,
  revoked_at  timestamptz);

-- LLM 账号池。🔴 0.2.1.101 业主新裁（10-06，R-V2-ACCOUNT-PER-USER）：账号按人隔离（owner_lc），个人凭据进平台，
-- 以团队数据密钥加密——平台持有者（主密钥持有人）技术上可解密；原「团队资源；个人凭据不进平台」作废。
CREATE TABLE IF NOT EXISTS g4_account_pool (
  id         TEXT PRIMARY KEY,            -- acct_<hex16>
  name       TEXT NOT NULL,                -- 本人内唯一（g4_account_pool_owner_name），不再全局唯一
  kind       TEXT NOT NULL CHECK (kind IN ('api_key','token_file')),
  cli        TEXT NOT NULL,
  llm        TEXT NOT NULL,
  secret_enc BYTEA NOT NULL,              -- 团队数据密钥加密的密文（信封加密）
  key_version INT NOT NULL,               -- 团队数据密钥版本号
  last4      TEXT NOT NULL DEFAULT '',    -- 登记时算好存下——console 只显示后四位，永不为此解密
  state      TEXT NOT NULL DEFAULT 'available'
             CHECK (state IN ('available','bound','disabled')),
  bound_agent_id TEXT,                    -- 历史列（0.2.1.101 起不再写）：占用改以 g4_hosted_seat.account_ref 为准
  created_by TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());

-- 账号可用范围：按用户授权（组长定 da622016）。0.2.1.101 起不再读写（归属改 owner_lc），留作历史与回退兼容
CREATE TABLE IF NOT EXISTS g4_account_grant (
  account_id TEXT NOT NULL REFERENCES g4_account_pool(id),
  user_lc    TEXT NOT NULL,
  granted_by TEXT NOT NULL,
  granted_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (account_id, user_lc));

-- 托管席位：以 identity 的 agent_id 为主键（不另立席位身份与名称空间）
CREATE TABLE IF NOT EXISTS g4_hosted_seat (
  agent_id    TEXT PRIMARY KEY,           -- identity.agents 的 id（ag_<hex16>）
  node_id     TEXT NOT NULL REFERENCES g4_node(id),
  cli         TEXT NOT NULL,
  llm         TEXT NOT NULL,
  cred_kind   TEXT NOT NULL CHECK (cred_kind IN ('api_key','token_file')),
  account_ref TEXT REFERENCES g4_account_pool(id),
  plan        TEXT NOT NULL CHECK (plan IN ('1c1.5g40','2c4g','4c8g')),
  model       TEXT NOT NULL DEFAULT '', -- 生效模型（包 B2 修订 3：建席时定死、落库；upgrade/restart 按记录值重算）
  state       TEXT NOT NULL DEFAULT 'queued'
              CHECK (state IN ('queued','pulling','starting','online','busy',
                               'disabled','stopped','error','destroyed')),
  state_detail TEXT NOT NULL DEFAULT '',  -- 机器码（契约 v2 错误码表）
  image_ref   TEXT NOT NULL DEFAULT '',   -- 当前镜像 ref（含 digest）
  created_by  TEXT NOT NULL,
  created_at  timestamptz NOT NULL DEFAULT now(),
  updated_at  timestamptz NOT NULL DEFAULT now());

-- 生命周期事件（状态机轨迹；审计的载体之一）
CREATE TABLE IF NOT EXISTS g4_seat_event (
  id       BIGSERIAL PRIMARY KEY,
  agent_id TEXT NOT NULL REFERENCES g4_hosted_seat(agent_id),
  kind     TEXT NOT NULL,   -- create/pull/start/online/upgrade/restart/disable/enable/destroy/fail
  detail   JSONB,
  at       timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_seat_event_agent_idx ON g4_seat_event(agent_id, id);

-- 生命周期异步任务：独立 job_id 防止同一席位的多轮操作互相串读。
CREATE TABLE IF NOT EXISTS g4_lifecycle_job (
  id         TEXT PRIMARY KEY,
  agent_id   TEXT NOT NULL REFERENCES g4_hosted_seat(agent_id),
  created_by TEXT NOT NULL,
  op         TEXT NOT NULL CHECK (op IN ('upgrade','restart','disable','enable','destroy','refresh_cred')),
  args       JSONB NOT NULL DEFAULT '{}',
  stage      TEXT NOT NULL DEFAULT 'queued'
             CHECK (stage IN ('queued','running','completed','failed')),
  reason     TEXT NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_lifecycle_job_agent_idx ON g4_lifecycle_job(agent_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS g4_lifecycle_job_one_active_idx ON g4_lifecycle_job(agent_id)
  WHERE stage IN ('queued','running');

-- 用户配额（管理层设；新上限低于已用量允许保存、只限新建——G6）
CREATE TABLE IF NOT EXISTS g4_quota (
  user_lc       TEXT PRIMARY KEY,
  agents_limit  INT    NOT NULL DEFAULT 4,      -- 业主 8971bddf：默认 Agent 数 4
  mem_mb_limit  BIGINT NOT NULL DEFAULT 16384,  -- 默认内存 16GB
  disk_mb_limit BIGINT NOT NULL DEFAULT 65536,  -- 默认磁盘 64GB
  vcpu_limit    INT    NOT NULL DEFAULT 4,      -- 默认 vCPU 4
  updated_by    TEXT NOT NULL,
  updated_at    timestamptz NOT NULL DEFAULT now());

-- 团队数据密钥（信封加密的中层）：库里只存被主密钥包裹后的密文＋版本号；主密钥不进库
CREATE TABLE IF NOT EXISTS g4_team_key (
  version    INT PRIMARY KEY,
  wrapped    BYTEA NOT NULL,              -- 主密钥包裹后的团队数据密钥
  state      TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active','retired')),
  created_by TEXT NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now());

-- G4 审计（账号登记/改密/授权变更/解密下发/绑定解绑/生命周期；actor/action/target 三段）
CREATE TABLE IF NOT EXISTS g4_audit (
  id     BIGSERIAL PRIMARY KEY,
  actor  TEXT NOT NULL,
  action TEXT NOT NULL,
  target TEXT NOT NULL DEFAULT '',
  detail JSONB,
  at     timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_audit_action_idx ON g4_audit(action, id);

-- 销毁备份登记（T-G4-53 备份管线：节点出卷→Hub 收落 dw 的 /data→14 天到期删；不落节点盘）
CREATE TABLE IF NOT EXISTS g4_backup (
  id          TEXT PRIMARY KEY,             -- bk_<hex16>
  node_id     TEXT NOT NULL,
  seat_id     TEXT NOT NULL,
  state       TEXT NOT NULL DEFAULT 'pending'
              CHECK (state IN ('pending','complete','expired')),
  path        TEXT NOT NULL DEFAULT '',     -- Hub 本地落盘路径（dw 的 /data 下）
  size_bytes  BIGINT NOT NULL DEFAULT 0,
  expires_at  timestamptz NOT NULL,         -- 保留 14 天到期删（业主 8971bddf）
  created_at  timestamptz NOT NULL DEFAULT now());
CREATE INDEX IF NOT EXISTS g4_backup_state_idx ON g4_backup(state, expires_at);

-- 包 B2（R-V2-SEAT-B2-SCHEMA-20261005002126）：g4_ 首次加列。生产已有 g4_hosted_seat（托管面已开、
-- 业主席位在跑）⇒ CREATE IF NOT EXISTS 不会加列，须幂等 ALTER；存量行 model='' ＝ 走节点默认。
ALTER TABLE g4_hosted_seat ADD COLUMN IF NOT EXISTS model TEXT NOT NULL DEFAULT '';

-- 2026-10-17: add the minimum plan without rewriting existing seats or quotas.
-- ApplySchema executes this DDL in one implicit transaction, including replacement
-- of the deployed inline CHECK; CREATE IF NOT EXISTS alone cannot upgrade it.
ALTER TABLE g4_hosted_seat DROP CONSTRAINT IF EXISTS g4_hosted_seat_plan_check;
ALTER TABLE g4_hosted_seat ADD CONSTRAINT g4_hosted_seat_plan_check
  CHECK (plan IN ('1c1.5g40','2c4g','4c8g'));

-- 0.2.1.101 账号包（R-V2-ACCOUNT-PER-USER）。整段 SchemaDDL 以单条多语句执行＝PG 隐式单事务：
-- 下面的 DROP＋ADD 不会留下「无约束」窗口。全部幂等，每次启动执行。
-- ① 归属：owner_lc＝identity.User.UserIDLC()（与 N3.Actor、席位 created_by 同形）；无主行＝''（任何人不可见）。
ALTER TABLE g4_account_pool ADD COLUMN IF NOT EXISTS owner_lc TEXT NOT NULL DEFAULT '';
-- ② 名字：全局唯一 → 本人内唯一（全局唯一会让「名称已存在」泄露他人账号名）。
ALTER TABLE g4_account_pool DROP CONSTRAINT IF EXISTS g4_account_pool_name_key;
CREATE UNIQUE INDEX IF NOT EXISTS g4_account_pool_owner_name ON g4_account_pool(owner_lc, name);
CREATE INDEX IF NOT EXISTS g4_account_pool_owner ON g4_account_pool(owner_lc);
-- ③ 存量回填：恰有一条授权的账号 ⇒ 归该用户；已有属主的行不动（owner_lc='' 条件）；多授权／无授权保持无主。
UPDATE g4_account_pool p SET owner_lc = g.user_lc
  FROM (SELECT account_id, min(user_lc) AS user_lc, count(*) AS n FROM g4_account_grant GROUP BY account_id) g
 WHERE p.id = g.account_id AND g.n = 1 AND p.owner_lc = '';
-- ④ 生命周期 job 加 refresh_cred（刷新凭据，R-V2-ACCOUNT-REFRESH-CRED）：存量表的内联 CHECK 换新。
ALTER TABLE g4_lifecycle_job DROP CONSTRAINT IF EXISTS g4_lifecycle_job_op_check;
ALTER TABLE g4_lifecycle_job ADD CONSTRAINT g4_lifecycle_job_op_check
  CHECK (op IN ('upgrade','restart','disable','enable','destroy','refresh_cred'));

-- 0.2.1.101 账号登录（R-V2-ACCOUNT-LOGIN-FLOW，codex 设备码）：登录会话。链接与码只在进行中保留，结束即清空；
-- 凭证永不入本表（节点主动上传后直接加密入池）。id＝32 位十六进制随机（上传端点的不可猜部分）。
CREATE TABLE IF NOT EXISTS g4_account_login (
  id          TEXT PRIMARY KEY,
  owner_lc    TEXT NOT NULL,
  cli         TEXT NOT NULL,
  llm         TEXT NOT NULL,
  email       TEXT NOT NULL,
  account_id  TEXT,                       -- 重新登录的目标账号（新登记为 NULL）
  node_id     TEXT NOT NULL,              -- 执行节点（上传只认它）
  state       TEXT NOT NULL CHECK (state IN ('starting','waiting_user','completing','done','failed','expired','cancelled')),
  url         TEXT NOT NULL DEFAULT '',
  user_code   TEXT NOT NULL DEFAULT '',
  reason      TEXT NOT NULL DEFAULT '',
  result_account_id TEXT,
  created_at  timestamptz NOT NULL DEFAULT now(),
  expires_at  timestamptz NOT NULL,
  finished_at timestamptz);
CREATE INDEX IF NOT EXISTS g4_account_login_owner ON g4_account_login(owner_lc, created_at);
CREATE INDEX IF NOT EXISTS g4_account_login_active ON g4_account_login(state) WHERE state IN ('starting','waiting_user','completing');
-- 每人同一时刻至多 1 个进行中登录（库层保证，不靠先查后插）。
CREATE UNIQUE INDEX IF NOT EXISTS g4_account_login_one_active ON g4_account_login(owner_lc) WHERE state IN ('starting','waiting_user','completing');
-- completing＝上传已被原子认领、正在入池（清扫与取消不碰它；认领超过 2 分钟仍未收尾或 Hub 重启 ⇒ failed/interrupted）。
ALTER TABLE g4_account_login ADD COLUMN IF NOT EXISTS claimed_at timestamptz;
-- 幂等升级（复核第 4 批建议 2）：若某库曾应用过早期定义（CHECK 无 completing、索引旧谓词），在此换新；同一隐式事务。
ALTER TABLE g4_account_login DROP CONSTRAINT IF EXISTS g4_account_login_state_check;
ALTER TABLE g4_account_login ADD CONSTRAINT g4_account_login_state_check
  CHECK (state IN ('starting','waiting_user','completing','done','failed','expired','cancelled'));
DROP INDEX IF EXISTS g4_account_login_one_active;
CREATE UNIQUE INDEX g4_account_login_one_active ON g4_account_login(owner_lc) WHERE state IN ('starting','waiting_user','completing');
DROP INDEX IF EXISTS g4_account_login_active;
CREATE INDEX g4_account_login_active ON g4_account_login(state) WHERE state IN ('starting','waiting_user','completing');
-- 账号登录入池的账号记邮箱（显示用；个人信息——只给属主本人，管理员列表不给）。
ALTER TABLE g4_account_pool ADD COLUMN IF NOT EXISTS email TEXT NOT NULL DEFAULT '';

-- R30_FROZEN_DDL_END
 END IF;
END $create_baseline$;
