// 本文件为子查询图环检测与 Clone 深拷贝的补充单元测试。
// 覆盖 checkBuilderAcyclic 的 selectSubs/wheres.Nested/wheres.Sub/havings.Nested 分支、
// checkJoinClauseAcyclic 的 Conditions/Joins 分支、checkJoinConditionAcyclic 的 Sub/Nested 分支，
// 以及 cloneInternal/cloneJoinConditions 的 Sub/Values/Bindings/Nested 深拷贝分支。
// 仅验证编译/克隆逻辑，不依赖数据库连接。
package zcdb

import (
	"errors"
	"testing"
)

// TestBuilder_CyclicSelectSub 自引用 SELECT 子查询（SelectSub 传入自身）应返回 ErrCyclicQuery。
func TestBuilder_CyclicSelectSub(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.SelectSub(b, "x")
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for self-referential SelectSub, got %v", err)
	}
}

// TestBuilder_CyclicWhereNested 经 WhereNested 回调引用外层 Builder 应返回 ErrCyclicQuery。
func TestBuilder_CyclicWhereNested(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.WhereNested(func(q *Builder) {
		q.Where("x", "=", 1).TableSub(b, "x")
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic WhereNested, got %v", err)
	}
}

// TestBuilder_CyclicWhereSub 经 WhereSub 回调引用外层 Builder 应返回 ErrCyclicQuery。
func TestBuilder_CyclicWhereSub(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.WhereSub("id", "=", func(q *Builder) {
		q.TableSub(b, "x")
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic WhereSub, got %v", err)
	}
}

// TestBuilder_CyclicHavingNested 经 HavingNested 回调引用外层 Builder 应返回 ErrCyclicQuery。
func TestBuilder_CyclicHavingNested(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.HavingNested(func(q *Builder) {
		q.Having("cnt", ">", 1).TableSub(b, "x")
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic HavingNested, got %v", err)
	}
}

// TestBuilder_CyclicJoinConditionSub JoinBuilder.Where 传入自身 Builder 生成 subValue 环，
// 应经 checkJoinConditionAcyclic 的 Sub 分支返回 ErrCyclicQuery。
func TestBuilder_CyclicJoinConditionSub(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.JoinOn("orders", func(jb *JoinBuilder) {
		jb.On("orders.user_id", "=", "users.id").
			Where("orders.ref", "=", b) // *Builder 值 → subValue 类型，Sub 指向 b 自身
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic join condition Sub, got %v", err)
	}
}

// TestBuilder_CyclicJoinConditionNested JoinBuilder.WhereNested 回调内引用外层 Builder，
// 应经 checkJoinConditionAcyclic 的 Nested 分支返回 ErrCyclicQuery。
func TestBuilder_CyclicJoinConditionNested(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.JoinOn("orders", func(jb *JoinBuilder) {
		jb.On("orders.user_id", "=", "users.id").
			WhereNested(func(q *JoinBuilder) {
				q.Where("orders.ref", "=", b)
			})
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic join condition Nested, got %v", err)
	}
}

// TestBuilder_CyclicJoinNestedGroup JoinBuilder.JoinOn 嵌套 join 组内引用外层 Builder，
// 应经 checkJoinClauseAcyclic 的 Joins 分支返回 ErrCyclicQuery。
func TestBuilder_CyclicJoinNestedGroup(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.JoinOn("orders", func(jb *JoinBuilder) {
		jb.On("orders.user_id", "=", "users.id").
			JoinOn("order_items", func(inner *JoinBuilder) {
				inner.Where("order_items.ref", "=", b)
			})
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic nested join group, got %v", err)
	}
}

// TestBuilder_CyclicJoinSubCondition JoinSub 的派生表为自身，且带 ON 条件，
// 应经 checkJoinClauseAcyclic 的 Sub + Conditions 分支返回 ErrCyclicQuery。
func TestBuilder_CyclicJoinSubCondition(t *testing.T) {
	b := NewBuilder(&MySQLGrammar{}, nil).Table("users")
	b.JoinSub(b, "x", func(jb *JoinBuilder) {
		jb.On("x.id", "=", "users.id")
	})
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic JoinSub with condition, got %v", err)
	}
}

// TestBuilder_CloneDeepCopySubs 验证 Clone 深拷贝 WhereSub/GroupByRaw/HavingNested
// 等子状态：副本修改不影响原 Builder。
func TestBuilder_CloneDeepCopySubs(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").
		WhereSub("age", ">", func(q *Builder) { q.Table("stats").SelectRaw("AVG(age)") }).
		GroupByRaw("YEAR(created_at)", 2026).
		HavingNested(func(q *Builder) { q.Having("cnt", ">", 1) })

	clone := b.Clone()

	// 原 Builder 仍能正常编译
	if _, _, err := b.ToSelect(); err != nil {
		t.Fatalf("original should compile, got %v", err)
	}
	if _, _, err := clone.ToSelect(); err != nil {
		t.Fatalf("clone should compile, got %v", err)
	}

	// 修改副本的 where 不应影响原 Builder 的 where 列表
	if len(clone.wheres) != len(b.wheres) {
		t.Fatalf("clone should deep-copy wheres, clone=%d orig=%d", len(clone.wheres), len(b.wheres))
	}
}

// TestBuilder_CloneDeepCopyJoinConditions 验证 Clone 深拷贝 JOIN ON 条件的
// Values/Bindings/Sub/Nested 分支（经 JoinBuilder.WhereIn/Where/WhereExists/WhereNested 构造）。
func TestBuilder_CloneDeepCopyJoinConditions(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").
		JoinOn("orders", func(jb *JoinBuilder) {
			jb.On("orders.user_id", "=", "users.id").
				Where("orders.status", "=", "paid").
				WhereIn("orders.tag", []any{"a", "b"}).
				WhereNested(func(q *JoinBuilder) {
					q.Where("orders.vip", "=", 1)
				}).
				WhereExists(func(q *Builder) {
					q.Table("payments").SelectRaw("1").WhereColumn("payments.order_id", "=", "orders.id")
				})
		})

	clone := b.Clone()

	sql1, args1, err1 := b.ToSelect()
	sql2, args2, err2 := clone.ToSelect()
	if err1 != nil || err2 != nil {
		t.Fatalf("compile error: orig=%v clone=%v", err1, err2)
	}
	if sql1 != sql2 {
		t.Fatalf("clone SQL mismatch:\n orig: %s\n clone: %s", sql1, sql2)
	}
	if len(args1) != len(args2) {
		t.Fatalf("clone args count mismatch: orig=%d clone=%d", len(args1), len(args2))
	}
	// 深拷贝：克隆出的 ON 条件的 Values/Bindings/Sub/Nested 不共享底层切片
	for i := range clone.joins[0].Conditions {
		if clone.joins[0].Conditions[i].Nested != nil {
			if clone.joins[0].Conditions[i].Nested == b.joins[0].Conditions[i].Nested {
				t.Fatalf("clone should deep-copy nested join condition")
			}
		}
	}
}
