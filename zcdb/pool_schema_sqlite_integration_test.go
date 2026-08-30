// 本文件为 Pool 生命周期与 SchemaInspector 的补充测试。
// Pool 部分为 SQLite 集成测试（需真实连接验证 Close/AddSlave/PickReadDB 行为）；
// SchemaInspector 部分覆盖三方言 Tables/Columns 的真实查询路径（SQLite 用内存库）。
package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestPoolInteg_CloseIdempotent 验证 Close 幂等：重复调用返回 nil。
func TestPoolInteg_CloseIdempotent(t *testing.T) {
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("second Close should be no-op, got %v", err)
	}
}

// TestPoolInteg_AddSlaveAfterClose 验证 Close 后 AddSlave 返回 errPoolClosed。
func TestPoolInteg_AddSlaveAfterClose(t *testing.T) {
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := pool.AddSlave("file::memory:?cache=shared"); !errors.Is(err, errPoolClosed) {
		t.Fatalf("expected errPoolClosed, got %v", err)
	}
}

// nilStrategy 从库选择策略：始终返回 nil，用于验证 PickReadDB 的降级兜底分支。
type nilStrategy struct{}

func (nilStrategy) Pick([]*sql.DB) *sql.DB { return nil }

// TestPoolInteg_PickReadDBNilStrategyFallback 验证策略返回 nil 时降级返回主库。
func TestPoolInteg_PickReadDBNilStrategyFallback(t *testing.T) {
	pool, err := NewPool(PoolConfig{
		DriverName:    "sqlite",
		DSN:           ":memory:",
		SlaveDSNs:     []string{"file::memory:?cache=shared"},
		SlaveStrategy: nilStrategy{},
	})
	if err != nil {
		t.Fatalf("NewPool failed: %v", err)
	}
	defer pool.Close()

	db := pool.PickReadDB()
	if db == nil {
		t.Fatal("PickReadDB should fall back to master, got nil")
	}
	if db != pool.master {
		t.Fatal("PickReadDB should return master when strategy returns nil")
	}
}

// TestSQLiteInteg_SchemaInspector 验证 SQLite SchemaInspector 的 Tables/Columns 查询。
func TestSQLiteInteg_SchemaInspector(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	inspector, err := NewSchemaInspector(db)
	if err != nil {
		t.Fatalf("NewSchemaInspector: %v", err)
	}
	sqliteInsp, ok := inspector.(*SQLiteSchemaInspector)
	if !ok {
		t.Fatalf("expected *SQLiteSchemaInspector, got %T", inspector)
	}

	tables, err := sqliteInsp.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	found := false
	for _, tb := range tables {
		if tb.Name == "users" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected users table in Tables result, got %+v", tables)
	}

	cols, err := sqliteInsp.Columns(context.Background(), "users")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(cols) == 0 {
		t.Fatal("expected columns for users table, got empty")
	}
	byName := map[string]ColumnInfo{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if _, ok := byName["id"]; !ok {
		t.Fatalf("expected id column, got %+v", cols)
	}
	if _, ok := byName["name"]; !ok {
		t.Fatalf("expected name column, got %+v", cols)
	}
}
