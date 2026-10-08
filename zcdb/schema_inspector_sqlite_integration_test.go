// 本文件为 SQLite 集成测试：SchemaInspector 元数据查询（Tables / Columns / Indexes）。
// 用例按被测源码归集（schema_inspector.go 与 sqlite_schema.go），不混入 Builder 的 builder_*_test.go；
// SQLite 使用内嵌内存库（modernc.org/sqlite），无需外部环境，本文件的用例恒实跑（非 Skip）。
package zcdb

import (
	"context"
	"reflect"
	"testing"
)

// ==================== Tables / Columns ====================

// TestSQLiteInteg_SchemaInspector_Tables 验证 Tables 返回表名（注释始终为空）。
func TestSQLiteInteg_SchemaInspector_Tables(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	tables, err := inspector.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}

	// 应包含 users 和 orders 表
	found := map[string]string{}
	for _, tbl := range tables {
		found[tbl.Name] = tbl.Comment
	}
	if _, ok := found["users"]; !ok {
		t.Errorf("expected 'users' table, not found: %v", tables)
	}
	if _, ok := found["orders"]; !ok {
		t.Errorf("expected 'orders' table, not found: %v", tables)
	}
	// SQLite 不支持表注释，Comment 应为空
	if found["users"] != "" {
		t.Errorf("users: expected empty comment, got %q", found["users"])
	}
}

// TestSQLiteInteg_SchemaInspector_Columns 验证 Columns 返回字段名、类型（注释始终为空）。
func TestSQLiteInteg_SchemaInspector_Columns(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE test_columns (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT NOT NULL,
		age INTEGER,
		email TEXT DEFAULT 'none'
	)`)
	defer mustExec(t, db, `DROP TABLE IF EXISTS test_columns`)

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "test_columns")
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}

	checks := []struct {
		name     string
		typ      string
		nullable bool
		hasDef   bool
	}{
		// SQLite 的 INTEGER PRIMARY KEY 是 rowid 别名，PRAGMA 中 notnull=0
		{"id", "INTEGER", true, false},
		{"name", "TEXT", false, false},
		{"age", "INTEGER", true, false},
		{"email", "TEXT", true, true},
	}
	for i, c := range checks {
		if columns[i].Name != c.name {
			t.Errorf("col[%d]: expected name %q, got %q", i, c.name, columns[i].Name)
		}
		if columns[i].Type != c.typ {
			t.Errorf("col[%d] %s: expected type %q, got %q", i, c.name, c.typ, columns[i].Type)
		}
		if columns[i].Comment != "" {
			t.Errorf("col[%d] %s: expected empty comment, got %q", i, c.name, columns[i].Comment)
		}
		if columns[i].Nullable != c.nullable {
			t.Errorf("col[%d] %s: expected nullable=%v, got %v", i, c.name, c.nullable, columns[i].Nullable)
		}
		if c.hasDef && columns[i].Default == nil {
			t.Errorf("col[%d] %s: expected default, got nil", i, c.name)
		}
		if !c.hasDef && columns[i].Default != nil {
			t.Errorf("col[%d] %s: expected no default, got %q", i, c.name, *columns[i].Default)
		}
	}
}

// TestSQLiteInteg_SchemaInspector_ColumnsNumericDefault 验证 Columns 能处理数值默认值：
// PRAGMA table_info 的 dflt_value 对 DEFAULT 0/1.5 返回 INTEGER/REAL（int64/float64），
// 而非 TEXT，扫描到 *string 必须显式转换。
func TestSQLiteInteg_SchemaInspector_ColumnsNumericDefault(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE test_num_default (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			status INTEGER DEFAULT 0,
			score REAL DEFAULT 1.5,
			flag TEXT DEFAULT 'x'
		)`)
	defer mustExec(t, db, `DROP TABLE IF EXISTS test_num_default`)

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "test_num_default")
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	if len(columns) != 4 {
		t.Fatalf("expected 4 columns, got %d", len(columns))
	}

	checks := []struct {
		name     string
		hasDef   bool
		defValue string
	}{
		{"id", false, ""},
		{"status", true, "0"},
		{"score", true, "1.5"},
		{"flag", true, "'x'"},
	}
	for i, c := range checks {
		if c.hasDef && (columns[i].Default == nil || *columns[i].Default != c.defValue) {
			t.Errorf("col[%d] %s: expected default %q, got %v", i, c.name, c.defValue, columns[i].Default)
		}
		if !c.hasDef && columns[i].Default != nil {
			t.Errorf("col[%d] %s: expected no default, got %q", i, c.name, *columns[i].Default)
		}
	}
}

// TestSQLiteInteg_SchemaInspector_NonexistentTable 边界固化（审查结论）：
// 不存在的表查 Columns 返回空切片与 nil 错误（PRAGMA table_info 对未知表返回空结果集），
// 不报错也不 panic；调用方以空切片自行判断。
func TestSQLiteInteg_SchemaInspector_NonexistentTable(t *testing.T) {
	db := openSQLiteTestDB(t)
	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "no_such_table_zz")
	if err != nil {
		t.Fatalf("Columns() 对不存在的表应返回空结果, got error: %v", err)
	}
	if len(columns) != 0 {
		t.Errorf("expected empty columns, got %v", columns)
	}
}

// ==================== PrimaryKey / Extra / Indexes ====================

// TestSQLiteInteg_SchemaInspector_ColumnsPrimaryKey 验证 SQLite 的主键标记与恒空的 Extra：
// rowid 表的 INTEGER PRIMARY KEY（含 AUTOINCREMENT，PRAGMA 中 notnull=0）、TEXT 主键、
// 联合主键的各列均标记 PrimaryKey；无主键表不标记；EXTRA 恒为空。
func TestSQLiteInteg_SchemaInspector_ColumnsPrimaryKey(t *testing.T) {
	db := openSQLiteTestDB(t)
	tables := map[string]struct {
		ddl   string
		prims map[string]bool
	}{
		"test_pk_rowid": {
			ddl:   `CREATE TABLE test_pk_rowid (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL)`,
			prims: map[string]bool{"id": true, "name": false},
		},
		"test_pk_text": {
			ddl:   `CREATE TABLE test_pk_text (id TEXT PRIMARY KEY, name TEXT)`,
			prims: map[string]bool{"id": true, "name": false},
		},
		"test_pk_composite": {
			ddl:   `CREATE TABLE test_pk_composite (a INT NOT NULL, b INT NOT NULL, c TEXT, PRIMARY KEY (b, a))`,
			prims: map[string]bool{"a": true, "b": true, "c": false},
		},
		"test_pk_none": {
			ddl:   `CREATE TABLE test_pk_none (a TEXT, b TEXT)`,
			prims: map[string]bool{"a": false, "b": false},
		},
	}
	for name, tc := range tables {
		// 建表前先清理：避免上次运行在 t.Cleanup 前中断后残留表导致本次以「table already exists」失败
		mustExec(t, db, "DROP TABLE IF EXISTS "+name)
		mustExec(t, db, tc.ddl)
		tableName := name
		t.Cleanup(func() { mustExec(t, db, "DROP TABLE IF EXISTS "+tableName) })
	}

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	for name, tc := range tables {
		columns, err := inspector.Columns(context.Background(), name)
		if err != nil {
			t.Fatalf("Columns(%s) error: %v", name, err)
		}
		byName := make(map[string]ColumnInfo, len(columns))
		for _, c := range columns {
			byName[c.Name] = c
		}
		for col, wantPrimary := range tc.prims {
			got, ok := byName[col]
			if !ok {
				t.Errorf("表 %s 缺少列 %s", name, col)
				continue
			}
			if got.PrimaryKey != wantPrimary {
				t.Errorf("表 %s 列 %s: PrimaryKey 期望 %v，实际 %v", name, col, wantPrimary, got.PrimaryKey)
			}
			if got.Extra != "" {
				t.Errorf("表 %s 列 %s: SQLite 的 Extra 应恒为空，实际 %q", name, col, got.Extra)
			}
		}
	}
}

// TestSQLiteInteg_SchemaInspector_Indexes 验证 SQLite 两段式索引查询：
//   - 主键行由 table_info 的 pk 序合成（rowid 表的 INTEGER PRIMARY KEY 在 index_list 中没有对应行），
//     联合主键按 pk 值升序给列；
//   - origin='pk' 的自动索引被跳过（TEXT 主键触发的 sqlite_autoindex_*），不产生重复主键行；
//   - UNIQUE 约束的自动索引（origin='u'）保留，原样呈现其自动名；
//   - 表达式索引列以 #expr 占位；部分索引 v1 忽略谓词，按普通索引呈现。
func TestSQLiteInteg_SchemaInspector_Indexes(t *testing.T) {
	db := openSQLiteTestDB(t)
	// 建表前先清理（同上：避免上次运行中断后残留表导致本次失败）
	mustExec(t, db, `DROP TABLE IF EXISTS test_indexes`)
	mustExec(t, db, `DROP TABLE IF EXISTS test_indexes_textpk`)
	mustExec(t, db, `DROP TABLE IF EXISTS test_indexes_pk`)
	mustExec(t, db, `CREATE TABLE test_indexes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		email TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL,
		status TEXT NOT NULL
	)`)
	mustExec(t, db, `CREATE INDEX idx_user_status ON test_indexes (user_id, status)`)
	mustExec(t, db, `CREATE INDEX idx_expr ON test_indexes (lower(email))`)
	mustExec(t, db, `CREATE INDEX idx_partial ON test_indexes (status) WHERE user_id > 0`)
	// TEXT 主键：主键以 origin='pk' 的 sqlite_autoindex_* 出现在 index_list 中，须跳过以免与合成行重复
	mustExec(t, db, `CREATE TABLE test_indexes_textpk (id TEXT PRIMARY KEY, a TEXT)`)
	// 联合主键：合成行的列序按 pk 值升序（ddl 中写作 PRIMARY KEY (b, a) → b 在前）
	mustExec(t, db, `CREATE TABLE test_indexes_pk (a INT NOT NULL, b INT NOT NULL, c TEXT, PRIMARY KEY (b, a))`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS test_indexes`)
		mustExec(t, db, `DROP TABLE IF EXISTS test_indexes_textpk`)
		mustExec(t, db, `DROP TABLE IF EXISTS test_indexes_pk`)
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	indexes, err := inspector.Indexes(context.Background(), "test_indexes")
	if err != nil {
		t.Fatalf("Indexes() error: %v", err)
	}
	byName := make(map[string]IndexInfo, len(indexes))
	var primaryCount int
	for _, idx := range indexes {
		byName[idx.Name] = idx
		if idx.Primary {
			primaryCount++
		}
	}
	if primaryCount != 1 {
		t.Errorf("应恰有 1 个主键索引（合成行），实际 %d: %v", primaryCount, indexes)
	}

	checks := []struct {
		name    string
		columns []string
		unique  bool
		primary bool
	}{
		// rowid 表的 INTEGER PRIMARY KEY 无二级索引，此行由 table_info 合成
		{"PRIMARY", []string{"id"}, true, true},
		// UNIQUE 约束的自动索引：元数据中没有用户命名，原样呈现自动名
		{"sqlite_autoindex_test_indexes_1", []string{"email"}, true, false},
		{"idx_user_status", []string{"user_id", "status"}, false, false},
		{"idx_expr", []string{"#expr"}, false, false},
		// 部分索引：v1 忽略谓词（该索引只覆盖 user_id > 0 的行，此处呈现为普通索引）
		{"idx_partial", []string{"status"}, false, false},
	}
	for _, c := range checks {
		idx, ok := byName[c.name]
		if !ok {
			t.Errorf("缺少索引 %s（实际：%v）", c.name, indexes)
			continue
		}
		if !reflect.DeepEqual(idx.Columns, c.columns) {
			t.Errorf("索引 %s: Columns 期望 %v，实际 %v", c.name, c.columns, idx.Columns)
		}
		if idx.Unique != c.unique {
			t.Errorf("索引 %s: Unique 期望 %v，实际 %v", c.name, c.unique, idx.Unique)
		}
		if idx.Primary != c.primary {
			t.Errorf("索引 %s: Primary 期望 %v，实际 %v", c.name, c.primary, idx.Primary)
		}
	}

	// TEXT 主键表：origin='pk' 的自动索引被跳过，只剩合成的 PRIMARY 行
	textPK, err := inspector.Indexes(context.Background(), "test_indexes_textpk")
	if err != nil {
		t.Fatalf("Indexes() error: %v", err)
	}
	if len(textPK) != 1 || !textPK[0].Primary || !reflect.DeepEqual(textPK[0].Columns, []string{"id"}) {
		t.Errorf("TEXT 主键表应只剩 1 个合成主键行 [id]，实际 %+v", textPK)
	}

	// 联合主键表：合成行的列序按 pk 值升序（即 ddl 中的定义顺序 b, a）
	composite, err := inspector.Indexes(context.Background(), "test_indexes_pk")
	if err != nil {
		t.Fatalf("Indexes() error: %v", err)
	}
	if len(composite) != 1 || !composite[0].Primary || !reflect.DeepEqual(composite[0].Columns, []string{"b", "a"}) {
		t.Errorf("联合主键表应只剩 1 个合成主键行 [b a]，实际 %+v", composite)
	}
}

// TestSQLiteInteg_SchemaInspector_IndexesNoPrimaryKey 边界固化：无主键且无二级索引的 rowid 表，
// Indexes 返回空切片与 nil 错误（table_info 没有 pk 列可合成、index_list 无行），
// 不报错也不 panic；调用方以空切片判断「该表没有索引信息」。
func TestSQLiteInteg_SchemaInspector_IndexesNoPrimaryKey(t *testing.T) {
	db := openSQLiteTestDB(t)
	const table = "test_no_index"
	mustExec(t, db, "DROP TABLE IF EXISTS "+table)
	mustExec(t, db, "CREATE TABLE "+table+" (a TEXT, b INTEGER)")
	defer mustExec(t, db, "DROP TABLE IF EXISTS "+table)

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	indexes, err := inspector.Indexes(context.Background(), table)
	if err != nil {
		t.Fatalf("Indexes() 对无索引表应返回空结果, got error: %v", err)
	}
	if len(indexes) != 0 {
		t.Errorf("expected empty indexes, got %v", indexes)
	}
	// 同一张表的主键标记也应全为 false（无主键）
	columns, err := inspector.Columns(context.Background(), table)
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	for _, c := range columns {
		if c.PrimaryKey {
			t.Errorf("列 %s 不应标记为主键", c.Name)
		}
	}
}

// TestSQLiteInteg_SchemaInspector_IndexesNonexistentTable 边界固化：不存在的表查 Indexes
// 返回空切片与 nil 错误（PRAGMA 对未知表返回空结果集：主键合成为 nil、index_list 无行），
// 与 Columns 的边界行为一致。
func TestSQLiteInteg_SchemaInspector_IndexesNonexistentTable(t *testing.T) {
	db := openSQLiteTestDB(t)
	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	indexes, err := inspector.Indexes(context.Background(), "no_such_table_zz")
	if err != nil {
		t.Fatalf("Indexes() 对不存在的表应返回空结果, got error: %v", err)
	}
	if len(indexes) != 0 {
		t.Errorf("expected empty indexes, got %v", indexes)
	}
}

// ==================== NewSchemaInspector 构造（原 pool 文件迁入） ====================

// TestSQLiteInteg_SchemaInspector 验证 SQLite SchemaInspector 的 Tables/Columns 查询。
func TestSQLiteInteg_SchemaInspector(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	inspector, err := NewSchemaInspector(db)
	if err != nil {
		t.Fatalf("NewSchemaInspector: %v", err)
	}
	sqliteInsp, ok := inspector.(*SQLiteSchemaInspector)
	if !ok {
		t.Fatalf("expected *SQLiteSchemaInspector, got %T", inspector)
	}

	tables, err := sqliteInsp.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables: %v", err)
	}
	found := false
	for _, tb := range tables {
		if tb.Name == "users" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected users table in Tables result, got %+v", tables)
	}

	cols, err := sqliteInsp.Columns(context.Background(), "users")
	if err != nil {
		t.Fatalf("Columns: %v", err)
	}
	if len(cols) == 0 {
		t.Fatal("expected columns for users table, got empty")
	}
	byName := map[string]ColumnInfo{}
	for _, c := range cols {
		byName[c.Name] = c
	}
	if _, ok := byName["id"]; !ok {
		t.Fatalf("expected id column, got %+v", cols)
	}
	if _, ok := byName["name"]; !ok {
		t.Fatalf("expected name column, got %+v", cols)
	}
}
