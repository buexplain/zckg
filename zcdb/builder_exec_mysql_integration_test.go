// 本文件为 MySQL 集成测试——写操作执行（Insert/Update/Delete/Upsert/Truncate 等）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "github.com/go-sql-driver/mysql"
	"testing"
	"time"
)

// TestMySQLInteg_InsertSingle 验证单条结构体插入：传入单个结构体，生成并执行 INSERT，确认数据正确写入。
func TestMySQLInteg_InsertSingle(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), insertData{Name: "frank", Age: 40, Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}
	count, _ := db.Builder().Table("users").Where("name", "=", "frank").Count(context.Background())
	if count != 1 {
		t.Errorf("expected 1 row for frank, got %d", count)
	}
}

// TestMySQLInteg_InsertBatch 验证批量插入：传入结构体切片，生成并执行单条 INSERT 多 VALUES，确认所有行正确写入。
func TestMySQLInteg_InsertBatch(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	data := []insertData{
		{Name: "frank", Age: 40, Email: "frank@test.com"},
		{Name: "grace", Age: 22, Email: "grace@test.com"},
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), data)
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	count, _ := db.Builder().Table("users").WhereIn("name", []any{"frank", "grace"}).Count(context.Background())
	if count != 2 {
		t.Errorf("expected 2 rows, got %d", count)
	}
}

// TestMySQLInteg_InsertPartial 验证指针字段部分插入：nil 指针字段不参与 INSERT，对应列使用数据库默认值。
func TestMySQLInteg_InsertPartial(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name   *string `db:"name"`
		Age    *int    `db:"age"`
		Email  *string `db:"email"`
		Status *string `db:"status"`
	}
	name := "frank"
	age := 40
	email := "frank@test.com"
	// Status 为 nil，应被跳过，使用数据库默认值 'active'
	_, err := db.Builder().Table("users").Insert(context.Background(), insertData{Name: &name, Age: &age, Email: &email})
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	type row struct {
		Status string `db:"status"`
	}
	var rows []row
	_ = db.Builder().Table("users").Select("status").Where("name", "=", "frank").Find(context.Background(), &rows)
	if len(rows) != 1 || rows[0].Status != "active" {
		t.Errorf("expected default status 'active', got %v", rows)
	}
}

// TestMySQLInteg_InsertPtrAllNil 验证全 nil 指针插入：所有指针字段均为 nil 时返回 ErrNoFields 错误。
func TestMySQLInteg_InsertPtrAllNil(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertPtrData struct {
		Name  *string `db:"name"`
		Age   *int    `db:"age"`
		Email *string `db:"email"`
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), insertPtrData{})
	if err == nil {
		t.Fatalf("expected error for all-nil insert, got nil")
	}
}

// TestMySQLInteg_InsertBatchPtr 验证指针字段批量插入：以首行确定列，后续行 nil 字段传入 nil。
func TestMySQLInteg_InsertBatchPtr(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertPtrData struct {
		Name  *string `db:"name"`
		Age   *int    `db:"age"`
		Email *string `db:"email"`
	}
	n1, e1 := "frank", "frank@test.com"
	a1 := 40
	n2 := "grace"
	a2 := 22
	data := []insertPtrData{
		{Name: &n1, Age: &a1, Email: &e1},
		{Name: &n2, Age: &a2},
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), data)
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	// grace 的 email 应为 NULL
	var email *string
	err = db.Builder().Table("users").Select("email").Where("name", "=", "grace").Value(context.Background(), &email)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if email != nil {
		t.Errorf("expected NULL email for grace, got %s", *email)
	}
}

// TestMySQLInteg_InsertOrIgnore 验证 INSERT IGNORE：当 UNIQUE 约束冲突时不报错且不插入新行。
func TestMySQLInteg_InsertOrIgnore(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	_, err := db.Builder().Table("users").InsertOrIgnore(context.Background(), insertData{Name: "alice_dup", Age: 99, Email: "alice@test.com"})
	if err != nil {
		t.Fatalf("InsertOrIgnore error: %v", err)
	}

	count, _ := db.Builder().Table("users").Where("name", "=", "alice_dup").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 rows for alice_dup (ignored), got %d", count)
	}
	total, _ := db.Builder().Table("users").Count(context.Background())
	if total != 5 {
		t.Errorf("expected 5 total users, got %d", total)
	}
}

// TestMySQLInteg_UpdateBasic 验证基础 UPDATE。
func TestMySQLInteg_UpdateBasic(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updateData struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), updateData{Name: "alice_updated", Age: 26})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	type verifyRow struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []verifyRow
	_ = db.Builder().Table("users").Select("name", "age").Where("id", "=", 1).Find(context.Background(), &rows)
	if len(rows) != 1 || rows[0].Name != "alice_updated" || rows[0].Age != 26 {
		t.Errorf("expected alice_updated/26, got %v", rows)
	}
}

// TestMySQLInteg_UpdatePartial 验证指针字段部分更新：nil 指针字段不参与 SET。
func TestMySQLInteg_UpdatePartial(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updateData struct {
		Name   *string `db:"name"`
		Age    *int    `db:"age"`
		Status *string `db:"status"`
	}
	newName := "alice_partial"
	// Age=nil, Status=nil 为零值指针，应被跳过，仅更新 Name
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), updateData{Name: &newName})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	type verifyRow struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []verifyRow
	_ = db.Builder().Table("users").Select("name", "age").Where("id", "=", 1).Find(context.Background(), &rows)
	if len(rows) != 1 || rows[0].Name != "alice_partial" || rows[0].Age != 25 {
		t.Errorf("expected alice_partial/25, got %v", rows)
	}
}

// TestMySQLInteg_UpdateWithRaw 验证 Raw 表达式更新：字段值为 Raw(`age` + 10)。
func TestMySQLInteg_UpdateWithRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updateRaw struct {
		Age any `db:"age"`
	}
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), updateRaw{Age: NewExpression("`age` + 10")})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	var age int
	_ = db.Builder().Table("users").Select("age").Where("id", "=", 1).Value(context.Background(), &age)
	if age != 35 {
		t.Errorf("expected age=35, got %d", age)
	}
}

// TestMySQLInteg_UpdatePtrAllNil 验证全 nil 指针更新：所有指针字段均为 nil 时返回 ErrNoFields 错误。
func TestMySQLInteg_UpdatePtrAllNil(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updatePtrData struct {
		Name   *string `db:"name"`
		Age    *int    `db:"age"`
		Status *string `db:"status"`
	}
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), updatePtrData{})
	if err == nil {
		t.Fatalf("expected error for all-nil update, got nil")
	}
}

// TestMySQLInteg_DeleteWithMultipleConditions 验证多条件 DELETE + ORDER BY + LIMIT。
func TestMySQLInteg_DeleteWithMultipleConditions(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").
		Where("status", "=", "inactive").
		OrderBy("id", "ASC").
		Limit(1).
		Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	// inactive: charlie(id=3), eve(id=5); LIMIT 1 → only charlie deleted
	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 4 {
		t.Errorf("expected 4 remaining users, got %d", count)
	}
	// charlie should be gone
	count, _ = db.Builder().Table("users").Where("name", "=", "charlie").Count(context.Background())
	if count != 0 {
		t.Errorf("expected charlie deleted, got %d", count)
	}
}

// TestMySQLInteg_DeleteAll 验证 Force() 允许无条件全表删除。
func TestMySQLInteg_DeleteAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").Force().Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after delete all, got %d", count)
	}
}

// TestMySQLInteg_Upsert 验证 INSERT ... ON DUPLICATE KEY UPDATE。
func TestMySQLInteg_Upsert(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type upsertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	// 插入新行
	_, err := db.Builder().Table("users").Upsert(context.Background(),
		upsertData{Name: "frank", Age: 40, Email: "frank@test.com"},
		[]string{"email"},
		[]string{"name", "age"},
	)
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	count, _ := db.Builder().Table("users").Where("name", "=", "frank").Count(context.Background())
	if count != 1 {
		t.Errorf("expected frank inserted, got count=%d", count)
	}

	// 冲突更新
	_, err = db.Builder().Table("users").Upsert(context.Background(),
		upsertData{Name: "alice_upserted", Age: 99, Email: "alice@test.com"},
		[]string{"email"},
		[]string{"name", "age"},
	)
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	type verifyRow struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []verifyRow
	_ = db.Builder().Table("users").Select("name", "age").Where("email", "=", "alice@test.com").Find(context.Background(), &rows)
	if len(rows) != 1 || rows[0].Name != "alice_upserted" || rows[0].Age != 99 {
		t.Errorf("expected alice_upserted/99, got %v", rows)
	}
}

// TestMySQLInteg_UpsertBatch 验证批量 Upsert。
func TestMySQLInteg_UpsertBatch(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type upsertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	data := []upsertData{
		{Name: "frank", Age: 40, Email: "frank@test.com"},
		{Name: "alice_upserted", Age: 99, Email: "alice@test.com"},
	}
	_, err := db.Builder().Table("users").Upsert(context.Background(), data,
		[]string{"email"}, []string{"name", "age"})
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	// frank 新增
	count, _ := db.Builder().Table("users").Where("name", "=", "frank").Count(context.Background())
	if count != 1 {
		t.Errorf("expected frank inserted, got count=%d", count)
	}
	// alice 冲突更新
	type verifyRow struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []verifyRow
	_ = db.Builder().Table("users").Select("name", "age").Where("email", "=", "alice@test.com").Find(context.Background(), &rows)
	if len(rows) != 1 || rows[0].Name != "alice_upserted" || rows[0].Age != 99 {
		t.Errorf("expected alice_upserted/99, got %v", rows)
	}
}

// TestMySQLInteg_InsertUsing 验证 INSERT INTO ... SELECT 子查询插入。
func TestMySQLInteg_InsertUsing(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64),
		age  INT
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)

	sqlStr, args, err := db.Builder().
		Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	if err != nil {
		t.Fatalf("ToInsertUsing error: %v", err)
	}
	mustExec(t, db, sqlStr, args...)

	count, _ := db.Builder().Table("users_archive").Count(context.Background())
	if count != 3 {
		t.Errorf("expected 3 archived users, got %d", count)
	}
}

// TestMySQLInteg_InsertUsingExec 验证 InsertUsing 执行封装：INSERT INTO ... SELECT 并返回受影响行数。
func TestMySQLInteg_InsertUsingExec(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64),
		age  INT
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)

	affected, err := db.Builder().
		Table("users_archive").
		InsertUsing(context.Background(), []string{"name", "age"}, func(sub *Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	if err != nil {
		t.Fatalf("InsertUsing error: %v", err)
	}
	if affected != 3 {
		t.Errorf("expected 3 affected rows, got %d", affected)
	}

	count, _ := db.Builder().Table("users_archive").Count(context.Background())
	if count != 3 {
		t.Errorf("expected 3 archived users, got %d", count)
	}
}

// TestMySQLInteg_Truncate 验证 TRUNCATE TABLE 清空表。
func TestMySQLInteg_Truncate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	err := db.Builder().Table("users").Truncate(context.Background())
	if err != nil {
		t.Fatalf("Truncate error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after truncate, got %d", count)
	}
}

// TestMySQLInteg_InsertGetId 验证 InsertGetId 插入并返回自增 ID。
func TestMySQLInteg_InsertGetId(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	id, err := db.Builder().Table("users").InsertGetId(context.Background(), insertData{Name: "frank", Age: 40, Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("InsertGetId error: %v", err)
	}
	if id != 6 {
		t.Errorf("expected id=6, got %d", id)
	}
}

// TestMySQLInteg_UpdateJoin 验证 UPDATE ... JOIN：通过 JOIN 关联更新。
func TestMySQLInteg_UpdateJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	// 将有订单金额 > 100 的用户状态改为 'vip'
	type updateData struct {
		Status string `db:"status"`
	}
	_, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		Update(context.Background(), updateData{Status: "vip"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	// alice(Laptop=120), bob(TV=200), diana(Camera=150) → 3 users updated to 'vip'
	count, _ := db.Builder().Table("users").Where("status", "=", "vip").Count(context.Background())
	if count != 3 {
		t.Errorf("expected 3 vip users, got %d", count)
	}
}

// TestMySQLInteg_UpdateOrderByLimit 验证 UPDATE ... ORDER BY ... LIMIT：仅更新前 N 行。
func TestMySQLInteg_UpdateOrderByLimit(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 只更新 age 最大的 2 个用户
	type updateData struct {
		Status string `db:"status"`
	}
	_, err := db.Builder().Table("users").
		WhereNotNull("age").
		OrderBy("age", "DESC").
		Limit(2).
		Update(context.Background(), updateData{Status: "top"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	// charlie(35), bob(30) → top
	count, _ := db.Builder().Table("users").Where("status", "=", "top").Count(context.Background())
	if count != 2 {
		t.Errorf("expected 2 top users, got %d", count)
	}
	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	_ = db.Builder().Table("users").Select("name").Where("status", "=", "top").OrderBy("age", "DESC").Find(context.Background(), &rows)
	if len(rows) != 2 || rows[0].Name != "charlie" || rows[1].Name != "bob" {
		t.Errorf("expected [charlie, bob], got %v", rows)
	}
}

// TestMySQLInteg_InsertInvalidData 验证 Insert 传入非法类型（int、string、nil）时返回错误。
func TestMySQLInteg_InsertInvalidData(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 传入 int
	_, err := db.Builder().Table("users").Insert(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	// 传入 string
	_, err = db.Builder().Table("users").Insert(context.Background(), "hello")
	if err == nil {
		t.Errorf("expected error for string data, got nil")
	}

	// 传入 nil
	_, err = db.Builder().Table("users").Insert(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}

	// 传入 map
	_, err = db.Builder().Table("users").Insert(context.Background(), map[string]any{"name": "test"})
	if err == nil {
		t.Errorf("expected error for map data, got nil")
	}
}

// TestMySQLInteg_InsertEmptySlice 验证 Insert 传入空切片时返回错误。
func TestMySQLInteg_InsertEmptySlice(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name string `db:"name"`
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), []insertData{})
	if err == nil {
		t.Fatalf("expected error for empty slice, got nil")
	}
}

// TestMySQLInteg_InsertOrIgnoreInvalidData 验证 InsertOrIgnore 传入非法类型时返回错误。
func TestMySQLInteg_InsertOrIgnoreInvalidData(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").InsertOrIgnore(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").InsertOrIgnore(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}
}

// TestMySQLInteg_UpsertInvalidData 验证 Upsert 传入非法类型时返回错误。
func TestMySQLInteg_UpsertInvalidData(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").Upsert(context.Background(), 123, []string{"email"}, []string{"name"})
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").Upsert(context.Background(), nil, []string{"email"}, []string{"name"})
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}
}

// TestMySQLInteg_UpdateInvalidData 验证 Update 传入非法类型（切片、int、nil）时返回错误。
func TestMySQLInteg_UpdateInvalidData(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updateData struct {
		Name string `db:"name"`
	}

	// 传入切片（Update 不支持批量）
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), []updateData{{Name: "test"}})
	if err == nil {
		t.Errorf("expected error for slice data in Update, got nil")
	}

	// 传入 int
	_, err = db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data in Update, got nil")
	}

	// 传入 nil
	_, err = db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data in Update, got nil")
	}
}

// TestMySQLInteg_Bug_UpdateWithJoinValueCondition 验证 UPDATE + JOIN 含 value 条件时
// 绑定参数顺序与数量正确，语句可正常执行并只更新符合条件的行。
func TestMySQLInteg_Bug_UpdateWithJoinValueCondition(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLProfilesTable(t, db)

	// 将 user_id=2 的 profiles.active 设为 0，使只有 user 1、3 的 active=99
	mustExec(t, db, `UPDATE profiles SET active = 0 WHERE user_id = 2`)

	type updateData struct {
		Name string `db:"name"`
	}
	// JOIN ON 中含 value 条件 (profiles.active = 99)，仅更新 users.id=1
	_, err := db.Builder().Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1).
		Update(context.Background(), updateData{Name: "updated"})
	// 修复后占位符与绑定参数数量一致，不应报错
	if err != nil {
		t.Fatalf("Update with JOIN value condition error: %v", err)
	}

	// user 1 应被更新
	type row struct {
		Name string `db:"name"`
	}
	var r row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r)
	if r.Name != "updated" {
		t.Errorf("expected user 1 name 'updated', got %q", r.Name)
	}
}

// TestMySQLInteg_Bug_InsertNilPtrInSlice 验证指针切片含 nil 元素时 Insert 返回错误而非 panic。
func TestMySQLInteg_Bug_InsertNilPtrInSlice(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	data := []*insertData{
		{Name: "frank", Age: 40, Email: "frank@test.com"},
		nil,
		{Name: "grace", Age: 22, Email: "grace@test.com"},
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), data)
	if err == nil {
		t.Fatalf("expected error for nil element in pointer slice, got nil")
	}
}

// TestMySQLInteg_DeleteQueryError 验证 Delete 查询不存在的表时返回错误。
func TestMySQLInteg_DeleteQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").Where("id", "=", 1).Delete(context.Background())
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_InsertGetIdInvalidData 验证 InsertGetId 传入非法数据时返回错误。
func TestMySQLInteg_InsertGetIdInvalidData(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").InsertGetId(context.Background(), 123)
	if err == nil {
		t.Error("expected error for invalid data, got nil")
	}
}

// TestMySQLInteg_UpdateSuccess 验证 Update 正常更新数据。
func TestMySQLInteg_UpdateSuccess(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	affected, err := db.Builder().Table("users").Where("name", "=", "alice").Update(context.Background(),
		struct {
			Age int `db:"age"`
		}{Age: 50})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}
}

// TestMySQLInteg_DeleteSuccess 验证 Delete 正常删除数据。
func TestMySQLInteg_DeleteSuccess(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	affected, err := db.Builder().Table("users").Where("name", "=", "alice").Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 4 {
		t.Errorf("expected 4 rows after delete, got %d", count)
	}
}

// TestMySQLInteg_InsertGetIdSuccess 验证 InsertGetId 正常插入并返回自增 ID。
func TestMySQLInteg_InsertGetIdSuccess(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type newUser struct {
		Name   string `db:"name"`
		Age    int    `db:"age"`
		Email  string `db:"email"`
		Status string `db:"status"`
	}
	id, err := db.Builder().Table("users").InsertGetId(context.Background(),
		newUser{Name: "frank", Age: 40, Email: "frank@test.com", Status: "active"})
	if err != nil {
		t.Fatalf("InsertGetId error: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive id, got %d", id)
	}
}

// TestMySQLInteg_UpdateQueryError 验证 Update 操作不存在的表时返回错误。
func TestMySQLInteg_UpdateQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").Where("id", "=", 1).Update(context.Background(),
		struct {
			Name string `db:"name"`
		}{Name: "test"})
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_InsertQueryError 验证 Insert 操作不存在的表时返回错误。
func TestMySQLInteg_InsertQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").Insert(context.Background(),
		struct {
			Name string `db:"name"`
		}{Name: "test"})
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_InsertOrIgnoreQueryError 验证 InsertOrIgnore 操作不存在的表时返回错误。
func TestMySQLInteg_InsertOrIgnoreQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").InsertOrIgnore(context.Background(),
		struct {
			Name string `db:"name"`
		}{Name: "test"})
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_UpsertQueryError 验证 Upsert 操作不存在的表时返回错误。
func TestMySQLInteg_UpsertQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").Upsert(context.Background(),
		struct {
			Name string `db:"name"`
		}{Name: "test"}, []string{"name"}, []string{"name"})
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_InsertGetIdExecError 验证 InsertGetId 执行不存在的表时返回错误。
func TestMySQLInteg_InsertGetIdExecError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").InsertGetId(context.Background(),
		struct {
			Name string `db:"name"`
		}{Name: "test"})
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_TruncateError 验证 Truncate 不存在的表时返回错误。
func TestMySQLInteg_TruncateError(t *testing.T) {
	db := openMySQLTestDB(t)
	err := db.Builder().Table("nonexistent_table").Truncate(context.Background())
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_TruncateEmptyTable 验证 Truncate 未设置表名时返回 ErrEmptyTable。
func TestMySQLInteg_TruncateEmptyTable(t *testing.T) {
	db := openMySQLTestDB(t)
	err := db.Builder().Truncate(context.Background())
	if err == nil {
		t.Error("expected error for empty table, got nil")
	}
}

// TestMySQLInteg_DeleteEmptyTable 验证 Delete 未设置表名时返回错误。
func TestMySQLInteg_DeleteEmptyTable(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Where("id", "=", 1).Delete(context.Background())
	if err == nil {
		t.Error("expected error for empty table, got nil")
	}
}

// TestMySQLInteg_Complex_InsertUsingJoinGroupHaving 验证 INSERT USING 复杂 SELECT（JOIN + WHERE + GROUP BY + HAVING）。
// 将「有 ≥2 笔订单且单笔 >30」的用户归档到 users_archive 表。
// 预期归档：alice(25), bob(30)
func TestMySQLInteg_Complex_InsertUsingJoinGroupHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64),
		age  INT
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)

	sqlStr, args, err := db.Builder().
		Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *Builder) {
			sub.Table("users").
				Select("users.name", "users.age").
				JoinOn("orders", func(j *JoinBuilder) {
					j.On("users.id", "=", "orders.user_id")
				}).
				Where("orders.amount", ">", 30).
				GroupBy("users.id", "users.name", "users.age").
				Having("COUNT(*)", ">=", 2)
		})
	if err != nil {
		t.Fatalf("ToInsertUsing error: %v", err)
	}
	mustExec(t, db, sqlStr, args...)

	count, _ := db.Builder().Table("users_archive").Count(context.Background())
	if count != 2 {
		t.Errorf("expected 2 archived users, got %d", count)
	}
}

// TestMySQLInteg_InsertUsingInvalidSubquery
// InsertUsing 子查询缺少数据源或带非法运算符时直接返回错误，不生成非法 SQL。
func TestMySQLInteg_InsertUsingInvalidSubquery(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)

	// 子查询缺少数据源 → ErrEmptyTable
	_, _, err := db.Builder().Table("users_archive").
		ToInsertUsing([]string{"name"}, func(sub *Builder) {})
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}

	// 子查询带非法运算符 → ErrInvalidOperator
	_, _, err = db.Builder().Table("users_archive").
		ToInsertUsing([]string{"name"}, func(sub *Builder) {
			sub.Table("users").Select("name").Where("id", "EVIL", 1)
		})
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}
}

// TestMySQLInteg_InsertOrIgnoreConflictZero
// 冲突未插入任何行时受影响行数为 0。
func TestMySQLInteg_InsertOrIgnoreConflictZero(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	// email 已存在（alice@test.com）：未插入任何行，受影响行数为 0
	affected, err := db.Builder().Table("users").InsertOrIgnore(context.Background(),
		insertData{Name: "duplicate", Age: 99, Email: "alice@test.com"})
	if err != nil {
		t.Fatalf("InsertOrIgnore error: %v", err)
	}
	if affected != 0 {
		t.Errorf("expected 0 affected rows on conflict, got %d", affected)
	}
	// 新 email：正常插入 1 行
	affected, err = db.Builder().Table("users").InsertOrIgnore(context.Background(),
		insertData{Name: "frank", Age: 40, Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("InsertOrIgnore error: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}
}

// TestMySQLInteg_InsertGetIdExpression
// InsertGetId 的 Expression 值内联进 SQL，不产生绑定参数。
func TestMySQLInteg_InsertGetIdExpression(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   any    `db:"age"`
		Email string `db:"email"`
	}
	// any 字段放 Expression：age = 40 直接内联
	id, err := db.Builder().Table("users").InsertGetId(context.Background(),
		insertData{Name: "frank", Age: NewExpression("40"), Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("InsertGetId error: %v", err)
	}
	if id != 6 {
		t.Errorf("expected id=6, got %d", id)
	}
	var age int
	err = db.Builder().Table("users").Select("age").Where("id", "=", 6).Value(context.Background(), &age)
	if err != nil {
		t.Fatalf("Value error: %v", err)
	}
	if age != 40 {
		t.Errorf("expected age=40, got %d", age)
	}
}

// TestMySQLInteg_InsertGetIdEmptyData
// 空结构体/空切片插入被拒绝（zcdb 不支持 default values 空插入）。
func TestMySQLInteg_InsertGetIdEmptyData(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 空结构体（无 db 字段）：ErrNoFields
	_, err := db.Builder().Table("users").InsertGetId(context.Background(), struct{}{})
	if !errors.Is(err, ErrNoFields) {
		t.Errorf("expected ErrNoFields, got %v", err)
	}
	// 空切片：ErrEmptyData
	type insertData struct {
		Name string `db:"name"`
	}
	_, err = db.Builder().Table("users").InsertGetId(context.Background(), []insertData{})
	if !errors.Is(err, ErrEmptyData) {
		t.Errorf("expected ErrEmptyData, got %v", err)
	}
}

// TestMySQLInteg_UpsertEmptyUniqueBy
// MySQL 的 Upsert 用 ON DUPLICATE KEY UPDATE，不依赖 uniqueBy；空 uniqueBy 正常执行。
func TestMySQLInteg_UpsertEmptyUniqueBy(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type upsertData struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}
	// 空 uniqueBy：MySQL 不生成 ON CONFLICT，正常插入
	affected, err := db.Builder().Table("users").Upsert(context.Background(),
		upsertData{Name: "frank", Email: "frank@test.com"},
		nil, []string{"name"})
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}
	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}
	// 冲突 email：name 被更新
	_, err = db.Builder().Table("users").Upsert(context.Background(),
		upsertData{Name: "frank2", Email: "frank@test.com"},
		nil, []string{"name"})
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}
	var name string
	err = db.Builder().Table("users").Select("name").Where("email", "=", "frank@test.com").Value(context.Background(), &name)
	if err != nil {
		t.Fatalf("Value error: %v", err)
	}
	if name != "frank2" {
		t.Errorf("expected name=frank2 after upsert conflict, got %q", name)
	}
}

// TestMySQLInteg_TruncateResetSequence
// MySQL TRUNCATE 自动重置 AUTO_INCREMENT，清空后自增主键从头开始。
func TestMySQLInteg_TruncateResetSequence(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 先插入一条使序列推进到 6
	_, err := db.Builder().Table("users").Insert(context.Background(), struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}{Name: "frank", Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}
	// Truncate：清空数据并重置自增序列
	if err := db.Builder().Table("users").Truncate(context.Background()); err != nil {
		t.Fatalf("Truncate error: %v", err)
	}
	// 插入后 id 从头开始（1），证明 AUTO_INCREMENT 已重置
	id, err := db.Builder().Table("users").InsertGetId(context.Background(), struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}{Name: "after_truncate", Email: "after@test.com"})
	if err != nil {
		t.Fatalf("InsertGetId error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1 after truncate (sequence reset), got %d", id)
	}
}

// TestMySQLInteg_JsonUpdate 验证 JSON 更新用 Update 值传 Expression
// （json_set 内联），覆盖基本/嵌套/数组索引。
func TestMySQLInteg_JsonUpdate(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		json_val JSON
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('{"name":"alice","age":25,"address":{"city":"Shanghai"}}'),
		('["red","green"]')`)

	type jsonUpdate struct {
		JsonVal any `db:"json_val"`
	}

	// 基本：json_set 顶层字段
	_, err := db.Builder().Table("json_conv_test").Where("id", "=", 1).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression("json_set(json_val, '$.age', 26)")})
	if err != nil {
		t.Fatalf("Update basic error: %v", err)
	}
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("json_extract(json_val, '$.age') = ?", 26).
		Count(context.Background())
	if err != nil {
		t.Fatalf("basic verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("basic update: expected age=26, got %d", count)
	}

	// 嵌套：json_set 嵌套路径
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 1).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression("json_set(json_val, '$.address.city', 'Guangzhou')")})
	if err != nil {
		t.Fatalf("Update nested error: %v", err)
	}
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_unquote(json_extract(json_val, '$.address.city')) = ?", "Guangzhou").
		Count(context.Background())
	if err != nil {
		t.Fatalf("nested verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("nested update: expected city=Guangzhou, got %d", count)
	}

	// 数组索引：json_set 修改数组元素
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 2).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression("json_set(json_val, '$[0]', 'blue')")})
	if err != nil {
		t.Fatalf("Update array error: %v", err)
	}
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_unquote(json_extract(json_val, '$[0]')) = ?", "blue").
		Count(context.Background())
	if err != nil {
		t.Fatalf("array verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("array update: expected [0]=blue, got %d", count)
	}

	// 数组嵌套索引：json_set 修改 $[0].name
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES ('[{"name":"a"},{"name":"b"}]')`)
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 3).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression("json_set(json_val, '$[0].name', 'x')")})
	if err != nil {
		t.Fatalf("Update array index error: %v", err)
	}
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("json_unquote(json_extract(json_val, '$[0].name')) = ?", "x").
		Count(context.Background())
	if err != nil {
		t.Fatalf("array index verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("array index update: expected $[0].name=x, got %d", count)
	}
}

// TestMySQLInteg_NewApi_IncrementDecrement 验证原子自增/自减在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_IncrementDecrement(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLWalletsTable(t, db)

	ctx := context.Background()

	// 编译形态断言：SET col = col + ?
	sqlStr, args, err := db.Builder().Table("wallets").Where("id", "=", 1).ToIncrement([]string{"balance", "points"}, []any{10, 5})
	assertNoError(t, err)
	assertSQL(t, "UPDATE `wallets` SET `balance` = `balance` + ?, `points` = `points` + ? WHERE `id` = ?", sqlStr)
	assertArgs(t, []any{10, 5, 1}, args)

	// 多列自增真实执行
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 50, "points", 5)
	assertNoError(t, err)
	type wallet struct {
		Balance int64 `db:"balance"`
		Points  int64 `db:"points"`
	}
	var w wallet
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance", "points").First(ctx, &w)
	assertNoError(t, err)
	if w.Balance != 150 || w.Points != 15 {
		t.Errorf("multi-column increment: expected {150 15}, got %+v", w)
	}

	// 自减
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Decrement(ctx, "balance", 30)
	assertNoError(t, err)
	var balance int64
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance").Value(ctx, &balance)
	assertNoError(t, err)
	if balance != 120 {
		t.Errorf("balance after decrement: expected 120, got %d", balance)
	}

	// 保护机制
	_, err = db.Builder().Table("wallets").Increment(ctx, "balance", 1)
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("expected ErrUpdateWithoutWhere, got %v", err)
	}
}

// TestMySQLInteg_NewApi_InsertOrIgnoreUsing 验证 INSERT IGNORE INTO ... SELECT 冲突忽略。
func TestMySQLInteg_NewApi_InsertOrIgnoreUsing(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLNewApiTables(t, db)
	setupMySQLArchiveTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("archive").ToInsertOrIgnoreUsing([]string{"name", "email"}, func(q *Builder) {
		q.Table("users").Select("name", "email").Where("status", "=", "active")
	})
	assertNoError(t, err)
	assertSQL(t,
		"INSERT IGNORE INTO `archive` (`name`, `email`) SELECT `name`, `email` FROM `users` WHERE `status` = ?",
		sqlStr)

	// 真实执行：alice 冲突忽略，bob/diana 插入
	affected, err := db.Builder().Table("archive").InsertOrIgnoreUsing(
		context.Background(),
		[]string{"name", "email"},
		func(q *Builder) {
			q.Table("users").Select("name", "email").Where("status", "=", "active")
		},
	)
	assertNoError(t, err)
	if affected != 2 {
		t.Errorf("affected: expected 2, got %d", affected)
	}
	count, err := db.Builder().Table("archive").Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("archive count: expected 4, got %d", count)
	}
}

// TestMySQLInteg_NewApi_DeleteJoin 验证 MySQL 多表 DELETE 直译形态与真实执行。
func TestMySQLInteg_NewApi_DeleteJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	// 编译形态断言
	sqlStr, args, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		ToDeleteJoin()
	assertNoError(t, err)
	assertSQL(t,
		"DELETE `users` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id` WHERE `orders`.`amount` > ?",
		sqlStr)
	assertArgs(t, []any{100}, args)

	// 真实执行
	affected, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		DeleteJoin(context.Background())
	assertNoError(t, err)
	if affected != 3 {
		t.Errorf("DeleteJoin affected: expected 3, got %d", affected)
	}
	count, err := db.Builder().Table("users").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("users after DeleteJoin: expected 2, got %d", count)
	}

	// 保护机制
	_, err = db.Builder().Table("users").DeleteJoin(context.Background())
	if !errors.Is(err, ErrDeleteWithoutWhere) {
		t.Errorf("expected ErrDeleteWithoutWhere, got %v", err)
	}
}

// TestMySQLInteg_Bug_EmptyWhereRawProtection 锁定 B1 修复：
// 空串/纯空白 WhereRaw 编译后无任何条件，不得计为有效 WHERE——
// 修复前 WhereRaw("").Delete 会绕过保护删除全表。
func TestMySQLInteg_Bug_EmptyWhereRawProtection(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	ctx := context.Background()

	for _, raw := range []string{"", "   "} {
		_, err := db.Builder().Table("users").WhereRaw(raw).Delete(ctx)
		if !errors.Is(err, ErrDeleteWithoutWhere) {
			t.Errorf("Delete with WhereRaw(%q): expected ErrDeleteWithoutWhere, got %v", raw, err)
		}
		_, err = db.Builder().Table("users").WhereRaw(raw).Update(ctx, struct {
			Status string `db:"status"`
		}{Status: "x"})
		if !errors.Is(err, ErrUpdateWithoutWhere) {
			t.Errorf("Update with WhereRaw(%q): expected ErrUpdateWithoutWhere, got %v", raw, err)
		}
		_, err = db.Builder().Table("users").WhereRaw(raw).Increment(ctx, "age", 1)
		if !errors.Is(err, ErrUpdateWithoutWhere) {
			t.Errorf("Increment with WhereRaw(%q): expected ErrUpdateWithoutWhere, got %v", raw, err)
		}
	}

	count, err := db.Builder().Table("users").Count(ctx)
	assertNoError(t, err)
	if count != 5 {
		t.Fatalf("users count after rejected ops: expected 5, got %d", count)
	}

	// 对照：非空 Raw 仍是有效限定/逃生口
	affected, err := db.Builder().Table("users").WhereRaw("1 = 1").Delete(ctx)
	assertNoError(t, err)
	if affected != 5 {
		t.Errorf("Delete with WhereRaw(\"1 = 1\"): expected 5, got %d", affected)
	}
}

// TestMySQLInteg_Bug_DanglingBoolean 锁定 M3 修复：
// 首条子句编译为空时，后续子句不得带悬挂连接词（修复前 "WHERE/ON/HAVING AND ..." 语法错误）。
func TestMySQLInteg_Bug_DanglingBoolean(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLProfilesTable(t, db)
	ctx := context.Background()

	// ON 侧：JoinBuilder 首条件为空 Raw
	count, err := db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.Raw("").On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", 99)
		}).Count(ctx)
	assertNoError(t, err)
	if count != 3 {
		t.Errorf("Join with leading empty Raw: expected 3, got %d", count)
	}

	// HAVING 侧：首条 HavingRaw 为空
	var statuses []string
	err = db.Builder().Table("users").Select("status").GroupBy("status").
		HavingRaw("").Having("status", "=", "inactive").
		Pluck(ctx, &statuses, "status")
	assertNoError(t, err)
	if len(statuses) != 1 || statuses[0] != "inactive" {
		t.Errorf("Having after empty HavingRaw: expected [inactive], got %v", statuses)
	}

	// WHERE 侧：空 Raw 后的有效条件正常生效
	affected, err := db.Builder().Table("users").WhereRaw("").Where("id", "=", 1).Delete(ctx)
	assertNoError(t, err)
	if affected != 1 {
		t.Errorf("Delete after empty WhereRaw: expected 1, got %d", affected)
	}
}

// TestMySQLInteg_Bug_UpdateMixedJoinBindings 锁定 B2 修复（MySQL 对照组）：
// MySQL 直译 JOIN，绑定顺序天然一致，本用例防回归；
// 错位敏感方言为 PG/SQLite（FROM/USING 形态），见对应方言同名用例。
func TestMySQLInteg_Bug_UpdateMixedJoinBindings(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLProfilesTable(t, db)
	setupMySQLOrdersTable(t, db)
	ctx := context.Background()

	// profiles.active=99 → user 1,2,3；orders amount>100 → user 1,2,4；
	// 交集 1,2 且 status='active' → alice/bob 2 行
	buildChain := func(status string) *Builder {
		sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
		return db.Builder().Table("users").
			JoinOn("profiles", func(j *JoinBuilder) {
				j.On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", 99)
			}).
			JoinSub(sub, "o", func(j *JoinBuilder) { j.On("o.user_id", "=", "users.id") }).
			Where("users.status", "=", status)
	}

	affected, err := buildChain("active").Update(ctx, struct {
		Status string `db:"status"`
	}{Status: "vip"})
	assertNoError(t, err)
	if affected != 2 {
		t.Fatalf("Update mixed-join affected: expected 2, got %d", affected)
	}

	// toIncDec 路径：alice 25→26, bob 30→31
	affected, err = buildChain("vip").Increment(ctx, "age", 1)
	assertNoError(t, err)
	if affected != 2 {
		t.Fatalf("Increment mixed-join affected: expected 2, got %d", affected)
	}
	var age int
	err = db.Builder().Table("users").Select("age").Where("id", "=", 1).Value(ctx, &age)
	assertNoError(t, err)
	if age != 26 {
		t.Errorf("alice age after increment: expected 26, got %d", age)
	}
	err = db.Builder().Table("users").Select("age").Where("id", "=", 2).Value(ctx, &age)
	assertNoError(t, err)
	if age != 31 {
		t.Errorf("bob age after increment: expected 31, got %d", age)
	}
}

// TestMySQLInteg_Bug_DeleteJoinMixedBindings 锁定 B2 修复（DeleteJoin 路径，MySQL 对照组）。
func TestMySQLInteg_Bug_DeleteJoinMixedBindings(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLProfilesTable(t, db)
	setupMySQLOrdersTable(t, db)
	ctx := context.Background()

	sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
	affected, err := db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", 99)
		}).
		JoinSub(sub, "o", func(j *JoinBuilder) { j.On("o.user_id", "=", "users.id") }).
		Where("users.status", "=", "active").
		DeleteJoin(ctx)
	assertNoError(t, err)
	if affected != 2 { // alice/bob
		t.Fatalf("DeleteJoin mixed-join affected: expected 2, got %d", affected)
	}
	count, err := db.Builder().Table("users").Count(ctx)
	assertNoError(t, err)
	if count != 3 {
		t.Errorf("users after DeleteJoin: expected 3, got %d", count)
	}
}

// TestMySQLInteg_Bug_UpsertDefaultExcludeUniqueBy 锁定 m1 修复：
// updateColumns 为空时排除 uniqueBy 列；全部插入列均为 uniqueBy 时退化为自赋值 no-op。
func TestMySQLInteg_Bug_UpsertDefaultExcludeUniqueBy(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	ctx := context.Background()

	// 默认 updateColumns（nil）：冲突时更新 name/age
	_, err := db.Builder().Table("users").Upsert(ctx, struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}{Name: "alice_new", Age: 99, Email: "alice@test.com"}, []string{"email"}, nil)
	assertNoError(t, err)
	var name string
	var age int
	err = db.Builder().Table("users").Select("name").Where("email", "=", "alice@test.com").Value(ctx, &name)
	assertNoError(t, err)
	err = db.Builder().Table("users").Select("age").Where("email", "=", "alice@test.com").Value(ctx, &age)
	assertNoError(t, err)
	if name != "alice_new" || age != 99 {
		t.Errorf("default upsert columns: expected alice_new/99, got %s/%d", name, age)
	}

	// 全部插入列均为 uniqueBy：冲突时 no-op，不报错、不更新
	// （tokens 不在公共清表清单内，先 DROP 保证可重复运行）
	_, _ = db.Exec(ctx, `DROP TABLE IF EXISTS tokens`)
	mustExec(t, db, `CREATE TABLE tokens (id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY, token VARCHAR(64) UNIQUE, hits BIGINT DEFAULT 0)`)
	mustExec(t, db, `INSERT INTO tokens (token, hits) VALUES ('tok1', 5)`)
	_, err = db.Builder().Table("tokens").Upsert(ctx, struct {
		Token string `db:"token"`
	}{Token: "tok1"}, []string{"token"}, nil)
	assertNoError(t, err)
	var hits int
	err = db.Builder().Table("tokens").Select("hits").Where("token", "=", "tok1").Value(ctx, &hits)
	assertNoError(t, err)
	if hits != 5 {
		t.Errorf("upsert all-uniqueBy should not update, got hits=%d", hits)
	}

	// 全部列为 uniqueBy 且 key 不存在：正常插入新行
	_, err = db.Builder().Table("tokens").Upsert(ctx, struct {
		Token string `db:"token"`
	}{Token: "tok2"}, []string{"token"}, nil)
	assertNoError(t, err)
	exists, err := db.Builder().Table("tokens").Where("token", "=", "tok2").Exists(ctx)
	assertNoError(t, err)
	if !exists {
		t.Errorf("upsert all-uniqueBy new row should be inserted")
	}
}

// TestMySQLInteg_Bug_UpsertExpressionValue 锁定 m2 修复：
// Upsert 的 Expression 字段值内联进 SQL（修复前作为绑定值传给驱动必报错）。
func TestMySQLInteg_Bug_UpsertExpressionValue(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	ctx := context.Background()

	_, err := db.Builder().Table("users").Upsert(ctx, struct {
		Name  string `db:"name"`
		Age   any    `db:"age"`
		Email string `db:"email"`
	}{Name: "frank", Age: NewExpression("40"), Email: "frank@test.com"},
		[]string{"email"}, []string{"name", "age"})
	assertNoError(t, err)

	var age int
	err = db.Builder().Table("users").Select("age").Where("email", "=", "frank@test.com").Value(ctx, &age)
	assertNoError(t, err)
	if age != 40 {
		t.Errorf("upsert expression age: expected 40, got %d", age)
	}
}

// TestMySQLInteg_Bug_MixedCaseOperatorDirection 锁定 m3 修复：
// 修复前 "Like" 误报 ErrInvalidOperator、"Desc" 静默归一为 ASC。
func TestMySQLInteg_Bug_MixedCaseOperatorDirection(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	ctx := context.Background()

	var names []string
	err := db.Builder().Table("users").Where("name", "Like", "ali%").Pluck(ctx, &names, "name")
	assertNoError(t, err)
	if len(names) != 1 || names[0] != "alice" {
		t.Errorf("mixed-case Like: expected [alice], got %v", names)
	}

	var ids []int
	err = db.Builder().Table("users").OrderBy("id", "Desc").Pluck(ctx, &ids, "id")
	assertNoError(t, err)
	if len(ids) != 5 || ids[0] != 5 {
		t.Errorf("mixed-case Desc: expected first id=5, got %v", ids)
	}
}

// TestMySQLInteg_Bug_OnSQLPanicRecovered 锁定 m5 修复：
// 慢 SQL 回调 panic 被 recover 隔离，不影响 Exec/Query 主流程。
func TestMySQLInteg_Bug_OnSQLPanicRecovered(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	panicDao, err := NewDBDao(db.Pool(), "mysql", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		panic("callback boom")
	}, "")
	assertNoError(t, err)
	ctx := context.Background()

	_, err = panicDao.Builder().Table("users").Insert(ctx, struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}{Name: "frank", Email: "frank@test.com"})
	assertNoError(t, err)

	rows, err := panicDao.Query(ctx, "SELECT COUNT(*) AS cnt FROM users")
	assertNoError(t, err)
	var dest []struct {
		Cnt int `db:"cnt"`
	}
	err = ScanStructClose(rows, &dest)
	assertNoError(t, err)
	if len(dest) != 1 || dest[0].Cnt != 6 {
		t.Errorf("query after panicked callback: expected cnt=6, got %+v", dest)
	}
}

// TestMySQLInteg_OrderByRawInUpdateDelete 覆盖 MySQL 方言 UPDATE/DELETE
// 编译中 ORDER BY 原始片段的分支（MySQL 支持 UPDATE/DELETE 带 ORDER BY + LIMIT）。
// （原 crossDialect 方言特有用例归位。）
func TestMySQLInteg_OrderByRawInUpdateDelete(t *testing.T) {
	type obUpdRow struct {
		Name string `db:"name"`
	}
	dao := openMySQLTestDB(t)
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_ob")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_ob (id INTEGER PRIMARY KEY, name TEXT)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_ob (id, name) VALUES (1, 'a'), (2, 'b')`)

	affected, err := dao.Builder().Table("cross_dialect_ob").Where("id", ">", 0).
		OrderByRaw("id DESC").OrderByRaw("name ASC").Limit(1).
		Update(ctx, &obUpdRow{Name: "x"})
	if err != nil || affected != 1 {
		t.Fatalf("UPDATE 携带 ORDER BY raw 失败: affected=%d err=%v", affected, err)
	}

	affected, err = dao.Builder().Table("cross_dialect_ob").Where("id", ">", 0).
		OrderByRaw("id DESC").OrderByRaw("name ASC").Limit(1).
		Delete(ctx)
	if err != nil || affected != 1 {
		t.Fatalf("DELETE 携带 ORDER BY raw 失败: affected=%d err=%v", affected, err)
	}
}

// TestMySQLInteg_UpsertEmptyUpdateColumnsNoop 覆盖 MySQL ON DUPLICATE KEY UPDATE
// 在 updateColumns 为空且插入列全部为 uniqueBy 时的 no-op 自赋值退化分支：
// 冲突时不更新任何列。
// （原 crossDialect 方言特有用例归位。）
func TestMySQLInteg_UpsertEmptyUpdateColumnsNoop(t *testing.T) {
	type upNoopRow struct {
		A int64 `db:"a"`
		B int64 `db:"b"`
	}
	dao := openMySQLTestDB(t)
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_upnoop")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_upnoop (a INTEGER, b INTEGER, UNIQUE KEY uniq_ab (a, b))`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_upnoop (a, b) VALUES (1, 2)`)

	// 插入列全部为 uniqueBy：updateColumns 解析为空 → 退化自赋值，冲突不更新
	if _, err := dao.Builder().Table("cross_dialect_upnoop").Upsert(ctx,
		upNoopRow{A: 1, B: 2}, []string{"a", "b"}, nil); err != nil {
		t.Fatalf("空 updateColumns no-op Upsert 不应报错: %v", err)
	}
	n, err := dao.Builder().Table("cross_dialect_upnoop").Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("no-op Upsert 后应仍为 1 行，实际 %d, err=%v", n, err)
	}
}
