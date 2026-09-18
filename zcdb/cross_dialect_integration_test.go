// 本文件为跨方言集成测试公共主体：
// 各方言入口文件（cross_dialect_{sqlite,mysql,postgres}_integration_test.go）负责
// 建连与门控，测试逻辑集中于此，三方言复用同一套断言。
// 覆盖目标：终端方法的执行期错误分支、Pluck/Cursor 扫描错误、
// Increment/Decrement、InsertUsing/InsertOrIgnoreUsing、批量插入、
// Upsert 退化分支、NOT IN 系列、空安全表达式、嵌套 Join 删除、事务开启失败等。
package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

const crossDialectMissingTable = "cross_dialect_missing_table_xyz"

const integrationSQLComment = "app:comment"

type commentDAOOpener func(*testing.T, string, SlowSQLCallback) *DBDao

type commentSQLCall struct {
	sql  string
	args []any
	tx   *sql.Tx
}

type commentSQLLog struct {
	calls []commentSQLCall
}

// collect 仅复制观测值，不在 DAO 会 recover 的回调内断言。
func (l *commentSQLLog) collect(ctx context.Context, _ time.Duration, query string, args []any) {
	l.calls = append(l.calls, commentSQLCall{query, append([]any(nil), args...), txFromCtx(ctx)})
}

// check 比较完整执行 SQL、参数值与类型以及调用次数，然后清空观测窗口。
func (l *commentSQLLog) check(t *testing.T, want ...commentSQLCall) {
	t.Helper()
	defer func() { l.calls = nil }()
	if len(l.calls) != len(want) {
		t.Fatalf("SQL callback count: got %d, want %d; calls=%#v", len(l.calls), len(want), l.calls)
	}
	for i, call := range l.calls {
		if call.sql != want[i].sql || !reflect.DeepEqual(call.args, append([]any(nil), want[i].args...)) {
			t.Errorf("callback[%d]: got SQL %q args %#v; want SQL %q args %#v", i, call.sql, call.args, want[i].sql, want[i].args)
		}
	}
}

// commentCompiledCall 以显式清空注释的编译产物为精确基线；参数还须独立匹配用例常量。
// 执行入口必须只添加一次最终后缀；默认/覆盖/清空的生命周期由调用者选择 suffix。
func commentCompiledCall(t *testing.T, b *Builder, suffix string, compile func(*Builder) (string, []any, error), args ...any) commentSQLCall {
	t.Helper()
	query, bindings, err := compile(b.Clone().Comment(""))
	if err != nil {
		t.Fatalf("compile uncommented baseline: %v", err)
	}
	if !reflect.DeepEqual(append([]any(nil), bindings...), append([]any(nil), args...)) {
		t.Fatalf("baseline bindings: got %#v, want %#v", bindings, args)
	}
	if suffix != "" {
		query += " /* " + suffix + " */"
	}
	return commentSQLCall{sql: query, args: args}
}

// runCrossDialectSQLComment 将 opener 交给各子组，每个独立用例自行创建 DAO、日志和所需隔离表。
// 连续写入和 Raw/Schema 各为一个完整流程；所有用例串行执行，不争用持久库表。
func runCrossDialectSQLComment(t *testing.T, open commentDAOOpener) {
	t.Helper()
	for _, tc := range []struct {
		name string
		run  func(*testing.T, commentDAOOpener)
	}{
		{"Writes", crossDialectCommentWrites},
		{"QueriesAndDerived", crossDialectCommentQueries},
		{"Cursors", crossDialectCommentCursors},
		{"ErrorsAndGuards", crossDialectCommentErrors},
		{"RawAndSchema", crossDialectCommentRawAndSchema},
		{"ConstantSelectSafety", crossDialectCommentSafety},
		{"PrimaryAndTransactions", crossDialectCommentTransactions},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.run(t, open)
		})
	}
}

// crossDialectCommentWrites 覆盖共享写终端、完整回调和真实读回，忽略冲突不得改写原行。
func crossDialectCommentWrites(t *testing.T, open commentDAOOpener) {
	t.Helper()
	log := &commentSQLLog{}
	dao := open(t, integrationSQLComment, log.collect)
	ctx := context.Background()
	const table = "comment_exec_items"
	setupCommentTable(t, dao, table)
	setupCommentTable(t, dao, "comment_exec_copy")
	log.calls = nil
	rows := []crossDialectItemRow{{ID: 1, Name: "one", Num: 10}, {ID: 2, Name: "two", Num: 20}}
	b := dao.Builder().Table(table)
	want := commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToInsert(rows) }, int64(1), "one", int64(10), int64(2), "two", int64(20))
	if n, err := b.Insert(ctx, rows); err != nil || n != 2 {
		t.Fatalf("Insert: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	assertCommentRows(t, dao, table, rows)
	log.calls = nil

	duplicate := crossDialectItemRow{ID: 1, Name: "ignored", Num: 99}
	want = commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToInsertOrIgnore(duplicate) }, int64(1), "ignored", int64(99))
	if n, err := b.InsertOrIgnore(ctx, duplicate); err != nil || n != 0 {
		t.Fatalf("InsertOrIgnore: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	assertCommentRows(t, dao, table, rows)
	log.calls = nil

	updated := crossDialectItemRow{ID: 1, Name: "upserted", Num: 30}
	want = commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) {
		return q.ToUpsert(updated, []string{"id"}, []string{"name", "num"})
	}, int64(1), "upserted", int64(30))
	wantAffected := int64(1)
	if _, mysql := dao.grammar.(*MySQLGrammar); mysql {
		wantAffected = 2 // MySQL 冲突更新计为两行，不能断言跨方言一致。
	}
	if n, err := b.Upsert(ctx, updated, []string{"id"}, []string{"name", "num"}); err != nil || n != wantAffected {
		t.Fatalf("Upsert: affected=%d want=%d err=%v", n, wantAffected, err)
	}
	log.check(t, want)
	rows[0] = updated
	assertCommentRows(t, dao, table, rows)
	log.calls = nil

	b = dao.Builder().Table(table).Where("id", "=", 1)
	patch := crossDialectUUpd{Name: "updated"}
	want = commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToUpdate(patch) }, "updated", 1)
	if n, err := b.Update(ctx, patch); err != nil || n != 1 {
		t.Fatalf("Update: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	rows[0].Name = "updated"
	assertCommentRows(t, dao, table, rows)
	log.calls = nil
	for _, tc := range []struct {
		name    string
		sign    int64
		compile func(*Builder) (string, []any, error)
		exec    func() (int64, error)
	}{
		{"Increment", 1, func(q *Builder) (string, []any, error) { return q.ToIncrement([]string{"num"}, []any{5}) }, func() (int64, error) { return b.Increment(ctx, "num", 5) }},
		{"Decrement", -1, func(q *Builder) (string, []any, error) { return q.ToDecrement([]string{"num"}, []any{5}) }, func() (int64, error) { return b.Decrement(ctx, "num", 5) }},
	} {
		want = commentCompiledCall(t, b, integrationSQLComment, tc.compile, 5, 1)
		if n, err := tc.exec(); err != nil || n != 1 {
			t.Fatalf("%s: affected=%d err=%v", tc.name, n, err)
		}
		log.check(t, want)
		rows[0].Num += tc.sign * 5
		assertCommentRows(t, dao, table, rows)
		log.calls = nil
	}

	copyBuilder := dao.Builder().Table("comment_exec_copy")
	source := func(q *Builder) {
		q.Table(table).Select("id", "name", "num").Where("id", ">", 0).Comment("inner:must-not-appear")
	}
	columns := []string{"id", "name", "num"}
	want = commentCompiledCall(t, copyBuilder, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToInsertUsing(columns, source) }, 0)
	if n, err := copyBuilder.InsertUsing(ctx, columns, source); err != nil || n != 2 {
		t.Fatalf("InsertUsing: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	assertCommentRows(t, dao, "comment_exec_copy", rows)
	log.calls = nil
	want = commentCompiledCall(t, copyBuilder, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToInsertOrIgnoreUsing(columns, source) }, 0)
	if n, err := copyBuilder.InsertOrIgnoreUsing(ctx, columns, source); err != nil || n != 0 {
		t.Fatalf("InsertOrIgnoreUsing: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	assertCommentRows(t, dao, "comment_exec_copy", rows)
	log.calls = nil

	join := copyBuilder.Join(table, table+".id", "=", "comment_exec_copy.id").Where(table+".id", "=", 1)
	want = commentCompiledCall(t, join, integrationSQLComment, (*Builder).ToDeleteJoin, 1)
	if n, err := join.DeleteJoin(ctx); err != nil || n != 1 {
		t.Fatalf("DeleteJoin: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	assertCommentRows(t, dao, "comment_exec_copy", rows[1:])
	log.calls = nil
	want = commentCompiledCall(t, b, integrationSQLComment, (*Builder).ToDelete, 1)
	if n, err := b.Delete(ctx); err != nil || n != 1 {
		t.Fatalf("Delete: affected=%d err=%v", n, err)
	}
	log.check(t, want)
	assertCommentRows(t, dao, table, rows[1:])
}

// assertCommentRows 独立读回全部字段，不仅验证 affected 或行数。
func assertCommentRows(t *testing.T, dao *DBDao, table string, want []crossDialectItemRow) {
	t.Helper()
	var got []crossDialectItemRow
	if err := dao.Builder().Table(table).OrderBy("id").Find(context.Background(), &got); err != nil {
		t.Fatalf("read back %s: %v", table, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("read back %s: got %#v, want %#v", table, got, want)
	}
}

// crossDialectCommentQueries 每种注释状态独立建连，验证派生查询保留默认、覆盖和显式清空；空分页只有 COUNT。
// 聚合查询使用独立夹具执行完整流程，不依赖任一注释状态用例。
func crossDialectCommentQueries(t *testing.T, open commentDAOOpener) {
	t.Helper()
	ctx := context.Background()
	const table = "comment_query_items"
	for _, state := range []struct{ name, text string }{{"default", integrationSQLComment}, {"override", "query:override"}, {"clear", ""}} {
		t.Run(state.name, func(t *testing.T) {
			log := &commentSQLLog{}
			dao := open(t, integrationSQLComment, log.collect)
			setupCommentTable(t, dao, table)
			mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10), (2, 'two', 20), (3, 'three', 30)")
			log.calls = nil
			base := dao.Builder().Table(table).Where("id", ">", 0).OrderBy("id")
			if state.name != "default" {
				base.Comment(state.text)
			}
			b := base.Clone()
			want := commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) { return q.Limit(1).ToSelect() }, 0)
			var first crossDialectItemRow
			if err := b.First(ctx, &first); err != nil || first != (crossDialectItemRow{1, "one", 10}) {
				t.Fatalf("First: row=%#v err=%v", first, err)
			}
			log.check(t, want)
			if b.limit != 0 {
				t.Fatalf("First modified original limit: %d", b.limit)
			}
			b = base.Clone().Select("name")
			want = commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) { return q.Limit(1).ToSelect() }, 0)
			var value string
			if err := b.Value(ctx, &value); err != nil || value != "one" {
				t.Fatalf("Value: value=%q err=%v", value, err)
			}
			log.check(t, want)
			if b.limit != 0 {
				t.Fatalf("Value modified original limit: %d", b.limit)
			}
			b = base.Clone()
			want = commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) { return q.Select("name").ToSelect() }, 0)
			var names []string
			if err := b.Pluck(ctx, &names, "name"); err != nil || !reflect.DeepEqual(names, []string{"one", "two", "three"}) {
				t.Fatalf("Pluck: names=%#v err=%v", names, err)
			}
			log.check(t, want)
			b = base.Clone()
			want = commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) { return q.Select("name", "id").ToSelect() }, 0)
			var pairs map[int64]string
			if err := b.Pluck(ctx, &pairs, "name", "id"); err != nil || !reflect.DeepEqual(pairs, map[int64]string{1: "one", 2: "two", 3: "three"}) {
				t.Fatalf("Pluck map: pairs=%#v err=%v", pairs, err)
			}
			log.check(t, want)
			b = base.Clone()
			want = commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) { return q.Select("id", "name", "num").ToSelect() }, 0)
			var keyed map[int64]crossDialectItemRow
			if err := b.Pluck(ctx, &keyed, "id"); err != nil || !reflect.DeepEqual(keyed, map[int64]crossDialectItemRow{1: {1, "one", 10}, 2: {2, "two", 20}, 3: {3, "three", 30}}) {
				t.Fatalf("Pluck keyBy: rows=%#v err=%v", keyed, err)
			}
			log.check(t, want)
			b = base.Clone().ForPage(2, 2)
			countSQL := commentCompiledCall(t, b, state.text, (*Builder).ToCount, 0)
			dataSQL := commentCompiledCall(t, b, state.text, (*Builder).ToSelect, 0)
			var page []crossDialectItemRow
			if total, err := b.Paginate(ctx, &page); err != nil || total != 3 || !reflect.DeepEqual(page, []crossDialectItemRow{{3, "three", 30}}) {
				t.Fatalf("Paginate: total=%d rows=%#v err=%v", total, page, err)
			}
			log.check(t, countSQL, dataSQL)
			// 重复执行不消费或叠加注释，COUNT 临时状态恢复后仍保持分页 SQL。
			page = nil
			if err := b.Find(ctx, &page); err != nil || !reflect.DeepEqual(page, []crossDialectItemRow{{3, "three", 30}}) {
				t.Fatalf("Find after Paginate: rows=%#v err=%v", page, err)
			}
			log.check(t, dataSQL)
			b = base.Clone().Where("id", "<", 0).ForPage(1, 2)
			want = commentCompiledCall(t, b, state.text, (*Builder).ToCount, 0, 0)
			page = nil
			if total, err := b.Paginate(ctx, &page); err != nil || total != 0 || len(page) != 0 {
				t.Fatalf("empty Paginate: total=%d rows=%#v err=%v", total, page, err)
			}
			log.check(t, want)
		})
	}
	t.Run("Aggregates", func(t *testing.T) {
		log := &commentSQLLog{}
		dao := open(t, integrationSQLComment, log.collect)
		setupCommentTable(t, dao, table)
		mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10), (2, 'two', 20), (3, 'three', 30)")
		log.calls = nil
		b := dao.Builder().Table(table).Where("id", ">", 0)
		want := commentCompiledCall(t, b, integrationSQLComment, (*Builder).ToCount, 0)
		if n, err := b.Count(ctx); err != nil || n != 3 {
			t.Fatalf("Count: n=%d err=%v", n, err)
		}
		log.check(t, want)
		want = commentCompiledCall(t, b, integrationSQLComment, (*Builder).ToExists, 0)
		if exists, err := b.Exists(ctx); err != nil || !exists {
			t.Fatalf("Exists: exists=%v err=%v", exists, err)
		}
		log.check(t, want)
		for _, tc := range []struct {
			fn   string
			run  func(context.Context, string) (float64, error)
			want float64
		}{{"MAX", b.Max, 30}, {"MIN", b.Min, 10}, {"SUM", b.Sum, 60}, {"AVG", b.Avg, 20}} {
			want = commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToAggregate(tc.fn, "num") }, 0)
			if n, err := tc.run(ctx, "num"); err != nil || n != tc.want {
				t.Fatalf("%s: value=%v want=%v err=%v", tc.fn, n, tc.want, err)
			}
			log.check(t, want)
		}
	})
}

// crossDialectCommentCursors 每种注释状态独立建连，验证流式游标与分批 Clone 保留注释，整批边界不多发空查询。
// 同一状态内的流式读取、升/降序分批、提前退出及读回组成同一 Builder 的完整游标流程。
func crossDialectCommentCursors(t *testing.T, open commentDAOOpener) {
	t.Helper()
	ctx := context.Background()
	const table = "comment_cursor_items"
	for _, state := range []struct{ name, text string }{{"default", integrationSQLComment}, {"override", "cursor:override"}, {"clear", ""}} {
		t.Run(state.name, func(t *testing.T) {
			log := &commentSQLLog{}
			dao := open(t, integrationSQLComment, log.collect)
			setupCommentTable(t, dao, table)
			mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10), (2, 'two', 20), (3, 'three', 30), (4, 'four', 40)")
			log.calls = nil
			b := dao.Builder().Table(table).Where("num", ">", 0).OrderBy("id")
			if state.name != "default" {
				b.Comment(state.text)
			}
			want := commentCompiledCall(t, b, state.text, (*Builder).ToSelect, 0)
			var row crossDialectItemRow
			var ids []int64
			for err := range b.Cursor(ctx, &row) {
				if err != nil {
					t.Fatalf("Cursor: %v", err)
				}
				ids = append(ids, row.ID)
			}
			if !reflect.DeepEqual(ids, []int64{1, 2, 3, 4}) {
				t.Fatalf("Cursor IDs: %#v", ids)
			}
			log.check(t, want)
			for _, desc := range []bool{false, true} {
				direction, operator, last := "ASC", ">", int64(2)
				wantIDs := []int64{1, 2, 3, 4}
				if desc {
					direction, operator, last = "DESC", "<", 3
					wantIDs = []int64{4, 3, 2, 1}
				}
				first := commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) {
					q.orders = nil
					return q.OrderBy("id", direction).Limit(3).ToSelect()
				}, 0)
				second := commentCompiledCall(t, b, state.text, func(q *Builder) (string, []any, error) {
					q.orders = nil
					return q.WhereRaw(q.grammar.WrapColumn("id")+" "+operator+" ?", last).OrderBy("id", direction).Limit(3).ToSelect()
				}, 0, last)
				ids = nil
				for err := range b.CursorBy(ctx, &row, 2, "id", desc) {
					if err != nil {
						t.Fatalf("CursorBy: %v", err)
					}
					ids = append(ids, row.ID)
				}
				if !reflect.DeepEqual(ids, wantIDs) {
					t.Fatalf("CursorBy IDs: got %#v want %#v", ids, wantIDs)
				}
				log.check(t, first, second)
			}
			// helper 的唯一从库 MaxOpenConns=1；提前 break 后在同一读池限时执行完整读取，
			// 验证 rows 已释放连接且原 Builder 没有残留游标条件，不能仅靠 ToSelect 证明。
			readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			var earlyRow crossDialectItemRow
			for err := range b.Cursor(readCtx, &earlyRow) {
				if err != nil {
					t.Fatalf("Cursor early break: %v", err)
				}
				break
			}
			if earlyRow != (crossDialectItemRow{1, "one", 10}) {
				t.Fatalf("Cursor did not yield the first row before break: %#v", earlyRow)
			}
			log.check(t, want)
			var rows []crossDialectItemRow
			wantRows := []crossDialectItemRow{{1, "one", 10}, {2, "two", 20}, {3, "three", 30}, {4, "four", 40}}
			if err := b.Find(readCtx, &rows); err != nil || !reflect.DeepEqual(rows, wantRows) {
				t.Fatalf("Find after Cursor early break: rows=%#v want=%#v err=%v", rows, wantRows, err)
			}
			log.check(t, want)
			for err := range b.CursorBy(ctx, &row, 0, "id") {
				t.Fatalf("zero chunk unexpectedly yielded: %v", err)
			}
			log.check(t)
			actual, args, err := b.ToSelect()
			if err != nil || actual != want.sql || !reflect.DeepEqual(args, want.args) {
				t.Fatalf("cursor mutated base: SQL=%q args=%#v err=%v", actual, args, err)
			}
		})
	}
}

// crossDialectCommentErrors 各错误/保护用例独立建连，编译/输入错误不得执行；注释不能授权全表写。
// 非法输入与 Force 各自使用完整夹具，Force 流程连续更新、删除并确认空表查询行为。
func crossDialectCommentErrors(t *testing.T, open commentDAOOpener) {
	t.Helper()
	ctx := context.Background()
	const table = "comment_guard_items"
	patch := crossDialectUUpd{Name: "forced"}
	for _, tc := range []struct {
		name    string
		compile func(*DBDao) (string, []any, error)
		run     func(*DBDao) error
		want    error
	}{
		{"empty table", func(dao *DBDao) (string, []any, error) { return dao.Builder().ToSelect() }, func(dao *DBDao) error {
			var dest []crossDialectItemRow
			return dao.Builder().Find(ctx, &dest)
		}, ErrEmptyTable},
		{"invalid operator", func(dao *DBDao) (string, []any, error) {
			return dao.Builder().Table(table).Where("id", "INVALID", 1).ToSelect()
		}, func(dao *DBDao) error {
			var dest []crossDialectItemRow
			return dao.Builder().Table(table).Where("id", "INVALID", 1).Find(ctx, &dest)
		}, ErrInvalidOperator},
		{"cyclic clone", func(dao *DBDao) (string, []any, error) {
			cyclic := dao.Builder().Table(table)
			cyclic.Union(cyclic)
			return cyclic.Clone().ToSelect()
		}, func(dao *DBDao) error {
			cyclic := dao.Builder().Table(table)
			cyclic.Union(cyclic)
			var dest []crossDialectItemRow
			return cyclic.Find(ctx, &dest)
		}, ErrCyclicQuery},
		{"invalid insert", func(dao *DBDao) (string, []any, error) { return dao.Builder().Table(table).ToInsert(123) }, func(dao *DBDao) error {
			_, err := dao.Builder().Table(table).Insert(ctx, 123)
			return err
		}, ErrInvalidStruct},
		{"empty insert", func(dao *DBDao) (string, []any, error) {
			return dao.Builder().Table(table).ToInsert([]crossDialectItemRow{})
		}, func(dao *DBDao) error {
			_, err := dao.Builder().Table(table).Insert(ctx, []crossDialectItemRow{})
			return err
		}, ErrEmptyData},
		{"missing delete join", func(dao *DBDao) (string, []any, error) {
			return dao.Builder().Table(table).Where("id", 1).ToDeleteJoin()
		}, func(dao *DBDao) error {
			_, err := dao.Builder().Table(table).Where("id", 1).DeleteJoin(ctx)
			return err
		}, ErrDeleteJoinNoJoin},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &commentSQLLog{}
			dao := open(t, integrationSQLComment, log.collect)
			setupCommentTable(t, dao, table)
			mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10)")
			log.calls = nil
			query, args, err := tc.compile(dao)
			if !errors.Is(err, tc.want) || query != "" || args != nil {
				t.Fatalf("compile error: SQL=%q args=%#v err=%v want=%v", query, args, err, tc.want)
			}
			if err := tc.run(dao); !errors.Is(err, tc.want) {
				t.Fatalf("execution error: got %v want %v", err, tc.want)
			}
			log.check(t)
		})
	}
	for _, tc := range []struct {
		name string
		run  func(*DBDao) (int64, error)
		want error
	}{
		{"Update", func(dao *DBDao) (int64, error) {
			return dao.Builder().Table(table).Comment("not:authorization").Update(ctx, patch)
		}, ErrUpdateWithoutWhere},
		{"Delete", func(dao *DBDao) (int64, error) {
			return dao.Builder().Table(table).Comment("not:authorization").Delete(ctx)
		}, ErrDeleteWithoutWhere},
		{"DeleteJoin", func(dao *DBDao) (int64, error) {
			return dao.Builder().Table(table).Comment("not:authorization").DeleteJoin(ctx)
		}, ErrDeleteWithoutWhere},
		{"Increment", func(dao *DBDao) (int64, error) {
			return dao.Builder().Table(table).Comment("not:authorization").Increment(ctx, "num", 1)
		}, ErrUpdateWithoutWhere},
		{"Decrement", func(dao *DBDao) (int64, error) {
			return dao.Builder().Table(table).Comment("not:authorization").Decrement(ctx, "num", 1)
		}, ErrUpdateWithoutWhere},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &commentSQLLog{}
			dao := open(t, integrationSQLComment, log.collect)
			setupCommentTable(t, dao, table)
			mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10)")
			log.calls = nil
			if n, err := tc.run(dao); n != 0 || !errors.Is(err, tc.want) {
				t.Fatalf("guard: affected=%d err=%v want=%v", n, err, tc.want)
			}
			log.check(t)
		})
	}
	t.Run("InvalidDestAndInput", func(t *testing.T) {
		log := &commentSQLLog{}
		dao := open(t, integrationSQLComment, log.collect)
		setupCommentTable(t, dao, table)
		mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10)")
		log.calls = nil
		b := dao.Builder().Table(table)
		if err := b.Pluck(ctx, 1, "name"); !errors.Is(err, ErrPluckDest) {
			t.Fatalf("Pluck invalid dest: %v", err)
		}
		var row crossDialectItemRow
		cursorErrors := 0
		for err := range b.Cursor(ctx, 1) {
			cursorErrors++
			if !errors.Is(err, ErrNotPointer) {
				t.Fatalf("Cursor invalid dest: %v", err)
			}
		}
		if cursorErrors != 1 {
			t.Fatalf("Cursor invalid dest error count: %d", cursorErrors)
		}
		cursorErrors = 0
		for err := range b.CursorBy(ctx, &row, 2, "missing") {
			cursorErrors++
			if !errors.Is(err, ErrCursorFieldNotFound) {
				t.Fatalf("CursorBy invalid field: %v", err)
			}
		}
		if cursorErrors != 1 {
			t.Fatalf("CursorBy invalid field error count: %d", cursorErrors)
		}
		if _, err := b.Increment(ctx, "num", 1, "unpaired"); !errors.Is(err, ErrIncrementColumns) {
			t.Fatalf("Increment error priority: %v", err)
		}
		log.check(t)
		assertCommentRows(t, dao, table, []crossDialectItemRow{{1, "one", 10}})
	})
	t.Run("Force", func(t *testing.T) {
		log := &commentSQLLog{}
		dao := open(t, integrationSQLComment, log.collect)
		setupCommentTable(t, dao, table)
		mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10)")
		log.calls = nil
		b := dao.Builder().Table(table).Force()
		want := commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToUpdate(patch) }, "forced")
		if n, err := b.Update(ctx, patch); err != nil || n != 1 {
			t.Fatalf("Force Update: affected=%d err=%v", n, err)
		}
		log.check(t, want)
		assertCommentRows(t, dao, table, []crossDialectItemRow{{1, "forced", 10}})
		log.calls = nil
		want = commentCompiledCall(t, b, integrationSQLComment, (*Builder).ToDelete)
		if n, err := b.Delete(ctx); err != nil || n != 1 {
			t.Fatalf("Force Delete: affected=%d err=%v", n, err)
		}
		log.check(t, want)
		want = commentCompiledCall(t, b, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.Limit(1).ToSelect() })
		var row crossDialectItemRow
		if err := b.First(ctx, &row); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("empty First: %v", err)
		}
		log.check(t, want) // 已执行但无行与编译错误不同，仍应回调。
		want = commentCompiledCall(t, b, integrationSQLComment, (*Builder).ToExists)
		if exists, err := b.Exists(ctx); err != nil || exists {
			t.Fatalf("empty Exists: %v %v", exists, err)
		}
		log.check(t, want)
	})
}

// crossDialectCommentRawAndSchema 锁死原始 DAO 三入口与 Schema 的原文边界；Grammar 不读取注释。
func crossDialectCommentRawAndSchema(t *testing.T, open commentDAOOpener) {
	t.Helper()
	log := &commentSQLLog{}
	dao := open(t, integrationSQLComment, log.collect)
	ctx := context.Background()
	const table = "comment_raw_items"
	setupCommentTable(t, dao, table)
	log.calls = nil
	placeholder := "?"
	if _, pg := dao.grammar.(*PostgresGrammar); pg {
		placeholder = "$1"
	}
	query := "INSERT INTO " + table + " (id, name, num) VALUES (" + placeholder + ", 'manual', 7) /* caller:exec ? $1 */"
	result, err := dao.Exec(ctx, query, 1)
	if err != nil {
		t.Fatalf("raw Exec: %v", err)
	}
	if n, err := result.RowsAffected(); err != nil || n != 1 {
		t.Fatalf("raw affected=%d err=%v", n, err)
	}
	log.check(t, commentSQLCall{sql: query, args: []any{1}})
	for _, run := range []func(context.Context, string, ...any) (*sql.Rows, error){dao.Query, dao.QueryPrimary} {
		query = "SELECT num FROM " + table + " WHERE id = " + placeholder + " /* caller:query ? $1 */"
		rows, err := run(ctx, query, 1)
		if err != nil {
			t.Fatalf("raw query: %v", err)
		}
		assertCommentScalarRows(t, rows, 7)
		log.check(t, commentSQLCall{sql: query, args: []any{1}})
	}
	b := dao.Builder().Table(table).Where("id", "=", 1)
	plain, _, err := b.Clone().Comment("").ToSelect()
	if err != nil {
		t.Fatalf("plain compile: %v", err)
	}
	if got := dao.grammar.CompileSelect(b, b.columns); got != plain {
		t.Fatalf("direct Grammar: got %q want %q", got, plain)
	}
	log.check(t)
	inspector, err := dao.Schema()
	if err != nil {
		t.Fatalf("Schema: %v", err)
	}
	var tableSQL, columnsSQL string
	var columnArgs []any
	switch dao.grammar.(type) {
	case *MySQLGrammar:
		tableSQL = "SELECT TABLE_NAME, IFNULL(TABLE_COMMENT, '') FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME"
		columnsSQL = "SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(COLUMN_COMMENT, ''), IS_NULLABLE, COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION"
		columnArgs = []any{table}
	case *PostgresGrammar:
		tableSQL = "SELECT c.relname, COALESCE(obj_description(c.oid, 'pg_class'), '') FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind = 'r' AND n.nspname = 'public' ORDER BY c.relname"
		columnsSQL = "SELECT a.attname, format_type(a.atttypid, a.atttypmod), COALESCE(col_description(c.oid, a.attnum), ''), NOT a.attnotnull, pg_get_expr(d.adbin, d.adrelid) FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum WHERE c.relname = $1 AND n.nspname = 'public' AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum"
		columnArgs = []any{table}
	case *SQLiteGrammar:
		tableSQL = `SELECT "name", '' FROM "sqlite_master" WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%' ORDER BY "name"`
		columnsSQL = `PRAGMA table_info("comment_raw_items")`
	}
	tables, err := inspector.Tables(ctx)
	if err != nil {
		t.Fatalf("Schema.Tables: %v", err)
	}
	found := false
	for _, item := range tables {
		if item.Name == table {
			found = true
		}
	}
	if !found {
		t.Fatalf("Schema.Tables omitted isolated table: %#v", tables)
	}
	log.check(t, commentSQLCall{sql: tableSQL})
	columns, err := inspector.Columns(ctx, table)
	if err != nil {
		t.Fatalf("Schema.Columns: %v", err)
	}
	var names []string
	for _, column := range columns {
		names = append(names, column.Name)
	}
	if !reflect.DeepEqual(names, []string{"id", "name", "num"}) {
		t.Fatalf("Schema.Columns names: %#v", names)
	}
	log.check(t, commentSQLCall{sql: columnsSQL, args: columnArgs})
}

// assertCommentScalarRows 处理所有 rows I/O 错误，验证恰好一行一个常量。
func assertCommentScalarRows(t *testing.T, rows *sql.Rows, want int) {
	t.Helper()
	defer func() {
		if err := rows.Close(); err != nil {
			t.Errorf("close rows: %v", err)
		}
	}()
	if !rows.Next() {
		t.Fatalf("missing scalar row: %v", rows.Err())
	}
	var got int
	if err := rows.Scan(&got); err != nil {
		t.Fatalf("scan scalar: %v", err)
	}
	if got != want {
		t.Fatalf("scalar: got %d want %d", got, want)
	}
	if rows.Next() {
		t.Fatal("unexpected extra scalar row")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate scalar: %v", err)
	}
}

// crossDialectCommentSafety 每个输入独立建连，只运行常量 SELECT；同一输入的直接/预编译执行为完整流程。
// 两种执行方式均保留真实绑定，禁止触碰业务表。
func crossDialectCommentSafety(t *testing.T, open commentDAOOpener) {
	t.Helper()
	ctx := context.Background()
	for _, tc := range []struct{ name, input, cleaned string }{
		{"overlap", "**// + 41 -- ", "+ 41 --"},
		{"nested", "a /* b", "a    b"},
		{"executable", "/*! + 41 */", "! + 41"},
		{"hint", "/*+ hint */", "+ hint"},
		{"controls", "a\x00b\r\nc\t\x1b\x7f\u0085d", "a b  c    d"},
		{"bindings", "普通中文 trace:? $1 ' \" ; / \\", "普通中文 trace:? $1 ' \" ;"},
		{"slash path", "svc:/api/v1 'q'", "svc: api v1 'q'"},
		{"backslash path", "C:\\svc\\api 'q'", "C: svc api 'q'"},
		{"invalid UTF8", "a\xffb", "a\ufffdb"},
		{"bounded", strings.Repeat("界", 300), strings.Repeat("界", 255)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			log := &commentSQLLog{}
			dao := open(t, integrationSQLComment, log.collect)
			placeholder := "?"
			if _, pg := dao.grammar.(*PostgresGrammar); pg {
				placeholder = "$1"
			}
			base := "SELECT " + placeholder + " + 0"
			// 与 Builder 最终编译出口共用追加函数，但执行的 SQL 仅含常量，避免用表证明安全性。
			query := dao.Builder().Comment(tc.input).appendSQLComment(base)
			want := base + " /* " + tc.cleaned + " */"
			if query != want {
				t.Fatalf("safe SQL: got %q want %q", query, want)
			}
			args := []any{1}
			rows, err := dao.Query(ctx, query, args...)
			if err != nil {
				t.Fatalf("direct constant SELECT: %v", err)
			}
			assertCommentScalarRows(t, rows, 1)
			log.check(t, commentSQLCall{sql: want, args: args})
			stmt, err := dao.Pool().PickWriteDB().PrepareContext(ctx, query)
			if err != nil {
				t.Fatalf("prepare constant SELECT: %v", err)
			}
			defer func() {
				if err := stmt.Close(); err != nil {
					t.Errorf("close statement: %v", err)
				}
			}()
			rows, err = stmt.QueryContext(ctx, args...)
			if err != nil {
				t.Fatalf("prepared constant SELECT: %v", err)
			}
			assertCommentScalarRows(t, rows, 1)
			if !reflect.DeepEqual(args, []any{1}) {
				t.Fatalf("mutated binding: %#v", args)
			}
			log.check(t) // 直接 sql.Stmt 不经过 DAO 回调。
		})
	}
}

// crossDialectCommentTransactions 回滚/提交各自独立建连建表，执行完整的读路由→事务→读回流程。
// 从库选择计数证明 Primary/事务绕过读路由；每次读取使用新的零值接收变量并比较完整行，避免旧值掩盖未扫描。
func crossDialectCommentTransactions(t *testing.T, open commentDAOOpener) {
	t.Helper()
	ctx := context.Background()
	const table = "comment_tx_items"
	for _, tc := range []struct {
		name   string
		commit bool
	}{{"rollback", false}, {"commit", true}} {
		t.Run(tc.name, func(t *testing.T) {
			log := &commentSQLLog{}
			dao := open(t, integrationSQLComment, log.collect)
			setupCommentTable(t, dao, table)
			mustExec(t, dao, "INSERT INTO "+table+" (id, name, num) VALUES (1, 'one', 10)")
			strategy := &RoundRobinStrategy{}
			dao.pool.slaveStrategy = strategy
			log.calls = nil
			base := dao.Builder().Table(table).Where("id", 1)
			original := crossDialectItemRow{1, "one", 10}
			updated := crossDialectItemRow{1, "transaction", 10}
			var readRow crossDialectItemRow
			want := commentCompiledCall(t, base, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.Limit(1).ToSelect() }, 1)
			if err := base.First(ctx, &readRow); err != nil || readRow != original {
				t.Fatalf("read route: row=%#v want=%#v err=%v", readRow, original, err)
			}
			if strategy.counter.Load() != 1 {
				t.Fatal("unmarked read did not select replica")
			}
			log.check(t, want)
			var primaryRow crossDialectItemRow
			if err := base.Clone().Primary().First(ctx, &primaryRow); err != nil || primaryRow != original {
				t.Fatalf("primary route: row=%#v want=%#v err=%v", primaryRow, original, err)
			}
			if strategy.counter.Load() != 1 {
				t.Fatal("Primary selected replica")
			}
			log.check(t, want)
			rollback := errors.New("comment transaction rollback")
			var tx *sql.Tx
			err := dao.Transaction(ctx, func(txCtx context.Context) error {
				tx = txFromCtx(txCtx)
				n, err := base.Update(txCtx, crossDialectUUpd{Name: "transaction"})
				if err != nil || n != 1 {
					return fmt.Errorf("transaction Update: n=%d err=%v", n, err)
				}
				return dao.Transaction(txCtx, func(nested context.Context) error {
					if txFromCtx(nested) != tx {
						return errors.New("nested transaction lost original tx")
					}
					var txRow crossDialectItemRow
					if err := base.First(nested, &txRow); err != nil {
						return err
					}
					if txRow != updated {
						return fmt.Errorf("transaction read own write: row=%#v want=%#v", txRow, updated)
					}
					if !tc.commit {
						return rollback
					}
					return nil
				})
			})
			if (!tc.commit && !errors.Is(err, rollback)) || (tc.commit && err != nil) {
				t.Fatalf("transaction commit=%v err=%v", tc.commit, err)
			}
			if tx == nil {
				t.Fatal("missing transaction")
			}
			for i, call := range log.calls {
				if call.tx != tx {
					t.Errorf("callback %d lost transaction context", i)
				}
			}
			if strategy.counter.Load() != 1 {
				t.Fatal("transaction selected replica")
			}
			update := commentCompiledCall(t, base, integrationSQLComment, func(q *Builder) (string, []any, error) { return q.ToUpdate(crossDialectUUpd{Name: "transaction"}) }, "transaction", 1)
			log.check(t, update, want)
			var postRow crossDialectItemRow
			if err := base.Clone().Primary().First(ctx, &postRow); err != nil {
				t.Fatalf("post transaction read: %v", err)
			}
			expected := original
			if tc.commit {
				expected = updated
			}
			if postRow != expected {
				t.Fatalf("commit=%v read=%#v want=%#v", tc.commit, postRow, expected)
			}
			log.check(t, want)
		})
	}
}

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
	// 多列自增的第二列必须一并验证，否则只自增首列的缺陷不会被发现。
	var level int64
	if err := dao.Builder().Table("cross_dialect_counter").Select("level").Where("id", "=", 1).Value(ctx, &level); err != nil {
		t.Fatalf("查询多列自增的 level 失败: %v", err)
	}
	if level != 3 {
		t.Fatalf("level 应为 3（1+2），实际 %d", level)
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
	// 仅比对行数无法发现列映射错误或写入空值，需回查具体值。
	var names []string
	if err := dao.Builder().Table("cross_dialect_dst").OrderBy("name").Pluck(ctx, &names, "name"); err != nil {
		t.Fatalf("回查 InsertUsing 写入值失败: %v", err)
	}
	if len(names) != 2 || names[0] != "a" || names[1] != "b" {
		t.Fatalf("InsertUsing 写入值应为 [a b]，实际 %v", names)
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
// MySQL 不走 FROM 展平（编译为 UPDATE ... JOIN ... SET），本用例同时锁死三方言
// 在该场景下的行为等价性。
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
