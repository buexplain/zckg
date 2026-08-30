// 本文件为第二轮覆盖率补强的第二批纯单元测试：
// 覆盖 InsertUsing/InsertOrIgnoreUsing/Increment/Decrement/DeleteJoin 的编译错误分支、
// MySQL Upsert 空 updateColumns 的 no-op 退化 else 分支、
// CompileInsertOrIgnore 的多行分隔符与 Expression 内联、
// 三方言 join 条件的 IN/NOT IN/inSub-Not 分支、
// extractInsertData/extractUpdateData 的 nil 嵌入指针分支。
// 不依赖数据库连接。
package zcdb

import (
	"context"
	"errors"
	"testing"
)

// TestCov2_ExecMethodCompileErrors 覆盖写操作方法的编译期错误分支。
func TestCov2_ExecMethodCompileErrors(t *testing.T) {
	ctx := context.Background()
	g := NewMySQLGrammar()

	t.Run("InsertUsing", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Table("dst").InsertUsing(ctx, []string{"name"}, func(sub *Builder) {})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("InsertUsing 子查询缺表应报 ErrEmptyTable，实际 %v", err)
		}
	})
	t.Run("InsertOrIgnoreUsing", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Table("dst").InsertOrIgnoreUsing(ctx, []string{"name"}, func(sub *Builder) {})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("InsertOrIgnoreUsing 子查询缺表应报 ErrEmptyTable，实际 %v", err)
		}
	})
	t.Run("IncrementEmptyTable", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Force().Increment(ctx, "wallet", 1)
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("Increment 缺表应报 ErrEmptyTable，实际 %v", err)
		}
	})
	t.Run("DecrementOddExtra", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Decrement(ctx, "wallet", 1, "extra")
		if !errors.Is(err, ErrIncrementColumns) {
			t.Fatalf("Decrement 奇数 extra 应报 ErrIncrementColumns，实际 %v", err)
		}
	})
	t.Run("DecrementEmptyTable", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Force().Decrement(ctx, "wallet", 1)
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("Decrement 缺表应报 ErrEmptyTable，实际 %v", err)
		}
	})
	t.Run("DeleteJoinNoJoin", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Force().Table("t").DeleteJoin(ctx)
		if !errors.Is(err, ErrDeleteJoinNoJoin) {
			t.Fatalf("DeleteJoin 无 join 应报 ErrDeleteJoinNoJoin，实际 %v", err)
		}
	})
}

// TestCov2_MySQLUpsertEmptyUpdateColumnsElse 覆盖 MySQL CompileUpsert 在
// updateColumns 为空且 uniqueBy 为空时退化为 columns[:1] 的 else 分支。
// 该分支经公共 ToUpsert 不可达（空 updateColumns 会被展开为全部非 uniqueBy 列），
// 故以 grammar 直调方式锁死。
func TestCov2_MySQLUpsertEmptyUpdateColumnsElse(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("t")
	sql := g.CompileUpsert(b, []string{"id"}, [][]any{{int64(1)}}, nil, nil, nil)
	if sql == "" || sql != "INSERT INTO `t` (`id`) VALUES (?) ON DUPLICATE KEY UPDATE `id` = VALUES(`id`)" {
		t.Fatalf("空 updateColumns + 空 uniqueBy 应退化为首列自赋值，实际: %s", sql)
	}
}

// TestCov2_InsertOrIgnoreMultiRowAndExpression 覆盖 MySQL/SQLite 方言
// CompileInsertOrIgnore 的多行分隔符与 Expression 内联分支。
func TestCov2_InsertOrIgnoreMultiRowAndExpression(t *testing.T) {
	rows := [][]any{
		{int64(1), NewExpression("UPPER('a')")},
		{int64(2), "b"},
	}
	my := NewMySQLGrammar()
	if sql := my.CompileInsertOrIgnore(NewBuilder(my, nil).Table("t"), []string{"id", "name"}, rows); sql == "" {
		t.Fatal("MySQL CompileInsertOrIgnore 应产出 SQL")
	}
	li := NewSQLiteGrammar()
	if sql := li.CompileInsertOrIgnore(NewBuilder(li, nil).Table("t"), []string{"id", "name"}, rows); sql == "" {
		t.Fatal("SQLite CompileInsertOrIgnore 应产出 SQL")
	}
}

// TestCov2_JoinConditionInVariants 覆盖三方言 join 条件的
// IN 空列表（0=1）、NOT IN 列表、NOT IN 子查询分支。
func TestCov2_JoinConditionInVariants(t *testing.T) {
	grammars := map[string]Grammar{
		"mysql":    NewMySQLGrammar(),
		"postgres": NewPostgresGrammar(),
		"sqlite":   NewSQLiteGrammar(),
	}
	for name, g := range grammars {
		t.Run(name, func(t *testing.T) {
			sub := NewBuilder(g, nil).Table("t").Select("id")
			cond := func(jc []JoinCondition) string {
				switch gg := g.(type) {
				case *MySQLGrammar:
					return gg.compileJoinConditions(jc)
				case *PostgresGrammar:
					return gg.compileJoinConditions(jc)
				case *SQLiteGrammar:
					return gg.compileJoinConditions(jc)
				}
				return ""
			}
			if s := cond([]JoinCondition{{Type: "in", First: "a", Values: []any{}}}); s != "0 = 1" {
				t.Fatalf("IN 空列表应编译为 0 = 1，实际 %q", s)
			}
			if s := cond([]JoinCondition{{Type: "in", First: "a", Values: []any{1, 2}, Not: true}}); s == "" {
				t.Fatal("NOT IN 列表应编译出 SQL")
			}
			if s := cond([]JoinCondition{{Type: "inSub", First: "a", Sub: sub, Not: true}}); s == "" {
				t.Fatal("NOT IN 子查询应编译出 SQL")
			}
		})
	}
}

type Cov2EmbBase struct {
	Extra string `db:"extra"`
}

type Cov2EmbRow struct {
	*Cov2EmbBase
	Name string `db:"name"`
}

// TestCov2_NilEmbeddedPointerSkips 覆盖 extractInsertData/extractUpdateData
// 遇到 nil 嵌入指针时跳过字段（或按 NULL 处理）的分支。
func TestCov2_NilEmbeddedPointerSkips(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("InsertSingleRowNilEmbed", func(t *testing.T) {
		// 首（唯一）行嵌入指针为 nil：收集列时跳过嵌入字段
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]Cov2EmbRow{{Name: "x"}})
		if err != nil {
			t.Fatalf("nil 嵌入指针首行应跳过嵌入列而非报错: %v", err)
		}
	})
	t.Run("InsertLaterRowNilEmbed", func(t *testing.T) {
		// 首行嵌入指针非 nil（确定列），后行为 nil（该列按 NULL）
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]Cov2EmbRow{
			{Cov2EmbBase: &Cov2EmbBase{Extra: "e"}, Name: "a"},
			{Name: "b"},
		})
		if err != nil {
			t.Fatalf("后行 nil 嵌入指针应按 NULL 处理: %v", err)
		}
	})
	t.Run("UpdateNilEmbed", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("t").ToUpdate(Cov2EmbRow{Name: "x"})
		if err != nil {
			t.Fatalf("更新时 nil 嵌入指针应跳过嵌入列: %v", err)
		}
	})

	type Cov2NilPtrRow struct {
		P    *string `db:"p"`
		Name string  `db:"name"`
	}
	strPtr := func(s string) *string { return &s }
	t.Run("InsertNilPtrFieldFirstRow", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]Cov2NilPtrRow{{Name: "x"}})
		if err != nil {
			t.Fatalf("首行 nil 指针字段应跳过列: %v", err)
		}
	})
	t.Run("InsertNilPtrFieldLaterRow", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]Cov2NilPtrRow{
			{P: strPtr("a"), Name: "x"},
			{Name: "y"},
		})
		if err != nil {
			t.Fatalf("后行 nil 指针字段应按 NULL 处理: %v", err)
		}
	})
}
