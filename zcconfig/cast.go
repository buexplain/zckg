package zcconfig

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"time"
)

// TypeMismatchError 描述一次“配置 key 存在、但值无法转换为目标类型”的读取失败。
//
// Env / Config 在 key 存在且转换失败时 panic 抛出该错误；key 不存在时返回调用方默认值，不会 panic。
// 调用方可用 errors.As 捕获并读取 Key / Expected / Actual 三个诊断字段：
//   - Key：调用方传入的原始 key；
//   - Expected：读取时指定的目标类型 T 的静态类型（目标为接口类型时即该接口类型）；
//   - Actual：存储值的动态类型。
type TypeMismatchError struct {
	Key      string
	Expected reflect.Type
	Actual   reflect.Type
}

// Error 实现 error 接口，消息同时携带 key、期望类型与实际类型。
func (e *TypeMismatchError) Error() string {
	return fmt.Sprintf("zcconfig: key %q 类型不匹配：期望 %v，实际 %v", e.Key, e.Expected, e.Actual)
}

// cast 尝试把任意类型的值 v 转换为 T 类型，返回转换结果与是否转换成功。
// 源值 v 为 nil 时不参与转换，返回 (def, false)，由调用方（Env / Config）视为“无值”返回默认值，
// 因此 nil 不会触发 TypeMismatchError。
//
// 转换优先级：
//  1. 直接类型断言 v.(T)；
//  2. string → time.Duration：time.ParseDuration；
//  3. string → 数值 / bool / 具名 string：strconv 解析或 reflect 转换；
//  4. 数值（含具名）→ bool：C 语言惯例，零值为 false、非零为 true；
//  5. 数值 → string：拒绝转换，避免 Go 中 int → string 的 Unicode 码点语义（65 → "A"）；
//  6. reflect 通用转换：Value.CanConvert（含同底层结构体、切片 → 数组）。
//
// 目标为接口类型时只接受直接类型断言，即值必须实现该接口；接口目标不参与后续任何转换。
// 普通与具名 string 可互转；数值特判涵盖整数（含 uintptr）与浮点，不含 complex。
// 全部规则均不适用时返回 (def, false)。
func cast[T any](v any, def T) (T, bool) {
	if v == nil {
		return def, false
	}

	if val, ok := v.(T); ok {
		return val, true
	}

	rv := reflect.ValueOf(v)
	// 目标类型取 T 的静态类型，而非 def 的动态类型：def 只提供默认值，不参与目标类型判定。
	rt := reflect.TypeFor[T]()
	if rt.Kind() == reflect.Interface {
		// 直接断言已失败，说明 v 未实现该接口；Go 中不存在其他到接口类型的转换路径。
		return def, false
	}

	// 针对 string 源值，尝试用 strconv 或 time.ParseDuration 解析到目标类型
	if rv.Kind() == reflect.String {
		s := rv.String()

		// time.Duration 特殊处理：支持 "10s"、"1h30m" 等格式
		// time.Duration 底层为 int64，需在通用 int 分支之前优先处理
		if rt == reflect.TypeOf(time.Duration(0)) {
			if d, err := time.ParseDuration(s); err == nil {
				return any(d).(T), true
			}
			return def, false
		}

		switch rt.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			if n, err := strconv.ParseInt(s, 10, rt.Bits()); err == nil {
				return reflect.ValueOf(n).Convert(rt).Interface().(T), true
			}
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			if n, err := strconv.ParseUint(s, 10, rt.Bits()); err == nil {
				return reflect.ValueOf(n).Convert(rt).Interface().(T), true
			}
		case reflect.Float32, reflect.Float64:
			// NaN/Inf 字面量可被 ParseFloat 接受且不报错，但 NaN 参与任何比较均为 false，
			// Inf 参与运算易产生意外结果，配置场景应视为非法输入。
			if f, err := strconv.ParseFloat(s, rt.Bits()); err == nil && !math.IsNaN(f) && !math.IsInf(f, 0) {
				return reflect.ValueOf(f).Convert(rt).Interface().(T), true
			}
		case reflect.Bool:
			if b, err := strconv.ParseBool(s); err == nil {
				return reflect.ValueOf(b).Convert(rt).Interface().(T), true
			}
		case reflect.String:
			return rv.Convert(rt).Interface().(T), true
		}
		return def, false
	}

	// 数值类型 -> bool 特判：C 语言惯例，零值为 false，非零为 true。
	// 此分支解决 .env 中 "1"/"0" 被 parseValue 推断为 int 后无法转 bool 的问题。
	if rt.Kind() == reflect.Bool && isNumericKind(rv.Kind()) {
		var isZero bool
		switch rv.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			isZero = rv.Int() == 0
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
			isZero = rv.Uint() == 0
		case reflect.Float32, reflect.Float64:
			isZero = rv.Float() == 0
		}
		return reflect.ValueOf(!isZero).Convert(rt).Interface().(T), true
	}

	// 防护数值类型 -> string 的码点陷阱：
	// Go 中 int -> string 的 ConvertibleTo 判定为 true，语义是 Unicode 码点映射（65 -> "A"），
	// 这不是配置场景的期望行为，应判定为转换失败。
	if rt.Kind() == reflect.String && isNumericKind(rv.Kind()) {
		return def, false
	}

	// 通过 reflect 做通用类型转换。
	// 注意：此处采用 Go 原生类型转换语义，float -> int 向零截断（99.99 -> 99），
	// 大位宽 -> 小位宽（如 32 位平台上 int64 -> int）可能溢出；
	// 默认值类型应与存储类型匹配；CanConvert 同时检查切片转数组所需的值长度。
	if rv.CanConvert(rt) {
		return rv.Convert(rt).Interface().(T), true
	}

	return def, false
}

// castOrPanic 是 Env / Config 共用的取值策略：仅在 key 存在时调用。
// nil 源值视为“无值”，返回默认值 def；转换失败 panic(*TypeMismatchError)，
// 携带 key、目标类型 T 与实际类型，便于启动阶段立即暴露配置错误。
func castOrPanic[T any](key string, v any, def T) T {
	if v == nil {
		return def
	}
	val, ok := cast(v, def)
	if !ok {
		panic(&TypeMismatchError{
			Key:      key,
			Expected: reflect.TypeFor[T](),
			Actual:   reflect.TypeOf(v),
		})
	}
	return val
}

// isNumericKind 识别整数（含 uintptr）与浮点类型，不含 complex。
func isNumericKind(k reflect.Kind) bool {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return true
	}
	return false
}
