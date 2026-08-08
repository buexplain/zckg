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

// TestPgGrammar_WhereNullSafe 验证空安全比较编译为 IS [NOT] DISTINCT FROM $N 形态。
func TestPgGrammar_WhereNullSafe(t *testing.T) {
	g := NewPostgresGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name:      "equals_nil",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeEquals("email", nil) },
			expected:  `SELECT * FROM "users" WHERE "email" IS NOT DISTINCT FROM $1`,
			expectedA: []any{nil},
		},
		{
			name:      "not_equals",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeNotEquals("email", "a@b.c") },
			expected:  `SELECT * FROM "users" WHERE "email" IS DISTINCT FROM $1`,
			expectedA: []any{"a@b.c"},
		},
		{
			name: "param_numbering_after_prior_binding",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("id", "=", 1).WhereNullSafeEquals("email", nil)
			},
			expected:  `SELECT * FROM "users" WHERE "id" = $1 AND "email" IS NOT DISTINCT FROM $2`,
			expectedA: []any{1, nil},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.build().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, tt.expectedA, args)
		})
	}
}
