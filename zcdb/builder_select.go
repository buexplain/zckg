package zcdb

// 本文件包含 Builder 的数据源与查询列构造方法：
// Table/TableSub（FROM 数据源，互斥覆盖）、Select 系列（查询列）、Distinct。

// SelectColumn 表示 SELECT 中的一个列。
// Raw 为 true 时，Value 直接嵌入 SQL，不经过 WrapColumn 引用。
type SelectColumn struct {
	Value string
	Raw   bool
}

// ==================== 表和列 ====================

// Table 设置查询的主表名；同时清空 TableSub 设置的 FROM 子查询与别名，
// 保证两者互斥且“后调用者生效”。返回自身以支持链式调用。
//
//	sql, args, _ := db.Builder().Table("users").ToSelect()
//	// SQL: SELECT * FROM `users`
func (b *Builder) Table(tableName string) *Builder {
	b.table = tableName
	b.tableSub = nil
	b.tableAlias = ""
	return b
}

// TableSub 设置 FROM 子查询（派生表），与 Table 互斥：后调用者生效。
// 编译时 tableSub 非 nil 则优先输出 (子查询) AS 别名，否则输出普通表名。
// 子查询内部的绑定参数排在最外层 WHERE 之前（绑定顺序：FROM_SUB → JOIN → WHERE）。
//
//	sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
//	sql, args, _ := db.Builder().TableSub(sub, "o").Where("o.user_id", ">", 1).ToSelect()
//	// SQL:  SELECT * FROM (SELECT `user_id` FROM `orders` WHERE `amount` > ?) AS `o` WHERE `o`.`user_id` > ?
//	// args: [100 1]
func (b *Builder) TableSub(sub *Builder, alias string) *Builder {
	b.tableSub = sub
	b.tableAlias = alias
	return b
}

// Select 显式指定 SELECT 的列名（替换语义：覆盖之前设置的列）。
// 列名经 Grammar.WrapColumn 引用（MySQL 反引号、PG/SQLite 双引号），支持 "表.列" 形式。
// 未调用 Select 时默认编译为 SELECT *。
//
//	sql, _, _ := db.Builder().Table("users").Select("id", "name").ToSelect()
//	// SQL: SELECT `id`, `name` FROM `users`
func (b *Builder) Select(columns ...string) *Builder {
	b.columns = make([]SelectColumn, len(columns))
	for i, col := range columns {
		b.columns[i] = SelectColumn{Value: col, Raw: false}
	}
	return b
}

// SelectRaw 追加原始 SQL 表达式作为列（追加语义），不经过 WrapColumn 引用，
// 适用于聚合函数、窗口函数等无法用普通列名表达的场景。
//
//	sql, _, _ := db.Builder().Table("users").Select("id").SelectRaw("COUNT(*) OVER() AS total").ToSelect()
//	// SQL: SELECT `id`, COUNT(*) OVER() AS total FROM `users`
func (b *Builder) SelectRaw(expression string) *Builder {
	b.columns = append(b.columns, SelectColumn{Value: expression, Raw: true})
	return b
}

// SelectSub 添加一个子查询作为 SELECT 列（追加语义），编译为 (SELECT ...) AS 别名。
// 子查询的绑定参数在所有其它绑定之前（绑定顺序：SELECT_SUB → FROM_SUB → JOIN → WHERE）。
//
//	cnt := db.Builder().Table("orders").SelectRaw("COUNT(*)").
//	    WhereColumn("orders.user_id", "=", "users.id")
//	sql, _, _ := db.Builder().Table("users").Select("id").
//	    SelectSub(cnt, "order_count").ToSelect()
//	// SQL: SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `orders`.`user_id` = `users`.`id`) AS `order_count` FROM `users`
func (b *Builder) SelectSub(sub *Builder, alias string) *Builder {
	b.selectSubs = append(b.selectSubs, SelectSub{Query: sub, Alias: alias})
	return b
}

// AddSelect 追加 SELECT 列（追加语义，与 Select 的替换语义区分），等价列去重：
// 已存在的非 Raw 列不会重复添加。
//
//	sql, _, _ := db.Builder().Table("users").Select("id").AddSelect("name", "id").ToSelect()
//	// SQL: SELECT `id`, `name` FROM `users`   // id 已存在，不重复
func (b *Builder) AddSelect(columns ...string) *Builder {
	for _, col := range columns {
		exists := false
		for _, c := range b.columns {
			if !c.Raw && c.Value == col {
				exists = true
				break
			}
		}
		if !exists {
			b.columns = append(b.columns, SelectColumn{Value: col, Raw: false})
		}
	}
	return b
}

// Distinct 设置查询返回去重结果，编译为 SELECT DISTINCT。
// 注意：Distinct 下 Count 会自动包裹为子查询再计数，保留去重语义。
//
//	sql, _, _ := db.Builder().Table("users").Select("city").Distinct().ToSelect()
//	// SQL: SELECT DISTINCT `city` FROM `users`
func (b *Builder) Distinct() *Builder {
	b.distinct = true
	return b
}
