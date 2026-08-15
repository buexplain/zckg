// 本文件为 PostgreSQL 集成测试——写操作执行（Insert/Update/Delete/Upsert/Truncate 等）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "github.com/lib/pq"
	"strings"
	"testing"
	"time"
)

// TestPgInteg_InsertSingle 验证单条结构体插入：传入单个结构体，生成并执行 INSERT，确认数据正确写入。
func TestPgInteg_InsertSingle(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertBatch 验证批量插入：传入结构体切片，生成并执行单条 INSERT 多 VALUES，确认所有行正确写入。
func TestPgInteg_InsertBatch(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertPtrPartial 验证指针字段 nil 跳过：nil 指针字段不参与 INSERT，对应列应为数据库默认值（NULL）。
func TestPgInteg_InsertPtrPartial(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertBatchPartial 验证批量插入部分列为 nil：nil 指针字段不参与 INSERT，对应列使用数据库默认值。
func TestPgInteg_InsertBatchPartial(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   *int   `db:"age"`
		Email string `db:"email"`
	}
	age1 := 40
	data := []insertData{
		{Name: "frank", Age: &age1, Email: "frank@test.com"},
		{Name: "grace", Age: nil, Email: "grace@test.com"},
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), data)
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	// grace 的 age 应为 NULL
	var age *int
	err = db.Builder().Table("users").Select("age").Where("name", "=", "grace").Value(context.Background(), &age)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if age != nil {
		t.Errorf("expected NULL age for grace, got %d", *age)
	}

	// frank 的 age 应为 40
	var age2 *int
	err = db.Builder().Table("users").Select("age").Where("name", "=", "frank").Value(context.Background(), &age2)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if age2 == nil || *age2 != 40 {
		t.Errorf("expected age=40 for frank, got %v", age2)
	}
}

// TestPgInteg_InsertNilFields 验证 nil interface 字段跳过：仅插入非 nil 字段，其余列使用数据库默认值。
func TestPgInteg_InsertNilFields(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   any    `db:"age"`
		Email any    `db:"email"`
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), insertData{Name: "frank", Age: nil, Email: nil})
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}

	// age 和 email 应为默认值（NULL）
	type row struct {
		Name string `db:"name"`
		Age  *int   `db:"age"`
	}
	var rows []row
	err = db.Builder().Table("users").Select("name", "age").Where("name", "=", "frank").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Age != nil {
		t.Errorf("expected NULL age, got %d", *rows[0].Age)
	}
}

// TestPgInteg_InsertPtrAllNil 验证全 nil 指针插入：所有指针字段均为 nil 时返回 ErrNoFields 错误。
func TestPgInteg_InsertPtrAllNil(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertOrIgnore 验证 INSERT ... ON CONFLICT DO NOTHING：当 UNIQUE 约束冲突时不报错且不插入新行。
func TestPgInteg_InsertOrIgnore(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UpdateBasic 验证基础 UPDATE。
func TestPgInteg_UpdateBasic(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UpdatePtrPartial 验证指针字段部分更新：nil 指针字段不参与 SET。
func TestPgInteg_UpdatePtrPartial(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UpdateWithRaw 验证 NewExpression 表达式更新：字段值为 NewExpression("age" + 10)。
func TestPgInteg_UpdateWithRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_DeleteAll 验证 Force() 允许无条件全表删除。
func TestPgInteg_DeleteAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	_, err := db.Builder().Table("users").Force().Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after delete all, got %d", count)
	}
}

// TestPgInteg_Upsert 验证 INSERT ... ON CONFLICT DO UPDATE。
func TestPgInteg_Upsert(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UpsertBatch 验证批量 Upsert：多行数据 ON CONFLICT DO UPDATE。
func TestPgInteg_UpsertBatch(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type upsertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}

	// 批量 upsert：frank 为新行，alice 为冲突更新
	_, err := db.Builder().Table("users").Upsert(context.Background(),
		[]upsertData{
			{Name: "frank", Age: 40, Email: "frank@test.com"},
			{Name: "alice_upserted", Age: 99, Email: "alice@test.com"},
		},
		[]string{"email"},
		[]string{"name", "age"},
	)
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}

	// frank 应被插入
	count, _ := db.Builder().Table("users").Where("name", "=", "frank").Count(context.Background())
	if count != 1 {
		t.Errorf("expected frank inserted, got count=%d", count)
	}

	// alice 应被更新
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

// TestPgInteg_InsertUsing 验证 INSERT INTO ... SELECT 子查询插入。
func TestPgInteg_InsertUsing(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGSERIAL PRIMARY KEY,
		name VARCHAR(64),
		age  INT
	)`)

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

// TestPgInteg_InsertUsingExec 验证 InsertUsing 执行封装：INSERT INTO ... SELECT 并返回受影响行数。
func TestPgInteg_InsertUsingExec(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGSERIAL PRIMARY KEY,
		name VARCHAR(64),
		age  INT
	)`)

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

// TestPgInteg_Truncate 验证 TRUNCATE TABLE 清空表。
func TestPgInteg_Truncate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Builder().Table("users").Truncate(context.Background())
	if err != nil {
		t.Fatalf("Truncate error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after truncate, got %d", count)
	}
}

// TestPgInteg_InsertGetId 验证 InsertGetId 在 PostgreSQL 中不支持 LastInsertId，应返回错误。
func TestPgInteg_InsertGetId(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	_, err := db.Builder().Table("users").InsertGetId(context.Background(), insertData{Name: "frank", Age: 40, Email: "frank@test.com"})
	if err == nil {
		t.Fatalf("expected error for InsertGetId on postgres (no LastInsertId support), got nil")
	}
}

// TestPgInteg_UpdateFromJoin 验证 UPDATE ... FROM（PostgreSQL 专属多表更新语法）。
func TestPgInteg_UpdateFromJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

	count, _ := db.Builder().Table("users").Where("status", "=", "vip").Count(context.Background())
	if count != 3 {
		t.Errorf("expected 3 vip users, got %d", count)
	}
}

// TestPgInteg_InsertInvalidData 验证 Insert 传入非法类型（int、string、nil）时返回错误。
func TestPgInteg_InsertInvalidData(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertEmptySlice 验证 Insert 传入空切片时返回错误。
func TestPgInteg_InsertEmptySlice(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type insertData struct {
		Name string `db:"name"`
	}
	_, err := db.Builder().Table("users").Insert(context.Background(), []insertData{})
	if err == nil {
		t.Fatalf("expected error for empty slice, got nil")
	}
}

// TestPgInteg_InsertOrIgnoreInvalidData 验证 InsertOrIgnore 传入非法类型时返回错误。
func TestPgInteg_InsertOrIgnoreInvalidData(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	_, err := db.Builder().Table("users").InsertOrIgnore(context.Background(), 123)
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").InsertOrIgnore(context.Background(), nil)
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}
}

// TestPgInteg_UpsertInvalidData 验证 Upsert 传入非法类型时返回错误。
func TestPgInteg_UpsertInvalidData(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	_, err := db.Builder().Table("users").Upsert(context.Background(), 123, []string{"email"}, []string{"name"})
	if err == nil {
		t.Errorf("expected error for int data, got nil")
	}

	_, err = db.Builder().Table("users").Upsert(context.Background(), nil, []string{"email"}, []string{"name"})
	if err == nil {
		t.Errorf("expected error for nil data, got nil")
	}
}

// TestPgInteg_UpdateInvalidData 验证 Update 传入非法类型（切片、int、nil）时返回错误。
func TestPgInteg_UpdateInvalidData(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Bug_UpdateJoinDropsValueCondition 验证 PostgreSQL UPDATE + JOIN 含 value 条件时
// 条件不再被静默丢弃，且绑定参数顺序正确：仅更新 profiles.active=99 的用户。
func TestPgInteg_Bug_UpdateJoinDropsValueCondition(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)

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

	// user 2 (bob) 的 profiles.active=0，不应被更新
	type row struct {
		Name string `db:"name"`
	}
	var r row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 2).First(context.Background(), &r)
	if r.Name == "updated" {
		t.Errorf("BUG: user 2 (bob) should NOT be updated (profiles.active=0), but was updated due to dropped value condition")
	}
	// user 1 应被更新
	var r1 row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r1)
	if r1.Name != "updated" {
		t.Errorf("expected user 1 name 'updated', got %q", r1.Name)
	}
}

// TestPgInteg_Bug_UpdateJoinSubEmptyFrom 审查复现用例：
// PostgreSQL 的 CompileUpdate FROM 子句直接取 join.Table，JoinSub 时 Table 为空（数据在
// Sub/Alias），导致生成 FROM "" 的无效 SQL；嵌套 join 组的内层表也不会进入 FROM。
// 预期：FROM 与 SELECT 的 joinTable 一致，派生表编译为 (SELECT ...) AS alias。
func TestPgInteg_Bug_UpdateJoinSubEmptyFrom(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)

	// user_id=2 的 active=0，只有 user 1/3 的 active=99
	mustExec(t, db, `UPDATE profiles SET active = 0 WHERE user_id = 2`)

	type updateData struct {
		Name string `db:"name"`
	}
	sub := db.Builder().Table("profiles").Select("user_id").Where("active", "=", 99)

	// 编译层面：FROM 应为派生表而非空表名，绑定顺序 SET → 子查询 → ON
	sqlStr, args, err := db.Builder().Table("users").
		JoinSub(sub, "p", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "p.user_id")
		}).
		ToUpdate(updateData{Name: "updated"})
	assertNoError(t, err)
	assertSQL(t,
		`UPDATE "users" SET "name" = $1 FROM (SELECT "user_id" FROM "profiles" WHERE "active" = $2) AS "p" WHERE "users"."id" = "p"."user_id"`,
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

// TestPgInteg_Bug_InsertNilPtrInSlice 验证指针切片含 nil 元素时 Insert 返回错误而非 panic。
func TestPgInteg_Bug_InsertNilPtrInSlice(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Complex_InsertUsingJoinGroupHaving 验证 INSERT USING 复杂 SELECT（JOIN + WHERE + GROUP BY + HAVING）。
// 预期归档：alice(25), bob(30)
func TestPgInteg_Complex_InsertUsingJoinGroupHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGSERIAL PRIMARY KEY,
		name VARCHAR(64),
		age  INT
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

// TestPgInteg_UpdatePtrAllNil 验证全指针字段均为 nil 时更新应返回错误。
func TestPgInteg_UpdatePtrAllNil(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertBatchPtr 验证批量插入指针字段：部分指针为 nil 时写入 NULL。
func TestPgInteg_InsertBatchPtr(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertUsingInvalidSubquery
// InsertUsing 子查询缺少数据源或带非法运算符时直接返回错误，不生成非法 SQL。
func TestPgInteg_InsertUsingInvalidSubquery(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGSERIAL PRIMARY KEY,
		name VARCHAR(64)
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

// TestPgInteg_InsertOrIgnoreConflictZero
// 冲突未插入任何行时受影响行数为 0。
func TestPgInteg_InsertOrIgnoreConflictZero(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InsertGetIdExpression
// PostgreSQL 驱动不支持 LastInsertId，InsertGetId 执行返回错误（zcdb 已知限制，见 TestPgInteg_InsertGetId）；
// Expression 内联能力通过 ToInsert 编译结果验证：age=40 直接内联、不产生绑定参数。
func TestPgInteg_InsertGetIdExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   any    `db:"age"`
		Email string `db:"email"`
	}
	// 编译层：Expression 内联进 SQL，不产生绑定参数
	sqlStr, args, err := db.Builder().Table("users").ToInsert(
		insertData{Name: "frank", Age: NewExpression("40"), Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("ToInsert error: %v", err)
	}
	if !strings.Contains(sqlStr, "40") {
		t.Errorf("expected inlined 40 in SQL, got %s", sqlStr)
	}
	if len(args) != 2 {
		t.Errorf("expected 2 args (expression not bound), got %d: %v", len(args), args)
	}
	// 执行层：PG 驱动不支持 LastInsertId，InsertGetId 返回错误
	_, err = db.Builder().Table("users").InsertGetId(context.Background(),
		insertData{Name: "frank", Age: NewExpression("40"), Email: "frank@test.com"})
	if err == nil {
		t.Errorf("expected error for InsertGetId on postgres (no LastInsertId support), got nil")
	}
}

// TestPgInteg_InsertGetIdEmptyData
// 空结构体/空切片插入被拒绝（zcdb 不支持 default values 空插入）。
func TestPgInteg_InsertGetIdEmptyData(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UpsertEmptyUniqueBy
// PostgreSQL 需要 uniqueBy 生成 ON CONFLICT 目标，空值直接拒绝。
func TestPgInteg_UpsertEmptyUniqueBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_TruncateResetSequence
// TRUNCATE 带 RESTART IDENTITY 重置序列，清空后自增主键从头开始
// （PG 驱动不支持 LastInsertId，改用查询验证自增 id）。
func TestPgInteg_TruncateResetSequence(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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
	// 插入后 id 从头开始（1），证明序列已重置
	_, err = db.Builder().Table("users").Insert(context.Background(), struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}{Name: "after_truncate", Email: "after@test.com"})
	if err != nil {
		t.Fatalf("Insert error: %v", err)
	}
	var id int64
	err = db.Builder().Table("users").Select("id").Where("email", "=", "after@test.com").Value(context.Background(), &id)
	if err != nil {
		t.Fatalf("Value error: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1 after truncate (sequence reset), got %d", id)
	}
}

// TestPgInteg_JsonUpdate 验证 JSON 更新用 Update 值传 Expression
// （jsonb_set 内联），覆盖基本/嵌套/数组索引。
func TestPgInteg_JsonUpdate(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id        SERIAL PRIMARY KEY,
		jsonb_val JSONB
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (jsonb_val) VALUES
		('{"name":"alice","age":25,"address":{"city":"Shanghai"}}'),
		('["red","green"]'),
		('[{"name":"a"},{"name":"b"}]')`)

	type jsonUpdate struct {
		JsonbVal any `db:"jsonb_val"`
	}

	// 基本：jsonb_set 顶层字段
	_, err := db.Builder().Table("json_conv_test").Where("id", "=", 1).
		Update(context.Background(), jsonUpdate{JsonbVal: NewExpression(`jsonb_set(jsonb_val, '{age}', '26'::jsonb)`)})
	if err != nil {
		t.Fatalf("Update basic error: %v", err)
	}
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("(jsonb_val->>'age')::int = ?", 26).
		Count(context.Background())
	if err != nil {
		t.Fatalf("basic verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("basic update: expected age=26, got %d", count)
	}

	// 嵌套：jsonb_set 嵌套路径
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 1).
		Update(context.Background(), jsonUpdate{JsonbVal: NewExpression(`jsonb_set(jsonb_val, '{address,city}', '"Guangzhou"'::jsonb)`)})
	if err != nil {
		t.Fatalf("Update nested error: %v", err)
	}
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("jsonb_val->'address'->>'city' = ?", "Guangzhou").
		Count(context.Background())
	if err != nil {
		t.Fatalf("nested verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("nested update: expected city=Guangzhou, got %d", count)
	}

	// 数组索引：jsonb_set 修改数组元素
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 2).
		Update(context.Background(), jsonUpdate{JsonbVal: NewExpression(`jsonb_set(jsonb_val, '{0}', '"blue"'::jsonb)`)})
	if err != nil {
		t.Fatalf("Update array error: %v", err)
	}
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("jsonb_val->>0 = ?", "blue").
		Count(context.Background())
	if err != nil {
		t.Fatalf("array verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("array update: expected [0]=blue, got %d", count)
	}

	// 数组嵌套索引：jsonb_set 修改 $[0].name
	_, err = db.Builder().Table("json_conv_test").Where("id", "=", 3).
		Update(context.Background(), jsonUpdate{JsonbVal: NewExpression(`jsonb_set(jsonb_val, '{0,name}', '"x"'::jsonb)`)})
	if err != nil {
		t.Fatalf("Update array index error: %v", err)
	}
	count, err = db.Builder().Table("json_conv_test").
		WhereRaw("jsonb_val->0->>'name' = ?", "x").
		Count(context.Background())
	if err != nil {
		t.Fatalf("array index verify error: %v", err)
	}
	if count != 1 {
		t.Errorf("array index update: expected [0].name=x, got %d", count)
	}
}

// TestPgInteg_NewApi_DeleteJoin 验证 PG 的 DELETE ... USING 编译形态与真实执行。
func TestPgInteg_NewApi_DeleteJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		ToDeleteJoin()
	assertNoError(t, err)
	assertSQL(t,
		`DELETE FROM "users" USING "orders" WHERE "users"."id" = "orders"."user_id" AND "orders"."amount" > $1`,
		sqlStr)
	assertArgs(t, []any{100}, args)

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

// TestPgInteg_NewApi_InsertOrIgnoreUsing 验证 INSERT INTO ... SELECT ... ON CONFLICT DO NOTHING。
func TestPgInteg_NewApi_InsertOrIgnoreUsing(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgNewApiTables(t, db)
	setupPgArchiveTable(t, db)

	sqlStr, _, err := db.Builder().Table("archive").ToInsertOrIgnoreUsing([]string{"name", "email"}, func(q *Builder) {
		q.Table("users").Select("name", "email").Where("status", "=", "active")
	})
	assertNoError(t, err)
	assertSQL(t,
		`INSERT INTO "archive" ("name", "email") SELECT "name", "email" FROM "users" WHERE "status" = $1 ON CONFLICT DO NOTHING`,
		sqlStr)

	affected, err := db.Builder().Table("archive").InsertOrIgnoreUsing(
		context.Background(),
		[]string{"name", "email"},
		func(q *Builder) {
			q.Table("users").Select("name", "email").Where("status", "=", "active")
		},
	)
	assertNoError(t, err)
	if affected != 2 { // alice 冲突忽略，bob/diana 插入
		t.Errorf("affected: expected 2, got %d", affected)
	}
	count, err := db.Builder().Table("archive").Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("archive count: expected 4, got %d", count)
	}
}

// TestPgInteg_NewApi_IncrementDecrement 验证 PG 上 SET 在 JOIN 之前的绑定顺序（$N）与真实执行。
func TestPgInteg_NewApi_IncrementDecrement(t *testing.T) {
	db := openPgTestDB(t)
	setupPgNewApiTables(t, db)
	setupPgWalletsTable(t, db)

	ctx := context.Background()

	// 编译形态：SET col = col + $1（PG SET 先于 WHERE）
	sqlStr, args, err := db.Builder().Table("wallets").Where("id", "=", 1).ToIncrement([]string{"balance", "points"}, []any{10, 5})
	assertNoError(t, err)
	assertSQL(t, `UPDATE "wallets" SET "balance" = "balance" + $1, "points" = "points" + $2 WHERE "id" = $3`, sqlStr)
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

// TestPgInteg_Bug_EmptyWhereRawProtection 锁定 B1 修复：
// 空串/纯空白 WhereRaw 编译后无任何条件，不得计为有效 WHERE——
// 修复前 WhereRaw("").Delete 会绕过保护删除全表。
func TestPgInteg_Bug_EmptyWhereRawProtection(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
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

// TestPgInteg_Bug_DanglingBoolean 锁定 M3 修复：
// 首条子句编译为空时，后续子句不得带悬挂连接词（修复前 "WHERE/ON/HAVING AND ..." 语法错误）。
func TestPgInteg_Bug_DanglingBoolean(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)
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

	// HAVING 侧：首条 HavingRaw 为空（PG 的 HAVING 引用 GROUP BY 列合法）
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

// TestPgInteg_Bug_UpdateMixedJoinBindings 锁定 B2 修复（核心红绿用例）：
// 靠前 join 带 ON 值绑定 + 靠后 join 为带绑定的派生表时，
// 修复前 $N 编号顺序（FROM 派生表先于 ON 条件）与绑定数组顺序（per-join 收集）错位，
// profiles.active 会被绑成子查询的 100 → 0 行命中（修复后应为 2 行）。
func TestPgInteg_Bug_UpdateMixedJoinBindings(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)
	setupPgOrdersTable(t, db)
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

// TestPgInteg_Bug_DeleteJoinMixedBindings 锁定 B2 修复（DeleteJoin USING 路径，核心红绿用例）：
// 修复前 USING 表列表（含派生表 $N）先于 ON 条件编号，绑定按 per-join 收集 → 错位 0 行删除。
func TestPgInteg_Bug_DeleteJoinMixedBindings(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)
	setupPgOrdersTable(t, db)
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

// TestPgInteg_Bug_UpsertDefaultExcludeUniqueBy 锁定 m1 修复：
// updateColumns 为空时排除 uniqueBy 列；全部插入列均为 uniqueBy 时退化为 DO NOTHING
// （修复前生成空 SET → 语法错误）。
func TestPgInteg_Bug_UpsertDefaultExcludeUniqueBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
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

	// 全部插入列均为 uniqueBy：冲突时 DO NOTHING，不报错、不更新
	// （tokens 不在公共清表清单内，先 DROP 保证可重复运行）
	_, _ = db.Exec(ctx, `DROP TABLE IF EXISTS tokens`)
	mustExec(t, db, `CREATE TABLE tokens (id BIGSERIAL PRIMARY KEY, token VARCHAR(64) UNIQUE, hits BIGINT DEFAULT 0)`)
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

// TestPgInteg_Bug_UpsertExpressionValue 锁定 m2 修复：
// Upsert 的 Expression 字段值内联进 SQL（修复前作为绑定值传给驱动必报错）。
func TestPgInteg_Bug_UpsertExpressionValue(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
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

// TestPgInteg_Bug_MixedCaseOperatorDirection 锁定 m3 修复：
// 修复前 "Like" 误报 ErrInvalidOperator、"Desc" 静默归一为 ASC。
func TestPgInteg_Bug_MixedCaseOperatorDirection(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
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

// TestPgInteg_Bug_InsertGetIdUnsupported 锁定 m4 修复：
// lib/pq 不支持 LastInsertId——PG 下 InsertGetId 在执行前直接返回错误，
// 避免「插入成功但返回错误」的半成功状态。
func TestPgInteg_Bug_InsertGetIdUnsupported(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	ctx := context.Background()

	_, err := db.Builder().Table("users").InsertGetId(ctx, struct {
		Name  string `db:"name"`
		Email string `db:"email"`
	}{Name: "frank", Email: "frank@test.com"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("InsertGetId on PG: expected not-supported error, got %v", err)
	}

	// 未执行插入：行数不变（无半成功状态）
	count, err := db.Builder().Table("users").Count(ctx)
	assertNoError(t, err)
	if count != 5 {
		t.Errorf("users count after rejected InsertGetId: expected 5, got %d", count)
	}
}

// TestPgInteg_Bug_OnSQLPanicRecovered 锁定 m5 修复：
// 慢 SQL 回调 panic 被 recover 隔离，不影响 Exec/Query 主流程。
func TestPgInteg_Bug_OnSQLPanicRecovered(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	panicDao, err := NewDBDao(db.Pool(), "postgres", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
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
