// 跨测试文件共享的请求执行辅助：以指定 Router 构造引擎并执行一次请求。
package zchttp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// serveRequest 以指定 Router 构造引擎并执行一次请求，返回响应记录器。
func serveRequest(t *testing.T, router *Router, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	engine := NewEngine()
	engine.Router = router
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, target, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	return rec
}
