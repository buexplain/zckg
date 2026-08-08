// 本文件为 PostgreSQL 方言单元测试——Join 系列与 JoinBuilder 连接条件。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// TestBug_PgJoinRawPlaceholder 验证 PostgreSQL JOIN ON Raw 中 ? 应转换为 $N。
func TestBug_PgJoinRawPlaceholder(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		jb.On("users.id", "=", "orders.user_id").
			Raw("orders.amount > ?", 100)
	})

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	// SQL 中不应出现 ? 占位符
	if containsStr(sql, "?") {
		t.Errorf("PostgreSQL JOIN ON Raw 中 ? 未转换为 $N:\n  got: %s", sql)
	}
	expectedSQL := `SELECT * FROM "users" INNER JOIN "orders" ON "users"."id" = "orders"."user_id" AND orders.amount > $1`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{100}, args)
}
