package zchttp

import (
	"context"
	"net/http"
)

// NextFunc 调用后继续执行下一层中间件，若当前已是最后一层则执行路由 handler
// 返回值 error 为下游（后续中间件或 handler）产生的错误
// 若不调用，则后续中间件与 handler 均不会执行（短路）
type NextFunc func() error

// MiddlewareHandler 洋葱模型中间件函数签名
// next 调用之前的逻辑为"前置"，next() 返回后的逻辑为"后置"
// 返回非 nil error 时，错误将向上层中间件传播
//
// 用法示例：
//
//	func(ctx, w, r, next) error {
//	    // 前置逻辑
//	    err := next()
//	    // 后置逻辑
//	    return err
//	}
type MiddlewareHandler func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error

// runChain 从外到内依次执行中间件，最内层执行 finalHandler
// 这构成了洋葱模型：middlewares[0] 在最外层，finalHandler 在最内层
func runChain(middlewares []MiddlewareHandler, ctx context.Context, w http.ResponseWriter, r *http.Request, finalHandler func() error) error {
	if len(middlewares) == 0 {
		return finalHandler()
	}
	return middlewares[0](ctx, w, r, func() error {
		return runChain(middlewares[1:], ctx, w, r, finalHandler)
	})
}
