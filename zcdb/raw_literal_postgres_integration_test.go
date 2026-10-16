// 本文件为 PostgreSQL 方言特有的内联（rawLiteral.go / CompileRaw）集成测试：
// 覆盖 jsonb 的 `?` 键存在操作符——它在 PostgreSQL 里是普通运算符而非占位符，
// 无论编译产物里是否同时带 $N，CompileRaw 都必须把它当字面内容并保持产物可执行。
// 环境依赖：本地 PostgreSQL（DSN 见 openPgTestDB，默认 host=127.0.0.1:5432 user=postgres password=root），
// 不可达时由 openPgTestDB 自动 Skip。
// docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15

package zcdb

import (
	"context"
	"strings"
	"testing"
)

// TestGrammar_CompileRaw_PostgresSyntaxBoundaries 验证 PostgreSQL 特有语法边界同样只在代码区识别 $N，
// 且产物被真库接受、值可回读：美元引用的内容整体保留、嵌套块注释内的 $N 不被替换、
// E'...' 内的转义引号不影响后续占位符。这三条无法由跨方言执行矩阵表达（属 PG 专有语法）。
func TestGrammar_CompileRaw_PostgresSyntaxBoundaries(t *testing.T) {
	dao := openPgTestDB(t)
	grammar := NewPostgresGrammar()
	tests := []struct {
		name   string
		sqlStr string
		args   []any
		want   []string
	}{
		{
			// 美元引用体内的 $1 是数据（整体保留），引用外的 $1 才内联。
			name:   "美元引用的内容整体保留",
			sqlStr: `SELECT $tag$ x $1 $tag$, $1`,
			args:   []any{7},
			want:   []string{" x $1 ", "7"},
		},
		{
			// 嵌套注释整体跨越到最外层 */：内层 */ 之后那个未配对的单引号仍属注释，故语句合法；
			// 若扫描器不识别嵌套，注释会在第一个 */ 处结束，单引号被当成字符串起始、把末尾的 $1
			// 吞进"未闭合字符串"，编译阶段就会报"没有 $N 占位符"——这条用例因此能区分嵌套语义。
			name:   "嵌套块注释里的 $N 不被替换",
			sqlStr: `SELECT 1 /* /* */ ' */ , $1`,
			args:   []any{7},
			want:   []string{"1", "7"},
		},
		{
			// E'\'' 是一个完整字面量（内容为单个单引号），其后的 $1 必须照常内联。
			name:   "E 字符串内的转义引号不影响后续占位符",
			sqlStr: `SELECT E'\'' , $1`,
			args:   []any{1},
			want:   []string{"'", "1"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			compiled, err := grammar.CompileRaw(tt.sqlStr, tt.args)
			if err != nil {
				t.Fatalf("CompileRaw 编译失败: sql=%s args=%#v err=%v", tt.sqlStr, tt.args, err)
			}
			rows, err := dao.Query(context.Background(), compiled)
			if err != nil {
				t.Fatalf("数据库未接受 CompileRaw 产物: sql=%s err=%v", compiled, err)
			}
			defer func() {
				if err := rows.Close(); err != nil {
					t.Errorf("close rows: %v", err)
				}
			}()
			got, ok := scanRawRow(t, rows)
			if !ok {
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("列数不符：got %#v，want %#v（sql=%s）", got, tt.want, compiled)
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Errorf("第 %d 列不符：got %q，want %q（sql=%s）", i+1, got[i], tt.want[i], compiled)
				}
			}
		})
	}
}

// TestGrammar_CompileRaw_PostgresJsonbOperator 验证 jsonb `?` 操作符在两条路径下都不被当成占位符：
// 有绑定参数时产物含 `?` 字面字符与内联后的参数；无绑定参数时产物与编译产物逐字节相同。
// 两种情况的无参执行都必须成功，读取结果证明查询语义未被改写。
func TestGrammar_CompileRaw_PostgresJsonbOperator(t *testing.T) {
	dao := openPgTestDB(t)
	ctx := context.Background()
	const table = "compile_raw_pg_jsonb"
	crossDialectDrop(t, dao, table)
	crossDialectExec(t, dao, `CREATE TABLE compile_raw_pg_jsonb (id INTEGER PRIMARY KEY, options JSONB)`)
	crossDialectExec(t, dao, `INSERT INTO compile_raw_pg_jsonb (id, options) VALUES (1, '{"k": 1}')`)

	// 有绑定：WhereRaw 里的 ?? 是字面 ? 的写法，编译产物为 `"options" ? $1`。
	withBinding, args, err := dao.Builder().Table(table).Select("id").
		WhereRaw(`"options" ?? ?`, "k").ToSelect()
	if err != nil {
		t.Fatalf("ToSelect: %v", err)
	}
	if !strings.Contains(withBinding, `"options" ? $1`) {
		t.Fatalf("编译产物应为 jsonb 字面 ? 加 $1 占位符: %s", withBinding)
	}
	inlineBound, err := dao.Grammar().CompileRaw(withBinding, args)
	if err != nil {
		t.Fatalf("CompileRaw: %v", err)
	}
	if !strings.Contains(inlineBound, `"options" ? 'k'`) {
		t.Errorf("jsonb 的 ? 应保持字面、$1 应内联为 'k': %s", inlineBound)
	}
	boundRows, err := dao.Query(ctx, inlineBound)
	if err != nil {
		t.Fatalf("无参执行失败: sql=%s err=%v", inlineBound, err)
	}
	assertCommentScalarRows(t, boundRows, 1)

	// 无绑定：编译产物里只有一个字面 ?，它必须原样保留而不是被当作占位符。
	noBinding, noArgs, err := dao.Builder().Table(table).Select("id").
		WhereRaw(`"options" ?? 'k'`).ToSelect()
	if err != nil {
		t.Fatalf("ToSelect: %v", err)
	}
	if len(noArgs) != 0 {
		t.Fatalf("该语句不应有绑定参数: %#v", noArgs)
	}
	inlineFree, err := dao.Grammar().CompileRaw(noBinding, noArgs)
	if err != nil {
		t.Fatalf("CompileRaw（无绑定）: %v", err)
	}
	if inlineFree != noBinding {
		t.Errorf("无绑定时产物应与编译产物逐字节相同：\n got: %s\nwant: %s", inlineFree, noBinding)
	}
	freeRows, err := dao.Query(ctx, inlineFree)
	if err != nil {
		t.Fatalf("无参执行失败: sql=%s err=%v", inlineFree, err)
	}
	assertCommentScalarRows(t, freeRows, 1)
}
