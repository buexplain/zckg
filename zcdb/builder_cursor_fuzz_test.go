// 本文件为 CursorBy 键集分页的 fuzz 测试：
// 以内存 SQLite 固定 32 行数据为 oracle，校验 chunkSize 0/负数/1..8、升降序、
// 提前 break、NULL 游标终止，以及“全量返回且不重不漏、顺序正确”的分页不变量。

package zcdb

import (
	"context"
	"errors"
	"fmt"
	"testing"

	_ "modernc.org/sqlite"
)

type fuzzCursorRow struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

type fuzzCursorNullRow struct {
	ID       int64   `db:"id"`
	Nullable *string `db:"nullable"`
}

// FuzzCursorByPagination 校验 CursorBy 的分页语义：
// shape=0 全量迭代（含 chunkSize=0/-1/1..8 与 ASC/DESC），
// shape=1 首行后 break 能正常停止，shape=2 游标列为 NULL 时返回 ErrCursorColumnNull。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzCursorByPagination$' -fuzztime=30s
func FuzzCursorByPagination(f *testing.F) {
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:", MaxOpenConns: 1})
	if err != nil {
		f.Fatalf("open sqlite: %v", err)
	}
	dao, err := NewDBDao(pool, "sqlite", nil, "", "")
	if err != nil {
		f.Fatalf("create dao: %v", err)
	}
	f.Cleanup(func() { _ = dao.Close() })
	ctx := context.Background()
	if _, err := dao.Exec(ctx, `CREATE TABLE fuzz_cursor (id INTEGER PRIMARY KEY, name TEXT, nullable TEXT)`); err != nil {
		f.Fatalf("create table: %v", err)
	}
	for id := 1; id <= 32; id++ {
		var nullable any
		if id != 1 {
			nullable = fmt.Sprintf("v%d", id)
		}
		if _, err := dao.Exec(ctx, `INSERT INTO fuzz_cursor (id, name, nullable) VALUES (?, ?, ?)`, id, fmt.Sprintf("n%d", id), nullable); err != nil {
			f.Fatalf("insert id=%d: %v", id, err)
		}
	}

	seeds := []struct {
		chunkRaw   uint8
		limitRaw   uint8
		desc       bool
		breakEarly bool
		shape      uint8
	}{
		{2, 32, false, false, 0},
		{0, 32, false, false, 0},
		{1, 32, false, false, 0},
		{7, 32, true, false, 0},
		{3, 5, false, false, 0},
		{3, 5, true, false, 0},
		{3, 32, false, true, 1},
		{3, 32, true, true, 1},
		{3, 32, false, false, 2},
		{3, 0, false, false, 2},
	}
	for _, seed := range seeds {
		f.Add(seed.chunkRaw, seed.limitRaw, seed.desc, seed.breakEarly, seed.shape)
	}

	f.Fuzz(func(t *testing.T, chunkRaw, limitRaw uint8, desc, breakEarly bool, shape uint8) {
		shape %= 3
		limit := int(limitRaw) % 33
		chunk := 1 + int(chunkRaw%8)
		if shape != 2 {
			switch chunkRaw % 5 {
			case 0:
				chunk = 0
			case 1:
				chunk = -1
			}
		}
		if shape == 2 {
			chunk = 2
		}

		if shape == 2 {
			var row fuzzCursorNullRow
			count := 0
			var gotErr error
			for err := range dao.Builder().Table("fuzz_cursor").Select("id", "nullable").
				Where("id", "<=", limit).
				CursorBy(ctx, &row, chunk, "nullable") {
				if err != nil {
					gotErr = err
					break
				}
				count++
				if count > 1000 {
					t.Fatal("CursorBy null cursor did not terminate")
				}
			}
			if limit == 0 {
				if gotErr != nil || count != 0 {
					t.Fatalf("limit=0 null cursor: err=%v count=%d, want nil/0", gotErr, count)
				}
				return
			}
			if !errors.Is(gotErr, ErrCursorColumnNull) {
				t.Fatalf("null cursor err = %v, want ErrCursorColumnNull (count=%d)", gotErr, count)
			}
			return
		}

		var row fuzzCursorRow
		var ids []int64
		for err := range dao.Builder().Table("fuzz_cursor").Select("id", "name").
			Where("id", "<=", limit).
			CursorBy(ctx, &row, chunk, "id", desc) {
			if err != nil {
				t.Fatalf("CursorBy shape=%d chunk=%d limit=%d desc=%v: %v", shape, chunk, limit, desc, err)
			}
			ids = append(ids, row.ID)
			if len(ids) > 1000 {
				t.Fatal("CursorBy did not terminate")
			}
			if shape == 1 && breakEarly {
				break
			}
		}

		if chunk == 0 {
			if len(ids) != 0 {
				t.Fatalf("chunkSize=0 returned %d rows, want 0", len(ids))
			}
			return
		}
		if shape == 1 && breakEarly {
			if len(ids) > 1 {
				t.Fatalf("break after first row returned %d rows", len(ids))
			}
			return
		}
		if len(ids) != limit {
			t.Fatalf("chunk=%d limit=%d desc=%v: got %d rows, want %d", chunk, limit, desc, len(ids), limit)
		}
		for i, id := range ids {
			want := int64(i + 1)
			if desc {
				want = int64(limit - i)
			}
			if id != want {
				t.Fatalf("chunk=%d limit=%d desc=%v: ids[%d]=%d, want %d (all=%v)", chunk, limit, desc, i, id, want, ids)
			}
		}
	})
}
