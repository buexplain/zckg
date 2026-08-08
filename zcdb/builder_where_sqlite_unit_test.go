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
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeEquals("email", nil) },
			expected:  `SELECT * FROM "users" WHERE "email" IS ?`,
			expectedA: []any{nil},
		},
		{
			name:      "not_equals",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeNotEquals("email", "a@b.c") },
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
