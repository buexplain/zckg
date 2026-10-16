// 本文件为 SQL 代码区扫描器（sqlScan.go）的单元测试：验证按方言语法切分代码区/非代码区，
// 覆盖字符串、引号标识符、行注释与块注释、PostgreSQL 美元引用与嵌套块注释、
// MySQL 的反斜杠转义与 -- 注释需空白、PostgreSQL 的 E'...' 转义字符串，以及未闭合字符串。
// 每个用例同时断言"片段拼接等于原文"这一不变量。

package zcdb

import (
	"strings"
	"testing"
)

// renderSegments 把分段渲染成 C(...) 代码区 / N(...) 非代码区 的紧凑形式，便于断言切分位置。
func renderSegments(segments []sqlSegment) string {
	var buf strings.Builder
	for _, segment := range segments {
		if segment.code {
			buf.WriteString("C(")
		} else {
			buf.WriteString("N(")
		}
		buf.WriteString(segment.text)
		buf.WriteString(")")
	}
	return buf.String()
}

// TestSplitSQLCode 验证各方言语法的切分结果：
// 只有语法上属于代码区的片段才允许被当作占位符所在位置。
func TestSplitSQLCode(t *testing.T) {
	tests := []struct {
		name    string
		syntax  sqlScanSyntax
		sqlStr  string
		wantSeg string
	}{
		{"空串无片段", mysqlScanSyntax, "", ""},
		{"纯代码区", mysqlScanSyntax, "SELECT ?", "C(SELECT ?)"},
		{"MySQL 单引号字符串", mysqlScanSyntax, "a'?'b", "C(a)N('?')C(b)"},
		{"单引号双写转义", mysqlScanSyntax, "'a''b'", "N('a''b')"},
		{"MySQL 反斜杠转义单引号", mysqlScanSyntax, `'a\'b'`, `N('a\'b')`},
		{"SQLite 反斜杠不转义", sqliteScanSyntax, `'a\'b'`, `N('a\')C(b)N(')`},
		{"MySQL 双引号是字符串并按反斜杠转义", mysqlScanSyntax, `"a\"b"`, `N("a\"b")`},
		{"PostgreSQL 双引号内反斜杠不转义", pgScanSyntax, `"a\"b"`, `N("a\")C(b)N(")`},
		{"MySQL 反引号标识符", mysqlScanSyntax, "`a?b`c", "N(`a?b`)C(c)"},
		{"PostgreSQL 无反引号语法", pgScanSyntax, "`a?b`c", "C(`a?b`c)"},
		{"MySQL 的 -- 注释", mysqlScanSyntax, "a -- ?\nb", "C(a )N(-- ?)C(\nb)"},
		{"MySQL 的 -- 后无空白不算注释", mysqlScanSyntax, "a--?b", "C(a--?b)"},
		{"PostgreSQL 的 -- 后无空白也算注释", pgScanSyntax, "a--?b", "C(a)N(--?b)"},
		{"MySQL 的 # 注释", mysqlScanSyntax, "a # ?", "C(a )N(# ?)"},
		{"PostgreSQL 无 # 注释", pgScanSyntax, "a # ?", "C(a # ?)"},
		{"块注释", sqliteScanSyntax, "a/* ? */b", "C(a)N(/* ? */)C(b)"},
		{"PostgreSQL 块注释可嵌套", pgScanSyntax, "a/* /* ? */ */b", "C(a)N(/* /* ? */ */)C(b)"},
		{"MySQL 块注释不嵌套", mysqlScanSyntax, "a/* /* ? */ */b", "C(a)N(/* /* ? */)C( */b)"},
		{"PostgreSQL 美元引用", pgScanSyntax, "a $tag$ ? $1 $tag$ b", "C(a )N($tag$ ? $1 $tag$)C( b)"},
		{"PostgreSQL 空标签美元引用", pgScanSyntax, "a $$ ? $$ b", "C(a )N($$ ? $$)C( b)"},
		{"PostgreSQL 的 $N 不是美元引用", pgScanSyntax, "a $1 b", "C(a $1 b)"},
		{"PostgreSQL E 前缀触发反斜杠转义", pgScanSyntax, `a E'\'' b`, `C(a E)N('\'')C( b)`},
		{"PostgreSQL 普通字符串里的 E 不影响转义规则", pgScanSyntax, `a 'E\' b`, `C(a )N('E\')C( b)`},
		{"未闭合字符串吞到结尾", mysqlScanSyntax, "a 'b", "C(a )N('b)"},
		{"未闭合块注释吞到结尾", sqliteScanSyntax, "a /* b", "C(a )N(/* b)"},
		{"PostgreSQL 输入以引号开头（E 前缀判定边界）", pgScanSyntax, "'a' x", "N('a')C( x)"},
		{"PostgreSQL 残缺标签不是美元引用", pgScanSyntax, "a $tag b", "C(a $tag b)"},
		{"PostgreSQL 未闭合美元引用吞到结尾", pgScanSyntax, "a $tag$ b", "C(a )N($tag$ b)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			segments := splitSQLCode(tt.sqlStr, tt.syntax)
			if got := renderSegments(segments); got != tt.wantSeg {
				t.Errorf("分段不符：\n got: %s\nwant: %s", got, tt.wantSeg)
			}
			var joined strings.Builder
			for _, segment := range segments {
				joined.WriteString(segment.text)
			}
			if joined.String() != tt.sqlStr {
				t.Errorf("片段拼接应等于原文：got %q，want %q", joined.String(), tt.sqlStr)
			}
		})
	}
}
