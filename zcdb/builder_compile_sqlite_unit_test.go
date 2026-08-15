// 本文件为 SQLite 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// TestBug_UpdateJoin_SQLite_DropsValueCondition 验证 SQLite UPDATE + JOIN 编译时
// value 类型条件被静默丢弃。
func TestBug_UpdateJoin_SQLite_DropsValueCondition(t *testing.T) {
	g := NewSQLiteGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := NewBuilder(g, nil).
		Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1)

	sql, args, err := b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)

	// 正确 SQL 应在 WHERE 中包含 "profiles"."active" = ?
	expectedSQL := `UPDATE "users" SET "name" = ? FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = ? AND "users"."id" = ?`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{"x", 99, 1}, args)
}

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

// TestSQLiteCompile_InsertUsingColumnMismatch 验证 SQLite 方言 InsertUsing 列数校验
// （详见 assertInsertUsingColumnMismatch：不一致报 ErrInsertUsingColumnMismatch，
// 一致/通配符/默认 SELECT * 时通过编译，后两者由数据库运行时校验）。
func TestSQLiteCompile_InsertUsingColumnMismatch(t *testing.T) {
	assertInsertUsingColumnMismatch(t, NewSQLiteGrammar())
}
