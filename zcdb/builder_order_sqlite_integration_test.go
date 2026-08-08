// 本文件为 SQLite 集成测试——OrderBy/Limit/Offset/Union/锁等排序分页与集合操作。
// 测试需真实数据库连接，连接与建表 helper 见 builder_sqlite_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	_ "modernc.org/sqlite"
	"testing"
)

// TestSQLiteInteg_OrderByLimitOffset 验证排序+分页：ORDER BY age DESC 后跳过 1 行取 2 行，确认结果正确。
func TestSQLiteInteg_OrderByLimitOffset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotNull("age").
		OrderBy("age", "DESC").
		Limit(2).
		Offset(1).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "diana" {
		t.Errorf("expected [bob, diana], got %v", rows)
	}
}

// TestSQLiteInteg_ForPage 验证 ForPage 便捷分页：page=2, perPage=2 应返回第 3~4 行。
func TestSQLiteInteg_ForPage(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(2, 2).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "charlie" || rows[1].Name != "diana" {
		t.Errorf("expected [charlie, diana], got %v", rows)
	}
}

// TestSQLiteInteg_ForPageFirst 验证第一页分页：第 1 页不生成 OFFSET。
func TestSQLiteInteg_ForPageFirst(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(1, 3).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 || rows[0].Name != "alice" || rows[2].Name != "charlie" {
		t.Errorf("expected [alice, bob, charlie], got %v", rows)
	}
}

// TestSQLiteInteg_InRandomOrder 验证随机排序：InRandomOrder 生成 ORDER BY RANDOM()，返回行数不变。
func TestSQLiteInteg_InRandomOrder(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").InRandomOrder().Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows))
	}
}

// TestSQLiteInteg_Union 验证 UNION 去重合并：两个查询的结果合并后自动去除重复行。
func TestSQLiteInteg_Union(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.Union(q2).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 union results, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_UnionAll 验证 UNION ALL 不去重合并：两个查询的结果直接拼接，保留重复行。
func TestSQLiteInteg_UnionAll(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.UnionAll(q2).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 6 {
		t.Errorf("expected 6 union all results, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_UnionLockForUpdate 验证 UNION 查询 + LockForUpdate 返回错误（SQLite 不支持锁子句）。
func TestSQLiteInteg_UnionLockForUpdate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.Union(q2).LockForUpdate().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_UnionAllSharedLock 验证 UNION ALL 查询 + SharedLock 返回错误（SQLite 不支持锁子句）。
func TestSQLiteInteg_UnionAllSharedLock(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.UnionAll(q2).SharedLock().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_LockForUpdate 验证 SQLite 不支持 FOR UPDATE：返回错误。
func TestSQLiteInteg_LockForUpdate(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 1).LockForUpdate().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_SharedLock 验证 SQLite 不支持 LOCK IN SHARE MODE：返回错误。
func TestSQLiteInteg_SharedLock(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").Where("id", "=", 1).SharedLock().Find(context.Background(), &rows)
	if !errors.Is(err, ErrSQLiteLockNotSupported) {
		t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
	}
}

// TestSQLiteInteg_OrderBy_Desc 验证 OrderBy 传 DESC 时降序排序。
func TestSQLiteInteg_OrderBy_Desc(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotNull("age").
		OrderBy("age", "DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	// age DESC: charlie(35), bob(30), diana(28), alice(25)
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if rows[0].Name != "charlie" {
		t.Errorf("expected first row charlie, got %s", rows[0].Name)
	}
}

// TestSQLiteInteg_OrderByRaw 验证 OrderByRaw 原始 SQL 排序。
func TestSQLiteInteg_OrderByRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").
		WhereNotNull("age").
		OrderByRaw("age DESC").
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows, got %d", len(rows))
	}
	if rows[0].Name != "charlie" {
		t.Errorf("expected first row charlie, got %s", rows[0].Name)
	}
}

// TestSQLiteInteg_Complex_UnionAllJoinOrderBy 验证 UNION ALL + JOIN 组合。
// 预期合并后 4 行。
func TestSQLiteInteg_Complex_UnionAllJoinOrderBy(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	bigSpender := db.Builder().Table("users").
		Select("users.name", "users.age").
		JoinOn("orders", func(j *JoinBuilder) {
			j.On("users.id", "=", "orders.user_id")
		}).
		Where("orders.amount", ">", 150)

	type row struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name", "age").
		Where("status", "=", "active").
		UnionAll(bigSpender).
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_OffsetWithoutLimit 验证仅 Offset 无 Limit 时真实执行。
func TestSQLiteInteg_OffsetWithoutLimit(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	var users []struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	err := db.Builder().
		Table("users").
		OrderBy("id", "ASC").
		Offset(2).
		Find(context.Background(), &users)
	assertNoError(t, err)
	if len(users) != 3 {
		t.Fatalf("Offset(2) without Limit: expected 3 rows, got %d", len(users))
	}
	if users[0].Name != "charlie" {
		t.Errorf("Offset(2) without Limit: expected first row charlie, got %s", users[0].Name)
	}
}

// TestSQLiteInteg_MultipleUnions
// 三个子查询连续追加 UNION / UNION ALL。
func TestSQLiteInteg_MultipleUnions(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	// UNION 去重：active(3) ∪ age>30(1) ∪ age<26(1) = 4 行
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	q3 := db.Builder().Table("users").Select("name").Where("age", "<", 26)
	var rows []row
	err := q1.Union(q2).Union(q3).OrderBy("name", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}

	// UNION ALL 保留重复：3+1+1 = 5 行
	q4 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q5 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	q6 := db.Builder().Table("users").Select("name").Where("age", "<", 26)
	var rows2 []row
	err = q4.UnionAll(q5).UnionAll(q6).Find(context.Background(), &rows2)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows2) != 5 {
		t.Errorf("expected 5 rows, got %d", len(rows2))
	}
}

// TestSQLiteInteg_UnionWithJoin
// union 分支子查询中带 JOIN。
func TestSQLiteInteg_UnionWithJoin(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	// 分支一：active 用户；分支二：在 orders 有订单的用户（join 去重）
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").
		Join("orders", "orders.user_id", "=", "users.id").Distinct()

	var rows []row
	err := q1.Union(q2).OrderBy("name", "ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 {
		t.Errorf("expected 4 rows, got %d: %v", len(rows), rows)
	}
}

// TestSQLiteInteg_UnionLimitOffset
// union 结果整体 limit/offset。
func TestSQLiteInteg_UnionLimitOffset(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	// 整体排序后取第 2、3 条：[alice, bob, charlie, diana] → [bob, charlie]
	var rows []row
	err := q1.Union(q2).OrderBy("name", "ASC").Limit(2).Offset(1).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 || rows[0].Name != "bob" || rows[1].Name != "charlie" {
		t.Errorf("expected [bob, charlie], got %v", rows)
	}
}

// TestSQLiteInteg_UnionOrderByRaw
// union 后 OrderByRaw 执行正常（多分支 where 绑定与排序表达式绑定顺序正确）。
func TestSQLiteInteg_UnionOrderByRaw(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)
	var rows []row
	err := q1.Union(q2).OrderByRaw("name ASC").Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 4 || rows[0].Name != "alice" || rows[3].Name != "diana" {
		t.Errorf("expected sorted [alice bob charlie diana], got %v", rows)
	}
}

// TestSQLiteInteg_UnionCountWithOrdersAndPaging
// 带排序/分页的 union 计数：总数不受 order/limit/offset 影响。
func TestSQLiteInteg_UnionCountWithOrdersAndPaging(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	q1 := db.Builder().Table("users").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Where("age", ">", 30)
	count, err := q1.Union(q2).OrderBy("name", "ASC").Limit(2).Offset(1).Count(context.Background())
	if err != nil {
		t.Fatalf("Count error: %v", err)
	}
	if count != 4 {
		t.Errorf("expected 4, got %d", count)
	}
}

// TestSQLiteInteg_InOrderOf 验证按给定顺序排序用 OrderByRaw 构造
// （CASE WHEN ... THEN n END），含单值与 where 组合。
func TestSQLiteInteg_InOrderOf(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)

	// 基本：active 优先，同组按 id
	var names []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("users").
		Select("name").
		OrderByRaw("CASE WHEN status = 'active' THEN 0 WHEN status = 'inactive' THEN 1 ELSE 2 END, id").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("basic Find error: %v", err)
	}
	expected := []string{"alice", "bob", "diana", "charlie", "eve"}
	if len(names) != len(expected) {
		t.Fatalf("basic: expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("basic[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}

	// 单值：仅一个特例优先
	names = nil
	err = db.Builder().Table("users").
		Select("name").
		OrderByRaw("CASE WHEN status = 'inactive' THEN 0 ELSE 1 END, id").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("single value Find error: %v", err)
	}
	expected = []string{"charlie", "eve", "alice", "bob", "diana"}
	if len(names) != len(expected) {
		t.Fatalf("single value: expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("single value[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}

	// 与 where 组合：age > 26 且 charlie 优先
	names = nil
	err = db.Builder().Table("users").
		Select("name").
		Where("age", ">", 26).
		OrderByRaw("CASE WHEN name = 'charlie' THEN 0 ELSE 1 END, id").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("with where Find error: %v", err)
	}
	expected = []string{"charlie", "bob", "diana"}
	if len(names) != len(expected) {
		t.Fatalf("with where: expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("with where[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}
}

// TestSQLiteInteg_OrderBySubQuery 验证排序列用子查询构造
// （OrderByRaw 内联子查询）。
func TestSQLiteInteg_OrderBySubQuery(t *testing.T) {
	db := openSQLiteTestDB(t)
	setupSQLiteUsersTable(t, db)
	setupSQLiteOrdersTable(t, db)

	// 按订单数降序（alice/bob 各 2 单，charlie/diana 各 1 单，eve 0 单），同数按 id
	var names []struct {
		Name string `db:"name"`
	}
	err := db.Builder().Table("users").
		Select("name").
		OrderByRaw("(SELECT COUNT(*) FROM orders WHERE orders.user_id = users.id) DESC, id ASC").
		Find(context.Background(), &names)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	expected := []string{"alice", "bob", "charlie", "diana", "eve"}
	if len(names) != len(expected) {
		t.Fatalf("expected %v, got %+v", expected, names)
	}
	for i, n := range names {
		if n.Name != expected[i] {
			t.Errorf("[%d]: expected %s, got %s", i, expected[i], n.Name)
		}
	}
}
