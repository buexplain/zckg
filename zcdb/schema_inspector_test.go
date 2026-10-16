package zcdb

import (
	"context"
	"errors"
	"strings"
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

// unsupportedGrammar 是仅用于覆盖 NewSchemaInspector 兜底分支的 Grammar 桩：
// 嵌入接口即可满足 Grammar，且不属于三个内置类型。
type unsupportedGrammar struct{ Grammar }

// TestNewSchemaInspector_Unsupported 验证非内置方言（含 nil Grammar）返回
// ErrSchemaGrammarNotSupported、不返回半成品 inspector，且自定义类型会出现在消息里。
func TestNewSchemaInspector_Unsupported(t *testing.T) {
	tests := []struct {
		name        string
		grammar     Grammar
		wantTypeMsg string // 期望出现在消息中的类型名；空表示只校验哨兵
	}{
		{name: "nil grammar", grammar: nil},
		{name: "非内置 Grammar", grammar: unsupportedGrammar{}, wantTypeMsg: "zcdb.unsupportedGrammar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inspector, err := NewSchemaInspector(&DBDao{grammar: tt.grammar})
			if !errors.Is(err, ErrSchemaGrammarNotSupported) {
				t.Fatalf("错误未匹配 ErrSchemaGrammarNotSupported：%v", err)
			}
			if tt.wantTypeMsg != "" && !strings.Contains(err.Error(), tt.wantTypeMsg) {
				t.Errorf("错误消息应带出实际 Grammar 类型 %q，got %q", tt.wantTypeMsg, err.Error())
			}
			if inspector != nil {
				t.Errorf("失败时应返回 nil inspector，got %T", inspector)
			}
		})
	}
}

// brokenQueryDAO 构造可完成建连、但连接池已关闭的 DAO：任何 Query/Exec 返回
// "sql: database is closed"。用于覆盖元数据查询在连接不可用时的失败分支——
// 数据库健康时这些分支无法触达。
func brokenQueryDAO(t *testing.T, dialect string) *DBDao {
	t.Helper()
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:", MaxOpenConns: 1})
	if err != nil {
		t.Fatalf("建连失败: %v", err)
	}
	dao, err := NewDBDao(pool, dialect, nil, "", "")
	if err != nil {
		t.Fatalf("创建 DAO 失败: %v", err)
	}
	if err := pool.Close(); err != nil {
		t.Fatalf("关闭连接池失败: %v", err)
	}
	return dao
}

// TestSchemaInspector_ClosedPoolQueryErrors 验证三方言的 Tables/Columns/Indexes
// 在连接不可用时如实上报查询错误（查询失败分支）。
func TestSchemaInspector_ClosedPoolQueryErrors(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		s    SchemaInspector
	}{
		{"mysql", &MySQLSchemaInspector{dao: brokenQueryDAO(t, "mysql")}},
		{"postgres", &PostgresSchemaInspector{dao: brokenQueryDAO(t, "postgres")}},
		{"sqlite", &SQLiteSchemaInspector{dao: brokenQueryDAO(t, "sqlite")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.s.Tables(ctx); err == nil {
				t.Error("Tables 在连接不可用时应上报查询错误")
			}
			if _, err := tc.s.Columns(ctx, "users"); err == nil {
				t.Error("Columns 在连接不可用时应上报查询错误")
			}
			if _, err := tc.s.Indexes(ctx, "users"); err == nil {
				t.Error("Indexes 在连接不可用时应上报查询错误")
			}
		})
	}
}

// TestSQLiteSchemaInspector_IndexHelperQueryErrors 覆盖 PRAGMA 子查询 helper 的查询失败分支：
// Indexes 在 sqlitePrimaryIndex 失败时即短路，两个 helper 需单独驱动。
func TestSQLiteSchemaInspector_IndexHelperQueryErrors(t *testing.T) {
	ctx := context.Background()
	s := &SQLiteSchemaInspector{dao: brokenQueryDAO(t, "sqlite")}
	if _, err := s.sqliteIndexList(ctx, `"users"`); err == nil {
		t.Error("sqliteIndexList 在连接不可用时应上报查询错误")
	}
	if _, err := s.sqliteIndexColumns(ctx, `"idx_users"`); err == nil {
		t.Error("sqliteIndexColumns 在连接不可用时应上报查询错误")
	}
}
