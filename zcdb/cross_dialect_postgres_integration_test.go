// 本文件为跨方言集成测试的 PostgreSQL 执行入口（不可达时自动 Skip）：
// 以统一 subtests 表运行 cross_dialect_integration_test.go 的共享用例。
// 原 PG 方言特有分支（SELECT 行锁 FOR UPDATE/FOR SHARE）已按功能归位至
// builder_order_postgres_integration_test.go。
package zcdb

import (
	"testing"
)

func TestCrossDialect_PostgreSQL_Integration(t *testing.T) {
	subtests := []struct {
		name string
		fn   func(*testing.T, *DBDao)
	}{
		{"QueryErrorsOnMissingTable", crossDialectTestQueryErrorsOnMissingTable},
		{"PluckScanErrors", crossDialectTestPluckScanErrors},
		{"CursorScanErrors", crossDialectTestCursorScanErrors},
		{"CursorByFieldNameFallback", crossDialectTestCursorByFieldNameFallback},
		{"IncrementDecrement", crossDialectTestIncrementDecrement},
		{"InsertUsing", crossDialectTestInsertUsing},
		{"BatchInsertAndExpressionValue", crossDialectTestBatchInsertAndExpressionValue},
		{"UpsertMultiUniqueBy", crossDialectTestUpsertMultiUniqueBy},
		{"WhereNotInVariants", crossDialectTestWhereNotInVariants},
		{"NullSafeExpression", crossDialectTestNullSafeExpression},
		{"DeleteJoinNested", crossDialectTestDeleteJoinNested},
		{"DeleteJoinExecError", crossDialectTestDeleteJoinExecError},
		{"UpdateJoinNested", crossDialectTestUpdateJoinNested},
		{"TransactionBeginError", crossDialectTestTransactionBeginError},
	}
	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			st.fn(t, openPgTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		crossDialectTestSchemaQueryErrors(t, openPgTestDB(t))
	})
}
