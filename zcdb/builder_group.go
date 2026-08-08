package zcdb

// 本文件包含 Builder 的分组与 HAVING 构造方法：GroupBy/GroupByRaw 与 Having 系列。

// ==================== GROUP BY / HAVING ====================

// GroupBy 设置 GROUP BY 子句，支持多列（追加式累积，多次调用按顺序拼接）。
// 列名会被方言包裹（MySQL 反引号/PG、SQLite 双引号），需要表达式分组请用 GroupByRaw。
//
//	sql, _, _ := db.Builder().Table("orders").Select("user_id").SelectRaw("SUM(amount) AS total").GroupBy("user_id").ToSelect()
//	// SQL: SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id`
//
//	sql, _, _ = db.Builder().Table("orders").GroupBy("user_id", "status").ToSelect()
//	// SQL: SELECT * FROM `orders` GROUP BY `user_id`, `status`
func (b *Builder) GroupBy(columns ...string) *Builder {
	for _, column := range columns {
		b.groups = append(b.groups, GroupClause{Column: column})
	}
	return b
}

// GroupByRaw 添加一个原始 SQL 的 GROUP BY 子句（与 HavingRaw 对称），不做标识符包裹。
// bindings 为 SQL 中 ? 占位符的绑定值；绑定顺序规则：where → groupBy → having。
//
//	sql, _, _ := db.Builder().Table("orders").SelectRaw("DATE(created_at) AS d, COUNT(*) AS cnt").GroupByRaw("DATE(created_at)").ToSelect()
//	// SQL: SELECT DATE(created_at) AS d, COUNT(*) AS cnt FROM `orders` GROUP BY DATE(created_at)
func (b *Builder) GroupByRaw(sql string, bindings ...any) *Builder {
	b.groups = append(b.groups, GroupClause{Raw: sql, Bindings: bindings})
	return b
}

// Having 添加一个 HAVING 条件。
// 支持两种形式：
//   - 三参：Having("cnt", ">", 5)，op 为运算符
//   - 两参简写：Having("cnt", 5)，缺省 = 运算符
//
// value 支持基本类型（int、string、float 等）。
//
//	sql, args, _ := db.Builder().Table("orders").Select("user_id").SelectRaw("SUM(amount) AS total").
//	    GroupBy("user_id").Having("total", ">", 100).ToSelect()
//	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING `total` > ?
//	// args: [100]
//
//	sql, args, _ = db.Builder().Table("orders").Select("status").SelectRaw("COUNT(*) AS cnt").
//	    GroupBy("status").Having("cnt", 5).ToSelect()
//	// SQL:  SELECT `status`, COUNT(*) AS cnt FROM `orders` GROUP BY `status` HAVING `cnt` = ?
//	// args: [5]
func (b *Builder) Having(column string, op any, value ...any) *Builder {
	return b.addHavingBasic(column, op, value, "AND")
}

// OrHaving 添加一个 OR HAVING 条件。形式规则同 Having。
//
//	b.GroupBy("user_id").Having("total", ">", 1000).OrHaving("total", "<", 10)
//	// SQL: ... GROUP BY `user_id` HAVING `total` > ? OR `total` < ?
//	// args: [1000 10]
func (b *Builder) OrHaving(column string, op any, value ...any) *Builder {
	return b.addHavingBasic(column, op, value, "OR")
}

// addHavingBasic 解析变参并追加 basic HAVING 条件，规则同 addWhereBasic。
func (b *Builder) addHavingBasic(column string, op any, value []any, boolean string) *Builder {
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
	b.havings = append(b.havings, HavingClause{
		Type:     "basic",
		Column:   column,
		Operator: operator,
		Value:    val,
		Boolean:  boolean,
	})
	return b
}

// HavingRaw 添加一个原始 SQL HAVING 条件，不做标识符包裹。
// bindings 为 SQL 中 ? 占位符的绑定值，支持基本类型和 Expression（直接嵌入 SQL）。
//
//	sql, args, _ := db.Builder().Table("orders").Select("user_id").SelectRaw("SUM(amount) AS total").
//	    GroupBy("user_id").HavingRaw("SUM(amount) > ?", 1000).ToSelect()
//	// SQL:  SELECT `user_id`, SUM(amount) AS total FROM `orders` GROUP BY `user_id` HAVING SUM(amount) > ?
//	// args: [1000]
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
// min、max 支持基本类型（int、float 等），走占位符绑定。
//
//	b.GroupBy("user_id").HavingBetween("total", 100, 500)
//	// SQL: ... GROUP BY `user_id` HAVING `total` BETWEEN ? AND ?
//	// args: [100 500]
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
//
//	b.GroupBy("user_id").HavingNotBetween("total", 100, 500)
//	// SQL: ... GROUP BY `user_id` HAVING `total` NOT BETWEEN ? AND ?
//	// args: [100 500]
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

// HavingNull 添加 HAVING column IS NULL 条件，支持多列（展开为多个 AND 条件）。
//
//	sql, _, _ := db.Builder().Table("orders").Select("remark").SelectRaw("COUNT(*) AS cnt").
//	    GroupBy("remark").HavingNull("remark").ToSelect()
//	// SQL: SELECT `remark`, COUNT(*) AS cnt FROM `orders` GROUP BY `remark` HAVING `remark` IS NULL
func (b *Builder) HavingNull(columns ...string) *Builder {
	for _, column := range columns {
		b.havings = append(b.havings, HavingClause{
			Type:    "null",
			Column:  column,
			Boolean: "AND",
		})
	}
	return b
}

// HavingNotNull 添加 HAVING column IS NOT NULL 条件，支持多列（展开为多个 AND 条件）。
//
//	b.GroupBy("remark").HavingNotNull("remark")
//	// SQL: ... GROUP BY `remark` HAVING `remark` IS NOT NULL
func (b *Builder) HavingNotNull(columns ...string) *Builder {
	for _, column := range columns {
		b.havings = append(b.havings, HavingClause{
			Type:    "notNull",
			Column:  column,
			Boolean: "AND",
		})
	}
	return b
}

// HavingNested 添加一个括号分组的嵌套 HAVING 条件组（仿 WhereNested），
// 回调内的 Having 系列条件编译时整体加括号；回调内条件为空时不追加。
//
//	b.Select("user_id").SelectRaw("SUM(amount) AS total, COUNT(*) AS cnt").GroupBy("user_id").
//	    HavingNested(func(q *zcdb.Builder) {
//	        q.Having("total", ">", 100).Having("cnt", ">", 3)
//	    })
//	// SQL: ... GROUP BY `user_id` HAVING (`total` > ? AND `cnt` > ?)
//	// args: [100 3]
func (b *Builder) HavingNested(callback func(*Builder)) *Builder {
	return b.addHavingNested(callback, "AND")
}

// OrHavingNested 添加一个 OR 括号分组的嵌套 HAVING 条件组。回调规则同 HavingNested。
//
//	b.GroupBy("user_id").Having("total", ">", 1000).OrHavingNested(func(q *zcdb.Builder) {
//	    q.Having("total", ">", 100).Having("cnt", ">", 5)
//	})
//	// SQL: ... GROUP BY `user_id` HAVING `total` > ? OR (`total` > ? AND `cnt` > ?)
//	// args: [1000 100 5]
func (b *Builder) OrHavingNested(callback func(*Builder)) *Builder {
	return b.addHavingNested(callback, "OR")
}

// addHavingNested 构造嵌套 Builder 收集 HAVING 条件，编译时加括号并合并绑定。
func (b *Builder) addHavingNested(callback func(*Builder), boolean string) *Builder {
	nested := NewBuilder(b.grammar, b.dao)
	nested.table = b.table
	callback(nested)
	if len(nested.havings) > 0 {
		b.havings = append(b.havings, HavingClause{
			Type:    "nested",
			Nested:  nested,
			Boolean: boolean,
		})
	}
	return b
}
