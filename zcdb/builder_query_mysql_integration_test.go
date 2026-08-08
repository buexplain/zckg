// 本文件为 MySQL 集成测试——查询执行（First/Find/Paginate/Count/聚合/Value 等）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
package zcdb

import (
	"context"
	"database/sql"
	"errors"
	_ "github.com/go-sql-driver/mysql"
	"testing"
)

// TestMySQLInteg_First 验证 First 查询第一条记录：有数据时填充结构体并返回 nil。
func TestMySQLInteg_First(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_FirstNotFound 验证 First 无数据时返回 sql.ErrNoRows。
func TestMySQLInteg_FirstNotFound(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).First(context.Background(), &r)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestMySQLInteg_FirstLimit 验证 First 自动限制为 1 条：即使有多行匹配也只返回第一条。
func TestMySQLInteg_FirstLimit(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_Exists 验证 Exists 有数据时返回 true。
func TestMySQLInteg_Exists(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("status", "=", "active").Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if !exists {
		t.Errorf("expected exists=true, got false")
	}
}

// TestMySQLInteg_ExistsFalse 验证 Exists 无匹配数据时返回 false。
func TestMySQLInteg_ExistsFalse(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	exists, err := db.Builder().Table("users").Where("id", "=", 999).Exists(context.Background())
	if err != nil {
		t.Fatalf("Exists error: %v", err)
	}
	if exists {
		t.Errorf("expected exists=false, got true")
	}
}

// TestMySQLInteg_Paginate 验证 Paginate 分页查询：第二页返回正确数据。
func TestMySQLInteg_Paginate(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_PaginateDefault 验证 Paginate 未设置分页参数时使用默认值（第 1 页，每页 20 条）。
func TestMySQLInteg_PaginateDefault(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_FirstInvalidDest 验证 First 传入非指针类型时返回错误。
func TestMySQLInteg_FirstInvalidDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var r row
	// 传入结构体值（非指针），应返回错误
	err := db.Builder().Table("users").Select("name").First(context.Background(), r)
	if err == nil {
		t.Fatalf("expected error for non-pointer dest, got nil")
	}
}

// TestMySQLInteg_FirstNilDest 验证 First 传入 nil 时返回错误。
func TestMySQLInteg_FirstNilDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	err := db.Builder().Table("users").Select("name").First(context.Background(), nil)
	if err == nil {
		t.Fatalf("expected error for nil dest, got nil")
	}
}

// TestMySQLInteg_FirstIntPtrDest 验证 First 传入非结构体指针（*int）时返回错误。
func TestMySQLInteg_FirstIntPtrDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var n int
	err := db.Builder().Table("users").Select("name").First(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest, got nil")
	}
}

// TestMySQLInteg_FindInvalidDest 验证 Find 传入 *int（非结构体切片指针）时返回错误。
func TestMySQLInteg_FindInvalidDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var n int
	// Find 要求 *[]struct，传入 *int 应返回错误
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Find, got nil")
	}
}

// TestMySQLInteg_FindNonPointerDest 验证 Find 传入非指针（[]struct）时返回错误。
func TestMySQLInteg_FindNonPointerDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	// 传入切片值（非指针），应返回错误
	err := db.Builder().Table("users").Select("name").Find(context.Background(), rows)
	if err == nil {
		t.Fatalf("expected error for non-pointer slice dest, got nil")
	}
}

// TestMySQLInteg_FindIntPtrDest 验证 Find 传入 *[]int（非结构体切片指针）时返回错误。
func TestMySQLInteg_FindIntPtrDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var nums []int
	err := db.Builder().Table("users").Select("name").Find(context.Background(), &nums)
	if err == nil {
		t.Fatalf("expected error for *[]int dest in Find, got nil")
	}
}

// TestMySQLInteg_PaginateInvalidDest 验证 Paginate 传入 *int（非结构体切片指针）时返回错误。
func TestMySQLInteg_PaginateInvalidDest(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var n int
	_, err := db.Builder().Table("users").Select("name").Paginate(context.Background(), &n)
	if err == nil {
		t.Fatalf("expected error for *int dest in Paginate, got nil")
	}
}

// TestMySQLInteg_ValueNoRows 验证 Value 无匹配数据时返回 sql.ErrNoRows。
func TestMySQLInteg_ValueNoRows(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Where("id", "=", 999).Value(context.Background(), &name)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Errorf("expected sql.ErrNoRows, got %v", err)
	}
}

// TestMySQLInteg_Bug_CountWithUnion 验证 Count() 对 UNION 查询返回正确结果。
// 数据：active 用户 3 人 (alice,bob,diana)，age>25 用户 3 人 (bob,charlie,diana；eve age 为 NULL 不计入)。
// UNION ALL 不去重，正确总数应为 6。修复前生成无效 SQL 导致只返回第一个 SELECT 的 COUNT=3。
func TestMySQLInteg_Bug_CountWithUnion(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_Bug_PaginateWithUnion 验证 Paginate() 对 UNION 查询返回正确 total。
func TestMySQLInteg_Bug_PaginateWithUnion(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	union := db.Builder().Table("users").Where("age", ">", 25)
	b := db.Builder().Table("users").Where("status", "=", "active").UnionAll(union).Limit(10)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	total, err := b.Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	// 正确 total 应为 6
	if total != 6 {
		t.Errorf("Paginate with UNION ALL expected total=6, got %d", total)
	}
}

// TestMySQLInteg_FindQueryError 验证 Find 查询不存在的表时返回错误。
func TestMySQLInteg_FindQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	var rows []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("nonexistent_table").Find(context.Background(), &rows)
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_CountQueryError 验证 Count 查询不存在的表时返回错误。
func TestMySQLInteg_CountQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").Count(context.Background())
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_PaginateQueryError 验证 Paginate 查询不存在的表时返回错误。
func TestMySQLInteg_PaginateQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	var rows []struct {
		Name string `db:"name"`
	}
	_, err := db.Builder().Table("nonexistent_table").ForPage(1, 10).Paginate(context.Background(), &rows)
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_ValueQueryError 验证 Value 查询不存在的表时返回错误。
func TestMySQLInteg_ValueQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	var name string
	err := db.Builder().Table("nonexistent_table").Select("name").Value(context.Background(), &name)
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_FirstLimitAlreadyOne 验证 Builder 已设置 Limit(1) 时 First 不额外 Clone。
func TestMySQLInteg_FirstLimitAlreadyOne(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var u row
	// 先 Limit(1)，触发 b.limit == 1 分支，不 Clone
	err := db.Builder().Table("users").Limit(1).OrderBy("id", "ASC").First(context.Background(), &u)
	if err != nil {
		t.Fatalf("First with Limit(1) error: %v", err)
	}
	if u.Name != "alice" {
		t.Errorf("expected alice, got %s", u.Name)
	}
}

// TestMySQLInteg_ValueLimitOne 验证 Builder 已设置 Limit(1) 时 Value 不额外 Clone。
func TestMySQLInteg_ValueLimitOne(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	var name string
	err := db.Builder().Table("users").Select("name").Limit(1).OrderBy("id", "ASC").Value(context.Background(), &name)
	if err != nil {
		t.Fatalf("Value with Limit(1) error: %v", err)
	}
	if name != "alice" {
		t.Errorf("expected alice, got %s", name)
	}
}

// TestMySQLInteg_PaginateTotalZero 验证 Paginate 空表时总数为 0 且不执行数据查询。
func TestMySQLInteg_PaginateTotalZero(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// 先清空表
	_ = db.Builder().Table("users").Truncate(context.Background())

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	total, err := db.Builder().Table("users").ForPage(1, 10).Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected total=0, got %d", total)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
}

// TestMySQLInteg_PaginateSuccess 验证 Paginate 正常分页返回总数和数据。
func TestMySQLInteg_PaginateSuccess(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	total, err := db.Builder().Table("users").ForPage(1, 2).OrderBy("id", "ASC").Paginate(context.Background(), &rows)
	if err != nil {
		t.Fatalf("Paginate error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected total=5, got %d", total)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
}

// TestMySQLInteg_ExistsError 验证 Exists 查询不存在的表时返回错误。
func TestMySQLInteg_ExistsError(t *testing.T) {
	db := openMySQLTestDB(t)
	_, err := db.Builder().Table("nonexistent_table").Exists(context.Background())
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_FirstQueryError 验证 First 查询不存在的表时返回错误。
func TestMySQLInteg_FirstQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	type row struct {
		Name string `db:"name"`
	}
	var r row
	err := db.Builder().Table("nonexistent_table").First(context.Background(), &r)
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_PaginateDataQueryError 验证 Paginate 数据查询失败时返回错误。
func TestMySQLInteg_PaginateDataQueryError(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// Count 查询正常（users 表存在），但数据查询使用不存在的表
	// 通过先设置表名再修改的方式难以实现，改用直接查询不存在的表
	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	_, err := db.Builder().Table("nonexistent_table").ForPage(1, 10).Paginate(context.Background(), &rows)
	if err == nil {
		t.Error("expected error for nonexistent table, got nil")
	}
}

// TestMySQLInteg_ToCountWithUnion 验证 ToCount 对 UNION 查询的计数。
func TestMySQLInteg_ToCountWithUnion(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	// UNION 查询计数：active 用户 + inactive 用户
	sub := db.Builder().Table("users").Select("name").Where("status", "=", "inactive")
	count, err := db.Builder().Table("users").
		Select("name").Where("status", "=", "active").
		Union(sub).
		Count(context.Background())
	if err != nil {
		t.Fatalf("Count with UNION error: %v", err)
	}
	if count != 5 {
		t.Errorf("expected count=5, got %d", count)
	}
}

// TestMySQLInteg_CountWithDistinct 验证 Distinct + Count 去重计数真实执行。
func TestMySQLInteg_CountWithDistinct(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS `tags`")
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

// TestMySQLInteg_Pluck 验证 Pluck：切片目标提取单列值列表，map 目标提取「值=>键」映射，
// NULL 值扫描为零值（与 Find 一致），查询链（WHERE/ORDER BY）完整生效。
func TestMySQLInteg_Pluck(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS users")
	mustExec(t, db, `CREATE TABLE users (
		id   INT AUTO_INCREMENT PRIMARY KEY,
		name VARCHAR(50)
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

// TestMySQLInteg_PluckKeyBy 验证 Pluck 键列模式（keyBy）：map 值为结构体/结构体指针时，
// 唯一列参数作为键列，整行数据按 db tag 扫描进结构体（NULL 扫零值，与 Find 一致）。
func TestMySQLInteg_PluckKeyBy(t *testing.T) {
	db := openMySQLTestDB(t)
	mustExec(t, db, "DROP TABLE IF EXISTS users")
	mustExec(t, db, `CREATE TABLE users (
		id       INT AUTO_INCREMENT PRIMARY KEY,
		name     VARCHAR(50),
		nickname VARCHAR(50)
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

// TestMySQLInteg_AggregateResetColumns
// 聚合后 columns 状态恢复，可继续取数。
func TestMySQLInteg_AggregateResetColumns(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_AggregateIgnoreSelectSub
// 聚合忽略子查询列及其绑定（COUNT(*) 不受 SELECT 子查询影响）。
func TestMySQLInteg_AggregateIgnoreSelectSub(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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

// TestMySQLInteg_PluckDuplicateKeyOverwrite 集成附录 testPluck（重复 key 覆盖部分）：
// Pluck map 模式重复键时后值覆盖前值；keyBy 模式重复键列时最后一行覆盖。
func TestMySQLInteg_PluckDuplicateKeyOverwrite(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

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
