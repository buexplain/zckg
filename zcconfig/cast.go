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
// 转换优先级（编号与 docs/zcconfig.md 的「转换优先级」一致）：
//  1. v == nil → 未命中（由调用方视为“无值”返回 def）；
//  2. 直接类型断言 v.(T)；
//  3. string → time.Duration：time.ParseDuration；
//  4. string → 数值 / bool / 具名 string：strconv 解析或 reflect 转换；
//  5. 数值（含具名）→ bool：C 语言惯例，零值为 false、非零为 true；
//  6. 数值 → string：拒绝转换，避免 Go 中 int → string 的 Unicode 码点语义（65 → "A"）；
//  7. 数值 → time.Duration：拒绝转换，避免裸数值被当作纳秒解释（3306 → 3.306µs）；
//  8. 数值族之间：只允许静态无损子集（同族加宽、无符号→更宽有符号、整型→浮点按尾数位），
//     跨族与窄化一律拒绝，避免 reflect 原生语义静默截断、回绕或丢精度；
//  9. reflect 通用转换：Value.CanConvert（含同底层结构体、切片 → 数组）；
//  10. 全部失败 → 未命中。
//
// 目标为接口类型时只接受直接类型断言，即值必须实现该接口；接口目标不参与后续任何转换。
// 普通与具名 string 可互转；数值特判涵盖整数（含 uintptr）与浮点，不含 complex；
// complex 仅参与第 8 步的同族加宽。
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
	// 该分支服务于 Config 通道：Register 注册的 map 可携带 int/float64 等数值，调用方以 bool 读取时按 C 惯例转换。
	// Env 通道存储的是文本，数值字面量走上面的 string 解析分支（"1"/"0" 由 ParseBool 处理）。
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

	// 目标为 time.Duration 且源为数值：拒绝转换。
	// time.Duration 底层是 int64，若放行数值转换，注册的裸数值会被当作纳秒解释（3306 -> 3.306µs），
	// 单位语义在转换中丢失；需要时长语义时应注册 time.Duration 本身或 "30s" 这类带单位文本。
	if rt == reflect.TypeFor[time.Duration]() && isNumericKind(rv.Kind()) {
		return def, false
	}

	// 数值族之间只允许静态无损子集：同族加宽/等宽、无符号→更宽有符号、整型→浮点按尾数位。
	// 其余组合（窄化、有符号→无符号、浮点→整数、整数→complex、complex 跨族）一律未命中，
	// 避免 reflect 兜底按 Go 原生转换语义静默截断、回绕或丢精度。
	srcNumeric, dstNumeric := isNumericFamilyKind(rv.Kind()), isNumericFamilyKind(rt.Kind())
	if srcNumeric && dstNumeric && !allowNumericConvert(rv.Type(), rt) {
		return def, false
	}

	// 通过 reflect 做通用类型转换。
	// 走到这里只剩非数值转换（结构体同底层、切片→数组等）与已通过安全子集的数值转换；
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

// 数值类型族：族内按位宽加宽无损，跨族转换无法对任意取值保证无损。
const (
	numericFamilyNone = iota
	numericFamilySigned
	numericFamilyUnsigned
	numericFamilyFloat
	numericFamilyComplex
)

// isNumericFamilyKind 判断 kind 是否属于四个数值族之一（有符号整数、无符号整数、浮点、复数）。
func isNumericFamilyKind(k reflect.Kind) bool {
	_, ok := numericFamily(k)
	return ok
}

// numericFamily 把数值 kind 归入类型族；非数值 kind 返回 (numericFamilyNone, false)。
func numericFamily(k reflect.Kind) (int, bool) {
	switch k {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return numericFamilySigned, true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return numericFamilyUnsigned, true
	case reflect.Float32, reflect.Float64:
		return numericFamilyFloat, true
	case reflect.Complex64, reflect.Complex128:
		return numericFamilyComplex, true
	}
	return numericFamilyNone, false
}

// allowNumericConvert 判断数值族之间的转换是否对源类型的任意取值都无损：
//  1. 同族且目标位宽 ≥ 源位宽（具名类型按底层位宽，含 complex 同族加宽）；
//  2. 无符号 → 有符号且目标位宽严格大于源位宽（无符号最大值需放进有符号正半区）；
//  3. 整数 → 浮点且源整型位宽 ≤ 目标尾数位（float32 24 位、float64 53 位）；
//  4. 其余组合（窄化、有符号→无符号、浮点→整数、跨族涉及 complex）均返回 false。
//
// 调用点须保证 src/dst 都是数值类型（isNumericFamilyKind 双重判断），
// 因此直接使用 reflect.Type.Bits 取位宽。int/uint/uintptr 的位宽取平台实际值：
// int64→int 在 64 位平台成立、32 位平台不成立；int→float64 在 64 位平台不成立、32 位平台成立。
func allowNumericConvert(src, dst reflect.Type) bool {
	famSrc, _ := numericFamily(src.Kind())
	famDst, _ := numericFamily(dst.Kind())
	srcBits, dstBits := src.Bits(), dst.Bits()

	if famSrc == famDst {
		// 同族：加宽或等宽无损，窄化拒绝
		return dstBits >= srcBits
	}
	if famSrc == numericFamilyUnsigned && famDst == numericFamilySigned {
		// 无符号 → 有符号：目标必须严格更宽，否则最大值放不进正半区
		return dstBits > srcBits
	}
	if (famSrc == numericFamilySigned || famSrc == numericFamilyUnsigned) && famDst == numericFamilyFloat {
		// 整数 → 浮点：源整型位宽不得超过目标尾数位，超过即可能丢精度
		mantissa := 53
		if dst.Kind() == reflect.Float32 {
			mantissa = 24
		}
		return srcBits <= mantissa
	}
	// 剩余组合：有符号→无符号（可能为负）、浮点→整数（可能带小数）、任何跨族涉及 complex
	return false
}
