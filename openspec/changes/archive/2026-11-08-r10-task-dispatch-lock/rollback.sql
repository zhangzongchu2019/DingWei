-- 离线回退：先关闭 DW_TASK_DISPATCH_LOCK_ENABLED 并停止新 Hub，备份 SQLite。
-- 只撤 R10 平台权限，不证明旧执行已停止，不删除账/历史，不重派、不重发。
BEGIN IMMEDIATE;
CREATE TEMP TABLE r10_rollback_work AS
 SELECT *, 'r10_rollback_' || lower(hex(randomblob(16))) AS rollback_id,
 CASE WHEN updated_at_utc > strftime('%Y-%m-%dT%H:%M:%f000000Z','now') THEN updated_at_utc
 ELSE strftime('%Y-%m-%dT%H:%M:%f000000Z','now') END AS rollback_utc
 FROM r10_task_lock WHERE state='active';
INSERT INTO r10_task_lock_operation(operation_id,provider,instance,repository_id,change_directory,task_id,operation_kind,request_sha256,actor_kind,actor_user_id_lc,actor_agent_id,actor_owner_lc,expected_generation,result_generation,outcome,result_json,created_at_utc,completed_at_utc)
 SELECT rollback_id,provider,instance,repository_id,change_directory,task_id,'rollback',
 '3b1692ac28d155720d2332723dd829709ad2348628ab45327226b2a547a9b9c39',
 'system','','','',generation,generation+1,'rollback_invalidated',
 json_object('operation_id',rollback_id,'outcome','rollback_invalidated','current_lease',json('false'),'lock',json_object('provider',provider,'instance',instance,'repository',repository_id,'change_dir',change_directory,'task_id',task_id,'generation',generation+1,'state','expired','commit_sha',commit_sha)),rollback_utc,rollback_utc
 FROM r10_rollback_work;
INSERT INTO r10_task_lock_event(operation_id,provider,instance,repository_id,change_directory,task_id,generation,event_type,actor_kind,actor_user_id_lc,actor_agent_id,actor_owner_lc,at_utc,detail)
 SELECT rollback_id,provider,instance,repository_id,change_directory,task_id,generation+1,'rollback','system','','','',rollback_utc,
 json_object('reason','operator_disabled_r10','previous_generation',generation,'holder_agent_id',holder_agent_id,'holder_owner_lc',holder_owner_lc,'commit_sha',commit_sha)
 FROM r10_rollback_work;
UPDATE r10_task_lock SET state='expired',generation=generation+1,lease_token=lower(hex(randomblob(32))),
 updated_at_utc=(SELECT w.rollback_utc FROM r10_rollback_work w WHERE w.provider=r10_task_lock.provider AND w.instance=r10_task_lock.instance AND w.repository_id=r10_task_lock.repository_id AND w.change_directory=r10_task_lock.change_directory AND w.task_id=r10_task_lock.task_id)
 WHERE (provider,instance,repository_id,change_directory,task_id) IN (SELECT provider,instance,repository_id,change_directory,task_id FROM r10_rollback_work);
UPDATE r10_task_lock_outbox SET delivery_state='unknown',reason='operator_rollback_no_delivery_proof',completed_at_utc=strftime('%Y-%m-%dT%H:%M:%f000000Z','now')
 WHERE delivery_state IN ('pending','attempting');
DROP TABLE temp.r10_rollback_work;
COMMIT;
