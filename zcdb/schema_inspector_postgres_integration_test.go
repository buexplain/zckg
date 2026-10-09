// 本文件为 PostgreSQL 集成测试：SchemaInspector 元数据查询（Tables / Columns / Indexes）。
// 用例按被测源码归集（schema_inspector.go 与 postgres_schema.go），不混入 Builder 的 builder_*_test.go；
// 建连统一走 testhelpers_postgres_test.go 的 openPgTestDB（容器不可达时 Skip）；
// Indexes 查询要求 PG >= 11（indnkeyatts 自 PG 11 引入）：
//
//	docker run -d --name zcdb_test_postgres -e POSTGRES_PASSWORD=root -p 5432:5432 postgres:15
package zcdb

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// ==================== Tables / Columns ====================

// TestPgInteg_SchemaInspector_Tables 验证 Tables 返回表名和注释。
func TestPgInteg_SchemaInspector_Tables(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, `CREATE TABLE "test_schema_a" ("id" INT NOT NULL PRIMARY KEY)`)
	mustExec(t, db, `COMMENT ON TABLE "test_schema_a" IS '表A注释'`)
	mustExec(t, db, `CREATE TABLE "test_schema_b" ("id" INT NOT NULL PRIMARY KEY)`)
	mustExec(t, db, `COMMENT ON TABLE "test_schema_b" IS '表B注释'`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS "test_schema_a"`)
		mustExec(t, db, `DROP TABLE IF EXISTS "test_schema_b"`)
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	tables, err := inspector.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}

	found := map[string]string{}
	for _, tbl := range tables {
		if tbl.Name == "test_schema_a" || tbl.Name == "test_schema_b" {
			found[tbl.Name] = tbl.Comment
		}
	}
	if len(found) != 2 {
		t.Fatalf("expected 2 test tables, got %d: %v", len(found), tables)
	}
	if found["test_schema_a"] != "表A注释" {
		t.Errorf("test_schema_a: expected '表A注释', got %q", found["test_schema_a"])
	}
	if found["test_schema_b"] != "表B注释" {
		t.Errorf("test_schema_b: expected '表B注释', got %q", found["test_schema_b"])
	}
}

// TestPgInteg_SchemaInspector_Columns 验证 Columns 返回字段名、类型、注释、Nullable、Default。
func TestPgInteg_SchemaInspector_Columns(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, `CREATE TABLE "test_columns" (
		"id" SERIAL PRIMARY KEY,
		"name" VARCHAR(64) NOT NULL,
		"age" INTEGER,
		"status" VARCHAR(16) NOT NULL DEFAULT 'active'
	)`)
	mustExec(t, db, `COMMENT ON TABLE "test_columns" IS '测试字段表'`)
	mustExec(t, db, `COMMENT ON COLUMN "test_columns"."name" IS '用户名'`)
	mustExec(t, db, `COMMENT ON COLUMN "test_columns"."age" IS '年龄'`)
	mustExec(t, db, `COMMENT ON COLUMN "test_columns"."status" IS '状态'`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS "test_columns"`)
	}()

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
		comment  string
		nullable bool
		hasDef   bool
		defVal   string
	}{
		{"id", "integer", "", false, true, ""},
		{"name", "character varying(64)", "用户名", false, false, ""},
		{"age", "integer", "年龄", true, false, ""},
		{"status", "character varying(16)", "状态", false, true, "'active'::character varying"},
	}
	for i, c := range checks {
		if columns[i].Name != c.name {
			t.Errorf("col[%d]: expected name %q, got %q", i, c.name, columns[i].Name)
		}
		if columns[i].Type != c.typ {
			t.Errorf("col[%d] %s: expected type %q, got %q", i, c.name, c.typ, columns[i].Type)
		}
		if columns[i].Comment != c.comment {
			t.Errorf("col[%d] %s: expected comment %q, got %q", i, c.name, c.comment, columns[i].Comment)
		}
		if columns[i].Nullable != c.nullable {
			t.Errorf("col[%d] %s: expected nullable=%v, got %v", i, c.name, c.nullable, columns[i].Nullable)
		}
		if c.hasDef && columns[i].Default == nil {
			t.Errorf("col[%d] %s: expected default, got nil", i, c.name)
		}
		if c.defVal != "" && columns[i].Default != nil && *columns[i].Default != c.defVal {
			t.Errorf("col[%d] %s: expected default %q, got %q", i, c.name, c.defVal, *columns[i].Default)
		}
	}
}

// TestPgInteg_SchemaInspector_NonexistentTable 边界固化（审查结论）：
// 不存在的表查 Columns 返回空切片与 nil 错误（pg_attribute 关联无命中行），
// 不报错也不 panic；调用方以空切片自行判断。
func TestPgInteg_SchemaInspector_NonexistentTable(t *testing.T) {
	db := openPgTestDB(t)
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

// TestPgInteg_SchemaInspector_ColumnsPrimaryKeyAndExtra 验证 PG 的主键标记与恒空的 Extra：
// 联合主键的各列 PrimaryKey 均为 true，普通列 false；PG 无 EXTRA 元数据列，Extra 恒为空
// （SERIAL 的自增语义经 Default 的 nextval(...) 表达式呈现，不在 Extra 中）。
func TestPgInteg_SchemaInspector_ColumnsPrimaryKeyAndExtra(t *testing.T) {
	db := openPgTestDB(t)
	// 建表前先清理：避免上次运行在 defer 前中断后残留表导致本次以「relation already exists」失败
	// （openPgTestDB 的 dropPgTables 只覆盖历史遗留表名）
	mustExec(t, db, `DROP TABLE IF EXISTS "test_pk_extra"`)
	mustExec(t, db, `DROP TABLE IF EXISTS "test_serial"`)
	mustExec(t, db, `CREATE TABLE "test_pk_extra" (
		"a" INT NOT NULL,
		"b" INT NOT NULL,
		"c" VARCHAR(16) NOT NULL,
		PRIMARY KEY ("a", "b")
	)`)
	mustExec(t, db, `CREATE TABLE "test_serial" (
		"id" SERIAL PRIMARY KEY,
		"name" TEXT NOT NULL
	)`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS "test_pk_extra"`)
		mustExec(t, db, `DROP TABLE IF EXISTS "test_serial"`)
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	columns, err := inspector.Columns(context.Background(), "test_pk_extra")
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	byName := make(map[string]ColumnInfo, len(columns))
	for _, c := range columns {
		byName[c.Name] = c
	}
	for _, name := range []string{"a", "b"} {
		got, ok := byName[name]
		if !ok {
			t.Errorf("缺少列 %s", name)
			continue
		}
		if !got.PrimaryKey {
			t.Errorf("联合主键列 %s 应标记 PrimaryKey", name)
		}
		if got.Extra != "" {
			t.Errorf("PG 的 Extra 应恒为空，列 %s 实际 %q", name, got.Extra)
		}
	}
	if got := byName["c"]; got.PrimaryKey {
		t.Errorf("非主键列 c 不应标记 PrimaryKey: %+v", got)
	}

	serialCols, err := inspector.Columns(context.Background(), "test_serial")
	if err != nil {
		t.Fatalf("Columns() error: %v", err)
	}
	for _, c := range serialCols {
		if c.Name != "id" {
			continue
		}
		if !c.PrimaryKey {
			t.Errorf("SERIAL 主键列 id 应标记 PrimaryKey")
		}
		if c.Extra != "" {
			t.Errorf("SERIAL 列的 Extra 应为空（自增经 Default 呈现），实际 %q", c.Extra)
		}
		if c.Default == nil || !strings.Contains(*c.Default, "nextval(") {
			t.Errorf("SERIAL 列的 Default 应为 nextval(...) 表达式，实际 %v", c.Default)
		}
	}
}

// TestPgInteg_SchemaInspector_Indexes 验证 PG 的索引查询：主键索引（名称为 xxx_pkey，不叫 PRIMARY）、
// 唯一索引、联合普通索引、表达式索引（attname 为 NULL → #expr）与 INCLUDE 列（不参与索引键，v1 不输出）。
func TestPgInteg_SchemaInspector_Indexes(t *testing.T) {
	db := openPgTestDB(t)
	// 建表前先清理（同上：dropPgTables 未覆盖该新表名）
	mustExec(t, db, `DROP TABLE IF EXISTS "test_indexes"`)
	mustExec(t, db, `CREATE TABLE "test_indexes" (
		"id" BIGINT NOT NULL,
		"email" VARCHAR(64) NOT NULL,
		"user_id" BIGINT NOT NULL,
		"status" VARCHAR(16) NOT NULL,
		PRIMARY KEY ("id")
	)`)
	mustExec(t, db, `CREATE UNIQUE INDEX "uk_email" ON "test_indexes" ("email")`)
	mustExec(t, db, `CREATE INDEX "idx_user_status" ON "test_indexes" ("user_id", "status")`)
	mustExec(t, db, `CREATE INDEX "idx_fn" ON "test_indexes" (lower("email"))`)
	// INCLUDE 列的索引键只有 user_id，status 仅随行存储
	mustExec(t, db, `CREATE INDEX "idx_incl" ON "test_indexes" ("user_id") INCLUDE ("status")`)
	defer func() {
		mustExec(t, db, `DROP TABLE IF EXISTS "test_indexes"`)
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
	var primaryColumns []string
	for _, idx := range indexes {
		byName[idx.Name] = idx
		if idx.Primary {
			primaryCount++
			primaryColumns = idx.Columns
		}
	}
	// 主键索引在 PG 中的名称是自动生成的（test_indexes_pkey），按标记而非名字定位
	if primaryCount != 1 {
		t.Errorf("应恰有 1 个主键索引，实际 %d: %v", primaryCount, indexes)
	}
	if !reflect.DeepEqual(primaryColumns, []string{"id"}) {
		t.Errorf("主键索引列应为 [id]，实际 %v", primaryColumns)
	}
	// 主键索引同时是唯一索引，且名称为 PG 自动生成的形态（如 test_indexes_pkey），
	// 不是 MySQL 风格的字面量 "PRIMARY"
	for _, idx := range indexes {
		if idx.Primary && (!idx.Unique || idx.Name == "PRIMARY") {
			t.Errorf("PG 主键索引应 Unique 且名称不是 %q，实际 %+v", "PRIMARY", idx)
		}
	}

	checks := []struct {
		name    string
		columns []string
		unique  bool
	}{
		{"uk_email", []string{"email"}, true},
		{"idx_user_status", []string{"user_id", "status"}, false},
		{"idx_fn", []string{"#expr"}, false},
		{"idx_incl", []string{"user_id"}, false},
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
	}
}

// TestPgInteg_SchemaInspector_IndexesNonexistentTable 边界固化：不存在的表查 Indexes
// 返回空切片与 nil 错误（pg_index 查询无命中行），与 Columns 的边界行为一致。
func TestPgInteg_SchemaInspector_IndexesNonexistentTable(t *testing.T) {
	db := openPgTestDB(t)
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
