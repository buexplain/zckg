# zchttp 参考手册

本文件是 `zchttp` 的**单一事实源**：标签、类型映射、默认值、错误、限制与不变量只在这里维护完整表格；主题文档只写摘要并链接到这里。若主题文档与本文冲突，以本文为准，并应同步修正主题文档。

文中“契约”指调用方可以依赖的公开行为；“当前实现”指解释代码用的内部细节，可能随优化变化。

## 一、标签总览

### 1. 来源与角色

| 标签 | 读取方 | 运行时 | OpenAPI |
| --- | --- | --- | --- |
| `json` | Req + Res | Form 链 fallback / JSON 键名 / 响应序列化名 | JSON schema 属性名 |
| `form` | Req | Form 链绑定名，优先于 `json` | query/multipart/path 参数名 |
| `default` | Req | 注册期/请求期填充默认值 | 请求 schema 的 `default` |
| `nonzero` | Req | 零值校验 | 请求 schema 的 `required` 推断 |
| `time_format` | Req | query/form/multipart/path 的时间解析 | 表单形态时间 schema |
| `time_location` | Req | 表单时间解析时区 | 无直接 schema 影响 |
| `ignore` | Req + Res | 无 | 从 schema/query 排除；path 声明除外 |
| `example` | Req + Res | 无 | schema `example` |
| `description` | Req + Res | 无 | schema/参数描述；`OpenAPIMeta` 上为操作描述 |
| `deprecated` | Req + Res | 无 | schema/参数/操作 `deprecated` |
| `OpenAPIMeta` 上的 `tags`/`summary`/`description`/`deprecated` | Req | 无 | 操作级元信息 |
| `KeepRawBody`（嵌入） | Req | 不读取请求体 | 通用 `*/*` requestBody |

### 2. `json` 与 `form`

| 字段标签 | Form 链（query / urlencoded / multipart / path） | JSON body |
| --- | --- | --- |
| 无标签 | 字段名 | 字段名 |
| `form:"q"` | `q` | 字段名或 `json` 名 |
| `form:"-"` | **不参与** | 按 `json` 标签/字段名正常参与 |
| `json:"x"` | `x`（普通 json 名作为 fallback） | `x` |
| `json:"-"` | **回退字段名** | **不参与** |
| `json:"-,"` | **回退字段名** | 键名 `-` |
| `form:"-" json:"x"` | 不参与 | `x` |

规则：

- Form 链优先级：`form` 普通名 > `json` 普通名 > 字段名；`json:"-"`/`json:"-,"` 是 JSON 专用值，不污染 Form 链。
- JSON 完全按 `encoding/json`：`json:"-"` 排除、`json:"-,"` 名为 `-`、无 json 名用字段名、键名大小写不敏感、`form` 不参与。
- `form:"-"` 只跳过 Form 链，字段仍可被 JSON 绑定。
- Path 参数完全复用 Form 链绑定名。
- 未导出普通字段跳过；未导出值嵌入展开；未导出匿名指针嵌入跳过（见下）。

### 3. `default`

- 仅在 Req 上生效；Res 上写 `default` 无任何效果。
- 支持类型：标量（`string`/`bool`/`int*`/`uint*`/`float32`/`float64`）、标量的任意层指针（最多 32 层）、元素为标量或标量指针的切片。
- 不支持：`time.Time`、`struct`、`*struct`、`[]struct`、`map`、`*map`、数组、指向容器的指针（`*[]T`）、容器嵌套（`[][]T`、`[]*[]T` 等）。
- 值必须能被运行时解析：`int` 的 `"abc"`、`bool` 的 `"yes"`、`NaN`/`Inf`、含非法元素的切片都视为无效；注册期 `slog.Warn`，`hasDefault=false`，运行时/OpenAPI 都不填充/展示。
- 标量值先 `TrimSpace`；切片先 `TrimSpace` 再去除首尾逗号，Trim 后为空视为空切片 `[]`。
- 两阶段：注册期填充模板中“注册期可达”的零值字段；请求期只补 nil 指针（详见“默认值规则”）。

### 4. `nonzero`

- 仅在 Req 上生效；Res 不校验，也不参与 required 推断。
- `nonzero:"true"` 校验零值；`"false"`、未标注、非法值都不校验。
- 校验使用 `reflect.Value.IsZero`：nil 指针/切片、空字符串、0、false 都是零值。
- 与 `default` 独立解析：校验阶段看的是默认值填充后的值；OpenAPI 只有“能解析且本身非零”的 default 才能取消 required。
- 递归：嵌套 struct/指针、单层容器（`[]Struct`、`[]*Struct`、`[N]Struct`、`[N]*Struct`、`map[K]Struct`、`map[K]*Struct`）及其非 nil 指针包裹；固定数组即使整体为零也逐元素；nil/空切片、nil map 无元素；多层容器不穿透。
- 错误路径使用 Form 链绑定名；切片/数组不附加索引；map 追加 `fmt.Sprint(key)`，key 中的点号不转义。

### 5. `time_format` / `time_location`

- 只作用于表单绑定链路（query / urlencoded / multipart / path）；JSON 请求体与响应由 `encoding/json` 处理，时间必须 RFC3339 字符串。
- `time_format`：`unix`、`unixmilli`、`unixmicro`、`unixnano` 或 Go layout；为空时自动探测。
- 自动探测：带可选负号的十进制整数按位数推断（13=毫秒、16=微秒、19=纳秒，其余秒）；非数字依次尝试 RFC3339Nano、RFC3339、`2006-01-02 15:04:05`、`2006-01-02T15:04:05`、`2006-01-02`、`2006/01/02 15:04:05`、`2006/01/02`、`15:04:05`。
- `time_location`：默认 `time.Local`；解析失败降级为 `time.Local` 并 `slog.Warn`。
- 支持 `time.Time`、其任意层指针、`[]time.Time`、`[]*time.Time`；`[][]time.Time`、`*[]time.Time`、`map[K]time.Time` 不参与标量绑定。
- 时间切片的空字符串元素跳过并保留原位置旧值；旧值为零时间时仍为零时间。

### 6. `ignore` / `example` / `description` / `deprecated`

- `ignore:"true"`：从请求/响应 schema 与 query 中排除字段，不影响绑定/校验；path 参数声明受 OpenAPI 规范强制，不受影响。
- `example`：只用于 OpenAPI；按字段 schema 类型转换（integer→int64、number→float64、boolean→bool；无法转换保留字符串；数组按逗号拆分）。
- `description`：只用于 OpenAPI；字段描述；写在 `OpenAPIMeta` 上为操作描述。
- `deprecated:"true"`：只用于 OpenAPI；字段 schema、query/path Parameter Object 输出 `deprecated: true`；写在 `OpenAPIMeta` 上为操作级废弃；值类型嵌套 struct 的 deprecated 会作为 `$ref` 兄弟键写入，但 OpenAPI 3.0 工具会忽略。
- `deprecated`/`ignore` 都用 `strconv.ParseBool`：`1/t/T/TRUE/true/True` 为真，`0/f/F/FALSE/false/False` 为假；空/非法值不启用。

### 7. `OpenAPIMeta` 与 `KeepRawBody`

- `OpenAPIMeta` 只能嵌入 Req（顶层）；空结构体，不参与绑定/校验。标签：
  - `tags`：`/` 分隔的标签列表（trim 空项）；
  - `summary`：缺省回退为 handler 短函数名；
  - `description`：操作描述；
  - `deprecated:"true"`：操作级废弃。
- 写在 Res 上的 `OpenAPIMeta`/`KeepRawBody` 无效，注册期 `slog.Warn`。
- `KeepRawBody` 只有 Req **顶层值嵌入**才识别；`*KeepRawBody`、深层嵌入、具名字段都不识别。
- `KeepRawBody` 与带 Form 链绑定名的 `*multipart.FileHeader`/`[]*multipart.FileHeader` 同时出现 → 注册期 panic（文件只能来自 multipart body）。
- `KeepRawBody` 的 Req 无论 method 只绑定 query/路径参数，不读取 body；GET/DELETE/HEAD 上嵌入等价于无效果。

## 二、类型支持矩阵

| Go 类型 | query/form/multipart | path | JSON 请求体 | 默认 JSON 响应 | OpenAPI Req | OpenAPI Res |
| --- | --- | --- | --- | --- | --- | --- |
| `string` | ✅ | ✅ | ✅ | ✅ | string | string |
| `bool` | ✅ `ParseBool` | ✅ | ✅ | ✅ | boolean | boolean |
| `int*` | ✅ `ParseInt` | ✅ | ✅ | ✅ | integer/int64 或 int32 | 同左 |
| `uint*` | ✅ `ParseUint` | ✅ | ✅ | ✅ | integer/int64, minimum 0 | 同左 |
| `float32/64` | ✅ `ParseFloat` | ✅ | ✅ | ✅ | number/float 或 double | 同左 |
| 上述标量指针 | ✅ 自动分配 | ✅ | ✅ | ✅（nil→null） | nullable 包装 | 同左 |
| 标量切片 | ✅ 同名多值 | ❌ 注册期 panic | ✅ | ✅（nil→null） | array + nullable | 同左 |
| `[]byte` / 命名 byte slice | ✅ 重复十进制字节 | ❌ 注册期 panic | ✅ 数字数组（也接受 base64） | base64 字符串 | array 0..255 + nullable | string/byte |
| `[N]T` 固定数组 | ❌ | ❌ 注册期 panic | ✅ | ✅ | array（不 nullable） | 同左 |
| `map[K]V` | ❌ 跳过 | ❌ 注册期 panic | ✅（encoding/json 约束） | ✅（nil→null） | object + additionalProperties + nullable | 同左 |
| `time.Time` | ✅（按标签/自动探测） | ✅（按标签/自动探测） | 仅 RFC3339 字符串 | RFC3339 字符串 | 表单按 `unix*`→integer，否则 date-time；JSON 恒 date-time | date-time |
| 具名 struct | ❌ 不递归 | ❌ 注册期 panic | ✅ | ✅ | `$ref` | `$ref` |
| `*struct` | ❌ | ❌ 注册期 panic | ✅ | ✅（nil→null） | nullable + allOf[$ref] | 同左 |
| `*multipart.FileHeader` | 仅 multipart 文件 | ❌ 注册期 panic | — | — | string/binary | — |
| `[]*multipart.FileHeader` | 仅 multipart 文件 | ❌ 注册期 panic | — | — | array of binary | — |
| `any` / 接口 | ❌ | ❌ 注册期 panic | ✅ | ✅ | 空 schema（不精确） | 空 schema |
| `chan`/`func` 等 | ❌ | ❌ 注册期 panic | ❌ | ❌ | 空 schema | 空 schema |

> `path` 列不是 Form 链的普通来源：只接受标量、`time.Time` 与指向它们的多级指针；slice、`[]byte`、数组、map、struct、文件字段、chan/func 在注册期 panic（见 [routing.md](routing.md) 的“路径参数”）。`query/form/multipart` 列表示运行时绑定能力；`*time.Time` 与 `time.Time` 同规则。

补充：

- 自定义 `MarshalJSON`/`UnmarshalJSON` 的类型（如 `json.RawMessage`）不按底层类型精确映射；文档按底层 Go 类型生成，可能与实际线上 JSON 不同。
- 文件字段只用于 multipart 请求体，schema 不输出 `nullable`。
- 非 string key 的 map 在 OpenAPI description 中注明 key type。

## 三、默认值规则

### 1. 支持范围

```text
标量：string / bool / int* / uint* / float32 / float64
指针：标量的任意层指针（最多 32 层）
切片：元素为标量或标量指针的切片（[]string、[]int、[]*int、[]**int 等）
```

不支持：`time.Time`、`struct`、`*struct`、`[]struct`、`map`、`*map`、数组、`*[]T`、`[][]T`、`[]*[]T` 等。类型支持不等于值合法；值无法解析同样按未设置处理。

### 2. 两阶段

| 阶段 | 时机 | 条件 | 范围 |
| --- | --- | --- | --- |
| 注册阶段 | `buildEntry` | `hasDefault && IsZero()` | 注册期可达的零值字段：顶层、值类型 struct 链、固定数组元素；nil 切片/map 无元素，字段要等绑定创建元素后请求阶段补 nil 指针 |
| 请求阶段 | 绑定后 | `hasDefault && Kind==Ptr && IsNil()` | 绑定后子元素中的 nil 指针；slice/map/nested ptr 动态创建，固定数组元素已存在 |

- 值类型字段请求阶段一律跳过，避免覆盖用户显式传入的零值。
- 显式 JSON `null` 对指针会置 nil，请求阶段可能回填 default；对切片/map 会置 nil，且不会恢复注册期默认值。
- nil 切片/map/nil struct 指针在注册阶段无元素可填，请求阶段创建元素后才补 nil 指针子字段。

### 3. 容器深度

| 容器 | 默认值 | nonzero | default 展示 |
| --- | --- | --- | --- |
| `[]Struct` / `[]*Struct` | 仅指针字段请求阶段补填 | 递归每个元素 | 仅指针字段 |
| `map[K]Struct` / `map[K]*Struct` | 仅指针字段请求阶段补填 | 递归每个值 | 仅指针字段 |
| `[N]Struct` | 注册期预填值字段与指针字段 | 即使全零也递归 | 当前仅指针字段 |
| `[N]*Struct` | nil 元素不创建；绑定后仅补指针子字段 | 递归非 nil 元素 | 仅指针字段 |
| `[][]Struct`、`map[K][]Struct` 等多层 | 不穿透 | 不穿透 | 不展示¹ |
| `*[]Struct`、`*[N]Struct`、`*map[K]Struct` | 非 nil 时可递归；绑定后补指针子字段 | 非 nil 时递归 | 仅指针字段 |

¹ 若该 struct 类型同时被单层容器/值嵌套路径到达，按类型并集展示。

### 4. 告警

`default` 写在以下位置注册期只 `slog.Warn`，不 panic、不拒绝注册，字段永不填充：

1. 类型不支持（`time.Time`/struct/map/数组/指向容器的指针等）；
2. 类型支持但值无法解析（`int:"abc"`、`NaN`、切片含非法元素）；
3. 值类型字段所在 struct 非值嵌套可达；
4. 指针字段所在 struct 非 `applyDefaults` 可达（如 `map[K][]Struct` 内部）。

## 四、错误、状态码与限制

### 1. 错误类型

| 类型 | 产生位置 | `Error()` | 默认处理 |
| --- | --- | --- | --- |
| `*BindingError` | query/body/path 绑定失败 | 底层错误文案（`Err`） | 400 |
| `*ValidationError` | nonzero / `Validate()` | `Err` 非 nil 用其文案；否则 `field "<Field>" <Message>`；Field 空只返回 Message | 400 |
| 其他 error | handler/中间件 | 自定义 | 500 |

两者都支持 `errors.Is/As` 穿透（`Unwrap`）。默认 `OnValidationError` 会把 `err.Error()` 回显给客户端；`OnError` 只回显通用 `internal server error`，详情写日志。

### 2. 回调与状态码

| 回调 | 触发 | 默认状态码 | 默认响应 |
| --- | --- | --- | --- |
| `OnResponse` | handler 成功且未写响应 | 200 | `{data, code:0, message:"success"}` |
| `OnValidationError` | BindingError/ValidationError | 400 | `{data:null, code:400, message:err}` |
| `OnError` | 其他 error | 500 | `{data:null, code:500, message:"internal server error"}` |
| `OnNotFound` | 路由未命中 | 404 | `{data:null, code:404, message:"not found"}` |
| `OnPanic` | recover 到 panic | 500 | 通用 `internal server error` |

默认回调都先检查 `IsResponseWritten(w)`；已写响应则跳过。`WantHtml(r)` 为 true 时返回 HTML，否则 JSON。

### 3. 大小与数量限制

| 项 | 默认 | 说明 |
| --- | --- | --- |
| `MaxBodyBytes` | 32 MB | `MaxBytesReader` 包裹 body；0 表示框架不额外限制 |
| `MultipartFormMaxMemory` | 32 MB | multipart 内存缓冲；文件超出落盘；非文件值共享 +10 MB 预算 |
| urlencoded 解析上限 | 10 MB | 标准库 `ParseForm` 固有；超限 400 `http: POST too large` |
| body 排空上限 | 2 KB | 请求结束限量排空以复用连接 |
| 中间件位图层数 | 64 | 超过回退递归实现 |
| 指针/容器解引用深度 | 32 | 防自引用类型无限递归 |
| `default` 指针层数 | 32 | 第 33 层不支持 |

## 五、不变量

以下是公开契约或已锁死的核心行为：

1. `HttpEngine` 必须 `NewEngine()` 构造；默认回调全部装配，`MaxBodyBytes`/`MultipartFormMaxMemory` 均 32 MB。
2. handler 签名固定 `func(ctx context.Context, req Req) (Res, error)`；Req/Res 为 struct 或 `*struct`；注册期校验失败 panic。
3. 路由匹配：静态段 > 参数段 > 通配尾段；末尾斜杠归一化；参数段不匹配空段；通配可零段。
4. 绑定优先级：path > body > query；body 显式字段覆盖 query，body 缺失字段保留 query。
5. query/body 一旦参与绑定就严格：非法 query、已知字段转换失败、JSON 解码失败均 400。
6. 没有 Form 文本字段不解析 query；没有 body 可绑定字段不读取 body；`KeepRawBody` 不读取 body。
7. Form 链绑定名重复、JSON 名重复在注册期 panic（含匿名嵌入展开）。
8. 路径参数参数名必须对应字段；目标类型限标量/`time.Time`/多级指针；文件/slice/map/struct/chan 注册期 panic。
9. 中间件 `next` 每层最多一次；重复调用返回 `ErrNextCalledMultipleTimes`；链最内层是 core（校验 + handler）。
10. `OnResponse`/`OnError`/`OnValidationError`/`OnPanic` 使用最近一次成功传给 `next` 的 w/r；`OnNotFound` 使用初始包装 writer 与入口 request。
11. `written` 由 `WriteHeader`（最终状态码）/`Write`/`ReadFrom`/`Flush`/`Hijack` 置位；除 101 外的 1xx 与 `Push` 不置位；重复最终 `WriteHeader` 被包装层挡下。
12. OpenAPI 请求/响应角色分开生成 schema；响应不输出 `required`/`default`；请求 `required` 由 `nonzero` + 有效非零 default 推断。
13. OpenAPI 输出确定性：生成前对 method/path 排序，多次生成字节级一致。
14. 注册必须在服务对外提供请求前完成；运行期注册属数据竞争。

源码与测试对应关系见 [internals.md](internals.md) 各节，以及 `zchttp/docs_deviation_review_test.go`。

## 六、OpenAPI 映射速查

| 概念 | 请求 schema | 响应 schema |
| --- | --- | --- |
| 同一类型 | 裸名 `<Type>` | `<Type>Response` |
| `default` | 展示（可解析且上下文可达） | 不展示 |
| `nonzero` | 影响 `required` | 不参与 |
| `time_format` | 表单形态读；JSON 请求体不读 | 不读，恒 date-time |
| `[]byte` | 字节数组（0..255，nullable） | base64 `string/byte` |
| 值类型嵌套 struct | `$ref`，兄弟键可能被工具忽略 | `$ref` |
| 指针 struct | `{nullable:true, allOf:[$ref]}` | 同左 |
| 文件字段 | multipart `string/binary` | — |
| path 参数 | 所有方法都声明，`required:true`；可选/通配语义写 description | — |
| KeepRawBody | 非路径字段作 query，requestBody `*/*` | — |

完整的 schema 生成流程见 [internals.md](internals.md) 的“OpenAPI 生成器”一节，标签角色清单见 [openapi.md](openapi.md)。
