// 本文件为 SQLite 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
	"time"
)

// TestBug_UpdateJoin_SQLite_DropsValueCondition 验证 SQLite UPDATE + JOIN 编译时
// value 类型条件被静默丢弃。
func TestBug_UpdateJoin_SQLite_DropsValueCondition(t *testing.T) {
	g := NewSQLiteGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := newTestBuilder(g, nil).
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
			return newTestBuilder(g, nil).Table("users").Where("age", 25)
		}, `SELECT * FROM "users" WHERE "age" = ?`, []any{25}},
		{"WhereNilEq", func() *Builder {
			return newTestBuilder(g, nil).Table("users").Where("age", "=", nil)
		}, `SELECT * FROM "users" WHERE "age" IS NULL`, nil},
		{"WhereNilNe", func() *Builder {
			return newTestBuilder(g, nil).Table("users").Where("age", "<>", nil)
		}, `SELECT * FROM "users" WHERE "age" IS NOT NULL`, nil},
		{"WhereDate", func() *Builder {
			return newTestBuilder(g, nil).Table("events").WhereDate("happened_at", "2024-06-15")
		}, `SELECT * FROM "events" WHERE strftime('%Y-%m-%d', "happened_at") = ?`, []any{"2024-06-15"}},
		{"WhereDate_Time", func() *Builder {
			return newTestBuilder(g, nil).Table("events").WhereDate("happened_at", time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC))
		}, `SELECT * FROM "events" WHERE strftime('%Y-%m-%d', "happened_at") = ?`, []any{"2024-06-15"}},
		{"NullSafeEquals", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereNullSafeEquals("age", 25)
		}, `SELECT * FROM "users" WHERE "age" IS ?`, []any{25}},
		{"NullSafeNotEquals", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereNullSafeNotEquals("age", 25)
		}, `SELECT * FROM "users" WHERE "age" IS NOT ?`, []any{25}},
		{"LikeCaseSensitive", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereLike("name", "*li*", true)
		}, `SELECT * FROM "users" WHERE "name" GLOB ?`, []any{"*li*"}},
		{"LikeDefault", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereLike("name", "%li%")
		}, `SELECT * FROM "users" WHERE "name" LIKE ?`, []any{"%li%"}},
		{"WhereNullMulti", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereNull("age", "email")
		}, `SELECT * FROM "users" WHERE "age" IS NULL AND "email" IS NULL`, nil},
		{"GroupByRaw", func() *Builder {
			return newTestBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").GroupByRaw("user_id + ?", 0)
		}, `SELECT COUNT(*) FROM "orders" GROUP BY user_id + ?`, []any{0}},
		{"BetweenColumns", func() *Builder {
			return newTestBuilder(g, nil).Table("ranges").WhereBetweenColumns("val", "lo", "hi")
		}, `SELECT * FROM "ranges" WHERE "val" BETWEEN "lo" AND "hi"`, nil},
		{"ValueBetween", func() *Builder {
			return newTestBuilder(g, nil).Table("ranges").WhereValueBetween(5, "lo", "hi")
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

// TestSQLiteCompile_InsertOrIgnoreMultiRowAndExpression 覆盖 SQLite 方言
// CompileInsertOrIgnore 的多行分隔符与 Expression 内联分支。
func TestSQLiteCompile_InsertOrIgnoreMultiRowAndExpression(t *testing.T) {
	rows := [][]any{
		{int64(1), NewExpression("UPPER('a')")},
		{int64(2), "b"},
	}
	li := NewSQLiteGrammar()
	if sql := li.CompileInsertOrIgnore(newTestBuilder(li, nil).Table("t"), []string{"id", "name"}, rows); sql == "" {
		t.Fatal("SQLite CompileInsertOrIgnore 应产出 SQL")
	}
}

// TestSQLiteCompile_SQLComment 验证全部公开编译入口、包装分支、JOIN/SET 参数顺序和 Grammar 不追加注释。
func TestSQLiteCompile_SQLComment(t *testing.T) {
	assertCommentCompileCases(t, NewSQLiteGrammar(), map[string]commentCompileExpected{
		"select_group":        {`SELECT "name" FROM "users" WHERE "id" = ? GROUP BY "name" HAVING COUNT(*) > ? ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7, 2}},
		"select_distinct":     {`SELECT DISTINCT "name" FROM "users" WHERE "id" = ? ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7}},
		"select":              {`SELECT "name" FROM "users" WHERE "id" = ? ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7}},
		"select_union":        {`SELECT "name" FROM "users" WHERE "id" = ? UNION SELECT "name" FROM "admins" WHERE "rank" > ? ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7, 9}},
		"insert":              {`INSERT INTO "users" ("name", "age") VALUES (?, ?)`, []any{"alice", 23}},
		"insert_ignore":       {`INSERT OR IGNORE INTO "users" ("name", "age") VALUES (?, ?)`, []any{"alice", 23}},
		"upsert":              {`INSERT INTO "users" ("name", "age") VALUES (?, ?) ON CONFLICT ("name") DO UPDATE SET "age" = EXCLUDED."age"`, []any{"alice", 23}},
		"upsert_no_update":    {`INSERT INTO "users" ("name") VALUES (?) ON CONFLICT ("name") DO NOTHING`, []any{"alice"}},
		"insert_using":        {`INSERT INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > ?`, []any{11}},
		"insert_ignore_using": {`INSERT OR IGNORE INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > ?`, []any{11}},
		"update":              {`UPDATE "users" SET "name" = ?, "age" = ? FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = ? AND "id" = ?`, []any{"alice", 23, 99, 7}},
		"delete":              {`DELETE FROM "users" WHERE "id" = ?`, []any{7}},
		"delete_join":         {`DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "profiles" ON "users"."id" = "profiles"."user_id" AND "profiles"."active" = ? WHERE "id" = ?)`, []any{99, 7}},
		"truncate":            {`DELETE FROM "users"`, nil},
		"count":               {`SELECT COUNT(*) FROM "users" WHERE "id" = ?`, []any{7}},
		"count_union":         {`SELECT COUNT(*) FROM (SELECT "name" FROM "users" WHERE "id" = ? UNION SELECT "name" FROM "admins" WHERE "rank" > ?) AS "t"`, []any{7, 9}},
		"count_group":         {`SELECT COUNT(*) FROM (SELECT 1 FROM "users" WHERE "id" = ? GROUP BY "name" HAVING COUNT(*) > ?) AS "t"`, []any{7, 2}},
		"count_distinct":      {`SELECT COUNT(*) FROM (SELECT DISTINCT "name" FROM "users" WHERE "id" = ?) AS "t"`, []any{7}},
		"exists":              {`SELECT 1 FROM "users" WHERE "id" = ? LIMIT 1`, []any{7}},
		"exists_union":        {`SELECT 1 FROM (SELECT 1 FROM "users" WHERE "id" = ? UNION SELECT "name" FROM "admins" WHERE "rank" > ?) AS "t" LIMIT 1`, []any{7, 9}},
		"aggregate":           {`SELECT SUM("age") AS "aggregate" FROM "users" WHERE "id" = ?`, []any{7}},
		"aggregate_union":     {`SELECT MAX("name") AS "aggregate" FROM (SELECT "name" FROM "users" WHERE "id" = ? UNION SELECT "name" FROM "admins" WHERE "rank" > ?) AS "t"`, []any{7, 9}},
		"increment":           {`UPDATE "users" SET "wallet" = "wallet" + ?, "level" = "level" + ? FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = ? AND "id" = ?`, []any{100, 2, 99, 7}},
		"decrement":           {`UPDATE "users" SET "wallet" = "wallet" - ?, "level" = "level" - ? FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = ? AND "id" = ?`, []any{50, 1, 99, 7}},
	})
}

// TestSQLiteCompile_SQLCommentErrors 验证编译错误空结果及 SQLite 锁、Upsert 冲突目标限制不被注释改变。
func TestSQLiteCompile_SQLCommentErrors(t *testing.T) {
	assertCommentCompileErrors(t, NewSQLiteGrammar())
}

// TestSQLiteCompile_SQLCommentSubqueries 验证各结构化子查询无内嵌注释、外层清空及子查询独立编译保留状态。
func TestSQLiteCompile_SQLCommentSubqueries(t *testing.T) {
	assertCommentSubqueries(t, NewSQLiteGrammar(), "sqlite", map[string]commentCompileExpected{
		"child":                {`SELECT "name" FROM "source" WHERE "score" > ?`, []any{11}},
		"select":               {`SELECT "name", (SELECT "name" FROM "source" WHERE "score" > ?) AS "picked" FROM "users" WHERE "id" = ?`, []any{11, 7}},
		"from":                 {`SELECT "name" FROM (SELECT "name" FROM "source" WHERE "score" > ?) AS "s" WHERE "id" = ?`, []any{11, 7}},
		"where_scalar":         {`SELECT "name" FROM "users" WHERE "id" = ? AND "name" = (SELECT "name" FROM "source" WHERE "score" > ?)`, []any{7, 11}},
		"where_in":             {`SELECT "name" FROM "users" WHERE "id" = ? AND "name" IN (SELECT "name" FROM "source" WHERE "score" > ?)`, []any{7, 11}},
		"where_exists":         {`SELECT "name" FROM "users" WHERE "id" = ? AND EXISTS (SELECT "name" FROM "source" WHERE "score" > ?)`, []any{7, 11}},
		"where_exists_builder": {`SELECT "name" FROM "users" WHERE "id" = ? AND EXISTS (SELECT "name" FROM "source" WHERE "score" > ?)`, []any{7, 11}},
		"join_table":           {`SELECT "name" FROM "users" INNER JOIN (SELECT "name" FROM "source" WHERE "score" > ?) AS "s" ON "users"."name" = "s"."name" AND "s"."name" <> ? WHERE "id" = ?`, []any{11, "blocked", 7}},
		"join_scalar":          {`SELECT "name" FROM "users" INNER JOIN "profiles" ON "profiles"."name" = (SELECT "name" FROM "source" WHERE "score" > ?) WHERE "id" = ?`, []any{11, 7}},
		"join_in":              {`SELECT "name" FROM "users" INNER JOIN "profiles" ON "profiles"."name" IN (SELECT "name" FROM "source" WHERE "score" > ?) WHERE "id" = ?`, []any{11, 7}},
		"join_exists":          {`SELECT "name" FROM "users" INNER JOIN "profiles" ON EXISTS (SELECT "name" FROM "source" WHERE "score" > ?) WHERE "id" = ?`, []any{11, 7}},
		"union":                {`SELECT "name" FROM "users" WHERE "id" = ? UNION SELECT "name" FROM "source" WHERE "score" > ?`, []any{7, 11}},
		"insert_using":         {`INSERT INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > ?`, []any{11}},
		"insert_ignore_using":  {`INSERT OR IGNORE INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > ?`, []any{11}},
		"select_from_where":    {`SELECT "name", (SELECT "name" FROM "source" WHERE "score" > ?) AS "picked" FROM (SELECT "name" FROM "source" WHERE "score" > ?) AS "s" WHERE "id" = ? AND "name" IN (SELECT "name" FROM "source" WHERE "score" > ?)`, []any{11, 11, 7, 11}},
	})
}
