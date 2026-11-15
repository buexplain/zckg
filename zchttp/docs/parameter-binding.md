# 参数绑定规则

本框架会根据请求的方法（method）与内容类型（Content-Type），自动将请求参数绑定到 handler 的 `Req` 结构体。绑定相关实现位于 `binding.go`，校验相关实现位于 `validate.go`。

绑定与校验分为两个独立函数：
- `bindRequestData(r, reqPtr, meta, multipartMaxMemory, keepRawBody)`：仅执行数据绑定（query/body），在路由命中后立即调用（`multipartMaxMemory` 为 multipart 解析的内存缓冲上限，取自引擎的 `MultipartFormMaxMemory`；`keepRawBody` 为注册期预计算的 `KeepRawBody` 嵌入标记，为真时不读取请求体，见“2.2 保留原始请求体（KeepRawBody）”）。
- `bindPathParams(reqPtr, params, values)`：仅执行路由路径参数绑定，在 `bindRequestData` 之后调用（仅参数路由触发，路径参数覆盖同名 query/body 值）。
- `validateRequest(reqPtr, meta, needsNonzero)`：仅执行参数校验（nonzero + Validator），在洋葱模型 core 层调用；`needsNonzero` 为注册期预计算的传递性标记，全树无 nonzero 字段时跳过遍历（详见 `parameter-validate.md`）。

> 默认值（`default` 标签）采用两阶段填充：注册阶段预填模板零值字段，请求阶段绑定后补填动态创建的子元素中的 nil 指针字段。详见[默认值机制](#六默认值机制)。

## 一、绑定来源：按 method 与 Content-Type 分流

### 0. Req 字段的两个来源投影

同一个 Req 字段最多有两套独立绑定语义，不能跨来源混用：

| 字段标签 | Form 链（query / urlencoded / multipart / path） | JSON body |
| --- | --- | --- |
| 无标签 | 字段名 | 字段名 |
| `form:"q"` | `q` | 字段名或 json 名 |
| `form:"-"` | **不参与** | 按 json 标签/字段名正常参与 |
| `json:"x"` | `x`（普通 json 名作为 fallback） | `x` |
| `json:"-"` | **回退字段名**（JSON 的特殊值不污染 Form 链） | **不参与** |
| `json:"-,"` | **回退字段名** | 键名 `-` |
| `form:"-" json:"x"` | 不参与 | `x` |

规则：

- `form:"-"` **无条件跳过 Form 链**；
- `json:"-"` 只跳过 JSON；Form 链按字段名继续绑定；
- `json:"-,"` 的 `-` 只对 JSON 生效；Form 链回退字段名；
- 普通 `json:"x"` 仍作为 Form 链 fallback，兼容现有接口；
- Path 参数完全复用 Form 链；
- JSON body 由 `encoding/json` 解码，Form 标签不参与。

### 1. GET / DELETE / HEAD

绑定 URL query 参数（`r.URL.Query()`）。

```
GET /search?keyword=go&page=3
```

> 只有 Req 存在 **Form 链可绑定字段** 时才解析并校验 query。URL query 语法错误（如非法百分号转义、非法分隔符），或已知字段的值无法转换（如 `page=abc`），都会返回 400；未知参数忽略。没有 Form 链可绑定字段时，不解析 query、不校验 query。

### 2. POST / PUT / PATCH 等带请求体的方法

> 例外：Req 嵌入 `zchttp.KeepRawBody` 时，无论 method 均按上一节 GET / DELETE / HEAD 规则只绑定 query，请求体原样保留，见“2.2 保留原始请求体（KeepRawBody）”。

采用**合并绑定**：先严格校验并绑定 URL query 参数，再按 `Content-Type` 绑定请求体，**body 中出现的字段覆盖 query 已绑定的同名字段**，body 中缺失的字段保留 query 绑定值（兼容 REST 常见的"query 放控制参数 + body 放资源数据"混合传参风格，如 `POST /orders/batch?dryRun=true` + JSON 资源体）。

> 只有 query 存在 Form 链可绑定字段时才解析/校验 query；只有选中的 body 来源存在可绑定字段时才读取 body。两者都没有时，Req 不进行任何绑定，body 原样保留、非法 query 也不报错。
>
> query 的非法值不会被 body 覆盖“救回”：在 query 来源参与绑定、且 body 对同名字段提供合法值时，query 语法错误或转换失败仍在 body 绑定前返回 400。body 只覆盖已经合法绑定的 query 值。

> ⚠️ **行为变更提示（breaking change）**：早期版本中这类方法仅绑定 body、query 参数被静默忽略；当前版本 query 参数会生效。若你的接口依赖"query 被忽略"的旧行为（如同名参数刻意只认 body），升级后需确认 query 传参不会意外影响字段值。

请求体按 `Content-Type` 选择绑定方式：

| Content-Type | 绑定方式 |
| --- | --- |
| `application/json` | `json.Decode` 解析请求体 |
| `application/x-www-form-urlencoded` | 解析请求体表单后绑定（body 覆盖 query） |
| `multipart/form-data` | `r.ParseMultipartForm(MultipartFormMaxMemory)` 后绑定表单字段与上传文件 |
| 其他（有请求体时） | 回退按 JSON 解析 |

> urlencoded body 对除 GET/DELETE/HEAD 外的**所有**方法生效（含 OPTIONS/TRACE/CONNECT）。框架复制 request 并清空 query 后解析 body，只绑定 Form 链；query 由独立的严格解析流程处理，不会因 body 解析再次校验 query。

**body 覆盖 query 的粒度由解码器语义决定**（以 JSON 为例）：

| body 中的情况 | 对同名字段的影响 |
| --- | --- |
| 显式出现（含显式零值，如 `"page": 0`、`"source": ""`） | 覆盖 query 值 |
| `null`，目标字段为非指针类型 | JSON 解码 no-op，**保留 query 值** |
| `null`，目标字段为指针类型 | 标准库语义置 `nil`，覆盖 query 值；若该字段有 `default`，请求阶段会因 nil 回填默认值（显式 null 不能清除 default） |
| 未出现 | 保留 query 值 |

表单（`x-www-form-urlencoded` / `multipart`）同理：body 中出现的字段覆盖同名 query 值，未出现则保留。

> `Content-Type` 通过 `mime.ParseMediaType` 解析：主类型按标准规则规范为小写后匹配，支持大小写变体、带引号参数和额外空白；语法非法时绑定失败并返回 400。
>
> JSON 请求体必须包含且仅包含一个完整 JSON 值；首个值后的空白合法，第二个 JSON 值或其他非空尾随内容会绑定失败并返回 400。空请求体保持现有 Req 模板值。
>
> `multipart/form-data` 的内存缓冲上限由引擎字段 `MultipartFormMaxMemory` 定义，`NewEngine` 默认 **32 MB**。文件部分的超出量由标准库写入临时文件；非文件表单值没有落盘路径，它与文件部分共享 `MultipartFormMaxMemory + 10 MB` 的内存预算，超出时绑定失败并返回 400（`multipart: message too large`）。按需调整示例：`engine.MultipartFormMaxMemory = 64 << 20`。

### 2.1 请求体大小限制（MaxBodyBytes）

所有带请求体的方法在绑定前统一受引擎字段 `MaxBodyBytes` 限制（`http.MaxBytesReader` 包裹 `r.Body`，对 JSON/表单/multipart 等所有 Content-Type 生效）：

| 配置 | 行为 |
| --- | --- |
| `NewEngine()` 默认 **32 MB** | 超限请求在绑定阶段失败，映射为 `BindingError` 返回 **400** |
| 显式调大（如 `engine.MaxBodyBytes = 64 << 20`） | 适用于合法的大请求体场景 |
| 显式置 `0` | 框架不额外限制（需自行评估超大请求体导致的内存/磁盘 DoS 风险）；但解析器自身的限制仍生效：`application/x-www-form-urlencoded` 由标准库 `ParseForm` 解析，超过 10 MB 时绑定失败返回 400（`http: POST too large`）；JSON 无额外隐式上限；multipart 的非文件部分仍受 `MultipartFormMaxMemory + 10 MB` 预算约束（文件部分可落盘） |

两个字段的关系：`MaxBodyBytes` 是请求体的**整体入口上限**（先于任何解析生效）；`MultipartFormMaxMemory` 仅控制 multipart 解析时的**内存缓冲上限**（文件部分超出时写入临时文件；非文件部分共享该上限 + 10 MB，超出即失败；整体仍受 `MaxBodyBytes` 约束）。需要大文件上传时两者需同步调大。

```go
func example() {
    engine := zchttp.NewEngine()
    engine.MaxBodyBytes = 64 << 20          // 请求体整体上限 64 MB
    engine.MultipartFormMaxMemory = 32 << 20 // multipart 内存缓冲上限，超出落盘
}
```

### 2.2 保留原始请求体（KeepRawBody）

反向代理、webhook 原始请求体验签、流式上传等场景需要 handler 拿到**未被读取的原始请求体**。引擎只对当前请求来源中**存在可绑定字段**的部分读取：有 Form 链字段才解析 query；选中的 body 来源有可绑定字段才读取 body。在 Req 中嵌入 `zchttp.KeepRawBody` 即可声明“本 Req 不绑定请求体”。完全空 Req、`json:"-"` 且 `form:"-"` 的字段、或当前来源没有可绑定字段时，不读取 body；`form:"-"` 只跳过 Form 链，`json:"-"` 只跳过 JSON，二者互不影响；`json:"-,"` 的 `-` 只用于 JSON 键名。query 是否存在严格校验取决于 Form 链是否有可绑定字段：

```go
type WebhookReq struct {
	// 声明：请求体原样保留，不解析
	zchttp.KeepRawBody
	// 仍从 query 绑定，nonzero 照常校验
	Sig string `json:"sig" nonzero:"true"`
}

type WebhookRes struct {
	OK bool `json:"ok"`
}

var webhookSecret = []byte("secret")

func webhook(ctx context.Context, req WebhookReq) (WebhookRes, error) {
	r, _ := zchttp.RequestFromContext(ctx)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		// 含超出 MaxBodyBytes 的 *http.MaxBytesError，包装为 BindingError 返回 400
		return WebhookRes{}, zchttp.NewBindingError(err)
	}
	mac := hmac.New(sha256.New, webhookSecret)
	mac.Write(body)
	if !hmac.Equal([]byte(req.Sig), []byte(hex.EncodeToString(mac.Sum(nil)))) {
		return WebhookRes{}, zchttp.NewBindingError(errors.New("invalid signature"))
	}
	return WebhookRes{OK: true}, nil
}

// 注册：r.POST("/webhook", webhook)
```

**语义：嵌入 `KeepRawBody` 的 Req，无论 HTTP 方法，均按 GET / DELETE / HEAD 规则绑定**——只绑定 query 与路径参数，`r.Body` 不被读取，原样留给中间件与 handler（经 `RequestFromContext` 获取）。query 仍严格校验（语法错误或转换失败 400）；默认值、`nonzero` 校验、`Validator`、“路径参数 > query”覆盖顺序均不变。

识别与校验规则：

| 情形 | 行为 |
| --- | --- |
| Req **顶层值嵌入** `zchttp.KeepRawBody` | 生效（注册期预计算为 `routeEntry.keepRawBody`，请求期仅读 bool） |
| `*KeepRawBody` 指针嵌入、嵌套在更深层结构体中、作为具名字段 | 不识别，按普通规则绑定 |
| 嵌入在 GET / DELETE / HEAD 路由上 | 不报错，等价于无效果（代理通常将同一 handler 注册到全部方法） |
| 同时含上传文件字段（`*multipart.FileHeader` 及其切片，含嵌入结构体中的） | 注册时 panic：文件只能来自 multipart 请求体，与声明自相矛盾 |
| 含 map / 嵌套 struct 等仅能经 JSON 绑定的字段 | 不报错，静默不绑定（与 GET 路由上同类字段的行为一致） |

`KeepRawBody` 为空结构体，嵌入后不产生任何绑定字段，不影响 `default` 填充、`nonzero` 校验与 OpenAPI schema（OpenAPI 输出规则见 `openapi.md`）。

使用限制：

- **`MaxBodyBytes` 仍生效**：请求体在路由匹配前即被 `http.MaxBytesReader` 包装，handler 读取超限时返回 `*http.MaxBytesError`；代理大文件需调大或设为 0。
- **排空不受影响**：handler 返回后引擎限量排空剩余请求体以复用连接；handler 已读尽时为空操作。
- **中间件读取请求体会使 handler 拿不到**：与普通 `http.Handler` 约定一致，由使用者负责。

### 3. 路由路径参数（所有 method）

若路由注册时使用了 `{name}` / `{name?}` / `{name...}` 路径参数（详见 `routing.md` 中“路由参数”章节），参数值在 query/body 绑定**之后**由 `bindPathParams` 写入 Req，规则如下：

- **参数名即字段绑定名**：按 form > json > 字段名优先级解析，注册阶段预计算绑定关系，参数名无对应字段时注册即 panic。
- **绑定名唯一性**：注册阶段分别检测 Form 链重复名与 JSON 重复名，任一重复都 panic；匿名嵌入展开后的重复也算。
- **覆盖语义**：三级覆盖链 **path > body > query**（路径参数是更精确的意图，优先级最高；带 body 方法内部为 body > query，见上节）。
- **类型由字段声明决定**：复用 `setScalar` 转换，支持 string/bool/int 全系/uint 全系/float/指针与 `time.Time`（时间字段的 `time_format`/`time_location` 标签同样生效，指针形态也是）；目标字段为 slice、map、结构体等其他类型时注册即 panic（时间切片因此不能作路径参数）。
- **失败语义**：某个来源存在可绑定字段并参与绑定后，该来源内 query/form/multipart/path 字段转换失败立即返回 400；JSON body 解码失败同样 400。没有可绑定字段的来源不解析、不校验、不报错。
- **可选参数省略**：`{name?}` 未出现在请求路径中时不写入字段，保留模板 `default` 值或零值。
- **通配尾段**：`{name...}` 捕获剩余全部路径（已百分号解码、可含 `/`、不做路径清理）；零段命中（如 `/dify/{rest...}` 收到 `/dify`）时同可选参数省略，不写入字段。

## 二、字段名解析规则

### 1. Form 链（query / urlencoded / multipart / path）

按以下优先级解析：

1. `form` 标签：`form:"-"` 无条件跳过；普通名字直接使用；
2. `json` 标签中的**普通名字**作为兼容 fallback；
3. 结构体字段名。

`json:"-"` 与 `json:"-,"` 是 JSON 专用特殊值，不参与 Form 链命名，Form 链回退结构体字段名。`form:"-"` 只跳过 Form 链，字段仍可被 JSON 绑定。

### 2. JSON body

完全按 `encoding/json`：

- `json:"-"` 跳过；
- `json:"-,"` 键名为 `-`；
- 普通 `json:"x"` 键名为 `x`；
- 无 json 名时使用字段名；
- `form` 标签不影响 JSON。

### 3. 重复名

Form 链绑定名重复、JSON 名重复都在注册期 panic。Req 定义必须显式无歧义；匿名嵌入展开后的重复也算。

### 4. Req 级读取判定

不存在任何可接收参数的字段时，引擎不读取 body、不解析/校验 query。是否存在可接收字段按**当前请求的来源**判断，具体见第一节的来源投影与下面各节。

上述绑定名作用于 query / 表单 / multipart / 路径参数。JSON 请求体由 `encoding/json` 直接解码，只认 `json` 标签（无 json 名时用字段名，键名大小写不敏感，`json:"-"` 永不绑定），`form` 标签对其无效；例如 `form:"q" json:"keyword"` 的字段在 JSON 中只接受 `keyword`（`KEYWORD` 也可），不接受 `q`。OpenAPI 的 JSON schema 按同一规则命名，见 `openapi.md`。

未导出的匿名**指针**嵌入（`type Req struct { *base }`）是例外：框架元数据跳过它（query / 表单不绑定其字段），但 JSON 请求体仍交给 `encoding/json`，只要 JSON 中出现该嵌入的导出字段，解码就会报 `json: cannot set embedded pointer to unexported struct`，整个请求返回 400；不含这些字段的请求不受影响。详见 `request.md`。

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
| 嵌套结构体 | 仅 JSON 路径支持（`json.Decoder` 递归反序列化）；query/form 路径不递归具名嵌套结构体，但匿名嵌入展开后的字段视为扁平字段处理 |

> **来源参与绑定后严格**：query、urlencoded/multipart body、路径参数的已知字段转换失败立即返回 400；JSON body 解码失败同样 400。不会跳过失败字段或保留旧值。没有可绑定字段的来源完全不解析、不校验。未知参数与不可绑定类型（map/struct/文件等）仍按既有规则忽略。body 不能覆盖或豁免非法 query（仅当 query 来源参与绑定）。无默认值的 nil 指针在字段未传时仍为 nil，因此其 `nonzero:"true"` 校验会失败；已有默认值或已有指针值不会被失败输入覆盖（失败输入现在直接 400）。
>
> query/form 参数值**不做 TrimSpace**：标量与切片一致，首尾空格原样保留；`default` 标签值仍按默认值规则 Trim（见第六节）。

### map 类型绑定

map 类型字段的绑定行为因来源而异：

- **JSON 绑定**：支持 `encoding/json` 能解码的 map 类型，例如 `map[string]any`、`map[string]string`、`map[string]int`、`map[string]*Struct`。JSON 对象键通常需为 string、整数类型或实现标准库支持的文本解码接口；不支持的键值类型会绑定失败并返回 400。
- **query / form 绑定**：`bindValues` 遍历 `structMeta.fields` 调用 `setFieldValue` → `setScalar`，map 类型不在 switch case 中，按不可绑定类型跳过（不算字段转换失败）。

因此，若 handler 的 Req 包含 map 字段，应使用 JSON 请求体，并遵守 `encoding/json` 的可解码类型约束。

### 切片绑定

同名参数出现多次时绑定为切片：

```
GET /search?tags=a&tags=b&tags=c   →   Tags []string{"a","b","c"}
```

`[]byte` 及底层为 `[]byte` 的命名类型按字节切片绑定：query/form/multipart 传重复的十进制字节参数（如 `data=104&data=101`），JSON 请求体传数字数组（如 `[104,101]`）。默认 JSON 响应由 `encoding/json` 编码为 base64 字符串，这是响应序列化行为，不属于请求绑定。JSON 请求体契约以数字数组为准；`encoding/json` 额外接受 base64 字符串属于标准库宽容行为，不在框架契约内，框架不额外校验或拒绝。

## 四、time.Time 解析

`time.Time` 字段通过 `time_format` 与 `time_location` 标签控制解析方式。这两个标签只作用于**表单绑定链路**（query / x-www-form-urlencoded / multipart / path）；JSON 请求体由 `encoding/json` 直接解码，不读取这两个标签，时间字段必须使用 RFC3339 字符串。

### 支持的时间字段形态

两个标签对表单绑定链路中所有能被 `setScalar` 写入的时间形态生效，且**逐个元素**应用（切片中每个元素都按同一标签解析）：

| 字段形态 | 是否解析标签 | 说明 |
| --- | --- | --- |
| `time.Time` | ✅ | 基础形态 |
| `*time.Time` | ✅ | 最多 32 层指针（如 `**time.Time`）均可，解析后落到最内层值；超出按不支持处理（防自引用命名类型） |
| `[]time.Time` | ✅ | 切片元素逐个解析 |
| `[]*time.Time` | ✅ | 每个元素分配独立值后解析 |
| `[][]time.Time`、`*[]time.Time` | ❌ | 绑定链路不支持多层容器与切片指针，标签不生效（注册期不会报错，字段保持绑定前值/零值，不会被写入） |
| `map[K]time.Time` | ❌ | map 值不参与标量绑定 |

> 时间切片的空字符串元素表示“跳过该元素并保留原位置旧值”（模板/default 或 nil），不会写成零时间；`[]time.Time` 与 `[]*time.Time` 一致。其余类型（如 `[]string`）的空字符串仍按正常值处理。

> 只有"请求侧"会解析时间：Res 结构体不参与绑定，`time.Time` 响应字段由 `encoding/json` 序列化，恒输出 RFC3339 字符串，`time_format`/`time_location` 写在 Res 上不生效。
>
> JSON 请求体同样不经过 `setScalar`：`time_format`/`time_location` 不生效，`time.Time` 字段只接受 RFC3339 字符串；`[]time.Time`、`map[K]time.Time` 也按 `encoding/json` 的字符串规则解码。OpenAPI 的 JSON 请求体/响应 schema 因此恒为 `date-time`，只有 query/form/multipart/path 才会按 `unix*` 生成 `integer`。

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

- **带可选负号的十进制整数**：视为时间戳，负号不计入位数；按数字位数推断精度——`10` 位=秒、`13`=毫秒、`16`=微秒、`19`=纳秒，其余按秒。四种精度分别使用 `time.Unix`、`time.UnixMilli`、`time.UnixMicro` 和纳秒构造，避免统一乘纳秒造成溢出。
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
    StartTime time.Time   `json:"start_time" time_format:"unix"`
    Date      time.Time   `json:"date" time_format:"2006-01-02" time_location:"Asia/Shanghai"`
    SlashDate time.Time   `json:"slash_date" time_format:"02/01/2006"`
    Auto      time.Time   `json:"auto"`  // 自动探测 RFC3339 或时间戳

    // 指针与切片形态同样按标签解析（逐元素应用）
    ExpireAt  *time.Time  `json:"expire_at" time_format:"unix" time_location:"UTC"`
    Days      []time.Time `json:"days" time_format:"2006-01-02" time_location:"Asia/Shanghai"`
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
    if req.File == nil {
        return Res{}, nil
    }
    f, err := req.File.Open()   // 打开文件流
    if err != nil {
        return Res{}, zchttp.NewBindingError(err)
    }
    defer f.Close()
    // 从 f 读取内容；req.File.Filename、req.File.Size 可读
    return Res{}, nil
}
```

## 六、默认值机制

通过 `default` 标签为字段设置默认值。框架采用**两阶段填充**策略，在注册阶段和请求阶段各执行一次 `applyDefaults`，保证不同嵌套场景下默认值均正确生效。

### default 标签支持范围

`default` 标签仅在以下类型上生效（`isDefaultSupported` 返回 true），并且**值必须能被运行时解析**才会置 `hasDefault=true`：

- 标量：`string`、`bool`、`int/uint` 全系列、`float32`/`float64`
- 标量指针：`*string`、`*int`、`*bool` 等（最多 32 层指针，如 `**int`；超出按不支持处理）
- 标量切片：`[]string`、`[]int`、`[]*int`、`[]**int` 等（元素必须是标量或标量指针）

> 类型支持不等于值合法：`int` 的 `default:"abc"`、`bool` 的 `default:"yes"`、浮点的 `NaN`/`+Inf`、含非法元素的 `[]int default:"1,x"` 都视为无法解析，注册期输出 `slog.Warn`，`hasDefault=false`，运行时永不填充，OpenAPI 也不展示该 default。标量值按运行时规则先 `TrimSpace`（如 `default:"  x  "` 实际填充 `"x"`）；切片先 TrimSpace 再去除首尾逗号，Trim 后为空视为空切片。

> 指向容器、容器的容器与数组（如 `*[]int`、`[][]int`、`[]*[]int`、`[N]int`）**不在支持范围**：`setFieldValue` 不会展开这些形态，注册期按“不支持类型”告警且永不填充；OpenAPI 也不会为它们展示 `default`（与运行时一致）。

> ⚠️ **误用检测为告警级（非阻断）**：`default` 写在上述范围之外（如 `time.Time`、`map`、`struct`、`*[]int`）、值无法解析（如 `int` 的 `"abc"`、`NaN`），或写在请求阶段永不可达的路径上时，注册期仅输出 `slog.Warn` 告警（含路由与 handler 位置），**不会 panic 或拒绝注册**，误用字段将"永不填充默认值"。生产环境若将日志级别调至 Error 则该提示不可见，请保持启动期日志可见并及时关注 Warn 输出。

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

> 显式 JSON `null` 对指针字段与“未传”在解码后都是 nil，无法区分；存在 `default` 时请求阶段会回填默认值，显式 `null` 不能清除 default。显式零值（如 `&0`、`&""`）不是 nil，不会被回填。

#### 关键要点

1. **`hasDefault=true` 的字段**（标量/标量指针/标量切片）在注册阶段**全部填充**；请求阶段仅**指针类型**的 nil 字段填充，值类型跳过以避免覆盖用户显式传入的零值（如 `{"page": 0}` 不应被改成 `1`）。
2. **`hasDefault=false` 的容器字段**（struct/map/切片）自身不支持 `default` 标签，但注册阶段会穷尽其内部所有零值字段；请求阶段在绑定创建子元素后，再对 nil 指针子字段补填。
3. **值 struct 与指针 struct 的关键差异**：`Addr Address` 模板为零值 struct → 注册阶段递归预填子字段；`BillAddr *Address` 模板为 nil → 注册阶段跳过，其子字段默认值仅在请求阶段（JSON 传入后）生效。
4. **OpenAPI 文档**展示 `default` 的规则与填充保证一致：只有能被运行时解析的 default 才会展示；**指针类型**字段在其所属 struct 可被 `applyDefaults` 到达时展示（`reachedByDefaults` 追踪）；**值类型**字段仅在其所在 struct 被值嵌套可达（顶层或值类型 struct 字段链）时展示（`reachedViaValue` 追踪）。多层容器（如 `map[K][]Struct`）中的 struct 不可达，其指针和值字段的 `default` 均不展示。详见 `openapi.md` 中 decorate 决策矩阵。
5. **不支持 default 的类型**（`map`、`any`、`*struct`、`[]struct`、`time.Time` 等）设置 `default` 标签无效，`isDefaultSupported` 返回 false，`hasDefault` 恒为 false；字段不会被填充，注册期会输出 `slog.Warn`（Warn 被日志级别过滤时可能不可见）。
6. **类型支持但值无法解析**（如 `int default:"abc"`、`bool default:"yes"`、`float64 default:"NaN"`、`[]int default:"1,x"`）同样按未设置处理：`hasDefault=false`，注册期告警，运行时与 OpenAPI 都不填充/不展示；`nonzero:"true"` 字段因此仍按必填推断。

### 注册阶段：模板预填

路由注册时，`applyDefaults` 遍历 Req 结构体树，将所有带 `default` 标签的零值字段填入默认值，生成 `defaultReq` 模板。值类型嵌套结构体会被递归进入并预填。指针类型嵌套结构体（模板中为 nil）跳过。

请求时通过 `reflect.New` + `Set(defaultReq)` 浅拷贝获得带默认值的实例。若模板中存在非 nil 的指针/切片/map 字段或元素内含引用的数组字段（`needsDeepCopy` 为 true），还会先执行 `deepCopyDefaults` 断开这些引用类型字段的共享引用，确保并发安全，然后再执行 JSON/表单绑定覆盖。

### 请求阶段：仅 nil 指针补填

JSON 绑定会**动态创建** slice/数组元素、map value、指针嵌套结构体。这些元素的子字段在注册阶段不存在（模板中容器为 nil 或零值），因此绑定后需再次调用 `applyDefaults(requestPhase=true)` 递归补填。

**关键约束**：请求阶段仅对 **nil 指针字段** 填充默认值，值类型（`int`/`string`/`bool` 等）一律跳过。原因是值类型的零值（`0`、`""`、`false`）无法区分"用户未传"与"用户显式传了零值"——直接填充会覆盖用户的合法零值输入。

**容器嵌套深度限制**：`applyDefaults` 可递归单层容器（`[]Struct`、`[]*Struct`、`[N]Struct`、`[N]*Struct`、`map[K]Struct`、`map[K]*Struct`）及其非 nil 指针包裹。固定数组与切片并不完全相同：`[N]Struct` 的 N 个值元素在注册模板中始终存在，元素内值字段和指针字段的 default 均可注册期预填；nil 切片/map 以及 nil 的容器指针没有元素可预填，绑定动态创建元素后仅补填 nil 指针子字段。多层容器（如 `map[K][]Struct`、`map[K][N]Struct`、`[][]Struct`）的内部元素无法被穿透。详见 `request.md` 中“容器嵌套深度限制”章节。

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
| `default` 标签值本身**无法解析** | 注册期告警，按未设置处理：字段保持零值，OpenAPI 不展示该 default；`nonzero:"true"` 时仍按必填处理 |
| query 或 form/multipart body 字段**传递了但解析失败**（如 `page=abc`、非法转义） | **400**：不保留默认值，body 也不能覆盖豁免 |
| JSON body 字段类型不匹配 | JSON 解码失败，返回 400 |
| 请求**传递且解析成功** | 使用请求值（覆盖默认值） |

规则要点：

- 默认值由注册阶段 `applyDefaults` 一次性填充到模板中，请求时浅拷贝获得带默认值的实例；只有 `hasDefault=true`（类型支持且值可解析）的字段参与填充。
- 若模板中存在非 nil 的指针/切片/map 字段（`needsDeepCopy` 为 true），请求时会先执行 `deepCopyDefaults` 断开共享引用，再执行绑定覆盖。
- 显式传递等于零值的值（如 `page=0`、`active=false`）在顶层字段中会被正确保留（模板预填后被 JSON 覆盖）；在嵌套容器（slice/map 元素）中，值类型字段若用户传了零值，该零值也会被保留——请求阶段仅填充 nil 指针，不处理值类型，因此不会用默认值覆盖用户的零值输入。
- 切片默认值以逗号分隔（如 `default:"a,b,c"`）。Trim 后为空（`default:""`、`default:" "`、`default:",,,"`）视为**空切片** `[]`，而非含单个空元素的切片 `[""]`。逗号是分隔符、无转义机制，元素值本身不能包含逗号。
