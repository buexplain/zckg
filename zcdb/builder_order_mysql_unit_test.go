// 本文件为 MySQL 方言单元测试——OrderBy/Limit/Offset/Union/锁等排序分页与集合操作。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// TestMySQLGrammar_LockSQL 验证 MySQL 方言的锁子句生成：LockForUpdate → FOR UPDATE，SharedLock → LOCK IN SHARE MODE。
func TestMySQLGrammar_LockSQL(t *testing.T) {
	g := &MySQLGrammar{}

	tests := []struct {
		name     string
		builder  *Builder
		expected string
	}{
		{
			name:     "LockForUpdate",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
			expected: "SELECT * FROM `users` WHERE `id` = ? FOR UPDATE",
		},
		{
			name:     "SharedLock",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
			expected: "SELECT * FROM `users` WHERE `id` = ? LOCK IN SHARE MODE",
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
