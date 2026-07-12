package zcconfig

import (
	"reflect"
	"strconv"
	"time"
)

// cast 将任意类型的值转换为 T 类型。
// 优先使用直接类型断言，若不匹配则尝试通过 reflect 进行类型转换，
// 对于 string 到数值类型的场景使用 strconv 做转换，
// 对于 string 到 time.Duration 的场景使用 time.ParseDuration 做转换，
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
			if f, err := strconv.ParseFloat(s, rt.Bits()); err == nil {
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

	// 通过 reflect 做通用类型转换
	if rv.IsValid() && rv.Type().ConvertibleTo(rt) {
		return rv.Convert(rt).Interface().(T)
	}

	return def
}
