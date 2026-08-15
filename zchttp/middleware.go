package zchttp

import (
	"context"
	"errors"
	"net/http"
	"sync"
)

// ErrNextCalledMultipleTimes 表示某个中间件对 next() 的调用超过一次。
// 重复调用不会重新执行下游链与 handler，而是直接返回此错误，
// 避免写库等业务副作用被静默重复触发。
var ErrNextCalledMultipleTimes = errors.New("middleware next() called multiple times")

// NextFunc 调用后继续执行下一层中间件，若当前已是最后一层则执行路由 handler
// 返回值 error 为下游（后续中间件或 handler）产生的错误
// 若不调用，则后续中间件与 handler 均不会执行（短路）
//
// 约束：next 必须在所属中间件执行期间的同一 goroutine 内同步调用。
// 跨 goroutine 调用会与位图防重标记与层号记录产生 data race，且层号错配
// 可能导致下游重复执行，行为未定义。
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
//
// 约束：next 必须在当前中间件的同一 goroutine 内同步调用（见 NextFunc 说明）。
type MiddlewareHandler func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error

// maxBitmaskMiddlewares 是位图防重标记支持的中间件层数上限，超出则回退递归实现（极罕见场景）
const maxBitmaskMiddlewares = 64

// chainRunner 承载中间件链执行状态：所有层共享同一个 next 函数值（创建池对象时
// 一次性绑定），用位图记录各层 next() 是否已被调用，避免递归实现中每层
// 都分配一个捕获 called 标记的闭包（热路径优化）
type chainRunner struct {
	middlewares []MiddlewareHandler
	ctx         context.Context
	w           http.ResponseWriter
	r           *http.Request
	final       func() error
	nextFunc    NextFunc // 池对象创建时绑定 c.advance，此后不再重新分配
	calledBits  uint64   // 第 i 层 next() 已调用标记（位 i）
	current     int      // 当前正在执行的中间件层号
}

// chainRunnerPool 复用 chainRunner；nextFunc 在 New 时绑定，随对象一同复用
var chainRunnerPool = sync.Pool{
	New: func() any {
		c := &chainRunner{}
		c.nextFunc = c.advance
		return c
	},
}

// runChain 从外到内依次执行中间件，最内层执行 finalHandler
// 这构成了洋葱模型：middlewares[0] 在最外层，finalHandler 在最内层。
// 每层的 next 仅允许调用一次，重复调用返回 ErrNextCalledMultipleTimes，
// 防止下游中间件与 handler 被重复执行。
func runChain(middlewares []MiddlewareHandler, ctx context.Context, w http.ResponseWriter, r *http.Request, finalHandler func() error) error {
	if len(middlewares) == 0 {
		return finalHandler()
	}
	if len(middlewares) > maxBitmaskMiddlewares {
		return runChainRecursive(middlewares, ctx, w, r, finalHandler)
	}
	c := chainRunnerPool.Get().(*chainRunner)
	c.middlewares = middlewares
	c.ctx = ctx
	c.w = w
	c.r = r
	c.final = finalHandler
	err := c.exec(0)
	// 清空引用避免经池长时间持有请求对象
	c.middlewares = nil
	c.ctx = nil
	c.w = nil
	c.r = nil
	c.final = nil
	c.calledBits = 0
	c.current = 0
	chainRunnerPool.Put(c)
	return err
}

// exec 执行第 level 层中间件；层号越界时执行最内层 finalHandler。
// 进入前记录并切换 current，退出时恢复，保证 next() 总能定位到调用者所在层
func (c *chainRunner) exec(level int) error {
	if level == len(c.middlewares) {
		return c.final()
	}
	prev := c.current
	c.current = level
	err := c.middlewares[level](c.ctx, c.w, c.r, c.nextFunc)
	c.current = prev
	return err
}

// advance 是所有层共享的 next 实现：按调用者层号做防重标记，再执行下一层
func (c *chainRunner) advance() error {
	bit := uint64(1) << c.current
	if c.calledBits&bit != 0 {
		return ErrNextCalledMultipleTimes
	}
	c.calledBits |= bit
	return c.exec(c.current + 1)
}

// runChainRecursive 是递归实现，作为中间件层数超过位图上限时的回退路径；
// 每层分配一个捕获 called 标记的 next 闭包
func runChainRecursive(middlewares []MiddlewareHandler, ctx context.Context, w http.ResponseWriter, r *http.Request, finalHandler func() error) error {
	if len(middlewares) == 0 {
		return finalHandler()
	}
	called := false
	return middlewares[0](ctx, w, r, func() error {
		if called {
			return ErrNextCalledMultipleTimes
		}
		called = true
		return runChainRecursive(middlewares[1:], ctx, w, r, finalHandler)
	})
}
