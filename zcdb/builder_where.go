package zcdb

import "time"

// 本文件包含 Builder 的 WHERE 查询条件构造方法（Where/OrWhere 全系列）：
// 基本比较、IN/NULL/BETWEEN、LIKE/空安全、嵌套逻辑组（Not/All/Any/None）、子查询条件。

// ==================== WHERE 条件 ====================

// Where 添加一个 AND WHERE 条件。
// 支持两种形式：
//   - 三参：Where("age", ">", 25)，op 为运算符（须在白名单内，否则累积 ErrInvalidOperator）
//   - 两参简写：Where("age", 25)，缺省 = 运算符
//
// value 支持基本类型（int、string、float、bool、time.Time 等）和 Expression（直接嵌入 SQL，不作为绑定参数）。
// value 为 nil 时自动转换：= 转 IS NULL，!=/<> 转 IS NOT NULL（防止生成永假的 = NULL）。
//
//	sql, args, _ := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `age` > ?
//	// args: [25]
//
//	sql, args, _ = db.Builder().Table("users").Where("status", "active").ToSelect() // 两参简写
//	// SQL:  SELECT * FROM `users` WHERE `status` = ?
//	// args: [active]
//
//	sql, _, _ = db.Builder().Table("users").Where("deleted_at", "=", nil).ToSelect() // nil 特判
//	// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL
//
//	sql, _, _ = db.Builder().Table("users").
//	    Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect() // Expression 内嵌
//	// SQL: SELECT * FROM `users` WHERE `id` = parent_id
func (b *Builder) Where(column string, op any, value ...any) *Builder {
	return b.addWhereBasic(column, op, value, "AND")
}

// OrWhere 添加一个 OR WHERE 条件。
// 形式与 nil 特判规则同 Where。注意 OR 不加括号时会改变多条件组合的优先级，
// 需要分组时请用 OrWhereNested/OrWhereAny 等括号方法。
//
//	sql, args, _ := db.Builder().Table("users").
//	    Where("age", ">", 25).OrWhere("vip", "=", 1).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `age` > ? OR `vip` = ?
//	// args: [25 1]
func (b *Builder) OrWhere(column string, op any, value ...any) *Builder {
	return b.addWhereBasic(column, op, value, "OR")
}

// addWhereBasic 解析变参并追加 basic 条件：
// value 非空时 op 必须是字符串运算符（三参形式）；否则 op 即值，缺省 = 运算符（两参简写）。
func (b *Builder) addWhereBasic(column string, op any, value []any, boolean string) *Builder {
	var operator string
	var val any
	if len(value) > 0 {
		s, ok := op.(string)
		if !ok {
			b.err = ErrInvalidOperator
			return b
		}
		operator = s
		val = value[0]
	} else {
		operator = "="
		val = op
	}
	if err := validateOperator(operator); err != nil {
		b.err = err
		return b
	}
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeBasic,
		Column:   column,
		Operator: operator,
		Value:    val,
		Boolean:  boolean,
	})
	return b
}

// WhereDate 添加一个 WHERE date(column) = value 条件，内部经 grammar.CompileWhereDate
// 按方言生成日期提取表达式，不同方言语法：
//   - MySQL:      date(`column`) = ?
//   - PostgreSQL: "column"::date = $1
//   - SQLite:     strftime('%Y-%m-%d', "column") = ?
//
// value 建议传 "YYYY-MM-DD" 格式字符串（或 time.Time）。
//
//	sql, args, _ := db.Builder().Table("users").WhereDate("created_at", "2026-08-08").ToSelect()
//	// MySQL SQL: SELECT * FROM `users` WHERE date(`created_at`) = ?
//	// args:      [2026-08-08]
func (b *Builder) WhereDate(column string, value any) *Builder {
	if t, ok := value.(time.Time); ok {
		value = t.Format("2006-01-02")
	}
	return b.WhereRaw(b.grammar.CompileWhereDate(column)+" = ?", value)
}

// WhereIn 添加一个 WHERE column IN (...) 条件。
// values 元素支持基本类型（int、string 等），每个元素占一个 ? 占位符。
//
//	sql, args, _ := db.Builder().Table("users").WhereIn("id", []any{1, 2, 3}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `id` IN (?, ?, ?)
//	// args: [1 2 3]
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
//
//	sql, args, _ := db.Builder().Table("users").WhereNotIn("status", []any{"banned", "frozen"}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` NOT IN (?, ?)
//	// args: [banned frozen]
func (b *Builder) WhereNotIn(column string, values []any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotIn,
		Column:  column,
		Values:  values,
		Boolean: "AND",
	})
	return b
}

// WhereNull 添加 WHERE column IS NULL 条件，支持多列（展开为多个 AND 条件），无绑定参数。
//
//	sql, _, _ := db.Builder().Table("users").WhereNull("deleted_at", "remark").ToSelect()
//	// SQL: SELECT * FROM `users` WHERE `deleted_at` IS NULL AND `remark` IS NULL
func (b *Builder) WhereNull(columns ...string) *Builder {
	for _, column := range columns {
		b.wheres = append(b.wheres, WhereClause{
			Type:    WhereTypeNull,
			Column:  column,
			Boolean: "AND",
		})
	}
	return b
}

// WhereNotNull 添加 WHERE column IS NOT NULL 条件，支持多列（展开为多个 AND 条件），无绑定参数。
//
//	sql, _, _ := db.Builder().Table("users").WhereNotNull("email").ToSelect()
//	// SQL: SELECT * FROM `users` WHERE `email` IS NOT NULL
func (b *Builder) WhereNotNull(columns ...string) *Builder {
	for _, column := range columns {
		b.wheres = append(b.wheres, WhereClause{
			Type:    WhereTypeNotNull,
			Column:  column,
			Boolean: "AND",
		})
	}
	return b
}

// WhereBetween 添加一个 WHERE column BETWEEN min AND max 条件（闭区间）。
// min、max 支持基本类型（int、string、time.Time 等），绑定顺序为先 min 后 max。
//
//	sql, args, _ := db.Builder().Table("users").WhereBetween("age", 18, 30).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `age` BETWEEN ? AND ?
//	// args: [18 30]
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
//
//	sql, args, _ := db.Builder().Table("users").WhereNotBetween("age", 18, 30).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `age` NOT BETWEEN ? AND ?
//	// args: [18 30]
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

// OrWhereBetween 添加一个 OR WHERE column BETWEEN min AND max 条件。
//
//	sql, args, _ := db.Builder().Table("users").
//	    Where("vip", "=", 1).OrWhereBetween("age", 18, 30).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` BETWEEN ? AND ?
//	// args: [1 18 30]
func (b *Builder) OrWhereBetween(column string, min, max any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeBetween,
		Column:  column,
		Min:     min,
		Max:     max,
		Boolean: "OR",
	})
	return b
}

// OrWhereNotBetween 添加一个 OR WHERE column NOT BETWEEN min AND max 条件。
//
//	sql, args, _ := db.Builder().Table("users").
//	    Where("vip", "=", 1).OrWhereNotBetween("age", 18, 30).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` NOT BETWEEN ? AND ?
//	// args: [1 18 30]
func (b *Builder) OrWhereNotBetween(column string, min, max any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotBetween,
		Column:  column,
		Min:     min,
		Max:     max,
		Boolean: "OR",
	})
	return b
}

// WhereBetweenColumns 添加一个两列区间比较条件（无绑定）：column BETWEEN min AND max，
// min/max 为列名（经 WrapColumn 引用），内部经 WhereRaw 实现。
//
//	sql, _, _ := db.Builder().Table("products").
//	    WhereBetweenColumns("price", "min_price", "max_price").ToSelect()
//	// SQL: SELECT * FROM `products` WHERE `price` BETWEEN `min_price` AND `max_price`
func (b *Builder) WhereBetweenColumns(column, min, max string) *Builder {
	g := b.grammar
	return b.WhereRaw(g.WrapColumn(column) + " BETWEEN " + g.WrapColumn(min) + " AND " + g.WrapColumn(max))
}

// WhereNotBetweenColumns 添加一个两列区间取反条件（无绑定）：column NOT BETWEEN min AND max。
//
//	sql, _, _ := db.Builder().Table("products").
//	    WhereNotBetweenColumns("price", "min_price", "max_price").ToSelect()
//	// SQL: SELECT * FROM `products` WHERE `price` NOT BETWEEN `min_price` AND `max_price`
func (b *Builder) WhereNotBetweenColumns(column, min, max string) *Builder {
	g := b.grammar
	return b.WhereRaw(g.WrapColumn(column) + " NOT BETWEEN " + g.WrapColumn(min) + " AND " + g.WrapColumn(max))
}

// OrWhereBetweenColumns 添加一个 OR 两列区间比较条件（无绑定）。
//
//	sql, args, _ := db.Builder().Table("products").
//	    Where("vip", "=", 1).OrWhereBetweenColumns("price", "min_price", "max_price").ToSelect()
//	// SQL:  SELECT * FROM `products` WHERE `vip` = ? OR `price` BETWEEN `min_price` AND `max_price`
//	// args: [1]
func (b *Builder) OrWhereBetweenColumns(column, min, max string) *Builder {
	g := b.grammar
	return b.OrWhereRaw(g.WrapColumn(column) + " BETWEEN " + g.WrapColumn(min) + " AND " + g.WrapColumn(max))
}

// OrWhereNotBetweenColumns 添加一个 OR 两列区间取反条件（无绑定）。
//
//	sql, args, _ := db.Builder().Table("products").
//	    Where("vip", "=", 1).OrWhereNotBetweenColumns("price", "min_price", "max_price").ToSelect()
//	// SQL:  SELECT * FROM `products` WHERE `vip` = ? OR `price` NOT BETWEEN `min_price` AND `max_price`
//	// args: [1]
func (b *Builder) OrWhereNotBetweenColumns(column, min, max string) *Builder {
	g := b.grammar
	return b.OrWhereRaw(g.WrapColumn(column) + " NOT BETWEEN " + g.WrapColumn(min) + " AND " + g.WrapColumn(max))
}

// WhereValueBetween 添加一个值在左的区间比较条件：? BETWEEN min AND max，
// value 为绑定参数，min/max 为列名。适用于“给定值是否落在表中区间内”的场景。
//
//	sql, args, _ := db.Builder().Table("users").WhereValueBetween(25, "min_age", "max_age").ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE ? BETWEEN `min_age` AND `max_age`
//	// args: [25]
func (b *Builder) WhereValueBetween(value any, min, max string) *Builder {
	g := b.grammar
	return b.WhereRaw("? BETWEEN "+g.WrapColumn(min)+" AND "+g.WrapColumn(max), value)
}

// OrWhereValueBetween 添加一个 OR 值在左的区间比较条件。
//
//	sql, args, _ := db.Builder().Table("users").
//	    Where("vip", "=", 1).OrWhereValueBetween(25, "min_age", "max_age").ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR ? BETWEEN `min_age` AND `max_age`
//	// args: [1 25]
func (b *Builder) OrWhereValueBetween(value any, min, max string) *Builder {
	g := b.grammar
	return b.OrWhereRaw("? BETWEEN "+g.WrapColumn(min)+" AND "+g.WrapColumn(max), value)
}

// WhereRaw 添加一个原始 SQL WHERE 条件，SQL 直接嵌入不做包裹。
// bindings 为 SQL 中 ? 占位符的绑定值，支持基本类型和 Expression（直接嵌入 SQL）。
//
//	sql, args, _ := db.Builder().Table("users").
//	    WhereRaw("created_at > NOW() - INTERVAL ? DAY", 7).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE created_at > NOW() - INTERVAL ? DAY
//	// args: [7]
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
//
//	sql, args, _ := db.Builder().Table("users").
//	    Where("status", "active").OrWhereRaw("score > ?", 90).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR score > ?
//	// args: [active 90]
func (b *Builder) OrWhereRaw(sql string, bindings ...any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:     WhereTypeRaw,
		SQL:      sql,
		Bindings: bindings,
		Boolean:  "OR",
	})
	return b
}

// WhereColumn 添加一个两列比较的 WHERE 条件，两侧均为列引用（经 WrapColumn），
// 无绑定参数。op 经白名单校验，非法时累积 ErrInvalidOperator。
//
//	sql, _, _ := db.Builder().Table("users").WhereColumn("updated_at", ">", "created_at").ToSelect()
//	// SQL: SELECT * FROM `users` WHERE `updated_at` > `created_at`
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

// WhereNested 添加一个括号分组的嵌套 WHERE 条件组（AND 连接）。
// callback 接收一个新的 Builder（继承当前表名），回调内未添加任何条件时该组被忽略。
//
//	sql, args, _ := db.Builder().Table("users").Where("status", "active").
//	    WhereNested(func(q *zcdb.Builder) {
//	        q.Where("age", ">", 18).Where("vip", "=", 1)
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` = ? AND (`age` > ? AND `vip` = ?)
//	// args: [active 18 1]
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

// OrWhereNested 添加一个 OR 连接的括号分组条件组，回调内条件仍为 AND 连接。
//
//	sql, args, _ := db.Builder().Table("users").Where("status", "active").
//	    OrWhereNested(func(q *zcdb.Builder) {
//	        q.Where("age", ">", 60).Where("vip", "=", 1)
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR (`age` > ? AND `vip` = ?)
//	// args: [active 60 1]
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

// WhereExists 添加一个 WHERE EXISTS (子查询) 条件。
// sub 支持两种形式：func(*Builder) 回调（在回调内构造子查询）或 *Builder（直接传已构造的子查询）；
// 其它类型会累积 ErrInvalidSubQuery。
//
//	sql, _, _ := db.Builder().Table("users").WhereExists(func(q *zcdb.Builder) {
//	    q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` WHERE EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
func (b *Builder) WhereExists(sub any) *Builder {
	return b.addWhereExists(sub, WhereTypeExists, "AND")
}

// OrWhereExists 添加一个 OR WHERE EXISTS (子查询) 条件。sub 形式同 WhereExists。
//
//	sql, args, _ := db.Builder().Table("users").Where("status", "active").
//	    OrWhereExists(func(q *zcdb.Builder) {
//	        q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
//	// args: [active]
func (b *Builder) OrWhereExists(sub any) *Builder {
	return b.addWhereExists(sub, WhereTypeExists, "OR")
}

// WhereNotExists 添加一个 WHERE NOT EXISTS (子查询) 条件。sub 形式同 WhereExists。
//
//	sql, _, _ := db.Builder().Table("users").WhereNotExists(func(q *zcdb.Builder) {
//	    q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` WHERE NOT EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
func (b *Builder) WhereNotExists(sub any) *Builder {
	return b.addWhereExists(sub, WhereTypeNotExists, "AND")
}

// OrWhereNotExists 添加一个 OR WHERE NOT EXISTS (子查询) 条件。sub 形式同 WhereExists。
//
//	sql, args, _ := db.Builder().Table("users").Where("status", "active").
//	    OrWhereNotExists(func(q *zcdb.Builder) {
//	        q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR NOT EXISTS (SELECT 1 FROM `orders` WHERE `orders`.`user_id` = `users`.`id`)
//	// args: [active]
func (b *Builder) OrWhereNotExists(sub any) *Builder {
	return b.addWhereExists(sub, WhereTypeNotExists, "OR")
}

// addWhereExists 解析 sub 参数（回调或已构造的 *Builder）并追加 EXISTS 条件。
func (b *Builder) addWhereExists(sub any, wt WhereType, boolean string) *Builder {
	var subBuilder *Builder
	switch s := sub.(type) {
	case func(*Builder):
		subBuilder = NewBuilder(b.grammar, b.dao)
		s(subBuilder)
	case *Builder:
		subBuilder = s
	default:
		b.err = ErrInvalidSubQuery
		return b
	}
	b.wheres = append(b.wheres, WhereClause{
		Type:    wt,
		Nested:  subBuilder,
		Boolean: boolean,
	})
	return b
}

// WhereSub 添加一个子查询比较条件：WHERE column op (SELECT ...)。
// op 经白名单校验；callback 内构造子查询，子查询绑定参数按位置计入总绑定。
//
//	sql, _, _ := db.Builder().Table("users").WhereSub("age", ">", func(q *zcdb.Builder) {
//	    q.Table("stats").SelectRaw("AVG(age)")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` WHERE `age` > (SELECT AVG(age) FROM `stats`)
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

// OrWhereSub 添加一个 OR 子查询比较条件：OR column op (SELECT ...)。
//
//	sql, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
//	    OrWhereSub("age", "<", func(q *zcdb.Builder) {
//	        q.Table("stats").SelectRaw("MIN(age)")
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `age` < (SELECT MIN(age) FROM `stats`)
//	// args: [1]
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
//
//	sql, args, _ := db.Builder().Table("users").WhereInSub("dept_id", func(q *zcdb.Builder) {
//	    q.Table("depts").Select("id").Where("level", ">", 3)
//	}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `dept_id` IN (SELECT `id` FROM `depts` WHERE `level` > ?)
//	// args: [3]
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
//
//	sql, args, _ := db.Builder().Table("users").WhereNotInSub("dept_id", func(q *zcdb.Builder) {
//	    q.Table("depts").Select("id").Where("level", "<=", 1)
//	}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `dept_id` NOT IN (SELECT `id` FROM `depts` WHERE `level` <= ?)
//	// args: [1]
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
// value 通常为 string 类型（如 "%keyword%"），也支持 Expression（直接嵌入 SQL）。
// caseSensitive 可选：为 true 时区分大小写，按方言生成不同 SQL：
//
//   - MySQL:      BINARY column LIKE ?（实测有效）
//
//   - PostgreSQL: column LIKE ?（默认不区分时为 ILIKE）
//
//   - SQLite:     column GLOB ?（通配符为 * / ?，非 % / _）
//
// 示例：
//
//	sql, args, _ := db.Builder().Table("users").WhereLike("name", "%alice%").ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `name` LIKE ?
//	// args: [%alice%]
//
//	sql, args, _ = db.Builder().Table("users").WhereLike("name", "a%", true).ToSelect()
//	// MySQL SQL: SELECT * FROM `users` WHERE BINARY `name` LIKE ?
func (b *Builder) WhereLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.addWhereLike(column, value, "AND", caseSensitive)
}

// OrWhereLike 添加一个 OR WHERE column LIKE value 条件。
// caseSensitive 规则同 WhereLike。
//
//	sql, args, _ := db.Builder().Table("users").
//	    Where("vip", "=", 1).OrWhereLike("name", "%bob%").ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR `name` LIKE ?
//	// args: [1 %bob%]
func (b *Builder) OrWhereLike(column string, value any, caseSensitive ...bool) *Builder {
	return b.addWhereLike(column, value, "OR", caseSensitive)
}

// addWhereLike 追加 Like 条件，caseSensitive 变参取首个元素。
func (b *Builder) addWhereLike(column string, value any, boolean string, caseSensitive []bool) *Builder {
	cs := false
	if len(caseSensitive) > 0 {
		cs = caseSensitive[0]
	}
	b.wheres = append(b.wheres, WhereClause{
		Type:          WhereTypeLike,
		Column:        column,
		Value:         value,
		Boolean:       boolean,
		CaseSensitive: cs,
	})
	return b
}

// WhereNotLike 添加一个 WHERE column NOT LIKE value 条件。
// value 类型规则同 WhereLike（不支持 caseSensitive 变参）。
// 大小写语义与 WhereLike 默认行为对称：
//
//   - MySQL/SQLite: NOT LIKE（与 LIKE 一样默认不区分大小写，取决于排序规则/ASCII）
//   - PostgreSQL:   NOT ILIKE（与 WhereLike 默认 ILIKE 对称，保证二者互补）
//
// PostgreSQL 下需区分大小写时可用 WhereRaw 自行表达（Expression 同样编译为 NOT ILIKE）。
//
//	sql, args, _ := db.Builder().Table("users").WhereNotLike("name", "%test%").ToSelect()
//	// MySQL SQL:      SELECT * FROM `users` WHERE `name` NOT LIKE ?
//	// PostgreSQL SQL: SELECT * FROM "users" WHERE "name" NOT ILIKE $1
//	// args: [%test%]
func (b *Builder) WhereNotLike(column string, value any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNotLike,
		Column:  column,
		Value:   value,
		Boolean: "AND",
	})
	return b
}

// WhereNullSafeEquals 添加空安全相等比较条件（NULL 参与运算，NULL = NULL 为 true），
// 适用于查找“列值等于某值或两者均为 NULL”的场景。不同方言语法：
//
//   - MySQL:      column <=> ?
//
//   - PostgreSQL: column IS NOT DISTINCT FROM ?
//
//   - SQLite:     column IS ?
//
// 示例：
//
//	sql, args, _ := db.Builder().Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
//	// MySQL SQL: SELECT * FROM `users` WHERE `remark` <=> ?
//	// args:      [<nil>]
func (b *Builder) WhereNullSafeEquals(column string, value any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNullSafe,
		Column:  column,
		Value:   value,
		Boolean: "AND",
	})
	return b
}

// WhereNullSafeNotEquals 添加空安全不等比较条件（NULL 与任意值比较为不等）。
// 不同方言语法：
//
//   - MySQL:      NOT column <=> ?
//
//   - PostgreSQL: column IS DISTINCT FROM ?
//
//   - SQLite:     column IS NOT ?
//
// 示例：
//
//	sql, args, _ := db.Builder().Table("users").WhereNullSafeNotEquals("remark", "x").ToSelect()
//	// MySQL SQL: SELECT * FROM `users` WHERE NOT `remark` <=> ?
//	// args:      [x]
func (b *Builder) WhereNullSafeNotEquals(column string, value any) *Builder {
	b.wheres = append(b.wheres, WhereClause{
		Type:    WhereTypeNullSafeNot,
		Column:  column,
		Value:   value,
		Boolean: "AND",
	})
	return b
}

// WhereNot 添加一个整体取反的嵌套条件组：NOT (...)。
// callback 内的条件经括号分组后整体取反，内部默认 AND 连接。
//
//	sql, args, _ := db.Builder().Table("users").WhereNot(func(q *zcdb.Builder) {
//	    q.Where("status", "banned").Where("age", "<", 18)
//	}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? AND `age` < ?)
//	// args: [banned 18]
func (b *Builder) WhereNot(callback func(*Builder)) *Builder {
	return b.addWhereNot(callback, "AND")
}

// OrWhereNot 添加一个 OR NOT (...) 嵌套条件组。
//
//	sql, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
//	    OrWhereNot(func(q *zcdb.Builder) {
//	        q.Where("status", "banned")
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR NOT (`status` = ?)
//	// args: [1 banned]
func (b *Builder) OrWhereNot(callback func(*Builder)) *Builder {
	return b.addWhereNot(callback, "OR")
}

// addWhereNot 复用 WhereNested 括号机制，仅加 NOT 前缀。
func (b *Builder) addWhereNot(callback func(*Builder), boolean string) *Builder {
	nested := NewBuilder(b.grammar, b.dao)
	nested.table = b.table
	callback(nested)
	if len(nested.wheres) > 0 {
		b.wheres = append(b.wheres, WhereClause{
			Type:    WhereTypeNested,
			Nested:  nested,
			Boolean: boolean,
			Not:     true,
		})
	}
	return b
}

// WhereAll 添加一个全部满足的嵌套条件组：(a AND b)，等价于 WhereNested，
// 命名上强调“全部满足”的语义。
//
//	sql, args, _ := db.Builder().Table("users").WhereAll(func(q *zcdb.Builder) {
//	    q.Where("age", ">", 18).Where("status", "active")
//	}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE (`age` > ? AND `status` = ?)
//	// args: [18 active]
func (b *Builder) WhereAll(callback func(*Builder)) *Builder {
	return b.WhereNested(callback)
}

// OrWhereAll 添加一个 OR (a AND b) 嵌套条件组。
//
//	sql, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
//	    OrWhereAll(func(q *zcdb.Builder) {
//	        q.Where("age", ">", 60).Where("status", "retired")
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR (`age` > ? AND `status` = ?)
//	// args: [1 60 retired]
func (b *Builder) OrWhereAll(callback func(*Builder)) *Builder {
	return b.OrWhereNested(callback)
}

// WhereAny 添加一个任一满足的嵌套条件组：(a OR b)。
// 实现上将回调内条件的顶层 Boolean 统一覆写为 OR（嵌套内部的布尔关系不受影响）。
//
//	sql, args, _ := db.Builder().Table("users").WhereAny(func(q *zcdb.Builder) {
//	    q.Where("age", ">", 60).Where("vip", "=", 1)
//	}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE (`age` > ? OR `vip` = ?)
//	// args: [60 1]
func (b *Builder) WhereAny(callback func(*Builder)) *Builder {
	return b.addWhereAny(callback, "AND", false)
}

// OrWhereAny 添加一个 OR (a OR b) 嵌套条件组。
//
//	sql, args, _ := db.Builder().Table("users").Where("status", "active").
//	    OrWhereAny(func(q *zcdb.Builder) {
//	        q.Where("age", ">", 60).Where("vip", "=", 1)
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `status` = ? OR (`age` > ? OR `vip` = ?)
//	// args: [active 60 1]
func (b *Builder) OrWhereAny(callback func(*Builder)) *Builder {
	return b.addWhereAny(callback, "OR", false)
}

// WhereNone 添加一个全部不满足的嵌套条件组：NOT (a OR b)。
//
//	sql, args, _ := db.Builder().Table("users").WhereNone(func(q *zcdb.Builder) {
//	    q.Where("status", "banned").Where("age", "<", 18)
//	}).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE NOT (`status` = ? OR `age` < ?)
//	// args: [banned 18]
func (b *Builder) WhereNone(callback func(*Builder)) *Builder {
	return b.addWhereAny(callback, "AND", true)
}

// OrWhereNone 添加一个 OR NOT (a OR b) 嵌套条件组。
//
//	sql, args, _ := db.Builder().Table("users").Where("vip", "=", 1).
//	    OrWhereNone(func(q *zcdb.Builder) {
//	        q.Where("status", "banned").Where("age", "<", 18)
//	    }).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `vip` = ? OR NOT (`status` = ? OR `age` < ?)
//	// args: [1 banned 18]
func (b *Builder) OrWhereNone(callback func(*Builder)) *Builder {
	return b.addWhereAny(callback, "OR", true)
}

// addWhereAny 构造 OR 连接的嵌套条件组；not 为 true 时整体取反（WhereNone 语义）。
// 回调内的条件默认 AND 连接，此处统一覆写顶层 Boolean 为 OR（嵌套内部的布尔关系不受影响）。
func (b *Builder) addWhereAny(callback func(*Builder), boolean string, not bool) *Builder {
	nested := NewBuilder(b.grammar, b.dao)
	nested.table = b.table
	callback(nested)
	if len(nested.wheres) > 0 {
		for i := range nested.wheres {
			nested.wheres[i].Boolean = "OR"
		}
		b.wheres = append(b.wheres, WhereClause{
			Type:    WhereTypeNested,
			Nested:  nested,
			Boolean: boolean,
			Not:     not,
		})
	}
	return b
}
