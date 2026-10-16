package zcdb

import "errors"

var (
	// ErrEmptyData 插入数据为空切片
	ErrEmptyData = errors.New("zcdb: data is empty")
	// ErrInvalidStruct 数据不是结构体/结构体指针（或它们的切片）
	ErrInvalidStruct = errors.New("zcdb: expected a struct or struct pointer")
	// ErrNoFields 写入或 Pluck keyBy 映射时没有可用的导出字段（包括字段全为 nil/不可用）
	ErrNoFields = errors.New("zcdb: no usable exported fields for write or keyBy mapping")
	// ErrEmptyTable 未设置数据源表名
	ErrEmptyTable = errors.New("zcdb: table name is required")

	// ErrPluckDest Pluck 目标必须是非 nil 的切片指针或 map 指针；底层切片/map 可为 nil
	ErrPluckDest = errors.New("zcdb: Pluck dest must be a non-nil pointer to a slice or map")
	// ErrPluckColumns Pluck 列数不匹配：切片/keyBy map 需 1 列，标量 map 需 2 列
	ErrPluckColumns = errors.New("zcdb: Pluck requires 1 column for slice or keyBy map, or 2 columns for scalar map")

	// ErrDialectRequired DAO 层错误
	ErrDialectRequired = errors.New("zcdb: dialect is required")
	ErrUnknownDialect  = errors.New("zcdb: unknown dialect")
	ErrPoolRequired    = errors.New("zcdb: pool is required")

	// ErrPgUnionLockNotSupported 锁子句不支持错误
	ErrPgUnionLockNotSupported = errors.New("zcdb: PostgreSQL does not support LOCK with UNION queries")
	ErrSQLiteLockNotSupported  = errors.New("zcdb: SQLite does not support LOCK clauses")

	// ErrSchemaGrammarNotSupported SchemaInspector 只支持内置三方言的 Grammar（错误消息带出实际类型）
	ErrSchemaGrammarNotSupported = errors.New("zcdb: unsupported grammar type for schema inspection")

	// ErrInvalidOperator 运算符不在白名单内
	ErrInvalidOperator = errors.New("zcdb: invalid operator")

	// ErrDeleteWithoutWhere 无 WHERE 条件的 DELETE 被拒绝（防误操作全表删除）
	ErrDeleteWithoutWhere = errors.New("zcdb: DELETE without WHERE is not allowed, call Force() or add a Where condition")

	// ErrUpdateWithoutWhere 无 WHERE 条件的 UPDATE 被拒绝（防误操作全表更新）
	ErrUpdateWithoutWhere = errors.New("zcdb: UPDATE without WHERE is not allowed, call Force() or add a Where condition")

	// ErrNotPointer 迭代器错误
	ErrNotPointer          = errors.New("zcdb: dest must be a pointer")
	ErrNotStruct           = errors.New("zcdb: dest must be a pointer to struct")
	ErrCursorFieldNotFound = errors.New("zcdb: cursor column not found in dest struct")

	// ErrCursorFieldUnavailable 游标列字段不可用（如 nil 嵌入指针结构体）
	ErrCursorFieldUnavailable = errors.New("zcdb: cursor column field is unavailable (nil embedded pointer)")

	// ErrCursorColumnNull 游标列值为 NULL，无法继续游标分页
	ErrCursorColumnNull = errors.New("zcdb: cursor column value is NULL, cannot continue pagination")

	// ErrUpsertUniqueByRequired PostgreSQL/SQLite 方言的 Upsert 需要 uniqueBy 生成 ON CONFLICT 目标
	ErrUpsertUniqueByRequired = errors.New("zcdb: upsert requires uniqueBy columns for postgres and sqlite dialects")

	// ErrInvalidSubQuery WhereExists 等方法的 sub 参数类型非法（仅支持 *Builder 或 func(*Builder)）
	ErrInvalidSubQuery = errors.New("zcdb: sub must be a *Builder or func(*Builder)")

	// ErrCyclicQuery 查询构造器的子查询图存在环（自引用/互引用，如 TableSub 或 Union 传入自身），
	// 编译/克隆会无限递归直至栈溢出，故在编译前拒绝
	ErrCyclicQuery = errors.New("zcdb: cyclic subquery graph detected")

	// ErrInvalidWhereInValues JoinBuilder 的 WhereIn/WhereNotIn values 参数类型非法（仅支持 []any 或 *Builder）
	ErrInvalidWhereInValues = errors.New("zcdb: WhereIn values must be []any or *Builder")

	// ErrInvalidAggregate ToAggregate 的聚合函数非法（仅支持 MAX/MIN/SUM/AVG）
	ErrInvalidAggregate = errors.New("zcdb: aggregate must be one of MAX, MIN, SUM, AVG")

	// ErrIncrementColumns Increment/Decrement 的列/增量参数非法（extra 不成对、列名非 string、columns 与 amounts 不等长或为空）
	ErrIncrementColumns = errors.New("zcdb: Increment/Decrement requires non-empty paired columns and amounts")

	// ErrInsertUsingColumnMismatch InsertUsing/InsertOrIgnoreUsing 的目标列数与子查询 SELECT 列数不一致
	ErrInsertUsingColumnMismatch = errors.New("zcdb: InsertUsing columns count does not match subquery select columns count")

	// ErrDeleteJoinNoJoin DeleteJoin 要求至少一个 JOIN
	ErrDeleteJoinNoJoin = errors.New("zcdb: DeleteJoin requires at least one JOIN")

	// ErrRawPlaceholderMismatch CompileRaw 的原始 SQL 占位符数量与绑定参数数量不一致
	ErrRawPlaceholderMismatch = errors.New("zcdb: raw SQL placeholder count does not match args count")

	// ErrRawValueNotInlinable CompileRaw 的参数值无法内联成 SQL 字面量（类型不支持、NaN/Inf、Valuer 报错）
	ErrRawValueNotInlinable = errors.New("zcdb: raw SQL arg cannot be inlined as a SQL literal")

	// ErrDeleteWithJoin 普通 Delete/ToDelete 不编译 JOIN，带 JOIN 时拒绝以免 JOIN 限定被静默丢弃
	ErrDeleteWithJoin = errors.New("zcdb: Delete does not compile JOIN clauses, use DeleteJoin instead")

	// ErrTxOwnerMismatch ctx 携带的事务属于另一个连接池，当前 DAO 拒绝在其上执行
	ErrTxOwnerMismatch = errors.New("zcdb: context carries a transaction owned by another pool")

	// ErrAggregateGrouped 标量聚合（Max/Min/Sum/Avg/ToAggregate）不支持 GROUP BY 或 HAVING
	ErrAggregateGrouped = errors.New("zcdb: scalar aggregate does not support GROUP BY or HAVING, use Select with Find for per-group results")

	// ErrScanDest ScanStruct 的 dest 是指针但指向的类型非法（必须是 *struct 或 *[]struct / *[]*struct）
	ErrScanDest = errors.New("zcdb: dest must be a pointer to struct or slice")

	// ErrScanConvert 列值转换为目标字段类型失败（数值解析、JSON 反序列化、类型不可转换等）
	ErrScanConvert = errors.New("zcdb: scan value conversion failed")
)
