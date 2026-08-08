// 本文件为方言无关的 Builder 单元测试（工具函数/错误定义/Grammar 通用行为/Bug 回归等），
// 仅验证编译与内部逻辑，不依赖数据库连接；同时存放各方言单元测试共用的断言 helper 与测试结构体。
package zcdb

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

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
	tableSub := NewBuilder(g, nil).Table("users").Where("age", ">", 25)

	b := NewBuilder(g, nil).
		SelectSub(selectSub, "sub_amount").
		TableSub(tableSub, "u")

	sql, args, err := b.ToSelect()
	assertNoError(t, err)

	// SQL 中 SELECT 子查询的 ? 在前，FROM 子查询的 ? 在后
	expected := "SELECT (SELECT `amount` FROM `orders` WHERE `status` = ?) AS `sub_amount` FROM (SELECT * FROM `users` WHERE `age` > ?) AS `u`"
	assertSQL(t, expected, sql)
	// 绑定顺序应为 ["active", 25]（与 SQL 中 ? 出现顺序一致）
	assertArgs(t, []any{"active", 25}, args)
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
	if len(b.columns) != 2 || b.columns[0].Value != "name" || b.columns[1].Value != "age" {
		t.Errorf("ToCount panic 后 columns 未恢复: expected [name age], got %v", b.columns)
	}
}

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

	// 边界固化（审查结论）：大小写变体行为——全小写（归一化为大写）与全大写被接受，
	// 首字符大写的混合大小写（如 "Like"）不被归一化而是拒绝；拒绝是安全方向，不视为 bug。
	if err := validateOperator("like"); err != nil {
		t.Errorf("全小写 like 应被接受, got %v", err)
	}
	if err := validateOperator("not like"); err != nil {
		t.Errorf("全小写 not like 应归一化后接受, got %v", err)
	}
	if err := validateOperator("NOT LIKE"); err != nil {
		t.Errorf("全大写 NOT LIKE 应被接受, got %v", err)
	}
	if err := validateOperator("Like"); !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("混合大小写 Like 应被拒绝, got %v", err)
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

	// 省略 direction 变参时默认升序 ASC
	t.Run("omitted_defaults_ASC", func(t *testing.T) {
		b := NewBuilder(g, nil).Table("t").OrderBy("col")
		if len(b.orders) != 1 || b.orders[0].Direction != "ASC" {
			t.Errorf("OrderBy without direction: expected ASC, got %v", b.orders)
		}
	})
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

// TestBuilder_TableSubOverridesTable 验证 Table 与 TableSub 互斥且"后调用者生效"：
// 先 TableSub 再 Table 应切回普通表（清除子查询状态）；先 Table 再 TableSub 应使用子查询。
func TestBuilder_TableSubOverridesTable(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name     string
		build    func() *Builder
		expected string
		wantArgs int
	}{
		{
			name: "TableSubAfterTableWins",
			build: func() *Builder {
				sub := NewBuilder(g, nil).Table("orders").Where("amount", ">", 100)
				return NewBuilder(g, nil).Table("users").TableSub(sub, "o")
			},
			expected: "SELECT * FROM (SELECT * FROM `orders` WHERE `amount` > ?) AS `o`",
			wantArgs: 1,
		},
		{
			name: "TableAfterTableSubRevertsToPlainTable",
			build: func() *Builder {
				sub := NewBuilder(g, nil).Table("orders").Where("amount", ">", 100)
				return NewBuilder(g, nil).TableSub(sub, "o").Table("users")
			},
			expected: "SELECT * FROM `users`",
			wantArgs: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			sql, args, err := b.ToSelect()
			assertNoError(t, err)
			if sql != tt.expected {
				t.Errorf("SQL 不匹配：\n期望: %s\n实际: %s", tt.expected, sql)
			}
			if len(args) != tt.wantArgs {
				t.Errorf("绑定参数数量不匹配：期望 %d 个，实际 %v", tt.wantArgs, args)
			}
		})
	}
}

// TestBuilder_CloneDeepCopy 验证 Clone 对各种可选字段的深拷贝（提升 Clone 覆盖率）。
func TestBuilder_CloneDeepCopy(t *testing.T) {
	g := NewMySQLGrammar()

	// 构造包含所有可选字段的 Builder
	tableSub := NewBuilder(g, nil).Table("sub_t")
	selectSub := NewBuilder(g, nil).Table("orders").Select("amount").Where("status", "=", "active")

	b := NewBuilder(g, nil).
		Table("users").
		Select("name", "age").
		Distinct().
		TableSub(tableSub, "f").
		SelectSub(selectSub, "sub_amount").
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

	// 验证 clone 的 tableSub 是独立副本
	clone.tableSub.Where("extra", "=", 1)
	origSQL, _, _ := b.ToSelect()
	if containsStr(origSQL, "extra") {
		t.Error("BUG: Clone 的 tableSub 与原 Builder 共享引用")
	}

	// 验证 clone 的 selectSubs 是独立副本
	clone.selectSubs[0].Query.Where("extra2", "=", 2)
	origSQL2, _, _ := b.ToSelect()
	if containsStr(origSQL2, "extra2") {
		t.Error("BUG: Clone 的 selectSubs 与原 Builder 共享引用")
	}

	// 验证 clone 的 groups 是独立副本
	clone.groups[0].Column = "modified"
	if b.groups[0].Column == "modified" {
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

// TestBug_CloneJoinDeepNesting 审查复现用例：
// JoinBuilder.JoinOn 可递归构造任意深度嵌套 join 组，但 Clone 只深拷贝两层，
// 第三层嵌套的 Conditions/Sub 切片与原 Builder 共享底层数组，违反深拷贝契约。
func TestBug_CloneJoinDeepNesting(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("orders.user_id", "=", "users.id")
		j.JoinOn("items", func(j2 *JoinBuilder) {
			j2.On("items.order_id", "=", "orders.id")
			j2.JoinOn("skus", func(j3 *JoinBuilder) {
				j3.Raw("skus.id = items.sku_id AND skus.warehouse = ?", "WH1")
			})
		})
	})

	// 三层嵌套编译本身应正确（compileJoin 递归支持）
	sqlStr, args, err := b.ToSelect()
	assertNoError(t, err)
	assertSQL(t,
		"SELECT * FROM `users` INNER JOIN (`orders` INNER JOIN (`items` INNER JOIN `skus` ON skus.id = items.sku_id AND skus.warehouse = ?) ON `items`.`order_id` = `orders`.`id`) ON `orders`.`user_id` = `users`.`id`",
		sqlStr)
	assertArgs(t, []any{"WH1"}, args)

	// Clone 后第三层的 Conditions 必须与原 Builder 完全独立
	clone := b.Clone()
	origConds := b.joins[0].Joins[0].Joins[0].Conditions
	cloneConds := clone.joins[0].Joins[0].Joins[0].Conditions
	if len(origConds) != len(cloneConds) || len(origConds) == 0 {
		t.Fatalf("expected non-empty third-level conditions, orig=%d clone=%d", len(origConds), len(cloneConds))
	}
	if &origConds[0] == &cloneConds[0] {
		t.Errorf("BUG: Clone 的第三层 join Conditions 与原 Builder 共享底层数组")
	}
	// 修改 clone 第三层的绑定值，原 Builder 不应受影响
	cloneConds[0].Bindings[0] = "HACKED"
	if b.joins[0].Joins[0].Joins[0].Conditions[0].Bindings[0] == "HACKED" {
		t.Errorf("BUG: 修改 clone 第三层 Bindings 影响了原 Builder")
	}
}

// TestBug_CloneJoinNestedJoins 审查复现用例：
// OnNested 回调内可再调 JoinOn 追加嵌套 join 组（JoinBuilder.Joins），
// 但 cloneJoinConditions 重建 Nested 时只复制 Conditions，Joins 状态被丢弃，
// 违反 Clone 深拷贝契约（克隆后状态与原 Builder 不一致）。
func TestBug_CloneJoinNestedJoins(t *testing.T) {
	g := NewMySQLGrammar()
	b := NewBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
		j.On("orders.user_id", "=", "users.id")
		j.OnNested(func(q *JoinBuilder) {
			q.On("orders.status", "=", "paid")
			q.JoinOn("items", func(q2 *JoinBuilder) {
				q2.On("items.order_id", "=", "orders.id")
			})
		})
	})

	// 前置条件：OnNested 内 JoinOn 应产生 Nested.Joins
	origNested := b.joins[0].Conditions[1].Nested
	if origNested == nil || len(origNested.Joins) == 0 {
		t.Fatalf("前置条件不成立：OnNested 内 JoinOn 应产生 Nested.Joins")
	}

	clone := b.Clone()
	cloneNested := clone.joins[0].Conditions[1].Nested
	if cloneNested == nil {
		t.Fatalf("Clone 丢失了 nested 条件")
	}
	if len(cloneNested.Joins) != len(origNested.Joins) {
		t.Fatalf("BUG: Clone 丢失 nested JoinBuilder 的 Joins：orig=%d clone=%d", len(origNested.Joins), len(cloneNested.Joins))
	}
	// 深层独立：修改 clone 的嵌套 join 条件不影响原 Builder
	cloneNested.Joins[0].Conditions[0].Second = "hacked"
	if origNested.Joins[0].Conditions[0].Second != "orders.id" {
		t.Errorf("BUG: clone 的 nested Joins 与原 Builder 共享底层数组")
	}
}

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

// TestBug_HavingWithExpression 验证 Having/OrHaving 传入 Expression 时直接内嵌 SQL。
// 当前行为（BUG）：compileHavings 的 basic 分支固定生成占位符，
// Expression 被当作绑定参数传入驱动导致执行失败。
func TestBug_HavingWithExpression(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "SELECT * FROM `users` GROUP BY `user_id` HAVING SUM(amount) > 100"},
		{"postgres", NewPostgresGrammar(), "SELECT * FROM \"users\" GROUP BY \"user_id\" HAVING SUM(amount) > 100"},
		{"sqlite", NewSQLiteGrammar(), "SELECT * FROM \"users\" GROUP BY \"user_id\" HAVING SUM(amount) > 100"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := NewBuilder(tt.grammar, nil).
				Table("users").
				GroupBy("user_id").
				Having("SUM(amount)", ">", NewExpression("100")).
				ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{}, args)
		})
	}
}

// TestBug_RawWithExpressionBindings 验证 WhereRaw/HavingRaw/Join.Raw 的绑定参数中含 Expression 时直接内嵌。
// 当前行为（BUG）：raw SQL 原样输出（含 ?），Expression 被当作绑定参数传入驱动导致执行失败。
func TestBug_RawWithExpressionBindings(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		build    func(b *Builder) *Builder
		expected string
	}{
		{
			"whereRaw", NewMySQLGrammar(),
			func(b *Builder) *Builder {
				return b.WhereRaw("amount > ?", NewExpression("100"))
			},
			"SELECT * FROM `users` WHERE amount > 100",
		},
		{
			"havingRaw", NewMySQLGrammar(),
			func(b *Builder) *Builder {
				return b.GroupBy("user_id").HavingRaw("SUM(amount) > ?", NewExpression("100"))
			},
			"SELECT * FROM `users` GROUP BY `user_id` HAVING SUM(amount) > 100",
		},
		{
			"joinRaw", NewMySQLGrammar(),
			func(b *Builder) *Builder {
				return b.JoinOn("orders", func(jb *JoinBuilder) {
					jb.Raw("orders.amount > ?", NewExpression("users.min_amount"))
				})
			},
			"SELECT * FROM `users` INNER JOIN `orders` ON orders.amount > users.min_amount",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.build(NewBuilder(tt.grammar, nil).Table("users")).ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{}, args)
		})
	}
}

// TestBug_ToCountWithDistinct 验证 Distinct + Count 的去重计数。
// 当前行为（BUG）：生成 SELECT DISTINCT COUNT(*)，DISTINCT 对聚合结果无效，返回总行数。
func TestBug_ToCountWithDistinct(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "SELECT COUNT(*) FROM (SELECT DISTINCT `name` FROM `users`) AS `t`"},
		{"postgres", NewPostgresGrammar(), "SELECT COUNT(*) FROM (SELECT DISTINCT \"name\" FROM \"users\") AS \"t\""},
		{"sqlite", NewSQLiteGrammar(), "SELECT COUNT(*) FROM (SELECT DISTINCT \"name\" FROM \"users\") AS \"t\""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := NewBuilder(tt.grammar, nil).
				Table("users").
				Select("name").
				Distinct().
				ToCount()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{}, args)
		})
	}
}

// TestBug_OffsetWithoutLimit 验证仅 Offset 无 Limit 时各方言生成合法 SQL。
// 当前行为（BUG）：MySQL/SQLite 直接输出 OFFSET n（无 LIMIT），数据库报语法错误。
func TestBug_OffsetWithoutLimit(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "SELECT * FROM `users` LIMIT 18446744073709551615 OFFSET 5"},
		{"postgres", NewPostgresGrammar(), "SELECT * FROM \"users\" OFFSET 5"},
		{"sqlite", NewSQLiteGrammar(), "SELECT * FROM \"users\" LIMIT -1 OFFSET 5"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, _, err := NewBuilder(tt.grammar, nil).
				Table("users").
				Offset(5).
				ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
		})
	}
}

// TestBug_CloneHavingsBindings 验证 Clone 后 havings 的 Bindings 相互独立。
// 当前行为（BUG）：Clone 只复制 havings 切片，Bindings 数组仍共享，
// 修改克隆会影响原 Builder（并发复用不安全）。
func TestBug_CloneHavingsBindings(t *testing.T) {
	b := NewBuilder(NewMySQLGrammar(), nil).
		Table("users").
		HavingRaw("SUM(amount) > ?", 100)
	c := b.Clone()

	// 修改克隆的绑定参数
	c.havings[0].Bindings[0] = 999
	if b.havings[0].Bindings[0] != 100 {
		t.Errorf("Clone should deep-copy havings bindings: original changed to %v", b.havings[0].Bindings[0])
	}
}

// TestBug_InsertUpdateNilPointer 验证 Insert/Update 传入 nil 结构体指针时
// 返回 ErrInvalidStruct 而非 panic。
// 当前行为（BUG）：extractInsertData/extractUpdateData 对 nil 指针执行
// v.Elem() 后 v.Type() 直接 panic（reflect: call of Type on zero Value）。
func TestBug_InsertUpdateNilPointer(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}
	var u *User

	// 防 panic：应返回 ErrInvalidStruct
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("extractInsertData panicked on nil pointer: %v", r)
			}
		}()
		_, _, err := extractInsertData(u)
		if !errors.Is(err, ErrInvalidStruct) {
			t.Errorf("extractInsertData: expected ErrInvalidStruct, got %v", err)
		}
	}()

	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("extractUpdateData panicked on nil pointer: %v", r)
			}
		}()
		_, _, err := extractUpdateData(u)
		if !errors.Is(err, ErrInvalidStruct) {
			t.Errorf("extractUpdateData: expected ErrInvalidStruct, got %v", err)
		}
	}()
}

// TestBug_ScanNumericToString 验证数值扫描到 string 字段时转换为数字字符串。
// 当前行为（BUG）：int64 123 经 ConvertibleTo 分支 Convert 为 string，
// 得到字符码 "{" 而不是 "123"。
func TestBug_ScanNumericToString(t *testing.T) {
	var s string
	n := nullSafeField{field: reflect.ValueOf(&s).Elem()}
	if err := n.Scan(int64(123)); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if s != "123" {
		t.Errorf("expected \"123\", got %q", s)
	}
}

// TestBug_ScanByteSliceToIntSlice 验证 []byte 扫描到非字节切片目标（如 []int）时不 panic。
// 当前行为（BUG）：reflect.Copy 元素类型不匹配直接 panic。
func TestBug_ScanByteSliceToIntSlice(t *testing.T) {
	var arr []int
	n := nullSafeField{field: reflect.ValueOf(&arr).Elem()}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Scan panicked on []byte → []int: %v", r)
		}
	}()
	if err := n.Scan([]byte("[1,2,3]")); err != nil {
		t.Fatalf("Scan error: %v", err)
	}
	if len(arr) != 3 || arr[0] != 1 || arr[2] != 3 {
		t.Errorf("expected [1 2 3], got %v", arr)
	}
}

// TestBug_ForceDeleteUpdateProtection 验证 Force 标记、Clone 传播，
// 以及编译层 ToDelete/ToUpdate 不受执行层保护影响。
func TestBug_ForceDeleteUpdateProtection(t *testing.T) {
	// Force 标记 + Clone 传播
	b := NewBuilder(NewMySQLGrammar(), nil).Table("users").Force()
	if !b.force {
		t.Error("Force should set force flag")
	}
	c := b.Clone()
	if !c.force {
		t.Error("Clone should propagate force flag")
	}

	// 编译层不受保护影响：无 WHERE 仍能生成 DELETE/UPDATE SQL（执行层才校验）
	sqlStr, _, err := b.ToDelete()
	assertNoError(t, err)
	assertSQL(t, "DELETE FROM `users`", sqlStr)

	type updateData struct {
		Name string `db:"name"`
	}
	sqlStr, _, err = b.ToUpdate(updateData{Name: "x"})
	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `name` = ?", sqlStr)
}

// TestBug_HasEffectiveWhere 验证 hasEffectiveWhere 对空嵌套的识别：
// WhereNested 空回调编译后无 WHERE，不能作为有效条件绕过无 WHERE 保护。
func TestBug_HasEffectiveWhere(t *testing.T) {
	// 无任何条件
	b := NewBuilder(NewMySQLGrammar(), nil)
	if b.hasEffectiveWhere() {
		t.Error("empty wheres should not have effective where")
	}

	// 普通条件
	b = NewBuilder(NewMySQLGrammar(), nil).Where("id", "=", 1)
	if !b.hasEffectiveWhere() {
		t.Error("basic where should be effective")
	}

	// 空嵌套：应视为无有效条件
	b = NewBuilder(NewMySQLGrammar(), nil).WhereNested(func(q *Builder) {})
	if b.hasEffectiveWhere() {
		t.Error("empty nested where should not be effective")
	}

	// 嵌套含有效条件：应视为有效
	b = NewBuilder(NewMySQLGrammar(), nil).WhereNested(func(q *Builder) {
		q.Where("id", ">", 10)
	})
	if !b.hasEffectiveWhere() {
		t.Error("nested with conditions should be effective")
	}

	// 空 JOIN（无 ON/Where 条件）：不应视为有效限定
	b = NewBuilder(NewMySQLGrammar(), nil).JoinOn("profiles", func(jb *JoinBuilder) {})
	if b.hasEffectiveJoin() {
		t.Error("empty join should not be effective")
	}
	if b.hasEffectiveWhere() || b.hasEffectiveJoin() {
		t.Error("empty join should not bypass protection")
	}

	// 带 ON 条件的 JOIN：应视为有效限定（UPDATE/DELETE JOIN 场景）
	b = NewBuilder(NewMySQLGrammar(), nil).JoinOn("profiles", func(jb *JoinBuilder) {
		jb.On("users.id", "=", "profiles.user_id")
	})
	if !b.hasEffectiveJoin() {
		t.Error("join with on condition should be effective")
	}
}

// TestBug_ToExistsSQL 验证 ToExists 生成 SELECT 1 ... LIMIT 1
// （而非 COUNT(*) 全表计数），且不破坏原 Builder 状态。
func TestBug_ToExistsSQL(t *testing.T) {
	// MySQL
	b := NewBuilder(NewMySQLGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, args, err := b.ToExists()
	assertNoError(t, err)
	assertSQL(t, "SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1", sqlStr)
	if len(args) != 1 || args[0] != 1 {
		t.Errorf("expected args [1], got %v", args)
	}

	// SQLite
	b = NewBuilder(NewSQLiteGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, _, err = b.ToExists()
	assertNoError(t, err)
	assertSQL(t, `SELECT 1 FROM "users" WHERE "id" = ? LIMIT 1`, sqlStr)

	// PostgreSQL
	b = NewBuilder(NewPostgresGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, args, err = b.ToExists()
	assertNoError(t, err)
	assertSQL(t, `SELECT 1 FROM "users" WHERE "id" = $1 LIMIT 1`, sqlStr)
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %v", args)
	}

	// UNION：整个 UNION 包裹为子查询后附加 LIMIT 1
	g := NewMySQLGrammar()
	union := NewBuilder(g, nil).Table("admins").Where("id", ">", 2)
	b = NewBuilder(g, nil).Table("users").Where("id", ">", 1).Union(union)
	sqlStr, _, err = b.ToExists()
	assertNoError(t, err)
	if !strings.Contains(sqlStr, "SELECT 1 FROM (") || !strings.Contains(sqlStr, "LIMIT 1") {
		t.Errorf("expected subquery wrapped union with LIMIT 1, got: %s", sqlStr)
	}

	// 状态恢复：ToExists 不应破坏原 Builder 的分页/列/锁状态
	b = NewBuilder(NewMySQLGrammar(), nil).Table("users").
		Select("name").Where("id", ">", 1).ForPage(2, 10).OrderBy("id", "DESC").LockForUpdate()
	_, _, err = b.ToExists()
	assertNoError(t, err)
	if b.limit != 10 || b.offset != 10 {
		t.Errorf("limit/offset should be restored, got %d/%d", b.limit, b.offset)
	}
	if len(b.columns) != 1 || b.columns[0].Value != "name" {
		t.Errorf("columns should be restored, got %v", b.columns)
	}
	if len(b.orders) != 1 || b.lockClause == "" {
		t.Errorf("orders/lockClause should be restored, got orders=%v lock=%q", b.orders, b.lockClause)
	}
}

// TestBug_SelectStarNotWrapped 验证星号不被标识符包裹：
// Select("*") 走 WrapColumn 的星号分支；Select("users.*") 的列名部分经 wrapValue，
// 星号同样不能加引号（否则生成 `"users"."*"` 导致 SQL 语法错误）。
func TestBug_SelectStarNotWrapped(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		column   string
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "*", "SELECT * FROM `users`"},
		{"postgres", NewPostgresGrammar(), "*", `SELECT * FROM "users"`},
		{"sqlite", NewSQLiteGrammar(), "*", `SELECT * FROM "users"`},
		{"mysql-qualified", NewMySQLGrammar(), "users.*", "SELECT `users`.* FROM `users`"},
		{"postgres-qualified", NewPostgresGrammar(), "users.*", `SELECT "users".* FROM "users"`},
		{"sqlite-qualified", NewSQLiteGrammar(), "users.*", `SELECT "users".* FROM "users"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlStr, _, err := NewBuilder(tt.grammar, nil).Table("users").Select(tt.column).ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sqlStr)
		})
	}
}

// TestBug_JoinSub_SQL 验证 JOIN 派生表（子查询）的 SQL 生成与绑定参数顺序：
// 子查询 IN 参数位于 ON value 参数之前、外层 WHERE 参数之前；
// PostgreSQL 的 $N 占位符必须连续递增（子查询 $1/$2 → ON $3 → WHERE $4/$5）。
func TestBug_JoinSub_SQL(t *testing.T) {
	codes := []any{"A", "B"}
	buildSub := func(g Grammar) *Builder {
		return NewBuilder(g, nil).Table("fund_net_value").
			Select("fund_code", "MAX(ed) AS ed").
			WhereIn("fund_code", codes).
			GroupBy("fund_code")
	}
	buildQuery := func(g Grammar) *Builder {
		return NewBuilder(g, nil).Table("fund_net_value AS t1").
			Select("t1.*").
			JoinSub(buildSub(g), "t2", func(j *JoinBuilder) {
				j.On("t1.fund_code", "=", "t2.fund_code").
					On("t1.ed", "=", "t2.ed")
			}).
			WhereIn("t1.fund_code", codes)
	}

	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "SELECT `t1`.* FROM `fund_net_value` AS `t1` INNER JOIN (SELECT `fund_code`, MAX(ed) AS ed FROM `fund_net_value` WHERE `fund_code` IN (?, ?) GROUP BY `fund_code`) AS `t2` ON `t1`.`fund_code` = `t2`.`fund_code` AND `t1`.`ed` = `t2`.`ed` WHERE `t1`.`fund_code` IN (?, ?)"},
		{"postgres", NewPostgresGrammar(), `SELECT "t1".* FROM "fund_net_value" AS "t1" INNER JOIN (SELECT "fund_code", MAX(ed) AS ed FROM "fund_net_value" WHERE "fund_code" IN ($1, $2) GROUP BY "fund_code") AS "t2" ON "t1"."fund_code" = "t2"."fund_code" AND "t1"."ed" = "t2"."ed" WHERE "t1"."fund_code" IN ($3, $4)`},
		{"sqlite", NewSQLiteGrammar(), `SELECT "t1".* FROM "fund_net_value" AS "t1" INNER JOIN (SELECT "fund_code", MAX(ed) AS ed FROM "fund_net_value" WHERE "fund_code" IN (?, ?) GROUP BY "fund_code") AS "t2" ON "t1"."fund_code" = "t2"."fund_code" AND "t1"."ed" = "t2"."ed" WHERE "t1"."fund_code" IN (?, ?)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlStr, args, err := buildQuery(tt.grammar).ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sqlStr)
			assertArgs(t, []any{"A", "B", "A", "B"}, args)
		})
	}
}

// TestBug_CrossJoinSub_SQL 验证 CrossJoinSub：CROSS JOIN 派生表（无 ON 条件），
// FROM 子查询与 JOIN 派生表子查询的绑定参数按 SQL 文本顺序收集。
func TestBug_CrossJoinSub_SQL(t *testing.T) {
	codes := []any{"店A", "店B"}
	buildSub := func(g Grammar) *Builder {
		return NewBuilder(g, nil).Table("sales").
			Select("store_name").
			Distinct().
			WhereIn("store_name", codes)
	}
	buildQuery := func(g Grammar) *Builder {
		m := NewBuilder(g, nil).Table("sales").Select("month").Distinct()
		return NewBuilder(g, nil).TableSub(m, "m").
			Select("m.month", "s.store_name").
			CrossJoinSub(buildSub(g), "s")
	}

	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "SELECT `m`.`month`, `s`.`store_name` FROM (SELECT DISTINCT `month` FROM `sales`) AS `m` CROSS JOIN (SELECT DISTINCT `store_name` FROM `sales` WHERE `store_name` IN (?, ?)) AS `s`"},
		{"postgres", NewPostgresGrammar(), `SELECT "m"."month", "s"."store_name" FROM (SELECT DISTINCT "month" FROM "sales") AS "m" CROSS JOIN (SELECT DISTINCT "store_name" FROM "sales" WHERE "store_name" IN ($1, $2)) AS "s"`},
		{"sqlite", NewSQLiteGrammar(), `SELECT "m"."month", "s"."store_name" FROM (SELECT DISTINCT "month" FROM "sales") AS "m" CROSS JOIN (SELECT DISTINCT "store_name" FROM "sales" WHERE "store_name" IN (?, ?)) AS "s"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlStr, args, err := buildQuery(tt.grammar).ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sqlStr)
			assertArgs(t, []any{"店A", "店B"}, args)
		})
	}
}

// TestBug_Pluck_ArgValidation 验证 Pluck 参数校验错误路径：
// dest 必须是非 nil 的切片/map 指针，且列数与目标容器匹配（切片 1 列、map 2 列）。
func TestBug_Pluck_ArgValidation(t *testing.T) {
	b := NewBuilder(NewSQLiteGrammar(), nil)
	ctx := context.Background()

	// 非指针 dest
	var names []string
	if err := b.Pluck(ctx, names, "name"); !errors.Is(err, ErrPluckDest) {
		t.Errorf("non-pointer dest: expected ErrPluckDest, got %v", err)
	}
	// nil 指针 dest
	var p *[]string
	if err := b.Pluck(ctx, p, "name"); !errors.Is(err, ErrPluckDest) {
		t.Errorf("nil pointer dest: expected ErrPluckDest, got %v", err)
	}
	// 结构体指针 dest
	var s struct{ Name string }
	if err := b.Pluck(ctx, &s, "name"); !errors.Is(err, ErrPluckDest) {
		t.Errorf("struct dest: expected ErrPluckDest, got %v", err)
	}
	// 切片 dest + 2 列
	if err := b.Pluck(ctx, &names, "name", "id"); !errors.Is(err, ErrPluckColumns) {
		t.Errorf("slice dest with 2 columns: expected ErrPluckColumns, got %v", err)
	}
	// 切片 dest + 0 列
	if err := b.Pluck(ctx, &names); !errors.Is(err, ErrPluckColumns) {
		t.Errorf("slice dest with 0 columns: expected ErrPluckColumns, got %v", err)
	}
	// map dest + 1 列
	var m map[int64]string
	if err := b.Pluck(ctx, &m, "name"); !errors.Is(err, ErrPluckColumns) {
		t.Errorf("map dest with 1 column: expected ErrPluckColumns, got %v", err)
	}

	// keyBy 模式：map 值结构体 + 0 列
	type User struct {
		Name string `db:"name"`
		Id   int    `db:"id"`
	}
	var mk map[int64]User
	if err := b.Pluck(ctx, &mk); !errors.Is(err, ErrPluckColumns) {
		t.Errorf("keyBy dest with 0 columns: expected ErrPluckColumns, got %v", err)
	}
	// keyBy 模式：map 值结构体 + 2 列
	if err := b.Pluck(ctx, &mk, "id", "name"); !errors.Is(err, ErrPluckColumns) {
		t.Errorf("keyBy dest with 2 columns: expected ErrPluckColumns, got %v", err)
	}
	// map 值类型非结构体（如嵌套 map）走标量键值对模式：列数仍要求 2 列
	var mn map[int64]map[string]int
	if err := b.Pluck(ctx, &mn, "a"); !errors.Is(err, ErrPluckColumns) {
		t.Errorf("nested map value dest with 1 column: expected ErrPluckColumns, got %v", err)
	}
	// keyBy 模式：结构体无导出字段
	var me map[int64]struct{}
	if err := b.Pluck(ctx, &me, "id"); !errors.Is(err, ErrNoFields) {
		t.Errorf("keyBy dest with empty struct: expected ErrNoFields, got %v", err)
	}
}

// TestBug_JoinSub_CloneIsolation 验证 Clone 对 JOIN 派生表子查询做深拷贝：
// 修改克隆体的子查询不影响原 Builder 的 SQL 生成。
func TestBug_JoinSub_CloneIsolation(t *testing.T) {
	sub := NewBuilder(NewMySQLGrammar(), nil).Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		GroupBy("fund_code")

	b := NewBuilder(NewMySQLGrammar(), nil).Table("fund_net_value AS t1").
		Select("t1.*").
		JoinSub(sub, "t2", func(j *JoinBuilder) {
			j.On("t1.fund_code", "=", "t2.fund_code")
		})
	origSQL, _, err := b.ToSelect()
	assertNoError(t, err)

	clone := b.Clone()
	// 修改克隆体的子查询：追加 WHERE 条件
	clone.joins[0].Sub.Where("fund_code", "=", "X")

	// 原 Builder 不应受影响
	afterSQL, afterArgs, err := b.ToSelect()
	assertNoError(t, err)
	assertSQL(t, origSQL, afterSQL)
	for _, a := range afterArgs {
		if a == "X" {
			t.Errorf("original builder args should not contain X, got %v", afterArgs)
		}
	}

	// 克隆体 SQL 应包含新增条件（值为绑定参数，出现在 args 中）
	cloneSQL, cloneArgs, err := clone.ToSelect()
	assertNoError(t, err)
	if !strings.Contains(cloneSQL, "`fund_code` = ?") {
		t.Errorf("clone SQL should contain new where condition, got: %s", cloneSQL)
	}
	foundX := false
	for _, a := range cloneArgs {
		if a == "X" {
			foundX = true
		}
	}
	if !foundX {
		t.Errorf("clone args should contain X, got %v", cloneArgs)
	}
}

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
		// 审查复现用例：带点号的 "表.列 AS 别名"，点号分支不应吞掉 AS 别名
		{"MySQL_table_col_as_alias", &MySQLGrammar{}, "users.name AS n", "`users`.`name` AS `n`"},
		{"Postgres_table_col_as_alias", &PostgresGrammar{}, "users.name AS n", `"users"."name" AS "n"`},
		{"SQLite_table_col_as_alias", &SQLiteGrammar{}, "users.name AS n", `"users"."name" AS "n"`},
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

// TestSelectRaw_BypassWrapColumn 验证 SelectRaw 的表达式不经过 WrapColumn 引用，直接嵌入 SQL。
func TestSelectRaw_BypassWrapColumn(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		builder  *Builder
		expected string
	}{
		{
			name:     "MySQL_SelectRaw_数字字面量",
			grammar:  &MySQLGrammar{},
			builder:  NewBuilder(&MySQLGrammar{}, nil).Table("users").SelectRaw("1"),
			expected: "SELECT 1 FROM `users`",
		},
		{
			name:     "MySQL_SelectRaw_算术表达式",
			grammar:  &MySQLGrammar{},
			builder:  NewBuilder(&MySQLGrammar{}, nil).Table("users").SelectRaw("age + 1 AS age_plus"),
			expected: "SELECT age + 1 AS age_plus FROM `users`",
		},
		{
			name:     "MySQL_SelectRaw_混合普通列",
			grammar:  &MySQLGrammar{},
			builder:  NewBuilder(&MySQLGrammar{}, nil).Table("users").Select("name", "age").SelectRaw("1"),
			expected: "SELECT `name`, `age`, 1 FROM `users`",
		},
		{
			name:     "PostgreSQL_SelectRaw_数字字面量",
			grammar:  &PostgresGrammar{},
			builder:  NewBuilder(&PostgresGrammar{}, nil).Table("users").SelectRaw("1"),
			expected: `SELECT 1 FROM "users"`,
		},
		{
			name:     "PostgreSQL_SelectRaw_混合普通列",
			grammar:  &PostgresGrammar{},
			builder:  NewBuilder(&PostgresGrammar{}, nil).Table("users").Select("name").SelectRaw("1"),
			expected: `SELECT "name", 1 FROM "users"`,
		},
		{
			name:     "SQLite_SelectRaw_数字字面量",
			grammar:  &SQLiteGrammar{},
			builder:  NewBuilder(&SQLiteGrammar{}, nil).Table("users").SelectRaw("1"),
			expected: `SELECT 1 FROM "users"`,
		},
		{
			name:     "SQLite_SelectRaw_混合普通列",
			grammar:  &SQLiteGrammar{},
			builder:  NewBuilder(&SQLiteGrammar{}, nil).Table("users").Select("name").SelectRaw("1"),
			expected: `SELECT "name", 1 FROM "users"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sqlStr := tt.grammar.CompileSelect(tt.builder, tt.builder.columns)
			if sqlStr != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, sqlStr)
			}
		})
	}
}

// TestSelectRaw_ColumnOrder 验证 Select 和 SelectRaw 混合调用时列顺序保持正确。
func TestSelectRaw_ColumnOrder(t *testing.T) {
	g := &MySQLGrammar{}
	b := NewBuilder(g, nil).Table("users").
		Select("name", "age").
		SelectRaw("COUNT(*) AS cnt").
		SelectRaw("1")

	sqlStr := g.CompileSelect(b, b.columns)
	expected := "SELECT `name`, `age`, COUNT(*) AS cnt, 1 FROM `users`"
	if sqlStr != expected {
		t.Errorf("expected %q, got %q", expected, sqlStr)
	}
}

// TestSelectRaw_ClonePreservesRawFlag 验证 Clone 后 SelectColumn 的 Raw 标志被正确复制。
func TestSelectRaw_ClonePreservesRawFlag(t *testing.T) {
	g := &MySQLGrammar{}
	b := NewBuilder(g, nil).Table("users").Select("name").SelectRaw("1")
	clone := b.Clone()

	if len(clone.columns) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(clone.columns))
	}
	if clone.columns[0].Raw {
		t.Error("first column should not be raw")
	}
	if !clone.columns[1].Raw {
		t.Error("second column should be raw")
	}

	// 修改克隆体不影响原始
	clone.columns[1] = SelectColumn{Value: "2", Raw: true}
	if b.columns[1].Value != "1" {
		t.Error("original should not be affected by clone modification")
	}
}

// TestNewApi_WhereNotAllNoneCompile 验证 WhereNot/All/Any/None 的括号/NOT 编译形态（三方言通用，以 MySQL 为例）。
func TestNewApi_WhereNotAllNoneCompile(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name    string
		builder func() *Builder
		sql     string
		args    []any
	}{
		{"WhereNot", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNot(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, "SELECT * FROM `users` WHERE NOT (`status` = ?)", []any{"active"}},
		{"OrWhereNot", func() *Builder {
			return NewBuilder(g, nil).Table("users").
				Where("id", "=", 1).
				OrWhereNot(func(q *Builder) { q.Where("age", ">", 18) })
		}, "SELECT * FROM `users` WHERE `id` = ? OR NOT (`age` > ?)", []any{1, 18}},
		{"WhereAll", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereAll(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE (`a` = ? AND `b` = ?)", []any{1, 2}},
		{"WhereAny", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereAny(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE (`a` = ? OR `b` = ?)", []any{1, 2}},
		{"WhereNone", func() *Builder {
			return NewBuilder(g, nil).Table("users").WhereNone(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE NOT (`a` = ? OR `b` = ?)", []any{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.sql, sql)
			assertArgs(t, tt.args, args)
		})
	}
}

// TestNewApi_HavingCompile 验证 Having 两参简写/HavingNested/HavingNull 的编译形态。
func TestNewApi_HavingCompile(t *testing.T) {
	g := NewMySQLGrammar()

	tests := []struct {
		name    string
		builder func() *Builder
		sql     string
		args    []any
	}{
		{"HavingShorthand", func() *Builder {
			return NewBuilder(g, nil).Table("users").SelectRaw("status, COUNT(*) AS cnt").
				GroupBy("status").Having("cnt", 5)
		}, "SELECT status, COUNT(*) AS cnt FROM `users` GROUP BY `status` HAVING `cnt` = ?", []any{5}},
		{"HavingNested", func() *Builder {
			return NewBuilder(g, nil).Table("orders").GroupBy("user_id").
				HavingNested(func(q *Builder) {
					q.Having("total", ">", 100).Having("count", "<", 10)
				})
		}, "SELECT * FROM `orders` GROUP BY `user_id` HAVING (`total` > ? AND `count` < ?)", []any{100, 10}},
		{"OrHavingNested", func() *Builder {
			return NewBuilder(g, nil).Table("orders").GroupBy("user_id").
				Having("total", ">", 250).
				OrHavingNested(func(q *Builder) { q.Having("total", "=", 30) })
		}, "SELECT * FROM `orders` GROUP BY `user_id` HAVING `total` > ? OR (`total` = ?)", []any{250, 30}},
		{"HavingNull", func() *Builder {
			return NewBuilder(g, nil).Table("users").GroupBy("dept_id").HavingNull("email")
		}, "SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NULL", nil},
		{"HavingNotNullMulti", func() *Builder {
			return NewBuilder(g, nil).Table("users").GroupBy("dept_id").HavingNotNull("email", "age")
		}, "SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NOT NULL AND `age` IS NOT NULL", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := tt.builder().ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.sql, sql)
			assertArgs(t, tt.args, args)
		})
	}
}

// TestNewApi_ToAggregate 验证 ToAggregate 编译形态、UNION 包裹与非法聚合错误。
func TestNewApi_ToAggregate(t *testing.T) {
	t.Run("MySQL", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").ToAggregate("MAX", "age")
		assertNoError(t, err)
		assertSQL(t, "SELECT MAX(`age`) AS `aggregate` FROM `users`", sql)
		assertArgs(t, nil, args)
	})

	t.Run("Postgres", func(t *testing.T) {
		g := NewPostgresGrammar()
		sql, _, err := NewBuilder(g, nil).Table("users").ToAggregate("MIN", "age")
		assertNoError(t, err)
		assertSQL(t, `SELECT MIN("age") AS "aggregate" FROM "users"`, sql)
	})

	t.Run("SQLite", func(t *testing.T) {
		g := NewSQLiteGrammar()
		sql, _, err := NewBuilder(g, nil).Table("users").ToAggregate("AVG", "age")
		assertNoError(t, err)
		assertSQL(t, `SELECT AVG("age") AS "aggregate" FROM "users"`, sql)
	})

	t.Run("WithWhere", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").
			Where("status", "=", "active").ToAggregate("SUM", "age")
		assertNoError(t, err)
		assertSQL(t, "SELECT SUM(`age`) AS `aggregate` FROM `users` WHERE `status` = ?", sql)
		assertArgs(t, []any{"active"}, args)
	})

	t.Run("UnionWrap", func(t *testing.T) {
		g := NewMySQLGrammar()
		b := NewBuilder(g, nil).Table("orders_a").
			Union(NewBuilder(g, nil).Table("orders_b"))
		sql, _, err := b.ToAggregate("SUM", "amount")
		assertNoError(t, err)
		assertSQL(t,
			"SELECT SUM(`amount`) AS `aggregate` FROM ((SELECT * FROM `orders_a`) UNION (SELECT * FROM `orders_b`)) AS `t`",
			sql)
	})

	t.Run("InvalidAggregate", func(t *testing.T) {
		g := NewMySQLGrammar()
		_, _, err := NewBuilder(g, nil).Table("users").ToAggregate("COUNT", "age")
		if !errors.Is(err, ErrInvalidAggregate) {
			t.Errorf("expected ErrInvalidAggregate, got %v", err)
		}
	})

	t.Run("StateRestored", func(t *testing.T) {
		// 编译后 Builder 状态应恢复，不影响后续 ToSelect
		g := NewMySQLGrammar()
		b := NewBuilder(g, nil).Table("users").Select("name").Limit(10)
		_, _, err := b.ToAggregate("MAX", "age")
		assertNoError(t, err)
		sql, _, err := b.ToSelect()
		assertNoError(t, err)
		assertSQL(t, "SELECT `name` FROM `users` LIMIT 10", sql)
	})
}

// TestNewApi_ToIncDec 验证 ToIncrement/ToDecrement 编译形态与 JOIN 绑定顺序方言差异。
func TestNewApi_ToIncDec(t *testing.T) {
	t.Run("MySQL_NoJoin", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("wallets").
			Where("id", "=", 1).
			ToIncrement([]string{"balance", "points"}, []any{10, 5})
		assertNoError(t, err)
		assertSQL(t, "UPDATE `wallets` SET `balance` = `balance` + ?, `points` = `points` + ? WHERE `id` = ?", sql)
		assertArgs(t, []any{10, 5, 1}, args)
	})

	t.Run("MySQL_JoinSetAfterJoin", func(t *testing.T) {
		// MySQL：JOIN → SET → WHERE 绑定顺序
		g := NewMySQLGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").
			Join("orders", "users.id", "=", "orders.user_id").
			Where("orders.amount", ">", 100).
			ToIncrement([]string{"age"}, []any{1})
		assertNoError(t, err)
		assertSQL(t,
			"UPDATE `users` INNER JOIN `orders` ON `users`.`id` = `orders`.`user_id` SET `age` = `age` + ? WHERE `orders`.`amount` > ?",
			sql)
		assertArgs(t, []any{1, 100}, args)
	})

	t.Run("Postgres_SetBeforeJoin", func(t *testing.T) {
		// PG：SET → JOIN(FROM) → WHERE 绑定顺序，$N 自动转换
		g := NewPostgresGrammar()
		sql, args, err := NewBuilder(g, nil).Table("users").
			Join("orders", "users.id", "=", "orders.user_id").
			Where("orders.amount", ">", 100).
			ToIncrement([]string{"age"}, []any{1})
		assertNoError(t, err)
		assertSQL(t,
			`UPDATE "users" SET "age" = "age" + $1 FROM "orders" WHERE "users"."id" = "orders"."user_id" AND "orders"."amount" > $2`,
			sql)
		assertArgs(t, []any{1, 100}, args)
	})

	t.Run("Decrement", func(t *testing.T) {
		g := NewSQLiteGrammar()
		sql, args, err := NewBuilder(g, nil).Table("wallets").
			Where("id", "=", 1).
			ToDecrement([]string{"balance"}, []any{30})
		assertNoError(t, err)
		assertSQL(t, `UPDATE "wallets" SET "balance" = "balance" - ? WHERE "id" = ?`, sql)
		assertArgs(t, []any{30, 1}, args)
	})

	t.Run("ColumnsMismatch", func(t *testing.T) {
		g := NewMySQLGrammar()
		_, _, err := NewBuilder(g, nil).Table("wallets").
			ToIncrement([]string{"balance"}, []any{10, 5})
		if !errors.Is(err, ErrIncrementColumns) {
			t.Errorf("expected ErrIncrementColumns, got %v", err)
		}
		_, _, err = NewBuilder(g, nil).Table("wallets").ToIncrement(nil, nil)
		if !errors.Is(err, ErrIncrementColumns) {
			t.Errorf("expected ErrIncrementColumns, got %v", err)
		}
	})
}

// TestNewApi_ToDeleteJoin 验证 ToDeleteJoin 的校验错误路径（编译形态已由集成测试覆盖）。
func TestNewApi_ToDeleteJoin(t *testing.T) {
	g := NewMySQLGrammar()

	// 无 JOIN → ErrDeleteJoinNoJoin
	_, _, err := NewBuilder(g, nil).Table("users").Where("id", "=", 1).ToDeleteJoin()
	if !errors.Is(err, ErrDeleteJoinNoJoin) {
		t.Errorf("expected ErrDeleteJoinNoJoin, got %v", err)
	}

	// 无表名 → ErrEmptyTable
	_, _, err = NewBuilder(g, nil).Join("orders", "a", "=", "b").ToDeleteJoin()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestNewApi_WhereShorthandInvalid 验证 Where 三参形式的非法运算符错误。
func TestNewApi_WhereShorthandInvalid(t *testing.T) {
	g := NewMySQLGrammar()

	// 三参形式 op 非 string → ErrInvalidOperator
	_, _, err := NewBuilder(g, nil).Table("users").Where("age", 25, 30).ToSelect()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}

	// 三参形式非法运算符 → ErrInvalidOperator
	_, _, err = NewBuilder(g, nil).Table("users").Where("age", "DROP", 30).ToSelect()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}
}

// TestNewApi_SelectSubCompile 验证 SelectSub 标量子查询列的编译形态（PG $N）。
func TestNewApi_SelectSubCompile(t *testing.T) {
	t.Run("Postgres", func(t *testing.T) {
		g := NewPostgresGrammar()
		sub := NewBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").WhereRaw("orders.user_id = users.id")
		sql, _, err := NewBuilder(g, nil).Table("users").
			Select("id").SelectSub(sub, "order_count").ToSelect()
		assertNoError(t, err)
		assertSQL(t,
			`SELECT "id", (SELECT COUNT(*) FROM "orders" WHERE orders.user_id = users.id) AS "order_count" FROM "users"`,
			sql)
	})

	t.Run("MySQL", func(t *testing.T) {
		g := NewMySQLGrammar()
		sub := NewBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").Where("amount", ">", 100)
		sql, args, err := NewBuilder(g, nil).Table("users").
			Select("id").SelectSub(sub, "order_count").ToSelect()
		assertNoError(t, err)
		assertSQL(t,
			"SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `amount` > ?) AS `order_count` FROM `users`",
			sql)
		assertArgs(t, []any{100}, args)
	})
}
