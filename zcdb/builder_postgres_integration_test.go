// 本文件为 PostgreSQL 集成测试的公共基础设施：连接建立、建表/清表 helper，
// 以及 Builder 基础能力（建连/事务/连接池/Clone/扫描等）测试。
package zcdb

import (
	"context"
	"encoding/json"
	"fmt"
	_ "github.com/lib/pq"
	"log"
	"strings"
	"testing"
	"time"
)

// openPgTestDB 打开 PostgreSQL 连接，自动创建测试数据库（若不存在），然后清理并重建 users/orders 相关表，保证测试隔离。
// docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
func openPgTestDB(t *testing.T) *DBDao {
	t.Helper()

	pool, err := NewPool(PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable",
	})
	if err != nil {
		// 门控：PostgreSQL 不可达时跳过而非失败，保证无数据库环境下 go test 不误报
		t.Skipf("postgres unavailable, skipping integration test: %v", err)
	}
	dao, err := NewDBDao(pool, "postgres", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	}, "")
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
	}, "")
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
		"byte_num_test", "byte_bool_test", "ptr_test", "json_err_test",
		"articles", "bit_test", "json_date_test"}
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

// TestPgInteg_SchemaInspector_NonexistentTable 边界固化（审查结论）：
// 不存在的表查 Columns 返回空切片与 nil 错误（pg_attribute 关联无命中行），
// 不报错也不 panic；调用方以空切片自行判断。
func TestPgInteg_SchemaInspector_NonexistentTable(t *testing.T) {
	db := openPgTestDB(t)
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

// setupPgNewApiTables 清理 PG 新增 API 测试专用表。
func setupPgNewApiTables(t *testing.T, db *DBDao) {
	t.Helper()
	for _, table := range []string{"events", "wallets", "archive", "colors", "names_cs", "empty_t"} {
		mustExec(t, db, "DROP TABLE IF EXISTS \""+table+"\"")
	}
}

// setupPgEventsTable 创建 events 表（WhereDate 测试用）。
func setupPgEventsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE events (
		id          BIGSERIAL PRIMARY KEY,
		happened_at TIMESTAMP NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO events (happened_at) VALUES
		('2024-06-15 10:00:00'),
		('2024-06-16 08:30:00'),
		('2024-06-15 23:59:59')`)
}

// setupPgWalletsTable 创建 wallets 表（Increment/Decrement 测试用）。
func setupPgWalletsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE wallets (
		id      BIGINT PRIMARY KEY,
		balance BIGINT NOT NULL,
		points  BIGINT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO wallets (id, balance, points) VALUES (1, 100, 10), (2, 200, 20)`)
}

// setupPgArchiveTable 创建 archive 表（InsertOrIgnoreUsing 测试用）。
func setupPgArchiveTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE archive (
		id    BIGSERIAL PRIMARY KEY,
		name  VARCHAR(64) NOT NULL,
		email VARCHAR(128) UNIQUE
	)`)
	mustExec(t, db, `INSERT INTO archive (name, email) VALUES
		('alice', 'alice@test.com'),
		('zoe', 'zoe@test.com')`)
}

// setupPgColorsTable 创建 colors 表（CrossJoinOn 测试用）。
func setupPgColorsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE colors (
		id   BIGINT PRIMARY KEY,
		name VARCHAR(16) NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO colors (id, name) VALUES (1, 'red'), (2, 'blue')`)
}

// setupPgNamesCsTable 创建大小写混合的名字表（Like caseSensitive 测试用）。
func setupPgNamesCsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE names_cs (
		id   BIGSERIAL PRIMARY KEY,
		name VARCHAR(64) NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO names_cs (name) VALUES ('alice'), ('Alice'), ('BOB')`)
}
