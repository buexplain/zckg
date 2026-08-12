package zchttp

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type helloReq struct {
	Name string `json:"name"`
}
type helloRes struct {
	Message string `json:"message"`
}

func hello(_ context.Context, req helloReq) (helloRes, error) {
	return helloRes{Message: "Hello, " + req.Name}, nil
}

func TestBasic(t *testing.T) {
	router := NewRouter()
	router.GET("/", hello)
	t.Log("BasicTest")
}

// helloPtr 使用结构体指针作为参数与返回值，验证指针类型支持
func helloPtr(_ context.Context, req *helloReq) (*helloRes, error) {
	return &helloRes{Message: "Hello, " + req.Name}, nil
}

// TestPointerHandler 验证 handler 可使用结构体指针作为 Req/Res
func TestPointerHandler(t *testing.T) {
	router := NewRouter()
	router.POST("/ptr", helloPtr)

	engine := NewEngine()
	engine.Router = router

	body := strings.NewReader(`{"name":"world"}`)
	req := httptest.NewRequest(http.MethodPost, "/ptr", body)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}
	var res helloRes
	decodeData(t, rec, &res)
	if res.Message != "Hello, world" {
		t.Fatalf("unexpected response message: %s", res.Message)
	}
}

// TestOnionMiddlewareOrder 验证洋葱模型中间件的执行顺序：
// 前置逻辑按注册顺序执行，handler 在最内层，后置逻辑按逆序执行
func TestOnionMiddlewareOrder(t *testing.T) {
	var order []string

	mwA := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "A-before")
		err := next()
		order = append(order, "A-after")
		return err
	}

	mwB := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "B-before")
		err := next()
		order = append(order, "B-after")
		return err
	}

	router := NewRouter()
	router.Use(mwA, mwB)
	router.POST("/test", func(ctx context.Context, req helloReq) (helloRes, error) {
		order = append(order, "handler")
		return helloRes{Message: "Hello, " + req.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := strings.NewReader(`{"name":"world"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	expected := []string{"A-before", "B-before", "handler", "B-after", "A-after"}
	if len(order) != len(expected) {
		t.Fatalf("execution order length mismatch: got %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("execution order mismatch at index %d: got %q, want %q", i, order[i], v)
		}
	}

	var res helloRes
	decodeData(t, rec, &res)
	if res.Message != "Hello, world" {
		t.Fatalf("unexpected response message: %s", res.Message)
	}
}

// TestOnionMiddlewareShortCircuit 验证中间件不调用 next 时短路，后续中间件与 handler 不执行
func TestOnionMiddlewareShortCircuit(t *testing.T) {
	var order []string

	mwA := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "A-before")
		// 不调用 next，直接短路
		order = append(order, "A-after")
		return nil
	}

	mwB := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "B-before")
		err := next()
		order = append(order, "B-after")
		return err
	}

	router := NewRouter()
	router.Use(mwA, mwB)
	router.POST("/test", func(ctx context.Context, req helloReq) (helloRes, error) {
		order = append(order, "handler")
		return helloRes{Message: "Hello, " + req.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := strings.NewReader(`{"name":"world"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	// mwA 不调用 next，所以 mwB 和 handler 不应执行
	expected := []string{"A-before", "A-after"}
	if len(order) != len(expected) {
		t.Fatalf("execution order length mismatch: got %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("execution order mismatch at index %d: got %q, want %q", i, order[i], v)
		}
	}
}

// TestOnionMiddlewareErrorPropagation 验证 handler 错误通过洋葱链向上传播，
// 中间件可拦截错误，未被拦截的错误最终由引擎返回 500
func TestOnionMiddlewareErrorPropagation(t *testing.T) {
	var capturedErr error

	mwA := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		err := next()
		if err != nil {
			capturedErr = err
		}
		return err
	}

	router := NewRouter()
	router.Use(mwA)
	router.POST("/test", func(ctx context.Context, req helloReq) (helloRes, error) {
		return helloRes{}, fmt.Errorf("handler error")
	})

	engine := NewEngine()
	engine.Router = router

	body := strings.NewReader(`{"name":"world"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if capturedErr == nil {
		t.Fatal("middleware should have captured the handler error")
	}
	if capturedErr.Error() != "handler error" {
		t.Fatalf("unexpected captured error: %v", capturedErr)
	}
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

// ======== P0 补充测试 ========

// TestRouteConflictPanic 验证同一 method+path 注册两次时 panic 并包含冲突信息
func TestRouteConflictPanic(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected panic on route conflict, got none")
		}
		msg := fmt.Sprintf("%v", r)
		if !strings.Contains(msg, "route conflict") {
			t.Fatalf("panic message should contain 'route conflict', got: %s", msg)
		}
		if !strings.Contains(msg, "POST") || !strings.Contains(msg, "/dup") {
			t.Fatalf("panic message should contain method and path, got: %s", msg)
		}
	}()

	router := NewRouter()
	router.POST("/dup", hello)
	router.POST("/dup", hello) // 重复注册，应 panic
}

// TestSamePathDifferentMethodNoConflict 验证相同路径不同方法不冲突
func TestSamePathDifferentMethodNoConflict(t *testing.T) {
	router := NewRouter()
	router.GET("/api", hello)
	router.POST("/api", hello)
	router.PUT("/api", hello)
	router.DELETE("/api", hello)
	// 无 panic 即通过
}

// TestGroupPrefixConcat 验证 Group 路由前缀拼接正确
func TestGroupPrefixConcat(t *testing.T) {
	router := NewRouter()
	api := router.Group("/api")
	api.GET("/users", hello)
	api.POST("/users", hello)

	engine := NewEngine()
	engine.Router = router

	// GET /api/users 应可达
	req := httptest.NewRequest(http.MethodGet, "/api/users?name=test", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/users expected 200, got %d", rec.Code)
	}

	// POST /api/users 应可达
	body := strings.NewReader(`{"name":"test"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/users", body)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/users expected 200, got %d", rec.Code)
	}

	// GET /users（无前缀）应 404
	req = httptest.NewRequest(http.MethodGet, "/users?name=test", nil)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /users expected 404, got %d", rec.Code)
	}
}

// TestNestedSubGroup 验证嵌套子分组前缀叠加
func TestNestedSubGroup(t *testing.T) {
	router := NewRouter()
	api := router.Group("/api")
	v1 := api.Group("/v1")
	v1.GET("/items", hello)

	engine := NewEngine()
	engine.Router = router

	// GET /api/v1/items 应可达
	req := httptest.NewRequest(http.MethodGet, "/api/v1/items?name=test", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/items expected 200, got %d", rec.Code)
	}

	// GET /api/items 应 404
	req = httptest.NewRequest(http.MethodGet, "/api/items?name=test", nil)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /api/items expected 404, got %d", rec.Code)
	}
}

// TestGroupMiddlewareWithGlobalMiddleware 验证全局中间件 + 分组中间件的叠加顺序
func TestGroupMiddlewareWithGlobalMiddleware(t *testing.T) {
	var order []string

	globalMW := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "global-before")
		err := next()
		order = append(order, "global-after")
		return err
	}

	groupMW := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "group-before")
		err := next()
		order = append(order, "group-after")
		return err
	}

	router := NewRouter()
	router.Use(globalMW)
	api := router.Group("/api", groupMW)
	api.POST("/action", func(ctx context.Context, req helloReq) (helloRes, error) {
		order = append(order, "handler")
		return helloRes{Message: req.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := strings.NewReader(`{"name":"test"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/action", body)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// 全局在外层，分组在内层
	expected := []string{"global-before", "group-before", "handler", "group-after", "global-after"}
	if len(order) != len(expected) {
		t.Fatalf("execution order = %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// TestNestedGroupMiddlewareInheritance 验证子分组继承父分组中间件
func TestNestedGroupMiddlewareInheritance(t *testing.T) {
	var order []string

	parentMW := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "parent")
		return next()
	}
	childMW := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "child")
		return next()
	}

	router := NewRouter()
	api := router.Group("/api", parentMW)
	v1 := api.Group("/v1", childMW)
	v1.POST("/run", func(ctx context.Context, req helloReq) (helloRes, error) {
		order = append(order, "handler")
		return helloRes{}, nil
	})

	engine := NewEngine()
	engine.Router = router

	body := strings.NewReader(`{"name":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/run", body)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	expected := []string{"parent", "child", "handler"}
	if len(order) != len(expected) {
		t.Fatalf("order = %v, want %v", order, expected)
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// TestAllHTTPMethods 验证所有 HTTP 方法均可注册并正确路由
func TestAllHTTPMethods(t *testing.T) {
	router := NewRouter()
	router.GET("/m", hello)
	router.POST("/m", hello)
	router.PUT("/m", hello)
	router.DELETE("/m", hello)
	router.PATCH("/m", hello)
	router.HEAD("/m", hello)
	router.OPTIONS("/m", hello)
	router.CONNECT("/m", hello)
	router.TRACE("/m", hello)

	engine := NewEngine()
	engine.Router = router

	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodPatch, http.MethodHead,
		http.MethodOptions, http.MethodConnect, http.MethodTrace,
	}

	for _, method := range methods {
		var req *http.Request
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete {
			req = httptest.NewRequest(method, "/m?name=test", nil)
		} else {
			req = httptest.NewRequest(method, "/m", strings.NewReader(`{"name":"test"}`))
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s /m expected 200, got %d", method, rec.Code)
		}
	}
}

// ======== P1 补充测试 ========

// TestNormalizePrefix 验证 normalizePrefix 规范化逻辑
func TestNormalizePrefix(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"/", ""},
		{"api", "/api"},
		{"/api", "/api"},
		{"/api/", "/api"},
		{"api/", "/api"},
		{"/api/v1/", "/api/v1"},
	}
	for _, c := range cases {
		got := normalizePrefix(c.input)
		if got != c.want {
			t.Errorf("normalizePrefix(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestNormalizePath 验证 normalizePath 规范化逻辑
func TestNormalizePath(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"", "/"},
		{"/", "/"},
		{"/hello", "/hello"},
		{"/hello/", "/hello"},
		{"/hello/world/", "/hello/world"},
		{"hello", "/hello"},
		{"hello/", "/hello"},
		{"hello/world", "/hello/world"},
		{"//", "/"},
	}
	for _, c := range cases {
		got := normalizePath(c.input)
		if got != c.want {
			t.Errorf("normalizePath(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// TestRouteWithoutLeadingSlash 验证无前导斜杠的路径注册后可被正常匹配：
// r.URL.Path 永远以 "/" 开头，若注册键不补前导斜杠则路由永不命中
func TestRouteWithoutLeadingSlash(t *testing.T) {
	router := NewRouter()
	router.GET("hello", hello)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/hello?name=world", nil)
	rec := httptest.NewRecorder()

	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("route registered without leading slash never matches: status = %d, want 200", rec.Code)
	}
	var res helloRes
	decodeData(t, rec, &res)
	if res.Message != "Hello, world" {
		t.Fatalf("unexpected response message: %s", res.Message)
	}
}

// TestInvalidHandlerPanic 验证注册无效签名的 handler 时 panic
func TestInvalidHandlerPanic(t *testing.T) {
	cases := []struct {
		name    string
		handler any
	}{
		{"nil handler", nil},
		{"string handler", "not a func"},
		{"wrong args count", func() {}},
		{"wrong return count", func(ctx context.Context, req helloReq) helloRes { return helloRes{} }},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("expected panic for handler %v, got none", c.name)
				}
			}()
			router := NewRouter()
			router.POST("/bad", c.handler)
		})
	}
}

// TestUseChainingRouter 验证 Router.Use 链式调用返回 *Router
func TestUseChainingRouter(t *testing.T) {
	mw := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}

	router := NewRouter()
	result := router.Use(mw).Use(mw)
	if result != router {
		t.Fatal("Use() should return the same *Router for chaining")
	}
}

// TestUseChainingGroup 验证 RouterGroup.Use 链式调用返回 *RouterGroup
func TestUseChainingGroup(t *testing.T) {
	mw := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}

	router := NewRouter()
	group := router.Group("/api")
	result := group.Use(mw).Use(mw)
	if result != group {
		t.Fatal("RouterGroup.Use() should return the same *RouterGroup for chaining")
	}
}

// TestGroupUseOnlyAffectsSubsequentRoutes 验证 Group.Use 只对此后注册的路由生效
func TestGroupUseOnlyAffectsSubsequentRoutes(t *testing.T) {
	var order []string

	lateMW := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		order = append(order, "late")
		return next()
	}

	router := NewRouter()
	api := router.Group("/api")

	// 先注册路由，再追加中间件
	api.POST("/before", func(ctx context.Context, req helloReq) (helloRes, error) {
		order = append(order, "handler-before")
		return helloRes{}, nil
	})
	api.Use(lateMW)
	api.POST("/after", func(ctx context.Context, req helloReq) (helloRes, error) {
		order = append(order, "handler-after")
		return helloRes{}, nil
	})

	engine := NewEngine()
	engine.Router = router

	// /api/before：注册时中间件链未含 lateMW
	order = nil
	body := strings.NewReader(`{"name":"x"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/before", body)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	for _, v := range order {
		if v == "late" {
			t.Fatal("/api/before should NOT have lateMW")
		}
	}

	// /api/after：注册时中间件链已含 lateMW
	order = nil
	body = strings.NewReader(`{"name":"x"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/after", body)
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	foundLate := false
	for _, v := range order {
		if v == "late" {
			foundLate = true
		}
	}
	if !foundLate {
		t.Fatal("/api/after should have lateMW")
	}
}

// TestRouterGroupAllHTTPMethods 验证 RouterGroup 的所有 HTTP 方法快捷注册
func TestRouterGroupAllHTTPMethods(t *testing.T) {
	router := NewRouter()
	api := router.Group("/api")
	api.GET("/m", hello)
	api.POST("/m", hello)
	api.PUT("/m", hello)
	api.DELETE("/m", hello)
	api.PATCH("/m", hello)
	api.HEAD("/m", hello)
	api.OPTIONS("/m", hello)
	api.CONNECT("/m", hello)
	api.TRACE("/m", hello)

	engine := NewEngine()
	engine.Router = router

	methods := []string{
		http.MethodGet, http.MethodPost, http.MethodPut,
		http.MethodDelete, http.MethodPatch, http.MethodHead,
		http.MethodOptions, http.MethodConnect, http.MethodTrace,
	}

	for _, method := range methods {
		var req *http.Request
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodDelete {
			req = httptest.NewRequest(method, "/api/m?name=test", nil)
		} else {
			req = httptest.NewRequest(method, "/api/m", strings.NewReader(`{"name":"test"}`))
		}
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("RouterGroup %s /api/m expected 200, got %d", method, rec.Code)
		}
	}
}

// TestMiddlewareNextCalledTwice 验证中间件重复调用 next() 时下游链与 handler 只执行一次：
// 若无保护，handler 会被重复执行，写库等业务副作用重复触发且完全静默
func TestMiddlewareNextCalledTwice(t *testing.T) {
	var handlerCalls int
	var secondErr error

	mw := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		if err := next(); err != nil {
			return err
		}
		// 模拟误写：重复调用 next()
		secondErr = next()
		return nil
	}

	router := NewRouter()
	router.Use(mw)
	router.POST("/twice", func(ctx context.Context, req helloReq) (helloRes, error) {
		handlerCalls++
		return helloRes{Message: "ok"}, nil
	})

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodPost, "/twice", strings.NewReader(`{"name":"x"}`))
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if handlerCalls != 1 {
		t.Fatalf("handler executed %d times, want 1 (duplicate next() re-runs downstream chain)", handlerCalls)
	}
	if secondErr == nil {
		t.Fatal("second next() call should return error instead of silently re-running downstream")
	}
}

// ======== 路由参数：注册期校验 ========

// TestParamRouteInvalidSyntaxPanic 验证参数路由路径语法非法时注册 panic
func TestParamRouteInvalidSyntaxPanic(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		errPart string
	}{
		{"digit-leading name", "/p/{1id}", "invalid parameter name"},
		{"empty name", "/p/{}", "invalid parameter name"},
		{"param mixed with literal", "/p/abc{id}", "invalid parameter segment"},
		{"required after optional", "/p/{post_id?}/{comment_id}", "not allowed after optional"},
		{"duplicate name", "/p/{post_id}/{post_id}", "duplicate parameter name"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			expectPanicContains(t, []string{"invalid route path", c.errPart}, func() {
				router := NewRouter()
				router.GET(c.path, paramEcho)
			})
		})
	}
}

// TestParamRouteNoFieldPanic 验证参数名在 Req 中无对应字段时注册 panic
func TestParamRouteNoFieldPanic(t *testing.T) {
	expectPanicContains(t, []string{"{nope}", "no corresponding field", "helloReq"}, func() {
		router := NewRouter()
		router.GET("/p/{nope}", hello)
	})
}

// TestMatchParam_UnknownMethod 覆盖 paramTrees 中无对应 method 时直接返回 nil 的分支
func TestMatchParam_UnknownMethod(t *testing.T) {
	router := NewRouter()
	entry, vals := router.matchParam("FOO", "/a/b")
	if entry != nil || vals != nil {
		t.Fatalf("expected nil for unknown method, got %v %v", entry, vals)
	}
	// 未初始化 paramTrees 的 Router（root 为 nil）同样安全返回 nil
	bare := &Router{}
	entry, vals = bare.matchParam(http.MethodGet, "/a/b")
	if entry != nil || vals != nil {
		t.Fatalf("expected nil for bare router, got %v %v", entry, vals)
	}
	// 根路径 "/" 归一化为空串后无可选参数注册时应未命中
	entry, vals = router.matchParam(http.MethodGet, "/")
	if entry != nil || vals != nil {
		t.Fatalf("expected nil for root path, got %v %v", entry, vals)
	}
}

// TestParamRouteOptionalDuplicatePanic 验证可选参数终点 entries[1] 重复注册时 panic
func TestParamRouteOptionalDuplicatePanic(t *testing.T) {
	type optReq struct {
		ID string `json:"id"`
	}
	type optRes struct{}
	h := func(_ context.Context, req optReq) (optRes, error) {
		return optRes{}, nil
	}
	expectPanicContains(t, []string{"conflict"}, func() {
		router := NewRouter()
		router.GET("/opt/{id?}", h)
		router.GET("/opt/{id?}", h)
	})
}

// TestParamRouteFileFieldPanic 验证参数绑定目标为文件字段时注册 panic
func TestParamRouteFileFieldPanic(t *testing.T) {
	type fileParamReq struct {
		File *multipart.FileHeader `json:"file"`
	}
	type fileParamRes struct{}
	handler := func(_ context.Context, req fileParamReq) (fileParamRes, error) {
		return fileParamRes{}, nil
	}
	expectPanicContains(t, []string{"{file}", "cannot bind to file field"}, func() {
		router := NewRouter()
		router.GET("/p/{file}", handler)
	})
}
