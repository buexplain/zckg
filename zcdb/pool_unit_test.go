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
