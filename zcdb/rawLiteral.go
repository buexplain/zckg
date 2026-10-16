package zcdb

import (
	"database/sql/driver"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// rawLiteralStyle 汇总方言之间「参数值 → SQL 字面量」的差异。
// 三方言共用单引号包裹字符串、以两个连续单引号转义字面单引号；差别在于 bool 字面量、
// 时间与字节的写法，以及 MySQL 默认开启反斜杠转义（PostgreSQL 默认
// standard_conforming_strings=on、SQLite 均把反斜杠当普通字符）。
type rawLiteralStyle struct {
	escapeBackslash bool
	trueLiteral     string
	falseLiteral    string
	// formatTime 把时间值格式化为可内联的挂钟时间文本（不做时区换算，保留值自身的偏移）。
	formatTime func(time.Time) string
	// formatBytes 把字节值格式化为方言字节字面量。
	formatBytes func([]byte) string
}

var (
	mysqlRawLiteralStyle = rawLiteralStyle{
		escapeBackslash: true,
		trueLiteral:     "1",
		falseLiteral:    "0",
		formatTime:      func(t time.Time) string { return t.Format("2006-01-02 15:04:05.999999") },
		formatBytes:     func(b []byte) string { return fmt.Sprintf("X'%X'", b) },
	}
	sqliteRawLiteralStyle = rawLiteralStyle{
		escapeBackslash: false,
		trueLiteral:     "1",
		falseLiteral:    "0",
		formatTime:      func(t time.Time) string { return t.Format("2006-01-02 15:04:05.999999") },
		formatBytes:     func(b []byte) string { return fmt.Sprintf("X'%X'", b) },
	}
	pgRawLiteralStyle = rawLiteralStyle{
		escapeBackslash: false,
		trueLiteral:     "TRUE",
		falseLiteral:    "FALSE",
		formatTime:      func(t time.Time) string { return t.Format("2006-01-02 15:04:05.999999-07:00") },
		formatBytes:     func(b []byte) string { return fmt.Sprintf(`'\x%x'::bytea`, b) },
	}
)

// inlineRawLiteral 把第 position 个绑定参数（从 1 起）内联为方言 SQL 字面量。
// Expression 原样内嵌（安全责任归调用方）；nil 接口、nil 指针与 nil 字节切片写成 NULL；
// driver.Valuer 先取 Value() 再按返回的 driver.Value 处理（不接受嵌套 Valuer）；
// 其余按具体类型：字符串、数值、bool、time.Time、[]byte，以及它们的命名类型。
// 类型不支持、值无字面量表示（NaN/Inf）或 Valuer 取值失败时返回 ErrRawValueNotInlinable，
// 错误消息携带参数位置与原因。
func inlineRawLiteral(arg any, style rawLiteralStyle, position int) (string, error) {
	literal, err := inlineRawLiteralValue(arg, style)
	if err != nil {
		return "", fmt.Errorf("%w: arg %d: %v", ErrRawValueNotInlinable, position, err)
	}
	return literal, nil
}

// inlineRawLiteralValue 按值形态内联，返回不含错误哨兵与参数位置的描述性错误。
func inlineRawLiteralValue(arg any, style rawLiteralStyle) (string, error) {
	if expr, ok := arg.(Expression); ok {
		return expr.Value(), nil
	}
	if arg == nil {
		return "NULL", nil
	}
	rv := reflect.ValueOf(arg)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface:
		// 先于 Valuer 断言判空：类型化 nil 指针满足 driver.Valuer 接口，
		// 直接调用 Value() 会解引用空指针。
		if rv.IsNil() {
			return "NULL", nil
		}
		return "", fmt.Errorf("unsupported type %T", arg)
	}
	if valuer, ok := arg.(driver.Valuer); ok {
		value, err := valuer.Value()
		if err != nil {
			return "", fmt.Errorf("%T.Value() failed: %v", arg, err)
		}
		// Value() 依契约只返回 driver.Value（nil/整数/浮点/bool/[]byte/string/time.Time），
		// 故按值处理，不再接受嵌套 Valuer，避免契约被破坏时无限递归。
		return inlineRawValue(value, style)
	}
	return inlineRawValue(arg, style)
}

// inlineRawValue 处理已确定为 driver.Value 或内置标量的值，涵盖其命名类型。
// 这里不再判指针/接口：入口 inlineRawLiteralValue 已把 nil 指针写成 NULL、非 nil 指针直接报错，
// 而 driver.Valuer 分支只会送来 driver.Value（不含指针），故 Ptr/Interface 在此不可达；
// 万一契约被破坏，下落到末尾的 unsupported type 报错。
func inlineRawValue(value any, style rawLiteralStyle) (string, error) {
	if value == nil {
		return "NULL", nil
	}
	switch v := value.(type) {
	case string:
		return quoteRawString(v, style.escapeBackslash), nil
	case []byte:
		if v == nil {
			return "NULL", nil
		}
		return style.formatBytes(v), nil
	case time.Time:
		return "'" + style.formatTime(v) + "'", nil
	}

	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Bool:
		if rv.Bool() {
			return style.trueLiteral, nil
		}
		return style.falseLiteral, nil
	case reflect.String:
		return quoteRawString(rv.String(), style.escapeBackslash), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return strconv.FormatUint(rv.Uint(), 10), nil
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return "", fmt.Errorf("%v has no SQL literal form", f)
		}
		bits := 64
		if rv.Kind() == reflect.Float32 {
			bits = 32
		}
		return strconv.FormatFloat(f, 'g', -1, bits), nil
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			if rv.IsNil() {
				return "NULL", nil
			}
			return style.formatBytes(rv.Bytes()), nil
		}
	}
	return "", fmt.Errorf("unsupported type %T", value)
}

// quoteRawString 以单引号包裹字符串字面量：字面单引号一律写成两个连续单引号；
// escapeBackslash 为 true（MySQL）时按默认转义模式处理反斜杠、NUL 与换行等控制字符。
func quoteRawString(s string, escapeBackslash bool) string {
	var buf strings.Builder
	buf.Grow(len(s) + 2)
	buf.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\'' {
			buf.WriteString("''")
			continue
		}
		if !escapeBackslash {
			buf.WriteByte(c)
			continue
		}
		switch c {
		case '\\':
			buf.WriteString(`\\`)
		case 0:
			buf.WriteString(`\0`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case 0x1a:
			buf.WriteString(`\Z`)
		default:
			buf.WriteByte(c)
		}
	}
	buf.WriteByte('\'')
	return buf.String()
}
