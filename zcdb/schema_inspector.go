package zcdb

import (
	"context"
	"fmt"
)

// TableInfo 表元数据。
type TableInfo struct {
	Name    string // 表名
	Comment string // 表注释（SQLite 始终为空）
}

// ColumnInfo 字段元数据。
type ColumnInfo struct {
	Name     string  // 字段名
	Type     string  // 字段类型（如 "varchar(255)"、"integer"）
	Comment  string  // 字段注释（SQLite 始终为空）
	Nullable bool    // 是否允许 NULL
	Default  *string // 默认值（nil 表示无默认值）
}

// SchemaInspector 数据库元数据查询接口。
type SchemaInspector interface {
	// Tables 返回当前数据库中所有用户表的名称和注释。
	Tables(ctx context.Context) ([]TableInfo, error)
	// Columns 返回指定表中所有字段的名称、类型、注释、是否可空、默认值。
	Columns(ctx context.Context, table string) ([]ColumnInfo, error)
}

// NewSchemaInspector 根据 DBDao 的 Grammar 类型创建对应的 SchemaInspector。
func NewSchemaInspector(dao *DBDao) (SchemaInspector, error) {
	switch dao.grammar.(type) {
	case *MySQLGrammar:
		return &MySQLSchemaInspector{dao: dao}, nil
	case *PostgresGrammar:
		return &PostgresSchemaInspector{dao: dao}, nil
	case *SQLiteGrammar:
		return &SQLiteSchemaInspector{dao: dao}, nil
	default:
		return nil, fmt.Errorf("zcdb: unsupported grammar type for schema inspection: %T", dao.grammar)
	}
}
