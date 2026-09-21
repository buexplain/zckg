package zcdb

import (
	"testing"
)

// 编译期锁死接口契约：SchemaInspector 含 Tables/Columns/Indexes 三个方法，
// 任一内置实现漏实现（或签名不一致）都会使本测试文件编译失败。
var (
	_ SchemaInspector = (*MySQLSchemaInspector)(nil)
	_ SchemaInspector = (*PostgresSchemaInspector)(nil)
	_ SchemaInspector = (*SQLiteSchemaInspector)(nil)
)

// TestSchemaInspector_ExprColumnPlaceholder 锁死表达式列的占位符为文档约定的 #expr：
// 三方言的表达式（函数）索引列名均无法从基础元数据取得，统一以该占位符呈现，
// 生成模型索引块的调用方（zcmodel）依赖此约定。
func TestSchemaInspector_ExprColumnPlaceholder(t *testing.T) {
	if exprColumnPlaceholder != "#expr" {
		t.Errorf("表达式列占位符应为 #expr，实际 %q", exprColumnPlaceholder)
	}
}

// TestSchemaInspector_IndexInfoFields 验证 IndexInfo 的字段语义（零值可作为「非主键、非唯一」的默认态）：
// Primary/Unique 为布尔标记，Columns 为按定义顺序的列名切片。
func TestSchemaInspector_IndexInfoFields(t *testing.T) {
	var idx IndexInfo
	if idx.Primary || idx.Unique || idx.Name != "" || idx.Columns != nil {
		t.Errorf("IndexInfo 零值应为空索引（非主键、非唯一、无列），实际 %+v", idx)
	}
	idx = IndexInfo{Name: "uk_email", Columns: []string{"email"}, Unique: true}
	if idx.Primary {
		t.Errorf("仅置 Unique 时 Primary 应为 false，实际 %+v", idx)
	}
}

// TestNewSchemaInspector_MySQL 验证 MySQL 方言返回 MySQLSchemaInspector。
func TestNewSchemaInspector_MySQL(t *testing.T) {
	dao := &DBDao{grammar: &MySQLGrammar{}}
	inspector, err := NewSchemaInspector(dao)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := inspector.(*MySQLSchemaInspector); !ok {
		t.Errorf("expected *MySQLSchemaInspector, got %T", inspector)
	}
}

// TestNewSchemaInspector_Postgres 验证 PostgreSQL 方言返回 PostgresSchemaInspector。
func TestNewSchemaInspector_Postgres(t *testing.T) {
	dao := &DBDao{grammar: &PostgresGrammar{}}
	inspector, err := NewSchemaInspector(dao)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := inspector.(*PostgresSchemaInspector); !ok {
		t.Errorf("expected *PostgresSchemaInspector, got %T", inspector)
	}
}

// TestNewSchemaInspector_SQLite 验证 SQLite 方言返回 SQLiteSchemaInspector。
func TestNewSchemaInspector_SQLite(t *testing.T) {
	dao := &DBDao{grammar: &SQLiteGrammar{}}
	inspector, err := NewSchemaInspector(dao)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := inspector.(*SQLiteSchemaInspector); !ok {
		t.Errorf("expected *SQLiteSchemaInspector, got %T", inspector)
	}
}

// TestNewSchemaInspector_Unsupported 验证不支持的方言返回错误。
func TestNewSchemaInspector_Unsupported(t *testing.T) {
	dao := &DBDao{grammar: nil}
	_, err := NewSchemaInspector(dao)
	if err == nil {
		t.Error("expected error for nil grammar, got nil")
	}
}
