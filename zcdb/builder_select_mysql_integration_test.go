// 本文件为 MySQL 集成测试——Select/SelectRaw/Distinct/子查询列选择。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
package zcdb

import (
	"context"
	_ "github.com/go-sql-driver/mysql"
	"testing"
)

// TestMySQLInteg_SelectAll 验证无条件全表查询：不设任何 WHERE，SELECT * 应返回所有行。
func TestMySQLInteg_SelectAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Id int `db:"id"`
	}
	var rows []row
	err := db.Builder().Table("users").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

// TestMySQLInteg_SelectColumns 验证指定列查询：仅选择 name 和 age 列，通过 WHERE 定位单行。
func TestMySQLInteg_SelectColumns(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name", "age").Where("id", "=", 1).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" || rows[0].Age != 25 {
		t.Errorf("expected alice/25, got %v", rows)
	}
}

// TestMySQLInteg_SelectDistinct 验证 DISTINCT 去重查询。
func TestMySQLInteg_SelectDistinct(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Status string `db:"status"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("status").Distinct().Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 distinct statuses, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_SelectRaw 验证 SelectRaw 原始表达式（COUNT(*)）。
func TestMySQLInteg_SelectRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

// TestMySQLInteg_TableSub 验证 FROM 子查询（派生表）。
func TestMySQLInteg_TableSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sub := db.Builder().Table("users").Select("name", "age").WhereNotNull("age")
	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().
		TableSub(sub, "sub").
		Select("name").
		Where("age", ">", 28).
		OrderBy("age", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", rows)
	}
}

// TestMySQLInteg_SelectSub 验证 SELECT 子句中的子查询。
func TestMySQLInteg_SelectSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").WhereRaw("`orders`.`user_id` = `users`.`id`")
	type row struct {
		Name        string `db:"name"`
		OrdersCount int    `db:"orders_count"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name").
		SelectSub(sub, "orders_count").
		Where("id", "=", 1).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" || rows[0].OrdersCount != 2 {
		t.Errorf("expected alice with 2 orders, got %v", rows)
	}
}

// TestMySQLInteg_Bug_SelectSubTableSubBindingOrder 验证 SELECT 子查询与 FROM 子查询同时含绑定参数时，
// 收集顺序与 SQL 占位符顺序一致（SELECT 子查询在前，FROM 子查询在后）。
func TestMySQLInteg_Bug_SelectSubTableSubBindingOrder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	// SELECT 子查询：统计 amount > 100 的订单数（标量，绑定 100），结果为 3 (120,200,150)
	scalarSub := db.Builder().Table("orders").SelectRaw("COUNT(*)").Where("amount", ">", 100)
	// FROM 子查询：age > 25 的用户（绑定 25），结果为 3 人 (bob,charlie,diana)
	tableSub := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name     string `db:"name"`
		BigCount int    `db:"big_count"`
	}
	var rows []row
	err := db.Builder().
		Select("name").
		SelectSub(scalarSub, "big_count").
		TableSub(tableSub, "t").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	// 绑定顺序错误时 age>100 会返回 0 行；正确时应为 3 行，每行 big_count=3
	if len(rows) != 3 {
		t.Fatalf("BUG: binding order wrong, expected 3 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.BigCount != 3 {
			t.Errorf("expected big_count=3 for %s, got %d", r.Name, r.BigCount)
		}
	}
}

// TestMySQLInteg_Complex_SelectSubWhereInSubNestedWhere 验证 SELECT子查询列 + WHERE IN子查询 + 嵌套WHERE。
// SQL: SELECT name, age,
//
//	(SELECT COUNT(*) FROM orders WHERE orders.user_id = users.id) AS order_count
//
// FROM users
// WHERE id IN (SELECT user_id FROM orders WHERE amount > 100)
// AND (age > 25 OR status = 'active')
// ORDER BY age DESC
// 预期：bob(30,2), diana(28,1), alice(25,2)
func TestMySQLInteg_Complex_SelectSubWhereInSubNestedWhere(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	// SELECT 子查询列：统计每个用户的订单数
	countSub := db.Builder().Table("orders").
		SelectRaw("COUNT(*)").
		WhereRaw("orders.user_id = users.id")

	// WHERE IN 子查询：有单笔金额 > 100 的订单的用户
	type row struct {
		Name       string `db:"name"`
		Age        int    `db:"age"`
		OrderCount int    `db:"order_count"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name", "age").
		SelectSub(countSub, "order_count").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id").Where("amount", ">", 100)
		}).
		WhereNested(func(b *Builder) {
			b.Where("age", ">", 25).OrWhere("status", "=", "active")
		}).
		OrderBy("age", "DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "bob" || rows[0].Age != 30 || rows[0].OrderCount != 2 {
		t.Errorf("row[0]: expected bob/30/2, got %v", rows[0])
	}
	if rows[1].Name != "diana" || rows[1].Age != 28 || rows[1].OrderCount != 1 {
		t.Errorf("row[1]: expected diana/28/1, got %v", rows[1])
	}
	if rows[2].Name != "alice" || rows[2].Age != 25 || rows[2].OrderCount != 2 {
		t.Errorf("row[2]: expected alice/25/2, got %v", rows[2])
	}
}

// TestMySQLInteg_Complex_MultiSubqueryCombination 验证多种子查询类型组合：
// WHERE NOT IN子查询 + WHERE EXISTS + JOIN + ORDER BY。
// 找出「没有个人档案但有订单」的用户，且至少有一笔订单金额 > 100。
// 预期：diana(有 Camera 150，无 profile)
func TestMySQLInteg_Complex_MultiSubqueryCombination(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)
	setupMySQLProfilesTable(t, db)

	// NOT IN 子查询：排除有 profile 的用户
	type row struct {
		Name    string  `db:"name"`
		Product string  `db:"product"`
		Amount  float64 `db:"amount"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("users.name", "orders.product", "orders.amount").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id")
		}).
		WhereNotInSub("users.id", func(sub *Builder) {
			sub.Table("profiles").Select("user_id")
		}).
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				SelectRaw("COUNT(*)").
				WhereRaw("orders.user_id = users.id").
				Where("orders.amount", ">", 100)
		}).
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// 没有 profile 的用户: diana(4), eve(5)
	// 其中有订单的: diana(Camera, 150)
	// 且有金额 > 100 的订单: diana ✓
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "diana" || rows[0].Product != "Camera" || rows[0].Amount != 150 {
		t.Errorf("expected diana/Camera/150, got %v", rows[0])
	}
}

// TestMySQLInteg_SelectReplacesColumns
// 多次 Select 为替换语义（后一次覆盖前一次）；Select("*") 恢复全列。
func TestMySQLInteg_SelectReplacesColumns(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 先 Select("name") 再 Select("age")：只有 age 列生效
	type ageRow struct {
		Age *int `db:"age"`
	}
	var rows []ageRow
	err := db.Builder().Table("users").Select("name").Select("age").
		Where("age", ">", 26).OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}

	// Select("*") 恢复全列：所有字段可扫描
	type fullRow struct {
		ID     int    `db:"id"`
		Name   string `db:"name"`
		Age    *int   `db:"age"`
		Email  string `db:"email"`
		Status string `db:"status"`
	}
	var all []fullRow
	err = db.Builder().Table("users").Select("name").Select("*").
		OrderBy("id", "ASC").Find(context.Background(), &all)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(all) != 5 || all[0].ID != 1 || all[0].Name != "alice" {
		t.Errorf("expected full rows, got %v", all)
	}
}

// TestMySQLInteg_SubSelectResetBindings
// 子查询中 Select("*") 为替换语义，无列/绑定残留。
func TestMySQLInteg_SubSelectResetBindings(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereExists(func(sub *Builder) {
			sub.Table("orders").Select("*").WhereColumn("orders.user_id", "=", "users.id")
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

// TestMySQLInteg_JsonSelectWhereOrder 验证 JSON 提取在 select/where/orderBy
// 中通过 SelectRaw/WhereRaw/OrderByRaw 组合（json_unquote/json_extract，含路径转义与布尔内联）。
func TestMySQLInteg_JsonSelectWhereOrder(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		json_val JSON
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('{"name":"alice","age":25,"address":{"city":"Shanghai"}}'),
		('{"name":"bob","age":30,"address":{"city":"Beijing"}}'),
		('{"name":"charlie","age":35,"address":{"city":"Shenzhen"}}'),
		('{"first name":"zoe","age":40,"is_active":true}')`)

	// select：json_unquote(json_extract(...)) AS name（zoe 无 name 键 → NULL）
	var names []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("json_conv_test").
		SelectRaw("json_unquote(json_extract(json_val, '$.name')) AS name").
		OrderBy("id", "ASC").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("Select Find error: %v", err)
	}
	if len(names) != 4 || names[0].Name != "alice" || names[2].Name != "charlie" || names[3].Name != "" {
		t.Errorf("select: expected [alice bob charlie <nil>], got %+v", names)
	}

	// where + orderBy：age > 28（zoe40、charlie35、bob30）且按 age 降序
	var rows []struct {
		Name string `db:"name"`
	}
	err = db.Builder().Table("json_conv_test").
		SelectRaw("json_unquote(json_extract(json_val, '$.name')) AS name").
		WhereRaw("json_extract(json_val, '$.age') > ?", 28).
		OrderByRaw("json_extract(json_val, '$.age') DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("where/orderBy Find error: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "" || rows[1].Name != "charlie" || rows[2].Name != "bob" {
		t.Errorf("where/orderBy: expected [<nil> charlie bob], got %+v", rows)
	}

	// 布尔 true 内联无绑定（对齐 testMySqlWrappingJsonWithBoolean）
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("json_extract(json_val, '$.is_active') = true").
		Count(context.Background())
	if err != nil {
		t.Fatalf("boolean Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("boolean: expected 1, got %d", count)
	}

	// 路径转义：键含空格的 $.\"first name\"
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_unquote(json_extract(json_val, '$.\"first name\"')) = ?", "zoe").
		Count(context.Background())
	if err != nil {
		t.Fatalf("path escaping Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("path escaping: expected 1, got %d", count)
	}
}

// TestMySQLInteg_NewApi_SelectSub 验证 SelectSub 标量子查询列在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_SelectSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	type userWithCount struct {
		ID         int64 `db:"id"`
		OrderCount int64 `db:"order_count"`
	}
	var rows []userWithCount
	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").WhereRaw("orders.user_id = users.id")
	err := db.Builder().Table("users").
		Select("id").
		SelectSub(sub, "order_count").
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	expected := []int64{2, 2, 1, 1, 0}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	for i, want := range expected {
		if rows[i].OrderCount != want {
			t.Errorf("row %d: expected %d, got %d", i, want, rows[i].OrderCount)
		}
	}
}
