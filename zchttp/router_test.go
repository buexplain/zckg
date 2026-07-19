package zchttp

import (
	"context"
	"fmt"
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
