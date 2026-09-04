// 本文件为 PostgreSQL 集成测试——OrderBy/Limit/Offset/Union/锁等排序分页与集合操作。
// 测试需真实数据库连接，连接与建表 helper 见 builder_postgres_integration_test.go。
package zcdb

import (
	"context"
	"errors"
	"fmt"
	_ "github.com/lib/pq"
	"testing"
)

// TestPgInteg_OrderByLimitOffset 验证排序+分页：ORDER BY + LIMIT + OFFSET。
func TestPgInteg_OrderByLimitOffset(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_ForPage 验证 ForPage 便捷分页。
func TestPgInteg_ForPage(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InRandomOrder 验证随机排序：ORDER BY RANDOM()。
func TestPgInteg_InRandomOrder(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Union 验证 UNION 去重合并。
func TestPgInteg_Union(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UnionAll 验证 UNION ALL 不去重合并。
func TestPgInteg_UnionAll(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_LockForUpdate 验证 SELECT ... FOR UPDATE 语法可执行（事务内）。
func TestPgInteg_LockForUpdate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type row struct {
			Name string `db:"name"`
		}
		var rows []row
		err := db.Builder().Table("users").Select("name").Where("id", "=", 1).LockForUpdate().Find(ctx, &rows)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			return fmt.Errorf("expected alice, got %v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestPgInteg_LockForUpdate error: %v", err)
	}
}

// TestPgInteg_SharedLock 验证 SELECT ... FOR SHARE 语法可执行（事务内）。
func TestPgInteg_SharedLock(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	err := db.Transaction(context.Background(), func(ctx context.Context) error {
		type row struct {
			Name string `db:"name"`
		}
		var rows []row
		err := db.Builder().Table("users").Select("name").Where("id", "=", 1).SharedLock().Find(ctx, &rows)
		if err != nil {
			return err
		}
		if len(rows) != 1 || rows[0].Name != "alice" {
			return fmt.Errorf("expected alice, got %v", rows)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TestPgInteg_SharedLock error: %v", err)
	}
}

// TestPgInteg_UnionLockNotSupported 验证 PostgreSQL UNION + LOCK 返回错误（PostgreSQL 不支持此组合）。
func TestPgInteg_UnionLockNotSupported(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 30)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.Union(q2).LockForUpdate().Find(context.Background(), &rows)
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestPgInteg_UnionSharedLockNotSupported 验证 PostgreSQL UNION + SharedLock 返回错误。
func TestPgInteg_UnionSharedLockNotSupported(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	q1 := db.Builder().Table("users").Select("name").Where("status", "=", "active")
	q2 := db.Builder().Table("users").Select("name").Where("age", ">", 25)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := q1.UnionAll(q2).SharedLock().Find(context.Background(), &rows)
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestPgInteg_OrderBy_Desc 验证 OrderBy 传 DESC 时降序排序。
func TestPgInteg_OrderBy_Desc(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_OrderByRaw 验证 OrderByRaw 原始 SQL 排序。
func TestPgInteg_OrderByRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_Complex_UnionAllJoinOrderBy 验证 UNION ALL + JOIN 组合。
// 预期合并后 4 行。
func TestPgInteg_Complex_UnionAllJoinOrderBy(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_Complex_NestedSubqueryLockForUpdate 验证多层嵌套子查询 + LOCK FOR UPDATE。
// 预期：alice, bob
func TestPgInteg_Complex_NestedSubqueryLockForUpdate(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").
		Select("name").
		WhereInSub("id", func(sub *Builder) {
			sub.Table("orders").
				Select("user_id").
				GroupBy("user_id").
				HavingRaw("AVG(amount) > $1", 75).
				HavingRaw("COUNT(*) >= $2", 2)
		}).
		OrderBy("name", "ASC").
		LockForUpdate().
		Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(rows), rows)
	}
	if rows[0].Name != "alice" {
		t.Errorf("row[0]: expected alice, got %s", rows[0].Name)
	}
	if rows[1].Name != "bob" {
		t.Errorf("row[1]: expected bob, got %s", rows[1].Name)
	}
}

// TestPgInteg_ForPageFirst 验证第一页分页：第 1 页不生成 OFFSET。
func TestPgInteg_ForPageFirst(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	type row struct {
		Name string `db:"name"`
	}
	var rows []row
	err := db.Builder().Table("users").Select("name").OrderBy("id", "ASC").ForPage(1, 3).Find(context.Background(), &rows)
	if err != nil {
		t.Fatalf("query error: %v", err)
	}
	if len(rows) != 3 {
		t.Errorf("expected 3 rows, got %d", len(rows))
	}
	if rows[0].Name != "alice" {
		t.Errorf("expected first row alice, got %s", rows[0].Name)
	}
}

// TestPgInteg_OffsetWithoutLimit 验证仅 Offset 无 Limit 时真实执行（PG 原生支持）。
func TestPgInteg_OffsetWithoutLimit(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_MultipleUnions
// 三个子查询连续追加 UNION / UNION ALL。
func TestPgInteg_MultipleUnions(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UnionWithJoin
// union 分支子查询中带 JOIN。
func TestPgInteg_UnionWithJoin(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_UnionLimitOffset
// union 结果整体 limit/offset。
func TestPgInteg_UnionLimitOffset(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UnionOrderByRaw
// union 后 OrderByRaw 执行正常（多分支 where 绑定与排序表达式绑定顺序正确）。
func TestPgInteg_UnionOrderByRaw(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_UnionCountWithOrdersAndPaging
// 带排序/分页的 union 计数：总数不受 order/limit/offset 影响。
func TestPgInteg_UnionCountWithOrdersAndPaging(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_InOrderOf 验证按给定顺序排序用 OrderByRaw 构造
// （CASE WHEN ... THEN n END），含单值与 where 组合。
func TestPgInteg_InOrderOf(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

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

// TestPgInteg_OrderBySubQuery 验证排序列用子查询构造
// （OrderByRaw 内联子查询）。
func TestPgInteg_OrderBySubQuery(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)
	setupPgOrdersTable(t, db)

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

// TestPgInteg_NewApi_LockSelect 验证带锁查询在 PG 事务中的真实执行（FOR UPDATE / FOR SHARE）。
func TestPgInteg_NewApi_LockSelect(t *testing.T) {
	db := openPgTestDB(t)
	setupPgUsersTable(t, db)

	// 编译形态断言
	sqlStr, _, err := db.Builder().Table("users").Where("id", "=", 1).LockForUpdate().ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" = $1 FOR UPDATE`, sqlStr)
	sqlStr, _, err = db.Builder().Table("users").Where("id", "=", 1).SharedLock().ToSelect()
	assertNoError(t, err)
	assertSQL(t, `SELECT * FROM "users" WHERE "id" = $1 FOR SHARE`, sqlStr)

	// 事务中带锁查询真实执行
	err = db.Transaction(context.Background(), func(ctx context.Context) error {
		type row struct {
			Name string `db:"name"`
		}
		var u row
		err := db.Builder().Table("users").Select("name").Where("id", "=", 1).
			Limit(1).LockForUpdate().First(ctx, &u)
		if err != nil {
			return err
		}
		if u.Name != "alice" {
			t.Errorf("LockForUpdate: expected alice, got %s", u.Name)
		}

		var u2 row
		err = db.Builder().Table("users").Select("name").Where("id", "=", 2).
			Limit(1).SharedLock().First(ctx, &u2)
		if err != nil {
			return err
		}
		if u2.Name != "bob" {
			t.Errorf("SharedLock: expected bob, got %s", u2.Name)
		}
		return nil
	})
	assertNoError(t, err)
}

// TestPgInteg_SelectRowLock 覆盖 PostgreSQL SELECT 行锁（FOR UPDATE / FOR SHARE）
// 编译分支：带锁查询可正常执行并返回结果。
// （原 crossDialect 方言特有用例归位。）
func TestPgInteg_SelectRowLock(t *testing.T) {
	dao := openPgTestDB(t)
	crossDialectSetupScanTable(t, dao)

	var rows []crossDialectRow
	if err := dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").
		LockForUpdate().Find(t.Context(), &rows); err != nil {
		t.Fatalf("FOR UPDATE 查询不应报错: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("FOR UPDATE 应返回 2 行，实际 %d", len(rows))
	}

	rows = nil
	if err := dao.Builder().Table("cross_dialect_scan").OrderBy("id", "ASC").
		SharedLock().Find(t.Context(), &rows); err != nil {
		t.Fatalf("FOR SHARE 查询不应报错: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("FOR SHARE 应返回 2 行，实际 %d", len(rows))
	}
}
