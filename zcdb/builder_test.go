package zcdb

import (
	"errors"
	"reflect"
	"sync"
	"testing"
)

// ==================== 测试用结构体 ====================

// INSERT/UPDATE 用结构体（字段为 any）
type userInsert struct {
	Name  any `db:"name"`
	Age   any `db:"age"`
	Email any `db:"email"`
}

// INSERT 用结构体（字段为指针类型）—— nil 指针会被跳过，非 nil 会被自动解引用
type userInsertPtr struct {
	Name  *string `db:"name"`
	Age   *int    `db:"age"`
	Email *string `db:"email"`
}

type userUpdate struct {
	Name   any `db:"name"`
	Age    any `db:"age"`
	Status any `db:"status"`
}

// UPDATE 用结构体（字段为指针类型）—— nil 指针会被跳过，非 nil 会被自动解引用
type userUpdatePtr struct {
	Name   *string `db:"name"`
	Age    *int    `db:"age"`
	Status *string `db:"status"`
}

// SELECT scan 用结构体（字段为具体类型）
type userRow struct {
	ID    int    `db:"id"`
	Name  string `db:"name"`
	Age   int    `db:"age"`
	Email string `db:"email"`
}

// 无标签结构体（测试 snake_case 转换）
type orderItem struct {
	OrderID   int    `db:"-"`
	ItemName  string // 应转为 item_name
	UnitPrice int    // 应转为 unit_price
}

// ==================== 反射工具测试 ====================

func TestSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"ID", "id"},
		{"UserID", "user_id"},
		{"ItemName", "item_name"},
		{"UnitPrice", "unit_price"},
		{"HTMLParser", "html_parser"},
		{"simpleCase", "simple_case"},
	}
	for _, tt := range tests {
		result := toSnakeCase(tt.input)
		if result != tt.expected {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSelectWithSnakeCaseColumns(t *testing.T) {
	g := NewMySQLGrammar()
	sql, _, err := NewBuilder(g, nil).
		Table("order_items").
		Select("item_name", "unit_price").
		ToSelect()

	assertNoError(t, err)
	// OrderID 有 db:"-" 标签应被跳过
	// ItemName 无标签应转为 item_name
	// UnitPrice 无标签应转为 unit_price
	assertSQL(t, "SELECT `item_name`, `unit_price` FROM `order_items`", sql)
}

// ==================== orderItem 结构体测试（snake_case 转换 + db:"-" 跳过）====================

// TestInsertWithSnakeCaseStruct 验证 ToInsert 对无标签结构体的处理：db:"-" 字段被跳过，无标签字段名自动转为 snake_case。
func TestInsertWithSnakeCaseStruct(t *testing.T) {
	g := NewMySQLGrammar()
	data := orderItem{
		OrderID:   100,
		ItemName:  "Widget",
		UnitPrice: 999,
	}
	sql, args, err := NewBuilder(g, nil).Table("order_items").ToInsert(data)
	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `order_items` (`item_name`, `unit_price`) VALUES (?, ?)", sql)
	assertArgs(t, []any{"Widget", 999}, args)
}

// TestInsertBatchWithSnakeCaseStruct 验证 ToInsert 批量插入对无标签结构体的处理：列名来自 snake_case 转换，db:"-" 字段被跳过。
func TestInsertBatchWithSnakeCaseStruct(t *testing.T) {
	g := NewMySQLGrammar()
	data := []orderItem{
		{OrderID: 1, ItemName: "Apple", UnitPrice: 300},
		{OrderID: 2, ItemName: "Banana", UnitPrice: 150},
	}
	sql, args, err := NewBuilder(g, nil).Table("order_items").ToInsert(data)
	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `order_items` (`item_name`, `unit_price`) VALUES (?, ?), (?, ?)", sql)
	assertArgs(t, []any{"Apple", 300, "Banana", 150}, args)
}

// TestUpdateWithSnakeCaseStruct 验证 ToUpdate 对无标签结构体的处理：db:"-" 字段被跳过，无标签字段名自动转为 snake_case。
func TestUpdateWithSnakeCaseStruct(t *testing.T) {
	g := NewMySQLGrammar()
	data := orderItem{
		OrderID:   100,
		ItemName:  "Gadget",
		UnitPrice: 1999,
	}
	sql, args, err := NewBuilder(g, nil).Table("order_items").Where("order_id", "=", 100).ToUpdate(data)
	assertNoError(t, err)
	assertSQL(t, "UPDATE `order_items` SET `item_name` = ?, `unit_price` = ? WHERE `order_id` = ?", sql)
	assertArgs(t, []any{"Gadget", 1999, 100}, args)
}

// ==================== userRow 结构体测试（具体类型字段）====================

// TestInsertWithConcreteTypeStruct 验证 ToInsert 对具体类型字段结构体的处理：所有字段均被包含，列名取自 db 标签。
func TestInsertWithConcreteTypeStruct(t *testing.T) {
	g := NewMySQLGrammar()
	data := userRow{
		ID:    1,
		Name:  "alice",
		Age:   25,
		Email: "alice@test.com",
	}
	sql, args, err := NewBuilder(g, nil).Table("users").ToInsert(data)
	assertNoError(t, err)
	assertSQL(t, "INSERT INTO `users` (`id`, `name`, `age`, `email`) VALUES (?, ?, ?, ?)", sql)
	assertArgs(t, []any{1, "alice", 25, "alice@test.com"}, args)
}

// TestUpdateWithConcreteTypeStruct 验证 ToUpdate 对具体类型字段结构体的处理：所有字段均参与 SET，零值也不会被跳过。
func TestUpdateWithConcreteTypeStruct(t *testing.T) {
	g := NewMySQLGrammar()
	data := userRow{
		ID:    0,
		Name:  "bob",
		Age:   0,
		Email: "",
	}
	sql, args, err := NewBuilder(g, nil).Table("users").Where("id", "=", 1).ToUpdate(data)
	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `id` = ?, `name` = ?, `age` = ?, `email` = ? WHERE `id` = ?", sql)
	assertArgs(t, []any{0, "bob", 0, "", 1}, args)
}

// ==================== 错误处理测试 ====================

func TestErrorEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).ToSelect()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

func TestErrorInvalidInsertData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).Table("users").ToInsert("not a struct")
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// ==================== LOCK SQL 生成测试 ====================

// TestMySQLGrammar_LockSQL 验证 MySQL 方言的锁子句生成：LockForUpdate → FOR UPDATE，SharedLock → LOCK IN SHARE MODE。
func TestMySQLGrammar_LockSQL(t *testing.T) {
	g := &MySQLGrammar{}

	tests := []struct {
		name     string
		builder  *Builder
		expected string
	}{
		{
			name:     "LockForUpdate",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
			expected: "SELECT * FROM `users` WHERE `id` = ? FOR UPDATE",
		},
		{
			name:     "SharedLock",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
			expected: "SELECT * FROM `users` WHERE `id` = ? LOCK IN SHARE MODE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{1}, args)
		})
	}
}

// TestPostgresGrammar_LockSQL 验证 PostgreSQL 方言的锁子句生成：LockForUpdate → FOR UPDATE，SharedLock → FOR SHARE（自动转换）。
func TestPostgresGrammar_LockSQL(t *testing.T) {
	g := &PostgresGrammar{}

	tests := []struct {
		name     string
		builder  *Builder
		expected string
	}{
		{
			name:     "LockForUpdate",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
			expected: "SELECT * FROM \"users\" WHERE \"id\" = $1 FOR UPDATE",
		},
		{
			name:     "SharedLock",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
			expected: "SELECT * FROM \"users\" WHERE \"id\" = $1 FOR SHARE",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{1}, args)
		})
	}
}

// TestSQLiteGrammar_LockSQL 验证 SQLite 方言不支持锁子句：LockForUpdate 和 SharedLock 均返回错误。
func TestSQLiteGrammar_LockSQL(t *testing.T) {
	g := &SQLiteGrammar{}

	tests := []struct {
		name    string
		builder *Builder
	}{
		{
			name:    "LockForUpdate_error",
			builder: NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
		},
		{
			name:    "SharedLock_error",
			builder: NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := tt.builder.ToSelect()
			if !errors.Is(err, ErrSQLiteLockNotSupported) {
				t.Errorf("expected ErrSQLiteLockNotSupported, got %v", err)
			}
		})
	}
}

// ==================== Bug 验证测试 ====================

// TestBug_ToCountWithUnion 验证 ToCount 对 UNION 查询生成无效 SQL。
// 期望：COUNT 应包裹整个 UNION 为子查询。
// 实际：生成 (SELECT COUNT(*) ...) UNION (...)，不是合法的计数查询。
func TestBug_ToCountWithUnion(t *testing.T) {
	g := NewMySQLGrammar()
	union := NewBuilder(g, nil).Table("users").Where("age", ">", 25)
	b := NewBuilder(g, nil).Table("users").Where("status", "=", "active").Union(union)

	sql, args, err := b.ToCount()
	assertNoError(t, err)

	// 正确的 SQL 应将 UNION 包裹为子查询
	expected := "SELECT COUNT(*) FROM ((SELECT * FROM `users` WHERE `status` = ?) UNION (SELECT * FROM `users` WHERE `age` > ?)) AS `t`"
	assertSQL(t, expected, sql)
	assertArgs(t, []any{"active", 25}, args)
}

// TestBug_CollectSelectBindings_SubqueryOrder 验证 SELECT 子查询与 FROM 子查询的绑定参数顺序。
// SQL 编译顺序：SELECT 子查询先出现，FROM 子查询后出现。
// 绑定收集顺序应与之匹配。
func TestBug_CollectSelectBindings_SubqueryOrder(t *testing.T) {
	g := NewMySQLGrammar()

	// SELECT 子查询（绑定 "active"）
	selectSub := NewBuilder(g, nil).Table("orders").Select("amount").Where("status", "=", "active")
	// FROM 子查询（绑定 25）
	fromSub := NewBuilder(g, nil).Table("users").Where("age", ">", 25)

	b := NewBuilder(g, nil).
		SelectSubquery(selectSub, "sub_amount").
		FromSub(fromSub, "u")

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	// SQL 中 SELECT 子查询的 ? 在前，FROM 子查询的 ? 在后
	expected := "SELECT (SELECT `amount` FROM `orders` WHERE `status` = ?) AS `sub_amount` FROM (SELECT * FROM `users` WHERE `age` > ?) AS `u`"
	assertSQL(t, expected, sql)
	// 绑定顺序应为 ["active", 25]（与 SQL 中 ? 出现顺序一致）
	assertArgs(t, []any{"active", 25}, args)
}

// TestBug_ToUpdateWithJoin_MySQL 验证 MySQL UPDATE + JOIN 的绑定参数数量。
// CompileUpdate 在 JOIN ON value 条件中生成 ? 占位符，
// 但 ToUpdate 只收集 WHERE 绑定，不收集 JOIN 绑定，导致参数数量不匹配。
func TestBug_ToUpdateWithJoin_MySQL(t *testing.T) {
	g := NewMySQLGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := NewBuilder(g, nil).
		Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1)

	sql, args, err := b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)

	// SQL 中应有 3 个 ?：SET 1个 + JOIN ON value 1个 + WHERE 1个
	expectedSQL := "UPDATE `users` INNER JOIN `profiles` ON `users`.`id` = `profiles`.`user_id` AND `profiles`.`active` = ? SET `name` = ? WHERE `users`.`id` = ?"
	assertSQL(t, expectedSQL, sql)
	// 绑定应为 3 个：[99, "x", 1]
	assertArgs(t, []any{99, "x", 1}, args)
}

// TestBug_UpdateJoin_PG_DropsValueCondition 验证 PostgreSQL UPDATE + JOIN 编译时
// value 类型条件被静默丢弃：生成的 SQL 中不包含 profiles.active = $N。
func TestBug_UpdateJoin_PG_DropsValueCondition(t *testing.T) {
	g := NewPostgresGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := NewBuilder(g, nil).
		Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1)

	sql, args, err := b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)

	// 正确 SQL 应在 WHERE 中包含 "profiles"."active" = $2
	expectedSQL := `UPDATE "users" SET "name" = $1 FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = $2 AND "users"."id" = $3`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{"x", 99, 1}, args)
}

// TestBug_UpdateJoin_SQLite_DropsValueCondition 验证 SQLite UPDATE + JOIN 编译时
// value 类型条件被静默丢弃。
func TestBug_UpdateJoin_SQLite_DropsValueCondition(t *testing.T) {
	g := NewSQLiteGrammar()
	type updateData struct {
		Name string `db:"name"`
	}
	b := NewBuilder(g, nil).
		Table("users").
		JoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
			jb.Where("profiles.active", "=", 99)
		}).
		Where("users.id", "=", 1)

	sql, args, err := b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)

	// 正确 SQL 应在 WHERE 中包含 "profiles"."active" = ?
	expectedSQL := `UPDATE "users" SET "name" = ? FROM "profiles" WHERE "users"."id" = "profiles"."user_id" AND "profiles"."active" = ? AND "users"."id" = ?`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{"x", 99, 1}, args)
}

// TestBug_ExtractInsertData_NilPtrInSlice 验证指针切片含 nil 元素时不应 panic，
// 应返回有意义的错误。
func TestBug_ExtractInsertData_NilPtrInSlice(t *testing.T) {
	type data struct {
		Name string `db:"name"`
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("BUG: extractInsertData panicked on nil pointer element: %v", r)
		}
	}()
	_, _, err := extractInsertData([]*data{{Name: "a"}, nil, {Name: "c"}})
	if err == nil {
		t.Errorf("expected error for nil pointer element in slice, got nil")
	}
}

// TestBug_CloneShallowCopy_Union 验证 Clone 后修改 UNION 子查询不应影响原 Builder。
func TestBug_CloneShallowCopy_Union(t *testing.T) {
	g := NewMySQLGrammar()
	union := NewBuilder(g, nil).Table("admins")
	b := NewBuilder(g, nil).Table("users").Union(union)

	clone := b.Clone()
	// 修改 clone 的 UNION 子查询
	clone.unions[0].Query.Where("status", "=", "super")

	// 原 Builder 不应受影响
	origSQL, _, _ := b.ToSelect()
	if origSQL != "(SELECT * FROM `users`) UNION (SELECT * FROM `admins`)" {
		t.Errorf("BUG: Clone shares UNION sub-builder reference, original affected:\n  got: %s", origSQL)
	}
}

// TestBug_CloneShallowCopy_WhereNested 验证 Clone 后修改嵌套 WHERE 不应影响原 Builder。
func TestBug_CloneShallowCopy_WhereNested(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").WhereNested(func(sub *Builder) {
		sub.Where("age", ">", 18)
	})

	clone := b.Clone()
	// 修改 clone 的嵌套 WHERE
	clone.wheres[0].Nested.Where("status", "=", "active")

	// 原 Builder 的嵌套 WHERE 不应受影响
	origSQL, _, _ := b.ToSelect()
	expected := "SELECT * FROM `users` WHERE (`age` > ?)"
	if origSQL != expected {
		t.Errorf("BUG: Clone shares nested WHERE sub-builder reference, original affected:\n  expected: %s\n  got:      %s", expected, origSQL)
	}
}

// ==================== 代码审查发现的 Bug 测试 ====================

// TestBug_PgUnionLock 验证 PostgreSQL UNION + LOCK 返回错误（PostgreSQL 不支持此组合）。
func TestBug_PgUnionLock(t *testing.T) {
	g := NewPostgresGrammar()
	union := NewBuilder(g, nil).Table("users").Where("age", ">", 25)
	b := NewBuilder(g, nil).Table("users").Where("status", "=", "active").Union(union).LockForUpdate()

	_, _, err := b.ToSelect()
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestBug_PgUnionSharedLock 验证 PostgreSQL UNION + SharedLock 返回错误。
func TestBug_PgUnionSharedLock(t *testing.T) {
	g := NewPostgresGrammar()
	union := NewBuilder(g, nil).Table("users").Where("age", ">", 25)
	b := NewBuilder(g, nil).Table("users").Where("status", "=", "active").Union(union).SharedLock()

	_, _, err := b.ToSelect()
	if !errors.Is(err, ErrPgUnionLockNotSupported) {
		t.Errorf("expected ErrPgUnionLockNotSupported, got %v", err)
	}
}

// TestBug_PgParamCountRace 验证 PostgresGrammar 并发编译时参数计数器不互相干扰。
func TestBug_PgParamCountRace(t *testing.T) {
	g := NewPostgresGrammar()
	const n = 50
	results := make([]string, n)
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			b := NewBuilder(g, nil).Table("users").Where("id", "=", 1)
			sql, _, _ := b.ToSelect()
			results[idx] = sql
		}(i)
	}
	wg.Wait()

	expected := `SELECT * FROM "users" WHERE "id" = $1`
	for i, sql := range results {
		if sql != expected {
			t.Errorf("并发编译结果[%d] 不正确:\n  expected: %s\n  got:      %s", i, expected, sql)
			return
		}
	}
}

// TestBug_CloneWhereValuesShallowCopy 验证 Clone 后 WhereIn 的 Values 切片不应与原 Builder 共享。
func TestBug_CloneWhereValuesShallowCopy(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").WhereIn("id", []any{1, 2, 3})
	clone := b.Clone()

	// 修改 clone 的 WhereIn Values
	clone.wheres[0].Values[0] = 999

	// 原 Builder 不应受影响
	_, origArgs, _ := b.ToSelect()
	for _, arg := range origArgs {
		if v, ok := arg.(int); ok && v == 999 {
			t.Error("BUG: Clone 的 WhereIn Values 与原 Builder 共享底层数组（浅拷贝）")
			break
		}
	}
}

// TestBug_CloneJoinBindingsShallowCopy 验证 Clone 后 JoinBuilder Raw 的 Bindings 切片不应与原 Builder 共享。
func TestBug_CloneJoinBindingsShallowCopy(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		jb.Raw("orders.amount > ?", 100)
	})
	clone := b.Clone()

	// 修改 clone 的 JOIN raw bindings
	clone.joins[0].Conditions[0].Bindings[0] = 999

	// 原 Builder 不应受影响
	_, origArgs, _ := b.ToSelect()
	for _, arg := range origArgs {
		if v, ok := arg.(int); ok && v == 999 {
			t.Error("BUG: Clone 的 JOIN Bindings 与原 Builder 共享底层数组（浅拷贝）")
			break
		}
	}
}

// TestBug_PgJoinRawPlaceholder 验证 PostgreSQL JOIN ON Raw 中 ? 应转换为 $N。
func TestBug_PgJoinRawPlaceholder(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		jb.On("users.id", "=", "orders.user_id").
			Raw("orders.amount > ?", 100)
	})

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	// SQL 中不应出现 ? 占位符
	if containsStr(sql, "?") {
		t.Errorf("PostgreSQL JOIN ON Raw 中 ? 未转换为 $N:\n  got: %s", sql)
	}
	expectedSQL := `SELECT * FROM "users" INNER JOIN "orders" ON "users"."id" = "orders"."user_id" AND orders.amount > $1`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{100}, args)
}

// TestBug_PgWhereRawPlaceholder 验证 PostgreSQL WhereRaw 中 ? 应转换为 $N。
func TestBug_PgWhereRawPlaceholder(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("users").WhereRaw("age > ? AND name LIKE ?", 25, "alice%")

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	if containsStr(sql, "?") {
		t.Errorf("PostgreSQL WhereRaw 中 ? 未转换为 $N:\n  got: %s", sql)
	}
	expectedSQL := `SELECT * FROM "users" WHERE age > $1 AND name LIKE $2`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{25, "alice%"}, args)
}

// TestBug_PgHavingRawPlaceholder 验证 PostgreSQL HavingRaw 中 ? 应转换为 $N。
func TestBug_PgHavingRawPlaceholder(t *testing.T) {
	g := NewPostgresGrammar()
	b := NewBuilder(g, nil).Table("orders").
		Select("user_id").
		GroupBy("user_id").
		HavingRaw("SUM(amount) > ?", 500)

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	if containsStr(sql, "?") {
		t.Errorf("PostgreSQL HavingRaw 中 ? 未转换为 $N:\n  got: %s", sql)
	}
	expectedSQL := `SELECT "user_id" FROM "orders" GROUP BY "user_id" HAVING SUM(amount) > $1`
	assertSQL(t, expectedSQL, sql)
	assertArgs(t, []any{500}, args)
}

// ==================== 代码审查问题测试 ====================

// TestBug_OperatorInjection 验证恶意运算符不应被拼入 SQL。
func TestBug_OperatorInjection(t *testing.T) {
	g := NewMySQLGrammar()
	malicious := "= 1; DROP TABLE users; --"
	b := NewBuilder(g, nil).Table("users").Where("id", malicious, 1)

	_, _, err := b.ToSelect()
	if err == nil {
		sql, _, _ := b.ToSelect()
		t.Errorf("恶意运算符不应生成 SQL，应返回错误:\n  got: %s", sql)
	}
}

// TestBug_OperatorInjection_JoinOn 验证 JoinBuilder.Where 的运算符也应校验。
func TestBug_OperatorInjection_JoinOn(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
		jb.Where("users.id", "= 1; DROP TABLE users; --", 1)
	})

	_, _, err := b.ToSelect()
	if err == nil {
		t.Error("JoinBuilder.Where 恶意运算符不应生成 SQL，应返回错误")
	}
}

// TestBug_OperatorInjection_Having 验证 Having 的运算符也应校验。
func TestBug_OperatorInjection_Having(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("orders").
		Select("user_id").
		GroupBy("user_id").
		Having("SUM(amount)", "evil", 500)

	_, _, err := b.ToSelect()
	if err == nil {
		t.Error("Having 恶意运算符不应生成 SQL，应返回错误")
	}
}

// TestBug_ToCountPanicSafety 验证 ToCount 在内部 panic 时不应污染原 Builder 状态。
func TestBug_ToCountPanicSafety(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").Select("name", "age").Where("age", ">", 25)
	b.limit = 10

	// 添加一个会导致 panic 的 WHERE 条件（使用不支持的 WhereType）
	b.wheres = append(b.wheres, WhereClause{Type: WhereType(999)})

	// ToCount 内部会 panic，但我们用 recover 捕获
	func() {
		defer func() { recover() }()
		_, _, _ = b.ToCount()
	}()

	// panic 后 Builder 状态应已恢复
	if b.limit != 10 {
		t.Errorf("ToCount panic 后 limit 未恢复: expected 10, got %d", b.limit)
	}
	if len(b.columns) != 2 || b.columns[0] != "name" || b.columns[1] != "age" {
		t.Errorf("ToCount panic 后 columns 未恢复: expected [name age], got %v", b.columns)
	}
}

// ==================== 嵌入结构体测试 ====================

// BaseModel 用于测试嵌入结构体
type BaseModel struct {
	ID   int    `db:"id"`
	Name string `db:"name"`
}

// UserWithEmbed 包含嵌入结构体的用户
type UserWithEmbed struct {
	BaseModel
	Age int `db:"age"`
}

// TestBug_EmbeddedStruct_Insert 验证嵌入结构体的字段应被正确展开为列。
func TestBug_EmbeddedStruct_Insert(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users")
	user := UserWithEmbed{
		BaseModel: BaseModel{ID: 1, Name: "alice"},
		Age:       25,
	}
	sql, args, err := b.ToInsert(user)
	assertNoError(t, err)
	// 应该包含 id, name, age 三个列
	if !containsStr(sql, "`id`") || !containsStr(sql, "`name`") || !containsStr(sql, "`age`") {
		t.Errorf("嵌入结构体字段未展开:\n  got: %s", sql)
	}
	if len(args) != 3 {
		t.Errorf("嵌入结构体参数数量错误: expected 3, got %d (args=%v)", len(args), args)
	}
}

// TestBug_EmbeddedStruct_Scan 验证扫描时嵌入结构体字段应被正确匹配。
func TestBug_EmbeddedStruct_Scan(t *testing.T) {
	// 测试 getScanFieldInfo 能正确展开嵌入结构体
	info := getScanFieldInfo(reflect.TypeOf(UserWithEmbed{}))
	if info == nil {
		t.Fatal("getScanFieldInfo returned nil")
	}
	// 应该包含 id, name, age 三个字段映射
	if _, ok := info.columnIndex["id"]; !ok {
		t.Error("嵌入结构体字段 id 未被解析")
	}
	if _, ok := info.columnIndex["name"]; !ok {
		t.Error("嵌入结构体字段 name 未被解析")
	}
	if _, ok := info.columnIndex["age"]; !ok {
		t.Error("嵌入结构体字段 age 未被解析")
	}
}

// ==================== 覆盖率提升测试 ====================

// TestBuilder_InvalidOperatorErrorBranch 验证各链式方法传入非法运算符时走入错误分支。
func TestBuilder_InvalidOperatorErrorBranch(t *testing.T) {
	g := NewMySQLGrammar()
	invalid := "EVIL"

	tests := []struct {
		name  string
		build func() *Builder
	}{
		{"OrWhere", func() *Builder {
			return NewBuilder(g, nil).Table("t").OrWhere("a", invalid, 1)
		}},
		{"WhereColumn", func() *Builder {
			return NewBuilder(g, nil).Table("t").WhereColumn("a", invalid, "b")
		}},
		{"OrHaving", func() *Builder {
			return NewBuilder(g, nil).Table("t").Select("a").GroupBy("a").OrHaving("SUM(a)", invalid, 1)
		}},
		{"WhereSub", func() *Builder {
			return NewBuilder(g, nil).Table("t").WhereSub("a", invalid, func(sub *Builder) {
				sub.Table("t2")
			})
		}},
		{"OrWhereSub", func() *Builder {
			return NewBuilder(g, nil).Table("t").OrWhereSub("a", invalid, func(sub *Builder) {
				sub.Table("t2")
			})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			if !errors.Is(b.err, ErrInvalidOperator) {
				t.Errorf("%s: expected ErrInvalidOperator, got %v", tt.name, b.err)
			}
		})
	}
}

// TestJoinBuilder_InvalidOperatorErrorBranch 验证 JoinBuilder 的 On/OrOn/OrWhere 传入非法运算符时走入错误分支。
func TestJoinBuilder_InvalidOperatorErrorBranch(t *testing.T) {
	g := NewMySQLGrammar()
	invalid := "EVIL"

	tests := []struct {
		name  string
		build func() *Builder
	}{
		{"LeftJoinOn", func() *Builder {
			return NewBuilder(g, nil).Table("t").LeftJoinOn("t2", func(jb *JoinBuilder) {
				jb.On("t.id", invalid, "t2.id")
			})
		}},
		{"RightJoinOn", func() *Builder {
			return NewBuilder(g, nil).Table("t").RightJoinOn("t2", func(jb *JoinBuilder) {
				jb.On("t.id", invalid, "t2.id")
			})
		}},
		{"JoinOn_OrOn", func() *Builder {
			return NewBuilder(g, nil).Table("t").JoinOn("t2", func(jb *JoinBuilder) {
				jb.OrOn("t.id", invalid, "t2.id")
			})
		}},
		{"JoinBuilder_OrWhere", func() *Builder {
			return NewBuilder(g, nil).Table("t").JoinOn("t2", func(jb *JoinBuilder) {
				jb.OrWhere("t.id", invalid, 1)
			})
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			if !errors.Is(b.err, ErrInvalidOperator) {
				t.Errorf("%s: expected ErrInvalidOperator, got %v", tt.name, b.err)
			}
		})
	}
}

// TestGrammar_WrapTable 验证 WrapTable 对普通表名和带别名表名的处理。
func TestGrammar_WrapTable(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		input    string
		expected string
	}{
		{"MySQL_simple", &MySQLGrammar{}, "users", "`users`"},
		{"MySQL_alias", &MySQLGrammar{}, "users as u", "`users` AS `u`"},
		{"MySQL_ALIAS", &MySQLGrammar{}, "users AS u", "`users` AS `u`"},
		{"Postgres_simple", &PostgresGrammar{}, "users", `"users"`},
		{"Postgres_alias", &PostgresGrammar{}, "users as u", `"users" AS "u"`},
		{"SQLite_simple", &SQLiteGrammar{}, "users", `"users"`},
		{"SQLite_alias", &SQLiteGrammar{}, "users as u", `"users" AS "u"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.grammar.WrapTable(tt.input)
			if result != tt.expected {
				t.Errorf("WrapTable(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestGrammar_WrapColumn 验证 WrapColumn 对各种列名格式的处理。
func TestGrammar_WrapColumn(t *testing.T) {
	g := &MySQLGrammar{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"star", "*", "*"},
		{"simple", "name", "`name`"},
		{"table_column", "users.name", "`users`.`name`"},
		{"function_expr", "COUNT(id)", "COUNT(id)"},
		{"as_alias", "name AS user_name", "`name` AS `user_name`"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := g.WrapColumn(tt.input)
			if result != tt.expected {
				t.Errorf("WrapColumn(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestBuilder_OrderByBranches 验证 OrderBy 对各种 direction 输入的处理。
func TestBuilder_OrderByBranches(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name      string
		direction string
		expected  string
	}{
		{"empty_defaults_ASC", "", "ASC"},
		{"ASC_upper", "ASC", "ASC"},
		{"DESC_upper", "DESC", "DESC"},
		{"desc_lower", "desc", "DESC"},
		{"random_string", "xyz", "ASC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewBuilder(g, nil).Table("t").OrderBy("col", tt.direction)
			if len(b.orders) != 1 || b.orders[0].Direction != tt.expected {
				t.Errorf("OrderBy(%q): expected direction %q, got %v", tt.direction, tt.expected, b.orders)
			}
		})
	}
}

// TestBuilder_ForPage_InvalidPage 验证 ForPage 传入 page < 1 时自动修正为 1。
func TestBuilder_ForPage_InvalidPage(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("t").ForPage(0, 10)
	if b.offset != 0 || b.limit != 10 {
		t.Errorf("ForPage(0, 10): expected offset=0 limit=10, got offset=%d limit=%d", b.offset, b.limit)
	}
	b = NewBuilder(g, nil).Table("t").ForPage(-5, 20)
	if b.offset != 0 || b.limit != 20 {
		t.Errorf("ForPage(-5, 20): expected offset=0 limit=20, got offset=%d limit=%d", b.offset, b.limit)
	}
}

// TestBuilder_CloneDeepCopy 验证 Clone 对各种可选字段的深拷贝（提升 Clone 覆盖率）。
func TestBuilder_CloneDeepCopy(t *testing.T) {
	g := NewMySQLGrammar()

	// 构造包含所有可选字段的 Builder
	fromSub := NewBuilder(g, nil).Table("sub_t")
	selectSub := NewBuilder(g, nil).Table("orders").Select("amount").Where("status", "=", "active")

	b := NewBuilder(g, nil).
		Table("users").
		Select("name", "age").
		Distinct().
		FromSub(fromSub, "f").
		SelectSubquery(selectSub, "sub_amount").
		Join("orders", "users.id", "=", "orders.user_id").
		LeftJoinOn("profiles", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "profiles.user_id")
		}).
		RightJoinOn("tags", func(jb *JoinBuilder) {
			jb.On("users.id", "=", "tags.user_id")
		}).
		CrossJoin("meta").
		WhereIn("id", []any{1, 2, 3}).
		WhereRaw("age > ?", 18).
		GroupBy("name").
		Having("COUNT(*)", ">", 1).
		OrderBy("name", "ASC").
		Limit(10).
		Offset(5).
		Union(NewBuilder(g, nil).Table("admins")).
		LockForUpdate()

	clone := b.Clone()

	// 验证 clone 的 fromSub 是独立副本
	clone.fromSub.Where("extra", "=", 1)
	origSQL, _, _ := b.ToSelect()
	if containsStr(origSQL, "extra") {
		t.Error("BUG: Clone 的 fromSub 与原 Builder 共享引用")
	}

	// 验证 clone 的 selectSubs 是独立副本
	clone.selectSubs[0].Query.Where("extra2", "=", 2)
	origSQL2, _, _ := b.ToSelect()
	if containsStr(origSQL2, "extra2") {
		t.Error("BUG: Clone 的 selectSubs 与原 Builder 共享引用")
	}

	// 验证 clone 的 groups 是独立副本
	clone.groups[0] = "modified"
	if b.groups[0] == "modified" {
		t.Error("BUG: Clone 的 groups 与原 Builder 共享底层数组")
	}

	// 验证 clone 的 havings 是独立副本
	clone.havings[0].Value = 999
	if b.havings[0].Value == 999 {
		t.Error("BUG: Clone 的 havings 与原 Builder 共享")
	}

	// 验证 clone 的 orders 是独立副本
	clone.orders[0].Column = "modified_col"
	if b.orders[0].Column == "modified_col" {
		t.Error("BUG: Clone 的 orders 与原 Builder 共享底层数组")
	}
}

// ==================== 覆盖率提升测试（第二轮）====================

// TestIntToStr 验证 intToStr 对零、正数、负数的处理。
func TestIntToStr(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{42, "42"},
		{1, "1"},
		{-1, "-1"},
		{-999, "-999"},
	}
	for _, tt := range tests {
		result := intToStr(tt.input)
		if result != tt.expected {
			t.Errorf("intToStr(%d) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// TestGrammar_WrapValue_Escaping 验证 wrapValue 对包含反引号/双引号的标识符的转义处理。
func TestGrammar_WrapValue_Escaping(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		input    string
		expected string
	}{
		// WrapColumn("table.col`umn") → wrapValue("col`umn") → 转义反引号
		{"MySQL_backtick_escape", &MySQLGrammar{}, "table.col`umn", "`table`.`col``umn`"},
		// WrapColumn("table.*") → wrapValue("*") → 不引用
		{"MySQL_star", &MySQLGrammar{}, "table.*", "`table`.*"},
		// PostgreSQL 双引号转义
		{"Postgres_quote_escape", &PostgresGrammar{}, `table.col"umn`, `"table"."col""umn"`},
		// SQLite 双引号转义
		{"SQLite_quote_escape", &SQLiteGrammar{}, `table.col"umn`, `"table"."col""umn"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.grammar.WrapColumn(tt.input)
			if result != tt.expected {
				t.Errorf("WrapColumn(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestBuilder_WhereExpression 验证 Where 传入 Expression 值时直接嵌入 SQL。
func TestBuilder_WhereExpression(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g, nil).
		Table("users").
		Where("updated_at", ">", NewExpression("created_at")).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `updated_at` > created_at", sql)
	// Expression 不作为绑定参数
	assertArgs(t, []any{}, args)
}

// TestBuilder_ToTruncateEmptyTable 验证 ToTruncate 未设置表名时返回错误。
func TestBuilder_ToTruncateEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, err := NewBuilder(g, nil).ToTruncate()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestBuilder_ToDeleteEmptyTable 验证 ToDelete 未设置表名时返回错误。
func TestBuilder_ToDeleteEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).ToDelete()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestBuilder_ToInsertInvalidData 验证 ToInsert 传入非结构体时返回错误。
func TestBuilder_ToInsertInvalidData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).Table("t").ToInsert(123)
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestBuilder_ToInsertUsingEmptyTable 验证 ToInsertUsing 未设置表名时返回错误。
func TestBuilder_ToInsertUsingEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).ToInsertUsing([]string{"a"}, func(sub *Builder) {
		sub.Table("t2")
	})
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestDialectGrammar_Unknown 验证未知方言返回错误。
func TestDialectGrammar_Unknown(t *testing.T) {
	_, err := dialectGrammar("oracle")
	if !errors.Is(err, ErrUnknownDialect) {
		t.Errorf("expected ErrUnknownDialect, got %v", err)
	}
}

// TestNewDBDao_UnknownDialect 验证 NewDBDao 传入未知方言时返回错误。
func TestNewDBDao_UnknownDialect(t *testing.T) {
	_, err := NewDBDao(nil, "oracle", nil)
	if !errors.Is(err, ErrUnknownDialect) {
		t.Errorf("expected ErrUnknownDialect, got %v", err)
	}
}

// TestDBDao_CloseNilPool 验证 DBDao 的 pool 为 nil 时 Close 不 panic。
func TestDBDao_CloseNilPool(t *testing.T) {
	d := &DBDao{}
	err := d.Close()
	if err != nil {
		t.Errorf("Close with nil pool should not error, got %v", err)
	}
}

// TestBuilder_CollectJoinBindings_Raw 验证 collectSelectBindings 包含 JOIN Raw 绑定。
func TestBuilder_CollectJoinBindings_Raw(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g, nil).
		Table("users").
		JoinOn("orders", func(jb *JoinBuilder) {
			jb.Raw("orders.amount > ?", 100)
		}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` INNER JOIN `orders` ON orders.amount > ?", sql)
	assertArgs(t, []any{100}, args)
}

// TestBuilder_WhereInEmpty 验证 WhereIn 空切片生成等价 false 条件。
func TestBuilder_WhereInEmpty(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g, nil).
		Table("users").
		WhereIn("id", []any{}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE 0 = 1", sql)
	assertArgs(t, []any{}, args)
}

// TestBuilder_WhereNotInEmpty 验证 WhereNotIn 空切片生成等价 true 条件。
func TestBuilder_WhereNotInEmpty(t *testing.T) {
	g := NewMySQLGrammar()
	sql, args, err := NewBuilder(g, nil).
		Table("users").
		WhereNotIn("id", []any{}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE 1 = 1", sql)
	assertArgs(t, []any{}, args)
}

// ==================== 测试辅助函数 ====================

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertSQL(t *testing.T, expected, actual string) {
	t.Helper()
	if expected != actual {
		t.Errorf("SQL mismatch:\n  expected: %s\n  actual:   %s", expected, actual)
	}
}

func assertArgs(t *testing.T, expected []any, actual []any) {
	t.Helper()
	if len(expected) == 0 && len(actual) == 0 {
		return
	}
	if len(expected) != len(actual) {
		t.Errorf("args count mismatch: expected %d, got %d\n  expected: %v\n  actual:   %v", len(expected), len(actual), expected, actual)
		return
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Errorf("args[%d] mismatch: expected %v (%T), got %v (%T)", i, expected[i], expected[i], actual[i], actual[i])
		}
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// ==================== 覆盖率提升测试（第三轮）====================

// TestBuilder_WhereExpression_AllGrammars 验证三方言 Where Expression 均直接嵌入 SQL。
func TestBuilder_WhereExpression_AllGrammars(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"MySQL", NewMySQLGrammar(), "SELECT * FROM `users` WHERE `updated_at` > created_at"},
		{"Postgres", NewPostgresGrammar(), `SELECT * FROM "users" WHERE "updated_at" > created_at`},
		{"SQLite", NewSQLiteGrammar(), `SELECT * FROM "users" WHERE "updated_at" > created_at`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := NewBuilder(tt.grammar, nil).
				Table("users").
				Where("updated_at", ">", NewExpression("created_at")).
				ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{}, args)
		})
	}
}

// TestGrammar_WrapColumn_AllGrammars 验证三方言 WrapColumn 对 AS 别名的处理。
func TestGrammar_WrapColumn_AllGrammars(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		input    string
		expected string
	}{
		{"MySQL_as_alias", &MySQLGrammar{}, "name AS user_name", "`name` AS `user_name`"},
		{"Postgres_as_alias", &PostgresGrammar{}, "name AS user_name", `"name" AS "user_name"`},
		{"SQLite_as_alias", &SQLiteGrammar{}, "name AS user_name", `"name" AS "user_name"`},
		{"Postgres_table_col", &PostgresGrammar{}, "users.name", `"users"."name"`},
		{"SQLite_table_col", &SQLiteGrammar{}, "users.name", `"users"."name"`},
		{"Postgres_star", &PostgresGrammar{}, "*", "*"},
		{"SQLite_star", &SQLiteGrammar{}, "*", "*"},
		{"Postgres_func", &PostgresGrammar{}, "COUNT(id)", "COUNT(id)"},
		{"SQLite_func", &SQLiteGrammar{}, "COUNT(id)", "COUNT(id)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.grammar.WrapColumn(tt.input)
			if result != tt.expected {
				t.Errorf("WrapColumn(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// TestBuilder_ToUpsertInvalidData 验证 ToUpsert 传入非结构体时返回错误。
func TestBuilder_ToUpsertInvalidData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).Table("t").ToUpsert("not a struct", []string{"id"}, nil)
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestBuilder_ToInsertOrIgnoreInvalidData 验证 ToInsertOrIgnore 传入非结构体时返回错误。
func TestBuilder_ToInsertOrIgnoreInvalidData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := NewBuilder(g, nil).Table("t").ToInsertOrIgnore(123)
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestBuilder_ToUpdateEmptyTable 验证 ToUpdate 未设置表名时返回错误。
func TestBuilder_ToUpdateEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	type d struct {
		Name string `db:"name"`
	}
	_, _, err := NewBuilder(g, nil).ToUpdate(d{Name: "x"})
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestBuilder_ToDeleteWithError 验证 ToDelete 携带累积错误时返回错误。
func TestBuilder_ToDeleteWithError(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("t").Where("id", "EVIL", 1)
	_, _, err := b.ToDelete()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}
}

// TestExtractInsertData_EmptySlice_Builder 验证 extractInsertData 传入空切片时返回错误。
func TestExtractInsertData_EmptySlice_Builder(t *testing.T) {
	type d struct {
		Name string `db:"name"`
	}
	_, _, err := extractInsertData([]d{})
	if !errors.Is(err, ErrEmptyData) {
		t.Errorf("expected ErrEmptyData, got %v", err)
	}
}

// TestExtractInsertData_NonStructSlice_Builder 验证 extractInsertData 传入非结构体切片时返回错误。
func TestExtractInsertData_NonStructSlice_Builder(t *testing.T) {
	_, _, err := extractInsertData([]int{1, 2, 3})
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestExtractUpdateData_NonStruct_Builder 验证 extractUpdateData 传入非结构体时返回错误。
func TestExtractUpdateData_NonStruct_Builder(t *testing.T) {
	_, _, err := extractUpdateData("not a struct")
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}
