// 本文件为 ScanStruct 的 SQLite 往返 fuzz：
// 以内存 SQLite 驱动的返回类型为 oracle，覆盖 TEXT/INTEGER/REAL/BLOB/NULL、
// 单行/多行/指针切片、未匹配列与空结果。纯 Go 驱动，无外部数据库依赖。

package zcdb

import (
	"bytes"
	"database/sql"
	"errors"
	"math"
	"testing"

	_ "modernc.org/sqlite"
)

type fuzzScanRow struct {
	S string  `db:"s"`
	I int64   `db:"i"`
	R float64 `db:"r"`
	B []byte  `db:"b"`
	N *string `db:"nullable"`
}

type fuzzScanNullRow struct {
	N string `db:"nullable"`
}

// FuzzScanRoundTrip 校验 SQLite 写入后 ScanStruct 的往返与 NULL/列映射语义：
// shape=0 单结构体、1 []结构体、2 []*结构体、3 NULL→string 零值、4 空结果。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzScanRoundTrip$' -fuzztime=30s
func FuzzScanRoundTrip(f *testing.F) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		f.Fatalf("open sqlite: %v", err)
	}
	db.SetMaxOpenConns(1)
	f.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE fuzz_scan (
		id INTEGER PRIMARY KEY,
		s TEXT, i INTEGER, r REAL, b BLOB, nullable TEXT, extra TEXT
	)`); err != nil {
		f.Fatalf("create table: %v", err)
	}

	seeds := []struct {
		shape       uint8
		s           string
		i           int64
		r           float64
		b           []byte
		nullableSet bool
		nullable    string
	}{
		{0, "alice", 42, 1.5, []byte("bin"), true, "note"},
		{1, "", 0, -0.25, nil, false, ""},
		{2, "中文 ' \" \\", -7, 1e3, []byte{0x00, 0xff}, true, ""},
		{3, "x", 1, 2.5, []byte("y"), false, ""},
		{3, "x", 1, 2.5, []byte("y"), true, "n"},
		{4, "x", 1, 2.5, []byte("y"), true, "n"},
	}
	for _, seed := range seeds {
		f.Add(seed.shape, seed.s, seed.i, seed.r, seed.b, seed.nullableSet, seed.nullable)
	}

	f.Fuzz(func(t *testing.T, shape uint8, s string, i int64, r float64, b []byte, nullableSet bool, nullable string) {
		if len(s) > 4<<10 || len(b) > 4<<10 || len(nullable) > 4<<10 || math.IsNaN(r) || math.IsInf(r, 0) {
			return
		}
		shape %= 5
		if _, err := db.Exec(`DELETE FROM fuzz_scan`); err != nil {
			t.Fatalf("delete: %v", err)
		}

		var nullableArg any
		if nullableSet {
			nullableArg = nullable
		}
		var blobArg any
		if b != nil {
			blobArg = b
		}
		insert := func(id int64) {
			if _, err := db.Exec(
				`INSERT INTO fuzz_scan (id, s, i, r, b, nullable, extra) VALUES (?, ?, ?, ?, ?, ?, ?)`,
				id, s, i, r, blobArg, nullableArg, "ignored",
			); err != nil {
				t.Fatalf("insert id=%d: %v", id, err)
			}
		}

		switch shape {
		case 0:
			insert(1)
			rows, err := db.Query(`SELECT s, i, r, b, nullable FROM fuzz_scan WHERE id = 1`)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			var got fuzzScanRow
			if err := ScanStructClose(rows, &got); err != nil {
				t.Fatalf("scan: %v", err)
			}
			assertFuzzScanRow(t, got, s, i, r, b, nullableSet, nullable)
		case 1, 2:
			insert(1)
			insert(2)
			rows, err := db.Query(`SELECT s, i, r, b, nullable FROM fuzz_scan ORDER BY id`)
			if err != nil {
				t.Fatalf("query: %v", err)
			}
			if shape == 1 {
				var got []fuzzScanRow
				if err := ScanStructClose(rows, &got); err != nil {
					t.Fatalf("scan slice: %v", err)
				}
				if len(got) != 2 {
					t.Fatalf("scan slice rows = %d, want 2", len(got))
				}
				for _, row := range got {
					assertFuzzScanRow(t, row, s, i, r, b, nullableSet, nullable)
				}
				return
			}
			var got []*fuzzScanRow
			if err := ScanStructClose(rows, &got); err != nil {
				t.Fatalf("scan ptr slice: %v", err)
			}
			if len(got) != 2 || got[0] == nil || got[1] == nil {
				t.Fatalf("scan ptr slice = %v, want 2 non-nil", got)
			}
			for _, row := range got {
				assertFuzzScanRow(t, *row, s, i, r, b, nullableSet, nullable)
			}
		case 3:
			insert(1)
			rows, err := db.Query(`SELECT nullable FROM fuzz_scan WHERE id = 1`)
			if err != nil {
				t.Fatalf("query nullable: %v", err)
			}
			var got fuzzScanNullRow
			if err := ScanStructClose(rows, &got); err != nil {
				t.Fatalf("scan nullable: %v", err)
			}
			want := ""
			if nullableSet {
				want = nullable
			}
			if got.N != want {
				t.Fatalf("nullable scan = %q, want %q (set=%v)", got.N, want, nullableSet)
			}
		default:
			insert(1)
			rows, err := db.Query(`SELECT s, i, r, b, nullable FROM fuzz_scan WHERE id = 999`)
			if err != nil {
				t.Fatalf("query empty: %v", err)
			}
			var single fuzzScanRow
			if err := ScanStructClose(rows, &single); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("empty single scan err = %v, want sql.ErrNoRows", err)
			}
			rows, err = db.Query(`SELECT s, i, r, b, nullable FROM fuzz_scan WHERE id = 999`)
			if err != nil {
				t.Fatalf("query empty slice: %v", err)
			}
			var all []fuzzScanRow
			if err := ScanStructClose(rows, &all); err != nil {
				t.Fatalf("empty slice scan: %v", err)
			}
			if len(all) != 0 {
				t.Fatalf("empty slice scan rows = %d, want 0", len(all))
			}
		}
	})
}

func assertFuzzScanRow(t *testing.T, got fuzzScanRow, s string, i int64, r float64, b []byte, nullableSet bool, nullable string) {
	t.Helper()
	if got.S != s || got.I != i || got.R != r || !bytes.Equal(got.B, b) {
		t.Fatalf("scan row = %+v, want s=%q i=%d r=%v b=%x", got, s, i, r, b)
	}
	if nullableSet {
		if got.N == nil || *got.N != nullable {
			t.Fatalf("nullable = %v, want %q", got.N, nullable)
		}
		return
	}
	if got.N != nil {
		t.Fatalf("nullable = %q, want nil", *got.N)
	}
}
