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
			builder:  newTestBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
			expected: "SELECT * FROM `users` WHERE `id` = ? FOR UPDATE",
		},
		{
			name:     "SharedLock",
			builder:  newTestBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
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

// TestMySQLGrammar_OffsetWithoutLimit 锁死「只有 OFFSET、没有有效 LIMIT」时补最大行数 LIMIT：
// Limit 未调用、Limit(0)、Limit(负数) 三种情形都不输出 LIMIT 子句（见 Limit 约定），
// 而 MySQL 要求 OFFSET 必须与 LIMIT 同时出现，缺补位会得到 Error 1064。
func TestMySQLGrammar_OffsetWithoutLimit(t *testing.T) {
	g := &MySQLGrammar{}
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
			assertSQL(t, "SELECT * FROM `users` LIMIT 18446744073709551615 OFFSET 20", sql)
		})
	}
}
