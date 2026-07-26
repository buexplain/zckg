package zcdb

import (
	"database/sql"
	"math/rand"
	"sync/atomic"
)

// SlaveStrategy 从库选择策略接口。
// 注意：Pick 方法应尽量避免返回 nil。如果所有从库不可用且无法降级，可返回 nil，
// pickReadDB 会将 nil 兜底降级到主库。
type SlaveStrategy interface {
	// Pick 从 slaves 中选择一个可用的从库连接。尽量避免返回 nil。
	Pick(slaves []*sql.DB) *sql.DB
}

// RandomStrategy 随机选择从库（默认策略）。
type RandomStrategy struct{}

// Pick 随机选择一个从库。
func (s *RandomStrategy) Pick(slaves []*sql.DB) *sql.DB {
	if len(slaves) == 0 {
		return nil
	}
	return slaves[rand.Intn(len(slaves))]
}

// RoundRobinStrategy 轮询选择从库。
type RoundRobinStrategy struct {
	counter atomic.Uint64
}

// Pick 轮询依次选择从库。
func (s *RoundRobinStrategy) Pick(slaves []*sql.DB) *sql.DB {
	if len(slaves) == 0 {
		return nil
	}
	idx := s.counter.Add(1) - 1
	return slaves[idx%uint64(len(slaves))]
}
