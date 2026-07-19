package zchttp

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

type Router struct {
	routes      map[string]map[string]*routeEntry
	middlewares []MiddlewareHandler // 全局中间件，作用于通过本 Router 注册的所有路由
}

func NewRouter() *Router {
	r := &Router{
		routes: make(map[string]map[string]*routeEntry),
	}
	r.routes[http.MethodGet] = make(map[string]*routeEntry)
	r.routes[http.MethodPost] = make(map[string]*routeEntry)
	r.routes[http.MethodPut] = make(map[string]*routeEntry)
	r.routes[http.MethodDelete] = make(map[string]*routeEntry)
	r.routes[http.MethodPatch] = make(map[string]*routeEntry)
	r.routes[http.MethodHead] = make(map[string]*routeEntry)
	r.routes[http.MethodOptions] = make(map[string]*routeEntry)
	r.routes[http.MethodConnect] = make(map[string]*routeEntry)
	r.routes[http.MethodTrace] = make(map[string]*routeEntry)
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
// 再检测路由冲突（复用 entry 中已计算的 handler 位置信息），最后存入路由表。
// groupMiddlewares 为分组的中间件；最终中间件链顺序为 [全局 ... , 分组 ...]
func (r *Router) register(method, path string, handler any, groupMiddlewares []MiddlewareHandler) {
	path = normalizePath(path)

	entry, err := buildEntry(handler, r.middlewares, groupMiddlewares)
	if err != nil {
		panic(err.Error())
	}

	// 注册阶段扫描整个类型树，检测 default 标签误用（包含路由信息以便定位）
	checkUnsupportedDefaults(entry.reqElemType, true, method, path, entry.handlerName, entry.handlerFile, entry.handlerLine, map[reflect.Type]bool{})
	var resElemType reflect.Type
	if entry.resIsPtr {
		resElemType = entry.resType.Elem()
	} else {
		resElemType = entry.resType
	}
	checkUnsupportedDefaults(resElemType, true, method, path, entry.handlerName, entry.handlerFile, entry.handlerLine, map[reflect.Type]bool{})

	if existing, ok := r.routes[method][path]; ok {
		panic(fmt.Sprintf(
			"route conflict: %s %s already registered by %s (%s:%d), conflicting with %s (%s:%d)",
			method, path,
			existing.handlerName, existing.handlerFile, existing.handlerLine,
			entry.handlerName, entry.handlerFile, entry.handlerLine,
		))
	}

	r.routes[method][path] = entry
}

func (r *Router) GET(path string, handler any) {
	r.register(http.MethodGet, path, handler, nil)
}

func (r *Router) POST(path string, handler any) {
	r.register(http.MethodPost, path, handler, nil)
}

func (r *Router) PUT(path string, handler any) {
	r.register(http.MethodPut, path, handler, nil)
}

func (r *Router) DELETE(path string, handler any) {
	r.register(http.MethodDelete, path, handler, nil)
}

func (r *Router) PATCH(path string, handler any) {
	r.register(http.MethodPatch, path, handler, nil)
}

func (r *Router) HEAD(path string, handler any) {
	r.register(http.MethodHead, path, handler, nil)
}

func (r *Router) OPTIONS(path string, handler any) {
	r.register(http.MethodOptions, path, handler, nil)
}

func (r *Router) CONNECT(path string, handler any) {
	r.register(http.MethodConnect, path, handler, nil)
}

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

func (g *RouterGroup) GET(path string, handler any) {
	g.router.register(http.MethodGet, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) POST(path string, handler any) {
	g.router.register(http.MethodPost, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) PUT(path string, handler any) {
	g.router.register(http.MethodPut, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) DELETE(path string, handler any) {
	g.router.register(http.MethodDelete, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) PATCH(path string, handler any) {
	g.router.register(http.MethodPatch, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) HEAD(path string, handler any) {
	g.router.register(http.MethodHead, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) OPTIONS(path string, handler any) {
	g.router.register(http.MethodOptions, g.prefix+path, handler, g.middlewares)
}

func (g *RouterGroup) CONNECT(path string, handler any) {
	g.router.register(http.MethodConnect, g.prefix+path, handler, g.middlewares)
}

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

// normalizePath 规范化路由路径：去除末尾的 "/"（如 /hello/ -> /hello），
// 使 /hello 与 /hello/ 等价；根路径 "/" 与空串统一返回 "/"
func normalizePath(p string) string {
	if len(p) > 1 {
		p = strings.TrimRight(p, "/")
	}
	if p == "" {
		p = "/"
	}
	return p
}
