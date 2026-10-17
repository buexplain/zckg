package zchttp

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

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

// TestTrieParamRejectsEmptySegment 锁死参数段不匹配空段：/a/{x}/b 对 /a//b 不命中（而非以 x="" 命中），
// "必选参数 + 通配尾段"同样不以空段命中（可选参数只能位于末尾，末尾空段已被归一化去除，无需单列）；同节点存在通配尾段时空段改由通配捕获（含前导 "/"）；
// 非空段照常命中参数分支。
func TestTrieParamRejectsEmptySegment(t *testing.T) {
	required := NewRouter()
	required.GET("/a/{x}/b", caParam)
	assertTrieMatches(t, required, []trieMatchCase{
		{"empty segment misses", "/a//b", "", nil},
		{"non-empty segment hits", "/a/1/b", "caParam", []string{"1"}},
	})

	paramThenRest := NewRouter()
	paramThenRest.GET("/o/{x}/{rest...}", caRest)
	assertTrieMatches(t, paramThenRest, []trieMatchCase{
		{"param before catch-all empty segment misses", "/o//y/z", "", nil},
		{"param before catch-all hits", "/o/1/y/z", "caRest", []string{"1", "y/z"}},
	})

	withCatchAll := NewRouter()
	withCatchAll.GET("/c/{x}/b", caParam)
	withCatchAll.GET("/c/{rest...}", caRest)
	assertTrieMatches(t, withCatchAll, []trieMatchCase{
		{"empty segment falls to catch-all", "/c//b", "caRest", []string{"/b"}},
		{"non-empty segment prefers param", "/c/1/b", "caParam", []string{"1"}},
	})
}

// ======== 线性参考匹配器差分测试 ========

// refSegKind 是参考实现独立定义的段类型，与被测基数树无共享代码。
type refSegKind int

const (
	refStaticSeg refSegKind = iota
	refParamSeg
	refOptionalSeg
	refCatchAllSeg
)

// refSegment 是参考路由的一段：静态段用 text 表示字面量，参数段用 text 表示参数名。
type refSegment struct {
	kind refSegKind
	text string
}

// refRoute 用段列表描述一条路由，不经过被测实现的 parseRoutePath。
type refRoute struct {
	segments []refSegment
}

func refStatic(text string) refSegment   { return refSegment{kind: refStaticSeg, text: text} }
func refParam(name string) refSegment    { return refSegment{kind: refParamSeg, text: name} }
func refOptional(name string) refSegment { return refSegment{kind: refOptionalSeg, text: name} }
func refCatchAll(name string) refSegment { return refSegment{kind: refCatchAllSeg, text: name} }

// path 将参考路由渲染为可交给被测 Router 注册的路径模板。
func (r refRoute) path() string {
	if len(r.segments) == 0 {
		return "/"
	}
	parts := make([]string, len(r.segments))
	for i, seg := range r.segments {
		switch seg.kind {
		case refStaticSeg:
			parts[i] = seg.text
		case refParamSeg:
			parts[i] = "{" + seg.text + "}"
		case refOptionalSeg:
			parts[i] = "{" + seg.text + "?}"
		case refCatchAllSeg:
			parts[i] = "{" + seg.text + "...}"
		}
	}
	return "/" + strings.Join(parts, "/")
}

// refMatchReq 覆盖随机路由可能用到的全部参数名，保证生成的路由都能通过注册期字段校验。
type refMatchReq struct {
	ID   string `json:"id"`
	X    string `json:"x"`
	Y    string `json:"y"`
	Z    string `json:"z"`
	Name string `json:"name"`
	Rest string `json:"rest"`
}
type refMatchRes struct{}

func refMatchHandler(_ context.Context, _ refMatchReq) (refMatchRes, error) {
	return refMatchRes{}, nil
}

// refRoutesConflict 独立判定两条参考路由在注册期是否会冲突：
// 静态段与参数/通配槽可共存；同位置通配名不一致或重复终点冲突；
// 同位置参数名或可选性不一致冲突；一方是另一方前缀时可共存。
func refRoutesConflict(a, b refRoute) bool {
	for i := 0; i < len(a.segments) && i < len(b.segments); i++ {
		x, y := a.segments[i], b.segments[i]
		switch {
		case x.kind == refStaticSeg && y.kind == refStaticSeg:
			if x.text != y.text {
				return false
			}
		case x.kind == refStaticSeg || y.kind == refStaticSeg:
			return false
		case x.kind == refCatchAllSeg && y.kind == refCatchAllSeg:
			return true
		case x.kind == refCatchAllSeg || y.kind == refCatchAllSeg:
			return false
		default:
			if x.text != y.text || (x.kind == refOptionalSeg) != (y.kind == refOptionalSeg) {
				return true
			}
		}
	}
	return len(a.segments) == len(b.segments)
}

// 参考匹配器为每个决策位置打分，按字典序取最高分，等价于基数树的
// 静态 > 参数 > 通配、路径耗尽时终点 > 可选省略 > 通配零段。
const (
	refRankStatic   = 3
	refRankParam    = 2
	refRankOmit     = 2
	refRankCatchAll = 1
	refRankTerminal = 3
)

// match 在单条参考路由上确定性匹配已切分的请求路径，
// 返回捕获值与决策分数序列；不匹配返回 ok=false。
func (r refRoute) match(pathSegs []string) (values []string, ranks []int, ok bool) {
	ranks = make([]int, 0, len(r.segments)+1)
	i := 0
	for _, seg := range r.segments {
		switch seg.kind {
		case refStaticSeg:
			if i >= len(pathSegs) || pathSegs[i] != seg.text {
				return nil, nil, false
			}
			ranks = append(ranks, refRankStatic)
			i++
		case refParamSeg, refOptionalSeg:
			if i == len(pathSegs) {
				// 可选参数只能位于末尾（路由语法保证），路径恰在此处耗尽时走省略分支
				if seg.kind != refOptionalSeg {
					return nil, nil, false
				}
				ranks = append(ranks, refRankOmit)
				continue
			}
			if pathSegs[i] == "" {
				return nil, nil, false
			}
			values = append(values, pathSegs[i])
			ranks = append(ranks, refRankParam)
			i++
		case refCatchAllSeg:
			if i < len(pathSegs) {
				values = append(values, strings.Join(pathSegs[i:], "/"))
			}
			ranks = append(ranks, refRankCatchAll)
			i = len(pathSegs)
		}
	}
	if i != len(pathSegs) {
		return nil, nil, false
	}
	ranks = append(ranks, refRankTerminal)
	return values, ranks, true
}

// refSplitPath 按 match 的输入契约切分请求路径：根路径无段，其余去掉一个前导 "/"。
func refSplitPath(path string) []string {
	if path == "" || path == "/" {
		return nil
	}
	return strings.Split(strings.TrimPrefix(path, "/"), "/")
}

// compareRefRanks 按字典序比较决策分数序列。
func compareRefRanks(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] == b[i] {
			continue
		}
		if a[i] < b[i] {
			return -1
		}
		return 1
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	default:
		return 0
	}
}

// refMatch 线性扫描全部参考路由，返回优先级最高的命中路由下标与捕获值；
// route 为 -1 表示未命中，ambiguous 表示两条候选的优先级完全相同时出现歧义。
func refMatch(routes []refRoute, path string) (route int, values []string, ambiguous bool) {
	pathSegs := refSplitPath(path)
	route = -1
	var bestRanks []int
	for idx := range routes {
		candidateValues, candidateRanks, ok := routes[idx].match(pathSegs)
		if !ok {
			continue
		}
		if route == -1 {
			route, values, bestRanks = idx, candidateValues, candidateRanks
			ambiguous = false
			continue
		}
		switch compareRefRanks(candidateRanks, bestRanks) {
		case 1:
			route, values, bestRanks = idx, candidateValues, candidateRanks
			ambiguous = false
		case 0:
			ambiguous = true
		}
	}
	return route, values, ambiguous
}

var (
	refStaticWords = []string{"a", "b", "c", "u", "v1", "users", "x"}
	refParamNames  = []string{"id", "x", "y", "z", "name"}
)

// refPickParamName 从未占用的参数名中随机挑选，名字耗尽时返回空串。
func refPickParamName(rng *rand.Rand, used map[string]bool) string {
	start := rng.Intn(len(refParamNames))
	for i := 0; i < len(refParamNames); i++ {
		name := refParamNames[(start+i)%len(refParamNames)]
		if !used[name] {
			return name
		}
	}
	return ""
}

// generateRefRoutes 生成指定数量的互不冲突随机路由，供差分测试使用。
func generateRefRoutes(rng *rand.Rand, want int) []refRoute {
	routes := make([]refRoute, 0, want)
	for attempts := 0; len(routes) < want && attempts < want*100; attempts++ {
		depth := rng.Intn(5)
		segments := make([]refSegment, 0, depth+1)
		used := make(map[string]bool, depth+1)
		for i := 0; i < depth; i++ {
			if rng.Intn(10) < 3 {
				name := refPickParamName(rng, used)
				if name == "" {
					segments = append(segments, refStatic(refStaticWords[rng.Intn(len(refStaticWords))]))
					continue
				}
				used[name] = true
				segments = append(segments, refParam(name))
				continue
			}
			segments = append(segments, refStatic(refStaticWords[rng.Intn(len(refStaticWords))]))
		}
		switch rng.Intn(6) {
		case 0:
			if name := refPickParamName(rng, used); name != "" {
				segments = append(segments, refOptional(name))
			}
		case 1:
			segments = append(segments, refCatchAll("rest"))
		}
		candidate := refRoute{segments: segments}
		conflict := false
		for _, existing := range routes {
			if refRoutesConflict(candidate, existing) {
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		routes = append(routes, candidate)
	}
	return routes
}

// refCuratedRouteTables 覆盖文档中锁死的优先级组合，作为随机生成之外的定向差分输入。
var refCuratedRouteTables = []struct {
	name   string
	routes []refRoute
}{
	{
		name: "static_param_catchall",
		routes: []refRoute{
			{[]refSegment{refStatic("a"), refStatic("s")}},
			{[]refSegment{refStatic("a"), refParam("x")}},
			{[]refSegment{refStatic("a"), refCatchAll("rest")}},
			{[]refSegment{refStatic("b"), refParam("x"), refStatic("z")}},
			{[]refSegment{refStatic("b"), refCatchAll("rest")}},
			{[]refSegment{refStatic("c")}},
			{[]refSegment{refStatic("c"), refCatchAll("rest")}},
		},
	},
	{
		name: "cross_level_static_priority",
		routes: []refRoute{
			{[]refSegment{refStatic("a"), refParam("x")}},
			{[]refSegment{refStatic("a"), refStatic("b"), refCatchAll("rest")}},
		},
	},
	{
		name: "optional_and_catchall",
		routes: []refRoute{
			{[]refSegment{refStatic("a"), refOptional("x")}},
			{[]refSegment{refStatic("a"), refCatchAll("rest")}},
		},
	},
	{
		name: "backtrack_to_param",
		routes: []refRoute{
			{[]refSegment{refStatic("a"), refParam("x"), refStatic("b")}},
			{[]refSegment{refStatic("a"), refStatic("static"), refStatic("c")}},
		},
	},
	{
		name: "root_variants",
		routes: []refRoute{
			{},
			{[]refSegment{refOptional("x")}},
			{[]refSegment{refCatchAll("rest")}},
		},
	},
	{
		name: "exact_beats_optional",
		routes: []refRoute{
			{[]refSegment{refStatic("posts"), refParam("id"), refStatic("comments"), refOptional("name")}},
			{[]refSegment{refStatic("posts"), refParam("id"), refStatic("comments")}},
		},
	},
	{
		name: "static_deep_and_catchall",
		routes: []refRoute{
			{[]refSegment{refStatic("api"), refParam("id"), refStatic("detail")}},
			{[]refSegment{refStatic("api"), refStatic("users"), refCatchAll("rest")}},
			{[]refSegment{refStatic("api"), refStatic("users"), refStatic("me")}},
		},
	},
	{
		name: "catchall_after_param",
		routes: []refRoute{
			{[]refSegment{refStatic("o"), refParam("x"), refCatchAll("rest")}},
			{[]refSegment{refStatic("o"), refParam("x"), refStatic("fixed")}},
			{[]refSegment{refStatic("o"), refParam("x")}},
		},
	},
}

// refAddPath 将具体段列表还原为请求路径并交给 add 去重收集。
func refAddPath(add func(string), parts []string) {
	if len(parts) == 0 {
		add("/")
		return
	}
	add("/" + strings.Join(parts, "/"))
}

// refAlphabet 收集参考路由中的静态字面量并追加一个诱饵，供穷举路径使用。
func refAlphabet(routes []refRoute) []string {
	seen := make(map[string]bool)
	alphabet := make([]string, 0, 8)
	for _, route := range routes {
		for _, seg := range route.segments {
			if seg.kind == refStaticSeg && !seen[seg.text] {
				seen[seg.text] = true
				alphabet = append(alphabet, seg.text)
			}
		}
	}
	return append(alphabet, "q")
}

// refExhaustivePaths 生成深度不超过 maxDepth 的全部路径，并为每个路径追加
// 内部空段变体（末段保持非空，以遵守 normalizePath 后的输入契约）。
func refExhaustivePaths(alphabet []string, maxDepth int) []string {
	paths := []string{"/"}
	var walk func(parts []string)
	walk = func(parts []string) {
		if len(parts) == maxDepth {
			return
		}
		for _, word := range alphabet {
			next := append(append([]string(nil), parts...), word)
			refAddPath(func(p string) { paths = append(paths, p) }, next)
			for i := 0; i < len(next)-1; i++ {
				variant := append([]string(nil), next...)
				variant[i] = ""
				refAddPath(func(p string) { paths = append(paths, p) }, variant)
			}
			walk(next)
		}
	}
	walk(nil)
	return paths
}

// refRequestPaths 由路由模板实例化出命中路径与逐段前缀（覆盖可选省略、通配零段），
// 再补充随机路径（含内部空段），使每条路由与不命中分支都有机会被比较。
func refRequestPaths(rng *rand.Rand, routes []refRoute, randomCount int) []string {
	paths := make([]string, 0, len(routes)*4+randomCount+1)
	seen := make(map[string]bool)
	add := func(path string) {
		if !seen[path] {
			seen[path] = true
			paths = append(paths, path)
		}
	}
	for _, route := range routes {
		concrete := make([]string, 0, len(route.segments)+2)
		refAddPath(add, concrete)
		for i, seg := range route.segments {
			switch seg.kind {
			case refStaticSeg:
				concrete = append(concrete, seg.text)
			case refParamSeg, refOptionalSeg:
				concrete = append(concrete, fmt.Sprintf("v%d", i))
			case refCatchAllSeg:
				concrete = append(concrete, fmt.Sprintf("c%d", i))
				refAddPath(add, concrete)
				concrete = append(concrete, "d")
			}
			refAddPath(add, concrete)
		}
	}
	words := []string{"a", "b", "c", "u", "v1", "users", "x", "y", "z", "id", "other", "d"}
	for i := 0; i < randomCount; i++ {
		depth := rng.Intn(6)
		parts := make([]string, depth)
		for j := range parts {
			if rng.Intn(8) == 0 {
				parts[j] = ""
			} else {
				parts[j] = words[rng.Intn(len(words))]
			}
		}
		if depth > 0 && parts[depth-1] == "" {
			parts[depth-1] = "z"
		}
		refAddPath(add, parts)
	}
	return paths
}

// registerRefRoutes 注册参考路由，注册期 panic 视为生成器冲突模型与被测实现不一致。
func registerRefRoutes(t *testing.T, routes []refRoute) *Router {
	t.Helper()
	router := NewRouter()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("generated route table failed to register: %v; routes %v", r, refRouteTemplates(routes))
		}
	}()
	for _, route := range routes {
		router.GET(route.path(), refMatchHandler)
	}
	return router
}

func refRouteTemplates(routes []refRoute) []string {
	templates := make([]string, len(routes))
	for i, route := range routes {
		templates[i] = route.path()
	}
	return templates
}

// refSameValues 比较捕获值，nil 与空切片视为等价（均表示未捕获参数）。
func refSameValues(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// assertTrieMatchesReference 对每条路径比较基数树匹配与线性参考匹配的路由及捕获值。
func assertTrieMatchesReference(t *testing.T, router *Router, routes []refRoute, paths []string) {
	t.Helper()
	registered := make(map[*routeEntry]string, len(router.routes))
	for _, record := range router.routes {
		if _, exists := registered[record.entry]; exists {
			t.Fatalf("route entry reused by %q", record.path)
		}
		registered[record.entry] = record.path
	}
	templates := refRouteTemplates(routes)
	for _, path := range paths {
		entry, gotValues := router.match(http.MethodGet, path)
		wantRoute, wantValues, ambiguous := refMatch(routes, path)
		if ambiguous {
			t.Fatalf("path %q: reference matcher ambiguous; routes %v", path, templates)
		}
		if wantRoute < 0 {
			if entry != nil {
				t.Fatalf("path %q: trie hit %q, reference miss; routes %v", path, registered[entry], templates)
			}
			if len(gotValues) != 0 {
				t.Fatalf("path %q: trie miss returned values %q; routes %v", path, gotValues, templates)
			}
			continue
		}
		if entry == nil {
			t.Fatalf("path %q: trie miss, reference hit %q values %q; routes %v", path, templates[wantRoute], wantValues, templates)
		}
		if gotPath := registered[entry]; gotPath != templates[wantRoute] {
			t.Fatalf("path %q: trie hit %q, reference hit %q; routes %v", path, gotPath, templates[wantRoute], templates)
		}
		if !refSameValues(gotValues, wantValues) {
			t.Fatalf("path %q: trie captured %q, reference captured %q (route %q); routes %v",
				path, gotValues, wantValues, templates[wantRoute], templates)
		}
	}
}

// TestTrieLinearReferenceDifferential 用独立实现的线性参考匹配器对基数树做差分校验：
// 定向表覆盖文档锁死的静态/参数/通配优先级、可选省略与零段命中；
// 随机表覆盖组合爆炸的路由形状与请求路径（含内部空段），逐条比较命中路由与捕获值。
// 参考实现按决策位置打分取字典序最高分，与被测基数树不共享匹配或解析代码。
func TestTrieLinearReferenceDifferential(t *testing.T) {
	t.Run("curated", func(t *testing.T) {
		for _, table := range refCuratedRouteTables {
			t.Run(table.name, func(t *testing.T) {
				router := registerRefRoutes(t, table.routes)
				paths := refExhaustivePaths(refAlphabet(table.routes), 4)
				assertTrieMatchesReference(t, router, table.routes, paths)
			})
		}
	})

	t.Run("random", func(t *testing.T) {
		const (
			trials   = 120
			baseSeed = int64(20261017)
		)
		for trial := 0; trial < trials; trial++ {
			seed := baseSeed + int64(trial)
			rng := rand.New(rand.NewSource(seed))
			routes := generateRefRoutes(rng, 1+rng.Intn(7))
			if len(routes) == 0 {
				t.Fatalf("trial %d (seed %d): generator produced no routes", trial, seed)
			}
			router := registerRefRoutes(t, routes)
			paths := refRequestPaths(rng, routes, 80)
			t.Run(fmt.Sprintf("seed_%d", seed), func(t *testing.T) {
				assertTrieMatchesReference(t, router, routes, paths)
			})
		}
	})
}

// FuzzRouterMatch 用线性参考匹配器差分校验基数树：fuzzer 变异路由表索引与请求路径，
// 断言命中路由与捕获值一致，并拒绝参考实现的同优先级歧义。
// 路由表固定为 8 组定向用例（静态/参数/通配优先级、可选省略、回溯、根路径），
// 路径先经 normalizePath 归一化；路径上限 64 KB，避免单次执行放大到不可控耗时。
// 普通 go test 只运行 f.Add 种子；探索命令：
//
//	go test ./zchttp -run '^$' -fuzz '^FuzzRouterMatch$' -fuzztime=30s
func FuzzRouterMatch(f *testing.F) {
	tables := refCuratedRouteTables
	routers := make([]*Router, len(tables))
	registered := make([]map[*routeEntry]string, len(tables))
	templates := make([][]string, len(tables))
	for i, table := range tables {
		router := NewRouter()
		for _, route := range table.routes {
			router.GET(route.path(), refMatchHandler)
		}
		routers[i] = router
		registered[i] = make(map[*routeEntry]string, len(router.routes))
		for _, record := range router.routes {
			registered[i][record.entry] = record.path
		}
		templates[i] = refRouteTemplates(table.routes)
	}

	pathSeeds := []string{
		"", "/", "/a", "/a/s", "/a/1", "/a/1/2", "/a//b", "/a/s/t",
		"/a/b", "/a/b/c", "/a/b/c/d", "/posts/1/comments", "/posts/1/comments/2",
		"/optional/7", "/elsewhere", "//b", "/static/c",
	}
	for i := range tables {
		for _, seed := range pathSeeds {
			f.Add(uint8(i), seed)
		}
	}

	f.Fuzz(func(t *testing.T, tableIndex uint8, path string) {
		if len(path) > 64<<10 {
			return
		}
		index := int(tableIndex) % len(tables)
		normalized := normalizePath(path)
		entry, gotValues := routers[index].match(http.MethodGet, normalized)
		wantRoute, wantValues, ambiguous := refMatch(tables[index].routes, normalized)
		if ambiguous {
			t.Fatalf("table=%d path=%q: reference matcher ambiguous", index, normalized)
		}
		if wantRoute < 0 {
			if entry != nil || len(gotValues) != 0 {
				t.Fatalf("table=%d path=%q: trie hit %q values %v, reference miss",
					index, normalized, registered[index][entry], gotValues)
			}
			return
		}
		if entry == nil {
			t.Fatalf("table=%d path=%q: trie miss, reference hit %q values %v",
				index, normalized, templates[index][wantRoute], wantValues)
		}
		if gotPath := registered[index][entry]; gotPath != templates[index][wantRoute] || !refSameValues(gotValues, wantValues) {
			t.Fatalf("table=%d path=%q: trie %q values %v, reference %q values %v",
				index, normalized, gotPath, gotValues, templates[index][wantRoute], wantValues)
		}
	})
}
