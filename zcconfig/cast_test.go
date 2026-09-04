package zcconfig

import (
	"fmt"
	"strconv"
	"testing"
	"time"
)

func TestCast_NilReturnsDefault(t *testing.T) {
	if v := cast[string](nil, "fallback"); v != "fallback" {
		t.Errorf("nil -> string 期望 'fallback'，实际 %q", v)
	}
	if v := cast[int](nil, 42); v != 42 {
		t.Errorf("nil -> int 期望 42，实际 %d", v)
	}
	if v := cast[bool](nil, true); v != true {
		t.Errorf("nil -> bool 期望 true，实际 %v", v)
	}
	if v := cast[float64](nil, 1.5); v != 1.5 {
		t.Errorf("nil -> float64 期望 1.5，实际 %f", v)
	}
}

// TestCast_NilInterfaceDefReturnsNil 验证目标为接口类型且 def 为 nil、断言失败时返回 nil 而非 panic。
// 触发路径：reflect.TypeOf(nil 接口) 返回 nil，若未加守卫会对 nil reflect.Type 调用 Kind() 触发空指针 panic。
func TestCast_NilInterfaceDefReturnsNil(t *testing.T) {
	if v := cast[error]("not-an-error", nil); v != nil {
		t.Errorf("nil 接口默认值 + 断言失败 期望 nil，实际 %v", v)
	}
	if v := cast[fmt.Stringer](42, nil); v != nil {
		t.Errorf("nil 接口默认值 + 断言失败 期望 nil，实际 %v", v)
	}
}

// --- string -> string ---

func TestCast_StringToString(t *testing.T) {
	if v := cast("hello", "fallback"); v != "hello" {
		t.Errorf("期望 'hello'，实际 %q", v)
	}
	if v := cast("", "fallback"); v != "" {
		t.Errorf("空字符串期望 ''，实际 %q", v)
	}
	if v := cast("中文内容", "fallback"); v != "中文内容" {
		t.Errorf("期望 '中文内容'，实际 %q", v)
	}
	if v := cast("with spaces", "fallback"); v != "with spaces" {
		t.Errorf("期望 'with spaces'，实际 %q", v)
	}
}

// --- string -> int 系列 ---

func TestCast_StringToInt(t *testing.T) {
	if v := cast("42", 0); v != 42 {
		t.Errorf("期望 42，实际 %d", v)
	}
	if v := cast("-42", 0); v != -42 {
		t.Errorf("期望 -42，实际 %d", v)
	}
	if v := cast("0", 0); v != 0 {
		t.Errorf("期望 0，实际 %d", v)
	}
}

func TestCast_StringToInt64(t *testing.T) {
	if v := cast("9223372036854775807", int64(0)); v != int64(9223372036854775807) {
		t.Errorf("期望 max int64，实际 %d", v)
	}
	if v := cast("-9223372036854775808", int64(0)); v != int64(-9223372036854775808) {
		t.Errorf("期望 min int64，实际 %d", v)
	}
}

func TestCast_StringToInt32(t *testing.T) {
	if v := cast("2147483647", int32(0)); v != int32(2147483647) {
		t.Errorf("期望 max int32，实际 %d", v)
	}
}

func TestCast_StringToInt16(t *testing.T) {
	if v := cast("32767", int16(0)); v != int16(32767) {
		t.Errorf("期望 max int16，实际 %d", v)
	}
}

func TestCast_StringToInt8(t *testing.T) {
	if v := cast("127", int8(0)); v != int8(127) {
		t.Errorf("期望 max int8，实际 %d", v)
	}
}

func TestCast_StringToIntOverflow(t *testing.T) {
	// 超出 int8 范围，应返回默认值
	if v := cast("200", int8(0)); v != int8(0) {
		t.Errorf("超出 int8 范围期望默认值 0，实际 %d", v)
	}
	if v := cast("99999999999", int32(0)); v != int32(0) {
		t.Errorf("超出 int32 范围期望默认值 0，实际 %d", v)
	}
}

func TestCast_StringToIntInvalid(t *testing.T) {
	if v := cast("abc", 0); v != 0 {
		t.Errorf("非数字字符串期望默认值 0，实际 %d", v)
	}
	if v := cast("12.5", 0); v != 0 {
		t.Errorf("浮点字符串转 int 期望默认值 0，实际 %d", v)
	}
	if v := cast("", 0); v != 0 {
		t.Errorf("空字符串转 int 期望默认值 0，实际 %d", v)
	}
	if v := cast("  42", 0); v != 0 {
		t.Errorf("带空格字符串期望默认值 0，实际 %d", v)
	}
}

// --- string -> uint 系列 ---

func TestCast_StringToUint(t *testing.T) {
	if v := cast("42", uint(0)); v != uint(42) {
		t.Errorf("期望 42，实际 %d", v)
	}
	if v := cast("0", uint(0)); v != uint(0) {
		t.Errorf("期望 0，实际 %d", v)
	}
}

func TestCast_StringToUint64(t *testing.T) {
	if v := cast("18446744073709551615", uint64(0)); v != uint64(18446744073709551615) {
		t.Errorf("期望 max uint64，实际 %d", v)
	}
}

func TestCast_StringToUint32(t *testing.T) {
	if v := cast("4294967295", uint32(0)); v != uint32(4294967295) {
		t.Errorf("期望 max uint32，实际 %d", v)
	}
}

func TestCast_StringToUintNegative(t *testing.T) {
	// 负数无法转 uint，应返回默认值
	if v := cast("-1", uint(0)); v != uint(0) {
		t.Errorf("负数转 uint 期望默认值 0，实际 %d", v)
	}
}

func TestCast_StringToUintOverflow(t *testing.T) {
	if v := cast("300", uint8(0)); v != uint8(0) {
		t.Errorf("超出 uint8 范围期望默认值 0，实际 %d", v)
	}
}

// --- string -> float 系列 ---

func TestCast_StringToFloat64(t *testing.T) {
	if v := cast("3.14", 0.0); v != 3.14 {
		t.Errorf("期望 3.14，实际 %f", v)
	}
	if v := cast("-0.5", 0.0); v != -0.5 {
		t.Errorf("期望 -0.5，实际 %f", v)
	}
	if v := cast("0", 0.0); v != 0.0 {
		t.Errorf("期望 0.0，实际 %f", v)
	}
	if v := cast("100", 0.0); v != 100.0 {
		t.Errorf("整数字符串转 float64 期望 100.0，实际 %f", v)
	}
}

func TestCast_StringToFloat32(t *testing.T) {
	if v := cast("1.5", float32(0)); v != float32(1.5) {
		t.Errorf("期望 1.5，实际 %f", v)
	}
}

func TestCast_StringToFloatInvalid(t *testing.T) {
	if v := cast("abc", 0.0); v != 0.0 {
		t.Errorf("非数字字符串转 float64 期望默认值 0.0，实际 %f", v)
	}
	if v := cast("", 0.0); v != 0.0 {
		t.Errorf("空字符串转 float64 期望默认值 0.0，实际 %f", v)
	}
}

func TestCast_StringToFloatNaNInf(t *testing.T) {
	// NaN/Inf 字面量可被 ParseFloat 接受，但配置场景应视为非法输入返回 def
	if v := cast("NaN", 0.0); v != 0.0 {
		t.Errorf("'NaN' 转 float64 期望默认值 0.0，实际 %f", v)
	}
	if v := cast("Inf", 1.5); v != 1.5 {
		t.Errorf("'Inf' 转 float64 期望默认值 1.5，实际 %f", v)
	}
	if v := cast("-Inf", 2.5); v != 2.5 {
		t.Errorf("'-Inf' 转 float64 期望默认值 2.5，实际 %f", v)
	}
	if v := cast("NaN", float32(0)); v != float32(0) {
		t.Errorf("'NaN' 转 float32 期望默认值 0，实际 %f", v)
	}
	// 溢出为 Inf 的指数（ParseFloat 返回 +Inf 且带 ErrRange 错误）同样返回 def
	if v := cast("1e999", 3.5); v != 3.5 {
		t.Errorf("'1e999' 转 float64 期望默认值 3.5，实际 %f", v)
	}
}

// --- string -> bool ---

func TestCast_StringToBool(t *testing.T) {
	validTrue := []string{"1", "t", "T", "true", "TRUE", "True"}
	for _, s := range validTrue {
		if v := cast(s, false); v != true {
			t.Errorf("字符串 %q 转 bool 期望 true，实际 %v", s, v)
		}
	}
	validFalse := []string{"0", "f", "F", "false", "FALSE", "False"}
	for _, s := range validFalse {
		if v := cast(s, true); v != false {
			t.Errorf("字符串 %q 转 bool 期望 false，实际 %v", s, v)
		}
	}
}

func TestCast_StringToBoolInvalid(t *testing.T) {
	if v := cast("yes", false); v != false {
		t.Errorf("无效布尔字符串 'yes' 期望默认值 false，实际 %v", v)
	}
	if v := cast("", true); v != true {
		t.Errorf("空字符串转 bool 期望默认值 true，实际 %v", v)
	}
	if v := cast("2", false); v != false {
		t.Errorf("'2' 转 bool 期望默认值 false，实际 %v", v)
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
		if v := cast(c.s, time.Duration(0)); v != c.exp {
			t.Errorf("%q 转 Duration 期望 %v，实际 %v", c.s, c.exp, v)
		}
	}
}

func TestCast_StringToDurationInvalid(t *testing.T) {
	def := 5 * time.Second
	if v := cast("abc", def); v != def {
		t.Errorf("无效字符串转 Duration 期望默认值 %v，实际 %v", def, v)
	}
	if v := cast("", def); v != def {
		t.Errorf("空字符串转 Duration 期望默认值 %v，实际 %v", def, v)
	}
	if v := cast("10", def); v != def {
		t.Errorf("无单位字符串 '10' 转 Duration 期望默认值 %v，实际 %v", def, v)
	}
}

// --- string -> 非数值类型（走 default 分支） ---

func TestCast_StringToStructReturnsDefault(t *testing.T) {
	type MyStruct struct{ X int }
	def := MyStruct{X: 99}
	if v := cast("hello", def); v != def {
		t.Errorf("string 转 struct 期望默认值，实际 %v", v)
	}
}

// --- 非 string 源值，走 reflect ConvertibleTo 路径 ---

func TestCast_IntToFloat64(t *testing.T) {
	if v := cast(42, 0.0); v != 42.0 {
		t.Errorf("int -> float64 期望 42.0，实际 %f", v)
	}
}

func TestCast_Int64ToInt(t *testing.T) {
	if v := cast(int64(100), 0); v != 100 {
		t.Errorf("int64 -> int 期望 100，实际 %d", v)
	}
}

func TestCast_Float64ToFloat32(t *testing.T) {
	if v := cast(3.14, float32(0)); v != float32(3.14) {
		t.Errorf("float64 -> float32 期望 %f，实际 %f", float32(3.14), v)
	}
}

// ZCC-02：reflect 兜底采用 Go 原生类型转换语义，float -> int 向零截断。
// 该截断语义已在 docs/zcconfig.md 显式声明，本测试固化截断行为：
// 典型触发链为 .env 中 PRICE=99.99 被推断为 float64，调用方 Env("PRICE", 0) 得到 99。
func TestCast_Float64ToIntTruncation(t *testing.T) {
	if v := cast(99.99, 0); v != 99 {
		t.Errorf("float64(99.99) -> int 期望向零截断为 99，实际 %d", v)
	}
	if v := cast(-99.99, 0); v != -99 {
		t.Errorf("float64(-99.99) -> int 期望向零截断为 -99，实际 %d", v)
	}
	if v := cast(100.0, 0); v != 100 {
		t.Errorf("float64(100.0) -> int 期望 100，实际 %d", v)
	}
	if v := cast(1.5, int8(0)); v != int8(1) {
		t.Errorf("float64(1.5) -> int8 期望向零截断为 1，实际 %d", v)
	}
}

func TestCast_IntToBool(t *testing.T) {
	// 数值类型 -> bool 特判：C 语言惯例，零值为 false，非零为 true。
	// 此特判解决 .env 中 "1"/"0" 被 parseValue 推断为 int 后无法转 bool 的问题。
	if v := cast(1, false); v != true {
		t.Errorf("int(1) -> bool 期望 true，实际 %v", v)
	}
	if v := cast(0, false); v != false {
		t.Errorf("int(0) -> bool 期望 false，实际 %v", v)
	}
	if v := cast(int64(42), false); v != true {
		t.Errorf("int64(42) -> bool 期望 true，实际 %v", v)
	}
	if v := cast(0, true); v != false {
		t.Errorf("int(0) -> bool 期望 false，实际 %v", v)
	}
	if v := cast(3.14, false); v != true {
		t.Errorf("float64(3.14) -> bool 期望 true，实际 %v", v)
	}
	if v := cast(0.0, true); v != false {
		t.Errorf("float64(0) -> bool 期望 false，实际 %v", v)
	}
}

func TestCast_UintToBool(t *testing.T) {
	// uint 系列 -> bool 走同一 C 惯例分支（cast.go 的 uint 分支）。
	if v := cast(uint(1), false); v != true {
		t.Errorf("uint(1) -> bool 期望 true，实际 %v", v)
	}
	if v := cast(uint(0), true); v != false {
		t.Errorf("uint(0) -> bool 期望 false，实际 %v", v)
	}
	if v := cast(uint64(42), false); v != true {
		t.Errorf("uint64(42) -> bool 期望 true，实际 %v", v)
	}
	if v := cast(uint32(0), true); v != false {
		t.Errorf("uint32(0) -> bool 期望 false，实际 %v", v)
	}
}

func TestCast_IntToStringReturnsDefault(t *testing.T) {
	// 数值类型 -> string 是 Unicode 码点语义（65 -> "A"），不是数字转字符串，应返回 def
	if v := cast(65, "fallback"); v != "fallback" {
		t.Errorf("int -> string 应返回默认值，实际 %q（码点陷阱）", v)
	}
	if v := cast(int64(100), "def"); v != "def" {
		t.Errorf("int64 -> string 应返回默认值，实际 %q", v)
	}
	if v := cast(uint(42), ""); v != "" {
		t.Errorf("uint -> string 应返回默认值，实际 %q", v)
	}
	if v := cast(3.14, "def"); v != "def" {
		t.Errorf("float64 -> string 应返回默认值，实际 %q", v)
	}
}

func TestParseValue_LargeInteger(t *testing.T) {
	// 超大整数应保留为 string，不应降级为 float64
	v := parseValue("99999999999999999999")
	if _, ok := v.(string); !ok {
		t.Errorf("超大整数期望保留为 string，实际类型 %T", v)
	}
	// 带小数点或指数的值仍应解析为 float64
	if f := parseValue("1.5"); f != 1.5 {
		t.Errorf("parseValue(\"1.5\") 期望 1.5，实际 %v", f)
	}
	if f := parseValue("1e100"); f != 1e100 {
		t.Errorf("parseValue(\"1e100\") 期望 1e100，实际 %v", f)
	}
}

// TestParseValue_IntBoundary 验证整型推断按平台 int 位宽（strconv.IntSize）解析：
// 平台范围内的最大值解析为 int；超出范围的值保持 string，不静默截断。
// 测试按 strconv.IntSize 动态取边界，在 32 位/64 位平台上均可通过。
func TestParseValue_IntBoundary(t *testing.T) {
	var maxIntStr, overflowStr string
	if strconv.IntSize == 64 {
		maxIntStr = "9223372036854775807"
		overflowStr = "9223372036854775808"
	} else {
		maxIntStr = "2147483647"
		overflowStr = "2147483648"
	}

	// 平台 int 最大值：解析为 int 且无截断
	v := parseValue(maxIntStr)
	n, ok := v.(int)
	if !ok {
		t.Fatalf("parseValue(%q) 期望解析为 int，实际类型 %T", maxIntStr, v)
	}
	if strconv.FormatInt(int64(n), 10) != maxIntStr {
		t.Errorf("parseValue(%q) 期望 %s，实际 %d（截断）", maxIntStr, maxIntStr, n)
	}

	// 超出平台 int 范围：保持 string（32 位平台不截断为负值/零值）
	v = parseValue(overflowStr)
	if _, ok := v.(string); !ok {
		t.Errorf("parseValue(%q) 期望保持 string，实际类型 %T（发生了截断）", overflowStr, v)
	}
}

func TestCast_BoolToIntNotConvertible(t *testing.T) {
	// bool 不可转换为 int，应返回默认值
	if v := cast(true, 0); v != 0 {
		t.Errorf("bool -> int 不可转换，期望默认值 0，实际 %d", v)
	}
}

// --- 直接类型断言命中 ---

func TestCast_DirectAssertion(t *testing.T) {
	if v := cast(42, 0); v != 42 {
		t.Errorf("int -> int 期望 42，实际 %d", v)
	}
	if v := cast(true, false); v != true {
		t.Errorf("bool -> bool 期望 true，实际 %v", v)
	}
	if v := cast(3.14, 0.0); v != 3.14 {
		t.Errorf("float64 -> float64 期望 3.14，实际 %f", v)
	}
}
