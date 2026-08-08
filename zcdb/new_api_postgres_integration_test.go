package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// ==================== new-api-implement-list.md 新增 API 集成测试（PostgreSQL 方言） ====================
//
// 重点覆盖 PG 方言分支：WhereDate ::date、NullSafe IS [NOT] DISTINCT FROM、
// WhereLike 默认 ILIKE/区分大小写 LIKE、CrossJoinOn 编译为 INNER JOIN、
// DeleteJoin USING、InsertOrIgnoreUsing ON CONFLICT DO NOTHING、$N 占位符绑定顺序、
// §51 加锁 select 走写连接。通用语义已在 SQLite 集成测试覆盖，此处做真实库复核。

// setupPgNewApiTables 清理 PG 新增 API 测试专用表。
func setupPgNewApiTables(t *testing.T, db *DBDao) {
	t.Helper()
	for _, table := range []string{"events", "wallets", "archive", "colors", "names_cs", "empty_t"} {
		mustExec(t, db, "DROP TABLE IF EXISTS \""+table+"\"")
	}
}

// setupPgEventsTable 创建 events 表（WhereDate 测试用）。
func setupPgEventsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE events (
		id          BIGSERIAL PRIMARY KEY,
		happened_at TIMESTAMP NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO events (happened_at) VALUES
		('2024-06-15 10:00:00'),
		('2024-06-16 08:30:00'),
		('2024-06-15 23:59:59')`)
}

// setupPgWalletsTable 创建 wallets 表（Increment/Decrement 测试用）。
func setupPgWalletsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE wallets (
		id      BIGINT PRIMARY KEY,
		balance BIGINT NOT NULL,
		points  BIGINT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO wallets (id, balance, points) VALUES (1, 100, 10), (2, 200, 20)`)
}

// setupPgArchiveTable 创建 archive 表（InsertOrIgnoreUsing 测试用）。
func setupPgArchiveTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE archive (
		id    BIGSERIAL PRIMARY KEY,
		name  VARCHAR(64) NOT NULL,
		email VARCHAR(128) UNIQUE
	)`)
	mustExec(t, db, `INSERT INTO archive (name, email) VALUES
		('alice', 'alice@test.com'),
		('zoe', 'zoe@test.com')`)
}

// setupPgColorsTable 创建 colors 表（CrossJoinOn 测试用）。
func setupPgColorsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE colors (
		id   BIGINT PRIMARY KEY,
		name VARCHAR(16) NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO colors (id, name) VALUES (1, 'red'), (2, 'blue')`)
}

// setupPgNamesCsTable 创建大小写混合的名字表（Like caseSensitive 测试用）。
func setupPgNamesCsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE names_cs (
		id   BIGSERIAL PRIMARY KEY,
		name VARCHAR(64) NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO names_cs (name) VALUES ('alice'), ('Alice'), ('BOB')`)
}

// ==================== §3 WhereDate（PG 分支：col::date） ====================

// TestPgInteg_NewApi_WhereDate 验证 WhereDate 在 PG 上编译为 "col"::date = $1 并真实执行。
func TestPgInteg_NewApi_WhereDate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgNewApiTables(t, db)
	setupPgEventsTable(t, db)

	sqlStr, args, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "events" WHERE "happened_at"::date = $1`, sqlStr)
	assertArgs(t, []any{"2024-06-15"}, args)

	count, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate: expected 2, got %d", count)
	}
}

// ==================== §10 NullSafe（PG 分支：IS [NOT] DISTINCT FROM） ====================

// TestPgInteg_NewApi_NullSafe 验证空安全比较在 PG 上编译为 IS [NOT] DISTINCT FROM 并真实执行。
func TestPgInteg_NewApi_NullSafe(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	sqlStr, _, err := db.Builder().Table("users").WhereNullSafeEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" IS NOT DISTINCT FROM $1`, sqlStr)
	sqlStr, _, err = db.Builder().Table("users").WhereNullSafeNotEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "age" IS DISTINCT FROM $1`, sqlStr)

	count, err := db.Builder().Table("users").WhereNullSafeEquals("age", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 1 { // eve
		t.Errorf("NullSafe =nil: expected 1, got %d", count)
	}
	count, err = db.Builder().Table("users").WhereNullSafeNotEquals("age", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("NullSafeNot =nil: expected 4, got %d", count)
	}
}

// ==================== §11 WhereLike caseSensitive（PG 分支：默认 ILIKE / 区分 LIKE） ====================

// TestPgInteg_NewApi_WhereLikeCaseSensitive 验证 PG 默认 ILIKE（不区分）、第三参 true 编译为 LIKE（区分）。
func TestPgInteg_NewApi_WhereLikeCaseSensitive(t *testing.T) {
	db := openPgTestDB(t)
	setupPgNewApiTables(t, db)
	setupPgNamesCsTable(t, db)

	// 默认编译为 ILIKE
	sqlStr, _, err := db.Builder().Table("names_cs").WhereLike("name", "a%").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "names_cs" WHERE "name" ILIKE $1`, sqlStr)

	// 默认不区分大小写：alice + Alice
	count, err := db.Builder().Table("names_cs").WhereLike("name", "a%").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("ilike: expected 2, got %d", count)
	}

	// 区分大小写编译为 LIKE
	sqlStr, _, err = db.Builder().Table("names_cs").WhereLike("name", "a%", true).ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "names_cs" WHERE "name" LIKE $1`, sqlStr)

	count, err = db.Builder().Table("names_cs").WhereLike("name", "a%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 { // 仅 'alice'
		t.Errorf("like case-sensitive a%%: expected 1, got %d", count)
	}
	count, err = db.Builder().Table("names_cs").WhereLike("name", "A%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 { // 仅 'Alice'
		t.Errorf("like case-sensitive A%%: expected 1, got %d", count)
	}
}

// ==================== §44 CrossJoinOn（PG 分支：编译为 INNER JOIN） ====================

// TestPgInteg_NewApi_CrossJoinOn 验证 PG 的 CROSS JOIN 不接受 ON，编译层转为 INNER JOIN（语义等价）。
func TestPgInteg_NewApi_CrossJoinOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgNewApiTables(t, db)
	setupPgColorsTable(t, db)

	sqlStr, _, err := db.Builder().Table("users").CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" INNER JOIN "colors" ON "colors"."id" = "users"."id"`, sqlStr)

	count, err := db.Builder().Table("users").CrossJoinOn("colors", "colors.id", "=", "users.id").Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // colors 仅 id 1、2 与 users 匹配
		t.Errorf("CrossJoinOn: expected 2, got %d", count)
	}
}

// ==================== §43 DeleteJoin（PG 分支：USING） ====================

// TestPgInteg_NewApi_DeleteJoin 验证 PG 的 DELETE ... USING 编译形态与真实执行。
func TestPgInteg_NewApi_DeleteJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		ToDeleteJoin()
	assertNoError(t, err)
	assertSQL(t,
		`DELETE FROM "users" USING "orders" WHERE "users"."id" = "orders"."user_id" AND "orders"."amount" > $1`,
		sqlStr)
	assertArgs(t, []any{100}, args)

	affected, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		DeleteJoin(context.Background())
	assertNoError(t, err)
	if affected != 3 {
		t.Errorf("DeleteJoin affected: expected 3, got %d", affected)
	}
	count, err := db.Builder().Table("users").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("users after DeleteJoin: expected 2, got %d", count)
	}

	// 保护机制
	_, err = db.Builder().Table("users").DeleteJoin(context.Background())
	if !errors.Is(err, ErrDeleteWithoutWhere) {
		t.Errorf("expected ErrDeleteWithoutWhere, got %v", err)
	}
}

// ==================== §41 InsertOrIgnoreUsing（PG 分支：ON CONFLICT DO NOTHING） ====================

// TestPgInteg_NewApi_InsertOrIgnoreUsing 验证 INSERT INTO ... SELECT ... ON CONFLICT DO NOTHING。
func TestPgInteg_NewApi_InsertOrIgnoreUsing(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgNewApiTables(t, db)
	setupPgArchiveTable(t, db)

	sqlStr, _, err := db.Builder().Table("archive").ToInsertOrIgnoreUsing([]string{"name", "email"}, func(q *Builder) {
		q.Table("users").Select("name", "email").Where("status", "=", "active")
	})
	assertNoError(t, err)
	assertSQL(t,
		`INSERT INTO "archive" ("name", "email") SELECT "name", "email" FROM "users" WHERE "status" = $1 ON CONFLICT DO NOTHING`,
		sqlStr)

	affected, err := db.Builder().Table("archive").InsertOrIgnoreUsing(
		context.Background(),
		[]string{"name", "email"},
		func(q *Builder) {
			q.Table("users").Select("name", "email").Where("status", "=", "active")
		},
	)
	assertNoError(t, err)
	if affected != 2 { // alice 冲突忽略，bob/diana 插入
		t.Errorf("affected: expected 2, got %d", affected)
	}
	count, err := db.Builder().Table("archive").Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("archive count: expected 4, got %d", count)
	}
}

// ==================== §32 Increment / Decrement（PG $N 绑定） ====================

// TestPgInteg_NewApi_IncrementDecrement 验证 PG 上 SET 在 JOIN 之前的绑定顺序（$N）与真实执行。
func TestPgInteg_NewApi_IncrementDecrement(t *testing.T) {
	db := openPgTestDB(t)
	setupPgNewApiTables(t, db)
	setupPgWalletsTable(t, db)

	ctx := context.Background()

	// 编译形态：SET col = col + $1（PG SET 先于 WHERE）
	sqlStr, args, err := db.Builder().Table("wallets").Where("id", "=", 1).ToIncrement([]string{"balance", "points"}, []any{10, 5})
	assertNoError(t, err)
	assertSQL(t, `UPDATE "wallets" SET "balance" = "balance" + $1, "points" = "points" + $2 WHERE "id" = $3`, sqlStr)
	assertArgs(t, []any{10, 5, 1}, args)

	// 多列自增真实执行
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 50, "points", 5)
	assertNoError(t, err)
	type wallet struct {
		Balance int64 `db:"balance"`
		Points  int64 `db:"points"`
	}
	var w wallet
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance", "points").First(ctx, &w)
	assertNoError(t, err)
	if w.Balance != 150 || w.Points != 15 {
		t.Errorf("multi-column increment: expected {150 15}, got %+v", w)
	}

	// 自减
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Decrement(ctx, "balance", 30)
	assertNoError(t, err)
	var balance int64
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance").Value(ctx, &balance)
	assertNoError(t, err)
	if balance != 120 {
		t.Errorf("balance after decrement: expected 120, got %d", balance)
	}

	// 保护机制
	_, err = db.Builder().Table("wallets").Increment(ctx, "balance", 1)
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("expected ErrUpdateWithoutWhere, got %v", err)
	}
}

// ==================== §24 Aggregate（真实库复核） ====================

// TestPgInteg_NewApi_Aggregate 验证 Max/Min/Sum/Avg 在 PG 上的真实执行与空表语义。
func TestPgInteg_NewApi_Aggregate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)
	setupPgNewApiTables(t, db)

	ctx := context.Background()

	maxAge, err := db.Builder().Table("users").Max(ctx, "age")
	assertNoError(t, err)
	if maxAge != 35 {
		t.Errorf("Max: expected 35, got %v", maxAge)
	}
	sumAmount, err := db.Builder().Table("orders").Sum(ctx, "amount")
	assertNoError(t, err)
	if sumAmount != 630 {
		t.Errorf("Sum: expected 630, got %v", sumAmount)
	}
	maxActive, err := db.Builder().Table("users").Where("status", "=", "active").Max(ctx, "age")
	assertNoError(t, err)
	if maxActive != 30 {
		t.Errorf("Max with where: expected 30, got %v", maxActive)
	}

	// 空表语义
	mustExec(t, db, `CREATE TABLE empty_t (id BIGINT PRIMARY KEY, amount BIGINT)`)
	sumEmpty, err := db.Builder().Table("empty_t").Sum(ctx, "amount")
	assertNoError(t, err)
	if sumEmpty != 0 {
		t.Errorf("Sum empty: expected 0, got %v", sumEmpty)
	}
	_, err = db.Builder().Table("empty_t").Max(ctx, "amount")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Max empty: expected sql.ErrNoRows, got %v", err)
	}
}

// ==================== §14 WhereExists(*Builder)（$N 绑定） ====================

// TestPgInteg_NewApi_WhereExistsBuilder 验证 WhereExists 直传 *Builder 在 PG 上的 $N 编译与真实执行。
func TestPgInteg_NewApi_WhereExistsBuilder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	sub := db.Builder().Table("orders").
		WhereRaw("orders.user_id = users.id").
		Where("amount", ">", 100)

	sqlStr, args, err := db.Builder().Table("users").WhereExists(sub).ToSelect()
	assertNoError(t, err)
	assertSQL(t,
		`SELECT * FROM "users" WHERE EXISTS (SELECT * FROM "orders" WHERE orders.user_id = users.id AND "amount" > $1)`,
		sqlStr)
	assertArgs(t, []any{100}, args)

	count, err := db.Builder().Table("users").WhereExists(sub).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Errorf("WhereExists(*Builder): expected 3, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("id", "=", 5).
		OrWhereNotExists(sub).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // eve(id=5) + charlie
		t.Errorf("OrWhereNotExists: expected 2, got %d", count)
	}
}

// ==================== §17 GroupByRaw（$N 绑定） ====================

// TestPgInteg_NewApi_GroupByRaw 验证 GroupByRaw 在 PG 上的 $N 占位符转换与真实执行。
func TestPgInteg_NewApi_GroupByRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	sqlStr, args, err := db.Builder().Table("orders").
		SelectRaw("SUM(amount) AS total").
		GroupByRaw("user_id + ?", 0).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT SUM(amount) AS total FROM "orders" GROUP BY user_id + $1`, sqlStr)
	assertArgs(t, []any{0}, args)

	type row struct {
		Total float64 `db:"total"`
	}
	var rows []row
	err = db.Builder().Table("orders").
		SelectRaw("SUM(amount) AS total").
		GroupByRaw("user_id + ?", 0).
		HavingNested(func(q *Builder) {
			// PG 的 HAVING 不能引用 SELECT 别名，用原始表达式
			q.HavingRaw("SUM(amount) > ?", 100).HavingRaw("SUM(amount) < ?", 200)
		}).
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 2 { // u1=170、u4=150
		t.Errorf("GroupByRaw+HavingNested: expected 2 groups, got %+v", rows)
	}
}

// ==================== §45/§46 JoinBuilder 条件 + 嵌套 join（$N 绑定） ====================

// TestPgInteg_NewApi_JoinBuilder 验证 JoinBuilder 条件与嵌套 join 在 PG 上的真实执行。
func TestPgInteg_NewApi_JoinBuilder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)
	setupPgProfilesTable(t, db)

	// JoinBuilder.Where 条件（$N 顺序：JOIN 条件先于 WHERE）
	count, err := db.Builder().Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("users.id", "=", "orders.user_id").Where("orders.amount", ">", 100)
	}).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Errorf("JoinOn Where: expected 3, got %d", count)
	}

	// WhereIn 带绑定
	count, err = db.Builder().Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("users.id", "=", "orders.user_id").
			WhereIn("orders.product", []any{"Book", "Pen"})
	}).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("JoinOn WhereIn: expected 2, got %d", count)
	}

	// 嵌套 join 组括号形态编译断言
	sqlStr, _, err := db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				JoinOn("orders", func(j2 *JoinBuilder) {
					j2.On("profiles.user_id", "=", "orders.user_id").
						Where("orders.amount", ">", 100)
				})
		}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t,
		`SELECT * FROM "users" INNER JOIN ("profiles" INNER JOIN "orders" ON "profiles"."user_id" = "orders"."user_id" AND "orders"."amount" > $1) ON "users"."id" = "profiles"."user_id"`,
		sqlStr)

	// 嵌套 join 真实执行
	count, err = db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				JoinOn("orders", func(j2 *JoinBuilder) {
					j2.On("profiles.user_id", "=", "orders.user_id").
						Where("orders.amount", ">", 100)
				})
		}).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // alice(120)、bob(200)
		t.Errorf("NestedJoin: expected 2, got %d", count)
	}
}

// ==================== §51 加锁 select 走写连接（PG） ====================

// TestPgInteg_NewApi_LockSelect 验证带锁查询在 PG 事务中的真实执行（FOR UPDATE / FOR SHARE）。
func TestPgInteg_NewApi_LockSelect(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("users").Where("id", "=", 1).LockForUpdate().ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" = $1 FOR UPDATE`, sqlStr)
	sqlStr, _, err = db.Builder().Table("users").Where("id", "=", 1).SharedLock().ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" = $1 FOR SHARE`, sqlStr)

	// 事务中带锁查询真实执行
	err = db.Transaction(context.Background(), func(ctx context.Context) error {
		type row struct {
			Name string `db:"name"`
		}
		var u row
		err := db.Builder().Table("users").Select("name").Where("id", "=", 1).
			Limit(1).LockForUpdate().First(ctx, &u)
		if err != nil {
			return err
		}
		if u.Name != "alice" {
			t.Errorf("LockForUpdate: expected alice, got %s", u.Name)
		}

		var u2 row
		err = db.Builder().Table("users").Select("name").Where("id", "=", 2).
			Limit(1).SharedLock().First(ctx, &u2)
		if err != nil {
			return err
		}
		if u2.Name != "bob" {
			t.Errorf("SharedLock: expected bob, got %s", u2.Name)
		}
		return nil
	})
	assertNoError(t, err)
}
