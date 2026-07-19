package zcdb

import (
	"strings"
)

// SQLiteGrammar SQLite 方言编译器。
// 与其它方言的主要差异：
//   - 标识符使用双引号 " 引用（SQL 标准）
//   - 参数占位符使用 ?（同 MySQL）
//   - INSERT OR IGNORE INTO ...（有别于 MySQL 的 INSERT IGNORE 和 PG 的 ON CONFLICT DO NOTHING）
//   - UPSERT 使用 ON CONFLICT (col) DO UPDATE SET col = excluded.col（SQLite 3.24+，语法同 PG）
//   - 没有 TRUNCATE，用 DELETE FROM 代替
//   - 不支持锁（FOR UPDATE / LOCK IN SHARE MODE 会被忽略）
//   - UPDATE 不生成 JOIN、ORDER BY、LIMIT（多表更新用 FROM 子句，SQLite 3.33+）
//   - DELETE 不生成 ORDER BY 和 LIMIT（除非编译时启用）
//   - 随机排序使用 RANDOM()
type SQLiteGrammar struct {
	tablePrefix string
}

// NewSQLiteGrammar 创建一个 SQLite 语法编译器
func NewSQLiteGrammar() *SQLiteGrammar {
	return &SQLiteGrammar{}
}

// SetTablePrefix 设置表名前缀
func (g *SQLiteGrammar) SetTablePrefix(prefix string) *SQLiteGrammar {
	g.tablePrefix = prefix
	return g
}

// Placeholder SQLite 使用 ? 作为参数占位符
func (g *SQLiteGrammar) Placeholder(_ int) string {
	return "?"
}

// CompileRandom 返回 SQLite 随机排序表达式
func (g *SQLiteGrammar) CompileRandom() string {
	return "RANDOM()"
}

// WrapColumn 使用双引号引用列名。
// 包含括号的表达式（如 COUNT(*)、YEAR(col)）不会被引用。
func (g *SQLiteGrammar) WrapColumn(column string) string {
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

// WrapTable 使用双引号引用表名
func (g *SQLiteGrammar) WrapTable(table string) string {
	if strings.Contains(table, " as ") || strings.Contains(table, " AS ") {
		idx := strings.Index(strings.ToLower(table), " as ")
		name := table[:idx]
		alias := table[idx+4:]
		return g.wrapValue(g.tablePrefix+strings.TrimSpace(name)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	return g.wrapValue(g.tablePrefix + table)
}

// wrapValue SQLite 使用双引号引用标识符
func (g *SQLiteGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// CompileSelect 编译 SELECT 查询
func (g *SQLiteGrammar) CompileSelect(b *Builder, columns []string) string {
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

	// UNION（SQLite 中 UNION 的子查询不允许显式括号，需要用 SELECT ... UNION SELECT ... 形式）
	if len(b.unions) > 0 {
		result := sql.String()
		for _, union := range b.unions {
			if union.All {
				result += " UNION ALL "
			} else {
				result += " UNION "
			}
			unionSQL := union.Query.grammar.CompileSelect(union.Query, union.Query.columns)
			result += unionSQL
		}
		return result
	}

	// LOCK: SQLite 不支持行锁，忽略

	return sql.String()
}

// CompileInsert 编译 INSERT 语句
func (g *SQLiteGrammar) CompileInsert(b *Builder, columns []string, rows [][]any) string {
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

// CompileInsertOrIgnore 编译 INSERT OR IGNORE 语句 (SQLite)
func (g *SQLiteGrammar) CompileInsertOrIgnore(b *Builder, columns []string, rows [][]any) string {
	var sql strings.Builder

	sql.WriteString("INSERT OR IGNORE INTO ")
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

// CompileUpsert 编译 SQLite 的 INSERT ... ON CONFLICT DO UPDATE 语句
// SQLite 3.24+ 支持，语法同 PostgreSQL，用 excluded.col 引用要插入的值
func (g *SQLiteGrammar) CompileUpsert(b *Builder, columns []string, rows [][]any, uniqueBy []string, updateColumns []string, _ []any) string {
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

	// ON CONFLICT 部分
	sql.WriteString(" ON CONFLICT (")
	for i, col := range uniqueBy {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(g.WrapColumn(col))
	}
	sql.WriteString(") DO UPDATE SET ")

	for i, col := range updateColumns {
		if i > 0 {
			sql.WriteString(", ")
		}
		wrapped := g.WrapColumn(col)
		sql.WriteString(wrapped)
		sql.WriteString(" = ")
		sql.WriteString(`"excluded".`)
		sql.WriteString(g.wrapValue(col))
	}

	return sql.String()
}

// CompileInsertUsing 编译 INSERT INTO ... SELECT 语句
func (g *SQLiteGrammar) CompileInsertUsing(b *Builder, columns []string, sub *Builder) string {
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

// CompileUpdate 编译 UPDATE 语句。
// 注意: SQLite 的 UPDATE 默认不支持 JOIN、ORDER BY、LIMIT。
// 多表更新采用 UPDATE ... SET ... FROM ...（SQLite 3.33+）。
func (g *SQLiteGrammar) CompileUpdate(b *Builder, columns []string, values []any) string {
	var sql strings.Builder

	sql.WriteString("UPDATE ")
	sql.WriteString(g.WrapTable(b.table))

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

	// FROM (SQLite 3.33+ 支持 UPDATE ... FROM ...)
	if len(b.joins) > 0 {
		sql.WriteString(" FROM ")
		for i, join := range b.joins {
			if i > 0 {
				sql.WriteString(", ")
			}
			sql.WriteString(g.WrapTable(join.Table))
		}
	}

	// WHERE
	whereSQL := g.compileWheres(b)

	// 有 JOIN 时把 ON 条件并入 WHERE
	if len(b.joins) > 0 {
		var joinConditions []string
		for _, join := range b.joins {
			for _, cond := range join.Conditions {
				if cond.Type == "column" {
					jc := g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.WrapColumn(cond.Second)
					joinConditions = append(joinConditions, jc)
				}
			}
		}
		if len(joinConditions) > 0 {
			joinWhere := strings.Join(joinConditions, " AND ")
			if whereSQL != "" {
				whereSQL = joinWhere + " AND " + whereSQL
			} else {
				whereSQL = joinWhere
			}
		}
	}

	if whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	return sql.String()
}

// CompileDelete 编译 DELETE 语句。
// 注意: SQLite 的 DELETE 默认不支持 ORDER BY 和 LIMIT。
func (g *SQLiteGrammar) CompileDelete(b *Builder) string {
	var sql strings.Builder

	sql.WriteString("DELETE FROM ")
	sql.WriteString(g.WrapTable(b.table))

	// WHERE
	if whereSQL := g.compileWheres(b); whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	return sql.String()
}

// CompileTruncate SQLite 没有 TRUNCATE，用 DELETE FROM 代替
func (g *SQLiteGrammar) CompileTruncate(b *Builder) string {
	return "DELETE FROM " + g.WrapTable(b.table)
}

// compileWheres 编译 WHERE 子句
func (g *SQLiteGrammar) compileWheres(b *Builder) string {
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

func (g *SQLiteGrammar) compileWhereBasic(w WhereClause) string {
	if expr, ok := w.Value.(Expression); ok {
		return g.WrapColumn(w.Column) + " " + w.Operator + " " + expr.Value()
	}
	return g.WrapColumn(w.Column) + " " + w.Operator + " ?"
}

func (g *SQLiteGrammar) compileWhereIn(w WhereClause) string {
	if len(w.Values) == 0 {
		return "0 = 1"
	}
	placeholders := make([]string, len(w.Values))
	for i := range w.Values {
		placeholders[i] = "?"
	}
	return g.WrapColumn(w.Column) + " IN (" + strings.Join(placeholders, ", ") + ")"
}

func (g *SQLiteGrammar) compileWhereNotIn(w WhereClause) string {
	if len(w.Values) == 0 {
		return "1 = 1"
	}
	placeholders := make([]string, len(w.Values))
	for i := range w.Values {
		placeholders[i] = "?"
	}
	return g.WrapColumn(w.Column) + " NOT IN (" + strings.Join(placeholders, ", ") + ")"
}

// compileJoin 编译 JOIN 子句
func (g *SQLiteGrammar) compileJoin(join JoinClause) string {
	var joinType string
	switch join.Type {
	case JoinTypeInner:
		joinType = "INNER JOIN"
	case JoinTypeLeft:
		joinType = "LEFT JOIN"
	case JoinTypeRight:
		// SQLite 3.39+ 才支持 RIGHT JOIN
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
func (g *SQLiteGrammar) compileHavings(b *Builder) string {
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
