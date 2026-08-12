package zchttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ========== 测试辅助类型 ==========

type engineReq struct {
	Name string `json:"name"`
}

type engineRes struct {
	Message string `json:"message"`
}

// ========== context 存取测试 ==========

// TestRequestResponseFromContext 验证 handler 可从 ctx 获取 *http.Request 与 ResponseWriter
func TestRequestResponseFromContext(t *testing.T) {
	router := NewRouter()
	router.GET("/ctx", func(ctx context.Context, req engineReq) (engineRes, error) {
		r, ok := RequestFromContext(ctx)
		if !ok || r == nil {
			t.Fatalf("request not found in ctx")
		}
		w, ok := ResponseWriterFromContext(ctx)
		if !ok || w == nil {
			t.Fatalf("response writer not found in ctx")
		}
		// 直接通过 ctx 中的 ResponseWriter 写文件内容
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("path=" + r.URL.Path))
		return engineRes{Message: "ignored"}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ctx", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// handler 已写入响应，默认回调应跳过 JSON
	if body := rec.Body.String(); body != "path=/ctx" {
		t.Fatalf("expected body written via ctx writer, got %q", body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Fatalf("content-type should stay text/plain, got %q", ct)
	}
}

// TestEngineFromContext 验证中间件/handler 可从 ctx 获取 *HttpEngine
func TestEngineFromContext(t *testing.T) {
	var gotEngine *HttpEngine

	router := NewRouter()
	router.GET("/eng", func(ctx context.Context, _ engineReq) (engineRes, error) {
		e, ok := EngineFromContext(ctx)
		if !ok {
			t.Fatalf("EngineFromContext returned false")
		}
		gotEngine = e
		return engineRes{}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/eng", nil))

	if gotEngine != engine {
		t.Fatal("EngineFromContext should return the same engine instance")
	}
}

// TestBoundResFromContext 验证后置中间件可从 ctx 获取 handler 的 Res
func TestBoundResFromContext(t *testing.T) {
	var capturedMsg string

	router := NewRouter()
	router.Use(func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		err := next()
		// 后置阶段获取 Res
		res, resErr := BoundResFromContext[engineRes](ctx)
		if resErr != nil {
			t.Fatalf("BoundResFromContext failed: %v", resErr)
		}
		capturedMsg = res.Message
		return err
	})
	router.GET("/res", func(_ context.Context, _ engineReq) (engineRes, error) {
		return engineRes{Message: "from handler"}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/res", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if capturedMsg != "from handler" {
		t.Fatalf("capturedMsg = %q, want 'from handler'", capturedMsg)
	}
}

// TestBoundResFromContext_NotFound 验证 handler 未执行时 BoundResFromContext 返回错误
func TestBoundResFromContext_NotFound(t *testing.T) {
	var resErr error

	router := NewRouter()
	router.Use(func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		// 不调用 next，handler 不执行，Res 为空
		_, resErr = BoundResFromContext[engineRes](ctx)
		return nil
	})
	router.GET("/no-res", func(_ context.Context, _ engineReq) (engineRes, error) {
		return engineRes{Message: "never"}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/no-res", nil))

	if resErr == nil {
		t.Fatal("BoundResFromContext should return error when Res not set")
	}
}

// TestBoundReqFromContext_ValueAndPointer 验证 BoundReqFromContext 同时支持指针和值类型调用
func TestBoundReqFromContext_ValueAndPointer(t *testing.T) {
	var (
		ptrReq *engineReq
		valReq engineReq
		ptrErr error
		valErr error
	)

	router := NewRouter()
	router.Use(func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		// 用指针类型获取
		ptrReq, ptrErr = BoundReqFromContext[*engineReq](ctx)
		// 用值类型获取（内部自动解引用）
		valReq, valErr = BoundReqFromContext[engineReq](ctx)
		return next()
	})
	router.GET("/bound-req", func(_ context.Context, req engineReq) (engineRes, error) {
		return engineRes{Message: req.Name}, nil
	})

	engine := NewEngine()
	engine.Router = router

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/bound-req?name=alice", nil))

	if ptrErr != nil {
		t.Fatalf("BoundReqFromContext[*engineReq] failed: %v", ptrErr)
	}
	if ptrReq == nil || ptrReq.Name != "alice" {
		t.Fatalf("pointer req: got %+v, want Name=alice", ptrReq)
	}

	if valErr != nil {
		t.Fatalf("BoundReqFromContext[engineReq] failed: %v", valErr)
	}
	if valReq.Name != "alice" {
		t.Fatalf("value req: got Name=%q, want alice", valReq.Name)
	}
}

// ======== BoundReq/BoundRes 边界分支补测 ========

type ctxMismatchReq struct{ X int }

// TestBoundReqFromContext_NotInjected 覆盖未注入时返回 ErrBoundReqNotFound 的分支
func TestBoundReqFromContext_NotInjected(t *testing.T) {
	_, err := BoundReqFromContext[*engineReq](context.Background())
	if !errors.Is(err, ErrBoundReqNotFound) {
		t.Fatalf("expected ErrBoundReqNotFound, got: %v", err)
	}
}

// TestBoundReqFromContext_PointerWithBindingErr 覆盖指针断言命中且存在绑定错误的分支
func TestBoundReqFromContext_PointerWithBindingErr(t *testing.T) {
	ctx := withBoundReq(context.Background(), &engineReq{Name: "bob"})
	ctx = withBindingErr(ctx, NewBindingError(errors.New("boom")))
	req, err := BoundReqFromContext[*engineReq](ctx)
	if err == nil {
		t.Fatal("expected binding error")
	}
	if req == nil || req.Name != "bob" {
		t.Fatalf("req should still be returned, got: %+v", req)
	}
}

// TestBoundReqFromContext_DerefWithBindingErr 覆盖值类型解引用命中且存在绑定错误的分支
func TestBoundReqFromContext_DerefWithBindingErr(t *testing.T) {
	ctx := withBoundReq(context.Background(), &engineReq{Name: "carol"})
	ctx = withBindingErr(ctx, NewBindingError(errors.New("boom")))
	req, err := BoundReqFromContext[engineReq](ctx)
	if err == nil {
		t.Fatal("expected binding error")
	}
	if req.Name != "carol" {
		t.Fatalf("req.Name = %q, want carol", req.Name)
	}
}

// TestBoundReqFromContext_NilPointerStored 覆盖存储 nil 指针时解引用跳过的分支
func TestBoundReqFromContext_NilPointerStored(t *testing.T) {
	ctx := withBoundReq(context.Background(), (*engineReq)(nil))
	_, err := BoundReqFromContext[engineReq](ctx)
	if !errors.Is(err, ErrBoundReqNotFound) {
		t.Fatalf("expected ErrBoundReqNotFound, got: %v", err)
	}
}

// TestBoundReqFromContext_ValueStoredWithPointerT 覆盖存储值类型但以指针类型请求的分支
func TestBoundReqFromContext_ValueStoredWithPointerT(t *testing.T) {
	ctx := withBoundReq(context.Background(), engineReq{Name: "dave"})
	_, err := BoundReqFromContext[*engineReq](ctx)
	if !errors.Is(err, ErrBoundReqNotFound) {
		t.Fatalf("expected ErrBoundReqNotFound, got: %v", err)
	}
}

// TestBoundResFromContext_NotInjected 覆盖 Res 容器未注入的分支
func TestBoundResFromContext_NotInjected(t *testing.T) {
	_, err := BoundResFromContext[engineRes](context.Background())
	if !errors.Is(err, ErrBoundResNotFound) {
		t.Fatalf("expected ErrBoundResNotFound, got: %v", err)
	}
}

// TestBoundResFromContext_NotContainer 覆盖存储值不是容器的分支
func TestBoundResFromContext_NotContainer(t *testing.T) {
	ctx := context.WithValue(context.Background(), boundResKey, "junk")
	_, err := BoundResFromContext[engineRes](ctx)
	if !errors.Is(err, ErrBoundResNotFound) {
		t.Fatalf("expected ErrBoundResNotFound, got: %v", err)
	}
}

// TestBoundResFromContext_ResNil 覆盖容器内 Res 尚未写入的分支
func TestBoundResFromContext_ResNil(t *testing.T) {
	ctx := withBoundResContainer(context.Background())
	_, err := BoundResFromContext[engineRes](ctx)
	if !errors.Is(err, ErrBoundResNotFound) {
		t.Fatalf("expected ErrBoundResNotFound, got: %v", err)
	}
}

// TestBoundResFromContext_TypeMismatch 覆盖 Res 类型断言失败的分支
func TestBoundResFromContext_TypeMismatch(t *testing.T) {
	ctx := withBoundResContainer(context.Background())
	setBoundRes(ctx, engineRes{Message: "ok"})
	_, err := BoundResFromContext[ctxMismatchReq](ctx)
	if !errors.Is(err, ErrBoundResNotFound) {
		t.Fatalf("expected ErrBoundResNotFound, got: %v", err)
	}
}

// TestContextAccessors_NoState 覆盖 ctx 中无 requestState 时各访问器的"不存在"分支
func TestContextAccessors_NoState(t *testing.T) {
	ctx := context.Background()
	if _, ok := EngineFromContext(ctx); ok {
		t.Fatal("EngineFromContext should return false without state")
	}
	if _, ok := RequestFromContext(ctx); ok {
		t.Fatal("RequestFromContext should return false without state")
	}
	if _, ok := ResponseWriterFromContext(ctx); ok {
		t.Fatal("ResponseWriterFromContext should return false without state")
	}
}

// TestWithHelpers_CreateStateFromBareCtx 覆盖 withRequestResponse/withBindingErr
// 在无状态 ctx 上自动创建 requestState 的兜底分支
func TestWithHelpers_CreateStateFromBareCtx(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/bare", nil)
	rec := httptest.NewRecorder()
	ctx := withRequestResponse(context.Background(), r, rec)
	if got, ok := RequestFromContext(ctx); !ok || got != r {
		t.Fatal("withRequestResponse should create state on bare ctx")
	}

	ctx = withBindingErr(context.Background(), NewBindingError(errors.New("bare-err")))
	_, err := BoundReqFromContext[engineReq](ctx)
	if !errors.Is(err, ErrBoundReqNotFound) {
		t.Fatalf("expected ErrBoundReqNotFound for bare ctx binding err, got: %v", err)
	}
}

// TestContextHelpers_CreateStateOnDemand 验证 with* 兼容助手在 ctx 无状态时
// 自动创建 requestState 并注入，各存取器可正常读回（向后兼容路径锁死）
func TestContextHelpers_CreateStateOnDemand(t *testing.T) {
	ctx := context.Background()

	e := NewEngine()
	ctx = withEngine(ctx, e)
	if got, ok := EngineFromContext(ctx); !ok || got != e {
		t.Fatalf("EngineFromContext = %v, %v; want same engine, true", got, ok)
	}

	r := httptest.NewRequest(http.MethodGet, "/compat", nil)
	rec := httptest.NewRecorder()
	ctx = withRequestResponse(ctx, r, rec)
	if got, ok := RequestFromContext(ctx); !ok || got != r {
		t.Fatal("RequestFromContext should return the injected request")
	}
	if got, ok := ResponseWriterFromContext(ctx); !ok || got != rec {
		t.Fatal("ResponseWriterFromContext should return the injected writer")
	}

	ctx = withBoundReq(ctx, &engineReq{Name: "compat"})
	req, err := BoundReqFromContext[*engineReq](ctx)
	if err != nil || req.Name != "compat" {
		t.Fatalf("BoundReqFromContext = %v, %v; want compat req, nil", req, err)
	}

	ctx = withBindingErr(ctx, NewBindingError(errors.New("boom")))
	if _, err := BoundReqFromContext[*engineReq](ctx); err == nil {
		t.Fatal("BoundReqFromContext should surface binding error")
	}

	// 已有 state 时 withBoundResContainer 直接返回原 ctx，Res 写入 state 可被读回
	ctx2 := withBoundResContainer(ctx)
	setBoundRes(ctx2, engineRes{Message: "compat-res"})
	res, err := BoundResFromContext[engineRes](ctx2)
	if err != nil || res.Message != "compat-res" {
		t.Fatalf("BoundResFromContext = %v, %v; want compat-res, nil", res, err)
	}
}
