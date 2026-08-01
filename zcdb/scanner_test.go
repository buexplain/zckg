package zcdb

import (
	"reflect"
	"testing"
)

// ==================== getScanFieldInfo 测试 ====================

// TestGetScanFieldInfo_BasicStruct 验证基本结构体的字段映射。
func TestGetScanFieldInfo_BasicStruct(t *testing.T) {
	type User struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Age  int    // 无标签，自动转 snake_case
	}

	info := getScanFieldInfo(reflect.TypeOf(User{}))
	if info == nil {
		t.Fatal("getScanFieldInfo returned nil")
	}

	tests := []struct {
		column string
		index  []int
	}{
		{"id", []int{0}},
		{"name", []int{1}},
		{"age", []int{2}},
	}

	for _, tt := range tests {
		idx, ok := info.columnIndex[tt.column]
		if !ok {
			t.Errorf("column %q not found in columnIndex", tt.column)
			continue
		}
		if !reflect.DeepEqual(idx, tt.index) {
			t.Errorf("column %q: expected index %v, got %v", tt.column, tt.index, idx)
		}
	}
}

// TestGetScanFieldInfo_DbTagSkip 验证 db:"-" 跳过字段。
func TestGetScanFieldInfo_DbTagSkip(t *testing.T) {
	type User struct {
		ID     int    `db:"id"`
		Secret string `db:"-"`
		Name   string `db:"name"`
	}

	info := getScanFieldInfo(reflect.TypeOf(User{}))
	if info == nil {
		t.Fatal("getScanFieldInfo returned nil")
	}

	if _, ok := info.columnIndex["secret"]; ok {
		t.Error("column 'secret' should be skipped (db:\"-\")")
	}
	if _, ok := info.columnIndex["id"]; !ok {
		t.Error("column 'id' should be present")
	}
	if _, ok := info.columnIndex["name"]; !ok {
		t.Error("column 'name' should be present")
	}
}

// TestGetScanFieldInfo_UnexportedFields 验证未导出字段被跳过。
func TestGetScanFieldInfo_UnexportedFields(t *testing.T) {
	type User struct {
		ID   int    `db:"id"`
		name string `db:"name"` // 未导出
		Age  int    `db:"age"`
	}

	info := getScanFieldInfo(reflect.TypeOf(User{}))
	if info == nil {
		t.Fatal("getScanFieldInfo returned nil")
	}

	if _, ok := info.columnIndex["name"]; ok {
		t.Error("unexported field 'name' should be skipped")
	}
	if _, ok := info.columnIndex["id"]; !ok {
		t.Error("column 'id' should be present")
	}
	if _, ok := info.columnIndex["age"]; !ok {
		t.Error("column 'age' should be present")
	}
}

// TestGetScanFieldInfo_PointerType 验证指针类型自动解引用。
func TestGetScanFieldInfo_PointerType(t *testing.T) {
	type User struct {
		ID int `db:"id"`
	}

	info := getScanFieldInfo(reflect.TypeOf(&User{}))
	if info == nil {
		t.Fatal("getScanFieldInfo returned nil for pointer type")
	}

	if _, ok := info.columnIndex["id"]; !ok {
		t.Error("column 'id' should be present for pointer type")
	}
}

// TestGetScanFieldInfo_CacheHit 验证缓存命中。
func TestGetScanFieldInfo_CacheHit(t *testing.T) {
	type User struct {
		ID int `db:"id"`
	}

	// 第一次调用构建缓存
	info1 := getScanFieldInfo(reflect.TypeOf(User{}))
	// 第二次调用应命中缓存
	info2 := getScanFieldInfo(reflect.TypeOf(User{}))

	if info1 != info2 {
		t.Error("expected cache hit, got different pointers")
	}
}

// ==================== makeScanValues 测试 ====================

// TestMakeScanValues_MatchingColumns 验证列名匹配结构体字段。
func TestMakeScanValues_MatchingColumns(t *testing.T) {
	type User struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
	}

	info := &scanFieldInfo{
		columnIndex: map[string][]int{
			"id":   {0},
			"name": {1},
		},
	}

	user := User{}
	structValue := reflect.ValueOf(&user).Elem()
	values := makeScanValues([]string{"id", "name"}, info, structValue)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	// 验证返回值是指针类型
	for i, v := range values {
		if _, ok := v.(*int); i == 0 && !ok {
			t.Errorf("value[%d] should be *int, got %T", i, v)
		}
		if _, ok := v.(*string); i == 1 && !ok {
			t.Errorf("value[%d] should be *string, got %T", i, v)
		}
	}
}

// TestMakeScanValues_UnmatchedColumns 验证未匹配列使用 discard。
func TestMakeScanValues_UnmatchedColumns(t *testing.T) {
	type User struct {
		ID int `db:"id"`
	}

	info := &scanFieldInfo{
		columnIndex: map[string][]int{
			"id": {0},
		},
	}

	user := User{}
	structValue := reflect.ValueOf(&user).Elem()
	values := makeScanValues([]string{"id", "extra_col"}, info, structValue)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	// 第一个值应该是 *int
	if _, ok := values[0].(*int); !ok {
		t.Errorf("values[0] should be *int, got %T", values[0])
	}

	// 第二个值应该是 *discard
	if _, ok := values[1].(*discard); !ok {
		t.Errorf("values[1] should be *discard, got %T", values[1])
	}
}

// TestMakeScanValues_EmbeddedStruct 验证嵌入结构体字段匹配。
func TestMakeScanValues_EmbeddedStruct(t *testing.T) {
	type Base struct {
		ID int `db:"id"`
	}
	type User struct {
		Base
		Name string `db:"name"`
	}

	info := &scanFieldInfo{
		columnIndex: map[string][]int{
			"id":   {0, 0}, // Base.ID 的索引路径
			"name": {1},
		},
	}

	user := User{}
	structValue := reflect.ValueOf(&user).Elem()
	values := makeScanValues([]string{"id", "name"}, info, structValue)

	if len(values) != 2 {
		t.Fatalf("expected 2 values, got %d", len(values))
	}

	// 验证嵌入字段的指针正确
	if _, ok := values[0].(*int); !ok {
		t.Errorf("values[0] should be *int (embedded), got %T", values[0])
	}
	if _, ok := values[1].(*string); !ok {
		t.Errorf("values[1] should be *string, got %T", values[1])
	}
}

// ==================== discard 测试 ====================

// TestDiscard_Scan 验证 discard.Scan 始终返回 nil。
func TestDiscard_Scan(t *testing.T) {
	d := &discard{}
	if err := d.Scan(nil); err != nil {
		t.Errorf("discard.Scan(nil) should return nil, got %v", err)
	}
	if err := d.Scan("test"); err != nil {
		t.Errorf("discard.Scan(\"test\") should return nil, got %v", err)
	}
	if err := d.Scan(123); err != nil {
		t.Errorf("discard.Scan(123) should return nil, got %v", err)
	}
}
