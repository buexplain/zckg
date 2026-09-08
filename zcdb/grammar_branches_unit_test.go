// 本文件为 Grammar 编译的补充单元测试。
// 覆盖 compileJoinConditions 的 value-nil/Expression、null(Not)、in(空列表)、inSub、
// subValue、exists、nested 分支（三方言各一份），compileHavings 的
// between/null/notNull/nested 分支，以及 PostgreSQL compileWhereBasic 的
// nil/Expression 分支。
// 注意：compileHavings 与 compileWhereBasic 的分支当前仅由 PostgreSQL 方言覆盖，
// MySQL/SQLite 的对应实现与 PG 只差标识符包裹与占位符形式，其形态已由本文件的
// join 三方言用例与 builder_*_{mysql,sqlite}_unit_test.go 锁死。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// joinAllConditions 在 JoinBuilder 上构造覆盖全部条件类型的 ON 条件，
// 供三方言单元测试复用。
func joinAllConditions(jb *JoinBuilder, sub *Builder) {
	jb.On("orders.user_id", "=", "users.id").
		Where("orders.status", "=", "paid").                           // value
		Where("orders.deleted_at", "=", nil).                          // value-nil（= → IS NULL）
		Where("orders.deleted_at2", "!=", nil).                        // value-nil（!= → IS NOT NULL）
		Where("orders.updated", ">", NewExpression("orders.created")). // value-Expression
		WhereNull("orders.remark").                                    // null
		WhereNotNull("orders.bio").                                    // null(Not)
		WhereIn("orders.tag", []any{"a", "b"}).                        // in
		WhereNotIn("orders.tag2", []any{}).                            // in(空)+Not
		WhereIn("orders.dept", sub).                                   // inSub
		Where("orders.amount", ">", sub).                              // subValue
		WhereExists(func(q *Builder) {                                 // exists
			q.Table("payments").SelectRaw("1").WhereColumn("payments.order_id", "=", "orders.id")
		}).
		WhereNested(func(q *JoinBuilder) { // nested
			q.Where("orders.vip", "=", 1).OrWhere("orders.amount", ">", 1000)
		})
}

// joinAllConditionsArgs 是 joinAllConditions 按编译顺序产生的全部参数：
// Where status（"paid"）、WhereIn tag（"a"、"b"）、WhereNested（1、1000）。
// 其余条件为 IS NULL / Expression / 空 IN 列表 / 子查询 / WhereColumn，均不产生参数。
var joinAllConditionsArgs = []any{"paid", "a", "b", 1, 1000}

// TestPgGrammar_JoinConditionsAllTypes 验证 PG 方言 JoinBuilder 各条件类型的编译：
// SQL 做关键形态抽查，args 做全量类型敏感断言，并验证 $N 编号与 args 索引一一对应。
func TestPgGrammar_JoinConditionsAllTypes(t *testing.T) {
	g := NewPostgresGrammar()
	sub := NewBuilder(g, nil).Table("depts").Select("id")
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		joinAllConditions(jb, sub)
	})
	sql, args, err := b.ToSelect()
	assertNoError(t, err)
	if sql == "" {
		t.Fatal("expected non-empty SQL")
	}
	// 关键形态抽查
	for _, want := range []string{
		`"orders"."status" = $1`,
		`"orders"."deleted_at" IS NULL`,
		`"orders"."deleted_at2" IS NOT NULL`,
		`"orders"."remark" IS NULL`,
		`"orders"."bio" IS NOT NULL`,
		`"orders"."tag" IN ($`,
		`1 = 1`, // WhereNotIn 空列表
		`"orders"."dept" IN (SELECT "id" FROM "depts")`,
		`"orders"."amount" > (SELECT "id" FROM "depts")`,
		`EXISTS (SELECT 1 FROM "payments"`,
	} {
		if !containsStr(sql, want) {
			t.Errorf("PG join SQL missing %q:\n  got: %s", want, sql)
		}
	}
	assertArgs(t, joinAllConditionsArgs, args)
	assertPgPlaceholderSequence(t, sql, len(args))
}

// TestMyGrammar_JoinConditionsAllTypes 验证 MySQL 方言 JoinBuilder 各条件类型的编译：
// SQL 做关键形态抽查，args 做全量类型敏感断言。
func TestMyGrammar_JoinConditionsAllTypes(t *testing.T) {
	g := NewMySQLGrammar()
	sub := NewBuilder(g, nil).Table("depts").Select("id")
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		joinAllConditions(jb, sub)
	})
	sql, args, err := b.ToSelect()
	assertNoError(t, err)
	for _, want := range []string{
		"`orders`.`status` = ?",
		"`orders`.`deleted_at` IS NULL",
		"`orders`.`deleted_at2` IS NOT NULL",
		"`orders`.`remark` IS NULL",
		"`orders`.`bio` IS NOT NULL",
		"`orders`.`tag` IN (?, ?)",
		"1 = 1",
		"`orders`.`dept` IN (SELECT `id` FROM `depts`)",
		"`orders`.`amount` > (SELECT `id` FROM `depts`)",
		"EXISTS (SELECT 1 FROM `payments`",
	} {
		if !containsStr(sql, want) {
			t.Errorf("MySQL join SQL missing %q:\n  got: %s", want, sql)
		}
	}
	assertArgs(t, joinAllConditionsArgs, args)
}

// TestSQLiteGrammar_JoinConditionsAllTypes 验证 SQLite 方言 JoinBuilder 各条件类型的编译：
// SQL 做关键形态抽查，args 做全量类型敏感断言。
func TestSQLiteGrammar_JoinConditionsAllTypes(t *testing.T) {
	g := NewSQLiteGrammar()
	sub := NewBuilder(g, nil).Table("depts").Select("id")
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		joinAllConditions(jb, sub)
	})
	sql, args, err := b.ToSelect()
	assertNoError(t, err)
	for _, want := range []string{
		`"orders"."status" = ?`,
		`"orders"."deleted_at" IS NULL`,
		`"orders"."deleted_at2" IS NOT NULL`,
		`"orders"."remark" IS NULL`,
		`"orders"."bio" IS NOT NULL`,
		`"orders"."tag" IN (?, ?)`,
		`1 = 1`,
		`"orders"."dept" IN (SELECT "id" FROM "depts")`,
		`"orders"."amount" > (SELECT "id" FROM "depts")`,
		`EXISTS (SELECT 1 FROM "payments"`,
	} {
		if !containsStr(sql, want) {
			t.Errorf("SQLite join SQL missing %q:\n  got: %s", want, sql)
		}
	}
	assertArgs(t, joinAllConditionsArgs, args)
}

// TestPgGrammar_HavingAllTypes 验证 PG 方言 HAVING 各类型（basic-nil/Expression/between/null/notNull/nested）的编译：
// SQL 做关键形态抽查，args 做全量类型敏感断言，并验证 $N 编号与 args 索引一一对应。
// 仅覆盖 PG 方言，MySQL/SQLite 的 HAVING 编译差异仅在标识符包裹与占位符形式。
func TestPgGrammar_HavingAllTypes(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("orders").Select("status").SelectRaw("COUNT(*) AS cnt").
		GroupBy("status").
		Having("cnt", ">", 100).                            // basic
		Having("remark", "=", nil).                         // basic-nil
		Having("note", "!=", nil).                          // basic-nil
		Having("total", ">", NewExpression("MAX(amount)")). // basic-Expression
		HavingBetween("total", 100, 500).                   // between
		HavingNotBetween("total", 0, 99).                   // between(Not)
		HavingNull("remark").                               // null
		HavingNotNull("note").                              // notNull
		HavingNested(func(q *Builder) {                     // nested
			q.Having("cnt", ">", 1).OrHaving("cnt", "<", 0)
		})
	sql, args, err := b.ToSelect()
	assertNoError(t, err)
	for _, want := range []string{
		`"cnt" > $1`,
		`"remark" IS NULL`,
		`"note" IS NOT NULL`,
		`"total" > MAX(amount)`,
		`"total" BETWEEN $`,
		`"total" NOT BETWEEN $`,
	} {
		if !containsStr(sql, want) {
			t.Errorf("PG having SQL missing %q:\n  got: %s", want, sql)
		}
	}
	// basic(100) + between(100,500) + notBetween(0,99) + nested(1,0)
	assertArgs(t, []any{100, 100, 500, 0, 99, 1, 0}, args)
	assertPgPlaceholderSequence(t, sql, len(args))
}

// TestPgGrammar_WhereBasicNilAndExpression 验证 PG 方言 compileWhereBasic 的 nil 与 Expression 分支。
func TestPgGrammar_WhereBasicNilAndExpression(t *testing.T) {
	g := NewPostgresGrammar()
	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "eq_nil",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("deleted_at", "=", nil)
			},
			expected: `SELECT * FROM "users" WHERE "deleted_at" IS NULL`,
		},
		{
			name: "neq_nil",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("deleted_at", "!=", nil)
			},
			expected: `SELECT * FROM "users" WHERE "deleted_at" IS NOT NULL`,
		},
		{
			name: "expression",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Where("id", "=", NewExpression("parent_id"))
			},
			expected: `SELECT * FROM "users" WHERE "id" = parent_id`,
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

// TestGrammar_JoinConditionsColumnAndRaw 验证 join 条件的 column/raw 类型在三方言下编译正确。
func TestGrammar_JoinConditionsColumnAndRaw(t *testing.T) {
	for _, tc := range []struct {
		name string
		g    Grammar
		want string
	}{
		{"pg", NewPostgresGrammar(), `"orders"."user_id" = "users"."id" AND orders.amount > $1`},
		{"mysql", NewMySQLGrammar(), "`orders`.`user_id` = `users`.`id` AND orders.amount > ?"},
		{"sqlite", NewSQLiteGrammar(), `"orders"."user_id" = "users"."id" AND orders.amount > ?`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(tc.g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
				jb.On("orders.user_id", "=", "users.id").
					Raw("orders.amount > ?", 100)
			})
			sql, args, err := b.ToSelect()
			assertNoError(t, err)
			if !containsStr(sql, tc.want) {
				t.Errorf("join column+raw SQL mismatch, want fragment %q:\n  got: %s", tc.want, sql)
			}
			assertArgs(t, []any{100}, args)
		})
	}
}

// TestGrammar_JoinConditionNotInVariants 覆盖 compileJoinConditions 的
// IN 空列表（0 = 1）、NOT IN 列表、NOT IN 子查询分支，并逐方言断言完整编译结果。
// 每个用例新建 Grammar 实例：PostgresGrammar 的 paramCount 会在同一实例上累加，
// 复用实例会让占位符编号随用例顺序漂移。
func TestGrammar_JoinConditionNotInVariants(t *testing.T) {
	newGrammars := map[string]func() Grammar{
		"mysql":    func() Grammar { return NewMySQLGrammar() },
		"postgres": func() Grammar { return NewPostgresGrammar() },
		"sqlite":   func() Grammar { return NewSQLiteGrammar() },
	}
	wants := map[string]struct {
		notInList string
		notInSub  string
	}{
		"mysql":    {"`a` NOT IN (?, ?)", "`a` NOT IN (SELECT `id` FROM `t`)"},
		"postgres": {`"a" NOT IN ($1, $2)`, `"a" NOT IN (SELECT "id" FROM "t")`},
		"sqlite":   {`"a" NOT IN (?, ?)`, `"a" NOT IN (SELECT "id" FROM "t")`},
	}
	for name, newGrammar := range newGrammars {
		t.Run(name, func(t *testing.T) {
			want := wants[name]
			cond := func(jc []JoinCondition) string {
				switch gg := newGrammar().(type) {
				case *MySQLGrammar:
					return gg.compileJoinConditions(jc)
				case *PostgresGrammar:
					return gg.compileJoinConditions(jc)
				case *SQLiteGrammar:
					return gg.compileJoinConditions(jc)
				}
				return ""
			}
			if s := cond([]JoinCondition{{Type: "in", First: "a", Values: []any{}}}); s != "0 = 1" {
				t.Errorf("IN 空列表应编译为 %q，实际 %q", "0 = 1", s)
			}
			sub := NewBuilder(newGrammar(), nil).Table("t").Select("id")
			if s := cond([]JoinCondition{{Type: "in", First: "a", Values: []any{1, 2}, Not: true}}); s != want.notInList {
				t.Errorf("NOT IN 列表编译结果不符:\n got:  %s\n want: %s", s, want.notInList)
			}
			if s := cond([]JoinCondition{{Type: "inSub", First: "a", Sub: sub, Not: true}}); s != want.notInSub {
				t.Errorf("NOT IN 子查询编译结果不符:\n got:  %s\n want: %s", s, want.notInSub)
			}
		})
	}
}
