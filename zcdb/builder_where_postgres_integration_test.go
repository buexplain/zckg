// 本文件为 PostgreSQL 集成测试——Where 系列条件构造。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "github.com/lib/pq"
	"strings"
	"testing"
)

// TestPgInteg_SelectWhereBasic 验证基础 WHERE 等值条件。
func TestPgInteg_SelectWhereBasic(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWithWhere 验证多条件 AND WHERE：多个 Where 调用生成 AND 连接。
func TestPgInteg_SelectWithWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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
	// age>20 AND status=active: alice(25), diana(28), bob(30) → 3 rows
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_SelectWhereOr 验证 OR 条件组合。
func TestPgInteg_SelectWhereOr(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereIn 验证 WHERE IN 条件。
func TestPgInteg_SelectWhereIn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereNotIn 验证 WHERE NOT IN 条件。
func TestPgInteg_SelectWhereNotIn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereNull 验证 WHERE IS NULL 条件。
func TestPgInteg_SelectWhereNull(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereNotNull 验证 WHERE IS NOT NULL 条件。
func TestPgInteg_SelectWhereNotNull(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereBetween 验证 WHERE BETWEEN 范围条件。
func TestPgInteg_SelectWhereBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereNotBetween 验证 WHERE NOT BETWEEN 条件。
func TestPgInteg_SelectWhereNotBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereNested 验证嵌套 WHERE 条件组（括号分组）。
func TestPgInteg_SelectWhereNested(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectWhereColumn 验证列与列比较：WhereColumn 不使用占位符，两侧均为列名。
func TestPgInteg_SelectWhereColumn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	type row struct {
		ProductId string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("product").
		WhereColumn("amount", ">", "id").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// amount > id: Book(50>1), Laptop(120>2), Phone(80>3), TV(200>4), Pen(30>5), Camera(150>6) → all 6
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// TestPgInteg_WhereLike 验证 LIKE 模糊匹配。
func TestPgInteg_WhereLike(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereLike("name", "%li%").
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// alice, charlie → 2 rows
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_SelectWhereRaw 验证原始 WHERE 表达式（WhereRaw）。
func TestPgInteg_SelectWhereRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").WhereRaw("age > $1 AND name LIKE $2", 28, "b%").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "bob" {
		t.Errorf("expected [bob], got %v", rows)
	}
}

// TestPgInteg_WhereSub 验证 WHERE 子查询比较：age > (SELECT AVG(age) ...)。
func TestPgInteg_WhereSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_WhereInSub 验证 WHERE IN 子查询。
func TestPgInteg_WhereInSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_WhereNotInSub 验证 WHERE NOT IN 子查询。
func TestPgInteg_WhereNotInSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id").Where("amount", ">", 100)
		}).
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// orders with amount > 100: user_id 1,2,4 → NOT IN: charlie(3), eve(5)
	if len(rows) != 2 {
		t.Errorf("expected 2 users, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_WhereExists 验证 WHERE EXISTS 子查询。
func TestPgInteg_WhereExists(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_WhereNotExists 验证 WHERE NOT EXISTS 子查询：只保留在 orders 表中不存在关联记录的用户。
func TestPgInteg_WhereNotExists(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_DeleteWithWhere 验证带条件删除。
func TestPgInteg_DeleteWithWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	_, err := db.Builder().Table("users").Where("id", "=", 1).Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 4 {
		t.Errorf("expected 4 remaining users, got %d", count)
	}
}

// TestPgInteg_DeleteMultipleWhere 验证多条件 DELETE：多个 WHERE 条件用 AND 连接。
func TestPgInteg_DeleteMultipleWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	_, err := db.Builder().Table("users").
		Where("status", "=", "inactive").
		Where("age", "<", 30).
		Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// eve is inactive but age=NULL (not < 30), so no rows deleted
	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 5 {
		t.Errorf("expected 5 users (no deletion), got %d", count)
	}
}

// TestPgInteg_DeleteWithoutWhere 验证无 WHERE 条件的 Delete 被拒绝（防误操作全表删除）。
func TestPgInteg_DeleteWithoutWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UpdateWithoutWhere 验证无 WHERE 条件的 Update 被拒绝（防误操作全表更新）。
func TestPgInteg_UpdateWithoutWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_OrWhereRaw 验证 OrWhereRaw：原始 SQL OR 条件与绑定参数。
func TestPgInteg_OrWhereRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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
	// active: alice, bob, diana (3); age>30: charlie(35) → 4
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_OrWhereNested 验证 OrWhereNested：OR 连接嵌套条件组。
func TestPgInteg_OrWhereNested(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_OrWhereSub 验证 OrWhereSub：OR 连接子查询比较条件。
func TestPgInteg_OrWhereSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_OrWhereLike 验证 OrWhereLike：OR 连接 LIKE 模糊匹配。
func TestPgInteg_OrWhereLike(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Complex_WhereExistsMultiJoinGroupBy 验证 WHERE EXISTS关联子查询 + 多表JOIN + GROUP BY。
// 预期：alice(2), bob(2)
func TestPgInteg_Complex_WhereExistsMultiJoinGroupBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)
	setupPgProfilesTable(t, db)

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

// TestPgInteg_WhereNotLike 验证 WHERE NOT LIKE：排除匹配模式的行。
func TestPgInteg_WhereNotLike(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_WhereLikeExpression 验证 PostgreSQL 上 WhereLike 传入 Expression 的真实执行：
// Expression 直接内嵌为原始 SQL（无占位符、无绑定参数），SQL 语法正确且结果正确。
// 注意：§11 后 PG 默认编译为 ILIKE（不区分大小写），测试数据全小写，'%a%' 命中 alice/charlie/diana。
func TestPgInteg_WhereLikeExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// 编译层面：Expression 内嵌，无占位符、无绑定参数
	sql, args, err := db.Builder().Table("users").WhereLike("name", NewExpression("'%a%'")).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "name" ILIKE '%a%'`, sql)
	assertArgs(t, nil, args)

	// 执行层面：alice/charlie/diana 名字含 a → 3 行
	count, err := db.Builder().Table("users").WhereLike("name", NewExpression("'%a%'")).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("LIKE '%%a%%' count: expected 3, got %d", count)
	}
}

// TestPgInteg_WhereNotLikeExpression 验证 PostgreSQL 上 WhereNotLike 传入 Expression 的真实执行。
func TestPgInteg_WhereNotLikeExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// 执行层面：bob/eve 名字不含 a → 2 行
	count, err := db.Builder().Table("users").WhereNotLike("name", NewExpression("'%a%'")).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Fatalf("NOT LIKE '%%a%%' count: expected 2, got %d", count)
	}
}

// TestPgInteg_WhereRawExpression 验证 WhereRaw 绑定参数含 Expression 时真实执行。
func TestPgInteg_WhereRawExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_MixedAndOrLeadingBoolean
// 编译层首个条件不输出前置 and，混合 AND/OR 连接执行结果正确。
func TestPgInteg_MixedAndOrLeadingBoolean(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_ExpressionValueNotBound
// Where/WhereRaw 的 Expression 值直接内联，不产生绑定参数。
func TestPgInteg_ExpressionValueNotBound(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_WhereInEmptyScalar
// zcdb WhereIn 入参为 []any 强类型，传标量在编译期即被拒绝（无法构造运行时异常）；
// 空切片语义：IN 空集等价 0=1 返回空，NOT IN 空集等价 1=1 返回全量。
func TestPgInteg_WhereInEmptyScalar(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_WhereSubInvalidOperator
// 子查询运算符不在白名单内时返回 ErrInvalidOperator。
func TestPgInteg_WhereSubInvalidOperator(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_DateWhere 验证日期 where 用 WhereRaw 手工构造
// （::date / extract / ::time），含 JSON 列变体。
func TestPgInteg_DateWhere(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE datetime_test (
		id       SERIAL PRIMARY KEY,
		date_val DATE,
		time_val TIME,
		ts_val   TIMESTAMP
	)`)
	mustExec(t, db, `INSERT INTO datetime_test (date_val, time_val, ts_val) VALUES
		('2024-06-15', '14:30:00', '2024-06-15 14:30:00'),
		('2023-12-25', '08:05:00', '2023-12-25 08:05:00'),
		('2024-06-01', '23:59:59', '2024-06-01 23:59:59')`)

	// whereDate: col::date = ?
	count, err := db.Builder().Table("datetime_test").
		WhereRaw("ts_val::date = ?", "2024-06-15").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDate Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDate: expected 1, got %d", count)
	}

	// whereDay: extract(day from col) = ?
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("extract(day from ts_val) = ?", 15).
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDay Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDay: expected 1, got %d", count)
	}

	// whereMonth: extract(month from col) = ?（两行 6 月）
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("extract(month from ts_val) = ?", 6).
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereMonth Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("whereMonth: expected 2, got %d", count)
	}

	// whereYear: extract(year from col) = ?（两行 2024）
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("extract(year from ts_val) = ?", 2024).
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereYear Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("whereYear: expected 2, got %d", count)
	}

	// whereTime: col::time = ?
	count, err = db.Builder().Table("datetime_test").
		WhereRaw("ts_val::time = ?", "14:30:00").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereTime Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereTime: expected 1, got %d", count)
	}

	// JSON 列变体：whereDate WithJson（(col->>'field')::date = ?）
	mustExec(t, db, `CREATE TABLE json_date_test (
		id   SERIAL PRIMARY KEY,
		info JSONB
	)`)
	mustExec(t, db, `INSERT INTO json_date_test (info) VALUES
		('{"birth":"1990-05-20"}'),
		('{"birth":"1985-01-01"}')`)
	count, err = db.Builder().Table("json_date_test").
		WhereRaw("(info->>'birth')::date = ?", "1990-05-20").
		Count(context.Background())
	if err != nil {
		t.Fatalf("whereDate json Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("whereDate json: expected 1, got %d", count)
	}
}

// TestPgInteg_Fulltext 验证全文检索用 WhereRaw 构造
// （to_tsvector @@ 五种模式）。
func TestPgInteg_Fulltext(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE articles (
		id       SERIAL PRIMARY KEY,
		body     TEXT,
		body_tsv TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', body)) STORED
	)`)
	mustExec(t, db, `INSERT INTO articles (body) VALUES
		('The quick brown fox jumps over the lazy dog'),
		('A quick brown rabbit runs fast'),
		('Slow green turtle walks slowly')`)

	// plainto_tsquery（自然语言）
	count, err := db.Builder().Table("articles").
		WhereRaw("to_tsvector('english', body) @@ plainto_tsquery('english', ?)", "quick").
		Count(context.Background())
	if err != nil {
		t.Fatalf("plain Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("plain: expected 2, got %d", count)
	}

	// phraseto_tsquery（短语）
	count, err = db.Builder().Table("articles").
		WhereRaw("to_tsvector('english', body) @@ phraseto_tsquery('english', ?)", "quick brown").
		Count(context.Background())
	if err != nil {
		t.Fatalf("phrase Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("phrase: expected 2, got %d", count)
	}

	// websearch_to_tsquery（多词默认 AND）
	count, err = db.Builder().Table("articles").
		WhereRaw("to_tsvector('english', body) @@ websearch_to_tsquery('english', ?)", "quick fox").
		Count(context.Background())
	if err != nil {
		t.Fatalf("websearch Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("websearch: expected 1, got %d", count)
	}

	// to_tsquery（raw 表达式）
	count, err = db.Builder().Table("articles").
		WhereRaw("to_tsvector('english', body) @@ to_tsquery('english', ?)", "quick & fox").
		Count(context.Background())
	if err != nil {
		t.Fatalf("raw Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("raw: expected 1, got %d", count)
	}

	// vector 列直用
	count, err = db.Builder().Table("articles").
		WhereRaw("body_tsv @@ plainto_tsquery('english', ?)", "rabbit").
		Count(context.Background())
	if err != nil {
		t.Fatalf("vector Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("vector: expected 1, got %d", count)
	}
}

// TestPgInteg_JsonContains 验证 JSON 包含查询用 WhereRaw 构造
// （@> 包含操作符）。
func TestPgInteg_JsonContains(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id        SERIAL PRIMARY KEY,
		jsonb_val JSONB
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (jsonb_val) VALUES
		('{"name":"alice","tags":["x","y"]}'),
		('{"name":"bob","tags":["z"]}')`)

	// contains: col @> ?::jsonb
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("jsonb_val @> ?::jsonb", `{"name":"alice"}`).
		Count(context.Background())
	if err != nil {
		t.Fatalf("contains Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("contains: expected 1, got %d", count)
	}

	// doesntContain
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("not (jsonb_val @> ?::jsonb)", `{"name":"alice"}`).
		Count(context.Background())
	if err != nil {
		t.Fatalf("doesntContain Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("doesntContain: expected 1, got %d", count)
	}
}

// TestPgInteg_JsonKeyLength 验证 JSON 键存在与长度查询用 WhereRaw 构造
// （? 键存在操作符（?? 转义）/ json_array_length）。
func TestPgInteg_JsonKeyLength(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id        SERIAL PRIMARY KEY,
		jsonb_val JSONB
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (jsonb_val) VALUES
		('{"name":"alice","tags":["x","y"]}'),
		('{"name":"bob","tags":["z"]}')`)

	// containsKey：jsonb ?? ?（?? 转义为字面 ? 操作符）
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("jsonb_val ?? ?", "name").
		Count(context.Background())
	if err != nil {
		t.Fatalf("containsKey Count error: %v", err)
	}
	if count != 2 {
		t.Errorf("containsKey: expected 2, got %d", count)
	}

	// length：jsonb_array_length(col->'tags') = ?（jsonb 需专用函数，json_array_length 仅收 json）
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("jsonb_array_length(jsonb_val->'tags') = ?", 2).
		Count(context.Background())
	if err != nil {
		t.Fatalf("length Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("length: expected 1, got %d", count)
	}
}

// TestPgInteg_QuestionMarkOperator 验证 PG ?? 操作符转义
// ：编译层 ?? → 字面 ?，执行层可用。
func TestPgInteg_QuestionMarkOperator(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id        SERIAL PRIMARY KEY,
		jsonb_val JSONB
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (jsonb_val) VALUES
		('{"name":"alice"}'),
		('{"age":30}')`)

	// 编译层："jsonb_val" ?? ? → "jsonb_val" ? $1，? 不被当作占位符
	sqlStr, args, err := db.Builder().Table("json_conv_test").
		WhereRaw("\"jsonb_val\" ?? ?", "name").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}
	if !strings.Contains(sqlStr, `"jsonb_val" ? $1`) {
		t.Errorf("expected literal ? operator, got: %s", sqlStr)
	}
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %d (%v)", len(args), args)
	}

	// 执行层：键存在
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("\"jsonb_val\" ?? ?", "name").
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1, got %d", count)
	}
}

// TestPgInteg_Bitwise 验证位运算条件用 WhereRaw/Expression 组合
// （& 位与、# 异或）。
func TestPgInteg_Bitwise(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE bit_test (
		id    SERIAL PRIMARY KEY,
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

	// 异或：flags # 6 = 4（仅 flags=2）
	count, err = db.Builder().Table("bit_test").
		WhereRaw("(flags # ?) = ?", 6, 4).
		Count(context.Background())
	if err != nil {
		t.Fatalf("bitwise # Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("bitwise #: expected 1, got %d", count)
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

// TestPgInteg_RowValues 验证行值比较用 WhereRaw 构造
// （(a, b) >= (?, ?)）。
func TestPgInteg_RowValues(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_ArrayWhereColumn 验证多列条件括号分组用 WhereRaw 构造
// （(a >= ? AND b <= ?)）。
func TestPgInteg_ArrayWhereColumn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_NewApi_WhereDate 验证 WhereDate 在 PG 上编译为 "col"::date = $1 并真实执行。
func TestPgInteg_NewApi_WhereDate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgNewApiTables(t, db)
	setupPgEventsTable(t, db)

	sqlStr, args, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "events" WHERE "happened_at"::date = $1`, sqlStr)
	assertArgs(t, []any{"2024-06-15"}, args)

	count, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate: expected 2, got %d", count)
	}
}

// TestPgInteg_NewApi_NullSafe 验证空安全比较在 PG 上编译为 IS [NOT] DISTINCT FROM 并真实执行。
func TestPgInteg_NewApi_NullSafe(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, _, err := db.Builder().Table("users").WhereNullSafeEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" IS NOT DISTINCT FROM $1`, sqlStr)
	sqlStr, _, err = db.Builder().Table("users").WhereNullSafeNotEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" IS DISTINCT FROM $1`, sqlStr)

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

// TestPgInteg_NewApi_WhereLikeCaseSensitive 验证 PG 默认 ILIKE（不区分）、第三参 true 编译为 LIKE（区分）。
func TestPgInteg_NewApi_WhereLikeCaseSensitive(t *testing.T) {
	db := openPgTestDB(t)
	setupPgNewApiTables(t, db)
	setupPgNamesCsTable(t, db)

	// 默认编译为 ILIKE
	sqlStr, _, err := db.Builder().Table("names_cs").WhereLike("name", "a%").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "names_cs" WHERE "name" ILIKE $1`, sqlStr)

	// 默认不区分大小写：alice + Alice
	count, err := db.Builder().Table("names_cs").WhereLike("name", "a%").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("ilike: expected 2, got %d", count)
	}

	// 区分大小写编译为 LIKE
	sqlStr, _, err = db.Builder().Table("names_cs").WhereLike("name", "a%", true).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "names_cs" WHERE "name" LIKE $1`, sqlStr)

	count, err = db.Builder().Table("names_cs").WhereLike("name", "a%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 { // 仅 'alice'
		t.Errorf("like case-sensitive a%%: expected 1, got %d", count)
	}
	count, err = db.Builder().Table("names_cs").WhereLike("name", "A%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 { // 仅 'Alice'
		t.Errorf("like case-sensitive A%%: expected 1, got %d", count)
	}
}

// TestPgInteg_NewApi_WhereExistsBuilder 验证 WhereExists 直传 *Builder 在 PG 上的 $N 编译与真实执行。
func TestPgInteg_NewApi_WhereExistsBuilder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sub := db.Builder().Table("orders").
		WhereRaw("orders.user_id = users.id").
		Where("amount", ">", 100)

	sqlStr, args, err := db.Builder().Table("users").WhereExists(sub).ToSelect()
	assertNoError(t, err)
	assertSQL(t,
		`SELECT * FROM "users" WHERE EXISTS (SELECT * FROM "orders" WHERE orders.user_id = users.id AND "amount" > $1)`,
		sqlStr)
	assertArgs(t, []any{100}, args)

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
	if count != 2 { // eve(id=5) + charlie
		t.Errorf("OrWhereNotExists: expected 2, got %d", count)
	}
}

// TestPgInteg_Bug_NotLikeCaseInsensitive 审查复现用例：
// PostgreSQL 下 WhereLike 默认编译为 ILIKE（不区分大小写），而 WhereNotLike 恒用
// NOT LIKE（区分大小写），两者不互补——同一 pattern 的命中集合与排除集合之和不等于全表。
// 预期：WhereNotLike 默认与 WhereLike 对称，编译为 NOT ILIKE；
// 区分大小写需求可用 WhereRaw 自行表达。
func TestPgInteg_Bug_NotLikeCaseInsensitive(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// 追加一行大写名字：小写 pattern '%frank%' 在大小写不敏感语义下应命中 FRANK
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES ('FRANK', 40, 'frank@test.com', 'active')`)

	// 基线：WhereLike 默认 ILIKE（不区分大小写），'%frank%' 命中 FRANK → 1
	count, err := db.Builder().Table("users").WhereLike("name", "%frank%").Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Fatalf("WhereLike ILIKE '%%frank%%': expected 1, got %d", count)
	}

	// 编译层面：WhereNotLike 默认应生成 NOT ILIKE 与 WhereLike 对称
	sqlStr, args, err := db.Builder().Table("users").WhereNotLike("name", "%frank%").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "name" NOT ILIKE $1`, sqlStr)
	assertArgs(t, []any{"%frank%"}, args)

	// 执行层面：全表 6 行，排除 FRANK → 5；
	// 修复前恒用 NOT LIKE（区分大小写）无法排除大写 FRANK，会得到 6，与 WhereLike 不互补
	count, err = db.Builder().Table("users").WhereNotLike("name", "%frank%").Count(context.Background())
	assertNoError(t, err)
	if count != 5 {
		t.Errorf("WhereNotLike default: expected 5, got %d", count)
	}
}
