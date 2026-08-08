package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// ==================== new-api-implement-list.md 新增 API 集成测试（SQLite 方言） ====================
//
// 覆盖条目：§3 Where两参简写/WhereDate/Having两参、§5 null特判、§8 WhereNull多列、
// §9 Between系列、§10 NullSafe、§11 Like caseSensitive、§12 WhereNot、§13 All/Any/None、
// §14/§26 Exists Builder重载与Or变体、§17 GroupByRaw、§22 HavingNested、§23 HavingNull、
// §24 Aggregate、§32 Increment/Decrement、§33 AddSelect/AddSelectSub、§41 InsertOrIgnoreUsing、
// §43 DeleteJoin、§44 CrossJoinOn、§45 JoinBuilder条件扩展、§46 嵌套join。

// setupSQLiteEventsTable 创建 events 表（日期过滤测试用）。
func setupSQLiteEventsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE events (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		happened_at TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO events (happened_at) VALUES
		('2024-06-15 10:00:00'),
		('2024-06-16 08:30:00'),
		('2024-06-15 23:59:59')`)
}

// setupSQLiteRangesTable 创建 ranges 表（BetweenColumns/ValueBetween 测试用）。
func setupSQLiteRangesTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE ranges (
		id INTEGER PRIMARY KEY,
		val INTEGER NOT NULL,
		lo  INTEGER NOT NULL,
		hi  INTEGER NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO ranges (id, val, lo, hi) VALUES
		(1, 5, 1, 10),
		(2, 15, 1, 10),
		(3, 7, 6, 8)`)
}

// setupSQLiteWalletsTable 创建 wallets 表（Increment/Decrement 测试用）。
func setupSQLiteWalletsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE wallets (
		id      INTEGER PRIMARY KEY,
		balance INTEGER NOT NULL,
		points  INTEGER NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO wallets (id, balance, points) VALUES
		(1, 100, 10),
		(2, 200, 20)`)
}

// setupSQLiteArchiveTable 创建 archive 表（InsertOrIgnoreUsing 测试用）。
func setupSQLiteArchiveTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE archive (
		id    INTEGER PRIMARY KEY AUTOINCREMENT,
		name  TEXT NOT NULL,
		email TEXT UNIQUE
	)`)
	mustExec(t, db, `INSERT INTO archive (name, email) VALUES
		('alice', 'alice@test.com'),
		('zoe', 'zoe@test.com')`)
}

// setupSQLiteColorsTable 创建 colors 表（CrossJoinOn 测试用）。
func setupSQLiteColorsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE colors (
		id   INTEGER PRIMARY KEY,
		name TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO colors (id, name) VALUES (1, 'red'), (2, 'blue')`)
}

// ==================== §3 Where 两参简写 / WhereDate / Having 两参 ====================

// TestSQLiteInteg_NewApi_WhereShorthand 验证 Where 两参简写（缺省 =）。
func TestSQLiteInteg_NewApi_WhereShorthand(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").Where("age", 25).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("Where shorthand: expected 1, got %d", count)
	}

	// 简写与三参混用
	count, err = db.Builder().Table("users").Where("age", 25).OrWhere("name", "=", "bob").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("Where shorthand + OrWhere: expected 2, got %d", count)
	}

	// 三参形式保持兼容
	count, err = db.Builder().Table("users").Where("age", ">", 29).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("Where 3-arg: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_WhereDate 验证 WhereDate：strftime('%Y-%m-%d', col) = ?。
func TestSQLiteInteg_NewApi_WhereDate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteEventsTable(t, db)

	count, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate: expected 2, got %d", count)
	}
}

// TestSQLiteInteg_NewApi_HavingShorthand 验证 Having 两参简写（缺省 =）。
func TestSQLiteInteg_NewApi_HavingShorthand(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Status string `db:"status"`
		Cnt    int    `db:"cnt"`
	}
	var rows []row
	err := db.Builder().Table("users").
		SelectRaw("status, COUNT(*) AS cnt").
		GroupBy("status").
		Having("cnt", 3).
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 1 || rows[0].Status != "active" || rows[0].Cnt != 3 {
		t.Errorf("Having shorthand: expected 1 row (active,3), got %+v", rows)
	}
}

// ==================== §5 Where 值 null 特判 ====================

// TestSQLiteInteg_NewApi_WhereNilValue 验证 nil 值特判：= nil → IS NULL，!=/<> nil → IS NOT NULL。
func TestSQLiteInteg_NewApi_WhereNilValue(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	tests := []struct {
		name     string
		op       string
		expected int
	}{
		{"eq_nil", "=", 1},   // eve
		{"ne_nil", "!=", 4},  // 其余 4 人
		{"ne2_nil", "<>", 4}, // 其余 4 人
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := db.Builder().Table("users").Where("age", tt.op, nil).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// ==================== §8 WhereNull / WhereNotNull 多列 ====================

// TestSQLiteInteg_NewApi_WhereNullMulti 验证 WhereNull/WhereNotNull 变参多列展开。
func TestSQLiteInteg_NewApi_WhereNullMulti(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 单列兼容
	count, err := db.Builder().Table("users").WhereNull("age").Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("WhereNull single: expected 1, got %d", count)
	}

	// 多列 AND 展开：age 且 email 均为 NULL 的行不存在
	count, err = db.Builder().Table("users").WhereNull("age", "email").Count(context.Background())
	assertNoError(t, err)
	if count != 0 {
		t.Errorf("WhereNull multi: expected 0, got %d", count)
	}

	// WhereNotNull 多列
	count, err = db.Builder().Table("users").WhereNotNull("age", "email").Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("WhereNotNull multi: expected 4, got %d", count)
	}
}

// ==================== §9 Between 系列 ====================

// TestSQLiteInteg_NewApi_BetweenSeries 验证 Between 系列 7 个新 API 的真实执行。
func TestSQLiteInteg_NewApi_BetweenSeries(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteRangesTable(t, db)

	type queryCase struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}
	tests := []queryCase{
		{"OrWhereBetween", func(b *Builder) *Builder {
			return b.Table("users").Where("age", ">", 34).OrWhereBetween("age", 25, 26)
		}, 2}, // charlie(35) + alice(25)
		{"WhereNotBetween", func(b *Builder) *Builder {
			return b.Table("users").WhereNotBetween("age", 25, 30)
		}, 1}, // charlie；eve 的 NULL 被排除
		{"OrWhereNotBetween", func(b *Builder) *Builder {
			return b.Table("users").Where("name", "=", "alice").OrWhereNotBetween("age", 25, 30)
		}, 2}, // alice + charlie
		{"WhereBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").WhereBetweenColumns("val", "lo", "hi")
		}, 2}, // id 1、3
		{"WhereNotBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").WhereNotBetweenColumns("val", "lo", "hi")
		}, 1}, // id 2
		{"OrWhereBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").Where("id", "=", 0).OrWhereBetweenColumns("val", "lo", "hi")
		}, 2},
		{"OrWhereNotBetweenColumns", func(b *Builder) *Builder {
			return b.Table("ranges").Where("id", "=", 0).OrWhereNotBetweenColumns("val", "lo", "hi")
		}, 1},
		{"WhereValueBetween", func(b *Builder) *Builder {
			return b.Table("ranges").WhereValueBetween(5, "lo", "hi")
		}, 2}, // id 1、2（区间均为 1~10）
		{"OrWhereValueBetween", func(b *Builder) *Builder {
			return b.Table("ranges").Where("id", "=", 0).OrWhereValueBetween(7, "lo", "hi")
		}, 3}, // 三行区间均含 7
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// ==================== §10 WhereNullSafeEquals / NotEquals ====================

// TestSQLiteInteg_NewApi_NullSafe 验证空安全相等比较（SQLite 编译为 IS / IS NOT）。
func TestSQLiteInteg_NewApi_NullSafe(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	tests := []struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}{
		{"eq_nil", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeEquals("age", nil)
		}, 1}, // eve
		{"eq_value", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeEquals("age", 25)
		}, 1}, // alice
		{"ne_nil", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeNotEquals("age", nil)
		}, 4},
		{"ne_value", func(b *Builder) *Builder {
			return b.Table("users").WhereNullSafeNotEquals("age", 25)
		}, 4}, // bob/charlie/diana/eve（NULL IS NOT 25 为真）
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// ==================== §11 WhereLike caseSensitive ====================

// TestSQLiteInteg_NewApi_WhereLikeCaseSensitive 验证 WhereLike 第三参（SQLite 区分大小写编译为 GLOB）。
func TestSQLiteInteg_NewApi_WhereLikeCaseSensitive(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 默认不区分大小写（LIKE）
	count, err := db.Builder().Table("users").WhereLike("name", "%LI%").Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // alice、charlie
		t.Errorf("default like: expected 2, got %d", count)
	}

	// 区分大小写（GLOB，通配符 * / ?）
	count, err = db.Builder().Table("users").WhereLike("name", "*li*", true).Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("glob match: expected 2, got %d", count)
	}
	count, err = db.Builder().Table("users").WhereLike("name", "*LI*", true).Count(context.Background())
	assertNoError(t, err)
	if count != 0 {
		t.Errorf("glob case-sensitive: expected 0, got %d", count)
	}

	// OrWhereLike
	count, err = db.Builder().Table("users").
		WhereLike("name", "al%").
		OrWhereLike("name", "bo%").
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("OrWhereLike: expected 2, got %d", count)
	}
}

// ==================== §12 WhereNot / OrWhereNot ====================

// TestSQLiteInteg_NewApi_WhereNot 验证 WhereNot/OrWhereNot 闭包整体取反。
func TestSQLiteInteg_NewApi_WhereNot(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").
		WhereNot(func(q *Builder) { q.Where("status", "=", "active") }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // charlie、eve
		t.Errorf("WhereNot: expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("name", "=", "alice").
		OrWhereNot(func(q *Builder) { q.Where("age", ">", 29) }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // alice + diana（28 不 > 29）
		t.Errorf("OrWhereNot: expected 2, got %d", count)
	}
}

// ==================== §13 WhereAll / WhereAny / WhereNone ====================

// TestSQLiteInteg_NewApi_WhereAllAnyNone 验证 WhereAll/Any/None 及 Or 变体。
func TestSQLiteInteg_NewApi_WhereAllAnyNone(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	tests := []struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}{
		{"WhereAll", func(b *Builder) *Builder {
			return b.Table("users").WhereAll(func(q *Builder) {
				q.Where("status", "=", "active").Where("age", ">", 26)
			})
		}, 2}, // bob、diana
		{"WhereAny", func(b *Builder) *Builder {
			return b.Table("users").WhereAny(func(q *Builder) {
				q.Where("name", "=", "alice").Where("age", "=", 35)
			})
		}, 2}, // alice、charlie
		{"WhereNone", func(b *Builder) *Builder {
			return b.Table("users").WhereNone(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, 2}, // charlie、eve
		{"OrWhereAll", func(b *Builder) *Builder {
			return b.Table("users").Where("id", "=", 0).OrWhereAll(func(q *Builder) {
				q.Where("status", "=", "active").Where("age", ">", 26)
			})
		}, 2},
		{"OrWhereAny", func(b *Builder) *Builder {
			return b.Table("users").Where("name", "=", "alice").OrWhereAny(func(q *Builder) {
				q.Where("age", "=", 35)
			})
		}, 2}, // alice、charlie
		{"OrWhereNone", func(b *Builder) *Builder {
			return b.Table("users").Where("id", "=", 0).OrWhereNone(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// ==================== §14/§26 WhereExists Builder 重载 + Or 变体 ====================

// TestSQLiteInteg_NewApi_WhereExistsBuilder 验证 WhereExists 直接传 *Builder 及 Or 变体。
func TestSQLiteInteg_NewApi_WhereExistsBuilder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// 子查询：存在金额 > 100 订单的用户
	sub := func() *Builder {
		return db.Builder().Table("orders").
			WhereRaw("orders.user_id = users.id").
			Where("amount", ">", 100)
	}

	count, err := db.Builder().Table("users").WhereExists(sub()).Count(context.Background())
	assertNoError(t, err)
	if count != 3 { // alice(120)、bob(200)、diana(150)
		t.Errorf("WhereExists(*Builder): expected 3, got %d", count)
	}

	count, err = db.Builder().Table("users").WhereNotExists(sub()).Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // charlie、eve
		t.Errorf("WhereNotExists(*Builder): expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("id", "=", 5).
		OrWhereExists(sub()).
		Count(context.Background())
	assertNoError(t, err)
	if count != 4 { // eve + alice/bob/diana
		t.Errorf("OrWhereExists: expected 4, got %d", count)
	}

	count, err = db.Builder().Table("users").
		Where("id", "=", 1).
		OrWhereNotExists(sub()).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 { // alice + charlie/eve
		t.Errorf("OrWhereNotExists: expected 3, got %d", count)
	}

	// 回调形式保持兼容
	count, err = db.Builder().Table("users").WhereExists(func(q *Builder) {
		q.Table("orders").WhereRaw("orders.user_id = users.id")
	}).Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("WhereExists callback: expected 4, got %d", count)
	}

	// 非法 sub 类型：编译期报错
	_, _, err = db.Builder().Table("users").WhereExists(123).ToSelect()
	if !errors.Is(err, ErrInvalidSubQuery) {
		t.Errorf("expected ErrInvalidSubQuery, got %v", err)
	}
}

// ==================== §17 GroupByRaw ====================

// TestSQLiteInteg_NewApi_GroupByRaw 验证 GroupByRaw 带绑定的原始分组。
func TestSQLiteInteg_NewApi_GroupByRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Grp string `db:"grp"`
		Cnt int    `db:"cnt"`
	}

	// 带绑定的原始分组：绑定顺序 WHERE → GROUP BY
	var rows []row
	err := db.Builder().Table("users").
		SelectRaw("status AS grp, COUNT(*) AS cnt").
		Where("age", ">", 20).
		GroupByRaw("CASE WHEN age > ? THEN status ELSE 'x' END", 34).
		Find(context.Background(), &rows)
	assertNoError(t, err)
	// alice(25)/bob(30)/diana(28) → 'x'；charlie(35) → 'inactive'；eve 被 WHERE 排除
	if len(rows) != 2 {
		t.Fatalf("GroupByRaw with bindings: expected 2 groups, got %d", len(rows))
	}

	// 无绑定形式
	rows = nil
	err = db.Builder().Table("users").
		SelectRaw("status AS grp, COUNT(*) AS cnt").
		GroupByRaw("status").
		OrderBy("grp", "ASC").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 2 {
		t.Fatalf("GroupByRaw: expected 2 groups, got %d", len(rows))
	}
	if rows[0].Grp != "active" || rows[0].Cnt != 3 || rows[1].Grp != "inactive" || rows[1].Cnt != 2 {
		t.Errorf("GroupByRaw result mismatch: %+v", rows)
	}
}

// ==================== §22 HavingNested / OrHavingNested ====================

// TestSQLiteInteg_NewApi_HavingNested 验证 HavingNested/OrHavingNested 括号分组。
func TestSQLiteInteg_NewApi_HavingNested(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// 每用户总额：u1=170 u2=280 u3=30 u4=150
	type row struct {
		UserID int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").
		SelectRaw("user_id, SUM(amount) AS total").
		GroupBy("user_id").
		HavingNested(func(q *Builder) {
			q.Having("total", ">", 100).Having("total", "<", 200)
		}).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 2 || rows[0].UserID != 1 || rows[1].UserID != 4 {
		t.Errorf("HavingNested: expected users [1 4], got %+v", rows)
	}

	rows = nil
	err = db.Builder().Table("orders").
		SelectRaw("user_id, SUM(amount) AS total").
		GroupBy("user_id").
		Having("total", ">", 250).
		OrHavingNested(func(q *Builder) {
			q.Having("total", "=", 30)
		}).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 2 || rows[0].UserID != 2 || rows[1].UserID != 3 {
		t.Errorf("OrHavingNested: expected users [2 3], got %+v", rows)
	}
}

// ==================== §23 HavingNull / HavingNotNull ====================

// TestSQLiteInteg_NewApi_HavingNull 验证 HavingNull/HavingNotNull 聚合后空值过滤。
func TestSQLiteInteg_NewApi_HavingNull(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Status string `db:"status"`
	}

	// null_cnt 恒非 NULL → HavingNotNull 返回全部分组
	var rows []row
	err := db.Builder().Table("users").
		SelectRaw("status, SUM(CASE WHEN age IS NULL THEN 1 ELSE 0 END) AS null_cnt").
		GroupBy("status").
		HavingNotNull("null_cnt").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 2 {
		t.Errorf("HavingNotNull: expected 2 groups, got %d", len(rows))
	}

	rows = nil
	err = db.Builder().Table("users").
		SelectRaw("status, SUM(CASE WHEN age IS NULL THEN 1 ELSE 0 END) AS null_cnt").
		GroupBy("status").
		HavingNull("null_cnt").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	if len(rows) != 0 {
		t.Errorf("HavingNull: expected 0 groups, got %d", len(rows))
	}
}

// ==================== §24 Aggregate 系列 ====================

// TestSQLiteInteg_NewApi_Aggregate 验证 Max/Min/Sum/Avg/Average 真实执行与空表语义。
func TestSQLiteInteg_NewApi_Aggregate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	ctx := context.Background()

	maxAge, err := db.Builder().Table("users").Max(ctx, "age")
	assertNoError(t, err)
	if maxAge != 35 {
		t.Errorf("Max: expected 35, got %v", maxAge)
	}

	minAge, err := db.Builder().Table("users").Min(ctx, "age")
	assertNoError(t, err)
	if minAge != 25 {
		t.Errorf("Min: expected 25, got %v", minAge)
	}

	sumAge, err := db.Builder().Table("users").Sum(ctx, "age")
	assertNoError(t, err)
	if sumAge != 118 { // 25+30+35+28，NULL 忽略
		t.Errorf("Sum: expected 118, got %v", sumAge)
	}

	avgAge, err := db.Builder().Table("users").Avg(ctx, "age")
	assertNoError(t, err)
	if avgAge != 29.5 {
		t.Errorf("Avg: expected 29.5, got %v", avgAge)
	}

	avgAge2, err := db.Builder().Table("users").Average(ctx, "age")
	assertNoError(t, err)
	if avgAge2 != avgAge {
		t.Errorf("Average alias mismatch: %v != %v", avgAge2, avgAge)
	}

	// 带 WHERE 条件
	maxActive, err := db.Builder().Table("users").Where("status", "=", "active").Max(ctx, "age")
	assertNoError(t, err)
	if maxActive != 30 {
		t.Errorf("Max with where: expected 30, got %v", maxActive)
	}

	sumAmount, err := db.Builder().Table("orders").Sum(ctx, "amount")
	assertNoError(t, err)
	if sumAmount != 630 {
		t.Errorf("Sum amount: expected 630, got %v", sumAmount)
	}

	// 空表语义
	mustExec(t, db, `CREATE TABLE empty_t (id INTEGER PRIMARY KEY, amount INTEGER)`)
	sumEmpty, err := db.Builder().Table("empty_t").Sum(ctx, "amount")
	assertNoError(t, err)
	if sumEmpty != 0 {
		t.Errorf("Sum empty: expected 0, got %v", sumEmpty)
	}
	avgEmpty, err := db.Builder().Table("empty_t").Avg(ctx, "amount")
	assertNoError(t, err)
	if avgEmpty != 0 {
		t.Errorf("Avg empty: expected 0, got %v", avgEmpty)
	}
	_, err = db.Builder().Table("empty_t").Max(ctx, "amount")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Max empty: expected sql.ErrNoRows, got %v", err)
	}
	_, err = db.Builder().Table("empty_t").Min(ctx, "amount")
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("Min empty: expected sql.ErrNoRows, got %v", err)
	}
}

// ==================== §32 Increment / Decrement ====================

// TestSQLiteInteg_NewApi_IncrementDecrement 验证原子自增/自减（含多列、保护机制）。
func TestSQLiteInteg_NewApi_IncrementDecrement(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteWalletsTable(t, db)

	ctx := context.Background()

	// 单列自增
	affected, err := db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 50)
	assertNoError(t, err)
	if affected != 1 {
		t.Errorf("Increment affected: expected 1, got %d", affected)
	}
	var balance int
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance").Value(ctx, &balance)
	assertNoError(t, err)
	if balance != 150 {
		t.Errorf("balance after increment: expected 150, got %d", balance)
	}

	// 多列自增（extra 成对传参）
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 10, "points", 5)
	assertNoError(t, err)
	type wallet struct {
		Balance int `db:"balance"`
		Points  int `db:"points"`
	}
	var w wallet
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance", "points").First(ctx, &w)
	assertNoError(t, err)
	if w.Balance != 160 || w.Points != 15 {
		t.Errorf("multi-column increment: expected {160 15}, got %+v", w)
	}

	// 自减
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Decrement(ctx, "balance", 30)
	assertNoError(t, err)
	err = db.Builder().Table("wallets").Where("id", "=", 1).Select("balance").Value(ctx, &balance)
	assertNoError(t, err)
	if balance != 130 {
		t.Errorf("balance after decrement: expected 130, got %d", balance)
	}

	// 保护机制：无 WHERE 拒绝
	_, err = db.Builder().Table("wallets").Increment(ctx, "balance", 1)
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("expected ErrUpdateWithoutWhere, got %v", err)
	}
	_, err = db.Builder().Table("wallets").Decrement(ctx, "balance", 1)
	if !errors.Is(err, ErrUpdateWithoutWhere) {
		t.Errorf("expected ErrUpdateWithoutWhere, got %v", err)
	}

	// Force 放行全表
	_, err = db.Builder().Table("wallets").Force().Increment(ctx, "points", 1)
	assertNoError(t, err)
	var points []int
	err = db.Builder().Table("wallets").OrderBy("id", "ASC").Pluck(ctx, &points, "points")
	assertNoError(t, err)
	if len(points) != 2 || points[0] != 16 || points[1] != 21 {
		t.Errorf("Force increment: expected [16 21], got %v", points)
	}

	// extra 参数非成对 → ErrIncrementColumns
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 1, "points")
	if !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("expected ErrIncrementColumns, got %v", err)
	}
	// extra 列名非 string → ErrIncrementColumns
	_, err = db.Builder().Table("wallets").Where("id", "=", 1).Increment(ctx, "balance", 1, 123, 5)
	if !errors.Is(err, ErrIncrementColumns) {
		t.Errorf("expected ErrIncrementColumns, got %v", err)
	}
}

// ==================== §33 AddSelect / AddSelectSub ====================

// TestSQLiteInteg_NewApi_AddSelect 验证 AddSelect 追加列（去重）与 AddSelectSub 子查询列。
func TestSQLiteInteg_NewApi_AddSelect(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// AddSelect 追加 + 等价列去重
	type user struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var users []user
	err := db.Builder().Table("users").
		Select("id", "name").
		AddSelect("age").
		AddSelect("age"). // 重复列去重
		Where("id", "=", 1).
		Find(context.Background(), &users)
	assertNoError(t, err)
	if len(users) != 1 || users[0].Age != 25 {
		t.Errorf("AddSelect: expected 1 row with age 25, got %+v", users)
	}
	// 编译 SQL 中 age 只出现一次
	sqlStr, _, err := db.Builder().Table("users").Select("id", "name").AddSelect("age", "age").ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT "id", "name", "age" FROM "users"`, sqlStr)

	// AddSelectSub：订单数标量子查询列
	type userWithCount struct {
		ID         int    `db:"id"`
		Name       string `db:"name"`
		OrderCount int64  `db:"order_count"`
	}
	var rows []userWithCount
	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").WhereRaw("orders.user_id = users.id")
	err = db.Builder().Table("users").
		Select("id", "name").
		AddSelectSub(sub, "order_count").
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	expectedCounts := []int64{2, 2, 1, 1, 0}
	if len(rows) != 5 {
		t.Fatalf("AddSelectSub: expected 5 rows, got %d", len(rows))
	}
	for i, want := range expectedCounts {
		if rows[i].OrderCount != want {
			t.Errorf("AddSelectSub row %d: expected %d, got %d", i, want, rows[i].OrderCount)
		}
	}
}

// ==================== §41 InsertOrIgnoreUsing ====================

// TestSQLiteInteg_NewApi_InsertOrIgnoreUsing 验证 INSERT OR IGNORE INTO ... SELECT 冲突忽略。
func TestSQLiteInteg_NewApi_InsertOrIgnoreUsing(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteArchiveTable(t, db)

	// users 中 active：alice/bob/diana；alice 与 archive 冲突 → 忽略，bob/diana 插入
	affected, err := db.Builder().Table("archive").InsertOrIgnoreUsing(
		context.Background(),
		[]string{"name", "email"},
		func(q *Builder) {
			q.Table("users").Select("name", "email").Where("status", "=", "active")
		},
	)
	assertNoError(t, err)
	if affected != 2 {
		t.Errorf("InsertOrIgnoreUsing affected: expected 2, got %d", affected)
	}

	count, err := db.Builder().Table("archive").Count(context.Background())
	assertNoError(t, err)
	if count != 4 { // 原 2 + 新 2
		t.Errorf("archive count: expected 4, got %d", count)
	}
}

// ==================== §43 DeleteJoin ====================

// TestSQLiteInteg_NewApi_DeleteJoin 验证按关联条件删除（SQLite 编译为主键 IN 子查询）。
func TestSQLiteInteg_NewApi_DeleteJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	ctx := context.Background()

	// 删除存在金额 > 100 订单的用户：alice/bob/diana
	affected, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		DeleteJoin(ctx)
	assertNoError(t, err)
	if affected != 3 {
		t.Errorf("DeleteJoin affected: expected 3, got %d", affected)
	}
	count, err := db.Builder().Table("users").Count(ctx)
	assertNoError(t, err)
	if count != 2 { // charlie、eve
		t.Errorf("users after DeleteJoin: expected 2, got %d", count)
	}

	// 有 join 无 where：保护机制放行（join 本身限定了删除范围）
	affected, err = db.Builder().Table("users").
		Join("profiles", "users.id", "=", "profiles.user_id").
		DeleteJoin(ctx)
	assertNoError(t, err)
	if affected != 1 { // charlie（profiles 中有 user_id=3）
		t.Errorf("DeleteJoin join-only affected: expected 1, got %d", affected)
	}

	// 无 join 无 where：保护机制拒绝
	_, err = db.Builder().Table("users").DeleteJoin(ctx)
	if !errors.Is(err, ErrDeleteWithoutWhere) {
		t.Errorf("expected ErrDeleteWithoutWhere, got %v", err)
	}

	// Force 绕过保护但无 join：ErrDeleteJoinNoJoin
	_, err = db.Builder().Table("users").Force().DeleteJoin(ctx)
	if !errors.Is(err, ErrDeleteJoinNoJoin) {
		t.Errorf("expected ErrDeleteJoinNoJoin, got %v", err)
	}
}

// ==================== §44 CrossJoinOn ====================

// TestSQLiteInteg_NewApi_CrossJoinOn 验证带 ON 条件的 CROSS JOIN（列到列比较）。
func TestSQLiteInteg_NewApi_CrossJoinOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteColorsTable(t, db)

	count, err := db.Builder().Table("users").
		CrossJoinOn("colors", "colors.id", "=", "users.id").
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // colors 仅 id 1、2 与 users 匹配
		t.Errorf("CrossJoinOn: expected 2, got %d", count)
	}
}

// ==================== §45 JoinBuilder 条件扩展 ====================

// TestSQLiteInteg_NewApi_JoinBuilderConditions 验证 JoinBuilder 新增条件方法（Where/OrWhere/WhereNull 族/WhereIn/WhereNested/WhereExists/子查询值）。
func TestSQLiteInteg_NewApi_JoinBuilderConditions(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	tests := []struct {
		name     string
		build    func(*Builder) *Builder
		expected int
	}{
		{"JoinOn_Where", func(b *Builder) *Builder {
			return b.Table("users").JoinOn("orders", func(j *JoinBuilder) {
				j.On("users.id", "=", "orders.user_id").Where("orders.amount", ">", 100)
			})
		}, 3}, // alice/bob/diana 各 1 条 > 100 订单
		{"JoinOn_WhereOrNested", func(b *Builder) *Builder {
			// OR 条件必须包在括号组内，否则 OR 优先级会破坏 ON 连接
			return b.Table("users").JoinOn("orders", func(j *JoinBuilder) {
				j.On("users.id", "=", "orders.user_id").WhereNested(func(j2 *JoinBuilder) {
					j2.Where("orders.amount", "=", 50).OrWhere("orders.amount", "=", 200)
				})
			})
		}, 2}, // alice(Book)、bob(TV)
		{"JoinOn_WhereIn", func(b *Builder) *Builder {
			return b.Table("users").JoinOn("orders", func(j *JoinBuilder) {
				j.On("users.id", "=", "orders.user_id").
					WhereIn("orders.product", []any{"Book", "Pen"})
			})
		}, 2}, // alice、charlie
		{"JoinOn_WhereNotNull", func(b *Builder) *Builder {
			return b.Table("users").JoinOn("profiles", func(j *JoinBuilder) {
				j.On("users.id", "=", "profiles.user_id").WhereNotNull("profiles.bio")
			})
		}, 3},
		{"JoinOn_WhereSubValue", func(b *Builder) *Builder {
			maxAmount := db.Builder().Table("orders").SelectRaw("MAX(amount)")
			return b.Table("users").JoinOn("orders", func(j *JoinBuilder) {
				j.On("users.id", "=", "orders.user_id").Where("orders.amount", "<", maxAmount)
			})
		}, 5}, // 除 200 外的 5 条订单
		{"JoinOn_WhereExists", func(b *Builder) *Builder {
			return b.Table("users").JoinOn("orders", func(j *JoinBuilder) {
				j.On("users.id", "=", "orders.user_id").
					WhereExists(db.Builder().Table("profiles").WhereRaw("profiles.user_id = users.id"))
			})
		}, 5}, // u1/u2/u3 有 profile，订单共 5 条
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			count, err := tt.build(db.Builder()).Count(context.Background())
			assertNoError(t, err)
			if count != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, count)
			}
		})
	}
}

// ==================== §46 嵌套 join ====================

// TestSQLiteInteg_NewApi_NestedJoin 验证 JoinBuilder.JoinOn 嵌套 join 组（括号形式）。
func TestSQLiteInteg_NewApi_NestedJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	// users JOIN (profiles JOIN orders ON profiles.user_id = orders.user_id AND orders.amount > 100)
	// ON users.id = profiles.user_id
	count, err := db.Builder().Table("users").
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

	// 编译 SQL 验证括号形态
	sqlStr, _, err := db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				JoinOn("orders", func(j2 *JoinBuilder) {
					j2.On("profiles.user_id", "=", "orders.user_id")
				})
		}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t,
		`SELECT * FROM "users" INNER JOIN ("profiles" INNER JOIN "orders" ON "profiles"."user_id" = "orders"."user_id") ON "users"."id" = "profiles"."user_id"`,
		sqlStr)
}
