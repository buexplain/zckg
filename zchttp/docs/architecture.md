# zchttp 架构与执行模型

本文说明 `zchttp` 的整体结构、注册期与请求期的分工，以及一次请求从进入到响应的完整路径。它回答“代码为什么这样组织”；具体字段/标签契约见 [reference.md](reference.md)，子系统细节见 [internals.md](internals.md)。

## 一、分层结构

```
┌─────────────────────────────────────────────────────────────┐
│ 公开 API：NewEngine / NewRouter / RouterGroup / 回调字段      │
├─────────────────────────────────────────────────────────────┤
│ HttpEngine.ServeHTTP                                        │
│   ├─ 路由匹配        router.go / router_trie.go              │
│   ├─ 注册期产物      buildEntry.go / routeEntry              │
│   ├─ Req 绑定        binding.go / defaults.go / meta.go      │
│   ├─ 中间件链        middleware.go / context.go              │
│   ├─ 参数校验        validate.go                             │
│   ├─ 响应与错误      httpEngine.go / responseWriter.go / errors.go │
│   └─ OpenAPI         openapi.go                              │
├─────────────────────────────────────────────────────────────┤
│ Go 标准库：net/http、encoding/json、mime/multipart、reflect   │
└─────────────────────────────────────────────────────────────┘
```

设计上的三个关键选择：

1. **注册期一次性预计算**：路由一旦注册，Req 的类型、字段元信息、默认值模板、路径参数绑定、中间件链、校验/默认值标记都固定下来；请求期直接复用，避免每请求重新反射解析标签。
2. **来源显式分流**：Form 链与 JSON body 是两套独立的命名/绑定语义；运行时按 method、Content-Type、`KeepRawBody`、`has*` 标志决定是否解析/读取。
3. **请求状态集中存放**：`requestState` 把 engine、request、writer、boundReq、bindingErr、res 合并进一个 ctx 值；中间件替换 w/r 时原地更新同一份状态，Context 访问器与回调看到同一组最近对象。

## 二、启动与注册期

### 1. 创建引擎

`NewEngine()` 返回带默认回调的 `HttpEngine`：

- `Router`：`NewRouter()`，预初始化 9 个标准方法的路由树；
- `OnNotFound` / `OnResponse` / `OnError` / `OnValidationError` / `OnPanic`：默认回调；
- `MaxBodyBytes`、`MultipartFormMaxMemory`：均 32 MB。

`HttpEngine{}` 字面量不会装配默认回调，`ServeHTTP` 也不做 nil 判断；因此约定必须用 `NewEngine()`。

### 2. 注册路由

`Router.GET/POST/.../Any` 与分组方法最终都走 `register`：

```
register
  → prepareRegistration
      → normalizePath
      → buildEntry（handler 签名、元信息、模板、中间件快照）
      → checkUnsupportedDefaults（default 误用告警）
      → parseRoutePath（路径语法）
      → attachPathParamBindings（路径参数 → 字段）
  → commitRegistration
      → insertRoute（基数树）
      → routes 顺序索引
```

`buildEntry` 的产物是 `routeEntry`，它同时服务绑定、校验、OpenAPI 与路由匹配：

| 字段 | 作用 |
| --- | --- |
| `handlerVal` / `reqType` / `resType` / `reqElemType` / `reqIsPtr` / `resIsPtr` | 请求期反射调用与构造 Req/Res |
| `reqMeta`（`structMeta`） | 字段绑定名、`nonzero`、`default`、时间标签、文件字段、来源能力标志 |
| `defaultReq` | 注册期已填默认值的 Req 模板 |
| `needsDeepCopy` | 模板是否含非 nil 引用字段，请求期是否需要深拷贝 |
| `needsRequestPhaseDefaults` | 类型树是否存在带 `default` 的指针字段 |
| `needsNonzeroValidation` | 类型树任意深度是否存在 `nonzero:"true"` |
| `pathParams` | 路径参数按注册顺序的字段索引与时间标签 |
| `keepRawBody` | 顶层值嵌入 `KeepRawBody` |
| `middlewares` | 全局 → 分组祖先 → 当前分组 → 路由级的快照 |
| `opMeta` | `OpenAPIMeta` 的操作级标签 |

注册期还会做这些“快速失败”：

- handler 不是函数、参数/返回值不符、Req/Res 不是 struct/`*struct` → panic；
- 路由语法非法、参数名非法/重复、可选参数后还有段、参数名无对应字段、路径目标类型不支持、文件字段作路径参数 → panic；
- Form 链绑定名重复、JSON 名重复 → panic；
- `KeepRawBody` 与带 Form 链绑定名的文件字段冲突 → panic；
- 路由冲突（同 method 下重复/参数名不一致/可选性不一致）→ panic。

`default` 的误用（不支持类型、值无法解析、请求期不可达）只告警，不阻断注册。

### 3. 中间件快照

`Router.Use` 与 `RouterGroup.Use` 只追加到当前对象；真正的链在注册路由时由 `buildEntry` 组装：

```
[全局中间件..., 最外层分组中间件..., 当前分组中间件..., 路由级中间件...]
```

路由级中间件来自注册方法的尾部变参，位于链的最内层、紧贴 core。快照后源切片再修改不影响已注册路由；之后调用 `Use` 也不追溯已注册路由。

## 三、请求期全链路

`HttpEngine.ServeHTTP` 的顺序（与 `httpEngine.go` 对应）：

| 步骤 | 行为 | 失败/边界 |
| --- | --- | --- |
| 1 | `MaxBodyBytes > 0` 时用 `http.MaxBytesReader` 包裹 `r.Body` | 读取超限返回 `*http.MaxBytesError`，绑定阶段读到则包装为 400 |
| 2 | 获取带 written 跟踪的 `responseWriter`；注册 body 排空/关闭 | 所有请求（含 404）都收尾 |
| 3 | 安装请求级 recover（含路由、绑定、中间件、handler、回调） | panic → `OnPanic`；回调再 panic → 第二层 recover + 兜底 500 |
| 4 | `normalizePath` 后路由匹配 | 未命中 → `OnNotFound`（404） |
| 5 | 构造 `requestState` 并注入 ctx | 后续 Context 访问器依赖它 |
| 6 | `reflect.New(reqElemType)` + 浅拷贝 `defaultReq`；按需 `deepCopyDefaults` | 模板不可变，请求实例独立 |
| 7 | `bindRequestData`：query（有 Form 文本字段时）→ body（有 body 来源字段时） | 错误存 `bindingErr`，不提前返回 |
| 8 | 参数路由 `bindPathParams`（仅在 `bindRequestData` 成功时）：覆盖同名 query/body | 转换失败走 `BindingError`（400） |
| 9 | `needsRequestPhaseDefaults` 时 `applyDefaults(requestPhase=true)` | 只补 nil 指针 |
| 10 | `st.boundReq = reqPtr.Interface()` | 即使绑定失败也注入，中间件可读错误 |
| 11 | `runChain` 执行中间件，最内层 core：绑定错误 → `validateRequest` → handler | 绑定错误优先，校验不执行 |
| 12 | handler 成功：`st.res` 注入，`OnResponse` 写出；错误按类型分发 | `BindingError`/`ValidationError` → 400；其余 → 500 |
| 13 | 回到外层：排空最多 2 KB 并 `Close` 入口 body | 连接复用由 net/http 决定 |

### 为什么绑定错误不提前返回

绑定失败时引擎仍然把 Req 和 `bindingErr` 注入 ctx，然后继续走中间件链，直到 core 层才返回错误。这样：

- 中间件可以通过 `BoundReqFromContext` 读到“已部分绑定 + 错误”，做日志/审计/统一处理；
- 错误统一在 `ServeHTTP` 最外层按 `*BindingError` 路由到 `OnValidationError`；
- 绑定失败不会执行参数校验，也不会调用 handler。

### 为什么 path 在 body 之后

路径参数来自路由模板，是比 query/body 更精确的意图。`bindRequestData` **成功**后才调用 `bindPathParams`，保证 `path > body > query`；路径参数转换失败直接 400。若 `bindRequestData` 已失败，path 不再写入（请求阶段默认值仍会执行），handler 也不会运行。

## 四、核心数据结构

```
routeEntry
 ├─ handler/中间件/位置信息
 ├─ reqType/resType/reqElemType/reqIsPtr/resIsPtr
 ├─ reqMeta structMeta
 │    └─ []fieldMeta{name, indices, field, jsonIncluded, nonzero, hasDefault, isFile, timeFormat...}
 ├─ defaultReq reflect.Value
 ├─ pathParams []pathParamBinding
 └─ needs* / keepRawBody

requestState
 ├─ engine *HttpEngine
 ├─ req *http.Request
 ├─ w http.ResponseWriter
 ├─ boundReq any
 ├─ bindingErr error
 └─ res any

chainRunner
 ├─ middlewares []MiddlewareHandler
 ├─ ctx/w/r/final
 ├─ calledBits uint64
 └─ current int

responseWriter
 ├─ http.ResponseWriter
 └─ written bool
```

- `routeEntry` 注册后只读，可安全并发共享。
- `requestState` 每请求一份，字段原地更新；中间件替换 w/r 时更新 `st.w/st.req`。
- `chainRunner` 从 `sync.Pool` 复用，位图按层防重；超过 64 层回退递归实现。
- `responseWriter` 从池中取基础包装器，按底层是否支持 Flusher/Hijacker/Pusher 组合出 8 种变体，避免接口断言误报能力。

## 五、并发与内存模型

- **注册必须早于服务**：`Router.trees`/`routes` 不是并发安全的；运行期注册属于数据竞争。
- **注册产物不可变**：`structMeta`、`routeEntry`、`defaultReq` 构建后只读；`structMetaCache` 用 `sync.Map` 缓存嵌套类型元信息。
- **模板隔离**：请求时浅拷贝 `defaultReq`；若模板含非 nil 指针/切片/map 或数组元素内含引用，先 `deepCopyDefaults` 断开共享。
- **对象池**：`responseWriter`、`chainRunner`、`json.Encoder`、默认值/校验的 visited map 都复用；归还前清空引用，避免池持有请求对象。
- **错误路径低分配**：`isNilError` 先判 `err == nil`，非 nil 才反射；JSON 先编码到池化缓冲，成功后再写响应。

## 六、错误与响应模型

| 产生位置 | 类型 | 默认处理 |
| --- | --- | --- |
| 绑定（query/body/path） | `*BindingError` | `OnValidationError` → 400 |
| 校验（nonzero/Validate） | `*ValidationError` | `OnValidationError` → 400 |
| handler/中间件普通 error | 任意 error | `OnError` → 500 |
| panic | recover 值 | `OnPanic` → 500（已写响应则跳过） |
| 路由未命中 | — | `OnNotFound` → 404 |

默认回调都先检查 `IsResponseWritten(w)`；已写响应则跳过，避免重复写头。`written` 由 responseWriter 跟踪：`WriteHeader`（最终状态码）/`Write`/`ReadFrom`/`Flush`/`Hijack` 会置位；除 101 外的 1xx 与 `Push` 不置位。

## 七、扩展点

| 扩展点 | 位置 | 说明 |
| --- | --- | --- |
| 中间件 | `Use` / `Group` / 路由尾部变参 | 洋葱模型，可替换 w/r、短路、捕获错误 |
| 响应/错误/校验失败/panic/404 回调 | `HttpEngine` 字段 | 默认 JSON/HTML；可完全自定义 |
| `Validator` | Req 实现 `Validate() error` | 在 nonzero 之后调用 |
| `OpenAPIInfo.ResponseWrapper` | OpenAPI 生成 | 自定义统一响应包装 schema |
| `KeepRawBody` | Req 顶层值嵌入 | 保留原始请求体 |
| 自定义 `OnResponse` | 回调 | 文件/流/Protobuf 等非 JSON 响应 |

## 八、继续阅读

- 标签/类型/错误/限制： [reference.md](reference.md)
- 元数据、trie、绑定、默认值、校验、中间件、响应、OpenAPI 的实现细节： [internals.md](internals.md)
- 主题用法： [routing.md](routing.md)、[request.md](request.md)、[parameter-binding.md](parameter-binding.md)、[parameter-validate.md](parameter-validate.md)、[middleware.md](middleware.md)、[http-engine-callback.md](http-engine-callback.md)、[openapi.md](openapi.md)
- 常见场景： [recipes.md](recipes.md)
