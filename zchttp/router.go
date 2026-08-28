package zchttp

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"
)

// Router 路由注册表。注册必须在服务启动前完成，运行期并发注册属未定义行为
type Router struct {
	trees       map[string]*routeNode // 基数树：method -> 根节点（静态段与参数段同树）
	routes      []routeRecord         // 顺序索引（按注册顺序）：供 OpenAPI 生成遍历
	middlewares []MiddlewareHandler   // 全局中间件，作用于通过本 Router 注册的所有路由
}

// routeRecord 保存一条路由的 method/path 模板与 entry，供 GenerateOpenAPI 还原路径模板
// （path 为归一化后的模板，参数路由含 {name}/{name?} 段）。
type routeRecord struct {
	method string
	path   string
	entry  *routeEntry
}

// NewRouter 创建空路由表，并预初始化 9 个标准 HTTP 方法的基数树根节点。
func NewRouter() *Router {
	r := &Router{
		trees: make(map[string]*routeNode),
	}
	for _, method := range []string{
		http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace,
	} {
		r.trees[method] = &routeNode{static: make(map[string]*routeNode)}
	}
	return r
}

// Use 注册全局中间件，按调用顺序追加；只对此后注册的路由生效（注册时快照中间件链）
func (r *Router) Use(middlewares ...MiddlewareHandler) *Router {
	r.middlewares = append(r.middlewares, middlewares...)
	return r
}

// Group 创建一个路由分组，分组内的路由会自动拼接 prefix 前缀，并叠加给定的中间件
func (r *Router) Group(prefix string, middlewares ...MiddlewareHandler) *RouterGroup {
	return &RouterGroup{
		router:      r,
		prefix:      normalizePrefix(prefix),
		middlewares: append([]MiddlewareHandler{}, middlewares...),
	}
}

// register 是所有 HTTP 方法共用的注册逻辑：先构建 entry（含签名校验与反射预计算），
// 再解析路径段、预计算 Req 字段绑定，最后插入基数树（静态段与参数段同树，冲突在树内检测）。
// groupMiddlewares 为分组的中间件；最终中间件链顺序为 [全局 ... , 分组 ...]
func (r *Router) register(method, path string, handler any, groupMiddlewares []MiddlewareHandler) {
	path = normalizePath(path)

	entry, err := buildEntry(handler, r.middlewares, groupMiddlewares)
	if err != nil {
		panic(err.Error())
	}

	// 注册阶段扫描 Req 类型树，检测 default 标签误用（包含路由信息以便定位）
	checkUnsupportedDefaults(entry.reqElemType, true, true, method, path, entry.handlerName, entry.handlerFile, entry.handlerLine, map[reflect.Type]bool{})

	segments, perr := parseRoutePath(path)
	if perr != nil {
		panic(fmt.Sprintf("invalid route path: %s", perr))
	}
	attachPathParamBindings(entry, segments, method, path)
	insertRoute(r.trees[method], segments, entry, method, path)
	r.routes = append(r.routes, routeRecord{method: method, path: path, entry: entry})
}

// match 在指定 method 的基数树上匹配请求路径，
// 命中时返回 entry 与按注册顺序捕获的参数值（被省略的尾部可选参数不在切片中）；未命中返回 nil。
// 静态段优先于参数段，静态分支失败时回溯尝试参数分支；
// 匹配采用逐段子串扫描，不预切分整个路径，捕获切片延迟到真正捕获参数时才分配
func (r *Router) match(method, path string) (*routeEntry, []string) {
	root := r.trees[method]
	if root == nil {
		return nil, nil
	}
	if path == "/" {
		path = ""
	}
	return root.matchPath(path, nil)
}

// routeSegment 表示参数路由路径中的一段：静态字面量或 {name}/{name?} 参数
type routeSegment struct {
	literal  string // 静态段内容（isParam=false 时有效）
	name     string // 参数名（isParam=true 时有效）
	isParam  bool   // 是否为参数段
	optional bool   // 是否为可选参数 {name?}
}

// paramNamePattern 限定参数名格式：字母/下划线开头，仅含字母、数字、下划线
var paramNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// splitPathSegments 将归一化后的路径（以 "/" 开头、无末尾 "/"）按段切分；
// 根路径 "/" 返回空切片
func splitPathSegments(path string) []string {
	path = strings.TrimPrefix(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

// parseRoutePath 解析参数路由路径为段列表，并执行语法校验：
//   - 参数必须独占整个段，形如 {name} 或 {name?}
//   - 参数名仅允许 [A-Za-z_][A-Za-z0-9_]*，同一路径内不允许重复
//   - 可选参数之后不允许再出现任何段（省略匹配会产生歧义，可选参数必须位于路径末尾）
func parseRoutePath(path string) ([]routeSegment, error) {
	parts := splitPathSegments(path)
	segments := make([]routeSegment, 0, len(parts))
	names := make(map[string]bool, len(parts))
	seenOptional := false
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty path segment in %q", path)
		}
		if seenOptional {
			return nil, fmt.Errorf("segment %q is not allowed after optional parameter in %q", part, path)
		}
		if !strings.ContainsAny(part, "{}") {
			segments = append(segments, routeSegment{literal: part})
			continue
		}
		if len(part) < 2 || part[0] != '{' || part[len(part)-1] != '}' {
			return nil, fmt.Errorf("invalid parameter segment %q in %q: parameter must occupy a whole segment as {name} or {name?}", part, path)
		}
		inner := part[1 : len(part)-1]
		optional := strings.HasSuffix(inner, "?")
		if optional {
			inner = inner[:len(inner)-1]
		}
		if !paramNamePattern.MatchString(inner) {
			return nil, fmt.Errorf("invalid parameter name %q in %q", inner, path)
		}
		if names[inner] {
			return nil, fmt.Errorf("duplicate parameter name %q in %q", inner, path)
		}
		names[inner] = true
		if optional {
			seenOptional = true
		}
		segments = append(segments, routeSegment{name: inner, isParam: true, optional: optional})
	}
	return segments, nil
}

// GET 注册处理 GET 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) GET(path string, handler any) {
	r.register(http.MethodGet, path, handler, nil)
}

// POST 注册处理 POST 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) POST(path string, handler any) {
	r.register(http.MethodPost, path, handler, nil)
}

// PUT 注册处理 PUT 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) PUT(path string, handler any) {
	r.register(http.MethodPut, path, handler, nil)
}

// DELETE 注册处理 DELETE 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) DELETE(path string, handler any) {
	r.register(http.MethodDelete, path, handler, nil)
}

// PATCH 注册处理 PATCH 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) PATCH(path string, handler any) {
	r.register(http.MethodPatch, path, handler, nil)
}

// HEAD 注册处理 HEAD 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) HEAD(path string, handler any) {
	r.register(http.MethodHead, path, handler, nil)
}

// OPTIONS 注册处理 OPTIONS 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) OPTIONS(path string, handler any) {
	r.register(http.MethodOptions, path, handler, nil)
}

// CONNECT 注册处理 CONNECT 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) CONNECT(path string, handler any) {
	r.register(http.MethodConnect, path, handler, nil)
}

// TRACE 注册处理 TRACE 方法的路由；handler 签名非法或路由冲突时 panic。
func (r *Router) TRACE(path string, handler any) {
	r.register(http.MethodTrace, path, handler, nil)
}

// =============== RouterGroup ===============

// RouterGroup 路由分组：携带 path 前缀与一组中间件，可嵌套生成子分组
type RouterGroup struct {
	router      *Router
	prefix      string
	middlewares []MiddlewareHandler
}

// Use 向当前分组追加中间件，返回自身以便链式调用；只对此后注册的路由生效
func (g *RouterGroup) Use(middlewares ...MiddlewareHandler) *RouterGroup {
	g.middlewares = append(g.middlewares, middlewares...)
	return g
}

// Group 创建嵌套子分组：前缀拼接，父分组的中间件被继承且位于子分组中间件之前
func (g *RouterGroup) Group(prefix string, middlewares ...MiddlewareHandler) *RouterGroup {
	sub := &RouterGroup{
		router: g.router,
		prefix: g.prefix + normalizePrefix(prefix),
	}
	sub.middlewares = make([]MiddlewareHandler, 0, len(g.middlewares)+len(middlewares))
	sub.middlewares = append(sub.middlewares, g.middlewares...)
	sub.middlewares = append(sub.middlewares, middlewares...)
	return sub
}

// GET 在分组内注册 GET 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) GET(path string, handler any) {
	g.router.register(http.MethodGet, g.prefix+path, handler, g.middlewares)
}

// POST 在分组内注册 POST 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) POST(path string, handler any) {
	g.router.register(http.MethodPost, g.prefix+path, handler, g.middlewares)
}

// PUT 在分组内注册 PUT 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) PUT(path string, handler any) {
	g.router.register(http.MethodPut, g.prefix+path, handler, g.middlewares)
}

// DELETE 在分组内注册 DELETE 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) DELETE(path string, handler any) {
	g.router.register(http.MethodDelete, g.prefix+path, handler, g.middlewares)
}

// PATCH 在分组内注册 PATCH 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) PATCH(path string, handler any) {
	g.router.register(http.MethodPatch, g.prefix+path, handler, g.middlewares)
}

// HEAD 在分组内注册 HEAD 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) HEAD(path string, handler any) {
	g.router.register(http.MethodHead, g.prefix+path, handler, g.middlewares)
}

// OPTIONS 在分组内注册 OPTIONS 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) OPTIONS(path string, handler any) {
	g.router.register(http.MethodOptions, g.prefix+path, handler, g.middlewares)
}

// CONNECT 在分组内注册 CONNECT 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) CONNECT(path string, handler any) {
	g.router.register(http.MethodConnect, g.prefix+path, handler, g.middlewares)
}

// TRACE 在分组内注册 TRACE 方法路由：完整路径为分组前缀 + path，中间件链为
// [全局..., 分组...] 快照；handler 签名非法或路由冲突时 panic。
func (g *RouterGroup) TRACE(path string, handler any) {
	g.router.register(http.MethodTrace, g.prefix+path, handler, g.middlewares)
}

// normalizePrefix 规范化分组前缀：保证以 "/" 开头、去除末尾 "/"，空串与 "/" 返回 ""
func normalizePrefix(p string) string {
	if p == "" || p == "/" {
		return ""
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	p = strings.TrimRight(p, "/")
	return p
}

// normalizePath 规范化路由路径：补全前导的 "/"（r.URL.Path 永远以 "/" 开头，
// 不补全会导致路由永不命中），并去除末尾的 "/"（如 /hello/ -> /hello），
// 使 /hello 与 /hello/ 等价；根路径 "/" 与空串统一返回 "/"
func normalizePath(p string) string {
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if p == "" {
		p = "/"
	}
	return p
}
