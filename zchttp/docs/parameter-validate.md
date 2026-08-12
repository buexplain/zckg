# 参数校验规则

本文档介绍 `zchttp` 框架的参数校验机制，包括 `nonzero` 非零值校验、`Validate()` 自定义业务校验，以及校验的执行顺序与错误处理。Req 结构体的定义与标签总览见 `request.md`。相关实现位于 `validate.go`（校验执行）与 `meta.go`（标签解析）。

## 一、非零值校验（nonzero）

`nonzero` 标签的解析在注册阶段由 `buildStructMeta` 一次性完成并存入 `fieldMeta.nonzero`，运行时 `validateNonzero` 和 OpenAPI 文档生成均直接读取该预计算结果，两者天然一致。注意：`buildStructMeta` 仅计算当前结构体的顶层字段（`indices` 为单层），嵌套结构体的 meta 在递归校验时按需现场计算。

`nonzero` 标签与 `default` 标签**独立解析、互不影响**：

- **校验阶段**：只要字段标记了 `nonzero:"true"`，就始终校验零值（所见即所得），不受 `default` 标签影响。
- **文档生成阶段**：`nonzero:"true"` 且**没有** `default` 标签 → 文档中标记为**必填**；其它情况（有 `default`、或 `nonzero:"false"`、或未标注 `nonzero`）一律视为**可选**。详见 `openapi.md`。

### 递归校验

`validateNonzero` 会递归进入嵌套结构体、结构体指针字段、单层容器（`[]Struct`、`[]*Struct`、`[N]Struct`、`[N]*Struct`、`map[K]Struct`、`map[K]*Struct`，固定长度数组与切片行为一致）及其指针包裹形式（`*[]Struct`、`*[]*Struct`、`*[N]Struct`、`*map[K]Struct`、`*map[K]*Struct`，指针解引用后穿透进入元素）的元素，校验所有 `nonzero:"true"` 字段。多层容器（如 `map[K][]Struct`、`map[K][N]Struct`）的内部元素无法穿透，详见 `request.md` 中"容器嵌套深度限制"章节。规则如下：

| 本级字段 | 零值 | 行为 |
|---------|------|------|
| `nonzero:"true"` | 是 | 报错 `"is required"`（嵌套字段带绑定名路径，如 `"company.name"`），不递归 |
| `nonzero:"true"` | 否 | 校验通过，若为嵌套结构体/指针则递归进入子字段 |
| 未标注 `nonzero` | 是 | 跳过，不报错，不递归 |
| 未标注 `nonzero` | 否 | 不报本级，但若为嵌套结构体/指针则递归进入子字段 |

典型场景——收货地址必填，发票选填但填了就必须校验抬头和金额：

```go
type Req struct {
    Name    string   `json:"name" nonzero:"true"`
    Addr    Address  `json:"addr" nonzero:"true" description:"收货地址"`
    Invoice *Invoice `json:"invoice" description:"发票，选填"`
}

type Address struct {
    City       string `json:"city" nonzero:"true"`
    PostalCode string `json:"postalCode" nonzero:"true"`
    Phone      string `json:"phone"`
}

type Invoice struct {
    Header string `json:"header" nonzero:"true" description:"抬头"`
    Amount string `json:"amount" nonzero:"true" description:"金额"`
    Email  string `json:"email"`
}
```

- `Addr` 标记了 `nonzero:"true"` 且非零 → 递归校验 `City`、`PostalCode`。
- `Invoice` 未标 `nonzero`：`nil` → 跳过；非 `nil` → 递归校验 `Header`、`Amount`。

递归过程通过 `visited map[visitKey]bool`（`visitKey` 同时记录指针地址与类型，避免值类型首字段与父结构体共享地址时被误判为循环引用）记录已访问的节点，防止循环引用导致无限递归。

嵌套字段的校验错误路径使用**字段绑定名**（json/form tag，与 API 命名一致），例如 `company.name`、`addr.city`，便于客户端直接定位请求字段。

### 零值判定与快速跳过

运行时 `validateNonzero` 遍历 `meta.fields`，零值判定使用 `reflect.Value.IsZero`：nil 指针/切片、空字符串、数字 0、bool false 等均视为零值。

**快速跳过（注册期传递性预计算）**：注册阶段由 `hasNonzeroInTree` 扫描 Req **整棵类型树**（穿透嵌套结构体、指针、切片/数组/map 容器），判定任意深度是否存在 `nonzero:"true"` 字段，结果存入路由条目的 `needsNonzeroValidation` 标记。请求阶段仅当该标记为 `true` 时才执行 `validateNonzero` 遍历，全树无 nonzero 字段的接口整体跳过，避免无意义的反射遍历。

> 该标记是**传递性**的：顶层无 nonzero 但嵌套层有（如 `Addr Address` 未标注而 `City` 标了 nonzero）也计为 `true`。若仅统计 Req 顶层字段做跳过决策，会漏校验嵌套层 nonzero 字段，故扫描必须穿透整棵类型树。

要点：

- 校验仅针对显式 `nonzero:"true"` 的字段，普通字段传零值不会报错。
- `nonzero` 与 `default` 独立：带 `default` 的 `nonzero:"true"` 字段在请求阶段**仍然会校验零值**（所见即所得），但在 OpenAPI 文档中标记为可选。
- 嵌套结构体的 `nonzero` 字段仅在**父字段非零**时才会被递归校验；父字段为零值时子字段的 nonzero 被跳过。
- 仅顶层 Req 的 `Validate()` 方法会被自动调用；嵌套结构体若需自定义校验逻辑，请在顶层 `Validate()` 中手动调用。

## 二、自定义业务校验（Validate）

声明式 `nonzero` 只能表达单字段非零值。若需跨字段、业务规则等更丰富的校验，可让 `Req` 结构体实现 `Validator` 接口：

```go
type Validator interface {
    Validate() error
}
```

绑定与 `nonzero` 校验均通过后，若 `Req` 实现了该接口（值接收者或指针接收者均可），引擎会自动调用其 `Validate()`。注册阶段通过 `structMeta.implementsValidator` 预判断是否实现该接口，避免请求阶段重复反射：

```go
type CreateEventReq struct {
    Start int `json:"start" nonzero:"true"`
    End   int `json:"end" nonzero:"true"`
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

## 三、校验执行顺序与错误处理

`ServeHTTP` 的执行流程分为两阶段：

**阶段一：路由命中后立即绑定（`bindRequestData`）**

```
浅拷贝模板（含默认值）→ 深拷贝引用字段 → 绑定（query/form/multipart/JSON）
```

绑定完成后将 Req 注入 `ctx`，中间件可通过 `BoundReqFromContext` 提前检查。此时尚未做参数校验。若绑定失败，错误（`*BindingError`）随 Req 一同注入 ctx，core 层统一处理。

**阶段二：洋葱模型 core 层校验（`validateRequest`）**

```
validateNonzero → validateCustom(Validate)
```

所有中间件执行完毕后，core 层从 `ctx` 取出已绑定的 Req 进行校验。

任一校验失败都会产生 `*ValidationError`：

```go
type ValidationError struct {
    Field   string // 校验失败的字段名（绑定名路径，如 "company.name"，业务校验可留空）
    Message string // 失败原因
    Err     error  // 可选：包装底层错误，支持 errors.Is/As 穿透
}
```

`*BindingError`（绑定失败）与 `*ValidationError`（校验失败）在 `ServeHTTP` 中通过 `errors.As` 被识别，统一路由到 `OnValidationError` 回调（默认 `DefaultValidationErrorHandler`，返回 **400**）；其余非校验错误走 `OnError`（默认 **500**）。回调的自定义方式见 `http-engine-callback.md`。

## 四、相关文档

- Req 结构体定义与标签总览：`request.md`
- 参数绑定来源、字段类型、`time.Time` 解析、`default` 机制、文件上传：`parameter-binding.md`
- OpenAPI 文档生成、`OpenAPIMeta`、字段级文档标签：`openapi.md`
- 回调机制与错误分发、自定义响应/错误/校验失败处理：`http-engine-callback.md`
