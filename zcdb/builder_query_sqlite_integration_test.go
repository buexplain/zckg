// 本文件为 SQLite 集成测试——查询执行（First/Find/Paginate/Count/聚合/Value 等）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	"database/sql"
	"errors"
	_ "modernc.org/sqlite"
	"strings"
	"testing"
	"time"
)

// TestSQLiteInteg_First 验证 First 查询第一条记录：有数据时填充结构体并返回 nil。
func TestSQLiteInteg_First(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var r row
	err := db.Builder().Table("users").Select("name", "age").OrderBy("id", "ASC").First(context.Background(), &r)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if r.Name != "alice" || r.Age != 25 {
		t.Errorf("expected alice/25, got %s/%d", r.Name, r.Age)
	}
}

// TestSQLiteInteg_FirstNotFound 验证 First 无数据时返回 sql.ErrNoRows。
func TestSQLiteInteg_FirstNotFound(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).First(context.Background(), &r)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestSQLiteInteg_FirstLimit 验证 First 自动限制为 1 条：即使有多行匹配也只返回第一条。
func TestSQLiteInteg_FirstLimit(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("status", "=", "active").OrderBy("id", "ASC").First(context.Background(), &r)
	if err != nil {
		t.Fatalf("First error: %v", err)
	}
	if r.Name != "alice" {
		t.Errorf("expected alice, got %s", r.Name)
	}
}

// TestSQLiteInteg_Exists 验证 Exists 有数据时返回 true。
func TestSQLiteInteg_Exists(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}
}

// TestSQLiteInteg_ExistsFalse 验证 Exists 无匹配数据时返回 false。
func TestSQLiteInteg_ExistsFalse(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("id", "=", 999).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false, got true")
	}
}

// TestSQLiteInteg_ExistsUsesLimit1 验证 Exists 走 SELECT 1 ... LIMIT 1 而非 COUNT(*)：
// 通过 onSQL 回调捕获实际执行 SQL 断言。
func TestSQLiteInteg_ExistsUsesLimit1(t *testing.T) {
	pool, err := NewPool(PoolConfig{DriverName: "sqlite", DSN: ":memory:"})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	var captured []string
	dao, err := NewDBDao(pool, "sqlite", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		captured = append(captured, sqlStr)
	})
	if err != nil {
		t.Fatalf("failed to create dao: %v", err)
	}
	defer dao.Close()

	setupSQLiteUsersTable(t, dao)

	exists, err := dao.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}

	// 捕获的 SQL 必须是 LIMIT 1 形式，不能是 COUNT(*)
	var sqlStr string
	for _, s := range captured {
		if strings.Contains(s, "SELECT 1") {
			sqlStr = s
			break
		}
	}
	if sqlStr == "" {
		t.Fatalf("no EXISTS SQL captured, got: %v", captured)
	}
	if strings.Contains(sqlStr, "COUNT(*)") {
		t.Errorf("Exists should not use COUNT(*), got: %s", sqlStr)
	}
	if !strings.Contains(sqlStr, "LIMIT 1") {
		t.Errorf("Exists should use SELECT 1 ... LIMIT 1, got: %s", sqlStr)
	}
}

// TestSQLiteInteg_Paginate 验证 Paginate 分页查询：第二页返回正确数据。
func TestSQLiteInteg_Paginate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	b := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(2, 2)
	total, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if len(rows) >= 2 && (rows[0].Name != "charlie" || rows[1].Name != "diana") {
		t.Errorf("expected [charlie, diana], got %v", rows)
	}
}

// TestSQLiteInteg_PaginateDefault 验证 Paginate 未设置分页参数时使用默认值（第 1 页，每页 20 条）。
func TestSQLiteInteg_PaginateDefault(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	b := db.Builder().Table("users").Select("name").OrderBy("id", "ASC")
	total, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	// 默认 ForPage(1, 20)，5 条数据全部返回
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

// TestSQLiteInteg_FirstInvalidDest 验证 First 传入非指针类型时返回错误。
func TestSQLiteInteg_FirstInvalidDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").First(context.Background(), r)
	if err == nil {
		t.Fatalf("expected error for non-pointer dest, got nil")
	}
}

// TestSQLiteInteg_FirstNilDest 验证 First 传入 nil 时返回错误。
func TestSQLiteInteg_FirstNilDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	err := db.Builder().Table("users").Select("name").First(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil dest, got nil")
	}
}

// TestSQLiteInteg_FirstIntPtrDest 验证 First 传入非结构体指针（*int）时返回错误。
func TestSQLiteInteg_FirstIntPtrDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").First(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest, got nil")
	}
}

// TestSQLiteInteg_FindInvalidDest 验证 Find 传入 *int（非结构体切片指针）时返回错误。
func TestSQLiteInteg_FindInvalidDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Find, got nil")
	}
}

// TestSQLiteInteg_FindNonPointerDest 验证 Find 传入非指针（[]struct）时返回错误。
func TestSQLiteInteg_FindNonPointerDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Find(context.Background(), rows)
	if err == nil {
		t.Fatalf("expected error for non-pointer slice dest, got nil")
	}
}

// TestSQLiteInteg_FindIntPtrDest 验证 Find 传入 *[]int（非结构体切片指针）时返回错误。
func TestSQLiteInteg_FindIntPtrDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Fatalf("expected error for *[]int dest in Find, got nil")
	}
}

// TestSQLiteInteg_PaginateInvalidDest 验证 Paginate 传入 *int（非结构体切片指针）时返回错误。
func TestSQLiteInteg_PaginateInvalidDest(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var n int
	_, err := db.Builder().Table("users").Select("name").Paginate(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Paginate, got nil")
	}
}

// TestSQLiteInteg_ValueNoRows 验证 Value 无匹配数据时返回 sql.ErrNoRows。
func TestSQLiteInteg_ValueNoRows(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).Value(context.Background(), &name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestSQLiteInteg_Bug_CountWithUnion 验证 Count() 对 UNION 查询返回正确结果。
// 数据：active 用户 3 人，age>25 用户 3 人 (eve age 为 NULL 不计入)。
// UNION ALL 不去重，正确总数应为 6。修复前生成无效 SQL 报错。
func TestSQLiteInteg_Bug_CountWithUnion(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	union := db.Builder().Table("users").Where("age", ">", 25)
	b := db.Builder().Table("users").Where("status", "=", "active").UnionAll(union)

	count, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	// 正确结果应为 6（3 active + 3 age>25，UNION ALL 不去重）
	if count != 6 {
		t.Errorf("Count with UNION ALL expected 6, got %d", count)
	}
}

// TestSQLiteInteg_ExistsWithGroupBy 验证 GROUP BY + HAVING 下的 Exists 真实执行：
// Exists 基于 Count（分组数量 > 0）判断，语义为“是否存在满足条件的分组”。
func TestSQLiteInteg_ExistsWithGroupBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteOrdersTable(t, db)

	// 3 组满足 SUM(amount) > 100 → true
	exists, err := db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 100).
		Exists(context.Background())
	assertNoError(t, err)
	if !exists {
		t.Error("Exists with GROUP BY + HAVING: expected true, got false")
	}

	// 无任何组满足 → false
	exists, err = db.Builder().
		Table("orders").
		GroupBy("user_id").
		Having("SUM(amount)", ">", 99999).
		Exists(context.Background())
	assertNoError(t, err)
	if exists {
		t.Error("Exists with GROUP BY + HAVING (no match): expected false, got true")
	}
}

// TestSQLiteInteg_CountWithDistinct 验证 Distinct + Count 去重计数真实执行。
func TestSQLiteInteg_CountWithDistinct(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE tags (name TEXT NOT NULL)`)
	mustExec(t, db, `INSERT INTO tags (name) VALUES ('a'), ('a'), ('b'), ('c')`)

	// 4 行 3 个去重值；修复前生成 SELECT DISTINCT COUNT(*) 会错误返回 4
	count, err := db.Builder().
		Table("tags").
		Select("name").
		Distinct().
		Count(context.Background())
	assertNoError(t, err)
	if count != 3 {
		t.Fatalf("Distinct Count: expected 3 distinct values, got %d", count)
	}
}

// TestSQLiteInteg_Pluck 验证 Pluck：切片目标提取单列值列表，map 目标提取「值=>键」映射，
// NULL 值扫描为零值（与 Find 一致），查询链（WHERE/ORDER BY）完整生效。
func TestSQLiteInteg_Pluck(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE users (
		id   INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT
	)`)
	mustExec(t, db, `INSERT INTO users (name) VALUES
		('John'),
		('Jane'),
		(NULL),
		('Bob')`)

	// 切片模式：单列值列表（含 NULL 行扫描为零值 ""）
	var names []string
	err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &names, "name")
	if err != nil {
		t.Fatalf("pluck slice error: %v", err)
	}
	expected := []string{"John", "Jane", "", "Bob"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}
	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}

	// map 模式：值=>键 映射（第一列为值、第二列为键，nil map 自动初始化）
	var m map[int64]string
	err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &m, "name", "id")
	if err != nil {
		t.Fatalf("pluck map error: %v", err)
	}
	expectMap := map[int64]string{1: "John", 2: "Jane", 3: "", 4: "Bob"}
	if len(m) != len(expectMap) {
		t.Fatalf("expected %d entries, got %d: %v", len(expectMap), len(m), m)
	}
	for id, exp := range expectMap {
		if m[id] != exp {
			t.Errorf("m[%d]: expected %q, got %q", id, exp, m[id])
		}
	}

	// 查询链生效：WHERE 过滤 NULL 行，ORDER BY DESC 倒序
	names = nil
	err = db.Builder().Table("users").
		Where("name", "!=", "").
		OrderBy("id", "DESC").
		Pluck(context.Background(), &names, "name")
	if err != nil {
		t.Fatalf("pluck filtered error: %v", err)
	}
	expectedFiltered := []string{"Bob", "Jane", "John"}
	if len(names) != len(expectedFiltered) {
		t.Fatalf("expected %d names, got %d: %v", len(expectedFiltered), len(names), names)
	}
	for i, exp := range expectedFiltered {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}
}

// TestSQLiteInteg_PluckKeyBy 验证 Pluck 键列模式（keyBy）：map 值为结构体/结构体指针时，
// 唯一列参数作为键列，整行数据按 db tag 扫描进结构体（NULL 扫零值，与 Find 一致）。
func TestSQLiteInteg_PluckKeyBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	mustExec(t, db, `CREATE TABLE users (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		name     TEXT,
		nickname TEXT
	)`)
	mustExec(t, db, `INSERT INTO users (name, nickname) VALUES
		('John', 'JJ'),
		('Jane', NULL),
		(NULL, 'NN'),
		('Bob', 'BB')`)

	// 场景 A：map 值为结构体，键列在结构体字段中（id 字段同时填充并作键）
	type User struct {
		Id       int
		Name     string
		Nickname string
	}
	var m map[int64]User
	err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &m, "id")
	if err != nil {
		t.Fatalf("pluck keyBy struct error: %v", err)
	}
	if len(m) != 4 {
		t.Fatalf("expected 4 entries, got %d: %v", len(m), m)
	}
	expected := map[int64]User{
		1: {Id: 1, Name: "John", Nickname: "JJ"},
		2: {Id: 2, Name: "Jane", Nickname: ""},
		3: {Id: 3, Name: "", Nickname: "NN"},
		4: {Id: 4, Name: "Bob", Nickname: "BB"},
	}
	for id, exp := range expected {
		if m[id] != exp {
			t.Errorf("m[%d]: expected %+v, got %+v", id, exp, m[id])
		}
	}

	// 场景 B：map 值为结构体指针，每行新建实例
	var mp map[int64]*User
	err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &mp, "id")
	if err != nil {
		t.Fatalf("pluck keyBy ptr error: %v", err)
	}
	if len(mp) != 4 {
		t.Fatalf("expected 4 ptr entries, got %d", len(mp))
	}
	for id, exp := range expected {
		if mp[id] == nil {
			t.Errorf("mp[%d]: expected non-nil pointer", id)
			continue
		}
		if *mp[id] != exp {
			t.Errorf("mp[%d]: expected %+v, got %+v", id, exp, *mp[id])
		}
	}

	// 场景 C：键列不在结构体字段中，SELECT 自动追加键列
	type userBrief struct {
		Name     string
		Nickname string
	}
	var kb map[int64]userBrief
	err = db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &kb, "id")
	if err != nil {
		t.Fatalf("pluck keyBy external key error: %v", err)
	}
	if len(kb) != 4 {
		t.Fatalf("expected 4 brief entries, got %d: %v", len(kb), kb)
	}
	if kb[1] != (userBrief{Name: "John", Nickname: "JJ"}) || kb[3].Name != "" || kb[3].Nickname != "NN" {
		t.Errorf("kb content mismatch: %+v", kb)
	}
}

// TestSQLiteInteg_AggregateResetColumns
// 聚合后 columns 状态恢复，可继续取数。
func TestSQLiteInteg_AggregateResetColumns(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	b := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	// 先聚合：COUNT 后 columns 恢复
	count, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 3 {
		t.Errorf("expected 3, got %d", count)
	}
	// 聚合后再次取数：列与绑定状态未被破坏
	var rows []row
	err = b.OrderBy("id", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "alice" || rows[2].Name != "diana" {
		t.Errorf("expected [alice bob diana], got %v", rows)
	}
}

// TestSQLiteInteg_AggregateIgnoreSelectSub
// 聚合忽略子查询列及其绑定（COUNT(*) 不受 SELECT 子查询影响）。
func TestSQLiteInteg_AggregateIgnoreSelectSub(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	count, err := db.Builder().Table("users").
		SelectSub(db.Builder().Table("users").Select("name").Where("id", "=", 1), "sub_name").
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected 5, got %d", count)
	}
}

// TestSQLiteInteg_PluckDuplicateKeyOverwrite 集成附录 testPluck（重复 key 覆盖部分）：
// Pluck map 模式重复键时后值覆盖前值；keyBy 模式重复键列时最后一行覆盖。
func TestSQLiteInteg_PluckDuplicateKeyOverwrite(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// map 值→键 模式：第一列为值、第二列为键；status 键重复时后者（id 更大）覆盖前者
	var m map[string]int64
	err := db.Builder().Table("users").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &m, "id", "status")
	if err != nil {
		t.Fatalf("pluck error: %v", err)
	}
	if m["active"] != 4 || m["inactive"] != 5 {
		t.Errorf("expected active=4 inactive=5 (last wins), got %v", m)
	}

	// keyBy 模式：插入两条同名记录，后者（id 更大）覆盖前者
	mustExec(t, db, `INSERT INTO users (name, age, email, status) VALUES
		('dup', 1, 'dup1@test.com', 'x'),
		('dup', 2, 'dup2@test.com', 'y')`)
	type userBrief struct {
		Id  int
		Age int
	}
	var dup map[string]userBrief
	err = db.Builder().Table("users").
		Where("name", "=", "dup").
		OrderBy("id", "ASC").
		Pluck(context.Background(), &dup, "name")
	if err != nil {
		t.Fatalf("pluck keyBy dup error: %v", err)
	}
	if dup["dup"].Id != 7 || dup["dup"].Age != 2 {
		t.Errorf("expected last dup row (id=7, age=2), got %+v", dup["dup"])
	}
}

// TestSQLiteInteg_NewApi_Aggregate 验证 Max/Min/Sum/Avg 真实执行与空表语义。
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
