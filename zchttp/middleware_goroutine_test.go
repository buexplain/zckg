//go:build !race

package zchttp

import (
	"context"
	"net/http"
	"testing"
)

// 本文件承载 m3-B / m7-② 的跨 goroutine 调用 next() 行为验证。
// NextFunc 的 GoDoc 明确约束：next 必须在所属中间件执行期间的同一 goroutine 内同步调用；
// 跨 goroutine 调用与位图防重标记、层号记录存在 data race，行为未定义。
// 因此这里仅锁死“同步等待型跨 goroutine 调用不 panic 且下游正常执行”的弱保证，
// 且仅在非 -race 模式下运行（见 //go:build 标签），防止未来改动静默放大问题。
//
// 说明：fire-and-forget 型（goroutine 内调 next 后中间件立即返回）在 runChain 归还
// 池对象前无人等待，必然触发越界/复位等未定义行为，无法给出“不 panic”断言，
// 故只验证“中间件等待异步 next 结果”这一行为可观测场景。

// TestRunChainCrossGoroutineNextWeakAssertion 验证中间件在另一 goroutine 内同步调用
// next() 并等待其结果：不 panic、无错误、下游 handler 正常执行。
func TestRunChainCrossGoroutineNextWeakAssertion(t *testing.T) {
	// 中间件开 goroutine 调用 next 并同步等待其结果：执行期间 runChain 尚未返回，
	// 链状态（层号/位图/中间件切片）一致，弱断言可进一步验证下游 handler 正常执行。
	errCh := make(chan error, 1)
	finalCalled := false
	mw := func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
		go func() {
			errCh <- next(w, r)
		}()
		return <-errCh
	}
	if err := runChain([]MiddlewareHandler{mw}, context.Background(), nil, nil, func(w http.ResponseWriter, r *http.Request) error {
		finalCalled = true
		return nil
	}); err != nil {
		t.Fatalf("runChain returned error: %v", err)
	}
	if !finalCalled {
		t.Fatal("final handler should execute when middleware waits for async next()")
	}
}
