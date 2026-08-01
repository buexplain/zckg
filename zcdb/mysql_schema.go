package zcdb

import (
	"context"
	"database/sql"
)

// MySQLSchemaInspector MySQL 数据库元数据查询。
type MySQLSchemaInspector struct {
	dao *DBDao
}

// Tables 查询当前数据库中所有用户表及其注释。
func (s *MySQLSchemaInspector) Tables(ctx context.Context) ([]TableInfo, error) {
	query := `SELECT TABLE_NAME, IFNULL(TABLE_COMMENT, '') FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`
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
func (s *MySQLSchemaInspector) Columns(ctx context.Context, table string) ([]ColumnInfo, error) {
	query := `SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(COLUMN_COMMENT, ''), IS_NULLABLE, COLUMN_DEFAULT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`
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
		var nullable string
		var defaultVal sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &c.Comment, &nullable, &defaultVal); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		if defaultVal.Valid {
			c.Default = &defaultVal.String
		}
		columns = append(columns, c)
	}
	return columns, rows.Err()
}
