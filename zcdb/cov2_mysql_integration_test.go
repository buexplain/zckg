// 本文件为第二轮覆盖率补强的 MySQL 集成入口（不可达时自动 Skip），
// 含 MySQL 方言特有分支：UPDATE/DELETE 携带 ORDER BY raw 的编译、
// 从库失联时 Pool.Ping 的错误上报、Upsert 空 updateColumns 退化。
package zcdb

import (
	"context"
	"database/sql"
	"testing"
)

func TestCov2_MySQL_Integration(t *testing.T) {
	subtests := []struct {
		name string
		fn   func(*testing.T, *DBDao)
	}{
		{"QueryErrorsOnMissingTable", cov2TestQueryErrorsOnMissingTable},
		{"PluckScanErrors", cov2TestPluckScanErrors},
		{"CursorScanErrors", cov2TestCursorScanErrors},
		{"CursorByFieldNameFallback", cov2TestCursorByFieldNameFallback},
		{"IncrementDecrement", cov2TestIncrementDecrement},
		{"InsertUsing", cov2TestInsertUsing},
		{"BatchInsertAndExpressionValue", cov2TestBatchInsertAndExpressionValue},
		{"WhereNotInVariants", cov2TestWhereNotInVariants},
		{"NullSafeExpression", cov2TestNullSafeExpression},
		{"DeleteJoinNested", cov2TestDeleteJoinNested},
		{"DeleteJoinExecError", cov2TestDeleteJoinExecError},
		{"TransactionBeginError", cov2TestTransactionBeginError},
	}
	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			st.fn(t, openMySQLTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		cov2TestSchemaQueryErrors(t, openMySQLTestDB(t))
	})

	t.Run("OrderByRawInUpdateDelete", cov2TestOrderByRawInUpdateDeleteMySQL)
	t.Run("PingSlaveError", cov2TestPoolPingSlaveErrorMySQL)
	t.Run("UpsertEmptyUpdateColumnsNoop", cov2TestUpsertEmptyUpdateColumnsMySQL)
}

type cov2ObUpd struct {
	Name string `db:"name"`
}

// cov2TestOrderByRawInUpdateDeleteMySQL 覆盖 MySQL 方言 UPDATE/DELETE
// 编译中 ORDER BY 原始片段的分支（MySQL 支持 UPDATE/DELETE 带 ORDER BY + LIMIT）。
func cov2TestOrderByRawInUpdateDeleteMySQL(t *testing.T) {
	dao := openMySQLTestDB(t)
	ctx := context.Background()
	cov2Drop(t, dao, "cov2_ob")
	cov2Exec(t, dao, `CREATE TABLE cov2_ob (id INTEGER PRIMARY KEY, name TEXT)`)
	cov2Exec(t, dao, `INSERT INTO cov2_ob (id, name) VALUES (1, 'a'), (2, 'b')`)

	affected, err := dao.Builder().Table("cov2_ob").Where("id", ">", 0).
		OrderByRaw("id DESC").OrderByRaw("name ASC").Limit(1).
		Update(ctx, &cov2ObUpd{Name: "x"})
	if err != nil || affected != 1 {
		t.Fatalf("UPDATE 携带 ORDER BY raw 失败: affected=%d err=%v", affected, err)
	}

	affected, err = dao.Builder().Table("cov2_ob").Where("id", ">", 0).
		OrderByRaw("id DESC").OrderByRaw("name ASC").Limit(1).
		Delete(ctx)
	if err != nil || affected != 1 {
		t.Fatalf("DELETE 携带 ORDER BY raw 失败: affected=%d err=%v", affected, err)
	}
}

// cov2TestPoolPingSlaveErrorMySQL 覆盖 Pool.Ping 在从库失联时报错的分支：
// 直接向池内注入一个指向不可达地址的从库连接（绕过 AddSlave 的建连校验），
// 使 Ping 的从库遍历命中错误路径。
func cov2TestPoolPingSlaveErrorMySQL(t *testing.T) {
	requireMySQLAvailable(t)
	pool, err := NewPool(PoolConfig{
		DriverName: "mysql",
		DSN:        mysqlTestMasterDSN,
	})
	if err != nil {
		t.Skipf("mysql unavailable, skipping integration test: %v", err)
	}
	defer func() { _ = pool.Close() }()

	badDB, err := sql.Open("mysql", "root:root@tcp(127.0.0.1:3999)/none?timeout=1s")
	if err != nil {
		t.Fatalf("构造失联从库失败: %v", err)
	}
	pool.slaves = append(pool.slaves, badDB)

	if err := pool.Ping(context.Background()); err == nil {
		t.Fatal("从库失联时 Ping 应报错")
	}
}

type cov2UpNoopRow struct {
	A int64 `db:"a"`
	B int64 `db:"b"`
}

// cov2TestUpsertEmptyUpdateColumnsMySQL 覆盖 MySQL ON DUPLICATE KEY UPDATE
// 在 updateColumns 为空且插入列全部为 uniqueBy 时的 no-op 自赋值退化分支：
// 冲突时不更新任何列。
func cov2TestUpsertEmptyUpdateColumnsMySQL(t *testing.T) {
	dao := openMySQLTestDB(t)
	ctx := context.Background()
	cov2Drop(t, dao, "cov2_upnoop")
	cov2Exec(t, dao, `CREATE TABLE cov2_upnoop (a INTEGER, b INTEGER, UNIQUE KEY uniq_ab (a, b))`)
	cov2Exec(t, dao, `INSERT INTO cov2_upnoop (a, b) VALUES (1, 2)`)

	// 插入列全部为 uniqueBy：updateColumns 解析为空 → 退化自赋值，冲突不更新
	if _, err := dao.Builder().Table("cov2_upnoop").Upsert(ctx,
		cov2UpNoopRow{A: 1, B: 2}, []string{"a", "b"}, nil); err != nil {
		t.Fatalf("空 updateColumns no-op Upsert 不应报错: %v", err)
	}
	n, err := dao.Builder().Table("cov2_upnoop").Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("no-op Upsert 后应仍为 1 行，实际 %d, err=%v", n, err)
	}
}
