// 本文件为 SQLite 集成测试——游标分批读取（Cursor/CursorBy，含 desc 倒序参数）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "modernc.org/sqlite"
	"testing"
)

// TestSQLiteInteg_Cursor_Stream 验证 Cursor 流式迭代：逐行扫描，break 时自动释放连接。
// 同时验证 NULL 安全扫描：eve 的 age 为 NULL，扫描到 int 类型时保留零值 0。
func TestSQLiteInteg_Cursor_Stream(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Cursor_Break 验证 Cursor 迭代中 break 能正常释放资源。
func TestSQLiteInteg_Cursor_Break(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_Keyset 验证 CursorBy 游标分页迭代：分批获取全部数据。
func TestSQLiteInteg_CursorBy_Keyset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_Break 验证 CursorBy 迭代中 break 能正常停止。
func TestSQLiteInteg_CursorBy_Break(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_IgnoresOrderBy 验证 CursorBy 会忽略已设置的 ORDER BY。
func TestSQLiteInteg_CursorBy_IgnoresOrderBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_NullCursorValue 验证游标列值为 NULL 时迭代器报错终止，
// 而不是无限循环重复返回同一批数据。
func TestSQLiteInteg_CursorBy_NullCursorValue(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// age 用指针类型，eve 的 age 为 NULL
	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Age  *int   `db:"age"`
	}
	var user row
	gotErr := false
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name", "age").CursorBy(context.Background(), &user, 2, "age") {
		if err != nil {
			gotErr = true
			break
		}
		count++
		if count > 100 {
			t.Fatalf("CursorBy 未终止，疑似死循环（已迭代 %d 次）", count)
		}
	}
	if !gotErr {
		t.Errorf("expected error for NULL cursor value, got nil (iterated %d times)", count)
	}
	if count != 0 {
		t.Errorf("expected 0 iterations before error, got %d", count)
	}
}

// TestSQLiteInteg_CursorBy_NilEmbeddedPtr 验证 dest 的嵌入指针结构体为 nil 时，
// CursorBy 返回错误而非 panic。
func TestSQLiteInteg_CursorBy_NilEmbeddedPtr(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 嵌入类型名必须大写导出，才会被字段展开
	type Base struct {
		ID int `db:"id"`
	}
	// 嵌入指针未初始化，为 nil
	type user struct {
		*Base
		Name string `db:"name"`
	}
	var u user
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("CursorBy panicked: %v", r)
		}
	}()
	gotErr := false
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &u, 2, "id") {
		if err != nil {
			gotErr = true
			break
		}
		count++
		if count > 10 {
			t.Fatalf("CursorBy 未终止，疑似死循环")
		}
	}
	if !gotErr {
		t.Errorf("expected error for unavailable cursor field, got nil (iterated %d times)", count)
	}
}

// TestSQLiteInteg_Cursor_ErrorPaths 验证 Cursor 对非法 dest 返回错误：
// 非指针与非结构体指针均应在迭代首轮 yield 错误。
func TestSQLiteInteg_Cursor_ErrorPaths(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 非指针 dest（结构体值）
	gotErr := false
	for err := range db.Builder().Table("users").Cursor(context.Background(), struct {
		Name string `db:"name"`
	}{}) {
		gotErr = errors.Is(err, ErrNotPointer)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotPointer for non-pointer dest, got nil")
	}

	// 非结构体指针 dest（*int）
	gotErr = false
	var num int
	for err := range db.Builder().Table("users").Cursor(context.Background(), &num) {
		gotErr = errors.Is(err, ErrNotStruct)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotStruct for *int dest, got nil")
	}
}

// TestSQLiteInteg_CursorBy_ErrorPaths 验证 CursorBy 对非法 dest 与缺失游标字段返回错误。
func TestSQLiteInteg_CursorBy_ErrorPaths(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 非指针 dest
	gotErr := false
	for err := range db.Builder().Table("users").CursorBy(context.Background(), struct {
		Name string `db:"name"`
	}{}, 2, "id") {
		gotErr = errors.Is(err, ErrNotPointer)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotPointer for non-pointer dest, got nil")
	}

	// 非结构体指针 dest（*int）
	gotErr = false
	var num int
	for err := range db.Builder().Table("users").CursorBy(context.Background(), &num, 2, "id") {
		gotErr = errors.Is(err, ErrNotStruct)
		break
	}
	if !gotErr {
		t.Error("expected ErrNotStruct for *int dest, got nil")
	}

	// 缺失游标字段：结构体不含 cursorColumn 对应字段
	gotErr = false
	var noCursor struct {
		Name string `db:"name"`
	}
	for err := range db.Builder().Table("users").CursorBy(context.Background(), &noCursor, 2, "id") {
		gotErr = errors.Is(err, ErrCursorFieldNotFound)
		break
	}
	if !gotErr {
		t.Error("expected ErrCursorFieldNotFound for missing cursor field, got nil")
	}
}

// TestSQLiteInteg_CursorBy_DefaultChunkSize 验证 chunkSize 为负数时使用默认值 100：
// 数据量小于默认分块大小时应完整迭代。
func TestSQLiteInteg_CursorBy_DefaultChunkSize(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var user row
	var names []string
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(context.Background(), &user, -1, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, user.Name)
	}
	if len(names) != 5 {
		t.Errorf("expected 5 names, got %d", len(names))
	}
}

// TestSQLiteInteg_CursorByZeroSize
// chunkSize 为 0 时直接返回，不执行任何查询。
func TestSQLiteInteg_CursorByZeroSize(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorByQualifiedColumn
// CursorBy 键列支持 table.column 限定形式。
func TestSQLiteInteg_CursorByQualifiedColumn(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_CursorBy_Desc
// CursorBy 传 desc=true 按游标列倒序分块。
func TestSQLiteInteg_CursorBy_Desc(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

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

// TestSQLiteInteg_Bug_CursorByCtxCancel 锁定 M1 修复：
// CursorBy 多批迭代中 ctx 取消时，错误必须 yield 给调用方（rows.Err() 检查 +
// 下一批 query 层报错双通道），不得静默截断——否则调用方会把出错当作正常结束而丢数据。
func TestSQLiteInteg_Bug_CursorByCtxCancel(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var user row
	var lastErr error
	count := 0
	// chunkSize=2：users 共 5 行 → 3 批；首批迭代完后取消，后续批次必须报错
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(ctx, &user, 2, "id") {
		if err != nil {
			lastErr = err
			break
		}
		count++
		if count == 2 {
			cancel()
		}
	}
	if lastErr == nil {
		t.Fatalf("ctx 取消后 CursorBy 静默结束（收到 %d 行），预期 yield 错误", count)
	}
	if !errors.Is(lastErr, context.Canceled) {
		t.Logf("yield 错误为 %v（非 context.Canceled 包装也可接受，关键是错误未静默）", lastErr)
	}
}
