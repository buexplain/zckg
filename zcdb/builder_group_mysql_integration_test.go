// 本文件为 MySQL 集成测试——GroupBy/Having 分组与分组过滤。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
package zcdb

import (
	"context"
	_ "github.com/go-sql-driver/mysql"
	"testing"
)

// TestMySQLInteg_GroupByHaving 验证 GROUP BY + HAVING 聚合过滤。
func TestMySQLInteg_GroupByHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_HavingBetween 验证 HAVING BETWEEN 聚合过滤。
func TestMySQLInteg_HavingBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_HavingBasic 验证 Having 基本用法：HAVING SUM(amount) > 100。
func TestMySQLInteg_HavingBasic(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_OrHaving 验证 OrHaving：HAVING SUM>200 OR SUM<50。
func TestMySQLInteg_OrHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_HavingNotBetween 验证 HavingNotBetween：HAVING SUM NOT BETWEEN 100 AND 200。
func TestMySQLInteg_HavingNotBetween(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_CountWithGroupBy 验证 MySQL 上 GROUP BY 的 Count 真实执行：
// 返回分组数量（非第一组行数）。
func TestMySQLInteg_CountWithGroupBy(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_CountWithGroupByHaving 验证 MySQL 上 GROUP BY + HAVING 的 Count 真实执行。
// 注意：MySQL 默认开启 ONLY_FULL_GROUP_BY，列替换为常量后子查询合法。
func TestMySQLInteg_CountWithGroupByHaving(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_HavingExpression 验证 HAVING 值传 Expression 时真实执行（直接内嵌 SQL）。
func TestMySQLInteg_HavingExpression(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLOrdersTable(t, db)

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

// TestMySQLInteg_GroupLatestPerFund 验证「分组取最新」：JOIN 派生表取每只基金 MAX(ed) 的完整记录。
// 等价 SQL：
//
//	SELECT t1.* FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN (?, ?) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  WHERE t1.fund_code IN (?, ?)
//
// 预期：A → 2024-03-01/1.50，B → 2024-02-01/2.30；C 不在查询范围不返回。
func TestMySQLInteg_GroupLatestPerFund(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS fund_net_value")
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INT AUTO_INCREMENT PRIMARY KEY,
		fund_code VARCHAR(20) NOT NULL,
		ed        VARCHAR(10) NOT NULL,
		net_value DOUBLE NOT NULL
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

// TestMySQLInteg_GroupLatestWindow 验证「分组取最新」窗口函数写法：
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
func TestMySQLInteg_GroupLatestWindow(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS fund_net_value")
	mustExec(t, db, `CREATE TABLE fund_net_value (
		id        INT AUTO_INCREMENT PRIMARY KEY,
		fund_code VARCHAR(20) NOT NULL,
		ed        VARCHAR(10) NOT NULL,
		net_value DOUBLE NOT NULL
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

// TestMySQLInteg_HavingThenFirst
// 分组聚合后 First 取数，having 绑定正确传递。
func TestMySQLInteg_HavingThenFirst(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
