-- B scheme session naming: migrate system producer owner to the canonical key.
UPDATE service_api_key
SET service_id = 'systemtaskintl'
WHERE service_id = 'system-v-task-internal';

INSERT OR IGNORE INTO api_key_account(api_key_id, chat_entity_id)
SELECT api_key_id, 'systemtaskintl'
FROM api_key_account
WHERE chat_entity_id = 'system-v-task-internal';

DELETE FROM api_key_account
WHERE chat_entity_id = 'system-v-task-internal';

UPDATE session_endpoint
SET owner_key = 'systemtaskintl'
WHERE owner_key = 'system-v-task-internal';

INSERT OR IGNORE INTO member(id, owner_key, display_name, feishu_open_id, role, evidence_optout, dm_optout, active)
SELECT id, 'systemtaskintl', display_name, feishu_open_id, role, evidence_optout, dm_optout, active
FROM member
WHERE owner_key = 'system-v-task-internal';

DELETE FROM member
WHERE owner_key = 'system-v-task-internal';

UPDATE registered_service
SET id = 'systemtaskintl'
WHERE id = 'system-v-task-internal'
  AND NOT EXISTS (SELECT 1 FROM registered_service WHERE id = 'systemtaskintl');

DELETE FROM registered_service
WHERE id = 'system-v-task-internal'
  AND EXISTS (SELECT 1 FROM registered_service WHERE id = 'systemtaskintl');
