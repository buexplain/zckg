// 本文件为 PostgreSQL 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"sync"
	"testing"
)

// TestBug_PgParamCountRace 验证 PostgresGrammar 并发编译时参数计数器不互相干扰。
func TestBug_PgParamCountRace(t *testing.T) {
	g := NewPostgresGrammar()
	const n = 50
	results := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			b := newTestBuilder(g, nil).Table("users").Where("id", "=", 1)
			sql, _, _ := b.ToSelect()
			results[idx] = sql
		}(i)
	}
	wg.Wait()

	expected := `SELECT * FROM "users" WHERE "id" = $1`
	for i, sql := range results {
		if stripTestComment(sql) != expected {
			t.Errorf("并发编译结果[%d] 不正确:\n  expected: %s\n  got:      %s", i, expected, sql)
			return
		}
	}
}

// TestPostgresCompile_InsertUsingColumnMismatch 验证 PostgreSQL 方言 InsertUsing 列数校验
// （详见 assertInsertUsingColumnMismatch：不一致报 ErrInsertUsingColumnMismatch，
// 一致/通配符/默认 SELECT * 时通过编译，后两者由数据库运行时校验）。
func TestPostgresCompile_InsertUsingColumnMismatch(t *testing.T) {
	assertInsertUsingColumnMismatch(t, NewPostgresGrammar())
}

// TestPostgresCompile_SQLComment 验证全部公开编译入口、包装分支、$N 参数顺序和 Grammar 不追加注释。
func TestPostgresCompile_SQLComment(t *testing.T) {
	assertCommentCompileCases(t, NewPostgresGrammar(), map[string]commentCompileExpected{
		"select_group":        {`SELECT "name" FROM "users" WHERE "id" = $1 GROUP BY "name" HAVING COUNT(*) > $2 ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7, 2}},
		"select_distinct":     {`SELECT DISTINCT "name" FROM "users" WHERE "id" = $1 ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7}},
		"select":              {`SELECT "name" FROM "users" WHERE "id" = $1 ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7}},
		"select_lock":         {`SELECT "name" FROM "users" WHERE "id" = $1 ORDER BY "name" ASC LIMIT 5 OFFSET 2 FOR UPDATE`, []any{7}},
		"select_shared_lock":  {`SELECT "name" FROM "users" WHERE "id" = $1 ORDER BY "name" ASC LIMIT 5 OFFSET 2 FOR SHARE`, []any{7}},
		"select_union":        {`(SELECT "name" FROM "users" WHERE "id" = $1) UNION (SELECT "name" FROM "admins" WHERE "rank" > $2) ORDER BY "name" ASC LIMIT 5 OFFSET 2`, []any{7, 9}},
		"insert":              {`INSERT INTO "users" ("name", "age") VALUES ($1, $2)`, []any{"alice", 23}},
		"insert_ignore":       {`INSERT INTO "users" ("name", "age") VALUES ($1, $2) ON CONFLICT DO NOTHING`, []any{"alice", 23}},
		"upsert":              {`INSERT INTO "users" ("name", "age") VALUES ($1, $2) ON CONFLICT ("name") DO UPDATE SET "age" = EXCLUDED."age"`, []any{"alice", 23}},
		"upsert_no_update":    {`INSERT INTO "users" ("name") VALUES ($1) ON CONFLICT ("name") DO NOTHING`, []any{"alice"}},
		"insert_using":        {`INSERT INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > $1`, []any{11}},
		"insert_ignore_using": {`INSERT INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > $1 ON CONFLICT DO NOTHING`, []any{11}},
		"update":              {`UPDATE "users" SET "name" = $1, "age" = $2 FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = $3 AND "id" = $4`, []any{"alice", 23, 99, 7}},
		"delete":              {`DELETE FROM "users" WHERE "id" = $1`, []any{7}},
		"delete_join":         {`DELETE FROM "users" USING "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = $1 AND "id" = $2`, []any{99, 7}},
		"truncate":            {`TRUNCATE TABLE "users" RESTART IDENTITY`, nil},
		"count":               {`SELECT COUNT(*) FROM "users" WHERE "id" = $1`, []any{7}},
		"count_union":         {`SELECT COUNT(*) FROM ((SELECT "name" FROM "users" WHERE "id" = $1) UNION (SELECT "name" FROM "admins" WHERE "rank" > $2)) AS "t"`, []any{7, 9}},
		"count_group":         {`SELECT COUNT(*) FROM (SELECT 1 FROM "users" WHERE "id" = $1 GROUP BY "name" HAVING COUNT(*) > $2) AS "t"`, []any{7, 2}},
		"count_distinct":      {`SELECT COUNT(*) FROM (SELECT DISTINCT "name" FROM "users" WHERE "id" = $1) AS "t"`, []any{7}},
		"exists":              {`SELECT 1 FROM "users" WHERE "id" = $1 LIMIT 1`, []any{7}},
		"exists_union":        {`SELECT 1 FROM ((SELECT 1 FROM "users" WHERE "id" = $1) UNION (SELECT "name" FROM "admins" WHERE "rank" > $2)) AS "t" LIMIT 1`, []any{7, 9}},
		"aggregate":           {`SELECT SUM("age") AS "aggregate" FROM "users" WHERE "id" = $1`, []any{7}},
		"aggregate_union":     {`SELECT MAX("name") AS "aggregate" FROM ((SELECT "name" FROM "users" WHERE "id" = $1) UNION (SELECT "name" FROM "admins" WHERE "rank" > $2)) AS "t"`, []any{7, 9}},
		"increment":           {`UPDATE "users" SET "wallet" = "wallet" + $1, "level" = "level" + $2 FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = $3 AND "id" = $4`, []any{100, 2, 99, 7}},
		"decrement":           {`UPDATE "users" SET "wallet" = "wallet" - $1, "level" = "level" - $2 FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = $3 AND "id" = $4`, []any{50, 1, 99, 7}},
	})
}

// TestPostgresCompile_SQLCommentErrors 验证编译错误空结果及 PostgreSQL UNION 锁、Upsert 冲突目标限制不被注释改变。
func TestPostgresCompile_SQLCommentErrors(t *testing.T) {
	assertCommentCompileErrors(t, NewPostgresGrammar())
}

// TestPostgresCompile_SQLCommentSubqueries 验证各结构化子查询无内嵌注释、全局占位符编号及子状态保留。
func TestPostgresCompile_SQLCommentSubqueries(t *testing.T) {
	assertCommentSubqueries(t, NewPostgresGrammar(), "postgres", map[string]commentCompileExpected{
		"child":                {`SELECT "name" FROM "source" WHERE "score" > $1`, []any{11}},
		"select":               {`SELECT "name", (SELECT "name" FROM "source" WHERE "score" > $1) AS "picked" FROM "users" WHERE "id" = $2`, []any{11, 7}},
		"from":                 {`SELECT "name" FROM (SELECT "name" FROM "source" WHERE "score" > $1) AS "s" WHERE "id" = $2`, []any{11, 7}},
		"where_scalar":         {`SELECT "name" FROM "users" WHERE "id" = $1 AND "name" = (SELECT "name" FROM "source" WHERE "score" > $2)`, []any{7, 11}},
		"where_in":             {`SELECT "name" FROM "users" WHERE "id" = $1 AND "name" IN (SELECT "name" FROM "source" WHERE "score" > $2)`, []any{7, 11}},
		"where_exists":         {`SELECT "name" FROM "users" WHERE "id" = $1 AND EXISTS (SELECT "name" FROM "source" WHERE "score" > $2)`, []any{7, 11}},
		"where_exists_builder": {`SELECT "name" FROM "users" WHERE "id" = $1 AND EXISTS (SELECT "name" FROM "source" WHERE "score" > $2)`, []any{7, 11}},
		"join_table":           {`SELECT "name" FROM "users" INNER JOIN (SELECT "name" FROM "source" WHERE "score" > $1) AS "s" ON "users"."name" = "s"."name" AND "s"."name" <> $2 WHERE "id" = $3`, []any{11, "blocked", 7}},
		"join_scalar":          {`SELECT "name" FROM "users" INNER JOIN "profiles" ON "profiles"."name" = (SELECT "name" FROM "source" WHERE "score" > $1) WHERE "id" = $2`, []any{11, 7}},
		"join_in":              {`SELECT "name" FROM "users" INNER JOIN "profiles" ON "profiles"."name" IN (SELECT "name" FROM "source" WHERE "score" > $1) WHERE "id" = $2`, []any{11, 7}},
		"join_exists":          {`SELECT "name" FROM "users" INNER JOIN "profiles" ON EXISTS (SELECT "name" FROM "source" WHERE "score" > $1) WHERE "id" = $2`, []any{11, 7}},
		"union":                {`(SELECT "name" FROM "users" WHERE "id" = $1) UNION (SELECT "name" FROM "source" WHERE "score" > $2)`, []any{7, 11}},
		"insert_using":         {`INSERT INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > $1`, []any{11}},
		"insert_ignore_using":  {`INSERT INTO "users" ("name") SELECT "name" FROM "source" WHERE "score" > $1 ON CONFLICT DO NOTHING`, []any{11}},
		"select_from_where":    {`SELECT "name", (SELECT "name" FROM "source" WHERE "score" > $1) AS "picked" FROM (SELECT "name" FROM "source" WHERE "score" > $2) AS "s" WHERE "id" = $3 AND "name" IN (SELECT "name" FROM "source" WHERE "score" > $4)`, []any{11, 11, 7, 11}},
	})
}
