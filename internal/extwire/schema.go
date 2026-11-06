package extwire

import (
	"context"
	"fmt"
	"strings"
)

// RequiredTables 外部席位链路（r53）启动前必须存在的 PG 表。
// 这些表不归 identity 域管，所以不放进 identity 的 pgRequiredTables（那会把两个域耦合，
// 且 identity 包自身的完整 schema 用例不含它们）；由集成装配在启动时单独核对，缺表即拒启动
// （rulings ㊹-补12 ⑦，fail closed）。D 路 0106 合入后在此追加其表名。
var RequiredTables = []string{
	// 0105_extseat.sql（A 路）
	"np_external_principal", "np_extseat", "np_extseat_token",
	"np_extseat_inbox", "np_extseat_send_key", "np_extseat_outbox",
}

// TableChecker 由装配层用实际的库连接实现（例如 SELECT to_regclass($1) IS NOT NULL）。
type TableChecker interface {
	TableExists(ctx context.Context, name string) (bool, error)
}

// CheckSchema 表都在才返回 nil；缺表时报出缺哪几张，供启动时拒绝运行。
func CheckSchema(ctx context.Context, c TableChecker) error {
	if c == nil {
		return fmt.Errorf("extwire: no table checker")
	}
	var missing []string
	for _, t := range RequiredTables {
		ok, err := c.TableExists(ctx, t)
		if err != nil {
			return fmt.Errorf("extwire: check table %s: %w", t, err)
		}
		if !ok {
			missing = append(missing, t)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("extwire: external-seat schema not applied, missing tables: %s", strings.Join(missing, ", "))
	}
	return nil
}
