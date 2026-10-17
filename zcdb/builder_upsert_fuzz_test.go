// 本文件为 Upsert / InsertUsing 选项矩阵的 fuzz：
// fuzzer 组合 uniqueBy、updateColumns、目标列数与子查询列数，校验方言错误规则、
// 参数与占位符一致、跨方言 args 一致，以及“全部插入列均为冲突列”时的退化行为。

package zcdb

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

type fuzzUpsertRow struct {
	A string  `db:"a"`
	B int64   `db:"b"`
	C *string `db:"c"`
}

// FuzzUpsertInsertUsingMatrix 覆盖：
// shape=0 ToUpsert；shape=1 ToInsertUsing；shape=2 ToInsertOrIgnoreUsing。
// UPSERT 校验 uniqueBy 为空时 PG/SQLite 的 ErrUpsertUniqueByRequired、MySQL 放行，
// 以及 updateColumns 为空时的推导/DO NOTHING 退化；InsertUsing 校验列数不匹配错误
// 与成功时唯一绑定参数。普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzUpsertInsertUsingMatrix$' -fuzztime=30s
func FuzzUpsertInsertUsingMatrix(f *testing.F) {
	seeds := []struct {
		shape uint8
		flags uint8
		a     string
		b     int64
		cSet  bool
		value string
	}{
		{0, 0, "a", 1, false, "v"},
		{0, 1, "a", 1, true, "v"},
		{0, 2, "a", 1, false, "v"},
		{0, 3, "a", 1, false, "v"},
		{0, 3 | 4, "a", 1, false, "v"},
		{0, 3 | 8, "a", 1, false, "v"},
		{0, 3 | 12, "a", 1, false, "v"},
		{1, 0, "a", 1, false, "v"},
		{1, 1, "a", 1, false, "v"},
		{1, 2, "a", 1, false, "v"},
		{1, 3, "a", 1, false, "v"},
		{2, 0, "a", 1, false, "v"},
		{2, 1, "a", 1, false, "v"},
	}
	for _, seed := range seeds {
		f.Add(seed.shape, seed.flags, seed.a, seed.b, seed.cSet, seed.value)
	}

	f.Fuzz(func(t *testing.T, shape, flags uint8, a string, b int64, cSet bool, value string) {
		if len(a) > 4<<10 || len(value) > 4<<10 {
			return
		}
		shape %= 3
		row := fuzzUpsertRow{A: a, B: b}
		if cSet {
			c := value
			row.C = &c
		}

		dialects := []builderFuzzDialect{
			{"MySQL", NewMySQLGrammar(), mysqlScanSyntax},
			{"PostgreSQL", NewPostgresGrammar(), pgScanSyntax},
			{"SQLite", NewSQLiteGrammar(), sqliteScanSyntax},
		}

		if shape == 0 {
			uniqueBy, updateColumns := fuzzUpsertOptions(flags)
			inserted := []string{"a", "b"}
			if cSet {
				inserted = append(inserted, "c")
			}
			var successArgs []any
			successName := ""
			for _, dialect := range dialects {
				builder := newTestBuilder(dialect.grammar, nil).Table("t")
				sql1, args1, err1 := builder.ToUpsert(row, uniqueBy, updateColumns)
				sql2, args2, err2 := builder.ToUpsert(row, uniqueBy, updateColumns)
				if !fuzzSameError(err1, err2) || sql1 != sql2 || !reflect.DeepEqual(args1, args2) {
					t.Fatalf("dialect=%s upsert repeated compile differs:\n(%q,%v,%v)\n(%q,%v,%v)",
						dialect.name, sql1, args1, err1, sql2, args2, err2)
				}

				wantErr := len(uniqueBy) == 0 && dialect.name != "MySQL"
				if wantErr {
					if !errors.Is(err1, ErrUpsertUniqueByRequired) {
						t.Fatalf("dialect=%s upsert uniqueBy empty err=%v, want ErrUpsertUniqueByRequired", dialect.name, err1)
					}
					continue
				}
				if err1 != nil {
					t.Fatalf("dialect=%s upsert unexpected err: %v", dialect.name, err1)
				}
				wantArgs := []any{a, b}
				if cSet {
					wantArgs = append(wantArgs, value)
				}
				if !reflect.DeepEqual(args1, wantArgs) {
					t.Fatalf("dialect=%s upsert args=%v, want %v", dialect.name, args1, wantArgs)
				}
				fuzzAssertPlaceholders(t, dialect, sql1, len(args1))
				wantUpdate := fuzzExpectedUpdateColumns(dialect.name, uniqueBy, updateColumns, inserted)
				fuzzAssertUpsertUpdateClause(t, dialect, sql1, uniqueBy, wantUpdate)
				if successName == "" {
					successName, successArgs = dialect.name, args1
				} else if !reflect.DeepEqual(successArgs, args1) {
					t.Fatalf("upsert args differ: %s=%v, %s=%v", successName, successArgs, dialect.name, args1)
				}
			}
			return
		}

		columns := fuzzInsertUsingColumns(flags)
		wantErr := len(columns) != 2
		successArgs := []any(nil)
		successName := ""
		for _, dialect := range dialects {
			builder := newTestBuilder(dialect.grammar, nil).Table("archive")
			run := func() (string, []any, error) {
				callback := func(sub *Builder) {
					sub.Table("src").Select("a", "b").Where("x", "=", value)
				}
				if shape == 1 {
					return builder.ToInsertUsing(columns, callback)
				}
				return builder.ToInsertOrIgnoreUsing(columns, callback)
			}
			sql1, args1, err1 := run()
			sql2, args2, err2 := run()
			if !fuzzSameError(err1, err2) || sql1 != sql2 || !reflect.DeepEqual(args1, args2) {
				t.Fatalf("dialect=%s insertUsing repeated compile differs", dialect.name)
			}
			if wantErr {
				if !errors.Is(err1, ErrInsertUsingColumnMismatch) {
					t.Fatalf("dialect=%s columns=%v err=%v, want ErrInsertUsingColumnMismatch", dialect.name, columns, err1)
				}
				continue
			}
			if err1 != nil {
				t.Fatalf("dialect=%s insertUsing unexpected err: %v", dialect.name, err1)
			}
			wantArgs := []any{value}
			if !reflect.DeepEqual(args1, wantArgs) {
				t.Fatalf("dialect=%s insertUsing args=%v, want %v", dialect.name, args1, wantArgs)
			}
			fuzzAssertPlaceholders(t, dialect, sql1, len(args1))
			if successName == "" {
				successName, successArgs = dialect.name, args1
			} else if !reflect.DeepEqual(successArgs, args1) {
				t.Fatalf("insertUsing args differ: %s=%v, %s=%v", successName, successArgs, dialect.name, args1)
			}
		}
	})
}

func fuzzUpsertOptions(flags uint8) (uniqueBy, updateColumns []string) {
	switch flags & 3 {
	case 1:
		uniqueBy = []string{"a"}
	case 2:
		uniqueBy = []string{"b"}
	case 3:
		uniqueBy = []string{"a", "b"}
	}
	switch (flags >> 2) & 3 {
	case 1:
		updateColumns = []string{"a"}
	case 2:
		updateColumns = []string{"b", "a"}
	case 3:
		updateColumns = []string{"c"}
	}
	return uniqueBy, updateColumns
}

// fuzzExpectedUpdateColumns 独立推导冲突时实际更新的列：
// 显式 updateColumns 原样使用；为空时先展开为「插入列 - uniqueBy」（ToUpsert 的统一规则）；
// 展开后仍为空时 PG/SQLite 退化为 DO NOTHING，MySQL 按 Grammar 规则选第一个同时出现在
// uniqueBy 与插入列中的列，都不匹配时回退插入首列，做 no-op 自赋值。
func fuzzExpectedUpdateColumns(dialectName string, uniqueBy, updateColumns, inserted []string) []string {
	expected := updateColumns
	if len(expected) == 0 {
		excluded := make(map[string]bool, len(uniqueBy))
		for _, col := range uniqueBy {
			excluded[col] = true
		}
		for _, col := range inserted {
			if !excluded[col] {
				expected = append(expected, col)
			}
		}
	}
	if len(expected) > 0 || dialectName != "MySQL" {
		return expected
	}
	for _, candidate := range uniqueBy {
		for _, col := range inserted {
			if col == candidate {
				return []string{col}
			}
		}
	}
	if len(inserted) > 0 {
		return []string{inserted[0]}
	}
	return nil
}

// fuzzAssertUpsertUpdateClause 按方言断言冲突目标与更新子句的列集合/顺序：
// PG/SQLite 校验 ON CONFLICT 目标列，以及 DO NOTHING 或 DO UPDATE SET 的 EXCLUDED 引用；
// MySQL 校验 ON DUPLICATE KEY UPDATE 的 VALUES() 自引用。
func fuzzAssertUpsertUpdateClause(t *testing.T, dialect builderFuzzDialect, sqlStr string, uniqueBy, updateColumns []string) {
	t.Helper()
	if dialect.name == "MySQL" {
		want := "ON DUPLICATE KEY UPDATE "
		for i, col := range updateColumns {
			if i > 0 {
				want += ", "
			}
			wrapped := dialect.grammar.WrapColumn(col)
			want += wrapped + " = VALUES(" + wrapped + ")"
		}
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("MySQL upsert SQL=%q, want update clause %q", sqlStr, want)
		}
		return
	}

	wantConflict := "ON CONFLICT ("
	for i, col := range uniqueBy {
		if i > 0 {
			wantConflict += ", "
		}
		wantConflict += dialect.grammar.WrapColumn(col)
	}
	wantConflict += ")"
	if !strings.Contains(sqlStr, wantConflict) {
		t.Fatalf("%s upsert SQL=%q, want conflict target %q", dialect.name, sqlStr, wantConflict)
	}

	if len(updateColumns) == 0 {
		if !strings.Contains(sqlStr, "DO NOTHING") {
			t.Fatalf("%s upsert all-conflict SQL=%q, want DO NOTHING", dialect.name, sqlStr)
		}
		return
	}
	wantUpdate := "DO UPDATE SET "
	for i, col := range updateColumns {
		if i > 0 {
			wantUpdate += ", "
		}
		wrapped := dialect.grammar.WrapColumn(col)
		wantUpdate += wrapped + " = EXCLUDED." + wrapped
	}
	if !strings.Contains(sqlStr, wantUpdate) {
		t.Fatalf("%s upsert SQL=%q, want update clause %q", dialect.name, sqlStr, wantUpdate)
	}
}

func fuzzInsertUsingColumns(flags uint8) []string {
	switch flags & 3 {
	case 0:
		return []string{"a", "b"}
	case 1:
		return []string{"a"}
	case 2:
		return []string{"a", "b", "c"}
	default:
		return nil
	}
}

func fuzzAssertPlaceholders(t *testing.T, dialect builderFuzzDialect, sqlStr string, argCount int) {
	t.Helper()
	if dialect.name == "PostgreSQL" {
		assertSequentialPlaceholders(t, dialect.name, sqlStr, dialect.syntax, argCount)
		return
	}
	if got := codePlaceholderCount(sqlStr, dialect.syntax, dialect.name); got != argCount {
		t.Fatalf("dialect=%s sql=%q: %d placeholder(s), %d arg(s)", dialect.name, sqlStr, got, argCount)
	}
}
