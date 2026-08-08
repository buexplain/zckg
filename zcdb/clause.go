package zcdb

// WhereType WHERE 子句类型
type WhereType int

const (
	WhereTypeBasic       WhereType = iota // column op value
	WhereTypeIn                           // column IN (...)
	WhereTypeNotIn                        // column NOT IN (...)
	WhereTypeNull                         // column IS NULL
	WhereTypeNotNull                      // column IS NOT NULL
	WhereTypeBetween                      // column BETWEEN min AND max
	WhereTypeNotBetween                   // column NOT BETWEEN min AND max
	WhereTypeRaw                          // 原始 SQL
	WhereTypeNested                       // 嵌套子查询 (...)
	WhereTypeColumn                       // column op column
	WhereTypeExists                       // EXISTS (subquery)
	WhereTypeNotExists                    // NOT EXISTS (subquery)
	WhereTypeSub                          // column op (subquery)
	WhereTypeInSub                        // column IN (subquery)
	WhereTypeNotInSub                     // column NOT IN (subquery)
	WhereTypeLike                         // column LIKE value
	WhereTypeNotLike                      // column NOT LIKE value
	WhereTypeNullSafe                     // column 空安全相等（MySQL <=> / PG IS NOT DISTINCT FROM / SQLite IS）
	WhereTypeNullSafeNot                  // column 空安全不等（MySQL NOT <=> / PG IS DISTINCT FROM / SQLite IS NOT）
)

// WhereClause 表示一个 WHERE 条件子句
type WhereClause struct {
	Type          WhereType
	Column        string
	Operator      string
	Value         any    // Basic
	Values        []any  // In / NotIn
	Min           any    // Between
	Max           any    // Between
	Boolean       string // "AND" / "OR"
	SQL           string // NewExpression
	Bindings      []any  // NewExpression 的绑定参数
	Second        string // Column 类型的第二列
	Nested        *Builder
	Sub           *Builder // 子查询 (WhereSub / WhereInSub)
	Not           bool     // 嵌套条件整体取反：NOT (...)
	CaseSensitive bool     // Like 区分大小写（方言分支编译）
}

// JoinType JOIN 类型
type JoinType int

const (
	JoinTypeInner JoinType = iota
	JoinTypeLeft
	JoinTypeRight
	JoinTypeCross
	JoinTypeCrossOn // CROSS JOIN 带 ON 条件（PG 不支持 CROSS JOIN ON，编译为 INNER JOIN）
)

// JoinCondition 表示一个 JOIN ON 条件
type JoinCondition struct {
	Type     string // "column" (ON col1 op col2) / "value" (ON col op ?) / "raw" / "null" / "in" / "inSub" / "subValue" / "exists" / "nested"
	First    string
	Operator string
	Second   string       // column 比较时的第二列
	Value    any          // value 比较时的值
	Values   []any        // in / notIn 的值列表
	Sub      *Builder     // inSub / exists / subValue 的子查询
	Nested   *JoinBuilder // nested 括号分组的嵌套条件
	Not      bool         // null / in / exists 的取反（NOT NULL / NOT IN / NOT EXISTS）
	SQL      string       // raw
	Bindings []any        // raw 绑定参数
	Boolean  string       // "AND" / "OR"
}

// JoinClause 表示一个 JOIN 子句
type JoinClause struct {
	Type       JoinType
	Table      string
	Sub        *Builder // 非 nil 时 JOIN 派生表（子查询），优先于 Table
	Alias      string   // 派生表别名
	Conditions []JoinCondition
	Joins      []JoinClause // 嵌套 join 组：编译为 (表 INNER JOIN ... ON ...) 括号形式
}

// JoinBuilder 用于构建复杂的 JOIN ON 条件
type JoinBuilder struct {
	Conditions []JoinCondition
	Joins      []JoinClause // 嵌套 join 组（JoinBuilder.JoinOn 追加）
	err        error        // 累积错误（如无效运算符）
	grammar    Grammar      // 供 WhereExists 回调等需要构造子查询的场景使用
	dao        *DBDao
}

// On 添加一个 AND ON 列比较条件
func (j *JoinBuilder) On(first, op, second string) *JoinBuilder {
	if err := validateOperator(op); err != nil {
		j.err = err
		return j
	}
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "column",
		First:    first,
		Operator: op,
		Second:   second,
		Boolean:  "AND",
	})
	return j
}

// OrOn 添加一个 OR ON 列比较条件
func (j *JoinBuilder) OrOn(first, op, second string) *JoinBuilder {
	if err := validateOperator(op); err != nil {
		j.err = err
		return j
	}
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "column",
		First:    first,
		Operator: op,
		Second:   second,
		Boolean:  "OR",
	})
	return j
}

// Where 添加一个 AND ON 值比较条件 (ON ... AND column op ?)。
// value 为 *Builder 时编译为子查询比较 (column op (SELECT ...))。
func (j *JoinBuilder) Where(column, op string, value any) *JoinBuilder {
	return j.addWhere(column, op, value, "AND")
}

// OrWhere 添加一个 OR ON 值比较条件。value 规则同 Where。
func (j *JoinBuilder) OrWhere(column, op string, value any) *JoinBuilder {
	return j.addWhere(column, op, value, "OR")
}

// addWhere Where/OrWhere 共用实现：*Builder 值走 subValue 类型，其余走 value 类型。
func (j *JoinBuilder) addWhere(column, op string, value any, boolean string) *JoinBuilder {
	if err := validateOperator(op); err != nil {
		j.err = err
		return j
	}
	if sub, ok := value.(*Builder); ok {
		j.Conditions = append(j.Conditions, JoinCondition{
			Type:     "subValue",
			First:    column,
			Operator: op,
			Sub:      sub,
			Boolean:  boolean,
		})
		return j
	}
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "value",
		First:    column,
		Operator: op,
		Value:    value,
		Boolean:  boolean,
	})
	return j
}

// WhereNull 添加 AND ON 空值条件 (ON ... AND column IS NULL)，支持多列展开。
func (j *JoinBuilder) WhereNull(columns ...string) *JoinBuilder {
	for _, column := range columns {
		j.Conditions = append(j.Conditions, JoinCondition{
			Type:    "null",
			First:   column,
			Boolean: "AND",
		})
	}
	return j
}

// WhereNotNull 添加 AND ON 非空条件 (ON ... AND column IS NOT NULL)，支持多列展开。
func (j *JoinBuilder) WhereNotNull(columns ...string) *JoinBuilder {
	for _, column := range columns {
		j.Conditions = append(j.Conditions, JoinCondition{
			Type:    "null",
			First:   column,
			Not:     true,
			Boolean: "AND",
		})
	}
	return j
}

// WhereIn 添加 AND ON IN 条件 (ON ... AND column IN (...))。
// values 支持 []any（值列表）或 *Builder（子查询 IN (SELECT ...)）。
func (j *JoinBuilder) WhereIn(column string, values any) *JoinBuilder {
	return j.addWhereIn(column, values, false)
}

// WhereNotIn 添加 AND ON NOT IN 条件。values 规则同 WhereIn。
func (j *JoinBuilder) WhereNotIn(column string, values any) *JoinBuilder {
	return j.addWhereIn(column, values, true)
}

// addWhereIn WhereIn/WhereNotIn 共用实现。
func (j *JoinBuilder) addWhereIn(column string, values any, not bool) *JoinBuilder {
	switch v := values.(type) {
	case []any:
		j.Conditions = append(j.Conditions, JoinCondition{
			Type:    "in",
			First:   column,
			Values:  v,
			Not:     not,
			Boolean: "AND",
		})
	case *Builder:
		j.Conditions = append(j.Conditions, JoinCondition{
			Type:    "inSub",
			First:   column,
			Sub:     v,
			Not:     not,
			Boolean: "AND",
		})
	default:
		j.err = ErrInvalidSubQuery
	}
	return j
}

// WhereExists 添加 AND ON EXISTS 条件 (ON ... AND EXISTS (SELECT ...))。
// sub 支持 func(*Builder) 回调或 *Builder（需由 Builder.JoinOn 等入口注入 grammar）。
func (j *JoinBuilder) WhereExists(sub any) *JoinBuilder {
	var subBuilder *Builder
	switch s := sub.(type) {
	case func(*Builder):
		if j.grammar == nil {
			j.err = ErrInvalidSubQuery
			return j
		}
		subBuilder = NewBuilder(j.grammar, j.dao)
		s(subBuilder)
	case *Builder:
		subBuilder = s
	default:
		j.err = ErrInvalidSubQuery
		return j
	}
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:    "exists",
		Sub:     subBuilder,
		Boolean: "AND",
	})
	return j
}

// WhereNested 添加括号分组的嵌套条件 (ON ... AND (...))。
func (j *JoinBuilder) WhereNested(callback func(*JoinBuilder)) *JoinBuilder {
	return j.addNested(callback, "AND")
}

// OrWhereNested 添加 OR 连接的括号分组嵌套条件。
func (j *JoinBuilder) OrWhereNested(callback func(*JoinBuilder)) *JoinBuilder {
	return j.addNested(callback, "OR")
}

// OnNested 添加括号分组的嵌套 On 条件（与 WhereNested 同语义，命名对齐 On 系列）。
func (j *JoinBuilder) OnNested(callback func(*JoinBuilder)) *JoinBuilder {
	return j.addNested(callback, "AND")
}

// addNested 嵌套条件组共用实现：回调内的条件编译时加括号。
func (j *JoinBuilder) addNested(callback func(*JoinBuilder), boolean string) *JoinBuilder {
	nested := &JoinBuilder{grammar: j.grammar, dao: j.dao}
	callback(nested)
	if nested.err != nil {
		j.err = nested.err
		return j
	}
	if len(nested.Conditions) > 0 || len(nested.Joins) > 0 {
		j.Conditions = append(j.Conditions, JoinCondition{
			Type:    "nested",
			Nested:  nested,
			Boolean: boolean,
		})
	}
	return j
}

// JoinOn 在当前 join 内追加嵌套 INNER JOIN（编译为带括号的 join 组）。
func (j *JoinBuilder) JoinOn(table string, callback func(*JoinBuilder)) *JoinBuilder {
	inner := &JoinBuilder{grammar: j.grammar, dao: j.dao}
	callback(inner)
	if inner.err != nil {
		j.err = inner.err
		return j
	}
	j.Joins = append(j.Joins, JoinClause{
		Type:       JoinTypeInner,
		Table:      table,
		Conditions: inner.Conditions,
		Joins:      inner.Joins,
	})
	return j
}

// CrossJoinOn 在当前 join 内追加嵌套 CROSS JOIN ON（编译为带括号的 join 组）。
func (j *JoinBuilder) CrossJoinOn(table, first, op, second string) *JoinBuilder {
	j.Joins = append(j.Joins, JoinClause{
		Type:  JoinTypeCrossOn,
		Table: table,
		Conditions: []JoinCondition{{
			Type:     "column",
			First:    first,
			Operator: op,
			Second:   second,
			Boolean:  "AND",
		}},
	})
	return j
}

// Raw 添加一个原始 SQL ON 条件
func (j *JoinBuilder) Raw(sql string, bindings ...any) *JoinBuilder {
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "raw",
		SQL:      sql,
		Bindings: bindings,
		Boolean:  "AND",
	})
	return j
}

// OrderClause 表示一个 ORDER BY 子句
type OrderClause struct {
	Column    string
	Direction string // "ASC" / "DESC"
	Raw       string // 如果非空，则使用原始 SQL
}

// GroupClause 表示一个 GROUP BY 子句。
// Raw 非空时为原始 SQL（GroupByRaw），否则 Column 经 WrapColumn 引用。
type GroupClause struct {
	Column   string
	Raw      string
	Bindings []any // Raw 的绑定参数
}

// HavingClause 表示一个 HAVING 子句
type HavingClause struct {
	Type     string // "basic" / "raw" / "between" / "null" / "notNull" / "nested"
	Column   string
	Operator string
	Value    any
	Boolean  string // "AND" / "OR"
	SQL      string // raw
	Bindings []any
	Min      any // between
	Max      any // between
	Not      bool
	Nested   *Builder // nested 括号分组的嵌套 HAVING 条件
}

// UnionClause 表示一个 UNION 子句
type UnionClause struct {
	Query *Builder
	All   bool
}

// SelectSub 表示一个 SELECT 中的子查询列
type SelectSub struct {
	Query *Builder
	Alias string
}
