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

// convertRawPlaceholders 将原始 SQL 中的 ? 依次替换为 $N 占位符。
func (g *PostgresGrammar) convertRawPlaceholders(sql string) string {
	var buf strings.Builder
	buf.Grow(len(sql) + 8)
	for i := 0; i < len(sql); i++ {
		if sql[i] == '?' {
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
	if b.fromSub != nil {
		subSQL := g.compileSelectInner(b.fromSub, b.fromSub.columns)
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
		if lockSQL != "" {
			result += " " + lockSQL
		}
		return result
	}

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
			sql.WriteString(gClone.nextParam())
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
		// 检查值是否为 Expression
		if expr, ok := values[i].(Expression); ok {
			sql.WriteString(expr.Value())
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
	var joinWhere string
	if len(b.joins) > 0 {
		var joinConditions []string
		for _, join := range b.joins {
			for i, cond := range join.Conditions {
				var jc string
				switch cond.Type {
				case "column":
					jc = gClone.WrapColumn(cond.First) + " " + cond.Operator + " " + gClone.WrapColumn(cond.Second)
				case "value":
					jc = gClone.WrapColumn(cond.First) + " " + cond.Operator + " " + gClone.nextParam()
				case "raw":
					jc = gClone.convertRawPlaceholders(cond.SQL)
				}
				if jc == "" {
					continue
				}
				if i == 0 {
					joinConditions = append(joinConditions, jc)
				} else {
					joinConditions = append(joinConditions, cond.Boolean+" "+jc)
				}
			}
		}
		joinWhere = strings.Join(joinConditions, " ")
	}

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

// CompileTruncate 编译 TRUNCATE 语句
func (g *PostgresGrammar) CompileTruncate(b *Builder) string {
	return "TRUNCATE TABLE " + g.WrapTable(b.table)
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
			clause = g.convertRawPlaceholders(w.SQL)
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
			clause = g.WrapColumn(w.Column) + " LIKE " + g.nextParam()
		case WhereTypeNotLike:
			clause = g.WrapColumn(w.Column) + " NOT LIKE " + g.nextParam()
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

// compileJoin 编译 JOIN 子句
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
				result += g.WrapColumn(cond.First) + " " + cond.Operator + " " + g.nextParam()
			case "raw":
				result += g.convertRawPlaceholders(cond.SQL)
			}
		}
	}
	return result
}

// compileHavings 编译 HAVING 子句
func (g *PostgresGrammar) compileHavings(b *Builder) string {
	var parts []string
	for i, h := range b.havings {
		var clause string
		switch h.Type {
		case "basic":
			clause = g.WrapColumn(h.Column) + " " + h.Operator + " " + g.nextParam()
		case "raw":
			clause = g.convertRawPlaceholders(h.SQL)
		case "between":
			if h.Not {
				clause = g.WrapColumn(h.Column) + " NOT BETWEEN " + g.nextParam() + " AND " + g.nextParam()
			} else {
				clause = g.WrapColumn(h.Column) + " BETWEEN " + g.nextParam() + " AND " + g.nextParam()
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
