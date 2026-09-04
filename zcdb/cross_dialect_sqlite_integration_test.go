// 本文件为跨方言集成测试的 SQLite 执行入口（纯 Go 驱动，始终可运行）：
// 以统一 subtests 表运行 cross_dialect_integration_test.go 的共享用例。
// 原 SQLite 方言特有分支（Truncate 的 sqlite_sequence 双状态清理、
// 大小写敏感 Like 的 GLOB + Expression、游标列 NULL 终止）已按功能归位至
// builder_exec_sqlite_integration_test.go / builder_where_sqlite_integration_test.go
// / builder_cursor_sqlite_integration_test.go。
package zcdb

import (
	"testing"
)

func TestCrossDialect_SQLite_Integration(t *testing.T) {
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
			st.fn(t, openSQLiteTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		crossDialectTestSchemaQueryErrors(t, openSQLiteTestDB(t))
	})
}
