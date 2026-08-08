// 本文件为 PostgreSQL 方言单元测试——Where 系列条件构造。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// TestBug_PgWhereRawPlaceholder 验证 PostgreSQL WhereRaw 中 ? 应转换为 $N。
func TestBug_PgWhereRawPlaceholder(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("users").WhereRaw("age > ? AND name LIKE ?", 25, "alice%")

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	if containsStr(sql, "?") {
		t.Errorf("PostgreSQL WhereRaw 中 ? 未转换为 $N:\n  got: %s", sql)
	}
	expectedSQL := `SELECT * FROM "users" WHERE age > $1 AND name LIKE $2`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{25, "alice%"}, args)
}
