# 参数绑定规则

本框架会根据请求的方法（method）与内容类型（Content-Type），自动将请求参数绑定到 handler 的 `Req` 结构体。绑定相关实现位于 `binding.go`，校验相关实现位于 `validate.go`。

绑定与校验分为两个独立函数：
- `bindRequestData(r, reqPtr, meta, multipartMaxMemory)`：仅执行数据绑定（query/body），在路由命中后立即调用（`multipartMaxMemory` 为 multipart 解析的内存缓冲上限，取自引擎的 `MultipartFormMaxMemory`）。
- `bindPathParams(reqPtr, params, values)`：仅执行路由路径参数绑定，在 `bindRequestData` 之后调用（仅参数路由触发，路径参数覆盖同名 query/body 值）。
- `validateRequest(reqPtr, meta, needsNonzero)`：仅执行参数校验（nonzero + Validator），在洋葱模型 core 层调用；`needsNonzero` 为注册期预计算的传递性标记，全树无 nonzero 字段时跳过遍历（详见 `parameter-validate.md`）。

> 默认值（`default` 标签）采用两阶段填充：注册阶段预填模板零值字段，请求阶段绑定后补填动态创建的子元素中的 nil 指针字段。详见[默认值机制](#六默认值机制)。

## 一、绑定来源：按 method 与 Content-Type 分流

### 1. GET / DELETE / HEAD

绑定 URL query 参数（`r.URL.Query()`）。

```
GET /search?keyword=go&page=3
```

### 2. POST / PUT / PATCH 等带请求体的方法

采用**合并绑定**：先绑定 URL query 参数，再按 `Content-Type` 绑定请求体，**body 中出现的字段覆盖 query 已绑定的同名字段**，body 中缺失的字段保留 query 绑定值（兼容 REST 常见的"query 放控制参数 + body 放资源数据"混合传参风格，如 `POST /orders/batch?dryRun=true` + JSON 资源体）。

> ⚠️ **行为变更提示（breaking change）**：早期版本中这类方法仅绑定 body、query 参数被静默忽略；当前版本 query 参数会生效。若你的接口依赖"query 被忽略"的旧行为（如同名参数刻意只认 body），升级后需确认 query 传参不会意外影响字段值。

请求体按 `Content-Type` 选择绑定方式：

| Content-Type | 绑定方式 |
| --- | --- |
| `application/json` | `json.Decode` 解析请求体 |
| `application/x-www-form-urlencoded` | `r.ParseForm()` 后绑定 `r.PostForm` |
| `multipart/form-data` | `r.ParseMultipartForm(MultipartFormMaxMemory)` 后绑定表单字段与上传文件 |
| 其他（有请求体时） | 回退按 JSON 解析 |

**body 覆盖 query 的粒度由解码器语义决定**（以 JSON 为例）：

| body 中的情况 | 对同名字段的影响 |
| --- | --- |
| 显式出现（含显式零值，如 `"page": 0`、`"source": ""`） | 覆盖 query 值 |
| `null`，目标字段为非指针类型 | JSON 解码 no-op，**保留 query 值** |
| `null`，目标字段为指针类型 | 标准库语义置 `nil`，覆盖 query 值 |
| 未出现 | 保留 query 值 |

表单（`x-www-form-urlencoded` / `multipart`）同理：body 中出现的字段覆盖同名 query 值，未出现则保留。

> `Content-Type` 会先去除 `; charset=utf-8` 等参数部分，仅保留主类型再匹配。
>
> `multipart/form-data` 的内存缓冲上限由引擎字段 `MultipartFormMaxMemory` 定义，`NewEngine` 默认 **32 MB**，超出部分由标准库写入临时文件；按需调整示例：`engine.MultipartFormMaxMemory = 64 << 20`。

### 2.1 请求体大小限制（MaxBodyBytes）

所有带请求体的方法在绑定前统一受引擎字段 `MaxBodyBytes` 限制（`http.MaxBytesReader` 包裹 `r.Body`，对 JSON/表单/multipart 等所有 Content-Type 生效）：

| 配置 | 行为 |
| --- | --- |
| `NewEngine()` 默认 **32 MB** | 超限请求在绑定阶段失败，映射为 `BindingError` 返回 **400** |
| 显式调大（如 `engine.MaxBodyBytes = 64 << 20`） | 适用于合法的大请求体场景 |
| 显式置 `0` | 不限制（需自行评估超大请求体导致的内存/磁盘 DoS 风险） |

两个字段的关系：`MaxBodyBytes` 是请求体的**整体入口上限**（先于任何解析生效）；`MultipartFormMaxMemory` 仅控制 multipart 解析时的**内存缓冲上限**（超出部分写入临时文件，仍受 `MaxBodyBytes` 总量约束）。需要大文件上传时两者需同步调大。

```go
engine := zchttp.NewEngine()
engine.MaxBodyBytes = 64 << 20          // 请求体整体上限 64 MB
engine.MultipartFormMaxMemory = 32 << 20 // multipart 内存缓冲上限，超出落盘
```

### 3. 路由路径参数（所有 method）

若路由注册时使用了 `{name}` / `{name?}` 路径参数（详见 `routing.md` 中“路由参数”章节），参数值在 query/body 绑定**之后**由 `bindPathParams` 写入 Req，规则如下：

- **参数名即字段绑定名**：按 form > json > 字段名优先级解析，注册阶段预计算绑定关系，参数名无对应字段时注册即 panic。
- **覆盖语义**：三级覆盖链 **path > body > query**（路径参数是更精确的意图，优先级最高；带 body 方法内部为 body > query，见上节）。
- **类型由字段声明决定**：复用 `setScalar` 转换，支持 string/bool/int 全系/uint 全系/float/指针与 `time.Time`（`time_format`/`time_location` 标签同样生效）。
- **失败语义区别于尽力绑定**：单个参数转换失败立即返回错误（包装为 `BindingError` 返回 400），而非跳过。
- **可选参数省略**：`{name?}` 未出现在请求路径中时不写入字段，保留模板 `default` 值或零值。

## 二、字段名解析规则

字段绑定名按以下优先级解析（取标签逗号前的名称部分，如 `name,omitempty` → `name`）：

1. `form` 标签
2. `json` 标签
3. 结构体字段名

字段名为空或为 `-` 时跳过该字段；未导出字段（小写开头）跳过。

```go
type Req struct {
    Keyword string `json:"keyword"`              // 绑定名 keyword
    Page    int    `form:"p" json:"page"`        // 绑定名 p（form 优先）
    ignore  string                               // 未导出，跳过
}
```

## 三、支持的字段类型

| 类型 | 说明 |
| --- | --- |
| `string` | 直接赋值 |
| `bool` | `strconv.ParseBool` |
| `int` / `int8` ~ `int64` | `strconv.ParseInt` |
| `uint` / `uint8` ~ `uint64` | `strconv.ParseUint` |
| `float32` / `float64` | `strconv.ParseFloat` |
| 上述类型的指针 | 自动分配后赋值 |
| 上述类型的切片 | 绑定同名的多个值 |
| `map[K]V` | 仅 JSON 路径支持（`json.Decoder` 原生反序列化），query/form 路径跳过 map 字段 |
| `time.Time` | 见下文时间解析 |
| `*multipart.FileHeader` | 单文件上传 |
| `[]*multipart.FileHeader` | 多文件上传 |
| 嵌套结构体 | 仅 JSON 路径支持（`json.Decoder` 递归反序列化），query/form 路径仅处理扁平字段 |

> **尽力绑定策略**：单个字段类型转换失败时会跳过该字段（保持零值），不会中断整个请求。

### map 类型绑定

map 类型字段的绑定行为因来源而异：

- **JSON 绑定**：`json.Decoder` 原生支持，可直接反序列化为 `map[string]any`、`map[string]string`、`map[string]int`、`map[string]*Struct` 等任意 key/value 组合。
- **query / form 绑定**：`bindValues` 遍历 `structMeta.fields` 调用 `setFieldValue` → `setScalar`，map 类型不在 switch case 中，被静默跳过，保持默认值（nil）。

因此，若 handler 的 Req 包含 map 字段，应确保使用 JSON 请求体（`Content-Type: application/json`）。

### 切片绑定

同名参数出现多次时绑定为切片：

```
GET /search?tags=a&tags=b&tags=c   →   Tags []string{"a","b","c"}
```

## 四、time.Time 解析

`time.Time` 字段通过 `time_format` 与 `time_location` 标签控制解析方式。

### time_format 标签

| 取值 | 含义 |
| --- | --- |
| `unix` | 秒级时间戳 |
| `unixmilli` | 毫秒级时间戳 |
| `unixmicro` | 微秒级时间戳 |
| `unixnano` | 纳秒级时间戳 |
| 其他非空值 | 作为 Go layout 解析，支持任意排列（如 `02/01/2006`） |
| 空（不设置） | 自动探测（见下） |

### 自动探测（未设置 time_format）

- **纯数字**：视为时间戳，按位数推断精度——`10` 位=秒、`13`=毫秒、`16`=微秒、`19`=纳秒，其余按秒。
- **非数字**：依次尝试常见布局 `defaultTimeLayouts`：
    - `RFC3339Nano`、`RFC3339`
    - `2006-01-02 15:04:05`、`2006-01-02T15:04:05`
    - `2006-01-02`
    - `2006/01/02 15:04:05`、`2006/01/02`
    - `15:04:05`

### time_location 标签

指定解析时区（如 `Asia/Shanghai`），默认 `time.Local`。时区解析失败时降级为 `time.Local` 并输出 `slog.Warn`。

### 示例

```go
type Req struct {
    StartTime time.Time `json:"start_time" time_format:"unix"`
    Date      time.Time `json:"date" time_format:"2006-01-02" time_location:"Asia/Shanghai"`
    SlashDate time.Time `json:"slash_date" time_format:"02/01/2006"`
    Auto      time.Time `json:"auto"`  // 自动探测 RFC3339 或时间戳
}
```

## 五、文件上传

在结构体中直接声明文件字段即可，无需额外 API：

```go
type UploadReq struct {
    Title string                  `json:"title"`  // 普通表单字段
    File  *multipart.FileHeader   `json:"file"`   // 单文件
    Files []*multipart.FileHeader `json:"files"`  // 多文件
}
```

handler 中通过标准库使用文件：

```go
func upload(ctx context.Context, req UploadReq) (Res, error) {
    if req.File != nil {
        f, err := req.File.Open()   // 打开文件流
        // req.File.Filename, req.File.Size ...
    }
    return Res{}, nil
}
```

## 六、默认值机制

通过 `default` 标签为字段设置默认值。框架采用**两阶段填充**策略，在注册阶段和请求阶段各执行一次 `applyDefaults`，保证不同嵌套场景下默认值均正确生效。

### default 标签支持范围

`default` 标签仅在以下类型上生效（`isDefaultSupported` 返回 true，`hasDefault` 置为 true）：

- 标量：`string`、`bool`、`int/uint` 全系列、`float32`/`float64`
- 标量指针：`*string`、`*int`、`*bool` 等
- 标量切片：`[]string`、`[]int`、`[]*int` 等

> ⚠️ **误用检测为告警级（非阻断）**：`default` 写在上述范围之外（如 `time.Time`、`map`、`struct`）或写在请求阶段永不可达的路径上时，注册期仅输出 `slog.Warn` 告警（含路由与 handler 位置），**不会 panic 或拒绝注册**，误用字段将"永不填充默认值"。生产环境若将日志级别调至 Error 则该提示不可见，请保持启动期日志可见并及时关注 Warn 输出。

### 两阶段填充规则

| 阶段 | 时机 | 填充条件 | 说明 |
| --- | --- | --- | --- |
| **注册阶段** | 路由注册时（`buildEntry`） | 所有零值字段（`IsZero()`） | 对空模板一次性预填，生成 `defaultReq` |
| **请求阶段** | JSON/表单绑定后（`httpEngine`） | 仅 nil 指针字段（`Kind==Ptr && IsNil()`） | 补填动态创建的子元素（slice/数组/map/nested ptr），值类型跳过 |

- **注册阶段**：模板为空，所有零值的 `default` 字段被预填，生成 `defaultReq`。`int`/`string`/`[]string`/`*int` 等均参与。
- **请求阶段**：JSON 解析动态创建了 slice/数组元素 / map value / nested ptr struct 后，递归进入并对其中 nil 的**指针字段**补填默认值。**值类型（`int`/`string`/`bool`）跳过**，避免覆盖用户显式传入的零值（如 `{"qty": 0}` 不应被改成 `1`）。

### 完整示例

以下示例覆盖了所有常见字段类型的默认值场景：

```go
type Address struct {
    City string `json:"city" default:"北京"`        // string 值类型
    Code *int   `json:"code" default:"100000"`    // *int 指针类型
}

type OrderReq struct {
    // —— 顶层标量 ——
    Page   int     `json:"page" default:"1"`       // int 值类型
    Age    *int    `json:"age" default:"18"`        // *int 指针类型
    Note   string  `json:"note" default:"ok"`      // string 值类型
    Status *string `json:"status" default:"new"`   // *string 指针类型

    // —— 顶层标量切片 ——
    Tags   []string `json:"tags" default:"a,b"`    // []string
    Scores []*int   `json:"scores" default:"1,2"`  // []*int

    // —— 嵌套值结构体 ——
    Addr Address `json:"addr"`                      // 值类型嵌套

    // —— 嵌套指针结构体 ——
    BillAddr *Address `json:"billAddr"`             // 指针类型嵌套

    // —— 值结构体切片 ——
    Addrs []Address `json:"addrs"`                  // []struct

    // —— 指针结构体切片 ——
    BillAddrs []*Address `json:"billAddrs"`           // []*struct

    // —— map[string]struct ——
    Meta map[string]Address `json:"meta"`           // map 值类型 struct

    // —— map[string]*struct ——
    BillMeta map[string]*Address `json:"billMeta"`   // map 指针类型 struct

    // —— 不支持 default 标签的类型（设置无效） ——
    Raw   map[string]any `json:"raw" default:"x"`  // ❌ map 不支持
    Extra any             `json:"extra" default:"y"` // ❌ any 不支持
}
```

### 各字段两阶段填充明细

下表中，"注册"列表示路由注册阶段对模板的填充行为，"请求"列表示请求阶段（JSON 绑定后）的填充行为。假设用户请求中**未传递**对应字段。

| 字段 | 类型 | `hasDefault` | 注册阶段（模板为空） | 请求阶段（JSON 未传） |
| --- | --- | :---: | --- | --- |
| `Page` | `int` | ✅ | 填充 `1`（`IsZero()`） | 跳过（值类型），保留模板值 `1` |
| `Age` | `*int` | ✅ | 填充 `&18`（nil → `IsZero()`） | nil 时填充 `&18`；若用户传了 `&0` 则跳过 |
| `Note` | `string` | ✅ | 填充 `"ok"`（`IsZero()`） | 跳过（值类型），保留模板值 |
| `Status` | `*string` | ✅ | 填充 `&"new"`（nil → `IsZero()`） | nil 时填充 `&"new"`；若用户传了 `&""` 则跳过 |
| `Tags` | `[]string` | ✅ | 填充 `["a","b"]`（nil → `IsZero()`） | 跳过（非指针类型） |
| `Scores` | `[]*int` | ✅ | 填充 `[&1, &2]`（nil → `IsZero()`） | 跳过（非指针类型） |
| `Addr` | `Address`（值 struct） | ❌ | 递归进入：`City` 填 `"北京"`，`Code` 填 `&100000` | 模板已带默认值，跳过；若 JSON 覆盖了，递归进入后仅 `Code` nil 时填充 |
| `BillAddr` | `*Address` | ❌ | 跳过（nil struct 指针，无法递归） | 绑定创建 `&Address{}` 后递归进入，`Code` nil 时填充 `&100000` |
| `Addrs` | `[]Address` | ❌ | 跳过（nil 切片，无元素可遍历） | 绑定创建元素后递归进入每个 `Address`，`Code` nil 时填充 |
| `BillAddrs` | `[]*Address` | ❌ | 跳过（nil 切片） | 绑定创建 `[]*Address{&Address{}}` 后，递归进入每个元素，`Code` nil 时填充 |
| `Meta` | `map[string]Address` | ❌ | 跳过（nil map） | 绑定创建 entry 后递归进入每个 value `Address`，`Code` nil 时填充（值拷贝，写回 map） |
| `BillMeta` | `map[string]*Address` | ❌ | 跳过（nil map） | 绑定创建 entry 后递归进入每个 `*Address`，`Code` nil 时填充（通过 pointee 写回） |
| `Raw` | `map[string]any` | ❌ | 跳过（map 不支持 default） | 跳过 |
| `Extra` | `any` | ❌ | 跳过（any 不支持 default） | 跳过 |

#### 关键要点

1. **`hasDefault=true` 的字段**（标量/标量指针/标量切片）在注册阶段**全部填充**；请求阶段仅**指针类型**的 nil 字段填充，值类型跳过以避免覆盖用户显式传入的零值（如 `{"page": 0}` 不应被改成 `1`）。
2. **`hasDefault=false` 的容器字段**（struct/map/切片）自身不支持 `default` 标签，但注册阶段会穷尽其内部所有零值字段；请求阶段在绑定创建子元素后，再对 nil 指针子字段补填。
3. **值 struct 与指针 struct 的关键差异**：`Addr Address` 模板为零值 struct → 注册阶段递归预填子字段；`BillAddr *Address` 模板为 nil → 注册阶段跳过，其子字段默认值仅在请求阶段（JSON 传入后）生效。
4. **OpenAPI 文档**展示 `default` 的规则与填充保证一致：**指针类型**字段在其所属 struct 可被 `applyDefaults` 到达时展示（`reachedByDefaults` 追踪）；**值类型**字段仅在其所在 struct 被值嵌套可达（顶层或值类型 struct 字段链）时展示（`reachedViaValue` 追踪）。多层容器（如 `map[K][]Struct`）中的 struct 不可达，其指针和值字段的 `default` 均不展示。详见 `openapi.md` 中 decorate 决策矩阵。
5. **不支持 default 的类型**（`map`、`any`、`*struct`、`[]struct`、`time.Time` 等）设置 `default` 标签无效，`isDefaultSupported` 返回 false，`hasDefault` 恒为 false，标签值被静默忽略。

### 注册阶段：模板预填

路由注册时，`applyDefaults` 遍历 Req 结构体树，将所有带 `default` 标签的零值字段填入默认值，生成 `defaultReq` 模板。值类型嵌套结构体会被递归进入并预填。指针类型嵌套结构体（模板中为 nil）跳过。

请求时通过 `reflect.New` + `Set(defaultReq)` 浅拷贝获得带默认值的实例。若模板中存在非 nil 的指针/切片/map 字段或元素内含引用的数组字段（`needsDeepCopy` 为 true），还会先执行 `deepCopyDefaults` 断开这些引用类型字段的共享引用，确保并发安全，然后再执行 JSON/表单绑定覆盖。

### 请求阶段：仅 nil 指针补填

JSON 绑定会**动态创建** slice/数组元素、map value、指针嵌套结构体。这些元素的子字段在注册阶段不存在（模板中容器为 nil 或零值），因此绑定后需再次调用 `applyDefaults(requestPhase=true)` 递归补填。

**关键约束**：请求阶段仅对 **nil 指针字段** 填充默认值，值类型（`int`/`string`/`bool` 等）一律跳过。原因是值类型的零值（`0`、`""`、`false`）无法区分"用户未传"与"用户显式传了零值"——直接填充会覆盖用户的合法零值输入。

**容器嵌套深度限制**：`applyDefaults` 仅支持单层容器（`[]Struct`、`[]*Struct`、`[N]Struct`、`[N]*Struct`、`map[K]Struct`、`map[K]*Struct`，固定长度数组与切片行为一致）及其指针包裹形式（`*[]Struct`、`*[]*Struct`、`*[N]Struct`、`*map[K]Struct`、`*map[K]*Struct`，指针解引用后穿透进入元素）。多层容器（如 `map[K][]Struct`、`map[K][N]Struct`、`[][]Struct`）的内部元素无法被穿透，其默认值填充、nonzero 校验均不生效。详见 `request.md` 中"容器嵌套深度限制"章节。

```go
// ✅ 嵌套容器中的默认值字段推荐使用指针类型
type Item struct {
    Name   string  `json:"name" nonzero:"true"`
    Qty    *int    `json:"qty" default:"1"`     // *int：nil="未传"，&0="传了0"
    Status *string `json:"status" default:"ok"`  // *string：nil="未传"，&""="传了空串"
}

type Req struct {
    Items []Item `json:"items"`
}
```

- 不传 `qty` → JSON 解码为 nil → `IsNil()=true` → 填充 `&1` ✅
- 传 `qty: 0` → JSON 解码为 `&0`（非 nil） → `IsNil()=false` → 跳过，保留 0 ✅

### 覆盖规则

| 场景 | 结果 |
| --- | --- |
| 请求**未传递**该字段 | 保留默认值（注册阶段模板预填） |
| 请求**传递了但解析失败**（如 `page=abc`） | 保留默认值 |
| 请求**传递且解析成功** | 使用请求值（覆盖默认值） |

规则要点：

- 默认值由注册阶段 `applyDefaults` 一次性填充到模板中，请求时浅拷贝获得带默认值的实例。
- 若模板中存在非 nil 的指针/切片/map 字段（`needsDeepCopy` 为 true），请求时会先执行 `deepCopyDefaults` 断开共享引用，再执行绑定覆盖。
- 显式传递等于零值的值（如 `page=0`、`active=false`）在顶层字段中会被正确保留（模板预填后被 JSON 覆盖）；在嵌套容器（slice/map 元素）中，值类型字段若用户传了零值，该零值也会被保留——请求阶段仅填充 nil 指针，不处理值类型，因此不会用默认值覆盖用户的零值输入。
- 切片默认值以逗号分隔（如 `default:"a,b,c"`）。Trim 后为空（`default:""`、`default:" "`、`default:",,,"`）视为**空切片** `[]`，而非含单个空元素的切片 `[""]`。逗号是分隔符、无转义机制，元素值本身不能包含逗号。
