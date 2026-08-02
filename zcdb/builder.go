package zcdb

// SelectColumn 表示 SELECT 中的一个列。
// Raw 为 true 时，Value 直接嵌入 SQL，不经过 WrapColumn 引用。
type SelectColumn struct {
	Value string
	Raw   bool
}

// Builder 是查询构造器的核心，负责收集用户的查询意图和数据。
// 遵循 Laravel 的设计：Builder 积累状态，Grammar 负责编译 SQL。
type Builder struct {
	grammar Grammar
	dao     *DBDao // 持有 DBDao 引用，用于终端方法执行 SQL

	// 查询状态
	table      string
	columns    []SelectColumn // 用于 SELECT 的列
	selectSubs []SelectSub
	distinct   bool
	fromSub    *Builder // FROM 子查询
	fromAlias  string   // FROM 子查询别名
	joins      []JoinClause
	wheres     []WhereClause
	groups     []string
	havings    []HavingClause
	orders     []OrderClause
	limit      int
	offset     int
	unions     []UnionClause
	lockClause string
	err        error // 累积错误（如无效运算符）
}

// NewBuilder 创建一个新的sql构造器。
func NewBuilder(grammar Grammar, dao *DBDao) *Builder {
	return &Builder{
		grammar: grammar,
		dao:     dao,
	}
}

// validOperators 运算符白名单，防止 SQL 注入。
var validOperators = map[string]bool{
	"=": true, "!=": true, "<>": true,
	"<": true, ">": true, "<=": true, ">=": true,
	"LIKE": true, "NOT LIKE": true,
	"IS": true, "IS NOT": true,
	"IN": true, "NOT IN": true,
	"BETWEEN": true, "NOT BETWEEN": true,
}

// validateOperator 检查运算符是否在白名单内。
func validateOperator(op string) error {
	// 尝试大写匹配
	upper := op
	if len(upper) > 0 && upper[0] >= 'a' && upper[0] <= 'z' {
		upper = ""
		for _, c := range op {
			if c >= 'a' && c <= 'z' {
				upper += string(c - 32)
			} else {
				upper += string(c)
			}
		}
	}
	if validOperators[upper] {
		return nil
	}
	return ErrInvalidOperator
}

// ==================== 表和列 ====================

func (b *Builder) Table(tableName string) *Builder {
	b.table = tableName
	return b
}

// Select 显式指定 SELECT 的列名。
func (b *Builder) Select(columns ...string) *Builder {
	b.columns = make([]SelectColumn, len(columns))
	for i, col := range columns {
		b.columns[i] = SelectColumn{Value: col, Raw: false}
	}
	return b
}

// SelectRaw 添加原始 SQL 表达式作为列，不经过 WrapColumn 引用。
func (b *Builder) SelectRaw(expression string) *Builder {
	b.columns = append(b.columns, SelectColumn{Value: expression, Raw: true})
	return b
}

// SelectSubquery 添加一个子查询作为 SELECT 列。
func (b *Builder) SelectSubquery(sub *Builder, alias string) *Builder {
	b.selectSubs = append(b.selectSubs, SelectSub{Query: sub, Alias: alias})
	return b
}

// FromSub 设置 FROM 子查询。
func (b *Builder) FromSub(sub *Builder, alias string) *Builder {
	b.fromSub = sub
	b.fromAlias = alias
	return b
}

// Distinct 设置查询返回去重结果。
func (b *Builder) Distinct() *Builder {
	b.distinct = true
	return b
}

// ==================== WHERE 条件 ====================

// Where 添加一个 AND WHERE 条件。
// value 支持基本类型（int、string、float、bool、time.Time 等）和 Expression（直接嵌入 SQL，不作为绑定参数）。
func (b *Builder) Where(column string, op string, value any) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeBasic,
		Column:   column,
		Operator: op,
		Value:    value,
		Boolean:  "AND",
	})
	return b
}

// OrWhere 添加一个 OR WHERE 条件。
// value 类型规则同 Where。
func (b *Builder) OrWhere(column string, op string, value any) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeBasic,
		Column:   column,
		Operator: op,
		Value:    value,
		Boolean:  "OR",
	})
	return b
}

// WhereIn 添加一个 WHERE column IN (...) 条件。
// values 元素支持基本类型（int、string 等）。
func (b *Builder) WhereIn(column string, values []any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeIn,
		Column:  column,
		Values:  values,
		Boolean: "AND",
	})
	return b
}

// WhereNotIn 添加一个 WHERE column NOT IN (...) 条件。
// values 元素类型规则同 WhereIn。
func (b *Builder) WhereNotIn(column string, values []any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotIn,
		Column:  column,
		Values:  values,
		Boolean: "AND",
	})
	return b
}

// WhereNull 添加一个 WHERE column IS NULL 条件。
func (b *Builder) WhereNull(column string) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNull,
		Column:  column,
		Boolean: "AND",
	})
	return b
}

// WhereNotNull 添加一个 WHERE column IS NOT NULL 条件。
func (b *Builder) WhereNotNull(column string) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotNull,
		Column:  column,
		Boolean: "AND",
	})
	return b
}

// WhereBetween 添加一个 WHERE column BETWEEN min AND max 条件。
// min、max 支持基本类型（int、string、time.Time 等）。
func (b *Builder) WhereBetween(column string, min, max any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeBetween,
		Column:  column,
		Min:     min,
		Max:     max,
		Boolean: "AND",
	})
	return b
}

// WhereNotBetween 添加一个 WHERE column NOT BETWEEN min AND max 条件。
// min、max 类型规则同 WhereBetween。
func (b *Builder) WhereNotBetween(column string, min, max any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotBetween,
		Column:  column,
		Min:     min,
		Max:     max,
		Boolean: "AND",
	})
	return b
}

// WhereRaw 添加一个原始 SQL WHERE 条件。
// bindings 为 SQL 中 ? 占位符的绑定值，支持基本类型和 Expression（直接嵌入 SQL）。
func (b *Builder) WhereRaw(sql string, bindings ...any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeRaw,
		SQL:      sql,
		Bindings: bindings,
		Boolean:  "AND",
	})
	return b
}

// OrWhereRaw 添加一个原始 SQL OR WHERE 条件。
// bindings 类型规则同 WhereRaw。
func (b *Builder) OrWhereRaw(sql string, bindings ...any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeRaw,
		SQL:      sql,
		Bindings: bindings,
		Boolean:  "OR",
	})
	return b
}

// WhereColumn 添加一个两列比较的 WHERE 条件。
func (b *Builder) WhereColumn(first string, op string, second string) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeColumn,
		Column:   first,
		Operator: op,
		Second:   second,
		Boolean:  "AND",
	})
	return b
}

// WhereNested 添加一个嵌套的 WHERE 子句组。
// callback 接收一个新的 Builder，用于构建嵌套条件。
func (b *Builder) WhereNested(callback func(*Builder)) *Builder {
	nested := NewBuilder(b.grammar, b.dao)
	nested.table = b.table
	callback(nested)
	if len(nested.wheres) > 0 {
		b.wheres = append(b.wheres, WhereClause{
			Type:    WhereTypeNested,
			Nested:  nested,
			Boolean: "AND",
		})
	}
	return b
}

// OrWhereNested 添加一个嵌套的 OR WHERE 子句组。
func (b *Builder) OrWhereNested(callback func(*Builder)) *Builder {
	nested := NewBuilder(b.grammar, b.dao)
	nested.table = b.table
	callback(nested)
	if len(nested.wheres) > 0 {
		b.wheres = append(b.wheres, WhereClause{
			Type:    WhereTypeNested,
			Nested:  nested,
			Boolean: "OR",
		})
	}
	return b
}

// WhereExists 添加一个 WHERE EXISTS (subquery) 条件。
func (b *Builder) WhereExists(callback func(*Builder)) *Builder {
	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeExists,
		Nested:  sub,
		Boolean: "AND",
	})
	return b
}

// WhereNotExists 添加一个 WHERE NOT EXISTS (subquery) 条件。
func (b *Builder) WhereNotExists(callback func(*Builder)) *Builder {
	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotExists,
		Nested:  sub,
		Boolean: "AND",
	})
	return b
}

// WhereSub 添加一个子查询 WHERE 条件: WHERE column op (SELECT ...)
func (b *Builder) WhereSub(column string, op string, callback func(*Builder)) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeSub,
		Column:   column,
		Operator: op,
		Sub:      sub,
		Boolean:  "AND",
	})
	return b
}

// OrWhereSub 添加一个子查询 OR WHERE 条件。
func (b *Builder) OrWhereSub(column string, op string, callback func(*Builder)) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeSub,
		Column:   column,
		Operator: op,
		Sub:      sub,
		Boolean:  "OR",
	})
	return b
}

// WhereInSub 添加一个 WHERE column IN (SELECT ...) 条件。
func (b *Builder) WhereInSub(column string, callback func(*Builder)) *Builder {
	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeInSub,
		Column:  column,
		Sub:     sub,
		Boolean: "AND",
	})
	return b
}

// WhereNotInSub 添加一个 WHERE column NOT IN (SELECT ...) 条件。
func (b *Builder) WhereNotInSub(column string, callback func(*Builder)) *Builder {
	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotInSub,
		Column:  column,
		Sub:     sub,
		Boolean: "AND",
	})
	return b
}

// WhereLike 添加一个 WHERE column LIKE value 条件。
// value 通常为 string 类型（如 "%keyword%"），也支持 Expression。
func (b *Builder) WhereLike(column string, value any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeLike,
		Column:  column,
		Value:   value,
		Boolean: "AND",
	})
	return b
}

// OrWhereLike 添加一个 OR WHERE column LIKE value 条件。
// value 类型规则同 WhereLike。
func (b *Builder) OrWhereLike(column string, value any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeLike,
		Column:  column,
		Value:   value,
		Boolean: "OR",
	})
	return b
}

// WhereNotLike 添加一个 WHERE column NOT LIKE value 条件。
// value 类型规则同 WhereLike。
func (b *Builder) WhereNotLike(column string, value any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotLike,
		Column:  column,
		Value:   value,
		Boolean: "AND",
	})
	return b
}

// ==================== JOIN ====================

// Join 添加一个 INNER JOIN（单条件简写）。
func (b *Builder) Join(table, first, op, second string) *Builder {
	b.joins = append(b.joins, JoinClause{
		Type:  JoinTypeInner,
		Table: table,
		Conditions: []JoinCondition{{
			Type:     "column",
			First:    first,
			Operator: op,
			Second:   second,
			Boolean:  "AND",
		}},
	})
	return b
}

// LeftJoin 添加一个 LEFT JOIN（单条件简写）。
func (b *Builder) LeftJoin(table, first, op, second string) *Builder {
	b.joins = append(b.joins, JoinClause{
		Type:  JoinTypeLeft,
		Table: table,
		Conditions: []JoinCondition{{
			Type:     "column",
			First:    first,
			Operator: op,
			Second:   second,
			Boolean:  "AND",
		}},
	})
	return b
}

// RightJoin 添加一个 RIGHT JOIN（单条件简写）。
func (b *Builder) RightJoin(table, first, op, second string) *Builder {
	b.joins = append(b.joins, JoinClause{
		Type:  JoinTypeRight,
		Table: table,
		Conditions: []JoinCondition{{
			Type:     "column",
			First:    first,
			Operator: op,
			Second:   second,
			Boolean:  "AND",
		}},
	})
	return b
}

// CrossJoin 添加一个 CROSS JOIN。
func (b *Builder) CrossJoin(table string) *Builder {
	b.joins = append(b.joins, JoinClause{
		Type:  JoinTypeCross,
		Table: table,
	})
	return b
}

// JoinOn 添加一个 INNER JOIN（支持多条件）。
func (b *Builder) JoinOn(table string, callback func(*JoinBuilder)) *Builder {
	jb := &JoinBuilder{}
	callback(jb)
	if jb.err != nil {
		b.err = jb.err
		return b
	}
	b.joins = append(b.joins, JoinClause{
		Type:       JoinTypeInner,
		Table:      table,
		Conditions: jb.Conditions,
	})
	return b
}

// LeftJoinOn 添加一个 LEFT JOIN（支持多条件）。
func (b *Builder) LeftJoinOn(table string, callback func(*JoinBuilder)) *Builder {
	jb := &JoinBuilder{}
	callback(jb)
	if jb.err != nil {
		b.err = jb.err
		return b
	}
	b.joins = append(b.joins, JoinClause{
		Type:       JoinTypeLeft,
		Table:      table,
		Conditions: jb.Conditions,
	})
	return b
}

// RightJoinOn 添加一个 RIGHT JOIN（支持多条件）。
func (b *Builder) RightJoinOn(table string, callback func(*JoinBuilder)) *Builder {
	jb := &JoinBuilder{}
	callback(jb)
	if jb.err != nil {
		b.err = jb.err
		return b
	}
	b.joins = append(b.joins, JoinClause{
		Type:       JoinTypeRight,
		Table:      table,
		Conditions: jb.Conditions,
	})
	return b
}

// ==================== GROUP BY / HAVING ====================

// GroupBy 设置 GROUP BY 子句。
func (b *Builder) GroupBy(columns ...string) *Builder {
	b.groups = append(b.groups, columns...)
	return b
}

// Having 添加一个 HAVING 条件。
// value 支持基本类型（int、string、float 等）。
func (b *Builder) Having(column string, op string, value any) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	b.havings = append(b.havings, HavingClause{
		Type:     "basic",
		Column:   column,
		Operator: op,
		Value:    value,
		Boolean:  "AND",
	})
	return b
}

// OrHaving 添加一个 OR HAVING 条件。
// value 类型规则同 Having。
func (b *Builder) OrHaving(column string, op string, value any) *Builder {
	if err := validateOperator(op); err != nil {
		b.err = err
		return b
	}
	b.havings = append(b.havings, HavingClause{
		Type:     "basic",
		Column:   column,
		Operator: op,
		Value:    value,
		Boolean:  "OR",
	})
	return b
}

// HavingRaw 添加一个原始 SQL HAVING 条件。
// bindings 为 SQL 中 ? 占位符的绑定值，支持基本类型和 Expression（直接嵌入 SQL）。
func (b *Builder) HavingRaw(sql string, bindings ...any) *Builder {
	b.havings = append(b.havings, HavingClause{
		Type:     "raw",
		SQL:      sql,
		Bindings: bindings,
		Boolean:  "AND",
	})
	return b
}

// HavingBetween 添加一个 HAVING column BETWEEN min AND max 条件。
// min、max 支持基本类型（int、float 等）。
func (b *Builder) HavingBetween(column string, min, max any) *Builder {
	b.havings = append(b.havings, HavingClause{
		Type:    "between",
		Column:  column,
		Min:     min,
		Max:     max,
		Boolean: "AND",
	})
	return b
}

// HavingNotBetween 添加一个 HAVING column NOT BETWEEN min AND max 条件。
// min、max 类型规则同 HavingBetween。
func (b *Builder) HavingNotBetween(column string, min, max any) *Builder {
	b.havings = append(b.havings, HavingClause{
		Type:    "between",
		Column:  column,
		Min:     min,
		Max:     max,
		Not:     true,
		Boolean: "AND",
	})
	return b
}

// ==================== ORDER BY ====================

// OrderBy 添加一个 ORDER BY 子句。
func (b *Builder) OrderBy(column string, direction string) *Builder {
	dir := "ASC"
	if len(direction) > 0 {
		upper := direction
		if direction[0] >= 'a' && direction[0] <= 'z' {
			upper = ""
			for _, c := range direction {
				if c >= 'a' && c <= 'z' {
					upper += string(c - 32)
				} else {
					upper += string(c)
				}
			}
		}
		if upper == "DESC" {
			dir = "DESC"
		}
	}
	b.orders = append(b.orders, OrderClause{Column: column, Direction: dir})
	return b
}

// OrderByDesc 按降序添加一个 ORDER BY 子句。
func (b *Builder) OrderByDesc(column string) *Builder {
	return b.OrderBy(column, "DESC")
}

// OrderByRaw 添加一个原始 SQL ORDER BY 子句。
func (b *Builder) OrderByRaw(sql string) *Builder {
	b.orders = append(b.orders, OrderClause{Raw: sql})
	return b
}

// InRandomOrder 按随机顺序排序。
func (b *Builder) InRandomOrder() *Builder {
	b.orders = append(b.orders, OrderClause{Raw: b.grammar.CompileRandom()})
	return b
}

// ==================== LIMIT / OFFSET ====================

// Limit 设置查询结果数量限制。
func (b *Builder) Limit(n int) *Builder {
	b.limit = n
	return b
}

// Offset 设置查询结果偏移量。
func (b *Builder) Offset(n int) *Builder {
	b.offset = n
	return b
}

// ForPage 设置分页（page 从 1 开始）。
func (b *Builder) ForPage(page, perPage int) *Builder {
	if page < 1 {
		page = 1
	}
	b.limit = perPage
	b.offset = (page - 1) * perPage
	return b
}

// ==================== UNION ====================

// Union 添加一个 UNION 查询。
func (b *Builder) Union(query *Builder) *Builder {
	b.unions = append(b.unions, UnionClause{Query: query, All: false})
	return b
}

// UnionAll 添加一个 UNION ALL 查询。
func (b *Builder) UnionAll(query *Builder) *Builder {
	b.unions = append(b.unions, UnionClause{Query: query, All: true})
	return b
}

// ==================== LOCK ====================

// LockForUpdate 设置排他锁 (FOR UPDATE)。
func (b *Builder) LockForUpdate() *Builder {
	b.lockClause = "FOR UPDATE"
	return b
}

// SharedLock 设置共享锁，不同方言语法：
//   - MySQL:      LOCK IN SHARE MODE
//   - PostgreSQL: FOR SHARE（自动转换）
//   - SQLite:     不支持，返回 ErrSQLiteLockNotSupported
func (b *Builder) SharedLock() *Builder {
	b.lockClause = "LOCK IN SHARE MODE"
	return b
}

// ==================== 终端方法：编译 SQL ====================

// ToSelect 编译 SELECT 查询。
// 列名通过 Select() 链式调用指定，未指定时默认 SELECT *。
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToSelect() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" && b.fromSub == nil {
		return "", nil, ErrEmptyTable
	}

	// 检查不支持的锁子句场景
	if b.lockClause != "" {
		switch b.grammar.(type) {
		case *PostgresGrammar:
			if len(b.unions) > 0 {
				return "", nil, ErrPgUnionLockNotSupported
			}
		case *SQLiteGrammar:
			return "", nil, ErrSQLiteLockNotSupported
		}
	}

	sql := b.grammar.CompileSelect(b, b.columns)
	args := b.collectSelectBindings()

	return sql, args, nil
}

// ToInsert 编译 INSERT 语句。
//
// data 必须是结构体或结构体切片（也可以是指向它们的指针），否则返回 ErrInvalidStruct。
// 结构体字段通过 `db` 标签映射列名，无标签时自动转为 snake_case，`db:"-"` 的字段会被跳过。
//
// 字段值处理规则：
//   - any(interface{}) 类型字段：nil → 该列被跳过（不参与 INSERT）；非 nil → 取实际值
//   - 指针类型字段（*string、*int 等）：nil → 该列被跳过；非 nil → 自动解引用为具体值
//   - 其它类型（含 Expression）：直接取值
//
// 切片输入时以首行为模板确定列，后续行对应列若为 nil 则传入 nil（SQL NULL）。
//
// 示例：
//
//	type User struct {
//	    Name  *string `db:"name"`
//	    Age   *int    `db:"age"`
//	    Email any     `db:"email"`
//	}
//
//	// 单条插入：Email 为 nil 会被跳过
//	name := "alice"
//	age := 25
//	sql, args, err := NewBuilder(g).
//	    Table("users").
//	    ToInsert(User{Name: &name, Age: &age})
//	// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?)
//	// args: ["alice", 25]
//
//	// 批量插入
//	age2 := 30
//	sql, args, err = NewBuilder(g).
//	    Table("users").
//	    ToInsert([]User{
//	        {Name: &name, Age: &age},
//	        {Name: &name, Age: &age2, Email: "bob@test.com"},
//	    })
//	// SQL:  INSERT INTO `users` (`name`, `age`) VALUES (?, ?), (?, ?)
//	// args: ["alice", 25, "alice", 30]
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToInsert(data any) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	columns, rows, err := extractInsertData(data)
	if err != nil {
		return "", nil, err
	}

	sql := b.grammar.CompileInsert(b, columns, rows)

	// 扁平化所有行的值作为绑定参数
	var args []any
	for _, row := range rows {
		args = append(args, row...)
	}

	return sql, args, nil
}

// ToInsertOrIgnore 编译 INSERT OR IGNORE 语句。
// 插入时忽略已存在的记录，不同方言语法：
//   - MySQL:      INSERT IGNORE INTO ...
//   - PostgreSQL: INSERT INTO ... ON CONFLICT DO NOTHING
//   - SQLite:     INSERT OR IGNORE INTO ...
//
// data 的约束和字段处理规则与 ToInsert 完全一致，参见 ToInsert 文档。
//
// 示例：
//
//	sql, args, err := NewBuilder(g).
//	    Table("users").
//	    ToInsertOrIgnore(User{Name: "alice", Age: 25, Email: "alice@test.com"})
//	// MySQL SQL: INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToInsertOrIgnore(data any) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	columns, rows, err := extractInsertData(data)
	if err != nil {
		return "", nil, err
	}

	sql := b.grammar.CompileInsertOrIgnore(b, columns, rows)

	var args []any
	for _, row := range rows {
		args = append(args, row...)
	}

	return sql, args, nil
}

// ToUpsert 编译 UPSERT（插入或更新）语句。
// 不同方言语法：
//   - MySQL:      INSERT INTO ... ON DUPLICATE KEY UPDATE ...
//   - PostgreSQL: INSERT INTO ... ON CONFLICT (...) DO UPDATE SET ...
//   - SQLite:     INSERT INTO ... ON CONFLICT (...) DO UPDATE SET ...
//
// 参数说明：
//   - data：必须是结构体或结构体切片，约束和字段处理规则与 ToInsert 一致
//   - uniqueBy：唯一索引列名，用于判断是否冲突
//   - updateColumns：冲突时要更新的列名列表；为空时更新所有插入列（排除 uniqueBy 列）
//
// 示例：
//
//	sql, args, err := NewBuilder(g).
//	    Table("users").
//	    ToUpsert(
//	        User{Name: "alice", Age: 25, Email: "alice@test.com"},
//	        []string{"email"},       // 以 email 作为唯一键
//	        []string{"name", "age"}, // 冲突时更新 name 和 age
//	    )
//	// MySQL SQL: INSERT INTO `users` (...) VALUES (?, ?, ?)
//	//            ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToUpsert(data any, uniqueBy []string, updateColumns []string) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	columns, rows, err := extractInsertData(data)
	if err != nil {
		return "", nil, err
	}

	// 如果未指定更新列，则更新所有插入列
	if len(updateColumns) == 0 {
		updateColumns = columns
	}

	// 构造更新值：使用 VALUES() 引用插入值（MySQL）或 EXCLUDED 引用（PostgreSQL）
	// 这里不传具体 values，Grammar 自行处理语法
	sql := b.grammar.CompileUpsert(b, columns, rows, uniqueBy, updateColumns, nil)

	var args []any
	for _, row := range rows {
		args = append(args, row...)
	}

	return sql, args, nil
}

// ToInsertUsing 编译 INSERT INTO ... SELECT 语句，将一个 SELECT 查询的结果插入目标表。
//
// 参数说明：
//   - columns：目标表的列名列表
//   - callback：构建 SELECT 子查询的回调函数
//
// 示例：
//
//	sql, args, err := NewBuilder(g).
//	    Table("users_archive").
//	    ToInsertUsing([]string{"name", "age", "email"}, func(sub *Builder) {
//	        sub.Table("users").Select("name", "age", "email").Where("status", "=", "active")
//	    })
//	// SQL:  INSERT INTO `users_archive` (`name`, `age`, `email`)
//	//       SELECT `name`, `age`, `email` FROM `users` WHERE `status` = ?
//	// args: ["active"]
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToInsertUsing(columns []string, callback func(*Builder)) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)

	sql := b.grammar.CompileInsertUsing(b, columns, sub)
	args := sub.collectSelectBindings()

	return sql, args, nil
}

// ToUpdate 编译 UPDATE 语句。
//
// data 必须是结构体（也可以是指向结构体的指针），否则返回 ErrInvalidStruct。
// 结构体字段通过 `db` 标签映射列名，无标签时自动转为 snake_case，`db:"-"` 的字段会被跳过。
//
// 字段值处理规则：
//   - any(interface{}) 类型字段：nil → 该列被跳过（不参与 SET）；非 nil → 取实际值
//   - 指针类型字段（*string、*int 等）：nil → 该列被跳过；非 nil → 自动解引用为具体值
//   - Expression 类型（Raw 表达式）：直接嵌入 SQL，不作为绑定参数
//   - 其它具体类型：直接取值
//
// 示例：
//
//	type UserUpdate struct {
//	    Name   *string `db:"name"`
//	    Age    *int    `db:"age"`
//	    Status any     `db:"status"`
//	}
//
//	// 部分更新：Status 为 nil 被跳过，Name 和 Age 解引用为具体值
//	name := "alice_new"
//	age := 26
//	sql, args, err := NewBuilder(g).
//	    Table("users").
//	    Where("id", "=", 1).
//	    ToUpdate(UserUpdate{Name: &name, Age: &age})
//	// SQL:  UPDATE `users` SET `name` = ?, `age` = ? WHERE `id` = ?
//	// args: ["alice_new", 26, 1]
//
//	// 使用 Raw 表达式：自增 age
//	sql, args, err = NewBuilder(g).
//	    Table("users").
//	    Where("id", "=", 1).
//	    ToUpdate(userUpdate{Age: NewExpression("`age` + 1")})
//	// SQL:  UPDATE `users` SET `age` = `age` + 1 WHERE `id` = ?
//	// args: [1]
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToUpdate(data any) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	columns, values, err := extractUpdateData(data)
	if err != nil {
		return "", nil, err
	}

	sql := b.grammar.CompileUpdate(b, columns, values)

	// 绑定参数顺序须与 SQL 中占位符出现顺序一致：
	// MySQL 为 UPDATE ... JOIN(ON) ... SET ... WHERE，JOIN 条件在 SET 之前；
	// PostgreSQL/SQLite 为 UPDATE ... SET ... FROM ... WHERE(JOIN 条件并入 WHERE 前部)，SET 在 JOIN 之前。
	// 通过 grammar.UpdateSetBeforeJoin() 区分两种顺序。
	var args []any
	setBindings := func() {
		for _, v := range values {
			if _, ok := v.(Expression); !ok {
				args = append(args, v)
			}
		}
	}
	if b.grammar.UpdateSetBeforeJoin() {
		// SET → JOIN → WHERE
		setBindings()
		args = append(args, b.collectJoinBindings()...)
	} else {
		// JOIN → SET → WHERE
		args = append(args, b.collectJoinBindings()...)
		setBindings()
	}
	args = append(args, b.collectWhereBindings()...)

	return sql, args, nil
}

// ToDelete 编译 DELETE 语句。
// 通过 Where() 等链式调用指定删除条件，无条件时将删除全表数据。
//
// 示例：
//
//	sql, args, err := NewBuilder(g).
//	    Table("users").
//	    Where("id", "=", 1).
//	    ToDelete()
//	// SQL:  DELETE FROM `users` WHERE `id` = ?
//	// args: [1]
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToDelete() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	sql := b.grammar.CompileDelete(b)
	args := b.collectWhereBindings()

	return sql, args, nil
}

// ToTruncate 编译 TRUNCATE 语句（清空表数据）。
// SQLite 方言不支持 TRUNCATE，会自动转为 DELETE FROM。
//
// 示例：
//
//	sql, err := NewBuilder(g).Table("users").ToTruncate()
//	// MySQL/PG SQL: TRUNCATE TABLE `users`
//	// SQLite SQL:   DELETE FROM `users`
//
// 返回 (SQL, 错误)
func (b *Builder) ToTruncate() (string, error) {
	if b.table == "" {
		return "", ErrEmptyTable
	}
	return b.grammar.CompileTruncate(b), nil
}

// ToCount 编译 COUNT 查询。
// 通过复用 CompileSelect 生成 SELECT COUNT(*) FROM ... 语句。
// 当查询包含 UNION 时，将整个 UNION 包裹为子查询再计数，
// 避免生成 (SELECT COUNT(*) ...) UNION (...) 这类无效 SQL。
func (b *Builder) ToCount() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" && b.fromSub == nil && len(b.unions) == 0 {
		return "", nil, ErrEmptyTable
	}

	// UNION 查询：将整个 UNION 作为子查询包裹后计数
	if len(b.unions) > 0 {
		// 保存并清除分页/排序/锁，这些对计数无意义且可能干扰子查询
		origLimit, origOffset := b.limit, b.offset
		origOrders := b.orders
		origLock := b.lockClause
		b.limit, b.offset = 0, 0
		b.orders = nil
		b.lockClause = ""

		unionSQL := b.grammar.CompileSelect(b, b.columns)
		args := b.collectSelectBindings()

		b.limit, b.offset = origLimit, origOffset
		b.orders = origOrders
		b.lockClause = origLock

		countSQL := "SELECT COUNT(*) FROM (" + unionSQL + ") AS " + b.grammar.WrapTable("t")
		return countSQL, args, nil
	}

	// GROUP BY 查询：COUNT(*) 与分组列组合时返回每组一行，
	// 执行端只取第一行会得到错误结果，因此将查询包裹为子查询再计数，
	// 返回分组数量。
	if len(b.groups) > 0 {
		// 保存并清除分页/排序/锁，这些对计数无意义且可能干扰子查询
		origLimit, origOffset := b.limit, b.offset
		origOrders := b.orders
		origLock := b.lockClause
		b.limit, b.offset = 0, 0
		b.orders = nil
		b.lockClause = ""

		// 将列替换为常量，避免 SELECT * 与 GROUP BY 在严格模式下冲突
		origColumns := b.columns
		origSelectSubs := b.selectSubs
		b.columns = []SelectColumn{{Value: "1", Raw: true}}
		b.selectSubs = nil

		subSQL := b.grammar.CompileSelect(b, b.columns)
		args := b.collectSelectBindings()

		b.limit, b.offset = origLimit, origOffset
		b.orders = origOrders
		b.lockClause = origLock
		b.columns = origColumns
		b.selectSubs = origSelectSubs

		countSQL := "SELECT COUNT(*) FROM (" + subSQL + ") AS " + b.grammar.WrapTable("t")
		return countSQL, args, nil
	}

	// 保存原始列并设置为 COUNT(*)，清除 SELECT 子查询
	origColumns := b.columns
	origSelectSubs := b.selectSubs
	// 使用 defer 确保 panic 时也能恢复状态
	defer func() {
		b.columns = origColumns
		b.selectSubs = origSelectSubs
	}()
	b.columns = []SelectColumn{{Value: "COUNT(*)", Raw: true}}
	b.selectSubs = nil

	sqlStr := b.grammar.CompileSelect(b, b.columns)
	// collectSelectBindings 会收集 SELECT_SUB → FROM_SUB → JOIN → WHERE → HAVING → UNION 的绑定参数
	// 由于 selectSubs 已清空，不会包含 SELECT 子查询的参数
	args := b.collectSelectBindings()

	return sqlStr, args, nil
}

// ==================== 内部方法：收集绑定参数 ====================

// collectSelectBindings 收集 SELECT 查询的所有绑定参数。
// 顺序与 CompileSelect 生成占位符的顺序一致：
// SELECT_SUB → FROM_SUB → JOIN → WHERE → HAVING → UNION
func (b *Builder) collectSelectBindings() []any {
	var args []any
	// SELECT sub 的绑定参数（编译时先于 FROM 出现）
	for _, ss := range b.selectSubs {
		args = append(args, ss.Query.collectSelectBindings()...)
	}
	// FROM sub 的绑定参数
	if b.fromSub != nil {
		args = append(args, b.fromSub.collectSelectBindings()...)
	}
	// JOIN 绑定参数
	for _, j := range b.joins {
		for _, c := range j.Conditions {
			switch c.Type {
			case "value":
				args = append(args, c.Value)
			case "raw":
				args = append(args, c.Bindings...)
			}
		}
	}
	// WHERE
	args = append(args, b.collectWhereBindings()...)
	// HAVING
	for _, h := range b.havings {
		switch h.Type {
		case "basic":
			args = append(args, h.Value)
		case "raw":
			args = append(args, h.Bindings...)
		case "between":
			args = append(args, h.Min, h.Max)
		}
	}
	// UNION
	for _, u := range b.unions {
		args = append(args, u.Query.collectSelectBindings()...)
	}
	return args
}

// collectJoinBindings 收集 JOIN ON 条件中的绑定参数（value 与 raw 类型）。
func (b *Builder) collectJoinBindings() []any {
	var args []any
	for _, j := range b.joins {
		for _, c := range j.Conditions {
			switch c.Type {
			case "value":
				args = append(args, c.Value)
			case "raw":
				args = append(args, c.Bindings...)
			}
		}
	}
	return args
}

// collectWhereBindings 收集 WHERE 子句的所有绑定参数。
func (b *Builder) collectWhereBindings() []any {
	var args []any
	for _, w := range b.wheres {
		switch w.Type {
		case WhereTypeBasic:
			if _, ok := w.Value.(Expression); !ok {
				args = append(args, w.Value)
			}
		case WhereTypeIn:
			args = append(args, w.Values...)
		case WhereTypeNotIn:
			args = append(args, w.Values...)
		case WhereTypeBetween, WhereTypeNotBetween:
			args = append(args, w.Min, w.Max)
		case WhereTypeRaw:
			args = append(args, w.Bindings...)
		case WhereTypeNested:
			if w.Nested != nil {
				args = append(args, w.Nested.collectWhereBindings()...)
			}
		case WhereTypeExists, WhereTypeNotExists:
			if w.Nested != nil {
				args = append(args, w.Nested.collectSelectBindings()...)
			}
		case WhereTypeSub:
			if w.Sub != nil {
				args = append(args, w.Sub.collectSelectBindings()...)
			}
		case WhereTypeInSub:
			if w.Sub != nil {
				args = append(args, w.Sub.collectSelectBindings()...)
			}
		case WhereTypeNotInSub:
			if w.Sub != nil {
				args = append(args, w.Sub.collectSelectBindings()...)
			}
		case WhereTypeLike, WhereTypeNotLike:
			// Expression 已直接内嵌到 SQL，不作为绑定参数
			if _, ok := w.Value.(Expression); !ok {
				args = append(args, w.Value)
			}
		case WhereTypeNull, WhereTypeNotNull, WhereTypeColumn:
			// 无绑定参数
		default:
			panic("unhandled default case")
		}
	}
	return args
}

// ==================== 辅助方法 ====================

// Clone 克隆当前 Builder，返回一个独立副本。
func (b *Builder) Clone() *Builder {
	clone := &Builder{
		grammar:    b.grammar,
		dao:        b.dao,
		table:      b.table,
		distinct:   b.distinct,
		limit:      b.limit,
		offset:     b.offset,
		lockClause: b.lockClause,
		fromAlias:  b.fromAlias,
		err:        b.err,
	}
	// FROM 子查询深拷贝
	if b.fromSub != nil {
		clone.fromSub = b.fromSub.Clone()
	}
	if b.columns != nil {
		clone.columns = make([]SelectColumn, len(b.columns))
		copy(clone.columns, b.columns)
	}
	if b.selectSubs != nil {
		clone.selectSubs = make([]SelectSub, len(b.selectSubs))
		copy(clone.selectSubs, b.selectSubs)
		for i := range clone.selectSubs {
			if clone.selectSubs[i].Query != nil {
				clone.selectSubs[i].Query = clone.selectSubs[i].Query.Clone()
			}
		}
	}
	if b.joins != nil {
		clone.joins = make([]JoinClause, len(b.joins))
		copy(clone.joins, b.joins)
		for i := range clone.joins {
			if clone.joins[i].Conditions != nil {
				conds := make([]JoinCondition, len(clone.joins[i].Conditions))
				copy(conds, clone.joins[i].Conditions)
				for j := range conds {
					if conds[j].Bindings != nil {
						cp := make([]any, len(conds[j].Bindings))
						copy(cp, conds[j].Bindings)
						conds[j].Bindings = cp
					}
				}
				clone.joins[i].Conditions = conds
			}
		}
	}
	if b.wheres != nil {
		clone.wheres = make([]WhereClause, len(b.wheres))
		copy(clone.wheres, b.wheres)
		for i := range clone.wheres {
			if clone.wheres[i].Values != nil {
				cp := make([]any, len(clone.wheres[i].Values))
				copy(cp, clone.wheres[i].Values)
				clone.wheres[i].Values = cp
			}
			if clone.wheres[i].Bindings != nil {
				cp := make([]any, len(clone.wheres[i].Bindings))
				copy(cp, clone.wheres[i].Bindings)
				clone.wheres[i].Bindings = cp
			}
			if clone.wheres[i].Nested != nil {
				clone.wheres[i].Nested = clone.wheres[i].Nested.Clone()
			}
			if clone.wheres[i].Sub != nil {
				clone.wheres[i].Sub = clone.wheres[i].Sub.Clone()
			}
		}
	}
	if b.groups != nil {
		clone.groups = make([]string, len(b.groups))
		copy(clone.groups, b.groups)
	}
	if b.havings != nil {
		clone.havings = make([]HavingClause, len(b.havings))
		copy(clone.havings, b.havings)
	}
	if b.orders != nil {
		clone.orders = make([]OrderClause, len(b.orders))
		copy(clone.orders, b.orders)
	}
	if b.unions != nil {
		clone.unions = make([]UnionClause, len(b.unions))
		copy(clone.unions, b.unions)
		for i := range clone.unions {
			if clone.unions[i].Query != nil {
				clone.unions[i].Query = clone.unions[i].Query.Clone()
			}
		}
	}
	return clone
}
