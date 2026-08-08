// 本文件为 SQLite 集成测试的公共基础设施：连接建立、建表/清表 helper，
// 以及 Builder 基础能力（建连/事务/连接池/Clone/扫描等）测试。
package zcdb

import (
	"context"
	"fmt"
	"log"
	_ "modernc.org/sqlite"
	"sync"
	"testing"
	"time"
)

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

// TestSQLiteInteg_SchemaInspector_NonexistentTable 边界固化（审查结论）：
// 不存在的表查 Columns 返回空切片与 nil 错误（PRAGMA table_info 对未知表返回空结果集），
// 不报错也不 panic；调用方以空切片自行判断。
func TestSQLiteInteg_SchemaInspector_NonexistentTable(t *testing.T) {
	db := openSQLiteTestDB(t)
	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "no_such_table_zz")
	if err != nil {
		t.Fatalf("Columns() 对不存在的表应返回空结果, got error: %v", err)
	}
	if len(columns) != 0 {
		t.Errorf("expected empty columns, got %v", columns)
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

// TestSQLiteInteg_QueryRoutingLockToWrite 验证 Builder.query 的读写路由：
// 带锁子句的查询强制走写（主）连接，无锁查询路由读（从）连接。
// SQLite :memory: 每个连接库独立，表仅建在主库：
// 无锁查询打到从库应报 no such table，带锁查询打到主库应成功。
func TestSQLiteInteg_QueryRoutingLockToWrite(t *testing.T) {
	db := openSQLiteTestDB(t)
	if err := db.Pool().AddSlave(":memory:"); err != nil {
		t.Fatalf("AddSlave error: %v", err)
	}
	// 仅在主（写）库建表，从库无此表
	mustExec(t, db, `CREATE TABLE route_t (id INTEGER)`)
	mustExec(t, db, `INSERT INTO route_t VALUES (1)`)

	ctx := context.Background()

	// 无锁：路由读连接（从库无 route_t → 报错）
	rows, err := db.Builder().Table("route_t").query(ctx, `SELECT "id" FROM "route_t"`)
	if err == nil {
		_ = rows.Close()
		t.Errorf("无锁查询期望打到从库报 no such table，实际无错误")
	}

	// 带锁：强制路由写连接（直接置 lockClause 绕开方言锁子句编译限制，仅验证路由分支）
	b2 := db.Builder().Table("route_t")
	b2.lockClause = " FOR UPDATE"
	rows2, err2 := b2.query(ctx, `SELECT "id" FROM "route_t"`)
	if err2 != nil {
		t.Fatalf("带锁查询期望打到主库成功，实际报错: %v", err2)
	}
	defer func() { _ = rows2.Close() }()
	if !rows2.Next() {
		t.Errorf("带锁查询期望返回 1 行数据")
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

// setupSQLiteEventsTable 创建 events 表（日期过滤测试用）。
func setupSQLiteEventsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE events (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		happened_at TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO events (happened_at) VALUES
		('2024-06-15 10:00:00'),
		('2024-06-16 08:30:00'),
		('2024-06-15 23:59:59')`)
}

// setupSQLiteRangesTable 创建 ranges 表（BetweenColumns/ValueBetween 测试用）。
func setupSQLiteRangesTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE ranges (
		id INTEGER PRIMARY KEY,
		val INTEGER NOT NULL,
		lo  INTEGER NOT NULL,
		hi  INTEGER NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO ranges (id, val, lo, hi) VALUES
		(1, 5, 1, 10),
		(2, 15, 1, 10),
		(3, 7, 6, 8)`)
}

// setupSQLiteWalletsTable 创建 wallets 表（Increment/Decrement 测试用）。
func setupSQLiteWalletsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE wallets (
		id      INTEGER PRIMARY KEY,
		balance INTEGER NOT NULL,
		points  INTEGER NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO wallets (id, balance, points) VALUES
		(1, 100, 10),
		(2, 200, 20)`)
}

// setupSQLiteArchiveTable 创建 archive 表（InsertOrIgnoreUsing 测试用）。
func setupSQLiteArchiveTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE archive (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		name  TEXT NOT NULL,
		email TEXT UNIQUE
	)`)
	mustExec(t, db, `INSERT INTO archive (name, email) VALUES
		('alice', 'alice@test.com'),
		('zoe', 'zoe@test.com')`)
}

// setupSQLiteColorsTable 创建 colors 表（CrossJoinOn 测试用）。
func setupSQLiteColorsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE colors (
		id   INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO colors (id, name) VALUES (1, 'red'), (2, 'blue')`)
}
