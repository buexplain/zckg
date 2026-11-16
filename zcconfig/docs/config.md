# Config 通道：注册、合并与路径读取

适用范围：`Register`、`Config`、`ConfigAll` 的对外契约与实现原理。转换细节见 [cast.md](cast.md)，错误模型见 [errors.md](errors.md)。

## 5 分钟上手（完整示例，编译级校验）

```go
package main

import (
	"fmt"

	"github.com/buexplain/zckg/zcconfig"
)

func main() {
	// key 为空字符串 → 合并到配置树根节点
	zcconfig.Register("", func() map[string]any {
		return map[string]any{
			"app_name": "demo",
			"debug":    true,
		}
	})

	// 按 "." 路径注册子模块；同名 key 后注册覆盖先前的值
	zcconfig.Register("app.database", func() map[string]any {
		return map[string]any{
			"host": "127.0.0.1",
			"port": 3306,
		}
	})

	name := zcconfig.Config("app_name", "")            // string
	debug := zcconfig.Config("debug", false)           // bool
	host := zcconfig.Config("app.database.host", "")   // 逐层查找
	port := zcconfig.Config("app.database.port", 0)    // int
	fmt.Println(name, debug, host, port)
}
```

## 数据模型

```text
configData map[string]any   // 嵌套层级只认 map[string]any
configMu   sync.RWMutex     // Register 写 Lock；Config / ConfigAll 读 RLock
```

- 值可以是任意 Go 类型（结构体、切片、指针……），但**中间层级**必须是 `map[string]any`，否则按配置结构错误处理。
- `Config` 是纯读操作；`Register` 仅允许启动阶段调用（见 [conventions.md](conventions.md)）。

## 注册：`Register(key, fn)`

执行顺序：

1. **立即执行** `fn()`（不持锁），返回的 map 直接存引用，不做拷贝；
2. `fn` 返回 nil → 静默忽略；
3. 加锁后按 key 分派：
   - `key == ""` → `mergeMap(configData, data)`，合并到根节点；
   - 否则 `parts := strings.Split(key, ".")` → `ensureMapAt(configData, parts)` 拿到/创建目标 map → `mergeMap(target, data)`。

### `ensureMapAt`：路径三态

沿路径逐段检查 `current[part]`：

| 当前值 | 处理 |
|---|---|
| 不存在 | 新建 `map[string]any` 并挂上，继续深入 |
| 存在且是非 nil 的 `map[string]any` | 直接复用，继续深入 |
| 存在但是 nil map 或**非 map 的标量**（string/int/...） | 新建空 map 覆盖原值，继续深入 |

即：在 `"app.name"` 是 string 的情况下注册 `"app.name.sub"`，原标量会被替换成 map。

### `mergeMap`：深度合并

对源 map 的每个键：

| 源值 | 目标值 | 行为 |
|---|---|---|
| `map[string]any`（含 nil map） | 非 nil `map[string]any` | 递归合并；源是 nil 时递归空转，目标保持不变 |
| `map[string]any` | nil map / 非 map / 键不存在 | 目标被源 map 替换 |
| 标量 | 任意 | 直接覆盖同名键 |

`Register` 的 `fn` 返回 nil（整个源 map 为 nil）时直接返回，不执行合并。

> 因此「nil 源 map 不清空已有非 nil 目标 map」只发生在**嵌套值**且目标已是非 nil map 的场景；若目标键不存在、是 nil map 或非 map，则该键会被替换成源 map（源为 nil map 时读取该路径得到 `def`）。

### 合并场景速查

| 场景 | 结果 |
|---|---|
| 两次 `Register("app", ...)`，同名 key | 后注册覆盖；新 key 新增；未提及的旧 key 保留 |
| 先 `Register("", ...)` 再 `Register("app", ...)` | 根节点保留，`app` 作为新子树合并 |
| 同一路径先标量后子树 | 标量被替换为 map（`ensureMapAt`） |
| `fn` 返回 nil | 不做任何操作 |
| 目标节点是 nil map，源是非 nil map | 目标被源替换 |

## 读取：`Config[T](key, def)`

1. 加 `RLock`；
2. `parts := strings.Split(key, ".")`，从 `configData` 开始逐层取 `map[string]any`；
3. 任一层不存在 → 返回 `def`；
4. 到达最后一段 → `castOrPanic(key, 值, def)`；
5. 中间层存在但**不是** `map[string]any`：
   - 值为 nil → 返回 `def`（视为未配置）；
   - 否则 panic `*TypeMismatchError`，`Expected=map[string]any`、`Actual=实际类型`。

**路径边界（重要）**

- `key` 中的 `.` **一律**是层级分隔符，无法转义：注册在 map 里、字面含 `.` 的 key 无法通过路径读到，会静默回退 `def`。
- 空路径段按**空字符串键**查找：`Config("app.", ...)` 等价于查找 `data["app"][""]`。
- `key == ""` 查找根层的 `""` 键；`Register("")` 只做合并、不创建 `""` 键，因此通常返回 `def`。
- 点分段不支持数组下标、通配符或大小写折叠。

**nil / 缺失 / 类型不符三态**

| 情况 | 行为 |
|---|---|
| 任一层级缺失 | 返回 `def` |
| 中间层或叶子值为 nil | 返回 `def` |
| 叶子存在且可转换 | 返回转换结果 |
| 叶子存在但转换失败 | panic |
| 中间层存在但不是 map | panic（Expected 记为 `map[string]any`） |

## `ConfigAll()`

返回 `configData` 的直接引用（零拷贝）。与 `EnvAll` 相同的只读约束；另外注意「深合并不替换 map 根节点」：`Register("")` 合并进同一个根 map，因此先前保存的引用仍然有效。详见 [internals.md](internals.md)。

## 实现路径

| 行为 | 实现 |
|---|---|
| 注册入口、fn 锁外执行、key 分派 | `config.go:Register` |
| 路径导航与三态覆盖 | `config.go:ensureMapAt` |
| 深度合并 | `config.go:mergeMap` |
| 路径读取、nil / 非 map 二分 | `config.go:Config` |
| 零拷贝导出 | `config.go:ConfigAll` |
| 转换与 panic | `cast.go:cast`、`cast.go:castOrPanic` |

## 代码与测试锚点

| 能力 | 测试 |
|---|---|
| 基础查找与嵌套注册 | `TestConfig_BasicLookup`、`TestConfig_NestedRegistration` |
| 默认值、fn 立即执行、多次注册合并 | `TestConfig_DefaultValues`、`TestConfig_ImmediateExecution`、`TestConfig_MergeRegistration` |
| nil 叶子 / nil 中间节点返回 def | `TestConfig_NilValueReturnsDefault` |
| 中间层非 map panic | `TestConfig_IntermediateNonMapPanics` |
| 点分隔不可转义、空路径段 | `TestConfig_DotSeparatorNotEscapable` |
| fn 返回 nil 无操作 | `TestRegister_NilDataNoop` |
| nil map 节点初始化 | `TestRegister_NilMapNodeInitialization` |
| 合并边界与递归合并 | `TestRegister_MapMergeBoundaries`、`TestRegister_NestedMapRecursiveMerge` |
| 标量路径被子树覆盖 | `TestRegister_ScalarThenSubtreeOverwrite` |
| 数值转换拒绝/允许 | `TestConfig_NumericConversionRejectsLossy` |
| ConfigAll 内容与零拷贝 | `TestConfigAll`、`TestConfigAll_ZeroCopy` |
| 并发注册与读取 | `TestConcurrent_RegisterAndConfig` |
| 切片→数组、complex→bool、Duration 字符串 | `TestConfig_SliceToArrayConversion`、`TestConfig_ComplexBoolPanics`、`TestConfig_DurationUnitlessStrings` |

相关章节：[cast.md](cast.md)（值→T）、[errors.md](errors.md)（诊断）、[internals.md](internals.md)（锁与零拷贝）。
