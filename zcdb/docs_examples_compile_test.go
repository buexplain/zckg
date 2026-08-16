package zcdb_test

// 文档-代码偏离审查 D 类（示例代码可编译性）验证：
// 将 7 份功能文档中的全部 Go 代码块按原文提取，补齐 import 与占位类型后
// 拼成本文件，用 go build/vet 验证“照文档写的代码能编译”。
// 各函数不实际执行（无真实数据库），仅做编译期核对。

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/buexplain/zckg/zcconfig"
	"github.com/buexplain/zckg/zcdb"
)

// ==================== README.md ====================

type readmeUser struct {
	Id   int64  `db:"id"`
	Name string `db:"name"`
	Age  int    `db:"age"`
}

func readmeQuickStart() {
	// 1. 创建连接池（主库 + 可选从库，创建时即 Ping 验证）
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: "mysql",
		DSN:        "user:pass@tcp(127.0.0.1:3306)/test?parseTime=true",
		SlaveDSNs:  []string{"user:pass@tcp(127.0.0.1:3307)/test?parseTime=true"},
	})
	if err != nil {
		panic(err)
	}

	// 2. 创建 DAO（dialect 决定 SQL 方言，最后一个参数为列映射标签名，传空串使用默认 db 标签）
	db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	// 3. 查询
	var users []readmeUser
	err = db.Builder().Table("users").
		Where("age", ">", 18).
		OrderBy("id", "ASC").
		Limit(10).
		Find(ctx, &users)
	if err != nil {
		panic(err)
	}
	fmt.Println(users)

	// 4. 写入
	id, err := db.Builder().Table("users").InsertGetId(ctx, readmeUser{Name: "alice", Age: 25})
	if err != nil {
		panic(err)
	}
	fmt.Println(id)
}

// ==================== compile.md ====================

type compileUserUpdate struct {
	Name *string `db:"name"`
	Age  *int    `db:"age"`
}

func compileMdExamples(db *zcdb.DBDao, user readmeUser) {
	ctx := context.Background()
	_ = ctx

	sqlStr, args, _ := db.Builder().Table("users").Where("age", ">", 25).ToSelect()
	_, _, _ = sqlStr, args, db

	sqlStr, args, _ = db.Builder().Table("users").Where("status", "=", "active").ToCount()

	sqlStr, _, _ = db.Builder().Table("orders").GroupBy("user_id").ToCount()

	sqlStr, _, _ = db.Builder().Table("users").Select("city").Distinct().ToCount()

	sqlStr, args, _ = db.Builder().Table("users").Where("id", "=", 1).ToExists()

	sqlStr, args, _ = db.Builder().Table("orders").Where("status", "=", "paid").ToAggregate("MAX", "amount")

	sqlStr, args, _ = db.Builder().Table("users").ToInsert([]readmeUser{
		{Name: "alice", Age: 25},
		{Name: "bob", Age: 30},
	})

	sqlStr, args, _ = db.Builder().Table("users").ToInsertOrIgnore(user)

	sqlStr, args, _ = db.Builder().Table("users").
		ToUpsert(user, []string{"email"}, []string{"name", "age"})

	sqlStr, args, _ = db.Builder().Table("users_archive").
		ToInsertUsing([]string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})

	name, age := "alice_new", 26
	sqlStr, args, _ = db.Builder().Table("users").Where("id", "=", 1).ToUpdate(compileUserUpdate{Name: &name, Age: &age})

	sqlStr, args, _ = db.Builder().Table("users").Where("id", "=", 1).ToDelete()

	sqlTrunc, err := db.Builder().Table("users").ToTruncate()
	_, _ = sqlTrunc, err

	sqlStr, args, _ = db.Builder().Table("users").Where("id", "=", 1).
		ToIncrement([]string{"wallet", "level"}, []any{100, 1})

	// Expression 作为 Where 比较值
	sqlStr, _, _ = db.Builder().Table("users").
		Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect()

	// Expression 作为 Update 字段值
	sqlStr, _, _ = db.Builder().Table("users").Where("id", "=", 1).
		ToUpdate(struct {
			UpdatedAt any `db:"updated_at"`
		}{UpdatedAt: zcdb.NewExpression("NOW()")})

	_ = args
}

// ==================== connection.md ====================

func connectionMdExamples(ctx context.Context) error {
	pool, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName:            "mysql",
		DSN:                   "user:pass@tcp(127.0.0.1:3306)/test?parseTime=true",
		SlaveDSNs:             []string{"user:pass@tcp(127.0.0.1:3307)/test?parseTime=true"},
		MaxOpenConns:          50,
		MaxIdleConns:          50,
		ConnMaxLifetimeSecond: 600,
		ConnectTimeout:        5 * time.Second,
		SlaveStrategy:         &zcdb.RandomStrategy{},
	})
	if err != nil {
		return err
	}

	// DSN 由 zcconfig.DBConfig 组装
	testDB := zcconfig.Config("database.test_db", zcconfig.DBConfig{})
	pool2, err := zcdb.NewPool(zcdb.PoolConfig{
		DriverName: testDB.Driver,
		DSN:        testDB.GetMasterDSN(),
		SlaveDSNs:  testDB.GetSlaveDSN(),
	})
	if err != nil {
		return err
	}
	_ = pool2

	// 运行期热加载从库
	err = pool.AddSlave("user:pass@tcp(127.0.0.1:3308)/test?parseTime=true")
	if err != nil {
		return err
	}

	db, err := zcdb.NewDBDao(pool, "mysql", nil, "")
	if err != nil {
		return err
	}
	defer db.Close()

	dbZc, err := zcdb.NewDBDao(pool, "mysql", nil, "zc")
	if err != nil {
		return err
	}
	type zcUser struct {
		Name string `zc:"user_name"`
		Age  int
	}
	rows, _ := dbZc.Query(ctx, "SELECT user_name, age FROM users")
	defer rows.Close()
	var zcUsers []zcUser
	_ = zcdb.ScanStruct(rows, &zcUsers, "zc") // 第三个参数指定标签名，缺省为 "db"

	// 事务
	err = db.Transaction(ctx, func(ctx context.Context) error {
		if _, err := db.Builder().Table("users").
			Where("id", "=", 1).
			Update(ctx, struct {
				Name string `db:"name"`
			}{Name: "alice"}); err != nil {
			return err
		}
		var user readmeUser
		if err := db.Builder().Table("users").
			Where("id", "=", 1).
			First(ctx, &user); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	// 嵌套事务复用
	err = db.Transaction(ctx, func(ctx context.Context) error {
		return ctx.Err()
	})

	// 原始 SQL
	cutoff := time.Now()
	_, err = db.Exec(ctx, "DELETE FROM logs WHERE created_at < ?", cutoff)
	if err != nil {
		return err
	}
	rows2, err := db.Query(ctx, "SELECT id, name FROM users WHERE age > ?", 18)
	if err != nil {
		return err
	}
	defer rows2.Close()
	var users []readmeUser
	if err := zcdb.ScanStruct(rows2, &users); err != nil {
		return err
	}
	rows3, err := db.QueryPrimary(ctx, "SELECT * FROM users WHERE id = ? FOR UPDATE", 1)
	if err != nil {
		return err
	}
	defer rows3.Close()

	// Primary() 强制主库
	type Order struct {
		Id int64 `db:"id"`
	}
	var order Order
	id := int64(1)
	err = db.Builder().Table("orders").Where("id", "=", id).Primary().First(ctx, &order)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	// 慢 SQL 回调
	dbSlow, err := zcdb.NewDBDao(pool, "mysql",
		func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
			if elapsed > 200*time.Millisecond {
				log.Printf("slow sql (%s): %s %v", elapsed, sqlStr, args)
			}
		}, "")
	if err != nil {
		return err
	}
	_ = dbSlow

	// 连接探活
	return pool.Ping(ctx)
}

// ==================== mutate.md ====================

func mutateMdExamples(ctx context.Context, db *zcdb.DBDao) {
	type User struct {
		Name  *string `db:"name"`
		Age   *int    `db:"age"`
		Email any     `db:"email"`
	}

	name, age := "alice", 25
	affected, err := db.Builder().Table("users").Insert(ctx, User{Name: &name, Age: &age})
	_, _ = affected, err

	affected, err = db.Builder().Table("users").Insert(ctx, []struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}{{Name: "alice", Age: 25}, {Name: "bob", Age: 30}})

	// Expression 插入
	affected, err = db.Builder().Table("users").Insert(ctx, struct {
		Name      string `db:"name"`
		CreatedAt any    `db:"created_at"`
	}{"alice", zcdb.NewExpression("NOW()")})

	id, err := db.Builder().Table("users").InsertGetId(ctx, struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}{"alice", 25})
	_, _ = id, err

	user := struct {
		Name  string `db:"name"`
		Age   int    `db:"age"`
		Email string `db:"email"`
	}{"alice", 25, "a@t.com"}
	affected, err = db.Builder().Table("users").InsertOrIgnore(ctx, &user)

	affected, err = db.Builder().Table("users").
		Upsert(ctx, &user, []string{"email"}, []string{"name", "age"})

	affected, err = db.Builder().Table("users_archive").
		InsertUsing(ctx, []string{"name", "age"}, func(sub *zcdb.Builder) {
			sub.Table("users").Select("name", "age").Where("status", "=", "active")
		})

	affected, err = db.Builder().Table("users").
		Where("id", "=", 1).
		Update(ctx, compileUserUpdate{Name: &name, Age: &age})

	// Expression 基于当前值更新
	affected, err = db.Builder().Table("users").Where("id", "=", 1).
		Update(ctx, struct {
			Age any `db:"age"`
		}{Age: zcdb.NewExpression("`age` + 1")})

	affected, err = db.Builder().Table("users").Where("id", "=", 1).Increment(ctx, "wallet", 100)
	affected, err = db.Builder().Table("users").Where("id", "=", 1).Decrement(ctx, "wallet", 50)
	affected, err = db.Builder().Table("users").Where("id", "=", 1).
		Increment(ctx, "wallet", 100, "level", 1)

	affected, err = db.Builder().Table("users").Where("id", "=", 1).Delete(ctx)

	affected, err = db.Builder().Table("users").
		Join("orders", "orders.user_id", "=", "users.id").
		Where("orders.status", "=", "cancelled").
		DeleteJoin(ctx)

	err = db.Builder().Table("users").Truncate(ctx)

	// 全表更新/删除（显式确认）
	type UserUpdate struct {
		Status string `db:"status"`
	}
	affected, err = db.Builder().Table("users").Force().Update(ctx, UserUpdate{Status: "archived"})
	affected, err = db.Builder().Table("users").Force().Delete(ctx)

	// 错误处理示例
	if _, err := db.Builder().Table("users").Delete(ctx); errors.Is(err, zcdb.ErrDeleteWithoutWhere) {
		// 被保护机制拒绝，补充 WHERE 条件或显式 Force()
		_ = affected
	}
}

// ==================== query-builder.md ====================

func queryBuilderMdExamples(db *zcdb.DBDao) {
	sqlStr, args, _ := db.Builder().Table("users").Where("age", ">", 18).ToSelect()
	_, _, _ = sqlStr, args, db

	sub := db.Builder().Table("orders").Select("user_id").Where("amount", ">", 100)
	sqlStr, args, _ = db.Builder().TableSub(sub, "o").Where("o.user_id", ">", 1).ToSelect()

	sqlStr, _, _ = db.Builder().Table("users").Select("id", "name").ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").Select("id").AddSelect("name", "id").ToSelect()
	sqlStr, _, _ = db.Builder().Table("orders").Select("user_id").SelectRaw("SUM(amount) AS total").ToSelect()

	cnt := db.Builder().Table("orders").SelectRaw("COUNT(*)").
		WhereColumn("orders.user_id", "=", "users.id")
	sqlStr, _, _ = db.Builder().Table("users").Select("id").SelectSub(cnt, "order_count").ToSelect()

	sqlStr, _, _ = db.Builder().Table("users").Select("city").Distinct().ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").Where("age", ">", 25).OrWhere("vip", "=", 1).ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").Where("deleted_at", "=", nil).ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").
		Where("id", "=", zcdb.NewExpression("parent_id")).ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").WhereIn("id", []any{1, 2, 3}).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereNotIn("status", []any{"banned", "frozen"}).ToSelect()

	sqlStr, _, _ = db.Builder().Table("users").WhereNull("deleted_at", "remark").ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").WhereNotNull("email").ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").WhereBetween("age", 18, 30).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereNotBetween("age", 18, 30).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").
		Where("vip", "=", 1).OrWhereBetween("age", 18, 30).ToSelect()
	sqlStr, _, _ = db.Builder().Table("products").
		WhereBetweenColumns("price", "min_price", "max_price").ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereValueBetween(25, "min_age", "max_age").ToSelect()
	// Or 变体存在性
	_, _, _ = db.Builder().Table("users").OrWhereNotBetween("age", 1, 2).ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereBetweenColumns("p", "a", "b").ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereNotBetweenColumns("p", "a", "b").ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereValueBetween(1, "a", "b").ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").
		WhereRaw("created_at > NOW() - INTERVAL ? DAY", 7).ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereRaw("vip = ?", 1).ToSelect()

	sqlStr, _, _ = db.Builder().Table("users").WhereColumn("updated_at", ">", "created_at").ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").Where("status", "active").
		WhereNested(func(q *zcdb.Builder) {
			q.Where("age", ">", 18).Where("vip", "=", 1)
		}).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").Where("status", "active").
		OrWhereNested(func(q *zcdb.Builder) {
			q.Where("age", ">", 60).Where("vip", "=", 1)
		}).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereNot(func(q *zcdb.Builder) {
		q.Where("status", "banned").Where("age", "<", 18)
	}).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereAny(func(q *zcdb.Builder) {
		q.Where("age", ">", 60).Where("vip", "=", 1)
	}).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereNone(func(q *zcdb.Builder) {
		q.Where("status", "banned").Where("age", "<", 18)
	}).ToSelect()
	// OR 变体存在性
	_, _, _ = db.Builder().Table("users").OrWhereNot(func(q *zcdb.Builder) {}).ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereAll(func(q *zcdb.Builder) {}).ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereAny(func(q *zcdb.Builder) {}).ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereNone(func(q *zcdb.Builder) {}).ToSelect()
	_ = db.Builder().Table("users").WhereAll(func(q *zcdb.Builder) {})

	sqlStr, args, _ = db.Builder().Table("users").WhereDate("created_at", "2026-08-08").ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").WhereLike("name", "%alice%").ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereLike("name", "a%", true).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereNotLike("name", "%test%").ToSelect()
	_, _, _ = db.Builder().Table("users").OrWhereLike("name", "%x%").ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").WhereNullSafeEquals("remark", nil).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereNullSafeNotEquals("remark", "x").ToSelect()

	sqlStr, _, _ = db.Builder().Table("users").WhereExists(func(q *zcdb.Builder) {
		q.Table("orders").SelectRaw("1").WhereColumn("orders.user_id", "=", "users.id")
	}).ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").WhereSub("age", ">", func(q *zcdb.Builder) {
		q.Table("stats").SelectRaw("AVG(age)")
	}).ToSelect()
	sqlStr, args, _ = db.Builder().Table("users").WhereInSub("dept_id", func(q *zcdb.Builder) {
		q.Table("depts").Select("id").Where("level", ">", 3)
	}).ToSelect()
	// 变体存在性
	_ = db.Builder().Table("users").WhereNotExists(func(q *zcdb.Builder) {})
	_ = db.Builder().Table("users").OrWhereExists(func(q *zcdb.Builder) {})
	_ = db.Builder().Table("users").OrWhereNotExists(func(q *zcdb.Builder) {})
	_ = db.Builder().Table("users").OrWhereSub("age", ">", func(q *zcdb.Builder) {})
	_ = db.Builder().Table("users").WhereNotInSub("dept_id", func(q *zcdb.Builder) {})

	sqlStr, _, _ = db.Builder().Table("users").Select("users.name", "orders.amount").
		Join("orders", "users.id", "=", "orders.user_id").ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").
		LeftJoin("orders", "users.id", "=", "orders.user_id").ToSelect()
	sqlStr, _, _ = db.Builder().Table("stores").CrossJoin("months").ToSelect()
	_ = db.Builder().Table("users").RightJoin("orders", "users.id", "=", "orders.user_id")
	_ = db.Builder().Table("users").RightJoinOn("orders", func(j *zcdb.JoinBuilder) {})
	_ = db.Builder().Table("users").RightJoinSub(db.Builder().Table("t"), "t", func(j *zcdb.JoinBuilder) {})

	sqlStr, _, _ = db.Builder().Table("users").
		CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()

	sqlStr, args, _ = db.Builder().Table("users").Select("users.name").
		JoinOn("orders", func(j *zcdb.JoinBuilder) {
			j.On("orders.user_id", "=", "users.id").
				Where("orders.status", "=", "paid")
		}).ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").LeftJoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").OrOn("orders.ref_user_id", "=", "users.id")
	}).ToSelect()

	// JoinBuilder 全部条件方法（文档方法表 8 行）
	_, _, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("a.id", "=", "b.id").
			OrOn("a.id2", "=", "b.id2").
			Where("a.s", "=", 1).
			OrWhere("a.s2", "=", 2).
			WhereNull("a.n1", "a.n2").
			WhereNotNull("a.n3").
			WhereIn("a.i", []any{1, 2}).
			WhereNotIn("a.i2", []any{3}).
			WhereExists(func(q *zcdb.JoinBuilder) { q.On("x", "=", "y") }).
			WhereNested(func(q *zcdb.JoinBuilder) { q.Where("a.x", ">", 0) }).
			OrWhereNested(func(q *zcdb.JoinBuilder) { q.Where("a.y", ">", 0) }).
			OnNested(func(q *zcdb.JoinBuilder) { q.On("a.z", "=", "b.z") }).
			Raw("YEAR(a.t) = ?", 2026)
	}).ToSelect()

	// 嵌套 join 组
	sqlStr, _, _ = db.Builder().Table("users").JoinOn("orders", func(j *zcdb.JoinBuilder) {
		j.On("orders.user_id", "=", "users.id").JoinOn("order_items", func(q *zcdb.JoinBuilder) {
			q.On("order_items.order_id", "=", "orders.id")
		})
	}).ToSelect()

	// JoinBuilder 内 CrossJoinOn 存在性（单条件简写形态）
	_, _, _ = db.Builder().Table("a").JoinOn("b", func(j *zcdb.JoinBuilder) {
		j.On("b.id", "=", "a.id").CrossJoinOn("c", "c.id", "=", "b.id")
	}).ToSelect()

	// 派生表 JOIN
	latest := db.Builder().Table("logs").Select("user_id").
		SelectRaw("MAX(created_at) AS last_at").GroupBy("user_id")
	sqlStr, _, _ = db.Builder().Table("users").Select("users.name", "l.last_at").
		JoinSub(latest, "l", func(j *zcdb.JoinBuilder) {
			j.On("l.user_id", "=", "users.id")
		}).ToSelect()
	months := db.Builder().Table("months").Select("month")
	sqlStr, _, _ = db.Builder().Table("stores").CrossJoinSub(months, "m").ToSelect()
	_ = db.Builder().Table("users").LeftJoinSub(latest, "l", func(j *zcdb.JoinBuilder) {})

	sqlStr, _, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").GroupBy("user_id").ToSelect()
	sqlStr, _, _ = db.Builder().Table("orders").GroupBy("user_id", "status").ToSelect()
	sqlStr, _, _ = db.Builder().Table("orders").
		SelectRaw("DATE(created_at) AS d, COUNT(*) AS cnt").
		GroupByRaw("DATE(created_at)").ToSelect()

	sqlStr, args, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").
		GroupBy("user_id").Having("total", ">", 100).ToSelect()
	sqlStr, args, _ = db.Builder().Table("orders").Select("user_id").
		SelectRaw("SUM(amount) AS total").
		GroupBy("user_id").HavingRaw("SUM(amount) > ?", 1000).ToSelect()
	// Having 变体存在性
	_ = db.Builder().Table("o").GroupBy("u").OrHaving("t", ">", 1)
	_ = db.Builder().Table("o").GroupBy("u").HavingBetween("t", 1, 2)
	_ = db.Builder().Table("o").GroupBy("u").HavingNotBetween("t", 1, 2)
	_ = db.Builder().Table("o").GroupBy("u").HavingNull("t1", "t2")
	_ = db.Builder().Table("o").GroupBy("u").HavingNotNull("t1")
	_ = db.Builder().Table("o").GroupBy("u").HavingNested(func(q *zcdb.Builder) {})
	_ = db.Builder().Table("o").GroupBy("u").OrHavingNested(func(q *zcdb.Builder) {})

	sqlStr, _, _ = db.Builder().Table("users").OrderBy("age", "DESC").OrderBy("name").ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").OrderByRaw("FIELD(status, 'active', 'frozen')").ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").InRandomOrder().ToSelect()

	sqlStr, _, _ = db.Builder().Table("users").Limit(10).Offset(20).ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").ForPage(2, 20).ToSelect()

	admins := db.Builder().Table("admins").Select("name")
	sqlStr, _, _ = db.Builder().Table("users").Select("name").Union(admins).ToSelect()
	sqlStr, _, _ = db.Builder().Table("users").Select("name").UnionAll(admins).ToSelect()

	ctx := context.Background()
	err := db.Transaction(ctx, func(ctx context.Context) error {
		var user readmeUser
		if err := db.Builder().Table("users").
			Where("id", "=", 1).LockForUpdate().First(ctx, &user); err != nil {
			return err
		}
		return nil
	})
	_ = err

	sqlStr, args, _ = db.Builder().Table("users").Where("id", "=", 1).SharedLock().ToSelect()

	// Clone
	base := db.Builder().Table("users").Where("status", "active")
	admins2 := base.Clone().Where("role", "admin")
	sqlBase, _, _ := base.ToSelect()
	sqlAdmins, _, _ := admins2.ToSelect()
	_, _ = sqlBase, sqlAdmins
}

// ==================== query-exec.md ====================

func queryExecMdExamples(ctx context.Context, db *zcdb.DBDao) error {
	type User struct {
		Id   int64  `db:"id"`
		Name string `db:"name"`
		Age  int    `db:"age"`
	}

	var users []User
	err := db.Builder().Table("users").Where("status", "=", "active").Find(ctx, &users)
	if err != nil {
		return err
	}

	var user User
	err = db.Builder().Table("users").Where("id", "=", 1).First(ctx, &user)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var name string
	err = db.Builder().Table("users").Select("name").Where("id", "=", 1).Value(ctx, &name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	var remark *string
	err = db.Builder().Table("users").Select("remark").Where("id", "=", 1).Value(ctx, &remark)
	// err == nil 且 remark == nil → 该列值为 NULL
	_ = err

	count, err := db.Builder().Table("users").Where("status", "=", "active").Count(ctx)
	_, _ = count, err
	exists, err := db.Builder().Table("users").Where("email", "=", "a@t.com").Exists(ctx)
	_, _ = exists, err

	maxAge, err := db.Builder().Table("users").Max(ctx, "age")
	_, _ = maxAge, err
	_, _ = db.Builder().Table("users").Min(ctx, "age")
	total, err := db.Builder().Table("orders").Where("status", "=", "paid").Sum(ctx, "amount")
	_, _ = total, err
	_, _ = db.Builder().Table("users").Avg(ctx, "age")

	var names []string
	err = db.Builder().Table("users").Where("vip", "=", 1).Pluck(ctx, &names, "name")
	if err != nil {
		return err
	}
	var m map[int64]string
	err = db.Builder().Table("users").Pluck(ctx, &m, "name", "id")
	if err != nil {
		return err
	}
	var mu map[int64]User
	err = db.Builder().Table("users").Pluck(ctx, &mu, "id")
	if err != nil {
		return err
	}

	var pageUsers []User
	totalCount, err := db.Builder().Table("users").
		Where("status", "=", "active").
		OrderBy("id", "ASC").
		ForPage(2, 20).
		Paginate(ctx, &pageUsers)
	_, _ = totalCount, err

	for err := range db.Builder().Table("users").OrderBy("id", "ASC").Cursor(ctx, &user) {
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(user.Name)
	}

	process := func(u User) { _ = u }
	for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id") {
		if err != nil {
			log.Fatal(err)
		}
		process(user)
	}
	for err := range db.Builder().Table("users").CursorBy(ctx, &user, 100, "id", true) {
		if err != nil {
			log.Fatal(err)
		}
	}

	rows, err := db.Query(ctx, "SELECT id, name, age FROM users WHERE age > ?", 18)
	if err != nil {
		return err
	}
	defer rows.Close()
	var scanned []User
	if err := zcdb.ScanStruct(rows, &scanned); err != nil {
		return err
	}
	var one User
	if err := zcdb.ScanStruct(rows, &one); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	rows2, err := db.Query(ctx, "SELECT id, name FROM users WHERE id = ?", 1)
	if err != nil {
		return err
	}
	var one2 User
	err = zcdb.ScanStructClose(rows2, &one2)
	return err
}

// ==================== schema.md ====================

func schemaMdExamples(ctx context.Context, db *zcdb.DBDao) error {
	inspector, err := db.Schema()
	if err != nil {
		return err
	}

	tables, err := inspector.Tables(ctx)
	if err != nil {
		return err
	}
	for _, t := range tables {
		fmt.Println(t.Name, t.Comment)
	}

	columns, err := inspector.Columns(ctx, "users")
	if err != nil {
		return err
	}
	for _, c := range columns {
		fmt.Printf("%s %s nullable=%v default=%v comment=%s\n",
			c.Name, c.Type, c.Nullable, c.Default, c.Comment)
	}

	// 完整示例
	for _, t := range tables {
		fmt.Printf("表 %s（%s）:\n", t.Name, t.Comment)
		columns, err := inspector.Columns(ctx, t.Name)
		if err != nil {
			return err
		}
		for _, c := range columns {
			null := "NOT NULL"
			if c.Nullable {
				null = "NULL"
			}
			fmt.Printf("  %s %s %s\n", c.Name, c.Type, null)
		}
	}
	return nil
}

// NewSchemaInspector 也可直接创建（schema.md 第一节）。
func schemaMdNewInspector(db *zcdb.DBDao) {
	inspector, err := db.Schema()
	_, _ = inspector, err
}
