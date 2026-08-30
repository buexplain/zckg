// 本文件为第二轮覆盖率补强的 SQLite 集成入口（纯 Go 驱动，始终可运行），
// 含 SQLite 方言特有分支：Truncate 的 sqlite_sequence 双状态清理、
// 大小写敏感 Like 的 GLOB + Expression 分支、游标列 NULL（any 字段）终止分支。
package zcdb

import (
	"context"
	"errors"
	"testing"
)

func TestCov2_SQLite_Integration(t *testing.T) {
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
			st.fn(t, openSQLiteTestDB(t))
		})
	}

	t.Run("SchemaQueryErrors", func(t *testing.T) {
		cov2TestSchemaQueryErrors(t, openSQLiteTestDB(t))
	})

	t.Run("TruncateSequenceStates", cov2TestTruncateSQLite)
	t.Run("LikeCaseSensitiveExpression", cov2TestLikeCaseSensitiveExpressionSQLite)
	t.Run("CursorByNullAnyField", cov2TestCursorByNullAnyFieldSQLite)
}

// cov2TestTruncateSQLite 覆盖 SQLite Truncate 的两种序列状态：
// 先清无 AUTOINCREMENT 的表（sqlite_sequence 不存在 → 跳过清理），
// 再清有 AUTOINCREMENT 的表（存在 → 删除序列记录，自增从头开始）。
// 顺序不可颠倒：首个用例依赖库内尚未出现 sqlite_sequence。
func cov2TestTruncateSQLite(t *testing.T) {
	dao := openSQLiteTestDB(t)
	ctx := context.Background()

	// 状态一：库内尚无 sqlite_sequence（未使用过 AUTOINCREMENT）
	cov2Exec(t, dao, `CREATE TABLE cov2_plain (id INTEGER PRIMARY KEY, name TEXT)`)
	cov2Exec(t, dao, `INSERT INTO cov2_plain (id, name) VALUES (1, 'a')`)
	if err := dao.Builder().Table("cov2_plain").Truncate(ctx); err != nil {
		t.Fatalf("Truncate 无 AUTOINCREMENT 表不应报错: %v", err)
	}
	n, err := dao.Builder().Table("cov2_plain").Count(ctx)
	if err != nil || n != 0 {
		t.Fatalf("Truncate 后应为空表，实际 %d, err=%v", n, err)
	}

	// 状态二：AUTOINCREMENT 表 → sqlite_sequence 存在，清理后自增重置
	cov2Exec(t, dao, `CREATE TABLE cov2_auto (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`)
	cov2Exec(t, dao, `INSERT INTO cov2_auto (name) VALUES ('a'), ('b')`)
	if err := dao.Builder().Table("cov2_auto").Truncate(ctx); err != nil {
		t.Fatalf("Truncate AUTOINCREMENT 表不应报错: %v", err)
	}
	cov2Exec(t, dao, `INSERT INTO cov2_auto (name) VALUES ('c')`)
	var id int64
	if err := dao.Builder().Table("cov2_auto").Select("id").Value(ctx, &id); err != nil {
		t.Fatalf("查询 Truncate 后自增值失败: %v", err)
	}
	if id != 1 {
		t.Fatalf("Truncate 后自增应重置为 1，实际 %d", id)
	}
}

// cov2TestLikeCaseSensitiveExpressionSQLite 覆盖 SQLite 大小写敏感 Like
// 退化为 GLOB 且值为 Expression 的内联分支。
func cov2TestLikeCaseSensitiveExpressionSQLite(t *testing.T) {
	dao := openSQLiteTestDB(t)
	cov2SetupScanTable(t, dao)
	ctx := context.Background()

	count, err := dao.Builder().Table("cov2_scan").
		WhereLike("name", NewExpression("'abc'"), true).Count(ctx)
	if err != nil {
		t.Fatalf("GLOB Expression 查询不应报错: %v", err)
	}
	if count != 1 {
		t.Fatalf("GLOB 'abc' 应命中 1 行，实际 %d", count)
	}
}

// cov2TestCursorByNullAnyFieldSQLite 覆盖 CursorBy 游标列值为 NULL 时
// 经 isNilValue(nil) 判定并返回 ErrCursorColumnNull 的分支：
// dest 游标字段为 any 类型，NULL 扫描后保持 nil，Interface() 得到未类型化 nil。
func cov2TestCursorByNullAnyFieldSQLite(t *testing.T) {
	dao := openSQLiteTestDB(t)
	ctx := context.Background()
	cov2Exec(t, dao, `CREATE TABLE cov2_nullcur (id INTEGER, name TEXT)`)
	cov2Exec(t, dao, `INSERT INTO cov2_nullcur (id, name) VALUES (NULL, 'x')`)

	type anyRow struct {
		ID   any    `db:"id"`
		Name string `db:"name"`
	}
	var r anyRow
	var got error
	for err := range dao.Builder().Table("cov2_nullcur").CursorBy(ctx, &r, 10, "id") {
		got = err
	}
	if !errors.Is(got, ErrCursorColumnNull) {
		t.Fatalf("游标列 NULL 应报 ErrCursorColumnNull，实际 %v", got)
	}
}
