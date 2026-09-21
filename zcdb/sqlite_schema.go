package zcdb

import (
	"context"
	"database/sql"
	"sort"
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

// Columns 通过 PRAGMA table_info 查询字段信息（注释始终为空，EXTRA 恒为空）。
// 表名通过 WrapTable 引用后拼接到 SQL，防止注入。pk 列为联合主键时的列序（1,2,3…，0 为非主键），
// 此处仅用于判定是否主键；SQLite 的 INTEGER PRIMARY KEY 是 rowid 别名，PRAGMA 中 notnull=0
// （Nullable 为 true，非元数据错误），AUTOINCREMENT 关键字不在 table_info 中。
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
			Name:       name,
			Type:       colType,
			Comment:    "",
			Nullable:   notNull == 0,
			Default:    dfltValue,
			PrimaryKey: pk > 0,
		})
	}
	return columns, rows.Err()
}

// sqliteIndexEntry 是 PRAGMA index_list 的一行（seq 由结果集顺序隐含）。
type sqliteIndexEntry struct {
	name   string
	unique bool
	origin string
}

// sqlitePrimaryIndex 从 PRAGMA table_info 的 pk 序合成主键索引行（无主键表返回 nil）：
// rowid 表的 INTEGER PRIMARY KEY（含 AUTOINCREMENT）是 rowid 别名、无需二级索引，
// index_list 中本就没有对应行，故主键行只能由此合成；TEXT 主键、联合主键与 WITHOUT ROWID 表
// 的主键会以 origin='pk' 的 sqlite_autoindex_* 出现在 index_list 中，由 Indexes 跳过以免重复。
// 合成行的 Name 取 PRIMARY（与 MySQL 一致），Columns 按 pk 值升序给出主键列序。
func (s *SQLiteSchemaInspector) sqlitePrimaryIndex(ctx context.Context, wrappedTable string) (*IndexInfo, error) {
	rows, err := s.dao.Query(ctx, "PRAGMA table_info("+wrappedTable+")")
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	type primaryColumn struct {
		name string
		pk   int
	}
	var pkColumns []primaryColumn
	for rows.Next() {
		var cid int
		var name, colType string
		var notNull, pk int
		var dfltValue *string
		if err := rows.Scan(&cid, &name, &colType, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		if pk > 0 {
			pkColumns = append(pkColumns, primaryColumn{name: name, pk: pk})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(pkColumns) == 0 {
		return nil, nil
	}
	sort.Slice(pkColumns, func(i, j int) bool { return pkColumns[i].pk < pkColumns[j].pk })
	cols := make([]string, 0, len(pkColumns))
	for _, c := range pkColumns {
		cols = append(cols, c.name)
	}
	return &IndexInfo{Name: mysqlPrimaryIndexName, Columns: cols, Unique: true, Primary: true}, nil
}

// sqliteIndexList 读取 PRAGMA index_list（seq/name/unique/origin/partial）。
// partial（部分索引）v1 忽略：其谓词不在基础元数据中，索引行按普通索引呈现。
func (s *SQLiteSchemaInspector) sqliteIndexList(ctx context.Context, wrappedTable string) ([]sqliteIndexEntry, error) {
	rows, err := s.dao.Query(ctx, "PRAGMA index_list("+wrappedTable+")")
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var entries []sqliteIndexEntry
	for rows.Next() {
		var seq, unique, partial int
		var name, origin string
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		entries = append(entries, sqliteIndexEntry{name: name, unique: unique == 1, origin: origin})
	}
	return entries, rows.Err()
}

// sqliteIndexColumns 读取 PRAGMA index_info 的列清单，按 seqno 顺序返回。
// 表达式列的 name 为 NULL（cid 为负，实测 -2）：以 name 是否为空为权威判定，
// 不按 cid 的具体负值匹配，统一以 #expr 占位。
func (s *SQLiteSchemaInspector) sqliteIndexColumns(ctx context.Context, wrappedIndex string) ([]string, error) {
	rows, err := s.dao.Query(ctx, "PRAGMA index_info("+wrappedIndex+")")
	if err != nil {
		return nil, err
	}
	defer func(rows *sql.Rows) {
		_ = rows.Close()
	}(rows)

	var cols []string
	for rows.Next() {
		var seqno, cid int
		var name sql.NullString
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		col := exprColumnPlaceholder
		if name.Valid {
			col = name.String
		}
		cols = append(cols, col)
	}
	return cols, rows.Err()
}

// Indexes 查询指定表的索引信息（主键、唯一、普通索引）。
// 两段式实现：主键行由 table_info 的 pk 序合成，其余索引走 index_list + 逐索引 index_info。
// 索引名经 WrapTable 同款包裹后拼接，防止注入。
// 注意：每个 PRAGMA 查询都在独立 helper 内关闭结果集，避免在结果集未关闭时发起下一个查询
// （连接池 MaxOpenConns 为 1 时嵌套查询会互相等待）。
func (s *SQLiteSchemaInspector) Indexes(ctx context.Context, table string) ([]IndexInfo, error) {
	wrappedTable := s.dao.grammar.WrapTable(table)

	primary, err := s.sqlitePrimaryIndex(ctx, wrappedTable)
	if err != nil {
		return nil, err
	}
	entries, err := s.sqliteIndexList(ctx, wrappedTable)
	if err != nil {
		return nil, err
	}

	var indexes []IndexInfo
	if primary != nil {
		indexes = append(indexes, *primary)
	}
	for _, entry := range entries {
		// origin='pk' 的自动索引：其信息已由合成的主键行覆盖
		if entry.origin == "pk" {
			continue
		}
		cols, err := s.sqliteIndexColumns(ctx, s.dao.grammar.WrapTable(entry.name))
		if err != nil {
			return nil, err
		}
		indexes = append(indexes, IndexInfo{Name: entry.name, Columns: cols, Unique: entry.unique})
	}
	return indexes, nil
}
