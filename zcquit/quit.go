// Package zcquit 提供优雅退出（Graceful Shutdown）支持，包括全局可取消上下文和操作系统信号监听。
//
// 核心机制：
//   - 导出全局 [Ctx]，通过 context.WithCancel 创建。任意协程可监听 Ctx.Done() 来感知退出信号。
//   - [AddSigHandler] 注册分级信号处理函数（handler），按 level 升序分批执行：同级别并发、级别间串行。
//   - [Listen] 是阻塞调用，用于在主协程中等待操作系统信号并触发退出流程。
//   - [Shutdown] 用于在代码中主动触发退出（如健康检查失败），效果等同于收到终止信号。
//
// 退出流程：
//  1. 取消全局上下文，通知所有监听 [Ctx].Done() 的协程退出。
//  2. 按 level 升序分批执行 handler（同级别内并发、各级别间串行等待，每个 handler 带 panic 恢复）。
//  3. 等待所有 handler 执行完毕后，[Listen] 返回。
//
// 典型用法：
//
//	func main() {
//	    // 1. 注册自定义清理逻辑（在 Listen 之前任意时机注册均可）
//	    zcquit.AddSigHandler(0, func(sig os.Signal) {
//	        slog.Info("收到信号，开始清理资源...", "signal", sig)
//	        // 关闭数据库连接、刷新缓冲区等
//	    })
//
//	    // 2. 启动业务协程，协程内部监听 Ctx.Done()
//	    go func() {
//	        <-zcquit.Ctx.Done()
//	        slog.Info("上下文已取消，协程退出")
//	    }()
//
//	    // 3. 阻塞主协程，等待信号（或在协程中主动调用 Shutdown 触发退出）
//	    zcquit.Listen()
//	    slog.Info("程序退出")
//	}
package zcquit

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
)

// Ctx 全局可取消上下文。任意协程如需同步退出，都应监听该 ctx 的 Done() 通道。
// 当操作系统终止信号到达或 [Shutdown] 被调用时，Ctx 将被取消。
var Ctx context.Context

// cancel 是 Ctx 的取消函数，与 context.WithCancel 配对。
// 仅在包内使用：listen 中信号到达时调用，以及 [Shutdown] 中主动触发退出时调用。
var cancel context.CancelFunc

// listenOnce 确保信号监听 goroutine 只启动一次，由 [doListen] 函数内部使用。
var listenOnce = sync.Once{}

// waitChan 用于同步：退出流程完成后关闭此通道，从而解除 [Listen] 的阻塞。
// 该通道在 [executeShutdown] 中关闭。
var waitChan = make(chan struct{})

// signalHandlerMux 保护 signalHandlerMap 的并发读写安全。
var signalHandlerMux sync.RWMutex

// shutdownOnce 确保退出流程（取消上下文、执行 handler、关闭 waitChan）只执行一次，
// 无论是操作系统信号触发还是 [Shutdown] 主动调用触发。
var shutdownOnce sync.Once

// signalHandlerMap 按级别存储信号处理函数，key 为级别（level），value 为该级别下的 handler 列表。
// 退出时按 level 升序分批执行：同级别内 handler 并发执行，不同级别间串行等待。
// 读写受 [signalHandlerMux] 保护。
var signalHandlerMap = map[int][]SigHandler{}

// SigHandler 信号处理函数类型。参数 sig 为触发该处理函数的操作系统信号。
// [Shutdown] 主动调用时传入 nil，handler 可通过 sig == nil 区分触发来源。
type SigHandler func(sig os.Signal)

// init 在包加载时初始化全局的 [Ctx] 与 cancel。
func init() {
	Ctx, cancel = context.WithCancel(context.Background())
}

// Listen 阻塞等待操作系统终止信号，触发优雅退出流程。
//
// 该函数通过内部的 sync.Once 保证信号监听只启动一次，多次调用安全。
// 首次调用时启动后台监听 goroutine，随后阻塞在 waitChan 上，直到信号到达并完成处理。
//
// 监听的信号包括：
//   - syscall.SIGTERM：终止信号（如 kill 命令默认发送）
//   - syscall.SIGINT：中断信号（如 Ctrl+C）
//   - syscall.SIGQUIT：退出信号（如 Ctrl+\）
//   - syscall.SIGHUP：挂起信号（如终端会话断开）—— 该信号被忽略，不会触发退出流程。
//
// 当非 SIGHUP 信号到达时：
//  1. 先取消全局上下文，通知所有监听协程开始收尾。
//  2. 再按 level 升序分批执行 handler（同级别内并发，各级别间串行等待）。
//  3. 关闭 waitChan，Listen 解除阻塞并返回。
func Listen() {
	doListen()
	<-waitChan
}

// listen 是信号监听的核心实现，运行在独立的 goroutine 中。
// 它阻塞等待操作系统信号，收到非 SIGHUP 信号后执行退出流程。
func listen() {
	signalCH := make(chan os.Signal, 1)
	signal.Notify(signalCH, []os.Signal{
		syscall.SIGHUP,  // 挂起信号（终端会话断开），由循环内忽略
		syscall.SIGTERM, // 终止信号
		syscall.SIGINT,  // 中断信号（Ctrl+C）
		syscall.SIGQUIT, // 退出信号（Ctrl+\）
	}...)

	var sig os.Signal
	// 循环读取信号通道，直到收到非 SIGHUP 信号
	for sig = range signalCH {
		if sig == syscall.SIGHUP {
			// SIGHUP 仅表示终端会话断开，不代表程序需要终止，忽略该信号
			continue
		}
		break
	}

	// 触发退出流程（shutdownOnce 保证与 Shutdown 互斥，只执行一次）
	doShutdown(sig)
}

// doShutdown 执行退出流程的核心逻辑，通过 shutdownOnce 保证全局仅执行一次。
// 参数 sig 为触发退出的信号：OS 信号到达时传入具体信号值，[Shutdown] 调用时传入 nil。
func doShutdown(sig os.Signal) {
	shutdownOnce.Do(func() {
		executeShutdown(sig)
	})
}

// executeShutdown 是退出流程的实际执行逻辑，不依赖 sync.Once，可被测试直接调用。
// 参数 sig 为触发退出的信号：OS 信号到达时传入具体信号值，[Shutdown] 调用时传入 nil。
func executeShutdown(sig os.Signal) {
	// 步骤 1：先取消全局上下文，通知所有业务协程准备退出
	cancel()

	// 步骤 2：按 level 升序分批执行 handler
	// 先快照 handler 映射（持读锁），释放锁后再执行，避免长时间持锁
	signalHandlerMux.RLock()
	snapshot := make(map[int][]SigHandler, len(signalHandlerMap))
	for level, handlers := range signalHandlerMap {
		snapshot[level] = handlers
	}
	signalHandlerMux.RUnlock()

	// 收集所有级别并升序排序
	levels := make([]int, 0, len(snapshot))
	for level := range snapshot {
		levels = append(levels, level)
	}
	sort.Ints(levels)

	// 按级别从小到大分批执行：同级别内 handler 并发执行，级别间串行等待
	for _, level := range levels {
		handlers := snapshot[level]
		wg := sync.WaitGroup{}
		for _, handler := range handlers {
			wg.Add(1)
			h := handler // Go 1.22+ 循环变量自动独立，此处显式捕获以兼容旧版本阅读习惯
			go func() {
				defer wg.Done()
				defer func() {
					if panicErr := recover(); panicErr != nil {
						slog.Default().Error("退出处理函数运行时发生 panic", "level", level, "panicErr", panicErr)
					}
				}()
				h(sig)
			}()
		}
		// 等待当前级别所有 handler 执行完毕，再进入下一级别
		wg.Wait()
	}

	// 步骤 3：关闭 waitChan，解除 Listen 的阻塞
	close(waitChan)
}

// doListen 通过 sync.Once 确保 listen goroutine 只启动一次。
// 被 [Listen] 和 [AddSigHandler] 共同调用：
//   - [Listen] 被调用时触发首次启动。
//   - [AddSigHandler] 被调用时也尝试触发，保证即使在 [Listen] 之前注册 handler 也能正常监听。
func doListen() {
	listenOnce.Do(func() {
		go listen()
	})
}

// Shutdown 主动触发退出流程，效果等同于收到操作系统终止信号。
//
// 调用后会：取消全局 [Ctx]、并发执行所有已注册的 handler、
// 最后使阻塞在 [Listen] 上的调用返回。
//
// 与操作系统信号触发互斥：若信号先到达，Shutdown 为 no-op；
// Shutdown 先调用则后续信号不再触发重复的退出流程。
//
// 适用场景：健康检查失败、管理接口触发关闭等需要在代码中主动退出的场景。
func Shutdown() {
	// 确保信号监听已启动（若此前未调用 AddSigHandler 或 Listen）
	doListen()
	// 传入 nil：handler 中可通过判断 sig == nil 区分主动退出和信号退出
	doShutdown(nil)
}

// AddSigHandler 注册一个或多个信号处理函数到指定级别。
//
// level 决定 handler 的执行顺序：退出时按 level 从小到大分批执行，
// 同级别内的 handler 并发执行，不同级别之间串行等待（当前级别全部完成后才进入下一级别）。
// level 可以为任意整数值（含负数），没有预设的级别常量。
//
// 首次调用时会隐式触发信号监听的启动（通过内部的 sync.Once 保证仅启动一次）。
// 后续调用仅追加 handler，不会重复启动监听。
//
// handler 在退出时并发执行（各自在独立 goroutine 中），执行期间不持有锁，
// 因此 handler 中可以安全地调用 AddSigHandler 追加新的 handler（但仅影响下次退出，本次已开始的执行不受影响）。
// 每个 handler 内部若发生 panic，会被 recover 捕获并通过 slog 记录错误日志，不会影响其他 handler 的执行。
func AddSigHandler(level int, handler ...SigHandler) {
	doListen()
	signalHandlerMux.Lock()
	defer signalHandlerMux.Unlock()
	signalHandlerMap[level] = append(signalHandlerMap[level], handler...)
}
