# zchttp 常见场景

本文件给出可直接套用的配方。每个配方说明“做什么、关键 API、边界与常见坑”，完整契约见 [reference.md](reference.md)。

## 文件上传

```go
type UploadReq struct {
    Title string                  `json:"title"`
    File  *multipart.FileHeader   `json:"file"`
    Files []*multipart.FileHeader `json:"files"`
}

func upload(ctx context.Context, req UploadReq) (UploadRes, error) {
    if req.File == nil {
        return UploadRes{}, nil
    }
    f, err := req.File.Open()
    if err != nil {
        return UploadRes{}, zchttp.NewBindingError(err)
    }
    defer f.Close()
    // 从 f 读取；req.File.Filename / Size 可读
    return UploadRes{Size: req.File.Size}, nil
}

// 注册：engine.Router.POST("/upload", upload)
```

要点：

- 文件字段类型必须是 `*multipart.FileHeader` 或 `[]*multipart.FileHeader`；有 Form 链绑定名才算文件字段。
- multipart 解析受 `MultipartFormMaxMemory`（默认 32 MB）与 `MaxBodyBytes`（默认 32 MB）双重限制；文件超出内存部分落临时盘。
- 没有文件字段的 Req 不会走 multipart；带文件字段的请求按 multipart 解析。
- 文件字段与 `KeepRawBody` 冲突，注册期 panic。

## 反向代理（保留原始请求体）

```go
type ProxyReq struct {
    zchttp.KeepRawBody
    Rest string `json:"rest"`
}

var upstream, _ = url.Parse("http://dify-api:5001")

var proxy = &httputil.ReverseProxy{
    Rewrite: func(pr *httputil.ProxyRequest) {
        pr.SetURL(upstream)
        pr.Out.URL.RawPath = strings.TrimPrefix(pr.In.URL.EscapedPath(), "/dify")
        pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/dify")
    },
}

func proxyHandler(ctx context.Context, req ProxyReq) (ProxyRes, error) {
    if req.Rest != "" && path.Clean("/"+req.Rest) != "/"+req.Rest {
        return ProxyRes{}, zchttp.NewBindingError(errors.New("invalid proxy path"))
    }
    r, _ := zchttp.RequestFromContext(ctx)
    w, _ := zchttp.ResponseWriterFromContext(ctx)
    proxy.ServeHTTP(w, r)
    return ProxyRes{}, nil
}

// 注册：r.POST("/dify/{rest...}", proxyHandler)
```

要点：

- `KeepRawBody` 让引擎不读取 body；路径参数与 query 仍绑定。
- 转发用 `EscapedPath()` 去前缀，保留原始转义；安全校验用已解码的 `Rest`。
- `ReverseProxy`/`Transport` 应全局复用，避免每请求新建连接池。
- 引擎包装器保留 `Flusher`，SSE 不会被攒批。

## 静态文件服务

```go
type StaticReq struct {
    File string `json:"file"`
}

var assets = os.DirFS("./public")

func serveStatic(ctx context.Context, req StaticReq) (StaticRes, error) {
    r, _ := zchttp.RequestFromContext(ctx)
    w, _ := zchttp.ResponseWriterFromContext(ctx)
    name := req.File
    if name == "" {
        name = "index.html"
    }
    http.ServeFileFS(w, r, assets, name)
    return StaticRes{}, nil
}

// 注册：static := r.Group("/static")
//       static.GET("/{file...}", serveStatic)
//       static.HEAD("/{file...}", serveStatic)
```

要点：

- 捕获值已百分号解码且不做路径清理；**禁止**直接 `filepath.Join(root, req.File)`，否则 `..` 可逃逸。
- `http.ServeFileFS` + `os.DirFS`/`embed.FS` 由 `fs.ValidPath` 拒绝 `..` 与非规范路径。
- 响应包装器透传 `io.ReaderFrom`；`*os.File` 在 Linux 可走 sendfile。
- HEAD 需显式注册（路由按方法分树）。

## SSE / 流式响应

```go
func stream(ctx context.Context, req StreamReq) (StreamRes, error) {
    w, _ := zchttp.ResponseWriterFromContext(ctx)
    flusher, ok := w.(http.Flusher)
    if !ok {
        return StreamRes{}, errors.New("streaming unsupported")
    }
    w.Header().Set("Content-Type", "text/event-stream")
    for _, event := range events {
        _, _ = fmt.Fprintf(w, "data: %s\n\n", event)
        flusher.Flush() // 置 written，默认 JSON 回调跳过
    }
    return StreamRes{}, nil
}
```

要点：

- 第一次 `Write`/`Flush` 后 `written=true`，`DefaultResponseHandler` 不再输出 JSON。
- 包装器按底层能力提供 `Flusher`；不支持时类型断言失败。
- 客户端断开后写入会返回错误，按业务决定是否返回。

## 自定义回调

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

要点：

- `OnValidationError` 同时接 `*BindingError` 与 `*ValidationError`；`OnError` 接其余错误。
- 回调收到的 w/r 是最近一次成功传给 `next` 的对象；`OnNotFound` 收到初始包装器与入口 request。
- 默认回调先看 `IsResponseWritten`；自定义回调也应按需判断或自行保证只写一次。

## 自定义 Validator

```go
type CreateEventReq struct {
    Start int `json:"start" nonzero:"true"`
    End   int `json:"end" nonzero:"true"`
}

func (r CreateEventReq) Validate() error {
    if r.Start >= r.End {
        return &zchttp.ValidationError{Field: "end", Message: "must be greater than start"}
    }
    return nil
}
```

要点：

- 只对顶层 Req 自动调用；嵌套 struct 需在顶层 `Validate` 中手动调用。
- 绑定与 `nonzero` 通过后才调用；返回 `*ValidationError` 直接透传，其他 error 自动包装。
- typed-nil 视为无错误；返回 nil 表示通过。
- `Validate` 不参与 OpenAPI 静态推导。

## OpenAPI 挂载

```go
func example() {
    engine := zchttp.NewEngine()

    var doc map[string]any
    engine.Router.GET("/openapi.json", func(ctx context.Context, _ struct{}) (struct{}, error) {
        w, ok := zchttp.ResponseWriterFromContext(ctx)
        if !ok {
            return struct{}{}, errors.New("response writer not found")
        }
        w.Header().Set("Content-Type", "application/json")
        return struct{}{}, json.NewEncoder(w).Encode(doc)
    })

    // 注册完文档路由后再生成，doc 会包含 /openapi.json 自身；只生成一次并缓存。
    doc = zchttp.GenerateOpenAPI(engine.Router, zchttp.OpenAPIInfo{
        Title:   "示例 API",
        Version: "1.0.0",
    })
}
```

要点：

- `GenerateOpenAPI` 返回 `map[string]any`，可直接 JSON 序列化；但**不能把它直接作为 Res 返回**：handler 的第一个返回值必须是 struct 或 `*struct`，否则注册期 panic；默认 `OnResponse` 还会把 Res 包进 `Response{data}`，Swagger UI 拿不到根文档。
- 推荐写法如上：handler 直接写 `ResponseWriterFromContext` 取到的 writer，返回空 struct；默认响应因 `written` 跳过。
- 生成前对 method/path 排序，多次生成字节级一致；生产环境在启动时生成一次并缓存。
- 自定义 `OnResponse` 输出非 JSON 时，自动生成的响应 schema 不再适用。

## Webhook 验签（KeepRawBody）

```go
type WebhookReq struct {
    zchttp.KeepRawBody
    Sig string `json:"sig" nonzero:"true"`
}

func webhook(ctx context.Context, req WebhookReq) (WebhookRes, error) {
    r, _ := zchttp.RequestFromContext(ctx)
    body, err := io.ReadAll(r.Body)
    if err != nil {
        return WebhookRes{}, zchttp.NewBindingError(err)
    }
    mac := hmac.New(sha256.New, secret)
    mac.Write(body)
    want := hex.EncodeToString(mac.Sum(nil))
    if !hmac.Equal([]byte(req.Sig), []byte(want)) {
        return WebhookRes{}, zchttp.NewBindingError(errors.New("invalid signature"))
    }
    return WebhookRes{OK: true}, nil
}

// 注册：r.POST("/webhook", webhook)
```

要点：

- `KeepRawBody` 只识别 Req 顶层值嵌入；`*KeepRawBody`、深层嵌入、具名字段无效。
- query 仍严格校验（存在 Form 链字段时）；路径参数仍绑定。
- 读取体超 `MaxBodyBytes` 会得到 `*http.MaxBytesError`；包装为 `BindingError` 可返回 400。
- 与上传文件字段冲突，注册期 panic。

## 优雅关闭

`zchttp` 只负责 HTTP 引擎；监听/关闭与信号处理交给 `zcquit` 或调用方。典型写法：

```go
func example() {
    engine := zchttp.NewEngine()
    server := &http.Server{Addr: ":8080", Handler: engine}
    go func() {
        if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
            slog.Default().Error("listen failed", "err", err)
        }
    }()
    // 在 zcquit 的清理 handler 中调用 server.Shutdown(ctx)
}
```

要点：`HttpEngine.Run` 只是设置 `server.Handler` 并调用 `ListenAndServe`，不含优雅关闭逻辑；需要时自行管理 `*http.Server`。
