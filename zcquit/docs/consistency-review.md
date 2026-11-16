# zcquit 文档—实现一致性审查报告

> 审查状态：已完成审查，并已按授权修复全部 9 条（DOC-01 至 DOC-09）。
> 审查基线：审查时 HEAD `26a978c` + 未提交改动；审查后仓库推进到 `c8bf66e`（本次提交之前），其间 `zcquit` 目录无改动。行号来自工作区；修复均为行内替换、未增删行，行号同时适用于修复前后。
> 说明：详情中的「文档证据」记录的是修复前原文，对应行现已被「修复执行记录」按修复方案改写；锚点核对以「修复执行记录」为准。

## 审查范围与方法

- 被审文档：
  - `zcquit/docs/zcquit.md`（工作区版本，625 行）
  - `README.md` 的 zcquit 段落（第 18、74、144、149、174 行）
  - `zcquit/quit.go` 的包注释（第 1-32 行）
- 对照源码：`zcquit/quit.go`（268 行，导出符号 5 个：`AddSigHandler`、`GetCtx`、`Listen`、`Shutdown`、`SigHandler`）。
- 对照测试：`zcquit/quit_test.go`、`zcquit/quit_signal_integration_test.go`、`zcquit/quit_signal_integration_unix_test.go`、`zcquit/quit_signal_platform_windows_test.go`、`zcquit/docs_examples_compile_test.go`。
- 基线说明：工作区不干净（`zcquit/docs/zcquit.md`、`zcquit/docs_examples_compile_test.go` 有未提交改动），因此行号来自工作区，机械校验使用 `--worktree`。
- 方法：
  - 正向逐声明比对：文档每条可验证声明 → 源码分支 → 锁死测试；
  - 运行时实测：编写独立探针程序（临时目录，未写入仓库），覆盖示例程序真实信号流程、`handler` 内 `Listen`、`GetCtx` 行为、`taskkill` 平台行为；
  - 反向审查：从 5 个导出符号与 `init`/`listen`/`doShutdown`/`executeShutdown`/`doListen` 路径反查文档遗漏。
- 实测清单：
  - 文档示例 1 编译运行，向独立进程组投递 `CTRL_BREAK_EVENT`（Windows 映射为 `SIGINT`），观察日志顺序与退出码；
  - `handler` 内调用 `Listen()`，2 秒窗口内观察是否返回；
  - `GetCtx()` 的 `Err()`、`Deadline()`、`Value()` 初始值与退出后值；
  - `taskkill`（不带 `/F`）与 `taskkill /F` 对 zcquit 控制台程序的实测。
- 未实测项及原因：SIGHUP/SIGQUIT 的真实投递（Windows 无该信号来源，仅有 Unix 测试覆盖）；`signal.Stop` 的实际执行时点（无法在进程外直接观察）；主动路径退订与第二信号的时序细节（已有集成测试锁死）。
- 问题总数：9 条；级别分布：中 1 条、低 8 条、高 0 条。
- 计入标准：会影响调用方理解、可被实际行为推翻、或与源码/测试不一致的表述。纯排版风格、不影响契约的措辞不计入。

## 问题汇总表

| 编号 | 级别 | 状态 | 一句话结论 |
|---|---|---|---|
| DOC-01 | 中 | 已修 | 9.5 说 `sig` 保留「哪个信号先到」的信息，与 5.5、3.5 的「由 CAS 获胜路径决定、不保证先到先得」矛盾 |
| DOC-02 | 低 | 已修 | 8.2 标题为「外部包测试」，但真实信号集成测试是内部包 `package zcquit` |
| DOC-03 | 低 | 已修 | 9.4 把 `stopListenCh` 的 `select` 守卫归因于「包内测试」，源码注释只说是防重复关闭 |
| DOC-04 | 低 | 已修 | 3.2 表把主动路径 `signal.Stop` 的时点锚定到 `close(stopListenCh)`，实际 `Stop` 在监听协程收到该通知之后执行 |
| DOC-05 | 低 | 已修 | 4.1 状态表 `stopListenCh` 的「读取方」漏了 `executeShutdown` 里的 `select` 守卫 |
| DOC-06 | 低 | 已修 | 3.1 称 ③ 先于 ④ 是「便于按日志判断收尾阶段」，但两处 `close` 之间没有任何日志 |
| DOC-07 | 低 | 已修 | 5.2 只写 nil handler「直接 panic」，未给出测试锁死的 panic 消息，调用方无法可靠识别 |
| DOC-08 | 低 | 已修 | 6.4 称 `taskkill`（不带 `/F`）「不是可靠的优雅退出入口」，实测是对控制台程序直接失败、根本不会终止 |
| DOC-09 | 低 | 已修 | 6.2/6.3 称 `Reset`/`Stop` 后「恢复默认处置」，未覆盖进程启动时 SIGHUP/SIGINT 已被忽略的边界 |

## 逐条详情

### DOC-01：`sig` 的「先到」表述与获胜路径语义矛盾

- 级别：中；类型：文档内部矛盾 / 误导性契约
- 文档证据：`zcquit/docs/zcquit.md:411` 写「传 `os.Signal` …… 保留了「哪个信号先到」的信息」；而 `zcquit/docs/zcquit.md:288-289` 与 `zcquit/docs/zcquit.md:172` 都明确「`sig` 由取得执行权的路径决定」「并发触发不保证先到先得」。
- 实现证据：`zcquit/quit.go:85-87` 注释说明 `sig` 由取得退出执行权的路径决定；`zcquit/quit.go:142` 把赢得 CAS 的 `sig` 传给 `doShutdown`，未获胜路径不会执行 handler。
- 测试证据：`zcquit/quit_test.go:736` 的 `TestShutdown_FirstTriggerWins` 用「主动退出」与「信号入口」两个子用例验证 `sig` 由获胜路径决定，并断言后续触发不改变 `sig`。
- 实测：无需新增探针——并发触发下 `sig` 取决于 CAS 获胜者而非到达顺序，测试已锁死；本轮 `probe_ctx` 实测主动路径下 handler 正常执行（`level 0` 开始日志），未发现相反证据。
- 影响：读者若只读 9.5，会误以为 `sig` 反映「先到达的信号」，进而在日志/审计中把 `sig` 当作到达顺序证据。
- 推荐方案：把 9.5 的「保留了哪个信号先到的信息」改为「保留了哪个信号赢得执行权的信息」，与 5.5/3.5 统一；或删去该半句，只保留「带 `os.Signal` 可区分主动/信号路径」。

### DOC-02：8.2 把内部包的信号集成测试归为「外部包测试」

- 级别：低；类型：文档错误 / 分类不准确
- 文档证据：`zcquit/docs/zcquit.md:367` 标题为「8.2 外部包测试：子进程隔离」，`zcquit/docs/zcquit.md:369` 以 `zcquit_test` 包（`docs_examples_compile_test.go`）开头，随后 `zcquit/docs/zcquit.md:371` 把「验证真实信号相关行为的测试」一并归入该标题。
- 实现证据：`zcquit/quit_signal_integration_test.go:1` 声明 `package zcquit`（内部包）；`zcquit/docs_examples_compile_test.go:1` 声明 `package zcquit_test`（外部包）。
- 测试证据：`zcquit/quit_signal_integration_test.go:217` 的 `startSignalHelper` 是内部包内的子进程 helper，与外部包无关。
- 实测：无需实测，包声明可直接读取。
- 影响：读者按「外部包测试」去找信号测试写法时，会找不到对应文件；把内部包与外部包的隔离手段混为一谈。
- 推荐方案：标题改为「无法重置全局状态的测试：子进程隔离」，正文分两句：外部包 `docs_examples_compile_test.go` 用 `exec` 跑示例；内部包信号测试用 `ZCQUIT_SIGNAL_HELPER` 环境变量自启子进程。

### DOC-03：9.4 对 `stopListenCh` 守卫的归因超出源码注释

- 级别：低；类型：措辞超出证据
- 文档证据：`zcquit/docs/zcquit.md:407` 写「`stopListenCh` 多一层 `select` 守卫……是为了让包内测试反复调用 `executeShutdown` 时不会二次关闭通道」。
- 实现证据：`zcquit/quit.go:201-203` 的注释只说「`stopListenCh` 由包初始化创建一次、不重建，故用 select 守卫防止重复关闭导致 double close panic」，未提测试。
- 测试证据：`zcquit/quit_test.go:18-32` 的 `resetState` 注释确实说明 `stopListenCh` 不重建、`executeShutdown` 以幂等守卫关闭，可支撑「测试场景」的推断，但源码层面的直接意图仍是通用防重复关闭。
- 实测：无需实测，属于注释归因比对。
- 影响：读者会以为守卫只为测试存在，生产路径不需要；实际上它同时防止任何重复调用导致的崩溃。
- 推荐方案：改为「源码注释的直接理由是防止重复关闭导致 double close panic；包内测试依赖该守卫，但守卫并非只为测试存在」。

### DOC-04：3.2 表把主动路径 `signal.Stop` 的时点锚定到错误的行

- 级别：低；类型：文档错误 / 锚点不精确
- 文档证据：`zcquit/docs/zcquit.md:126` 写主动路径「清理完成后，由监听 goroutine 收到 `stopListenCh` 才执行」，并附源码锚点指向 `close(stopListenCh)` 处。
- 实现证据：`zcquit/quit.go:204-208` 是 `executeShutdown` 里 `close(stopListenCh)` 的守卫；真正的 `signal.Stop` 在 `zcquit/quit.go:126`（监听协程的 `case <-stopListenCh` 分支）。
- 测试证据：无直接断言 `signal.Stop` 执行时点的测试；`TestShutdown_FirstSignalDuringBlockedHandler` 只验证不重复清理与不强制终止。
- 实测：无法在进程外直接观察 `signal.Stop`，依据代码分支确定。
- 影响：读者按锚点查看时会看到 `close`，误以为关闭通道本身就是退订；实际退订发生在监听协程被唤醒之后，存在调度窗口。
- 推荐方案：锚点改为同时给出 `zcquit/quit.go:126`（执行 `Stop`）与 `zcquit/quit.go:204-208`（触发通知），并明确「`Shutdown` 返回不保证 `Stop` 已执行」。

### DOC-05：4.1 状态表 `stopListenCh` 的读取方不完整

- 级别：低；类型：关键细节遗漏
- 文档证据：`zcquit/docs/zcquit.md:188` 的「读取」列只写「监听 goroutine」。
- 实现证据：`zcquit/quit.go:204-208` 的 `select` 守卫也读取 `stopListenCh`（用于判断是否已关闭）；`zcquit/quit.go:124` 是监听协程的读取。
- 测试证据：无（状态清单是文档描述，不是可执行行为）。
- 实测：无需实测，代码可直接读出两个读取点。
- 影响：读者会漏掉 `executeShutdown` 通过该通道做幂等判断这一层，难以理解「谁在什么条件下关闭它」。
- 推荐方案：「读取」列补「`executeShutdown` 的 `select` 守卫（判断是否已关闭）」。

### DOC-06：3.1 对 ③ 先于 ④ 的理由无代码依据

- 级别：低；类型：错误论证
- 文档证据：`zcquit/docs/zcquit.md:100` 写「实现把 ③ 放在 ④ 前，只是让「停止监听」的通知先于「对外放行」发出，便于按日志判断收尾阶段」。
- 实现证据：`zcquit/quit.go:201-213` 在 `close(stopListenCh)` 与 `close(waitChan)` 之间没有任何日志；收尾阶段的日志只有各级 handler 的开始/完成。
- 测试证据：无（文档推理，不涉及行为断言）。
- 实测：无需实测，代码中该区间无日志语句。
- 影响：读者会去寻找「按日志判断收尾阶段」的能力，但实际不存在；也会误以为 ③④ 的先后有可观测意义。
- 推荐方案：改为「实现顺序如此；`Listen` 的契约只依赖 ④，③ 的先后不影响 `Listen` 返回时点」。

### DOC-07：5.2 未给出 nil handler 的 panic 消息

- 级别：低；类型：诊断细节缺失
- 文档证据：`zcquit/docs/zcquit.md:239` 只写「发现 nil 直接 panic（`TestAddSigHandler_NilHandlerPanics`）」，未写 panic 值。
- 实现证据：`zcquit/quit.go:261` 的 panic 值为字符串 `zcquit: 不允许注册 nil 的 SigHandler`。
- 测试证据：`zcquit/quit_test.go:392` 断言 panic 消息包含 `zcquit: 不允许注册 nil 的 SigHandler`。
- 实测：运行 `go test ./zcquit -run TestAddSigHandler_NilHandlerPanics -count=1` 通过（本轮全量测试已覆盖），无需额外探针。
- 影响：调用方若在注册包装层 `recover` 后按 panic 值分类，缺少稳定标识，只能把它当作未知 panic。
- 推荐方案：在 5.2 补一句「panic 值为字符串 `zcquit: 不允许注册 nil 的 SigHandler`」，并说明它不是 `error`。

### DOC-08：6.4 对 `taskkill` 的描述弱于实测行为

- 级别：低；类型：文档错误 / 与实测不符
- 文档证据：`zcquit/docs/zcquit.md:331` 写「`taskkill`（不加 `/F`）不是可靠的优雅退出入口，`taskkill /F` 直接终止进程」。
- 实现证据：不适用（平台外部行为，不涉及 zcquit 代码）。
- 测试证据：无（仓库没有 taskkill 相关测试）。
- 实测：对 `probe_wait.exe`（注册了 level 0 handler 并 `Listen` 的控制台程序）执行 `taskkill /PID <pid>`，输出 `ERROR: The process with PID ... could not be terminated. Reason: This process can only be terminated forcefully (with /F option).`，退出码 1，进程仍存活，stdout 只有 `READY`、无 `CLEANUP_RAN`；随后 `taskkill /F /PID <pid>` 退出码 0，进程被终止，仍未输出 `CLEANUP_RAN`。
- 影响：读者可能以为不带 `/F` 会「尝试优雅关闭但不保证」，实际是直接失败、进程根本不会退出；排障时会误判。
- 推荐方案：改为「对控制台程序，`taskkill`（不带 `/F`）会直接失败（`could not be terminated ... /F option`），进程继续运行；`taskkill /F` 强制终止，信号回调与清理都不执行」。

### DOC-09：6.2/6.3 未覆盖「进程启动时 SIGHUP/SIGINT 已被忽略」的边界

- 级别：低；类型：关键行为遗漏 / 边界未说明
- 文档证据：`zcquit/docs/zcquit.md:310` 写「要恢复默认处置只能由调用方 `signal.Reset`」；`zcquit/docs/zcquit.md:324` 写「`signal.Stop` 只在所有订阅该信号的 channel 都被 Stop 时才恢复默认处置」。
- 实现证据：`zcquit/quit.go:126` 与 `zcquit/quit.go:139` 只调用 `signal.Stop`；是否恢复默认取决于 Go 运行时对「启动时已被忽略」信号的处理。
- 测试证据：`zcquit/quit_signal_integration_test.go:367` 的 `TestListen_SecondSignalDefaultTerminates` 覆盖「启动时未被忽略 + 无其他订阅者」的默认终止，未覆盖「启动时已忽略」。
- 实测：未实测——本机 Windows 无 SIGHUP 投递能力，且「以 SIG_IGN 启动」难以在当前会话构造；依据 `GOROOT/src/os/signal/doc.go:76-83`（启动时 SIGHUP/SIGINT 已被忽略时，`Reset` 或 `Stop` 全部 channel 后信号「再次被忽略」）。
- 影响：以「忽略 SIGHUP 启动」（如 `nohup` 某些写法、父进程设 SIG_IGN）的程序，在 zcquit 退出后 `Reset` 并不恢复终止语义，读者按文档预期会出错。
- 推荐方案：6.2 与 6.3 各补半句「若进程启动时该信号已被忽略，`Reset`/`Stop` 后仍保持忽略，平台默认处置不会恢复」，并在 6.3 的边界说明处引用该条件。

## 已核对且未发现偏差的关键行为

以下声明逐条对照了源码、测试或实测，未发现偏差，后续修复不要改动：

- 「一次退出四个动作、`cancel` 先于 handler」：实测示例 1 日志顺序为「业务协程收到退出信号」→「开始执行退出处理函数 level 0」→「业务协程已结束」→ level 0 完成 → level 1 →「程序优雅退出」，退出码 0。
- 「handler 按 level 升序、同级并发、跨级串行」：`zcquit/quit_test.go:89`、`zcquit/quit_test.go:125`、`zcquit/quit_test.go:160` 三个测试锁死。
- 「handler 内调用 `Listen()` 必死锁」：实测 `probe_listen` 输出 `HANDLER_LISTEN_BLOCKED`，2 秒内未返回；日志只有 level 0 开始、无完成。
- 「`GetCtx()` 无 deadline/value，退出后 `Err()` 为 `context.Canceled`」：实测输出 `initial_err=<nil> has_deadline=false value_is_nil=true`、`after_err=context canceled is_canceled=true`。
- 「`Shutdown()` 先于 `Listen()` 时 `Listen()` 立即返回」：`zcquit/quit_test.go:570` 的 `TestListen_AfterShutdownReturnsImmediately` 锁死。
- 「多协程并发 `Listen` 全部返回」：`TestShutdown_FullFlow` 用 3 个 goroutine 并发调用并断言全部返回。
- 「nil handler 整批拒绝、不部分写入」：`TestAddSigHandler_NilHandlerPanics` 断言 panic 前 map 为空。
- 「快照后新增 handler 不执行」：`TestExecuteShutdown_HandlerCallsAddSigHandler` 锁死。
- 「日志字段 `zckg=zcquit`、`shutdownLevel`、`count`」：实测示例 1 输出与 `zcquit/quit.go:181`、`zcquit/quit.go:198` 一致。
- 「Windows Ctrl+Break 映射为 `os.Interrupt`」：实测示例 1 日志 `signal=interrupt`；`GOROOT/src/runtime/os_windows.go:1104-1105` 与之一致。
- 「第二个信号默认终止 / 其他订阅者场景」：`TestListen_SecondSignalDefaultTerminates`、`TestListen_SecondSignalWithIndependentNotify` 锁死（Windows 断言退出码 `0xC000013A`）。
- 「SIGHUP 忽略、SIGQUIT 不再 dump」：依据 `GOROOT/src/os/signal/doc.go:63-66` 与 `TestListen_SIGHUPIgnored`（Unix 测试，本机未运行）。
- 「Windows SIGTERM 只给有限清理窗口」：`GOROOT/src/runtime/os_windows.go:1113-1118` 与文档一致。
- 「README 的 zcquit 描述与示例」：`README.md:18`、`README.md:74`、`README.md:144`、`README.md:149`、`README.md:174` 与文档无矛盾（level 10 属任意整数用法）。
- 「包注释与文档一致」：`zcquit/quit.go:1-32` 与文档 1、3、5 章无矛盾。

## Go 示例覆盖

| 文档#章节#序号 | 语法/结构 | 证据/原因 |
|---|---|---|
| zcquit.md#2.-五分钟上手#1 | 通过 | `doc_go_examples.py --strict` 输出 `parse=ok`；`TestDocExamples_Compile` 真实 `go build` 通过；本轮实测运行并投递 `CTRL_BREAK`，日志顺序与文档 2 节一致 |
| zcquit.md#11.1-主动触发退出#1 | 通过 | `parse=ok`；`TestDocExamples_Compile` 编译通过；行为由 `TestExecuteShutdown_SignalPassedToHandler` 覆盖 `sig=nil` |
| zcquit.md#11.2-HTTP-服务多级清理#1 | 通过 | `parse=ok`；`TestDocExamples_Compile` 编译通过；`TestDocExamples_HTTPPortConflict` 用随机占用端口运行并校验三级清理日志顺序 |

## 修复执行记录

- DOC-01：把 `zcquit/docs/zcquit.md:411` 的「哪个信号先到」改为「哪个信号取得了执行权」，与 5.5/3.5 的获胜路径语义统一。载体：文档。
- DOC-02：`zcquit/docs/zcquit.md:367` 标题改为「无法重置的测试：子进程隔离」；正文补充「外部包用 `exec` 跑示例、内部包用 `ZCQUIT_SIGNAL_HELPER` 自启子进程」。载体：文档。
- DOC-03：`zcquit/docs/zcquit.md:407` 改为「直接理由是防止重复关闭导致 double close panic；包内测试依赖该守卫，但守卫并非只为测试存在」。载体：文档。
- DOC-04：`zcquit/docs/zcquit.md:126` 拆开 `close(stopListenCh)` 与 `signal.Stop` 两个时点，补 `quit.go:126` 锚点与「`Shutdown` 返回不保证已执行」。载体：文档。
- DOC-05：`zcquit/docs/zcquit.md:188` 的读取方补「`executeShutdown` 的 `select` 守卫（判断是否已关闭）」。载体：文档。
- DOC-06：`zcquit/docs/zcquit.md:100` 改为「实现顺序是 ③ 在 ④ 前；该顺序不影响 `Listen` 的返回时点，代码在这两步之间也没有日志输出」。载体：文档。
- DOC-07：`zcquit/docs/zcquit.md:239` 补 panic 值字符串 `zcquit: 不允许注册 nil 的 SigHandler`。载体：文档。
- DOC-08：`zcquit/docs/zcquit.md:331` 按实测改写为「不带 `/F` 直接失败、进程继续运行；`/F` 强制终止且不执行清理」。载体：文档。
- DOC-09：`zcquit/docs/zcquit.md:310` 补「启动时该信号已被忽略则 `Reset` 后仍保持忽略」；`zcquit/docs/zcquit.md:324` 补「`Stop` 全部订阅后该信号仍保持忽略」。载体：文档。
- 未改代码与测试的理由：9 条均为文档表述、锚点或分类问题，对应行为已被现有测试或本轮实测确认与代码一致，不存在实现缺陷；按技能约定「文档错误 → 正式文档」，不改 `quit.go` 与测试。

## 实际验收结果

| 验收项 | 结果 |
|---|---|
| `doc_go_examples.py --strict` | 0 错误，3 个 Go 块全部 `parse=ok` |
| `go test ./zcquit -run TestDocExamples -count=1` | PASS（14.55s） |
| `go test ./zcquit -count=1 -race` | PASS（28.43s），无 DATA RACE；唯一 SKIP 为 helper 父进程入口 |
| `git diff --check -- zcquit/docs/zcquit.md` | 退出码 0，无行尾空白问题 |
| `gofmt -l zcquit` | 无输出 |
| `validate_report.py`（结构 + `--anchors`） | 0 错误、0 警告，全部引用锚点语义核对通过 |

定向复核：

- 全文搜索「先到」：只剩 5.5/3.5 的否定表述「不保证先到先得」，9.5 已改为「取得执行权」。
- 3.2 表同时指向 `quit.go:126`（执行 `Stop`）与 `quit.go:204-208`（关闭 `stopListenCh`），并注明 `Shutdown` 返回不保证已执行。
- 5.2 已出现完整 panic 字符串；6.4 的 `taskkill` 句与实测一致；4.1/8.2/9.4 的表述已按报告改写。

结论：9 条问题全部修复；本轮只改文档，未改 `quit.go` 与测试，行为不变量不受影响。
