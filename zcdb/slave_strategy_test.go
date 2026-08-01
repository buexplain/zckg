package zcdb

import (
	"database/sql"
	"sync"
	"testing"
)

// ==================== RandomStrategy 测试 ====================

// TestRandomStrategy_EmptySlaves 验证空从库列表返回 nil。
func TestRandomStrategy_EmptySlaves(t *testing.T) {
	s := &RandomStrategy{}
	if got := s.Pick(nil); got != nil {
		t.Errorf("expected nil for empty slaves, got %v", got)
	}
	if got := s.Pick([]*sql.DB{}); got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

// TestRandomStrategy_SingleSlave 验证单个从库始终返回该从库。
func TestRandomStrategy_SingleSlave(t *testing.T) {
	s := &RandomStrategy{}
	db := &sql.DB{}
	slaves := []*sql.DB{db}
	for i := 0; i < 100; i++ {
		if got := s.Pick(slaves); got != db {
			t.Errorf("expected single slave, got %v", got)
		}
	}
}

// TestRandomStrategy_MultipleSlaves 验证多从库时返回值在列表范围内。
func TestRandomStrategy_MultipleSlaves(t *testing.T) {
	s := &RandomStrategy{}
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	db3 := &sql.DB{}
	slaves := []*sql.DB{db1, db2, db3}

	validSet := map[*sql.DB]bool{db1: true, db2: true, db3: true}
	for i := 0; i < 200; i++ {
		got := s.Pick(slaves)
		if !validSet[got] {
			t.Errorf("Pick returned unexpected db: %v", got)
		}
	}
}

// TestRandomStrategy_Distribution 验证随机策略在多从库下有一定分散性。
func TestRandomStrategy_Distribution(t *testing.T) {
	s := &RandomStrategy{}
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	db3 := &sql.DB{}
	slaves := []*sql.DB{db1, db2, db3}

	counts := map[*sql.DB]int{}
	iterations := 3000
	for i := 0; i < iterations; i++ {
		counts[s.Pick(slaves)]++
	}

	// 每个从库至少应被选中一次（3000 次迭代，3 个从库，概率极低全不命中）
	for _, db := range slaves {
		if counts[db] == 0 {
			t.Errorf("slave %v was never selected in %d iterations", db, iterations)
		}
	}
}

// ==================== RoundRobinStrategy 测试 ====================

// TestRoundRobinStrategy_EmptySlaves 验证空从库列表返回 nil。
func TestRoundRobinStrategy_EmptySlaves(t *testing.T) {
	s := &RoundRobinStrategy{}
	if got := s.Pick(nil); got != nil {
		t.Errorf("expected nil for empty slaves, got %v", got)
	}
	if got := s.Pick([]*sql.DB{}); got != nil {
		t.Errorf("expected nil for empty slice, got %v", got)
	}
}

// TestRoundRobinStrategy_SingleSlave 验证单个从库始终返回该从库。
func TestRoundRobinStrategy_SingleSlave(t *testing.T) {
	s := &RoundRobinStrategy{}
	db := &sql.DB{}
	slaves := []*sql.DB{db}
	for i := 0; i < 100; i++ {
		if got := s.Pick(slaves); got != db {
			t.Errorf("expected single slave, got %v", got)
		}
	}
}

// TestRoundRobinStrategy_RoundRobin 验证轮询策略按顺序循环选择。
func TestRoundRobinStrategy_RoundRobin(t *testing.T) {
	s := &RoundRobinStrategy{}
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	db3 := &sql.DB{}
	slaves := []*sql.DB{db1, db2, db3}

	// 验证两轮完整轮询
	expected := []*sql.DB{db1, db2, db3, db1, db2, db3}
	for i, want := range expected {
		got := s.Pick(slaves)
		if got != want {
			t.Errorf("iteration %d: expected %v, got %v", i, want, got)
		}
	}
}

// TestRoundRobinStrategy_WrapAround 验证轮询在超过从库数量后正确回绕。
func TestRoundRobinStrategy_WrapAround(t *testing.T) {
	s := &RoundRobinStrategy{}
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	slaves := []*sql.DB{db1, db2}

	// 先消耗 5 次（超过一轮）
	for i := 0; i < 5; i++ {
		s.Pick(slaves)
	}

	// 第 6 次应回到 db1（5 % 2 = 1 → index 1 → db2）
	// counter = 6, idx = 5, 5 % 2 = 1 → db2
	got := s.Pick(slaves)
	if got != db2 {
		t.Errorf("after 6 picks with 2 slaves: expected db2, got %v", got)
	}
}

// TestRoundRobinStrategy_Concurrent 验证轮询策略的并发安全性。
func TestRoundRobinStrategy_Concurrent(t *testing.T) {
	s := &RoundRobinStrategy{}
	db1 := &sql.DB{}
	db2 := &sql.DB{}
	db3 := &sql.DB{}
	slaves := []*sql.DB{db1, db2, db3}

	const goroutines = 20
	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				got := s.Pick(slaves)
				if got != db1 && got != db2 && got != db3 {
					t.Errorf("unexpected db: %v", got)
				}
			}
		}()
	}
	wg.Wait()

	// 总调用次数 = goroutines * iterations
	totalCalls := goroutines * iterations
	expectedCounter := uint64(totalCalls)
	if s.counter.Load() != expectedCounter {
		t.Errorf("counter mismatch: expected %d, got %d", expectedCounter, s.counter.Load())
	}
}
