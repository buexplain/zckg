// 本文件为 Pool 的单元测试——AddSlave 锁粒度：
// 验证 sql.Open + Ping（网络往返）在写锁之外执行，不阻塞 PickReadDB 的读锁。
package zcdb

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"sync"
	"testing"
	"time"
)

// blockingPingDriver 测试专用驱动：Open 返回的连接在 Ping 时阻塞，直到外部 close(release)。
// 用于在单元测试中模拟 AddSlave 的慢网络往返，验证其不持有写锁。
type blockingPingDriver struct {
	mu          sync.Mutex
	release     chan struct{}
	pingStarted chan struct{}
	pingOnce    sync.Once
}

func (d *blockingPingDriver) reset(release, pingStarted chan struct{}) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.release = release
	d.pingStarted = pingStarted
	d.pingOnce = sync.Once{}
}

func (d *blockingPingDriver) Open(name string) (driver.Conn, error) {
	return &blockingPingConn{driver: d}, nil
}

type blockingPingConn struct {
	driver *blockingPingDriver
}

func (c *blockingPingConn) Prepare(query string) (driver.Stmt, error) {
	return nil, errors.New("blockingPingConn: Prepare not supported")
}

func (c *blockingPingConn) Close() error { return nil }

func (c *blockingPingConn) Begin() (driver.Tx, error) {
	return nil, errors.New("blockingPingConn: Begin not supported")
}

// Ping 阻塞直到 release 关闭或 ctx 取消；首次调用时通知 pingStarted。
func (c *blockingPingConn) Ping(ctx context.Context) error {
	d := c.driver
	d.mu.Lock()
	release := d.release
	pingStarted := d.pingStarted
	d.mu.Unlock()

	d.pingOnce.Do(func() { close(pingStarted) })
	select {
	case <-release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var (
	testBlockingPingDriver         = &blockingPingDriver{}
	registerBlockingPingDriverOnce sync.Once
)

// registerBlockingPingDriver 注册测试驱动（进程内仅一次，避免重复注册 panic）。
func registerBlockingPingDriver() {
	registerBlockingPingDriverOnce.Do(func() {
		sql.Register("zcdb_test_blocking_ping_driver", testBlockingPingDriver)
	})
}

// TestPool_AddSlaveDoesNotBlockPickReadDB 验证 AddSlave 的 Ping 阻塞期间 PickReadDB 不被写锁阻塞：
// 旧实现将 sql.Open + Ping 置于写锁内，慢网络下所有读路由停顿；修复后锁外建连，
// 仅在连接验证成功后短暂持锁追加。
func TestPool_AddSlaveDoesNotBlockPickReadDB(t *testing.T) {
	registerBlockingPingDriver()
	release := make(chan struct{})
	pingStarted := make(chan struct{})
	testBlockingPingDriver.reset(release, pingStarted)

	master, err := sql.Open("zcdb_test_blocking_ping_driver", "master")
	if err != nil {
		t.Fatalf("open master failed: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	pool := &Pool{
		master:                master,
		driverName:            "zcdb_test_blocking_ping_driver",
		slaveStrategy:         &RandomStrategy{},
		maxOpenConns:          50,
		maxIdleConns:          50,
		connMaxLifetimeSecond: 600,
	}

	// AddSlave 将阻塞在 Ping（模拟慢网络）
	addDone := make(chan error, 1)
	go func() {
		addDone <- pool.AddSlave("slave1")
	}()

	// 等待 Ping 实际开始（旧实现此刻已持有写锁）
	select {
	case <-pingStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("AddSlave did not reach Ping in time")
	}

	// AddSlave 仍阻塞期间，PickReadDB 必须立即返回（旧实现在此阻塞直至 AddSlave 完成）
	pickDone := make(chan struct{})
	go func() {
		_ = pool.PickReadDB()
		close(pickDone)
	}()
	select {
	case <-pickDone:
		// 锁外 Open+Ping：读路由不被阻塞
	case <-time.After(200 * time.Millisecond):
		t.Fatal("PickReadDB blocked while AddSlave is pinging (write lock held during network IO)")
	}

	// 释放 Ping，AddSlave 正常完成并追加从库
	close(release)
	select {
	case err := <-addDone:
		if err != nil {
			t.Fatalf("AddSlave error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("AddSlave did not complete after release")
	}
	if got := pool.PickReadDB(); got == pool.master {
		t.Errorf("expected PickReadDB to return the newly added slave")
	}
}

// TestPool_NewPoolConnectTimeout 验证 NewPool 的主库 Ping 受 ConnectTimeout 限时（ZCDB-03 回归锁定：
// 修复前 Ping 无超时，不可达主机静默丢包时启动长时间挂起）。
// 采用单元测试的原因：超时行为依赖“阻塞式 Ping”模拟（静默丢包主机在集成环境无法稳定复现），
// 复用包内 blockingPingDriver 可确定性验证。
func TestPool_NewPoolConnectTimeout(t *testing.T) {
	registerBlockingPingDriver()
	release := make(chan struct{}) // 永不关闭：模拟不可达主机的静默丢包
	pingStarted := make(chan struct{})
	testBlockingPingDriver.reset(release, pingStarted)

	start := time.Now()
	_, err := NewPool(PoolConfig{
		DriverName:     "zcdb_test_blocking_ping_driver",
		DSN:            "master",
		ConnectTimeout: 100 * time.Millisecond,
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping 超时时 NewPool 应返回错误")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("NewPool 未在超时内返回（耗时 %v），主库 Ping 未受 ConnectTimeout 限时", elapsed)
	}
}

// TestPool_AddSlaveConnectTimeout 验证 AddSlave 的 Ping 受池的 connectTimeout 限时（ZCDB-03），
// 且超时失败后不追加从库、读路由回退主库。
func TestPool_AddSlaveConnectTimeout(t *testing.T) {
	registerBlockingPingDriver()
	release := make(chan struct{}) // 永不关闭：模拟不可达从库
	pingStarted := make(chan struct{})
	testBlockingPingDriver.reset(release, pingStarted)

	master, err := sql.Open("zcdb_test_blocking_ping_driver", "master")
	if err != nil {
		t.Fatalf("open master failed: %v", err)
	}
	t.Cleanup(func() { _ = master.Close() })
	pool := &Pool{
		master:                master,
		driverName:            "zcdb_test_blocking_ping_driver",
		slaveStrategy:         &RandomStrategy{},
		maxOpenConns:          50,
		maxIdleConns:          50,
		connMaxLifetimeSecond: 600,
		connectTimeout:        100 * time.Millisecond,
	}

	start := time.Now()
	err = pool.AddSlave("slave1")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Ping 超时时 AddSlave 应返回错误")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("AddSlave 未在超时内返回（耗时 %v），从库 Ping 未受限", elapsed)
	}
	if got := pool.PickReadDB(); got != pool.master {
		t.Errorf("超时失败后无从库，PickReadDB 应回退主库")
	}
}

// TestPool_CloseIdempotentAndAddSlaveAfterClose 验证 Close 幂等与关闭后拒绝新增从库
// （ZCDB-04 回归锁定：修复前 Close 非幂等语义未定义，关闭后 AddSlave 仍能成功追加从库）。
func TestPool_CloseIdempotentAndAddSlaveAfterClose(t *testing.T) {
	registerBlockingPingDriver()
	release := make(chan struct{})
	close(release) // Ping 立即通过
	pingStarted := make(chan struct{})
	testBlockingPingDriver.reset(release, pingStarted)

	master, err := sql.Open("zcdb_test_blocking_ping_driver", "master")
	if err != nil {
		t.Fatalf("open master failed: %v", err)
	}
	pool := &Pool{
		master:                master,
		driverName:            "zcdb_test_blocking_ping_driver",
		slaveStrategy:         &RandomStrategy{},
		maxOpenConns:          50,
		maxIdleConns:          50,
		connMaxLifetimeSecond: 600,
		connectTimeout:        time.Second,
	}

	// 首次 Close 执行实际清理
	if err := pool.Close(); err != nil {
		t.Fatalf("首次 Close 不应报错: %v", err)
	}
	// 重复 Close 幂等返回 nil
	if err := pool.Close(); err != nil {
		t.Errorf("重复 Close 应幂等返回 nil，实际: %v", err)
	}
	// Close 后 AddSlave 快速失败，不形成“池已关闭但持有活从库”的不一致状态
	if err := pool.AddSlave("slave1"); !errors.Is(err, errPoolClosed) {
		t.Errorf("Close 后 AddSlave 应返回 errPoolClosed，实际: %v", err)
	}
}
