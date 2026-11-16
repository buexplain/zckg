# 约定、不变量与变更记录

本文档集中维护 zcconfig 的**不变量**、**被否决的设计方案**与**破坏性变更迁移**。其他文档只链接这里，不重复。

## 不变量（改代码前必须确认）

1. **`.env` 只存原文**：`LoadEnv` 只做文本处理（BOM / `export` / 配对引号 / 首尾空白），值按 `string` 存储；不在装载侧推断类型，`Env[T]` 在读取时解析。
2. **键缺失与类型不符二分**：key 不存在（含 Config 路径缺失、值为 nil）返回默认值；key 存在但无法转换（含 Config 路径中途非 map）panic `*TypeMismatchError`，不得静默回退。
3. **数值转换只允许静态无损子集**：同族加宽、无符号→更宽有符号、整型→浮点按尾数位；窄化、有符号→无符号（非加宽）、浮点→整数、数值→`time.Duration` 一律 panic。数值→bool 保持 C 风格。
4. **零拷贝 + 调用方只读**：`EnvAll` / `ConfigAll` 返回内部 map 引用；写操作会污染全局并与并发读取冲突。
5. **装载/注册全有或全无**：`LoadEnv` 整文件成功才合并；`Register` 的 `fn` 执行完才合并。
6. **启动期 fail-fast**：类型错误 panic；`LoadEnv` 的 I/O 错误返回 error。
7. **首行 BOM 剥离**：`.env` 首行（含 BOM 前有空白的情况）必须兼容。
8. **点分隔不可转义**：`Config` 的 `.` 是唯一层级分隔符；空路径段按空字符串键查找。
9. **驱动名大小写敏感**：DSN 格式只匹配 `mysql`/`postgres`/`postgresql`/`pgsql`/`sqlite`/`sqlite3` 小写字面量；未知或大小写不符返回空串。

## 被否决的方案（避免重复讨论）

| 方案 | 否决理由 |
|---|---|
| 装载时推断 int/float64/bool | 类型属于消费点；猜测会让 `Env[string]` 对纯数字 panic、并在 Config 侧制造静默截断/回绕。见迁移 1。 |
| 数值转换做值级校验（值放得下才允许） | ①`TypeMismatchError` 只有类型三元组，无法说明「值越界」；②同一配置项会因值不同时好时坏，排查成本高；③浮点精确性（`3.14`、NaN、-0）规则复杂。静态子集让「能否读」在启动期就是常量。 |
| `Config` 支持 `map[string]any` → struct 解码 | zcconfig 不是反序列化库；嵌套类型、tag、默认值语义会迅速膨胀。需要时用 `DBConfig` 这类显式结构体注册。 |
| `Config` 支持点号转义 | 会让路径语法变复杂（转义符、错误报告），而字面含 `.` 的 key 在配置树里本就罕见；明确不支持更可预测。 |
| 数值 → string 按数字格式化 | Go 的 `int→string` 是码点语义；若改成格式化，会与「类型不符即 panic」二分冲突，且丢失原文（`007`→`7`）。需要数字文本请先读数值再 `strconv`。 |
| `EnvAll` / `ConfigAll` 返回拷贝 | `ConfigAll` 的 `map[string]any` 值可能是 slice/指针/chan/func，任何浅/深拷贝都无法真正隔离；`EnvAll` 的 `map[string]string` 虽可安全拷贝，但统一零拷贝契约更简单，也避免两套 API 语义。 |
| 运行期热更新配置 | 与零拷贝引用、`Register` 的「启动期」约定冲突；需要热更应由外部配置中心 + 重建对象解决。 |

## 破坏性变更与迁移

> 状态：以下两项为当前工作区已实施、尚未发版的变更。按仓库 v1.x 约定，破坏性变更可走 MINOR，Release 说明必须置顶「⚠️ 破坏性变更」。

### 1. `.env` 值改为原文存储（取消装载侧类型推断）

| 旧写法 / 旧行为 | 新行为 | 迁移 |
|---|---|---|
| `Env[int]("PORT")` 读 `.env` `8080` | 仍为 `8080`（读取时解析） | 无需改 |
| `Env[string]("PORT")` 对 `8080` panic | 返回 `"8080"`（原文） | 无需改，或按需读取 |
| `Env[time.Duration]("TIMEOUT")` 对 `30` 得 `30ns` | panic（缺单位） | 写 `TIMEOUT=30s` |
| `Env[int8]("X")` 对 `200` 静默变 `-56` | panic（解析超范围） | 读更宽类型或修正配置 |
| `Env[int]("PRICE")` 对 `99.99` 静默变 `99` | panic（解析失败） | 读 `float64` 或修正配置 |
| `EnvAll()` 返回 `map[string]any`（int/float64/bool） | 返回 `map[string]string` | 用 `Env[T]` 取类型化值；`EnvAll` 仅用于只读导出 |
| `Env[bool]("B")` 对 `2` 得 `true` | panic（非法布尔文本） | 写 `true`/`false`/`1`/`0` |

### 2. Config 通道数值转换收紧为安全子集

| 旧行为 | 新行为 | 迁移 |
|---|---|---|
| 注册 `int(200)`，读 `int8` → `-56` | panic | 注册时写 `int8`，或按 `int` 读后自行转换 |
| 注册 `int(-1)`，读 `uint` → 巨大值 | panic | 注册 `uint` 或使用有符号类型 |
| 注册 `float64(99.99)`，读 `int` → `99` | panic（含 `100.0`） | 注册 `int`，或按 `float64` 读后显式取整 |
| 注册 `float64(3.14)`，读 `float32` → 精度损失 | panic | 注册 `float32` |
| 注册 `int(3306)`，读 `time.Duration` → `3.306µs` | panic | 注册 `time.Duration` 或 `"30s"` 文本 |
| 注册 `int32(42)`，读 `float64` | 仍为 `42`（32 ≤ 53 尾数位） | 无需改 |
| 注册 `int`，读 `int64` | 仍允许（同族加宽/等宽） | 无需改 |

## 版本与发版

- 版本号按 semver：新增公开 API → MINOR；破坏性 → MAJOR；修 bug/升依赖/注释整理 → PATCH。
- 本仓库处于 v1.x 时，破坏性变更可选择 **MINOR**（避免模块路径迁移 `/v2`），但 Release 说明必须置顶「⚠️ 破坏性变更」，给出旧/新调用对照与迁移写法。
- tag 使用 annotated tag；已发布 tag 绝不重指（Go 模块代理缓存不可变）。

## 文档维护规则

- **单一事实来源**：转换规则只在 [cast.md](cast.md)，错误模型只在 [errors.md](errors.md)，不变量只在本文档。
- **实现路径**：章节末尾列函数级锚点（`文件:函数`），不写死行号。
- **示例校验**：完整 `package main` 示例由 `docs_examples_compile_test.go` 编译；片段必须能作为函数体通过 `gofmt` 解析。
- **四者一致**：代码、源码注释、本文档集、测试在同一改动中同步；不一致按实现与实测为准修正文档。

相关章节：[README.md](README.md)（总览）、[internals.md](internals.md)（实现原理）。
