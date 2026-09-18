// 本文件为方言无关的 Builder 单元测试（工具函数/错误定义/Grammar 通用行为/Bug 回归等），
// 仅验证编译与内部逻辑，不依赖数据库连接；同时存放各方言单元测试共用的断言 helper 与测试结构体。
package zcdb

import (
	"context"
	"errors"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
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

// TestNormalizeSQLComment 验证精确规范化、两条入口一致、星号/斜杠/反斜杠/控制字符替换、Unicode 截断及幂等性。
func TestNormalizeSQLComment(t *testing.T) {
	tests := []struct{ name, input, want string }{
		{"empty", "", ""},
		{"whitespace", " \t\r\n ", ""},
		{"cleaned_empty", "*\x00\r\n", ""},
		{"slash_only", "/", ""},
		{"backslash_only", "\\", ""},
		{"delimiters_only", "*//*\\", ""},
		{"trim", " app:user-service ", "app:user-service"},
		{"unicode_placeholders", "普通中文 trace:? $1", "普通中文 trace:? $1"},
		{"overlapping_terminator", "**// + 41 -- ", "+ 41 --"},
		{"nested_opener", "a /* b", "a    b"},
		{"controls", "a\x00b\r\nc", "a b  c"},
		{"tab_escape", "a\tb\x1bc", "a b c"},
		{"invalid_utf8", "a\xffb", "a\ufffdb"},
		{"truncated_utf8", "a\xe4\xb8", "a\ufffd\ufffd"},
		{"terminator", "*/ + 41 --", "+ 41 --"},
		{"repeated_delimiters", "a */ */ /* b", "a" + strings.Repeat(" ", 10) + "b"},
		{"executable", "/*! + 41 */", "! + 41"},
		{"hint", "/*+ test */", "+ test"},
		{"leading_bang", "! + 41", "! + 41"},
		{"leading_plus", "+ 41", "+ 41"},
		{"quotes_placeholders", "svc:api 'name' \"value\"; ? $1", "svc:api 'name' \"value\"; ? $1"},
		{"path_flattened", "svc:/api/v1/trace", "svc: api v1 trace"},
		{"windows_path_flattened", "C:\\svc\\api", "C: svc api"},
		{"escape_sequence", "a\\nb", "a nb"},
		{"internal_spaces", "  a  b  ", "a  b"},
		{"unicode_spaces", "\u3000a\u00a0b\u3000", "a\u00a0b"},
		{"unicode_non_controls", "a\u2028b\u2029c\u200bd", "a\u2028b\u2029c\u200bd"},
		{"ascii_254", strings.Repeat("a", 254), strings.Repeat("a", 254)},
		{"ascii_255", strings.Repeat("a", 255), strings.Repeat("a", 255)},
		{"ascii_256", strings.Repeat("a", 256), strings.Repeat("a", 255)},
		{"ascii_300", strings.Repeat("a", 300), strings.Repeat("a", 255)},
		{"chinese_256", strings.Repeat("中", 256), strings.Repeat("中", 255)},
		{"four_byte_rune", strings.Repeat("a", 254) + "\U00020000z", strings.Repeat("a", 254) + "\U00020000"},
		{"trim_after_truncate", strings.Repeat("a", 254) + " b", strings.Repeat("a", 254)},
		{"control_at_cutoff", strings.Repeat("a", 254) + "\x1bb", strings.Repeat("a", 254)},
		{"slash_at_cutoff", strings.Repeat("a", 254) + "/b", strings.Repeat("a", 254)},
		{"backslash_at_cutoff", strings.Repeat("a", 254) + "\\b", strings.Repeat("a", 254)},
		{"trim_before_truncate", "  " + strings.Repeat("中", 256), strings.Repeat("中", 255)},
		{"invalid_at_cutoff", strings.Repeat("a", 254) + "\xffb", strings.Repeat("a", 254) + "\ufffd"},
	}
	for r := rune(0); r <= 0x9f; r++ {
		if r <= 0x1f || r >= 0x7f {
			tests = append(tests, struct{ name, input, want string }{
				"control_" + strconv.FormatInt(int64(r), 16), "a" + string(r) + "b", "a b",
			})
		}
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeSQLComment(tt.input)
			if got != tt.want {
				t.Fatalf("normalize(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if !utf8.ValidString(got) || utf8.RuneCountInString(got) > 255 || strings.TrimSpace(got) != got || normalizeSQLComment(got) != got {
				t.Fatalf("normalization properties failed: %q", got)
			}
			for _, r := range got {
				if r == '*' || r == '/' || r == '\\' || unicode.IsControl(r) {
					t.Fatalf("forbidden rune %U in %q", r, got)
				}
			}
			dao, err := NewDBDao(&Pool{}, "mysql", nil, "", tt.input)
			assertNoError(t, err)
			if dao.comment != tt.want {
				t.Fatalf("DAO comment = %q, want %q", dao.comment, tt.want)
			}
			for _, b := range []*Builder{dao.Builder(), NewBuilder(NewMySQLGrammar(), nil).Comment(tt.input)} {
				if b.comment != tt.want {
					t.Fatalf("Builder comment = %q, want %q", b.comment, tt.want)
				}
				wantSQL := "SELECT * FROM `users` WHERE `id` = ?"
				if tt.want != "" {
					wantSQL += " /* " + tt.want + " */"
				}
				sql, args, err := b.Table("users").Where("id", 7).ToSelect()
				// 注释专项用例必须精确相等：容忍式 assertSQL 会放过“正文 + 统一测试注释后缀”，
				// 令规范化后本应无注释的输入失去约束。
				assertCommentCompileResult(t, commentCompileExpected{wantSQL, []any{7}}, sql, args, err)
			}
		})
	}
}

// TestBuilder_CommentLifecycle 验证默认复制、覆盖清空、反复编译及 DAO/Builder 隔离。
func TestBuilder_CommentLifecycle(t *testing.T) {
	dao, err := NewDBDao(&Pool{}, "mysql", nil, "custom", " app:one ")
	assertNoError(t, err)
	other, err := NewDBDao(&Pool{}, "mysql", nil, "", "app:two")
	assertNoError(t, err)
	b := dao.Builder().Table("users").Where("id", 7)
	untouched := dao.Builder()
	for _, tt := range []struct{ input, want string }{
		{"app:one", "app:one"}, {" report:daily ", "report:daily"}, {"last", "last"},
		{"", ""}, {" \t\n ", ""}, {"*\x00\r\n", ""},
	} {
		t.Run(strconv.Quote(tt.input), func(t *testing.T) {
			if b.Comment(tt.input) != b {
				t.Fatal("Comment must return the same Builder")
			}
			want := "SELECT * FROM `users` WHERE `id` = ?"
			if tt.want != "" {
				want += " /* " + tt.want + " */"
			}
			for range 2 {
				sql, args, err := b.ToSelect()
				assertNoError(t, err)
				assertSQL(t, want, sql)
				assertArgs(t, []any{7}, args)
			}
			if b.comment != tt.want || dao.comment != "app:one" || untouched.comment != "app:one" || dao.Builder().comment != "app:one" || other.Builder().comment != "app:two" {
				t.Fatal("comment lifecycle leaked across builders or DAOs")
			}
			if b.tagName() != "custom" {
				t.Fatal("comment changed column tag configuration")
			}
		})
	}
	plain := NewBuilder(NewMySQLGrammar(), nil).Table("users")
	sql, args, err := plain.ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users`", sql)
	assertArgs(t, nil, args)
	broken := dao.Builder().Table("users").Where("id", "INVALID", 1)
	originalErr := broken.err
	broken.Comment("report:broken")
	sql, args, err = broken.ToSelect()
	if !errors.Is(err, ErrInvalidOperator) || err != originalErr || sql != "" || args != nil {
		t.Fatalf("Comment lost accumulated error: SQL=%q args=%v error=%v", sql, args, err)
	}
}

// TestBuilder_CommentClone 验证默认/覆盖/清空、递归子查询及环错误副本均保留注释。
func TestBuilder_CommentClone(t *testing.T) {
	for _, comment := range []string{"app:default", "report:custom", ""} {
		t.Run(strconv.Quote(comment), func(t *testing.T) {
			dao, err := NewDBDao(&Pool{}, "mysql", nil, "", "app:default")
			assertNoError(t, err)
			b := dao.Builder().Table("users").Where("id", 7)
			if comment != "app:default" {
				b.Comment(comment)
			}
			clone := b.Clone()
			if clone == b || clone.comment != comment || clone.Clone().comment != comment {
				t.Fatal("Clone did not preserve independent comment state")
			}
			clone.Comment("clone:changed")
			if b.comment != comment || dao.comment != "app:default" {
				t.Fatal("Clone change propagated to source")
			}
			b.Comment("original:changed")
			if clone.comment != "clone:changed" {
				t.Fatal("source change propagated to Clone")
			}
		})
	}
	g := NewMySQLGrammar()
	sub := NewBuilder(g, nil).Table("orders").Comment("inner:orders")
	b := NewBuilder(g, nil).TableSub(sub, "o").Union(sub).Comment("outer:report")
	clone := b.Clone()
	if clone.tableSub == sub || clone.unions[0].Query == sub || clone.tableSub.comment != "inner:orders" || clone.unions[0].Query.comment != "inner:orders" {
		t.Fatal("recursive Clone lost or shared subquery comment")
	}
	clone.tableSub.Comment("")
	if sub.comment != "inner:orders" || clone.unions[0].Query.comment != "inner:orders" {
		t.Fatal("cloned subqueries share mutable state")
	}
	for _, tt := range []struct {
		name   string
		attach func(*Builder, *Builder)
		child  func(*Builder) *Builder
	}{
		{"select", func(b, c *Builder) { b.SelectSub(c, "x") }, func(b *Builder) *Builder { return b.selectSubs[0].Query }},
		{"from", func(b, c *Builder) { b.TableSub(c, "x") }, func(b *Builder) *Builder { return b.tableSub }},
		{"union", func(b, c *Builder) { b.Union(c) }, func(b *Builder) *Builder { return b.unions[0].Query }},
		{"where_nested", func(b, c *Builder) { b.wheres = []WhereClause{{Nested: c}} }, func(b *Builder) *Builder { return b.wheres[0].Nested }},
		{"where_sub", func(b, c *Builder) { b.wheres = []WhereClause{{Sub: c}} }, func(b *Builder) *Builder { return b.wheres[0].Sub }},
		{"having_nested", func(b, c *Builder) { b.havings = []HavingClause{{Nested: c}} }, func(b *Builder) *Builder { return b.havings[0].Nested }},
		{"join_sub", func(b, c *Builder) { b.joins = []JoinClause{{Sub: c}} }, func(b *Builder) *Builder { return b.joins[0].Sub }},
		{"join_nested", func(b, c *Builder) { b.joins = []JoinClause{{Joins: []JoinClause{{Sub: c}}}} }, func(b *Builder) *Builder { return b.joins[0].Joins[0].Sub }},
		{"on_sub", func(b, c *Builder) { b.joins = []JoinClause{{Conditions: []JoinCondition{{Sub: c}}}} }, func(b *Builder) *Builder { return b.joins[0].Conditions[0].Sub }},
		{"on_nested", func(b, c *Builder) {
			b.joins = []JoinClause{{Conditions: []JoinCondition{{Nested: &JoinBuilder{Conditions: []JoinCondition{{Sub: c}}}}}}}
		}, func(b *Builder) *Builder { return b.joins[0].Conditions[0].Nested.Conditions[0].Sub }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := NewBuilder(g, nil).Table("users").Comment("parent")
			child := NewBuilder(g, nil).Table("orders").Comment("child")
			tt.attach(parent, child)
			copied := tt.child(parent.Clone())
			if copied == child || copied.comment != "child" {
				t.Fatal("recursive Clone did not preserve independent comment")
			}
			copied.Comment("copy")
			if child.comment != "child" || parent.comment != "parent" {
				t.Fatal("child comment update escaped Clone")
			}
			sql, args, err := copied.ToSelect()
			assertNoError(t, err)
			assertSQL(t, "SELECT * FROM `orders` /* copy */", sql)
			assertArgs(t, nil, args)
		})
	}
	cyclic := NewBuilder(g, nil).Table("users").Comment("cycle:report")
	cyclic.Union(cyclic)
	broken := cyclic.Clone()
	if broken.comment != "cycle:report" {
		t.Fatal("cyclic error clone lost comment")
	}
	sql, args, err := broken.ToSelect()
	if !errors.Is(err, ErrCyclicQuery) || sql != "" || args != nil {
		t.Fatalf("cyclic clone: SQL=%q args=%v error=%v", sql, args, err)
	}
}

// TestNewDBDao_CommentValidationOrder 验证第五参数不改变方言与连接池校验的原错误优先级。
func TestNewDBDao_CommentValidationOrder(t *testing.T) {
	for _, tt := range []struct {
		name, dialect string
		pool          *Pool
		want          error
	}{
		{"dialect_required", "", nil, ErrDialectRequired},
		{"dialect_unknown", "missing", nil, ErrUnknownDialect},
		{"pool_required", "mysql", nil, ErrPoolRequired},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dao, err := NewDBDao(tt.pool, tt.dialect, nil, "", "*\x00\xff")
			if dao != nil || !errors.Is(err, tt.want) {
				t.Fatalf("DAO=%v error=%v, want nil and %v", dao, err, tt.want)
			}
		})
	}
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
	sql, _, err := newTestBuilder(g, nil).
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
	sql, args, err := newTestBuilder(g, nil).Table("order_items").ToInsert(data)
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
	sql, args, err := newTestBuilder(g, nil).Table("order_items").ToInsert(data)
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
	sql, args, err := newTestBuilder(g, nil).Table("order_items").Where("order_id", "=", 100).ToUpdate(data)
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
	sql, args, err := newTestBuilder(g, nil).Table("users").ToInsert(data)
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
	sql, args, err := newTestBuilder(g, nil).Table("users").Where("id", "=", 1).ToUpdate(data)
	assertNoError(t, err)
	assertSQL(t, "UPDATE `users` SET `id` = ?, `name` = ?, `age` = ?, `email` = ? WHERE `id` = ?", sql)
	assertArgs(t, []any{0, "bob", 0, "", 1}, args)
}

func TestErrorEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := newTestBuilder(g, nil).ToSelect()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

func TestErrorInvalidInsertData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := newTestBuilder(g, nil).Table("users").ToInsert("not a struct")
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestBug_ToCountWithUnion 验证 ToCount 对 UNION 查询生成无效 SQL。
// 期望：COUNT 应包裹整个 UNION 为子查询。
// 实际：生成 (SELECT COUNT(*) ...) UNION (...)，不是合法的计数查询。
func TestBug_ToCountWithUnion(t *testing.T) {
	g := NewMySQLGrammar()
	union := newTestBuilder(g, nil).Table("users").Where("age", ">", 25)
	b := newTestBuilder(g, nil).Table("users").Where("status", "=", "active").Union(union)

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
	selectSub := newTestBuilder(g, nil).Table("orders").Select("amount").Where("status", "=", "active")
	// FROM 子查询（绑定 25）
	tableSub := newTestBuilder(g, nil).Table("users").Where("age", ">", 25)

	b := newTestBuilder(g, nil).
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
	b := newTestBuilder(g, nil).
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
	_, _, err := extractInsertData([]*data{{Name: "a"}, nil, {Name: "c"}}, "")
	if err == nil {
		t.Errorf("expected error for nil pointer element in slice, got nil")
	}
}

// TestBug_CloneShallowCopy_Union 验证 Clone 后修改 UNION 子查询不应影响原 Builder。
func TestBug_CloneShallowCopy_Union(t *testing.T) {
	g := NewMySQLGrammar()
	union := newTestBuilder(g, nil).Table("admins")
	b := newTestBuilder(g, nil).Table("users").Union(union)

	clone := b.Clone()
	// 修改 clone 的 UNION 子查询
	clone.unions[0].Query.Where("status", "=", "super")

	// 原 Builder 不应受影响
	origSQL, _, _ := b.ToSelect()
	if stripTestComment(origSQL) != "(SELECT * FROM `users`) UNION (SELECT * FROM `admins`)" {
		t.Errorf("BUG: Clone shares UNION sub-builder reference, original affected:\n  got: %s", origSQL)
	}
}

// TestBug_CloneShallowCopy_WhereNested 验证 Clone 后修改嵌套 WHERE 不应影响原 Builder。
func TestBug_CloneShallowCopy_WhereNested(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("users").WhereNested(func(sub *Builder) {
		sub.Where("age", ">", 18)
	})

	clone := b.Clone()
	// 修改 clone 的嵌套 WHERE
	clone.wheres[0].Nested.Where("status", "=", "active")

	// 原 Builder 的嵌套 WHERE 不应受影响
	origSQL, _, _ := b.ToSelect()
	expected := "SELECT * FROM `users` WHERE (`age` > ?)"
	if stripTestComment(origSQL) != expected {
		t.Errorf("BUG: Clone shares nested WHERE sub-builder reference, original affected:\n  expected: %s\n  got:      %s", expected, origSQL)
	}
}

// TestBug_CloneWhereValuesShallowCopy 验证 Clone 后 WhereIn 的 Values 切片不应与原 Builder 共享。
func TestBug_CloneWhereValuesShallowCopy(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("users").WhereIn("id", []any{1, 2, 3})
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
	b := newTestBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
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

// TestBug_CloneJoinNestedErrCopy 验证 Clone 完整复制嵌套 JoinBuilder 的 err 字段。
// 该状态经公开 API 不可达（addNested 在 err != nil 时提前返回并把错误提升给父级），
// 因此这里直接构造字段锁定行为：逐字段构造副本时漏字段会让 Clone 静默丢错，
// 也让 cloneInternal 的"深拷贝全部查询状态"承诺失效。
func TestBug_CloneJoinNestedErrCopy(t *testing.T) {
	g := NewMySQLGrammar()
	nested := &JoinBuilder{
		grammar: g,
		err:     ErrInvalidOperator,
		Conditions: []JoinCondition{{
			Type: "column", First: "users.id", Operator: "=", Second: "orders.user_id",
		}},
	}
	b := newTestBuilder(g, nil).Table("users")
	b.joins = []JoinClause{{Type: JoinTypeInner, Table: "orders", Conditions: []JoinCondition{{Type: "nested", Nested: nested}}}}

	clone := b.Clone()
	got := clone.joins[0].Conditions[0].Nested
	if got == nil || got == nested {
		t.Fatalf("Clone 应深拷贝嵌套 JoinBuilder，实际同一指针: %v", got == nested)
	}
	if got.err != ErrInvalidOperator {
		t.Errorf("Clone 未复制嵌套 JoinBuilder.err: got %v want %v", got.err, ErrInvalidOperator)
	}
	// 副本与原件隔离：改写副本的错误不影响原件
	got.err = nil
	if nested.err != ErrInvalidOperator {
		t.Error("副本修改影响了原嵌套 JoinBuilder")
	}
	// 嵌套 JoinBuilder 的错误只作字段保存，不参与编译（与既有语义一致）
	want := "SELECT * FROM `users` INNER JOIN `orders` ON (`users`.`id` = `orders`.`user_id`)"
	origSQL, _, err := b.ToSelect()
	assertNoError(t, err)
	assertSQL(t, want, origSQL)
	cloneSQL, _, err := clone.ToSelect()
	assertNoError(t, err)
	assertSQL(t, want, cloneSQL)
}

// TestBug_OperatorInjection 验证恶意运算符不应被拼入 SQL。
func TestBug_OperatorInjection(t *testing.T) {
	g := NewMySQLGrammar()
	malicious := "= 1; DROP TABLE users; --"
	b := newTestBuilder(g, nil).Table("users").Where("id", malicious, 1)

	_, _, err := b.ToSelect()
	if err == nil {
		sql, _, _ := b.ToSelect()
		t.Errorf("恶意运算符不应生成 SQL，应返回错误:\n  got: %s", sql)
	}
}

// TestBug_OperatorInjection_JoinOn 验证 JoinBuilder.Where 的运算符也应校验。
func TestBug_OperatorInjection_JoinOn(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("users").JoinOn("orders", func(jb *JoinBuilder) {
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
	b := newTestBuilder(g, nil).Table("orders").
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
	b := newTestBuilder(g, nil).Table("users").Select("name", "age").Where("age", ">", 25)
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

// TestBug_ToCountUnionPanicSafety 验证 ToCount 的 UNION 分支在编译/收集绑定 panic 时
// 临时修改的分页/排序/锁状态也能经 defer 完整恢复（ZCDB-06 回归锁定：
// 修复前 UNION/GROUP BY/DISTINCT 分支手动恢复状态，panic 时残留污染）。
func TestBug_ToCountUnionPanicSafety(t *testing.T) {
	g := NewMySQLGrammar()
	sub := newTestBuilder(g, nil).Table("admins").Select("name")
	b := newTestBuilder(g, nil).Table("users").Select("name").Union(sub)
	b.limit = 10
	b.offset = 5
	b.orders = []OrderClause{{Column: "id", Direction: "ASC"}}
	b.lockClause = "FOR UPDATE"

	// 添加一个会导致 collectWhereBindings panic 的 WHERE 条件（不支持的 WhereType）
	b.wheres = append(b.wheres, WhereClause{Type: WhereType(999)})

	func() {
		defer func() { recover() }()
		_, _, _ = b.ToCount()
	}()

	// panic 后 UNION 分支临时清空的状态应已全部恢复
	if b.limit != 10 || b.offset != 5 {
		t.Errorf("ToCount(UNION) panic 后分页未恢复: expected limit=10 offset=5, got limit=%d offset=%d", b.limit, b.offset)
	}
	if len(b.orders) != 1 || b.orders[0].Column != "id" {
		t.Errorf("ToCount(UNION) panic 后 orders 未恢复: got %v", b.orders)
	}
	if b.lockClause != "FOR UPDATE" {
		t.Errorf("ToCount(UNION) panic 后 lockClause 未恢复: got %q", b.lockClause)
	}
	if len(b.columns) != 1 || b.columns[0].Value != "name" {
		t.Errorf("ToCount(UNION) panic 后 columns 未恢复: got %v", b.columns)
	}
}

// TestBug_ToAggregateUnionPanicSafety 验证 ToAggregate 的 UNION 分支 panic 时
// 临时状态同样经 defer 完整恢复（ZCDB-06 回归锁定）。
func TestBug_ToAggregateUnionPanicSafety(t *testing.T) {
	g := NewMySQLGrammar()
	sub := newTestBuilder(g, nil).Table("admins").Select("age")
	b := newTestBuilder(g, nil).Table("users").Select("age").Union(sub)
	b.limit = 20
	b.orders = []OrderClause{{Column: "age", Direction: "DESC"}}

	b.wheres = append(b.wheres, WhereClause{Type: WhereType(999)})

	func() {
		defer func() { recover() }()
		_, _, _ = b.ToAggregate("MAX", "age")
	}()

	if b.limit != 20 {
		t.Errorf("ToAggregate(UNION) panic 后 limit 未恢复: expected 20, got %d", b.limit)
	}
	if len(b.orders) != 1 || b.orders[0].Direction != "DESC" {
		t.Errorf("ToAggregate(UNION) panic 后 orders 未恢复: got %v", b.orders)
	}
	if len(b.columns) != 1 || b.columns[0].Value != "age" {
		t.Errorf("ToAggregate(UNION) panic 后 columns 未恢复: got %v", b.columns)
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
	b := newTestBuilder(g, nil).Table("users")
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
	info := getScanFieldInfo(reflect.TypeOf(UserWithEmbed{}), "")
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
			return newTestBuilder(g, nil).Table("t").OrWhere("a", invalid, 1)
		}},
		{"WhereColumn", func() *Builder {
			return newTestBuilder(g, nil).Table("t").WhereColumn("a", invalid, "b")
		}},
		{"OrHaving", func() *Builder {
			return newTestBuilder(g, nil).Table("t").Select("a").GroupBy("a").OrHaving("SUM(a)", invalid, 1)
		}},
		{"WhereSub", func() *Builder {
			return newTestBuilder(g, nil).Table("t").WhereSub("a", invalid, func(sub *Builder) {
				sub.Table("t2")
			})
		}},
		{"OrWhereSub", func() *Builder {
			return newTestBuilder(g, nil).Table("t").OrWhereSub("a", invalid, func(sub *Builder) {
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

	// 边界固化（m3 修复后）：大小写归一化对所有形态生效，混合大小写同样被接受。
	if err := validateOperator("like"); err != nil {
		t.Errorf("全小写 like 应被接受, got %v", err)
	}
	if err := validateOperator("not like"); err != nil {
		t.Errorf("全小写 not like 应归一化后接受, got %v", err)
	}
	if err := validateOperator("NOT LIKE"); err != nil {
		t.Errorf("全大写 NOT LIKE 应被接受, got %v", err)
	}
	if err := validateOperator("Like"); err != nil {
		t.Errorf("混合大小写 Like 应被接受（m3 修复）, got %v", err)
	}
	if err := validateOperator("EvIl"); !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("非法运算符 EvIl 应被拒绝, got %v", err)
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
			return newTestBuilder(g, nil).Table("t").LeftJoinOn("t2", func(jb *JoinBuilder) {
				jb.On("t.id", invalid, "t2.id")
			})
		}},
		{"RightJoinOn", func() *Builder {
			return newTestBuilder(g, nil).Table("t").RightJoinOn("t2", func(jb *JoinBuilder) {
				jb.On("t.id", invalid, "t2.id")
			})
		}},
		{"JoinOn_OrOn", func() *Builder {
			return newTestBuilder(g, nil).Table("t").JoinOn("t2", func(jb *JoinBuilder) {
				jb.OrOn("t.id", invalid, "t2.id")
			})
		}},
		{"JoinBuilder_OrWhere", func() *Builder {
			return newTestBuilder(g, nil).Table("t").JoinOn("t2", func(jb *JoinBuilder) {
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
		{"MySQL_MixedCase", &MySQLGrammar{}, "users As u", "`users` AS `u`"},
		{"Postgres_simple", &PostgresGrammar{}, "users", `"users"`},
		{"Postgres_alias", &PostgresGrammar{}, "users as u", `"users" AS "u"`},
		{"Postgres_ALIAS", &PostgresGrammar{}, "users AS u", `"users" AS "u"`},
		// ZCDB-01 回归锁定：修复前 PG 混合大小写别名漏判，整体包裹为 `"users As u"`
		{"Postgres_MixedCase", &PostgresGrammar{}, "users As u", `"users" AS "u"`},
		{"Postgres_MixedCase2", &PostgresGrammar{}, "users aS u", `"users" AS "u"`},
		{"SQLite_simple", &SQLiteGrammar{}, "users", `"users"`},
		{"SQLite_alias", &SQLiteGrammar{}, "users as u", `"users" AS "u"`},
		{"SQLite_MixedCase", &SQLiteGrammar{}, "users As u", `"users" AS "u"`},
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
			b := newTestBuilder(g, nil).Table("t").OrderBy("col", tt.direction)
			if len(b.orders) != 1 || b.orders[0].Direction != tt.expected {
				t.Errorf("OrderBy(%q): expected direction %q, got %v", tt.direction, tt.expected, b.orders)
			}
		})
	}

	// 省略 direction 变参时默认升序 ASC
	t.Run("omitted_defaults_ASC", func(t *testing.T) {
		b := newTestBuilder(g, nil).Table("t").OrderBy("col")
		if len(b.orders) != 1 || b.orders[0].Direction != "ASC" {
			t.Errorf("OrderBy without direction: expected ASC, got %v", b.orders)
		}
	})
}

// TestBuilder_ForPage_InvalidPage 验证 ForPage 传入 page < 1 时自动修正为 1。
func TestBuilder_ForPage_InvalidPage(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("t").ForPage(0, 10)
	if b.offset != 0 || b.limit != 10 {
		t.Errorf("ForPage(0, 10): expected offset=0 limit=10, got offset=%d limit=%d", b.offset, b.limit)
	}
	b = newTestBuilder(g, nil).Table("t").ForPage(-5, 20)
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
				sub := newTestBuilder(g, nil).Table("orders").Where("amount", ">", 100)
				return newTestBuilder(g, nil).Table("users").TableSub(sub, "o")
			},
			expected: "SELECT * FROM (SELECT * FROM `orders` WHERE `amount` > ?) AS `o`",
			wantArgs: 1,
		},
		{
			name: "TableAfterTableSubRevertsToPlainTable",
			build: func() *Builder {
				sub := newTestBuilder(g, nil).Table("orders").Where("amount", ">", 100)
				return newTestBuilder(g, nil).TableSub(sub, "o").Table("users")
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
			assertSQL(t, tt.expected, sql)
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
	tableSub := newTestBuilder(g, nil).Table("sub_t")
	selectSub := newTestBuilder(g, nil).Table("orders").Select("amount").Where("status", "=", "active")

	b := newTestBuilder(g, nil).
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
		Union(newTestBuilder(g, nil).Table("admins")).
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

// TestBuilder_PrimaryFlag 验证 Primary 强制主库标记的行为：
// 1. 仅设置执行层路由标记，不改变编译出的 SQL（与 LockForUpdate 的本质区别）；
// 2. Clone 保留该标记（First/Value/Paginate 等内部克隆的终端方法依赖此性质）；
// 3. 副本与原 Builder 标记相互独立。
func TestBuilder_PrimaryFlag(t *testing.T) {
	g := NewMySQLGrammar()

	withPrimary := newTestBuilder(g, nil).Table("users").Where("id", "=", 1).Primary()
	plain := newTestBuilder(g, nil).Table("users").Where("id", "=", 1)

	if !withPrimary.usePrimary {
		t.Fatal("Primary() 应置位 usePrimary 标记")
	}

	// 编译中立：加不加 Primary，SQL 与绑定参数完全一致（不引入锁子句）
	sql1, args1, err1 := withPrimary.ToSelect()
	assertNoError(t, err1)
	sql2, args2, err2 := plain.ToSelect()
	assertNoError(t, err2)
	if sql1 != sql2 {
		t.Errorf("Primary 不应改变编译 SQL: %q vs %q", sql1, sql2)
	}
	if len(args1) != len(args2) {
		t.Errorf("Primary 不应改变绑定参数: %v vs %v", args1, args2)
	}

	// Clone 保留标记
	clone := withPrimary.Clone()
	if !clone.usePrimary {
		t.Error("BUG: Clone 丢失 usePrimary 标记")
	}
	// 副本独立：清除副本标记不影响原 Builder
	clone.usePrimary = false
	if !withPrimary.usePrimary {
		t.Error("BUG: 修改副本 usePrimary 影响了原 Builder")
	}
}

// TestBug_CloneJoinDeepNesting 审查复现用例：
// JoinBuilder.JoinOn 可递归构造任意深度嵌套 join 组，但 Clone 只深拷贝两层，
// 第三层嵌套的 Conditions/Sub 切片与原 Builder 共享底层数组，违反深拷贝契约。
func TestBug_CloneJoinDeepNesting(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
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
	b := newTestBuilder(g, nil).Table("users").JoinOn("orders", func(j *JoinBuilder) {
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

	// ZCDB-02 回归锁定：修复前手写实现对 MinInt 取负溢出仍为负，负分支无限写 '-' 死循环；
	// 期望值用 strconv.Itoa 生成（intToStr 已委托 strconv.Itoa，两者必须一致），同时锁定 MaxInt 上界。
	if got := intToStr(math.MinInt); got != strconv.Itoa(math.MinInt) {
		t.Errorf("intToStr(math.MinInt) = %q, want %q", got, strconv.Itoa(math.MinInt))
	}
	if got := intToStr(math.MaxInt); got != strconv.Itoa(math.MaxInt) {
		t.Errorf("intToStr(math.MaxInt) = %q, want %q", got, strconv.Itoa(math.MaxInt))
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
	sql, args, err := newTestBuilder(g, nil).
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
	_, err := newTestBuilder(g, nil).ToTruncate()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestBuilder_ToDeleteEmptyTable 验证 ToDelete 未设置表名时返回错误。
func TestBuilder_ToDeleteEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := newTestBuilder(g, nil).ToDelete()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestBuilder_ToInsertInvalidData 验证 ToInsert 传入非结构体时返回错误。
func TestBuilder_ToInsertInvalidData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := newTestBuilder(g, nil).Table("t").ToInsert(123)
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestBuilder_ToInsertUsingEmptyTable 验证 ToInsertUsing 未设置表名时返回错误。
func TestBuilder_ToInsertUsingEmptyTable(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := newTestBuilder(g, nil).ToInsertUsing([]string{"a"}, func(sub *Builder) {
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
	_, err := NewDBDao(nil, "oracle", nil, "", "")
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
	sql, args, err := newTestBuilder(g, nil).
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
	sql, args, err := newTestBuilder(g, nil).
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
	sql, args, err := newTestBuilder(g, nil).
		Table("users").
		WhereNotIn("id", []any{}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE 1 = 1", sql)
	assertArgs(t, []any{}, args)
}

// TestBuilder_WhereInExpression 验证 WhereIn/WhereNotIn 的 Expression 元素直接内嵌 SQL、
// 不占占位符，其余元素照常绑定（三方言对称）。
func TestBuilder_WhereInExpression(t *testing.T) {
	tests := []struct {
		name     string
		grammar  Grammar
		expected string
	}{
		{"mysql", NewMySQLGrammar(), "SELECT * FROM `users` WHERE `id` IN (?, parent_id)"},
		{"postgres", NewPostgresGrammar(), `SELECT * FROM "users" WHERE "id" IN ($1, parent_id)`},
		{"sqlite", NewSQLiteGrammar(), `SELECT * FROM "users" WHERE "id" IN (?, parent_id)`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args, err := newTestBuilder(tt.grammar, nil).
				Table("users").
				WhereIn("id", []any{1, NewExpression("parent_id")}).
				ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.expected, sql)
			assertArgs(t, []any{1}, args)
		})
	}

	// NOT IN 同构（MySQL 形态）
	sql, args, err := newTestBuilder(NewMySQLGrammar(), nil).
		Table("users").
		WhereNotIn("id", []any{NewExpression("parent_id"), 2}).
		ToSelect()
	assertNoError(t, err)
	assertSQL(t, "SELECT * FROM `users` WHERE `id` NOT IN (parent_id, ?)", sql)
	assertArgs(t, []any{2}, args)
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
			sql, args, err := newTestBuilder(tt.grammar, nil).
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
			sql, args, err := tt.build(newTestBuilder(tt.grammar, nil).Table("users")).ToSelect()
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
			sql, args, err := newTestBuilder(tt.grammar, nil).
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
			sql, _, err := newTestBuilder(tt.grammar, nil).
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
	b := newTestBuilder(NewMySQLGrammar(), nil).
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
		_, _, err := extractInsertData(u, "")
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
		_, _, err := extractUpdateData(u, "")
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
	b := newTestBuilder(NewMySQLGrammar(), nil).Table("users").Force()
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
	b := newTestBuilder(NewMySQLGrammar(), nil)
	if b.hasEffectiveWhere() {
		t.Error("empty wheres should not have effective where")
	}

	// 普通条件
	b = newTestBuilder(NewMySQLGrammar(), nil).Where("id", "=", 1)
	if !b.hasEffectiveWhere() {
		t.Error("basic where should be effective")
	}

	// 空嵌套：应视为无有效条件
	b = newTestBuilder(NewMySQLGrammar(), nil).WhereNested(func(q *Builder) {})
	if b.hasEffectiveWhere() {
		t.Error("empty nested where should not be effective")
	}

	// 嵌套含有效条件：应视为有效
	b = newTestBuilder(NewMySQLGrammar(), nil).WhereNested(func(q *Builder) {
		q.Where("id", ">", 10)
	})
	if !b.hasEffectiveWhere() {
		t.Error("nested with conditions should be effective")
	}

	// 空 JOIN（无 ON/Where 条件）：不应视为有效限定
	b = newTestBuilder(NewMySQLGrammar(), nil).JoinOn("profiles", func(jb *JoinBuilder) {})
	if b.hasEffectiveJoin() {
		t.Error("empty join should not be effective")
	}
	if b.hasEffectiveWhere() || b.hasEffectiveJoin() {
		t.Error("empty join should not bypass protection")
	}

	// 带 ON 条件的 JOIN：应视为有效限定（UPDATE/DELETE JOIN 场景）
	b = newTestBuilder(NewMySQLGrammar(), nil).JoinOn("profiles", func(jb *JoinBuilder) {
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
	b := newTestBuilder(NewMySQLGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, args, err := b.ToExists()
	assertNoError(t, err)
	assertSQL(t, "SELECT 1 FROM `users` WHERE `id` = ? LIMIT 1", sqlStr)
	if len(args) != 1 || args[0] != 1 {
		t.Errorf("expected args [1], got %v", args)
	}

	// SQLite
	b = newTestBuilder(NewSQLiteGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, _, err = b.ToExists()
	assertNoError(t, err)
	assertSQL(t, `SELECT 1 FROM "users" WHERE "id" = ? LIMIT 1`, sqlStr)

	// PostgreSQL
	b = newTestBuilder(NewPostgresGrammar(), nil).Table("users").Where("id", "=", 1)
	sqlStr, args, err = b.ToExists()
	assertNoError(t, err)
	assertSQL(t, `SELECT 1 FROM "users" WHERE "id" = $1 LIMIT 1`, sqlStr)
	if len(args) != 1 {
		t.Errorf("expected 1 arg, got %v", args)
	}

	// UNION：整个 UNION 包裹为子查询后附加 LIMIT 1
	g := NewMySQLGrammar()
	union := newTestBuilder(g, nil).Table("admins").Where("id", ">", 2)
	b = newTestBuilder(g, nil).Table("users").Where("id", ">", 1).Union(union)
	sqlStr, _, err = b.ToExists()
	assertNoError(t, err)
	if !strings.Contains(sqlStr, "SELECT 1 FROM (") || !strings.Contains(sqlStr, "LIMIT 1") {
		t.Errorf("expected subquery wrapped union with LIMIT 1, got: %s", sqlStr)
	}

	// 状态恢复：ToExists 不应破坏原 Builder 的分页/列/锁状态
	b = newTestBuilder(NewMySQLGrammar(), nil).Table("users").
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
			sqlStr, _, err := newTestBuilder(tt.grammar, nil).Table("users").Select(tt.column).ToSelect()
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
		return newTestBuilder(g, nil).Table("fund_net_value").
			Select("fund_code", "MAX(ed) AS ed").
			WhereIn("fund_code", codes).
			GroupBy("fund_code")
	}
	buildQuery := func(g Grammar) *Builder {
		return newTestBuilder(g, nil).Table("fund_net_value AS t1").
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
		return newTestBuilder(g, nil).Table("sales").
			Select("store_name").
			Distinct().
			WhereIn("store_name", codes)
	}
	buildQuery := func(g Grammar) *Builder {
		m := newTestBuilder(g, nil).Table("sales").Select("month").Distinct()
		return newTestBuilder(g, nil).TableSub(m, "m").
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
	b := newTestBuilder(NewSQLiteGrammar(), nil)
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
	sub := newTestBuilder(NewMySQLGrammar(), nil).Table("fund_net_value").
		Select("fund_code", "MAX(ed) AS ed").
		GroupBy("fund_code")

	b := newTestBuilder(NewMySQLGrammar(), nil).Table("fund_net_value AS t1").
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

// 以下 helper 仅服务 builder_compile 的三方言单测，不混入执行/连接测试。
type commentCompileExpected struct {
	sql  string
	args []any
}

type commentCompileFixture struct {
	name    string
	b       *Builder
	compile func(*Builder) (string, []any, error)
	direct  func(*Builder) string
}

type commentCompileRow struct {
	Name string `db:"name"`
	Age  int    `db:"age"`
}

func commentCompileFixtures(g Grammar) []commentCompileFixture {
	base := func() *Builder {
		return NewBuilder(g, nil).Table("users").Select("name").Where("id", 7).OrderBy("name").Limit(5).Offset(2)
	}
	union := func() *Builder {
		return base().Union(NewBuilder(g, nil).Table("admins").Select("name").Where("rank", ">", 9).Comment("inner:admin"))
	}
	joined := func() *Builder {
		return NewBuilder(g, nil).Table("users").Where("id", 7).JoinOn("profiles", func(j *JoinBuilder) {
			j.On("users.id", "=", "profiles.user_id").Where("profiles.active", "=", 99)
		})
	}
	using := func(sub *Builder) {
		sub.Table("source").Select("name").Where("score", ">", 11).Comment("inner:source")
	}
	row := commentCompileRow{Name: "alice", Age: 23}
	columns, rows := []string{"name", "age"}, [][]any{{"alice", 23}}
	fixtures := []commentCompileFixture{
		{"select", base(), (*Builder).ToSelect, func(b *Builder) string { return g.CompileSelect(b, b.columns) }},
		{"select_union", union(), (*Builder).ToSelect, nil},
		{"insert", base(), func(b *Builder) (string, []any, error) { return b.ToInsert(row) }, func(b *Builder) string { return g.CompileInsert(b, columns, rows) }},
		{"insert_ignore", base(), func(b *Builder) (string, []any, error) { return b.ToInsertOrIgnore(row) }, func(b *Builder) string { return g.CompileInsertOrIgnore(b, columns, rows) }},
		{"upsert", base(), func(b *Builder) (string, []any, error) { return b.ToUpsert(row, []string{"name"}, []string{"age"}) }, func(b *Builder) string {
			return g.CompileUpsert(b, columns, rows, []string{"name"}, []string{"age"}, nil)
		}},
		{"upsert_no_update", base(), func(b *Builder) (string, []any, error) {
			return b.ToUpsert(struct {
				Name string `db:"name"`
			}{"alice"}, []string{"name"}, nil)
		}, nil},
		{"insert_using", base(), func(b *Builder) (string, []any, error) { return b.ToInsertUsing([]string{"name"}, using) }, func(b *Builder) string {
			sub := NewBuilder(g, nil)
			using(sub)
			return g.CompileInsertUsing(b, []string{"name"}, sub)
		}},
		{"insert_ignore_using", base(), func(b *Builder) (string, []any, error) { return b.ToInsertOrIgnoreUsing([]string{"name"}, using) }, func(b *Builder) string {
			sub := NewBuilder(g, nil)
			using(sub)
			return g.CompileInsertOrIgnoreUsing(b, []string{"name"}, sub)
		}},
		{"update", joined(), func(b *Builder) (string, []any, error) { return b.ToUpdate(row) }, func(b *Builder) string { return g.CompileUpdate(b, columns, rows[0]) }},
		{"delete", base(), (*Builder).ToDelete, g.CompileDelete},
		{"delete_join", joined(), (*Builder).ToDeleteJoin, g.CompileDeleteJoin},
		{"truncate", base(), func(b *Builder) (string, []any, error) { sql, err := b.ToTruncate(); return sql, nil, err }, g.CompileTruncate},
		{"count", base().LockForUpdate(), (*Builder).ToCount, nil},
		{"count_union", union().LockForUpdate(), (*Builder).ToCount, nil},
		{"count_group", base().GroupBy("name").HavingRaw("COUNT(*) > ?", 2).LockForUpdate(), (*Builder).ToCount, nil},
		{"count_distinct", base().Distinct().LockForUpdate(), (*Builder).ToCount, nil},
		{"exists", base().LockForUpdate(), (*Builder).ToExists, nil},
		{"exists_union", union().LockForUpdate(), (*Builder).ToExists, nil},
		{"aggregate", base().LockForUpdate(), func(b *Builder) (string, []any, error) { return b.ToAggregate("SUM", "age") }, nil},
		{"aggregate_union", union().LockForUpdate(), func(b *Builder) (string, []any, error) { return b.ToAggregate("MAX", "name") }, nil},
		{"increment", joined(), func(b *Builder) (string, []any, error) {
			return b.ToIncrement([]string{"wallet", "level"}, []any{100, 2})
		}, nil},
		{"decrement", joined(), func(b *Builder) (string, []any, error) {
			return b.ToDecrement([]string{"wallet", "level"}, []any{50, 1})
		}, nil},
	}
	if _, sqlite := g.(*SQLiteGrammar); !sqlite {
		fixtures = append(fixtures,
			commentCompileFixture{"select_lock", base().LockForUpdate(), (*Builder).ToSelect, nil},
			commentCompileFixture{"select_shared_lock", base().SharedLock(), (*Builder).ToSelect, nil})
	}
	// 状态恢复后还要普通 ToSelect；此处只保留方言支持的锁组合，错误组合另在 C09 覆盖。
	for _, tc := range fixtures {
		if _, sqlite := g.(*SQLiteGrammar); sqlite {
			tc.b.lockClause = ""
		}
		if _, pg := g.(*PostgresGrammar); pg && len(tc.b.unions) > 0 {
			tc.b.lockClause = ""
		}
	}
	return fixtures
}

// commentInspectGrammar 在真正 Grammar 开始编译前观察子状态，防止以清空再恢复的方式抑制子注释。
type commentInspectGrammar struct {
	Grammar
	check func()
}

func (g *commentInspectGrammar) CompileSelect(b *Builder, columns []SelectColumn) string {
	g.check()
	return g.Grammar.CompileSelect(b, columns)
}

func (g *commentInspectGrammar) CompileInsertUsing(b *Builder, columns []string, sub *Builder) string {
	g.check()
	return g.Grammar.CompileInsertUsing(b, columns, sub)
}

func (g *commentInspectGrammar) CompileInsertOrIgnoreUsing(b *Builder, columns []string, sub *Builder) string {
	g.check()
	return g.Grammar.CompileInsertOrIgnoreUsing(b, columns, sub)
}

func assertCommentSubqueries(t *testing.T, g Grammar, dialect string, expected map[string]commentCompileExpected) {
	t.Helper()
	names := []string{"select", "from", "where_scalar", "where_in", "where_exists", "where_exists_builder", "join_table", "join_scalar", "join_in", "join_exists", "union", "insert_using", "insert_ignore_using", "select_from_where"}
	if len(expected) != len(names)+1 {
		t.Fatalf("子查询期望矩阵数量错误：%d", len(expected))
	}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			for _, explicit := range []bool{false, true} {
				childMode, childSuffix := "inherited", " /* dao:default */"
				if explicit {
					childMode, childSuffix = "explicit", " /* inner:child */"
				}
				t.Run(childMode, func(t *testing.T) {
					for _, mode := range []struct{ name, suffix string }{
						{"default", " /* dao:default */"}, {"override", " /* outer:query */"}, {"clear", ""},
					} {
						t.Run(mode.name, func(t *testing.T) {
							// 空 Pool 仅通过构造校验；本测试不打开连接、不执行 SQL。
							dao, err := NewDBDao(&Pool{}, dialect, nil, "", "dao:default")
							assertNoError(t, err)
							type capturedChild struct {
								b     *Builder
								state Builder
							}
							var children []capturedChild
							configure := func(q *Builder) {
								q.Table("source").Select("name").Where("score", ">", 11)
								if explicit {
									q.Comment("inner:child")
								}
								children = append(children, capturedChild{q, snapshotCommentCompileState(q)})
							}
							child := dao.Builder()
							configure(child)
							b := dao.Builder().Table("users").Select("name").Where("id", 7)
							compile := (*Builder).ToSelect
							switch name {
							case "select":
								b.SelectSub(child, "picked")
							case "from":
								b.TableSub(child, "s")
							case "where_scalar":
								b.WhereSub("name", "=", configure)
							case "where_in":
								b.WhereInSub("name", configure)
							case "where_exists":
								b.WhereExists(configure)
							case "where_exists_builder":
								b.WhereExists(child)
							case "join_table":
								b.JoinSub(child, "s", func(j *JoinBuilder) { j.On("users.name", "=", "s.name").Where("s.name", "<>", "blocked") })
							case "join_scalar":
								b.JoinOn("profiles", func(j *JoinBuilder) { j.Where("profiles.name", "=", child) })
							case "join_in":
								b.JoinOn("profiles", func(j *JoinBuilder) { j.WhereIn("profiles.name", child) })
							case "join_exists":
								b.JoinOn("profiles", func(j *JoinBuilder) { j.WhereExists(configure) })
							case "union":
								b.Union(child)
							case "insert_using":
								compile = func(b *Builder) (string, []any, error) { return b.ToInsertUsing([]string{"name"}, configure) }
							case "insert_ignore_using":
								compile = func(b *Builder) (string, []any, error) { return b.ToInsertOrIgnoreUsing([]string{"name"}, configure) }
							case "select_from_where":
								b.SelectSub(child, "picked").TableSub(child, "s").WhereInSub("name", configure)
							}
							if mode.name == "override" {
								b.Comment("outer:query")
							}
							if mode.name == "clear" {
								b.Comment("")
							}
							checks := 0
							b.grammar = &commentInspectGrammar{Grammar: g, check: func() {
								checks++
								for _, c := range children {
									assertCommentCompileState(t, c.b, c.state)
								}
							}}
							before := snapshotCommentCompileState(b)
							want, ok := expected[name]
							if !ok {
								t.Fatalf("缺少 %s 期望", name)
							}
							childWant, ok := expected["child"]
							if !ok {
								t.Fatal("缺少子查询独立编译期望")
							}
							for range 2 {
								sql, args, err := compile(b)
								assertCommentCompileResult(t, commentCompileExpected{want.sql + mode.suffix, want.args}, sql, args, err)
								assertCommentCompileState(t, b, before)
								for _, c := range children {
									assertCommentCompileState(t, c.b, c.state)
									sql, args, err := c.b.ToSelect()
									assertCommentCompileResult(t, commentCompileExpected{childWant.sql + childSuffix, childWant.args}, sql, args, err)
									assertCommentCompileState(t, c.b, c.state)
								}
							}
							if checks < 2 {
								t.Fatalf("未观察到两次 Grammar 调用：%d", checks)
							}
						})
					}
				})
			}
		})
	}
}

func snapshotCommentCompileState(b *Builder) Builder {
	state := *b
	state.columns = slices.Clone(b.columns)
	state.selectSubs = slices.Clone(b.selectSubs)
	state.orders = slices.Clone(b.orders)
	state.wheres = slices.Clone(b.wheres)
	state.groups = slices.Clone(b.groups)
	state.havings = slices.Clone(b.havings)
	state.joins = slices.Clone(b.joins)
	state.unions = slices.Clone(b.unions)
	return state
}

func assertCommentCompileState(t *testing.T, b *Builder, want Builder) {
	t.Helper()
	if !reflect.DeepEqual(*b, want) {
		t.Fatalf("编译污染 Builder 状态：\nwant: %#v\n got: %#v", want, *b)
	}
}

// assertCommentCompileResult 注释专项断言：SQL 必须与期望值精确相等，不走容忍式 assertSQL，
// 否则"注释多追加一次"这类缺陷会被容忍规则掩盖。
func assertCommentCompileResult(t *testing.T, want commentCompileExpected, sql string, args []any, err error) {
	t.Helper()
	assertNoError(t, err)
	if sql != want.sql {
		t.Errorf("SQL mismatch:\n  expected: %s\n  actual:   %s", want.sql, sql)
	}
	// 与通用 assertArgs 不同，此处也锁死无绑定时必须返回 nil。
	if !reflect.DeepEqual(want.args, args) {
		t.Fatalf("绑定参数不匹配：want %#v, got %#v", want.args, args)
	}
}

func assertCommentCompileError(t *testing.T, want error, sql string, args []any, err error) {
	t.Helper()
	if sql != "" || args != nil || !errors.Is(err, want) {
		t.Fatalf("错误出口应为空 SQL/nil args/%v，实际 (%q, %#v, %v)", want, sql, args, err)
	}
}

func assertCommentCompileErrors(t *testing.T, g Grammar) {
	t.Helper()
	for _, tc := range commentCompileFixtures(g) {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"accumulated", "empty_table"} {
				t.Run(path, func(t *testing.T) {
					b, want := tc.b.Where("id", "INVALID", 1), ErrInvalidOperator
					if path == "empty_table" {
						b, want = NewBuilder(g, nil), ErrEmptyTable
					}
					b.Comment("error:compile ? $1")
					before := snapshotCommentCompileState(b)
					for range 2 {
						sql, args, err := tc.compile(b)
						assertCommentCompileError(t, want, sql, args, err)
						assertCommentCompileState(t, b, before)
					}
				})
			}
		})
	}
	base := func() *Builder { return NewBuilder(g, nil).Table("users").Comment("error:compile") }
	row := commentCompileRow{Name: "alice", Age: 23}
	type errorCase struct {
		name    string
		b       *Builder
		compile func(*Builder) (string, []any, error)
		want    error
	}
	cases := []errorCase{
		{"insert_invalid", base(), func(b *Builder) (string, []any, error) { return b.ToInsert(7) }, ErrInvalidStruct},
		{"insert_empty", base(), func(b *Builder) (string, []any, error) { return b.ToInsert([]commentCompileRow{}) }, ErrEmptyData},
		{"insert_no_fields", base(), func(b *Builder) (string, []any, error) { return b.ToInsert(struct{}{}) }, ErrNoFields},
		{"ignore_invalid", base(), func(b *Builder) (string, []any, error) { return b.ToInsertOrIgnore(7) }, ErrInvalidStruct},
		{"upsert_invalid", base(), func(b *Builder) (string, []any, error) { return b.ToUpsert(7, []string{"name"}, nil) }, ErrInvalidStruct},
		{"update_invalid", base(), func(b *Builder) (string, []any, error) { return b.ToUpdate(7) }, ErrInvalidStruct},
		{"aggregate_invalid", base(), func(b *Builder) (string, []any, error) { return b.ToAggregate("COUNT;--", "age") }, ErrInvalidAggregate},
		{"increment_empty", base(), func(b *Builder) (string, []any, error) { return b.ToIncrement(nil, nil) }, ErrIncrementColumns},
		{"decrement_mismatch", base(), func(b *Builder) (string, []any, error) { return b.ToDecrement([]string{"age"}, nil) }, ErrIncrementColumns},
		{"delete_join_missing", base(), (*Builder).ToDeleteJoin, ErrDeleteJoinNoJoin},
		{"where_sub_invalid", base().WhereExists(42), (*Builder).ToSelect, ErrInvalidSubQuery},
		{"join_in_invalid", base().JoinOn("profiles", func(j *JoinBuilder) { j.WhereIn("id", 42) }), (*Builder).ToSelect, ErrInvalidWhereInValues},
	}
	for _, ignore := range []bool{false, true} {
		prefix := "insert_using_"
		if ignore {
			prefix = "insert_ignore_using_"
		}
		for _, subCase := range []struct {
			name      string
			configure func(*Builder)
			want      error
		}{
			{"empty", func(sub *Builder) { sub.Comment("inner:error") }, ErrEmptyTable},
			{"invalid", func(sub *Builder) { sub.Table("source").Where("id", "INVALID", 1).Comment("inner:error") }, ErrInvalidOperator},
			{"mismatch", func(sub *Builder) { sub.Table("source").Select("name", "age").Comment("inner:error") }, ErrInsertUsingColumnMismatch},
			{"cycle", func(sub *Builder) { sub.Table("source").Select("name").Comment("inner:error").TableSub(sub, "self") }, ErrCyclicQuery},
		} {
			cases = append(cases, errorCase{prefix + subCase.name, base(), func(b *Builder) (string, []any, error) {
				if ignore {
					return b.ToInsertOrIgnoreUsing([]string{"name"}, subCase.configure)
				}
				return b.ToInsertUsing([]string{"name"}, subCase.configure)
			}, subCase.want})
		}
	}
	// 环错误仅适用于会读取结构化查询图的编译入口，INSERT VALUES/TRUNCATE 不读取它。
	for _, tc := range commentCompileFixtures(g) {
		switch tc.name {
		case "select", "update", "delete", "delete_join", "count", "exists", "aggregate", "increment", "decrement":
			b := base()
			b.WhereExists(b)
			cases = append(cases, errorCase{tc.name + "_cycle", b, tc.compile, ErrCyclicQuery})
		}
	}
	switch g.(type) {
	case *PostgresGrammar:
		cases = append(cases, errorCase{"union_lock", base().Union(NewBuilder(g, nil).Table("admins")).LockForUpdate(), (*Builder).ToSelect, ErrPgUnionLockNotSupported})
	case *SQLiteGrammar:
		cases = append(cases,
			errorCase{"lock", base().LockForUpdate(), (*Builder).ToSelect, ErrSQLiteLockNotSupported},
			errorCase{"shared_lock", base().SharedLock(), (*Builder).ToSelect, ErrSQLiteLockNotSupported})
	}
	if _, mysql := g.(*MySQLGrammar); !mysql {
		cases = append(cases, errorCase{"upsert_unique_required", base(), func(b *Builder) (string, []any, error) { return b.ToUpsert(row, nil, nil) }, ErrUpsertUniqueByRequired})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := snapshotCommentCompileState(tc.b)
			for range 2 {
				sql, args, err := tc.compile(tc.b)
				assertCommentCompileError(t, tc.want, sql, args, err)
				assertCommentCompileState(t, tc.b, before)
			}
		})
	}
}

func assertCommentCompileCases(t *testing.T, g Grammar, expected map[string]commentCompileExpected) {
	t.Helper()
	fixtures := commentCompileFixtures(g)
	if len(expected) != len(fixtures)+2 {
		t.Fatalf("期望矩阵应为编译用例加两条恢复基线：%d != %d", len(expected), len(fixtures)+2)
	}
	for _, tc := range fixtures {
		t.Run(tc.name, func(t *testing.T) {
			want, ok := expected[tc.name]
			if !ok {
				t.Fatalf("缺少 %s 的字面量期望", tc.name)
			}
			for _, mode := range []struct{ name, comment, suffix string }{
				{"baseline", "", ""},
				{"comment", "app:compile ? $1", " /* app:compile ? $1 */"},
				{"cleared", "", ""},
			} {
				t.Run(mode.name, func(t *testing.T) {
					// 每个模式独立初始化，单独筛选 cleared 也确实从有注释状态开始。
					tc := tc
					tc.b = tc.b.Clone().Primary()
					if mode.name == "cleared" {
						tc.b.Comment("previous:query")
					}
					before := snapshotCommentCompileState(tc.b)
					before.comment = mode.comment
					tc.b.Comment(mode.comment)
					assertCommentCompileState(t, tc.b, before)
					for range 2 {
						sql, args, err := tc.compile(tc.b)
						assertCommentCompileResult(t, commentCompileExpected{want.sql + mode.suffix, want.args}, sql, args, err)
						assertCommentCompileState(t, tc.b, before)
						switch tc.name {
						case "count", "count_union", "count_group", "count_distinct", "exists", "exists_union", "aggregate", "aggregate_union":
							selectName := "select"
							switch tc.name {
							case "count_union", "exists_union", "aggregate_union":
								selectName = "select_union"
							case "count_group":
								selectName = "select_group"
							case "count_distinct":
								selectName = "select_distinct"
							}
							selectWant, ok := expected[selectName]
							if !ok {
								t.Fatalf("缺少 %s 恢复基线", selectName)
							}
							if before.lockClause != "" {
								selectWant.sql += " FOR UPDATE"
							}
							sql, args, err = tc.b.ToSelect()
							assertCommentCompileResult(t, commentCompileExpected{selectWant.sql + mode.suffix, selectWant.args}, sql, args, err)
							assertCommentCompileState(t, tc.b, before)
						}
					}
					if tc.direct != nil {
						// C10：十个公开 Grammar 编译方法均不读取 Builder 注释。
						assertSQL(t, want.sql, tc.direct(tc.b))
						assertCommentCompileState(t, tc.b, before)
					}
				})
			}
		})
	}
}

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// testComment 是整套测试统一注入的短业务标识。除注释专项用例外，所有通过 Builder 构造 SQL 的
// 用例都携带它（DAO 默认值或 newTestBuilder），用于顺带验证 Comment 不干扰既有行为；
// 这些用例不断言注释本身，只要求注释之外的正文字节不变（见 assertSQL 的容忍规则）。
const testComment = "zcdb:test"

// testCommentSuffix 是 testComment 规范化后的固定后缀，供容忍式断言与期望值拼接使用。
const testCommentSuffix = " /* " + testComment + " */"

// newTestBuilder 等价 NewBuilder + Comment(testComment)：语法路径（无 DAO）的测试统一经此构造，
// 使编译产物始终带注释；注释专项用例不得使用它，以免掩盖注释状态。
func newTestBuilder(g Grammar, dao *DBDao) *Builder {
	return NewBuilder(g, dao).Comment(testComment)
}

// stripTestComment 剥离统一测试注释后缀：非注释专项用例中未走 assertSQL 的本地 SQL 比较
// （直接 !=、HasSuffix 等）先剥离再比对正文。后缀被重复追加或插错位置时剥离一次后仍不相等，
// 因此这些比较仍能发现注释干扰。
func stripTestComment(sql string) string {
	return strings.TrimSuffix(sql, testCommentSuffix)
}

// assertSQL 比较 SQL 正文：接受"正文"或"正文 + 统一测试注释后缀"两种形式。
// 非注释专项用例只校验正文不受干扰，因此不重复书写注释文本；注释被插错位置、
// 重复追加或泄漏进子查询都会导致正文不一致而失败。注释专项用例自带完整期望值，
// 走第一分支精确匹配（其 builder 不携带 testComment）。
func assertSQL(t *testing.T, expected, actual string) {
	t.Helper()
	if actual == expected || actual == expected+testCommentSuffix {
		return
	}
	t.Errorf("SQL mismatch:\n  expected: %s\n      or: %s\n  actual:   %s", expected, expected+testCommentSuffix, actual)
}

// assertArgs 类型敏感地断言参数列表：长度与每个位置的值、动态类型都必须一致。
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
		if !argsEqual(expected[i], actual[i]) {
			t.Errorf("args[%d] mismatch: expected %v (%T), got %v (%T)", i, expected[i], expected[i], actual[i], actual[i])
		}
	}
}

// argsEqual 比较单个参数：类型敏感（int(1) 与 int64(1) 判为不等），
// 且对 []byte、切片、map 等不可用 == 比较的动态类型也安全——直接用 != 比较接口值
// 在这些类型上会 panic，故统一走 reflect.DeepEqual（它同样要求动态类型一致）。
// nil 单独处理：reflect.DeepEqual(nil, nil) 返回 false，且无类型 nil 与
// 有类型 nil 指针应判为不等。
func argsEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == nil && actual == nil
	}
	return reflect.DeepEqual(expected, actual)
}

// assertPgPlaceholderSequence 验证 PG 方言 SQL 中的 $N 占位符与 args 索引一一对应：
// 按出现顺序的编号必须是 1、2、…、argCount，即无跳号、无重复、无越界。
func assertPgPlaceholderSequence(t *testing.T, sql string, argCount int) {
	t.Helper()
	var nums []int
	for i := 0; i < len(sql); i++ {
		if sql[i] != '$' {
			continue
		}
		j := i + 1
		for j < len(sql) && sql[j] >= '0' && sql[j] <= '9' {
			j++
		}
		if j == i+1 {
			continue
		}
		n, err := strconv.Atoi(sql[i+1 : j])
		if err != nil {
			t.Fatalf("解析占位符编号失败 %q: %v", sql[i:j], err)
		}
		nums = append(nums, n)
		i = j - 1
	}
	if len(nums) != argCount {
		t.Errorf("占位符数量与 args 长度不符: 占位符 %v, args 长度 %d\n  SQL: %s", nums, argCount, sql)
		return
	}
	for idx, n := range nums {
		if n != idx+1 {
			t.Errorf("占位符编号应从 $1 起连续递增: 第 %d 个为 $%d\n  SQL: %s", idx+1, n, sql)
			return
		}
	}
}

// TestArgsEqual_TypeSensitiveAndPanicSafe 锁死 argsEqual 的比较语义：
// 类型敏感（int vs int64、无类型 nil vs 有类型 nil 指针均判为不等），
// 且对 []byte / map 等不可用 == 比较的动态类型不会 panic。
func TestArgsEqual_TypeSensitiveAndPanicSafe(t *testing.T) {
	var nilPtr *int
	tests := []struct {
		name           string
		expected       any
		actual         any
		wantEqualValue bool
	}{
		{"both_untyped_nil", nil, nil, true},
		{"untyped_nil_vs_typed_nil_ptr", nil, nilPtr, false},
		{"typed_nil_ptr_vs_typed_nil_ptr", nilPtr, (*int)(nil), true},
		{"untyped_nil_vs_value", nil, 1, false},
		{"int_vs_int64", 1, int64(1), false},
		{"int_vs_int", 1, 1, true},
		{"bytes_equal", []byte("abc"), []byte("abc"), true},
		{"bytes_differ", []byte("abc"), []byte("abd"), false},
		{"bytes_vs_string", []byte("abc"), "abc", false},
		{"map_equal", map[string]int{"a": 1}, map[string]int{"a": 1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 旧实现对 []byte / map 用 != 比较接口值会 panic，此处必须正常返回结果
			if got := argsEqual(tt.expected, tt.actual); got != tt.wantEqualValue {
				t.Errorf("argsEqual(%#v, %#v) = %v, want %v", tt.expected, tt.actual, got, tt.wantEqualValue)
			}
		})
	}
}

// assertInsertUsingColumnMismatch 验证给定方言下 ToInsertUsing/ToInsertOrIgnoreUsing 的列数校验：
// 子查询显式 Select 且不含 * 时列数必须与目标列数一致，不一致返回 ErrInsertUsingColumnMismatch；
// 列数一致、SELECT *、未显式 Select（默认 *）时通过编译，后两者由数据库运行时校验。
func assertInsertUsingColumnMismatch(t *testing.T, g Grammar) {
	t.Helper()
	checkUsing := func(columns []string, subFn func(*Builder)) error {
		b := newTestBuilder(g, nil).Table("archive")
		_, _, err := b.ToInsertUsing(columns, subFn)
		return err
	}
	// 列数不一致 → 哨兵错误
	if err := checkUsing([]string{"name", "age"}, func(sub *Builder) {
		sub.Table("users").Select("name")
	}); !errors.Is(err, ErrInsertUsingColumnMismatch) {
		t.Errorf("ToInsertUsing 列数不一致: expected ErrInsertUsingColumnMismatch, got %v", err)
	}
	// 列数一致 → 无错误
	if err := checkUsing([]string{"name"}, func(sub *Builder) {
		sub.Table("users").Select("name")
	}); err != nil {
		t.Errorf("ToInsertUsing 列数一致: unexpected error %v", err)
	}
	// SELECT * → 无法静态判定，不报错（运行时校验）
	if err := checkUsing([]string{"name"}, func(sub *Builder) {
		sub.Table("users").Select("*")
	}); err != nil {
		t.Errorf("ToInsertUsing SELECT *: unexpected error %v", err)
	}
	// 未显式 Select → 默认 SELECT *，不报错（运行时校验）
	if err := checkUsing([]string{"name"}, func(sub *Builder) {
		sub.Table("users")
	}); err != nil {
		t.Errorf("ToInsertUsing 默认列: unexpected error %v", err)
	}
	// ToInsertOrIgnoreUsing 同样校验
	b := newTestBuilder(g, nil).Table("archive")
	_, _, err := b.ToInsertOrIgnoreUsing([]string{"name", "age"}, func(sub *Builder) {
		sub.Table("users").Select("name")
	})
	if !errors.Is(err, ErrInsertUsingColumnMismatch) {
		t.Errorf("ToInsertOrIgnoreUsing 列数不一致: expected ErrInsertUsingColumnMismatch, got %v", err)
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
			sql, args, err := newTestBuilder(tt.grammar, nil).
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
	_, _, err := newTestBuilder(g, nil).Table("t").ToUpsert("not a struct", []string{"id"}, nil)
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestBuilder_ToInsertOrIgnoreInvalidData 验证 ToInsertOrIgnore 传入非结构体时返回错误。
func TestBuilder_ToInsertOrIgnoreInvalidData(t *testing.T) {
	g := NewMySQLGrammar()
	_, _, err := newTestBuilder(g, nil).Table("t").ToInsertOrIgnore(123)
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
	_, _, err := newTestBuilder(g, nil).ToUpdate(d{Name: "x"})
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestBuilder_ToDeleteWithError 验证 ToDelete 携带累积错误时返回错误。
func TestBuilder_ToDeleteWithError(t *testing.T) {
	g := NewMySQLGrammar()
	b := newTestBuilder(g, nil).Table("t").Where("id", "EVIL", 1)
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
	_, _, err := extractInsertData([]d{}, "")
	if !errors.Is(err, ErrEmptyData) {
		t.Errorf("expected ErrEmptyData, got %v", err)
	}
}

// TestExtractInsertData_NonStructSlice_Builder 验证 extractInsertData 传入非结构体切片时返回错误。
func TestExtractInsertData_NonStructSlice_Builder(t *testing.T) {
	_, _, err := extractInsertData([]int{1, 2, 3}, "")
	if !errors.Is(err, ErrInvalidStruct) {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestExtractUpdateData_NonStruct_Builder 验证 extractUpdateData 传入非结构体时返回错误。
func TestExtractUpdateData_NonStruct_Builder(t *testing.T) {
	_, _, err := extractUpdateData("not a struct", "")
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
			builder:  newTestBuilder(&MySQLGrammar{}, nil).Table("users").SelectRaw("1"),
			expected: "SELECT 1 FROM `users`",
		},
		{
			name:     "MySQL_SelectRaw_算术表达式",
			grammar:  &MySQLGrammar{},
			builder:  newTestBuilder(&MySQLGrammar{}, nil).Table("users").SelectRaw("age + 1 AS age_plus"),
			expected: "SELECT age + 1 AS age_plus FROM `users`",
		},
		{
			name:     "MySQL_SelectRaw_混合普通列",
			grammar:  &MySQLGrammar{},
			builder:  newTestBuilder(&MySQLGrammar{}, nil).Table("users").Select("name", "age").SelectRaw("1"),
			expected: "SELECT `name`, `age`, 1 FROM `users`",
		},
		{
			name:     "PostgreSQL_SelectRaw_数字字面量",
			grammar:  &PostgresGrammar{},
			builder:  newTestBuilder(&PostgresGrammar{}, nil).Table("users").SelectRaw("1"),
			expected: `SELECT 1 FROM "users"`,
		},
		{
			name:     "PostgreSQL_SelectRaw_混合普通列",
			grammar:  &PostgresGrammar{},
			builder:  newTestBuilder(&PostgresGrammar{}, nil).Table("users").Select("name").SelectRaw("1"),
			expected: `SELECT "name", 1 FROM "users"`,
		},
		{
			name:     "SQLite_SelectRaw_数字字面量",
			grammar:  &SQLiteGrammar{},
			builder:  newTestBuilder(&SQLiteGrammar{}, nil).Table("users").SelectRaw("1"),
			expected: `SELECT 1 FROM "users"`,
		},
		{
			name:     "SQLite_SelectRaw_混合普通列",
			grammar:  &SQLiteGrammar{},
			builder:  newTestBuilder(&SQLiteGrammar{}, nil).Table("users").Select("name").SelectRaw("1"),
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
	b := newTestBuilder(g, nil).Table("users").
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
	b := newTestBuilder(g, nil).Table("users").Select("name").SelectRaw("1")
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
			return newTestBuilder(g, nil).Table("users").WhereNot(func(q *Builder) {
				q.Where("status", "=", "active")
			})
		}, "SELECT * FROM `users` WHERE NOT (`status` = ?)", []any{"active"}},
		{"OrWhereNot", func() *Builder {
			return newTestBuilder(g, nil).Table("users").
				Where("id", "=", 1).
				OrWhereNot(func(q *Builder) { q.Where("age", ">", 18) })
		}, "SELECT * FROM `users` WHERE `id` = ? OR NOT (`age` > ?)", []any{1, 18}},
		{"WhereAll", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereAll(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE (`a` = ? AND `b` = ?)", []any{1, 2}},
		{"WhereAny", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereAny(func(q *Builder) {
				q.Where("a", 1).Where("b", 2)
			})
		}, "SELECT * FROM `users` WHERE (`a` = ? OR `b` = ?)", []any{1, 2}},
		{"WhereNone", func() *Builder {
			return newTestBuilder(g, nil).Table("users").WhereNone(func(q *Builder) {
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
			return newTestBuilder(g, nil).Table("users").SelectRaw("status, COUNT(*) AS cnt").
				GroupBy("status").Having("cnt", 5)
		}, "SELECT status, COUNT(*) AS cnt FROM `users` GROUP BY `status` HAVING `cnt` = ?", []any{5}},
		{"HavingNested", func() *Builder {
			return newTestBuilder(g, nil).Table("orders").GroupBy("user_id").
				HavingNested(func(q *Builder) {
					q.Having("total", ">", 100).Having("count", "<", 10)
				})
		}, "SELECT * FROM `orders` GROUP BY `user_id` HAVING (`total` > ? AND `count` < ?)", []any{100, 10}},
		{"OrHavingNested", func() *Builder {
			return newTestBuilder(g, nil).Table("orders").GroupBy("user_id").
				Having("total", ">", 250).
				OrHavingNested(func(q *Builder) { q.Having("total", "=", 30) })
		}, "SELECT * FROM `orders` GROUP BY `user_id` HAVING `total` > ? OR (`total` = ?)", []any{250, 30}},
		{"HavingNull", func() *Builder {
			return newTestBuilder(g, nil).Table("users").GroupBy("dept_id").HavingNull("email")
		}, "SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NULL", nil},
		{"HavingNotNullMulti", func() *Builder {
			return newTestBuilder(g, nil).Table("users").GroupBy("dept_id").HavingNotNull("email", "age")
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

// TestNewApi_HavingNil 验证 Having 传 nil 时按运算符编译：= nil → IS NULL、
// != / <> nil → IS NOT NULL（与 Where 对齐，避免永假的 HAVING = NULL）；
// 其它运算符仍生成占位符并绑定 nil。
func TestNewApi_HavingNil(t *testing.T) {
	tests := []struct {
		name    string
		grammar Grammar
		eqSQL   string
		neSQL   string
		gtSQL   string
	}{
		{"mysql", NewMySQLGrammar(),
			"SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NULL",
			"SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` IS NOT NULL",
			"SELECT * FROM `users` GROUP BY `dept_id` HAVING `email` > ?"},
		{"postgres", NewPostgresGrammar(),
			`SELECT * FROM "users" GROUP BY "dept_id" HAVING "email" IS NULL`,
			`SELECT * FROM "users" GROUP BY "dept_id" HAVING "email" IS NOT NULL`,
			`SELECT * FROM "users" GROUP BY "dept_id" HAVING "email" > $1`},
		{"sqlite", NewSQLiteGrammar(),
			`SELECT * FROM "users" GROUP BY "dept_id" HAVING "email" IS NULL`,
			`SELECT * FROM "users" GROUP BY "dept_id" HAVING "email" IS NOT NULL`,
			`SELECT * FROM "users" GROUP BY "dept_id" HAVING "email" > ?`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newTestBuilder(tt.grammar, nil).Table("users").GroupBy("dept_id").Having("email", "=", nil)
			sql, args, err := b.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.eqSQL, sql)
			assertArgs(t, nil, args)

			b = newTestBuilder(tt.grammar, nil).Table("users").GroupBy("dept_id").Having("email", "!=", nil)
			sql, args, err = b.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.neSQL, sql)
			assertArgs(t, nil, args)

			b = newTestBuilder(tt.grammar, nil).Table("users").GroupBy("dept_id").Having("email", ">", nil)
			sql, args, err = b.ToSelect()
			assertNoError(t, err)
			assertSQL(t, tt.gtSQL, sql)
			assertArgs(t, []any{nil}, args)
		})
	}
}

// TestNewApi_ToAggregate 验证 ToAggregate 编译形态、UNION 包裹与非法聚合错误。
func TestNewApi_ToAggregate(t *testing.T) {
	t.Run("MySQL", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := newTestBuilder(g, nil).Table("users").ToAggregate("MAX", "age")
		assertNoError(t, err)
		assertSQL(t, "SELECT MAX(`age`) AS `aggregate` FROM `users`", sql)
		assertArgs(t, nil, args)
	})

	t.Run("Postgres", func(t *testing.T) {
		g := NewPostgresGrammar()
		sql, _, err := newTestBuilder(g, nil).Table("users").ToAggregate("MIN", "age")
		assertNoError(t, err)
		assertSQL(t, `SELECT MIN("age") AS "aggregate" FROM "users"`, sql)
	})

	t.Run("SQLite", func(t *testing.T) {
		g := NewSQLiteGrammar()
		sql, _, err := newTestBuilder(g, nil).Table("users").ToAggregate("AVG", "age")
		assertNoError(t, err)
		assertSQL(t, `SELECT AVG("age") AS "aggregate" FROM "users"`, sql)
	})

	t.Run("WithWhere", func(t *testing.T) {
		g := NewMySQLGrammar()
		sql, args, err := newTestBuilder(g, nil).Table("users").
			Where("status", "=", "active").ToAggregate("SUM", "age")
		assertNoError(t, err)
		assertSQL(t, "SELECT SUM(`age`) AS `aggregate` FROM `users` WHERE `status` = ?", sql)
		assertArgs(t, []any{"active"}, args)
	})

	t.Run("UnionWrap", func(t *testing.T) {
		g := NewMySQLGrammar()
		b := newTestBuilder(g, nil).Table("orders_a").
			Union(newTestBuilder(g, nil).Table("orders_b"))
		sql, _, err := b.ToAggregate("SUM", "amount")
		assertNoError(t, err)
		assertSQL(t,
			"SELECT SUM(`amount`) AS `aggregate` FROM ((SELECT * FROM `orders_a`) UNION (SELECT * FROM `orders_b`)) AS `t`",
			sql)
	})

	t.Run("InvalidAggregate", func(t *testing.T) {
		g := NewMySQLGrammar()
		_, _, err := newTestBuilder(g, nil).Table("users").ToAggregate("COUNT", "age")
		if !errors.Is(err, ErrInvalidAggregate) {
			t.Errorf("expected ErrInvalidAggregate, got %v", err)
		}
	})

	t.Run("StateRestored", func(t *testing.T) {
		// 编译后 Builder 状态应恢复，不影响后续 ToSelect
		g := NewMySQLGrammar()
		b := newTestBuilder(g, nil).Table("users").Select("name").Limit(10)
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
		sql, args, err := newTestBuilder(g, nil).Table("wallets").
			Where("id", "=", 1).
			ToIncrement([]string{"balance", "points"}, []any{10, 5})
		assertNoError(t, err)
		assertSQL(t, "UPDATE `wallets` SET `balance` = `balance` + ?, `points` = `points` + ? WHERE `id` = ?", sql)
		assertArgs(t, []any{10, 5, 1}, args)
	})

	t.Run("MySQL_JoinSetAfterJoin", func(t *testing.T) {
		// MySQL：JOIN → SET → WHERE 绑定顺序
		g := NewMySQLGrammar()
		sql, args, err := newTestBuilder(g, nil).Table("users").
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
		sql, args, err := newTestBuilder(g, nil).Table("users").
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
		sql, args, err := newTestBuilder(g, nil).Table("wallets").
			Where("id", "=", 1).
			ToDecrement([]string{"balance"}, []any{30})
		assertNoError(t, err)
		assertSQL(t, `UPDATE "wallets" SET "balance" = "balance" - ? WHERE "id" = ?`, sql)
		assertArgs(t, []any{30, 1}, args)
	})

	t.Run("ColumnsMismatch", func(t *testing.T) {
		g := NewMySQLGrammar()
		_, _, err := newTestBuilder(g, nil).Table("wallets").
			ToIncrement([]string{"balance"}, []any{10, 5})
		if !errors.Is(err, ErrIncrementColumns) {
			t.Errorf("expected ErrIncrementColumns, got %v", err)
		}
		_, _, err = newTestBuilder(g, nil).Table("wallets").ToIncrement(nil, nil)
		if !errors.Is(err, ErrIncrementColumns) {
			t.Errorf("expected ErrIncrementColumns, got %v", err)
		}
	})
}

// TestNewApi_ToDeleteJoin 验证 ToDeleteJoin 的校验错误路径（编译形态已由集成测试覆盖）。
func TestNewApi_ToDeleteJoin(t *testing.T) {
	g := NewMySQLGrammar()

	// 无 JOIN → ErrDeleteJoinNoJoin
	_, _, err := newTestBuilder(g, nil).Table("users").Where("id", "=", 1).ToDeleteJoin()
	if !errors.Is(err, ErrDeleteJoinNoJoin) {
		t.Errorf("expected ErrDeleteJoinNoJoin, got %v", err)
	}

	// 无表名 → ErrEmptyTable
	_, _, err = newTestBuilder(g, nil).Join("orders", "a", "=", "b").ToDeleteJoin()
	if !errors.Is(err, ErrEmptyTable) {
		t.Errorf("expected ErrEmptyTable, got %v", err)
	}
}

// TestNewApi_WhereShorthandInvalid 验证 Where 三参形式的非法运算符错误。
func TestNewApi_WhereShorthandInvalid(t *testing.T) {
	g := NewMySQLGrammar()

	// 三参形式 op 非 string → ErrInvalidOperator
	_, _, err := newTestBuilder(g, nil).Table("users").Where("age", 25, 30).ToSelect()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}

	// 三参形式非法运算符 → ErrInvalidOperator
	_, _, err = newTestBuilder(g, nil).Table("users").Where("age", "DROP", 30).ToSelect()
	if !errors.Is(err, ErrInvalidOperator) {
		t.Errorf("expected ErrInvalidOperator, got %v", err)
	}
}

// TestNewApi_SelectSubCompile 验证 SelectSub 标量子查询列的编译形态（PG $N）。
func TestNewApi_SelectSubCompile(t *testing.T) {
	t.Run("Postgres", func(t *testing.T) {
		g := NewPostgresGrammar()
		sub := newTestBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").WhereRaw("orders.user_id = users.id")
		sql, _, err := newTestBuilder(g, nil).Table("users").
			Select("id").SelectSub(sub, "order_count").ToSelect()
		assertNoError(t, err)
		assertSQL(t,
			`SELECT "id", (SELECT COUNT(*) FROM "orders" WHERE orders.user_id = users.id) AS "order_count" FROM "users"`,
			sql)
	})

	t.Run("MySQL", func(t *testing.T) {
		g := NewMySQLGrammar()
		sub := newTestBuilder(g, nil).Table("orders").SelectRaw("COUNT(*)").Where("amount", ">", 100)
		sql, args, err := newTestBuilder(g, nil).Table("users").
			Select("id").SelectSub(sub, "order_count").ToSelect()
		assertNoError(t, err)
		assertSQL(t,
			"SELECT `id`, (SELECT COUNT(*) FROM `orders` WHERE `amount` > ?) AS `order_count` FROM `users`",
			sql)
		assertArgs(t, []any{100}, args)
	})
}

// ==================== 子查询图环检测（防栈溢出）回归测试 ====================

// TestBuilder_CyclicTableSub 自引用 FROM 子查询（TableSub 传入自身）应在编译前返回
// ErrCyclicQuery，而非陷入无限递归导致栈溢出。
func TestBuilder_CyclicTableSub(t *testing.T) {
	b := newTestBuilder(&MySQLGrammar{}, nil)
	b.TableSub(b, "x")
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for self-referential TableSub, got %v", err)
	}
}

// TestBuilder_CyclicUnion 自引用 UNION（Union 传入自身）应返回 ErrCyclicQuery。
func TestBuilder_CyclicUnion(t *testing.T) {
	b := newTestBuilder(&MySQLGrammar{}, nil).Table("users")
	b.Union(b)
	if _, _, err := b.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for self-referential Union, got %v", err)
	}
}

// TestBuilder_CyclicClone Clone 遇环引用时不应递归崩溃，而应返回携带错误的副本，
// 使后续编译方法经 b.err 前置检查返回 ErrCyclicQuery。
func TestBuilder_CyclicClone(t *testing.T) {
	b := newTestBuilder(&MySQLGrammar{}, nil)
	b.TableSub(b, "x")
	clone := b.Clone()
	if _, _, err := clone.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery from cloned cyclic builder, got %v", err)
	}
}

// TestBuilder_CyclicMutual 互引用（a.TableSub(b) 且 b.Union(a)）应返回 ErrCyclicQuery。
func TestBuilder_CyclicMutual(t *testing.T) {
	a := newTestBuilder(&MySQLGrammar{}, nil).Table("users")
	b := newTestBuilder(&MySQLGrammar{}, nil).Table("orders")
	a.TableSub(b, "o")
	b.Union(a)
	if _, _, err := a.ToSelect(); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for mutual cycle, got %v", err)
	}
}

// TestBuilder_CyclicJoinSubUpdate 写路径（UPDATE）经 JoinSub 自引用也应返回 ErrCyclicQuery，
// 覆盖非 SELECT 编译入口的环检测。
func TestBuilder_CyclicJoinSubUpdate(t *testing.T) {
	b := newTestBuilder(&MySQLGrammar{}, nil).Table("users")
	b.JoinSub(b, "x", nil)
	if _, _, err := b.ToUpdate(userInsert{Name: "alice"}); !errors.Is(err, ErrCyclicQuery) {
		t.Fatalf("expected ErrCyclicQuery for cyclic join sub in UPDATE, got %v", err)
	}
}

// TestBuilder_AcyclicSharedSubquery 同一子查询被多次引用（菱形引用，非环）不应误报，应正常编译。
func TestBuilder_AcyclicSharedSubquery(t *testing.T) {
	g := NewMySQLGrammar()
	shared := newTestBuilder(g, nil).Table("orders").Where("status", "=", "paid")
	b := newTestBuilder(g, nil).Table("users").
		SelectSub(shared, "cnt_a").
		SelectSub(shared, "cnt_b")
	sql, args, err := b.ToSelect()
	if err != nil {
		t.Fatalf("expected no error for shared (diamond) subquery, got %v", err)
	}
	if !strings.Contains(sql, "AS `cnt_a`") || !strings.Contains(sql, "AS `cnt_b`") {
		t.Errorf("expected both subquery aliases in SQL, got %q", sql)
	}
	// 两个子查询各绑定一次 "paid"
	assertArgs(t, []any{"paid", "paid"}, args)
}
