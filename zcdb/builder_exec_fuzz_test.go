// 本文件为无 WHERE 破坏性操作保护的 fuzz 测试：
// fuzzer 组合 WHERE 条件形态（普通/空 Raw/空 IN/NOT IN/空嵌套/EXISTS/恒真 Raw）、
// JOIN 条件与 Force 标记，参考模型判定是否应拒绝；拒绝时用 nil DAO 调用
// Update/Delete/DeleteJoin，证明保护在访问数据库前生效。

package zcdb

import (
	"context"
	"errors"
	"fmt"
	"testing"
)

type fuzzGuardUpdate struct {
	V string `db:"v"`
}

// FuzzDestructiveGuard 覆盖 Update/Delete/DeleteJoin 的无 WHERE 保护：
// op=0 Update、1 Delete、2 DeleteJoin；whereFlags/joinFlags 选择条件形态，
// force 控制显式放行。参考模型独立推导 where/join 是否构成有效限定。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzDestructiveGuard$' -fuzztime=30s
func FuzzDestructiveGuard(f *testing.F) {
	seeds := []struct {
		op         uint8
		force      bool
		whereFlags uint8
		joinFlags  uint8
		operator   string
		value      string
	}{
		{0, false, 0, 0, "=", "x"},       // Update 无任何条件 → 拒绝
		{0, true, 0, 0, "=", "x"},        // Force 放行
		{0, false, 1, 0, "=", "x"},       // 普通 WHERE 放行
		{0, false, 2, 0, "=", "x"},       // 空 Raw 不构成限定 → 拒绝
		{0, false, 4, 0, "=", "x"},       // 空 IN（恒假）仍能限定 → 放行
		{0, false, 8, 0, "=", "x"},       // 空 NOT IN（恒真）→ 拒绝
		{0, false, 16, 0, "=", "x"},      // 空嵌套组 → 拒绝
		{0, false, 32, 0, "=", "x"},      // 空 EXISTS（恒真）→ 拒绝
		{0, false, 64, 0, "=", "x"},      // 带条件 EXISTS → 放行
		{0, false, 128, 0, "=", "x"},     // WhereRaw("1=1") 逃生口 → 放行
		{0, false, 0, 2, "=", "x"},       // 有效 ON 条件 → 放行
		{0, false, 0, 1, "=", "x"},       // 无条件 JOIN → 拒绝
		{1, false, 0, 0, "=", "x"},       // Delete 无条件 → 拒绝
		{1, false, 1, 0, "=", "x"},       // Delete 有 WHERE → 放行
		{1, false, 0, 2, "=", "x"},       // Delete 带 JOIN → ErrDeleteWithJoin
		{2, false, 0, 0, "=", "x"},       // DeleteJoin 仅无条件 JOIN → 拒绝
		{2, false, 0, 2, "=", "x"},       // DeleteJoin 有效 ON → 放行
		{0, false, 1, 0, "INVALID", "x"}, // 非法运算符，跳过判定
	}
	for _, seed := range seeds {
		f.Add(seed.op, seed.force, seed.whereFlags, seed.joinFlags, seed.operator, seed.value)
	}

	f.Fuzz(func(t *testing.T, op uint8, force bool, whereFlags, joinFlags uint8, operator, value string) {
		if len(operator) > 256 || len(value) > 4<<10 {
			return
		}
		op %= 3
		joinFlags &= 0x0F // 仅低 4 位有对应 JOIN 形态
		builder := buildGuardFuzzBuilder(NewMySQLGrammar(), op, force, whereFlags, joinFlags, operator, value)
		if err := builder.validateWriteBuilder(); err != nil {
			return
		}

		wantWhere := fuzzGuardWhereEffective(whereFlags)
		wantJoin := fuzzGuardJoinEffective(joinFlags)
		if got := builder.hasEffectiveWhere(); got != wantWhere {
			t.Fatalf("op=%d whereFlags=%#x: hasEffectiveWhere=%v, want %v", op, whereFlags, got, wantWhere)
		}
		if got := builder.hasEffectiveJoin(); got != wantJoin {
			t.Fatalf("op=%d joinFlags=%#x: hasEffectiveJoin=%v, want %v", op, joinFlags, got, wantJoin)
		}

		ctx := context.Background()
		switch op {
		case 0:
			reject := !force && !wantWhere && !wantJoin
			if !reject {
				return
			}
			err := callGuardWithoutDAO(func() error {
				_, err := builder.Update(ctx, fuzzGuardUpdate{V: "x"})
				return err
			})
			if !errors.Is(err, ErrUpdateWithoutWhere) {
				t.Fatalf("Update with no effective where/join returned %v, want ErrUpdateWithoutWhere", err)
			}
		case 1:
			if joinFlags != 0 {
				err := callGuardWithoutDAO(func() error {
					_, err := builder.Delete(ctx)
					return err
				})
				if !errors.Is(err, ErrDeleteWithJoin) {
					t.Fatalf("Delete with joins returned %v, want ErrDeleteWithJoin", err)
				}
				return
			}
			if force || wantWhere {
				return
			}
			err := callGuardWithoutDAO(func() error {
				_, err := builder.Delete(ctx)
				return err
			})
			if !errors.Is(err, ErrDeleteWithoutWhere) {
				t.Fatalf("Delete with no effective where returned %v, want ErrDeleteWithoutWhere", err)
			}
		default:
			if force || wantWhere || wantJoin {
				return
			}
			err := callGuardWithoutDAO(func() error {
				_, err := builder.DeleteJoin(ctx)
				return err
			})
			if !errors.Is(err, ErrDeleteWithoutWhere) {
				t.Fatalf("DeleteJoin with no effective where/join returned %v, want ErrDeleteWithoutWhere", err)
			}
		}
	})
}

// callGuardWithoutDAO 调用预期在 DAO 访问前返回的保护路径；
// 若错误地放行到 DAO，nil DAO 会 panic，这里转成可读错误而不是让 fuzz 栈直接爆炸。
func callGuardWithoutDAO(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("guard did not reject before DAO access: %v", r)
		}
	}()
	return fn()
}

// buildGuardFuzzBuilder 按 flag 组合 WHERE/JOIN 形态；DeleteJoin 在没有任何 join flag 时
// 追加一个无条件 JOIN，保证进入“无有效 WHERE/JOIN”保护分支而非 ErrDeleteJoinNoJoin。
func buildGuardFuzzBuilder(g Grammar, op uint8, force bool, whereFlags, joinFlags uint8, operator, value string) *Builder {
	builder := NewBuilder(g, nil).Table("t")
	if whereFlags&1 != 0 {
		builder.Where("id", operator, value)
	}
	if whereFlags&2 != 0 {
		builder.WhereRaw("")
	}
	if whereFlags&4 != 0 {
		builder.WhereIn("id", []any{})
	}
	if whereFlags&8 != 0 {
		builder.WhereNotIn("id", []any{})
	}
	if whereFlags&16 != 0 {
		builder.WhereNested(func(*Builder) {})
	}
	if whereFlags&32 != 0 {
		builder.WhereExists(func(*Builder) {})
	}
	if whereFlags&64 != 0 {
		builder.WhereExists(func(q *Builder) {
			q.Table("x").Select("1").Where("x.id", "=", 1)
		})
	}
	if whereFlags&128 != 0 {
		builder.WhereRaw("1=1")
	}
	if joinFlags&1 != 0 {
		builder.JoinOn("j", func(*JoinBuilder) {})
	}
	if joinFlags&2 != 0 {
		builder.JoinOn("j", func(j *JoinBuilder) {
			j.On("j.id", "=", "t.id")
		})
	}
	if joinFlags&4 != 0 {
		builder.JoinOn("j", func(j *JoinBuilder) {
			j.JoinOn("k", func(*JoinBuilder) {})
		})
	}
	if joinFlags&8 != 0 {
		builder.JoinOn("j", func(j *JoinBuilder) {
			j.JoinOn("k", func(k *JoinBuilder) {
				k.On("k.id", "=", "j.id")
			})
		})
	}
	if op == 2 && joinFlags == 0 {
		builder.JoinOn("j", func(*JoinBuilder) {})
	}
	if force {
		builder.Force()
	}
	return builder
}

// fuzzGuardWhereEffective 独立归约 AND 条件链：任一恒假或未知即构成有效限定；
// 空 Raw/空嵌套为 absent，空 IN 为恒假，空 NOT IN/空 EXISTS 为恒真，非空 Raw 为未知。
func fuzzGuardWhereEffective(flags uint8) bool {
	if flags&1 != 0 || flags&64 != 0 || flags&128 != 0 {
		return true // 普通条件 / 带条件 EXISTS / 非空 Raw
	}
	if flags&4 != 0 {
		return true // 空 IN 恒假
	}
	return false
}

// fuzzGuardJoinEffective 独立判定 JOIN 是否含有效 ON 条件；空 JOIN/空嵌套组不算限定。
func fuzzGuardJoinEffective(flags uint8) bool {
	return flags&2 != 0 || flags&8 != 0
}
