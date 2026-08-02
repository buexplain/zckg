package zcdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// ==================== PostgreSQL 基础设施 ====================

// openPgTestDB 打开 PostgreSQL 连接，自动创建测试数据库（若不存在），然后清理并重建 users/orders 相关表，保证测试隔离。
// docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
func openPgTestDB(t *testing.T) *DBDao {
	t.Helper()

	pool, err := NewPool(PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable",
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	dao, err := NewDBDao(pool, "postgres", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	if err := dao.Pool().Ping(context.Background()); err != nil {
		t.Fatalf("failed to ping postgres: %v", err)
	}
	// 创建测试数据库（若不存在）
	exists, err := dao.Builder().Table("pg_database").Where("datname", "=", "zckg_test_integ").Exists(context.Background())
	if err != nil {
		t.Fatalf("failed to check database existence: %v", err)
	}
	if !exists {
		_, err = dao.Exec(context.Background(), "CREATE DATABASE zckg_test_integ")
		if err != nil {
			t.Fatalf("failed to create database: %v", err)
		}
	}
	_ = dao.Close()

	// 重新连接到测试数据库
	pool, err = NewPool(PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable dbname=zckg_test_integ",
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	dao, err = NewDBDao(pool, "postgres", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	if err := dao.Pool().Ping(context.Background()); err != nil {
		t.Fatalf("failed to ping postgres: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })

	// 清理旧表
	dropPgTables(t, dao)
	return dao
}

// dropPgTables 清除所有测试用表
func dropPgTables(t *testing.T, db *DBDao) {
	t.Helper()
	tables := []string{"users_archive", "orders", "profiles", "users",
		"numeric_test", "string_test", "bool_test", "datetime_test",
		"binary_test", "json_test", "uuid_test", "network_test",
		"array_test", "money_test", "json_conv_test",
		"byte_num_test", "byte_bool_test", "ptr_test", "json_err_test"}
	for _, table := range tables {
		_, _ = db.Exec(context.Background(), "DROP TABLE IF EXISTS "+table+" CASCADE")
	}
}

// setupPgUsersTable 创建 PostgreSQL 版 users 表并预填 5 条数据。
func setupPgUsersTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE users (
		id     BIGSERIAL PRIMARY KEY,
		name   VARCHAR(64) NOT NULL,
		age    INT NULL,
		email  VARCHAR(128) NULL UNIQUE,
		status VARCHAR(16) NOT NULL DEFAULT 'active'
	)`)
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES
		('alice', 25, 'alice@test.com', 'active'),
		('bob', 30, 'bob@test.com', 'active'),
		('charlie', 35, 'charlie@test.com', 'inactive'),
		('diana', 28, 'diana@test.com', 'active'),
		('eve', NULL, 'eve@test.com', 'inactive')`)
}

// setupPgProfilesTable 创建 PostgreSQL 版 profiles 表并预填数据。
func setupPgProfilesTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE profiles (
		id      BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		bio     VARCHAR(255) NULL,
		active  BIGINT DEFAULT 1
	)`)
	mustExec(t, db, `INSERT INTO profiles (user_id, bio, active) VALUES
		(1, 'Alice bio', 99),
		(2, 'Bob bio', 99),
		(3, 'Charlie bio', 99)`)
}

// setupPgOrdersTable 创建 PostgreSQL 版 orders 表并预填数据。
func setupPgOrdersTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE orders (
		id      BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL,
		amount  DECIMAL(10,2) NOT NULL,
		product VARCHAR(64) NULL
	)`)
	mustExec(t, db, `INSERT INTO orders (user_id, amount, product) VALUES
		(1, 50.00, 'Book'),
		(1, 120.00, 'Laptop'),
		(2, 80.00, 'Phone'),
		(2, 200.00, 'TV'),
		(3, 30.00, 'Pen'),
		(4, 150.00, 'Camera')`)
}

// ==================== Group 1: INSERT ====================

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

// ==================== Group 2: SELECT 基础查询 ====================

// TestPgInteg_SelectAll 验证无条件全表查询：不设任何 WHERE，SELECT * 应返回所有行。
func TestPgInteg_SelectAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectColumns 验证指定列查询：仅选择 name 和 age 列，通过 WHERE 定位单行。
func TestPgInteg_SelectColumns(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_SelectDistinct 验证 DISTINCT 去重查询。
func TestPgInteg_SelectDistinct(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// ==================== Group 3: SELECT 高级 WHERE ====================

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

// ==================== Group 4: JOIN ====================

// TestPgInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行。
func TestPgInteg_InnerJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		Join("orders", "users.id", "=", "orders.user_id").
		Distinct().
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留。
func TestPgInteg_LeftJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		LeftJoin("orders", "users.id", "=", "orders.user_id").
		Distinct().
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 users with left join, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积。
func TestPgInteg_CrossJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").CrossJoin("orders").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestPgInteg_JoinOnMultiple 验证 JoinOn 多 ON 条件：多个 On 调用生成 AND 连接的 ON 条件。
func TestPgInteg_JoinOnMultiple(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				On("users.name", "!=", "orders.product")
		}).
		Distinct().
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// users.name != orders.product is always true (names differ from products)
	if len(rows) != 4 {
		t.Errorf("expected 4 users, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：ON 子句附加额外过滤。
func TestPgInteg_JoinOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Where("orders.amount", ">", 100)
		}).
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows with amount > 100, got %d", len(rows))
	}
}

// ==================== Group 5: 聚合/分组/排序 ====================

// TestPgInteg_HavingBetween 验证 HAVING BETWEEN：分组后使用 BETWEEN 过滤。
func TestPgInteg_HavingBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingBetween("SUM(amount)", 50, 200).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user 1: 170, user 2: 280, user 3: 30, user 4: 150 → BETWEEN 50 AND 200: user 1(170), user 4(150)
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤。
func TestPgInteg_GroupByHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > $1", 100).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 groups with total > 100, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_OrderByLimitOffset 验证排序+分页：ORDER BY + LIMIT + OFFSET。
func TestPgInteg_OrderByLimitOffset(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotNull("age").
		OrderBy("age", "DESC").
		Limit(2).
		Offset(1).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "diana" {
		t.Errorf("expected [bob, diana], got %v", rows)
	}
}

// TestPgInteg_ForPage 验证 ForPage 便捷分页。
func TestPgInteg_ForPage(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(2, 2).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "charlie" || rows[1].Name != "diana" {
		t.Errorf("expected [charlie, diana], got %v", rows)
	}
}

// TestPgInteg_InRandomOrder 验证随机排序：ORDER BY RANDOM()。
func TestPgInteg_InRandomOrder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").InRandomOrder().Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

// TestPgInteg_SelectRaw 验证 SelectRaw 原始表达式（COUNT(*)）。
func TestPgInteg_SelectRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

// ==================== Group 6: 子查询 ====================

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

// TestPgInteg_SelectSubquery 验证 SELECT 子句中的子查询：子查询结果作为额外列返回。
func TestPgInteg_SelectSubquery(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").WhereRaw(`"orders"."user_id" = "users"."id"`)
	type row struct {
		Name        string `db:"name"`
		OrdersCount int    `db:"orders_count"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name").
		SelectSubquery(sub, "orders_count").
		OrderBy("name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// alice: 2 orders, bob: 2, charlie: 1, diana: 1, eve: 0
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	if rows[0].Name != "alice" || rows[0].OrdersCount != 2 {
		t.Errorf("expected alice/2, got %s/%d", rows[0].Name, rows[0].OrdersCount)
	}
}

// TestPgInteg_FromSub 验证 FROM 子查询（派生表）。
func TestPgInteg_FromSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sub := db.Builder().Table("users").Select("name", "age").WhereNotNull("age")
	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().
		FromSub(sub, "sub").
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

// ==================== Group 7: UPDATE ====================

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

// ==================== Group 8: DELETE ====================

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

// ==================== Group 9: UPSERT / INSERT USING / UNION / TRUNCATE ====================

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

// TestPgInteg_Union 验证 UNION 去重合并。
func TestPgInteg_Union(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.Union(q2).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 union results, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_UnionAll 验证 UNION ALL 不去重合并。
func TestPgInteg_UnionAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.UnionAll(q2).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 6 {
		t.Errorf("expected 6 union all results, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_Clone 验证 Builder 克隆后独立查询。
func TestPgInteg_Clone(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	base := db.Builder().Table("users").Where("status", "=", "active")

	type row struct {
		Name string `db:"name"`
	}
	var rows1, rows2 []row
	err := base.Clone().Where("age", ">", 25).Select("name").OrderBy("age", "ASC").Find(context.Background(), &rows1)
	if err != nil {
		t.Fatalf("clone1 error: %v", err)
	}
	err = base.Clone().Where("age", "<", 28).Select("name").OrderBy("age", "ASC").Find(context.Background(), &rows2)
	if err != nil {
		t.Fatalf("clone2 error: %v", err)
	}

	// clone1: active and age>25 → bob(30), diana(28) = 2
	if len(rows1) != 2 {
		t.Errorf("expected 2 rows for clone1, got %d: %v", len(rows1), rows1)
	}
	// clone2: active and age<28 → alice(25) = 1
	if len(rows2) != 1 {
		t.Errorf("expected 1 row for clone2, got %d: %v", len(rows2), rows2)
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

// ==================== Group 10: builder_exec 终端方法 ====================

// TestPgInteg_First 验证 First 查询第一条记录：有数据时填充结构体并返回 nil。
func TestPgInteg_First(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var r row
	err := db.Builder().Table("users").Select("name", "age").OrderBy("id", "ASC").First(context.Background(), &r)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if r.Name != "alice" || r.Age != 25 {
		t.Errorf("expected alice/25, got %s/%d", r.Name, r.Age)
	}
}

// TestPgInteg_FirstNotFound 验证 First 无数据时返回 sql.ErrNoRows。
func TestPgInteg_FirstNotFound(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).First(context.Background(), &r)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestPgInteg_FirstLimit 验证 First 自动限制为 1 条：即使有多行匹配也只返回第一条。
func TestPgInteg_FirstLimit(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("status", "=", "active").OrderBy("id", "ASC").First(context.Background(), &r)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if r.Name != "alice" {
		t.Errorf("expected alice, got %s", r.Name)
	}
}

// TestPgInteg_Exists 验证 Exists 有数据时返回 true。
func TestPgInteg_Exists(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}
}

// TestPgInteg_ExistsFalse 验证 Exists 无匹配数据时返回 false。
func TestPgInteg_ExistsFalse(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("id", "=", 999).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false, got true")
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

// TestPgInteg_Paginate 验证 Paginate 分页查询：第二页返回正确数据。
func TestPgInteg_Paginate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	b := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(2, 2)
	total, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if len(rows) >= 2 && (rows[0].Name != "charlie" || rows[1].Name != "diana") {
		t.Errorf("expected [charlie, diana], got %v", rows)
	}
}

// TestPgInteg_PaginateDefault 验证 Paginate 未设置分页参数时使用默认值（第 1 页，每页 20 条）。
func TestPgInteg_PaginateDefault(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	b := db.Builder().Table("users").Select("name").OrderBy("id", "ASC")
	total, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	// 默认 ForPage(1, 20)，5 条数据全部返回
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

// ==================== Group 11: PostgreSQL 专属能力 ====================

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

// TestPgInteg_LockForUpdate 验证 SELECT ... FOR UPDATE 语法可执行（事务内）。
func TestPgInteg_LockForUpdate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type row struct {
			Name string `db:"name"`
		}
		var rows []row
		err := db.Builder().Table("users").Select("name").Where("id", "=", 1).LockForUpdate().Find(ctx, &rows)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			return fmt.Errorf("expected alice, got %v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestPgInteg_LockForUpdate error: %v", err)
	}
}

// TestPgInteg_SharedLock 验证 SELECT ... FOR SHARE 语法可执行（事务内）。
func TestPgInteg_SharedLock(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type row struct {
			Name string `db:"name"`
		}
		var rows []row
		err := db.Builder().Table("users").Select("name").Where("id", "=", 1).SharedLock().Find(ctx, &rows)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			return fmt.Errorf("expected alice, got %v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestPgInteg_SharedLock error: %v", err)
	}
}

// TestPgInteg_UnionLockNotSupported 验证 PostgreSQL UNION + LOCK 返回错误（PostgreSQL 不支持此组合）。
func TestPgInteg_UnionLockNotSupported(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.Union(q2).LockForUpdate().Find(context.Background(), &rows)
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestPgInteg_UnionSharedLockNotSupported 验证 PostgreSQL UNION + SharedLock 返回错误。
func TestPgInteg_UnionSharedLockNotSupported(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.UnionAll(q2).SharedLock().Find(context.Background(), &rows)
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestPgInteg_ConcurrentCompile 验证多 goroutine 并发执行查询时 $N 占位符编号正确。
func TestPgInteg_ConcurrentCompile(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			type row struct {
				Name string `db:"name"`
			}
			var rows []row
			// 多条件查询，$N 编号必须从 $1 开始递增
			err := db.Builder().Table("users").
				Select("name").
				Where("status", "=", "active").
				Where("age", ">", 25).
				OrderBy("name", "ASC").
				Find(context.Background(), &rows)
			if err != nil {
				errCh <- fmt.Errorf("goroutine[%d]: %v", idx, err)
				return
			}
			// active 且 age>25: bob(30), diana(28) → 2 条
			if len(rows) != 2 {
				errCh <- fmt.Errorf("goroutine[%d]: expected 2 rows, got %d", idx, len(rows))
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发查询失败: %v", err)
	}
}

// ==================== Group 12: Transaction ====================

// TestPgInteg_TransactionCommit 验证事务提交：回调返回 nil 时，修改持久化。
func TestPgInteg_TransactionCommit(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type updateData struct {
			Name string `db:"name"`
		}
		_, err := db.Builder().Table("users").Where("id", "=", 1).Update(ctx, updateData{Name: "alice_tx"})
		return err
	})
	if err != nil {
		t.Fatalf("Transaction error: %v", err)
	}

	// 提交后数据应持久化
	type row struct {
		Name string `db:"name"`
	}
	var r row
	err = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if r.Name != "alice_tx" {
		t.Errorf("expected alice_tx after commit, got %s", r.Name)
	}
}

// TestPgInteg_TransactionRollback 验证事务回滚：回调返回 error 时，修改被撤销。
func TestPgInteg_TransactionRollback(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type updateData struct {
			Name string `db:"name"`
		}
		_, err := db.Builder().Table("users").Where("id", "=", 1).Update(ctx, updateData{Name: "alice_rolled_back"})
		if err != nil {
			return err
		}
		// 主动返回错误触发回滚
		return fmt.Errorf("intentional error")
	})
	if err == nil {
		t.Fatalf("expected error from Transaction, got nil")
	}

	// 回滚后数据应保持不变
	type row struct {
		Name string `db:"name"`
	}
	var r row
	err = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if r.Name != "alice" {
		t.Errorf("expected alice after rollback, got %s", r.Name)
	}
}

// TestPgInteg_TransactionNested 验证嵌套事务传播：内层事务复用外层事务，提交后整体生效。
func TestPgInteg_TransactionNested(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(outerCtx context.Context) error {
		type updateData struct {
			Name string `db:"name"`
		}
		_, err := db.Builder().Table("users").Where("id", "=", 1).Update(outerCtx, updateData{Name: "alice_nested"})
		if err != nil {
			return err
		}
		// 嵌套调用：应复用外层事务
		return db.Transaction(outerCtx, func(innerCtx context.Context) error {
			_, err := db.Builder().Table("users").Where("id", "=", 2).Update(innerCtx, updateData{Name: "bob_nested"})
			return err
		})
	})
	if err != nil {
		t.Fatalf("Transaction error: %v", err)
	}

	// 两次修改都应生效
	type row struct {
		Name string `db:"name"`
	}
	var r1, r2 row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r1)
	if r1.Name != "alice_nested" {
		t.Errorf("expected alice_nested, got %s", r1.Name)
	}
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 2).First(context.Background(), &r2)
	if r2.Name != "bob_nested" {
		t.Errorf("expected bob_nested, got %s", r2.Name)
	}
}

// TestPgInteg_TransactionPanicRollback 验证事务回调 panic 时，事务应自动回滚。
func TestPgInteg_TransactionPanicRollback(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// 回调中 panic，事务应自动回滚
	func() {
		defer func() { recover() }()
		_ = db.Transaction(context.Background(), func(ctx context.Context) error {
			type updateData struct {
				Name string `db:"name"`
			}
			_, _ = db.Builder().Table("users").Where("id", "=", 1).Update(ctx, updateData{Name: "should_rollback"})
			panic("test panic in transaction")
		})
	}()

	// 验证数据未被修改（事务已回滚）
	type row struct {
		Name string `db:"name"`
	}
	var r row
	_ = db.Builder().Table("users").Select("name").Where("id", "=", 1).First(context.Background(), &r)
	if r.Name == "should_rollback" {
		t.Error("事务 panic 后数据未回滚，修改已持久化")
	}
}

// ==================== Group 13: any 入参约束验证 ====================

// TestPgInteg_FirstInvalidDest 验证 First 传入非指针类型时返回错误。
func TestPgInteg_FirstInvalidDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").First(context.Background(), r)
	if err == nil {
		t.Fatalf("expected error for non-pointer dest, got nil")
	}
}

// TestPgInteg_FirstNilDest 验证 First 传入 nil 时返回错误。
func TestPgInteg_FirstNilDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Builder().Table("users").Select("name").First(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil dest, got nil")
	}
}

// TestPgInteg_FirstIntPtrDest 验证 First 传入非结构体指针（*int）时返回错误。
func TestPgInteg_FirstIntPtrDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").First(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest, got nil")
	}
}

// TestPgInteg_FindInvalidDest 验证 Find 传入 *int（非结构体切片指针）时返回错误。
func TestPgInteg_FindInvalidDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Find, got nil")
	}
}

// TestPgInteg_FindNonPointerDest 验证 Find 传入非指针（[]struct）时返回错误。
func TestPgInteg_FindNonPointerDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Find(context.Background(), rows)
	if err == nil {
		t.Fatalf("expected error for non-pointer slice dest, got nil")
	}
}

// TestPgInteg_FindIntPtrDest 验证 Find 传入 *[]int（非结构体切片指针）时返回错误。
func TestPgInteg_FindIntPtrDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Fatalf("expected error for *[]int dest in Find, got nil")
	}
}

// TestPgInteg_PaginateInvalidDest 验证 Paginate 传入 *int（非结构体切片指针）时返回错误。
func TestPgInteg_PaginateInvalidDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var n int
	_, err := db.Builder().Table("users").Select("name").Paginate(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Paginate, got nil")
	}
}

// TestPgInteg_ValueNoRows 验证 Value 无匹配数据时返回 sql.ErrNoRows。
func TestPgInteg_ValueNoRows(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).Value(context.Background(), &name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
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

// ==================== Group 15: Bug 验证集成测试 ====================

// TestPgInteg_Bug_CountWithUnion 验证 Count() 对 UNION 查询返回正确结果。
// 数据：active 用户 3 人，age>25 用户 3 人 (eve age 为 NULL 不计入)。
// UNION ALL 不去重，正确总数应为 6。修复前生成无效 SQL 报错。
func TestPgInteg_Bug_CountWithUnion(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	union := db.Builder().Table("users").Where("age", ">", 25)
	b := db.Builder().Table("users").Where("status", "=", "active").UnionAll(union)

	count, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 6 {
		t.Errorf("Count with UNION ALL expected 6, got %d", count)
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

// TestPgInteg_Bug_SelectSubFromSubBindingOrder 验证 SELECT 子查询与 FROM 子查询同时含绑定参数时，
// 收集顺序与 SQL 占位符顺序一致（SELECT 子查询在前，FROM 子查询在后）。
func TestPgInteg_Bug_SelectSubFromSubBindingOrder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	// SELECT 子查询：统计 amount > 100 的订单数（标量，绑定 100），结果为 3 (120,200,150)
	scalarSub := db.Builder().Table("orders").SelectRaw("COUNT(*)").Where("amount", ">", 100)
	// FROM 子查询：age > 25 的用户（绑定 25），结果为 3 人 (bob,charlie,diana)
	fromSub := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name     string `db:"name"`
		BigCount int    `db:"big_count"`
	}
	var rows []row
	err := db.Builder().
		Select("name").
		SelectSubquery(scalarSub, "big_count").
		FromSub(fromSub, "t").
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

// TestPgInteg_Bug_CloneNestedBuilder 验证 Clone 对嵌套 Builder（UNION 子查询）深拷贝：
// 修改原始嵌套 Builder 后，克隆体不受影响。
func TestPgInteg_Bug_CloneNestedBuilder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// unionSub 作为嵌套 Builder 被 base 引用
	unionSub := db.Builder().Table("users").Where("status", "=", "active") // 3 人
	base := db.Builder().Table("users").Where("age", ">", 25).UnionAll(unionSub)

	clone := base.Clone()

	// 修改原始嵌套 Builder：若 Clone 为浅拷贝，clone 会一并受影响
	unionSub.Where("age", ">", 100) // active 且 age>100 → 0 人

	// clone 应仍为 age>25 (3) UNION ALL active (3) = 6
	count, err := clone.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 6 {
		t.Errorf("BUG: clone affected by nested builder mutation: expected 6, got %d", count)
	}
}

// TestPgInteg_Bug_TransactionCommitError 验证 Commit 失败时返回提交错误本身，
// 而非因再次 Rollback 产生的误导性 ErrTxDone 错误。
func TestPgInteg_Bug_TransactionCommitError(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type insertData struct {
			Name  string `db:"name"`
			Email string `db:"email"`
		}
		// 故意违反唯一约束使事务进入 aborted 状态，但忽略该错误
		_, _ = db.Builder().Table("users").Insert(ctx, insertData{Name: "dup", Email: "alice@test.com"})
		// 回调返回 nil，触发 Commit；此时 PG 事务已 aborted，Commit 必然失败
		return nil
	})
	if err == nil {
		t.Fatalf("expected commit failure, got nil")
	}
	// 修复后应返回 commit 错误，而非 "transaction has already been committed or rolled back"
	if strings.Contains(err.Error(), "already been committed or rolled back") {
		t.Errorf("BUG: got misleading ErrTxDone error instead of commit error: %v", err)
	}
	if !strings.Contains(err.Error(), "commit failed") {
		t.Errorf("expected error to mention commit failure, got: %v", err)
	}
}

// ==================== Group 16: OR 条件 ====================

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

// ==================== Group 17: RIGHT JOIN ====================

// TestPgInteg_RightJoin 验证 RIGHT JOIN：右表所有行都保留。
func TestPgInteg_RightJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name *string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		RightJoin("orders", "users.id", "=", "orders.user_id").
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// orders 有 6 行，所有 user_id 都有效 → 6 行
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// TestPgInteg_RightJoinOn 验证 RightJoinOn 多条件：RIGHT JOIN + 回调式 ON 条件。
func TestPgInteg_RightJoinOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name *string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		RightJoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Where("orders.amount", ">", 100)
		}).
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// ON users.id=orders.user_id OR orders.amount>100 → 6 rows
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// ==================== Group 18: HAVING 子句 ====================

// TestPgInteg_HavingBasic 验证 Having 基本用法：HAVING SUM(amount) > 100。
func TestPgInteg_HavingBasic(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170, user2=280, user4=150 > 100 → 3 groups
	if len(rows) != 3 {
		t.Errorf("expected 3 groups, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_OrHaving 验证 OrHaving：HAVING SUM>200 OR SUM<50。
func TestPgInteg_OrHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 200).
		OrHaving("SUM(amount)", "<", 50).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170, user2=280, user3=30, user4=150
	// SUM>200: user2(280); SUM<50: user3(30) → 2
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_HavingNotBetween 验证 HavingNotBetween：HAVING SUM NOT BETWEEN 100 AND 200。
func TestPgInteg_HavingNotBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingNotBetween("SUM(amount)", 100, 200).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170 [100,200], user2=280 outside, user3=30 outside, user4=150 [100,200]
	// NOT BETWEEN → user2, user3 → 2
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// ==================== Group 19: 高级 ORDER BY ====================

// TestPgInteg_OrderByDesc 验证 OrderByDesc 降序排序。
func TestPgInteg_OrderByDesc(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotNull("age").
		OrderByDesc("age").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// age DESC: charlie(35), bob(30), diana(28), alice(25)
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if rows[0].Name != "charlie" {
		t.Errorf("expected first row charlie, got %s", rows[0].Name)
	}
}

// TestPgInteg_OrderByRaw 验证 OrderByRaw 原始 SQL 排序。
func TestPgInteg_OrderByRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotNull("age").
		OrderByRaw("age DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if rows[0].Name != "charlie" {
		t.Errorf("expected first row charlie, got %s", rows[0].Name)
	}
}

// ==================== Group 20: JoinBuilder 高级方法 ====================

// TestPgInteg_JoinOnOrWhere 验证 JoinBuilder.OrWhere：JOIN ON 中的 OR 值条件。
func TestPgInteg_JoinOnOrWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				OrWhere("orders.amount", ">", 140)
		}).
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// INNER JOIN ON id=user_id OR amount>140: 6 条匹配 id + 额外 amount>140 交叉匹配 → 14
	if len(rows) != 14 {
		t.Errorf("expected 14 rows, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_JoinOnRaw 验证 JoinBuilder.Raw：JOIN ON 中的原始 SQL 条件。
func TestPgInteg_JoinOnRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Raw("orders.amount > $1", 100)
		}).
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// users.id=orders.user_id AND orders.amount>100 → 3 (Laptop, TV, Camera)
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// ==================== 复杂SQL能力验证 ====================

// TestPgInteg_Complex_FromSubJoinGroupHaving 验证 FROM子查询 + JOIN + GROUP BY + HAVING 组合。
// 预期：bob(2单,280), alice(2单,170)
func TestPgInteg_Complex_FromSubJoinGroupHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sub := db.Builder().Table("orders").
		Select("user_id").
		SelectRaw("COUNT(*) AS order_count").
		SelectRaw("SUM(amount) AS total_amount").
		GroupBy("user_id").
		Having("COUNT(*)", ">=", 2)

	type row struct {
		Name       string  `db:"name"`
		OrderCount int     `db:"order_count"`
		TotalAmt   float64 `db:"total_amount"`
	}
	var rows []row
	err := db.Builder().
		Select("users.name", "t.order_count", "t.total_amount").
		FromSub(sub, "t").
		JoinOn("users", func(j *JoinBuilder) {
			j.On("t.user_id", "=", "users.id")
		}).
		OrderBy("t.total_amount", "DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "bob" || rows[0].OrderCount != 2 || rows[0].TotalAmt != 280 {
		t.Errorf("row[0]: expected bob/2/280, got %v", rows[0])
	}
	if rows[1].Name != "alice" || rows[1].OrderCount != 2 || rows[1].TotalAmt != 170 {
		t.Errorf("row[1]: expected alice/2/170, got %v", rows[1])
	}
}

// TestPgInteg_Complex_SelectSubWhereInSubNestedWhere 验证 SELECT子查询列 + WHERE IN子查询 + 嵌套WHERE。
// 预期：bob(30,2), diana(28,1), alice(25,2)
func TestPgInteg_Complex_SelectSubWhereInSubNestedWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	countSub := db.Builder().Table("orders").
		SelectRaw("COUNT(*)").
		WhereRaw("orders.user_id = users.id")

	type row struct {
		Name       string `db:"name"`
		Age        int    `db:"age"`
		OrderCount int    `db:"order_count"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name", "age").
		SelectSubquery(countSub, "order_count").
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

// TestPgInteg_Complex_UnionAllJoinOrderBy 验证 UNION ALL + JOIN 组合。
// 预期合并后 4 行。
func TestPgInteg_Complex_UnionAllJoinOrderBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	bigSpender := db.Builder().Table("users").
		Select("users.name", "users.age").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id")
		}).
		Where("orders.amount", ">", 150)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name", "age").
		Where("status", "=", "active").
		UnionAll(bigSpender).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
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

// TestPgInteg_Complex_NestedSubqueryLockForUpdate 验证多层嵌套子查询 + LOCK FOR UPDATE。
// 预期：alice, bob
func TestPgInteg_Complex_NestedSubqueryLockForUpdate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").
				Select("user_id").
				GroupBy("user_id").
				HavingRaw("AVG(amount) > $1", 75).
				HavingRaw("COUNT(*) >= $2", 2)
		}).
		OrderBy("name", "ASC").
		LockForUpdate().
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "alice" {
		t.Errorf("row[0]: expected alice, got %s", rows[0].Name)
	}
	if rows[1].Name != "bob" {
		t.Errorf("row[1]: expected bob, got %s", rows[1].Name)
	}
}

// ==================== SchemaInspector 集成测试 ====================

// TestPgInteg_SchemaInspector_Tables 验证 Tables 返回表名和注释。
func TestPgInteg_SchemaInspector_Tables(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, `CREATE TABLE "test_schema_a" ("id" INT NOT NULL PRIMARY KEY)`)
	mustExec(t, db, `COMMENT ON TABLE "test_schema_a" IS '表A注释'`)
	mustExec(t, db, `CREATE TABLE "test_schema_b" ("id" INT NOT NULL PRIMARY KEY)`)
	mustExec(t, db, `COMMENT ON TABLE "test_schema_b" IS '表B注释'`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS "test_schema_a"`)
		mustExec(t, db, `DROP TABLE IF EXISTS "test_schema_b"`)
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	tables, err := inspector.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}

	found := map[string]string{}
	for _, tbl := range tables {
		if tbl.Name == "test_schema_a" || tbl.Name == "test_schema_b" {
			found[tbl.Name] = tbl.Comment
		}
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 test tables, got %d: %v", len(found), tables)
	}
	if found["test_schema_a"] != "表A注释" {
		t.Errorf("test_schema_a: expected '表A注释', got %q", found["test_schema_a"])
	}
	if found["test_schema_b"] != "表B注释" {
		t.Errorf("test_schema_b: expected '表B注释', got %q", found["test_schema_b"])
	}
}

// TestPgInteg_SchemaInspector_Columns 验证 Columns 返回字段名、类型、注释、Nullable、Default。
func TestPgInteg_SchemaInspector_Columns(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, `CREATE TABLE "test_columns" (
		"id" SERIAL PRIMARY KEY,
		"name" VARCHAR(64) NOT NULL,
		"age" INTEGER,
		"status" VARCHAR(16) NOT NULL DEFAULT 'active'
	)`)
	mustExec(t, db, `COMMENT ON TABLE "test_columns" IS '测试字段表'`)
	mustExec(t, db, `COMMENT ON COLUMN "test_columns"."name" IS '用户名'`)
	mustExec(t, db, `COMMENT ON COLUMN "test_columns"."age" IS '年龄'`)
	mustExec(t, db, `COMMENT ON COLUMN "test_columns"."status" IS '状态'`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS "test_columns"`)
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "test_columns")
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}

	checks := []struct {
		name     string
		typ      string
		comment  string
		nullable bool
		hasDef   bool
		defVal   string
	}{
		{"id", "integer", "", false, true, ""},
		{"name", "character varying(64)", "用户名", false, false, ""},
		{"age", "integer", "年龄", true, false, ""},
		{"status", "character varying(16)", "状态", false, true, "'active'::character varying"},
	}
	for i, c := range checks {
		if columns[i].Name != c.name {
			t.Errorf("col[%d]: expected name %q, got %q", i, c.name, columns[i].Name)
		}
		if columns[i].Type != c.typ {
			t.Errorf("col[%d] %s: expected type %q, got %q", i, c.name, c.typ, columns[i].Type)
		}
		if columns[i].Comment != c.comment {
			t.Errorf("col[%d] %s: expected comment %q, got %q", i, c.name, c.comment, columns[i].Comment)
		}
		if columns[i].Nullable != c.nullable {
			t.Errorf("col[%d] %s: expected nullable=%v, got %v", i, c.name, c.nullable, columns[i].Nullable)
		}
		if c.hasDef && columns[i].Default == nil {
			t.Errorf("col[%d] %s: expected default, got nil", i, c.name)
		}
		if c.defVal != "" && columns[i].Default != nil && *columns[i].Default != c.defVal {
			t.Errorf("col[%d] %s: expected default %q, got %q", i, c.name, c.defVal, *columns[i].Default)
		}
	}
}

// ==================== nullSafeField 集成测试 ====================

// TestPgInteg_NullSafeField_NumericTypes 验证数值类型的 NULL 安全扫描。
// 覆盖：SMALLINT, INTEGER, BIGINT, SERIAL, REAL, DOUBLE PRECISION, NUMERIC
func TestPgInteg_NullSafeField_NumericTypes(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE numeric_test (
		id       SERIAL PRIMARY KEY,
		small_val SMALLINT,
		int_val  INTEGER,
		big_val  BIGINT,
		real_val REAL,
		double_val DOUBLE PRECISION,
		numeric_val NUMERIC(10,2)
	)`)

	// 插入有值行和 NULL 行
	mustExec(t, db, `INSERT INTO numeric_test (small_val, int_val, big_val, real_val, double_val, numeric_val)
		VALUES (32767, 2147483647, 9223372036854775807, 3.14, 2.718281828, 99999.99)`)
	mustExec(t, db, `INSERT INTO numeric_test (small_val, int_val, big_val, real_val, double_val, numeric_val)
		VALUES (NULL, NULL, NULL, NULL, NULL, NULL)`)

	type numericRow struct {
		ID      int     `db:"id"`
		Small   int16   `db:"small_val"`
		Int     int     `db:"int_val"`
		Big     int64   `db:"big_val"`
		Real    float32 `db:"real_val"`
		Double  float64 `db:"double_val"`
		Numeric float64 `db:"numeric_val"`
	}

	var results []numericRow
	err := db.Builder().Table("numeric_test").
		Select("id", "small_val", "int_val", "big_val", "real_val", "double_val", "numeric_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.Small != 32767 {
		t.Errorf("Small: expected 32767, got %d", r.Small)
	}
	if r.Int != 2147483647 {
		t.Errorf("Int: expected 2147483647, got %d", r.Int)
	}
	if r.Big != 9223372036854775807 {
		t.Errorf("Big: expected max int64, got %d", r.Big)
	}
	if r.Double != 2.718281828 {
		t.Errorf("Double: expected 2.718281828, got %f", r.Double)
	}
	if r.Numeric != 99999.99 {
		t.Errorf("Numeric: expected 99999.99, got %f", r.Numeric)
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Small != 0 {
		t.Errorf("NULL Small: expected 0, got %d", nullRow.Small)
	}
	if nullRow.Big != 0 {
		t.Errorf("NULL Big: expected 0, got %d", nullRow.Big)
	}
}

// TestPgInteg_NullSafeField_StringTypes 验证字符串类型的 NULL 安全扫描。
// 覆盖：CHAR, VARCHAR, TEXT
func TestPgInteg_NullSafeField_StringTypes(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE string_test (
		id          SERIAL PRIMARY KEY,
		char_val    CHAR(10),
		varchar_val VARCHAR(100),
		text_val    TEXT
	)`)

	mustExec(t, db, `INSERT INTO string_test (char_val, varchar_val, text_val)
		VALUES ('hello', 'world', 'this is a long text')`)
	mustExec(t, db, `INSERT INTO string_test (char_val, varchar_val, text_val)
		VALUES (NULL, NULL, NULL)`)

	type stringRow struct {
		ID      int    `db:"id"`
		Char    string `db:"char_val"`
		Varchar string `db:"varchar_val"`
		Text    string `db:"text_val"`
	}

	var results []stringRow
	err := db.Builder().Table("string_test").
		Select("id", "char_val", "varchar_val", "text_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if strings.TrimSpace(r.Char) != "hello" {
		t.Errorf("Char: expected hello, got %q", r.Char)
	}
	if r.Varchar != "world" {
		t.Errorf("Varchar: expected world, got %s", r.Varchar)
	}
	if r.Text != "this is a long text" {
		t.Errorf("Text: expected 'this is a long text', got %s", r.Text)
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Varchar != "" {
		t.Errorf("NULL Varchar: expected empty, got %s", nullRow.Varchar)
	}
}

// TestPgInteg_NullSafeField_BooleanType 验证 BOOLEAN 类型的 NULL 安全扫描。
func TestPgInteg_NullSafeField_BooleanType(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE bool_test (
		id         SERIAL PRIMARY KEY,
		is_active  BOOLEAN,
		is_deleted BOOLEAN,
		is_valid   BOOLEAN NOT NULL DEFAULT TRUE
	)`)

	mustExec(t, db, `INSERT INTO bool_test (is_active, is_deleted, is_valid) VALUES (TRUE, FALSE, TRUE)`)
	mustExec(t, db, `INSERT INTO bool_test (is_active, is_deleted, is_valid) VALUES (FALSE, TRUE, FALSE)`)
	mustExec(t, db, `INSERT INTO bool_test (is_active, is_deleted, is_valid) VALUES (NULL, NULL, TRUE)`)

	type boolRow struct {
		ID        int  `db:"id"`
		IsActive  bool `db:"is_active"`
		IsDeleted bool `db:"is_deleted"`
		IsValid   bool `db:"is_valid"`
	}

	var results []boolRow
	err := db.Builder().Table("bool_test").
		Select("id", "is_active", "is_deleted", "is_valid").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(results))
	}

	// 验证 TRUE 行
	if !results[0].IsActive {
		t.Errorf("row[0].IsActive: expected true, got false")
	}
	if results[0].IsDeleted {
		t.Errorf("row[0].IsDeleted: expected false, got true")
	}

	// 验证 FALSE 行
	if results[1].IsActive {
		t.Errorf("row[1].IsActive: expected false, got true")
	}
	if !results[1].IsDeleted {
		t.Errorf("row[1].IsDeleted: expected true, got false")
	}

	// 验证 NULL 行
	if results[2].IsActive {
		t.Errorf("row[2].IsActive: expected false (NULL), got true")
	}
}

// TestPgInteg_NullSafeField_DateTimeTypes 验证日期时间类型的 NULL 安全扫描。
// 覆盖：DATE, TIME, TIMESTAMP, TIMESTAMP WITH TIME ZONE
func TestPgInteg_NullSafeField_DateTimeTypes(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE datetime_test (
		id         SERIAL PRIMARY KEY,
		date_val   DATE,
		time_val   TIME,
		ts_val     TIMESTAMP,
		tstz_val   TIMESTAMP WITH TIME ZONE
	)`)

	mustExec(t, db, `INSERT INTO datetime_test (date_val, time_val, ts_val, tstz_val)
		VALUES ('2024-06-15', '14:30:00', '2024-06-15 14:30:00', '2024-06-15 14:30:00+08')`)
	mustExec(t, db, `INSERT INTO datetime_test (date_val, time_val, ts_val, tstz_val)
		VALUES (NULL, NULL, NULL, NULL)`)

	type datetimeRow struct {
		ID   int    `db:"id"`
		Date string `db:"date_val"`
		Time string `db:"time_val"`
		TS   string `db:"ts_val"`
		TSTZ string `db:"tstz_val"`
	}

	var results []datetimeRow
	err := db.Builder().Table("datetime_test").
		Select("id", "date_val", "time_val", "ts_val", "tstz_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.Date == "" {
		t.Errorf("Date: expected non-empty, got empty")
	}
	if r.Time == "" {
		t.Errorf("Time: expected non-empty, got empty")
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Date != "" {
		t.Errorf("NULL Date: expected empty, got %s", nullRow.Date)
	}
}

// TestPgInteg_NullSafeField_BinaryType 验证 BYTEA 类型的 NULL 安全扫描。
func TestPgInteg_NullSafeField_BinaryType(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE binary_test (
		id       SERIAL PRIMARY KEY,
		data_val BYTEA
	)`)

	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	mustExec(t, db, `INSERT INTO binary_test (data_val) VALUES ($1)`, binaryData)
	mustExec(t, db, `INSERT INTO binary_test (data_val) VALUES (NULL)`)

	type binaryRow struct {
		ID   int    `db:"id"`
		Data []byte `db:"data_val"`
	}

	var results []binaryRow
	err := db.Builder().Table("binary_test").
		Select("id", "data_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if len(r.Data) != len(binaryData) {
		t.Errorf("Data: expected length %d, got %d", len(binaryData), len(r.Data))
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Data != nil {
		t.Errorf("NULL Data: expected nil, got %v", nullRow.Data)
	}
}

// TestPgInteg_NullSafeField_JSONTypes 验证 JSON/JSONB 类型的 NULL 安全扫描。
func TestPgInteg_NullSafeField_JSONTypes(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_test (
		id       SERIAL PRIMARY KEY,
		json_val JSON,
		jsonb_val JSONB
	)`)

	mustExec(t, db, `INSERT INTO json_test (json_val, jsonb_val) VALUES ('{"key": "value"}', '{"name": "test"}')`)
	mustExec(t, db, `INSERT INTO json_test (json_val, jsonb_val) VALUES (NULL, NULL)`)

	type jsonRow struct {
		ID    int    `db:"id"`
		JSON  string `db:"json_val"`
		JSONB string `db:"jsonb_val"`
	}

	var results []jsonRow
	err := db.Builder().Table("json_test").
		Select("id", "json_val", "jsonb_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.JSON == "" {
		t.Errorf("JSON: expected non-empty, got empty")
	}
	if r.JSONB == "" {
		t.Errorf("JSONB: expected non-empty, got empty")
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.JSON != "" {
		t.Errorf("NULL JSON: expected empty, got %s", nullRow.JSON)
	}
}

// TestPgInteg_NullSafeField_UUIDType 验证 UUID 类型的 NULL 安全扫描。
func TestPgInteg_NullSafeField_UUIDType(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE uuid_test (
		id      SERIAL PRIMARY KEY,
		uuid_val UUID
	)`)

	mustExec(t, db, `INSERT INTO uuid_test (uuid_val) VALUES ('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11')`)
	mustExec(t, db, `INSERT INTO uuid_test (uuid_val) VALUES (NULL)`)

	type uuidRow struct {
		ID   int    `db:"id"`
		UUID string `db:"uuid_val"`
	}

	var results []uuidRow
	err := db.Builder().Table("uuid_test").
		Select("id", "uuid_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.UUID != "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11" {
		t.Errorf("UUID: expected a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11, got %s", r.UUID)
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.UUID != "" {
		t.Errorf("NULL UUID: expected empty, got %s", nullRow.UUID)
	}
}

// TestPgInteg_NullSafeField_NetworkTypes 验证网络地址类型的 NULL 安全扫描。
// 覆盖：INET, CIDR, MACADDR
func TestPgInteg_NullSafeField_NetworkTypes(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE network_test (
		id        SERIAL PRIMARY KEY,
		inet_val  INET,
		cidr_val  CIDR,
		mac_val   MACADDR
	)`)

	mustExec(t, db, `INSERT INTO network_test (inet_val, cidr_val, mac_val)
		VALUES ('192.168.1.1', '192.168.0.0/24', '08:00:2b:01:02:03')`)
	mustExec(t, db, `INSERT INTO network_test (inet_val, cidr_val, mac_val)
		VALUES (NULL, NULL, NULL)`)

	type networkRow struct {
		ID   int    `db:"id"`
		Inet string `db:"inet_val"`
		CIDR string `db:"cidr_val"`
		MAC  string `db:"mac_val"`
	}

	var results []networkRow
	err := db.Builder().Table("network_test").
		Select("id", "inet_val", "cidr_val", "mac_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.Inet != "192.168.1.1" {
		t.Errorf("Inet: expected 192.168.1.1, got %s", r.Inet)
	}
	if r.CIDR != "192.168.0.0/24" {
		t.Errorf("CIDR: expected 192.168.0.0/24, got %s", r.CIDR)
	}
	if r.MAC != "08:00:2b:01:02:03" {
		t.Errorf("MAC: expected 08:00:2b:01:02:03, got %s", r.MAC)
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Inet != "" {
		t.Errorf("NULL Inet: expected empty, got %s", nullRow.Inet)
	}
}

// TestPgInteg_NullSafeField_ArrayTypes 验证数组类型的 NULL 安全扫描。
// 覆盖：INT[], TEXT[]
func TestPgInteg_NullSafeField_ArrayTypes(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE array_test (
		id       SERIAL PRIMARY KEY,
		int_arr  INT[],
		text_arr TEXT[]
	)`)

	mustExec(t, db, `INSERT INTO array_test (int_arr, text_arr) VALUES ('{1,2,3}', '{"a","b","c"}')`)
	mustExec(t, db, `INSERT INTO array_test (int_arr, text_arr) VALUES (NULL, NULL)`)

	type arrayRow struct {
		ID      int    `db:"id"`
		IntArr  string `db:"int_arr"`
		TextArr string `db:"text_arr"`
	}

	var results []arrayRow
	err := db.Builder().Table("array_test").
		Select("id", "int_arr", "text_arr").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.IntArr == "" {
		t.Errorf("IntArr: expected non-empty, got empty")
	}
	if r.TextArr == "" {
		t.Errorf("TextArr: expected non-empty, got empty")
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.IntArr != "" {
		t.Errorf("NULL IntArr: expected empty, got %s", nullRow.IntArr)
	}
}

// TestPgInteg_NullSafeField_MoneyType 验证 MONEY 类型的 NULL 安全扫描。
func TestPgInteg_NullSafeField_MoneyType(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE money_test (
		id       SERIAL PRIMARY KEY,
		money_val MONEY
	)`)

	mustExec(t, db, `INSERT INTO money_test (money_val) VALUES (123.45)`)
	mustExec(t, db, `INSERT INTO money_test (money_val) VALUES (NULL)`)

	type moneyRow struct {
		ID    int    `db:"id"`
		Money string `db:"money_val"`
	}

	var results []moneyRow
	err := db.Builder().Table("money_test").
		Select("id", "money_val").
		OrderBy("id", "ASC").
		Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 验证有值行
	r := results[0]
	if r.Money == "" {
		t.Errorf("Money: expected non-empty, got empty")
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Money != "" {
		t.Errorf("NULL Money: expected empty, got %s", nullRow.Money)
	}
}

// TestPgInteg_NullSafeField_JSONConversions 验证 JSON/JSONB 类型可以转换到多种 Go 类型。
// 覆盖：[]byte、json.RawMessage、map[string]any、自定义结构体
func TestPgInteg_NullSafeField_JSONConversions(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id        SERIAL PRIMARY KEY,
		json_val  JSON,
		jsonb_val JSONB
	)`)

	mustExec(t, db, `INSERT INTO json_conv_test (json_val, jsonb_val)
		VALUES ('{"name":"alice","age":25}', '{"name":"bob","age":30}')`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val, jsonb_val) VALUES (NULL, NULL)`)

	// 测试 1: JSON → string
	t.Run("JSON_to_string", func(t *testing.T) {
		type row struct {
			ID   int    `db:"id"`
			Data string `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if results[0].Data == "" {
			t.Errorf("expected non-empty string, got empty")
		}
		if results[1].Data != "" {
			t.Errorf("NULL: expected empty, got %s", results[1].Data)
		}
	})

	// 测试 2: JSONB → []byte
	t.Run("JSONB_to_[]byte", func(t *testing.T) {
		type row struct {
			ID   int    `db:"id"`
			Data []byte `db:"jsonb_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "jsonb_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if len(results[0].Data) == 0 {
			t.Errorf("expected non-empty []byte, got empty")
		}
		if results[1].Data != nil {
			t.Errorf("NULL: expected nil, got %v", results[1].Data)
		}
	})

	// 测试 3: JSON → json.RawMessage
	t.Run("JSON_to_RawMessage", func(t *testing.T) {
		type row struct {
			ID   int             `db:"id"`
			Data json.RawMessage `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if len(results[0].Data) == 0 {
			t.Errorf("expected non-empty RawMessage, got empty")
		}
		if results[1].Data != nil {
			t.Errorf("NULL: expected nil, got %v", results[1].Data)
		}
	})

	// 测试 4: JSONB → map[string]any
	t.Run("JSONB_to_map", func(t *testing.T) {
		type row struct {
			ID   int            `db:"id"`
			Data map[string]any `db:"jsonb_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "jsonb_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		// JSONB 存储的 name 是 bob
		if results[0].Data["name"] != "bob" {
			t.Errorf("expected name=bob, got %v", results[0].Data["name"])
		}
		if results[1].Data != nil {
			t.Errorf("NULL: expected nil map, got %v", results[1].Data)
		}
	})

	// 测试 5: JSON → 自定义结构体
	t.Run("JSON_to_struct", func(t *testing.T) {
		type Person struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		type row struct {
			ID   int    `db:"id"`
			Data Person `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if results[0].Data.Name != "alice" {
			t.Errorf("expected name=alice, got %s", results[0].Data.Name)
		}
		if results[0].Data.Age != 25 {
			t.Errorf("expected age=25, got %d", results[0].Data.Age)
		}
		if results[1].Data.Name != "" {
			t.Errorf("NULL: expected zero struct, got %+v", results[1].Data)
		}
	})

	// 测试 6: JSON → *结构体（字段本身是指针）
	t.Run("JSON_to_ptr_struct", func(t *testing.T) {
		type Person struct {
			Name string `json:"name"`
			Age  int    `json:"age"`
		}
		type row struct {
			ID   int     `db:"id"`
			Data *Person `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if results[0].Data == nil {
			t.Fatalf("expected non-nil *Person")
		}
		if results[0].Data.Name != "alice" {
			t.Errorf("expected name=alice, got %s", results[0].Data.Name)
		}
		if results[0].Data.Age != 25 {
			t.Errorf("expected age=25, got %d", results[0].Data.Age)
		}
		// NULL 行：指针应为 nil
		if results[1].Data != nil {
			t.Errorf("NULL: expected nil *Person, got %+v", results[1].Data)
		}
	})

	// 测试 7: JSON → 结构体含嵌套指针结构体字段
	t.Run("JSON_to_nested_ptr_struct", func(t *testing.T) {
		type Address struct {
			City string `json:"city"`
		}
		type Person struct {
			Name    string   `json:"name"`
			Age     int      `json:"age"`
			Address *Address `json:"address"`
		}
		// 插入含嵌套对象的数据
		mustExec(t, db, `INSERT INTO json_conv_test (json_val, jsonb_val)
			VALUES ('{"name":"charlie","age":35,"address":{"city":"Shanghai"}}', '{}')`)
		type row struct {
			ID   int    `db:"id"`
			Data Person `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		// 第三行是嵌套数据
		r := results[2]
		if r.Data.Name != "charlie" {
			t.Errorf("expected name=charlie, got %s", r.Data.Name)
		}
		if r.Data.Address == nil {
			t.Fatalf("expected non-nil Address")
		}
		if r.Data.Address.City != "Shanghai" {
			t.Errorf("expected city=Shanghai, got %s", r.Data.Address.City)
		}
		// 第一行没有 address 字段，嵌套指针应为 nil
		if results[0].Data.Address != nil {
			t.Errorf("expected nil Address for row without address, got %+v", results[0].Data.Address)
		}
	})
}

// TestPgInteg_NullSafeField_ByteToNumeric 验证 []byte → 数值类型的转换。
// PostgreSQL NUMERIC 驱动返回 []byte，需要 ParseInt/ParseUint/ParseFloat 转换。
func TestPgInteg_NullSafeField_ByteToNumeric(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE byte_num_test (
		id        SERIAL PRIMARY KEY,
		num_val   NUMERIC(18,0),
		dec_val   NUMERIC(10,2)
	)`)

	mustExec(t, db, `INSERT INTO byte_num_test (num_val, dec_val) VALUES (12345, 99.99)`)
	mustExec(t, db, `INSERT INTO byte_num_test (num_val, dec_val) VALUES (-888, 0.01)`)
	mustExec(t, db, `INSERT INTO byte_num_test (num_val, dec_val) VALUES (NULL, NULL)`)

	// []byte → int / int64
	t.Run("byte_to_int", func(t *testing.T) {
		type row struct {
			ID  int `db:"id"`
			Num int `db:"num_val"`
		}
		var results []row
		err := db.Builder().Table("byte_num_test").Select("id", "num_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if results[0].Num != 12345 {
			t.Errorf("row[0]: expected 12345, got %d", results[0].Num)
		}
		if results[1].Num != -888 {
			t.Errorf("row[1]: expected -888, got %d", results[1].Num)
		}
		// NULL → 零值
		if results[2].Num != 0 {
			t.Errorf("NULL: expected 0, got %d", results[2].Num)
		}
	})

	// []byte → int8 / int16 / int32
	t.Run("byte_to_small_int", func(t *testing.T) {
		type row struct {
			ID  int  `db:"id"`
			Num int8 `db:"num_val"`
		}
		// 插入小值
		mustExec(t, db, `INSERT INTO byte_num_test (num_val, dec_val) VALUES (100, 0)`)
		var results []row
		err := db.Builder().Table("byte_num_test").Select("id", "num_val").
			Where("num_val", "=", 100).Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if len(results) != 1 || results[0].Num != 100 {
			t.Errorf("expected 100, got %v", results)
		}
	})

	// []byte → uint / uint64
	t.Run("byte_to_uint", func(t *testing.T) {
		type row struct {
			ID  int    `db:"id"`
			Num uint64 `db:"num_val"`
		}
		var results []row
		err := db.Builder().Table("byte_num_test").Select("id", "num_val").
			Where("num_val", "=", 12345).Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if len(results) != 1 || results[0].Num != 12345 {
			t.Errorf("expected 12345, got %v", results)
		}
	})

	// []byte → float64
	t.Run("byte_to_float", func(t *testing.T) {
		type row struct {
			ID  int     `db:"id"`
			Dec float64 `db:"dec_val"`
		}
		var results []row
		err := db.Builder().Table("byte_num_test").Select("id", "dec_val").
			Where("dec_val", "=", 99.99).Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if len(results) != 1 {
			t.Fatalf("expected 1 row, got %d", len(results))
		}
		if results[0].Dec != 99.99 {
			t.Errorf("expected 99.99, got %f", results[0].Dec)
		}
	})
}

// TestPgInteg_NullSafeField_ByteToBool 验证 []byte → bool 的转换。
func TestPgInteg_NullSafeField_ByteToBool(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE byte_bool_test (
		id       SERIAL PRIMARY KEY,
		text_val TEXT
	)`)

	mustExec(t, db, `INSERT INTO byte_bool_test (text_val) VALUES ('true')`)
	mustExec(t, db, `INSERT INTO byte_bool_test (text_val) VALUES ('false')`)
	mustExec(t, db, `INSERT INTO byte_bool_test (text_val) VALUES ('1')`)
	mustExec(t, db, `INSERT INTO byte_bool_test (text_val) VALUES (NULL)`)

	type row struct {
		ID   int  `db:"id"`
		Flag bool `db:"text_val"`
	}
	var results []row
	err := db.Builder().Table("byte_bool_test").Select("id", "text_val").
		OrderBy("id", "ASC").Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if !results[0].Flag {
		t.Errorf("row[0] 'true': expected true, got false")
	}
	if results[1].Flag {
		t.Errorf("row[1] 'false': expected false, got true")
	}
	if !results[2].Flag {
		t.Errorf("row[2] '1': expected true, got false")
	}
	// NULL → 零值 false
	if results[3].Flag {
		t.Errorf("NULL: expected false, got true")
	}
}

// TestPgInteg_NullSafeField_PointerFields 验证各种指针类型字段的 NULL 处理。
func TestPgInteg_NullSafeField_PointerFields(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE ptr_test (
		id        SERIAL PRIMARY KEY,
		int_val   INTEGER,
		str_val   TEXT,
		bool_val  BOOLEAN,
		float_val DOUBLE PRECISION,
		time_val  DATE
	)`)

	// 有值行
	mustExec(t, db, `INSERT INTO ptr_test (int_val, str_val, bool_val, float_val, time_val)
		VALUES (42, 'hello', true, 3.14, '2024-01-15')`)
	// NULL 行
	mustExec(t, db, `INSERT INTO ptr_test (int_val, str_val, bool_val, float_val, time_val)
		VALUES (NULL, NULL, NULL, NULL, NULL)`)

	type row struct {
		ID     int      `db:"id"`
		PInt   *int     `db:"int_val"`
		PStr   *string  `db:"str_val"`
		PBool  *bool    `db:"bool_val"`
		PFloat *float64 `db:"float_val"`
		PTime  *string  `db:"time_val"`
	}
	var results []row
	err := db.Builder().Table("ptr_test").Select("id", "int_val", "str_val", "bool_val", "float_val", "time_val").
		OrderBy("id", "ASC").Find(context.Background(), &results)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	// 验证有值行
	r := results[0]
	if r.PInt == nil || *r.PInt != 42 {
		t.Errorf("PInt: expected *42, got %v", r.PInt)
	}
	if r.PStr == nil || *r.PStr != "hello" {
		t.Errorf("PStr: expected *'hello', got %v", r.PStr)
	}
	if r.PBool == nil || !*r.PBool {
		t.Errorf("PBool: expected *true, got %v", r.PBool)
	}
	if r.PFloat == nil || *r.PFloat != 3.14 {
		t.Errorf("PFloat: expected *3.14, got %v", r.PFloat)
	}
	if r.PTime == nil || *r.PTime == "" {
		t.Errorf("PTime: expected non-empty, got %v", r.PTime)
	}

	// 验证 NULL 行（所有指针应为 nil）
	nullRow := results[1]
	if nullRow.PInt != nil {
		t.Errorf("NULL PInt: expected nil, got %v", nullRow.PInt)
	}
	if nullRow.PStr != nil {
		t.Errorf("NULL PStr: expected nil, got %v", nullRow.PStr)
	}
	if nullRow.PBool != nil {
		t.Errorf("NULL PBool: expected nil, got %v", nullRow.PBool)
	}
	if nullRow.PFloat != nil {
		t.Errorf("NULL PFloat: expected nil, got %v", nullRow.PFloat)
	}
	if nullRow.PTime != nil {
		t.Errorf("NULL PTime: expected nil, got %v", nullRow.PTime)
	}
}

// TestPgInteg_NullSafeField_JSONError 验证无效 JSON 转换时返回错误。
func TestPgInteg_NullSafeField_JSONError(t *testing.T) {
	db := openPgTestDB(t)

	mustExec(t, db, `CREATE TABLE json_err_test (
		id       SERIAL PRIMARY KEY,
		json_val JSON
	)`)

	// 插入无效 JSON（PostgreSQL JSON 类型会拒绝无效 JSON，所以用 TEXT 存储）
	mustExec(t, db, `DROP TABLE IF EXISTS json_err_test`)
	mustExec(t, db, `CREATE TABLE json_err_test (
		id       SERIAL PRIMARY KEY,
		json_val TEXT
	)`)
	mustExec(t, db, `INSERT INTO json_err_test (json_val) VALUES ('not valid json')`)

	type Person struct {
		Name string `json:"name"`
	}
	type row struct {
		ID   int    `db:"id"`
		Data Person `db:"json_val"`
	}
	var results []row
	err := db.Builder().Table("json_err_test").Select("id", "json_val").
		Find(context.Background(), &results)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
	// 验证错误消息包含有用信息
	if !strings.Contains(err.Error(), "zcdb") {
		t.Errorf("error should contain 'zcdb': %v", err)
	}
}

// ==================== Cursor 集成测试 ====================

// TestPgInteg_Cursor_Stream 验证 Cursor 流式迭代：逐行读取所有数据。
func TestPgInteg_Cursor_Stream(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"` // 非指针类型，NULL 时保留零值
	}
	var user row
	var names []string
	var ages []int
	for err := range db.Builder().Table("users").Select("name", "age").OrderBy("id", "ASC").Cursor(context.Background(), &user) {
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		names = append(names, user.Name)
		ages = append(ages, user.Age)
	}
	if len(names) != 5 {
		t.Errorf("expected 5 names, got %d: %v", len(names), names)
	}
	if names[0] != "alice" {
		t.Errorf("expected first name alice, got %s", names[0])
	}
	// eve 的 age 为 NULL，应该是零值 0
	if ages[4] != 0 {
		t.Errorf("expected eve's age=0 (NULL), got %d", ages[4])
	}
}

// TestPgInteg_Cursor_Break 验证 Cursor 迭代中 break 能正常释放资源。
func TestPgInteg_Cursor_Break(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var user row
	count := 0
	for err := range db.Builder().Table("users").Select("name").OrderBy("id", "ASC").Cursor(context.Background(), &user) {
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		count++
		if count == 2 {
			break // 只取前 2 条
		}
	}
	if count != 2 {
		t.Errorf("expected 2 iterations, got %d", count)
	}
}

// TestPgInteg_CursorBy_Keyset 验证 CursorBy 游标分页迭代：分批获取全部数据。
func TestPgInteg_CursorBy_Keyset(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	var lastID int
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, user.Name)
		lastID = user.ID
	}
	if len(names) != 5 {
		t.Errorf("expected 5 names, got %d: %v", len(names), names)
	}
	if names[0] != "alice" || names[4] != "eve" {
		t.Errorf("expected alice...eve, got %v", names)
	}
	if lastID != 5 {
		t.Errorf("expected last id=5, got %d", lastID)
	}
}

// TestPgInteg_CursorBy_Break 验证 CursorBy 迭代中 break 能正常停止。
func TestPgInteg_CursorBy_Break(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		count++
		if count == 3 {
			break
		}
	}
	if count != 3 {
		t.Errorf("expected 3 iterations, got %d", count)
	}
}

// TestPgInteg_CursorBy_IgnoresOrderBy 验证 CursorBy 会忽略已设置的 ORDER BY。
func TestPgInteg_CursorBy_IgnoresOrderBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var ids []int
	// 用户先设置了 ORDER BY name DESC，但 CursorBy 应该忽略它，强制按 id ASC
	for err := range db.Builder().Table("users").Select("id", "name").OrderBy("name", "DESC").CursorBy(context.Background(), &user, 10, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		ids = append(ids, user.ID)
	}
	// 验证结果是按 id 升序，而非 name 降序
	expected := []int{1, 2, 3, 4, 5}
	if len(ids) != len(expected) {
		t.Errorf("expected %d ids, got %d: %v", len(expected), len(ids), ids)
	}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("ids[%d]: expected %d, got %d", i, expected[i], id)
		}
	}
}

// ==================== 复杂查询补充 ====================

// TestPgInteg_Complex_MultiSubqueryCombination 验证多种子查询类型组合：
// WHERE NOT IN子查询 + WHERE EXISTS + JOIN + ORDER BY。
// 找出「没有个人档案但有订单」的用户，且至少有一笔订单金额 > 100。
// 预期：diana(有 Camera 150，无 profile)
func TestPgInteg_Complex_MultiSubqueryCombination(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)
	setupPgProfilesTable(t, db)

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

// ==================== 分页补充 ====================

// TestPgInteg_ForPageFirst 验证第一页分页：第 1 页不生成 OFFSET。
func TestPgInteg_ForPageFirst(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(1, 3).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Name != "alice" {
		t.Errorf("expected first row alice, got %s", rows[0].Name)
	}
}

// ==================== JOIN 补充 ====================

// TestPgInteg_LeftJoinOnOrOn 验证 LeftJoinOn + OrOn：LEFT JOIN 带 OR 条件的 ON 子句。
func TestPgInteg_LeftJoinOnOrOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)

	type row struct {
		Name string  `db:"name"`
		Bio  *string `db:"bio"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "profiles.bio").
		LeftJoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				OrOn("profiles.active", "=", "users.id")
		}).
		OrderBy("users.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// 所有 5 个用户都应保留（LEFT JOIN）
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d: %v", len(rows), rows)
	}
}

// ==================== UPDATE 补充 ====================

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

// ==================== WHERE 补充 ====================

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

// ==================== INSERT 补充 ====================

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

// ==================== BUG 修复验证（真实数据库执行） ====================

// TestPgInteg_WhereLikeExpression 验证 PostgreSQL 上 WhereLike 传入 Expression 的真实执行：
// Expression 直接内嵌为原始 SQL（无占位符、无绑定参数），SQL 语法正确且结果正确。
// 注意：PG 的 LIKE 大小写敏感，测试数据全小写，'%a%' 命中 alice/charlie/diana。
func TestPgInteg_WhereLikeExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CountWithGroupBy 验证 PostgreSQL 上 GROUP BY 的 Count 真实执行：
// 返回分组数量（非第一组行数）。
func TestPgInteg_CountWithGroupBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	// orders 共 6 条：user_id 1×2、2×2、3×1、4×1 → 4 个分组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Fatalf("Count with GROUP BY: expected 4 (number of groups), got %d", count)
	}
}

// TestPgInteg_CountWithGroupByHaving 验证 PostgreSQL 上 GROUP BY + HAVING 的 Count 真实执行。
func TestPgInteg_CountWithGroupByHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	// 各组 SUM(amount)：user1=170、user2=280、user3=30、user4=150 → >100 的有 3 组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("Count with GROUP BY + HAVING: expected 3, got %d", count)
	}
}

// TestPgInteg_HavingExpression 验证 HAVING 值传 Expression 时真实执行（直接内嵌 SQL）。
func TestPgInteg_HavingExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	// 各组 SUM(amount)：170/280/30/150 → >100 的有 3 组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", NewExpression("100")).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("Having with Expression: expected 3 groups, got %d", count)
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

// TestPgInteg_OffsetWithoutLimit 验证仅 Offset 无 Limit 时真实执行（PG 原生支持）。
func TestPgInteg_OffsetWithoutLimit(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var users []struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	err := db.Builder().
		Table("users").
		OrderBy("id", "ASC").
		Offset(2).
		Find(context.Background(), &users)
	assertNoError(t, err)
	if len(users) != 3 {
		t.Fatalf("Offset(2) without Limit: expected 3 rows, got %d", len(users))
	}
	if users[0].Name != "charlie" {
		t.Errorf("Offset(2) without Limit: expected first row charlie, got %s", users[0].Name)
	}
}

// TestPgInteg_CountWithDistinct 验证 Distinct + Count 去重计数真实执行。
func TestPgInteg_CountWithDistinct(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS tags")
	mustExec(t, db, `CREATE TABLE tags (name VARCHAR(16) NOT NULL)`)
	mustExec(t, db, `INSERT INTO tags (name) VALUES ('a'), ('a'), ('b'), ('c')`)

	// 4 行 3 个去重值；修复前生成 SELECT DISTINCT COUNT(*) 会错误返回 4
	count, err := db.Builder().
		Table("tags").
		Select("name").
		Distinct().
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("Distinct Count: expected 3 distinct values, got %d", count)
	}
}
