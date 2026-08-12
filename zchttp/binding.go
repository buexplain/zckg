package zchttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// defaultMaxMemory 是解析 multipart/form-data 时内存缓冲上限，超出部分写入临时文件
const defaultMaxMemory = 32 << 20 // 32 MB

// fileHeaderPtrType 用于识别 handler 结构体中的上传文件字段
var fileHeaderPtrType = reflect.TypeOf((*multipart.FileHeader)(nil))

// timeType 用于识别 time.Time 字段
var timeType = reflect.TypeOf(time.Time{})

// defaultTimeLayouts 是自动探测模式下依次尝试的时间字符串格式
var defaultTimeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
	"15:04:05",
}

// bindRequestData 仅执行请求数据绑定（query/body），不做参数校验。
// 默认值已在 ServeHTTP 阶段通过预计算模板深拷贝完成，此处不再重复执行 applyDefaults。
// 用于提前解析 Req 的场景（如路由命中后立即绑定，将结果注入 ctx，
// 后续在 core 层再做校验）。
// meta 为注册阶段预计算的 structMeta，避免请求阶段重复反射解析。
func bindRequestData(r *http.Request, reqPtr reflect.Value, meta structMeta) error {
	switch r.Method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		return bindValues(reqPtr, r.URL.Query(), nil, meta)
	default:
		return bindBody(r, reqPtr, meta)
	}
}

// bindBody 处理携带请求体的方法，依据 Content-Type 选择 JSON、表单或 multipart 绑定。
// meta 为注册阶段预计算的 structMeta，透传给 bindValues。
func bindBody(r *http.Request, reqPtr reflect.Value, meta structMeta) error {
	contentType := r.Header.Get("Content-Type")
	// 去掉 charset 等参数，只保留主类型，如 "application/json; charset=utf-8"
	if idx := strings.IndexByte(contentType, ';'); idx != -1 {
		contentType = contentType[:idx]
	}
	contentType = strings.TrimSpace(contentType)

	switch contentType {
	case "application/json":
		if r.Body == nil {
			return nil
		}
		// 仅当 req 有可绑定字段时才进行 JSON 解码，避免空结构体时 Decode 返回 EOF 报错
		if len(meta.fields) == 0 {
			return nil
		}
		if err := json.NewDecoder(r.Body).Decode(reqPtr.Interface()); err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("empty request body")
			}
			return err
		}
		return nil
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return err
		}
		return bindValues(reqPtr, r.PostForm, nil, meta)
	case "multipart/form-data":
		if err := r.ParseMultipartForm(defaultMaxMemory); err != nil {
			return err
		}
		if r.MultipartForm == nil {
			return nil
		}
		return bindValues(reqPtr, r.MultipartForm.Value, r.MultipartForm.File, meta)
	default:
		// 未知 Content-Type：若有请求体且有可绑定字段则尝试按 JSON 解析，否则不绑定
		if r.Body != nil && len(meta.fields) > 0 {
			if err := json.NewDecoder(r.Body).Decode(reqPtr.Interface()); err != nil {
				if errors.Is(err, io.EOF) {
					return fmt.Errorf("empty request body")
				}
				return err
			}
		}
		return nil
	}
}

// bindValues 将 values（query 或表单字段）与 files（上传文件）按字段标签映射到结构体。
// meta 为注册阶段预计算的 structMeta，直接遍历其 fields 避免请求阶段反射。
func bindValues(reqPtr reflect.Value, values map[string][]string, files map[string][]*multipart.FileHeader, meta structMeta) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	for i := range meta.fields {
		fm := &meta.fields[i]
		if fm.name == "" || fm.name == "-" {
			continue
		}
		fieldValue := fieldByIndex(elem, fm.indices)
		if !fieldValue.CanSet() {
			continue
		}
		// 上传文件字段不从 values 绑定：有文件则设置，随后一律跳过
		if fm.isFile || fm.isFileSlice {
			if files != nil {
				setFileField(fieldValue, files[fm.name])
			}
			continue
		}
		vs, ok := values[fm.name]
		if !ok || len(vs) == 0 {
			continue
		}
		// 单个字段转换失败时跳过，保持尽力绑定（time 标签已预解析）
		_ = setFieldValue(fieldValue, vs, fm.timeFormat, fm.timeLocation)
	}
	return nil
}

// bindPathParams 将捕获的路由路径参数值按注册顺序写入 Req 字段。
// 路径参数覆盖同名 query/body 值，必须在 bindRequestData 之后调用。
// 单个参数转换失败立即返回错误（区别于 bindValues 的尽力绑定策略）；
// 被省略的尾部可选参数无捕获值，跳过绑定以保留模板默认值/零值。
func bindPathParams(reqPtr reflect.Value, params []pathParamBinding, values []string) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	for i := range params {
		if i >= len(values) {
			// 尾部可选参数被省略，剩余绑定均跳过
			break
		}
		fieldValue := fieldByIndex(elem, params[i].indices)
		if !fieldValue.CanSet() {
			continue
		}
		if err := setScalar(fieldValue, values[i], params[i].timeFormat, params[i].timeLocation); err != nil {
			return fmt.Errorf("invalid path parameter value %q: %w", values[i], err)
		}
	}
	return nil
}

// resolveFieldName 解析字段绑定名：优先 form 标签，其次 json 标签，最后使用字段名
func resolveFieldName(field reflect.StructField) string {
	if name := tagName(field.Tag.Get("form")); name != "" {
		return name
	}
	if name := tagName(field.Tag.Get("json")); name != "" {
		return name
	}
	return field.Name
}

// tagName 取标签中逗号前的名称部分，如 "name,omitempty" -> "name"
func tagName(tag string) string {
	if tag == "" {
		return ""
	}
	if idx := strings.IndexByte(tag, ','); idx != -1 {
		tag = tag[:idx]
	}
	return tag
}

// setFileField 处理上传文件字段，支持 *multipart.FileHeader 与 []*multipart.FileHeader
// 返回 true 表示该字段是文件字段（无论是否有文件都视为已处理）
func setFileField(fieldValue reflect.Value, headers []*multipart.FileHeader) bool {
	ft := fieldValue.Type()
	switch {
	case ft == fileHeaderPtrType:
		if len(headers) > 0 {
			fieldValue.Set(reflect.ValueOf(headers[0]))
		}
		return true
	case ft.Kind() == reflect.Slice && ft.Elem() == fileHeaderPtrType:
		if len(headers) > 0 {
			fieldValue.Set(reflect.ValueOf(headers))
		}
		return true
	}
	return false
}

// setFieldValue 将字符串值列表写入字段：切片字段绑定全部值，标量字段绑定第一个值
func setFieldValue(fieldValue reflect.Value, values []string, timeFormat string, loc *time.Location) error {
	if fieldValue.Kind() == reflect.Slice {
		slice := reflect.MakeSlice(fieldValue.Type(), len(values), len(values))
		for i, v := range values {
			v = strings.TrimSpace(v)
			if err := setScalar(slice.Index(i), v, timeFormat, loc); err != nil {
				return err
			}
		}
		fieldValue.Set(slice)
		return nil
	}
	return setScalar(fieldValue, values[0], timeFormat, loc)
}

// setScalar 将单个字符串转换并写入标量字段，支持指针、字符串、布尔、整型、浮点型、time.Time
func setScalar(fieldValue reflect.Value, value string, timeFormat string, loc *time.Location) error {
	// 逐层解指针（带深度上限，防自引用指针类型无限分配）
	for depth := 0; fieldValue.Kind() == reflect.Ptr; depth++ {
		if depth >= maxPtrDerefDepth {
			return fmt.Errorf("pointer nesting too deep for type %s", fieldValue.Type())
		}
		if fieldValue.IsNil() {
			fieldValue.Set(reflect.New(fieldValue.Type().Elem()))
		}
		fieldValue = fieldValue.Elem()
	}
	// time.Time 需特殊解析（时间戳或多种时间格式）
	if fieldValue.Type() == timeType {
		return setTime(fieldValue, value, timeFormat, loc)
	}
	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(value)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		fieldValue.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		// 按目标类型的位宽解析，溢出时 ParseInt 直接报错，避免 SetInt 静默截断
		n, err := strconv.ParseInt(value, 10, fieldValue.Type().Bits())
		if err != nil {
			return err
		}
		fieldValue.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		// 按目标类型的位宽解析，溢出时 ParseUint 直接报错，避免 SetUint 静默截断
		n, err := strconv.ParseUint(value, 10, fieldValue.Type().Bits())
		if err != nil {
			return err
		}
		fieldValue.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return err
		}
		fieldValue.SetFloat(f)
	default:
		// 其他类型暂不支持，跳过
	}
	return nil
}

// setTime 将字符串解析为 time.Time：
//   - time_format 为 unix/unixmilli/unixmicro/unixnano 时按对应精度的时间戳解析
//   - time_format 为其他非空值时作为 Go 时间布局（layout）解析，可支持任意非标准排列
//   - time_format 为空时自动探测：纯数字按时间戳（按位数推断精度），否则依次尝试常见布局
func setTime(fieldValue reflect.Value, value, timeFormat string, loc *time.Location) error {
	if value == "" {
		return nil
	}
	if loc == nil {
		loc = time.Local
	}
	switch timeFormat {
	case "unix":
		return setUnix(fieldValue, value, time.Second, loc)
	case "unixmilli":
		return setUnix(fieldValue, value, time.Millisecond, loc)
	case "unixmicro":
		return setUnix(fieldValue, value, time.Microsecond, loc)
	case "unixnano":
		return setUnix(fieldValue, value, time.Nanosecond, loc)
	case "":
		// 自动探测：纯数字视为时间戳，否则尝试常见字符串布局
		if isAllDigits(value) {
			return setUnixAuto(fieldValue, value, loc)
		}
		for _, layout := range defaultTimeLayouts {
			if tv, err := time.ParseInLocation(layout, value, loc); err == nil {
				fieldValue.Set(reflect.ValueOf(tv))
				return nil
			}
		}
		return fmt.Errorf("cannot parse %q as time.Time", value)
	default:
		// 将 timeFormat 作为 Go layout 解析
		tv, err := time.ParseInLocation(timeFormat, value, loc)
		if err != nil {
			return err
		}
		fieldValue.Set(reflect.ValueOf(tv))
		return nil
	}
}

// setUnix 按给定精度将时间戳字符串解析为 time.Time
func setUnix(fieldValue reflect.Value, value string, unit time.Duration, loc *time.Location) error {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}
	tv := time.Unix(0, n*int64(unit)).In(loc)
	fieldValue.Set(reflect.ValueOf(tv))
	return nil
}

// setUnixAuto 依据数字位数推断时间戳精度（13=毫秒,16=微秒,19=纳秒），其余按秒处理
func setUnixAuto(fieldValue reflect.Value, value string, loc *time.Location) error {
	var unit time.Duration
	switch len(value) {
	case 13:
		unit = time.Millisecond
	case 16:
		unit = time.Microsecond
	case 19:
		unit = time.Nanosecond
	default:
		unit = time.Second
	}
	return setUnix(fieldValue, value, unit, loc)
}

// isAllDigits 判断字符串是否全部为数字（允许首位负号，但单独的 "-" 不算数字）
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	hasDigit := false
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
		hasDigit = true
	}
	return hasDigit
}
