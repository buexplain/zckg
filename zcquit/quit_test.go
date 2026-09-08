package zcquit

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// resetState 在每个测试前重置包级可变状态，使测试可重复执行。
// 测试直接调用 executeShutdown（不依赖幂等标记）以绕过全局状态；shutdownStarted 同步重置。
// stopListenCh 不重建：变量不重新赋值（包初始化创建一次），通道可被 executeShutdown 关闭和接收
// （close 属写操作语义，executeShutdown 以幂等守卫关闭）。
// listenOnce 不重置：listen goroutine 全局仅启动一次，依赖完整退出路径（Shutdown/Listen）的用例
// 统一放在文件末尾的 TestShutdown_FullFlow 和 TestListen_AfterShutdownReturnsImmediately 中
// （后者必须位于前者之后，依赖 go test 按声明顺序执行同文件测试的当前实现）。
// 警告：本函数并非完整重置（listenOnce、stopListenCh 不可重建），不得在白盒测试中使用 t.Parallel()。
func resetState() {
	ctx, cancel = context.WithCancel(context.Background())
	signalHandlerMap = map[int][]SigHandler{}
	signalHandlerMux = sync.RWMutex{}
	waitChan = make(chan struct{})
	shutdownStarted.Store(false)
}

// TestAddSigHandler 测试 AddSigHandler 注册 handler 到正确的级别
func TestAddSigHandler(t *testing.T) {
	resetState()

	called0 := false
	called1 := false

	AddSigHandler(0, func(sig os.Signal) {
		called0 = true
	})
	AddSigHandler(1, func(sig os.Signal) {
		called1 = true
	})

	// 验证 handler 已注册到正确的级别
	signalHandlerMux.RLock()

	if len(signalHandlerMap[0]) != 1 {
		signalHandlerMux.RUnlock()
		t.Fatalf("期望 level 0 有 1 个 handler，实际 %d", len(signalHandlerMap[0]))
	}
	if len(signalHandlerMap[1]) != 1 {
		signalHandlerMux.RUnlock()
		t.Fatalf("期望 level 1 有 1 个 handler，实际 %d", len(signalHandlerMap[1]))
	}

	// 执行 handler 验证可调用
	signalHandlerMap[0][0](nil)
	signalHandlerMap[1][0](nil)
	signalHandlerMux.RUnlock()

	if !called0 || !called1 {
		t.Fatal("handler 未被正确调用")
	}
}

// TestAddSigHandler_MultipleHandlersSameLevel 测试同一级别注册多个 handler
func TestAddSigHandler_MultipleHandlersSameLevel(t *testing.T) {
	resetState()

	AddSigHandler(0,
		func(sig os.Signal) {},
		func(sig os.Signal) {},
	)
	AddSigHandler(0, func(sig os.Signal) {})

	signalHandlerMux.RLock()
	defer signalHandlerMux.RUnlock()

	if len(signalHandlerMap[0]) != 3 {
		t.Fatalf("期望 level 0 有 3 个 handler，实际 %d", len(signalHandlerMap[0]))
	}
}

// TestExecuteShutdown_LevelOrder 测试 handler 按 level 升序执行，级别间串行
func TestExecuteShutdown_LevelOrder(t *testing.T) {
	resetState()

	var order []int
	var mu sync.Mutex

	AddSigHandler(2, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	})
	AddSigHandler(0, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 0)
		mu.Unlock()
	})
	AddSigHandler(1, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	})

	executeShutdown(nil)

	if len(order) != 3 {
		t.Fatalf("期望 3 个 handler 被执行，实际 %d", len(order))
	}
	// 级别间串行，所以顺序一定是 0 → 1 → 2
	for i, expected := range []int{0, 1, 2} {
		if order[i] != expected {
			t.Fatalf("期望第 %d 个执行的 level 为 %d，实际为 %d，完整顺序: %v", i, expected, order[i], order)
		}
	}
}

// TestExecuteShutdown_ConcurrentWithinLevel 测试同级别内 handler 并发执行
func TestExecuteShutdown_ConcurrentWithinLevel(t *testing.T) {
	resetState()

	var running atomic.Int32
	var maxConcurrent atomic.Int32
	var executed atomic.Int32

	for i := 0; i < 5; i++ {
		AddSigHandler(0, func(sig os.Signal) {
			executed.Add(1)
			cur := running.Add(1)
			// 更新最大并发数
			for {
				old := maxConcurrent.Load()
				if cur <= old || maxConcurrent.CompareAndSwap(old, cur) {
					break
				}
			}
			time.Sleep(50 * time.Millisecond) // 保持一段时间以检测并发
			running.Add(-1)
		})
	}

	executeShutdown(nil)

	// 全部 handler 必须执行完毕：WaitGroup 提前完成等遗漏执行的 bug 应在此暴露
	if executed.Load() != 5 {
		t.Fatalf("期望 5 个 handler 全部执行，实际执行 %d 个", executed.Load())
	}
	if maxConcurrent.Load() < 2 {
		t.Fatalf("期望同级别 handler 存在并发执行（最大并发 >= 2），实际最大并发: %d", maxConcurrent.Load())
	}
}

// TestExecuteShutdown_LevelSequential 测试不同级别间严格串行
func TestExecuteShutdown_LevelSequential(t *testing.T) {
	resetState()

	var level0Done atomic.Bool
	var level1Overlap atomic.Bool

	AddSigHandler(0, func(sig os.Signal) {
		time.Sleep(50 * time.Millisecond)
		level0Done.Store(true)
	})
	AddSigHandler(1, func(sig os.Signal) {
		// 如果 level 0 还没完成，说明存在重叠（不应该发生）
		if !level0Done.Load() {
			level1Overlap.Store(true)
		}
	})

	executeShutdown(nil)

	if level1Overlap.Load() {
		t.Fatal("level 1 的 handler 在 level 0 完成前就开始执行，级别间未串行等待")
	}
}

// TestExecuteShutdown_PanicRecovery 测试单个 handler panic 不影响其他 handler
func TestExecuteShutdown_PanicRecovery(t *testing.T) {
	resetState()

	// 同级与高级 handler 分别用独立标记：共享布尔无法区分两者是否分别执行
	var level0NormalCalled bool
	var level1Called bool

	AddSigHandler(0,
		func(sig os.Signal) {
			panic("模拟 panic")
		},
		func(sig os.Signal) {
			level0NormalCalled = true
		},
	)
	// 高级别的 handler 也应正常执行
	AddSigHandler(1, func(sig os.Signal) {
		level1Called = true
	})

	executeShutdown(nil)

	if !level0NormalCalled {
		t.Fatal("level 0 的 panic handler 不应影响同级别正常 handler 的执行")
	}
	if !level1Called {
		t.Fatal("某个 handler panic 后，高级别 handler 未被执行")
	}
}

// TestExecuteShutdown_ContextCancelled 测试 executeShutdown 取消全局上下文
func TestExecuteShutdown_ContextCancelled(t *testing.T) {
	resetState()

	select {
	case <-GetCtx().Done():
		t.Fatal("executeShutdown 之前 Ctx 不应已被取消")
	default:
	}

	executeShutdown(nil)

	select {
	case <-GetCtx().Done():
		// 期望：上下文已取消
	default:
		t.Fatal("executeShutdown 之后 Ctx 应已被取消")
	}
}

// TestExecuteShutdown_WaitChanClosed 测试 executeShutdown 关闭 waitChan 以解除 Listen 阻塞
func TestExecuteShutdown_WaitChanClosed(t *testing.T) {
	resetState()

	select {
	case <-waitChan:
		t.Fatal("executeShutdown 之前 waitChan 不应已关闭")
	default:
	}

	executeShutdown(nil)

	select {
	case <-waitChan:
		// 期望：waitChan 已关闭
	case <-time.After(time.Second):
		t.Fatal("executeShutdown 之后 waitChan 应已关闭")
	}
}

// TestExecuteShutdown_SignalPassedToHandler 测试信号值正确传递给 handler：
// 覆盖 nil（Shutdown 主动触发）与具体信号值（OS 信号触发）两条路径。
func TestExecuteShutdown_SignalPassedToHandler(t *testing.T) {
	resetState()

	var receivedSig os.Signal

	AddSigHandler(0, func(sig os.Signal) {
		receivedSig = sig
	})

	// executeShutdown(nil) 模拟 Shutdown() 主动调用，handler 收到 nil
	executeShutdown(nil)

	if receivedSig != nil {
		t.Fatalf("Shutdown 触发时 handler 应收到 nil 信号，实际收到 %v", receivedSig)
	}

	// 第二阶段：resetState 后验证具体信号值经退出流程原样传递给 handler
	resetState()
	receivedSig = nil

	AddSigHandler(0, func(sig os.Signal) {
		receivedSig = sig
	})

	// executeShutdown(syscall.SIGTERM) 模拟 OS 信号触发，handler 收到该信号值
	executeShutdown(syscall.SIGTERM)

	if receivedSig != syscall.SIGTERM {
		t.Fatalf("OS 信号触发时 handler 应收到 %v，实际收到 %v", syscall.SIGTERM, receivedSig)
	}
}

// TestExecuteShutdown_NegativeLevel 测试负数 level 也能正确排序执行
func TestExecuteShutdown_NegativeLevel(t *testing.T) {
	resetState()

	var order []int
	var mu sync.Mutex

	AddSigHandler(1, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	})
	AddSigHandler(-1, func(sig os.Signal) {
		mu.Lock()
		order = append(order, -1)
		mu.Unlock()
	})
	AddSigHandler(0, func(sig os.Signal) {
		mu.Lock()
		order = append(order, 0)
		mu.Unlock()
	})

	executeShutdown(nil)

	expected := []int{-1, 0, 1}
	// 先校验长度：handler 遗漏时给出清晰的失败消息，而非索引越界 panic（与 LevelOrder 一致）
	if len(order) != 3 {
		t.Fatalf("期望 3 个 handler 被执行，实际 %d", len(order))
	}
	for i, v := range expected {
		if order[i] != v {
			t.Fatalf("期望执行顺序 %v，实际 %v", expected, order)
		}
	}
}

// TestExecuteShutdown_NoHandlers 测试没有注册 handler 时 executeShutdown 也能正常完成
func TestExecuteShutdown_NoHandlers(t *testing.T) {
	resetState()

	// 不应 panic 或死锁
	executeShutdown(nil)

	select {
	case <-GetCtx().Done():
		// 上下文应已取消
	case <-time.After(time.Second):
		t.Fatal("无 handler 时 executeShutdown 应仍能正常完成")
	}

	select {
	case <-waitChan:
		// waitChan 应已关闭
	default:
		t.Fatal("无 handler 时 waitChan 也应被关闭")
	}
}

// TestCtx_InitialState 测试初始状态下 Ctx 未被取消
func TestCtx_InitialState(t *testing.T) {
	resetState()

	select {
	case <-GetCtx().Done():
		t.Fatal("初始状态下 Ctx 不应已被取消")
	default:
		// 期望：未取消
	}
}

// TestExecuteShutdown_CancelBeforeHandlers 测试 cancel 在 handler 执行之前被调用
func TestExecuteShutdown_CancelBeforeHandlers(t *testing.T) {
	resetState()

	var ctxDoneDuringHandler atomic.Bool

	AddSigHandler(0, func(sig os.Signal) {
		// handler 执行时，Ctx 应该已经被取消
		select {
		case <-GetCtx().Done():
			ctxDoneDuringHandler.Store(true)
		default:
		}
	})

	executeShutdown(nil)

	if !ctxDoneDuringHandler.Load() {
		t.Fatal("handler 执行时 Ctx 应已被取消（cancel 应先于 handler 执行）")
	}
}

// TestAddSigHandler_NilHandlerPanics 测试注册 nil handler 立即 panic（注册期防御）
func TestAddSigHandler_NilHandlerPanics(t *testing.T) {
	resetState()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("注册 nil handler 应触发 panic")
		}
		// 断言 panic 值为预期的注册期防御消息，避免其他意外 panic 被误判为通过
		if msg, ok := r.(string); !ok || !strings.Contains(msg, "zcquit: 不允许注册 nil 的 SigHandler") {
			t.Fatalf("panic 值应为 nil handler 注册防御消息，实际: %v", r)
		}
		// AddSigHandler 先校验全部参数再加锁写入，panic 发生于任何写入之前，
		// 混合传入中的非 nil handler 不应残留（注册原子性的防御性断言）
		signalHandlerMux.RLock()
		n := len(signalHandlerMap[0])
		signalHandlerMux.RUnlock()
		if n != 0 {
			t.Fatalf("panic 前不应有 handler 写入，level 0 实际已有 %d 个", n)
		}
	}()

	// 混合传入：非 nil 在前，nil 在后，也应 panic
	AddSigHandler(0, func(sig os.Signal) {}, nil)
}

// TestExecuteShutdown_HandlerCallsAddSigHandler 测试 handler 内调用 AddSigHandler 无死锁，且新 handler 不被本次退出执行
func TestExecuteShutdown_HandlerCallsAddSigHandler(t *testing.T) {
	resetState()

	var newHandlerCalled atomic.Bool

	AddSigHandler(0, func(sig os.Signal) {
		// 注册更高 level 的新 handler：快照已固化，本次不应执行
		AddSigHandler(1, func(sig os.Signal) {
			newHandlerCalled.Store(true)
		})
	})

	// 若无死锁，executeShutdown 将正常返回
	executeShutdown(nil)

	if newHandlerCalled.Load() {
		t.Fatal("退出期间新注册的 handler 不应被本次退出执行（快照已固化）")
	}
}

// TestExecuteShutdown_AllHandlersPanic 测试所有 handler 均 panic 时退出流程仍能完成
func TestExecuteShutdown_AllHandlersPanic(t *testing.T) {
	resetState()

	AddSigHandler(0,
		func(sig os.Signal) { panic("panic-1") },
		func(sig os.Signal) { panic("panic-2") },
	)
	AddSigHandler(1, func(sig os.Signal) { panic("panic-3") })

	// 若无 wg.Done 泄漏，executeShutdown 将正常返回（不会永久阻塞）
	executeShutdown(nil)

	select {
	case <-waitChan:
		// 期望：全部 handler panic 后 waitChan 仍被关闭
	default:
		t.Fatal("全部 handler panic 后 waitChan 仍应被关闭")
	}

	select {
	case <-GetCtx().Done():
		// 期望：handler 全部 panic 不影响上下文取消（退出流程步骤 1 照常完成）
	default:
		t.Fatal("全部 handler panic 后上下文仍应被取消")
	}
}

// TestConcurrent_AddSigHandlerAndShutdown 压力测试：并发注册 handler 与退出流程并发进行，验证无竞争无死锁
func TestConcurrent_AddSigHandlerAndShutdown(t *testing.T) {
	resetState()

	start := make(chan struct{})
	var wg sync.WaitGroup

	// 20 个 goroutine 并发注册 handler，与退出流程的快照/执行形成竞争窗口
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			AddSigHandler(0, func(sig os.Signal) {})
		}()
	}

	close(start)
	executeShutdown(nil) // 与并发注册竞争：竞争安全性由 -race 检测
	wg.Wait()

	// 并发注册的 handler 可能部分落入快照之后，精确执行数量无法断言；
	// 核心验证点：无 panic、无死锁、无数据竞争，退出流程正常完成
	select {
	case <-waitChan:
		// 期望：waitChan 已关闭
	default:
		t.Fatal("并发压力场景下 waitChan 应被关闭")
	}
}

// TestShutdown_FullFlow 依赖幂等标记与 listenOnce 的完整退出路径测试（声明在文件末尾）。
// 覆盖：Shutdown 幂等、信号路径与 Shutdown 并发收敛、handler 内调用 Shutdown/AddSigHandler（无死锁）、多协程并发 Listen 解除阻塞。
func TestShutdown_FullFlow(t *testing.T) {
	resetState()

	var execCount atomic.Int32
	var lateHandlerCalled atomic.Bool

	AddSigHandler(0, func(sig os.Signal) {
		execCount.Add(1)
		Shutdown() // handler 内重入：CAS 已置位，应立即返回为 no-op，无死锁
		// 注册高 level 新 handler：快照已固化，本次不应执行
		AddSigHandler(9, func(sig os.Signal) { lateHandlerCalled.Store(true) })
	})

	// 多个 goroutine 并发 Listen，均应被解除阻塞
	const listenerN = 3
	var listenDone sync.WaitGroup
	for i := 0; i < listenerN; i++ {
		listenDone.Add(1)
		go func() {
			defer listenDone.Done()
			Listen()
		}()
	}

	// 并发触发：多个 Shutdown + doShutdown(信号) 模拟「信号到达 + Shutdown 并发」场景
	var trigger sync.WaitGroup
	for i := 0; i < 10; i++ {
		trigger.Add(1)
		go func() {
			defer trigger.Done()
			Shutdown()
		}()
	}
	for i := 0; i < 10; i++ {
		trigger.Add(1)
		go func() {
			defer trigger.Done()
			doShutdown(syscall.SIGTERM)
		}()
	}
	trigger.Wait()

	// 验证所有 Listen 均被解除阻塞
	waitListenDone := make(chan struct{})
	go func() {
		listenDone.Wait()
		close(waitListenDone)
	}()
	select {
	case <-waitListenDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Shutdown 后并发 Listen 未在超时内返回")
	}

	// 显式断言 waitChan 已关闭：Listen 返回仅是间接证据，此处直接验证退出流程收尾步骤
	select {
	case <-waitChan:
		// 期望：完整退出流程后 waitChan 已被关闭
	default:
		t.Fatal("完整退出流程后 waitChan 应已被关闭")
	}

	if execCount.Load() != 1 {
		t.Fatalf("退出流程应仅执行一次，实际 handler 执行次数: %d", execCount.Load())
	}
	if lateHandlerCalled.Load() {
		t.Fatal("handler 内新注册的 handler 不应被本次退出执行")
	}
	select {
	case <-GetCtx().Done():
		// 期望：上下文已取消
	default:
		t.Fatal("完整退出流程后上下文应已被取消")
	}
}

// TestListen_AfterShutdownReturnsImmediately 锁死「Shutdown 先于 Listen 调用时，Listen 立即返回」不变量：
// 先同步完成整个退出流程，再调用 Listen，应因 waitChan 已关闭而立即返回（不阻塞等待信号）。
// 依赖 listenOnce 全局仅启动一次，声明在 TestShutdown_FullFlow 之后（与现有布局约定一致）。
func TestListen_AfterShutdownReturnsImmediately(t *testing.T) {
	resetState()

	Shutdown() // 先同步完成整个退出流程

	done := make(chan struct{})
	go func() {
		Listen()
		close(done)
	}()

	select {
	case <-done: // 期望：立即返回
	case <-time.After(time.Second):
		t.Fatal("Shutdown 完成后 Listen 应立即返回")
	}
}
