// 本文件为 MySQL 集成测试：SchemaInspector 元数据查询（Tables / Columns / Indexes）。
// 用例按被测源码归集（schema_inspector.go 与 mysql_schema.go），不混入 Builder 的 builder_*_test.go；
// 建连统一走 testhelpers_mysql_test.go 的 openMySQLTestDB（容器不可达时 Skip）：
//   docker run -d -p 3306:3306 -e MYSQL_ROOT_PASSWORD=root --name zcdb_test_mysql mysql:8.4
package zcdb

import (
	"context"
	"reflect"
	"testing"
)

// ==================== Tables / Columns ====================

// TestMySQLInteg_SchemaInspector_Tables 验证 Tables 返回表名和注释。
func TestMySQLInteg_SchemaInspector_Tables(t *testing.T) {
	db := openMySQLTestDB(t)
	// 创建带注释的表
	mustExec(t, db, "CREATE TABLE `test_schema_a` (`id` INT NOT NULL PRIMARY KEY) ENGINE=InnoDB COMMENT='表A注释'")
	mustExec(t, db, "CREATE TABLE `test_schema_b` (`id` INT NOT NULL PRIMARY KEY) ENGINE=InnoDB COMMENT='表B注释'")
	defer func() {
		mustExec(t, db, "DROP TABLE IF EXISTS `test_schema_a`")
		mustExec(t, db, "DROP TABLE IF EXISTS `test_schema_b`")
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	tables, err := inspector.Tables(context.Background())
	if err != nil {
		t.Fatalf("Tables() error: %v", err)
	}

	// 找到我们创建的表
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

// TestMySQLInteg_SchemaInspector_Columns 验证 Columns 返回字段名、类型、注释、Nullable、Default。
func TestMySQLInteg_SchemaInspector_Columns(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, `CREATE TABLE `+"`test_columns`"+` (
		`+"`id`"+` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		`+"`name`"+` VARCHAR(64) NOT NULL COMMENT '用户名',
		`+"`age`"+` INT NULL COMMENT '年龄',
		`+"`status`"+` VARCHAR(16) NOT NULL DEFAULT 'active' COMMENT '状态'
	) ENGINE=InnoDB COMMENT='测试字段表'`)
	defer func() {
		mustExec(t, db, "DROP TABLE IF EXISTS `test_columns`")
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

	// 验证每个字段
	checks := []struct {
		name     string
		typ      string
		comment  string
		nullable bool
		hasDef   bool
		defVal   string
	}{
		{"id", "bigint unsigned", "", false, false, ""},
		{"name", "varchar(64)", "用户名", false, false, ""},
		{"age", "int", "年龄", true, false, ""},
		{"status", "varchar(16)", "状态", false, true, "active"},
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
		if c.hasDef {
			if columns[i].Default == nil {
				t.Errorf("col[%d] %s: expected default %q, got nil", i, c.name, c.defVal)
			} else if *columns[i].Default != c.defVal {
				t.Errorf("col[%d] %s: expected default %q, got %q", i, c.name, c.defVal, *columns[i].Default)
			}
		} else if columns[i].Default != nil && c.name != "id" {
			// id 有默认值 '' (空字符串)，跳过检查
			t.Errorf("col[%d] %s: expected no default, got %q", i, c.name, *columns[i].Default)
		}
	}
}

// TestMySQLInteg_SchemaInspector_NonexistentTable 边界固化（审查结论）：
// 不存在的表查 Columns 返回空切片与 nil 错误（information_schema 查询无命中行），
// 不报错也不 panic；调用方以空切片自行判断。
func TestMySQLInteg_SchemaInspector_NonexistentTable(t *testing.T) {
	db := openMySQLTestDB(t)
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

// TestMySQLInteg_SchemaInspector_ColumnsPrimaryKeyAndExtra 验证 Columns 的主键标记与 EXTRA 透传：
// 联合主键的各列 PrimaryKey 均为 true；EXTRA 原样给出 information_schema 的值
// （自增小写形态、DEFAULT_GENERATED 与 on update CURRENT_TIMESTAMP 共存、生成列的 STORED GENERATED），
// 非生成/非自增列与默认值列不为 EXTRA 所影响；PG/SQLite 无此列，恒为空。
func TestMySQLInteg_SchemaInspector_ColumnsPrimaryKeyAndExtra(t *testing.T) {
	db := openMySQLTestDB(t)
	// 建表前先清理：避免上次运行在 defer 前中断（进程被杀/panic）后残留表导致本次以
	// 「table already exists」失败（openMySQLTestDB 的 dropMySQLTables 只覆盖历史遗留表名）
	mustExec(t, db, "DROP TABLE IF EXISTS `test_pk_extra`")
	mustExec(t, db, `CREATE TABLE `+"`test_pk_extra`"+` (
		`+"`id`"+` BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
		`+"`tenant_id`"+` BIGINT NOT NULL,
		`+"`name`"+` VARCHAR(32) NOT NULL DEFAULT 'x',
		`+"`updated_at`"+` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
		`+"`gen_col`"+` INT GENERATED ALWAYS AS (`+"`tenant_id`"+` * 2) STORED,
		PRIMARY KEY (`+"`id`"+`, `+"`tenant_id`"+`)
	) ENGINE=InnoDB`)
	defer func() {
		mustExec(t, db, "DROP TABLE IF EXISTS `test_pk_extra`")
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

	checks := []struct {
		name    string
		primary bool
		extra   string
	}{
		{"id", true, "auto_increment"},
		{"tenant_id", true, ""},
		{"name", false, ""},
		// 表达式默认值列：EXTRA 为 DEFAULT_GENERATED，与 on update 标记以空格共存
		{"updated_at", false, "DEFAULT_GENERATED on update CURRENT_TIMESTAMP"},
		// 生成列：EXTRA 标注生成方式（该列不可写）
		{"gen_col", false, "STORED GENERATED"},
	}
	for _, c := range checks {
		got, ok := byName[c.name]
		if !ok {
			t.Errorf("缺少列 %s", c.name)
			continue
		}
		if got.PrimaryKey != c.primary {
			t.Errorf("列 %s: PrimaryKey 期望 %v，实际 %v", c.name, c.primary, got.PrimaryKey)
		}
		if got.Extra != c.extra {
			t.Errorf("列 %s: Extra 期望 %q，实际 %q", c.name, c.extra, got.Extra)
		}
	}
}

// TestMySQLInteg_SchemaInspector_Indexes 验证 Indexes 返回主键、唯一、联合普通索引与
// 前缀索引、函数索引：列按 SEQ_IN_INDEX 顺序聚合，SUB_PART 呈现为 col(n)，
// 函数索引的 NULL 列名以 #expr 占位；联合主键的 PRIMARY 行按定义顺序而非列名字典序给列。
func TestMySQLInteg_SchemaInspector_Indexes(t *testing.T) {
	db := openMySQLTestDB(t)
	// 建表前先清理（同上：dropMySQLTables 未覆盖这两个新表名）
	mustExec(t, db, "DROP TABLE IF EXISTS `test_indexes`")
	mustExec(t, db, "DROP TABLE IF EXISTS `test_indexes_pk`")
	mustExec(t, db, `CREATE TABLE `+"`test_indexes`"+` (
		`+"`id`"+` BIGINT NOT NULL,
		`+"`email`"+` VARCHAR(64) NOT NULL,
		`+"`user_id`"+` BIGINT NOT NULL,
		`+"`status`"+` VARCHAR(16) NOT NULL,
		PRIMARY KEY (`+"`id`"+`),
		UNIQUE KEY `+"`uk_email`"+` (`+"`email`"+`),
		KEY `+"`idx_user_status`"+` (`+"`user_id`"+`, `+"`status`"+`),
		KEY `+"`idx_prefix`"+` (`+"`email`"+`(10)),
		KEY `+"`idx_fn`"+` ((lower(`+"`email`"+`)))
	) ENGINE=InnoDB`)
	// 联合主键刻意让定义顺序与列名字典序相反（b, a），锁死列序取自定义顺序
	mustExec(t, db, `CREATE TABLE `+"`test_indexes_pk`"+` (
		`+"`a`"+` INT NOT NULL,
		`+"`b`"+` INT NOT NULL,
		`+"`c`"+` INT,
		PRIMARY KEY (`+"`b`"+`, `+"`a`"+`)
	) ENGINE=InnoDB`)
	defer func() {
		mustExec(t, db, "DROP TABLE IF EXISTS `test_indexes`")
		mustExec(t, db, "DROP TABLE IF EXISTS `test_indexes_pk`")
	}()

	inspector, err := db.Schema()
	if err != nil {
		t.Fatalf("Schema() error: %v", err)
	}
	indexes, err := inspector.Indexes(context.Background(), "test_indexes")
	if err != nil {
		t.Fatalf("Indexes() error: %v", err)
	}
	if len(indexes) != 5 {
		t.Fatalf("expected 5 indexes, got %d: %v", len(indexes), indexes)
	}
	byName := make(map[string]IndexInfo, len(indexes))
	for _, idx := range indexes {
		byName[idx.Name] = idx
	}

	checks := []struct {
		name    string
		columns []string
		unique  bool
		primary bool
	}{
		{"PRIMARY", []string{"id"}, true, true},
		{"uk_email", []string{"email"}, true, false},
		{"idx_user_status", []string{"user_id", "status"}, false, false},
		{"idx_prefix", []string{"email(10)"}, false, false},
		{"idx_fn", []string{"#expr"}, false, false},
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

	pkIndexes, err := inspector.Indexes(context.Background(), "test_indexes_pk")
	if err != nil {
		t.Fatalf("Indexes() error: %v", err)
	}
	if len(pkIndexes) != 1 {
		t.Fatalf("联合主键表应只有 1 个索引（主键），实际 %d: %v", len(pkIndexes), pkIndexes)
	}
	if !pkIndexes[0].Primary || !reflect.DeepEqual(pkIndexes[0].Columns, []string{"b", "a"}) {
		t.Errorf("联合主键行的列序应取自定义顺序 [b a]，实际 %+v", pkIndexes[0])
	}
}

// TestMySQLInteg_SchemaInspector_IndexesNonexistentTable 边界固化：不存在的表查 Indexes
// 返回空切片与 nil 错误（information_schema 查询无命中行），与 Columns 的边界行为一致。
func TestMySQLInteg_SchemaInspector_IndexesNonexistentTable(t *testing.T) {
	db := openMySQLTestDB(t)
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
