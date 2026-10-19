// 本文件为 SQLite 方言单元测试——OrderBy/Limit/Offset/Union/锁等排序分页与集合操作。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"errors"
	"testing"
)

// TestSQLiteGrammar_LockSQL 验证 SQLite 方言不支持锁子句：LockForUpdate 和 SharedLock 均返回错误。
func TestSQLiteGrammar_LockSQL(t *testing.T) {
	g := &SQLiteGrammar{}

	tests := []struct {
		name    string
		builder *Builder
	}{
		{
			name:    "LockForUpdate_error",
			builder: newTestBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
		},
		{
			name:    "SharedLock_error",
			builder: newTestBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.builder.ToSelect()
			if !errors.Is(err, ErrSQLiteLockNotSupported) {
				t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
			}
		})
	}
}

// TestSQLiteGrammar_OffsetWithoutLimit 锁死「只有 OFFSET、没有有效 LIMIT」时补 LIMIT -1：
// Limit 未调用、Limit(0)、Limit(负数) 三种情形都不输出 LIMIT 子句（见 Limit 约定），
// 而 SQLite 要求 OFFSET 必须跟在 LIMIT 之后，缺补位会得到语法错误。
func TestSQLiteGrammar_OffsetWithoutLimit(t *testing.T) {
	g := &SQLiteGrammar{}
	cases := []struct {
		name    string
		builder *Builder
	}{
		{"limit_unset", newTestBuilder(g, nil).Table("users").Offset(20)},
		{"limit_zero", newTestBuilder(g, nil).Table("users").Limit(0).Offset(20)},
		{"limit_negative", newTestBuilder(g, nil).Table("users").Limit(-5).Offset(20)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sql, _, err := c.builder.ToSelect()
			assertNoError(t, err)
			assertSQL(t, `SELECT * FROM "users" LIMIT -1 OFFSET 20`, sql)
		})
	}
}
