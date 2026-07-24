package zchttp

import (
	"encoding/json"
	"errors"
	"fmt"
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

// validateRequest 对已绑定的请求执行参数校验：nonzero 字段 + 自定义 Validator。
// meta 为注册阶段预计算的 structMeta，避免请求阶段重复反射解析。
func validateRequest(reqPtr reflect.Value, meta structMeta) error {
	if err := validateNonzero(reqPtr, meta); err != nil {
		return err
	}
	return validateCustom(reqPtr, meta)
}

// Validator 由 handler 的 Req 结构体可选实现，用于声明式 nonzero 之外的
// 业务校验与跨字段校验（如「两者至少填一个」「结束时间需晚于开始时间」等）。
// 绑定与 nonzero 校验通过后，若 Req 实现了该接口则调用其 Validate 方法。
type Validator interface {
	Validate() error
}

// validateCustom 调用 Req 的 Validate 方法（若实现 Validator），并将其错误归一化为
// *ValidationError，以便 HttpEngine 统一路由到 OnValidationError 回调（默认 400）。
// 仅校验顶层 Req，不递归进入嵌套结构体。
// meta 为注册阶段预计算的 structMeta，直接使用其 implementsValidator 判断。
func validateCustom(reqPtr reflect.Value, meta structMeta) error {
	if !meta.implementsValidator {
		return nil
	}
	v, ok := reqPtr.Interface().(Validator)
	if !ok {
		return nil
	}
	err := v.Validate()
	if err == nil {
		return nil
	}
	// 用户已返回结构化校验错误则透传，否则包装以保留原始错误链
	var ve *ValidationError
	if errors.As(err, &ve) {
		return err
	}
	return &ValidationError{Message: err.Error(), Err: err}
}

// ValidationError 表示请求参数校验失败，由 validateNonzero、Validate() 等校验逻辑产生。
// HttpEngine 在 ServeHTTP 中通过 errors.As 识别该类型，并路由到 OnValidationError
// 回调处理（默认 DefaultValidationErrorHandler，返回 400）。
type ValidationError struct {
	Field   string // 校验失败的字段名（绑定名），业务校验可留空
	Message string // 失败原因
	Err     error  // 可选：包装底层错误（如 Validate() 返回的业务错误），支持 errors.Is/As 穿透
}

// NewValidationError 创建一个字段级校验错误，用于在自定义校验逻辑中返回。
func NewValidationError(Field string, Message string, Err error) *ValidationError {
	return &ValidationError{Field: Field, Message: Message, Err: Err}
}

func (e *ValidationError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return fmt.Sprintf("field %q %s", e.Field, e.Message)
}

// Unwrap 返回被包装的底层错误，使 errors.Is/As 可穿透到原始错误
func (e *ValidationError) Unwrap() error { return e.Err }

// BindingError 表示请求数据绑定失败（如 JSON 解析错误、类型转换失败等）。
// HttpEngine 在 ServeHTTP 中通过 errors.As 识别该类型，同样路由到 OnValidationError
// 回调处理（默认 400）。与 ValidationError 的区别在于它发生在绑定阶段而非校验阶段。
type BindingError struct {
	Message string // 失败原因
	Err     error  // 包装底层错误（如 *json.SyntaxError），支持 errors.Is/As 穿透
}

// NewBindingError 创建一个绑定错误。
func NewBindingError(err error) *BindingError {
	return &BindingError{Message: err.Error(), Err: err}
}

func (e *BindingError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	return e.Message
}

// Unwrap 返回被包装的底层错误，使 errors.Is/As 可穿透到原始错误
func (e *BindingError) Unwrap() error { return e.Err }

// validateNonzero 在参数绑定完成后，校验 nonzero 字段不得为零值。
// 递归进入嵌套结构体字段（含 *struct 指针穿透），使用 visited 防止循环引用。
//
// 校验规则：
//   - 只要字段标注 nonzero:"true"，就校验零值，所见即所得；
//   - 未标注 nonzero 的字段一律不做零值校验。
//
// 零值判定使用 reflect.Value.IsZero：nil 指针/切片、空字符串、数字 0、bool false 等均视为零值。
// meta 为注册阶段预计算的 structMeta，直接遍历其 fields 避免请求阶段反射。
func validateNonzero(reqPtr reflect.Value, meta structMeta) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	return validateNonzeroWalk(elem, meta, nil, "", false)
}

// visitKey 用于 visited map 的复合键，同时记录地址和类型，
// 避免值类型首字段与父结构体共享地址时被误判为循环引用。
type visitKey struct {
	ptr uintptr
	typ reflect.Type
}

// validateNonzeroWalk 递归遍历结构体树，校验每个结构体的 nonzero 字段。
// 规则：
//   - 若字段 nonzero 且零值 → 报错
//   - 若字段 nonzero 且非零 → 校验通过，若为嵌套结构体/指针/结构体切片/map 则递归进入
//   - 若字段非 nonzero 但为嵌套结构体/指针且非零值 → 不报本级，但递归进入子字段校验
//
// prefix 为嵌套路径前缀（如 "company."），顶层调用时传空字符串。
// 报错时 Field 为 prefix + 字段绑定名，如 "company.name"，与 API 命名一致，便于客户端定位。
// isTempCopy 表示 v 是临时副本（如 map 值的可寻址拷贝）：副本地址不代表原始数据，
// 且临时对象被 GC 回收后地址可能被后续分配复用，若计入 visited 会误判"已访问"
// 导致漏校验，因此不将副本自身地址注册进 visited（环检测由 map 桶指针键承担）。
func validateNonzeroWalk(v reflect.Value, meta structMeta, visited map[visitKey]bool, prefix string, isTempCopy bool) error {
	if v.Kind() != reflect.Struct {
		return nil
	}
	if visited == nil {
		visited = make(map[visitKey]bool)
	}
	if !isTempCopy {
		key := visitKey{ptr: v.Addr().Pointer(), typ: v.Type()}
		if visited[key] {
			return nil // 已访问，防止循环递归
		}
		visited[key] = true
	}

	for i := range meta.fields {
		fm := &meta.fields[i]
		fv := fieldByIndex(v, fm.indices)

		if fm.nonzero {
			// nonzero 字段：零值则报错
			if fv.IsZero() {
				return &ValidationError{Field: prefix + fm.name, Message: "is required"}
			}
		}

		// 若为非零值的嵌套结构体/指针字段，递归进入子字段校验
		// （nonzero 字段已校验通过；非 nonzero 字段只要非零值就递归）
		if fv.IsZero() {
			continue
		}
		subV := fv
		wasPtr := subV.Kind() == reflect.Ptr
		if wasPtr {
			subV = subV.Elem()
		}
		if subV.Kind() == reflect.Struct {
			subMeta := buildStructMeta(subV.Type())
			// 指针解引用后的目标是真实堆对象（地址稳定）；
			// 值类型字段与所属 struct 同属一块内存，继承临时副本标记
			if err := validateNonzeroWalk(subV, subMeta, visited, prefix+fm.name+".", isTempCopy && !wasPtr); err != nil {
				return err
			}
		} else if subV.Kind() == reflect.Slice {
			// 结构体切片/结构体指针切片：递归校验每个元素的 nonzero 字段
			elemType := subV.Type().Elem()
			isPtrElem := elemType.Kind() == reflect.Ptr
			if isPtrElem {
				elemType = elemType.Elem()
			}
			if elemType.Kind() == reflect.Struct {
				subMeta := buildStructMeta(elemType)
				for i := 0; i < subV.Len(); i++ {
					elem := subV.Index(i)
					if isPtrElem {
						if elem.IsNil() {
							continue
						}
						elem = elem.Elem()
					}
					// 切片元素位于共享的底层数组，地址稳定，非临时副本
					if err := validateNonzeroWalk(elem, subMeta, visited, prefix+fm.name+".", false); err != nil {
						return err
					}
				}
			}
		} else if subV.Kind() == reflect.Map {
			// map[string]Struct / map[string]*Struct：递归校验每个 value 的 nonzero 字段
			valType := subV.Type().Elem()
			isPtrVal := valType.Kind() == reflect.Ptr
			if isPtrVal {
				valType = valType.Elem()
			}
			if valType.Kind() == reflect.Struct {
				// 对 map 自身（底层桶指针）做循环检测：
				// 非指针值类型的 struct 副本仍共享 map 底层桶，可形成环，
				// 仅靠副本地址或指针值地址无法覆盖此场景
				mapKey := visitKey{ptr: subV.Pointer(), typ: subV.Type()}
				if visited[mapKey] {
					continue // 该 map 已在递归路径中处理过，跳过防止循环递归
				}
				visited[mapKey] = true
				subMeta := buildStructMeta(valType)
				for _, key := range subV.MapKeys() {
					val := subV.MapIndex(key)
					if isPtrVal {
						if val.IsNil() {
							continue
						}
						// 对指针类型的 map 值，使用原始指针地址做循环检测
						ptrKey := visitKey{ptr: val.Pointer(), typ: valType}
						if visited[ptrKey] {
							continue // 已访问，跳过防止循环递归
						}
						visited[ptrKey] = true
						val = val.Elem()
					}
					// MapIndex 返回的值不可寻址，需复制一份再传入校验；
					// 副本为临时对象，其地址不可靠，标记 isTempCopy=true
					valCopy := reflect.New(valType).Elem()
					valCopy.Set(val)
					if err := validateNonzeroWalk(valCopy, subMeta, visited, prefix+fm.name+".", true); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
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
		return json.NewDecoder(r.Body).Decode(reqPtr.Interface())
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
		// 未知 Content-Type：若有请求体则尝试按 JSON 解析，否则不绑定
		if r.Body != nil {
			return json.NewDecoder(r.Body).Decode(reqPtr.Interface())
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

// maxPtrDerefDepth 指针/元素解引用的最大层数，
// 防止自引用命名类型（如 type P *P、type S []S）导致死循环或无限递归
const maxPtrDerefDepth = 32

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
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return err
		}
		fieldValue.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, 64)
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
