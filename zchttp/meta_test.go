package zchttp

import (
	"mime/multipart"
	"reflect"
	"sync"
	"testing"
	"time"
)

// ========== buildStructMeta 字段类型判定 ==========

func TestBuildStructMeta_FileFields(t *testing.T) {
	type reqFiles struct {
		Avatar *multipart.FileHeader   `json:"avatar"`
		Docs   []*multipart.FileHeader `json:"docs"`
		Tags   []string                `json:"tags"`
		Name   string                  `json:"name"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqFiles{}))

	fieldMap := make(map[string]fieldMeta)
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	if !fieldMap["avatar"].isFile {
		t.Fatal("avatar should have isFile=true")
	}
	if fieldMap["avatar"].isFileSlice || fieldMap["avatar"].isSlice {
		t.Fatal("avatar should not have isFileSlice or isSlice")
	}

	if !fieldMap["docs"].isFileSlice {
		t.Fatal("docs should have isFileSlice=true")
	}
	if fieldMap["docs"].isFile || fieldMap["docs"].isSlice {
		t.Fatal("docs should not have isFile or isSlice")
	}

	if !fieldMap["tags"].isSlice {
		t.Fatal("tags should have isSlice=true")
	}
	if fieldMap["tags"].isFile || fieldMap["tags"].isFileSlice {
		t.Fatal("tags should not have isFile or isFileSlice")
	}

	if fieldMap["name"].isFile || fieldMap["name"].isFileSlice || fieldMap["name"].isSlice {
		t.Fatal("name should have no file/slice flags")
	}
}

func TestBuildStructMeta_TimeFields(t *testing.T) {
	type reqTime struct {
		Start time.Time `json:"start" time_format:"2006-01-02" time_location:"Asia/Shanghai"`
		End   time.Time `json:"end" time_format:"unix"`
		Plain time.Time `json:"plain"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqTime{}))

	fieldMap := make(map[string]fieldMeta)
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	if fieldMap["start"].timeFormat != "2006-01-02" {
		t.Fatalf("start timeFormat: got %q, want '2006-01-02'", fieldMap["start"].timeFormat)
	}
	if fieldMap["start"].timeLocation == nil || fieldMap["start"].timeLocation.String() != "Asia/Shanghai" {
		t.Fatal("start timeLocation should be Asia/Shanghai")
	}

	if fieldMap["end"].timeFormat != "unix" {
		t.Fatalf("end timeFormat: got %q, want 'unix'", fieldMap["end"].timeFormat)
	}
	if fieldMap["end"].timeLocation != nil {
		t.Fatal("end should have nil timeLocation")
	}

	if fieldMap["plain"].timeFormat != "" {
		t.Fatalf("plain timeFormat should be empty, got %q", fieldMap["plain"].timeFormat)
	}
}

func TestBuildStructMeta_InvalidTimeLocation(t *testing.T) {
	type reqBadTz struct {
		T time.Time `json:"t" time_format:"2006-01-02" time_location:"Invalid/Zone"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqBadTz{}))

	for _, fm := range meta.fields {
		if fm.name == "t" && fm.timeLocation != nil {
			t.Fatal("invalid time_location should result in nil (fallback to time.Local)")
		}
	}
}

// ========== buildStructMeta 跳过规则 ==========

func TestBuildStructMeta_SkipUnexported(t *testing.T) {
	type reqMixed struct {
		Name   string `json:"name"`
		secret string //nolint:unused // 未导出字段应被跳过
		Age    int    `json:"age"`
	}
	_ = reqMixed{secret: ""}

	meta := buildStructMeta(reflect.TypeOf(reqMixed{}))

	if len(meta.fields) != 2 {
		t.Fatalf("expected 2 fields (unexported skipped), got %d", len(meta.fields))
	}
}

func TestBuildStructMeta_SkipOpenAPIMeta(t *testing.T) {
	type reqAPIMeta struct {
		OpenAPIMeta `tags:"Test" summary:"test"`
		Name        string `json:"name"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqAPIMeta{}))

	if len(meta.fields) != 1 {
		t.Fatalf("expected 1 field (OpenAPIMeta skipped), got %d", len(meta.fields))
	}
	if meta.fields[0].name != "name" {
		t.Fatalf("expected field 'name', got %q", meta.fields[0].name)
	}
}

func TestBuildStructMeta_JsonDash(t *testing.T) {
	type reqDash struct {
		Name   string `json:"name"`
		Secret string `json:"-"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqDash{}))

	// json:"-" 字段仍被记录（name 为 "-"），但不会被 bindValues 绑定
	if len(meta.fields) != 2 {
		t.Fatalf("expected 2 fields (json:'-' recorded), got %d", len(meta.fields))
	}
	// json:"-" 字段不应解析 nonzero/default 等标签
	for _, fm := range meta.fields {
		if fm.name == "-" {
			if fm.nonzero || fm.hasDefault {
				t.Fatal("json:'-' field should not have nonzero/hasDefault parsed")
			}
		}
	}
}

func TestBuildStructMeta_NonzeroParsing(t *testing.T) {
	type reqNonzero struct {
		A string `json:"a" nonzero:"true"`
		B string `json:"b" nonzero:"false"`
		C string `json:"c" nonzero:"invalid"`
		D string `json:"d"`
	}

	meta := buildStructMeta(reflect.TypeOf(reqNonzero{}))

	fieldMap := make(map[string]fieldMeta)
	for _, fm := range meta.fields {
		fieldMap[fm.name] = fm
	}

	if !fieldMap["a"].nonzero {
		t.Fatal("a: nonzero:'true' should set nonzero=true")
	}
	if fieldMap["b"].nonzero {
		t.Fatal("b: nonzero:'false' should set nonzero=false")
	}
	if fieldMap["c"].nonzero {
		t.Fatal("c: nonzero:'invalid' should not set nonzero=true")
	}
	if fieldMap["d"].nonzero {
		t.Fatal("d: no nonzero tag should default to false")
	}
}

// ========== buildStructMeta implementsValidator ==========

// buildEntryValidatorReq 实现 Validator 接口，用于 implementsValidator 检测测试
type buildEntryValidatorReq struct {
	Name string `json:"name"`
}

func (r buildEntryValidatorReq) Validate() error {
	return nil
}

func TestBuildStructMeta_ImplementsValidator(t *testing.T) {
	meta := buildStructMeta(reflect.TypeOf(buildEntryValidatorReq{}))

	if !meta.implementsValidator {
		t.Fatal("implementsValidator should be true for type implementing Validator")
	}
}

func TestBuildStructMeta_NotImplementsValidator(t *testing.T) {
	meta := buildStructMeta(reflect.TypeOf(testReq{}))

	if meta.implementsValidator {
		t.Fatal("implementsValidator should be false for type not implementing Validator")
	}
}

// ========== fieldByIndex 测试 ==========

func TestFieldByIndex_Simple(t *testing.T) {
	type simple struct {
		Name string
		Age  int
	}
	v := reflect.ValueOf(&simple{Name: "alice", Age: 30}).Elem()

	nameField := fieldByIndex(v, []int{0})
	if nameField.String() != "alice" {
		t.Fatalf("expected 'alice', got %q", nameField.String())
	}

	ageField := fieldByIndex(v, []int{1})
	if ageField.Int() != 30 {
		t.Fatalf("expected 30, got %d", ageField.Int())
	}
}

func TestFieldByIndex_NilPtrAutoInit(t *testing.T) {
	type inner struct {
		Value string
	}
	type outer struct {
		Ref *inner
	}

	o := &outer{}
	v := reflect.ValueOf(o).Elem()

	// indices [0, 0]：先访问 outer.Ref（*inner），再访问 inner.Value
	result := fieldByIndex(v, []int{0, 0})

	// nil 指针应被自动初始化
	if o.Ref == nil {
		t.Fatal("nil pointer should be auto-initialized by fieldByIndex")
	}

	// result 应为 inner.Value（空字符串）
	if result.Kind() != reflect.String {
		t.Fatalf("expected string field, got %v", result.Kind())
	}
	if result.String() != "" {
		t.Fatalf("expected empty string, got %q", result.String())
	}
}

func TestFieldByIndex_MultiLevel(t *testing.T) {
	type level2 struct {
		Code int
	}
	type level1 struct {
		L2 *level2
	}
	type root struct {
		L1 *level1
	}

	r := &root{}
	v := reflect.ValueOf(r).Elem()

	// indices [0, 0, 0]：root.L1 → level1.L2 → level2.Code
	result := fieldByIndex(v, []int{0, 0, 0})

	if r.L1 == nil {
		t.Fatal("L1 should be auto-initialized")
	}
	if r.L1.L2 == nil {
		t.Fatal("L2 should be auto-initialized")
	}
	if result.Kind() != reflect.Int {
		t.Fatalf("expected int field, got %v", result.Kind())
	}
}

// ========== derefType 测试 ==========

// TestDerefType_SelfReferentialPtr 验证自引用指针类型不会使 derefType 死循环
func TestDerefType_SelfReferentialPtr(t *testing.T) {
	done := make(chan reflect.Type, 1)
	go func() {
		done <- derefType(reflect.TypeOf((selfPtr)(nil)))
	}()
	select {
	case got := <-done:
		// 达到深度上限后应原样返回（仍为指针类型），而非死循环
		if got.Kind() != reflect.Ptr {
			t.Errorf("derefType(selfPtr) kind = %v, want Ptr", got.Kind())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("confirmed: derefType infinite loop on self-referential pointer type (timed out)")
	}
}

// TestIsStructLikeVariants 覆盖结构体、指针、非结构体类型的判定分支
func TestIsStructLikeVariants(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
		want bool
	}{
		{"struct", reflect.TypeOf(helloReq{}), true},
		{"ptr to struct", reflect.TypeOf(&helloReq{}), true},
		{"int", reflect.TypeOf(0), false},
		{"ptr to int", reflect.TypeOf((*int)(nil)), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isStructLike(tc.typ); got != tc.want {
				t.Fatalf("isStructLike(%v) = %v, want %v", tc.typ, got, tc.want)
			}
		})
	}
}

// ========== cachedStructMeta 测试（Nit-1: LoadOrStore） ==========

// TestCachedStructMeta_ConcurrentConsistent 验证并发首次构建同一类型时，
// 所有 goroutine 经 LoadOrStore 返回同一份缓存 structMeta（内容一致），
// 且缓存命中后的后续调用与首次构建结果一致
func TestCachedStructMeta_ConcurrentConsistent(t *testing.T) {
	type cacheProbe struct {
		Name    string   `json:"name" default:"x"`
		Count   int      `json:"count" nonzero:"true"`
		Percent *float64 `json:"percent"`
	}
	tp := reflect.TypeOf(cacheProbe{})

	const n = 16
	metas := make([]structMeta, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			metas[idx] = cachedStructMeta(tp)
		}(i)
	}
	wg.Wait()

	for i, m := range metas {
		if !reflect.DeepEqual(m, metas[0]) {
			t.Fatalf("goroutine %d returned inconsistent structMeta", i)
		}
	}
	if again := cachedStructMeta(tp); !reflect.DeepEqual(again, metas[0]) {
		t.Fatal("cache hit returned inconsistent structMeta")
	}
}
