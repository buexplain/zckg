// 本文件为 PostgreSQL 集成测试——Join 系列与 JoinBuilder 连接条件。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	_ "github.com/lib/pq"
	"testing"
)

// TestPgInteg_InnerJoin 验证 INNER JOIN：只返回两表都匹配的行。
func TestPgInteg_InnerJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_LeftJoin 验证 LEFT JOIN：左表所有行都保留。
func TestPgInteg_LeftJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_CrossJoin 验证 CROSS JOIN 笛卡尔积。
func TestPgInteg_CrossJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	count, err := db.Builder().Table("users").SelectRaw("COUNT(*) as cnt").CrossJoin("orders").Count(context.Background())
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if count != 30 {
		t.Errorf("expected 30 cross join rows, got %d", count)
	}
}

// TestPgInteg_JoinOnMultiple 验证 JoinOn 多 ON 条件：多个 On 调用生成 AND 连接的 ON 条件。
func TestPgInteg_JoinOnMultiple(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				On("users.name", "!=", "orders.product")
		}).
		Distinct().
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// users.name != orders.product is always true (names differ from products)
	if len(rows) != 4 {
		t.Errorf("expected 4 users, got %d: %v", len(rows), rows)
	}
}

// TestPgInteg_JoinOn 验证 JoinOn 自定义 JOIN 条件：ON 子句附加额外过滤。
func TestPgInteg_JoinOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_RightJoin 验证 RIGHT JOIN：右表所有行都保留。
func TestPgInteg_RightJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_RightJoinOn 验证 RightJoinOn 多条件：RIGHT JOIN + 回调式 ON 条件。
func TestPgInteg_RightJoinOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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
	// ON users.id=orders.user_id OR orders.amount>100 → 6 rows
	if len(rows) != 6 {
		t.Errorf("expected 6 rows, got %d", len(rows))
	}
}

// TestPgInteg_JoinOnOrWhere 验证 JoinBuilder.OrWhere：JOIN ON 中的 OR 值条件。
func TestPgInteg_JoinOnOrWhere(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_JoinOnRaw 验证 JoinBuilder.Raw：JOIN ON 中的原始 SQL 条件。
func TestPgInteg_JoinOnRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name    string `db:"name"`
		Product string `db:"product"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("users.name", "orders.product").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id").
				Raw("orders.amount > $1", 100)
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

// TestPgInteg_Complex_TableSubJoinGroupHaving 验证 FROM子查询 + JOIN + GROUP BY + HAVING 组合。
// 预期：bob(2单,280), alice(2单,170)
func TestPgInteg_Complex_TableSubJoinGroupHaving(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_LeftJoinOnOrOn 验证 LeftJoinOn + OrOn：LEFT JOIN 带 OR 条件的 ON 子句。
func TestPgInteg_LeftJoinOnOrOn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)

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

// TestPgInteg_JoinSub_LeftJoin 验证 LeftJoinSub：主表 LEFT JOIN 聚合派生表，
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
func TestPgInteg_JoinSub_LeftJoin(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS funds")
	mustExec(t, db, "DROP TABLE IF EXISTS fund_net_value")
	mustExec(t, db, `CREATE TABLE funds (
		fund_code VARCHAR(20) PRIMARY KEY,
		name      VARCHAR(32) NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO funds (fund_code, name) VALUES
		('A', '基金A'), ('B', '基金B'), ('D', '基金D')`)
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

// TestPgInteg_JoinSub_RightJoin 验证 RightJoinSub：聚合派生表 RIGHT JOIN 主表，
// 右侧（funds）全保留，与 LeftJoin 用例镜像。
// 等价 SQL：
//
//	SELECT f.fund_code, t2.ed
//	FROM (SELECT fund_code, MAX(ed) AS ed
//	  FROM fund_net_value GROUP BY fund_code) AS t2
//	  RIGHT JOIN funds AS f ON t2.fund_code = f.fund_code
//
// 预期：A/2024-03-01，B/2024-02-01，D/""（D 无匹配，t2.ed 为 NULL）。
func TestPgInteg_JoinSub_RightJoin(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS funds")
	mustExec(t, db, "DROP TABLE IF EXISTS fund_net_value")
	mustExec(t, db, `CREATE TABLE funds (
		fund_code VARCHAR(20) PRIMARY KEY,
		name      VARCHAR(32) NOT NULL
	)`)
	mustExec(t, db, `INSERT INTO funds (fund_code, name) VALUES
		('A', '基金A'), ('B', '基金B'), ('D', '基金D')`)
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

// TestPgInteg_JoinSub_MultiSub 验证同一查询串联两个 JoinSub（派生表）：
// 子查询绑定（WhereIn + HAVING 值）、ON 值绑定（j.Where）、主查询绑定的收集顺序与 SQL 文本一致。
// PG 占位符编号：t2 子查询 IN $1,$2 → t3 子查询 HAVING $3 → ON 值 $4 → 主查询 WHERE $5,$6。
// 等价 SQL：
//
//	SELECT t1.fund_code, t1.net_value, t3.cnt
//	FROM fund_net_value AS t1
//	  INNER JOIN (SELECT fund_code, MAX(ed) AS ed FROM fund_net_value
//	    WHERE fund_code IN ($1, $2) GROUP BY fund_code) AS t2
//	  ON t1.fund_code = t2.fund_code AND t1.ed = t2.ed
//	  INNER JOIN (SELECT fund_code, COUNT(*) AS cnt FROM fund_net_value
//	    GROUP BY fund_code HAVING COUNT(*) >= $3) AS t3
//	  ON t1.fund_code = t3.fund_code AND t3.cnt > $4
//	WHERE t1.fund_code IN ($5, $6)
//
// 预期：A/1.50/3，B/2.30/2；C 被子查询 HAVING 过滤。
func TestPgInteg_JoinSub_MultiSub(t *testing.T) {
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

// TestPgInteg_CrossJoinSub 验证 CrossJoinSub：CROSS JOIN 派生表生成「门店 × 月份」组合矩阵，
// 再 LEFT JOIN 事实表补零（无销售记录的组合 amount=0）。
// 等价 SQL：
//
//	SELECT m.month, s.store_name, COALESCE(sales.amount, 0) AS amount
//	FROM (SELECT DISTINCT month FROM sales) AS m
//	  CROSS JOIN (SELECT DISTINCT store_name FROM sales
//	    WHERE store_name IN ($1, $2)) AS s
//	  LEFT JOIN sales ON sales.month = m.month AND sales.store_name = s.store_name
//
// 预期 6 行矩阵：店A/店B × 2024-01/02/03，其中 2024-03 店A、2024-02/03 店B 无销售记录补 0。
func TestPgInteg_CrossJoinSub(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS sales")
	mustExec(t, db, `CREATE TABLE sales (
		id         SERIAL PRIMARY KEY,
		store_name VARCHAR(20) NOT NULL,
		month      VARCHAR(7) NOT NULL,
		amount     DOUBLE PRECISION NOT NULL
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

// TestPgInteg_Bug_JoinWhereNullExpansion 锁定 M2 修复：
// JoinBuilder.Where/OrWhere 遇 nil 值时 = 展开为 IS NULL、!=/<>
// 展开为 IS NOT NULL（与 Builder.Where 语义对齐）——
// 修复前编译为 "col = $N" 绑定 nil，条件恒 UNKNOWN，INNER JOIN 静默丢行。
func TestPgInteg_Bug_JoinWhereNullExpansion(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgProfilesTable(t, db)
	ctx := context.Background()

	// diana(user_id=4) 的 profile bio 为 NULL
	mustExec(t, db, `INSERT INTO profiles (user_id, bio, active) VALUES (4, NULL, 99)`)

	// = nil → IS NULL：命中 diana（修复前恒 UNKNOWN → 0 行）
	var names []string
	err := db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").Where("profiles.bio", "=", nil)
		}).Pluck(ctx, &names, "name")
	assertNoError(t, err)
	if len(names) != 1 || names[0] != "diana" {
		t.Errorf("join where = nil: expected [diana], got %v", names)
	}

	// != nil → IS NOT NULL：命中 alice/bob/charlie（修复前恒 UNKNOWN → 0 行）
	names = nil
	err = db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").Where("profiles.bio", "!=", nil)
		}).OrderBy("users.id", "ASC").Pluck(ctx, &names, "name")
	assertNoError(t, err)
	if len(names) != 3 || names[0] != "alice" || names[1] != "bob" || names[2] != "charlie" {
		t.Errorf("join where != nil: expected [alice bob charlie], got %v", names)
	}

	// OrWhere nil 变体：ON a AND (bio IS NULL OR active = 100)
	names = nil
	err = db.Builder().Table("users").
		JoinOn("profiles", func(j *JoinBuilder) {
			j.On("profiles.user_id", "=", "users.id").
				WhereNested(func(q *JoinBuilder) {
					q.Where("profiles.bio", "=", nil).OrWhere("profiles.active", "=", 100)
				})
		}).Pluck(ctx, &names, "name")
	assertNoError(t, err)
	if len(names) != 1 || names[0] != "diana" {
		t.Errorf("join nested orWhere nil: expected [diana], got %v", names)
	}
}
