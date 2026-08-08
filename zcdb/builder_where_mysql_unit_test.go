// 本文件为 MySQL 方言单元测试——Where 系列条件构造。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import "testing"

// TestMySQLGrammar_WhereBetweenColumns 验证列区间/值区间系列条件的编译形态（无绑定/单绑定、AND/OR 布尔连接）。
func TestMySQLGrammar_WhereBetweenColumns(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name: "between_columns",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("products").WhereBetweenColumns("price", "min_price", "max_price")
			},
			expected:  "SELECT * FROM `products` WHERE `price` BETWEEN `min_price` AND `max_price`",
			expectedA: []any{},
		},
		{
			name: "not_between_columns",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("products").WhereNotBetweenColumns("price", "min_price", "max_price")
			},
			expected:  "SELECT * FROM `products` WHERE `price` NOT BETWEEN `min_price` AND `max_price`",
			expectedA: []any{},
		},
		{
			name: "or_not_between_columns",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("products").Where("id", "=", 1).OrWhereNotBetweenColumns("price", "min_price", "max_price")
			},
			expected:  "SELECT * FROM `products` WHERE `id` = ? OR `price` NOT BETWEEN `min_price` AND `max_price`",
			expectedA: []any{1},
		},
		{
			name:      "value_between",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereValueBetween(25, "min_age", "max_age") },
			expected:  "SELECT * FROM `users` WHERE ? BETWEEN `min_age` AND `max_age`",
			expectedA: []any{25},
		},
		{
			name: "or_value_between",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("vip", "=", 1).OrWhereValueBetween(25, "min_age", "max_age")
			},
			expected:  "SELECT * FROM `users` WHERE `vip` = ? OR ? BETWEEN `min_age` AND `max_age`",
			expectedA: []any{1, 25},
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

// TestMySQLGrammar_WhereNullSafe 验证空安全比较编译为 <=> 与 NOT <=> 形态。
func TestMySQLGrammar_WhereNullSafe(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name:      "equals_nil",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeEquals("email", nil) },
			expected:  "SELECT * FROM `users` WHERE `email` <=> ?",
			expectedA: []any{nil},
		},
		{
			name:      "equals_value",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeEquals("email", "a@b.c") },
			expected:  "SELECT * FROM `users` WHERE `email` <=> ?",
			expectedA: []any{"a@b.c"},
		},
		{
			name:      "not_equals",
			build:     func() *Builder { return NewBuilder(g, nil).Table("users").WhereNullSafeNotEquals("email", "a@b.c") },
			expected:  "SELECT * FROM `users` WHERE NOT `email` <=> ?",
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

// TestMySQLGrammar_WhereNotAnyNone 验证 WhereNot/OrWhereNot/OrWhereAny/OrWhereNone 嵌套组编译形态。
func TestMySQLGrammar_WhereNotAnyNone(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name: "or_where_not",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("vip", "=", 1).
					OrWhereNot(func(q *Builder) { q.Where("status", "banned") })
			},
			expected:  "SELECT * FROM `users` WHERE `vip` = ? OR NOT (`status` = ?)",
			expectedA: []any{1, "banned"},
		},
		{
			name: "or_where_any",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("status", "active").
					OrWhereAny(func(q *Builder) { q.Where("age", ">", 60).Where("vip", "=", 1) })
			},
			expected:  "SELECT * FROM `users` WHERE `status` = ? OR (`age` > ? OR `vip` = ?)",
			expectedA: []any{"active", 60, 1},
		},
		{
			name: "or_where_none",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("vip", "=", 1).
					OrWhereNone(func(q *Builder) { q.Where("status", "banned").Where("age", "<", 18) })
			},
			expected:  "SELECT * FROM `users` WHERE `vip` = ? OR NOT (`status` = ? OR `age` < ?)",
			expectedA: []any{1, "banned", 18},
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
