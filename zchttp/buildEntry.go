package zchttp

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// OpenAPIMeta 可嵌入到 handler 的 Req 结构体中，用于声明 OpenAPI 操作级元信息。
// 通过结构体标签设置（各自独立的 tag）：
//   - tags:        以 "/" 分隔的标签，如 "User Management/Account"
//   - summary:     操作摘要
//   - description: 操作详细描述
//
// 示例：
//
//	type CreateUserReq struct {
//	    zchttp.OpenAPIMeta `tags:"User Management/Account" summary:"创建用户"`
//	    Name string `json:"name" required:"true"`
//	}
type OpenAPIMeta struct{}

// metaType 用于识别嵌入的 OpenAPIMeta 字段
var metaType = reflect.TypeOf(OpenAPIMeta{})

// validatorType 是 Validator 接口的 reflect.Type，用于快速判断结构体是否实现了校验接口
var validatorType = reflect.TypeOf((*Validator)(nil)).Elem()

// fieldMeta 缓存单个结构体字段的绑定元信息，包括字段名、类型特征、标签值等
type fieldMeta struct {
	name         string         // 字段绑定名（form/json 标签或字段名）
	indices      []int          // 从根结构体出发的字段索引路径，如 []int{0}
	required     bool           // 是否必填（required:"true" 且无 default）
	hasDefault   bool           // 是否声明了 default 标签
	defaultVal   string         // default 标签的值
	isSlice      bool           // 是否为非文件切片（用于默认值逗号分隔展开）
	isFile       bool           // 是否为 *multipart.FileHeader
	isFileSlice  bool           // 是否为 []*multipart.FileHeader
	timeFormat   string         // time_format 标签值
	timeLocation *time.Location // time_location 标签解析后的时区
}

// structMeta 缓存单个结构体类型的聚合元信息
type structMeta struct {
	fields              []fieldMeta // 所有可绑定字段的元信息列表
	hasRequired         bool        // 是否存在 required 字段
	implementsValidator bool        // 结构体是否实现 Validator 接口
}

// operationMeta 从 Req 结构体的 OpenAPIMeta 嵌入字段解析出的操作级元信息
type operationMeta struct {
	tags        []string
	summary     string
	description string
}

// routeEntry 是单条路由在注册表中的存储单元，包含 handler、该路由作用域内的中间件链快照，
// 以及注册阶段预计算的反射信息（避免请求阶段重复反射）
type routeEntry struct {
	handler     any
	middlewares []MiddlewareHandler

	// 以下字段在注册阶段通过反射预计算，请求阶段直接使用
	handlerVal  reflect.Value // reflect.ValueOf(handler)，用于反射调用
	reqType     reflect.Type  // handler 第二个参数的类型（Req）
	resType     reflect.Type  // handler 第一个返回值的类型（Res）
	reqIsPtr    bool          // Req 是否为指针类型（*struct）
	resIsPtr    bool          // Res 是否为指针类型（*struct）
	reqElemType reflect.Type  // Req 结构体的具体类型（已解引用指针），用于 reflect.New 创建实例

	// handler 的位置信息，用于路由冲突提示与 OpenAPI 摘要
	handlerName string // 全限定函数名
	handlerFile string // 定义文件路径
	handlerLine int    // 定义行号

	// Req 结构体的反射元信息，注册阶段预计算，供 binding/validation/defaults 及 OpenAPI 生成使用
	reqMeta structMeta
	// Res 结构体的反射元信息，注册阶段预计算，供 OpenAPI 生成使用
	resMeta structMeta
	// Req 的 OpenAPIMeta 操作级元信息，注册阶段预计算
	opMeta operationMeta
	// 预填充默认值的 Req 模板，请求时浅拷贝后直接绑定
	defaultReq reflect.Value
	// 是否需要深拷贝（模板中存在非 nil 的指针/切片/map 字段）
	needsDeepCopy bool
}

// buildEntry 校验 handler 签名、构建中间件链、预计算反射信息，返回完整的 routeEntry。
// handler 必须是 func(ctx context.Context, req Req) (Res, error) 的形式，
// 其中 Req 和 Res 必须是结构体或结构体指针。
// globalMiddlewares 与 groupMiddlewares 合并后作为该路由的中间件链快照。
func buildEntry(handler any, globalMiddlewares, groupMiddlewares []MiddlewareHandler) (*routeEntry, error) {
	if handler == nil {
		return nil, fmt.Errorf("handler must not be nil")
	}
	t := reflect.TypeOf(handler)
	if t.Kind() != reflect.Func {
		return nil, fmt.Errorf("handler must be a function")
	}
	if t.NumIn() != 2 {
		return nil, fmt.Errorf("handler must have exactly two arguments: context.Context and a struct")
	}
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	if t.In(0) != contextType {
		return nil, fmt.Errorf("the first argument of handler must be context.Context")
	}
	if t.In(1).Kind() != reflect.Struct && !isStructPtr(t.In(1)) {
		return nil, fmt.Errorf("the second argument of handler must be a struct or a pointer to struct")
	}
	if t.NumOut() != 2 {
		return nil, fmt.Errorf("handler must have exactly two return values: a struct and an error")
	}
	if t.Out(0).Kind() != reflect.Struct && !isStructPtr(t.Out(0)) {
		return nil, fmt.Errorf("the first return value of handler must be a struct or a pointer to struct")
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	if !t.Out(1).Implements(errorType) {
		return nil, fmt.Errorf("the second return value of handler must be an error")
	}

	// 预计算反射信息：请求阶段直接使用，避免重复 reflect.TypeOf / reflect.ValueOf
	reqType := t.In(1)
	resType := t.Out(0)
	reqIsPtr := isStructPtr(reqType)
	resIsPtr := isStructPtr(resType)
	var reqElemType reflect.Type
	if reqIsPtr {
		reqElemType = reqType.Elem()
	} else {
		reqElemType = reqType
	}

	// 预计算 Req 结构体的字段元信息（binding/validation/defaults 及 OpenAPI 生成使用）
	reqMeta := buildStructMeta(reqElemType)

	// 预计算 Res 结构体的字段元信息（OpenAPI 生成使用，避免重复反射遍历结构体字段）
	var resElemType reflect.Type
	if resIsPtr {
		resElemType = resType.Elem()
	} else {
		resElemType = resType
	}
	resMeta := buildStructMeta(resElemType)

	// 获取 handler 的位置信息（用于路由冲突提示、OpenAPI 摘要、default 误用警告）
	handlerName, handlerFile, handlerLine := "unknown", "unknown", 0
	pc := reflect.ValueOf(handler).Pointer()
	if fn := runtime.FuncForPC(pc); fn != nil {
		handlerName = fn.Name()
		handlerFile, handlerLine = fn.FileLine(pc)
	}

	// 预计算 Req 的 OpenAPIMeta 操作级元信息（OpenAPI 生成使用）
	opMeta := buildOperationMeta(reqElemType)

	// 预计算带默认值的 Req 模板：注册阶段填充默认值，请求时浅拷贝复用
	defaultReqPtr := reflect.New(reqElemType)
	_ = applyDefaults(defaultReqPtr, reqMeta)
	defaultReq := defaultReqPtr.Elem()

	// 扫描模板中是否存在非 nil 引用类型字段（指针/切片/map），
	// 若存在则请求阶段需要深拷贝以断开共享（string/int 等不可变值类型无需深拷贝）
	needsDeepCopy := hasRefFields(defaultReq)

	// 快照中间件链
	mws := make([]MiddlewareHandler, 0, len(globalMiddlewares)+len(groupMiddlewares))
	mws = append(mws, globalMiddlewares...)
	mws = append(mws, groupMiddlewares...)

	return &routeEntry{
		handler:       handler,
		middlewares:   mws,
		handlerVal:    reflect.ValueOf(handler),
		reqType:       reqType,
		resType:       resType,
		reqIsPtr:      reqIsPtr,
		resIsPtr:      resIsPtr,
		reqElemType:   reqElemType,
		reqMeta:       reqMeta,
		resMeta:       resMeta,
		opMeta:        opMeta,
		defaultReq:    defaultReq,
		needsDeepCopy: needsDeepCopy,
		handlerName:   handlerName,
		handlerFile:   handlerFile,
		handlerLine:   handlerLine,
	}, nil
}

// isStructPtr 判断类型是否为指向结构体的指针
func isStructPtr(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr && t.Elem().Kind() == reflect.Struct
}

// buildOperationMeta 从 Req 结构体中查找嵌入的 OpenAPIMeta 字段并解析其标签，
// 提取 tags（以 "/" 分隔）、summary 和 description 操作级元信息。
// 若未嵌入 OpenAPIMeta 或未设置对应标签，则返回零值。
func buildOperationMeta(reqType reflect.Type) operationMeta {
	var m operationMeta
	if reqType.Kind() != reflect.Struct {
		return m
	}
	for i := 0; i < reqType.NumField(); i++ {
		f := reqType.Field(i)
		if !(f.Anonymous && f.Type == metaType) {
			continue
		}
		if v := f.Tag.Get("tags"); v != "" {
			for _, p := range strings.Split(v, "/") {
				if p = strings.TrimSpace(p); p != "" {
					m.tags = append(m.tags, p)
				}
			}
		}
		m.summary = f.Tag.Get("summary")
		m.description = f.Tag.Get("description")
		return m
	}
	return m
}

// checkUnsupportedDefaults 递归遍历结构体类型树，检测两种 default 误用：
//
//  1. 类型不支持：default 写在 time.Time / struct 等非标量类型上，注册阶段即不填充。
//  2. 值类型无法到达：default 写在值类型字段（int/string/bool）上，但所在 struct
//     非"值嵌套可达"（即路径上存在指针/切片/map 边），请求阶段永不填充。
//
// viaValue 表示当前 struct 是否通过纯值类型链可达（含义同 walkTypeUsage），
// 顶层 Req/Res 为 true。visiting 用于自引用循环检测。
// method/path 为接口路由信息，handlerName/handlerFile/handlerLine 为 handler 源码位置
// （闭包 handler 的 handlerFile 为 "<autogenerated>"，此时降级使用 handlerName）。
func checkUnsupportedDefaults(t reflect.Type, viaValue bool, method, path, handlerName, handlerFile string, handlerLine int, visiting map[reflect.Type]bool) {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return
	}
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Anonymous && f.Type == metaType {
			continue
		}

		handlerLoc := fmt.Sprintf("%s:%d", handlerFile, handlerLine)
		if handlerFile == "<autogenerated>" {
			handlerLoc = handlerName
		}

		_, hasDefault := f.Tag.Lookup("default")
		if hasDefault {
			if !isDefaultSupported(f.Type) {
				// 情况 1：类型不支持（如 time.Time / struct 类型）
				slog.Warn("default tag on unsupported type, ignored",
					"route", method+" "+path,
					"handler", handlerLoc,
					"struct", t.Name(),
					"field", f.Name,
					"type", f.Type.String(),
				)
			} else if f.Type.Kind() != reflect.Ptr && !viaValue {
				// 情况 2：值类型字段在非"值嵌套可达"的 struct 中。
				// applyDefaults 请求阶段仅填 nil 指针，值类型永不填充；
				// 注册阶段因路径上有 nil 指针/切片/map 也无法到达。
				slog.Warn("default tag on value field in non-value-reachable struct, never applied",
					"route", method+" "+path,
					"handler", handlerLoc,
					"struct", t.Name(),
					"field", f.Name,
					"type", f.Type.String(),
				)
			}
		}

		// 递归进入嵌套结构体：viaValue 传播规则与 walkTypeUsage 一致
		// Ptr / Slice / Map 打断值嵌套链 → viaValue=false
		ft := f.Type
		switch ft.Kind() {
		case reflect.Ptr:
			checkUnsupportedDefaults(ft.Elem(), false, method, path, handlerName, handlerFile, handlerLine, visiting)
		case reflect.Struct:
			checkUnsupportedDefaults(ft, viaValue, method, path, handlerName, handlerFile, handlerLine, visiting)
		case reflect.Slice, reflect.Array:
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				checkUnsupportedDefaults(elem.Elem(), false, method, path, handlerName, handlerFile, handlerLine, visiting)
			} else {
				checkUnsupportedDefaults(elem, false, method, path, handlerName, handlerFile, handlerLine, visiting)
			}
		case reflect.Map:
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				checkUnsupportedDefaults(elem.Elem(), false, method, path, handlerName, handlerFile, handlerLine, visiting)
			} else {
				checkUnsupportedDefaults(elem, false, method, path, handlerName, handlerFile, handlerLine, visiting)
			}
		default:
			// Interface / Func / Chan / UnsafePointer 等无法反射出 struct，无需处理
		}
	}
}

// buildStructMeta 通过反射遍历结构体字段，预计算 structMeta 与 fieldMeta 列表。
// 注册阶段一次性完成，请求阶段直接复用 meta 避免重复反射。
// 预计算内容包括：字段名解析、文件字段判定、required/hasDefault 判定、时间格式预解析。
func buildStructMeta(t reflect.Type) structMeta {
	meta := structMeta{}
	if t.Kind() != reflect.Struct {
		return meta
	}

	// 判断 *T 是否实现 Validator（值接收者和指针接收者均可）
	meta.implementsValidator = reflect.PointerTo(t).Implements(validatorType)

	// 预分配切片容量，避免大结构体反复扩容
	meta.fields = make([]fieldMeta, 0, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		// 跳过未导出字段
		if f.PkgPath != "" {
			continue
		}
		// 跳过内嵌的 OpenAPIMeta
		if f.Anonymous && f.Type == metaType {
			continue
		}

		fm := fieldMeta{indices: []int{i}}
		fm.name = resolveFieldName(f)
		if fm.name == "" || fm.name == "-" {
			// 仍然记录字段，bindValues 中根据 name 跳过
			meta.fields = append(meta.fields, fm)
			continue
		}

		// 文件字段判定
		switch {
		case f.Type == fileHeaderPtrType:
			fm.isFile = true
		case f.Type.Kind() == reflect.Slice && f.Type.Elem() == fileHeaderPtrType:
			fm.isFileSlice = true
		case f.Type.Kind() == reflect.Slice:
			fm.isSlice = true
		}

		// required 判定：default 标签优先（带 default 的字段视为可选），
		// 其次检查 required:"true" 标签。预计算结果供运行时校验和 OpenAPI 生成直接复用。
		if _, ok := f.Tag.Lookup("default"); ok {
			if isDefaultSupported(f.Type) {
				fm.hasDefault = true
				fm.defaultVal = f.Tag.Get("default")
			} else {
				slog.Warn("default tag on unsupported type, ignored",
					"struct", t.Name(),
					"field", f.Name,
					"type", f.Type.String(),
				)
			}
		} else if v, ok := f.Tag.Lookup("required"); ok {
			if b, err := strconv.ParseBool(v); err == nil && b {
				fm.required = true
				meta.hasRequired = true
			}
		}

		// 时间相关标签（仅对 time.Time 类型解析）
		if f.Type == timeType {
			fm.timeFormat = f.Tag.Get("time_format")
			if locTag := f.Tag.Get("time_location"); locTag != "" {
				if loc, err := time.LoadLocation(locTag); err == nil {
					fm.timeLocation = loc
				} else {
					slog.Warn("invalid time_location tag, falling back to time.Local",
						"tag_value", locTag,
						"struct", t.Name(),
						"field", f.Name,
						"error", err,
					)
				}
			}
		}

		meta.fields = append(meta.fields, fm)
	}
	return meta
}

// applyDefaults 递归遍历结构体树，为带 default 标签的字段初始化默认值。
// 注册阶段（requestPhase=false）：对模板中的所有零值字段填充默认值。
// 请求阶段（requestPhase=true）：仅递归进入动态创建的子元素（切片/map/nested ptr），
// 并仅对 nil 指针字段填充默认值（值类型如 int/string 不填充，避免覆盖用户显式传入的零值）。
// meta 为注册阶段预计算的 structMeta，直接遍历其 fields 避免请求阶段反射。
func applyDefaults(reqPtr reflect.Value, meta structMeta, requestPhase ...bool) error {
	elem := reqPtr.Elem()
	if elem.Kind() != reflect.Struct {
		return nil
	}
	rp := len(requestPhase) > 0 && requestPhase[0]
	for i := range meta.fields {
		fm := &meta.fields[i]
		fv := fieldByIndex(elem, fm.indices)

		// 决定是否填充默认值：
		// - 注册阶段：所有零值字段均填充
		// - 请求阶段：仅填充 nil 指针字段，值类型零值可能来自用户显式传入，跳过
		var shouldFill bool
		if !rp {
			shouldFill = fm.hasDefault && fv.CanSet() && fv.IsZero()
		} else {
			shouldFill = fm.hasDefault && fv.CanSet() && fv.Kind() == reflect.Ptr && fv.IsNil()
		}
		if shouldFill {
			var values []string
			if fm.isSlice {
				// 切片默认值以逗号分隔，如 default:"a,b,c"；去除首尾逗号避免多余空元素
				values = strings.Split(strings.Trim(fm.defaultVal, ","), ",")
			} else {
				values = []string{strings.TrimSpace(fm.defaultVal)}
			}
			// 尽力填充，转换失败则跳过（time.Time 复用预解析的 time_format/时区）
			_ = setFieldValue(fv, values, fm.timeFormat, fm.timeLocation)
		}

		// 递归进入嵌套结构体/指针/切片/map 字段
		subV := fv
		if subV.Kind() == reflect.Ptr {
			if subV.IsNil() {
				continue
			}
			subV = subV.Elem()
		}
		if subV.Kind() == reflect.Struct {
			subMeta := buildStructMeta(subV.Type())
			_ = applyDefaults(subV.Addr(), subMeta, rp)
		} else if subV.Kind() == reflect.Slice {
			// 结构体切片/结构体指针切片：递归填充每个元素中带 default 标签的字段
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
					_ = applyDefaults(elem.Addr(), subMeta, rp)
				}
			}
		} else if subV.Kind() == reflect.Map {
			// map[K]Struct / map[K]*Struct：递归填充每个 value 中带 default 标签的字段
			valType := subV.Type().Elem()
			isPtrVal := valType.Kind() == reflect.Ptr
			if isPtrVal {
				valType = valType.Elem()
			}
			if valType.Kind() == reflect.Struct {
				subMeta := buildStructMeta(valType)
				for _, key := range subV.MapKeys() {
					val := subV.MapIndex(key)
					if isPtrVal {
						if val.IsNil() {
							continue
						}
						val = val.Elem()
					}
					// MapIndex 返回的值不可寻址，需复制一份再调用 applyDefaults
					valCopy := reflect.New(valType).Elem()
					valCopy.Set(val)
					_ = applyDefaults(valCopy.Addr(), subMeta, rp)
					// 写回 map：值类型用 SetMapIndex，指针类型通过 pointee 写回
					if isPtrVal {
						subV.MapIndex(key).Elem().Set(valCopy)
					} else {
						subV.SetMapIndex(key, valCopy)
					}
				}
			}
		}
	}
	return nil
}

// fieldByIndex 沿索引路径从根结构体出发定位到目标字段，中间若遇 nil 指针则自动初始化。
// 供绑定/校验/默认值路径使用，保证嵌套结构体中的字段可安全访问。
func fieldByIndex(v reflect.Value, indices []int) reflect.Value {
	for _, i := range indices {
		if v.Kind() == reflect.Ptr {
			if v.IsNil() {
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}

// deepCopyDefaults 递归深拷贝结构体树中所有指针/切片/map 字段，确保并发请求间不共享底层内存。
// 在 ServeHTTP 中，模板浅拷贝后调用此函数完成深拷贝。
func deepCopyDefaults(v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			deepCopyDefaults(v.Field(i))
		}
	case reflect.Ptr:
		if !v.IsNil() {
			newPtr := reflect.New(v.Type().Elem())
			newPtr.Elem().Set(v.Elem())
			v.Set(newPtr)
			// 递归拷贝指针指向的元素（struct/slice/map），而非指针本身，避免无限递归
			deepCopyDefaults(v.Elem())
		}
	case reflect.Slice:
		if !v.IsNil() {
			newSlice := reflect.MakeSlice(v.Type(), v.Len(), v.Cap())
			reflect.Copy(newSlice, v)
			v.Set(newSlice)
			for i := 0; i < v.Len(); i++ {
				deepCopyDefaults(v.Index(i))
			}
		}
	case reflect.Map:
		if !v.IsNil() {
			newMap := reflect.MakeMap(v.Type())
			for _, key := range v.MapKeys() {
				newMap.SetMapIndex(key, v.MapIndex(key))
			}
			v.Set(newMap)
		}
	default:
		return
	}
}

// hasRefFields 扫描结构体树，判断是否存在非 nil 的指针、切片或 map 字段。
// 用于注册阶段预计算 needsDeepCopy：若 defaultReq 中存在非 nil 引用字段，
// 则请求阶段需要深拷贝以断开共享。string/int 等不可变值类型不触发。
func hasRefFields(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if hasRefFields(v.Field(i)) {
				return true
			}
		}
		return false
	case reflect.Ptr:
		if v.IsNil() {
			return false
		}
		return true
	case reflect.Slice:
		return !v.IsNil()
	case reflect.Map:
		return !v.IsNil()
	default:
		return false
	}
}

// isDefaultSupported 判定字段类型是否支持 default 标签：仅标量类型及其切片支持
func isDefaultSupported(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	case reflect.Slice, reflect.Ptr:
		return isDefaultSupported(t.Elem())
	default:
		return false
	}
}
