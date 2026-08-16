# HttpEngine 回调机制（响应与错误处理）

`HttpEngine` 通过回调函数支持自定义响应、错误、参数校验失败、panic 恢复与未命中路由处理，默认提供统一 JSON 响应、HTTP 500 错误处理、HTTP 400 校验失败处理、panic 日志恢复与 404 处理。相关实现位于 `httpEngine.go`。

## 一、回调类型

```go
// 成功响应回调，res 为 handler 的第一个返回值
type ResponseHandler func(w http.ResponseWriter, r *http.Request, res any)

// 错误响应回调，err 为中间件链或 handler 产生的错误
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)

// panic 恢复回调，recovered 为 recover() 捕获的值
type PanicHandler func(w http.ResponseWriter, r *http.Request, recovered any)
```

`HttpEngine` 结构体的回调字段：

```go
type HttpEngine struct {
    Router            *Router
    OnNotFound        func(w http.ResponseWriter, r *http.Request) // 未命中路由回调，默认 DefaultNotFoundHandler
    OnResponse        ResponseHandler                              // 成功响应回调，默认 DefaultResponseHandler
    OnError           ErrorHandler                                 // 错误响应回调，默认 DefaultErrorHandler
    OnValidationError ErrorHandler                                 // 参数校验/绑定失败回调，默认 DefaultValidationErrorHandler
    OnPanic           PanicHandler                                 // panic 恢复回调，默认 DefaultPanicHandler
    MaxBodyBytes           int64 // 请求体整体大小上限（字节），0 表示不限制；NewEngine 默认 32 MB
    MultipartFormMaxMemory int64 // multipart/form-data 解析的内存缓冲上限（字节），超出部分写入临时文件；NewEngine 默认 32 MB
}
```

`HttpEngine` 必须通过 `NewEngine()` 构造，它会自动装配全部默认回调，并将 `MaxBodyBytes` 与 `MultipartFormMaxMemory` 均默认置为 **32 MB**。不允许直接使用 `HttpEngine{}` 字面量构造，因此 `ServeHTTP` 不再对回调做 nil 判断。

启动监听可使用 `Run` 方法：

```go
// 将引擎设置为 server 的 Handler 并调用 ListenAndServe 启动监听；
// 不包含优雅关闭逻辑，需要时由调用方自行管理所传入的 *http.Server（如 Shutdown）
func (e *HttpEngine) Run(server *http.Server) error
```

## 二、统一 JSON 结构与默认回调

默认回调使用统一的 JSON 响应结构：

```go
type Response struct {
    Data    any    `json:"data"`
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

| 回调 | 行为 |
| --- | --- |
| `DefaultResponseHandler` | 若响应尚未被写入，以 `{data: res, code: 0, message: "success"}` 的统一 JSON 结构返回；若 handler 已写入响应（如文件下载），则跳过 |
| `DefaultErrorHandler` | 若响应尚未被写入，根据 `WantHtml(r)` 决定返回 HTML 或统一 JSON 结构（`{data: null, code: 500, message: "internal server error"}`），状态码统一为 500；err 详情仅写入服务端日志（`slog.Error`），不向客户端暴露内部信息；若已写入则跳过 |
| `DefaultValidationErrorHandler` | 参数校验失败或绑定失败时调用。若响应尚未被写入，根据 `WantHtml(r)` 决定返回 HTML 或统一 JSON 结构（`{data: null, code: 400, message: err}`），状态码统一为 400；若已写入则跳过 |
| `DefaultNotFoundHandler` | 未命中路由时调用。若响应尚未被写入，根据 `WantHtml(r)` 决定返回 HTML 或统一 JSON 结构（`{data: null, code: 404, message: "not found"}`），状态码统一为 404；若已写入则跳过 |
| `DefaultPanicHandler` | 捕获 panic 时调用。通过 `slog.Error` 将 panic 值与完整堆栈输出到服务端日志。若响应尚未被写入，根据 `WantHtml(r)` 决定返回 HTML 或统一 JSON 结构，两者均只含通用消息 `internal server error`（不向客户端暴露堆栈，防泄漏服务端内部结构），状态码统一为 500；若已写入则跳过 |

默认 JSON 响应由进程级池化的 `json.Encoder` 编码（关闭 HTML 转义）：`res` 中的 `<`、`>`、`&` 字符**原样输出**，不再转义为 `\u003c`、`\u003e`、`\u0026`。若业务上需要转义后的输出，请自行替换 `OnResponse` 回调。

### WantHtml

```go
// 判断请求头 Accept 是否包含 text/html 或 text/plain
func WantHtml(r *http.Request) bool
```

`DefaultErrorHandler`、`DefaultValidationErrorHandler`、`DefaultNotFoundHandler` 与 `DefaultPanicHandler` 均依据它选择响应格式：

- `WantHtml` 为 true（浏览器等）：返回 `text/html; charset=utf-8` 的简单错误页面。
- 否则：返回统一 JSON 结构。

## 三、错误分发：绑定失败、校验失败与普通错误

中间件链或 handler 产生的 error 传播到最外层后，`ServeHTTP` 通过 `errors.As` 区分错误类型：

- 若命中 `*BindingError`（绑定失败，如 JSON 格式错误）→ 交由 `OnValidationError`（默认 400）。
- 若命中 `*ValidationError`（参数校验失败，来自 `nonzero` 非零值校验或 `Validate()` 业务校验）→ 交由 `OnValidationError`（默认 400）。
- 其余错误 → 交由 `OnError`（默认 500，客户端仅收到通用 `"internal server error"` 消息，err 详情写入服务端日志）。

> `OnValidationError` 有意回显校验细节（字段名、类型名、路径参数原始值）以便调用方修正请求，且信息仅来自请求侧、敏感度低；如需脱敏请自定义该回调。

> `*BindingError` 的定义及参数绑定细节详见 `parameter-binding.md`；`*ValidationError` 的定义及参数校验规则详见 `parameter-validate.md`。

## 四、panic 恢复

`ServeHTTP` 入口处通过 `defer recover` 捕获整个请求生命周期（含中间件链与 handler）中的 panic，交由 `OnPanic` 回调处理。默认 `DefaultPanicHandler` 在 `slog.Error` 中输出请求方法与路径、panic 值及调用栈，随后向客户端返回 500。

```go
func DefaultPanicHandler(w http.ResponseWriter, r *http.Request, recovered any) {
    stack := debug.Stack()
    slog.Error("panic recovered",
        "method", r.Method,
        "path", r.URL.Path,
        "error", recovered,
        "stack", string(stack),
    )
    // 若响应未写入，返回 500
    ...
}
```

## 五、"是否已写入"的判定

引擎用 `responseWriter` 包装原始 `http.ResponseWriter`，记录 `WriteHeader` / `Write` / `ReadFrom` / `Flush` / `Hijack` 是否被调用过（`Written()`；`ReadFrom` 为 `io.Copy(w, file)` 等零拷贝写入路径）。`Push` 推送的是独立流（HTTP/2 server push），**不影响本响应的 written 状态**——即使中间件调用过 `Push`，后续仍可按正常流程写入响应。`IsResponseWritten` 通过接口断言判定：

```go
func IsResponseWritten(w http.ResponseWriter) bool {
    rw, ok := w.(interface{ Written() bool })
    return ok && rw.Written()
}
```

- 判定依据是是否真正写入过响应体或状态码，而非仅仅设置了 Header。
- 这样，仅设置 Header 的中间件（如 CORS）不会导致 JSON 响应被误跳过；只有真正写了响应（如文件流）才跳过。

## 六、自定义响应示例

### 文件下载（handler 内直接写响应）

handler 或中间件中通过 `ResponseWriterFromContext` 获取 `w`，直接写入文件内容并设置头，默认回调会自动跳过 JSON：

```go
func download(ctx context.Context, req Req) (Res, error) {
    w, ok := zchttp.ResponseWriterFromContext(ctx)
    if !ok {
        return Res{}, errors.New("no response writer")
    }
    w.Header().Set("Content-Type", "application/octet-stream")
    w.Header().Set("Content-Disposition", `attachment; filename="a.txt"`)
    _, _ = w.Write(fileBytes)
    // 已写入响应，默认响应回调将跳过 JSON
    return Res{}, nil
}
```

### 统一响应包装

```go
engine := zchttp.NewEngine()
engine.OnResponse = func(w http.ResponseWriter, r *http.Request, res any) {
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(map[string]any{
        "code": 0,
        "data": res,
    })
}
```

### 自定义校验失败响应（如返回业务错误码）

```go
engine.OnValidationError = func(w http.ResponseWriter, r *http.Request, err error) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "code": 1001,
        "msg":  err.Error(),
    })
}
```

### 自定义错误响应

```go
engine.OnError = func(w http.ResponseWriter, r *http.Request, err error) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusBadRequest)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "code": 1,
        "msg":  err.Error(),
    })
}
```

### 自定义 panic 响应

```go
engine.OnPanic = func(w http.ResponseWriter, r *http.Request, recovered any) {
    // 自定义 panic 告警（如发送到监控系统）
    alertSystem.Send("panic", recovered)
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusInternalServerError)
    _ = json.NewEncoder(w).Encode(map[string]any{
        "code": 500,
        "msg":  "internal server error",
    })
}
```

## 七、执行流程（全链路）

1. **请求体限制**：`MaxBodyBytes > 0` 时以 `http.MaxBytesReader` 包裹 `r.Body`（`NewEngine` 默认 **32 MB**，置 0 不限制），超限在绑定阶段失败并映射为 400（详见 `parameter-binding.md`）。
2. **路由匹配**：`ServeHTTP` 按 method → normalizePath 查找路由条目，未命中 → `OnNotFound`。
3. **panic 保护**：`defer recover` 包裹全流程，捕获 panic → `OnPanic`（`slog.Error` + 堆栈）。
4. **绑定请求数据**：`reflect.New` 浅拷贝预计算模板（含默认值），若 `needsDeepCopy` 则深拷贝引用字段，随后 `bindRequestData` 绑定 query/body（POST 等带 body 方法为先 query 后 body 的合并绑定）；参数路由再由 `bindPathParams` 绑定路径参数（覆盖同名 query/body 值）；若注册期预计算 `needsRequestPhaseDefaults` 为 true，执行 `applyDefaults(requestPhase=true)` 为绑定后动态创建的子元素（切片/数组/map/nested ptr）补填 nil 指针的默认值；绑定错误（`*BindingError`，由 `NewBindingError` 包装底层错误构造）随 Req 注入 ctx。
5. **中间件链执行**：洋葱模型从外到内执行中间件（`runChain`：池化执行对象 + 位图按层防重，超过 64 层回退递归实现）；各中间件可通过 `BoundReqFromContext[T]` 获取已绑定的 Req。
6. **core 层校验**（最内层）：`validateRequest` 依次执行 `validateNonzero` + `validateCustom(Validate)` → 校验失败产生 `*ValidationError`。
7. **反射调用 handler**：校验通过后 `entry.handlerVal.Call` 调用 handler，成功 → `OnResponse(w, r, res)`，Res 同时注入 ctx 供后置中间件通过 `BoundResFromContext[T]` 获取。
8. **错误分发**：任一环节返回 error，按类型分发：
    - `*BindingError` / `*ValidationError` → `OnValidationError`（默认 400）
    - 其余 → `OnError`（默认 500）
9. 各回调收到的 `w` 均为带 `Written()` 追踪能力的包装对象，已写入则跳过默认响应。
10. **请求收尾**：请求处理结束后限量排空剩余未读请求体（上限 `maxBodyDrainBytes` = 2 KB）以复用 keep-alive 连接；超出上限不再排空，连接由 net/http 关闭。

## 八、在 handler 与中间件中获取上下文资源

handler 的签名固定为 `func(ctx context.Context, req Req) (Res, error)`，不直接暴露 `*http.Request`、`http.ResponseWriter` 与 `*HttpEngine`。引擎在 `ServeHTTP` 中已将三者注入 `ctx`，可通过以下方法获取：

```go
func RequestFromContext(ctx context.Context) (*http.Request, bool)
func ResponseWriterFromContext(ctx context.Context) (http.ResponseWriter, bool)
func EngineFromContext(ctx context.Context) (*HttpEngine, bool)
func BoundReqFromContext[T any](ctx context.Context) (T, error)
func BoundResFromContext[T any](ctx context.Context) (T, error)
```

| 方法 | 用途 |
| --- | --- |
| `RequestFromContext` | 获取 `*http.Request` |
| `ResponseWriterFromContext` | 获取包装后的 `http.ResponseWriter`（带 `Written()` 追踪） |
| `EngineFromContext` | 获取 `*HttpEngine` |
| `BoundReqFromContext[T]` | 获取已绑定的 Req（绑定阶段出错则 err 为非 nil 的 `*BindingError`；ctx 中不存在 Req 时返回 `ErrBoundReqNotFound`） |
| `BoundResFromContext[T]` | 获取 handler 的 Res（仅在 `next()` 返回后可用；ctx 中不存在或类型不符时返回 `ErrBoundResNotFound`） |

> 注入的 `ResponseWriter` 是带 `Written()` 追踪能力的包装对象。因此 handler 内若直接通过它写响应（如文件流），默认响应回调会自动跳过 JSON 编码。
