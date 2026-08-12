package zchttp

import (
	"context"
	"errors"
	"net/http"
	"reflect"
)

// stateKey 是 requestState 在 context 中的唯一存储键。
// 请求作用域的所有状态（engine/request/responseWriter/boundReq/bindingErr/res）
// 合并存入单个 requestState 对象，仅执行一次 context.WithValue，
// 避免逐项注入产生的多层 valueCtx 包装与多次堆分配（热路径优化）。
type ctxStateKey struct{}

var stateKey ctxStateKey

// requestState 承载单个请求作用域内的全部上下文状态。
// 在 ServeHTTP 路由命中后一次性创建并注入 ctx，后续字段原地更新，
// 所有持有同一 ctx 的中间件层（包括后置阶段）均可见最新值。
type requestState struct {
	engine     *HttpEngine
	req        *http.Request
	w          http.ResponseWriter
	boundReq   any
	bindingErr error
	res        any
}

// requestStateFromContext 从 ctx 中取出当前请求的状态对象，不存在时返回 nil
func requestStateFromContext(ctx context.Context) *requestState {
	st, _ := ctx.Value(stateKey).(*requestState)
	return st
}

// withEngine 将 *HttpEngine 注入 ctx，供 handler 与中间件获取
func withEngine(ctx context.Context, e *HttpEngine) context.Context {
	st := requestStateFromContext(ctx)
	if st == nil {
		st = &requestState{}
		ctx = context.WithValue(ctx, stateKey, st)
	}
	st.engine = e
	return ctx
}

// withRequestResponse 将 *http.Request 与 http.ResponseWriter 注入 ctx，供 handler 获取
func withRequestResponse(ctx context.Context, r *http.Request, w http.ResponseWriter) context.Context {
	st := requestStateFromContext(ctx)
	if st == nil {
		st = &requestState{}
		ctx = context.WithValue(ctx, stateKey, st)
	}
	st.req = r
	st.w = w
	return ctx
}

// withBoundReq 将已解析绑定的 Req 注入 ctx，供中间件与 core 层获取
func withBoundReq(ctx context.Context, req any) context.Context {
	st := requestStateFromContext(ctx)
	if st == nil {
		st = &requestState{}
		ctx = context.WithValue(ctx, stateKey, st)
	}
	st.boundReq = req
	return ctx
}

// withBindingErr 将绑定阶段的错误注入 ctx，与 Req 一同存储，供 core 层统一处理
func withBindingErr(ctx context.Context, err error) context.Context {
	st := requestStateFromContext(ctx)
	if st == nil {
		st = &requestState{}
		ctx = context.WithValue(ctx, stateKey, st)
	}
	st.bindingErr = err
	return ctx
}

// boundResContainer 是旧版 Res 共享容器，仅保留用于兼容读取：
// 若外部在调用链上游以旧方式注入了该容器，BoundResFromContext 仍可读取
type boundResContainer struct {
	res any
}

// boundResKey 旧版 Res 容器的 context key（兼容读取用）
type ctxBoundResKey struct{}

var boundResKey ctxBoundResKey

// withBoundResContainer 标记 ctx 已具备 Res 共享能力。
// 新版实现中 Res 存储于 requestState.res，本函数仅在 state 缺失时
// 兜底注入旧版容器以保持向后兼容，正常请求路径不产生额外分配
func withBoundResContainer(ctx context.Context) context.Context {
	if requestStateFromContext(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, boundResKey, &boundResContainer{})
}

// setBoundRes 将 handler 返回的 Res 写入共享存储（core 层调用）
func setBoundRes(ctx context.Context, res any) {
	if st := requestStateFromContext(ctx); st != nil {
		st.res = res
		return
	}
	if c, ok := ctx.Value(boundResKey).(*boundResContainer); ok {
		c.res = res
	}
}

// RequestFromContext 从 ctx 中获取当前请求的 *http.Request
// 第二个返回值表示是否存在
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	if st := requestStateFromContext(ctx); st != nil {
		return st.req, st.req != nil
	}
	return nil, false
}

// ResponseWriterFromContext 从 ctx 中获取当前请求的 http.ResponseWriter
// 第二个返回值表示是否存在
func ResponseWriterFromContext(ctx context.Context) (http.ResponseWriter, bool) {
	if st := requestStateFromContext(ctx); st != nil {
		return st.w, st.w != nil
	}
	return nil, false
}

// EngineFromContext 从 ctx 中获取当前请求关联的 *HttpEngine
// 第二个返回值表示是否存在
func EngineFromContext(ctx context.Context) (*HttpEngine, bool) {
	if st := requestStateFromContext(ctx); st != nil {
		return st.engine, st.engine != nil
	}
	return nil, false
}

// BoundReqFromContext 从 ctx 中获取路由命中时已解析绑定的 Req。
// T 为 Req 的具体类型（指针或值均可），若绑定阶段出错则 err 为非 nil 的 *BindingError；
// 若 Req 未注入 ctx 则返回 ErrBoundReqNotFound。
//
// 注意：context 中存储的是 *Req（指针），推荐使用指针类型调用：
//
//	req, err := BoundReqFromContext[*MyReq](ctx)
//
// 也支持值类型调用（内部自动解引用）：
//
//	req, err := BoundReqFromContext[MyReq](ctx)
func BoundReqFromContext[T any](ctx context.Context) (T, error) {
	var val any
	var bindingErr error
	if st := requestStateFromContext(ctx); st != nil {
		val = st.boundReq
		bindingErr = st.bindingErr
	}
	if val == nil {
		var zero T
		return zero, ErrBoundReqNotFound
	}
	// 直接类型断言（T 为指针类型时命中）
	if req, ok := val.(T); ok {
		if bindingErr != nil {
			return req, bindingErr
		}
		return req, nil
	}
	// 兼容值类型：context 中存的是 *Struct，用户传入 Struct 时尝试解引用
	rv := reflect.ValueOf(val)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() {
		elem := rv.Elem().Interface()
		if req, ok := elem.(T); ok {
			if bindingErr != nil {
				return req, bindingErr
			}
			return req, nil
		}
	}
	var zero T
	return zero, ErrBoundReqNotFound
}

// ErrBoundReqNotFound 表示 ctx 中不存在已绑定的 Req
var ErrBoundReqNotFound = errors.New("bound request not found in context")

// ErrBoundResNotFound 表示 ctx 中不存在已绑定的 Res
var ErrBoundResNotFound = errors.New("bound response not found in context")

// BoundResFromContext 从 ctx 中获取 handler 的响应结果。
// T 为 Res 的具体类型；若 Res 未注入 ctx 或类型转换失败则返回 ErrBoundResNotFound。
//
// 用法示例：
//
//	res, err := BoundResFromContext[MyRes](ctx)
func BoundResFromContext[T any](ctx context.Context) (T, error) {
	var res any
	if st := requestStateFromContext(ctx); st != nil {
		res = st.res
	} else if c, ok := ctx.Value(boundResKey).(*boundResContainer); ok {
		res = c.res
	}
	if res == nil {
		var zero T
		return zero, ErrBoundResNotFound
	}
	v, ok := res.(T)
	if !ok {
		var zero T
		return zero, ErrBoundResNotFound
	}
	return v, nil
}
