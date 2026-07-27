package zcdb

import (
	"errors"
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

// TestSQLiteGrammar_LockSQL 验证 SQLite 方言忽略锁子句：LockForUpdate 和 SharedLock 均不生成锁 SQL。
func TestSQLiteGrammar_LockSQL(t *testing.T) {
	g := &SQLiteGrammar{}

	tests := []struct {
		name     string
		builder  *Builder
		expected string
	}{
		{
			name:     "LockForUpdate_ignored",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).LockForUpdate(),
			expected: "SELECT * FROM \"users\" WHERE \"id\" = ?",
		},
		{
			name:     "SharedLock_ignored",
			builder:  NewBuilder(g, nil).Table("users").Where("id", "=", 1).SharedLock(),
			expected: "SELECT * FROM \"users\" WHERE \"id\" = ?",
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
