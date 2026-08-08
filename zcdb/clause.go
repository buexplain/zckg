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
