package zcdb

import (
	"testing"
)

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
