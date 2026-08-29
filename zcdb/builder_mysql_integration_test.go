// 本文件为 MySQL 集成测试的公共基础设施：连接建立、建表/清表 helper，
// 以及 Builder 基础能力（建连/事务/连接池/Clone/扫描等）测试。
package zcdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"testing"
	"time"
)

// mysqlTestMasterDSN 集成测试共用的主库 DSN。
const mysqlTestMasterDSN = "root:root@tcp(127.0.0.1:3306)/?charset=utf8mb4&parseTime=true&loc=Local"

// requireMySQLAvailable 探测 MySQL 主库是否可达：不可达时跳过测试（门控），
// 保证无数据库环境下 go test ./... 不误报。
func requireMySQLAvailable(t *testing.T) {
	t.Helper()
	db, err := sql.Open("mysql", mysqlTestMasterDSN)
	if err != nil {
		t.Skipf("mysql unavailable, skipping integration test: %v", err)
	}
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Skipf("mysql unavailable, skipping integration test: %v", err)
	}
}

// openMySQLTestDB 打开 MySQL 连接，自动创建测试数据库（若不存在），然后清理并重建 users/orders 相关表，保证测试隔离。
// docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
func openMySQLTestDB(t *testing.T) *DBDao {
	t.Helper()
	pool, err := NewPool(PoolConfig{
		DriverName: "mysql",
		DSN:        mysqlTestMasterDSN,
	})
	if err != nil {
		// 门控：MySQL 不可达时跳过而非失败，保证无数据库环境下 go test 不误报
		t.Skipf("mysql unavailable, skipping integration test: %v", err)
	}
	dao, err := NewDBDao(pool, "mysql", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	}, "")
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
	tables := []string{"users_archive", "profiles", "orders", "users",
		"numeric_test", "datetime_test", "string_test", "binary_test", "bool_test",
		"json_conv_test", "articles", "bit_test"}
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

// TestMySQLInteg_TransactionPanicRollback 验证事务回调 panic 时，事务应自动回滚。
func TestMySQLInteg_TransactionPanicRollback(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_Bug_CloneNestedBuilder 验证 Clone 对嵌套 Builder（UNION 子查询）深拷贝：
// 修改原始嵌套 Builder 后，克隆体不受影响。
func TestMySQLInteg_Bug_CloneNestedBuilder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_AddSlave_Success 验证 AddSlave 成功添加从库后，PickReadDB 返回从库连接而非主库。
func TestMySQLInteg_AddSlave_Success(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	pool := db.Pool()
	// 添加前：无从库，PickReadDB 应返回主库
	if readDB := pool.PickReadDB(); readDB != pool.PickWriteDB() {
		t.Fatalf("expected PickReadDB to return master before AddSlave")
	}

	// 使用相同 DSN 作为从库（测试机制，非真实复制）
	slaveDSN := "root:root@tcp(127.0.0.1:3306)/zckg_test_integ?charset=utf8mb4&parseTime=true&loc=Local"
	if err := pool.AddSlave(slaveDSN); err != nil {
		t.Fatalf("AddSlave error: %v", err)
	}

	// 添加后：PickReadDB 应返回从库（非主库）
	readDB := pool.PickReadDB()
	if readDB == pool.PickWriteDB() {
		t.Errorf("expected PickReadDB to return slave after AddSlave, got master")
	}

	// 验证 Ping 主库 + 从库均正常
	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("Ping error after AddSlave: %v", err)
	}

	// 验证通过从库可以正常查询
	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query via slave error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows from slave, got %d", len(rows))
	}
}

// TestMySQLInteg_AddSlave_InvalidDSN 验证 AddSlave 使用无效 DSN 时返回错误且不破坏连接池状态。
func TestMySQLInteg_AddSlave_InvalidDSN(t *testing.T) {
	db := openMySQLTestDB(t)
	pool := db.Pool()

	// 无效 DSN：连接不可达
	badDSN := "root:wrongpassword@tcp(127.0.0.1:33999)/nonexistent?charset=utf8mb4&parseTime=true&loc=Local"
	err := pool.AddSlave(badDSN)
	if err == nil {
		t.Fatalf("expected error for invalid slave DSN, got nil")
	}

	// 添加失败后，PickReadDB 应仍返回主库（无从库）
	readDB := pool.PickReadDB()
	if readDB != pool.PickWriteDB() {
		t.Errorf("expected PickReadDB to return master after failed AddSlave")
	}

	// 主库仍正常工作
	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("Ping error after failed AddSlave: %v", err)
	}
}

// TestMySQLInteg_AddSlave_Multiple 验证添加多个从库后，PickReadDB 始终返回从库之一。
func TestMySQLInteg_AddSlave_Multiple(t *testing.T) {
	db := openMySQLTestDB(t)
	pool := db.Pool()

	slaveDSN := "root:root@tcp(127.0.0.1:3306)/zckg_test_integ?charset=utf8mb4&parseTime=true&loc=Local"
	// 添加 3 个从库（同一 DSN，测试机制）
	for i := 0; i < 3; i++ {
		if err := pool.AddSlave(slaveDSN); err != nil {
			t.Fatalf("AddSlave #%d error: %v", i, err)
		}
	}

	// 多次调用 PickReadDB，应始终返回从库
	masterDB := pool.PickWriteDB()
	for i := 0; i < 10; i++ {
		if readDB := pool.PickReadDB(); readDB == masterDB {
			t.Errorf("PickReadDB returned master on iteration %d, expected slave", i)
			break
		}
	}

	// Ping 应正常（主库 + 3 个从库）
	if err := pool.Ping(context.Background()); err != nil {
		t.Errorf("Ping error with multiple slaves: %v", err)
	}
}

// TestMySQLInteg_NewPoolErrors 验证 NewPool 对空 DriverName、空 DSN、无效驱动的校验。
func TestMySQLInteg_NewPoolErrors(t *testing.T) {
	// 空 DriverName
	_, err := NewPool(PoolConfig{DSN: "root:root@tcp(127.0.0.1:3306)/"})
	if err == nil {
		t.Error("expected error for empty DriverName, got nil")
	}

	// 空 DSN
	_, err = NewPool(PoolConfig{DriverName: "mysql"})
	if err == nil {
		t.Error("expected error for empty DSN, got nil")
	}

	// 无效驱动
	_, err = NewPool(PoolConfig{DriverName: "invalid_driver", DSN: "test"})
	if err == nil {
		t.Error("expected error for invalid driver, got nil")
	}
}

// TestMySQLInteg_NewPoolPingFail 验证 NewPool 连接失败时返回错误。
func TestMySQLInteg_NewPoolPingFail(t *testing.T) {
	_, err := NewPool(PoolConfig{
		DriverName: "mysql",
		DSN:        "root:wrong_password@tcp(127.0.0.1:33306)/?charset=utf8mb4",
	})
	if err == nil {
		t.Error("expected error for unreachable server, got nil")
	}
}

// TestMySQLInteg_ScanStructPtrSlice 验证 ScanStruct 支持 *[]*struct 指针切片。
func TestMySQLInteg_ScanStructPtrSlice(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []*row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Find with *[]*struct error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
	if rows[0].Name != "alice" {
		t.Errorf("expected alice, got %s", rows[0].Name)
	}
}

// TestMySQLInteg_ScanStructNonStruct 验证 ScanStruct 传入非结构体目标时返回错误。
func TestMySQLInteg_ScanStructNonStruct(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 传入 *int 而非 *struct，应返回错误
	var n int
	err := db.Builder().Table("users").Select("name").Limit(1).First(context.Background(), &n)
	if err == nil {
		t.Error("expected error for non-struct dest, got nil")
	}
}

// TestMySQLInteg_PoolPingNoSlaves 验证 Pool.Ping 无从库时仅 Ping 主库。
func TestMySQLInteg_PoolPingNoSlaves(t *testing.T) {
	db := openMySQLTestDB(t)
	err := db.Pool().Ping(context.Background())
	if err != nil {
		t.Errorf("Ping with no slaves error: %v", err)
	}
}

// TestMySQLInteg_PoolPickReadDBNoSlaves 验证 Pool.PickReadDB 无从库时降级返回主库。
func TestMySQLInteg_PoolPickReadDBNoSlaves(t *testing.T) {
	db := openMySQLTestDB(t)
	readDB := db.Pool().PickReadDB()
	masterDB := db.Pool().PickWriteDB()
	if readDB != masterDB {
		t.Error("expected PickReadDB to return master when no slaves configured")
	}
}

// TestMySQLInteg_ScanAllRowsNonStruct 验证 Find 传入非结构体切片时返回错误。
func TestMySQLInteg_ScanAllRowsNonStruct(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 传入 []int 而非 []struct，应返回错误
	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Error("expected error for non-struct slice dest, got nil")
	}
}

// TestMySQLInteg_PoolCloseAndPing 验证 Pool 创建后 Close 再 Ping 返回错误。
func TestMySQLInteg_PoolCloseAndPing(t *testing.T) {
	requireMySQLAvailable(t)
	pool, err := NewPool(PoolConfig{
		DriverName: "mysql",
		DSN:        mysqlTestMasterDSN,
	})
	if err != nil {
		t.Fatalf("NewPool error: %v", err)
	}

	// 关闭连接池
	if err := pool.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}

	// 关闭后 Ping 应返回错误
	if err := pool.Ping(context.Background()); err == nil {
		t.Error("expected error after Close, got nil")
	}
}

// TestMySQLInteg_NewPoolWithSlaveFail 验证 NewPool 从库连接失败时整体返回错误。
// 需先探测主库可达：若主库不可达则跳过（避免因主库失败而静默通过），
// 主库可达、从库不可达时才断言 NewPool 返回错误。
func TestMySQLInteg_NewPoolWithSlaveFail(t *testing.T) {
	requireMySQLAvailable(t)
	_, err := NewPool(PoolConfig{
		DriverName: "mysql",
		DSN:        mysqlTestMasterDSN,
		SlaveDSNs:  []string{"root:wrong_password@tcp(127.0.0.1:33306)/?charset=utf8mb4"},
	})
	if err == nil {
		t.Error("expected error for unreachable slave, got nil")
	}
}

// TestMySQLInteg_SchemaInspector_Tables 验证 Tables 返回表名和注释。
func TestMySQLInteg_SchemaInspector_Tables(t *testing.T) {
	db := openMySQLTestDB(t)
	// 创建带注释的表
	mustExec(t, db, "CREATE TABLE `test_schema_a` (`id` INT NOT NULL PRIMARY KEY) ENGINE=InnoDB COMMENT='表A注释'")
	mustExec(t, db, "CREATE TABLE `test_schema_b` (`id` INT NOT NULL PRIMARY KEY) ENGINE=InnoDB COMMENT='表B注释'")
	defer func() {
		mustExec(t, db, "DROP TABLE IF EXISTS `test_schema_a`")
		mustExec(t, db, "DROP TABLE IF EXISTS `test_schema_b`")
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	tables, err := inspector.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}

	// 找到我们创建的表
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

// TestMySQLInteg_SchemaInspector_Columns 验证 Columns 返回字段名、类型、注释、Nullable、Default。
func TestMySQLInteg_SchemaInspector_Columns(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, `CREATE TABLE `+"`test_columns`"+` (
		`+"`id`"+` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		`+"`name`"+` VARCHAR(64) NOT NULL COMMENT '用户名',
		`+"`age`"+` INT NULL COMMENT '年龄',
		`+"`status`"+` VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT '状态'
	) ENGINE=InnoDB COMMENT='测试字段表'`)
	defer func() {
		mustExec(t, db, "DROP TABLE IF EXISTS `test_columns`")
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

	// 验证每个字段
	checks := []struct {
		name     string
		typ      string
		comment  string
		nullable bool
		hasDef   bool
		defVal   string
	}{
		{"id", "bigint unsigned", "", false, false, ""},
		{"name", "varchar(64)", "用户名", false, false, ""},
		{"age", "int", "年龄", true, false, ""},
		{"status", "varchar(16)", "状态", false, true, "active"},
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
		if c.hasDef {
			if columns[i].Default == nil {
				t.Errorf("col[%d] %s: expected default %q, got nil", i, c.name, c.defVal)
			} else if *columns[i].Default != c.defVal {
				t.Errorf("col[%d] %s: expected default %q, got %q", i, c.name, c.defVal, *columns[i].Default)
			}
		} else if columns[i].Default != nil && c.name != "id" {
			// id 有默认值 '' (空字符串)，跳过检查
			t.Errorf("col[%d] %s: expected no default, got %q", i, c.name, *columns[i].Default)
		}
	}
}

// TestMySQLInteg_SchemaInspector_NonexistentTable 边界固化（审查结论）：
// 不存在的表查 Columns 返回空切片与 nil 错误（information_schema 查询无命中行），
// 不报错也不 panic；调用方以空切片自行判断。
func TestMySQLInteg_SchemaInspector_NonexistentTable(t *testing.T) {
	db := openMySQLTestDB(t)
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

// TestMySQLInteg_NullSafeField_NumericTypes 验证数值类型的 NULL 安全扫描。
// 覆盖：TINYINT, SMALLINT, MEDIUMINT, INT, BIGINT, FLOAT, DOUBLE, DECIMAL, BIT
func TestMySQLInteg_NullSafeField_NumericTypes(t *testing.T) {
	db := openMySQLTestDB(t)

	// 创建包含所有数值类型的表
	mustExec(t, db, `CREATE TABLE numeric_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		-- 整数类型
		tiny_val TINYINT,
		small_val SMALLINT,
		med_val  MEDIUMINT,
		int_val  INT,
		big_val  BIGINT,
		-- 无符号整数
		utiny_val TINYINT UNSIGNED,
		ubig_val  BIGINT UNSIGNED,
		-- 浮点类型
		float_val FLOAT,
		double_val DOUBLE,
		-- 精确小数
		decimal_val DECIMAL(10,2),
		-- 位字段
		bit_val  BIT(8)
	)`)

	// 插入有值行和 NULL 行
	mustExec(t, db, `INSERT INTO numeric_test (tiny_val, small_val, med_val, int_val, big_val,
		utiny_val, ubig_val, float_val, double_val, decimal_val, bit_val)
		VALUES (127, 32767, 8388607, 2147483647, 9223372036854775807,
			255, 18446744073709551615, 3.14, 2.718281828, 99999.99, B'10101010')`)
	mustExec(t, db, `INSERT INTO numeric_test (tiny_val, small_val, med_val, int_val, big_val,
		utiny_val, ubig_val, float_val, double_val, decimal_val, bit_val)
		VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)

	type numericRow struct {
		ID      int     `db:"id"`
		Tiny    int8    `db:"tiny_val"`
		Small   int16   `db:"small_val"`
		Med     int32   `db:"med_val"`
		Int     int     `db:"int_val"`
		Big     int64   `db:"big_val"`
		UTiny   uint8   `db:"utiny_val"`
		UBig    uint64  `db:"ubig_val"`
		Float   float32 `db:"float_val"`
		Double  float64 `db:"double_val"`
		Decimal float64 `db:"decimal_val"`
		Bit     []byte  `db:"bit_val"`
	}

	var results []numericRow
	err := db.Builder().Table("numeric_test").
		Select("id", "tiny_val", "small_val", "med_val", "int_val", "big_val",
			"utiny_val", "ubig_val", "float_val", "double_val", "decimal_val", "bit_val").
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
	if r.Tiny != 127 {
		t.Errorf("Tiny: expected 127, got %d", r.Tiny)
	}
	if r.Small != 32767 {
		t.Errorf("Small: expected 32767, got %d", r.Small)
	}
	if r.Big != 9223372036854775807 {
		t.Errorf("Big: expected max int64, got %d", r.Big)
	}
	if r.UTiny != 255 {
		t.Errorf("UTiny: expected 255, got %d", r.UTiny)
	}
	if r.Double != 2.718281828 {
		t.Errorf("Double: expected 2.718281828, got %f", r.Double)
	}
	if r.Decimal != 99999.99 {
		t.Errorf("Decimal: expected 99999.99, got %f", r.Decimal)
	}

	// 验证 NULL 行（非指针类型应为零值）
	nullRow := results[1]
	if nullRow.Tiny != 0 {
		t.Errorf("NULL Tiny: expected 0, got %d", nullRow.Tiny)
	}
	if nullRow.Big != 0 {
		t.Errorf("NULL Big: expected 0, got %d", nullRow.Big)
	}
	if nullRow.Double != 0 {
		t.Errorf("NULL Double: expected 0, got %f", nullRow.Double)
	}
}

// TestMySQLInteg_NullSafeField_DateTimeTypes 验证日期时间类型的 NULL 安全扫描。
// 覆盖：DATE, TIME, DATETIME, TIMESTAMP, YEAR
func TestMySQLInteg_NullSafeField_DateTimeTypes(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE datetime_test (
		id         INT AUTO_INCREMENT PRIMARY KEY,
		date_val   DATE,
		time_val   TIME,
		datetime_val DATETIME,
		timestamp_val TIMESTAMP NULL DEFAULT NULL,
		year_val   YEAR
	)`)

	// 插入有值行和 NULL 行
	mustExec(t, db, `INSERT INTO datetime_test (date_val, time_val, datetime_val, timestamp_val, year_val)
		VALUES ('2024-06-15', '14:30:00', '2024-06-15 14:30:00', '2024-06-15 14:30:00', 2024)`)
	mustExec(t, db, `INSERT INTO datetime_test (date_val, time_val, datetime_val, timestamp_val, year_val)
		VALUES (NULL, NULL, NULL, NULL, NULL)`)

	type datetimeRow struct {
		ID        int    `db:"id"`
		Date      string `db:"date_val"`
		Time      string `db:"time_val"`
		DateTime  string `db:"datetime_val"`
		Timestamp string `db:"timestamp_val"`
		Year      int    `db:"year_val"`
	}

	var results []datetimeRow
	err := db.Builder().Table("datetime_test").
		Select("id", "date_val", "time_val", "datetime_val", "timestamp_val", "year_val").
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
	if r.Date != "2024-06-15T00:00:00+08:00" {
		t.Errorf("Date: expected 2024-06-15T00:00:00+08:00, got %s", r.Date)
	}
	if r.Time != "14:30:00" {
		t.Errorf("Time: expected 14:30:00, got %s", r.Time)
	}
	if r.Year != 2024 {
		t.Errorf("Year: expected 2024, got %d", r.Year)
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Date != "" {
		t.Errorf("NULL Date: expected empty, got %s", nullRow.Date)
	}
	if nullRow.Year != 0 {
		t.Errorf("NULL Year: expected 0, got %d", nullRow.Year)
	}
}

// TestMySQLInteg_NullSafeField_StringTypes 验证字符串类型的 NULL 安全扫描。
// 覆盖：CHAR, VARCHAR, TINYTEXT, TEXT, MEDIUMTEXT, LONGTEXT, ENUM, SET, JSON
func TestMySQLInteg_NullSafeField_StringTypes(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE string_test (
		id          INT AUTO_INCREMENT PRIMARY KEY,
		char_val    CHAR(10),
		varchar_val VARCHAR(100),
		tinytext_val TINYTEXT,
		text_val    TEXT,
		mediumtext_val MEDIUMTEXT,
		longtext_val LONGTEXT,
		enum_val    ENUM('small', 'medium', 'large'),
		set_val     SET('a', 'b', 'c'),
		json_val    JSON
	)`)

	// 插入有值行和 NULL 行
	mustExec(t, db, `INSERT INTO string_test (char_val, varchar_val, tinytext_val, text_val,
		mediumtext_val, longtext_val, enum_val, set_val, json_val)
		VALUES ('hello', 'world', 'tiny text', 'normal text', 'medium text', 'long text',
			'medium', 'a,b', '{"key": "value"}')`)
	mustExec(t, db, `INSERT INTO string_test (char_val, varchar_val, tinytext_val, text_val,
		mediumtext_val, longtext_val, enum_val, set_val, json_val)
		VALUES (NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL, NULL)`)

	type stringRow struct {
		ID         int    `db:"id"`
		Char       string `db:"char_val"`
		Varchar    string `db:"varchar_val"`
		TinyText   string `db:"tinytext_val"`
		Text       string `db:"text_val"`
		MediumText string `db:"mediumtext_val"`
		LongText   string `db:"longtext_val"`
		Enum       string `db:"enum_val"`
		Set        string `db:"set_val"`
		JSON       string `db:"json_val"`
	}

	var results []stringRow
	err := db.Builder().Table("string_test").
		Select("id", "char_val", "varchar_val", "tinytext_val", "text_val",
			"mediumtext_val", "longtext_val", "enum_val", "set_val", "json_val").
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
	if r.Char != "hello" {
		t.Errorf("Char: expected hello, got %s", r.Char)
	}
	if r.Varchar != "world" {
		t.Errorf("Varchar: expected world, got %s", r.Varchar)
	}
	if r.Enum != "medium" {
		t.Errorf("Enum: expected medium, got %s", r.Enum)
	}
	if r.Set != "a,b" {
		t.Errorf("Set: expected a,b, got %s", r.Set)
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Varchar != "" {
		t.Errorf("NULL Varchar: expected empty, got %s", nullRow.Varchar)
	}
	if nullRow.Enum != "" {
		t.Errorf("NULL Enum: expected empty, got %s", nullRow.Enum)
	}
}

// TestMySQLInteg_NullSafeField_BinaryTypes 验证二进制类型的 NULL 安全扫描。
// 覆盖：BINARY, VARBINARY, TINYBLOB, BLOB, MEDIUMBLOB, LONGBLOB
func TestMySQLInteg_NullSafeField_BinaryTypes(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE binary_test (
		id           INT AUTO_INCREMENT PRIMARY KEY,
		binary_val   BINARY(16),
		varbinary_val VARBINARY(100),
		tinyblob_val TINYBLOB,
		blob_val     BLOB,
		mediumblob_val MEDIUMBLOB,
		longblob_val LONGBLOB
	)`)

	// 插入有值行和 NULL 行
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	mustExec(t, db, `INSERT INTO binary_test (binary_val, varbinary_val, tinyblob_val, blob_val, mediumblob_val, longblob_val)
		VALUES (?, ?, ?, ?, ?, ?)`, binaryData, binaryData, binaryData, binaryData, binaryData, binaryData)
	mustExec(t, db, `INSERT INTO binary_test (binary_val, varbinary_val, tinyblob_val, blob_val, mediumblob_val, longblob_val)
		VALUES (NULL, NULL, NULL, NULL, NULL, NULL)`)

	type binaryRow struct {
		ID         int    `db:"id"`
		Binary     []byte `db:"binary_val"`
		Varbinary  []byte `db:"varbinary_val"`
		TinyBlob   []byte `db:"tinyblob_val"`
		Blob       []byte `db:"blob_val"`
		MediumBlob []byte `db:"mediumblob_val"`
		LongBlob   []byte `db:"longblob_val"`
	}

	var results []binaryRow
	err := db.Builder().Table("binary_test").
		Select("id", "binary_val", "varbinary_val", "tinyblob_val", "blob_val", "mediumblob_val", "longblob_val").
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
	if len(r.Blob) != len(binaryData) {
		t.Errorf("Blob: expected length %d, got %d", len(binaryData), len(r.Blob))
	}

	// 验证 NULL 行
	nullRow := results[1]
	if nullRow.Blob != nil {
		t.Errorf("NULL Blob: expected nil, got %v", nullRow.Blob)
	}
}

// TestMySQLInteg_NullSafeField_BooleanType 验证 BOOLEAN 类型的 NULL 安全扫描。
// MySQL 的 BOOLEAN 实际是 TINYINT(1) 的别名。
func TestMySQLInteg_NullSafeField_BooleanType(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE bool_test (
		id         INT AUTO_INCREMENT PRIMARY KEY,
		is_active  BOOLEAN,
		is_deleted BOOLEAN,
		is_valid   BOOLEAN NOT NULL DEFAULT TRUE
	)`)

	// 插入 TRUE/FALSE/NULL
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

	// 验证 NULL 行（非指针应为零值 false）
	if results[2].IsActive {
		t.Errorf("row[2].IsActive: expected false (NULL), got true")
	}
}

// TestMySQLInteg_NullSafeField_JSONConversions 验证 JSON 类型可以转换到多种 Go 类型。
// 覆盖：[]byte、json.RawMessage、map[string]any、自定义结构体
func TestMySQLInteg_NullSafeField_JSONConversions(t *testing.T) {
	db := openMySQLTestDB(t)

	mustExec(t, db, `CREATE TABLE json_conv_test (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		json_val JSON
	)`)

	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES ('{"name":"alice","age":25}')`)
	mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES (NULL)`)

	// 测试 1: JSON → []byte
	t.Run("JSON_to_[]byte", func(t *testing.T) {
		type row struct {
			ID   int    `db:"id"`
			Data []byte `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
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

	// 测试 2: JSON → json.RawMessage
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

	// 测试 3: JSON → map[string]any
	t.Run("JSON_to_map", func(t *testing.T) {
		type row struct {
			ID   int            `db:"id"`
			Data map[string]any `db:"json_val"`
		}
		var results []row
		err := db.Builder().Table("json_conv_test").Select("id", "json_val").
			OrderBy("id", "ASC").Find(context.Background(), &results)
		if err != nil {
			t.Fatalf("Find error: %v", err)
		}
		if results[0].Data["name"] != "alice" {
			t.Errorf("expected name=alice, got %v", results[0].Data["name"])
		}
		if results[1].Data != nil {
			t.Errorf("NULL: expected nil map, got %v", results[1].Data)
		}
	})

	// 测试 4: JSON → 自定义结构体
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

	// 测试 5: JSON → *结构体（字段本身是指针）
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

	// 测试 6: JSON → 结构体含嵌套指针结构体字段
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
		mustExec(t, db, `INSERT INTO json_conv_test (json_val) VALUES ('{"name":"charlie","age":35,"address":{"city":"Shanghai"}}')`)
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

// setupMySQLNewApiTables 清理并创建 MySQL 新增 API 测试专用表。
func setupMySQLNewApiTables(t *testing.T, db *DBDao) {
	t.Helper()
	for _, table := range []string{"events", "wallets", "archive", "colors", "names_cs"} {
		mustExec(t, db, "DROP TABLE IF EXISTS `"+table+"`")
	}
}

// setupMySQLEventsTable 创建 events 表（WhereDate 测试用）。
func setupMySQLEventsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE events (
		id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		happened_at DATETIME NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO events (happened_at) VALUES
		('2024-06-15 10:00:00'),
		('2024-06-16 08:30:00'),
		('2024-06-15 23:59:59')`)
}

// setupMySQLWalletsTable 创建 wallets 表（Increment/Decrement 测试用）。
func setupMySQLWalletsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE wallets (
		id      BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		balance BIGINT NOT NULL,
		points  BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO wallets (id, balance, points) VALUES (1, 100, 10), (2, 200, 20)`)
}

// setupMySQLArchiveTable 创建 archive 表（InsertOrIgnoreUsing 测试用）。
func setupMySQLArchiveTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE archive (
		id    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name  VARCHAR(64) NOT NULL,
		email VARCHAR(128) NULL,
		UNIQUE KEY uk_email (email)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO archive (name, email) VALUES
		('alice', 'alice@test.com'),
		('zoe', 'zoe@test.com')`)
}

// setupMySQLColorsTable 创建 colors 表（CrossJoinOn 测试用）。
func setupMySQLColorsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE colors (
		id   BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		name VARCHAR(16) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO colors (id, name) VALUES (1, 'red'), (2, 'blue')`)
}

// setupMySQLNamesCsTable 创建大小写混合的名字表（Like caseSensitive 测试用）。
func setupMySQLNamesCsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE names_cs (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO names_cs (name) VALUES ('alice'), ('Alice'), ('BOB')`)
}
