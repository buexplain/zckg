# 请求结构体（Req）定义与标签

本文档介绍 handler 的请求结构体（`Req`）如何定义、支持哪些标签。参数绑定的来源与类型转换细节见 `parameter-binding.md`；校验规则见 `parameter-validate.md`。相关实现位于 `binding.go`（绑定）、`meta.go`（标签解析）与 `validate.go`（校验）。

## 一、handler 签名与 Req 形态

handler 的签名固定为：

```go
func(ctx context.Context, req Req) (Res, error)
```

- `Req` 既可以是结构体值类型，也可以是结构体指针类型（`*T`）；两者均受支持。
- 引擎在绑定阶段始终使用指向底层结构体的指针（`*T`）进行赋值与校验，再按 handler 声明类型决定传入值还是指针。
- 未导出字段（小写开头）会被跳过，不参与绑定与校验。

```go
type CreateUserReq struct {
    Name string `json:"name" nonzero:"true"`
    Age  *int   `json:"age"`
}

func CreateUser(ctx context.Context, req CreateUserReq) (CreateUserRes, error) { ... }
// 或指针形态：func CreateUser(ctx context.Context, req *CreateUserReq) (...)
```

## 二、结构体标签总览

| 标签 | 作用 | 说明 |
| --- | --- | --- |
| `json` | 绑定名 / 序列化名 | 绑定名解析优先级：`form` > `json` > 字段名（取逗号前部分） |
| `form` | 绑定名 | 优先级高于 `json`，常用于 query / 表单 |
| `default` | 默认值 | 仅对标量类型（及其切片/指针）生效；注册阶段预填充到模板，详见 `parameter-binding.md` |
| `time_format` | 时间解析格式 | `unix`/`unixmilli`/… 或 Go layout，详见 `parameter-binding.md` |
| `time_location` | 时间解析时区 | 如 `Asia/Shanghai`，默认 `time.Local`；解析失败降级并输出 `slog.Warn` |
| `nonzero` | 非零值校验 | `"true"` 校验非零值、`"false"` 不校验；详见 `parameter-validate.md` |
| `ignore` | 文档排除 | `ignore:"true"` 仅将字段从 OpenAPI 文档中排除，不影响绑定与校验 |
| `example` / `description` | 文档信息 | 仅用于 OpenAPI 文档生成，详见 `openapi.md` |

此外，可在 `Req` 中嵌入 `zchttp.OpenAPIMeta` 声明操作级元信息（`tags`/`summary`/`description`），它是空结构体，不参与绑定与校验，详见 `openapi.md`。

## 三、标签示例

### `json` — 绑定名 / 序列化名

绑定名解析优先级：`form` > `json` > 字段名（取逗号前部分）。

```go
type Req struct {
    UserName string `json:"user_name"` // 绑定名为 "user_name"
    Email    string `json:"email,omitempty"` // 绑定名为 "email"（取逗号前部分）
}
```

### `form` — 绑定名（优先级高于 `json`）

常用于 query 参数或表单字段，当 `form` 与 `json` 同时存在时，`form` 优先。

```go
type Req struct {
    Keyword string `json:"keyword" form:"q"` // 绑定名为 "q"（form 优先）
    Page    int    `json:"page" form:"page"` // 绑定名为 "page"
}
```

### `default` — 默认值

为字段设置默认值，仅对标量类型（及其切片/指针）生效。默认值的填充分**两个阶段**：

| 阶段 | 时机 | 填充范围 | 说明 |
| --- | --- | --- | --- |
| 注册阶段 | 路由注册时 | 所有零值字段 | 预填充到模板，作为每个请求的初始值 |
| 请求阶段 | 参数绑定后 | 仅 nil 指针字段 | 补填绑定过程中动态创建的子元素中的默认值，值类型跳过以避免覆盖用户显式传入的零值 |

#### 顶层字段

顶层 Req 中的所有字段（无论值类型还是指针类型）均在**注册阶段**被填充到模板，请求时通过模板浅拷贝继承：

```go
type ListReq struct {
    Page     int      `json:"page" default:"1"`          // 值类型：注册阶段填充 → 请求未传时为 1
    Keyword  string   `json:"keyword" default:"all"`     // 值类型：注册阶段填充 → 请求未传时为 "all"
    Sort     *string  `json:"sort" default:"created_at"` // 指针类型：注册阶段填充 → 请求未传时为 "created_at"
    Tags     []string `json:"tags" default:"go,web"`     // 切片：逗号分隔，注册阶段填充
}
```

#### 嵌套值结构（值嵌套 struct）

值嵌套的 struct 在注册阶段已存在（非 nil），其内部字段可被注册阶段到达并填充：

```go
type Address struct {
    City    string  `json:"city" default:"Beijing"`     // ✅ 值类型：注册阶段可到达 → 填充生效
    Zip     *string `json:"zip" default:"100000"`       // ✅ 指针类型：注册阶段递归进入 Address 后填充 → 填充生效
}

type CreateUserReq struct {
    Name    string  `json:"name" nonzero:"true"`
    Address Address `json:"address"`                    // 值嵌套：注册阶段 Address 已存在，递归可达
}
// City 未传 → "Beijing"，Zip 未传 → "100000"（均在注册阶段填充到模板）
```

#### 嵌套指针结构体（`*Struct`）

指针嵌套的 struct 在注册阶段为 nil，其内部的**值类型字段**无法被注册阶段到达，default 不生效；**指针类型字段**在请求阶段绑定创建子结构体后可被补填：

```go
type Company struct {
    Name    string  `json:"name" default:"Acme"`
    Country *string `json:"country" default:"CN"`
}

type CreateUserReq struct {
    Name    string   `json:"name" nonzero:"true"`
    Company *Company `json:"company"`                   // 指针嵌套：注册阶段为 nil
}
// Company 未传 → Company 整体为 nil，Name/Country 均无默认值
// Company 传了 {} → Name 为空字符串（default 不生效），Country 为 "CN"（指针补填生效）
//
// Name 为何不生效：请求阶段动态创建的结构体中，只有指针字段才能区分客户端是否传递（nil = 未传，非 nil = 已传），
// 值类型字段无法区分“未传”与“显式传零值”，故请求阶段不填充值类型字段。
```

#### 切片 / Map 中的结构体元素

切片和 Map 的元素是绑定阶段动态创建的，行为与指针嵌套类似——只有指针类型字段的 default 能在请求阶段补填：

```go
type Item struct {
    Qty    *int    `json:"qty" default:"1"`             // ✅ 指针类型：请求阶段补填 → 生效
    Status *string `json:"status" default:"active"`     // ✅ 指针类型：请求阶段补填 → 生效
    Note   string  `json:"note" default:"none"`         // ❌ 值类型：动态创建的元素无法被注册阶段到达 → 不生效，在请求阶段无法区分零值 → 不生效
}

type OrderReq struct {
    OrderNo string             `json:"orderNo" nonzero:"true"`
    Items   []Item             `json:"items"`           // 值元素切片
    Extras  map[string]*Item   `json:"extras"`          // 指针元素 map
}
// Items[0] 传 {"qty":null,"status":null} → Qty=1, Status="active"（nil 指针被补填）
// Items[0] 传 {} → Note 仍为空字符串（值类型 default 不生效）
```

#### 不支持的类型

以下类型的 `default` 标签值会被忽略（注册时会输出 `slog.Warn`）：`struct`、`*struct`、`[]struct`、`[]*struct`、`map[K]V`、`*map[K]V`、`time.Time`、任意非标量的指针/切片/数组。

#### 容器嵌套深度限制

框架对**默认值填充**、**nonzero 校验**、**OpenAPI 文档生成**的支持范围不同，以下按容器类型分类说明：

##### 支持的单层容器

| 容器类型 | 默认值填充 | nonzero 校验 | Schema 结构 | default 展示 | description/example | required 计算 |
| --- | --- | --- | --- | --- | --- | --- |
| `[]Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个元素 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `[]*Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个元素 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `map[K]Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个值 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `map[K]*Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个值 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `[N]Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个元素 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `[N]*Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个元素 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |

> 固定长度数组与切片行为一致：默认值填充、nonzero 校验、OpenAPI 展示三者在数组元素上的判定规则与切片完全相同。

##### 不支持的多层容器

| 容器类型 | 默认值填充 | nonzero 校验 | Schema 结构 | default 展示 | description/example | required 计算 |
| --- | --- | --- | --- | --- | --- | --- |
| `[][]Struct` | ❌ 无法穿透第二层切片 | ❌ 无法穿透第二层切片 | ✅ | ❌ 不展示¹ | ✅ | ✅（但不校验） |
| `[]map[K]Struct` | ❌ 无法穿透切片+map | ❌ 无法穿透切片+map | ✅ | ❌ 不展示¹ | ✅ | ✅（但不校验） |
| `map[K][]Struct` | ❌ 无法穿透 map+切片 | ❌ 无法穿透 map+切片 | ✅ | ❌ 不展示¹ | ✅ | ✅（但不校验） |
| `map[K][]*Struct` | ❌ 无法穿透 map+切片 | ❌ 无法穿透 map+切片 | ✅ | ❌ 不展示¹ | ✅ | ✅（但不校验） |
| `map[K][N]Struct` | ❌ 无法穿透 map+数组 | ❌ 无法穿透 map+数组 | ✅ | ❌ 不展示¹ | ✅ | ✅（但不校验） |
| `map[K]map[K]Struct` | ❌ 无法穿透 map+map | ❌ 无法穿透 map+map | ✅ | ❌ 不展示¹ | ✅ | ✅（但不校验） |

> ¹ 若该结构体类型同时被用在单层容器中，则 `default` 会展示（类型可达性是按类型标记的，非按路径）

##### 指针包裹容器（与对应单层容器行为一致）

| 容器类型 | 默认值填充 | nonzero 校验 | Schema 结构 | default 展示 | description/example | required 计算 |
| --- | --- | --- | --- | --- | --- | --- |
| `*[]Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个元素 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `*[N]Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个元素 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |
| `*map[K]Struct` | ✅ 指针字段请求阶段补填；❌ 值字段不生效 | ✅ 递归校验每个值 | ✅ | ✅ 仅指针字段 | ✅ | ✅ |

> 指针本身不阻断可达性：`applyDefaults`、`validateNonzero` 与 OpenAPI 可达性分析均会穿透非 nil 指针，因此 `*[]Struct` 与 `[]Struct`、`*[N]Struct` 与 `[N]Struct`、`*map[K]Struct` 与 `map[K]Struct` 的行为完全一致。

##### 示例：多层容器的行为差异

```go
// 单层容器中的结构体
type SingleItem struct {
    Name     string `json:"name" nonzero:"true"`              // 必填字段
    IsActive *bool  `json:"isActive" default:"true"`          // 指针字段有默认值
}

// 多层容器中的结构体（与 SingleItem 字段相同，但仅用于多层容器）
type MultiItem struct {
    Name     string `json:"name" nonzero:"true"`              // 必填字段
    IsActive *bool  `json:"isActive" default:"true"`          // 指针字段有默认值
}

type Req struct {
    // ✅ 单层容器：指针字段默认值生效，nonzero 校验生效
    Items    []SingleItem           `json:"items"`             // 指针 default✅ nonzero✅
    Extras   map[string]*SingleItem `json:"extras"`            // 指针 default✅ nonzero✅
    
    // ❌ 多层容器：默认值和校验均不生效
    DeepMap  map[string][]MultiItem `json:"deepMap"`           // 指针 default❌ nonzero❌
    Nested   [][]MultiItem          `json:"nested"`            // 指针 default❌ nonzero❌
}
```

**行为说明：**

- `Items[0].Name`（`SingleItem`）：nonzero 校验生效，空字符串会报错
- `Items[0].IsActive`（`SingleItem`）：未传时自动填充 `true`（请求阶段补填 nil 指针）
- `SingleItem` 的值类型字段（如 `Name`）即使带 `default` 也**不生效**（切片元素的值类型字段无法被注册阶段到达，请求阶段也不填充值类型）
- `DeepMap["key"][0].Name`（`MultiItem`）：nonzero 校验**不生效**，空字符串不会报错（框架无法穿透 `map[K][]Struct`）
- `DeepMap["key"][0].IsActive`（`MultiItem`）：未传时**不会**自动填充 `true`（框架无法穿透 `map[K][]Struct`）
- OpenAPI 文档中 `SingleItem` 的 `IsActive` 的 `default` 值**展示**；`MultiItem` 的 `IsActive` 的 `default` 值**不展示**（因为框架知道该默认值无法生效）

##### 启动期警告

当检测到多层容器中的字段带有 `default` 标签时，框架会在启动时输出警告日志：

```
WARN default tag on pointer field in non-defaults-reachable struct, never applied
  route=POST /api/create
  handler=main.CreateHandler
  struct=MultiItem
  field=IsActive
  type=*bool
```

该警告帮助开发者及时发现 `default` 标签配置错误（虽然 JSON 反序列化不受影响，但框架增强功能无法生效）。

### `time_format` — 时间解析格式

指定 `time.Time` 字段的解析格式。支持 `unix`/`unixmilli`/`unixmicro`/`unixnano` 或 Go layout 字符串。

```go
type Req struct {
    CreatedAt time.Time `json:"created_at" time_format:"2006-01-02"`          // Go layout
    Timestamp time.Time `json:"timestamp" time_format:"unix"`                 // Unix 秒级时间戳
    Milli     time.Time `json:"milli" time_format:"unixmilli"`               // Unix 毫秒时间戳
}
```

### `time_location` — 时间解析时区

指定时间解析的时区，默认 `time.Local`。解析失败时降级为 `time.Local` 并输出 `slog.Warn`。

```go
type Req struct {
    MeetingTime time.Time `json:"meeting_time" time_format:"2006-01-02 15:04" time_location:"Asia/Shanghai"`
}
```

### `nonzero` — 非零值校验

标记字段是否必须为非零值。`"true"` 表示校验，`"false"` 或不标注表示不校验。校验的详细规则（递归行为、与 `default` 的关系等）见 `parameter-validate.md`。

```go
type Req struct {
    Name   string `json:"name" nonzero:"true"`   // 必填：空字符串会报错
    Age    int    `json:"age" nonzero:"false"`   // 不校验：传 0 不报错
    Email  string `json:"email"`                 // 不校验：未标注等同于 nonzero:"false"
}
```

### `ignore` — 文档排除

`ignore:"true"` 将字段从 OpenAPI 文档中排除，不影响绑定与校验。

```go
type Req struct {
    Name   string `json:"name" nonzero:"true"`
    Secret string `json:"secret" ignore:"true"` // 文档中不展示，但绑定与校验正常
}
```

### `example` / `description` — 文档信息

仅用于 OpenAPI 文档生成。`example` 按字段类型自动转换为对应的 JSON 类型。

```go
type Req struct {
    Name  string `json:"name" nonzero:"true" description:"用户姓名" example:"张三"`
    Age   int    `json:"age" description:"年龄" example:"25"`
    Active bool  `json:"active" description:"是否激活" example:"true"`
}
```

### `OpenAPIMeta` 嵌入 — 操作级元信息

在 `Req` 中嵌入 `zchttp.OpenAPIMeta`，声明操作标签、摘要、描述。

```go
type ListUserReq struct {
    zchttp.OpenAPIMeta `tags:"User/Account" summary:"用户列表" description:"分页查询用户"`
    Keyword string `json:"keyword" default:"" description:"搜索关键字"`
    Page    int    `json:"page" default:"1"`
}
```

## 四、综合示例

```go
type CreateUserReq struct {
    zchttp.OpenAPIMeta `tags:"User Management" summary:"创建用户" description:"创建一个新的用户账户"`

    Name      string    `json:"name" nonzero:"true" description:"用户姓名" example:"张三"`
    Email     string    `json:"email" nonzero:"true" description:"邮箱地址" example:"user@example.com"`
    Age       *int      `json:"age" description:"年龄" example:"25"`
    Status    string    `json:"status" default:"active" description:"账户状态"`
    Tags      []string  `json:"tags" default:"new" description:"用户标签"`
    Birthday  time.Time `json:"birthday" time_format:"2006-01-02" time_location:"Asia/Shanghai" description:"生日"`
    Internal  string    `json:"internal" ignore:"true"` // 不参与文档
}
```

## 五、已知边界

- **中间件无法替换下游的 `http.ResponseWriter`**：`MiddlewareHandler` 的 `next` 无参且链上的 `w` 在请求开始时已固定，gzip 等需要包装下游 `ResponseWriter` 的中间件模式无法实现。这是中间件签名的架构决策；若需支持，需修改 `NextFunc` 签名（破坏性变更），当前版本不支持。详见 `middleware.md`。

## 六、相关文档

- 参数绑定来源、字段类型、`time.Time` 解析、`default` 机制、文件上传：`parameter-binding.md`
- 校验规则（`nonzero` 递归校验、`Validate()` 自定义校验、错误处理）：`parameter-validate.md`
- OpenAPI 文档生成、`OpenAPIMeta`、字段级文档标签：`openapi.md`
- 回调机制与错误分发、自定义响应/错误/校验失败处理：`http-engine-callback.md`
