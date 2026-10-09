package zchttp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// expectPanicContains 断言 fn 触发 panic，且 panic 消息包含所有 want 子串
func expectPanicContains(t *testing.T, want []string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %v, got none", want)
		}
		msg := fmt.Sprintf("%v", r)
		for _, w := range want {
			if !strings.Contains(msg, w) {
				t.Fatalf("panic message should contain %q, got: %s", w, msg)
			}
		}
	}()
	fn()
}

// ======== splitPathSegments / parseRoutePath 单元测试 ========

func TestSplitPathSegments(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"/", nil},
		{"/a", []string{"a"}},
		{"/a/b", []string{"a", "b"}},
		{"/posts/{post_id}", []string{"posts", "{post_id}"}},
	}
	for _, c := range cases {
		got := splitPathSegments(c.input)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitPathSegments(%q) = %v, want %v", c.input, got, c.want)
		}
	}
}

func TestParseRoutePathValid(t *testing.T) {
	segments, err := parseRoutePath("/posts/{post_id}/comments/{comment_id?}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []routeSegment{
		{literal: "posts"},
		{name: "post_id", isParam: true},
		{literal: "comments"},
		{name: "comment_id", isParam: true, optional: true},
	}
	if !reflect.DeepEqual(segments, want) {
		t.Fatalf("segments = %+v, want %+v", segments, want)
	}
}

func TestParseRoutePathInvalid(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		errPart string
	}{
		{"invalid name starts with digit", "/p/{1id}", "invalid parameter name"},
		{"empty name", "/p/{}", "invalid parameter name"},
		{"invalid char in name", "/p/{a-b}", "invalid parameter name"},
		{"param not whole segment prefix", "/p/abc{id}", "invalid parameter segment"},
		{"param not whole segment suffix", "/p/{id}x", "invalid parameter segment"},
		{"unbalanced braces", "/p/{id", "invalid parameter segment"},
		{"required after optional param", "/p/{a?}/{b}", "not allowed after optional"},
		{"static after optional param", "/p/{a?}/x", "not allowed after optional"},
		{"duplicate param name", "/p/{a}/{a}", "duplicate parameter name"},
		{"empty segment", "/p//{a}", "empty path segment"},
		{"param after catch-all", "/p/{rest...}/{b}", "not allowed after catch-all"},
		{"static after catch-all", "/p/{rest...}/x", "not allowed after catch-all"},
		{"optional then catch-all suffix", "/p/{a?...}", `invalid parameter name "a?"`},
		{"catch-all then optional suffix", "/p/{a...?}", `invalid parameter name "a..."`},
		{"catch-all without name", "/p/{...}", `invalid parameter name ""`},
		{"catch-all not whole segment", "/p/x{a...}", "{name...}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := parseRoutePath(c.path)
			if err == nil {
				t.Fatalf("expected error containing %q for path %q, got nil", c.errPart, c.path)
			}
			if !strings.Contains(err.Error(), c.errPart) {
				t.Fatalf("error should contain %q, got: %v", c.errPart, err)
			}
		})
	}
}

// TestParseRoutePathCatchAllValid 验证通配尾段 {name...} 解析为 isParam=true、catchAll=true，
// 且不标记 optional；覆盖单独使用、跟在必选参数后、根路径三种合法位置
func TestParseRoutePathCatchAllValid(t *testing.T) {
	cases := []struct {
		name string
		path string
		want []routeSegment
	}{
		{"alone after static", "/dify/{rest...}", []routeSegment{
			{literal: "dify"},
			{name: "rest", isParam: true, catchAll: true},
		}},
		{"after required param", "/a/{x}/{rest...}", []routeSegment{
			{literal: "a"},
			{name: "x", isParam: true},
			{name: "rest", isParam: true, catchAll: true},
		}},
		{"root", "/{rest...}", []routeSegment{
			{name: "rest", isParam: true, catchAll: true},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			segments, err := parseRoutePath(c.path)
			if err != nil {
				t.Fatalf("parseRoutePath(%q) unexpected error: %v", c.path, err)
			}
			if !reflect.DeepEqual(segments, c.want) {
				t.Fatalf("segments = %+v, want %+v", segments, c.want)
			}
		})
	}
}

// ======== 基数树注册与匹配 ========

// paramReq 覆盖参数路由测试所需的各类字段
type paramReq struct {
	PostID    int    `json:"post_id"`
	CommentID int64  `json:"comment_id"`
	ID        string `json:"id"`
	X         string `json:"x"`
	Y         string `json:"y"`
}
type paramRes struct {
	Echo string `json:"echo"`
}

func paramEcho(_ context.Context, req paramReq) (paramRes, error) {
	return paramRes{Echo: fmt.Sprintf("post=%d comment=%d id=%s x=%s", req.PostID, req.CommentID, req.ID, req.X)}, nil
}

func TestTrieRequiredParamMatch(t *testing.T) {
	router := NewRouter()
	router.GET("/posts/{post_id}", paramEcho)

	entry, values := router.match("GET", "/posts/42")
	if entry == nil {
		t.Fatal("expected match, got nil")
	}
	if !reflect.DeepEqual(values, []string{"42"}) {
		t.Fatalf("captured values = %v, want [42]", values)
	}
	if entry, values := router.match("GET", "/posts"); entry == nil || values != nil {
		// /posts 无终点 entry，应不命中
		if e, _ := router.match("GET", "/posts"); e != nil {
			t.Fatal("/posts should not match /posts/{post_id}")
		}
	}
}

func TestTrieOptionalParamMatch(t *testing.T) {
	router := NewRouter()
	router.GET("/user/{name?}", hello)

	// 提供参数时
	entry, values := router.match("GET", "/user/guest")
	if entry == nil || !reflect.DeepEqual(values, []string{"guest"}) {
		t.Fatalf("match /user/guest = %v, %v; want hit with [guest]", entry, values)
	}
	// 省略可选参数时
	entry, values = router.match("GET", "/user")
	if entry == nil || len(values) != 0 {
		t.Fatalf("match /user = %v, %v; want omitted branch with empty values", entry, values)
	}
	// 多一段不命中
	if e, _ := router.match("GET", "/user/a/b"); e != nil {
		t.Fatal("/user/a/b should not match /user/{name?}")
	}
}

func TestTrieMultiParamsTrailingOptional(t *testing.T) {
	router := NewRouter()
	router.GET("/posts/{post_id}/comments/{comment_id?}", paramEcho)

	entry, values := router.match("GET", "/posts/1/comments")
	if entry == nil || !reflect.DeepEqual(values, []string{"1"}) {
		t.Fatalf("omitted trailing optional: entry=%v values=%v", entry, values)
	}
	entry, values = router.match("GET", "/posts/1/comments/2")
	if entry == nil || !reflect.DeepEqual(values, []string{"1", "2"}) {
		t.Fatalf("present trailing optional: entry=%v values=%v", entry, values)
	}
	if e, _ := router.match("GET", "/posts/1"); e != nil {
		t.Fatal("/posts/1 should not match (no terminal entry)")
	}
}

func TestTrieStaticPreferredOverParam(t *testing.T) {
	router := NewRouter()
	router.GET("/user/list", hello)
	router.GET("/user/{name}", hello)

	// 静态段优先于参数段：/user/list 命中静态路由，不捕获参数
	entry, values := router.match("GET", "/user/list")
	if entry == nil || values != nil {
		t.Fatalf("match /user/list should hit static branch with no captured values, got %v, %v", entry, values)
	}
	// /user/other 回退参数分支
	entry, values = router.match("GET", "/user/other")
	if entry == nil || !reflect.DeepEqual(values, []string{"other"}) {
		t.Fatalf("match /user/other should fall back to param branch, got %v, %v", entry, values)
	}
	// 引擎层面：静态路由优先于参数路由
	engine := NewEngine()
	engine.Router = router
	req := httptest.NewRequest(http.MethodGet, "/user/list?name=ignored", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("GET /user/list expected 200, got %d", rec.Code)
	}
}

func TestTrieBacktrackToParamBranch(t *testing.T) {
	router := NewRouter()
	router.GET("/a/{x}/b", paramEcho)
	router.GET("/a/static/c", hello)

	// 静态分支 static->c 在请求 /a/static/b 下失败，应回溯参数分支命中 x=static
	entry, values := router.match("GET", "/a/static/b")
	if entry == nil || !reflect.DeepEqual(values, []string{"static"}) {
		t.Fatalf("backtrack match failed: entry=%v values=%v", entry, values)
	}
}

func TestTrieParamConflictPanics(t *testing.T) {
	// 同一路径重复注册
	expectPanicContains(t, []string{"route conflict", "GET", "/posts/{post_id}"}, func() {
		router := NewRouter()
		router.GET("/posts/{post_id}", paramEcho)
		router.GET("/posts/{post_id}", paramEcho)
	})
	// 同一位置参数名不一致
	expectPanicContains(t, []string{"parameter name conflict", "{x}", "{y}"}, func() {
		router := NewRouter()
		router.GET("/a/{x}/b", paramEcho)
		router.GET("/a/{y}/c", paramEcho)
	})
	// 同一参数可选性不一致
	expectPanicContains(t, []string{"optionality conflict", "{post_id}"}, func() {
		router := NewRouter()
		router.GET("/p/{post_id}", paramEcho)
		router.GET("/p/{post_id?}", paramEcho)
	})
}

// TestMatchParamNilTree 覆盖基数树未初始化时 match 的提前返回分支
func TestMatchParamNilTree(t *testing.T) {
	r := &Router{}
	entry, values := r.match(http.MethodGet, "/x")
	if entry != nil || values != nil {
		t.Fatalf("expected nil entry and values, got %v / %v", entry, values)
	}
}

// TestTrieSharedStaticPrefix 覆盖两条路由共享静态前缀时中间节点复用的分支
func TestTrieSharedStaticPrefix(t *testing.T) {
	router := NewRouter()
	router.GET("/api/shared/x/{id}", paramEcho)
	router.GET("/api/shared/y/{id}", paramEcho)

	for _, p := range []string{"/api/shared/x/1", "/api/shared/y/2"} {
		entry, values := router.match(http.MethodGet, p)
		if entry == nil || len(values) != 1 {
			t.Fatalf("route %s should match, got entry=%v values=%v", p, entry, values)
		}
	}
}

// TestRouteConflictPanic_NilExisting 覆盖 existing 为 nil 时的冲突提示格式
// （中间节点冲突且该节点尚无终点 entry 的防御分支，正常注册路径不可达）。
func TestRouteConflictPanic_NilExisting(t *testing.T) {
	incoming := &routeEntry{handlerName: "pkg.H", handlerFile: "h.go", handlerLine: 9}
	defer func() {
		msg, ok := recover().(string)
		if !ok {
			t.Fatal("应 panic")
		}
		if !strings.Contains(msg, "route conflict") || !strings.Contains(msg, "pkg.H") || strings.Contains(msg, "already registered") {
			t.Fatalf("existing=nil 的冲突消息格式不符: %s", msg)
		}
	}()
	routeConflictPanic("GET", "/x", nil, incoming, "")
}

// ======== 通配尾段 {name...} ========

// catchAllReq 覆盖通配路由测试所需字段：rest/other 为通配名，x 为参数名
type catchAllReq struct {
	Rest  string `json:"rest"`
	Other string `json:"other"`
	X     string `json:"x"`
}

// 以下 handler 仅用于区分命中的是哪条路由（按 handlerName 后缀识别）
func caRest(_ context.Context, _ catchAllReq) (paramRes, error)     { return paramRes{}, nil }
func caParam(_ context.Context, _ catchAllReq) (paramRes, error)    { return paramRes{}, nil }
func caStatic(_ context.Context, _ catchAllReq) (paramRes, error)   { return paramRes{}, nil }
func caOptional(_ context.Context, _ catchAllReq) (paramRes, error) { return paramRes{}, nil }

// trieMatchCase 描述一次 GET 匹配的期望：handler 为期望命中的 handler 函数名（"" 表示不命中），
// values 为期望捕获值（nil 与空切片等价，均表示无捕获）
type trieMatchCase struct {
	name    string
	path    string
	handler string
	values  []string
}

// assertTrieMatches 对每个用例先经 normalizePath 归一化（与 ServeHTTP 一致）再调用 router.match，
// 断言命中的 handler 与捕获值
func assertTrieMatches(t *testing.T, router *Router, cases []trieMatchCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Helper()
			entry, values := router.match(http.MethodGet, normalizePath(c.path))
			if c.handler == "" {
				if entry != nil {
					t.Fatalf("match %q should miss, got handler %s values %q", c.path, entry.handlerName, values)
				}
				return
			}
			if entry == nil {
				t.Fatalf("match %q should hit %s, got nil", c.path, c.handler)
			}
			if !strings.HasSuffix(entry.handlerName, "."+c.handler) {
				t.Fatalf("match %q hit %s, want %s", c.path, entry.handlerName, c.handler)
			}
			if !(len(values) == 0 && len(c.values) == 0) && !reflect.DeepEqual(values, c.values) {
				t.Fatalf("match %q captured %q, want %q", c.path, values, c.values)
			}
		})
	}
}

// TestTrieCatchAllMatch 锁死通配尾段的捕获口径：多段捕获保留原始子串（含内部 "/"）；
// 零段（/dify、/dify/）命中但不追加捕获值；末尾 "/" 经归一化去除（/dify/v1/ → v1）；
// 中间空段不折叠（/dify//x → "/x"）；字面 ".."、"." 原样捕获不做路径清理；
// 前缀不完整（/difyx）不命中；根路径通配 /{rest...} 对 "/" 零段命中
func TestTrieCatchAllMatch(t *testing.T) {
	router := NewRouter()
	router.GET("/dify/{rest...}", caRest)
	assertTrieMatches(t, router, []trieMatchCase{
		{"multi segments", "/dify/v1/chat-messages", "caRest", []string{"v1/chat-messages"}},
		{"single segment", "/dify/health", "caRest", []string{"health"}},
		{"zero segment", "/dify", "caRest", nil},
		{"zero segment trailing slash", "/dify/", "caRest", nil},
		{"trailing slash trimmed", "/dify/v1/", "caRest", []string{"v1"}},
		{"inner empty segment kept", "/dify//x", "caRest", []string{"/x"}},
		{"dot dot not cleaned", "/dify/../etc/passwd", "caRest", []string{"../etc/passwd"}},
		{"dot not cleaned", "/dify/./a", "caRest", []string{"./a"}},
		{"prefix mismatch", "/difyx", "", nil},
		{"other path", "/other/x", "", nil},
	})

	root := NewRouter()
	root.GET("/{rest...}", caRest)
	assertTrieMatches(t, root, []trieMatchCase{
		{"root zero segment", "/", "caRest", nil},
		{"root multi segments", "/a/b", "caRest", []string{"a/b"}},
	})
}

// TestTrieCatchAllAfterRequiredParam 验证通配尾段跟在必选参数后时捕获值按注册顺序排列，
// 且通配零段时只有必选参数被捕获；必选参数缺失时不命中
func TestTrieCatchAllAfterRequiredParam(t *testing.T) {
	router := NewRouter()
	router.GET("/a/{x}/{rest...}", caRest)
	assertTrieMatches(t, router, []trieMatchCase{
		{"param and rest", "/a/1/2/3", "caRest", []string{"1", "2/3"}},
		{"param only", "/a/1", "caRest", []string{"1"}},
		{"missing required param", "/a", "", nil},
	})
}

// TestTrieCatchAllBacktrackFromStatic 验证静态深链完整命中时优先；静态链在更深层失败
// （无后续段或终点缺失）时回溯到上层通配槽，捕获值从通配起点算起，包含已被静态分支切出的段
func TestTrieCatchAllBacktrackFromStatic(t *testing.T) {
	router := NewRouter()
	router.GET("/a/b/c", caStatic)
	router.GET("/a/{rest...}", caRest)
	assertTrieMatches(t, router, []trieMatchCase{
		{"static deep chain", "/a/b/c", "caStatic", nil},
		{"static fails at last segment", "/a/b/d", "caRest", []string{"b/d"}},
		{"static node without terminal", "/a/b", "caRest", []string{"b"}},
		{"static chain too long", "/a/b/c/d", "caRest", []string{"b/c/d"}},
	})
}

// TestTrieCatchAllLowestPriority 锁死同节点优先级 静态 > 参数 > 通配：
// 静态与参数分别命中各自路由；参数节点无后续匹配时回溯通配（多段落通配）；
// 参数节点无终点 entry 时同样回溯通配；路径耗尽时终点 entry 优先于通配零段
func TestTrieCatchAllLowestPriority(t *testing.T) {
	router := NewRouter()
	router.GET("/a/s", caStatic)
	router.GET("/a/{x}", caParam)
	router.GET("/a/{rest...}", caRest)
	router.GET("/b/{x}/z", caParam)
	router.GET("/b/{rest...}", caRest)
	router.GET("/c", caStatic)
	router.GET("/c/{rest...}", caRest)
	assertTrieMatches(t, router, []trieMatchCase{
		{"static first", "/a/s", "caStatic", nil},
		{"param second", "/a/1", "caParam", []string{"1"}},
		{"multi segments fall to catch-all", "/a/1/2", "caRest", []string{"1/2"}},
		{"static prefix then extra segments", "/a/s/t", "caRest", []string{"s/t"}},
		{"zero segment catch-all", "/a", "caRest", nil},
		{"param node without terminal", "/b/1", "caRest", []string{"1"}},
		{"param deep hit", "/b/1/z", "caParam", []string{"1"}},
		{"param deep miss", "/b/1/y", "caRest", []string{"1/y"}},
		{"terminal entry over zero segment", "/c", "caStatic", nil},
		{"catch-all after terminal", "/c/x", "caRest", []string{"x"}},
	})
}

// TestTrieCatchAllCrossLevelPriority 锁死跨层优先级：静态优先贯穿各层，
// /a/{x} 与 /a/b/{rest...} 并存时 /a/b 走静态 b 链命中通配零段（而非 {x}=b），
// 其余首段仍命中 {x}
func TestTrieCatchAllCrossLevelPriority(t *testing.T) {
	router := NewRouter()
	router.GET("/a/{x}", caParam)
	router.GET("/a/b/{rest...}", caRest)
	assertTrieMatches(t, router, []trieMatchCase{
		{"static chain zero segment", "/a/b", "caRest", nil},
		{"static chain with rest", "/a/b/1", "caRest", []string{"1"}},
		{"param on other segment", "/a/c", "caParam", []string{"c"}},
	})
}

// TestTrieCatchAllCoexistOptional 锁死同节点可选参数与通配并存时的零段优先级：
// 可选参数省略分支优先于通配零段；单段命中可选参数；多段落通配
func TestTrieCatchAllCoexistOptional(t *testing.T) {
	router := NewRouter()
	router.GET("/a/{x?}", caOptional)
	router.GET("/a/{rest...}", caRest)
	assertTrieMatches(t, router, []trieMatchCase{
		{"optional omitted over zero segment", "/a", "caOptional", nil},
		{"optional present", "/a/1", "caOptional", []string{"1"}},
		{"multi segments", "/a/1/2", "caRest", []string{"1/2"}},
	})
}

// TestTrieCatchAllConflictPanics 验证通配路由冲突检测：同路径重复注册报 route conflict；
// 同位置通配名不一致报 catch-all name conflict 且消息含双方名
// （Req 同时具备 rest/other 字段，确保 panic 来自 insertRoute 而非字段绑定校验）
func TestTrieCatchAllConflictPanics(t *testing.T) {
	expectPanicContains(t, []string{"route conflict", "GET", "/a/{rest...}"}, func() {
		router := NewRouter()
		router.GET("/a/{rest...}", caRest)
		router.GET("/a/{rest...}", caRest)
	})
	expectPanicContains(t, []string{"catch-all name conflict", "{rest...}", "{other...}"}, func() {
		router := NewRouter()
		router.GET("/a/{rest...}", caRest)
		router.GET("/a/{other...}", caRest)
	})
}

// TestTrieMatchAllocs 锁死匹配分配量：同节点存在通配兄弟时静态命中仍 0 分配；
// 无同节点参数槽时通配命中仅兜底 append 一次分配
func TestTrieMatchAllocs(t *testing.T) {
	router := NewRouter()
	router.GET("/a/s", caStatic)
	router.GET("/a/{rest...}", caRest)
	if n := testing.AllocsPerRun(100, func() { router.match(http.MethodGet, "/a/s") }); n != 0 {
		t.Fatalf("static match with catch-all sibling allocs = %v, want 0", n)
	}
	if n := testing.AllocsPerRun(100, func() { router.match(http.MethodGet, "/a/x/y") }); n != 1 {
		t.Fatalf("catch-all match allocs = %v, want 1", n)
	}
}

// BenchmarkMatchStatic 为无通配兄弟的静态命中基线，供 BenchmarkMatchStaticWithCatchAllSibling 对照
func BenchmarkMatchStatic(b *testing.B) {
	router := NewRouter()
	router.GET("/a/s", caStatic)
	b.ReportAllocs()
	for b.Loop() {
		router.match(http.MethodGet, "/a/s")
	}
}

// BenchmarkMatchStaticWithCatchAllSibling 观测同节点存在通配兄弟时静态命中的耗时与分配（对照 BenchmarkMatchStatic）
func BenchmarkMatchStaticWithCatchAllSibling(b *testing.B) {
	router := NewRouter()
	router.GET("/a/s", caStatic)
	router.GET("/a/{rest...}", caRest)
	b.ReportAllocs()
	for b.Loop() {
		router.match(http.MethodGet, "/a/s")
	}
}

// BenchmarkMatchCatchAll 观测无同节点参数槽时通配多段命中的耗时与分配（预期 1 次分配）
func BenchmarkMatchCatchAll(b *testing.B) {
	router := NewRouter()
	router.GET("/dify/{rest...}", caRest)
	b.ReportAllocs()
	for b.Loop() {
		router.match(http.MethodGet, "/dify/v1/chat-messages")
	}
}

// BenchmarkMatchCatchAllAfterParamMiss 观测同节点参数与通配并存、参数分支失败后落通配的分配
// （预期 2 次：参数试探 append 被浪费一次，兜底 append 一次）
func BenchmarkMatchCatchAllAfterParamMiss(b *testing.B) {
	router := NewRouter()
	router.GET("/a/{x}", caParam)
	router.GET("/a/{rest...}", caRest)
	b.ReportAllocs()
	for b.Loop() {
		router.match(http.MethodGet, "/a/1/2")
	}
}
