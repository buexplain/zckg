# 引擎回调、响应与错误

本文说明 `HttpEngine` 的回调、默认响应、错误分发、panic 恢复、“是否已写入”的判定，以及请求全链路。实现细节（responseWriter 状态机、错误路由）见 [internals.md](internals.md) 的“响应与错误”一节。

适合：需要自定义响应/错误/校验失败/panic 处理，或排查状态码/响应体问题的开发者。

## 一、回调类型与字段

```go
type ResponseHandler func(w http.ResponseWriter, r *http.Request, res any)
type ErrorHandler func(w http.ResponseWriter, r *http.Request, err error)
type PanicHandler func(w http.ResponseWriter, r *http.Request, recovered any)

type HttpEngine struct {
    Router            *Router
    OnNotFound        func(w http.ResponseWriter, r *http.Request)
    OnResponse        ResponseHandler
    OnError           ErrorHandler
    OnValidationError ErrorHandler
    OnPanic           PanicHandler

    MaxBodyBytes           int64 // 默认 32 MB；0 表示框架不额外限制
    MultipartFormMaxMemory int64 // 默认 32 MB
}
```

`HttpEngine` 必须用 `NewEngine()` 构造：默认回调、`MaxBodyBytes`、`MultipartFormMaxMemory` 都已装配。`ServeHTTP` 不对回调做 nil 判断；字面量构造会在请求期 panic。

`Run(server)` 只是设置 `server.Handler = e` 并 `ListenAndServe`，不含优雅关闭。

## 二、默认回调

| 回调 | 触发 | 默认行为 |
| --- | --- | --- |
| `DefaultResponseHandler` | handler 成功且未写响应 | JSON `{data:res, code:0, message:"success"}`；res 无法编码（func/chan）→ 500 text/plain `internal server error`；写出失败只记日志 |
| `DefaultErrorHandler` | 其他 error | 500；详情只写日志，客户端只见通用消息 |
| `DefaultValidationErrorHandler` | BindingError/ValidationError | 400；回显 `err.Error()` |
| `DefaultNotFoundHandler` | 路由未命中 | 404 |
| `DefaultPanicHandler` | recover 到 panic | 记录 panic + stack；未写响应才 500 |

所有默认回调：

- 先检查 `IsResponseWritten(w)`，已写则跳过；
- 按 `WantHtml(r)` 选择 HTML 或 JSON；
- 日志带 `zckg=zchttp`（业务自定义日志不需要）；
- 默认 JSON 由池化 `json.Encoder` 生成，关闭 HTML 转义。

`WantHtml`：

```go
func WantHtml(r *http.Request) bool {
    accept := r.Header.Get("Accept")
    return strings.Contains(accept, "text/html") || strings.Contains(accept, "text/plain")
}
```

`Accept` 包含 `text/html` 或 `text/plain` 即返回 HTML/纯文本格式。

## 三、错误分发

`ServeHTTP` 最外层拿到 error 后：

```text
errors.As(*BindingError) || errors.As(*ValidationError)
    → OnValidationError（默认 400）
否则
    → OnError（默认 500）
```

- 绑定失败（JSON 语法、query 转换、multipart 解析、path 转换）→ `*BindingError`。
- 校验失败（nonzero/Validate）→ `*ValidationError`。
- handler/中间件其他 error → `OnError`。
- 回调收到的 w/r 是最近一次成功传给 `next` 的对象；`OnNotFound` 收到初始包装 writer 与入口 request。

`OnValidationError` 有意回显字段名、类型名、路径参数原始值等请求侧信息，便于客户端修正；敏感场景自定义脱敏。

## 四、panic 恢复

`ServeHTTP` 入口安装 `defer recover`，覆盖：

- 路由匹配、绑定、路径参数、默认值；
- 中间件链、core 校验、handler；
- `OnResponse`/`OnError`/`OnValidationError`/`OnNotFound`。

`OnPanic` 收到最近一次成功传给 `next` 的 w/r；进入中间件链前 panic 用初始 writer/request。默认处理器记录 method/path/panic/stack，未写响应才 500。

`OnPanic` 自身再 panic：`invokePanicHandler` 第二层 recover 记录，响应未写时兜底 `WriteHeader(500)`，不递归调用 `OnPanic`，也不会传播到 `net/http`。

## 五、written 判定

```go
func IsResponseWritten(w http.ResponseWriter) bool {
    rw, ok := w.(interface{ Written() bool })
    return ok && rw.Written()
}
```

`written` 由以下操作置位：

- `WriteHeader` 的最终状态码（含 101）；
- `Write`（0 字节也置位）；
- `ReadFrom`（`io.Copy(w, file)` 零拷贝路径）；
- `Flush`；
- `Hijack`。

不置位：

- 仅 `Header().Set(...)`；
- 除 101 外的 1xx（如 103 Early Hints）；
- `Push`（HTTP/2 server push，独立流）。

其他规则：

- 最终 `WriteHeader` 幂等：首次生效，重复被包装层挡下，避免 `superfluous response.WriteHeader call`；
- 101 视为最终响应，置 written，后续状态码/默认回调被挡；
- 包装层实现 `Unwrap() http.ResponseWriter`，`http.NewResponseController` 的 deadline/full-duplex 可穿透；直接写 `Unwrap()` 的底层 writer 会绕过 written 跟踪；
- 自定义 ResponseWriter 要保留 `Written()` 转发，否则默认回调无法判断。

## 六、全链路

1. `MaxBodyBytes > 0` 时包裹 body；取带 written 跟踪的 writer；注册 body 排空/关闭；
2. 安装 recover；
3. `normalizePath` + 路由匹配；未命中 → `OnNotFound`；
4. 构造 `requestState` 注入 ctx；
5. `reflect.New` + 模板浅拷贝 + 按需深拷贝；
6. `bindRequestData`（query/body）；仅在成功时 `bindPathParams`；请求阶段默认值仍执行；
7. 绑定错误存 `bindingErr`，Req 仍注入 ctx；
8. 中间件链；core：绑定错误 → `validateNonzero` → `validateCustom` → handler；
9. 成功：`st.res`，`OnResponse`；错误：按类型分发；
10. 所有请求结束后排空最多 2 KB 并 `Close`。

## 七、Context 资源

handler 签名不暴露 request/writer/engine，通过以下方法获取：

```go
func RequestFromContext(ctx context.Context) (*http.Request, bool)
func ResponseWriterFromContext(ctx context.Context) (http.ResponseWriter, bool)
func EngineFromContext(ctx context.Context) (*HttpEngine, bool)
func BoundReqFromContext[T any](ctx context.Context) (T, error)
func BoundResFromContext[T any](ctx context.Context) (T, error)
```

| 方法 | 说明 |
| --- | --- |
| `RequestFromContext` | 最近一次传给 `next` 的 request |
| `ResponseWriterFromContext` | 最近一次传给 `next` 的 writer |
| `EngineFromContext` | 当前 `*HttpEngine` |
| `BoundReqFromContext[T]` | 进入中间件链前已绑定的 Req；绑定失败返回 `*BindingError`；不存在/类型不符返回 `ErrBoundReqNotFound`；`T` 值类型为浅拷贝，`*Req` 为同一实例 |
| `BoundResFromContext[T]` | handler 的 Res，仅在 `next()` 返回后可用；不存在/类型不符返回 `ErrBoundResNotFound` |

## 八、自定义示例

```go
func example() {
    engine := zchttp.NewEngine()

    engine.OnResponse = func(w http.ResponseWriter, r *http.Request, res any) {
        w.Header().Set("Content-Type", "application/json")
        _ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": res})
    }

    engine.OnValidationError = func(w http.ResponseWriter, r *http.Request, err error) {
        w.Header().Set("Content-Type", "application/json")
        w.WriteHeader(http.StatusBadRequest)
        _ = json.NewEncoder(w).Encode(map[string]any{"code": 1001, "msg": err.Error()})
    }

    engine.OnError = func(w http.ResponseWriter, r *http.Request, err error) {
        slog.Default().Error("request failed", "err", err)
        w.WriteHeader(http.StatusInternalServerError)
    }
}
```

## 九、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| 默认回调、错误分发、panic | `httpEngine.go` | `httpEngine_test.go` |
| written、1xx/101/Push/Unwrap | `responseWriter.go` | `responseWriter_test.go` |
| BindingError/ValidationError | `errors.go` | `errors_test.go` |
| requestState、Context 访问器 | `context.go` | `context_test.go` |

继续阅读：[reference.md](reference.md)（错误/状态码/限制）、[internals.md](internals.md)（responseWriter/回调实现）、[middleware.md](middleware.md)（w/r 替换）。
