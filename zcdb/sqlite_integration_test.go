package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// ==================== 基础设施 ====================

// openSQLiteTestDB 打开一个内存 SQLite 数据库，测试结束后自动关闭。
func openSQLiteTestDB(t *testing.T) *DBDao {
	t.Helper()
	pool, err := NewPool(PoolConfig{
		DriverName: "sqlite",
		DSN:        ":memory:",
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	dao, err := NewDBDao(pool, "sqlite", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	return dao
}

// mustExec 执行 SQL，失败则 Fatal。
func mustExec(t *testing.T, db *DBDao, query string, args ...any) {
	t.Helper()
	_, err := db.Exec(context.Background(), query, args...)
	if err != nil {
		t.Fatalf("exec failed: %s\nerror: %v", query, err)
	}
}

// setupSQLiteUsersTable 创建 users 表并预填 5 条数据。
func setupSQLiteUsersTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE users (
		id     INTEGER PRIMARY KEY AUTOINCREMENT,
		name   TEXT NOT NULL,
		age    INTEGER,
		email  TEXT UNIQUE,
		status TEXT DEFAULT 'active'
	)`)
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES
		('alice', 25, 'alice@test.com', 'active'),
		('bob', 30, 'bob@test.com', 'active'),
		('charlie', 35, 'charlie@test.com', 'inactive'),
		('diana', 28, 'diana@test.com', 'active'),
		('eve', NULL, 'eve@test.com', 'inactive')`)
}

// setupSQLiteProfilesTable 创建 profiles 表并预填数据。
func setupSQLiteProfilesTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE profiles (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		bio     TEXT,
		active  INTEGER DEFAULT 1
	)`)
	mustExec(t, db, `INSERT INTO profiles (user_id, bio, active) VALUES
		(1, 'Alice bio', 99),
		(2, 'Bob bio', 99),
		(3, 'Charlie bio', 99)`)
}

// setupSQLiteOrdersTable 创建 orders 表并预填数据。
func setupSQLiteOrdersTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE orders (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		user_id INTEGER NOT NULL,
		amount  REAL NOT NULL,
		product TEXT
	)`)
	mustExec(t, db, `INSERT INTO orders (user_id, amount, product) VALUES
		(1, 50.0, 'Book'),
		(1, 120.0, 'Laptop'),
		(2, 80.0, 'Phone'),
		(2, 200.0, 'TV'),
		(3, 30.0, 'Pen'),
		(4, 150.0, 'Camera')`)
}

// ==================== Group 1: INSERT ====================

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

// ==================== Group 2: SELECT 基础查询 ====================

// TestSQLiteInteg_SelectAll 验证无条件全表查询：不设任何 WHERE，SELECT * 应返回所有行。
func TestSQLiteInteg_SelectAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_SelectColumns 验证指定列查询：仅选择 name 和 age 列，并通过 WHERE 定位单行，确认返回值正确。
func TestSQLiteInteg_SelectColumns(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_SelectDistinct 验证 DISTINCT 去重：对有重复值的列使用 Distinct()，确认结果已去重。
func TestSQLiteInteg_SelectDistinct(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// ==================== Group 3: SELECT 高级 WHERE ====================

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

// ==================== Group 4: JOIN ====================

// TestSQLiteInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行，无订单的用户不出现。
func TestSQLiteInteg_InnerJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留，无匹配的右表列为 NULL。
func TestSQLiteInteg_LeftJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积：结果行数 = 左表行数 × 右表行数。
func TestSQLiteInteg_CrossJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").CrossJoin("orders").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestSQLiteInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：在 ON 子句中附加额外过滤条件（amount > 100）。
func TestSQLiteInteg_JoinOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_LeftJoinOnOrOn 验证 LEFT JOIN ON + OR ON 条件。
func TestSQLiteInteg_LeftJoinOnOrOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)

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

// ==================== Group 5: 聚合/分组/排序 ====================

// TestSQLiteInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤：按 user_id 分组后筛选 SUM(amount) > 100 的组。
func TestSQLiteInteg_GroupByHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_HavingBetween 验证 HAVING BETWEEN 聚合过滤。
func TestSQLiteInteg_HavingBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_OrderByLimitOffset 验证排序+分页：ORDER BY age DESC 后跳过 1 行取 2 行，确认结果正确。
func TestSQLiteInteg_OrderByLimitOffset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_ForPage 验证 ForPage 便捷分页：page=2, perPage=2 应返回第 3~4 行。
func TestSQLiteInteg_ForPage(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_ForPageFirst 验证第一页分页：第 1 页不生成 OFFSET。
func TestSQLiteInteg_ForPageFirst(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(1, 3).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "alice" || rows[2].Name != "charlie" {
		t.Errorf("expected [alice, bob, charlie], got %v", rows)
	}
}

// TestSQLiteInteg_InRandomOrder 验证随机排序：InRandomOrder 生成 ORDER BY RANDOM()，返回行数不变。
func TestSQLiteInteg_InRandomOrder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_SelectRaw 验证 SelectRaw 原始表达式：使用 COUNT(*) 聚合函数，确认返回正确计数。
func TestSQLiteInteg_SelectRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

// ==================== Group 6: 子查询 ====================

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

// TestSQLiteInteg_TableSub 验证 FROM 子查询（派生表）：先通过子查询过滤，再在外层查询中继续筛选。
func TestSQLiteInteg_TableSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_SelectSubquery 验证 SELECT 子句中的子查询。
func TestSQLiteInteg_SelectSubquery(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").WhereRaw(`"orders"."user_id" = "users"."id"`)
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

// ==================== Group 8: DELETE ====================

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

// ==================== Group 9: UPSERT / INSERT USING / UNION / TRUNCATE ====================

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

// TestSQLiteInteg_Union 验证 UNION 去重合并：两个查询的结果合并后自动去除重复行。
func TestSQLiteInteg_Union(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_UnionAll 验证 UNION ALL 不去重合并：两个查询的结果直接拼接，保留重复行。
func TestSQLiteInteg_UnionAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_UnionLockForUpdate 验证 UNION 查询 + LockForUpdate 返回错误（SQLite 不支持锁子句）。
func TestSQLiteInteg_UnionLockForUpdate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.Union(q2).LockForUpdate().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_UnionAllSharedLock 验证 UNION ALL 查询 + SharedLock 返回错误（SQLite 不支持锁子句）。
func TestSQLiteInteg_UnionAllSharedLock(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.UnionAll(q2).SharedLock().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_Clone 验证 Builder 克隆后独立查询。
func TestSQLiteInteg_Clone(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// ==================== Group 10: builder_exec 终端方法 ====================

// TestSQLiteInteg_First 验证 First 查询第一条记录：有数据时填充结构体并返回 nil。
func TestSQLiteInteg_First(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_FirstNotFound 验证 First 无数据时返回 sql.ErrNoRows。
func TestSQLiteInteg_FirstNotFound(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).First(context.Background(), &r)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestSQLiteInteg_FirstLimit 验证 First 自动限制为 1 条：即使有多行匹配也只返回第一条。
func TestSQLiteInteg_FirstLimit(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Exists 验证 Exists 有数据时返回 true。
func TestSQLiteInteg_Exists(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}
}

// TestSQLiteInteg_ExistsFalse 验证 Exists 无匹配数据时返回 false。
func TestSQLiteInteg_ExistsFalse(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("id", "=", 999).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false, got true")
	}
}

// TestSQLiteInteg_ExistsUsesLimit1 验证 Exists 走 SELECT 1 ... LIMIT 1 而非 COUNT(*)：
// 通过 onSQL 回调捕获实际执行 SQL 断言。
func TestSQLiteInteg_ExistsUsesLimit1(t *testing.T) {
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	var captured []string
	dao, err := NewDBDao(pool, "sqlite", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		captured = append(captured, sqlStr)
	})
	if err != nil {
		t.Fatalf("failed to create dao: %v", err)
	}
	defer dao.Close()

	setupSQLiteUsersTable(t, dao)

	exists, err := dao.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}

	// 捕获的 SQL 必须是 LIMIT 1 形式，不能是 COUNT(*)
	var sqlStr string
	for _, s := range captured {
		if strings.Contains(s, "SELECT 1") {
			sqlStr = s
			break
		}
	}
	if sqlStr == "" {
		t.Fatalf("no EXISTS SQL captured, got: %v", captured)
	}
	if strings.Contains(sqlStr, "COUNT(*)") {
		t.Errorf("Exists should not use COUNT(*), got: %s", sqlStr)
	}
	if !strings.Contains(sqlStr, "LIMIT 1") {
		t.Errorf("Exists should use SELECT 1 ... LIMIT 1, got: %s", sqlStr)
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

// TestSQLiteInteg_Paginate 验证 Paginate 分页查询：第二页返回正确数据。
func TestSQLiteInteg_Paginate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_PaginateDefault 验证 Paginate 未设置分页参数时使用默认值（第 1 页，每页 20 条）。
func TestSQLiteInteg_PaginateDefault(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// ==================== Group 11: Lock ====================

// TestSQLiteInteg_LockForUpdate 验证 SQLite 不支持 FOR UPDATE：返回错误。
func TestSQLiteInteg_LockForUpdate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 1).LockForUpdate().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_SharedLock 验证 SQLite 不支持 LOCK IN SHARE MODE：返回错误。
func TestSQLiteInteg_SharedLock(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 1).SharedLock().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// ==================== Group 12: Transaction ====================

// TestSQLiteInteg_TransactionCommit 验证事务提交：回调返回 nil 时，修改持久化。
func TestSQLiteInteg_TransactionCommit(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_TransactionRollback 验证事务回滚：回调返回 error 时，修改被撤销。
func TestSQLiteInteg_TransactionRollback(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_TransactionNested 验证嵌套事务传播：内层事务复用外层事务，提交后整体生效。
func TestSQLiteInteg_TransactionNested(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_TransactionPanicRollback 验证事务回调 panic 时，事务应自动回滚。
func TestSQLiteInteg_TransactionPanicRollback(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_FirstInvalidDest 验证 First 传入非指针类型时返回错误。
func TestSQLiteInteg_FirstInvalidDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").First(context.Background(), r)
	if err == nil {
		t.Fatalf("expected error for non-pointer dest, got nil")
	}
}

// TestSQLiteInteg_FirstNilDest 验证 First 传入 nil 时返回错误。
func TestSQLiteInteg_FirstNilDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	err := db.Builder().Table("users").Select("name").First(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil dest, got nil")
	}
}

// TestSQLiteInteg_FirstIntPtrDest 验证 First 传入非结构体指针（*int）时返回错误。
func TestSQLiteInteg_FirstIntPtrDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").First(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest, got nil")
	}
}

// TestSQLiteInteg_FindInvalidDest 验证 Find 传入 *int（非结构体切片指针）时返回错误。
func TestSQLiteInteg_FindInvalidDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Find, got nil")
	}
}

// TestSQLiteInteg_FindNonPointerDest 验证 Find 传入非指针（[]struct）时返回错误。
func TestSQLiteInteg_FindNonPointerDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Find(context.Background(), rows)
	if err == nil {
		t.Fatalf("expected error for non-pointer slice dest, got nil")
	}
}

// TestSQLiteInteg_FindIntPtrDest 验证 Find 传入 *[]int（非结构体切片指针）时返回错误。
func TestSQLiteInteg_FindIntPtrDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Fatalf("expected error for *[]int dest in Find, got nil")
	}
}

// TestSQLiteInteg_PaginateInvalidDest 验证 Paginate 传入 *int（非结构体切片指针）时返回错误。
func TestSQLiteInteg_PaginateInvalidDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var n int
	_, err := db.Builder().Table("users").Select("name").Paginate(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Paginate, got nil")
	}
}

// TestSQLiteInteg_ValueNoRows 验证 Value 无匹配数据时返回 sql.ErrNoRows。
func TestSQLiteInteg_ValueNoRows(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).Value(context.Background(), &name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
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

// ==================== Group 15: Bug 验证集成测试 ====================

// TestSQLiteInteg_Bug_CountWithUnion 验证 Count() 对 UNION 查询返回正确结果。
// 数据：active 用户 3 人，age>25 用户 3 人 (eve age 为 NULL 不计入)。
// UNION ALL 不去重，正确总数应为 6。修复前生成无效 SQL 报错。
func TestSQLiteInteg_Bug_CountWithUnion(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	union := db.Builder().Table("users").Where("age", ">", 25)
	b := db.Builder().Table("users").Where("status", "=", "active").UnionAll(union)

	count, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	// 正确结果应为 6（3 active + 3 age>25，UNION ALL 不去重）
	if count != 6 {
		t.Errorf("Count with UNION ALL expected 6, got %d", count)
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

// TestSQLiteInteg_Bug_SelectSubTableSubBindingOrder 验证 SELECT 子查询与 FROM 子查询同时含绑定参数时，
// 收集顺序与 SQL 占位符顺序一致（SELECT 子查询在前，FROM 子查询在后）。
func TestSQLiteInteg_Bug_SelectSubTableSubBindingOrder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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
		SelectSubquery(scalarSub, "big_count").
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

// TestSQLiteInteg_Bug_CloneNestedBuilder 验证 Clone 对嵌套 Builder（UNION 子查询）深拷贝：
// 修改原始嵌套 Builder 后，克隆体不受影响。
func TestSQLiteInteg_Bug_CloneNestedBuilder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// ==================== Group 16: OR 条件 ====================

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

// ==================== Group 17: RIGHT JOIN ====================

// TestSQLiteInteg_RightJoin 验证 RIGHT JOIN：右表所有行都保留（SQLite 3.39+）。
func TestSQLiteInteg_RightJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_RightJoinOn 验证 RightJoinOn 多条件：RIGHT JOIN + 回调式 ON 条件。
func TestSQLiteInteg_RightJoinOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// ==================== Group 18: HAVING 子句 ====================

// TestSQLiteInteg_HavingBasic 验证 Having 基本用法：HAVING SUM(amount) > 100。
func TestSQLiteInteg_HavingBasic(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_OrHaving 验证 OrHaving：HAVING SUM>200 OR SUM<50。
func TestSQLiteInteg_OrHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_HavingNotBetween 验证 HavingNotBetween：HAVING SUM NOT BETWEEN 100 AND 200。
func TestSQLiteInteg_HavingNotBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_OrderByDesc 验证 OrderByDesc 降序排序。
func TestSQLiteInteg_OrderByDesc(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_OrderByRaw 验证 OrderByRaw 原始 SQL 排序。
func TestSQLiteInteg_OrderByRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_JoinOnOrWhere 验证 JoinBuilder.OrWhere：JOIN ON 中的 OR 值条件。
func TestSQLiteInteg_JoinOnOrWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_JoinOnRaw 验证 JoinBuilder.Raw：JOIN ON 中的原始 SQL 条件。
func TestSQLiteInteg_JoinOnRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Raw("orders.amount > ?", 100)
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

// TestSQLiteInteg_Complex_TableSubJoinGroupHaving 验证 FROM子查询 + JOIN + GROUP BY + HAVING 组合。
// 预期：bob(2单,280), alice(2单,170)
func TestSQLiteInteg_Complex_TableSubJoinGroupHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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
		TableSub(sub, "t").
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

// TestSQLiteInteg_Complex_SelectSubWhereInSubNestedWhere 验证 SELECT子查询列 + WHERE IN子查询 + 嵌套WHERE。
// 预期：bob(30,2), diana(28,1), alice(25,2)
func TestSQLiteInteg_Complex_SelectSubWhereInSubNestedWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_Complex_UnionAllJoinOrderBy 验证 UNION ALL + JOIN 组合。
// 预期合并后 4 行。
func TestSQLiteInteg_Complex_UnionAllJoinOrderBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_Complex_MultiSubqueryCombination 验证多种子查询类型组合：
// WHERE NOT IN子查询 + WHERE EXISTS + JOIN + ORDER BY + LIMIT。
// 找出「没有个人档案但有订单」的用户，且至少有一笔订单金额 > 100。
// 预期：diana(有 Camera 150，无 profile)
func TestSQLiteInteg_Complex_MultiSubqueryCombination(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)
	setupSQLiteProfilesTable(t, db)

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

// ==================== SchemaInspector 集成测试 ====================

// TestSQLiteInteg_SchemaInspector_Tables 验证 Tables 返回表名（注释始终为空）。
func TestSQLiteInteg_SchemaInspector_Tables(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	tables, err := inspector.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}

	// 应包含 users 和 orders 表
	found := map[string]string{}
	for _, tbl := range tables {
		found[tbl.Name] = tbl.Comment
	}
	if _, ok := found["users"]; !ok {
		t.Errorf("expected 'users' table, not found: %v", tables)
	}
	if _, ok := found["orders"]; !ok {
		t.Errorf("expected 'orders' table, not found: %v", tables)
	}
	// SQLite 不支持表注释，Comment 应为空
	if found["users"] != "" {
		t.Errorf("users: expected empty comment, got %q", found["users"])
	}
}

// TestSQLiteInteg_SchemaInspector_Columns 验证 Columns 返回字段名、类型（注释始终为空）。
func TestSQLiteInteg_SchemaInspector_Columns(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE test_columns (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		age INTEGER,
		email TEXT DEFAULT 'none'
	)`)
	defer mustExec(t, db, `DROP TABLE IF EXISTS test_columns`)

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
		nullable bool
		hasDef   bool
	}{
		// SQLite 的 INTEGER PRIMARY KEY 是 rowid 别名，PRAGMA 中 notnull=0
		{"id", "INTEGER", true, false},
		{"name", "TEXT", false, false},
		{"age", "INTEGER", true, false},
		{"email", "TEXT", true, true},
	}
	for i, c := range checks {
		if columns[i].Name != c.name {
			t.Errorf("col[%d]: expected name %q, got %q", i, c.name, columns[i].Name)
		}
		if columns[i].Type != c.typ {
			t.Errorf("col[%d] %s: expected type %q, got %q", i, c.name, c.typ, columns[i].Type)
		}
		if columns[i].Comment != "" {
			t.Errorf("col[%d] %s: expected empty comment, got %q", i, c.name, columns[i].Comment)
		}
		if columns[i].Nullable != c.nullable {
			t.Errorf("col[%d] %s: expected nullable=%v, got %v", i, c.name, c.nullable, columns[i].Nullable)
		}
		if c.hasDef && columns[i].Default == nil {
			t.Errorf("col[%d] %s: expected default, got nil", i, c.name)
		}
		if !c.hasDef && columns[i].Default != nil {
			t.Errorf("col[%d] %s: expected no default, got %q", i, c.name, *columns[i].Default)
		}
	}
}

// TestSQLiteInteg_SchemaInspector_ColumnsNumericDefault 验证 Columns 能处理数值默认值：
// PRAGMA table_info 的 dflt_value 对 DEFAULT 0/1.5 返回 INTEGER/REAL（int64/float64），
// 而非 TEXT，扫描到 *string 必须显式转换。
func TestSQLiteInteg_SchemaInspector_ColumnsNumericDefault(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE test_num_default (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status INTEGER DEFAULT 0,
			score REAL DEFAULT 1.5,
			flag TEXT DEFAULT 'x'
		)`)
	defer mustExec(t, db, `DROP TABLE IF EXISTS test_num_default`)

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "test_num_default")
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}

	checks := []struct {
		name     string
		hasDef   bool
		defValue string
	}{
		{"id", false, ""},
		{"status", true, "0"},
		{"score", true, "1.5"},
		{"flag", true, "'x'"},
	}
	for i, c := range checks {
		if c.hasDef && (columns[i].Default == nil || *columns[i].Default != c.defValue) {
			t.Errorf("col[%d] %s: expected default %q, got %v", i, c.name, c.defValue, columns[i].Default)
		}
		if !c.hasDef && columns[i].Default != nil {
			t.Errorf("col[%d] %s: expected no default, got %q", i, c.name, *columns[i].Default)
		}
	}
}

// ==================== Cursor 迭代器集成测试 ====================

// TestSQLiteInteg_Cursor_Stream 验证 Cursor 流式迭代：逐行扫描，break 时自动释放连接。
// 同时验证 NULL 安全扫描：eve 的 age 为 NULL，扫描到 int 类型时保留零值 0。
func TestSQLiteInteg_Cursor_Stream(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Cursor_Break 验证 Cursor 迭代中 break 能正常释放资源。
func TestSQLiteInteg_Cursor_Break(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_Keyset 验证 CursorBy 游标分页迭代：分批获取全部数据。
func TestSQLiteInteg_CursorBy_Keyset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_Break 验证 CursorBy 迭代中 break 能正常停止。
func TestSQLiteInteg_CursorBy_Break(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_IgnoresOrderBy 验证 CursorBy 会忽略已设置的 ORDER BY。
func TestSQLiteInteg_CursorBy_IgnoresOrderBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_NullCursorValue 验证游标列值为 NULL 时迭代器报错终止，
// 而不是无限循环重复返回同一批数据。
func TestSQLiteInteg_CursorBy_NullCursorValue(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// age 用指针类型，eve 的 age 为 NULL
	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Age  *int   `db:"age"`
	}
	var user row
	gotErr := false
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name", "age").CursorBy(context.Background(), &user, 2, "age") {
		if err != nil {
			gotErr = true
			break
		}
		count++
		if count > 100 {
			t.Fatalf("CursorBy 未终止，疑似死循环（已迭代 %d 次）", count)
		}
	}
	if !gotErr {
		t.Errorf("expected error for NULL cursor value, got nil (iterated %d times)", count)
	}
	if count != 0 {
		t.Errorf("expected 0 iterations before error, got %d", count)
	}
}

// TestSQLiteInteg_CursorBy_NilEmbeddedPtr 验证 dest 的嵌入指针结构体为 nil 时，
// CursorBy 返回错误而非 panic。
func TestSQLiteInteg_CursorBy_NilEmbeddedPtr(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 嵌入类型名必须大写导出，才会被字段展开
	type Base struct {
		ID int `db:"id"`
	}
	// 嵌入指针未初始化，为 nil
	type user struct {
		*Base
		Name string `db:"name"`
	}
	var u user
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CursorBy panicked: %v", r)
		}
	}()
	gotErr := false
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &u, 2, "id") {
		if err != nil {
			gotErr = true
			break
		}
		count++
		if count > 10 {
			t.Fatalf("CursorBy 未终止，疑似死循环")
		}
	}
	if !gotErr {
		t.Errorf("expected error for unavailable cursor field, got nil (iterated %d times)", count)
	}
}

// TestSQLiteInteg_Cursor_ErrorPaths 验证 Cursor 对非法 dest 返回错误：
// 非指针与非结构体指针均应在迭代首轮 yield 错误。
func TestSQLiteInteg_Cursor_ErrorPaths(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 非指针 dest（结构体值）
	gotErr := false
	for err := range db.Builder().Table("users").Cursor(context.Background(), struct {
		Name string `db:"name"`
	}{}) {
		gotErr = errors.Is(err, ErrNotPointer)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotPointer for non-pointer dest, got nil")
	}

	// 非结构体指针 dest（*int）
	gotErr = false
	var num int
	for err := range db.Builder().Table("users").Cursor(context.Background(), &num) {
		gotErr = errors.Is(err, ErrNotStruct)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotStruct for *int dest, got nil")
	}
}

// TestSQLiteInteg_CursorBy_ErrorPaths 验证 CursorBy 对非法 dest 与缺失游标字段返回错误。
func TestSQLiteInteg_CursorBy_ErrorPaths(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 非指针 dest
	gotErr := false
	for err := range db.Builder().Table("users").CursorBy(context.Background(), struct {
		Name string `db:"name"`
	}{}, 2, "id") {
		gotErr = errors.Is(err, ErrNotPointer)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotPointer for non-pointer dest, got nil")
	}

	// 非结构体指针 dest（*int）
	gotErr = false
	var num int
	for err := range db.Builder().Table("users").CursorBy(context.Background(), &num, 2, "id") {
		gotErr = errors.Is(err, ErrNotStruct)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotStruct for *int dest, got nil")
	}

	// 缺失游标字段：结构体不含 cursorColumn 对应字段
	gotErr = false
	var noCursor struct {
		Name string `db:"name"`
	}
	for err := range db.Builder().Table("users").CursorBy(context.Background(), &noCursor, 2, "id") {
		gotErr = errors.Is(err, ErrCursorFieldNotFound)
		break
	}
	if !gotErr {
		t.Error("expected ErrCursorFieldNotFound for missing cursor field, got nil")
	}
}

// TestSQLiteInteg_CursorBy_DefaultChunkSize 验证 chunkSize 为负数时使用默认值 100：
// 数据量小于默认分块大小时应完整迭代。
func TestSQLiteInteg_CursorBy_DefaultChunkSize(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, -1, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, user.Name)
	}
	if len(names) != 5 {
		t.Errorf("expected 5 names, got %d", len(names))
	}
}

// TestSQLiteInteg_ScanStruct_ErrorPaths 验证 ScanStruct/ScanStructClose 对非法 dest 返回错误：
// 非指针、非结构体指针、切片元素非结构体。
func TestSQLiteInteg_ScanStruct_ErrorPaths(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	ctx := context.Background()

	// 非指针 dest（结构体值）
	rows, err := db.Query(ctx, "SELECT id, name FROM users")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if err := ScanStruct(rows, struct {
		Name string `db:"name"`
	}{}); err == nil {
		t.Error("expected error for non-pointer dest, got nil")
	}
	rows.Close()

	// 非结构体指针 dest（*int）
	rows, err = db.Query(ctx, "SELECT id FROM users")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if err := ScanStruct(rows, new(int)); err == nil {
		t.Error("expected error for *int dest, got nil")
	}
	rows.Close()

	// 切片元素非结构体（*[]int）
	rows, err = db.Query(ctx, "SELECT id FROM users")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	var ints []int
	if err := ScanStruct(rows, &ints); err == nil {
		t.Error("expected error for *[]int dest, got nil")
	}
	rows.Close()

	// ScanStructClose 对非法 dest 返回错误，且自动关闭 rows
	rows, err = db.Query(ctx, "SELECT id FROM users")
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if err := ScanStructClose(rows, new(int)); err == nil {
		t.Error("expected error for *int dest via ScanStructClose, got nil")
	}
}

// ==================== nullSafeField 集成测试 ====================

// TestSQLiteInteg_NullSafeField_AllTypes 验证 nullSafeField 能正确处理各种数据类型的 NULL 和非 NULL 值。
// 测试覆盖：整数、浮点数、字符串、布尔值、指针类型。
func TestSQLiteInteg_NullSafeField_AllTypes(t *testing.T) {
	db := openSQLiteTestDB(t)

	// 创建包含各种类型的表
	mustExec(t, db, `CREATE TABLE type_test (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		-- 整数类型
		i_val    INTEGER,
		-- 浮点类型
		f_val    REAL,
		-- 字符串类型
		s_val    TEXT,
		-- 布尔类型（SQLite 用 0/1 存储）
		b_val    INTEGER
	)`)

	// 插入两行数据：一行有值，一行全部为 NULL
	mustExec(t, db, `INSERT INTO type_test (i_val, f_val, s_val, b_val) VALUES (42, 3.14, 'hello', 1)`)
	mustExec(t, db, `INSERT INTO type_test (i_val, f_val, s_val, b_val) VALUES (NULL, NULL, NULL, NULL)`)

	// 测试非指针类型：NULL 时保留零值
	t.Run("NonPointer", func(t *testing.T) {
		type row struct {
			IVal int     `db:"i_val"`
			FVal float64 `db:"f_val"`
			SVal string  `db:"s_val"`
			BVal bool    `db:"b_val"`
		}

		var results []row
		var r row
		for err := range db.Builder().Table("type_test").Select("i_val", "f_val", "s_val", "b_val").
			OrderBy("id", "ASC").Cursor(context.Background(), &r) {
			if err != nil {
				t.Fatalf("Cursor error: %v", err)
			}
			results = append(results, r)
		}

		if len(results) != 2 {
			t.Fatalf("expected 2 rows, got %d", len(results))
		}

		// 第一行：有值
		if results[0].IVal != 42 {
			t.Errorf("row[0].IVal: expected 42, got %d", results[0].IVal)
		}
		if results[0].FVal != 3.14 {
			t.Errorf("row[0].FVal: expected 3.14, got %f", results[0].FVal)
		}
		if results[0].SVal != "hello" {
			t.Errorf("row[0].SVal: expected 'hello', got %q", results[0].SVal)
		}
		if !results[0].BVal {
			t.Errorf("row[0].BVal: expected true, got false")
		}

		// 第二行：NULL → 零值
		if results[1].IVal != 0 {
			t.Errorf("row[1].IVal: expected 0 (NULL), got %d", results[1].IVal)
		}
		if results[1].FVal != 0.0 {
			t.Errorf("row[1].FVal: expected 0.0 (NULL), got %f", results[1].FVal)
		}
		if results[1].SVal != "" {
			t.Errorf("row[1].SVal: expected '' (NULL), got %q", results[1].SVal)
		}
		if results[1].BVal {
			t.Errorf("row[1].BVal: expected false (NULL), got true")
		}
	})

	// 测试指针类型：NULL 时为 nil
	t.Run("Pointer", func(t *testing.T) {
		type row struct {
			IVal *int     `db:"i_val"`
			FVal *float64 `db:"f_val"`
			SVal *string  `db:"s_val"`
			BVal *bool    `db:"b_val"`
		}

		var results []row
		var r row
		for err := range db.Builder().Table("type_test").Select("i_val", "f_val", "s_val", "b_val").
			OrderBy("id", "ASC").Cursor(context.Background(), &r) {
			if err != nil {
				t.Fatalf("Cursor error: %v", err)
			}
			results = append(results, r)
		}

		if len(results) != 2 {
			t.Fatalf("expected 2 rows, got %d", len(results))
		}

		// 第一行：有值
		if results[0].IVal == nil || *results[0].IVal != 42 {
			t.Errorf("row[0].IVal: expected *42, got %v", results[0].IVal)
		}
		if results[0].FVal == nil || *results[0].FVal != 3.14 {
			t.Errorf("row[0].FVal: expected *3.14, got %v", results[0].FVal)
		}
		if results[0].SVal == nil || *results[0].SVal != "hello" {
			t.Errorf("row[0].SVal: expected *'hello', got %v", results[0].SVal)
		}
		if results[0].BVal == nil || !*results[0].BVal {
			t.Errorf("row[0].BVal: expected *true, got %v", results[0].BVal)
		}

		// 第二行：NULL → nil
		if results[1].IVal != nil {
			t.Errorf("row[1].IVal: expected nil (NULL), got %v", *results[1].IVal)
		}
		if results[1].FVal != nil {
			t.Errorf("row[1].FVal: expected nil (NULL), got %v", *results[1].FVal)
		}
		if results[1].SVal != nil {
			t.Errorf("row[1].SVal: expected nil (NULL), got %v", *results[1].SVal)
		}
		if results[1].BVal != nil {
			t.Errorf("row[1].BVal: expected nil (NULL), got %v", *results[1].BVal)
		}
	})
}

// TestSQLiteInteg_PoolConcurrentAddSlavePick 验证 AddSlave 与 PickReadDB 的并发安全性：
// 多个 goroutine 并发 PickReadDB，同时主 goroutine 反复 AddSlave，
// 不应 panic 或返回 nil；添加从库后 PickReadDB 应返回从库之一。
// 用 go test -race 运行可验证无数据竞争。
func TestSQLiteInteg_PoolConcurrentAddSlavePick(t *testing.T) {
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("failed to open pool: %v", err)
	}
	defer pool.Close()

	master := pool.PickWriteDB()

	const pickers = 8
	var wg sync.WaitGroup
	wg.Add(pickers)
	stop := make(chan struct{})
	for g := 0; g < pickers; g++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					if db := pool.PickReadDB(); db == nil {
						t.Errorf("PickReadDB returned nil")
						return
					}
				}
			}
		}()
	}

	// 主 goroutine 反复添加从库（SQLite :memory: 每个连接独立，仅验证路由行为）
	for i := 0; i < 5; i++ {
		if err := pool.AddSlave(":memory:"); err != nil {
			t.Fatalf("AddSlave error: %v", err)
		}
	}

	close(stop)
	wg.Wait()

	// 添加从库后 PickReadDB 应返回从库之一（随机策略）
	if readDB := pool.PickReadDB(); readDB == master {
		t.Errorf("expected PickReadDB to return a slave after AddSlave, got master")
	}
}

// TestSQLiteInteg_NullSafeField_IntTypes 验证各种整数类型的 NULL 安全扫描。
func TestSQLiteInteg_NullSafeField_IntTypes(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE int_test (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		i8_val  INTEGER,
		i16_val INTEGER,
		i32_val INTEGER,
		i64_val INTEGER,
		u8_val  INTEGER,
		u16_val INTEGER,
		u32_val INTEGER,
		u64_val INTEGER
	)`)

	// 插入有值行和 NULL 行（uint64 最大值超出 SQLite int64 存储范围，改用 int64 最大值）
	mustExec(t, db, `INSERT INTO int_test (i8_val, i16_val, i32_val, i64_val, u8_val, u16_val, u32_val, u64_val)
		VALUES (127, 32767, 2147483647, 9223372036854775807, 255, 65535, 4294967295, 9223372036854775807)`)
	mustExec(t, db, `INSERT INTO int_test (i8_val, i16_val, i32_val, i64_val, u8_val, u16_val, u32_val, u64_val)
		VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)

	type row struct {
		I8  int8   `db:"i8_val"`
		I16 int16  `db:"i16_val"`
		I32 int32  `db:"i32_val"`
		I64 int64  `db:"i64_val"`
		U8  uint8  `db:"u8_val"`
		U16 uint16 `db:"u16_val"`
		U32 uint32 `db:"u32_val"`
		U64 uint64 `db:"u64_val"`
	}

	var results []row
	var r row
	for err := range db.Builder().Table("int_test").
		Select("i8_val", "i16_val", "i32_val", "i64_val", "u8_val", "u16_val", "u32_val", "u64_val").
		OrderBy("id", "ASC").Cursor(context.Background(), &r) {
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		results = append(results, r)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 第一行：验证各种整数类型的最大值
	if results[0].I8 != 127 {
		t.Errorf("I8: expected 127, got %d", results[0].I8)
	}
	if results[0].I16 != 32767 {
		t.Errorf("I16: expected 32767, got %d", results[0].I16)
	}
	if results[0].I32 != 2147483647 {
		t.Errorf("I32: expected 2147483647, got %d", results[0].I32)
	}
	if results[0].I64 != 9223372036854775807 {
		t.Errorf("I64: expected 9223372036854775807, got %d", results[0].I64)
	}
	if results[0].U8 != 255 {
		t.Errorf("U8: expected 255, got %d", results[0].U8)
	}
	if results[0].U16 != 65535 {
		t.Errorf("U16: expected 65535, got %d", results[0].U16)
	}
	if results[0].U32 != 4294967295 {
		t.Errorf("U32: expected 4294967295, got %d", results[0].U32)
	}
	if results[0].U64 != 9223372036854775807 {
		t.Errorf("U64: expected 9223372036854775807, got %d", results[0].U64)
	}

	// 第二行：全部为零值
	if results[1].I8 != 0 || results[1].I16 != 0 || results[1].I32 != 0 || results[1].I64 != 0 {
		t.Errorf("row[1] signed ints: expected all 0, got %d %d %d %d",
			results[1].I8, results[1].I16, results[1].I32, results[1].I64)
	}
	if results[1].U8 != 0 || results[1].U16 != 0 || results[1].U32 != 0 || results[1].U64 != 0 {
		t.Errorf("row[1] unsigned ints: expected all 0, got %d %d %d %d",
			results[1].U8, results[1].U16, results[1].U32, results[1].U64)
	}
}

// TestSQLiteInteg_NullSafeField_FloatTypes 验证 float32/float64 的 NULL 安全扫描。
func TestSQLiteInteg_NullSafeField_FloatTypes(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE float_test (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		f32_val REAL,
		f64_val REAL
	)`)

	mustExec(t, db, `INSERT INTO float_test (f32_val, f64_val) VALUES (1.5, 2.718281828)`)
	mustExec(t, db, `INSERT INTO float_test (f32_val, f64_val) VALUES (NULL, NULL)`)

	type row struct {
		F32 float32 `db:"f32_val"`
		F64 float64 `db:"f64_val"`
	}

	var results []row
	var r row
	for err := range db.Builder().Table("float_test").Select("f32_val", "f64_val").
		OrderBy("id", "ASC").Cursor(context.Background(), &r) {
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		results = append(results, r)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(results))
	}

	// 第一行
	if results[0].F32 != 1.5 {
		t.Errorf("F32: expected 1.5, got %f", results[0].F32)
	}
	if results[0].F64 != 2.718281828 {
		t.Errorf("F64: expected 2.718281828, got %f", results[0].F64)
	}

	// 第二行：NULL → 0
	if results[1].F32 != 0 {
		t.Errorf("F32: expected 0 (NULL), got %f", results[1].F32)
	}
	if results[1].F64 != 0 {
		t.Errorf("F64: expected 0 (NULL), got %f", results[1].F64)
	}
}

// TestSQLiteInteg_NullSafeField_BooleanType 验证 BOOLEAN 类型（SQLite 用 INTEGER 0/1 存储）的 NULL 安全扫描。
// SQLite 没有原生 BOOLEAN 类型，DECLARE BOOLEAN 实际使用 INTEGER 亲和性。
func TestSQLiteInteg_NullSafeField_BooleanType(t *testing.T) {
	db := openSQLiteTestDB(t)

	// 创建包含 BOOLEAN 类型字段的表（SQLite 会当作 INTEGER 处理）
	mustExec(t, db, `CREATE TABLE bool_test (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		is_active  BOOLEAN,
		is_deleted BOOLEAN,
		is_valid   BOOLEAN NOT NULL DEFAULT 1
	)`)

	// 插入测试数据：TRUE/FALSE/NULL
	mustExec(t, db, `INSERT INTO bool_test (is_active, is_deleted, is_valid) VALUES (1, 0, 1)`)
	mustExec(t, db, `INSERT INTO bool_test (is_active, is_deleted, is_valid) VALUES (0, 1, 0)`)
	mustExec(t, db, `INSERT INTO bool_test (is_active, is_deleted, is_valid) VALUES (NULL, NULL, 1)`)

	// 测试非指针 bool 类型
	t.Run("NonPointer", func(t *testing.T) {
		type row struct {
			IsActive  bool `db:"is_active"`
			IsDeleted bool `db:"is_deleted"`
			IsValid   bool `db:"is_valid"`
		}

		var results []row
		var r row
		for err := range db.Builder().Table("bool_test").Select("is_active", "is_deleted", "is_valid").
			OrderBy("id", "ASC").Cursor(context.Background(), &r) {
			if err != nil {
				t.Fatalf("Cursor error: %v", err)
			}
			results = append(results, r)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(results))
		}

		// 第一行：TRUE, FALSE, TRUE
		if !results[0].IsActive {
			t.Errorf("row[0].IsActive: expected true, got false")
		}
		if results[0].IsDeleted {
			t.Errorf("row[0].IsDeleted: expected false, got true")
		}
		if !results[0].IsValid {
			t.Errorf("row[0].IsValid: expected true, got false")
		}

		// 第二行：FALSE, TRUE, FALSE
		if results[1].IsActive {
			t.Errorf("row[1].IsActive: expected false, got true")
		}
		if !results[1].IsDeleted {
			t.Errorf("row[1].IsDeleted: expected true, got false")
		}
		if results[1].IsValid {
			t.Errorf("row[1].IsValid: expected false, got true")
		}

		// 第三行：NULL, NULL, TRUE → false, false, true
		if results[2].IsActive {
			t.Errorf("row[2].IsActive: expected false (NULL), got true")
		}
		if results[2].IsDeleted {
			t.Errorf("row[2].IsDeleted: expected false (NULL), got true")
		}
		if !results[2].IsValid {
			t.Errorf("row[2].IsValid: expected true, got false")
		}
	})

	// 测试指针 *bool 类型
	t.Run("Pointer", func(t *testing.T) {
		type row struct {
			IsActive  *bool `db:"is_active"`
			IsDeleted *bool `db:"is_deleted"`
			IsValid   *bool `db:"is_valid"`
		}

		var results []row
		var r row
		for err := range db.Builder().Table("bool_test").Select("is_active", "is_deleted", "is_valid").
			OrderBy("id", "ASC").Cursor(context.Background(), &r) {
			if err != nil {
				t.Fatalf("Cursor error: %v", err)
			}
			results = append(results, r)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(results))
		}

		// 第一行：TRUE, FALSE, TRUE
		if results[0].IsActive == nil || !*results[0].IsActive {
			t.Errorf("row[0].IsActive: expected *true, got %v", results[0].IsActive)
		}
		if results[0].IsDeleted == nil || *results[0].IsDeleted {
			t.Errorf("row[0].IsDeleted: expected *false, got %v", results[0].IsDeleted)
		}
		if results[0].IsValid == nil || !*results[0].IsValid {
			t.Errorf("row[0].IsValid: expected *true, got %v", results[0].IsValid)
		}

		// 第二行：FALSE, TRUE, FALSE
		if results[1].IsActive == nil || *results[1].IsActive {
			t.Errorf("row[1].IsActive: expected *false, got %v", results[1].IsActive)
		}
		if results[1].IsDeleted == nil || !*results[1].IsDeleted {
			t.Errorf("row[1].IsDeleted: expected *true, got %v", results[1].IsDeleted)
		}
		if results[1].IsValid == nil || *results[1].IsValid {
			t.Errorf("row[1].IsValid: expected *false, got %v", results[1].IsValid)
		}

		// 第三行：NULL, NULL, TRUE → nil, nil, *true
		if results[2].IsActive != nil {
			t.Errorf("row[2].IsActive: expected nil (NULL), got %v", *results[2].IsActive)
		}
		if results[2].IsDeleted != nil {
			t.Errorf("row[2].IsDeleted: expected nil (NULL), got %v", *results[2].IsDeleted)
		}
		if results[2].IsValid == nil || !*results[2].IsValid {
			t.Errorf("row[2].IsValid: expected *true, got %v", results[2].IsValid)
		}
	})
}

// TestSQLiteInteg_NullSafeField_BlobType 验证 BLOB 类型的 NULL 安全扫描。
// BLOB 用于存储二进制数据（如图片、文件），在 Go 中对应 []byte。
func TestSQLiteInteg_NullSafeField_BlobType(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE blob_test (
		id      INTEGER PRIMARY KEY AUTOINCREMENT,
		data    BLOB,
		thumbnail BLOB
	)`)

	// 插入测试数据：有值和 NULL
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	thumbnailData := []byte("thumbnail_bytes")
	mustExec(t, db, `INSERT INTO blob_test (data, thumbnail) VALUES (?, ?)`, binaryData, thumbnailData)
	mustExec(t, db, `INSERT INTO blob_test (data, thumbnail) VALUES (NULL, NULL)`)
	mustExec(t, db, `INSERT INTO blob_test (data, thumbnail) VALUES (?, NULL)`, []byte("only_data"))

	// 测试 []byte 类型（非指针）
	t.Run("ByteSlice", func(t *testing.T) {
		type row struct {
			Data      []byte `db:"data"`
			Thumbnail []byte `db:"thumbnail"`
		}

		var results []row
		var r row
		for err := range db.Builder().Table("blob_test").Select("data", "thumbnail").
			OrderBy("id", "ASC").Cursor(context.Background(), &r) {
			if err != nil {
				t.Fatalf("Cursor error: %v", err)
			}
			results = append(results, r)
		}

		if len(results) != 3 {
			t.Fatalf("expected 3 rows, got %d", len(results))
		}

		// 第一行：有值
		if len(results[0].Data) != len(binaryData) {
			t.Errorf("row[0].Data: expected %d bytes, got %d", len(binaryData), len(results[0].Data))
		}
		for i, b := range results[0].Data {
			if b != binaryData[i] {
				t.Errorf("row[0].Data[%d]: expected %x, got %x", i, binaryData[i], b)
			}
		}
		if string(results[0].Thumbnail) != "thumbnail_bytes" {
			t.Errorf("row[0].Thumbnail: expected 'thumbnail_bytes', got %q", results[0].Thumbnail)
		}

		// 第二行：NULL → nil 或空切片
		if results[1].Data != nil && len(results[1].Data) != 0 {
			t.Errorf("row[1].Data: expected nil or empty (NULL), got %v", results[1].Data)
		}
		if results[1].Thumbnail != nil && len(results[1].Thumbnail) != 0 {
			t.Errorf("row[1].Thumbnail: expected nil or empty (NULL), got %v", results[1].Thumbnail)
		}

		// 第三行：data 有值，thumbnail 为 NULL
		if string(results[2].Data) != "only_data" {
			t.Errorf("row[2].Data: expected 'only_data', got %q", results[2].Data)
		}
		if results[2].Thumbnail != nil && len(results[2].Thumbnail) != 0 {
			t.Errorf("row[2].Thumbnail: expected nil or empty (NULL), got %v", results[2].Thumbnail)
		}
	})
}

// ==================== BUG 验证 ====================

// TestSQLiteInteg_CountWithGroupBy 验证 GROUP BY 下的 Count 行为。
// 当前行为（BUG）：生成 SELECT COUNT(*) FROM orders GROUP BY user_id，
// 返回每组一行的计数，Count 只取第一行（user_id=1 的 2），与"记录总数"语义不符。
// 期望行为：返回分组数量（4）。
func TestSQLiteInteg_CountWithGroupBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// orders 共 6 条：user_id 1×2、2×2、3×1、4×1 → 4 个分组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 4 {
		t.Fatalf("Count with GROUP BY: expected 4 (number of groups), got %d", count)
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

// TestSQLiteInteg_CountWithGroupByHaving 验证 GROUP BY + HAVING 的 Count 真实执行：
// 返回满足 HAVING 条件的分组数量（非第一组行数）。
func TestSQLiteInteg_CountWithGroupByHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_ExistsWithGroupBy 验证 GROUP BY + HAVING 下的 Exists 真实执行：
// Exists 基于 Count（分组数量 > 0）判断，语义为“是否存在满足条件的分组”。
func TestSQLiteInteg_ExistsWithGroupBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// 3 组满足 SUM(amount) > 100 → true
	exists, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		Exists(context.Background())
	assertNoError(t, err)
	if !exists {
		t.Error("Exists with GROUP BY + HAVING: expected true, got false")
	}

	// 无任何组满足 → false
	exists, err = db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 99999).
		Exists(context.Background())
	assertNoError(t, err)
	if exists {
		t.Error("Exists with GROUP BY + HAVING (no match): expected false, got true")
	}
}

// TestSQLiteInteg_HavingExpression 验证 HAVING 值传 Expression 时真实执行（直接内嵌 SQL）。
func TestSQLiteInteg_HavingExpression(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_OffsetWithoutLimit 验证仅 Offset 无 Limit 时真实执行。
func TestSQLiteInteg_OffsetWithoutLimit(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CountWithDistinct 验证 Distinct + Count 去重计数真实执行。
func TestSQLiteInteg_CountWithDistinct(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE tags (name TEXT NOT NULL)`)
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

// TestSQLiteInteg_ScanNumericToString 验证 SQLite 数值列（驱动返回 int64）
// 扫描到 string 字段时得到数字字符串而非字符码。
func TestSQLiteInteg_ScanNumericToString(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE num_str_test (id INTEGER PRIMARY KEY AUTOINCREMENT, v INTEGER)`)
	mustExec(t, db, `INSERT INTO num_str_test (v) VALUES (123)`)

	var r struct {
		V string `db:"v"`
	}
	err := db.Builder().
		Table("num_str_test").
		Select("v").
		First(context.Background(), &r)
	assertNoError(t, err)
	if r.V != "123" {
		t.Errorf("expected \"123\", got %q", r.V)
	}
}

// TestSQLiteInteg_ScanJsonToIntSlice 验证 JSON 列扫描到 []int 字段：
// BLOB 列（驱动返回 []byte）与 TEXT 列（驱动返回 string）都应正确反序列化。
func TestSQLiteInteg_ScanJsonToIntSlice(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE json_arr_test (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		arr_blob BLOB,
		arr_text TEXT
	)`)
	// [1,2,3] 的十六进制：5B 31 2C 32 2C 33 5D
	mustExec(t, db, `INSERT INTO json_arr_test (arr_blob, arr_text) VALUES (X'5B312C322C335D', '[1,2,3]')`)

	var r struct {
		ArrBlob []int `db:"arr_blob"`
		ArrText []int `db:"arr_text"`
	}
	err := db.Builder().
		Table("json_arr_test").
		Select("arr_blob", "arr_text").
		First(context.Background(), &r)
	assertNoError(t, err)
	if len(r.ArrBlob) != 3 || r.ArrBlob[0] != 1 || r.ArrBlob[2] != 3 {
		t.Errorf("arr_blob: expected [1 2 3], got %v", r.ArrBlob)
	}
	if len(r.ArrText) != 3 || r.ArrText[0] != 1 || r.ArrText[2] != 3 {
		t.Errorf("arr_text: expected [1 2 3], got %v", r.ArrText)
	}
}

// TestSQLiteInteg_GroupLatestPerFund 验证「分组取最新」：JOIN 派生表取每只基金 MAX(ed) 的完整记录。
// 等价 SQL：
//
//	SELECT t1.* FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN (?, ?) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  WHERE t1.fund_code IN (?, ?)
//
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestSQLiteInteg_GroupLatestPerFund(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	codes := []any{"A", "B"}
	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		WhereIn("fund_code", codes).
		GroupBy("fund_code")

	type row struct {
		FundCode string  `db:"fund_code"`
		Ed       string  `db:"ed"`
		NetValue float64 `db:"net_value"`
	}
	var rows []row
	err := db.Builder().Table("fund_net_value AS t1").
		Select("t1.*").
		JoinSub(sub, "t2", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t2.fund_code").
				On("t1.ed", "=", "t2.ed")
		}).
		WhereIn("t1.fund_code", codes).
		OrderBy("t1.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 latest rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Ed != "2024-03-01" || rows[0].NetValue != 1.50 {
		t.Errorf("row[0]: expected A/2024-03-01/1.50, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Ed != "2024-02-01" || rows[1].NetValue != 2.30 {
		t.Errorf("row[1]: expected B/2024-02-01/2.30, got %+v", rows[1])
	}
}

// TestSQLiteInteg_GroupLatestWindow 验证「分组取最新」窗口函数写法：
// ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) 取每组最新一条，结果与 JoinSub 版一致。
// 等价 SQL：
//
//	SELECT x.fund_code, x.ed, x.net_value
//	FROM (
//	  SELECT fund_code, ed, net_value,
//	    ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) AS rn
//	  FROM fund_net_value
//	) AS x
//	WHERE x.fund_code IN (?, ?) AND x.rn = 1
//
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestSQLiteInteg_GroupLatestWindow(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	codes := []any{"A", "B"}
	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "ed", "net_value",
			"ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) AS rn")

	type row struct {
		FundCode string  `db:"fund_code"`
		Ed       string  `db:"ed"`
		NetValue float64 `db:"net_value"`
	}
	var rows []row
	err := db.Builder().TableSub(sub, "x").
		Select("x.fund_code", "x.ed", "x.net_value").
		WhereIn("x.fund_code", codes).
		Where("x.rn", "=", 1).
		OrderBy("x.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 latest rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Ed != "2024-03-01" || rows[0].NetValue != 1.50 {
		t.Errorf("row[0]: expected A/2024-03-01/1.50, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Ed != "2024-02-01" || rows[1].NetValue != 2.30 {
		t.Errorf("row[1]: expected B/2024-02-01/2.30, got %+v", rows[1])
	}
}

// TestSQLiteInteg_JoinSub_LeftJoin 验证 LeftJoinSub：主表 LEFT JOIN 聚合派生表，
// 未匹配的基金行保留且派生表列为 NULL（扫描为零值）。
// 等价 SQL：
//
//	SELECT f.fund_code, f.name, t2.ed, t2.cnt
//	FROM funds AS f
//	  LEFT JOIN (SELECT fund_code, MAX(ed) AS ed, COUNT(*) AS cnt
//	    FROM fund_net_value GROUP BY fund_code) AS t2
//	  ON f.fund_code = t2.fund_code
//
// 预期：A/基金A/2024-03-01/3，B/基金B/2024-02-01/2，D/基金D/""/0（D 无净值记录，t2 列为 NULL）。
func TestSQLiteInteg_JoinSub_LeftJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE funds (
		fund_code TEXT PRIMARY KEY,
		name      TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO funds (fund_code, name) VALUES
		('A', '基金A'), ('B', '基金B'), ('D', '基金D')`)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed", "COUNT(*) AS cnt").
		GroupBy("fund_code")

	type row struct {
		FundCode string `db:"fund_code"`
		Name     string `db:"name"`
		Ed       string `db:"ed"`
		Cnt      int    `db:"cnt"`
	}
	var rows []row
	err := db.Builder().Table("funds AS f").
		Select("f.fund_code", "f.name", "t2.ed", "t2.cnt").
		LeftJoinSub(sub, "t2", func(j *JoinBuilder) {
			j.On("f.fund_code", "=", "t2.fund_code")
		}).
		OrderBy("f.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Name != "基金A" || rows[0].Ed != "2024-03-01" || rows[0].Cnt != 3 {
		t.Errorf("row[0]: expected A/基金A/2024-03-01/3, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Name != "基金B" || rows[1].Ed != "2024-02-01" || rows[1].Cnt != 2 {
		t.Errorf("row[1]: expected B/基金B/2024-02-01/2, got %+v", rows[1])
	}
	if rows[2].FundCode != "D" || rows[2].Name != "基金D" || rows[2].Ed != "" || rows[2].Cnt != 0 {
		t.Errorf("row[2]: expected D/基金D//0 (NULL scanned to zero value), got %+v", rows[2])
	}
}

// TestSQLiteInteg_JoinSub_RightJoin 验证 RightJoinSub：聚合派生表 RIGHT JOIN 主表，
// 右侧（funds）全保留，与 LeftJoin 用例镜像。
// 等价 SQL：
//
//	SELECT f.fund_code, t2.ed
//	FROM (SELECT fund_code, MAX(ed) AS ed
//	  FROM fund_net_value GROUP BY fund_code) AS t2
//	  RIGHT JOIN funds AS f ON t2.fund_code = f.fund_code
//
// 预期：A/2024-03-01，B/2024-02-01，D/""（D 无匹配，t2.ed 为 NULL）。
func TestSQLiteInteg_JoinSub_RightJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE funds (
		fund_code TEXT PRIMARY KEY,
		name      TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO funds (fund_code, name) VALUES
		('A', '基金A'), ('B', '基金B'), ('D', '基金D')`)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		GroupBy("fund_code")

	type row struct {
		FundCode string `db:"fund_code"`
		Ed       string `db:"ed"`
	}
	var rows []row
	err := db.Builder().TableSub(sub, "t2").
		Select("f.fund_code", "t2.ed").
		RightJoinSub(db.Builder().Table("funds"), "f", func(j *JoinBuilder) {
			j.On("t2.fund_code", "=", "f.fund_code")
		}).
		OrderBy("f.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Ed != "2024-03-01" {
		t.Errorf("row[0]: expected A/2024-03-01, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Ed != "2024-02-01" {
		t.Errorf("row[1]: expected B/2024-02-01, got %+v", rows[1])
	}
	if rows[2].FundCode != "D" || rows[2].Ed != "" {
		t.Errorf("row[2]: expected D/\"\" (NULL scanned to zero value), got %+v", rows[2])
	}
}

// TestSQLiteInteg_JoinSub_MultiSub 验证同一查询串联两个 JoinSub（派生表）：
// 子查询绑定（WhereIn + HAVING 值）、ON 值绑定（j.Where）、主查询绑定的收集顺序与 SQL 文本一致。
// 等价 SQL：
//
//	SELECT t1.fund_code, t1.net_value, t3.cnt
//	FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN (?, ?) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  INNER JOIN (SELECT fund_code, COUNT(*) AS cnt FROM fund_net_value
//	    GROUP BY fund_code HAVING COUNT(*) >= ?) AS t3
//	  ON t1.fund_code = t3.fund_code AND t3.cnt > ?
//	WHERE t1.fund_code IN (?, ?)
//
// 绑定顺序：t2 子查询 IN → t3 子查询 HAVING → ON 值 → 主查询 WHERE。
// 预期：A/1.50/3，B/2.30/2；C 被子查询 HAVING 过滤。
func TestSQLiteInteg_JoinSub_MultiSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	codes := []any{"A", "B"}
	t2 := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		WhereIn("fund_code", codes).
		GroupBy("fund_code")
	t3 := db.Builder().Table("fund_net_value").
		Select("fund_code", "COUNT(*) AS cnt").
		GroupBy("fund_code").
		Having("COUNT(*)", ">=", 2)

	type row struct {
		FundCode string  `db:"fund_code"`
		NetValue float64 `db:"net_value"`
		Cnt      int     `db:"cnt"`
	}
	var rows []row
	err := db.Builder().Table("fund_net_value AS t1").
		Select("t1.fund_code", "t1.net_value", "t3.cnt").
		JoinSub(t2, "t2", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t2.fund_code").
				On("t1.ed", "=", "t2.ed")
		}).
		JoinSub(t3, "t3", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t3.fund_code").
				Where("t3.cnt", ">", 0)
		}).
		WhereIn("t1.fund_code", codes).
		OrderBy("t1.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].NetValue != 1.50 || rows[0].Cnt != 3 {
		t.Errorf("row[0]: expected A/1.50/3, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].NetValue != 2.30 || rows[1].Cnt != 2 {
		t.Errorf("row[1]: expected B/2.30/2, got %+v", rows[1])
	}
}

// TestSQLiteInteg_CrossJoinSub 验证 CrossJoinSub：CROSS JOIN 派生表生成「门店 × 月份」组合矩阵，
// 再 LEFT JOIN 事实表补零（无销售记录的组合 amount=0）。
// 等价 SQL：
//
//	SELECT m.month, s.store_name, COALESCE(sales.amount, 0) AS amount
//	FROM (SELECT DISTINCT month FROM sales) AS m
//	  CROSS JOIN (SELECT DISTINCT store_name FROM sales
//	    WHERE store_name IN (?, ?)) AS s
//	  LEFT JOIN sales ON sales.month = m.month AND sales.store_name = s.store_name
//
// 预期 6 行矩阵：店A/店B × 2024-01/02/03，其中 2024-03 店A、2024-02/03 店B 无销售记录补 0。
func TestSQLiteInteg_CrossJoinSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE sales (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		store_name TEXT NOT NULL,
		month      TEXT NOT NULL,
		amount     REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO sales (store_name, month, amount) VALUES
		('店A', '2024-01', 100.00),
		('店A', '2024-02', 150.00),
		('店B', '2024-01', 200.00),
		('店C', '2024-01', 50.00),
		('店C', '2024-03', 300.00)`)

	codes := []any{"店A", "店B"}
	m := db.Builder().Table("sales").Select("month").Distinct()
	s := db.Builder().Table("sales").
		Select("store_name").
		Distinct().
		WhereIn("store_name", codes)

	type row struct {
		Month  string  `db:"month"`
		Store  string  `db:"store_name"`
		Amount float64 `db:"amount"`
	}
	var rows []row
	err := db.Builder().TableSub(m, "m").
		Select("m.month", "s.store_name", "COALESCE(sales.amount, 0) AS amount").
		CrossJoinSub(s, "s").
		LeftJoinOn("sales", func(j *JoinBuilder) {
			j.On("sales.month", "=", "m.month").
				On("sales.store_name", "=", "s.store_name")
		}).
		OrderBy("m.month", "ASC").
		OrderBy("s.store_name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected 6 matrix rows, got %d: %v", len(rows), rows)
	}
	expected := []row{
		{Month: "2024-01", Store: "店A", Amount: 100},
		{Month: "2024-01", Store: "店B", Amount: 200},
		{Month: "2024-02", Store: "店A", Amount: 150},
		{Month: "2024-02", Store: "店B", Amount: 0},
		{Month: "2024-03", Store: "店A", Amount: 0},
		{Month: "2024-03", Store: "店B", Amount: 0},
	}
	for i, exp := range expected {
		if rows[i] != exp {
			t.Errorf("row[%d]: expected %+v, got %+v", i, exp, rows[i])
		}
	}
}

// TestSQLiteInteg_Pluck 验证 Pluck：切片目标提取单列值列表，map 目标提取「值=>键」映射（与 Laravel pluck 一致），
// NULL 值扫描为零值（与 Find 一致），查询链（WHERE/ORDER BY）完整生效。
func TestSQLiteInteg_Pluck(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE users (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	mustExec(t, db, `INSERT INTO users (name) VALUES
		('John'),
		('Jane'),
		(NULL),
		('Bob')`)

	// 切片模式：单列值列表（含 NULL 行扫描为零值 ""）
	var names []string
	err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &names, "name")
	if err != nil {
		t.Fatalf("pluck slice error: %v", err)
	}
	expected := []string{"John", "Jane", "", "Bob"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}
	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}

	// map 模式：值=>键 映射（第一列为值、第二列为键，nil map 自动初始化）
	var m map[int64]string
	err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &m, "name", "id")
	if err != nil {
		t.Fatalf("pluck map error: %v", err)
	}
	expectMap := map[int64]string{1: "John", 2: "Jane", 3: "", 4: "Bob"}
	if len(m) != len(expectMap) {
		t.Fatalf("expected %d entries, got %d: %v", len(expectMap), len(m), m)
	}
	for id, exp := range expectMap {
		if m[id] != exp {
			t.Errorf("m[%d]: expected %q, got %q", id, exp, m[id])
		}
	}

	// 查询链生效：WHERE 过滤 NULL 行，ORDER BY DESC 倒序
	names = nil
	err = db.Builder().Table("users").
		Where("name", "!=", "").
		OrderBy("id", "DESC").
		Pluck(context.Background(), &names, "name")
	if err != nil {
		t.Fatalf("pluck filtered error: %v", err)
	}
	expectedFiltered := []string{"Bob", "Jane", "John"}
	if len(names) != len(expectedFiltered) {
		t.Fatalf("expected %d names, got %d: %v", len(expectedFiltered), len(names), names)
	}
	for i, exp := range expectedFiltered {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}
}

// TestSQLiteInteg_PluckKeyBy 验证 Pluck 键列模式（keyBy）：map 值为结构体/结构体指针时，
// 唯一列参数作为键列，整行数据按 db tag 扫描进结构体（NULL 扫零值，与 Find 一致）。
func TestSQLiteInteg_PluckKeyBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE users (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		name     TEXT,
		nickname TEXT
	)`)
	mustExec(t, db, `INSERT INTO users (name, nickname) VALUES
		('John', 'JJ'),
		('Jane', NULL),
		(NULL, 'NN'),
		('Bob', 'BB')`)

	// 场景 A：map 值为结构体，键列在结构体字段中（id 字段同时填充并作键）
	type User struct {
		Id       int
		Name     string
		Nickname string
	}
	var m map[int64]User
	err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &m, "id")
	if err != nil {
		t.Fatalf("pluck keyBy struct error: %v", err)
	}
	if len(m) != 4 {
		t.Fatalf("expected 4 entries, got %d: %v", len(m), m)
	}
	expected := map[int64]User{
		1: {Id: 1, Name: "John", Nickname: "JJ"},
		2: {Id: 2, Name: "Jane", Nickname: ""},
		3: {Id: 3, Name: "", Nickname: "NN"},
		4: {Id: 4, Name: "Bob", Nickname: "BB"},
	}
	for id, exp := range expected {
		if m[id] != exp {
			t.Errorf("m[%d]: expected %+v, got %+v", id, exp, m[id])
		}
	}

	// 场景 B：map 值为结构体指针，每行新建实例
	var mp map[int64]*User
	err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &mp, "id")
	if err != nil {
		t.Fatalf("pluck keyBy ptr error: %v", err)
	}
	if len(mp) != 4 {
		t.Fatalf("expected 4 ptr entries, got %d", len(mp))
	}
	for id, exp := range expected {
		if mp[id] == nil {
			t.Errorf("mp[%d]: expected non-nil pointer", id)
			continue
		}
		if *mp[id] != exp {
			t.Errorf("mp[%d]: expected %+v, got %+v", id, exp, *mp[id])
		}
	}

	// 场景 C：键列不在结构体字段中，SELECT 自动追加键列
	type userBrief struct {
		Name     string
		Nickname string
	}
	var kb map[int64]userBrief
	err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &kb, "id")
	if err != nil {
		t.Fatalf("pluck keyBy external key error: %v", err)
	}
	if len(kb) != 4 {
		t.Fatalf("expected 4 brief entries, got %d: %v", len(kb), kb)
	}
	if kb[1] != (userBrief{Name: "John", Nickname: "JJ"}) || kb[3].Name != "" || kb[3].Nickname != "NN" {
		t.Errorf("kb content mismatch: %+v", kb)
	}
}

// ==================== Laravel 对比：补测试分类 ====================
// 对应 docs/laravel-test-comparison-implementable.md 中"补测试"条目：
// 能力已存在（部分条目配套最小实现），采用集成测试方式验证行为对齐。

// TestSQLiteInteg_LaravelCmp_SelectReplacesColumns 第一章 testBasicSelectWithGetColumns：
// Select 为替换语义、无状态残留——后续 Select 覆盖前列，Select("*") 恢复全列。
func TestSQLiteInteg_LaravelCmp_SelectReplacesColumns(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_LaravelCmp_MixedAndOrLeadingBoolean 第三章 testUppercaseLeadingBooleansAreRemoved 等：
// 编译层首个条件不输出前置 and，混合 AND/OR 连接执行结果正确。
func TestSQLiteInteg_LaravelCmp_MixedAndOrLeadingBoolean(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_ExpressionValueNotBound 第四章 testDateBasedWheresExpressionIsNotBound：
// Where/WhereRaw 的 Expression 值直接内联，不产生绑定参数。
func TestSQLiteInteg_LaravelCmp_ExpressionValueNotBound(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_WhereInEmptyScalar 第九章 testBasicWhereInsException：
// zcdb WhereIn 入参为 []any 强类型，传标量在编译期即被拒绝（无法构造运行时异常）；
// 空切片语义：IN 空集等价 0=1 返回空，NOT IN 空集等价 1=1 返回全量。
func TestSQLiteInteg_LaravelCmp_WhereInEmptyScalar(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_MultipleUnions 第十三章 testMultipleUnions/testMultipleUnionAlls：
// 三个子查询连续追加 UNION / UNION ALL。
func TestSQLiteInteg_LaravelCmp_MultipleUnions(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	// UNION 去重：active(3) ∪ age>30(1) ∪ age<26(1) = 4 行
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	q3 := db.Builder().Table("users").Select("name").Where("age", "<", 26)
	var rows []row
	err := q1.Union(q2).Union(q3).OrderBy("name", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}

	// UNION ALL 保留重复：3+1+1 = 5 行
	q4 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q5 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	q6 := db.Builder().Table("users").Select("name").Where("age", "<", 26)
	var rows2 []row
	err = q4.UnionAll(q5).UnionAll(q6).Find(context.Background(), &rows2)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows2) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows2))
	}
}

// TestSQLiteInteg_LaravelCmp_UnionWithJoin 第十三章 testUnionWithJoin：
// union 分支子查询中带 JOIN。
func TestSQLiteInteg_LaravelCmp_UnionWithJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	// 分支一：active 用户；分支二：在 orders 有订单的用户（join 去重）
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").
		Join("orders", "orders.user_id", "=", "users.id").Distinct()

	var rows []row
	err := q1.Union(q2).OrderBy("name", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_LaravelCmp_UnionLimitOffset 第十三章 testUnionLimitsAndOffsets：
// union 结果整体 limit/offset。
func TestSQLiteInteg_LaravelCmp_UnionLimitOffset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	// 整体排序后取第 2、3 条：[alice, bob, charlie, diana] → [bob, charlie]
	var rows []row
	err := q1.Union(q2).OrderBy("name", "ASC").Limit(2).Offset(1).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", rows)
	}
}

// TestSQLiteInteg_LaravelCmp_UnionOrderByRaw 第十五章 testOrderByRawUnion：
// union 后 OrderByRaw 执行正常（多分支 where 绑定与排序表达式绑定顺序正确）。
func TestSQLiteInteg_LaravelCmp_UnionOrderByRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	var rows []row
	err := q1.Union(q2).OrderByRaw("name ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 || rows[0].Name != "alice" || rows[3].Name != "diana" {
		t.Errorf("expected sorted [alice bob charlie diana], got %v", rows)
	}
}

// TestSQLiteInteg_LaravelCmp_HavingThenFirst 第十六章 testHavingFollowedBySelectGet：
// 分组聚合后 First 取数，having 绑定正确传递。
func TestSQLiteInteg_LaravelCmp_HavingThenFirst(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type groupRow struct {
		Status string `db:"status"`
	}
	var g groupRow
	// 两组均满足 COUNT(*)>1，按 status 排序取第一组（active）
	err := db.Builder().Table("users").
		Select("status").
		GroupBy("status").
		HavingRaw("COUNT(*) > ?", 1).
		OrderBy("status", "ASC").
		First(context.Background(), &g)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if g.Status != "active" {
		t.Errorf("expected active group, got %q", g.Status)
	}
}

// TestSQLiteInteg_LaravelCmp_UnionCountWithOrdersAndPaging 第十七章 testGetCountForPaginationWithUnionOrders/...WithUnionLimitAndOffset：
// 带排序/分页的 union 计数：总数不受 order/limit/offset 影响。
func TestSQLiteInteg_LaravelCmp_UnionCountWithOrdersAndPaging(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Where("age", ">", 30)
	count, err := q1.Union(q2).OrderBy("name", "ASC").Limit(2).Offset(1).Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4, got %d", count)
	}
}

// TestSQLiteInteg_LaravelCmp_SubSelectResetBindings 第十九章 testSubSelectResetBindings：
// 子查询中 Select("*") 为替换语义，无列/绑定残留。
func TestSQLiteInteg_LaravelCmp_SubSelectResetBindings(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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

// TestSQLiteInteg_LaravelCmp_WhereSubInvalidOperator 第十九章 testSubSelect（非法参数）：
// 子查询运算符不在白名单内时返回 ErrInvalidOperator。
func TestSQLiteInteg_LaravelCmp_WhereSubInvalidOperator(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_AggregateResetColumns 第二十二章 testAggregateResetFollowedByGet 等：
// 聚合后 columns 状态恢复，可继续取数。
func TestSQLiteInteg_LaravelCmp_AggregateResetColumns(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	b := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	// 先聚合：COUNT 后 columns 恢复
	count, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
	// 聚合后再次取数：列与绑定状态未被破坏
	var rows []row
	err = b.OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "alice" || rows[2].Name != "diana" {
		t.Errorf("expected [alice bob diana], got %v", rows)
	}
}

// TestSQLiteInteg_LaravelCmp_AggregateIgnoreSelectSub 第二十二章 testAggregateWithSubSelect：
// 聚合忽略子查询列及其绑定（COUNT(*) 不受 SELECT 子查询影响）。
func TestSQLiteInteg_LaravelCmp_AggregateIgnoreSelectSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").
		SelectSubquery(db.Builder().Table("users").Select("name").Where("id", "=", 1), "sub_name").
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

// TestSQLiteInteg_LaravelCmp_InsertUsingInvalidSubquery 第二十三章 testInsertUsingInvalidSubquery：
// InsertUsing 子查询缺少数据源或带非法运算符时直接返回错误，不生成非法 SQL。
func TestSQLiteInteg_LaravelCmp_InsertUsingInvalidSubquery(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_InsertOrIgnoreConflictZero 第二十三章 testInsertOrIgnoreReturningDoesNotMarkRecordsModifiedWhenNoRowsWereInserted：
// 冲突未插入任何行时受影响行数为 0。
func TestSQLiteInteg_LaravelCmp_InsertOrIgnoreConflictZero(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_InsertGetIdExpression 第二十三章 testInsertGetIdMethodRemovesExpressions：
// InsertGetId 的 Expression 值内联进 SQL，不产生绑定参数。
func TestSQLiteInteg_LaravelCmp_InsertGetIdExpression(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_InsertGetIdEmptyData 第二十三章 testInsertGetIdWithEmptyValues：
// 空结构体/空切片插入被拒绝（zcdb 不支持 Laravel 的 default values 空插入）。
func TestSQLiteInteg_LaravelCmp_InsertGetIdEmptyData(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_UpsertEmptyUniqueBy 第二十五章 testUpsertMethodWithEmptyUniqueByArray/...String：
// SQLite 需要 uniqueBy 生成 ON CONFLICT 目标，空值直接拒绝。
func TestSQLiteInteg_LaravelCmp_UpsertEmptyUniqueBy(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_TruncateResetSequence 第二十六章 testTruncateMethod（SQLite 清序列部分）：
// SQLite truncate 清空数据并重置 AUTOINCREMENT 序列，自增主键从头开始。
func TestSQLiteInteg_LaravelCmp_TruncateResetSequence(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_CursorByZeroSize 第三十二章 testChunkWithCountZero：
// chunkSize 为 0 时直接返回，不执行任何查询。
func TestSQLiteInteg_LaravelCmp_CursorByZeroSize(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	n := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 0, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		n++
	}
	if n != 0 {
		t.Errorf("expected no iterations for chunkSize=0, got %d", n)
	}
}

// TestSQLiteInteg_LaravelCmp_CursorByQualifiedColumn 第三十二章 testChunkPaginatesUsingIdWithAlias：
// CursorBy 键列支持 table.column 限定形式。
func TestSQLiteInteg_LaravelCmp_CursorByQualifiedColumn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "users.id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, user.Name)
	}
	expected := []string{"alice", "bob", "charlie", "diana", "eve"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}
}

// TestSQLiteInteg_LaravelCmp_CursorByDesc 第三十二章 testChunkPaginatesUsingIdDesc：
// CursorByDesc 按游标列倒序分块（对齐 Laravel chunkByIdDesc）。
func TestSQLiteInteg_LaravelCmp_CursorByDesc(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	for err := range db.Builder().Table("users").Select("id", "name").CursorByDesc(context.Background(), &user, 2, "id") {
		if err != nil {
			t.Fatalf("CursorByDesc error: %v", err)
		}
		names = append(names, user.Name)
	}
	expected := []string{"eve", "diana", "charlie", "bob", "alice"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}
}

// TestSQLiteInteg_LaravelCmp_PluckDuplicateKeyOverwrite 集成附录 testPluck（重复 key 覆盖部分）：
// Pluck map 模式重复键时后值覆盖前值；keyBy 模式重复键列时最后一行覆盖。
func TestSQLiteInteg_LaravelCmp_PluckDuplicateKeyOverwrite(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// map 值→键 模式：第一列为值、第二列为键；status 键重复时后者（id 更大）覆盖前者
	var m map[string]int64
	err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &m, "id", "status")
	if err != nil {
		t.Fatalf("pluck error: %v", err)
	}
	if m["active"] != 4 || m["inactive"] != 5 {
		t.Errorf("expected active=4 inactive=5 (last wins), got %v", m)
	}

	// keyBy 模式：插入两条同名记录，后者（id 更大）覆盖前者
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES
		('dup', 1, 'dup1@test.com', 'x'),
		('dup', 2, 'dup2@test.com', 'y')`)
	type userBrief struct {
		Id  int
		Age int
	}
	var dup map[string]userBrief
	err = db.Builder().Table("users").
		Where("name", "=", "dup").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &dup, "name")
	if err != nil {
		t.Fatalf("pluck keyBy dup error: %v", err)
	}
	if dup["dup"].Id != 7 || dup["dup"].Age != 2 {
		t.Errorf("expected last dup row (id=7, age=2), got %+v", dup["dup"])
	}
}

// ==================== LaravelCmp: 现有 API 组合 ====================

// TestSQLiteInteg_LaravelCmp_DateWhere 验证日期 where 用 WhereRaw 手工构造
// （strftime 系列，对齐 Laravel SQLite 方言的 date/day/month/year/time）。
func TestSQLiteInteg_LaravelCmp_DateWhere(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_JsonSelectWhereOrder 验证 JSON 提取在 select/where/orderBy
// 中通过 SelectRaw/WhereRaw/OrderByRaw 组合（json_extract，含路径引号转义）。
func TestSQLiteInteg_LaravelCmp_JsonSelectWhereOrder(t *testing.T) {
	db := openSQLiteTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		json_val TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES
		('{"name":"alice","age":25,"address":{"city":"Shanghai"}}'),
		('{"name":"bob","age":30,"address":{"city":"Beijing"}}'),
		('{"name":"charlie","age":35,"address":{"city":"Shenzhen"}}'),
		('{"first name":"zoe","age":40}')`)

	// select：json_extract(...) AS name
	var names []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("json_conv_test").
		SelectRaw("json_extract(json_val, '$.name') AS name").
		OrderBy("id", "ASC").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("Select Find error: %v", err)
	}
	if len(names) != 4 || names[0].Name != "alice" || names[2].Name != "charlie" || names[3].Name != "" {
		t.Errorf("select: expected [alice bob charlie <nil>], got %+v", names)
	}

	// where + orderBy：age > 28（zoe40、charlie35、bob30）且按 age 降序；zoe 无 name 键 → NULL
	var rows []struct {
		Name string `db:"name"`
	}
	err = db.Builder().Table("json_conv_test").
		SelectRaw("json_extract(json_val, '$.name') AS name").
		WhereRaw("json_extract(json_val, '$.age') > ?", 28).
		OrderByRaw("json_extract(json_val, '$.age') DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("where/orderBy Find error: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "" || rows[1].Name != "charlie" || rows[2].Name != "bob" {
		t.Errorf("where/orderBy: expected [<nil> charlie bob], got %+v", rows)
	}

	// 路径转义：键含空格的 $.\"first name\"
	count, err := db.Builder().Table("json_conv_test").
		WhereRaw("json_extract(json_val, '$.\"first name\"') = ?", "zoe").
		Count(context.Background())
	if err != nil {
		t.Fatalf("path escaping Count error: %v", err)
	}
	if count != 1 {
		t.Errorf("path escaping: expected 1, got %d", count)
	}
}

// TestSQLiteInteg_LaravelCmp_JsonUpdate 验证 JSON 更新用 Update 值传 Expression
// （json_patch 合并，对齐 Laravel SQLite 方言），覆盖基本/嵌套/数组替换。
func TestSQLiteInteg_LaravelCmp_JsonUpdate(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_JsonContains 验证 JSON 包含查询用 WhereRaw 构造
// （json_each 子查询，对齐 Laravel SQLite 方言的 contains / doesntContain）。
func TestSQLiteInteg_LaravelCmp_JsonContains(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_JsonKeyLength 验证 JSON 键存在与长度查询
// （json_type is not null / json_array_length，对齐 Laravel SQLite 方言）。
func TestSQLiteInteg_LaravelCmp_JsonKeyLength(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_Bitwise 验证位运算条件用 WhereRaw/Expression 组合
// （(flags & ?) = ?，对齐 Laravel testBitwiseOperators 通用 & 形式）。
func TestSQLiteInteg_LaravelCmp_Bitwise(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_RowValues 验证行值比较用 WhereRaw 构造
// （(a, b) >= (?, ?)，对齐 Laravel testWhereRowValues）。
func TestSQLiteInteg_LaravelCmp_RowValues(t *testing.T) {
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

// TestSQLiteInteg_LaravelCmp_InOrderOf 验证按给定顺序排序用 OrderByRaw 构造
// （CASE WHEN ... THEN n END，对齐 Laravel testInOrderOf），含单值与 where 组合。
func TestSQLiteInteg_LaravelCmp_InOrderOf(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 基本：active 优先，同组按 id
	var names []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("users").
		Select("name").
		OrderByRaw("CASE WHEN status = 'active' THEN 0 WHEN status = 'inactive' THEN 1 ELSE 2 END, id").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("basic Find error: %v", err)
	}
	expected := []string{"alice", "bob", "diana", "charlie", "eve"}
	if len(names) != len(expected) {
		t.Fatalf("basic: expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("basic[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}

	// 单值：仅一个特例优先
	names = nil
	err = db.Builder().Table("users").
		Select("name").
		OrderByRaw("CASE WHEN status = 'inactive' THEN 0 ELSE 1 END, id").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("single value Find error: %v", err)
	}
	expected = []string{"charlie", "eve", "alice", "bob", "diana"}
	if len(names) != len(expected) {
		t.Fatalf("single value: expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("single value[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}

	// 与 where 组合：age > 26 且 charlie 优先
	names = nil
	err = db.Builder().Table("users").
		Select("name").
		Where("age", ">", 26).
		OrderByRaw("CASE WHEN name = 'charlie' THEN 0 ELSE 1 END, id").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("with where Find error: %v", err)
	}
	expected = []string{"charlie", "bob", "diana"}
	if len(names) != len(expected) {
		t.Fatalf("with where: expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("with where[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}
}

// TestSQLiteInteg_LaravelCmp_OrderBySubQuery 验证排序列用子查询构造
// （OrderByRaw 内联子查询，对齐 Laravel testOrderBySubQueries）。
func TestSQLiteInteg_LaravelCmp_OrderBySubQuery(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// 按订单数降序（alice/bob 各 2 单，charlie/diana 各 1 单，eve 0 单），同数按 id
	var names []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("users").
		Select("name").
		OrderByRaw("(SELECT COUNT(*) FROM orders WHERE orders.user_id = users.id) DESC, id ASC").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	expected := []string{"alice", "bob", "charlie", "diana", "eve"}
	if len(names) != len(expected) {
		t.Fatalf("expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}
}

// TestSQLiteInteg_LaravelCmp_ArrayWhereColumn 验证多列条件括号分组用 WhereRaw 构造
// （(a >= ? AND b <= ?)，对齐 Laravel testArrayWhereColumn 的括号语义）。
func TestSQLiteInteg_LaravelCmp_ArrayWhereColumn(t *testing.T) {
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
