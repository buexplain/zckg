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

	// ErrNotPointer 迭代器错误
	ErrNotPointer          = errors.New("zcdb: dest must be a pointer")
	ErrNotStruct           = errors.New("zcdb: dest must be a pointer to struct")
	ErrCursorFieldNotFound = errors.New("zcdb: cursor column not found in dest struct")
)
