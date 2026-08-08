// 本文件为 MySQL 集成测试——游标分批读取（Cursor/CursorBy，含 desc 倒序参数）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_mysql_integration_test.go。
package zcdb

import (
	"context"
	_ "github.com/go-sql-driver/mysql"
	"testing"
)

// TestMySQLInteg_Cursor_Stream 验证 Cursor 流式迭代：逐行读取所有数据。
func TestMySQLInteg_Cursor_Stream(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"` // 非指针类型，NULL 时保留零值
	}
	var user row
	var names []string
	var ages []int
	for err := range db.Builder().Table("users").Select("name", "age").OrderBy("id", "ASC").Cursor(context.Background(), &user) {
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		names = append(names, user.Name)
		ages = append(ages, user.Age)
	}
	if len(names) != 5 {
		t.Errorf("expected 5 names, got %d: %v", len(names), names)
	}
	if names[0] != "alice" {
		t.Errorf("expected first name alice, got %s", names[0])
	}
	// eve 的 age 为 NULL，应该是零值 0
	if ages[4] != 0 {
		t.Errorf("expected eve's age=0 (NULL), got %d", ages[4])
	}
}

// TestMySQLInteg_Cursor_Break 验证 Cursor 迭代中 break 能正常释放资源。
func TestMySQLInteg_Cursor_Break(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var user row
	count := 0
	for err := range db.Builder().Table("users").Select("name").OrderBy("id", "ASC").Cursor(context.Background(), &user) {
		if err != nil {
			t.Fatalf("Cursor error: %v", err)
		}
		count++
		if count == 2 {
			break // 只取前 2 条
		}
	}
	if count != 2 {
		t.Errorf("expected 2 iterations, got %d", count)
	}
}

// TestMySQLInteg_CursorBy_Keyset 验证 CursorBy 游标分页迭代：分批获取全部数据。
func TestMySQLInteg_CursorBy_Keyset(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	var lastID int
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, user.Name)
		lastID = user.ID
	}
	if len(names) != 5 {
		t.Errorf("expected 5 names, got %d: %v", len(names), names)
	}
	if names[0] != "alice" || names[4] != "eve" {
		t.Errorf("expected alice...eve, got %v", names)
	}
	if lastID != 5 {
		t.Errorf("expected last id=5, got %d", lastID)
	}
}

// TestMySQLInteg_CursorBy_Break 验证 CursorBy 迭代中 break 能正常停止。
func TestMySQLInteg_CursorBy_Break(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		count++
		if count == 3 {
			break
		}
	}
	if count != 3 {
		t.Errorf("expected 3 iterations, got %d", count)
	}
}

// TestMySQLInteg_CursorBy_IgnoresOrderBy 验证 CursorBy 会忽略已设置的 ORDER BY。
func TestMySQLInteg_CursorBy_IgnoresOrderBy(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var ids []int
	// 用户先设置了 ORDER BY name DESC，但 CursorBy 应该忽略它，强制按 id ASC
	for err := range db.Builder().Table("users").Select("id", "name").OrderBy("name", "DESC").CursorBy(context.Background(), &user, 10, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		ids = append(ids, user.ID)
	}
	// 验证结果是按 id 升序，而非 name 降序
	expected := []int{1, 2, 3, 4, 5}
	if len(ids) != len(expected) {
		t.Errorf("expected %d ids, got %d: %v", len(expected), len(ids), ids)
	}
	for i, id := range ids {
		if id != expected[i] {
			t.Errorf("ids[%d]: expected %d, got %d", i, expected[i], id)
		}
	}
}

// TestMySQLInteg_CursorByZeroSize
// chunkSize 为 0 时直接返回，不执行任何查询。
func TestMySQLInteg_CursorByZeroSize(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	n := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 0, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		n++
	}
	if n != 0 {
		t.Errorf("expected no iterations for chunkSize=0, got %d", n)
	}
}

// TestMySQLInteg_CursorByQualifiedColumn
// CursorBy 键列支持 table.column 限定形式。
func TestMySQLInteg_CursorByQualifiedColumn(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "users.id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, user.Name)
	}
	expected := []string{"alice", "bob", "charlie", "diana", "eve"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}
}

// TestMySQLInteg_CursorBy_Desc
// CursorBy 传 desc=true 按游标列倒序分块。
func TestMySQLInteg_CursorBy_Desc(t *testing.T) {
	db := openMySQLTestDB(t)
	setupMySQLUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, 2, "id", true) {
		if err != nil {
			t.Fatalf("CursorBy(desc) error: %v", err)
		}
		names = append(names, user.Name)
	}
	expected := []string{"eve", "diana", "charlie", "bob", "alice"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d", len(expected), len(names))
	}
	for i, exp := range expected {
		if names[i] != exp {
			t.Errorf("names[%d]: expected %q, got %q", i, exp, names[i])
		}
	}
}
