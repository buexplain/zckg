package zcdb

import (
	"errors"
	"testing"
)

// ==================== new-api-implement-list.md 新增 API 编译层单元测试 ====================
//
// 覆盖集成测试未断言的编译形态与错误路径：SQLite 方言编译形态、WhereNot/All/None SQL 形态、
// ToAggregate（含 UNION 包裹与非法聚合）、ToIncrement/ToDecrement（含 JOIN 绑定顺序方言差异）、
// ToDeleteJoin 无 JOIN 错误、Where 三参非法运算符等。

// ==================== SQLite 方言编译形态 ====================

// TestNewApi_SQLiteCompileForms 验证 SQLite 方言新增条件的编译形态。
func TestNewApi_SQLiteCompileForms(t *testing.T) {
	g := NewSQLiteGrammar()

	tests := []struct {
		name    string
		builder func() *Builder
		sql     string
		args    []any
	}{
		{"WhereShorthand", func() *Builder {
			return NewBuilder(g, nil).Table("users").Where("age", 25)
		}, `SELECT * FROM "users" WHERE "age" = ?`, []any{25}},
		{"WhereNilEq", func() *Builder {
			return NewBuilder(g, nil).Table("users").Where("age", "=", nil)
		}, `SELECT * FROM "users" WHERE "age" IS NULL`, nil},
		{"WhereNilNe", func() *Builder {
			return NewBuilder(g, nil).Table("users").Where("age", "<>", nil)
		}, `SELECT * FROM "users" WHERE "age" IS NOT NULL`, nil},
		{"WhereDate", func() *Builder {
			return NewBuilder(g, nil).Table("events").WhereDate("happened_at", "2024-06-15")
		}, `SELECT * FROM "events" WHERE strftime('%Y-%m-%d', "happened_at") = ?`, []any{"2024-06-15"}},
		{"NullSafeEquals", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNullSafeEquals("age", 25)
		}, `SELECT * FROM "users" WHERE "age" IS ?`, []any{25}},
		{"NullSafeNotEquals", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNullSafeNotEquals("age", 25)
		}, `SELECT * FROM "users" WHERE "age" IS NOT ?`, []any{25}},
		{"LikeCaseSensitive", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereLike("name", "*li*", true)
		}, `SELECT * FROM "users" WHERE "name" GLOB ?`, []any{"*li*"}},
		{"LikeDefault", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereLike("name", "%li%")
		}, `SELECT * FROM "users" WHERE "name" LIKE ?`, []any{"%li%"}},
		{"WhereNullMulti", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNull("age", "email")
		}, `SELECT * FROM "users" WHERE "age" IS NULL AND "email" IS NULL`, nil},
		{"GroupByRaw", func() *Builder {
			return NewBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").GroupByRaw("user_id + ?", 0)
		}, `SELECT COUNT(*) FROM "orders" GROUP BY user_id + ?`, []any{0}},
		{"BetweenColumns", func() *Builder {
			return NewBuilder(g, nil).Table("ranges").WhereBetweenColumns("val", "lo", "hi")
		}, `SELECT * FROM "ranges" WHERE "val" BETWEEN "lo" AND "hi"`, nil},
		{"ValueBetween", func() *Builder {
			return NewBuilder(g, nil).Table("ranges").WhereValueBetween(5, "lo", "hi")
		}, `SELECT * FROM "ranges" WHERE ? BETWEEN "lo" AND "hi"`, []any{5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.sql, sql)
			assertArgs(t, tt.args, args)
		})
	}
}

// ==================== WhereNot / All / Any / None 编译形态 ====================

// TestNewApi_WhereNotAllNoneCompile 验证 WhereNot/All/Any/None 的括号/NOT 编译形态（三方言通用，以 MySQL 为例）。
func TestNewApi_WhereNotAllNoneCompile(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name    string
		builder func() *Builder
		sql     string
		args    []any
	}{
		{"WhereNot", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNot(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, "SELECT * FROM `users` WHERE NOT (`status` = ?)", []any{"active"}},
		{"OrWhereNot", func() *Builder {
			return NewBuilder(g, nil).Table("users").
				Where("id", "=", 1).
				OrWhereNot(func(q *Builder) { q.Where("age", ">", 18) })
		}, "SELECT * FROM `users` WHERE `id` = ? OR NOT (`age` > ?)", []any{1, 18}},
		{"WhereAll", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereAll(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE (`a` = ? AND `b` = ?)", []any{1, 2}},
		{"WhereAny", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereAny(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE (`a` = ? OR `b` = ?)", []any{1, 2}},
		{"WhereNone", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNone(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE NOT (`a` = ? OR `b` = ?)", []any{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.sql, sql)
			assertArgs(t, tt.args, args)
		})
	}
}

// ==================== Having 扩展编译形态 ====================

// TestNewApi_HavingCompile 验证 Having 两参简写/HavingNested/HavingNull 的编译形态。
func TestNewApi_HavingCompile(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name    string
		builder func() *Builder
		sql     string
		args    []any
	}{
		{"HavingShorthand", func() *Builder {
			return NewBuilder(g, nil).Table("users").SelectRaw("status, COUNT(*) AS cnt").
				GroupBy("status").Having("cnt", 5)
		}, "SELECT status, COUNT(*) AS cnt FROM `users` GROUP BY `status` HAVING `cnt` = ?", []any{5}},
		{"HavingNested", func() *Builder {
			return NewBuilder(g, nil).Table("orders").GroupBy("user_id").
				HavingNested(func(q *Builder) {
					q.Having("total", ">", 100).Having("count", "<", 10)
				})
		}, "SELECT * FROM `orders` GROUP BY `user_id` HAVING (`total` > ? AND `count` < ?)", []any{100, 10}},
		{"OrHavingNested", func() *Builder {
			return NewBuilder(g, nil).Table("orders").GroupBy("user_id").
				Having("total", ">", 250).
				OrHavingNested(func(q *Builder) { q.Having("total", "=", 30) })
		}, "SELECT * FROM `orders` GROUP BY `user_id` HAVING `total` > ? OR (`total` = ?)", []any{250, 30}},
		{"HavingNull", func() *Builder {
			return NewBuilder(g, nil).Table("users").GroupBy("dept_id").HavingNull("email")
		}, "SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NULL", nil},
		{"HavingNotNullMulti", func() *Builder {
			return NewBuilder(g, nil).Table("users").GroupBy("dept_id").HavingNotNull("email", "age")
		}, "SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NOT NULL AND `age` IS NOT NULL", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.sql, sql)
			assertArgs(t, tt.args, args)
		})
	}
}

// ==================== §24 ToAggregate ====================

// TestNewApi_ToAggregate 验证 ToAggregate 编译形态、UNION 包裹与非法聚合错误。
func TestNewApi_ToAggregate(t *testing.T) {
	t.Run("MySQL", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").ToAggregate("MAX", "age")
		assertNoError(t, err)
		assertSQL(t, "SELECT MAX(`age`) AS `aggregate` FROM `users`", sql)
		assertArgs(t, nil, args)
	})

	t.Run("Postgres", func(t *testing.T) {
		g := NewPostgresGrammar()
		sql, _, err := NewBuilder(g, nil).Table("users").ToAggregate("MIN", "age")
		assertNoError(t, err)
		assertSQL(t, `SELECT MIN("age") AS "aggregate" FROM "users"`, sql)
	})

	t.Run("SQLite", func(t *testing.T) {
		g := NewSQLiteGrammar()
		sql, _, err := NewBuilder(g, nil).Table("users").ToAggregate("AVG", "age")
		assertNoError(t, err)
		assertSQL(t, `SELECT AVG("age") AS "aggregate" FROM "users"`, sql)
	})

	t.Run("WithWhere", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").
			Where("status", "=", "active").ToAggregate("SUM", "age")
		assertNoError(t, err)
		assertSQL(t, "SELECT SUM(`age`) AS `aggregate` FROM `users` WHERE `status` = ?", sql)
		assertArgs(t, []any{"active"}, args)
	})

	t.Run("UnionWrap", func(t *testing.T) {
		g := NewMySQLGrammar()
		b := NewBuilder(g, nil).Table("orders_a").
			Union(NewBuilder(g, nil).Table("orders_b"))
		sql, _, err := b.ToAggregate("SUM", "amount")
		assertNoError(t, err)
		assertSQL(t,
			"SELECT SUM(`amount`) AS `aggregate` FROM ((SELECT * FROM `orders_a`) UNION (SELECT * FROM `orders_b`)) AS `t`",
			sql)
	})

	t.Run("InvalidAggregate", func(t *testing.T) {
		g := NewMySQLGrammar()
		_, _, err := NewBuilder(g, nil).Table("users").ToAggregate("COUNT", "age")
		if !errors.Is(err, ErrInvalidAggregate) {
			t.Errorf("expected ErrInvalidAggregate, got %v", err)
		}
	})

	t.Run("StateRestored", func(t *testing.T) {
		// 编译后 Builder 状态应恢复，不影响后续 ToSelect
		g := NewMySQLGrammar()
		b := NewBuilder(g, nil).Table("users").Select("name").Limit(10)
		_, _, err := b.ToAggregate("MAX", "age")
		assertNoError(t, err)
		sql, _, err := b.ToSelect()
		assertNoError(t, err)
		assertSQL(t, "SELECT `name` FROM `users` LIMIT 10", sql)
	})
}

// ==================== §32 ToIncrement / ToDecrement ====================

// TestNewApi_ToIncDec 验证 ToIncrement/ToDecrement 编译形态与 JOIN 绑定顺序方言差异。
func TestNewApi_ToIncDec(t *testing.T) {
	t.Run("MySQL_NoJoin", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("wallets").
			Where("id", "=", 1).
			ToIncrement([]string{"balance", "points"}, []any{10, 5})
		assertNoError(t, err)
		assertSQL(t, "UPDATE `wallets` SET `balance` = `balance` + ?, `points` = `points` + ? WHERE `id` = ?", sql)
		assertArgs(t, []any{10, 5, 1}, args)
	})

	t.Run("MySQL_JoinSetAfterJoin", func(t *testing.T) {
		// MySQL：JOIN → SET → WHERE 绑定顺序
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").
			Join("orders", "users.id", "=", "orders.user_id").
			Where("orders.amount", ">", 100).
			ToIncrement([]string{"age"}, []any{1})
		assertNoError(t, err)
		assertSQL(t,
			"UPDATE `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id` SET `age` = `age` + ? WHERE `orders`.`amount` > ?",
			sql)
		assertArgs(t, []any{1, 100}, args)
	})

	t.Run("Postgres_SetBeforeJoin", func(t *testing.T) {
		// PG：SET → JOIN(FROM) → WHERE 绑定顺序，$N 自动转换
		g := NewPostgresGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").
			Join("orders", "users.id", "=", "orders.user_id").
			Where("orders.amount", ">", 100).
			ToIncrement([]string{"age"}, []any{1})
		assertNoError(t, err)
		assertSQL(t,
			`UPDATE "users" SET "age" = "age" + $1 FROM "orders" WHERE "users"."id" = "orders"."user_id" AND "orders"."amount" > $2`,
			sql)
		assertArgs(t, []any{1, 100}, args)
	})

	t.Run("Decrement", func(t *testing.T) {
		g := NewSQLiteGrammar()
		sql, args, err := NewBuilder(g, nil).Table("wallets").
			Where("id", "=", 1).
			ToDecrement([]string{"balance"}, []any{30})
		assertNoError(t, err)
		assertSQL(t, `UPDATE "wallets" SET "balance" = "balance" - ? WHERE "id" = ?`, sql)
		assertArgs(t, []any{30, 1}, args)
	})

	t.Run("ColumnsMismatch", func(t *testing.T) {
		g := NewMySQLGrammar()
		_, _, err := NewBuilder(g, nil).Table("wallets").
			ToIncrement([]string{"balance"}, []any{10, 5})
		if !errors.Is(err, ErrIncrementColumns) {
			t.Errorf("expected ErrIncrementColumns, got %v", err)
		}
		_, _, err = NewBuilder(g, nil).Table("wallets").ToIncrement(nil, nil)
		if !errors.Is(err, ErrIncrementColumns) {
			t.Errorf("expected ErrIncrementColumns, got %v", err)
		}
	})
}

// ==================== §43 ToDeleteJoin ====================

// TestNewApi_ToDeleteJoin 验证 ToDeleteJoin 的校验错误路径（编译形态已由集成测试覆盖）。
func TestNewApi_ToDeleteJoin(t *testing.T) {
	g := NewMySQLGrammar()

	// 无 JOIN → ErrDeleteJoinNoJoin
	_, _, err := NewBuilder(g, nil).Table("users").Where("id", "=", 1).ToDeleteJoin()
	if !errors.Is(err, ErrDeleteJoinNoJoin) {
		t.Errorf("expected ErrDeleteJoinNoJoin, got %v", err)
	}

	// 无表名 → ErrEmptyTable
	_, _, err = NewBuilder(g, nil).Join("orders", "a", "=", "b").ToDeleteJoin()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// ==================== Where 简写错误路径 ====================

// TestNewApi_WhereShorthandInvalid 验证 Where 三参形式的非法运算符错误。
func TestNewApi_WhereShorthandInvalid(t *testing.T) {
	g := NewMySQLGrammar()

	// 三参形式 op 非 string → ErrInvalidOperator
	_, _, err := NewBuilder(g, nil).Table("users").Where("age", 25, 30).ToSelect()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}

	// 三参形式非法运算符 → ErrInvalidOperator
	_, _, err = NewBuilder(g, nil).Table("users").Where("age", "DROP", 30).ToSelect()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}
}

// ==================== §33 AddSelectSub 编译形态 ====================

// TestNewApi_AddSelectSubCompile 验证 AddSelectSub 标量子查询列的编译形态（PG $N）。
func TestNewApi_AddSelectSubCompile(t *testing.T) {
	t.Run("Postgres", func(t *testing.T) {
		g := NewPostgresGrammar()
		sub := NewBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").WhereRaw("orders.user_id = users.id")
		sql, _, err := NewBuilder(g, nil).Table("users").
			Select("id").AddSelectSub(sub, "order_count").ToSelect()
		assertNoError(t, err)
		assertSQL(t,
			`SELECT "id", (SELECT COUNT(*) FROM "orders" WHERE orders.user_id = users.id) AS "order_count" FROM "users"`,
			sql)
	})

	t.Run("MySQL", func(t *testing.T) {
		g := NewMySQLGrammar()
		sub := NewBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").Where("amount", ">", 100)
		sql, args, err := NewBuilder(g, nil).Table("users").
			Select("id").AddSelectSub(sub, "order_count").ToSelect()
		assertNoError(t, err)
		assertSQL(t,
			"SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `amount` > ?) AS `order_count` FROM `users`",
			sql)
		assertArgs(t, []any{100}, args)
	})
}
