// 本文件为 DBDao 自身的单元测试（db_dao.go）：验证 Grammar() 访问器与慢 SQL 回调的
// grammar 注入。不执行 SQL 断言、不断言注释；仅用内存 SQLite 建立真实的连接池，
// 以便 DAO 经 NewDBDao 正常装配（三种 dialect 共用该池，dialect 只决定编译器）。

package zcdb

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// TestDBDao_Grammar 验证 Grammar() 返回 NewDBDao 按 dialect 推导出的具体编译器：
// 类型正确、能按方言形态包裹标识符、与 DAO 内部实例是同一对象（非克隆副本），
// 且经它编译的 SQL 与 DAO 自身 Builder 路径逐字节一致。
func TestDBDao_Grammar(t *testing.T) {
	pool := openSQLiteTestDB(t).Pool()
	cases := []struct {
		dialect  string
		want     Grammar // 期望的具体编译器类型（仅用于类型比对）
		wantWrap string  // WrapColumn("users") 的方言形态
	}{
		{"mysql", &MySQLGrammar{}, "`users`"},
		{"postgresql", &PostgresGrammar{}, `"users"`},
		{"sqlite", &SQLiteGrammar{}, `"users"`},
	}
	for _, c := range cases {
		t.Run(c.dialect, func(t *testing.T) {
			dao, err := NewDBDao(pool, c.dialect, nil, "", "")
			if err != nil {
				t.Fatalf("NewDBDao(%q) 返回错误: %v", c.dialect, err)
			}
			g := dao.Grammar()
			if g == nil {
				t.Fatalf("Grammar() 返回 nil，期望 %T", c.want)
			}
			if reflect.TypeOf(g) != reflect.TypeOf(c.want) {
				t.Fatalf("Grammar() 类型不符：got %T，want %T", g, c.want)
			}
			if got := g.WrapColumn("users"); got != c.wantWrap {
				t.Errorf("Grammar().WrapColumn(\"users\") = %q，want %q", got, c.wantWrap)
			}
			if g != dao.grammar {
				t.Errorf("Grammar() 未返回 DAO 内部实例：got %p，want %p", g, dao.grammar)
			}

			// 返回值与 DAO 共用编译器：经它编译与经 DAO.Builder() 编译必须完全一致。
			// 这里刻意不用 newTestBuilder（AGENTS.md 的 zcdb 专项约定）：要与 DAO.Builder() 路径
			// 逐字节比较，两条路径的注释状态必须相同（本 DAO 的 comment 为空），
			// 注入统一测试注释会破坏可比性——该偏离已记入提交说明。
			gotSQL, gotArgs, err := NewBuilder(g, dao).Table("users").Where("age", ">", 18).ToSelect()
			if err != nil {
				t.Fatalf("经 Grammar() 编译失败: %v", err)
			}
			wantSQL, wantArgs, err := dao.Builder().Table("users").Where("age", ">", 18).ToSelect()
			if err != nil {
				t.Fatalf("经 DAO.Builder() 编译失败: %v", err)
			}
			assertSQL(t, wantSQL, gotSQL)
			if !reflect.DeepEqual(gotArgs, wantArgs) {
				t.Errorf("绑定参数不一致：Grammar() 路径 %v，Builder() 路径 %v", gotArgs, wantArgs)
			}
		})
	}
}

// TestDBDao_Grammar_PostgresCounterUntouched 锁死返回值可安全复用：PostgreSQL 编译器带
// 编译期参数计数器，经 Grammar() 的实例连续编译两次都必须产出 $1 编号，
// 且 DAO 内部实例的计数器始终保持初始值（编译只在克隆副本上累加，不留残状态）。
func TestDBDao_Grammar_PostgresCounterUntouched(t *testing.T) {
	pool := openSQLiteTestDB(t).Pool()
	dao, err := NewDBDao(pool, "postgresql", nil, "", "")
	if err != nil {
		t.Fatalf("NewDBDao(postgresql) 返回错误: %v", err)
	}

	g := dao.Grammar()
	for i := 1; i <= 2; i++ {
		// 同 TestDBDao_Grammar：为与 DAO.Builder() 路径的产物逐字节比较，不注入统一测试注释。
		sql, args, err := NewBuilder(g, dao).Table("users").Where("age", ">", 18).ToSelect()
		if err != nil {
			t.Fatalf("第 %d 次经 Grammar() 编译失败: %v", i, err)
		}
		assertSQL(t, `SELECT * FROM "users" WHERE "age" > $1`, sql)
		if !reflect.DeepEqual(args, []any{18}) {
			t.Errorf("第 %d 次编译绑定参数 = %v，want [18]", i, args)
		}
	}

	pg, ok := dao.grammar.(*PostgresGrammar)
	if !ok {
		t.Fatalf("DAO 持有的编译器类型不符：got %T，want *PostgresGrammar", dao.grammar)
	}
	if pg.paramCount != 0 {
		t.Errorf("经 Grammar() 编译后 DAO 内部实例 paramCount = %d，want 0（状态不得残留）", pg.paramCount)
	}
}

// TestDBDao_SlowSQLCallbackReceivesGrammar 验证慢 SQL 回调第二个参数是 DAO 持有的编译器实例：
// 与 dao.Grammar() 返回同一对象，回调内可据此判断方言或自行编译。
func TestDBDao_SlowSQLCallbackReceivesGrammar(t *testing.T) {
	pool := openSQLiteTestDB(t).Pool()
	var got Grammar
	dao, err := NewDBDao(pool, "sqlite",
		func(_ context.Context, grammar Grammar, _ time.Duration, _ string, _ []any) { got = grammar },
		"", "")
	if err != nil {
		t.Fatalf("NewDBDao 返回错误: %v", err)
	}
	if _, err := dao.Exec(context.Background(), "SELECT 1"); err != nil {
		t.Fatalf("Exec 失败: %v", err)
	}
	if got == nil {
		t.Fatal("回调未收到 grammar")
	}
	if got != dao.Grammar() {
		t.Errorf("回调收到的 grammar 与 DAO 持有实例不同：got %p，want %p", got, dao.Grammar())
	}
}
