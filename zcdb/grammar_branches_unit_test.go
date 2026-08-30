// 本文件为三方言 Grammar 编译的补充单元测试。
// 覆盖 compileJoinConditions 的 value-nil/Expression、null(Not)、in(空列表)、inSub、
// subValue、exists、nested 分支，compileHavings 的 between/null/notNull/nested 分支，
// 以及 PostgreSQL compileWhereBasic 的 nil/Expression 分支。
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

// TestPgGrammar_JoinConditionsAllTypes 验证 PG 方言 JoinBuilder 全部条件类型的编译。
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
	_ = args
}

// TestMyGrammar_JoinConditionsAllTypes 验证 MySQL 方言 JoinBuilder 全部条件类型的编译。
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
	_ = args
}

// TestSQLiteGrammar_JoinConditionsAllTypes 验证 SQLite 方言 JoinBuilder 全部条件类型的编译。
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
	_ = args
}

// TestPgGrammar_HavingAllTypes 验证 PG 方言 HAVING 全部类型（basic-nil/Expression/between/null/notNull/nested）。
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
	_ = args
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
