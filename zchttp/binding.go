package zchttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

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
// 按来源分别判定：存在 Form 链可绑定字段时才解析/绑定 query；选中的 body 来源存在
// 可绑定字段时才读取 body。两者都没有时不读取 body、不解析 query。
// query 一旦参与绑定就严格：URL query 语法错误或已知字段转换失败直接返回错误
// （最终由 HttpEngine 包装为 *BindingError 并返回 400）。
// 按 method 分流：GET/DELETE/HEAD 仅绑定 query；其余方法（POST/PUT/PATCH 等）
// 先严格绑定 query，再绑定请求体，body 中出现的字段覆盖 query 已绑定的同名字段，
// body 中缺失的字段保留 query 绑定值
// （兼容 REST 常见的"query 放控制参数 + body 放资源数据"混合传参风格）。
// keepRawBody 为真（Req 顶层嵌入 KeepRawBody）时无论 method 均仅绑定 query，
// r.Body 不被读取，原样留给中间件与 handler。
// 路径参数在 ServeHTTP 中于本函数之后绑定，覆盖同名 query/body 值（path > body > query）。
// body 覆盖粒度由解码器语义决定：JSON 中显式出现的字段（含显式零值如 "page": 0）
// 覆盖 query 值；JSON null 对非指针字段为 no-op（保留 query 值）、对指针字段置 nil。
// 详见 docs/parameter-binding.md。
// meta 为注册阶段预计算的 structMeta，避免请求阶段重复反射解析。
// multipartMaxMemory 为 multipart/form-data 解析的内存缓冲上限（字节）。
func bindRequestData(r *http.Request, reqPtr reflect.Value, meta structMeta, multipartMaxMemory int64, keepRawBody bool) error {
	if meta.hasFormText {
		queryValues, err := parseStrictQuery(r)
		if err != nil {
			return err
		}
		if err := bindQueryValues(reqPtr, queryValues, meta); err != nil {
			return err
		}
	}
	if keepRawBody {
		return nil
	}
	switch r.Method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		return nil
	default:
		if !meta.hasAnyBody {
			return nil
		}
		return bindBody(r, reqPtr, meta, multipartMaxMemory)
	}
}

// parseStrictQuery 严格解析 URL query：与 r.URL.Query() 不同，语法错误会返回而不是丢弃。
// 这样 GET/DELETE/HEAD 与 form/JSON 等所有来源对非法 query 的行为一致。
func parseStrictQuery(r *http.Request) (url.Values, error) {
	if r.URL == nil {
		return nil, nil
	}
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("invalid query string: %w", err)
	}
	return values, nil
}

// bindBody 处理携带请求体的方法，依据 Content-Type 选择 JSON、表单或 multipart 绑定。
// meta 为注册阶段预计算的 structMeta，透传给 bindValues。
// multipartMaxMemory 为 multipart/form-data 解析的内存缓冲上限（字节）。
func bindBody(r *http.Request, reqPtr reflect.Value, meta structMeta, multipartMaxMemory int64) error {
	contentType := r.Header.Get("Content-Type")
	if contentType != "" {
		mediaType, _, err := mime.ParseMediaType(contentType)
		if err != nil {
			return fmt.Errorf("invalid Content-Type %q: %w", contentType, err)
		}
		contentType = mediaType
	}

	switch contentType {
	case "application/json":
		if !meta.hasJSON {
			return nil
		}
		return decodeJSONBody(r.Body, reqPtr.Interface())
	case "application/x-www-form-urlencoded":
		if !meta.hasFormText {
			return nil
		}
		values, err := parseURLEncodedBody(r)
		if err != nil {
			return err
		}
		return bindValues(reqPtr, values, nil, meta)
	case "multipart/form-data":
		if !meta.hasFormText && !meta.hasFormFile {
			return nil
		}
		// 超出部分由 net/http 写入临时文件；去掉 query 后再解析，避免没有 query
		// 绑定字段时仍因 query 解析错误而失败。
		fake := requestWithoutQuery(r)
		if err := fake.ParseMultipartForm(multipartMaxMemory); err != nil {
			return err
		}
		r.MultipartForm = fake.MultipartForm
		r.Form = fake.Form
		r.PostForm = fake.PostForm
		return bindValues(reqPtr, r.MultipartForm.Value, r.MultipartForm.File, meta)
	default:
		// 未知 Content-Type：若有请求体则尝试按 JSON 解析，否则不绑定
		if !meta.hasJSON {
			return nil
		}
		return decodeJSONBody(r.Body, reqPtr.Interface())
	}
}

// requestWithoutQuery 复制 request 并清空 URL query，用于只解析 body 的表单/multipart，
// 避免 ParseForm/ParseMultipartForm 顺带解析并校验 query。
func requestWithoutQuery(r *http.Request) *http.Request {
	fake := new(http.Request)
	*fake = *r
	if r.URL != nil {
		u := *r.URL
		u.RawQuery = ""
		fake.URL = &u
	}
	return fake
}

// parseURLEncodedBody 只解析 urlencoded 请求体并返回待绑定的表单值。
// net/http 的 ParseForm 只对 POST/PUT/PATCH 读取 body，其余方法（OPTIONS/TRACE/CONNECT）
// 会静默忽略 body；这里用浅拷贝伪装成 POST 复用标准库读取逻辑，同时清空 query，
// 避免 body 解析顺带解析/校验 query。保留 ParseForm 的 10 MB 上限与错误语义。
func parseURLEncodedBody(r *http.Request) (url.Values, error) {
	// 只解析 body：复制 request 并清空 query，避免 ParseForm 顺带解析/校验 query。
	fake := requestWithoutQuery(r)
	fake.Method = http.MethodPost
	if err := fake.ParseForm(); err != nil {
		return nil, err
	}
	r.PostForm = fake.PostForm
	r.Form = fake.Form
	return r.PostForm, nil
}

// decodeJSONBody 只接受一个完整 JSON 值；空请求体合法，尾随空白之外的内容均报错。
func decodeJSONBody(body io.Reader, dst any) error {
	if body == nil {
		return nil
	}
	decoder := json.NewDecoder(body)
	if err := decoder.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != nil {
		if errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
	return errors.New("request body must contain exactly one JSON value")
}

// bindValues 将表单 body 的 values/files 按字段标签映射到结构体。
// meta 为注册阶段预计算的 structMeta，直接遍历其 fields 避免请求阶段反射。
// 表单 body 与 query 一样采用严格绑定：已知字段转换失败返回错误，由调用方包装为
// BindingError 并返回 400；未知参数与不可绑定类型仍按既有规则跳过。
func bindValues(reqPtr reflect.Value, values map[string][]string, files map[string][]*multipart.FileHeader, meta structMeta) error {
	return bindValuesMode(reqPtr, values, files, meta, "form")
}

// bindQueryValues 严格绑定 query：任何已知字段的值转换失败都会返回错误，
// 由调用方包装为 BindingError 并返回 400；未知参数与不可绑定类型仍按既有规则跳过。
func bindQueryValues(reqPtr reflect.Value, values map[string][]string, meta structMeta) error {
	return bindValuesMode(reqPtr, values, nil, meta, "query")
}

func bindValuesMode(reqPtr reflect.Value, values map[string][]string, files map[string][]*multipart.FileHeader, meta structMeta, source string) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	for i := range meta.fields {
		fm := &meta.fields[i]
		if fm.name == "" {
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
		// 单字段转换失败立即返回错误，不写入半成品；错误信息标明来源。
		if err := setFieldValue(fieldValue, vs, fm.timeFormat, fm.timeLocation); err != nil {
			return fmt.Errorf("invalid %s parameter %q: %w", source, fm.name, err)
		}
	}
	return nil
}

// bindPathParams 将捕获的路由路径参数值按注册顺序写入 Req 字段。
// 路径参数覆盖同名 query/body 值，必须在 bindRequestData 之后调用。
// 单个参数转换失败立即返回错误（区别于 bindValues 的尽力绑定策略）；
// 尾部可选参数省略或通配零段命中时无捕获值，跳过绑定以保留模板默认值或零值。
func bindPathParams(reqPtr reflect.Value, params []pathParamBinding, values []string) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	for i := range params {
		if i >= len(values) {
			// 尾部可选参数省略或通配零段命中，剩余绑定均跳过
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

// resolveFormName 解析 Form 链（query/form/multipart/path）绑定名：
//   - form:"-" 无条件跳过，返回空；
//   - form 标签有普通名字时直接使用；
//   - 无 form 时，普通 json 名作为兼容 fallback；
//   - json:"-"、json:"-," 是 JSON 语义专用值，不污染 Form 链，回退字段名；
//   - 最后回退字段名。
func resolveFormName(field reflect.StructField) string {
	if formTag, ok := field.Tag.Lookup("form"); ok {
		name := tagName(formTag)
		if name == "-" {
			return ""
		}
		if name != "" {
			return name
		}
	}
	if jsonTag, ok := field.Tag.Lookup("json"); ok {
		name := tagName(jsonTag)
		if name == "-" {
			// json:"-" 与 json:"-," 的 "-" 都是 JSON 语义，Form 链回退字段名。
			return field.Name
		}
		if name != "" {
			return name
		}
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

// setFieldValue 将字符串值列表写入字段：切片字段绑定全部值，标量字段绑定第一个值。
// 切片元素必须是 setScalar 可写入的标量（或其指针）：元素为结构体、数组、多层切片等
// 不支持的类型时直接跳过，保持字段绑定前值，避免写入同长度的全零元素。
func setFieldValue(fieldValue reflect.Value, values []string, timeFormat string, loc *time.Location) error {
	if fieldValue.Kind() == reflect.Slice {
		if !isScalarType(fieldValue.Type().Elem()) {
			return nil
		}
		old := fieldValue
		slice := reflect.MakeSlice(fieldValue.Type(), len(values), len(values))
		for i, v := range values {
			// 时间切片的空字符串元素表示“跳过该元素并保留原值”，
			// 与标量 time.Time 的空值不写入保持一致；其余类型（如 string）的空值仍正常写入。
			if v == "" && isTimeValueType(fieldValue.Type().Elem()) {
				if i < old.Len() {
					slice.Index(i).Set(old.Index(i))
				}
				continue
			}
			if err := setScalar(slice.Index(i), v, timeFormat, loc); err != nil {
				return err
			}
		}
		fieldValue.Set(slice)
		return nil
	}
	return setScalar(fieldValue, values[0], timeFormat, loc)
}

// isTimeValueType 判断类型（含指针）是否为 time.Time，用于识别时间切片的空字符串元素。
func isTimeValueType(t reflect.Type) bool {
	return derefType(t) == timeType
}

// setScalar 将单个字符串转换并写入标量字段，支持指针、字符串、布尔、有符号/无符号整型、
// 浮点型、time.Time。转换先在临时值中完成，成功后才提交；失败时目标字段保持原值。
// 不支持的类型静默跳过（尽力绑定语义，不返回错误）。
func setScalar(fieldValue reflect.Value, value string, timeFormat string, loc *time.Location) error {
	parsed := reflect.New(fieldValue.Type()).Elem()
	supported, err := parseScalar(parsed, value, timeFormat, loc, 0)
	if err != nil {
		return err
	}
	if supported {
		fieldValue.Set(parsed)
	}
	return nil
}

func parseScalar(fieldValue reflect.Value, value string, timeFormat string, loc *time.Location, depth int) (bool, error) {
	if fieldValue.Kind() == reflect.Ptr {
		if depth >= maxPtrDerefDepth {
			return false, fmt.Errorf("pointer nesting too deep for type %s", fieldValue.Type())
		}
		parsed := reflect.New(fieldValue.Type().Elem())
		supported, err := parseScalar(parsed.Elem(), value, timeFormat, loc, depth+1)
		if err != nil || !supported {
			return supported, err
		}
		fieldValue.Set(parsed)
		return true, nil
	}
	if fieldValue.Type() == timeType {
		if value == "" {
			return false, nil
		}
		return true, setTime(fieldValue, value, timeFormat, loc)
	}
	switch fieldValue.Kind() {
	case reflect.String:
		fieldValue.SetString(value)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return false, err
		}
		fieldValue.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, fieldValue.Type().Bits())
		if err != nil {
			return false, err
		}
		fieldValue.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(value, 10, fieldValue.Type().Bits())
		if err != nil {
			return false, err
		}
		fieldValue.SetUint(n)
	case reflect.Float32, reflect.Float64:
		f, err := strconv.ParseFloat(value, fieldValue.Type().Bits())
		if err != nil {
			return false, err
		}
		fieldValue.SetFloat(f)
	default:
		return false, nil
	}
	return true, nil
}

// isScalarType 判断类型能否被 parseScalar 写入：string/bool/int/uint/float 各宽度、time.Time，
// 以及它们的指针（指针层数上限与 parseScalar 一致）
func isScalarType(t reflect.Type) bool {
	for depth := 0; t.Kind() == reflect.Ptr; depth++ {
		if depth >= maxPtrDerefDepth {
			return false
		}
		t = t.Elem()
	}
	if t == timeType {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// isQueryBindableType 判定字段能否被 query/form 扁平绑定写入：标量（含任意层指针）
// 或元素为标量的切片。数组、map、结构体、多层切片与指针包裹的容器均不可绑定，
// 供 OpenAPI 生成过滤 query 参数，避免展示运行时不会写入的字段。
func isQueryBindableType(t reflect.Type) bool {
	if isScalarType(t) {
		return true
	}
	return t.Kind() == reflect.Slice && isScalarType(t.Elem())
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

// setUnix 按给定精度将时间戳字符串解析为 time.Time，避免先统一换算为纳秒导致溢出。
func setUnix(fieldValue reflect.Value, value string, unit time.Duration, loc *time.Location) error {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return err
	}
	var tv time.Time
	switch unit {
	case time.Second:
		tv = time.Unix(n, 0)
	case time.Millisecond:
		tv = time.UnixMilli(n)
	case time.Microsecond:
		tv = time.UnixMicro(n)
	case time.Nanosecond:
		tv = time.Unix(0, n)
	default:
		return fmt.Errorf("unsupported unix timestamp unit %s", unit)
	}
	fieldValue.Set(reflect.ValueOf(tv.In(loc)))
	return nil
}

// setUnixAuto 依据数字位数推断时间戳精度；可选负号不计入位数。
func setUnixAuto(fieldValue reflect.Value, value string, loc *time.Location) error {
	digits := strings.TrimPrefix(value, "-")
	var unit time.Duration
	switch len(digits) {
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
