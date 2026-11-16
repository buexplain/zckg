// 本文件为 PostgreSQL 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"context"
	"errors"
	"strings"
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
		"count_group":         {`SELECT COUNT(*) FROM (SELECT "name" FROM "users" WHERE "id" = $1 GROUP BY "name" HAVING COUNT(*) > $2) AS "t"`, []any{7, 2}},
		"count_distinct":      {`SELECT COUNT(*) FROM (SELECT DISTINCT "name" FROM "users" WHERE "id" = $1) AS "t"`, []any{7}},
		"exists":              {`SELECT 1 FROM "users" WHERE "id" = $1 LIMIT 1`, []any{7}},
		"exists_union":        {`SELECT 1 FROM ((SELECT "name" FROM "users" WHERE "id" = $1) UNION (SELECT "name" FROM "admins" WHERE "rank" > $2)) AS "t" LIMIT 1`, []any{7, 9}},
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

// TestPostgresGrammar_WriteOrderLimitRejected 验证 PostgreSQL 写语句不支持 ORDER BY/LIMIT：
// 引擎语法本身没有该子句（UPDATE/DELETE ... ORDER BY 报 42601），若静默忽略会把写范围
// 放大到全部命中行，因此编译期统一返回 ErrWriteOrderLimitUnsupported。
func TestPostgresGrammar_WriteOrderLimitRejected(t *testing.T) {
	g := NewPostgresGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	cases := []struct {
		name    string
		compile func() error
	}{
		{"update", func() error {
			_, _, err := newTestBuilder(g, nil).Table("users").Where("id", ">", 0).
				OrderBy("id").Limit(1).ToUpdate(updateData{Name: "x"})
			return err
		}},
		{"delete", func() error {
			_, _, err := newTestBuilder(g, nil).Table("users").Where("id", ">", 0).
				OrderBy("id").Limit(1).ToDelete()
			return err
		}},
		{"delete_join", func() error {
			_, _, err := newTestBuilder(g, nil).Table("users").
				Join("orders", "orders.user_id", "=", "users.id").
				Where("users.id", ">", 0).Limit(1).ToDeleteJoin()
			return err
		}},
		{"increment", func() error {
			_, _, err := newTestBuilder(g, nil).Table("users").Where("id", ">", 0).
				OrderBy("id").Limit(1).ToIncrement([]string{"wallet"}, []any{1})
			return err
		}},
		{"decrement", func() error {
			_, _, err := newTestBuilder(g, nil).Table("users").Where("id", ">", 0).
				OrderBy("id").Limit(1).ToDecrement([]string{"wallet"}, []any{1})
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.compile()
			if !errors.Is(err, ErrWriteOrderLimitUnsupported) {
				t.Fatalf("应返回 ErrWriteOrderLimitUnsupported，got %v", err)
			}
			if !strings.Contains(err.Error(), "postgres") {
				t.Errorf("错误消息应指出方言: %v", err)
			}
		})
	}

	// 不携带 ORDER BY/LIMIT 的写语句不受影响。
	t.Run("without_order_limit_allowed", func(t *testing.T) {
		sqlStr, _, err := newTestBuilder(g, nil).Table("users").Where("id", ">", 0).
			ToUpdate(updateData{Name: "x"})
		assertNoError(t, err)
		assertSQL(t, `UPDATE "users" SET "name" = $1 WHERE "id" > $2`, sqlStr)
	})
}

// TestPostgresGrammar_InsertGetIdUnsupported 验证 PG 方言的 InsertGetId 在执行前返回哨兵
// ErrInsertGetIdUnsupported：错误消息保留原有 "not supported on postgres" 文案，
// 且不依赖 DAO（纯编译 Builder 即可复现）。
func TestPostgresGrammar_InsertGetIdUnsupported(t *testing.T) {
	type insertData struct {
		Name string `db:"name"`
	}
	_, err := newTestBuilder(NewPostgresGrammar(), nil).Table("users").
		InsertGetId(context.Background(), insertData{Name: "alice"})
	if !errors.Is(err, ErrInsertGetIdUnsupported) {
		t.Fatalf("应返回 ErrInsertGetIdUnsupported，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "not supported on postgres") {
		t.Errorf("错误消息应保留原文案: %v", err)
	}
}

// TestPostgresGrammar_ExpressionPlaceholderBoundary 锁死 reference/builder-compile.md「占位符边界」矩阵在 PostgreSQL 的形态：
// WHERE/HAVING/IN/LIKE/NULL 安全比较与 Raw 绑定中的 `?` 原样保留（成为非法占位符）；
// UPDATE SET 与 JOIN ON 值比较会把 `?` 转成 `$N` 并占用编号（Expression 本身仍不新增绑定参数，
// 因此 $N 没有配对参数——这正是文档要求「不要在 Expression 中使用 ?」的原因）。
func TestPostgresGrammar_ExpressionPlaceholderBoundary(t *testing.T) {
	expr := NewExpression("a ? b")
	users := func(g Grammar) *Builder { return newTestBuilder(g, nil).Table("users") }
	orders := func(g Grammar) *Builder { return newTestBuilder(g, nil).Table("orders") }
	cases := []exprPlaceholderCase{
		{"where", func(g Grammar) *Builder {
			return users(g).Where("id", "=", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "users" WHERE "id" = a ? b`, nil},
		{"where_in", func(g Grammar) *Builder {
			return users(g).WhereIn("id", []any{expr})
		}, (*Builder).ToSelect, `SELECT * FROM "users" WHERE "id" IN (a ? b)`, nil},
		{"where_like", func(g Grammar) *Builder {
			return users(g).WhereLike("name", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "users" WHERE "name" ILIKE a ? b`, nil},
		{"null_safe", func(g Grammar) *Builder {
			return users(g).WhereNullSafeEquals("name", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "users" WHERE "name" IS NOT DISTINCT FROM a ? b`, nil},
		{"having", func(g Grammar) *Builder {
			return orders(g).GroupBy("uid").Having("cnt", ">", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "orders" GROUP BY "uid" HAVING "cnt" > a ? b`, nil},
		{"having_raw", func(g Grammar) *Builder {
			return orders(g).GroupBy("uid").HavingRaw("SUM(amount) > ?", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "orders" GROUP BY "uid" HAVING SUM(amount) > a ? b`, nil},
		{"group_by_raw", func(g Grammar) *Builder {
			return orders(g).GroupByRaw("DATE(?)", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "orders" GROUP BY DATE(a ? b)`, nil},
		{"join_on_value", func(g Grammar) *Builder {
			return users(g).JoinOn("orders", func(j *JoinBuilder) {
				j.On("orders.uid", "=", "users.id").Where("orders.amt", ">", expr)
			})
		}, (*Builder).ToSelect,
			`SELECT * FROM "users" INNER JOIN "orders" ON "orders"."uid" = "users"."id" AND "orders"."amt" > a $1 b`, nil},
		{"join_raw", func(g Grammar) *Builder {
			return users(g).JoinOn("orders", func(j *JoinBuilder) {
				j.On("orders.uid", "=", "users.id").Raw("YEAR(orders.created_at) = ?", expr)
			})
		}, (*Builder).ToSelect,
			`SELECT * FROM "users" INNER JOIN "orders" ON "orders"."uid" = "users"."id" AND YEAR(orders.created_at) = a ? b`, nil},
		{"where_raw", func(g Grammar) *Builder {
			return users(g).WhereRaw("x = ?", expr)
		}, (*Builder).ToSelect, `SELECT * FROM "users" WHERE x = a ? b`, nil},
		{"update_set", func(g Grammar) *Builder {
			return users(g).Where("id", "=", 1)
		}, func(b *Builder) (string, []any, error) {
			return b.ToUpdate(expressionPlaceholderRow{V: expr})
		}, `UPDATE "users" SET "v" = a $1 b WHERE "id" = $2`, []any{1}},
		{"insert_values", func(g Grammar) *Builder {
			return users(g)
		}, func(b *Builder) (string, []any, error) {
			return b.ToInsert(expressionPlaceholderInsert{Name: "x", V: expr})
		}, `INSERT INTO "users" ("name", "v") VALUES ($1, a ? b)`, []any{"x"}},
	}
	runExpressionPlaceholderCases(t, NewPostgresGrammar(), cases)
}
