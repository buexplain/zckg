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

// FuzzSplitSQLCode 用不变量 fuzz 校验 SQL 代码区扫描器：fuzzer 变异方言索引与 SQL 文本，
// 断言片段拼接等于原文、无空段、无相邻代码段、结果确定。
// 非代码段的边界语义由 TestSplitSQLCode 的精确期望锁定；这里不再用 nonCodeLen 自证片段长度，
// 避免 oracle 与被测实现同源。相邻非代码段是合法形态（如 /**/ 紧邻未闭合引号，二者之间没有代码区）。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzSplitSQLCode$' -fuzztime=30s
func FuzzSplitSQLCode(f *testing.F) {
	syntaxes := []sqlScanSyntax{mysqlScanSyntax, pgScanSyntax, sqliteScanSyntax}
	seeds := []string{
		"", "SELECT ?", "a'?'b", "'a''b'", `'a\'b'`, `"a\"b"`, "`a?b`c",
		"a -- ?\nb", "a--?b", "a # ?", "a/* ? */b", "a/* /* ? */ */b",
		"a $tag$ ? $1 $tag$ b", "a $$ ? $$ b", "a $1 b", `a E'\'' b`,
		`a 'E\' b`, "a 'b", "a /* b", "'a' x", "a $tag b", "a $tag$ b",
		"?''?", "/**/\"", "''''", "$$", "$tag$", "--", "#", "/*", "*/", "`", `"`, "'",
		"\x00'?'", "中文'?'", strings.Repeat("'", 8),
	}
	for i := range syntaxes {
		for _, seed := range seeds {
			f.Add(uint8(i), seed)
		}
	}
	f.Fuzz(func(t *testing.T, dialectIndex uint8, sqlStr string) {
		if len(sqlStr) > 64<<10 {
			return
		}
		syntax := syntaxes[int(dialectIndex)%len(syntaxes)]
		segments := splitSQLCode(sqlStr, syntax)
		var joined strings.Builder
		for i, segment := range segments {
			if segment.text == "" {
				t.Fatalf("dialect=%d sql=%q: empty segment at %d", dialectIndex, sqlStr, i)
			}
			if i > 0 && segments[i-1].code && segment.code {
				t.Fatalf("dialect=%d sql=%q: adjacent code segments at %d", dialectIndex, sqlStr, i)
			}
			joined.WriteString(segment.text)
		}
		if joined.String() != sqlStr {
			t.Fatalf("dialect=%d sql=%q: joined segments = %q", dialectIndex, sqlStr, joined.String())
		}
		again := splitSQLCode(sqlStr, syntax)
		if len(again) != len(segments) {
			t.Fatalf("dialect=%d sql=%q: segment count differs on second run: %d vs %d",
				dialectIndex, sqlStr, len(again), len(segments))
		}
		for i := range segments {
			if again[i] != segments[i] {
				t.Fatalf("dialect=%d sql=%q: segment %d differs on second run: %+v vs %+v",
					dialectIndex, sqlStr, i, again[i], segments[i])
			}
		}
	})
}
