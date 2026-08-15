// 本文件为 PostgreSQL 集成测试——游标分批读取（Cursor/CursorBy，含 desc 倒序参数）。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	_ "github.com/lib/pq"
	"sync/atomic"
	"testing"
	"time"
)

// TestPgInteg_Cursor_Stream 验证 Cursor 流式迭代：逐行读取所有数据。
func TestPgInteg_Cursor_Stream(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Cursor_Break 验证 Cursor 迭代中 break 能正常释放资源。
func TestPgInteg_Cursor_Break(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorBy_Keyset 验证 CursorBy 游标分页迭代：分批获取全部数据。
func TestPgInteg_CursorBy_Keyset(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorBy_ContextCancel 边界固化用例（审查结论）：
// CursorBy 迭代期间 ctx 被取消时，错误能传播给调用方（本库驱动均在 query 层缓冲结果集，
// 取消在下一批 query 时报错；单批场景下驱动缓冲使 rows.Next 不受影响）。
// 已修复：批次循环后补 rows.Err() 检查（M1），非缓冲驱动在中途出错时也能经此通道 yield 错误。
func TestPgInteg_CursorBy_ContextCancel(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var user row
	var lastErr error
	count := 0
	for err := range db.Builder().Table("users").Select("id", "name").CursorBy(ctx, &user, 3, "id") {
		lastErr = err
		if err != nil {
			break
		}
		count++
		if count == 1 {
			// 首批第 1 行后取消：下次 rows.Next() 应报错，rows.Err() 非 nil
			cancel()
		}
	}
	if lastErr == nil {
		t.Errorf("ctx 取消后 CursorBy 静默结束（收到 %d 行），预期 yield 错误", count)
	}
}

// TestPgInteg_CursorBy_Break 验证 CursorBy 迭代中 break 能正常停止。
func TestPgInteg_CursorBy_Break(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorBy_IgnoresOrderBy 验证 CursorBy 会忽略已设置的 ORDER BY。
func TestPgInteg_CursorBy_IgnoresOrderBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorByZeroSize
// chunkSize 为 0 时直接返回，不执行任何查询。
func TestPgInteg_CursorByZeroSize(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorByQualifiedColumn
// CursorBy 键列支持 table.column 限定形式。
func TestPgInteg_CursorByQualifiedColumn(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorBy_Desc
// CursorBy 传 desc=true 按游标列倒序分块。
func TestPgInteg_CursorBy_Desc(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_CursorBy_ExactPageBoundary 验证 PostgreSQL 下数据量恰为 chunkSize 整数倍时，
// 末批通过多取一条探测判断结束（探测行不丢、不重），不再执行一次返回 0 行的空查询。
func TestPgInteg_CursorBy_ExactPageBoundary(t *testing.T) {
	openPgTestDB(t) // 确保测试库存在并完成清理

	var sqlCount int32
	pool, err := NewPool(PoolConfig{
		DriverName: "postgres",
		DSN:        "host=127.0.0.1 port=5432 user=postgres password=root sslmode=disable dbname=zckg_test_integ",
	})
	if err != nil {
		t.Fatalf("failed to open postgres: %v", err)
	}
	dao, err := NewDBDao(pool, "postgres", func(ctx context.Context, elapsed time.Duration, sqlStr string, args []any) {
		atomic.AddInt32(&sqlCount, 1)
	}, "")
	if err != nil {
		t.Fatalf("failed to create dao: %v", err)
	}
	t.Cleanup(func() { _ = dao.Close() })

	// 6 行数据（6 = 2 × 3，整页边界场景）
	mustExec(t, dao, `DROP TABLE IF EXISTS cursor_items CASCADE`)
	mustExec(t, dao, `CREATE TABLE cursor_items (
		id BIGSERIAL PRIMARY KEY,
		name VARCHAR(64) NOT NULL
	)`)
	mustExec(t, dao, `INSERT INTO cursor_items (name) VALUES ('a'), ('b'), ('c'), ('d'), ('e'), ('f')`)
	atomic.StoreInt32(&sqlCount, 0)

	type row struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	var item row
	var names []string
	for err := range dao.Builder().Table("cursor_items").Select("id", "name").CursorBy(context.Background(), &item, 3, "id") {
		if err != nil {
			t.Fatalf("CursorBy error: %v", err)
		}
		names = append(names, item.Name)
	}
	if len(names) != 6 {
		t.Fatalf("expected 6 rows, got %d: %v", len(names), names)
	}
	want := []string{"a", "b", "c", "d", "e", "f"}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("names[%d]: expected %q, got %q", i, want[i], names[i])
		}
	}
	// 旧实现：3+3+0 共 3 次查询（末尾一次空查询）；修复后：每批多取一条探测，共 2 次查询
	if got := atomic.LoadInt32(&sqlCount); got != 2 {
		t.Errorf("expected 2 queries for exact page boundary, got %d", got)
	}
}
