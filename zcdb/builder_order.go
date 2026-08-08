package zcdb

// 本文件包含 Builder 的排序、分页、UNION、锁构造方法：
// OrderBy 系列、Limit/Offset/ForPage、Union/UnionAll、LockForUpdate/SharedLock。
// 分类依据见 docs/builder-api.md 第 6 节。

// ==================== ORDER BY ====================

// OrderBy 添加一个 ORDER BY 子句，多次调用按顺序累积多个排序列。
// direction 大小写不敏感，仅 "DESC"（忽略大小写）为降序，其余任何值（含空串）均归一为 ASC。
//
//	sql, _, _ := db.Builder().Table("users").OrderBy("age", "DESC").OrderBy("name", "whatever").ToSelect()
//	// SQL: SELECT * FROM `users` ORDER BY `age` DESC, `name` ASC
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

// OrderByDesc 按降序添加一个 ORDER BY 子句，等价 OrderBy(column, "DESC")。
//
//	sql, _, _ := db.Builder().Table("users").OrderByDesc("created_at").ToSelect()
//	// SQL: SELECT * FROM `users` ORDER BY `created_at` DESC
func (b *Builder) OrderByDesc(column string) *Builder {
	return b.OrderBy(column, "DESC")
}

// OrderByRaw 添加一个原始 SQL ORDER BY 子句，不做标识符包裹、不支持绑定（需自行内联值）。
//
//	sql, _, _ := db.Builder().Table("users").OrderByRaw("FIELD(status, 'active', 'frozen')").ToSelect()
//	// SQL: SELECT * FROM `users` ORDER BY FIELD(status, 'active', 'frozen')
func (b *Builder) OrderByRaw(sql string) *Builder {
	b.orders = append(b.orders, OrderClause{Raw: sql})
	return b
}

// InRandomOrder 按随机顺序排序，随机函数由方言决定：
// MySQL 为 RAND()，PostgreSQL/SQLite 为 RANDOM()。
//
//	sql, _, _ := db.Builder().Table("users").InRandomOrder().ToSelect()
//	// MySQL SQL: SELECT * FROM `users` ORDER BY RAND()
//	// PG/SQLite SQL: SELECT * FROM "users" ORDER BY RANDOM()
func (b *Builder) InRandomOrder() *Builder {
	b.orders = append(b.orders, OrderClause{Raw: b.grammar.CompileRandom()})
	return b
}

// ==================== LIMIT / OFFSET ====================

// Limit 设置查询结果数量限制（编译为 LIMIT n，n<=0 时不输出 LIMIT）。
//
//	sql, _, _ := db.Builder().Table("users").Limit(10).ToSelect()
//	// SQL: SELECT * FROM `users` LIMIT 10
func (b *Builder) Limit(n int) *Builder {
	b.limit = n
	return b
}

// Offset 设置查询结果偏移量（编译为 OFFSET n，n<=0 时不输出）。
//
//	sql, _, _ := db.Builder().Table("users").Limit(10).Offset(20).ToSelect()
//	// SQL: SELECT * FROM `users` LIMIT 10 OFFSET 20
func (b *Builder) Offset(n int) *Builder {
	b.offset = n
	return b
}

// ForPage 设置分页（page 从 1 开始，page<1 时修正为 1），
// 等价 Limit(perPage).Offset((page-1)*perPage)。
//
//	sql, _, _ := db.Builder().Table("users").ForPage(2, 20).ToSelect()
//	// SQL: SELECT * FROM `users` LIMIT 20 OFFSET 20
func (b *Builder) ForPage(page, perPage int) *Builder {
	if page < 1 {
		page = 1
	}
	b.limit = perPage
	b.offset = (page - 1) * perPage
	return b
}

// ==================== UNION ====================

// Union 添加一个 UNION 查询（去重合并），可链式多次调用追加。
// 编译时各查询加括号包裹，绑定按主查询 → 各 UNION 查询顺序合并。
//
//	admins := db.Builder().Table("admins").Select("name")
//	sql, _, _ := db.Builder().Table("users").Select("name").Union(admins).ToSelect()
//	// SQL: (SELECT `name` FROM `users`) UNION (SELECT `name` FROM `admins`)
func (b *Builder) Union(query *Builder) *Builder {
	b.unions = append(b.unions, UnionClause{Query: query, All: false})
	return b
}

// UnionAll 添加一个 UNION ALL 查询（不去重合并）。规则同 Union。
//
//	sql, _, _ := db.Builder().Table("users").Select("name").UnionAll(admins).ToSelect()
//	// SQL: (SELECT `name` FROM `users`) UNION ALL (SELECT `name` FROM `admins`)
func (b *Builder) UnionAll(query *Builder) *Builder {
	b.unions = append(b.unions, UnionClause{Query: query, All: true})
	return b
}

// ==================== LOCK ====================

// LockForUpdate 设置排他锁 (FOR UPDATE)，需在事务中使用。
// 执行时带锁查询会强制走写（主库）连接，避免读写分离下锁不生效。
//
//	sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).LockForUpdate().ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `id` = ? FOR UPDATE
//	// args: [1]
func (b *Builder) LockForUpdate() *Builder {
	b.lockClause = "FOR UPDATE"
	return b
}

// SharedLock 设置共享锁，需在事务中使用，不同方言编译结果不同：
//   - MySQL:      LOCK IN SHARE MODE
//   - PostgreSQL: FOR SHARE（自动转换）
//   - SQLite:     不支持，编译时返回错误 zcdb: SQLite does not support LOCK clauses
//
// 执行时带锁查询会强制走写（主库）连接。
//
//	sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).SharedLock().ToSelect()
//	// MySQL SQL: SELECT * FROM `users` WHERE `id` = ? LOCK IN SHARE MODE
//	// PG SQL:    SELECT * FROM "users" WHERE "id" = $1 FOR SHARE
//	// args: [1]
func (b *Builder) SharedLock() *Builder {
	b.lockClause = "LOCK IN SHARE MODE"
	return b
}
