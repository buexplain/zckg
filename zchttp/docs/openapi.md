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
    Name string `json:"name" required:"true"`
}
```

| 标签 | 说明 |
| --- | --- |
| `tags` | 操作标签，以 `/` 分隔多个标签，如 `A/B` → `["A", "B"]` |
| `summary` | 操作摘要；缺省时回退为 handler 函数名（`shortFuncName` 去除包路径） |
| `description` | 操作详细描述 |

> `OpenAPIMeta` 是空结构体，仅承载标签信息，对请求参数绑定没有任何副作用。

## required 推断规则

字段是否 `required` 按以下优先级判定（文档生成与运行时校验使用完全相同的规则）：

1. 若字段带 `default` 标签 → **可选**（有默认值即非必填，优先级最高）。
2. 否则若字段带 `required` 标签：
    - `required:"true"` → **必填**；
    - `required:"false"` → **可选**。
3. 否则（未标注 `required`）→ **可选**（不做类型推断）。

> `default` 与 `required:"true"` 同时出现时以 `default` 为准（判为可选），避免「有默认值却又必填」的矛盾。

### 运行时非零值校验

`required` 不仅用于生成文档，还会在参数绑定完成后参与校验（`validateRequired` 复用上述判定，与文档一致）：

- 若字段被判为必填（即显式 `required:"true"` 且未带 `default`），但绑定后仍为零值（空字符串、数字 `0`、`false`、nil 指针/切片等），则绑定返回 `*ValidationError`。
- 该错误经由 `OnValidationError` 回调处理（默认 `DefaultValidationErrorHandler`，返回 **400**）；其余非校验错误仍走 `OnError`（默认 **500**）。

### 自定义业务校验（Validate）

声明式 `required` 只能表达单字段非零值。若需跨字段、业务规则等更丰富的校验，可让 Req 结构体实现 `Validator` 接口：

```go
type Validator interface {
    Validate() error
}
```

绑定与 `required` 校验均通过后，若 Req（值或指针接收者均可）实现了该接口，则自动调用 `Validate()`：

```go
type CreateEventReq struct {
    Start int `json:"start"`
    End   int `json:"end"`
}

func (r CreateEventReq) Validate() error {
    if r.Start >= r.End {
        return &zchttp.ValidationError{Field: "end", Message: "must be greater than start"}
    }
    return nil
}
```

- 返回 `nil` → 校验通过；
- 返回 `*ValidationError` → 直接透传（可携带 `Field` 等结构化信息）；
- 返回其他普通 `error` → 自动兜底包装为 `*ValidationError`（保留原始错误链，`errors.Is/As` 可穿透）。

三种情况下校验失败都统一走 `OnValidationError`（默认 **400**）。`Validate()` 是运行时任意 Go 代码，不会被反射提取为 OpenAPI schema 约束。

## 字段级标签

| 标签 | 说明 |
| --- | --- |
| `description` | 字段描述 |
| `example` | 字段示例值（按字段类型自动转换为对应 JSON 类型：整数→`int64`、浮点→`float64`、布尔→`bool`，无法转换时保留字符串） |
| `ignore:"true"` | 从文档中**排除**该字段（不影响绑定与校验） |
| `default` | 为字段设置默认值。标签受类型限制、分两阶段填充，且文档展示有额外约束——详见 [default 标签详解](#default-标签详解)。 |

字段名沿用绑定规则：优先 `form` 标签，其次 `json` 标签，最后使用字段名。

### default 标签详解

#### 支持范围（`isDefaultSupported`）

`default` 标签仅在以下类型上生效（`isDefaultSupported` 返回 true，`hasDefault` 置为 true）：

- 标量：`string`、`bool`、`int/uint` 全系列、`float32`/`float64`
- 标量指针：`*string`、`*int`、`*bool` 等
- 标量切片：`[]string`、`[]int`、`[]*int` 等

以下类型**不支持**：`struct`、`*struct`、`map`、`[]struct`、`[]*struct`、`time.Time`、`any` 等。设置后标签值被忽略，`hasDefault` 恒为 false。

#### 两阶段运行时填充

框架分别在注册阶段和请求阶段调用 `applyDefaults`，两阶段填充条件不同：

| 阶段 | 时机 | 判定条件 | 填充的类型 |
| --- | --- | --- | --- |
| **注册阶段** | 路由注册时（`buildEntry`） | `fv.IsZero()` | `isDefaultSupported=true` 的**全部类型**（标量、标量指针、标量切片） |
| **请求阶段** | JSON/表单绑定后（`httpEngine`） | `fv.Kind()==Ptr && fv.IsNil()` | 仅**标量指针**（`*int`/`*string`/`*bool` 等） |

- **注册阶段**：模板为空，所有零值的 `default` 字段被预填，生成 `defaultReq`。`int`/`string`/`[]string`/`*int` 等均参与。
- **请求阶段**：JSON 解析动态创建了 slice 元素 / map value / nested ptr struct 后，递归进入并对其中 nil 的**指针字段**补填默认值。**值类型（`int`/`string`/`bool`）跳过**，避免覆盖用户显式传入的零值（如 `{"qty": 0}` 不应被改成 `1`）。

#### OpenAPI 文档展示

生成的 schema 按**嵌套上下文**决定是否输出 `default` 属性：

| 字段类型 | 值嵌套 struct（含顶层 Req/Res）| 指针嵌套 struct（`*Company`）|
| --- | --- | --- |
| 指针类型（`*int`/`*string` 等）| 展示 default | 展示 default |
| 值类型（`int`/`string` 等）| 展示 default | **不展示** default |

> 规则依据：
> - **指针类型**：请求阶段（post-bind）对 nil 指针字段无条件填充，故在任何嵌套上下文中均可靠。
> - **值类型**：仅在注册阶段填充（请求阶段跳过以避免覆盖显式传入的零值），而注册阶段只能到达"值嵌套"路径上的 struct（顶层 Req/Res + 值类型 struct 字段）。指针嵌套（如 `*Company`）在注册阶段为 nil，其内部字段无法被到达，值类型的 default 实际不生效，故不展示。
> - 若同一 struct 被多处使用——路径 A 值嵌套、路径 B 指针嵌套——则取并集（展示），因为至少在一个场景下 default 有效。

实现上，`GenerateOpenAPI` 采用**两遍遍历**架构：第一遍收集每个 struct 类型的"值嵌套可达性"（`reachedViaValue` map），第二遍构造 schema 时 `decorate` 依据当前 struct 的可达性决定是否输出值类型字段的 `default`。详见 [内部实现架构](#内部实现架构)。

## 类型映射

| Go 类型 | OpenAPI schema |
| --- | --- |
| `string` | `{type: string}` |
| `bool` | `{type: boolean}` |
| `int` / `int64` | `{type: integer, format: int64}` |
| `int8`/`int16`/`int32` | `{type: integer, format: int32}` |
| `uint` / `uint8` ~ `uint64` | `{type: integer, format: int64, minimum: 0}` |
| `float32` | `{type: number, format: float}` |
| `float64` | `{type: number, format: double}` |
| `time.Time` | `{type: string, format: date-time}`；若 `time_format` 为 `unix*` 则为 `{type: integer, format: int64}` |
| 切片/数组 | `{type: array, items: ...}`，元素递归推断 |
| 结构体 | 注册到 `components/schemas` 并以 `$ref` 引用；同名结构体去重且支持递归嵌套循环引用检测 |
| 指针 | 被指向类型为 `$ref` 时包裹 `{nullable: true, allOf: [$ref]}`；否则在被指向类型上加 `nullable: true` |
| `map[K]V` | `{type: object, additionalProperties: <V 的 schema>}`；非 string key 在 description 中注明 `key type: <kind>` |
| `*multipart.FileHeader` | `{type: string, format: binary}` |
| `[]*multipart.FileHeader` | `{type: array, items: {type: string, format: binary}}` |

### map 类型详解

map 的 value 类型通过 `t.Elem()` 递归推断：

- `map[string]string` → `{"type":"object","additionalProperties":{"type":"string"}}`
- `map[string]int` → `{"type":"object","additionalProperties":{"type":"integer","format":"int64"}}`
- `map[string]*SomeStruct` → `{"type":"object","additionalProperties":{"nullable":true,"allOf":[{"$ref":"#/components/schemas/SomeStruct"}]}}`
- `map[string][]int` → `{"type":"object","additionalProperties":{"type":"array","items":{"type":"integer","format":"int64"}}}`
- `map[int]string` → `{"type":"object","additionalProperties":{"type":"string"},"description":"key type: int"}`（非 string key 在 description 注明）

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
- **其余方法**：请求字段生成为 `requestBody`；含文件字段（`*multipart.FileHeader` 或 `[]*multipart.FileHeader`）时使用 `multipart/form-data`，否则 `application/json`。
- **响应**：统一包装为 `Response{data, code, message}` 结构，schema 名称为 `Response_<Type>`；成功时统一返回 `200`（与 `HttpEngine` 默认响应行为一致）。

## 路由遍历

`GenerateOpenAPI` 遍历 Router 中全部 method × path 组合，为每条已注册路由生成对应的 operation。注册阶段预计算的 `reqMeta` / `resMeta` 被直接复用（字段绑定名、`required` 判定等），避免生成时重复反射。

## 内部实现架构

`GenerateOpenAPI` 的整体流程分为**初始化和两遍遍历**：

```
1. 初始化 openAPIGenerator
   ├── schemas:         已注册的类型 schema（components/schemas）
   ├── typeNames:       类型 → schema 名（去重 + 循环引用检测）
   ├── nameToType:      schema 名 → 类型（重名去重）
   ├── reachedViaValue:  struct → 是否被"值嵌套"路径到达
   └── currentType:      当前正在生成 schema 的 struct 类型（decorate 据此判断上下文）

2. 【Pass 1】collectTypeUsages(r)    —— 收集"值嵌套可达性"

3. 【Pass 2】遍历路由表，每条路由：
   ├── buildOperation(method, entry)
   │   ├── GET/DELETE/HEAD → buildQueryParams     → query 参数列表
   │   ├── 其他方法        → buildRequestBody      → requestBody schema
   │   └── 通用            → buildResponses         → 响应体（包装为 Response{data,code,message}）
   │       └── wrapResponseSchema
   │           └── registerStructSchema            → 注册 struct → 返回 $ref
   │               └── typeToSchema                → Go 类型 → JSON Schema 映射
   │                   └── decorate                 → 附加 default/example/description
   └── 组装 top-level 文档结构（openapi / info / paths / components）
```

### Pass 1：collectTypeUsages — 收集值嵌套可达性

第一遍遍历的职责是：**确定每个 struct 类型是否被"值嵌套"路径从顶层 Req/Res 到达**。

```go
// 简化逻辑
func (g *openAPIGenerator) collectTypeUsages(r *Router) {
    for each route:
        g.walkTypeUsage(derefType(reqType), viaValue=true, visiting={})
        g.walkTypeUsage(derefType(resType), viaValue=true, visiting={})
}
```

`walkTypeUsage` 递归遍历类型树，核心是 `viaValue` 标志的传播规则：

```
walkTypeUsage(t, viaValue, visiting):
  1. derefType(t)，非 struct → 返回
  2. if viaValue → g.reachedViaValue[t] = true   ← 标记为"值嵌套可达"
  3. 自引用检测：if visiting[t] → 返回（防止无限递归）
  4. visiting[t]=true; defer delete(visiting,t)
  5. 遍历字段，按 Kind 分派 viaValue：
     ├── Ptr（*Struct）        → viaValue=false  ← 指针断开"值嵌套"链
     ├── Struct                 → 继承父级 viaValue  ← 值类型嵌套继续传递
     ├── Slice/Array            → viaValue=false  ← 元素是动态创建的
     └── Map                    → viaValue=false  ← value 是动态创建的
```

`viaValue` 的语义：
- **`true`**：从顶层到当前类型，路径上所有 struct 字段均为值类型——注册阶段 `applyDefaults(requestPhase=false)` 能到达并填充所有零值字段。
- **`false`**：路径上存在指针/切片/map 断点——注册阶段无法到达（指针为 nil 跳过），只有请求阶段 `applyDefaults(requestPhase=true)` 能处理 nil 指针字段。

**示例**：

```go
type CreateUserReq struct {
    Name    string   `default:"无名"`
    Company *Company  // 指针嵌套
    School  School    // 值嵌套
}

type Company struct {
    Phone string  `default:"138"`   // 值类型
    Email *string `default:"a@b"`   // 指针类型
}

type School struct {
    Name string `default:"默认学校"`  // 值类型
}
```

Walk 过程：

```
walkTypeUsage(CreateUserReq, viaValue=true)
├── reachedViaValue[CreateUserReq] = true
├── field: Company (*Company)  → Ptr → walkTypeUsage(Company, viaValue=false)
│   └── viaValue=false → 不标记 Company
│   └── 但继续递归 Company 的子字段（标记 Company 内部引用的其他 struct 类型）
└── field: School (School)     → Struct → walkTypeUsage(School, viaValue=true)
    └── reachedViaValue[School] = true
```

最终 `reachedViaValue = {CreateUserReq: true, School: true}`，`Company` 不在 map 中（等价于 `false`）。

### Pass 2：Schema 构造

#### registerStructSchema — 注册 struct 并设置 currentType 上下文

每个 struct 在首次遇到时注册到 `components/schemas`，后续遇到返回 `$ref` 引用（天然支持去重和循环引用）：

```go
func (g *openAPIGenerator) registerStructSchema(t reflect.Type, meta structMeta) map[string]any {
    // 1. 去重：已注册 → 返回 $ref
    if name, ok := g.typeNames[t]; ok { return refSchema(name) }

    // 2. 分配唯一名称，占位到 schemas（防止递归嵌套时死循环）
    name := g.uniqueName(t)
    g.typeNames[t] = name
    g.schemas[name] = {"type": "object"}  // 占位

    // 3. 设置 currentType —— decorate 据此判断值类型 default 是否展示
    prevType := g.currentType
    g.currentType = t
    defer func() { g.currentType = prevType }()

    // 4. 遍历字段 → typeToSchema → 填充 properties 和 required
    for each field in meta.fields:
        props[name] = g.typeToSchema(f.Type, f)
        if required: append to required list
}
```

**`currentType` 的关键作用**：当 `decorate` 处理 `Company.Phone` 时，`g.currentType` 指向 `Company` 的 `reflect.Type`。`decorate` 通过 `g.reachedViaValue[g.currentType]` 查询 Company 是否被值嵌套到达，从而决定是否展示 `Phone` 的 default。

#### typeToSchema — Go 类型 → JSON Schema 映射

类型分发树（简化）：

```
typeToSchema(t, field):
  ├── Ptr（*Struct）
  │   ├── 递归 typeToSchema(t.Elem(), field)
  │   ├── 结果是 $ref → 包裹为 {nullable:true, allOf:[$ref]}
  │   └── 否则 → 直接加 nullable:true
  │   └── decorate(...)  ← 指针字段自身的标签
  ├── time.Time → timeSchema（unix→integer，否则→date-time）
  ├── String / Bool / Int* / Uint* / Float*
  │   → 基础类型 schema + decorate
  ├── Slice / Array
  │   → {type:array, items: typeToSchema(elem, emptyField)} + decorate
  ├── Struct
  │   → registerStructSchema(t, ...) → $ref + decorate
  ├── Map
  │   → {type:object, additionalProperties: typeToSchema(elem, emptyField)} + decorate
  └── 其他 → {}
```

> 切片元素和 map value 使用 `emptyField`（Tag 为空），因此不会附加 default/example/description 标签信息，仅传递类型 schema。

#### decorate — default 展示的最终决策点

```go
func (g *openAPIGenerator) decorate(schema, field) {
    if hasDefaultTag && isDefaultSupported(field.Type) {
        // 展示 default 的两条路径，满足任一即可：
        if field.Type.Kind() == reflect.Ptr ||           // 条件A：指针类型 → 请求阶段可靠填充
           g.reachedViaValue[g.currentType] {             // 条件B：值嵌套可达 → 注册阶段可靠填充
            schema["default"] = coerceExample(schema, def)
        }
    }
    // example / description 无额外条件，始终附加
}
```

决策矩阵（结合两阶段运行时语义）：

| 字段类型 | 所属 struct 的嵌套上下文 | 条件A（Ptr）| 条件B（reachedViaValue）| 展示 default? | 运行时保证 |
|---------|----------------------|------------|----------------------|-------------|----------|
| `*string` | 值嵌套（如 School）| ✅ | ✅ | ✅ | 注册阶段 + 请求阶段双保险 |
| `*string` | 指针嵌套（如 Company）| ✅ | ❌ | ✅ | 请求阶段填充 nil 指针 |
| `string` | 值嵌套（如 School）| ❌ | ✅ | ✅ | 注册阶段填充零值 |
| `string` | 指针嵌套（如 Company）| ❌ | ❌ | ❌ | 永不填充（注册阶段 nil 跳过，请求阶段值类型跳过）|

### 完整端到端示例

以下示例覆盖全部四种场景：

```go
type Company struct {
    Phone string  `default:"13800138000"`  // 值类型 + 指针嵌套
    Email *string `default:"a@b.com"`      // 指针类型 + 指针嵌套
}

type School struct {
    Name string `default:"默认学校"`        // 值类型 + 值嵌套
}

type CreateUserReq struct {
    Name    string   `default:"无名"`       // 值类型 + 顶层（值嵌套）
    Company *Company                        // 指针嵌套
    School  School                          // 值嵌套
}
```

**运行时行为**：

| 字段 | 注册阶段填充? | 请求阶段填充? | 最终保证? |
|------|-------------|-------------|----------|
| `CreateUserReq.Name` | ✅ 零值触发 | ❌ 值类型跳过 | ✅ |
| `School.Name` | ✅ 零值触发（School 值嵌套可达）| ❌ 值类型跳过 | ✅ |
| `Company.Phone` | ❌ Company 为 nil，未到达 | ❌ 值类型跳过 | ❌ 永不填充 |
| `Company.Email` | ❌ Company 为 nil，未到达 | ✅ nil 指针触发（若 JSON 传入 `{"Company":{}}`）| ✅ |

**文档生成结果**（Pass 1 → Pass 2）：

```
Pass 1: reachedViaValue = {CreateUserReq: true, School: true}
         Company 不在 map 中（等价 false）

Pass 2: 生成 Company schema（currentType=Company）：
  - Phone (string, default="13800138000")
    → isDefaultSupported ✅, Kind ≠ Ptr, reachedViaValue[Company] = false
    → 不展示 default
  - Email (*string, default="a@b.com")
    → isDefaultSupported ✅, Kind = Ptr
    → 展示 default: "a@b.com"

Pass 2: 生成 School schema（currentType=School）：
  - Name (string, default="默认学校")
    → isDefaultSupported ✅, Kind ≠ Ptr, reachedViaValue[School] = true
    → 展示 default: "默认学校"

Pass 2: 生成 CreateUserReq schema（currentType=CreateUserReq）：
  - Name (string, default="无名")
    → isDefaultSupported ✅, Kind ≠ Ptr, reachedViaValue[CreateUserReq] = true
    → 展示 default: "无名"
```

最终 OpenAPI 输出中，`Company.Phone` 的 schema **不含** `default` 属性，其余三个字段的 schema **均含** `default`，精确匹配运行时填充语义。
