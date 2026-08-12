package zchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

// ======== 路由参数 e2e ========

// e2ePathParamReq 覆盖必选 int 参数与带默认值的可选参数
type e2ePathParamReq struct {
	PostID    int `json:"post_id"`
	CommentID int `json:"comment_id" default:"99"`
}
type e2ePathParamRes struct {
	PostID    int `json:"post_id"`
	CommentID int `json:"comment_id"`
}

func e2ePathParamHandler(_ context.Context, req e2ePathParamReq) (e2ePathParamRes, error) {
	return e2ePathParamRes{PostID: req.PostID, CommentID: req.CommentID}, nil
}

// TestPathParamE2E_BindAndOptional 验证必选参数绑定、可选参数命中与省略（省略时保留 default）
func TestPathParamE2E_BindAndOptional(t *testing.T) {
	router := NewRouter()
	router.GET("/posts/{post_id}/comments/{comment_id?}", e2ePathParamHandler)

	engine := NewEngine()
	engine.Router = router

	cases := []struct {
		path      string
		wantPost  int
		wantReply int
	}{
		{"/posts/1/comments/5", 1, 5},   // 可选参数提供
		{"/posts/1/comments", 1, 99},    // 可选参数省略，保留 default
		{"/posts/42/comments/", 42, 99}, // 末尾斜杠归一化后命中省略分支
	}
	for _, c := range cases {
		req := httptest.NewRequest(http.MethodGet, c.path, nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s expected 200, got %d", c.path, rec.Code)
		}
		var res e2ePathParamRes
		decodeData(t, rec, &res)
		if res.PostID != c.wantPost || res.CommentID != c.wantReply {
			t.Fatalf("GET %s = %+v, want post=%d comment=%d", c.path, res, c.wantPost, c.wantReply)
		}
	}
}

// TestPathParamE2E_InvalidValue400 验证必选参数类型转换失败返回 400（BindingError 通道）
func TestPathParamE2E_InvalidValue400(t *testing.T) {
	router := NewRouter()
	router.GET("/posts/{post_id}/comments/{comment_id?}", e2ePathParamHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/posts/abc/comments", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid int param, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid path parameter value") {
		t.Fatalf("response should contain binding error detail, got: %s", rec.Body.String())
	}
}

// TestPathParamE2E_NoMatch404 验证参数段数不匹配时返回 404
func TestPathParamE2E_NoMatch404(t *testing.T) {
	router := NewRouter()
	router.GET("/posts/{post_id}/comments/{comment_id?}", e2ePathParamHandler)

	engine := NewEngine()
	engine.Router = router

	for _, path := range []string{"/posts", "/posts/1/extra/deep"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET %s expected 404, got %d", path, rec.Code)
		}
	}
}

// TestPathParamE2E_OverridesQuery 验证路径参数覆盖同名 query 参数
func TestPathParamE2E_OverridesQuery(t *testing.T) {
	router := NewRouter()
	router.GET("/posts/{post_id}/comments/{comment_id?}", e2ePathParamHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/posts/7/comments?post_id=999&comment_id=888", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res e2ePathParamRes
	decodeData(t, rec, &res)
	if res.PostID != 7 || res.CommentID != 888 {
		t.Fatalf("path param should override query: got %+v, want post=7 comment=888", res)
	}
}

// TestPathParamE2E_WithBody 验证 POST 请求体与路径参数同时绑定
type e2eBodyParamReq struct {
	PostID int    `json:"post_id"`
	Body   string `json:"body"`
}
type e2eBodyParamRes struct {
	PostID int    `json:"post_id"`
	Body   string `json:"body"`
}

func TestPathParamE2E_WithBody(t *testing.T) {
	router := NewRouter()
	router.POST("/posts/{post_id}/comments", func(_ context.Context, req e2eBodyParamReq) (e2eBodyParamRes, error) {
		return e2eBodyParamRes{PostID: req.PostID, Body: req.Body}, nil
	})

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodPost, "/posts/3/comments", strings.NewReader(`{"body":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var res e2eBodyParamRes
	decodeData(t, rec, &res)
	if res.PostID != 3 || res.Body != "hi" {
		t.Fatalf("got %+v, want post=3 body=hi", res)
	}
}

// TestPathParamE2E_GroupPrefix 验证分组前缀与参数路由组合
func TestPathParamE2E_GroupPrefix(t *testing.T) {
	router := NewRouter()
	api := router.Group("/api")
	api.GET("/items/{post_id}", e2ePathParamHandler)

	engine := NewEngine()
	engine.Router = router

	req := httptest.NewRequest(http.MethodGet, "/api/items/8", nil)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var res e2ePathParamRes
	decodeData(t, rec, &res)
	if res.PostID != 8 {
		t.Fatalf("post_id = %d, want 8", res.PostID)
	}
}

// TestPathParamE2E_ExactRoutePreferred 验证精确路由优先于可选参数路由的省略分支
func TestPathParamE2E_ExactRoutePreferred(t *testing.T) {
	router := NewRouter()
	router.GET("/user", func(_ context.Context, _ helloReq) (helloRes, error) {
		return helloRes{Message: "exact"}, nil
	})
	router.GET("/user/{name?}", hello)

	engine := NewEngine()
	engine.Router = router

	// /user 命中精确路由
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/user", nil))
	var res helloRes
	decodeData(t, rec, &res)
	if res.Message != "exact" {
		t.Fatalf("message = %q, want 'exact' (exact route preferred)", res.Message)
	}

	// /user/x 命中参数路由
	rec = httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/user/x", nil))
	decodeData(t, rec, &res)
	if res.Message != "Hello, x" {
		t.Fatalf("message = %q, want 'Hello, x'", res.Message)
	}
}

// ======== Default*Handler 编码失败分支与 Run ========

// failingWriter 的 Write 永远失败，用于触发各 Default*Handler 的 json 编码失败分支
type failingWriter struct {
	header http.Header
	code   int
}

func (w *failingWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *failingWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }
func (w *failingWriter) WriteHeader(code int)      { w.code = code }

// TestDefaultResponseHandler_EncodeFail 验证 JSON 编码失败时回退 500 纯文本响应
func TestDefaultResponseHandler_EncodeFail(t *testing.T) {
	w := &failingWriter{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	DefaultResponseHandler(w, req, helloRes{Message: "hi"})
	if w.code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.code)
	}
}

// TestDefaultResponseHandler_SkipWhenWritten 验证响应已写入时跳过
func TestDefaultResponseHandler_SkipWhenWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec}
	rw.Header().Set("Content-Type", "text/plain")
	_, _ = rw.Write([]byte("custom"))

	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	DefaultResponseHandler(rw, req, helloRes{Message: "hi"})

	if rec.Body.String() != "custom" {
		t.Fatalf("body = %q, want 'custom' (response already written)", rec.Body.String())
	}
}

// TestDefaultErrorHandler_EncodeFail 验证错误响应 JSON 编码失败时仅记录日志不 panic
func TestDefaultErrorHandler_EncodeFail(t *testing.T) {
	w := &failingWriter{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	DefaultErrorHandler(w, req, fmt.Errorf("boom"))
	if w.code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.code)
	}
}

// TestDefaultValidationErrorHandler_EncodeFail 验证校验错误响应 JSON 编码失败分支
func TestDefaultValidationErrorHandler_EncodeFail(t *testing.T) {
	w := &failingWriter{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	DefaultValidationErrorHandler(w, req, fmt.Errorf("bad param"))
	if w.code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400", w.code)
	}
}

// TestDefaultPanicHandler_EncodeFail 验证 panic 响应 JSON 编码失败分支
func TestDefaultPanicHandler_EncodeFail(t *testing.T) {
	w := &failingWriter{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	DefaultPanicHandler(w, req, "recovered")
	if w.code != http.StatusInternalServerError {
		t.Fatalf("code = %d, want 500", w.code)
	}
}

// TestDefaultNotFoundHandler_EncodeFail 验证 404 响应 JSON 编码失败分支
func TestDefaultNotFoundHandler_EncodeFail(t *testing.T) {
	w := &failingWriter{}
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	DefaultNotFoundHandler(w, req)
	if w.code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", w.code)
	}
}

// TestHttpEngine_Run_InvalidAddr 验证 Run 将监听错误透传返回（无效端口立即失败）
func TestHttpEngine_Run_InvalidAddr(t *testing.T) {
	engine := NewEngine()
	err := engine.Run(&http.Server{Addr: "127.0.0.1:-1"})
	if err == nil {
		t.Fatal("expected listen error for invalid addr, got nil")
	}
}

// TestDefaultErrorHandler_SkipWhenWritten 覆盖响应已写入时跳过的分支
func TestDefaultErrorHandler_SkipWhenWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec}
	_, _ = rw.Write([]byte("partial"))
	DefaultErrorHandler(rw, httptest.NewRequest(http.MethodGet, "/x", nil), fmt.Errorf("boom"))
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want 'partial' (response already written)", rec.Body.String())
	}
}

// TestDefaultNotFoundHandler_SkipWhenWritten 覆盖响应已写入时跳过的分支
func TestDefaultNotFoundHandler_SkipWhenWritten(t *testing.T) {
	rec := httptest.NewRecorder()
	rw := &responseWriter{ResponseWriter: rec}
	_, _ = rw.Write([]byte("partial"))
	DefaultNotFoundHandler(rw, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Body.String() != "partial" {
		t.Fatalf("body = %q, want 'partial' (response already written)", rec.Body.String())
	}
}

// TestDefaultResponse_NoHTMLEscape 验证默认 JSON 响应不再对 < > & 做 HTML 转义（池化编码器行为锁死）
func TestDefaultResponse_NoHTMLEscape(t *testing.T) {
	type escRes struct {
		Text string `json:"text"`
	}
	router := NewRouter()
	router.GET("/esc", func(_ context.Context, req engineReq) (escRes, error) {
		return escRes{Text: "<b>a&b</b>"}, nil
	})
	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/esc", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<b>a&b</b>") {
		t.Fatalf("expected raw HTML chars in JSON body, got %q", body)
	}
	if strings.Contains(body, `\u003c`) {
		t.Fatalf("HTML escaping should be disabled, got %q", body)
	}
}

// countingReader 统计已读字节数，用于验证请求体排空上限
type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// TestBodyDrainLimited 验证 handler 未消费的超大请求体不会被全量排空，
// 读取量不超过 maxBodyDrainBytes（防止慢客户端借排空占用服务端 IO）
func TestBodyDrainLimited(t *testing.T) {
	type drainEmptyReq struct{}
	type drainEmptyRes struct{ OK bool }
	router := NewRouter()
	router.POST("/drain", func(_ context.Context, req drainEmptyReq) (drainEmptyRes, error) {
		return drainEmptyRes{OK: true}, nil
	})
	engine := NewEngine()
	engine.Router = router

	big := make([]byte, 64<<10)
	cr := &countingReader{r: bytes.NewReader(big)}
	req := httptest.NewRequest(http.MethodPost, "/drain", cr)
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if cr.n > maxBodyDrainBytes {
		t.Fatalf("drained %d bytes, want <= %d", cr.n, maxBodyDrainBytes)
	}
}
