# zchttp 文档

`zchttp` 是一个基于 `net/http` 的小型 HTTP 框架：用**注册期反射预计算**换取请求期低反射开销，用**结构体标签**表达参数来源、默认值与校验，并在同一套 Req/Res 类型上生成 OpenAPI 3.0 文档。

本目录是 `zchttp` 的完整文档。目标不是罗列 API，而是让读者同时掌握三件事：

1. **怎么用**：路由、Req/Res、标签、中间件、回调、OpenAPI 的公开契约；
2. **代码怎么走**：一次请求从 `ServeHTTP` 到响应写出的完整路径；
3. **为什么这样设计**：注册期/请求期拆分、反射元数据、缓存与池化、各来源的优先级与边界。

## 五分钟示例

```go
func example() {
    engine := zchttp.NewEngine()

    engine.Router.POST("/users/{id}", func(ctx context.Context, req UpdateUserReq) (UpdateUserRes, error) {
        return UpdateUserRes{ID: req.ID, Name: req.Name}, nil
    })

    server := &http.Server{Addr: ":8080"}
    _ = engine.Run(server)
}

type UpdateUserReq struct {
    ID   int64  `json:"id" nonzero:"true"`
    Name string `json:"name" default:"anonymous"`
}

type UpdateUserRes struct {
    ID   int64  `json:"id"`
    Name string `json:"name"`
}
```

这段代码已经覆盖框架的主链路：`NewEngine` 装配默认回调与请求体上限，`Router.POST` 在注册期校验 handler 签名并预计算 Req 元信息，请求期完成绑定/校验/调用，最后由默认回调写出统一 JSON 响应。

## 心智模型

先记住四条主线，后面的文档都围绕它们展开：

1. **一次请求只有一个入口**：`HttpEngine.ServeHTTP`。路由匹配、绑定、中间件、校验、handler、响应/错误分发都在这里串起来。
2. **注册期做重活，请求期做轻活**：注册时构建 `routeEntry`、`structMeta`、`defaultReq`、路径参数绑定、中间件快照和各类 `needs*` 标志；请求时只做模板拷贝、绑定、校验、反射调用与响应。
3. **Req 字段有两套来源投影**：Form 链（query / urlencoded / multipart / path）与 JSON body 各自解析绑定名，不能跨来源混用；同一个字段可能同时参与两者。
4. **优先级是明确的**：匹配时静态段 > 参数段 > 通配尾段；绑定值 path > body > query；显式请求值 > `default`；`nonzero` 是最终校验。

### 请求生命周期

```
ServeHTTP
  │
  ├─ MaxBodyBytes 包裹 r.Body；包装 ResponseWriter；注册 body 排空/关闭
  ├─ 路由匹配（静态 > 参数 > 通配，失败回溯；末尾斜杠归一化）
  ├─ reflect.New + 浅拷贝 defaultReq（需要时 deepCopyDefaults）
  ├─ bindRequestData：query → body（按 method / Content-Type；KeepRawBody 跳过 body）
  ├─ bindPathParams：路径参数覆盖同名 query/body
  ├─ applyDefaults(requestPhase=true)：只补 nil 指针
  ├─ 中间件洋葱链（runChain；core 在最内层）
  │    └─ core：validateNonzero → validateCustom → handler → OnResponse
  ├─ 错误分发：BindingError / ValidationError → OnValidationError；其余 → OnError
  ├─ panic：OnPanic（第二层 recover 兜底）
  └─ 限量排空并关闭 body
```

这张图的每一段都有对应文档：路由见 [routing.md](routing.md)，绑定见 [parameter-binding.md](parameter-binding.md)，校验见 [parameter-validate.md](parameter-validate.md)，中间件见 [middleware.md](middleware.md)，回调与响应见 [http-engine-callback.md](http-engine-callback.md)。

### 注册期与请求期

| 阶段 | 主要工作 | 代码 |
| --- | --- | --- |
| 注册期 | 校验 handler 签名；构建 `structMeta`/`fieldMeta`；生成 `defaultReq`；预计算 `needsDeepCopy`、`needsRequestPhaseDefaults`、`needsNonzeroValidation`；绑定路径参数；快照中间件 | `buildEntry`、`buildStructMeta`、`applyDefaults`、`attachPathParamBindings` |
| 请求期 | 路由匹配；模板拷贝；绑定；请求阶段默认值；中间件；校验；反射调用 handler；响应 | `ServeHTTP`、`bindRequestData`、`bindPathParams`、`runChain`、`validateRequest` |

注册期的产物是不可变的，可跨 goroutine 共享；请求期状态放在 `requestState` 中，随 ctx 传递。详见 [architecture.md](architecture.md) 与 [internals.md](internals.md)。

## 文档地图

| 文档 | 类型 | 适合谁 | 回答什么 |
| --- | --- | --- | --- |
| [README.md](README.md) | 入口 | 所有人 | zchttp 是什么、怎么读、核心模型 |
| [architecture.md](architecture.md) | 原理 | 想理解整体的人 | 分层、注册期/请求期、生命周期、数据结构 |
| [routing.md](routing.md) | 指南 | 业务开发者 | 路由、分组、路径参数、匹配与冲突 |
| [request.md](request.md) | 指南 | 业务开发者 | Req/Res 定义、标签总览、默认值/校验/时间的基础用法 |
| [parameter-binding.md](parameter-binding.md) | 指南 | 业务开发者 | query/body/path 的来源分流、合并、类型、文件、KeepRawBody |
| [parameter-validate.md](parameter-validate.md) | 指南 | 业务开发者 | nonzero、Validate、执行顺序与错误 |
| [middleware.md](middleware.md) | 指南 | 扩展开发者 | 洋葱模型、next、w/r 替换、上下文、注册 |
| [http-engine-callback.md](http-engine-callback.md) | 指南 | 扩展开发者 | 默认回调、错误分发、panic、written、上下文访问 |
| [openapi.md](openapi.md) | 指南 | 接口维护者 | OpenAPI 生成、标签映射、required、响应包装 |
| [reference.md](reference.md) | 参考 | 所有人 | 标签、类型矩阵、错误、限制、不变量的单一事实源 |
| [internals.md](internals.md) | 原理 | 维护者/深度使用者 | 元数据、trie、绑定、默认值、校验、中间件、响应、OpenAPI 实现 |
| [recipes.md](recipes.md) | 配方 | 业务开发者 | 上传、代理、静态文件、SSE、自定义回调等 |

### 按角色阅读

- **第一次使用**：README → routing.md → request.md → parameter-binding.md → parameter-validate.md → recipes.md。
- **要用中间件/自定义响应**：middleware.md → http-engine-callback.md → architecture.md。
- **要维护/扩展框架**：architecture.md → internals.md → reference.md，再按需回看主题文档。
- **要排查行为差异**：reference.md（单一事实源）→ 主题文档 → internals.md 对应小节。

## 代码地图

| 概念 | 源码 | 锁死测试 |
| --- | --- | --- |
| 引擎入口、默认回调、MaxBodyBytes | `httpEngine.go` | `httpEngine_test.go` |
| 路由器、分组、注册冲突 | `router.go` | `router_test.go` |
| 基数树匹配、可选/通配 | `router_trie.go` | `router_trie_test.go` |
| 注册期预计算、KeepRawBody、路径参数绑定 | `buildEntry.go` | `buildEntry_test.go` |
| 字段元信息、来源投影、重复名 | `meta.go` | `meta_test.go` |
| query/body/path 绑定、时间解析、文件 | `binding.go` | `binding_test.go` |
| 默认值两阶段、容器深度、告警 | `defaults.go` | `defaults_test.go` |
| nonzero 递归、Validator、错误包装 | `validate.go`、`errors.go` | `validate_test.go`、`errors_test.go` |
| 中间件链、next 防重、w/r 替换 | `middleware.go` | `middleware_test.go` |
| 请求状态与 Context 访问器 | `context.go` | `context_test.go` |
| written 跟踪、1xx/101/Push/Unwrap | `responseWriter.go` | `responseWriter_test.go` |
| OpenAPI 生成、schema、required、包装 | `openapi.go` | `openapi_test.go` |
| 文档示例编译校验 | — | `docs_examples_compile_test.go` |
| 文档-代码偏离回归 | — | `docs_deviation_review_test.go` |

## 阅读约定

- **公开契约**与**当前实现**分开写。契约是调用方可以依赖的行为；标为“当前实现”的内容用于解释代码，可能随内部优化变化。
- 行为声明尽量给出 **源码函数** 与 **锁死测试**，而不是只给结论。
- 标签、类型、错误、限制只在 [reference.md](reference.md) 维护一份；主题文档只写摘要并链接，避免同一事实多处漂移。
- Go 示例要求语法/结构正确；代表性示例另由 `docs_examples_compile_test.go` 做编译级校验。伪代码、流程图用 `text` 代码块。
- 文档中的优先级、默认值、错误码若与 [reference.md](reference.md) 冲突，以 reference 为准，并应同步修正主题文档。

## 关键不变量速查

- `HttpEngine` 必须用 `NewEngine()` 构造（默认回调、`MaxBodyBytes`/`MultipartFormMaxMemory` 均 32 MB）；`ServeHTTP` 不对回调做 nil 判断。
- handler 签名固定为 `func(ctx context.Context, req Req) (Res, error)`；注册期校验失败即 panic。
- 路由优先级：静态段 > 参数段 > 通配尾段；绑定优先级：path > body > query。
- query/body 一旦参与绑定就严格：语法错误或已知字段转换失败返回 400。
- 没有可绑定字段的来源不解析、不校验；没有 body 可绑定字段时不读取 body。
- Form 链绑定名重复、JSON 名重复在注册期 panic。
- 中间件链的 `next` 每层只能调用一次；重复调用返回 `ErrNextCalledMultipleTimes`。
- 默认 JSON 响应由 `encoding/json` 生成；默认错误 500、校验/绑定失败 400、未命中 404。
- OpenAPI 请求/响应角色分开生成 schema；响应不输出请求侧约束标签。

完整清单与源码/测试映射见 [reference.md](reference.md)。
