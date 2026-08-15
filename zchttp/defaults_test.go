package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// ========== deepCopyDefaults 测试 ==========

type deepCopyInner struct {
	Name string
	Age  int
}

type deepCopyOuter struct {
	Ptr   *deepCopyInner
	Items []*deepCopyInner
	Meta  map[string]*deepCopyInner
}

func TestDeepCopyDefaults_NoStackOverflow(t *testing.T) {
	// 构造一个包含指针字段、切片、map 的结构体，其中所有引用字段均非 nil。
	// Bug：修复前 deepCopyDefaults 在 Ptr 分支递归调用自身（deepCopyDefaults(v)），
	//      而 v 的 Kind 仍是 Ptr 且非 nil，造成无限递归 → 栈溢出。
	// 正确行为：递归到 v.Elem()（指针指向的元素），而非指针本身。
	inner := &deepCopyInner{Name: "test", Age: 30}
	v := reflect.ValueOf(&deepCopyOuter{
		Ptr:   inner,
		Items: []*deepCopyInner{inner},
		Meta:  map[string]*deepCopyInner{"a": inner},
	}).Elem()

	// 记录原始指针地址，用于后续验证深拷贝是否断开共享
	origPtrAddr := v.FieldByName("Ptr").Pointer()

	// 执行深拷贝：如果存在无限递归 bug，此处会栈溢出导致测试崩溃
	deepCopyDefaults(v)

	// 验证指针已被深拷贝（地址不同）
	newPtrAddr := v.FieldByName("Ptr").Pointer()
	if newPtrAddr == origPtrAddr {
		t.Fatal("pointer should be deep-copied (different address)")
	}

	// 验证内容一致
	newInner := v.FieldByName("Ptr").Elem().Interface().(deepCopyInner)
	if newInner.Name != "test" || newInner.Age != 30 {
		t.Fatalf("deep-copied content mismatch: %+v", newInner)
	}

	// 验证切片被深拷贝
	itemsField := v.FieldByName("Items")
	if itemsField.Len() != 1 {
		t.Fatalf("expected 1 item in slice, got %d", itemsField.Len())
	}

	// 验证 map 被深拷贝（map 本身是新的，但内部元素目前未递归深拷贝，此处仅验证不崩溃）
	metaField := v.FieldByName("Meta")
	if metaField.Len() != 1 {
		t.Fatalf("expected 1 entry in map, got %d", metaField.Len())
	}

	// 验证并发安全性：多次调用不应崩溃
	for range 10 {
		v2 := reflect.ValueOf(&deepCopyOuter{
			Ptr:   &deepCopyInner{Name: "concurrent"},
			Items: []*deepCopyInner{{Name: "item1"}, {Name: "item2"}},
		}).Elem()
		deepCopyDefaults(v2)
	}
}

// TestDeepCopyDefaults_MapValueNotShared 验证 deepCopyDefaults 后 map 内指针值与原始模板断开共享
func TestDeepCopyDefaults_MapValueNotShared(t *testing.T) {
	inner := &deepCopyInner{Name: "original", Age: 10}
	v := reflect.ValueOf(&deepCopyOuter{
		Meta: map[string]*deepCopyInner{"key": inner},
	}).Elem()

	// 记录原始指针地址
	origAddr := v.FieldByName("Meta").MapIndex(reflect.ValueOf("key")).Pointer()

	// 执行深拷贝
	deepCopyDefaults(v)

	// 检查 map 内的指针值是否已断开共享
	newAddr := v.FieldByName("Meta").MapIndex(reflect.ValueOf("key")).Pointer()
	if newAddr == origAddr {
		t.Fatal("BUG3 confirmed: map value pointer still shared after deepCopyDefaults")
	}

	// 内容应保持一致
	newVal := v.FieldByName("Meta").MapIndex(reflect.ValueOf("key")).Elem().Interface().(deepCopyInner)
	if newVal.Name != "original" || newVal.Age != 10 {
		t.Fatalf("content mismatch after deep copy: %+v", newVal)
	}
}

// TestDeepCopyDefaults_ArrayElemNotShared 验证数组元素内的指针字段经深拷贝后断开共享
// （P2-02 配套：deepCopyDefaults 的 Array 分支必须与 applyDefaults 对齐，
// 否则并发请求间共享元素内指针导致数据污染）
func TestDeepCopyDefaults_ArrayElemNotShared(t *testing.T) {
	type arrElem struct {
		Ref *int
	}
	type withArr struct {
		Items [2]arrElem
	}

	shared := 7
	orig := &withArr{Items: [2]arrElem{{Ref: &shared}, {Ref: &shared}}}
	v := reflect.ValueOf(orig).Elem()

	origAddr := v.FieldByName("Items").Index(0).FieldByName("Ref").Pointer()

	deepCopyDefaults(v)

	// 每个元素内的指针都应是新分配的，与原指针及彼此不共享
	ref0 := v.FieldByName("Items").Index(0).FieldByName("Ref")
	ref1 := v.FieldByName("Items").Index(1).FieldByName("Ref")
	if ref0.Pointer() == origAddr {
		t.Fatal("array elem[0] pointer still shared with original after deepCopyDefaults")
	}
	if ref0.Pointer() == ref1.Pointer() {
		t.Fatal("array elem[0] and elem[1] pointers should not share after deep copy")
	}

	// 内容保持一致
	if ref0.Elem().Int() != 7 || ref1.Elem().Int() != 7 {
		t.Fatalf("content mismatch after deep copy: %d, %d", ref0.Elem().Int(), ref1.Elem().Int())
	}

	// 修改拷贝不影响原值（共享已断开）
	ref0.Elem().SetInt(99)
	if shared != 7 {
		t.Fatal("modifying deep-copied array elem affected the original value")
	}
}

// ========== hasRefFields 测试 ==========

func TestHasRefFields_PureValue(t *testing.T) {
	type pureValue struct {
		Name string
		Age  int
		OK   bool
	}
	v := reflect.ValueOf(pureValue{Name: "test", Age: 1, OK: true})
	if hasRefFields(v) {
		t.Fatal("pure value struct should return false")
	}
}

func TestHasRefFields_NonNilPtr(t *testing.T) {
	type withPtr struct {
		Name string
		Ref  *int
	}
	n := 42
	v := reflect.ValueOf(withPtr{Ref: &n})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil ptr should return true")
	}
}

func TestHasRefFields_NilPtr(t *testing.T) {
	type withNilPtr struct {
		Name string
		Ref  *int
	}
	v := reflect.ValueOf(withNilPtr{})
	if hasRefFields(v) {
		t.Fatal("struct with only nil ptr should return false")
	}
}

func TestHasRefFields_NonNilSlice(t *testing.T) {
	type withSlice struct {
		Items []string
	}
	v := reflect.ValueOf(withSlice{Items: []string{"a"}})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil slice should return true")
	}
}

func TestHasRefFields_NilSlice(t *testing.T) {
	type withNilSlice struct {
		Items []string
	}
	v := reflect.ValueOf(withNilSlice{})
	if hasRefFields(v) {
		t.Fatal("struct with nil slice should return false")
	}
}

func TestHasRefFields_NonNilMap(t *testing.T) {
	type withMap struct {
		Meta map[string]string
	}
	v := reflect.ValueOf(withMap{Meta: map[string]string{"k": "v"}})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil map should return true")
	}
}

func TestHasRefFields_NilMap(t *testing.T) {
	type withNilMap struct {
		Meta map[string]string
	}
	v := reflect.ValueOf(withNilMap{})
	if hasRefFields(v) {
		t.Fatal("struct with nil map should return false")
	}
}

func TestHasRefFields_NestedStruct(t *testing.T) {
	type inner struct {
		Ref *int
	}
	type outer struct {
		Name  string
		Inner inner
	}
	n := 1
	v := reflect.ValueOf(outer{Inner: inner{Ref: &n}})
	if !hasRefFields(v) {
		t.Fatal("nested struct with non-nil ptr should return true")
	}
}

// TestHasRefFields_ArrayWithRef 验证数组元素内含非 nil 引用字段时返回 true
// （P2-02 配套：hasRefFields 的 Array 分支缺失会导致 needsDeepCopy 误判为 false）
func TestHasRefFields_ArrayWithRef(t *testing.T) {
	type arrElem struct {
		Ref *int
	}
	type withArr struct {
		Items [2]arrElem
	}
	n := 1
	v := reflect.ValueOf(withArr{Items: [2]arrElem{{Ref: &n}, {}}})
	if !hasRefFields(v) {
		t.Fatal("array with non-nil ptr in element should return true")
	}
}

// TestHasRefFields_ArrayPureValue 验证纯值数组返回 false（数组本身是值类型，不触发深拷贝）
func TestHasRefFields_ArrayPureValue(t *testing.T) {
	type withValArr struct {
		Nums [3]int
		Tags [2]string
	}
	v := reflect.ValueOf(withValArr{Nums: [3]int{1, 2, 3}, Tags: [2]string{"a", "b"}})
	if hasRefFields(v) {
		t.Fatal("pure value array should return false")
	}
}

// ========== isDefaultSupported 测试 ==========

func TestIsDefaultSupported(t *testing.T) {
	cases := []struct {
		name     string
		typ      reflect.Type
		expected bool
	}{
		{"string", reflect.TypeOf(""), true},
		{"int", reflect.TypeOf(0), true},
		{"int64", reflect.TypeOf(int64(0)), true},
		{"uint", reflect.TypeOf(uint(0)), true},
		{"float32", reflect.TypeOf(float32(0)), true},
		{"float64", reflect.TypeOf(float64(0)), true},
		{"bool", reflect.TypeOf(false), true},
		{"*int", reflect.TypeOf((*int)(nil)), true},
		{"*string", reflect.TypeOf((*string)(nil)), true},
		{"[]string", reflect.TypeOf([]string{}), true},
		{"[]int", reflect.TypeOf([]int{}), true},
		{"[]*int", reflect.TypeOf([]*int{}), true},
		{"time.Time", reflect.TypeOf(time.Time{}), false},
		{"struct", reflect.TypeOf(struct{}{}), false},
		{"map[string]string", reflect.TypeOf(map[string]string{}), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isDefaultSupported(tc.typ)
			if got != tc.expected {
				t.Fatalf("isDefaultSupported(%v) = %v, want %v", tc.typ, got, tc.expected)
			}
		})
	}
}

// ========== checkUnsupportedDefaults 测试 ==========

func TestCheckUnsupportedDefaults_NoPanic(t *testing.T) {
	// 验证各种场景不会 panic

	// 不支持的类型设 default
	type reqUnsupported struct {
		When time.Time `json:"when" default:"2023-01-01"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqUnsupported{}), true, true, "GET", "/test", "handler", "file.go", 1, make(map[reflect.Type]bool))

	// 值类型字段在指针路径下
	type nested struct {
		Name string `json:"name" default:"hello"`
	}
	type reqPtr struct {
		Inner *nested `json:"inner"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqPtr{}), true, true, "POST", "/test", "handler", "file.go", 1, make(map[reflect.Type]bool))

	// 自引用结构体不会无限递归
	type selfRef struct {
		Name   string   `json:"name"`
		Parent *selfRef `json:"parent"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(selfRef{}), true, true, "POST", "/self", "handler", "file.go", 1, make(map[reflect.Type]bool))
}

func TestCheckUnsupportedDefaults_SliceMapRecursion(t *testing.T) {
	// 验证切片/map/数组元素中的 default 检查不会 panic
	type item struct {
		Status string `json:"status" default:"active"`
	}
	type reqSlice struct {
		Items []item           `json:"items"`
		Map   map[string]*item `json:"map"`
		Arr   []*item          `json:"arr"`
		Fix   [2]item          `json:"fix"`  // 值元素数组（P2-02 Array 分支）
		FixP  [2]*item         `json:"fixp"` // 指针元素数组
	}
	// 不应 panic
	checkUnsupportedDefaults(reflect.TypeOf(reqSlice{}), true, true, "POST", "/items", "handler", "file.go", 1, make(map[reflect.Type]bool))
}

// TestCheckUnsupportedDefaults_MapArrayBoundaryWarning 验证 map→array 边界的告警判定：
// map 值类型为数组时 applyDefaults 无法穿透，元素内指针 default 应告警"never applied"；
// 对照组：直接使用数组时可达，不应告警（P2-02 四函数对齐的告警侧回归保障）
func TestCheckUnsupportedDefaults_MapArrayBoundaryWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	type boundaryItem struct {
		Qty *int `json:"qty" default:"1"`
	}

	// 对照组：直接数组使用，指针 default 可达 → 不告警
	type reqArrDirect struct {
		Items [2]boundaryItem `json:"items"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqArrDirect{}), true, true, "POST", "/arr", "handler", "file.go", 1, make(map[reflect.Type]bool))
	if buf.Len() != 0 {
		t.Fatalf("direct array usage is defaults-reachable, expected no warning, got: %s", buf.String())
	}

	// 边界组：map 值类型为数组，applyDefaults 无法穿透 → 应告警
	type reqMapArr struct {
		Deep map[string][2]boundaryItem `json:"deep"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqMapArr{}), true, true, "POST", "/maparr", "handler", "file.go", 1, make(map[reflect.Type]bool))
	out := buf.String()
	if !strings.Contains(out, "never applied") {
		t.Fatalf("map value array should break viaDefaults and warn 'never applied', got: %s", out)
	}
	if !strings.Contains(out, "boundaryItem") {
		t.Fatalf("warning should identify struct boundaryItem, got: %s", out)
	}
}

func TestCheckUnsupportedDefaults_AutogeneratedFile(t *testing.T) {
	// handlerFile 为 "<autogenerated>" 时使用 handlerName 作为位置
	type reqAuto struct {
		When time.Time `json:"when" default:"now"`
	}
	// 不应 panic，仅验证能正确处理
	checkUnsupportedDefaults(reflect.TypeOf(reqAuto{}), true, true, "GET", "/auto", "pkg.handler", "<autogenerated>", 0, make(map[reflect.Type]bool))
}

// selfSlice 病态自引用切片类型：Elem() 返回自身，无限递归
type selfSlice []selfSlice

// TestIsDefaultSupported_SelfReferentialTypes 验证自引用类型不会使 isDefaultSupported 无限递归（栈溢出），
// 超过深度上限后应判定为不支持 default 标签
func TestIsDefaultSupported_SelfReferentialTypes(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"selfSlice", reflect.TypeOf(selfSlice{})},
		{"selfPtr", reflect.TypeOf((selfPtr)(nil))},
	}
	for _, c := range cases {
		done := make(chan bool, 1)
		go func() {
			done <- isDefaultSupported(c.typ)
		}()
		select {
		case got := <-done:
			if got {
				t.Errorf("isDefaultSupported(%s) = true, want false", c.name)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("isDefaultSupported(%s) did not terminate", c.name)
		}
	}
}

// ========== hasRequestPhaseDefaults 预计算测试 ==========

// TestHasRequestPhaseDefaults 表驱动测试：验证各种类型结构下请求阶段是否需要执行 applyDefaults
func TestHasRequestPhaseDefaults(t *testing.T) {
	tests := []struct {
		name string
		req  any
		want bool
	}{
		{
			name: "无default标签",
			req: struct {
				Name string `json:"name"`
				Age  int    `json:"age"`
			}{},
			want: false,
		},
		{
			name: "仅值类型default",
			req: struct {
				Name string `json:"name" default:"hello"`
				Age  int    `json:"age" default:"18"`
			}{},
			want: false,
		},
		{
			name: "指针字段有default",
			req: struct {
				Name *string `json:"name" default:"hello"`
			}{},
			want: true,
		},
		{
			name: "指针字段无default",
			req: struct {
				Name *string `json:"name"`
			}{},
			want: false,
		},
		{
			name: "嵌套值结构体含指针default",
			req: struct {
				Inner struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			want: true,
		},
		{
			name: "嵌套指针结构体含值default",
			req: struct {
				Inner *struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			want: false,
		},
		{
			name: "嵌套指针结构体含指针default",
			req: struct {
				Inner *struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			want: true,
		},
		{
			name: "切片元素含指针default",
			req: struct {
				Items []struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "切片元素仅值类型default",
			req: struct {
				Items []struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "map值含指针default",
			req: struct {
				Meta map[string]struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"meta"`
			}{},
			want: true,
		},
		{
			name: "map值仅值类型default",
			req: struct {
				Meta map[string]struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"meta"`
			}{},
			want: false,
		},
		{
			name: "指针切片含指针default",
			req: struct {
				Items []*struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "指针切片仅值default",
			req: struct {
				Items []*struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "值切片含指针default",
			req: struct {
				Tags []string `json:"tags" default:"a,b"`
			}{},
			want: false,
		},
		{
			name: "指针切片default",
			req: struct {
				Tags []*string `json:"tags" default:"a,b"`
			}{},
			want: false,
		},
		{
			name: "多层嵌套值结构体含指针default",
			req: struct {
				Outer struct {
					Inner struct {
						Deep *int `json:"deep" default:"42"`
					} `json:"inner"`
				} `json:"outer"`
			}{},
			want: true,
		},
		{
			name: "空结构体",
			req:  struct{}{},
			want: false,
		},
		// 指针包裹容器：*[]Struct / *map[K]Struct，元素/值含指针 default
		// 修复前 hasRequestPhaseDefaults 经 Ptr 递归到达 Slice/Map 时被 "非 Struct" 拦截返回 false
		{
			name: "*[]Struct元素含指针default（bug修复场景）",
			req: struct {
				Items *[]struct {
					Qty *int `json:"qty" default:"1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "*map[K]Struct值含指针default（bug修复场景）",
			req: struct {
				Extras *map[string]struct {
					Qty *int `json:"qty" default:"1"`
				} `json:"extras"`
			}{},
			want: true,
		},
		{
			name: "*[]Struct元素仅值default（bug修复场景对照组）",
			req: struct {
				Items *[]struct {
					Qty int `json:"qty" default:"1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "多层*[]嵌套含指针default",
			req: struct {
				Mids *[]struct {
					Children *[]struct {
						Active *bool `json:"active" default:"true"`
					} `json:"children"`
				} `json:"mids"`
			}{},
			want: true,
		},
		// 数组容器（P2-02 四函数对齐后新增）：与切片同等支持
		{
			name: "数组元素含指针default",
			req: struct {
				Items [2]struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "数组元素仅值default（对照组）",
			req: struct {
				Items [2]struct {
					Tag string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: false,
		},
		{
			name: "指针元素数组含指针default",
			req: struct {
				Items [2]*struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{},
			want: true,
		},
		{
			name: "*[N]Struct元素含指针default（对照*[]Struct bug修复场景）",
			req: struct {
				Items *[2]struct {
					Qty *int `json:"qty" default:"1"`
				} `json:"items"`
			}{},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqType := reflect.TypeOf(tt.req)
			got := hasRequestPhaseDefaults(reqType, nil)
			if got != tt.want {
				t.Fatalf("hasRequestPhaseDefaults()=%v, want %v", got, tt.want)
			}
		})
	}
}

// ========== hasRequestPhaseDefaults 自引用循环检测 ==========

// hasPhaseSelfRef 自引用结构体，内部含指针 default 字段
// 用于验证 visiting 机制能阻止无限递归
type hasPhaseSelfRef struct {
	Name   string           `json:"name"`
	Child  *hasPhaseSelfRef `json:"child"`
	Status *string          `json:"status" default:"active"`
}

// TestHasRequestPhaseDefaults_SelfRef 验证自引用结构体不会导致无限递归
func TestHasRequestPhaseDefaults_SelfRef(t *testing.T) {
	done := make(chan bool, 1)
	go func() {
		done <- hasRequestPhaseDefaults(reflect.TypeOf(hasPhaseSelfRef{}), nil)
	}()
	select {
	case got := <-done:
		// 自引用含指针 default → 应返回 true
		if !got {
			t.Fatal("hasRequestPhaseDefaults should return true for self-ref struct with ptr default")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hasRequestPhaseDefaults did not terminate on self-referential type")
	}
}

// hasPhaseNoDefaultSelfRef 自引用结构体，不含任何 default 标签
// 用于验证 visiting 在无匹配时也能正常终止
type hasPhaseNoDefaultSelfRef struct {
	Name  string                               `json:"name"`
	Child *hasPhaseNoDefaultSelfRef            `json:"child"`
	Meta  map[string]*hasPhaseNoDefaultSelfRef `json:"meta"`
}

// TestHasRequestPhaseDefaults_SelfRefNoDefault 验证无 default 的自引用结构体正常终止并返回 false
func TestHasRequestPhaseDefaults_SelfRefNoDefault(t *testing.T) {
	done := make(chan bool, 1)
	go func() {
		done <- hasRequestPhaseDefaults(reflect.TypeOf(hasPhaseNoDefaultSelfRef{}), nil)
	}()
	select {
	case got := <-done:
		if got {
			t.Fatal("hasRequestPhaseDefaults should return false for self-ref struct without default")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("hasRequestPhaseDefaults did not terminate on self-referential type without default")
	}
}

// ========== applyDefaults 请求阶段直接单元测试 ==========

// TestApplyDefaults_RequestPhase 表驱动测试：验证请求阶段 applyDefaults 的填充规则
func TestApplyDefaults_RequestPhase(t *testing.T) {
	tests := []struct {
		name  string
		req   any                                 // 请求结构体实例（模拟 JSON 反序列化后的状态）
		check func(t *testing.T, v reflect.Value) // 验证填充结果
	}{
		{
			name: "nil指针填充default",
			req: &struct {
				Name *string `json:"name" default:"hello"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				namePtr := v.Elem().FieldByName("Name")
				if namePtr.IsNil() {
					t.Fatal("nil pointer should be filled with default")
				}
				if namePtr.Elem().String() != "hello" {
					t.Fatalf("expected 'hello', got %q", namePtr.Elem().String())
				}
			},
		},
		{
			name: "非nil指针不覆盖",
			req: func() any {
				s := "explicit"
				return &struct {
					Name *string `json:"name" default:"hello"`
				}{Name: &s}
			}(),
			check: func(t *testing.T, v reflect.Value) {
				namePtr := v.Elem().FieldByName("Name")
				if namePtr.Elem().String() != "explicit" {
					t.Fatalf("non-nil pointer should not be overwritten, got %q", namePtr.Elem().String())
				}
			},
		},
		{
			name: "值类型不填充",
			req: &struct {
				Name string `json:"name" default:"hello"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				nameVal := v.Elem().FieldByName("Name").String()
				if nameVal != "" {
					t.Fatalf("value type should not be filled in request phase, got %q", nameVal)
				}
			},
		},
		{
			name: "值类型零值不被默认值覆盖",
			req: &struct {
				Age int `json:"age" default:"18"`
			}{Age: 0},
			check: func(t *testing.T, v reflect.Value) {
				ageVal := v.Elem().FieldByName("Age").Int()
				if ageVal != 0 {
					t.Fatalf("explicit zero should be kept, got %d", ageVal)
				}
			},
		},
		{
			name: "嵌套值结构体内nil指针填充",
			req: &struct {
				Inner struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				tagPtr := v.Elem().FieldByName("Inner").FieldByName("Tag")
				if tagPtr.IsNil() {
					t.Fatal("nested nil pointer should be filled with default")
				}
				if tagPtr.Elem().String() != "v1" {
					t.Fatalf("expected 'v1', got %q", tagPtr.Elem().String())
				}
			},
		},
		{
			name: "切片元素内nil指针填充",
			req: &struct {
				Items []struct {
					Qty *int `json:"qty" default:"10"`
				} `json:"items"`
			}{
				Items: []struct {
					Qty *int `json:"qty" default:"10"`
				}{{}},
			},
			check: func(t *testing.T, v reflect.Value) {
				qtyPtr := v.Elem().FieldByName("Items").Index(0).FieldByName("Qty")
				if qtyPtr.IsNil() {
					t.Fatal("slice elem nil pointer should be filled with default")
				}
				if qtyPtr.Elem().Int() != 10 {
					t.Fatalf("expected 10, got %d", qtyPtr.Elem().Int())
				}
			},
		},
		{
			name: "map值内nil指针填充",
			req: &struct {
				Meta map[string]struct {
					Status *string `json:"status" default:"ok"`
				} `json:"meta"`
			}{
				Meta: map[string]struct {
					Status *string `json:"status" default:"ok"`
				}{"a": {}},
			},
			check: func(t *testing.T, v reflect.Value) {
				statusPtr := v.Elem().FieldByName("Meta").MapIndex(reflect.ValueOf("a")).FieldByName("Status")
				if statusPtr.IsNil() {
					t.Fatal("map value nil pointer should be filled with default")
				}
				if statusPtr.Elem().String() != "ok" {
					t.Fatalf("expected 'ok', got %q", statusPtr.Elem().String())
				}
			},
		},
		{
			name: "nil外层指针阻断内层default填充",
			req: &struct {
				Inner *struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"inner"`
			}{Inner: nil},
			check: func(t *testing.T, v reflect.Value) {
				innerPtr := v.Elem().FieldByName("Inner")
				if !innerPtr.IsNil() {
					t.Fatal("nil outer pointer should remain nil, inner defaults not applied")
				}
			},
		},
		{
			name: "不支持类型default不填充",
			req: &struct {
				When time.Time `json:"when" default:"2023-01-01"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				whenVal := v.Elem().FieldByName("When").Interface().(time.Time)
				if !whenVal.IsZero() {
					t.Fatal("unsupported type default should be silently ignored")
				}
			},
		},
		{
			name: "零长度切片跳过（不崩溃）",
			req: &struct {
				Items []struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"items"`
			}{
				Items: []struct {
					Tag *string `json:"tag" default:"v1"`
				}{},
			},
			check: func(t *testing.T, v reflect.Value) {
				// 零元素切片不崩溃即可
			},
		},
		{
			name: "数组元素内nil指针填充（Array分支）",
			req: &struct {
				Items [1]struct {
					Qty *int `json:"qty" default:"10"`
				} `json:"items"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				qtyPtr := v.Elem().FieldByName("Items").Index(0).FieldByName("Qty")
				if qtyPtr.IsNil() {
					t.Fatal("array elem nil pointer should be filled with default")
				}
				if qtyPtr.Elem().Int() != 10 {
					t.Fatalf("expected 10, got %d", qtyPtr.Elem().Int())
				}
			},
		},
		{
			name: "指针元素数组nil元素跳过（Array isPtrElem分支）",
			req: &struct {
				Items [2]*struct {
					Qty *int `json:"qty" default:"10"`
				} `json:"items"`
			}{},
			check: func(t *testing.T, v reflect.Value) {
				// 两个 nil 元素均应跳过，不崩溃不创建
				for i := 0; i < 2; i++ {
					if !v.Elem().FieldByName("Items").Index(i).IsNil() {
						t.Fatalf("nil array elem %d should be skipped", i)
					}
				}
			},
		},
		{
			name: "指针元素数组非nil元素内nil指针填充（Array isPtrElem分支）",
			req: func() any {
				type arrPtrElem struct {
					Items [2]*struct {
						Qty *int `json:"qty" default:"10"`
					} `json:"items"`
				}
				r := &arrPtrElem{}
				r.Items[0] = &struct {
					Qty *int `json:"qty" default:"10"`
				}{}
				// Items[1] 保持 nil：验证混合场景
				return r
			}(),
			check: func(t *testing.T, v reflect.Value) {
				elem0 := v.Elem().FieldByName("Items").Index(0).Elem()
				qtyPtr := elem0.FieldByName("Qty")
				if qtyPtr.IsNil() {
					t.Fatal("non-nil array elem's nil pointer should be filled")
				}
				if qtyPtr.Elem().Int() != 10 {
					t.Fatalf("expected 10, got %d", qtyPtr.Elem().Int())
				}
				if !v.Elem().FieldByName("Items").Index(1).IsNil() {
					t.Fatal("nil array elem should remain nil")
				}
			},
		},
		{
			name: "空map跳过（不崩溃）",
			req: &struct {
				Meta map[string]struct {
					Tag *string `json:"tag" default:"v1"`
				} `json:"meta"`
			}{
				Meta: map[string]struct {
					Tag *string `json:"tag" default:"v1"`
				}{},
			},
			check: func(t *testing.T, v reflect.Value) {
				// 空 map 不崩溃即可
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqPtr := reflect.ValueOf(tt.req)
			reqElemType := reqPtr.Type().Elem()
			meta := buildStructMeta(reqElemType)

			applyDefaults(reqPtr, meta, true)

			tt.check(t, reqPtr)
		})
	}
}

// ========== applyDefaults 请求阶段自引用循环安全测试 ==========

// applySelfRef 自引用结构体用于测试 applyDefaults 运行时的循环安全
type applySelfRef struct {
	Name   string        `json:"name"`
	Child  *applySelfRef `json:"child"`
	Status *string       `json:"status" default:"ok"`
}

// TestApplyDefaults_SelfRef_NoInfiniteRecursion 验证自引用结构体在 applyDefaults 中不会无限递归
func TestApplyDefaults_SelfRef_NoInfiniteRecursion(t *testing.T) {
	// 构造自引用：node.Child 指向自身
	node := &applySelfRef{Name: "self"}
	node.Child = node

	reqPtr := reflect.ValueOf(node)
	meta := buildStructMeta(reflect.TypeOf(applySelfRef{}))

	// 如果递归没有终止条件，此处会栈溢出
	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		applyDefaults(reqPtr, meta, true)
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("applyDefaults panicked or errored on self-ref: %v", err)
		}
		// Status 应该被填充
		if node.Status == nil || *node.Status != "ok" {
			t.Fatalf("Status should be filled with 'ok', got %v", node.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("applyDefaults did not terminate on self-referential struct (infinite recursion)")
	}
}

// TestApplyDefaults_SelfRefMap_NoInfiniteRecursion 验证 map 值自引用在 applyDefaults 不会无限递归
func TestApplyDefaults_SelfRefMap_NoInfiniteRecursion(t *testing.T) {
	type mapSelfRef struct {
		Name     string                 `json:"name"`
		Children map[string]*mapSelfRef `json:"children"`
		Tag      *string                `json:"tag" default:"leaf"`
	}

	// 构造 A → Children["b"] → B → Children["a"] → A（环）
	a := &mapSelfRef{Name: "A", Children: map[string]*mapSelfRef{}}
	b := &mapSelfRef{Name: "B", Children: map[string]*mapSelfRef{}}
	a.Children["b"] = b
	b.Children["a"] = a

	reqPtr := reflect.ValueOf(a)
	meta := buildStructMeta(reflect.TypeOf(mapSelfRef{}))

	done := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		applyDefaults(reqPtr, meta, true)
		done <- nil
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("applyDefaults panicked or errored on map self-ref: %v", err)
		}
		// Tag 应该被填充
		if a.Tag == nil || *a.Tag != "leaf" {
			t.Fatalf("Tag should be filled with 'leaf', got %v", a.Tag)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("applyDefaults did not terminate on map self-referential struct (infinite recursion)")
	}
}

// ========== applyDefaults 三层嵌套指针 default 边界测试 ==========

// TestApplyDefaults_RequestPhase_DeepNesting 验证三层嵌套指针链中 default 填充行为
// 链路：Outer → *Mid → *Inner → *Value (default)
func TestApplyDefaults_RequestPhase_DeepNesting(t *testing.T) {
	type deepInner struct {
		Value *string `json:"value" default:"deep_ok"`
	}
	type deepMid struct {
		Inner *deepInner `json:"inner"`
	}
	type deepOuter struct {
		Mid *deepMid `json:"mid"`
	}

	t.Run("全链路非nil→最深层nil指针填充", func(t *testing.T) {
		req := &deepOuter{Mid: &deepMid{Inner: &deepInner{}}}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		applyDefaults(reflect.ValueOf(req), meta, true)
		if req.Mid.Inner.Value == nil || *req.Mid.Inner.Value != "deep_ok" {
			t.Fatal("deepest nil pointer should be filled with default")
		}
	})

	t.Run("中间层nil→深层不填充", func(t *testing.T) {
		req := &deepOuter{Mid: nil}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		applyDefaults(reflect.ValueOf(req), meta, true)
		if req.Mid != nil {
			t.Fatal("nil Mid should remain nil")
		}
	})

	t.Run("内层nil→最深层不填充", func(t *testing.T) {
		req := &deepOuter{Mid: &deepMid{Inner: nil}}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		applyDefaults(reflect.ValueOf(req), meta, true)
		if req.Mid.Inner != nil {
			t.Fatal("nil Inner should remain nil")
		}
	})

	t.Run("最深层已传值→不覆盖", func(t *testing.T) {
		s := "explicit"
		req := &deepOuter{Mid: &deepMid{Inner: &deepInner{Value: &s}}}
		meta := buildStructMeta(reflect.TypeOf(deepOuter{}))
		applyDefaults(reflect.ValueOf(req), meta, true)
		if *req.Mid.Inner.Value != "explicit" {
			t.Fatalf("explicit value should not be overwritten, got %q", *req.Mid.Inner.Value)
		}
	})
}

// ========== 默认值端到端绑定测试（经 HTTP 引擎完整链路验证） ==========

// defaultReq 通过 default 标签为字段设置默认值
type defaultReq struct {
	Keyword  string   `json:"keyword" default:"all"`
	Page     int      `json:"page" default:"1"`
	PageSize int      `json:"page_size" default:"20"`
	Sort     string   `json:"sort" default:"created_at"`
	Tags     []string `json:"tags" default:"a,b,c"`
}

type defaultRes struct {
	Keyword  string   `json:"keyword"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Sort     string   `json:"sort"`
	Tags     []string `json:"tags"`
}

func defaultHandler(_ context.Context, req defaultReq) (defaultRes, error) {
	return defaultRes{
		Keyword:  req.Keyword,
		Page:     req.Page,
		PageSize: req.PageSize,
		Sort:     req.Sort,
		Tags:     req.Tags,
	}, nil
}

// TestBindDefaultsAllMissing 验证无任何参数时所有带 default 的字段都被填充默认值
func TestBindDefaultsAllMissing(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/list", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Keyword != "all" || res.Page != 1 || res.PageSize != 20 || res.Sort != "created_at" {
		t.Fatalf("unexpected defaults: %+v", res)
	}
	if len(res.Tags) != 3 || res.Tags[0] != "a" || res.Tags[1] != "b" || res.Tags[2] != "c" {
		t.Fatalf("unexpected tags default: %v", res.Tags)
	}
}

// TestBindDefaultsPartial 验证已传递的参数不被默认值覆盖，未传递的才使用默认值
func TestBindDefaultsPartial(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	// 仅传递 keyword 与 page，其余用默认值
	req := httptest.NewRequest(http.MethodGet, "/list?keyword=go&page=5", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Keyword != "go" || res.Page != 5 {
		t.Fatalf("provided values overwritten: %+v", res)
	}
	if res.PageSize != 20 || res.Sort != "created_at" {
		t.Fatalf("missing fields not defaulted: %+v", res)
	}
	if len(res.Tags) != 3 {
		t.Fatalf("tags default not applied: %v", res.Tags)
	}
}

// TestBindDefaultsParseErrorKeepsDefault 验证"传递了但解析失败"时保留默认值
func TestBindDefaultsParseErrorKeepsDefault(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	// page=abc 无法解析为 int，应保留默认值 1
	req := httptest.NewRequest(http.MethodGet, "/list?page=abc", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Page != 1 {
		t.Fatalf("parse error should keep default 1, got %d", res.Page)
	}
}

// TestBindDefaultsExplicitZeroNotOverwritten 验证显式传递等于零值的值不会被默认值覆盖
func TestBindDefaultsExplicitZeroNotOverwritten(t *testing.T) {
	router := NewRouter()
	router.GET("/list", defaultHandler)

	engine := NewEngine()
	engine.Router = router

	// 显式传递 page=0，应保留 0 而非默认值 1
	req := httptest.NewRequest(http.MethodGet, "/list?page=0", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res defaultRes
	decodeData(t, rec, &res)
	if res.Page != 0 {
		t.Fatalf("explicit zero should be kept, got %d", res.Page)
	}
}

// -------- slice/map default 递归测试 --------

// itemWithDefault 作为切片/map 元素，包含 default 子字段
// Qty/Status 使用指针类型：在 slice/map 嵌套元素中，只有 nil 指针才会被请求阶段 default 填充，
// 值类型（如 int/string）的零值无法区分"未传"与"传了 0"，因此不在请求阶段填充。
type itemWithDefault struct {
	Name   string  `json:"name" nonzero:"true"`
	Qty    *int    `json:"qty" default:"1"`
	Status *string `json:"status" default:"active"`
	Note   string  `json:"note"`
}

// itemDefaultRes 用于回显默认值填充结果
type itemDefaultRes struct {
	Name   string `json:"name"`
	Qty    int    `json:"qty"`
	Status string `json:"status"`
	Note   string `json:"note"`
}

// ====== 切片 default 递归 ======

type orderDefaultSliceReq struct {
	OrderNo string             `json:"orderNo" nonzero:"true"`
	Items   []itemWithDefault  `json:"items" nonzero:"true"`
	Extras  []*itemWithDefault `json:"extras"`
}

type orderDefaultSliceRes struct {
	OrderNo string         `json:"orderNo"`
	First   itemDefaultRes `json:"first"`
	Count   int            `json:"count"`
}

func orderDefaultSliceHandler(_ context.Context, req orderDefaultSliceReq) (orderDefaultSliceRes, error) {
	res := orderDefaultSliceRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}
	if len(req.Items) > 0 {
		qty, status := 0, ""
		if req.Items[0].Qty != nil {
			qty = *req.Items[0].Qty
		}
		if req.Items[0].Status != nil {
			status = *req.Items[0].Status
		}
		res.First = itemDefaultRes{
			Name:   req.Items[0].Name,
			Qty:    qty,
			Status: status,
			Note:   req.Items[0].Note,
		}
	}
	return res, nil
}

// TestBindDefaultsSliceElem 切片元素未传 default 字段时，自动填充默认值
func TestBindDefaultsSliceElem(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultSlice", orderDefaultSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// items[0] 只传了 name（required），qty 和 status 应使用默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"item1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultSliceRes
	decodeData(t, rec, &res)
	if res.First.Qty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.First.Qty)
	}
	if res.First.Status != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.First.Status)
	}
}

// TestBindDefaultsSliceElemExplicit 切片元素显式传值时不覆盖
func TestBindDefaultsSliceElemExplicit(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultSlice", orderDefaultSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// 显式传 qty=5, status=pending → 不被默认值覆盖
	body := `{"orderNo":"ORD-001","items":[{"name":"item1","qty":5,"status":"pending"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultSliceRes
	decodeData(t, rec, &res)
	if res.First.Qty != 5 {
		t.Fatalf("explicit Qty should be 5, got %d", res.First.Qty)
	}
	if res.First.Status != "pending" {
		t.Fatalf("explicit Status should be 'pending', got %s", res.First.Status)
	}
}

// TestBindDefaultsSliceElemPtr 指针切片元素默认值填充
func TestBindDefaultsSliceElemPtr(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultSlice", orderDefaultSliceHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 中元素未传 qty/status → 填充默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"i1"}],"extras":[{"name":"e1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultSlice", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		t.Log("slice elem ptr default test passed")
	}
}

// ====== map default 递归 ======

type orderDefaultMapReq struct {
	OrderNo string                      `json:"orderNo" nonzero:"true"`
	Items   map[string]itemWithDefault  `json:"items" nonzero:"true"`
	Extras  map[string]*itemWithDefault `json:"extras"`
}

type orderDefaultMapRes struct {
	OrderNo string `json:"orderNo"`
	Aqty    int    `json:"aqty"`
	Astatus string `json:"astatus"`
	Count   int    `json:"count"`
}

func orderDefaultMapHandler(_ context.Context, req orderDefaultMapReq) (orderDefaultMapRes, error) {
	res := orderDefaultMapRes{OrderNo: req.OrderNo, Count: len(req.Items) + len(req.Extras)}
	if v, ok := req.Items["a"]; ok {
		if v.Qty != nil {
			res.Aqty = *v.Qty
		}
		if v.Status != nil {
			res.Astatus = *v.Status
		}
	}
	return res, nil
}

// TestBindDefaultsMapValue map value 未传 default 字段时，自动填充默认值
func TestBindDefaultsMapValue(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultMap", orderDefaultMapHandler)

	engine := NewEngine()
	engine.Router = router

	// items["a"] 只传了 name → qty/status 应使用默认值
	body := `{"orderNo":"ORD-001","items":{"a":{"name":"itemA"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultMapRes
	decodeData(t, rec, &res)
	if res.Aqty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.Aqty)
	}
	if res.Astatus != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.Astatus)
	}
}

// TestBindDefaultsMapValueExplicit map value 显式传值时不覆盖
func TestBindDefaultsMapValueExplicit(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultMap", orderDefaultMapHandler)

	engine := NewEngine()
	engine.Router = router

	// 显式传 qty=10, status=done → 不被默认值覆盖
	body := `{"orderNo":"ORD-001","items":{"a":{"name":"itemA","qty":10,"status":"done"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res orderDefaultMapRes
	decodeData(t, rec, &res)
	if res.Aqty != 10 {
		t.Fatalf("explicit Qty should be 10, got %d", res.Aqty)
	}
	if res.Astatus != "done" {
		t.Fatalf("explicit Status should be 'done', got %s", res.Astatus)
	}
}

// TestBindDefaultsMapValuePtr 指针 map value 默认值填充
func TestBindDefaultsMapValuePtr(t *testing.T) {
	router := NewRouter()
	router.POST("/orderDefaultMap", orderDefaultMapHandler)

	engine := NewEngine()
	engine.Router = router

	// extras 中 value 未传 qty/status → 填充默认值
	body := `{"orderNo":"ORD-001","items":{"a":{"name":"iA"}},"extras":{"x":{"name":"eX"}}}`
	req := httptest.NewRequest(http.MethodPost, "/orderDefaultMap", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	if rec.Code == http.StatusOK {
		t.Log("map value ptr default test passed")
	}
}

// ========== 指针包裹容器 default 递归 ==========

// ptrContainerReq 包含 *[]Struct 和 *map[K]Struct 字段
type ptrContainerReq struct {
	OrderNo string                       `json:"orderNo" nonzero:"true"`
	Items   *[]itemWithDefault           `json:"items" nonzero:"true"`
	Extras  *map[string]*itemWithDefault `json:"extras"`
}

type ptrContainerRes struct {
	OrderNo string         `json:"orderNo"`
	First   itemDefaultRes `json:"first"`
	Count   int            `json:"count"`
}

func ptrContainerHandler(_ context.Context, req ptrContainerReq) (ptrContainerRes, error) {
	res := ptrContainerRes{OrderNo: req.OrderNo}
	if req.Items != nil && len(*req.Items) > 0 {
		first := (*req.Items)[0]
		qty, status := 0, ""
		if first.Qty != nil {
			qty = *first.Qty
		}
		if first.Status != nil {
			status = *first.Status
		}
		res.First = itemDefaultRes{
			Name:   first.Name,
			Qty:    qty,
			Status: status,
			Note:   first.Note,
		}
		res.Count = len(*req.Items)
	}
	if req.Extras != nil {
		res.Count += len(*req.Extras)
	}
	return res, nil
}

// TestBindDefaultsPtrSliceElem *[]Struct 元素未传 default 字段时，自动填充默认值
func TestBindDefaultsPtrSliceElem(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrContainer", ptrContainerHandler)

	engine := NewEngine()
	engine.Router = router

	// items[0] 只传了 name（required），qty 和 status 应使用默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"item1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/ptrContainer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res ptrContainerRes
	decodeData(t, rec, &res)
	if res.First.Qty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.First.Qty)
	}
	if res.First.Status != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.First.Status)
	}
}

// TestBindDefaultsPtrMapValue *map[K]Struct value 未传 default 字段时，自动填充默认值
func TestBindDefaultsPtrMapValue(t *testing.T) {
	router := NewRouter()
	router.POST("/ptrContainer", func(_ context.Context, req ptrContainerReq) (struct {
		Aqty    int    `json:"aqty"`
		Astatus string `json:"astatus"`
	}, error) {
		res := struct {
			Aqty    int    `json:"aqty"`
			Astatus string `json:"astatus"`
		}{}
		if req.Extras != nil {
			if v, ok := (*req.Extras)["a"]; ok && v != nil {
				if v.Qty != nil {
					res.Aqty = *v.Qty
				}
				if v.Status != nil {
					res.Astatus = *v.Status
				}
			}
		}
		return res, nil
	})

	engine := NewEngine()
	engine.Router = router

	// extras["a"] 只传了 name → qty/status 应使用默认值
	body := `{"orderNo":"ORD-001","items":[{"name":"i1"}],"extras":{"a":{"name":"itemA"}}}`
	req := httptest.NewRequest(http.MethodPost, "/ptrContainer", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res struct {
		Aqty    int    `json:"aqty"`
		Astatus string `json:"astatus"`
	}
	decodeData(t, rec, &res)
	if res.Aqty != 1 {
		t.Fatalf("default Qty should be 1, got %d", res.Aqty)
	}
	if res.Astatus != "active" {
		t.Fatalf("default Status should be 'active', got %s", res.Astatus)
	}
}

// ---- 并发隔离 ----
type concurrentReq struct {
	Status *string `json:"status" default:"idle"`
}
type concurrentRes struct {
	Status string `json:"status"`
}

func concurrentHandler(_ context.Context, req concurrentReq) (concurrentRes, error) {
	s := ""
	if req.Status != nil {
		s = *req.Status
	}
	return concurrentRes{Status: s}, nil
}

// ---- 指针嵌套 struct 默认值 ----
type billAddress struct {
	City string `json:"city" default:"Beijing"`
	Zip  *int   `json:"zip" default:"100000"`
}
type billOrderReq struct {
	Name    string       `json:"name" nonzero:"true"`
	Address *billAddress `json:"address"`
}
type billOrderRes struct {
	City string `json:"city"`
	Zip  int    `json:"zip"`
}

func billOrderHandler(_ context.Context, req billOrderReq) (billOrderRes, error) {
	r := billOrderRes{}
	if req.Address != nil {
		r.City = req.Address.City
		if req.Address.Zip != nil {
			r.Zip = *req.Address.Zip
		}
	}
	return r, nil
}

// TestUnsupportedDefaultTypeSilentlyIgnored 验证不支持 default 的类型（time.Time/map/any）设置 default 被静默忽略
func TestUnsupportedDefaultTypeSilentlyIgnored(t *testing.T) {
	type unsupportedDefReq struct {
		Name string    `json:"name"`
		When time.Time `json:"when" default:"2023-01-01"` // 不支持
	}
	type unsupportedDefRes struct {
		IsZero bool `json:"is_zero"`
	}

	router := NewRouter()
	router.GET("/ud", func(_ context.Context, r unsupportedDefReq) (unsupportedDefRes, error) {
		return unsupportedDefRes{IsZero: r.When.IsZero()}, nil
	})
	engine := NewEngine()
	engine.Router = router

	httpReq := httptest.NewRequest(http.MethodGet, "/ud", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var r unsupportedDefRes
	decodeData(t, rec, &r)
	// time.Time 不支持 default，应被忽略，保持零值
	if !r.IsZero {
		t.Fatal("time.Time default should be silently ignored, field should remain zero")
	}
}

// TestConcurrentRequestIsolation 验证并发请求间默认值模板不共享（深拷贝断开引用）
func TestConcurrentRequestIsolation(t *testing.T) {
	router := NewRouter()
	router.POST("/conc", concurrentHandler)
	engine := NewEngine()
	engine.Router = router

	const n = 20
	results := make([]string, n)
	errs := make([]error, n)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// 奇数显式传值，偶数不传（使用默认值 "idle"）
			var body string
			if idx%2 == 1 {
				body = `{"status":"active"}`
			} else {
				body = `{}`
			}
			httpReq := httptest.NewRequest(http.MethodPost, "/conc", strings.NewReader(body))
			httpReq.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, httpReq)

			mu.Lock()
			defer mu.Unlock()
			if rec.Code != http.StatusOK {
				errs[idx] = fmt.Errorf("request %d: expected 200, got %d", idx, rec.Code)
				return
			}
			var r concurrentRes
			decodeData(t, rec, &r)
			results[idx] = r.Status
		}(i)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	// 验证结果无交叉污染
	for i := 0; i < n; i++ {
		if i%2 == 1 {
			if results[i] != "active" {
				t.Fatalf("request %d (odd): expected 'active', got %q", i, results[i])
			}
		} else {
			if results[i] != "idle" {
				t.Fatalf("request %d (even): expected 'idle', got %q", i, results[i])
			}
		}
	}
}

// TestPointerNestedStructDefault 验证指针嵌套 struct 中：值类型 default 不生效，指针类型 default 生效
func TestPointerNestedStructDefault(t *testing.T) {
	router := NewRouter()
	router.POST("/bill", billOrderHandler)
	engine := NewEngine()
	engine.Router = router

	// 传 address:{} → City 值类型 default 不生效，Zip 指针类型 default 生效
	body := `{"name":"alice","address":{}}`
	req := httptest.NewRequest(http.MethodPost, "/bill", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res billOrderRes
	decodeData(t, rec, &res)
	// 值类型 default 在指针嵌套下不生效
	if res.City != "" {
		t.Fatalf("value-type default in ptr-nested struct should NOT apply, got %q", res.City)
	}
	// 指针类型 default 在请求阶段补填生效
	if res.Zip != 100000 {
		t.Fatalf("ptr-type default in ptr-nested struct should apply, got %d, want 100000", res.Zip)
	}
}

// ========== P2-02: 数组默认值测试 ==========

// TestBindDefaultsArrayElem 验证数组元素的 default 标签生效
func TestBindDefaultsArrayElem(t *testing.T) {
	type item struct {
		Qty  *int   `json:"qty" default:"5"`
		Name string `json:"name"`
	}
	type req struct {
		Items [2]item `json:"items"`
	}
	type res struct {
		Items [2]item `json:"items"`
	}

	router := NewRouter()
	router.POST("/test", func(_ context.Context, r req) (res, error) {
		return res{Items: r.Items}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	httpReq := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"items":[{"name":"a"},{"name":"b"}]}`))
	httpReq.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", rec.Code, rec.Body.String())
	}

	var result struct {
		Data struct {
			Items []struct {
				Qty  *int   `json:"qty"`
				Name string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	for i, item := range result.Data.Items {
		if item.Qty == nil {
			t.Fatalf("items[%d].qty should be filled with default 5, got nil", i)
		}
		if *item.Qty != 5 {
			t.Fatalf("items[%d].qty = %d, want 5", i, *item.Qty)
		}
	}
}

// ========== m1: 切片/数组为外层的多层容器警告 ==========

// TestCheckUnsupportedDefaults_MultiLayerContainerWarning 验证切片/数组为外层的多层容器
// （[][]Struct、[]map[K]Struct）中带 default 的字段触发启动期警告，与 map 分支的
// "never applied" 警告行为对齐；单层切片（[]Struct）为对照组：defaults 可达，不警告。
func TestCheckUnsupportedDefaults_MultiLayerContainerWarning(t *testing.T) {
	var buf bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(old)

	type mclLeaf struct {
		Qty *int `json:"qty" default:"1"`
	}

	// 对照组：单层切片，applyDefaults 可穿透到 struct 元素 → 不警告
	type reqSingleSlice struct {
		Items []mclLeaf `json:"items"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqSingleSlice{}), true, true, "POST", "/ss", "handler", "file.go", 1, make(map[reflect.Type]bool))
	if buf.Len() != 0 {
		t.Fatalf("single-layer slice is defaults-reachable, expected no warning, got: %s", buf.String())
	}

	// [][]Struct：外层切片→内层切片→struct，applyDefaults 无法穿透 → 应告警
	type reqDoubleSlice struct {
		Matrix [][]mclLeaf `json:"matrix"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqDoubleSlice{}), true, true, "POST", "/ds", "handler", "file.go", 1, make(map[reflect.Type]bool))
	out := buf.String()
	if !strings.Contains(out, "never applied") {
		t.Fatalf("[][]Struct should warn 'never applied', got: %s", out)
	}
	if !strings.Contains(out, "mclLeaf") {
		t.Fatalf("warning should identify struct mclLeaf, got: %s", out)
	}

	// []map[K]Struct：外层切片→map→struct，applyDefaults 无法穿透 → 应告警
	buf.Reset()
	type reqSliceMap struct {
		Rows []map[string]mclLeaf `json:"rows"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqSliceMap{}), true, true, "POST", "/sm", "handler", "file.go", 1, make(map[reflect.Type]bool))
	out = buf.String()
	if !strings.Contains(out, "never applied") {
		t.Fatalf("[]map[K]Struct should warn 'never applied', got: %s", out)
	}

	// [][2]Struct：外层切片→内层数组→struct，同样无法穿透 → 应告警
	buf.Reset()
	type reqSliceArray struct {
		Grid [][2]mclLeaf `json:"grid"`
	}
	checkUnsupportedDefaults(reflect.TypeOf(reqSliceArray{}), true, true, "POST", "/sa", "handler", "file.go", 1, make(map[reflect.Type]bool))
	out = buf.String()
	if !strings.Contains(out, "never applied") {
		t.Fatalf("[][2]Struct should warn 'never applied', got: %s", out)
	}
}

// ========== n4: 空切片默认值（default:"" 视为空切片） ==========

// TestApplyDefaults_EmptySliceDefault 验证 default:""（及空白/纯逗号）在注册阶段产生空切片
// （len=0 的非 nil 切片），而非含单个空元素的切片
func TestApplyDefaults_EmptySliceDefault(t *testing.T) {
	cases := []struct {
		name string
		typ  reflect.Type
	}{
		{"empty string", reflect.TypeOf(struct {
			Tags []string `json:"tags" default:""`
		}{})},
		{"whitespace only", reflect.TypeOf(struct {
			Tags []string `json:"tags" default:"   "`
		}{})},
		{"commas only", reflect.TypeOf(struct {
			Tags []string `json:"tags" default:",,,"`
		}{})},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			reqPtr := reflect.New(c.typ)
			meta := buildStructMeta(c.typ)
			applyDefaults(reqPtr, meta) // 注册阶段

			fv := reqPtr.Elem().FieldByName("Tags")
			if fv.Kind() != reflect.Slice {
				t.Fatalf("Tags kind = %v, want Slice", fv.Kind())
			}
			if fv.IsNil() {
				t.Fatal("default should produce a non-nil empty slice, got nil")
			}
			if fv.Len() != 0 {
				t.Fatalf("default should produce an empty slice, got len=%d (%v)", fv.Len(), fv.Interface())
			}
		})
	}
}

// TestApplyDefaults_EmptySliceDefaultE2E 端到端锁死：default:"" 的切片字段经完整链路后为空切片，
// 且客户端显式传值时不被默认值覆盖
func TestApplyDefaults_EmptySliceDefaultE2E(t *testing.T) {
	type emptySliceReq struct {
		Tags []string `json:"tags" default:""`
	}
	type emptySliceRes struct {
		TagLen int      `json:"tag_len"`
		Tags   []string `json:"tags"`
	}

	router := NewRouter()
	router.POST("/tags", func(_ context.Context, req emptySliceReq) (emptySliceRes, error) {
		return emptySliceRes{TagLen: len(req.Tags), Tags: req.Tags}, nil
	})

	engine := NewEngine()
	engine.Router = router

	// 未传 tags：default:"" → 空切片（len=0），而非 [""]（len=1）
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/tags", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	var res emptySliceRes
	decodeData(t, rec, &res)
	if res.TagLen != 0 {
		t.Fatalf("default:\"\" should produce empty slice, got len=%d (%v)", res.TagLen, res.Tags)
	}

	// 显式传值：不被默认值覆盖
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/tags", strings.NewReader(`{"tags":["a","b"]}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", rec.Code, rec.Body.String())
	}
	res = emptySliceRes{}
	decodeData(t, rec, &res)
	if res.TagLen != 2 || res.Tags[0] != "a" || res.Tags[1] != "b" {
		t.Fatalf("explicit tags should be kept, got %v", res.Tags)
	}
}

// ========== Interface（any）字段分支测试（Minor-2） ==========
// 配套修复：hasRefFields / deepCopyDefaults 补 reflect.Interface 分支，
// 消除“default 白名单不放开 any”的隐式约定

// TestHasRefFields_NonNilInterface 验证 any 字段动态值为非 nil 指针时返回 true
func TestHasRefFields_NonNilInterface(t *testing.T) {
	type withAny struct {
		V any
	}
	n := 1
	v := reflect.ValueOf(withAny{V: &n})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil pointer inside any field should return true")
	}
}

// TestHasRefFields_NilInterface 验证 nil 的 any 字段返回 false
func TestHasRefFields_NilInterface(t *testing.T) {
	type withAny struct {
		V any
	}
	v := reflect.ValueOf(withAny{})
	if hasRefFields(v) {
		t.Fatal("struct with nil any field should return false")
	}
}

// TestHasRefFields_InterfaceNestedRef 验证 any 字段动态值为结构体且内含非 nil 引用时返回 true
func TestHasRefFields_InterfaceNestedRef(t *testing.T) {
	type inner struct {
		Ref *int
	}
	type withAny struct {
		V any
	}
	n := 1
	v := reflect.ValueOf(withAny{V: inner{Ref: &n}})
	if !hasRefFields(v) {
		t.Fatal("struct with non-nil ref nested in any field should return true")
	}
}

// TestDeepCopyDefaults_InterfaceNotShared 验证 any 字段动态指针经深拷贝后与原值断开共享
func TestDeepCopyDefaults_InterfaceNotShared(t *testing.T) {
	type withAny struct {
		V any
	}
	inner := &deepCopyInner{Name: "orig", Age: 10}
	v := reflect.ValueOf(&withAny{V: inner}).Elem()

	origAddr := v.FieldByName("V").Elem().Pointer()

	deepCopyDefaults(v)

	// 动态指针已断开共享：地址不同
	dyn := v.FieldByName("V").Elem()
	if dyn.Pointer() == origAddr {
		t.Fatal("any dynamic pointer still shared after deepCopyDefaults")
	}
	// 内容一致
	copied := dyn.Interface().(*deepCopyInner)
	if copied.Name != "orig" || copied.Age != 10 {
		t.Fatalf("content mismatch after deep copy: %+v", copied)
	}
	// 修改拷贝不影响原值（共享已断开）
	copied.Name = "mutated"
	if inner.Name != "orig" {
		t.Fatal("modifying deep-copied dynamic value affected the original")
	}
}

// TestDeepCopyDefaults_NilInterface 验证 nil 的 any 字段深拷贝不 panic、保持 nil
func TestDeepCopyDefaults_NilInterface(t *testing.T) {
	type withAny struct {
		V any
	}
	v := reflect.ValueOf(&withAny{}).Elem()
	deepCopyDefaults(v)
	if !v.FieldByName("V").IsNil() {
		t.Fatal("nil any field should remain nil after deepCopyDefaults")
	}
}
