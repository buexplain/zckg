package zcdb

import (
	"errors"
	"testing"
)

// TestBuilder_CompileEntriesCyclicGuard 验证各编译入口（ToDelete/ToDeleteJoin/ToCount/
// ToExists/ToAggregate/ToIncrement/ToDecrement/ToInsertOrIgnoreUsing）的 validateAcyclic 错误分支：
// 自引用子查询在这些入口同样返回 ErrCyclicQuery 而非递归崩溃。
func TestBuilder_CompileEntriesCyclicGuard(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("ToDelete", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.JoinSub(b, "x", nil)
		if _, _, err := b.ToDelete(); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToDeleteJoin", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.JoinSub(b, "x", nil)
		if _, _, err := b.ToDeleteJoin(); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToCount", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.TableSub(b, "x")
		if _, _, err := b.ToCount(); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToExists", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.TableSub(b, "x")
		if _, _, err := b.ToExists(); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToAggregate", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.TableSub(b, "x")
		if _, _, err := b.ToAggregate("MAX", "id"); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToIncrement", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.JoinSub(b, "x", nil)
		if _, _, err := b.ToIncrement([]string{"age"}, []any{1}); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToDecrement", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.JoinSub(b, "x", nil)
		if _, _, err := b.ToDecrement([]string{"age"}, []any{1}); !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery, got %v", err)
		}
	})
	t.Run("ToInsertOrIgnoreUsing_empty_sub", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertOrIgnoreUsing([]string{"a"}, func(sub *Builder) {})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
}
