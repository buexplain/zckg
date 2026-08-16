# zchttp 递归函数与方法审查计划

> 本计划用于审查 zchttp 模块全部**涉及递归的方法和函数**：梳理递归清单、递归函数之间的调用关联（自递归 / 运行时互递归 / 跨阶段间接递归链），并逐一证明或证伪"是否会死递归（栈溢出 / 无限循环）"。审查结论请按第 9 节"输出建议"整理到 `docs/recursion-review-report.md`。

## 1. 审查目标

本次审查要回答两个问题：

1. **递归清单是否完整**：zchttp 中每一处递归（含自递归、互递归、经第三方调用回环的间接递归、`for` 循环式的"递归等价"展开）是否都被识别、登记并建立调用关联图。
2. **每个递归函数/方法是否会死递归**：对每个递归点给出终止性证明或反例。判定"会死递归"的标准是：存在**合法可达的输入**（合法的 Go 类型定义、合法的运行时数据、合法的调用方式）使递归不终止——栈溢出（死递归）或 `for` 循环永不退出（死循环）均计入。

每个问题都必须给出：函数名与位置、递归形式、终止性证明或反例（含可复现的最小用例）、风险分级、建议修复方向。

## 2. 审查范围

### 2.1 代码基准

- **13 个非测试源码文件全部审查**：`binding.go`、`buildEntry.go`、`context.go`、`defaults.go`、`errors.go`、`httpEngine.go`、`meta.go`、`middleware.go`、`openapi.go`、`responseWriter.go`、`router.go`、`router_trie.go`、`validate.go`。
- **代码基准以当前工作区为准**（含未提交变更），`git diff -- zchttp/` 显示本批变更集中在 `binding.go`/`defaults.go`/`httpEngine.go`/`meta.go`/`responseWriter.go`/`router.go`/`validate.go`（08-16 13:33–13:35），递归相关逻辑（`validateNonzeroWalk` 的数组分支、`deepCopyDefaults` 的 Interface 分支等）刚发生过联动修改，属高风险区。
- **排除**：`*_test.go` 测试文件与 `docs/` 下的文档/审查产物不作为被审查对象（测试代码不随生产代码发布；但测试中已有的防环断言可作为第 6 节验证手段复用）。

### 2.2 递归清单（计划阶段初步清点，审查时逐一确认）

| # | 函数/方法 | 位置 | 递归形式 | 现有终止/防环机制 | 初步评估 |
|---|---|---|---|---|---|
| 1 | `routeNode.matchPath` | router_trie.go:94 | 自递归（static 分支 + param 分支） | path 逐段缩短（`rest` 严格短于 `path`） | ✅ 结构上必然终止 |
| 2 | `runChainRecursive` | middleware.go:121 | 自递归（`middlewares[1:]`） | 切片每次缩短 1 | ✅ 必然终止 |
| 3 | `chainRunner.exec` ↔ `chainRunner.advance` | middleware.go:98、110 | 运行时互递归（exec → next → advance → exec） | 层号 `current+1` 单调递增 + 位图防重（`calledBits`） | ✅ 有界（≤ 64 层位图 / 超出走 #2），需核 goroutine 约束 |
| 4 | `buildStructMeta` | meta.go:98 | 自递归（匿名嵌入 struct 展开） | **无** | ⚠️ 见可疑点 6/7 → ✅ 已修复（REC-01，见报告） |
| 5 | `isStructLike` | meta.go:223 | `for` 循环迭代解指针（递归等价） | **无上限** | ⚠️ 见可疑点 3 → 证伪（触发路径被编译器拒绝）+ 已加固（REC-07） |
| 6 | `checkUnsupportedDefaults` | defaults.go:29 | 自递归 + 切片/数组多层容器展开 `for` 循环 | `visiting` 类型级防环（仅 struct 生效）；**展开循环无保护** | ⚠️ 见可疑点 1 → ✅ 已修复（REC-05，展开循环加上限） |
| 7 | `applyDefaultsWithVisiting` | defaults.go:156 | 自递归（struct/ptr/slice/array/map 五分支） | `visitKey` 实例级防环（struct 地址 / map 桶指针 / map 指针值） | 基本安全，需核边界（见可疑点 9） |
| 8 | `deepCopyDefaults` | defaults.go:307 | 自递归（struct/ptr/slice/map/array/interface 六分支） | **无** | ⚠️ 潜在风险（见可疑点 8）→ ✅ 已加固（REC-06，深度上限） |
| 9 | `hasRefFields` | defaults.go:367 | 自递归 | Ptr/Slice/Map 分支短路返回，不进入内容 | ✅ 必然终止 |
| 10 | `hasRequestPhaseDefaults` | defaults.go:415 | 自递归 | `visiting` 类型级防环（仅 struct 生效）；**顶层 Slice/Map 穿透在防环检查之前** | ⚠️ 见可疑点 2 → ✅ 已修复（REC-02） |
| 11 | `isDefaultSupportedDepth` | defaults.go:488 | 自递归（Slice/Ptr 透传） | 深度上限 `maxPtrDerefDepth = 32` | ✅ 有界终止 |
| 12 | `derefType` | meta.go:65 | `for` 循环迭代解指针 | 上限 32 | ✅ 有界终止 |
| 13 | `walkTypeUsage` | openapi.go:170 | 自递归（struct 树） | `visiting` 类型级防环；不进入 slice/map 自身 | ✅ 自身终止（但依赖 `buildStructMeta`，见 #4） |
| 14 | `walkDefaultsReachability` | openapi.go:254 | 自递归（struct 树） | 同上 | ✅ 同上 |
| 15 | `typeToSchema` | openapi.go:594 | 自递归（Ptr/Slice/Map/Struct 四分支） | Struct 分支经 `registerStructSchema` 占位防环；**Ptr/Slice/Map 分支无防环无上限** | ⚠️ 见可疑点 4 → ✅ 已修复（REC-04，depth 上限） |
| 16 | `registerStructSchema` | openapi.go:552 | 经 `typeToSchema` 间接递归 | `typeNames` 先占位后展开（struct 环安全） | ✅ struct 环安全 |
| 17 | `coerceExample` | openapi.go:707 | 自递归（array items） | 递归深度 = schema 嵌套深度（有限） | ✅ 必然终止 |
| 18 | `hasNonzeroInTree` | validate.go:83 | 自递归 | `visiting` 类型级防环（仅 struct 生效）；**顶层 Slice/Map 穿透在防环检查之前** | ⚠️ 见可疑点 2 → ✅ 已修复（REC-03） |
| 19 | `validateNonzeroWalk` | validate.go:166 | 自递归（struct/ptr/slice/array/map 五分支） | `visitKey` 实例级防环（struct 地址 / map 桶 / map 指针值，**标记不删除**） | 基本安全，需核边界（见可疑点 10） |

> 计数说明：不递归但参与防环的支撑函数（`acquireVisitMap`/`releaseVisitMap`/`fieldByIndex`/`cachedStructMeta`/`setScalar` 等）不在本清单，但作为递归函数的依赖一并核对。

### 2.3 递归调用关联（三个阶段的调用链）

**注册阶段**（`Router.register` / `RouterGroup.*`）：

```
Router.register
  ├─ buildEntry
  │    ├─ buildStructMeta(Req) / buildStructMeta(Res)      ← 递归 #4（无防环，全链最上游风险）
  │    ├─ applyDefaults → applyDefaultsWithVisiting        ← 递归 #7
  │    ├─ hasRefFields                                     ← 递归 #9
  │    ├─ hasRequestPhaseDefaults                          ← 递归 #10（缺陷）
  │    └─ hasNonzeroInTree                                 ← 递归 #18（缺陷）
  └─ checkUnsupportedDefaults                              ← 递归 #6（缺陷）
```

**请求阶段**（`HttpEngine.ServeHTTP`）：

```
ServeHTTP
  ├─ deepCopyDefaults                     ← 递归 #8（无防环，潜在）
  ├─ bindRequestData / bindPathParams     （无递归）
  ├─ applyDefaults(requestPhase) → applyDefaultsWithVisiting   ← 递归 #7
  ├─ validateRequest → validateNonzero → validateNonzeroWalk   ← 递归 #19
  └─ runChain
       ├─ ≤64 层：chainRunner.exec ↔ advance   ← 运行时互递归 #3
       └─ >64 层：runChainRecursive            ← 递归 #2
```

**OpenAPI 生成阶段**（`GenerateOpenAPI`）：

```
GenerateOpenAPI
  ├─ collectTypeUsages → walkTypeUsage          ← 递归 #13（自身防环，但内部调用 buildStructMeta #4）
  ├─ collectDefaultsReachability → walkDefaultsReachability  ← 递归 #14（同上）
  └─ buildOperation → buildQueryParams/buildPathParams/buildRequestBody/buildResponses
       └─ typeToSchema                          ← 递归 #15（缺陷）
            ├─ Struct → registerStructSchema    ← 递归 #16（struct 环安全）
            └─ array 分支 → coerceExample       ← 递归 #17
```

**路由匹配阶段**：`Router.matchParam → routeNode.matchPath` ← 递归 #1。

**共享弱点链**：`buildStructMeta`（#4）无防环，而它是注册阶段（buildEntry）、OpenAPI 阶段（walkTypeUsage/walkDefaultsReachability/registerStructSchema）与请求阶段（cachedStructMeta）三方的共同入口——一处缺陷打击四条链。`type S []S` 一类合法自引用类型同时打击 #6/#10/#15/#18。

## 3. 判定维度与风险分级

### 3.1 终止性证明四要素

对每个递归点，按以下要素逐一核验：

| 要素 | 判定 | 示例（本模块） |
|---|---|---|
| 单调递减 | 每次递归调用是否在某个良序度量上严格下降（长度、层号、深度、地址数量） | `matchPath` 的 path 缩短、`runChainRecursive` 的切片缩短 |
| 类型级防环 | 对自引用/互引用的**类型**（编译期可见的环）是否有 `visiting map[reflect.Type]bool` 且位置正确（先检查再递归） | `walkTypeUsage` 正确；`hasRequestPhaseDefaults`/`hasNonzeroInTree` 的 Slice/Map 穿透在检查**之前**（缺陷） |
| 实例级防环 | 对自引用/互引用的**运行时数据**（`a.Next = &a`、map 自引用等）是否有 `visitKey`（地址+类型）防环且键选择正确（临时副本、零尺寸 struct、nil map 桶指针等边界） | `applyDefaultsWithVisiting`/`validateNonzeroWalk` 基本正确，需核边界 |
| 深度上限 / 占位注册 | 无法用上述方式防环时是否有人工上限或先占位后展开 | `derefType`/`isDefaultSupportedDepth`/`setScalar` 上限 32；`registerStructSchema` 先占位 |

### 3.2 风险分级

| 级别 | 判定标准 | 典型场景 |
|---|---|---|
| 高 | 存在合法可达输入使函数不终止（栈溢出/死循环），且该输入无需恶意构造——合法 Go 类型定义或正常注册/请求流程即可触发 | `type E1 struct{ *E1 }` 作为 Req → `buildStructMeta` 栈溢出；`type S []S` 作为 Req 字段 → `hasRequestPhaseDefaults`/`hasNonzeroInTree` 栈溢出、`typeToSchema` 栈溢出 |
| 中 | 需要刻意构造的运行时数据（环状指针/map）才能触发，正常流程（JSON 反序列化、标量默认值）不会产生 | `deepCopyDefaults` 遇环状数据（当前模板数据无环，属潜在风险）；`validateNonzeroWalk` 对非 JSON 来源的环状数据 |
| 低 | 防环机制存在但位置/键选择/边界有瑕疵，极端情形下误判（漏检或误跳过），不导致不终止 | 零尺寸 struct 共享地址导致 visited 误判、`isTempCopy` 边界、`maxPtrDerefDepth=32` 对超深合法类型的误报 |

### 3.3 反例构造的三类输入（已实证的合法 Go 类型）

计划阶段已用独立模块实测（`go build` 验证），**以下类型定义均为合法 Go 代码**（仅值嵌入互引用被编译器拒绝），是各递归点对抗用例的核心武器：

| 用例 | 定义 | 合法性 | 打击目标 |
|---|---|---|---|
| U1 切片自引用 | `type S []S` | ✅ 合法 | #6（展开循环）、#10、#15、#18 |
| U2 map 自引用 | `type M map[string]M` | ✅ 合法 | #10、#15、#18 |
| U3 指针自引用 | `type P *P` | ✅ 合法 | #5（`isStructLike`）、#15 |
| U4 指针互引用 | `type A *B; type B *A` | ✅ 合法 | #5、#15 |
| U5 嵌入自身指针 | `type E1 struct{ *E1 }` | ✅ 合法 | #4（`buildStructMeta`） |
| U6 嵌入互引用 | `type E2 struct{ *E3 }; type E3 struct{ *E2 }` | ✅ 合法 | #4 |
| U7 值嵌入互引用 | `type V1 struct{ V2 }; type V2 struct{ V1 }` | ❌ 编译器拒绝（invalid recursive type） | #4 值嵌入路径天然安全（对照基准） |
| U8 接口自引用 | `type I interface{ M() I }` | ✅ 合法 | 各函数的 Interface 分支（预期跳过，核对无反射穿透） |
| U9 运行期环数据 | `a.Next = &a`；`m["x"] = a`（map 自引用）；`a.L = []A{a}; a.L[0].L = a.L` | ✅ 合法构造 | #7、#8、#19（实例级防环） |

## 4. 逐函数核对要点

### 4.1 `routeNode.matchPath`（router_trie.go:94）

- [ ] 证明 `rest` 严格短于 `path`（首段切分逻辑对 `path = "/a"`、`"/a/b"`、尾部无 `/` 的归一化路径逐一推演）。
- [ ] 空串基例：`entries[0]` 优先、可选参数省略分支（`entries[1]`）的顺序与回退正确性。
- [ ] `append(captured, seg)` 在 param 分支的切片共享是否会因回溯污染捕获值（终止性之外的正确性，顺带核对）。
- [ ] 结论应为：**终止（结构保证）**，无死递归可能。

### 4.2 `runChainRecursive` 与 `exec` ↔ `advance`（middleware.go）

- [ ] `runChainRecursive`：`middlewares[1:]` 每层缩短 1 → 终止；核对 `called` 闭包防重后**不可能在某一层内无限重入**。
- [ ] `exec`/`advance` 互递归：证明 `current` 每次经 `advance` 严格 +1（`calledBits` 位图保证同层 next 第二次调用直接返回错误、不再递归），层数 ≤ len(middlewares) → 终止。
- [ ] 边界：中间件跨 goroutine 调用 next（文档标注未定义行为）是否可能破坏位图防重导致下游重复执行——属并发约束而非死递归，确认文档约束充分。
- [ ] 用户中间件自身重入 `runChain`/`ServeHTTP` 的场景（用户代码导致的无限链）是否需在文档中警示。

### 4.3 `buildStructMeta`（meta.go:98）——最高优先级

- [ ] 确认匿名嵌入展开递归**无任何防环**；用 U5（`E1 struct{*E1}`）与 U6（`E2/E3`）作为 Req/Res 注册路由，实测是否栈溢出（`go test` 下观察 panic，注意 `recover` 无法捕获栈溢出）。
- [ ] 确认 U7（值嵌入互引用）被编译器拒绝，值嵌入路径无环 → 现有代码对值嵌入安全。
- [ ] 评估修复方向：`visiting map[reflect.Type]bool` 防环，或对已展开类型做 `sync.Map` 标记（注意与 `cachedStructMeta` 的缓存协同，避免把"展开中"误判为"已展开"导致字段丢失）。
- [ ] 关联影响：本函数被 #13/#14/#16 及 `cachedStructMeta` 调用——修复点是否一处生效、全部链解除。

### 4.4 `isStructLike`（meta.go:223）

- [ ] `for t.Kind() == reflect.Ptr` 无上限；用 U3（`type P *P`）经 `type X struct{ *P }` 匿名嵌入触发，实测死循环。
- [ ] 核对调用点：`buildStructMeta` 中所有 `isStructLike` 调用（含 `PkgPath != ""` 分支）都会先于任何 struct 判断进入该循环。
- [ ] 修复方向：复用 `derefType`（带上限）或加深度上限。

### 4.5 `checkUnsupportedDefaults`（defaults.go:29）

- [ ] struct 级 `visiting` 防环位置与删除时机（`defer delete`）正确性。
- [ ] **Slice/Array 分支的多层容器展开 `for` 循环**：用 U1（`type S []S`）作为 struct 字段，实测该循环是否永不退出；同分支对 `[][]S`、`[]map[string]S` 等组合逐格推演。
- [ ] Map 分支（值类型为 Slice/Array 的二次穿透）与 Ptr 分支是否同样受 U1/U2 打击。
- [ ] 修复方向：展开循环加迭代上限，或改为带 `visiting` 的递归。

### 4.6 `applyDefaultsWithVisiting`（defaults.go:156）

- [ ] `visitKey` 键选择三要素：struct 地址（`reqPtr.Pointer()`）、map 桶指针（`subV.Pointer()`）、map 指针值（`val.Pointer()`）——用 U9 三类环数据实测终止。
- [ ] `defer delete`（路径式防环）与 `validateNonzeroWalk` 的"不删除"（全局式防环）两种策略的正确性对比与各自边界。
- [ ] 边界核对：nil map 的 `Pointer()` 语义、零尺寸 struct 共享地址导致的误跳过、map 值副本（`valCopy`）的新地址是否绕过 struct 级防环（依赖 map 桶键兜底——推演确认）。
- [ ] 请求阶段（`rp=true`）与注册阶段递归范围一致性。

### 4.7 `deepCopyDefaults`（defaults.go:307）

- [ ] **无防环**：用 U9 的 `a.Next = &a` 数据实测指针分支是否无限分配新指针直到 OOM/栈溢出。
- [ ] 可达性分析：当前调用链（ServeHTTP 对模板深拷贝）中模板数据是否必然无环（零值模板 + 仅标量/标量切片默认值，`isDefaultSupported` 拒绝容器）——确认"潜在风险"而非"现实风险"的结论依据。
- [ ] 顺带核对：Interface 分支对环状 `any` 值的行为；`hasRefFields`（#9）的短路策略与 `deepCopyDefaults` 的深拷贝策略是否产生"needsDeepCopy 漏判"联动缺陷。

### 4.8 `hasRequestPhaseDefaults`（defaults.go:415）与 `hasNonzeroInTree`（validate.go:83）

- [ ] 两者同构缺陷：顶层（及字段级穿透）Slice/Map 递归发生在 `visiting` 检查**之前**——用 U1（`type S []S`）、U2（`type M map[string]M`）作为 Req struct 字段注册路由，实测栈溢出。
- [ ] 对照 U3（`type P *P`）：`derefType` 的上限使 Ptr 路径安全（返回非 struct → false），确认只有 Slice/Map 路径暴露。
- [ ] struct 环（`A{ s []A }`）经 `visiting` 正确拦截的对照验证（确认防环机制本身有效，只是位置错）。
- [ ] 修复方向：将 `visiting` 检查提前到 Slice/Map 穿透之前，或引入统一的"类型树遍历"工具函数收敛四处重复实现（#6/#10/#18 及 openapi 的 #13/#14）。

### 4.9 `typeToSchema`（openapi.go:594）

- [ ] Ptr 分支：用 U3（`P *P`）/U4 作为 Req/Res 字段，调用 `GenerateOpenAPI` 实测栈溢出（无深度上限、无防环）。
- [ ] Slice/Map 分支：用 U1/U2 实测；Struct 分支对照 U5/U6——注意先受 `buildStructMeta` 打击，需隔离验证（修复 #4 后再测 struct 环经 `registerStructSchema` 占位机制是否安全）。
- [ ] `registerStructSchema` 的占位注册（先 `typeNames[t] = name` 后展开）对 struct 自引用/互引用的正确性推演。
- [ ] `coerceExample` 的递归深度 = 有限 schema 嵌套深度，证明无独立风险。

### 4.10 `validateNonzeroWalk`（validate.go:166）

- [ ] 实例级防环：`visitKey`（地址+类型）、"标记不删除"策略的注释依据（JSON 不会产生共享指针）逐条核对。
- [ ] `isTempCopy` 边界：map 值副本不注册地址、map 桶键兜底的组合是否完备——用 U9 的 map 自引用（`m["x"] = a` 值拷贝共享桶）实测。
- [ ] 数组分支（本批新增，P2-02 联动）：与切片分支的防环一致性。
- [ ] Interface 字段不递归（预期跳过）的确认。

### 4.11 支撑函数联动核对

- [ ] `derefType`/`isDefaultSupportedDepth`/`setScalar` 的深度上限 32 的一致性（三处独立上限，修改需同步）。
- [ ] `cachedStructMeta` 的缓存与 `buildStructMeta` 防环修复的交互（"展开中"类型不得被缓存为"已展开"）。
- [ ] `visitMapPool` 的 `clear`/归还逻辑不影响防环正确性（超大 map 不入池仅影响性能）。

## 5. 审查方法

按以下顺序执行，每一步产出可核查的中间记录：

1. **清单复核**：以第 2.2 节清单为基线，用 `grep -n` 全量复核 13 个文件中所有自我调用与相互调用（如 `grep -n "buildStructMeta\|applyDefaultsWithVisiting\|validateNonzeroWalk" zchttp/*.go`），确认无遗漏的递归点；顺带确认 `_test.go` 中是否已有防环断言可复用。
2. **终止性静态证明**：对每个递归点按第 3.1 节四要素写出证明或反例（单调度量 / 防环位置 / 上限 / 占位注册），形成"终止性核对表"。
3. **对抗用例实测**（核心步骤）：用第 3.3 节的 U1–U9 用例逐点打击：
   - 类型级用例（U1–U8）：写临时测试文件，定义对应类型，分别作为 Req/Res 注册路由（覆盖 `buildEntry`/`checkUnsupportedDefaults` 触发链）并调用 `GenerateOpenAPI`（覆盖 openapi 链）；观察栈溢出/死循环（建议每个用例单独 `go test -run` 并加 `-timeout`，避免一个死循环卡死整个测试批次）。
   - 数据级用例（U9）：手工构造环状数据传入 `applyDefaults`/`deepCopyDefaults`/`validateNonzero` 入口，验证实例级防环。
   - 栈溢出无法被 `recover` 捕获，需以"进程崩溃/超时"作为观测信号记录。
4. **跨链关联验证**：对每个高危点确认其可达性——即构造的输入能否从公开 API（`Router.GET`、`HttpEngine.ServeHTTP`、`GenerateOpenAPI`）一路到达该递归点（防止"死递归但不可达"的假阳性），并记录完整调用路径。
5. **修复预案评估**：对每个确认的问题给出最小修复方向（加 `visiting`、加深度上限、复用 `derefType`、统一类型树遍历工具），评估修复对 `cachedStructMeta`/`visitMapPool` 等缓存与池的联动影响。
6. 运行验证命令参考：`go test ./zchttp/ -count=1 -timeout 60s`；`go test -race ./zchttp/ -run TestChain -count=1`（互递归路径并发约束）；`go vet ./zchttp/`。现有 14500+ 行测试是回归基线，修复后全量回归。

## 6. 检查清单

### 逐函数（19 项，对应第 2.2 节清单）

- [ ] #1 `matchPath`：#4 其余各项证明/核对完成，结论"必然终止"
- [ ] #2 `runChainRecursive` / #3 `exec`↔`advance`：单调性证明 + goroutine 约束核对完成
- [ ] #4 `buildStructMeta`：U5/U6 实测完成，结论与修复方向明确（最高优先级）
- [ ] #5 `isStructLike`：U3/U4 实测完成
- [ ] #6 `checkUnsupportedDefaults`：U1 展开循环实测完成
- [ ] #7 `applyDefaultsWithVisiting`：U9 三类环数据实测完成
- [ ] #8 `deepCopyDefaults`：U9 实测 + "当前不可达"结论依据充分
- [ ] #9 `hasRefFields`：短路策略复核完成
- [ ] #10 `hasRequestPhaseDefaults` / #18 `hasNonzeroInTree`：U1/U2 实测完成
- [ ] #11/#12/#5 深度上限三处一致性核对完成
- [ ] #13/#14 `walkTypeUsage`/`walkDefaultsReachability`：自身防环复核 + 对 #4 的依赖链确认
- [ ] #15/#16/#17 `typeToSchema`/`registerStructSchema`/`coerceExample`：U1/U2/U3/U4 + struct 环实测完成

### 通用

- [ ] 递归清单与 grep 复核一致，无遗漏
- [ ] 每个高危点的公开 API 可达性已确认（含完整调用路径记录）
- [ ] 修复预案对缓存/池（`cachedStructMeta`/`visitMapPool`/`chainRunnerPool`）的联动影响已评估
- [ ] 修复后 `go test ./zchttp/ -count=1` 全量回归通过

## 7. 计划阶段观察到的可疑点（待审查确认，非结论）

制定计划时通读全部 13 个源码文件，并用独立模块实测了 Go 递归类型定义的合法性（`type S []S`、`type M map[string]M`、`type P *P`、`type A *B; type B *A`、`type E1 struct{ *E1 }`、`type E2 struct{ *E3 }; type E3 struct{ *E2 }`、`type I interface{ M() I }` **全部编译通过**；`type V1 struct{ V2 }; type V2 struct{ V1 }` 被编译器以 "invalid recursive type" 拒绝）。观察到以下信号：

1. **`checkUnsupportedDefaults` 的容器展开 `for` 循环无保护**（defaults.go:110）：`type S []S` 作为 struct 字段时，`elem = elem.Elem()` 恒返回自身，循环永不退出——疑似死循环（高）。Struct 级 `visiting` 防环存在，但展开循环发生在其之前且不经过 struct。
2. **`hasRequestPhaseDefaults`/`hasNonzeroInTree` 的 Slice/Map 穿透在防环检查之前**（defaults.go:418-423、validate.go:85-90）：`type S []S`/`type M map[string]M` 作为 Req struct 字段注册路由即触发无限递归——疑似死递归（高）。两函数同构，且与 #1 同属"类型树遍历"问题，建议统一修复。
3. **`isStructLike` 解指针循环无上限**（meta.go:224）：`type P *P` 合法，`type X struct{ *P }` 匿名嵌入触发死循环——疑似死循环（高）。同文件 `derefType` 有上限 32，修复可复用。
4. **`typeToSchema` 的 Ptr/Slice/Map 分支无防环无上限**（openapi.go:595-655）：U1/U2/U3/U4 作为 Req/Res 字段调用 `GenerateOpenAPI` 即无限递归——疑似死递归（高）。Struct 分支经 `registerStructSchema` 占位机制安全，说明修法已有先例。
5. **`buildStructMeta` 匿名嵌入展开递归无防环**（meta.go:130）：U5（`E1 struct{*E1}`）/U6（`E2/E3`）作为 Req/Res 注册路由即无限递归——疑似死递归（高）。该函数是注册/请求/OpenAPI 三个阶段的共同入口，一处缺陷打击四条链（#13/#14/#16/`cachedStructMeta`）。
6. **`deepCopyDefaults` 无防环**（defaults.go:307）：指针分支对环状数据无限分配新指针——疑似死递归（中）。当前模板数据必然无环（零值 + 仅标量默认值），属潜在风险；但本批改动新增 Interface 分支后与"未来放开容器默认值"的注释相互印证，风险敞口在扩大，需评估是否预防性加防环。
7. **`applyDefaultsWithVisiting` 与 `validateNonzeroWalk` 的防环策略差异**：前者 `defer delete`（路径式）、后者不删除（全局式）——两处语义差异的注释依据（JSON 不产生共享指针）需核对是否仍成立（用户手工构造 Req 直接注入场景）。
8. **零尺寸 struct 的 `Addr().Pointer()` 共享地址**：`validateNonzeroWalk`/`applyDefaultsWithVisiting` 对零尺寸 struct 的防环键可能全部相同，导致同树中多个零尺寸实例被误判为"已访问"而跳过校验/填充——不影响终止性，但属防环机制的准确性边界（低）。
9. **`middleware.go` 的互递归并发约束**：`next` 跨 goroutine 调用会破坏 `current`/`calledBits` 的状态（文档已标注未定义行为）——核对是否需要防御性加固（如 `atomic` 或 panic 早失败），避免"位图误判导致下游重复执行"（低）。

## 8. 输出建议

审查完成后，将结果整理到 `zchttp/docs/recursion-review-report.md`（与本文件同名的 report 文件），建议结构：

1. **审查概况**：审查日期、代码基准（工作区，注明未提交变更范围）、执行的方法（第 5 节 6 步的完成情况）、总体结论（每个递归点的终止性结论汇总表）。
2. **问题清单**：表格，列含：编号、级别（高/中/低）、函数与位置、递归形式、触发输入（U1–U9 编号或最小用例）、实测表现（栈溢出/死循环/超时）、建议修复方向、状态。
3. **逐函数终止性结论**：19 项清单每项一段（✅ 必然终止 + 证明摘要 / ⚠️ 问题编号），并附对抗用例实测记录。
4. **修复建议汇总**：区分"必须修复"（高/中级）与"预防性加固"（低/潜在）；统一类型树遍历工具（收敛 `checkUnsupportedDefaults`/`hasRequestPhaseDefaults`/`hasNonzeroInTree`/`walkTypeUsage`/`walkDefaultsReachability` 的重复实现）作为优先方案评估。
5. **闭环说明**：代码修复后回归 `go test ./zchttp/ -count=1`；新增防环用例沉淀为回归测试（如 `defaults_recursion_test.go`），并同步更新本计划与报告的对应条目。
