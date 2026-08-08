// 本文件为 PostgreSQL 集成测试——SQL 编译（ToXxx 系列）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	"fmt"
	_ "github.com/lib/pq"
	"sync"
	"testing"
)

// TestPgInteg_ConcurrentCompile 验证多 goroutine 并发执行查询时 $N 占位符编号正确。
func TestPgInteg_ConcurrentCompile(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	const n = 20
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			type row struct {
				Name string `db:"name"`
			}
			var rows []row
			// 多条件查询，$N 编号必须从 $1 开始递增
			err := db.Builder().Table("users").
				Select("name").
				Where("status", "=", "active").
				Where("age", ">", 25).
				OrderBy("name", "ASC").
				Find(context.Background(), &rows)
			if err != nil {
				errCh <- fmt.Errorf("goroutine[%d]: %v", idx, err)
				return
			}
			// active 且 age>25: bob(30), diana(28) → 2 条
			if len(rows) != 2 {
				errCh <- fmt.Errorf("goroutine[%d]: expected 2 rows, got %d", idx, len(rows))
				return
			}
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("并发查询失败: %v", err)
	}
}
