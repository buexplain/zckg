// 本文件为 SQLite 集成测试共享的公共基础设施：
// 连接建立、mustExec、建表/清表 helper。

package zcdb

import (
	"context"
	"fmt"
	"log"
	_ "modernc.org/sqlite"
	"testing"
	"time"
)

// openSQLiteTestDB 打开一个内存 SQLite 数据库，测试结束后自动关闭。
// MaxOpenConns 必须为 1：SQLite :memory: 的每个连接持有各自独立的内存库，
// 多连接下一个连接建表写入的数据在另一个连接上查不到（与 docReviewSQLiteDAO 一致）。
func openSQLiteTestDB(t *testing.T) *DBDao {
	t.Helper()
	pool, err := NewPool(PoolConfig{
		DriverName:   "sqlite",
		DSN:          ":memory:",
		MaxOpenConns: 1,
	})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	dao, err := NewDBDao(pool, "sqlite", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		log.Default().Println(sqlStr, args)
	}, "", testComment)
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })
	return dao
}

// openSQLiteCommentDAO 为每个测试建立唯一共享内存库和独立主从连接，以便验证路由。
// 纯 Go 驱动，无外部服务或跳过分支；DSN 为 file:sql_comment_<testing.T地址>?mode=memory&cache=shared。
func openSQLiteCommentDAO(t *testing.T, comment string, callback SlowSQLCallback) *DBDao {
	t.Helper()
	dsn := fmt.Sprintf("file:sql_comment_%p?mode=memory&cache=shared", t)
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: dsn, SlaveDSNs: []string{dsn}, MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("open sqlite comment pool: %v", err)
	}
	t.Cleanup(func() {
		if err := pool.Close(); err != nil {
			t.Errorf("close sqlite comment pool: %v", err)
		}
	})
	dao, err := NewDBDao(pool, "sqlite", callback, "", comment)
	if err != nil {
		t.Fatalf("create sqlite comment DAO: %v", err)
	}
	return dao
}

// setupCommentTable 为跨方言注释用例创建隔离表，注册后置 DROP（先于 DAO cleanup 执行）。
func setupCommentTable(t *testing.T, dao *DBDao, table string, definition ...string) {
	t.Helper()
	wrapped := dao.grammar.WrapTable(table)
	drop := "DROP TABLE IF EXISTS " + wrapped
	mustExec(t, dao, drop)
	t.Cleanup(func() {
		if _, err := dao.Exec(context.Background(), drop); err != nil {
			t.Errorf("cleanup %s: %v", table, err)
		}
	})
	columns := "id INTEGER PRIMARY KEY, name VARCHAR(64), num INTEGER"
	if len(definition) > 0 {
		columns = definition[0]
	}
	mustExec(t, dao, "CREATE TABLE "+wrapped+" ("+columns+")")
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
