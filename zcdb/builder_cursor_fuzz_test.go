// 本文件为 CursorBy 键集分页的 fuzz 测试：
// 以内存 SQLite 固定 32 行数据为 oracle，校验 chunkSize 0/负数/1..8、升降序、
// 提前 break、NULL 游标终止、int/string/限定游标列，以及“全量返回且不重不漏”的分页不变量；
// 另含 context 取消的确定性故障注入。

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
	Code string `db:"code"`
	Name string `db:"name"`
}

type fuzzCursorNullRow struct {
	ID       int64   `db:"id"`
	Nullable *string `db:"nullable"`
}

// FuzzCursorEmbedded 必须是导出类型：未导出匿名嵌入字段会被字段收集器跳过，
// 那样 row 里根本不存在 id 索引，只会得到 ErrCursorFieldNotFound，无法覆盖 ErrCursorFieldUnavailable。
type FuzzCursorEmbedded struct {
	ID int64 `db:"id"`
}

type fuzzCursorUnavailableRow struct {
	*FuzzCursorEmbedded
	Name string `db:"name"`
}

// FuzzCursorByPagination 校验 CursorBy 的分页语义：
// shape=0 全量迭代（含 chunkSize=0/-1/1..8 与 ASC/DESC），
// shape=1 首行后 break 能正常停止，shape=2 游标列为 NULL 时返回 ErrCursorColumnNull，
// shape=3 游标字段不存在时返回 ErrCursorFieldNotFound，
// shape=4 nil 嵌入指针导致游标字段不可用时返回 ErrCursorFieldUnavailable；
// columnKind 选择 id / code / fuzz_cursor.id 三种游标列。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzCursorByPagination$' -fuzztime=30s
func FuzzCursorByPagination(f *testing.F) {
	// 复用统一建连 helper：SQLite 内存库需单连接，DAO 默认注入测试注释。
	dao := openSQLiteTestDB(f)
	setupFuzzCursorTable(f, dao)

	seeds := []struct {
		chunkRaw   uint8
		limitRaw   uint8
		desc       bool
		breakEarly bool
		shape      uint8
		columnKind uint8
	}{
		{2, 32, false, false, 0, 0},
		{0, 32, false, false, 0, 0},
		{1, 32, false, false, 0, 0},
		{7, 32, true, false, 0, 0},
		{3, 5, false, false, 0, 0},
		{3, 5, true, false, 0, 1},
		{3, 32, false, false, 0, 1},
		{3, 32, true, false, 0, 1},
		{3, 32, false, false, 0, 2},
		{3, 32, false, true, 1, 0},
		{3, 32, true, true, 1, 1},
		{3, 32, false, false, 2, 0},
		{3, 0, false, false, 2, 0},
		{3, 32, false, false, 3, 0},
		{3, 32, false, false, 4, 0},
	}
	for _, seed := range seeds {
		f.Add(seed.chunkRaw, seed.limitRaw, seed.desc, seed.breakEarly, seed.shape, seed.columnKind)
	}

	f.Fuzz(func(t *testing.T, chunkRaw, limitRaw uint8, desc, breakEarly bool, shape, columnKind uint8) {
		shape %= 5
		columnKind %= 3
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
		if (shape == 3 || shape == 4) && limit == 0 {
			// 错误分支必须先有扫描行才会触发；limit=0 时查询为空。
			limit = 1
		}

		if shape == 3 || shape == 4 {
			collectErr := func(dest any, cursorColumn string) error {
				for err := range dao.Builder().Table("fuzz_cursor").Select("id", "code", "name").
					Where("id", "<=", limit).
					CursorBy(context.Background(), dest, 2, cursorColumn) {
					if err != nil {
						return err
					}
					return fmt.Errorf("unexpected successful row before error (column %s)", cursorColumn)
				}
				return nil
			}
			if shape == 3 {
				var row fuzzCursorRow
				if err := collectErr(&row, "missing"); !errors.Is(err, ErrCursorFieldNotFound) {
					t.Fatalf("missing cursor field err = %v, want ErrCursorFieldNotFound", err)
				}
				return
			}
			var row fuzzCursorUnavailableRow
			if err := collectErr(&row, "id"); !errors.Is(err, ErrCursorFieldUnavailable) {
				t.Fatalf("unavailable cursor field err = %v, want ErrCursorFieldUnavailable", err)
			}
			return
		}

		if shape == 2 {
			var row fuzzCursorNullRow
			count := 0
			var gotErr error
			for err := range dao.Builder().Table("fuzz_cursor").Select("id", "nullable").
				Where("id", "<=", limit).
				CursorBy(context.Background(), &row, chunk, "nullable") {
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

		cursorColumn := []string{"id", "code", "fuzz_cursor.id"}[columnKind]
		var row fuzzCursorRow
		var ids []int64
		for err := range dao.Builder().Table("fuzz_cursor").Select("id", "code", "name").
			Where("id", "<=", limit).
			CursorBy(context.Background(), &row, chunk, cursorColumn, desc) {
			if err != nil {
				t.Fatalf("CursorBy shape=%d column=%s chunk=%d limit=%d desc=%v: %v", shape, cursorColumn, chunk, limit, desc, err)
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
			t.Fatalf("column=%s chunk=%d limit=%d desc=%v: got %d rows, want %d", cursorColumn, chunk, limit, desc, len(ids), limit)
		}
		for i, id := range ids {
			want := int64(i + 1)
			if desc {
				want = int64(limit - i)
			}
			if id != want {
				t.Fatalf("column=%s chunk=%d limit=%d desc=%v: ids[%d]=%d, want %d (all=%v)", cursorColumn, chunk, limit, desc, i, id, want, ids)
			}
		}
	})
}

// setupFuzzCursorTable 创建 32 行固定数据；code 为 k01..k32，与 id 同序；
// id=1 的 nullable 为 NULL，用于 NULL 游标终止用例。
func setupFuzzCursorTable(t testing.TB, dao *DBDao) {
	t.Helper()
	ctx := context.Background()
	if _, err := dao.Exec(ctx, `CREATE TABLE fuzz_cursor (id INTEGER PRIMARY KEY, code TEXT UNIQUE, name TEXT, nullable TEXT)`); err != nil {
		t.Fatalf("create cursor table: %v", err)
	}
	for id := 1; id <= 32; id++ {
		var nullable any
		if id != 1 {
			nullable = fmt.Sprintf("v%d", id)
		}
		if _, err := dao.Exec(ctx, `INSERT INTO fuzz_cursor (id, code, name, nullable) VALUES (?, ?, ?, ?)`,
			id, fmt.Sprintf("k%02d", id), fmt.Sprintf("n%d", id), nullable); err != nil {
			t.Fatalf("insert cursor id=%d: %v", id, err)
		}
	}
}

// TestCursorBy_ContextCancel 验证分页中途取消 context 会向上返回错误并终止，
// 而不是把“错误截断”当成正常迭代完成静默丢数据。
func TestCursorBy_ContextCancel(t *testing.T) {
	dao := openSQLiteTestDB(t)
	setupFuzzCursorTable(t, dao)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var row fuzzCursorRow
	count := 0
	var gotErr error
	for err := range dao.Builder().Table("fuzz_cursor").Select("id", "code", "name").CursorBy(ctx, &row, 1, "id") {
		if err != nil {
			gotErr = err
			break
		}
		count++
		if count == 3 {
			cancel()
		}
		if count > 100 {
			t.Fatal("CursorBy did not terminate after context cancel")
		}
	}
	if !errors.Is(gotErr, context.Canceled) {
		t.Fatalf("CursorBy cancel err=%v count=%d, want context.Canceled", gotErr, count)
	}
	if count != 3 {
		t.Fatalf("CursorBy cancel count=%d, want 3", count)
	}
}
