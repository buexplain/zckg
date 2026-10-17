// 本文件为 nullSafeField.Scan 的转换矩阵 fuzz：
// fuzzer 选择目标字段类型与源值类型（nil/[]byte/string/整数/浮点/bool/time.Time），
// 参考模型独立实现文档中的转换规则，校验成功值、ErrScanConvert、NULL 与指针语义。

package zcdb

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strconv"
	"testing"
	"time"
)

type fuzzScanNamedString string
type fuzzScanNamedBool bool
type fuzzScanNamedBytes []byte
type fuzzScanJSONValue struct {
	A int    `json:"a"`
	B string `json:"b"`
}

var fuzzScanTargetTypes = []reflect.Type{
	reflect.TypeOf(""),
	reflect.TypeOf(fuzzScanNamedString("")),
	reflect.TypeOf([]byte(nil)),
	reflect.TypeOf(fuzzScanNamedBytes(nil)),
	reflect.TypeOf(json.RawMessage(nil)),
	reflect.TypeOf(int(0)),
	reflect.TypeOf(int8(0)),
	reflect.TypeOf(int16(0)),
	reflect.TypeOf(int32(0)),
	reflect.TypeOf(int64(0)),
	reflect.TypeOf(uint(0)),
	reflect.TypeOf(uint8(0)),
	reflect.TypeOf(uint16(0)),
	reflect.TypeOf(uint32(0)),
	reflect.TypeOf(uint64(0)),
	reflect.TypeOf(float32(0)),
	reflect.TypeOf(float64(0)),
	reflect.TypeOf(false),
	reflect.TypeOf(fuzzScanNamedBool(false)),
	reflect.TypeOf(time.Time{}),
	reflect.TypeOf((*string)(nil)),
	reflect.TypeOf((*int64)(nil)),
	reflect.TypeOf((*bool)(nil)),
	reflect.TypeOf((*time.Time)(nil)),
	reflect.TypeOf([]int(nil)),
	reflect.TypeOf([]string(nil)),
	reflect.TypeOf(map[string]any(nil)),
	reflect.TypeOf(map[string]string(nil)),
	reflect.TypeOf(fuzzScanJSONValue{}),
	reflect.TypeOf((*[]int)(nil)),
	reflect.TypeOf((*map[string]any)(nil)),
	reflect.TypeOf((*fuzzScanJSONValue)(nil)),
}

// FuzzNullSafeFieldScan 覆盖 nullSafeField.Scan 的目标类型 × 源类型矩阵：
// 0 nil、1 []byte、2 string、3 int64、4 uint64、5 float64、6 bool、7 time.Time。
// 普通 go test 只运行种子；探索命令：
//
//	go test ./zcdb -run '^$' -fuzz '^FuzzNullSafeFieldScan$' -fuzztime=30s
func FuzzNullSafeFieldScan(f *testing.F) {
	type seedValue struct {
		kind uint8
		text string
		i    int64
		u    uint64
		f    float64
		b    bool
	}
	seedValues := []seedValue{
		{0, "", 0, 0, 0, false},
		{1, "123", 0, 0, 0, false},
		{1, `{"a":1,"b":"x"}`, 0, 0, 0, false},
		{1, `[1,2]`, 0, 0, 0, false},
		{1, "not-json", 0, 0, 0, false},
		{2, "2026-10-17T12:00:00Z", 0, 0, 0, false},
		{3, "", 300, 0, 0, false},
		{4, "", 0, 5, 0, false},
		{5, "", 0, 0, 1.5, false},
		{6, "", 0, 0, 0, true},
		{7, "", 1700000000, 0, 0, false},
		// 字符串浮点特殊值：NaN 不能直接用 == / DeepEqual 比较，必须走 NaN-aware 断言。
		{2, "NaN", 0, 0, 0, false},
		{2, "Inf", 0, 0, 0, false},
		{2, "-Inf", 0, 0, 0, false},
	}
	for targetIndex := range fuzzScanTargetTypes {
		for _, seed := range seedValues {
			f.Add(uint8(targetIndex), seed.kind, seed.text, seed.i, seed.u, seed.f, seed.b)
		}
	}

	f.Fuzz(func(t *testing.T, targetIndex, sourceKind uint8, text string, i int64, u uint64, fv float64, b bool) {
		if len(text) > 4<<10 {
			return
		}
		targetType := fuzzScanTargetTypes[int(targetIndex)%len(fuzzScanTargetTypes)]

		var src any
		switch sourceKind % 8 {
		case 0:
			src = nil
		case 1:
			src = []byte(text)
		case 2:
			src = text
		case 3:
			src = i
		case 4:
			src = u
		case 5:
			src = fv
		case 6:
			src = b
		default:
			src = time.Unix(i, 0).UTC()
		}

		field := reflect.New(targetType).Elem()
		err := (&nullSafeField{field: field}).Scan(src)
		want, wantErr := fuzzScanExpected(src, targetType)
		if (err != nil) != (wantErr != nil) {
			t.Fatalf("target=%v src=%T(%v): err=%v, want %v", targetType, src, src, err, wantErr)
		}
		if wantErr != nil {
			if !errors.Is(err, ErrScanConvert) {
				t.Fatalf("target=%v src=%T: err=%v, want errors.Is(ErrScanConvert)", targetType, src, err)
			}
			return
		}
		if !fuzzScanValuesEqual(field, want) {
			t.Fatalf("target=%v src=%T(%v): got %#v, want %#v", targetType, src, src, field.Interface(), want.Interface())
		}
	})
}

func fuzzScanExpected(src any, targetType reflect.Type) (reflect.Value, error) {
	zero := reflect.Zero(targetType)
	if src == nil {
		return zero, nil
	}
	isPtr := targetType.Kind() == reflect.Ptr
	elemType := targetType
	if isPtr {
		elemType = targetType.Elem()
	}
	value, err := fuzzScanConvert(src, elemType)
	if err != nil {
		return zero, err
	}
	if isPtr {
		ptr := reflect.New(elemType)
		ptr.Elem().Set(value)
		return ptr, nil
	}
	return value, nil
}

func fuzzScanConvert(src any, target reflect.Type) (reflect.Value, error) {
	zero := reflect.Zero(target)
	switch value := src.(type) {
	case []byte:
		return fuzzScanConvertBytes(value, target)
	case string:
		return fuzzScanConvertBytes([]byte(value), target)
	case time.Time:
		if target.Kind() == reflect.String {
			return reflect.ValueOf(value.Format(time.RFC3339)).Convert(target), nil
		}
		if reflect.TypeOf(value).ConvertibleTo(target) {
			return reflect.ValueOf(value).Convert(target), nil
		}
		return zero, ErrScanConvert
	}

	value := reflect.ValueOf(src)
	if target.Kind() == reflect.String {
		switch value.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return reflect.ValueOf(strconv.FormatInt(value.Int(), 10)).Convert(target), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return reflect.ValueOf(strconv.FormatUint(value.Uint(), 10)).Convert(target), nil
		case reflect.Float32, reflect.Float64:
			return reflect.ValueOf(strconv.FormatFloat(value.Float(), 'g', -1, 64)).Convert(target), nil
		}
	}
	if target.Kind() == reflect.Bool {
		switch value.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return reflect.ValueOf(value.Int() != 0).Convert(target), nil
		case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			return reflect.ValueOf(value.Uint() != 0).Convert(target), nil
		case reflect.Float32, reflect.Float64:
			return reflect.ValueOf(value.Float() != 0).Convert(target), nil
		}
	}
	if value.Type().ConvertibleTo(target) {
		return value.Convert(target), nil
	}
	return zero, ErrScanConvert
}

func fuzzScanConvertBytes(value []byte, target reflect.Type) (reflect.Value, error) {
	zero := reflect.Zero(target)
	switch target.Kind() {
	case reflect.String:
		return reflect.ValueOf(string(value)).Convert(target), nil
	case reflect.Slice:
		if target.Elem().Kind() == reflect.Uint8 {
			dst := reflect.MakeSlice(target, len(value), len(value))
			reflect.Copy(dst, reflect.ValueOf(value))
			return dst, nil
		}
		return fuzzScanUnmarshalJSON(value, target)
	case reflect.Map:
		if target.Key().Kind() != reflect.String {
			return zero, ErrScanConvert
		}
		return fuzzScanUnmarshalJSON(value, target)
	case reflect.Struct:
		return fuzzScanUnmarshalJSON(value, target)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(string(value), 10, target.Bits())
		if err != nil {
			return zero, ErrScanConvert
		}
		return reflect.ValueOf(parsed).Convert(target), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(string(value), 10, target.Bits())
		if err != nil {
			return zero, ErrScanConvert
		}
		return reflect.ValueOf(parsed).Convert(target), nil
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return zero, ErrScanConvert
		}
		return reflect.ValueOf(parsed).Convert(target), nil
	case reflect.Bool:
		parsed, err := strconv.ParseBool(string(value))
		if err != nil {
			return zero, ErrScanConvert
		}
		return reflect.ValueOf(parsed).Convert(target), nil
	default:
		return zero, ErrScanConvert
	}
}

func fuzzScanUnmarshalJSON(value []byte, target reflect.Type) (reflect.Value, error) {
	dst := reflect.New(target)
	if err := json.Unmarshal(value, dst.Interface()); err != nil {
		return reflect.Zero(target), ErrScanConvert
	}
	return dst.Elem(), nil
}

// fuzzScanValuesEqual 比较实现与参考模型的转换结果。NaN 与自身不相等，
// 直接使用 reflect.DeepEqual 会把正确的 NaN 转换误报为失败；目标类型矩阵中
// 只有浮点标量（含未来可能新增的浮点指针）会产生 NaN，因此单独处理。
func fuzzScanValuesEqual(got, want reflect.Value) bool {
	if got.Kind() == reflect.Ptr || want.Kind() == reflect.Ptr {
		if got.Kind() != want.Kind() || got.IsNil() != want.IsNil() {
			return false
		}
		if got.IsNil() {
			return true
		}
		return fuzzScanValuesEqual(got.Elem(), want.Elem())
	}
	if got.Kind() == reflect.Float32 || got.Kind() == reflect.Float64 {
		g, w := got.Float(), want.Float()
		return g == w || math.IsNaN(g) && math.IsNaN(w)
	}
	return reflect.DeepEqual(got.Interface(), want.Interface())
}
