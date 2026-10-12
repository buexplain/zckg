// 跨测试文件共享的请求执行辅助：以指定 Router 构造引擎并执行一次请求。
package zchttp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveJSONOn 在已构造好的 Handler 上执行一次请求并返回响应记录器：
// body 非空时以 JSON 请求发送（Content-Type: application/json），body 为空时不带请求体。
// 不介入引擎构造，供需要对同一 engine 连续发起多次请求的场景使用；不解码响应。
func serveJSONOn(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// serveRequest 以指定 Router 构造引擎并执行一次请求，返回响应记录器。
// body 非空时按 JSON 请求发送；不解码响应，状态码与响应体断言由调用方完成。
func serveRequest(t *testing.T, router *Router, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	engine := NewEngine()
	engine.Router = router
	return serveJSONOn(t, engine, method, target, body)
}

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
