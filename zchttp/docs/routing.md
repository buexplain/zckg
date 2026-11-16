# 路由注册与匹配

本文说明 `zchttp` 的路由注册、路径规范化、参数路由、匹配优先级与冲突检测。实现细节（基数树结构、插入/匹配算法）见 [internals.md](internals.md) 的“路由基数树”一节；中间件注册见 [middleware.md](middleware.md)。

适合：需要注册路由、分组、路径参数的开发者。

## 一、心智模型

- 每个 HTTP method 一棵基数树；同一节点可同时有静态子节点、参数子节点、通配尾段。
- 匹配优先级：**静态段 > 参数段 > 通配尾段**；前两者失败时回溯。
- 路径在注册与匹配时都先 `normalizePath`：补前导 `/`、去末尾 `/`。
- 路由注册是**一次性**的：所有预计算（handler 反射、Req 元信息、中间件快照、路径参数绑定）发生在注册期；注册后不可并发修改。
- `Any` 原子注册 9 个标准方法：全部预检通过才提交。

## 二、创建与注册

```go
func example() {
    r := zchttp.NewRouter()
    r.GET("/users", listUsers)
    r.POST("/users", createUser)
    r.PUT("/users/{id}", updateUser)
    r.DELETE("/users/{id}", deleteUser)
    r.PATCH("/users/{id}", patchUser)
    r.HEAD("/users", headUsers)
    r.OPTIONS("/users", optionsUsers)
    r.CONNECT("/proxy", connectProxy)
    r.TRACE("/debug", traceHandler)
    r.Any("/webhook", webhook) // 同时注册上述 9 个方法
}
```

`NewRouter` 预初始化 9 个标准方法；`Any` 不包含 `PROPFIND` 等自定义方法。

注册方法尾部可传路由级中间件：

```go
func example() {
    r := zchttp.NewRouter()
    r.GET("/users", listUsers, authMiddleware)
    api := r.Group("/api", groupMiddleware)
    api.GET("/orders", listOrders, auditMiddleware)
    r.Any("/webhook", webhook, signMiddleware)
}
```

路由级中间件只作用于本次注册的路由；`Any` 的 9 个方法共用同一份。

## 三、注册期校验

`buildEntry` 在注册时校验，失败即 panic：

| 约束 | 说明 |
| --- | --- |
| handler 必须是函数 | `nil`、非函数 panic |
| 参数恰好 2 个 | 第 1 个 `context.Context`，第 2 个 struct 或 `*struct` |
| 返回值恰好 2 个 | 第 1 个 struct 或 `*struct`，第 2 个实现 `error` |
| 路由语法 | 参数必须独占段；参数名 `[A-Za-z_][A-Za-z0-9_]*`；同名不允许 |
| 可选/通配位置 | 可选参数与通配尾段必须在路径末尾 |
| 参数名对应字段 | 参数名必须等于 Req 字段的 Form 链绑定名，否则 panic |
| 参数类型 | 仅 string/bool/int/uint/float（含命名标量）、`time.Time` 及指向它们（多级）的指针；slice/map/struct/chan panic |
| 文件字段 | 路径参数不能是 `*multipart.FileHeader` / `[]*multipart.FileHeader` |
| 路由冲突 | 同 method 下重复、参数名不一致、可选性不一致 panic |

`default` 误用只告警，不阻断注册（见 [reference.md](reference.md)）。

## 四、路径规范化

`normalizePath`：

1. 补前导 `/`：`hello` → `/hello`；
2. 非根路径去掉所有末尾 `/`：`/hello/` → `/hello`、`/a///` → `/a`；
3. 空串/纯斜杠 → `/`。

行为：

- `/hello` 与 `/hello/` 视为同一路由；重复注册会冲突。
- 内部多重斜杠不折叠：注册 `//a`、`/a//b` panic（空段无法表达）。
- 请求 `/a//b` 不命中静态/参数路由（参数段不匹配空段）；`/a/{rest...}` 可捕获 `/b`。
- 分组前缀与子路径都先归一化；`api.GET("/hello/")` 注册为 `/api/hello`。

## 五、路径参数

### 1. 声明

```go
type GetCommentReq struct {
    PostID    int `json:"post_id"`
    CommentID int `json:"comment_id" default:"99"`
}

// 注册：r.GET("/posts/{post_id}/comments/{comment_id?}", getComment)
// /posts/1/comments/2 → PostID=1, CommentID=2
// /posts/1/comments   → PostID=1, CommentID=99（省略，保留 default）
```

三种参数：

| 形式 | 含义 |
| --- | --- |
| `{name}` | 必选，必须匹配一个非空段 |
| `{name?}` | 可选，只能位于路径末尾；省略时不写入字段 |
| `{name...}` | 通配尾段，捕获剩余全部路径（可含 `/`，可零段） |

### 2. 绑定与优先级

- 参数名 = 字段 Form 链绑定名（`form` > 普通 `json` > 字段名）。
- 路径参数在 query/body 绑定**成功后**写入，优先级 **path > body > query**；若 query/body 绑定失败，path 不写入。
- 类型转换复用 `setScalar`：支持标量/`time.Time`/多级指针；`time_format`/`time_location` 生效。
- 必选参数转换失败返回 400（`BindingError`），不是 404；段数不匹配才是 404。
- 可选参数省略/通配零段不写入字段，保留模板 `default` 或零值；字段 `nonzero` 且取值为零时校验 400。
- 必选 `{name}` 的 `default` 永远被路径值覆盖，OpenAPI 不展示；可选/通配零段保留 default，OpenAPI 展示。

### 3. 通配尾段

- 捕获值来自 `r.URL.Path`，已百分号解码；不做路径清理（`..`、`.` 原样保留）。
- 末尾 `/` 已归一化；中间多重斜杠不折叠（`/dify//x` 捕获 `/x`）。
- 零段命中（`/dify`、`/dify/`）不写入字段。
- 安全校验由使用者负责：`..`、`%2e%2e` 不会自动清理。静态文件场景优先 `http.ServeFileFS`/`fs.ValidPath`。

## 六、匹配规则

- 静态 > 参数 > 通配；前者分支失败时回溯。
- 静态路由始终优先：`/user` 与 `/user/{name?}` 并存，`/user` 命中静态。
- 参数段不匹配空段、不跨 `/`；跨段用通配。
- 路径耗尽时优先级：该节点终点路由 > 可选参数省略分支 > 通配零段。
- 跨层静态优先：`/a/{x}` 与 `/a/b/{rest...}` 并存，`/a/b` 沿静态 `b` 命中通配零段，而不是 `{x}=b`。
- 同节点允许静态/参数/通配三类子路由并存。

## 七、分组

```go
func example(r *zchttp.Router, auth zchttp.MiddlewareHandler) {
    api := r.Group("/api", auth)
    api.GET("/users", listUsers)   // /api/users
    api.GET("orders", listOrders)  // /api/orders
    api.GET("/", apiIndex)         // /api

    v1 := api.Group("v1")
    v1.GET("users", listUsers)     // /api/v1/users
}
```

- 前缀空串与 `/` 归一化为 `""`；不以 `/` 开头自动补；去末尾 `/`。
- 分组前缀与子路径拼接恰有一个 `/`；子路径先 `normalizePath`。
- 父分组中间件在子分组之前；先创建子分组、后向父分组 `Use`，子路由注册时仍继承新增中间件。

## 八、冲突与并发

- 同 method + 同路径重复注册 panic；错误含双方 handler 文件名/行号。
- 同位置参数名不一致、可选性不一致 panic。
- 路由注册必须在服务对外提供请求前完成；运行期注册属数据竞争（可能 `fatal error` 或状态不一致）。

## 九、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| 路由器/分组/注册 | `router.go` | `router_test.go` |
| 基数树插入/匹配 | `router_trie.go` | `router_trie_test.go` |
| 注册期预计算、路径参数绑定 | `buildEntry.go` | `buildEntry_test.go` |
| 路由级中间件 | `buildEntry.go`、`middleware.go` | `middleware_test.go` |

继续阅读：[request.md](request.md)（Req/Res 与标签）、[parameter-binding.md](parameter-binding.md)（路径参数的值绑定细节）、[internals.md](internals.md)（trie 算法）。
