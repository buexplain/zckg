package zcdb

import (
	"reflect"
	"testing"
	"time"
)

// ==================== getScanFieldInfo 测试 ====================

// TestGetScanFieldInfo_BasicStruct 验证基本结构体的字段映射。
func TestGetScanFieldInfo_BasicStruct(t *testing.T) {
	type User struct {
		ID   int    `db:"id"`
		Name string `db:"name"`
		Age  int    // 无标签，自动转 snake_case
	}

	info := getScanFieldInfo(reflect.TypeOf(User{}), "")
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

	info := getScanFieldInfo(reflect.TypeOf(User{}), "")
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

	info := getScanFieldInfo(reflect.TypeOf(User{}), "")
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

	info := getScanFieldInfo(reflect.TypeOf(&User{}), "")
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
	info1 := getScanFieldInfo(reflect.TypeOf(User{}), "")
	// 第二次调用应命中缓存
	info2 := getScanFieldInfo(reflect.TypeOf(User{}), "")

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

	// 验证返回值是 nullSafeField 类型
	for i, v := range values {
		if _, ok := v.(*nullSafeField); !ok {
			t.Errorf("value[%d] should be *nullSafeField, got %T", i, v)
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

	// 第一个值应该是 *nullSafeField
	if _, ok := values[0].(*nullSafeField); !ok {
		t.Errorf("values[0] should be *nullSafeField, got %T", values[0])
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

	// 验证嵌入字段的 nullSafeField 正确
	if _, ok := values[0].(*nullSafeField); !ok {
		t.Errorf("values[0] should be *nullSafeField (embedded), got %T", values[0])
	}
	if _, ok := values[1].(*nullSafeField); !ok {
		t.Errorf("values[1] should be *nullSafeField, got %T", values[1])
	}
}

// TestGetScanFieldInfo_EmbeddedShadowOuterWins 验证扫描列映射的「外层优先」遮蔽：
// 同名列应映射到外层字段而非嵌入层字段，与声明顺序无关。
func TestGetScanFieldInfo_EmbeddedShadowOuterWins(t *testing.T) {
	type Base struct {
		Name string `db:"name"`
	}
	type OuterFirst struct {
		Name string `db:"name"`
		Base
	}
	type EmbedFirst struct {
		Base
		Name string `db:"name"`
	}

	for _, tc := range []struct {
		name string
		typ  reflect.Type
		want []int
	}{
		{"外层在前", reflect.TypeOf(OuterFirst{}), []int{0}},
		{"嵌入在前", reflect.TypeOf(EmbedFirst{}), []int{1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info := getScanFieldInfo(tc.typ, "")
			got, ok := info.columnIndex["name"]
			if !ok {
				t.Fatal("column name not found")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("expected index %v, got %v", tc.want, got)
			}
		})
	}
}

// TestGetScanFieldInfo_RecursiveEmbedTerminates 锁死：自引用嵌入结构体经
// getScanFieldInfo（扫描路径）展开终止而非无限循环，且普通字段仍可映射。
func TestGetScanFieldInfo_RecursiveEmbedTerminates(t *testing.T) {
	// 类型名须大写导出：否则匿名嵌入字段被 IsExported 跳过、无法触发展开路径
	type RecSelf struct {
		*RecSelf
		Name string `db:"name"`
	}

	info := getScanFieldInfo(reflect.TypeOf(RecSelf{}), "")
	if info == nil {
		t.Fatal("getScanFieldInfo returned nil")
	}
	idx, ok := info.columnIndex["name"]
	if !ok {
		t.Fatalf("column 'name' not found in columnIndex: %+v", info.columnIndex)
	}
	if !reflect.DeepEqual(idx, []int{1}) {
		t.Errorf("expected name@[1], got %v", idx)
	}
}

// ==================== nullSafeField.Scan 测试 ====================

// TestBug_ScanStringToMapJSON 复现审查候选 #7：[]byte→map 走 JSON 反序列化，
// 但 string→map（PG/SQLite TEXT 驱动返回 string）直接报错，导致同一
// map[string]any 字段在 MySQL 可扫描、在 PG/SQLite 报错的方言不对称。
func TestBug_ScanStringToMapJSON(t *testing.T) {
	var m map[string]any
	n := &nullSafeField{field: reflect.ValueOf(&m).Elem()}
	if err := n.Scan(`{"a":1,"b":"x"}`); err != nil {
		t.Fatalf("string → map[string]any 应 JSON 反序列化，实际报错: %v", err)
	}
	if v, ok := m["a"].(float64); !ok || v != 1 {
		t.Errorf("expected m[\"a\"]=1, got %v", m["a"])
	}
	if m["b"] != "x" {
		t.Errorf("expected m[\"b\"]=\"x\", got %v", m["b"])
	}
}

// TestBug_ScanStringToStructJSON 复现审查候选 #7 的另一半：[]byte→struct 走 JSON，
// string→struct 却报错（PG/SQLite TEXT 列 JSON 文本无法扫入结构体字段）。
func TestBug_ScanStringToStructJSON(t *testing.T) {
	type meta struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	var s meta
	n := &nullSafeField{field: reflect.ValueOf(&s).Elem()}
	if err := n.Scan(`{"a":7,"b":"y"}`); err != nil {
		t.Fatalf("string → struct 应 JSON 反序列化，实际报错: %v", err)
	}
	if s.A != 7 || s.B != "y" {
		t.Errorf("expected {7 y}, got %+v", s)
	}
}

// TestNullSafeFieldScan_Boundaries 边界固化用例（审查结论）：
// 固化 nullSafeField.Scan 的类型转换矩阵与错误语义（返回错误而非 panic）。
func TestNullSafeFieldScan_Boundaries(t *testing.T) {
	newField := func(dst any) *nullSafeField {
		return &nullSafeField{field: reflect.ValueOf(dst).Elem()}
	}

	t.Run("NULL→零值与 nil 指针", func(t *testing.T) {
		s := "dirty"
		if err := newField(&s).Scan(nil); err != nil || s != "" {
			t.Errorf("NULL → 非指针字段应置零值, s=%q err=%v", s, err)
		}
		v := new(int)
		if err := newField(&v).Scan(nil); err != nil || v != nil {
			t.Errorf("NULL → 指针字段应置 nil, v=%v err=%v", v, err)
		}
	})

	t.Run("[]byte→基础类型", func(t *testing.T) {
		s := ""
		if err := newField(&s).Scan([]byte("abc")); err != nil || s != "abc" {
			t.Errorf("[]byte→string 失败: s=%q err=%v", s, err)
		}
		i := int64(0)
		if err := newField(&i).Scan([]byte("123")); err != nil || i != 123 {
			t.Errorf("[]byte→int64 失败: i=%d err=%v", i, err)
		}
		u := uint64(0)
		if err := newField(&u).Scan([]byte("456")); err != nil || u != 456 {
			t.Errorf("[]byte→uint64 失败: u=%d err=%v", u, err)
		}
		f := 0.0
		if err := newField(&f).Scan([]byte("1.5")); err != nil || f != 1.5 {
			t.Errorf("[]byte→float64 失败: f=%v err=%v", f, err)
		}
		b := false
		if err := newField(&b).Scan([]byte("true")); err != nil || !b {
			t.Errorf("[]byte→bool 失败: b=%v err=%v", b, err)
		}
		var slice []int
		if err := newField(&slice).Scan([]byte("[1,2]")); err != nil || len(slice) != 2 {
			t.Errorf("[]byte→[]int JSON 失败: %v err=%v", slice, err)
		}
		// 非法 JSON / 非法数字 → 返回错误而非 panic
		if err := newField(&slice).Scan([]byte("not-json")); err == nil {
			t.Error("非法 JSON → []int 应报错")
		}
		if err := newField(&i).Scan([]byte("abc")); err == nil {
			t.Error("非数字 []byte → int64 应报错")
		}
		// []byte → []byte 拷贝隔离
		src := []byte("xyz")
		var dst []byte
		if err := newField(&dst).Scan(src); err != nil || string(dst) != "xyz" {
			t.Errorf("[]byte→[]byte 失败: %v err=%v", dst, err)
		}
		// 非 string 键的 map → 报错而非 panic
		var im map[int]string
		if err := newField(&im).Scan([]byte("{\"1\":\"a\"}")); err == nil {
			t.Error("[]byte → map[int]string 应报错")
		}
	})

	t.Run("time.Time→string", func(t *testing.T) {
		s := ""
		ts := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
		if err := newField(&s).Scan(ts); err != nil || s != "2024-01-02T03:04:05Z" {
			t.Errorf("time.Time→string 应为 RFC3339: s=%q err=%v", s, err)
		}
		// time.Time → time.Time 直接匹配
		var t2 time.Time
		if err := newField(&t2).Scan(ts); err != nil || !t2.Equal(ts) {
			t.Errorf("time.Time→time.Time 失败: %v err=%v", t2, err)
		}
	})

	t.Run("数值→string 显式格式化", func(t *testing.T) {
		// 若误走 ConvertibleTo 会把数值当 rune 转字符（123 → "{"），必须固化数字字符串行为
		s := ""
		if err := newField(&s).Scan(int64(123)); err != nil || s != "123" {
			t.Errorf("int64→string 应为 \"123\": s=%q err=%v", s, err)
		}
		if err := newField(&s).Scan(uint64(456)); err != nil || s != "456" {
			t.Errorf("uint64→string 应为 \"456\": s=%q err=%v", s, err)
		}
		if err := newField(&s).Scan(float64(1.5)); err != nil || s != "1.5" {
			t.Errorf("float64→string 应为 \"1.5\": s=%q err=%v", s, err)
		}
	})

	t.Run("string→基础类型", func(t *testing.T) {
		bs := []byte{}
		if err := newField(&bs).Scan("abc"); err != nil || string(bs) != "abc" {
			t.Errorf("string→[]byte 失败: %v err=%v", bs, err)
		}
		b := false
		if err := newField(&b).Scan("true"); err != nil || !b {
			t.Errorf("string→bool 失败: %v err=%v", b, err)
		}
		i := int64(0)
		if err := newField(&i).Scan("42"); err != nil || i != 42 {
			t.Errorf("string→int64 失败: %v err=%v", i, err)
		}
		var strs []string
		if err := newField(&strs).Scan(`["a","b"]`); err != nil || len(strs) != 2 {
			t.Errorf("string→[]string JSON 失败: %v err=%v", strs, err)
		}
		// 非法输入 → 返回错误而非 panic
		if err := newField(&i).Scan("not-int"); err == nil {
			t.Error("非数字 string → int64 应报错")
		}
	})

	t.Run("数值→bool（SQLite 0/1）", func(t *testing.T) {
		b := false
		if err := newField(&b).Scan(int64(1)); err != nil || !b {
			t.Errorf("int64(1)→bool 失败: %v err=%v", b, err)
		}
		if err := newField(&b).Scan(int64(0)); err != nil || b {
			t.Errorf("int64(0)→bool 应为 false: %v err=%v", b, err)
		}
	})

	t.Run("不可转换类型报错而非 panic", func(t *testing.T) {
		var t2 time.Time
		if err := newField(&t2).Scan(int64(1)); err == nil {
			t.Error("int64 → time.Time 应报错")
		}
		var slice []int
		if err := newField(&slice).Scan(time.Now()); err == nil {
			t.Error("time.Time → []int 应报错")
		}
	})

	t.Run("指针字段解间接赋值", func(t *testing.T) {
		var p *string
		if err := newField(&p).Scan([]byte("ptr")); err != nil || p == nil || *p != "ptr" {
			t.Errorf("[]byte→*string 失败: p=%v err=%v", p, err)
		}
		var pi *int64
		if err := newField(&pi).Scan("7"); err != nil || pi == nil || *pi != 7 {
			t.Errorf("string→*int64 失败: pi=%v err=%v", pi, err)
		}
	})
}

// TestFieldByIndexSafe_NilChain 边界固化：多级嵌入指针链遇 nil 中间节点时
// 返回不可用标记而非 panic。
func TestFieldByIndexSafe_NilChain(t *testing.T) {
	type L2 struct {
		V int `db:"v"`
	}
	type L1 struct {
		*L2
	}
	type Root struct {
		*L1
	}

	r := Root{} // L1 为 nil
	v, ok := fieldByIndexSafe(reflect.ValueOf(&r).Elem(), []int{0, 0, 0})
	if ok {
		t.Error("nil 嵌入指针链应返回 ok=false")
	}
	if v.IsValid() {
		t.Error("nil 链应返回零值 Value")
	}

	r = Root{L1: &L1{L2: &L2{V: 9}}}
	v, ok = fieldByIndexSafe(reflect.ValueOf(&r).Elem(), []int{0, 0, 0})
	if !ok || v.Int() != 9 {
		t.Errorf("完整指针链应取到值 9, ok=%v v=%v", ok, v)
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
