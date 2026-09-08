package zcmodel

import (
	"reflect"
	"testing"
)

// TestFormatJSONTag 验证 formatJSONTag 按六种命名风格转换列名，未知风格返回空串
func TestFormatJSONTag(t *testing.T) {
	tests := []struct {
		name     string
		colName  string
		caseType NameCase
		want     string
	}{
		{"lowerCamel", "user_id", NameCaseLowerCamel, "userId"},
		{"upperCamel", "user_id", NameCaseUpperCamel, "UserId"},
		{"lowerSnake", "userId", NameCaseLowerSnake, "user_id"},
		{"upperSnake", "userId", NameCaseUpperSnake, "USER_ID"},
		{"lowerKebab", "userId", NameCaseLowerKebab, "user-id"},
		{"upperKebab", "user_id", NameCaseUpperKebab, "USER-ID"},
		{"未知风格", "user_id", NameCase("camelCase"), ""},
		{"空列名", "", NameCaseLowerCamel, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatJSONTag(tt.colName, tt.caseType); got != tt.want {
				t.Errorf("formatJSONTag(%q, %q) = %q, want %q", tt.colName, tt.caseType, got, tt.want)
			}
		})
	}
}

// TestToPascalCase 验证 toPascalCase 对多种输入风格转换为 PascalCase，单词 id 统一转为 ID
func TestToPascalCase(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"下划线", "user_info", "UserInfo"},
		{"下划线含id结尾", "user_info_id", "UserInfoID"},
		{"lowerCamel输入", "userId", "UserID"},
		{"PascalCase输入", "UserID", "UserID"},
		{"UPPER_SNAKE输入", "USER_ID", "UserID"},
		{"连字符", "user-info", "UserInfo"},
		{"空格分隔", "user id", "UserID"},
		{"连续大写缩写", "HTTPServer", "HttpServer"},
		{"含数字", "user2_name", "User2Name"},
		{"单词id", "id", "ID"},
		{"单字符", "a", "A"},
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toPascalCase(tt.in); got != tt.want {
				t.Errorf("toPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestLowerFirst 验证 lowerFirst 按 rune 语义小写首字符（ZCM-02）：
// ASCII 首字符正常小写；多字节字符（如中文）首字符完整保留，不被按字节截断产生残缺字节。
func TestLowerFirst(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"DO后缀", "UserInfoDO", "userInfoDO"},
		{"Entity后缀", "UserInfoEntity", "userInfoEntity"},
		{"全大写缩写", "ID", "iD"},
		{"单字符", "A", "a"},
		{"空串", "", ""},
		// 中文首字符按 rune 完整保留，不产生残缺字节导致非法标识符
		{"中文前缀DO", "订单DO", "订单DO"},
		{"中文前缀Entity", "订单Entity", "订单Entity"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := lowerFirst(tt.in); got != tt.want {
				t.Errorf("lowerFirst(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestSplitWords 验证 splitWords 的拆分规则：分隔符、小写到大写边界、连续大写后接小写边界
func TestSplitWords(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"空串", "", nil},
		{"下划线", "user_id", []string{"user", "id"}},
		{"lowerCamel", "userId", []string{"user", "Id"}},
		{"PascalCase含缩写", "UserID", []string{"User", "ID"}},
		{"UPPER_SNAKE", "USER_ID", []string{"USER", "ID"}},
		{"连续大写后接小写", "HTTPServer", []string{"HTTP", "Server"}},
		{"连字符", "user-info", []string{"user", "info"}},
		{"空格分隔", "user id", []string{"user", "id"}},
		{"多段下划线", "a_b_c", []string{"a", "b", "c"}},
		{"含数字", "user2_name", []string{"user2", "name"}},
		{"单个单词", "single", []string{"single"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := splitWords(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("splitWords(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

// TestToCase_各命名风格 验证六个命名风格转换函数对单词列表的输出
func TestToCase_各命名风格(t *testing.T) {
	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"toLowerCamel", func() string { return toLowerCamel([]string{"user", "id"}) }, "userId"},
		{"toLowerCamel_首单词全小写", func() string { return toLowerCamel([]string{"User", "ID"}) }, "userId"},
		{"toLowerCamel_单单词", func() string { return toLowerCamel([]string{"ID"}) }, "id"},
		{"toLowerCamel_空列表", func() string { return toLowerCamel(nil) }, ""},
		{"toUpperCamel", func() string { return toUpperCamel([]string{"user", "id"}) }, "UserId"},
		{"toUpperCamel_空列表", func() string { return toUpperCamel(nil) }, ""},
		{"toLowerSnake", func() string { return toLowerSnake([]string{"user", "ID"}) }, "user_id"},
		{"toLowerSnake_空列表", func() string { return toLowerSnake(nil) }, ""},
		{"toUpperSnake", func() string { return toUpperSnake([]string{"user", "id"}) }, "USER_ID"},
		{"toUpperSnake_空列表", func() string { return toUpperSnake(nil) }, ""},
		{"toLowerKebab", func() string { return toLowerKebab([]string{"user", "id"}) }, "user-id"},
		{"toLowerKebab_空列表", func() string { return toLowerKebab(nil) }, ""},
		{"toUpperKebab", func() string { return toUpperKebab([]string{"user", "id"}) }, "USER-ID"},
		{"toUpperKebab_空列表", func() string { return toUpperKebab(nil) }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

// TestToCase_EmptyWordSkipped 验证各命名风格转换函数对单词列表中的空串元素按跳过处理
// （覆盖 toPascalCase/toLowerCamel/toUpperCamel/toLowerSnake/toUpperSnake/toLowerKebab/toUpperKebab
// 内部的 w == "" continue 防御分支）。
func TestToCase_EmptyWordSkipped(t *testing.T) {
	emptyWord := []string{"user", "", "id"}
	leadingEmpty := []string{"", "user"}

	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"toPascalCase_空单词", func() string { return toPascalCase("user__id") }, "UserID"},
		{"toLowerCamel_空单词", func() string { return toLowerCamel(emptyWord) }, "userId"},
		{"toLowerCamel_前导空", func() string { return toLowerCamel(leadingEmpty) }, "User"},
		{"toUpperCamel_空单词", func() string { return toUpperCamel(emptyWord) }, "UserId"},
		{"toUpperCamel_前导空", func() string { return toUpperCamel(leadingEmpty) }, "User"},
		{"toLowerSnake_空单词", func() string { return toLowerSnake(emptyWord) }, "user_id"},
		{"toUpperSnake_空单词", func() string { return toUpperSnake(emptyWord) }, "USER_ID"},
		{"toLowerKebab_空单词", func() string { return toLowerKebab(emptyWord) }, "user-id"},
		{"toUpperKebab_空单词", func() string { return toUpperKebab(emptyWord) }, "USER-ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fn(); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}
