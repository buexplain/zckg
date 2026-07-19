package zchttp

import (
	"context"
	"errors"
	"net/http"
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
// T 为 Req 的具体类型，若绑定阶段出错则 err 为非 nil 的 *BindingError；
// 若 Req 未注入 ctx 则返回 ErrBoundReqNotFound。
//
// 用法示例：
//
//	req, err := BoundReqFromContext[MyReq](ctx)
func BoundReqFromContext[T any](ctx context.Context) (T, error) {
	val := ctx.Value(boundReqKey)
	if val == nil {
		var zero T
		return zero, ErrBoundReqNotFound
	}
	req, ok := val.(T)
	if !ok {
		var zero T
		return zero, ErrBoundReqNotFound
	}
	// 绑定阶段产生的错误随 Req 一同注入 ctx，供 core 层统一处理
	if err, _ := ctx.Value(bindingErrKey).(error); err != nil {
		return req, err
	}
	return req, nil
}

// ErrBoundReqNotFound 表示 ctx 中不存在已绑定的 Req
var ErrBoundReqNotFound = errors.New("bound request not found in context")

// withBindingErr 将绑定阶段的错误注入 ctx，与 Req 一同存储，供 core 层统一处理
func withBindingErr(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, bindingErrKey, err)
}

// withBoundRes 将 handler 返回的 Res 注入 ctx，供中间件后置阶段与 OnResponse 回调获取
func withBoundRes(ctx context.Context, res any) context.Context {
	return context.WithValue(ctx, boundResKey, res)
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
	res, ok := val.(T)
	if !ok {
		var zero T
		return zero, ErrBoundResNotFound
	}
	return res, nil
}
