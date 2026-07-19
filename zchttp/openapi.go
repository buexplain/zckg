package zchttp

import (
	"net/http"
	"reflect"
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
	Title       string
	Description string
	Version     string
	Servers     []OpenAPIServer
}

// openAPIGenerator 承载一次生成过程中的可复用状态
type openAPIGenerator struct {
	schemas         map[string]any          // components/schemas
	typeNames       map[reflect.Type]string // 已注册类型 → schema 名，避免重复与循环递归
	nameToType      map[string]reflect.Type // schema 名 → 类型，用于命名去重
	reachedViaValue map[reflect.Type]bool   // struct 类型是否被"值嵌套"路径到达（含顶层）
	currentType     reflect.Type            // 当前正在生成 schema 的 struct 类型（decorate 据此判断上下文）
}

// GenerateOpenAPI 遍历路由表，通过反射生成 OpenAPI 3.0 文档（map 形式，可序列化为 JSON）
func GenerateOpenAPI(r *Router, info OpenAPIInfo) map[string]any {
	g := &openAPIGenerator{
		schemas:         map[string]any{},
		typeNames:       map[reflect.Type]string{},
		nameToType:      map[string]reflect.Type{},
		reachedViaValue: map[reflect.Type]bool{},
	}

	// Pass 1：收集所有 struct 类型的嵌套上下文（值嵌套 vs 指针嵌套）。
	// 值嵌套（顶层 + 值类型 struct 字段）→ 注册阶段 fill 所有类型；
	// 指针嵌套（*Struct）→ 注册阶段无法到达，仅请求阶段 fill nil 指针。
	// 此信息在 decorate 中判断值类型字段的 default 是否应展示。
	g.collectTypeUsages(r)

	paths := map[string]any{}
	for method, routes := range r.routes {
		for path, entry := range routes {
			op := g.buildOperation(method, entry)
			if op == nil {
				continue
			}
			item, ok := paths[path].(map[string]any)
			if !ok {
				item = map[string]any{}
				paths[path] = item
			}
			item[strings.ToLower(method)] = op
		}
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
	for _, routes := range r.routes {
		for _, entry := range routes {
			if entry.reqType != nil {
				g.walkTypeUsage(derefType(entry.reqType), true, map[reflect.Type]bool{})
			}
			if entry.resType != nil {
				g.walkTypeUsage(derefType(entry.resType), true, map[reflect.Type]bool{})
			}
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
		f := t.Field(fm.indices[0])
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
			} else {
				g.walkTypeUsage(elem, false, visiting)
			}
		default:
			// Interface / Func / Chan / UnsafePointer 等无法反射出 struct，无需处理
		}
	}
}

// buildOperation 构造单个操作对象（parameters/requestBody/responses/tags/summary）
func (g *openAPIGenerator) buildOperation(method string, entry *routeEntry) map[string]any {
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
		if params := g.buildQueryParams(reqType, entry.reqMeta); len(params) > 0 {
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
// meta 为注册阶段预计算的 structMeta，直接使用其字段名和 required 判定，避免重复反射遍历。
func (g *openAPIGenerator) buildQueryParams(reqType reflect.Type, meta structMeta) []any {
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	var params []any
	for _, fm := range meta.fields {
		if fm.name == "" || fm.name == "-" {
			continue
		}
		f := reqType.Field(fm.indices[0])
		if isIgnored(f) {
			continue
		}
		params = append(params, map[string]any{
			"name":     fm.name,
			"in":       "query",
			"required": fm.required,
			"schema":   g.typeToSchema(f.Type, f),
		})
	}
	return params
}

// buildRequestBody 为携带请求体的方法生成 requestBody；含文件字段时使用 multipart/form-data。
// meta 为注册阶段预计算的 structMeta，透传给 registerStructSchema 以复用 required 判定。
func (g *openAPIGenerator) buildRequestBody(reqType reflect.Type, meta structMeta) map[string]any {
	if reqType.Kind() != reflect.Struct {
		return nil
	}
	contentType := "application/json"
	if hasFileField(reqType) {
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

// wrapResponseSchema 将 Res 结构体包装进统一的 Response{data,code,message} 结构。
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
		g.schemas[wrapName] = map[string]any{
			"type": "object",
			"properties": map[string]any{
				"data":    dataSchema,
				"code":    map[string]any{"type": "integer"},
				"message": map[string]any{"type": "string"},
			},
		}
	}
	return refSchema(wrapName)
}

// registerStructSchema 将结构体注册到 components/schemas 并返回 $ref 引用。
// meta 为 structMeta（来自 routeEntry 或 buildStructMeta 现算），提供字段名和 required 判定。
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
		f := t.Field(fm.indices[0])
		if isIgnored(f) {
			continue
		}
		props[fm.name] = g.typeToSchema(f.Type, f)
		if fm.required {
			required = append(required, fm.name)
		}
	}
	obj["properties"] = props
	if len(required) > 0 {
		obj["required"] = required
	}
	return refSchema(name)
}

// typeToSchema 将 Go 类型映射为 JSON Schema；field 提供 default/example/description/time_format 等信息
func (g *openAPIGenerator) typeToSchema(t reflect.Type, field reflect.StructField) map[string]any {
	if t.Kind() == reflect.Ptr {
		if t == fileHeaderPtrType {
			return map[string]any{"type": "string", "format": "binary"}
		}
		inner := g.typeToSchema(t.Elem(), field)
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
			"items": g.typeToSchema(t.Elem(), emptyField),
		}, field)
	case reflect.Struct:
		return g.decorate(g.registerStructSchema(t, buildStructMeta(t)), field)
	case reflect.Map:
		valueSchema := g.typeToSchema(t.Elem(), emptyField)
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
//   - 指针类型（*int/*string 等）：请求阶段可靠填充 → 始终展示 default
//   - 值类型（int/string 等）：仅当所属 struct 被"值嵌套"到达时才展示 default（注册阶段可靠）
func (g *openAPIGenerator) decorate(schema map[string]any, field reflect.StructField) map[string]any {
	if field.Tag == "" {
		return schema
	}
	if def, ok := field.Tag.Lookup("default"); ok && isDefaultSupported(field.Type) {
		// 指针类型：请求阶段 fill nil 指针 → 始终展示
		// 值类型：仅当所属 struct 被值嵌套到达（注册阶段 fill）才展示
		if field.Type.Kind() == reflect.Ptr || g.reachedViaValue[g.currentType] {
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

// hasFileField 判断结构体是否含上传文件字段（决定请求体是否使用 multipart/form-data）
func hasFileField(t reflect.Type) bool {
	t = derefType(t)
	if t.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Type == fileHeaderPtrType {
			return true
		}
		if f.Type.Kind() == reflect.Slice && f.Type.Elem() == fileHeaderPtrType {
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

// derefType 解引用指针类型直到得到非指针类型
func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return t
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
