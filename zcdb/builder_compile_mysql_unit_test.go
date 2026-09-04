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

// TestMySQLCompile_InsertUsingColumnMismatch 验证 MySQL 方言 InsertUsing 列数校验
// （详见 assertInsertUsingColumnMismatch：不一致报 ErrInsertUsingColumnMismatch，
// 一致/通配符/默认 SELECT * 时通过编译，后两者由数据库运行时校验）。
func TestMySQLCompile_InsertUsingColumnMismatch(t *testing.T) {
	assertInsertUsingColumnMismatch(t, NewMySQLGrammar())
}

// TestMySQLCompile_UpsertEmptyUpdateColumnsElse 覆盖 MySQL CompileUpsert 在
// updateColumns 为空且 uniqueBy 为空时退化为 columns[:1] 的 else 分支。
// 该分支经公共 ToUpsert 不可达（空 updateColumns 会被展开为全部非 uniqueBy 列），
// 故以 grammar 直调方式锁死。
func TestMySQLCompile_UpsertEmptyUpdateColumnsElse(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("t")
	sql := g.CompileUpsert(b, []string{"id"}, [][]any{{int64(1)}}, nil, nil, nil)
	if sql == "" || sql != "INSERT INTO `t` (`id`) VALUES (?) ON DUPLICATE KEY UPDATE `id` = VALUES(`id`)" {
		t.Fatalf("空 updateColumns + 空 uniqueBy 应退化为首列自赋值，实际: %s", sql)
	}
}

// TestMySQLCompile_InsertOrIgnoreMultiRowAndExpression 覆盖 MySQL 方言
// CompileInsertOrIgnore 的多行分隔符与 Expression 内联分支。
func TestMySQLCompile_InsertOrIgnoreMultiRowAndExpression(t *testing.T) {
	rows := [][]any{
		{int64(1), NewExpression("UPPER('a')")},
		{int64(2), "b"},
	}
	my := NewMySQLGrammar()
	if sql := my.CompileInsertOrIgnore(NewBuilder(my, nil).Table("t"), []string{"id", "name"}, rows); sql == "" {
		t.Fatal("MySQL CompileInsertOrIgnore 应产出 SQL")
	}
}
