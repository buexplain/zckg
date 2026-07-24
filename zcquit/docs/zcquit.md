# zcquit 优雅退出模块

## 概述

zcquit 是一个轻量级优雅退出（Graceful Shutdown）模块，提供两套相互配合的退出机制：

- **全局可取消上下文（Ctx）**：任意协程通过监听 `Ctx.Done()` 感知退出信号，实现协程级联关闭。
- **操作系统信号监听 + 分级清理 handler**：自动监听 SIGTERM / SIGINT / SIGQUIT 信号。信号到达时先取消上下文通知业务协程收尾，再按 level 升序分批执行 handler（同级别内并发、级别间串行）完成资源清理。
- **主动退出（Shutdown）**：提供 `Shutdown()` 函数，支持在代码中主动触发退出（如健康检查失败）。

`Listen()` 是阻塞调用，应放在 `main()` 末尾，等待信号触发退出流程。`AddSigHandler(level, handler...)` 按级别注册清理逻辑，退出时低级别优先执行。

## 文件结构

```
zcquit/
├── quit.go        # 核心实现：全局上下文初始化、信号监听、handler 注册与并发执行
├── quit_test.go   # 单元测试
└── docs/
    └── zcquit.md  # 本文档
```

## 架构设计

```
┌─────────────────────────────────────────────────────────┐
│                       zcquit 包                           │
│                                                          │
│  ┌──────── 初始化（init） ─────────────────────────────┐ │
│  │                                                    │ │
│  │  context.WithCancel(context.Background())           │ │
│  │    → 生成 Ctx 与 cancel                             │ │
│  │                                                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌──────── Handler 注册（AddSigHandler） ──────────────┐ │
│  │                                                    │ │
│  │  AddSigHandler(level, h1, h2, ...)                 │ │
│  │    ├─ doListen()  (sync.Once，仅首次生效，启动监听)   │ │
│  │    └─ signalHandlerMap[level] = append(..., h1, h2)│ │
│  │       （Lock 保护并发写入）                           │ │
│  │                                                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌──────── 阻塞等待（Listen） ─────────────────────────┐ │
│  │                                                    │ │
│  │  Listen()  →  doListen()  →  <-waitChan（阻塞）      │ │
│  │                               │                    │ │
│  │       waitChan 在 listen() 返回时关闭，解除阻塞      │ │
│  │                                                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌──────── 信号监听与处理（listen goroutine） ──────────┐ │
│  │                                                    │ │
│  │  signal.Notify(SIGHUP|SIGTERM|SIGINT|SIGQUIT)      │ │
│  │    │                                               │ │
│  │    ▼  循环读取信号通道                               │ │
│  │  SIGHUP  → continue（忽略）                          │ │
│  │  其他    → break                                    │ │
│  │    │                                               │ │
│  │    ▼                                               │ │
│  │  ① cancel() ─── 先取消上下文，通知业务协程           │ │
│  │    │                                               │ │
│  │    ▼                                               │ │
│  │  ② 按 level 升序分批执行 handler                     │ │
│  │     ├─ 快照 signalHandlerMap 并排序 level            │ │
│  │     ├─ 每 level 内：handler 并发（独立 goroutine）    │ │
│  │     ├─ panic recover（slog 记录错误，不影响其他）     │ │
│  │     └─ 每批 wg.Wait()，完成后进入下一 level          │ │
│  │    │                                               │ │
│  │    ▼                                               │ │
│  │  ③ close(waitChan) ─── Listen 解除阻塞并返回        │ │
│  │                                                    │ │
│  └────────────────────────────────────────────────────┘ │
│                                                          │
│  ┌──────── 协程退出感知 ───────────────────────────────┐ │
│  │                                                    │ │
│  │  业务协程中：                                        │ │
│  │    <-zcquit.Ctx.Done()                             │ │
│  │    // 执行收尾逻辑并 return                          │ │
│  │                                                    │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```

## 对外方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `Listen` | `func Listen()` | **阻塞调用**，等待操作系统信号并触发退出流程。内部通过 `sync.Once` 保证信号监听只启动一次。收到非 SIGHUP 信号后，先取消上下文，再并发执行所有 handler，最后返回。应放在 `main()` 末尾。 |
| `Shutdown` | `func Shutdown()` | 主动触发退出流程，效果等同于收到操作系统终止信号。取消全局 `Ctx`，触发 handler 并发执行，并使 `Listen` 返回。可安全多次调用，仅首次生效。适用于健康检查失败、管理接口关闭等场景。 |
| `AddSigHandler` | `func AddSigHandler(level int, handler ...SigHandler)` | 注册一个或多个信号处理函数到指定级别。退出时按 level 升序分批执行（同级别内并发，级别间串行）。首次调用隐式触发信号监听启动（`sync.Once`），后续调用仅追加 handler。每个 handler 独立 goroutine，带 panic 恢复。 |

## 导出变量与类型

| 名称 | 类型 | 说明 |
|------|------|------|
| `Ctx` | `context.Context` | 全局可取消上下文。协程通过 `<-Ctx.Done()` 感知退出信号。信号到达或 `Shutdown()` 调用后取消。 |
| `SigHandler` | `func(sig os.Signal)` | 信号处理函数类型。参数 `sig` 为实际接收到的操作系统信号（SIGTERM / SIGINT / SIGQUIT 之一）；通过 [Shutdown] 触发时 `sig` 为 `nil`。 |

## 内部机制

### 信号处理流程

```
操作系统信号到达        上下文取消          handler 分批执行          Listen 解除阻塞
      │                   │                      │                      │
      ▼                   ▼                      ▼                      ▼
  signal.Notify       cancel()          level_0: h1,h2 并发      close(waitChan)
  监听到 SIGTERM/         │               wg.Wait()                  │
  SIGINT/SIGQUIT          ▼                 │                      ▼
      │             Ctx.Done() 关闭      level_1: h3,h4 并发   Listen() 返回
      │             所有业务协程           wg.Wait()              main 退出
      │             开始收尾                  │
      │                   │             level_2: h5 并发
      │                   │             wg.Wait()
      │                   │                 │
      └───────────────────┴─────────────────┘
```

**关键时序**：
1. `cancel()` **先于** handler 执行 — 业务协程收到 `Ctx.Done()` 可立即开始收尾。
2. handler 按 level **升序分批**执行 — 同级别内并发，级别间串行；低级别（如通知外部系统停止下发数据）先完成，高级别（如关闭连接池）后执行。
3. 所有级别的 handler 完成后才关闭 `waitChan`，`Listen` 才返回。

### 同步机制

| 组件 | 用途 |
|------|------|
| `listenOnce`（`sync.Once`） | 保证 `listen` goroutine 全局仅启动一次 |
| `waitChan`（`chan struct{}`） | `Listen` 阻塞在此通道上，`executeShutdown` 完成所有 handler 执行后关闭，解除阻塞 |
| `signalHandlerMux`（`sync.RWMutex`） | `AddSigHandler` 写锁追加到 map；`executeShutdown` 读锁快照后立即释放（handler 在锁外分批执行） |
| `wg`（`sync.WaitGroup`） | 等待所有并发 handler 完成 |

## 线程安全

- **`Ctx` / `cancel`**：`context.WithCancel` 返回的 cancel 函数并发安全，可被多次调用（仅首次生效）。
- **`AddSigHandler`**：持 `Lock` 追加 handler 到 map，与 `executeShutdown` 快照时的 `RLock` 互斥。
- **handler 执行**：在 `RLock` 释放后启动 goroutine，因此 handler 内部可安全调用 `AddSigHandler`（不会死锁）。
- **panic 隔离**：每个 handler 有独立的 `recover`，单个 handler panic 不影响其他 handler，错误通过 `slog` 记录。

## 使用示例

### 基本用法：Listen 阻塞主协程

```go
package main

import (
    "log/slog"
    "os"

    "github.com/buexplain/zckg/zcquit"
)

func main() {
    // 1. 注册清理 handler（在 Listen 之前任意时机）
    zcquit.AddSigHandler(0, func(sig os.Signal) {
        slog.Info("开始清理资源...", "signal", sig)
        // 关闭数据库连接、刷新日志缓冲区等
    })

    // 2. 启动业务协程
    go runServer()

    // 3. 阻塞等待退出信号，所有 handler 执行完毕后返回
    zcquit.Listen()
    slog.Info("程序优雅退出")
}

func runServer() {
    for {
        select {
        case <-zcquit.Ctx.Done():
            slog.Info("业务协程收到退出信号")
            return
        default:
            // 处理业务...
        }
    }
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
    "time"

    "github.com/buexplain/zckg/zcquit"
)

func main() {
    srv := &http.Server{Addr: ":8080"}

    // HTTP 服务启动
    go func() {
        slog.Info("HTTP 服务器启动", "addr", ":8080")
        if err := srv.ListenAndServe(); err != http.ErrServerClosed {
            slog.Error("HTTP 服务器异常退出", "error", err)
        }
    }()

    // 按资源依赖关系分级注册清理 handler
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

    // 阻塞等待退出
    zcquit.Listen()
    slog.Info("程序已退出")
}
```

## 注意事项

1. **`cancel()` 先于 handler**：上下文先取消，业务协程可立即感知并开始收尾；handler 随后按 level 分批执行清理逻辑。因此 handler 中如需访问业务资源，应自行处理同步（业务协程可能已在关闭资源）。

2. **handler 分级执行**：handler 按 level 升序分批执行，同级别内并发、级别间串行。应利用级别来表达资源间的依赖关系（如先通知外部系统停止下发数据，再关闭连接），同级别内的 handler 应互相独立。

3. **handler panic 隔离**：每个 handler 有独立的 `recover`，单个 panic 会被 `slog.Error` 记录（含所在 level），不影响同级别或后续级别的 handler 执行，也不影响 `Listen` 最终返回。

4. **handler 中可安全调用 `AddSigHandler`**：`executeShutdown` 在执行前已快照 handler map 并释放锁，因此 handler 中调用 `AddSigHandler` 不会死锁。但新增的 handler 仅在下次退出时生效（本次快照已固化）。

5. **SIGHUP 被忽略**：SIGHUP 通常由终端会话断开触发，不代表程序需要终止。如需响应 SIGHUP（如重新加载配置），可自行扩展。

6. **`Shutdown` 可多次调用**：内部通过 `shutdownOnce`（`sync.Once`）保证退出流程仅执行一次，`cancel` 函数本身也支持多次调用，后续调用为 no-op。

7. **退出流程不可逆**：信号监听 goroutine 在收到第一个非 SIGHUP 信号后退出循环，不会再响应后续信号。`waitChan` 关闭后不可重用。
