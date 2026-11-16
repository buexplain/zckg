# Req/Res 与结构体标签

本文说明 handler 的 Req/Res 形态、匿名嵌入展开，以及各标签在 Req/Res 上的作用边界。完整标签表、类型矩阵与默认值/错误规则见 [reference.md](reference.md)；绑定细节见 [parameter-binding.md](parameter-binding.md)，校验见 [parameter-validate.md](parameter-validate.md)。

适合：定义接口入参与返回值的业务开发者。

## 一、handler 签名

```go
type Handler func(ctx context.Context, req Req) (Res, error)
```

- `Req` 可为 struct 或 `*struct`；`Res` 同理。
- 引擎在绑定/校验阶段始终用 `*Req` 指针操作底层结构体，最后按 handler 声明类型传值或传指针。
- 普通未导出字段跳过，不参与绑定/校验；未导出匿名值嵌入会展开，未导出匿名指针嵌入跳过（见下）。
- 签名不合法在注册期 panic，不会等到运行时。

```go
type CreateUserReq struct {
    Name string `json:"name" nonzero:"true"`
    Age  *int   `json:"age"`
}

type CreateUserRes struct{}

func CreateUser(ctx context.Context, req CreateUserReq) (CreateUserRes, error) {
    return CreateUserRes{}, nil
}
```

## 二、两套来源投影

同一个 Req 字段最多有两套独立语义：

| 标签 | Form 链（query/urlencoded/multipart/path） | JSON body |
| --- | --- | --- |
| 无标签 | 字段名 | 字段名 |
| `form:"q"` | `q` | 字段名或 json 名 |
| `form:"-"` | 不参与 | 按 json 标签/字段名参与 |
| `json:"x"` | `x`（普通 json 名 fallback） | `x` |
| `json:"-"` | 回退字段名 | 不参与 |
| `json:"-,"` | 回退字段名 | 键名 `-` |

- Form 链优先级：`form` > 普通 `json` 名 > 字段名。
- JSON 完全按 `encoding/json`，`form` 标签无效。
- Path 参数复用 Form 链绑定名。
- `form:"-"` 只跳过 Form 链；`json:"-"` 只跳过 JSON。

## 三、匿名嵌入

- **query / form / path**：匿名嵌入（值或 `*struct`）展开为顶层字段，按子字段绑定名直接绑定。
- **JSON**：按 `encoding/json`——无 json 名的匿名嵌入展开到父级；带 json 名的匿名嵌入作为父级嵌套对象。
- 嵌入指针在注册期由 `fieldByIndex` 物化，内部 default 与顶层一样在注册期填充；内部 `nonzero` 没有“父字段判零”环节，等价于顶层必填。
- 未导出匿名指针嵌入（`type Req struct { *base }`）不展开：query/form 不绑定、不填默认值、不做 nonzero、OpenAPI 不输出；JSON 只有在 Req 还有其他 JSON 可绑定字段、body 被解码时才可能报 `json: cannot set embedded pointer to unexported struct`（400）。只含该嵌入的 Req 不读取 body，JSON 内容不会被绑定。
- 展开后的字段与顶层字段在 Form 链/默认值/校验上等价；JSON 仍按 json 标签规则。

```go
type Base struct {
    Name string `json:"name" nonzero:"true"`
    Page int    `json:"page" default:"1"`
}

type Req struct {
    *Base
    Note string `json:"note"`
}
// query/form: name / page / note 都是顶层绑定名
// JSON: name / page / note 同样展开（Base 无 json 名）
```

## 四、标签速查

完整表与优先级见 [reference.md](reference.md) 的“标签总览”。这里只列用途：

| 标签 | 作用 | 生效侧 |
| --- | --- | --- |
| `json` | Form 链 fallback / JSON 键名 / 响应序列化名 | Req + Res |
| `form` | Form 链绑定名，优先于 json | Req |
| `default` | 默认值（两阶段填充） | Req |
| `nonzero` | 零值校验 + required 推断 | Req |
| `time_format` | 表单链路时间解析格式 | Req |
| `time_location` | 表单链路时间解析时区 | Req |
| `ignore` | 从 OpenAPI schema/query 排除 | Req + Res |
| `example` | OpenAPI 示例值 | Req + Res |
| `description` | OpenAPI 描述 | Req + Res |
| `deprecated` | OpenAPI 废弃标记 | Req + Res |
| `OpenAPIMeta` 上的 `tags`/`summary`/`description`/`deprecated` | 操作级元信息 | Req |
| `KeepRawBody`（嵌入） | 不读取请求体 | Req |

写在 Res 上的 `form`/`default`/`nonzero`/`time_format`/`time_location` 无任何效果；`Res` 没有专属标签。

## 五、默认值

- 仅对标量（及其切片/指针）生效；值必须能被运行时解析，否则注册期告警并按未设置处理。
- 两阶段：注册期填模板中“注册期可达”的零值字段；请求期只补 nil 指针。
- 顶层字段、值类型 struct 链、固定数组元素在注册期可预填；nil 指针/切片/map 中的字段要等请求期创建元素后补 nil 指针。
- 嵌套指针 struct 的值类型字段 default 不生效；指针字段在请求阶段可补。
- 切片/map 元素的值类型字段 default 不生效；指针字段请求阶段补。
- 显式请求值优先于 default；显式零值不会被请求阶段覆盖；显式 JSON `null` 对指针会置 nil（有 default 时请求阶段回填），对切片/map 会置 nil 且不恢复默认值。

示例与容器深度矩阵见 [parameter-binding.md](parameter-binding.md) 的“默认值机制”，完整规则见 [reference.md](reference.md)。

## 六、时间标签

- `time_format`/`time_location` 只作用于 query/form/multipart/path。
- JSON 请求体与响应由 `encoding/json` 处理：`time.Time` 只接受/输出 RFC3339 字符串，标签不参与。
- 支持 `time.Time`、其多级指针、`[]time.Time`、`[]*time.Time`；其他容器不参与标量绑定。
- 未设置 `time_format` 时自动探测：数字按位数推断时间戳精度，非数字尝试常见布局。
- `time_location` 默认 `time.Local`；解析失败降级并告警。

```go
type Req struct {
    CreatedAt time.Time `json:"created_at" time_format:"2006-01-02"`
    Timestamp time.Time `json:"timestamp" time_format:"unix"`
}
```

## 七、nonzero

- `nonzero:"true"` 校验零值；`"false"`/未标注不校验。
- 递归进入嵌套 struct/指针/单层容器；固定数组即使全零也逐元素；nil/空切片、nil map 无元素；多层容器不穿透。
- 与 default 独立：校验看默认值填充后的值；OpenAPI 只有能解析且非零的 default 才能取消 required。
- 错误路径用 Form 链绑定名；切片/数组不附加索引；map 追加 key。

详见 [parameter-validate.md](parameter-validate.md)。

## 八、文档标签与 OpenAPIMeta

```go
type ListUserReq struct {
    zchttp.OpenAPIMeta `tags:"User/Account" summary:"用户列表" description:"分页查询用户"`

    Keyword string `json:"keyword" description:"搜索关键字" example:"go"`
    Page    int    `json:"page" default:"1" description:"页码"`
    Secret  string `json:"secret" ignore:"true"` // 文档不展示，仍参与绑定
}
```

- `ignore:"true"` 从请求/响应 schema 与 query 排除；path 声明仍保留。
- `example` 按字段 schema 类型转换；`description`/`deprecated` 只影响文档。
- `OpenAPIMeta` 只能嵌入 Req（顶层）；标签 `tags`/`summary`/`description`/`deprecated` 生成操作级元信息；写在 Res 上会告警且无效。
- 值类型嵌套 struct 的 `deprecated` 会作为 `$ref` 兄弟键写入，但 OpenAPI 3.0 工具忽略；需要生效用指针嵌套。

## 九、综合示例

```go
type CreateUserReq struct {
    zchttp.OpenAPIMeta `tags:"User Management" summary:"创建用户" description:"创建一个新的用户账户"`

    Name     string    `json:"name" nonzero:"true" description:"用户姓名" example:"张三"`
    Email    string    `json:"email" nonzero:"true" description:"邮箱地址" example:"user@example.com"`
    Age      *int      `json:"age" description:"年龄" example:"25"`
    Status   string    `json:"status" default:"active" description:"账户状态"`
    Tags     []string  `json:"tags" default:"new" description:"用户标签"`
    Birthday time.Time `json:"birthday" time_format:"2006-01-02" time_location:"Asia/Shanghai" description:"生日"`
    Internal string    `json:"internal" ignore:"true"` // 不参与文档
}

type CreateUserRes struct {
    ID int64 `json:"id"`
}
```

## 十、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| 字段元信息、来源投影、匿名嵌入、重复名 | `meta.go` | `meta_test.go` |
| 默认值两阶段 | `defaults.go` | `defaults_test.go` |
| 时间解析 | `binding.go` | `binding_test.go` |
| nonzero/Validate | `validate.go` | `validate_test.go` |
| 标签的 OpenAPI 映射 | `openapi.go` | `openapi_test.go` |

继续阅读：[reference.md](reference.md)（完整标签/类型/默认值/错误）、[parameter-binding.md](parameter-binding.md)（绑定）、[parameter-validate.md](parameter-validate.md)（校验）、[openapi.md](openapi.md)（文档生成）。
