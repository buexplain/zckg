package zcdb

import (
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// ==================== 基础设施 ====================

// openSQLiteTestDB 打开一个内存 SQLite 数据库，测试结束后自动关闭。
func openSQLiteTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// mustExec 执行 SQL，失败则 Fatal。
func mustExec(t *testing.T, db *sql.DB, query string, args ...any) sql.Result {
	t.Helper()
	res, err := db.Exec(query, args...)
	if err != nil {
		t.Fatalf("exec failed: %s\nerror: %v", query, err)
	}
	return res
}

// queryCount 执行 SELECT COUNT(*) 返回计数。
func queryCount(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("queryCount failed: %s\nerror: %v", query, err)
	}
	return count
}

// queryStrings 执行查询返回某一列的所有字符串值。
func queryStrings(t *testing.T, db *sql.DB, query string, args ...any) []string {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("queryStrings failed: %s\nerror: %v", query, err)
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		results = append(results, s)
	}
	return results
}

// queryInts 执行查询返回某一列的所有 int 值。
func queryInts(t *testing.T, db *sql.DB, query string, args ...any) []int {
	t.Helper()
	rows, err := db.Query(query, args...)
	if err != nil {
		t.Fatalf("queryInts failed: %s\nerror: %v", query, err)
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)
	var results []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		results = append(results, n)
	}
	return results
}

// setupSQLiteUsersTable 创建 users 表并预填 5 条数据。
func setupSQLiteUsersTable(t *testing.T, db *sql.DB) {
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

// setupSQLiteOrdersTable 创建 orders 表并预填数据。
func setupSQLiteOrdersTable(t *testing.T, db *sql.DB) {
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

// newSQLiteBuilder 创建使用 SQLiteGrammar 的 Builder。
func newSQLiteBuilder() *Builder {
	return NewBuilder(NewSQLiteGrammar())
}

// ==================== Group 1: INSERT ====================

// TestSQLiteInteg_InsertSingle 验证单条结构体插入：传入单个结构体，生成并执行 INSERT，确认数据正确写入。
func TestSQLiteInteg_InsertSingle(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 构造 INSERT SQL
	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		ToInsert(insertData{Name: "frank", Age: 40, Email: "frank@test.com"})
	if err != nil {
		t.Fatalf("ToInsert error: %v", err)
	}

	// 执行
	mustExec(t, db, sqlStr, args...)

	// 验证
	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE name = 'frank'")
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
	sqlStr, args, err := newSQLiteBuilder().
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
	// Age 为 nil → 不参与 INSERT
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		ToInsert(insertPtrData{Name: &name, Email: &email})
	if err != nil {
		t.Fatalf("ToInsert error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// 验证 age 为 NULL
	var age sql.NullInt64
	if err := db.QueryRow("SELECT age FROM users WHERE name = 'frank'").Scan(&age); err != nil {
		t.Fatalf("query error: %v", err)
	}
	if age.Valid {
		t.Errorf("expected NULL age, got %d", age.Int64)
	}
}

// TestSQLiteInteg_InsertOrIgnore 验证冲突忽略插入：当 UNIQUE 约束冲突时不报错且不插入新行，原有数据不受影响。
func TestSQLiteInteg_InsertOrIgnore(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 尝试插入重复 email（email 有 UNIQUE 约束）
	type insertData struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		ToInsertOrIgnore(insertData{Name: "alice_dup", Age: 99, Email: "alice@test.com"})
	if err != nil {
		t.Fatalf("ToInsertOrIgnore error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// 验证：不应有 alice_dup
	count := queryCount(t, db, "SELECT COUNT(*) FROM users WHERE name = 'alice_dup'")
	if count != 0 {
		t.Errorf("expected 0 rows for alice_dup (ignored), got %d", count)
	}
	// 总数仍为 5
	total := queryCount(t, db, "SELECT COUNT(*) FROM users")
	if total != 5 {
		t.Errorf("expected 5 total users, got %d", total)
	}
}

// ==================== Group 2: SELECT 基础查询 ====================

// TestSQLiteInteg_SelectAll 验证无条件全表查询：不设任何 WHERE，SELECT * 应返回所有行。
func TestSQLiteInteg_SelectAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	rows, err := db.Query(sqlStr, args...)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	count := 0
	for rows.Next() {
		count++
	}
	if count != 5 {
		t.Errorf("expected 5 rows, got %d", count)
	}
}

// TestSQLiteInteg_SelectColumns 验证指定列查询：仅选择 name 和 age 列，并通过 WHERE 定位单行，确认返回值正确。
func TestSQLiteInteg_SelectColumns(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectDistinct 验证 DISTINCT 去重：对有重复值的列使用 Distinct()，确认结果已去重。
func TestSQLiteInteg_SelectDistinct(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereBasic 验证基础 WHERE 等值条件：WHERE age = 25 应精确匹配到对应行。
func TestSQLiteInteg_SelectWhereBasic(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereOr 验证 OR 条件组合：WHERE age=25 OR age=30 应返回满足任一条件的行。
func TestSQLiteInteg_SelectWhereOr(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereIn 验证 WHERE IN 条件：传入值列表，确认只返回 ID 在列表中的行。
func TestSQLiteInteg_SelectWhereIn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereNotIn 验证 WHERE NOT IN 条件：排除指定 ID，确认返回剩余行。
func TestSQLiteInteg_SelectWhereNotIn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereNull 验证 WHERE IS NULL 条件：筛选字段值为 NULL 的行。
func TestSQLiteInteg_SelectWhereNull(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereNotNull 验证 WHERE IS NOT NULL 条件：排除字段值为 NULL 的行。
func TestSQLiteInteg_SelectWhereNotNull(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereBetween 验证 WHERE BETWEEN 范围条件：筛选 age 在 [25, 30] 区间内的行。
func TestSQLiteInteg_SelectWhereBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Select("name").
		WhereBetween("age", 25, 30).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// alice(25), diana(28), bob(30)
	if len(results) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_SelectWhereNotBetween 验证 WHERE NOT BETWEEN 条件：排除 age 在 [25, 30] 区间内的行（NULL 值也被排除）。
func TestSQLiteInteg_SelectWhereNotBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Select("name").
		WhereNotBetween("age", 25, 30).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// charlie(35)，eve 的 age 为 NULL 不满足 NOT BETWEEN
	if len(results) != 1 || results[0] != "charlie" {
		t.Errorf("expected [charlie], got %v", results)
	}
}

// TestSQLiteInteg_SelectWhereNested 验证嵌套 WHERE 条件组：使用 WhereNested 生成 (age > 25 AND status = 'active') 括号分组。
func TestSQLiteInteg_SelectWhereNested(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// (age > 25 AND status = 'active') → bob(30), diana(28)
	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_SelectWhereRaw 验证原始 WHERE 表达式：通过 WhereRaw 传入手写 SQL 片段及绑定参数。
func TestSQLiteInteg_SelectWhereRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行，无订单的用户不出现。
func TestSQLiteInteg_InnerJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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
	// alice(2 orders), bob(2 orders), charlie(1 order), diana(1 order) → 4 distinct users
	if len(results) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留，无匹配的右表列为 NULL。
func TestSQLiteInteg_LeftJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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
	// LEFT JOIN → all 5 users (eve has no orders but still appears)
	if len(results) != 5 {
		t.Errorf("expected 5 users with left join, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积：结果行数 = 左表行数 × 右表行数。
func TestSQLiteInteg_CrossJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		SelectRaw("COUNT(*) as cnt").
		CrossJoin("orders").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	count := queryCount(t, db, sqlStr, args...)
	// 5 users × 6 orders = 30
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestSQLiteInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：在 ON 子句中附加额外过滤条件（amount > 100）。
func TestSQLiteInteg_JoinOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// JOIN orders ON users.id = orders.user_id AND orders.amount > 100
	sqlStr, args, err := newSQLiteBuilder().
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
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	count := 0
	for rows.Next() {
		count++
	}
	// alice(Laptop=120), bob(TV=200), diana(Camera=150) → 3 rows
	if count != 3 {
		t.Errorf("expected 3 rows with amount > 100, got %d", count)
	}
}

// ==================== Group 5: 聚合/分组/排序 ====================

// TestSQLiteInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤：按 user_id 分组后筛选 SUM(amount) > 100 的组。
func TestSQLiteInteg_GroupByHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// 每个用户的订单总额，HAVING SUM(amount) > 100
	sqlStr, args, err := newSQLiteBuilder().
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
	// alice: 50+120=170, bob: 80+200=280, diana: 150 → user_id 1, 2, 4
	if len(results) != 3 {
		t.Errorf("expected 3 groups with total > 100, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_OrderByLimitOffset 验证排序+分页：ORDER BY age DESC 后跳过 1 行取 2 行，确认结果正确。
func TestSQLiteInteg_OrderByLimitOffset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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
	// age DESC with NotNull: charlie(35), bob(30), diana(28), alice(25)
	// offset 1, limit 2 → bob, diana
	if len(results) != 2 || results[0] != "bob" || results[1] != "diana" {
		t.Errorf("expected [bob, diana], got %v", results)
	}
}

// TestSQLiteInteg_ForPage 验证 ForPage 便捷分页：page=2, perPage=2 应返回第 3~4 行。
func TestSQLiteInteg_ForPage(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// page=2, perPage=2 → offset 2, limit 2
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Select("name").
		OrderBy("id", "ASC").
		ForPage(2, 2).
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// id ASC: alice(1), bob(2), charlie(3), diana(4), eve(5)
	// page 2, perPage 2 → charlie, diana
	if len(results) != 2 || results[0] != "charlie" || results[1] != "diana" {
		t.Errorf("expected [charlie, diana], got %v", results)
	}
}

// TestSQLiteInteg_InRandomOrder 验证随机排序：InRandomOrder 生成 ORDER BY RANDOM()，返回行数不变。
func TestSQLiteInteg_InRandomOrder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Select("name").
		InRandomOrder().
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// 不能断言顺序，只断言行数
	if len(results) != 5 {
		t.Errorf("expected 5 rows, got %d", len(results))
	}
}

// TestSQLiteInteg_SelectRaw 验证 SelectRaw 原始表达式：使用 COUNT(*) 聚合函数，确认返回正确计数。
func TestSQLiteInteg_SelectRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_WhereSub 验证 WHERE 子查询比较：age > (SELECT AVG(age) ...)，筛选出大于平均值的行。
func TestSQLiteInteg_WhereSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// age > (SELECT AVG(age) FROM users WHERE age IS NOT NULL)
	// AVG(25,30,35,28) = 29.5 → charlie(35), bob(30)
	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_WhereInSub 验证 WHERE IN 子查询：id IN (SELECT user_id FROM orders ...)，通过子查询动态生成值列表。
func TestSQLiteInteg_WhereInSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// id IN (SELECT user_id FROM orders WHERE amount > 100)
	// orders with amount > 100: user_id 1(120), 2(200), 4(150) → alice, bob, diana
	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_WhereExists 验证 WHERE EXISTS 子查询：只保留在 orders 表中存在关联记录的用户。
func TestSQLiteInteg_WhereExists(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// 存在订单的用户
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Select("name").
		WhereExists(func(sub *Builder) {
			sub.Table("orders").
				SelectRaw("1").
				WhereColumn("orders.user_id", "=", "users.id")
		}).
		OrderBy("name", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// alice, bob, charlie, diana have orders; eve doesn't
	if len(results) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_FromSub 验证 FROM 子查询（派生表）：先通过子查询过滤，再在外层查询中继续筛选。
func TestSQLiteInteg_FromSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// FROM (SELECT name, age FROM users WHERE age IS NOT NULL) sub WHERE sub.age > 28
	sub := newSQLiteBuilder().Table("users").Select("name", "age").WhereNotNull("age")
	sqlStr, args, err := newSQLiteBuilder().
		FromSub(sub, "sub").
		Select("name").
		Where("age", ">", 28).
		OrderBy("age", "ASC").
		ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// bob(30), charlie(35)
	if len(results) != 2 || results[0] != "bob" || results[1] != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", results)
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
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updateData{Name: "alice_updated", Age: 26})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// 验证
	names := queryStrings(t, db, "SELECT name FROM users WHERE id = 1")
	if len(names) != 1 || names[0] != "alice_updated" {
		t.Errorf("expected alice_updated, got %v", names)
	}
	ages := queryInts(t, db, "SELECT age FROM users WHERE id = 1")
	if len(ages) != 1 || ages[0] != 26 {
		t.Errorf("expected age=26, got %v", ages)
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
	// Age 和 Status 为 nil → 不参与 UPDATE
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updatePtrData{Name: &newName})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// 验证 name 变了，age 和 status 没变
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

// TestSQLiteInteg_UpdateWithRaw 验证 Raw 表达式更新：字段值为 Raw("age" + 10) 时生成原始 SQL 而非占位符。
func TestSQLiteInteg_UpdateWithRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type updateRaw struct {
		Age any `db:"age"`
	}
	sqlStr, args, err := newSQLiteBuilder().
		Table("users").
		Where("id", "=", 1).
		ToUpdate(updateRaw{Age: Raw("\"age\" + 10")})
	if err != nil {
		t.Fatalf("ToUpdate error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// alice 原 age=25，加 10 应为 35
	ages := queryInts(t, db, "SELECT age FROM users WHERE id = 1")
	if len(ages) != 1 || ages[0] != 35 {
		t.Errorf("expected age=35, got %v", ages)
	}
}

// ==================== Group 8: DELETE ====================

// TestSQLiteInteg_DeleteWithWhere 验证带条件删除：WHERE id=1 只删除一行，其余行不受影响。
func TestSQLiteInteg_DeleteWithWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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

// TestSQLiteInteg_DeleteAll 验证无条件全表删除：不设 WHERE 时 DELETE 清空整张表。
func TestSQLiteInteg_DeleteAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, args, err := newSQLiteBuilder().
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
	sqlStr, args, err := newSQLiteBuilder().
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

	// 冲突更新（email 已存在）
	sqlStr, args, err = newSQLiteBuilder().
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

	// alice 的 name 和 age 应该被更新
	names := queryStrings(t, db, "SELECT name FROM users WHERE email = 'alice@test.com'")
	if len(names) != 1 || names[0] != "alice_upserted" {
		t.Errorf("expected alice_upserted, got %v", names)
	}
	ages := queryInts(t, db, "SELECT age FROM users WHERE email = 'alice@test.com'")
	if len(ages) != 1 || ages[0] != 99 {
		t.Errorf("expected age=99, got %v", ages)
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
	sqlStr, args, err := newSQLiteBuilder().
		Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})
	if err != nil {
		t.Fatalf("ToInsertUsing error: %v", err)
	}

	mustExec(t, db, sqlStr, args...)

	// active users: alice, bob, diana → 3 rows
	count := queryCount(t, db, "SELECT COUNT(*) FROM users_archive")
	if count != 3 {
		t.Errorf("expected 3 archived users, got %d", count)
	}
}

// TestSQLiteInteg_Union 验证 UNION 去重合并：两个查询的结果合并后自动去除重复行。
func TestSQLiteInteg_Union(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// UNION 去重
	q1 := newSQLiteBuilder().Table("users").Select("name").Where("status", "=", "active")
	q2 := newSQLiteBuilder().Table("users").Select("name").Where("age", ">", 30)

	sqlStr, args, err := q1.Union(q2).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// active: alice, bob, diana; age>30: charlie(35); UNION 去重 → 4 names
	if len(results) != 4 {
		t.Errorf("expected 4 union results, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_UnionAll 验证 UNION ALL 不去重合并：两个查询的结果直接拼接，保留重复行。
func TestSQLiteInteg_UnionAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := newSQLiteBuilder().Table("users").Select("name").Where("status", "=", "active")
	q2 := newSQLiteBuilder().Table("users").Select("name").Where("age", ">", 25)

	sqlStr, args, err := q1.UnionAll(q2).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect error: %v", err)
	}

	results := queryStrings(t, db, sqlStr, args...)
	// active: alice, bob, diana (3); age>25: bob, charlie, diana (3); UNION ALL → 6
	if len(results) != 6 {
		t.Errorf("expected 6 union all results, got %d: %v", len(results), results)
	}
}

// TestSQLiteInteg_Truncate 验证 TRUNCATE 清空表：执行后表中行数归零。
func TestSQLiteInteg_Truncate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	sqlStr, err := newSQLiteBuilder().
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
