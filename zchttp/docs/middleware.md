# 中间件

本文说明 `zchttp` 的中间件签名、洋葱模型、`next` 规则、w/r 替换、上下文访问与注册快照。实现细节（chainRunner、位图、requestState）见 [internals.md](internals.md) 的“中间件链”一节。

适合：需要鉴权、日志、追踪、压缩、响应捕获等横切逻辑的开发者。

## 一、签名与基本写法

```go
type NextFunc func(w http.ResponseWriter, r *http.Request) error

type MiddlewareHandler func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error
```

```go
func myMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    // 前置逻辑
    err := next(w, r)
    // 后置逻辑
    return err
}
```

- `next` 之前是前置，`next` 返回之后是后置。
- 返回值 error 向上层中间件传播；最终由引擎按类型分发。
- 不调用 `next` 即短路，后续中间件与 handler 都不执行。
- `next` 必须在当前中间件所属 goroutine 内同步调用；跨 goroutine 属未定义行为。

## 二、洋葱模型

```
A 前置 → B 前置 → core（校验 + handler） → B 后置 → A 后置
```

- `middlewares[0]` 最外层，`finalHandler`（core）最内层。
- 校验发生在 core 内：各层前置已执行、后置尚未执行；路由级中间件紧贴 core，不直接紧贴裸 handler。
- 每层的 `next` 只能调用一次；重复调用返回 `ErrNextCalledMultipleTimes`，不会重新执行下游/handler。

## 三、替换 w / r

```go
func gzipMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    gz := gzip.NewWriter(w)
    wrapped := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
    err := next(wrapped, r)
    gz.Close()
    return err
}
```

每次成功调用 `next(w,r)`：

- 引擎把这对对象记录为当前请求状态；
- 下游中间件、core、handler 通过 `ResponseWriterFromContext`/`RequestFromContext` 看到最近一次传入的对象；
- `OnResponse`/`OnError`/`OnValidationError`/`OnPanic` 也使用这组对象；
- 外层中间件自身的 `w/r` 变量不变，但通过 ctx 访问器读到的是最新状态。

多层替换时，最终 core/访问器/回调使用最内层最后传入的 w/r；调用返回后状态不回退到外层对象。

### `r.WithContext` 注入值

```go
func traceMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    ctx = context.WithValue(ctx, traceKey{}, newTraceID())
    return next(w, r.WithContext(ctx))
}
```

- 传入的 `r` 与当前不同对象时，下游 `ctx` 改为 `r.Context()`。
- 如果新 ctx 不含框架请求状态，引擎会补挂同一份 `requestState`，所以 `RequestFromContext`、`ResponseWriterFromContext`、`EngineFromContext`、`BoundReqFromContext`、`BoundResFromContext` 仍可用。
- 原样传入 `r` 时下游 ctx 与当前层相同；外层中间件的 ctx 参数不受下游替换影响。

替换 `*http.Request` 不会重新绑定参数：`BoundReqFromContext` 仍返回进入中间件链之前已经绑定的 Req。

## 四、在中间件中读 Req/Res

### 已绑定 Req

```go
func authMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    req, err := zchttp.BoundReqFromContext[LoginReq](ctx)
    if err != nil {
        // 绑定阶段出错（*BindingError）：交给 core 统一 400
        return next(w, r)
    }
    if req.Token == "" {
        w.WriteHeader(http.StatusUnauthorized)
        return nil
    }
    return next(w, r)
}
```

- 此时 Req 已完成数据绑定，但尚未做 nonzero/Validate；校验在 core。
- 绑定出错时 `BoundReqFromContext` 返回 `*BindingError`。
- 值类型 `BoundReqFromContext[LoginReq]` 得到浅拷贝；`*LoginReq` 得到 handler 即将使用的同一实例，`next` 前修改会影响 handler 入参，且修改发生在校验之前。
- 类型参数与注册 Req 类型不符时返回 `ErrBoundReqNotFound`，与 ctx 中没有 Req 无法区分。

### handler 的 Res

```go
func responseLogger(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    err := next(w, r)
    res, resErr := zchttp.BoundResFromContext[MyRes](ctx)
    if resErr == nil {
        slog.Default().Info("response", "res", res)
    }
    return err
}
```

`BoundResFromContext` 只在 `next()` 返回后可用；handler 未成功或类型不符返回 `ErrBoundResNotFound`。

## 五、错误传播

- handler/下游中间件的 error 作为 `next` 返回值向上传播；每层可捕获、包装、拦截。
- 最外层仍返回 error 时，引擎按类型分发：
  - `*BindingError` / `*ValidationError` → `OnValidationError`（默认 400）；
  - 其余 → `OnError`（默认 500）。
- 中间件返回 nil 可吞掉错误；返回包装后的 error 会改变类型分发。

## 六、panic 恢复

`ServeHTTP` 入口有 `defer recover`，覆盖路由、绑定、中间件、handler、回调。中间件无需自行 recover。

- `OnPanic` 收到最近一次成功传给 `next` 的 w/r；
- 默认 `DefaultPanicHandler` 记录 panic 与 stack，未写响应才返回 500；
- 自定义 `OnPanic` 若再次 panic，引擎第二层 recover 记录并兜底 500，不会递归调用 `OnPanic`。

## 七、注册与快照

三级入口：

```go
func example() {
    r := zchttp.NewRouter()
    r.Use(logger, recoverError)                    // 全局
    api := r.Group("/api", authMiddleware)         // 分组
    api.GET("/users", listUsers, auditMiddleware)  // 路由级
}
```

- 注册路由时快照 `[全局..., 最外层分组..., 当前分组..., 路由级...]`，之后 `Use` 不影响已注册路由。
- 子分组保留父分组引用：先建子组、后给父组 `Use`，子路由注册时仍继承。
- 路由级中间件只作用于本次注册的路由；`Any` 的 9 个方法共用同一份；注册后修改传入切片不影响快照。
- 框架不校验 nil、不去重：nil 中间件请求期 panic 由 `OnPanic` 兜底；重复传入会执行多次。
- 路由级中间件位于链的最内层，紧贴 core（校验 + handler）。

## 八、常见坑

- 在中间件里重新绑定/解析 body：引擎不会重新绑定；替换 request 也不会。
- 在 `next` 之前修改指针 Req 的字段：会影响 handler 入参，且发生在校验之前；只读请用值类型。
- 在中间件中捕获 panic：可以，但会绕过引擎的统一 `OnPanic`；非必要不要这样做。
- 自定义 ResponseWriter 未透传 `Written()`：默认回调无法判断是否已写，可能重复写响应。详见 [http-engine-callback.md](http-engine-callback.md)。

## 九、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| 链执行、next 防重、位图 | `middleware.go` | `middleware_test.go` |
| requestState、Context 访问器 | `context.go` | `context_test.go` |
| 中间件快照、core 位置 | `buildEntry.go`、`httpEngine.go` | `buildEntry_test.go`、`httpEngine_test.go` |

继续阅读：[internals.md](internals.md)（chainRunner/位图）、[architecture.md](architecture.md)（请求生命周期）、[http-engine-callback.md](http-engine-callback.md)（回调与响应）。
