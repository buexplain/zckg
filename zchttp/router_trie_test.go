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
