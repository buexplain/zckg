# zcquit 优雅退出模块

## 概述

zcquit 是一个轻量级优雅退出（Graceful Shutdown）模块，提供两套相互配合的退出机制（全局上下文 + 信号监听与分级清理），并提供 `Shutdown()` 作为主动触发入口：

- **全局可取消上下文（通过 `GetCtx()` 获取）**：任意协程通过监听 `GetCtx().Done()` 感知退出通知；取消上下文本身不等待业务协程收尾。
- **操作系统信号监听 + 分级清理 handler**：自动监听 SIGTERM / SIGINT / SIGQUIT 信号（另监听 SIGHUP 但忽略，不触发退出，见注意事项 5）。信号路径先取消自身信号订阅，再竞争退出执行权；获胜后先取消上下文通知业务协程收尾，再按 level 升序分批执行 handler（同级别内并发、级别间串行）完成资源清理。
- **主动退出（Shutdown）**：提供 `Shutdown()` 函数，支持在代码中主动触发退出（如健康检查失败）；信号订阅的取消时点与信号触发不同，见注意事项 8。

`Listen()` 是阻塞调用，应放在 `main()` 末尾，等待信号或主动退出触发的清理完成。`AddSigHandler(level, handler...)` 按级别注册清理逻辑，退出时低级别优先执行。所有资源 handler 必须在任何退出触发前注册完毕；首次注册返回时信号订阅已就绪，但不代表后续级别也已注册。

## 文件结构

```
zcquit/
├── quit.go                               # 核心实现：上下文、信号监听与分级清理
├── quit_test.go                          # 单元测试
├── quit_signal_integration_test.go       # 真实信号与子进程的共同测试流程
├── quit_signal_platform_windows_test.go  # Windows 控制事件投递
├── quit_signal_integration_unix_test.go  # Unix 信号投递与 SIGHUP 测试
├── docs_examples_compile_test.go         # 正式文档示例编译及运行时回归
└── docs/
    └── zcquit.md                         # 本文档
```

## 架构设计

```
init → context.WithCancel → ctx（GetCtx 只读访问）+ cancel

AddSigHandler → 校验全部 handler 非 nil
              → 加写锁 → append handler → 解锁 → doListen
Listen        → doListen → 等待 waitChan 关闭
Shutdown      → doListen → doShutdown(nil)

 doListen（sync.Once，仅初始化一次）
   创建信号 channel → 同步 signal.Notify(SIGHUP/SIGTERM/SIGINT/SIGQUIT)
                   → go listen(channel) → 返回（信号订阅已就绪）

 listen goroutine（消费已注册的 channel，不在此调用 Notify）
   ├─ SIGHUP：忽略，继续监听
   ├─ 终止信号：signal.Stop(channel) → doShutdown(sig) → 返回
   └─ stopListenCh 关闭：signal.Stop(channel) → 返回

 doShutdown(sig)（与主动 Shutdown 共用 CAS）
   ├─ 未获执行权：立即返回，不等待
   └─ 获得执行权：同步 executeShutdown(sig)
        ① cancel()：通知业务协程，不等待其完成
        ② 读锁快照 handler map，解锁后按 level 升序执行
           每级 handler 并发、独立 recover；wg.Wait 后进入下一级
        ③ close(stopListenCh)：通知监听协程停止
        ④ close(waitChan)：Listen 可以返回，不 join 监听协程

业务协程 → 收到 ctx.Done → 收尾 → 关闭 done
需要等待业务完成时：低级别 handler 等待 done，高级别 handler 关闭依赖资源
```

## 对外方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `Listen` | `func Listen()` | **阻塞调用**，等待信号或主动 `Shutdown()` 触发的全部 handler 完成。首次初始化时同步完成 `signal.Notify`，再启动监听 goroutine（`sync.Once`）。应放在 `main()` 末尾；若退出流程此前已完成则立即返回。返回不保证普通业务协程或监听 goroutine 已退出；不要在 handler 内调用，否则循环等待。 |
| `Shutdown` | `func Shutdown()` | 主动竞争退出执行权。**CAS 获胜的调用同步等待**：取消上下文、按 level 升序分批清理（同级并发、跨级串行），完成后返回。未获执行权的调用立即返回，不等待；需要等待时在 handler 外调用 `Listen()`。信号订阅取消时点见注意事项 8。 |
| `AddSigHandler` | `func AddSigHandler(level int, handler ...SigHandler)` | 注册到指定级别（任意整数，按升序执行）。先校验全部 handler 非 nil，再加锁追加、解锁，最后调用 `doListen`；首次初始化同步完成 `signal.Notify` 后才启动 goroutine，故首次注册返回时本次 handler 已入表且信号订阅已就绪。后续调用仅追加，不重新启动监听；退出后追加的 handler 不执行。所有资源 handler 必须在任何退出触发前注册完毕。每个 handler 独立 goroutine，带 panic 恢复；传入 nil 直接 panic。 |
| `GetCtx` | `func GetCtx() context.Context` | 返回全局可取消上下文，不启动监听。获胜的退出路径在执行 handler 前取消它；协程通过 `<-GetCtx().Done()` 感知通知，但不会被自动等待，需自行用 done/WaitGroup 同步。以函数形式提供，避免导出变量被外部意外覆盖。 |

## 导出类型

| 名称 | 类型 | 说明 |
|------|------|------|
| `SigHandler` | `func(sig os.Signal)` | 信号处理函数类型。参数由 CAS 获胜路径决定：信号路径获胜为该终止信号，`Shutdown()` 获胜为 `nil`。`nil` 不代表没有 OS 信号到达，handler 不应假设 `sig` 非 nil（见注意事项 7）。 |

## 内部机制

### 信号处理流程

```
首次初始化（调用方）：创建 channel → 同步 signal.Notify → go listen(channel) → 返回

信号触发：listen 收到终止信号 → signal.Stop → CAS 获胜 ─┐
主动触发：Shutdown → doListen → CAS 获胜 ──────────────┤
                                                      ▼
              cancel → level_0 并发 + 等待 → level_1 并发 + 等待 → …
                                                      │
                                   全部 handler 完成 ─┘
                                      │
                                      ▼
                             close(stopListenCh)
                                      │
                                      ▼
                             close(waitChan) → Listen 返回

主动退出时，listener 收到 stopListenCh 后才执行 signal.Stop；Listen 不等待该步骤。
若主动清理期间收到首个终止信号，listener 可先消费它并 Stop，但 CAS 失败，不重复清理。
```

**关键时序**：
1. 注册先入表再初始化监听；`signal.Notify` 在调用方的 `sync.Once` 内同步完成，不在 `listen` 内执行。首次 `AddSigHandler` 返回是订阅就绪边界，不是全部级别注册完成的屏障。
2. `cancel()` **先于** handler 执行 — 业务协程收到 `Ctx.Done()` 可开始收尾，但此时不保证已开始或已完成收尾。
3. handler 按 level **升序分批**执行 — 同级别内并发，级别间串行；需要等待业务协程时，应在低级别 handler 等待 done/WaitGroup，再在更高级别关闭依赖资源。
4. 所有级别的 handler 完成后才关闭 `waitChan`，`Listen` 才返回；它不自动等待普通业务协程，也不 join 监听协程。

### 同步机制

| 组件 | 用途 |
|------|------|
| `listenOnce`（`sync.Once`） | 在 `doListen` 中仅一次创建 channel、同步 `signal.Notify`，完成后才 `go listen(channel)`；并发初始化调用也等待 Notify 完成 |
| `shutdownStarted`（原子 CAS） | 仅获胜路径同步执行清理并决定 `sig`；其他调用立即返回 |
| `waitChan`（`chan struct{}`） | `Listen` 阻塞在此通道上，`executeShutdown` 完成所有 handler 执行后关闭，解除阻塞；不代表监听协程已 Stop |
| `stopListenCh`（`chan struct{}`） | 全部 handler 完成后关闭；监听协程收到后取消自身信号订阅并退出（也可更早因终止信号 Stop） |
| `signalHandlerMux`（`sync.RWMutex`） | `AddSigHandler` 写锁追加到 map，解锁后调用 `doListen`；`executeShutdown` 读锁快照后立即释放（handler 在锁外分批执行） |
| `wg`（`sync.WaitGroup`） | 只等待当前级别的并发 handler，不自动跟踪业务协程 |

## 线程安全

- **全局上下文**：`context.WithCancel` 返回的 cancel 函数并发安全，可被多次调用（仅首次生效）；上下文仅包内赋值，外部通过 `GetCtx()` 只读访问，不会被意外覆盖。
- **`AddSigHandler`**：持 `Lock` 追加 handler 到 map，与 `executeShutdown` 快照时的 `RLock` 互斥。
- **handler 执行**：在 `RLock` 释放后启动 goroutine，因此 handler 内部可安全调用 `AddSigHandler`（不会死锁）。
- **panic 隔离**：每个 handler 有独立的 `recover`，单个 handler panic 不影响其他 handler，错误通过 `slog` 记录。

## 使用示例

### 基本用法：Listen 阻塞主协程

先启动一个等待放行的 worker，再注册等待它的 handler，避免早到信号让 handler 等待尚未启动的 worker。所有清理注册完毕才放行业务；等待放行期间也必须响应取消。这个启动门不使多次注册成为原子操作，仍应在任何退出触发前完成全部资源 handler 注册。

```go
package main

import (
    "log/slog"
    "os"

    "github.com/buexplain/zckg/zcquit"
)

func main() {
    workerStart := make(chan struct{})
    workerDone := make(chan struct{})
    go runServer(workerStart, workerDone) // 先启动，注册期间只等待放行或取消

    // level 0：ctx 仅通知退出，必须显式等待业务完成收尾
    zcquit.AddSigHandler(0, func(sig os.Signal) {
        <-workerDone
        slog.Info("业务协程已结束", "signal", sig)
    })
    // level 1：等待业务结束后，才能关闭其依赖资源
    zcquit.AddSigHandler(1, func(sig os.Signal) {
        slog.Info("开始清理资源...", "signal", sig)
        // 关闭数据库连接、刷新日志缓冲区等
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
    // 完成业务收尾后返回；此时依赖资源仍可用
}
```

### 注册多个并发执行的 handler

```go
// 同一级别的 handler 并发执行；不同级别间串行等待
// 示例：按资源依赖关系分级注册
zcquit.AddSigHandler(0,
    func(sig os.Signal) {
        slog.Info("通知外部系统停止下发数据...")
    },
)
zcquit.AddSigHandler(1,
    func(sig os.Signal) {
        slog.Info("关闭 HTTP 服务器...")
    },
)
zcquit.AddSigHandler(2,
    func(sig os.Signal) {
        slog.Info("关闭数据库连接...")
    },
    func(sig os.Signal) {
        slog.Info("刷新日志缓冲区...")
    },
)
```

### 手动触发退出

```go
func main() {
    zcquit.AddSigHandler(0, func(sig os.Signal) {
        slog.Info("清理资源...")
    })

    // 健康检查失败时主动退出
    go func() {
        if err := healthCheck(); err != nil {
            slog.Error("健康检查失败，主动退出", "error", err)
            zcquit.Shutdown()
        }
    }()

    zcquit.Listen()
    slog.Info("程序退出")
}
```

### 与 HTTP 服务器集成（多级清理示例）

```go
package main

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

## 注意事项

1. **`cancel()` 只通知，不 join 业务协程**：上下文先取消，业务协程可以开始收尾，handler 随后按 level 执行。`Listen()` 只等待 handler，不自动等待普通业务协程；应在适当级别的 handler 内通过 done/WaitGroup 等待业务结束，再关闭依赖资源（见基本示例），或在 main 返回前显式等待。

2. **handler 分级执行**：handler 按 level 升序分批执行，同级别内并发、级别间串行。应利用级别来表达资源间的依赖关系（如先通知外部系统停止下发数据，再关闭连接），同级别内的 handler 应互相独立。

3. **handler panic 隔离**：每个 handler 有独立的 `recover`，单个 panic 会被 `slog.Error` 记录（本模块框架日志统一带 `zckg=zcquit` 标识，并以 `shutdownLevel` 字段标记所在级别），不影响同级别或后续级别的 handler 执行，也不影响 `Listen` 最终返回。

4. **注册时机与监听就绪**：`AddSigHandler` 先校验、加锁追加、解锁，再初始化监听，首次返回前 `signal.Notify` 已完成，本次 handler 已入表。该保证不覆盖后续注册，也不保证清理尚未开始：全部资源 handler 仍须在任何退出触发前注册。handler 内追加不会死锁，但快照已固化，新增 handler 不执行；退出后再追加也不会重新启动监听或执行清理。

5. **SIGHUP 被忽略**：本模块在监听期间收到 SIGHUP 时继续等待，不触发退出；Unix 上该信号常见于终端会话断开。如需响应 SIGHUP（如重新加载配置），需自行实现相应处理。

6. **`Shutdown` 的等待边界**：CAS 获胜的 `Shutdown()` 调用会同步等待全部分级清理完成；其他调用立即返回，不能把任意一次 `Shutdown()` 返回当作清理完成。需要等待时调用 `Listen()`；若退出流程此前已完成，它会立即返回。handler 内可以重入 `Shutdown()`，但**不能调用 `Listen()`**，否则 handler 等待清理完成、清理又等待 handler，形成死锁。

7. **`sig` 由 CAS 获胜路径决定**：`Shutdown()` 获胜时为 `nil`，信号路径获胜时为该终止信号；几乎同时触发时，不保证按外部观察到的到达顺序获胜。`nil` 仅表示主动路径取得执行权，不代表没有 OS 信号到达。它不是由清理完成后监听 `select` 的停止通知分支决定的，handler 不应以 `sig` 非 nil 作为清理前提。

8. **退出不可逆，但信号取消时点因入口而异**：
   - 信号路径收到首个终止信号后，先 `signal.Stop` 取消自身订阅，再竞争 CAS 并执行清理。
   - 主动 `Shutdown()` 仅在全部 handler 完成后关闭 `stopListenCh`；监听协程收到后才 `signal.Stop`。主动清理期间，首个终止信号仍可被监听协程消费并使其取消订阅，但不会重复清理，也不保证立即强制终止。
   - `Listen()` 返回仅保证 handler 完成，不 join 监听协程，不保证它已实际执行 `signal.Stop`。
   - `signal.Stop` 只取消 zcquit 自身订阅。其他 `signal.Notify` 订阅者仍可阻止恢复默认信号行为；即使没有其他订阅者，也须结合平台及原先的信号处置判断，不能承诺“再次 Ctrl+C 一定立即终止”。监听不重启，`waitChan` 不可重用。

9. **handler 应自行控制超时**：退出流程会等待所有 handler 执行完毕（`wg.Wait()`），单个 handler 卡死会导致整个退出流程永久阻塞、进程无法退出。handler 内部应使用带超时的操作（如 `context.WithTimeout`，参考上文 HTTP 服务器集成示例中的 `srv.Shutdown(ctx)`）；等待业务完成也需按应用需求考虑超时策略。

10. **Windows 信号来源与限制**：Ctrl+C / Ctrl+Break 映射为 `SIGINT`；关闭控制台、注销或系统关机事件映射为 `SIGTERM`，但系统仍会终止进程，只提供有限的清理机会，不保证全部 handler 完成。`taskkill` 不是可靠的优雅退出入口，`taskkill /F` 强制终止会绕过 Go 信号回调和清理。Windows 不会实际投递 `SIGHUP` / `SIGQUIT`；`Shutdown()` 主动退出不受这些信号来源限制。

11. **nil handler 拒绝注册**：`AddSigHandler` 传入 nil handler 会直接 panic（注册期防御）；整批先校验，不会在发现 nil 前部分注册。请在注册前确保 handler 非 nil。
