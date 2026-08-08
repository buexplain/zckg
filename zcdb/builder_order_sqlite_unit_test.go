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
			builder: NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
		},
		{
			name:    "SharedLock_error",
			builder: NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
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
