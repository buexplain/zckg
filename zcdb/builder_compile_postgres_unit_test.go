// 本文件为 PostgreSQL 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"sync"
	"testing"
)

// TestBug_PgParamCountRace 验证 PostgresGrammar 并发编译时参数计数器不互相干扰。
func TestBug_PgParamCountRace(t *testing.T) {
	g := NewPostgresGrammar()
	const n = 50
	results := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			b := NewBuilder(g, nil).Table("users").Where("id", "=", 1)
			sql, _, _ := b.ToSelect()
			results[idx] = sql
		}(i)
	}
	wg.Wait()

	expected := `SELECT * FROM "users" WHERE "id" = $1`
	for i, sql := range results {
		if sql != expected {
			t.Errorf("并发编译结果[%d] 不正确:\n  expected: %s\n  got:      %s", i, expected, sql)
			return
		}
	}
}
