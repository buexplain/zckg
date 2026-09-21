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

// Columns 查询指定表中所有字段的名称、类型、注释、是否可空、默认值与是否主键。
// is_primary 用 EXISTS 子查询判定（pg_index.indkey 为 int2vector，可直接用 ANY 比较）；
// PostgreSQL 无 MySQL 的 EXTRA 元数据列，ColumnInfo.Extra 恒为空
// （serial/bigserial 的自增语义经 Default 的 nextval(...) 表达式呈现）。
func (s *PostgresSchemaInspector) Columns(ctx context.Context, table string) ([]ColumnInfo, error) {
	query := `SELECT a.attname, format_type(a.atttypid, a.atttypmod), COALESCE(col_description(c.oid, a.attnum), ''), NOT a.attnotnull, pg_get_expr(d.adbin, d.adrelid), EXISTS (SELECT 1 FROM pg_catalog.pg_index ix WHERE ix.indrelid = c.oid AND ix.indisprimary AND a.attnum = ANY(ix.indkey)) FROM pg_catalog.pg_attribute a JOIN pg_catalog.pg_class c ON c.oid = a.attrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace LEFT JOIN pg_catalog.pg_attrdef d ON d.adrelid = c.oid AND d.adnum = a.attnum WHERE c.relname = $1 AND n.nspname = 'public' AND a.attnum > 0 AND NOT a.attisdropped ORDER BY a.attnum`
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
		if err := rows.Scan(&c.Name, &c.Type, &c.Comment, &c.Nullable, &defaultVal, &c.PrimaryKey); err != nil {
			return nil, err
		}
		if defaultVal.Valid {
			c.Default = &defaultVal.String
		}
		columns = append(columns, c)
	}
	return columns, rows.Err()
}

// Indexes 查询指定表的索引信息（主键、唯一、普通索引），一条查询聚合列并按定义顺序排列。
// unnest(ix.indkey) WITH ORDINALITY 给出列序（indkey 为 int2vector，可直接 unnest）；
// k.ord <= ix.indnkeyatts 过滤 INCLUDE 列（PG 11+ 起 INCLUDE 列不参与索引键，v1 不输出，
// 该列自 PG 11 引入，故本查询要求 PG ≥ 11）；表达式列的 attname 为 NULL，以 #expr 占位。
func (s *PostgresSchemaInspector) Indexes(ctx context.Context, table string) ([]IndexInfo, error) {
	query := `SELECT i.relname, ix.indisunique, ix.indisprimary, a.attname, k.ord FROM pg_catalog.pg_index ix JOIN pg_catalog.pg_class c ON c.oid = ix.indrelid JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid CROSS JOIN LATERAL unnest(ix.indkey) WITH ORDINALITY AS k(attnum, ord) LEFT JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum WHERE n.nspname = 'public' AND c.relname = $1 AND k.ord <= ix.indnkeyatts ORDER BY i.relname, k.ord`
	rows, err := s.dao.Query(ctx, query, table)
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var indexes []IndexInfo
	positions := make(map[string]int) // 索引名 → indexes 中的下标
	for rows.Next() {
		var name string
		var unique, primary bool
		var column sql.NullString
		var ord int
		if err := rows.Scan(&name, &unique, &primary, &column, &ord); err != nil {
			return nil, err
		}
		pos, ok := positions[name]
		if !ok {
			indexes = append(indexes, IndexInfo{Name: name, Unique: unique, Primary: primary})
			pos = len(indexes) - 1
			positions[name] = pos
		}
		col := exprColumnPlaceholder
		if column.Valid {
			col = column.String
		}
		indexes[pos].Columns = append(indexes[pos].Columns, col)
	}
	return indexes, rows.Err()
}
