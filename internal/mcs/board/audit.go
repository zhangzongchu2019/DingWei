// audit.go —— P3 集成件:收件面审计/角色查询的最小 Board API(供 mcsd 收件面接线)。
package board

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// AuditEvent 收件面审计事件落账(★criteria §3.6-F:每条拒路径必落账不可吞;H3 路由结果统一落 board_event)。
// actor=系统哨兵(system:identity-gate/system:route-gate)或已过闸 principal;禁匿名(与 insertEvent 同则)。
// 非 guarded:审计事件不迁移实体状态,event=审计+提示(不变量),写失败向上返错不静默。
func (b *Board) AuditEvent(ctx context.Context, kind, taskID, actor string, data map[string]any) error {
	if actor == "" {
		return errors.New("mcs/board: audit event without actor (禁匿名事件)")
	}
	payload := "{}"
	if data != nil {
		bs, err := json.Marshal(data)
		if err != nil {
			return err
		}
		payload = string(bs)
	}
	var tid any
	if taskID != "" {
		tid = taskID
	}
	_, err := b.db.ExecContext(ctx,
		`INSERT INTO board_event(kind,task_id,agent_id,ts,data) VALUES(?,?,?,?,?)`,
		kind, tid, actor, b.now(), payload)
	return err
}

// RoleOfWho task 内某 who 的角色(执行席优先;""=非成员)。
// ★caller 义务②(shence F1):空 taskID → 恒 ""(Authz 拒空 taskID,fail-closed)。
func (b *Board) RoleOfWho(ctx context.Context, taskID, who string) string {
	if taskID == "" || who == "" {
		return ""
	}
	var role string
	err := b.db.QueryRowContext(ctx, `
SELECT role FROM task_role WHERE task_id=? AND who=?
 ORDER BY CASE role WHEN 'implementer' THEN 0 WHEN 'tester' THEN 1 ELSE 2 END LIMIT 1`,
		taskID, who).Scan(&role)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return "" // 查询错=fail-closed 当非成员
		}
		return ""
	}
	return role
}
