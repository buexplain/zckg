# 请求结构体（Req）定义与校验规则

本文档介绍 handler 的请求结构体（`Req`）如何定义、支持哪些标签，以及绑定完成后的校验规则。参数绑定的来源与类型转换细节见 `parameter-binding.md`；本文聚焦结构体定义与校验。相关实现位于 `binding.go` 与 `buildEntry.go`。

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
    Name string `json:"name" required:"true"`
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
| `required` | 必填校验 | `"true"` 必填、`"false"` 可选，参与运行时非零值校验与 OpenAPI 文档 |
| `ignore` | 文档排除 | `ignore:"true"` 仅将字段从 OpenAPI 文档中排除，不影响绑定与校验 |
| `example` / `description` | 文档信息 | 仅用于 OpenAPI 文档生成，详见 `openapi.md` |

此外，可在 `Req` 中嵌入 `zchttp.OpenAPIMeta` 声明操作级元信息（`tags`/`summary`/`description`），它是空结构体，不参与绑定与校验，详见 `openapi.md`。

```go
type ListUserReq struct {
    zchttp.OpenAPIMeta `tags:"User/Account" summary:"用户列表"`
    Keyword string `json:"keyword" default:"" description:"搜索关键字"`
    Page    int    `json:"page" default:"1"`
    Status  string `json:"status" required:"true"`
    secret  string // 未导出，跳过
}
```

### default 标签支持范围

`default` 标签（由 `isDefaultSupported` 判定）仅对以下类型生效：

- 标量：`string`、`bool`、`int`/`int8`/`int16`/`int32`/`int64`、`uint`/`uint8`/`uint16`/`uint32`/`uint64`、`float32`/`float64`
- 标量的切片：`[]string`、`[]int` 等
- 标量的指针：`*string`、`*int` 等

以下类型设置 `default` 标签无效（标签值被忽略，不会参与模板预填充与 OpenAPI schema 生成）：

- `struct`、`*struct`、`[]struct`、`[]*struct`
- `map[K]V`、`*map[K]V`
- 任意非标量的指针/切片/数组

## 三、必填校验（required）

必填判定在注册阶段由 `buildStructMeta` 一次性计算并存入 `fieldMeta.required`，运行时 `validateRequired` 和 OpenAPI 文档生成均直接读取该预计算结果，两者天然一致。注意：`buildStructMeta` 仅计算当前结构体的顶层字段（`indices` 为单层），嵌套结构体的 meta 在递归校验时按需现场计算。

判定优先级如下：

1. 字段带 `default` 标签且类型支持 default（`isDefaultSupported`）→ **可选**（有默认值即非必填，优先级最高）。
2. 否则带 `required` 标签：`required:"true"` → **必填**；`required:"false"` → **可选**。
3. 否则（未标注 `required`）→ **可选**（不做类型推断）。

### 递归校验

`validateRequired` 会递归进入嵌套结构体和结构体指针字段，遍历整棵结构体树校验所有 `required:"true"` 字段。规则如下：

| 本级字段 | 零值 | 行为 |
|---------|------|------|
| `required:"true"` | 是 | 报错 `"is required"`，不递归 |
| `required:"true"` | 否 | 校验通过，若为嵌套结构体/指针则递归进入子字段 |
| 未标注 `required` | 是 | 跳过，不报错，不递归 |
| 未标注 `required` | 否 | 不报本级，但若为嵌套结构体/指针则递归进入子字段 |

典型场景——收货地址必填，发票选填但填了就必须校验抬头和金额：

```go
type Req struct {
    Name    string   `json:"name" required:"true"`
    Addr    Address  `json:"addr" required:"true" description:"收货地址"`
    Invoice *Invoice `json:"invoice" description:"发票，选填"`
}

type Address struct {
    City       string `json:"city" required:"true"`
    PostalCode string `json:"postalCode" required:"true"`
    Phone      string `json:"phone"`
}

type Invoice struct {
    Header string `json:"header" required:"true" description:"抬头"`
    Amount string `json:"amount" required:"true" description:"金额"`
    Email  string `json:"email"`
}
```

- `Addr` 为 required 且非零 → 递归校验 `City`、`PostalCode`。
- `Invoice` 未标 required：`nil` → 跳过；非 `nil` → 递归校验 `Header`、`Amount`。

递归过程通过 `visited map[uintptr]bool` 记录已访问的结构体指针，防止循环引用导致无限递归。

### 零值判定与快速跳过

运行时 `validateRequired` 遍历 `meta.fields`，零值判定使用 `reflect.Value.IsZero`：nil 指针/切片、空字符串、数字 0、bool false 等均视为零值。`structMeta.hasRequired` 标记是否存在 required 字段，若为 `false` 则直接跳过遍历，加速请求阶段校验。

要点：

- 校验仅针对显式 `required:"true"` 的字段，普通字段传零值不会报错。
- `default` 与 `required:"true"` 同时出现时，注册阶段即判定为 `hasDefault=true`、`required=false`，避免「有默认值却又必填」的矛盾。
- 嵌套结构体的 `required` 字段仅在**父字段非零**时才会被递归校验；父字段为零值时子字段的 required 被跳过。
- 仅顶层 Req 的 `Validate()` 方法会被自动调用；嵌套结构体若需自定义校验逻辑，请在顶层 `Validate()` 中手动调用。

## 四、自定义业务校验（Validate）

声明式 `required` 只能表达单字段非零值。若需跨字段、业务规则等更丰富的校验，可让 `Req` 结构体实现 `Validator` 接口：

```go
type Validator interface {
    Validate() error
}
```

绑定与 `required` 校验均通过后，若 `Req` 实现了该接口（值接收者或指针接收者均可），引擎会自动调用其 `Validate()`。注册阶段通过 `structMeta.implementsValidator` 预判断是否实现该接口，避免请求阶段重复反射：

```go
type CreateEventReq struct {
    Start int `json:"start" required:"true"`
    End   int `json:"end" required:"true"`
}

func (r CreateEventReq) Validate() error {
    if r.Start >= r.End {
        return &zchttp.ValidationError{Field: "end", Message: "must be greater than start"}
    }
    return nil
}
```

`Validate()` 返回值处理：

- 返回 `nil` → 校验通过；
- 返回 `*ValidationError` → 直接透传（可携带 `Field` 等结构化信息）；
- 返回其他普通 `error` → 自动兜底包装为 `*ValidationError`（保留原始错误链，`errors.Is/As` 可穿透）。

> `Validate()` 是运行时任意 Go 代码，不会被反射提取为 OpenAPI schema 约束，因此不影响生成的文档。

## 五、校验执行顺序与错误处理

`ServeHTTP` 的执行流程分为两阶段：

**阶段一：路由命中后立即绑定（`bindRequestData`）**

```
浅拷贝模板（含默认值）→ 深拷贝引用字段 → 绑定（query/form/multipart/JSON）
```

绑定完成后将 Req 注入 `ctx`，中间件可通过 `BoundReqFromContext` 提前检查。此时尚未做参数校验。若绑定失败，错误（`*BindingError`）随 Req 一同注入 ctx，core 层统一处理。

**阶段二：洋葱模型 core 层校验（`validateRequest`）**

```
validateRequired → validateCustom(Validate)
```

所有中间件执行完毕后，core 层从 `ctx` 取出已绑定的 Req 进行校验。

任一校验失败都会产生 `*ValidationError`：

```go
type ValidationError struct {
    Field   string // 校验失败的字段名（绑定名），业务校验可留空
    Message string // 失败原因
    Err     error  // 可选：包装底层错误，支持 errors.Is/As 穿透
}
```

`*BindingError`（绑定失败）与 `*ValidationError`（校验失败）在 `ServeHTTP` 中通过 `errors.As` 被识别，统一路由到 `OnValidationError` 回调（默认 `DefaultValidationErrorHandler`，返回 **400**）；其余非校验错误走 `OnError`（默认 **500**）。回调的自定义方式见 `http-engine-callback.md`。

## 六、相关文档

- 参数绑定来源、字段类型、`time.Time` 解析、`default` 机制、文件上传：`parameter-binding.md`
- OpenAPI 文档生成、`OpenAPIMeta`、字段级文档标签：`openapi.md`
- 回调机制与错误分发、自定义响应/错误/校验失败处理：`http-engine-callback.md`
