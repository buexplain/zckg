// 本文件为 MySQL/PostgreSQL 真实引擎的字面量转义 fuzz（夜间门控）：
// 仅设置 ZCDB_ENGINE_FUZZ=1 时运行；数据库不可达时由 comment DAO helper 跳过。
// 重点验证字符串/字节在各方言转义后经真实引擎读回一致（SQLite 版见
// rawLiteral_engine_fuzz_test.go）；常量矩阵仍由 cross_dialect_integration_test.go 覆盖。

package zcdb

import (
	"context"
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzRawLiteralMySQLRoundTrip 夜间校验 MySQL 字面量转义往返。
// 运行：ZCDB_ENGINE_FUZZ=1 go test ./zcdb -run '^$' -fuzz '^FuzzRawLiteralMySQLRoundTrip$' -fuzztime=30s
func FuzzRawLiteralMySQLRoundTrip(f *testing.F) {
	if os.Getenv("ZCDB_ENGINE_FUZZ") == "" {
		f.Skip("set ZCDB_ENGINE_FUZZ=1 to run external engine fuzz (requires local MySQL)")
	}
	dao := openMySQLCommentDAO(f, "", nil)
	fuzzRawLiteralExternalEscaping(f, dao, mysqlRawLiteralStyle)
}

// FuzzRawLiteralPGRoundTrip 夜间校验 PostgreSQL 字面量转义往返。
// 运行：ZCDB_ENGINE_FUZZ=1 go test ./zcdb -run '^$' -fuzz '^FuzzRawLiteralPGRoundTrip$' -fuzztime=30s
func FuzzRawLiteralPGRoundTrip(f *testing.F) {
	if os.Getenv("ZCDB_ENGINE_FUZZ") == "" {
		f.Skip("set ZCDB_ENGINE_FUZZ=1 to run external engine fuzz (requires local PostgreSQL)")
	}
	dao := openPgCommentDAO(f, "", nil)
	fuzzRawLiteralExternalEscaping(f, dao, pgRawLiteralStyle)
}

func fuzzRawLiteralExternalEscaping(f *testing.F, dao *DBDao, style rawLiteralStyle) {
	seeds := []struct {
		kind uint8
		text string
	}{
		{0, ""},
		{1, "alice"},
		{1, "a'b\\c\n\r\t"},
		{1, "中文"},
		{1, ""},
		{2, string([]byte{0x00, 0xff, 0x41})},
		{2, ""},
		{3, "named"},
		{4, string([]byte{0x01, 0x02})},
	}
	for _, seed := range seeds {
		f.Add(seed.kind, seed.text)
	}

	ctx := context.Background()
	f.Fuzz(func(t *testing.T, kind uint8, text string) {
		if len(text) > 4<<10 {
			return
		}
		kind %= 5
		var value any
		switch kind {
		case 0:
			value = nil
		case 1:
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return
			}
			value = text
		case 2:
			value = []byte(text)
		case 3:
			if !utf8.ValidString(text) || strings.ContainsRune(text, 0) {
				return
			}
			value = fuzzRawNamedString(text)
		default:
			value = fuzzRawNamedBytes([]byte(text))
		}

		literal, err := inlineRawLiteral(value, style, 1)
		if err != nil {
			t.Fatalf("inline %T: %v", value, err)
		}
		rows, err := dao.Query(ctx, "SELECT "+literal)
		if err != nil {
			t.Fatalf("query literal %q: %v", literal, err)
		}
		if !rows.Next() {
			// 已准备立即 Fatal：关闭错误不会改变主失败原因，故刻意忽略。
			_ = rows.Close()
			t.Fatalf("literal %q returned no row: %v", literal, rows.Err())
		}
		var got any
		if err := rows.Scan(&got); err != nil {
			// 已准备立即 Fatal：关闭错误不会改变主失败原因，故刻意忽略。
			_ = rows.Close()
			t.Fatalf("scan literal %q: %v", literal, err)
		}
		if err := rows.Err(); err != nil {
			// 已准备立即 Fatal：关闭错误不会改变主失败原因，故刻意忽略。
			_ = rows.Close()
			t.Fatalf("rows err for literal %q: %v", literal, err)
		}
		if err := rows.Close(); err != nil {
			t.Fatalf("close rows for literal %q: %v", literal, err)
		}

		if value == nil {
			if got != nil {
				t.Fatalf("literal %q read back %#v, want nil", literal, got)
			}
			return
		}
		want := text
		if kind == 2 || kind == 4 {
			want = string([]byte(text))
		}
		if got == nil {
			t.Fatalf("literal %q read back nil, want %q", literal, want)
		}
		if gotText := driverValueText(t, got); gotText != want {
			t.Fatalf("literal %q read back %q, want %q", literal, gotText, want)
		}
	})
}
