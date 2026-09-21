package zcdb

import (
	"context"
	"database/sql"
	"fmt"
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

// Columns 查询指定表中所有字段的名称、类型、注释、是否可空、默认值、是否主键与 EXTRA。
// 列名为 EXTRA（非 COLUMN_EXTRA），并用 IFNULL 兜底可能为 NULL 的元数据列。
func (s *MySQLSchemaInspector) Columns(ctx context.Context, table string) ([]ColumnInfo, error) {
	query := `SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(COLUMN_COMMENT, ''), IS_NULLABLE, COLUMN_DEFAULT, IFNULL(COLUMN_KEY, ''), IFNULL(EXTRA, '') FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`
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
		var columnKey string
		if err := rows.Scan(&c.Name, &c.Type, &c.Comment, &nullable, &defaultVal, &columnKey, &c.Extra); err != nil {
			return nil, err
		}
		c.Nullable = nullable == "YES"
		c.PrimaryKey = columnKey == "PRI"
		if defaultVal.Valid {
			c.Default = &defaultVal.String
		}
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

// mysqlPrimaryIndexName 是 MySQL 主键索引的名称（information_schema.STATISTICS 中的固定值）。
const mysqlPrimaryIndexName = "PRIMARY"

// Indexes 查询指定表的索引信息（主键、唯一、普通索引），列按 SEQ_IN_INDEX 顺序聚合。
// 前缀索引渲染为 col(n)；函数索引的 COLUMN_NAME 为 NULL，以 #expr 占位（不查询 EXPRESSION 列，
// 该列自 MySQL 8.0.13 才存在，直接引用在旧版本上报错）。
func (s *MySQLSchemaInspector) Indexes(ctx context.Context, table string) ([]IndexInfo, error) {
	query := `SELECT INDEX_NAME, COLUMN_NAME, SUB_PART, NON_UNIQUE FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY INDEX_NAME, SEQ_IN_INDEX`
	rows, err := s.dao.Query(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var indexes []IndexInfo
	positions := make(map[string]int) // 索引名 → indexes 中的下标（同名行按 SEQ_IN_INDEX 顺序聚合列）
	for rows.Next() {
		var name string
		var column sql.NullString
		var subPart sql.NullInt64
		var nonUnique int
		if err := rows.Scan(&name, &column, &subPart, &nonUnique); err != nil {
			return nil, err
		}
		col := exprColumnPlaceholder
		if column.Valid {
			col = column.String
		}
		if subPart.Valid {
			col = fmt.Sprintf("%s(%d)", col, subPart.Int64)
		}
		pos, ok := positions[name]
		if !ok {
			indexes = append(indexes, IndexInfo{Name: name, Primary: name == mysqlPrimaryIndexName, Unique: nonUnique == 0})
			pos = len(indexes) - 1
			positions[name] = pos
		}
		indexes[pos].Columns = append(indexes[pos].Columns, col)
	}
	return indexes, rows.Err()
}
