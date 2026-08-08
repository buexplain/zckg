package zcdb

import (
	"strings"
)

// PostgresGrammar PostgreSQL 方言编译器。
// 与 MySQL 的主要差异：
//   - 标识符使用双引号 " 引用
//   - 参数占位符使用 $1, $2, $3 ...
//   - 共享锁使用 FOR SHARE
//   - DELETE/UPDATE 不支持 ORDER BY 和 LIMIT
//   - INSERT 支持 RETURNING 子句
type PostgresGrammar struct {
	paramCount int // 编译期间的参数计数器（每次编译入口重置）
}

// cloneForCompile 克隆当前 Grammar 用于单次编译，保证并发安全。
// paramCount 从 0 开始，各 goroutine 互不干扰。
func (g *PostgresGrammar) cloneForCompile() *PostgresGrammar {
	return &PostgresGrammar{}
}

// convertRawPlaceholders 将原始 SQL 中的 ? 依次替换为 $N 占位符，
// bindings 中的 Expression 直接内嵌为 SQL 文本且不占用占位符编号；
// 连续两个 ?? 转义为字面 ?（jsonb 键存在操作符），不消耗绑定（对齐 Laravel）。
func (g *PostgresGrammar) convertRawPlaceholders(sql string, bindings []any) string {
	var buf strings.Builder
	buf.Grow(len(sql) + 8)
	bi := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' {
			// ?? 转义为字面 ?（jsonb 键存在操作符），不占绑定
			if i+1 < len(sql) && sql[i+1] == '?' {
				buf.WriteByte('?')
				i++
				continue
			}
			if bi < len(bindings) {
				if expr, ok := bindings[bi].(Expression); ok {
					buf.WriteString(expr.Value())
					bi++
					continue
				}
				bi++
			}
			buf.WriteString(g.nextParam())
		} else {
			buf.WriteByte(sql[i])
		}
	}
	return buf.String()
}

// NewPostgresGrammar 创建一个 PostgreSQL 语法编译器
func NewPostgresGrammar() *PostgresGrammar {
	return &PostgresGrammar{}
}

// nextParam 生成下一个占位符并返回
func (g *PostgresGrammar) nextParam() string {
	g.paramCount++
	return "$" + intToStr(g.paramCount)
}

// CompileRandom 返回 PostgreSQL 随机排序表达式
func (g *PostgresGrammar) CompileRandom() string {
	return "RANDOM()"
}

// CompileWhereDate 返回 WhereDate 的日期比较表达式（PostgreSQL 用 ::date 强制转换提取日期部分）。
func (g *PostgresGrammar) CompileWhereDate(column string) string {
	return g.WrapColumn(column) + "::date"
}

// UpdateSetBeforeJoin PostgreSQL 的 UPDATE ... SET ... FROM ... WHERE ... 中 SET 在 JOIN 条件之前。
func (g *PostgresGrammar) UpdateSetBeforeJoin() bool {
	return true
}

// WrapColumn 使用双引号引用列名。
// 包含括号的表达式（如 COUNT(*)、YEAR(col)）不会被引用。
func (g *PostgresGrammar) WrapColumn(column string) string {
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
func (g *PostgresGrammar) WrapTable(table string) string {
	if strings.Contains(table, " as ") || strings.Contains(table, " AS ") {
		idx := strings.Index(strings.ToLower(table), " as ")
		name := table[:idx]
		alias := table[idx+4:]
		return g.wrapValue(strings.TrimSpace(name)) + " AS " + g.wrapValue(strings.TrimSpace(alias))
	}
	return g.wrapValue(table)
}

// wrapValue PostgreSQL 使用双引号引用标识符
func (g *PostgresGrammar) wrapValue(value string) string {
	if value == "*" {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
}

// CompileSelect 编译 SELECT 查询
func (g *PostgresGrammar) CompileSelect(b *Builder, columns []SelectColumn) string {
	return g.cloneForCompile().compileSelectInner(b, columns)
}

func (g *PostgresGrammar) compileSelectInner(b *Builder, columns []SelectColumn) string {
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
			subSQL := g.compileSelectInner(ss.Query, ss.Query.columns)
			sql.WriteString("(" + subSQL + ") AS " + g.wrapValue(ss.Alias))
			first = false
		}
	}

	// FROM
	sql.WriteString(" FROM ")
	if b.tableSub != nil {
		subSQL := g.compileSelectInner(b.tableSub, b.tableSub.columns)
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
				sql.WriteString(g.convertRawPlaceholders(group.Raw, group.Bindings))
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
			sb.WriteString(" OFFSET ")
			sb.WriteString(intToStr(b.offset))
		}
	}

	// LOCK (PostgreSQL 使用 FOR SHARE 代替 LOCK IN SHARE MODE)
	var lockSQL string
	if b.lockClause != "" {
		lockSQL = b.lockClause
		if lockSQL == "LOCK IN SHARE MODE" {
			lockSQL = "FOR SHARE"
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
			unionSQL := g.compileSelectInner(union.Query, union.Query.columns)
			result += "(" + unionSQL + ")"
		}
		// ORDER BY / LIMIT / OFFSET / LOCK 必须放在 UNION 之后
		var tail strings.Builder
		appendOrderLimitOffset(&tail)
		result += tail.String()
		if lockSQL != "" {
			result += " " + lockSQL
		}
		return result
	}

	appendOrderLimitOffset(&sql)
	if lockSQL != "" {
		sql.WriteString(" ")
		sql.WriteString(lockSQL)
	}

	return sql.String()
}

// CompileInsert 编译 INSERT 语句
func (g *PostgresGrammar) CompileInsert(b *Builder, columns []string, rows [][]any) string {
	gClone := g.cloneForCompile()

	var sql strings.Builder

	sql.WriteString("INSERT INTO ")
	sql.WriteString(gClone.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(gClone.WrapColumn(col))
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
				sql.WriteString(gClone.nextParam())
			}
		}
		sql.WriteString(")")
	}

	return sql.String()
}

// CompileInsertOrIgnore 编译 INSERT ... ON CONFLICT DO NOTHING 语句 (PostgreSQL)
func (g *PostgresGrammar) CompileInsertOrIgnore(b *Builder, columns []string, rows [][]any) string {
	return g.CompileInsert(b, columns, rows) + " ON CONFLICT DO NOTHING"
}

// CompileUpsert 编译 PostgreSQL 的 INSERT ... ON CONFLICT DO UPDATE 语句
func (g *PostgresGrammar) CompileUpsert(b *Builder, columns []string, rows [][]any, uniqueBy []string, updateColumns []string, _ []any) string {
	gClone := g.cloneForCompile()

	var sql strings.Builder

	sql.WriteString("INSERT INTO ")
	sql.WriteString(gClone.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(gClone.WrapColumn(col))
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
			sql.WriteString(gClone.nextParam())
		}
		sql.WriteString(")")
	}

	// ON CONFLICT 部分
	sql.WriteString(" ON CONFLICT (")
	for i, col := range uniqueBy {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(gClone.WrapColumn(col))
	}
	sql.WriteString(") DO UPDATE SET ")

	for i, col := range updateColumns {
		if i > 0 {
			sql.WriteString(", ")
		}
		wrapped := gClone.WrapColumn(col)
		sql.WriteString(wrapped)
		sql.WriteString(" = EXCLUDED.")
		sql.WriteString(wrapped)
	}

	return sql.String()
}

// CompileInsertUsing 编译 INSERT INTO ... SELECT 语句
func (g *PostgresGrammar) CompileInsertUsing(b *Builder, columns []string, sub *Builder) string {
	gClone := g.cloneForCompile()

	var sql strings.Builder

	sql.WriteString("INSERT INTO ")
	sql.WriteString(gClone.WrapTable(b.table))
	sql.WriteString(" (")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(gClone.WrapColumn(col))
	}
	sql.WriteString(") ")
	sql.WriteString(gClone.compileSelectInner(sub, sub.columns))

	return sql.String()
}

// CompileInsertOrIgnoreUsing 编译忽略冲突的 INSERT INTO ... SELECT 语句（末尾追加 ON CONFLICT DO NOTHING）
func (g *PostgresGrammar) CompileInsertOrIgnoreUsing(b *Builder, columns []string, sub *Builder) string {
	return g.CompileInsertUsing(b, columns, sub) + " ON CONFLICT DO NOTHING"
}

// CompileUpdate 编译 UPDATE 语句。
// 注意: PostgreSQL 的 UPDATE 不支持 ORDER BY 和 LIMIT。
func (g *PostgresGrammar) CompileUpdate(b *Builder, columns []string, values []any) string {
	gClone := g.cloneForCompile()

	var sql strings.Builder

	sql.WriteString("UPDATE ")
	sql.WriteString(gClone.WrapTable(b.table))

	// SET
	sql.WriteString(" SET ")
	for i, col := range columns {
		if i > 0 {
			sql.WriteString(", ")
		}
		sql.WriteString(gClone.WrapColumn(col))
		sql.WriteString(" = ")
		// 检查值是否为 Expression：内部的 ? 占位符需转为 $N（如 ToIncrement 的 col + ?）
		if expr, ok := values[i].(Expression); ok {
			sql.WriteString(gClone.convertRawPlaceholders(expr.Value(), nil))
		} else {
			sql.WriteString(gClone.nextParam())
		}
	}

	// FROM (PostgreSQL 使用 FROM 代替 JOIN 进行多表更新)
	if len(b.joins) > 0 {
		sql.WriteString(" FROM ")
		for i, join := range b.joins {
			if i > 0 {
				sql.WriteString(", ")
			}
			sql.WriteString(gClone.WrapTable(join.Table))
		}
	}

	// WHERE
	// 有 JOIN 时先将 ON 条件并入 WHERE 前部。
	// 注意：必须在 compileWheres 之前编译 JOIN 条件，
	// 以保证 $N 占位符顺序（JOIN 条件 → WHERE 条件）与 collectJoinBindings → collectWhereBindings 的绑定顺序一致。
	joinWhere := gClone.compileJoinWheres(b.joins)

	whereSQL := gClone.compileWheres(b)
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
// 注意: PostgreSQL 的 DELETE 不支持 ORDER BY 和 LIMIT。
func (g *PostgresGrammar) CompileDelete(b *Builder) string {
	gClone := g.cloneForCompile()

	var sql strings.Builder

	sql.WriteString("DELETE FROM ")
	sql.WriteString(gClone.WrapTable(b.table))

	// WHERE
	if whereSQL := gClone.compileWheres(b); whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	return sql.String()
}

// CompileDeleteJoin 编译按关联条件删除的 DELETE 语句。
// PostgreSQL 采用原生 USING 形式：DELETE FROM t USING t2 WHERE join条件 AND where条件。
// 嵌套 join 组的表被展平列入 USING（条件仍按原有布尔连接符合并到 WHERE）。
func (g *PostgresGrammar) CompileDeleteJoin(b *Builder) string {
	gClone := g.cloneForCompile()

	var sql strings.Builder

	sql.WriteString("DELETE FROM ")
	sql.WriteString(gClone.WrapTable(b.table))

	// USING：展平列举全部 join 目标表（含嵌套组与派生表）
	sql.WriteString(" USING ")
	first := true
	var flatten func(joins []JoinClause)
	flatten = func(joins []JoinClause) {
		for _, join := range joins {
			if !first {
				sql.WriteString(", ")
			}
			sql.WriteString(gClone.joinTable(join))
			first = false
			flatten(join.Joins)
		}
	}
	flatten(b.joins)

	// WHERE：先 JOIN 条件后 WHERE 条件（$N 顺序与 collectJoinBindings → collectWhereBindings 一致）
	whereSQL := gClone.compileJoinWheres(b.joins)
	if w := gClone.compileWheres(b); w != "" {
		if whereSQL != "" {
			whereSQL += " AND "
		}
		whereSQL += w
	}
	if whereSQL != "" {
		sql.WriteString(" WHERE ")
		sql.WriteString(whereSQL)
	}

	return sql.String()
}

// CompileTruncate 编译 TRUNCATE 语句（RESTART IDENTITY 重置自增序列，对齐 Laravel 行为）
func (g *PostgresGrammar) CompileTruncate(b *Builder) string {
	return "TRUNCATE TABLE " + g.WrapTable(b.table) + " RESTART IDENTITY"
}

// compileWheres 编译 WHERE 子句
func (g *PostgresGrammar) compileWheres(b *Builder) string {
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
			clause = g.WrapColumn(w.Column) + " BETWEEN " + g.nextParam() + " AND " + g.nextParam()
		case WhereTypeNotBetween:
			clause = g.WrapColumn(w.Column) + " NOT BETWEEN " + g.nextParam() + " AND " + g.nextParam()
		case WhereTypeRaw:
			clause = g.convertRawPlaceholders(w.SQL, w.Bindings)
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
				subSQL := g.compileSelectInner(w.Nested, w.Nested.columns)
				clause = "EXISTS (" + subSQL + ")"
			}
		case WhereTypeNotExists:
			if w.Nested != nil {
				subSQL := g.compileSelectInner(w.Nested, w.Nested.columns)
				clause = "NOT EXISTS (" + subSQL + ")"
			}
		case WhereTypeSub:
			if w.Sub != nil {
				subSQL := g.compileSelectInner(w.Sub, w.Sub.columns)
				clause = g.WrapColumn(w.Column) + " " + w.Operator + " (" + subSQL + ")"
			}
		case WhereTypeInSub:
			if w.Sub != nil {
				subSQL := g.compileSelectInner(w.Sub, w.Sub.columns)
				clause = g.WrapColumn(w.Column) + " IN (" + subSQL + ")"
			}
		case WhereTypeNotInSub:
			if w.Sub != nil {
				subSQL := g.compileSelectInner(w.Sub, w.Sub.columns)
				clause = g.WrapColumn(w.Column) + " NOT IN (" + subSQL + ")"
			}
		case WhereTypeLike:
			// PostgreSQL 默认 ILIKE（不区分大小写），区分大小写时用 LIKE
			op := "ILIKE"
			if w.CaseSensitive {
				op = "LIKE"
			}
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " " + op + " " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " " + op + " " + g.nextParam()
			}
		case WhereTypeNotLike:
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " NOT LIKE " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " NOT LIKE " + g.nextParam()
			}
		case WhereTypeNullSafe:
			// 空安全相等：PostgreSQL 用 IS NOT DISTINCT FROM
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " IS NOT DISTINCT FROM " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " IS NOT DISTINCT FROM " + g.nextParam()
			}
		case WhereTypeNullSafeNot:
			if expr, ok := w.Value.(Expression); ok {
				clause = g.WrapColumn(w.Column) + " IS DISTINCT FROM " + expr.Value()
			} else {
				clause = g.WrapColumn(w.Column) + " IS DISTINCT FROM " + g.nextParam()
			}
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

func (g *PostgresGrammar) compileWhereBasic(w WhereClause) string {
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
	return g.WrapColumn(w.Column) + " " + w.Operator + " " + g.nextParam()
}

func (g *PostgresGrammar) compileWhereIn(w WhereClause) string {
	if len(w.Values) == 0 {
		return "0 = 1"
	}
	placeholders := make([]string, len(w.Values))
	for i := range w.Values {
		placeholders[i] = g.nextParam()
	}
	return g.WrapColumn(w.Column) + " IN (" + strings.Join(placeholders, ", ") + ")"
}

func (g *PostgresGrammar) compileWhereNotIn(w WhereClause) string {
	if len(w.Values) == 0 {
		return "1 = 1"
	}
	placeholders := make([]string, len(w.Values))
	for i := range w.Values {
		placeholders[i] = g.nextParam()
	}
	return g.WrapColumn(w.Column) + " NOT IN (" + strings.Join(placeholders, ", ") + ")"
}

// compileJoin 编译 JOIN 子句。
// 嵌套 join 组（join.Joins）编译为带括号的 join 组：
// INNER JOIN (表 INNER JOIN 子表 ON ...) ON ...
func (g *PostgresGrammar) compileJoin(join JoinClause) string {
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
		// PostgreSQL 的 CROSS JOIN 不接受 ON 条件，编译为语义等价的 INNER JOIN
		joinType = "INNER JOIN"
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
// 子查询用当前编译上下文编译，共享 $N 占位符计数器。
func (g *PostgresGrammar) compileJoinConditions(conditions []JoinCondition) string {
	var parts []string
	for i, cond := range conditions {
		var clause string
		switch cond.Type {
		case "column":
			clause = g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.WrapColumn(cond.Second)
		case "value":
			if expr, ok := cond.Value.(Expression); ok {
				clause = g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.convertRawPlaceholders(expr.Value(), nil)
			} else {
				clause = g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.nextParam()
			}
		case "raw":
			clause = g.convertRawPlaceholders(cond.SQL, cond.Bindings)
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
					placeholders[k] = g.nextParam()
				}
				op := "IN"
				if cond.Not {
					op = "NOT IN"
				}
				clause = g.WrapColumn(cond.First) + " " + op + " (" + strings.Join(placeholders, ", ") + ")"
			}
		case "inSub":
			if cond.Sub != nil {
				subSQL := g.compileSelectInner(cond.Sub, cond.Sub.columns)
				op := "IN"
				if cond.Not {
					op = "NOT IN"
				}
				clause = g.WrapColumn(cond.First) + " " + op + " (" + subSQL + ")"
			}
		case "subValue":
			if cond.Sub != nil {
				subSQL := g.compileSelectInner(cond.Sub, cond.Sub.columns)
				clause = g.WrapColumn(cond.First) + " " + cond.Operator + " (" + subSQL + ")"
			}
		case "exists":
			if cond.Sub != nil {
				subSQL := g.compileSelectInner(cond.Sub, cond.Sub.columns)
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
		if i == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, cond.Boolean+" "+clause)
		}
	}
	return strings.Join(parts, " ")
}

// compileJoinWheres 将 JOIN 的 ON 条件展平编译为 WHERE 条件片段（供 UPDATE FROM / DELETE USING 使用）。
// 遍历顺序与 collectJoinClauseBindings 一致：嵌套 join 组在前、自身条件在后。
func (g *PostgresGrammar) compileJoinWheres(joins []JoinClause) string {
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
// 注意：派生表必须用当前编译上下文 g 编译（compileSelectInner），
// 以共享 $N 占位符递增计数器，否则编号会从头开始导致重复。
func (g *PostgresGrammar) joinTable(join JoinClause) string {
	if join.Sub != nil {
		subSQL := g.compileSelectInner(join.Sub, join.Sub.columns)
		return "(" + subSQL + ") AS " + g.wrapValue(join.Alias)
	}
	return g.WrapTable(join.Table)
}

// compileHavings 编译 HAVING 子句
func (g *PostgresGrammar) compileHavings(b *Builder) string {
	var parts []string
	for i, h := range b.havings {
		var clause string
		switch h.Type {
		case "basic":
			if expr, ok := h.Value.(Expression); ok {
				clause = g.WrapColumn(h.Column) + " " + h.Operator + " " + expr.Value()
			} else {
				clause = g.WrapColumn(h.Column) + " " + h.Operator + " " + g.nextParam()
			}
		case "raw":
			clause = g.convertRawPlaceholders(h.SQL, h.Bindings)
		case "between":
			if h.Not {
				clause = g.WrapColumn(h.Column) + " NOT BETWEEN " + g.nextParam() + " AND " + g.nextParam()
			} else {
				clause = g.WrapColumn(h.Column) + " BETWEEN " + g.nextParam() + " AND " + g.nextParam()
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
		if i == 0 {
			parts = append(parts, clause)
		} else {
			parts = append(parts, h.Boolean+" "+clause)
		}
	}
	return strings.Join(parts, " ")
}
