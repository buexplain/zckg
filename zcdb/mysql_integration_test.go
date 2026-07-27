package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// ==================== MySQL 基础设施 ====================

// openMySQLTestDB 打开 MySQL 连接，自动创建测试数据库（若不存在），然后清理并重建 users/orders 相关表，保证测试隔离。
// docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
func openMySQLTestDB(t *testing.T) *DBDao {
	t.Helper()
	pool, err := NewPool(PoolConfig{
		DriverName: "mysql",
		DSN:        "root:root@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=Local",
	})
	if err != nil {
		t.Fatalf("failed to open mysql: %v", err)
	}
	dao, err := NewDBDao(pool, "mysql", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	})
	if err != nil {
		t.Fatalf("failed to open mysql: %v", err)
	}
	if err := dao.Pool().Ping(context.Background()); err != nil {
		t.Fatalf("failed to ping mysql: %v", err)
	}

	// 创建测试数据库（若不存在）并切换
	_, err = dao.Exec(context.Background(), "CREATE DATABASE IF NOT EXISTS `zckg_test_integ` DEFAULT CHARACTER SET utf8mb4")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	_, err = dao.Exec(context.Background(), "USE `zckg_test_integ`")
	if err != nil {
		t.Fatalf("failed to use database: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	// 清理旧表
	dropMySQLTables(t, dao)
	return dao
}

// dropMySQLTables 清除所有测试用表
func dropMySQLTables(t *testing.T, db *DBDao) {
	t.Helper()
	tables := []string{"users_archive", "profiles", "orders", "users"}
	for _, table := range tables {
		_, _ = db.Exec(context.Background(), "DROP TABLE IF EXISTS `"+table+"`")
	}
}

// setupMySQLUsersTable 创建 MySQL 版 users 表并预填 5 条数据。
func setupMySQLUsersTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE users (
		id     BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name   VARCHAR(64) NOT NULL,
		age    INT NULL,
		email  VARCHAR(128) NULL,
		status VARCHAR(16) NOT NULL DEFAULT 'active',
		UNIQUE KEY uk_email (email)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES
		('alice', 25, 'alice@test.com', 'active'),
		('bob', 30, 'bob@test.com', 'active'),
		('charlie', 35, 'charlie@test.com', 'inactive'),
		('diana', 28, 'diana@test.com', 'active'),
		('eve', NULL, 'eve@test.com', 'inactive')`)
}

// setupMySQLOrdersTable 创建 MySQL 版 orders 表并预填数据。
func setupMySQLOrdersTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE orders (
		id      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		user_id BIGINT UNSIGNED NOT NULL,
		amount  DECIMAL(10,2) NOT NULL,
		product VARCHAR(64) NULL,
		KEY idx_user (user_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO orders (user_id, amount, product) VALUES
		(1, 50.00, 'Book'),
		(1, 120.00, 'Laptop'),
		(2, 80.00, 'Phone'),
		(2, 200.00, 'TV'),
		(3, 30.00, 'Pen'),
		(4, 150.00, 'Camera')`)
}

// setupMySQLProfilesTable 创建 MySQL 版 profiles 表并预填数据。
func setupMySQLProfilesTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE profiles (
		id      BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		user_id BIGINT UNSIGNED NOT NULL,
		bio     TEXT,
		active  BIGINT UNSIGNED DEFAULT 1,
		KEY idx_user (user_id)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO profiles (user_id, bio, active) VALUES
		(1, 'Alice bio', 99),
		(2, 'Bob bio', 99),
		(3, 'Charlie bio', 99)`)
}

// ==================== Group 1: INSERT ====================

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

// TestMySQLInteg_InsertPtrPartial 验证指针字段 nil 跳过：nil 指针字段不参与 INSERT，对应列应为数据库默认值（NULL）。
func TestMySQLInteg_InsertPtrPartial(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// ==================== Group 2: SELECT 基础查询 ====================

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

// ==================== Group 3: SELECT 高级 WHERE ====================

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

// ==================== Group 4: JOIN ====================

// TestMySQLInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行。
func TestMySQLInteg_InnerJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留。
func TestMySQLInteg_LeftJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积。
func TestMySQLInteg_CrossJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").CrossJoin("orders").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestMySQLInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：ON 子句附加额外过滤。
func TestMySQLInteg_JoinOn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_JoinOnMultiple 验证 JoinOn 多 ON 条件（AND 连接）。
func TestMySQLInteg_JoinOnMultiple(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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
	// alice(Laptop=120), bob(TV=200), diana(Camera=150) → 3 rows
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_JoinOnOrCondition 验证 JoinOn OR 条件：OrOn 生成 OR 连接的 ON 条件。
func TestMySQLInteg_JoinOnOrCondition(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLProfilesTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				OrOn("users.id", "=", "profiles.user_id")
		}).
		Distinct().
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// alice, bob, charlie have profiles → 3 distinct users
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_LeftJoinOn 验证 LeftJoinOn 多 ON 条件：LEFT JOIN + 多 ON 条件（AND 连接）。
func TestMySQLInteg_LeftJoinOn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLProfilesTable(t, db)

	type row struct {
		Name string  `db:"name"`
		Bio  *string `db:"bio"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "profiles.bio").
		LeftJoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				On("profiles.active", "=", "users.id")
		}).
		OrderBy("users.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// profiles.active=99 不匹配任何 users.id，所以 LEFT JOIN 全部为 NULL
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
	for _, r := range rows {
		if r.Bio != nil {
			t.Errorf("expected NULL bio for %s, got %s", r.Name, *r.Bio)
		}
	}
}

// ==================== Group 5: 聚合/分组/排序 ====================

// TestMySQLInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤。
func TestMySQLInteg_GroupByHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 100).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 groups with total > 100, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_HavingBetween 验证 HAVING BETWEEN 聚合过滤。
func TestMySQLInteg_HavingBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingBetween("SUM(amount)", 100, 200).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170, user2=280, user3=30, user4=150 → user1(170) and user4(150) in [100,200]
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_OrderByLimitOffset 验证排序+分页：ORDER BY + LIMIT + OFFSET。
func TestMySQLInteg_OrderByLimitOffset(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_ForPage 验证 ForPage 便捷分页。
func TestMySQLInteg_ForPage(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_ForPageFirst 验证第一页分页：第 1 页不生成 OFFSET。
func TestMySQLInteg_ForPageFirst(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_InRandomOrder 验证随机排序：ORDER BY RAND()。
func TestMySQLInteg_InRandomOrder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// ==================== Group 6: 子查询 ====================

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

// TestMySQLInteg_FromSub 验证 FROM 子查询（派生表）。
func TestMySQLInteg_FromSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_SelectSubquery 验证 SELECT 子句中的子查询。
func TestMySQLInteg_SelectSubquery(t *testing.T) {
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
		SelectSubquery(sub, "orders_count").
		Where("id", "=", 1).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "alice" || rows[0].OrdersCount != 2 {
		t.Errorf("expected alice with 2 orders, got %v", rows)
	}
}

// ==================== Group 7: UPDATE ====================

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

// TestMySQLInteg_UpdatePtrPartial 验证指针字段部分更新：nil 指针字段不参与 SET。
func TestMySQLInteg_UpdatePtrPartial(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// ==================== Group 8: DELETE ====================

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

// TestMySQLInteg_DeleteAll 验证无条件全表删除。
func TestMySQLInteg_DeleteAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	_, err := db.Builder().Table("users").Delete(context.Background())
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}

	count, _ := db.Builder().Table("users").Count(context.Background())
	if count != 0 {
		t.Errorf("expected 0 users after delete all, got %d", count)
	}
}

// ==================== Group 9: UPSERT / INSERT USING / UNION / TRUNCATE ====================

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

// TestMySQLInteg_Union 验证 UNION 去重合并。
func TestMySQLInteg_Union(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
		t.Errorf("expected 4 union result, got %d: %v", len(rows), rows)
	}
}

// TestMySQLInteg_UnionAll 验证 UNION ALL 不去重合并。
func TestMySQLInteg_UnionAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
		t.Errorf("expected 6 union all result, got %d: %v", len(rows), rows)
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

// TestMySQLInteg_Clone 验证 Builder 克隆后独立查询。
func TestMySQLInteg_Clone(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
	// clone2: active and age<28 → alice(25), diana(28... no, 28 is not < 28) → alice(25) = 1
	if len(rows2) != 1 {
		t.Errorf("expected 1 row for clone2, got %d: %v", len(rows2), rows2)
	}
}

// ==================== Group 10: builder_exec 终端方法 ====================

// TestMySQLInteg_First 验证 First 查询第一条记录：有数据时填充结构体并返回 nil。
func TestMySQLInteg_First(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_FirstNotFound 验证 First 无数据时返回 sql.ErrNoRows。
func TestMySQLInteg_FirstNotFound(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).First(context.Background(), &r)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestMySQLInteg_FirstLimit 验证 First 自动限制为 1 条：即使有多行匹配也只返回第一条。
func TestMySQLInteg_FirstLimit(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_Exists 验证 Exists 有数据时返回 true。
func TestMySQLInteg_Exists(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}
}

// TestMySQLInteg_ExistsFalse 验证 Exists 无匹配数据时返回 false。
func TestMySQLInteg_ExistsFalse(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("id", "=", 999).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false, got true")
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

// TestMySQLInteg_Paginate 验证 Paginate 分页查询：第二页返回正确数据。
func TestMySQLInteg_Paginate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	b := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(2, 2)
	_, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if len(rows) >= 2 && (rows[0].Name != "charlie" || rows[1].Name != "diana") {
		t.Errorf("expected [charlie, diana], got %v", rows)
	}
}

// TestMySQLInteg_PaginateDefault 验证 Paginate 未设置分页参数时使用默认值（第 1 页，每页 20 条）。
func TestMySQLInteg_PaginateDefault(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	b := db.Builder().Table("users").Select("name").OrderBy("id", "ASC")
	_, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	// 默认 ForPage(1, 20)，5 条数据全部返回
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

// ==================== Group 11: MySQL 专属能力 ====================

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

// TestMySQLInteg_LockForUpdate 验证 SELECT ... FOR UPDATE 语法可执行（事务内）。
func TestMySQLInteg_LockForUpdate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
		t.Fatalf("TestMySQLInteg_LockForUpdate error: %v", err)
	}
}

// TestMySQLInteg_SharedLock 验证 SELECT ... LOCK IN SHARE MODE 语法可执行（事务内）。
func TestMySQLInteg_SharedLock(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
		t.Fatalf("TestMySQLInteg_SharedLock error: %v", err)
	}
}

// ==================== Group 12: Transaction ====================

// TestMySQLInteg_TransactionCommit 验证事务提交：回调返回 nil 时，修改持久化。
func TestMySQLInteg_TransactionCommit(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_TransactionRollback 验证事务回滚：回调返回 error 时，修改被撤销。
func TestMySQLInteg_TransactionRollback(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_TransactionNested 验证嵌套事务传播：内层事务复用外层事务，提交后整体生效。
func TestMySQLInteg_TransactionNested(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// ==================== Group 13: any 入参约束验证 ====================

// TestMySQLInteg_FirstInvalidDest 验证 First 传入非指针类型时返回错误。
func TestMySQLInteg_FirstInvalidDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	// 传入结构体值（非指针），应返回错误
	err := db.Builder().Table("users").Select("name").First(context.Background(), r)
	if err == nil {
		t.Fatalf("expected error for non-pointer dest, got nil")
	}
}

// TestMySQLInteg_FirstNilDest 验证 First 传入 nil 时返回错误。
func TestMySQLInteg_FirstNilDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	err := db.Builder().Table("users").Select("name").First(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil dest, got nil")
	}
}

// TestMySQLInteg_FirstIntPtrDest 验证 First 传入非结构体指针（*int）时返回错误。
func TestMySQLInteg_FirstIntPtrDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").First(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest, got nil")
	}
}

// TestMySQLInteg_FindInvalidDest 验证 Find 传入 *int（非结构体切片指针）时返回错误。
func TestMySQLInteg_FindInvalidDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var n int
	// Find 要求 *[]struct，传入 *int 应返回错误
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Find, got nil")
	}
}

// TestMySQLInteg_FindNonPointerDest 验证 Find 传入非指针（[]struct）时返回错误。
func TestMySQLInteg_FindNonPointerDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	// 传入切片值（非指针），应返回错误
	err := db.Builder().Table("users").Select("name").Find(context.Background(), rows)
	if err == nil {
		t.Fatalf("expected error for non-pointer slice dest, got nil")
	}
}

// TestMySQLInteg_FindIntPtrDest 验证 Find 传入 *[]int（非结构体切片指针）时返回错误。
func TestMySQLInteg_FindIntPtrDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Fatalf("expected error for *[]int dest in Find, got nil")
	}
}

// TestMySQLInteg_PaginateInvalidDest 验证 Paginate 传入 *int（非结构体切片指针）时返回错误。
func TestMySQLInteg_PaginateInvalidDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var n int
	_, err := db.Builder().Table("users").Select("name").Paginate(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Paginate, got nil")
	}
}

// TestMySQLInteg_ValueNoRows 验证 Value 无匹配数据时返回 sql.ErrNoRows。
func TestMySQLInteg_ValueNoRows(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).Value(context.Background(), &name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
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
