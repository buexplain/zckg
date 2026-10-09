# 中间件规则

本框架采用**洋葱模型（Onion Model）**实现中间件，支持前置逻辑、后置逻辑与短路控制。相关实现位于 `middleware.go`，执行位于 `httpEngine.go` 的 `ServeHTTP`。

## 一、中间件签名

```go
type NextFunc func(w http.ResponseWriter, r *http.Request) error

type MiddlewareHandler func(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error
```

- `next` 调用**之前**的逻辑为「前置」处理。
- `next(w, r)` 返回**之后**的逻辑为「后置」处理。
- 返回值 `error` 会向上层中间件传播。
- `next` 接收的 `w` 和 `r` 将传递给下游；若无需替换，原样传入当前收到的 `w` 和 `r` 即可。

标准写法：

```go
func myMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    // 前置逻辑
    err := next(w, r)
    // 后置逻辑
    return err
}
```

## 二、洋葱模型执行顺序

中间件按注册顺序由外到内包裹，handler 位于最内层。假设注册了中间件 A、B，则执行顺序为：

```
A 前置 → B 前置 → handler → B 后置 → A 后置
```

流程示意：

```
请求进入
  │
  ▼
Middleware A ── 前置逻辑
  │
  ├──▶ Middleware B ── 前置逻辑
  │     │
  │     ├──▶ Handler（最内层）
  │     │
  │  ◀──┤ B 后置逻辑
  │
  ◀──┤ A 后置逻辑
  │
  ▼
响应返回
```

该链由 `runChain` 执行：`middlewares[0]` 在最外层，`finalHandler`（handler 调用）在最内层。每层的 `next` 仅允许调用一次，重复调用不会重新执行下游链与 handler，而是直接返回 `ErrNextCalledMultipleTimes`，避免业务副作用被重复触发。

实现上不为每层构造闭包：执行状态由池化的 `chainRunner` 对象承载，各层 `next()` 是否已调用以 `uint64` 位图按层号记录（热路径零闭包分配）；仅当中间件层数超过 64 层（极罕见场景）时回退为递归实现（`runChainRecursive`），语义与防重规则完全一致。

> **并发约束**：`next()` 必须在中间件所属的 goroutine 内调用，不得另起 goroutine 调用 `next()`。内部调用状态标记（位图 / bool）未做并发安全保护。

## 三、替换 ResponseWriter 与 Request

中间件可通过 `next(w, r)` 向下游传递替换的 `http.ResponseWriter` 或 `*http.Request`。典型场景包括 gzip 压缩、响应体捕获、请求头注入等：

```go
// gzip 压缩中间件
func gzipMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    gz := gzip.NewWriter(w)
    wrapped := &gzipResponseWriter{Writer: gz, ResponseWriter: w}
    err := next(wrapped, r)   // 下游及 Context 访问器观察到 wrapped
    gz.Close()
    return err
}
```

每次成功调用 `next(w, r)` 时，引擎都会先把这组对象记录为当前请求状态，再进入下一层。因此以下位置观察到的是同一组、最近一次传给 `next` 的对象：

- 下游中间件与最内层 core；
- 业务 handler 通过 `ResponseWriterFromContext(ctx)`、`RequestFromContext(ctx)` 取得的对象；
- `OnResponse`、`OnError`、`OnValidationError` 与 `OnPanic` 回调收到的对象。

多层中间件可逐层替换。外层把 `w2/r2` 传给 `next`、内层再把 `w3/r3` 传给 `next` 时，core、Context 访问器和链后回调最终都使用 `w3/r3`。该状态在调用返回时不会回退为外层对象。

替换 `*http.Request` 不会重新执行参数绑定：`BoundReqFromContext` 仍返回进入中间件链之前已经绑定的 Req。若无需替换 w/r，原样透传即可：

```go
func logger(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    start := time.Now()
    err := next(w, r)   // 原样传递，不替换
    log.Printf("%s %s cost=%s", r.Method, r.URL.Path, time.Since(start))
    return err
}
```

> 自定义 ResponseWriter 若要让默认回调正确判断是否已经写响应，应保留底层写入能力并正确转发所需接口；引擎自身的响应包装器会跟踪写操作。详细规则见 `http-engine-callback.md`。

## 四、短路控制

中间件**不调用 `next()`** 即可短路，后续中间件与 handler 都不会执行：

```go
func authMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    if !authorized(r) {
        w.WriteHeader(http.StatusUnauthorized)
        return nil          // 不调用 next，直接短路
    }
    return next(w, r)
}
```

### 在中间件中获取已解析的 Req

路由命中后引擎会立即绑定请求数据并注入 `ctx`，中间件可通过泛型方法 `BoundReqFromContext` 获取类型安全的 Req，在 `next()` 之前做前置检查：

```go
func authMiddleware(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    req, err := zchttp.BoundReqFromContext[LoginReq](ctx)
    if err != nil {
        // 绑定阶段出错（如 JSON 格式错误），交由 core 层路由到 OnValidationError
        return next(w, r)
    }
    if req.Token == "" {
        w.WriteHeader(http.StatusUnauthorized)
        return nil
    }
    return next(w, r)
}
```

> 此时 Req 已完成数据绑定但尚未做参数校验（nonzero / Validate），校验在 core 层执行。若绑定阶段出错，`BoundReqFromContext` 会返回 `*BindingError`。

### 在中间件中获取 handler 的响应 Res

handler 执行完毕后（`next()` 返回），可通过 `BoundResFromContext` 获取 handler 的返回值，在 `next()` 之后做后置处理：

```go
func responseLogger(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    err := next(w, r)
    // 后置逻辑：获取 handler 的 Res
    res, resErr := zchttp.BoundResFromContext[MyRes](ctx)
    if resErr == nil {
        log.Printf("response: %+v", res)
    }
    return err
}
```

## 五、错误传播

- handler 返回的 `error` 会作为 `next()` 的返回值向上传播。
- 每层中间件都能捕获、包装或拦截该错误。
- 若错误一路传播到最外层仍未被处理，引擎按错误类型分发：`*BindingError`（绑定失败）与 `*ValidationError`（参数校验失败）交由 `OnValidationError`（默认 **HTTP 400**），其余错误交由 `OnError`（默认 **HTTP 500**）。详见 `http-engine-callback.md`。

```go
func recoverError(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    err := next(w, r)
    if err != nil {
        log.Printf("handler error: %v", err)
        // 可选择返回 nil 拦截错误，或继续向上返回 err
    }
    return err
}
```

## 六、panic 恢复

`HttpEngine.ServeHTTP` 内置 `defer recover`，捕获 handler 或中间件中的 panic，交由 `OnPanic` 回调处理（默认 `DefaultPanicHandler`）。中间件无需自行处理 panic，引擎层已覆盖；回调收到最近一次成功传给 `next` 的 w/r：

```go
// 默认 panic 处理：输出 slog.Error + 堆栈，返回 500 错误；
// 若响应已写入（IsResponseWritten）则跳过，避免重复写头；支持 HTML 格式（WantHtml）
DefaultPanicHandler(w, r, recovered)
```

自定义 panic 处理：

```go
engine.OnPanic = func(w http.ResponseWriter, r *http.Request, recovered any) {
    log.Printf("panic: %v", recovered)
    w.WriteHeader(http.StatusInternalServerError)
}
```

`OnPanic` 自身若再次 panic，引擎会进行第二层 recover、记录错误，并在响应尚未写入时兜底写入 HTTP 500；该二次 panic 不会继续传播到 `net/http`。

## 七、注册方式

中间件通过 `Use(...)` 注册，分为全局与分组两级：

```go
r := zchttp.NewRouter()
r.Use(logger, recoverError)         // 全局中间件

api := r.Group("/api", authMiddleware)   // 分组中间件
api.GET("/users", listUsers)
```

关键规则：

- **只对此后注册的路由生效**：注册路由时会将全局中间件与当前分组祖先链的当前状态快照到路由条目；已注册路由不受后续 `Use(...)` 影响。
- 子分组保留父分组引用。先创建子分组、后向父分组追加中间件时，之后注册的子路由仍会继承新增中间件。
- 最终链顺序为 `[全局中间件..., 最外层分组中间件..., 当前分组中间件...]`。

> 注册与快照的细节详见 `routing.md`。

## 八、完整示例

```go
func logger(ctx context.Context, w http.ResponseWriter, r *http.Request, next NextFunc) error {
    start := time.Now()
    err := next(w, r)
    log.Printf("%s %s cost=%s", r.Method, r.URL.Path, time.Since(start))
    return err
}

r := zchttp.NewRouter()
r.Use(logger)
r.GET("/ping", pingHandler)
```
