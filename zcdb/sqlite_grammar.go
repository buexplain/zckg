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
//   - 不支持锁（FOR UPDATE / LOCK IN SHARE MODE 返回 ErrSQLiteLockNotSupported）
//   - UPDATE 不生成 JOIN、ORDER BY、LIMIT（多表更新用 FROM 子句，SQLite 3.33+）
//   - DELETE 不生成 ORDER BY 和 LIMIT（除非编译时启用）
//   - 随机排序使用 RANDOM()
type SQLiteGrammar struct{}

// NewSQLiteGrammar 创建一个 SQLite 语法编译器
func NewSQLiteGrammar() *SQLiteGrammar {
	return &SQLiteGrammar{}
}

// CompileRandom 返回 SQLite 随机排序表达式
func (g *SQLiteGrammar) CompileRandom() string {
	return "RANDOM()"
}

// CompileWhereDate 返回 WhereDate 的日期比较表达式（SQLite 用 strftime 提取 YYYY-MM-DD 日期部分）。
func (g *SQLiteGrammar) CompileWhereDate(column string) string {
	return "strftime('%Y-%m-%d', " + g.WrapColumn(column) + ")"
}

// UpdateSetBeforeJoin SQLite 的 UPDATE ... SET ... FROM ... WHERE ... 中 SET 在 JOIN 条件之前。
func (g *SQLiteGrammar) UpdateSetBeforeJoin() bool {
	return true
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

// WrapTable 使用双引号引用表名
func (g *SQLiteGrammar) WrapTable(table string) string {
	// 对 ToLower 后的表名统一做 " as " 判定（大小写不敏感，避免 "orders As o" 等混合大小写漏判）
	if idx := strings.Index(strings.ToLower(table), " as "); idx != -1 {
		name := table[:idx]
		alias := table[idx+4:]
		return g.wrapValue(strings.TrimSpace(name)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	return g.wrapValue(table)
}

// wrapValue SQLite 使用双引号引用标识符
func (g *SQLiteGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// CompileSelect 编译 SELECT 查询
func (g *SQLiteGrammar) CompileSelect(b *Builder, columns []SelectColumn) string {
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
			// SQLite 的 OFFSET 必须配合 LIMIT，无 LIMIT 时用 -1 表示不限制行数
			if b.limit == 0 {
				sb.WriteString(" LIMIT -1")
			}
			sb.WriteString(" OFFSET ")
			sb.WriteString(intToStr(b.offset))
		}
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
		// ORDER BY / LIMIT / OFFSET 必须放在 UNION 之后
		var tail strings.Builder
		appendOrderLimitOffset(&tail)
		return result + tail.String()
	}

	// LOCK: SQLite 不支持行锁，忽略

	appendOrderLimitOffset(&sql)

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
			// Expression 直接内联，不生成占位符
			if expr, ok := row[j].(Expression); ok {
				sql.WriteString(expr.Value())
			} else {
				sql.WriteString("?")
			}
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
	// updateColumns 为空（全部插入列均为 uniqueBy）时退化为 DO NOTHING（冲突时不更新）
	if len(updateColumns) == 0 {
		sql.WriteString(") DO NOTHING")
		return sql.String()
	}
	sql.WriteString(") DO UPDATE SET ")

	for i, col := range updateColumns {
		if i > 0 {
			sql.WriteString(", ")
		}
		wrapped := g.WrapColumn(col)
		sql.WriteString(wrapped)
		sql.WriteString(" = EXCLUDED.")
		sql.WriteString(wrapped)
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

// CompileInsertOrIgnoreUsing 编译 INSERT OR IGNORE INTO ... SELECT 语句（冲突时静默跳过）
func (g *SQLiteGrammar) CompileInsertOrIgnoreUsing(b *Builder, columns []string, sub *Builder) string {
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
	// 展平嵌套 join 组；JoinSub 编译为派生表 (SELECT ...) AS alias
	if len(b.joins) > 0 {
		var tables []string
		var flatten func(joins []JoinClause)
		flatten = func(joins []JoinClause) {
			for _, join := range joins {
				tables = append(tables, g.joinTable(join))
				if len(join.Joins) > 0 {
					flatten(join.Joins)
				}
			}
		}
		flatten(b.joins)
		sql.WriteString(" FROM ")
		sql.WriteString(strings.Join(tables, ", "))
	}

	// WHERE
	// 有 JOIN 时先将 ON 条件并入 WHERE 前部，
	// 保证占位符顺序（JOIN 条件 → WHERE 条件）与 collectJoinBindings → collectWhereBindings 的绑定顺序一致。
	joinWhere := g.compileJoinWheres(b.joins)

	whereSQL := g.compileWheres(b)
	if joinWhere != "" {
		if whereSQL != "" {
			whereSQL = joinWhere + " AND " + whereSQL
		} else {
			whereSQL = joinWhere
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

// CompileDeleteJoin 编译按关联条件删除的 DELETE 语句。
// SQLite 不支持多表 DELETE 语法，采用主键 IN 子查询方案（依赖主键列，默认 id）：
// DELETE FROM t WHERE "id" IN (SELECT t."id" FROM t JOIN ... WHERE ...)
func (g *SQLiteGrammar) CompileDeleteJoin(b *Builder) string {
	var sub strings.Builder

	sub.WriteString("SELECT ")
	sub.WriteString(g.WrapColumn(b.table + ".id"))
	sub.WriteString(" FROM ")
	sub.WriteString(g.WrapTable(b.table))
	for _, join := range b.joins {
		sub.WriteString(" ")
		sub.WriteString(g.compileJoin(join))
	}
	if whereSQL := g.compileWheres(b); whereSQL != "" {
		sub.WriteString(" WHERE ")
		sub.WriteString(whereSQL)
	}

	return "DELETE FROM " + g.WrapTable(b.table) + " WHERE " + g.WrapColumn("id") + " IN (" + sub.String() + ")"
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
			// 区分大小写时用 GLOB（天然区分大小写；通配符为 * / ?，调用方需自行转换）
			if w.CaseSensitive {
				if expr, ok := w.Value.(Expression); ok {
					clause = g.WrapColumn(w.Column) + " GLOB " + expr.Value()
				} else {
					clause = g.WrapColumn(w.Column) + " GLOB ?"
				}
			} else if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " LIKE " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " LIKE ?"
			}
		case WhereTypeNotLike:
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " NOT LIKE " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " NOT LIKE ?"
			}
		case WhereTypeNullSafe:
			// 空安全相等：SQLite 用 IS（NULL IS NULL 为 true）
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " IS " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " IS ?"
			}
		case WhereTypeNullSafeNot:
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " IS NOT " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " IS NOT ?"
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
func (g *SQLiteGrammar) compileWhereBasic(w WhereClause) string {
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

// compileWhereNotIn 编译 NOT IN 条件；空值列表编译为 1 = 1（恒真）。
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

// compileJoin 编译 JOIN 子句。
// 嵌套 join 组（join.Joins）编译为带括号的 join 组：
// INNER JOIN (表 INNER JOIN 子表 ON ...) ON ...
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
	case JoinTypeCrossOn:
		// SQLite 支持 CROSS JOIN ... ON ...
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
func (g *SQLiteGrammar) compileJoinConditions(conditions []JoinCondition) string {
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

// compileJoinWheres 将 JOIN 的 ON 条件展平编译为 WHERE 条件片段（供 UPDATE FROM 使用）。
// 遍历顺序与 collectJoinClauseBindings 一致：嵌套 join 组在前、自身条件在后。
func (g *SQLiteGrammar) compileJoinWheres(joins []JoinClause) string {
	var parts []string
	for _, join := range joins {
		if inner := g.compileJoinWheres(join.Joins); inner != "" {
			parts = append(parts, inner)
		}
		if s := g.compileJoinConditions(join.Conditions); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " AND ")
}

// joinTable 编译 JOIN 目标表：普通表名或派生表（子查询）。
func (g *SQLiteGrammar) joinTable(join JoinClause) string {
	if join.Sub != nil {
		subSQL := join.Sub.grammar.CompileSelect(join.Sub, join.Sub.columns)
		return "(" + subSQL + ") AS " + g.wrapValue(join.Alias)
	}
	return g.WrapTable(join.Table)
}

// compileHavings 编译 HAVING 子句
func (g *SQLiteGrammar) compileHavings(b *Builder) string {
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
