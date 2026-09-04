// 本文件为 PostgreSQL 集成测试共享的公共基础设施：
// 连接建立（不可达时门控跳过）、建表/清表 helper。

package zcdb

import (
	"context"
	_ "github.com/lib/pq"
	"log"
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
