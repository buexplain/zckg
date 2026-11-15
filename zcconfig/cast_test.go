package zcconfig

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// castProbeStringer 用于验证接口目标只接受直接类型断言（值必须实现该接口）。
type castProbeStringer struct{ Label string }

func (s castProbeStringer) String() string { return s.Label }

// castProbeStructA/B/Tagged/Diff 用于验证结构体源值的 reflect 转换与拒绝路径：
// A→B 与 A→Tagged 底层类型相同（tag 不参与底层类型判定），A→Diff 字段不同不可转换。
type castProbeStructA struct {
	X int
	Y string
}

type castProbeStructB struct {
	X int
	Y string
}

type castProbeStructTagged struct {
	X int    `json:"x"`
	Y string `json:"y"`
}

type castProbeStructDiff struct {
	X int
	Z string
}

// castCase 构造单条 cast 成功断言子测试，供 table-driven 测试按行复用，
// 目标类型由 def/want 的类型推断。
func castCase[T comparable](input any, def, want T) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		got, ok := cast(input, def)
		if !ok {
			t.Fatalf("cast(%v, def=%v) 期望可转换，实际转换失败", input, def)
		}
		if got != want {
			t.Errorf("cast(%v, def=%v) 期望 %v，实际 %v", input, def, want, got)
		}
	}
}

// castFails 构造单条 cast 失败断言子测试：期望返回 (def, false)，此时由 Env/Config 决定 panic。
func castFails[T comparable](input any, def T) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		got, ok := cast(input, def)
		if ok {
			t.Fatalf("cast(%v, def=%v) 期望转换失败，实际成功返回 %v", input, def, got)
		}
		if got != def {
			t.Errorf("转换失败时应返回 def %v，实际 %v", def, got)
		}
	}
}

// assertTypeMismatchPanic 断言 fn panic 出 *TypeMismatchError，且 Key / Expected / Actual
// 与错误消息三要素（key、期望类型、实际类型）均符合预期。
func assertTypeMismatchPanic(t *testing.T, key string, expected, actual reflect.Type, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("期望 panic(*TypeMismatchError)，实际正常返回")
		}
		err, ok := r.(error)
		if !ok {
			t.Fatalf("panic 值期望实现 error，实际 %T(%v)", r, r)
		}
		var mismatch *TypeMismatchError
		if !errors.As(err, &mismatch) {
			t.Fatalf("panic 值期望可 errors.As 为 *TypeMismatchError，实际 %T(%v)", err, err)
		}
		if mismatch.Key != key {
			t.Errorf("Key 期望 %q，实际 %q", key, mismatch.Key)
		}
		if mismatch.Expected != expected {
			t.Errorf("Expected 期望 %v，实际 %v", expected, mismatch.Expected)
		}
		if mismatch.Actual != actual {
			t.Errorf("Actual 期望 %v，实际 %v", actual, mismatch.Actual)
		}
		msg := mismatch.Error()
		for _, want := range []string{"zcconfig: key", fmt.Sprintf("%q", key), expected.String(), actual.String()} {
			if !strings.Contains(msg, want) {
				t.Errorf("错误消息 %q 期望包含 %q", msg, want)
			}
		}
	}()
	fn()
}

// --- nil 源值 ---

// TestCast_NilNotConvertible 验证 nil 源值不参与转换：返回 (def, false)，
// 由 Env/Config 的取值策略把 nil 视为“无值”返回默认值，而不是类型不符。
func TestCast_NilNotConvertible(t *testing.T) {
	if got, ok := cast[string](nil, "fallback"); ok || got != "fallback" {
		t.Errorf("nil -> string 期望 (fallback,false)，实际 (%q,%v)", got, ok)
	}
	if got, ok := cast[int](nil, 42); ok || got != 42 {
		t.Errorf("nil -> int 期望 (42,false)，实际 (%d,%v)", got, ok)
	}
	if got, ok := cast[bool](nil, true); ok || got != true {
		t.Errorf("nil -> bool 期望 (true,false)，实际 (%v,%v)", got, ok)
	}
	if got, ok := cast[float64](nil, 1.5); ok || got != 1.5 {
		t.Errorf("nil -> float64 期望 (1.5,false)，实际 (%v,%v)", got, ok)
	}
}

// TestCastOrPanic_NilReturnsDefault 验证 key 存在但值为 nil 时按缺省处理：返回 def，不 panic。
func TestCastOrPanic_NilReturnsDefault(t *testing.T) {
	if got := castOrPanic("NIL_STRING", nil, "fallback"); got != "fallback" {
		t.Errorf("nil -> string 期望 fallback，实际 %q", got)
	}
	if got := castOrPanic("NIL_INT", nil, 42); got != 42 {
		t.Errorf("nil -> int 期望 42，实际 %d", got)
	}
}

// --- TypeMismatchError 诊断信息 ---

// TestTypeMismatchError_Message 验证错误消息精确包含 key、期望类型与实际类型。
func TestTypeMismatchError_Message(t *testing.T) {
	err := &TypeMismatchError{
		Key:      "db.port",
		Expected: reflect.TypeFor[int](),
		Actual:   reflect.TypeFor[string](),
	}
	want := `zcconfig: key "db.port" 类型不匹配：期望 int，实际 string`
	if got := err.Error(); got != want {
		t.Errorf("Error() 期望 %q，实际 %q", want, got)
	}
}

// TestCastOrPanic_TypeMismatchDiagnostics 验证转换失败 panic 的三要素：
// key 为调用方原始 key，期望类型取目标 T 的静态类型，实际类型取存储值动态类型。
func TestCastOrPanic_TypeMismatchDiagnostics(t *testing.T) {
	assertTypeMismatchPanic(t, "DB_PORT", reflect.TypeFor[int](), reflect.TypeFor[string](), func() {
		castOrPanic("DB_PORT", "abc", 0)
	})

	// 目标为接口类型且值未实现该接口：期望类型报接口本身，而不是 def 的动态类型。
	assertTypeMismatchPanic(t, "ERR", reflect.TypeFor[error](), reflect.TypeFor[int](), func() {
		castOrPanic[error]("ERR", 42, nil)
	})
}

// TestCast_InterfaceTargetRequiresImplementation 验证接口目标只接受直接类型断言，
// 未实现接口的值不参与任何后续转换。
func TestCast_InterfaceTargetRequiresImplementation(t *testing.T) {
	if got, ok := cast[error]("not-an-error", nil); ok || got != nil {
		t.Errorf("string 未实现 error，期望 (nil,false)，实际 (%v,%v)", got, ok)
	}
	if got, ok := cast[fmt.Stringer](42, nil); ok || got != nil {
		t.Errorf("int 未实现 fmt.Stringer，期望 (nil,false)，实际 (%v,%v)", got, ok)
	}

	wantErr := errors.New("boom")
	if got, ok := cast[error](wantErr, nil); !ok || !errors.Is(got, wantErr) {
		t.Errorf("error 实现值期望断言命中，实际 (%v,%v)", got, ok)
	}
	if got, ok := cast[fmt.Stringer](castProbeStringer{Label: "s"}, nil); !ok || got.String() != "s" {
		t.Errorf("Stringer 实现值期望断言命中，实际 (%v,%v)", got, ok)
	}
}

// --- string -> string ---

func TestCast_StringToString(t *testing.T) {
	if v, ok := cast("hello", "fallback"); !ok || v != "hello" {
		t.Errorf("期望 'hello'，实际 (%q,%v)", v, ok)
	}
	if v, ok := cast("", "fallback"); !ok || v != "" {
		t.Errorf("空字符串期望 ''，实际 (%q,%v)", v, ok)
	}
	if v, ok := cast("中文内容", "fallback"); !ok || v != "中文内容" {
		t.Errorf("期望 '中文内容'，实际 (%q,%v)", v, ok)
	}
	if v, ok := cast("with spaces", "fallback"); !ok || v != "with spaces" {
		t.Errorf("期望 'with spaces'，实际 (%q,%v)", v, ok)
	}
}

// --- string -> int 系列（table-driven，合并原 ToInt/ToInt64/ToInt32/ToInt16/ToInt8 正常路径） ---

func TestCast_StringToIntSeries(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"int 正数", castCase("42", 0, 42)},
		{"int 负数", castCase("-42", 0, -42)},
		{"int 零", castCase("0", 0, 0)},
		{"int64 最大值", castCase("9223372036854775807", int64(0), int64(9223372036854775807))},
		{"int64 最小值", castCase("-9223372036854775808", int64(0), int64(-9223372036854775808))},
		{"int32 最大值", castCase("2147483647", int32(0), int32(2147483647))},
		{"int16 最大值", castCase("32767", int16(0), int16(32767))},
		{"int8 最大值", castCase("127", int8(0), int8(127))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestCast_StringToIntOverflow(t *testing.T) {
	// 超出目标范围不可转换；def 取非零值，避免与“失败返回类型零值”混淆
	for _, run := range []func(t *testing.T){
		castFails("200", int8(77)),
		castFails("99999999999", int32(42)),
	} {
		run(t)
	}
}

func TestCast_StringToIntInvalid(t *testing.T) {
	// 各用例 def 取互不相同的非零值，避免与“失败返回类型零值”混淆
	for _, run := range []func(t *testing.T){
		castFails("abc", 99),
		castFails("12.5", 88),
		castFails("", 77),
		castFails("  42", 66),
	} {
		run(t)
	}
}

// --- string -> uint 系列（table-driven，合并原 ToUint/ToUint64/ToUint32 正常路径） ---

func TestCast_StringToUintSeries(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"uint 正数", castCase("42", uint(0), uint(42))},
		{"uint 零", castCase("0", uint(0), uint(0))},
		{"uint64 最大值", castCase("18446744073709551615", uint64(0), uint64(18446744073709551615))},
		{"uint32 最大值", castCase("4294967295", uint32(0), uint32(4294967295))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestCast_StringToUintNegative(t *testing.T) {
	// 负数无法转 uint，不可转换
	castFails("-1", uint(0))(t)
}

func TestCast_StringToUintOverflow(t *testing.T) {
	// def 取非零值，避免与“失败返回类型零值”混淆
	castFails("300", uint8(77))(t)
}

// --- string -> float 系列 ---

// table-driven，合并原 ToFloat64/ToFloat32 正常路径
func TestCast_StringToFloatSeries(t *testing.T) {
	tests := []struct {
		name string
		run  func(t *testing.T)
	}{
		{"float64 小数", castCase("3.14", 0.0, 3.14)},
		{"float64 负小数", castCase("-0.5", 0.0, -0.5)},
		{"float64 零", castCase("0", 0.0, 0.0)},
		{"float64 整数字符串", castCase("100", 0.0, 100.0)},
		{"float32 小数", castCase("1.5", float32(0), float32(1.5))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

func TestCast_StringToFloatInvalid(t *testing.T) {
	// def 取非零值，避免与“失败返回类型零值”混淆
	for _, run := range []func(t *testing.T){
		castFails("abc", 1.23),
		castFails("", 4.56),
	} {
		run(t)
	}
}

func TestCast_StringToFloatNaNInf(t *testing.T) {
	// NaN/Inf 字面量可被 ParseFloat 接受，但配置场景应视为非法输入、不可转换
	for _, run := range []func(t *testing.T){
		castFails("NaN", 0.0),
		castFails("Inf", 1.5),
		castFails("-Inf", 2.5),
		castFails("1e999", 3.5),
	} {
		run(t)
	}
}

// --- string -> bool ---

func TestCast_StringToBool(t *testing.T) {
	validTrue := []string{"1", "t", "T", "true", "TRUE", "True"}
	for _, s := range validTrue {
		if v, ok := cast(s, false); !ok || v != true {
			t.Errorf("字符串 %q 转 bool 期望 true，实际 (%v,%v)", s, v, ok)
		}
	}
	validFalse := []string{"0", "f", "F", "false", "FALSE", "False"}
	for _, s := range validFalse {
		if v, ok := cast(s, true); !ok || v != false {
			t.Errorf("字符串 %q 转 bool 期望 false，实际 (%v,%v)", s, v, ok)
		}
	}
}

func TestCast_StringToBoolInvalid(t *testing.T) {
	for _, run := range []func(t *testing.T){
		castFails("yes", false),
		castFails("", true),
		castFails("2", false),
	} {
		run(t)
	}
}

// --- string -> time.Duration ---

func TestCast_StringToDuration(t *testing.T) {
	cases := []struct {
		s   string
		exp time.Duration
	}{
		{"10s", 10 * time.Second},
		{"1h30m", 90 * time.Minute},
		{"500ms", 500 * time.Millisecond},
		{"100us", 100 * time.Microsecond},
		{"100ns", 100 * time.Nanosecond},
		{"1h0m0s", time.Hour},
		{"0s", 0},
	}
	for _, c := range cases {
		if v, ok := cast(c.s, time.Duration(0)); !ok || v != c.exp {
			t.Errorf("%q 转 Duration 期望 %v，实际 (%v,%v)", c.s, c.exp, v, ok)
		}
	}
}

func TestCast_StringToDurationInvalid(t *testing.T) {
	def := 5 * time.Second
	for _, run := range []func(t *testing.T){
		castFails("abc", def),
		castFails("", def),
		castFails("10", def),
	} {
		run(t)
	}
}

// --- string -> 非数值类型（走 default 分支） ---

func TestCast_StringToStructRejected(t *testing.T) {
	type MyStruct struct{ X int }
	def := MyStruct{X: 99}
	castFails("hello", def)(t)
}

// --- 非 string 源值，走 reflect ConvertibleTo 路径 ---

func TestCast_IntToFloat64(t *testing.T) {
	if v, ok := cast(42, 0.0); !ok || v != 42.0 {
		t.Errorf("int -> float64 期望 42.0，实际 (%v,%v)", v, ok)
	}
}

func TestCast_Int64ToInt(t *testing.T) {
	if v, ok := cast(int64(100), 0); !ok || v != 100 {
		t.Errorf("int64 -> int 期望 100，实际 (%d,%v)", v, ok)
	}
}

func TestCast_Float64ToFloat32(t *testing.T) {
	if v, ok := cast(3.14, float32(0)); !ok || v != float32(3.14) {
		t.Errorf("float64 -> float32 期望 %f，实际 (%f,%v)", float32(3.14), v, ok)
	}
}

// ZCC-02：reflect 兜底采用 Go 原生类型转换语义，float -> int 向零截断。
// 该截断语义已在 docs/zcconfig.md 显式声明，本测试固化截断行为：
// 典型触发链为 .env 中 PRICE=99.99 被推断为 float64，调用方 Env("PRICE", 0) 得到 99。
func TestCast_Float64ToIntTruncation(t *testing.T) {
	if v, ok := cast(99.99, 0); !ok || v != 99 {
		t.Errorf("float64(99.99) -> int 期望向零截断为 99，实际 (%d,%v)", v, ok)
	}
	if v, ok := cast(-99.99, 0); !ok || v != -99 {
		t.Errorf("float64(-99.99) -> int 期望向零截断为 -99，实际 (%d,%v)", v, ok)
	}
	if v, ok := cast(100.0, 0); !ok || v != 100 {
		t.Errorf("float64(100.0) -> int 期望 100，实际 (%d,%v)", v, ok)
	}
	if v, ok := cast(1.5, int8(0)); !ok || v != int8(1) {
		t.Errorf("float64(1.5) -> int8 期望向零截断为 1，实际 (%d,%v)", v, ok)
	}
}

// --- 数值类型 → bool 特判（C 惯例：零值为 false） ---

func TestCast_IntToBool(t *testing.T) {
	// 数值类型 -> bool 特判：C 语言惯例，零值为 false，非零为 true。
	// 此特判解决 .env 中 "1"/"0" 被 parseValue 推断为 int 后无法转 bool 的问题。
	cases := []struct {
		value any
		def   bool
		want  bool
	}{
		{1, false, true},
		{0, false, false},
		{int64(42), false, true},
		{0, true, false},
		{3.14, false, true},
		{0.0, true, false},
	}
	for _, c := range cases {
		if v, ok := cast(c.value, c.def); !ok || v != c.want {
			t.Errorf("%T(%v) -> bool 期望 %v，实际 (%v,%v)", c.value, c.value, c.want, v, ok)
		}
	}
}

func TestCast_UintToBool(t *testing.T) {
	// uint 系列 -> bool 走同一 C 惯例分支（cast.go 的 uint 分支）。
	cases := []struct {
		value any
		def   bool
		want  bool
	}{
		{uint(1), false, true},
		{uint(0), true, false},
		{uint64(42), false, true},
		{uint32(0), true, false},
	}
	for _, c := range cases {
		if v, ok := cast(c.value, c.def); !ok || v != c.want {
			t.Errorf("%T(%v) -> bool 期望 %v，实际 (%v,%v)", c.value, c.value, c.want, v, ok)
		}
	}
}

// --- 数值类型 → string 防护（拒绝 Unicode 码点语义） ---

func TestCast_IntToStringRejected(t *testing.T) {
	// 数值类型 -> string 是 Unicode 码点语义（65 -> "A"），不是数字转字符串，应不可转换
	for _, run := range []func(t *testing.T){
		castFails(65, "fallback"),
		castFails(int64(100), "def"),
		castFails(uint(42), ""),
		castFails(3.14, "def"),
	} {
		run(t)
	}
}

func TestCast_BoolToIntNotConvertible(t *testing.T) {
	// bool 不可转换为 int，应不可转换
	castFails(true, 0)(t)
}

// --- 直接类型断言命中 ---

func TestCast_DirectAssertion(t *testing.T) {
	if v, ok := cast(42, 0); !ok || v != 42 {
		t.Errorf("int -> int 期望 42，实际 (%d,%v)", v, ok)
	}
	if v, ok := cast(true, false); !ok || v != true {
		t.Errorf("bool -> bool 期望 true，实际 (%v,%v)", v, ok)
	}
	if v, ok := cast(3.14, 0.0); !ok || v != 3.14 {
		t.Errorf("float64 -> float64 期望 3.14，实际 (%v,%v)", v, ok)
	}
}

// TestCast_NumericToNamedBool 验证各整数与浮点类型按零值转换为具名 bool，不因类型断言而 panic。
func TestCast_NumericToNamedBool(t *testing.T) {
	type flag bool
	type count int
	type address uintptr
	tests := []struct {
		name    string
		zero    any
		nonzero any
	}{
		{"int", int(0), int(-1)},
		{"int8", int8(0), int8(-1)},
		{"int16", int16(0), int16(-1)},
		{"int32", int32(0), int32(-1)},
		{"int64", int64(0), int64(-1)},
		{"uint", uint(0), uint(1)},
		{"uint8", uint8(0), uint8(1)},
		{"uint16", uint16(0), uint16(1)},
		{"uint32", uint32(0), uint32(1)},
		{"uint64", uint64(0), uint64(1)},
		{"uintptr", uintptr(0), uintptr(1)},
		{"float32", float32(0), float32(-0.5)},
		{"float64", float64(0), float64(-0.5)},
		{"named_int", count(0), count(-1)},
		{"named_uintptr", address(0), address(1)},
		{"string", "false", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := cast(tt.zero, flag(true)); !ok || got != flag(false) {
				t.Errorf("cast(%T(%v), true) = (%v,%v), want false", tt.zero, tt.zero, got, ok)
			}
			if got, ok := cast(tt.nonzero, flag(false)); !ok || got != flag(true) {
				t.Errorf("cast(%T(%v), false) = (%v,%v), want true", tt.nonzero, tt.nonzero, got, ok)
			}
		})
	}
}

// TestCast_NamedString 验证普通与具名 string 双向转换，保留空值且不扩大到 struct 或数值码点转换。
func TestCast_NamedString(t *testing.T) {
	type label string
	type other string
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"plain_to_named", castCase("hello", label("fallback"), label("hello"))},
		{"named_to_plain", castCase(label("hello"), "fallback", "hello")},
		{"named_to_other", castCase(label("hello"), other("fallback"), other("hello"))},
		{"same_named", castCase(label("hello"), label("fallback"), label("hello"))},
		{"empty_to_named", castCase("", label("fallback"), label(""))},
		{"named_empty_to_plain", castCase(label(""), "fallback", "")},
		{"named_numeric_to_int", castCase(label("42"), 7, 42)},
		{"int_to_named_rejected", castFails(65, label("fallback"))},
		{"string_to_struct_rejected", castFails("hello", struct{ N int }{7})},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

// TestCast_Uintptr 验证 uintptr 的字符串解析、布尔转换与码点防护，包括平台位宽溢出。
func TestCast_Uintptr(t *testing.T) {
	type address uintptr
	type label string
	maxValue := ^uintptr(0)
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"string", castCase("42", uintptr(7), uintptr(42))},
		{"named_target", castCase("42", address(7), address(42))},
		{"max", castCase(strconv.FormatUint(uint64(maxValue), 10), uintptr(7), maxValue)},
		{"overflow", castFails("18446744073709551616", uintptr(7))},
		{"negative", castFails("-1", uintptr(7))},
		{"invalid", castFails("invalid", uintptr(7))},
		{"zero_to_bool", castCase(uintptr(0), true, false)},
		{"nonzero_to_bool", castCase(uintptr(65), false, true)},
		{"named_source_to_bool", castCase(address(65), false, true)},
		{"string_rejected", castFails(uintptr(65), "fallback")},
		{"named_string_rejected", castFails(address(65), label("fallback"))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

// TestCast_ComplexToBoolRejected 验证复数不参与数值转 bool 特判，零与非零均不可转换。
func TestCast_ComplexToBoolRejected(t *testing.T) {
	type flag bool
	for _, value := range []any{complex64(0), complex64(1 + 2i), complex128(0), complex128(1 + 2i)} {
		t.Run(fmt.Sprintf("%T_%v", value, value), func(t *testing.T) {
			for _, def := range []bool{false, true} {
				if got, ok := cast(value, def); ok || got != def {
					t.Errorf("cast(%v, %v) = (%v,%v), want default", value, def, got, ok)
				}
				if got, ok := cast(value, flag(def)); ok || got != flag(def) {
					t.Errorf("cast(%v, flag(%v)) = (%v,%v), want default", value, flag(def), got, ok)
				}
			}
		})
	}
}

// TestCast_SliceToArrayValueSafety 验证短切片不可转换，足长切片正常转数组及数组指针。
func TestCast_SliceToArrayValueSafety(t *testing.T) {
	tests := []struct {
		name  string
		value []int
		want  [2]int
		valid bool
	}{
		{"nil", nil, [2]int{8, 9}, false},
		{"empty", []int{}, [2]int{8, 9}, false},
		{"short", []int{1}, [2]int{8, 9}, false},
		{"exact", []int{1, 2}, [2]int{1, 2}, true},
		{"long", []int{1, 2, 3}, [2]int{1, 2}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			def := [2]int{8, 9}
			got, ok := cast(tt.value, def)
			if tt.valid {
				if !ok || got != tt.want {
					t.Fatalf("数组转换 = (%v,%v), want (%v,true)", got, ok, tt.want)
				}
			} else if ok || got != def {
				t.Fatalf("不可转换时期望 (%v,false)，实际 (%v,%v)", def, got, ok)
			}

			gotPtr, ok := cast(tt.value, &def)
			if tt.valid {
				if !ok || gotPtr == nil || *gotPtr != tt.want {
					t.Fatalf("数组指针转换 = (%v,%v), want 指向 (%v,true)", gotPtr, ok, tt.want)
				}
			} else if ok || gotPtr != &def {
				t.Fatalf("不可转换时期望返回原默认值指针，实际 (%v,%v)", gotPtr, ok)
			}
		})
	}
}

// TestCast_DurationUnitlessZero 验证仅零及符号零可省略单位，非零数字字符串不可转换。
func TestCast_DurationUnitlessZero(t *testing.T) {
	for _, value := range []string{"0", "+0", "-0"} {
		t.Run(value, castCase(value, 5*time.Second, time.Duration(0)))
	}
	t.Run("nonzero", castFails("10", 5*time.Second))
}

// TestCast_StructToSameUnderlyingStruct 锁定结构体源值的转换路径：
// 直接断言失败后走 reflect.CanConvert，底层类型相同的结构体（含 tag 不同、匿名结构体）会被转换；
// 字段不同的结构体不可转换，返回 (def,false) 并由 Env/Config panic。
func TestCast_StructToSameUnderlyingStruct(t *testing.T) {
	src := castProbeStructA{X: 1, Y: "a"}

	if got, ok := cast(src, castProbeStructB{}); !ok || got != (castProbeStructB{X: 1, Y: "a"}) {
		t.Errorf("同底层结构体期望转换成功，实际 (%+v,%v)", got, ok)
	}
	if got, ok := cast(src, castProbeStructTagged{}); !ok || got != (castProbeStructTagged{X: 1, Y: "a"}) {
		t.Errorf("tag 不同的同底层结构体期望转换成功，实际 (%+v,%v)", got, ok)
	}
	if got, ok := cast(struct {
		X int
		Y string
	}{X: 1, Y: "a"}, castProbeStructB{}); !ok || got != (castProbeStructB{X: 1, Y: "a"}) {
		t.Errorf("匿名结构体期望转换为具名同底层结构体，实际 (%+v,%v)", got, ok)
	}
	if got, ok := cast(src, castProbeStructDiff{X: 9, Z: "def"}); ok || got != (castProbeStructDiff{X: 9, Z: "def"}) {
		t.Errorf("字段不同的结构体期望不可转换，实际 (%+v,%v)", got, ok)
	}
	// Config 不做 JSON 式解码：map[string]any 不能转换为结构体。
	if got, ok := cast(map[string]any{"X": 1}, castProbeStructB{}); ok || got != (castProbeStructB{}) {
		t.Errorf("map 期望不可转换为结构体，实际 (%+v,%v)", got, ok)
	}
}
