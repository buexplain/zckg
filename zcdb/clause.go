package zcdb

// WhereType WHERE 子句类型
type WhereType int

const (
	WhereTypeBasic      WhereType = iota // column op value
	WhereTypeIn                          // column IN (...)
	WhereTypeNotIn                       // column NOT IN (...)
	WhereTypeNull                        // column IS NULL
	WhereTypeNotNull                     // column IS NOT NULL
	WhereTypeBetween                     // column BETWEEN min AND max
	WhereTypeNotBetween                  // column NOT BETWEEN min AND max
	WhereTypeRaw                         // 原始 SQL
	WhereTypeNested                      // 嵌套子查询 (...)
	WhereTypeColumn                      // column op column
	WhereTypeExists                      // EXISTS (subquery)
	WhereTypeNotExists                   // NOT EXISTS (subquery)
	WhereTypeSub                         // column op (subquery)
	WhereTypeInSub                       // column IN (subquery)
	WhereTypeNotInSub                    // column NOT IN (subquery)
	WhereTypeLike                        // column LIKE value
	WhereTypeNotLike                     // column NOT LIKE value
)

// WhereClause 表示一个 WHERE 条件子句
type WhereClause struct {
	Type     WhereType
	Column   string
	Operator string
	Value    any    // Basic
	Values   []any  // In / NotIn
	Min      any    // Between
	Max      any    // Between
	Boolean  string // "AND" / "OR"
	SQL      string // NewExpression
	Bindings []any  // NewExpression 的绑定参数
	Second   string // Column 类型的第二列
	Nested   *Builder
	Sub      *Builder // 子查询 (WhereSub / WhereInSub)
}

// JoinType JOIN 类型
type JoinType int

const (
	JoinTypeInner JoinType = iota
	JoinTypeLeft
	JoinTypeRight
	JoinTypeCross
)

// JoinCondition 表示一个 JOIN ON 条件
type JoinCondition struct {
	Type     string // "column" (ON col1 op col2) / "value" (ON col op ?) / "raw"
	First    string
	Operator string
	Second   string // column 比较时的第二列
	Value    any    // value 比较时的值
	SQL      string // raw
	Bindings []any  // raw 绑定参数
	Boolean  string // "AND" / "OR"
}

// JoinClause 表示一个 JOIN 子句
type JoinClause struct {
	Type       JoinType
	Table      string
	Sub        *Builder // 非 nil 时 JOIN 派生表（子查询），优先于 Table
	Alias      string   // 派生表别名
	Conditions []JoinCondition
}

// JoinBuilder 用于构建复杂的 JOIN ON 条件
type JoinBuilder struct {
	Conditions []JoinCondition
	err        error // 累积错误（如无效运算符）
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

// Where 添加一个 AND ON 值比较条件 (ON ... AND column op ?)
func (j *JoinBuilder) Where(column, op string, value any) *JoinBuilder {
	if err := validateOperator(op); err != nil {
		j.err = err
		return j
	}
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "value",
		First:    column,
		Operator: op,
		Value:    value,
		Boolean:  "AND",
	})
	return j
}

// OrWhere 添加一个 OR ON 值比较条件
func (j *JoinBuilder) OrWhere(column, op string, value any) *JoinBuilder {
	if err := validateOperator(op); err != nil {
		j.err = err
		return j
	}
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "value",
		First:    column,
		Operator: op,
		Value:    value,
		Boolean:  "OR",
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

// HavingClause 表示一个 HAVING 子句
type HavingClause struct {
	Type     string // "basic" / "raw" / "between"
	Column   string
	Operator string
	Value    any
	Boolean  string // "AND" / "OR"
	SQL      string // raw
	Bindings []any
	Min      any // between
	Max      any // between
	Not      bool
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
