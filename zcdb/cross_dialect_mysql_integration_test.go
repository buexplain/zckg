// 本文件为跨方言集成测试的 MySQL 执行入口（不可达时自动 Skip）：
// 以统一 subtests 表运行 cross_dialect_integration_test.go 的共享用例。
// 原 MySQL 方言特有分支（UPDATE/DELETE 带 ORDER BY raw、Pool.Ping 从库失联、
// Upsert 空 updateColumns 退化）已按功能归位至 builder_exec_mysql_integration_test.go
// 与 builder_mysql_integration_test.go。
package zcdb

import (
	"testing"
)

// TestCrossDialect_MySQL_SQLComment 注册 C07–C10 共享执行不变量。
// DSN: root:root@tcp(127.0.0.1:3306)/zckg_test_integ?charset=utf8mb4&parseTime=true&loc=Local；不可达 Skip。
// docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
func TestCrossDialect_MySQL_SQLComment(t *testing.T) {
	runCrossDialectSQLComment(t, openMySQLCommentDAO)
}

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
			st.fn(t, openMySQLTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		crossDialectTestSchemaQueryErrors(t, openMySQLTestDB(t))
	})
}
