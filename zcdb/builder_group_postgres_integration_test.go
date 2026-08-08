// 本文件为 PostgreSQL 集成测试——GroupBy/Having 分组与分组过滤。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	_ "github.com/lib/pq"
	"testing"
)

// TestPgInteg_HavingBetween 验证 HAVING BETWEEN：分组后使用 BETWEEN 过滤。
func TestPgInteg_HavingBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingBetween("SUM(amount)", 50, 200).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// user 1: 170, user 2: 280, user 3: 30, user 4: 150 → BETWEEN 50 AND 200: user 1(170), user 4(150)
	if len(rows) != 2 {
		t.Errorf("expected 2 groups, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤。
func TestPgInteg_GroupByHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		UserId int `db:"user_id"`
	}
	var rows []row
	err := db.Builder().Table("orders").Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > $1", 100).
		OrderBy("user_id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 groups with total > 100, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_HavingBasic 验证 Having 基本用法：HAVING SUM(amount) > 100。
func TestPgInteg_HavingBasic(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_OrHaving 验证 OrHaving：HAVING SUM>200 OR SUM<50。
func TestPgInteg_OrHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_HavingNotBetween 验证 HavingNotBetween：HAVING SUM NOT BETWEEN 100 AND 200。
func TestPgInteg_HavingNotBetween(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_CountWithGroupBy 验证 PostgreSQL 上 GROUP BY 的 Count 真实执行：
// 返回分组数量（非第一组行数）。
func TestPgInteg_CountWithGroupBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

	// orders 共 6 条：user_id 1×2、2×2、3×1、4×1 → 4 个分组
	count, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Count(context.Background())
	assertNoError(t, err)
	if count != 4 {
		t.Fatalf("Count with GROUP BY: expected 4 (number of groups), got %d", count)
	}
}

// TestPgInteg_CountWithGroupByHaving 验证 PostgreSQL 上 GROUP BY + HAVING 的 Count 真实执行。
func TestPgInteg_CountWithGroupByHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_HavingExpression 验证 HAVING 值传 Expression 时真实执行（直接内嵌 SQL）。
func TestPgInteg_HavingExpression(t *testing.T) {
	db := openPgTestDB(t)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_GroupLatestPerFund 验证「分组取最新」：JOIN 派生表取每只基金 MAX(ed) 的完整记录。
// 等价 SQL：
//
//	SELECT t1.* FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN ($1, $2) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  WHERE t1.fund_code IN ($3, $4)
//
// 该用例同时隐式验证 PG 占位符编号：派生表子查询与主查询共享 $N 递增计数器（$1-$4 连续）。
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestPgInteg_GroupLatestPerFund(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS fund_net_value")
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        SERIAL PRIMARY KEY,
		fund_code VARCHAR(20) NOT NULL,
		ed        VARCHAR(10) NOT NULL,
		net_value DOUBLE PRECISION NOT NULL
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

// TestPgInteg_GroupLatestWindow 验证「分组取最新」窗口函数写法：
// ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) 取每组最新一条，结果与 JoinSub 版一致。
// 等价 SQL：
//
//	SELECT x.fund_code, x.ed, x.net_value
//	FROM (
//	  SELECT fund_code, ed, net_value,
//	    ROW_NUMBER() OVER (PARTITION BY fund_code ORDER BY ed DESC) AS rn
//	  FROM fund_net_value
//	) AS x
//	WHERE x.fund_code IN ($1, $2) AND x.rn = $3
//
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestPgInteg_GroupLatestWindow(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS fund_net_value")
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        SERIAL PRIMARY KEY,
		fund_code VARCHAR(20) NOT NULL,
		ed        VARCHAR(10) NOT NULL,
		net_value DOUBLE PRECISION NOT NULL
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

// TestPgInteg_HavingThenFirst
// 分组聚合后 First 取数，having 绑定正确传递。
func TestPgInteg_HavingThenFirst(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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
