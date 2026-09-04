// 本文件为 Builder 编译/构造/终端方法的错误前置分支补充单元测试。
// 覆盖各 To* 方法的 b.err != nil 短路、空表名 ErrEmptyTable、
// 终端方法（First/Pluck/Count/Exists/Max/Value/Cursor/CursorBy）与便捷写方法
// （InsertUsing/Increment/Decrement/DeleteJoin）的编译错误传播，
// 以及 JoinBuilder/addJoinOn/addJoinSub/addNested/WhereExists/addHavingBasic 的 error 累积分支。
// 仅验证错误分支，不依赖数据库连接。
package zcdb

import (
	"context"
	"errors"
	"testing"
)

// TestBuilder_ToMethodsErrShortCircuit 验证各 To* 方法在 b.err 已累积时直接返回该错误。
func TestBuilder_ToMethodsErrShortCircuit(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("ToSelect", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToInsert", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToInsert(userInsert{Name: "a"}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToInsertOrIgnore", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToInsertOrIgnore(userInsert{Name: "a"}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToUpsert", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToUpsert(userInsert{Name: "a"}, []string{"id"}, nil); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToInsertUsing", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToInsertUsing([]string{"a"}, func(sub *Builder) {}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToInsertOrIgnoreUsing", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToInsertOrIgnoreUsing([]string{"a"}, func(sub *Builder) {}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToUpdate", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToUpdate(userInsert{Name: "a"}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToDelete", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToDelete(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToDeleteJoin", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToDeleteJoin(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToTruncate", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, err := b.ToTruncate(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
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
		if _, _, err := b.ToAggregate("MAX", "id"); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToIncrement", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToIncrement([]string{"age"}, []any{1}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("ToDecrement", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users")
		b.err = ErrInvalidOperator
		if _, _, err := b.ToDecrement([]string{"age"}, []any{1}); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
}

// TestBuilder_ToMethodsEmptyTable 验证各 To* 方法未设置表名时返回 ErrEmptyTable。
func TestBuilder_ToMethodsEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("ToInsert", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToInsert(userInsert{Name: "a"})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("ToInsertOrIgnore", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToInsertOrIgnore(userInsert{Name: "a"})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("ToUpsert", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToUpsert(userInsert{Name: "a"}, []string{"id"}, nil)
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("ToUpdate", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToUpdate(userInsert{Name: "a"})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("ToInsertOrIgnoreUsing", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToInsertOrIgnoreUsing([]string{"a"}, func(sub *Builder) {})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("ToIncrement", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToIncrement([]string{"age"}, []any{1})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
	t.Run("ToDecrement", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).ToDecrement([]string{"age"}, []any{1})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable, got %v", err)
		}
	})
}

// TestBuilder_ToInsertUsingSubErrors 验证 ToInsertUsing/ToInsertOrIgnoreUsing 的子查询错误分支：
// 子查询自身 err、子查询无数据源、子查询环、列数不匹配。
func TestBuilder_ToInsertUsingSubErrors(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("sub_err", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertUsing([]string{"a"}, func(sub *Builder) {
			sub.Table("src").Where("x", "INVALID", 1)
		})
		if !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator from sub, got %v", err)
		}
	})
	t.Run("sub_empty_table", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertUsing([]string{"a"}, func(sub *Builder) {})
		if !errors.Is(err, ErrEmptyTable) {
			t.Fatalf("expected ErrEmptyTable from empty sub, got %v", err)
		}
	})
	t.Run("sub_cyclic", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertUsing([]string{"a"}, func(sub *Builder) {
			sub.Table("src")
			sub.TableSub(sub, "x")
		})
		if !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery from cyclic sub, got %v", err)
		}
	})
	t.Run("column_mismatch", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertUsing([]string{"a", "b"}, func(sub *Builder) {
			sub.Table("src").Select("c")
		})
		if !errors.Is(err, ErrInsertUsingColumnMismatch) {
			t.Fatalf("expected ErrInsertUsingColumnMismatch, got %v", err)
		}
	})
	t.Run("or_ignore_sub_err", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertOrIgnoreUsing([]string{"a"}, func(sub *Builder) {
			sub.Table("src").Where("x", "INVALID", 1)
		})
		if !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator from sub, got %v", err)
		}
	})
	t.Run("or_ignore_cyclic", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertOrIgnoreUsing([]string{"a"}, func(sub *Builder) {
			sub.Table("src")
			sub.TableSub(sub, "x")
		})
		if !errors.Is(err, ErrCyclicQuery) {
			t.Fatalf("expected ErrCyclicQuery from cyclic sub, got %v", err)
		}
	})
	t.Run("or_ignore_mismatch", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("dst")
		_, _, err := b.ToInsertOrIgnoreUsing([]string{"a", "b"}, func(sub *Builder) {
			sub.Table("src").Select("c")
		})
		if !errors.Is(err, ErrInsertUsingColumnMismatch) {
			t.Fatalf("expected ErrInsertUsingColumnMismatch, got %v", err)
		}
	})
}

// TestBuilder_ToUpsertUniqueByRequired 验证 PG/SQLite 下 ToUpsert 未指定 uniqueBy 时返回错误。
func TestBuilder_ToUpsertUniqueByRequired(t *testing.T) {
	for _, g := range []Grammar{NewPostgresGrammar(), NewSQLiteGrammar()} {
		_, _, err := NewBuilder(g, nil).Table("users").ToUpsert(userInsert{Name: "a"}, nil, nil)
		if !errors.Is(err, ErrUpsertUniqueByRequired) {
			t.Fatalf("expected ErrUpsertUniqueByRequired, got %v", err)
		}
	}
}

// TestBuilder_ToAggregateInvalid 验证 ToAggregate 非法聚合函数返回 ErrInvalidAggregate。
func TestBuilder_ToAggregateInvalid(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).Table("users").ToAggregate("MEDIAN", "id")
	if !errors.Is(err, ErrInvalidAggregate) {
		t.Fatalf("expected ErrInvalidAggregate, got %v", err)
	}
}

// TestBuilder_ToIncrementInvalidArgs 验证 toIncDec 列/值数量不一致返回 ErrIncrementColumns。
func TestBuilder_ToIncrementInvalidArgs(t *testing.T) {
	g := NewMySQLGrammar()
	if _, _, err := NewBuilder(g, nil).Table("users").ToIncrement(nil, nil); !errors.Is(err, ErrIncrementColumns) {
		t.Fatalf("expected ErrIncrementColumns for empty columns, got %v", err)
	}
	if _, _, err := NewBuilder(g, nil).Table("users").ToIncrement([]string{"a"}, nil); !errors.Is(err, ErrIncrementColumns) {
		t.Fatalf("expected ErrIncrementColumns for length mismatch, got %v", err)
	}
}

// TestJoinBuilder_ErrorAccumulation 验证 JoinBuilder 错误累积：无效运算符经 On/Where 累积，
// 最终在 ToSelect 返回。
func TestJoinBuilder_ErrorAccumulation(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("On_invalid_op", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
			jb.On("orders.user_id", "INVALID", "users.id")
		})
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("Where_invalid_op", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
			jb.Where("orders.status", "INVALID", 1)
		})
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("WhereIn_invalid_values", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
			jb.WhereIn("orders.status", 123)
		})
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidWhereInValues) {
			t.Fatalf("expected ErrInvalidWhereInValues, got %v", err)
		}
	})
	t.Run("WhereExists_invalid_sub", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
			jb.WhereExists(123)
		})
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidSubQuery) {
			t.Fatalf("expected ErrInvalidSubQuery, got %v", err)
		}
	})
	t.Run("Nested_err_propagation", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
			jb.WhereNested(func(q *JoinBuilder) {
				q.On("orders.x", "INVALID", "orders.y")
			})
		})
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator from nested, got %v", err)
		}
	})
}

// TestJoinBuilder_JoinOnInvalidOp 验证 JoinBuilder.JoinOn 嵌套 join 的错误传播。
func TestJoinBuilder_JoinOnInvalidOp(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		jb.JoinOn("order_items", func(inner *JoinBuilder) {
			inner.On("order_items.x", "INVALID", "order_items.y")
		})
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
		t.Fatalf("expected ErrInvalidOperator from nested join, got %v", err)
	}
}

// TestBuilder_HavingInvalidOp 验证 addHavingBasic 的 op 非 string 与非法运算符分支。
func TestBuilder_HavingInvalidOp(t *testing.T) {
	g := NewMySQLGrammar()

	t.Run("op_not_string", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("orders").GroupBy("status").Having("cnt", 123, 456)
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
	t.Run("invalid_operator", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("orders").GroupBy("status").Having("cnt", "INVALID", 456)
		if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
			t.Fatalf("expected ErrInvalidOperator, got %v", err)
		}
	})
}

// TestBuilder_JoinSubInvalidOp 验证 JoinSub 的 callback 错误累积。
func TestBuilder_JoinSubInvalidOp(t *testing.T) {
	g := NewMySQLGrammar()
	sub := NewBuilder(g, nil).Table("logs").Select("user_id")
	b := NewBuilder(g, nil).Table("users").JoinSub(sub, "l", func(jb *JoinBuilder) {
		jb.On("l.user_id", "INVALID", "users.id")
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrInvalidOperator) {
		t.Fatalf("expected ErrInvalidOperator, got %v", err)
	}
}

// TestBuilder_TerminalMethodsPropagateCompileError 验证各终端方法在
// Builder 未设置表名时把 ErrEmptyTable 传播给调用方（编译期错误分支）。
func TestBuilder_TerminalMethodsPropagateCompileError(t *testing.T) {
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

// TestBuilder_ExecMethodCompileErrors 覆盖便捷写方法（非 To* 层）的编译期错误分支：
// 与 To* 层共用同一错误码，此处锁定经便捷方法入口的传播路径。
func TestBuilder_ExecMethodCompileErrors(t *testing.T) {
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

// TestJoinBuilder_ZeroValueWhereExists 覆盖零值 JoinBuilder（无 grammar）
// 调用 WhereExists 时累积 ErrInvalidSubQuery 的防御分支。
func TestJoinBuilder_ZeroValueWhereExists(t *testing.T) {
	var j JoinBuilder
	j.WhereExists(func(q *Builder) {})
	if !errors.Is(j.err, ErrInvalidSubQuery) {
		t.Fatalf("expected ErrInvalidSubQuery, got %v", j.err)
	}
}

// TestBuilder_ExtractErrorPropagation 覆盖 Insert/Update 终端方法对
// extractInsertData / extractUpdateData 非法入参错误的传播分支：
// nil、含 nil 指针元素的切片、无字段结构体。
func TestBuilder_ExtractErrorPropagation(t *testing.T) {
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

// EmbBase / EmbRow 为导出嵌入类型：嵌入指针为 nil 时才会进入
// fieldByIndexSafe 的「嵌入字段不可用」分支（非导出嵌入指针在 parse 阶段即被跳过）。
type EmbBase struct {
	Extra string `db:"extra"`
}

type EmbRow struct {
	*EmbBase
	Name string `db:"name"`
}

// TestBuilder_NilEmbeddedPointerSkips 覆盖 ToInsert/ToUpdate 编译路径中
// extract 遇到 nil 嵌入指针（或 nil 指针字段）时跳过列/按 NULL 处理的分支。
func TestBuilder_NilEmbeddedPointerSkips(t *testing.T) {
	g := NewMySQLGrammar()
	strPtr := func(s string) *string { return &s }

	t.Run("InsertSingleRowNilEmbed", func(t *testing.T) {
		// 首（唯一）行嵌入指针为 nil：收集列时跳过嵌入字段
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]EmbRow{{Name: "x"}})
		if err != nil {
			t.Fatalf("nil 嵌入指针首行应跳过嵌入列而非报错: %v", err)
		}
	})
	t.Run("InsertLaterRowNilEmbed", func(t *testing.T) {
		// 首行嵌入指针非 nil（确定列），后行为 nil（该列按 NULL）
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]EmbRow{
			{EmbBase: &EmbBase{Extra: "e"}, Name: "a"},
			{Name: "b"},
		})
		if err != nil {
			t.Fatalf("后行 nil 嵌入指针应按 NULL 处理: %v", err)
		}
	})
	t.Run("UpdateNilEmbed", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("t").ToUpdate(EmbRow{Name: "x"})
		if err != nil {
			t.Fatalf("更新时 nil 嵌入指针应跳过嵌入列: %v", err)
		}
	})

	type nilPtrRow struct {
		P    *string `db:"p"`
		Name string  `db:"name"`
	}
	t.Run("InsertNilPtrFieldFirstRow", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]nilPtrRow{{Name: "x"}})
		if err != nil {
			t.Fatalf("首行 nil 指针字段应跳过列: %v", err)
		}
	})
	t.Run("InsertNilPtrFieldLaterRow", func(t *testing.T) {
		_, _, err := NewBuilder(g, nil).Table("t").ToInsert([]nilPtrRow{
			{P: strPtr("a"), Name: "x"},
			{Name: "y"},
		})
		if err != nil {
			t.Fatalf("后行 nil 指针字段应按 NULL 处理: %v", err)
		}
	})
}
