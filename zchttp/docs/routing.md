# 路由注册规则

路由系统实现位于 `router.go` 与 `router_trie.go`，由 `Router`（路由器）与 `RouterGroup`（路由分组）两部分组成。注册阶段通过 `buildEntry`（`buildEntry.go`）校验 handler 签名、快照中间件链、预计算反射信息，运行时无需重复反射解析。

## 一、创建路由器

```go
r := zchttp.NewRouter()
```

`NewRouter` 预初始化以下 HTTP 方法的路由表：`GET`、`POST`、`PUT`、`DELETE`、`PATCH`、`HEAD`、`OPTIONS`、`CONNECT`、`TRACE`。

## 二、注册路由

`Router` 与 `RouterGroup` 均提供对应各 HTTP 方法的注册方法，以及一次注册全部受支持方法的 `Any`：

```go
r.GET("/users", listUsers)
r.POST("/users", createUser)
r.PUT("/users", updateUser)
r.DELETE("/users", deleteUser)
r.PATCH("/users", patchUser)
r.HEAD("/users", headUsers)
r.OPTIONS("/users", optionsUsers)
r.CONNECT("/proxy", connectProxy)
r.TRACE("/debug", traceHandler)
r.Any("/webhook", webhook) // 同时注册上述 9 个方法
```

`Any` 只注册 `NewRouter` 支持的 9 个标准方法，不包含 `PROPFIND` 等自定义方法。注册过程具有原子性：先构建并预检全部 9 个方法，全部通过后才统一写入路由树与顺序索引；handler 校验或任一方法的路由冲突导致 panic 时，不会残留部分方法。

路由匹配基于单一基数树：**静态段 > 参数段 > 通配尾段**，前者分支失败时依次回溯尝试后者（见下文“路由参数”章节），均未命中则走 `OnNotFound`。

### 路径归一化（normalizePath）

注册与匹配都会先经过 `normalizePath` 对路径做两项规范化处理：

1. **补全前导 `/`**：若路径不以 `/` 开头则自动补上（如 `hello` → `/hello`），确保与 `r.URL.Path` 格式一致。
2. **去除末尾 `/`**：非根路径去除末尾的 `/`（如 `/hello/` → `/hello`、`/api/users/` → `/api/users`），使 `/hello` 与 `/hello/` 视为同一路由。

特殊路径处理：

- 根路径 `/` 不会被裁剪为空串；空串也统一归一化为 `/`。
- 连续尾斜杠会被整体去除：`/a///` 归一化为 `/a`；仅由斜杠组成的 `//`、`///` 等路径最终归一化为根路径 `/`。
- **内部多重斜杠不折叠**：`//a`、`/a//b` 等含空段的路径在**注册时**会 panic（基数树按 `/` 逐段切分，空段无法表达）。注册 `/a/b` 后，请求 `/a//b` 仍返回 404（不命中）；通配尾段例外，`/a/{rest...}` 可命中 `/a//b` 并捕获 `/b`（见“通配尾段”）。经反向代理（如 nginx）转发时需注意代理是否会合并多余斜杠。

归一化在两处生效：

- **注册时**（`register`）：`/hello` 与 `/hello/` 归一到同一 key，重复注册会正确触发冲突检测。
- **匹配时**（`ServeHTTP`）：请求 `/hello/` 能命中注册的 `/hello`。

归一化作用于**含分组前缀的完整路径**，如 `api.GET("/hello/")` 最终注册为 `/api/hello`。

## 三、handler 签名约束

所有 handler 必须满足如下签名，否则注册时（`buildEntry` 内联校验）会 **panic**：

```go
func(ctx context.Context, req Req) (Res, error)
```

校验规则：

| 位置 | 约束 |
| --- | --- |
| 参数个数 | 必须恰好 2 个 |
| 第 1 个参数 | 必须是 `context.Context` |
| 第 2 个参数 `Req` | 必须是结构体或结构体指针 |
| 返回值个数 | 必须恰好 2 个 |
| 第 1 个返回值 `Res` | 必须是结构体或结构体指针 |
| 第 2 个返回值 | 必须实现 `error` |
| handler 本身 | 不能为 `nil`，且必须是函数 |

合法示例：

```go
// 值类型
func hello(ctx context.Context, req HelloReq) (HelloRes, error) { ... }

// 指针类型
func hello(ctx context.Context, req *HelloReq) (*HelloRes, error) { ... }
```

> `Req` / `Res` 支持值类型与指针类型；引擎会根据声明类型自动构造并传参。

### 注册阶段预计算

handler 注册时，`buildEntry` 会通过反射一次性预计算以下信息并存入 `routeEntry`：

- **handler 反射值**（`handlerVal`）：`reflect.ValueOf(handler)`，请求时直接 `Call` 调用，避免重复获取。
- **Req/Res 类型信息**（`reqType`/`resType`/`reqElemType`/`reqIsPtr`/`resIsPtr`）：`reqElemType` 是解引用指针后的 Req 具体类型，用于 `reflect.New` 创建实例；`reqIsPtr`/`resIsPtr` 标记 handler 声明的是值还是指针，决定 core 层传入值还是传指针。
- **Req/Res 元信息**（`reqMeta`/`resMeta`，类型 `structMeta`）：缓存 Req/Res 顶层字段的绑定名、`nonzero` 判定、`default` 标签值、`time_format`/`time_location`、文件字段标记等，绑定、校验与 OpenAPI 生成阶段直接使用。嵌套结构体的 meta 不在注册阶段计算，而是在递归校验/默认值填充首次使用时经 `cachedStructMeta` 构建，随后存入进程级缓存（`sync.Map`），后续请求直接复用，无重复反射开销。
- **Req 模板**（`defaultReq`）：创建 Req 实例并通过 `applyDefaults` 初始化 `default` 标签字段（注册阶段：所有零值字段均填充）。请求阶段浅拷贝复用，绑定后再次调用 `applyDefaults(requestPhase=true)` 补填动态创建的子元素（slice/数组/map/nested ptr）中的 nil 指针字段。详见[默认值机制](parameter-binding.md#六默认值机制)。
- **操作级元信息**（`opMeta`，类型 `operationMeta`）：从 Req 嵌入的 `OpenAPIMeta` 中提取 `tags`/`summary`/`description`，供 OpenAPI 文档生成。
- **中间件快照**（`middlewares`）：注册时将当前 `[全局中间件..., 分组中间件...]` 的副本固定到该路由条目，后续 `Use(...)` 不影响已注册路由。
- **深拷贝标记**（`needsDeepCopy`）：若模板中存在非 nil 的指针/切片/map 字段或元素内含引用的数组字段，标记为需深拷贝，请求时对模板浅拷贝后通过 `deepCopyDefaults` 断开共享引用。
- **请求阶段默认值标记**（`needsRequestPhaseDefaults`）：通过 `hasRequestPhaseDefaults` 扫描结构体树，判断是否存在带 `default` 的指针字段。仅当该标记为 `true` 时，请求阶段才执行 `applyDefaults(requestPhase=true)` 补填 nil 指针字段，避免无意义的递归遍历。
- **nonzero 校验标记**（`needsNonzeroValidation`）：通过 `hasNonzeroInTree` 扫描 Req 整棵类型树（穿透嵌套结构体、指针、容器），判断任意深度是否存在 `nonzero:"true"` 字段。仅当该标记为 `true` 时，请求阶段才执行 `validateNonzero` 遍历，全树无 nonzero 字段的接口整体跳过。详见 `parameter-validate.md` 中"零值判定与快速跳过"章节。
- **保留原始请求体标记**（`keepRawBody`）：Req 顶层值嵌入 `zchttp.KeepRawBody` 时为 `true`，请求阶段只绑定 query 与路径参数、不读取请求体；同时含上传文件字段时注册即 panic。详见 `parameter-binding.md` 中“保留原始请求体（KeepRawBody）”章节。
- **handler 位置信息**（`handlerName`/`handlerFile`/`handlerLine`）：通过 `runtime.FuncForPC` 提取全限定函数名与定义位置，用于路由冲突提示与 OpenAPI 操作摘要。

## 四、路由参数（必选/可选/通配尾段）

路径中可用 `{name}` 声明**必选参数**、`{name?}` 声明**可选参数**、`{name...}` 声明**通配尾段**（捕获剩余全部路径，见下文“通配尾段”），参数值在请求阶段自动绑定到 handler 的 `Req` 结构体字段：

```go
type GetCommentReq struct {
    PostID    int    `json:"post_id"`
    CommentID int    `json:"comment_id" default:"99"` // 可选参数省略时的回退值
}

r.GET("/posts/{post_id}/comments/{comment_id?}", getComment)
// GET /posts/1/comments/2  → PostID=1, CommentID=2
// GET /posts/1/comments    → PostID=1, CommentID=99（省略，保留 default）
```

### 语法与注册期校验

| 规则 | 违反时行为 |
| --- | --- |
| 参数必须独占整个 path 段，形如 `{name}` / `{name?}` / `{name...}` | 注册时 panic |
| 参数名仅允许 `[A-Za-z_][A-Za-z0-9_]*`，同一路径内不允许重复；`{a?...}`、`{a...?}`、`{...}` 等后缀组合或空名非法 | 注册时 panic |
| 可选参数与通配尾段必须位于路径末尾，其后不允许再出现任何段 | 注册时 panic |
| 参数名必须与 Req 中某字段的绑定名（form > json > 字段名）精确对应 | 注册时 panic |
| 参数目标字段不允许是上传文件字段（`*multipart.FileHeader` 及其切片） | 注册时 panic |
| 同一 method 下参数模式重复、同位置参数名不一致（含通配名 `{a...}` 与 `{b...}`）、可选性不一致 | 注册时 panic（冲突提示含双方 handler 位置） |

### 匹配规则

- 所有路由（静态段、参数段与通配尾段）统一存储于按 method 划分的基数树（`router_trie.go`），同一节点按**静态段 > 参数段 > 通配尾段**依次尝试，前者分支失败时回溯尝试后者。匹配采用逐段子串扫描（`matchPath`），不预切分整个请求路径，捕获切片延迟到真正命中参数时才分配。
- 可选参数的省略分支与命中分支在插入时一次性展开，匹配时无需反复回溯。
- 静态路由始终优先：`/user`（静态）与 `/user/{name?}`（参数）并存时，请求 `/user` 命中静态路由。
- `{name}` / `{name?}` 不匹配含 `/` 的值（逐段匹配的天然限制），需要跨段捕获时使用 `{name...}`；末尾斜杠归一化同样适用（`/user/` ≡ `/user` 命中省略分支）。

### 通配尾段 {name...}

`{name...}` 捕获匹配点之后的剩余全部路径，用于反向代理、静态文件服务等“按任意深度子路径处理”的场景：

```go
type ProxyReq struct {
    Rest string `json:"rest"`
}

r.GET("/dify/{rest...}", getDify)
// GET /dify/v1/chat-messages → Rest="v1/chat-messages"
// GET /dify/health           → Rest="health"
// GET /dify  或  /dify/      → 零段命中，Rest 保留 default 或零值
```

| 维度 | 规则 |
| --- | --- |
| 捕获值 | 匹配点之后的剩余子路径去掉一个前导 `/`，可含多段与内部 `/` |
| 解码与清理 | 取自 `r.URL.Path`，**已百分号解码**（`%2F`→`/`、`%3F`→`?`、`%2e%2e`→`..`）；**不做路径清理**（`..`、`.` 原样保留），安全校验由使用者负责（见下文使用场景） |
| 斜杠归一化 | 末尾 `/` 全部去除（`/dify/v1/` 捕获 `v1`）；中间多重斜杠不折叠（`/dify//x` 捕获 `/x`） |
| 零段命中 | 路径在通配起点耗尽（`/dify`、`/dify/`）时命中，不写入字段，保留 `default` 或零值；字段带 `nonzero` 时校验失败返回 400 |
| 同节点零段优先级 | 路径在某节点耗尽时：该节点终点路由 > 可选参数省略分支 > 通配零段命中 |
| 跨层优先级 | 静态优先贯穿各层：`/a/{x}` 与 `/a/b/{rest...}` 并存时，请求 `/a/b` 沿静态 `b` 链命中**通配零段**，而非 `/a/{x}`（x=b）；请求 `/a/c` 命中 `/a/{x}` |
| 同级共存 | 同节点允许静态、参数、通配三类子路由并存，用于“个别端点精确接管、其余全转发”（如单独注册 `/dify/v1/files/upload`，其余走 `/dify/{rest...}`） |

> 通配尾段与普通路径参数完全同构：Req 必须有对应绑定名字段（字段强约束），因此带请求体的方法会照常绑定 body。需要把请求体原样留给 handler（如代理转发）时，在 Req 中嵌入 `zchttp.KeepRawBody`，详见 `parameter-binding.md` 中“保留原始请求体（KeepRawBody）”章节。

### 使用场景：反向代理

对 `/dify` 前缀做权限校验，其余任意深度路径连同请求体整条转发上游：

```go
type ProxyReq struct {
	zchttp.KeepRawBody
	Rest string `json:"rest"`
}

type ProxyRes struct{}

var upstream, _ = url.Parse("http://dify-api:5001")

// 全局单例：连接池随 Transport 复用，禁止每请求新建
var proxyTransport = func() *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.MaxIdleConns = 256
	t.MaxIdleConnsPerHost = 128 // 默认 2，单上游高并发时须调大
	return t
}()

// 复用 32 KB 拷贝缓冲
type bufferPool struct{ p sync.Pool }

func (b *bufferPool) Get() []byte {
	if v, ok := b.p.Get().(*[]byte); ok {
		return *v
	}
	return make([]byte, 32<<10)
}

func (b *bufferPool) Put(buf []byte) { b.p.Put(&buf) }

var proxy = &httputil.ReverseProxy{
	Transport:  proxyTransport,
	BufferPool: &bufferPool{},
	Rewrite: func(pr *httputil.ProxyRequest) {
		pr.SetURL(upstream)
		// 转发用原始转义路径去前缀，避免 Rest（已解码）中的 %2F/%3F 语义被改写
		pr.Out.URL.RawPath = strings.TrimPrefix(pr.In.URL.EscapedPath(), "/dify")
		pr.Out.URL.Path = strings.TrimPrefix(pr.In.URL.Path, "/dify")
	},
}

func difyProxy(ctx context.Context, req ProxyReq) (ProxyRes, error) {
	// 校验用 Rest（已解码，%2e%2e 已还原为 ..），拒绝穿越与非规范路径
	if req.Rest != "" && path.Clean("/"+req.Rest) != "/"+req.Rest {
		return ProxyRes{}, zchttp.NewBindingError(errors.New("invalid proxy path"))
	}
	r, _ := zchttp.RequestFromContext(ctx)
	w, _ := zchttp.ResponseWriterFromContext(ctx)
	// 已写入响应：默认回调检测 IsResponseWritten 后不再输出；包装器保留 Flusher，SSE 可用
	proxy.ServeHTTP(w, r)
	return ProxyRes{}, nil
}

g := r.Group("/dify", authMiddleware)
g.Any("/{rest...}", difyProxy)
```

注意事项（框架层无性能瓶颈，转发性能取决于 `ReverseProxy` 配置）：

1. **连接池**：`http.DefaultTransport` 的 `MaxIdleConnsPerHost` 默认为 2，高并发转发单一上游时连接频繁新建关闭，须使用自定义 `Transport` 调大 `MaxIdleConns` 与 `MaxIdleConnsPerHost`。
2. **实例复用**：`ReverseProxy` 与 `Transport` 必须为全局单例，禁止每请求新建（否则连接池失效）；设置 `BufferPool` 避免每次拷贝响应体时分配 32 KB 缓冲。
3. **流式响应**：上游响应为 `text/event-stream` 时 `ReverseProxy` 自动立即刷新；引擎包装器按底层能力保留 `http.Flusher`，SSE 不会被攒批。
4. **请求体上限**：`MaxBodyBytes`（默认 32 MB）对代理请求体同样生效，代理大文件需调大或设为 0。
5. **路径**：转发路径用 `EscapedPath()` 去前缀，保留客户端的原始转义；安全校验用已解码的捕获值。

### 使用场景：静态文件服务

`/static/{file...}` 按任意深度子路径提供文件，且可复用框架中间件能力（鉴权下载、审计日志）：

```go
type StaticReq struct {
	File string `json:"file"`
}

type StaticRes struct{}

var assets = os.DirFS("./public") // 或 embed.FS（无 sendfile、无 Last-Modified，需自设缓存头）

func serveStatic(ctx context.Context, req StaticReq) (StaticRes, error) {
	r, _ := zchttp.RequestFromContext(ctx)
	w, _ := zchttp.ResponseWriterFromContext(ctx)
	name := req.File
	if name == "" {
		name = "index.html"
	}
	// name 必须为相对路径（无前导 /）：fs.ValidPath 拒绝 .. 与非规范路径，
	// ServeFileFS 另对 r.URL.Path 中的 .. 拒绝，两道检查叠加
	http.ServeFileFS(w, r, assets, name)
	return StaticRes{}, nil
}

static := r.Group("/static") // 不挂重型中间件
static.GET("/{file...}", serveStatic)
static.HEAD("/{file...}", serveStatic)
```

**路径穿越（比性能更关键）**：捕获值为解码后原样、不清理，`%2e%2e` 会还原为 `..`。**禁止** `os.Open(filepath.Join(root, req.File))`——`filepath.Join("/srv", "../etc/passwd")` 结果为 `/etc/passwd`，直接逃逸根目录。安全做法三选一：

- `http.ServeFileFS` / `http.FileServerFS` + `os.DirFS` 或 `embed.FS`（`fs.ValidPath` 拒绝 `..`）；
- `http.Dir`（`Open` 内部先 `path.Clean`）；
- Go 1.24+ 的 `os.Root`（限定在根目录内打开）。

其他注意事项：

1. **零拷贝与条件请求**：响应包装器透传底层 `io.ReaderFrom`，`ServeContent` / `ServeFileFS` 以 `*os.File` 为源时在 Linux 上走 sendfile；handler 把引擎原始 `r` 交给标准库，304 / 206（Range）/ HEAD 均由标准库处理。
2. **全局中间件放大**：一次页面加载可能触发数十个静态资源请求，全局挂载的日志、鉴权、追踪中间件开销远大于发文件本身，静态路由应注册到不挂重型中间件的分组。
3. **无需框架能力时不经引擎**：纯公开静态资源最快的方式是外层 `http.ServeMux` 直挂 `http.FileServerFS`，其余请求交给 `HttpEngine`（实现 `http.Handler`）。
4. **缓存头**：`embed.FS` 文件 modtime 为零，`ServeContent` 不输出 `Last-Modified`，无法返回 304，需自行设置 `ETag` 或 `Cache-Control`（文件名带内容哈希时可设长缓存）。
5. **HEAD 需显式注册**：路由按方法分树，未注册 HEAD 则 HEAD 请求走 404。

### 参数绑定与错误语义

- 参数值在 query/body 绑定之后写入 Req，**路径参数覆盖同名 query/body 值**。绑定实现（`bindPathParams`）复用 `setScalar` 类型转换，支持 string/bool/int 全系/uint 全系/float/指针及 `time_format`/`time_location` 标签。详见 `parameter-binding.md` 中“路由路径参数”章节。
- 必选参数类型转换失败（如 int 字段收到 `/posts/abc/comments`）返回 **400**（`BindingError` 通道），而非 404；段数不匹配才返回 404。
- 可选参数被省略、通配尾段零段命中时不写入字段，保留注册阶段模板的 `default` 值或零值。
- 必选参数字段上的 `default` 标签无害但无效（路径值必然覆盖）。

## 五、路由冲突检测

同一 method + path 重复注册会立即 **panic**，错误信息包含冲突双方的函数名、文件路径与行号：

```
route conflict: GET /users already registered by main.listUsers (/app/main.go:20),
conflicting with main.listUsersV2 (/app/main.go:35)
```

位置信息在 `buildEntry` 中通过 `runtime.FuncForPC` 内联提取。

## 六、路由分组（RouterGroup）

### 创建分组

```go
api := r.Group("/api", authMiddleware)
api.GET("/users", listUsers)   // 实际路径 /api/users
api.GET("orders", listOrders)  // 同样自动拼接为 /api/orders
api.GET("/", apiIndex)         // 根子路径注册为 /api
```

分组会为组内路由自动拼接 `prefix` 前缀，并叠加分组中间件。分组前缀和子路径均可省略前导 `/`；拼接时会保证前缀与子路径之间恰有一个 `/`，不会出现 `/apiusers` 这类静默错路由。

### 嵌套分组

分组可嵌套，前缀逐层拼接，父分组中间件被继承且位于子分组中间件**之前**：

```go
api := r.Group("api", mwA)
v1 := api.Group("v1", mwB)
v1.GET("users", listUsers)    // 路径 /api/v1/users，中间件顺序 [全局..., mwA, mwB]
```

### 前缀规范化（normalizePrefix）

- 空串 `""` 与 `"/"` 归一化为 `""`
- 不以 `/` 开头时自动补 `/`
- 去除末尾的 `/`

例如 `"users/"` → `"/users"`，`"/api/"` → `"/api"`。子路径还会先经过 `normalizePath`：`"users"` 与 `"/users"` 等价，`""` 与 `"/"` 都表示当前分组根路径。

## 七、中间件注册与快照

- `Use(...)` 向 `Router`（全局）或 `RouterGroup`（分组）追加中间件，返回自身支持链式调用。
- **中间件只对此后注册的路由生效**：注册路由时才读取全局中间件以及当前分组的完整祖先链，并将结果快照到该路由的 `routeEntry.middlewares`。
- 子分组保留父分组引用。即使先创建子分组、后向父分组调用 `Use(...)`，只要子路由尚未注册，注册时仍会继承父分组新增的中间件。
- 已注册路由持有独立快照，之后对全局、父分组或当前分组调用 `Use(...)` 都不会追溯修改它。
- 最终中间件链顺序为 `[全局中间件..., 最外层分组中间件..., 当前分组中间件...]`。

```go
r.Use(logger)
api := r.Group("/api")
v1 := api.Group("/v1")

v1.GET("/before", handlerA) // 应用 [logger]
api.Use(auth)                // 子组已创建，但 /after 尚未注册
v1.GET("/after", handlerB)  // 应用 [logger, auth]；/before 不受影响
```

> 中间件的执行模型（洋葱模型）详见 `middleware.md`。

## 八、并发约束

路由基数树（`Router.trees`）与顺序索引（`Router.routes`）不支持动态注册后并发读取：

- **必须在服务对外提供请求之前完成所有路由注册**（典型用法：启动时注册完再 `ListenAndServe`）。
- 服务运行期间**不支持动态注册路由**。若在服务运行中调用 `GET`/`POST` 等注册方法，会触发 Go map 并发读写导致进程崩溃（`fatal error: concurrent map read and map write`）。
