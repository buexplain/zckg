// 本文件为第二轮覆盖率补强的纯单元测试：
// 覆盖 ToCount/ToExists/ToAggregate 的 b.err 短路、终端方法对编译错误的传播、
// extractInsertData/extractUpdateData 的非法入参分支、
// JoinBuilder 零值防御分支与 join 条件嵌套环检测递归分支。
// 不依赖数据库连接。
package zcdb

import (
	"context"
	"errors"
	"testing"
)

// TestCov2_ToMethodsErrShortCircuit 补 ToCount/ToExists/ToAggregate 的 b.err 短路分支。
func TestCov2_ToMethodsErrShortCircuit(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("ToCount", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToCount(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToExists", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToExists(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToAggregate", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToAggregate("MAX", "age"); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
}

// TestCov2_TerminalMethodsPropagateCompileError 验证各终端方法在
// Builder 未设置表名时把 ErrEmptyTable 传播给调用方（编译期错误分支）。
func TestCov2_TerminalMethodsPropagateCompileError(t *testing.T) {
	ctx := context.Background()
	g := NewMySQLGrammar()
	type row struct {
		ID int64 `db:"id"`
	}

	t.Run("First", func(t *testing.T) {
		var r row
		if err := NewBuilder(g, nil).First(ctx, &r); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("PluckSlice", func(t *testing.T) {
		var ids []int64
		if err := NewBuilder(g, nil).Pluck(ctx, &ids, "id"); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("PluckKeyBy", func(t *testing.T) {
		m := map[int64]row{}
		if err := NewBuilder(g, nil).Pluck(ctx, &m, "id"); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("Count", func(t *testing.T) {
		if _, err := NewBuilder(g, nil).Count(ctx); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("Exists", func(t *testing.T) {
		if _, err := NewBuilder(g, nil).Exists(ctx); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("Max", func(t *testing.T) {
		if _, err := NewBuilder(g, nil).Max(ctx, "age"); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("Value", func(t *testing.T) {
		var v int64
		if err := NewBuilder(g, nil).Select("id").Value(ctx, &v); !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("Cursor", func(t *testing.T) {
		var r row
		var got error
		for err := range NewBuilder(g, nil).Cursor(ctx, &r) {
			got = err
		}
		if !errors.Is(got, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", got)
		}
	})
	t.Run("CursorBy", func(t *testing.T) {
		var r row
		var got error
		for err := range NewBuilder(g, nil).CursorBy(ctx, &r, 10, "id") {
			got = err
		}
		if !errors.Is(got, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", got)
		}
	})
}

// TestCov2_ExtractDataInvalidInputs 覆盖 extractInsertData / extractUpdateData
// 对 nil、含 nil 指针元素的切片、无字段结构体等非法输入的防御分支。
func TestCov2_ExtractDataInvalidInputs(t *testing.T) {
	ctx := context.Background()
	g := NewMySQLGrammar()

	t.Run("InsertNil", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Table("users").Insert(ctx, nil)
		if !errors.Is(err, ErrInvalidStruct) {
			t.Fatalf("expected ErrInvalidStruct, got %v", err)
		}
	})
	t.Run("InsertSliceFirstNilPtr", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Table("users").Insert(ctx, []*userInsert{nil})
		if !errors.Is(err, ErrInvalidStruct) {
			t.Fatalf("expected ErrInvalidStruct, got %v", err)
		}
	})
	t.Run("InsertSliceMiddleNilPtr", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Table("users").Insert(ctx, []*userInsert{{Name: "a"}, nil})
		if !errors.Is(err, ErrInvalidStruct) {
			t.Fatalf("expected ErrInvalidStruct, got %v", err)
		}
	})
	t.Run("UpdateNil", func(t *testing.T) {
		_, err := NewBuilder(g, nil).Table("users").Force().Update(ctx, nil)
		if !errors.Is(err, ErrInvalidStruct) {
			t.Fatalf("expected ErrInvalidStruct, got %v", err)
		}
	})
	t.Run("UpdateNoFields", func(t *testing.T) {
		type noFields struct{ hidden string } //nolint:unused // 无导出字段即无映射列
		_, err := NewBuilder(g, nil).Table("users").Force().Update(ctx, noFields{hidden: "x"})
		if !errors.Is(err, ErrNoFields) {
			t.Fatalf("expected ErrNoFields, got %v", err)
		}
	})
}

// TestCov2_JoinBuilderZeroValueWhereExists 覆盖零值 JoinBuilder（无 grammar）
// 调用 WhereExists 时累积 ErrInvalidSubQuery 的防御分支。
func TestCov2_JoinBuilderZeroValueWhereExists(t *testing.T) {
	var j JoinBuilder
	j.WhereExists(func(q *Builder) {})
	if !errors.Is(j.err, ErrInvalidSubQuery) {
		t.Fatalf("expected ErrInvalidSubQuery, got %v", j.err)
	}
}

// TestCov2_JoinConditionAcyclicNested 覆盖 checkJoinConditionAcyclic
// 递归进入 Nested 条件组（含子条件与子 join）的分支：
// 无环结构应通过，嵌套自引用应报 ErrCyclicQuery。
func TestCov2_JoinConditionAcyclicNested(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("nested acyclic", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.JoinOn("orders", func(j *JoinBuilder) {
			j.On("orders.user_id", "=", "users.id").
				WhereNested(func(n *JoinBuilder) {
					n.On("orders.status", "=", "paid").
						JoinOn("payments", func(p *JoinBuilder) {
							p.On("payments.order_id", "=", "orders.id")
						})
				})
		})
		if _, _, err := b.ToSelect(); err != nil {
			t.Fatalf("嵌套 join 条件无环时应正常编译: %v", err)
		}
	})
}
