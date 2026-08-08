// 本文件为 PostgreSQL 方言单元测试——OrderBy/Limit/Offset/Union/锁等排序分页与集合操作。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"errors"
	"testing"
)

// TestBug_PgUnionLock 验证 PostgreSQL UNION + LOCK 返回错误（PostgreSQL 不支持此组合）。
func TestBug_PgUnionLock(t *testing.T) {
	g := NewPostgresGrammar()
	union := NewBuilder(g, nil).Table("users").Where("age", ">", 25)
	b := NewBuilder(g, nil).Table("users").Where("status", "=", "active").Union(union).LockForUpdate()

	_, _, err := b.ToSelect()
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestBug_PgUnionSharedLock 验证 PostgreSQL UNION + SharedLock 返回错误。
func TestBug_PgUnionSharedLock(t *testing.T) {
	g := NewPostgresGrammar()
	union := NewBuilder(g, nil).Table("users").Where("age", ">", 25)
	b := NewBuilder(g, nil).Table("users").Where("status", "=", "active").Union(union).SharedLock()

	_, _, err := b.ToSelect()
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestPostgresGrammar_LockSQL 验证 PostgreSQL 方言的锁子句生成：LockForUpdate → FOR UPDATE，SharedLock → FOR SHARE（自动转换）。
func TestPostgresGrammar_LockSQL(t *testing.T) {
	g := &PostgresGrammar{}

	tests := []struct {
		name     string
		builder  *Builder
		expected string
	}{
		{
			name:     "LockForUpdate",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
			expected: "SELECT * FROM \"users\" WHERE \"id\" = $1 FOR UPDATE",
		},
		{
			name:     "SharedLock",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
			expected: "SELECT * FROM \"users\" WHERE \"id\" = $1 FOR SHARE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{1}, args)
		})
	}
}
