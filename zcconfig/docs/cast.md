# 类型转换引擎：`cast`

`cast` 是 Env / Config 两条通道共用的唯一转换入口：

```text
func cast[T any](v any, def T) (T, bool)             // bool=false 表示未命中
func castOrPanic[T any](key string, v any, def T) T  // nil → def；未命中 → panic(*TypeMismatchError)
```

`cast` 只做判定与转换，不决定「失败怎么办」；`Env` / `Config` 通过 `castOrPanic` 把未命中升级为 panic（见 [errors.md](errors.md)）。

## 判定树（10 步）

```mermaid
flowchart TD
    S[v, def] --> N{v == nil?}
    N -->|是| MISS1[未命中 → def]
    N -->|否| A{v.(T) 直接断言?}
    A -->|成功| HIT[命中 → 返回]
    A -->|失败| IF{T 是接口?}
    IF -->|是| MISS2[未命中]
    IF -->|否| STR{源是 string?}
    STR -->|是| SB[Duration / int / uint / float / bool / string 解析]
    SB -->|成功| HIT
    SB -->|失败| MISS3[未命中]
    STR -->|否| B{目标 bool 且源数值?}
    B -->|是| BOOL[零值 false / 非零 true]
    BOOL --> HIT
    B -->|否| NS{目标 string 且源数值?}
    NS -->|是| MISS4[拒绝：防 65→A]
    NS -->|否| D{目标 Duration 且源数值?}
    D -->|是| MISS5[拒绝：裸数值不当纳秒]
    D -->|否| FAM{两侧都是数值族?}
    FAM -->|是| SAFE{安全子集?}
    SAFE -->|否| MISS6[拒绝]
    SAFE -->|是| REF
    FAM -->|否| REF[reflect: CanConvert]
    REF -->|成功| HIT
    REF -->|失败| MISS7[未命中]
```

| # | 步骤 | 为什么需要这一步 |
|---|---|---|
| 1 | `v == nil` → 未命中 | nil 表示「无值」，由读取层返回 def，不应报类型错 |
| 2 | 直接类型断言 `v.(T)` | 同类型最快路径；接口目标也必须命中这里 |
| 3 | 接口目标只接受直接断言 | Go 没有其他到接口的转换路径；值未实现接口即类型不符 |
| 4 | 源是 `string`：按目标 kind 解析 | 文本通道（`.env`、OS）与 Config 中的字符串值都走这里 |
| 5 | 数值 → `bool`：零 false / 非零 true | C 惯例；`bool` 不属于数值族，需单独判定 |
| 6 | 数值 → `string`：拒绝 | Go 的 `int→string` 是 Unicode 码点语义（65→"A"），不是配置期望 |
| 7 | 数值 → `time.Duration`：拒绝 | `Duration` 底层是 int64，放行会把裸数值当纳秒（3306→3.306µs） |
| 8 | 数值族之间：只允许安全子集 | 防 reflect 原生转换静默截断/回绕/丢精度 |
| 9 | reflect 兜底 `CanConvert` | 非数值转换：同底层结构体（忽略 tag）、切片→数组等 |
| 10 | 全部失败 | 返回 `(def, false)`，由 `castOrPanic` panic |

## 源是 `string`：文本 → T

| 目标类型 | 解析方式 | 成功示例 | 失败（panic） |
|---|---|---|---|
| `time.Duration` | `time.ParseDuration` | `"10s"`、`"1h30m"`、`"500ms"`、`"0"`/`"+0"`/`"-0"` | `"10"`（缺单位）、`"abc"` |
| 有符号整数 | `strconv.ParseInt(s, 10, bits)` | `"42"`、`"007"`→`7`、`"+5"`→`5` | `"200"`→`int8`（超范围）、`"abc"` |
| 无符号整数（含 `uintptr`） | `strconv.ParseUint(s, 10, bits)` | `"42"` | `"-1"`、`"abc"` |
| 浮点 | `strconv.ParseFloat(s, bits)`，拒绝 NaN/Inf | `"3.14"`、`"1e3"`→`1000` | `"NaN"`、`"Inf"`、`"1e999"`、`"abc"` |
| `bool` / 具名 bool | `strconv.ParseBool` | `"true"`、`"1"`、`"TRUE"` | `"yes"`、`"2"` |
| `string` / 具名 string | 直接断言或 `reflect` 转换 | `"hello"`、`""` | 不会失败 |
| 其他（struct/切片/指针/…） | 不支持 | — | 类型不符 |

要点：

- `bits` 取目标类型的实际位宽，因此 `Env[int8]` 对 `"200"` 在**解析阶段**就报错，而不是截断。
- 具名数值类型（`type Port int`）按底层 kind/位宽走同一张表。
- 空字符串 `""` 对 `string` 恒成功；对数值/bool/Duration 均解析失败（key 存在 → panic）。

## 源不是 string：Config 通道的 typed 值

### 数值 → bool

`int/uint/float`（不含 `complex`）按 C 惯例：零值 `false`、非零 `true`。具名 bool 也可作为目标。

### 数值 → string

一律拒绝，避免码点语义（`65 → "A"`）；需要数字文本请读取为数值后自行格式化。

### 数值 → time.Duration

一律拒绝：裸数值没有单位，放行会把数量级解释成纳秒。需要时长语义时注册 `time.Duration` 本身或 `"30s"`。

### 数值族安全子集

数值 kind 分四族：**有符号整数**（`int/int8/16/32/64`）、**无符号整数**（`uint/uint8/…/uintptr`）、**浮点**（`float32/64`）、**复数**（`complex64/128`）。转换只允许对源类型**任意取值都无损**的组合：

| 规则 | 内容 |
|---|---|
| R1 同族 | 目标位宽 ≥ 源位宽 → 允许；窄化 → 拒绝 |
| R2 无符号 → 有符号 | 目标位宽**严格大于**源位宽 → 允许（`uint8→int16`）；否则拒绝 |
| R3 有符号 → 无符号 | 一律拒绝（值可能为负） |
| R4 整数 → 浮点 | 源整型位宽 ≤ 目标尾数位（`float32`=24、`float64`=53）→ 允许；否则拒绝 |
| R5 浮点 → 整数 | 一律拒绝（可能带小数） |
| R6 复数 | 只参与 R1 同族加宽，不参与任何跨族转换 |

允许矩阵（单元格为允许条件）：

| 源 \ 目标 | 有符号 | 无符号 | float32 | float64 | complex64 | complex128 |
|---|---|---|---|---|---|---|
| 有符号 | 加宽/等宽 | 拒绝 | 位宽≤24 | 位宽≤53 | 拒绝 | 拒绝 |
| 无符号 | 严格加宽 | 加宽/等宽 | 位宽≤24 | 位宽≤53 | 拒绝 | 拒绝 |
| float32 | 拒绝 | 拒绝 | 等宽 | 加宽 | 拒绝 | 拒绝 |
| float64 | 拒绝 | 拒绝 | 拒绝 | 等宽 | 拒绝 | 拒绝 |
| complex64 | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 等宽 | 加宽 |
| complex128 | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 拒绝 | 等宽 |

**平台依赖**（无平台特例，按 `reflect.Type.Bits()` 取实际位宽）：

| 转换 | 64 位平台 | 32 位平台 |
|---|---|---|
| `int64 → int` | 允许（等宽） | 拒绝（窄化） |
| `int → int64` | 允许 | 允许（加宽） |
| `int → float64` | 拒绝（64 > 53） | 允许（32 ≤ 53） |
| `uint → int` | 拒绝（等宽且跨符号） | 拒绝 |

### reflect 兜底

两侧不是「都是数值族」时走 `reflect.Value.CanConvert`：

| 源 | 目标 | 结果 |
|---|---|---|
| 同底层或仅 struct tag 不同的结构体 | 结构体 | 成功（Go 转换规则比较 struct 类型时忽略 tag） |
| 长度足够的切片 | 数组 / 数组指针 | 成功；长度不足 → 未命中（`CanConvert` 已按值长度判定） |
| `[]byte` / `[]rune` 等 | 可转换类型 | 按 Go 规则 |
| `map[string]any` | 结构体 | 失败（不做 JSON 式解码） |
| `&T` | `T` | 失败（不自动解引用） |
| 数值 | 结构体/切片 | 失败 |

## 值 vs 指针、接口与具名类型

- **具名类型**：`type Port int` 与 `int` 可互转（同 kind/位宽，走 R1）；具名 string 与 string 可互转。
- **接口目标**：只接受直接类型断言；`Env[any]` 恒成功（string 实现了 `any`），`Env[error]` 对 string 值 panic。
- **指针**：`*T` 与 `T` 之间不自动转换。

## 实现路径

| 行为 | 实现 |
|---|---|
| 判定树与字符串解析 | `cast.go:cast` |
| 数值 → bool / string / Duration 防护 | `cast.go:cast` |
| 数值族判定与位宽规则 | `cast.go:isNumericFamilyKind`、`cast.go:numericFamily`、`cast.go:allowNumericConvert` |
| 失败升级为 panic、nil → def | `cast.go:castOrPanic` |

## 代码与测试锚点

| 能力 | 测试 |
|---|---|
| nil 与直接断言 | `TestCast_NilNotConvertible`、`TestCastOrPanic_NilReturnsDefault`、`TestCast_DirectAssertion` |
| 字符串→整数（含溢出/非法） | `TestCast_StringToIntSeries`、`TestCast_StringToIntOverflow`、`TestCast_StringToIntInvalid` |
| 字符串→无符号（含负数/溢出） | `TestCast_StringToUintSeries`、`TestCast_StringToUintNegative`、`TestCast_StringToUintOverflow`、`TestCast_Uintptr` |
| 字符串→浮点（含 NaN/Inf） | `TestCast_StringToFloatSeries`、`TestCast_StringToFloatInvalid`、`TestCast_StringToFloatNaNInf` |
| 字符串→bool | `TestCast_StringToBool`、`TestCast_StringToBoolInvalid` |
| Duration 文本解析与零值例外 | `TestCast_StringToDuration`、`TestCast_StringToDurationInvalid`、`TestCast_DurationUnitlessZero` |
| 字符串→string / 结构体拒绝 | `TestCast_StringToString`、`TestCast_NamedString`、`TestCast_StringToStructRejected` |
| 数值→bool（C 风格） | `TestCast_IntToBool`、`TestCast_UintToBool`、`TestCast_NumericToNamedBool`、`TestCast_ComplexToBoolRejected` |
| 数值→string 防护 | `TestCast_IntToStringRejected` |
| 数值族安全子集（允许/拒绝/平台/具名/ Duration） | `TestCast_NumericSafeSubsetAllowed`、`TestCast_NumericSafeSubsetRejected`、`TestCast_NumericPlatformWidth`、`TestCast_NumericNamedTypes`、`TestCast_NumericToDurationRejected` |
| 结构体/切片/数组 | `TestCast_StructToSameUnderlyingStruct`、`TestCast_SliceToArrayValueSafety` |
| 接口目标 | `TestCast_InterfaceTargetRequiresImplementation` |
| 错误载荷与诊断字段 | `TestTypeMismatchError_Message`、`TestCastOrPanic_TypeMismatchDiagnostics` |

相关章节：[env.md](env.md)（文本来源）、[config.md](config.md)（typed 值来源）、[errors.md](errors.md)（失败语义）。
