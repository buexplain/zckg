# zcquit 代码审查计划

## 1. 审查目标

- **退出时序正确性**：`cancel() → 按 level 升序分批执行 handler → close(waitChan) → Listen 返回` 的顺序在信号触发与 `Shutdown()` 主动触发两条路径下均严格成立。
- **并发原语正确性**：`sync.Once`、`RWMutex`、`WaitGroup`、channel 的组合使用无死锁、无竞争、无重复执行。
- **panic 隔离**：单个 handler panic 不影响同级/后续 handler 及 `Listen` 返回。
- **信号处理语义**：SIGTERM/SIGINT/SIGQUIT 触发退出、SIGHUP 忽略、退出流程不可逆。
- **跨平台行为**：Windows 下信号支持有限（`signal.Notify` 对 SIGQUIT 等的实际行为），确认模块在 Windows 的可用性边界。

## 2. 审查范围

模块仅 1 个源文件 + 1 个测试文件，一次审查完成，但因全是并发原语组合，需**逐行**精读：

| 文件 | 行数 | 内容 |
|---|---|---|
| `quit.go` | 132 | 全局上下文、信号监听、handler 注册与分级并发执行 |
| `quit_test.go` | — | 单元测试 |

## 3. 审查清单

### 3.1 初始化与全局状态

- [ ] `init()` 中 `context.WithCancel(context.Background())` 生成 `Ctx`/`cancel`；`Ctx` 为导出变量——评估被外部误赋值覆盖的风险（`zcquit.Ctx = nil`），是否值得改为函数返回（记录为 API 设计讨论项，涉及破坏性变更需谨慎）。
- [ ] 全局变量清单核对：`listenOnce`、`shutdownOnce`、`waitChan`、`signalHandlerMap`、`signalHandlerMux`——每个变量的保护机制标注清楚。

### 3.2 AddSigHandler（注册路径）

- [ ] 首次调用触发 `doListen()`（`sync.Once`）——`Listen` 与 `AddSigHandler` 两个入口都调 `doListen` 时监听仅启动一次。
- [ ] `signalHandlerMap[level] = append(...)` 全程持 `Lock`；map 为 nil 时的初始化位置。
- [ ] 变参 handler 为空、handler 为 nil 时的行为（nil handler 执行时 panic 会被 recover 吗——建议注册期过滤 nil，记录为改进项）。
- [ ] **退出流程已开始后再注册**的竞争场景：快照已固化，新增 handler 本次不执行——该语义是否与文档"仅在下次退出时生效"一致（而实际退出流程只会发生一次，"下次"永不到来——文档表述准确性核对）。

### 3.3 listen goroutine（信号路径）

- [ ] `signal.Notify` 注册的信号集：SIGHUP/SIGTERM/SIGINT/SIGQUIT；channel 缓冲大小（无缓冲会丢信号，标准做法至少 1）。
- [ ] 循环中 SIGHUP → continue，其余 → break；break 后是否 `signal.Stop`/`signal.Reset`（不停止的话后续信号仍进 channel 但无人消费——评估第二次 Ctrl+C 是否应恢复默认行为立即强杀，记录为设计讨论项）。
- [ ] **Windows 平台**：`os.Interrupt`（Ctrl+C）可用，SIGTERM/SIGQUIT 在 Windows 的 `signal.Notify` 行为——确认编译无错且注释说明平台差异。

### 3.4 executeShutdown（执行路径，核心）

- [ ] **时序第一步**：`cancel()` 必须先于一切 handler 执行（业务协程先收到通知）。
- [ ] 快照逻辑：`RLock` → 拷贝 map（每个 level 的 slice 是否**深拷贝**——浅拷贝 slice header 后若注册路径 append 触发扩容无碍，但未扩容时共享底层数组，确认快照后注册不会污染正在迭代的 slice）→ 立即 `RUnlock`——handler 执行全程不持锁（文档承诺 handler 内可安全调 `AddSigHandler`）。
- [ ] level 排序：升序（`sort.Ints` 或等价）；负数 level、重复 level 的行为。
- [ ] 同级并发：每个 handler 独立 goroutine + `wg.Add/Done` 配对；`wg.Wait()` 在每个 level 之间（级间串行）。
- [ ] **panic recover**：recover 在每个 handler goroutine 内部（defer 位置正确）；`slog.Error` 记录含 level 信息；panic 后 `wg.Done` 仍执行（Done 在 defer 中且先于/独立于 recover——顺序核对，Done 若在 recover 之前的语句而非 defer，panic 会跳过导致 Wait 永久阻塞，Blocker 级检查项）。
- [ ] handler 参数 `sig os.Signal`：信号路径传实际信号、`Shutdown()` 路径传 nil——文档已说明，handler 侧对 nil 的健壮性提示。
- [ ] **最后一步**：所有 level 完成后 `close(waitChan)`；仅关闭一次（由 Once 保证还是由流程唯一性保证——确认信号路径与 Shutdown 路径并发触发时不会 double close panic，重点核对 `shutdownOnce` 的包裹范围是否覆盖两条路径的公共入口）。

### 3.5 Listen / Shutdown

- [ ] `Listen()`：`doListen()` + `<-waitChan` 阻塞；多个 goroutine 同时 `Listen()` 均能被 close 唤醒。
- [ ] `Shutdown()`：`shutdownOnce` 保证仅首次生效；与信号路径的互斥——信号与 Shutdown **同时**触发时 executeShutdown 只执行一次（两条路径是否收敛到同一个 Once）。
- [ ] `Shutdown()` 在 `Listen()` 之前调用的行为：waitChan 已关闭后再 Listen 应立即返回——核对。
- [ ] 未调用 `AddSigHandler` 也未调用 `Listen`，直接 `Shutdown()`：doListen 未跑时 waitChan 的关闭路径是否仍完整（谁负责 close）。

### 3.6 边界与竞争场景矩阵

逐一构造以下场景核查（代码走查 + 测试验证）：

| 场景 | 期望行为 |
|---|---|
| 信号到达 + Shutdown 并发 | 退出流程仅执行一次，无 double close |
| handler 内调用 Shutdown | 无死锁（Once 重入安全） |
| handler 内调用 AddSigHandler | 无死锁（锁已释放），新 handler 不执行 |
| handler 内读 `Ctx.Done()` | 已关闭（cancel 先行） |
| 0 个 handler 时收到信号 | cancel + 直接 close，Listen 正常返回 |
| handler 全部 panic | 全部被记录，Listen 仍返回 |
| Listen 前信号已到达 | 由 doListen 启动时机决定——核对首次注册即启动监听的语义 |

## 4. 专项审查

### 4.1 并发验证

- [ ] `go test -race -count=10 ./zcquit/...`（多次运行放大竞争窗口）。
- [ ] 压力场景：100 个 goroutine 并发 `AddSigHandler` + 触发 `Shutdown` + 并发 `Listen`。

### 4.2 可观测性与超时（改进评估项，非阻塞）

- [ ] handler 无超时机制：单个 handler 卡死 → `wg.Wait()` 永久阻塞 → 进程无法退出。评估是否提供带超时的执行选项或在文档中强调 handler 自行控制超时（对照文档示例中 `srv.Shutdown(ctx)` 已带 timeout 的示范性）。
- [ ] 退出流程各阶段是否有 `slog.Info` 级别的进度日志（level X 开始/完成），便于排查退出卡住。

## 5. 测试审查

- [ ] `quit_test.go` 现有用例覆盖核对：分级顺序（level 间串行）、同级并发性、panic 隔离、Shutdown 幂等、Ctx 取消先行、Listen 解除阻塞。
- [ ] 全局状态与 `sync.Once` 导致**测试间不可重置**：核对测试如何处理（单测试进程只能走一次退出流程——用例是否因此合并为单个流程测试，覆盖是否受限；评估内部函数级测试的补充空间）。
- [ ] 信号触发路径的测试方式：真实 `syscall.Kill`（Windows 不可用）还是抽象注入——确认 Windows 上 `go test ./zcquit/...` 可通过。
- [ ] 补测建议（若缺失）：3.6 矩阵中的并发场景、负数 level 排序、nil handler。
- [ ] 执行：`go test -race -count=10 ./zcquit/...`，覆盖率目标 ≥ 85%。

## 6. 文档一致性核对

- [ ] `docs/zcquit.md` 的关键时序三条、同步机制表、线程安全四条、注意事项七条逐一与实现比对。
- [ ] 注意事项 4"新增的 handler 仅在下次退出时生效"——与"退出流程不可逆（注意事项 7）"存在表述矛盾（不会有下次），建议文档修订为"本次退出不执行"。
- [ ] `SigHandler` 的 `sig` 参数在 Shutdown 路径为 nil 已在文档说明——核对代码注释同步。

## 7. 产出物与完成标准

- **问题清单**：`文件:行号` + 场景描述 + 期望/实际时序 + 严重级别。
- **严重级别**：
    - Blocker：死锁、double close panic、wg.Done 泄漏导致永久阻塞、退出流程重复执行。
    - Major：时序违反（handler 先于 cancel）、快照竞争、panic 逃逸。
    - Minor：nil handler 未过滤、缺进度日志、无超时机制说明。
    - Nit：文档表述矛盾、注释补齐、Ctx 导出变量设计讨论。
- **完成标准**：清单全部勾选；`-race -count=10` 通过；3.6 场景矩阵全部验证；Blocker/Major 建立修复项。
