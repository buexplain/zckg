# 错误模型与排查

zcconfig 把「配置缺失」与「配置错误」严格二分：缺失是正常路径（返回默认值），配置错误是启动期故障（panic 或 error）。

## 行为二分

| 情况 | 行为 | 载体 |
|---|---|---|
| key 不存在（`.env` 与 OS 都没有；Config 任一层级缺失） | 返回调用方默认值 `def` | 正常返回 |
| key 存在但值为 nil（仅 Config；Env 存储值恒为 string） | 返回 `def` | 正常返回 |
| key 存在、值可转换 | 返回转换结果 | 正常返回 |
| key 存在、值无法转换为 `T` | **panic** `*TypeMismatchError` | `Env[T]` / `Config[T]` |
| Config 路径中间层存在但不是 `map[string]any` | **panic** `*TypeMismatchError`，`Expected=map[string]any` | `Config[T]` |
| `.env` 文件打不开 / 读失败 / 单行超限 | 返回 `error`（消息含文件路径） | `LoadEnv` |

- `Register` 自身不返回 error、不主动 panic；`fn` 返回 nil 时静默忽略，`fn` 内的 panic 会原样向上传播（zcconfig 不 recover）。
- panic 策略的目的：配置错误应在进程启动阶段立刻暴露，而不是运行期某次读取才返回错误。

## `TypeMismatchError`

```go
type TypeMismatchError struct {
    Key      string       // 调用方传入的原始 key（Config 为完整路径）
    Expected reflect.Type // 目标类型 T 的静态类型（接口目标即接口类型；中间层非 map 时为 map[string]any）
    Actual   reflect.Type // 存储值的动态类型（Env 通道恒为 string）
}
```

消息格式固定为：

```text
zcconfig: key "<key>" 类型不匹配：期望 <Expected>，实际 <Actual>
```

实际样例：

```text
zcconfig: key "app.db" 类型不匹配：期望 zcconfig.DBConfig，实际 string
zcconfig: key "app.name.sub" 类型不匹配：期望 map[string]interface {}，实际 string
zcconfig: key "PORT" 类型不匹配：期望 int8，实际 string
```

> `Expected` 为 `map[string]any` 时，反射类型名显示为 `map[string]interface {}`（与 `fmt` 一致）。

## 捕获与诊断

推荐在 `main` / `init` 的最外层统一 recover，打印三要素后退出：

```go
func example() {
    defer func() {
        if r := recover(); r != nil {
            var mismatch *zcconfig.TypeMismatchError
            if err, ok := r.(error); ok && errors.As(err, &mismatch) {
                fmt.Printf("配置错误: key=%s expected=%v actual=%v\n", mismatch.Key, mismatch.Expected, mismatch.Actual)
            }
            panic(r) // 继续向上传播，保留原始 panic
        }
    }()
}
```

## 排查表

| 症状 | 常见原因 | 修复 |
|---|---|---|
| `Env[int]` panic，`Actual=string` | `.env`/OS 是文本，值不是合法整数（如 `8080.0`、`abc`、空值 `KEY=`） | 改成合法整数文本；空值不要当默认值用 |
| `Env[time.Duration]` panic | 文本缺单位（`30`）；数值源（Config 注册 int 30）被拒绝 | 写 `30s`，或注册 `time.Duration` 本身 |
| `Env[int8]`/`Env[uint]` panic | 文本超目标类型范围或负数转无符号 | 读成足够宽的整数，或修正配置值 |
| Config 数值读窄类型 panic | 数值安全子集拒绝窄化/跨族/浮点→整数（如 int 200→int8、float64 99.99→int） | 注册时就写目标类型，或按原类型读取 |
| Config 读 Duration panic | 注册的是裸数值（int/float64），单位不明 | 注册 `time.Duration` 或 `"30s"` |
| `Config[DBConfig]` panic | 注册值是 string/map，不是 `DBConfig` | 注册 `DBConfig{}` 值 |
| Config 路径中途 panic（期望 `map[string]interface {}`） | 路径中某层是标量，注册结构不对 | 修正注册路径或读取 key |
| 读不到值却返回了默认值 | key 拼写/点分隔（字面含 `.` 的 key 不可读）/Config 中间层 nil | 核对 key 与注册结构 |
| `LoadEnv` 返回 error | 文件不存在、目录、单行 ≥1MB（`bufio.ErrTooLong`） | 检查路径/文件；超长行拆行或用外部文件 |

## 实现路径

| 行为 | 实现 |
|---|---|
| nil → def、未命中 → panic、三字段填充 | `cast.go:castOrPanic` |
| Config 中间层非 map panic | `config.go:Config` |
| `LoadEnv` 错误包装（含路径） | `env.go:LoadEnv` |
| 消息格式 | `cast.go:TypeMismatchError.Error` |

## 代码与测试锚点

| 能力 | 测试 |
|---|---|
| 消息三要素格式 | `TestTypeMismatchError_Message` |
| panic 载荷字段 | `TestCastOrPanic_TypeMismatchDiagnostics` |
| Config 中间层非 map | `TestConfig_IntermediateNonMapPanics` |
| Config 类型不符 | `TestConfig_TypeMismatchPanics` |
| Env 类型不符 | `TestEnv_TypeConversion` |
| 加载错误含路径、超长行 | `TestLoadEnv_FileNotFound`、`TestLoadEnv_LineTooLong` |

相关章节：[cast.md](cast.md)（在哪一步失败）、[env.md](env.md)、[config.md](config.md)。
