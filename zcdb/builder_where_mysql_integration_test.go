// 本文件为 MySQL 集成测试——Where 系列条件构造。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "github.com/go-sql-driver/mysql"
	"testing"
	"time"
)

// TestMySQLInteg_SelectWhereBasic 验证基础 WHERE 等值条件。
func TestMySQLInteg_SelectWhereBasic(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWithWhere 验证多条件 AND WHERE 组合。
func TestMySQLInteg_SelectWithWhere(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereOr 验证 OR 条件组合。
func TestMySQLInteg_SelectWhereOr(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereIn 验证 WHERE IN 条件。
func TestMySQLInteg_SelectWhereIn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereNotIn 验证 WHERE NOT IN 条件。
func TestMySQLInteg_SelectWhereNotIn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereNull 验证 WHERE IS NULL 条件。
func TestMySQLInteg_SelectWhereNull(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereNotNull 验证 WHERE IS NOT NULL 条件。
func TestMySQLInteg_SelectWhereNotNull(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereBetween 验证 WHERE BETWEEN 范围条件。
func TestMySQLInteg_SelectWhereBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereNotBetween 验证 WHERE NOT BETWEEN 条件。
func TestMySQLInteg_SelectWhereNotBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereNested 验证嵌套 WHERE 条件组（括号分组）。
func TestMySQLInteg_SelectWhereNested(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectWhereRaw 验证原始 WHERE 表达式（WhereRaw）。
func TestMySQLInteg_SelectWhereRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_WhereSub 验证 WHERE 子查询比较：age > (SELECT AVG(age) ...)。
func TestMySQLInteg_WhereSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_WhereInSub 验证 WHERE IN 子查询。
func TestMySQLInteg_WhereInSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_WhereNotInSub 验证 WHERE NOT IN 子查询。
func TestMySQLInteg_WhereNotInSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_WhereExists 验证 WHERE EXISTS 子查询。
func TestMySQLInteg_WhereExists(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				Select("orders.user_id").
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

// TestMySQLInteg_WhereNotExists 验证 WHERE NOT EXISTS 子查询：只保留在 orders 表中不存在关联记录的用户。
func TestMySQLInteg_WhereNotExists(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_WhereLike 验证 LIKE 模糊匹配。
func TestMySQLInteg_WhereLike(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_WhereNotLike 验证 NOT LIKE 排除匹配。
func TestMySQLInteg_WhereNotLike(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_DeleteWithWhere 验证带条件删除。
func TestMySQLInteg_DeleteWithWhere(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").Where("id", "=", 1).Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 4 {
		t.Errorf("expected 4 remaining users, got %d", count)
	}
}

// TestMySQLInteg_DeleteWithoutWhere 验证无 WHERE 条件的 Delete 被拒绝（防误操作全表删除）。
func TestMySQLInteg_DeleteWithoutWhere(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_UpdateWithoutWhere 验证无 WHERE 条件的 Update 被拒绝（防误操作全表更新）。
func TestMySQLInteg_UpdateWithoutWhere(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_OrWhereRaw 验证 OrWhereRaw：原始 SQL OR 条件与绑定参数。
func TestMySQLInteg_OrWhereRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		Where("status", "=", "active").
		OrWhereRaw("age > ?", 30).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// active: alice, bob, diana (3); age>30: charlie(35) → 4
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_OrWhereNested 验证 OrWhereNested：OR 连接嵌套条件组。
func TestMySQLInteg_OrWhereNested(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
	// active: alice, bob, diana (3); charlie(age=35, name=charlie) → 4
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_OrWhereSub 验证 OrWhereSub：OR 连接子查询比较条件。
func TestMySQLInteg_OrWhereSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_OrWhereLike 验证 OrWhereLike：OR 连接 LIKE 模糊匹配。
func TestMySQLInteg_OrWhereLike(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_Complex_WhereExistsMultiJoinGroupBy 验证 WHERE EXISTS关联子查询 + 多表JOIN + GROUP BY。
// SQL: SELECT users.name, COUNT(orders.id) AS order_count
//
//	FROM users
//	INNER JOIN orders ON users.id = orders.user_id
//	LEFT JOIN profiles ON users.id = profiles.user_id
//	WHERE EXISTS (SELECT 1 FROM orders o2 WHERE o2.user_id = users.id
//	              GROUP BY o2.user_id HAVING COUNT(*) >= 2)
//	GROUP BY users.id, users.name
//	ORDER BY users.name ASC
//
// 预期：alice(2), bob(2) — 只有这两位有 ≥2 笔订单
func TestMySQLInteg_Complex_WhereExistsMultiJoinGroupBy(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)
	setupMySQLProfilesTable(t, db)

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

// TestMySQLInteg_WhereLikeExpression 验证 MySQL 上 WhereLike 传入 Expression 的真实执行：
// Expression 直接内嵌为原始 SQL（无占位符、无绑定参数），SQL 语法正确且结果正确。
func TestMySQLInteg_WhereLikeExpression(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 编译层面：Expression 内嵌，无占位符、无绑定参数
	sqlStr, args, err := db.Builder().Table("users").WhereLike("name", NewExpression("'%a%'")).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `name` LIKE '%a%'", sqlStr)
	assertArgs(t, nil, args)

	// 执行层面：alice/charlie/diana 名字含 a → 3 行
	count, err := db.Builder().Table("users").WhereLike("name", NewExpression("'%a%'")).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("LIKE '%%a%%' count: expected 3, got %d", count)
	}
}

// TestMySQLInteg_WhereNotLikeExpression 验证 MySQL 上 WhereNotLike 传入 Expression 的真实执行。
func TestMySQLInteg_WhereNotLikeExpression(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 执行层面：bob/eve 名字不含 a → 2 行
	count, err := db.Builder().Table("users").WhereNotLike("name", NewExpression("'%a%'")).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Fatalf("NOT LIKE '%%a%%' count: expected 2, got %d", count)
	}
}

// TestMySQLInteg_WhereRawExpression 验证 WhereRaw 绑定参数含 Expression 时真实执行。
func TestMySQLInteg_WhereRawExpression(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_MixedAndOrLeadingBoolean
// 编译层首个条件不输出前置 and，混合 AND/OR 连接执行结果正确。
func TestMySQLInteg_MixedAndOrLeadingBoolean(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_ExpressionValueNotBound
// Where/WhereRaw 的 Expression 值直接内联，不产生绑定参数。
func TestMySQLInteg_ExpressionValueNotBound(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_WhereInEmptyScalar
// zcdb WhereIn 入参为 []any 强类型，传标量在编译期即被拒绝（无法构造运行时异常）；
// 空切片语义：IN 空集等价 0=1 返回空，NOT IN 空集等价 1=1 返回全量。
func TestMySQLInteg_WhereInEmptyScalar(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_WhereSubInvalidOperator
// 子查询运算符不在白名单内时返回 ErrInvalidOperator。
func TestMySQLInteg_WhereSubInvalidOperator(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_DateWhere 验证日期 where 用 WhereRaw 手工构造
// （date/day/month/year/time 函数）。
func TestMySQLInteg_DateWhere(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE datetime_test (
		id            INT AUTO_INCREMENT PRIMARY KEY,
		date_val      DATE,
		time_val      TIME,
		datetime_val  DATETIME,
		timestamp_val TIMESTAMP NULL DEFAULT NULL,
		year_val      YEAR
	)`)
	mustExec(t, db, `INSERT INTO datetime_test (date_val, time_val, datetime_val, timestamp_val, year_val) VALUES
		('2024-06-15', '14:30:00', '2024-06-15 14:30:00', '2024-06-15 14:30:00', 2024),
		('2023-12-25', '08:05:00', '2023-12-25 08:05:00', '2023-12-25 08:05:00', 2023),
		('2024-06-01', '23:59:59', '2024-06-01 23:59:59', '2024-06-01 23:59:59', 2024)`)

	// whereDate: date(col) = ?
	count, err := db.Builder().Table("datetime_test").
		WhereRaw("date(datetime_val) = ?", "2024-06-15").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDate Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDate: expected 1, got %d", count)
	}

	// whereDay: day(col) = ?
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("day(datetime_val) = ?", 15).
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDay Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDay: expected 1, got %d", count)
	}

	// whereMonth: month(col) = ?（两行 6 月）
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("month(datetime_val) = ?", 6).
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereMonth Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("whereMonth: expected 2, got %d", count)
	}

	// whereYear: year(col) = ?（两行 2024）
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("year(datetime_val) = ?", 2024).
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereYear Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("whereYear: expected 2, got %d", count)
	}

	// whereTime: time(col) = ?
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("time(datetime_val) = ?", "14:30:00").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereTime Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereTime: expected 1, got %d", count)
	}
}

// TestMySQLInteg_Fulltext 验证全文检索用 WhereRaw 构造
// （match ... against 三种模式）。
func TestMySQLInteg_Fulltext(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE articles (
		id   INT AUTO_INCREMENT PRIMARY KEY,
		body TEXT,
		FULLTEXT KEY ft_body (body)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO articles (body) VALUES
		('The quick brown fox jumps over the lazy dog'),
		('A quick brown rabbit runs fast'),
		('Slow green turtle walks slowly')`)

	// natural language mode
	count, err := db.Builder().Table("articles").
		WhereRaw("match (body) against (? in natural language mode)", "quick").
		Count(context.Background())
	if err != nil {
		t.Fatalf("natural Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("natural: expected 2, got %d", count)
	}

	// boolean mode：+quick +fox 两词都必须出现
	count, err = db.Builder().Table("articles").
		WhereRaw("match (body) against (? in boolean mode)", "+quick +fox").
		Count(context.Background())
	if err != nil {
		t.Fatalf("boolean Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("boolean: expected 1, got %d", count)
	}

	// query expansion
	count, err = db.Builder().Table("articles").
		WhereRaw("match (body) against (? with query expansion)", "quick").
		Count(context.Background())
	if err != nil {
		t.Fatalf("query expansion Count error: %v", err)
	}
	if count < 2 {
		t.Errorf("query expansion: expected >= 2, got %d", count)
	}
}

// TestMySQLInteg_JsonWhereNull 验证 JSON 空值条件用 WhereRaw 构造
// （is null OR json_type = 'NULL' 双条件），含 Expression 变体。
func TestMySQLInteg_JsonWhereNull(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		json_val JSON
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('{"name":"alice","age":25}'),
		('{"name":"bob","age":null}'),
		('{}')`)

	// is null：age 为 JSON null 或不存在 → 2 行（json_type 单参，用 -> 操作符传提取表达式）
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("json_val->'$.age' is null or json_type(json_val->'$.age') = 'NULL'").
		Count(context.Background())
	if err != nil {
		t.Fatalf("is null Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("is null: expected 2, got %d", count)
	}

	// is not null
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_val->'$.age' is not null and json_type(json_val->'$.age') != 'NULL'").
		Count(context.Background())
	if err != nil {
		t.Fatalf("is not null Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("is not null: expected 1, got %d", count)
	}

	// Expression 变体：路径用 Expression 内联不产生绑定
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_val->? is null or json_type(json_val->?) = 'NULL'",
			NewExpression("'$.age'"), NewExpression("'$.age'")).Count(context.Background())
	if err != nil {
		t.Fatalf("Expression Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("Expression: expected 2, got %d", count)
	}
}

// TestMySQLInteg_JsonContainsOverlaps 验证 JSON 包含/重叠查询用 WhereRaw 构造
// （json_contains/json_overlaps）。
func TestMySQLInteg_JsonContainsOverlaps(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		json_val JSON
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('["x","y"]'),
		('["z"]')`)

	// contains：数组含 "x"（场景列本身为数组）
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("json_contains(json_val, ?)", `"x"`).
		Count(context.Background())
	if err != nil {
		t.Fatalf("contains Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("contains: expected 1, got %d", count)
	}

	// overlaps：tags 与 "z" 有交集
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_overlaps(json_val, ?)", `"z"`).
		Count(context.Background())
	if err != nil {
		t.Fatalf("overlaps Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("overlaps: expected 1, got %d", count)
	}

	// doesntContain
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("not json_contains(json_val, ?)", `"x"`).
		Count(context.Background())
	if err != nil {
		t.Fatalf("doesntContain Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("doesntContain: expected 1, got %d", count)
	}

	// doesntOverlap
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("not json_overlaps(json_val, ?)", `"z"`).
		Count(context.Background())
	if err != nil {
		t.Fatalf("doesntOverlap Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("doesntOverlap: expected 1, got %d", count)
	}
}

// TestMySQLInteg_JsonKeyLength 验证 JSON 键存在与长度查询用 WhereRaw 构造
// （json_contains_path/json_length）。
func TestMySQLInteg_JsonKeyLength(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		json_val JSON
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('{"name":"alice","tags":["x","y"]}'),
		('{"name":"bob","tags":["z"]}')`)

	// containsKey：json_contains_path(col, 'one', '$.name')
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("json_contains_path(json_val, 'one', '$.name')").
		Count(context.Background())
	if err != nil {
		t.Fatalf("containsKey Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("containsKey: expected 2, got %d", count)
	}

	// length：json_length(col, '$.tags') = ?
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_length(json_val, '$.tags') = ?", 2).
		Count(context.Background())
	if err != nil {
		t.Fatalf("length Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("length: expected 1, got %d", count)
	}
}

// TestMySQLInteg_SoundsLike 验证 sounds like 条件用 WhereRaw 构造
// 。
func TestMySQLInteg_SoundsLike(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// SOUNDEX('alice') = SOUNDEX('Alice') = A420 → 仅 alice 匹配
	count, err := db.Builder().Table("users").
		WhereRaw("name sounds like ?", "Alice").
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

// TestMySQLInteg_Bitwise 验证位运算条件用 WhereRaw/Expression 组合
// （(flags & ?) = ?）。
func TestMySQLInteg_Bitwise(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE bit_test (
		id    INT AUTO_INCREMENT PRIMARY KEY,
		flags INT NOT NULL
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

// TestMySQLInteg_RowValues 验证行值比较用 WhereRaw 构造
// （(a, b) >= (?, ?)）。
func TestMySQLInteg_RowValues(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLOrdersTable(t, db)

	// (user_id, amount) >= (2, 100)：user2-200、user3-30、user4-150 → 3 行
	var rows []struct {
		ID     int64   `db:"id"`
		UserID int64   `db:"user_id"`
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

// TestMySQLInteg_ArrayWhereColumn 验证多列条件括号分组用 WhereRaw 构造
// （(a >= ? AND b <= ?)）。
func TestMySQLInteg_ArrayWhereColumn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_NewApi_WhereDate 验证 WhereDate 在 MySQL 上编译为 date(col) = ? 并真实执行。
func TestMySQLInteg_NewApi_WhereDate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLEventsTable(t, db)

	// 编译形态断言
	sqlStr, args, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `events` WHERE date(`happened_at`) = ?", sqlStr)
	assertArgs(t, []any{"2024-06-15"}, args)

	count, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate: expected 2, got %d", count)
	}
}

// TestMySQLInteg_NewApi_WhereDate_Time 验证 WhereDate 传 time.Time 时在 MySQL 上编译与执行均正确。
func TestMySQLInteg_NewApi_WhereDate_Time(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLEventsTable(t, db)

	dt := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)

	sqlStr, args, err := db.Builder().Table("events").WhereDate("happened_at", dt).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `events` WHERE date(`happened_at`) = ?", sqlStr)
	assertArgs(t, []any{"2024-06-15"}, args)

	count, err := db.Builder().Table("events").WhereDate("happened_at", dt).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate(time.Time): expected 2, got %d", count)
	}
}

// TestMySQLInteg_NewApi_SugarAndNil 验证 Where 两参简写与 nil 特判在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_SugarAndNil(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	count, err := db.Builder().Table("users").Where("age", 25).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("Where shorthand: expected 1, got %d", count)
	}

	count, err = db.Builder().Table("users").Where("age", "=", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("Where =nil: expected 1, got %d", count)
	}

	count, err = db.Builder().Table("users").Where("age", "<>", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("Where <>nil: expected 4, got %d", count)
	}
}

// TestMySQLInteg_NewApi_NullSafe 验证空安全比较在 MySQL 上编译为 <=> / NOT <=> 并真实执行。
func TestMySQLInteg_NewApi_NullSafe(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("users").WhereNullSafeEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `age` <=> ?", sqlStr)
	sqlStr, _, err = db.Builder().Table("users").WhereNullSafeNotEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE NOT `age` <=> ?", sqlStr)

	count, err := db.Builder().Table("users").WhereNullSafeEquals("age", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 1 { // eve
		t.Errorf("NullSafe =nil: expected 1, got %d", count)
	}
	count, err = db.Builder().Table("users").WhereNullSafeNotEquals("age", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("NullSafeNot =nil: expected 4, got %d", count)
	}
}

// TestMySQLInteg_NewApi_WhereLikeCaseSensitive 验证 WhereLike 区分大小写编译为 BINARY col LIKE 并真实执行。
func TestMySQLInteg_NewApi_WhereLikeCaseSensitive(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLNamesCsTable(t, db)

	// 默认不区分大小写：alice + Alice
	count, err := db.Builder().Table("names_cs").WhereLike("name", "%lic%").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("default like: expected 2, got %d", count)
	}

	// 区分大小写编译形态
	sqlStr, _, err := db.Builder().Table("names_cs").WhereLike("name", "%lic%", true).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `names_cs` WHERE BINARY `name` LIKE ?", sqlStr)

	// 区分大小写真实执行：a% 仅命中 'alice'（'Alice' 大写开头不匹配）
	count, err = db.Builder().Table("names_cs").WhereLike("name", "a%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("binary like a%%: expected 1, got %d", count)
	}
	count, err = db.Builder().Table("names_cs").WhereLike("name", "A%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("binary like A%%: expected 1, got %d", count)
	}
}

// TestMySQLInteg_NewApi_WhereNotAllAnyNone 验证 WhereNot 与 All/Any/None 在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_WhereNotAllAnyNone(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	count, err := db.Builder().Table("users").
		WhereNot(func(q *Builder) { q.Where("status", "=", "active") }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereNot: expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		WhereAll(func(q *Builder) { q.Where("status", "=", "active").Where("age", ">", 26) }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereAll: expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		WhereNone(func(q *Builder) { q.Where("status", "=", "active") }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereNone: expected 2, got %d", count)
	}
}

// TestMySQLInteg_NewApi_WhereExistsBuilder 验证 WhereExists 直传 *Builder 在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_WhereExistsBuilder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sub := db.Builder().Table("orders").
		WhereRaw("orders.user_id = users.id").
		Where("amount", ">", 100)
	count, err := db.Builder().Table("users").WhereExists(sub).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Errorf("WhereExists(*Builder): expected 3, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("id", "=", 5).
		OrWhereNotExists(sub).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // eve(id=5) + charlie（无 > 100 订单）
		t.Errorf("OrWhereNotExists: expected 2, got %d", count)
	}
}
