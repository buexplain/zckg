// 本文件为 Builder.Clone 深拷贝的 fuzz 测试：
// fuzzer 构造包含 FROM/SELECT 子查询、混合 JOIN、嵌套 WHERE/HAVING、原始绑定、UNION、
// 注释与 Force/Primary/Lock 标记的 Builder，校验原对象与克隆对象、克隆链、
// 双向增量修改隔离，以及环引用克隆快速失败（ErrCyclicQuery）而非栈溢出。
// 仅编译、不执行 SQL；绑定值本身按 Clone 文档为共享引用，不在断言范围内。

package zcdb

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type cloneFuzzOutput struct {
	sql  string
	args []any
	err  error
}

func cloneFuzzCompile(builder *Builder) cloneFuzzOutput {
	sqlStr, args, err := builder.ToSelect()
	return cloneFuzzOutput{sql: sqlStr, args: args, err: err}
}

func assertCloneFuzzEqual(t *testing.T, stage string, want, got cloneFuzzOutput) {
	t.Helper()
	if !fuzzSameError(want.err, got.err) || want.sql != got.sql || !reflect.DeepEqual(want.args, got.args) {
		t.Fatalf("%s differs:\nwant (%q,%v,%v)\ngot  (%q,%v,%v)", stage, want.sql, want.args, want.err, got.sql, got.args, got.err)
	}
}

// FuzzBuilderClone 校验 Clone 深拷贝语义：原对象与 Clone 编译结果一致、Clone 可链式再克隆、
// 修改 Clone 不影响原对象且反向亦然；Force/Primary 不进入 SQL，需直接比较副本字段，
// 锁子句则经编译产物间接验证；shape=7 构造 TableSub 自引用，Clone 必须快速返回
// 携带 ErrCyclicQuery 的副本。普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzBuilderClone$' -fuzztime=30s
func FuzzBuilderClone(f *testing.F) {
	seeds := []struct {
		shape    uint8
		table    string
		column   string
		operator string
		value    string
		comment  string
		limit    uint16
		offset   uint16
		flags    uint8
	}{
		{0, "users", "age", ">", "18", "trace:svc", 10, 5, 0},
		{1, "users", "age", ">", "18", "state:query", 0, 0, 2},
		{2, `u"sers`, "a`ge", "=", "x'y", "a*/b", 1, 1, 4},
		{3, "users", "age", ">", "18", `C:\svc`, 0, 0, 8},
		{4, "users", "status", "=", "active", "中文 trace", 100, 0, 1 | 2 | 4},
		{5, "users", "age", ">", "18", "", 0, 0, 0},
		{6, "", "age", ">", "18", "", 0, 0, 0},
		{7, "cycle", "age", ">", "18", "", 0, 0, 0},
		{0, "users", "age", "INVALID", "18", "", 0, 0, 0},
	}
	for _, seed := range seeds {
		f.Add(seed.shape, seed.table, seed.column, seed.operator, seed.value, seed.comment, seed.limit, seed.offset, seed.flags)
	}

	dialects := []builderFuzzDialect{
		{"MySQL", NewMySQLGrammar(), mysqlScanSyntax},
		{"PostgreSQL", NewPostgresGrammar(), pgScanSyntax},
		{"SQLite", NewSQLiteGrammar(), sqliteScanSyntax},
	}
	f.Fuzz(func(t *testing.T, shape uint8, table, column, operator, value, comment string, limit, offset uint16, flags uint8) {
		if len(table) > 4<<10 || len(column) > 4<<10 || len(operator) > 256 || len(value) > 4<<10 || len(comment) > 4<<10 {
			return
		}
		// 列名含 "(" 按文档视为原始表达式，不属于常态标识符，排除出本目标。
		if strings.Contains(column, "(") {
			return
		}
		shape %= 8

		for _, dialect := range dialects {
			original := buildCloneFuzzBuilder(dialect.grammar, shape, table, column, operator, value, comment, limit, offset, flags)
			baseline := cloneFuzzCompile(original)

			clone := original.Clone()
			// Force/Primary 只影响执行保护与连接路由，不改变 ToSelect 产物；
			// 若只比较编译结果，Clone 丢失这两个标记不会被发现，故直接断言副本字段。
			if clone.force != original.force || clone.usePrimary != original.usePrimary || clone.lockClause != original.lockClause {
				t.Fatalf("dialect=%s: Clone lost execution flags: force %v->%v, primary %v->%v, lock %q->%q",
					dialect.name, original.force, clone.force, original.usePrimary, clone.usePrimary, original.lockClause, clone.lockClause)
			}
			assertCloneFuzzEqual(t, dialect.name+" original/clone", baseline, cloneFuzzCompile(clone))
			assertCloneFuzzEqual(t, dialect.name+" clone/clone-of-clone", cloneFuzzCompile(clone), cloneFuzzCompile(clone.Clone()))

			if shape == 7 {
				if !errors.Is(baseline.err, ErrCyclicQuery) {
					t.Fatalf("dialect=%s: cyclic clone should carry ErrCyclicQuery, got %v", dialect.name, baseline.err)
				}
				continue
			}
			if baseline.err != nil {
				continue
			}

			// 修改 Clone 不得影响原对象。
			clone.Where("clone_marker", "=", value)
			assertCloneFuzzEqual(t, dialect.name+" original after clone mutation", baseline, cloneFuzzCompile(original))
			cloneMutated := cloneFuzzCompile(clone)
			if cloneMutated.sql == baseline.sql && reflect.DeepEqual(cloneMutated.args, baseline.args) {
				t.Fatalf("dialect=%s: clone mutation did not change clone output", dialect.name)
			}

			// 修改原对象不得影响 Clone。
			original.Where("orig_marker", "=", value)
			assertCloneFuzzEqual(t, dialect.name+" clone after original mutation", cloneMutated, cloneFuzzCompile(clone))
			if after := cloneFuzzCompile(original); after.sql == baseline.sql && reflect.DeepEqual(after.args, baseline.args) {
				t.Fatalf("dialect=%s: original mutation did not change original output", dialect.name)
			}
		}
	})
}

// buildCloneFuzzBuilder 构造覆盖 Clone 全部复制分支的 Builder。
// 先经 newTestBuilder 注入统一测试注释，再以 fuzz 输入覆盖 Comment：
// 注释本身就是本目标的 fuzz 维度，属于注释专项例外（不要求保留 testComment）。
// FROM/SELECT 子查询（共享同一子 Builder，形成菱形引用）、JoinOn + JoinSub、
// 嵌套 WHERE/HAVING、WhereIn/WhereRaw/GroupByRaw/HavingRaw、Order/Limit/Offset、
// UNION、Comment 与 Force/Primary/Lock 标记。shape=7 构造自引用环。
func buildCloneFuzzBuilder(g Grammar, shape uint8, table, column, operator, value, comment string, limit, offset uint16, flags uint8) *Builder {
	if shape == 7 {
		builder := newTestBuilder(g, nil).Table("cycle")
		builder.TableSub(builder, "self")
		return builder
	}
	sharedSub := newTestBuilder(g, nil).Table("orders").Select("user_id").Where("amount", ">", value)
	unionSub := newTestBuilder(g, nil).Table("admins").Select(column).Where(column, ">", value)

	builder := newTestBuilder(g, nil)
	if shape%2 == 1 {
		builder.TableSub(sharedSub, "base")
	} else {
		builder.Table(table)
	}
	builder.Select(column).
		SelectSub(sharedSub, "s").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").Where("profiles.active", "=", value)
		}).
		JoinSub(sharedSub, "o", func(j *JoinBuilder) {
			j.On("o.user_id", "=", "users.id").Where("o.score", ">", value)
		}).
		Where(column, operator, value).
		WhereIn(column, []any{value, "fallback"}).
		WhereRaw("score > ?", value).
		WhereNested(func(q *Builder) {
			q.Where(column, ">", value)
		}).
		WhereExists(func(q *Builder) {
			q.Table("audit").Select("id").Where("audit.user_id", "=", value)
		}).
		GroupByRaw("YEAR(created_at)", value).
		Having(column, operator, value).
		HavingRaw("COUNT(*) > ?", value).
		HavingNested(func(q *Builder) {
			q.Having("cnt", ">", value)
		}).
		OrderBy(column, "desc").
		Union(unionSub).
		Comment(comment)
	if limit > 0 {
		builder.Limit(int(limit))
	}
	if offset > 0 {
		builder.Offset(int(offset))
	}
	if flags&1 != 0 {
		builder.Distinct()
	}
	if flags&2 != 0 {
		builder.Force()
	}
	if flags&4 != 0 {
		builder.Primary()
	}
	if flags&8 != 0 {
		builder.LockForUpdate()
	}
	return builder
}
