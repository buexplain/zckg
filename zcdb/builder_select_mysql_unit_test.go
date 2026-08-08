// 本文件为 MySQL 方言单元测试——Select/SelectRaw/Distinct/子查询列选择。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
package zcdb

import "testing"

// TestMySQLGrammar_AddSelectDedup 验证 AddSelect 追加列的语义：
// 1) 与既有非 Raw 列重复时不重复添加；
// 2) Raw 列不参与去重（即使文本相同）；
// 3) 未先调用 Select 时 AddSelect 直接建立列清单（不再是 SELECT *）。
func TestMySQLGrammar_AddSelectDedup(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name     string
		build    func() *Builder
		expected string
	}{
		{
			name: "dedup_existing_column",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Select("id").AddSelect("name", "id")
			},
			expected: "SELECT `id`, `name` FROM `users`",
		},
		{
			name: "raw_column_not_deduped",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").SelectRaw("name").AddSelect("name")
			},
			expected: "SELECT name, `name` FROM `users`",
		},
		{
			name: "append_without_prior_select",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").AddSelect("id")
			},
			expected: "SELECT `id` FROM `users`",
		},
		{
			name: "append_after_select_raw",
			build: func() *Builder {
				return NewBuilder(g, nil).Table("users").Select("id").SelectRaw("COUNT(*) AS c").AddSelect("name")
			},
			expected: "SELECT `id`, COUNT(*) AS c, `name` FROM `users`",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.build().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{}, args)
		})
	}
}
