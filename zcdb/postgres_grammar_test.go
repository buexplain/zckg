package zcdb

import (
	"testing"
)

// ==================== PostgreSQL SELECT 测试 ====================

// TestPgGrammar_SelectBasic 验证基本 SELECT 语句：指定列名被双引号包裹，FROM 表名正确。
func TestPgGrammar_SelectBasic(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users"`, sql)
	assertArgs(t, []any{}, args)
}

// TestPgGrammar_SelectAll 验证 SELECT *：通配符不被双引号包裹。
func TestPgGrammar_SelectAll(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users"`, sql)
	assertArgs(t, []any{}, args)
}

// TestPgGrammar_SelectWithWhere 验证多条件 AND WHERE：多个 Where 调用生成 AND 连接，占位符使用 $N 格式。
func TestPgGrammar_SelectWithWhere(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("age", ">", 18).
		Where("status", "=", "active").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "age" > $1 AND "status" = $2`, sql)
	assertArgs(t, []any{18, "active"}, args)
}

// TestPgGrammar_SelectOrWhere 验证 OR WHERE：OrWhere 调用生成 OR 连接的条件。
func TestPgGrammar_SelectOrWhere(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("age", ">", 18).
		OrWhere("role", "=", "admin").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "age" > $1 OR "role" = $2`, sql)
	assertArgs(t, []any{18, "admin"}, args)
}

// TestPgGrammar_SelectWhereIn 验证 WHERE IN 子句：值列表展开为 $1, $2, $3 占位符。
func TestPgGrammar_SelectWhereIn(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereIn("id", []any{1, 2, 3}).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "id" IN ($1, $2, $3)`, sql)
	assertArgs(t, []any{1, 2, 3}, args)
}

// TestPgGrammar_SelectWhereNull 验证 WHERE IS NULL：生成 IS NULL 判断而非占位符。
func TestPgGrammar_SelectWhereNull(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereNull("deleted_at").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "deleted_at" IS NULL`, sql)
	assertArgs(t, []any{}, args)
}

// TestPgGrammar_SelectWhereBetween 验证 WHERE BETWEEN：两个边界值生成 $1 AND $2 占位符。
func TestPgGrammar_SelectWhereBetween(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereBetween("age", 18, 30).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "age" BETWEEN $1 AND $2`, sql)
	assertArgs(t, []any{18, 30}, args)
}

// TestPgGrammar_SelectWhereNested 验证嵌套 WHERE：WhereNested 内的条件被括号包裹。
func TestPgGrammar_SelectWhereNested(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "active").
		WhereNested(func(b *Builder) {
			b.Where("age", ">", 18).OrWhere("role", "=", "admin")
		}).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "status" = $1 AND ("age" > $2 OR "role" = $3)`, sql)
	assertArgs(t, []any{"active", 18, "admin"}, args)
}

// TestPgGrammar_SelectWhereRaw 验证 WhereRaw：原始 SQL 表达式不被转义或包裹，占位符由用户自行管理。
func TestPgGrammar_SelectWhereRaw(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereRaw("EXTRACT(YEAR FROM created_at) = $1", 2024).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE EXTRACT(YEAR FROM created_at) = $1`, sql)
	assertArgs(t, []any{2024}, args)
}

// TestPgGrammar_SelectJoin 验证 INNER JOIN：生成正确的 JOIN ON 语法，列名带表前缀双引号包裹。
func TestPgGrammar_SelectJoin(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.total", ">", 100).
		Select("users.name", "orders.total").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "users"."name", "orders"."total" FROM "users" INNER JOIN "orders" ON "users"."id" = "orders"."user_id" WHERE "orders"."total" > $1`, sql)
	assertArgs(t, []any{100}, args)
}

// TestPgGrammar_SelectLeftJoin 验证 LEFT JOIN：生成 LEFT JOIN 关键字和 ON 条件。
func TestPgGrammar_SelectLeftJoin(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		LeftJoin("profiles", "users.id", "=", "profiles.user_id").
		Select("users.*", "profiles.bio").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "users".*, "profiles"."bio" FROM "users" LEFT JOIN "profiles" ON "users"."id" = "profiles"."user_id"`, sql)
}

// TestPgGrammar_SelectGroupByHaving 验证 GROUP BY + HAVING：分组后过滤条件正确生成。
func TestPgGrammar_SelectGroupByHaving(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("orders").
		Select("user_id", "COUNT(*) as cnt").
		GroupBy("user_id").
		Having("cnt", ">", 5).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "user_id", COUNT(*) as cnt FROM "orders" GROUP BY "user_id" HAVING "cnt" > $1`, sql)
	assertArgs(t, []any{5}, args)
}

// TestPgGrammar_SelectOrderByLimitOffset 验证 ORDER BY + LIMIT + OFFSET：排序、分页子句完整生成。
func TestPgGrammar_SelectOrderByLimitOffset(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "active").
		OrderBy("name", "ASC").
		OrderByDesc("created_at").
		Limit(10).
		Offset(20).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "status" = $1 ORDER BY "name" ASC, "created_at" DESC LIMIT 10 OFFSET 20`, sql)
	assertArgs(t, []any{"active"}, args)
}

// TestPgGrammar_SelectDistinct 验证 DISTINCT：SELECT 后正确插入 DISTINCT 关键字。
func TestPgGrammar_SelectDistinct(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Distinct().
		Select("name", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT DISTINCT "name", "email" FROM "users"`, sql)
}

// TestPgGrammar_SelectUnion 验证 UNION：两个查询用 UNION 连接，各自用括号包裹。
func TestPgGrammar_SelectUnion(t *testing.T) {
	g := NewPostgresGrammar()
	q1 := NewBuilder(g).Table("users").Select("name", "email").Where("active", "=", true)
	q2 := NewBuilder(g).Table("admins").Select("name", "email").Where("level", ">", 1)

	sql, args, err := q1.Union(q2).ToSelect()

	assertNoError(t, err)
	assertSQL(t, `(SELECT "name", "email" FROM "users" WHERE "active" = $1) UNION (SELECT "name", "email" FROM "admins" WHERE "level" > $2)`, sql)
	assertArgs(t, []any{true, 1}, args)
}

// TestPgGrammar_SelectLockForUpdate 验证悲观锁 FOR UPDATE：SELECT 末尾追加 FOR UPDATE 子句。
func TestPgGrammar_SelectLockForUpdate(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		LockForUpdate().
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "id" = $1 FOR UPDATE`, sql)
}

// ==================== PostgreSQL INSERT 测试 ====================

// TestPgGrammar_InsertSingle 验证单条结构体插入：字段映射正确，占位符使用 $N 格式。
func TestPgGrammar_InsertSingle(t *testing.T) {
	g := NewPostgresGrammar()
	data := userInsert{Name: "Alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).Table("users").ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3)`, sql)
	assertArgs(t, []any{"Alice", 25, "alice@test.com"}, args)
}

// TestPgGrammar_InsertBatch 验证批量插入：切片生成多组 VALUES，占位符序号连续递增。
func TestPgGrammar_InsertBatch(t *testing.T) {
	g := NewPostgresGrammar()
	data := []userInsert{
		{Name: "Alice", Age: 25, Email: "alice@test.com"},
		{Name: "Bob", Age: 30, Email: "bob@test.com"},
	}
	sql, args, err := NewBuilder(g).Table("users").ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3), ($4, $5, $6)`, sql)
	assertArgs(t, []any{"Alice", 25, "alice@test.com", "Bob", 30, "bob@test.com"}, args)
}

// TestPgGrammar_InsertBatchPartial 验证批量插入部分字段：列基于第一行非 nil 字段确定，nil 字段被跳过。
func TestPgGrammar_InsertBatchPartial(t *testing.T) {
	// 批量插入时，列基于第一行非 nil 字段确定
	g := NewPostgresGrammar()
	data := []userInsert{
		{Name: "Alice", Age: 25, Email: nil},
		{Name: "Bob", Age: 30, Email: nil},
	}
	sql, args, err := NewBuilder(g).Table("users").ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age") VALUES ($1, $2), ($3, $4)`, sql)
	assertArgs(t, []any{"Alice", 25, "Bob", 30}, args)
}

// TestPgGrammar_InsertNilFields 验证 nil 字段跳过：仅插入非 nil 字段。
func TestPgGrammar_InsertNilFields(t *testing.T) {
	g := NewPostgresGrammar()
	data := userInsert{Name: "Alice", Age: nil, Email: nil}
	sql, args, err := NewBuilder(g).Table("users").ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name") VALUES ($1)`, sql)
	assertArgs(t, []any{"Alice"}, args)
}

// ==================== PostgreSQL UPDATE 测试 ====================

// TestPgGrammar_UpdateBasic 验证基本 UPDATE：生成 UPDATE SET ... WHERE 语法，nil 字段被跳过。
func TestPgGrammar_UpdateBasic(t *testing.T) {
	g := NewPostgresGrammar()
	data := userUpdate{Name: "Alice", Age: 26, Status: nil}
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(data)

	assertNoError(t, err)
	assertSQL(t, `UPDATE "users" SET "name" = $1, "age" = $2 WHERE "id" = $3`, sql)
	assertArgs(t, []any{"Alice", 26, 1}, args)
}

// TestPgGrammar_UpdateWithExpression 验证表达式更新：Raw 表达式直接内联到 SQL 中，不作为占位符。
func TestPgGrammar_UpdateWithExpression(t *testing.T) {
	g := NewPostgresGrammar()
	data := userUpdate{Age: Raw(`"age" + 1`), Status: "vip"}
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(data)

	assertNoError(t, err)
	assertSQL(t, `UPDATE "users" SET "age" = "age" + 1, "status" = $1 WHERE "id" = $2`, sql)
	assertArgs(t, []any{"vip", 1}, args)
}

// TestPgGrammar_UpdateNoOrderByLimit 验证 PostgreSQL UPDATE 不支持 ORDER BY/LIMIT：设置了也不会出现在生成的 SQL 中。
func TestPgGrammar_UpdateNoOrderByLimit(t *testing.T) {
	// PostgreSQL 的 UPDATE 不支持 ORDER BY 和 LIMIT，应该忽略
	g := NewPostgresGrammar()
	data := userUpdate{Status: "inactive"}
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "active").
		OrderBy("created_at", "ASC").
		Limit(10).
		ToUpdate(data)

	assertNoError(t, err)
	// PostgreSQL 编译结果不应包含 ORDER BY 和 LIMIT
	assertSQL(t, `UPDATE "users" SET "status" = $1 WHERE "status" = $2`, sql)
	assertArgs(t, []any{"inactive", "active"}, args)
}

// ==================== PostgreSQL DELETE 测试 ====================

// TestPgGrammar_DeleteBasic 验证基本 DELETE：生成 DELETE FROM ... WHERE 语法。
func TestPgGrammar_DeleteBasic(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToDelete()

	assertNoError(t, err)
	assertSQL(t, `DELETE FROM "users" WHERE "id" = $1`, sql)
	assertArgs(t, []any{1}, args)
}

// TestPgGrammar_DeleteMultipleWhere 验证多条件 DELETE：多个 WHERE 条件用 AND 连接。
func TestPgGrammar_DeleteMultipleWhere(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "inactive").
		Where("age", "<", 18).
		ToDelete()

	assertNoError(t, err)
	assertSQL(t, `DELETE FROM "users" WHERE "status" = $1 AND "age" < $2`, sql)
	assertArgs(t, []any{"inactive", 18}, args)
}

// TestPgGrammar_DeleteNoOrderByLimit 验证 PostgreSQL DELETE 不支持 ORDER BY/LIMIT：设置了也不会出现在生成的 SQL 中。
func TestPgGrammar_DeleteNoOrderByLimit(t *testing.T) {
	// PostgreSQL 的 DELETE 不支持 ORDER BY 和 LIMIT，应该忽略
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "inactive").
		OrderBy("created_at", "ASC").
		Limit(10).
		ToDelete()

	assertNoError(t, err)
	// PostgreSQL 编译结果不应包含 ORDER BY 和 LIMIT
	assertSQL(t, `DELETE FROM "users" WHERE "status" = $1`, sql)
	assertArgs(t, []any{"inactive"}, args)
}

// ==================== PostgreSQL 表前缀测试 ====================

// TestPgGrammar_TablePrefix 验证表前缀：设置 SetTablePrefix 后，表名自动拼接前缀。
func TestPgGrammar_TablePrefix(t *testing.T) {
	g := NewPostgresGrammar().SetTablePrefix("app_")
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "app_users"`, sql)
}

// ==================== PostgreSQL WhereNotIn 测试 ====================

// TestPgGrammar_SelectWhereNotIn 验证 WHERE NOT IN：生成 NOT IN ($1, $2, $3) 语法。
func TestPgGrammar_SelectWhereNotIn(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereNotIn("id", []any{1, 2, 3}).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "id" NOT IN ($1, $2, $3)`, sql)
	assertArgs(t, []any{1, 2, 3}, args)
}

// ==================== PostgreSQL WhereColumn 测试 ====================

// TestPgGrammar_SelectWhereColumn 验证列与列比较：不使用占位符，两侧均为双引号包裹的列名。
func TestPgGrammar_SelectWhereColumn(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereColumn("updated_at", ">", "created_at").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "updated_at" > "created_at"`, sql)
	assertArgs(t, []any{}, args)
}

// ==================== PostgreSQL SharedLock 测试 ====================

// TestPgGrammar_SelectSharedLock 验证共享锁：PostgreSQL 使用 FOR SHARE 代替 MySQL 的 LOCK IN SHARE MODE。
func TestPgGrammar_SelectSharedLock(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		SharedLock().
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	// PostgreSQL 使用 FOR SHARE 代替 MySQL 的 LOCK IN SHARE MODE
	assertSQL(t, `SELECT "id", "name", "age", "email" FROM "users" WHERE "id" = $1 FOR SHARE`, sql)
}

// ==================== PostgreSQL ForPage 测试 ====================

// TestPgGrammar_ForPage 验证分页：第 N 页正确计算 OFFSET = (page-1)*perPage。
func TestPgGrammar_ForPage(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		ForPage(3, 10).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name" FROM "users" LIMIT 10 OFFSET 20`, sql)
}

// ==================== PostgreSQL SelectSub 测试 ====================

// TestPgGrammar_SelectSubquery 验证子查询列：作为 SELECT 子句的子查询用括号包裹并带 AS 别名。
func TestPgGrammar_SelectSubquery(t *testing.T) {
	g := NewPostgresGrammar()
	sub := NewBuilder(g).Table("orders").Select("COUNT(*)").WhereRaw(`"orders"."user_id" = "users"."id"`)

	sql, args, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		SelectSubquery(sub, "orders_count").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", (SELECT COUNT(*) FROM "orders" WHERE "orders"."user_id" = "users"."id") AS "orders_count" FROM "users"`, sql)
	assertArgs(t, []any{}, args)
}

// ==================== PostgreSQL FromSub 测试 ====================

// TestPgGrammar_FromSub 验证子查询作为数据源：FROM 子句使用 (subquery) AS alias 语法。
func TestPgGrammar_FromSub(t *testing.T) {
	g := NewPostgresGrammar()
	sub := NewBuilder(g).Table("orders").Select("user_id", "SUM(amount) as total").GroupBy("user_id")

	sql, args, err := NewBuilder(g).
		FromSub(sub, "sub").
		Select("*").
		Where("total", ">", 100).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM (SELECT "user_id", SUM(amount) as total FROM "orders" GROUP BY "user_id") AS "sub" WHERE "total" > $1`, sql)
	assertArgs(t, []any{100}, args)
}

// ==================== PostgreSQL WhereSub 测试 ====================

// TestPgGrammar_WhereSub 验证 WHERE 子查询比较：子查询作为 WHERE 条件的右操作数。
func TestPgGrammar_WhereSub(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereSub("id", "=", func(sub *Builder) {
			sub.Table("orders").Select("user_id").OrderByDesc("created_at").Limit(1)
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" = (SELECT "user_id" FROM "orders" ORDER BY "created_at" DESC LIMIT 1)`, sql)
	assertArgs(t, []any{}, args)
}

// ==================== PostgreSQL WhereInSub 测试 ====================

// TestPgGrammar_WhereInSub 验证 WHERE IN 子查询：IN 子句使用子查询而非值列表。
func TestPgGrammar_WhereInSub(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id").Where("amount", ">", 100)
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" IN (SELECT "user_id" FROM "orders" WHERE "amount" > $1)`, sql)
	assertArgs(t, []any{100}, args)
}

// TestPgGrammar_WhereNotInSub 验证 WHERE NOT IN 子查询：生成 NOT IN (subquery) 语法。
func TestPgGrammar_WhereNotInSub(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereNotInSub("id", func(sub *Builder) {
			sub.Table("blacklist").Select("user_id")
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" NOT IN (SELECT "user_id" FROM "blacklist")`, sql)
	assertArgs(t, []any{}, args)
}

// ==================== PostgreSQL WhereLike 测试 ====================

// TestPgGrammar_WhereLike 验证 LIKE 模糊匹配：生成 WHERE column LIKE $1 语法。
func TestPgGrammar_WhereLike(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereLike("name", "%alice%").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "name" LIKE $1`, sql)
	assertArgs(t, []any{"%alice%"}, args)
}

// ==================== PostgreSQL JoinOn 多条件 测试 ====================

// TestPgGrammar_JoinOnMultipleConditions 验证 JOIN ON 多条件：多个 On 调用生成 AND 连接的 ON 条件。
func TestPgGrammar_JoinOnMultipleConditions(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		JoinOn("contacts", func(j *JoinBuilder) {
			j.On("users.id", "=", "contacts.user_id").
				On("users.name", "=", "contacts.name")
		}).
		Select("users.*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "users".* FROM "users" INNER JOIN "contacts" ON "users"."id" = "contacts"."user_id" AND "users"."name" = "contacts"."name"`, sql)
}

// TestPgGrammar_JoinOnWithWhere 验证 JOIN ON 带 WHERE 值条件：JoinBuilder.Where 生成带占位符的 ON 条件。
func TestPgGrammar_JoinOnWithWhere(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		JoinOn("contacts", func(j *JoinBuilder) {
			j.On("users.id", "=", "contacts.user_id").
				Where("contacts.type", "=", "primary")
		}).
		Select("users.*", "contacts.phone").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "users".*, "contacts"."phone" FROM "users" INNER JOIN "contacts" ON "users"."id" = "contacts"."user_id" AND "contacts"."type" = $1`, sql)
	assertArgs(t, []any{"primary"}, args)
}

// ==================== PostgreSQL InRandomOrder 测试 ====================

// TestPgGrammar_InRandomOrder 验证随机排序：PostgreSQL 使用 ORDER BY RANDOM() 实现随机排序。
func TestPgGrammar_InRandomOrder(t *testing.T) {
	g := NewPostgresGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("*").
		InRandomOrder().
		Limit(5).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" ORDER BY RANDOM() LIMIT 5`, sql)
}

// ==================== PostgreSQL HavingBetween 测试 ====================

// TestPgGrammar_HavingBetween 验证 HAVING BETWEEN：分组后使用 BETWEEN 过滤。
func TestPgGrammar_HavingBetween(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("orders").
		Select("user_id", "SUM(amount) as total").
		GroupBy("user_id").
		HavingBetween("total", 100, 500).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, `SELECT "user_id", SUM(amount) as total FROM "orders" GROUP BY "user_id" HAVING "total" BETWEEN $1 AND $2`, sql)
	assertArgs(t, []any{100, 500}, args)
}

// ==================== PostgreSQL Upsert 测试 ====================

// TestPgGrammar_Upsert 验证单条 Upsert：生成 INSERT ... ON CONFLICT (...) DO UPDATE SET 语法，使用 EXCLUDED 引用新值。
func TestPgGrammar_Upsert(t *testing.T) {
	g := NewPostgresGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToUpsert(data, []string{"email"}, []string{"name", "age"})

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// TestPgGrammar_UpsertBatch 验证批量 Upsert：多行数据生成多组 VALUES 并带 ON CONFLICT DO UPDATE。
func TestPgGrammar_UpsertBatch(t *testing.T) {
	g := NewPostgresGrammar()
	data := []userInsert{
		{Name: "alice", Age: 25, Email: "alice@test.com"},
		{Name: "bob", Age: 30, Email: "bob@test.com"},
	}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToUpsert(data, []string{"email"}, []string{"name", "age"})

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3), ($4, $5, $6) ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com", "bob", 30, "bob@test.com"}, args)
}

// ==================== PostgreSQL InsertOrIgnore 测试 ====================

// TestPgGrammar_InsertOrIgnore 验证 INSERT ... ON CONFLICT DO NOTHING：冲突时静默忽略。
func TestPgGrammar_InsertOrIgnore(t *testing.T) {
	g := NewPostgresGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsertOrIgnore(data)

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// ==================== PostgreSQL InsertUsing 测试 ====================

// TestPgGrammar_InsertUsing 验证 INSERT ... SELECT：从子查询插入数据。
func TestPgGrammar_InsertUsing(t *testing.T) {
	g := NewPostgresGrammar()
	sql, args, err := NewBuilder(g).
		Table("users_archive").
		ToInsertUsing([]string{"name", "email"}, func(sub *Builder) {
			sub.Table("users").Select("name", "email").Where("active", "=", false)
		})

	assertNoError(t, err)
	assertSQL(t, `INSERT INTO "users_archive" ("name", "email") SELECT "name", "email" FROM "users" WHERE "active" = $1`, sql)
	assertArgs(t, []any{false}, args)
}

// ==================== PostgreSQL Truncate 测试 ====================

// TestPgGrammar_Truncate 验证 TRUNCATE TABLE：生成清空表语句。
func TestPgGrammar_Truncate(t *testing.T) {
	g := NewPostgresGrammar()
	sql, err := NewBuilder(g).
		Table("users").
		ToTruncate()

	assertNoError(t, err)
	assertSQL(t, `TRUNCATE TABLE "users"`, sql)
}
