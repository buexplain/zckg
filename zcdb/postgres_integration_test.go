package zcdb

import (
	"database/sql"
	"testing"

	_ "github.com/lib/pq"
)

// ==================== PostgreSQL 基础设施 ====================

// openPgTestDB 打开 PostgreSQL 连接，自动创建测试数据库（若不存在），然后清理并重建 users/orders 相关表，保证测试隔离。
// docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
func openPgTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping postgres: %v", err)
	}

	// 创建测试数据库（若不存在）
	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = 'zckg_test_integ')").Scan(&exists)
	if err != nil {
		t.Fatalf("failed to check database existence: %v", err)
	}
	if !exists {
		_, err = db.Exec("CREATE DATABASE zckg_test_integ")
		if err != nil {
			t.Fatalf("failed to create database: %v", err)
		}
	}
	_ = db.Close()

	// 重新连接到测试数据库
	dsn = "host=127.0.0.1 port=5432 user=postgres password=root dbname=zckg_test_integ sslmode=disable"
	db, err = sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to open test database: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// 清理旧表
	dropPgTables(t, db)
	return db
}

// dropPgTables 清除所有测试用表
func dropPgTables(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := []string{"users_archive", "orders", "users"}
	for _, table := range tables {
		_, _ = db.Exec("DROP TABLE IF EXISTS " + table + " CASCADE")
	}
}

// setupPgUsersTable 创建 PostgreSQL 版 users 表并预填 5 条数据。
func setupPgUsersTable(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE users (
		id     BIGSERIAL PRIMARY KEY,
		name   VARCHAR(64) NOT NULL,
		age    INT NULL,
		email  VARCHAR(128) NOT NULL UNIQUE,
		status VARCHAR(16) NOT NULL DEFAULT 'active'
	)`)
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES
		('alice', 25, 'alice@test.com', 'active'),
		('bob', 30, 'bob@test.com', 'active'),
		('charlie', 35, 'charlie@test.com', 'inactive'),
		('diana', 28, 'diana@test.com', 'active'),
		('eve', NULL, 'eve@test.com', 'inactive')`)
}

// setupPgOrdersTable 创建 PostgreSQL 版 orders 表并预填数据。
func setupPgOrdersTable(t *testing.T, db *sql.DB) {
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

// newPgBuilder 创建使用 PostgresGrammar 的 Builder。
func newPgBuilder() *Builder {
	return NewBuilder(NewPostgresGrammar())
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToInsert(insertData{Name: "frank", Age: 40, Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("ToInsert error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE name = 'frank'")
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToInsert(data)
	if err != nil {
		t.Fatalf("ToInsert error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE name IN ('frank', 'grace')")
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToInsert(insertPtrData{Name: &name, Email: &email})
	if err != nil {
		t.Fatalf("ToInsert error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	var age sql.NullInt64
	if err := db.QueryRow("SELECT age FROM users WHERE name = 'frank'").Scan(&age); err != nil {
		t.Fatalf("query error: %v", err)
	}
	if age.Valid {
		t.Errorf("expected NULL age, got %d", age.Int64)
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToInsertOrIgnore(insertData{Name: "alice_dup", Age: 99, Email: "alice@test.com"})
	if err != nil {
		t.Fatalf("ToInsertOrIgnore error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE name = 'alice_dup'")
	if count != 0 {
		t.Errorf("expected 0 rows for alice_dup (ignored), got %d", count)
	}
	total := queryCount(t, db, "SELECT COUNT(*) FROM users")
	if total != 5 {
		t.Errorf("expected 5 total users, got %d", total)
	}
}

// ==================== Group 2: SELECT 基础查询 ====================

// TestPgInteg_SelectAll 验证无条件全表查询：不设任何 WHERE，SELECT * 应返回所有行。
func TestPgInteg_SelectAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		count++
	}
	if count != 5 {
		t.Errorf("expected 5 rows, got %d", count)
	}
}

// TestPgInteg_SelectColumns 验证指定列查询：仅选择 name 和 age 列，通过 WHERE 定位单行。
func TestPgInteg_SelectColumns(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name", "age").
		Where("id", "=", 1).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	var name string
	var age int
	if err := db.QueryRow(sqlStr, args...).Scan(&name, &age); err != nil {
		t.Fatalf("scan error: %v", err)
	}
	if name != "alice" || age != 25 {
		t.Errorf("expected alice/25, got %s/%d", name, age)
	}
}

// TestPgInteg_SelectDistinct 验证 DISTINCT 去重查询。
func TestPgInteg_SelectDistinct(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("status").
		Distinct().
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 {
		t.Errorf("expected 2 distinct statuses, got %d: %v", len(results), results)
	}
}

// TestPgInteg_SelectWhereBasic 验证基础 WHERE 等值条件。
func TestPgInteg_SelectWhereBasic(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		Where("age", "=", 25).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 1 || results[0] != "alice" {
		t.Errorf("expected [alice], got %v", results)
	}
}

// TestPgInteg_SelectWhereOr 验证 OR 条件组合。
func TestPgInteg_SelectWhereOr(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		Where("age", "=", 25).
		OrWhere("age", "=", 30).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 || results[0] != "alice" || results[1] != "bob" {
		t.Errorf("expected [alice, bob], got %v", results)
	}
}

// TestPgInteg_SelectWhereIn 验证 WHERE IN 条件。
func TestPgInteg_SelectWhereIn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereIn("id", []any{1, 3, 5}).
		OrderBy("id", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(results), results)
	}
}

// TestPgInteg_SelectWhereNotIn 验证 WHERE NOT IN 条件。
func TestPgInteg_SelectWhereNotIn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereNotIn("id", []any{1, 2}).
		OrderBy("id", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(results), results)
	}
}

// ==================== Group 3: SELECT 高级 WHERE ====================

// TestPgInteg_SelectWhereNull 验证 WHERE IS NULL 条件。
func TestPgInteg_SelectWhereNull(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereNull("age").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 1 || results[0] != "eve" {
		t.Errorf("expected [eve], got %v", results)
	}
}

// TestPgInteg_SelectWhereNotNull 验证 WHERE IS NOT NULL 条件。
func TestPgInteg_SelectWhereNotNull(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereNotNull("age").
		OrderBy("id", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 4 {
		t.Errorf("expected 4 rows, got %d", len(results))
	}
}

// TestPgInteg_SelectWhereBetween 验证 WHERE BETWEEN 范围条件。
func TestPgInteg_SelectWhereBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereBetween("age", 25, 30).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(results), results)
	}
}

// TestPgInteg_SelectWhereNotBetween 验证 WHERE NOT BETWEEN 条件。
func TestPgInteg_SelectWhereNotBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereNotBetween("age", 25, 30).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 1 || results[0] != "charlie" {
		t.Errorf("expected [charlie], got %v", results)
	}
}

// TestPgInteg_SelectWhereNested 验证嵌套 WHERE 条件组（括号分组）。
func TestPgInteg_SelectWhereNested(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereNested(func(b *Builder) {
			b.Where("age", ">", 25).Where("status", "=", "active")
		}).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 {
		t.Errorf("expected 2 rows, got %d: %v", len(results), results)
	}
	if results[0] != "diana" || results[1] != "bob" {
		t.Errorf("expected [diana, bob], got %v", results)
	}
}

// TestPgInteg_SelectWhereRaw 验证原始 WHERE 表达式（WhereRaw）。
func TestPgInteg_SelectWhereRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereRaw("age > $1 AND name LIKE $2", 28, "b%").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 1 || results[0] != "bob" {
		t.Errorf("expected [bob], got %v", results)
	}
}

// ==================== Group 4: JOIN ====================

// TestPgInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行。
func TestPgInteg_InnerJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("users.name").
		Join("orders", "users.id", "=", "orders.user_id").
		Distinct().
		OrderBy("users.name", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(results), results)
	}
}

// TestPgInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留。
func TestPgInteg_LeftJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("users.name").
		LeftJoin("orders", "users.id", "=", "orders.user_id").
		Distinct().
		OrderBy("users.name", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 5 {
		t.Errorf("expected 5 users with left join, got %d: %v", len(results), results)
	}
}

// TestPgInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积。
func TestPgInteg_CrossJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		SelectRaw("COUNT(*) as cnt").
		CrossJoin("orders").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	count := queryCount(t, db, sqlStr, args...)
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestPgInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：ON 子句附加额外过滤。
func TestPgInteg_JoinOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Where("orders.amount", ">", 100)
		}).
		OrderBy("users.name", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	defer func() { _ = rows.Close() }()

	count := 0
	for rows.Next() {
		count++
	}
	if count != 3 {
		t.Errorf("expected 3 rows with amount > 100, got %d", count)
	}
}

// ==================== Group 5: 聚合/分组/排序 ====================

// TestPgInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤。
func TestPgInteg_GroupByHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("orders").
		Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > $1", 100).
		OrderBy("user_id", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryInts(t, db, sqlStr, args...)
	if len(results) != 3 {
		t.Errorf("expected 3 groups with total > 100, got %d: %v", len(results), results)
	}
}

// TestPgInteg_OrderByLimitOffset 验证排序+分页：ORDER BY + LIMIT + OFFSET。
func TestPgInteg_OrderByLimitOffset(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereNotNull("age").
		OrderBy("age", "DESC").
		Limit(2).
		Offset(1).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 || results[0] != "bob" || results[1] != "diana" {
		t.Errorf("expected [bob, diana], got %v", results)
	}
}

// TestPgInteg_ForPage 验证 ForPage 便捷分页。
func TestPgInteg_ForPage(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		OrderBy("id", "ASC").
		ForPage(2, 2).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 || results[0] != "charlie" || results[1] != "diana" {
		t.Errorf("expected [charlie, diana], got %v", results)
	}
}

// TestPgInteg_InRandomOrder 验证随机排序：ORDER BY RANDOM()。
func TestPgInteg_InRandomOrder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		InRandomOrder().
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 5 {
		t.Errorf("expected 5 rows, got %d", len(results))
	}
}

// TestPgInteg_SelectRaw 验证 SelectRaw 原始表达式（COUNT(*)）。
func TestPgInteg_SelectRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		SelectRaw("COUNT(*) as cnt").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	count := queryCount(t, db, sqlStr, args...)
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

// ==================== Group 6: 子查询 ====================

// TestPgInteg_WhereSub 验证 WHERE 子查询比较：age > (SELECT AVG(age) ...)。
func TestPgInteg_WhereSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereSub("age", ">", func(sub *Builder) {
			sub.Table("users").SelectRaw("AVG(age)").WhereNotNull("age")
		}).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 || results[0] != "bob" || results[1] != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", results)
	}
}

// TestPgInteg_WhereInSub 验证 WHERE IN 子查询。
func TestPgInteg_WhereInSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").Select("user_id").Where("amount", ">", 100)
		}).
		OrderBy("name", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 3 {
		t.Errorf("expected 3 users, got %d: %v", len(results), results)
	}
}

// TestPgInteg_WhereExists 验证 WHERE EXISTS 子查询。
func TestPgInteg_WhereExists(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				Select("orders.user_id").
				WhereColumn("orders.user_id", "=", "users.id")
		}).
		OrderBy("name", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(results), results)
	}
}

// TestPgInteg_FromSub 验证 FROM 子查询（派生表）。
func TestPgInteg_FromSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sub := newPgBuilder().Table("users").Select("name", "age").WhereNotNull("age")
	sqlStr, args, err := newPgBuilder().
		FromSub(sub, "sub").
		Select("name").
		Where("age", ">", 28).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 2 || results[0] != "bob" || results[1] != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", results)
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updateData{Name: "alice_updated", Age: 26})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	names := queryStrings(t, db, "SELECT name FROM users WHERE id = 1")
	if len(names) != 1 || names[0] != "alice_updated" {
		t.Errorf("expected alice_updated, got %v", names)
	}
	ages := queryInts(t, db, "SELECT age FROM users WHERE id = 1")
	if len(ages) != 1 || ages[0] != 26 {
		t.Errorf("expected age=26, got %v", ages)
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updatePtrData{Name: &newName})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	names := queryStrings(t, db, "SELECT name FROM users WHERE id = 1")
	if names[0] != "alice_ptr" {
		t.Errorf("expected alice_ptr, got %s", names[0])
	}
	ages := queryInts(t, db, "SELECT age FROM users WHERE id = 1")
	if ages[0] != 25 {
		t.Errorf("expected age still 25, got %d", ages[0])
	}
	statuses := queryStrings(t, db, "SELECT status FROM users WHERE id = 1")
	if statuses[0] != "active" {
		t.Errorf("expected status still active, got %s", statuses[0])
	}
}

// TestPgInteg_UpdateWithRaw 验证 Raw 表达式更新：字段值为 Raw("age" + 10)。
func TestPgInteg_UpdateWithRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type updateRaw struct {
		Age any `db:"age"`
	}
	sqlStr, args, err := newPgBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updateRaw{Age: Raw("\"age\" + 10")})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	ages := queryInts(t, db, "SELECT age FROM users WHERE id = 1")
	if len(ages) != 1 || ages[0] != 35 {
		t.Errorf("expected age=35, got %v", ages)
	}
}

// ==================== Group 8: DELETE ====================

// TestPgInteg_DeleteWithWhere 验证带条件删除。
func TestPgInteg_DeleteWithWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Where("id", "=", 1).
		ToDelete()
	if err != nil {
		t.Fatalf("ToDelete error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users")
	if count != 4 {
		t.Errorf("expected 4 remaining users, got %d", count)
	}
}

// TestPgInteg_DeleteAll 验证无条件全表删除。
func TestPgInteg_DeleteAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToDelete()
	if err != nil {
		t.Fatalf("ToDelete error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users")
	if count != 0 {
		t.Errorf("expected 0 users after delete all, got %d", count)
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
	sqlStr, args, err := newPgBuilder().
		Table("users").
		ToUpsert(
			upsertData{Name: "frank", Age: 40, Email: "frank@test.com"},
			[]string{"email"},
			[]string{"name", "age"},
		)
	if err != nil {
		t.Fatalf("ToUpsert error: %v", err)
	}
	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE name = 'frank'")
	if count != 1 {
		t.Errorf("expected frank inserted, got count=%d", count)
	}

	// 冲突更新
	sqlStr, args, err = newPgBuilder().
		Table("users").
		ToUpsert(
			upsertData{Name: "alice_upserted", Age: 99, Email: "alice@test.com"},
			[]string{"email"},
			[]string{"name", "age"},
		)
	if err != nil {
		t.Fatalf("ToUpsert error: %v", err)
	}
	mustExec(t, db, sqlStr, args...)

	names := queryStrings(t, db, "SELECT name FROM users WHERE email = 'alice@test.com'")
	if len(names) != 1 || names[0] != "alice_upserted" {
		t.Errorf("expected alice_upserted, got %v", names)
	}
	ages := queryInts(t, db, "SELECT age FROM users WHERE email = 'alice@test.com'")
	if len(ages) != 1 || ages[0] != 99 {
		t.Errorf("expected age=99, got %v", ages)
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

	sqlStr, args, err := newPgBuilder().
		Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	if err != nil {
		t.Fatalf("ToInsertUsing error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users_archive")
	if count != 3 {
		t.Errorf("expected 3 archived users, got %d", count)
	}
}

// TestPgInteg_Union 验证 UNION 去重合并。
func TestPgInteg_Union(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := newPgBuilder().Table("users").Select("name").Where("status", "=", "active")
	q2 := newPgBuilder().Table("users").Select("name").Where("age", ">", 30)

	sqlStr, args, err := q1.Union(q2).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 4 {
		t.Errorf("expected 4 union results, got %d: %v", len(results), results)
	}
}

// TestPgInteg_UnionAll 验证 UNION ALL 不去重合并。
func TestPgInteg_UnionAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := newPgBuilder().Table("users").Select("name").Where("status", "=", "active")
	q2 := newPgBuilder().Table("users").Select("name").Where("age", ">", 25)

	sqlStr, args, err := q1.UnionAll(q2).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 6 {
		t.Errorf("expected 6 union all results, got %d: %v", len(results), results)
	}
}

// TestPgInteg_Truncate 验证 TRUNCATE TABLE 清空表。
func TestPgInteg_Truncate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, err := newPgBuilder().
		Table("users").
		ToTruncate()
	if err != nil {
		t.Fatalf("ToTruncate error: %v", err)
	}

	mustExec(t, db, sqlStr)

	count := queryCount(t, db, "SELECT COUNT(*) FROM users")
	if count != 0 {
		t.Errorf("expected 0 users after truncate, got %d", count)
	}
}

// ==================== Group 10: PostgreSQL 专属能力 ====================

// TestPgInteg_UpdateFromJoin 验证 UPDATE ... FROM（PostgreSQL 专属多表更新语法）。
func TestPgInteg_UpdateFromJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	// 将有订单金额 > 100 的用户状态改为 'vip'
	type updateData struct {
		Status string `db:"status"`
	}
	sqlStr, args, err := newPgBuilder().
		Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		ToUpdate(updateData{Status: "vip"})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// alice(Laptop=120), bob(TV=200), diana(Camera=150) → 3 users updated to 'vip'
	count := queryCount(t, db, "SELECT COUNT(DISTINCT id) FROM users WHERE status = 'vip'")
	if count != 3 {
		t.Errorf("expected 3 vip users, got %d", count)
	}
}

// TestPgInteg_LockForUpdate 验证 SELECT ... FOR UPDATE 语法可执行（事务内）。
func TestPgInteg_LockForUpdate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		Where("id", "=", 1).
		LockForUpdate().
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	var name string
	if err := tx.QueryRow(sqlStr, args...).Scan(&name); err != nil {
		t.Fatalf("query error: %v", err)
	}
	if name != "alice" {
		t.Errorf("expected alice, got %s", name)
	}
}

// TestPgInteg_SharedLock 验证 SELECT ... FOR SHARE 语法可执行（事务内）。
func TestPgInteg_SharedLock(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	sqlStr, args, err := newPgBuilder().
		Table("users").
		Select("name").
		Where("id", "=", 1).
		SharedLock().
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	var name string
	if err := tx.QueryRow(sqlStr, args...).Scan(&name); err != nil {
		t.Fatalf("query error: %v", err)
	}
	if name != "alice" {
		t.Errorf("expected alice, got %s", name)
	}
}
