package zcdb

import (
	"fmt"
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
	// CompileRaw 把一条带 ? 占位符的原始 SQL 与绑定参数内联成一条**可直接执行**的完整 SQL：
	// 产物不含占位符、也不需要参数，用于手工执行、日志留档或交给其它组件。
	//
	// 安全警示：产物把参数值写成了 SQL 文本，等于放弃占位符绑定这一注入防线。
	// 绝不允许把不可信输入（用户提交、请求体、密钥）当作 args 传入；Expression 参数同样
	// 原样内嵌，安全责任归调用方。
	//
	// 占位符与参数按出现顺序一一对应，数量不一致返回 ErrRawPlaceholderMismatch；
	// 参数值无法内联（类型不支持、NaN/Inf、Valuer 报错）返回 ErrRawValueNotInlinable，
	// 值内联规则见 rawLiteral.go（nil→NULL、字符串加引号转义、数值裸写、bool/时间/字节按方言）。
	// 占位符识别与数据库引擎的解析一致：只认代码区，字符串字面量、引号标识符、注释与
	// PostgreSQL 美元引用中的 `?` / `$N` 都是字面内容（见 sqlScan.go 的已知未覆盖语法）。
	// 形态随方言而定：MySQL/SQLite 认 `?`；PostgreSQL 只认 `$N`——“`?` 在 PostgreSQL 里是
	// jsonb 键存在操作符等运算符”，故 `?` 一律按普通字符处理。产物不追加 DAO 默认注释。
	CompileRaw(sqlStr string, args []any) (string, error)
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

// replaceRawExpression 将原始 SQL 代码区中的 ? 与 bindings 按位置对应：
// Expression 直接内嵌为 SQL 文本，其余值保留 ? 占位符。
// 字符串字面量、引号标识符与注释内的 ? 是字面内容，原样保留（与引擎的解析一致）。
func replaceRawExpression(sql string, bindings []any, syntax sqlScanSyntax) string {
	if len(bindings) == 0 {
		return sql
	}
	var buf strings.Builder
	buf.Grow(len(sql) + 8)
	bi := 0
	for _, segment := range splitSQLCode(sql, syntax) {
		if !segment.code {
			buf.WriteString(segment.text)
			continue
		}
		for i := 0; i < len(segment.text); i++ {
			if segment.text[i] == '?' && bi < len(bindings) {
				if expr, ok := bindings[bi].(Expression); ok {
					buf.WriteString(expr.Value())
				} else {
					buf.WriteByte('?')
				}
				bi++
			} else {
				buf.WriteByte(segment.text[i])
			}
		}
	}
	return buf.String()
}

// compileRaw 把 ? 风格方言（MySQL/SQLite）原始 SQL 代码区中的 ? 按位置替换为参数的字面量，
// 产出单条可直接执行的 SQL。字符串字面量、引号标识符与注释内的 ? 是字面内容，原样保留。
func compileRaw(sqlStr string, args []any, syntax sqlScanSyntax, style rawLiteralStyle) (string, error) {
	segments := splitSQLCode(sqlStr, syntax)
	placeholders := 0
	for _, segment := range segments {
		if segment.code {
			placeholders += strings.Count(segment.text, "?")
		}
	}
	if err := checkRawPlaceholders(placeholders, len(args)); err != nil {
		return "", err
	}
	var buf strings.Builder
	buf.Grow(len(sqlStr) + 16)
	argIndex := 0
	for _, segment := range segments {
		if !segment.code {
			buf.WriteString(segment.text)
			continue
		}
		for i := 0; i < len(segment.text); i++ {
			if segment.text[i] != '?' {
				buf.WriteByte(segment.text[i])
				continue
			}
			literal, err := inlineRawLiteral(args[argIndex], style, argIndex+1)
			if err != nil {
				return "", err
			}
			argIndex++
			buf.WriteString(literal)
		}
	}
	return buf.String(), nil
}

// checkRawPlaceholders 校验占位符数量与参数数量一致，不一致时返回携带两侧实际数量的
// ErrRawPlaceholderMismatch；此时编译产物为空串，调用方不会误用半成品 SQL。
func checkRawPlaceholders(placeholders, args int) error {
	if placeholders != args {
		return fmt.Errorf("%w: sql has %d placeholder(s), got %d arg(s)", ErrRawPlaceholderMismatch, placeholders, args)
	}
	return nil
}

// numberedDigits 返回 sqlStr[at] 处 $N 占位符的连续数字位数；该位置不是 $N 时返回 0。
func numberedDigits(sqlStr string, at int) int {
	if sqlStr[at] != '$' {
		return 0
	}
	digits := 0
	for i := at + 1; i < len(sqlStr) && sqlStr[i] >= '0' && sqlStr[i] <= '9'; i++ {
		digits++
	}
	return digits
}

// compileRawNumbered 编译 PostgreSQL 的 $N 形态原始 SQL：按编号取参数内联为字面量。
// 只在代码区识别 $N——字符串字面量、引号标识符、注释与美元引用内的 $N 是字面内容，原样保留；
// 同理，`?` 在任何位置都不是占位符（PostgreSQL 里它是 jsonb 键存在操作符等运算符）。
// 校验与 lib/pq 的执行契约一致：编号须落在 1..len(args) 内，且最大编号等于参数个数
// （允许同一编号重复引用，也允许区间内未被引用的参数）；不满足返回 ErrRawPlaceholderMismatch，
// 此时产物为空串。
func compileRawNumbered(sqlStr string, args []any, syntax sqlScanSyntax, style rawLiteralStyle) (string, error) {
	var buf strings.Builder
	buf.Grow(len(sqlStr) + 16)
	maxIndex := 0
	for _, segment := range splitSQLCode(sqlStr, syntax) {
		if !segment.code {
			buf.WriteString(segment.text)
			continue
		}
		text := segment.text
		for i := 0; i < len(text); i++ {
			digits := numberedDigits(text, i)
			if digits == 0 {
				buf.WriteByte(text[i])
				continue
			}
			placeholder := text[i : i+1+digits]
			index, err := strconv.Atoi(text[i+1 : i+1+digits])
			if err != nil || index < 1 || index > len(args) {
				return "", fmt.Errorf("%w: %s is out of range for %d arg(s)", ErrRawPlaceholderMismatch, placeholder, len(args))
			}
			literal, err := inlineRawLiteral(args[index-1], style, index)
			if err != nil {
				return "", err
			}
			if index > maxIndex {
				maxIndex = index
			}
			buf.WriteString(literal)
			i += digits
		}
	}
	if maxIndex == 0 && len(args) > 0 {
		return "", fmt.Errorf("%w: sql has no $N placeholder but got %d arg(s)", ErrRawPlaceholderMismatch, len(args))
	}
	if maxIndex != len(args) {
		return "", fmt.Errorf("%w: sql references up to $%d but got %d arg(s)", ErrRawPlaceholderMismatch, maxIndex, len(args))
	}
	return buf.String(), nil
}
