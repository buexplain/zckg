// 本文件为「参数值内联」（rawLiteral.go）与 CompileRaw 的编译层单元测试：
// 覆盖占位符形态识别（字符串/引号标识符/注释/美元引用）、编号模式校验、
// 不支持类型与 NaN/Inf 报错、占位符数量校验，并包含 CompileRaw 的 fuzz 不变量与计数模型；
// 文件中的 rawTestTime 与命名字节/标量类型供本包单元层与集成层用例共用。
// 仅验证编译结果，不依赖数据库连接、不执行 SQL；用例不经过 Builder，因此不涉及 DAO 默认注释。
// 「值内联后被数据库认可」的可执行验证在 cross_dialect_integration_test.go
// （crossDialectTestCompileRawLiterals / crossDialectTestScanEngineParity），
// PostgreSQL 专有语法边界的真库验证在 raw_literal_postgres_integration_test.go。

package zcdb

import (
	"database/sql/driver"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"
)

// rawTestTime 是内联时间用例的固定值（供本包单元层与集成层用例共用）：带 +08:00 偏移、含毫秒。
var rawTestTime = time.Date(2026, 10, 16, 12, 34, 56, 789000000, time.FixedZone("CST", 8*3600))

type rawNamedInt int64
type rawNamedString string
type rawNamedBytes []byte

// TestGrammar_CompileRaw_SQLStructure 验证占位符识别只发生在代码区（与引擎解析一致），
// 这里只保留无法由跨方言执行矩阵表达的方言特有语法：MySQL 的双引号串/反引号标识符/
// `--` 需空白/`#` 注释/`??` 计数、SQLite 的反斜杠不转义。
// PostgreSQL 特有语法（jsonb `?`、美元引用、嵌套块注释、E'...'）已在
// raw_literal_postgres_integration_test.go 用真库执行验证；跨方言的字符串字面量与
// 块注释边界由 raw_literal_integration_test.go 的执行矩阵覆盖。
func TestGrammar_CompileRaw_SQLStructure(t *testing.T) {
	tests := []struct {
		name    string
		grammar Grammar
		sqlStr  string
		args    []any
		wantSQL string
	}{
		{
			name: "MySQL 双引号字符串里的问号不算占位符", grammar: NewMySQLGrammar(),
			sqlStr: `SELECT "a?b", ?`, args: []any{1}, wantSQL: `SELECT "a?b", 1`,
		},
		{
			name: "MySQL 反引号标识符里的问号不算占位符", grammar: NewMySQLGrammar(),
			sqlStr: "SELECT `a?b`, ?", args: []any{1}, wantSQL: "SELECT `a?b`, 1",
		},
		{
			name: "SQLite 单引号内反斜杠不是转义符", grammar: NewSQLiteGrammar(),
			sqlStr: `SELECT 'a\' , ?`, args: []any{1}, wantSQL: `SELECT 'a\' , 1`,
		},
		{
			name: "MySQL 的 -- 注释里的问号不算占位符", grammar: NewMySQLGrammar(),
			sqlStr: "SELECT ? -- ?\n", args: []any{1}, wantSQL: "SELECT 1 -- ?\n",
		},
		{
			name: "MySQL 的 -- 后必须有空白才算注释", grammar: NewMySQLGrammar(),
			sqlStr: "SELECT ?--?", args: []any{1, 2}, wantSQL: "SELECT 1--2",
		},
		{
			name: "MySQL 的 # 行注释里的问号不算占位符", grammar: NewMySQLGrammar(),
			sqlStr: "SELECT ? # 注释里的 ? ", args: []any{1}, wantSQL: "SELECT 1 # 注释里的 ? ",
		},
		{
			name: "MySQL 连续 ?? 是两个占位符", grammar: NewMySQLGrammar(),
			sqlStr: "SELECT ??", args: []any{1, 2}, wantSQL: "SELECT 12",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, err := tt.grammar.CompileRaw(tt.sqlStr, tt.args)
			if err != nil {
				t.Fatalf("CompileRaw 返回错误: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL 不符：\n got: %q\nwant: %q", gotSQL, tt.wantSQL)
			}
		})
	}
}

// TestGrammar_CompileRaw_SingleStringSemantics 验证产物是单条自足 SQL：
// 参数全部内联后不再残留占位符（MySQL/SQLite 不再有 ?，PostgreSQL 不再有 $N）。
func TestGrammar_CompileRaw_SingleStringSemantics(t *testing.T) {
	tests := []struct {
		grammar Grammar
		sqlStr  string
	}{
		{NewMySQLGrammar(), "SELECT a, ? FROM t WHERE id = ?"},
		{NewPostgresGrammar(), `SELECT a, $1 FROM t WHERE id = $2`},
		{NewSQLiteGrammar(), "SELECT a, ? FROM t WHERE id = ?"},
	}
	for _, tt := range tests {
		gotSQL, err := tt.grammar.CompileRaw(tt.sqlStr, []any{"x", 1})
		if err != nil {
			t.Fatalf("%T: CompileRaw 返回错误: %v", tt.grammar, err)
		}
		// 用例输入数据不含 ? 与 $，故产物里出现它们即说明占位符未被内联（$ 用编号边界判定）。
		if strings.Contains(gotSQL, "?") {
			t.Errorf("%T: 产物不应残留占位符：%q", tt.grammar, gotSQL)
		}
		if containsNumberedPlaceholder(gotSQL) {
			t.Errorf("%T: 产物不应含 $N 编号：%q", tt.grammar, gotSQL)
		}
	}
}

// TestGrammar_CompileRaw_PlaceholderMismatch 验证占位符数量与参数数量不一致时返回
// ErrRawPlaceholderMismatch（消息含两侧实际数量）且产物为空串：
// MySQL/SQLite 按代码区的 ? 计数（连续 ?? 算两个）；PostgreSQL 认 $N 编号，
// 只含 ? 的输入会因"没有 $N"而报错——这正是"只认本方言占位符"的收口表现。
func TestGrammar_CompileRaw_PlaceholderMismatch(t *testing.T) {
	tests := []struct {
		name    string
		grammar Grammar
		sqlStr  string
		args    []any
		wantMsg string
	}{
		{
			name: "MySQL 参数过少", grammar: NewMySQLGrammar(),
			sqlStr: "UPDATE t SET a = ? WHERE id = ?", args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has 2 placeholder(s), got 1 arg(s)",
		},
		{
			name: "MySQL 参数过多", grammar: NewMySQLGrammar(),
			sqlStr: "UPDATE t SET a = ?", args: []any{1, 2},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has 1 placeholder(s), got 2 arg(s)",
		},
		{
			name: "MySQL 的 ?? 按两个占位符计", grammar: NewMySQLGrammar(),
			sqlStr: "SELECT ??", args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has 2 placeholder(s), got 1 arg(s)",
		},
		{
			name: "MySQL 字符串字面量里的问号不计数", grammar: NewMySQLGrammar(),
			sqlStr: `SELECT 'a?b'`, args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has 0 placeholder(s), got 1 arg(s)",
		},
		{
			name: "SQLite 参数过少", grammar: NewSQLiteGrammar(),
			sqlStr: "DELETE FROM t WHERE id = ?", args: nil,
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has 1 placeholder(s), got 0 arg(s)",
		},
		{
			name: "SQLite 的 ?? 按两个占位符计", grammar: NewSQLiteGrammar(),
			sqlStr: "SELECT 'a' ?? 'b'", args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has 2 placeholder(s), got 1 arg(s)",
		},
		{
			name: "PostgreSQL 参数过少", grammar: NewPostgresGrammar(),
			sqlStr: `SELECT * FROM "t" WHERE "a" = $1 AND "b" = $2`, args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: $2 is out of range for 1 arg(s)",
		},
		{
			name: "PostgreSQL 参数过多", grammar: NewPostgresGrammar(),
			sqlStr: `SELECT * FROM "t" WHERE "a" = $1`, args: []any{1, 2},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql references up to $1 but got 2 arg(s)",
		},
		{
			name: "PostgreSQL 的问号输入不再被当占位符", grammar: NewPostgresGrammar(),
			sqlStr: `SELECT * FROM "t" WHERE "a" = ?`, args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has no $N placeholder but got 1 arg(s)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, err := tt.grammar.CompileRaw(tt.sqlStr, tt.args)
			if !errors.Is(err, ErrRawPlaceholderMismatch) {
				t.Fatalf("错误未匹配 ErrRawPlaceholderMismatch：%v", err)
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("错误消息不符：\n got: %s\nwant: %s", err.Error(), tt.wantMsg)
			}
			if gotSQL != "" {
				t.Errorf("失败时产物应为空串，got %q", gotSQL)
			}
		})
	}
}

// TestGrammar_CompileRaw_ValueNotInlinable 验证无法内联的值返回 ErrRawValueNotInlinable：
// 类型不支持（struct/map/非字节切片/chan/complex）、NaN 与 Inf 无字面量表示、Valuer 自身报错；
// 错误消息含参数位置（从 1 起）。Expression 之外的未知类型一律报错，不做 fmt 兜底。
func TestGrammar_CompileRaw_ValueNotInlinable(t *testing.T) {
	tests := []struct {
		name       string
		args       []any
		wantDetail string
	}{
		{"结构体", []any{struct{ A int }{1}}, "unsupported type struct { A int }"},
		{"非 nil 指针不解引用", []any{new(int)}, "unsupported type *int"},
		{"map", []any{map[string]int{"a": 1}}, "unsupported type map[string]int"},
		{"非字节切片", []any{[]int{1, 2}}, "unsupported type []int"},
		{"channel", []any{make(chan int)}, "unsupported type chan int"},
		{"复数", []any{complex(1, 2)}, "unsupported type complex128"},
		{"NaN", []any{math.NaN()}, "NaN has no SQL literal form"},
		{"正无穷", []any{math.Inf(1)}, "+Inf has no SQL literal form"},
		{"负无穷", []any{math.Inf(-1)}, "-Inf has no SQL literal form"},
		{"Valuer 报错", []any{rawErrValuer{}}, "rawErrValuer.Value() failed: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, err := NewMySQLGrammar().CompileRaw("SELECT ?", tt.args)
			if !errors.Is(err, ErrRawValueNotInlinable) {
				t.Fatalf("错误未匹配 ErrRawValueNotInlinable：%v", err)
			}
			if !strings.Contains(err.Error(), "arg 1: ") || !strings.Contains(err.Error(), tt.wantDetail) {
				t.Errorf("错误消息应含位置与原因，got %q", err.Error())
			}
			if gotSQL != "" {
				t.Errorf("失败时产物应为空串，got %q", gotSQL)
			}
		})
	}

	t.Run("第二个参数报错时携带位置 2", func(t *testing.T) {
		_, err := NewPostgresGrammar().CompileRaw("SELECT $1, $2", []any{"ok", []int{1}})
		if !errors.Is(err, ErrRawValueNotInlinable) {
			t.Fatalf("错误未匹配 ErrRawValueNotInlinable：%v", err)
		}
		if !strings.Contains(err.Error(), "arg 2: ") {
			t.Errorf("错误消息应定位到第二个参数，got %q", err.Error())
		}
	})
}

// TestGrammar_CompileRaw_PostgresNumbered 验证 PostgreSQL 的 $N 形态输入
// （Builder 的 ToSelect/CompileSelect 产物即此形态）：按编号取参数内联、允许乱序与重复引用、
// 最大编号等于参数个数；编号模式下 `?` 是普通字符（jsonb 键存在操作符可直接写）。
func TestGrammar_CompileRaw_PostgresNumbered(t *testing.T) {
	tests := []struct {
		name    string
		sqlStr  string
		args    []any
		wantSQL string
	}{
		{
			name: "按编号内联", sqlStr: `SELECT * FROM "t" WHERE "id" = $1 AND "name" = $2`,
			args: []any{7, "x"}, wantSQL: `SELECT * FROM "t" WHERE "id" = 7 AND "name" = 'x'`,
		},
		{
			name: "乱序编号按编号取值", sqlStr: `SELECT * FROM "t" WHERE "a" = $2 AND "b" = $1`,
			args: []any{1, 2}, wantSQL: `SELECT * FROM "t" WHERE "a" = 2 AND "b" = 1`,
		},
		{
			name: "同一编号重复引用", sqlStr: `SELECT * FROM "t" WHERE "a" = $1 OR "b" = $1`,
			args: []any{"k"}, wantSQL: `SELECT * FROM "t" WHERE "a" = 'k' OR "b" = 'k'`,
		},
		{
			name: "区间内未被引用的参数合法", sqlStr: `SELECT * FROM "t" WHERE "b" = $2`,
			args: []any{"未用", 2}, wantSQL: `SELECT * FROM "t" WHERE "b" = 2`,
		},
		{
			name: "编号模式下的问号是普通字符", sqlStr: `SELECT * FROM "t" WHERE "options" ? $1 AND "id" = $2`,
			args: []any{"k", 7}, wantSQL: `SELECT * FROM "t" WHERE "options" ? 'k' AND "id" = 7`,
		},
		{
			name: "编号模式下不做 ?? 转义", sqlStr: `SELECT * FROM "t" WHERE "a" ?? $1`,
			args: []any{7}, wantSQL: `SELECT * FROM "t" WHERE "a" ?? 7`,
		},
		{
			name: "Expression 按编号内嵌", sqlStr: `UPDATE "t" SET "at" = $2 WHERE "id" = $1`,
			args: []any{1, NewExpression("NOW()")}, wantSQL: `UPDATE "t" SET "at" = NOW() WHERE "id" = 1`,
		},
		{
			name: "美元引用按普通文本保留", sqlStr: `SELECT $tag$ x $tag$`,
			args: nil, wantSQL: `SELECT $tag$ x $tag$`,
		},
		{
			name: "字符串字面量里的 $N 是字面内容", sqlStr: `SELECT '$1'`,
			args: nil, wantSQL: `SELECT '$1'`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, err := NewPostgresGrammar().CompileRaw(tt.sqlStr, tt.args)
			if err != nil {
				t.Fatalf("CompileRaw 返回错误: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL 不符：\n got: %q\nwant: %q", gotSQL, tt.wantSQL)
			}
		})
	}
}

// TestGrammar_CompileRaw_PostgresNumberedMismatch 验证 $N 形态的编号校验：
// 编号越界（含 $0）或最大编号不等于参数个数时返回 ErrRawPlaceholderMismatch（消息含编号与实际参数数）。
func TestGrammar_CompileRaw_PostgresNumberedMismatch(t *testing.T) {
	tests := []struct {
		name    string
		sqlStr  string
		args    []any
		wantMsg string
	}{
		{
			name: "$1 但没给参数", sqlStr: `SELECT $1`, args: nil,
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: $1 is out of range for 0 arg(s)",
		},
		{
			name: "参数个数多于最大编号", sqlStr: `SELECT $1`, args: []any{1, 2},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql references up to $1 but got 2 arg(s)",
		},
		{
			name: "编号跳空超出参数个数", sqlStr: `SELECT $3`, args: []any{1, 2},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: $3 is out of range for 2 arg(s)",
		},
		{
			name: "编号从 0 起非法", sqlStr: `SELECT $0`, args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: $0 is out of range for 1 arg(s)",
		},
		{
			name: "只有字面量里的 $N 时参数无处安放", sqlStr: `SELECT '$1'`, args: []any{1},
			wantMsg: "zcdb: raw SQL placeholder count does not match args count: sql has no $N placeholder but got 1 arg(s)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, err := NewPostgresGrammar().CompileRaw(tt.sqlStr, tt.args)
			if !errors.Is(err, ErrRawPlaceholderMismatch) {
				t.Fatalf("错误未匹配 ErrRawPlaceholderMismatch：%v", err)
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("错误消息不符：\n got: %s\nwant: %s", err.Error(), tt.wantMsg)
			}
			if gotSQL != "" {
				t.Errorf("失败时产物应为空串，got %q", gotSQL)
			}
		})
	}
}

// TestGrammar_CompileRaw_NumberedOnlyPostgres 验证 $N 只属于 PostgreSQL：
// MySQL/SQLite 不做编号识别，$1 只是普通文本，因此不传参数时原样返回、传参数时报数量不匹配。
func TestGrammar_CompileRaw_NumberedOnlyPostgres(t *testing.T) {
	for _, g := range []Grammar{NewMySQLGrammar(), NewSQLiteGrammar()} {
		gotSQL, err := g.CompileRaw("SELECT $1", nil)
		if err != nil {
			t.Fatalf("%T: 无参数时应原样返回，却报错: %v", g, err)
		}
		if gotSQL != "SELECT $1" {
			t.Errorf("%T: 应原样返回，got %q", g, gotSQL)
		}
		if _, err := g.CompileRaw("SELECT $1", []any{7}); !errors.Is(err, ErrRawPlaceholderMismatch) {
			t.Errorf("%T: 带参数时应报数量不匹配，got %v", g, err)
		}
	}
}

// rawErrValuer 是 Value() 返回错误的 driver.Valuer 桩，用于覆盖 Valuer 失败分支。
// 签名必须逐字匹配 driver.Valuer（driver.Value 是定义类型而非 any 别名）。
type rawErrValuer struct{}

// Value 固定返回错误，模拟驱动取值失败。
func (rawErrValuer) Value() (driver.Value, error) {
	return nil, errors.New("boom")
}

// TestGrammar_CompileRaw_ControlCharEscaping 验证 MySQL 默认转义模式下的控制字符字面量形态：
// 反斜杠、NUL、换行、回车、Ctrl-Z 逐个转义（这些分支只在 MySQL 的 escapeBackslash 路径可达），
// 单引号仍走双写。数值型/命名类型的字面量形态由跨方言执行矩阵负责，此处只锁转义文本。
func TestGrammar_CompileRaw_ControlCharEscaping(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantSQL string
	}{
		{name: "反斜杠与 NUL", value: "a\\b\x00c", wantSQL: `SELECT 'a\\b\0c'`},
		{name: "换行、回车与 Ctrl-Z", value: "a\nb\rc\x1ad", wantSQL: `SELECT 'a\nb\rc\Zd'`},
		{name: "控制字符与单引号混合", value: "'\n'", wantSQL: `SELECT '''\n'''`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSQL, err := NewMySQLGrammar().CompileRaw("SELECT ?", []any{tt.value})
			if err != nil {
				t.Fatalf("CompileRaw 返回错误: %v", err)
			}
			if gotSQL != tt.wantSQL {
				t.Errorf("SQL 不符：\n got: %q\nwant: %q", gotSQL, tt.wantSQL)
			}
		})
	}
}

// TestGrammar_CompileRaw_NamedNilByteSlice 验证命名字节切片类型的 nil 值写成 NULL：
// []byte(nil) 由类型 switch 的 []byte 分支处理，命名类型则落到 reflect.Slice 分支的 nil 判定，
// 两条路径都必须覆盖（后者只由本用例覆盖）。
func TestGrammar_CompileRaw_NamedNilByteSlice(t *testing.T) {
	gotSQL, err := NewMySQLGrammar().CompileRaw("SELECT ?", []any{rawNamedBytes(nil)})
	if err != nil {
		t.Fatalf("CompileRaw 返回错误: %v", err)
	}
	if gotSQL != "SELECT NULL" {
		t.Errorf("命名字节切片的 nil 值应写成 NULL：got %q", gotSQL)
	}
}

// FuzzCompileRaw 用占位符计数模型 fuzz 校验 CompileRaw：
// fuzzer 变异方言、SQL、参数个数与首个字符串参数，模型先算出应成功还是 ErrRawPlaceholderMismatch；
// 计数模型与被测实现共用 sqlScan 扫描器，因此它验证的是编译语义而非扫描器本身
// （扫描器边界由 TestSplitSQLCode 与 FuzzSplitSQLCode 覆盖）；
// 成功时再扫描产物代码区，断言占位符已全部内联（字符串/注释/引号标识符/美元引用内的不算）。
// 其余参数固定为含 ?、$1、引号、反斜杠、数值、bool、字节与时间，验证内联文本不会破坏后续扫描。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzCompileRaw$' -fuzztime=30s
func FuzzCompileRaw(f *testing.F) {
	type dialect struct {
		name    string
		grammar Grammar
		syntax  sqlScanSyntax
	}
	dialects := []dialect{
		{"MySQL", NewMySQLGrammar(), mysqlScanSyntax},
		{"PostgreSQL", NewPostgresGrammar(), pgScanSyntax},
		{"SQLite", NewSQLiteGrammar(), sqliteScanSyntax},
	}
	argPool := func(text string) []any {
		return []any{
			text,
			"q? $1 ' \\ \n",
			int64(-7),
			2.5,
			true,
			[]byte{0x00, 0x01, 0xff},
			rawTestTime,
		}
	}
	seeds := []string{
		"", "SELECT ?", "SELECT '?'", "SELECT $$ $1 $$",
		"SELECT /* ? */ 1", "SELECT 'a''?b'", "SELECT ? + ?",
		"SELECT $1", "SELECT $2, $1, $2", "SELECT $0", "??", "`?`", `"?"`,
		`E'\'' ?`, "-- ?\n?", "/* ? */ ?", "$tag$ $1 $tag$ ?",
	}
	for i := range dialects {
		for _, seed := range seeds {
			for _, argText := range []string{"plain", "a'b\\c? $1"} {
				for argCount := uint8(0); argCount <= 3; argCount++ {
					f.Add(uint8(i), seed, argText, argCount)
				}
			}
		}
	}
	f.Fuzz(func(t *testing.T, dialectIndex uint8, sqlStr, argText string, argCount uint8) {
		if len(sqlStr) > 64<<10 || len(argText) > 4<<10 {
			return
		}
		d := dialects[int(dialectIndex)%len(dialects)]
		args := argPool(argText)
		count := int(argCount) % (len(args) + 1)
		fuzzArgs := args[:count]
		wantOK := fuzzRawPlaceholdersMatch(sqlStr, d.syntax, d.name, count)
		out, err := d.grammar.CompileRaw(sqlStr, fuzzArgs)
		if !wantOK {
			if !errors.Is(err, ErrRawPlaceholderMismatch) {
				t.Fatalf("dialect=%s sql=%q args=%d: want placeholder mismatch, got err=%v out=%q",
					d.name, sqlStr, count, err, out)
			}
			return
		}
		if err != nil {
			t.Fatalf("dialect=%s sql=%q args=%d: want success, got %v", d.name, sqlStr, count, err)
		}
		if remaining := codePlaceholderCount(out, d.syntax, d.name); remaining != 0 {
			t.Fatalf("dialect=%s sql=%q args=%d: %d placeholder(s) left in code region: %q",
				d.name, sqlStr, count, remaining, out)
		}
	})
}

// fuzzRawPlaceholdersMatch 计算 CompileRaw 是否应成功：
// PostgreSQL 的每个 $N 必须在 1..argCount 内且最大编号等于 argCount；
// MySQL/SQLite 的代码区 ? 数量必须等于 argCount。
// 注意：这里复用被测的 splitSQLCode/numberedDigits，属于同源计数模型，
// 不能用来证明扫描器本身正确。
func fuzzRawPlaceholdersMatch(sqlStr string, syntax sqlScanSyntax, dialect string, argCount int) bool {
	if dialect == "PostgreSQL" {
		maxIndex := 0
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
				if err != nil || index < 1 || index > argCount {
					return false
				}
				if index > maxIndex {
					maxIndex = index
				}
				i += digits
			}
		}
		return maxIndex == argCount
	}
	return codePlaceholderCount(sqlStr, syntax, dialect) == argCount
}

// codePlaceholderCount 统计代码区中的占位符数量：PG 为 $N 引用次数，
// MySQL/SQLite 为 ? 数量；非代码区一律不计。
// 与 splitSQLCode 同源：用于验证编译产物，不作为扫描器的独立 oracle。
func codePlaceholderCount(sqlStr string, syntax sqlScanSyntax, dialect string) int {
	count := 0
	for _, segment := range splitSQLCode(sqlStr, syntax) {
		if !segment.code {
			continue
		}
		if dialect == "PostgreSQL" {
			for i := 0; i < len(segment.text); i++ {
				if digits := numberedDigits(segment.text, i); digits > 0 {
					count++
					i += digits
				}
			}
			continue
		}
		count += strings.Count(segment.text, "?")
	}
	return count
}
