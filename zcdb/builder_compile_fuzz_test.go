// 本文件为 Builder 编译路径的跨方言 fuzz 测试（builder_compile.go）：
// fuzzer 变异查询形状、标识符、运算符、值与分页参数，对 MySQL/PostgreSQL/SQLite
// 分别编译并校验：无 panic、重复编译确定性、错误形态一致、跨方言参数一致，
// 以及 MySQL/SQLite 的 ? 数量、PostgreSQL 的 $N 顺序与参数数量一致。
// 不执行 SQL、不依赖数据库；引擎语义验证仍由集成测试承担。

package zcdb

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type builderFuzzDialect struct {
	name    string
	grammar Grammar
	syntax  sqlScanSyntax
}

type builderFuzzResult struct {
	name string
	sql  string
	args []any
	err  error
}

// FuzzBuilderCompile 对 Builder 编译路径做跨方言差分 fuzz：
// shape 选择 Select/Join/Group+Having/Count/Exists/Aggregate/In/Between/Null 等形态，
// table/column/operator/value 由 fuzzer 变异；列名含 "(" 时按文档约定属于原始表达式，
// 不纳入标识符不变量校验。普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzBuilderCompile$' -fuzztime=30s
func FuzzBuilderCompile(f *testing.F) {
	seeds := []struct {
		shape    uint8
		table    string
		column   string
		operator string
		value    string
		distinct bool
		limit    uint16
		offset   uint16
	}{
		{0, "users", "age", ">", "18", false, 0, 0},
		{1, "users", "name", "LIKE", "%a%", true, 10, 5},
		{2, "users", `u"sers`, "=", "x'y", false, 0, 0},
		{3, "users", "a`ge", "=", "v", false, 0, 0},
		{4, "users", "city", "=", "Hangzhou", false, 0, 0},
		{5, "users", "age", ">", "18", true, 0, 0},
		{6, "users", "age", ">", "18", false, 0, 0},
		{7, "users", "age", ">", "18", false, 0, 0},
		{8, "users", "age", "=", "18", false, 0, 0},
		{9, "users", "age", "=", "18", false, 0, 0},
		{10, "users", "age", "=", "18", false, 0, 0},
		{0, "", "age", ">", "18", false, 0, 0},
		{0, "users", "age", "INVALID", "18", false, 0, 0},
	}
	for _, seed := range seeds {
		f.Add(seed.shape, seed.table, seed.column, seed.operator, seed.value, seed.distinct, seed.limit, seed.offset)
	}

	dialects := []builderFuzzDialect{
		{"MySQL", NewMySQLGrammar(), mysqlScanSyntax},
		{"PostgreSQL", NewPostgresGrammar(), pgScanSyntax},
		{"SQLite", NewSQLiteGrammar(), sqliteScanSyntax},
	}
	f.Fuzz(func(t *testing.T, shape uint8, table, column, operator, value string, distinct bool, limit, offset uint16) {
		if len(table) > 4<<10 || len(column) > 4<<10 || len(operator) > 256 || len(value) > 4<<10 {
			return
		}
		// 文档约定：列名含 "(" 视为原始表达式（如 COUNT(*)），不属于标识符；
		// 该形态无法套用“参数数量与占位符一致”的标识符不变量，排除出本目标。
		if strings.Contains(column, "(") {
			return
		}
		shape %= 11

		results := make([]builderFuzzResult, len(dialects))
		for i, dialect := range dialects {
			builder := buildFuzzBuilder(dialect.grammar, shape, table, column, operator, value, distinct, limit, offset)
			sql1, args1, err1 := runFuzzTerminal(builder, shape, column)
			sql2, args2, err2 := runFuzzTerminal(builder, shape, column)
			if (err1 != nil) != (err2 != nil) || sql1 != sql2 || !reflect.DeepEqual(args1, args2) {
				t.Fatalf("dialect=%s shape=%d table=%q column=%q op=%q value=%q: repeated compile differs:\n(%q,%v,%v)\n(%q,%v,%v)",
					dialect.name, shape, table, column, operator, value, sql1, args1, err1, sql2, args2, err2)
			}
			results[i] = builderFuzzResult{name: dialect.name, sql: sql1, args: args1, err: err1}
		}

		allErr := results[0].err != nil
		for _, result := range results[1:] {
			if (result.err != nil) != allErr {
				t.Fatalf("shape=%d table=%q column=%q op=%q value=%q: error presence differs across dialects: %+v", shape, table, column, operator, value, results)
			}
		}
		if allErr {
			return
		}

		baseArgs := results[0].args
		for i, result := range results {
			if !reflect.DeepEqual(result.args, baseArgs) {
				t.Fatalf("shape=%d table=%q column=%q op=%q value=%q: args differ: %s=%v, %s=%v",
					shape, table, column, operator, value, results[0].name, baseArgs, result.name, result.args)
			}
			if result.name == "PostgreSQL" {
				assertFuzzSequentialPlaceholders(t, result.name, result.sql, dialects[i].syntax, len(result.args))
				continue
			}
			if got := fuzzRawCodePlaceholderCount(result.sql, dialects[i].syntax, result.name); got != len(result.args) {
				t.Fatalf("dialect=%s shape=%d sql=%q: %d placeholder(s), %d arg(s)", result.name, shape, result.sql, got, len(result.args))
			}
		}
	})
}

func buildFuzzBuilder(g Grammar, shape uint8, table, column, operator, value string, distinct bool, limit, offset uint16) *Builder {
	builder := NewBuilder(g, nil).Table(table)
	switch shape {
	case 0, 1, 5, 6, 7:
		builder.Select(column).Where(column, operator, value)
	case 2:
		builder.Select(column).Join("orders", "orders.user_id", "=", "users.id")
	case 3:
		builder.Select(column).Join("orders", "orders.user_id", "=", "users.id").Where(column, operator, value)
	case 4:
		builder.Select(column).GroupBy(column).Having(column, operator, value)
	case 8:
		builder.Select(column).WhereIn(column, []any{value, "fallback"})
	case 9:
		builder.Select(column).WhereBetween(column, value, "fallback")
	case 10:
		builder.Select(column).WhereNull(column)
	}
	if distinct {
		builder.Distinct()
	}
	if limit > 0 {
		builder.Limit(int(limit))
	}
	if offset > 0 {
		builder.Offset(int(offset))
	}
	return builder
}

func runFuzzTerminal(builder *Builder, shape uint8, column string) (string, []any, error) {
	switch shape {
	case 5:
		return builder.ToCount()
	case 6:
		return builder.ToExists()
	case 7:
		return builder.ToAggregate("MAX", column)
	default:
		return builder.ToSelect()
	}
}

// assertFuzzSequentialPlaceholders 断言 PostgreSQL 产物代码区的 $N 严格按 1..argCount 顺序出现。
// Builder 生成的参数顺序必须与 SQL 占位符顺序一致，重复、跳号或越界都会在这里暴露。
func assertFuzzSequentialPlaceholders(t *testing.T, dialect, sqlStr string, syntax sqlScanSyntax, argCount int) {
	t.Helper()
	expected := 1
	for _, segment := range splitSQLCode(sqlStr, syntax) {
		if !segment.code {
			continue
		}
		for i := 0; i < len(segment.text); i++ {
			digits := numberedDigits(segment.text, i)
			if digits == 0 {
				continue
			}
			index, err := strconv.Atoi(segment.text[i+1 : i+1+digits])
			if err != nil || index != expected {
				t.Fatalf("dialect=%s sql=%q: placeholder $%d (err=%v), want $%d", dialect, sqlStr, index, err, expected)
			}
			expected++
			i += digits
		}
	}
	if expected-1 != argCount {
		t.Fatalf("dialect=%s sql=%q: %d placeholder(s), %d arg(s)", dialect, sqlStr, expected-1, argCount)
	}
}

// FuzzBuilderSubqueryCompile 覆盖子查询编译路径的跨方言不变量：
// TableSub/SelectSub/WhereInSub/WhereExists/WhereSub/JoinSub/Union/InsertUsing
// 及两层嵌套子查询；子查询自身使用固定合法条件，外层运算符由 fuzzer 变异。
// 校验重复编译确定性、错误形态一致、跨方言 args 一致与占位符数量/顺序。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzBuilderSubqueryCompile$' -fuzztime=30s
func FuzzBuilderSubqueryCompile(f *testing.F) {
	seeds := []struct {
		shape    uint8
		variant  bool
		table    string
		column   string
		operator string
		value    string
	}{
		{0, false, "users", "age", ">", "18"},
		{1, false, "users", "id", "=", "1"},
		{2, false, "users", "dept_id", "=", "3"},
		{2, true, "users", "dept_id", "=", "3"},
		{3, false, "users", "age", ">", "18"},
		{3, true, "users", "age", ">", "18"},
		{4, false, "users", "age", ">", "18"},
		{4, true, "users", "age", ">", "18"},
		{5, false, "users", "id", "=", "1"},
		{5, true, "users", "id", "=", "1"},
		{6, false, "users", "name", "=", "alice"},
		{6, true, "users", "name", "=", "alice"},
		{7, false, "archive", "name", "=", "alice"},
		{7, true, "archive", "name", "=", "alice"},
		{8, false, "users", "age", ">", "18"},
		{9, false, "users", "age", ">", "18"},
		{10, false, "users", "age", ">", "18"},
		{11, false, "archive", "name", "=", "alice"},
		{11, true, "archive", "name", "=", "alice"},
		{1, false, "", "age", "INVALID", "18"},
	}
	for _, seed := range seeds {
		f.Add(seed.shape, seed.variant, seed.table, seed.column, seed.operator, seed.value)
	}

	dialects := []builderFuzzDialect{
		{"MySQL", NewMySQLGrammar(), mysqlScanSyntax},
		{"PostgreSQL", NewPostgresGrammar(), pgScanSyntax},
		{"SQLite", NewSQLiteGrammar(), sqliteScanSyntax},
	}
	f.Fuzz(func(t *testing.T, shape uint8, variant bool, table, column, operator, value string) {
		if len(table) > 4<<10 || len(column) > 4<<10 || len(operator) > 256 || len(value) > 4<<10 {
			return
		}
		if strings.Contains(column, "(") {
			return
		}
		shape %= 12

		results := make([]builderFuzzResult, len(dialects))
		for i, dialect := range dialects {
			builder := buildSubqueryFuzzBuilder(dialect.grammar, shape, variant, table, column, operator, value)
			sql1, args1, err1 := runSubqueryFuzzTerminal(builder, shape, variant, column, value)
			sql2, args2, err2 := runSubqueryFuzzTerminal(builder, shape, variant, column, value)
			if (err1 != nil) != (err2 != nil) || sql1 != sql2 || !reflect.DeepEqual(args1, args2) {
				t.Fatalf("dialect=%s shape=%d variant=%v table=%q column=%q op=%q value=%q: repeated compile differs:\n(%q,%v,%v)\n(%q,%v,%v)",
					dialect.name, shape, variant, table, column, operator, value, sql1, args1, err1, sql2, args2, err2)
			}
			results[i] = builderFuzzResult{name: dialect.name, sql: sql1, args: args1, err: err1}
		}

		allErr := results[0].err != nil
		for _, result := range results[1:] {
			if (result.err != nil) != allErr {
				t.Fatalf("shape=%d variant=%v table=%q column=%q op=%q value=%q: error presence differs across dialects: %+v",
					shape, variant, table, column, operator, value, results)
			}
		}
		if allErr {
			return
		}

		baseArgs := results[0].args
		for i, result := range results {
			if !reflect.DeepEqual(result.args, baseArgs) {
				t.Fatalf("shape=%d variant=%v table=%q column=%q op=%q value=%q: args differ: %s=%v, %s=%v",
					shape, variant, table, column, operator, value, results[0].name, baseArgs, result.name, result.args)
			}
			if result.name == "PostgreSQL" {
				assertFuzzSequentialPlaceholders(t, result.name, result.sql, dialects[i].syntax, len(result.args))
				continue
			}
			if got := fuzzRawCodePlaceholderCount(result.sql, dialects[i].syntax, result.name); got != len(result.args) {
				t.Fatalf("dialect=%s shape=%d sql=%q: %d placeholder(s), %d arg(s)", result.name, shape, result.sql, got, len(result.args))
			}
		}
	})
}

// buildSubqueryFuzzBuilder 按 shape 构造带子查询的 Builder；子查询内部使用固定合法条件，
// 外层 where 使用 fuzzer 提供的运算符/值，便于单独观察外层错误与参数收集。
func buildSubqueryFuzzBuilder(g Grammar, shape uint8, variant bool, table, column, operator, value string) *Builder {
	sub := func() *Builder {
		return NewBuilder(g, nil).Table("source").Select(column).Where(column, ">", value)
	}
	subCallback := func(q *Builder) {
		q.Table("source").Select(column).Where(column, ">", value)
	}
	switch shape {
	case 0:
		return NewBuilder(g, nil).TableSub(sub(), "s").Select(column).Where(column, operator, value)
	case 1:
		return NewBuilder(g, nil).Table(table).Select(column).SelectSub(sub(), "s").Where(column, operator, value)
	case 2:
		builder := NewBuilder(g, nil).Table(table).Select(column)
		if variant {
			builder.WhereNotInSub(column, subCallback)
		} else {
			builder.WhereInSub(column, subCallback)
		}
		return builder.Where(column, operator, value)
	case 3:
		builder := NewBuilder(g, nil).Table(table).Select(column)
		if variant {
			builder.OrWhereSub(column, operator, subCallback)
		} else {
			builder.WhereSub(column, operator, subCallback)
		}
		return builder.Where(column, operator, value)
	case 4:
		builder := NewBuilder(g, nil).Table(table).Select(column)
		if variant {
			builder.WhereNotExists(subCallback)
		} else {
			builder.WhereExists(subCallback)
		}
		return builder.Where(column, operator, value)
	case 5:
		builder := NewBuilder(g, nil).Table(table).Select(column)
		callback := func(j *JoinBuilder) {
			j.On("s.user_id", "=", "users.id").Where("s.score", ">", value)
		}
		if variant {
			builder.LeftJoinSub(sub(), "s", callback)
		} else {
			builder.JoinSub(sub(), "s", callback)
		}
		return builder.Where(column, operator, value)
	case 6:
		union := NewBuilder(g, nil).Table("admins").Select(column).Where(column, ">", value)
		builder := NewBuilder(g, nil).Table(table).Select(column).Where(column, operator, value)
		if variant {
			builder.UnionAll(union)
		} else {
			builder.Union(union)
		}
		return builder
	case 7, 11:
		return NewBuilder(g, nil).Table(table)
	case 8:
		inner := NewBuilder(g, nil).Table("deep").Select(column).Where(column, ">", value)
		middle := NewBuilder(g, nil).Table("source").Select(column).
			TableSub(inner, "d").
			Where(column, ">", value)
		return NewBuilder(g, nil).TableSub(middle, "m").Select(column).Where(column, operator, value)
	case 9:
		nested := NewBuilder(g, nil).Table("source").Select(column).
			WhereInSub(column, func(q *Builder) {
				q.Table("dept").Select(column).Where(column, ">", value)
			}).
			Where(column, ">", value)
		return NewBuilder(g, nil).Table(table).Select(column).SelectSub(nested, "s").Where(column, operator, value)
	case 10:
		nested := NewBuilder(g, nil).Table("source").Select(column).
			WhereSub(column, ">", func(q *Builder) {
				q.Table("stats").Select(column).Where(column, ">", value)
			}).
			Where(column, ">", value)
		return NewBuilder(g, nil).Table(table).Select(column).WhereExists(nested).Where(column, operator, value)
	default:
		return NewBuilder(g, nil).Table(table).Select(column)
	}
}

func runSubqueryFuzzTerminal(builder *Builder, shape uint8, variant bool, column, value string) (string, []any, error) {
	simpleSub := func(q *Builder) {
		q.Table("source").Select(column).Where(column, ">", value)
	}
	switch shape {
	case 7:
		if variant {
			return builder.ToInsertOrIgnoreUsing([]string{column}, simpleSub)
		}
		return builder.ToInsertUsing([]string{column}, simpleSub)
	case 11:
		sub := func(q *Builder) {
			joined := NewBuilder(builder.grammar, nil).Table("joined").Select(column).Where(column, ">", value)
			q.Table("source").Select(column).
				JoinSub(joined, "j", func(jb *JoinBuilder) {
					jb.On("j.uid", "=", "source.uid").Where("j.score", ">", value)
				}).
				Where(column, ">", value)
		}
		if variant {
			return builder.ToInsertOrIgnoreUsing([]string{column}, sub)
		}
		return builder.ToInsertUsing([]string{column}, sub)
	default:
		return builder.ToSelect()
	}
}
