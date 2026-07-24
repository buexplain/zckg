package zchttp

import (
	"context"
	"errors"
	"net/http"
	"reflect"
)

// ctxKey 是本包内部使用的 context key 类型，避免与外部 key 冲突
type ctxKey string

const (
	requestKey        ctxKey = "zckg.request"
	responseWriterKey ctxKey = "zckg.responseWriter"
	engineKey         ctxKey = "zckg.engine"
	boundReqKey       ctxKey = "zckg.boundReq"
	bindingErrKey     ctxKey = "zckg.bindingErr"
	boundResKey       ctxKey = "zckg.boundRes"
)

// withRequestResponse 将 *http.Request 与 http.ResponseWriter 注入 ctx，供 handler 获取
func withRequestResponse(ctx context.Context, r *http.Request, w http.ResponseWriter) context.Context {
	ctx = context.WithValue(ctx, requestKey, r)
	ctx = context.WithValue(ctx, responseWriterKey, w)
	return ctx
}

// RequestFromContext 从 ctx 中获取当前请求的 *http.Request
// 第二个返回值表示是否存在
func RequestFromContext(ctx context.Context) (*http.Request, bool) {
	r, ok := ctx.Value(requestKey).(*http.Request)
	return r, ok
}

// ResponseWriterFromContext 从 ctx 中获取当前请求的 http.ResponseWriter
// 第二个返回值表示是否存在
func ResponseWriterFromContext(ctx context.Context) (http.ResponseWriter, bool) {
	w, ok := ctx.Value(responseWriterKey).(http.ResponseWriter)
	return w, ok
}

// withEngine 将 *HttpEngine 注入 ctx，供 handler 与中间件获取
func withEngine(ctx context.Context, e *HttpEngine) context.Context {
	return context.WithValue(ctx, engineKey, e)
}

// EngineFromContext 从 ctx 中获取当前请求关联的 *HttpEngine
// 第二个返回值表示是否存在
func EngineFromContext(ctx context.Context) (*HttpEngine, bool) {
	e, ok := ctx.Value(engineKey).(*HttpEngine)
	return e, ok
}

// withBoundReq 将已解析绑定的 Req 注入 ctx，供中间件与 core 层获取
func withBoundReq(ctx context.Context, req any) context.Context {
	return context.WithValue(ctx, boundReqKey, req)
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
	val := ctx.Value(boundReqKey)
	if val == nil {
		var zero T
		return zero, ErrBoundReqNotFound
	}
	// 直接类型断言（T 为指针类型时命中）
	if req, ok := val.(T); ok {
		if err, _ := ctx.Value(bindingErrKey).(error); err != nil {
			return req, err
		}
		return req, nil
	}
	// 兼容值类型：context 中存的是 *Struct，用户传入 Struct 时尝试解引用
	rv := reflect.ValueOf(val)
	if rv.Kind() == reflect.Ptr && !rv.IsNil() {
		elem := rv.Elem().Interface()
		if req, ok := elem.(T); ok {
			if err, _ := ctx.Value(bindingErrKey).(error); err != nil {
				return req, err
			}
			return req, nil
		}
	}
	var zero T
	return zero, ErrBoundReqNotFound
}

// ErrBoundReqNotFound 表示 ctx 中不存在已绑定的 Req
var ErrBoundReqNotFound = errors.New("bound request not found in context")

// withBindingErr 将绑定阶段的错误注入 ctx，与 Req 一同存储，供 core 层统一处理
func withBindingErr(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, bindingErrKey, err)
}

// boundResContainer 共享可变容器，解决 core 层写入的 Res 对前置中间件不可见的问题。
// context 存的是指针（不可变），指针指向的 res 字段可变，所有持有同一指针的层都能读到最新值。
type boundResContainer struct {
	res any
}

// withBoundResContainer 将空的 Res 容器注入 ctx，应在中间件链执行前调用
func withBoundResContainer(ctx context.Context) context.Context {
	return context.WithValue(ctx, boundResKey, &boundResContainer{})
}

// setBoundRes 将 handler 返回的 Res 写入共享容器（core 层调用）
func setBoundRes(ctx context.Context, res any) {
	if c, ok := ctx.Value(boundResKey).(*boundResContainer); ok {
		c.res = res
	}
}

// ErrBoundResNotFound 表示 ctx 中不存在已绑定的 Res
var ErrBoundResNotFound = errors.New("bound response not found in context")

// BoundResFromContext 从 ctx 中获取 handler 的响应结果。
// T 为 Res 的具体类型；若 Res 未注入 ctx 或类型转换失败则返回 ErrBoundResNotFound。
//
// 用法示例：
//
//	res, err := BoundResFromContext[MyRes](ctx)
func BoundResFromContext[T any](ctx context.Context) (T, error) {
	val := ctx.Value(boundResKey)
	if val == nil {
		var zero T
		return zero, ErrBoundResNotFound
	}
	c, ok := val.(*boundResContainer)
	if !ok {
		var zero T
		return zero, ErrBoundResNotFound
	}
	if c.res == nil {
		var zero T
		return zero, ErrBoundResNotFound
	}
	res, ok := c.res.(T)
	if !ok {
		var zero T
		return zero, ErrBoundResNotFound
	}
	return res, nil
}
