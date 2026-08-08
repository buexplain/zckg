// 本文件为 PostgreSQL 方言单元测试——GroupBy/Having 分组与分组过滤。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// TestBug_PgHavingRawPlaceholder 验证 PostgreSQL HavingRaw 中 ? 应转换为 $N。
func TestBug_PgHavingRawPlaceholder(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("orders").
		Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 500)

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	if containsStr(sql, "?") {
		t.Errorf("PostgreSQL HavingRaw 中 ? 未转换为 $N:\n  got: %s", sql)
	}
	expectedSQL := `SELECT "user_id" FROM "orders" GROUP BY "user_id" HAVING SUM(amount) > $1`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{500}, args)
}
