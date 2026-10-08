// 本文件为 Pool 生命周期的 SQLite 集成测试（需真实连接验证 Close/AddSlave/PickReadDB 行为）。
package zcdb

import (
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
