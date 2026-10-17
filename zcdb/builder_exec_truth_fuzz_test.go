// 本文件为 WHERE 真值评估的 fuzz 测试：
// 字节序列编码条件原子（普通/空 Raw/空 IN/空 NOT IN/恒真 Raw/空 EXISTS/带条件 EXISTS/空嵌套）、
// OR 连接、嵌套组与 NOT；参考模型按 SQL AND 优先归约恒真/恒假/未知，
// 并联动 Update/Delete 的无 WHERE 保护验证拒绝路径。

package zcdb

import (
	"context"
	"errors"
	"testing"
)

// FuzzWhereTruthEvaluation 覆盖 evalWhereTruth 的 AND/OR 优先级、嵌套组与 NOT：
// 每个字节低 3 位选原子，bit3=OR 连接、bit4=嵌套组、bit5=NOT；首条件按 AND 处理。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzWhereTruthEvaluation$' -fuzztime=30s
func FuzzWhereTruthEvaluation(f *testing.F) {
	seeds := [][]byte{
		{0}, {1}, {2}, {3}, {4}, {5}, {6}, {7},
		{0, 8 | 2},      // unknown OR false → unknown
		{3, 8 | 0},      // true OR unknown → true
		{3, 2},          // true AND false → false
		{3, 8 | 3},      // true OR true → true
		{32 | 5},        // NOT (EXISTS empty=true) → false
		{32 | 3},        // NOT (NOT IN empty=true) → false
		{16 | 0},        // nested unknown
		{16 | 7},        // nested empty → absent
		{8 | 0},         // first OR ignored → unknown
		{0, 8 | 1},      // unknown OR absent → unknown
		{0, 8 | 32 | 3}, // unknown OR NOT(true) = unknown OR false → unknown
		{3, 8 | 32 | 5}, // true OR NOT(true)=false → true
	}
	for _, seed := range seeds {
		f.Add(seed, false, uint8(0))
		f.Add(seed, true, uint8(1))
	}

	f.Fuzz(func(t *testing.T, spec []byte, force bool, op uint8) {
		if len(spec) > 8 {
			return
		}
		builder := newTestBuilder(NewMySQLGrammar(), nil).Table("t")
		for i, code := range spec {
			atom := code & 7
			or := i > 0 && code&8 != 0
			nested := code&16 != 0
			not := code&32 != 0
			switch {
			case not:
				if or {
					builder.OrWhereNot(func(q *Builder) { addTruthAtom(q, atom) })
				} else {
					builder.WhereNot(func(q *Builder) { addTruthAtom(q, atom) })
				}
			case nested:
				if or {
					builder.OrWhereNested(func(q *Builder) { addTruthAtom(q, atom) })
				} else {
					builder.WhereNested(func(q *Builder) { addTruthAtom(q, atom) })
				}
			default:
				if or {
					addTruthAtomOr(builder, atom)
				} else {
					addTruthAtom(builder, atom)
				}
			}
		}

		wantTruth := fuzzTruthSequence(spec)
		wantEffective := wantTruth == truthUnknown || wantTruth == truthFalse
		if got := builder.hasEffectiveWhere(); got != wantEffective {
			t.Fatalf("spec=%v truth=%v: hasEffectiveWhere=%v, want %v", spec, wantTruth, got, wantEffective)
		}
		if force || wantEffective {
			return
		}

		ctx := context.Background()
		var err error
		if op%2 == 0 {
			err = callGuardWithoutDAO(func() error {
				_, err := builder.Update(ctx, fuzzGuardUpdate{V: "x"})
				return err
			})
			if !errors.Is(err, ErrUpdateWithoutWhere) {
				t.Fatalf("spec=%v Update err=%v, want ErrUpdateWithoutWhere", spec, err)
			}
			return
		}
		err = callGuardWithoutDAO(func() error {
			_, err := builder.Delete(ctx)
			return err
		})
		if !errors.Is(err, ErrDeleteWithoutWhere) {
			t.Fatalf("spec=%v Delete err=%v, want ErrDeleteWithoutWhere", spec, err)
		}
	})
}

func addTruthAtom(builder *Builder, atom uint8) {
	switch atom {
	case 0:
		builder.Where("id", "=", 1)
	case 1:
		builder.WhereRaw("")
	case 2:
		builder.WhereIn("id", []any{})
	case 3:
		builder.WhereNotIn("id", []any{})
	case 4:
		builder.WhereRaw("1=1")
	case 5:
		builder.WhereExists(func(*Builder) {})
	case 6:
		builder.WhereExists(func(q *Builder) {
			q.Table("x").Select("1").Where("x.id", "=", 1)
		})
	default:
		builder.WhereNested(func(*Builder) {})
	}
}

func addTruthAtomOr(builder *Builder, atom uint8) {
	switch atom {
	case 0:
		builder.OrWhere("id", "=", 1)
	case 1:
		builder.OrWhereRaw("")
	case 2:
		builder.OrWhereNested(func(q *Builder) { q.WhereIn("id", []any{}) })
	case 3:
		builder.OrWhereNested(func(q *Builder) { q.WhereNotIn("id", []any{}) })
	case 4:
		builder.OrWhereRaw("1=1")
	case 5:
		builder.OrWhereExists(func(*Builder) {})
	case 6:
		builder.OrWhereExists(func(q *Builder) {
			q.Table("x").Select("1").Where("x.id", "=", 1)
		})
	default:
		builder.OrWhereNested(func(*Builder) {})
	}
}

// fuzzTruthSequence 独立实现 SQL 的 AND 优先归约：按 OR 切分 AND 链，链内 AND，链间 OR；
// absent 条件不参与；NOT 对 true/false 取反，unknown/absent 保持。
func fuzzTruthSequence(spec []byte) whereTruth {
	result, chain := truthAbsent, truthAbsent
	for i, code := range spec {
		truth := fuzzTruthAtom(code & 7)
		if code&32 != 0 {
			switch truth {
			case truthTrue:
				truth = truthFalse
			case truthFalse:
				truth = truthTrue
			}
		}
		if truth == truthAbsent {
			continue
		}
		or := i > 0 && code&8 != 0
		if or && chain != truthAbsent {
			result = fuzzOrTruth(result, chain)
			chain = truthAbsent
		}
		chain = fuzzAndTruth(chain, truth)
	}
	return fuzzOrTruth(result, chain)
}

func fuzzTruthAtom(atom uint8) whereTruth {
	switch atom {
	case 0, 4, 6:
		return truthUnknown
	case 1, 7:
		return truthAbsent
	case 2:
		return truthFalse
	default: // 3, 5
		return truthTrue
	}
}

func fuzzAndTruth(a, b whereTruth) whereTruth {
	switch {
	case a == truthAbsent:
		return b
	case b == truthAbsent:
		return a
	case a == truthFalse || b == truthFalse:
		return truthFalse
	case a == truthUnknown || b == truthUnknown:
		return truthUnknown
	default:
		return truthTrue
	}
}

func fuzzOrTruth(a, b whereTruth) whereTruth {
	switch {
	case a == truthAbsent:
		return b
	case b == truthAbsent:
		return a
	case a == truthTrue || b == truthTrue:
		return truthTrue
	case a == truthUnknown || b == truthUnknown:
		return truthUnknown
	default:
		return truthFalse
	}
}
