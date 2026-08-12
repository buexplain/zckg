package zchttp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDefaultResponseJSON 验证默认响应回调输出 JSON
func TestDefaultResponseJSON(t *testing.T) {
	router := NewRouter()
	router.GET("/hello", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{Message: "hi " + req.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/hello?name=bob", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected json content-type, got %q", ct)
	}
	var out struct {
		Data    engineRes `json:"data"`
		Code    int       `json:"code"`
		Message string    `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Code != 0 || out.Message != "success" {
		t.Fatalf("expected code=0 message=success, got code=%d message=%q", out.Code, out.Message)
	}
	if out.Data.Message != "hi bob" {
		t.Fatalf("unexpected message: %s", out.Data.Message)
	}
}

// TestDefaultResponseSkipWhenWritten 验证 handler 已写入响应（如文件）时默认回调跳过 JSON
func TestDefaultResponseSkipWhenWritten(t *testing.T) {
	// handler 内通过 middleware 无关的方式直接写响应：这里用一个中间件在 next 前写文件内容
	router := NewRouter()
	router.Use(func(_ context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		// 模拟文件/自定义响应：设置头并写入 body
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", `attachment; filename="a.txt"`)
		_, _ = w.Write([]byte("file-content"))
		return next()
	})
	router.GET("/download", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{Message: "should-be-ignored"}, nil
	})

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/download", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("content-type should stay text/plain, got %q", ct)
	}
	if body := rec.Body.String(); body != "file-content" {
		t.Fatalf("body should be file-content (json skipped), got %q", body)
	}
}

// TestDefaultErrorHandler 验证默认错误回调返回 500 与统一 JSON 结构
func TestDefaultErrorHandler(t *testing.T) {
	router := NewRouter()
	router.GET("/err", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{}, fmt.Errorf("boom")
	})

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected json content-type, got %q", ct)
	}
	var out struct {
		Data    any    `json:"data"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Code != http.StatusInternalServerError || out.Message != "boom" {
		t.Fatalf("unexpected json error: code=%d message=%q", out.Code, out.Message)
	}
}

// TestDefaultErrorHandlerHtml 验证 Accept 包含 text/html 时错误回调返回 HTML
func TestDefaultErrorHandlerHtml(t *testing.T) {
	router := NewRouter()
	router.GET("/err", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{}, fmt.Errorf("boom")
	})

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/err", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected html content-type, got %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "boom") || !strings.Contains(body, "<h1>") {
		t.Fatalf("unexpected html body: %q", body)
	}
}

// TestDefaultNotFoundJSON 验证未命中路由默认返回 404 与统一 JSON 结构
func TestDefaultNotFoundJSON(t *testing.T) {
	engine := NewEngine()

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected json content-type, got %q", ct)
	}
	var out struct {
		Data    any    `json:"data"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.Code != http.StatusNotFound || out.Message != "not found" {
		t.Fatalf("unexpected json: code=%d message=%q", out.Code, out.Message)
	}
}

// TestDefaultNotFoundHtml 验证 Accept 包含 text/html 时未命中路由返回 HTML
func TestDefaultNotFoundHtml(t *testing.T) {
	engine := NewEngine()

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	req.Header.Set("Accept", "text/html")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("expected html content-type, got %q", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "404") || !strings.Contains(body, "/missing") {
		t.Fatalf("unexpected html body: %q", body)
	}
}

// TestWantHtml 验证 Accept 头判定
func TestWantHtml(t *testing.T) {
	cases := []struct {
		accept string
		want   bool
	}{
		{"text/html,application/xhtml+xml", true},
		{"text/plain", true},
		{"application/json", false},
		{"", false},
	}
	for _, c := range cases {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		if c.accept != "" {
			r.Header.Set("Accept", c.accept)
		}
		if got := WantHtml(r); got != c.want {
			t.Fatalf("WantHtml(%q)=%v, want %v", c.accept, got, c.want)
		}
	}
}

// TestCustomResponseAndErrorHandler 验证自定义响应/错误回调生效
func TestCustomResponseAndErrorHandler(t *testing.T) {
	router := NewRouter()
	router.GET("/ok", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{Message: "x"}, nil
	})
	router.GET("/bad", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{}, fmt.Errorf("bad")
	})

	engine := NewEngine()
	engine.Router = router
	engine.OnResponse = func(w http.ResponseWriter, r *http.Request, res any) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("custom-ok"))
	}
	engine.OnError = func(w http.ResponseWriter, r *http.Request, err error) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("custom-err:" + err.Error()))
	}

	// 成功路径
	recOK := httptest.NewRecorder()
	engine.ServeHTTP(recOK, httptest.NewRequest(http.MethodGet, "/ok", nil))
	if recOK.Code != http.StatusCreated || recOK.Body.String() != "custom-ok" {
		t.Fatalf("custom response failed: code=%d body=%q", recOK.Code, recOK.Body.String())
	}

	// 错误路径
	recBad := httptest.NewRecorder()
	engine.ServeHTTP(recBad, httptest.NewRequest(http.MethodGet, "/bad", nil))
	if recBad.Code != http.StatusBadRequest || recBad.Body.String() != "custom-err:bad" {
		t.Fatalf("custom error failed: code=%d body=%q", recBad.Code, recBad.Body.String())
	}
}

// ======== 覆盖率补充测试 ========

// TestDefaultPanicHandler_JSON 验证 handler panic 时返回 500 JSON 响应
func TestDefaultPanicHandler_JSON(t *testing.T) {
	router := NewRouter()
	router.GET("/boom", func(_ context.Context, _ engineReq) (engineRes, error) {
		panic("something went wrong")
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	// 响应体不应包含 panic 信息（安全：不向客户端泄露内部堆栈），仅包含通用错误消息
	body := rec.Body.String()
	if !strings.Contains(body, "internal server error") {
		t.Fatalf("response body should contain 'internal server error', got: %s", body)
	}
	if strings.Contains(body, "something went wrong") {
		t.Fatalf("response body should NOT contain panic message for security, got: %s", body)
	}
}

// TestDefaultPanicHandler_HTML 验证 panic 时请求 Accept: text/html 返回 HTML
func TestDefaultPanicHandler_HTML(t *testing.T) {
	router := NewRouter()
	router.GET("/boom", func(_ context.Context, _ engineReq) (engineRes, error) {
		panic("html panic")
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	req.Header.Set("Accept", "text/html")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/html; charset=utf-8", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "500 Internal Server Error") {
		t.Fatalf("response should contain HTML 500 heading, got: %s", body)
	}
	// 安全：响应不应包含 panic 消息
	if strings.Contains(body, "html panic") {
		t.Fatalf("response should NOT contain panic message for security, got: %s", body)
	}
	if !strings.Contains(body, "internal server error") {
		t.Fatalf("response should contain 'internal server error', got: %s", body)
	}
}

// TestDefaultPanicHandler_SkipWhenWritten 验证 panic 时响应已写入则不再写入
func TestDefaultPanicHandler_SkipWhenWritten(t *testing.T) {
	router := NewRouter()
	router.GET("/boom", func(ctx context.Context, _ engineReq) (engineRes, error) {
		w, _ := ResponseWriterFromContext(ctx)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("partial"))
		panic("after write")
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	engine.ServeHTTP(rec, req)

	// 不应覆盖已写入的内容
	if body := rec.Body.String(); body != "partial" {
		t.Fatalf("body = %q, want 'partial' (panic handler should skip)", body)
	}
}

// TestDefaultValidationErrorHandler_HTML 验证校验失败 + Accept: text/html 时返回 HTML 400
func TestDefaultValidationErrorHandler_HTML(t *testing.T) {
	type valReq struct {
		Email string `json:"email" nonzero:"true"`
	}
	type valRes struct {
		OK bool `json:"ok"`
	}

	router := NewRouter()
	router.POST("/val", func(_ context.Context, _ valReq) (valRes, error) {
		return valRes{OK: true}, nil
	})

	engine := NewEngine()
	engine.Router = router

	// 发送空 JSON body，email 为空 → nonzero 校验失败
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/val", strings.NewReader(`{}`))
	req.Header.Set("Accept", "text/html")
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Fatalf("content-type = %q, want text/html; charset=utf-8", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "400 Bad Request") {
		t.Fatalf("response should contain HTML 400 heading, got: %s", body)
	}
}

// TestDefaultValidationErrorHandler_SkipWhenWritten 验证校验失败但响应已写入时跳过
func TestDefaultValidationErrorHandler_SkipWhenWritten(t *testing.T) {
	type valReq struct {
		Email string `json:"email" nonzero:"true"`
	}
	type valRes struct {
		OK bool `json:"ok"`
	}

	router := NewRouter()
	// 中间件提前写入响应
	router.Use(func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("early"))
		return next()
	})
	router.POST("/val", func(_ context.Context, _ valReq) (valRes, error) {
		return valRes{OK: true}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/val", strings.NewReader(`{}`))
	engine.ServeHTTP(rec, req)

	// 响应应保持中间件写入的内容
	if body := rec.Body.String(); body != "early" {
		t.Fatalf("body = %q, want 'early'", body)
	}
}
