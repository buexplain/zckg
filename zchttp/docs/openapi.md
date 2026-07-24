# OpenAPI 3.0 文档生成

`zchttp` 可以从已注册的路由表通过反射自动生成 OpenAPI 3.0 文档，无需手写 YAML/JSON。

## 快速开始

```go
r := zchttp.NewRouter()
r.POST("/users", CreateUser)
r.GET("/users", ListUsers)

doc := zchttp.GenerateOpenAPI(r, zchttp.OpenAPIInfo{
Title:       "示例 API",
Description: "用户服务接口",
Version:     "1.0.0",
Servers: []zchttp.OpenAPIServer{
{URL: "https://api.example.com", Description: "生产环境"},
},
})

// doc 是 map[string]any，可直接序列化为 JSON
data, _ := json.Marshal(doc)
```

`GenerateOpenAPI` 返回 `map[string]any`，可用 `encoding/json` 序列化后对外提供，或挂载到某个路由供 Swagger UI 消费。

## OpenAPIMeta 嵌入：声明操作级元信息

在 handler 的 `Req` 结构体中嵌入 `zchttp.OpenAPIMeta`，通过结构体标签声明标签、摘要、描述：

```go
type CreateUserReq struct {
zchttp.OpenAPIMeta `tags:"User Management/Account" summary:"创建用户" description:"创建一个新的用户账户"`
Name string `json:"name" nonzero:"true"`
}
```

| 标签            | 说明                                             |
|---------------|------------------------------------------------|
| `tags`        | 操作标签，以 `/` 分隔多个标签，如 `A/B` → `["A", "B"]`       |
| `summary`     | 操作摘要；缺省时回退为 handler 函数名（`shortFuncName` 去除包路径） |
| `description` | 操作详细描述                                         |

> `OpenAPIMeta` 是空结构体，仅承载标签信息，对请求参数绑定没有任何副作用。

## 字段级标签

以下标签同时适用于 Req（请求结构体）和 Res（响应结构体），但部分标签仅在 Req 上有运行时语义：

| 标签              | Req | Res | 说明                                                                       |
|-----------------|-----|-----|--------------------------------------------------------------------------|
| `description`   | ✅   | ✅   | 字段描述                                                                     |
| `example`       | ✅   | ✅   | 字段示例值（按字段类型自动转换为对应 JSON 类型：整数→`int64`、浮点→`float64`、布尔→`bool`，无法转换时保留字符串） |
| `ignore:"true"` | ✅   | ✅   | 从文档中**排除**该字段（不影响绑定与校验）                                                  |
| `default`       | ✅   | ❌   | 为字段设置默认值（仅 Req 有效）。受类型限制、分两阶段填充，且文档展示有额外约束——详见 [default 文档展示规则](#default-文档展示规则仅-req)。    |
| `nonzero`       | ✅   | ❌   | 标记字段为非零值必填（仅 Req 有效），影响运行时校验和 required 推断——详见 [required 推断规则](#required-推断规则)。       |

> Res 结构体上的 `default` 和 `nonzero` 标签会被忽略：Res 不参与参数绑定与校验，`applyDefaults` 和 `validateNonzero` 均仅作用于 Req。

字段名沿用绑定规则：优先 `form` 标签，其次 `json` 标签，最后使用字段名。

## default 文档展示规则（仅 Req）

`default` 标签仅对 Req 结构体有效。生成的 schema 按**嵌套上下文**决定是否输出 `default` 属性。

### struct 嵌套上下文

| 字段类型                     | 值嵌套 struct（含顶层 Req） | 指针嵌套 struct（`*Company`） |
|--------------------------|-------------------------|-------------------------|
| 指针类型（`*int`/`*string` 等） | 展示 default              | 展示 default              |
| 值类型（`int`/`string` 等）    | 展示 default              | **不展示** default         |

> 规则依据：
> - **指针类型**：请求阶段（post-bind）对 nil 指针字段无条件填充，故在任何 struct 嵌套上下文中均可靠（前提是该 struct 可被 `applyDefaults` 到达）。
> - **值类型**：仅在注册阶段填充（请求阶段跳过以避免覆盖显式传入的零值），而注册阶段只能到达"值嵌套"路径上的 struct（顶层
    Req + 值类型 struct 字段）。指针嵌套（如 `*Company`）在注册阶段为 nil，其内部字段无法被到达，值类型的 default
    实际不生效，故不展示。
> - 若同一 struct 被多处使用——路径 A 值嵌套、路径 B 指针嵌套——则取并集（展示），因为至少在一个场景下 default 有效。

### 容器嵌套深度

上述规则假设 struct 可被 `applyDefaults` 到达。对于**多层容器**（如 `map[K][]Struct`、`[][]Struct`），框架无法穿透内部元素，即使是指针字段的 `default` 也**不展示**（`reachedByDefaults=false`）。详见 `request.md` 中"容器嵌套深度限制"章节。

`GenerateOpenAPI` 采用**三遍遍历**架构：第一遍（`collectTypeUsages`）收集每个 struct 类型的"值嵌套可达性"（`reachedViaValue` map）；第二遍（`collectDefaultsReachability`）收集每个 struct 类型是否可被 `applyDefaults` 递归到达（`reachedByDefaults` map）；第三遍构造 schema 时 `decorate` 依据这两个可达性标记决定是否输出 `default`——指针字段依赖 `reachedByDefaults`，值字段依赖 `reachedViaValue`。

## required 推断规则（仅 Req）

Req 字段在 OpenAPI 文档中是否标记为 `required`，按以下规则判定：

1. 字段带 `nonzero:"true"` 且**没有** `default` 标签 → **必填**。
2. 其它情况（有 `default`、或 `nonzero:"false"`、或未标注 `nonzero`）→ **可选**。

> `nonzero` 标签与 `default` 标签独立解析：带 `default` 的 `nonzero:"true"` 字段在运行时**仍然会校验零值**
> （所见即所得），但在文档中标记为可选，避免「有默认值却又必填」的矛盾。

## 类型映射

| Go 类型                       | OpenAPI schema                                                                                         |
|-----------------------------|--------------------------------------------------------------------------------------------------------|
| `string`                    | `{type: string}`                                                                                       |
| `bool`                      | `{type: boolean}`                                                                                      |
| `int` / `int64`             | `{type: integer, format: int64}`                                                                       |
| `int8`/`int16`/`int32`      | `{type: integer, format: int32}`                                                                       |
| `uint` / `uint8` ~ `uint64` | `{type: integer, format: int64, minimum: 0}`                                                           |
| `float32`                   | `{type: number, format: float}`                                                                        |
| `float64`                   | `{type: number, format: double}`                                                                       |
| `time.Time`                 | `{type: string, format: date-time}`；若 `time_format` 为 `unix*` 则为 `{type: integer, format: int64}`      |
| 切片/数组                       | `{type: array, items: ...}`，元素递归推断                                                                     |
| 结构体                         | 注册到 `components/schemas` 并以 `$ref` 引用；同名结构体去重且支持递归嵌套循环引用检测                                             |
| 指针                          | 被指向类型为 `$ref` 时包裹 `{nullable: true, allOf: [$ref]}`；否则在被指向类型上加 `nullable: true`                        |
| `map[K]V`                   | `{type: object, additionalProperties: <V 的 schema>}`；非 string key 在 description 中注明 `key type: <kind>` |
| `*multipart.FileHeader`     | `{type: string, format: binary}`                                                                       |
| `[]*multipart.FileHeader`   | `{type: array, items: {type: string, format: binary}}`                                                 |


### map 类型详解

map 的 value 类型通过 `t.Elem()` 递归推断：

- `map[string]string` → `{"type":"object","additionalProperties":{"type":"string"}}`
- `map[string]int` → `{"type":"object","additionalProperties":{"type":"integer","format":"int64"}}`
- `map[string]*SomeStruct` →
  `{"type":"object","additionalProperties":{"nullable":true,"allOf":[{"$ref":"#/components/schemas/SomeStruct"}]}}`
- `map[string][]int` →
  `{"type":"object","additionalProperties":{"type":"array","items":{"type":"integer","format":"int64"}}}`
- `map[int]string` → `{"type":"object","additionalProperties":{"type":"string"},"description":"key type: int"}`（非
  string key 在 description 注明）

### 指针类型 nullable 处理

指针类型分两种情况生成 schema：

1. 若被指向类型已生成为 `$ref`（即结构体），使用 `allOf` + `nullable` 包裹，保持 `$ref` 引用的规范性：

```
{"nullable": true, "allOf": [{"$ref": "#/components/schemas/Foo"}]}
```

2. 若被指向类型是标量（如 `*int`、`*string`），直接在被指向类型 schema 上追加 `nullable: true`：

```
{"type": "integer", "format": "int64", "nullable": true}
```

## 参数位置与响应

- **GET / DELETE / HEAD**：请求字段生成为 `query` 参数（`in: query`）。
- **其余方法**：请求字段生成为 `requestBody`；含文件字段（`*multipart.FileHeader` 或 `[]*multipart.FileHeader`）时使用
  `multipart/form-data`，否则 `application/json`。
- **响应**：统一包装为 `Response{data, code, message}` 结构，schema 名称为 `Response_<Type>`；成功时统一返回 `200`（与
  `HttpEngine` 默认响应行为一致）。可通过 `OpenAPIInfo.ResponseWrapper` 指定自定义响应包装结构体样例（如 `MyResponse{}`），为 nil 时使用默认结构；自定义结构体中 `interface{}` 类型字段被视为 data 占位符，替换为实际 Res schema。
