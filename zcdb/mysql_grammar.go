package zcdb

import (
	"strings"
)

// MySQLGrammar MySQL 方言编译器
type MySQLGrammar struct{}

// NewMySQLGrammar 创建一个 MySQL 语法编译器
func NewMySQLGrammar() *MySQLGrammar {
	return &MySQLGrammar{}
}

// CompileRandom 返回 MySQL 随机排序表达式
func (g *MySQLGrammar) CompileRandom() string {
	return "RAND()"
}

// UpdateSetBeforeJoin MySQL 的 UPDATE ... JOIN ... SET ... 中 JOIN 条件在 SET 之前。
func (g *MySQLGrammar) UpdateSetBeforeJoin() bool {
	return false
}

// WrapColumn 使用反引号引用列名。
// 包含括号的表达式（如 COUNT(*)、YEAR(col)）不会被引用。
func (g *MySQLGrammar) WrapColumn(column string) string {
	if column == "*" {
		return column
	}
	// 包含 ( 的视为原始表达式，不加引号
	if strings.Contains(column, "(") {
		return column
	}
	// 处理 column AS alias 形式（先于点号检查，避免 "表.列 AS 别名" 的别名被点号分支吞掉）
	if idx := strings.Index(strings.ToLower(column), " as "); idx != -1 {
		col := column[:idx]
		alias := column[idx+4:]
		return g.WrapColumn(strings.TrimSpace(col)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	// 处理 table.column 形式
	if strings.Contains(column, ".") {
		parts := strings.SplitN(column, ".", 2)
		return g.WrapTable(parts[0]) + "." + g.wrapValue(parts[1])
	}
	return g.wrapValue(column)
}

// WrapTable 使用反引号引用表名
func (g *MySQLGrammar) WrapTable(table string) string {
	// 对 ToLower 后的表名统一做 " as " 判定（大小写不敏感，避免 "orders As o" 等混合大小写漏判）
	if idx := strings.Index(strings.ToLower(table), " as "); idx != -1 {
		name := table[:idx]
		alias := table[idx+4:]
		return g.wrapValue(strings.TrimSpace(name)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	return g.wrapValue(table)
}

// wrapValue 反引号包裹标识符，内部反引号加倍转义；"*" 原样返回。
func (g *MySQLGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

// CompileWhereDate 返回 WhereDate 的日期比较表达式（MySQL 用 date() 函数提取日期部分）。
func (g *MySQLGrammar) CompileWhereDate(column string) string {
	return "date(" + g.WrapColumn(column) + ")"
}

// CompileSelect 编译 SELECT 查询
func (g *MySQLGrammar) CompileSelect(b *Builder, columns []SelectColumn) string {
	var sql strings.Builder

	// SELECT [DISTINCT]
	sql.WriteString("SELECT ")
	if b.distinct {
		sql.WriteString("DISTINCT ")
	}

	// columns
	if len(columns) == 0 && len(b.selectSubs) == 0 {
		sql.WriteString("*")
	} else {
		first := true
		for _, col := range columns {
			if !first {
				sql.WriteString(", ")
			}
			if col.Raw {
				sql.WriteString(col.Value)
			} else {
				sql.WriteString(g.WrapColumn(col.Value))
			}
			first = false
		}
		for _, ss := range b.selectSubs {
			if !first {
				sql.WriteString(", ")
			}
			subSQL := ss.Query.grammar.CompileSelect(ss.Query, ss.Query.columns)
			sql.WriteString("(" + subSQL + ") AS " + g.wrapValue(ss.Alias))
			first = false
		}
	}

	// FROM
	sql.WriteString(" FROM ")
	if b.tableSub != nil {
		subSQL := b.tableSub.grammar.CompileSelect(b.tableSub, b.tableSub.columns)
		sql.WriteString("(" + subSQL + ") AS " + g.wrapValue(b.tableAlias))
	} else {
		sql.WriteString(g.WrapTable(b.table))
	}

	// JOINS
	if len(b.joins) > 0 {
		for _, join := range b.joins {
			sql.WriteString(" ")
			sql.WriteString(g.compileJoin(join))
		}
	}

	// WHERE
	if whereSQL := g.compileWheres(b); whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	// GROUP BY
	if len(b.groups) > 0 {
		sql.WriteString(" GROUP BY ")
		for i, group := range b.groups {
			if i > 0 {
				sql.WriteString(", ")
			}
			if group.Raw != "" {
				sql.WriteString(replaceRawExpression(group.Raw, group.Bindings))
			} else {
				sql.WriteString(g.WrapColumn(group.Column))
			}
		}
	}

	// HAVING
	if len(b.havings) > 0 {
		sql.WriteString(" HAVING ")
		sql.WriteString(g.compileHavings(b))
	}

	// ORDER BY / LIMIT / OFFSET（UNION 时必须追加在 UNION 之后，故抽为内联闭包）
	appendOrderLimitOffset := func(sb *strings.Builder) {
		// ORDER BY
		if len(b.orders) > 0 {
			sb.WriteString(" ORDER BY ")
			for i, order := range b.orders {
				if i > 0 {
					sb.WriteString(", ")
				}
				if order.Raw != "" {
					sb.WriteString(order.Raw)
				} else {
					sb.WriteString(g.WrapColumn(order.Column))
					sb.WriteString(" ")
					sb.WriteString(order.Direction)
				}
			}
		}

		// LIMIT
		if b.limit > 0 {
			sb.WriteString(" LIMIT ")
			sb.WriteString(intToStr(b.limit))
		}

		// OFFSET
		if b.offset > 0 {
			// MySQL 的 OFFSET 必须配合 LIMIT，无 LIMIT 时使用最大行数
			if b.limit == 0 {
				sb.WriteString(" LIMIT 18446744073709551615")
			}
			sb.WriteString(" OFFSET ")
			sb.WriteString(intToStr(b.offset))
		}
	}

	// UNION
	if len(b.unions) > 0 {
		result := "(" + sql.String() + ")"
		for _, union := range b.unions {
			if union.All {
				result += " UNION ALL "
			} else {
				result += " UNION "
			}
			unionSQL := union.Query.grammar.CompileSelect(union.Query, union.Query.columns)
			result += "(" + unionSQL + ")"
		}
		// ORDER BY / LIMIT / OFFSET / LOCK 必须放在 UNION 之后
		var tail strings.Builder
		appendOrderLimitOffset(&tail)
		result += tail.String()
		if b.lockClause != "" {
			result += " " + b.lockClause
		}
		return result
	}

	// LOCK
	appendOrderLimitOffset(&sql)
	if b.lockClause != "" {
		sql.WriteString(" ")
		sql.WriteString(b.lockClause)
	}

	return sql.String()
}

// CompileInsert 编译 INSERT 语句
func (g *MySQLGrammar) CompileInsert(b *Builder, columns []string, rows [][]any) string {
	var sql strings.Builder

	sql.WriteString("INSERT INTO ")
	sql.WriteString(g.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
	}
	sql.WriteString(") VALUES ")

	for i, row := range rows {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString("(")
		for j := range row {
			if j > 0 {
				sql.WriteString(", ")
			}
			// Expression 直接内联，不生成占位符
			if expr, ok := row[j].(Expression); ok {
				sql.WriteString(expr.Value())
			} else {
				sql.WriteString("?")
			}
		}
		sql.WriteString(")")
	}

	return sql.String()
}

// CompileInsertOrIgnore 编译 INSERT IGNORE 语句 (MySQL)
func (g *MySQLGrammar) CompileInsertOrIgnore(b *Builder, columns []string, rows [][]any) string {
	var sql strings.Builder

	sql.WriteString("INSERT IGNORE INTO ")
	sql.WriteString(g.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
	}
	sql.WriteString(") VALUES ")

	for i, row := range rows {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString("(")
		for j := range row {
			if j > 0 {
				sql.WriteString(", ")
			}
			// Expression 直接内联，不生成占位符
			if expr, ok := row[j].(Expression); ok {
				sql.WriteString(expr.Value())
			} else {
				sql.WriteString("?")
			}
		}
		sql.WriteString(")")
	}

	return sql.String()
}

// CompileUpsert 编译 MySQL 的 INSERT ... ON DUPLICATE KEY UPDATE 语句
func (g *MySQLGrammar) CompileUpsert(b *Builder, columns []string, rows [][]any, uniqueBy []string, updateColumns []string, _ []any) string {
	var sql strings.Builder

	// INSERT 部分
	sql.WriteString("INSERT INTO ")
	sql.WriteString(g.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
	}
	sql.WriteString(") VALUES ")

	for i, row := range rows {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString("(")
		for j := range row {
			if j > 0 {
				sql.WriteString(", ")
			}
			// Expression 直接内联，不生成占位符
			if expr, ok := row[j].(Expression); ok {
				sql.WriteString(expr.Value())
			} else {
				sql.WriteString("?")
			}
		}
		sql.WriteString(")")
	}

	// ON DUPLICATE KEY UPDATE 部分
	// updateColumns 为空（全部插入列均为 uniqueBy）时退化为 no-op 自赋值，等价冲突时不更新
	if len(updateColumns) == 0 {
		if len(uniqueBy) > 0 {
			updateColumns = uniqueBy[:1]
		} else {
			updateColumns = columns[:1]
		}
	}
	sql.WriteString(" ON DUPLICATE KEY UPDATE ")
	for i, col := range updateColumns {
		if i > 0 {
			sql.WriteString(", ")
		}
		wrapped := g.WrapColumn(col)
		sql.WriteString(wrapped)
		sql.WriteString(" = VALUES(")
		sql.WriteString(wrapped)
		sql.WriteString(")")
	}

	return sql.String()
}

// CompileInsertUsing 编译 INSERT INTO ... SELECT 语句
func (g *MySQLGrammar) CompileInsertUsing(b *Builder, columns []string, sub *Builder) string {
	var sql strings.Builder

	sql.WriteString("INSERT INTO ")
	sql.WriteString(g.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
	}
	sql.WriteString(") ")
	sql.WriteString(sub.grammar.CompileSelect(sub, sub.columns))

	return sql.String()
}

// CompileInsertOrIgnoreUsing 编译 INSERT IGNORE INTO ... SELECT 语句（冲突时静默跳过）
func (g *MySQLGrammar) CompileInsertOrIgnoreUsing(b *Builder, columns []string, sub *Builder) string {
	var sql strings.Builder

	sql.WriteString("INSERT IGNORE INTO ")
	sql.WriteString(g.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
	}
	sql.WriteString(") ")
	sql.WriteString(sub.grammar.CompileSelect(sub, sub.columns))

	return sql.String()
}

// CompileUpdate 编译 UPDATE 语句
func (g *MySQLGrammar) CompileUpdate(b *Builder, columns []string, values []any) string {
	var sql strings.Builder

	sql.WriteString("UPDATE ")
	sql.WriteString(g.WrapTable(b.table))

	// JOINS (MySQL 支持 UPDATE ... JOIN ...)
	if len(b.joins) > 0 {
		for _, join := range b.joins {
			sql.WriteString(" ")
			sql.WriteString(g.compileJoin(join))
		}
	}

	// SET
	sql.WriteString(" SET ")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
		sql.WriteString(" = ")
		// 检查值是否为 Expression
		if expr, ok := values[i].(Expression); ok {
			sql.WriteString(expr.Value())
		} else {
			sql.WriteString("?")
		}
	}

	// WHERE
	if whereSQL := g.compileWheres(b); whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	// ORDER BY (MySQL 支持)
	if len(b.orders) > 0 {
		sql.WriteString(" ORDER BY ")
		for i, order := range b.orders {
			if i > 0 {
				sql.WriteString(", ")
			}
			if order.Raw != "" {
				sql.WriteString(order.Raw)
			} else {
				sql.WriteString(g.WrapColumn(order.Column))
				sql.WriteString(" ")
				sql.WriteString(order.Direction)
			}
		}
	}

	// LIMIT (MySQL 支持)
	if b.limit > 0 {
		sql.WriteString(" LIMIT ")
		sql.WriteString(intToStr(b.limit))
	}

	return sql.String()
}

// CompileDeleteJoin 编译按关联条件删除的 DELETE 语句（多表 DELETE 直译）。
// 例如：DELETE `users` FROM `users` INNER JOIN `orders` ON ... WHERE ...
func (g *MySQLGrammar) CompileDeleteJoin(b *Builder) string {
	var sql strings.Builder

	sql.WriteString("DELETE ")
	sql.WriteString(g.WrapTable(b.table))
	sql.WriteString(" FROM ")
	sql.WriteString(g.WrapTable(b.table))

	for _, join := range b.joins {
		sql.WriteString(" ")
		sql.WriteString(g.compileJoin(join))
	}

	// WHERE
	if whereSQL := g.compileWheres(b); whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	return sql.String()
}

// CompileDelete 编译 DELETE 语句
func (g *MySQLGrammar) CompileDelete(b *Builder) string {
	var sql strings.Builder

	sql.WriteString("DELETE FROM ")
	sql.WriteString(g.WrapTable(b.table))

	// WHERE
	if whereSQL := g.compileWheres(b); whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	// ORDER BY (MySQL 支持)
	if len(b.orders) > 0 {
		sql.WriteString(" ORDER BY ")
		for i, order := range b.orders {
			if i > 0 {
				sql.WriteString(", ")
			}
			if order.Raw != "" {
				sql.WriteString(order.Raw)
			} else {
				sql.WriteString(g.WrapColumn(order.Column))
				sql.WriteString(" ")
				sql.WriteString(order.Direction)
			}
		}
	}

	// LIMIT (MySQL 支持)
	if b.limit > 0 {
		sql.WriteString(" LIMIT ")
		sql.WriteString(intToStr(b.limit))
	}

	return sql.String()
}

// CompileTruncate 编译 TRUNCATE 语句
func (g *MySQLGrammar) CompileTruncate(b *Builder) string {
	return "TRUNCATE TABLE " + g.WrapTable(b.table)
}

// compileWheres 编译 WHERE 子句
func (g *MySQLGrammar) compileWheres(b *Builder) string {
	if len(b.wheres) == 0 {
		return ""
	}

	var parts []string
	for _, w := range b.wheres {
		var clause string
		switch w.Type {
		case WhereTypeBasic:
			clause = g.compileWhereBasic(w)
		case WhereTypeIn:
			clause = g.compileWhereIn(w)
		case WhereTypeNotIn:
			clause = g.compileWhereNotIn(w)
		case WhereTypeNull:
			clause = g.WrapColumn(w.Column) + " IS NULL"
		case WhereTypeNotNull:
			clause = g.WrapColumn(w.Column) + " IS NOT NULL"
		case WhereTypeBetween:
			clause = g.WrapColumn(w.Column) + " BETWEEN ? AND ?"
		case WhereTypeNotBetween:
			clause = g.WrapColumn(w.Column) + " NOT BETWEEN ? AND ?"
		case WhereTypeRaw:
			clause = replaceRawExpression(w.SQL, w.Bindings)
		case WhereTypeNested:
			if w.Nested != nil {
				nested := g.compileWheres(w.Nested)
				if nested != "" {
					if w.Not {
						clause = "NOT (" + nested + ")"
					} else {
						clause = "(" + nested + ")"
					}
				}
			}
		case WhereTypeColumn:
			clause = g.WrapColumn(w.Column) + " " + w.Operator + " " + g.WrapColumn(w.Second)
		case WhereTypeExists:
			if w.Nested != nil {
				subSQL := w.Nested.grammar.CompileSelect(w.Nested, w.Nested.columns)
				clause = "EXISTS (" + subSQL + ")"
			}
		case WhereTypeNotExists:
			if w.Nested != nil {
				subSQL := w.Nested.grammar.CompileSelect(w.Nested, w.Nested.columns)
				clause = "NOT EXISTS (" + subSQL + ")"
			}
		case WhereTypeSub:
			if w.Sub != nil {
				subSQL := w.Sub.grammar.CompileSelect(w.Sub, w.Sub.columns)
				clause = g.WrapColumn(w.Column) + " " + w.Operator + " (" + subSQL + ")"
			}
		case WhereTypeInSub:
			if w.Sub != nil {
				subSQL := w.Sub.grammar.CompileSelect(w.Sub, w.Sub.columns)
				clause = g.WrapColumn(w.Column) + " IN (" + subSQL + ")"
			}
		case WhereTypeNotInSub:
			if w.Sub != nil {
				subSQL := w.Sub.grammar.CompileSelect(w.Sub, w.Sub.columns)
				clause = g.WrapColumn(w.Column) + " NOT IN (" + subSQL + ")"
			}
		case WhereTypeLike:
			// 区分大小写时加 BINARY 前缀（MySQL 默认 LIKE 不区分大小写）
			prefix := ""
			if w.CaseSensitive {
				prefix = "BINARY "
			}
			if expr, ok := w.Value.(Expression); ok {
				clause = prefix + g.WrapColumn(w.Column) + " LIKE " + expr.Value()
			} else {
				clause = prefix + g.WrapColumn(w.Column) + " LIKE ?"
			}
		case WhereTypeNotLike:
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " NOT LIKE " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " NOT LIKE ?"
			}
		case WhereTypeNullSafe:
			// 空安全相等：MySQL 用 <=> 操作符
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " <=> " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " <=> ?"
			}
		case WhereTypeNullSafeNot:
			if expr, ok := w.Value.(Expression); ok {
				clause = "NOT " + g.WrapColumn(w.Column) + " <=> " + expr.Value()
			} else {
				clause = "NOT " + g.WrapColumn(w.Column) + " <=> ?"
			}
		}

		if clause == "" {
			continue
		}

		// 以 parts 是否为空判定首条有效子句：前序子句可能编译为空（空 Raw/空嵌套组）被跳过，
		// 若按下标判定会产生 "WHERE AND ..." 悬挂连接词
		if len(parts) == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, w.Boolean+" "+clause)
		}
	}

	return strings.Join(parts, " ")
}

// compileWhereBasic 编译 basic 条件（column op value）。
// nil 特判：= nil / != nil / <> nil 编译为 IS NULL / IS NOT NULL，防止永假的 = NULL；
// Expression 直接内嵌，其余值生成 ? 占位符。
func (g *MySQLGrammar) compileWhereBasic(w WhereClause) string {
	// nil 特判：= nil / != nil / <> nil 编译为 IS NULL / IS NOT NULL，防止永假的 = NULL
	if w.Value == nil {
		switch w.Operator {
		case "=":
			return g.WrapColumn(w.Column) + " IS NULL"
		case "!=", "<>":
			return g.WrapColumn(w.Column) + " IS NOT NULL"
		}
	}
	if expr, ok := w.Value.(Expression); ok {
		return g.WrapColumn(w.Column) + " " + w.Operator + " " + expr.Value()
	}
	return g.WrapColumn(w.Column) + " " + w.Operator + " ?"
}

// compileWhereIn 编译 IN 条件；空值列表编译为 0 = 1（恒假）。
func (g *MySQLGrammar) compileWhereIn(w WhereClause) string {
	if len(w.Values) == 0 {
		return "0 = 1" // 空 IN 等价于 false
	}
	placeholders := make([]string, len(w.Values))
	for i := range w.Values {
		placeholders[i] = "?"
	}
	return g.WrapColumn(w.Column) + " IN (" + strings.Join(placeholders, ", ") + ")"
}

// compileWhereNotIn 编译 NOT IN 条件；空值列表编译为 1 = 1（恒真）。
func (g *MySQLGrammar) compileWhereNotIn(w WhereClause) string {
	if len(w.Values) == 0 {
		return "1 = 1" // 空 NOT IN 等价于 true
	}
	placeholders := make([]string, len(w.Values))
	for i := range w.Values {
		placeholders[i] = "?"
	}
	return g.WrapColumn(w.Column) + " NOT IN (" + strings.Join(placeholders, ", ") + ")"
}

// compileJoin 编译 JOIN 子句。
// 嵌套 join 组（join.Joins）编译为带括号的 join 组：
// INNER JOIN (表 INNER JOIN 子表 ON ...) ON ...
func (g *MySQLGrammar) compileJoin(join JoinClause) string {
	var joinType string
	switch join.Type {
	case JoinTypeInner:
		joinType = "INNER JOIN"
	case JoinTypeLeft:
		joinType = "LEFT JOIN"
	case JoinTypeRight:
		joinType = "RIGHT JOIN"
	case JoinTypeCross:
		joinType = "CROSS JOIN"
	case JoinTypeCrossOn:
		// MySQL 支持 CROSS JOIN ... ON ...
		joinType = "CROSS JOIN"
	}

	// 目标表：带嵌套 join 组时加括号
	tablePart := g.joinTable(join)
	if len(join.Joins) > 0 {
		for _, inner := range join.Joins {
			tablePart += " " + g.compileJoin(inner)
		}
		tablePart = "(" + tablePart + ")"
	}

	result := joinType + " " + tablePart
	if join.Type != JoinTypeCross && len(join.Conditions) > 0 {
		result += " ON " + g.compileJoinConditions(join.Conditions)
	}
	return result
}

// compileJoinConditions 编译 ON 条件列表（含新增的 null/in/inSub/exists/subValue/nested 类型）。
func (g *MySQLGrammar) compileJoinConditions(conditions []JoinCondition) string {
	var parts []string
	for _, cond := range conditions {
		var clause string
		switch cond.Type {
		case "column":
			clause = g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.WrapColumn(cond.Second)
		case "value":
			// nil 特判：= nil / != nil / <> nil 编译为 IS NULL / IS NOT NULL（与 Builder.Where 语义对齐）；
			// 绑定收集侧 collectJoinConditionBindings 对这三种情形跳过绑定，二者须保持一致
			if cond.Value == nil && (cond.Operator == "=" || cond.Operator == "!=" || cond.Operator == "<>") {
				if cond.Operator == "=" {
					clause = g.WrapColumn(cond.First) + " IS NULL"
				} else {
					clause = g.WrapColumn(cond.First) + " IS NOT NULL"
				}
			} else if expr, ok := cond.Value.(Expression); ok {
				clause = g.WrapColumn(cond.First) + " " + cond.Operator + " " + expr.Value()
			} else {
				clause = g.WrapColumn(cond.First) + " " + cond.Operator + " ?"
			}
		case "raw":
			clause = replaceRawExpression(cond.SQL, cond.Bindings)
		case "null":
			if cond.Not {
				clause = g.WrapColumn(cond.First) + " IS NOT NULL"
			} else {
				clause = g.WrapColumn(cond.First) + " IS NULL"
			}
		case "in":
			if len(cond.Values) == 0 {
				if cond.Not {
					clause = "1 = 1"
				} else {
					clause = "0 = 1"
				}
			} else {
				placeholders := make([]string, len(cond.Values))
				for k := range cond.Values {
					placeholders[k] = "?"
				}
				op := "IN"
				if cond.Not {
					op = "NOT IN"
				}
				clause = g.WrapColumn(cond.First) + " " + op + " (" + strings.Join(placeholders, ", ") + ")"
			}
		case "inSub":
			if cond.Sub != nil {
				subSQL := cond.Sub.grammar.CompileSelect(cond.Sub, cond.Sub.columns)
				op := "IN"
				if cond.Not {
					op = "NOT IN"
				}
				clause = g.WrapColumn(cond.First) + " " + op + " (" + subSQL + ")"
			}
		case "subValue":
			if cond.Sub != nil {
				subSQL := cond.Sub.grammar.CompileSelect(cond.Sub, cond.Sub.columns)
				clause = g.WrapColumn(cond.First) + " " + cond.Operator + " (" + subSQL + ")"
			}
		case "exists":
			if cond.Sub != nil {
				subSQL := cond.Sub.grammar.CompileSelect(cond.Sub, cond.Sub.columns)
				clause = "EXISTS (" + subSQL + ")"
			}
		case "nested":
			if cond.Nested != nil {
				inner := g.compileJoinConditions(cond.Nested.Conditions)
				if inner != "" {
					clause = "(" + inner + ")"
				}
			}
		}
		if clause == "" {
			continue
		}
		// 同 compileWheres：以 parts 是否为空判定首条有效条件，避免悬挂连接词
		if len(parts) == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, cond.Boolean+" "+clause)
		}
	}
	return strings.Join(parts, " ")
}

// joinTable 编译 JOIN 目标表：普通表名或派生表（子查询）。
func (g *MySQLGrammar) joinTable(join JoinClause) string {
	if join.Sub != nil {
		subSQL := join.Sub.grammar.CompileSelect(join.Sub, join.Sub.columns)
		return "(" + subSQL + ") AS " + g.wrapValue(join.Alias)
	}
	return g.WrapTable(join.Table)
}

// compileHavings 编译 HAVING 子句
func (g *MySQLGrammar) compileHavings(b *Builder) string {
	var parts []string
	for _, h := range b.havings {
		var clause string
		switch h.Type {
		case "basic":
			if expr, ok := h.Value.(Expression); ok {
				clause = g.WrapColumn(h.Column) + " " + h.Operator + " " + expr.Value()
			} else {
				clause = g.WrapColumn(h.Column) + " " + h.Operator + " ?"
			}
		case "raw":
			clause = replaceRawExpression(h.SQL, h.Bindings)
		case "between":
			if h.Not {
				clause = g.WrapColumn(h.Column) + " NOT BETWEEN ? AND ?"
			} else {
				clause = g.WrapColumn(h.Column) + " BETWEEN ? AND ?"
			}
		case "null":
			clause = g.WrapColumn(h.Column) + " IS NULL"
		case "notNull":
			clause = g.WrapColumn(h.Column) + " IS NOT NULL"
		case "nested":
			if h.Nested != nil {
				nested := g.compileHavings(h.Nested)
				if nested != "" {
					clause = "(" + nested + ")"
				}
			}
		}
		if clause == "" {
			continue
		}
		// 同 compileWheres：以 parts 是否为空判定首条有效条件，避免悬挂连接词
		if len(parts) == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, h.Boolean+" "+clause)
		}
	}
	return strings.Join(parts, " ")
}
