package zcdb

// 本文件包含 Builder 的 ToXxx 编译系列（只生成 SQL 与绑定参数、不执行）：
// ToSelect/ToInsert/ToUpdate/ToDelete/ToCount/ToAggregate 等，
// 以及配套的绑定参数收集内部方法（collectXxxBindings）。

// ==================== 终端方法：编译 SQL ====================

// ToSelect 编译 SELECT 查询。
// 列名通过 Select() 链式调用指定，未指定时默认 SELECT *。
// 未设置数据源（Table/TableSub）时返回 ErrEmptyTable；SQLite 不支持锁子句（报错），
// PostgreSQL 不支持 UNION + 锁组合（报错）。
//
//	sql, args, _ := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
//	// SQL:  SELECT * FROM `users` WHERE `age` > ?
//	// args: [25]
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToSelect() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" && b.tableSub == nil {
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

	// 扁平化所有行的值作为绑定参数；Expression 已内联进 SQL，不作为绑定参数
	var args []any
	for _, row := range rows {
		for _, v := range row {
			if _, ok := v.(Expression); !ok {
				args = append(args, v)
			}
		}
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
//	    ToInsertOrIgnore(User{Name: "alice", Age: 25, Email: "a@t.com"})
//	// MySQL SQL: INSERT IGNORE INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
//	// PG SQL:    INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3) ON CONFLICT DO NOTHING
//	// SQLite SQL: INSERT OR IGNORE INTO "users" ("name", "age", "email") VALUES (?, ?, ?)
//	// args: [alice 25 a@t.com]
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
		// Expression 已内联进 SQL，不作为绑定参数
		for _, v := range row {
			if _, ok := v.(Expression); !ok {
				args = append(args, v)
			}
		}
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
//	        User{Name: "alice", Age: 25, Email: "a@t.com"},
//	        []string{"email"},       // 以 email 作为唯一键
//	        []string{"name", "age"}, // 冲突时更新 name 和 age
//	    )
//	// MySQL SQL: INSERT INTO `users` (`name`, `age`, `email`) VALUES (?, ?, ?)
//	//            ON DUPLICATE KEY UPDATE `name` = VALUES(`name`), `age` = VALUES(`age`)
//	// PG SQL:    INSERT INTO "users" ("name", "age", "email") VALUES ($1, $2, $3)
//	//            ON CONFLICT ("email") DO UPDATE SET "name" = EXCLUDED."name", "age" = EXCLUDED."age"
//	// args: [alice 25 a@t.com]
//
// 注意：PostgreSQL/SQLite 下 uniqueBy 为空时返回 ErrUpsertUniqueByRequired（无法生成 ON CONFLICT 目标）。
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

	// MySQL 使用 ON DUPLICATE KEY UPDATE 无需冲突目标列；
	// PostgreSQL/SQLite 需要 uniqueBy 生成 ON CONFLICT 目标，为空时生成的 SQL 非法，直接拒绝
	if len(uniqueBy) == 0 {
		switch b.grammar.(type) {
		case *PostgresGrammar, *SQLiteGrammar:
			return "", nil, ErrUpsertUniqueByRequired
		}
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
//	    ToInsertUsing([]string{"name", "age"}, func(sub *Builder) {
//	        sub.Table("users").Select("name", "age").Where("status", "=", "active")
//	    })
//	// SQL:  INSERT INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users` WHERE `status` = ?
//	// args: [active]
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

	// 参数校验：子查询编译错误或缺少数据源时直接返回错误，避免生成非法 SQL
	if sub.err != nil {
		return "", nil, sub.err
	}
	if sub.table == "" && sub.tableSub == nil {
		return "", nil, ErrEmptyTable
	}

	sql := b.grammar.CompileInsertUsing(b, columns, sub)
	args := sub.collectSelectBindings()

	return sql, args, nil
}

// ToInsertOrIgnoreUsing 编译忽略冲突的 INSERT INTO ... SELECT 语句，
// 与 ToInsertUsing 的区别仅在冲突处理：已存在的记录被静默跳过。
// 不同方言语法：
//   - MySQL:      INSERT IGNORE INTO ... SELECT ...
//   - PostgreSQL: INSERT INTO ... SELECT ... ON CONFLICT DO NOTHING
//   - SQLite:     INSERT OR IGNORE INTO ... SELECT ...
//
// 参数说明与校验规则同 ToInsertUsing。
//
// 示例：
//
//	sql, _, err := NewBuilder(g).
//	    Table("users_archive").
//	    ToInsertOrIgnoreUsing([]string{"name", "age"}, func(sub *Builder) {
//	        sub.Table("users").Select("name", "age")
//	    })
//	// MySQL SQL: INSERT IGNORE INTO `users_archive` (`name`, `age`) SELECT `name`, `age` FROM `users`
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToInsertOrIgnoreUsing(columns []string, callback func(*Builder)) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}

	sub := NewBuilder(b.grammar, b.dao)
	callback(sub)

	// 参数校验：子查询编译错误或缺少数据源时直接返回错误，避免生成非法 SQL
	if sub.err != nil {
		return "", nil, sub.err
	}
	if sub.table == "" && sub.tableSub == nil {
		return "", nil, ErrEmptyTable
	}

	sql := b.grammar.CompileInsertOrIgnoreUsing(b, columns, sub)
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

// ToDeleteJoin 编译按关联条件删除主表行的 DELETE 语句。
// 三方言实现路径不同：
//   - MySQL:      多表 DELETE 直译 DELETE t FROM t JOIN ... WHERE ...
//   - PostgreSQL: DELETE FROM t USING t2 WHERE join条件 AND where条件
//   - SQLite:     DELETE FROM t WHERE id IN (SELECT t.id FROM t JOIN ... WHERE ...)
//
// 要求至少一个 JOIN，否则返回 ErrDeleteJoinNoJoin。
//
// 示例：
//
//	sql, args, err := db.Builder().Table("users").
//	    Join("orders", "orders.user_id", "=", "users.id").
//	    Where("orders.status", "=", "cancelled").
//	    ToDeleteJoin()
//	// MySQL SQL:  DELETE `users` FROM `users` INNER JOIN `orders` ON `orders`.`user_id` = `users`.`id` WHERE `orders`.`status` = ?
//	// PG SQL:     DELETE FROM "users" USING "orders" WHERE "orders"."user_id" = "users"."id" AND "orders"."status" = $1
//	// SQLite SQL: DELETE FROM "users" WHERE "id" IN (SELECT "users"."id" FROM "users" INNER JOIN "orders" ON ... WHERE "orders"."status" = ?)
//	// args: [cancelled]
//
// 返回 (SQL, 绑定参数, 错误)
func (b *Builder) ToDeleteJoin() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}
	if len(b.joins) == 0 {
		return "", nil, ErrDeleteJoinNoJoin
	}

	sqlStr := b.grammar.CompileDeleteJoin(b)

	// 绑定顺序三方言一致：JOIN 条件 → WHERE 条件
	var args []any
	args = append(args, b.collectJoinBindings()...)
	args = append(args, b.collectWhereBindings()...)

	return sqlStr, args, nil
}

// ToTruncate 编译 TRUNCATE 语句（清空表数据）。
// SQLite 方言不支持 TRUNCATE，会自动转为 DELETE FROM。
//
// 示例：
//
//	sql, err := NewBuilder(g).Table("users").ToTruncate()
//	// MySQL/PG SQL: TRUNCATE TABLE `users`
//	// SQLite SQL:   DELETE FROM "users"
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
// 当查询包含 UNION、GROUP BY 或 DISTINCT 时，将整个查询包裹为子查询再计数，
// 避免生成无效 SQL 或丢失分组/去重语义（此时返回的是分组数/去重后行数）。
//
//	sql, args, _ := db.Builder().Table("users").Where("status", "=", "active").ToCount()
//	// SQL:  SELECT COUNT(*) FROM `users` WHERE `status` = ?
//	// args: [active]
//
//	sql, _, _ = db.Builder().Table("orders").GroupBy("user_id").ToCount()
//	// SQL: SELECT COUNT(*) FROM (SELECT 1 FROM `orders` GROUP BY `user_id`) AS `t`
//
//	sql, _, _ = db.Builder().Table("users").Select("city").Distinct().ToCount()
//	// SQL: SELECT COUNT(*) FROM (SELECT DISTINCT `city` FROM `users`) AS `t`
func (b *Builder) ToCount() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" && b.tableSub == nil && len(b.unions) == 0 {
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
	if len(b.groups) > 0 { // 保存并清除分页/排序/锁，这些对计数无意义且可能干扰子查询
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

	// DISTINCT 查询：SELECT DISTINCT COUNT(*) 中 DISTINCT 对聚合结果无效，
	// 会丢失去重语义返回总行数，因此将查询包裹为子查询再计数。
	if b.distinct {
		// 保存并清除分页/排序/锁，这些对计数无意义且可能干扰子查询
		origLimit, origOffset := b.limit, b.offset
		origOrders := b.orders
		origLock := b.lockClause
		b.limit, b.offset = 0, 0
		b.orders = nil
		b.lockClause = ""

		subSQL := b.grammar.CompileSelect(b, b.columns)
		args := b.collectSelectBindings()

		b.limit, b.offset = origLimit, origOffset
		b.orders = origOrders
		b.lockClause = origLock

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

// ToExists 编译存在性查询：SELECT 1 ... LIMIT 1。
// 数据库找到第一条匹配记录即返回，避免 ToCount 的 COUNT(*) 扫描所有匹配行，
// 大表场景下存在性检查显著更快。
// UNION 查询与 ToCount 相同：整个 UNION 包裹为子查询再附加 LIMIT 1；
// GROUP BY/DISTINCT 场景直接编译（SELECT 1 与这些子句无冲突）。
//
//	sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).ToExists()
//	// SQL:  SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1
//	// args: [1]
func (b *Builder) ToExists() (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" && b.tableSub == nil && len(b.unions) == 0 {
		return "", nil, ErrEmptyTable
	}

	// 保存并清除分页/排序/锁/列：存在性检查只关心是否有匹配行
	origLimit, origOffset := b.limit, b.offset
	origOrders := b.orders
	origLock := b.lockClause
	origColumns := b.columns
	origSelectSubs := b.selectSubs
	b.limit, b.offset = 0, 0
	b.orders = nil
	b.lockClause = ""
	b.columns = []SelectColumn{{Value: "1", Raw: true}}
	b.selectSubs = nil

	// 使用 defer 确保 panic 时也能恢复状态
	defer func() {
		b.limit, b.offset = origLimit, origOffset
		b.orders = origOrders
		b.lockClause = origLock
		b.columns = origColumns
		b.selectSubs = origSelectSubs
	}()

	subSQL := b.grammar.CompileSelect(b, b.columns)
	args := b.collectSelectBindings()

	// UNION 查询：整个 UNION 作为子查询后附加 LIMIT 1
	if len(b.unions) > 0 {
		return "SELECT 1 FROM (" + subSQL + ") AS " + b.grammar.WrapTable("t") + " LIMIT 1", args, nil
	}
	return subSQL + " LIMIT 1", args, nil
}

// ToAggregate 编译聚合查询（MAX/MIN/SUM/AVG），生成 SELECT AGG(col) AS aggregate FROM ...。
// aggregate 必须是 MAX/MIN/SUM/AVG 之一，否则返回 ErrInvalidAggregate。
// UNION 查询与 ToCount 相同：将整个 UNION 包裹为子查询后再聚合；
// GROUP BY/DISTINCT 场景直接编译（聚合列与这些子句无冲突，执行端取首行）。
//
//	sql, args, _ := db.Builder().Table("orders").Where("status", "=", "paid").ToAggregate("MAX", "amount")
//	// SQL:  SELECT MAX(`amount`) AS `aggregate` FROM `orders` WHERE `status` = ?
//	// args: [paid]
func (b *Builder) ToAggregate(aggregate string, column string) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" && b.tableSub == nil && len(b.unions) == 0 {
		return "", nil, ErrEmptyTable
	}
	switch aggregate {
	case "MAX", "MIN", "SUM", "AVG":
	default:
		return "", nil, ErrInvalidAggregate
	}

	// 聚合表达式：AGG(wrap col) AS aggregate
	aggExpr := aggregate + "(" + b.grammar.WrapColumn(column) + ") AS " + b.grammar.WrapColumn("aggregate")

	// UNION 查询：将整个 UNION 作为子查询包裹后聚合
	if len(b.unions) > 0 {
		// 保存并清除分页/排序/锁，这些对聚合无意义且可能干扰子查询
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

		return "SELECT " + aggExpr + " FROM (" + unionSQL + ") AS " + b.grammar.WrapTable("t"), args, nil
	}

	// 保存原始状态并替换为聚合列；清除分页/排序/锁/SELECT 子查询
	origColumns := b.columns
	origSelectSubs := b.selectSubs
	origLimit, origOffset := b.limit, b.offset
	origOrders := b.orders
	origLock := b.lockClause
	// 使用 defer 确保 panic 时也能恢复状态
	defer func() {
		b.columns = origColumns
		b.selectSubs = origSelectSubs
		b.limit, b.offset = origLimit, origOffset
		b.orders = origOrders
		b.lockClause = origLock
	}()
	b.columns = []SelectColumn{{Value: aggExpr, Raw: true}}
	b.selectSubs = nil
	b.limit, b.offset = 0, 0
	b.orders = nil
	b.lockClause = ""

	sqlStr := b.grammar.CompileSelect(b, b.columns)
	args := b.collectSelectBindings()

	return sqlStr, args, nil
}

// ToIncrement 编译原子自增 UPDATE：SET col = col + ?（多列一次更新）。
// columns 与 amounts 必须等长且非空，否则返回 ErrIncrementColumns。
// 复用 CompileUpdate 编译路径，绑定顺序按 UpdateSetBeforeJoin 区分方言。
//
//	sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).
//	    ToIncrement([]string{"wallet", "level"}, []any{100, 1})
//	// SQL:  UPDATE `users` SET `wallet` = `wallet` + ?, `level` = `level` + ? WHERE `id` = ?
//	// args: [100 1 1]
func (b *Builder) ToIncrement(columns []string, amounts []any) (string, []any, error) {
	return b.toIncDec(columns, amounts, "+")
}

// ToDecrement 编译原子自减 UPDATE：SET col = col - ?（多列一次更新）。
// 参数规则同 ToIncrement。
//
//	sql, args, _ := db.Builder().Table("users").Where("id", "=", 1).
//	    ToDecrement([]string{"wallet"}, []any{50})
//	// SQL:  UPDATE `users` SET `wallet` = `wallet` - ? WHERE `id` = ?
//	// args: [50 1]
func (b *Builder) ToDecrement(columns []string, amounts []any) (string, []any, error) {
	return b.toIncDec(columns, amounts, "-")
}

// toIncDec ToIncrement/ToDecrement 共用实现：
// 用 grammar.WrapColumn(col) + " op ?" 构造 Expression 后走 CompileUpdate 编译路径。
// Expression 内的 ? 占位符由 amounts 按 SET 位置填充（PG 编译层自动转 $N）。
func (b *Builder) toIncDec(columns []string, amounts []any, op string) (string, []any, error) {
	if b.err != nil {
		return "", nil, b.err
	}
	if b.table == "" {
		return "", nil, ErrEmptyTable
	}
	if len(columns) == 0 || len(columns) != len(amounts) {
		return "", nil, ErrIncrementColumns
	}

	values := make([]any, len(columns))
	for i, col := range columns {
		values[i] = NewExpression(b.grammar.WrapColumn(col) + " " + op + " ?")
	}

	sqlStr := b.grammar.CompileUpdate(b, columns, values)

	// 绑定顺序与 ToUpdate 一致：amounts 占据 SET 位置
	var args []any
	if b.grammar.UpdateSetBeforeJoin() {
		// SET → JOIN → WHERE
		args = append(args, amounts...)
		args = append(args, b.collectJoinBindings()...)
	} else {
		// JOIN → SET → WHERE
		args = append(args, b.collectJoinBindings()...)
		args = append(args, amounts...)
	}
	args = append(args, b.collectWhereBindings()...)

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
	if b.tableSub != nil {
		args = append(args, b.tableSub.collectSelectBindings()...)
	}
	// JOIN 绑定参数
	for _, j := range b.joins {
		args = append(args, collectJoinClauseBindings(j)...)
	}
	// WHERE
	args = append(args, b.collectWhereBindings()...)
	// GROUP BY（绑定顺序规则：where → groupBy → having）
	for _, g := range b.groups {
		for _, v := range g.Bindings {
			if _, ok := v.(Expression); !ok {
				args = append(args, v)
			}
		}
	}
	// HAVING
	args = append(args, collectHavingBindings(b.havings)...)
	// UNION
	for _, u := range b.unions {
		args = append(args, u.Query.collectSelectBindings()...)
	}
	return args
}

// collectHavingBindings 递归收集 HAVING 子句的绑定参数（含嵌套分组）。
func collectHavingBindings(havings []HavingClause) []any {
	var args []any
	for _, h := range havings {
		switch h.Type {
		case "basic":
			// Expression 已直接内嵌到 SQL，不作为绑定参数
			if _, ok := h.Value.(Expression); !ok {
				args = append(args, h.Value)
			}
		case "raw":
			for _, v := range h.Bindings {
				if _, ok := v.(Expression); !ok {
					args = append(args, v)
				}
			}
		case "between":
			args = append(args, h.Min, h.Max)
		case "nested":
			if h.Nested != nil {
				args = append(args, collectHavingBindings(h.Nested.havings)...)
			}
		case "null", "notNull":
			// 无绑定参数
		}
	}
	return args
}

// collectJoinBindings 收集 JOIN ON 条件中的绑定参数（value 与 raw 类型）。
func (b *Builder) collectJoinBindings() []any {
	var args []any
	for _, j := range b.joins {
		// UPDATE ... JOIN 场景不含派生表子查询与嵌套 join 组的 SQL 文本，
		// 但 MySQL 多表 UPDATE 直译 JOIN 时嵌套组的条件也会编译，故一并收集
		args = append(args, collectJoinClauseBindings(j)...)
	}
	return args
}

// collectJoinClauseBindings 收集单个 JOIN 子句的绑定参数。
// 顺序与编译顺序一致：派生表子查询 → 嵌套 join 组 → ON 条件。
func collectJoinClauseBindings(join JoinClause) []any {
	var args []any
	if join.Sub != nil {
		args = append(args, join.Sub.collectSelectBindings()...)
	}
	for _, inner := range join.Joins {
		args = append(args, collectJoinClauseBindings(inner)...)
	}
	args = append(args, collectJoinConditionBindings(join.Conditions)...)
	return args
}

// collectJoinConditionBindings 递归收集 ON 条件列表的绑定参数。
func collectJoinConditionBindings(conditions []JoinCondition) []any {
	var args []any
	for _, c := range conditions {
		switch c.Type {
		case "value":
			if _, ok := c.Value.(Expression); !ok {
				args = append(args, c.Value)
			}
		case "raw":
			for _, v := range c.Bindings {
				if _, ok := v.(Expression); !ok {
					args = append(args, v)
				}
			}
		case "in":
			args = append(args, c.Values...)
		case "inSub", "exists", "subValue":
			if c.Sub != nil {
				args = append(args, c.Sub.collectSelectBindings()...)
			}
		case "nested":
			if c.Nested != nil {
				args = append(args, collectJoinConditionBindings(c.Nested.Conditions)...)
			}
		case "column", "null":
			// 无绑定参数
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
			// nil 特判：= nil / != nil 编译为 IS NULL / IS NOT NULL，无绑定参数
			if w.Value == nil && (w.Operator == "=" || w.Operator == "!=" || w.Operator == "<>") {
				continue
			}
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
			for _, v := range w.Bindings {
				if _, ok := v.(Expression); !ok {
					args = append(args, v)
				}
			}
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
		case WhereTypeNullSafe, WhereTypeNullSafeNot:
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
