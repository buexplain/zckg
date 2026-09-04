// 本文件为 SQLite 集成测试——Where 系列条件构造。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "modernc.org/sqlite"
	"testing"
	"time"
)

// TestSQLiteInteg_SelectWhereBasic 验证基础 WHERE 等值条件：WHERE age = 25 应精确匹配到对应行。
func TestSQLiteInteg_SelectWhereBasic(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Where("age", "=", 25).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" {
		t.Errorf("expected [alice], got %v", rows)
	}
}

// TestSQLiteInteg_SelectWithWhere 验证多条件 AND WHERE 组合。
func TestSQLiteInteg_SelectWithWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("age", ">", 20).
		Where("status", "=", "active").
		OrderBy("age", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// alice(25,active), bob(30,active), diana(28,active) → 3 rows
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_SelectWhereOr 验证 OR 条件组合：WHERE age=25 OR age=30 应返回满足任一条件的行。
func TestSQLiteInteg_SelectWhereOr(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("age", "=", 25).
		OrWhere("age", "=", 30).
		OrderBy("age", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "alice" || rows[1].Name != "bob" {
		t.Errorf("expected [alice, bob], got %v", rows)
	}
}

// TestSQLiteInteg_SelectWhereIn 验证 WHERE IN 条件：传入值列表，确认只返回 ID 在列表中的行。
func TestSQLiteInteg_SelectWhereIn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereIn("id", []any{1, 3, 5}).
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_SelectWhereNotIn 验证 WHERE NOT IN 条件：排除指定 ID，确认返回剩余行。
func TestSQLiteInteg_SelectWhereNotIn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotIn("id", []any{1, 2}).
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_SelectWhereNull 验证 WHERE IS NULL 条件：筛选字段值为 NULL 的行。
func TestSQLiteInteg_SelectWhereNull(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereNull("age").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "eve" {
		t.Errorf("expected [eve], got %v", rows)
	}
}

// TestSQLiteInteg_SelectWhereNotNull 验证 WHERE IS NOT NULL 条件：排除字段值为 NULL 的行。
func TestSQLiteInteg_SelectWhereNotNull(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereNotNull("age").OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d", len(rows))
	}
}

// TestSQLiteInteg_SelectWhereBetween 验证 WHERE BETWEEN 范围条件：筛选 age 在 [25, 30] 区间内的行。
func TestSQLiteInteg_SelectWhereBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereBetween("age", 25, 30).OrderBy("age", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_SelectWhereNotBetween 验证 WHERE NOT BETWEEN 条件：排除 age 在 [25, 30] 区间内的行（NULL 值也被排除）。
func TestSQLiteInteg_SelectWhereNotBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereNotBetween("age", 25, 30).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "charlie" {
		t.Errorf("expected [charlie], got %v", rows)
	}
}

// TestSQLiteInteg_SelectWhereNested 验证嵌套 WHERE 条件组：使用 WhereNested 生成 (age > 25 AND status = 'active') 括号分组。
func TestSQLiteInteg_SelectWhereNested(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNested(func(b *Builder) {
			b.Where("age", ">", 25).Where("status", "=", "active")
		}).
		OrderBy("age", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if len(rows) >= 2 && (rows[0].Name != "diana" || rows[1].Name != "bob") {
		t.Errorf("expected [diana, bob], got %v", rows)
	}
}

// TestSQLiteInteg_SelectWhereRaw 验证原始 WHERE 表达式：通过 WhereRaw 传入手写 SQL 片段及绑定参数。
func TestSQLiteInteg_SelectWhereRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereRaw("age > ? AND name LIKE ?", 28, "b%").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "bob" {
		t.Errorf("expected [bob], got %v", rows)
	}
}

// TestSQLiteInteg_WhereLike 验证 LIKE 模糊匹配。
func TestSQLiteInteg_WhereLike(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereLike("name", "%ali%").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" {
		t.Errorf("expected [alice], got %v", rows)
	}
}

// TestSQLiteInteg_WhereNotLike 验证 NOT LIKE 排除匹配。
func TestSQLiteInteg_WhereNotLike(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereNotLike("name", "%ali%").OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_WhereSub 验证 WHERE 子查询比较：age > (SELECT AVG(age) ...)，筛选出大于平均值的行。
func TestSQLiteInteg_WhereSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereSub("age", ">", func(sub *Builder) {
			sub.Table("users").SelectRaw("AVG(age)").WhereNotNull("age")
		}).
		OrderBy("age", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", rows)
	}
}

// TestSQLiteInteg_WhereInSub 验证 WHERE IN 子查询：id IN (SELECT user_id FROM orders ...)，通过子查询动态生成值列表。
func TestSQLiteInteg_WhereInSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id").Where("amount", ">", 100)
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 users, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_WhereNotInSub 验证 WHERE NOT IN 子查询。
func TestSQLiteInteg_WhereNotInSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id")
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// eve(id=5) has no orders
	if len(rows) != 1 || rows[0].Name != "eve" {
		t.Errorf("expected [eve], got %v", rows)
	}
}

// TestSQLiteInteg_WhereExists 验证 WHERE EXISTS 子查询：只保留在 orders 表中存在关联记录的用户。
func TestSQLiteInteg_WhereExists(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				SelectRaw("1").
				WhereColumn("orders.user_id", "=", "users.id")
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_WhereNotExists 验证 WHERE NOT EXISTS 子查询：只保留在 orders 表中不存在关联记录的用户。
func TestSQLiteInteg_WhereNotExists(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotExists(func(sub *Builder) {
			sub.Table("orders").
				Select("orders.id").
				WhereColumn("orders.user_id", "=", "users.id")
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// eve(id=5) has no orders
	if len(rows) != 1 || rows[0].Name != "eve" {
		t.Errorf("expected [eve], got %v", rows)
	}
}

// TestSQLiteInteg_DeleteWithWhere 验证带条件删除：WHERE id=1 只删除一行，其余行不受影响。
func TestSQLiteInteg_DeleteWithWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	_, err := db.Builder().Table("users").Where("id", "=", 1).Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 4 {
		t.Errorf("expected 4 remaining users, got %d", count)
	}
}

// TestSQLiteInteg_DeleteWithoutWhere 验证无 WHERE 条件的 Delete 被拒绝（防误操作全表删除）。
func TestSQLiteInteg_DeleteWithoutWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	_, err := db.Builder().Table("users").Delete(context.Background())
	if !errors.Is(err, ErrDeleteWithoutWhere) {
		t.Fatalf("expected ErrDeleteWithoutWhere, got %v", err)
	}

	// 数据应原封不动
	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 5 {
		t.Errorf("expected 5 users (delete rejected), got %d", count)
	}
}

// TestSQLiteInteg_UpdateWithoutWhere 验证无 WHERE 条件的 Update 被拒绝（防误操作全表更新）。
func TestSQLiteInteg_UpdateWithoutWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type updateData struct {
		Status string `db:"status"`
	}
	_, err := db.Builder().Table("users").Update(context.Background(), updateData{Status: "vip"})
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Fatalf("expected ErrUpdateWithoutWhere, got %v", err)
	}

	// 数据应原封不动
	count, _ := db.Builder().Table("users").Where("status", "=", "vip").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 vip users (update rejected), got %d", count)
	}
}

// TestSQLiteInteg_OrWhereRaw 验证 OrWhereRaw：原始 SQL OR 条件与绑定参数。
func TestSQLiteInteg_OrWhereRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("status", "=", "active").
		OrWhereRaw("age IS NOT NULL").
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// active: alice, bob, diana (3); age IS NOT NULL: alice,bob,charlie,diana → 去重后 4
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_OrWhereNested 验证 OrWhereNested：OR 连接嵌套条件组。
func TestSQLiteInteg_OrWhereNested(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("status", "=", "active").
		OrWhereNested(func(b *Builder) {
			b.Where("age", ">", 30).Where("name", "=", "charlie")
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// active: alice, bob, diana (3); charlie(age=35) → 4
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_OrWhereSub 验证 OrWhereSub：OR 连接子查询比较条件。
func TestSQLiteInteg_OrWhereSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("id", ">", 2).
		OrWhereSub("age", ">", func(sub *Builder) {
			sub.Table("users").SelectRaw("AVG(age)").WhereNotNull("age")
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// id>2: charlie(3), diana(4), eve(5); AVG(age)=29.5, age>29.5: bob(30), charlie(35)
	// 合并去重: bob, charlie, diana, eve → 4
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_OrWhereLike 验证 OrWhereLike：OR 连接 LIKE 模糊匹配。
func TestSQLiteInteg_OrWhereLike(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereLike("name", "a%").
		OrWhereLike("name", "b%").
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// name LIKE 'a%': alice; name LIKE 'b%': bob → 2
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_Complex_WhereExistsMultiJoinGroupBy 验证 WHERE EXISTS关联子查询 + 多表JOIN + GROUP BY。
// 预期：alice(2), bob(2)
func TestSQLiteInteg_Complex_WhereExistsMultiJoinGroupBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	type row struct {
		Name       string `db:"name"`
		OrderCount int    `db:"order_count"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("users.name").
		SelectRaw("COUNT(orders.id) AS order_count").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id")
		}).
		LeftJoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id")
		}).
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				SelectRaw("COUNT(*)").
				WhereRaw("orders.user_id = users.id").
				Having("COUNT(*)", ">=", 2)
		}).
		GroupBy("users.id", "users.name").
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "alice" || rows[0].OrderCount != 2 {
		t.Errorf("row[0]: expected alice/2, got %v", rows[0])
	}
	if rows[1].Name != "bob" || rows[1].OrderCount != 2 {
		t.Errorf("row[1]: expected bob/2, got %v", rows[1])
	}
}

// TestSQLiteInteg_WhereLikeExpression 验证 WhereLike 传入 Expression 的真实执行：
// Expression 直接内嵌为原始 SQL（无占位符、无绑定参数），SQL 语法正确且结果正确。
func TestSQLiteInteg_WhereLikeExpression(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 编译层面：Expression 内嵌，无占位符、无绑定参数
	sql, args, err := db.Builder().Table("users").WhereLike("name", NewExpression("'%a%'")).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "name" LIKE '%a%'`, sql)
	assertArgs(t, nil, args)

	// 执行层面：alice/charlie/diana 名字含 a → 3 行
	count, err := db.Builder().Table("users").WhereLike("name", NewExpression("'%a%'")).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("LIKE '%%a%%' count: expected 3, got %d", count)
	}
}

// TestSQLiteInteg_WhereNotLikeExpression 验证 WhereNotLike 传入 Expression 的真实执行。
func TestSQLiteInteg_WhereNotLikeExpression(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 执行层面：bob/eve 名字不含 a → 2 行
	count, err := db.Builder().Table("users").WhereNotLike("name", NewExpression("'%a%'")).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Fatalf("NOT LIKE '%%a%%' count: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_WhereRawExpression 验证 WhereRaw 绑定参数含 Expression 时真实执行。
func TestSQLiteInteg_WhereRawExpression(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// amount > 100：120/200/150 共 3 行
	count, err := db.Builder().
		Table("orders").
		WhereRaw("amount > ?", NewExpression("100")).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("WhereRaw with Expression: expected 3 rows, got %d", count)
	}
}

// TestSQLiteInteg_MixedAndOrLeadingBoolean
// 编译层首个条件不输出前置 and，混合 AND/OR 连接执行结果正确。
func TestSQLiteInteg_MixedAndOrLeadingBoolean(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	// status='active' OR (age>30 AND id>=1)：alice/bob/diana + charlie
	err := db.Builder().Table("users").Select("name").
		Where("status", "=", "active").
		OrWhere("age", ">", 30).
		Where("id", ">=", 1).
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_ExpressionValueNotBound
// Where/WhereRaw 的 Expression 值直接内联，不产生绑定参数。
func TestSQLiteInteg_ExpressionValueNotBound(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	// Where 值传 Expression：编译为 age = 30，无绑定
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("age", "=", NewExpression("30")).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "bob" {
		t.Errorf("expected bob, got %v", rows)
	}

	// WhereRaw 混合绑定：Expression 不占位，普通绑定顺序不受影响
	var rows2 []row
	err = db.Builder().Table("users").Select("name").
		WhereRaw("age > ? AND age < ?", 20, NewExpression("40")).
		OrderBy("id", "ASC").
		Find(context.Background(), &rows2)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows2) != 4 {
		t.Errorf("expected 4 rows (20<age<40), got %d: %v", len(rows2), rows2)
	}
}

// TestSQLiteInteg_WhereInEmptyScalar
// zcdb WhereIn 入参为 []any 强类型，传标量在编译期即被拒绝（无法构造运行时异常）；
// 空切片语义：IN 空集等价 0=1 返回空，NOT IN 空集等价 1=1 返回全量。
func TestSQLiteInteg_WhereInEmptyScalar(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	// 空 IN：无结果
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereIn("id", []any{}).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for empty IN, got %d", len(rows))
	}
	// 空 NOT IN：全部行
	var rows2 []row
	err = db.Builder().Table("users").Select("name").WhereNotIn("id", []any{}).OrderBy("id", "ASC").Find(context.Background(), &rows2)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows2) != 5 {
		t.Errorf("expected 5 rows for empty NOT IN, got %d", len(rows2))
	}
	// 单元素切片正常（标量需包成 []any）
	var rows3 []row
	err = db.Builder().Table("users").Select("name").WhereIn("id", []any{1}).Find(context.Background(), &rows3)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows3) != 1 || rows3[0].Name != "alice" {
		t.Errorf("expected alice, got %v", rows3)
	}
}

// TestSQLiteInteg_WhereSubInvalidOperator
// 子查询运算符不在白名单内时返回 ErrInvalidOperator。
func TestSQLiteInteg_WhereSubInvalidOperator(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").
		WhereSub("id", "EVIL", func(sub *Builder) {
			sub.Table("users").Select("id")
		}).
		Find(context.Background(), &rows)
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}
}

// TestSQLiteInteg_DateWhere 验证日期 where 用 WhereRaw 手工构造
// （strftime 系列）。
func TestSQLiteInteg_DateWhere(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE datetime_test (
		id     INTEGER PRIMARY KEY AUTOINCREMENT,
		ts_val TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO datetime_test (ts_val) VALUES
		('2024-06-15 14:30:00'),
		('2023-12-25 08:05:00'),
		('2024-06-01 23:59:59')`)

	// whereDate: strftime('%Y-%m-%d', col) = ?
	count, err := db.Builder().Table("datetime_test").
		WhereRaw("strftime('%Y-%m-%d', ts_val) = ?", "2024-06-15").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDate Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDate: expected 1, got %d", count)
	}

	// whereDay: strftime('%d', col) = ?
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("strftime('%d', ts_val) = ?", "15").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDay Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDay: expected 1, got %d", count)
	}

	// whereMonth: strftime('%m', col) = ?（两行 6 月）
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("strftime('%m', ts_val) = ?", "06").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereMonth Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("whereMonth: expected 2, got %d", count)
	}

	// whereYear: strftime('%Y', col) = ?（两行 2024）
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("strftime('%Y', ts_val) = ?", "2024").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereYear Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("whereYear: expected 2, got %d", count)
	}

	// whereTime: strftime('%H:%M:%S', col) = ?
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("strftime('%H:%M:%S', ts_val) = ?", "14:30:00").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereTime Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereTime: expected 1, got %d", count)
	}
}

// TestSQLiteInteg_JsonContains 验证 JSON 包含查询用 WhereRaw 构造
// （json_each 子查询）。
func TestSQLiteInteg_JsonContains(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE json_items (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		items TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO json_items (items) VALUES
		('["apple","banana"]'),
		('["cherry"]'),
		('{"name":"alice"}')`)

	// contains: exists (select 1 from json_each(col, '$') where json_each.value = ?)
	count, err := db.Builder().Table("json_items").
		WhereRaw("exists (select 1 from json_each(items, '$') where json_each.value = ?)", "banana").
		Count(context.Background())
	if err != nil {
		t.Fatalf("contains Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("contains: expected 1, got %d", count)
	}

	// doesntContain: not exists (...)
	count, err = db.Builder().Table("json_items").
		WhereRaw("not exists (select 1 from json_each(items, '$') where json_each.value = ?)", "banana").
		Count(context.Background())
	if err != nil {
		t.Fatalf("doesntContain Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("doesntContain: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_JsonKeyLength 验证 JSON 键存在与长度查询
// （json_type is not null / json_array_length）。
func TestSQLiteInteg_JsonKeyLength(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE json_items (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		items TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO json_items (items) VALUES
		('["apple","banana"]'),
		('["cherry"]'),
		('{"name":"alice","tags":[1,2,3]}')`)

	// containsKey: json_type(col, '$.field') is not null
	count, err := db.Builder().Table("json_items").
		WhereRaw("json_type(items, '$.name') is not null").
		Count(context.Background())
	if err != nil {
		t.Fatalf("containsKey Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("containsKey: expected 1, got %d", count)
	}

	// length: json_array_length(col) = ?
	count, err = db.Builder().Table("json_items").
		WhereRaw("json_array_length(items) = ?", 2).
		Count(context.Background())
	if err != nil {
		t.Fatalf("length Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("length: expected 1, got %d", count)
	}

	// length 带路径：json_array_length(col, '$.tags') = ?
	count, err = db.Builder().Table("json_items").
		WhereRaw("json_array_length(items, '$.tags') = ?", 3).
		Count(context.Background())
	if err != nil {
		t.Fatalf("length with path Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("length with path: expected 1, got %d", count)
	}
}

// TestSQLiteInteg_Bitwise 验证位运算条件用 WhereRaw/Expression 组合
// （(flags & ?) = ?）。
func TestSQLiteInteg_Bitwise(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE bit_test (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		flags INTEGER NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO bit_test (flags) VALUES (1), (2), (4), (6), (3)`)

	// 位与：flags 含 bit2（2、6、3 → 3 行）
	count, err := db.Builder().Table("bit_test").
		WhereRaw("(flags & ?) = ?", 2, 2).
		Count(context.Background())
	if err != nil {
		t.Fatalf("bitwise & Count error: %v", err)
	}
	if count != 3 {
		t.Errorf("bitwise &: expected 3, got %d", count)
	}

	// Expression 形式：flags = (flags & 2)（仅 flags=2 成立）
	count, err = db.Builder().Table("bit_test").
		Where("flags", "=", NewExpression("flags & 2")).
		Count(context.Background())
	if err != nil {
		t.Fatalf("bitwise Expression Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("bitwise Expression: expected 1, got %d", count)
	}
}

// TestSQLiteInteg_RowValues 验证行值比较用 WhereRaw 构造
// （(a, b) >= (?, ?)）。
func TestSQLiteInteg_RowValues(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// (user_id, amount) >= (2, 100)：user2-200、user3-30、user4-150 → 3 行
	var rows []struct {
		ID     int     `db:"id"`
		UserID int     `db:"user_id"`
		Amount float64 `db:"amount"`
	}
	err := db.Builder().Table("orders").
		Select("id", "user_id", "amount").
		WhereRaw("(user_id, amount) >= (?, ?)", 2, 100).
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(rows) != 3 || rows[0].UserID != 2 || rows[1].UserID != 3 || rows[2].UserID != 4 {
		t.Errorf("row values: expected user_id [2 3 4], got %+v", rows)
	}
}

// TestSQLiteInteg_ArrayWhereColumn 验证多列条件括号分组用 WhereRaw 构造
// （(a >= ? AND b <= ?)）。
func TestSQLiteInteg_ArrayWhereColumn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 25 <= age <= 30：alice(25)、bob(30)、diana(28) → 3 行
	count, err := db.Builder().Table("users").
		WhereRaw("(age >= ? AND age <= ?)", 25, 30).
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereShorthand 验证 Where 两参简写（缺省 =）。
func TestSQLiteInteg_NewApi_WhereShorthand(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").Where("age", 25).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("Where shorthand: expected 1, got %d", count)
	}

	// 简写与三参混用
	count, err = db.Builder().Table("users").Where("age", 25).OrWhere("name", "=", "bob").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("Where shorthand + OrWhere: expected 2, got %d", count)
	}

	// 三参形式保持兼容
	count, err = db.Builder().Table("users").Where("age", ">", 29).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("Where 3-arg: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereDate 验证 WhereDate：strftime('%Y-%m-%d', col) = ?。
func TestSQLiteInteg_NewApi_WhereDate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteEventsTable(t, db)

	count, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereDate_Time 验证 WhereDate 传 time.Time 时仍能按日期匹配。
func TestSQLiteInteg_NewApi_WhereDate_Time(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteEventsTable(t, db)

	dt := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	count, err := db.Builder().Table("events").WhereDate("happened_at", dt).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate(time.Time): expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereNilValue 验证 nil 值特判：= nil → IS NULL，!=/<> nil → IS NOT NULL。
func TestSQLiteInteg_NewApi_WhereNilValue(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	tests := []struct {
		name     string
		op       string
		expected int
	}{
		{"eq_nil", "=", 1},   // eve
		{"ne_nil", "!=", 4},  // 其余 4 人
		{"ne2_nil", "<>", 4}, // 其余 4 人
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := db.Builder().Table("users").Where("age", tt.op, nil).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// TestSQLiteInteg_NewApi_WhereNullMulti 验证 WhereNull/WhereNotNull 变参多列展开。
func TestSQLiteInteg_NewApi_WhereNullMulti(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 单列兼容
	count, err := db.Builder().Table("users").WhereNull("age").Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("WhereNull single: expected 1, got %d", count)
	}

	// 多列 AND 展开：age 且 email 均为 NULL 的行不存在
	count, err = db.Builder().Table("users").WhereNull("age", "email").Count(context.Background())
	assertNoError(t, err)
	if count != 0 {
		t.Errorf("WhereNull multi: expected 0, got %d", count)
	}

	// WhereNotNull 多列
	count, err = db.Builder().Table("users").WhereNotNull("age", "email").Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("WhereNotNull multi: expected 4, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_BetweenSeries 验证 Between 系列 7 个新 API 的真实执行。
func TestSQLiteInteg_NewApi_BetweenSeries(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteRangesTable(t, db)

	type queryCase struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}
	tests := []queryCase{
		{"OrWhereBetween", func(b *Builder) *Builder {
			return b.Table("users").Where("age", ">", 34).OrWhereBetween("age", 25, 26)
		}, 2}, // charlie(35) + alice(25)
		{"WhereNotBetween", func(b *Builder) *Builder {
			return b.Table("users").WhereNotBetween("age", 25, 30)
		}, 1}, // charlie；eve 的 NULL 被排除
		{"OrWhereNotBetween", func(b *Builder) *Builder {
			return b.Table("users").Where("name", "=", "alice").OrWhereNotBetween("age", 25, 30)
		}, 2}, // alice + charlie
		{"WhereBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").WhereBetweenColumns("val", "lo", "hi")
		}, 2}, // id 1、3
		{"WhereNotBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").WhereNotBetweenColumns("val", "lo", "hi")
		}, 1}, // id 2
		{"OrWhereBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").Where("id", "=", 0).OrWhereBetweenColumns("val", "lo", "hi")
		}, 2},
		{"OrWhereNotBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").Where("id", "=", 0).OrWhereNotBetweenColumns("val", "lo", "hi")
		}, 1},
		{"WhereValueBetween", func(b *Builder) *Builder {
			return b.Table("ranges").WhereValueBetween(5, "lo", "hi")
		}, 2}, // id 1、2（区间均为 1~10）
		{"OrWhereValueBetween", func(b *Builder) *Builder {
			return b.Table("ranges").Where("id", "=", 0).OrWhereValueBetween(7, "lo", "hi")
		}, 3}, // 三行区间均含 7
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// TestSQLiteInteg_NewApi_NullSafe 验证空安全相等比较（SQLite 编译为 IS / IS NOT）。
func TestSQLiteInteg_NewApi_NullSafe(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	tests := []struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}{
		{"eq_nil", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeEquals("age", nil)
		}, 1}, // eve
		{"eq_value", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeEquals("age", 25)
		}, 1}, // alice
		{"ne_nil", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeNotEquals("age", nil)
		}, 4},
		{"ne_value", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeNotEquals("age", 25)
		}, 4}, // bob/charlie/diana/eve（NULL IS NOT 25 为真）
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// TestSQLiteInteg_NewApi_WhereLikeCaseSensitive 验证 WhereLike 第三参（SQLite 区分大小写编译为 GLOB）。
func TestSQLiteInteg_NewApi_WhereLikeCaseSensitive(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 默认不区分大小写（LIKE）
	count, err := db.Builder().Table("users").WhereLike("name", "%LI%").Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // alice、charlie
		t.Errorf("default like: expected 2, got %d", count)
	}

	// 区分大小写（GLOB，通配符 * / ?）
	count, err = db.Builder().Table("users").WhereLike("name", "*li*", true).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("glob match: expected 2, got %d", count)
	}
	count, err = db.Builder().Table("users").WhereLike("name", "*LI*", true).Count(context.Background())
	assertNoError(t, err)
	if count != 0 {
		t.Errorf("glob case-sensitive: expected 0, got %d", count)
	}

	// OrWhereLike
	count, err = db.Builder().Table("users").
		WhereLike("name", "al%").
		OrWhereLike("name", "bo%").
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("OrWhereLike: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereNot 验证 WhereNot/OrWhereNot 闭包整体取反。
func TestSQLiteInteg_NewApi_WhereNot(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").
		WhereNot(func(q *Builder) { q.Where("status", "=", "active") }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // charlie、eve
		t.Errorf("WhereNot: expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("name", "=", "alice").
		OrWhereNot(func(q *Builder) { q.Where("age", ">", 29) }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // alice + diana（28 不 > 29）
		t.Errorf("OrWhereNot: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereAllAnyNone 验证 WhereAll/Any/None 及 Or 变体。
func TestSQLiteInteg_NewApi_WhereAllAnyNone(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	tests := []struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}{
		{"WhereAll", func(b *Builder) *Builder {
			return b.Table("users").WhereAll(func(q *Builder) {
				q.Where("status", "=", "active").Where("age", ">", 26)
			})
		}, 2}, // bob、diana
		{"WhereAny", func(b *Builder) *Builder {
			return b.Table("users").WhereAny(func(q *Builder) {
				q.Where("name", "=", "alice").Where("age", "=", 35)
			})
		}, 2}, // alice、charlie
		{"WhereNone", func(b *Builder) *Builder {
			return b.Table("users").WhereNone(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, 2}, // charlie、eve
		{"OrWhereAll", func(b *Builder) *Builder {
			return b.Table("users").Where("id", "=", 0).OrWhereAll(func(q *Builder) {
				q.Where("status", "=", "active").Where("age", ">", 26)
			})
		}, 2},
		{"OrWhereAny", func(b *Builder) *Builder {
			return b.Table("users").Where("name", "=", "alice").OrWhereAny(func(q *Builder) {
				q.Where("age", "=", 35)
			})
		}, 2}, // alice、charlie
		{"OrWhereNone", func(b *Builder) *Builder {
			return b.Table("users").Where("id", "=", 0).OrWhereNone(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// TestSQLiteInteg_NewApi_WhereExistsBuilder 验证 WhereExists 直接传 *Builder 及 Or 变体。
func TestSQLiteInteg_NewApi_WhereExistsBuilder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// 子查询：存在金额 > 100 订单的用户
	sub := func() *Builder {
		return db.Builder().Table("orders").
			WhereRaw("orders.user_id = users.id").
			Where("amount", ">", 100)
	}

	count, err := db.Builder().Table("users").WhereExists(sub()).Count(context.Background())
	assertNoError(t, err)
	if count != 3 { // alice(120)、bob(200)、diana(150)
		t.Errorf("WhereExists(*Builder): expected 3, got %d", count)
	}

	count, err = db.Builder().Table("users").WhereNotExists(sub()).Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // charlie、eve
		t.Errorf("WhereNotExists(*Builder): expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("id", "=", 5).
		OrWhereExists(sub()).
		Count(context.Background())
	assertNoError(t, err)
	if count != 4 { // eve + alice/bob/diana
		t.Errorf("OrWhereExists: expected 4, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("id", "=", 1).
		OrWhereNotExists(sub()).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 { // alice + charlie/eve
		t.Errorf("OrWhereNotExists: expected 3, got %d", count)
	}

	// 回调形式保持兼容
	count, err = db.Builder().Table("users").WhereExists(func(q *Builder) {
		q.Table("orders").WhereRaw("orders.user_id = users.id")
	}).Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("WhereExists callback: expected 4, got %d", count)
	}

	// 非法 sub 类型：编译期报错
	_, _, err = db.Builder().Table("users").WhereExists(123).ToSelect()
	if !errors.Is(err, ErrInvalidSubQuery) {
		t.Errorf("expected ErrInvalidSubQuery, got %v", err)
	}
}

// TestSQLiteInteg_LikeCaseSensitiveExpression 覆盖 SQLite 大小写敏感 Like
// 退化为 GLOB 且值为 Expression 的内联分支。
// （原 crossDialect 方言特有用例归位。）
func TestSQLiteInteg_LikeCaseSensitiveExpression(t *testing.T) {
	dao := openSQLiteTestDB(t)
	crossDialectSetupScanTable(t, dao)
	ctx := context.Background()

	count, err := dao.Builder().Table("cross_dialect_scan").
		WhereLike("name", NewExpression("'abc'"), true).Count(ctx)
	if err != nil {
		t.Fatalf("GLOB Expression 查询不应报错: %v", err)
	}
	if count != 1 {
		t.Fatalf("GLOB 'abc' 应命中 1 行，实际 %d", count)
	}
}
