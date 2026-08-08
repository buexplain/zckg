// 本文件为 MySQL 方言单元测试——SQL 编译（ToXxx 系列）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import (
	"testing"
)

// TestBug_ToUpdateWithJoin_MySQL 验证 MySQL UPDATE + JOIN 的绑定参数数量。
// CompileUpdate 在 JOIN ON value 条件中生成 ? 占位符，
// 但 ToUpdate 只收集 WHERE 绑定，不收集 JOIN 绑定，导致参数数量不匹配。
func TestBug_ToUpdateWithJoin_MySQL(t *testing.T) {
	g := NewMySQLGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := NewBuilder(g, nil).
		Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1)

	sql, args, err := b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)

	// SQL 中应有 3 个 ?：SET 1个 + JOIN ON value 1个 + WHERE 1个
	expectedSQL := "UPDATE `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? SET `name` = ? WHERE `users`.`id` = ?"
	assertSQL(t, expectedSQL, sql)
	// 绑定应为 3 个：[99, "x", 1]
	assertArgs(t, []any{99, "x", 1}, args)
}
