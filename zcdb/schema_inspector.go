package zcdb

import (
	"context"
	"fmt"
)

// exprColumnPlaceholder 是索引列元数据取不到列名时（表达式/函数索引）的占位符：
// MySQL 的函数索引 COLUMN_NAME 为 NULL、PostgreSQL 的 attnum 为 0、SQLite 的 index_info.name 为 NULL，
// 三方言统一呈现为该占位符（表达式文本不在基础元数据中，v1 不查询方言特有的表达式列）。
const exprColumnPlaceholder = "#expr"

// TableInfo 表元数据。
type TableInfo struct {
	Name    string // 表名
	Comment string // 表注释（SQLite 始终为空）
}

// ColumnInfo 字段元数据。
type ColumnInfo struct {
	Name     string // 字段名
	Type     string // 字段类型（如 "varchar(255)"、"integer"）
	Comment  string // 字段注释（SQLite 始终为空）
	Nullable bool   // 是否允许 NULL
	// Default 默认值（nil 表示无默认值）。
	// 字符串为方言原生格式，不做归一化：
	// MySQL 为裸值（active）；PostgreSQL 为表达式（'active'::character varying、nextval(...)）；
	// SQLite 为字面量（字符串带引号 'x'，数值为 0/1.5）。
	Default *string
	// PrimaryKey 是否主键列（联合主键的各列均为 true）。
	PrimaryKey bool
	// Extra MySQL information_schema 的 EXTRA 列原样（如 "auto_increment"、"DEFAULT_GENERATED"、
	// "on update CURRENT_TIMESTAMP"、"VIRTUAL GENERATED"、"STORED GENERATED"）；PostgreSQL/SQLite 恒为空。
	Extra string
}

// IndexInfo 索引元数据。
type IndexInfo struct {
	// Name 索引名。MySQL 主键索引固定为 "PRIMARY"，SQLite 的合成主键行同为 "PRIMARY"（该行不显示物理索引名），
	// PostgreSQL 为实际索引名（如 user_order_pkey）。
	Name string
	// Columns 索引列，按定义顺序；表达式列以 #expr 占位，MySQL 前缀索引为 col(n)（如 email(10)）。
	Columns []string
	Unique  bool // 是否唯一索引
	Primary bool // 是否主键索引
}

// SchemaInspector 数据库元数据查询接口。
type SchemaInspector interface {
	// Tables 返回当前数据库中所有用户表的名称和注释。
	Tables(ctx context.Context) ([]TableInfo, error)
	// Columns 返回指定表中所有字段的名称、类型、注释、是否可空、默认值、是否主键与 EXTRA。
	Columns(ctx context.Context, table string) ([]ColumnInfo, error)
	// Indexes 返回指定表的索引信息（主键、唯一、普通索引）。
	Indexes(ctx context.Context, table string) ([]IndexInfo, error)
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
