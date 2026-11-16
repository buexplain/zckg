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
// A→B 底层类型相同；A→Tagged 仅 struct tag 不同（Go 转换规则比较结构体类型时忽略 tag）；
// A→Diff 字段不同，不可转换。
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

// --- 非 string 源值：数值族安全子集（静态无损才允许） ---

// numericAllowCase 构造一条"允许"的数值转换断言：cast 必须成功，且结果转回源类型等于原值
// （无损性 oracle，不照抄 allowNumericConvert 的规则表）。
func numericAllowCase[S comparable, D comparable](input S, def D, want D) func(t *testing.T) {
	return func(t *testing.T) {
		t.Helper()
		got, ok := cast(any(input), def)
		if !ok {
			t.Fatalf("cast(%T(%v), %T) 期望允许，实际未命中", input, input, def)
		}
		if got != want {
			t.Errorf("cast(%T(%v), %T) 期望 %v，实际 %v", input, input, def, want, got)
		}
		back := reflect.ValueOf(got).Convert(reflect.TypeOf(input)).Interface()
		if !reflect.DeepEqual(back, any(input)) {
			t.Errorf("往返转换丢失信息：%T(%v) -> %T(%v) -> %T(%v)", input, input, def, got, back, back)
		}
	}
}

// TestCast_NumericSafeSubsetAllowed 验证同族加宽/等宽、无符号→更宽有符号、整型→浮点按尾数位的转换，
// 并用"结果转回源类型等于原值"证明允许的转换确实无损。
func TestCast_NumericSafeSubsetAllowed(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"signed_widen", numericAllowCase(int8(-5), int64(0), int64(-5))},
		{"signed_widen_int32", numericAllowCase(int32(42), int64(0), int64(42))},
		{"int16_to_float64", numericAllowCase(int16(3306), 0.0, 3306.0)},
		{"int32_to_float64", numericAllowCase(int32(42), 0.0, 42.0)},
		{"int8_to_float32", numericAllowCase(int8(-5), float32(0), float32(-5))},
		{"uint8_to_int16", numericAllowCase(uint8(255), int16(0), int16(255))},
		{"uint32_to_int64", numericAllowCase(uint32(1<<20), int64(0), int64(1<<20))},
		{"unsigned_widen", numericAllowCase(uint8(255), uint64(0), uint64(255))},
		{"uintptr_to_uint64", numericAllowCase(uintptr(42), uint64(0), uint64(42))},
		{"uint16_to_float32", numericAllowCase(uint16(300), float32(0), float32(300))},
		{"uint32_to_float64", numericAllowCase(uint32(1<<31), 0.0, float64(1<<31))},
		{"float_widen", numericAllowCase(float32(2.5), 0.0, 2.5)},
		{"complex_widen", numericAllowCase(complex64(1+2i), complex128(0), complex128(1+2i))},
		{"duration_to_int64", numericAllowCase(time.Duration(30), int64(0), int64(30))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

// TestCast_NumericSafeSubsetRejected 验证窄化、跨族与浮点→整数一律未命中；
// 每个用例带一个会出问题的反例值（如 int64(200)→int8 会回绕成 -56），证明拒绝是必要的。
func TestCast_NumericSafeSubsetRejected(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"int64_to_int8", castFails(int64(200), int8(0))},
		{"int32_to_int16", castFails(int32(1), int16(0))},
		{"signed_to_unsigned", castFails(-1, uint64(0))},
		{"unsigned_to_signed_equal_width", castFails(uint64(1), int64(0))},
		{"int64_to_float64", castFails(int64(1), 0.0)},
		{"uint64_to_float64", castFails(uint64(1), 0.0)},
		{"int32_to_float32", castFails(int32(1), float32(0))},
		{"float_to_int", castFails(100.0, 0)},
		{"float_to_int8", castFails(1.5, int8(0))},
		{"float_narrow", castFails(3.14, float32(0))},
		{"complex_narrow", castFails(complex128(1+2i), complex64(0))},
		{"complex_to_int", castFails(complex128(1+2i), int64(0))},
		{"int_to_complex", castFails(int32(1), complex128(0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

// TestCast_NumericPlatformWidth 验证 int/uint 的位宽按平台实际值参与安全子集判定：
// 64 位平台 int64→int 等宽可读、int→float64 因 64 位超尾数位被拒；32 位平台反之。
// uint→int 两个平台都拒绝（位宽相等或更小）。
func TestCast_NumericPlatformWidth(t *testing.T) {
	if strconv.IntSize == 64 {
		t.Run("int64_to_int_allow", numericAllowCase(int64(42), 0, 42))
		t.Run("int_to_float64_reject", castFails(42, 0.0))
	} else {
		t.Run("int64_to_int_reject", castFails(int64(42), 0))
		t.Run("int_to_float64_allow", numericAllowCase(42, 0.0, 42.0))
	}
	t.Run("int_to_int64_allow", numericAllowCase(42, int64(0), int64(42)))
	t.Run("uint_to_int_reject", castFails(uint(1), 0))
}

// TestCast_NumericNamedTypes 验证具名数值类型按底层 kind 与位宽参与规则。
func TestCast_NumericNamedTypes(t *testing.T) {
	type Port int
	type Amount float64
	type Code uint8
	t.Run("port_to_int64", numericAllowCase(Port(443), int64(0), int64(443)))
	t.Run("port_narrow_reject", castFails(Port(443), int8(0)))
	t.Run("amount_narrow_reject", castFails(Amount(1.5), float32(0)))
	t.Run("code_to_int16", numericAllowCase(Code(7), int16(0), int16(7)))
	t.Run("code_to_uint32", numericAllowCase(Code(7), uint32(0), uint32(7)))
}

// TestCast_NumericToDurationRejected 验证裸数值不会被当作纳秒解释：
// 目标为 time.Duration 且源为数值时未命中；源为 time.Duration 本身时正常。
func TestCast_NumericToDurationRejected(t *testing.T) {
	tests := []struct {
		name string
		run  func(*testing.T)
	}{
		{"int", castFails(3306, time.Duration(0))},
		{"int64", castFails(int64(30), time.Duration(0))},
		{"uint8", castFails(uint8(30), time.Duration(0))},
		{"float", castFails(1.5, time.Duration(0))},
		{"duration_self", castCase(time.Duration(30), time.Duration(0), time.Duration(30))},
		{"duration_to_int64", castCase(time.Duration(30), int64(0), int64(30))},
	}
	for _, tt := range tests {
		t.Run(tt.name, tt.run)
	}
}

// --- 数值类型 → bool 特判（C 惯例：零值为 false） ---

func TestCast_IntToBool(t *testing.T) {
	// 数值类型 -> bool 特判：C 语言惯例，零值为 false，非零为 true。
	// 该分支服务于 Config 通道（Register 注册的 map 可携带数值）；Env 通道的 "1"/"0" 走 string 解析分支。
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
