package zcdb

import "errors"

var (
	ErrEmptyData     = errors.New("zcdb: data is empty")
	ErrInvalidStruct = errors.New("zcdb: expected a struct or struct pointer")
	ErrNoFields      = errors.New("zcdb: struct has no exportable fields")
	ErrEmptyTable    = errors.New("zcdb: table name is required")

	// ErrPluckDest Pluck 目标必须是切片指针或 map 指针
	ErrPluckDest = errors.New("zcdb: Pluck dest must be a pointer to a non-nil slice or map")
	// ErrPluckColumns Pluck 列数不匹配：切片目标需 1 列，map 目标需 2 列
	ErrPluckColumns = errors.New("zcdb: Pluck requires exactly 1 column for slice dest or exactly 2 columns for map dest")

	// ErrDialectRequired DAO 层错误
	ErrDialectRequired = errors.New("zcdb: dialect is required")
	ErrUnknownDialect  = errors.New("zcdb: unknown dialect")
	ErrPoolRequired    = errors.New("zcdb: pool is required")

	// ErrPgUnionLockNotSupported 锁子句不支持错误
	ErrPgUnionLockNotSupported = errors.New("zcdb: PostgreSQL does not support LOCK with UNION queries")
	ErrSQLiteLockNotSupported  = errors.New("zcdb: SQLite does not support LOCK clauses")

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

	// ErrInvalidAggregate ToAggregate 的聚合函数非法（仅支持 MAX/MIN/SUM/AVG）
	ErrInvalidAggregate = errors.New("zcdb: aggregate must be one of MAX, MIN, SUM, AVG")

	// ErrIncrementColumns Increment/Decrement 的 extra 参数必须成对（column, amount, ...）
	ErrIncrementColumns = errors.New("zcdb: Increment/Decrement extra args must be paired as (column, amount)")

	// ErrDeleteJoinNoJoin DeleteJoin 要求至少一个 JOIN
	ErrDeleteJoinNoJoin = errors.New("zcdb: DeleteJoin requires at least one JOIN")
)
