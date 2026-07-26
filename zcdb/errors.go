package zcdb

import "errors"

var (
	ErrEmptyData     = errors.New("zcdb: data is empty")
	ErrInvalidStruct = errors.New("zcdb: expected a struct or struct pointer")
	ErrNoFields      = errors.New("zcdb: struct has no exportable fields")
	ErrEmptyTable    = errors.New("zcdb: table name is required")

	// DAO 层错误
	ErrDialectRequired = errors.New("zcdb: dialect is required")
	ErrUnknownDialect  = errors.New("zcdb: unknown dialect")
	ErrPoolRequired    = errors.New("zcdb: pool is required")
	ErrNoRows          = errors.New("zcdb: no rows in result set")
)
