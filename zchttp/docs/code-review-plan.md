# zchttp 代码审查计划

## 1. 审查目标

- **热路径正确性与性能**：注册期预计算（`buildEntry`）与请求期执行（`ServeHTTP`）的分工边界；池化对象（`chainRunner`、`json.Encoder`）复用时的状态重置。
- **绑定/校验/默认值三大机制**的语义正确性，尤其是两阶段默认值填充与容器穿透深度限制。
- **路由匹配正确性**：基数树回溯、可选参数展开、冲突检测、路径归一化。
- **请求生命周期安全**：panic 恢复覆盖面、错误分发、响应写入追踪、body 排空。
- **并发安全**：进程级缓存（`sync.Map`）、模板浅拷贝 + 深拷贝隔离、`sync.Pool` 对象跨请求污染。

## 2. 审查范围与批次

模块共 13 个源文件（约 2900 行）+ 每文件对应测试 + 性能基准，按请求生命周期分 5 批：

| 批次 | 主题 | 文件 | 行数 |
|---|---|---|---|
| ① 路由注册 | 路由表、基数树、注册期预计算 | `router.go`、`router_trie.go`、`buildEntry.go` | 540 |
| ② 元信息与绑定 | 标签解析、参数绑定、默认值 | `meta.go`、`binding.go`、`defaults.go` | 878 |
| ③ 校验与错误 | nonzero/Validate、错误类型 | `validate.go`、`errors.go` | 276 |
| ④ 引擎执行 | ServeHTTP、中间件链、上下文、响应包装 | `httpEngine.go`、`middleware.go`、`context.go`、`responseWriter.go` | 602 |
| ⑤ 文档生成 | OpenAPI 3.0 | `openapi.go` | 582 |

> ①→④ 按生命周期顺序审查可建立完整心智模型；⑤ 独立性强放最后。

## 3. 文件级审查清单

### 批次①：路由注册

- [ ] `router.go`：
    - `normalizePath`：前导 `/` 补全、末尾 `/` 裁剪、根路径与空串归一为 `/`、**多重斜杠不折叠**——注册与匹配两处使用同一函数。
    - 精确路由优先于参数路由的匹配顺序；未命中走 `OnNotFound`。
    - 冲突检测：同 method+path 重复注册 panic，错误信息含双方 handler 的函数名/文件/行号。
    - `Group` 前缀规范化（`normalizePrefix`）与嵌套分组前缀拼接；父分组中间件在子分组之前。
    - `Use` 返回自身链式调用；中间件**快照语义**（注册时固化，之后 Use 不影响已注册路由）。
    - 并发约束：routes map 与 paramTrees 运行期只读——确认代码注释与文档均明确"不支持运行期动态注册"。
- [ ] `router_trie.go`（103 行，逻辑密度高）：
    - 静态段优先、失败回溯参数分支的匹配算法；逐段子串扫描（`matchPath`）不预切分路径的边界（首/尾字符、空段）。
    - 可选参数 `{name?}` 的省略分支与命中分支**插入时一次性展开**——两分支 entry 一致性。
    - 捕获切片延迟分配的正确性（未命中参数时零分配）。
    - 插入期冲突检测：同位置参数名不一致、可选性不一致、参数模式重复。
- [ ] `buildEntry.go`：
    - handler 签名校验完整性：参数个数/类型、返回值个数/类型、nil、非函数——每种非法形态都 panic 且信息可定位。
    - 路径参数与 Req 字段绑定名（form > json > 字段名）的注册期校验；参数指向文件字段时 panic。
    - 预计算项逐一核对：`handlerVal`、`reqType/resType/reqElemType/reqIsPtr/resIsPtr`、`reqMeta/resMeta`、`defaultReq` 模板、`opMeta`、中间件快照、`needsDeepCopy`、`needsRequestPhaseDefaults`、`needsNonzeroValidation`、handler 位置信息。
    - `needsDeepCopy` 判定：模板中存在非 nil 指针/切片/map 或"元素内含引用的数组"——漏判会导致**跨请求共享可变状态**（高危）。
    - `hasNonzeroInTree` 的**传递性**扫描：穿透嵌套结构体、指针、切片/数组/map；循环引用类型是否会无限递归。

### 批次②：元信息与绑定

- [ ] `meta.go`：
    - 绑定名解析优先级 form > json > 字段名；取逗号前部分；`-` 与空名跳过；未导出字段跳过。
    - `cachedStructMeta` 进程级缓存（`sync.Map`）：缓存 key（类型）；并发首次构建的重复计算是否可接受（LoadOrStore）。
    - `implementsValidator` 预判断：值接收者与指针接收者两种实现均能识别。
- [ ] `binding.go`：
    - method/Content-Type 分流：GET/DELETE/HEAD → query；POST 等按 CT 分流；CT 参数剥离（`; charset=`）；无 CT 有 body 回退 JSON。
    - multipart 内存上限 32MB（`defaultMaxMemory`）；文件字段 `*multipart.FileHeader` 单/多文件绑定。
    - **尽力绑定**：单字段类型转换失败跳过保零值不报错——与**路径参数严格失败**（转换失败即 `BindingError` → 400）的双重语义边界清晰。
    - `bindPathParams` 在 query/body 之后执行且**覆盖**同名值；可选参数省略不写入。
    - `setScalar` 类型全覆盖：string/bool/int 全系/uint 全系/float/指针自动分配/切片多值。
    - `time.Time`：`time_format`（unix 系/layout）、自动探测（纯数字按位数推断精度、非数字依次尝试 `defaultTimeLayouts`）、`time_location` 失败降级 `time.Local` + `slog.Warn`。
    - JSON 路径：`json.Decode` 错误全部包装为 `*BindingError`；body 为空、非法 JSON、类型不匹配的分支。
- [ ] `defaults.go`（397 行，语义最复杂）：
    - `isDefaultSupported` 白名单：标量/标量指针/标量切片；不支持类型的标签**注册期 `slog.Warn`**。
    - 两阶段规则：注册阶段填所有 `IsZero()` 字段；请求阶段仅填 **nil 指针**（值类型跳过防覆盖用户显式零值）——对照文档"各字段两阶段填充明细"表逐行核对代码分支。
    - 容器穿透：单层容器（slice/数组/map，元素为 struct/*struct）及指针包裹形式可穿透；多层容器不穿透——穿透判定代码与文档表格一致。
    - map value 为值类型 struct 时的**写回**（值拷贝后 Set 回 map）；数组与切片行为一致性。
    - `deepCopyDefaults`：断开模板中指针/切片/map 共享引用——递归深度、数组内含引用的处理；漏一种类型即跨请求污染（高危）。
    - 切片默认值逗号分割的转义局限（值本身含逗号）是否文档化。

### 批次③：校验与错误

- [ ] `validate.go`：
    - `validateNonzero` 递归规则四象限（标注×零值）与文档表格一致；父字段零值时子字段跳过。
    - `visitKey`（指针地址+类型）防循环引用：值类型首字段与父结构体**共享地址**的场景不被误判——重点核对。
    - 错误路径用绑定名拼接（`company.name`）；`ValidationError.Field` 语义。
    - `validateCustom`：`Validate()` 返回 `*ValidationError` 透传、普通 error 包装（保留错误链，`errors.Is/As` 可穿透）。
    - `needsNonzeroValidation=false` 时整体跳过——与 `hasNonzeroInTree` 判定联动，确认不会漏检（嵌套层有 nonzero 但顶层无）。
- [ ] `errors.go`：`BindingError`/`ValidationError` 的 `Error()`/`Unwrap()` 实现；`errors.As` 可识别。

### 批次④：引擎执行

- [ ] `httpEngine.go`：
    - `NewEngine` 强制构造（默认回调全装配）；`ServeHTTP` 不再判 nil 回调——确认无绕过构造的路径（`HttpEngine{}` 字面量的防御说明）。
    - 执行流程八步（路由匹配 → recover → 绑定注入 ctx → 中间件链 → core 校验 → 反射调用 → 错误分发 → 收尾）与文档一致。
    - `defer recover` 覆盖**全生命周期**（含绑定、中间件、handler、回调自身 panic 是否也被捕获）。
    - 错误分发：`errors.As` 判 `*BindingError`/`*ValidationError` → `OnValidationError`(400)，其余 → `OnError`(500)。
    - 模板浅拷贝（`reflect.New` + Set）→ `needsDeepCopy` 时深拷贝 → 绑定 → `needsRequestPhaseDefaults` 时补填——顺序与条件开关。
    - 池化 `json.Encoder`（关闭 HTML 转义）：**Pool 取出后状态重置**、并发复用无残留；`<`/`>`/`&` 原样输出已文档化。
    - 请求收尾 body 排空上限 2KB（`maxBodyDrainBytes`）：超限不排空由 net/http 关连接——实现与注释一致。
    - `WantHtml` 判定（Accept 含 text/html 或 text/plain）在四个默认错误回调中的一致使用。
- [ ] `middleware.go`：
    - `runChain`：池化 `chainRunner` + `uint64` 位图防重——**归还 Pool 前状态清零**；`next()` 重复调用返回 `ErrNextCalledMultipleTimes` 不重执行下游。
    - 超过 64 层回退 `runChainRecursive`：语义与位图路径完全一致（防重、错误传播）。
    - `next()` 必须同 goroutine 调用的约束已注释（位图无并发保护）。
    - 短路语义：不调 `next()` 时 core 层（校验+handler）不执行。
- [ ] `context.go`：
    - 五个 FromContext 方法的 key 类型隔离（非导出 key 类型防碰撞）。
    - `BoundReqFromContext[T]`：T 与实际 Req 类型不匹配时的行为（错误 or 零值）；绑定错误随 Req 注入后的取出语义。
    - `BoundResFromContext[T]` 仅在 `next()` 返回后可用——注入时机核对。
- [ ] `responseWriter.go`：
    - `Written()` 追踪 `WriteHeader/Write/Flush/Hijack/Push` 五个入口；仅设 Header 不算已写入。
    - 接口透传完整性：底层 rw 支持 `Flusher/Hijacker/Pusher` 时包装层不丢失能力（接口断言组合）。

### 批次⑤：OpenAPI 生成

- [ ] `openapi.go`：
    - **三遍遍历**：`collectTypeUsages`（值嵌套可达）→ `collectDefaultsReachability`（defaults 可达）→ schema 构造 + `decorate`——两个可达性 map 的填充与消费逻辑对照文档 decorate 决策矩阵。
    - `$ref` 去重与递归/循环引用检测（自引用结构体不死循环）。
    - required 推断：`nonzero:"true"` 且无 `default` → 必填；类型映射表逐行核对（uint 加 `minimum: 0`、time 按 `time_format` 分支、指针 nullable 两种包裹形态、map 非 string key 注明）。
    - GET/DELETE/HEAD → query 参数；其余 → requestBody；含文件字段 → `multipart/form-data`。
    - `ResponseWrapper` 自定义包装：`interface{}` 字段识别为 data 占位符的替换逻辑；同名结构体去重策略（不同包同名类型是否冲突）。
    - 多层容器中 `default` 不展示 + 启动期 WARN 日志——与 `defaults.go` 的穿透判定使用同一套逻辑（不得各写一份产生偏差）。

## 4. 专项审查

### 4.1 并发与状态污染（最高优先级）

- [ ] `go test -race -count=1 ./zchttp/...` 全量。
- [ ] 三个共享点专项核查：
    1. `defaultReq` 模板：浅拷贝后引用字段是否全部被 `deepCopyDefaults` 断开（构造并发请求修改切片元素的测试）。
    2. `sync.Pool` 对象（chainRunner、Encoder）：归还前重置、取出后无上次请求残留。
    3. `cachedStructMeta` 的 `sync.Map`：只增不改，value 构建后不可变。

### 4.2 安全

- [ ] multipart 32MB 上限外还有无整体 body 大小限制（DoS 面评估，记录为改进建议）。
- [ ] 默认错误响应中 `err.Error()` 直接返回客户端：是否可能泄漏内部信息（路径、SQL 等），评估是否需要脱敏开关。
- [ ] panic 堆栈仅在 `WantHtml` 时返回页面：生产环境暴露堆栈的风险评估。

### 4.3 性能基准

- [ ] 运行 `go test -bench . -benchmem -run '^$' ./zchttp/`，记录基线；核对 `bench_perf_test.go` 覆盖：静态路由、参数路由、中间件链、绑定+校验全链路。
- [ ] 热路径分配审查：匹配零分配（无参数命中）、绑定阶段 reflect 分配次数。

## 5. 测试审查

- [ ] 13 个源文件均有对应 `_test.go`：抽查每个测试文件的用例名与批次③清单的对应关系，标记缺口。
- [ ] 重点补测建议（若缺失）：中间件中 panic、`next()` 跨 goroutine 调用（文档禁止，行为验证）、可选参数+default 组合、65 层中间件回退路径、multipart 超 32MB、Req 为指针类型 handler 的全流程。
- [ ] 执行：`go test -race -count=1 ./zchttp/...`，覆盖率目标 ≥ 80%（引擎与绑定核心文件 ≥ 90%）。

## 6. 文档一致性核对

- [ ] 七篇文档（routing/middleware/parameter-binding/parameter-validate/request/openapi/http-engine-callback）中的行为承诺逐条与实现比对；重点：两阶段填充明细表、容器穿透三张表、decorate 决策矩阵、执行流程八步。
- [ ] 文档间交叉引用的章节锚点有效性。

## 7. 产出物与完成标准

- **问题清单**：`文件:行号` + 复现请求（method/path/body/CT）+ 期望/实际行为 + 严重级别。
- **严重级别**：
    - Blocker：跨请求状态污染、panic 未被捕获、路由匹配错误、绑定值错位。
    - Major：默认值/校验语义与文档不符、池对象残留、trie 回溯遗漏、OpenAPI 可达性误判。
    - Minor：错误信息泄漏面、性能建议、日志级别。
    - Nit：命名、注释、文档锚点。
- **完成标准**：五批清单全部勾选；`-race` 通过；基准无明显回退；Blocker/Major 建立修复项。
