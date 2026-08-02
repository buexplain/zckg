package zcdb

import "errors"

var (
	ErrEmptyData     = errors.New("zcdb: data is empty")
	ErrInvalidStruct = errors.New("zcdb: expected a struct or struct pointer")
	ErrNoFields      = errors.New("zcdb: struct has no exportable fields")
	ErrEmptyTable    = errors.New("zcdb: table name is required")

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
)
