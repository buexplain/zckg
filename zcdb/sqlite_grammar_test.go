package zcdb

import (
	"testing"
)

// ==================== SQLite SELECT 测试 ====================

// TestSQLiteGrammar_SelectBasic 验证基本 SELECT 语句：指定列名被双引号包裹，FROM 表名正确。
func TestSQLiteGrammar_SelectBasic(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users"`, sql)
	assertArgs(t, []any{}, args)
}

// TestSQLiteGrammar_SelectAll 验证 SELECT *：通配符不被双引号包裹。
func TestSQLiteGrammar_SelectAll(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users"`, sql)
	assertArgs(t, []any{}, args)
}

// TestSQLiteGrammar_SelectWithWhere 验证多条件 AND WHERE：多个 Where 调用生成 AND 连接，占位符使用 ? 格式。
func TestSQLiteGrammar_SelectWithWhere(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("age", ">", 18).
		Where("status", "=", "active").
		Select("id", "name").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name" FROM "users" WHERE "age" > ? AND "status" = ?`, sql)
	assertArgs(t, []any{18, "active"}, args)
}

// TestSQLiteGrammar_SelectOrWhere 验证 OR WHERE：OrWhere 调用生成 OR 连接的条件。
func TestSQLiteGrammar_SelectOrWhere(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("age", ">", 18).
		OrWhere("role", "=", "admin").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" > ? OR "role" = ?`, sql)
	assertArgs(t, []any{18, "admin"}, args)
}

// TestSQLiteGrammar_SelectWhereIn 验证 WHERE IN 子句：值列表正确展开为 ? 占位符。
func TestSQLiteGrammar_SelectWhereIn(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereIn("id", []any{1, 2, 3}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" IN (?, ?, ?)`, sql)
	assertArgs(t, []any{1, 2, 3}, args)
}

// TestSQLiteGrammar_SelectWhereNull 验证 WHERE IS NULL：生成 IS NULL 判断而非占位符。
func TestSQLiteGrammar_SelectWhereNull(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		WhereNull("deleted_at").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "deleted_at" IS NULL`, sql)
}

// TestSQLiteGrammar_SelectWhereBetween 验证 WHERE BETWEEN：两个边界值生成 ? AND ? 占位符。
func TestSQLiteGrammar_SelectWhereBetween(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereBetween("age", 18, 30).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" BETWEEN ? AND ?`, sql)
	assertArgs(t, []any{18, 30}, args)
}

// TestSQLiteGrammar_SelectWhereNested 验证嵌套 WHERE：WhereNested 内的条件被括号包裹。
func TestSQLiteGrammar_SelectWhereNested(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "active").
		WhereNested(func(b *Builder) {
			b.Where("age", ">", 18).OrWhere("role", "=", "admin")
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "status" = ? AND ("age" > ? OR "role" = ?)`, sql)
	assertArgs(t, []any{"active", 18, "admin"}, args)
}

// TestSQLiteGrammar_SelectWhereLike 验证 LIKE 模糊匹配：生成 WHERE column LIKE ? 语法。
func TestSQLiteGrammar_SelectWhereLike(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereLike("name", "%alice%").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "name" LIKE ?`, sql)
	assertArgs(t, []any{"%alice%"}, args)
}

// TestSQLiteGrammar_SelectJoin 验证 INNER JOIN：生成正确的 JOIN ON 语法，列名带表前缀双引号包裹。
func TestSQLiteGrammar_SelectJoin(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.total", ">", 100).
		Select("users.name", "orders.total").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "users"."name", "orders"."total" FROM "users" INNER JOIN "orders" ON "users"."id" = "orders"."user_id" WHERE "orders"."total" > ?`, sql)
	assertArgs(t, []any{100}, args)
}

// TestSQLiteGrammar_SelectJoinOn 验证 LEFT JOIN ON 多条件：使用 OrOn 生成 OR 连接的 ON 条件。
func TestSQLiteGrammar_SelectJoinOn(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("users.*").
		LeftJoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				OrOn("profiles.active", "=", "users.active")
		}).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "users".* FROM "users" LEFT JOIN "profiles" ON "users"."id" = "profiles"."user_id" OR "profiles"."active" = "users"."active"`, sql)
}

// TestSQLiteGrammar_SelectGroupByHaving 验证 GROUP BY + HAVING：分组后过滤条件正确生成。
func TestSQLiteGrammar_SelectGroupByHaving(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("orders").
		Select("user_id", "COUNT(*) as cnt").
		GroupBy("user_id").
		Having("cnt", ">", 5).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "user_id", COUNT(*) as cnt FROM "orders" GROUP BY "user_id" HAVING "cnt" > ?`, sql)
	assertArgs(t, []any{5}, args)
}

// TestSQLiteGrammar_SelectHavingBetween 验证 HAVING BETWEEN：分组后使用 BETWEEN 过滤。
func TestSQLiteGrammar_SelectHavingBetween(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("orders").
		Select("user_id", "SUM(amount) as total").
		GroupBy("user_id").
		HavingBetween("total", 100, 500).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "user_id", SUM(amount) as total FROM "orders" GROUP BY "user_id" HAVING "total" BETWEEN ? AND ?`, sql)
	assertArgs(t, []any{100, 500}, args)
}

// TestSQLiteGrammar_SelectOrderByLimitOffset 验证 ORDER BY + LIMIT + OFFSET：排序、分页子句完整生成。
func TestSQLiteGrammar_SelectOrderByLimitOffset(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "active").
		OrderBy("name", "ASC").
		OrderByDesc("created_at").
		Limit(10).
		Offset(20).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "status" = ? ORDER BY "name" ASC, "created_at" DESC LIMIT 10 OFFSET 20`, sql)
	assertArgs(t, []any{"active"}, args)
}

// TestSQLiteGrammar_ForPage 验证分页：第 N 页正确计算 OFFSET = (page-1)*perPage。
func TestSQLiteGrammar_ForPage(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("*").
		ForPage(3, 15).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" LIMIT 15 OFFSET 30`, sql)
}

// TestSQLiteGrammar_InRandomOrder 验证随机排序：SQLite 使用 ORDER BY RANDOM() 实现随机排序。
func TestSQLiteGrammar_InRandomOrder(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("*").
		InRandomOrder().
		Limit(5).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" ORDER BY RANDOM() LIMIT 5`, sql)
}

// TestSQLiteGrammar_SelectDistinct 验证 DISTINCT：SELECT 后正确插入 DISTINCT 关键字。
func TestSQLiteGrammar_SelectDistinct(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Distinct().
		Select("name", "age").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT DISTINCT "name", "age" FROM "users"`, sql)
}

// TestSQLiteGrammar_LockIgnored 验证 SQLite 锁子句被忽略：SQLite 不支持行锁，LockForUpdate 不会出现在生成的 SQL 中。
func TestSQLiteGrammar_LockIgnored(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		Select("*").
		LockForUpdate().
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" = ?`, sql)
}

// ==================== SQLite 子查询测试 ====================

// TestSQLiteGrammar_SelectSubquery 验证子查询列：作为 SELECT 子句的子查询用括号包裹并带 AS 别名。
func TestSQLiteGrammar_SelectSubquery(t *testing.T) {
	g := NewSQLiteGrammar()
	subQ := NewBuilder(g).Table("orders").Select("COUNT(*)").WhereColumn("orders.user_id", "=", "users.id")

	sql, _, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		SelectSubquery(subQ, "order_count").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", (SELECT COUNT(*) FROM "orders" WHERE "orders"."user_id" = "users"."id") AS "order_count" FROM "users"`, sql)
}

// TestSQLiteGrammar_FromSub 验证子查询作为数据源：FROM 子句使用 (subquery) AS alias 语法。
func TestSQLiteGrammar_FromSub(t *testing.T) {
	g := NewSQLiteGrammar()
	subQ := NewBuilder(g).Table("users").Select("id", "name").Where("active", "=", true)

	sql, args, err := NewBuilder(g).
		FromSub(subQ, "u").
		Select("u.name").
		Where("u.id", ">", 100).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "u"."name" FROM (SELECT "id", "name" FROM "users" WHERE "active" = ?) AS "u" WHERE "u"."id" > ?`, sql)
	assertArgs(t, []any{true, 100}, args)
}

// TestSQLiteGrammar_WhereSub 验证 WHERE 子查询比较：子查询作为 WHERE 条件的右操作数。
func TestSQLiteGrammar_WhereSub(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("*").
		WhereSub("age", ">", func(b *Builder) {
			b.Table("stats").Select("AVG(age)").Where("country", "=", "US")
		}).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" > (SELECT AVG(age) FROM "stats" WHERE "country" = ?)`, sql)
	assertArgs(t, []any{"US"}, args)
}

// TestSQLiteGrammar_WhereInSub 验证 WHERE IN 子查询：IN 子句使用子查询而非值列表。
func TestSQLiteGrammar_WhereInSub(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("*").
		WhereInSub("id", func(b *Builder) {
			b.Table("orders").Select("user_id").Where("total", ">", 100)
		}).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" IN (SELECT "user_id" FROM "orders" WHERE "total" > ?)`, sql)
	assertArgs(t, []any{100}, args)
}

// ==================== SQLite UNION 测试 ====================

// TestSQLiteGrammar_Union 验证 UNION/UNION ALL：SQLite 中 UNION 各查询之间不使用括号包裹。
func TestSQLiteGrammar_Union(t *testing.T) {
	g := NewSQLiteGrammar()
	q1 := NewBuilder(g).Table("users").Select("id", "name").Where("age", ">", 18)
	q2 := NewBuilder(g).Table("admins").Select("id", "name").Where("role", "=", "root")

	sql, args, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		Where("status", "=", "active").
		Union(q1).
		UnionAll(q2).
		ToSelect()

	assertNoError(t, err)
	// SQLite 的 UNION 各查询之间不允许显式括号
	assertSQL(t, `SELECT "id", "name" FROM "users" WHERE "status" = ? UNION SELECT "id", "name" FROM "users" WHERE "age" > ? UNION ALL SELECT "id", "name" FROM "admins" WHERE "role" = ?`, sql)
	assertArgs(t, []any{"active", 18, "root"}, args)
}

// ==================== SQLite INSERT 测试 ====================

// TestSQLiteGrammar_Insert 验证单条结构体插入：字段映射正确，生成 INSERT INTO ... VALUES (?, ?, ?) 语法。
func TestSQLiteGrammar_Insert(t *testing.T) {
	g := NewSQLiteGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES (?, ?, ?)`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// TestSQLiteGrammar_InsertBatch 验证批量插入：切片生成多组 VALUES 占位符。
func TestSQLiteGrammar_InsertBatch(t *testing.T) {
	g := NewSQLiteGrammar()
	data := []userInsert{
		{Name: "alice", Age: 25, Email: "alice@test.com"},
		{Name: "bob", Age: 30, Email: "bob@test.com"},
	}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES (?, ?, ?), (?, ?, ?)`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com", "bob", 30, "bob@test.com"}, args)
}

// TestSQLiteGrammar_InsertOrIgnore 验证 INSERT OR IGNORE：SQLite 使用 INSERT OR IGNORE 语法，冲突时静默忽略。
func TestSQLiteGrammar_InsertOrIgnore(t *testing.T) {
	g := NewSQLiteGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsertOrIgnore(data)

	assertNoError(t, err)
	// SQLite 使用 INSERT OR IGNORE 语法
	assertSQL(t, `INSERT OR IGNORE INTO "users" ("name", "age", "email") VALUES (?, ?, ?)`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// TestSQLiteGrammar_Upsert 验证 Upsert：生成 INSERT ... ON CONFLICT (...) DO UPDATE SET 语法，使用 "excluded" 引用新值。
func TestSQLiteGrammar_Upsert(t *testing.T) {
	g := NewSQLiteGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToUpsert(data, []string{"email"}, []string{"name", "age"})

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES (?, ?, ?) ON CONFLICT ("email") DO UPDATE SET "name" = "excluded"."name", "age" = "excluded"."age"`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// TestSQLiteGrammar_InsertUsing 验证 INSERT ... SELECT：从子查询插入数据。
func TestSQLiteGrammar_InsertUsing(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users_archive").
		ToInsertUsing([]string{"name", "email"}, func(sub *Builder) {
			sub.Table("users").Select("name", "email").Where("active", "=", false)
		})

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users_archive" ("name", "email") SELECT "name", "email" FROM "users" WHERE "active" = ?`, sql)
	assertArgs(t, []any{false}, args)
}

// ==================== SQLite UPDATE 测试 ====================

// TestSQLiteGrammar_Update 验证基本 UPDATE：生成 UPDATE SET ... WHERE 语法。
func TestSQLiteGrammar_Update(t *testing.T) {
	g := NewSQLiteGrammar()
	data := userUpdate{Name: "bob", Age: 30}
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(data)

	assertNoError(t, err)
	assertSQL(t, `UPDATE "users" SET "name" = ?, "age" = ? WHERE "id" = ?`, sql)
	assertArgs(t, []any{"bob", 30, 1}, args)
}

// TestSQLiteGrammar_UpdateNoOrderLimit 验证 SQLite UPDATE 不生成 ORDER BY/LIMIT：设置了也不会出现在生成的 SQL 中。
func TestSQLiteGrammar_UpdateNoOrderLimit(t *testing.T) {
	g := NewSQLiteGrammar()
	data := userUpdate{Name: "bob"}
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "old").
		OrderBy("id", "ASC").
		Limit(10).
		ToUpdate(data)

	assertNoError(t, err)
	assertSQL(t, `UPDATE "users" SET "name" = ? WHERE "status" = ?`, sql)
	assertArgs(t, []any{"bob", "old"}, args)
}

// TestSQLiteGrammar_UpdateWithJoin 验证 SQLite 多表更新：使用 FROM 子句实现 JOIN 更新（SQLite 特有语法）。
func TestSQLiteGrammar_UpdateWithJoin(t *testing.T) {
	g := NewSQLiteGrammar()
	data := userUpdate{Status: "vip"}
	sql, args, err := NewBuilder(g).
		Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.total", ">", 1000).
		ToUpdate(data)

	assertNoError(t, err)
	assertSQL(t, `UPDATE "users" SET "status" = ? FROM "orders" WHERE "users"."id" = "orders"."user_id" AND "orders"."total" > ?`, sql)
	assertArgs(t, []any{"vip", 1000}, args)
}

// ==================== SQLite DELETE 测试 ====================

// TestSQLiteGrammar_Delete 验证基本 DELETE：生成 DELETE FROM ... WHERE 语法。
func TestSQLiteGrammar_Delete(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToDelete()

	assertNoError(t, err)
	assertSQL(t, `DELETE FROM "users" WHERE "id" = ?`, sql)
	assertArgs(t, []any{1}, args)
}

// TestSQLiteGrammar_DeleteNoOrderLimit 验证 SQLite DELETE 不生成 ORDER BY/LIMIT：设置了也不会出现在生成的 SQL 中。
func TestSQLiteGrammar_DeleteNoOrderLimit(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "inactive").
		OrderBy("id", "ASC").
		Limit(100).
		ToDelete()

	assertNoError(t, err)
	assertSQL(t, `DELETE FROM "users" WHERE "status" = ?`, sql)
}

// ==================== SQLite TRUNCATE 测试 ====================

// TestSQLiteGrammar_Truncate 验证 SQLite Truncate：SQLite 没有 TRUNCATE，转换为 DELETE FROM 语句。
func TestSQLiteGrammar_Truncate(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, err := NewBuilder(g).
		Table("users").
		ToTruncate()

	assertNoError(t, err)
	assertSQL(t, `DELETE FROM "users"`, sql)
}

// ==================== SQLite 前缀 / 别名 ====================

// TestSQLiteGrammar_TablePrefix 验证表前缀：设置 SetTablePrefix 后，表名自动拼接前缀。
func TestSQLiteGrammar_TablePrefix(t *testing.T) {
	g := NewSQLiteGrammar().SetTablePrefix("app_")
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name" FROM "app_users"`, sql)
}

// TestSQLiteGrammar_TableAlias 验证表别名：Table("users AS u") 生成 FROM "users" AS "u" 语法。
func TestSQLiteGrammar_TableAlias(t *testing.T) {
	g := NewSQLiteGrammar()
	sql, _, err := NewBuilder(g).
		Table("users AS u").
		Select("u.id", "u.name").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "u"."id", "u"."name" FROM "users" AS "u"`, sql)
}
