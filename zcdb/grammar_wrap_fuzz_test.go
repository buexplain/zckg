// 本文件为 WrapTable/WrapColumn 的标识符包裹 fuzz：
// 三方言校验包裹结果可按方言规则逆变换回原文（引号加倍是唯一转义），
// SQLite 额外把包裹结果作为列名与别名执行，验证解析器仍将其视为单个标识符。

package zcdb

import (
	"database/sql"
	"strings"
	"testing"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

type fuzzWrapDialect struct {
	name    string
	grammar Grammar
	quote   byte
}

// FuzzIdentifierWrapping 校验标识符引用不可破坏 SQL 结构：
// 0 WrapTable、1 WrapColumn；含 "("、点号或 " as " 的表达式/别名形态不属于标识符，跳过。
// 三方言做逆变换校验，SQLite 再执行列/别名查询；普通 go test 只运行种子，探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzIdentifierWrapping$' -fuzztime=30s
func FuzzIdentifierWrapping(f *testing.F) {
	dialects := []fuzzWrapDialect{
		{"MySQL", NewMySQLGrammar(), '`'},
		{"PostgreSQL", NewPostgresGrammar(), '"'},
		{"SQLite", NewSQLiteGrammar(), '"'},
	}
	seeds := []string{
		"users", "a b", "a\"b", "a`b", "a'b", "中文", "", "select", "a\nb",
		"a\\b", "/*x*/", "--x", "#x", "a;b", "a.b", "a(b", "a as b", "*",
	}
	for i := range dialects {
		for kind := uint8(0); kind < 2; kind++ {
			for _, seed := range seeds {
				f.Add(uint8(i), kind, seed)
			}
		}
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		f.Fatalf("open sqlite: %v", err)
	}
	f.Cleanup(func() {
		if err := db.Close(); err != nil {
			f.Errorf("close sqlite: %v", err)
		}
	})

	f.Fuzz(func(t *testing.T, dialectIndex, kind uint8, identifier string) {
		if len(identifier) > 4<<10 || strings.ContainsRune(identifier, 0) ||
			strings.Contains(identifier, ".") || strings.Contains(identifier, "(") ||
			strings.Contains(strings.ToLower(identifier), " as ") || identifier == "*" {
			return
		}
		dialect := dialects[int(dialectIndex)%len(dialects)]
		var wrapped string
		if kind%2 == 0 {
			wrapped = dialect.grammar.WrapTable(identifier)
		} else {
			wrapped = dialect.grammar.WrapColumn(identifier)
		}

		if got, err := fuzzUnquoteIdentifier(wrapped, dialect.quote); err != nil || got != identifier {
			t.Fatalf("dialect=%s kind=%d id=%q: wrapped=%q, unquote=%q err=%v", dialect.name, kind, identifier, wrapped, got, err)
		}

		if dialect.name != "SQLite" {
			return
		}
		if !utf8.ValidString(identifier) {
			return
		}
		if kind%2 == 0 {
			var value int
			if err := db.QueryRow("SELECT * FROM (SELECT 1 AS v) AS " + wrapped).Scan(&value); err != nil {
				t.Fatalf("SQLite table alias %q rejected: %v", wrapped, err)
			}
			if value != 1 {
				t.Fatalf("SQLite table alias %q returned %d, want 1", wrapped, value)
			}
			return
		}
		var value int
		query := "SELECT " + wrapped + " FROM (SELECT 1 AS " + wrapped + ")"
		if err := db.QueryRow(query).Scan(&value); err != nil {
			t.Fatalf("SQLite column %q rejected: %v (query=%s)", wrapped, err, query)
		}
		if value != 1 {
			t.Fatalf("SQLite column %q returned %d, want 1", wrapped, value)
		}
	})
}

// fuzzUnquoteIdentifier 按“同种引号双写”规则解引用，并要求内部不存在未转义引号；
// 这是 WrapTable/WrapColumn 的独立逆变换，用于发现漏转义或多余包裹。
func fuzzUnquoteIdentifier(wrapped string, quote byte) (string, error) {
	if len(wrapped) < 2 || wrapped[0] != quote || wrapped[len(wrapped)-1] != quote {
		return "", errFuzzWrap("missing quote delimiters")
	}
	inner := wrapped[1 : len(wrapped)-1]
	var builder strings.Builder
	for i := 0; i < len(inner); i++ {
		if inner[i] != quote {
			builder.WriteByte(inner[i])
			continue
		}
		if i+1 >= len(inner) || inner[i+1] != quote {
			return "", errFuzzWrap("unescaped quote inside identifier")
		}
		builder.WriteByte(quote)
		i++
	}
	return builder.String(), nil
}

type errFuzzWrap string

func (e errFuzzWrap) Error() string { return string(e) }
