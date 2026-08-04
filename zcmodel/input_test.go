package zcmodel

import "testing"

// TestNameCase_IsValid 验证 NameCase 枚举的合法值与非法值判断
func TestNameCase_IsValid(t *testing.T) {
	tests := []struct {
		name string
		c    NameCase
		want bool
	}{
		{"lowerCamel", NameCaseLowerCamel, true},
		{"upperCamel", NameCaseUpperCamel, true},
		{"lowerSnake", NameCaseLowerSnake, true},
		{"upperSnake", NameCaseUpperSnake, true},
		{"lowerKebab", NameCaseLowerKebab, true},
		{"upperKebab", NameCaseUpperKebab, true},
		{"空值", "", false},
		{"未知值", NameCase("PascalCase"), false},
		{"大小写敏感", NameCase("lowercamel"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.c.IsValid(); got != tt.want {
				t.Errorf("NameCase(%q).IsValid() = %v, want %v", tt.c, got, tt.want)
			}
		})
	}
}

// TestNameCase_Constants 验证六种命名风格常量的值
func TestNameCase_Constants(t *testing.T) {
	tests := []struct {
		c    NameCase
		want string
	}{
		{NameCaseLowerCamel, "lowerCamel"},
		{NameCaseUpperCamel, "upperCamel"},
		{NameCaseLowerSnake, "lowerSnake"},
		{NameCaseUpperSnake, "upperSnake"},
		{NameCaseLowerKebab, "lowerKebab"},
		{NameCaseUpperKebab, "upperKebab"},
	}
	for _, tt := range tests {
		if string(tt.c) != tt.want {
			t.Errorf("NameCase 常量值 = %q, want %q", tt.c, tt.want)
		}
	}
}

// TestDialect_Constants 验证三种数据库方言常量的值
func TestDialect_Constants(t *testing.T) {
	tests := []struct {
		dialect Dialect
		want    string
	}{
		{DialectMysql, "mysql"},
		{DialectPostgres, "postgres"},
		{DialectSqlite, "sqlite"},
	}
	for _, tt := range tests {
		if string(tt.dialect) != tt.want {
			t.Errorf("Dialect 常量值 = %q, want %q", tt.dialect, tt.want)
		}
	}
}
