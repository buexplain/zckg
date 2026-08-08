package zcdb

import (
	"reflect"
	"testing"
)

// ==================== parseStruct 测试 ====================

// TestParseStruct_BasicStruct 验证基本结构体解析。
func TestParseStruct_BasicStruct(t *testing.T) {
	type User struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Age  int
	}

	info := parseStruct(reflect.TypeOf(User{}), "")
	if info == nil {
		t.Fatal("parseStruct returned nil")
	}
	if len(info.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(info.Fields))
	}

	expected := []struct {
		column string
		index  []int
	}{
		{"id", []int{0}},
		{"name", []int{1}},
		{"age", []int{2}}, // 无标签自动转 snake_case
	}

	for i, e := range expected {
		if info.Fields[i].Column != e.column {
			t.Errorf("field %d: expected column %q, got %q", i, e.column, info.Fields[i].Column)
		}
		if !reflect.DeepEqual(info.Fields[i].Index, e.index) {
			t.Errorf("field %d: expected index %v, got %v", i, e.index, info.Fields[i].Index)
		}
	}
}

// TestParseStruct_DbTagSkip 验证 db:"-" 跳过字段。
func TestParseStruct_DbTagSkip(t *testing.T) {
	type User struct {
		ID     int    `db:"id"`
		Secret string `db:"-"`
		Name   string `db:"name"`
	}

	info := parseStruct(reflect.TypeOf(User{}), "")
	if info == nil {
		t.Fatal("parseStruct returned nil")
	}
	if len(info.Fields) != 2 {
		t.Fatalf("expected 2 fields (secret skipped), got %d", len(info.Fields))
	}
	for _, f := range info.Fields {
		if f.Column == "secret" {
			t.Error("field 'secret' should be skipped")
		}
	}
}

// TestParseStruct_UnexportedFields 验证未导出字段被跳过。
func TestParseStruct_UnexportedFields(t *testing.T) {
	type User struct {
		ID   int    `db:"id"`
		name string `db:"name"` // 未导出
		Age  int    `db:"age"`
	}

	info := parseStruct(reflect.TypeOf(User{}), "")
	if len(info.Fields) != 2 {
		t.Fatalf("expected 2 fields (unexported skipped), got %d", len(info.Fields))
	}
}

// TestParseStruct_PointerType 验证指针类型自动解引用。
func TestParseStruct_PointerType(t *testing.T) {
	type User struct {
		ID int `db:"id"`
	}

	info := parseStruct(reflect.TypeOf(&User{}), "")
	if info == nil {
		t.Fatal("parseStruct returned nil for pointer type")
	}
	if len(info.Fields) != 1 || info.Fields[0].Column != "id" {
		t.Error("pointer type should be dereferenced")
	}
}

// TestParseStruct_NonStruct 验证非结构体返回 nil。
func TestParseStruct_NonStruct(t *testing.T) {
	if info := parseStruct(reflect.TypeOf(123), ""); info != nil {
		t.Error("expected nil for int type")
	}
	if info := parseStruct(reflect.TypeOf("hello"), ""); info != nil {
		t.Error("expected nil for string type")
	}
	if info := parseStruct(reflect.TypeOf([]int{}), ""); info != nil {
		t.Error("expected nil for slice type")
	}
}

// TestParseStruct_EmbeddedStruct 验证嵌入结构体字段展开。
func TestParseStruct_EmbeddedStruct(t *testing.T) {
	type Base struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	type User struct {
		Base
		Age int `db:"age"`
	}

	info := parseStruct(reflect.TypeOf(User{}), "")
	if info == nil {
		t.Fatal("parseStruct returned nil")
	}
	if len(info.Fields) != 3 {
		t.Fatalf("expected 3 fields (embedded expanded), got %d", len(info.Fields))
	}

	expected := map[string][]int{
		"id":   {0, 0},
		"name": {0, 1},
		"age":  {1},
	}

	for _, f := range info.Fields {
		exp, ok := expected[f.Column]
		if !ok {
			t.Errorf("unexpected column %q", f.Column)
			continue
		}
		if !reflect.DeepEqual(f.Index, exp) {
			t.Errorf("column %q: expected index %v, got %v", f.Column, exp, f.Index)
		}
	}
}

// TestParseStruct_NestedEmbed 验证多层嵌入结构体。
func TestParseStruct_NestedEmbed(t *testing.T) {
	type L1 struct {
		A int `db:"a"`
	}
	type L2 struct {
		L1
		B int `db:"b"`
	}
	type L3 struct {
		L2
		C int `db:"c"`
	}

	info := parseStruct(reflect.TypeOf(L3{}), "")
	if len(info.Fields) != 3 {
		t.Fatalf("expected 3 fields, got %d", len(info.Fields))
	}

	expected := map[string][]int{
		"a": {0, 0, 0},
		"b": {0, 1},
		"c": {1},
	}

	for _, f := range info.Fields {
		exp := expected[f.Column]
		if !reflect.DeepEqual(f.Index, exp) {
			t.Errorf("column %q: expected index %v, got %v", f.Column, exp, f.Index)
		}
	}
}

// TestParseStruct_CacheHit 验证缓存命中。
func TestParseStruct_CacheHit(t *testing.T) {
	type User struct {
		ID int `db:"id"`
	}

	info1 := parseStruct(reflect.TypeOf(User{}), "")
	info2 := parseStruct(reflect.TypeOf(User{}), "")
	if info1 != info2 {
		t.Error("expected cache hit, got different pointers")
	}
}

// TestParseStruct_EmbeddedPtrStruct 验证嵌入指针结构体（*struct 匿名嵌入）字段展开。
// 当前行为（BUG）：嵌入条件限制为值类型结构体，*Base 不会被展开，
// 而是被当作普通字段（列名变为 base）。
func TestParseStruct_EmbeddedPtrStruct(t *testing.T) {
	type Base struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	type User struct {
		*Base
		Age int `db:"age"`
	}

	info := parseStruct(reflect.TypeOf(User{}), "")
	if info == nil {
		t.Fatal("parseStruct returned nil")
	}
	if len(info.Fields) != 3 {
		t.Fatalf("expected 3 fields (embedded ptr expanded), got %d: %+v", len(info.Fields), info.Fields)
	}

	expected := map[string][]int{
		"id":   {0, 0},
		"name": {0, 1},
		"age":  {1},
	}
	for _, f := range info.Fields {
		exp, ok := expected[f.Column]
		if !ok {
			t.Errorf("unexpected column %q", f.Column)
			continue
		}
		if !reflect.DeepEqual(f.Index, exp) {
			t.Errorf("column %q: expected index %v, got %v", f.Column, exp, f.Index)
		}
	}
}

// ==================== extractInsertData 测试 ====================

// TestExtractInsertData_SingleStruct 验证单个结构体。
func TestExtractInsertData_SingleStruct(t *testing.T) {
	type User struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}

	cols, rows, err := extractInsertData(User{Name: "alice", Age: 25}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 || len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("unexpected result: cols=%v, rows=%v", cols, rows)
	}
}

// TestExtractInsertData_StructPointer 验证结构体指针。
func TestExtractInsertData_StructPointer(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}

	cols, rows, err := extractInsertData(&User{Name: "alice"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 1 || rows[0][0] != "alice" {
		t.Errorf("unexpected result: cols=%v, rows=%v", cols, rows)
	}
}

// TestExtractInsertData_Slice 验证结构体切片。
func TestExtractInsertData_Slice(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}

	cols, rows, err := extractInsertData([]User{{Name: "alice"}, {Name: "bob"}}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 1 || len(rows) != 2 {
		t.Fatalf("unexpected result: cols=%v, rows=%v", cols, rows)
	}
	if rows[0][0] != "alice" || rows[1][0] != "bob" {
		t.Errorf("unexpected row values")
	}
}

// TestExtractInsertData_PointerSlice 验证指针切片。
func TestExtractInsertData_PointerSlice(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}

	u1, u2 := &User{Name: "alice"}, &User{Name: "bob"}
	_, rows, err := extractInsertData([]*User{u1, u2}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 2 || rows[0][0] != "alice" || rows[1][0] != "bob" {
		t.Errorf("unexpected result")
	}
}

// TestExtractInsertData_NilInterfaceField 验证 nil interface 字段被跳过。
func TestExtractInsertData_NilInterfaceField(t *testing.T) {
	type User struct {
		Name  string `db:"name"`
		Extra any    `db:"extra"`
	}

	cols, _, err := extractInsertData(User{Name: "alice", Extra: nil}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range cols {
		if c == "extra" {
			t.Error("nil interface field should be skipped")
		}
	}
}

// TestExtractInsertData_NilPointerField 验证 nil 指针字段被跳过。
func TestExtractInsertData_NilPointerField(t *testing.T) {
	type User struct {
		Name  string  `db:"name"`
		Email *string `db:"email"`
	}

	cols, _, err := extractInsertData(User{Name: "alice", Email: nil}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, c := range cols {
		if c == "email" {
			t.Error("nil pointer field should be skipped")
		}
	}
}

// TestExtractInsertData_NonNilPointerField 验证非 nil 指针字段被解引用。
func TestExtractInsertData_NonNilPointerField(t *testing.T) {
	type User struct {
		Name  string  `db:"name"`
		Email *string `db:"email"`
	}

	email := "alice@test.com"
	cols, rows, err := extractInsertData(User{Name: "alice", Email: &email}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if rows[0][1] != "alice@test.com" {
		t.Errorf("expected dereferenced value, got %v", rows[0][1])
	}
}

// TestExtractInsertData_EmptySlice 验证空切片返回错误。
func TestExtractInsertData_EmptySlice(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}

	_, _, err := extractInsertData([]User{}, "")
	if err != ErrEmptyData {
		t.Errorf("expected ErrEmptyData, got %v", err)
	}
}

// TestExtractInsertData_NonStruct 验证非结构体返回错误。
func TestExtractInsertData_NonStruct(t *testing.T) {
	_, _, err := extractInsertData("not a struct", "")
	if err != ErrInvalidStruct {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestExtractInsertData_NilInput 验证 nil 输入返回错误。
func TestExtractInsertData_NilInput(t *testing.T) {
	_, _, err := extractInsertData(nil, "")
	if err != ErrInvalidStruct {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestExtractInsertData_ExpressionField 验证 Expression 字段直接取值。
func TestExtractInsertData_ExpressionField(t *testing.T) {
	type User struct {
		Name string     `db:"name"`
		Age  Expression `db:"age"`
	}

	_, rows, err := extractInsertData(User{Name: "alice", Age: NewExpression("25 + 1")}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("unexpected result: rows=%v", rows)
	}
	if _, ok := rows[0][1].(Expression); !ok {
		t.Errorf("Expression field should be preserved, got %T", rows[0][1])
	}
}

// TestExtractInsertData_SliceNilPointerInMiddle 验证切片中 nil 指针传入 NULL。
func TestExtractInsertData_SliceNilPointerInMiddle(t *testing.T) {
	type User struct {
		Name  string  `db:"name"`
		Email *string `db:"email"`
	}

	email := "alice@test.com"
	data := []*User{
		{Name: "alice", Email: &email},
		{Name: "bob", Email: nil}, // nil → SQL NULL
	}

	_, rows, err := extractInsertData(data, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows[0][1] != "alice@test.com" {
		t.Errorf("row 0: expected email, got %v", rows[0][1])
	}
	if rows[1][1] != nil {
		t.Errorf("row 1: expected nil (SQL NULL), got %v", rows[1][1])
	}
}

// TestExtractInsertData_EmbeddedPtrStruct 验证嵌入指针结构体的数据提取：
// 非 nil 时字段展开取值；nil 时不 panic 且跳过该嵌入字段。
func TestExtractInsertData_EmbeddedPtrStruct(t *testing.T) {
	type Base struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}
	type User struct {
		*Base
		Age int `db:"age"`
	}

	// 非 nil 嵌入指针：id/name/age 全部提取
	cols, rows, err := extractInsertData(&User{Base: &Base{ID: 1, Name: "alice"}, Age: 25}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns with non-nil embedded ptr, got %d: %v", len(cols), cols)
	}
	if len(rows) != 1 || len(rows[0]) != 3 {
		t.Fatalf("unexpected rows: %v", rows)
	}

	// nil 嵌入指针：不 panic，id/name 跳过，只剩 age
	cols, rows, err = extractInsertData(&User{Age: 30}, "")
	if err != nil {
		t.Fatalf("unexpected error with nil embedded ptr: %v", err)
	}
	if len(cols) != 1 || cols[0] != "age" {
		t.Fatalf("expected only age column with nil embedded ptr, got %v", cols)
	}
}

// ==================== extractUpdateData 测试 ====================

// TestExtractUpdateData_SingleStruct 验证单个结构体。
func TestExtractUpdateData_SingleStruct(t *testing.T) {
	type User struct {
		Name string `db:"name"`
		Age  int    `db:"age"`
	}

	cols, vals, err := extractUpdateData(User{Name: "alice", Age: 25}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 || len(vals) != 2 {
		t.Fatalf("unexpected result: cols=%v, vals=%v", cols, vals)
	}
}

// TestExtractUpdateData_StructPointer 验证结构体指针。
func TestExtractUpdateData_StructPointer(t *testing.T) {
	type User struct {
		Name string `db:"name"`
	}

	cols, vals, err := extractUpdateData(&User{Name: "alice"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 1 || vals[0] != "alice" {
		t.Errorf("unexpected result")
	}
}

// TestExtractUpdateData_NilInterfaceField 验证 nil interface 字段被跳过。
func TestExtractUpdateData_NilInterfaceField(t *testing.T) {
	type User struct {
		Name  string `db:"name"`
		Extra any    `db:"extra"`
	}

	cols, _, err := extractUpdateData(User{Name: "alice", Extra: nil}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 1 || cols[0] != "name" {
		t.Errorf("nil interface field should be skipped")
	}
}

// TestExtractUpdateData_NilPointerField 验证 nil 指针字段被跳过。
func TestExtractUpdateData_NilPointerField(t *testing.T) {
	type User struct {
		Name  string  `db:"name"`
		Email *string `db:"email"`
	}

	cols, _, err := extractUpdateData(User{Name: "alice", Email: nil}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 1 || cols[0] != "name" {
		t.Errorf("nil pointer field should be skipped")
	}
}

// TestExtractUpdateData_NonStruct 验证非结构体返回错误。
func TestExtractUpdateData_NonStruct(t *testing.T) {
	_, _, err := extractUpdateData(123, "")
	if err != ErrInvalidStruct {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestExtractUpdateData_NilInput 验证 nil 输入返回错误。
func TestExtractUpdateData_NilInput(t *testing.T) {
	_, _, err := extractUpdateData(nil, "")
	if err != ErrInvalidStruct {
		t.Errorf("expected ErrInvalidStruct, got %v", err)
	}
}

// TestExtractUpdateData_ExpressionField 验证 Expression 字段直接取值。
func TestExtractUpdateData_ExpressionField(t *testing.T) {
	type User struct {
		Name string     `db:"name"`
		Age  Expression `db:"age"`
	}

	cols, vals, err := extractUpdateData(User{Name: "alice", Age: NewExpression("age + 1")}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cols) != 2 {
		t.Fatalf("expected 2 columns, got %d", len(cols))
	}
	if _, ok := vals[1].(Expression); !ok {
		t.Errorf("Expression field should be preserved, got %T", vals[1])
	}
}

// ==================== toSnakeCase 测试 ====================

// TestToSnakeCase 验证 PascalCase/camelCase 转 snake_case。
func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"UserName", "user_name"},
		{"ID", "id"},
		{"HTTPCode", "http_code"},
		{"SimpleXML", "simple_xml"},
		{"getHTTPResponse", "get_http_response"},
		{"myField", "my_field"},
		{"name", "name"},
		{"A", "a"},
		{"AB", "ab"},
		{"ABCDef", "abc_def"},
		// Unicode 字段名：中文字符后的大写字母不应误插下划线
		{"AX中", "ax中"},
		{"用户Name", "用户_name"},
		{"中Name", "中_name"},
	}

	for _, tt := range tests {
		result := toSnakeCase(tt.input)
		if result != tt.expected {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}
