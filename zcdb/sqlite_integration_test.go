package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
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

// TestSQLiteInteg_FromSub 验证 FROM 子查询（派生表）：先通过子查询过滤，再在外层查询中继续筛选。
func TestSQLiteInteg_FromSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_DeleteAll 验证无条件全表删除：不设 WHERE 时 DELETE 清空整张表。
func TestSQLiteInteg_DeleteAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Bug_SelectSubFromSubBindingOrder 验证 SELECT 子查询与 FROM 子查询同时含绑定参数时，
// 收集顺序与 SQL 占位符顺序一致（SELECT 子查询在前，FROM 子查询在后）。
func TestSQLiteInteg_Bug_SelectSubFromSubBindingOrder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

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
