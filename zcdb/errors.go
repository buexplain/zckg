package zcdb

import "errors"

var (
	// ErrEmptyData 插入数据为空切片
	ErrEmptyData = errors.New("zcdb: data is empty")
	// ErrInvalidStruct 数据不是结构体/结构体指针（或它们的切片）
	ErrInvalidStruct = errors.New("zcdb: expected a struct or struct pointer")
	// ErrNoFields 写入或 KeyBy 映射时没有可用的导出字段（包括字段全为 nil/不可用）
	ErrNoFields = errors.New("zcdb: no usable exported fields for write or keyBy mapping")
	// ErrEmptyTable 未设置数据源表名
	ErrEmptyTable = errors.New("zcdb: table name is required")

	// ErrPluckDest Pluck 目标必须是非 nil 的切片指针；底层切片可为 nil。
	// map 目标请用 PluckMap（标量值）或 KeyBy（结构体值）
	ErrPluckDest = errors.New("zcdb: Pluck dest must be a non-nil pointer to a slice (use PluckMap or KeyBy for map destinations)")
	// ErrPluckMapDest PluckMap 目标必须是非 nil 的 map 指针，且键/值类型不能是结构体；
	// 值类型为结构体/结构体指针时请用 KeyBy
	ErrPluckMapDest = errors.New("zcdb: PluckMap dest must be a non-nil pointer to a map with non-struct key and value types (use KeyBy for struct values)")
	// ErrKeyByDest KeyBy 目标必须是非 nil 的 map 指针，且值类型为结构体或结构体指针
	ErrKeyByDest = errors.New("zcdb: KeyBy dest must be a non-nil pointer to a map with struct or struct pointer values")

	// ErrDialectRequired DAO 层错误
	ErrDialectRequired = errors.New("zcdb: dialect is required")
	ErrUnknownDialect  = errors.New("zcdb: unknown dialect")
	ErrPoolRequired    = errors.New("zcdb: pool is required")

	// ErrDriverNameRequired NewPool 未提供 DriverName
	ErrDriverNameRequired = errors.New("zcdb: DriverName is required")
	// ErrDSNRequired NewPool 未提供主库 DSN
	ErrDSNRequired = errors.New("zcdb: DSN is required")
	// ErrPoolClosed 连接池已关闭：Close 后 AddSlave 返回本错误（Close 本身幂等返回 nil）
	ErrPoolClosed = errors.New("zcdb: pool is closed")
	// ErrPoolConnect 主库/从库的打开或 Ping 验证失败；底层驱动错误以 %w 包装，仍可用 errors.Is 匹配
	ErrPoolConnect = errors.New("zcdb: pool connect failed")

	// ErrTxBegin 开启事务失败；ErrTxCommit 提交失败；ErrTxRollback 回滚失败。
	// 三者均以 %w 包装底层错误，原始错误仍可 errors.Is 匹配。
	ErrTxBegin    = errors.New("zcdb: begin transaction failed")
	ErrTxCommit   = errors.New("zcdb: commit failed")
	ErrTxRollback = errors.New("zcdb: rollback failed")

	// ErrInsertGetIdUnsupported PostgreSQL 方言不支持 InsertGetId（lib/pq 无 LastInsertId）
	ErrInsertGetIdUnsupported = errors.New("zcdb: InsertGetId is not supported on postgres dialect")

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

	// ErrWriteOrderLimitUnsupported 写语句携带 ORDER BY/LIMIT，但当前方言或语句形态不支持：
	// PostgreSQL/SQLite 的 UPDATE/DELETE 没有该语法（忽略子句会把写范围放大到全部命中行），
	// MySQL 的多表 UPDATE/DeleteJoin 会被引擎以 Error 1221 拒绝。编译期直接返回本错误，
	// 避免「静默全量」或执行期才失败；MySQL 单表 UPDATE/DELETE 不受影响。
	ErrWriteOrderLimitUnsupported = errors.New("zcdb: ORDER BY/LIMIT is not supported on this write statement")

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
