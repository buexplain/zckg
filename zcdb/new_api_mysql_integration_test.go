package zcdb

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// ==================== new-api-implement-list.md 新增 API 集成测试（MySQL 方言） ====================
//
// 重点覆盖 MySQL 方言分支：WhereDate date()、NullSafe <=>、Like BINARY、
// InsertOrIgnoreUsing INSERT IGNORE、DeleteJoin 多表直译、CrossJoinOn 带 ON、
// §51 加锁 select 走写连接。通用语义已在 SQLite 集成测试覆盖，此处做真实库复核。

// setupMySQLNewApiTables 清理并创建 MySQL 新增 API 测试专用表。
func setupMySQLNewApiTables(t *testing.T, db *DBDao) {
	t.Helper()
	for _, table := range []string{"events", "wallets", "archive", "colors", "names_cs"} {
		mustExec(t, db, "DROP TABLE IF EXISTS `"+table+"`")
	}
}

// setupMySQLEventsTable 创建 events 表（WhereDate 测试用）。
func setupMySQLEventsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE events (
		id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		happened_at DATETIME NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO events (happened_at) VALUES
		('2024-06-15 10:00:00'),
		('2024-06-16 08:30:00'),
		('2024-06-15 23:59:59')`)
}

// setupMySQLWalletsTable 创建 wallets 表（Increment/Decrement 测试用）。
func setupMySQLWalletsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE wallets (
		id      BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		balance BIGINT NOT NULL,
		points  BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO wallets (id, balance, points) VALUES (1, 100, 10), (2, 200, 20)`)
}

// setupMySQLArchiveTable 创建 archive 表（InsertOrIgnoreUsing 测试用）。
func setupMySQLArchiveTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE archive (
		id    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name  VARCHAR(64) NOT NULL,
		email VARCHAR(128) NULL,
		UNIQUE KEY uk_email (email)
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO archive (name, email) VALUES
		('alice', 'alice@test.com'),
		('zoe', 'zoe@test.com')`)
}

// setupMySQLColorsTable 创建 colors 表（CrossJoinOn 测试用）。
func setupMySQLColorsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE colors (
		id   BIGINT UNSIGNED NOT NULL PRIMARY KEY,
		name VARCHAR(16) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO colors (id, name) VALUES (1, 'red'), (2, 'blue')`)
}

// setupMySQLNamesCsTable 创建大小写混合的名字表（Like caseSensitive 测试用）。
func setupMySQLNamesCsTable(t *testing.T, db *DBDao) {
	t.Helper()
	mustExec(t, db, `CREATE TABLE names_cs (
		id   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(64) NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`)
	mustExec(t, db, `INSERT INTO names_cs (name) VALUES ('alice'), ('Alice'), ('BOB')`)
}

// ==================== §3 WhereDate（MySQL 分支：date(col)） ====================

// TestMySQLInteg_NewApi_WhereDate 验证 WhereDate 在 MySQL 上编译为 date(col) = ? 并真实执行。
func TestMySQLInteg_NewApi_WhereDate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLEventsTable(t, db)

	// 编译形态断言
	sqlStr, args, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `events` WHERE date(`happened_at`) = ?", sqlStr)
	assertArgs(t, []any{"2024-06-15"}, args)

	count, err := db.Builder().Table("events").WhereDate("happened_at", "2024-06-15").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereDate: expected 2, got %d", count)
	}
}

// ==================== §3/§5 简写与 null 特判（真实库复核） ====================

// TestMySQLInteg_NewApi_SugarAndNil 验证 Where 两参简写与 nil 特判在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_SugarAndNil(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	count, err := db.Builder().Table("users").Where("age", 25).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("Where shorthand: expected 1, got %d", count)
	}

	count, err = db.Builder().Table("users").Where("age", "=", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("Where =nil: expected 1, got %d", count)
	}

	count, err = db.Builder().Table("users").Where("age", "<>", nil).Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("Where <>nil: expected 4, got %d", count)
	}
}

// ==================== §10 NullSafe（MySQL 分支：<=>） ====================

// TestMySQLInteg_NewApi_NullSafe 验证空安全比较在 MySQL 上编译为 <=> / NOT <=> 并真实执行。
func TestMySQLInteg_NewApi_NullSafe(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("users").WhereNullSafeEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `age` <=> ?", sqlStr)
	sqlStr, _, err = db.Builder().Table("users").WhereNullSafeNotEquals("age", 25).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE NOT `age` <=> ?", sqlStr)

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

// ==================== §11 WhereLike caseSensitive（MySQL 分支：BINARY） ====================

// TestMySQLInteg_NewApi_WhereLikeCaseSensitive 验证 WhereLike 区分大小写编译为 BINARY col LIKE 并真实执行。
func TestMySQLInteg_NewApi_WhereLikeCaseSensitive(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLNamesCsTable(t, db)

	// 默认不区分大小写：alice + Alice
	count, err := db.Builder().Table("names_cs").WhereLike("name", "%lic%").Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("default like: expected 2, got %d", count)
	}

	// 区分大小写编译形态
	sqlStr, _, err := db.Builder().Table("names_cs").WhereLike("name", "%lic%", true).ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `names_cs` WHERE BINARY `name` LIKE ?", sqlStr)

	// 区分大小写真实执行：a% 仅命中 'alice'（'Alice' 大写开头不匹配）
	count, err = db.Builder().Table("names_cs").WhereLike("name", "a%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("binary like a%%: expected 1, got %d", count)
	}
	count, err = db.Builder().Table("names_cs").WhereLike("name", "A%", true).Count(context.Background())
	assertNoError(t, err)
	if count != 1 {
		t.Errorf("binary like A%%: expected 1, got %d", count)
	}
}

// ==================== §24 Aggregate（真实库复核） ====================

// TestMySQLInteg_NewApi_Aggregate 验证 Max/Min/Sum/Avg 在 MySQL 上的真实执行与空表语义。
func TestMySQLInteg_NewApi_Aggregate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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
	avgAge, err := db.Builder().Table("users").Avg(ctx, "age")
	assertNoError(t, err)
	if avgAge != 29.5 {
		t.Errorf("Avg: expected 29.5, got %v", avgAge)
	}

	// 空表语义
	mustExec(t, db, "DROP TABLE IF EXISTS `empty_t`")
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
	mustExec(t, db, "DROP TABLE IF EXISTS `empty_t`")
}

// ==================== §32 Increment / Decrement（真实库复核） ====================

// TestMySQLInteg_NewApi_IncrementDecrement 验证原子自增/自减在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_IncrementDecrement(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLNewApiTables(t, db)
	setupMySQLWalletsTable(t, db)

	ctx := context.Background()

	// 编译形态断言：SET col = col + ?
	sqlStr, args, err := db.Builder().Table("wallets").Where("id", "=", 1).ToIncrement([]string{"balance", "points"}, []any{10, 5})
	assertNoError(t, err)
	assertSQL(t, "UPDATE `wallets` SET `balance` = `balance` + ?, `points` = `points` + ? WHERE `id` = ?", sqlStr)
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

// ==================== §41 InsertOrIgnoreUsing（MySQL 分支：INSERT IGNORE） ====================

// TestMySQLInteg_NewApi_InsertOrIgnoreUsing 验证 INSERT IGNORE INTO ... SELECT 冲突忽略。
func TestMySQLInteg_NewApi_InsertOrIgnoreUsing(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLNewApiTables(t, db)
	setupMySQLArchiveTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("archive").ToInsertOrIgnoreUsing([]string{"name", "email"}, func(q *Builder) {
		q.Table("users").Select("name", "email").Where("status", "=", "active")
	})
	assertNoError(t, err)
	assertSQL(t,
		"INSERT IGNORE INTO `archive` (`name`, `email`) SELECT `name`, `email` FROM `users` WHERE `status` = ?",
		sqlStr)

	// 真实执行：alice 冲突忽略，bob/diana 插入
	affected, err := db.Builder().Table("archive").InsertOrIgnoreUsing(
		context.Background(),
		[]string{"name", "email"},
		func(q *Builder) {
			q.Table("users").Select("name", "email").Where("status", "=", "active")
		},
	)
	assertNoError(t, err)
	if affected != 2 {
		t.Errorf("affected: expected 2, got %d", affected)
	}
	count, err := db.Builder().Table("archive").Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Errorf("archive count: expected 4, got %d", count)
	}
}

// ==================== §43 DeleteJoin（MySQL 分支：多表直译） ====================

// TestMySQLInteg_NewApi_DeleteJoin 验证 MySQL 多表 DELETE 直译形态与真实执行。
func TestMySQLInteg_NewApi_DeleteJoin(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	// 编译形态断言
	sqlStr, args, err := db.Builder().Table("users").
		Join("orders", "users.id", "=", "orders.user_id").
		Where("orders.amount", ">", 100).
		ToDeleteJoin()
	assertNoError(t, err)
	assertSQL(t,
		"DELETE `users` FROM `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id` WHERE `orders`.`amount` > ?",
		sqlStr)
	assertArgs(t, []any{100}, args)

	// 真实执行
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

// ==================== §44 CrossJoinOn（MySQL 支持 CROSS JOIN ... ON） ====================

// TestMySQLInteg_NewApi_CrossJoinOn 验证 MySQL CROSS JOIN 带 ON 的编译形态与真实执行。
func TestMySQLInteg_NewApi_CrossJoinOn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLNewApiTables(t, db)
	setupMySQLColorsTable(t, db)

	sqlStr, _, err := db.Builder().Table("users").CrossJoinOn("colors", "colors.id", "=", "users.id").ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` CROSS JOIN `colors` ON `colors`.`id` = `users`.`id`", sqlStr)

	count, err := db.Builder().Table("users").CrossJoinOn("colors", "colors.id", "=", "users.id").Count(context.Background())
	assertNoError(t, err)
	if count != 2 { // colors 仅 id 1、2 与 users 匹配
		t.Errorf("CrossJoinOn: expected 2, got %d", count)
	}
}

// ==================== §17/§22/§23 GroupByRaw / HavingNested / HavingNull（真实库复核） ====================

// TestMySQLInteg_NewApi_GroupHaving 验证 GroupByRaw/HavingNested/HavingNull 在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_GroupHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLOrdersTable(t, db)

	type row struct {
		Total float64 `db:"total"`
	}

	// GroupByRaw 带绑定 + HavingNested
	// 注意：MySQL only_full_group_by 模式下 SELECT 非聚合列必须与 GROUP BY 表达式一致，
	// 故 SELECT 仅保留聚合列，分组表达式用带绑定的原始 SQL
	var rows []row
	err := db.Builder().Table("orders").
		SelectRaw("SUM(amount) AS total").
		Where("amount", ">", 10).
		GroupByRaw("user_id + ?", 0).
		HavingNested(func(q *Builder) {
			q.Having("total", ">", 100).Having("total", "<", 200)
		}).
		Find(context.Background(), &rows)
	assertNoError(t, err)
	// 各用户总额：u1=170 u2=280 u3=30 u4=150 → HAVING (100,200) 命中 u1、u4
	if len(rows) != 2 {
		t.Errorf("GroupByRaw+HavingNested: expected 2 groups, got %+v", rows)
	}

	// HavingNull/HavingNotNull
	type gRow struct {
		Status string `db:"grp"`
	}
	var gRows []gRow
	err = db.Builder().Table("orders").
		SelectRaw("user_id AS grp, SUM(amount) AS total").
		GroupBy("user_id").
		HavingNotNull("total").
		Find(context.Background(), &gRows)
	assertNoError(t, err)
	if len(gRows) != 4 {
		t.Errorf("HavingNotNull: expected 4 groups, got %d", len(gRows))
	}
	gRows = nil
	err = db.Builder().Table("orders").
		SelectRaw("user_id AS grp, SUM(amount) AS total").
		GroupBy("user_id").
		HavingNull("total").
		Find(context.Background(), &gRows)
	assertNoError(t, err)
	if len(gRows) != 0 {
		t.Errorf("HavingNull: expected 0 groups, got %d", len(gRows))
	}
}

// ==================== §12/§13 WhereNot / All/Any/None（真实库复核） ====================

// TestMySQLInteg_NewApi_WhereNotAllAnyNone 验证 WhereNot 与 All/Any/None 在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_WhereNotAllAnyNone(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	count, err := db.Builder().Table("users").
		WhereNot(func(q *Builder) { q.Where("status", "=", "active") }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereNot: expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		WhereAll(func(q *Builder) { q.Where("status", "=", "active").Where("age", ">", 26) }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereAll: expected 2, got %d", count)
	}

	count, err = db.Builder().Table("users").
		WhereNone(func(q *Builder) { q.Where("status", "=", "active") }).
		Count(context.Background())
	assertNoError(t, err)
	if count != 2 {
		t.Errorf("WhereNone: expected 2, got %d", count)
	}
}

// ==================== §14 WhereExists(*Builder)（真实库复核） ====================

// TestMySQLInteg_NewApi_WhereExistsBuilder 验证 WhereExists 直传 *Builder 在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_WhereExistsBuilder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	sub := db.Builder().Table("orders").
		WhereRaw("orders.user_id = users.id").
		Where("amount", ">", 100)
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
	if count != 2 { // eve(id=5) + charlie（无 > 100 订单）
		t.Errorf("OrWhereNotExists: expected 2, got %d", count)
	}
}

// ==================== §33 AddSelectSub（真实库复核） ====================

// TestMySQLInteg_NewApi_AddSelectSub 验证 AddSelectSub 标量子查询列在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_AddSelectSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

	type userWithCount struct {
		ID         int64 `db:"id"`
		OrderCount int64 `db:"order_count"`
	}
	var rows []userWithCount
	sub := db.Builder().Table("orders").SelectRaw("COUNT(*)").WhereRaw("orders.user_id = users.id")
	err := db.Builder().Table("users").
		Select("id").
		AddSelectSub(sub, "order_count").
		OrderBy("id", "ASC").
		Find(context.Background(), &rows)
	assertNoError(t, err)
	expected := []int64{2, 2, 1, 1, 0}
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows, got %d", len(rows))
	}
	for i, want := range expected {
		if rows[i].OrderCount != want {
			t.Errorf("row %d: expected %d, got %d", i, want, rows[i].OrderCount)
		}
	}
}

// ==================== §45/§46 JoinBuilder 条件扩展 + 嵌套 join（真实库复核） ====================

// TestMySQLInteg_NewApi_JoinBuilder 验证 JoinBuilder 条件与嵌套 join 在 MySQL 上的真实执行。
func TestMySQLInteg_NewApi_JoinBuilder(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)
	setupMySQLProfilesTable(t, db)

	// JoinBuilder.Where 条件
	count, err := db.Builder().Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("users.id", "=", "orders.user_id").Where("orders.amount", ">", 100)
	}).Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Errorf("JoinOn Where: expected 3, got %d", count)
	}

	// 嵌套 join 组括号形态编译断言
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
		"SELECT * FROM `users` INNER JOIN (`profiles` INNER JOIN `orders` ON `profiles`.`user_id` = `orders`.`user_id`) ON `users`.`id` = `profiles`.`user_id`",
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

// ==================== §51 加锁 select 走写连接（MySQL） ====================

// TestMySQLInteg_NewApi_LockSelect 验证带锁查询在 MySQL 事务中的真实执行（FOR UPDATE / LOCK IN SHARE MODE）。
func TestMySQLInteg_NewApi_LockSelect(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("users").Where("id", "=", 1).LockForUpdate().ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `id` = ? FOR UPDATE", sqlStr)

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
