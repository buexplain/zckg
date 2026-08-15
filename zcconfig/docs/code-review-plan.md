# zcconfig 代码审查计划

## 1. 审查目标

- 确认 Env / Config 两条通道的**并发安全性**（RWMutex 使用是否正确、锁粒度是否合理、是否存在数据竞争）。
- 确认 `cast` 泛型转换引擎的**类型转换正确性**（优先级顺序、溢出处理、失败回退语义）。
- 确认 `.env` 解析的**边界场景健壮性**（畸形行、引号、注释、特殊字符）。
- 确认深度合并与路径查找逻辑的**语义正确性**（覆盖规则、副本隔离）。
- 核对代码行为与 `docs/zcconfig.md` 文档描述的一致性。

## 2. 审查范围与批次

模块共 4 个源文件（约 283 行）+ 2 个测试文件，规模小，可一次性完成，但按依赖顺序审查：

| 批次 | 文件 | 行数 | 理由 |
|---|---|---|---|
| ① | `cast.go` | 54 | 公共引擎，Env/Config 都依赖它 |
| ② | `env.go` | 105 | Env 通道完整链路 |
| ③ | `config.go` | 92 | Config 通道完整链路 |
| ④ | `dbConfig.go` | 32 | 纯数据结构，快速过一遍 |
| ⑤ | `cast_test.go`、`zcconfig_test.go` | — | 测试覆盖核对 |

## 3. 文件级审查清单

### 3.1 cast.go（类型转换引擎）

- [ ] 转换优先级是否严格遵循：`nil → 直接断言 → time.Duration → strconv → reflect → def`，顺序颠倒会产生语义偏差。
- [ ] **time.Duration 特判**：确认 Duration 分支在 strconv 之前；`ParseDuration` 失败时是否直接返回 `def` 而不会错误回退到 `ParseInt` 路径。
- [ ] **strconv 溢出检查**：`ParseInt`/`ParseUint` 是否传入了正确的 bitSize（如 int8 用 8），`"200"` → `int8` 这类溢出是否返回 `def` 而非截断值。
- [ ] **reflect.ConvertibleTo 陷阱**：Go 中 `int` → `string` 的 Convertible 判定为 true（会转成 Unicode 码点字符，如 `65` → `"A"`）。确认是否有防护，否则 `Env("PORT", "8080")` 读到 int 值时会产生乱码字符串——这是本文件最高优先级检查项。
- [ ] 负数转 uint、`"yes"` 转 bool 等非法转换是否返回 `def`（对照文档转换表逐行核对）。
- [ ] 泛型 `T` 为接口类型 / 指针类型 / 自定义类型（如 `type Port int`）时的行为是否符合预期。

### 3.2 env.go（.env 加载与读取）

- [ ] 逐行解析：注释行（`#` 开头）、空行、`export ` 前缀、无 `=` 的行、`=` 在值中出现多次（如 `URL=a=b`）的处理是否正确（应仅按第一个 `=` 分割）。
- [ ] `unquote`：仅去除**配对的**单/双引号；单边引号、引号内含引号的场景是否会误裁。
- [ ] `parseValue` 类型推断顺序：`ParseInt → ParseFloat → ParseBool → string`；确认 `"1"` 推断为 int 而非 bool 的取舍是否与文档一致；前导零字符串（如 `"007"`）、大整数溢出时的降级路径。
- [ ] `LoadEnv` 多次调用的**合并覆盖语义**：后加载覆盖先前，且写入全程持 `envMu.Lock`。
- [ ] `Env[T]`：查找顺序（envData → `os.LookupEnv` 回退 → def）；OS 环境变量取到的是 string，经 cast 转换的路径是否与 envData 路径行为一致。
- [ ] `EnvAll` 返回**浅拷贝**：确认 value 均为不可变标量所以浅拷贝安全；若未来存入引用类型是否有风险（可作为备注项）。
- [ ] 文件读取错误（不存在、权限）时的错误返回是否清晰、是否会污染已加载数据。

### 3.3 config.go（业务配置注册与读取）

- [ ] `Register`：`fn()` 是否确实在**锁外执行**（文档承诺，防止 fn 内部再调 Config 造成死锁）；fn 返回 nil map 时的行为。
- [ ] `ensureMapAt` 路径导航：中间层级已存在但**不是** `map[string]any` 时（如已注册了标量）的处理——是覆盖、报错还是静默丢弃，须与文档一致。
- [ ] `mergeMap` 深度合并：同名 key 且双方均为 map 时递归合并；一方为 map 一方为标量时的覆盖方向；合并是否会共享源 map 引用（外部持有 fn 返回的 map 再修改，是否穿透影响 configData）。
- [ ] `Config[T]` 路径查找：key 为空串、以 `.` 开头/结尾、连续 `..` 的行为；中间层级非 map 时返回 `def`；纯读操作全程 `RLock`。
- [ ] `ConfigAll` **深拷贝**：递归拷贝是否覆盖嵌套 map；非 map 的引用类型 value（slice、指针）是否仍共享——与 `EnvAll` 浅拷贝的不对称设计是否有意为之。
- [ ] `Reset` 是否同时清空两条通道且持有各自的写锁。

### 3.4 dbConfig.go（数据库配置结构体）

- [ ] 纯结构体定义，检查字段注释准确性、示例代码可编译性。
- [ ] **文档缺口**：`docs/zcconfig.md` 的"文件结构"一节未列出 `dbConfig.go`，审查时记录为文档问题。
- [ ] `DBSlaveConfig` 仅含 Host/Port/Username/Password（无 Database/Charset），确认与使用方（拼 DSN 的代码）的预期一致，是否刻意复用主库的 Database/Charset——如是，建议注释写明。

## 4. 专项审查

### 4.1 并发安全

- [ ] 用 `go test -race ./zcconfig/...` 全量跑一遍。
- [ ] 人工构造场景核查：并发 `LoadEnv` + `Env`、并发 `Register` + `Config`、`Register` 的 fn 内调用 `Config`（读写锁不可重入，确认 fn 在锁外执行使该场景安全）。
- [ ] `EnvAll`/`ConfigAll` 返回副本后，调用方修改副本不得影响内部存储。

### 4.2 API 设计与错误处理

- [ ] 所有失败路径都以"返回 def"静默兜底——确认这是有意设计；评估是否需要 Debug 级日志辅助排查配置未生效问题（记录为改进建议，不阻塞）。
- [ ] 泛型 API 的零值 def 陷阱：`Env("KEY", 0)` 无法区分"未配置"与"配置为 0"，确认文档已说明。

## 5. 测试审查

- [ ] `cast_test.go`（28 用例）：对照 3.1 清单核对是否覆盖 Duration、溢出、非法转换、reflect 路径；补充 int→string 码点陷阱用例（若缺失）。
- [ ] `zcconfig_test.go`（12 用例）：覆盖 LoadEnv 边界行、合并覆盖、Config 深合并、路径查找失败、OS 环境变量回退。
- [ ] 缺失项建议：并发压力测试、`Reset` 后再读取、`ConfigAll` 深拷贝隔离性断言。
- [ ] 执行：`go test -race -count=1 ./zcconfig/...`，记录覆盖率 `go test -cover`，目标 ≥ 85%。

## 6. 文档一致性核对

逐节对照 `docs/zcconfig.md`：

- [ ] 对外方法签名表与实际导出符号一致。
- [ ] string 源值 / 非 string 源值转换表逐行与 `cast.go` 行为一致。
- [ ] `.env` 解析规则五条与 `env.go` 实现一致。
- [ ] 文件结构一节补充 `dbConfig.go`。

## 7. 产出物与完成标准

- **问题清单**：每条含 `文件:行号`、问题描述、严重级别、修复建议。
- **严重级别**：
    - Blocker：数据竞争、类型转换产生错误值（如 int→string 码点问题）。
    - Major：边界解析错误、合并语义与文档不符。
    - Minor：错误信息不清晰、缺日志。
    - Nit：命名、注释、文档补齐。
- **完成标准**：清单全部勾选；`-race` 测试通过；Blocker/Major 均已建立修复项或确认为误报。
