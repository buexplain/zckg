package zchttp

import (
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// emptyField 供无字段上下文（如切片元素）复用，Tag 为空不会附加任何装饰信息
var emptyField reflect.StructField

// OpenAPIServer 描述一个服务端点
type OpenAPIServer struct {
	URL         string
	Description string
}

// OpenAPIInfo 文档元信息，由调用方在生成时提供
type OpenAPIInfo struct {
	Title           string
	Description     string
	Version         string
	Servers         []OpenAPIServer
	ResponseWrapper any // 自定义响应包装结构体样例；必须恰有一个带 json 名的 interface data 占位字段，非法配置 panic
}

// openAPIGenerator 承载一次生成过程中的可复用状态
type openAPIGenerator struct {
	schemas           map[string]any        // components/schemas
	typeNames         map[schemaKey]string  // 已注册的类型/角色/命名规则 → schema 名，避免重复与循环递归
	nameToType        map[string]schemaKey  // schema 名 → 注册键，用于命名去重
	reachedViaValue   map[reflect.Type]bool // struct 类型是否被"值嵌套"路径到达（含顶层；仅请求侧）
	reachedByDefaults map[reflect.Type]bool // struct 类型是否可被 applyDefaults 递归到达（仅请求侧）
	currentType       reflect.Type          // 当前正在生成 schema 的 struct 类型（decorate 据此判断上下文）
	responseWrapper   any                   // 自定义响应包装结构体样例
}

// schemaRole 区分同一 struct 类型参与的两种 schema 角色：
// 请求侧读取 default/nonzero/time_format 等绑定与校验约束标签，
// 响应侧只描述实际序列化产物，不读上述标签（响应无默认值填充、无校验，时间恒按 RFC3339 输出）。
type schemaRole uint8

const (
	roleRequest schemaRole = iota
	roleResponse
)

// responseSchemaSuffix 是响应角色 schema 名的固定后缀，
// 使同一类型同时用作 Req 与 Res 时，文档中两份 schema 可以被明确区分。
const responseSchemaSuffix = "Response"

// schemaKey 区分同一类型的四类 schema：请求/响应角色 × JSON 命名（encoding/json 规则）/ form 绑定名
type schemaKey struct {
	t    reflect.Type
	form bool
	role schemaRole
}

// GenerateOpenAPI 遍历路由表，通过反射生成 OpenAPI 3.0 文档（map 形式，可序列化为 JSON）。
// ResponseWrapper 非 nil 时必须是结构体或结构体指针，所有参与输出的导出字段必须有显式 json 名，
// 且必须恰有一个 interface 字段作为业务响应 data 占位符；配置非法时 panic。
func GenerateOpenAPI(r *Router, info OpenAPIInfo) map[string]any {
	validateResponseWrapper(info.ResponseWrapper)
	g := &openAPIGenerator{
		schemas:           map[string]any{},
		typeNames:         map[schemaKey]string{},
		nameToType:        map[string]schemaKey{},
		reachedViaValue:   map[reflect.Type]bool{},
		reachedByDefaults: map[reflect.Type]bool{},
		responseWrapper:   info.ResponseWrapper,
	}

	// Pass 1：收集请求侧所有 struct 类型的嵌套上下文（值嵌套 vs 指针嵌套）。
	// 值嵌套（顶层 + 值类型 struct 字段）→ 注册阶段 fill 所有类型；
	// 指针嵌套（*Struct）→ 注册阶段无法到达，仅请求阶段 fill nil 指针。
	// 此信息在 decorate 中判断值类型字段的 default 是否应展示（default 只在请求侧 schema 出现）。
	g.collectTypeUsages(r)

	// Pass 1b：收集请求侧所有 struct 类型是否可被 applyDefaults 递归到达。
	// 指针字段的 default 仅在所属 struct 可被 defaults 到达时才展示。
	g.collectDefaultsReachability(r)

	paths := map[string]any{}
	// 排序 method 与 path 保证输出确定性（不同包同名类型的 schema 序号稳定，快照/增量 diff 友好）
	sortedRoutes := make([]routeRecord, len(r.routes))
	copy(sortedRoutes, r.routes)
	sort.Slice(sortedRoutes, func(i, j int) bool {
		if sortedRoutes[i].method != sortedRoutes[j].method {
			return sortedRoutes[i].method < sortedRoutes[j].method
		}
		return sortedRoutes[i].path < sortedRoutes[j].path
	})
	for _, rec := range sortedRoutes {
		// 参数路由的 {name}/{name?}/{name...} 段声明为 path 参数并从 query 排除；
		// 静态路由无参数段，paramNames/pathParamNotes 为空集。
		// OpenAPI 无 {name?}/{name...} 语法，二者转换为 {name} 形式；
		// OpenAPI 3.0 要求 path 参数必须 required:true，运行时可选/通配语义写入 description。
		segments, perr := parseRoutePath(rec.path)
		if perr != nil {
			continue // 注册阶段已校验，防御性跳过
		}
		paramNames := make(map[string]bool)
		pathParamNotes := make(map[string]string)
		for _, seg := range segments {
			if !seg.isParam {
				continue
			}
			paramNames[seg.name] = true
			switch {
			case seg.optional:
				pathParamNotes[seg.name] = "运行时可选：省略该段时路由仍可命中；OpenAPI 3.0 要求 path 参数必须声明为 required。"
			case seg.catchAll:
				pathParamNotes[seg.name] = "运行时为通配尾段：捕获剩余路径（可为空）；OpenAPI 3.0 要求 path 参数必须声明为 required。"
			}
		}
		op := g.buildOperation(rec.method, rec.entry, paramNames, pathParamNotes)
		if op == nil {
			continue
		}
		// 路径模板转换为 OpenAPI 规范形式：{name?} → {name}、{name...} → {name}（参数名不含 "."，替换无误伤）
		openapiPath := strings.ReplaceAll(rec.path, "?}", "}")
		openapiPath = strings.ReplaceAll(openapiPath, "...}", "}")
		item, ok := paths[openapiPath].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[openapiPath] = item
		}
		item[strings.ToLower(rec.method)] = op
	}

	infoMap := map[string]any{
		"title":   info.Title,
		"version": info.Version,
	}
	if info.Description != "" {
		infoMap["description"] = info.Description
	}

	doc := map[string]any{
		"openapi": "3.0.3",
		"info":    infoMap,
		"paths":   paths,
	}
	if len(info.Servers) > 0 {
		servers := make([]any, 0, len(info.Servers))
		for _, s := range info.Servers {
			sv := map[string]any{"url": s.URL}
			if s.Description != "" {
				sv["description"] = s.Description
			}
			servers = append(servers, sv)
		}
		doc["servers"] = servers
	}
	if len(g.schemas) > 0 {
		doc["components"] = map[string]any{"schemas": g.schemas}
	}
	return doc
}

// collectTypeUsages 第一遍遍历：收集请求侧所有 struct 类型是否被"值嵌套"路径到达。
// 值嵌套（含顶层 Req 和值类型 struct 字段）→ 注册阶段 fill 所有类型；
// 指针嵌套（*Struct）或切片/map 元素 → 注册阶段无法到达，仅请求阶段 fill 指针类型。
// 此信息在 decorate 中判断值类型字段 default 是否应展示。
// 只遍历 Req：default 仅在请求角色 schema 中展示，响应不读该标签（见 schemaRole）。
func (g *openAPIGenerator) collectTypeUsages(r *Router) {
	for _, rec := range r.routes {
		entry := rec.entry
		if entry.reqType != nil {
			g.walkTypeUsage(derefType(entry.reqType), true, map[reflect.Type]bool{})
		}
	}
}

// walkTypeUsage 递归遍历类型树，标记 struct 类型的嵌套上下文。
// viaValue=true 表示从顶层到当前类型路径上所有 struct 均为值类型嵌套。
// visiting 用于检测自引用循环，防止无限递归。
func (g *openAPIGenerator) walkTypeUsage(t reflect.Type, viaValue bool, visiting map[reflect.Type]bool) {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return
	}
	if viaValue {
		g.reachedViaValue[t] = true
	}
	// 自引用循环检测：当前类型已在递归栈中，跳过
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)

	for _, et := range taggedEmbeddedStructs(t) {
		g.walkTypeUsage(et, viaValue, visiting)
	}
	meta := buildStructMeta(t)
	for _, fm := range meta.fields {
		f := fm.field
		ft := f.Type
		switch ft.Kind() {
		case reflect.Ptr:
			// *Struct：子结构体无法被注册阶段到达，传递 viaValue=false
			g.walkTypeUsage(ft.Elem(), false, visiting)
		case reflect.Struct:
			// 值类型嵌套 struct：继承父级上下文
			g.walkTypeUsage(ft, viaValue, visiting)
		case reflect.Slice, reflect.Array:
			// 切片/数组元素是动态创建的，注册阶段无法预填
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				g.walkTypeUsage(elem.Elem(), false, visiting)
			} else {
				g.walkTypeUsage(elem, false, visiting)
			}
		case reflect.Map:
			// map value 是动态创建的，注册阶段无法预填
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				g.walkTypeUsage(elem.Elem(), false, visiting)
			} else if elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array {
				// map 值类型为切片/数组：穿透到元素类型
				subElem := elem.Elem()
				if subElem.Kind() == reflect.Ptr {
					g.walkTypeUsage(subElem.Elem(), false, visiting)
				} else {
					g.walkTypeUsage(subElem, false, visiting)
				}
			} else {
				g.walkTypeUsage(elem, false, visiting)
			}
		default:
			// Interface / Func / Chan / UnsafePointer 等无法反射出 struct，无需处理
		}
	}
}

// collectDefaultsReachability 收集请求侧所有 struct 类型是否可被 applyDefaults 递归到达。
// 与 collectTypeUsages 的区别：
//   - collectTypeUsages 追踪"值嵌套"可达性（viaValue），仅值类型 default 展示依赖它
//   - collectDefaultsReachability 追踪"默认值填充"可达性（viaDefaults），指针字段 default 展示依赖它
//
// 可达性规则（与 applyDefaultsWithVisiting 一致）：
//   - 顶层 Req：可达
//   - 值类型 struct 字段：继承父级可达性
//   - 指针字段（*Struct）：继承父级可达性（applyDefaults 可跟随非 nil 指针）
//   - 指针包裹容器（*[]Struct / *[N]Struct / *map[K]Struct）：按解包后的容器处理
//   - 切片/数组元素：继承父级可达性（applyDefaults 可遍历切片元素）
//   - map 值类型为 Struct 或 *Struct：继承父级可达性
//   - map 值类型为 Slice/Array：不可达（applyDefaults 无法穿透此类多层容器）
//
// 只遍历 Req：default 只在请求角色 schema 中展示（Res 不执行默认值填充，也不读该标签）。
func (g *openAPIGenerator) collectDefaultsReachability(r *Router) {
	for _, rec := range r.routes {
		entry := rec.entry
		if entry.reqType != nil {
			g.walkDefaultsReachability(derefType(entry.reqType), true, map[reflect.Type]bool{})
		}
	}
}

// walkDefaultsReachability 递归遍历类型树，标记 struct 类型是否可被 applyDefaults 到达。
// viaDefaults 表示从顶层到当前类型路径上是否所有边都可通过 applyDefaults 穿透。
// visiting 用于检测自引用循环，防止无限递归。
func (g *openAPIGenerator) walkDefaultsReachability(t reflect.Type, viaDefaults bool, visiting map[reflect.Type]bool) {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return
	}
	if viaDefaults {
		g.reachedByDefaults[t] = true
	}
	// 自引用循环检测
	if visiting[t] {
		return
	}
	visiting[t] = true
	defer delete(visiting, t)

	for _, et := range taggedEmbeddedStructs(t) {
		g.walkDefaultsReachability(et, viaDefaults, visiting)
	}
	meta := buildStructMeta(t)
	for _, fm := range meta.fields {
		f := fm.field
		ft := f.Type
		// 指针包裹容器需先解包，否则只接受 struct 的递归入口会漏掉容器元素。
		if ft.Kind() == reflect.Ptr {
			switch ft.Elem().Kind() {
			case reflect.Slice, reflect.Array, reflect.Map:
				ft = ft.Elem()
			}
		}
		switch ft.Kind() {
		case reflect.Ptr:
			// *Struct：applyDefaults 可跟随非 nil 指针，viaDefaults 不变
			g.walkDefaultsReachability(ft.Elem(), viaDefaults, visiting)
		case reflect.Struct:
			// 值类型嵌套 struct：继承父级上下文
			g.walkDefaultsReachability(ft, viaDefaults, visiting)
		case reflect.Slice, reflect.Array:
			// 切片/数组元素：applyDefaults 可遍历，viaDefaults 不变
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				g.walkDefaultsReachability(elem.Elem(), viaDefaults, visiting)
			} else {
				g.walkDefaultsReachability(elem, viaDefaults, visiting)
			}
		case reflect.Map:
			// map 值类型为 Struct 或 *Struct：applyDefaults 可穿透，viaDefaults 不变
			// map 值类型为 Slice/Array：applyDefaults 无法穿透，viaDefaults=false
			elem := ft.Elem()
			if elem.Kind() == reflect.Ptr {
				g.walkDefaultsReachability(elem.Elem(), viaDefaults, visiting)
			} else if elem.Kind() == reflect.Slice || elem.Kind() == reflect.Array {
				subElem := elem.Elem()
				if subElem.Kind() == reflect.Ptr {
					g.walkDefaultsReachability(subElem.Elem(), false, visiting)
				} else {
					g.walkDefaultsReachability(subElem, false, visiting)
				}
			} else {
				g.walkDefaultsReachability(elem, viaDefaults, visiting)
			}
		default:
			// Interface / Func / Chan / UnsafePointer 等无法反射出 struct，无需处理
		}
	}
}

// buildOperation 构造单个操作对象（parameters/requestBody/responses/tags/summary/deprecated）。
// paramNames 为路径模板中的 {name}/{name?}/{name...} 参数名集合（无参数段的路由为空集）：所有方法均声明
// path 参数（含被 ignore 的字段——path 声明受规范强制，见 buildPathParams），
// GET/DELETE/HEAD 不再重复展示为 query；pathParamNotes 为可选参数与通配尾段的运行时语义说明。
// Req 嵌入 KeepRawBody 时与 GET 同样只绑定 query：其余方法的非路径字段输出为 query 参数，
// requestBody 输出通用声明 {"content": {"*/*": {"schema": {}}}}（接受任意请求体、原样处理，不从字段推导）。
func (g *openAPIGenerator) buildOperation(method string, entry *routeEntry, paramNames map[string]bool, pathParamNotes map[string]string) map[string]any {
	// 使用注册阶段预计算的类型信息，避免重复反射
	if entry.reqType == nil || entry.resType == nil {
		return nil
	}
	reqType := derefType(entry.reqType)
	resType := derefType(entry.resType)

	opMeta := entry.opMeta

	op := map[string]any{}
	if len(opMeta.tags) > 0 {
		tags := make([]any, len(opMeta.tags))
		for i, t := range opMeta.tags {
			tags[i] = t
		}
		op["tags"] = tags
	}
	if opMeta.summary != "" {
		op["summary"] = opMeta.summary
	} else {
		op["summary"] = shortFuncName(entry.handlerName)
	}
	if opMeta.description != "" {
		op["description"] = opMeta.description
	}
	if opMeta.deprecated {
		op["deprecated"] = true
	}

	var params []any
	if len(paramNames) > 0 {
		params = append(params, g.buildPathParams(reqType, entry.reqMeta, paramNames, pathParamNotes)...)
	}
	switch method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		params = append(params, g.buildQueryParams(reqType, entry.reqMeta, paramNames)...)
	default:
		if entry.keepRawBody {
			params = append(params, g.buildQueryParams(reqType, entry.reqMeta, paramNames)...)
			op["requestBody"] = map[string]any{
				"content": map[string]any{"*/*": map[string]any{"schema": map[string]any{}}},
			}
		} else if body := g.buildRequestBody(reqType, entry.reqMeta, requestBodyRequired(entry, paramNames)); body != nil {
			op["requestBody"] = body
		}
	}
	if len(params) > 0 {
		op["parameters"] = params
	}

	op["responses"] = g.buildResponses(resType)
	return op
}

// buildQueryParams 为 GET/DELETE/HEAD 生成 query 参数列表。
// meta 为注册阶段预计算的 structMeta，直接使用其字段名和 nonzero+default 判定，避免重复反射遍历。
// paramNames 为路径参数名集合：绑定到路径参数的字段不作为 query 参数展示。
// 仅保留 query 绑定实际可写入的类型（标量、标量指针与标量切片），跳过数组、map、
// 嵌套结构体、多层容器与指针包裹的容器；展示无法绑定的参数会误导 API 使用者；
// 文件字段一并跳过（GET 无 multipart）。
// 带 deprecated:"true" 的字段在 Parameter Object 顶层输出 deprecated: true。
func (g *openAPIGenerator) buildQueryParams(reqType reflect.Type, meta structMeta, paramNames map[string]bool) []any {
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	// 设置当前类型上下文（与 registerStructSchema 一致），
	// 否则 decorate 无法判定顶层 Req 的值类型字段 default 是否应展示
	prevType := g.currentType
	g.currentType = reqType
	defer func() { g.currentType = prevType }()
	var params []any
	for _, fm := range meta.fields {
		if fm.name == "" || fm.name == "-" {
			continue
		}
		if paramNames[fm.name] {
			continue // 绑定到路径参数的字段，声明为 path 参数而非 query
		}
		f := fm.field
		if isIgnored(f) {
			continue
		}
		if fm.isFile || fm.isFileSlice {
			continue // 文件字段无法经 query 绑定（GET 无 multipart）
		}
		if !isQueryBindableType(f.Type) {
			continue // 仅标量与标量切片可绑定；数组/容器/嵌套结构体展示会误导调用方
		}
		param := map[string]any{
			"name":     fm.name,
			"in":       "query",
			"required": fm.nonzero && !fm.hasDefault,
			"schema":   g.typeToSchema(f.Type, f, 0, roleRequest),
		}
		if isDeprecated(f) {
			param["deprecated"] = true
		}
		params = append(params, param)
	}
	return params
}

// buildPathParams 为所有方法的参数路由 {name}/{name?}/{name...} 段声明 path 参数。
// path 声明受 OpenAPI 规范强制（每个模板表达式都必须有对应参数），因此**不受 ignore 影响**：
// 被 ignore 的路径字段仍声明为 path 参数，只是不再出现在 body schema 与 query 中，
// 从而避免生成"占位符无对应声明"的非法文档。
// meta 为注册阶段预计算的 structMeta；paramNames 为路径模板中的参数名集合；
// pathParamNotes 为可选参数（{name?}）与通配尾段（{name...}）的运行时语义说明。
// OpenAPI 3.0 强制 path 参数 required:true，因此所有 path 参数一律输出 required:true，
// 可省略语义只能通过 description 表达（生成文档仍可被规范校验器接受）。
// 带 deprecated:"true" 的字段在 Parameter Object 顶层输出 deprecated: true。
func (g *openAPIGenerator) buildPathParams(reqType reflect.Type, meta structMeta, paramNames map[string]bool, pathParamNotes map[string]string) []any {
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	// 设置当前类型上下文（与 registerStructSchema 一致），保证 decorate 判定正确
	prevType := g.currentType
	g.currentType = reqType
	defer func() { g.currentType = prevType }()
	var params []any
	for _, fm := range meta.fields {
		if !paramNames[fm.name] {
			continue
		}
		f := fm.field
		param := map[string]any{
			"name":     fm.name,
			"in":       "path",
			"required": true,
			"schema":   g.typeToSchema(f.Type, f, 0, roleRequest),
		}
		if note := pathParamNotes[fm.name]; note != "" {
			param["description"] = note
		}
		if isDeprecated(f) {
			param["deprecated"] = true
		}
		params = append(params, param)
	}
	return params
}

// requestBodyRequired 判断省略 body 且没有 query 值时，默认 Req 模板能否通过内建 nonzero 校验。
// 路径参数由路由匹配单独提供，不构成 body 必填依据；自定义 Validator 属任意业务代码，不静态推导。
func requestBodyRequired(entry *routeEntry, paramNames map[string]bool) bool {
	if !entry.needsNonzeroValidation {
		return false
	}
	meta := entry.reqMeta
	if len(paramNames) > 0 {
		fields := make([]fieldMeta, 0, len(meta.fields))
		for _, fm := range meta.fields {
			if !paramNames[fm.name] {
				fields = append(fields, fm)
			}
		}
		meta.fields = fields
	}
	reqPtr := reflect.New(entry.reqElemType)
	reqPtr.Elem().Set(entry.defaultReq)
	return validateNonzero(reqPtr, meta) != nil
}

// buildRequestBody 为携带请求体的方法生成 requestBody；含文件字段时使用 multipart/form-data。
// application/json 的 schema 按 encoding/json 字段规则命名（与运行时 JSON 解码一致）；
// multipart/form-data 的顶层 schema 按 form 优先绑定名命名（与表单绑定一致）。
// required 表示省略 body 时默认 Req 模板无法通过内建 nonzero 校验。
// 请求体恒为请求角色：default/nonzero/time_format 等约束标签在此生效。
func (g *openAPIGenerator) buildRequestBody(reqType reflect.Type, meta structMeta, required bool) map[string]any {
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	contentType := "application/json"
	form := hasFileField(meta)
	if form {
		contentType = "multipart/form-data"
	}
	return map[string]any{
		"required": required,
		"content": map[string]any{
			contentType: map[string]any{
				"schema": g.registerRequestSchema(reqType, meta, form),
			},
		},
	}
}

// buildResponses 生成响应对象：成功统一返回 200（与 HttpEngine 默认响应行为一致）；响应体统一包装为 Response 结构。
func (g *openAPIGenerator) buildResponses(resType reflect.Type) map[string]any {
	return map[string]any{
		"200": map[string]any{
			"description": "成功",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": g.wrapResponseSchema(resType),
				},
			},
		},
	}
}

// wrapResponseSchema 将 Res 结构体包装进统一的响应结构。
// 若设置了 responseWrapper，则通过反射推导自定义包装 schema；否则使用默认 Response{data,code,message}。
// Res 以响应角色注册：只描述序列化产物，不读取 default/nonzero/time_format 等请求侧标签。
func (g *openAPIGenerator) wrapResponseSchema(resType reflect.Type) map[string]any {
	rt := derefType(resType)
	dataSchema := g.registerResponseSchema(rt)
	resName := g.typeNames[schemaKey{t: rt, role: roleResponse}]
	if resName == "" {
		resName = "AnonymousStruct"
	}
	// 包装层沿用文档约定的 Response_<Type>：从响应数据 schema 名去掉固定的角色后缀
	wrapName := "Response_" + strings.TrimSuffix(resName, responseSchemaSuffix)
	if _, ok := g.schemas[wrapName]; !ok {
		g.schemas[wrapName] = g.buildWrapperProperties(dataSchema)
	}
	return refSchema(wrapName)
}

func validateResponseWrapper(wrapper any) reflect.Type {
	if wrapper == nil {
		return nil
	}
	wt := derefType(reflect.TypeOf(wrapper))
	if wt.Kind() != reflect.Struct {
		panic(fmt.Sprintf("zchttp: OpenAPI ResponseWrapper must be a struct or pointer to struct, got %T", wrapper))
	}
	placeholders := 0
	for i := 0; i < wt.NumField(); i++ {
		f := wt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		jsonName := parseJSONName(f.Tag.Get("json"))
		if jsonName == "-" {
			continue
		}
		if jsonName == "" {
			panic(fmt.Sprintf("zchttp: OpenAPI ResponseWrapper field %s.%s must declare a non-empty json name or json:\"-\"", wt.Name(), f.Name))
		}
		if f.Type.Kind() == reflect.Interface {
			placeholders++
		}
	}
	if placeholders != 1 {
		panic(fmt.Sprintf("zchttp: OpenAPI ResponseWrapper %s must contain exactly one exported interface data placeholder with a valid json name, got %d", wt.String(), placeholders))
	}
	return wt
}

// buildWrapperProperties 根据 responseWrapper 构建响应包装的 schema properties。
// nil 使用默认 {data, code, message}；自定义结构体已通过 validateResponseWrapper 校验。
func (g *openAPIGenerator) buildWrapperProperties(dataSchema map[string]any) map[string]any {
	if g.responseWrapper == nil {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data":    dataSchema,
				"code":    map[string]any{"type": "integer"},
				"message": map[string]any{"type": "string"},
			},
		}
	}

	wt := validateResponseWrapper(g.responseWrapper)
	props := map[string]any{}
	for i := 0; i < wt.NumField(); i++ {
		f := wt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		jsonName := parseJSONName(f.Tag.Get("json"))
		if jsonName == "-" {
			continue
		}
		if f.Type.Kind() == reflect.Interface {
			props[jsonName] = dataSchema
		} else {
			props[jsonName] = g.typeToSchema(f.Type, f, 0, roleResponse)
		}
	}
	return map[string]any{
		"type":       "object",
		"properties": props,
	}
}

// parseJSONName 从 json 标签中提取字段名（去掉 omitempty 等选项）
func parseJSONName(tag string) string {
	if tag == "" {
		return ""
	}
	if idx := strings.Index(tag, ","); idx != -1 {
		return tag[:idx]
	}
	return tag
}

// registerRequestSchema 以请求角色注册结构体 schema：属性名按 form（multipart 顶层请求体）
// 或 encoding/json 规则生成，default/nonzero/time_format 等请求侧标签全部生效。
// meta 为注册阶段预计算的 structMeta（routeEntry 或 buildStructMeta 现算），提供 nonzero+default 判定。
func (g *openAPIGenerator) registerRequestSchema(t reflect.Type, meta structMeta, form bool) map[string]any {
	return g.registerStructSchema(t, meta, form, roleRequest)
}

// registerResponseSchema 以响应角色注册结构体 schema：只描述实际序列化产物，
// 键名按 encoding/json 规则，不读取 default/nonzero/time_format（响应无默认值填充、无校验、
// 时间恒由 encoding/json 按 RFC3339 输出），故不需要 structMeta。
func (g *openAPIGenerator) registerResponseSchema(t reflect.Type) map[string]any {
	return g.registerStructSchema(t, structMeta{}, false, roleResponse)
}

// schemaBaseName 返回类型在指定角色下的 schema 基础名：请求角色用类型名，
// 响应角色固定追加 "Response" 后缀，使同一类型的两份 schema 在文档中可区分。
func schemaBaseName(t reflect.Type, role schemaRole) string {
	base := t.Name()
	if base == "" {
		base = "AnonymousStruct"
	}
	if role == roleResponse {
		base += responseSchemaSuffix
	}
	return base
}

// registerStructSchema 将结构体注册到 components/schemas 并返回 $ref 引用。
// form=false 时属性集合与名称按 encoding/json 规则（jsonFields）生成，用于 application/json 请求体、
// 响应与嵌套结构体；form=true 时按 meta 的 form 优先绑定名生成，仅用于 multipart/form-data 顶层请求体
// （form 与 roleResponse 不会同时出现）。
// meta 仅在请求角色下被读取（用于推断 required）：响应角色的调用方传空 structMeta。
func (g *openAPIGenerator) registerStructSchema(t reflect.Type, meta structMeta, form bool, role schemaRole) map[string]any {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return map[string]any{"type": "object"}
	}
	key := schemaKey{t: t, form: form, role: role}
	if name, ok := g.typeNames[key]; ok {
		return refSchema(name)
	}
	name := g.uniqueName(key, schemaBaseName(t, role))
	g.typeNames[key] = name

	obj := map[string]any{"type": "object"}
	g.schemas[name] = obj // 先占位，防止循环递归

	// 设置当前类型上下文，decorate 据此判断值类型 default 是否应展示
	prevType := g.currentType
	g.currentType = t
	defer func() { g.currentType = prevType }()

	// required 仅请求角色输出：nonzero 是请求侧的运行时校验约束，响应无对应语义。
	// 以 nonzero 校验实际覆盖的字段（按索引路径对齐）为准。
	//
	// requiredEmbedPrefixes 记录“扁平化路径上存在无法由默认值满足的 nonzero 字段”的匿名嵌入
	// 索引前缀。带 json 名的匿名嵌入在 JSON schema 中是嵌套对象：客户端省略整个对象时，
	// 运行时的扁平校验仍会检查其子字段（匿名嵌入没有父字段判零环节），因此父级必须把
	// 该嵌入属性声明为 required，否则生成的文档允许省略、运行期却返回 400。
	requiredAt := map[string]bool{}
	requiredEmbedPrefixes := map[string]bool{}
	if role == roleRequest {
		for _, fm := range meta.fields {
			if fm.name == "" || fm.name == "-" || !fm.nonzero || fm.hasDefault {
				continue
			}
			requiredAt[fmt.Sprint(fm.indices)] = true
			for n := 1; n < len(fm.indices); n++ {
				requiredEmbedPrefixes[fmt.Sprint(fm.indices[:n])] = true
			}
		}
	}

	props := map[string]any{}
	var required []any
	if form {
		for _, fm := range meta.fields {
			if fm.name == "" || fm.name == "-" {
				continue
			}
			f := fm.field
			if isIgnored(f) {
				continue
			}
			props[fm.name] = g.typeToSchema(f.Type, f, 0, role)
			if requiredAt[fmt.Sprint(fm.indices)] {
				required = append(required, fm.name)
			}
		}
	} else {
		for _, jf := range jsonFields(t) {
			if isIgnored(jf.field) {
				continue
			}
			props[jf.name] = g.typeToSchema(jf.field.Type, jf.field, 0, role)
			if requiredAt[fmt.Sprint(jf.index)] {
				required = append(required, jf.name)
			} else if jf.field.Anonymous && isStructLike(jf.field.Type) && requiredEmbedPrefixes[fmt.Sprint(jf.index)] {
				required = append(required, jf.name)
			}
		}
	}
	obj["properties"] = props
	if len(required) > 0 {
		obj["required"] = required
	}
	return refSchema(name)
}

// jsonField 是 encoding/json 对结构体可见的一个字段
type jsonField struct {
	name   string
	tagged bool
	index  []int
	field  reflect.StructField
}

// jsonFields 按 encoding/json 的字段选择规则列出 t 的 JSON 字段：json 标签名优先（非法标签名回退字段名），
// json:"-" 排除、json:"-," 名为 "-"；无 json 名的匿名 struct 嵌入展开，带 json 名的嵌入作为普通字段；
// 同名字段取嵌入深度最浅者，同深度时带标签者胜出，仍并列则全部丢弃。
// 与 encoding/json 的差异：未导出的匿名指针嵌入不展开（其 nil 指针在解码时报错，框架元数据同样跳过），
// 框架的 OpenAPIMeta 嵌入不输出。
func jsonFields(t reflect.Type) []jsonField {
	type pending struct {
		typ   reflect.Type
		index []int
	}
	var fields []jsonField
	next := []pending{{typ: t}}
	var count, nextCount map[reflect.Type]int
	visited := map[reflect.Type]bool{}
	for len(next) > 0 {
		current := next
		next = nil
		count, nextCount = nextCount, map[reflect.Type]int{}
		for _, p := range current {
			if visited[p.typ] {
				continue
			}
			visited[p.typ] = true
			for i := 0; i < p.typ.NumField(); i++ {
				sf := p.typ.Field(i)
				if sf.Anonymous {
					if sf.Type == metaType {
						continue
					}
					et := sf.Type
					if et.Kind() == reflect.Ptr {
						if !sf.IsExported() {
							continue
						}
						et = et.Elem()
					}
					if !sf.IsExported() && et.Kind() != reflect.Struct {
						continue
					}
				} else if !sf.IsExported() {
					continue
				}
				tag := sf.Tag.Get("json")
				if tag == "-" {
					continue
				}
				name := tagName(tag)
				if !isValidJSONTag(name) {
					name = ""
				}
				index := make([]int, len(p.index)+1)
				copy(index, p.index)
				index[len(p.index)] = i
				ft := sf.Type
				if ft.Name() == "" && ft.Kind() == reflect.Ptr {
					ft = ft.Elem()
				}
				if name != "" || !sf.Anonymous || ft.Kind() != reflect.Struct {
					jf := jsonField{name: name, tagged: name != "", index: index, field: sf}
					if jf.name == "" {
						jf.name = sf.Name
					}
					fields = append(fields, jf)
					if count[p.typ] > 1 {
						// 同一嵌入类型在本层出现多次：追加副本，使下方的同名消解将其判为冲突
						fields = append(fields, jf)
					}
					continue
				}
				nextCount[ft]++
				if nextCount[ft] == 1 {
					next = append(next, pending{typ: ft, index: index})
				}
			}
		}
	}

	sort.Slice(fields, func(i, j int) bool {
		x := fields
		if x[i].name != x[j].name {
			return x[i].name < x[j].name
		}
		if len(x[i].index) != len(x[j].index) {
			return len(x[i].index) < len(x[j].index)
		}
		if x[i].tagged != x[j].tagged {
			return x[i].tagged
		}
		return indexLess(x[i].index, x[j].index)
	})
	out := fields[:0]
	for advance, i := 0, 0; i < len(fields); i += advance {
		for advance = 1; i+advance < len(fields); advance++ {
			if fields[i+advance].name != fields[i].name {
				break
			}
		}
		group := fields[i : i+advance]
		if len(group) > 1 && len(group[0].index) == len(group[1].index) && group[0].tagged == group[1].tagged {
			continue
		}
		out = append(out, group[0])
	}
	sort.Slice(out, func(i, j int) bool { return indexLess(out[i].index, out[j].index) })
	return out
}

// taggedEmbeddedStructs 返回 t 中带合法 json 名的匿名 struct 嵌入类型。encoding/json 把它们作为具名嵌套对象
// （schema 中为独立 $ref），框架元数据却把其字段扁平展开到父级并随父级填充默认值，故可达性与父级一致。
func taggedEmbeddedStructs(t reflect.Type) []reflect.Type {
	var out []reflect.Type
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.Anonymous || sf.Type == metaType || (!sf.IsExported() && sf.Type.Kind() == reflect.Ptr) {
			continue
		}
		tag := sf.Tag.Get("json")
		if tag == "-" || !isValidJSONTag(tagName(tag)) {
			continue
		}
		if et := derefType(sf.Type); et.Kind() == reflect.Struct {
			out = append(out, et)
		}
	}
	return out
}

// indexLess 按字段索引路径的字典序比较，得到与结构体声明一致的顺序
func indexLess(a, b []int) bool {
	for k := 0; k < len(a) && k < len(b); k++ {
		if a[k] != b[k] {
			return a[k] < b[k]
		}
	}
	return len(a) < len(b)
}

// isValidJSONTag 与 encoding/json 的标签名合法性判定一致，非法名回退为字段名
func isValidJSONTag(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		switch {
		case strings.ContainsRune("!#$%&()*+-./:;<=>?@[]^_{|}~ ", c):
		case !unicode.IsLetter(c) && !unicode.IsDigit(c):
			return false
		}
	}
	return true
}

// typeToSchema 将 Go 类型映射为 JSON Schema；field 提供 default/example/description/deprecated/time_format 等信息。
// role 决定标签生效范围：请求角色读取全部约束标签，响应角色只读纯文档标签（见 schemaRole）。
// depth 为 Ptr/Slice/Map 链的累计嵌套深度：防自引用命名类型（type S []S / type P *P /
// type A *B 与 type B *A 等）在容器/指针分支无限递归，超限退化为空 schema（与
// default 分支一致）；上限复用 maxPtrDerefDepth，与 derefType/isDefaultSupportedDepth/
// setScalar 的防环哲学一致（修复 REC-04）。Struct 分支经 registerStructSchema
// 占位机制防环，不消耗 depth。
func (g *openAPIGenerator) typeToSchema(t reflect.Type, field reflect.StructField, depth int, role schemaRole) map[string]any {
	if depth > maxPtrDerefDepth {
		return map[string]any{}
	}
	if t.Kind() == reflect.Ptr {
		if t == fileHeaderPtrType {
			return g.decorate(map[string]any{"type": "string", "format": "binary"}, field, role)
		}
		inner := g.typeToSchema(t.Elem(), field, depth+1, role)
		if _, isRef := inner["$ref"]; isRef {
			return g.decorate(map[string]any{"nullable": true, "allOf": []any{inner}}, field, role)
		}
		inner["nullable"] = true
		return g.decorate(inner, field, role)
	}

	if t == timeType {
		return g.timeSchema(field, role)
	}

	switch t.Kind() {
	case reflect.String:
		return g.decorate(map[string]any{"type": "string"}, field, role)
	case reflect.Bool:
		return g.decorate(map[string]any{"type": "boolean"}, field, role)
	case reflect.Int, reflect.Int64:
		return g.decorate(map[string]any{"type": "integer", "format": "int64"}, field, role)
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return g.decorate(map[string]any{"type": "integer", "format": "int32"}, field, role)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return g.decorate(map[string]any{"type": "integer", "format": "int64", "minimum": 0}, field, role)
	case reflect.Float32:
		return g.decorate(map[string]any{"type": "number", "format": "float"}, field, role)
	case reflect.Float64:
		return g.decorate(map[string]any{"type": "number", "format": "double"}, field, role)
	case reflect.Slice, reflect.Array:
		if t.Elem() == fileHeaderPtrType {
			return g.decorate(map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "format": "binary"},
			}, field, role)
		}
		schema := map[string]any{
			"type":  "array",
			"items": g.containerElementSchema(t.Elem(), field, depth, role),
		}
		// nil slice 经 encoding/json 序列化为 null；固定数组没有 nil 形态，不加 nullable
		if t.Kind() == reflect.Slice {
			schema["nullable"] = true
		}
		return g.decorate(schema, field, role)
	case reflect.Struct:
		if role == roleResponse {
			return g.decorate(g.registerResponseSchema(t), field, role)
		}
		return g.decorate(g.registerRequestSchema(t, buildStructMeta(t), false), field, role)
	case reflect.Map:
		valueSchema := g.containerElementSchema(t.Elem(), field, depth, role)
		schema := map[string]any{
			"type":                 "object",
			"additionalProperties": valueSchema,
			// nil map 经 encoding/json 序列化为 null
			"nullable": true,
		}
		result := g.decorate(schema, field, role)
		// 非 string key 无法在标准 OpenAPI 3.0 中表达，追加到 description 中
		if t.Key().Kind() != reflect.String {
			keyNote := " (key type: " + t.Key().Kind().String() + ")"
			if d, ok := result["description"]; ok {
				result["description"] = d.(string) + keyNote
			} else {
				result["description"] = "key type: " + t.Key().Kind().String()
			}
		}
		return result
	default:
		return map[string]any{}
	}
}

// containerElementSchema 生成容器（切片/数组/map）元素的 schema。
// time.Time 元素复用容器字段自身的 time_format 标签：绑定与默认值填充都按元素逐个解析，
// schema 必须与运行时一致，否则会出现"文档写 date-time、运行时按时间戳解析"的偏离。
// 元素不带字段级装饰（description/example/default 等只作用于容器字段自身），
// 其余元素类型走常规递归。
func (g *openAPIGenerator) containerElementSchema(elem reflect.Type, field reflect.StructField, depth int, role schemaRole) map[string]any {
	base := derefType(elem)
	if base == timeType {
		items := g.timeSchemaForTag(field.Tag.Get("time_format"), role)
		if elem.Kind() == reflect.Ptr {
			items["nullable"] = true
		}
		return items
	}
	return g.typeToSchema(elem, emptyField, depth+1, role)
}

// timeSchema 依据 time_format 标签决定 time.Time 的 schema 表现（时间戳→integer，其余→date-time），
// 并附加字段级装饰。
// 响应角色恒为 date-time：响应由 encoding/json 序列化，time.Time 一律输出 RFC3339 字符串，
// time_format 不参与输出，读它只会生成与实际响应不符的文档。
func (g *openAPIGenerator) timeSchema(field reflect.StructField, role schemaRole) map[string]any {
	return g.decorate(g.timeSchemaForTag(field.Tag.Get("time_format"), role), field, role)
}

// timeSchemaForTag 按 time_format 标签值生成 time.Time 的 schema（不含字段级装饰）。
func (g *openAPIGenerator) timeSchemaForTag(format string, role schemaRole) map[string]any {
	if role == roleRequest {
		switch format {
		case "unix", "unixmilli", "unixmicro", "unixnano":
			return map[string]any{"type": "integer", "format": "int64"}
		}
	}
	return map[string]any{"type": "string", "format": "date-time"}
}

// decorate 依据字段标签补充 default / example / description / deprecated（后三者为纯文档标签，两个角色都读）。
// default 只在请求角色展示（响应由服务端产出，不存在"缺省值"语义；applyDefaults 也仅作用于 Req），
// 展示规则遵循两阶段填充语义：
//   - 注册阶段：模板预填所有零值字段，但仅限"值嵌套"路径（顶层 + 值类型 struct 字段）能到达的 struct
//   - 请求阶段（post-bind）：仅 nil 指针字段被填充，值类型跳过
//
// 因此：
//   - 指针类型（*int/*string 等）：仅当所属 struct 可被 applyDefaults 到达时展示 default
//     （reachedByDefaults 追踪，与 applyDefaultsWithVisiting 可达性一致）
//   - 值类型（int/string 等）：仅当所属 struct 被"值嵌套"到达时才展示 default（注册阶段可靠）
//
// deprecated 为纯文档标签，不查可达性，无条件写入字段自身生成的 schema 节点。
// 嵌套 struct 的 $ref 会先经 decorate 再包装（值嵌套为 $ref 兄弟键、指针嵌套放入 allOf[0]），
// 故同一批标签在两处各出现一份副本；渲染层忽略 $ref 副本，生效的始终是最外层键。
func (g *openAPIGenerator) decorate(schema map[string]any, field reflect.StructField, role schemaRole) map[string]any {
	if field.Tag == "" {
		return schema
	}
	if role == roleRequest {
		if def, ok := field.Tag.Lookup("default"); ok && isDefaultSupported(field.Type) {
			// 指针类型：请求阶段 fill nil 指针 → 仅当所属 struct 可被 defaults 到达时展示
			// 值类型：仅当所属 struct 被值嵌套到达（注册阶段 fill）才展示
			showDefault := false
			if field.Type.Kind() == reflect.Ptr {
				showDefault = g.reachedByDefaults[g.currentType]
			} else {
				showDefault = g.reachedViaValue[g.currentType]
			}
			if showDefault {
				schema["default"] = coerceExample(schema, def)
			}
		}
	}
	if ex := field.Tag.Get("example"); ex != "" {
		schema["example"] = coerceExample(schema, ex)
	}
	if d := field.Tag.Get("description"); d != "" {
		schema["description"] = d
	}
	if isDeprecated(field) {
		schema["deprecated"] = true
	}
	return schema
}

// coerceExample 依据 schema 类型把字符串标签值转换为对应的 JSON 类型，使输出更规范
func coerceExample(schema map[string]any, raw string) any {
	switch schema["type"] {
	case "integer":
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return n
		}
	case "number":
		if f, err := strconv.ParseFloat(raw, 64); err == nil {
			return f
		}
	case "boolean":
		if b, err := strconv.ParseBool(raw); err == nil {
			return b
		}
	case "array":
		// 切片默认值以逗号分隔（如 default:"a,b"），逐元素按 items 类型递归转换；
		// Trim 后为空（default:""、default:",,,"）视为空切片，与运行时填充语义一致
		trimmed := strings.Trim(strings.TrimSpace(raw), ",")
		if trimmed == "" {
			return []any{}
		}
		items, _ := schema["items"].(map[string]any)
		parts := strings.Split(trimmed, ",")
		arr := make([]any, 0, len(parts))
		for _, p := range parts {
			arr = append(arr, coerceExample(items, strings.TrimSpace(p)))
		}
		return arr
	}
	return raw
}

// isIgnored 判定字段是否通过 ignore 标签从文档中排除（body schema 与 query）。
// path 参数声明不受其影响：规范要求每个路径模板表达式都有对应参数，见 buildPathParams。
func isIgnored(field reflect.StructField) bool {
	if v := field.Tag.Get("ignore"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return false
}

// isDeprecated 判定字段是否通过 deprecated 标签标记为已废弃
func isDeprecated(field reflect.StructField) bool {
	if v := field.Tag.Get("deprecated"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return false
}

// hasFileField 判断结构体元信息中是否含上传文件字段（决定请求体是否使用 multipart/form-data）。
// 复用注册阶段预计算的 meta.fields（已展开嵌入字段），与绑定端判定逻辑保持单一来源
func hasFileField(meta structMeta) bool {
	for i := range meta.fields {
		if meta.fields[i].isFile || meta.fields[i].isFileSlice {
			return true
		}
	}
	return false
}

// uniqueName 为注册键分配唯一的 schema 名：优先使用 base，重名时追加序号。
// base 由 schemaBaseName 按角色产生（响应角色带 "Response" 后缀），相同类型的不同角色
// 因此得到不同名字，可直接从 schema 名分辨其用途。
func (g *openAPIGenerator) uniqueName(key schemaKey, base string) string {
	name := base
	for i := 2; ; i++ {
		owner, used := g.nameToType[name]
		if !used {
			g.nameToType[name] = key
			return name
		}
		if owner == key {
			return name
		}
		name = base + strconv.Itoa(i)
	}
}

// refSchema 生成对 components/schemas 的引用对象
func refSchema(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

// shortFuncName 从全限定函数名中取出简短名（去除包路径前缀与方法值 "-fm" 后缀）
func shortFuncName(full string) string {
	if i := strings.LastIndex(full, "."); i != -1 {
		full = full[i+1:]
	}
	return strings.TrimSuffix(full, "-fm")
}
