package zcconfig

import (
	"math"
	"reflect"
	"strconv"
	"time"
)

// cast 将任意类型的值转换为 T 类型。
// 优先使用直接类型断言，若不匹配则尝试通过 reflect 进行类型转换，
// 对于 string 到数值类型的场景使用 strconv 做转换，
// 对于 string 到 time.Duration 的场景使用 time.ParseDuration 做转换。
// 此外包含两个特判分支：
//   - 数值 → bool：C 语言惯例，零值为 false、非零为 true
//     （解决 .env 中 "1"/"0" 被推断为 int 后无法转 bool 的问题）；
//   - 数值 → string：直接返回 def，防止 Go 中 int -> string 的
//     Unicode 码点语义（65 -> "A"）陷阱。
//
// 均失败时返回默认值 def。
func cast[T any](v any, def T) T {
	if v == nil {
		return def
	}

	// 直接类型断言，命中则返回
	if val, ok := v.(T); ok {
		return val
	}

	// 通过 reflect 检查是否可转换，例如 int -> float64、int32 -> int 等
	rv := reflect.ValueOf(v)
	rt := reflect.TypeOf(def)

	// 针对 string 源值，尝试用 strconv 或 time.ParseDuration 解析到目标类型
	if rv.Kind() == reflect.String {
		s := rv.String()

		// time.Duration 特殊处理：支持 "10s"、"1h30m" 等格式
		// time.Duration 底层为 int64，需在通用 int 分支之前优先处理
		if rt == reflect.TypeOf(time.Duration(0)) {
			if d, err := time.ParseDuration(s); err == nil {
				return any(d).(T)
			}
			return def
		}

		switch rt.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if n, err := strconv.ParseInt(s, 10, rt.Bits()); err == nil {
				return reflect.ValueOf(n).Convert(rt).Interface().(T)
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if n, err := strconv.ParseUint(s, 10, rt.Bits()); err == nil {
				return reflect.ValueOf(n).Convert(rt).Interface().(T)
			}
		case reflect.Float32, reflect.Float64:
			// NaN/Inf 字面量可被 ParseFloat 接受且不报错，但 NaN 参与任何比较均为 false，
			// Inf 参与运算易产生意外结果，配置场景应视为非法输入返回 def。
			if f, err := strconv.ParseFloat(s, rt.Bits()); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				return reflect.ValueOf(f).Convert(rt).Interface().(T)
			}
		case reflect.Bool:
			if b, err := strconv.ParseBool(s); err == nil {
				return reflect.ValueOf(b).Convert(rt).Interface().(T)
			}
		default:
			return def
		}
	}

	// 数值类型 -> bool 特判：C 语言惯例，零值为 false，非零为 true。
	// 此分支解决 .env 中 "1"/"0" 被 parseValue 推断为 int 后无法转 bool 的问题。
	if rt.Kind() == reflect.Bool && isNumericKind(rv.Kind()) {
		var isZero bool
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			isZero = rv.Int() == 0
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			isZero = rv.Uint() == 0
		case reflect.Float32, reflect.Float64:
			isZero = rv.Float() == 0
		}
		return any(!isZero).(T)
	}

	// 防护数值类型 -> string 的码点陷阱：
	// Go 中 int -> string 的 ConvertibleTo 判定为 true，语义是 Unicode 码点映射（65 -> "A"），
	// 这不是配置场景的期望行为，应返回 def。
	if rt.Kind() == reflect.String && isNumericKind(rv.Kind()) {
		return def
	}

	// 通过 reflect 做通用类型转换。
	// 注意：此处采用 Go 原生类型转换语义，float -> int 向零截断（99.99 -> 99），
	// 大位宽 -> 小位宽（如 32 位平台上 int64 -> int）可能溢出；
	// 该截断语义已在 docs/zcconfig.md 中显式声明，调用方应保证
	// 默认值类型与存储的配置值类型匹配。
	if rv.IsValid() && rv.Type().ConvertibleTo(rt) {
		return rv.Convert(rt).Interface().(T)
	}

	return def
}

// isNumericKind 判断 reflect.Kind 是否为数值类型（int/uint/float 系列）。
func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
