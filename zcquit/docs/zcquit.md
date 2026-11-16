# zcquit 优雅退出

zcquit 把「进程如何优雅退出」收敛成三件事：

- 一个全局可取消上下文（`GetCtx`），退出开始时取消；
- 一个按级别执行的清理队列（`AddSigHandler`）；
- 一条把操作系统信号接到清理队列的监听（`Listen`），以及一个主动触发入口（`Shutdown`）。

本文不是 API 清单，而是**执行模型**：先讲一次退出到底发生什么，再讲每个公开符号的契约，最后讲实现为什么这样写。关键行为都标注实现位置（`quit.go` 行号）与锁死它的测试（`quit_test.go`、`quit_signal*_test.go`），读者可以按锚点自行核对。

> 文中所有 `go` 代码块都是完整示例程序（省略 `package` 声明，校验测试会补齐 `package main`），由 `docs_examples_compile_test.go` 真实编译并运行关键示例；伪代码、时序图和反例使用 `text` 块。

## 1. 定位与边界

zcquit 只做「通知 + 编排 + 接线」，不替调用方管理业务生命周期。边界先讲清楚，后面的设计才容易理解：

| 维度 | zcquit 提供 | zcquit 不提供 |
|---|---|---|
| 退出通知 | `GetCtx()` 返回的上下文在退出开始时取消 | 不 join 业务协程，不等待其收尾完成 |
| 清理编排 | 按级别升序、同级并发、跨级串行，单个 handler panic 隔离 | 不提供超时、重试、强制终止 |
| 触发入口 | 三个终止类 OS 信号（另接管 SIGHUP 但忽略）+ `Shutdown()` 主动触发 | 一个进程内只能退出一次，不支持重置或复用 |
| 信号处置 | 接管 SIGHUP / SIGTERM / SIGINT / SIGQUIT | 不恢复默认处置，不提供 reload 钩子 |
| 可观测性 | 每级开始/结束日志、panic 日志，统一走 `slog.Default()` | 不注入指标，不封装 logger |

「进程内一次性」是设计前提而不是遗漏：`init` 创建唯一的全局上下文，`Listen` 与 `Shutdown` 共用同一个 `waitChan`，退出完成后所有入口都退化为 no-op 或立即返回。第 8 章给出在测试中使用 zcquit 的可行方式。

## 2. 五分钟上手

一个最小但完整的程序：业务协程监听上下文，两级 handler 分别等待业务收尾、关闭资源。

```go
// 示例 1：基本用法（worker 门控 + 分级清理）
import (
	"log/slog"
	"os"

	"github.com/buexplain/zckg/zcquit"
)

func main() {
	workerStart := make(chan struct{})
	workerDone := make(chan struct{})
	go runServer(workerStart, workerDone) // 先启动，注册期间只等待放行或取消

	// level 0：ctx 只通知退出，必须显式等待业务完成收尾
	zcquit.AddSigHandler(0, func(sig os.Signal) {
		<-workerDone
		slog.Info("业务协程已结束", "signal", sig)
	})
	// level 1：等待业务结束后，才能关闭其依赖资源
	zcquit.AddSigHandler(1, func(sig os.Signal) {
		slog.Info("开始清理资源...", "signal", sig)
	})

	close(workerStart) // 所有 handler 注册完毕，再放行业务
	zcquit.Listen()
	slog.Info("程序优雅退出")
}

func runServer(start <-chan struct{}, done chan<- struct{}) {
	defer close(done) // 必须在业务收尾完成后通知等待者
	select {
	case <-zcquit.GetCtx().Done():
		return // 尚未放行就退出，无业务需要收尾
	case <-start:
	}
	// 此处执行实际业务，期间也应检查 GetCtx().Done()
	<-zcquit.GetCtx().Done()
	slog.Info("业务协程收到退出信号")
}
```

这个程序按下 Ctrl+C 后的实际顺序：

1. `AddSigHandler` 首次返回时，`signal.Notify` 已同步完成，信号已能送达（[quit.go:216-224](../quit.go#L216-L224)）。
2. `Listen()` 先执行首次监听初始化，再阻塞在 `waitChan` 上（[quit.go:110-113](../quit.go#L110-L113)）。
3. SIGINT 到达 → `listen` 停掉自身订阅 → 取得执行权 → `cancel()` 取消 `GetCtx()`。
4. `runServer` 看到 `Done()`，完成收尾后 `close(workerDone)`。
5. level 0 handler 等到 `workerDone` 后返回；随后 level 1 handler 关闭资源。
6. `close(waitChan)`，`Listen()` 返回，`main` 打印最后一行。

注意这里没有「Listen 自动等业务协程」这一步：等待被显式写在 level 0 handler 里。这是 zcquit 的核心用法——**用 handler 的级别表达资源依赖，用 handler 内部的等待表达业务收尾**。

## 3. 执行模型

### 3.1 一次退出只有四个动作

无论从哪条路径触发，取得执行权的一方依次做四件事（`executeShutdown`，[quit.go:158-214](../quit.go#L158-L214)）：

```text
① cancel()                       取消全局上下文，通知所有监听 GetCtx() 的协程
② 快照 + 排序 + 逐级执行 handler  读锁复制 map → 收集 level 升序 → 每级并发执行并等待
③ close(stopListenCh)           通知监听 goroutine 停止（带 select 守卫）
④ close(waitChan)               放行所有阻塞在 Listen 上的调用方
```

顺序不可交换，原因分别是：

- `cancel()` 必须先于 handler：业务协程只有先收到取消通知，才可能在 handler 等待它时完成收尾。
- ③ 与 ④ 的先后不构成 `Listen` 的契约：`Listen` 只依赖 ④（`waitChan` 关闭）。实现顺序是 ③ 在 ④ 前；该顺序不影响 `Listen` 的返回时点，代码在这两步之间也没有日志输出。
- ④ 必须最后：`Listen` 的语言承诺是「全部 handler 完成后返回」（[quit.go:94-113](../quit.go#L94-L113)）。

### 3.2 两条触发路径

zcquit 有两个触发入口，它们共用同一个「一次性」开关，但**信号退订的时点不同**，这是使用中最容易踩的差别：

```text
信号路径（Unix 以 SIGTERM 为例）                  主动路径（Shutdown）
──────────────────────────────────               ──────────────────────────────
OS 投递 SIGTERM → signalCH                        调用方 goroutine
listen goroutine 收到信号                         doListen()（已初始化则 no-op）
  ├─ SIGHUP：continue，继续等待                    doShutdown(nil) → CAS
  └─ 其他：signal.Stop(signalCH)   ← 先退订            ├─ 未取得：立即返回，不等待
           doShutdown(sig) → CAS                      └─ 取得：executeShutdown(nil)
              ├─ 未取得：listen goroutine 返回              ├─ cancel()
              └─ 取得：executeShutdown(sig)                ├─ 逐级执行 handler
                     ├─ cancel()                           ├─ close(stopListenCh)
                     ├─ 逐级执行 handler                     │    → listen goroutine 收到
                     ├─ close(stopListenCh)（无人接收）      │    → signal.Stop → 返回
                     └─ close(waitChan)                    └─ close(waitChan)
```

| 维度 | 信号路径 | 主动路径 |
|---|---|---|
| 谁执行清理 | 监听 goroutine 自己 | `Shutdown` 的调用方 |
| `signal.Stop` 时点 | 收到信号后、清理开始前（[quit.go:139](../quit.go#L139)） | 清理完成后 `close(stopListenCh)`（[quit.go:204-208](../quit.go#L204-L208)），监听 goroutine 收到后才执行 `signal.Stop`（[quit.go:126](../quit.go#L126)）；`Shutdown` 返回不保证已执行 |
| `stopListenCh` 的关闭 | 无人接收，仅满足统一收尾 | 唤醒监听 goroutine 去停订阅 |
| handler 收到的 `sig` | 该信号值 | `nil` |
| 入口返回时 | `Listen` 等全部 handler 完成 | 取得执行权的调用等全部 handler 完成；未取得者立即返回 |

### 3.3 四个「只能发生一次」的状态

```text
listenOnce      ──Do──▶ 已初始化：channel 创建 + signal.Notify + go listen，之后永不再做
shutdownStarted false ──CAS──▶ true：取得执行权，之后所有 doShutdown 直接返回
waitChan        open ──close──▶ closed：所有 Listen 解除阻塞，之后永不再关
stopListenCh    open ──close──▶ closed：监听 goroutine 停止，之后永不再关
```

四个状态的职责不同，不能合并：

- `listenOnce` 保证**订阅只建立一次**，与退出无关；
- `shutdownStarted` 保证**清理只执行一次**，并决定 `sig`；
- `waitChan` 只表达「清理已完成」，是 `Listen` 的返回条件；
- `stopListenCh` 只表达「监听 goroutine 该停了」，与 `Listen` 解耦。

### 3.4 谁在等谁

理解阻塞关系比记注意事项更有效。zcquit 的等待关系只有四条：

| 阻塞方 | 等待对象 | 解除条件 | 卡住时的后果 |
|---|---|---|---|
| `Listen` 调用方 | `waitChan` | `executeShutdown` 跑完全部 handler | 没有任何触发时永久阻塞（预期行为） |
| 取得执行权的 `Shutdown` 调用方 | 各级 handler 的 `WaitGroup` | 该级全部 handler 返回 | 某个 handler 卡死则永久阻塞 |
| 监听 goroutine | `signalCH` 或 `stopListenCh` | 信号到达，或清理完成关闭 `stopListenCh` | 清理不完成则一直驻留（`Listen` 不等它） |
| 每个 handler | 自身逻辑 | 自身返回 | 卡死会阻塞本级剩余 handler 的汇总，进而阻塞后续级别 |

由此得到两条硬性结论：

- **handler 内不能调用 `Listen()`**：handler 正在清理流程中，`Listen` 等 `waitChan`，而 `waitChan` 要等全部 handler（含当前这个）返回，必然死锁。`Shutdown()` 可以重入，因为 CAS 已置位会立即返回（[quit.go:149-154](../quit.go#L149-L154)）。
- **`Listen` 不 join 监听 goroutine**：主动路径下 `Listen` 可能在监听 goroutine 执行 `signal.Stop` 之前返回（[quit.go:226-233](../quit.go#L226-L233)）。

### 3.5 不变式清单

这些行为已被测试锁死，改实现前必须逐条确认：

1. 退出流程全局只执行一次，无论信号与 `Shutdown` 如何并发（`TestShutdown_FullFlow`）。
2. `cancel()` 先于任何 handler 执行（`TestExecuteShutdown_CancelBeforeHandlers`）。
3. handler 按 level 升序、同级并发、跨级串行（`TestExecuteShutdown_LevelOrder`、`TestExecuteShutdown_ConcurrentWithinLevel`、`TestExecuteShutdown_LevelSequential`）。
4. handler 列表在清理开始时快照，之后新增的不执行（`TestExecuteShutdown_HandlerCallsAddSigHandler`）。
5. `Listen` 可被多个协程并发调用，退出后调用立即返回（`TestShutdown_FullFlow`、`TestListen_AfterShutdownReturnsImmediately`）。
6. handler 的 `sig` 由取得执行权的路径决定，不保证先到先得（`TestShutdown_FirstTriggerWins`）。
7. 监听 goroutine 最多一个；`signal.Notify` 在首次注册返回前完成（`TestAddSigHandler_ListenerReady`、`TestAddSigHandler_ConcurrentFirstRegistrations`）。

## 4. 状态与调用链

### 4.1 包级状态清单

`quit.go` 的包级变量不多，但每个都有明确的「创建—写—读—终结」生命周期。按这张表读源码，比按函数顺序读更快：

| 状态 | 创建 | 写入 | 读取 | 终结 |
|---|---|---|---|---|
| `ctx` / `cancel` | 包 `init`（[quit.go:89-92](../quit.go#L89-L92)） | `executeShutdown` 调 `cancel()` | `GetCtx()` 的调用方 | 进程结束 |
| `listenOnce` | 包初始化 | `doListen` 的 `Once.Do`（[quit.go:216-224](../quit.go#L216-L224)） | `AddSigHandler`、`Listen`、`Shutdown` | 执行一次后不再生效 |
| `signalHandlerMap` | 包初始化 | `AddSigHandler` 持写锁 `append`（[quit.go:264-266](../quit.go#L264-L266)） | `executeShutdown` 持读锁快照 | 进程结束 |
| `signalHandlerMux` | 包初始化 | `Lock`（注册）、`RLock`（快照） | 同上 | 进程结束 |
| `shutdownStarted` | 包初始化 `false` | `doShutdown` 的 `CAS`（[quit.go:150](../quit.go#L150)） | `doShutdown` | 退出后恒为 `true` |
| `stopListenCh` | 包初始化 | `executeShutdown` 关闭（带 `select` 守卫，[quit.go:204-208](../quit.go#L204-L208)） | 监听 goroutine；`executeShutdown` 的 `select` 守卫（判断是否已关闭，[quit.go:204-208](../quit.go#L204-L208)） | 关闭后不可重开 |
| `waitChan` | 包初始化 | `executeShutdown` 关闭（无守卫，[quit.go:213](../quit.go#L213)） | `Listen` | 关闭后不可重开 |

### 4.2 调用链

```text
                      ┌─────────────────────────────────────────────┐
                      │ init：ctx, cancel = context.WithCancel(...)  │
                      └─────────────────────────────────────────────┘

AddSigHandler(level, handlers...)          Listen()                Shutdown()
  ├─ 逐个校验非 nil，nil 立即 panic           ├─ doListen()           ├─ doListen()
  ├─ 写锁 append 到 map[level]                └─ <-waitChan           └─ doShutdown(nil)
  └─ doListen()
       └─ Once.Do: make(chan, 1) → signal.Notify(SIGHUP, SIGTERM, SIGINT, SIGQUIT)
                   → go listen(channel)

listen(ch)                                  doShutdown(sig)
  ├─ <-stopListenCh：signal.Stop → return       ├─ CAS 失败：return
  ├─ <-ch：SIGHUP → continue                   └─ CAS 成功：executeShutdown(sig)
  └─ 其他信号：signal.Stop → doShutdown(sig)        ├─ cancel()
                                                    ├─ 快照 handler、level 升序
                                                    ├─ 每级：并发执行 + recover + wg.Wait
                                                    ├─ close(stopListenCh)
                                                    └─ close(waitChan)
```

## 5. 公开契约

### 5.1 `GetCtx`

**签名**：`func GetCtx() context.Context`

**语义**：返回包初始化时用 `context.WithCancel(context.Background())` 创建的上下文（[quit.go:49-53](../quit.go#L49-L53)）。它没有 deadline、没有 value，只在退出流程第一步被取消。

**保证**：

- 退出开始后 `GetCtx().Done()` 关闭，`GetCtx().Err()` 为 `context.Canceled`（`TestShutdown_FirstTriggerWins` 断言）。
- 取消与 handler 执行是两件事：上下文取消不会等待业务协程。
- 返回的是函数而不是导出变量，外部无法替换（[quit.go:45-47](../quit.go#L45-L47)）。

**边界**：不要在 handler 里依赖「上下文取消 = 业务已停止」；业务是否停止只能由业务自己的 done/WaitGroup 证明。

### 5.2 `AddSigHandler`

**签名**：`func AddSigHandler(level int, handler ...SigHandler)`

**语义**：把一批 handler 追加到指定级别（[quit.go:257-268](../quit.go#L257-L268)）。`level` 可以是任意整数（含负数），没有预设常量，升序执行。

**保证**：

- 先整批校验 nil，再一次性写入；发现 nil 直接 panic，panic 值为字符串 `zcquit: 不允许注册 nil 的 SigHandler`，且不会残留部分注册（`TestAddSigHandler_NilHandlerPanics`）。
- 写入在写锁内完成，与清理时的快照互斥。
- 写入后调用 `doListen()`：首次调用会**同步**完成 `signal.Notify` 并启动监听 goroutine，返回时信号订阅已就绪（`TestAddSigHandler_ListenerReady`、`TestAddSigHandler_ConcurrentFirstRegistrations`）。
- 在 handler 内调用不会死锁：handler 执行时不持锁（`TestExecuteShutdown_HandlerCallsAddSigHandler`）；但快照已固化，新注册的不会参与本次退出。

**边界**：

- 「首次注册返回即订阅就绪」只覆盖本次注册，不代表后续级别的 handler 已注册；所有 handler 应在任何退出触发前注册完。
- 退出流程完成后追加的 handler 不会执行，也不会重启监听。

### 5.3 `Listen`

**签名**：`func Listen()`

**语义**：确保监听已初始化，然后阻塞到 `waitChan` 关闭（[quit.go:110-113](../quit.go#L110-L113)）。

**保证**：

- 首次调用同步完成 `signal.Notify` 后再启动监听 goroutine，因此返回前订阅已生效。
- 多个协程可以同时调用，退出时全部返回（`TestShutdown_FullFlow`）。
- 退出流程此前已完成时立即返回——这是仓库锁死的不变量（`TestListen_AfterShutdownReturnsImmediately`）。
- 只等 handler，不等业务协程，也不 join 监听 goroutine。

**边界**：不能在 handler 内调用（3.4 节已给出死锁推导）。

### 5.4 `Shutdown`

**签名**：`func Shutdown()`

**语义**：主动触发与信号相同的清理流程（[quit.go:234-239](../quit.go#L234-L239)）。先 `doListen()` 确保订阅存在，再 `doShutdown(nil)`。

**保证**：

- 取得执行权的调用**同步等待全部 handler 完成**后才返回。
- 未取得执行权的调用立即返回，不等待清理结束；不能把任意一次 `Shutdown()` 返回当作清理完成。
- handler 内重入安全：CAS 已置位，立即返回（`TestShutdown_FullFlow`）。
- 传给 handler 的 `sig` 是 `nil`，可用于区分主动退出与信号退出（`TestExecuteShutdown_SignalPassedToHandler`）。

**边界**：主动路径的 `signal.Stop` 发生在清理完成之后、由监听 goroutine 执行；`Shutdown()` 返回不保证监听 goroutine 已退订。

### 5.5 `SigHandler` 与 `sig`

**签名**：`type SigHandler func(sig os.Signal)`

**语义**：`sig` 由取得执行权的路径决定（[quit.go:85-87](../quit.go#L85-L87)）：信号路径获胜时是那个终止信号，`Shutdown()` 获胜时是 `nil`。

**保证**：

- 同一级别内 handler 并发执行，各自 goroutine，各自 `recover`。
- `sig == nil` 只表示主动路径取得了执行权，不代表没有 OS 信号同时到达；并发触发不保证按外部观察到的到达顺序获胜。
- 所有 handler 收到的 `sig` 是同一个值，不会出现部分 `nil` 部分非 `nil`。

**边界**：不要用 `sig != nil` 作为「这次退出有信号」的前提，也不要在 handler 内假设别的 handler 已经做完。

## 6. 信号语义与平台矩阵

### 6.1 被接管的四个信号

`doListen` 一次性注册这四个信号（[quit.go:221](../quit.go#L221)）：

| 信号 | Unix 来源 | Unix 默认处置 | zcquit 接管后 | Windows |
|---|---|---|---|---|
| SIGHUP | 控制终端断开 | 终止进程 | **忽略**，继续等待 | 系统不投递 |
| SIGINT | Ctrl+C | 终止进程 | 触发退出 | Ctrl+C / Ctrl+Break 映射为 `os.Interrupt` |
| SIGTERM | `kill` 默认信号 | 终止进程 | 触发退出 | 控制台关闭/注销/关机事件；进程仍会被系统终止，只有有限清理窗口 |
| SIGQUIT | Ctrl+\ | 终止并打印 goroutine dump | 触发退出，**不再打印 dump** | 系统不投递 |

### 6.2 接管会禁用默认处置

Go 的 `os/signal` 文档（GOROOT `os/signal/doc.go` 的 Changing the behavior 一节）写明：`Notify` 会禁用 SIGHUP、SIGINT、SIGQUIT、SIGABRT、SIGTERM 的默认行为，改为投递到 channel。因此 zcquit 注册这四个信号后：

- **SIGHUP 不再导致进程退出**。这是「忽略」的副作用，不是「放行默认行为」；依赖 SIGHUP 做终端断开退出的程序会感知到差异。要恢复默认处置只能由调用方 `signal.Reset`（它会同时撤销 zcquit 的订阅），zcquit 不提供开关；若进程启动时该信号已被忽略，`Reset` 后仍保持忽略，系统默认处置不会恢复。
- **Ctrl+\ 不再打印 goroutine dump**，而是走 zcquit 的清理流程。排障时如果需要 dump，要另找入口。
- 这也是「脚本里 `kill -HUP` 不生效」的原因：信号被 zcquit 消费并继续等待。

### 6.3 第二个信号与 `signal.Stop`

zcquit 只退订自己的 channel，且两条路径的退订时点不同（3.2 节）。由此产生三种可观察结果：

| 场景 | 结果 | 锁死测试 |
|---|---|---|
| 信号路径已开始清理，handler 阻塞，无其他订阅者，再发一个终止信号 | 按平台默认处置终止进程，清理不保证完成 | `TestListen_SecondSignalDefaultTerminates` |
| 同上，但调用方另有 `signal.Notify` 订阅 | 不会被终止；第二个信号仍投递给其他订阅者 | `TestListen_SecondSignalWithIndependentNotify` |
| 主动路径的 handler 阻塞期间收到首个终止信号 | 监听 goroutine 消费它并退订，但 CAS 失败，不重复清理，也不强制终止 | `TestShutdown_FirstSignalDuringBlockedHandler` |

边界说明：`signal.Stop` 只在「所有订阅该信号的 channel 都被 Stop」时才恢复默认处置；若进程启动时该信号已被忽略，`Stop` 全部订阅后该信号仍保持忽略（Go 文档：`os/signal` 的 Changing the behavior 一节）。zcquit 无法判断其他订阅者是否存在，因此不能承诺「第二次 Ctrl+C 一定立即终止」。

### 6.4 Windows 限制

- Ctrl+C 与 Ctrl+Break 由 Go 运行时映射为 `_SIGINT`（GOROOT `runtime/os_windows.go:1104-1105`），对应 `os.Interrupt`。
- 关闭控制台、注销、关机映射为 `SIGTERM`，但 Windows 在控制台处理函数返回后仍会终止进程，只给有限清理机会；不能假设全部 handler 都能跑完。
- `SIGHUP` / `SIGQUIT` 不会被系统投递，注册它们没有实际效果。
- 对控制台程序，`taskkill`（不加 `/F`）会直接失败（`could not be terminated ... /F option`），进程继续运行；`taskkill /F` 强制终止，信号回调与清理都不执行。

## 7. 日志与可观测性

zcquit 用 `slog.Default()` 输出三类日志（[quit.go:181](../quit.go#L181)、[quit.go:190](../quit.go#L190)、[quit.go:198](../quit.go#L198)），框架自身日志统一带 `"zckg", "zcquit"`：

| 时机 | 级别 | 消息 | 关键字段 |
|---|---|---|---|
| 某个级别开始执行 | Info | `开始执行退出处理函数` | `shutdownLevel`、`count` |
| 某个级别全部完成 | Info | `退出处理函数执行完成` | `shutdownLevel` |
| 某个 handler panic | Error | `退出处理函数运行时发生 panic` | `shutdownLevel`、`panicErr` |

可观测含义：

- 级别是**串行**的，所以「开始 level N」一定在所有更低级别的「执行完成」之后。
- `count` 是该级别快照到的 handler 数，可用于核对注册是否符合预期。
- 同级别的多个 handler 并发执行，它们各自的业务日志顺序不保证。
- panic 日志出现在该级别的「开始」与「完成」之间；panic 被 `recover` 捕获，不影响其他 handler 和 `Listen` 返回。

一次两级退出的日志形态：

```text
time=... level=INFO msg="开始执行退出处理函数" zckg=zcquit shutdownLevel=0 count=1
time=... level=INFO msg="退出处理函数执行完成" zckg=zcquit shutdownLevel=0
time=... level=INFO msg="开始执行退出处理函数" zckg=zcquit shutdownLevel=1 count=1
time=... level=INFO msg="退出处理函数执行完成" zckg=zcquit shutdownLevel=1
```

日志走 `slog.Default()`，格式与去向完全由调用方配置；zcquit 不提供 logger 注入，也不要依赖日志来做退出同步——同步只能靠 handler 与 `waitChan`。

## 8. 可测试性与进程内一次性

### 8.1 为什么没有 `Reset`

`ctx`、`listenOnce`、`waitChan`、`stopListenCh` 都是包级状态，`quit.go` 不导出重置函数。原因不是偷懒：`signal.Notify` 的接管是进程级的，`waitChan` 的关闭不可逆；提供 `Reset` 只会制造「看起来重置了、实际还有残留订阅」的假象。因此 zcquit 的语义就是「一个进程一次优雅退出」。

### 8.2 无法重置的测试：子进程隔离

`zcquit_test` 包（`docs_examples_compile_test.go`）无法重置 zcquit，也不应该重置。因此外部包用 `exec` 跑独立示例程序，内部包（`quit_signal_integration_test.go`）用 `ZCQUIT_SIGNAL_HELPER` 环境变量自启子进程，两者都采用「测试二进制自启子进程」模式：

- 父进程设置环境变量、启动子进程、通过 stdout 握手（如 `READY`）确认订阅就绪，再投递真实信号。
- 每个场景使用全新进程，避免包级状态互相污染。
- 环境无法投递信号时明确 `t.Skip`，不把「没跑」伪装成「通过」。

### 8.3 包内白盒：`resetState` 的限制

`quit_test.go` 的 `resetState` 只重置 `ctx`、`cancel`、`signalHandlerMap`、`signalHandlerMux`、`waitChan`、`shutdownStarted`；`listenOnce` 与 `stopListenCh` 不重建。因此：

- 白盒测试**不能**使用 `t.Parallel()`；
- 依赖完整退出路径的用例集中放在文件末尾，并依赖同文件测试的声明顺序（`TestShutdown_FullFlow` 必须在 `TestListen_AfterShutdownReturnsImmediately` 之前）；
- 新增用例如果会触发 `Listen`/`Shutdown` 的完整流程，应优先走 8.2 的子进程模式，而不是扩展 `resetState`。

### 8.4 应用侧建议

- 在 `main` 里注册完全部 handler 再启动业务；不要在运行期动态增删依赖退出顺序的 handler。
- 需要「重启式」测试的应用，把 zcquit 的使用收敛到一个可替换的接口后面，测试时注入假实现，而不是试图重置 zcquit。
- 退出后同进程内再次调用 `Listen` 会立即返回，再次调用 `Shutdown` 会立即返回；这不代表清理「又跑了一次」。

## 9. 设计取舍

这一章回答「为什么不是另一种写法」。每条都给出替代方案与代价，便于判断修改实现时的风险。

### 9.1 为什么用 `atomic.Bool` CAS，而不是 `sync.Once`

`sync.Once.Do` 对**所有**调用方都会阻塞到函数返回；如果 handler 内部再次调用 `Shutdown()`，内层会等外层清理完成，而外层正等 handler 返回，形成死锁。`atomic.Bool` 的 CAS 让未取得执行权的调用**立即返回**，只让赢得 CAS 的调用同步执行（[quit.go:74-78](../quit.go#L74-L78)、[quit.go:149-154](../quit.go#L149-L154)）。代价是调用方不能假设「`Shutdown()` 返回 = 清理完成」，文档必须反复强调等待边界。

### 9.2 为什么 `waitChan` 与 `stopListenCh` 分开

`Listen` 只需要知道「handler 跑完了」，不需要知道「监听 goroutine 停了」。如果合成一个通道，主动路径下 `Listen` 就必须等监听 goroutine 完成 `signal.Stop` 才能返回，把 `Listen` 的返回时点绑到调度上；分开后，`Listen` 的语义最小、最稳定（[quit.go:62-69](../quit.go#L62-L69)）。

### 9.3 为什么 handler 执行期间不持锁

快照时持读锁复制 map，随后立即释放，handler 在锁外执行（[quit.go:163-169](../quit.go#L163-L169)）。这样 handler 内调用 `AddSigHandler` 不会死锁，长耗时 handler 也不会阻塞注册。代价是快照之后注册的 handler 不参与本次退出——这被明确写进契约，而不是缺陷。

### 9.4 为什么 `stopListenCh` 防二次关闭、`waitChan` 不防

生产路径上 `executeShutdown` 由 CAS 保证只执行一次，两者都不需要守卫。`stopListenCh` 多一层 `select` 守卫（[quit.go:201-208](../quit.go#L201-L208)）的直接理由是防止重复关闭导致 double close panic；包内测试依赖该守卫，但守卫并非只为测试存在。`waitChan` 保持无守卫（[quit.go:210-213](../quit.go#L210-L213)），因为重复调用 `executeShutdown` 本身就是违反不变量，测试用 `resetState` 重建通道来规避。这不是遗漏，而是「不给错误用法兜底」的取舍。

### 9.5 为什么 `SigHandler` 带 `os.Signal`

纯粹的无参 handler 无法区分「信号触发」和「主动退出」，而这两种场景在日志、告警、审计上往往需要区别对待。传 `os.Signal` 的成本极低，并且保留了「哪个信号取得了执行权」的信息；`nil` 语义（主动路径）让调用方可以选择忽略参数。代价是 `sig` 不能作为「是否发生过信号」的证据（[quit.go:85-87](../quit.go#L85-L87)）。

### 9.6 为什么没有超时与强制终止

zcquit 无法知道每个 handler 的合理耗时，也无法安全地中断任意 goroutine（Go 没有抢占式取消）。加一个全局超时只会把「卡死」变成「超时后进程仍卡死，但日志说超时了」。因此设计上要求 handler 自己带超时（如 `context.WithTimeout`），并把「单个 handler 卡死会阻塞整个退出」写成显式契约（[quit.go:256](../quit.go#L256)）。

### 9.7 为什么先 `cancel` 再执行 handler

如果先跑 handler 再取消上下文，handler 里等待业务收尾就会永久等待——业务根本不知道要退出。先取消、再清理，让「通知」与「等待」分离：通知是全局的、一次性的；等待由 handler 按级别显式表达。这也是「低级别等待业务、高级别关闭资源」这一惯用法的由来。

## 10. 常见错误与反例

**反例 1：在 handler 内调用 `Listen()` 等待清理完成**

```text
zcquit.AddSigHandler(0, func(sig os.Signal) {
    zcquit.Listen() // 死锁：Listen 等 waitChan，waitChan 等这个 handler 返回
})
```

正确做法：需要等待清理完成的代码放在 `Listen()` 之后的 `main` 里；handler 内只需完成自己的清理。

**反例 2：handler 没有超时**

```text
zcquit.AddSigHandler(1, func(sig os.Signal) {
    db.Close() // 底层网络挂起时可能永久阻塞
})
```

正确做法：handler 内使用带超时的操作，或把可能阻塞的清理拆到有超时保护的子流程里。整个退出流程会等这一级全部返回（3.4 节）。

**反例 3：用 `sig == nil` 判断「这次退出没有信号」**

```text
zcquit.AddSigHandler(0, func(sig os.Signal) {
    if sig == nil {
        metrics.Inc("manual_shutdown") // 并不成立：并发时信号可能同时到达
    }
})
```

正确做法：`sig` 只反映「哪条路径取得了执行权」，不能证明没有信号到达（5.5 节）。

**反例 4：退出后再注册 handler，期望它被执行**

```text
zcquit.Listen() // 已经返回，说明退出流程完成
zcquit.AddSigHandler(0, cleanup) // 不会执行，也不会重启监听
```

正确做法：所有 handler 在任何退出触发前注册完毕。

**反例 5：期望 SIGHUP 触发清理，或期望 Ctrl+\ 打印 goroutine dump**

zcquit 接管了这两个信号：SIGHUP 被忽略并继续等待，SIGQUIT 走清理流程而不再 dump（6.2 节）。需要 reload 或 dump 时，另行注册独立 handler，并注意 `signal.Stop` 不会撤销其他订阅者。

**反例 6：以为 `Listen()` 会等业务协程**

`Listen()` 只等 handler（3.4 节）。业务协程的收尾必须由某个 handler 显式等待（示例 1 的 level 0），否则 `main` 可能在业务尚未收尾时就返回。

## 11. 完整示例

### 11.1 主动触发退出

健康检查失败时主动走一次与信号相同的清理流程；`Shutdown()` 的调用方会等清理完成。

```go
// 示例 2：主动触发退出
import (
	"errors"
	"log/slog"
	"os"

	"github.com/buexplain/zckg/zcquit"
)

func main() {
	zcquit.AddSigHandler(0, func(sig os.Signal) {
		slog.Info("通知外部系统停止下发数据...", "signal", sig)
	})
	zcquit.AddSigHandler(1, func(sig os.Signal) {
		slog.Info("关闭数据库连接...", "signal", sig)
	})

	// 健康检查失败：主动触发与信号相同的清理流程
	go func() {
		if err := healthCheck(); err != nil {
			slog.Error("健康检查失败，主动退出", "error", err)
			zcquit.Shutdown()
		}
	}()

	zcquit.Listen()
	slog.Info("程序退出")
}

func healthCheck() error {
	return errors.New("模拟健康检查失败")
}
```

运行这个程序会立即完成一次主动退出：`sig` 为 `nil`，日志中能看到 level 0、level 1 依次执行，最后 `Listen()` 返回。

### 11.2 HTTP 服务多级清理

按依赖关系分三级：先摘流量，再关 HTTP 服务器等待在途请求，最后关底层连接。

```go
// 示例 3：HTTP 服务多级清理
import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/buexplain/zckg/zcquit"
)

func main() {
	srv := &http.Server{Addr: ":8080"}

	// 先注册全部清理 handler，再启动 HTTP 服务
	// level 0（最先执行）：通知外部系统停止下发流量
	zcquit.AddSigHandler(0,
		func(sig os.Signal) {
			slog.Info("通知网关摘除本节点...")
			// 通知负载均衡/注册中心摘除本节点
		},
	)

	// level 1：关闭 HTTP 服务器，等待现有请求处理完毕
	zcquit.AddSigHandler(1,
		func(sig os.Signal) {
			slog.Info("正在关闭 HTTP 服务器...", "signal", sig)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				slog.Error("HTTP 服务器关闭失败", "error", err)
			}
		},
	)

	// level 2（最后执行）：关闭底层资源连接
	zcquit.AddSigHandler(2,
		func(sig os.Signal) {
			slog.Info("正在关闭数据库连接...")
			// db.Close()
		},
		func(sig os.Signal) {
			slog.Info("正在关闭 Redis 连接...")
			// redisClient.Close()
		},
	)

	// 启动失败（如端口占用）也必须主动退出，否则 Listen 会一直等待
	go func() {
		slog.Info("HTTP 服务器启动", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("HTTP 服务器异常退出", "error", err)
			zcquit.Shutdown()
		}
	}()

	// 阻塞等待全部清理 handler 完成
	zcquit.Listen()
	slog.Info("程序已退出")
}
```

这个示例同时覆盖了「启动失败主动退出」与「分级清理」两条路径；`docs_examples_compile_test.go` 会用随机占用端口运行它，验证启动失败后三级清理各执行一次且 `Listen` 正常返回。

## 附录 A：行为—源码—测试映射

| 行为 | 实现 | 锁死测试 |
|---|---|---|
| 全局上下文只取消一次、先于 handler | [quit.go:149-154](../quit.go#L149-L154)、[quit.go:158-160](../quit.go#L158-L160) | `TestExecuteShutdown_ContextCancelled`、`TestExecuteShutdown_CancelBeforeHandlers` |
| handler 按 level 升序、同级并发、跨级串行 | [quit.go:171-199](../quit.go#L171-L199) | `TestExecuteShutdown_LevelOrder`、`TestExecuteShutdown_ConcurrentWithinLevel`、`TestExecuteShutdown_LevelSequential`、`TestExecuteShutdown_NegativeLevel` |
| 单个 handler panic 不影响其他 handler | [quit.go:186-194](../quit.go#L186-L194) | `TestExecuteShutdown_PanicRecovery`、`TestExecuteShutdown_AllHandlersPanic` |
| nil handler 注册即 panic，且不部分写入 | [quit.go:258-263](../quit.go#L258-L263) | `TestAddSigHandler_NilHandlerPanics` |
| 首次注册返回前 `signal.Notify` 已生效 | [quit.go:216-224](../quit.go#L216-L224) | `TestAddSigHandler_ListenerReady`、`TestAddSigHandler_ConcurrentFirstRegistrations` |
| 注册先入表、再初始化监听 | [quit.go:264-267](../quit.go#L264-L267) | `TestAddSigHandler_RegistersBeforeListening` |
| `Shutdown` 后 `Listen` 立即返回 | [quit.go:110-113](../quit.go#L110-L113)、[quit.go:213](../quit.go#L213) | `TestListen_AfterShutdownReturnsImmediately` |
| 退出只执行一次（信号与 `Shutdown` 并发） | [quit.go:149-154](../quit.go#L149-L154) | `TestShutdown_FullFlow`、`TestShutdown_FirstTriggerWins` |
| handler 内可重入 `Shutdown`、可 `AddSigHandler` | [quit.go:149-154](../quit.go#L149-L154)、[quit.go:163-169](../quit.go#L163-L169) | `TestShutdown_FullFlow`、`TestExecuteShutdown_HandlerCallsAddSigHandler` |
| 快照后新增的 handler 不执行 | [quit.go:164-169](../quit.go#L164-L169) | `TestExecuteShutdown_HandlerCallsAddSigHandler` |
| 无 handler 时也能正常完成 | [quit.go:171-213](../quit.go#L171-L213) | `TestExecuteShutdown_NoHandlers` |
| `sig` 由执行权路径决定 | [quit.go:85-87](../quit.go#L85-L87) | `TestExecuteShutdown_SignalPassedToHandler`、`TestShutdown_FirstTriggerWins` |
| SIGHUP 忽略、继续等待 | [quit.go:130-133](../quit.go#L130-L133) | `TestListen_SIGHUPIgnored`（Unix） |
| 第二个信号按平台默认终止 / 其他订阅者场景 | [quit.go:138-142](../quit.go#L138-L142) | `TestListen_SecondSignalDefaultTerminates`、`TestListen_SecondSignalWithIndependentNotify` |
| 主动路径清理期间的首个信号不重复清理 | [quit.go:204-208](../quit.go#L204-L208) | `TestShutdown_FirstSignalDuringBlockedHandler` |
| 通过低级别 handler 等待业务收尾 | 使用方模式，示例 1 | `TestListen_WaitsForJoinedWorker` |
| 并发注册与退出无竞争 | [quit.go:264-266](../quit.go#L264-L266) | `TestConcurrent_AddSigHandlerAndShutdown`（配合 `-race`） |

## 附录 B：文件结构

```text
zcquit/
├── quit.go                               # 核心实现：上下文、信号监听、分级清理
├── quit_test.go                          # 单元/白盒测试：级别顺序、并发、panic、幂等
├── quit_signal_integration_test.go       # 子进程 + 真实信号：触发、第二信号、主动路径
├── quit_signal_integration_unix_test.go  # Unix 专用：SIGHUP 忽略、SIGTERM 默认终止
├── quit_signal_platform_windows_test.go  # Windows 专用：控制事件投递与默认退出码
├── docs_examples_compile_test.go         # 本文 Go 示例的编译与运行校验
└── docs/
    └── zcquit.md                         # 本文
```

验证本文档的命令：

```shell
go test ./zcquit -run TestDocExamples -count=1 -v   # 示例编译与 HTTP 示例运行
go test ./zcquit -count=1 -race                     # 模块全量（含真实信号集成测试）
```
