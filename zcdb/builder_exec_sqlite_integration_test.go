// 本文件为 SQLite 集成测试——写操作执行（Insert/Update/Delete/Upsert/Truncate 等）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "modernc.org/sqlite"
	"strings"
	"testing"
	"time"
)

// TestSQLiteInteg_InsertSingle 验证单条结构体插入：传入单个结构体，生成并执行 INSERT，确认数据正确写入。
func TestSQLiteInteg_InsertSingle(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertBatch 验证批量插入：传入结构体切片，生成并执行单条 INSERT 多 VALUES，确认所有行正确写入。
func TestSQLiteInteg_InsertBatch(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertPtrPartial 验证指针字段 nil 跳过：nil 指针字段不参与 INSERT，对应列应为数据库默认值（NULL）。
func TestSQLiteInteg_InsertPtrPartial(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type insertPtrData struct {
		Name  *string `db:"name"`
		Age   *int    `db:"age"`
		Email *string `db:"email"`
	}
	name := "frank"
	email := "frank@test.com"
	_, err := db.Builder().Table("users").Insert(context.Background(), insertPtrData{Name: &name, Email: &email})
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	var age *int
	err = db.Builder().Table("users").Select("age").Where("name", "=", "frank").Value(context.Background(), &age)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if age != nil {
		t.Errorf("expected NULL age, got %d", *age)
	}
}

// TestSQLiteInteg_InsertPtrAllNil 验证全 nil 指针插入：所有指针字段均为 nil 时返回错误。
func TestSQLiteInteg_InsertPtrAllNil(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertBatchPtr 验证批量指针插入：部分行含 nil 指针字段，对应列应为 NULL。
func TestSQLiteInteg_InsertBatchPtr(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertOrIgnore 验证冲突忽略插入：当 UNIQUE 约束冲突时不报错且不插入新行，原有数据不受影响。
func TestSQLiteInteg_InsertOrIgnore(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_UpdateBasic 验证基础 UPDATE：通过结构体指定更新字段，WHERE 定位单行，确认字段值变更。
func TestSQLiteInteg_UpdateBasic(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_UpdatePtrPartial 验证指针字段部分更新：nil 指针字段不参与 SET，对应列保持原值不变。
func TestSQLiteInteg_UpdatePtrPartial(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type updatePtrData struct {
		Name   *string `db:"name"`
		Age    *int    `db:"age"`
		Status *string `db:"status"`
	}
	newName := "alice_ptr"
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), updatePtrData{Name: &newName})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	type verifyRow struct {
		Name   string `db:"name"`
		Age    int    `db:"age"`
		Status string `db:"status"`
	}
	var rows []verifyRow
	_ = db.Builder().Table("users").Select("name", "age", "status").Where("id", "=", 1).Find(context.Background(), &rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Name != "alice_ptr" {
		t.Errorf("expected alice_ptr, got %s", rows[0].Name)
	}
	if rows[0].Age != 25 {
		t.Errorf("expected age still 25, got %d", rows[0].Age)
	}
	if rows[0].Status != "active" {
		t.Errorf("expected status still active, got %s", rows[0].Status)
	}
}

// TestSQLiteInteg_UpdateWithRaw 验证 NewExpression 表达式更新：字段值为 NewExpression("age" + 10) 时生成原始 SQL 而非占位符。
func TestSQLiteInteg_UpdateWithRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type updateRaw struct {
		Age any `db:"age"`
	}
	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), updateRaw{Age: NewExpression("\"age\" + 10")})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	var age int
	_ = db.Builder().Table("users").Select("age").Where("id", "=", 1).Value(context.Background(), &age)
	if age != 35 {
		t.Errorf("expected age=35, got %d", age)
	}
}

// TestSQLiteInteg_UpdatePtrAllNil 验证全 nil 指针更新：所有指针字段均为 nil 时返回错误。
func TestSQLiteInteg_UpdatePtrAllNil(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_UpdateWithJoin 验证 SQLite 多表更新：使用 FROM 子句实现 JOIN 更新。
func TestSQLiteInteg_UpdateWithJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type updateData struct {
		Status string `db:"status"`
	}
	// 将有订单金额 > 100 的用户状态改为 'vip'
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

// TestSQLiteInteg_Bug_UpdateJoinSubEmptyFrom 审查复现用例（SQLite 方言）：
// CompileUpdate FROM 子句直接取 join.Table，JoinSub 时 Table 为空导致无效 SQL。
// 与 TestPgInteg_Bug_UpdateJoinSubEmptyFrom 同因，修复后 FROM 应编译为派生表。
func TestSQLiteInteg_Bug_UpdateJoinSubEmptyFrom(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	// user_id=2 的 active=0，只有 user 1/3 的 active=99
	mustExec(t, db, `UPDATE profiles SET active = 0 WHERE user_id = 2`)

	type updateData struct {
		Name string `db:"name"`
	}
	sub := db.Builder().Table("profiles").Select("user_id").Where("active", "=", 99)

	// 编译层面：FROM 应为派生表而非空表名
	sqlStr, args, err := db.Builder().Table("users").
		JoinSub(sub, "p", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "p.user_id")
		}).
		ToUpdate(updateData{Name: "updated"})
	assertNoError(t, err)
	assertSQL(t,
		`UPDATE "users" SET "name" = ? FROM (SELECT "user_id" FROM "profiles" WHERE "active" = ?) AS "p" WHERE "users"."id" = "p"."user_id"`,
		sqlStr)
	assertArgs(t, []any{"updated", 99}, args)

	// 执行层面：仅 user 1/3 被更新，user 2 不受影响
	_, err = db.Builder().Table("users").
		JoinSub(sub, "p", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "p.user_id")
		}).
		Update(context.Background(), updateData{Name: "updated"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	type row struct {
		Name string `db:"name"`
	}
	var r2 row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 2).First(context.Background(), &r2)
	if r2.Name == "updated" {
		t.Errorf("user 2 (bob) should NOT be updated (profiles.active=0)")
	}
	var r1 row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r1)
	if r1.Name != "updated" {
		t.Errorf("expected user 1 name 'updated', got %q", r1.Name)
	}
}

// TestSQLiteInteg_DeleteAll 验证 Force() 允许无条件全表删除。
func TestSQLiteInteg_DeleteAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	_, err := db.Builder().Table("users").Force().Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after delete all, got %d", count)
	}
}

// TestSQLiteInteg_Upsert 验证 UPSERT（INSERT ... ON CONFLICT DO UPDATE）：新行正常插入，冲突行触发更新指定列。
func TestSQLiteInteg_Upsert(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

	// 冲突更新（email 已存在）
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

// TestSQLiteInteg_UpsertBatch 验证批量 UPSERT：切片中新增行与冲突行同时处理。
func TestSQLiteInteg_UpsertBatch(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertUsing 验证 INSERT ... SELECT 子查询插入：从源表查询数据直接插入目标表。
func TestSQLiteInteg_InsertUsing(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 创建归档表
	mustExec(t, db, `CREATE TABLE users_archive (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		age INTEGER
	)`)

	// INSERT INTO users_archive (name, age) SELECT name, age FROM users WHERE status = 'active'
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

// TestSQLiteInteg_InsertUsingExec 验证 InsertUsing 执行封装：INSERT ... SELECT 并返回受影响行数。
func TestSQLiteInteg_InsertUsingExec(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 创建归档表
	mustExec(t, db, `CREATE TABLE users_archive (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		age INTEGER
	)`)

	// INSERT INTO users_archive (name, age) SELECT name, age FROM users WHERE status = 'active'
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

// TestSQLiteInteg_InsertGetId 验证 InsertGetId 插入并返回自增 ID。
func TestSQLiteInteg_InsertGetId(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Truncate 验证 TRUNCATE 清空表：执行后表中行数归零。
func TestSQLiteInteg_Truncate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	err := db.Builder().Table("users").Truncate(context.Background())
	if err != nil {
		t.Fatalf("Truncate error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after truncate, got %d", count)
	}
}

// TestSQLiteInteg_InsertInvalidData 验证 Insert 传入非法类型（int、string、nil）时返回错误。
func TestSQLiteInteg_InsertInvalidData(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	_, err := db.Builder().Table("users").Insert(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").Insert(context.Background(), "hello")
	if err == nil {
		t.Errorf("expected error for string data, got nil")
	}

	_, err = db.Builder().Table("users").Insert(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}

	_, err = db.Builder().Table("users").Insert(context.Background(), map[string]any{"name": "test"})
	if err == nil {
		t.Errorf("expected error for map data, got nil")
	}
}

// TestSQLiteInteg_InsertEmptySlice 验证 Insert 传入空切片时返回错误。
func TestSQLiteInteg_InsertEmptySlice(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type insertData struct {
		Name string `db:"name"`
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), []insertData{})
	if err == nil {
		t.Fatalf("expected error for empty slice, got nil")
	}
}

// TestSQLiteInteg_InsertOrIgnoreInvalidData 验证 InsertOrIgnore 传入非法类型时返回错误。
func TestSQLiteInteg_InsertOrIgnoreInvalidData(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	_, err := db.Builder().Table("users").InsertOrIgnore(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").InsertOrIgnore(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}
}

// TestSQLiteInteg_UpsertInvalidData 验证 Upsert 传入非法类型时返回错误。
func TestSQLiteInteg_UpsertInvalidData(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	_, err := db.Builder().Table("users").Upsert(context.Background(), 123, []string{"email"}, []string{"name"})
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").Upsert(context.Background(), nil, []string{"email"}, []string{"name"})
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}
}

// TestSQLiteInteg_UpdateInvalidData 验证 Update 传入非法类型（切片、int、nil）时返回错误。
func TestSQLiteInteg_UpdateInvalidData(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type updateData struct {
		Name string `db:"name"`
	}

	_, err := db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), []updateData{{Name: "test"}})
	if err == nil {
		t.Errorf("expected error for slice data in Update, got nil")
	}

	_, err = db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data in Update, got nil")
	}

	_, err = db.Builder().Table("users").Where("id", "=", 1).Update(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data in Update, got nil")
	}
}

// TestSQLiteInteg_Bug_UpdateJoinDropsValueCondition 验证 SQLite UPDATE + JOIN 含 value 条件时
// 条件被静默丢弃：更新影响了不应被影响的行。
func TestSQLiteInteg_Bug_UpdateJoinDropsValueCondition(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	// profiles: user_id=1 active=99, user_id=2 active=99, user_id=3 active=99
	// 先将 user_id=2 的 active 设为 0，这样只有 user_id=1 和 3 的 active=99
	mustExec(t, db, `UPDATE profiles SET active = 0 WHERE user_id = 2`)

	type updateData struct {
		Name string `db:"name"`
	}
	// 意图：只更新 profiles.active=99 的用户（user 1 和 3）
	_, err := db.Builder().Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Update(context.Background(), updateData{Name: "updated"})
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}

	// 检查 user 2 (bob) 是否被错误更新（bob 的 profiles.active=0，不应被更新）
	type row struct {
		Name string `db:"name"`
	}
	var r row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 2).First(context.Background(), &r)
	if r.Name == "updated" {
		t.Errorf("BUG: user 2 (bob) should NOT be updated (profiles.active=0), but was updated due to dropped value condition")
	}
}

// TestSQLiteInteg_Bug_InsertNilPtrInSlice 验证指针切片含 nil 元素时 Insert 返回错误而非 panic。
func TestSQLiteInteg_Bug_InsertNilPtrInSlice(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Complex_InsertUsingJoinGroupHaving 验证 INSERT USING 复杂 SELECT（JOIN + WHERE + GROUP BY + HAVING）。
// 预期归档：alice(25), bob(30)
func TestSQLiteInteg_Complex_InsertUsingJoinGroupHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		age INTEGER
	)`)

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

// TestSQLiteInteg_InsertNilPointer 验证 Insert 传入 nil 结构体指针返回 ErrInvalidStruct 而非 panic。
func TestSQLiteInteg_InsertNilPointer(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type user struct {
		Name string `db:"name"`
	}
	var u *user
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Insert panicked on nil pointer: %v", r)
		}
	}()
	_, err := db.Builder().Table("users").Insert(context.Background(), u)
	if !errors.Is(err, ErrInvalidStruct) {
		t.Fatalf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestSQLiteInteg_InsertUsingInvalidSubquery
// InsertUsing 子查询缺少数据源或带非法运算符时直接返回错误，不生成非法 SQL。
func TestSQLiteInteg_InsertUsingInvalidSubquery(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)

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

// TestSQLiteInteg_InsertOrIgnoreConflictZero
// 冲突未插入任何行时受影响行数为 0。
func TestSQLiteInteg_InsertOrIgnoreConflictZero(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertGetIdExpression
// InsertGetId 的 Expression 值内联进 SQL，不产生绑定参数。
func TestSQLiteInteg_InsertGetIdExpression(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_InsertGetIdEmptyData
// 空结构体/空切片插入被拒绝（zcdb 不支持 default values 空插入）。
func TestSQLiteInteg_InsertGetIdEmptyData(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_UpsertEmptyUniqueBy
// SQLite 需要 uniqueBy 生成 ON CONFLICT 目标，空值直接拒绝。
func TestSQLiteInteg_UpsertEmptyUniqueBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type upsertData struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}
	_, err := db.Builder().Table("users").Upsert(context.Background(),
		upsertData{Name: "frank", Email: "frank@test.com"},
		nil, []string{"name"})
	if !errors.Is(err, ErrUpsertUniqueByRequired) {
		t.Errorf("expected ErrUpsertUniqueByRequired, got %v", err)
	}
}

// TestSQLiteInteg_TruncateResetSequence
// SQLite truncate 清空数据并重置 AUTOINCREMENT 序列，自增主键从头开始。
func TestSQLiteInteg_TruncateResetSequence(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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
	// 插入后 id 从头开始（1），证明 sqlite_sequence 已重置
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

// TestSQLiteInteg_JsonUpdate 验证 JSON 更新用 Update 值传 Expression
// （json_patch 合并），覆盖基本/嵌套/数组替换。
func TestSQLiteInteg_JsonUpdate(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		json_val TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('{"name":"alice","age":25,"address":{"city":"Shanghai"}}'),
		('["red","green"]')`)

	type jsonUpdate struct {
		JsonVal any `db:"json_val"`
	}

	// 基本：json_patch 合并顶层字段
	_, err := db.Builder().Table("json_conv_test").Where("id", "=", 1).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression(`json_patch(ifnull(json_val, '{}'), '{"age":26}')`)})
	if err != nil {
		t.Fatalf("Update basic error: %v", err)
	}
	var val string
	err = db.Builder().Table("json_conv_test").Select("json_val").Where("id", "=", 1).
		Value(context.Background(), &val)
	if err != nil {
		t.Fatalf("Value basic error: %v", err)
	}
	if !strings.Contains(val, `"age":26`) {
		t.Errorf("basic update: expected age=26 in %s", val)
	}

	// 嵌套：json_patch 合并嵌套对象
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 1).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression(`json_patch(ifnull(json_val, '{}'), '{"address":{"city":"Guangzhou"}}')`)})
	if err != nil {
		t.Fatalf("Update nested error: %v", err)
	}
	err = db.Builder().Table("json_conv_test").Select("json_val").Where("id", "=", 1).
		Value(context.Background(), &val)
	if err != nil {
		t.Fatalf("Value nested error: %v", err)
	}
	if !strings.Contains(val, `"city":"Guangzhou"`) {
		t.Errorf("nested update: expected city=Guangzhou in %s", val)
	}

	// 数组：json_patch 整体替换数组
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 2).
		Update(context.Background(), jsonUpdate{JsonVal: NewExpression(`json_patch(ifnull(json_val, '{}'), '["blue","yellow"]')`)})
	if err != nil {
		t.Fatalf("Update array error: %v", err)
	}
	err = db.Builder().Table("json_conv_test").Select("json_val").Where("id", "=", 2).
		Value(context.Background(), &val)
	if err != nil {
		t.Fatalf("Value array error: %v", err)
	}
	if !strings.Contains(val, `"blue"`) || !strings.Contains(val, `"yellow"`) || strings.Contains(val, "red") {
		t.Errorf("array update: expected [blue,yellow] in %s", val)
	}
}

// TestSQLiteInteg_NewApi_IncrementDecrement 验证原子自增/自减（含多列、保护机制）。
func TestSQLiteInteg_NewApi_IncrementDecrement(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteWalletsTable(t, db)

	ctx := context.Background()

	// 单列自增
	affected, err := db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 50)
	assertNoError(t, err)
	if affected != 1 {
		t.Errorf("Increment affected: expected 1, got %d", affected)
	}
	var balance int
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance").Value(ctx, &balance)
	assertNoError(t, err)
	if balance != 150 {
		t.Errorf("balance after increment: expected 150, got %d", balance)
	}

	// 多列自增（extra 成对传参）
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 10, "points", 5)
	assertNoError(t, err)
	type wallet struct {
		Balance int `db:"balance"`
		Points  int `db:"points"`
	}
	var w wallet
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance", "points").First(ctx, &w)
	assertNoError(t, err)
	if w.Balance != 160 || w.Points != 15 {
		t.Errorf("multi-column increment: expected {160 15}, got %+v", w)
	}

	// 自减
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Decrement(ctx, "balance", 30)
	assertNoError(t, err)
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance").Value(ctx, &balance)
	assertNoError(t, err)
	if balance != 130 {
		t.Errorf("balance after decrement: expected 130, got %d", balance)
	}

	// 保护机制：无 WHERE 拒绝
	_, err = db.Builder().Table("wallets").Increment(ctx, "balance", 1)
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("expected ErrUpdateWithoutWhere, got %v", err)
	}
	_, err = db.Builder().Table("wallets").Decrement(ctx, "balance", 1)
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("expected ErrUpdateWithoutWhere, got %v", err)
	}

	// Force 放行全表
	_, err = db.Builder().Table("wallets").Force().Increment(ctx, "points", 1)
	assertNoError(t, err)
	var points []int
	err = db.Builder().Table("wallets").OrderBy("id", "ASC").Pluck(ctx, &points, "points")
	assertNoError(t, err)
	if len(points) != 2 || points[0] != 16 || points[1] != 21 {
		t.Errorf("Force increment: expected [16 21], got %v", points)
	}

	// extra 参数非成对 → ErrIncrementColumns
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 1, "points")
	if !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("expected ErrIncrementColumns, got %v", err)
	}
	// extra 列名非 string → ErrIncrementColumns
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 1, 123, 5)
	if !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("expected ErrIncrementColumns, got %v", err)
	}
}

// TestSQLiteInteg_NewApi_InsertOrIgnoreUsing 验证 INSERT OR IGNORE INTO ... SELECT 冲突忽略。
func TestSQLiteInteg_NewApi_InsertOrIgnoreUsing(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteArchiveTable(t, db)

	// users 中 active：alice/bob/diana；alice 与 archive 冲突 → 忽略，bob/diana 插入
	affected, err := db.Builder().Table("archive").InsertOrIgnoreUsing(
		context.Background(),
		[]string{"name", "email"},
		func(q *Builder) {
			q.Table("users").Select("name", "email").Where("status", "=", "active")
		},
	)
	assertNoError(t, err)
	if affected != 2 {
		t.Errorf("InsertOrIgnoreUsing affected: expected 2, got %d", affected)
	}

	count, err := db.Builder().Table("archive").Count(context.Background())
	assertNoError(t, err)
	if count != 4 { // 原 2 + 新 2
		t.Errorf("archive count: expected 4, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_DeleteJoin 验证按关联条件删除（SQLite 编译为主键 IN 子查询）。
func TestSQLiteInteg_NewApi_DeleteJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	ctx := context.Background()

	// 删除存在金额 > 100 订单的用户：alice/bob/diana
	affected, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		DeleteJoin(ctx)
	assertNoError(t, err)
	if affected != 3 {
		t.Errorf("DeleteJoin affected: expected 3, got %d", affected)
	}
	count, err := db.Builder().Table("users").Count(ctx)
	assertNoError(t, err)
	if count != 2 { // charlie、eve
		t.Errorf("users after DeleteJoin: expected 2, got %d", count)
	}

	// 有 join 无 where：保护机制放行（join 本身限定了删除范围）
	affected, err = db.Builder().Table("users").
		Join("profiles", "users.id", "=", "profiles.user_id").
		DeleteJoin(ctx)
	assertNoError(t, err)
	if affected != 1 { // charlie（profiles 中有 user_id=3）
		t.Errorf("DeleteJoin join-only affected: expected 1, got %d", affected)
	}

	// 无 join 无 where：保护机制拒绝
	_, err = db.Builder().Table("users").DeleteJoin(ctx)
	if !errors.Is(err, ErrDeleteWithoutWhere) {
		t.Errorf("expected ErrDeleteWithoutWhere, got %v", err)
	}

	// Force 绕过保护但无 join：ErrDeleteJoinNoJoin
	_, err = db.Builder().Table("users").Force().DeleteJoin(ctx)
	if !errors.Is(err, ErrDeleteJoinNoJoin) {
		t.Errorf("expected ErrDeleteJoinNoJoin, got %v", err)
	}
}

// TestSQLiteInteg_Bug_EmptyWhereRawProtection 锁定 B1 修复：
// 空串/纯空白 WhereRaw 编译后无任何条件，不得计为有效 WHERE——
// 修复前 WhereRaw("").Delete 会绕过保护删除全表（实测 affected=2）。
func TestSQLiteInteg_Bug_EmptyWhereRawProtection(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
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

	// 数据未被破坏
	count, err := db.Builder().Table("users").Count(ctx)
	assertNoError(t, err)
	if count != 5 {
		t.Fatalf("users count after rejected ops: expected 5, got %d", count)
	}

	// 对照：非空 Raw 仍是有效限定/逃生口，保护逻辑不误伤
	affected, err := db.Builder().Table("users").WhereRaw("1 = 1").Delete(ctx)
	assertNoError(t, err)
	if affected != 5 {
		t.Errorf("Delete with WhereRaw(\"1 = 1\"): expected 5, got %d", affected)
	}
}

// TestSQLiteInteg_Bug_DanglingBoolean 锁定 M3 修复：
// 首条子句编译为空（空 Raw/空嵌套组）时，后续子句不得带悬挂连接词——
// 修复前产出 "WHERE AND ..." / "ON AND ..." / "HAVING AND ..." 语法错误。
func TestSQLiteInteg_Bug_DanglingBoolean(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)
	ctx := context.Background()

	// ON 侧：JoinBuilder 首条件为空 Raw（修复前 "ON AND ..." 语法错误）
	count, err := db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.Raw("").On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", 99)
		}).Count(ctx)
	assertNoError(t, err)
	if count != 3 { // profiles 覆盖 user 1,2,3
		t.Errorf("Join with leading empty Raw: expected 3, got %d", count)
	}

	// HAVING 侧：首条 HavingRaw 为空（修复前 "HAVING AND ..." 语法错误）
	var statuses []string
	err = db.Builder().Table("users").Select("status").GroupBy("status").
		HavingRaw("").Having("status", "=", "inactive").
		Pluck(ctx, &statuses, "status")
	assertNoError(t, err)
	if len(statuses) != 1 || statuses[0] != "inactive" {
		t.Errorf("Having after empty HavingRaw: expected [inactive], got %v", statuses)
	}

	// WHERE 侧：空 Raw 后的有效条件正常生效（修复前 "WHERE AND `id` = ?" 语法错误）
	affected, err := db.Builder().Table("users").WhereRaw("").Where("id", "=", 1).Delete(ctx)
	assertNoError(t, err)
	if affected != 1 {
		t.Errorf("Delete after empty WhereRaw: expected 1, got %d", affected)
	}
}

// TestSQLiteInteg_Bug_UpdateMixedJoinBindings 锁定 B2 修复（Update 与 toIncDec 路径）：
// 靠前 join 带 ON 值绑定 + 靠后 join 为带绑定的派生表时，
// 修复前占位符顺序（FROM 派生表先于 ON 条件）与绑定数组顺序（per-join 收集）错位，
// profiles.active 会被绑成子查询的 100 → 0 行命中。
func TestSQLiteInteg_Bug_UpdateMixedJoinBindings(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)
	setupSQLiteOrdersTable(t, db)
	ctx := context.Background()

	// profiles.active=99 → user 1,2,3；orders amount>100 → user 1,2,4；
	// 交集 1,2 且 status='active' → alice/bob 2 行
	buildChain := func() *Builder {
		sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
		return db.Builder().Table("users").
			JoinOn("profiles", func(j *JoinBuilder) {
				j.On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", 99)
			}).
			JoinSub(sub, "o", func(j *JoinBuilder) { j.On("o.user_id", "=", "users.id") }).
			Where("users.status", "=", "active")
	}

	affected, err := buildChain().Update(ctx, struct {
		Status string `db:"status"`
	}{Status: "vip"})
	assertNoError(t, err)
	if affected != 2 {
		t.Fatalf("Update mixed-join affected: expected 2, got %d", affected)
	}
	vipCount, err := db.Builder().Table("users").Where("status", "=", "vip").Count(ctx)
	assertNoError(t, err)
	if vipCount != 2 {
		t.Errorf("vip count: expected 2, got %d", vipCount)
	}

	// toIncDec 路径（同样的混合 join 绑定顺序）：alice 25→26, bob 30→31
	sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
	affected, err = db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", 99)
		}).
		JoinSub(sub, "o", func(j *JoinBuilder) { j.On("o.user_id", "=", "users.id") }).
		Where("users.status", "=", "vip").
		Increment(ctx, "age", 1)
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

// TestSQLiteInteg_Bug_DeleteJoinMixedBindings 锁定 B2 修复（DeleteJoin 路径）：
// SQLite 走主键 IN 子查询直译 JOIN（绑定顺序天然一致，本用例防回归）；
// 错位敏感方言为 PG（USING 形态），见 TestPgInteg_Bug_DeleteJoinMixedBindings。
func TestSQLiteInteg_Bug_DeleteJoinMixedBindings(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)
	setupSQLiteOrdersTable(t, db)
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
	if count != 3 { // charlie/diana/eve
		t.Errorf("users after DeleteJoin: expected 3, got %d", count)
	}
}

// TestSQLiteInteg_Bug_UpsertDefaultExcludeUniqueBy 锁定 m1 修复：
// updateColumns 为空时默认更新所有插入列并排除 uniqueBy 列；
// 全部插入列均为 uniqueBy 时退化为「冲突时不更新」（修复前生成空 SET → 语法错误）。
func TestSQLiteInteg_Bug_UpsertDefaultExcludeUniqueBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	ctx := context.Background()

	// 默认 updateColumns（nil）：冲突时更新 name/age（uniqueBy 的 email 不参与 SET）
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

	// 全部插入列均为 uniqueBy（自建表，避免 users.name 的 NOT NULL 约束干扰）：
	// 冲突时 DO NOTHING，不报错、不更新（修复前生成空 SET → 语法错误）
	mustExec(t, db, `CREATE TABLE tokens (id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT UNIQUE, hits INTEGER DEFAULT 0)`)
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

	// 全部列为 uniqueBy 且 key 不存在：正常插入新行（默认值生效）
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

// TestSQLiteInteg_Bug_UpsertExpressionValue 锁定 m2 修复：
// Upsert 的 Expression 字段值内联进 SQL（修复前作为绑定值传给驱动必报错）。
func TestSQLiteInteg_Bug_UpsertExpressionValue(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
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

// TestSQLiteInteg_Bug_MixedCaseOperatorDirection 锁定 m3 修复：
// 运算符与排序方向的大小写归一化不再仅对首字符小写生效——
// 修复前 "Like" 误报 ErrInvalidOperator、"Desc" 静默归一为 ASC。
func TestSQLiteInteg_Bug_MixedCaseOperatorDirection(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
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

// TestSQLiteInteg_Bug_OnSQLPanicRecovered 锁定 m5 修复：
// 慢 SQL 回调 panic 被 recover 隔离，不影响 Exec/Query 主流程。
func TestSQLiteInteg_Bug_OnSQLPanicRecovered(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	panicDao, err := NewDBDao(db.Pool(), "sqlite", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		panic("callback boom")
	}, "")
	assertNoError(t, err)
	ctx := context.Background()

	// Exec 路径
	_, err = panicDao.Builder().Table("users").Insert(ctx, struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}{Name: "frank", Email: "frank@test.com"})
	assertNoError(t, err)

	// Query 路径
	rows, err := panicDao.Query(ctx, `SELECT COUNT(*) AS cnt FROM users`)
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
