# OpenAPI 文档生成

本文说明 `zchttp` 如何从已注册路由生成 OpenAPI 3.0 文档：操作级元信息、字段标签、schema 角色、类型映射、required 推断、参数位置与响应包装。实现细节（三遍遍历、可达性）见 [internals.md](internals.md) 的“OpenAPI 生成器”一节。

适合：需要对外提供 OpenAPI/Swagger 文档的接口维护者。

## 一、快速开始

```go
func example() {
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

    data, _ := json.Marshal(doc)
    _ = data
}
```

`GenerateOpenAPI` 返回 `map[string]any`，可用 `encoding/json` 序列化，或挂到某个路由供 Swagger UI 消费。生成前对 method/path 排序，多次生成字节级一致；生产环境通常启动时生成一次并缓存。

## 二、OpenAPIMeta

在 Req 中嵌入 `zchttp.OpenAPIMeta`：

```go
type CreateUserReq struct {
    zchttp.OpenAPIMeta `tags:"User Management/Account" summary:"创建用户" description:"创建一个新的用户账户"`
    Name string `json:"name" nonzero:"true"`
}

type DeleteUserReq struct {
    zchttp.OpenAPIMeta `tags:"User" summary:"删除用户" deprecated:"true" description:"已废弃，请使用 DELETE /v2/users/{id}"`
    ID int64 `json:"id" nonzero:"true"`
}
```

| 标签 | 说明 |
| --- | --- |
| `tags` | 以 `/` 分隔，trim 空项，输出标签数组 |
| `summary` | 操作摘要；缺省回退 handler 短函数名 |
| `description` | 操作描述 |
| `deprecated` | `true` 输出操作级 `deprecated: true` |

- `OpenAPIMeta` 空结构体，不参与绑定/校验。
- 只能嵌入 Req 顶层；写在 Res 上注册期告警且无效。
- 操作级与字段级 `deprecated` 都用 `strconv.ParseBool`；空/非法值不启用。

## 三、标签与 schema 角色

| 标签 | Req | Res | 说明 |
| --- | --- | --- | --- |
| `description` | ✅ | ✅ | 字段描述 |
| `example` | ✅ | ✅ | schema `example`，按类型转换 |
| `ignore:"true"` | ✅ | ✅ | 从 schema/query 排除；path 声明除外 |
| `default` | ✅ | ❌ | 请求 schema 展示；响应不展示 |
| `nonzero` | ✅ | ❌ | 影响请求 `required`；响应不参与 |
| `time_format` | ✅ | ❌ | 仅表单形态读；JSON 请求体/响应恒 date-time |
| `deprecated` | ✅ | ✅ | schema/query/path 参数输出 |

- 同一类型同时作 Req/Res 时生成两份：请求裸名 `<Type>`，响应 `<Type>Response`；响应不读请求侧约束。
- 同一类型既作 multipart 顶层请求体又作 JSON 请求结构（同角色）时，因 `form` 维度不同生成两份，后注册者名追加序号（`Type2`）；响应变体仍用 `<Type>Response`。
- `ignore` 影响请求/响应 schema 与 query，不影响 path 声明。
- 值类型嵌套 struct 的 `deprecated` 作为 `$ref` 兄弟键输出，但 OpenAPI 3.0 工具忽略；需要生效用指针嵌套。

### deprecated 标记

操作级：

```go
type DeleteUserReq struct {
    zchttp.OpenAPIMeta `tags:"User" summary:"删除用户" deprecated:"true" description:"已废弃，请使用 DELETE /v2/users/{id}"`
    ID int64 `json:"id" nonzero:"true"`
}
```

字段级（Req 与 Res 的受支持字段）：

```go
type UpdateUserReq struct {
    zchttp.OpenAPIMeta `tags:"User" summary:"更新用户"`
    Name    string `json:"name" nonzero:"true"`
    OldNick string `json:"old_nick" deprecated:"true" description:"已废弃，请使用 name"`
}
```

- 生成字段 schema 的 `"deprecated": true`；GET/DELETE/HEAD 的 query 参数、所有方法的 path 参数在 Parameter Object 顶层输出同一标记。
- 其余方法仍保留 requestBody；要从 body schema 移除路径字段，用 `ignore:"true"`（path 声明仍保留）。
- 值类型嵌套 struct 字段的 deprecated 作为 `$ref` 兄弟键写入，但 OpenAPI 3.0 工具忽略；需要生效用指针嵌套。
- 单文件/文件切片的 binary schema 也保留 deprecated/description/example；切片/数组/map 的标记只作用于字段自身，不传给元素。

废弃原因写在 `description`（支持 Markdown）：

```go
type LegacyFields struct {
    OldField string `json:"old_field" deprecated:"true" description:"~~已废弃~~ 请使用 \x60new_field\x60，本字段将在 v3 移除"`
}
```

Go 原始字符串不能转义反引号；`\x60` 由 `reflect.StructTag.Get` 还原为反引号。`deprecated` 是纯文档标签，不影响绑定/校验/默认值。

## 四、字段命名

- query/path/multipart 顶层：Form 链绑定名（`form` > 普通 `json` > 字段名）；`json:"-"`/`json:"-,"` 回退字段名，`form:"-"` 不参与。
- multipart 顶层只展示 `bindValues` 可写入的扁平字段：标量、标量指针、标量切片、文件字段；命名 struct/map/数组/多层容器/指针包裹容器不输出。
- JSON 请求体、响应、嵌套 struct：`encoding/json` 规则——json 名优先、空/非法名回退字段名、`json:"-"` 排除、`json:"-,"` 名为 `-`、匿名嵌入展开/带名嵌套、同深度冲突丢弃；`form` 不参与。
- 与 `encoding/json` 的差异：未导出匿名指针嵌入不展开；`,string` 不改变 schema 类型；JSON 请求体不读 `time_format`；`[]byte` 请求侧按字节数组描述。

## 五、类型映射

| Go 类型 | OpenAPI schema |
| --- | --- |
| `string` | `{type:string}` |
| `bool` | `{type:boolean}` |
| `int`/`int64` | `{type:integer, format:int64}` |
| `int8/16/32` | `{type:integer, format:int32}` |
| `uint*` | `{type:integer, format:int64, minimum:0}` |
| `float32` | `{type:number, format:float}` |
| `float64` | `{type:number, format:double}` |
| `time.Time` | query/form/multipart/path 默认 date-time；`unix*` 为 integer/int64；JSON 请求体/响应恒 date-time |
| `[]byte`/命名 byte slice | Req：array 0..255 + nullable；Res：string/byte（base64） |
| 切片/数组 | array + items；普通切片 nullable，固定数组不 |
| 结构体 | `$ref` 到 components/schemas |
| 指针 | 指向 `$ref` → `{nullable:true, allOf:[$ref]}`；否则加 nullable |
| `map[K]V` | object + additionalProperties + nullable；非 string key 在 description 注明 |
| 文件 | `string/binary` 或 array of binary |

自定义 `MarshalJSON`/`UnmarshalJSON` 的类型不保证精确映射；请用 `description` 说明线上格式。

## 六、required 推断

请求 schema 的 `required`：

1. 字段 `nonzero:"true"`，且不存在“受支持、可解析、本身非零”的 `default` → 必填；
2. 其他情况 → 可选。

示例：

- `time.Time nonzero:"true" default:"x"`：default 不支持 → required；
- `int default:"abc"`：无法解析 → required；
- `int default:"0"`、`string default:""`：可解析但零值 → required；
- `int default:"1"`：可解析且非零 → 可选。

响应 schema 不输出 `required`：nonzero 是请求侧运行时约束，响应没有对应语义。

带 json 名的匿名嵌入在 JSON schema 中是嵌套对象；若其扁平 nonzero 子字段无法由 default 满足，父级会把嵌入属性名加入 required。无 json 名的匿名嵌入不产生属性，必填子字段直接列在父级 `required`。

## 七、参数位置

### GET / DELETE / HEAD

- 字段生成 `in: query` 参数。
- 只保留 `isQueryBindableType` 的扁平字段：标量、标量切片、指针标量、`time.Time`、`[]byte`；命名 struct、map、文件字段跳过。
- 绑定到路径参数的字段不重复作 query。
- `ignore` 字段跳过；`deprecated` 输出在 Parameter Object 顶层。

### 其余方法

- 生成 `requestBody`；含文件字段（含嵌入 struct 中的）→ `multipart/form-data`，否则 `application/json`。
- multipart 顶层只描述扁平字段；命名 struct/map/数组/多层容器不输出。
- `requestBody.required`：以 `defaultReq` 为初始状态，排除 path 字段后跑内建 `validateNonzero`；失败为 true。自定义 `Validate` 不参与。该推导有意比运行时可接受输入更严格（不考虑 query 满足 body 字段）。
- `KeepRawBody`：非路径字段作 query，requestBody 固定 `{"content":{"*/*":{"schema":{}}}}`，不 required；GET/DELETE/HEAD 上无效果。

### 路径参数

- 所有方法都声明 `in: path`、`required: true`。
- `{name?}`/`{name...}` 转为 OpenAPI `{name}`；可选/通配语义写入 description。
- 必选 `{name}` 不展示 default（值必被覆盖）；可选/通配零段保留 default 并展示。
- `ignore` 不抑制 path 声明；字段仍从 body/query schema 排除。

## 八、响应

- 统一包装为 `Response_<Type>`，`data` 指向 `<Type>Response`；成功固定 200。
- `OpenAPIInfo.ResponseWrapper` 为 nil 时用默认 `{data, code, message}`；非 nil 必须是 struct/`*struct`，导出字段必须有非空 json 名或 `json:"-"`，且恰有一个 interface kind 占位符；否则 panic。
- 自动响应 schema 只描述默认 JSON 响应（`[]byte` base64）；自定义 `OnResponse` 输出文件/流/Protobuf 时不再适用。

## 九、default 展示

- 只在请求 schema 展示；响应不展示。
- 只有可解析的 default 才展示；非法值（`int:"abc"`、`bool:"yes"`、`NaN`、切片含非法元素）不展示。
- 指针字段：所属 struct 可被 `applyDefaults` 到达时展示（`reachedByDefaults`）。
- 值类型字段：所属 struct 被值嵌套可达时展示（`reachedViaValue`）；指针嵌套/多层容器中的值字段不展示。
- 同一类型多处使用时按类型取并集；多层容器路径不改变单层/值嵌套路径的展示。
- 单层容器及外包一层指针的 struct 可被 `applyDefaults` 到达；多层容器不穿透。
- 切片 default 按逗号拆分；Trim 后为空视为 `[]`；`[]byte` 请求 default 展示为字节数组。

## 十、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| 生成入口、三遍遍历 | `openapi.go` | `openapi_test.go` |
| 类型映射、decorate | `openapi.go` | `openapi_test.go` |
| required、requestBody | `openapi.go` | `openapi_test.go` |
| 响应包装、确定性 | `openapi.go` | `openapi_test.go` |

继续阅读：[reference.md](reference.md)（标签/类型/required 速查）、[internals.md](internals.md)（生成器实现）、[request.md](request.md)（Req/Res 与标签）。
