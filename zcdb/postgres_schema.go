package zcdb

import (
	"context"
	"database/sql"
)

// PostgresSchemaInspector PostgreSQL 数据库元数据查询。
type PostgresSchemaInspector struct {
	dao *DBDao
}

// Tables 查询 public schema 中所有用户表及其注释。
func (s *PostgresSchemaInspector) Tables(ctx context.Context) ([]TableInfo, error) {
	query := `SELECT c.relname, COALESCE(obj_description(c.oid, 'pg_class'), '') FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE c.relkind = 'r' AND n.nspname = 'public' ORDER BY c.relname`
	rows, err := s.dao.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Comment); err != nil {
			return nil, err
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

// Columns 查询指定表中所有字段的名称、类型、注释、是否可空、默认值。
func (s *PostgresSchemaInspector) Columns(ctx context.Context, table string) ([]ColumnInfo, error) {
	query := `SELECT a.attname, format_type(a.atttypid, a.atttypmod), COALESCE(col_description(c.oid, a.attnum), ''), NOT a.attnotnull, pg_get_expr(d.adbin, d.adrelid) FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum WHERE c.relname = $1 AND n.nspname = 'public' AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`
	rows, err := s.dao.Query(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var columns []ColumnInfo
	for rows.Next() {
		var c ColumnInfo
		var defaultVal sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &c.Comment, &c.Nullable, &defaultVal); err != nil {
			return nil, err
		}
		if defaultVal.Valid {
			c.Default = &defaultVal.String
		}
		columns = append(columns, c)
	}
	return columns, rows.Err()
}
