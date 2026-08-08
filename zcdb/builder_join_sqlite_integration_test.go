// 本文件为 SQLite 集成测试——Join 系列与 JoinBuilder 连接条件。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	_ "modernc.org/sqlite"
	"testing"
)

// TestSQLiteInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行，无订单的用户不出现。
func TestSQLiteInteg_InnerJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		Join("orders", "users.id", "=", "orders.user_id").
		Distinct().
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 users with orders, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留，无匹配的右表列为 NULL。
func TestSQLiteInteg_LeftJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		LeftJoin("orders", "users.id", "=", "orders.user_id").
		Distinct().
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 users with left join, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积：结果行数 = 左表行数 × 右表行数。
func TestSQLiteInteg_CrossJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").CrossJoin("orders").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestSQLiteInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：在 ON 子句中附加额外过滤条件（amount > 100）。
func TestSQLiteInteg_JoinOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Where("orders.amount", ">", 100)
		}).
		OrderBy("users.name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows with amount > 100, got %d", len(rows))
	}
}

// TestSQLiteInteg_LeftJoinOnOrOn 验证 LEFT JOIN ON + OR ON 条件。
func TestSQLiteInteg_LeftJoinOnOrOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteProfilesTable(t, db)

	type row struct {
		Name string  `db:"name"`
		Bio  *string `db:"bio"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "profiles.bio").
		LeftJoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").
				OrOn("profiles.active", "=", "users.id")
		}).
		OrderBy("users.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// 所有 5 个用户都应保留（LEFT JOIN）
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_RightJoin 验证 RIGHT JOIN：右表所有行都保留（SQLite 3.39+）。
func TestSQLiteInteg_RightJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name *string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		RightJoin("orders", "users.id", "=", "orders.user_id").
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// orders 有 6 行，所有 user_id 都有效 → 6 行
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// TestSQLiteInteg_RightJoinOn 验证 RightJoinOn 多条件：RIGHT JOIN + 回调式 ON 条件。
func TestSQLiteInteg_RightJoinOn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name *string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		RightJoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Where("orders.amount", ">", 100)
		}).
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// TestSQLiteInteg_JoinOnOrWhere 验证 JoinBuilder.OrWhere：JOIN ON 中的 OR 值条件。
func TestSQLiteInteg_JoinOnOrWhere(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				OrWhere("orders.amount", ">", 140)
		}).
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// INNER JOIN ON id=user_id OR amount>140: 6 条匹配 id + 额外 amount>140 交叉匹配 → 14
	if len(rows) != 14 {
		t.Errorf("expected 14 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_JoinOnRaw 验证 JoinBuilder.Raw：JOIN ON 中的原始 SQL 条件。
func TestSQLiteInteg_JoinOnRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Raw("orders.amount > ?", 100)
		}).
		OrderBy("orders.id", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// users.id=orders.user_id AND orders.amount>100 → 3 (Laptop, TV, Camera)
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_Complex_TableSubJoinGroupHaving 验证 FROM子查询 + JOIN + GROUP BY + HAVING 组合。
// 预期：bob(2单,280), alice(2单,170)
func TestSQLiteInteg_Complex_TableSubJoinGroupHaving(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	sub := db.Builder().Table("orders").
		Select("user_id").
		SelectRaw("COUNT(*) AS order_count").
		SelectRaw("SUM(amount) AS total_amount").
		GroupBy("user_id").
		Having("COUNT(*)", ">=", 2)

	type row struct {
		Name       string  `db:"name"`
		OrderCount int     `db:"order_count"`
		TotalAmt   float64 `db:"total_amount"`
	}
	var rows []row
	err := db.Builder().
		Select("users.name", "t.order_count", "t.total_amount").
		TableSub(sub, "t").
		JoinOn("users", func(j *JoinBuilder) {
			j.On("t.user_id", "=", "users.id")
		}).
		OrderBy("t.total_amount", "DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "bob" || rows[0].OrderCount != 2 || rows[0].TotalAmt != 280 {
		t.Errorf("row[0]: expected bob/2/280, got %v", rows[0])
	}
	if rows[1].Name != "alice" || rows[1].OrderCount != 2 || rows[1].TotalAmt != 170 {
		t.Errorf("row[1]: expected alice/2/170, got %v", rows[1])
	}
}

// TestSQLiteInteg_JoinSub_LeftJoin 验证 LeftJoinSub：主表 LEFT JOIN 聚合派生表，
// 未匹配的基金行保留且派生表列为 NULL（扫描为零值）。
// 等价 SQL：
//
//	SELECT f.fund_code, f.name, t2.ed, t2.cnt
//	FROM funds AS f
//	  LEFT JOIN (SELECT fund_code, MAX(ed) AS ed, COUNT(*) AS cnt
//	    FROM fund_net_value GROUP BY fund_code) AS t2
//	  ON f.fund_code = t2.fund_code
//
// 预期：A/基金A/2024-03-01/3，B/基金B/2024-02-01/2，D/基金D/""/0（D 无净值记录，t2 列为 NULL）。
func TestSQLiteInteg_JoinSub_LeftJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE funds (
		fund_code TEXT PRIMARY KEY,
		name      TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO funds (fund_code, name) VALUES
		('A', '基金A'), ('B', '基金B'), ('D', '基金D')`)
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

	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed", "COUNT(*) AS cnt").
		GroupBy("fund_code")

	type row struct {
		FundCode string `db:"fund_code"`
		Name     string `db:"name"`
		Ed       string `db:"ed"`
		Cnt      int    `db:"cnt"`
	}
	var rows []row
	err := db.Builder().Table("funds AS f").
		Select("f.fund_code", "f.name", "t2.ed", "t2.cnt").
		LeftJoinSub(sub, "t2", func(j *JoinBuilder) {
			j.On("f.fund_code", "=", "t2.fund_code")
		}).
		OrderBy("f.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Name != "基金A" || rows[0].Ed != "2024-03-01" || rows[0].Cnt != 3 {
		t.Errorf("row[0]: expected A/基金A/2024-03-01/3, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Name != "基金B" || rows[1].Ed != "2024-02-01" || rows[1].Cnt != 2 {
		t.Errorf("row[1]: expected B/基金B/2024-02-01/2, got %+v", rows[1])
	}
	if rows[2].FundCode != "D" || rows[2].Name != "基金D" || rows[2].Ed != "" || rows[2].Cnt != 0 {
		t.Errorf("row[2]: expected D/基金D//0 (NULL scanned to zero value), got %+v", rows[2])
	}
}

// TestSQLiteInteg_JoinSub_RightJoin 验证 RightJoinSub：聚合派生表 RIGHT JOIN 主表，
// 右侧（funds）全保留，与 LeftJoin 用例镜像。
// 等价 SQL：
//
//	SELECT f.fund_code, t2.ed
//	FROM (SELECT fund_code, MAX(ed) AS ed
//	  FROM fund_net_value GROUP BY fund_code) AS t2
//	  RIGHT JOIN funds AS f ON t2.fund_code = f.fund_code
//
// 预期：A/2024-03-01，B/2024-02-01，D/""（D 无匹配，t2.ed 为 NULL）。
func TestSQLiteInteg_JoinSub_RightJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE funds (
		fund_code TEXT PRIMARY KEY,
		name      TEXT NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO funds (fund_code, name) VALUES
		('A', '基金A'), ('B', '基金B'), ('D', '基金D')`)
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

	sub := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		GroupBy("fund_code")

	type row struct {
		FundCode string `db:"fund_code"`
		Ed       string `db:"ed"`
	}
	var rows []row
	err := db.Builder().TableSub(sub, "t2").
		Select("f.fund_code", "t2.ed").
		RightJoinSub(db.Builder().Table("funds"), "f", func(j *JoinBuilder) {
			j.On("t2.fund_code", "=", "f.fund_code")
		}).
		OrderBy("f.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].Ed != "2024-03-01" {
		t.Errorf("row[0]: expected A/2024-03-01, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].Ed != "2024-02-01" {
		t.Errorf("row[1]: expected B/2024-02-01, got %+v", rows[1])
	}
	if rows[2].FundCode != "D" || rows[2].Ed != "" {
		t.Errorf("row[2]: expected D/\"\" (NULL scanned to zero value), got %+v", rows[2])
	}
}

// TestSQLiteInteg_JoinSub_MultiSub 验证同一查询串联两个 JoinSub（派生表）：
// 子查询绑定（WhereIn + HAVING 值）、ON 值绑定（j.Where）、主查询绑定的收集顺序与 SQL 文本一致。
// 等价 SQL：
//
//	SELECT t1.fund_code, t1.net_value, t3.cnt
//	FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN (?, ?) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  INNER JOIN (SELECT fund_code, COUNT(*) AS cnt FROM fund_net_value
//	    GROUP BY fund_code HAVING COUNT(*) >= ?) AS t3
//	  ON t1.fund_code = t3.fund_code AND t3.cnt > ?
//	WHERE t1.fund_code IN (?, ?)
//
// 绑定顺序：t2 子查询 IN → t3 子查询 HAVING → ON 值 → 主查询 WHERE。
// 预期：A/1.50/3，B/2.30/2；C 被子查询 HAVING 过滤。
func TestSQLiteInteg_JoinSub_MultiSub(t *testing.T) {
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
	t2 := db.Builder().Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		WhereIn("fund_code", codes).
		GroupBy("fund_code")
	t3 := db.Builder().Table("fund_net_value").
		Select("fund_code", "COUNT(*) AS cnt").
		GroupBy("fund_code").
		Having("COUNT(*)", ">=", 2)

	type row struct {
		FundCode string  `db:"fund_code"`
		NetValue float64 `db:"net_value"`
		Cnt      int     `db:"cnt"`
	}
	var rows []row
	err := db.Builder().Table("fund_net_value AS t1").
		Select("t1.fund_code", "t1.net_value", "t3.cnt").
		JoinSub(t2, "t2", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t2.fund_code").
				On("t1.ed", "=", "t2.ed")
		}).
		JoinSub(t3, "t3", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t3.fund_code").
				Where("t3.cnt", ">", 0)
		}).
		WhereIn("t1.fund_code", codes).
		OrderBy("t1.fund_code", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].FundCode != "A" || rows[0].NetValue != 1.50 || rows[0].Cnt != 3 {
		t.Errorf("row[0]: expected A/1.50/3, got %+v", rows[0])
	}
	if rows[1].FundCode != "B" || rows[1].NetValue != 2.30 || rows[1].Cnt != 2 {
		t.Errorf("row[1]: expected B/2.30/2, got %+v", rows[1])
	}
}

// TestSQLiteInteg_CrossJoinSub 验证 CrossJoinSub：CROSS JOIN 派生表生成「门店 × 月份」组合矩阵，
// 再 LEFT JOIN 事实表补零（无销售记录的组合 amount=0）。
// 等价 SQL：
//
//	SELECT m.month, s.store_name, COALESCE(sales.amount, 0) AS amount
//	FROM (SELECT DISTINCT month FROM sales) AS m
//	  CROSS JOIN (SELECT DISTINCT store_name FROM sales
//	    WHERE store_name IN (?, ?)) AS s
//	  LEFT JOIN sales ON sales.month = m.month AND sales.store_name = s.store_name
//
// 预期 6 行矩阵：店A/店B × 2024-01/02/03，其中 2024-03 店A、2024-02/03 店B 无销售记录补 0。
func TestSQLiteInteg_CrossJoinSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE sales (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		store_name TEXT NOT NULL,
		month      TEXT NOT NULL,
		amount     REAL NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO sales (store_name, month, amount) VALUES
		('店A', '2024-01', 100.00),
		('店A', '2024-02', 150.00),
		('店B', '2024-01', 200.00),
		('店C', '2024-01', 50.00),
		('店C', '2024-03', 300.00)`)

	codes := []any{"店A", "店B"}
	m := db.Builder().Table("sales").Select("month").Distinct()
	s := db.Builder().Table("sales").
		Select("store_name").
		Distinct().
		WhereIn("store_name", codes)

	type row struct {
		Month  string  `db:"month"`
		Store  string  `db:"store_name"`
		Amount float64 `db:"amount"`
	}
	var rows []row
	err := db.Builder().TableSub(m, "m").
		Select("m.month", "s.store_name", "COALESCE(sales.amount, 0) AS amount").
		CrossJoinSub(s, "s").
		LeftJoinOn("sales", func(j *JoinBuilder) {
			j.On("sales.month", "=", "m.month").
				On("sales.store_name", "=", "s.store_name")
		}).
		OrderBy("m.month", "ASC").
		OrderBy("s.store_name", "ASC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("expected 6 matrix rows, got %d: %v", len(rows), rows)
	}
	expected := []row{
		{Month: "2024-01", Store: "店A", Amount: 100},
		{Month: "2024-01", Store: "店B", Amount: 200},
		{Month: "2024-02", Store: "店A", Amount: 150},
		{Month: "2024-02", Store: "店B", Amount: 0},
		{Month: "2024-03", Store: "店A", Amount: 0},
		{Month: "2024-03", Store: "店B", Amount: 0},
	}
	for i, exp := range expected {
		if rows[i] != exp {
			t.Errorf("row[%d]: expected %+v, got %+v", i, exp, rows[i])
		}
	}
}

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
