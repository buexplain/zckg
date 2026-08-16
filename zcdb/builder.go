package zcdb

// 本文件包含 Builder 查询构造器的核心定义：
// Builder 结构体与构造函数 NewBuilder、运算符白名单校验，
// 以及状态管理类方法 Force（全表操作授权）与 Clone（深拷贝）。
// 其余方法按功能分类拆分到 builder_select.go / builder_where.go / builder_join.go /
// builder_group.go / builder_order.go / builder_compile.go / builder_query.go /
// builder_exec.go / builder_cursor.go。

// Builder 是查询构造器的核心，负责收集用户的查询意图和数据。
// Builder 积累状态，Grammar 负责编译 SQL。
type Builder struct {
	grammar Grammar
	dao     *DBDao // 持有 DBDao 引用，用于终端方法执行 SQL

	// 查询状态
	table      string
	columns    []SelectColumn // 用于 SELECT 的列
	selectSubs []SelectSub
	distinct   bool
	tableSub   *Builder // FROM 子查询
	tableAlias string   // FROM 子查询别名
	joins      []JoinClause
	wheres     []WhereClause
	groups     []GroupClause
	havings    []HavingClause
	orders     []OrderClause
	limit      int
	offset     int
	unions     []UnionClause
	lockClause string
	force      bool  // 允许执行无 WHERE 条件的 Delete/Update（全表操作）
	usePrimary bool  // 强制读查询走写（主库）连接（写后读场景），仅影响执行层路由、不影响 SQL 编译
	err        error // 累积错误（如无效运算符）
}

// NewBuilder 创建一个新的sql构造器。
func NewBuilder(grammar Grammar, dao *DBDao) *Builder {
	return &Builder{
		grammar: grammar,
		dao:     dao,
	}
}

// tagName 返回列映射使用的结构体标签名：
// DAO 配置了自定义标签名时优先使用，否则回退默认的 db 标签。
func (b *Builder) tagName() string {
	if b.dao != nil && b.dao.tagName != "" {
		return b.dao.tagName
	}
	return defaultTagName
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
// 运算符大小写不敏感（统一转大写后匹配白名单）。
func validateOperator(op string) error {
	upper := toUpperASCII(op)
	if validOperators[upper] {
		return nil
	}
	return ErrInvalidOperator
}

// toUpperASCII 将 ASCII 小写字母转大写（运算符/排序方向均为 ASCII，不引入 strings.ToUpper 的 Unicode 开销）。
func toUpperASCII(s string) string {
	hasLower := false
	for i := 0; i < len(s); i++ {
		if s[i] >= 'a' && s[i] <= 'z' {
			hasLower = true
			break
		}
	}
	if !hasLower {
		return s
	}
	buf := []byte(s)
	for i, c := range buf {
		if c >= 'a' && c <= 'z' {
			buf[i] = c - 32
		}
	}
	return string(buf)
}

// ==================== 辅助方法 ====================

// Force 标记允许执行无 WHERE 条件的 Delete/Update（全表操作）。
// Update/Increment/Decrement/Delete/DeleteJoin 默认拒绝无 WHERE 条件，防止误操作清空/更新整张表；
// 确需全表操作时显式调用 Force 表示已知意图，或使用 WhereRaw("1=1") 作为逃生口。
// 该标记仅影响执行层保护逻辑，不影响 SQL 编译结果。
//
//	affected, err := db.Builder().Table("users").Force().Delete(ctx)
//	// 无 Force() 时返回 ErrDeleteWithoutWhere；加了 Force() 后执行：
//	// SQL: DELETE FROM `users`
func (b *Builder) Force() *Builder {
	b.force = true
	return b
}

// Primary 标记本次读查询强制走写（主库）连接，不加任何锁。
// 典型场景：写后立即读（如订单写入后立刻查询回读），从库可能因复制延迟尚无数据；
// 与 LockForUpdate 的区别：Primary 不改变编译出的 SQL（无锁子句），仅影响连接路由。
// 该标记经 Clone 保留，First/Value/Paginate 等内部克隆的终端方法同样生效。
//
//	var order Order
//	err := db.Builder().Table("orders").Where("id", "=", id).Primary().First(ctx, &order)
//	// SQL: SELECT * FROM `orders` WHERE `id` = ? LIMIT 1（与不加 Primary 完全一致，但强制命中主库）
func (b *Builder) Primary() *Builder {
	b.usePrimary = true
	return b
}

// Clone 克隆当前 Builder，返回一个独立副本。
// 深拷贝全部查询状态：列、FROM 子查询、JOIN（含派生表与嵌套 join 组）、
// WHERE（含 Values/Bindings 切片与嵌套子查询）、GROUP BY、HAVING、ORDER BY、
// UNION、锁子句与强制主库标记；副本上继续链式修改不会影响原 Builder，反之亦然。
// First/Value/Paginate/CursorBy 等终端方法内部即用 Clone 避免污染调用方的 Builder。
//
//	base := db.Builder().Table("users").Where("status", "active")
//	admins := base.Clone().Where("role", "admin")
//	// base 编译:  SELECT * FROM `users` WHERE `status` = ?            args: [active]
//	// admins 编译: SELECT * FROM `users` WHERE `status` = ? AND `role` = ?  args: [active admin]
func (b *Builder) Clone() *Builder {
	clone := &Builder{
		grammar:    b.grammar,
		dao:        b.dao,
		table:      b.table,
		distinct:   b.distinct,
		limit:      b.limit,
		offset:     b.offset,
		lockClause: b.lockClause,
		tableAlias: b.tableAlias,
		force:      b.force,
		usePrimary: b.usePrimary,
		err:        b.err,
	}
	// FROM 子查询深拷贝
	if b.tableSub != nil {
		clone.tableSub = b.tableSub.Clone()
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
		clone.joins = cloneJoinClauses(b.joins)
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
		clone.groups = make([]GroupClause, len(b.groups))
		copy(clone.groups, b.groups)
		for i := range clone.groups {
			if clone.groups[i].Bindings != nil {
				cp := make([]any, len(clone.groups[i].Bindings))
				copy(cp, clone.groups[i].Bindings)
				clone.groups[i].Bindings = cp
			}
		}
	}
	if b.havings != nil {
		clone.havings = make([]HavingClause, len(b.havings))
		copy(clone.havings, b.havings)
		for i := range clone.havings {
			if clone.havings[i].Bindings != nil {
				cp := make([]any, len(clone.havings[i].Bindings))
				copy(cp, clone.havings[i].Bindings)
				clone.havings[i].Bindings = cp
			}
			if clone.havings[i].Nested != nil {
				clone.havings[i].Nested = clone.havings[i].Nested.Clone()
			}
		}
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

// cloneJoinClauses 递归深拷贝 join 子句列表（含任意深度的嵌套 join 组、派生表与 ON 条件）。
func cloneJoinClauses(joins []JoinClause) []JoinClause {
	if joins == nil {
		return nil
	}
	cloned := make([]JoinClause, len(joins))
	copy(cloned, joins)
	for i := range cloned {
		// 派生表子查询深拷贝
		if cloned[i].Sub != nil {
			cloned[i].Sub = cloned[i].Sub.Clone()
		}
		cloned[i].Conditions = cloneJoinConditions(cloned[i].Conditions)
		// 嵌套 join 组递归深拷贝
		cloned[i].Joins = cloneJoinClauses(cloned[i].Joins)
	}
	return cloned
}

// cloneJoinConditions 深拷贝 ON 条件列表（Values/Bindings 切片、Sub 子查询、
// Nested 嵌套条件（含其内部嵌套 join 组））。
func cloneJoinConditions(conditions []JoinCondition) []JoinCondition {
	if conditions == nil {
		return nil
	}
	conds := make([]JoinCondition, len(conditions))
	copy(conds, conditions)
	for j := range conds {
		if conds[j].Bindings != nil {
			cp := make([]any, len(conds[j].Bindings))
			copy(cp, conds[j].Bindings)
			conds[j].Bindings = cp
		}
		if conds[j].Values != nil {
			cp := make([]any, len(conds[j].Values))
			copy(cp, conds[j].Values)
			conds[j].Values = cp
		}
		if conds[j].Sub != nil {
			conds[j].Sub = conds[j].Sub.Clone()
		}
		if conds[j].Nested != nil {
			inner := &JoinBuilder{
				Conditions: cloneJoinConditions(conds[j].Nested.Conditions),
				Joins:      cloneJoinClauses(conds[j].Nested.Joins),
				grammar:    conds[j].Nested.grammar,
				dao:        conds[j].Nested.dao,
			}
			conds[j].Nested = inner
		}
	}
	return conds
}
