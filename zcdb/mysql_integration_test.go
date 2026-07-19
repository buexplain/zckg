package zcdb

import (
	"database/sql"
	"testing"

	_ "github.com/go-sql-driver/mysql"
)

// ==================== MySQL 基础设施 ====================

// openMySQLTestDB 打开 MySQL 连接，自动创建测试数据库（若不存在），然后清理并重建 users/orders 相关表，保证测试隔离。
// docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
func openMySQLTestDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := "root:root@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=Local"
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("failed to open mysql: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("failed to ping mysql: %v", err)
	}
	// 创建测试数据库（若不存在）并切换
	_, err = db.Exec("CREATE DATABASE IF NOT EXISTS `zckg_test_integ` DEFAULT CHARACTER SET utf8mb4")
	if err != nil {
		t.Fatalf("failed to create database: %v", err)
	}
	_, err = db.Exec("USE `zckg_test_integ`")
	if err != nil {
		t.Fatalf("failed to use database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// 清理旧表
	dropMySQLTables(t, db)
	return db
}

// dropMySQLTables 清除所有测试用表
func dropMySQLTables(t *testing.T, db *sql.DB) {
	t.Helper()
	tables := []string{"users_archive", "orders", "users"}
	for _, table := range tables {
		_, _ = db.Exec("DROP TABLE IF EXISTS `" + table + "`")
	}
}

// setupMySQLUsersTable 创建 MySQL 版 users 表并预填 5 条数据。
func setupMySQLUsersTable(t *testing.T, db *sql.DB) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE users (
		id     BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name   VARCHAR(64) NOT NULL,
		age    INT NULL,
		email  VARCHAR(128) NOT NULL,
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
func setupMySQLOrdersTable(t *testing.T, db *sql.DB) {
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

// newMySQLBuilder 创建使用 MySQLGrammar 的 Builder。
func newMySQLBuilder() *Builder {
	return NewBuilder(NewMySQLGrammar())
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
	sqlStr, args, err := newMySQLBuilder().
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
	sqlStr, args, err := newMySQLBuilder().
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
	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_InsertOrIgnore 验证 INSERT IGNORE：当 UNIQUE 约束冲突时不报错且不插入新行。
func TestMySQLInteg_InsertOrIgnore(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectAll 验证无条件全表查询：不设任何 WHERE，SELECT * 应返回所有行。
func TestMySQLInteg_SelectAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectColumns 验证指定列查询：仅选择 name 和 age 列，通过 WHERE 定位单行。
func TestMySQLInteg_SelectColumns(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectDistinct 验证 DISTINCT 去重查询。
func TestMySQLInteg_SelectDistinct(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereBasic 验证基础 WHERE 等值条件。
func TestMySQLInteg_SelectWhereBasic(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereOr 验证 OR 条件组合。
func TestMySQLInteg_SelectWhereOr(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereIn 验证 WHERE IN 条件。
func TestMySQLInteg_SelectWhereIn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereNotIn 验证 WHERE NOT IN 条件。
func TestMySQLInteg_SelectWhereNotIn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereNull 验证 WHERE IS NULL 条件。
func TestMySQLInteg_SelectWhereNull(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereNotNull 验证 WHERE IS NOT NULL 条件。
func TestMySQLInteg_SelectWhereNotNull(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereBetween 验证 WHERE BETWEEN 范围条件。
func TestMySQLInteg_SelectWhereBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereNotBetween 验证 WHERE NOT BETWEEN 条件。
func TestMySQLInteg_SelectWhereNotBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereNested 验证嵌套 WHERE 条件组（括号分组）。
func TestMySQLInteg_SelectWhereNested(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectWhereRaw 验证原始 WHERE 表达式（WhereRaw）。
func TestMySQLInteg_SelectWhereRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
		Table("users").
		Select("name").
		WhereRaw("age > ? AND name LIKE ?", 28, "b%").
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

// TestMySQLInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行。
func TestMySQLInteg_InnerJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留。
func TestMySQLInteg_LeftJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积。
func TestMySQLInteg_CrossJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：ON 子句附加额外过滤。
func TestMySQLInteg_JoinOn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤。
func TestMySQLInteg_GroupByHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
		Table("orders").
		Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 100).
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

// TestMySQLInteg_OrderByLimitOffset 验证排序+分页：ORDER BY + LIMIT + OFFSET。
func TestMySQLInteg_OrderByLimitOffset(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_ForPage 验证 ForPage 便捷分页。
func TestMySQLInteg_ForPage(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_InRandomOrder 验证随机排序：ORDER BY RAND()。
func TestMySQLInteg_InRandomOrder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SelectRaw 验证 SelectRaw 原始表达式（COUNT(*)）。
func TestMySQLInteg_SelectRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_WhereSub 验证 WHERE 子查询比较：age > (SELECT AVG(age) ...)。
func TestMySQLInteg_WhereSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_WhereInSub 验证 WHERE IN 子查询。
func TestMySQLInteg_WhereInSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_WhereExists 验证 WHERE EXISTS 子查询。
func TestMySQLInteg_WhereExists(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_FromSub 验证 FROM 子查询（派生表）。
func TestMySQLInteg_FromSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sub := newMySQLBuilder().Table("users").Select("name", "age").WhereNotNull("age")
	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_UpdateBasic 验证基础 UPDATE。
func TestMySQLInteg_UpdateBasic(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updateData struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	sqlStr, args, err := newMySQLBuilder().
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
	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_UpdateWithRaw 验证 Raw 表达式更新：字段值为 Raw(`age` + 10)。
func TestMySQLInteg_UpdateWithRaw(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type updateRaw struct {
		Age any `db:"age"`
	}
	sqlStr, args, err := newMySQLBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updateRaw{Age: Raw("`age` + 10")})
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

// TestMySQLInteg_DeleteWithWhere 验证带条件删除。
func TestMySQLInteg_DeleteWithWhere(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_DeleteAll 验证无条件全表删除。
func TestMySQLInteg_DeleteAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, args, err := newMySQLBuilder().
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
	sqlStr, args, err := newMySQLBuilder().
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
	sqlStr, args, err = newMySQLBuilder().
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

// TestMySQLInteg_InsertUsing 验证 INSERT INTO ... SELECT 子查询插入。
func TestMySQLInteg_InsertUsing(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	mustExec(t, db, `CREATE TABLE users_archive (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64),
		age  INT
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_Union 验证 UNION 去重合并。
func TestMySQLInteg_Union(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	q1 := newMySQLBuilder().Table("users").Select("name").Where("status", "=", "active")
	q2 := newMySQLBuilder().Table("users").Select("name").Where("age", ">", 30)

	sqlStr, args, err := q1.Union(q2).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 4 {
		t.Errorf("expected 4 union results, got %d: %v", len(results), results)
	}
}

// TestMySQLInteg_UnionAll 验证 UNION ALL 不去重合并。
func TestMySQLInteg_UnionAll(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	q1 := newMySQLBuilder().Table("users").Select("name").Where("status", "=", "active")
	q2 := newMySQLBuilder().Table("users").Select("name").Where("age", ">", 25)

	sqlStr, args, err := q1.UnionAll(q2).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	if len(results) != 6 {
		t.Errorf("expected 6 union all results, got %d: %v", len(results), results)
	}
}

// TestMySQLInteg_Truncate 验证 TRUNCATE TABLE 清空表。
func TestMySQLInteg_Truncate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	sqlStr, err := newMySQLBuilder().
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

// ==================== Group 10: MySQL 专属能力 ====================

// TestMySQLInteg_UpdateJoin 验证 UPDATE ... JOIN：通过 JOIN 关联更新。
func TestMySQLInteg_UpdateJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	// 将有订单金额 > 100 的用户状态改为 'vip'
	type updateData struct {
		Status string `db:"status"`
	}
	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_UpdateOrderByLimit 验证 UPDATE ... ORDER BY ... LIMIT：仅更新前 N 行。
func TestMySQLInteg_UpdateOrderByLimit(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 只更新 age 最大的 2 个用户
	type updateData struct {
		Status string `db:"status"`
	}
	sqlStr, args, err := newMySQLBuilder().
		Table("users").
		WhereNotNull("age").
		OrderBy("age", "DESC").
		Limit(2).
		ToUpdate(updateData{Status: "top"})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// charlie(35), bob(30) → top
	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE status = 'top'")
	if count != 2 {
		t.Errorf("expected 2 top users, got %d", count)
	}
	names := queryStrings(t, db, "SELECT name FROM users WHERE status = 'top' ORDER BY age DESC")
	if len(names) != 2 || names[0] != "charlie" || names[1] != "bob" {
		t.Errorf("expected [charlie, bob], got %v", names)
	}
}

// TestMySQLInteg_LockForUpdate 验证 SELECT ... FOR UPDATE 语法可执行（事务内）。
func TestMySQLInteg_LockForUpdate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	sqlStr, args, err := newMySQLBuilder().
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

// TestMySQLInteg_SharedLock 验证 SELECT ... LOCK IN SHARE MODE 语法可执行（事务内）。
func TestMySQLInteg_SharedLock(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	tx, err := db.Begin()
	if err != nil {
		t.Fatalf("begin tx error: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	sqlStr, args, err := newMySQLBuilder().
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
