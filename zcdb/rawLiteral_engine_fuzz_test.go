// 本文件为 rawLiteral 的 SQLite 引擎差分 fuzz：
// fuzzer 选择值形态（nil/string/[]byte/整数/浮点/bool/time/命名类型/指针），
// inlineRawLiteral 生成 SQLite 字面量后执行 SELECT <literal>，用驱动读回值作为 oracle；
// 非空指针预期返回 ErrRawValueNotInlinable。MySQL/PG 的字面量差异需容器环境，另行夜间验证。

package zcdb

import (
	"bytes"
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

type fuzzRawNamedString string
type fuzzRawNamedBytes []byte

// FuzzRawLiteralSQLiteRoundTrip 校验 SQLite 字面量内联的往返一致性：
// 0 nil、1 string、2 []byte、3 int64、4 uint64、5 float64、6 bool、7 time.Time、
// 8 非空指针（预期错误）、9 nil 指针、10 命名 string、11 命名 []byte。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzRawLiteralSQLiteRoundTrip$' -fuzztime=30s
func FuzzRawLiteralSQLiteRoundTrip(f *testing.F) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		f.Fatalf("open sqlite: %v", err)
	}
	f.Cleanup(func() { _ = db.Close() })

	seeds := []struct {
		kind uint8
		text string
		i    int64
		u    uint64
		f    float64
		b    bool
	}{
		{0, "", 0, 0, 0, false},
		{1, "alice", 0, 0, 0, false},
		{1, "a'b\\c\n", 0, 0, 0, false},
		{2, string([]byte{0x00, 0xff}), 0, 0, 0, false},
		{3, "", -7, 0, 0, false},
		{4, "", 0, 42, 0, false},
		{5, "", 0, 0, 1.5, false},
		{6, "", 0, 0, 0, true},
		{7, "", 1700000000, 0, 0, false},
		{8, "ptr", 0, 0, 0, false},
		{9, "", 0, 0, 0, false},
		{10, "named", 0, 0, 0, false},
		{11, "bytes", 0, 0, 0, false},
	}
	for _, seed := range seeds {
		f.Add(seed.kind, seed.text, seed.i, seed.u, seed.f, seed.b)
	}

	f.Fuzz(func(t *testing.T, kind uint8, text string, i int64, u uint64, fv float64, b bool) {
		if len(text) > 4<<10 || math.IsNaN(fv) || math.IsInf(fv, 0) || u > math.MaxInt64 {
			return
		}
		var value any
		switch kind % 12 {
		case 0:
			value = nil
		case 1:
			if strings.ContainsRune(text, 0) {
				return // SQLite SQL 文本中的 NUL 语义不属于 TEXT 字面量往返范围
			}
			value = text
		case 2:
			value = []byte(text)
		case 3:
			value = i
		case 4:
			value = u
		case 5:
			value = fv
		case 6:
			value = b
		case 7:
			value = time.Unix(i, 0).UTC()
		case 8:
			ptr := text
			value = &ptr
		case 9:
			var ptr *string
			value = ptr
		case 10:
			if strings.ContainsRune(text, 0) {
				return
			}
			value = fuzzRawNamedString(text)
		default:
			value = fuzzRawNamedBytes([]byte(text))
		}

		literal, err := inlineRawLiteral(value, sqliteRawLiteralStyle, 1)
		if kind%12 == 8 {
			if !errors.Is(err, ErrRawValueNotInlinable) {
				t.Fatalf("non-nil pointer literal err = %v, want ErrRawValueNotInlinable", err)
			}
			return
		}
		if err != nil {
			t.Fatalf("inline %T: %v", value, err)
		}

		var got any
		query := "SELECT " + literal
		if kind%12 == 5 {
			// 整数值的浮点字面量（如 1.0 被格式化为 "1"）在裸 SELECT 中会被 SQLite
			// 当作整数返回；CAST AS REAL 固定按浮点语义读取。
			query = "SELECT CAST(" + literal + " AS REAL)"
		}
		if err := db.QueryRow(query).Scan(&got); err != nil {
			t.Fatalf("select literal %q: %v", literal, err)
		}
		switch kind % 12 {
		case 0, 9:
			if got != nil {
				t.Fatalf("literal %q read back %#v, want nil", literal, got)
			}
		case 1, 10:
			if got != text {
				t.Fatalf("literal %q read back %#v, want %q", literal, got, text)
			}
		case 2, 11:
			gotBytes, ok := got.([]byte)
			if !ok || !bytes.Equal(gotBytes, []byte(text)) {
				t.Fatalf("literal %q read back %#v, want bytes %x", literal, got, []byte(text))
			}
		case 3:
			if got != i {
				t.Fatalf("literal %q read back %#v, want %d", literal, got, i)
			}
		case 4:
			if got != int64(u) {
				t.Fatalf("literal %q read back %#v, want %d", literal, got, u)
			}
		case 5:
			gotFloat, ok := got.(float64)
			if !ok || gotFloat != fv {
				t.Fatalf("literal %q read back %#v, want %v", literal, got, fv)
			}
		case 6:
			want := int64(0)
			if b {
				want = 1
			}
			if got != want {
				t.Fatalf("literal %q read back %#v, want %d", literal, got, want)
			}
		case 7:
			want := time.Unix(i, 0).UTC().Format("2006-01-02 15:04:05.999999")
			if got != want {
				t.Fatalf("literal %q read back %#v, want %q", literal, got, want)
			}
		}
	})
}
