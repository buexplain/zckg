# zcconfig 配置模块

## 概述

zcconfig 是一个轻量级配置加载与读取模块，提供两条独立的配置通道：

- **Env 通道**：从 `.env` 文件读取环境配置，支持自动类型推断和 OS 环境变量回退
- **Config 通道**：业务侧注册嵌套配置树，支持 `.` 分隔的路径递归查找

两条通道完全独立，各自维护存储和锁，互不干扰。

## 文件结构

```
zcconfig/
├── cast.go            # 泛型类型转换引擎（内部）
├── env.go             # .env 文件加载与 Env 泛型读取
├── config.go          # 业务配置注册与 Config 泛型读取
├── dbConfig.go        # 数据库连接配置结构体（DBConfig / DBSlaveConfig）
├── cast_test.go       # cast 函数测试
└── zcconfig_test.go   # env 与 config 集成测试
```

## 架构设计

```
┌─────────────────────────────────────────────────────┐
│                    zcconfig 包                        │
│                                                      │
│  ┌─────────────── Env 通道 ──────────────┐           │
│  │                                      │           │
│  │  LoadEnv(path)                       │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  .env 文件 → 逐行解析                │           │
│  │    │           │                     │           │
│  │    ▼           ▼                     │           │
│  │  unquote()  parseValue()            │           │
│  │  (去引号)    (类型推断)              │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  envData map[string]any              │           │
│  │  (sync.RWMutex 保护)                 │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  Env[T](key, def)                   │           │
│  │    ├─ 查 envData                     │           │
│  │    ├─ 回退 os.LookupEnv              │           │
│  │    └─ cast(v, def) 类型转换          │           │
│  │                                      │           │
│  └──────────────────────────────────────┘           │
│                                                      │
│  ┌───────────── Config 通道 ────────────┐           │
│  │                                      │           │
│  │  Register(key, fn)                   │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  fn() 立即执行 → map[string]any      │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  ensureMapAt() 导航/创建路径         │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  mergeMap() 深度合并到 configData    │           │
│  │                                      │           │
│  │  configData map[string]any           │           │
│  │  (sync.RWMutex 保护)                 │           │
│  │    │                                 │           │
│  │    ▼                                 │           │
│  │  Config[T](key, def)                 │           │
│  │    ├─ 按 "." 分割路径                │           │
│  │    ├─ 逐层递归查找 map[string]any    │           │
│  │    └─ cast(v, def) 类型转换          │           │
│  │                                      │           │
│  └──────────────────────────────────────┘           │
│                                                      │
│  ┌───────────── 公共引擎 ───────────────┐           │
│  │                                      │           │
│  │  cast[T any](v any, def T) T         │           │
│  │    1. nil → 返回 def                  │           │
│  │    2. 直接类型断言 v.(T)             │           │
│  │    3. string → time.ParseDuration    │           │
│  │    4. string → strconv 精确解析      │           │
│  │    5. reflect ConvertibleTo 通用转换  │           │
│  │    6. 均失败 → 返回 def              │           │
│  │                                      │           │
│  └──────────────────────────────────────┘           │
└──────────────────────────────────────────────────────┘
```

## 对外方法

### Env 通道

| 方法 | 签名 | 说明 |
|------|------|------|
| `LoadEnv` | `func LoadEnv(path string) error` | 读取 `.env` 文件，解析键值对存入 env 存储。值自动推断类型（int / float64 / bool / string），支持注释行、`export` 前缀（大小写不敏感，兼容多空格/Tab）、引号去除。单行上限 1MB（超出返回错误）。多次调用合并数据，后加载覆盖先前。 |
| `Env` | `func Env[T any](key string, def T) T` | 按 key 查找 env 值并转换为 T 类型。查找顺序：先查 `LoadEnv` 加载的数据，再回退 OS 环境变量。未找到或转换失败时返回默认值 `def`。 |
| `EnvAll` | `func EnvAll() map[string]any` | 返回内部 env 存储的直接引用（零拷贝，不含 OS 环境变量）。**仅限只读场景**：写操作会污染全局配置并与并发读取产生数据竞争，详见[架构约定](#架构约定)。 |

### Config 通道

| 方法 | 签名 | 说明 |
|------|------|------|
| `Register` | `func Register(key string, fn func() map[string]any)` | 注册业务配置。`fn` 在调用时**立即执行**，返回的数据按 `key` 路径深度合并到配置树。`key` 为空字符串时合并到根节点。多次调用深度合并，同名 key 后注册覆盖先前的值。约定：`fn` 返回的 map 在 Register 之后不得再被修改（zcconfig 不拷贝返回值，直接存储引用）；若 `key` 路径中间层级已存在标量值，注册子节点会覆盖原标量为 map。 |
| `Config` | `func Config[T any](key string, def T) T` | 按 `.` 分隔的 key 递归查找嵌套 `map[string]any{}`，找到后转换为 T 类型返回。路径不存在或中间层级非 map 时返回默认值 `def`。纯读操作，使用 `RLock`。 |
| `ConfigAll` | `func ConfigAll() map[string]any` | 返回内部配置存储的直接引用（零拷贝）。**仅限只读场景**：写操作会污染全局配置并与并发读取产生数据竞争，详见[架构约定](#架构约定)。 |

### 辅助方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `reset` | `func reset()` | 清空所有 env 数据和 config 数据（**小写未导出**，仅测试场景内部使用）。 |

## 类型转换引擎 cast

`cast` 是内部函数（小写，不导出），被 `Env` 和 `Config` 共用，负责将 `any` 类型的存储值转换为目标泛型类型 `T`。

### 转换优先级

```
1. v == nil              → 返回 def
2. v.(T) 直接断言        → 命中则返回
3. string → time.Duration → time.ParseDuration（如 "10s"、"1h30m"）
4. string → strconv      → ParseInt / ParseUint / ParseFloat / ParseBool
5. reflect 转换          → ConvertibleTo 检查（如 int→float64, int64→int）
6. 全部失败               → 返回 def
```

> **time.Duration 特殊处理**：`time.Duration` 底层为 `int64`，若不特殊处理会走 `strconv.ParseInt` 路径，但 `"10s"` 等格式无法被 `ParseInt` 解析。因此在 strconv 分支之前优先用 `time.ParseDuration` 解析，解析失败直接返回 `def`，不回退到 `ParseInt`。`Env` 和 `Config` 共用同一个 `cast`，所以两者都自动支持 `time.Duration`。

### string 源值转换表

| 源值示例 | 目标类型 | 转换路径 | 结果 |
|---------|---------|---------|------|
| `"42"` | `int` | `strconv.ParseInt` | `42` |
| `"-42"` | `int` | `strconv.ParseInt` | `-42` |
| `"42"` | `int8` | `strconv.ParseInt` + 溢出检查 | `42` |
| `"200"` | `int8` | `strconv.ParseInt` + 溢出检查 | 返回 `def`（超出范围） |
| `"42"` | `uint` | `strconv.ParseUint` | `42` |
| `"-1"` | `uint` | `strconv.ParseUint` | 返回 `def`（负数无法转 uint） |
| `"3.14"` | `float64` | `strconv.ParseFloat` | `3.14` |
| `"100"` | `float64` | `strconv.ParseFloat` | `100.0` |
| `"NaN"` / `"Inf"` / `"-Inf"` | `float64` | `strconv.ParseFloat` | 返回 `def`（NaN/Inf 视为非法配置值） |
| `"1e999"` | `float64` | `strconv.ParseFloat` | 返回 `def`（溢出为 Inf，带范围错误） |
| `"true"` | `bool` | `strconv.ParseBool` | `true` |
| `"1"` | `bool` | `strconv.ParseBool` | `true` |
| `"0"` | `bool` | `strconv.ParseBool` | `false` |
| `"yes"` | `bool` | `strconv.ParseBool` | 返回 `def`（非法布尔值） |
| `"10s"` | `time.Duration` | `time.ParseDuration` | `10 * time.Second` |
| `"1h30m"` | `time.Duration` | `time.ParseDuration` | `90 * time.Minute` |
| `"500ms"` | `time.Duration` | `time.ParseDuration` | `500 * time.Millisecond` |
| `"10"` | `time.Duration` | `time.ParseDuration` | 返回 `def`（缺少时间单位） |
| `"abc"` | `int` | `strconv.ParseInt` | 返回 `def`（解析失败） |
| `"hello"` | `string` | 直接类型断言 | `"hello"` |
| `"hello"` | `struct{}` | default 分支 | 返回 `def` |

### 非 string 源值转换表

| 源值 | 目标类型 | 转换路径 | 结果 |
|------|---------|---------|------|
| `42` (int) | `float64` | `reflect.ConvertibleTo` | `42.0` |
| `int64(100)` | `int` | `reflect.ConvertibleTo` | `100` |
| `3.14` (float64) | `float32` | `reflect.ConvertibleTo` | `float32(3.14)` |
| `42` (int) | `bool` | 不可转换 | 返回 `def` |
| `true` (bool) | `int` | 不可转换 | 返回 `def` |
| `42` (int) | `int` | 直接类型断言 | `42` |

## .env 文件格式

```env
# 注释行（以 # 开头）
APP_NAME="my app"
PORT=8080
DEBUG=true
PRICE=99.99
export EXPORTED_KEY=exported_value
EMPTY_KEY=
NO_QUOTE=hello world
```

**解析规则**：
- `#` 开头的行跳过
- 空行跳过
- `export` 前缀自动去除（**大小写不敏感**，`export` 后跟任意数量的空格/Tab 均可识别；无空白分隔的 `exportKEY=v` 视为普通 key）
- `=` 左侧为 key（去除首尾空格）
- `=` 右侧为 value（去除首尾空格，再去除配对的单/双引号）
- 值自动推断类型：`ParseInt`（按平台 int 位宽，超出范围保持 string）→ `int`，`ParseFloat`（仅含 `.eE` 的值尝试）→ `float64`，`ParseBool` → `bool`，均失败 → `string`
- **单行上限 1MB**：超过上限的文件加载失败并返回错误（默认 `bufio.Scanner` 上限仅 64KB，zcconfig 已调大缓冲区以支持内联证书、长密钥等超长配置值）

## 线程安全

- `envData` 由 `envMu`（`sync.RWMutex`）保护：`LoadEnv` 写时用 `Lock`，`Env` / `EnvAll` 读时用 `RLock`
- `configData` 由 `configMu`（`sync.RWMutex`）保护：`Register` 写时用 `Lock`，`Config` / `ConfigAll` 读时用 `RLock`
- `Register` 中 `fn()` 在锁外执行，仅合并操作持锁，避免 fn 内逻辑阻塞其他读者

## 架构约定

- **零拷贝**：`ConfigAll` / `EnvAll` 返回内部存储的**直接引用**（不做任何拷贝），**仅限只读场景**（调试输出、配置导出等）。任何写操作都会污染全局配置，并与并发读取（`Config` / `Env` / `Register` / `LoadEnv`）产生数据竞争，可能导致程序崩溃。例如：

  ```go
  all := zcconfig.ConfigAll()
  all["app"].(map[string]any)["name"] = "modified" // ❌ 危险：写穿内部存储
  ```

  返回值的生命周期与内部存储一致——外部保存的引用在后续 `Register` / `LoadEnv` 合并后仍指向同一底层数据（深合并不替换 map 根节点），**不要在返回值上做任何写操作**。
- **启动期注册、运行期只读**：`Register` / `LoadEnv` 仅允许在启动阶段调用，运行期配置只读。
- **注册后不可变**：`Register` 的 `fn` 返回的 map 及其嵌套结构在 Register 之后不得再被修改；zcconfig 不拷贝返回值，直接存储引用。
- 模块选择不引入任何拷贝（浅拷贝或深拷贝）的原因：`map[string]any` 的 value 可为任意 Go 类型，部分拷贝（无论深浅）无法覆盖 slice/指针/chan/func 等全部引用类型，反而制造虚假安全感。

## 使用示例

### 读取 .env 文件

```go
package main

import "github.com/buexplain/zckg/zcconfig"

func main() {
    // 加载 .env 文件
    if err := zcconfig.LoadEnv(".env"); err != nil {
        panic(err)
    }

    // 读取配置，自动类型推断
    port := zcconfig.Env("PORT", 8080)          // int
    debug := zcconfig.Env("DEBUG", false)       // bool
    name := zcconfig.Env("APP_NAME", "default") // string
    price := zcconfig.Env("PRICE", 0.0)         // float64

    // 不存在的 key 返回默认值
    timeout := zcconfig.Env("TIMEOUT", 30)      // int, 默认 30

    // .env 中不存在时会回退到 OS 环境变量
    home := zcconfig.Env("HOME", "/tmp")        // string
}
```

### 注册与读取业务配置

```go
package main

import "github.com/buexplain/zckg/zcconfig"

func main() {
    // 注册根级配置
    zcconfig.Register("", func() map[string]any {
        return map[string]any{
            "app_name": "myapp",
            "debug":    true,
        }
    })

    // 注册子模块配置（key 支持 . 分隔的路径）
    zcconfig.Register("app.database", func() map[string]any {
        return map[string]any{
            "host": "localhost",
            "port": 3306,
        }
    })

    // 注册嵌套配置
    zcconfig.Register("app.cache", func() map[string]any {
        return map[string]any{
            "redis": map[string]any{
                "host": "127.0.0.1",
                "port": 6379,
            },
        }
    })

    // 通过 . 路径读取
    host := zcconfig.Config("app.database.host", "127.0.0.1") // string
    port := zcconfig.Config("app.database.port", 3306)        // int
    cacheHost := zcconfig.Config("app.cache.redis.host", "")  // string
    appName := zcconfig.Config("app_name", "")                // string
    debug := zcconfig.Config("debug", false)                  // bool

    // 不存在的路径返回默认值
    missing := zcconfig.Config("app.notexist", "fallback")    // string

    // 获取内部配置存储的直接引用（零拷贝，仅限只读场景，详见"架构约定"）
    all := zcconfig.ConfigAll()
    _ = all
}
```

### time.Duration 支持

`Env` 和 `Config` 都支持将字符串自动解析为 `time.Duration`：

```go
// .env 文件：
//   SEND_DEADLINE = "10s"
//   RETRY_DELAY = 500ms

zcconfig.LoadEnv(".env")

// Env 通道
deadline := zcconfig.Env("SEND_DEADLINE", 5*time.Second)       // 10 * time.Second
retryDelay := zcconfig.Env("RETRY_DELAY", 100*time.Millisecond) // 500 * time.Millisecond

// Config 通道
zcconfig.Register("server", func() map[string]any {
    return map[string]any{
        "timeout": "30s",
        "poll_interval": "2s",
    }
})
timeout := zcconfig.Config("server.timeout", 10*time.Second)        // 30 * time.Second
pollInterval := zcconfig.Config("server.poll_interval", 1*time.Second) // 2 * time.Second
```

支持的 Duration 格式遵循 Go `time.ParseDuration` 规范，如 `"10s"`、`"1h30m"`、`"500ms"`、`"100us"`、`"100ns"` 等。纯数字（如 `"10"`）缺少时间单位，解析失败返回默认值。

### 多次注册深度合并

```go
// 第一次注册
zcconfig.Register("app", func() map[string]any {
    return map[string]any{
        "name": "myapp",
        "port": 3000,
    }
})

// 第二次注册，同名 key 覆盖，新 key 新增
zcconfig.Register("app", func() map[string]any {
    return map[string]any{
        "port":  8080,  // 覆盖 3000
        "debug": true,  // 新增
    }
})

// 结果：app.name = "myapp"（保留）, app.port = 8080（覆盖）, app.debug = true（新增）
```
