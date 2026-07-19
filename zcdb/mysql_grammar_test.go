package zcdb

import (
	"testing"
)

// ==================== MySQL SELECT 测试 ====================

// TestMySQLGrammar_SelectBasic 验证基本 SELECT 语句：指定列名被反引号包裹，FROM 表名正确。
func TestMySQLGrammar_SelectBasic(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users`", sql)
	assertArgs(t, []any{}, args)
}

// TestMySQLGrammar_SelectAll 验证 SELECT *：通配符不被反引号包裹。
func TestMySQLGrammar_SelectAll(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users`", sql)
	assertArgs(t, []any{}, args)
}

// TestMySQLGrammar_SelectWithWhere 验证多条件 AND WHERE：多个 Where 调用生成 AND 连接的条件。
func TestMySQLGrammar_SelectWithWhere(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("age", ">", 18).
		Where("status", "=", "active").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `age` > ? AND `status` = ?", sql)
	assertArgs(t, []any{18, "active"}, args)
}

// TestMySQLGrammar_SelectOrWhere 验证 OR WHERE：OrWhere 调用生成 OR 连接的条件。
func TestMySQLGrammar_SelectOrWhere(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("age", ">", 18).
		OrWhere("role", "=", "admin").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `age` > ? OR `role` = ?", sql)
	assertArgs(t, []any{18, "admin"}, args)
}

// TestMySQLGrammar_SelectWhereIn 验证 WHERE IN 子句：值列表正确展开为占位符。
func TestMySQLGrammar_SelectWhereIn(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereIn("id", []any{1, 2, 3}).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `id` IN (?, ?, ?)", sql)
	assertArgs(t, []any{1, 2, 3}, args)
}

// TestMySQLGrammar_SelectWhereNull 验证 WHERE IS NULL：生成 IS NULL 判断而非占位符。
func TestMySQLGrammar_SelectWhereNull(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereNull("deleted_at").
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `deleted_at` IS NULL", sql)
	assertArgs(t, []any{}, args)
}

// TestMySQLGrammar_SelectWhereBetween 验证 WHERE BETWEEN：两个边界值生成对应占位符。
func TestMySQLGrammar_SelectWhereBetween(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereBetween("age", 18, 30).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `age` BETWEEN ? AND ?", sql)
	assertArgs(t, []any{18, 30}, args)
}

// TestMySQLGrammar_SelectWhereNested 验证嵌套 WHERE：WhereNested 内的条件被括号包裹。
func TestMySQLGrammar_SelectWhereNested(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "active").
		WhereNested(func(b *Builder) {
			b.Where("age", ">", 18).OrWhere("role", "=", "admin")
		}).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `status` = ? AND (`age` > ? OR `role` = ?)", sql)
	assertArgs(t, []any{"active", 18, "admin"}, args)
}

// TestMySQLGrammar_SelectWhereRaw 验证 WhereRaw：原始 SQL 表达式不被转义或包裹。
func TestMySQLGrammar_SelectWhereRaw(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereRaw("YEAR(created_at) = ?", 2024).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE YEAR(created_at) = ?", sql)
	assertArgs(t, []any{2024}, args)
}

// TestMySQLGrammar_SelectJoin 验证 INNER JOIN：生成正确的 JOIN ON 语法，列名带表前缀。
func TestMySQLGrammar_SelectJoin(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.total", ">", 100).
		Select("users.name", "orders.total").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `users`.`name`, `orders`.`total` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id` WHERE `orders`.`total` > ?", sql)
	assertArgs(t, []any{100}, args)
}

// TestMySQLGrammar_SelectLeftJoin 验证 LEFT JOIN：生成 LEFT JOIN 关键字和 ON 条件。
func TestMySQLGrammar_SelectLeftJoin(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		LeftJoin("profiles", "users.id", "=", "profiles.user_id").
		Select("users.*", "profiles.bio").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `users`.*, `profiles`.`bio` FROM `users` LEFT JOIN `profiles` ON `users`.`id` = `profiles`.`user_id`", sql)
}

// TestMySQLGrammar_SelectGroupByHaving 验证 GROUP BY + HAVING：分组后过滤条件正确生成。
func TestMySQLGrammar_SelectGroupByHaving(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("orders").
		Select("user_id", "COUNT(*) as cnt").
		GroupBy("user_id").
		Having("cnt", ">", 5).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `user_id`, COUNT(*) as cnt FROM `orders` GROUP BY `user_id` HAVING `cnt` > ?", sql)
	assertArgs(t, []any{5}, args)
}

// TestMySQLGrammar_SelectOrderByLimitOffset 验证 ORDER BY + LIMIT + OFFSET：排序、分页子句完整生成。
func TestMySQLGrammar_SelectOrderByLimitOffset(t *testing.T) {
	g := NewMySQLGrammar()
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
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `status` = ? ORDER BY `name` ASC, `created_at` DESC LIMIT 10 OFFSET 20", sql)
	assertArgs(t, []any{"active"}, args)
}

// TestMySQLGrammar_SelectDistinct 验证 DISTINCT：SELECT 后正确插入 DISTINCT 关键字。
func TestMySQLGrammar_SelectDistinct(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Distinct().
		Select("name", "age").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT DISTINCT `name`, `age` FROM `users`", sql)
}

// TestMySQLGrammar_SelectWhereExists 验证 WHERE EXISTS 子查询：EXISTS 子句包含完整子查询。
func TestMySQLGrammar_SelectWhereExists(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				Select("1").
				WhereRaw("`orders`.`user_id` = `users`.`id`")
		}).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE EXISTS (SELECT `1` FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)", sql)
	assertArgs(t, []any{}, args)
}

// TestMySQLGrammar_SelectLockForUpdate 验证悲观锁 FOR UPDATE：SELECT 末尾追加 FOR UPDATE 子句。
func TestMySQLGrammar_SelectLockForUpdate(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		LockForUpdate().
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `id` = ? FOR UPDATE", sql)
}

// ==================== MySQL INSERT 测试 ====================

// TestMySQLGrammar_InsertSingle 验证单条结构体插入：字段映射正确，生成 INSERT INTO ... VALUES (?) 语法。
func TestMySQLGrammar_InsertSingle(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(userInsert{Name: "alice", Age: 25, Email: "alice@test.com"})

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)", sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// TestMySQLGrammar_InsertPartial 验证部分字段插入：零值字段被跳过，仅插入非零字段。
func TestMySQLGrammar_InsertPartial(t *testing.T) {
	g := NewMySQLGrammar()
	// Email 字段为 nil，应被跳过
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(userInsert{Name: "bob", Age: 30})

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`) VALUES (?, ?)", sql)
	assertArgs(t, []any{"bob", 30}, args)
}

// TestMySQLGrammar_InsertBatch 验证批量插入：切片生成多组 VALUES 占位符。
func TestMySQLGrammar_InsertBatch(t *testing.T) {
	g := NewMySQLGrammar()
	data := []userInsert{
		{Name: "alice", Age: 25, Email: "alice@test.com"},
		{Name: "bob", Age: 30, Email: "bob@test.com"},
	}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?), (?, ?, ?)", sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com", "bob", 30, "bob@test.com"}, args)
}

// TestMySQLGrammar_InsertPtrPartial 验证指针字段插入：nil 指针被跳过，非 nil 指针解引用后插入。
func TestMySQLGrammar_InsertPtrPartial(t *testing.T) {
	g := NewMySQLGrammar()
	name := "alice"
	age := 25
	// Email 为 nil 应被跳过
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(userInsertPtr{Name: &name, Age: &age})

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`) VALUES (?, ?)", sql)
	assertArgs(t, []any{"alice", 25}, args)
}

// TestMySQLGrammar_InsertPtrAllNil 验证全 nil 指针插入：所有指针字段均为 nil 时返回 ErrNoFields 错误。
func TestMySQLGrammar_InsertPtrAllNil(t *testing.T) {
	g := NewMySQLGrammar()
	// 所有指针都为 nil，应回退 ErrNoFields
	_, _, err := NewBuilder(g).
		Table("users").
		ToInsert(userInsertPtr{})

	if err != ErrNoFields {
		t.Errorf("expected ErrNoFields, got %v", err)
	}
}

// TestMySQLGrammar_InsertBatchPtr 验证指针字段批量插入：以首行为模板确定列，后续行 nil 字段传入 nil。
func TestMySQLGrammar_InsertBatchPtr(t *testing.T) {
	g := NewMySQLGrammar()
	n1, e1 := "alice", "alice@test.com"
	a1 := 25
	n2 := "bob"
	a2 := 30
	// 第二行的 Email 为 nil，应该传入 nil
	data := []userInsertPtr{
		{Name: &n1, Age: &a1, Email: &e1},
		{Name: &n2, Age: &a2},
	}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsert(data)

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?), (?, ?, ?)", sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com", "bob", 30, nil}, args)
}

// ==================== MySQL UPDATE 测试 ====================

// TestMySQLGrammar_UpdateBasic 验证基本 UPDATE：生成 UPDATE SET ... WHERE 语法，字段和条件正确。
func TestMySQLGrammar_UpdateBasic(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(userUpdate{Name: "alice_new", Age: 26})

	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?", sql)
	assertArgs(t, []any{"alice_new", 26, 1}, args)
}

// TestMySQLGrammar_UpdatePartial 验证部分字段更新：零值字段被跳过，仅更新非零字段。
func TestMySQLGrammar_UpdatePartial(t *testing.T) {
	g := NewMySQLGrammar()
	// 只更新 Status，其他为 nil 被跳过
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(userUpdate{Status: "inactive"})

	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `status` = ? WHERE `id` = ?", sql)
	assertArgs(t, []any{"inactive", 1}, args)
}

// TestMySQLGrammar_UpdateWithExpression 验证表达式更新：Raw 表达式不作为占位符，直接内联到 SQL 中。
func TestMySQLGrammar_UpdateWithExpression(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(userUpdate{Age: Raw("`age` + 1")})

	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `age` = `age` + 1 WHERE `id` = ?", sql)
	assertArgs(t, []any{1}, args)
}

// TestMySQLGrammar_UpdatePtrPartial 验证指针字段更新：nil 指针被跳过，非 nil 指针解引用后传入具体值。
func TestMySQLGrammar_UpdatePtrPartial(t *testing.T) {
	g := NewMySQLGrammar()
	name := "alice_new"
	age := 26
	// Status 为 nil 应被跳过
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(userUpdatePtr{Name: &name, Age: &age})

	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?", sql)
	// 注意：传递给 SQL 驱动的应该是解引用后的具体值（string / int），而不是指针
	assertArgs(t, []any{"alice_new", 26, 1}, args)
}

// TestMySQLGrammar_UpdatePtrAllNil 验证全 nil 指针更新：所有指针字段均为 nil 时返回 ErrNoFields 错误。
func TestMySQLGrammar_UpdatePtrAllNil(t *testing.T) {
	g := NewMySQLGrammar()
	// 所有指针都为 nil，应回退 ErrNoFields
	_, _, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToUpdate(userUpdatePtr{})

	if err != ErrNoFields {
		t.Errorf("expected ErrNoFields, got %v", err)
	}
}

// ==================== MySQL DELETE 测试 ====================

// TestMySQLGrammar_DeleteBasic 验证基本 DELETE：生成 DELETE FROM ... WHERE 语法。
func TestMySQLGrammar_DeleteBasic(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		ToDelete()

	assertNoError(t, err)
	assertSQL(t, "DELETE FROM `users` WHERE `id` = ?", sql)
	assertArgs(t, []any{1}, args)
}

// TestMySQLGrammar_DeleteWithMultipleConditions 验证多条件 DELETE：支持 WHERE + ORDER BY + LIMIT 组合（MySQL 特有）。
func TestMySQLGrammar_DeleteWithMultipleConditions(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		Where("status", "=", "inactive").
		Where("last_login", "<", "2023-01-01").
		OrderBy("id", "asc").
		Limit(100).
		ToDelete()

	assertNoError(t, err)
	assertSQL(t, "DELETE FROM `users` WHERE `status` = ? AND `last_login` < ? ORDER BY `id` ASC LIMIT 100", sql)
	assertArgs(t, []any{"inactive", "2023-01-01"}, args)
}

// ==================== MySQL 表前缀测试 ====================

// TestMySQLGrammar_TablePrefix 验证表前缀：设置 SetTablePrefix 后，表名自动拼接前缀。
func TestMySQLGrammar_TablePrefix(t *testing.T) {
	g := NewMySQLGrammar().SetTablePrefix("app_")
	sql, _, err := NewBuilder(g).
		Table("users").
		Where("id", "=", 1).
		Select("id", "name", "age", "email").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `app_users` WHERE `id` = ?", sql)
}

// ==================== MySQL Clone 测试 ====================

// TestMySQLGrammar_Clone 验证 Builder 克隆：克隆后修改不影响原始对象，各自独立生成 SQL。
func TestMySQLGrammar_Clone(t *testing.T) {
	g := NewMySQLGrammar()
	base := NewBuilder(g).Table("users").Where("active", "=", true)

	q1 := base.Clone().Where("age", ">", 18)
	q2 := base.Clone().Where("age", "<", 30)

	sql1, _, _ := q1.Select("id", "name", "age", "email").ToSelect()
	sql2, _, _ := q2.Select("id", "name", "age", "email").ToSelect()

	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `active` = ? AND `age` > ?", sql1)
	assertSQL(t, "SELECT `id`, `name`, `age`, `email` FROM `users` WHERE `active` = ? AND `age` < ?", sql2)
}

// ==================== MySQL ForPage 测试 ====================

// TestMySQLGrammar_ForPage 验证分页：第 N 页正确计算 OFFSET = (page-1)*perPage。
func TestMySQLGrammar_ForPage(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		ForPage(2, 15).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name` FROM `users` LIMIT 15 OFFSET 15", sql)
}

// TestMySQLGrammar_ForPageFirst 验证第一页分页：第 1 页时不生成 OFFSET 子句。
func TestMySQLGrammar_ForPageFirst(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		ForPage(1, 10).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name` FROM `users` LIMIT 10", sql)
}

// ==================== MySQL SelectRaw 测试 ====================

// TestMySQLGrammar_SelectRaw 验证 SelectRaw：含括号的表达式不被反引号包裹，与普通列混合使用。
func TestMySQLGrammar_SelectRaw(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("orders").
		Select("user_id").
		SelectRaw("SUM(amount) as total").
		GroupBy("user_id").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `user_id`, SUM(amount) as total FROM `orders` GROUP BY `user_id`", sql)
}

// ==================== MySQL SelectSub 测试 ====================

// TestMySQLGrammar_SelectSubquery 验证子查询列：作为 SELECT 子句的子查询用括号包裹并带 AS 别名。
func TestMySQLGrammar_SelectSubquery(t *testing.T) {
	g := NewMySQLGrammar()
	sub := NewBuilder(g).Table("orders").Select("COUNT(*)").WhereRaw("`orders`.`user_id` = `users`.`id`")

	sql, args, err := NewBuilder(g).
		Table("users").
		Select("id", "name").
		SelectSubquery(sub, "orders_count").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `id`, `name`, (SELECT COUNT(*) FROM `orders` WHERE `orders`.`user_id` = `users`.`id`) AS `orders_count` FROM `users`", sql)
	assertArgs(t, []any{}, args)
}

// ==================== MySQL FromSub 测试 ====================

// TestMySQLGrammar_FromSub 验证子查询作为数据源：FROM 子句使用 (subquery) AS alias 语法。
func TestMySQLGrammar_FromSub(t *testing.T) {
	g := NewMySQLGrammar()
	sub := NewBuilder(g).Table("orders").Select("user_id", "SUM(amount) as total").GroupBy("user_id")

	sql, _, err := NewBuilder(g).
		FromSub(sub, "sub").
		Select("*").
		Where("total", ">", 100).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM (SELECT `user_id`, SUM(amount) as total FROM `orders` GROUP BY `user_id`) AS `sub` WHERE `total` > ?", sql)
}

// ==================== MySQL WhereSub 测试 ====================

// TestMySQLGrammar_WhereSub 验证 WHERE 子查询比较：子查询作为 WHERE 条件的右操作数。
func TestMySQLGrammar_WhereSub(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereSub("id", "=", func(sub *Builder) {
			sub.Table("orders").Select("user_id").OrderByDesc("created_at").Limit(1)
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `id` = (SELECT `user_id` FROM `orders` ORDER BY `created_at` DESC LIMIT 1)", sql)
	assertArgs(t, []any{}, args)
}

// ==================== MySQL WhereInSub 测试 ====================

// TestMySQLGrammar_WhereInSub 验证 WHERE IN 子查询：IN 子句使用子查询而非值列表。
func TestMySQLGrammar_WhereInSub(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id").Where("amount", ">", 100)
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `id` IN (SELECT `user_id` FROM `orders` WHERE `amount` > ?)", sql)
	assertArgs(t, []any{100}, args)
}

// TestMySQLGrammar_WhereNotInSub 验证 WHERE NOT IN 子查询：生成 NOT IN (subquery) 语法。
func TestMySQLGrammar_WhereNotInSub(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereNotInSub("id", func(sub *Builder) {
			sub.Table("blacklist").Select("user_id")
		}).
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `id` NOT IN (SELECT `user_id` FROM `blacklist`)", sql)
	assertArgs(t, []any{}, args)
}

// ==================== MySQL WhereLike 测试 ====================

// TestMySQLGrammar_WhereLike 验证 LIKE 模糊匹配：生成 WHERE column LIKE ? 语法。
func TestMySQLGrammar_WhereLike(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereLike("name", "%alice%").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `name` LIKE ?", sql)
	assertArgs(t, []any{"%alice%"}, args)
}

// TestMySQLGrammar_WhereNotLike 验证 NOT LIKE：生成 WHERE column NOT LIKE ? 语法。
func TestMySQLGrammar_WhereNotLike(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		WhereNotLike("email", "%spam%").
		Select("*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `email` NOT LIKE ?", sql)
	assertArgs(t, []any{"%spam%"}, args)
}

// ==================== MySQL JoinOn 多条件 测试 ====================

// TestMySQLGrammar_JoinOnMultipleConditions 验证 JOIN ON 多条件：多个 On 调用生成 AND 连接的 ON 条件。
func TestMySQLGrammar_JoinOnMultipleConditions(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		JoinOn("contacts", func(j *JoinBuilder) {
			j.On("users.id", "=", "contacts.user_id").
				On("users.name", "=", "contacts.name")
		}).
		Select("users.*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `users`.* FROM `users` INNER JOIN `contacts` ON `users`.`id` = `contacts`.`user_id` AND `users`.`name` = `contacts`.`name`", sql)
}

// TestMySQLGrammar_JoinOnOrCondition 验证 JOIN ON OR 条件：OrOn 调用生成 OR 连接的 ON 条件。
func TestMySQLGrammar_JoinOnOrCondition(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		JoinOn("contacts", func(j *JoinBuilder) {
			j.On("users.id", "=", "contacts.user_id").
				OrOn("users.id", "=", "contacts.alt_user_id")
		}).
		Select("users.*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `users`.* FROM `users` INNER JOIN `contacts` ON `users`.`id` = `contacts`.`user_id` OR `users`.`id` = `contacts`.`alt_user_id`", sql)
}

// TestMySQLGrammar_JoinOnWithWhere 验证 JOIN ON 带 WHERE 值条件：JoinBuilder.Where 生成带占位符的 ON 条件。
func TestMySQLGrammar_JoinOnWithWhere(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users").
		JoinOn("contacts", func(j *JoinBuilder) {
			j.On("users.id", "=", "contacts.user_id").
				Where("contacts.type", "=", "primary")
		}).
		Select("users.*", "contacts.phone").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `users`.*, `contacts`.`phone` FROM `users` INNER JOIN `contacts` ON `users`.`id` = `contacts`.`user_id` AND `contacts`.`type` = ?", sql)
	assertArgs(t, []any{"primary"}, args)
}

// TestMySQLGrammar_LeftJoinOn 验证 LEFT JOIN ON 多条件：使用 LeftJoinOn 生成 LEFT JOIN + 多 ON 条件。
func TestMySQLGrammar_LeftJoinOn(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		LeftJoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				On("profiles.active", "=", "users.active")
		}).
		Select("users.*").
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `users`.* FROM `users` LEFT JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = `users`.`active`", sql)
}

// ==================== MySQL InRandomOrder 测试 ====================

// TestMySQLGrammar_InRandomOrder 验证随机排序：MySQL 使用 ORDER BY RAND() 实现随机排序。
func TestMySQLGrammar_InRandomOrder(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g).
		Table("users").
		Select("*").
		InRandomOrder().
		Limit(5).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` ORDER BY RAND() LIMIT 5", sql)
}

// ==================== MySQL HavingBetween 测试 ====================

// TestMySQLGrammar_HavingBetween 验证 HAVING BETWEEN：分组后使用 BETWEEN 过滤。
func TestMySQLGrammar_HavingBetween(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("orders").
		Select("user_id", "SUM(amount) as total").
		GroupBy("user_id").
		HavingBetween("total", 100, 500).
		ToSelect()

	assertNoError(t, err)
	assertSQL(t, "SELECT `user_id`, SUM(amount) as total FROM `orders` GROUP BY `user_id` HAVING `total` BETWEEN ? AND ?", sql)
	assertArgs(t, []any{100, 500}, args)
}

// ==================== MySQL Upsert 测试 ====================

// TestMySQLGrammar_Upsert 验证单条 Upsert：生成 INSERT ... ON DUPLICATE KEY UPDATE 语法。
func TestMySQLGrammar_Upsert(t *testing.T) {
	g := NewMySQLGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToUpsert(data, []string{"email"}, []string{"name", "age"})

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)", sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// TestMySQLGrammar_UpsertBatch 验证批量 Upsert：多行数据生成多组 VALUES 并带 ON DUPLICATE KEY UPDATE。
func TestMySQLGrammar_UpsertBatch(t *testing.T) {
	g := NewMySQLGrammar()
	data := []userInsert{
		{Name: "alice", Age: 25, Email: "alice@test.com"},
		{Name: "bob", Age: 30, Email: "bob@test.com"},
	}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToUpsert(data, []string{"email"}, []string{"name", "age"})

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?), (?, ?, ?) ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)", sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com", "bob", 30, "bob@test.com"}, args)
}

// ==================== MySQL InsertOrIgnore 测试 ====================

// TestMySQLGrammar_InsertOrIgnore 验证 INSERT IGNORE：生成 INSERT IGNORE INTO 语法，冲突时静默忽略。
func TestMySQLGrammar_InsertOrIgnore(t *testing.T) {
	g := NewMySQLGrammar()
	data := userInsert{Name: "alice", Age: 25, Email: "alice@test.com"}
	sql, args, err := NewBuilder(g).
		Table("users").
		ToInsertOrIgnore(data)

	assertNoError(t, err)
	assertSQL(t, "INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)", sql)
	assertArgs(t, []any{"alice", 25, "alice@test.com"}, args)
}

// ==================== MySQL InsertUsing 测试 ====================

// TestMySQLGrammar_InsertUsing 验证 INSERT ... SELECT：从子查询插入数据。
func TestMySQLGrammar_InsertUsing(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g).
		Table("users_archive").
		ToInsertUsing([]string{"name", "email"}, func(sub *Builder) {
			sub.Table("users").Select("name", "email").Where("active", "=", false)
		})

	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users_archive` (`name`, `email`) SELECT `name`, `email` FROM `users` WHERE `active` = ?", sql)
	assertArgs(t, []any{false}, args)
}

// ==================== MySQL Truncate 测试 ====================

// TestMySQLGrammar_Truncate 验证 TRUNCATE TABLE：生成清空表语句。
func TestMySQLGrammar_Truncate(t *testing.T) {
	g := NewMySQLGrammar()
	sql, err := NewBuilder(g).
		Table("users").
		ToTruncate()

	assertNoError(t, err)
	assertSQL(t, "TRUNCATE TABLE `users`", sql)
}
