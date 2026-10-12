package zchttp

import (
	"context"
	"errors"
	"fmt"
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
			err := next(w, r)
			order = append(order, name+"-post")
			return err
		}
	}
	final := func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
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
			return next(w, r)
		},
	}
	err := runChain(mws, context.Background(), nil, nil, func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
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
		if err := next(w, r); err != nil {
			return err
		}
		outerSecondErr = next(w, r)
		return nil
	}
	mid := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		if err := next(w, r); err != nil {
			return err
		}
		midSecondErr = next(w, r)
		return nil
	}
	final := func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
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
			err := next(w, r)
			postCount++
			return err
		})
	}
	finalCalled := false
	err := runChain(mws, context.Background(), nil, nil, func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
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
		if err := next(w, r); err != nil {
			return err
		}
		secondErr = next(w, r)
		return nil
	})
	for i := 1; i < n; i++ {
		mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			return next(w, r)
		})
	}
	if err := runChain(mws, context.Background(), nil, nil, func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
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
	if err := runChain(nil, context.Background(), nil, nil, func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
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
	if !IsResponseWritten(rw) {
		t.Fatal("expected written=true after WriteHeader")
	}
	releaseResponseWriter(rw)

	w2 := httptest.NewRecorder()
	rw2 := acquireResponseWriter(w2)
	if IsResponseWritten(rw2) {
		t.Fatal("pooled responseWriter must reset written=false")
	}
	// 池化的基础包装器必须绑定新的底层 writer：写入后能到达 w2 即证明绑定正确
	_, _ = rw2.Write([]byte("probe"))
	if w2.Body.String() != "probe" {
		t.Fatal("pooled responseWriter must bind the new underlying writer")
	}
	releaseResponseWriter(rw2)
}

// TestRunChainResponseWriterReplacement 验证中间件可通过 next(w, r) 替换下游的 ResponseWriter
func TestRunChainResponseWriterReplacement(t *testing.T) {
	var receivedW []http.ResponseWriter
	original := httptest.NewRecorder()
	replacement := httptest.NewRecorder()

	mw := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next(replacement, r)
	}
	inner := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		receivedW = append(receivedW, w)
		return next(w, r)
	}
	final := func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
		receivedW = append(receivedW, w)
		return nil
	}

	err := runChain([]MiddlewareHandler{mw, inner}, context.Background(), original, nil, final)
	if err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if len(receivedW) != 2 {
		t.Fatalf("expected 2 received w, got %d", len(receivedW))
	}
	if receivedW[0] != replacement {
		t.Fatal("inner middleware should receive replacement w")
	}
	if receivedW[1] != replacement {
		t.Fatal("final handler should receive replacement w")
	}
}

// TestRunChainRequestReplacement 验证中间件可通过 next(w, r) 替换下游的 *http.Request
func TestRunChainRequestReplacement(t *testing.T) {
	var receivedR []*http.Request
	originalReq := httptest.NewRequest("GET", "/original", nil)
	replacementReq := httptest.NewRequest("GET", "/replacement", nil)

	mw := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next(w, replacementReq)
	}
	inner := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		receivedR = append(receivedR, r)
		return next(w, r)
	}
	final := func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
		receivedR = append(receivedR, r)
		return nil
	}

	err := runChain([]MiddlewareHandler{mw, inner}, context.Background(), httptest.NewRecorder(), originalReq, final)
	if err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if len(receivedR) != 2 {
		t.Fatalf("expected 2 received r, got %d", len(receivedR))
	}
	if receivedR[0].URL.Path != "/replacement" {
		t.Fatalf("inner middleware should receive replacement r, got %s", receivedR[0].URL.Path)
	}
	if receivedR[1].URL.Path != "/replacement" {
		t.Fatalf("final handler should receive replacement r, got %s", receivedR[1].URL.Path)
	}
}

// TestRunChainMultipleLayersReplace 验证多层各自替换 w 时，每层拿到上一层传入的版本
func TestRunChainMultipleLayersReplace(t *testing.T) {
	w1 := httptest.NewRecorder()
	w2 := httptest.NewRecorder()
	w3 := httptest.NewRecorder()
	var seen []http.ResponseWriter

	layerA := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		seen = append(seen, w)
		return next(w2, r)
	}
	layerB := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		seen = append(seen, w)
		return next(w3, r)
	}
	final := func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
		seen = append(seen, w)
		return nil
	}

	err := runChain([]MiddlewareHandler{layerA, layerB}, context.Background(), w1, nil, final)
	if err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if len(seen) != 3 {
		t.Fatalf("expected 3 seen w, got %d", len(seen))
	}
	if seen[0] != w1 {
		t.Fatal("layerA should see original w1")
	}
	if seen[1] != w2 {
		t.Fatal("layerB should see w2 (replaced by layerA)")
	}
	if seen[2] != w3 {
		t.Fatal("final should see w3 (replaced by layerB)")
	}
}

// TestRunChainRecursiveResponseWriterReplacement 验证 >64 层回退递归时 w 替换仍生效
func TestRunChainRecursiveResponseWriterReplacement(t *testing.T) {
	n := maxBitmaskMiddlewares + 1
	replacement := httptest.NewRecorder()
	var finalW http.ResponseWriter

	mws := make([]MiddlewareHandler, 0, n)
	// 第一层替换 w
	mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		return next(replacement, r)
	})
	// 其余层透传
	for i := 1; i < n; i++ {
		mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
			return next(w, r)
		})
	}

	err := runChain(mws, context.Background(), httptest.NewRecorder(), nil, func(_ context.Context, w http.ResponseWriter, r *http.Request) error {
		finalW = w
		return nil
	})
	if err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if finalW != replacement {
		t.Fatal("final handler should receive replacement w in recursive path")
	}
}

type chainCtxKey struct{}

// TestRunChainRequestContextPropagation 锁死 next(w, r') 的 ctx 传递语义（位图与 >64 层递归两条路径）：
//   - 不替换 r：下游与 final 收到的 ctx 与入口 ctx 为同一实例；
//   - r' 的 Context 由中间件 ctx 派生（已含 requestState）：下游直接采用 r'.Context()；
//   - r' 的 Context 由原始 r.Context() 派生（不含 requestState）：下游 ctx 携带注入值，且补挂同一个 requestState。
func TestRunChainRequestContextPropagation(t *testing.T) {
	for _, n := range []int{2, maxBitmaskMiddlewares + 1} {
		for _, mode := range []string{"none", "fromMiddlewareCtx", "fromRequestCtx"} {
			t.Run(fmt.Sprintf("n=%d/%s", n, mode), func(t *testing.T) {
				st := &requestState{}
				entryCtx := context.WithValue(context.Background(), stateKey, st)
				origReq := httptest.NewRequest(http.MethodGet, "/x", nil)
				var replaced *http.Request
				var innerCtx context.Context

				mws := make([]MiddlewareHandler, 0, n)
				mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
					switch mode {
					case "fromMiddlewareCtx":
						replaced = r.WithContext(context.WithValue(ctx, chainCtxKey{}, "mw"))
					case "fromRequestCtx":
						replaced = r.WithContext(context.WithValue(r.Context(), chainCtxKey{}, "mw"))
					default:
						return next(w, r)
					}
					return next(w, replaced)
				})
				for i := 1; i < n; i++ {
					last := i == n-1
					mws = append(mws, func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
						if last {
							innerCtx = ctx
						}
						return next(w, r)
					})
				}
				var finalCtx context.Context
				err := runChain(mws, entryCtx, httptest.NewRecorder(), origReq, func(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
					finalCtx = ctx
					return nil
				})
				if err != nil {
					t.Fatalf("runChain returned error: %v", err)
				}
				if innerCtx != finalCtx {
					t.Fatal("innermost middleware and final must observe the same ctx")
				}
				if requestStateFromContext(finalCtx) != st {
					t.Fatal("final ctx must carry the same requestState")
				}
				if mode == "none" {
					if finalCtx != entryCtx {
						t.Fatal("ctx must stay identical when request is not replaced")
					}
					return
				}
				if got := finalCtx.Value(chainCtxKey{}); got != "mw" {
					t.Fatalf("final ctx value = %v, want mw", got)
				}
				if st.req != replaced {
					t.Fatal("requestState.req must track the replaced request")
				}
				if mode == "fromMiddlewareCtx" && finalCtx != replaced.Context() {
					t.Fatal("ctx already carrying requestState must be adopted as-is")
				}
			})
		}
	}
}

// TestEngineHandlerSeesReplacedRequestContext 验证经引擎执行时，中间件 next(w, r.WithContext(...))
// 注入的值对业务 handler 的 ctx 参数可见，且 handler ctx 上的 Context 访问器仍返回当前请求与引擎
func TestEngineHandlerSeesReplacedRequestContext(t *testing.T) {
	type req struct {
		Name string `form:"name"`
	}
	var (
		gotValue   any
		gotReq     *http.Request
		gotEngine  *HttpEngine
		gotBound   *req
		replaced   *http.Request
		gotBindErr error
	)
	router := NewRouter()
	router.Use(func(_ context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		replaced = r.WithContext(context.WithValue(r.Context(), chainCtxKey{}, "trace-1"))
		return next(w, replaced)
	})
	router.GET("/ctx", func(ctx context.Context, _ req) (struct{}, error) {
		gotValue = ctx.Value(chainCtxKey{})
		gotReq, _ = RequestFromContext(ctx)
		gotEngine, _ = EngineFromContext(ctx)
		gotBound, gotBindErr = BoundReqFromContext[*req](ctx)
		return struct{}{}, nil
	})
	engine := NewEngine()
	engine.Router = router
	rec := serveJSONOn(t, engine, http.MethodGet, "/ctx?name=a", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body=%s", rec.Code, rec.Body.String())
	}
	if gotValue != "trace-1" {
		t.Fatalf("handler ctx value = %v, want trace-1", gotValue)
	}
	if gotReq != replaced {
		t.Fatal("RequestFromContext(handler ctx) must return the replaced request")
	}
	if gotEngine != engine {
		t.Fatal("EngineFromContext(handler ctx) must return the engine")
	}
	if gotBindErr != nil || gotBound == nil || gotBound.Name != "a" {
		t.Fatalf("BoundReqFromContext = %+v, %v; want Name=a", gotBound, gotBindErr)
	}
}

// TestNextContext_NilRequestKeepsCurrent 锁死 nextContext 的防御分支：中间件向 next 传入 nil request 时
// 下游 ctx 沿用当前 ctx（同一实例），不因取 r.Context() 而 panic。
func TestNextContext_NilRequestKeepsCurrent(t *testing.T) {
	cur := context.WithValue(context.Background(), stateKey, &requestState{})
	if got := nextContext(cur, nil); got != cur {
		t.Fatalf("nextContext(cur, nil) = %v, want the same ctx %v", got, cur)
	}
}
