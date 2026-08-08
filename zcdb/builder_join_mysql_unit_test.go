// 本文件为 MySQL 方言单元测试——Join 系列与 JoinBuilder 连接条件。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"errors"
	"testing"
)

// TestMySQLGrammar_JoinBuilderNullConditions 验证 JoinBuilder.WhereNull/WhereNotNull 多列展开编译形态。
func TestMySQLGrammar_JoinBuilderNullConditions(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "where_null_multi_columns",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("profiles", func(j *JoinBuilder) {
					j.On("profiles.user_id", "=", "users.id").WhereNull("profiles.avatar", "profiles.bio")
				})
			},
			expected: "SELECT * FROM `users` INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND `profiles`.`avatar` IS NULL AND `profiles`.`bio` IS NULL",
		},
		{
			name: "where_not_null",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("profiles", func(j *JoinBuilder) {
					j.On("profiles.user_id", "=", "users.id").WhereNotNull("profiles.avatar")
				})
			},
			expected: "SELECT * FROM `users` INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND `profiles`.`avatar` IS NOT NULL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.build().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{}, args)
		})
	}
}

// TestMySQLGrammar_JoinBuilderInConditions 验证 JoinBuilder.WhereIn/WhereNotIn 的值列表、空列表与子查询形态。
func TestMySQLGrammar_JoinBuilderInConditions(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name: "where_in_values",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").WhereIn("orders.status", []any{"paid", "shipped"})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` IN (?, ?)",
			expectedA: []any{"paid", "shipped"},
		},
		{
			name: "where_not_in_values",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").WhereNotIn("orders.status", []any{"cancelled"})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` NOT IN (?)",
			expectedA: []any{"cancelled"},
		},
		{
			name: "where_in_empty_values",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").WhereIn("orders.status", []any{})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND 0 = 1",
			expectedA: []any{},
		},
		{
			name: "where_not_in_empty_values",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").WhereNotIn("orders.status", []any{})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND 1 = 1",
			expectedA: []any{},
		},
		{
			name: "where_in_subquery",
			build: func() *Builder {
				sub := NewBuilder(g, nil).Table("vip_orders").Select("user_id")
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").WhereIn("orders.user_id", sub)
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`user_id` IN (SELECT `user_id` FROM `vip_orders`)",
			expectedA: []any{},
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

// TestMySQLGrammar_JoinBuilderWhereExists 验证 JoinBuilder.WhereExists 的 Builder/回调入参与非法入参错误。
func TestMySQLGrammar_JoinBuilderWhereExists(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("builder_argument", func(t *testing.T) {
		sub := NewBuilder(g, nil).Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
		sql, args, err := NewBuilder(g, nil).Table("users").JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").WhereExists(sub)
		}).ToSelect()
		assertNoError(t, err)
		assertSQL(t, "SELECT * FROM `users` INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)", sql)
		assertArgs(t, []any{}, args)
	})

	t.Run("callback_argument", func(t *testing.T) {
		sql, _, err := NewBuilder(g, nil).Table("users").JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").WhereExists(func(q *Builder) {
				q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
			})
		}).ToSelect()
		assertNoError(t, err)
		assertSQL(t, "SELECT * FROM `users` INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)", sql)
	})

	t.Run("invalid_argument_type", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("users").JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").WhereExists(123)
		}).ToSelect()
		if !errors.Is(err, ErrInvalidSubQuery) {
			t.Errorf("WhereExists(非 Builder/回调) 期望 ErrInvalidSubQuery，得到: %v", err)
		}
	})
}

// TestMySQLGrammar_JoinBuilderNested 验证 OnNested/OrWhereNested 嵌套条件组的括号编译形态。
func TestMySQLGrammar_JoinBuilderNested(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name: "on_nested",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").OnNested(func(q *JoinBuilder) {
						q.Where("orders.status", "=", "paid").OrWhere("orders.vip", "=", 1)
					})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND (`orders`.`status` = ? OR `orders`.`vip` = ?)",
			expectedA: []any{"paid", 1},
		},
		{
			name: "or_where_nested",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").OrWhereNested(func(q *JoinBuilder) {
						q.Where("orders.gift", "=", 1)
					})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` OR (`orders`.`gift` = ?)",
			expectedA: []any{1},
		},
		{
			name: "empty_nested_skipped",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").OnNested(func(q *JoinBuilder) {})
				})
			},
			expected:  "SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id`",
			expectedA: []any{},
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

// TestMySQLGrammar_JoinBuilderNestedJoin 验证 JoinBuilder 内嵌 JoinOn/CrossJoinOn 的嵌套 join 组编译形态。
func TestMySQLGrammar_JoinBuilderNestedJoin(t *testing.T) {
	g := NewMySQLGrammar()

	sql, args, err := NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").JoinOn("items", func(q *JoinBuilder) {
			q.On("items.order_id", "=", "orders.id")
		})
	}).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` INNER JOIN (`orders` INNER JOIN `items` ON `items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`", sql)
	assertArgs(t, []any{}, args)

	// CrossJoinOn 在 JoinBuilder 内嵌套 CROSS JOIN 带 ON 条件
	sql2, _, err2 := NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").CrossJoinOn("items", "items.order_id", "=", "orders.id")
	}).ToSelect()
	assertNoError(t, err2)
	assertSQL(t, "SELECT * FROM `users` INNER JOIN (`orders` CROSS JOIN `items` ON `items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`", sql2)
}
