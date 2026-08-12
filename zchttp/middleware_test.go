package zchttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRunChainOrder 验证洋葱模型执行顺序与错误传播
func TestRunChainOrder(t *testing.T) {
	var order []string
	mw := func(name string) MiddlewareHandler {
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			order = append(order, name+"-pre")
			err := next()
			order = append(order, name+"-post")
			return err
		}
	}
	final := func() error {
		order = append(order, "final")
		return nil
	}
	err := runChain([]MiddlewareHandler{mw("a"), mw("b"), mw("c")}, context.Background(), nil, nil, final)
	if err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	want := []string{"a-pre", "b-pre", "c-pre", "final", "c-post", "b-post", "a-post"}
	if len(order) != len(want) {
		t.Fatalf("order = %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("order = %v, want %v", order, want)
		}
	}
}

// TestRunChainShortCircuit 验证不调用 next 时下游与 handler 均不执行
func TestRunChainShortCircuit(t *testing.T) {
	finalCalled := false
	innerCalled := false
	mws := []MiddlewareHandler{
		func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			return errors.New("short-circuit")
		},
		func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			innerCalled = true
			return next()
		},
	}
	err := runChain(mws, context.Background(), nil, nil, func() error {
		finalCalled = true
		return nil
	})
	if err == nil || err.Error() != "short-circuit" {
		t.Fatalf("expected short-circuit error, got %v", err)
	}
	if innerCalled || finalCalled {
		t.Fatalf("downstream should not run: innerCalled=%v finalCalled=%v", innerCalled, finalCalled)
	}
}

// TestRunChainNextCalledTwiceEachLevel 验证每一层的 next 都只允许调用一次：
// 外层与中间层分别重复调用 next() 时，第二次均返回 ErrNextCalledMultipleTimes，
// 且下游 handler 只执行一次（位图防重标记按层独立）
func TestRunChainNextCalledTwiceEachLevel(t *testing.T) {
	handlerCalls := 0
	var outerSecondErr, midSecondErr error

	outer := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		if err := next(); err != nil {
			return err
		}
		outerSecondErr = next()
		return nil
	}
	mid := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		if err := next(); err != nil {
			return err
		}
		midSecondErr = next()
		return nil
	}
	final := func() error {
		handlerCalls++
		return nil
	}

	if err := runChain([]MiddlewareHandler{outer, mid}, context.Background(), nil, nil, final); err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler executed %d times, want 1", handlerCalls)
	}
	if !errors.Is(outerSecondErr, ErrNextCalledMultipleTimes) {
		t.Fatalf("outer second next() = %v, want ErrNextCalledMultipleTimes", outerSecondErr)
	}
	if !errors.Is(midSecondErr, ErrNextCalledMultipleTimes) {
		t.Fatalf("mid second next() = %v, want ErrNextCalledMultipleTimes", midSecondErr)
	}
}

// TestRunChainFallbackOver64 验证中间件层数超过位图上限时回退递归实现且语义一致
func TestRunChainFallbackOver64(t *testing.T) {
	n := maxBitmaskMiddlewares + 1
	var preCount, postCount int
	mws := make([]MiddlewareHandler, 0, n)
	for i := 0; i < n; i++ {
		mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			preCount++
			err := next()
			postCount++
			return err
		})
	}
	finalCalled := false
	err := runChain(mws, context.Background(), nil, nil, func() error {
		finalCalled = true
		return nil
	})
	if err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if !finalCalled || preCount != n || postCount != n {
		t.Fatalf("fallback chain broken: final=%v pre=%d post=%d want n=%d", finalCalled, preCount, postCount, n)
	}
}

// TestRunChainRecursiveNextCalledTwice 验证 >64 层回退递归实现中
// 重复调用 next() 同样被拦截且下游 handler 只执行一次
func TestRunChainRecursiveNextCalledTwice(t *testing.T) {
	n := maxBitmaskMiddlewares + 1
	handlerCalls := 0
	var secondErr error
	mws := make([]MiddlewareHandler, 0, n)
	// 第一层双重调用 next()，其余层透传
	mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		if err := next(); err != nil {
			return err
		}
		secondErr = next()
		return nil
	})
	for i := 1; i < n; i++ {
		mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			return next()
		})
	}
	if err := runChain(mws, context.Background(), nil, nil, func() error {
		handlerCalls++
		return nil
	}); err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("handler executed %d times, want 1", handlerCalls)
	}
	if !errors.Is(secondErr, ErrNextCalledMultipleTimes) {
		t.Fatalf("recursive second next() = %v, want ErrNextCalledMultipleTimes", secondErr)
	}
}

// TestRunChainEmpty 验证无中间件时直接执行 handler
func TestRunChainEmpty(t *testing.T) {
	called := false
	if err := runChain(nil, context.Background(), nil, nil, func() error {
		called = true
		return nil
	}); err != nil || !called {
		t.Fatalf("empty chain failed: err=%v called=%v", err, called)
	}
}

// TestResponseWriterPoolReuse 验证池化的 responseWriter 归还后状态被正确重置，
// 下一次取出时不复用上一请求的 written 标记与底层 writer
func TestResponseWriterPoolReuse(t *testing.T) {
	w1 := httptest.NewRecorder()
	rw := acquireResponseWriter(w1)
	rw.WriteHeader(http.StatusOK)
	if !rw.Written() {
		t.Fatal("expected written=true after WriteHeader")
	}
	releaseResponseWriter(rw)

	w2 := httptest.NewRecorder()
	rw2 := acquireResponseWriter(w2)
	if rw2.Written() {
		t.Fatal("pooled responseWriter must reset written=false")
	}
	if rw2.ResponseWriter != w2 {
		t.Fatal("pooled responseWriter must bind the new underlying writer")
	}
	releaseResponseWriter(rw2)
}
