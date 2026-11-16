# 参数校验

本文说明 `zchttp` 的参数校验：`nonzero` 非零值校验、`Validate()` 自定义业务校验、执行顺序与错误处理。完整错误/状态码见 [reference.md](reference.md)，递归实现见 [internals.md](internals.md) 的“校验”一节。

适合：需要声明式必填或跨字段校验的业务开发者。

## 一、心智模型

- `nonzero` 是**声明式零值校验**：只检查 `reflect.Value.IsZero`，不做业务规则。
- `Validate()` 是**顶层 Req 的自定义校验**：在绑定和 nonzero 之后调用，可以访问全部字段。
- 两者都发生在 core 层、handler 之前；校验失败返回 `*ValidationError`，默认 400。
- 校验只看绑定/默认值填充后的最终值；`default` 不会让 nonzero 失效（默认值非零则通过，零值仍失败）。

## 二、nonzero

### 1. 声明与语义

```go
type Req struct {
    Name  string   `json:"name" nonzero:"true"`
    Age   int      `json:"age" nonzero:"false"`
    Email string   `json:"email"`
    Addr  Address  `json:"addr" nonzero:"true"`
    Note  *Invoice `json:"note"`
}

type Address struct {
    City       string `json:"city" nonzero:"true"`
    PostalCode string `json:"postalCode" nonzero:"true"`
}

type Invoice struct {
    Header string `json:"header" nonzero:"true"`
    Amount string `json:"amount" nonzero:"true"`
}
```

- `nonzero:"true"` 且零值 → 错误 `"is required"`；字段名用 Form 链绑定名，嵌套路径如 `addr.city`。
- `nonzero:"true"` 且非零 → 通过，若为 struct/指针/容器则继续递归。
- 未标注/`"false"`/非法值 → 不校验该字段；零值时通常跳过，固定数组例外。
- `Age nonzero:"false"` 与未标注等价；传 0 不报错。

### 2. 零值

`reflect.Value.IsZero`：nil 指针/切片/接口、空字符串、0、false、全零 struct 都是零值。

### 3. 递归范围

- 嵌套 struct、`*struct`（非 nil）、单层容器：`[]Struct`、`[]*Struct`、`[N]Struct`、`[N]*Struct`、`map[K]Struct`、`map[K]*Struct`，以及它们的非 nil 指针包裹。
- 固定数组即使整体为零也逐元素校验；nil/空切片、nil map 无元素可校验。
- 多层容器（`map[K][]Struct`、`[][]Struct` 等）不穿透。
- 切片/数组错误路径不附加索引（第 3 个 `items` 元素失败仍是 `items.name`）；map 追加 `fmt.Sprint(key)`，key 中的点号不转义。
- 匿名嵌入字段没有“父字段判零”环节，内部 nonzero 等价于顶层必填。

### 4. 与 default 的关系

- 校验看默认值填充后的值：`default:"x"` 的非空字符串通过 nonzero；`default:"0"`/`default:""` 仍是零值，失败。
- 只有“类型支持、值可解析、本身非零”的 default 才能让 OpenAPI 的字段变为可选；否则 required。非法/不支持的 default 不改变 required。
- 字段未传且无 default 的 nil 指针会失败 nonzero；显式传零值同样失败。

## 三、快速跳过

注册期 `hasNonzeroInTree` 扫描 Req 整棵类型树（穿透嵌套 struct/指针/切片/数组/map），任意深度存在 `nonzero:"true"` 就置 `needsNonzeroValidation=true`；请求期只有为 true 才执行遍历。该标记是传递性的，避免顶层无 nonzero 但嵌套层有时漏检。

## 四、Validate()

```go
type Validator interface {
    Validate() error
}
```

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

- 只有顶层 Req 的 `Validate()` 自动调用；嵌套 struct 需要在顶层 `Validate` 中手动调用。
- 值接收者或指针接收者均可；注册期用 `implementsValidator` 预判断。
- 返回 `nil` 或 typed-nil（如 `(*ValidationError)(nil)`）→ 通过。
- 返回 `*ValidationError` → 直接透传（可带 Field/Err）。
- 返回其他 error → 包装为 `*ValidationError{Message: err.Error(), Err: err}`，`errors.Is/As` 可穿透。
- `Validate()` 是运行时任意代码，不参与 OpenAPI 静态推导。

## 五、执行顺序与错误

一次请求的校验顺序：

```
绑定（query/body/path）
  → 请求阶段默认值（仅 nil 指针）
  → 中间件链
  → core：validateNonzero → validateCustom(Validate)
  → handler
```

- 绑定失败时 core 直接返回 `*BindingError`，不执行校验。
- 校验失败 → `*ValidationError`；默认 `OnValidationError` 返回 400，并把 `err.Error()` 回显。
- 其他错误（handler/中间件）→ `OnError`（默认 500）。
- 默认回调都先判断 `IsResponseWritten`；已写响应则跳过。

错误类型：

```go
type ValidationError struct {
    Field   string // 绑定名路径；业务校验可留空
    Message string
    Err     error  // 可选，支持 errors.Is/As
}

type BindingError struct {
    Message string
    Err     error
}

func NewValidationError(field, message string, err error) *ValidationError
func NewBindingError(err error) *BindingError
```

| 类型 | 产生位置 | Error() | 默认状态码 |
| --- | --- | --- | --- |
| `*BindingError` | query/body/path 绑定 | 底层错误文案 | 400 |
| `*ValidationError` | nonzero / Validate | Err 非 nil 用其文案；否则 `field "<Field>" <Message>`；Field 空只返回 Message | 400 |

## 六、代码与测试

| 概念 | 源码 | 测试 |
| --- | --- | --- |
| nonzero 递归、固定数组、map 路径 | `validate.go` | `validate_test.go` |
| hasNonzeroInTree | `validate.go` | `validate_test.go` |
| Validator、typed-nil、错误包装 | `validate.go`、`errors.go` | `validate_test.go`、`errors_test.go` |
| 请求顺序、错误分发 | `httpEngine.go` | `httpEngine_test.go` |

继续阅读：[reference.md](reference.md)（错误/限制/不变量）、[internals.md](internals.md)（校验实现）、[middleware.md](middleware.md)（校验在链中的位置）。
