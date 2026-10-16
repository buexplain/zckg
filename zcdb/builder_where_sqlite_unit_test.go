// 本文件为 SQLite 方言单元测试——Where 系列条件构造。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import "testing"

// TestSQLiteGrammar_WhereNullSafe 验证空安全比较编译为 IS ? / IS NOT ? 形态。
func TestSQLiteGrammar_WhereNullSafe(t *testing.T) {
	g := NewSQLiteGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name:      "equals_nil",
			build:     func() *Builder { return newTestBuilder(g, nil).Table("users").WhereNullSafeEquals("email", nil) },
			expected:  `SELECT * FROM "users" WHERE "email" IS ?`,
			expectedA: []any{nil},
		},
		{
			name:      "not_equals",
			build:     func() *Builder { return newTestBuilder(g, nil).Table("users").WhereNullSafeNotEquals("email", "a@b.c") },
			expected:  `SELECT * FROM "users" WHERE "email" IS NOT ?`,
			expectedA: []any{"a@b.c"},
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

// TestSQLiteGrammar_WhereRawLiteralQuestion 验证原始条件里只有代码区的 ? 是占位符：
// 字符串字面量（反斜杠不是转义符）、引号标识符与注释里的 ? 原样保留，不参与绑定。
func TestSQLiteGrammar_WhereRawLiteralQuestion(t *testing.T) {
	g := NewSQLiteGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name: "字面量内的问号不参与绑定",
			build: func() *Builder {
				return newTestBuilder(g, nil).Table("users").
					WhereRaw(`name LIKE '%?%' AND age > ?`, 18)
			},
			expected:  `SELECT * FROM "users" WHERE name LIKE '%?%' AND age > ?`,
			expectedA: []any{18},
		},
		{
			name: "反斜杠在字面量里不是转义符",
			build: func() *Builder {
				return newTestBuilder(g, nil).Table("users").
					WhereRaw(`note = 'a\' AND age > ?`, 18)
			},
			expected:  `SELECT * FROM "users" WHERE note = 'a\' AND age > ?`,
			expectedA: []any{18},
		},
		{
			name: "Expression 只取代代码区的问号",
			build: func() *Builder {
				return newTestBuilder(g, nil).Table("users").
					WhereRaw(`note = '?' AND ts > ?`, NewExpression("datetime('now')"))
			},
			expected:  `SELECT * FROM "users" WHERE note = '?' AND ts > datetime('now')`,
			expectedA: nil,
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
