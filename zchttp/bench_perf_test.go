package zchttp

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// benchExpectOK 基准用例的正确性守卫：用独立 Recorder 断言一次请求返回 200，
// 防止后续优化破坏行为时基准仍照常跑（基准本身不做断言）
func benchExpectOK(tb testing.TB, e *HttpEngine, req *http.Request) {
	tb.Helper()
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		tb.Fatalf("benchmark scenario broken: %s %s expected 200, got %d, body: %s",
			req.Method, req.URL.Path, w.Code, w.Body.String())
	}
}

// ---------- 基准测试用 handler 与 Req/Res ----------

type benchSimpleReq struct {
	Name string `json:"name" form:"name"`
	Age  int    `json:"age" form:"age"`
}

type benchSimpleRes struct {
	OK bool `json:"ok"`
}

func benchSimpleHandler(_ context.Context, _ *benchSimpleReq) (benchSimpleRes, error) {
	return benchSimpleRes{OK: true}, nil
}

type benchNestedItem struct {
	Title  string `json:"title" nonzero:"true"`
	Score  int    `json:"score"`
	Detail *struct {
		Desc string `json:"desc" nonzero:"true"`
	} `json:"detail"`
}

type benchNestedReq struct {
	ID    string            `json:"id" nonzero:"true"`
	Items []benchNestedItem `json:"items"`
}

type benchNestedRes struct {
	Count int `json:"count"`
}

func benchNestedHandler(_ context.Context, req *benchNestedReq) (benchNestedRes, error) {
	return benchNestedRes{Count: len(req.Items)}, nil
}

type benchPathParamReq struct {
	UserID string `json:"userId"`
}

type benchPathParamRes struct {
	UserID string `json:"userId"`
}

func benchPathParamHandler(_ context.Context, req *benchPathParamReq) (benchPathParamRes, error) {
	return benchPathParamRes{UserID: req.UserID}, nil
}

// ---------- 基准用例 ----------

// 静态路由 + GET query 绑定：最小热路径
func BenchmarkServeHTTP_StaticRoute_GET(b *testing.B) {
	e := NewEngine()
	e.Router.GET("/api/user", benchSimpleHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/user?name=alice&age=18", nil)
	benchExpectOK(b, e, req)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.ServeHTTP(w, req)
	}
}

// 静态路由 + POST JSON 绑定：典型业务路径
func BenchmarkServeHTTP_StaticRoute_POST_JSON(b *testing.B) {
	e := NewEngine()
	e.Router.POST("/api/user", benchSimpleHandler)
	body := `{"name":"alice","age":18}`
	check := httptest.NewRequest(http.MethodPost, "/api/user", strings.NewReader(body))
	check.Header.Set("Content-Type", "application/json")
	benchExpectOK(b, e, check)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/user", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(w, req)
	}
}

// 嵌套结构体 + nonzero 校验 + 请求阶段默认值：暴露反射遍历/元信息重建开销
func BenchmarkServeHTTP_Nested_Nonzero(b *testing.B) {
	e := NewEngine()
	e.Router.POST("/api/nested", benchNestedHandler)
	body := `{"id":"x1","items":[{"title":"a","score":1,"detail":{"desc":"d1"}},{"title":"b","score":2,"detail":{"desc":"d2"}}]}`
	check := httptest.NewRequest(http.MethodPost, "/api/nested", strings.NewReader(body))
	check.Header.Set("Content-Type", "application/json")
	benchExpectOK(b, e, check)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/nested", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		e.ServeHTTP(w, req)
	}
}

// 参数路由基数树匹配路径：与 StaticRoute_GET 使用相同的 handler 形状（benchSimpleHandler，
// 仅路径注册方式不同），使基准真正隔离路由匹配开销（含路径参数绑定）
func BenchmarkServeHTTP_ParamRoute(b *testing.B) {
	e := NewEngine()
	e.Router.GET("/api/{name}", benchSimpleHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/alice?age=18", nil)
	benchExpectOK(b, e, req)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.ServeHTTP(w, req)
	}
}

// 带中间件链（3 层）的路径：runChain 闭包分配
func BenchmarkServeHTTP_WithMiddlewares(b *testing.B) {
	e := NewEngine()
	noop := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next()
	}
	e.Router.Use(noop, noop, noop)
	e.Router.GET("/api/user", benchSimpleHandler)
	req := httptest.NewRequest(http.MethodGet, "/api/user?name=alice&age=18", nil)
	benchExpectOK(b, e, req)
	w := httptest.NewRecorder()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		e.ServeHTTP(w, req)
	}
}

// handler 内 io.Copy 大文件下载（n7-B）：验证 responseWriter.ReadFrom 透传（sendfile 优化）路径的基准，
// 与底层 io.ReaderFrom 组合后 io.Copy 不再逐字节拷贝。
// 每迭代新建 Recorder 防止 4MB 响应体在复用 Recorder 中无限累积。
func BenchmarkServeHTTP_LargeFileDownload(b *testing.B) {
	const fileSize = 4 << 20 // 4MB
	payload := strings.Repeat("f", fileSize)
	e := NewEngine()
	e.Router.GET("/file", func(ctx context.Context, _ benchSimpleReq) (any, error) {
		w, _ := ResponseWriterFromContext(ctx)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, err := io.Copy(w, strings.NewReader(payload))
		return nil, err
	})
	benchExpectOK(b, e, httptest.NewRequest(http.MethodGet, "/file", nil))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/file", nil))
	}
}

// TestServeHTTP_ConcurrentStress 并发压测：验证 requestState 每请求隔离、
// structMeta 全局缓存并发安全（建议配合 go test -race 运行）
func TestServeHTTP_ConcurrentStress(t *testing.T) {
	e := NewEngine()
	e.Router.POST("/api/nested", benchNestedHandler)
	e.Router.GET("/api/user", benchSimpleHandler)
	e.Router.GET("/api/user/{userId}/profile", benchPathParamHandler)
	body := `{"id":"x1","items":[{"title":"a","score":1,"detail":{"desc":"d1"}}]}`
	var wg sync.WaitGroup
	for g := 0; g < 16; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				req := httptest.NewRequest(http.MethodPost, "/api/nested", strings.NewReader(body))
				req.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				e.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Errorf("nested: expected 200, got %d", w.Code)
					return
				}
				w2 := httptest.NewRecorder()
				e.ServeHTTP(w2, httptest.NewRequest(http.MethodGet, "/api/user?name=a&age=1", nil))
				if w2.Code != http.StatusOK {
					t.Errorf("get: expected 200, got %d", w2.Code)
					return
				}
				w3 := httptest.NewRecorder()
				e.ServeHTTP(w3, httptest.NewRequest(http.MethodGet, "/api/user/42/profile", nil))
				if w3.Code != http.StatusOK || !strings.Contains(w3.Body.String(), "42") {
					t.Errorf("param: expected 200 with userId 42, got %d %s", w3.Code, w3.Body.String())
					return
				}
			}
		}()
	}
	wg.Wait()
}
