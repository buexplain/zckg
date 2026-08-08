package zcdb

// 本文件包含 Builder 的连表 JOIN 构造方法：
// Join/LeftJoin/RightJoin（单条件简写）、JoinOn 系列（多条件回调）、
// CrossJoin 系列、JoinSub 系列（派生表），以及 ON 条件构造器 JoinBuilder。
// 分类依据见 docs/builder-api.md 第 4 节。

// ==================== JOIN ====================

// Join 添加一个 INNER JOIN（单条件简写），条件为列到列比较（无绑定参数）。
// 多条件或需要值比较时请用 JoinOn。
//
//	sql, _, _ := db.Builder().Table("users").Select("users.name", "orders.amount").
//	    Join("orders", "users.id", "=", "orders.user_id").ToSelect()
//	// SQL: SELECT `users`.`name`, `orders`.`amount` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id`
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

// LeftJoin 添加一个 LEFT JOIN（单条件简写），主表行在右表无匹配时仍保留。
//
//	sql, _, _ := db.Builder().Table("users").Select("users.name", "orders.amount").
//	    LeftJoin("orders", "users.id", "=", "orders.user_id").ToSelect()
//	// SQL: SELECT `users`.`name`, `orders`.`amount` FROM `users` LEFT JOIN `orders` ON `users`.`id` = `orders`.`user_id`
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

// RightJoin 添加一个 RIGHT JOIN（单条件简写），右表行在左表无匹配时仍保留。
// 注意 SQLite 不支持 RIGHT JOIN（3.39 以下）。
//
//	sql, _, _ := db.Builder().Table("users").Select("users.name", "orders.amount").
//	    RightJoin("orders", "users.id", "=", "orders.user_id").ToSelect()
//	// SQL: SELECT `users`.`name`, `orders`.`amount` FROM `users` RIGHT JOIN `orders` ON `users`.`id` = `orders`.`user_id`
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

// CrossJoin 添加一个无条件 CROSS JOIN（笛卡尔积），无 ON 条件、无绑定参数。
//
//	sql, _, _ := db.Builder().Table("stores").Select("stores.name", "months.month").
//	    CrossJoin("months").ToSelect()
//	// SQL: SELECT `stores`.`name`, `months`.`month` FROM `stores` CROSS JOIN `months`
func (b *Builder) CrossJoin(table string) *Builder {
	b.joins = append(b.joins, JoinClause{
		Type:  JoinTypeCross,
		Table: table,
	})
	return b
}

// CrossJoinOn 添加一个带 ON 列比较条件的 CROSS JOIN。
// MySQL/SQLite 直译 CROSS JOIN ... ON ...；
// PostgreSQL 的 CROSS JOIN 不接受 ON，编译层自动转为 INNER JOIN ... ON ...（语义等价）。
//
//	sql, _, _ := db.Builder().Table("users").
//	    CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
//	// MySQL SQL: SELECT * FROM `users` CROSS JOIN `colors` ON `colors`.`id` = `users`.`id`
//	// PG SQL:    SELECT * FROM "users" INNER JOIN "colors" ON "colors"."id" = "users"."id"
func (b *Builder) CrossJoinOn(table, first, op, second string) *Builder {
	b.joins = append(b.joins, JoinClause{
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
	return b
}

// CrossJoinSub 添加一个 CROSS JOIN 派生表（子查询），无 ON 条件，结果集为笛卡尔积。
// 典型场景：先 CROSS JOIN 生成维度组合矩阵（如门店 × 月份），再 LEFT JOIN 事实表补零。
// 子查询的绑定参数先于 ON/WHERE 条件计入总绑定。
//
//	months := db.Builder().Table("months").Select("month")
//	sql, _, _ := db.Builder().Table("stores").Select("stores.name", "m.month").
//	    CrossJoinSub(months, "m").ToSelect()
//	// SQL: SELECT `stores`.`name`, `m`.`month` FROM `stores` CROSS JOIN (SELECT `month` FROM `months`) AS `m`
func (b *Builder) CrossJoinSub(sub *Builder, alias string) *Builder {
	b.joins = append(b.joins, JoinClause{
		Type:  JoinTypeCross,
		Sub:   sub,
		Alias: alias,
	})
	return b
}

// JoinOn 添加一个 INNER JOIN（支持多条件），callback 内用 JoinBuilder 构造 ON 条件：
// 支持列比较（On）、值比较（Where）、NULL/IN/EXISTS、括号分组与嵌套 join 组。
// ON 条件的绑定参数按 JOIN → WHERE 顺序计入总绑定。
//
//	sql, args, _ := db.Builder().Table("users").Select("users.name").
//	    JoinOn("orders", func(j *zcdb.JoinBuilder) {
//	        j.On("orders.user_id", "=", "users.id").Where("orders.status", "=", "paid")
//	    }).ToSelect()
//	// SQL:  SELECT `users`.`name` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` = ?
//	// args: [paid]
func (b *Builder) JoinOn(table string, callback func(*JoinBuilder)) *Builder {
	return b.addJoinOn(JoinTypeInner, table, callback)
}

// LeftJoinOn 添加一个 LEFT JOIN（支持多条件）。
// 注意：多个顶层条件混用 AND/OR 时裸 OR 会改变结合范围，
// 需要分组请用 WhereNested/OrWhereNested 括号包裹。
//
//	sql, _, _ := db.Builder().Table("users").LeftJoinOn("orders", func(j *zcdb.JoinBuilder) {
//	    j.On("orders.user_id", "=", "users.id").OrOn("orders.ref_user_id", "=", "users.id")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` LEFT JOIN `orders` ON `orders`.`user_id` = `users`.`id` OR `orders`.`ref_user_id` = `users`.`id`
func (b *Builder) LeftJoinOn(table string, callback func(*JoinBuilder)) *Builder {
	return b.addJoinOn(JoinTypeLeft, table, callback)
}

// RightJoinOn 添加一个 RIGHT JOIN（支持多条件）。
//
//	sql, _, _ := db.Builder().Table("users").RightJoinOn("orders", func(j *zcdb.JoinBuilder) {
//	    j.On("orders.user_id", "=", "users.id")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` RIGHT JOIN `orders` ON `orders`.`user_id` = `users`.`id`
func (b *Builder) RightJoinOn(table string, callback func(*JoinBuilder)) *Builder {
	return b.addJoinOn(JoinTypeRight, table, callback)
}

// addJoinOn 供 JoinOn/LeftJoinOn/RightJoinOn 复用：
// 向 JoinBuilder 注入 grammar/dao（供 WhereExists 等构造子查询），并同步嵌套 join 组。
func (b *Builder) addJoinOn(joinType JoinType, table string, callback func(*JoinBuilder)) *Builder {
	jb := &JoinBuilder{grammar: b.grammar, dao: b.dao}
	callback(jb)
	if jb.err != nil {
		b.err = jb.err
		return b
	}
	b.joins = append(b.joins, JoinClause{
		Type:       joinType,
		Table:      table,
		Conditions: jb.Conditions,
		Joins:      jb.Joins,
	})
	return b
}

// addJoinSub 添加一个派生表（子查询）JOIN，供 JoinSub/LeftJoinSub/RightJoinSub 复用。
func (b *Builder) addJoinSub(joinType JoinType, sub *Builder, alias string, callback func(*JoinBuilder)) *Builder {
	jb := &JoinBuilder{grammar: b.grammar, dao: b.dao}
	if callback != nil {
		callback(jb)
	}
	if jb.err != nil {
		b.err = jb.err
		return b
	}
	b.joins = append(b.joins, JoinClause{
		Type:       joinType,
		Sub:        sub,
		Alias:      alias,
		Conditions: jb.Conditions,
		Joins:      jb.Joins,
	})
	return b
}

// JoinSub 添加一个 INNER JOIN 派生表（子查询）。
// sub 为子查询 Builder，alias 为其别名，callback 构建 ON 条件。
// 子查询绑定参数先于 ON 条件计入总绑定。
//
//	latest := db.Builder().Table("logs").Select("user_id").
//	    SelectRaw("MAX(created_at) AS last_at").GroupBy("user_id")
//	sql, _, _ := db.Builder().Table("users").Select("users.name", "l.last_at").
//	    JoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
//	        j.On("l.user_id", "=", "users.id")
//	    }).ToSelect()
//	// SQL: SELECT `users`.`name`, `l`.`last_at` FROM `users` INNER JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`
func (b *Builder) JoinSub(sub *Builder, alias string, callback func(*JoinBuilder)) *Builder {
	return b.addJoinSub(JoinTypeInner, sub, alias, callback)
}

// LeftJoinSub 添加一个 LEFT JOIN 派生表（子查询），参数规则同 JoinSub。
//
//	sql, _, _ := db.Builder().Table("users").LeftJoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
//	    j.On("l.user_id", "=", "users.id")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` LEFT JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`
func (b *Builder) LeftJoinSub(sub *Builder, alias string, callback func(*JoinBuilder)) *Builder {
	return b.addJoinSub(JoinTypeLeft, sub, alias, callback)
}

// RightJoinSub 添加一个 RIGHT JOIN 派生表（子查询），参数规则同 JoinSub。
//
//	sql, _, _ := db.Builder().Table("users").RightJoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
//	    j.On("l.user_id", "=", "users.id")
//	}).ToSelect()
//	// SQL: SELECT * FROM `users` RIGHT JOIN (SELECT `user_id`, MAX(created_at) AS last_at FROM `logs` GROUP BY `user_id`) AS `l` ON `l`.`user_id` = `users`.`id`
func (b *Builder) RightJoinSub(sub *Builder, alias string, callback func(*JoinBuilder)) *Builder {
	return b.addJoinSub(JoinTypeRight, sub, alias, callback)
}

// JoinBuilder 用于构建复杂的 JOIN ON 条件，由 Builder 的 JoinOn/LeftJoinOn/RightJoinOn/JoinSub 等方法的回调参数传入。
// On 系列（On/OrOn）生成列与列的比较条件；Where 系列生成列与值的比较条件（带占位符绑定）；
// 嵌套方法（WhereNested/OnNested）将回调内条件用括号分组；JoinOn/CrossJoinOn 可在当前 join 内继续嵌套 join（编译为带括号的 join 组）。
type JoinBuilder struct {
	Conditions []JoinCondition
	Joins      []JoinClause // 嵌套 join 组（JoinBuilder.JoinOn 追加）
	err        error        // 累积错误（如无效运算符）
	grammar    Grammar      // 供 WhereExists 回调等需要构造子查询的场景使用
	dao        *DBDao
}

// On 添加一个 AND ON 列比较条件（列与列比较，不产生占位符绑定）。
// op 非法时错误累积到 JoinBuilder，最终在 ToSelect 等编译方法返回。
//
//	db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
//	    j.On("orders.user_id", "=", "users.id")
//	})
//	// SQL: SELECT * FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id`
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

// OrOn 添加一个 OR ON 列比较条件。多个顶层条件混用 AND/OR 时裸 OR 会改变结合范围，
// 需要分组请用 OnNested/WhereNested 括号包裹。
//
//	j.On("orders.user_id", "=", "users.id").OrOn("orders.ref_user_id", "=", "users.id")
//	// SQL: ... LEFT JOIN `orders` ON `orders`.`user_id` = `users`.`id` OR `orders`.`ref_user_id` = `users`.`id`
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

// Where 添加一个 AND ON 值比较条件 (ON ... AND column op ?)，值走占位符绑定。
// value 为 *Builder 时编译为子查询比较 (column op (SELECT ...))。
//
//	j.On("orders.user_id", "=", "users.id").Where("orders.status", "=", "paid")
//	// SQL: ... INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` = ?
//	// args: [paid]
//
//	j.On("orders.user_id", "=", "users.id").Where("orders.amount", ">", db.Builder().Table("stats").SelectRaw("AVG(amount)"))
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND `orders`.`amount` > (SELECT AVG(amount) FROM `stats`)
func (j *JoinBuilder) Where(column, op string, value any) *JoinBuilder {
	return j.addWhere(column, op, value, "AND")
}

// OrWhere 添加一个 OR ON 值比较条件 (ON ... OR column op ?)。value 规则同 Where。
//
//	j.On("orders.user_id", "=", "users.id").OrWhere("orders.vip_channel", "=", 1)
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` OR `orders`.`vip_channel` = ?
//	// args: [1]
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

// WhereNull 添加 AND ON 空值条件 (ON ... AND column IS NULL)，支持多列展开（每列一个 IS NULL 条件）。
//
//	j.On("profiles.user_id", "=", "users.id").WhereNull("profiles.deleted_at")
//	// SQL: ... LEFT JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND `profiles`.`deleted_at` IS NULL
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

// WhereNotNull 添加 AND ON 非空条件 (ON ... AND column IS NOT NULL)，支持多列展开（每列一个 IS NOT NULL 条件）。
//
//	j.On("profiles.user_id", "=", "users.id").WhereNotNull("profiles.avatar", "profiles.bio")
//	// SQL: ... INNER JOIN `profiles` ON `profiles`.`user_id` = `users`.`id` AND `profiles`.`avatar` IS NOT NULL AND `profiles`.`bio` IS NOT NULL
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
// values 支持 []any（值列表，逐个占位符绑定）或 *Builder（子查询 IN (SELECT ...)），其余类型报错。
//
//	j.On("orders.user_id", "=", "users.id").WhereIn("orders.status", []any{"paid", "shipped"})
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` IN (?, ?)
//	// args: [paid shipped]
//
//	j.On("orders.user_id", "=", "users.id").WhereIn("orders.product_id", db.Builder().Table("hot_products").Select("id"))
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND `orders`.`product_id` IN (SELECT `id` FROM `hot_products`)
func (j *JoinBuilder) WhereIn(column string, values any) *JoinBuilder {
	return j.addWhereIn(column, values, false)
}

// WhereNotIn 添加 AND ON NOT IN 条件。values 规则同 WhereIn。
//
//	j.On("orders.user_id", "=", "users.id").WhereNotIn("orders.status", []any{"cancelled"})
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND `orders`.`status` NOT IN (?)
//	// args: [cancelled]
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
// sub 支持 func(*Builder) 回调（回调参数是注入了当前 grammar 的新 Builder）或 *Builder。
//
//	j.On("orders.user_id", "=", "users.id").WhereExists(func(q *zcdb.Builder) {
//	    q.Table("payments").SelectRaw("1").WhereColumn("payments.order_id", "=", "orders.id")
//	})
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND EXISTS (SELECT 1 FROM `payments` WHERE `payments`.`order_id` = `orders`.`id`)
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

// WhereNested 添加括号分组的嵌套条件 (ON ... AND (...))，回调内的条件编译时整体加括号。
// 回调内条件为空时不追加任何条件。
//
//	j.On("orders.user_id", "=", "users.id").WhereNested(func(q *zcdb.JoinBuilder) {
//	    q.Where("orders.status", "=", "paid").Where("orders.amount", ">", 100)
//	})
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND (`orders`.`status` = ? AND `orders`.`amount` > ?)
//	// args: [paid 100]
func (j *JoinBuilder) WhereNested(callback func(*JoinBuilder)) *JoinBuilder {
	return j.addNested(callback, "AND")
}

// OrWhereNested 添加 OR 连接的括号分组嵌套条件 (ON ... OR (...))。回调规则同 WhereNested。
//
//	j.On("orders.user_id", "=", "users.id").OrWhereNested(func(q *zcdb.JoinBuilder) {
//	    q.Where("orders.vip_channel", "=", 1)
//	})
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` OR (`orders`.`vip_channel` = ?)
//	// args: [1]
func (j *JoinBuilder) OrWhereNested(callback func(*JoinBuilder)) *JoinBuilder {
	return j.addNested(callback, "OR")
}

// OnNested 添加括号分组的嵌套 On 条件（与 WhereNested 同语义，命名对齐 On 系列）。
//
//	j.On("orders.user_id", "=", "users.id").OnNested(func(q *zcdb.JoinBuilder) {
//	    q.Where("orders.status", "=", "paid").OrWhere("orders.amount", ">", 1000)
//	})
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND (`orders`.`status` = ? OR `orders`.`amount` > ?)
//	// args: [paid 1000]
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

// JoinOn 在当前 join 内追加嵌套 INNER JOIN（编译为带括号的 join 组），
// 用于 MySQL 风格的显式结合优先级控制。callback 内构造内层 join 的 ON 条件。
//
//	db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
//	    j.On("orders.user_id", "=", "users.id").JoinOn("order_items", func(q *zcdb.JoinBuilder) {
//	        q.On("order_items.order_id", "=", "orders.id")
//	    })
//	})
//	// SQL: SELECT * FROM `users` INNER JOIN (`orders` INNER JOIN `order_items` ON `order_items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`
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
// 注意：PostgreSQL 不支持 CROSS JOIN ... ON，编译时会转为 INNER JOIN ... ON。
//
//	j.On("orders.user_id", "=", "users.id").CrossJoinOn("coupons", "coupons.id", "=", "orders.coupon_id")
//	// MySQL SQL: ... INNER JOIN (`orders` CROSS JOIN `coupons` ON `coupons`.`id` = `orders`.`coupon_id`) ON `orders`.`user_id` = `users`.`id`
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

// Raw 添加一个 AND 连接的原始 SQL ON 条件，sql 内的 ? 按序绑定 bindings。
// 原始片段不做标识符包裹，表名/列名需自行书写完整。
//
//	j.On("orders.user_id", "=", "users.id").Raw("YEAR(orders.created_at) = ?", 2026)
//	// SQL: ... ON `orders`.`user_id` = `users`.`id` AND YEAR(orders.created_at) = ?
//	// args: [2026]
func (j *JoinBuilder) Raw(sql string, bindings ...any) *JoinBuilder {
	j.Conditions = append(j.Conditions, JoinCondition{
		Type:     "raw",
		SQL:      sql,
		Bindings: bindings,
		Boolean:  "AND",
	})
	return j
}
