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

// TestPgGrammar_JoinBuilderValueParamNumbering 验证 JoinBuilder 值条件与 WHERE 绑定混合时 $N 编号全局递增。
func TestPgGrammar_JoinBuilderValueParamNumbering(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("users").
		Where("users.status", "=", "active").
		JoinOn("orders", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "orders.user_id").
				Where("orders.amount", ">", 100).
				WhereIn("orders.status", []any{"paid", "shipped"})
		})

	sql, args, err := b.ToSelect()
	assertNoError(t, err)
	// 绑定顺序 JOIN 先于 WHERE：JOIN 值条件占 $1~$3，WHERE 占 $4
	expectedSQL := `SELECT * FROM "users" INNER JOIN "orders" ON "users"."id" = "orders"."user_id" AND "orders"."amount" > $1 AND "orders"."status" IN ($2, $3) WHERE "users"."status" = $4`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{100, "paid", "shipped", "active"}, args)
}

// TestPgGrammar_JoinBuilderNullAndNested 验证 PG 方言 JoinBuilder 空值条件与嵌套条件组的编译形态。
func TestPgGrammar_JoinBuilderNullAndNested(t *testing.T) {
	g := NewPostgresGrammar()

	tests := []struct {
		name      string
		build     func() *Builder
		expected  string
		expectedA []any
	}{
		{
			name: "where_null_and_not_null",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("profiles", func(j *JoinBuilder) {
					j.On("profiles.user_id", "=", "users.id").WhereNull("profiles.avatar").WhereNotNull("profiles.bio")
				})
			},
			expected:  `SELECT * FROM "users" INNER JOIN "profiles" ON "profiles"."user_id" = "users"."id" AND "profiles"."avatar" IS NULL AND "profiles"."bio" IS NOT NULL`,
			expectedA: []any{},
		},
		{
			name: "on_nested",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").OnNested(func(q *JoinBuilder) {
						q.Where("orders.status", "=", "paid").OrWhere("orders.vip", "=", 1)
					})
				})
			},
			expected:  `SELECT * FROM "users" INNER JOIN "orders" ON "orders"."user_id" = "users"."id" AND ("orders"."status" = $1 OR "orders"."vip" = $2)`,
			expectedA: []any{"paid", 1},
		},
		{
			name: "where_not_in_empty_values",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
					j.On("orders.user_id", "=", "users.id").WhereNotIn("orders.status", []any{})
				})
			},
			expected:  `SELECT * FROM "users" INNER JOIN "orders" ON "orders"."user_id" = "users"."id" AND 1 = 1`,
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
