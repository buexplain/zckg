# 路由注册规则

路由系统实现位于 `router.go`，由 `Router`（路由器）与 `RouterGroup`（路由分组）两部分组成。注册阶段通过 `buildEntry`（`buildEntry.go`）校验 handler 签名、快照中间件链、预计算反射信息，运行时无需重复反射解析。

## 一、创建路由器

```go
r := zchttp.NewRouter()
```

`NewRouter` 预初始化以下 HTTP 方法的路由表：`GET`、`POST`、`PUT`、`DELETE`、`PATCH`、`HEAD`、`OPTIONS`、`CONNECT`、`TRACE`。

## 二、注册路由

`Router` 与 `RouterGroup` 均提供对应各 HTTP 方法的注册方法：

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
```

路由匹配为**精确匹配**：请求先按 method 查表，再按归一化后的 `r.URL.Path` 精确查找，未命中则走 `OnNotFound`。

### 路径归一化（normalizePath）

注册与匹配都会先经过 `normalizePath` 对路径做两项规范化处理：

1. **补全前导 `/`**：若路径不以 `/` 开头则自动补上（如 `hello` → `/hello`），确保与 `r.URL.Path` 格式一致。
2. **去除末尾 `/`**：非根路径去除末尾的 `/`（如 `/hello/` → `/hello`、`/api/users/` → `/api/users`），使 `/hello` 与 `/hello/` 视为同一路由。

特殊路径处理：

- 根路径 `/` 不会被裁剪为空串；空串也统一归一化为 `/`。
- **多重斜杠不折叠**：`//a` 与 `/a` 是两个不同的路由。注册 `/a` 后，请求 `//a` 会返回 404；反之亦然。经反向代理（如 nginx）转发时需注意代理是否会合并多余斜杠。

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
- **Req/Res 元信息**（`reqMeta`/`resMeta`，类型 `structMeta`）：缓存 Req/Res 顶层字段的绑定名、`nonzero` 判定、`default` 标签值、`time_format`/`time_location`、文件字段标记等，绑定、校验与 OpenAPI 生成阶段直接使用。嵌套结构体的 meta 不在注册阶段计算，由递归校验按需现场构建。
- **Req 模板**（`defaultReq`）：创建 Req 实例并通过 `applyDefaults` 初始化 `default` 标签字段（注册阶段：所有零值字段均填充）。请求阶段浅拷贝复用，绑定后再次调用 `applyDefaults(requestPhase=true)` 补填动态创建的子元素（slice/数组/map/nested ptr）中的 nil 指针字段。详见[默认值机制](parameter-binding.md#六默认值机制)。
- **操作级元信息**（`opMeta`，类型 `operationMeta`）：从 Req 嵌入的 `OpenAPIMeta` 中提取 `tags`/`summary`/`description`，供 OpenAPI 文档生成。
- **中间件快照**（`middlewares`）：注册时将当前 `[全局中间件..., 分组中间件...]` 的副本固定到该路由条目，后续 `Use(...)` 不影响已注册路由。
- **深拷贝标记**（`needsDeepCopy`）：若模板中存在非 nil 的指针/切片/map 字段或元素内含引用的数组字段，标记为需深拷贝，请求时对模板浅拷贝后通过 `deepCopyDefaults` 断开共享引用。
- **请求阶段默认值标记**（`needsRequestPhaseDefaults`）：通过 `hasRequestPhaseDefaults` 扫描结构体树，判断是否存在带 `default` 的指针字段。仅当该标记为 `true` 时，请求阶段才执行 `applyDefaults(requestPhase=true)` 补填 nil 指针字段，避免无意义的递归遍历。
- **nonzero 校验标记**（`needsNonzeroValidation`）：通过 `hasNonzeroInTree` 扫描 Req 整棵类型树（穿透嵌套结构体、指针、容器），判断任意深度是否存在 `nonzero:"true"` 字段。仅当该标记为 `true` 时，请求阶段才执行 `validateNonzero` 遍历，全树无 nonzero 字段的接口整体跳过。详见 `parameter-validate.md` 中"零值判定与快速跳过"章节。
- **handler 位置信息**（`handlerName`/`handlerFile`/`handlerLine`）：通过 `runtime.FuncForPC` 提取全限定函数名与定义位置，用于路由冲突提示与 OpenAPI 操作摘要。

## 四、路由冲突检测

同一 method + path 重复注册会立即 **panic**，错误信息包含冲突双方的函数名、文件路径与行号：

```
route conflict: GET /users already registered by main.listUsers (/app/main.go:20),
conflicting with main.listUsersV2 (/app/main.go:35)
```

位置信息在 `buildEntry` 中通过 `runtime.FuncForPC` 内联提取。

## 五、路由分组（RouterGroup）

### 创建分组

```go
api := r.Group("/api", authMiddleware)
api.GET("/users", listUsers)   // 实际路径 /api/users
```

分组会为组内路由自动拼接 `prefix` 前缀，并叠加分组中间件。

### 嵌套分组

分组可嵌套，前缀逐层拼接，父分组中间件被继承且位于子分组中间件**之前**：

```go
api := r.Group("/api", mwA)
v1 := api.Group("/v1", mwB)
v1.GET("/users", listUsers)    // 路径 /api/v1/users，中间件顺序 [全局..., mwA, mwB]
```

### 前缀规范化（normalizePrefix）

- 空串 `""` 与 `"/"` 归一化为 `""`
- 不以 `/` 开头时自动补 `/`
- 去除末尾的 `/`

例如 `"users/"` → `"/users"`，`"/api/"` → `"/api"`。

## 六、中间件注册与快照

- `Use(...)` 向 `Router`（全局）或 `RouterGroup`（分组）追加中间件，返回自身支持链式调用。
- **中间件只对此后注册的路由生效**：注册路由时会将当前中间件链快照存入该路由的 `routeEntry.middlewares`。
- 最终中间件链顺序为 `[全局中间件..., 分组中间件...]`。

```go
r.Use(logger)              // 全局中间件
r.GET("/a", handlerA)      // 应用 [logger]

r.Use(auth)                // 之后追加
r.GET("/b", handlerB)      // 应用 [logger, auth]；/a 不受影响
```

> 中间件的执行模型（洋葱模型）详见 `middleware.md`。

## 七、并发约束

路由表（`Router.routes`）不支持动态注册后并发读取：

- **必须在服务对外提供请求之前完成所有路由注册**（典型用法：启动时注册完再 `ListenAndServe`）。
- 服务运行期间**不支持动态注册路由**。若在服务运行中调用 `GET`/`POST` 等注册方法，会触发 Go map 并发读写导致进程崩溃（`fatal error: concurrent map read and map write`）。
