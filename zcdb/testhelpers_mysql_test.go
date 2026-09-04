// 本文件为 MySQL 集成测试共享的公共基础设施：
// 连接建立（不可达时门控跳过）、建表/清表 helper。

package zcdb

import (
	"context"
	"database/sql"
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
