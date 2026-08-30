// 本文件为第二轮覆盖率补强的 PostgreSQL 集成入口（不可达时自动 Skip），
// 含 PG 方言特有分支：SELECT 行锁（FOR UPDATE / FOR SHARE）、多列 uniqueBy Upsert。
package zcdb

import (
	"testing"
)

func TestCov2_PostgreSQL_Integration(t *testing.T) {
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
		{"UpsertMultiUniqueBy", cov2TestUpsertMultiUniqueBy},
		{"WhereNotInVariants", cov2TestWhereNotInVariants},
		{"NullSafeExpression", cov2TestNullSafeExpression},
		{"DeleteJoinNested", cov2TestDeleteJoinNested},
		{"DeleteJoinExecError", cov2TestDeleteJoinExecError},
		{"UpdateJoinNested", cov2TestUpdateJoinNested},
		{"TransactionBeginError", cov2TestTransactionBeginError},
	}
	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			st.fn(t, openPgTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		cov2TestSchemaQueryErrors(t, openPgTestDB(t))
	})

	t.Run("SelectLock", cov2TestSelectLockPG)
}

// cov2TestSelectLockPG 覆盖 PostgreSQL SELECT 行锁（FOR UPDATE / FOR SHARE）
// 编译分支：带锁查询可正常执行并返回结果。
func cov2TestSelectLockPG(t *testing.T) {
	dao := openPgTestDB(t)
	cov2SetupScanTable(t, dao)

	var rows []cov2Row
	if err := dao.Builder().Table("cov2_scan").OrderBy("id", "ASC").
		LockForUpdate().Find(t.Context(), &rows); err != nil {
		t.Fatalf("FOR UPDATE 查询不应报错: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("FOR UPDATE 应返回 2 行，实际 %d", len(rows))
	}

	rows = nil
	if err := dao.Builder().Table("cov2_scan").OrderBy("id", "ASC").
		SharedLock().Find(t.Context(), &rows); err != nil {
		t.Fatalf("FOR SHARE 查询不应报错: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("FOR SHARE 应返回 2 行，实际 %d", len(rows))
	}
}
