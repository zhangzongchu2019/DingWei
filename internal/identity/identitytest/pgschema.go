package identitytest

import (
	"strings"

	"github.com/zhangzongchu2019/dingwei/internal/identity"
)

// testAuditLogDDL 审计复用既有 audit_log；正式环境由 G2 的迁移账本建。测试里建一张字段兼容的最小表。
const testAuditLogDDL = `CREATE TABLE IF NOT EXISTS audit_log (
	id bigserial PRIMARY KEY, actor text, action text, target text, before_json text, after_json text, ts timestamptz)`

// PGTestSchema 在测试库里建身份存储所需全部表的语句（最小 audit_log ＋ pgschema.sql 逐条），按顺序执行。
// 只供测试：正式环境的表由迁移账本建。
func PGTestSchema() []string {
	return append([]string{testAuditLogDDL}, splitSQL(identity.PGSchemaSQL)...)
}

// splitSQL 按分号切开迁移文件里的多条语句（文件里没有函数体与字符串内分号）。
func splitSQL(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ";") {
		var keep []string
		for _, ln := range strings.Split(part, "\n") {
			if t := strings.TrimSpace(ln); t != "" && !strings.HasPrefix(t, "--") {
				keep = append(keep, ln)
			}
		}
		if q := strings.TrimSpace(strings.Join(keep, "\n")); q != "" {
			out = append(out, q)
		}
	}
	return out
}
