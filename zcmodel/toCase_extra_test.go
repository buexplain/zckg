package zcmodel

import "testing"

// TestToCase_EmptyWordSkipped 验证各命名风格转换函数对单词列表中的空串元素按跳过处理
// （覆盖 toPascalCase/toLowerCamel/toUpperCamel/toLowerSnake/toUpperSnake/toLowerKebab/toUpperKebab
// 内部的 w == "" continue 防御分支）。
func TestToCase_EmptyWordSkipped(t *testing.T) {
	emptyWord := []string{"user", "", "id"}
	leadingEmpty := []string{"", "user"}

	tests := []struct {
		name string
		got  string
		want string
	}{
		{"toPascalCase_空单词", toPascalCase("user__id"), "UserID"},
		{"toLowerCamel_空单词", toLowerCamel(emptyWord), "userId"},
		{"toLowerCamel_前导空", toLowerCamel(leadingEmpty), "User"},
		{"toUpperCamel_空单词", toUpperCamel(emptyWord), "UserId"},
		{"toUpperCamel_前导空", toUpperCamel(leadingEmpty), "User"},
		{"toLowerSnake_空单词", toLowerSnake(emptyWord), "user_id"},
		{"toUpperSnake_空单词", toUpperSnake(emptyWord), "USER_ID"},
		{"toLowerKebab_空单词", toLowerKebab(emptyWord), "user-id"},
		{"toUpperKebab_空单词", toUpperKebab(emptyWord), "USER-ID"},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}
