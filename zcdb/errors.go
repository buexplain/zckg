package zcdb

import "errors"

var (
	ErrEmptyData     = errors.New("zcdb: data is empty")
	ErrInvalidStruct = errors.New("zcdb: expected a struct or struct pointer")
	ErrNoFields      = errors.New("zcdb: struct has no exportable fields")
	ErrEmptyTable    = errors.New("zcdb: table name is required")
)
