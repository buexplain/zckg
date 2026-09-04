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
		in   string
		want string
	}{
		{"user_info", "UserInfo"},
		{"user_info_id", "UserInfoID"},
		{"userId", "UserID"},
		{"UserID", "UserID"},
		{"USER_ID", "UserID"},
		{"user-info", "UserInfo"},
		{"user id", "UserID"},
		{"HTTPServer", "HttpServer"},
		{"user2_name", "User2Name"},
		{"id", "ID"},
		{"a", "A"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := toPascalCase(tt.in); got != tt.want {
			t.Errorf("toPascalCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestLowerFirst 验证 lowerFirst 按 rune 语义小写首字符（ZCM-02）：
// ASCII 首字符正常小写；多字节字符（如中文）首字符完整保留，不被按字节截断产生残缺字节。
func TestLowerFirst(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"UserInfoDO", "userInfoDO"},
		{"UserInfoEntity", "userInfoEntity"},
		{"ID", "iD"},
		{"A", "a"},
		{"", ""},
		// 中文首字符按 rune 完整保留，不产生残缺字节导致非法标识符
		{"订单DO", "订单DO"},
		{"订单Entity", "订单Entity"},
	}
	for _, tt := range tests {
		if got := lowerFirst(tt.in); got != tt.want {
			t.Errorf("lowerFirst(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestSplitWords 验证 splitWords 的拆分规则：分隔符、小写到大写边界、连续大写后接小写边界
func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"user_id", []string{"user", "id"}},
		{"userId", []string{"user", "Id"}},
		{"UserID", []string{"User", "ID"}},
		{"USER_ID", []string{"USER", "ID"}},
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"user-info", []string{"user", "info"}},
		{"user id", []string{"user", "id"}},
		{"a_b_c", []string{"a", "b", "c"}},
		{"user2_name", []string{"user2", "name"}},
		{"single", []string{"single"}},
	}
	for _, tt := range tests {
		if got := splitWords(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("splitWords(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

// TestToCase_各命名风格 验证六个命名风格转换函数对单词列表的输出
func TestToCase_各命名风格(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"toLowerCamel", toLowerCamel([]string{"user", "id"}), "userId"},
		{"toLowerCamel_首单词全小写", toLowerCamel([]string{"User", "ID"}), "userId"},
		{"toLowerCamel_单单词", toLowerCamel([]string{"ID"}), "id"},
		{"toLowerCamel_空列表", toLowerCamel(nil), ""},
		{"toUpperCamel", toUpperCamel([]string{"user", "id"}), "UserId"},
		{"toUpperCamel_空列表", toUpperCamel(nil), ""},
		{"toLowerSnake", toLowerSnake([]string{"user", "ID"}), "user_id"},
		{"toLowerSnake_空列表", toLowerSnake(nil), ""},
		{"toUpperSnake", toUpperSnake([]string{"user", "id"}), "USER_ID"},
		{"toUpperSnake_空列表", toUpperSnake(nil), ""},
		{"toLowerKebab", toLowerKebab([]string{"user", "id"}), "user-id"},
		{"toLowerKebab_空列表", toLowerKebab(nil), ""},
		{"toUpperKebab", toUpperKebab([]string{"user", "id"}), "USER-ID"},
		{"toUpperKebab_空列表", toUpperKebab(nil), ""},
	}
	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
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
