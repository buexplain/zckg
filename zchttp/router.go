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
// （path 为归一化后的模板，参数路由含 {name}/{name?}/{name...} 段）。
type routeRecord struct {
	method string
	path   string
	entry  *routeEntry
}

type routeRegistration struct {
	method   string
	path     string
	segments []routeSegment
	entry    *routeEntry
}

var allHTTPMethods = [...]string{
	http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
	http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodConnect, http.MethodTrace,
}

// NewRouter 创建空路由表，并预初始化 9 个标准 HTTP 方法的基数树根节点。
func NewRouter() *Router {
	r := &Router{
		trees: make(map[string]*routeNode),
	}
	for _, method := range allHTTPMethods {
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

// register 是所有 HTTP 方法共用的注册逻辑：先完整准备，再写入路由树与顺序索引。
func (r *Router) register(method, path string, handler any, groupMiddlewares []MiddlewareHandler) {
	registration := r.prepareRegistration(method, path, handler, groupMiddlewares)
	r.commitRegistration(registration)
}

func (r *Router) prepareRegistration(method, path string, handler any, groupMiddlewares []MiddlewareHandler) routeRegistration {
	path = normalizePath(path)
	entry, err := buildEntry(handler, r.middlewares, groupMiddlewares)
	if err != nil {
		panic(err.Error())
	}
	checkUnsupportedDefaults(entry.reqElemType, true, true, method, path, entry.handlerName, entry.handlerFile, entry.handlerLine, map[reflect.Type]bool{})
	segments, err := parseRoutePath(path)
	if err != nil {
		panic(fmt.Sprintf("invalid route path: %s", err))
	}
	attachPathParamBindings(entry, segments, method, path)
	return routeRegistration{method: method, path: path, segments: segments, entry: entry}
}

func (r *Router) commitRegistration(registration routeRegistration) {
	insertRoute(r.trees[registration.method], registration.segments, registration.entry, registration.method, registration.path)
	r.routes = append(r.routes, routeRecord{method: registration.method, path: registration.path, entry: registration.entry})
}

func (r *Router) registerAny(path string, handler any, groupMiddlewares []MiddlewareHandler) {
	registrations := make([]routeRegistration, 0, len(allHTTPMethods))
	for _, method := range allHTTPMethods {
		registrations = append(registrations, r.prepareRegistration(method, path, handler, groupMiddlewares))
	}
	for _, registration := range registrations {
		rootCopy := cloneRouteNode(r.trees[registration.method])
		insertRoute(rootCopy, registration.segments, registration.entry, registration.method, registration.path)
	}
	for _, registration := range registrations {
		r.commitRegistration(registration)
	}
}

// match 在指定 method 的基数树上匹配请求路径，
// 命中时返回 entry 与按注册顺序捕获的参数值（被省略的尾部可选参数、零段命中的通配尾段不在切片中）；
// 未命中返回 nil。同节点优先级为静态段 > 参数段 > 通配尾段，前者分支失败时依次回溯；
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

// routeSegment 表示参数路由路径中的一段：静态字面量或 {name}/{name?}/{name...} 参数
type routeSegment struct {
	literal  string // 静态段内容（isParam=false 时有效）
	name     string // 参数名（isParam=true 时有效）
	isParam  bool   // 是否为参数段（含 {name...} 通配尾段）
	optional bool   // 是否为可选参数 {name?}
	catchAll bool   // 是否为通配尾段 {name...}
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
//   - 参数必须独占整个段，形如 {name}、{name?} 或 {name...}
//   - 参数名仅允许 [A-Za-z_][A-Za-z0-9_]*，同一路径内不允许重复
//   - 可选参数之后不允许再出现任何段（省略匹配会产生歧义，可选参数必须位于路径末尾）
//   - 通配尾段 {name...} 捕获剩余全部路径，其后同样不允许再出现任何段
//   - 后缀先判 "..." 再判 "?"：{a?...}、{a...?} 剥离后缀后的残余名不合法，由参数名校验拒绝
func parseRoutePath(path string) ([]routeSegment, error) {
	parts := splitPathSegments(path)
	segments := make([]routeSegment, 0, len(parts))
	names := make(map[string]bool, len(parts))
	seenOptional := false
	seenCatchAll := false
	for _, part := range parts {
		if part == "" {
			return nil, fmt.Errorf("empty path segment in %q", path)
		}
		if seenOptional {
			return nil, fmt.Errorf("segment %q is not allowed after optional parameter in %q", part, path)
		}
		if seenCatchAll {
			return nil, fmt.Errorf("segment %q is not allowed after catch-all parameter in %q", part, path)
		}
		if !strings.ContainsAny(part, "{}") {
			segments = append(segments, routeSegment{literal: part})
			continue
		}
		if len(part) < 2 || part[0] != '{' || part[len(part)-1] != '}' {
			return nil, fmt.Errorf("invalid parameter segment %q in %q: parameter must occupy a whole segment as {name}, {name?} or {name...}", part, path)
		}
		inner := part[1 : len(part)-1]
		optional, catchAll := false, false
		if strings.HasSuffix(inner, "...") {
			catchAll = true
			inner = inner[:len(inner)-3]
		} else if strings.HasSuffix(inner, "?") {
			optional = true
			inner = inner[:len(inner)-1]
		}
		if !paramNamePattern.MatchString(inner) {
			return nil, fmt.Errorf("invalid parameter name %q in %q", inner, path)
		}
		if names[inner] {
			return nil, fmt.Errorf("duplicate parameter name %q in %q", inner, path)
		}
		names[inner] = true
		seenOptional = optional
		seenCatchAll = catchAll
		segments = append(segments, routeSegment{name: inner, isParam: true, optional: optional, catchAll: catchAll})
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

// Any 为全部 9 个受支持的 HTTP 方法原子注册同一路由；预检全部通过后才统一提交。
func (r *Router) Any(path string, handler any) {
	r.registerAny(path, handler, nil)
}

// =============== RouterGroup ===============

// RouterGroup 路由分组：保存父组引用与当前组自己的中间件，注册路由时再展开完整祖先链。
type RouterGroup struct {
	router      *Router
	parent      *RouterGroup
	prefix      string
	middlewares []MiddlewareHandler
}

// Use 向当前分组追加中间件，返回自身以便链式调用；只对此后注册的路由生效
func (g *RouterGroup) Use(middlewares ...MiddlewareHandler) *RouterGroup {
	g.middlewares = append(g.middlewares, middlewares...)
	return g
}

// Group 创建嵌套子分组：安全拼接前缀，并保留父组引用以在注册时展开完整中间件链。
func (g *RouterGroup) Group(prefix string, middlewares ...MiddlewareHandler) *RouterGroup {
	return &RouterGroup{
		router:      g.router,
		parent:      g,
		prefix:      normalizePrefix(joinGroupPath(g.prefix, prefix)),
		middlewares: append([]MiddlewareHandler{}, middlewares...),
	}
}

func (g *RouterGroup) middlewareChain() []MiddlewareHandler {
	groups := make([]*RouterGroup, 0, 4)
	total := 0
	for current := g; current != nil; current = current.parent {
		groups = append(groups, current)
		total += len(current.middlewares)
	}
	middlewares := make([]MiddlewareHandler, 0, total)
	for i := len(groups) - 1; i >= 0; i-- {
		middlewares = append(middlewares, groups[i].middlewares...)
	}
	return middlewares
}

func (g *RouterGroup) fullPath(path string) string {
	return joinGroupPath(g.prefix, path)
}

// GET 在分组内注册 GET 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) GET(path string, handler any) {
	g.router.register(http.MethodGet, g.fullPath(path), handler, g.middlewareChain())
}

// POST 在分组内注册 POST 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) POST(path string, handler any) {
	g.router.register(http.MethodPost, g.fullPath(path), handler, g.middlewareChain())
}

// PUT 在分组内注册 PUT 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) PUT(path string, handler any) {
	g.router.register(http.MethodPut, g.fullPath(path), handler, g.middlewareChain())
}

// DELETE 在分组内注册 DELETE 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) DELETE(path string, handler any) {
	g.router.register(http.MethodDelete, g.fullPath(path), handler, g.middlewareChain())
}

// PATCH 在分组内注册 PATCH 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) PATCH(path string, handler any) {
	g.router.register(http.MethodPatch, g.fullPath(path), handler, g.middlewareChain())
}

// HEAD 在分组内注册 HEAD 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) HEAD(path string, handler any) {
	g.router.register(http.MethodHead, g.fullPath(path), handler, g.middlewareChain())
}

// OPTIONS 在分组内注册 OPTIONS 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) OPTIONS(path string, handler any) {
	g.router.register(http.MethodOptions, g.fullPath(path), handler, g.middlewareChain())
}

// CONNECT 在分组内注册 CONNECT 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) CONNECT(path string, handler any) {
	g.router.register(http.MethodConnect, g.fullPath(path), handler, g.middlewareChain())
}

// TRACE 在分组内注册 TRACE 方法路由；注册时快照全局及祖先分组的当前中间件链。
func (g *RouterGroup) TRACE(path string, handler any) {
	g.router.register(http.MethodTrace, g.fullPath(path), handler, g.middlewareChain())
}

// Any 在分组内为全部 9 个受支持的方法原子注册同一路由。
func (g *RouterGroup) Any(path string, handler any) {
	g.router.registerAny(g.fullPath(path), handler, g.middlewareChain())
}

func joinGroupPath(prefix, path string) string {
	prefix = normalizePrefix(prefix)
	path = normalizePath(path)
	if path == "/" {
		if prefix == "" {
			return "/"
		}
		return prefix
	}
	if prefix == "" {
		return path
	}
	return prefix + path
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
