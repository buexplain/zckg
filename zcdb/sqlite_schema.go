package zcdb

import (
	"context"
	"database/sql"
)

// SQLiteSchemaInspector SQLite 数据库元数据查询。
// SQLite 不支持表注释和字段注释，Comment 字段始终为空。
type SQLiteSchemaInspector struct {
	dao *DBDao
}

// Tables 查询所有用户表（注释始终为空）。
func (s *SQLiteSchemaInspector) Tables(ctx context.Context) ([]TableInfo, error) {
	query := `SELECT "name", '' FROM "sqlite_master" WHERE "type" = 'table' AND "name" NOT LIKE 'sqlite_%' ORDER BY "name"`
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

// Columns 通过 PRAGMA table_info 查询字段信息（注释始终为空）。
// 表名通过 WrapTable 引用后拼接到 SQL，防止注入。
func (s *SQLiteSchemaInspector) Columns(ctx context.Context, table string) ([]ColumnInfo, error) {
	wrappedTable := s.dao.grammar.WrapTable(table)
	query := "PRAGMA table_info(" + wrappedTable + ")"
	rows, err := s.dao.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var columns []ColumnInfo
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		columns = append(columns, ColumnInfo{
			Name:     name,
			Type:     colType,
			Comment:  "",
			Nullable: notNull == 0,
			Default:  dfltValue,
		})
	}
	return columns, rows.Err()
}
