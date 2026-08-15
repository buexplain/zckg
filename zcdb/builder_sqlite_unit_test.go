// 本文件为 SQLite 方言单元测试——Builder 基础能力（方言特有的通用行为）。
// 仅验证 Grammar 编译结果，不依赖数据库连接。
//
// 当前该类别暂无独立用例；本文件占位以保持"方言 × 类别"目录结构完整。
package zcdb

import "testing"

// TestSQLiteGrammar_WrapTable_MixedCaseAlias 验证 WrapTable 的 " as " 拆分对大小写不敏感：
// "orders As o"、"orders aS o" 等混合大小写别名均被正确拆分包裹，而非整体当作一个标识符。
func TestSQLiteGrammar_WrapTable_MixedCaseAlias(t *testing.T) {
	g := NewSQLiteGrammar()
	tests := []struct {
		in   string
		want string
	}{
		{"orders", `"orders"`},
		{"orders as o", `"orders" AS "o"`},
		{"orders AS o", `"orders" AS "o"`},
		{"orders As o", `"orders" AS "o"`},
		{"orders aS o", `"orders" AS "o"`},
	}
	for _, tt := range tests {
		if got := g.WrapTable(tt.in); got != tt.want {
			t.Errorf("WrapTable(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
