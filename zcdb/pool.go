package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Pool 数据库连接池，封装主从 *sql.DB 的创建和配置。
type Pool struct {
	mu            sync.RWMutex  // 保护 slaves 切片的并发读写
	master        *sql.DB       // 主库连接（写操作 + 事务）
	slaves        []*sql.DB     // 从库连接列表（读操作，可为空）
	slaveStrategy SlaveStrategy // 从库选择策略
	driverName    string        // 驱动名称

	// 连接池配置（用于热加载从库时复用）
	maxOpenConns          int
	maxIdleConns          int
	connMaxLifetimeSecond int
}

// PoolConfig 连接池配置。
type PoolConfig struct {
	DriverName            string        // 驱动名，传给 sql.Open: "mysql", "postgres", "sqlite3" 等
	DSN                   string        // 主库连接字符串
	SlaveDSNs             []string      // 从库连接字符串列表，为空则不分读写
	MaxOpenConns          int           // 最大打开连接数，默认 50（主库和每个从库独立应用）
	MaxIdleConns          int           // 最大空闲连接数，默认 50（主库和每个从库独立应用）
	ConnMaxLifetimeSecond int           // 连接最大存活秒数，默认 600（主库和每个从库独立应用）
	SlaveStrategy         SlaveStrategy // 从库选择策略，默认 RandomStrategy
}

// NewPool 创建连接池（内部完成 sql.Open → 配置 → Ping）。
// DriverName 用于 sql.Open 打开主库连接。
// SlaveDSNs 中的每个 DSN 都会创建一个独立的从库 *sql.DB。
func NewPool(cfg PoolConfig) (*Pool, error) {
	if cfg.DriverName == "" {
		return nil, errors.New("zcdb: DriverName is required")
	}
	if cfg.DSN == "" {
		return nil, errors.New("zcdb: DSN is required")
	}

	// 默认值
	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns <= 0 {
		maxOpenConns = 50
	}
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns <= 0 {
		maxIdleConns = 50
	}
	connMaxLifetimeSecond := cfg.ConnMaxLifetimeSecond
	if connMaxLifetimeSecond <= 0 {
		connMaxLifetimeSecond = 600
	}

	// 打开主库
	master, err := sql.Open(cfg.DriverName, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("zcdb: open master failed: %w", err)
	}
	master.SetMaxOpenConns(maxOpenConns)
	master.SetMaxIdleConns(maxIdleConns)
	master.SetConnMaxLifetime(time.Duration(connMaxLifetimeSecond) * time.Second)

	// 验证主库连接
	if err := master.Ping(); err != nil {
		_ = master.Close()
		return nil, fmt.Errorf("zcdb: ping master failed: %w", err)
	}

	pool := &Pool{
		master:                master,
		driverName:            cfg.DriverName,
		maxOpenConns:          maxOpenConns,
		maxIdleConns:          maxIdleConns,
		connMaxLifetimeSecond: connMaxLifetimeSecond,
	}

	// 从库选择策略
	if cfg.SlaveStrategy != nil {
		pool.slaveStrategy = cfg.SlaveStrategy
	} else {
		pool.slaveStrategy = &RandomStrategy{}
	}

	// 打开从库
	for _, dsn := range cfg.SlaveDSNs {
		if err := pool.AddSlave(dsn); err != nil {
			_ = pool.Close()
			return nil, fmt.Errorf("zcdb: open slave failed: %w", err)
		}
	}

	return pool, nil
}

// AddSlave 动态添加从库连接。可在运行期调用，热加载从库。
// 内部使用 sync.RWMutex 保护 slaves 切片，并发安全。
// 新添加的从库同样应用 MaxOpenConns / MaxIdleConns / ConnMaxLifetimeSecond 配置。
func (p *Pool) AddSlave(dsn string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	db, err := sql.Open(p.driverName, dsn)
	if err != nil {
		return fmt.Errorf("open slave db: %w", err)
	}
	db.SetMaxOpenConns(p.maxOpenConns)
	db.SetMaxIdleConns(p.maxIdleConns)
	db.SetConnMaxLifetime(time.Duration(p.connMaxLifetimeSecond) * time.Second)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return fmt.Errorf("ping slave db: %w", err)
	}

	p.slaves = append(p.slaves, db)
	return nil
}

func (p *Pool) Ping(ctx context.Context) error {
	err := p.master.PingContext(ctx)
	if err != nil {
		return err
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if len(p.slaves) == 0 {
		return nil
	}
	for _, s := range p.slaves {
		err = s.PingContext(ctx)
		if err != nil {
			return err
		}
	}
	return nil
}

// Close 关闭连接池（主库 + 所有从库）。
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error
	if p.master != nil {
		if err := p.master.Close(); err != nil {
			firstErr = err
		}
	}
	for _, slave := range p.slaves {
		if err := slave.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	p.slaves = nil
	return firstErr
}

// PickReadDB 选择读操作的目标连接（不感知事务，事务检测由调用方 DBDao 处理）。
// 无从库配置或策略返回 nil 时，降级返回主库。
func (p *Pool) PickReadDB() *sql.DB {
	p.mu.RLock()
	slaves := p.slaves
	p.mu.RUnlock()

	if len(slaves) == 0 {
		return p.master
	}

	db := p.slaveStrategy.Pick(slaves)
	if db == nil {
		// 策略返回 nil，兜底降级到主库
		return p.master
	}
	return db
}

// PickWriteDB 选择写操作的目标连接（始终返回主库）。
func (p *Pool) PickWriteDB() *sql.DB {
	return p.master
}
