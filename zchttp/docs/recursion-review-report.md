# zchttp 递归函数与方法审查报告

> 本报告是 `docs/recursion-review-plan.md` 的执行结果：对 zchttp 模块全部递归点完成清单复核、终止性静态证明、U1–U9 对抗用例实测与公开 API 可达性验证。

## 1. 审查概况

- **审查日期**：2026-08-16。
- **代码基准**：当前工作区（含未提交变更）。`git diff -- zchttp/` 显示本批变更集中在 `binding.go`/`defaults.go`/`httpEngine.go`/`meta.go`/`responseWriter.go`/`router.go`/`validate.go`，与计划 2.1 节判定一致；递归高风险区（`validateNonzeroWalk` 数组分支、`deepCopyDefaults` Interface 分支）均在变更范围内，已重点核验。
- **执行方法**（计划第 5 节 6 步全部完成）：
  1. 清单复核：grep 全量核对 13 个源码文件中 19 个递归点的调用位置，与计划 2.2 节清单一致，无遗漏；
  2. 终止性静态证明：按计划 3.1 节四要素（单调递减 / 类型级防环 / 实例级防环 / 深度上限与占位注册）逐点核验；
  3. 对抗用例实测：新建 `recursion_probe_test.go`（子进程自执行 harness，25 个探测动作），栈溢出以子进程崩溃（输出含 `goroutine stack exceeds`）为信号、死循环以子进程超时被杀（无 `PROBE_DONE` 输出）为信号，全部实测结果与静态分析一致；
  4. 公开 API 可达性验证：对每个确认的问题构造从 `Router.GET/POST`、`GenerateOpenAPI` 出发的完整调用路径（见第 3 节各条目"可达路径"）；
  5. 修复预案评估：见第 4 节（含 `cachedStructMeta`/`visitMapPool`/`chainRunnerPool` 联动分析）；
  6. 运行验证：`go test ./zchttp/ -count=1 -timeout 600s` 全量通过（41.153s）；`go test -race ./zchttp/ -run "TestChain|TestRunChain" -count=1` 通过；`go vet ./zchttp/` 无告警。

### 总体结论汇总表

| # | 递归点 | 递归形式 | 终止性结论 |
|---|---|---|---|
| 1 | `routeNode.matchPath` | 自递归 | ✅ 必然终止（路径严格缩短） |
| 2 | `runChainRecursive` | 自递归 | ✅ 必然终止（切片严格缩短） |
| 3 | `exec` ↔ `advance` | 运行时互递归 | ✅ 有界终止（层号 +1、位图防重） |
| 4 | `buildStructMeta` | 自递归 | ❌ 死递归（REC-01，高） |
| 5 | `isStructLike` | `for` 循环（递归等价） | ✅ 结构性安全（触发路径被编译器拒绝，见 REC-07 加固建议） |
| 6 | `checkUnsupportedDefaults` | 自递归 + 展开 `for` 循环 | ❌ 展开循环死循环（REC-05，高）；递归主体有 struct 级防环 |
| 7 | `applyDefaultsWithVisiting` | 自递归 | ✅ 终止（实例级防环，U9 三类环数据实测通过） |
| 8 | `deepCopyDefaults` | 自递归 | ⚠️ 无防环（REC-06，中；当前模板数据无环不可达，潜在风险） |
| 9 | `hasRefFields` | 自递归 | ✅ 必然终止（引用分支短路） |
| 10 | `hasRequestPhaseDefaults` | 自递归 | ❌ 死递归（REC-02，高） |
| 11 | `isDefaultSupportedDepth` | 自递归 | ✅ 有界终止（深度上限 32） |
| 12 | `derefType` | `for` 循环 | ✅ 有界终止（上限 32，既有测试锁死） |
| 13 | `walkTypeUsage` | 自递归 | ✅ 自身终止；依赖 #4，随 REC-01 解除 |
| 14 | `walkDefaultsReachability` | 自递归 | ✅ 自身终止；依赖 #4，随 REC-01 解除 |
| 15 | `typeToSchema` | 自递归 | ❌ 死递归（REC-04，高） |
| 16 | `registerStructSchema` | 经 #15 间接递归 | ✅ struct 环安全（占位注册，实测通过） |
| 17 | `coerceExample` | 自递归 | ✅ 必然终止（深度 = 有限 schema 嵌套） |
| 18 | `hasNonzeroInTree` | 自递归 | ❌ 死递归（REC-03，高） |
| 19 | `validateNonzeroWalk` | 自递归 | ✅ 终止（实例级防环，U9 三类环数据实测通过） |

**核心结论**：19 个递归点中 4 个确认"存在合法可达输入使函数不终止"（REC-01~REC-04），1 个确认死循环（REC-05），1 个潜在风险（REC-06）；其余 13 个终止性证明成立并经实测或既有测试锁死。计划 7 节的 9 个可疑点中 6 个证实（1/2/4/5/6 及 7 的差异核对），**可疑点 3（`isStructLike` 死循环）被证伪**——触发路径不存在（见 2.1 节 REC-07）。

## 2. 问题清单

| 编号 | 级别 | 函数与位置 | 递归形式 | 触发输入 | 实测表现 | 建议修复方向 | 状态 |
|---|---|---|---|---|---|---|---|
| REC-01 | 高 | `buildStructMeta`（meta.go:98，嵌入展开于 :130） | 自递归，无防环 | U5 `type E1 struct{ *E1 }`、U6 `E2/E3` 嵌入互引用，作为 Req/Res 匿名嵌入注册路由 | 子进程栈溢出（exit 2，`goroutine stack exceeds`），e1 耗时 1.81s、e2 0.69s | 增加 `visiting map[reflect.Type]bool`（内部函数携带），环命中时跳过该嵌入字段展开并 `slog.Warn`；与 `cachedStructMeta` 协同见第 4 节 | ✅ 已修复（`buildStructMetaWithVisiting`，入口断环 + 嵌入级跳过告警双防线） |
| REC-02 | 高 | `hasRequestPhaseDefaults`（defaults.go:415，Slice/Map 穿透于 :418-424） | 自递归，顶层 Slice/Map 穿透在 `visiting` 检查**之前** | U1 `type S []S`、U2 `type M map[string]M` 作为 Req 字段注册路由 | 子进程栈溢出（buildentry_field_s/m 均 0.5s 内崩溃）；注册全链 `register_field_s` 实际倒在本函数（先于 REC-05） | 将 `visiting` 检查与标记提前到 Slice/Map 穿透之前（对容器类型同样登记），或与 REC-03/REC-05 统一为类型树遍历工具 | ✅ 已修复（visiting 登记提前到穿透之前，容器类型同样登记） |
| REC-03 | 高 | `hasNonzeroInTree`（validate.go:83，Slice/Map 穿透于 :85-91） | 自递归，与 REC-02 同构 | 同 REC-02 | 子进程栈溢出（nonztree_field_s/m 均 0.5s 内崩溃） | 同 REC-02，两函数必须同步修复 | ✅ 已修复（与 REC-02 同构同步） |
| REC-04 | 高 | `typeToSchema`（openapi.go:594，Ptr :599 / Slice :635 / Map :640） | 自递归，Ptr/Slice/Map 分支无防环无上限 | U1/U2/U3 `type P *P`/U4 `type A *B; type B *A` 作为 Req/Res 字段 | 子进程栈溢出（隔离探测 openapi_field_* 与公开 API 全链 openapi_gen_field_p/ab 均崩溃；U3/U4 注册阶段可存活，倒在 `GenerateOpenAPI`） | 增加深度参数（上限复用 `maxPtrDerefDepth`，超限返回空 schema），或扩展 `typeNames` 占位机制到容器/指针类型；Struct 分支已有占位先例（#16） | ✅ 已修复（depth 参数沿 Ptr/Slice/Map 传递，上限 `maxPtrDerefDepth`，Struct 占位机制不变） |
| REC-05 | 高 | `checkUnsupportedDefaults`（defaults.go:29，多层容器展开 `for` 于 :110-115） | 展开 `for` 循环无保护 | U1 作为 struct 字段（直接调用隔离验证）；`elem = elem.Elem()` 恒返回自身 | 子进程死循环：30s 超时被杀，无输出 | 展开循环加迭代上限（复用 `maxPtrDerefDepth`），或改为带 `visiting` 的递归。**必须与 REC-02 同步修复**：当前注册链中 U1 先倒在 REC-02，修复 REC-02 后本循环即暴露为注册挂起 | ✅ 已修复（展开循环加 `maxPtrDerefDepth` 迭代上限，与 REC-02 同批提交） |
| REC-06 | 中 | `deepCopyDefaults`（defaults.go:307，Ptr 分支 :313-320） | 自递归，无防环 | U9 `a.Next = &a` 环状数据 | 子进程栈溢出（deepcopy_ptr_cycle 0.63s 崩溃，每层分配新指针无限递归） | 增加 `visited map[uintptr]reflect.Value`（原地址→新值映射，保留共享语义）或深度上限。当前不可达（模板恒无环），但 Interface 分支新增后"未来放开容器/any 默认值"的注释印证风险敞口扩大，建议预防性加固 | ✅ 已修复（深度上限方案：既有测试 `TestDeepCopyDefaults_ArrayElemNotShared` 锁死"每处出现独立新副本"语义，与地址记忆方案冲突，故采用 `deepCopyDefaultsDepth` 深度上限） |
| REC-07 | 低 | `isStructLike`（meta.go:223）、`buildStructMeta` 嵌入解指针循环（meta.go:126） | `for` 循环解指针无上限 | 计划假设 U3 经 `type X struct{ *P }` 匿名嵌入触发——**实测不成立**：编译器拒绝指针类型的匿名嵌入（`embedded field type cannot be a pointer`，Go 规范要求嵌入 `*T` 时 `T` 不得为指针类型） | 对照探测 register_field_p_anon 正常完成；嵌入版 `recReqEmbedP` 编译期即被 go vet 拒绝 | 结构性安全，无需修复；建议预防性复用 `derefType`（带上限）替换裸循环，消除未来演化隐患 | ✅ 已加固（两处裸循环均改为复用 `derefType`） |
| REC-08 | 低 | `applyDefaultsWithVisiting` / `validateNonzeroWalk` 的防环键 | — | 零尺寸 struct（如 `struct{}{}`）多实例共享地址 | 未实测出实际影响（零尺寸 struct 无可绑定字段，nonzero/default 均不生效） | 不修复；记录为防环机制准确性边界（不误终止，仅理论上可能误跳过） | 已知边界 |

## 3. 逐函数终止性结论（19 项）

以下每项含：终止性结论 + 证明摘要（按计划 3.1 节四要素）+ 对抗用例实测记录。实测均通过 `recursion_probe_test.go` 的子进程探测（父进程以退出码/超时/输出关键字为观测信号）。

### #1 `routeNode.matchPath`（router_trie.go:94）✅ 必然终止

- **单调递减**：进入非空分支时 `path` 以 `/` 开头，`rest = path[1:]` 再按首段切分，递归参数 `rest` 长度严格小于 `path`（至少消耗开头的 `/`）；度量 `len(path)` 严格下降，基例 `path == ""` 直接返回。
- **归一化推演**：`"/a"` → `rest=""`；`"/a/b"` → `seg="a", rest="/b"` 保持 `/` 开头形态递进；根路径在 `matchParam` 中归一化为空串。
- **回溯正确性（顺带核对）**：static 分支传入原 `captured`，param 分支传入 `append(captured, seg)`；static 分支完整返回后才进入 param 分支，无并发写。`append` 的容量共享不会污染：param 分支写入的位置更深，且 static 分支不再使用切片。
- **实测**：既有 `router_trie_test.go` 全量回归通过（含参数/可选参数/回溯用例）。

### #2 `runChainRecursive` / #3 `exec` ↔ `advance`（middleware.go）✅ 必然终止 / 有界终止

- **#2 单调递减**：`middlewares[1:]` 每层严格缩短 1，基例 `len==0` 执行 finalHandler。`called` 闭包保证同层 next 第二次调用直接返回 `ErrNextCalledMultipleTimes`、不再递归 → 某层内不可能无限重入。
- **#3 单调递减 + 防重**：`advance` 每次使 `current+1` 进入下一层；`calledBits` 位图保证同层 next 第二次调用返回错误不再递归。层号单调递增且上界为 `len(middlewares)`（≤64 走位图路径，>64 回退 #2），递归深度有界。
- **goroutine 约束核对**：`NextFunc`/`MiddlewareHandler` 文档均声明"next 必须在同一 goroutine 内同步调用，跨 goroutine 行为未定义"；`middleware_goroutine_test.go` 锁死"同步等待型跨 goroutine 调用不 panic 且下游正常执行"的弱保证；`go test -race -run "TestChain|TestRunChain"` 通过。位图在并发误用下可能误判导致下游重复执行——属并发约束问题而非死递归，文档约束充分，无需防御性加固。
- **用户中间件重入 `runChain`/`ServeHTTP`**：每次 `ServeHTTP` 是独立有界链，用户代码自造无限链属用户代码行为（与 net/http 同等边界），无需在文档中额外警示。
- **实测**：`middleware_test.go`（含重复 next、>64 层回退）与 `middleware_goroutine_test.go` 全量回归通过。

### #4 `buildStructMeta`（meta.go:98）❌ 死递归（REC-01，最高优先级）

- **无防环**：匿名嵌入展开（:124-143）直接递归 `buildStructMeta(embeddedType)`，无 `visiting`、无深度上限。
- **U5 实测**：`type recE1 struct{ *recE1 }` 匿名嵌入作 Req 注册路由 → 子进程栈溢出（register_embed_e1，exit 2，`goroutine stack exceeds`，1.81s）。
- **U6 实测**：`E2{*E3}`/`E3{*E2}` 互引用 → 子进程栈溢出（register_embed_e2，0.69s）。
- **U7 对照**：值嵌入互引用 `type V1 struct{ V2 }; type V2 struct{ V1 }` 被编译器以 "invalid recursive type" 拒绝（计划阶段已实测），值嵌入路径天然无环。
- **对照基准**：非匿名字段的 struct 自引用（`recCatReq{Root recCatNode}`，`Children []*recCatNode`）注册成功（register_field_struct_cycle 正常完成）——展开仅限匿名嵌入，非嵌入字段不进递归。
- **关联影响**：本函数被注册阶段（`buildEntry`）、请求阶段（`cachedStructMeta`）、OpenAPI 阶段（`walkTypeUsage`/`walkDefaultsReachability`/`registerStructSchema`）四方调用——一处修复全链解除（实测 #16 占位机制在 buildStructMeta 存活前提下对 struct 环安全）。

### #5 `isStructLike`（meta.go:223）✅ 结构性安全（计划假设被证伪）

- **计划假设**：U3 经 `type X struct{ *P }` 匿名嵌入触发解指针死循环。
- **证伪**：Go 规范规定嵌入字段为 `*T` 时 `T` 不得为指针类型；`type X struct{ recP }`（`recP = *recP`）在 go vet/编译期即报 `embedded field type cannot be a pointer`。U4（`A *B; B *A`）同理不可嵌入。因此 `isStructLike` 的解指针循环只对"终止于 struct 的有限指针链"可达，结构上必然终止。`buildStructMeta:126` 的解指针循环位于 `isStructLike` 通过之后，同样被守护。
- **实测**：对照探测 register_field_p_anon（U3 作普通字段）正常完成；嵌入版被编译器拒绝（本次审查的 vet 输出即为证据）。
- **遗留**：裸循环无上限仍是代码气味，建议复用 `derefType`（REC-07，低）。

### #6 `checkUnsupportedDefaults`（defaults.go:29）❌ 展开循环死循环（REC-05）

- **struct 级防环本身正确**：`visiting[t]` 检查+标记+`defer delete` 位置正确（:34-38），struct 自引用环（`A{ s []A }`）可被拦截。
- **缺陷**：Slice/Array 分支的多层容器展开 `for` 循环（:110-115）无任何保护：U1（`type S []S`）时 `elem.Elem()` 恒返回 `recS`，循环永不退出。
- **实测**：隔离探测 check_unsupported_field_s 死循环（30s 超时被杀、无输出）；对照 check_unsupported_field_m（U2）正常完成——Map 分支对 `map[string]M` 递归到 `M` 后经 `derefType` 得到非 struct 即返回，**U2 不打击 #6**（与计划 4.5 节猜测不同，已修正）。
- **可达性说明**：注册链中 U1 先死于 REC-02（register_field_s 实测倒于 `hasRequestPhaseDefaults`），故本循环当前被 REC-02 "掩盖"；修复 REC-02 后若不修复本项，`type S []S` 字段将使路由注册**挂起**（比崩溃更隐蔽），必须同步修复。

### #7 `applyDefaultsWithVisiting`（defaults.go:156）✅ 终止

- **实例级防环三键核验**：struct 地址键（`reqPtr.Pointer()+类型`，:163）、map 桶指针键（`subV.Pointer()`，:265）、map 指针值键（`val.Pointer()`，:278）；struct 级 `defer delete` 路径式策略对指针自环充分（环上地址必在路径上重复）。
- **U9 实测**：指针自环 `a.Next=&a`（applydefaults_ptr_cycle）与 map 自引用 `m["x"]=m`（applydefaults_map_cycle）均正常完成。map 场景的 valCopy 新地址确实绕过 struct 级键，但由 map 桶指针键兜底拦截（与计划 4.6 节推演一致）。
- **边界**：nil map 的 `Pointer()` 返回 0 但 nil map 不进入该分支（valType 判定前 map 非 nil 才有键）；请求阶段（rp=true）与注册阶段递归范围一致（同一函数、同一分支集）。

### #8 `deepCopyDefaults`（defaults.go:307）⚠️ 无防环（REC-06，潜在风险）

- **实测**：U9 指针自环 → 子进程栈溢出（deepcopy_ptr_cycle 0.63s 崩溃）：Ptr 分支每层 `reflect.New` 分配新指针后递归进入副本，副本内的回边指针再次触发分配，无限递归。
- **可达性分析（"潜在"而非"现实"的依据）**：`ServeHTTP` 仅对 `entry.needsDeepCopy` 的模板执行深拷贝；模板由"零值 + 注册阶段 `applyDefaults`"构造，而 `isDefaultSupported` 白名单仅标量/标量指针/标量切片——模板中指针字段恒为 nil 或指向标量、切片字段恒为标量元素、map/interface 字段恒为零值，**模板数据必然无环**。当前无任何公开 API 路径能把环状数据送入本函数。
- **风险敞口**：本批变更新增 Interface 分支，注释明确为"未来放开容器/any 默认值"做防御——一旦白名单放开，模板即可能携带引用环，本函数将从潜在转为现实风险。建议预防性加固（第 4 节）。
- **联动核对**：`hasRefFields`（#9）短路策略与深拷贝策略无"needsDeepCopy 漏判"联动缺陷——Interface 分支两者已对齐（`hasRefFields` Interface 分支递归动态值、`deepCopyDefaults` Interface 分支拷贝动态值），判定面 ⊆ 拷贝面。

### #9 `hasRefFields`（defaults.go:367）✅ 必然终止

- **短路策略**：Ptr/Slice/Map 非 nil 即返回 true、**不进入内容**；仅 Struct/Array/Interface 递归。值类型容器无法在 Go 数据中形成环（数组元素是值拷贝），递归深度 = 类型树深度 → 必然终止。
- **实测**：注册链对照探测（register_field_struct_cycle、register_field_i）中随全链执行通过。

### #10 `hasRequestPhaseDefaults`（defaults.go:415）❌ 死递归（REC-02）

- **缺陷定位**：顶层 Slice/Map 穿透（:418-424）发生在 `visiting` 检查（:431）**之前**，且穿透不登记容器类型：U1 经 `Slice → hasRequestPhaseDefaults(t.Elem())` 恒返回自身，永不触及 struct 分支的防环。
- **实测**：buildentry_field_s、buildentry_field_m 均子进程栈溢出（约 0.5s）；注册全链 register_field_s 亦倒于此（先于 #6）。
- **U3 对照**：`type P *P` 经 `derefType` 上限 32 后仍为 Ptr → default 分支 → 非 struct 返回 false，**Ptr 路径安全**，仅 Slice/Map 路径暴露（与计划 4.8 节判定一致）。
- **struct 环对照**：`A{ s []A }` 经字段级 Slice 穿透到 `A` 时 `visiting[A]` 命中返回 false——防环机制本身有效，只是位置错。

### #11 `isDefaultSupportedDepth`（defaults.go:488）✅ 有界终止

- `depth >= maxPtrDerefDepth(32)` 返回 false；Slice/Ptr 每层 depth+1 → 深度有界。U1/U3 类自引用类型在 32 层后判定为"不支持"，是保守且正确的方向（默认值白名单本就不应接纳自引用容器）。

### #12 `derefType`（meta.go:65）✅ 有界终止

- 循环上限 `maxPtrDerefDepth = 32`，超限原样返回；既有测试 `TestDerefType_SelfReferentialPtr`（meta_test.go:291，goroutine+超时断言）已锁死 U3 不死循环。

### #13 `walkTypeUsage`（openapi.go:170）/ #14 `walkDefaultsReachability`（openapi.go:254）✅ 自身终止

- **防环位置正确**：`derefType` → struct 判定 → `visiting[t]` 检查+标记+`defer delete`（:179-183 / :263-267），先检查再递归。
- **Slice/Map 仅单层穿透**（到 `Elem()`，无展开循环），与 #6 的缺陷形态不同；U1/U2 到达时为 Kind Slice/Map 非 struct 直接返回，不形成递归环。
- **依赖链**：两函数内部调用 `buildStructMeta`（:185 / :269）——struct 嵌入环（U5/U6）会先死于 REC-01；REC-01 修复后，struct 字段环经 `visiting` 拦截，全链安全。
- **实测**：register_field_i（U8 接口自引用全链：注册 + `GenerateOpenAPI`）正常完成，Interface 分支无反射穿透。

### #15 `typeToSchema`（openapi.go:594）❌ 死递归（REC-04）

- **缺陷**：Ptr（:599）、Slice（:635 items）、Map（:640 additionalProperties）三个分支直接递归 `t.Elem()`，无防环、无深度上限（与 Struct 分支的占位机制形成鲜明对比）。
- **实测**（隔离探测 + 公开 API 全链双重验证）：
  - openapi_field_s（U1 Slice）、openapi_field_m（U2 Map）、openapi_field_p（U3 Ptr）、openapi_field_ab（U4 互引用）：均子进程栈溢出；
  - **全链可达性**：openapi_gen_field_p / openapi_gen_field_ab（`Router.POST` 注册 + `GenerateOpenAPI`）：注册阶段因 `derefType` 上限存活（hasRequestPhaseDefaults/hasNonzeroInTree/checkUnsupportedDefaults 的 Ptr 路径均被 32 层上限拦截），倒在 `GenerateOpenAPI → buildOperation → buildRequestBody → registerStructSchema → typeToSchema` Ptr 分支——证明该缺陷无需恶意构造，正常注册 + 生成文档即触发。
- **Struct 分支对照**：经 `registerStructSchema` 占位安全（见 #16）。

### #16 `registerStructSchema`（openapi.go:552）✅ struct 环安全

- **占位注册推演**：先 `typeNames[t] = name` + `schemas[name] = obj` 占位（:561-564），再展开字段；字段内回边（`[]*Self`）经 `typeToSchema` Struct 分支回到本函数时 `typeNames` 命中直接返回 `$ref` → 环被打断。
- **实测**：openapi_struct_cycle（`recCatNode{Children []*recCatNode}`）正常完成。

### #17 `coerceExample`（openapi.go:707）✅ 必然终止

- 仅 array 分支递归进入 `items`，递归深度 = schema 的 items 嵌套深度；schema 由有限类型树生成（REC-04 修复后有界），且每次递归 schema 严格变"深一层"（`items` 子对象），无环结构 → 必然终止。无独立风险。

### #18 `hasNonzeroInTree`（validate.go:83）❌ 死递归（REC-03）

- 与 REC-02 **同构缺陷**：顶层 Slice/Map 穿透（:85-91）在 `visiting` 检查（:98）之前。
- **实测**：nonztree_field_s、nonztree_field_m 均子进程栈溢出。
- **修复必须与 REC-02 同步**（同构同修，避免两处策略漂移）。

### #19 `validateNonzeroWalk`（validate.go:166）✅ 终止

- **实例级防环**：`visitKey{地址+类型}`（:174）；"标记不删除"全局式策略的注释依据（:179-181，JSON 反序列化不产生共享指针）成立——该函数处理的数据来源为 JSON/表单绑定或用户手工构造，共享指针最坏导致第二路径**漏校验**（已知取舍），不导致不终止。
- **`isTempCopy` 边界完备性**：map 值副本不注册自身地址（副本地址不可靠，GC 复用会误判）+ map 桶指针键兜底（:270-274）——U9 map 自引用实测（validate_map_cycle）正常完成，组合完备。
- **数组分支（本批新增）**：与切片分支防环一致——元素地址稳定、非临时副本、注册 visited 键（:235-258）。
- **U9 实测**：指针自环（validate_ptr_cycle）、map 自引用（validate_map_cycle）、切片共享环 `a.L=[]A{a}; a.L[0].L=a.L`（validate_slice_cycle）均正常完成。
- **Interface 字段**：walk 仅对 Struct/Slice/Array/Map 分支递归，Interface 字段不穿透（与 `applyDefaultsWithVisiting` 一致）。

### 支撑函数联动核对（计划 4.11 节）

- **深度上限三处一致性**：`derefType`（meta.go:61）、`isDefaultSupportedDepth`（defaults.go:489）、`setScalar`（binding.go:233）均引用同一常量 `maxPtrDerefDepth = 32`，grep 核实无各自硬编码，修改一处即同步。
- **`cachedStructMeta` 与 REC-01 修复的交互**：现为"build 完成后 LoadOrStore"。修复方案若保证 `buildStructMeta` 对环输入产出**确定性结果**（环嵌入跳过一次、其余字段完整展开），则结果可安全缓存，不存在"展开中被缓存为已展开"问题；若采用"展开中"状态标记方案则必须禁止中途写缓存（详见第 4 节）。
- **`visitMapPool`**：归还前 `clear`、超 1024 键不入池（validate.go:147-153），仅影响性能不影响防环正确性；与 REC-02~REC-05 的类型级 `map[reflect.Type]bool`（各函数自建）无交集。

## 4. 修复建议汇总

### 必须修复（高级，4 项 + 1 项联动）

| 编号 | 修复方向 | 要点 |
|---|---|---|
| REC-01 | `buildStructMeta` 增加类型级防环 | 拆出内部 `buildStructMetaWithVisiting(t, visiting)`：入口检查+标记+`defer delete`；匿名嵌入递归前命中环则跳过该嵌入字段并 `slog.Warn`（含类型名与字段名）。跳过策略产出确定性结果，`cachedStructMeta` 缓存无需改造。一处修复同时解除注册/请求/OpenAPI（#13/#14/#16）四条链 |
| REC-02 + REC-03 | `visiting` 检查提前（同构同修） | 两函数顶部改为：先初始化/检查 `visiting[t]`（对容器类型同样登记），再做 Slice/Map 穿透。改动约 10 行/函数，语义不变（环上返回 false 属保守方向） |
| REC-05 | 展开循环加迭代上限 | `for` 循环计数，超过 `maxPtrDerefDepth` 即跳出并以当前 `elem` 继续（或改递归纳入 `visiting`）。**与 REC-02 同一 PR 提交**，否则修复 REC-02 会把注册崩溃置换为注册挂起 |
| REC-04 | `typeToSchema` 加深度上限 | 增加 depth 参数沿 Ptr/Slice/Map 传递，超限返回 `map[string]any{}`（与 default 分支一致）；上限复用 `maxPtrDerefDepth`，与既有三处上限哲学一致。Struct 分支占位机制保持不变 |

### 预防性加固（中/低级）

| 编号 | 建议 | 理由 |
|---|---|---|
| REC-06 | `deepCopyDefaults` 增加 `visited map[uintptr]reflect.Value`（原地址→新值，保留共享语义）或深度上限 | 当前模板恒无环不可达，但 Interface 分支与"未来放开容器/any 默认值"注释表明敞口扩大；加固成本约 15 行 |
| REC-07 | `isStructLike` 与 `buildStructMeta:126` 复用 `derefType` | 触发路径虽被编译器封死，裸循环仍是演化隐患，零成本消除 |
| 中长期 | 统一"类型树遍历"工具 | `checkUnsupportedDefaults`/`hasRequestPhaseDefaults`/`hasNonzeroInTree`/`walkTypeUsage`/`walkDefaultsReachability` 五处重复实现"字段 switch + 容器穿透 + visiting"，本次 4 个高危缺陷中 3 个（REC-02/03/05）源于该重复实现的策略漂移；建议收敛为带策略参数的单一遍历器 |

### 缓存/池联动影响评估

- `cachedStructMeta`：REC-01 方案（跳过环嵌入、结果确定性）下无需改造；禁止任何"展开中"中间状态写入 `structMetaCache`。
- `visitMapPool`：服务于实例级防环（#7/#19），与本轮类型级修复正交，无影响。
- `chainRunnerPool`：与递归修复无关（#3 结论为安全），无影响。

## 5. 闭环说明

1. **修复实施（2026-08-16）**：REC-01~REC-07 全部修复落地，变更文件：`meta.go`（REC-01/07）、`defaults.go`（REC-02/05/06）、`validate.go`（REC-03）、`openapi.go`（REC-04）。关键实施记录：
   - REC-01 采用双防线：入口级 `visiting[t]` 断环（返回空 meta，结果确定可缓存）+ 嵌入级 `visiting[embeddedType]` 命中跳过并 `slog.Warn`；
   - REC-02/03 将 `visiting` 登记提前到 Slice/Map 穿透之前，容器类型同样登记，同构同修；
   - REC-04 增加 depth 参数（外部调用点传 0，Struct 分支经占位机制不消耗 depth）；
   - REC-06 首次实施为地址记忆方案（保留共享语义），但回归发现与既有测试 `TestDeepCopyDefaults_ArrayElemNotShared` 锁死的"每处出现独立新副本"语义冲突，改用深度上限方案（`deepCopyDefaultsDepth`）——防环只需终止性保证，既有共享语义不变。
2. **回归锁死**：审查阶段的子进程探测 harness（`recursion_probe_test.go`）已翻转为包内回归测试 `recursion_regression_test.go`（8 组 `TestRecFix_*`）：修复后全部用例在进程内正常完成并断言后验（注册全链可服务请求、嵌入环跳过不丢同级字段、类型扫描终止且不过度防环、OpenAPI `$ref` 占位机制未被深度上限误伤、环状数据深拷贝终止且每处出现独立拷贝、U9 实例级防环基线、正常自引用端到端功能）。若修复回退，用例将以栈溢出/超时形式失败。
3. **回归基线**：修复后 `go test ./zchttp/ -count=1` 全量通过（3.557s，移除子进程 harness 后较审查基线 41.153s 大幅下降）；`go test -race ./zchttp/ -count=1` 全量通过；`go vet ./zchttp/` 无告警。
4. **计划同步**：计划第 7 节可疑点 3（`isStructLike`）已证伪、可疑点 1 的触发次序修正（U1 注册链先倒于 REC-02）；计划 2.2 节清单对应行的状态已同步标注。
5. **遗留（中长期）**：统一"类型树遍历"工具（收敛 #6/#10/#18/#13/#14 五处重复实现）未在本批实施，作为重构项保留。
