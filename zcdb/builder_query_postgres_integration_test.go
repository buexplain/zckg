// 本文件为 PostgreSQL 集成测试——查询执行（First/Find/Paginate/Count/聚合/Value 等）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	"database/sql"
	"errors"
	_ "github.com/lib/pq"
	"testing"
)

// TestPgInteg_First 验证 First 查询第一条记录：有数据时填充结构体并返回 nil。
func TestPgInteg_First(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_FirstNotFound 验证 First 无数据时返回 sql.ErrNoRows。
func TestPgInteg_FirstNotFound(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).First(context.Background(), &r)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestPgInteg_FirstLimit 验证 First 自动限制为 1 条：即使有多行匹配也只返回第一条。
func TestPgInteg_FirstLimit(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Exists 验证 Exists 有数据时返回 true。
func TestPgInteg_Exists(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}
}

// TestPgInteg_ExistsFalse 验证 Exists 无匹配数据时返回 false。
func TestPgInteg_ExistsFalse(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("id", "=", 999).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false, got true")
	}
}

// TestPgInteg_Paginate 验证 Paginate 分页查询：第二页返回正确数据。
func TestPgInteg_Paginate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_PaginateDefault 验证 Paginate 未设置分页参数时使用默认值（第 1 页，每页 20 条）。
func TestPgInteg_PaginateDefault(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_FirstInvalidDest 验证 First 传入非指针类型时返回错误。
func TestPgInteg_FirstInvalidDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").First(context.Background(), r)
	if err == nil {
		t.Fatalf("expected error for non-pointer dest, got nil")
	}
}

// TestPgInteg_FirstNilDest 验证 First 传入 nil 时返回错误。
func TestPgInteg_FirstNilDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Builder().Table("users").Select("name").First(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil dest, got nil")
	}
}

// TestPgInteg_FirstIntPtrDest 验证 First 传入非结构体指针（*int）时返回错误。
func TestPgInteg_FirstIntPtrDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").First(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest, got nil")
	}
}

// TestPgInteg_FindInvalidDest 验证 Find 传入 *int（非结构体切片指针）时返回错误。
func TestPgInteg_FindInvalidDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Find, got nil")
	}
}

// TestPgInteg_FindNonPointerDest 验证 Find 传入非指针（[]struct）时返回错误。
func TestPgInteg_FindNonPointerDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Find(context.Background(), rows)
	if err == nil {
		t.Fatalf("expected error for non-pointer slice dest, got nil")
	}
}

// TestPgInteg_FindIntPtrDest 验证 Find 传入 *[]int（非结构体切片指针）时返回错误。
func TestPgInteg_FindIntPtrDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Fatalf("expected error for *[]int dest in Find, got nil")
	}
}

// TestPgInteg_PaginateInvalidDest 验证 Paginate 传入 *int（非结构体切片指针）时返回错误。
func TestPgInteg_PaginateInvalidDest(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var n int
	_, err := db.Builder().Table("users").Select("name").Paginate(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Paginate, got nil")
	}
}

// TestPgInteg_ValueNoRows 验证 Value 无匹配数据时返回 sql.ErrNoRows。
func TestPgInteg_ValueNoRows(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).Value(context.Background(), &name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestPgInteg_Bug_CountWithUnion 验证 Count() 对 UNION 查询返回正确结果。
// 数据：active 用户 3 人，age>25 用户 3 人 (eve age 为 NULL 不计入)。
// UNION ALL 不去重，正确总数应为 6。修复前生成无效 SQL 报错。
func TestPgInteg_Bug_CountWithUnion(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	union := db.Builder().Table("users").Where("age", ">", 25)
	b := db.Builder().Table("users").Where("status", "=", "active").UnionAll(union)

	count, err := b.Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 6 {
		t.Errorf("Count with UNION ALL expected 6, got %d", count)
	}
}

// TestPgInteg_CountWithDistinct 验证 Distinct + Count 去重计数真实执行。
func TestPgInteg_CountWithDistinct(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS tags")
	mustExec(t, db, `CREATE TABLE tags (name VARCHAR(16) NOT NULL)`)
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

// TestPgInteg_Pluck 验证 Pluck：切片目标提取单列值列表，map 目标提取「值=>键」映射，
// NULL 值扫描为零值（与 Find 一致），查询链（WHERE/ORDER BY）完整生效。
func TestPgInteg_Pluck(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS users")
	mustExec(t, db, `CREATE TABLE users (
		id   SERIAL PRIMARY KEY,
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

// TestPgInteg_PluckKeyBy 验证 Pluck 键列模式（keyBy）：map 值为结构体/结构体指针时，
// 唯一列参数作为键列，整行数据按 db tag 扫描进结构体（NULL 扫零值，与 Find 一致）。
func TestPgInteg_PluckKeyBy(t *testing.T) {
	db := openPgTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS users")
	mustExec(t, db, `CREATE TABLE users (
		id       SERIAL PRIMARY KEY,
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

// TestPgInteg_AggregateResetColumns
// 聚合后 columns 状态恢复，可继续取数。
func TestPgInteg_AggregateResetColumns(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_AggregateIgnoreSelectSub
// 聚合忽略子查询列及其绑定（COUNT(*) 不受 SELECT 子查询影响）。
func TestPgInteg_AggregateIgnoreSelectSub(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_PluckDuplicateKeyOverwrite 集成附录 testPluck（重复 key 覆盖部分）：
// Pluck map 模式重复键时后值覆盖前值；keyBy 模式重复键列时最后一行覆盖。
func TestPgInteg_PluckDuplicateKeyOverwrite(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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
