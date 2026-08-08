// 本文件为 SQLite 集成测试——GroupBy/Having 分组与分组过滤。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	_ "modernc.org/sqlite"
	"testing"
)

// TestSQLiteInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤：按 user_id 分组后筛选 SUM(amount) > 100 的组。
func TestSQLiteInteg_GroupByHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 100).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 groups with total > 100, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_HavingBetween 验证 HAVING BETWEEN 聚合过滤。
func TestSQLiteInteg_HavingBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingBetween("SUM(amount)", 100, 200).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170, user2=280, user3=30, user4=150 → user1(170) and user4(150) in [100,200]
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_HavingBasic 验证 Having 基本用法：HAVING SUM(amount) > 100。
func TestSQLiteInteg_HavingBasic(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170, user2=280, user4=150 > 100 → 3 groups
	if len(rows) != 3 {
		t.Errorf("expected 3 groups, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_OrHaving 验证 OrHaving：HAVING SUM>200 OR SUM<50。
func TestSQLiteInteg_OrHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 200).
		OrHaving("SUM(amount)", "<", 50).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170, user2=280, user3=30, user4=150
	// SUM>200: user2(280); SUM<50: user3(30) → 2
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_HavingNotBetween 验证 HavingNotBetween：HAVING SUM NOT BETWEEN 100 AND 200。
func TestSQLiteInteg_HavingNotBetween(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingNotBetween("SUM(amount)", 100, 200).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user1=170 [100,200], user2=280 outside, user3=30 outside, user4=150 [100,200]
	// NOT BETWEEN → user2, user3 → 2
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_CountWithGroupBy 验证 GROUP BY 下的 Count 行为。
// 当前行为（BUG）：生成 SELECT COUNT(*) FROM orders GROUP BY user_id，
// 返回每组一行的计数，Count 只取第一行（user_id=1 的 2），与"记录总数"语义不符。
// 期望行为：返回分组数量（4）。
func TestSQLiteInteg_CountWithGroupBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// orders 共 6 条：user_id 1×2、2×2、3×1、4×1 → 4 个分组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}
	if count != 4 {
		t.Fatalf("Count with GROUP BY: expected 4 (number of groups), got %d", count)
	}
}

// TestSQLiteInteg_CountWithGroupByHaving 验证 GROUP BY + HAVING 的 Count 真实执行：
// 返回满足 HAVING 条件的分组数量（非第一组行数）。
func TestSQLiteInteg_CountWithGroupByHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// 各组 SUM(amount)：user1=170、user2=280、user3=30、user4=150 → >100 的有 3 组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("Count with GROUP BY + HAVING: expected 3, got %d", count)
	}
}

// TestSQLiteInteg_HavingExpression 验证 HAVING 值传 Expression 时真实执行（直接内嵌 SQL）。
func TestSQLiteInteg_HavingExpression(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// 各组 SUM(amount)：170/280/30/150 → >100 的有 3 组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", NewExpression("100")).
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("Having with Expression: expected 3 groups, got %d", count)
	}
}

// TestSQLiteInteg_GroupLatestPerFund 验证「分组取最新」：JOIN 派生表取每只基金 MAX(ed) 的完整记录。
// 等价 SQL：
//
//	SELECT t1.* FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN (?, ?) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  WHERE t1.fund_code IN (?, ?)
//
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestSQLiteInteg_GroupLatestPerFund(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	codes := []any{"A", "B"}
	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		WhereIn("fund_code", codes).
		GroupBy("fund_code")

	type row struct {
		FundCode string  `db:"fund_code"`
		Ed       string  `db:"ed"`
		NetValue float64 `db:"net_value"`
	}
	var rows []row
	err := db.Builder().Table("fund_net_value AS t1").
		Select("t1.*").
		JoinSub(sub, "t2", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t2.fund_code").
				On("t1.ed", "=", "t2.ed")
		}).
		WhereIn("t1.fund_code", codes).
		OrderBy("t1.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 latest rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Ed != "2024-03-01" || rows[0].NetValue != 1.50 {
		t.Errorf("row[0]: expected A/2024-03-01/1.50, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Ed != "2024-02-01" || rows[1].NetValue != 2.30 {
		t.Errorf("row[1]: expected B/2024-02-01/2.30, got %+v", rows[1])
	}
}

// TestSQLiteInteg_GroupLatestWindow 验证「分组取最新」窗口函数写法：
// ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) 取每组最新一条，结果与 JoinSub 版一致。
// 等价 SQL：
//
//	SELECT x.fund_code, x.ed, x.net_value
//	FROM (
//	  SELECT fund_code, ed, net_value,
//	    ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) AS rn
//	  FROM fund_net_value
//	) AS x
//	WHERE x.fund_code IN (?, ?) AND x.rn = 1
//
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestSQLiteInteg_GroupLatestWindow(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INTEGER PRIMARY KEY AUTOINCREMENT,
		fund_code TEXT NOT NULL,
		ed        TEXT NOT NULL,
		net_value REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO fund_net_value (fund_code, ed, net_value) VALUES
		('A', '2024-01-01', 1.00),
		('A', '2024-02-01', 1.20),
		('A', '2024-03-01', 1.50),
		('B', '2024-01-01', 2.00),
		('B', '2024-02-01', 2.30),
		('C', '2024-01-01', 3.00)`)

	codes := []any{"A", "B"}
	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "ed", "net_value",
			"ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) AS rn")

	type row struct {
		FundCode string  `db:"fund_code"`
		Ed       string  `db:"ed"`
		NetValue float64 `db:"net_value"`
	}
	var rows []row
	err := db.Builder().TableSub(sub, "x").
		Select("x.fund_code", "x.ed", "x.net_value").
		WhereIn("x.fund_code", codes).
		Where("x.rn", "=", 1).
		OrderBy("x.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 latest rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Ed != "2024-03-01" || rows[0].NetValue != 1.50 {
		t.Errorf("row[0]: expected A/2024-03-01/1.50, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Ed != "2024-02-01" || rows[1].NetValue != 2.30 {
		t.Errorf("row[1]: expected B/2024-02-01/2.30, got %+v", rows[1])
	}
}

// TestSQLiteInteg_HavingThenFirst
// 分组聚合后 First 取数，having 绑定正确传递。
func TestSQLiteInteg_HavingThenFirst(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type groupRow struct {
		Status string `db:"status"`
	}
	var g groupRow
	// 两组均满足 COUNT(*)>1，按 status 排序取第一组（active）
	err := db.Builder().Table("users").
		Select("status").
		GroupBy("status").
		HavingRaw("COUNT(*) > ?", 1).
		OrderBy("status", "ASC").
		First(context.Background(), &g)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if g.Status != "active" {
		t.Errorf("expected active group, got %q", g.Status)
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
