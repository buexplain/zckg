package zchttp

import (
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
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
	ResponseWrapper any // 自定义响应包装结构体样例（如 MyResponse{}），为 nil 时使用默认 Response{data,code,message}
}

// openAPIGenerator 承载一次生成过程中的可复用状态
type openAPIGenerator struct {
	schemas           map[string]any          // components/schemas
	typeNames         map[reflect.Type]string // 已注册类型 → schema 名，避免重复与循环递归
	nameToType        map[string]reflect.Type // schema 名 → 类型，用于命名去重
	reachedViaValue   map[reflect.Type]bool   // struct 类型是否被"值嵌套"路径到达（含顶层）
	reachedByDefaults map[reflect.Type]bool   // struct 类型是否可被 applyDefaults 递归到达
	currentType       reflect.Type            // 当前正在生成 schema 的 struct 类型（decorate 据此判断上下文）
	responseWrapper   any                     // 自定义响应包装结构体样例
}

// GenerateOpenAPI 遍历路由表，通过反射生成 OpenAPI 3.0 文档（map 形式，可序列化为 JSON）
func GenerateOpenAPI(r *Router, info OpenAPIInfo) map[string]any {
	g := &openAPIGenerator{
		schemas:           map[string]any{},
		typeNames:         map[reflect.Type]string{},
		nameToType:        map[string]reflect.Type{},
		reachedViaValue:   map[reflect.Type]bool{},
		reachedByDefaults: map[reflect.Type]bool{},
		responseWrapper:   info.ResponseWrapper,
	}

	// Pass 1：收集所有 struct 类型的嵌套上下文（值嵌套 vs 指针嵌套）。
	// 值嵌套（顶层 + 值类型 struct 字段）→ 注册阶段 fill 所有类型；
	// 指针嵌套（*Struct）→ 注册阶段无法到达，仅请求阶段 fill nil 指针。
	// 此信息在 decorate 中判断值类型字段的 default 是否应展示。
	g.collectTypeUsages(r)

	// Pass 1b：收集所有 struct 类型是否可被 applyDefaults 递归到达。
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
		// 参数路由的 {name}/{name?} 段声明为 path 参数并从 query 排除；
		// 静态路由无参数段，paramNames/optionalParams 为空集。
		// OpenAPI 无 {name?} 语法，可选参数转换为 {name} 形式并以 required:false 声明
		segments, perr := parseRoutePath(rec.path)
		if perr != nil {
			continue // 注册阶段已校验，防御性跳过
		}
		paramNames := make(map[string]bool)
		optionalParams := make(map[string]bool)
		for _, seg := range segments {
			if seg.isParam {
				paramNames[seg.name] = true
				if seg.optional {
					optionalParams[seg.name] = true
				}
			}
		}
		op := g.buildOperation(rec.method, rec.entry, paramNames, optionalParams)
		if op == nil {
			continue
		}
		// 路径模板转换为 OpenAPI 规范形式：{name?} → {name}
		openapiPath := strings.ReplaceAll(rec.path, "?}", "}")
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

// collectTypeUsages 第一遍遍历：收集所有 struct 类型是否被"值嵌套"路径到达。
// 值嵌套（含顶层 Req/Res 和值类型 struct 字段）→ 注册阶段 fill 所有类型；
// 指针嵌套（*Struct）或切片/map 元素 → 注册阶段无法到达，仅请求阶段 fill 指针类型。
// 此信息在 decorate 中判断值类型字段 default 是否应展示。
func (g *openAPIGenerator) collectTypeUsages(r *Router) {
	for _, rec := range r.routes {
		entry := rec.entry
		if entry.reqType != nil {
			g.walkTypeUsage(derefType(entry.reqType), true, map[reflect.Type]bool{})
		}
		if entry.resType != nil {
			g.walkTypeUsage(derefType(entry.resType), true, map[reflect.Type]bool{})
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

// collectDefaultsReachability 收集所有 struct 类型是否可被 applyDefaults 递归到达。
// 与 collectTypeUsages 的区别：
//   - collectTypeUsages 追踪"值嵌套"可达性（viaValue），仅值类型 default 展示依赖它
//   - collectDefaultsReachability 追踪"默认值填充"可达性（viaDefaults），指针字段 default 展示依赖它
//
// 可达性规则（与 applyDefaultsWithVisiting 一致）：
//   - 顶层 Req/Res：可达
//   - 值类型 struct 字段：继承父级可达性
//   - 指针字段（*Struct）：继承父级可达性（applyDefaults 可跟随非 nil 指针）
//   - 切片/数组元素：继承父级可达性（applyDefaults 可遍历切片元素）
//   - map 值类型为 Struct 或 *Struct：继承父级可达性
//   - map 值类型为 Slice/Array：不可达（applyDefaults 无法穿透此类多层容器）
func (g *openAPIGenerator) collectDefaultsReachability(r *Router) {
	for _, rec := range r.routes {
		entry := rec.entry
		if entry.reqType != nil {
			g.walkDefaultsReachability(derefType(entry.reqType), true, map[reflect.Type]bool{})
		}
		if entry.resType != nil {
			g.walkDefaultsReachability(derefType(entry.resType), true, map[reflect.Type]bool{})
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

	meta := buildStructMeta(t)
	for _, fm := range meta.fields {
		f := fm.field
		ft := f.Type
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

// buildOperation 构造单个操作对象（parameters/requestBody/responses/tags/summary）。
// paramNames 为路径模板中的 {name}/{name?} 参数名集合（无参数段的路由为空集）：对应字段声明为
// path 参数（in: path）且不再作为 query 参数展示；optionalParams 标记其中可选参数。
func (g *openAPIGenerator) buildOperation(method string, entry *routeEntry, paramNames, optionalParams map[string]bool) map[string]any {
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

	switch method {
	case http.MethodGet, http.MethodDelete, http.MethodHead:
		var params []any
		if len(paramNames) > 0 {
			params = append(params, g.buildPathParams(reqType, entry.reqMeta, paramNames, optionalParams)...)
		}
		if qp := g.buildQueryParams(reqType, entry.reqMeta, paramNames); len(qp) > 0 {
			params = append(params, qp...)
		}
		if len(params) > 0 {
			op["parameters"] = params
		}
	default:
		if body := g.buildRequestBody(reqType, entry.reqMeta); body != nil {
			op["requestBody"] = body
		}
	}

	op["responses"] = g.buildResponses(resType, entry.resMeta)
	return op
}

// buildQueryParams 为 GET/DELETE/HEAD 生成 query 参数列表。
// meta 为注册阶段预计算的 structMeta，直接使用其字段名和 nonzero+default 判定，避免重复反射遍历。
// paramNames 为路径参数名集合：绑定到路径参数的字段不作为 query 参数展示。
// 跳过非扁平字段（Map 与命名 struct）：query 绑定仅处理扁平字段，展示会误导 API 使用者；
// 文件字段一并跳过（GET 无 multipart）。
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
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Map || (ft.Kind() == reflect.Struct && ft != timeType) {
			continue // 非扁平字段：query 绑定仅处理扁平字段
		}
		params = append(params, map[string]any{
			"name":     fm.name,
			"in":       "query",
			"required": fm.nonzero && !fm.hasDefault,
			"schema":   g.typeToSchema(f.Type, f, 0),
		})
	}
	return params
}

// buildPathParams 为参数路由的 {name}/{name?} 段声明 path 参数。
// meta 为注册阶段预计算的 structMeta；paramNames 为路径模板中的参数名集合；
// optionalParams 标记可选参数（{name?}），其 required 为 false，
// 可选参数被省略时保留字段 default 值或零值。
func (g *openAPIGenerator) buildPathParams(reqType reflect.Type, meta structMeta, paramNames, optionalParams map[string]bool) []any {
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
		params = append(params, map[string]any{
			"name":     fm.name,
			"in":       "path",
			"required": !optionalParams[fm.name],
			"schema":   g.typeToSchema(f.Type, f, 0),
		})
	}
	return params
}

// buildRequestBody 为携带请求体的方法生成 requestBody；含文件字段时使用 multipart/form-data。
// meta 为注册阶段预计算的 structMeta，透传给 registerStructSchema 以复用 nonzero+default 判定。
func (g *openAPIGenerator) buildRequestBody(reqType reflect.Type, meta structMeta) map[string]any {
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	contentType := "application/json"
	if hasFileField(meta) {
		contentType = "multipart/form-data"
	}
	return map[string]any{
		"required": true,
		"content": map[string]any{
			contentType: map[string]any{
				"schema": g.registerStructSchema(reqType, meta),
			},
		},
	}
}

// buildResponses 生成响应对象：成功统一返回 200（与 HttpEngine 默认响应行为一致）；响应体统一包装为 Response 结构。
// meta 为注册阶段预计算的 Res 结构体的 structMeta，透传给下层避免重复反射。
func (g *openAPIGenerator) buildResponses(resType reflect.Type, meta structMeta) map[string]any {
	return map[string]any{
		"200": map[string]any{
			"description": "成功",
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": g.wrapResponseSchema(resType, meta),
				},
			},
		},
	}
}

// wrapResponseSchema 将 Res 结构体包装进统一的响应结构。
// 若设置了 responseWrapper，则通过反射推导自定义包装 schema；否则使用默认 Response{data,code,message}。
// meta 为注册阶段预计算的 Res 结构体的 structMeta。
func (g *openAPIGenerator) wrapResponseSchema(resType reflect.Type, meta structMeta) map[string]any {
	rt := derefType(resType)
	dataSchema := g.registerStructSchema(rt, meta)
	resName := g.typeNames[rt]
	if resName == "" {
		resName = "AnonymousStruct"
	}
	wrapName := "Response_" + resName
	if _, ok := g.schemas[wrapName]; !ok {
		g.schemas[wrapName] = g.buildWrapperProperties(dataSchema)
	}
	return refSchema(wrapName)
}

// buildWrapperProperties 根据 responseWrapper 构建响应包装的 schema properties。
// 若 responseWrapper 为 nil，返回默认 {data, code, message}；
// 否则通过反射读取自定义结构体的 json 标签，将 any/interface{} 类型字段视为 data 占位符。
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

	wt := reflect.TypeOf(g.responseWrapper)
	wt = derefType(wt)
	if wt.Kind() != reflect.Struct {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data":    dataSchema,
				"code":    map[string]any{"type": "integer"},
				"message": map[string]any{"type": "string"},
			},
		}
	}

	props := map[string]any{}
	for i := 0; i < wt.NumField(); i++ {
		f := wt.Field(i)
		if f.PkgPath != "" {
			continue
		}
		jsonName := parseJSONName(f.Tag.Get("json"))
		if jsonName == "" || jsonName == "-" {
			continue
		}
		// any/interface{} 类型字段视为 data 占位符，替换为实际 Res schema
		if f.Type.Kind() == reflect.Interface {
			props[jsonName] = dataSchema
		} else {
			props[jsonName] = g.typeToSchema(f.Type, f, 0)
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

// registerStructSchema 将结构体注册到 components/schemas 并返回 $ref 引用。
// meta 为 structMeta（来自 routeEntry 或 buildStructMeta 现算），提供字段名和 nonzero+default 判定。
func (g *openAPIGenerator) registerStructSchema(t reflect.Type, meta structMeta) map[string]any {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return map[string]any{"type": "object"}
	}
	if name, ok := g.typeNames[t]; ok {
		return refSchema(name)
	}
	name := g.uniqueName(t)
	g.typeNames[t] = name

	obj := map[string]any{"type": "object"}
	g.schemas[name] = obj // 先占位，防止循环递归

	// 设置当前类型上下文，decorate 据此判断值类型 default 是否应展示
	prevType := g.currentType
	g.currentType = t
	defer func() { g.currentType = prevType }()

	props := map[string]any{}
	var required []any
	for _, fm := range meta.fields {
		if fm.name == "" || fm.name == "-" {
			continue
		}
		f := fm.field
		if isIgnored(f) {
			continue
		}
		props[fm.name] = g.typeToSchema(f.Type, f, 0)
		if fm.nonzero && !fm.hasDefault {
			required = append(required, fm.name)
		}
	}
	obj["properties"] = props
	if len(required) > 0 {
		obj["required"] = required
	}
	return refSchema(name)
}

// typeToSchema 将 Go 类型映射为 JSON Schema；field 提供 default/example/description/time_format 等信息。
// depth 为 Ptr/Slice/Map 链的累计嵌套深度：防自引用命名类型（type S []S / type P *P /
// type A *B 与 type B *A 等）在容器/指针分支无限递归，超限退化为空 schema（与
// default 分支一致）；上限复用 maxPtrDerefDepth，与 derefType/isDefaultSupportedDepth/
// setScalar 的防环哲学一致（修复 REC-04）。Struct 分支经 registerStructSchema
// 占位机制防环，不消耗 depth。
func (g *openAPIGenerator) typeToSchema(t reflect.Type, field reflect.StructField, depth int) map[string]any {
	if depth > maxPtrDerefDepth {
		return map[string]any{}
	}
	if t.Kind() == reflect.Ptr {
		if t == fileHeaderPtrType {
			return map[string]any{"type": "string", "format": "binary"}
		}
		inner := g.typeToSchema(t.Elem(), field, depth+1)
		if _, isRef := inner["$ref"]; isRef {
			return g.decorate(map[string]any{"nullable": true, "allOf": []any{inner}}, field)
		}
		inner["nullable"] = true
		return g.decorate(inner, field)
	}

	if t == timeType {
		return g.timeSchema(field)
	}

	switch t.Kind() {
	case reflect.String:
		return g.decorate(map[string]any{"type": "string"}, field)
	case reflect.Bool:
		return g.decorate(map[string]any{"type": "boolean"}, field)
	case reflect.Int, reflect.Int64:
		return g.decorate(map[string]any{"type": "integer", "format": "int64"}, field)
	case reflect.Int8, reflect.Int16, reflect.Int32:
		return g.decorate(map[string]any{"type": "integer", "format": "int32"}, field)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return g.decorate(map[string]any{"type": "integer", "format": "int64", "minimum": 0}, field)
	case reflect.Float32:
		return g.decorate(map[string]any{"type": "number", "format": "float"}, field)
	case reflect.Float64:
		return g.decorate(map[string]any{"type": "number", "format": "double"}, field)
	case reflect.Slice, reflect.Array:
		if t.Elem() == fileHeaderPtrType {
			return g.decorate(map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "string", "format": "binary"},
			}, field)
		}
		return g.decorate(map[string]any{
			"type":  "array",
			"items": g.typeToSchema(t.Elem(), emptyField, depth+1),
		}, field)
	case reflect.Struct:
		return g.decorate(g.registerStructSchema(t, buildStructMeta(t)), field)
	case reflect.Map:
		valueSchema := g.typeToSchema(t.Elem(), emptyField, depth+1)
		schema := map[string]any{
			"type":                 "object",
			"additionalProperties": valueSchema,
		}
		result := g.decorate(schema, field)
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

// timeSchema 依据 time_format 标签决定 time.Time 的 schema 表现（时间戳→integer，其余→date-time）
func (g *openAPIGenerator) timeSchema(field reflect.StructField) map[string]any {
	switch field.Tag.Get("time_format") {
	case "unix", "unixmilli", "unixmicro", "unixnano":
		return g.decorate(map[string]any{"type": "integer", "format": "int64"}, field)
	default:
		return g.decorate(map[string]any{"type": "string", "format": "date-time"}, field)
	}
}

// decorate 依据字段标签补充 default / example / description。
// default 展示规则遵循两阶段填充语义：
//   - 注册阶段：模板预填所有零值字段，但仅限"值嵌套"路径（顶层 + 值类型 struct 字段）能到达的 struct
//   - 请求阶段（post-bind）：仅 nil 指针字段被填充，值类型跳过
//
// 因此：
//   - 指针类型（*int/*string 等）：仅当所属 struct 可被 applyDefaults 到达时展示 default
//     （reachedByDefaults 追踪，与 applyDefaultsWithVisiting 可达性一致）
//   - 值类型（int/string 等）：仅当所属 struct 被"值嵌套"到达时才展示 default（注册阶段可靠）
func (g *openAPIGenerator) decorate(schema map[string]any, field reflect.StructField) map[string]any {
	if field.Tag == "" {
		return schema
	}
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
	if ex := field.Tag.Get("example"); ex != "" {
		schema["example"] = coerceExample(schema, ex)
	}
	if d := field.Tag.Get("description"); d != "" {
		schema["description"] = d
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

// isIgnored 判定字段是否通过 ignore 标签从文档中排除
func isIgnored(field reflect.StructField) bool {
	if v := field.Tag.Get("ignore"); v != "" {
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

// uniqueName 为类型分配唯一的 schema 名，重名时追加序号
func (g *openAPIGenerator) uniqueName(t reflect.Type) string {
	base := t.Name()
	if base == "" {
		base = "AnonymousStruct"
	}
	name := base
	for i := 2; ; i++ {
		owner, used := g.nameToType[name]
		if !used {
			g.nameToType[name] = t
			return name
		}
		if owner == t {
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
