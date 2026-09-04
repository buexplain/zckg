// 本文件为跨方言集成测试公共主体：
// 各方言入口文件（cross_dialect_{sqlite,mysql,postgres}_integration_test.go）负责
// 建连与门控，测试逻辑集中于此，三方言复用同一套断言。
// 覆盖目标：终端方法的执行期错误分支、Pluck/Cursor 扫描错误、
// Increment/Decrement、InsertUsing/InsertOrIgnoreUsing、批量插入、
// Upsert 退化分支、NOT IN 系列、空安全表达式、嵌套 Join 删除、事务开启失败等。
package zcdb

import (
	"context"
	"errors"
	"testing"
)

const crossDialectMissingTable = "cross_dialect_missing_table_xyz"

// crossDialectExec 执行 DDL/DML，失败则 Fatal。
func crossDialectExec(t *testing.T, dao *DBDao, query string, args ...any) {
	t.Helper()
	if _, err := dao.Exec(context.Background(), query, args...); err != nil {
		t.Fatalf("exec failed: %s\nerror: %v", query, err)
	}
}

// crossDialectDrop 清理上一轮运行残留的表（MySQL/PostgreSQL 为持久库），三方言通用。
func crossDialectDrop(t *testing.T, dao *DBDao, tables ...string) {
	t.Helper()
	for _, tb := range tables {
		if _, err := dao.Exec(context.Background(), "DROP TABLE IF EXISTS "+tb); err != nil {
			t.Fatalf("drop table %s failed: %v", tb, err)
		}
	}
}

type crossDialectRow struct {
	ID int64 `db:"id"`
}

type crossDialectItemRow struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
	Num  int64  `db:"num"`
}

// crossDialectSetupScanTable 创建 cross_dialect_scan 表并插入两行（name 为文本列，用于类型不匹配扫描错误）。
func crossDialectSetupScanTable(t *testing.T, dao *DBDao) {
	t.Helper()
	crossDialectDrop(t, dao, "cross_dialect_scan")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_scan (id INTEGER PRIMARY KEY, name TEXT, num INTEGER)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_scan (id, name, num) VALUES (1, 'abc', 10), (2, 'def', 20)`)
}

// crossDialectTestQueryErrorsOnMissingTable 覆盖各终端方法在执行期遇到不存在表时
// 把数据库错误上报给调用方的分支（query/exec 错误路径）。
func crossDialectTestQueryErrorsOnMissingTable(t *testing.T, dao *DBDao) {
	ctx := context.Background()

	var r crossDialectRow
	if err := dao.Builder().Table(crossDialectMissingTable).First(ctx, &r); err == nil {
		t.Fatal("First 查询不存在的表应报错")
	}
	var rows []crossDialectRow
	if err := dao.Builder().Table(crossDialectMissingTable).Find(ctx, &rows); err == nil {
		t.Fatal("Find 查询不存在的表应报错")
	}
	var ids []int64
	if err := dao.Builder().Table(crossDialectMissingTable).Pluck(ctx, &ids, "id"); err == nil {
		t.Fatal("Pluck 查询不存在的表应报错")
	}
	keyBy := map[int64]crossDialectRow{}
	if err := dao.Builder().Table(crossDialectMissingTable).Pluck(ctx, &keyBy, "id"); err == nil {
		t.Fatal("Pluck keyBy 查询不存在的表应报错")
	}
	if _, err := dao.Builder().Table(crossDialectMissingTable).Count(ctx); err == nil {
		t.Fatal("Count 查询不存在的表应报错")
	}
	if _, err := dao.Builder().Table(crossDialectMissingTable).Paginate(ctx, &rows); err == nil {
		t.Fatal("Paginate 查询不存在的表应报错")
	}
	if _, err := dao.Builder().Table(crossDialectMissingTable).Exists(ctx); err == nil {
		t.Fatal("Exists 查询不存在的表应报错")
	}
	if _, err := dao.Builder().Table(crossDialectMissingTable).Max(ctx, "id"); err == nil {
		t.Fatal("Max 查询不存在的表应报错")
	}
	var v int64
	if err := dao.Builder().Table(crossDialectMissingTable).Select("id").Value(ctx, &v); err == nil {
		t.Fatal("Value 查询不存在的表应报错")
	}
	var curErr error
	for err := range dao.Builder().Table(crossDialectMissingTable).Cursor(ctx, &r) {
		curErr = err
	}
	if curErr == nil {
		t.Fatal("Cursor 查询不存在的表应报错")
	}
	curErr = nil
	for err := range dao.Builder().Table(crossDialectMissingTable).CursorBy(ctx, &r, 10, "id") {
		curErr = err
	}
	if curErr == nil {
		t.Fatal("CursorBy 查询不存在的表应报错")
	}
}

// crossDialectTestPluckScanErrors 覆盖 Pluck 三种模式下扫描类型不匹配的错误分支：
// 切片模式、map 键值模式、map 键列（keyBy）模式。
func crossDialectTestPluckScanErrors(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectSetupScanTable(t, dao)

	// 切片模式：文本列 'abc' 扫描进 int64 失败
	var ids []int64
	if err := dao.Builder().Table("cross_dialect_scan").Pluck(ctx, &ids, "name"); err == nil {
		t.Fatal("Pluck 切片模式扫描文本列到 int64 应报错")
	}

	// map 键值模式：值列（第一列）类型不匹配
	m := map[string]int64{}
	if err := dao.Builder().Table("cross_dialect_scan").Pluck(ctx, &m, "name", "id"); err == nil {
		t.Fatal("Pluck map 模式扫描文本列到 int64 应报错")
	}

	// keyBy 模式：结构体字段类型与列类型不匹配
	type badKeyBy struct {
		Name int64 `db:"name"`
	}
	kb := map[int64]badKeyBy{}
	if err := dao.Builder().Table("cross_dialect_scan").Pluck(ctx, &kb, "id"); err == nil {
		t.Fatal("Pluck keyBy 模式扫描文本列到 int64 应报错")
	}
}

type crossDialectBadScanRow struct {
	ID   int64 `db:"id"`
	Name int64 `db:"name"`
}

// crossDialectTestCursorScanErrors 覆盖 Cursor 与 CursorBy 的行扫描错误分支。
func crossDialectTestCursorScanErrors(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectSetupScanTable(t, dao)

	var r crossDialectBadScanRow
	var got error
	for err := range dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").Cursor(ctx, &r) {
		got = err
	}
	if got == nil {
		t.Fatal("Cursor 扫描类型不匹配应报错")
	}

	got = nil
	for err := range dao.Builder().Table("cross_dialect_scan").CursorBy(ctx, &r, 10, "id") {
		got = err
	}
	if got == nil {
		t.Fatal("CursorBy 扫描类型不匹配应报错")
	}
}

// crossDialectTestCursorByFieldNameFallback 覆盖 CursorBy 游标列按字段名兜底匹配的分支：
// db 标签映射查不到时，退化为结构体字段名精确匹配。
func crossDialectTestCursorByFieldNameFallback(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectSetupScanTable(t, dao)

	// RowID 的 db 标签为 id：toSnakeCase("RowID")="row_id" 查不到，
	// 字段名兜底匹配成功；但 SQL 中 "RowID" 不是真实列名，查询应报驱动错误
	// （而非 ErrCursorFieldNotFound）
	type fbRow struct {
		RowID int64 `db:"id"`
	}
	var r fbRow
	var got error
	count := 0
	for err := range dao.Builder().Table("cross_dialect_scan").CursorBy(ctx, &r, 10, "RowID") {
		got = err
		count++
	}
	if count == 0 {
		t.Fatal("字段名兜底匹配后应继续执行查询")
	}
	if errors.Is(got, ErrCursorFieldNotFound) {
		t.Fatalf("字段名兜底匹配不应再报 ErrCursorFieldNotFound: %v", got)
	}
}

// crossDialectTestIncrementDecrement 覆盖 Increment/Decrement 的正常路径
// （含多列 IncrementEach）与执行期错误分支。
func crossDialectTestIncrementDecrement(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_counter")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_counter (id INTEGER PRIMARY KEY, wallet INTEGER, level INTEGER)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_counter (id, wallet, level) VALUES (1, 100, 1)`)

	affected, err := dao.Builder().Table("cross_dialect_counter").Where("id", "=", 1).
		Increment(ctx, "wallet", 50, "level", 2)
	if err != nil || affected != 1 {
		t.Fatalf("Increment 多列自增失败: affected=%d err=%v", affected, err)
	}
	var wallet int64
	if err := dao.Builder().Table("cross_dialect_counter").Select("wallet").Where("id", "=", 1).Value(ctx, &wallet); err != nil {
		t.Fatalf("查询自增结果失败: %v", err)
	}
	if wallet != 150 {
		t.Fatalf("wallet 应为 150，实际 %d", wallet)
	}

	affected, err = dao.Builder().Table("cross_dialect_counter").Where("id", "=", 1).Decrement(ctx, "wallet", 30)
	if err != nil || affected != 1 {
		t.Fatalf("Decrement 失败: affected=%d err=%v", affected, err)
	}
	if err := dao.Builder().Table("cross_dialect_counter").Select("wallet").Where("id", "=", 1).Value(ctx, &wallet); err != nil {
		t.Fatalf("查询自减结果失败: %v", err)
	}
	if wallet != 120 {
		t.Fatalf("wallet 应为 120，实际 %d", wallet)
	}

	if _, err := dao.Builder().Table(crossDialectMissingTable).Where("id", "=", 1).Increment(ctx, "wallet", 1); err == nil {
		t.Fatal("Increment 不存在的表应报错")
	}
	if _, err := dao.Builder().Table(crossDialectMissingTable).Where("id", "=", 1).Decrement(ctx, "wallet", 1); err == nil {
		t.Fatal("Decrement 不存在的表应报错")
	}
}

// crossDialectTestInsertUsing 覆盖 InsertUsing/InsertOrIgnoreUsing 的正常路径与执行期错误分支。
func crossDialectTestInsertUsing(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_dst", "cross_dialect_src")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_src (id INTEGER PRIMARY KEY, name TEXT)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_src (id, name) VALUES (1, 'a'), (2, 'b')`)
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_dst (name TEXT)`)

	affected, err := dao.Builder().Table("cross_dialect_dst").InsertUsing(ctx, []string{"name"}, func(sub *Builder) {
		sub.Table("cross_dialect_src").Select("name")
	})
	if err != nil || affected != 2 {
		t.Fatalf("InsertUsing 失败: affected=%d err=%v", affected, err)
	}
	n, err := dao.Builder().Table("cross_dialect_dst").Count(ctx)
	if err != nil || n != 2 {
		t.Fatalf("InsertUsing 后行数应为 2，实际 %d, err=%v", n, err)
	}

	affected, err = dao.Builder().Table("cross_dialect_dst").InsertOrIgnoreUsing(ctx, []string{"name"}, func(sub *Builder) {
		sub.Table("cross_dialect_src").Select("name")
	})
	if err != nil || affected != 2 {
		t.Fatalf("InsertOrIgnoreUsing 失败: affected=%d err=%v", affected, err)
	}

	if _, err := dao.Builder().Table(crossDialectMissingTable).InsertUsing(ctx, []string{"name"}, func(sub *Builder) {
		sub.Table("cross_dialect_src").Select("name")
	}); err == nil {
		t.Fatal("InsertUsing 目标表不存在应报错")
	}
	if _, err := dao.Builder().Table(crossDialectMissingTable).InsertOrIgnoreUsing(ctx, []string{"name"}, func(sub *Builder) {
		sub.Table("cross_dialect_src").Select("name")
	}); err == nil {
		t.Fatal("InsertOrIgnoreUsing 目标表不存在应报错")
	}
}

// crossDialectTestBatchInsertAndExpressionValue 覆盖多行批量插入（VALUES 分隔符分支）
// 与 struct 字段值为 Expression 时的内联编译分支。
func crossDialectTestBatchInsertAndExpressionValue(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_batch")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_batch (id INTEGER PRIMARY KEY, name TEXT, num INTEGER)`)

	affected, err := dao.Builder().Table("cross_dialect_batch").Insert(ctx, []crossDialectItemRow{
		{ID: 1, Name: "x", Num: 1},
		{ID: 2, Name: "y", Num: 2},
	})
	if err != nil || affected != 2 {
		t.Fatalf("批量插入失败: affected=%d err=%v", affected, err)
	}

	// Expression 值内联：name = UPPER('abc')
	type exprRow struct {
		ID   int64 `db:"id"`
		Name any   `db:"name"`
	}
	affected, err = dao.Builder().Table("cross_dialect_batch").Insert(ctx, exprRow{ID: 9, Name: NewExpression("UPPER('abc')")})
	if err != nil || affected != 1 {
		t.Fatalf("Expression 值插入失败: affected=%d err=%v", affected, err)
	}
	var name string
	if err := dao.Builder().Table("cross_dialect_batch").Select("name").Where("id", "=", 9).Value(ctx, &name); err != nil {
		t.Fatalf("查询 Expression 插入结果失败: %v", err)
	}
	if name != "ABC" {
		t.Fatalf("Expression 插入应得到 ABC，实际 %q", name)
	}
}

type crossDialectUp2Row struct {
	A int64  `db:"a"`
	B int64  `db:"b"`
	V string `db:"v"`
}

// crossDialectTestUpsertMultiUniqueBy 覆盖多列 uniqueBy 编译时的分隔符分支。
func crossDialectTestUpsertMultiUniqueBy(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_up2")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_up2 (a INTEGER, b INTEGER, v TEXT)`)
	crossDialectExec(t, dao, `CREATE UNIQUE INDEX cross_dialect_up2_ab ON cross_dialect_up2(a, b)`)

	_, err := dao.Builder().Table("cross_dialect_up2").Upsert(ctx,
		crossDialectUp2Row{A: 1, B: 2, V: "v1"},
		[]string{"a", "b"}, []string{"v"})
	if err != nil {
		t.Fatalf("多列 uniqueBy Upsert 插入不应报错: %v", err)
	}
	_, err = dao.Builder().Table("cross_dialect_up2").Upsert(ctx,
		crossDialectUp2Row{A: 1, B: 2, V: "v2"},
		[]string{"a", "b"}, []string{"v"})
	if err != nil {
		t.Fatalf("多列 uniqueBy Upsert 冲突更新不应报错: %v", err)
	}
	var v string
	if err := dao.Builder().Table("cross_dialect_up2").Select("v").Where("a", "=", 1).Where("b", "=", 2).Value(ctx, &v); err != nil {
		t.Fatalf("查询 Upsert 结果失败: %v", err)
	}
	if v != "v2" {
		t.Fatalf("冲突更新后 v 应为 v2，实际 %q", v)
	}
}

// crossDialectTestWhereNotInVariants 覆盖 NOT IN 列表/空列表/子查询与 NOT 嵌套条件分支。
func crossDialectTestWhereNotInVariants(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectSetupScanTable(t, dao)
	crossDialectDrop(t, dao, "cross_dialect_src2")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_src2 (id INTEGER PRIMARY KEY)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_src2 (id) VALUES (1)`)

	var ids []int64
	if err := dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").
		WhereNotIn("id", []any{1, 2}).Pluck(ctx, &ids, "id"); err != nil {
		t.Fatalf("NOT IN 列表查询不应报错: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("NOT IN (1,2) 应无结果，实际 %v", ids)
	}

	// 空列表 NOT IN 恒真（1=1）：返回全部行
	ids = nil
	if err := dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").
		WhereNotIn("id", []any{}).Pluck(ctx, &ids, "id"); err != nil {
		t.Fatalf("空 NOT IN 查询不应报错: %v", err)
	}
	if len(ids) != 2 {
		t.Fatalf("空 NOT IN 应返回全部 2 行，实际 %v", ids)
	}

	// NOT IN 子查询
	ids = nil
	if err := dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").
		WhereNotInSub("id", func(q *Builder) { q.Table("cross_dialect_src2").Select("id") }).
		Pluck(ctx, &ids, "id"); err != nil {
		t.Fatalf("NOT IN 子查询不应报错: %v", err)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("NOT IN 子查询应只剩 id=2，实际 %v", ids)
	}

	// NOT 嵌套条件
	ids = nil
	if err := dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").
		WhereNot(func(q *Builder) { q.Where("id", "=", 1) }).
		Pluck(ctx, &ids, "id"); err != nil {
		t.Fatalf("NOT 嵌套条件查询不应报错: %v", err)
	}
	if len(ids) != 1 || ids[0] != 2 {
		t.Fatalf("NOT 嵌套条件应只剩 id=2，实际 %v", ids)
	}
}

// crossDialectTestNullSafeExpression 覆盖空安全比较传入 Expression 值时的内联编译分支。
func crossDialectTestNullSafeExpression(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectSetupScanTable(t, dao)

	var n int
	count, err := dao.Builder().Table("cross_dialect_scan").
		WhereNullSafeEquals("name", NewExpression("name")).Count(ctx)
	if err != nil {
		t.Fatalf("空安全相等（Expression）查询不应报错: %v", err)
	}
	n = int(count)
	if n != 2 {
		t.Fatalf("name <=> name 应命中全部 2 行，实际 %d", n)
	}

	count, err = dao.Builder().Table("cross_dialect_scan").
		WhereNullSafeNotEquals("name", NewExpression("name")).Count(ctx)
	if err != nil {
		t.Fatalf("空安全不等（Expression）查询不应报错: %v", err)
	}
	if count != 0 {
		t.Fatalf("name 空安全不等 name 应命中 0 行，实际 %d", count)
	}
}

// crossDialectTestDeleteJoinNested 覆盖嵌套 Join（join 内再 join）的 DeleteJoin：
// 方言编译需展平嵌套 join 表并递归编译嵌套 join 条件。
func crossDialectTestDeleteJoinNested(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_x", "cross_dialect_o", "cross_dialect_u")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_u (id INTEGER PRIMARY KEY, name TEXT)`)
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_o (id INTEGER PRIMARY KEY, user_id INTEGER, status TEXT)`)
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_x (o_id INTEGER)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_u (id, name) VALUES (1, 'a'), (2, 'b')`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_o (id, user_id, status) VALUES (10, 1, 'cancelled'), (11, 2, 'ok')`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_x (o_id) VALUES (10)`)

	affected, err := dao.Builder().Table("cross_dialect_u").
		JoinOn("cross_dialect_o", func(j *JoinBuilder) {
			j.On("cross_dialect_o.user_id", "=", "cross_dialect_u.id").
				JoinOn("cross_dialect_x", func(x *JoinBuilder) {
					x.On("cross_dialect_x.o_id", "=", "cross_dialect_o.id")
				})
		}).
		Where("cross_dialect_o.status", "=", "cancelled").
		DeleteJoin(ctx)
	if err != nil || affected != 1 {
		t.Fatalf("嵌套 Join DeleteJoin 失败: affected=%d err=%v", affected, err)
	}
	n, err := dao.Builder().Table("cross_dialect_u").Count(ctx)
	if err != nil || n != 1 {
		t.Fatalf("DeleteJoin 后应剩 1 行，实际 %d, err=%v", n, err)
	}
}

// crossDialectTestTransactionBeginError 覆盖已取消 ctx 下开启事务失败并上报的分支。
func crossDialectTestTransactionBeginError(t *testing.T, dao *DBDao) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := dao.Transaction(ctx, func(context.Context) error { return nil })
	if err == nil {
		t.Fatal("已取消的 ctx 开启事务应报错")
	}
}

// crossDialectTestSchemaQueryErrors 覆盖 Schema 检查器 Tables/Columns 的查询错误分支：
// 关闭连接后查询必然失败。使用独立连接，避免影响其它测试。
func crossDialectTestSchemaQueryErrors(t *testing.T, dao *DBDao) {
	insp, err := dao.Schema()
	if err != nil {
		t.Fatalf("获取 Schema 检查器失败: %v", err)
	}
	_ = dao.Close()

	ctx := context.Background()
	if _, err := insp.Tables(ctx); err == nil {
		t.Fatal("连接关闭后 Tables 应报错")
	}
	if _, err := insp.Columns(ctx, "users"); err == nil {
		t.Fatal("连接关闭后 Columns 应报错")
	}
}

type crossDialectUUpd struct {
	Name string `db:"name"`
}

// crossDialectTestUpdateJoinNested 覆盖 PG/SQLite 方言 Update 的 FROM 展平递归分支：
// 更新带嵌套 join（join 内再 join）时，FROM 子句需递归展平嵌套 join 组。
func crossDialectTestUpdateJoinNested(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_x", "cross_dialect_o", "cross_dialect_u")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_u (id INTEGER PRIMARY KEY, name TEXT)`)
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_o (id INTEGER PRIMARY KEY, user_id INTEGER, status TEXT)`)
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_x (o_id INTEGER)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_u (id, name) VALUES (1, 'a'), (2, 'b')`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_o (id, user_id, status) VALUES (10, 1, 'cancelled'), (11, 2, 'ok')`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_x (o_id) VALUES (10)`)

	affected, err := dao.Builder().Table("cross_dialect_u").
		JoinOn("cross_dialect_o", func(j *JoinBuilder) {
			j.On("cross_dialect_o.user_id", "=", "cross_dialect_u.id").
				JoinOn("cross_dialect_x", func(x *JoinBuilder) {
					x.On("cross_dialect_x.o_id", "=", "cross_dialect_o.id")
				})
		}).
		Where("cross_dialect_o.status", "=", "cancelled").
		Update(ctx, &crossDialectUUpd{Name: "z"})
	if err != nil || affected != 1 {
		t.Fatalf("Update 带嵌套 join 失败: affected=%d err=%v", affected, err)
	}
	var name string
	if err := dao.Builder().Table("cross_dialect_u").Select("name").Where("id", "=", 1).Value(ctx, &name); err != nil {
		t.Fatalf("查询 Update 结果失败: %v", err)
	}
	if name != "z" {
		t.Fatalf("Update 应命中 user_id=1 的行，实际 name=%q", name)
	}
}

// crossDialectTestDeleteJoinExecError 覆盖 DeleteJoin 执行期错误分支：
// join 与 where 合法（编译通过），但目标表不存在，执行时报错。
func crossDialectTestDeleteJoinExecError(t *testing.T, dao *DBDao) {
	ctx := context.Background()
	crossDialectDrop(t, dao, "cross_dialect_other")
	crossDialectExec(t, dao, `CREATE TABLE cross_dialect_other (id INTEGER PRIMARY KEY, user_id INTEGER)`)
	crossDialectExec(t, dao, `INSERT INTO cross_dialect_other (id, user_id) VALUES (1, 1)`)

	_, err := dao.Builder().Table(crossDialectMissingTable).
		JoinOn("cross_dialect_other", func(j *JoinBuilder) {
			j.On("cross_dialect_other.user_id", "=", crossDialectMissingTable+".id")
		}).
		Where("cross_dialect_other.user_id", "=", 1).
		DeleteJoin(ctx)
	if err == nil {
		t.Fatal("DeleteJoin 目标表不存在应报错")
	}
}
