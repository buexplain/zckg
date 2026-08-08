// 本文件为 PostgreSQL 方言单元测试——Builder 基础能力（方言特有的通用行为）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import "testing"

// TestPostgresGrammar_RawPlaceholderEscape 验证 PG 方言原始 SQL 占位符处理：
// ?? 转义为字面 ?（jsonb 键存在操作符）、Expression 内联不占编号、普通绑定顺序正确。
func TestPostgresGrammar_RawPlaceholderEscape(t *testing.T) {
	g := &PostgresGrammar{}

	tests := []struct {
		name     string
		builder  *Builder
		expected string
		args     []any
	}{
		{
			name:     "DoubleQuestionMarkEscapesToLiteral",
			builder:  NewBuilder(g, nil).Table("users").WhereRaw(`"options" ?? ?`, "foo"),
			expected: `SELECT * FROM "users" WHERE "options" ? $1`,
			args:     []any{"foo"},
		},
		{
			name:     "DoubleQuestionMarkWithoutBinding",
			builder:  NewBuilder(g, nil).Table("users").WhereRaw(`"options" ?? 'foo'`),
			expected: `SELECT * FROM "users" WHERE "options" ? 'foo'`,
			args:     []any{},
		},
		{
			name:     "ExpressionInlinedNotConsumingParam",
			builder:  NewBuilder(g, nil).Table("users").WhereRaw("age > ? AND age < ?", 20, NewExpression("40")),
			expected: `SELECT * FROM "users" WHERE age > $1 AND age < 40`,
			args:     []any{20},
		},
		{
			name:     "MixedLiteralOperatorAndBindings",
			builder:  NewBuilder(g, nil).Table("users").WhereRaw("\"a\" ?? ? AND \"b\" = ?", "k", 1),
			expected: `SELECT * FROM "users" WHERE "a" ? $1 AND "b" = $2`,
			args:     []any{"k", 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, tt.args, args)
		})
	}
}
