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
	// 处理 table.column 形式
	if strings.Contains(column, ".") {
		parts := strings.SplitN(column, ".", 2)
		return g.WrapTable(parts[0]) + "." + g.wrapValue(parts[1])
	}
	// 处理 column AS alias 形式
	if idx := strings.Index(strings.ToLower(column), " as "); idx != -1 {
		col := column[:idx]
		alias := column[idx+4:]
		return g.WrapColumn(strings.TrimSpace(col)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	return g.wrapValue(column)
}

// WrapTable 使用反引号引用表名
func (g *MySQLGrammar) WrapTable(table string) string {
	if strings.Contains(table, " as ") || strings.Contains(table, " AS ") {
		idx := strings.Index(strings.ToLower(table), " as ")
		name := table[:idx]
		alias := table[idx+4:]
		return g.wrapValue(strings.TrimSpace(name)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	return g.wrapValue(table)
}

func (g *MySQLGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return "`" + strings.ReplaceAll(value, "`", "``") + "`"
}

// CompileSelect 编译 SELECT 查询
func (g *MySQLGrammar) CompileSelect(b *Builder, columns []string) string {
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
			sql.WriteString(g.WrapColumn(col))
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
	if b.fromSub != nil {
		subSQL := b.fromSub.grammar.CompileSelect(b.fromSub, b.fromSub.columns)
		sql.WriteString("(" + subSQL + ") AS " + g.wrapValue(b.fromAlias))
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
			sql.WriteString(g.WrapColumn(group))
		}
	}

	// HAVING
	if len(b.havings) > 0 {
		sql.WriteString(" HAVING ")
		sql.WriteString(g.compileHavings(b))
	}

	// ORDER BY
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

	// LIMIT
	if b.limit > 0 {
		sql.WriteString(" LIMIT ")
		sql.WriteString(intToStr(b.limit))
	}

	// OFFSET
	if b.offset > 0 {
		sql.WriteString(" OFFSET ")
		sql.WriteString(intToStr(b.offset))
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
		if b.lockClause != "" {
			result += " " + b.lockClause
		}
		return result
	}

	// LOCK
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
			sql.WriteString("?")
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
			sql.WriteString("?")
		}
		sql.WriteString(")")
	}

	return sql.String()
}

// CompileUpsert 编译 MySQL 的 INSERT ... ON DUPLICATE KEY UPDATE 语句
func (g *MySQLGrammar) CompileUpsert(b *Builder, columns []string, rows [][]any, _ []string, updateColumns []string, _ []any) string {
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
			sql.WriteString("?")
		}
		sql.WriteString(")")
	}

	// ON DUPLICATE KEY UPDATE 部分
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
	for i, w := range b.wheres {
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
			clause = w.SQL
		case WhereTypeNested:
			if w.Nested != nil {
				nested := g.compileWheres(w.Nested)
				if nested != "" {
					clause = "(" + nested + ")"
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
			clause = g.WrapColumn(w.Column) + " LIKE ?"
		case WhereTypeNotLike:
			clause = g.WrapColumn(w.Column) + " NOT LIKE ?"
		}

		if clause == "" {
			continue
		}

		if i == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, w.Boolean+" "+clause)
		}
	}

	return strings.Join(parts, " ")
}

func (g *MySQLGrammar) compileWhereBasic(w WhereClause) string {
	if expr, ok := w.Value.(Expression); ok {
		return g.WrapColumn(w.Column) + " " + w.Operator + " " + expr.Value()
	}
	return g.WrapColumn(w.Column) + " " + w.Operator + " ?"
}

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

// compileJoin 编译 JOIN 子句
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
	}

	result := joinType + " " + g.WrapTable(join.Table)
	if join.Type != JoinTypeCross && len(join.Conditions) > 0 {
		result += " ON "
		for i, cond := range join.Conditions {
			if i > 0 {
				result += " " + cond.Boolean + " "
			}
			switch cond.Type {
			case "column":
				result += g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.WrapColumn(cond.Second)
			case "value":
				result += g.WrapColumn(cond.First) + " " + cond.Operator + " ?"
			case "raw":
				result += cond.SQL
			}
		}
	}
	return result
}

// compileHavings 编译 HAVING 子句
func (g *MySQLGrammar) compileHavings(b *Builder) string {
	var parts []string
	for i, h := range b.havings {
		var clause string
		switch h.Type {
		case "basic":
			clause = g.WrapColumn(h.Column) + " " + h.Operator + " ?"
		case "raw":
			clause = h.SQL
		case "between":
			if h.Not {
				clause = g.WrapColumn(h.Column) + " NOT BETWEEN ? AND ?"
			} else {
				clause = g.WrapColumn(h.Column) + " BETWEEN ? AND ?"
			}
		}
		if clause == "" {
			continue
		}
		if i == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, h.Boolean+" "+clause)
		}
	}
	return strings.Join(parts, " ")
}
