package zcdb

import (
	"strconv"
	"strings"
)

// Grammar 定义了 SQL 编译器接口。
// 不同数据库方言实现此接口以生成对应语法的 SQL。
type Grammar interface {
	// CompileSelect 编译 SELECT 查询
	CompileSelect(b *Builder, columns []SelectColumn) string
	// CompileInsert 编译 INSERT 语句
	CompileInsert(b *Builder, columns []string, rows [][]any) string
	// CompileInsertOrIgnore 编译 INSERT OR IGNORE 语句
	CompileInsertOrIgnore(b *Builder, columns []string, rows [][]any) string
	// CompileUpsert 编译 UPSERT 语句 (INSERT ... ON DUPLICATE KEY UPDATE / ON CONFLICT DO UPDATE)
	// uniqueBy: 唯一索引列（PostgreSQL/SQLite 需要）
	// updateColumns: 冲突时要更新的列
	// updateValues: 保留参数，当前三方实现均忽略（更新值统一由 VALUES()/EXCLUDED 派生）
	CompileUpsert(b *Builder, columns []string, rows [][]any, uniqueBy []string, updateColumns []string, updateValues []any) string
	// CompileInsertUsing 编译 INSERT INTO ... SELECT 语句
	CompileInsertUsing(b *Builder, columns []string, sub *Builder) string
	// CompileInsertOrIgnoreUsing 编译忽略冲突的 INSERT INTO ... SELECT 语句
	CompileInsertOrIgnoreUsing(b *Builder, columns []string, sub *Builder) string
	// CompileUpdate 编译 UPDATE 语句
	CompileUpdate(b *Builder, columns []string, values []any) string
	// UpdateSetBeforeJoin 报告 UPDATE 语句中 SET 子句的绑定参数是否出现在 JOIN 条件之前。
	// MySQL 的 UPDATE ... JOIN ... SET ... 中 JOIN 条件在前（返回 false）；
	// PostgreSQL/SQLite 的 UPDATE ... SET ... FROM ... WHERE ... 中 SET 在前（返回 true）。
	UpdateSetBeforeJoin() bool
	// CompileDelete 编译 DELETE 语句
	CompileDelete(b *Builder) string
	// CompileDeleteJoin 编译按关联条件删除的 DELETE 语句（方言实现路径不同）
	CompileDeleteJoin(b *Builder) string
	// CompileTruncate 编译 TRUNCATE 语句
	CompileTruncate(b *Builder) string
	// WrapColumn 引用列标识符
	WrapColumn(column string) string
	// WrapTable 引用表标识符
	WrapTable(table string) string
	// CompileRandom 返回随机排序的 SQL 表达式
	CompileRandom() string
	// CompileWhereDate 返回 WhereDate 的日期比较表达式（方言分支：MySQL date(col) / PG col::date / SQLite strftime）
	CompileWhereDate(column string) string
}

// intToStr 将 int 转换为字符串。
// 委托 strconv.Itoa 实现：覆盖全部边界（含 math.MinInt——早期手写版本对 MinInt
// 取负溢出仍为负，负分支会无限写入 '-' 造成死循环）。
func intToStr(n int) string {
	return strconv.Itoa(n)
}

// replaceRawExpression 将原始 SQL 中的 ? 与 bindings 按位置对应：
// Expression 直接内嵌为 SQL 文本，其余值保留 ? 占位符。
// 用于 MySQL/SQLite 方言（? 占位符风格）。
func replaceRawExpression(sql string, bindings []any) string {
	if len(bindings) == 0 {
		return sql
	}
	var buf strings.Builder
	buf.Grow(len(sql) + 8)
	bi := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' && bi < len(bindings) {
			if expr, ok := bindings[bi].(Expression); ok {
				buf.WriteString(expr.Value())
			} else {
				buf.WriteByte('?')
			}
			bi++
		} else {
			buf.WriteByte(sql[i])
		}
	}
	return buf.String()
}
