// 本文件为跨方言集成测试的 MySQL 执行入口（不可达时自动 Skip）：
// 以统一 subtests 表运行 cross_dialect_integration_test.go 的共享用例。
// 原 MySQL 方言特有分支（UPDATE/DELETE 带 ORDER BY raw、Pool.Ping 从库失联、
// Upsert 空 updateColumns 退化）已按功能归位至 builder_exec_mysql_integration_test.go
// 与 builder_mysql_integration_test.go。
package zcdb

import (
	"testing"
)

func TestCrossDialect_MySQL_Integration(t *testing.T) {
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
		{"WhereNotInVariants", crossDialectTestWhereNotInVariants},
		{"NullSafeExpression", crossDialectTestNullSafeExpression},
		{"DeleteJoinNested", crossDialectTestDeleteJoinNested},
		{"DeleteJoinExecError", crossDialectTestDeleteJoinExecError},
		{"TransactionBeginError", crossDialectTestTransactionBeginError},
	}
	for _, st := range subtests {
		t.Run(st.name, func(t *testing.T) {
			st.fn(t, openMySQLTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		crossDialectTestSchemaQueryErrors(t, openMySQLTestDB(t))
	})
}
